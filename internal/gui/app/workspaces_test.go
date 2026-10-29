package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The tests of the workspace tabs of the one window (step 9b.1; they were the tests of several windows of step 7.8):
// the manager of the tabs over the implicit window of the widget tester, which makes no other window.

// multiEnv is the folders of a user: what several runs of the program (several multiApp) share.
type multiEnv struct {
	t     *testing.T
	paths storage.Paths
	clock *storagetest.Clock
	// realBus makes runRoot leave the desktop to the session bus named by DBUS_SESSION_BUS_ADDRESS (a test points it
	// at nothing) instead of installing a fake one.
	realBus bool
}

func newMultiEnv(t *testing.T, extra map[string]any) *multiEnv {
	t.Helper()
	paths := testutil.Isolate(t)
	// the menu bar of the tests is the classic one: the tests tap its titles (the compact menu has tests of its own)
	values := map[string]any{"editor.fontSize": 14, "files.hotExit": "off", "window.menuBarVisibility": "classic"}
	for k, v := range extra {
		values[k] = v
	}
	testutil.Settings(t, paths, values)
	return &multiEnv{t: t, paths: paths, clock: storagetest.NewClock()}
}

// folder makes a folder with files in it, and returns its path.
func (e *multiEnv) folder(name string, files ...string) string {
	e.t.Helper()
	dir := filepath.Join(e.t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	writeTree(e.t, dir, files...)
	return dir
}

// multiApp is one run of the program: its process and its tabs, shown in a tester.
type multiApp struct {
	t     *testing.T
	env   *multiEnv
	ts    *gluttertest.Tester
	p     *Process
	m     *Workspaces
	ctx   context.Context
	quits int
}

// run starts the program the way Run does (the plan of the start, the first window shown, the others with the first
// frame) in a tester, with the options of the command line.
func (e *multiEnv) run(opts Options) *multiApp {
	e.t.Helper()
	return e.runWith(opts, ProcessOptions{})
}

func (e *multiEnv) runWith(opts Options, po ProcessOptions) *multiApp {
	e.t.Helper()
	a, _ := e.runRoot(opts, po, false)
	return a
}

// runRoot is runWith that can show the root of the application as Run makes it: the tabs inside the system tray (step
// 9b.4), on a fake desktop that the test gets (without one the tray would use the session bus of the machine).
func (e *multiEnv) runRoot(opts Options, po ProcessOptions, tray bool) (*multiApp, *gluttertest.FakeDesktop) {
	e.t.Helper()
	po.Paths = e.paths
	if po.StateAfter == nil {
		po.StateAfter = e.clock.AfterFunc
	}
	ctx := e.t.Context()
	p := newTestProcess(po)
	a := &multiApp{t: e.t, env: e, p: p, m: p.Workspaces, ctx: ctx}
	a.m.ctx = ctx
	a.m.Quit = func() { a.quits++ }
	shown, err := a.m.start(ctx, opts)
	if err != nil {
		e.t.Fatalf("start: %v", err)
	}
	shown.steps = nil
	a.ts = testutil.New(e.t, ui.Size{Width: 1000, Height: 700})
	if !tray {
		a.ts.PumpWidget(a.m.hostWidget())
		a.settle()
		return a, nil
	}
	var fd *gluttertest.FakeDesktop
	if !e.realBus {
		fd = a.ts.FakeDesktop()
	}
	a.ts.PumpWidget(a.m.rootWidget())
	a.settle()
	return a, fd
}

// settle pumps the frames the windows need to be made and shown.
func (a *multiApp) settle() {
	a.t.Helper()
	for range 8 {
		a.ts.Pump(20 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
}

// waitFor pumps until the condition holds.
func (a *multiApp) waitFor(what string, cond func() bool) {
	a.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			a.t.Fatalf("timed out waiting for %s", what)
		}
		a.ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
}

// folders are the folders of the windows, in the order they were made ("" for a window without one).
func (a *multiApp) folders() []string {
	var out []string
	for _, w := range a.m.All() {
		out = append(out, w.Folder)
	}
	return out
}

// otherWindows are the Glutter windows the app made besides the one window of RunApp: there are none, whatever the
// tabs do (SPEC §10.14).
func (a *multiApp) otherWindows() []gluttertest.TestWindow { return a.ts.Windows() }

// shellOf is the shell of a window.
func (a *multiApp) shellOf(w *Workspace) *shellState {
	a.t.Helper()
	if w.shell == nil {
		a.t.Fatalf("the window %q has no shell", w.Folder)
	}
	return w.shell
}

// command runs a command in a window.
func (a *multiApp) command(w *Workspace, id string, args any) error {
	a.t.Helper()
	err := w.Commands.Run(command.Context{Ctx: a.ctx, Keys: w.Keys, Source: command.SourceAPI}, id, args)
	a.settle()
	return err
}

// open opens a file in a window and makes it dirty.
func (a *multiApp) edit(w *Workspace, path string) {
	a.t.Helper()
	if err := a.command(w, "dev.openFile", map[string]any{"path": path}); err != nil {
		a.t.Fatal(err)
	}
	doc, ok := w.Documents.Get(document.FileURI(path))
	if !ok {
		a.t.Fatalf("%s is not open", path)
	}
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "x"}}, document.EditOptions{}); err != nil {
		a.t.Fatal(err)
	}
	a.settle()
}

// dialog is the message of the dialog that shows in a window, or "".
func (a *multiApp) dialog(w *Workspace) string {
	spec, ok := w.Dialogs.Current()
	if !ok {
		return ""
	}
	return spec.Message
}

// answer presses a button of the dialog of a window.
func (a *multiApp) answer(w *Workspace, label string) {
	a.t.Helper()
	if !w.Dialogs.IsShowing() {
		a.t.Fatalf("no dialog shows in %q (looking for %q)", w.Folder, label)
	}
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText(label)))
	a.settle()
}

// windowOf is the window that has the folder.
func (a *multiApp) workspaceOf(folder string) *Workspace {
	a.t.Helper()
	w := a.m.workspaceForFolder(folder)
	if w == nil {
		a.t.Fatalf("no window has %q (windows: %q)", folder, a.folders())
	}
	return w
}

func askAbout(name string) string {
	return "Do you want to save the changes you made to " + name + "?"
}

func TestWorkspacesListsTheTabsInTheOrderOfTheStrip(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if got := a.folders(); !slices.Equal(got, []string{one}) {
		t.Fatalf("tabs %q", got)
	}
	if w, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil || w.Folder != two {
		t.Fatalf("Open = %v, %v", w, err)
	}
	a.settle()
	if got := a.folders(); !slices.Equal(got, []string{one, two}) {
		t.Errorf("tabs %q", got)
	}
	if got := a.m.Active(); got == nil || got.Folder != two {
		t.Errorf("the tab that was opened last is the active one: %v", got)
	}
	// one window holds both tabs: no other Glutter window is made
	if n := len(a.otherWindows()); n != 0 {
		t.Errorf("%d Glutter windows besides the first, want 0", n)
	}
	// every tab has a kind, a stable id, a state id of its own, and its own services
	tabs := a.m.Tabs()
	if len(tabs) != 2 || tabs[0].Kind() != TabWorkspace || tabs[0].ID() == "" || tabs[0].ID() == tabs[1].ID() {
		t.Errorf("tabs %+v", tabs)
	}
	if tabs[0].Workspace().Tab() != tabs[0] {
		t.Error("a workspace does not know its tab")
	}
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	if w1.StateID() == "" || w1.StateID() == w2.StateID() {
		t.Errorf("state ids %q %q", w1.StateID(), w2.StateID())
	}
	if w1.Commands == w2.Commands || w1.Notify == w2.Notify || w1.Documents == w2.Documents || w1.Settings == w2.Settings {
		t.Error("the tabs share services that are the workspace's")
	}
	if w1.Process != w2.Process {
		t.Error("the tabs are of different processes")
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestAFolderIsNeverOpenInTwoTabs(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	// opening the folder of the first tab makes that tab the active one
	w, err := a.m.Open(WorkspaceSpec{Folder: one})
	if err != nil {
		t.Fatal(err)
	}
	a.settle()
	if w != a.m.All()[0] || a.m.Count() != 2 || a.m.Active() != w {
		t.Errorf("a second tab was made for the folder: %d tabs, active %v", a.m.Count(), a.m.Active().Folder)
	}
	// through a link to the folder too
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(two, link); err != nil {
		t.Skip("no symbolic links")
	}
	if w, err := a.m.Open(WorkspaceSpec{Folder: link}); err != nil || w.Folder != two || a.m.Count() != 2 || a.m.Active() != w {
		t.Errorf("Open(link) = %v, %v with %d tabs", w, err, a.m.Count())
	}
}

func TestNewWorkspaceTabOpensAnEmptyTabWithItsOwnState(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a := env.run(Options{Folder: one})
	w1 := a.workspaceOf(one)
	// the command through the key, as the dispatcher runs it
	if !sendChord(a.ts, chordOf(t, "ctrl+shift+n")) {
		t.Fatal("ctrl+shift+n was not used")
	}
	a.settle()
	if a.m.Count() != 2 {
		t.Fatalf("%d tabs after New Workspace Tab, want 2", a.m.Count())
	}
	w2 := a.m.All()[1]
	if w2.Folder != "" || w2.Workspace != nil || len(w2.Documents.All()) != 0 {
		t.Errorf("the new tab is not empty: folder %q, %d documents", w2.Folder, len(w2.Documents.All()))
	}
	if w2.StateID() == "" || w2.StateID() == w1.StateID() {
		t.Errorf("state ids %q and %q", w1.StateID(), w2.StateID())
	}
	if w1.Documents == w2.Documents {
		t.Error("the new tab shares the documents")
	}
	// a second one has an id of its own too, and opens after the active tab
	a.m.Activate(w1.Tab())
	if err := a.command(w1, "workbench.action.newWindow", nil); err != nil {
		t.Fatal(err)
	}
	if a.m.Count() != 3 {
		t.Fatalf("%d tabs", a.m.Count())
	}
	w3 := a.m.Active()
	if w3.StateID() == w2.StateID() || w3.StateID() == w1.StateID() {
		t.Errorf("state id %q of the third tab is not new", w3.StateID())
	}
	if a.m.All()[1] != w3 {
		t.Errorf("a tab opens after the one that was active: %q", a.folders())
	}
	// the open tabs are noted in the global state, in the order of the strip
	g, _ := a.p.State.LoadGlobal(a.ctx)
	if len(g.WorkspaceTabs) != 3 || g.WorkspaceTabs[0].Folder != one || g.WorkspaceTabs[1].StateID != w3.StateID() || g.WorkspaceTabs[2].StateID != w2.StateID() {
		t.Errorf("tabs %+v", g.WorkspaceTabs)
	}
	if g.ActiveWorkspaceTab != a.m.ActiveTab().ID() {
		t.Errorf("active tab %q, want %q", g.ActiveWorkspaceTab, a.m.ActiveTab().ID())
	}
	if len(g.OpenWindows) != 0 {
		t.Errorf("the list of windows of an older Emetos is written again: %+v", g.OpenWindows)
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// No window is made besides the one of RunApp, whatever opens: the platform is never asked for a regular window.
func TestNoGlutterWindowIsMadeForATab(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	// a platform that could not make a window is of no consequence
	a.ts.SetWindowCreateError(os.ErrPermission)
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := a.m.NewTab(); err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	a.settle()
	if a.m.Count() != 3 || len(a.otherWindows()) != 0 {
		t.Errorf("%d tabs, %d Glutter windows, want 3 and 0", a.m.Count(), len(a.otherWindows()))
	}
}

func TestPlaceFolder(t *testing.T) {
	tests := []struct {
		name string
		req  folderRequest
		want FolderPlacement
	}{
		// a folder that is open comes to the front, whatever else is asked
		{"open, default", folderRequest{Setting: "default", Open: true}, PlaceFocus},
		{"open, on", folderRequest{Setting: "on", Open: true}, PlaceFocus},
		{"open, off", folderRequest{Setting: "off", Open: true}, PlaceFocus},
		{"open, -n", folderRequest{Setting: "off", ForceNew: true, Open: true}, PlaceFocus},
		{"open, -r", folderRequest{Setting: "on", ForceReuse: true, Open: true}, PlaceFocus},
		// the options of the command line win over the setting
		{"-n with off", folderRequest{Setting: "off", ForceNew: true}, PlaceNewTab},
		{"-r with on", folderRequest{Setting: "on", ForceReuse: true}, PlaceSameTab},
		{"-n with default", folderRequest{Setting: "default", ForceNew: true}, PlaceNewTab},
		{"-r with default", folderRequest{Setting: "default", ForceReuse: true}, PlaceSameTab},
		{"-n on an untouched No Folder tab", folderRequest{Setting: "default", ForceNew: true, TargetEmpty: true}, PlaceNewTab},
		// the setting, read as "in a new tab"
		{"on", folderRequest{Setting: "on"}, PlaceNewTab},
		{"off", folderRequest{Setting: "off"}, PlaceSameTab},
		{"default", folderRequest{Setting: "default"}, PlaceNewTab},
		{"default, an untouched No Folder tab", folderRequest{Setting: "default", TargetEmpty: true}, PlaceSameTab},
		{"on, an untouched No Folder tab", folderRequest{Setting: "on", TargetEmpty: true}, PlaceNewTab},
		{"off, an untouched No Folder tab", folderRequest{Setting: "off", TargetEmpty: true}, PlaceSameTab},
		{"a value that is not one", folderRequest{Setting: "maybe"}, PlaceNewTab},
		{"no value", folderRequest{}, PlaceNewTab},
		{"no value, an untouched No Folder tab", folderRequest{TargetEmpty: true}, PlaceSameTab},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := placeFolder(tc.req); got != tc.want {
				t.Errorf("placeFolder = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPlaceFiles(t *testing.T) {
	tests := []struct {
		setting         string
		forceNew, reuse bool
		want            bool
	}{
		{"off", false, false, false},
		{"on", false, false, true},
		{"default", false, false, false},
		{"", false, false, false},
		{"off", true, false, true},
		{"default", true, false, true},
		{"on", false, true, false},
	}
	for _, tc := range tests {
		if got := placeFiles(tc.setting, tc.forceNew, tc.reuse); got != tc.want {
			t.Errorf("placeFiles(%q, %v, %v) = %v, want %v", tc.setting, tc.forceNew, tc.reuse, got, tc.want)
		}
	}
}

func TestOpenFolderFollowsTheSettingForEverySource(t *testing.T) {
	type source struct {
		name string
		do   func(a *multiApp, from *Workspace, folder string)
	}
	sources := []source{
		{"Open Recent", func(a *multiApp, from *Workspace, folder string) {
			if err := a.command(from, "openRecentFolder", folder); err != nil {
				a.t.Fatal(err)
			}
		}},
		{"the manager (a folder picked in the app)", func(a *multiApp, from *Workspace, folder string) {
			a.m.OpenFolder(from, folder, false, false, nil)
			a.settle()
		}},
		{"a launch", func(a *multiApp, from *Workspace, folder string) {
			a.m.HandleLaunch(ipcOpen(folder), func(LaunchOutcome) {})
			a.settle()
		}},
	}
	tests := []struct {
		setting string
		// wantNew[source] says whether a new tab opens, else the active tab is replaced
		wantNew [3]bool
	}{
		{"on", [3]bool{true, true, true}},
		{"off", [3]bool{false, false, false}},
		{"default", [3]bool{true, true, true}},
	}
	for _, tc := range tests {
		for i, src := range sources {
			t.Run(tc.setting+"/"+src.name, func(t *testing.T) {
				env := newMultiEnv(t, map[string]any{"window.openFoldersInNewWindow": tc.setting})
				one := env.folder("one", "a.go")
				other := env.folder("other", "b.go")
				a := env.run(Options{Folder: one})
				src.do(a, a.m.All()[0], other)
				a.waitFor("the folder", func() bool { return a.m.workspaceForFolder(other) != nil })
				if tc.wantNew[i] {
					if got := a.folders(); !slices.Equal(got, []string{one, other}) {
						t.Errorf("windows %q, want a new one", got)
					}
					return
				}
				if got := a.folders(); !slices.Equal(got, []string{other}) {
					t.Errorf("windows %q, want the folder to replace the window", got)
				}
			})
		}
	}
}

func TestAnOpenFolderComesToTheFrontInsteadOfOpeningAgain(t *testing.T) {
	env := newMultiEnv(t, map[string]any{"window.openFoldersInNewWindow": "on"})
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1 := a.workspaceOf(one)
	if a.m.Active() == w1 {
		t.Fatal("the first window is the active one already")
	}
	var got *Workspace
	a.m.OpenFolder(a.m.Active(), one, false, false, func(w *Workspace) { got = w })
	a.settle()
	if got != w1 || a.m.Active() != w1 || a.m.Count() != 2 {
		t.Errorf("got %v active %v with %d windows", got, a.m.Active(), a.m.Count())
	}
	// the window of the implicit controller was asked to activate
	if acts := a.ts.ViewActions(ui.ImplicitViewID); !slices.ContainsFunc(acts, func(w ui.WindowAction) bool { return w.Kind == ui.WindowActionActivate }) {
		t.Errorf("the first window was not asked to activate: %v", acts)
	}
}

func TestOpeningAFolderInTheSameTabRecreatesTheWorkspace(t *testing.T) {
	env := newMultiEnv(t, map[string]any{"window.openFoldersInNewWindow": "off"})
	one := env.folder("one", "a.go", "b.go")
	other := env.folder("other", "c.go")
	a := env.run(Options{Folder: one})
	old := a.workspaceOf(one)
	tab := old.Tab()
	a.edit(old, filepath.Join(one, "a.go"))
	// the tab has unsaved changes: the folder waits for the answer, and Cancel keeps the tab as it is
	a.m.OpenFolder(old, other, false, false, nil)
	a.settle()
	if got := a.dialog(old); got != askAbout("a.go") {
		t.Fatalf("the dialog says %q", got)
	}
	a.answer(old, "Cancel")
	if a.m.All()[0] != old || old.Folder != one {
		t.Fatal("the window changed after Cancel")
	}
	a.m.OpenFolder(old, other, false, false, nil)
	a.settle()
	a.answer(old, "Don't Save")
	a.waitFor("the new window", func() bool { return a.m.All()[0].Folder == other })
	next := a.m.All()[0]
	a.settle()
	if next == old || a.m.Count() != 1 {
		t.Fatalf("the workspace was not made again: %d tabs", a.m.Count())
	}
	if next.Tab() != tab || tab.Workspace() != next || a.m.Tabs()[0] != tab {
		t.Error("the tab did not keep its place and its identity")
	}
	if next.StateID() == old.StateID() || next.Folder != other || next.Workspace == nil {
		t.Errorf("the new window: state id %q folder %q", next.StateID(), next.Folder)
	}
	// the services of the old window are disposed and those of the new one run
	if old.shell != nil || old.Tab() != nil {
		t.Error("the old workspace is still shown")
	}
	if old.Backups() != nil {
		t.Error("the backups of the old window are still tracked")
	}
	if next.shell == nil || !next.shell.Mounted() {
		t.Error("the new window has no shell")
	}
	if len(old.Documents.All()) != 0 {
		t.Error("the old window keeps documents")
	}
	if got := a.ts.Windows(); len(got) != 0 {
		t.Errorf("a Glutter window was made for a workspace that replaces another: %+v", got)
	}
	g, _ := a.p.State.LoadGlobal(a.ctx)
	if len(g.WorkspaceTabs) != 1 || g.WorkspaceTabs[0].Folder != other || g.WorkspaceTabs[0].ID != tab.ID() || g.LastWindow == nil || g.LastWindow.Folder != other {
		t.Errorf("global state: tabs %+v last %+v", g.WorkspaceTabs, g.LastWindow)
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestOpeningAFolderRestoresItsSavedState(t *testing.T) {
	env := newMultiEnv(t, map[string]any{"window.openFoldersInNewWindow": "off"})
	one := env.folder("one", "a.go", "b.go")
	other := env.folder("other", "c.go", "d.go")
	// a run that leaves editors in the second folder
	first := env.run(Options{Folder: other})
	first.edit(first.workspaceOf(other), filepath.Join(other, "d.go"))
	if err := first.command(first.workspaceOf(other), "workbench.action.files.save", nil); err != nil {
		t.Fatal(err)
	}
	first.workspaceOf(other).RequestClose(CloseReasonQuit)
	first.waitFor("the quit", func() bool { return first.quits == 1 })
	// a run in the first folder opens the second in the same window: its editors come back
	a := env.run(Options{Folder: one})
	old := a.workspaceOf(one)
	a.m.OpenFolder(old, other, false, false, nil)
	a.waitFor("the folder", func() bool { return a.m.All()[0].Folder == other })
	a.settle()
	next := a.m.All()[0]
	names := func() []string {
		var out []string
		for _, in := range next.shell.activeGroup().Model.Editors(0, false) {
			out = append(out, in.Name())
		}
		return out
	}
	a.waitFor("the saved editors", func() bool { return slices.Contains(names(), "d.go") })
}

func TestClosingOneTabAsksOnlyAboutItsDocuments(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	a.edit(w1, filepath.Join(one, "a.go"))
	a.edit(w2, filepath.Join(two, "b.go"))
	w2.RequestClose(CloseReasonTab)
	a.settle()
	if got := a.dialog(w2); got != askAbout("b.go") {
		t.Fatalf("the dialog of the closing tab says %q", got)
	}
	if got := a.dialog(w1); got != "" {
		t.Errorf("the other tab asks %q", got)
	}
	// Cancel keeps both
	a.answer(w2, "Cancel")
	if a.m.Count() != 2 || a.quits != 0 {
		t.Fatalf("after Cancel: %d tabs, %d quits", a.m.Count(), a.quits)
	}
	w2.RequestClose(CloseReasonTab)
	a.settle()
	a.answer(w2, "Don't Save")
	a.waitFor("the tab to go", func() bool { return a.m.Count() == 1 })
	a.settle()
	if a.quits != 0 {
		t.Error("the application quit although a tab stays")
	}
	if w2.shell != nil || w2.Backups() != nil || w2.Tab() != nil {
		t.Error("the closed tab still has services")
	}
	if a.m.Active() != w1 || a.dialog(w1) != "" {
		t.Errorf("the other tab: active %v dialog %q", a.m.Active() == w1, a.dialog(w1))
	}
	if n := len(a.otherWindows()); n != 0 {
		t.Errorf("%d Glutter windows", n)
	}
	// the list of the open tabs no longer has it
	g, _ := a.p.State.LoadGlobal(a.ctx)
	if len(g.WorkspaceTabs) != 1 || g.WorkspaceTabs[0].Folder != one || g.ActiveWorkspaceTab != g.WorkspaceTabs[0].ID {
		t.Errorf("tabs %+v active %q", g.WorkspaceTabs, g.ActiveWorkspaceTab)
	}
}

func TestClosingTheLastTabLeavesANoFolderTab(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a := env.run(Options{Folder: one})
	w1 := a.workspaceOf(one)
	a.edit(w1, filepath.Join(one, "a.go"))
	w1.RequestClose(CloseReasonTab)
	a.settle()
	if got := a.dialog(w1); got != askAbout("a.go") {
		t.Fatalf("the dialog says %q", got)
	}
	// Cancel keeps the tab
	a.answer(w1, "Cancel")
	if a.m.Count() != 1 || a.m.Active() != w1 {
		t.Fatalf("after Cancel: %d tabs", a.m.Count())
	}
	w1.RequestClose(CloseReasonTab)
	a.settle()
	a.answer(w1, "Don't Save")
	a.waitFor("the new tab", func() bool { return a.m.Count() == 1 && a.m.Active() != w1 })
	a.settle()
	next := a.m.Active()
	if next.Folder != "" || next.Workspace != nil || len(next.Documents.All()) != 0 || a.m.Tabs()[0].Kind() != TabWorkspace {
		t.Errorf("the tab that is left is not an empty workspace: %q", next.Folder)
	}
	if next.shell == nil || !next.shell.Mounted() {
		t.Error("the new tab is not shown")
	}
	if a.quits != 0 {
		t.Error("closing the last tab quit the application")
	}
	g, _ := a.p.State.LoadGlobal(a.ctx)
	if len(g.WorkspaceTabs) != 1 || g.WorkspaceTabs[0].Folder != "" || g.WorkspaceTabs[0].StateID != next.StateID() {
		t.Errorf("tabs %+v", g.WorkspaceTabs)
	}
	// and the No Folder tab closing leaves another one
	next.RequestClose(CloseReasonTab)
	a.waitFor("a new No Folder tab", func() bool { return a.m.Count() == 1 && a.m.Active() != next })
	if a.m.Active().StateID() == next.StateID() {
		t.Error("the new No Folder tab has the state of the one that closed")
	}
}

func TestClosingATabThatIsNotActiveBringsItToTheFrontForTheDialog(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	a.edit(w1, filepath.Join(one, "a.go"))
	if a.m.Active() != w2 {
		t.Fatal("the second tab is not the active one")
	}
	w1.RequestClose(CloseReasonTab)
	a.settle()
	if a.m.Active() != w1 || a.dialog(w1) != askAbout("a.go") {
		t.Errorf("active %q dialog %q", a.m.Active().Folder, a.dialog(w1))
	}
	a.answer(w1, "Don't Save")
	a.waitFor("the tab to go", func() bool { return a.m.Count() == 1 })
	if a.m.Active() != w2 {
		t.Error("the tab that was active before is not the active one again")
	}
}

func TestQuitAsksEveryTabInTurn(t *testing.T) {
	setup := func(t *testing.T) (*multiApp, *Workspace, *Workspace) {
		env := newMultiEnv(t, nil)
		one := env.folder("one", "a.go")
		two := env.folder("two", "b.go")
		a := env.run(Options{Folder: one})
		if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
			t.Fatal(err)
		}
		a.settle()
		w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
		a.edit(w1, filepath.Join(one, "a.go"))
		a.edit(w2, filepath.Join(two, "b.go"))
		return a, w1, w2
	}
	t.Run("Cancel on the second keeps both", func(t *testing.T) {
		a, w1, w2 := setup(t)
		w2.RequestClose(CloseReasonQuit)
		a.settle()
		// the tabs are asked in the order of the strip, whichever asked
		if got := a.dialog(w1); got != askAbout("a.go") || a.dialog(w2) != "" {
			t.Fatalf("first window asks %q, second %q", got, a.dialog(w2))
		}
		a.answer(w1, "Don't Save")
		if got := a.dialog(w2); got != askAbout("b.go") {
			t.Fatalf("second window asks %q", got)
		}
		a.answer(w2, "Cancel")
		if a.quits != 0 || a.m.Count() != 2 {
			t.Errorf("quits %d tabs %d", a.quits, a.m.Count())
		}
		// the first window answered "Don't Save", but nothing was closed: the changes are still there
		if !w1.Documents.All()[0].Dirty() {
			t.Error("the changes of the first window were dropped although the quit was cancelled")
		}
		// a new quit asks again
		w1.RequestClose(CloseReasonQuit)
		a.settle()
		if a.dialog(w1) == "" {
			t.Error("the second quit did not ask")
		}
	})
	t.Run("Don't Save on both quits once", func(t *testing.T) {
		a, w1, w2 := setup(t)
		w1.RequestClose(CloseReasonQuit)
		a.settle()
		a.answer(w1, "Don't Save")
		a.answer(w2, "Don't Save")
		a.waitFor("the quit", func() bool { return a.quits == 1 })
		if a.quits != 1 {
			t.Errorf("quit %d times", a.quits)
		}
		// asking again while the questions go on is ignored
		a.m.RequestQuit(w1)
	})
	t.Run("Save on both saves both", func(t *testing.T) {
		a, w1, w2 := setup(t)
		w1.RequestClose(CloseReasonQuit)
		a.settle()
		a.answer(w1, "Save")
		a.waitFor("the second question", func() bool { return a.dialog(w2) != "" })
		a.answer(w2, "Save")
		a.waitFor("the quit", func() bool { return a.quits == 1 })
		for _, w := range []*Workspace{w1, w2} {
			for _, doc := range w.Documents.All() {
				if doc.Dirty() {
					t.Errorf("%s is still dirty", doc.URI())
				}
			}
		}
	})
	t.Run("the tab the dialog asks in becomes the active one", func(t *testing.T) {
		a, w1, w2 := setup(t)
		w2.RequestClose(CloseReasonQuit)
		a.settle()
		a.answer(w1, "Don't Save")
		if a.m.Active() != w2 {
			t.Errorf("the tab that asks is not the active one")
		}
	})
}

func TestTheCloseButtonOfTheWindowIsAQuitOverEveryTab(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	a.edit(w1, filepath.Join(one, "a.go"))
	// the platform's request to close the window is the one Glutter gives the app: it is not carried out before the
	// questions, and it asks every tab (with a tray the close button hides the window instead: tray_test.go)
	if closes := a.ts.RequestWindowClose(ui.ImplicitViewID); closes {
		t.Fatal("the platform would close the window before the question")
	}
	a.settle()
	if got := a.dialog(w1); got != askAbout("a.go") || a.m.Active() != w1 {
		t.Fatalf("the first tab asks %q (active %q)", got, a.m.Active().Folder)
	}
	a.answer(w1, "Don't Save")
	a.waitFor("the quit", func() bool { return a.quits == 1 })
	if a.m.Count() != 2 || w2.shell == nil {
		t.Errorf("%d tabs after the quit was agreed", a.m.Count())
	}
}

func TestTheApplicationsRequestToExitIsAQuit(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	a.edit(a.workspaceOf(two), filepath.Join(two, "b.go"))
	if resp := a.ts.Binding.HandleRequestAppExit(); resp != ui.AppExitResponseCancel {
		t.Errorf("the response is %v: the application ends itself", resp)
	}
	a.settle()
	if got := a.dialog(a.workspaceOf(two)); got != askAbout("b.go") {
		t.Errorf("the dialog says %q", got)
	}
}

func TestSettingsReachEveryWindow(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	if err := w1.Settings.Update(a.ctx, "editor.fontSize", 21, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	for _, w := range []*Workspace{w1, w2} {
		if got := settings.Get[float64](w.Settings, "editor.fontSize", ""); got != 21 {
			t.Errorf("editor.fontSize = %v in %q", got, w.Folder)
		}
	}
	// a setting of the folder of a window is that window's
	if err := w2.Settings.Update(a.ctx, "editor.tabSize", 3, settings.TargetWorkspace); err != nil {
		t.Fatal(err)
	}
	if got := settings.Get[float64](w1.Settings, "editor.tabSize", ""); got == 3 {
		t.Error("the workspace setting of one window reached the other")
	}
	if got := settings.Get[float64](w2.Settings, "editor.tabSize", ""); got != 3 {
		t.Errorf("editor.tabSize = %v", got)
	}
	// each window has its own theme service, since the profiles of the windows may differ (SPEC §10.12)
	if w1.Themes == w2.Themes {
		t.Error("the windows share a theme service")
	}
}

func TestCommandsAndNotificationsBelongToTheWindow(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	// a command opens the file in the window it runs in
	if err := a.command(w1, "dev.openFile", filepath.Join(one, "a.go")); err != nil {
		t.Fatal(err)
	}
	if len(w1.Documents.All()) != 1 || len(w2.Documents.All()) != 0 {
		t.Errorf("documents: %d and %d", len(w1.Documents.All()), len(w2.Documents.All()))
	}
	if err := a.command(w2, "dev.notify", "only here"); err != nil {
		t.Fatal(err)
	}
	if len(w2.Notify.Items()) != 1 || len(w1.Notify.Items()) != 0 {
		t.Errorf("notifications: %d and %d", len(w1.Notify.Items()), len(w2.Notify.Items()))
	}
}

func TestSwitchWindowIsThePickerOfTheTabs(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	if err := a.command(w2, "workbench.action.switchWindow", nil); err != nil {
		t.Fatal(err)
	}
	if !w2.QuickInput.Visible() {
		t.Fatal("the picker is not shown")
	}
	a.settle()
	items := w2.QuickInput.Picker().Items()
	if len(items) != 2 {
		t.Fatalf("%d rows, want 2", len(items))
	}
	// the active tab is the first row (step 9b.5), and says so
	var current []string
	for _, it := range items {
		if it.Label == "" {
			t.Errorf("a row without a name: %+v", it)
		}
		if strings.HasPrefix(it.Description, SwitchTabCurrent) {
			current = append(current, it.ID)
		}
	}
	if len(current) != 1 || current[0] != items[0].ID || items[0].ID != w2.Tab().ID() {
		t.Errorf("the rows marked as the current tab: %v", current)
	}
	// the tab after it is the one the picker is on
	if row, ok := w2.QuickInput.Picker().Active(); !ok || row.Item.ID != items[1].ID || row.Item.ID != w1.Tab().ID() {
		t.Errorf("the active row is %+v", row)
	}
	w2.QuickInput.Accept(quickinput.KeyMods{})
	a.settle()
	if a.m.Active() != w1 {
		t.Error("choosing the row did not make the window the active one")
	}
}

func TestDevOpenFolderInNewTab(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	w1 := a.workspaceOf(one)
	if err := a.command(w1, "dev.openFolderInNewTab", map[string]any{"path": two}); err != nil {
		t.Fatal(err)
	}
	if got := a.folders(); !slices.Equal(got, []string{one, two}) {
		t.Errorf("windows %q", got)
	}
	// a relative path is relative to the working directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, two)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.command(w1, "dev.openFolderInNewTab", rel); err != nil || a.m.Count() != 2 {
		t.Errorf("an open folder by a relative path: %v with %d windows", err, a.m.Count())
	}
	// a folder that is not there fails with the tag of the workspace
	err = a.command(w1, "dev.openFolderInNewTab", map[string]any{"path": filepath.Join(one, "nothing")})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("WorkspaceFolderNotFound") {
		t.Errorf("error %v", err)
	}
}

func TestParseOpenFolderInNewTab(t *testing.T) {
	getwd := func() (string, error) { return "/work", nil }
	tests := []struct {
		name string
		args any
		want string
		tag  string
	}{
		{"a path", "/a/b", "/a/b", ""},
		{"an object", map[string]any{"path": "/a/b/"}, "/a/b", ""},
		{"relative", map[string]any{"path": "x/y"}, "/work/x/y", ""},
		{"nothing", nil, "", "parseOpenFolderInNewTab.DevOpenFolderInvalid"},
		{"a number", 3.0, "", "parseOpenFolderInNewTab.DevOpenFolderInvalid"},
		{"an empty path", "", "", "parseOpenFolderInNewTab.DevOpenFolderInvalid"},
		{"no path", map[string]any{"folder": "/a"}, "", "parseOpenFolderInNewTab.DevOpenFolderInvalid"},
		{"a path that is not a text", map[string]any{"path": 4.0}, "", "parseOpenFolderInNewTab.DevOpenFolderInvalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOpenFolderInNewTab(tc.args, getwd)
			if tc.tag == "" {
				if err != nil || got != tc.want {
					t.Errorf("got %q, %v, want %q", got, err, tc.want)
				}
				return
			}
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tc.tag) {
				t.Errorf("error %v, want the route %s", err, tc.tag)
			}
		})
	}
	// a working directory that cannot be found
	_, err := parseOpenFolderInNewTab("x", func() (string, error) { return "", os.ErrNotExist })
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("parseOpenFolderInNewTab.DevOpenFolderInvalid") {
		t.Errorf("error %v", err)
	}
}

func TestStateOfTheOpenWindowsIsWrittenOnceAndAgrees(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	// the recently opened lists are the process's: the files of both windows are in the one list
	a.edit(w1, filepath.Join(one, "a.go"))
	w1.AddRecentFile(filepath.Join(one, "a.go"))
	w2.AddRecentFile(filepath.Join(two, "b.go"))
	env.clock.Advance(2 * storage.DefaultStateDelay)
	g, err := a.p.State.LoadGlobal(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.Recent.Files, []string{filepath.Join(two, "b.go"), filepath.Join(one, "a.go")}) {
		t.Errorf("recent files %q", g.Recent.Files)
	}
	if !slices.Contains(g.Recent.Folders, one) || !slices.Contains(g.Recent.Folders, two) {
		t.Errorf("recent folders %q", g.Recent.Folders)
	}
	if a.p.State.Pending() {
		t.Error("state waits to be written after the clock moved")
	}
}

// ipcOpen is a launch of a folder.
func ipcOpen(folder string) ipc.OpenArgs { return ipc.OpenArgs{Folder: folder} }

// ---- hot exit, restore and launches across windows ----

// tabFoldersOf lists the folders of the windows the global state keeps ("" for one without a folder).
func tabFoldersOf(a *multiApp) []string {
	g, _ := a.p.State.LoadGlobal(a.ctx)
	var out []string
	for _, w := range g.WorkspaceTabs {
		out = append(out, w.Folder)
	}
	return out
}

// dirtyDocs are the paths of the documents of a window that have unsaved changes.
func dirtyDocs(w *Workspace) []string {
	var out []string
	for _, d := range w.Documents.All() {
		if d != nil && d.Dirty() {
			out = append(out, filepath.Base(d.URI().Path))
		}
	}
	return out
}

func TestQuitBacksUpEveryWindowWithoutAskingAndTheNextStartRestoresBoth(t *testing.T) {
	env := newMultiEnv(t, map[string]any{"files.hotExit": "onExit"})
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	a.edit(w1, filepath.Join(one, "a.go"))
	a.edit(w2, filepath.Join(two, "b.go"))
	w2.RequestClose(CloseReasonQuit)
	a.waitFor("the quit", func() bool { return a.quits == 1 })
	if a.dialog(w1) != "" || a.dialog(w2) != "" {
		t.Errorf("a dialog asked: %q %q", a.dialog(w1), a.dialog(w2))
	}
	// both windows were kept in the list, and each has its backup
	if got := tabFoldersOf(a); !slices.Equal(got, []string{one, two}) {
		t.Errorf("open windows %q", got)
	}
	b := env.run(Options{})
	b.waitFor("both windows", func() bool { return b.m.Count() == 2 })
	b.settle()
	if got := b.folders(); !slices.Equal(got, []string{one, two}) {
		t.Fatalf("restored windows %q", got)
	}
	if got := dirtyDocs(b.workspaceOf(one)); !slices.Equal(got, []string{"a.go"}) {
		t.Errorf("the first window restored %q", got)
	}
	if got := dirtyDocs(b.workspaceOf(two)); !slices.Equal(got, []string{"b.go"}) {
		t.Errorf("the second window restored %q", got)
	}
	// the window that was active last is the active one again
	if b.m.Active() != b.workspaceOf(two) {
		t.Errorf("the active window is %q", b.m.Active().Folder)
	}
	if errs := b.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestClosingOneWindowAsksWithOnExitButBacksUpAFolderWithOnExitAndWindowClose(t *testing.T) {
	for _, tc := range []struct {
		hotExit string
		asks    bool
	}{{"onExit", true}, {"onExitAndWindowClose", false}} {
		t.Run(tc.hotExit, func(t *testing.T) {
			env := newMultiEnv(t, map[string]any{"files.hotExit": tc.hotExit})
			one := env.folder("one", "a.go")
			two := env.folder("two", "b.go")
			a := env.run(Options{Folder: one})
			if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
				t.Fatal(err)
			}
			a.settle()
			w2 := a.workspaceOf(two)
			a.edit(w2, filepath.Join(two, "b.go"))
			w2.RequestClose(CloseReasonTab)
			a.settle()
			if got := a.dialog(w2) != ""; got != tc.asks {
				t.Fatalf("asks = %v, want %v", got, tc.asks)
			}
			if tc.asks {
				a.answer(w2, "Cancel")
				return
			}
			a.waitFor("the window to go", func() bool { return a.m.Count() == 1 })
		})
	}
}

func TestEachModeOfRestoreWindows(t *testing.T) {
	tests := []struct {
		mode string
		want []string // the folders of the windows, with "-" for one without a folder, as "one", "two"
	}{
		{"all", []string{"one", "two", "-"}},
		{"preserve", []string{"one", "two", "-"}},
		{"folders", []string{"one", "two"}},
		{"one", []string{"two"}},
		{"none", []string{"-"}},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			env := newMultiEnv(t, map[string]any{"window.restoreWindows": tc.mode})
			one := env.folder("one", "a.go")
			two := env.folder("two", "b.go")
			a := env.run(Options{Folder: one})
			if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
				t.Fatal(err)
			}
			if _, err := a.m.NewTab(); err != nil {
				t.Fatal(err)
			}
			a.settle()
			a.workspaceOf(two).Raise()
			a.workspaceOf(one).RequestClose(CloseReasonQuit)
			a.waitFor("the quit", func() bool { return a.quits == 1 })
			b := env.run(Options{})
			b.waitFor("the windows", func() bool { return b.m.Count() == len(tc.want) })
			b.settle()
			var got []string
			for _, f := range b.folders() {
				switch f {
				case one:
					f = "one"
				case two:
					f = "two"
				case "":
					f = "-"
				}
				got = append(got, f)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("windows %q, want %q", got, tc.want)
			}
			if b.m.Count() != len(tc.want) {
				t.Errorf("%d windows", b.m.Count())
			}
		})
	}
}

func TestACommandLineOpensOnTopOfTheRestoredWindowsOnlyWithPreserve(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want int // windows
	}{{"all", 1}, {"preserve", 3}} {
		t.Run(tc.mode, func(t *testing.T) {
			env := newMultiEnv(t, map[string]any{"window.restoreWindows": tc.mode})
			one := env.folder("one", "a.go")
			two := env.folder("two", "b.go")
			three := env.folder("three", "c.go")
			a := env.run(Options{Folder: one})
			if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
				t.Fatal(err)
			}
			a.settle()
			a.workspaceOf(one).RequestClose(CloseReasonQuit)
			a.waitFor("the quit", func() bool { return a.quits == 1 })
			b := env.run(Options{Folder: three})
			b.waitFor("the windows", func() bool { return b.m.Count() == tc.want })
			b.settle()
			if got := b.folders(); got[0] == three && tc.mode == "preserve" {
				t.Errorf("windows %q: the folder of the command line is a window after the restored ones", got)
			}
			if b.m.workspaceForFolder(three) == nil {
				t.Errorf("windows %q have no window for the command line", b.folders())
			}
		})
	}
	t.Run("a folder that was open opens no window of its own", func(t *testing.T) {
		env := newMultiEnv(t, map[string]any{"window.restoreWindows": "preserve"})
		one := env.folder("one", "a.go")
		two := env.folder("two", "b.go")
		a := env.run(Options{Folder: one})
		if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
			t.Fatal(err)
		}
		a.settle()
		a.workspaceOf(one).RequestClose(CloseReasonQuit)
		a.waitFor("the quit", func() bool { return a.quits == 1 })
		b := env.run(Options{Folder: two, Files: []string{filepath.Join(two, "b.go")}})
		b.waitFor("the windows", func() bool { return b.m.Count() == 2 })
		b.settle()
		if got := b.folders(); !slices.Equal(got, []string{one, two}) {
			t.Errorf("windows %q", got)
		}
		w := b.workspaceOf(two)
		b.waitFor("the file", func() bool { return len(w.Documents.All()) == 1 })
	})
}

func TestTheBackupsOfEveryWindowComeBackAfterACrashWhateverTheMode(t *testing.T) {
	env := newMultiEnv(t, map[string]any{"files.hotExit": "onExit", "window.restoreWindows": "none"})
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	a.edit(w1, filepath.Join(one, "a.go"))
	a.edit(w2, filepath.Join(two, "b.go"))
	// the backups are written as the documents change, and the process dies (no RequestClose)
	env.clock.Advance(2 * storage.DefaultBackupDelay)
	a.waitFor("both backups", func() bool {
		for _, w := range []*Workspace{w1, w2} {
			store, err := storage.NewBackupStore(env.paths, w.StateID(), storage.BackupStoreOptions{})
			if err != nil || !store.HasBackups() {
				return false
			}
		}
		return true
	})
	if err := a.p.State.Flush(a.ctx); err != nil {
		t.Fatal(err)
	}
	b := env.run(Options{})
	b.waitFor("the windows", func() bool { return b.m.Count() == 2 })
	b.settle()
	// the mode "none" restores no window, but the two that have backups come back with their changes (and the
	// empty window of the mode is not needed)
	if got := b.folders(); !slices.Equal(got, []string{one, two}) {
		t.Fatalf("windows %q", got)
	}
	for _, tc := range []struct {
		folder string
		want   string
	}{{one, "a.go"}, {two, "b.go"}} {
		if got := dirtyDocs(b.workspaceOf(tc.folder)); !slices.Equal(got, []string{tc.want}) {
			t.Errorf("%s restored %q", tc.folder, got)
		}
	}
}

// launched runs a launch and settles.
func (a *multiApp) launched(args ipc.OpenArgs) *launchResult {
	a.t.Helper()
	r := &launchResult{}
	r.cancel = a.m.HandleLaunch(args, func(o LaunchOutcome) { r.outcomes = append(r.outcomes, o) })
	a.settle()
	return r
}

func TestLaunchesOpenInTheRightWindow(t *testing.T) {
	setup := func(t *testing.T, extra map[string]any) (*multiApp, string, string) {
		env := newMultiEnv(t, extra)
		one := env.folder("one", "a.go", "a2.go")
		two := env.folder("two", "b.go")
		a := env.run(Options{Folder: one})
		if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
			t.Fatal(err)
		}
		a.settle()
		return a, one, two
	}
	target := func(dir, name string) ipc.FileTarget { return ipc.FileTarget{Path: filepath.Join(dir, name)} }
	t.Run("a folder that is open gets the files and comes to the front", func(t *testing.T) {
		a, one, _ := setup(t, nil)
		w1 := a.workspaceOf(one)
		r := a.launched(ipc.OpenArgs{Folder: one, Files: []ipc.FileTarget{target(one, "a.go")}})
		wantOutcomes(t, r, LaunchOutcome{})
		a.waitFor("the file", func() bool { return len(w1.Documents.All()) == 1 })
		if a.m.Count() != 2 || a.m.Active() != w1 {
			t.Errorf("%d windows, active %q", a.m.Count(), a.m.Active().Folder)
		}
	})
	t.Run("a new folder opens in a new tab with the default", func(t *testing.T) {
		a, _, _ := setup(t, nil)
		three := a.env.folder("three", "c.go")
		a.launched(ipc.OpenArgs{Folder: three, Files: []ipc.FileTarget{target(three, "c.go")}})
		a.waitFor("the window", func() bool { return a.m.workspaceForFolder(three) != nil })
		w := a.m.workspaceForFolder(three)
		a.waitFor("the file", func() bool { return len(w.Documents.All()) == 1 })
		if a.m.Count() != 3 {
			t.Errorf("%d windows", a.m.Count())
		}
	})
	t.Run("a new folder replaces the window with off, or --reuse-window", func(t *testing.T) {
		for _, reuse := range []bool{false, true} {
			extra := map[string]any{"window.openFoldersInNewWindow": "off"}
			if reuse {
				extra = map[string]any{"window.openFoldersInNewWindow": "on"}
			}
			a, _, two := setup(t, extra)
			three := a.env.folder("three", "c.go")
			a.launched(ipc.OpenArgs{Folder: three, ReuseWindow: reuse})
			a.waitFor("the window", func() bool { return a.m.workspaceForFolder(three) != nil })
			if a.m.Count() != 2 || a.m.workspaceForFolder(two) != nil {
				t.Errorf("reuse %v: windows %q, want the active window replaced", reuse, a.folders())
			}
		}
	})
	t.Run("--new-window opens a new window with off", func(t *testing.T) {
		a, _, _ := setup(t, map[string]any{"window.openFoldersInNewWindow": "off"})
		three := a.env.folder("three", "c.go")
		a.launched(ipc.OpenArgs{Folder: three, NewWindow: true})
		a.waitFor("the window", func() bool { return a.m.workspaceForFolder(three) != nil })
		if a.m.Count() != 3 {
			t.Errorf("windows %q", a.folders())
		}
	})
	t.Run("files go to the active tab, whatever folder the other tabs have", func(t *testing.T) {
		a, one, two := setup(t, nil)
		w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
		// the second tab is the active one: the file of the first tab's folder opens there
		a.launched(ipc.OpenArgs{Files: []ipc.FileTarget{target(one, "a2.go")}})
		a.waitFor("the file in the active tab", func() bool { return len(w2.Documents.All()) == 1 })
		if a.m.Active() != w2 || len(w1.Documents.All()) != 0 || a.m.Count() != 2 {
			t.Errorf("active %q, %d documents in the first tab, %d tabs", a.m.Active().Folder, len(w1.Documents.All()), a.m.Count())
		}
	})
	t.Run("files open in a new No Folder tab with --new-window and with the setting on", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			extra map[string]any
			args  ipc.OpenArgs
			want  int
		}{
			{"--new-window", nil, ipc.OpenArgs{NewWindow: true}, 3},
			{"on", map[string]any{"window.openFilesInNewWindow": "on"}, ipc.OpenArgs{}, 3},
			{"default", map[string]any{"window.openFilesInNewWindow": "default"}, ipc.OpenArgs{}, 2},
			{"--reuse-window beats on", map[string]any{"window.openFilesInNewWindow": "on"}, ipc.OpenArgs{ReuseWindow: true}, 2},
			{"off", nil, ipc.OpenArgs{}, 2},
		} {
			a, _, two := setup(t, tc.extra)
			args := tc.args
			args.Files = []ipc.FileTarget{target(two, "b.go")}
			a.launched(args)
			a.waitFor(tc.name+": the tabs", func() bool { return a.m.Count() == tc.want })
			a.settle()
			if a.m.Count() != tc.want {
				t.Errorf("%s: %d tabs", tc.name, a.m.Count())
			}
			w := a.m.Active()
			a.waitFor(tc.name+": the file", func() bool { return len(w.Documents.All()) == 1 })
			if tc.want == 3 && w.Folder != "" {
				t.Errorf("%s: the new tab has the folder %q", tc.name, w.Folder)
			}
		}
	})
	t.Run("--new-window with nothing else opens an empty window", func(t *testing.T) {
		a, _, _ := setup(t, nil)
		a.launched(ipc.OpenArgs{NewWindow: true})
		if a.m.Count() != 3 || a.m.All()[2].Folder != "" {
			t.Errorf("windows %q", a.folders())
		}
	})
	t.Run("--wait ends when the file closes in the window it opened in", func(t *testing.T) {
		a, one, two := setup(t, nil)
		w2 := a.workspaceOf(two)
		r := a.launched(ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{target(two, "b.go")}})
		a.waitFor("the file", func() bool { return len(w2.Documents.All()) == 1 })
		if len(r.outcomes) != 0 {
			t.Fatalf("the launch ended before the file closed: %v", r.outcomes)
		}
		_ = one
		s := a.shellOf(w2)
		doc := w2.Documents.All()[0]
		s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{s.inputFor(s.activeGroup(), doc)})
		a.waitFor("the end of the wait", func() bool { return len(r.outcomes) == 1 })
		wantOutcomes(t, r, LaunchOutcome{})
	})
	t.Run("--wait ends when the window closes", func(t *testing.T) {
		a, _, two := setup(t, nil)
		w2 := a.workspaceOf(two)
		r := a.launched(ipc.OpenArgs{Wait: true, Files: []ipc.FileTarget{target(two, "b.go")}})
		a.waitFor("the file", func() bool { return len(w2.Documents.All()) == 1 })
		w2.RequestClose(CloseReasonTab)
		a.waitFor("the end of the wait", func() bool { return len(r.outcomes) == 1 })
		a.settle()
		wantOutcomes(t, r, LaunchOutcome{})
	})
	t.Run("a merge of files that cannot be read ends with the failure, whichever window", func(t *testing.T) {
		a, _, _ := setup(t, nil)
		r := a.launched(ipc.OpenArgs{Wait: true, Merge: []string{"/1", "/2", "/3", "/4"}})
		a.waitFor("the end of the wait", func() bool { return len(r.outcomes) == 1 })
		wantOutcomes(t, r, LaunchOutcome{ExitCode: 1, Message: "none of the files could be opened"})
	})
}

func TestTheLauncherReachesTheManagerOfTheWindows(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	launcher := NewLauncher()
	a := env.runWith(Options{Folder: one}, ProcessOptions{Launcher: launcher})
	out := make(chan ipc.Reply, 1)
	go func() {
		out <- launcher.Handle(context.Background(), ipc.OpenArgs{Folder: two, NewWindow: true})
	}()
	a.waitFor("the window", func() bool { return a.m.workspaceForFolder(two) != nil })
	select {
	case r := <-out:
		if r.Type != ipc.TypeOK {
			t.Errorf("reply %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reply")
	}
	// the application ends: the launcher answers that Emetos is closing
	a.workspaceOf(one).RequestClose(CloseReasonQuit)
	a.waitFor("the quit", func() bool { return a.quits == 1 })
	if r := launcher.Handle(context.Background(), ipc.OpenArgs{}); r.Type != ipc.TypeError || r.Message != LauncherClosingMessage {
		t.Errorf("reply %+v", r)
	}
}

func TestStartupPlan(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	gone := env.folder("gone", "c.go")
	p := newTestProcess(ProcessOptions{Paths: env.paths})
	ctx := t.Context()
	setMode := func(mode string) {
		t.Helper()
		if err := p.Settings.Validate("window.restoreWindows", mode); err != "" {
			t.Fatal(err)
		}
		store := testutil.Settings(t, env.paths, map[string]any{"window.restoreWindows": mode, "files.hotExit": "off"})
		_ = store
	}
	const emptyID = "00000000000000aa"
	if err := p.State.UpdateGlobal(ctx, func(g *storage.GlobalState) bool {
		g.WorkspaceTabs = []storage.WorkspaceTab{
			{ID: "1111111111111111", Folder: one}, {ID: "4444444444444444", StateID: emptyID},
			{ID: "2222222222222222", Folder: two}, {ID: "3333333333333333", Folder: gone},
		}
		g.ActiveWorkspaceTab = "2222222222222222"
		g.LastWindow = &storage.LastWindow{Folder: two}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	gone2 := env.folder("gone2", "d.go")
	folders := func(plan []WorkspaceSpec) []string {
		var out []string
		for _, w := range plan {
			if w.Folder == "" {
				out = append(out, "-")
				continue
			}
			out = append(out, filepath.Base(w.Folder))
		}
		return out
	}
	tests := []struct {
		name string
		mode string
		opts Options
		want []string
		// active is the place in the plan of the tab that is shown first
		active int
	}{
		{"all", "all", Options{}, []string{"one", "-", "two"}, 2},
		{"preserve", "preserve", Options{}, []string{"one", "-", "two"}, 2},
		{"folders", "folders", Options{}, []string{"one", "two"}, 1},
		{"one", "one", Options{}, []string{"two"}, 0},
		{"none", "none", Options{}, []string{"-"}, 0},
		{"all with a folder", "all", Options{Folder: one}, []string{"one"}, 0},
		{"all with files", "all", Options{Files: []string{"/x/y.txt"}}, []string{"-"}, 0},
		{"preserve with a folder that was open", "preserve", Options{Folder: two}, []string{"one", "-", "two"}, 2},
		{"preserve with a folder that was not", "preserve", Options{Folder: gone2}, []string{"one", "-", "two", "gone2"}, 3},
		{"preserve with files", "preserve", Options{Files: []string{"/x/y.txt"}}, []string{"one", "-", "two"}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setMode(tc.mode)
			plan, active := p.startupPlan(ctx, tc.opts)
			if got := folders(plan); !slices.Equal(got, tc.want) {
				t.Errorf("plan %q, want %q", got, tc.want)
			}
			if active != tc.active {
				t.Errorf("the tab shown first is %d, want %d", active, tc.active)
			}
			// a tab without a folder has a state id; the first one is never without one
			for _, w := range plan {
				if w.Folder == "" && w.StateID == "" {
					t.Errorf("a tab without a folder has no id: %+v", w)
				}
			}
		})
	}
	// the tabs that come back keep their ids, so that the strip is the same
	t.Run("the ids of the tabs come back", func(t *testing.T) {
		setMode("all")
		plan, _ := p.startupPlan(ctx, Options{})
		var ids []string
		for _, w := range plan {
			ids = append(ids, w.TabID)
		}
		if !slices.Equal(ids, []string{"1111111111111111", "4444444444444444", "2222222222222222"}) {
			t.Errorf("tab ids %q", ids)
		}
	})
	t.Run("the files of preserve go to the tab that was active", func(t *testing.T) {
		setMode("preserve")
		plan, _ := p.startupPlan(ctx, Options{Files: []string{"/x/y.txt"}})
		for _, w := range plan {
			if (w.Folder == two) != (len(w.Files) == 1) {
				t.Errorf("window %q has the files %v", w.Folder, w.Files)
			}
		}
	})
}
