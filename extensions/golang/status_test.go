package golang

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang/gotest"
)

// The status bar item (step 14.1): the Go version, the state of gopls and the menu.

// item is the status item as the extension last set it.
func (r *rig) item() *itemView {
	r.t.Helper()
	it := r.api.StatusItem(StatusID)
	if it == nil {
		r.t.Fatal("no status item")
	}
	return &itemView{Text: it.Text, Tooltip: it.Tooltip, Visible: !it.Hidden, Click: it.OnClick}
}

type itemView struct {
	Text, Tooltip string
	Visible       bool
	Click         func()
}

// goEditor tells the extension that a Go file is in front.
func (r *rig) goEditor(language string) {
	r.t.Helper()
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: "file:///work/main.go", LanguageID: language})
	r.api.Pump()
}

// withVersion waits for the answer of go env GOVERSION.
func (r *rig) withVersion(label string) {
	r.t.Helper()
	r.api.WaitFor("the version "+label, func() bool {
		return strings.Contains(r.item().Text, label) || strings.Contains(r.item().Tooltip, label)
	})
}

func TestTheStatusItemShowsTheVersionOfGo(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvVersion + "=go1.26.1"}})
	r.withVersion("Go 1.26.1")
	if it := r.item(); it.Text != "Go 1.26.1" || it.Visible {
		t.Errorf("item = %+v, want the version, hidden until a Go file is in front", it)
	}
	// go env GOVERSION ran in the folder of the workspace, with a toolchain that is never downloaded
	runs := r.runs()
	if len(runs) != 1 || !slices.Equal(runs[0].Args, []string{"env", "GOVERSION"}) || runs[0].Dir != realPath(t, r.folder) || runs[0].Env["GOTOOLCHAIN"] != "local" {
		t.Errorf("runs = %+v", runs)
	}
}

// realPath resolves links (the temporary folder may be one).
func realPath(t *testing.T, p string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTheStatusItemFollowsTheStateOfGopls(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvVersion + "=go1.26.1"}})
	r.withVersion("Go 1.26.1")
	r.goEditor("go")
	tests := []struct {
		name    string
		status  extension.ServerStatus
		text    string
		tooltip string
		visible bool
	}{
		{"not started", extension.ServerStatus{State: extension.ServerIdle}, "Go 1.26.1", "gopls has not started", true},
		{"starting", extension.ServerStatus{State: extension.ServerStarting}, "$(sync~spin) Go 1.26.1", "gopls is starting", true},
		{"running", extension.ServerStatus{State: extension.ServerRunning}, "$(check) Go 1.26.1", "gopls is running", true},
		{"running and working", extension.ServerStatus{State: extension.ServerRunning, Progress: "Loading packages 40%"}, "$(sync~spin) Go 1.26.1: Loading packages 40%", "gopls: Loading packages 40%", true},
		{"crashed too often", extension.ServerStatus{State: extension.ServerFailed, Crashes: 5}, "$(error) Go 1.26.1: gopls stopped", "not started again", true},
		{"not found", extension.ServerStatus{State: extension.ServerMissing}, "$(warning) Go 1.26.1: gopls not found", "gopls was not found", true},
		{"the activation ended", extension.ServerStatus{State: extension.ServerStopped}, "Go 1.26.1", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.api.SetServerStatus(ServerID, tt.status)
			it := r.item()
			if it.Text != tt.text || !strings.Contains(it.Tooltip, tt.tooltip) || it.Visible != tt.visible || !strings.HasSuffix(it.Tooltip, "Click for the menu.") || it.Click == nil {
				t.Errorf("item = %+v", it)
			}
		})
	}
}

func TestTheStatusItemShowsForGoFilesOnly(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true})
	r.withVersion("Go 1.99.1")
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerRunning})
	tests := []struct {
		name  string
		event extension.ActiveEditor
		want  bool
	}{
		{"nothing in front yet", extension.ActiveEditor{}, false},
		{"a Go file", extension.ActiveEditor{URI: "file:///a.go", LanguageID: "go"}, true},
		{"a Markdown file", extension.ActiveEditor{URI: "file:///a.md", LanguageID: "markdown"}, false},
		{"a go.mod", extension.ActiveEditor{URI: "file:///go.mod", LanguageID: "go.mod"}, true},
		{"no text editor in front: the last one stays", extension.ActiveEditor{}, true},
		{"a go.work", extension.ActiveEditor{URI: "file:///go.work", LanguageID: "go.work"}, true},
		{"a go.sum", extension.ActiveEditor{URI: "file:///go.sum", LanguageID: "go.sum"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.api.Emit(extension.TopicEditorActiveChanged, tt.event)
			r.api.Pump()
			if got := r.item().Visible; got != tt.want {
				t.Errorf("visible = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTheStatusItemIsToldWhenGoIsNotThere(t *testing.T) {
	tests := []struct {
		name    string
		opts    rigOptions
		tooltip string
	}{
		{"no go program", rigOptions{}, "The go command was not found"},
		{"go env fails", rigOptions{withGo: true, env: []string{gotest.EnvEnvFail + "=go: toolchain not available"}}, "go env GOVERSION failed: go: toolchain not available"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.opts)
			r.goEditor("go")
			r.api.WaitFor("the warning", func() bool { return strings.Contains(r.item().Tooltip, tt.tooltip) })
			if it := r.item(); it.Text != "$(warning) Go" || !it.Visible {
				t.Errorf("item = %+v", it)
			}
		})
	}
}

func TestTheVersionIsAskedAgainWhenTheProgramsChange(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvVersion + "=go1.1.1"}})
	r.withVersion("Go 1.1.1")
	// the environment of the tools decides the answer of the fake; the older, slower question is dropped
	r.api.SetSetting(ToolsEnvVarsSetting, map[string]any{gotest.EnvVersion: "go2.2.2", gotest.EnvDelayMS: "500"})
	r.api.SetSetting(ToolsEnvVarsSetting, map[string]any{gotest.EnvVersion: "go3.3.3"})
	r.withVersion("Go 3.3.3")
	r.ext.wait()
	r.api.Pump()
	if it := r.item(); it.Text != "Go 3.3.3" {
		t.Errorf("the slow answer of an older question replaced the newer one: %+v", it)
	}
	// a setting that changes neither does not ask again
	n := len(r.runs())
	r.api.SetSetting(ToolsManagementGoSetting, "")
	r.api.Pump()
	r.ext.wait()
	if got := len(r.runs()); got != n {
		t.Errorf("%d runs, want %d: nothing the question depends on changed", got, n)
	}
}

func TestGoAlternateToolsChoosesTheGoProgram(t *testing.T) {
	r := newRig(t, rigOptions{env: []string{gotest.EnvVersion + "=go1.7.7"}})
	r.goEditor("go")
	r.api.WaitFor("the warning", func() bool { return strings.Contains(r.item().Tooltip, "not found") })
	// a program of another name, as a wrapper script is: found by its path
	alt := filepath.Join(t.TempDir(), "mygo")
	data, err := os.ReadFile(filepath.Join(fakeGoDir, "go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alt, data, 0o755); err != nil {
		t.Fatal(err)
	}
	r.api.SetSetting(AlternateToolsSetting, map[string]any{"go": alt, "dlv": "ignored"})
	r.withVersion("Go 1.7.7")
	if it := r.item(); it.Text != "Go 1.7.7" {
		t.Errorf("item = %+v", it)
	}
}

func TestTheMenuOfTheStatusItem(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true})
	r.withVersion("Go 1.99.1")
	r.goEditor("go")
	r.item().Click()
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("the click showed no list")
	}
	want := []string{"$(debug-restart) Restart Language Server", "$(output) Show gopls Output", "$(tools) Install/Update Tools", "$(info) Show Go Environment", "$(settings-gear) Open Settings"}
	if !slices.Equal(pick.Labels(), want) || pick.Options().Placeholder != "Go 1.99.1 - gopls" {
		t.Fatalf("menu = %v, %q", pick.Labels(), pick.Options().Placeholder)
	}
	// Restart
	pick.Choose(menuRestart)
	if r.api.ServerRestarts(ServerID) != 1 {
		t.Errorf("restarts = %d", r.api.ServerRestarts(ServerID))
	}
	// Show Output
	r.item().Click()
	r.api.LastPick().Choose(menuOutput)
	if r.api.ServerOutputShown(ServerID) != 1 {
		t.Errorf("output shown = %d", r.api.ServerOutputShown(ServerID))
	}
	// Open Settings is a command of the editor, with the setting to look for
	r.item().Click()
	r.api.LastPick().Choose(menuSettings)
	if cmds := r.api.RanCommands(); len(cmds) != 1 || cmds[0].ID != "workbench.action.openSettings" || cmds[0].Args != GoplsSetting {
		t.Errorf("commands run = %+v", cmds)
	}
	// Show Go Environment
	r.item().Click()
	r.api.LastPick().Choose(menuEnvironment)
	r.api.WaitFor("go env", func() bool {
		ch := r.api.Channel(EnvironmentChannel)
		return ch != nil && slices.Contains(ch.Lines(), "GOOS='linux'")
	})
	// Install/Update Tools
	r.item().Click()
	r.api.LastPick().Choose(menuTools)
	r.api.WaitFor("the tools to be installed", func() bool { return r.ext.tools.run != nil && !r.ext.tools.run.running })
	// a closed menu does nothing
	r.item().Click()
	before := len(r.api.RanCommands())
	r.api.LastPick().Close()
	if r.api.ServerRestarts(ServerID) != 1 || len(r.api.RanCommands()) != before {
		t.Error("closing the menu did something")
	}
}
