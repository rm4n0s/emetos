package app

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/workspace"
)

// The tests of the window tabs (step 9b.1, SPEC §10.14): opening, activating, moving and closing them, what the one
// window shows of the active one, and what the others keep while they are not shown.

// focusIn tells whether the keyboard focus is in the tree of the workspace.
func (a *multiApp) focusIn(w *Workspace) bool {
	a.t.Helper()
	n := a.ts.Binding.FocusManager().PrimaryFocus()
	return n != nil && w.keyNode != nil && slices.Contains(n.Ancestors(), w.keyNode)
}

// tabs3 opens three folders as three tabs: the first is the active one at the start, the third after the others.
func tabs3(t *testing.T, extra map[string]any) (*multiApp, []string) {
	t.Helper()
	env := newMultiEnv(t, extra)
	one := env.folder("one", "a.go", "b.go")
	two := env.folder("two", "c.go")
	three := env.folder("three", "d.go")
	a := env.run(Options{Folder: one})
	for _, f := range []string{two, three} {
		if _, err := a.m.Open(WorkspaceSpec{Folder: f}); err != nil {
			t.Fatal(err)
		}
		a.settle()
	}
	return a, []string{one, two, three}
}

func TestActivateAndMoveTheTabs(t *testing.T) {
	a, f := tabs3(t, nil)
	tabs := a.m.Tabs()
	if len(tabs) != 3 || a.m.ActiveTab() != tabs[2] {
		t.Fatalf("%d tabs, active %v", len(tabs), a.m.ActiveTab() == tabs[2])
	}
	w1 := a.workspaceOf(f[0])
	shell := w1.shell
	// activate
	if !a.m.Activate(tabs[0]) || a.m.Active() != w1 || a.m.IndexOf(a.m.ActiveTab()) != 0 {
		t.Error("Activate did not make the first tab the active one")
	}
	if !a.m.ActivateIndex(1) || a.m.ActiveTab() != tabs[1] {
		t.Error("ActivateIndex(1)")
	}
	if a.m.ActivateIndex(3) || a.m.ActivateIndex(-1) || a.m.ActiveTab() != tabs[1] {
		t.Error("a place with no tab changed the active tab")
	}
	if a.m.Activate(nil) {
		t.Error("Activate(nil) = true")
	}
	// move: the order changes, the active tab stays, and the tabs keep their widgets
	a.settle()
	changes := 0
	stop := a.m.OnChange(func() { changes++ })
	defer stop()
	if !a.m.Move(tabs[0], 2) {
		t.Fatal("Move failed")
	}
	a.settle()
	if got := a.folders(); !slices.Equal(got, []string{f[1], f[2], f[0]}) {
		t.Errorf("order %q", got)
	}
	if a.m.ActiveTab() != tabs[1] || w1.shell != shell || !shell.Mounted() {
		t.Error("moving a tab changed the active tab or lost the widgets of the moved one")
	}
	if !a.m.Move(tabs[2], -5) || !slices.Equal(a.folders(), []string{f[2], f[1], f[0]}) {
		t.Errorf("Move to a place before the first: %q", a.folders())
	}
	if !a.m.Move(tabs[2], 1) {
		t.Error("Move failed")
	}
	if at := a.m.IndexOf(tabs[2]); !a.m.Move(tabs[2], at) || a.m.IndexOf(tabs[2]) != at {
		t.Error("Move to the place it is at")
	}
	if changes == 0 {
		t.Error("the listeners were not told of the moves")
	}
	// the order is in the state, the active tab too
	g, _ := a.p.State.LoadGlobal(a.ctx)
	var got []string
	for _, tab := range g.WorkspaceTabs {
		got = append(got, tab.Folder)
	}
	if !slices.Equal(got, a.folders()) || g.ActiveWorkspaceTab != tabs[1].ID() {
		t.Errorf("state tabs %q active %q", got, g.ActiveWorkspaceTab)
	}
	// a tab that left cannot be moved or activated
	a.m.Close(tabs[0], nil)
	a.waitFor("the tab to go", func() bool { return a.m.Count() == 2 })
	if a.m.Move(tabs[0], 0) || a.m.Activate(tabs[0]) {
		t.Error("a closed tab was moved or activated")
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestSwitchingTabsKeepsTheEditorsScrollPositionsAndFocusOfEach(t *testing.T) {
	long := strings.Repeat("package a // a long line to scroll through\n", 400)
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go", "b.go")
	two := env.folder("two", "c.go")
	if err := os.WriteFile(filepath.Join(one, "a.go"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	a := env.run(Options{Folder: one})
	w1 := a.workspaceOf(one)
	for _, name := range []string{"a.go", "b.go"} {
		if err := a.command(w1, "dev.openFile", filepath.Join(one, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.command(w1, "dev.openFile", filepath.Join(one, "a.go")); err != nil {
		t.Fatal(err)
	}
	e1 := w1.shell.activeEditor()
	e1.view.SetScrollTop(1234)
	a.settle()
	if !a.focusIn(w1) {
		t.Fatal("the editor of the first tab has no focus")
	}
	top, count := e1.view.ScrollTop(), w1.shell.editorCount()
	if top == 0 || count != 2 {
		t.Fatalf("scroll %v editors %d", top, count)
	}
	// the second tab: its own editor, which takes the focus
	w2, err := a.m.Open(WorkspaceSpec{Folder: two})
	if err != nil {
		t.Fatal(err)
	}
	a.settle()
	if err := a.command(w2, "dev.openFile", filepath.Join(two, "c.go")); err != nil {
		t.Fatal(err)
	}
	if !a.focusIn(w2) || a.focusIn(w1) {
		t.Errorf("focus in the second tab %v, in the first %v", a.focusIn(w2), a.focusIn(w1))
	}
	// back: the same editors, the same scroll position, the focus where it was
	a.m.Activate(w1.Tab())
	a.settle()
	if w1.shell.activeEditor() != e1 || w1.shell.editorCount() != count || e1.view.ScrollTop() != top {
		t.Errorf("the first tab changed: editor kept %v, %d editors, scroll %v", w1.shell.activeEditor() == e1, w1.shell.editorCount(), e1.view.ScrollTop())
	}
	if !a.focusIn(w1) || !w1.shell.activeEditor().focus.HasFocus() {
		t.Error("the focus did not come back to the editor of the first tab")
	}
	if a.focusIn(w2) {
		t.Error("the second tab keeps the focus")
	}
	a.m.Activate(w2.Tab())
	a.settle()
	if !a.focusIn(w2) || w2.shell.editorCount() != 1 {
		t.Error("the focus did not come back to the second tab")
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestOnlyTheActiveTabTicksAndPaints(t *testing.T) {
	a, f := tabs3(t, nil)
	modes := func() []bool {
		var out []bool
		for _, tab := range a.m.Tabs() {
			ctx := tab.key.CurrentContext()
			if ctx == nil {
				t.Fatal("a tab has no widgets")
			}
			// the tickers below the slot of the tab: the mode the shell of the workspace sees
			out = append(out, widgets.TickerModeValuesOf(tab.ws.shell.Context()).Enabled)
		}
		return out
	}
	if got := modes(); !slices.Equal(got, []bool{false, false, true}) {
		t.Errorf("tickers on %v, want only the third", got)
	}
	a.m.Activate(a.m.Tabs()[0])
	a.settle()
	if got := modes(); !slices.Equal(got, []bool{true, false, false}) {
		t.Errorf("tickers on %v, want only the first", got)
	}
	// every tab stays mounted
	for _, w := range a.m.All() {
		if w.shell == nil || !w.shell.Mounted() {
			t.Errorf("the tab of %q is not mounted", w.Folder)
		}
	}
	// what the window shows is the active tab only: the screen of two tabs is the screen of the one that is shown
	a.edit(a.workspaceOf(f[0]), filepath.Join(f[0], "a.go"))
	a.m.Activate(a.m.Tabs()[0])
	a.settle()
	shown := a.ts.Screenshot().Pix
	other := a.workspaceOf(f[1])
	a.m.Activate(other.Tab())
	a.settle()
	a.m.Activate(a.workspaceOf(f[0]).Tab())
	a.settle()
	if again := a.ts.Screenshot().Pix; string(again) != string(shown) {
		t.Error("the first tab does not look the same after another was shown")
	}
}

func TestKeysGoToTheActiveTabOnly(t *testing.T) {
	a, f := tabs3(t, nil)
	w1, w3 := a.workspaceOf(f[0]), a.workspaceOf(f[2])
	// ctrl+n opens an untitled document in the workspace whose dispatcher uses the key
	if !sendChord(a.ts, chordOf(t, "ctrl+n")) {
		t.Fatal("ctrl+n was not used")
	}
	a.settle()
	if n := len(w3.Documents.All()); n != 1 {
		t.Errorf("the active tab has %d documents", n)
	}
	for _, w := range []*Workspace{w1, a.workspaceOf(f[1])} {
		if len(w.Documents.All()) != 0 {
			t.Errorf("the tab of %q got the key", w.Folder)
		}
	}
	a.m.Activate(w1.Tab())
	a.settle()
	if !sendChord(a.ts, chordOf(t, "ctrl+n")) {
		t.Fatal("ctrl+n was not used")
	}
	a.settle()
	if len(w1.Documents.All()) != 1 || len(w3.Documents.All()) != 1 {
		t.Errorf("documents %d and %d", len(w1.Documents.All()), len(w3.Documents.All()))
	}
}

func TestTheWindowShowsTheTitleOfTheActiveTab(t *testing.T) {
	a, f := tabs3(t, nil)
	w1, w3 := a.workspaceOf(f[0]), a.workspaceOf(f[2])
	controller, ok := a.m.implicitController()
	if !ok {
		t.Fatal("no controller")
	}
	a.edit(w1, filepath.Join(f[0], "a.go"))
	if !strings.Contains(w1.Title(), "a.go") || !strings.Contains(w3.Title(), "three") {
		t.Fatalf("titles %q %q", w1.Title(), w3.Title())
	}
	if got := controller.Title(); got != w3.Title() {
		t.Errorf("the window is titled %q, want the active tab's %q", got, w3.Title())
	}
	a.m.Activate(w1.Tab())
	a.settle()
	if got := controller.Title(); got != w1.Title() {
		t.Errorf("the window is titled %q, want %q", got, w1.Title())
	}
	// a tab that is not shown does not give the window its title when it changes
	if err := a.command(w3, "dev.openFile", filepath.Join(f[2], "d.go")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w3.Title(), "d.go") || controller.Title() != w1.Title() {
		t.Errorf("w3 %q, window %q", w3.Title(), controller.Title())
	}
	a.m.Activate(w3.Tab())
	a.settle()
	if controller.Title() != w3.Title() {
		t.Errorf("window %q, want %q", controller.Title(), w3.Title())
	}
}

func TestATabThatIsNotShownKeepsRunning(t *testing.T) {
	t.Run("its watcher applies an external change", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		one := env.folder("one", "a.txt")
		two := env.folder("two", "b.txt")
		a := env.runWith(Options{Folder: one}, ProcessOptions{WatchFiles: true})
		if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
			t.Fatal(err)
		}
		a.settle()
		w1 := a.workspaceOf(one)
		path := filepath.Join(one, "a.txt")
		if err := a.command(w1, "dev.openFile", path); err != nil {
			t.Fatal(err)
		}
		doc := w1.Documents.All()[0]
		before := doc.Text()
		// the second tab is the active one; the file of the first changes on disk
		a.waitFor("the watcher", func() bool { return w1.Watcher() != nil })
		time.Sleep(200 * time.Millisecond)
		if err := os.WriteFile(path, []byte("changed outside\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		a.waitFor("the change", func() bool { return doc.Text() != before })
		if a.m.Active() == w1 || doc.Text() != "changed outside\n" {
			t.Errorf("active %q text %q", a.m.Active().Folder, doc.Text())
		}
	})
	t.Run("the tree of its folder follows the disk", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		one := env.folder("one", "a.txt")
		two := env.folder("two", "b.txt")
		a := env.runWith(Options{Folder: one}, ProcessOptions{WatchFiles: true})
		if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
			t.Fatal(err)
		}
		a.settle()
		w1 := a.workspaceOf(one)
		got := make(chan workspace.Event, 16)
		stop := w1.OnFileEvents(func(events []workspace.Event) {
			for _, e := range events {
				select {
				case got <- e:
				default:
				}
			}
		})
		defer stop()
		a.waitFor("the watcher", func() bool { return w1.Watcher() != nil })
		time.Sleep(200 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(one, "new.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		a.waitFor("the event", func() bool {
			select {
			case e := <-got:
				return strings.HasSuffix(e.Path, "new.txt")
			default:
				return false
			}
		})
	})
	t.Run("its auto save saves", func(t *testing.T) {
		env := newMultiEnv(t, map[string]any{"files.autoSave": "afterDelay", "files.autoSaveDelay": 100})
		one := env.folder("one", "a.txt")
		two := env.folder("two", "b.txt")
		a := env.run(Options{Folder: one})
		if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
			t.Fatal(err)
		}
		a.settle()
		w1 := a.workspaceOf(one)
		a.edit(w1, filepath.Join(one, "a.txt"))
		a.m.Activate(a.workspaceOf(two).Tab())
		a.waitFor("the save", func() bool { return len(dirtyDocs(w1)) == 0 })
		if got, _ := os.ReadFile(filepath.Join(one, "a.txt")); !strings.HasPrefix(string(got), "x") {
			t.Errorf("a.txt = %q", got)
		}
	})
	t.Run("its backups are written", func(t *testing.T) {
		env := newMultiEnv(t, map[string]any{"files.hotExit": "onExit"})
		one := env.folder("one", "a.txt")
		two := env.folder("two", "b.txt")
		a := env.run(Options{Folder: one})
		if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
			t.Fatal(err)
		}
		a.settle()
		w1 := a.workspaceOf(one)
		a.edit(w1, filepath.Join(one, "a.txt"))
		a.m.Activate(a.workspaceOf(two).Tab())
		env.clock.Advance(2 * storage.DefaultBackupDelay)
		a.waitFor("the backup", func() bool {
			store, err := storage.NewBackupStore(env.paths, w1.StateID(), storage.BackupStoreOptions{})
			return err == nil && store.HasBackups()
		})
	})
}

func TestLeavingATabIsAWindowChangeForAutoSave(t *testing.T) {
	env := newMultiEnv(t, map[string]any{"files.autoSave": "onWindowChange"})
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	a.m.Activate(w1.Tab())
	a.edit(w1, filepath.Join(one, "a.go"))
	a.edit(w2, filepath.Join(two, "b.go"))
	// w1 is left for w2: its file is saved, the file of w2 (which was not shown) is not
	a.m.Activate(w2.Tab())
	a.waitFor("the save of the tab that was left", func() bool { return len(dirtyDocs(w1)) == 0 })
	if got := dirtyDocs(w2); !slices.Equal(got, []string{"b.go"}) {
		t.Errorf("the second tab has %q dirty", got)
	}
	// and the window losing the focus saves the tab that is shown, only
	a.ts.SetViewFocus(0, false)
	a.waitFor("the save of the shown tab", func() bool { return len(dirtyDocs(w2)) == 0 })
}

func TestOpeningAndClosingTenTabsLeavesNoGoroutinesBehind(t *testing.T) {
	env := newMultiEnv(t, nil)
	base := env.folder("base", "a.go")
	var folders []string
	for i := range 10 {
		folders = append(folders, env.folder("f"+string(rune('a'+i)), "x.go"))
	}
	a := env.run(Options{Folder: base})
	count := func() int { return runtime.NumGoroutine() }
	// let what the first tab started settle, then count
	a.settle()
	before := count()
	for _, f := range folders {
		if _, err := a.m.Open(WorkspaceSpec{Folder: f}); err != nil {
			t.Fatal(err)
		}
		a.settle()
	}
	if a.m.Count() != 11 {
		t.Fatalf("%d tabs", a.m.Count())
	}
	for _, w := range a.m.All()[1:] {
		a.m.Close(w.Tab(), nil)
		a.settle()
	}
	a.waitFor("the tabs to go", func() bool { return a.m.Count() == 1 })
	deadline := time.Now().Add(10 * time.Second)
	for count() > before+2 && time.Now().Before(deadline) {
		a.ts.Pump(10 * time.Millisecond)
		time.Sleep(10 * time.Millisecond)
	}
	if after := count(); after > before+2 {
		t.Errorf("%d goroutines before opening 10 tabs, %d after closing them", before, after)
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestDevActivateWindowTab(t *testing.T) {
	a, f := tabs3(t, nil)
	w3 := a.workspaceOf(f[2])
	if err := a.command(w3, "dev.activateWindowTab", map[string]any{"index": 0.0}); err != nil {
		t.Fatal(err)
	}
	if a.m.Active().Folder != f[0] {
		t.Errorf("active %q", a.m.Active().Folder)
	}
	if err := a.command(w3, "dev.activateWindowTab", 1.0); err != nil || a.m.Active().Folder != f[1] {
		t.Errorf("a number: %v, active %q", err, a.m.Active().Folder)
	}
	// a place with no tab
	err := a.command(w3, "dev.activateWindowTab", map[string]any{"index": 7.0})
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "DevActivateWindowTabMissing" {
		t.Errorf("error %v", err)
	}
	for _, args := range []any{nil, "x", -1.0, 1.5, map[string]any{}, map[string]any{"index": "1"}} {
		err := a.command(w3, "dev.activateWindowTab", args)
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("parseActivateWindowTab.DevActivateWindowTabInvalid") {
			t.Errorf("%v: error %v", args, err)
		}
	}
	if _, err := parseActivateWindowTab(2); err != nil {
		t.Errorf("an int: %v", err)
	}
	if _, err := parseActivateWindowTab(-2); err == nil {
		t.Error("a negative int")
	}
}

func TestAWorkspaceThatNoTabShowsCannotBeReplaced(t *testing.T) {
	win := newTestWorkspace(t, "")
	_, err := win.Process.Workspaces.replaceWorkspace(win, "", "")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Workspaces.replaceWorkspace.WorkspaceNotInATab") {
		t.Errorf("error %v", err)
	}
}

func TestTwoTabsUseTwoProfiles(t *testing.T) {
	env := newMultiEnv(t, nil)
	workProfile(t, env)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two, Profile: "Work"}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	if w1.ProfileName() != "Default" || w2.ProfileName() != "Work" {
		t.Fatalf("profiles %s and %s", w1.ProfileName(), w2.ProfileName())
	}
	if w1.Themes == w2.Themes || w1.Settings == w2.Settings {
		t.Error("the tabs share what is the profile's")
	}
	// each tab's state is read from the folder of its profile, and the list of tabs says which
	g, _ := a.p.State.LoadGlobal(a.ctx)
	if len(g.WorkspaceTabs) != 2 || g.WorkspaceTabs[0].Profile != "" || g.WorkspaceTabs[1].Profile != "Work" {
		t.Errorf("tabs %+v", g.WorkspaceTabs)
	}
	// switching tabs does not change a profile, and a tab switches its own
	a.m.Activate(w1.Tab())
	a.settle()
	if w1.ProfileName() != "Default" || w2.ProfileName() != "Work" {
		t.Error("the profiles changed with the active tab")
	}
	if err := a.command(w1, "dev.switchProfile", "Work"); err != nil {
		t.Fatal(err)
	}
	if w1.ProfileName() != "Work" || w2.ProfileName() != "Work" {
		t.Errorf("profiles %s and %s", w1.ProfileName(), w2.ProfileName())
	}
}

func TestOpenFolderInNewTabBeforeTheWindowShowsIt(t *testing.T) {
	// a tab can be added before the host is mounted (the plan of the start does it)
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	p := newTestProcess(ProcessOptions{Paths: env.paths})
	p.Workspaces.ctx = t.Context()
	w, err := p.Workspaces.Open(WorkspaceSpec{Folder: one})
	if err != nil || w.Folder != one || p.Workspaces.Count() != 1 || p.Workspaces.Active() != w {
		t.Fatalf("Open = %v, %v", w, err)
	}
	if w2, err := p.Workspaces.Open(WorkspaceSpec{Folder: two}); err != nil || p.Workspaces.Count() != 2 || p.Workspaces.Active() != w2 {
		t.Fatalf("second Open = %v, %v", w2, err)
	}
	if got := p.Workspaces.IndexOf(w.Tab()); got != 0 {
		t.Errorf("index %d", got)
	}
}

func TestTheWindowGeometryIsKeptOnceInTheGlobalState(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: env.folder("two", "b.go")}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	env.clock.Advance(2 * storage.DefaultStateDelay)
	a.workspaceOf(one).RequestClose(CloseReasonQuit)
	a.waitFor("the quit", func() bool { return a.quits == 1 })
	g, _ := a.p.State.LoadGlobal(a.ctx)
	if g.WindowGeometry == nil || !g.WindowGeometry.Valid() {
		t.Fatalf("window geometry %+v", g.WindowGeometry)
	}
	if g.Geometry != nil {
		t.Errorf("the geometry of an older Emetos is written: %+v", g.Geometry)
	}
	// the next start asks for that size
	b := env.run(Options{})
	cfg := startupWindowConfig(b.m.All()[0], nil)
	if cfg.Size.Width != g.WindowGeometry.Width || cfg.Size.Height != g.WindowGeometry.Height {
		t.Errorf("window config %v, want %+v", cfg.Size, g.WindowGeometry)
	}
}

func TestAnOldStateFileRestoresTheWindowsAsTabs(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	idOne, _ := storage.WindowID(one)
	idTwo, _ := storage.WindowID(two)
	dir := filepath.Join(env.paths.StateHome, "emetos")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	text := `{"version":1,"emptyWindowId":"00000000000000aa","lastWindow":{"folder":"` + two + `"},
		"geometry":{"width":1111,"height":777},
		"openWindows":[{"id":"` + idOne + `","folder":"` + one + `"},{"id":"00000000000000aa"},{"id":"` + idTwo + `","folder":"` + two + `"}],
		"recent":{}}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	a := env.run(Options{})
	if got := a.folders(); !slices.Equal(got, []string{one, "", two}) {
		t.Fatalf("tabs %q", got)
	}
	// the window that was last open is the shown tab
	if a.m.Active().Folder != two {
		t.Errorf("active %q", a.m.Active().Folder)
	}
	cfg := startupWindowConfig(a.m.All()[0], nil)
	if cfg.Size.Width != 1111 || cfg.Size.Height != 777 {
		t.Errorf("the geometry of the last window is not the window's: %v", cfg.Size)
	}
	// and the state written from then on has the tabs
	if err := a.p.State.Flush(a.ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"workspaceTabs"`) || strings.Contains(string(raw), `"openWindows"`) {
		t.Errorf("state.json: %s", raw)
	}
}
