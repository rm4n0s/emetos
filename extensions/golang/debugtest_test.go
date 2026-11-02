package golang

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The debug test commands and lenses over the fake API (step 25.7).

// debugStarts waits for n calls of StartDebugging (the command starts the session on a goroutine) and returns them.
func (r *rig) debugStarts(n int) []fakeapi.DebugStart {
	r.t.Helper()
	r.api.WaitFor("the debug sessions to start", func() bool { return len(r.api.DebugStarts()) >= n })
	r.ext.wait()
	return r.api.DebugStarts()
}

func lensArguments(r *rig, name string) map[string]any {
	return map[string]any{"uri": fileURI(filepath.Join(r.folder, "sample_test.go")), "name": name}
}

func TestTheDebugLensStartsASessionOfModeTest(t *testing.T) {
	tests := []struct {
		name     string
		function string
		settings map[string]any
		want     func(r *rig) cfg
	}{
		{"a test", "TestAdd", nil, func(r *rig) cfg {
			return cfg{"name": "Debug Test", "type": "go", "request": "launch", "mode": "test", "program": r.folder, "env": map[string]any{},
				"args": []any{"-test.run", "^TestAdd$"}, "buildFlags": ""}
		}},
		{"an example", "ExampleAdd", nil, func(r *rig) cfg {
			return cfg{"name": "Debug Test", "type": "go", "request": "launch", "mode": "test", "program": r.folder, "env": map[string]any{},
				"args": []any{"-test.run", "^ExampleAdd$"}, "buildFlags": ""}
		}},
		{"a benchmark runs no test", "BenchmarkAdd", nil, func(r *rig) cfg {
			return cfg{"name": "Debug Test", "type": "go", "request": "launch", "mode": "test", "program": r.folder, "env": map[string]any{},
				"args": []any{"-test.bench", "^BenchmarkAdd$", "-test.run", "a^"}, "buildFlags": ""}
		}},
		{"go.testFlags before -args build, after it run", "TestSub", map[string]any{TestFlagsSetting: []any{"-race", "-tags=integration", "-args", "-v", "x"}}, func(r *rig) cfg {
			return cfg{"name": "Debug Test", "type": "go", "request": "launch", "mode": "test", "program": r.folder, "env": map[string]any{},
				"args": []any{"-test.run", "^TestSub$", "-v", "x"}, "buildFlags": "-race -tags=integration"}
		}},
		{"go.testEnvVars and go.testEnvFile", "TestAdd", map[string]any{TestEnvVarsSetting: map[string]any{"A": "1"}, TestEnvFileSetting: "test.env"}, func(r *rig) cfg {
			return cfg{"name": "Debug Test", "type": "go", "request": "launch", "mode": "test", "program": r.folder, "env": map[string]any{"A": "1"},
				"args": []any{"-test.run", "^TestAdd$"}, "buildFlags": "", "envFile": filepath.Join(r.folder, "test.env")}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := debugRig(t, rigOptions{settings: tt.settings})
			if err := r.api.Run(DebugTestFunctionCommand, lensArguments(r, tt.function)); err != nil {
				t.Fatal(err)
			}
			starts := r.debugStarts(1)
			if len(starts) != 1 {
				t.Fatalf("%d sessions started", len(starts))
			}
			if s := starts[0]; s.Folder != r.folder || s.Target.Name != "" || !reflect.DeepEqual(s.Target.Config, tt.want(r)) || s.Options != (extension.DebugStartOptions{}) {
				t.Errorf("start = %+v\nwant config %v", s, tt.want(r))
			}
		})
	}
}

func TestDebugTestAtCursorStartsTheFunctionAtTheCursor(t *testing.T) {
	tests := []struct {
		name   string
		source string
		file   string
		args   []any
		note   string
	}{
		{"in a test", strings.Replace(sampleTests, "_ = 1", "§_ = 1", 1), "sample_test.go", []any{"-test.run", "^TestAdd$"}, ""},
		{"on the func line of another", strings.Replace(sampleTests, "func TestSub", "func Test§Sub", 1), "sample_test.go", []any{"-test.run", "^TestSub$"}, ""},
		{"in a benchmark", strings.Replace(sampleTests, "func BenchmarkAdd(b *testing.B) {}", "func BenchmarkAdd(b *testing.B) {§}", 1), "sample_test.go",
			[]any{"-test.bench", "^BenchmarkAdd$", "-test.run", "a^"}, ""},
		{"in an example", strings.Replace(sampleTests, "func ExampleAdd() {}", "func ExampleAdd() {§}", 1), "sample_test.go", []any{"-test.run", "^ExampleAdd$"}, ""},
		{"in a fuzz target", strings.Replace(sampleTests, "func FuzzAdd(f *testing.F) {}", "func FuzzAdd(f *testing.F) {§}", 1), "sample_test.go", []any{"-test.run", "^FuzzAdd$"}, ""},
		{"outside every function", strings.Replace(sampleTests, "import", "§import", 1), "sample_test.go", nil, "No test function found at the cursor in sample_test.go."},
		{"in a helper", strings.Replace(sampleTests, "func helper() {}", "func helper() {§}", 1), "sample_test.go", nil, "No test function found at the cursor in sample_test.go."},
		{"in a file that is not a test file", "package sample\n\nfunc F() {§}\n", "sample.go", nil, "Debug Test At Cursor: sample.go is not a test file (its name does not end in _test.go)."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := debugRig(t, rigOptions{})
			r.focus(tt.file, tt.source)
			if err := r.api.Run(DebugTestAtCursorCommand, nil); err != nil {
				t.Fatal(err)
			}
			if tt.args == nil {
				r.ext.wait()
				if n := len(r.api.DebugStarts()); n != 0 {
					t.Fatalf("%d sessions started", n)
				}
				if sev, msg := r.lastNote(); sev != "info" || msg != tt.note {
					t.Errorf("note = %s %q, want %q", sev, msg, tt.note)
				}
				return
			}
			starts := r.debugStarts(1)
			got := starts[0].Target.Config
			if got["mode"] != "test" || got["program"] != r.folder || !reflect.DeepEqual(got["args"], tt.args) {
				t.Errorf("config = %v", got)
			}
		})
	}
}

func TestTheDebugTestCommandsRefuseWhatIsNoTest(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
	}{
		{"not a test file", map[string]any{"uri": "file:///w/main.go", "name": "TestAdd"}},
		{"not a file", map[string]any{"uri": "untitled:1", "name": "TestAdd"}},
		{"no function", map[string]any{"uri": "file:///w/a_test.go"}},
		{"a name that is not an identifier", map[string]any{"uri": "file:///w/a_test.go", "name": "Test Add"}},
		{"a pattern", map[string]any{"uri": "file:///w/a_test.go", "name": "Test.*"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := debugRig(t, rigOptions{})
			err := r.api.Run(DebugTestFunctionCommand, tt.args)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute("debugTestFunctionCommand.DebugTestFunctionInvalid") {
				t.Errorf("err = %v", err)
			}
			if n := len(r.api.DebugStarts()); n != 0 {
				t.Errorf("%d sessions started", n)
			}
		})
	}
}

func TestADebugTestThatCannotStartIsReported(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(r *rig)
		note    string
		noError bool
	}{
		{"a host with no debug service", func(r *rig) { r.api.SetDebugUnavailable(true) }, "error: Debug TestAdd: the scope has no debug service", false},
		{"a start that the providers cancelled", func(r *rig) {
			r.api.OnDebugStart(func(fakeapi.DebugStart) (bool, error) { return false, nil })
		}, "", true},
		{"a start that failed", func(r *rig) {
			r.api.OnDebugStart(func(fakeapi.DebugStart) (bool, error) {
				return false, errors.New("TestStartFailed", "the adapter did not start")
			})
		}, "error: Debug TestAdd: the adapter did not start", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := debugRig(t, rigOptions{})
			tt.setup(r)
			if err := r.api.Run(DebugTestFunctionCommand, lensArguments(r, "TestAdd")); err != nil {
				t.Fatal(err)
			}
			r.ext.wait()
			r.api.Pump()
			var got string
			for _, n := range r.notes() {
				got = n
			}
			if got != tt.note {
				t.Errorf("notification = %q, want %q", got, tt.note)
			}
		})
	}
}

func TestAClientDebugsATestAsTheHostDoes(t *testing.T) {
	// the command of a client's view runs in the one scope of the workspace, with the origin of the client: the session is the
	// host's, and what goes wrong is told on the screen of the client
	const client extension.Origin = 3
	r, _ := debugRig(t, rigOptions{})
	var err error
	r.api.RunAs(client, func() { err = r.api.Run(DebugTestFunctionCommand, lensArguments(r, "TestAdd")) })
	if err != nil {
		t.Fatal(err)
	}
	starts := r.debugStarts(1)
	if got := starts[0].Target.Config["args"]; !reflect.DeepEqual(got, []any{"-test.run", "^TestAdd$"}) {
		t.Errorf("args = %v", got)
	}
	r.api.SetDebugUnavailable(true)
	r.api.RunAs(client, func() { err = r.api.Run(DebugTestFunctionCommand, lensArguments(r, "TestAdd")) })
	if err != nil {
		t.Fatal(err)
	}
	r.ext.wait()
	r.api.Pump()
	if sev, msg := r.lastNote(); sev != "error" || !strings.Contains(msg, "no debug service") {
		t.Errorf("note = %s %q", sev, msg)
	}
}

func TestTheDebugTestStartsOnAGoroutineOfItsOwn(t *testing.T) {
	r, _ := debugRig(t, rigOptions{})
	release := make(chan struct{})
	r.api.OnDebugStart(func(fakeapi.DebugStart) (bool, error) { <-release; return true, nil })
	if err := r.api.Run(DebugTestFunctionCommand, lensArguments(r, "TestAdd")); err != nil {
		t.Fatal(err)
	}
	// the command returned at once: the start is on a goroutine of its own
	r.api.WaitFor("the start", func() bool { return len(r.api.DebugStarts()) == 1 })
	close(release)
	r.ext.wait()
}
