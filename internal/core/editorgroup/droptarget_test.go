package editorgroup

import (
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestZoneAt(t *testing.T) {
	const w, h = 300.0, 200.0
	vertical := ZoneOptions{Split: true, PreferVertical: true}
	horizontal := ZoneOptions{Split: true}
	for _, tc := range []struct {
		name string
		x, y float64
		o    ZoneOptions
		want DropZone
	}{
		{"the middle", 150, 100, vertical, DropCenter},
		{"inside the edge ring", 31, 21, vertical, DropCenter},
		{"on the ring at the left", 30, 100, vertical, DropLeft},
		{"on the ring at the right", 270, 100, vertical, DropRight},
		{"on the ring at the top", 150, 20, vertical, DropUp},
		{"on the ring at the bottom", 150, 180, vertical, DropDown},
		{"the corner goes to the side by thirds", 10, 10, vertical, DropLeft},
		{"the far corner", 290, 190, vertical, DropRight},
		{"the top ring, left third, still left", 99, 5, vertical, DropLeft},
		{"the top ring, middle third, up", 150, 5, vertical, DropUp},
		{"the bottom ring, middle third, down", 150, 195, vertical, DropDown},
		{"the middle third boundary belongs to the middle", 200, 5, vertical, DropUp},
		{"the middle third boundary, past it, is right", 201, 5, vertical, DropRight},
		{"horizontal: the top ring is up whatever x", 10, 10, horizontal, DropUp},
		{"horizontal: the bottom ring is down", 290, 190, horizontal, DropDown},
		{"horizontal: the left ring in the middle band is left", 10, 100, horizontal, DropLeft},
		{"horizontal: the right ring in the middle band is right", 290, 100, horizontal, DropRight},
		{"horizontal: the middle", 150, 100, horizontal, DropCenter},
		{"horizontal: a point in the ring above the middle third is up", 150, 5, horizontal, DropUp},
		{"without splitting everything is the center", 5, 5, ZoneOptions{}, DropCenter},
		{"without splitting the far corner is the center", 299, 199, ZoneOptions{PreferVertical: true}, DropCenter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ZoneAt(tc.x, tc.y, w, h, tc.o); got != tc.want {
				t.Errorf("ZoneAt(%v, %v) = %v, want %v", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

func TestZoneAtCoversTheWholeAreaWithOneZone(t *testing.T) {
	// every point has a zone, and a side zone has a direction
	for _, o := range []ZoneOptions{{Split: true}, {Split: true, PreferVertical: true}, {}} {
		for x := 0.0; x <= 300; x += 7 {
			for y := 0.0; y <= 200; y += 7 {
				z := ZoneAt(x, y, 300, 200, o)
				if z == DropNone {
					t.Fatalf("no zone at (%v, %v) with %+v", x, y, o)
				}
				_, split := z.Direction()
				if split != (z != DropCenter) {
					t.Fatalf("zone %v at (%v, %v): split = %v", z, x, y, split)
				}
			}
		}
	}
}

func TestZoneRectAndDirection(t *testing.T) {
	for _, tc := range []struct {
		zone       DropZone
		rect       [4]float64
		dir        Direction
		splits     bool
		wantString string
	}{
		{DropCenter, [4]float64{0, 0, 300, 200}, 0, false, "center"},
		{DropNone, [4]float64{0, 0, 300, 200}, 0, false, "none"},
		{DropLeft, [4]float64{0, 0, 150, 200}, DirLeft, true, "left"},
		{DropRight, [4]float64{150, 0, 150, 200}, DirRight, true, "right"},
		{DropUp, [4]float64{0, 0, 300, 100}, DirUp, true, "up"},
		{DropDown, [4]float64{0, 100, 300, 100}, DirDown, true, "down"},
	} {
		t.Run(tc.wantString, func(t *testing.T) {
			l, tp, w, h := ZoneRect(tc.zone, 300, 200)
			if got := [4]float64{l, tp, w, h}; got != tc.rect {
				t.Errorf("rect %v, want %v", got, tc.rect)
			}
			dir, split := tc.zone.Direction()
			if split != tc.splits || (split && dir != tc.dir) {
				t.Errorf("direction %v %v, want %v %v", dir, split, tc.dir, tc.splits)
			}
			if tc.zone.String() != tc.wantString {
				t.Errorf("string %q", tc.zone.String())
			}
		})
	}
}

func TestTabDropIndex(t *testing.T) {
	tabs := []Span{{0, 100}, {100, 220}, {220, 320}}
	for _, tc := range []struct {
		name string
		x    float64
		tabs []Span
		want int
	}{
		{"left half of the first tab", 10, tabs, 0},
		{"the middle of a tab counts as left", 50, tabs, 0},
		{"right half of the first tab", 51, tabs, 1},
		{"left half of the second", 150, tabs, 1},
		{"right half of the second", 200, tabs, 2},
		{"right half of the last", 300, tabs, 3},
		{"past the last tab", 500, tabs, 3},
		{"before the first tab", -20, tabs, 0},
		{"on a boundary belongs to the tab on its right", 100, tabs, 1},
		{"no tabs", 10, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TabDropIndex(tc.tabs, tc.x); got != tc.want {
				t.Errorf("TabDropIndex(%v) = %d, want %d", tc.x, got, tc.want)
			}
		})
	}
}

// dropGrid makes a grid of two groups: group 1 holds a, b, c and group 2 holds x.
func dropGrid(t *testing.T, closeEmpty bool) *Grid {
	t.Helper()
	opts := DefaultGridOptions()
	opts.CloseEmptyGroups = closeEmpty
	g := NewGrid(opts)
	mustAdd(t, g, 1, DirRight)
	for _, id := range []string{"a", "b", "c"} {
		g.Group(1).Model.OpenEditor(newCopyInput(id), OpenOptions{Pinned: true, Active: true})
	}
	g.Group(2).Model.OpenEditor(newCopyInput("x"), OpenOptions{Pinned: true, Active: true})
	return g
}

func TestDropOnTabs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		from   int
		editor string
		onto   int
		index  int
		copied bool
		want   map[int][]string
	}{
		{"a tab to the start of its strip", 1, "c", 1, 0, false, map[int][]string{1: {"c", "a", "b"}}},
		{"a tab to the end of its strip", 1, "a", 1, 3, false, map[int][]string{1: {"b", "c", "a"}}},
		{"a tab after itself stays", 1, "b", 1, 2, false, map[int][]string{1: {"a", "b", "c"}}},
		{"a tab before itself stays", 1, "b", 1, 1, false, map[int][]string{1: {"a", "b", "c"}}},
		{"a tab one place on counts the tab itself", 1, "a", 1, 2, false, map[int][]string{1: {"b", "a", "c"}}},
		{"a tab to another strip, between its tabs", 1, "b", 2, 0, false, map[int][]string{1: {"a", "c"}, 2: {"b", "x"}}},
		{"a tab to the end of another strip", 1, "a", 2, 1, false, map[int][]string{1: {"b", "c"}, 2: {"x", "a"}}},
		{"a copy keeps the tab", 1, "a", 2, 1, true, map[int][]string{1: {"a", "b", "c"}, 2: {"x", "a"}}},
		{"a copy within the strip moves it", 1, "a", 1, 3, true, map[int][]string{1: {"b", "c", "a"}}},
		{"an index past the end is the end", 2, "x", 1, 99, false, map[int][]string{1: {"a", "b", "c", "x"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := dropGrid(t, false)
			editor := g.Group(tc.from).Model.EditorAt(slices.Index(editorNames(g.Group(tc.from)), tc.editor))
			if err := g.DropOnTabs(g.Group(tc.from), editor, g.Group(tc.onto), tc.index, tc.copied); err != nil {
				t.Fatal(err)
			}
			for id, want := range tc.want {
				if got := editorNames(g.Group(id)); !slices.Equal(got, want) {
					t.Errorf("group %d has %v, want %v", id, got, want)
				}
			}
			if g.Active() != g.Group(tc.onto) {
				t.Errorf("the group %d is active, want %d", g.Active().ID(), tc.onto)
			}
		})
	}
}

func TestDropOnTabsPinsTheEditorAndKeepsStickyTabsFirst(t *testing.T) {
	g := dropGrid(t, false)
	m := g.Group(1).Model
	m.Stick(m.EditorAt(0)) // a is sticky
	c := m.EditorAt(2)
	if err := g.DropOnTabs(g.Group(1), c, g.Group(1), 0, false); err != nil {
		t.Fatal(err)
	}
	// dropped in the sticky region: the tab becomes sticky too
	if got := editorNames(g.Group(1)); !slices.Equal(got, []string{"c", "a", "b"}) || !m.IsSticky(c) {
		t.Errorf("tabs %v, c sticky %v", got, m.IsSticky(c))
	}
	b := m.EditorAt(2)
	if err := g.DropOnTabs(g.Group(1), b, g.Group(1), 0, false); err != nil {
		t.Fatal(err)
	}
	if got := editorNames(g.Group(1)); !slices.Equal(got, []string{"b", "c", "a"}) || !m.IsSticky(b) {
		t.Errorf("tabs %v, b sticky %v", got, m.IsSticky(b))
	}
}

func TestDropOnTabsRoutes(t *testing.T) {
	g := dropGrid(t, false)
	stranger := newCopyInput("zzz")
	err := g.DropOnTabs(g.Group(1), stranger, g.Group(2), 0, false)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("Grid.DropOnTabs.DropOnTabsEditorMissing") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	other := NewGrid(DefaultGridOptions())
	err = g.DropOnTabs(other.Group(1), stranger, g.Group(2), 0, false)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("Grid.DropOnTabs") {
		t.Errorf("a group of another grid: %v", err)
	}
}

func TestDropOnZone(t *testing.T) {
	for _, tc := range []struct {
		name       string
		closeEmpty bool
		from       int
		editor     string
		onto       int
		zone       DropZone
		copied     bool
		shape      string
		want       map[int][]string
		active     int // the id of the active group; 0 when nothing happened
		dropped    bool
	}{
		{"center moves into the group", true, 1, "b", 2, DropCenter, false, "H(1 2)", map[int][]string{1: {"a", "c"}, 2: {"x", "b"}}, 2, true},
		{"center with a copy", true, 1, "b", 2, DropCenter, true, "H(1 2)", map[int][]string{1: {"a", "b", "c"}, 2: {"x", "b"}}, 2, true},
		{"right of a group moves the last editor's group there", true, 2, "x", 1, DropRight, false, "H(1 2)", map[int][]string{1: {"a", "b", "c"}, 2: {"x"}}, 2, true},
		{"left of a group with many tabs adds a group", true, 1, "c", 2, DropLeft, false, "H(1 3 2)", map[int][]string{1: {"a", "b"}, 3: {"c"}, 2: {"x"}}, 3, true},
		{"down splits in the other axis", true, 1, "c", 1, DropDown, false, "H(V(1 3) 2)", map[int][]string{1: {"a", "b"}, 3: {"c"}}, 3, true},
		{"up of the same group with a copy", true, 1, "c", 1, DropUp, true, "H(V(3 1) 2)", map[int][]string{1: {"a", "b", "c"}, 3: {"c"}}, 3, true},
		{"the last editor moves its group when empty groups close", true, 2, "x", 1, DropDown, false, "V(1 2)", map[int][]string{1: {"a", "b", "c"}, 2: {"x"}}, 2, true},
		{"the last editor is moved to a new group when empty groups stay", false, 2, "x", 1, DropDown, false, "H(V(1 3) 2)", map[int][]string{1: {"a", "b", "c"}, 3: {"x"}, 2: nil}, 3, true},
		{"the last editor as a copy leaves its group", true, 2, "x", 1, DropRight, true, "H(1 3 2)", map[int][]string{2: {"x"}, 3: {"x"}}, 3, true},
		{"the only editor of a group cannot be dropped on it", true, 2, "x", 2, DropCenter, false, "H(1 2)", map[int][]string{2: {"x"}}, 0, false},
		{"nor on its side", true, 2, "x", 2, DropRight, false, "H(1 2)", map[int][]string{2: {"x"}}, 0, false},
		{"unless it is a copy", true, 2, "x", 2, DropRight, true, "H(1 2 3)", map[int][]string{2: {"x"}, 3: {"x"}}, 3, true},
		{"no zone does nothing", true, 1, "a", 2, DropNone, false, "H(1 2)", map[int][]string{1: {"a", "b", "c"}, 2: {"x"}}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := dropGrid(t, tc.closeEmpty)
			before := g.Active()
			editor := g.Group(tc.from).Model.EditorAt(slices.Index(editorNames(g.Group(tc.from)), tc.editor))
			got, err := g.DropOnZone(g.Group(tc.from), editor, g.Group(tc.onto), tc.zone, tc.copied)
			if err != nil {
				t.Fatal(err)
			}
			if (got != nil) != tc.dropped {
				t.Fatalf("dropped = %v, want %v", got != nil, tc.dropped)
			}
			if g.Active() != got && tc.dropped {
				t.Errorf("the group %d is active, want %d", g.Active().ID(), got.ID())
			}
			if !tc.dropped && g.Active() != before {
				t.Error("nothing changed the active group")
			}
			if tc.dropped && tc.active != got.ID() {
				t.Errorf("the editor is in group %d, want %d", got.ID(), tc.active)
			}
			if s := shape(g); s != tc.shape {
				t.Errorf("tree %s, want %s", s, tc.shape)
			}
			for id, want := range tc.want {
				grp := g.Group(id)
				if grp == nil {
					t.Errorf("no group %d in %s", id, shape(g))
					continue
				}
				if names := editorNames(grp); !slices.Equal(names, want) {
					t.Errorf("group %d has %v, want %v", id, names, want)
				}
			}
		})
	}
}

func TestDropOnZoneRoutes(t *testing.T) {
	g := dropGrid(t, true)
	stranger := newCopyInput("zzz")
	_, err := g.DropOnZone(g.Group(1), stranger, g.Group(2), DropCenter, false)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("Grid.DropOnZone.DropOnZoneEditorMissing") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestDropAllowed(t *testing.T) {
	g := dropGrid(t, true)
	one, many := g.Group(2), g.Group(1)
	for _, tc := range []struct {
		name         string
		from, onto   *Group
		copied, want bool
	}{
		{"into another group", many, one, false, true},
		{"into its own group with other tabs", many, many, false, true},
		{"into its own group when it is the only tab", one, one, false, false},
		{"a copy into its own group", one, one, true, true},
		{"from nothing", nil, one, false, false},
		{"onto nothing", one, nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DropAllowed(tc.from, tc.onto, tc.copied); got != tc.want {
				t.Errorf("DropAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}
