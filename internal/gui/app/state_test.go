package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// stateEnv is a project folder and the isolated folders of the user, over which windows are opened one after
// the other, as the runs of the program are.
type stateEnv struct {
	t     *testing.T
	paths storage.Paths
	root  string
	clock *storagetest.Clock
	// size is the size of the window, and prepare, when set, configures the tester before the window is shown.
	size    ui.Size
	prepare func(ts *gluttertest.Tester)
}

// stateFiles are the files of the project of the tests; the long ones can be scrolled.
var stateFiles = []string{"main.go", "src/a.go", "src/deep/b.go", "docs/c.md", "docs/more/d.md", "e.txt"}

func newStateEnv(t *testing.T, settingsJSON map[string]any) *stateEnv {
	t.Helper()
	paths := testutil.Isolate(t)
	// closing asks about unsaved changes unless a test turns hot exit on
	extra := map[string]any{"editor.fontSize": 14, "files.hotExit": "off"}
	for k, v := range settingsJSON {
		extra[k] = v
	}
	testutil.Settings(t, paths, extra)
	// the project is in the home folder, so that the recently opened list writes its paths as "~/projects/emetos"
	root := filepath.Join(os.Getenv("HOME"), "projects", "emetos")
	for _, f := range stateFiles {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(longText(300)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &stateEnv{t: t, paths: paths, root: root, clock: storagetest.NewClock(), size: testWindowSize}
}

// folder makes another folder of the home folder, with files in it.
func (e *stateEnv) folder(name string, files ...string) string {
	e.t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), "projects", name)
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			e.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package x\n// "+f+"\n"), 0o644); err != nil {
			e.t.Fatal(err)
		}
	}
	return dir
}

func (e *stateEnv) path(rel string) string { return filepath.Join(e.root, filepath.FromSlash(rel)) }

// run is one run of the program: its window, shown in a tester.
type stateRun struct {
	env      *stateEnv
	win      *Window
	ts       *gluttertest.Tester
	quits    int
	geometry *storage.Geometry
}

// start opens a window over the folder (the project when folder is empty and withFolder is true) the way Run does:
// the saved state is read, then the files of the command line are opened, then the window is shown.
func (e *stateEnv) start(folder string, files ...string) *stateRun {
	e.t.Helper()
	return e.startWith(folder, ProcessOptions{}, files...)
}

func (e *stateEnv) startWith(folder string, opts ProcessOptions, files ...string) *stateRun {
	e.t.Helper()
	opts.Paths = e.paths
	if opts.StateAfter == nil {
		opts.StateAfter = e.clock.AfterFunc
	}
	ctx := context.Background()
	process := newTestProcess(opts)
	win, err := NewWindow(ctx, process, folder)
	if err != nil {
		e.t.Fatal(err)
	}
	r := &stateRun{env: e, win: win}
	win.Quit = func() { r.quits++ }
	r.geometry = win.PrepareRestore(ctx)
	var docs []*document.Document
	for _, f := range files {
		doc, err := win.Documents.OpenFile(ctx, f, document.NewOptions())
		if err != nil {
			e.t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	r.ts = testutil.New(e.t, e.size)
	if e.prepare != nil {
		e.prepare(r.ts)
	}
	r.ts.PumpWidget(newAppDocs(ctx, win, docs, nil, ""))
	r.pump()
	if os.Getenv("DEBUGSTATE") != "" {
		for _, err := range r.ts.TakeErrors() {
			e.t.Logf("%+v", err)
		}
	}
	return r
}

func (r *stateRun) pump() {
	for range 6 {
		r.ts.Pump(20 * time.Millisecond)
		time.Sleep(2 * time.Millisecond)
	}
}

func (r *stateRun) shell() *shellState {
	return r.ts.Element(gluttertest.ByType[*shell]()).(*widgets.StatefulElement).State().(*shellState)
}

func (r *stateRun) command(id string, args any) {
	r.env.t.Helper()
	err := r.win.Process.Commands.Run(command.Context{Ctx: context.Background(), Keys: r.win.Keys, Source: command.SourceAPI}, id, args)
	if err != nil {
		r.env.t.Fatalf("%s: %v", id, err)
	}
	r.pump()
}

func (r *stateRun) open(path string, group int) {
	r.env.t.Helper()
	arg := map[string]any{"path": path}
	if group > 0 {
		arg["group"] = float64(group)
	}
	r.command("dev.openFile", arg)
}

// close closes the window the way the user does and shows that the program would end; the window goes.
func (r *stateRun) close() {
	r.env.t.Helper()
	before := r.quits
	r.win.RequestClose(CloseReasonQuit)
	r.pump()
	if r.quits != before+1 {
		r.env.t.Fatalf("the window did not close: %d quits", r.quits)
	}
	// the window is gone: its widgets are unmounted, as they are when the program ends
	r.ts.PumpWidget(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{}))
}

// layout lists what the groups hold: each group as "id: tab, tab*" with the active tab starred, a preview in
// brackets and a sticky tab marked with a pin.
func (r *stateRun) layout() []string {
	s := r.shell()
	var out []string
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		var tabs []string
		for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
			name := in.Name()
			switch {
			case g.Model.IsSticky(in):
				name = "^" + name
			case !g.Model.IsPinned(in):
				name = "[" + name + "]"
			}
			if in == g.Model.ActiveEditor() {
				name += "*"
			}
			tabs = append(tabs, name)
		}
		active := ""
		if g == s.group.grid.Active() {
			active = "!"
		}
		out = append(out, fmt.Sprintf("%d%s: %s", g.ID(), active, strings.Join(tabs, ", ")))
	}
	return out
}

// editorOf is the editor of the tab of a file in a group.
func (r *stateRun) editorOf(group int, name string) *textEditor {
	r.env.t.Helper()
	s := r.shell()
	g := s.group.grid.Group(group)
	if g == nil {
		r.env.t.Fatalf("no group %d", group)
	}
	for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
		if in.Name() == name {
			return s.group.editors[in]
		}
	}
	r.env.t.Fatalf("no tab %s in group %d: %v", name, group, r.layout())
	return nil
}

// expanded are the folders the Explorer has expanded, relative to the project.
func (r *stateRun) expanded() []string {
	view := r.shell().explorer.view
	if view == nil {
		return nil
	}
	return storage.ExpandedRelative(r.win.Folder, view.Tree().ExpandedKeys())
}

// savedWindow reads the state file of the window straight from the disk.
func (e *stateEnv) savedWindow(folder string) (storage.WindowState, bool) {
	e.t.Helper()
	id, err := storage.WindowID(folder)
	if err != nil {
		e.t.Fatal(err)
	}
	path, err := e.paths.WindowStatePath(storage.DefaultProfile, id)
	if err != nil {
		e.t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return storage.WindowState{}, false
	}
	if err != nil {
		e.t.Fatal(err)
	}
	var st storage.WindowState
	if err := json.Unmarshal(data, &st); err != nil {
		e.t.Fatal(err)
	}
	return st, true
}

func (e *stateEnv) writeWindowState(folder, content string) {
	e.t.Helper()
	id, _ := storage.WindowID(folder)
	path, err := e.paths.WindowStatePath(storage.DefaultProfile, id)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

func TestEverythingTheWindowHadComesBackAfterARestart(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)

	// three files in two groups: main.go and src/a.go in the first, docs/c.md beside them
	first.open(env.path("main.go"), 0)
	first.open(env.path("src/a.go"), 0)
	first.command("workbench.action.splitEditorRight", nil)
	first.open(env.path("docs/c.md"), 0)
	// the first group has a pinned (sticky) tab and a preview
	first.command("dev.openFile", map[string]any{"path": env.path("e.txt"), "group": float64(1), "preview": true})
	wantLayout := first.layout()
	if len(wantLayout) != 2 {
		t.Fatalf("two groups were expected: %v", wantLayout)
	}

	// the cursors, the scroll positions and the folds
	a := first.editorOf(1, "a.go")
	a.view.Cursors().SetSelections([]buffer.Selection{
		{Anchor: buffer.Position{Line: 5, Col: 2}, Active: buffer.Position{Line: 7, Col: 4}},
		buffer.NewCursor(buffer.Position{Line: 20, Col: 0}),
	})
	a.view.SetScrollTop(400)
	c := first.editorOf(2, "c.md")
	c.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 33, Col: 3})})
	c.view.SetScrollTop(250)
	wantA, wantC := a.view.ViewState(), c.view.ViewState()

	// the Explorer
	first.pump()
	view := first.shell().explorer.view
	view.Tree().Expand(view.KeyOf(env.path("docs")))
	first.pump()
	// the layout: no side bar, a panel of another size
	first.win.Layout.SetSideBarVisible(false)
	first.win.Layout.SetPanelVisible(true)
	first.pump()
	first.win.Layout.SetPanelSize(222)
	first.pump()
	wantLayoutState := first.win.Layout.State()
	if wantLayoutState.PanelHeight != 222 {
		t.Fatalf("layout %+v", wantLayoutState)
	}
	wantExpanded := first.expanded()
	if !slices.Contains(wantExpanded, "docs") || !slices.Contains(wantExpanded, "src") {
		t.Fatalf("expanded folders %v", wantExpanded)
	}

	first.close()
	if _, ok := env.savedWindow(env.root); !ok {
		t.Fatal("closing the window wrote no state")
	}

	// a new run over the same folders
	second := env.start(env.root)
	if got := second.layout(); !slices.Equal(got, wantLayout) {
		t.Errorf("groups %v, want %v", got, wantLayout)
	}
	if got := second.win.Layout.State(); got != wantLayoutState {
		t.Errorf("layout %+v, want %+v", got, wantLayoutState)
	}
	if got := second.expanded(); !slices.Equal(got, wantExpanded) {
		t.Errorf("expanded %v, want %v", got, wantExpanded)
	}
	a2, c2 := second.editorOf(1, "a.go"), second.editorOf(2, "c.md")
	for _, tc := range []struct {
		name      string
		got, want editor.ViewState
	}{{"a.go", a2.view.ViewState(), wantA}, {"c.md", c2.view.ViewState(), wantC}} {
		if !slices.Equal(tc.got.Selections, tc.want.Selections) || tc.got.ScrollTop != tc.want.ScrollTop {
			t.Errorf("%s: selections %v scroll %v, want %v and %v", tc.name, tc.got.Selections, tc.got.ScrollTop, tc.want.Selections, tc.want.ScrollTop)
		}
	}
	if got := second.win.Recent.List(); len(got) == 0 {
		t.Error("the files of the quick open were not restored")
	}
	if errs := second.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
	if n := len(second.win.Process.Notify.Items()); n != 0 {
		t.Errorf("%d notifications after a restore with nothing missing", n)
	}
}

func TestPinnedPreviewAndActiveEditorsComeBack(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.open(env.path("src/a.go"), 0)
	first.command("workbench.action.pinEditor", nil) // a.go is sticky
	first.open(env.path("e.txt"), 0)
	first.command("dev.openFile", map[string]any{"path": env.path("docs/c.md"), "preview": true})
	first.command("workbench.action.splitEditorDown", nil)
	first.open(env.path("src/deep/b.go"), 0)
	first.command("workbench.action.focusFirstEditorGroup", nil)
	want := first.layout()
	if len(want) != 2 || !strings.Contains(want[0], "^a.go") || !strings.Contains(want[0], "[c.md]") || !strings.HasPrefix(want[0], "1!") {
		t.Fatalf("groups %v", want)
	}
	first.close()
	second := env.start(env.root)
	if got := second.layout(); !slices.Equal(got, want) {
		t.Errorf("groups %v, want %v", got, want)
	}
	// the shape of the grid (a split down) and the order of use come back too
	g := second.shell().group.grid
	if g.Tree().Orientation != editorgroup.Vertical {
		t.Errorf("the grid is %v, want a split down", g.Tree().Orientation)
	}
	if mru := g.Groups(editorgroup.MostRecentlyActiveGroups); len(mru) != 2 || mru[0].ID() != 1 {
		t.Errorf("most recently active group %v", mru)
	}
}

func TestEditorsWhoseFilesAreGoneAreDroppedWithOneNotice(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.open(env.path("src/a.go"), 0)
	first.command("workbench.action.splitEditorRight", nil)
	first.open(env.path("docs/c.md"), 0)
	first.open(env.path("docs/more/d.md"), 0) // the active editor of the second group
	// the split put a copy of a.go in the second group: three editors have their file deleted below
	first.close()

	// d.md and a.go are deleted, a folder is gone with the file below it
	for _, f := range []string{"docs/more/d.md", "src/a.go"} {
		if err := os.Remove(env.path(f)); err != nil {
			t.Fatal(err)
		}
	}
	second := env.start(env.root)
	// the editors of the dropped files are gone; the second group is the active one and shows c.md, the editor
	// used before d.md
	if got, want := second.layout(), []string{"1: main.go*", "2!: c.md*"}; !slices.Equal(got, want) {
		t.Errorf("groups %v, want %v", got, want)
	}
	items := second.win.Process.Notify.Items()
	if len(items) != 1 || items[0].Message != "3 editors could not be restored because their files no longer exist." {
		t.Fatalf("notifications %+v", items)
	}
	if items[0].Severity != notify.Info {
		t.Errorf("severity %v", items[0].Severity)
	}
	// the dropped editors keep nothing open: the documents are closed
	if n := len(second.win.Documents.All()); n != 2 {
		t.Errorf("%d documents are open, want 2 (main.go, c.md)", n)
	}
}

func TestEverythingGoneStartsWithAnEmptyGroup(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.close()
	if err := os.Remove(env.path("main.go")); err != nil {
		t.Fatal(err)
	}
	second := env.start(env.root)
	if got := second.layout(); !slices.Equal(got, []string{"1!: "}) {
		t.Errorf("groups %v", got)
	}
	items := second.win.Process.Notify.Items()
	if len(items) != 1 || items[0].Message != "1 editor could not be restored because its file no longer exists." {
		t.Errorf("notifications %+v", items)
	}
	if errs := second.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestDroppedEditorsNoticeCounts(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{1, "1 editor could not be restored because its file no longer exists."},
		{2, "2 editors could not be restored because their files no longer exist."},
		{11, "11 editors could not be restored because their files no longer exist."},
	}
	for _, tc := range tests {
		if got := DroppedEditorsNotice(tc.n); got != tc.want {
			t.Errorf("DroppedEditorsNotice(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestAFileOfTheCommandLineOpensOnTopOfTheRestoredEditors(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.open(env.path("src/a.go"), 0)
	first.close()

	second := env.start(env.root, env.path("e.txt"))
	if got, want := second.layout(), []string{"1!: main.go, a.go, e.txt*"}; !slices.Equal(got, want) {
		t.Errorf("groups %v, want %v", got, want)
	}
	// a file that is open already is just shown
	third := env.start(env.root, env.path("main.go"))
	if got := third.layout(); len(got) != 1 || !strings.Contains(got[0], "main.go*") || strings.Count(got[0], "main.go") != 1 {
		t.Errorf("groups %v", got)
	}
}

func TestRestoreWindowsNoneRestoresNothing(t *testing.T) {
	env := newStateEnv(t, map[string]any{"window.restoreWindows": "none"})
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.win.Layout.SetSideBarVisible(false)
	first.close()
	if _, ok := env.savedWindow(env.root); !ok {
		t.Error("the state is kept even when it is not restored, so that the setting can be turned back")
	}
	second := env.start(env.root)
	if got := second.layout(); !slices.Equal(got, []string{"1!: "}) {
		t.Errorf("groups %v", got)
	}
	if !second.win.Layout.State().SideBarVisible {
		t.Error("the layout was restored")
	}
	if second.geometry != nil {
		t.Errorf("geometry %+v", second.geometry)
	}
	// every other value restores
	for _, mode := range []string{"preserve", "all", "folders", "one"} {
		if err := second.win.Settings.Update(context.Background(), "window.restoreWindows", mode, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
		if got := restoreMode(second.win.Settings); got != mode {
			t.Fatalf("mode %q", got)
		}
		third := env.start(env.root)
		if got := third.layout(); !strings.Contains(got[0], "main.go") {
			t.Errorf("window.restoreWindows = %s: groups %v", mode, got)
		}
		third.close()
	}
}

func TestAStateFileThatCannotBeUsedStartsCleanAndIsReplaced(t *testing.T) {
	for name, content := range map[string]string{
		"corrupt": "{ this is not json",
		"newer":   `{"version": 99, "folder": "/x"}`,
		"empty":   "",
	} {
		t.Run(name, func(t *testing.T) {
			env := newStateEnv(t, nil)
			env.writeWindowState(env.root, content)
			first := env.start(env.root)
			if got := first.layout(); !slices.Equal(got, []string{"1!: "}) {
				t.Fatalf("groups %v", got)
			}
			if n := len(first.win.Process.Notify.Items()); n != 0 {
				t.Errorf("%d notifications for an unusable file", n)
			}
			first.open(env.path("main.go"), 0)
			first.close()
			st, ok := env.savedWindow(env.root)
			if !ok || st.Version != storage.StateVersion || st.Grid == nil {
				t.Fatalf("the file was not replaced: %+v, %v", st, ok)
			}
			second := env.start(env.root)
			if got := second.layout(); len(got) != 1 || !strings.Contains(got[0], "main.go") {
				t.Errorf("groups %v", got)
			}
		})
	}

	t.Run("the global state", func(t *testing.T) {
		env := newStateEnv(t, nil)
		path, _ := env.paths.GlobalStatePath(storage.DefaultProfile)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("}{"), 0o600); err != nil {
			t.Fatal(err)
		}
		first := env.start(env.root)
		first.open(env.path("main.go"), 0)
		first.close()
		g, err := first.win.Process.State.LoadGlobal(context.Background())
		if err != nil || !slices.Contains(g.Recent.Folders, env.root) || !slices.Contains(g.Recent.Files, env.path("main.go")) {
			t.Errorf("global state %+v, %v", g, err)
		}
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), `"version":1`) {
			t.Errorf("the global file was not replaced: %s, %v", data, err)
		}
	})
}

func TestTheStateIsFlushedWhenTheWindowCloses(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	// no time passes: neither the snapshot nor the store's delay has run
	first.editorOf(1, "main.go").view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 9, Col: 3})})
	first.editorOf(1, "main.go").view.SetScrollTop(120)
	first.editorOf(1, "main.go").view.SelectionsChanged()
	if _, ok := env.savedWindow(env.root); ok {
		t.Fatal("the file was written before anything asked for it")
	}
	first.close()
	st, ok := env.savedWindow(env.root)
	if !ok || st.Grid == nil {
		t.Fatalf("saved %+v, %v", st, ok)
	}
	var vs editor.ViewState
	if err := json.Unmarshal(st.Grid.Root[0].Model.Editors[0].State, &vs); err != nil {
		t.Fatal(err)
	}
	if want := (buffer.Selection{Anchor: buffer.Position{Line: 9, Col: 3}, Active: buffer.Position{Line: 9, Col: 3}}); len(vs.Selections) != 1 || vs.Selections[0] != want || vs.ScrollTop != 120 {
		t.Errorf("saved view state %+v", vs)
	}
}

func TestChangesAreEncodedAndWrittenAfterTheirDelays(t *testing.T) {
	var writes []string
	env := newStateEnv(t, nil)
	run := env.startWith(env.root, ProcessOptions{StateWriteFile: func(path string, data []byte) error {
		writes = append(writes, filepath.Base(path))
		return storage.WriteFileAtomic(path, data)
	}})
	run.open(env.path("main.go"), 0)
	run.ts.Pump(stateSnapshotDelay + 50*time.Millisecond)
	env.clock.Advance(2 * storage.DefaultStateDelay)
	writes = nil

	// a burst of changes: one snapshot after the snapshot delay, one write after the store's delay
	ed := run.editorOf(1, "main.go")
	for i := range 10 {
		ed.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: i + 1})})
		ed.view.SelectionsChanged()
	}
	run.ts.Pump(100 * time.Millisecond)
	env.clock.Advance(2 * storage.DefaultStateDelay)
	if len(writes) != 0 {
		t.Fatalf("written before the snapshot: %v", writes)
	}
	run.ts.Pump(stateSnapshotDelay)
	if len(writes) != 0 {
		t.Fatalf("written before the store's delay: %v", writes)
	}
	env.clock.Advance(storage.DefaultStateDelay)
	if len(writes) != 1 {
		t.Fatalf("writes after the burst: %v", writes)
	}
	st, ok := env.savedWindow(env.root)
	if !ok {
		t.Fatal("no file")
	}
	var vs editor.ViewState
	if err := json.Unmarshal(st.Grid.Root[0].Model.Editors[0].State, &vs); err != nil || vs.Selections[0].Active.Line != 10 {
		t.Errorf("saved view state %+v, %v", vs, err)
	}
	// nothing changed: nothing is written
	run.ts.Pump(2 * time.Second)
	env.clock.Advance(5 * storage.DefaultStateDelay)
	if len(writes) != 1 {
		t.Errorf("writes %v", writes)
	}
}

func TestEachKindOfChangeSavesTheState(t *testing.T) {
	changes := []struct {
		name   string
		change func(r *stateRun)
		check  func(t *testing.T, st storage.WindowState)
	}{
		{"an editor opens", func(r *stateRun) { r.open(r.env.path("e.txt"), 0) }, func(t *testing.T, st storage.WindowState) {
			if n := len(st.Grid.Root[0].Model.Editors); n != 2 {
				t.Errorf("%d editors", n)
			}
		}},
		{"an editor closes", func(r *stateRun) { r.command("workbench.action.closeActiveEditor", nil) }, func(t *testing.T, st storage.WindowState) {
			if n := len(st.Grid.Root[0].Model.Editors); n != 0 {
				t.Errorf("%d editors", n)
			}
		}},
		{"a split", func(r *stateRun) { r.command("workbench.action.splitEditorRight", nil) }, func(t *testing.T, st storage.WindowState) {
			if len(st.Grid.Root) != 2 {
				t.Errorf("%d groups", len(st.Grid.Root))
			}
		}},
		{"the layout", func(r *stateRun) { r.win.Layout.SetSideBarVisible(false) }, func(t *testing.T, st storage.WindowState) {
			if !strings.Contains(string(st.Layout), `"sideBarVisible":false`) {
				t.Errorf("layout %s", st.Layout)
			}
		}},
		{"an expanded folder", func(r *stateRun) {
			view := r.shell().explorer.view
			view.Tree().Expand(view.KeyOf(r.env.path("docs")))
		}, func(t *testing.T, st storage.WindowState) {
			if st.Explorer == nil || !slices.Contains(st.Explorer.Expanded, "docs") {
				t.Errorf("explorer %+v", st.Explorer)
			}
		}},
		{"the quick open list", func(r *stateRun) { r.win.Recent.Add(r.env.path("e.txt")) }, func(t *testing.T, st storage.WindowState) {
			if len(st.Recent) == 0 || st.Recent[0] != filepath.Join(filepath.Dir(st.Recent[0]), "e.txt") {
				t.Errorf("recent %v", st.Recent)
			}
		}},
		{"the size of the window", func(r *stateRun) {
			r.shell().readGeometry(&fakeWindow{size: ui.Size{Width: 1000, Height: 700}})
		}, func(t *testing.T, st storage.WindowState) {
			if st.Geometry == nil || st.Geometry.Width != 1000 || st.Geometry.Height != 700 {
				t.Errorf("geometry %+v", st.Geometry)
			}
		}},
	}
	for _, tc := range changes {
		t.Run(tc.name, func(t *testing.T) {
			env := newStateEnv(t, nil)
			run := env.start(env.root, env.path("main.go"))
			tc.change(run)
			run.ts.Pump(20 * time.Millisecond) // a change of the layout reaches the shell a frame later
			run.ts.Pump(stateSnapshotDelay + 50*time.Millisecond)
			env.clock.Advance(storage.DefaultStateDelay)
			st, ok := env.savedWindow(env.root)
			if !ok {
				t.Fatal("the state was not written")
			}
			tc.check(t, st)
		})
	}
}

// fakeWindow is a window controller that tells its size and state.
type fakeWindow struct {
	foundation.ChangeNotifier
	size                  ui.Size
	maximized, fullscreen bool
}

func (f *fakeWindow) ContentSize() ui.Size { return f.size }
func (f *fakeWindow) IsMaximized() bool    { return f.maximized }
func (f *fakeWindow) IsFullscreen() bool   { return f.fullscreen }

func TestTheGeometryOfTheWindowIsKeptWhileItIsNeitherMaximizedNorFullScreen(t *testing.T) {
	env := newStateEnv(t, nil)
	run := env.start(env.root)
	s := run.shell()
	win := &fakeWindow{ChangeNotifier: *foundation.NewChangeNotifier(), size: ui.Size{Width: 1100, Height: 650}}
	s.state.source = nil
	s.trackWindow(win)
	if got := s.state.geometry; got.Width != 1100 || got.Height != 650 || got.Maximized || got.Fullscreen {
		t.Fatalf("geometry %+v", got)
	}
	// maximized: the size is the screen's, which the window does not come back to
	win.size, win.maximized = ui.Size{Width: 1920, Height: 1080}, true
	win.NotifyListeners()
	if got := s.state.geometry; got.Width != 1100 || !got.Maximized {
		t.Errorf("maximized: %+v", got)
	}
	win.fullscreen = true
	win.NotifyListeners()
	if got := s.state.geometry; got.Width != 1100 || !got.Fullscreen {
		t.Errorf("full screen: %+v", got)
	}
	win.maximized, win.fullscreen, win.size = false, false, ui.Size{Width: 900, Height: 600}
	win.NotifyListeners()
	if got := s.state.geometry; got != (storage.Geometry{Width: 900, Height: 600}) {
		t.Errorf("restored: %+v", got)
	}
	// the listener is gone with the shell
	run.close()
	if win.HasListeners() {
		t.Error("the window is still followed")
	}
}

func TestTheFirstWindowIsOpenedAsTheSavedStateSays(t *testing.T) {
	tests := []struct {
		name       string
		settings   map[string]any
		saved      *storage.Geometry
		global     *storage.Geometry
		wantSize   ui.Size
		wantState  widgets.WindowState
		restoreOff bool
	}{
		{name: "nothing saved", wantSize: ui.Size{Width: windowWidth, Height: windowHeight}},
		{name: "the size of the window", saved: &storage.Geometry{Width: 1000, Height: 700}, wantSize: ui.Size{Width: 1000, Height: 700}},
		{name: "maximized", saved: &storage.Geometry{Width: 1000, Height: 700, Maximized: true}, wantSize: ui.Size{Width: 1000, Height: 700}, wantState: widgets.WindowStateMaximized},
		{name: "full screen is not restored by default", saved: &storage.Geometry{Width: 1000, Height: 700, Fullscreen: true}, wantSize: ui.Size{Width: 1000, Height: 700}},
		{name: "full screen with window.restoreFullscreen", settings: map[string]any{"window.restoreFullscreen": true},
			saved: &storage.Geometry{Width: 1000, Height: 700, Fullscreen: true}, wantSize: ui.Size{Width: 1000, Height: 700}, wantState: widgets.WindowStateFullscreen},
		{name: "the size of the last window for a window with none", global: &storage.Geometry{Width: 800, Height: 500}, wantSize: ui.Size{Width: 800, Height: 500}},
		{name: "an invalid size", saved: &storage.Geometry{Width: -1, Height: 700}, wantSize: ui.Size{Width: windowWidth, Height: windowHeight}},
		{name: "window.restoreWindows none", settings: map[string]any{"window.restoreWindows": "none"},
			saved: &storage.Geometry{Width: 1000, Height: 700, Maximized: true}, wantSize: ui.Size{Width: 1000, Height: 700}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newStateEnv(t, tc.settings)
			if tc.global != nil {
				store := newTestProcess(ProcessOptions{Paths: env.paths}).State
				if err := store.UpdateGlobal(context.Background(), func(g *storage.GlobalState) bool { g.Geometry = tc.global; return true }); err != nil {
					t.Fatal(err)
				}
				if err := store.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			win, err := NewWindow(context.Background(), newTestProcess(ProcessOptions{Paths: env.paths}), env.root)
			if err != nil {
				t.Fatal(err)
			}
			cfg := startupWindowConfig(win, tc.saved)
			// with window.restoreWindows none the size is the default one
			want := tc.wantSize
			if tc.name == "window.restoreWindows none" {
				want = ui.Size{Width: 1000, Height: 700}
			}
			if cfg.Size != want || cfg.State != tc.wantState || cfg.Title != Title {
				t.Errorf("config size %v state %v, want %v and %v", cfg.Size, cfg.State, want, tc.wantState)
			}
		})
	}
}

func TestTheFolderOfTheLastWindowOpensAgain(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.close()
	other := t.TempDir()

	tests := []struct {
		name string
		opts Options
		mode string
		want string
	}{
		{"nothing on the command line", Options{}, "all", env.root},
		{"preserve", Options{}, "preserve", env.root},
		{"folders", Options{}, "folders", env.root},
		{"one", Options{}, "one", env.root},
		{"none", Options{}, "none", ""},
		{"a folder is the one that opens", Options{Folder: other}, "all", other},
		{"files open in the window without a folder", Options{Files: []string{env.path("main.go")}}, "all", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(env.paths.ConfigHome, "emetos", "settings.json"), []byte(`{"window.restoreWindows": "`+tc.mode+`"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			p := newTestProcess(ProcessOptions{Paths: env.paths})
			if got := p.startupFolder(context.Background(), tc.opts); got != tc.want {
				t.Errorf("folder %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("a folder that is gone", func(t *testing.T) {
		gone := t.TempDir()
		run := env.start(gone)
		run.close()
		if err := os.Remove(gone); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(env.paths.ConfigHome, "emetos", "settings.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		p := newTestProcess(ProcessOptions{Paths: env.paths})
		if got := p.startupFolder(context.Background(), Options{}); got != "" {
			t.Errorf("folder %q", got)
		}
	})

	t.Run("the last window had no folder", func(t *testing.T) {
		run := env.start("")
		run.close()
		p := newTestProcess(ProcessOptions{Paths: env.paths})
		if got := p.startupFolder(context.Background(), Options{}); got != "" {
			t.Errorf("folder %q", got)
		}
	})
}

func TestAWindowWithoutAFolderKeepsItsEditorsUnderItsOwnId(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start("")
	id := first.win.StateID()
	if id == "" {
		t.Fatal("the window has no state id")
	}
	first.open(env.path("main.go"), 0)
	first.close()
	second := env.start("")
	if second.win.StateID() != id {
		t.Errorf("the id changed: %q, was %q", second.win.StateID(), id)
	}
	if got := second.layout(); len(got) != 1 || !strings.Contains(got[0], "main.go") {
		t.Errorf("groups %v", got)
	}
	// and it is not the state of the folder
	folder := env.start(env.root)
	if got := folder.layout(); !slices.Equal(got, []string{"1!: "}) {
		t.Errorf("groups of the folder %v", got)
	}
	if folder.win.StateID() == id {
		t.Error("the folder has the id of the empty window")
	}
}

func TestExpandedFoldersThatAreGoneAreDropped(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("docs/more/d.md"), 0)
	first.open(env.path("src/deep/b.go"), 0)
	want := first.expanded()
	for _, dir := range []string{"docs", "docs/more", "src", "src/deep"} {
		if !slices.Contains(want, dir) {
			t.Fatalf("expanded %v", want)
		}
	}
	first.close()
	if err := os.RemoveAll(env.path("docs/more")); err != nil {
		t.Fatal(err)
	}
	second := env.start(env.root)
	got := second.expanded()
	if slices.Contains(got, "docs/more") || !slices.Contains(got, "src/deep") || !slices.Contains(got, "docs") {
		t.Errorf("expanded %v", got)
	}
	// the expansion is not only in the model: the rows are there
	rows := second.shell().explorer.view.Tree().Rows()
	var keys []string
	for _, n := range rows {
		keys = append(keys, relativeKey(env.root, n.Key))
	}
	if !slices.Contains(keys, "src/deep/b.go") {
		t.Errorf("rows %v", keys)
	}
}

func relativeKey(root, key string) string {
	rel, err := filepath.Rel(root, key)
	if err != nil {
		return key
	}
	return filepath.ToSlash(rel)
}

func TestRestoreViewStateSetting(t *testing.T) {
	env := newStateEnv(t, map[string]any{"workbench.editor.restoreViewState": false})
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.editorOf(1, "main.go").view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 8, Col: 1})})
	first.close()
	second := env.start(env.root)
	if got := second.layout(); len(got) != 1 || !strings.Contains(got[0], "main.go") {
		t.Fatalf("groups %v", got)
	}
	if sel := second.editorOf(1, "main.go").view.Cursors().Selections(); len(sel) != 1 || sel[0] != (buffer.Selection{}) {
		t.Errorf("selections %v: the view state was restored with the setting off", sel)
	}
}

func TestAnUnknownSavedEditorIsDroppedSilently(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.close()
	st, _ := env.savedWindow(env.root)
	st.Grid.Root[0].Model.Editors = append(st.Grid.Root[0].Model.Editors, editorgroup.SerializedInput{TypeID: "emetos.future", Value: "x"})
	st.Grid.Root[0].Model.MRU = append(st.Grid.Root[0].Model.MRU, 1)
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	env.writeWindowState(env.root, string(data))
	second := env.start(env.root)
	if got := second.layout(); len(got) != 1 || !strings.Contains(got[0], "main.go") || strings.Contains(got[0], "x") {
		t.Errorf("groups %v", got)
	}
	if errs := second.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestASavedGridThatIsNotAGridStartsWithOneGroup(t *testing.T) {
	env := newStateEnv(t, nil)
	env.writeWindowState(env.root, `{"version":1,"grid":{"orientation":0,"root":[{"size":1,"groups":[{"size":1}]}],"active":1,"mru":[1],"nextId":2}}`)
	run := env.start(env.root)
	if got := run.layout(); !slices.Equal(got, []string{"1!: "}) {
		t.Errorf("groups %v", got)
	}
}

// ---- the recently opened folders and files ----

// recentLabels are the labels of the rows of File > Open Recent, with "-" for a separator, a dimmed row
// followed by "~", and the marks of the mnemonics taken out.
func (r *stateRun) recentLabels() []string {
	var out []string
	for _, it := range r.shell().menus.Items(command.MenubarRecentMenu, nil) {
		label := strings.ReplaceAll(it.Label, "&", "")
		switch {
		case it.Separator:
			label = "-"
		case it.Disabled:
			label += "~"
		}
		out = append(out, label)
	}
	return out
}

// recentRows are the rows of the Open Recent picker: "folders" and "files" for the headings, else the label and the
// description.
func (r *stateRun) pickerRows() []string {
	var out []string
	for _, row := range r.win.QuickInput.Picker().Rows() {
		if row.Separator != "" {
			out = append(out, "# "+row.Separator)
		}
		out = append(out, row.Item.Label+" | "+row.Item.Description)
	}
	return out
}

func (r *stateRun) global() storage.GlobalState {
	r.env.t.Helper()
	g, err := r.win.Process.State.LoadGlobal(context.Background())
	if err != nil {
		r.env.t.Fatal(err)
	}
	return g
}

// recentEnv is a project and another folder that were opened one after the other: the project is open, its main.go
// and src/a.go were opened last.
func recentEnv(t *testing.T) (*stateEnv, *stateRun, string) {
	t.Helper()
	env := newStateEnv(t, nil)
	other := env.folder("glutter", "g.go", "pkg/h.go")
	first := env.start(other)
	first.open(filepath.Join(other, "g.go"), 0)
	first.close()
	second := env.start(env.root)
	second.open(env.path("main.go"), 0)
	second.open(env.path("src/a.go"), 0)
	return env, second, other
}

func TestTheRecentlyOpenedFoldersAndFilesAreSaved(t *testing.T) {
	env, run, other := recentEnv(t)
	run.close()
	g := run.global()
	if want := []string{env.root, other}; !slices.Equal(g.Recent.Folders, want) {
		t.Errorf("folders %v, want %v", g.Recent.Folders, want)
	}
	if want := []string{env.path("src/a.go"), env.path("main.go"), filepath.Join(other, "g.go")}; !slices.Equal(g.Recent.Files, want) {
		t.Errorf("files %v, want %v", g.Recent.Files, want)
	}
	if g.LastWindow == nil || g.LastWindow.Folder != env.root {
		t.Errorf("last window %+v", g.LastWindow)
	}
	// a new run reads them from the disk
	again := env.start(env.root)
	if got := again.global().Recent; !slices.Equal(got.Folders, g.Recent.Folders) || !slices.Equal(got.Files, g.Recent.Files) {
		t.Errorf("read again %+v", got)
	}
	// opening a file again moves it to the front, once
	again.open(env.path("main.go"), 0)
	if got := again.global().Recent.Files; got[0] != env.path("main.go") || len(got) != 3 {
		t.Errorf("files %v", got)
	}
}

func TestTheOpenRecentMenuListsFoldersThenFiles(t *testing.T) {
	env, run, other := recentEnv(t)
	want := []string{
		"Reopen Closed Editor~", "-",
		"~/projects/emetos", "~/projects/glutter", "-",
		"~/projects/emetos/src/a.go", "~/projects/emetos/main.go", "~/projects/glutter/g.go", "-",
		"More...", "-", "Clear Recently Opened...",
	}
	if got := run.recentLabels(); !slices.Equal(got, want) {
		t.Errorf("rows\n got %q\nwant %q", got, want)
	}
	// the rows run the commands with their paths
	var args []any
	var ids []string
	for _, it := range run.shell().menus.Items(command.MenubarRecentMenu, nil) {
		if it.Data != nil {
			ids, args = append(ids, it.ID), append(args, it.Data)
		}
	}
	if !slices.Equal(ids, []string{"openRecentFolder", "openRecentFolder", "openRecentFile", "openRecentFile", "openRecentFile"}) ||
		args[1] != other || args[2] != env.path("src/a.go") {
		t.Errorf("commands %v with %v", ids, args)
	}
	// the submenu is in the File menu, after Open Folder
	var found bool
	var before string
	for _, it := range run.shell().menus.Items(command.MenubarFileMenu, nil) {
		if it.Submenu && it.ID == command.MenubarRecentMenu {
			found = strings.ReplaceAll(it.Label, "&", "") == "Open Recent" && before == "Open Folder..."
			if len(it.Items) != len(want) {
				t.Errorf("the submenu has %d rows, want %d", len(it.Items), len(want))
			}
		}
		before = strings.ReplaceAll(it.Label, "&", "")
	}
	if !found {
		t.Errorf("the File menu has no Open Recent submenu after Open Folder")
	}
}

func TestTheOpenRecentMenuLeavesOutWhatIsGoneAndStopsAtTen(t *testing.T) {
	var names []string
	for i := range 14 {
		names = append(names, fmt.Sprintf("f%02d.go", i))
	}
	env := newStateEnv(t, nil)
	dir := env.folder("many", names...)
	first := env.start(dir)
	for _, n := range names {
		first.open(filepath.Join(dir, n), 0)
	}
	first.close()
	if err := os.Remove(filepath.Join(dir, "f13.go")); err != nil { // the most recent one
		t.Fatal(err)
	}
	second := env.start(dir)
	var files []string
	for _, l := range second.recentLabels() {
		if strings.HasPrefix(l, "~/projects/many/f") {
			files = append(files, strings.TrimPrefix(l, "~/projects/many/"))
		}
	}
	want := []string{"f12.go", "f11.go", "f10.go", "f09.go", "f08.go", "f07.go", "f06.go", "f05.go", "f04.go", "f03.go"}
	if !slices.Equal(files, want) {
		t.Errorf("files %v, want %v", files, want)
	}
	// the list itself keeps the deleted file: it comes back if the file does
	if !slices.Contains(second.global().Recent.Files, filepath.Join(dir, "f13.go")) {
		t.Error("the list lost the file that is gone")
	}
	// a path of the home folder is written with "~", another is whole, and a long label is cut in the middle
	if got := truncateMiddle(strings.Repeat("a", 100)+"/"+strings.Repeat("b", 100), 120); len([]rune(got)) != 120 || !strings.Contains(got, "…") ||
		!strings.HasPrefix(got, "aaaa") || !strings.HasSuffix(got, "bbbb") {
		t.Errorf("truncated to %q", got)
	}
	if got := truncateMiddle("short", 120); got != "short" {
		t.Errorf("%q", got)
	}
	if got := recentLabel("/elsewhere/x.go"); got != "/elsewhere/x.go" {
		t.Errorf("%q", got)
	}
}

func TestReopenClosedEditorIsInTheOpenRecentSubmenu(t *testing.T) {
	_, run, _ := recentEnv(t)
	if got := run.recentLabels()[0]; got != "Reopen Closed Editor~" {
		t.Fatalf("first row %q", got)
	}
	run.command("workbench.action.closeActiveEditor", nil)
	if got := run.recentLabels()[0]; got != "Reopen Closed Editor" {
		t.Fatalf("first row %q after closing an editor", got)
	}
	var item workbenchItem
	for _, it := range run.shell().menus.Items(command.MenubarRecentMenu, nil) {
		if strings.ReplaceAll(it.Label, "&", "") == "Reopen Closed Editor" {
			item = workbenchItem{id: it.ID}
			run.shell().menus.Run(it)
		}
	}
	run.pump()
	run.ts.Pump(100 * time.Millisecond)
	run.pump()
	if item.id != "workbench.action.reopenClosedEditor" {
		t.Fatalf("the row runs %q", item.id)
	}
	if got := run.layout(); len(got) != 1 || !strings.Contains(got[0], "a.go*") {
		t.Errorf("groups %v: the closed editor did not come back", got)
	}
}

type workbenchItem struct{ id string }

func TestTheOpenRecentPicker(t *testing.T) {
	env, run, other := recentEnv(t)
	run.command("workbench.action.openRecent", nil)
	run.waitFor("the picker", func() bool { return run.win.QuickInput.Visible() })
	want := []string{
		"# folders", "emetos | ~/projects", "glutter | ~/projects",
		"# files", "a.go | ~/projects/emetos/src", "main.go | ~/projects/emetos", "g.go | ~/projects/glutter",
	}
	if got := run.pickerRows(); !slices.Equal(got, want) {
		t.Fatalf("rows\n got %q\nwant %q", got, want)
	}
	p := run.win.QuickInput.Picker()
	if p.Placeholder() != RecentPlaceholder || !p.MatchOnDescription {
		t.Errorf("placeholder %q, match on description %v", p.Placeholder(), p.MatchOnDescription)
	}
	// the first folder is the one that is open: the second is the active row
	if row, ok := p.Active(); !ok || row.Item.ID != other {
		t.Errorf("active row %+v", row.Item)
	}
	// the buttons remove a row from the list
	var row quickinput.Row
	for _, r := range p.Rows() {
		if r.Item.Label == "main.go" {
			row = r
		}
	}
	if len(row.Item.Buttons) != 1 || row.Item.Buttons[0].Tooltip != "Remove from Recently Opened" {
		t.Fatalf("buttons %+v", row.Item.Buttons)
	}
	run.win.QuickInput.Trigger(row, 0, quickinput.KeyMods{})
	run.pump()
	if got := run.global().Recent.Files; slices.Contains(got, env.path("main.go")) || len(got) != 2 {
		t.Errorf("files after removing main.go: %v", got)
	}
	for _, r := range run.pickerRows() {
		if strings.HasPrefix(r, "main.go") {
			t.Errorf("the picker still lists main.go: %v", run.pickerRows())
		}
	}
	// the filter looks at the description too
	run.win.QuickInput.Picker().SetValue("glutter")
	run.pump()
	if got := run.pickerRows(); len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), "g.go | ~/projects/glutter") {
		t.Errorf("filtered rows %v", got)
	}
}

func TestChoosingARecentFileOpensItAndChoosingAFolderOpensTheFolder(t *testing.T) {
	env, run, other := recentEnv(t)
	run.command("workbench.action.openRecent", nil)
	run.waitFor("the picker", func() bool { return run.win.QuickInput.Visible() })
	var file quickinput.Row
	for _, row := range run.win.QuickInput.Picker().Rows() {
		if row.Item.ID == filepath.Join(other, "g.go") {
			file = row
		}
	}
	run.win.QuickInput.AcceptRow(file, quickinput.KeyMods{})
	run.waitFor("the file", func() bool { return strings.Contains(strings.Join(run.layout(), " "), "g.go*") })

	run.command("workbench.action.openRecent", nil)
	run.waitFor("the picker", func() bool { return run.win.QuickInput.Visible() })
	var folder quickinput.Row
	for _, row := range run.win.QuickInput.Picker().Rows() {
		if row.Item.ID == other {
			folder = row
		}
	}
	oldID := run.win.StateID()
	run.win.QuickInput.AcceptRow(folder, quickinput.KeyMods{})
	run.waitFor("the folder", func() bool { return run.win.Folder == other })
	if run.win.StateID() == oldID || run.win.StateID() == "" {
		t.Errorf("the state id %q did not change from %q", run.win.StateID(), oldID)
	}
	if got := run.layout(); !slices.Equal(got, []string{"1!: "}) {
		t.Errorf("groups %v after opening the folder", got)
	}
	_ = env
}

func TestOpeningAFolderKeepsTheSavedStateOfTheFolderItLeaves(t *testing.T) {
	env, run, other := recentEnv(t)
	run.command("openRecentFolder", other)
	run.waitFor("the folder", func() bool { return run.win.Folder == other })
	run.ts.Pump(stateSnapshotDelay + 100*time.Millisecond)
	env.clock.Advance(2 * storage.DefaultStateDelay)
	// the editors of the project were closed to open the other folder, and the state of the project still has them
	st, ok := env.savedWindow(env.root)
	if !ok || st.Grid == nil || len(st.Grid.Root[0].Model.Editors) != 2 {
		t.Fatalf("the saved state of the project: %+v, %v", st.Grid, ok)
	}
	// the state of the other folder is the one of the window now
	run.open(filepath.Join(other, "pkg/h.go"), 0)
	run.ts.Pump(stateSnapshotDelay + 100*time.Millisecond)
	env.clock.Advance(2 * storage.DefaultStateDelay)
	st, ok = env.savedWindow(other)
	if !ok || len(st.Grid.Root[0].Model.Editors) != 1 || st.Folder != other {
		t.Errorf("the saved state of the other folder: %+v, %v", st, ok)
	}
	if run.global().LastWindow.Folder != other {
		t.Errorf("last window %+v", run.global().LastWindow)
	}
	// closing the folder leaves the state of the window under the id of the empty window
	run.command("workbench.action.closeFolder", nil)
	if run.win.Folder != "" || run.win.StateID() == "" {
		t.Errorf("folder %q state id %q", run.win.Folder, run.win.StateID())
	}
	st, _ = env.savedWindow(other)
	if len(st.Grid.Root[0].Model.Editors) != 1 {
		t.Errorf("the state of the folder that was closed was written over: %+v", st.Grid.Root[0].Model.Editors)
	}
}

func TestACancelledFolderChangeGoesOnSavingTheState(t *testing.T) {
	env, run, other := recentEnv(t)
	doc, ok := run.win.Documents.Get(document.FileURI(env.path("main.go")))
	if !ok {
		t.Fatal("main.go is not open")
	}
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "x"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	run.command("openRecentFolder", other)
	run.pump()
	if !run.win.Dialogs.IsShowing() {
		t.Fatal("no question about the unsaved changes")
	}
	run.tapDialog("Cancel")
	if run.win.Folder != env.root {
		t.Fatalf("the folder changed to %q", run.win.Folder)
	}
	run.open(env.path("e.txt"), 0)
	run.ts.Pump(stateSnapshotDelay + 100*time.Millisecond)
	env.clock.Advance(2 * storage.DefaultStateDelay)
	st, _ := env.savedWindow(env.root)
	if n := len(st.Grid.Root[0].Model.Editors); n != 3 {
		t.Errorf("%d editors saved, want 3", n)
	}
}

func (r *stateRun) tapDialog(label string) {
	r.env.t.Helper()
	if !r.win.Dialogs.IsShowing() {
		r.env.t.Fatalf("no dialog shows (looking for %q)", label)
	}
	r.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText(label)))
	r.pump()
}

func (r *stateRun) waitFor(what string, cond func() bool) {
	r.env.t.Helper()
	for range 200 {
		if cond() {
			return
		}
		r.ts.Pump(20 * time.Millisecond)
		time.Sleep(2 * time.Millisecond)
	}
	r.env.t.Fatalf("timed out waiting for %s", what)
}

func TestClearRecentlyOpened(t *testing.T) {
	for _, tc := range []struct {
		button  string
		cleared bool
	}{{"Clear", true}, {"Cancel", false}} {
		t.Run(tc.button, func(t *testing.T) {
			_, run, _ := recentEnv(t)
			run.command("workbench.action.clearRecentFiles", nil)
			spec, ok := run.win.Dialogs.Current()
			if !ok || spec.Message != RecentClearMessage || spec.Detail != RecentClearDetail || spec.Type != kit.DialogWarning {
				t.Fatalf("the question: %+v, %v", spec, ok)
			}
			run.tapDialog(tc.button)
			g := run.global()
			if tc.cleared {
				if !g.Recent.Empty() || run.win.Recent.Len() != 0 {
					t.Errorf("not cleared: %+v, %v", g.Recent, run.win.Recent.List())
				}
				// the menu has no recent rows left
				if got, want := run.recentLabels(), []string{"Reopen Closed Editor~", "-", "More...", "-", "Clear Recently Opened..."}; !slices.Equal(got, want) {
					t.Errorf("rows %q", got)
				}
				// and the next file is the first of a new list
				run.open(run.env.path("e.txt"), 0)
				if got := run.global().Recent.Files; !slices.Equal(got, []string{run.env.path("e.txt")}) {
					t.Errorf("files %v", got)
				}
			} else if g.Recent.Empty() || run.win.Recent.Len() == 0 {
				t.Error("cleared after Cancel")
			}
		})
	}
}

func TestTheRecentCommandsNeedAPath(t *testing.T) {
	_, run, _ := recentEnv(t)
	for _, id := range []string{"openRecentFolder", "openRecentFile"} {
		for _, args := range []any{nil, "", "relative/path", 42} {
			err := run.win.Process.Commands.Run(command.Context{Ctx: context.Background(), Keys: run.win.Keys, Source: command.SourceAPI}, id, args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("%s(%v): expected *errors.Error, got %T", id, args, err)
			}
			if !appErr.HasRoute("OpenRecentPathInvalid") {
				t.Errorf("%s(%v): unexpected failure path: %s", id, args, appErr.Route())
			}
		}
	}
}

func TestCtrlROpensTheRecentList(t *testing.T) {
	_, run, _ := recentEnv(t)
	run.pump()
	if !sendChord(run.ts, ctrl(keybinding.KeyR)) {
		t.Fatal("ctrl+r was not used")
	}
	run.waitFor("the picker", func() bool { return run.win.QuickInput.Visible() })
	if got := run.pickerRows(); len(got) == 0 || got[0] != "# folders" {
		t.Errorf("rows %v", got)
	}
}

func TestAFailingStateWriteDoesNotKeepTheWindowOpen(t *testing.T) {
	env := newStateEnv(t, nil)
	var attempts int
	run := env.startWith(env.root, ProcessOptions{StateWriteFile: func(string, []byte) error {
		attempts++
		return os.ErrPermission
	}})
	run.open(env.path("main.go"), 0)
	run.close() // the write fails, is logged, and the window closes
	if attempts == 0 {
		t.Error("nothing tried to write")
	}
	if _, ok := env.savedWindow(env.root); ok {
		t.Error("a file was written by a write that fails")
	}
}

func TestTheStateIsNotWrittenWhileTheCloseQuestionIsUnanswered(t *testing.T) {
	env := newStateEnv(t, nil)
	run := env.start(env.root, env.path("main.go"))
	doc, _ := run.win.Documents.Get(document.FileURI(env.path("main.go")))
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "x"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	run.win.RequestClose(CloseReasonQuit)
	run.pump()
	if !run.win.Dialogs.IsShowing() || run.quits != 0 {
		t.Fatalf("dialog %v, quits %d", run.win.Dialogs.IsShowing(), run.quits)
	}
	run.tapDialog("Cancel")
	if run.quits != 0 {
		t.Fatal("the window closed")
	}
	if _, ok := env.savedWindow(env.root); ok {
		t.Error("the state was written although the window stays open")
	}
	// Don't Save closes the window, and the state of its editors is kept as it is
	run.win.RequestClose(CloseReasonQuit)
	run.pump()
	run.tapDialog("Don't Save")
	if run.quits != 1 {
		t.Fatalf("quits %d", run.quits)
	}
	st, ok := env.savedWindow(env.root)
	if !ok || len(st.Grid.Root[0].Model.Editors) != 1 {
		t.Errorf("saved %+v, %v", st, ok)
	}
}

func TestEveryWindowIdIsItsOwnFile(t *testing.T) {
	env := newStateEnv(t, nil)
	other := env.folder("glutter", "g.go")
	a := env.start(env.root)
	a.open(env.path("main.go"), 0)
	a.close()
	b := env.start(other)
	b.open(filepath.Join(other, "g.go"), 0)
	b.close()
	idA, _ := storage.WindowID(env.root)
	idB, _ := storage.WindowID(other)
	if idA == idB {
		t.Fatal("two folders have one id")
	}
	for folder, want := range map[string]string{env.root: "main.go", other: "g.go"} {
		st, ok := env.savedWindow(folder)
		if !ok || st.Folder != folder || !strings.Contains(string(st.Grid.Root[0].Model.Editors[0].Value), want) {
			t.Errorf("%s: %+v, %v", folder, st, ok)
		}
	}
}

func TestTheSavedSizeSurvivesAWindowThatStartsMaximized(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	s := first.shell()
	win := &fakeWindow{ChangeNotifier: *foundation.NewChangeNotifier(), size: ui.Size{Width: 1000, Height: 700}}
	s.state.source = nil
	s.trackWindow(win)
	win.size, win.maximized = ui.Size{Width: 1920, Height: 1080}, true
	win.NotifyListeners()
	first.close()
	st, ok := env.savedWindow(env.root)
	if !ok || st.Geometry == nil || *st.Geometry != (storage.Geometry{Width: 1000, Height: 700, Maximized: true}) {
		t.Fatalf("saved geometry %+v", st.Geometry)
	}

	// the next run opens maximized: the window reports the size of the screen, and the size it comes back to stays
	second := env.start(env.root)
	if second.geometry == nil || *second.geometry != *st.Geometry {
		t.Fatalf("restored geometry %+v", second.geometry)
	}
	s = second.shell()
	win = &fakeWindow{ChangeNotifier: *foundation.NewChangeNotifier(), size: ui.Size{Width: 1920, Height: 1080}, maximized: true}
	s.state.source = nil
	s.trackWindow(win)
	if got := s.state.geometry; got != (storage.Geometry{Width: 1000, Height: 700, Maximized: true}) {
		t.Errorf("geometry %+v", got)
	}
}

func TestASavedLayoutThatIsNotALayoutIsLeftOut(t *testing.T) {
	env := newStateEnv(t, nil)
	win := newTestWindow(t, "")
	for _, raw := range []string{`[1,2]`, `"layout"`, `{"sideBarVisible": "yes"}`, `{`} {
		err := win.restoreLayout(json.RawMessage(raw))
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("%s: expected *errors.Error, got %T (%v)", raw, err, err)
		}
		if !appErr.HasRoute("Window.restoreLayout.LayoutStateInvalid") {
			t.Errorf("%s: unexpected failure path: %s", raw, appErr.Route())
		}
	}
	if got := win.Layout.State(); got != workbench.LayoutStateFromSettings(win.Settings) {
		t.Errorf("the layout changed: %+v", got)
	}
	// and a window that starts over such a file starts with the layout of its settings
	env.writeWindowState(env.root, `{"version":1,"layout":"nonsense"}`)
	run := env.start(env.root)
	if got := run.win.Layout.State(); got != workbench.LayoutStateFromSettings(run.win.Settings) {
		t.Errorf("layout %+v", got)
	}
}

func TestASavedEditorThatIsNotAFileIsNotOpened(t *testing.T) {
	win := newTestWindow(t, "")
	for value, route := range map[string]string{
		"untitled:1":               "Window.openSavedDocument.SavedUntitledWithoutBackup",
		"emetos-virtual:/settings": "Window.openSavedDocument.SavedEditorNotAFile",
		"/relative":                "Window.openSavedDocument->ParseURI.URIInvalid",
		"file://relative/x":        "Window.openSavedDocument->ParseURI.URIInvalid",
	} {
		_, err := win.openSavedDocument(context.Background(), &restoredWindow{backups: map[string]storage.Backup{}}, value)
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Errorf("%s: expected *errors.Error, got %T (%v)", value, err, err)
			continue
		}
		if !appErr.HasRoute(route) {
			t.Errorf("%s: unexpected failure path: %s, want %s", value, appErr.Route(), route)
		}
	}
	// a saved editor of that kind is dropped and counted
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.close()
	st, _ := env.savedWindow(env.root)
	st.Grid.Root[0].Model.Editors = append(st.Grid.Root[0].Model.Editors, editorgroup.SerializedInput{TypeID: editorgroup.TextInputTypeID, Value: "emetos-virtual:/settings"})
	st.Grid.Root[0].Model.MRU = append(st.Grid.Root[0].Model.MRU, 1)
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	env.writeWindowState(env.root, string(data))
	second := env.start(env.root)
	if got := second.layout(); len(got) != 1 || !strings.Contains(got[0], "main.go") {
		t.Errorf("groups %v", got)
	}
	if items := second.win.Process.Notify.Items(); len(items) != 1 || items[0].Message != DroppedEditorsNotice(1) {
		t.Errorf("notifications %+v", items)
	}
}
