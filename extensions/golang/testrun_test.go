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
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
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

// ctl is the controller of the Go tests as the fake host has it.
func (r *rig) ctl() *fakeapi.FakeTestController {
	r.t.Helper()
	c := r.api.TestController(TestControllerID)
	if c == nil {
		r.t.Fatal("the extension made no test controller")
	}
	return c
}

// testsEnded waits for the next run of the controller that has not been returned yet to end and returns it.
func (r *rig) testsEnded() *fakeapi.FakeTestRun {
	r.t.Helper()
	var run *fakeapi.FakeTestRun
	r.api.WaitFor("the tests to end", func() bool {
		runs := r.ctl().Runs()
		if len(runs) <= r.consumed {
			return false
		}
		run = runs[r.consumed]
		return run.Ended()
	})
	r.consumed++
	return run
}

// discovered waits until the worker has read the test files of the folder.
func (r *rig) discovered() {
	r.t.Helper()
	r.api.WaitFor("the discovery of the tests", func() bool { return r.ext.tests.idle() })
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
			run := r.testsEnded()
			runs := r.goTests()
			if len(runs) != 1 || !slices.Equal(runs[0].Args, tt.want) || runs[0].Dir != sub {
				t.Errorf("runs = %+v, want %v in %s", runs, tt.want, sub)
			}
			if run.Name() != "go test" || !run.Persist() {
				t.Errorf("run %q persist %v", run.Name(), run.Persist())
			}
			if run.Request().Profile == nil || run.Request().Profile.Kind() != extension.TestKindRun {
				t.Errorf("the run was not made for the Run profile: %+v", run.Request())
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testRig(t, "passing.jsonl")
			path, _, _ := r.focus(tt.file, tt.src)
			if err := r.api.Run(tt.command, nil); err != nil {
				t.Fatal(err)
			}
			if tt.want == nil {
				if _, msg := r.lastNote(); msg != tt.note || len(r.goTests()) != 0 || len(r.ctl().Runs()) != 0 {
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
	// the package command falls back to the folder of the workspace, which has no tests here
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	if _, msg := r.lastNote(); msg != "No tests found in the workspace folder." || len(r.ctl().Runs()) != 0 {
		t.Errorf("note %q, runs %d", msg, len(r.ctl().Runs()))
	}
	// and with a test file in it, runs its package
	if err := os.WriteFile(filepath.Join(r.folder, "root_test.go"), []byte(sampleTests), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.testsEnded()
	if runs := r.goTests(); len(runs) != 1 || runs[0].Dir != realPath(t, r.folder) && runs[0].Dir != r.folder || !slices.Equal(runs[0].Args, []string{"test", "-json", "-timeout", "30s"}) {
		t.Errorf("runs = %+v", runs)
	}
}

func TestTestPreviousRunsTheLastCommandAgain(t *testing.T) {
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
	r.testsEnded()
	// the cursor moves elsewhere: Test Previous still runs TestAdd
	r.focus("other_test.go", "package sample\n\n§func TestOther(t *testing.T) {}\n")
	if err := r.api.Run(TestPreviousCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.testsEnded()
	runs := r.goTests()
	if len(runs) != 2 || !slices.Equal(runs[0].Args, runs[1].Args) || runs[1].Args[len(runs[1].Args)-1] != "^TestAdd$" {
		t.Errorf("runs = %+v", runs)
	}
	// the file is gone: the tests of the last run are too
	if err := os.Remove(filepath.Join(r.folder, "sample_test.go")); err != nil {
		t.Fatal(err)
	}
	r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileDeleted, Path: "sample_test.go"}}})
	r.discovered()
	if err := r.api.Run(TestPreviousCommand, nil); err != nil {
		t.Fatal(err)
	}
	if _, msg := r.lastNote(); msg != "The tests of the last run are gone." || len(r.goTests()) != 2 {
		t.Errorf("note %q, runs %d", msg, len(r.goTests()))
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
	r.api.WaitFor("the note", func() bool { sev, _ := r.lastNote(); return sev == "error" })
	if _, msg := r.lastNote(); !strings.Contains(msg, "The go command was not found") || len(r.ctl().Runs()) != 0 {
		t.Errorf("note %q, runs %d", msg, len(r.ctl().Runs()))
	}
}
