package editorgroup

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// copyInput is a test input that can be shown in several groups: its copies match it.
type copyInput struct {
	testInput
}

func newCopyInput(id string) *copyInput { return &copyInput{testInput{id: id}} }

func (c *copyInput) CopyInput() EditorInput { return newCopyInput(c.id) }

func (c *copyInput) Matches(other EditorInput) bool {
	o, ok := other.(*copyInput)
	return ok && o != nil && o.id == c.id
}

// shape writes the tree of the grid: H(...) and V(...) for splits along the axes, the id of a group.
func shape(g *Grid) string {
	var walk func(n Node) string
	walk = func(n Node) string {
		if n.Group != nil {
			return fmt.Sprint(n.Group.ID())
		}
		var parts []string
		for _, c := range n.Children {
			parts = append(parts, walk(c))
		}
		o := "H"
		if n.Orientation == Vertical {
			o = "V"
		}
		return o + "(" + strings.Join(parts, " ") + ")"
	}
	return walk(g.Tree())
}

// sizesAt are the sizes of the split at a path in percent.
func sizesAt(g *Grid, path ...int) []int {
	var out []int
	for _, s := range g.Sizes(path) {
		out = append(out, int(math.Round(s*100)))
	}
	return out
}

func ids(groups []*Group) []int {
	out := make([]int, len(groups))
	for i, g := range groups {
		out[i] = g.ID()
	}
	return out
}

func mustAdd(t *testing.T, g *Grid, at int, dir Direction) *Group {
	t.Helper()
	grp, err := g.AddGroup(g.Group(at), dir)
	if err != nil {
		t.Fatal(err)
	}
	return grp
}

func TestGridSplitDirections(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(t *testing.T, g *Grid)
		want string
	}{
		{"one group", func(t *testing.T, g *Grid) {}, "H(1)"},
		{"right", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight) }, "H(1 2)"},
		{"left", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirLeft) }, "H(2 1)"},
		{"down", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirDown) }, "V(1 2)"},
		{"up", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirUp) }, "V(2 1)"},
		{"right twice", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirRight) }, "H(1 2 3)"},
		{"right of the first", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 1, DirRight) }, "H(1 3 2)"},
		{"down in a column", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirDown) }, "H(1 V(2 3))"},
		{"up in a column", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirUp) }, "H(1 V(3 2))"},
		{"right in a nested row", func(t *testing.T, g *Grid) {
			mustAdd(t, g, 1, DirRight)
			mustAdd(t, g, 2, DirDown)
			mustAdd(t, g, 3, DirRight)
		}, "H(1 V(2 H(3 4)))"},
		{"down in a nested split goes beside its sibling", func(t *testing.T, g *Grid) {
			mustAdd(t, g, 1, DirRight)
			mustAdd(t, g, 2, DirDown)
			mustAdd(t, g, 3, DirDown)
		}, "H(1 V(2 3 4))"},
		{"the first group split down", func(t *testing.T, g *Grid) {
			mustAdd(t, g, 1, DirRight)
			mustAdd(t, g, 1, DirDown)
		}, "H(V(1 3) 2)"},
		{"two by two", func(t *testing.T, g *Grid) {
			mustAdd(t, g, 1, DirRight)
			mustAdd(t, g, 1, DirDown)
			mustAdd(t, g, 2, DirDown)
		}, "H(V(1 3) V(2 4))"},
		{"a single group changes the axis of the root", func(t *testing.T, g *Grid) {
			mustAdd(t, g, 1, DirDown)
			mustAdd(t, g, 1, DirRight)
		}, "V(H(1 3) 2)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGrid(DefaultGridOptions())
			tc.do(t, g)
			if got := shape(g); got != tc.want {
				t.Errorf("tree %s, want %s", got, tc.want)
			}
		})
	}
}

func TestGridSplitSizing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sizing SplitSizing
		// resize sets the sizes of the root after the first split
		resize []float64
		// want are the sizes after group 1 was split again: the groups lie as 1 3 2
		want []int
	}{
		{"auto: equal groups are all made equal", SizingAuto, nil, []int{33, 33, 33}},
		{"auto: unequal groups split the one group", SizingAuto, []float64{0.7, 0.3}, []int{35, 35, 30}},
		{"distribute: always equal", SizingDistribute, []float64{0.7, 0.3}, []int{33, 33, 33}},
		{"split: always splits the group", SizingSplit, nil, []int{25, 25, 50}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := DefaultGridOptions()
			opts.SplitSizing = tc.sizing
			g := NewGrid(opts)
			mustAdd(t, g, 1, DirRight)
			if tc.resize != nil {
				g.SetSizes(nil, tc.resize)
			}
			mustAdd(t, g, 1, DirRight)
			if got := sizesAt(g); !slices.Equal(got, tc.want) {
				t.Errorf("sizes %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGridNewSplitOfOneGroupIsHalf(t *testing.T) {
	for _, dir := range []Direction{DirUp, DirDown, DirLeft, DirRight} {
		g := NewGrid(DefaultGridOptions())
		mustAdd(t, g, 1, dir)
		if got := sizesAt(g); !slices.Equal(got, []int{50, 50}) {
			t.Errorf("direction %d: sizes %v", dir, got)
		}
	}
}

func TestGridRemoveGroups(t *testing.T) {
	for _, tc := range []struct {
		name   string
		build  func(t *testing.T, g *Grid)
		remove int
		want   string
	}{
		{"the last of two", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight) }, 2, "H(1)"},
		{"the first of three", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirRight) }, 1, "H(2 3)"},
		{"a split with one child left folds", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirDown) }, 3, "H(1 2)"},
		{"the root gets the axis of the split that is left", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirDown) }, 1, "V(2 3)"},
		{"a nested split is hoisted", func(t *testing.T, g *Grid) {
			mustAdd(t, g, 1, DirRight)
			mustAdd(t, g, 2, DirDown)
			mustAdd(t, g, 3, DirRight)
		}, 2, "H(1 3 4)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGrid(DefaultGridOptions())
			tc.build(t, g)
			if removed, err := g.RemoveGroup(g.Group(tc.remove)); err != nil || !removed {
				t.Fatalf("RemoveGroup: %v %v", removed, err)
			}
			if got := shape(g); got != tc.want {
				t.Errorf("tree %s, want %s", got, tc.want)
			}
			if g.Group(tc.remove) != nil {
				t.Error("the group is still in the grid")
			}
		})
	}
}

func TestGridRemoveGivesTheRoomToTheNeighbor(t *testing.T) {
	opts := DefaultGridOptions()
	opts.SplitSizing = SizingSplit
	g := NewGrid(opts)
	mustAdd(t, g, 1, DirRight)
	mustAdd(t, g, 2, DirRight) // 50 25 25
	if got := sizesAt(g); !slices.Equal(got, []int{50, 25, 25}) {
		t.Fatalf("sizes %v", got)
	}
	if _, err := g.RemoveGroup(g.Group(3)); err != nil {
		t.Fatal(err)
	}
	if got := sizesAt(g); !slices.Equal(got, []int{50, 50}) {
		t.Errorf("sizes %v after removing the last, want the left neighbor to take its room", got)
	}
	// auto with equal sizes distributes
	g = NewGrid(DefaultGridOptions())
	mustAdd(t, g, 1, DirRight)
	mustAdd(t, g, 2, DirRight)
	_, _ = g.RemoveGroup(g.Group(2))
	if got := sizesAt(g); !slices.Equal(got, []int{50, 50}) {
		t.Errorf("sizes %v after removing from three equal groups", got)
	}
}

func TestGridTheLastGroupCannotBeRemoved(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	removed, err := g.RemoveGroup(g.Group(1))
	if err != nil || removed {
		t.Errorf("removed %v, err %v", removed, err)
	}
}

func TestGridFindByDirection(t *testing.T) {
	// 1 | (2 over 3), and 4 right of 3 inside the column: H(1 V(2 H(3 4)))
	build := func(t *testing.T) *Grid {
		g := NewGrid(DefaultGridOptions())
		mustAdd(t, g, 1, DirRight)
		mustAdd(t, g, 2, DirDown)
		mustAdd(t, g, 3, DirRight)
		return g
	}
	for _, tc := range []struct {
		name string
		from int
		dir  Direction
		wrap bool
		want int // 0 for none
	}{
		{"right of the first", 1, DirRight, false, 2},
		{"left of 2", 2, DirLeft, false, 1},
		{"left of 3", 3, DirLeft, false, 1},
		{"left of 4", 4, DirLeft, false, 3},
		{"up from 3", 3, DirUp, false, 2},
		{"up from 4", 4, DirUp, false, 2},
		{"down from 2", 2, DirDown, false, 3},
		{"nothing left of the first", 1, DirLeft, false, 0},
		{"nothing right of 4", 4, DirRight, false, 0},
		{"nothing below 3", 3, DirDown, false, 0},
		{"wrap right from 4", 4, DirRight, true, 1},
		{"wrap left from 1", 1, DirLeft, true, 2},
		{"wrap down from 3", 3, DirDown, true, 2},
		{"wrap up from 2", 2, DirUp, true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := build(t)
			got := g.FindByDirection(g.Group(tc.from), tc.dir, tc.wrap)
			if tc.want == 0 {
				if got != nil {
					t.Errorf("found %d, want none", got.ID())
				}
				return
			}
			if got == nil || got.ID() != tc.want {
				t.Errorf("found %v, want %d", got, tc.want)
			}
		})
	}
}

func TestGridFindByDirectionPrefersTheMostRecentlyActive(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	mustAdd(t, g, 1, DirRight)
	mustAdd(t, g, 2, DirDown)
	// 2 is above 3 at the right of 1
	// a group that was never active comes first in VS Code's sort, so both have been active
	_ = g.Activate(g.Group(2))
	_ = g.Activate(g.Group(3))
	for _, tc := range []struct{ active, want int }{{2, 2}, {3, 3}} {
		_ = g.Activate(g.Group(tc.active))
		_ = g.Activate(g.Group(1))
		if got := g.FindByDirection(g.Group(1), DirRight, false); got.ID() != tc.want {
			t.Errorf("after %d was active: %d, want %d", tc.active, got.ID(), tc.want)
		}
	}
}

func TestGridFindByLocation(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	mustAdd(t, g, 1, DirRight)
	mustAdd(t, g, 2, DirDown)
	// grid order: 1 2 3
	for _, tc := range []struct {
		name string
		from int
		loc  Location
		wrap bool
		want int
	}{
		{"first", 3, LocationFirst, false, 1},
		{"last", 1, LocationLast, false, 3},
		{"next", 1, LocationNext, false, 2},
		{"next of the last", 3, LocationNext, false, 0},
		{"next of the last wraps", 3, LocationNext, true, 1},
		{"previous", 3, LocationPrevious, false, 2},
		{"previous of the first", 1, LocationPrevious, false, 0},
		{"previous of the first wraps", 1, LocationPrevious, true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := g.FindByLocation(g.Group(tc.from), tc.loc, tc.wrap)
			if (got == nil) != (tc.want == 0) || got != nil && got.ID() != tc.want {
				t.Errorf("found %v, want %d", got, tc.want)
			}
		})
	}
}

func TestGridGroupOrders(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	mustAdd(t, g, 1, DirRight) // 2
	mustAdd(t, g, 1, DirDown)  // 3 under 1: H(V(1 3) 2)
	_ = g.Activate(g.Group(2))
	_ = g.Activate(g.Group(1))
	for _, tc := range []struct {
		name  string
		order GroupsOrder
		want  []int
	}{
		{"creation time", CreationTime, []int{1, 2, 3}},
		{"grid appearance", GridAppearance, []int{1, 3, 2}},
		{"most recently active", MostRecentlyActiveGroups, []int{1, 2, 3}},
	} {
		if got := ids(g.Groups(tc.order)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if g.Index(g.Group(3)) != 1 || g.Index(&Group{Model: NewModel(9, Options{})}) != -1 {
		t.Error("Index is wrong")
	}
}

func TestGridFocusIndex(t *testing.T) {
	for _, tc := range []struct {
		name      string
		groups    int // groups before
		n         int
		preferred Direction
		want      int // the id of the group found, 0 for none
		count     int // groups after
		shape     string
	}{
		{"an existing group", 3, 2, DirRight, 2, 3, "H(1 2 3)"},
		{"the first", 3, 1, DirRight, 1, 3, "H(1 2 3)"},
		{"the group after the last is added right", 2, 3, DirRight, 3, 3, "H(1 2 3)"},
		{"or below", 2, 3, DirDown, 3, 3, "H(1 V(2 3))"},
		{"further away finds nothing", 2, 4, DirRight, 0, 2, "H(1 2)"},
		{"zero finds nothing", 2, 0, DirRight, 0, 2, "H(1 2)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGrid(DefaultGridOptions())
			for i := 1; i < tc.groups; i++ {
				mustAdd(t, g, i, DirRight)
			}
			got, err := g.FocusIndex(tc.n, tc.preferred)
			if err != nil {
				t.Fatal(err)
			}
			if (got == nil) != (tc.want == 0) || got != nil && got.ID() != tc.want {
				t.Errorf("found %v, want %d", got, tc.want)
			}
			if g.Count() != tc.count || shape(g) != tc.shape {
				t.Errorf("%d groups, tree %s, want %d, %s", g.Count(), shape(g), tc.count, tc.shape)
			}
		})
	}
}

func TestGridActivationAndTheRecentOrder(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	mustAdd(t, g, 1, DirRight)
	mustAdd(t, g, 2, DirRight)
	var events []GridChangeKind
	g.OnChange(func(e GridEvent) { events = append(events, e.Kind) })
	if err := g.Activate(g.Group(3)); err != nil {
		t.Fatal(err)
	}
	if g.Active().ID() != 3 || !slices.Equal(ids(g.Groups(MostRecentlyActiveGroups)), []int{3, 1, 2}) {
		t.Errorf("active %d, recent %v", g.Active().ID(), ids(g.Groups(MostRecentlyActiveGroups)))
	}
	_ = g.Activate(g.Group(3))
	if !slices.Equal(events, []GridChangeKind{ActiveGroupChanged}) {
		t.Errorf("events %v: a group that is active already is not announced again", events)
	}
	// removing the active group activates the group that was active before it
	_ = g.Activate(g.Group(2))
	_ = g.Activate(g.Group(3))
	if _, err := g.RemoveGroup(g.Group(3)); err != nil {
		t.Fatal(err)
	}
	if g.Active().ID() != 2 {
		t.Errorf("active %d after removing the active group, want 2", g.Active().ID())
	}
}

func TestGridErrors(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	other := NewGrid(DefaultGridOptions())
	stranger := other.Group(1)
	mustAdd(t, g, 1, DirRight)
	for _, tc := range []struct {
		name  string
		do    func() error
		route string
	}{
		{"add next to a stranger", func() error { _, err := g.AddGroup(stranger, DirRight); return err }, "Grid.AddGroup->Grid.addGroup.GroupUnknown"},
		{"remove a stranger", func() error { _, err := g.RemoveGroup(stranger); return err }, "Grid.RemoveGroup.GroupUnknown"},
		{"activate a stranger", func() error { return g.Activate(stranger) }, "Grid.Activate.GroupUnknown"},
		{"move onto itself", func() error { return g.MoveGroup(g.Group(1), g.Group(1), DirRight) }, "Grid.MoveGroup.GroupMoveOntoItself"},
		{"merge onto itself", func() error { _, err := g.MergeGroup(g.Group(1), g.Group(1)); return err }, "Grid.MergeGroup.GroupMergeOntoItself"},
		{"an unknown target", func() error { _, err := g.MoveOrCopyTarget(g.Group(1), "sideways", 0, DirRight); return err }, "Grid.MoveOrCopyTarget.GroupTargetInvalid"},
		{"an empty layout", func() error { return g.ApplyLayout(Layout{}) }, "Grid.ApplyLayout.LayoutInvalid"},
		{"a layout with another orientation", func() error {
			return g.ApplyLayout(Layout{Orientation: 5, Groups: []LayoutNode{{}}})
		}, "Grid.ApplyLayout.LayoutInvalid"},
		{"a saved grid with no group", func() error { _, err := RestoreGrid(SerializedGrid{}, DefaultGridOptions(), nil); return err }, "RestoreGridWith.GridStateInvalid"},
		{"a saved node that is both", func() error {
			_, err := RestoreGrid(SerializedGrid{Root: []SerializedNode{{Model: &SerializedModel{ID: 1}, Groups: []SerializedNode{{}, {}}}}}, DefaultGridOptions(), nil)
			return err
		}, "RestoreGridWith.func1.GridStateInvalid"},
		{"a saved group id used twice", func() error {
			m := &SerializedModel{ID: 1}
			_, err := RestoreGrid(SerializedGrid{Root: []SerializedNode{{Model: m}, {Model: m}}}, DefaultGridOptions(), nil)
			return err
		}, "RestoreGridWith.func1.GridStateInvalid"},
		{"a saved orientation that is not known", func() error {
			_, err := RestoreGrid(SerializedGrid{Orientation: 3, Root: []SerializedNode{{Model: &SerializedModel{ID: 1}}}}, DefaultGridOptions(), nil)
			return err
		}, "RestoreGridWith.GridStateInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.do()
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tc.route)
			}
		})
	}
}

func TestGridMaximize(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	if err := g.Maximize(g.Group(1)); err != nil || g.Maximized() != nil {
		t.Fatalf("a single group cannot be maximized: %v %v", err, g.Maximized())
	}
	mustAdd(t, g, 1, DirRight)
	mustAdd(t, g, 2, DirDown)
	g.SetSizes(nil, []float64{0.3, 0.7})
	before := g.Layout()
	if err := g.ToggleMaximize(g.Group(3)); err != nil {
		t.Fatal(err)
	}
	if g.Maximized() == nil || g.Maximized().ID() != 3 {
		t.Fatalf("maximized %v", g.Maximized())
	}
	for id, want := range map[int]bool{1: true, 2: true, 3: false} {
		if g.Hidden(g.Group(id)) != want {
			t.Errorf("group %d hidden %v, want %v", id, g.Hidden(g.Group(id)), want)
		}
	}
	// the sizes are kept
	if !layoutsEqual(g.Layout(), before) {
		t.Errorf("maximizing changed the sizes: %v, want %v", g.Layout(), before)
	}
	// activating another group shows the groups again
	if err := g.Activate(g.Group(1)); err != nil {
		t.Fatal(err)
	}
	if g.Maximized() != nil {
		t.Error("activating another group leaves the maximized group")
	}
	// toggling twice
	_ = g.ToggleMaximize(g.Group(2))
	_ = g.ToggleMaximize(g.Group(2))
	if g.Maximized() != nil {
		t.Error("a second toggle does not restore")
	}
	// adding, removing, moving and evening out exit
	// in this order: "remove" takes the group that "add" made
	for _, c := range []struct {
		name string
		do   func()
	}{
		{"add", func() { mustAdd(t, g, 1, DirDown) }},
		{"remove", func() { _, _ = g.RemoveGroup(g.Group(4)) }},
		{"even", func() { g.DistributeSizes() }},
		{"move", func() { _ = g.MoveGroup(g.Group(1), g.Group(2), DirRight) }},
	} {
		_ = g.Maximize(g.Group(2))
		c.do()
		if g.Maximized() != nil {
			t.Errorf("%s keeps the group maximized", c.name)
		}
	}
}

func layoutsEqual(a, b Layout) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestGridDistributeSizes(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	mustAdd(t, g, 1, DirRight)
	mustAdd(t, g, 2, DirDown)
	g.SetSizes(nil, []float64{0.8, 0.2})
	g.SetSizes([]int{1}, []float64{0.9, 0.1})
	v := g.Version()
	g.DistributeSizes()
	if got := sizesAt(g); !slices.Equal(got, []int{50, 50}) {
		t.Errorf("root sizes %v", got)
	}
	if got := sizesAt(g, 1); !slices.Equal(got, []int{50, 50}) {
		t.Errorf("nested sizes %v", got)
	}
	if g.Version() == v {
		t.Error("evening out does not change the version")
	}
}

func TestGridSetSizes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		path  []int
		sizes []float64
		want  []int
	}{
		{"normalised", nil, []float64{300, 100}, []int{75, 25}},
		{"nested", []int{1}, []float64{1, 3}, []int{25, 75}},
		{"a wrong number is ignored", nil, []float64{1, 1, 1}, []int{50, 50}},
		{"a size that is not positive is ignored", nil, []float64{1, 0}, []int{50, 50}},
		{"a path to a group is ignored", []int{0}, []float64{1}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGrid(DefaultGridOptions())
			mustAdd(t, g, 1, DirRight)
			mustAdd(t, g, 2, DirDown)
			g.Sizes(nil)
			v := g.Version()
			g.SetSizes(tc.path, tc.sizes)
			if got := sizesAt(g, tc.path...); !slices.Equal(got, tc.want) {
				t.Errorf("sizes %v, want %v", got, tc.want)
			}
			if g.Version() != v {
				t.Error("dragging a sash changes the version")
			}
		})
	}
}

func TestGridMoveGroup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		build  func(t *testing.T, g *Grid)
		move   int
		target int
		dir    Direction
		want   string
	}{
		{"right of its neighbor", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirRight) }, 1, 2, DirRight, "H(2 1 3)"},
		{"left of its neighbor", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirRight) }, 3, 2, DirLeft, "H(1 3 2)"},
		{"to the end", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirRight) }, 1, 3, DirRight, "H(2 3 1)"},
		{"onto the same place", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirRight) }, 2, 1, DirRight, "H(1 2 3)"},
		{"below a group of another split", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirRight) }, 1, 3, DirDown, "H(2 V(3 1))"},
		{"into the column", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight); mustAdd(t, g, 2, DirDown) }, 1, 2, DirDown, "V(2 1 3)"},
		{"two groups swap axes", func(t *testing.T, g *Grid) { mustAdd(t, g, 1, DirRight) }, 1, 2, DirDown, "V(2 1)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGrid(DefaultGridOptions())
			tc.build(t, g)
			if err := g.MoveGroup(g.Group(tc.move), g.Group(tc.target), tc.dir); err != nil {
				t.Fatal(err)
			}
			if got := shape(g); got != tc.want {
				t.Errorf("tree %s, want %s", got, tc.want)
			}
		})
	}
}

func TestGridMoveGroupKeepsTheSizesOfAReorderedSplit(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	mustAdd(t, g, 1, DirRight)
	mustAdd(t, g, 2, DirRight)
	g.SetSizes(nil, []float64{0.5, 0.3, 0.2})
	_ = g.MoveGroup(g.Group(1), g.Group(3), DirRight)
	if got := sizesAt(g); !slices.Equal(got, []int{30, 20, 50}) {
		t.Errorf("sizes %v: a group keeps its size when it moves in its split", got)
	}
}

func TestGridLayouts(t *testing.T) {
	one := []LayoutNode{{}}
	two := []LayoutNode{{}, {}}
	three := []LayoutNode{{}, {}, {}}
	for _, tc := range []struct {
		name   string
		groups int // groups before, side by side
		layout Layout
		want   string
		count  int
	}{
		{"single", 1, Layout{Horizontal, one}, "H(1)", 1},
		{"two columns", 1, Layout{Horizontal, two}, "H(1 2)", 2},
		{"three columns", 1, Layout{Horizontal, three}, "H(1 2 3)", 3},
		{"two rows", 1, Layout{Vertical, two}, "V(1 2)", 2},
		{"three rows", 2, Layout{Vertical, three}, "V(1 2 3)", 3},
		{"grid 2x2", 1, Layout{Horizontal, []LayoutNode{{Groups: two}, {Groups: two}}}, "H(V(1 2) V(3 4))", 4},
		{"two columns bottom", 1, Layout{Vertical, []LayoutNode{{}, {Groups: two}}}, "V(1 H(2 3))", 3},
		{"two rows right", 1, Layout{Horizontal, []LayoutNode{{}, {Groups: two}}}, "H(1 V(2 3))", 3},
		{"fewer places merge the extra groups", 4, Layout{Horizontal, two}, "H(1 2)", 2},
		{"a single place keeps the first group", 3, Layout{Horizontal, one}, "H(1)", 1},
		{"a split of one is that node", 1, Layout{Horizontal, []LayoutNode{{}, {Groups: one}}}, "H(1 2)", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGrid(DefaultGridOptions())
			for i := 1; i < tc.groups; i++ {
				mustAdd(t, g, i, DirRight)
			}
			if err := g.ApplyLayout(tc.layout); err != nil {
				t.Fatal(err)
			}
			if got := shape(g); got != tc.want || g.Count() != tc.count {
				t.Errorf("tree %s with %d groups, want %s with %d", got, g.Count(), tc.want, tc.count)
			}
		})
	}
}

func TestGridLayoutSizesAndRoundTrip(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	err := g.ApplyLayout(Layout{Horizontal, []LayoutNode{{Size: 0.25}, {Size: 0.75, Groups: []LayoutNode{{Size: 0.4}, {Size: 0.6}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := sizesAt(g); !slices.Equal(got, []int{25, 75}) {
		t.Errorf("root sizes %v", got)
	}
	if got := sizesAt(g, 1); !slices.Equal(got, []int{40, 60}) {
		t.Errorf("nested sizes %v", got)
	}
	l := g.Layout()
	data, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	var back Layout
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	h := NewGrid(DefaultGridOptions())
	if err := h.ApplyLayout(back); err != nil {
		t.Fatal(err)
	}
	if shape(h) != shape(g) || !layoutsEqual(h.Layout(), l) {
		t.Errorf("the layout does not round trip: %s %v vs %s %v", shape(h), h.Layout(), shape(g), l)
	}
}

func TestGridLayoutSizesOfNodesWithoutOne(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	if err := g.ApplyLayout(Layout{Horizontal, []LayoutNode{{Size: 0.5}, {}, {}}}); err != nil {
		t.Fatal(err)
	}
	// 0.5 defined, the two others share 0.5: 0.25 each, then normalised by their sum
	if got := sizesAt(g); !slices.Equal(got, []int{50, 25, 25}) {
		t.Errorf("sizes %v", got)
	}
}

func TestGridApplyLayoutMergesTheEditorsOfTheExtraGroups(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	mustAdd(t, g, 1, DirRight)
	mustAdd(t, g, 2, DirRight)
	for i, name := range []string{"a", "b", "c"} {
		g.Group(i+1).Model.OpenEditor(newCopyInput(name), OpenOptions{Pinned: true, Active: true})
	}
	if err := g.ApplyLayout(Layout{Horizontal, []LayoutNode{{}, {}}}); err != nil {
		t.Fatal(err)
	}
	if got := editorNames(g.Group(2)); !slices.Equal(got, []string{"b", "c"}) {
		t.Errorf("the last group has %v, want the editors of the group that went away after its own", got)
	}
}

func editorNames(grp *Group) []string {
	var out []string
	for _, e := range grp.Model.Editors(Sequential, false) {
		out = append(out, e.Name())
	}
	return out
}

func TestGridMoveEditor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close bool // workbench.editor.closeEmptyGroups
		do    func(t *testing.T, g *Grid)
		want  map[int][]string // the editors of the groups left
		shape string
	}{
		{"to another group", true, func(t *testing.T, g *Grid) {
			a := g.Group(1).Model.EditorAt(0)
			if ok, err := g.MoveEditor(g.Group(1), a, g.Group(2), MoveOptions{}); err != nil || !ok {
				t.Fatal(ok, err)
			}
		}, map[int][]string{1: {"b"}, 2: {"a"}}, "H(1 2)"},
		{"the last editor closes the group", true, func(t *testing.T, g *Grid) {
			a, b := g.Group(1).Model.EditorAt(0), g.Group(1).Model.EditorAt(1)
			_, _ = g.MoveEditor(g.Group(1), a, g.Group(2), MoveOptions{})
			_, _ = g.MoveEditor(g.Group(1), b, g.Group(2), MoveOptions{})
		}, map[int][]string{2: {"a", "b"}}, "H(2)"},
		{"unless the setting keeps the empty group", false, func(t *testing.T, g *Grid) {
			a, b := g.Group(1).Model.EditorAt(0), g.Group(1).Model.EditorAt(1)
			_, _ = g.MoveEditor(g.Group(1), a, g.Group(2), MoveOptions{})
			_, _ = g.MoveEditor(g.Group(1), b, g.Group(2), MoveOptions{})
		}, map[int][]string{1: nil, 2: {"a", "b"}}, "H(1 2)"},
		{"to an index of the target", true, func(t *testing.T, g *Grid) {
			g.Group(2).Model.OpenEditor(newCopyInput("x"), OpenOptions{Pinned: true})
			g.Group(2).Model.OpenEditor(newCopyInput("y"), OpenOptions{Pinned: true})
			_, _ = g.MoveEditor(g.Group(1), g.Group(1).Model.EditorAt(0), g.Group(2), MoveOptions{Index: 1, HasIndex: true})
		}, map[int][]string{1: {"b"}, 2: {"x", "a", "y"}}, "H(1 2)"},
		{"within the group to an index", true, func(t *testing.T, g *Grid) {
			_, _ = g.MoveEditor(g.Group(1), g.Group(1).Model.EditorAt(0), g.Group(1), MoveOptions{Index: 1, HasIndex: true})
		}, map[int][]string{1: {"b", "a"}, 2: nil}, "H(1 2)"},
		{"within the group without an index does nothing", true, func(t *testing.T, g *Grid) {
			_, _ = g.MoveEditor(g.Group(1), g.Group(1).Model.EditorAt(0), g.Group(1), MoveOptions{})
		}, map[int][]string{1: {"a", "b"}, 2: nil}, "H(1 2)"},
		{"an editor the target has already is not added twice", true, func(t *testing.T, g *Grid) {
			g.Group(2).Model.OpenEditor(newCopyInput("a"), OpenOptions{Pinned: true})
			_, _ = g.MoveEditor(g.Group(1), g.Group(1).Model.EditorAt(0), g.Group(2), MoveOptions{})
		}, map[int][]string{1: {"b"}, 2: {"a"}}, "H(1 2)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := DefaultGridOptions()
			opts.CloseEmptyGroups = tc.close
			g := NewGrid(opts)
			mustAdd(t, g, 1, DirRight)
			g.Group(1).Model.OpenEditor(newCopyInput("a"), OpenOptions{Pinned: true, Active: true})
			g.Group(1).Model.OpenEditor(newCopyInput("b"), OpenOptions{Pinned: true})
			tc.do(t, g)
			for id, want := range tc.want {
				if got := editorNames(g.Group(id)); !slices.Equal(got, want) {
					t.Errorf("group %d has %v, want %v", id, got, want)
				}
			}
			if got := shape(g); got != tc.shape {
				t.Errorf("tree %s, want %s", got, tc.shape)
			}
		})
	}
}

func TestGridMoveEditorKeepsTheStateAndTheInput(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	mustAdd(t, g, 1, DirRight)
	a := newCopyInput("a")
	g.Group(1).Model.OpenEditor(a, OpenOptions{Sticky: true, Active: true})
	g.Group(1).Model.OpenEditor(newCopyInput("b"), OpenOptions{Pinned: true})
	var closed []ChangeEvent
	g.OnChange(func(e GridEvent) {
		if e.Kind == GroupEditorsChanged && e.Change.Kind == EditorClose {
			closed = append(closed, e.Change)
		}
	})
	_, _ = g.MoveEditor(g.Group(1), a, g.Group(2), MoveOptions{})
	moved := g.Group(2).Model.EditorAt(0)
	if moved != EditorInput(a) {
		t.Error("the very same input moves")
	}
	if !g.Group(2).Model.IsSticky(moved) || !g.Group(2).Model.IsPinned(moved) || !g.Group(2).Model.IsActive(moved) {
		t.Error("the editor is sticky, pinned and active in the target")
	}
	if len(closed) != 1 || closed[0].Context != CloseMove || closed[0].Editor != EditorInput(a) {
		t.Errorf("close events %v: want one with the context of a move", closed)
	}
	if g.Group(1).Model.ContainsStrict(a) {
		t.Error("the source still holds the input")
	}
}

func TestGridCopyEditorAndSplit(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	a := newCopyInput("a")
	g.Group(1).Model.OpenEditor(a, OpenOptions{Pinned: true, Active: true})
	g.Group(1).Model.OpenEditor(newCopyInput("b"), OpenOptions{Pinned: true, Active: true})
	nu, err := g.SplitGroup(g.Group(1), DirRight)
	if err != nil {
		t.Fatal(err)
	}
	if got := editorNames(nu); !slices.Equal(got, []string{"b"}) {
		t.Errorf("the new group has %v, want a copy of the active editor", got)
	}
	if nu.Model.EditorAt(0) == g.Group(1).Model.ActiveEditor() {
		t.Error("the copy is another input")
	}
	if g.Active() != nu {
		t.Error("the new group is active")
	}
	if got := editorNames(g.Group(1)); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("the source group has %v: a split keeps its editors", got)
	}
	// an input that cannot be copied stays in one group
	p := g.Group(1).Model
	p.OpenEditor(&testInput{id: "single"}, OpenOptions{Pinned: true, Active: true})
	more, err := g.SplitGroup(g.Group(1), DirDown)
	if err != nil {
		t.Fatal(err)
	}
	if !more.Empty() {
		t.Errorf("the new group has %v: an editor that cannot be copied is not split", editorNames(more))
	}
	// the copy of an editor the target has already is dropped
	got, ok, err := g.CopyEditor(g.Group(1), a, nu, MoveOptions{})
	if err != nil || !ok || got == nil {
		t.Fatalf("CopyEditor: %v %v %v", got, ok, err)
	}
	if nu.Model.Count() != 2 {
		t.Errorf("the target has %d editors", nu.Model.Count())
	}
	if _, ok, _ := g.CopyEditor(g.Group(1), a, nu, MoveOptions{}); !ok || nu.Model.Count() != 2 {
		t.Error("copying an editor the target has leaves one of it")
	}
}

func TestGridSplitOfAnEmptyGroup(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	nu, err := g.SplitGroup(g.Group(1), DirDown)
	if err != nil || !nu.Empty() || g.Active() != nu || shape(g) != "V(1 2)" {
		t.Errorf("%v %v %s", nu, err, shape(g))
	}
}

func TestGridMerge(t *testing.T) {
	setup := func() *Grid {
		g := NewGrid(DefaultGridOptions())
		mustAdd(t, g, 1, DirRight)
		mustAdd(t, g, 2, DirRight)
		for i, names := range [][]string{{"a", "b"}, {"c"}, {"d", "a"}} {
			for _, n := range names {
				g.Group(i+1).Model.OpenEditor(newCopyInput(n), OpenOptions{Pinned: true, Active: true})
			}
		}
		return g
	}
	t.Run("a group into another", func(t *testing.T) {
		g := setup()
		removed, err := g.MergeGroup(g.Group(1), g.Group(2))
		if err != nil || !removed {
			t.Fatal(removed, err)
		}
		if got := editorNames(g.Group(2)); !slices.Equal(got, []string{"c", "a", "b"}) {
			t.Errorf("target has %v", got)
		}
		if shape(g) != "H(2 3)" {
			t.Errorf("tree %s", shape(g))
		}
	})
	t.Run("editors the target has are not added again", func(t *testing.T) {
		g := setup()
		_, _ = g.MergeGroup(g.Group(1), g.Group(3))
		if got := editorNames(g.Group(3)); !slices.Equal(got, []string{"d", "a", "b"}) {
			t.Errorf("target has %v", got)
		}
	})
	t.Run("all groups", func(t *testing.T) {
		g := setup()
		if err := g.MergeAllGroups(g.Group(2)); err != nil {
			t.Fatal(err)
		}
		if shape(g) != "H(2)" || g.Count() != 1 {
			t.Errorf("tree %s", shape(g))
		}
		if got := editorNames(g.Group(2)); len(got) != 4 {
			t.Errorf("target has %v, want four editors", got)
		}
		if g.Active().ID() != 2 {
			t.Errorf("active %d", g.Active().ID())
		}
	})
	t.Run("the active editor of the active group stays active", func(t *testing.T) {
		g := setup()
		_ = g.Activate(g.Group(1))
		_, _ = g.MergeGroup(g.Group(1), g.Group(2))
		if g.Group(2).Model.ActiveEditor().Name() != "b" {
			t.Errorf("active editor %s, want b", g.Group(2).Model.ActiveEditor().Name())
		}
	})
	t.Run("an inactive group's editors leave the target's active editor", func(t *testing.T) {
		g := setup()
		_ = g.Activate(g.Group(2))
		_, _ = g.MergeGroup(g.Group(1), g.Group(2))
		if g.Group(2).Model.ActiveEditor().Name() != "c" {
			t.Errorf("active editor %s, want c", g.Group(2).Model.ActiveEditor().Name())
		}
	})
	t.Run("removing a group with editors merges them into the last active group", func(t *testing.T) {
		g := setup()
		_ = g.Activate(g.Group(3))
		_ = g.Activate(g.Group(1))
		if _, err := g.RemoveGroup(g.Group(1)); err != nil {
			t.Fatal(err)
		}
		if got := editorNames(g.Group(3)); !slices.Equal(got, []string{"d", "a", "b"}) {
			t.Errorf("group 3 has %v", got)
		}
	})
}

func TestGridMoveOrCopyTarget(t *testing.T) {
	// H(1 V(2 3))
	build := func() *Grid {
		g := NewGrid(DefaultGridOptions())
		mustAdd(t, g, 1, DirRight)
		mustAdd(t, g, 2, DirDown)
		return g
	}
	for _, tc := range []struct {
		name      string
		from      int
		to        Target
		value     int
		preferred Direction
		want      int // the id, or -1 for a group that is new, 0 for none
		shape     string
	}{
		{"right finds the neighbor", 1, TargetRight, 0, DirRight, 2, "H(1 V(2 3))"},
		{"right adds a group", 2, TargetRight, 0, DirRight, -1, "H(1 V(H(2 4) 3))"},
		{"left adds a group", 1, TargetLeft, 0, DirRight, -1, "H(4 1 V(2 3))"},
		{"up adds a group", 1, TargetUp, 0, DirRight, -1, "H(V(4 1) V(2 3))"},
		{"down finds the neighbor", 2, TargetDown, 0, DirRight, 3, "H(1 V(2 3))"},
		{"first", 3, TargetFirst, 0, DirRight, 1, "H(1 V(2 3))"},
		{"last", 1, TargetLast, 0, DirRight, 3, "H(1 V(2 3))"},
		{"next", 1, TargetNext, 0, DirRight, 2, "H(1 V(2 3))"},
		{"previous", 3, TargetPrevious, 0, DirRight, 2, "H(1 V(2 3))"},
		{"next of the last adds on the preferred side", 3, TargetNext, 0, DirRight, -1, "H(1 V(2 H(3 4)))"},
		{"next of the last adds below when preferred", 3, TargetNext, 0, DirDown, -1, "H(1 V(2 3 4))"},
		{"previous of the first adds left when the preferred side is right", 1, TargetPrevious, 0, DirRight, -1, "H(4 1 V(2 3))"},
		{"previous of the first adds above when it is down", 1, TargetPrevious, 0, DirDown, -1, "H(V(4 1) V(2 3))"},
		{"center", 1, TargetCenter, 0, DirRight, 1, "H(1 V(2 3))"},
		{"position", 1, TargetPosition, 3, DirRight, 3, "H(1 V(2 3))"},
		{"a position that is not there", 1, TargetPosition, 4, DirRight, 0, "H(1 V(2 3))"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := build()
			got, err := g.MoveOrCopyTarget(g.Group(tc.from), tc.to, tc.value, tc.preferred)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.want == 0 && got != nil:
				t.Errorf("found %d, want none", got.ID())
			case tc.want == -1 && (got == nil || got.ID() != 4):
				t.Errorf("found %v, want a new group", got)
			case tc.want > 0 && (got == nil || got.ID() != tc.want):
				t.Errorf("found %v, want %d", got, tc.want)
			}
			if shape(g) != tc.shape {
				t.Errorf("tree %s, want %s", shape(g), tc.shape)
			}
		})
	}
}

func TestGridCopyGroup(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	m := g.Group(1).Model
	m.OpenEditor(newCopyInput("a"), OpenOptions{Pinned: true, Sticky: true, Active: true})
	m.OpenEditor(newCopyInput("b"), OpenOptions{Pinned: true, Active: true})
	m.OpenEditor(newCopyInput("c"), OpenOptions{Active: true})
	m.OpenEditor(&testInput{id: "single"}, OpenOptions{Pinned: true})
	m.SetActive(m.EditorAt(2))
	dup, err := g.CopyGroup(g.Group(1), g.Group(1), DirRight)
	if err != nil {
		t.Fatal(err)
	}
	if got := editorNames(dup); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("the copy has %v: an editor that cannot be copied is left out", got)
	}
	d := dup.Model
	if !d.IsSticky(d.EditorAt(0)) || d.IsSticky(d.EditorAt(1)) || !d.IsPinned(d.EditorAt(1)) || d.IsPinned(d.EditorAt(2)) || d.ActiveEditor() != d.EditorAt(2) {
		t.Error("the copy keeps sticky, pinned, preview and active")
	}
	if d.EditorAt(0) == m.EditorAt(0) {
		t.Error("the inputs are copies")
	}
	if shape(g) != "H(1 2)" || dup.ID() == 1 {
		t.Errorf("tree %s, id %d", shape(g), dup.ID())
	}
}

func TestGridClosesEmptyGroups(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	mustAdd(t, g, 1, DirRight)
	a := newCopyInput("a")
	g.Group(2).Model.OpenEditor(a, OpenOptions{Pinned: true, Active: true})
	_ = g.Activate(g.Group(2))
	var kinds []GridChangeKind
	g.OnChange(func(e GridEvent) { kinds = append(kinds, e.Kind) })
	g.Group(2).Model.CloseEditor(a, CloseUnknown, true)
	if g.Count() != 1 || g.Active().ID() != 1 || shape(g) != "H(1)" {
		t.Errorf("%d groups, active %d, tree %s", g.Count(), g.Active().ID(), shape(g))
	}
	if kinds[0] != GroupEditorsChanged || !slices.Contains(kinds, GroupRemoved) {
		t.Errorf("events %v: the close is announced before the group goes", kinds)
	}
	// the last group stays
	b := newCopyInput("b")
	g.Group(1).Model.OpenEditor(b, OpenOptions{Pinned: true, Active: true})
	g.Group(1).Model.CloseEditor(b, CloseUnknown, true)
	if g.Count() != 1 {
		t.Error("the last group closed")
	}
}

func TestGridSerializeRoundTrip(t *testing.T) {
	g := NewGrid(DefaultGridOptions())
	mustAdd(t, g, 1, DirRight)
	mustAdd(t, g, 2, DirDown)
	mustAdd(t, g, 1, DirDown)
	g.SetSizes(nil, []float64{0.4, 0.6})
	for id, names := range map[int][]string{1: {"a", "b"}, 3: {"c"}, 4: {"d", "e"}} {
		for _, n := range names {
			g.Group(id).Model.OpenEditor(newCopyInput(n), OpenOptions{Pinned: true, Active: true})
		}
	}
	g.Group(4).Model.Stick(g.Group(4).Model.EditorAt(1))
	_ = g.Activate(g.Group(3))
	_ = g.Activate(g.Group(4))
	_ = g.Maximize(g.Group(4))
	data, err := json.Marshal(g.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	var saved SerializedGrid
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	h, err := RestoreGrid(saved, DefaultGridOptions(), func(typeID, value string) (EditorInput, bool) {
		return newCopyInput(value), typeID == testInputTypeID
	})
	if err != nil {
		t.Fatal(err)
	}
	if shape(h) != shape(g) || !layoutsEqual(h.Layout(), g.Layout()) {
		t.Errorf("tree %s %v, want %s %v", shape(h), h.Layout(), shape(g), g.Layout())
	}
	if h.Active().ID() != 4 || h.Maximized() == nil || h.Maximized().ID() != 4 {
		t.Errorf("active %d, maximized %v", h.Active().ID(), h.Maximized())
	}
	if !slices.Equal(ids(h.Groups(MostRecentlyActiveGroups)), ids(g.Groups(MostRecentlyActiveGroups))) {
		t.Errorf("recent order %v, want %v", ids(h.Groups(MostRecentlyActiveGroups)), ids(g.Groups(MostRecentlyActiveGroups)))
	}
	for _, id := range []int{1, 2, 3, 4} {
		if !slices.Equal(editorNames(h.Group(id)), editorNames(g.Group(id))) {
			t.Errorf("group %d has %v, want %v", id, editorNames(h.Group(id)), editorNames(g.Group(id)))
		}
	}
	if !h.Group(4).Model.IsSticky(h.Group(4).Model.EditorAt(0)) {
		t.Error("the sticky editor is not sticky")
	}
	// a new group gets an id that was not used
	if nu := mustAdd(t, h, 1, DirRight); nu.ID() != 5 {
		t.Errorf("new group %d, want 5", nu.ID())
	}
}

func TestGridOptions(t *testing.T) {
	opts := DefaultGridOptions()
	opts.Model.OpenPositioning = PositionFirst
	g := NewGrid(opts)
	nu := mustAdd(t, g, 1, DirRight)
	if nu.Model.Options().OpenPositioning != PositionFirst {
		t.Error("new groups follow the model options")
	}
	opts.Model.OpenPositioning = PositionLast
	g.SetOptions(opts)
	if g.Group(1).Model.Options().OpenPositioning != PositionLast || nu.Model.Options().OpenPositioning != PositionLast {
		t.Error("SetOptions reaches the groups")
	}
	g.Dispose()
	g.Dispose()
}

func TestModelClone(t *testing.T) {
	// ported from VS Code: editorGroupModel.test.ts "Clone Group" (without the locking)
	h := newHarness(t)
	group := h.model()
	input1, input2, input3 := h.input(), h.input(), h.input()
	group.OpenEditor(input1, OpenOptions{Pinned: true, Active: true})
	group.OpenEditor(input2, OpenOptions{Pinned: true, Active: true})
	group.OpenEditor(input3, OpenOptions{Pinned: false, Active: true})
	group.Stick(input2)
	if !group.IsSticky(input2) {
		t.Fatal("the editor is not sticky")
	}
	clone := group.Clone(group.ID()+1, nil)
	if clone.ID() == group.ID() || clone.Count() != 3 {
		t.Errorf("clone id %d with %d editors", clone.ID(), clone.Count())
	}
	labelChanged := false
	stop := clone.OnChange(func(e ChangeEvent) {
		if e.Kind == EditorLabel {
			labelChanged = true
		}
	})
	input1.setLabel()
	if !labelChanged {
		t.Error("the clone does not follow the label of its inputs")
	}
	stop()
	for _, tc := range []struct {
		in                  *testInput
		pinned, active, stk bool
	}{
		{input1, true, false, false},
		{input2, true, false, true},
		{input3, false, true, false},
	} {
		if clone.IsPinned(tc.in) != tc.pinned || clone.IsActive(tc.in) != tc.active || clone.IsSticky(tc.in) != tc.stk {
			t.Errorf("input %s: pinned %v active %v sticky %v", tc.in.id, clone.IsPinned(tc.in), clone.IsActive(tc.in), clone.IsSticky(tc.in))
		}
	}
	// a dup that refuses an editor leaves it out, with its sticky count
	only := group.Clone(9, func(e EditorInput) EditorInput {
		if e == EditorInput(input2) {
			return nil
		}
		return e
	})
	if only.Count() != 2 || only.StickyCount() != 0 || only.Contains(input2) {
		t.Errorf("clone without input2: %d editors, %d sticky", only.Count(), only.StickyCount())
	}
}
