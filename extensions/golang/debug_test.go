package golang

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The Go debugger over the fake API (SPEC §10.31 "The debuggers", Go; step 25.7): the manifest, the configuration provider, the adapter
// descriptor, the process picker and the debug test commands. dlv is never run here: the tests of the real dlv carry the build tag
// emetos_dlv (debug_dlv_test.go).

// cfg is a configuration.
type cfg = extension.DebugConfiguration

// result is the answer of a provider that ran on a goroutine.
type result struct {
	cfg cfg
	err error
}

// fakeDlv makes a program that stands in for dlv (it is never run) and returns its path.
func fakeDlv(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dlv")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// debugRig is a rig with dlv set in go.alternateTools and a Go file in front.
func debugRig(t *testing.T, opts rigOptions) (*rig, string) {
	t.Helper()
	dlv := fakeDlv(t)
	if opts.settings == nil {
		opts.settings = map[string]any{}
	}
	if _, set := opts.settings[AlternateToolsSetting]; !set {
		opts.settings[AlternateToolsSetting] = map[string]any{"dlv": dlv}
	}
	r := newRig(t, opts)
	r.openGo("main.go", "package main\n\nfunc main() {}\n")
	return r, dlv
}

// resolveAsync runs the providers of the debugger on a goroutine, as the host does (they may wait for the user), and returns the channel
// of the answer. The test pumps the fake's UI thread while it waits (wait).
func (r *rig) resolveAsync(folder string, c cfg) <-chan result {
	ch := make(chan result, 1)
	go func() {
		out, err := r.api.ResolveDebugConfiguration(context.Background(), DebugType, folder, c)
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

// resolve resolves a configuration and waits for the answer.
func (r *rig) resolve(c cfg) result { r.t.Helper(); return r.wait(r.resolveAsync(r.folder, c)) }

// substitute plays the second resolution, after the variables are substituted.
func (r *rig) substitute(c cfg) result {
	r.t.Helper()
	out, err := r.api.ResolveDebugConfigurationSubstituted(context.Background(), DebugType, r.folder, c)
	return result{out, err}
}

// notes lists the messages of the notifications with their severity.
func (r *rig) notes() []string {
	var out []string
	for _, n := range r.api.Notes() {
		out = append(out, n.Severity+": "+n.Message)
	}
	return out
}

func TestTheGoDebuggerIsDeclared(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatalf("the manifest is refused: %v", err)
	}
	if len(m.Contributes.Debuggers) != 1 {
		t.Fatalf("%d debuggers", len(m.Contributes.Debuggers))
	}
	d := m.Contributes.Debuggers[0]
	if d.Type != "go" || d.Label != "Go" || !reflect.DeepEqual(d.Languages, []string{"go"}) {
		t.Errorf("debugger = %s %s %v", d.Type, d.Label, d.Languages)
	}
	// the attributes are VS Code Go's: a few members of each request, with their defaults
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
		{"launch", "program", launch, "${workspaceFolder}"},
		{"launch", "mode", launch, "auto"},
		{"launch", "stopOnEntry", launch, false},
		{"launch", "envFile", launch, nil},
		{"launch", "buildFlags", launch, nil},
		{"launch", "hideSystemGoroutines", launch, false},
		{"launch", "showGlobalVariables", launch, false},
		{"launch", "substitutePath", launch, nil},
		{"attach", "processId", attach, nil},
		{"attach", "mode", attach, "local"},
		{"attach", "port", attach, nil},
		{"attach", "host", attach, nil},
		{"attach", "substitutePath", attach, nil},
	}
	for _, tt := range tests {
		t.Run(tt.request+"."+tt.name, func(t *testing.T) {
			p, ok := tt.have[tt.name]
			if !ok {
				t.Fatalf("the %s request has no attribute %s", tt.request, tt.name)
			}
			if tt.def != nil && p["default"] != tt.def {
				t.Errorf("default = %v, want %v", p["default"], tt.def)
			}
		})
	}
	var labels []string
	for _, s := range d.ConfigurationSnippets {
		labels = append(labels, s.Label)
	}
	wantLabels := []string{"Go: Launch package", "Go: Launch file", "Go: Launch test function", "Go: Attach to local process", "Go: Connect to server"}
	if !reflect.DeepEqual(labels, wantLabels) {
		t.Errorf("snippets = %v", labels)
	}
	if len(m.Contributes.Breakpoints) != 1 || m.Contributes.Breakpoints[0].Language != "go" {
		t.Errorf("breakpoints = %+v", m.Contributes.Breakpoints)
	}
	titles := map[string]string{}
	for _, c := range m.Contributes.Commands {
		titles[c.ID] = c.Title
	}
	if titles[DebugTestAtCursorCommand] != "Debug Test At Cursor" {
		t.Errorf("titles = %v", titles)
	}
	if title, declared := titles[DebugTestFunctionCommand]; !declared || title != "" {
		t.Errorf("the lens command is declared with no title: %q, %v", title, declared)
	}
}

func TestTheDebuggerRegistersItsProviderAndFactory(t *testing.T) {
	r, _ := debugRig(t, rigOptions{})
	if got := r.api.DebugFactoryTypes(); !reflect.DeepEqual(got, []string{"go"}) {
		t.Errorf("factories = %v", got)
	}
}

func TestWithoutADebugServiceTheExtensionHasNoDebuggerAndGoesOn(t *testing.T) {
	e := New().(*golang)
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: t.TempDir()})
	api.SetDebugUnavailable(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); e.wait() }()
	if err := e.Activate(ctx, api); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if got := api.DebugFactoryTypes(); len(got) != 0 {
		t.Errorf("factories = %v", got)
	}
}

func TestTheProviderResolvesAConfiguration(t *testing.T) {
	const mainSrc = "package main\n\nfunc main() {}\n"
	tests := []struct {
		name string
		// editor is the file in front ("" none, a name in the folder otherwise) and language its language
		editor, language string
		settings         map[string]any
		in               cfg
		want             cfg
		notes            []string
	}{
		{"F5 with no launch.json", "main.go", "go", nil, cfg{"type": "go"},
			cfg{"name": "Launch", "type": "go", "request": "launch", "mode": "debug", "program": "@folder", "debugAdapter": "dlv-dap", "dlvToolPath": "@dlv",
				"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": false, "logOutput": "debugger", "dlvFlags": []any{}, "hideSystemGoroutines": false}, nil},
		{"F5 with a test file in front", "x_test.go", "go", nil, cfg{"type": "go"},
			cfg{"name": "Launch", "type": "go", "request": "launch", "mode": "test", "program": "@folder", "debugAdapter": "dlv-dap", "dlvToolPath": "@dlv",
				"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": false, "logOutput": "debugger", "dlvFlags": []any{}, "hideSystemGoroutines": false}, nil},
		{"a launch with no mode, the file in front is a test", "x_test.go", "go", nil, cfg{"name": "L", "request": "launch", "program": "${fileDirname}"},
			cfg{"name": "L", "type": "go", "request": "launch", "mode": "test", "program": "${fileDirname}", "debugAdapter": "dlv-dap", "dlvToolPath": "@dlv",
				"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": false, "logOutput": "debugger", "dlvFlags": []any{}, "hideSystemGoroutines": false}, nil},
		{"the program is a file: it decides the mode", "x_test.go", "go", nil, cfg{"name": "L", "request": "launch", "mode": "auto", "program": "/w/main.go"},
			cfg{"name": "L", "type": "go", "request": "launch", "mode": "debug", "program": "/w/main.go", "debugAdapter": "dlv-dap", "dlvToolPath": "@dlv",
				"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": false, "logOutput": "debugger", "dlvFlags": []any{}, "hideSystemGoroutines": false}, nil},
		{"a test file as the program of a test is its package", "main.go", "go", nil, cfg{"name": "L", "request": "launch", "mode": "test", "program": "/w/pkg/a_test.go"},
			cfg{"name": "L", "type": "go", "request": "launch", "mode": "test", "program": "/w/pkg", "debugAdapter": "dlv-dap", "dlvToolPath": "@dlv",
				"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": false, "logOutput": "debugger", "dlvFlags": []any{}, "hideSystemGoroutines": false}, nil},
		{"the defaults of go.delveConfig, and what the configuration sets wins", "main.go", "go",
			map[string]any{DelveConfigSetting: map[string]any{"showLog": true, "logOutput": "dap", "hideSystemGoroutines": true, "dlvFlags": []any{"--check-go-version=false"}}},
			cfg{"name": "L", "request": "launch", "mode": "debug", "program": "/w", "hideSystemGoroutines": false},
			cfg{"name": "L", "type": "go", "request": "launch", "mode": "debug", "program": "/w", "debugAdapter": "dlv-dap", "dlvToolPath": "@dlv",
				"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": true, "logOutput": "dap", "dlvFlags": []any{"--check-go-version=false"}, "hideSystemGoroutines": false}, nil},
		{"an attach has the mode local", "main.go", "go", nil, cfg{"name": "A", "type": "go", "request": "attach", "processId": float64(42)},
			cfg{"name": "A", "type": "go", "request": "attach", "mode": "local", "processId": float64(42), "debugAdapter": "dlv-dap", "dlvToolPath": "@dlv",
				"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": false, "logOutput": "debugger", "dlvFlags": []any{}, "hideSystemGoroutines": false}, nil},
		{"-gcflags are removed from the build flags and the GOFLAGS", "main.go", "go", nil,
			cfg{"name": "L", "request": "launch", "mode": "debug", "program": "/w", "buildFlags": "-tags=a -gcflags=all=-N -race", "env": map[string]any{"GOFLAGS": "-mod=mod -gcflags 'all=-l'", "X": "1"}},
			cfg{"name": "L", "type": "go", "request": "launch", "mode": "debug", "program": "/w", "buildFlags": "-tags=a -race", "env": map[string]any{"GOFLAGS": "-mod=mod", "X": "1"},
				"debugAdapter": "dlv-dap", "dlvToolPath": "@dlv",
				"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": false, "logOutput": "debugger", "dlvFlags": []any{}, "hideSystemGoroutines": false},
			[]string{"warn: The build flag '-gcflags' in 'buildFlags' is ignored: dlv sets the flags that make the program debuggable.",
				"warn: The build flag '-gcflags' in 'GOFLAGS' is ignored: dlv sets the flags that make the program debuggable."}},
		{"a list of build flags", "main.go", "go", nil,
			cfg{"name": "L", "request": "launch", "mode": "debug", "program": "/w", "buildFlags": []any{"-tags", "a", "-gcflags", "all=-N", "-gcflags=-l"}},
			cfg{"name": "L", "type": "go", "request": "launch", "mode": "debug", "program": "/w", "buildFlags": []any{"-tags", "a"}, "debugAdapter": "dlv-dap", "dlvToolPath": "@dlv",
				"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": false, "logOutput": "debugger", "dlvFlags": []any{}, "hideSystemGoroutines": false},
			[]string{"warn: The build flag '-gcflags' in 'buildFlags' is ignored: dlv sets the flags that make the program debuggable."}},
		{"the legacy adapter is read as dlv-dap", "main.go", "go", nil,
			cfg{"name": "L", "request": "launch", "mode": "debug", "program": "/w", "debugAdapter": "legacy"},
			cfg{"name": "L", "type": "go", "request": "launch", "mode": "debug", "program": "/w", "debugAdapter": "dlv-dap", "dlvToolPath": "@dlv",
				"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": false, "logOutput": "debugger", "dlvFlags": []any{}, "hideSystemGoroutines": false},
			[]string{"warn: The debug adapter \"legacy\" is not available; dlv dap is used."}},
		{"a launch in remote mode is deprecated", "main.go", "go", nil,
			cfg{"name": "L", "request": "launch", "mode": "remote", "port": float64(2345)},
			cfg{"name": "L", "type": "go", "request": "launch", "mode": "remote", "port": float64(2345), "debugAdapter": "dlv-dap", "dlvToolPath": "@dlv",
				"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": false, "logOutput": "debugger", "dlvFlags": []any{}, "hideSystemGoroutines": false},
			[]string{"warn: The request type 'launch' with mode 'remote' is deprecated: use the request type 'attach' with mode 'remote'."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, dlv := debugRig(t, rigOptions{settings: tt.settings})
			if tt.editor != "x_test.go" {
				r.openGo(tt.editor, mainSrc)
			} else {
				r.openGo("x_test.go", "package main\n")
			}
			got := r.resolve(tt.in)
			if got.err != nil {
				t.Fatal(got.err)
			}
			want := cfg{}
			for k, v := range tt.want {
				switch v {
				case "@folder":
					v = r.folder
				case "@dlv":
					v = dlv
				}
				want[k] = v
			}
			if !reflect.DeepEqual(got.cfg, want) {
				t.Errorf("resolved =\n%v\nwant\n%v", got.cfg, want)
			}
			if notes := r.notes(); !reflect.DeepEqual(notes, tt.notes) {
				t.Errorf("notifications = %q, want %q", notes, tt.notes)
			}
		})
	}
}

func TestTheProviderDoesNotChangeTheConfigurationItIsGiven(t *testing.T) {
	r, _ := debugRig(t, rigOptions{})
	in := cfg{"name": "L", "request": "launch", "mode": "debug", "program": "/w", "buildFlags": "-gcflags=-N", "env": map[string]any{"GOFLAGS": "-gcflags=-N"}}
	before, _ := json.Marshal(in)
	if got := r.resolve(in); got.err != nil || got.cfg == nil {
		t.Fatalf("resolve = %+v", got)
	}
	after, _ := json.Marshal(in)
	if string(before) != string(after) {
		t.Errorf("the configuration changed:\n%s\n%s", before, after)
	}
}

func TestACwdWithATildeIsExpanded(t *testing.T) {
	r, _ := debugRig(t, rigOptions{})
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := r.resolve(cfg{"name": "L", "request": "launch", "mode": "debug", "program": "/w", "cwd": "~/work"})
	if got.err != nil || got.cfg["cwd"] != filepath.Join(home, "work") {
		t.Errorf("cwd = %v, %v", got.cfg["cwd"], got.err)
	}
}

func TestNoEditorOfGoMeansNothingToLaunch(t *testing.T) {
	tests := []struct {
		name, editorURI, language string
	}{
		{"no editor yet", "", ""},
		{"a file that is not Go", "file:///w/notes.md", "markdown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{settings: map[string]any{AlternateToolsSetting: map[string]any{"dlv": fakeDlv(t)}}})
			if tt.editorURI != "" {
				r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: tt.editorURI, LanguageID: tt.language})
				r.api.Pump()
			}
			got := r.resolve(cfg{"type": "go"})
			if got.err != nil || got.cfg != nil {
				t.Errorf("resolve = %+v, want a cancelled launch", got)
			}
			if sev, msg := r.lastNote(); sev != "info" || !strings.Contains(msg, "Debug needs a Go file") {
				t.Errorf("note = %s %q", sev, msg)
			}
		})
	}
}

func TestAMissingDlvCancelsTheLaunchAndOffersToInstallIt(t *testing.T) {
	// no go.alternateTools, an empty PATH and no GOPATH: dlv is nowhere
	r := newRig(t, rigOptions{withGo: true, settings: map[string]any{ToolsEnvVarsSetting: map[string]any{"GOPATH": t.TempDir()}}})
	r.openGo("main.go", "package main\n")
	t.Setenv("GOBIN", "")
	for i := 0; i < 2; i++ {
		got := r.resolve(cfg{"type": "go"})
		if got.err != nil || got.cfg != nil {
			t.Fatalf("resolve = %+v, want a cancelled launch", got)
		}
	}
	notes := r.api.Notes()
	if len(notes) != 2 {
		t.Fatalf("%d notifications; it tells every time a session is started without dlv", len(notes))
	}
	if n := notes[0]; n.Severity != "warn" || !strings.HasPrefix(n.Message, "dlv was not found. It debugs Go programs.") || !reflect.DeepEqual(n.Actions, []string{"Install"}) {
		t.Errorf("note = %+v", n)
	}
	r.api.RunNoteAction(0, "Install")
	run := r.finished()
	if st := states(run); st["dlv"] != toolInstalled || len(st) != 1 {
		t.Errorf("states = %v", st)
	}
	installs := r.installs()
	if len(installs) != 1 || !reflect.DeepEqual(installs[0].Args, []string{"install", "github.com/go-delve/delve/cmd/dlv@latest"}) {
		t.Errorf("installs = %+v", installs)
	}
}

func TestDlvIsFoundWhereTheToolsAreInstalled(t *testing.T) {
	exe := func(dir string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "dlv"), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name string
		// setup makes the settings and the folders; it returns the settings and the folder dlv is in
		setup func(t *testing.T) (settings map[string]any, bin string)
	}{
		{"go.toolsGopath", func(t *testing.T) (map[string]any, string) {
			gp := t.TempDir()
			exe(filepath.Join(gp, "bin"))
			return map[string]any{ToolsGopathSetting: gp}, filepath.Join(gp, "bin")
		}},
		{"GOBIN of go.toolsEnvVars", func(t *testing.T) (map[string]any, string) {
			bin := filepath.Join(t.TempDir(), "gobin")
			exe(bin)
			return map[string]any{ToolsEnvVarsSetting: map[string]any{"GOBIN": bin}}, bin
		}},
		{"a GOPATH with two folders", func(t *testing.T) (map[string]any, string) {
			a, b := t.TempDir(), t.TempDir()
			exe(filepath.Join(b, "bin"))
			return map[string]any{ToolsEnvVarsSetting: map[string]any{"GOPATH": a + string(os.PathListSeparator) + b}}, filepath.Join(b, "bin")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings, bin := tt.setup(t)
			r := newRig(t, rigOptions{settings: settings})
			got, err := r.ext.dlvProgram()
			if err != nil || got != filepath.Join(bin, "dlv") {
				t.Errorf("dlv = %q, %v; want %q", got, err, filepath.Join(bin, "dlv"))
			}
		})
	}
}

func TestAnAlternateDlvThatCannotBeRunIsAnError(t *testing.T) {
	r := newRig(t, rigOptions{settings: map[string]any{AlternateToolsSetting: map[string]any{"dlv": "/no/such/dlv"}}})
	_, err := r.ext.dlvProgram()
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("dlvProgram->findDlv.GoDlvNotFound") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(appErr.Message, "go.alternateTools names a dlv that cannot be run") {
		t.Errorf("message = %q", appErr.Message)
	}
}

func TestTheInitialConfigurationsAreChosenInAList(t *testing.T) {
	tests := []struct {
		name   string
		choose int
		host   string
		port   string
		want   cfg
	}{
		{"launch the package", 0, "", "", cfg{"name": "Launch Package", "type": "go", "request": "launch", "mode": "auto", "program": "${fileDirname}"}},
		{"attach to a process", 1, "", "", cfg{"name": "Attach to Process", "type": "go", "request": "attach", "mode": "local", "processId": float64(0)}},
		{"connect to a server", 2, "10.0.0.7", "4040", cfg{"name": "Connect to server", "type": "go", "request": "attach", "mode": "remote",
			"remotePath": "${workspaceFolder}", "port": float64(4040), "host": "10.0.0.7"}},
		{"connect to a server with the defaults", 2, "", "", cfg{"name": "Connect to server", "type": "go", "request": "attach", "mode": "remote",
			"remotePath": "${workspaceFolder}", "port": float64(2345), "host": "127.0.0.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := debugRig(t, rigOptions{})
			ch := make(chan struct {
				list []cfg
				err  error
			}, 1)
			go func() {
				list, err := r.api.ProvideDebugConfigurations(context.Background(), DebugType, r.folder, extension.DebugTriggerInitial)
				ch <- struct {
					list []cfg
					err  error
				}{list, err}
			}()
			r.api.WaitFor("the list of configurations", func() bool { return r.api.LastPick() != nil })
			pick := r.api.LastPick()
			if got := pick.Labels(); !reflect.DeepEqual(got, []string{"Go: Launch Package", "Go: Attach to local process", "Go: Connect to server"}) {
				t.Errorf("labels = %v", got)
			}
			pick.Choose(tt.choose)
			answered := 0
			if tt.choose == 2 {
				for _, answer := range []string{tt.host, tt.port} {
					r.api.WaitFor("an input box", func() bool { return len(r.api.Inputs()) > answered })
					in := r.api.Inputs()[answered]
					answered++
					if answer == "" {
						answer = in.Value()
					}
					in.Accept(answer)
				}
			}
			var got struct {
				list []cfg
				err  error
			}
			r.api.WaitFor("the answer", func() bool {
				select {
				case got = <-ch:
					return true
				default:
					return false
				}
			})
			if got.err != nil || len(got.list) != 1 || !reflect.DeepEqual(got.list[0], tt.want) {
				t.Errorf("configurations = %v, %v; want %v", got.list, got.err, tt.want)
			}
		})
	}
}

// openGo writes a Go file into the folder and tells the extension that it is in front, with the cursor at its start.
func (r *rig) openGo(name, text string) string {
	r.t.Helper()
	path := filepath.Join(r.folder, name)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		r.t.Fatal(err)
	}
	uri := fileURI(path)
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: "go"})
	r.api.Emit(extension.TopicEditorSelectionChanged, extension.EditorSelection{URI: uri, Selections: []extension.Selection{{}}})
	r.api.Pump()
	return path
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

func TestAListThatCannotBeShownFailsTheProvider(t *testing.T) {
	tests := []struct {
		name        string
		pick, input bool
		route       string
	}{
		{"the list of configurations", true, false, "pickItem.GoDebugPickFailed"},
		{"the host of a server", false, true, "askText.GoDebugPickFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New().(*golang)
			fake := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: t.TempDir()})
			ctx, cancel := context.WithCancel(context.Background())
			defer func() { cancel(); e.wait() }()
			if err := e.Activate(ctx, failingAPI{API: fake, pick: tt.pick, input: tt.input}); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, err := e.provideDebugConfigurations(ctx, "")
				done <- err
			}()
			if !tt.pick {
				fake.WaitFor("the list", func() bool { return fake.LastPick() != nil })
				fake.LastPick().Choose(2)
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

// The pickers of the configuration provider are shown on the screen that started the session (decision 1500): the quick pick of the
// initial configurations and the input boxes after it carry the origin of the context the host gives the provider.
func TestTheProvidersPickersCarryTheOriginOfTheStart(t *testing.T) {
	const view extension.Origin = 4
	tests := []struct {
		name string
		ctx  func(context.Context) context.Context
		want extension.Origin
	}{
		{"a client's start", func(ctx context.Context) context.Context { return extension.WithOrigin(ctx, view) }, view},
		{"the host's start", func(ctx context.Context) context.Context { return ctx }, extension.HostOrigin},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New().(*golang)
			fake := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: t.TempDir()})
			ctx, cancel := context.WithCancel(context.Background())
			defer func() { cancel(); e.wait() }()
			if err := e.Activate(ctx, fake); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, err := e.provideDebugConfigurations(tt.ctx(ctx), "")
				done <- err
			}()
			fake.WaitFor("the list", func() bool { return fake.LastPick() != nil })
			if got := fake.LastPick().Options().Origin; got != tt.want {
				t.Errorf("the list of configurations is shown for the origin %d, want %d", got, tt.want)
			}
			fake.LastPick().Choose(2) // Connect to server: asks for the host and the port
			for i := 1; i <= 2; i++ {
				fake.WaitFor("the input box", func() bool { return len(fake.Inputs()) == i })
				if got := fake.LastInput().Options().Origin; got != tt.want {
					t.Errorf("input box %d is shown for the origin %d, want %d", i, got, tt.want)
				}
				fake.LastInput().Accept([]string{"", "2345"}[i-1])
			}
			fake.WaitFor("the provider to finish", func() bool {
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
					return true
				default:
					return false
				}
			})
		})
	}
}
