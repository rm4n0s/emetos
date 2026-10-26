package golang

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// The events of `go test -json` (step 14.3). The fixtures in testdata/testjson are the output of the real go test (Go 1.27) for small
// packages: mixed (a pass, a failure, a skip, a table of subtests with a failure), panic (a test that panics), build (a package whose
// test does not build; the build events of Go 1.24 and later), and by hand build_legacy (the same failure as older Go printed it), nofiles
// and passing.

// fixture reads a file of testdata/testjson with {{DIR}} replaced by the folder of the package.
func fixture(t *testing.T, name, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "testjson", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(strings.ReplaceAll(string(data), "{{DIR}}", dir)), "\n")
}

// show writes the tree of the results as indented lines: the package, its status and time, and its tests.
func show(r *testResults) string {
	names := map[int]string{statusRunning: "running", statusPass: "pass", statusFail: "fail", statusSkip: "skip"}
	var sb strings.Builder
	var walk func(nodes []*testNode, depth int)
	walk = func(nodes []*testNode, depth int) {
		for _, n := range nodes {
			sb.WriteString(strings.Repeat("  ", depth) + n.short + " " + names[n.status])
			if n.hasElapsed {
				sb.WriteString(" " + strconv.FormatFloat(n.elapsed, 'f', -1, 64))
			}
			sb.WriteString("\n")
			walk(n.children, depth+1)
		}
	}
	for _, p := range r.packages {
		sb.WriteString(p.path + " " + names[p.status])
		if p.buildFailed {
			sb.WriteString(" build-failed")
		}
		sb.WriteString("\n")
		walk(p.tests, 1)
	}
	return sb.String()
}

func TestFixturesOfGoTestParseIntoTheTree(t *testing.T) {
	tests := []struct {
		file   string
		want   string
		counts testCounts
		output map[string]string // the output of a node, by "package" or "package:test"
	}{
		{"mixed.jsonl", "example.com/mixed fail\n  TestAddPasses pass 0\n  TestAddFails fail 0\n  TestSkipped skip 0\n  TestTable fail 0\n    small pass 0\n    wrong fail 0\n",
			testCounts{Pass: 2, Fail: 3, Skip: 1, Packages: 1, FailedPackages: 1}, map[string]string{
				"example.com/mixed:TestAddFails":    "    mixed_test.go:14: Add(2, 2) = 4, want 5\n--- FAIL: TestAddFails (0.00s)",
				"example.com/mixed:TestTable/wrong": "    mixed_test.go:29: Add(2, 3) = 5, want 6\n--- FAIL: TestTable/wrong (0.00s)",
				"example.com/mixed:TestSkipped":     "    mixed_test.go:19: needs a network\n--- SKIP: TestSkipped (0.00s)",
				"example.com/mixed":                 "FAIL\nexit status 1\nFAIL\texample.com/mixed\t0.002s",
			}},
		{"passing.jsonl", "example.com/ok pass\n  TestOne pass 1.25\n  TestTwo pass 0.01\n", testCounts{Pass: 2, Packages: 1}, map[string]string{
			"example.com/ok": "PASS\nok  \texample.com/ok\t1.3s", "example.com/ok:TestOne": "--- PASS: TestOne (1.25s)",
		}},
		{"panic.jsonl", "example.com/panics fail\n  TestOk pass 0\n  TestBoom fail 0\n", testCounts{Pass: 1, Fail: 1, Packages: 1, FailedPackages: 1}, nil},
		{"build.jsonl", "example.com/broken fail build-failed\n", testCounts{Packages: 1, FailedPackages: 1, BuildFailed: 1}, map[string]string{
			"example.com/broken": "# example.com/broken [example.com/broken.test]\n./b_test.go:6:12: undefined: undefinedName\nFAIL\texample.com/broken [build failed]",
		}},
		{"build_legacy.jsonl", "example.com/broken fail build-failed\n", testCounts{Packages: 1, FailedPackages: 1, BuildFailed: 1}, map[string]string{
			"example.com/broken": "# example.com/broken [example.com/broken.test]\n./b_test.go:6:12: undefined: undefinedName\nFAIL\texample.com/broken [build failed]",
		}},
		{"nofiles.jsonl", "example.com/empty skip\n", testCounts{Packages: 1}, map[string]string{"example.com/empty": "?   \texample.com/empty\t[no test files]"}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			r := newTestResults()
			for _, l := range fixture(t, tt.file, "/work/p") {
				r.addLine(l)
			}
			r.finish()
			if got := show(r); got != tt.want {
				t.Errorf("tree =\n%s\nwant\n%s", got, tt.want)
			}
			if c := r.counts(); c != tt.counts {
				t.Errorf("counts = %+v, want %+v", c, tt.counts)
			}
			for key, want := range tt.output {
				pkg, test, _ := strings.Cut(key, ":")
				p := r.byPath[pkg]
				if p == nil {
					t.Fatalf("no package %s", pkg)
				}
				lines := p.output
				if test != "" {
					lines = p.byName[test].output
				}
				if got := strings.Join(lines, "\n"); got != want {
					t.Errorf("output of %s =\n%q\nwant\n%q", key, got, want)
				}
			}
		})
	}
}

func TestAPanicInATestFailsTheTestAndTheOutputKeepsTheTrace(t *testing.T) {
	r := newTestResults()
	for _, l := range fixture(t, "panic.jsonl", "/work/p") {
		r.addLine(l)
	}
	p := r.byPath["example.com/panics"]
	boom := p.byName["TestBoom"]
	if boom.status != statusFail || p.status != statusFail {
		t.Fatalf("statuses: test %d package %d", boom.status, p.status)
	}
	out := strings.Join(boom.output, "\n")
	for _, want := range []string{"panic: assignment to entry in nil map", "goroutine ", "/work/p/p.go:3", "/work/p/p_test.go:9"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output of the test does not have %q:\n%s", want, out)
		}
	}
}

func TestATestStillRunningWhenThePackageFailsDidNotPass(t *testing.T) {
	r := newTestResults()
	for _, l := range []string{
		`{"Action":"run","Package":"p","Test":"TestHang"}`,
		`{"Action":"output","Package":"p","Test":"TestHang","Output":"panic: test timed out after 30s\n"}`,
		`{"Action":"fail","Package":"p","Elapsed":30}`,
	} {
		r.addLine(l)
	}
	if got := r.byPath["p"].byName["TestHang"].status; got != statusFail {
		t.Errorf("status = %d, want fail", got)
	}
	// a run that is stopped leaves the tests that had no result as skipped
	r = newTestResults()
	r.addLine(`{"Action":"run","Package":"p","Test":"TestSlow"}`)
	if c := r.counts(); c.Running != 1 {
		t.Errorf("counts = %+v", c)
	}
	r.finish()
	if c := r.counts(); c.Running != 0 || c.Skip != 1 || r.byPath["p"].status != statusSkip {
		t.Errorf("counts after the end = %+v", c)
	}
}

func TestLinesThatAreNotEventsAreKeptAsTheOutputOfTheRun(t *testing.T) {
	r := newTestResults()
	for _, l := range []string{"go: downloading example.com/x v1.0.0", "", `{"Action":"pass","Package":"p"}`, "{not json", `{"Package":"p"}`} {
		r.addLine(l)
	}
	if got := strings.Join(r.stray, "|"); got != "go: downloading example.com/x v1.0.0|{not json|{\"Package\":\"p\"}" {
		t.Errorf("stray = %q", got)
	}
	if len(r.packages) != 1 || r.packages[0].status != statusPass {
		t.Errorf("packages = %+v", r.packages)
	}
}

func TestOutputThatIsTooLongKeepsTheLastLines(t *testing.T) {
	r := newTestResults()
	for i := 0; i < maxTestOutputLines+50; i++ {
		r.addLine(`{"Action":"output","Package":"p","Test":"TestNoisy","Output":"line ` + strconv.Itoa(i) + `\n"}`)
	}
	out := r.byPath["p"].byName["TestNoisy"].output
	if len(out) != maxTestOutputLines || out[0] != "line 50" || out[len(out)-1] != "line "+strconv.Itoa(maxTestOutputLines+49) {
		t.Errorf("%d lines, first %q last %q", len(out), out[0], out[len(out)-1])
	}
}

func TestAnEventThatIsNotJSONFailsWithItsTag(t *testing.T) {
	for _, line := range []string{"plain text", "{broken", `{"Test":"TestX"}`} {
		_, err := parseTestEvent(line)
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("%q: expected *errors.Error, got %T", line, err)
		}
		if !appErr.HasRoute("parseTestEvent.TestEventInvalid") {
			t.Errorf("%q: unexpected failure path: %s", line, appErr.Route())
		}
	}
	ev, err := parseTestEvent(`{"Action":"pass","Package":"p","Test":"TestX","Elapsed":0.5,"OutputType":"frame"}`)
	if err != nil || ev.Action != "pass" || ev.Test != "TestX" || ev.Elapsed != 0.5 {
		t.Errorf("event = %+v, %v", ev, err)
	}
}
