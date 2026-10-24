package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests of the grid of groups: they build the window with files in the groups and look at the
// tree, the tabs and the editors of each group.

func (a *groupApp) grid() *editorgroup.Grid { return a.shell().group.grid }

// shape writes the tree of the grid by the ids of the groups: H(1 V(2 3)).
func (a *groupApp) shape() string {
	var walk func(n editorgroup.Node) string
	walk = func(n editorgroup.Node) string {
		if n.Group != nil {
			return fmt.Sprint(n.Group.ID())
		}
		var parts []string
		for _, c := range n.Children {
			parts = append(parts, walk(c))
		}
		o := "H"
		if n.Orientation == editorgroup.Vertical {
			o = "V"
		}
		return o + "(" + strings.Join(parts, " ") + ")"
	}
	return walk(a.grid().Tree())
}

// group is the group with an id.
func (a *groupApp) group(id int) *editorgroup.Group {
	a.t.Helper()
	g := a.grid().Group(id)
	if g == nil {
		a.t.Fatalf("no group %d in %s", id, a.shape())
	}
	return g
}

// names are the names of the tabs of a group, as its model has them.
func (a *groupApp) names(id int) []string {
	var out []string
	for _, e := range a.group(id).Model.Editors(editorgroup.Sequential, false) {
		out = append(out, e.Name())
	}
	return out
}

// shown are the names of the tabs the widgets of a group show.
func (a *groupApp) shown(id int) []string {
	a.t.Helper()
	finder := gluttertest.Descendant(gluttertest.ByKey(a.shell().view(a.group(id)).key), gluttertest.ByType[*kit.Tabs]())
	if !a.ts.Exists(finder) {
		return nil
	}
	var out []string
	for _, it := range a.ts.Widget(finder).(*kit.Tabs).Items() {
		out = append(out, it.Name)
	}
	return out
}

func (a *groupApp) activeID() int { return a.grid().Active().ID() }

// ids are the ids of the groups in the order of the grid.
func (a *groupApp) ids() []int {
	var out []int
	for _, g := range a.grid().Groups(editorgroup.GridAppearance) {
		out = append(out, g.ID())
	}
	return out
}

// area is the place of the widgets of a group on the screen.
func (a *groupApp) area(id int) (topLeft ui.Offset, size ui.Size) {
	a.t.Helper()
	finder := gluttertest.ByKey(a.shell().view(a.group(id)).key)
	return a.ts.TopLeft(finder), a.ts.Size(finder)
}

func (a *groupApp) hasArea(id int) bool {
	return a.ts.Exists(gluttertest.ByKey(a.shell().view(a.group(id)).key))
}

// editorOf is the editor of the active tab of a group.
func (a *groupApp) editorOf(id int) *textEditor {
	a.t.Helper()
	e := a.shell().group.editors[a.group(id).Model.ActiveEditor()]
	if e == nil {
		a.t.Fatalf("group %d has no editor", id)
	}
	return e
}

func (a *groupApp) runWith(id string, args any) error {
	a.t.Helper()
	err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, id, args)
	a.pump()
	return err
}

func (a *groupApp) wantShape(want string) {
	a.t.Helper()
	if got := a.shape(); got != want {
		a.t.Errorf("the grid is %s, want %s", got, want)
	}
}

func TestSplittingWithTheCommands(t *testing.T) {
	for _, tc := range []struct {
		cmd   string
		shape string
	}{
		{"workbench.action.splitEditorRight", "H(1 2)"},
		{"workbench.action.splitEditorLeft", "H(2 1)"},
		{"workbench.action.splitEditorDown", "V(1 2)"},
		{"workbench.action.splitEditorUp", "V(2 1)"},
		{"workbench.action.splitEditor", "H(1 2)"},
		{"workbench.action.splitEditorOrthogonal", "V(1 2)"},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
			a.clickTab("b.go")
			a.run(tc.cmd)
			a.wantShape(tc.shape)
			// the active editor is copied into the new group, which is the active group
			if got := a.names(2); !slices.Equal(got, []string{"b.go"}) {
				t.Errorf("the new group has %v", got)
			}
			if got := a.names(1); !slices.Equal(got, []string{"a.go", "b.go"}) {
				t.Errorf("the first group has %v", got)
			}
			if a.activeID() != 2 {
				t.Errorf("active group %d", a.activeID())
			}
			if got := a.shown(2); !slices.Equal(got, []string{"b.go"}) {
				t.Errorf("the new group shows %v", got)
			}
			if got := a.shown(1); !slices.Equal(got, []string{"a.go", "b.go"}) {
				t.Errorf("the first group shows %v", got)
			}
			if v := a.win.Keys.Get("multipleEditorGroups"); v != true {
				t.Error("multipleEditorGroups is not set")
			}
			a.noErrors()
		})
	}
}

func TestSplitKeysAndTheSideBySideDirection(t *testing.T) {
	for _, tc := range []struct {
		name      string
		direction string
		keys      string
		shape     string
	}{
		{"Ctrl+backslash splits to the right by default", "", "ctrl+\\", "H(1 2)"},
		{"and below when the setting says down", "down", "ctrl+\\", "V(1 2)"},
		{"Ctrl+K Ctrl+backslash splits the other way", "", "ctrl+k ctrl+\\", "V(1 2)"},
		{"and the other way again with down", "down", "ctrl+k ctrl+\\", "H(1 2)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go"}, "a.go")
			if tc.direction != "" {
				a.setting("workbench.editor.openSideBySideDirection", tc.direction)
			}
			a.press(tc.keys)
			a.wantShape(tc.shape)
			a.noErrors()
		})
	}
}

func TestSplittingAnEmptyGroupAddsAnEmptyGroup(t *testing.T) {
	a := newGroupApp(t, nil)
	a.run("workbench.action.splitEditorRight")
	a.wantShape("H(1 2)")
	if !a.group(2).Empty() || a.activeID() != 2 {
		t.Errorf("group 2 empty %v, active %d", a.group(2).Empty(), a.activeID())
	}
	if v := a.win.Keys.Get("activeEditorGroupEmpty"); v != true {
		t.Error("activeEditorGroupEmpty is not set")
	}
	// the empty groups show the logo and the name
	if got := a.ts.Count(gluttertest.ByText("Emetos")); got != 2 {
		t.Errorf("%d empty states, want 2", got)
	}
	a.noErrors()
}

func TestFocusingGroups(t *testing.T) {
	// three groups: H(1 V(2 3)), the third active
	setup := func(t *testing.T) *groupApp {
		a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
		a.run("workbench.action.splitEditorRight")
		a.run("workbench.action.splitEditorDown")
		a.wantShape("H(1 V(2 3))")
		return a
	}
	for _, tc := range []struct {
		name string
		keys string
		cmd  string
		from int
		want int
		// shape, when the command adds a group
		shape string
	}{
		{"Ctrl+1", "ctrl+1", "", 3, 1, ""},
		{"Ctrl+2", "ctrl+2", "", 1, 2, ""},
		{"Ctrl+3", "ctrl+3", "", 1, 3, ""},
		{"Ctrl+4 adds the group after the last", "ctrl+4", "", 1, 4, "H(1 V(2 H(3 4)))"},
		{"Ctrl+5 finds nothing", "ctrl+5", "", 1, 1, ""},
		{"Ctrl+8 finds nothing", "ctrl+8", "", 1, 1, ""},
		{"focus last", "", "workbench.action.focusLastEditorGroup", 1, 3, ""},
		{"focus next", "", "workbench.action.focusNextGroup", 1, 2, ""},
		{"focus next goes round", "", "workbench.action.focusNextGroup", 3, 1, ""},
		{"focus previous", "", "workbench.action.focusPreviousGroup", 3, 2, ""},
		{"focus previous goes round", "", "workbench.action.focusPreviousGroup", 1, 3, ""},
		// the groups 2 and 3 are right of group 1: the one that was active last, 3, is found
		{"Ctrl+K Ctrl+Right", "ctrl+k ctrl+right", "", 1, 3, ""},
		{"Ctrl+K Ctrl+Left", "ctrl+k ctrl+left", "", 2, 1, ""},
		{"Ctrl+K Ctrl+Down", "ctrl+k ctrl+down", "", 2, 3, ""},
		{"Ctrl+K Ctrl+Up", "ctrl+k ctrl+up", "", 3, 2, ""},
		{"Ctrl+K Ctrl+Up goes round", "ctrl+k ctrl+up", "", 2, 3, ""},
		{"left without wrap stays", "", "workbench.action.focusLeftGroupWithoutWrap", 1, 1, ""},
		{"navigate between groups", "", "workbench.action.navigateEditorGroups", 2, 3, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := setup(t)
			a.run("workbench.action.focusFirstEditorGroup")
			if tc.from != 1 {
				if err := a.grid().Activate(a.group(tc.from)); err != nil {
					t.Fatal(err)
				}
				a.pump()
			}
			if tc.keys != "" {
				a.press(tc.keys)
			} else {
				a.run(tc.cmd)
			}
			if a.activeID() != tc.want {
				t.Errorf("active group %d, want %d", a.activeID(), tc.want)
			}
			if tc.shape != "" {
				a.wantShape(tc.shape)
			}
			// the keyboard focus follows: the editor of the group has it, and keys reach it
			if g := a.group(a.activeID()); !g.Empty() {
				if ed := a.editorOf(g.ID()); !ed.focus.HasFocus() {
					t.Errorf("the editor of group %d does not have the focus", g.ID())
				}
			}
			a.noErrors()
		})
	}
}

func TestFocusFollowsClicks(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	a.run("workbench.action.splitEditorRight")
	if a.activeID() != 2 {
		t.Fatalf("active group %d", a.activeID())
	}
	// a click in the text of the first group
	topLeft, size := a.area(1)
	a.ts.TapAt(topLeft.Add(ui.Offset{Dx: size.Width / 2, Dy: size.Height / 2}), mouseButton)
	a.pump()
	if a.activeID() != 1 || !a.editorOf(1).focus.HasFocus() {
		t.Errorf("after a click in group 1: active %d, focus %v", a.activeID(), a.editorOf(1).focus.HasFocus())
	}
	// a click on a tab of the other group
	a.ts.TapAt(a.ts.Center(gluttertest.Descendant(gluttertest.Descendant(gluttertest.ByKey(a.shell().view(a.group(2)).key), gluttertest.ByType[*kit.Tabs]()), gluttertest.ByText("a.go"))), mouseButton)
	a.pump()
	if a.activeID() != 2 || !a.editorOf(2).focus.HasFocus() {
		t.Errorf("after a click on a tab of group 2: active %d, focus %v", a.activeID(), a.editorOf(2).focus.HasFocus())
	}
	// the strip of the group that is not active has the colors of an unfocused group
	for id, want := range map[int]bool{1: true, 2: false} {
		finder := gluttertest.Descendant(gluttertest.ByKey(a.shell().view(a.group(id)).key), gluttertest.ByType[*kit.Tabs]())
		_ = finder
		if got := a.shell().tabsOptions(a.win, a.group(id)).Unfocused; got != want {
			t.Errorf("group %d: unfocused %v, want %v", id, got, want)
		}
	}
	a.noErrors()
}

func TestAClickInAnEmptyGroupActivatesIt(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.run("workbench.action.newGroupRight")
	a.wantShape("H(1 2)")
	a.run("workbench.action.focusFirstEditorGroup")
	if a.activeID() != 1 {
		t.Fatalf("active group %d", a.activeID())
	}
	topLeft, size := a.area(2)
	a.ts.TapAt(topLeft.Add(ui.Offset{Dx: size.Width / 2, Dy: size.Height / 2}), mouseButton)
	a.pump()
	if a.activeID() != 2 || !a.shell().view(a.group(2)).empty.HasFocus() {
		t.Errorf("active %d, the empty area has the focus: %v", a.activeID(), a.shell().view(a.group(2)).empty.HasFocus())
	}
	a.noErrors()
}

func TestSashDragsResizeTheGroups(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	a.run("workbench.action.splitEditorRight")
	_, s1 := a.area(1)
	_, s2 := a.area(2)
	if d := s1.Width - s2.Width; d > 2 || d < -2 {
		t.Fatalf("the groups start equal: %v and %v", s1.Width, s2.Width)
	}
	sash := gluttertest.Descendant(gluttertest.ByKey(workbench.PartKey(workbench.PartEditor)), gluttertest.ByType[*kit.Sash]())
	if got := a.ts.Count(sash); got != 1 {
		t.Fatalf("%d sashes in the editor area, want 1", got)
	}
	a.ts.Drag(sash, ui.Offset{Dx: 120})
	a.pump()
	_, n1 := a.area(1)
	_, n2 := a.area(2)
	if n1.Width < s1.Width+100 || n2.Width > s2.Width-100 {
		t.Errorf("widths after the drag: %v and %v, from %v and %v", n1.Width, n2.Width, s1.Width, s2.Width)
	}
	// the grid knows the sizes, and they survive a change of the grid that keeps them
	sizes := a.grid().Sizes(nil)
	if len(sizes) != 2 || sizes[0] < 0.55 {
		t.Errorf("sizes of the grid %v", sizes)
	}
	a.run("workbench.action.splitEditorDown")
	_, m1 := a.area(1)
	if m1.Width < s1.Width+100 {
		t.Errorf("group 1 is %v wide after a split of the other: the drag was lost", m1.Width)
	}
	a.noErrors()
}

func TestEvenSizesAndMaximize(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	a.run("workbench.action.splitEditorRight")
	sash := gluttertest.Descendant(gluttertest.ByKey(workbench.PartKey(workbench.PartEditor)), gluttertest.ByType[*kit.Sash]())
	a.ts.Drag(sash, ui.Offset{Dx: 150})
	a.pump()
	_, w1 := a.area(1)
	_, w2 := a.area(2)
	if w1.Width-w2.Width < 200 {
		t.Fatalf("the drag did not resize: %v %v", w1.Width, w2.Width)
	}
	// the groups are sized again as equal
	a.run("workbench.action.evenEditorWidths")
	_, e1 := a.area(1)
	_, e2 := a.area(2)
	if d := e1.Width - e2.Width; d > 2 || d < -2 {
		t.Errorf("widths after Reset Editor Group Sizes: %v and %v", e1.Width, e2.Width)
	}
	// a drag, then maximize: the other group is gone, and comes back with its size
	a.ts.Drag(sash, ui.Offset{Dx: 100})
	a.pump()
	_, b1 := a.area(1)
	_, b2 := a.area(2)
	a.press("ctrl+k ctrl+m")
	if a.grid().Maximized() == nil || a.grid().Maximized().ID() != 2 {
		t.Fatalf("maximized %v", a.grid().Maximized())
	}
	if a.hasArea(1) {
		t.Error("the other group is still laid out")
	}
	_, full := a.area(2)
	if full.Width < b1.Width+b2.Width-4 {
		t.Errorf("the maximized group is %v wide, want %v", full.Width, b1.Width+b2.Width)
	}
	if v := a.win.Keys.Get("editorPartMaximizedEditorGroup"); v != true {
		t.Error("editorPartMaximizedEditorGroup is not set")
	}
	a.press("ctrl+k ctrl+m")
	if a.grid().Maximized() != nil {
		t.Error("the second toggle leaves the group maximized")
	}
	_, r1 := a.area(1)
	_, r2 := a.area(2)
	if d := r1.Width - b1.Width; d > 2 || d < -2 {
		t.Errorf("group 1 is %v wide after the restore, was %v", r1.Width, b1.Width)
	}
	if d := r2.Width - b2.Width; d > 2 || d < -2 {
		t.Errorf("group 2 is %v wide after the restore, was %v", r2.Width, b2.Width)
	}
	// focusing another group while one is maximized restores the others
	a.press("ctrl+k ctrl+m")
	a.run("workbench.action.focusFirstEditorGroup")
	if a.grid().Maximized() != nil || !a.hasArea(1) || !a.hasArea(2) {
		t.Error("focusing another group leaves the group maximized")
	}
	a.noErrors()
}

func TestTheMaximizeKeyNeedsSeveralGroups(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.press("ctrl+k ctrl+m")
	if a.grid().Maximized() != nil {
		t.Error("a single group was maximized")
	}
}

func TestEditorLayoutCommands(t *testing.T) {
	for _, tc := range []struct {
		cmd   string
		shape string
	}{
		{"workbench.action.editorLayoutSingle", "H(1)"},
		{"workbench.action.editorLayoutTwoColumns", "H(1 2)"},
		{"workbench.action.editorLayoutThreeColumns", "H(1 2 3)"},
		{"workbench.action.editorLayoutTwoRows", "V(1 2)"},
		{"workbench.action.editorLayoutThreeRows", "V(1 2 3)"},
		{"workbench.action.editorLayoutTwoByTwoGrid", "H(V(1 2) V(3 4))"},
		{"workbench.action.editorLayoutTwoColumnsBottom", "V(1 H(2 3))"},
		{"workbench.action.editorLayoutTwoRowsRight", "H(1 V(2 3))"},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
			a.run(tc.cmd)
			a.wantShape(tc.shape)
			// the editors stay in the first group, and every group is on screen
			if got := a.names(1); !slices.Equal(got, []string{"a.go", "b.go"}) {
				t.Errorf("the first group has %v", got)
			}
			for _, id := range a.ids() {
				if !a.hasArea(id) {
					t.Errorf("group %d is not on screen", id)
				}
			}
			a.noErrors()
		})
	}
}

func TestLayoutEditorGroupsTakesAnArgument(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	err := a.runWith("layoutEditorGroups", map[string]any{
		"orientation": 0.0,
		"groups":      []any{map[string]any{"size": 0.3}, map[string]any{"size": 0.7, "groups": []any{map[string]any{}, map[string]any{}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.wantShape("H(1 V(2 3))")
	if got := a.grid().Sizes(nil); len(got) != 2 || got[0] > 0.31 || got[0] < 0.29 {
		t.Errorf("sizes %v", got)
	}
	for _, args := range []any{nil, "x", map[string]any{"groups": []any{}}, map[string]any{"orientation": 2.0, "groups": []any{map[string]any{}}},
		map[string]any{"groups": []any{"x"}}, map[string]any{"groups": []any{map[string]any{"size": "big"}}}} {
		err := a.runWith("layoutEditorGroups", args)
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("parseLayout") && !appErr.HasRoute("parseLayoutNodes") {
			t.Errorf("argument %v: %v", args, err)
		}
		if ok && appErr.Tag != "LayoutArgumentInvalid" {
			t.Errorf("argument %v: tag %s", args, appErr.Tag)
		}
	}
	// a layout the grid does not take
	appErr, ok := errors.FromError(a.runWith("layoutEditorGroups", map[string]any{"groups": []any{map[string]any{}}, "orientation": 1.0}))
	if ok {
		t.Errorf("a layout of one group is fine: %v", appErr)
	}
}

func TestMovingEditorsBetweenGroups(t *testing.T) {
	for _, tc := range []struct {
		name  string
		keys  string
		cmd   string
		args  any
		setup func(a *groupApp)
		// the tabs of the groups left, by id
		want   map[int][]string
		shape  string
		active int
	}{
		{name: "to the next group with Ctrl+Alt+Right", keys: "ctrl+alt+right",
			setup: func(a *groupApp) {
				a.run("workbench.action.newGroupRight")
				a.run("workbench.action.focusFirstEditorGroup")
			},
			want: map[int][]string{1: {"b.go", "c.go"}, 2: {"a.go"}}, shape: "H(1 2)", active: 2},
		{name: "to the next group adds a group when it is the last", keys: "ctrl+alt+right",
			want: map[int][]string{1: {"b.go", "c.go"}, 2: {"a.go"}}, shape: "H(1 2)", active: 2},
		{name: "to the previous group adds one on the left", keys: "ctrl+alt+left",
			want: map[int][]string{1: {"b.go", "c.go"}, 2: {"a.go"}}, shape: "H(2 1)", active: 2},
		{name: "to the group below", cmd: "workbench.action.moveEditorToBelowGroup",
			want: map[int][]string{1: {"b.go", "c.go"}, 2: {"a.go"}}, shape: "V(1 2)", active: 2},
		{name: "to the first group", keys: "shift+alt+1",
			setup: func(a *groupApp) { a.run("workbench.action.splitEditorRight") },
			want:  map[int][]string{1: {"a.go", "b.go", "c.go"}}, shape: "H(1)", active: 1},
		{name: "copy into the next group", cmd: "workbench.action.splitEditorToNextGroup",
			want: map[int][]string{1: {"a.go", "b.go", "c.go"}, 2: {"a.go"}}, shape: "H(1 2)", active: 2},
		{name: "the last editor leaves and the group closes", keys: "ctrl+alt+right",
			setup: func(a *groupApp) {
				a.run("workbench.action.closeOtherEditors")
			},
			want: map[int][]string{2: {"a.go"}}, shape: "H(2)", active: 2},
		{name: "moveActiveEditor by group", cmd: "moveActiveEditor", args: map[string]any{"to": "right", "by": "group"},
			want: map[int][]string{1: {"b.go", "c.go"}, 2: {"a.go"}}, shape: "H(1 2)", active: 2},
		{name: "moveActiveEditor by tab", cmd: "moveActiveEditor", args: map[string]any{"to": "right", "by": "tab", "value": 2.0},
			want: map[int][]string{1: {"b.go", "c.go", "a.go"}}, shape: "H(1)", active: 1},
		{name: "moveActiveEditor to the last tab", cmd: "moveActiveEditor", args: map[string]any{"to": "last"},
			want: map[int][]string{1: {"b.go", "c.go", "a.go"}}, shape: "H(1)", active: 1},
		{name: "copyActiveEditor to a position", cmd: "copyActiveEditor", args: map[string]any{"to": "position", "by": "group", "value": 2.0},
			setup: func(a *groupApp) {
				a.run("workbench.action.newGroupRight")
				a.run("workbench.action.focusFirstEditorGroup")
			},
			want: map[int][]string{1: {"a.go", "b.go", "c.go"}, 2: {"a.go"}}, shape: "H(1 2)", active: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
			if tc.setup != nil {
				tc.setup(a)
			}
			switch {
			case tc.keys != "":
				a.press(tc.keys)
			case tc.args != nil:
				if err := a.runWith(tc.cmd, tc.args); err != nil {
					t.Fatal(err)
				}
			default:
				a.run(tc.cmd)
			}
			a.wantShape(tc.shape)
			for id, want := range tc.want {
				if got := a.names(id); !slices.Equal(got, want) {
					t.Errorf("group %d has %v, want %v", id, got, want)
				}
				if got := a.shown(id); !slices.Equal(got, want) {
					t.Errorf("group %d shows %v, want %v", id, got, want)
				}
			}
			if a.activeID() != tc.active {
				t.Errorf("active group %d, want %d", a.activeID(), tc.active)
			}
			// every document has an editor for each tab and is open
			uses := map[*document.Document]int{}
			for _, g := range a.grid().Groups(editorgroup.CreationTime) {
				for _, e := range g.Model.Editors(editorgroup.Sequential, false) {
					uses[e.(*editorgroup.TextInput).Document()]++
				}
			}
			for doc, want := range uses {
				if got := a.win.Documents.Refs(doc); got != want {
					t.Errorf("%s has %d users, want %d", doc.URI().Title(), got, want)
				}
			}
			if len(a.shell().group.editors) != sumValues(uses) {
				t.Errorf("%d editors for %d tabs", len(a.shell().group.editors), sumValues(uses))
			}
			a.noErrors()
		})
	}
}

func sumValues(m map[*document.Document]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func TestMovingAnEditorKeepsItsView(t *testing.T) {
	a := newGroupApp(t, []string{"long.go", "b.go"}, "long.go", "b.go")
	a.clickTab("long.go")
	vm := a.vm()
	vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 100, Col: 2})})
	vm.SetScrollTop(900)
	a.pump()
	a.press("ctrl+alt+right")
	moved := a.editorOf(2)
	if moved.view != vm {
		t.Error("the editor that moves keeps its view model")
	}
	if got := moved.view.Cursors().Primary().Selection.Active; got.Line != 100 {
		t.Errorf("the cursor is at %v", got)
	}
	if moved.view.ScrollTop() < 800 {
		t.Errorf("scrolled to %v", moved.view.ScrollTop())
	}
	a.noErrors()
}

func TestMergingAndClosingGroups(t *testing.T) {
	setup := func(t *testing.T) *groupApp {
		a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go")
		a.run("workbench.action.splitEditorRight")
		a.run("workbench.action.splitEditorDown")
		a.wantShape("H(1 V(2 3))")
		return a
	}
	t.Run("join all groups", func(t *testing.T) {
		a := setup(t)
		a.run("workbench.action.joinAllGroups")
		a.wantShape("H(3)")
		if got := a.names(3); !slices.Equal(got, []string{"b.go", "a.go"}) && len(got) != 2 {
			t.Errorf("the group has %v", got)
		}
		a.noErrors()
	})
	t.Run("join two groups goes to the neighbor on the right first", func(t *testing.T) {
		a := setup(t)
		a.run("workbench.action.focusFirstEditorGroup")
		a.run("workbench.action.joinTwoGroups")
		if len(a.ids()) != 2 || a.grid().Group(1) != nil {
			t.Errorf("groups %v after joining group 1", a.ids())
		}
		a.noErrors()
	})
	t.Run("close group removes an empty group", func(t *testing.T) {
		a := setup(t)
		a.setting("workbench.editor.closeEmptyGroups", false)
		a.run("workbench.action.closeEditorsInGroup")
		if !a.group(3).Empty() || a.grid().Count() != 3 {
			t.Fatalf("the group is empty and stays: %v %d", a.group(3).Empty(), a.grid().Count())
		}
		a.press("ctrl+w")
		a.wantShape("H(1 2)")
		a.noErrors()
	})
	t.Run("closing the last editor closes the group", func(t *testing.T) {
		a := setup(t)
		a.press("ctrl+w")
		a.wantShape("H(1 2)")
		if a.activeID() == 3 || a.grid().Group(3) != nil {
			t.Errorf("active %d", a.activeID())
		}
		a.noErrors()
	})
	t.Run("close all groups", func(t *testing.T) {
		a := setup(t)
		a.press("ctrl+k ctrl+shift+w")
		a.wantShape("H(1)")
		if a.grid().Count() != 1 || !a.group(a.activeID()).Empty() {
			t.Errorf("%d groups left", a.grid().Count())
		}
		a.noErrors()
	})
	t.Run("close all editors closes the editors of every group", func(t *testing.T) {
		a := setup(t)
		a.setting("workbench.editor.closeEmptyGroups", false)
		a.press("ctrl+k ctrl+w")
		for _, g := range a.grid().Groups(editorgroup.CreationTime) {
			if !g.Empty() {
				t.Errorf("group %d has %v", g.ID(), a.names(g.ID()))
			}
		}
		if a.grid().Count() != 3 {
			t.Errorf("%d groups", a.grid().Count())
		}
		a.noErrors()
	})
	t.Run("close editors in other groups", func(t *testing.T) {
		a := setup(t)
		a.run("workbench.action.closeEditorsInOtherGroups")
		a.wantShape("H(3)")
		a.noErrors()
	})
	t.Run("close editors and group", func(t *testing.T) {
		a := setup(t)
		a.setting("workbench.editor.closeEmptyGroups", false)
		a.run("workbench.action.closeEditorsAndGroup")
		a.wantShape("H(1 2)")
		a.noErrors()
	})
	t.Run("close the editor in all groups", func(t *testing.T) {
		a := setup(t)
		// the active editor, a.go, is in the three groups
		a.run("workbench.action.closeEditorInAllGroups")
		a.wantShape("H(1)")
		if got := a.names(1); !slices.Equal(got, []string{"b.go"}) {
			t.Errorf("group 1 has %v", got)
		}
		a.noErrors()
	})
}

func TestMovingAndDuplicatingGroups(t *testing.T) {
	setup := func(t *testing.T) *groupApp {
		a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go")
		a.run("workbench.action.splitEditorRight") // 2 right of 1
		a.run("workbench.action.newGroupRight")    // 3 right of 2, active
		a.wantShape("H(1 2 3)")
		return a
	}
	for _, tc := range []struct {
		name  string
		keys  string
		cmd   string
		from  int
		shape string
	}{
		{"Ctrl+K Left moves the group left", "ctrl+k left", "", 3, "H(1 3 2)"},
		{"Ctrl+K Right moves the group right", "ctrl+k right", "", 1, "H(2 1 3)"},
		{"Ctrl+K Down moves the group below the neighbor", "ctrl+k down", "", 3, "H(1 V(2 3))"},
		{"Ctrl+K Up moves the group above the neighbor", "ctrl+k up", "", 3, "H(1 V(3 2))"},
		{"a group at the edge has no neighbor to move past", "ctrl+k right", "", 3, "H(1 2 3)"},
		{"duplicate right", "", "workbench.action.duplicateActiveEditorGroupRight", 1, "H(1 4 2 3)"},
		{"duplicate down", "", "workbench.action.duplicateActiveEditorGroupDown", 1, "H(V(1 4) 2 3)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := setup(t)
			if err := a.grid().Activate(a.group(tc.from)); err != nil {
				t.Fatal(err)
			}
			a.pump()
			if tc.keys != "" {
				a.press(tc.keys)
			} else {
				a.run(tc.cmd)
			}
			a.wantShape(tc.shape)
			a.noErrors()
		})
	}
	t.Run("a duplicate has copies of the editors", func(t *testing.T) {
		a := setup(t)
		a.run("workbench.action.focusFirstEditorGroup")
		a.run("workbench.action.duplicateActiveEditorGroupRight")
		if got := a.names(4); !slices.Equal(got, []string{"a.go"}) {
			t.Errorf("the duplicate has %v", got)
		}
		doc := a.doc("a.go")
		if got := a.win.Documents.Refs(doc); got != 3 {
			t.Errorf("a.go has %d users, want 3 (groups 1, 2 and 4)", got)
		}
		if a.activeID() != 4 {
			t.Errorf("active %d", a.activeID())
		}
		a.noErrors()
	})
}

func TestTwoEditorsOfOneDocument(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.run("workbench.action.splitEditorRight")
	doc := a.doc("a.go")
	first, second := a.editorOf(1), a.editorOf(2)
	if first == second || first.view == second.view {
		t.Fatal("each group has an editor with a view model of its own")
	}
	if first.doc != doc || second.doc != doc {
		t.Fatal("the editors share the document")
	}
	if got := a.win.Documents.Refs(doc); got != 2 {
		t.Errorf("the document has %d users, want 2", got)
	}
	// an edit in one shows in the other, and in both tabs
	a.edit("a.go")
	if first.view.Document().Text() != second.view.Document().Text() || !strings.HasPrefix(second.view.Document().Text(), "x") {
		t.Errorf("the texts differ: %q %q", first.view.Document().Text(), second.view.Document().Text())
	}
	for _, id := range []int{1, 2} {
		finder := gluttertest.Descendant(gluttertest.ByKey(a.shell().view(a.group(id)).key), gluttertest.ByType[*kit.Tabs]())
		if items := a.ts.Widget(finder).(*kit.Tabs).Items(); len(items) != 1 || !items[0].Dirty {
			t.Errorf("group %d: tabs %+v", id, items)
		}
	}
	// the cursors are the editors' own
	first.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 1, Col: 3})})
	second.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 0, Col: 2})})
	if first.view.Cursors().Primary().Selection.Active == second.view.Cursors().Primary().Selection.Active {
		t.Error("the cursors are the same")
	}
	// the marks they put on the document are their own: both search, then one closes its search
	for _, ed := range []*textEditor{first, second} {
		ed.view.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 1, Col: 3}, Active: buffer.Position{Line: 1, Col: 4}}})
		if err := ed.view.RunCommand("actions.find", nil); err != nil {
			t.Fatal(err)
		}
	}
	owners := func() map[int]int {
		m := map[int]int{}
		for _, d := range doc.AllDecorations() {
			m[d.Owner]++
		}
		return m
	}
	before := owners()
	first.view.DisposeFind()
	after := owners()
	if len(after) >= len(before) {
		t.Errorf("owners before %v and after the first editor closed its search %v: marks are shared", before, after)
	}
	// a view model of the document that is not shown any more takes its marks off
	a.run("workbench.action.closeActiveEditor")
	if len(a.grid().Groups(editorgroup.CreationTime)) != 1 {
		t.Fatalf("groups %v", a.ids())
	}
	if got := a.win.Documents.Refs(doc); got != 1 {
		t.Errorf("the document has %d users after a group closed, want 1", got)
	}
	if len(a.shell().group.editors) != 1 {
		t.Errorf("%d editors left", len(a.shell().group.editors))
	}
	if _, open := a.win.Documents.Get(doc.URI()); !open {
		t.Error("the document closed while a group shows it")
	}
	if !second.disposed && a.grid().Active().Model.ActiveEditor() != second.input {
		t.Error("the editor of the group that closed was not disposed")
	}
	// the last editor of a document with unsaved changes asks first
	a.press("ctrl+w")
	if !a.win.Dialogs.IsShowing() {
		t.Fatal("closing the last editor of a document with unsaved changes did not ask")
	}
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText("Don't Save")))
	a.pump()
	if _, open := a.win.Documents.Get(doc.URI()); open {
		t.Error("the document stays open after its last editor closed")
	}
	a.noErrors()
}

func TestAnEditorOfTheSameFileIsNotAddedTwiceToAGroup(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.run("workbench.action.splitEditorRight")
	a.show("a.go", false)
	if got := a.names(2); !slices.Equal(got, []string{"a.go"}) {
		t.Errorf("group 2 has %v", got)
	}
	if got := a.win.Documents.Refs(a.doc("a.go")); got != 2 {
		t.Errorf("%d users, want 2", got)
	}
	a.noErrors()
}

func TestOpeningAFileInAGroup(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go")
	a.run("workbench.action.splitEditorRight")
	a.run("workbench.action.focusFirstEditorGroup")
	// a plain open goes to the active group
	a.show("b.go", false)
	if got := a.names(1); !slices.Equal(got, []string{"a.go", "b.go"}) {
		t.Errorf("group 1 has %v", got)
	}
	// an explicit group is used, and becomes the active group
	doc, err := a.win.Documents.Acquire(context.Background(), a.files["c.go"], document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	s := a.shell()
	s.show(widgets.WidgetsBindingOf(s.Context()), doc, openOptions{Group: a.group(2)})
	a.pump()
	if got := a.names(2); !slices.Equal(got, []string{"a.go", "c.go"}) {
		t.Errorf("group 2 has %v", got)
	}
	if a.activeID() != 2 {
		t.Errorf("active group %d", a.activeID())
	}
	a.noErrors()
}

func TestDevOpenFileInAGroup(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go")
	a.run("workbench.action.splitEditorDown")
	a.run("workbench.action.focusFirstEditorGroup")
	// group 2 is the second in the grid
	if err := a.runWith("dev.openFile", map[string]any{"path": a.files["b.go"], "group": 2.0}); err != nil {
		t.Fatal(err)
	}
	if got := a.names(2); !slices.Equal(got, []string{"a.go", "b.go"}) || a.activeID() != 2 {
		t.Errorf("group 2 has %v, active %d", got, a.activeID())
	}
	if err := a.runWith("dev.openFile", map[string]any{"path": a.files["c.go"], "group": 1.0, "line": 2.0, "column": 3.0}); err != nil {
		t.Fatal(err)
	}
	if got := a.names(1); !slices.Equal(got, []string{"a.go", "c.go"}) || a.activeID() != 1 {
		t.Errorf("group 1 has %v, active %d", got, a.activeID())
	}
	if got := a.editorOf(1).view.Cursors().Primary().Selection.Active; got != (buffer.Position{Line: 1, Col: 2}) {
		t.Errorf("the cursor is at %v", got)
	}
	// without a group it is the active group
	if err := a.runWith("dev.openFile", a.files["b.go"]); err != nil {
		t.Fatal(err)
	}
	if got := a.names(1); !slices.Equal(got, []string{"a.go", "c.go", "b.go"}) {
		t.Errorf("group 1 has %v", got)
	}

	for _, tc := range []struct {
		name  string
		group any
		route string
	}{
		{"a group that does not exist", 3.0, "devOpenFile.DevOpenFileGroupInvalid"},
		{"far away", 99.0, "devOpenFile.DevOpenFileGroupInvalid"},
		{"zero", 0.0, "parseOpenFile.DevOpenFileInvalid"},
		{"negative", -1.0, "parseOpenFile.DevOpenFileInvalid"},
		{"a fraction", 1.5, "parseOpenFile.DevOpenFileInvalid"},
		{"a string", "2", "parseOpenFile.DevOpenFileInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := a.runWith("dev.openFile", map[string]any{"path": a.files["b.go"], "group": tc.group})
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	a.noErrors()
}

func TestTheGridCommandsAreBound(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	reg := a.win.Commands
	var missing []string
	for _, id := range []string{
		"workbench.action.splitEditor", "workbench.action.splitEditorOrthogonal", "workbench.action.splitEditorLeft",
		"workbench.action.focusFirstEditorGroup", "workbench.action.focusEighthEditorGroup", "workbench.action.moveEditorToNextGroup",
		"workbench.action.joinAllGroups", "workbench.action.toggleMaximizeEditorGroup", "workbench.action.evenEditorWidths",
		"workbench.action.closeGroup", "workbench.action.closeAllGroups", "moveActiveEditor", "copyActiveEditor", "layoutEditorGroups",
		"workbench.action.editorLayoutTwoByTwoGrid", "workbench.action.newGroupBelow", "workbench.action.moveActiveEditorGroupLeft",
		"workbench.action.duplicateActiveEditorGroupUp", "workbench.action.splitEditorToLastGroup",
	} {
		if !reg.HasHandler(id) {
			missing = append(missing, id)
		}
	}
	if len(missing) != 0 {
		t.Errorf("no handler for %v", missing)
	}
}

func TestMoveAndCopyArgumentsAreChecked(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	for _, args := range []any{
		map[string]any{"to": "sideways", "by": "group"},
		map[string]any{"to": "right", "by": "window"},
		map[string]any{"to": "sideways", "by": "tab"},
	} {
		err := a.runWith("moveActiveEditor", args)
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Errorf("%v: expected *errors.Error, got %T (%v)", args, err, err)
			continue
		}
		if !appErr.HasRoute("moveCopyActive.EditorMoveArgumentInvalid") && !appErr.HasRoute("moveTab.EditorMoveArgumentInvalid") &&
			!appErr.HasRoute("MoveOrCopyTarget.GroupTargetInvalid") {
			t.Errorf("%v: unexpected failure path: %s", args, appErr.Route())
		}
	}
}

// menuItemsOf are the items of the menu that is open.
func menuItemsOf(t *testing.T, ts *gluttertest.Tester) []kit.MenuItem {
	t.Helper()
	els := ts.Elements(gluttertest.ByType[*kit.Menu]())
	if len(els) == 0 {
		t.Fatal("no menu is open")
	}
	return els[len(els)-1].Widget().(*kit.Menu).Items()
}

func TestEditorLayoutAndSwitchGroupMenus(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.ts.Tap(gluttertest.ByText("View"))
	a.pump()
	if _, ok := menuItem(menuItemsOf(t, a.ts), "workbench.action.editorLayoutTwoColumns"); !ok {
		t.Fatal("no Two Columns in View > Editor Layout")
	}
	a.ts.Tap(gluttertest.ByText("Editor Layout"))
	a.pump()
	a.ts.Tap(gluttertest.ByText("Two Columns"))
	a.pump()
	st := a.shell()
	if st.group.grid.Count() != 2 {
		t.Errorf("%d groups after View > Editor Layout > Two Columns", st.group.grid.Count())
	}
	// "Go" is also the language in the status bar: the menu is the one in the menu bar
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.MenuBar](), gluttertest.ByText("Go")))
	a.pump()
	a.ts.Tap(gluttertest.ByText("Switch Group"))
	a.pump()
	items := menuItemsOf(t, a.ts)
	if it, ok := menuItem(items, "workbench.action.focusFirstEditorGroup"); !ok || it.Keybinding != "Ctrl+1" {
		t.Errorf("Group 1: %+v %v", it, ok)
	}
	if it, ok := menuItem(items, "workbench.action.focusNextGroup"); !ok || it.Disabled {
		t.Errorf("Next Group with two groups: %+v %v", it, ok)
	}
	// the group numbers above the first two are enabled as soon as there are several groups, as VS Code has them
	if it, ok := menuItem(items, "workbench.action.focusThirdEditorGroup"); !ok || it.Disabled {
		t.Errorf("Group 3: %+v %v", it, ok)
	}
	a.ts.Tap(gluttertest.ByText("Group 1"))
	a.pump()
	if st.activeGroup().ID() != 1 {
		t.Errorf("active group %d after Switch Group > Group 1", st.activeGroup().ID())
	}
	a.noErrors()
}
