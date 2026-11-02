package python

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The Python debugger over the fake API (SPEC §10.31 "The debuggers", Python; step 25.8): the manifest, the configuration providers, the
// adapter descriptor, the process picker and the install of a missing debugpy. No debugpy is run here: the programs are scripts of the
// tests, and the tests of the real debugpy carry the build tag emetos_debugpy (debug_debugpy_test.go).

// cfg is a configuration.
type cfg = extension.DebugConfiguration

// result is the answer of a provider that ran on a goroutine.
type result struct {
	cfg cfg
	err error
}

// resolveAsync runs the providers of the debugger on a goroutine, as the host does (they may wait for the user).
func (r *rig) resolveAsync(typ string, c cfg) <-chan result {
	ch := make(chan result, 1)
	go func() {
		out, err := r.api.ResolveDebugConfiguration(context.Background(), typ, r.folder, c)
		ch <- result{out, err}
	}()
	return ch
}

// wait waits for the answer of a goroutine, pumping the posted work meanwhile.
func (r *rig) wait(ch <-chan result) result {
	r.t.Helper()
	var got result
	done := false
	r.api.WaitFor("the provider to answer", func() bool {
		select {
		case got = <-ch:
			done = true
		default:
		}
		return done
	})
	return got
}

// resolve resolves a configuration of the type debugpy and waits for the answer.
func (r *rig) resolve(c cfg) result { r.t.Helper(); return r.wait(r.resolveAsync(DebugType, c)) }

// substitute plays the second resolution, after the variables are substituted.
func (r *rig) substitute(c cfg) result {
	r.t.Helper()
	out, err := r.api.ResolveDebugConfigurationSubstituted(context.Background(), DebugType, r.folder, c)
	return result{out, err}
}

// adapter asks the factory of the type for the adapter of a session with the configuration.
func (r *rig) adapter(c cfg) (*extension.DebugAdapter, error) {
	r.t.Helper()
	s := r.api.NewDebugSession("s1", DebugType, "py", c)
	s.SetFolder(r.folder)
	return r.api.DebugAdapter(context.Background(), DebugType, s, nil)
}

// noteList lists the notifications with their severity.
func (r *rig) noteList() []string {
	var out []string
	for _, n := range r.api.Notes() {
		out = append(out, n.Severity+": "+n.Message)
	}
	return out
}

// pythonBin is the program the fake machine's PATH gives: the first python3.
func (r *rig) pythonBin() string { return filepath.Join(r.bin, "python3") }

func TestThePythonDebuggersAreDeclared(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatalf("the manifest is refused: %v", err)
	}
	if len(m.Contributes.Debuggers) != 2 {
		t.Fatalf("%d debuggers", len(m.Contributes.Debuggers))
	}
	d, old := m.Contributes.Debuggers[0], m.Contributes.Debuggers[1]
	if d.Type != "debugpy" || d.Label != "Python Debugger" || !reflect.DeepEqual(d.Languages, []string{"python"}) || !d.DynamicConfigurations {
		t.Errorf("debugger = %s %s %v %v", d.Type, d.Label, d.Languages, d.DynamicConfigurations)
	}
	// the old name serves no language, so that F5 with no launch.json has one debugger to choose
	if old.Type != "python" || len(old.Languages) != 0 || old.DynamicConfigurations {
		t.Errorf("old name = %+v", old)
	}
	props := func(request string) map[string]map[string]any {
		var schema struct {
			Properties map[string]map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(d.ConfigurationAttributes[request], &schema); err != nil {
			t.Fatalf("the %s attributes: %v", request, err)
		}
		return schema.Properties
	}
	launch, attach := props("launch"), props("attach")
	tests := []struct {
		request, name string
		have          map[string]map[string]any
		def           any
	}{
		{"launch", "program", launch, "${file}"},
		{"launch", "module", launch, ""},
		{"launch", "console", launch, "integratedTerminal"},
		{"launch", "justMyCode", launch, true},
		{"launch", "stopOnEntry", launch, false},
		{"launch", "cwd", launch, "${workspaceFolder}"},
		{"launch", "envFile", launch, "${workspaceFolder}/.env"},
		{"launch", "python", launch, "${command:python.interpreterPath}"},
		{"launch", "django", launch, false},
		{"attach", "connect", attach, nil},
		{"attach", "listen", attach, nil},
		{"attach", "processId", attach, nil},
		{"attach", "justMyCode", attach, true},
		{"attach", "pathMappings", attach, []any{}},
	}
	for _, tt := range tests {
		p, ok := tt.have[tt.name]
		if !ok {
			t.Errorf("%s: no attribute %s", tt.request, tt.name)
			continue
		}
		if tt.def != nil && !reflect.DeepEqual(p["default"], tt.def) {
			t.Errorf("%s.%s default = %#v, want %#v", tt.request, tt.name, p["default"], tt.def)
		}
	}
	if len(d.ConfigurationSnippets) != 9 {
		t.Fatalf("%d snippets", len(d.ConfigurationSnippets))
	}
	for _, s := range d.ConfigurationSnippets {
		if s.Body["type"] != "debugpy" || s.Body["request"] == nil || s.Body["name"] == nil {
			t.Errorf("snippet %q = %v", s.Label, s.Body)
		}
	}
	if len(m.Contributes.Breakpoints) != 1 || m.Contributes.Breakpoints[0].Language != "python" {
		t.Errorf("breakpoints = %+v", m.Contributes.Breakpoints)
	}
	var keys []string
	for _, s := range m.Contributes.Settings {
		keys = append(keys, s.Key)
	}
	for _, want := range []string{EnvFileSetting, DebugJustMyCodeSetting, DefaultInterpreterSetting} {
		if !slices.Contains(keys, want) {
			t.Errorf("no setting %s", want)
		}
	}
}

func TestBothTypesHaveProvidersAndAFactory(t *testing.T) {
	r := newRig(t, rigOptions{})
	for _, typ := range []string{DebugType, DebugTypeOld} {
		got, err := r.api.ResolveDebugConfiguration(context.Background(), typ, r.folder, cfg{"name": "x", "type": typ, "request": "launch", "program": "a.py"})
		if err != nil || got == nil || got["type"] != typ || got["python"] != r.pythonBin() {
			t.Errorf("%s: resolved to %v, %v", typ, got, err)
		}
	}
	list, err := r.api.ProvideDebugConfigurations(context.Background(), DebugType, r.folder, extension.DebugTriggerDynamic)
	if err != nil || len(list) != 1 || list[0]["name"] != nameFile {
		t.Errorf("dynamic = %v, %v", list, err)
	}
	if list, _ := r.api.ProvideDebugConfigurations(context.Background(), DebugTypeOld, r.folder, extension.DebugTriggerDynamic); len(list) != 0 {
		t.Errorf("the old name has dynamic configurations: %v", list)
	}
}

func TestTheFirstResolutionCompletesAConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		in       cfg
		language string
		want     cfg
		absent   []string
	}{
		{"F5 with a Python file in front", cfg{"type": "debugpy"}, "python",
			cfg{"name": nameFile, "request": "launch", "program": "${file}", "console": "integratedTerminal", "clientOS": "unix"}, nil},
		{"F5 with no configuration at all", cfg{}, "python", cfg{"name": nameFile, "type": "debugpy", "request": "launch"}, nil},
		{"the interpreter in use", cfg{"request": "launch", "program": "a.py"}, "",
			cfg{"type": "debugpy", "python": "@python3", "debugAdapterPython": "@python3", "debugLauncherPython": "@python3"}, []string{"pythonPath"}},
		{"python is kept", cfg{"request": "launch", "program": "a.py", "python": "/opt/py/bin/python"}, "",
			cfg{"python": "/opt/py/bin/python", "debugAdapterPython": "/opt/py/bin/python", "debugLauncherPython": "/opt/py/bin/python"}, nil},
		{"the legacy pythonPath", cfg{"request": "launch", "program": "a.py", "pythonPath": "/opt/legacy/python"}, "",
			cfg{"python": "/opt/legacy/python", "debugAdapterPython": "/opt/legacy/python"}, []string{"pythonPath"}},
		{"the variable of the interpreter", cfg{"request": "launch", "program": "a.py", "python": "${command:python.interpreterPath}"}, "",
			cfg{"python": "@python3"}, nil},
		{"another adapter python", cfg{"request": "launch", "program": "a.py", "python": "/opt/a/python", "debugAdapterPython": "/opt/b/python"}, "",
			cfg{"python": "/opt/a/python", "debugAdapterPython": "/opt/b/python", "debugLauncherPython": "/opt/a/python"}, nil},
		{"the old name of the console", cfg{"request": "launch", "program": "a.py", "consoleName": "My Console"}, "",
			cfg{"consoleTitle": "My Console"}, []string{"consoleName"}},
		{"an attach keeps its words", cfg{"request": "attach", "connect": map[string]any{"port": float64(5678)}}, "",
			cfg{"request": "attach", "clientOS": "unix"}, []string{"python", "debugAdapterPython"}},
		{"a number stays a process id", cfg{"request": "attach", "processId": float64(77)}, "", cfg{"processId": float64(77)}, nil},
		{"a text of digits is left for the second resolution", cfg{"request": "attach", "processId": "77"}, "", cfg{"processId": "77"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			if tt.language != "" {
				r.editor("a.py", tt.language, "x = 1\n")
			}
			got := r.resolve(tt.in)
			if got.err != nil || got.cfg == nil {
				t.Fatalf("resolved to %v, %v", got.cfg, got.err)
			}
			for k, v := range tt.want {
				if v == "@python3" {
					v = r.pythonBin()
				}
				if !reflect.DeepEqual(got.cfg[k], v) {
					t.Errorf("%s = %#v, want %#v (configuration %v)", k, got.cfg[k], v, got.cfg)
				}
			}
			for _, k := range tt.absent {
				if _, has := got.cfg[k]; has {
					t.Errorf("%s is there: %v", k, got.cfg[k])
				}
			}
		})
	}
}

func TestF5WithoutAPythonFileIsCancelledAndSaysSo(t *testing.T) {
	for _, tt := range []struct{ name, language string }{{"no editor", ""}, {"another language", "markdown"}} {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			if tt.language != "" {
				r.editor("a.md", tt.language, "# x\n")
			}
			got := r.resolve(cfg{"type": "debugpy"})
			if got.err != nil || got.cfg != nil {
				t.Errorf("resolved to %v, %v; want the launch cancelled", got.cfg, got.err)
			}
			if notes := r.noteList(); len(notes) != 1 || !strings.HasPrefix(notes[0], "info: Debug needs a Python file") {
				t.Errorf("notes = %v", notes)
			}
		})
	}
}

func TestTheFirstResolutionFailures(t *testing.T) {
	tests := []struct {
		name  string
		opts  rigOptions
		in    cfg
		route string
		note  string
	}{
		{"the deprecated request test", rigOptions{}, cfg{"request": "test", "program": "a.py"}, "resolveDebug.PythonDebugRequestInvalid", ""},
		{"no interpreter", rigOptions{noPython: true}, cfg{"request": "launch", "program": "a.py"}, "interpreterInUse.PythonDebugNoInterpreter", "No Python interpreter was found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.opts)
			got := r.resolve(tt.in)
			appErr, ok := errors.FromError(got.err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Fatalf("err = %v, want the route %s", got.err, tt.route)
			}
			if tt.note != "" {
				if notes := r.noteList(); len(notes) != 1 || !strings.Contains(notes[0], tt.note) {
					t.Errorf("notes = %v", notes)
				}
			}
		})
	}
}

func TestArgsThatAskAreTypedInABox(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		cancel bool
		want   any
	}{
		{"words", "--fast -n 3", false, "--fast -n 3"},
		{"nothing typed", "  ", false, []any{}},
		{"the box is closed", "", true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			ch := r.resolveAsync(DebugType, cfg{"request": "launch", "program": "a.py", "args": "${command:pickArgs}"})
			r.api.WaitFor("the box", func() bool { return r.api.LastInput() != nil })
			if tt.cancel {
				r.api.LastInput().Cancel()
			} else {
				r.api.LastInput().Accept(tt.answer)
			}
			got := r.wait(ch)
			if got.err != nil {
				t.Fatal(got.err)
			}
			if tt.cancel {
				if got.cfg != nil {
					t.Errorf("the launch was not cancelled: %v", got.cfg)
				}
				return
			}
			if !reflect.DeepEqual(got.cfg["args"], tt.want) {
				t.Errorf("args = %#v, want %#v", got.cfg["args"], tt.want)
			}
		})
	}
}

func TestTheSecondResolutionOfALaunch(t *testing.T) {
	tests := []struct {
		name  string
		in    cfg
		files map[string]string
		set   map[string]any
		env   map[string]string
		want  cfg
		// wantEnv are members of env that must be there, and noEnv those that must not
		wantEnv map[string]any
		options []any
	}{
		{"the defaults", cfg{"request": "launch", "program": "/w/a.py"}, nil, nil, nil,
			cfg{"cwd": "@folder", "stopOnEntry": false, "showReturnValue": true, "console": "integratedTerminal", "internalConsoleOptions": "neverOpen",
				"terminalQuoteCharacter": "\"", "justMyCode": true, "workspaceFolder": "@folder"},
			map[string]any{"PYTHONIOENCODING": "UTF-8", "PYTHONUNBUFFERED": "1"}, []any{"ShowReturnValue"}},
		{"the internal console redirects the output", cfg{"request": "launch", "program": "a.py", "console": "internalConsole"}, nil, nil, nil,
			cfg{"redirectOutput": true}, nil, []any{"ShowReturnValue", "RedirectOutput"}},
		{"an external terminal does not open the console", cfg{"request": "launch", "program": "a.py", "console": "externalTerminal"}, nil, nil, nil,
			cfg{"internalConsoleOptions": "neverOpen"}, nil, nil},
		{"the options of the program", cfg{"request": "launch", "module": "flask", "stopOnEntry": true, "django": true, "sudo": true, "subProcess": true, "gevent": true}, nil, nil, nil,
			cfg{"stopOnEntry": true}, map[string]any{"GEVENT_SUPPORT": "True"}, []any{"StopOnEntry", "ShowReturnValue", "Django", "Sudo", "Multiprocess", "Jinja"}},
		{"fastapi gets jinja", cfg{"request": "launch", "module": "FastAPI"}, nil, nil, nil, nil, nil, []any{"ShowReturnValue", "Jinja"}},
		{"jinja false stays off for pyramid", cfg{"request": "launch", "module": "x", "pyramid": true, "jinja": false}, nil, nil, nil, nil, nil, []any{"ShowReturnValue"}},
		{"justMyCode from the setting", cfg{"request": "launch", "program": "a.py"}, nil, map[string]any{DebugJustMyCodeSetting: false}, nil, cfg{"justMyCode": false}, nil, nil},
		{"justMyCode of the configuration wins", cfg{"request": "launch", "program": "a.py", "justMyCode": true}, nil, map[string]any{DebugJustMyCodeSetting: false}, nil, cfg{"justMyCode": true}, nil, nil},
		{"cwd is kept", cfg{"request": "launch", "program": "a.py", "cwd": "/elsewhere"}, nil, nil, nil, cfg{"cwd": "/elsewhere"}, nil, nil},
		{"the .env of the folder", cfg{"request": "launch", "program": "a.py", "env": map[string]any{"B": "own"}},
			map[string]string{".env": "A=1\nB=file\nC=${A}2\n"}, nil, nil, nil, map[string]any{"A": "1", "B": "own", "C": "12"}, nil},
		{"the envFile of the configuration", cfg{"request": "launch", "program": "a.py", "envFile": "@folder/other.env"},
			map[string]string{".env": "A=default\n", "other.env": "A=other\n"}, nil, nil, nil, map[string]any{"A": "other"}, nil},
		{"the envFile setting", cfg{"request": "launch", "program": "a.py"},
			map[string]string{"conf/dev.env": "A=dev\n"}, map[string]any{EnvFileSetting: "${workspaceFolder}/conf/dev.env"}, nil, nil, map[string]any{"A": "dev"}, nil},
		{"a file that is not there", cfg{"request": "launch", "program": "a.py", "envFile": "/no/such/file.env"}, nil, nil, nil, nil, map[string]any{"PYTHONUNBUFFERED": "1"}, nil},
		{"PYTHONPATH is added to, not replaced", cfg{"request": "launch", "program": "a.py", "env": map[string]any{"PYTHONPATH": "/own"}}, nil, nil,
			map[string]string{"PYTHONPATH": "/machine"}, nil, map[string]any{"PYTHONPATH": "/own:/machine"}, nil},
		{"PYTHONUNBUFFERED of the user wins", cfg{"request": "launch", "program": "a.py", "env": map[string]any{"PYTHONUNBUFFERED": "0"}}, nil, nil, nil, nil,
			map[string]any{"PYTHONUNBUFFERED": "0"}, nil},
		{"the legacy interpreter fills what is missing", cfg{"request": "launch", "program": "a.py", "pythonPath": "/p/python"}, nil, nil, nil,
			cfg{"python": "/p/python", "debugAdapterPython": "/p/python"}, nil, nil},
		{"path mappings are made absolute", cfg{"request": "launch", "program": "a.py", "pathMappings": []any{map[string]any{"localRoot": "${workspaceFolder}/src", "remoteRoot": "/app"}}},
			nil, nil, nil, cfg{"pathMappings": []any{map[string]any{"localRoot": "@folder/src", "remoteRoot": "/app"}}}, nil, nil},
		{"no mappings give none", cfg{"request": "launch", "program": "a.py", "pathMappings": []any{}}, nil, nil, nil, nil, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{files: tt.files, settings: tt.set})
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if _, has := tt.env["PYTHONPATH"]; !has {
				t.Setenv("PYTHONPATH", "")
			}
			subst := func(v any) any {
				switch x := v.(type) {
				case string:
					return strings.ReplaceAll(x, "@folder", r.folder)
				case []any:
					out := make([]any, len(x))
					for i, e := range x {
						if m, ok := e.(map[string]any); ok {
							c := map[string]any{}
							for k, val := range m {
								if s, ok := val.(string); ok {
									val = strings.ReplaceAll(s, "@folder", r.folder)
								}
								c[k] = val
							}
							e = c
						}
						out[i] = e
					}
					return out
				}
				return v
			}
			in := cfg{}
			for k, v := range tt.in {
				in[k] = subst(v)
			}
			got := r.substitute(in)
			if got.err != nil || got.cfg == nil {
				t.Fatalf("resolved to %v, %v", got.cfg, got.err)
			}
			for k, v := range tt.want {
				if !reflect.DeepEqual(got.cfg[k], subst(v)) {
					t.Errorf("%s = %#v, want %#v", k, got.cfg[k], subst(v))
				}
			}
			env, _ := got.cfg["env"].(map[string]any)
			for k, v := range tt.wantEnv {
				if env[k] != v {
					t.Errorf("env[%s] = %#v, want %#v (env %v)", k, env[k], v, env)
				}
			}
			if _, has := got.cfg["envFile"]; has {
				t.Error("envFile is still there")
			}
			if tt.options != nil && !reflect.DeepEqual(got.cfg["debugOptions"], tt.options) {
				t.Errorf("debugOptions = %v, want %v", got.cfg["debugOptions"], tt.options)
			}
			if tt.name == "no mappings give none" {
				if _, has := got.cfg["pathMappings"]; has {
					t.Error("pathMappings is there")
				}
			}
		})
	}
}

func TestTheSecondResolutionOfAnAttach(t *testing.T) {
	tests := []struct {
		name   string
		in     cfg
		want   cfg
		absent []string
	}{
		{"a legacy attach is on this machine", cfg{"request": "attach", "port": float64(5678)},
			cfg{"host": "localhost", "justMyCode": true, "showReturnValue": true, "debugOptions": []any{"RedirectOutput", "ShowReturnValue"},
				"pathMappings": []any{map[string]any{"localRoot": "@folder", "remoteRoot": "@folder"}}}, nil},
		{"connect has no host and no mapping", cfg{"request": "attach", "connect": map[string]any{"host": "10.0.0.5", "port": float64(5678)}},
			cfg{"justMyCode": true, "debugOptions": []any{"RedirectOutput", "ShowReturnValue"}}, []string{"host", "pathMappings"}},
		{"the mappings of a remote attach are kept", cfg{"request": "attach", "connect": map[string]any{"port": float64(1)}, "pathMappings": []any{map[string]any{"localRoot": "${workspaceFolder}", "remoteRoot": "."}}},
			cfg{"pathMappings": []any{map[string]any{"localRoot": "${workspaceFolder}", "remoteRoot": "."}}}, nil},
		{"localRoot and remoteRoot of old", cfg{"request": "attach", "host": "127.0.0.1", "port": float64(1), "localRoot": "/l", "remoteRoot": "/r"},
			cfg{"pathMappings": []any{map[string]any{"localRoot": "/l", "remoteRoot": "/r"}}}, nil},
		{"options", cfg{"request": "attach", "connect": map[string]any{"port": float64(1)}, "django": true, "jinja": true, "subProcess": true, "redirectOutput": false, "justMyCode": false},
			cfg{"justMyCode": false, "debugOptions": []any{"Django", "Jinja", "Multiprocess", "ShowReturnValue"}}, nil},
		{"a process id as text becomes a number", cfg{"request": "attach", "processId": "4242"}, cfg{"processId": float64(4242)}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			fix := func(v any) any {
				if s, ok := v.([]any); ok {
					out := make([]any, len(s))
					for i, e := range s {
						if m, ok := e.(map[string]any); ok {
							c := map[string]any{}
							for k, val := range m {
								if str, ok := val.(string); ok {
									val = strings.ReplaceAll(str, "@folder", r.folder)
								}
								c[k] = val
							}
							e = c
						}
						out[i] = e
					}
					return out
				}
				return v
			}
			got := r.substitute(tt.in)
			if got.err != nil {
				t.Fatal(got.err)
			}
			for k, v := range tt.want {
				if !reflect.DeepEqual(got.cfg[k], fix(v)) {
					t.Errorf("%s = %#v, want %#v", k, got.cfg[k], fix(v))
				}
			}
			for _, k := range tt.absent {
				if _, has := got.cfg[k]; has {
					t.Errorf("%s is there: %v", k, got.cfg[k])
				}
			}
		})
	}
}

func TestAnEnvironmentFileThatCannotBeReadFailsTheLaunch(t *testing.T) {
	r := newRig(t, rigOptions{files: map[string]string{"big.env": "A=" + strings.Repeat("x", maxEnvFile+10) + "\n"}})
	got := r.substitute(cfg{"request": "launch", "program": "a.py", "envFile": filepath.Join(r.folder, "big.env")})
	appErr, ok := errors.FromError(got.err)
	if !ok || !appErr.HasRoute("readEnvFile.PythonDebugEnvFileReadFailed") {
		t.Errorf("err = %v", got.err)
	}
	// an unreadable file: the program must be able to read it for the test to mean something
	if os.Getuid() != 0 {
		path := filepath.Join(r.folder, "closed.env")
		if err := os.WriteFile(path, []byte("A=1\n"), 0o000); err != nil {
			t.Fatal(err)
		}
		got = r.substitute(cfg{"request": "launch", "program": "a.py", "envFile": path})
		appErr, ok = errors.FromError(got.err)
		if !ok || !appErr.HasRoute("readEnvFile.PythonDebugEnvFileReadFailed") {
			t.Errorf("unreadable: err = %v", got.err)
		}
	}
}

func TestTheAdapterOfALaunchIsDebugpyInTheInterpreter(t *testing.T) {
	r := newRig(t, rigOptions{})
	py := r.pythonBin()
	tests := []struct {
		name string
		in   cfg
		want extension.DebugAdapter
	}{
		{"the adapter module", cfg{"request": "launch", "python": py},
			extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: py, Args: []string{"-m", "debugpy.adapter"}, Cwd: r.bin}},
		{"debugAdapterPython", cfg{"request": "launch", "python": "/opt/a/python", "debugAdapterPython": py},
			extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: py, Args: []string{"-m", "debugpy.adapter"}, Cwd: r.bin}},
		{"no python in the configuration", cfg{"request": "launch"},
			extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: py, Args: []string{"-m", "debugpy.adapter"}, Cwd: r.bin}},
		{"another adapter", cfg{"request": "launch", "python": py, "debugAdapterPath": "/opt/debugpy/adapter"},
			extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: py, Args: []string{"/opt/debugpy/adapter"}, Cwd: r.bin}},
		{"an attach by process id", cfg{"request": "attach", "python": py, "processId": float64(10)},
			extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: py, Args: []string{"-m", "debugpy.adapter"}, Cwd: r.bin}},
		{"an attach that listens", cfg{"request": "attach", "python": py, "listen": map[string]any{"port": float64(5678)}},
			extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: py, Args: []string{"-m", "debugpy.adapter"}, Cwd: r.bin}},
		{"an attach that connects", cfg{"request": "attach", "connect": map[string]any{"host": "10.0.0.5", "port": float64(5678)}},
			extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: "10.0.0.5", Port: 5678}},
		{"connect with the port as text and no host", cfg{"request": "attach", "connect": map[string]any{"port": "5679"}},
			extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: "127.0.0.1", Port: 5679}},
		{"the legacy host and port", cfg{"request": "attach", "host": "localhost", "port": float64(5680)},
			extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: "localhost", Port: 5680}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.adapter(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*got, tt.want) {
				t.Errorf("adapter = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestTheAdapterLogsToTheCacheWhenAskedTo(t *testing.T) {
	r := newRig(t, rigOptions{})
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	got, err := r.adapter(cfg{"request": "launch", "python": r.pythonBin(), "logToFile": true})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cache, "emetos", "debugpy")
	if want := []string{"-m", "debugpy.adapter", "--log-dir", dir}; !slices.Equal(got.Args, want) {
		t.Errorf("args = %v, want %v", got.Args, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("the log folder: %v", err)
	}
}

func TestTheAdapterFailures(t *testing.T) {
	tests := []struct {
		name  string
		opts  rigOptions
		in    cfg
		route string
	}{
		{"an attach with nothing to attach to", rigOptions{}, cfg{"request": "attach"}, "debugAdapter.PythonDebugAttachInvalid"},
		{"connect without a port", rigOptions{}, cfg{"request": "attach", "connect": map[string]any{"host": "x"}}, "debugAdapter.PythonDebugAttachInvalid"},
		{"no interpreter", rigOptions{noPython: true}, cfg{"request": "launch"}, "interpreterInUse.PythonDebugNoInterpreter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.opts)
			_, err := r.adapter(tt.in)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Errorf("err = %v, want the route %s", err, tt.route)
			}
		})
	}
}

// pythonScript is a python that answers the question whether it can import debugpy.
func pythonScript(t *testing.T, dir, body string) string {
	t.Helper()
	return writeFile(t, dir, "python-debugpy", "#!/bin/sh\n"+body+"\n", 0o755)
}

func TestAMissingDebugpyIsToldWithAnInstallThatRunsInATerminal(t *testing.T) {
	tests := []struct {
		name string
		// venv makes the interpreter that of a virtual environment
		venv bool
		want string
	}{
		{"a global interpreter installs for the user", false, "%s -m pip install --user debugpy"},
		{"an environment installs in it", true, "%s -m pip install debugpy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			var py string
			if tt.venv {
				prefix := filepath.Join(r.base, "env")
				writeFile(t, prefix, "pyvenv.cfg", "version = 3.12.1\n", 0o644)
				py = writeFile(t, prefix, "bin/python", "#!/bin/sh\necho \"ModuleNotFoundError: No module named 'debugpy'\" >&2\nexit 1\n", 0o755)
			} else {
				py = pythonScript(t, r.bin, "echo \"ModuleNotFoundError: No module named 'debugpy'\" >&2\nexit 1")
			}
			var created, ran []map[string]any
			fakeapi.OnRequestValue(r.api, terminalCreate, func(_ context.Context, p map[string]any) (any, error) {
				created = append(created, p)
				return map[string]any{"id": 5}, nil
			})
			fakeapi.OnRequestValue(r.api, terminalRun, func(_ context.Context, p map[string]any) (any, error) {
				ran = append(ran, p)
				return map[string]any{}, nil
			})
			_, err := r.adapter(cfg{"request": "launch", "python": py})
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute("checkDebugpy.PythonDebugpyMissing") {
				t.Fatalf("err = %v", err)
			}
			notes := r.api.Notes()
			if len(notes) != 1 || notes[0].Severity != "warn" || !strings.Contains(notes[0].Message, "debugpy was not found in "+py) || !slices.Equal(notes[0].Actions, []string{"Install"}) {
				t.Fatalf("notes = %+v", notes)
			}
			r.api.RunNoteAction(0, "Install")
			r.api.WaitFor("the command typed", func() bool { return len(ran) == 1 })
			if want := strings.Replace(tt.want, "%s", py, 1); ran[0]["text"] != want || ran[0]["id"] != float64(5) {
				t.Errorf("typed %v, want %q in terminal 5", ran[0], want)
			}
			if created[0]["name"] != "Install debugpy" || created[0]["show"] != true {
				t.Errorf("the terminal was asked for as %v", created[0])
			}
			// each session started without debugpy tells again
			if _, err := r.adapter(cfg{"request": "launch", "python": py}); err == nil || len(r.api.Notes()) != 2 {
				t.Errorf("second start: err = %v, notes = %d", err, len(r.api.Notes()))
			}
		})
	}
}

func TestWithoutATerminalTheInstallCommandIsInTheMessage(t *testing.T) {
	r := newRig(t, rigOptions{})
	py := pythonScript(t, r.bin, "echo \"ModuleNotFoundError: No module named 'debugpy'\" >&2\nexit 1")
	if _, err := r.adapter(cfg{"request": "launch", "python": py}); err == nil {
		t.Fatal("no error")
	}
	r.api.RunNoteAction(0, "Install")
	r.api.WaitFor("the failure", func() bool { return len(r.api.Notes()) == 2 })
	n := r.api.Notes()[1]
	if n.Severity != "error" || !strings.Contains(n.Message, "Cannot open a terminal") || !strings.Contains(n.Message, py+" -m pip install --user debugpy") {
		t.Errorf("note = %+v", n)
	}
}

func TestAnInterpreterThatCannotBeRunFailsTheStart(t *testing.T) {
	r := newRig(t, rigOptions{})
	tests := []struct{ name, py string }{
		{"not a program", filepath.Join(r.base, "nothing")},
		{"fails for another reason", pythonScript(t, r.bin, "echo 'Segmentation fault' >&2\nexit 139")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.adapter(cfg{"request": "launch", "python": tt.py})
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute("checkDebugpy.PythonDebugInterpreterFailed") {
				t.Errorf("err = %v", err)
			}
			if n := r.noteList(); len(n) != 0 {
				t.Errorf("notes = %v", n)
			}
		})
	}
}

func TestADebugpyThatImportsIsStarted(t *testing.T) {
	r := newRig(t, rigOptions{})
	py := pythonScript(t, r.bin, "echo 1.8.22")
	got, err := r.adapter(cfg{"request": "launch", "python": py})
	if err != nil || got.Command != py {
		t.Errorf("adapter = %+v, %v", got, err)
	}
}

func TestAListThatCannotBeShownFailsTheProvider(t *testing.T) {
	tests := []struct {
		name        string
		pick, input bool
		route       string
	}{
		{"the list of configurations", true, false, "pickItem.PythonDebugPickFailed"},
		{"the host of a server", false, true, "askText.PythonDebugPickFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New().(*python)
			t.Setenv("PATH", t.TempDir())
			fake := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Name: "tab", Folder: t.TempDir()})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := e.Activate(ctx, failingAPI{API: fake, pick: tt.pick, input: tt.input}); err != nil {
				t.Fatal(err)
			}
			defer e.interps.wait()
			done := make(chan error, 1)
			go func() {
				_, err := e.provideDebugConfigurations(ctx, "")
				done <- err
			}()
			if !tt.pick {
				fake.WaitFor("the list", func() bool { return fake.LastPick() != nil })
				fake.LastPick().Choose(3) // Remote Attach
			}
			var err error
			fake.WaitFor("the provider to fail", func() bool {
				select {
				case err = <-done:
					return true
				default:
					return false
				}
			})
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Errorf("err = %v, want the route %s", err, tt.route)
			}
		})
	}
}

// failingAPI is the fake API whose quick pick or input box cannot be shown.
type failingAPI struct {
	*fakeapi.API
	pick, input bool
}

type failingPick struct{ extension.QuickPick }

func (failingPick) Show(extension.QuickPickOptions, func(int)) error {
	return errors.New("TestPickUnavailable", "no quick pick")
}

type failingInput struct{ extension.InputBox }

func (failingInput) Show(extension.InputBoxOptions, func(string, bool)) error {
	return errors.New("TestInputUnavailable", "no input box")
}

func (f failingAPI) QuickPick() extension.QuickPick {
	if f.pick {
		return failingPick{f.API.QuickPick()}
	}
	return f.API.QuickPick()
}

func (f failingAPI) InputBox() extension.InputBox {
	if f.input {
		return failingInput{f.API.InputBox()}
	}
	return f.API.InputBox()
}

func TestTheInitialConfigurationsAreChosenFromAList(t *testing.T) {
	tests := []struct {
		name    string
		choice  int
		answers []string
		want    cfg
		nothing bool
	}{
		{"the file", 0, nil, cfg{"name": nameFile, "type": "debugpy", "request": "launch", "program": "${file}", "console": "integratedTerminal"}, false},
		{"the file with arguments", 1, nil, cfg{"name": nameFileWithArgs, "program": "${file}", "args": "${command:pickArgs}"}, false},
		{"a module", 2, []string{"  my.pkg "}, cfg{"name": nameModule, "module": "my.pkg"}, false},
		{"a module is refused empty and the box closed", 2, []string{"\x00cancel"}, nil, true},
		{"a remote attach", 3, []string{"10.1.2.3", "6000"}, cfg{"name": nameAttach, "request": "attach", "connect": map[string]any{"host": "10.1.2.3", "port": float64(6000)}}, false},
		{"a remote attach with the host box closed", 3, []string{"\x00cancel"}, nil, true},
		{"a process", 4, nil, cfg{"name": nameAttachPID, "request": "attach", "processId": "${command:pickProcess}"}, false},
		{"django", 5, nil, cfg{"name": nameDjango, "program": "${workspaceFolder}/manage.py", "args": []any{"runserver"}, "django": true}, false},
		{"fastapi", 6, nil, cfg{"name": nameFastAPI, "module": "uvicorn", "jinja": true}, false},
		{"flask", 7, nil, cfg{"name": nameFlask, "module": "flask", "env": map[string]any{"FLASK_APP": "app.py", "FLASK_DEBUG": "1"}}, false},
		{"pyramid", 8, nil, cfg{"name": namePyramid, "module": "pyramid.scripts.pserve", "pyramid": true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			type listing struct {
				list []cfg
				err  error
			}
			ch := make(chan listing, 1)
			go func() {
				list, err := r.api.ProvideDebugConfigurations(context.Background(), DebugType, r.folder, extension.DebugTriggerInitial)
				ch <- listing{list, err}
			}()
			r.api.WaitFor("the list", func() bool { return r.api.LastPick() != nil })
			if got := len(r.api.LastPick().Options().Items); got != 9 {
				t.Fatalf("%d items", got)
			}
			r.api.LastPick().Choose(tt.choice)
			for i, answer := range tt.answers {
				i := i
				r.api.WaitFor("a box", func() bool { return len(r.api.Inputs()) > i })
				box := r.api.Inputs()[i]
				if answer == "\x00cancel" {
					box.Cancel()
					break
				}
				box.Accept(answer)
			}
			var got listing
			r.api.WaitFor("the answer", func() bool {
				select {
				case got = <-ch:
					return true
				default:
					return false
				}
			})
			if got.err != nil {
				t.Fatal(got.err)
			}
			if tt.nothing {
				if len(got.list) != 0 {
					t.Errorf("list = %v", got.list)
				}
				return
			}
			if len(got.list) != 1 {
				t.Fatalf("list = %v", got.list)
			}
			for k, v := range tt.want {
				if !reflect.DeepEqual(got.list[0][k], v) {
					t.Errorf("%s = %#v, want %#v", k, got.list[0][k], v)
				}
			}
		})
	}
}

func TestAClosedListGivesNoConfiguration(t *testing.T) {
	r := newRig(t, rigOptions{})
	ch := make(chan []cfg, 1)
	go func() {
		list, _ := r.api.ProvideDebugConfigurations(context.Background(), DebugType, r.folder, extension.DebugTriggerInitial)
		ch <- list
	}()
	r.api.WaitFor("the list", func() bool { return r.api.LastPick() != nil })
	r.api.LastPick().Close()
	var list []cfg
	r.api.WaitFor("the answer", func() bool {
		select {
		case list = <-ch:
			return true
		default:
			return false
		}
	})
	if len(list) != 0 {
		t.Errorf("list = %v", list)
	}
}

func TestTheDynamicConfigurationsAreTheFileAndDjango(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"the file", nil, []string{nameFile}},
		{"a Django application", map[string]string{"manage.py": "# django\n"}, []string{nameFile, nameDjango}},
		{"a folder called manage.py", map[string]string{"manage.py/x": ""}, []string{nameFile}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{files: tt.files})
			list, err := r.api.ProvideDebugConfigurations(context.Background(), DebugType, r.folder, extension.DebugTriggerDynamic)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, c := range list {
				names = append(names, c["name"].(string))
				if c["type"] != "debugpy" || c["request"] != "launch" {
					t.Errorf("configuration = %v", c)
				}
			}
			if !slices.Equal(names, tt.want) {
				t.Errorf("names = %v, want %v", names, tt.want)
			}
		})
	}
}

// ---- the process picker ----

// psLine is a row of the listing ps makes with psArgs: the pid, the name in a column of 50 characters, the command line.
func psLine(pid int, name, command string) string {
	return fmt.Sprintf("%5d %-*s %s", pid, secondColumn-1, name, command)
}

// psListing is the output of ps: the header of the table, then the rows.
func psListing(rows ...string) string {
	return "    " + strings.Repeat("a", secondColumn) + "\n" + strings.Join(rows, "\n") + "\n"
}

// fakePS puts a ps that prints the listing (or fails with the status) first in PATH.
func fakePS(t *testing.T, r *rig, listing string, status int) {
	t.Helper()
	writeFile(t, r.bin, "ps.out", listing, 0o644)
	writeFile(t, r.bin, "ps", "#!/bin/sh\nwhile IFS= read -r line; do printf '%s\\n' \"$line\"; done < \"${0%/*}/ps.out\"\nexit "+strconv.Itoa(status)+"\n", 0o755)
}

func TestParsePS(t *testing.T) {
	long := strings.Repeat("x", 60)
	tests := []struct {
		name, in string
		want     []process
	}{
		{"the header is skipped", psListing(psLine(1, "init", "/sbin/init splash"), psLine(22, "bash", "-bash")),
			[]process{{1, "init", "/sbin/init splash"}, {22, "bash", "-bash"}}},
		{"a command line with no arguments", psListing(psLine(7, "kthreadd", "")), []process{{7, "kthreadd", ""}}},
		{"a name of 15 characters and a long command line", psListing(psLine(9, "systemd-journal", "/usr/lib/systemd/systemd-journald "+long)),
			[]process{{9, "systemd-journal", "/usr/lib/systemd/systemd-journald " + long}}},
		{"lines that are not rows", psListing("garbage", "", psLine(3, "ok", "run")), []process{{3, "ok", "run"}}},
		{"nothing", "", nil},
		{"only the header", psListing(), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePS(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("processes = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestThePythonProcessesComeFirst(t *testing.T) {
	list := []process{{1, "nginx", "nginx: master"}, {2, "python3", "python3 /b.py"}, {3, "Bash", "-bash"}, {4, "python3.14", "python3.14 /A.py"}, {5, "apache", "httpd"}}
	sortProcesses(list)
	var got []int
	for _, p := range list {
		got = append(got, p.pid)
	}
	if want := []int{2, 4, 5, 3, 1}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestTheProcessPickerAnswersTheProcessId(t *testing.T) {
	listing := psListing(
		psLine(30, "nginx", "nginx: worker"),
		psLine(10, "python3", "python3 /srv/app.py --serve"),
		psLine(20, "bash", "-bash"),
	)
	tests := []struct {
		name      string
		processID any
		shown     bool
		choose    int
		want      any
	}{
		{"the variable pickProcess", "${command:pickProcess}", true, 0, float64(10)},
		{"the second row", "${command:pickProcess}", true, 1, float64(20)},
		{"a number is left", float64(77), false, 0, float64(77)},
		{"a number as text is left for the substitution", "77", false, 0, "77"},
		{"another variable is left for the substitution", "${input:pid}", false, 0, "${input:pid}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			fakePS(t, r, listing, 0)
			ch := r.resolveAsync(DebugType, cfg{"name": "A", "request": "attach", "processId": tt.processID})
			if tt.shown {
				r.api.WaitFor("the picker", func() bool { return r.api.LastPick() != nil })
				items := r.api.LastPick().Options().Items
				if len(items) != 3 || items[0].Label != "python3" || items[0].Description != "10  python3 /srv/app.py --serve" {
					t.Errorf("items = %+v", items)
				}
				if !r.api.LastPick().Options().MatchOnDescription {
					t.Error("the command line is not searched")
				}
				r.api.LastPick().Choose(tt.choose)
			}
			got := r.wait(ch)
			if got.err != nil {
				t.Fatal(got.err)
			}
			if got.cfg["processId"] != tt.want {
				t.Errorf("processId = %#v, want %#v", got.cfg["processId"], tt.want)
			}
			if !tt.shown && len(r.api.Picks()) != 0 {
				t.Error("a picker was shown")
			}
		})
	}
}

func TestTheProcessPickerFailsWithoutAChoiceOrAList(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, r *rig)
		// closePick closes the picker with no choice
		closePick bool
		route     string
	}{
		{"the list is closed", func(t *testing.T, r *rig) { fakePS(t, r, psListing(psLine(10, "a", "b")), 0) }, true, "chooseProcess.PythonDebugNoProcessSelected"},
		{"there is no process", func(t *testing.T, r *rig) { fakePS(t, r, psListing(), 0) }, false, "chooseProcess.PythonDebugNoProcessSelected"},
		{"ps is not there", func(t *testing.T, r *rig) {}, false, "listProcesses.PythonDebugProcessListFailed"},
		{"ps fails", func(t *testing.T, r *rig) { fakePS(t, r, "", 3) }, false, "listProcesses.PythonDebugProcessListFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			tt.setup(t, r)
			ch := r.resolveAsync(DebugType, cfg{"request": "attach", "processId": "${command:pickProcess}"})
			if tt.closePick {
				r.api.WaitFor("the picker", func() bool { return r.api.LastPick() != nil })
				r.api.LastPick().Close()
			}
			got := r.wait(ch)
			appErr, ok := errors.FromError(got.err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Errorf("err = %v, want the route %s", got.err, tt.route)
			}
		})
	}
}

// ---- the environment file ----

func TestParseEnvFile(t *testing.T) {
	tests := []struct {
		name string
		in   string
		base map[string]string
		want map[string]string
	}{
		{"plain lines", "A=1\nB=two words\n", nil, map[string]string{"A": "1", "B": "two words"}},
		{"comments and blank lines are not variables", "# a comment\n\nA=1\n   # another\n", nil, map[string]string{"A": "1"}},
		{"quotes are removed", "A=\"x y\"\nB='z'\n", nil, map[string]string{"A": "x y", "B": "z"}},
		{"\\n in quotes is a line feed", "A=\"one\\ntwo\"\n", nil, map[string]string{"A": "one\ntwo"}},
		{"a quoted value spans lines", "A=\"one\ntwo\"\nB=3\n", nil, map[string]string{"A": "one\ntwo", "B": "3"}},
		{"an escaped quote does not end the value", "A=\"say \\\"hi\\\"\"\n", nil, map[string]string{"A": "say \\\"hi\\\""}},
		{"no line feed at the end", "A=1\nB=2", nil, map[string]string{"A": "1", "B": "2"}},
		{"spaces around the equals sign", "  A  =  1  \n", nil, map[string]string{"A": "1"}},
		{"an empty value", "A=\nB=''\n", nil, map[string]string{"A": "", "B": ""}},
		{"a name that is not a name", "1A=x\nA B=y\n-C=z\n_D=ok\n", nil, map[string]string{"_D": "ok"}},
		{"an earlier variable", "A=1\nB=${A}2\n", nil, map[string]string{"A": "1", "B": "12"}},
		{"a variable of the base", "B=${HOME_X}/bin\n", map[string]string{"HOME_X": "/h"}, map[string]string{"B": "/h/bin"}},
		{"the file wins over the base", "A=file\nB=${A}\n", map[string]string{"A": "base"}, map[string]string{"A": "file", "B": "file"}},
		{"an unknown variable is empty", "B=x${NOPE}y\n", nil, map[string]string{"B": "xy"}},
		{"an escaped reference is kept", "B=\\${A}\nC=\\$5\n", nil, map[string]string{"B": "${A}", "C": "$5"}},
		{"a bogus reference leaves the value alone", "A=1\nB=${A}${A:-x}\n", nil, map[string]string{"A": "1", "B": "${A}${A:-x}"}},
		{"windows line ends", "A=1\r\nB=2\r\n", nil, map[string]string{"A": "1", "B": "2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseEnvFile(tt.in, tt.base); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("variables = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDebugEnvironment(t *testing.T) {
	machine := map[string]string{"PATH": "/usr/bin", "PYTHONPATH": "/machine"}
	get := func(k string) string { return machine[k] }
	tests := []struct {
		name   string
		env    map[string]string
		file   map[string]string
		gevent bool
		want   map[string]string
	}{
		{"nothing", nil, nil, false, map[string]string{"PYTHONIOENCODING": "UTF-8", "PYTHONUNBUFFERED": "1"}},
		{"env wins over the file", map[string]string{"A": "env"}, map[string]string{"A": "file", "B": "file"}, false,
			map[string]string{"A": "env", "B": "file", "PYTHONIOENCODING": "UTF-8", "PYTHONUNBUFFERED": "1"}},
		{"PATH is added after the file's and the machine's", map[string]string{"PATH": "/own"}, map[string]string{"PATH": "/file"}, false,
			map[string]string{"PATH": "/file:/own:/usr/bin", "PYTHONIOENCODING": "UTF-8", "PYTHONUNBUFFERED": "1"}},
		{"PYTHONPATH of the file only", nil, map[string]string{"PYTHONPATH": "/file"}, false,
			map[string]string{"PYTHONPATH": "/file:/machine", "PYTHONIOENCODING": "UTF-8", "PYTHONUNBUFFERED": "1"}},
		{"the user's encoding is kept", map[string]string{"PYTHONIOENCODING": "latin-1"}, nil, true,
			map[string]string{"PYTHONIOENCODING": "latin-1", "PYTHONUNBUFFERED": "1", "GEVENT_SUPPORT": "True"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := debugEnvironment(tt.env, tt.file, tt.gevent, get); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("env = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPipInstallArgs(t *testing.T) {
	tests := []struct {
		name string
		py   func(t *testing.T) string
		want string
	}{
		{"global", func(t *testing.T) string { return filepath.Join(t.TempDir(), "bin", "python3") }, "-m pip install --user debugpy"},
		{"a virtual environment", func(t *testing.T) string {
			prefix := t.TempDir()
			writeFile(t, prefix, "pyvenv.cfg", "version = 3.12\n", 0o644)
			return filepath.Join(prefix, "bin", "python")
		}, "-m pip install debugpy"},
		{"a conda environment", func(t *testing.T) string {
			prefix := t.TempDir()
			if err := os.MkdirAll(filepath.Join(prefix, "conda-meta"), 0o755); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(prefix, "bin", "python")
		}, "-m pip install debugpy"},
		{"a path with a space is quoted", func(t *testing.T) string { return "/opt/my python/bin/python" }, "-m pip install --user debugpy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			py := tt.py(t)
			got := shellJoin(pipInstallArgs(py))
			if want := shellQuote(py) + " " + tt.want; got != want {
				t.Errorf("line = %q, want %q", got, want)
			}
		})
	}
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Errorf("quote = %s", got)
	}
}
