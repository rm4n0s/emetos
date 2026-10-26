package golang

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang/gotest"
)

// Running the tests from the commands and the lenses (step 14.3) with the fake go: `go test -json` prints the lines of a fixture.

// fixturePath is the path of a file of testdata/testjson.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "testjson", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// testRig activates the extension with the fake go printing the fixture for go test.
func testRig(t *testing.T, fixture string, extra ...string) *rig {
	t.Helper()
	env := append([]string{gotest.EnvTestFile + "=" + fixturePath(t, fixture)}, extra...)
	return newRig(t, rigOptions{withGo: true, env: env})
}

// goTests are the runs of the fake go that were `go test`.
func (r *rig) goTests() []goRun {
	var out []goRun
	for _, run := range r.runs() {
		if len(run.Args) > 0 && run.Args[0] == "test" {
			out = append(out, run)
		}
	}
	return out
}

// testsEnded waits for the newest run of the tests to end and returns it.
func (r *rig) testsEnded() *testJob {
	r.t.Helper()
	r.api.WaitFor("the tests to end", func() bool { return r.ext.tests.job != nil && r.ext.tests.job.state != runRunning })
	return r.ext.tests.job
}

// lensCommandOf finds the lens with the title in the lenses of the source and returns its command id and its first argument as the
// command handler gets it.
func lensCommandOf(t *testing.T, r *rig, uri, src, title, name string) (string, any) {
	t.Helper()
	lenses, err := goLenses{r.ext}.ProvideCodeLenses(context.Background(), extension.TextDocument{URI: uri, LanguageID: "go", Text: src})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lenses {
		var a map[string]any
		if len(l.Command.Arguments) > 0 {
			if err := json.Unmarshal(l.Command.Arguments[0], &a); err != nil {
				t.Fatal(err)
			}
		}
		if l.Command.Title == title && (name == "" || a["name"] == name) {
			if l.Disabled {
				t.Fatalf("the lens %q is disabled", title)
			}
			return l.Command.ID, any(a)
		}
	}
	t.Fatalf("no lens %q (%q)", title, name)
	return "", nil
}

const sampleTests = `package sample

import "testing"

func TestAdd(t *testing.T) {
	t.Run("a", func(t *testing.T) {
		_ = 1
	})
}

func TestSub(t *testing.T) {}

func BenchmarkAdd(b *testing.B) {}

func ExampleAdd() {}

func FuzzAdd(f *testing.F) {}

func helper() {}
`

func TestTheLensesRunTheRightPattern(t *testing.T) {
	tests := []struct {
		title, name string
		want        []string
	}{
		{"run test", "TestAdd", []string{"test", "-json", "-timeout", "30s", "-run", "^TestAdd$"}},
		{"run test", "ExampleAdd", []string{"test", "-json", "-timeout", "30s", "-run", "^ExampleAdd$"}},
		{"run test", "FuzzAdd", []string{"test", "-json", "-timeout", "30s", "-run", "^FuzzAdd$"}},
		{"run benchmark", "BenchmarkAdd", []string{"test", "-json", "-run=^$", "-bench", "^BenchmarkAdd$", "-benchmem"}},
		{"run file tests", "", []string{"test", "-json", "-timeout", "30s", "-run", "^(TestAdd|TestSub|ExampleAdd|FuzzAdd)$"}},
		{"run package tests", "", []string{"test", "-json", "-timeout", "30s"}},
	}
	for _, tt := range tests {
		t.Run(tt.title+" "+tt.name, func(t *testing.T) {
			r := testRig(t, "passing.jsonl")
			sub := filepath.Join(r.folder, "sample")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(sub, "sample_test.go")
			if err := os.WriteFile(path, []byte(sampleTests), 0o600); err != nil {
				t.Fatal(err)
			}
			id, arg := lensCommandOf(t, r, fileURI(path), sampleTests, tt.title, tt.name)
			if err := r.api.Run(id, arg); err != nil {
				t.Fatal(err)
			}
			job := r.testsEnded()
			runs := r.goTests()
			if len(runs) != 1 || !slices.Equal(runs[0].Args, tt.want) || runs[0].Dir != sub {
				t.Errorf("runs = %+v, want %v in %s", runs, tt.want, sub)
			}
			if job.state != runExited || job.results.counts().Pass != 2 {
				t.Errorf("state %d, counts %+v", job.state, job.results.counts())
			}
			if view := r.api.View(TestsViewID); len(view.Reveals) == 0 || !view.Reveals[0] {
				t.Errorf("the panel was revealed %v", view.Reveals)
			}
		})
	}
}

func TestTheCommandsAtTheCursor(t *testing.T) {
	tests := []struct {
		name    string
		command string
		src     string
		file    string
		want    []string // the arguments of go test; none when nothing runs
		note    string
	}{
		{"in a test function", TestCursorCommand, strings.Replace(sampleTests, "_ = 1", "§_ = 1", 1), "sample_test.go", []string{"test", "-json", "-timeout", "30s", "-run", "^TestAdd$"}, ""},
		{"on the func line of a test", TestCursorCommand, strings.Replace(sampleTests, "func TestSub", "func Test§Sub", 1), "sample_test.go", []string{"test", "-json", "-timeout", "30s", "-run", "^TestSub$"}, ""},
		{"in an example", TestCursorCommand, strings.Replace(sampleTests, "func ExampleAdd() {}", "func ExampleAdd() {§}", 1), "sample_test.go", []string{"test", "-json", "-timeout", "30s", "-run", "^ExampleAdd$"}, ""},
		{"outside every function", TestCursorCommand, strings.Replace(sampleTests, "import", "§import", 1), "sample_test.go", nil, "No test function found at the cursor in sample_test.go."},
		{"in a benchmark with the test command", TestCursorCommand, strings.Replace(sampleTests, "func BenchmarkAdd(b *testing.B) {}", "func BenchmarkAdd(b *testing.B) {§}", 1), "sample_test.go", nil, "No test function found at the cursor in sample_test.go."},
		{"a benchmark", BenchmarkCursorCommand, strings.Replace(sampleTests, "func BenchmarkAdd(b *testing.B) {}", "func BenchmarkAdd(b *testing.B) {§}", 1), "sample_test.go", []string{"test", "-json", "-run=^$", "-bench", "^BenchmarkAdd$", "-benchmem"}, ""},
		{"a test with the benchmark command", BenchmarkCursorCommand, strings.Replace(sampleTests, "_ = 1", "§_ = 1", 1), "sample_test.go", nil, "No benchmark function found at the cursor in sample_test.go."},
		{"a file that is not a test file", TestCursorCommand, "package sample\n\nfunc F() {§}\n", "sample.go", nil, "Test Function At Cursor: sample.go is not a test file (its name does not end in _test.go)."},
		{"the whole file", TestFileCommand, "§" + sampleTests, "sample_test.go", []string{"test", "-json", "-timeout", "30s", "-run", "^(TestAdd|TestSub|ExampleAdd|FuzzAdd)$"}, ""},
		{"a file with no tests", TestFileCommand, "§package sample\n\nfunc helper() {}\n", "x_test.go", nil, "No tests found in x_test.go."},
		{"the file command on a source file", TestFileCommand, "§package sample\n", "sample.go", nil, "Test File: sample.go is not a test file (its name does not end in _test.go)."},
		{"the package", TestPackageCommand, "§package sample\n", "sample.go", []string{"test", "-json", "-timeout", "30s"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testRig(t, "passing.jsonl")
			path, _, _ := r.focus(tt.file, tt.src)
			if err := r.api.Run(tt.command, nil); err != nil {
				t.Fatal(err)
			}
			if tt.want == nil {
				if _, msg := r.lastNote(); msg != tt.note || len(r.goTests()) != 0 || r.ext.tests.job != nil {
					t.Errorf("note %q, runs %v", msg, r.goTests())
				}
				return
			}
			r.testsEnded()
			runs := r.goTests()
			if len(runs) != 1 || !slices.Equal(runs[0].Args, tt.want) || runs[0].Dir != filepath.Dir(path) {
				t.Errorf("runs = %+v, want %v", runs, tt.want)
			}
		})
	}
}

func TestTheCommandsWithNoGoFileInFront(t *testing.T) {
	r := testRig(t, "passing.jsonl")
	for _, id := range []string{TestCursorCommand, BenchmarkCursorCommand, TestFileCommand} {
		if err := r.api.Run(id, nil); err != nil {
			t.Fatal(err)
		}
		if _, msg := r.lastNote(); !strings.Contains(msg, "needs a Go file") {
			t.Errorf("%s: note %q", id, msg)
		}
	}
	// the package command falls back to the folder of the workspace
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.testsEnded()
	if runs := r.goTests(); len(runs) != 1 || runs[0].Dir != realPath(t, r.folder) && runs[0].Dir != r.folder {
		t.Errorf("runs = %+v", runs)
	}
	if r.ext.tests.job.req.label != "." {
		t.Errorf("label = %q", r.ext.tests.job.req.label)
	}
}

func TestTestPreviousRunsTheLastRequestAgain(t *testing.T) {
	r := testRig(t, "passing.jsonl")
	if err := r.api.Run(TestPreviousCommand, nil); err != nil {
		t.Fatal(err)
	}
	if _, msg := r.lastNote(); msg != "No tests have been run yet." {
		t.Errorf("note = %q", msg)
	}
	r.focus("sample_test.go", strings.Replace(sampleTests, "_ = 1", "§_ = 1", 1))
	if err := r.api.Run(TestCursorCommand, nil); err != nil {
		t.Fatal(err)
	}
	first := r.testsEnded()
	// the cursor moves elsewhere: Test Previous still runs TestAdd
	r.focus("other_test.go", "package sample\n\n§func TestOther(t *testing.T) {}\n")
	if err := r.api.Run(TestPreviousCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the second run", func() bool { return r.ext.tests.job != first && r.ext.tests.job.state != runRunning })
	runs := r.goTests()
	if len(runs) != 2 || !slices.Equal(runs[0].Args, runs[1].Args) || runs[1].Args[len(runs[1].Args)-1] != "^TestAdd$" {
		t.Errorf("runs = %+v", runs)
	}
	// Run Again in the panel is the same request
	r.ext.startTests(r.ext.tests.job.req)
	r.api.WaitFor("the third run", func() bool { return len(r.goTests()) == 3 && r.ext.tests.job.state != runRunning })
	if r.ext.tests.job == first {
		t.Error("the job was not replaced")
	}
}

func TestTheSettingsOfTheTestRun(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		want     []string
	}{
		{"the defaults", nil, []string{"test", "-json", "-timeout", "30s", "-run", "^TestAdd$"}},
		{"flags and a timeout", map[string]any{TestFlagsSetting: []string{"-race", "-tags=integration"}, TestTimeoutSetting: "2m"}, []string{"test", "-json", "-timeout", "2m", "-race", "-tags=integration", "-run", "^TestAdd$"}},
		{"a timeout in the flags wins", map[string]any{TestFlagsSetting: []string{"-timeout=5s"}}, []string{"test", "-json", "-timeout=5s", "-run", "^TestAdd$"}},
		{"a timeout that is not a duration", map[string]any{TestTimeoutSetting: "soon"}, []string{"test", "-json", "-timeout", "30s", "-run", "^TestAdd$"}},
		{"an empty timeout", map[string]any{TestTimeoutSetting: " "}, []string{"test", "-json", "-timeout", "30s", "-run", "^TestAdd$"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testRig(t, "passing.jsonl")
			for k, v := range tt.settings {
				r.api.SetSetting(k, v)
			}
			r.focus("sample_test.go", strings.Replace(sampleTests, "_ = 1", "§_ = 1", 1))
			if err := r.api.Run(TestCursorCommand, nil); err != nil {
				t.Fatal(err)
			}
			r.testsEnded()
			if runs := r.goTests(); len(runs) != 1 || !slices.Equal(runs[0].Args, tt.want) {
				t.Errorf("runs = %+v, want %v", runs, tt.want)
			}
		})
	}
}

func TestTheEnvironmentOfTheTests(t *testing.T) {
	r := testRig(t, "passing.jsonl", "FAKE_TEST_VAR=", "FAKE_ENVFILE_VAR=", "FAKE_ENVFILE_QUOTED=")
	if err := os.WriteFile(filepath.Join(r.folder, "test.env"), []byte("# comment\nFAKE_TEST_VAR=from-file\nexport FAKE_ENVFILE_VAR = plain\nFAKE_ENVFILE_QUOTED=\"two words\"\nnot a line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.api.SetSetting(TestEnvFileSetting, "test.env")
	r.api.SetSetting(TestEnvVarsSetting, map[string]any{"FAKE_TEST_VAR": "from-settings"})
	r.focus("sample_test.go", strings.Replace(sampleTests, "_ = 1", "§_ = 1", 1))
	if err := r.api.Run(TestCursorCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.testsEnded()
	runs := r.goTests()
	if len(runs) != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	// go.testEnvVars wins over the file; the file gives the rest
	if env := runs[0].Env; env["FAKE_TEST_VAR"] != "from-settings" || env["FAKE_ENVFILE_VAR"] != "plain" || env["FAKE_ENVFILE_QUOTED"] != "two words" {
		t.Errorf("env = %v", env)
	}
}

func TestAnEnvironmentFileOutsideTheWorkspaceIsIgnoredWithAWarning(t *testing.T) {
	r := testRig(t, "passing.jsonl", "FAKE_TEST_VAR=")
	outside := filepath.Join(t.TempDir(), "outside.env")
	if err := os.WriteFile(outside, []byte("FAKE_TEST_VAR=leaked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// a link inside the workspace that points out of it does not count either
	link := filepath.Join(r.folder, "link.env")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{outside, "../outside.env", "link.env", "missing.env"} {
		r.api.SetSetting(TestEnvFileSetting, file)
		r.focus("sample_test.go", strings.Replace(sampleTests, "_ = 1", "§_ = 1", 1))
		before := len(r.goTests())
		if err := r.api.Run(TestCursorCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.testsEnded()
		runs := r.goTests()
		if len(runs) != before+1 {
			t.Fatalf("%s: runs = %d", file, len(runs))
		}
		if v, set := runs[len(runs)-1].Env["FAKE_TEST_VAR"]; set && v == "leaked" {
			t.Errorf("%s: the file outside the workspace was read", file)
		}
		if sev, msg := r.lastNote(); sev != "warn" || !strings.Contains(msg, "go.testEnvFile is ignored") {
			t.Errorf("%s: note %s %q", file, sev, msg)
		}
	}
}

func TestReadEnvFileFailsWithItsTags(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.env"), []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.env")
	if err := os.WriteFile(big, make([]byte, maxEnvFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if vars, err := readEnvFile(dir, "ok.env"); err != nil || !slices.Equal(vars, []string{"A=1"}) {
		t.Errorf("vars = %v, %v", vars, err)
	}
	if vars, err := readEnvFile(dir, filepath.Join(dir, "ok.env")); err != nil || len(vars) != 1 {
		t.Errorf("an absolute path inside: %v, %v", vars, err)
	}
	tests := []struct {
		name, folder, path, route string
	}{
		{"no folder", "", "ok.env", "readEnvFile.TestEnvFileOutsideWorkspace"},
		{"outside", dir, "/etc/hostname", "readEnvFile.TestEnvFileOutsideWorkspace"},
		{"parent", dir, "../x.env", "readEnvFile.TestEnvFileOutsideWorkspace"},
		{"missing", dir, "missing.env", "readEnvFile.TestEnvFileReadFailed"},
		{"too big", dir, "big.env", "readEnvFile.TestEnvFileReadFailed"},
		{"a folder that is gone", filepath.Join(dir, "gone"), "ok.env", "readEnvFile.TestEnvFileReadFailed"},
		{"a directory", dir, ".", "readEnvFile.TestEnvFileReadFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readEnvFile(tt.folder, tt.path)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestParseEnvFile(t *testing.T) {
	got := parseEnvFile("# c\n\nA=1\r\nexport B=two\n C = three \nD=\"x\\ny\"\nE='q r'\nF=\nnovalue\n=bad\nG H=no\n")
	want := []string{"A=1", "B=two", "C=three", "D=x\ny", "E=q r", "F="}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("vars = %q, want %q", got, want)
	}
}

func TestTheLensCommandRefusesBadArguments(t *testing.T) {
	r := testRig(t, "passing.jsonl")
	for name, args := range map[string]any{
		"none": nil, "no name": map[string]any{"uri": "file:///w/a_test.go"}, "not a test file": map[string]any{"uri": "file:///w/a.go", "name": "TestA"},
		"not an identifier": map[string]any{"uri": "file:///w/a_test.go", "name": "TestA$(x)"}, "not a file": map[string]any{"uri": "untitled:1", "name": "TestA"},
	} {
		t.Run(name, func(t *testing.T) {
			err := r.api.Run(TestFunctionCommand, args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute("golang.testFunctionCommand.TestFunctionInvalid") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	if len(r.goTests()) != 0 {
		t.Error("a bad command ran go test")
	}
}

func TestTheTestsNeedAGoProgram(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.focus("sample_test.go", strings.Replace(sampleTests, "_ = 1", "§_ = 1", 1))
	if err := r.api.Run(TestCursorCommand, nil); err != nil {
		t.Fatal(err)
	}
	if sev, msg := r.lastNote(); sev != "error" || !strings.Contains(msg, "The go command was not found") || r.ext.tests.job != nil {
		t.Errorf("note %s %q, job %v", sev, msg, r.ext.tests.job)
	}
}

func TestTheResultsArriveWhileTheTestsRunAndTheGroupEndsWithTheRun(t *testing.T) {
	// the program prints the first results and waits for the gate before the rest
	lines := fixture(t, "mixed.jsonl", "{{DIR}}")
	cut := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, `"Test":"TestSkipped"`) })
	withGate := append(append(slices.Clone(lines[:cut]), "#gate"), lines[cut:]...)
	file := filepath.Join(t.TempDir(), "gated.jsonl")
	if err := os.WriteFile(file, []byte(strings.Join(withGate, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gate := filepath.Join(t.TempDir(), "gate")
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvTestFile + "=" + file, gotest.EnvRunGate + "=" + gate}})
	r.focus("sample_test.go", "§"+sampleTests)
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	job := r.ext.tests.job
	r.api.WaitFor("the first results", func() bool {
		p := job.results.byPath["example.com/mixed"]
		return p != nil && p.byName["TestAddFails"] != nil && p.byName["TestAddFails"].status == statusFail
	})
	if job.state != runRunning || r.ext.tests.model.Len() == 0 {
		t.Errorf("state %d, %d rows while the program waits", job.state, r.ext.tests.model.Len())
	}
	if c := job.results.counts(); c.Pass != 1 || c.Fail != 1 || job.results.byPath["example.com/mixed"].byName["TestSkipped"] != nil {
		t.Errorf("counts = %+v", c)
	}
	if !strings.HasPrefix(job.summary(), "Running the package in ") {
		t.Errorf("summary = %q", job.summary())
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.testsEnded()
	if job.state != runFailed || job.status != "exit status 1" || job.results.counts().Skip != 1 {
		t.Errorf("state %d, status %q, counts %+v", job.state, job.status, job.results.counts())
	}
	// the panel selected the first failure
	if r.ext.tests.selected != "t:example.com/mixed:TestAddFails" {
		t.Errorf("selected = %q", r.ext.tests.selected)
	}
	if out := strings.Join(r.ext.tests.outputOf(), "\n"); !strings.Contains(out, "Add(2, 2) = 4, want 5") {
		t.Errorf("output = %q", out)
	}
}

func TestStopEndsTheProgramAndItsGroupAndANewRunTakesThePlaceOfTheOld(t *testing.T) {
	hang := filepath.Join(t.TempDir(), "hang.jsonl")
	text := `{"Action":"run","Package":"p","Test":"TestSlow"}` + "\n#hang\n"
	if err := os.WriteFile(hang, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	pidfile := filepath.Join(t.TempDir(), "pids")
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvTestFile + "=" + hang, gotest.EnvPidfile + "=" + pidfile}})
	r.focus("sample_test.go", "§"+sampleTests)
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	pid, child := pidsOf(t, pidfile)
	first := r.ext.tests.job
	if err := os.Remove(pidfile); err != nil {
		t.Fatal(err)
	}
	// a new run stops the old one
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	if first.state != runRunning || !first.stopping {
		t.Errorf("the first run: state %d stopping %v", first.state, first.stopping)
	}
	pid2, child2 := pidsOf(t, pidfile)
	requireGone(t, "the first run", pid)
	if !gone(pid) && !gone(child) {
		t.Error("the first run is not gone")
	}
	r.api.WaitFor("the old run to end", func() bool { return first.state == runStopped })
	for _, p := range []int{pid, child} {
		requireGone(t, "the process of the first run", p)
	}
	// Stop ends the second
	second := r.ext.tests.job
	if second == first || second.state != runRunning {
		t.Fatalf("the second run: state %d", second.state)
	}
	if err := r.api.Run(TestCancelCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.testsEnded()
	if second.state != runStopped || second.status != "stopped" {
		t.Errorf("state %d, status %q", second.state, second.status)
	}
	for _, p := range []int{pid2, child2} {
		requireGone(t, "the process of the second run", p)
	}
	// the test that had no result is not shown as running, and a stopped run puts nothing into Problems
	if c := second.results.counts(); c.Running != 0 || c.Skip != 1 || r.api.MarkerCount() != 0 {
		t.Errorf("counts %+v, markers %d", c, r.api.MarkerCount())
	}
	if !strings.HasPrefix(second.summary(), "Stopped the package in ") {
		t.Errorf("summary = %q", second.summary())
	}
}

func TestTheRunEndsWithTheActivation(t *testing.T) {
	hang := filepath.Join(t.TempDir(), "hang.jsonl")
	if err := os.WriteFile(hang, []byte("#hang\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pidfile := filepath.Join(t.TempDir(), "pids")
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvTestFile + "=" + hang, gotest.EnvPidfile + "=" + pidfile}})
	r.focus("sample_test.go", "§"+sampleTests)
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	pid, child := pidsOf(t, pidfile)
	r.cancel()
	r.ext.wait()
	requireGone(t, "go test", pid)
	requireGone(t, "its child", child)
}

func TestFailuresGoIntoProblems(t *testing.T) {
	tests := []struct {
		fixture string
		want    map[string][]string // file -> "line:col message (test)"
	}{
		{"mixed.jsonl", map[string][]string{"mixed_test.go": {"14:1 Add(2, 2) = 4, want 5 [TestAddFails]", "29:1 Add(2, 3) = 5, want 6 [TestTable/wrong]"}}},
		{"panic.jsonl", map[string][]string{"p.go": {"3:1 panic: assignment to entry in nil map [recovered, repanicked] [TestBoom]"}}},
		{"build.jsonl", map[string][]string{"b_test.go": {"6:12 undefined: undefinedName []"}}},
		{"build_legacy.jsonl", map[string][]string{"b_test.go": {"6:12 undefined: undefinedName []"}}},
		{"passing.jsonl", nil},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			r := testRig(t, tt.fixture)
			r.focus("sample_test.go", "§"+sampleTests)
			if err := r.api.Run(TestPackageCommand, nil); err != nil {
				t.Fatal(err)
			}
			r.testsEnded()
			got := map[string][]string{}
			for uri, ms := range r.api.Markers(TestsMarkerOwner) {
				name := filepath.Base(strings.TrimPrefix(uri, "file://"))
				for _, m := range ms {
					if m.Source != "go test" || m.Severity != extension.SeverityError {
						t.Errorf("marker %+v", m)
					}
					got[name] = append(got[name], itoa(m.StartLine)+":"+itoa(m.StartColumn)+" "+m.Message+" ["+m.Code+"]")
				}
			}
			if len(got) == 0 {
				got = nil
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("markers = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTheFailuresOfATableTestAreBothInProblems(t *testing.T) {
	r := testRig(t, "mixed.jsonl")
	r.focus("sample_test.go", "§"+sampleTests)
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.testsEnded()
	var lines []string
	for _, ms := range r.api.Markers(TestsMarkerOwner) {
		for _, m := range ms {
			lines = append(lines, itoa(m.StartLine)+" "+m.Code)
		}
	}
	slices.Sort(lines)
	if !slices.Equal(lines, []string{"14 TestAddFails", "29 TestTable/wrong"}) {
		t.Errorf("markers = %v", lines)
	}
}

func TestTheMarkersOfTheLastRunGoWhenTheNextOneStarts(t *testing.T) {
	r := testRig(t, "mixed.jsonl")
	r.focus("sample_test.go", "§"+sampleTests)
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.testsEnded()
	if r.api.MarkerCount() != 2 {
		t.Fatalf("%d markers", r.api.MarkerCount())
	}
	// the next run starts: the old markers go at once, before the new results
	t.Setenv(gotest.EnvTestFile, fixturePath(t, "passing.jsonl"))
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	if r.api.MarkerCount() != 0 {
		t.Errorf("%d markers when the run starts", r.api.MarkerCount())
	}
	r.api.WaitFor("the second run", func() bool { return r.ext.tests.job.state != runRunning })
	if r.api.MarkerCount() != 0 {
		t.Errorf("%d markers after a run that passed", r.api.MarkerCount())
	}
}
