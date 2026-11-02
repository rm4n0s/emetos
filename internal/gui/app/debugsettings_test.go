package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// The settings of Run and Debug that step 25.11 finished (SPEC §10.31 "Settings"): each against a session of the fake adapter. The tasks
// and debug.onTaskErrors are in debugtasks_test.go.

// startOne starts the configuration of the folder by its name with the options and waits until the session has stopped (the script of the
// fixture stops it at a breakpoint).
func (r *debugRig) startOne(name string, opts debug.SessionOptions) {
	r.t.Helper()
	r.breakpoint() // the script of the fixture answers a setBreakpoints
	launch := r.svc().Launches().All()[0].Launch
	opts.StartedByUser = true
	done := make(chan startResult, 1)
	go func() {
		ok, err := r.svc().StartDebugging(context.Background(), launch, debug.Target{Name: name}, opts)
		done <- startResult{ok, err}
	}()
	s := r.started(done)
	if s.err != nil || !s.ok {
		r.t.Fatalf("start %s: %v %v", name, s.ok, s.err)
	}
	r.waitFor("the stop", func() bool { return r.win.Keys.Get("debugState") == "stopped" })
}

// stopAll stops every session and waits for the end.
func (r *debugRig) stopAll() {
	r.t.Helper()
	go func() { _ = r.svc().Stop(context.Background(), nil, false, false) }()
	r.waitState("inactive")
}

// openContainer puts a container of the side bar in front.
func (r *debugRig) openContainer(id string) {
	r.win.Views.Open(id)
	r.pump()
}

func TestDebugOpenDebugOpensRunAndDebugAsTheSettingSays(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name    string
		setting string
		reason  string
		noDebug *bool
		want    bool
	}{
		{"neverOpen with a breakpoint", "neverOpen", "breakpoint", nil, false},
		{"openOnSessionStart opens at the start", "openOnSessionStart", "step", nil, true},
		{"openOnSessionStart does not open for a run without debugging", "openOnSessionStart", "step", &yes, false},
		{"openOnFirstSessionStart opens at the first start", "openOnFirstSessionStart", "step", nil, true},
		{"openOnDebugBreak opens at a breakpoint", "openOnDebugBreak", "breakpoint", &no, true},
		{"openOnDebugBreak does not open at another stop", "openOnDebugBreak", "step", nil, false},
		{"openOnDebugBreak does not open at an exception", "openOnDebugBreak", "exception", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			script := scriptOf(stopSteps(tc.reason, "1")...)
			if tc.noDebug != nil && *tc.noDebug {
				script = withoutBreakpoints(stopSteps(tc.reason, "1")...)
			}
			r := newDebugRigWith(t, debugFolder(t, script), rigOpts{settings: map[string]any{"debug.openDebug": tc.setting}})
			r.openContainer(command.ViewContainerExplorer)
			r.startOne("Debug the order", debug.SessionOptions{NoDebug: tc.noDebug})
			if got := r.win.Views.Active() == command.ViewContainerDebug; got != tc.want {
				t.Errorf("Run and Debug is open: %v, want %v (the container is %q)", got, tc.want, r.win.Views.Active())
			}
			r.stopAll()
		})
	}
}

func TestDebugOpenDebugOnlyOpensAtTheFirstSessionForOpenOnFirstSessionStart(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("step", "1")...)), rigOpts{settings: map[string]any{"debug.openDebug": "openOnFirstSessionStart"}})
	r.openContainer(command.ViewContainerExplorer)
	r.startOne("Debug the order", debug.SessionOptions{})
	if got := r.win.Views.Active(); got != command.ViewContainerDebug {
		t.Fatalf("the first session left the container at %q", got)
	}
	r.stopAll()
	r.openContainer(command.ViewContainerExplorer)
	r.startOne("Debug the order", debug.SessionOptions{})
	if got := r.win.Views.Active(); got != command.ViewContainerExplorer {
		t.Errorf("the second session changed the container to %q", got)
	}
	r.stopAll()
}

func TestDebugOpenDebugDoesNotOpenForASessionThatKeepsTheViewClosed(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "1")...)), rigOpts{settings: map[string]any{"debug.openDebug": "openOnSessionStart"}})
	r.openContainer(command.ViewContainerExplorer)
	r.startOne("Debug the order", debug.SessionOptions{SuppressDebugView: true})
	if got := r.win.Views.Active(); got != command.ViewContainerExplorer {
		t.Errorf("the container is %q", got)
	}
	r.stopAll()
}

func TestDebugOpenExplorerOnEnd(t *testing.T) {
	tests := []struct {
		name    string
		setting bool
		sidebar bool
		want    string // the container when the session ended
	}{
		{"off", false, true, command.ViewContainerDebug},
		{"on opens the Explorer", true, true, command.ViewContainerExplorer},
		{"on with the side bar hidden leaves it", true, false, command.ViewContainerDebug},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "1")...)), rigOpts{settings: map[string]any{"debug.openExplorerOnEnd": tc.setting}})
			r.startOne("Debug the order", debug.SessionOptions{})
			r.openContainer(command.ViewContainerDebug)
			r.win.Layout.SetSideBarVisible(tc.sidebar)
			r.stopAll()
			r.pump()
			if got := r.win.Views.Active(); got != tc.want {
				t.Errorf("the container is %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDebugOpenExplorerOnEndWaitsForTheLastSession(t *testing.T) {
	r := newDebugRigWith(t, tasksFolder(t, fakeConfig("one", ""), fakeConfig("two", "")), rigOpts{settings: map[string]any{"debug.openExplorerOnEnd": true}})
	r.startOne("one", debug.SessionOptions{})
	r.startOne("two", debug.SessionOptions{})
	r.openContainer(command.ViewContainerDebug)
	sessions := r.svc().Model().Sessions(false)
	if len(sessions) != 2 {
		t.Fatalf("%d sessions", len(sessions))
	}
	if err := r.svc().Stop(context.Background(), sessions[0], false, false); err != nil {
		t.Fatal(err)
	}
	r.waitFor("one session to end", func() bool { return len(r.svc().Model().Sessions(false)) == 1 })
	r.pump()
	if got := r.win.Views.Active(); got != command.ViewContainerDebug {
		t.Fatalf("the container is %q while a session runs", got)
	}
	r.stopAll()
	r.pump()
	if got := r.win.Views.Active(); got != command.ViewContainerExplorer {
		t.Errorf("the container is %q after the last session", got)
	}
}

func TestDebugFocusEditorOnBreak(t *testing.T) {
	tests := []struct {
		name    string
		setting bool
		focused bool // the editor has the keyboard
	}{
		{"on gives the editor the keyboard", true, true},
		{"off shows the source and leaves the keyboard", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "1")...)), rigOpts{settings: map[string]any{"debug.focusEditorOnBreak": tc.setting}})
			r.startOne("Debug the order", debug.SessionOptions{})
			r.waitFor("the source to open", func() bool {
				e := r.win.shell.activeEditor()
				return e != nil && e.doc != nil && filepath.Base(e.doc.URI().Path) == "main.go"
			})
			r.pump()
			keys := r.win.Scopes.ScopeOf(r.ts.Binding.FocusManager().PrimaryFocus(), r.win.Keys)
			if got := keys.Get("editorTextFocus") == true; got != tc.focused {
				t.Errorf("the editor has the keyboard: %v, want %v", got, tc.focused)
			}
			r.stopAll()
		})
	}
}

func TestDebugFocusWindowOnBreak(t *testing.T) {
	tests := []struct {
		name         string
		setting      bool
		windowFocus  bool
		wantActivate bool
	}{
		{"on brings a window without the focus to the front", true, false, true},
		{"on leaves a window that has the focus", true, true, false},
		{"off leaves the window", false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "1")...)), rigOpts{settings: map[string]any{"debug.focusWindowOnBreak": tc.setting}})
			r.win.shell.windowFocused = tc.windowFocus
			activations := func() int {
				n := 0
				for _, a := range r.ts.ViewActions(ui.ImplicitViewID) {
					if a.Kind == ui.WindowActionActivate {
						n++
					}
				}
				return n
			}
			before := activations()
			r.startOne("Debug the order", debug.SessionOptions{})
			if got := activations() > before; got != tc.wantActivate {
				t.Errorf("the window was brought to the front: %v, want %v", got, tc.wantActivate)
			}
			r.stopAll()
		})
	}
}

func TestDebugSaveBeforeStart(t *testing.T) {
	tests := []struct {
		name         string
		setting      string
		fileSaved    bool
		untitledSave bool // the Save As dialog is asked for the untitled editor
	}{
		{"none saves nothing", "none", false, false},
		{"nonUntitledEditorsInActiveGroup saves the files", "nonUntitledEditorsInActiveGroup", true, false},
		{"allEditorsInActiveGroup saves the files and the untitled editor in front", "allEditorsInActiveGroup", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDebugRigWith(t, debugFolder(t, nil), rigOpts{open: []string{"main.go"}, settings: map[string]any{"debug.saveBeforeStart": tc.setting}})
			asked := 0
			r.win.SaveDialog = func(_ context.Context, _ widgets.BuildContext, _ SaveDialogOptions) (string, error) {
				asked++
				return filepath.Join(r.folder, "scratch.txt"), nil
			}
			var file *document.Document
			for _, d := range r.win.Documents.All() {
				if filepath.Base(d.URI().Path) == "main.go" {
					file = d
				}
			}
			if file == nil {
				t.Fatal("main.go is not open")
			}
			before := file.Text()
			if _, err := file.Edit([]document.Edit{{Range: buffer.Range{}, Text: "// changed\n"}}, document.EditOptions{}); err != nil {
				t.Fatal(err)
			}
			r.run("workbench.action.files.newUntitledFile", nil)
			untitled := r.win.shell.activeEditor().doc
			untitled.SetText("scratch\n")
			done := make(chan error, 1)
			go func() { done <- r.win.debugSaveBeforeStart(context.Background()) }()
			var err error
			r.waitFor("the save to end", func() bool {
				select {
				case err = <-done:
					return true
				default:
					return false
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := !file.Dirty(); got != tc.fileSaved {
				t.Errorf("the file is saved: %v, want %v", got, tc.fileSaved)
			}
			if got := asked > 0; got != tc.untitledSave {
				t.Errorf("the untitled editor was saved: %v, want %v", got, tc.untitledSave)
			}
			if tc.fileSaved {
				if data, err := os.ReadFile(filepath.Join(r.folder, "main.go")); err != nil || string(data) != "// changed\n"+before {
					t.Errorf("main.go on disk: %q %v", data, err)
				}
			}
		})
	}
}

func TestDebugHideLauncherWhileDebugging(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "1")...)), rigOpts{})
	items := func() (start, additional bool) {
		ctx := menuContext{base: r.win.Keys, settings: r.win.Settings, extra: map[string]any{"view": command.ViewContainerDebug}}
		for _, e := range r.win.Menus.Items(command.DebugContainerTitle, ctx) {
			switch e.Command {
			case command.DebugStartCommand:
				start = true
			case command.DebugSelectAndStart:
				additional = true
			}
		}
		return start, additional
	}
	tests := []struct {
		name           string
		debugging      bool
		hide           bool
		location       string
		start, further bool
	}{
		{"no session: the launcher shows", false, true, "floating", true, false},
		{"a session, not hidden", true, false, "floating", true, false},
		{"a session, hidden: Start Additional Session takes its place", true, true, "floating", false, true},
		{"a session with the toolbar docked: the launcher gives way to the toolbar", true, false, "docked", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r.t = t
			r.setSetting("debug.hideLauncherWhileDebugging", tc.hide)
			r.setSetting("debug.toolBarLocation", tc.location)
			if tc.debugging && r.win.Keys.Get("debugState") != "stopped" {
				r.startOne("Debug the order", debug.SessionOptions{})
			}
			if !tc.debugging && r.win.Keys.Get("debugState") != "inactive" {
				r.stopAll()
			}
			r.pump()
			start, further := items()
			if start != tc.start || further != tc.further {
				t.Errorf("Start Debugging shows: %v, Start Additional Session: %v; want %v and %v", start, further, tc.start, tc.further)
			}
		})
	}
	if r.win.Keys.Get("debugState") != "inactive" {
		r.stopAll()
	}
}

// withoutBreakpoints is a script for a run without debugging, which sends no breakpoints: the fixture's script without the answers to them.
func withoutBreakpoints(steps ...daptest.Step) func(daptest.Script) daptest.Script {
	return func(def daptest.Script) daptest.Script {
		var out daptest.Script
		for _, st := range scriptOf(steps...)(def) {
			if st.Op == daptest.OpReply && (st.Command == "setBreakpoints" || st.Command == "setFunctionBreakpoints" || st.Command == "setExceptionBreakpoints") {
				continue
			}
			out = append(out, st)
		}
		return out
	}
}
