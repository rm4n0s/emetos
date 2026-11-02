package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/devdebug"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The Run and Debug view in the window (step 25.4, SPEC §10.31): the container, its views and menus, the commands and keys, the toolbar and the
// status bar, against the fake debug adapter of daptest through the dev debugger of extensions/devdebug.

// debugRig is a window over a copy of the folder of the proof (testdata/proof/25.4/project): a main.go, a launch.json with two
// configurations of the fake debugger, and the script of the fake adapter, a session of two threads that stops at a breakpoint.
type debugRig struct {
	t      *testing.T
	win    *Workspace
	ts     *gluttertest.Tester
	folder string
}

// debugFolder makes the folder of a rig: the fixture of the proof, with the script changed by script when it is not nil.
func debugFolder(t *testing.T, script func(daptest.Script) daptest.Script) string {
	t.Helper()
	folder := t.TempDir()
	copyTree(t, filepath.Join(testutil.RepoRoot(t), "testdata", "proof", "25.4", "project"), folder)
	if script != nil {
		path := filepath.Join(folder, "fake.jsonl")
		s, err := daptest.LoadScript(path)
		if err != nil {
			t.Fatal(err)
		}
		lines, err := script(s).Lines()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, lines, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

// rigOpts tune a debugRig.
type rigOpts struct {
	// theme is the color theme ("Dark Modern" when empty) and size the size of the window.
	theme string
	size  ui.Size
	// open are the files of the folder that start open in the editor.
	open []string
	// settings are more settings.
	settings map[string]any
}

// newDebugRig makes the window and waits until the launches are read.
func newDebugRig(t *testing.T, folder string) *debugRig { return newDebugRigWith(t, folder, rigOpts{}) }

// newDebugRigWith is newDebugRig with options.
func newDebugRigWith(t *testing.T, folder string, o rigOpts) *debugRig {
	t.Helper()
	t.Setenv(devdebug.AdapterEnv, daptest.Program(t))
	paths := testutil.Isolate(t)
	// the Debug Console opens with the first session (step 25.6); the goldens of the other views are made without it, the tests of the
	// console ask for it
	values := map[string]any{"debug.internalConsoleOptions": "neverOpen", "files.hotExit": "off", "editor.minimap.enabled": false, "breadcrumbs.enabled": false, "editor.fontSize": 14,
		"window.menuBarVisibility": "classic"}
	if o.theme != "" {
		values["workbench.colorTheme"] = o.theme
	}
	for k, v := range o.settings {
		values[k] = v
	}
	testutil.Settings(t, paths, values)
	p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{devdebug.New}})
	win, err := NewWorkspace(context.Background(), p, folder)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(win.release)
	var docs []*document.Document
	for _, name := range o.open {
		doc, err := win.Documents.OpenFile(context.Background(), filepath.Join(folder, name), document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	size := o.size
	if size.Width == 0 {
		size = testWindowSize
	}
	ts := testutil.New(t, size)
	ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	ts.PumpWidget(newAppDocs(context.Background(), win, docs, nil, ""))
	r := &debugRig{t: t, win: win, ts: ts, folder: folder}
	if _, err := os.Stat(filepath.Join(folder, ".emetos", "launch.json")); err == nil {
		r.waitFor("the launches to be read", func() bool { return len(win.DebugService().Launches().All()) > 0 })
	}
	return r
}

// pump runs the window for a few frames.
func (r *debugRig) pump() {
	for range 4 {
		r.ts.Pump(20 * time.Millisecond)
	}
}

// waitFor pumps the window until the condition holds.
func (r *debugRig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
		r.ts.FlushPosted()
		r.ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	r.pump()
}

// run runs a command in the window.
func (r *debugRig) run(id string, args any) {
	r.t.Helper()
	if err := r.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: r.win.Keys, Source: command.SourceAPI}, id, args); err != nil {
		r.t.Fatalf("%s: %v", id, err)
	}
	r.pump()
}

// svc is the debug service of the window.
func (r *debugRig) svc() *debug.Service { return r.win.DebugService() }

// breakpoint sets a breakpoint at main.go:21 when there is none: the script of the fixture answers a setBreakpoints.
func (r *debugRig) breakpoint() {
	r.t.Helper()
	if len(r.svc().Model().Breakpoints(debug.BreakpointFilter{})) == 0 {
		if _, err := r.svc().AddBreakpoints(context.Background(), filepath.Join(r.folder, "main.go"), []debug.BreakpointData{{Line: 21}}); err != nil {
			r.t.Fatal(err)
		}
	}
}

// stopped starts the selected configuration, with the views of Run and Debug open (the script answers the requests of its Variables), and
// waits for the stop of the script.
func (r *debugRig) stopped() {
	r.t.Helper()
	r.breakpoint()
	r.run(command.ViewContainerDebug, nil)
	r.run(command.DebugStartCommand, nil)
	r.waitFor("the stop", func() bool {
		f := r.svc().Model().Focus()
		return f.Frame != nil && r.win.Keys.Get("debugState") == "stopped"
	})
}

func TestTheContainerOfRunAndDebugIsInTheActivityBar(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, nil))
	var ids []string
	for _, c := range r.win.Views.Registry().Containers(command.ViewLocationSideBar) {
		ids = append(ids, c.ID)
	}
	if want := command.ViewContainerDebug; !contains(ids, want) {
		t.Fatalf("the activity bar has %v, want %s in it", ids, want)
	}
	r.run(command.ViewContainerDebug, nil)
	if got := r.win.Views.Active(); got != command.ViewContainerDebug {
		t.Errorf("the active container is %q", got)
	}
}

func TestStartingASessionStopsAtTheBreakpointAndTheViewsFollow(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, nil))
	if _, err := r.svc().AddBreakpoints(context.Background(), filepath.Join(r.folder, "main.go"), []debug.BreakpointData{{Line: 21}}); err != nil {
		t.Fatal(err)
	}
	r.run(command.ViewContainerDebug, nil)
	if got := r.win.Keys.Get("debugUx"); got != "default" {
		t.Errorf("debugUx = %v with a configuration", got)
	}
	r.stopped()
	for key, want := range map[string]any{"debugState": "stopped", "inDebugMode": true, "debugType": "dev.debug.fake", "breakpointsExist": true,
		"restartFrameSupported": true, "debugSetVariableSupported": true, "focusedSessionIsAttach": false} {
		if got := r.win.Keys.Get(key); got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	if !r.win.shell.debug.debugging {
		t.Error("the status bar does not take the colors of the session")
	}
	panes := r.win.shell.debug.panes
	r.waitFor("the variables", func() bool { return panes.Variables.Model().Len() > 0 })
	if _, ok := panes.CallStack.Row(r.svc().Model().Focus().Thread.Key()); !ok {
		t.Error("the call stack has no row for the stopped thread")
	}
	r.run(command.DebugStopCommand, nil)
	r.waitFor("the end", func() bool { return r.win.Keys.Get("debugState") == "inactive" })
	if r.win.shell.debug.debugging {
		t.Error("the status bar keeps the colors of the session after it ended")
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

var _ = extension.HostOrigin
