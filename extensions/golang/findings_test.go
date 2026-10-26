package golang

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The places that the tools name (step 14.3).

func TestParseFindingsReadsThePlacesOfVetAndTheLintTools(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		withCode bool
		want     []finding
	}{
		{"go vet", []string{"# example.com/p", "./a.go:7:2: fmt.Printf format %d has arg \"x\" of wrong type string", "a.go:9: unreachable code"}, false, []finding{
			{Path: "/w/p/a.go", Line: 7, Col: 2, Message: `fmt.Printf format %d has arg "x" of wrong type string`},
			{Path: "/w/p/a.go", Line: 9, Message: "unreachable code"},
		}},
		{"go vet that cannot type-check", []string{"vet: ./a.go:3:5: undefined: foo"}, false, []finding{{Path: "/w/p/a.go", Line: 3, Col: 5, Message: "undefined: foo", Error: true}}},
		{"staticcheck", []string{"a.go:12:2: this value of err is never used (SA4006)", "sub/b.go:3:1: package comment should be of the form \"Package x ...\" (ST1000)"}, true, []finding{
			{Path: "/w/p/a.go", Line: 12, Col: 2, Message: "this value of err is never used", Code: "SA4006"},
			{Path: "/w/p/sub/b.go", Line: 3, Col: 1, Message: `package comment should be of the form "Package x ..."`, Code: "ST1000"},
		}},
		{"golangci-lint", []string{"a.go:5:9: Error return value of `os.Remove` is not checked (errcheck)", "level=warning msg=\"x\""}, true, []finding{
			{Path: "/w/p/a.go", Line: 5, Col: 9, Message: "Error return value of `os.Remove` is not checked", Code: "errcheck"},
		}},
		{"revive", []string{"/w/p/a.go:4:6: [exported] exported function Foo should have comment or be unexported"}, true, []finding{
			{Path: "/w/p/a.go", Line: 4, Col: 6, Message: "exported function Foo should have comment or be unexported", Code: "exported"},
		}},
		{"a severity word", []string{"a.go:1:1: error: boom", "a.go:2:1: warning: careful"}, false, []finding{
			{Path: "/w/p/a.go", Line: 1, Col: 1, Message: "boom", Error: true}, {Path: "/w/p/a.go", Line: 2, Col: 1, Message: "careful"},
		}},
		{"lines with no place", []string{"", "exit status 1", "FAIL example.com/p [build failed]", "a.go:0:1: line zero", "a.go:x:1: no number"}, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFindings(tt.lines, "/w/p", tt.withCode, true)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findings =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestFailureFindingsReadTheOutputOfFailedTests(t *testing.T) {
	const dir = "/w/p"
	tests := []struct {
		name  string
		lines []string
		want  []finding
	}{
		{"a failed assertion", []string{"    mixed_test.go:14: Add(2, 2) = 4, want 5", "--- FAIL: TestAddFails (0.00s)"}, []finding{
			{Path: "/w/p/mixed_test.go", Line: 14, Message: "Add(2, 2) = 4, want 5", Code: "TestAddFails", Error: true}}},
		{"a message that goes on", []string{"    x_test.go:8: values differ:", "        got:  2", "        want: 3", "    x_test.go:9: second"}, []finding{
			{Path: "/w/p/x_test.go", Line: 8, Message: "values differ: got:  2 want: 3", Code: "TestAddFails", Error: true},
			{Path: "/w/p/x_test.go", Line: 9, Message: "second", Code: "TestAddFails", Error: true}}},
		{"a build error", []string{"# example.com/p [example.com/p.test]", "./b_test.go:6:12: undefined: undefinedName"}, []finding{
			{Path: "/w/p/b_test.go", Line: 6, Col: 12, Message: "undefined: undefinedName", Code: "TestAddFails", Error: true}}},
		{"a panic: the first frame in the workspace", []string{
			"panic: assignment to entry in nil map [recovered, repanicked]", "goroutine 19 [running]:", "testing.tRunner.func1.2({0x6b8270, 0x6f0100})",
			"\t/usr/local/go/src/testing/testing.go:2125 +0x232", "example.com/panics.Boom(...)", "\t/w/p/p.go:3", "example.com/panics.TestBoom(0x2f6debd5a488?)", "\t/w/p/p_test.go:9 +0x29"}, []finding{
			{Path: "/w/p/p.go", Line: 3, Message: "panic: assignment to entry in nil map [recovered, repanicked]", Code: "TestAddFails", Error: true}}},
		{"a file outside the workspace is dropped", []string{"    ../other/x_test.go:3: elsewhere"}, nil},
		{"no place", []string{"=== RUN", "--- FAIL: TestX (0.00s)", "FAIL"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := failureFindings(tt.lines, dir, "/w/p", "TestAddFails")
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findings =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestFailureFindingsOfTheFixtures(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		{"panic.jsonl", "/w/p/p.go:3: panic: assignment to entry in nil map [recovered, repanicked]"},
		{"build.jsonl", "/w/p/b_test.go:6:12: undefined: undefinedName"},
		{"build_legacy.jsonl", "/w/p/b_test.go:6:12: undefined: undefinedName"},
		{"mixed.jsonl", "/w/p/mixed_test.go:14: Add(2, 2) = 4, want 5\n/w/p/mixed_test.go:29: Add(2, 3) = 5, want 6"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			r := newTestResults()
			for _, l := range fixture(t, tt.file, "/w/p") {
				r.addLine(l)
			}
			var got []string
			add := func(fs []finding) {
				for _, f := range fs {
					text := f.Path + ":" + itoa(f.Line)
					if f.Col > 0 {
						text += ":" + itoa(f.Col)
					}
					got = append(got, text+": "+f.Message)
				}
			}
			for _, p := range r.packages {
				add(failureFindings(p.output, "/w/p", "/w/p", ""))
				for _, name := range []string{"TestAddFails", "TestTable/wrong", "TestBoom"} {
					if n := p.byName[name]; n != nil {
						add(failureFindings(n.output, "/w/p", "/w/p", name))
					}
				}
			}
			if strings.Join(got, "\n") != tt.want {
				t.Errorf("findings =\n%s\nwant\n%s", strings.Join(got, "\n"), tt.want)
			}
		})
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestWithinAndAbsolute(t *testing.T) {
	for _, tt := range []struct {
		root, path string
		want       bool
	}{{"/w", "/w/a.go", true}, {"/w", "/w", true}, {"/w", "/x/a.go", false}, {"/w", "/w/../x/a.go", false}, {"", "/anything", true}, {"/w", "/wx/a.go", false}} {
		if got := within(tt.root, tt.path); got != tt.want {
			t.Errorf("within(%q, %q) = %v", tt.root, tt.path, got)
		}
	}
	if got := absolute("/w/p", "./sub/../a.go"); got != "/w/p/a.go" {
		t.Errorf("absolute = %q", got)
	}
	if got := absolute("/w/p", "/q/a.go"); got != "/q/a.go" {
		t.Errorf("absolute = %q", got)
	}
}
