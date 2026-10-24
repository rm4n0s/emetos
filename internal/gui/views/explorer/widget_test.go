package explorer

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// pump shows a widget at the top left of a window of a size, in the theme and with the test fonts, on the
// background of the side bar.
func pump(t *testing.T, colors kit.Colors, size ui.Size, child widgets.Widget) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, size)
	ts.PumpWidget(widgets.NewWidgetsApp(colors.SideBarBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				page := widgets.NewColoredBox(colors.SideBarBackground, widgets.NewAlign(
					widgets.NewSizedBox(child, widgets.SizedBoxOpts{Width: foundation.Some(size.Width), Height: foundation.Some(size.Height)}),
					widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
				return kit.NewTheme(colors, page, kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	ts.Pump(time.Millisecond)
	return ts
}

// wrig is the folder view shown in a window.
type wrig struct {
	*rig
	ts *gluttertest.Tester
}

func newWRig(t *testing.T, colors kit.Colors, size ui.Size, entries ...string) *wrig {
	t.Helper()
	return newWRigFS(t, colors, size, nil, entries...)
}

func newWRigFS(t *testing.T, colors kit.Colors, size ui.Size, fsys func(workspace.FS) workspace.FS, entries ...string) *wrig {
	t.Helper()
	r := newRigFS(t, fsys, entries...)
	w := &wrig{rig: r}
	w.ts = pump(t, colors, size, NewBody(r.view, BodyOpts{AfterFrame: func(f func()) {
		r.mu.Lock()
		r.posted = append(r.posted, f)
		r.mu.Unlock()
	}}))
	w.settle()
	return w
}

// settle runs the frames and what the background reads handed over.
func (w *wrig) settle() {
	for range 4 {
		w.run()
		w.ts.Pump(time.Millisecond)
		time.Sleep(time.Millisecond)
	}
}

func (w *wrig) y(i int) float64 { return float64(i)*22 + 11 }

// tapRow presses the row i, in its label.
func (w *wrig) tapRow(i int) {
	w.t.Helper()
	w.ts.TapAt(ui.Offset{Dx: 120, Dy: w.y(i)}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	w.settle()
}

func (w *wrig) key(k services.LogicalKeyboardKey) {
	w.t.Helper()
	w.ts.SendKeyDownEvent(k)
	w.ts.SendKeyUpEvent(k)
	w.settle()
}

func (w *wrig) noErrors() {
	w.t.Helper()
	if errs := w.ts.TakeErrors(); len(errs) != 0 {
		w.t.Errorf("errors: %v", errs)
	}
}

var size = ui.Size{Width: 280, Height: 330}

func TestBodyShowsTheFilesWithTheirIcons(t *testing.T) {
	w := newWRig(t, kit.DarkModern(), size, "main.go", "README.md", "docs/guide.md", "data.bin")
	for _, name := range []string{"docs", "main.go", "README.md", "data.bin"} {
		if err := gluttertest.FindsOneWidget(w.ts, gluttertest.ByText(name)); err != nil {
			t.Error(err)
		}
	}
	// Seti glyphs: Go is E03A, the readme (a file name rule) is E04D, the default file is E023, and a
	// folder has none
	for glyph, want := range map[rune]int{0xE03A: 1, 0xE04D: 1, 0xE023: 1} {
		if got := len(w.ts.Elements(gluttertest.ByText(string(glyph)))); got != want {
			t.Errorf("%U is shown %d times, want %d", glyph, got, want)
		}
	}
	w.noErrors()
}

func TestBodyClicks(t *testing.T) {
	w := newWRig(t, kit.DarkModern(), size, "docs/guide.md", "docs/more/deep.md", "main.go")
	// the rows are docs, main.go; clicks that are further apart than the interval of a double click are single ones
	w.tapRow(1)
	if got, want := w.opened, []opened{{w.path("main.go"), false}}; !slices.Equal(got, want) {
		t.Errorf("a click opened %v, want %v", got, want)
	}
	if got := w.view.SelectedPath(); got != w.path("main.go") {
		t.Errorf("selected %q", got)
	}
	// a double click keeps the file
	w.opened = nil
	w.ts.Pump(500 * time.Millisecond)
	w.tapRow(1)
	w.tapRow(1)
	if len(w.opened) < 2 || w.opened[len(w.opened)-1] != (opened{w.path("main.go"), true}) {
		t.Errorf("a double click opened %v, want the last to be pinned", w.opened)
	}
	// a click on a folder opens it and reads it
	w.opened = nil
	w.ts.Pump(500 * time.Millisecond)
	w.tapRow(0)
	w.wait("docs", func() bool { return w.ws.Find(w.path("docs")) != nil && w.ws.Find(w.path("docs")).Resolved() })
	w.settle()
	if !w.view.Tree().IsExpanded(w.path("docs")) {
		t.Errorf("rows %v: docs did not open", w.keys2())
	}
	if len(w.opened) != 0 {
		t.Errorf("a folder was opened as a file: %v", w.opened)
	}
	for _, name := range []string{"guide.md", "more"} {
		if err := gluttertest.FindsOneWidget(w.ts, gluttertest.ByText(name)); err != nil {
			t.Error(err)
		}
	}
	// a second click closes it again
	w.ts.Pump(500 * time.Millisecond)
	w.tapRow(0)
	if w.view.Tree().IsExpanded(w.path("docs")) {
		t.Error("docs did not close")
	}
	w.noErrors()
}

func TestBodyKeys(t *testing.T) {
	w := newWRig(t, kit.DarkModern(), size, "docs/guide.md", "main.go", "go.mod")
	w.tapRow(0) // opens docs and gives the tree the focus
	w.wait("docs", w.loaded)
	w.settle()
	tests := []struct {
		name  string
		key   services.LogicalKeyboardKey
		focus string
		open  []opened
		rows  int
	}{
		{"down goes to the first file", services.LogicalKeyboardKeyArrowDown, "docs/guide.md", nil, 4},
		{"down again", services.LogicalKeyboardKeyArrowDown, "go.mod", nil, 4},
		{"enter opens it kept and as a preview first", services.LogicalKeyboardKeyEnter, "go.mod", []opened{{"go.mod", false}, {"go.mod", true}}, 4},
		{"up", services.LogicalKeyboardKeyArrowUp, "docs/guide.md", nil, 4},
		{"left goes to the folder", services.LogicalKeyboardKeyArrowLeft, "docs", nil, 4},
		{"left again folds it", services.LogicalKeyboardKeyArrowLeft, "docs", nil, 3},
		{"right opens it", services.LogicalKeyboardKeyArrowRight, "docs", nil, 4},
		{"right goes in", services.LogicalKeyboardKeyArrowRight, "docs/guide.md", nil, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w.opened = nil
			w.key(tt.key)
			if key, _ := w.view.Tree().Focused(); key != w.path(tt.focus) {
				t.Errorf("focus %q, want %q", key, tt.focus)
			}
			var want []opened
			for _, o := range tt.open {
				want = append(want, opened{w.path(o.path), o.pinned})
			}
			if !slices.Equal(w.opened, want) {
				t.Errorf("opened %v, want %v", w.opened, want)
			}
			if got := len(w.view.Tree().Rows()); got != tt.rows {
				t.Errorf("%d rows %v, want %d", got, w.keys2(), tt.rows)
			}
		})
	}
	w.noErrors()
}

// gated is a file system whose folder read waits for the test.
type gated struct {
	workspace.FS
	path    string
	release chan struct{}
}

func (g gated) ReadDir(path string) ([]fs.DirEntry, error) {
	if path == g.path {
		<-g.release
	}
	return g.FS.ReadDir(path)
}

func TestBodyLoadingPlaceholder(t *testing.T) {
	release := make(chan struct{})
	gate := &gated{release: release}
	w := newWRigFS(t, kit.DarkModern(), size, func(base workspace.FS) workspace.FS { gate.FS = base; return gate }, "docs/guide.md", "main.go")
	gate.path = filepath.Join(w.root, "docs")
	w.tapRow(0)
	if err := gluttertest.FindsOneWidget(w.ts, gluttertest.ByText("Loading...")); err != nil {
		t.Fatalf("while docs is read: %v", err)
	}
	close(release)
	w.wait("docs", func() bool { return w.loaded() })
	w.settle()
	if w.ts.Exists(gluttertest.ByText("Loading...")) {
		t.Error("the placeholder stays")
	}
	if err := gluttertest.FindsOneWidget(w.ts, gluttertest.ByText("guide.md")); err != nil {
		t.Error(err)
	}
	w.noErrors()
}

func TestBodyFolderThatCannotBeRead(t *testing.T) {
	var root string
	r := &rig{t: t, root: tree(t, "a.go"), keys: contextkey.NewRoot()}
	root = r.root
	cfg := workspace.DefaultConfig()
	ws, err := workspace.Open(context.Background(), root, workspace.Options{Config: &cfg, FS: failing{osFS{}, map[string]bool{root: true}}})
	if err != nil {
		t.Fatal(err)
	}
	r.view = NewView(context.Background(), ws, ViewOpts{Post: func(f func()) { r.mu.Lock(); r.posted = append(r.posted, f); r.mu.Unlock() }})
	t.Cleanup(r.view.Dispose)
	w := &wrig{rig: r}
	w.ts = pump(t, kit.DarkModern(), size, NewBody(r.view))
	w.wait("the folder", r.view.Loaded)
	w.settle()
	if err := gluttertest.FindsOneWidget(w.ts, gluttertest.ByKey(ErrorKey)); err != nil {
		t.Fatal(err)
	}
	appErr, ok := errors.FromError(r.view.RootError())
	if !ok || !appErr.HasRoute("WorkspaceReadDirFailed") {
		t.Errorf("error %v", r.view.RootError())
	}
	w.noErrors()
}

func TestBodyScrollsToARevealedFile(t *testing.T) {
	var entries []string
	for i := range 40 {
		entries = append(entries, "dir/file"+string(rune('a'+i%26))+string(rune('a'+i/26))+".txt")
	}
	entries = append(entries, "z.txt")
	w := newWRig(t, kit.DarkModern(), size, entries...)
	var done bool
	w.view.Reveal(w.path("dir/fileza.txt"), RevealSelect, func(bool) { done = true })
	w.wait("the reveal", func() bool { return done })
	w.settle()
	if got := w.view.scrollTo; got != "" {
		t.Errorf("the scroll to %q was not made", got)
	}
	if off := w.view.Controller().ScrollOffset(); off <= 0 {
		t.Errorf("the tree was not scrolled (%v)", off)
	}
	w.noErrors()
}

func TestActionsRunTheirCommands(t *testing.T) {
	var ran []string
	ts := pump(t, kit.DarkModern(), ui.Size{Width: 80, Height: 24}, NewActions(ActionsOpts{OnRun: func(c string) { ran = append(ran, c) }}))
	for _, icon := range []int{fonts.CodiconRefresh, fonts.CodiconCollapseAll} {
		if err := gluttertest.FindsOneWidget(ts, gluttertest.ByIcon(fonts.CodiconIcon(icon))); err != nil {
			t.Error(err)
		}
	}
	ts.Tap(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconRefresh)))
	ts.Tap(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCollapseAll)))
	ts.Pump(time.Millisecond)
	if want := []string{CommandRefresh, CommandCollapse}; !slices.Equal(ran, want) {
		t.Errorf("ran %v, want %v", ran, want)
	}
}

func TestWelcome(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opts    WelcomeOpts
		texts   []string
		absent  []string
		pressed int
	}{
		{"no editors", WelcomeOpts{}, []string{WelcomeText, WelcomeButton}, []string{WelcomeEditorsText}, 1},
		{"editors are open", WelcomeOpts{HasEditors: true}, []string{WelcomeText, WelcomeButton, WelcomeEditorsText}, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pressed int
			opts := tc.opts
			opts.OnOpenFolder = func() { pressed++ }
			ts := pump(t, kit.DarkModern(), size, NewWelcome(opts))
			for _, s := range tc.texts {
				if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText(s)); err != nil {
					t.Error(err)
				}
			}
			for _, s := range tc.absent {
				if ts.Exists(gluttertest.ByText(s)) {
					t.Errorf("%q is shown", s)
				}
			}
			ts.Tap(gluttertest.ByKey(OpenFolderKey))
			ts.Pump(time.Millisecond)
			if pressed != tc.pressed {
				t.Errorf("the button was pressed %d times, want %d", pressed, tc.pressed)
			}
		})
	}
}

// fakeInput is an editor of a file, for the Open Editors.
type fakeInput struct {
	editorgroup.BaseInput
	path  string
	dirty bool
}

func (f *fakeInput) TypeID() string { return "test.input" }
func (f *fakeInput) Resource() (document.URI, bool) {
	return document.FileURI(f.path), true
}
func (f *fakeInput) Name() string                           { return filepath.Base(f.path) }
func (f *fakeInput) Description() string                    { return filepath.Base(filepath.Dir(f.path)) }
func (f *fakeInput) Title() string                          { return f.path }
func (f *fakeInput) Dirty() bool                            { return f.dirty }
func (f *fakeInput) Capabilities() editorgroup.Capabilities { return 0 }
func (f *fakeInput) Serialize() (string, bool)              { return "", false }
func (f *fakeInput) Dispose()                               { f.BaseInput.Dispose() }
func (f *fakeInput) Matches(other editorgroup.EditorInput) bool {
	o, ok := other.(*fakeInput)
	return ok && o.path == f.path
}

// editorsRig is an Open Editors over a grid.
type editorsRig struct {
	t       *testing.T
	grid    *editorgroup.Grid
	view    *OpenEditors
	order   string
	visible int
	opened  []string
	groups  []int
	closed  []string
}

func newEditorsRig(t *testing.T) *editorsRig {
	t.Helper()
	r := &editorsRig{t: t, grid: editorgroup.NewGrid(editorgroup.GridOptions{}), order: "editorOrder", visible: 9}
	r.view = NewOpenEditors(OpenEditorsOpts{
		Grid:      r.grid,
		SortOrder: func() string { return r.order },
		Visible:   func() int { return r.visible },
		Icons: func() *theme.FileIconTheme {
			th, err := theme.BundledFileIconTheme(theme.DefaultFileIconTheme)
			if err != nil {
				t.Error(err)
			}
			return th
		},
		Language: func(path string) string { return languageOf(path) },
		Keys:     contextkey.NewRoot(),
		OnOpen: func(g *editorgroup.Group, in editorgroup.EditorInput, pinned bool) {
			r.opened = append(r.opened, in.Name()+map[bool]string{true: " pinned", false: ""}[pinned])
		},
		OnActivateGroup: func(g *editorgroup.Group) { r.groups = append(r.groups, g.ID()) },
		OnClose:         func(g *editorgroup.Group, in editorgroup.EditorInput) { r.closed = append(r.closed, in.Name()) },
	})
	t.Cleanup(r.view.Dispose)
	return r
}

// open opens the files in a group, kept, the last one active.
func (r *editorsRig) open(g *editorgroup.Group, paths ...string) []*fakeInput {
	var out []*fakeInput
	for _, p := range paths {
		in := &fakeInput{path: p}
		r.t.Cleanup(in.Dispose)
		g.Model.OpenEditor(in, editorgroup.OpenOptions{Pinned: true, Active: true})
		out = append(out, in)
	}
	return out
}

// rows are the rows of the list: a group as "[Group 1]", an editor as its name, "*" before the selected one,
// "!" after a dirty editor.
func (r *editorsRig) rows() []string {
	var out []string
	for _, row := range r.view.List().Rows() {
		var s string
		switch d := row.Data.(type) {
		case groupRow:
			s = "[" + d.Label + "]"
		case editorRow:
			s = d.Input.Name()
			if d.Dirty {
				s += "!"
			}
			if d.Preview {
				s += "~"
			}
		}
		if r.view.List().IsSelected(row.Key) {
			s = "*" + s
		}
		out = append(out, s)
	}
	return out
}

func TestOpenEditorsRows(t *testing.T) {
	r := newEditorsRig(t)
	g1 := r.grid.Active()
	r.open(g1, "/p/b.go", "/p/a.go", "/q/c.go")
	if got, want := r.rows(), []string{"b.go", "a.go", "*c.go"}; !slices.Equal(got, want) {
		t.Errorf("one group: %v, want %v", got, want)
	}
	// several groups show a row for each
	g2, err := r.grid.AddGroup(g1, editorgroup.DirRight)
	if err != nil {
		t.Fatal(err)
	}
	r.open(g2, "/p/x.go")
	if err := r.grid.Activate(g2); err != nil {
		t.Fatal(err)
	}
	if got, want := r.rows(), []string{"[Group 1]", "b.go", "a.go", "c.go", "[Group 2]", "*x.go"}; !slices.Equal(got, want) {
		t.Errorf("two groups: %v, want %v", got, want)
	}
	// the active editor of the active group is the selected one
	if err := r.grid.Activate(g1); err != nil {
		t.Fatal(err)
	}
	if got, want := r.rows(), []string{"[Group 1]", "b.go", "a.go", "*c.go", "[Group 2]", "x.go"}; !slices.Equal(got, want) {
		t.Errorf("the first group is active: %v, want %v", got, want)
	}
	if got := r.view.Count(); got != 6 {
		t.Errorf("count %d", got)
	}
	// closing the second group moves its editors to the first, which leaves one group
	if _, err := r.grid.RemoveGroup(g2); err != nil {
		t.Fatal(err)
	}
	if got, want := r.rows(), []string{"b.go", "a.go", "*c.go", "x.go"}; !slices.Equal(got, want) {
		t.Errorf("one group again: %v, want %v", got, want)
	}
}

func TestOpenEditorsSortOrders(t *testing.T) {
	tests := []struct {
		order string
		want  []string
	}{
		{"editorOrder", []string{"b.go", "a.go", "*c.go"}},
		{"alphabetical", []string{"a.go", "b.go", "*c.go"}},
		{"fullPath", []string{"a.go", "*c.go", "b.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.order, func(t *testing.T) {
			r := newEditorsRig(t)
			r.order = tt.order
			r.open(r.grid.Active(), "/z/b.go", "/p/a.go", "/q/c.go")
			if got := r.rows(); !slices.Equal(got, tt.want) {
				t.Errorf("%s: %v, want %v", tt.order, got, tt.want)
			}
		})
	}
}

func TestOpenEditorsDirtyAndPreview(t *testing.T) {
	r := newEditorsRig(t)
	g := r.grid.Active()
	ins := r.open(g, "/p/a.go", "/p/b.go")
	// a preview editor opens in the place of the other preview
	preview := &fakeInput{path: "/p/p.go"}
	t.Cleanup(preview.Dispose)
	g.Model.OpenEditor(preview, editorgroup.OpenOptions{Active: true})
	if got, want := r.rows(), []string{"a.go", "b.go", "*p.go~"}; !slices.Equal(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	ins[0].dirty = true
	ins[0].Emit(editorgroup.InputDirty)
	if got, want := r.rows(), []string{"a.go!", "b.go", "*p.go~"}; !slices.Equal(got, want) {
		t.Errorf("a dirty editor: %v, want %v", got, want)
	}
	if got := r.view.DirtyCount(); got != 1 {
		t.Errorf("%d dirty editors, want 1", got)
	}
}

func TestOpenEditorsBodyHeight(t *testing.T) {
	tests := []struct {
		name    string
		editors int
		visible int
		want    float64
	}{
		{"none", 0, 9, 0},
		{"fewer than visible", 3, 9, 66},
		{"more than visible scroll", 12, 9, 198},
		{"a setting of 2", 12, 2, 44},
		{"at least one row", 5, 0, 22},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newEditorsRig(t)
			r.visible = tt.visible
			for i := range tt.editors {
				r.open(r.grid.Active(), "/p/f"+string(rune('a'+i))+".go")
			}
			if got := r.view.BodyHeight(); got != tt.want {
				t.Errorf("height %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenEditorsClicks(t *testing.T) {
	r := newEditorsRig(t)
	g1 := r.grid.Active()
	r.open(g1, "/p/a.go", "/p/b.go")
	g2, err := r.grid.AddGroup(g1, editorgroup.DirRight)
	if err != nil {
		t.Fatal(err)
	}
	r.open(g2, "/p/c.go")
	rows := r.view.List().Rows()
	r.view.open(rows[1], false)
	r.view.open(rows[1], true)
	r.view.open(rows[0], false)
	r.view.open(rows[3], false)
	if want := []string{"a.go", "a.go pinned"}; !slices.Equal(r.opened, want) {
		t.Errorf("opened %v, want %v", r.opened, want)
	}
	if want := []int{g1.ID(), g2.ID()}; !slices.Equal(r.groups, want) {
		t.Errorf("groups %v, want %v", r.groups, want)
	}
}

func TestOpenEditorsWidget(t *testing.T) {
	r := newEditorsRig(t)
	g1 := r.grid.Active()
	ins := r.open(g1, "/p/a.go", "/p/b.go", "/p/README.md")
	ins[1].dirty = true
	ins[1].Emit(editorgroup.InputDirty)
	ts := pump(t, kit.DarkModern(), size, NewOpenEditorsBody(r.view))
	ts.Pump(time.Millisecond)
	for _, name := range []string{"a.go", "b.go", "README.md"} {
		if !ts.Exists(gluttertest.ByText(name)) {
			t.Errorf("no row for %s", name)
		}
	}
	// the selected row (the last one) has the close button; a click on it closes that editor
	ts.TapAt(ui.Offset{Dx: 16, Dy: 2*22 + 11}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	ts.Pump(time.Millisecond)
	if want := []string{"README.md"}; !slices.Equal(r.closed, want) {
		t.Errorf("closed %v, want %v", r.closed, want)
	}
	// a click on a row opens it
	ts.TapAt(ui.Offset{Dx: 120, Dy: 11}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	ts.Pump(time.Millisecond)
	if want := []string{"a.go"}; !slices.Equal(r.opened, want) {
		t.Errorf("opened %v, want %v", r.opened, want)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}
