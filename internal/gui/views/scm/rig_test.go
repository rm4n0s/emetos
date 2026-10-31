package scm

import (
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var goldenThemes = []struct {
	name   string
	colors kit.Colors
}{
	{"dark", kit.DarkModern()},
	{"light", kit.LightModern()},
}

// pump shows a widget at the top left of a window of a size, in the theme and with the test fonts, on the background of the side
// bar.
func pump(t testing.TB, colors kit.Colors, size ui.Size, child widgets.Widget) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, size)
	ts.PumpWidget(appOf(colors, size, child))
	ts.Pump(time.Millisecond)
	return ts
}

// appOf is the app that pump shows: the child at the top left, in a box of a size.
func appOf(colors kit.Colors, size ui.Size, child widgets.Widget) widgets.Widget {
	return widgets.NewWidgetsApp(colors.SideBarBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				page := widgets.NewColoredBox(colors.SideBarBackground, widgets.NewAlign(
					widgets.NewSizedBox(child, widgets.SizedBoxOpts{Width: foundation.Some(size.Width), Height: foundation.Some(size.Height)}),
					widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
				return kit.NewTheme(colors, page, kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	})
}

// run is a command the view ran.
type run struct {
	id   string
	args any
}

// opened is a file the view opened.
type opened struct {
	uri string
	o   OpenOptions
}

// shown is a context menu the view asked for.
type shown struct {
	menu  string
	extra map[string]any
	args  []any
}

// rig is the Changes view over a model, with fakes for what the window gives it.
type rig struct {
	t     *testing.T
	model *scmcore.Model
	view  *View
	ts    *gluttertest.Tester

	runs   []run
	opens  []opened
	menus  []shown
	decor  map[string]Decoration
	always bool
	side   bool
	drags  []*fakeDrag
	opened []dropped

	// the boxes (input_test.go): boxes keeps the input of each repository, the rest are what the window gives
	after              []func()
	boxes              bool
	minLines, maxLines int
	font               InputFont
	noButton           bool
	acceptLabel        string
	dropdowns          []shownItems
	reveals            int
	keys               *contextkey.Scope
	scopes             *kit.FocusScopes
}

// shownItems is a dropdown the view asked for.
type shownItems struct {
	items []kit.MenuItem
	at    ui.Offset
}

// fakeMenus is what the registry would give for the menus of the view: the inline buttons of a Git-like provider.
func fakeMenus(menu string, extra map[string]any) []command.MenuEntry {
	group, _ := extra["scmResourceGroup"].(string)
	switch menu {
	case command.SCMResourceContext:
		out := []command.MenuEntry{{Command: "git.openFile", Title: "Open File", Icon: "go-to-file", Group: "inline"}}
		switch group {
		case "workingTree":
			out = append(out, command.MenuEntry{Command: "git.clean", Title: "Discard Changes", Icon: "discard", Group: "inline"},
				command.MenuEntry{Command: "git.stage", Title: "Stage Changes", Icon: "add", Group: "inline"})
		case "index":
			out = append(out, command.MenuEntry{Command: "git.unstage", Title: "Unstage Changes", Icon: "remove", Group: "inline"})
		}
		return out
	case command.SCMResourceGroupContext:
		switch group {
		case "workingTree":
			return []command.MenuEntry{{Command: "git.cleanAll", Title: "Discard All Changes", Icon: "discard", Group: "inline"},
				{Command: "git.stageAll", Title: "Stage All Changes", Icon: "add", Group: "inline"}}
		case "index":
			return []command.MenuEntry{{Command: "git.unstageAll", Title: "Unstage All Changes", Icon: "remove", Group: "inline"}}
		}
	case command.SCMTitle:
		return []command.MenuEntry{{Command: "git.commit", Title: "Commit", Icon: "check", Group: "navigation"},
			{Command: "git.refresh", Title: "Refresh", Icon: "refresh", Group: "navigation"}}
	}
	return nil
}

func iconTheme(t testing.TB) func() *theme.FileIconTheme {
	th, err := theme.BundledFileIconTheme(theme.DefaultFileIconTheme)
	if err != nil {
		t.Fatal(err)
	}
	return func() *theme.FileIconTheme { return th }
}

// newRig makes the view over an empty model; add repositories with addRepo.
func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, model: scmcore.NewModel(scmcore.DefaultConfig()), decor: map[string]Decoration{},
		minLines: 1, maxLines: 10, font: InputFont{Size: 13}, keys: contextkey.NewRoot(), scopes: kit.NewFocusScopes()}
	r.view = New(Options{
		Scopes: r.scopes, Keys: r.keys,
		InputLines:       func() (int, int) { return r.minLines, r.maxLines },
		InputFont:        func() InputFont { return r.font },
		ShowActionButton: func() bool { return !r.noButton },
		AcceptKeyLabel:   func() string { return r.acceptLabel },
		ShowItems:        func(items []kit.MenuItem, at ui.Offset) { r.dropdowns = append(r.dropdowns, shownItems{items, at}) },
		Reveal:           func() { r.reveals++ },

		Model:   r.model,
		Run:     func(id string, args any) { r.runs = append(r.runs, run{id, args}) },
		Entries: fakeMenus,
		ShowMenu: func(menu string, extra map[string]any, args []any, _ ui.Offset) {
			r.menus = append(r.menus, shown{menu, extra, args})
		},
		OpenResource: func(uri string, o OpenOptions) { r.opens = append(r.opens, opened{uri, o}) },
		Decoration: func(uri string) (Decoration, bool) {
			d, ok := r.decor[uri]
			return d, ok
		},
		Icons:             iconTheme(t),
		AlwaysShowActions: func() bool { return r.always },
		Side:              func() bool { return r.side },
		BeginDrag: func(label string) (DragSession, error) {
			d := &fakeDrag{label: label}
			r.drags = append(r.drags, d)
			return d, nil
		},
		OpenIn: func(uri string, group, index int) error {
			r.opened = append(r.opened, dropped{uri, group, index})
			return nil
		},
	})
	t.Cleanup(r.view.Dispose)
	return r
}

// newBoxRig makes the view over an empty model that keeps the box of each repository, and runs what the view asks to be done after
// a frame when the test settles.
func newBoxRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.boxes = true
	r.view.opts.AfterFrame = func(f func()) { r.after = append(r.after, f) }
	return r
}

// change is a file of a fixture: its path below the root, its letter, tooltip and color.
type change struct {
	path, letter, tooltip, color string
	strike                       bool
}

// group is a group of a fixture.
type group struct {
	id, label string
	changes   []change
}

// addRepo adds a source control with the groups and the decorations of its files.
func (r *rig) addRepo(name string, groups ...group) *scmcore.Source {
	r.t.Helper()
	root := "/work/" + name
	src := r.model.Add(scmcore.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file://" + root, ContextValue: "repository", Owner: "e"})
	// the rows of the changes are tested without the box (input_test.go has the boxes)
	if !r.boxes {
		src.Input().SetVisible(false)
	}
	src.SetStatusBarCommands([]scmcore.StatusCommand{{Text: "$(git-branch) main", Tooltip: "main, Checkout Branch/Tag...", Command: "git.checkout"}})
	for _, g := range groups {
		grp := src.AddGroup(g.id, g.label, g.id != "workingTree", "")
		var rs []scmcore.Resource
		for _, c := range g.changes {
			uri := "file://" + root + "/" + c.path
			rs = append(rs, scmcore.Resource{URI: uri, Decorations: scmcore.Decorations{Tooltip: c.tooltip, StrikeThrough: c.strike}})
			if c.letter != "" {
				r.decor[uri] = Decoration{Badge: c.letter, Tooltip: c.tooltip, Color: c.color}
			}
		}
		grp.SetResources(rs)
	}
	return src
}

// show puts the body of the view in a window.
func (r *rig) show(colors kit.Colors, size ui.Size) {
	r.t.Helper()
	r.ts = pump(r.t, colors, size, NewBody(r.view, BodyOpts{}))
	r.settle()
}

func (r *rig) settle() {
	for range 3 {
		r.ts.Pump(time.Millisecond)
		// what the view asked to run after the frame
		for len(r.after) > 0 {
			f := r.after[0]
			r.after = r.after[1:]
			f()
		}
	}
}

// rowKeys are the keys of the rows, for the checks of the structure.
func (r *rig) rowKeys() []string {
	var out []string
	for _, row := range r.view.List().Rows() {
		out = append(out, row.Key)
	}
	return out
}

// labels are the labels of the rows.
func (r *rig) labels() []string {
	var out []string
	for _, row := range r.view.List().Rows() {
		out = append(out, row.Label)
	}
	return out
}

func (r *rig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("errors: %v", errs)
	}
}

func (r *rig) key(k services.LogicalKeyboardKey) {
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	r.settle()
}

// y is the middle of the row at an index, in window coordinates.
func (r *rig) y(i int) float64 { return float64(i)*rowHeight + rowHeight/2 }

func (r *rig) tapRow(i int) {
	r.t.Helper()
	r.ts.Pump(time.Second) // the press that follows one within 400 ms is a double click
	r.ts.TapAt(ui.Offset{Dx: 100, Dy: r.y(i)}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.settle()
}

var _ = slices.Contains[[]string]

// the colors of the letters, as the Git extension gives them
const (
	colModified = "gitDecoration.modifiedResourceForeground"
	colAdded    = "gitDecoration.addedResourceForeground"
	colDeleted  = "gitDecoration.deletedResourceForeground"
	colUntrack  = "gitDecoration.untrackedResourceForeground"
	colConflict = "gitDecoration.conflictingResourceForeground"
	colStaged   = "gitDecoration.stageModifiedResourceForeground"
	colRenamed  = "gitDecoration.renamedResourceForeground"
)

// gitGroups is the changes of a repository with a merge in progress, staged and unstaged changes, as the proof shows them.
func gitGroups() []group {
	return []group{
		{"merge", "Merge Changes", []change{{"src/loader.go", "U", "Conflict: Both Modified", colConflict, false}}},
		{"index", "Staged Changes", []change{
			{"src/loader_test.go", "M", "Index Modified", colStaged, false},
			{"docs/notes.md", "A", "Index Added", colAdded, false},
		}},
		{"workingTree", "Changes", []change{
			{"src/config.go", "M", "Modified", colModified, false},
			{"README.md", "M", "Modified", colModified, false},
			{"old/legacy.go", "D", "Deleted", colDeleted, true},
			{"notes.txt", "U", "Untracked", colUntrack, false},
		}},
	}
}

// fakeDrag is the drag session of the window: it records where the pointer went and drops on group 2 at place 1.
type fakeDrag struct {
	label string
	moves []ui.Offset
	ended bool
	drop  bool
}

func (d *fakeDrag) Move(at ui.Offset) { d.moves = append(d.moves, at) }

func (d *fakeDrag) End(_ ui.Offset, drop bool, open func(group, index int) error) (bool, error) {
	d.ended, d.drop = true, drop
	if drop {
		return true, open(2, 1)
	}
	return false, nil
}

// dropped is a file the view opened in a group at the end of a drag.
type dropped struct {
	uri          string
	group, index int
}
