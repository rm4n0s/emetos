package kit

import (
	"slices"
	"testing"
)

func TestDropPositionAt(t *testing.T) {
	cases := []struct {
		offset float64
		want   DropPosition
	}{
		{0, DropBefore}, {5, DropBefore}, {5.49, DropBefore}, {5.5, DropInside}, {10, DropInside},
		{16.49, DropInside}, {16.5, DropAfter}, {21.9, DropAfter},
	}
	for _, tc := range cases {
		if got := DropPositionAt(tc.offset, 22); got != tc.want {
			t.Errorf("DropPositionAt(%v, 22) = %v, want %v", tc.offset, got, tc.want)
		}
	}
	if got := DropPositionAt(3, 0); got != DropInside {
		t.Errorf("an empty row: %v", got)
	}
	// a taller row has the same quarters
	if got := DropPositionAt(24, 100); got != DropBefore {
		t.Errorf("24 of 100: %v", got)
	}
	if got := DropPositionAt(25, 100); got != DropInside {
		t.Errorf("25 of 100: %v", got)
	}
	if got := DropPositionAt(75, 100); got != DropAfter {
		t.Errorf("75 of 100: %v", got)
	}
}

func TestEdgeStep(t *testing.T) {
	cases := []struct {
		name      string
		y, height float64
		want      float64
	}{
		{"the middle", 150, 300, 0},
		{"just inside the top strip", 34, 300, -1},
		{"the edge of the strip", 35, 300, 0},
		{"half way up the strip", 17, 300, -6},
		{"at the top", 0, 300, -11},
		{"above the view", -100, 300, -14},
		{"just inside the bottom strip", 266, 300, 0},
		{"near the bottom", 290, 300, 7},
		{"below the view", 600, 300, 14},
	}
	for _, tc := range cases {
		if got := edgeStep(tc.y, tc.height); got != tc.want {
			t.Errorf("%s: edgeStep(%v, %v) = %v, want %v", tc.name, tc.y, tc.height, got, tc.want)
		}
	}
}

// navOf makes the navigation state of rows named a, b, ... of a height each.
func navOf(n int, height float64, blocked ...int) *rowNav {
	nav := newRowNav(height)
	var keys []string
	b := make([]bool, n)
	for i := range n {
		keys = append(keys, string(rune('a'+i)))
	}
	for _, i := range blocked {
		b[i] = true
	}
	nav.setRows(keys, b, nil)
	return &nav
}

func TestDragSessionTargets(t *testing.T) {
	type call struct {
		target string
		pos    DropPosition
		keys   []string
	}
	var calls []call
	dnd := &DragAndDrop{CanDrop: func(target string, pos DropPosition, keys []string) DropEffect {
		calls = append(calls, call{target, pos, slices.Clone(keys)})
		switch {
		case target == "d":
			return DropEffectNone // a row that refuses
		case pos == DropInside && target == "c":
			return DropEffectCopy
		}
		return DropEffectMove
	}}
	nav := navOf(5, 22, 4)
	s := &dragSession{dnd: dnd, keys: []string{"a"}}
	cases := []struct {
		name string
		y    float64
		want DropTarget
	}{
		{"the top quarter of the first row", 2, DropTarget{"a", DropBefore, DropEffectMove}},
		{"the middle of a row", 22 + 11, DropTarget{"b", DropInside, DropEffectMove}},
		{"the bottom quarter", 22 + 20, DropTarget{"b", DropAfter, DropEffectMove}},
		{"a row can say copy", 44 + 11, DropTarget{"c", DropInside, DropEffectCopy}},
		{"a row that refuses", 66 + 11, DropTarget{"d", DropInside, DropEffectNone}},
		{"a placeholder is no target", 88 + 11, DropTarget{"", DropInside, DropEffectNone}},
		{"below the rows is the root", 500, DropTarget{"", DropInside, DropEffectMove}},
		{"above the rows is the root too", -3, DropTarget{"", DropInside, DropEffectMove}},
	}
	for _, tc := range cases {
		calls = nil
		got := s.targetAt(nav, tc.y)
		if got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
		if got.OK() != (tc.want.Effect != DropEffectNone) {
			t.Errorf("%s: OK() %v", tc.name, got.OK())
		}
		if tc.name != "a placeholder is no target" && (len(calls) != 1 || !slices.Equal(calls[0].keys, []string{"a"}) || calls[0].target != tc.want.Key || calls[0].pos != tc.want.Position) {
			t.Errorf("%s: CanDrop was called with %+v", tc.name, calls)
		}
		if tc.name == "a placeholder is no target" && len(calls) != 0 {
			t.Errorf("CanDrop is not asked about a placeholder: %+v", calls)
		}
	}
	none := &dragSession{dnd: &DragAndDrop{}}
	if got := none.targetAt(nav, 11); got.OK() {
		t.Errorf("without CanDrop nothing can be dropped: %+v", got)
	}
}

func TestDragSessionRowsOfDifferentHeights(t *testing.T) {
	rn := newRowNav(22)
	nav := &rn
	nav.setRows([]string{"a", "b", "c"}, nil, func(i int) float64 { return []float64{20, 80, 20}[i] })
	var got []DropTarget
	s := &dragSession{dnd: &DragAndDrop{CanDrop: func(string, DropPosition, []string) DropEffect { return DropEffectMove }}}
	for _, y := range []float64{25, 40, 90, 99} { // row b spans 20..100: quarters at 40 and 80
		got = append(got, s.targetAt(nav, y))
	}
	want := []DropPosition{DropBefore, DropInside, DropAfter, DropAfter}
	for i, g := range got {
		if g.Key != "b" || g.Position != want[i] {
			t.Errorf("case %d: %+v", i, g)
		}
	}
}

func TestDragKeys(t *testing.T) {
	nav := navOf(6, 22)
	nav.has = func(k string) bool { return k != "zz" }
	nav.SetMultiSelect(true)
	nav.SetSelection([]string{"d", "b", "hidden", "zz"})
	cases := []struct {
		name    string
		pressed string
		want    []string
	}{
		{"a row that is not selected is dragged alone", "a", []string{"a"}},
		{"a selected row drags the selection, in the order of the rows", "d", []string{"b", "d"}},
		{"another selected row", "b", []string{"b", "d"}},
	}
	for _, tc := range cases {
		if got := dragKeys(nav, tc.pressed); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
