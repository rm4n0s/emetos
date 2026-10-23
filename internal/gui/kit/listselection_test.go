package kit

import (
	"slices"
	"testing"
)

// testRows is a RowSource of keys "0".."n-1" with the keys given as hidden not among them.
type testRows struct {
	keys    []string
	blocked map[int]bool
}

func numbered(n int) *testRows {
	r := &testRows{}
	for i := range n {
		r.keys = append(r.keys, string(rune('a'+i)))
	}
	return r
}

func (r *testRows) Len() int           { return len(r.keys) }
func (r *testRows) KeyAt(i int) string { return r.keys[i] }
func (r *testRows) IndexOfKey(key string) int {
	return slices.Index(r.keys, key)
}
func (r *testRows) Selectable(i int) bool { return !r.blocked[i] }

// press runs a sequence of clicks on a selection: each step is "key", "ctrl:key", "shift:key" or
// "ctrl+shift:key"; the focus follows the clicks as the widget does.
func press(m *ListSelectionModel, rows RowSource, focus string, steps ...string) string {
	for _, st := range steps {
		mods := ClickModifiers{}
		key := st
		switch {
		case len(st) > 11 && st[:11] == "ctrl+shift:":
			mods, key = ClickModifiers{Ctrl: true, Shift: true}, st[11:]
		case len(st) > 5 && st[:5] == "ctrl:":
			mods, key = ClickModifiers{Ctrl: true}, st[5:]
		case len(st) > 6 && st[:6] == "shift:":
			mods, key = ClickModifiers{Shift: true}, st[6:]
		}
		m.Press(rows, key, focus, mods)
		if i := rows.IndexOfKey(key); i >= 0 && rows.Selectable(i) {
			focus = key
		}
	}
	return focus
}

func sortedKeys(m *ListSelectionModel, rows RowSource) []string { return m.Sorted(rows) }

// The clicks of VS Code's MouseController.changeSelection.
func TestListSelectionClicks(t *testing.T) {
	cases := []struct {
		name    string
		multi   bool
		rows    int
		steps   []string
		want    []string
		primary string
		anchor  string
	}{
		{"a click selects one row", true, 6, []string{"c"}, []string{"c"}, "c", "c"},
		{"a second click replaces", true, 6, []string{"c", "e"}, []string{"e"}, "e", "e"},
		{"ctrl adds", true, 6, []string{"b", "ctrl:d", "ctrl:f"}, []string{"b", "d", "f"}, "f", "f"},
		{"ctrl toggles off", true, 6, []string{"b", "ctrl:d", "ctrl:b"}, []string{"d"}, "d", "b"},
		{"ctrl toggling the last one off leaves nothing", true, 6, []string{"b", "ctrl:b"}, nil, "", "b"},
		{"shift selects from the anchor down", true, 6, []string{"b", "shift:e"}, []string{"b", "c", "d", "e"}, "e", "b"},
		{"shift selects from the anchor up", true, 6, []string{"e", "shift:b"}, []string{"b", "c", "d", "e"}, "b", "e"},
		{"a second shift click moves the end of the range", true, 6, []string{"b", "shift:e", "shift:c"}, []string{"b", "c"}, "c", "b"},
		{"the anchor is the row of the last ctrl click", true, 8, []string{"a", "ctrl:g", "shift:c"}, []string{"a", "c", "d", "e", "f", "g"}, "c", "g"},
		{"shift from the anchor of a ctrl click", true, 8, []string{"b", "shift:d", "ctrl:g", "shift:b"}, []string{"b", "c", "d", "e", "f", "g"}, "b", "g"},
		{"ctrl+shift adds the range", true, 8, []string{"b", "shift:d", "ctrl:g", "ctrl+shift:e"}, []string{"b", "c", "d", "e", "f", "g"}, "e", "g"},
		{"ctrl+shift keeps what the range covers", true, 6, []string{"b", "ctrl:c", "ctrl:d", "ctrl+shift:c"}, []string{"b", "c", "d"}, "c", "d"},
		{"ctrl then shift ranges from the ctrl click", true, 8, []string{"b", "ctrl:d", "shift:f"}, []string{"b", "d", "e", "f"}, "f", "d"},
		{"without multi, modifiers are ignored", false, 6, []string{"b", "ctrl:d", "shift:f"}, []string{"f"}, "f", "f"},
		{"without multi a click replaces", false, 6, []string{"b", "d"}, []string{"d"}, "d", "d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := numbered(tc.rows)
			m := NewListSelectionModel(tc.multi)
			press(m, rows, "", tc.steps...)
			if got := sortedKeys(m, rows); !slices.Equal(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("selection %q, want %q", got, tc.want)
			}
			if got := m.Primary(); got != tc.primary {
				t.Errorf("primary %q, want %q", got, tc.primary)
			}
			if got, _ := m.Anchor(); got != tc.anchor {
				t.Errorf("anchor %q, want %q", got, tc.anchor)
			}
		})
	}
}

// A shift click replaces the run of selected rows that holds the anchor and keeps the other blocks
// (`getContiguousRangeContaining`, `relativeComplement`).
func TestListSelectionShiftKeepsOtherBlocks(t *testing.T) {
	rows := numbered(8)
	m := NewListSelectionModel(true)
	m.Set("a", "b", "c", "g")
	m.SetAnchor("b")
	m.Press(rows, "e", "g", ClickModifiers{Shift: true})
	if got := sortedKeys(m, rows); !slices.Equal(got, []string{"b", "c", "d", "e", "g"}) {
		t.Errorf("selection %q", got)
	}
	// with Ctrl too, the run stays
	m.Set("a", "b", "c", "g")
	m.SetAnchor("b")
	m.Press(rows, "e", "g", ClickModifiers{Shift: true, Ctrl: true})
	if got := sortedKeys(m, rows); !slices.Equal(got, []string{"a", "b", "c", "d", "e", "g"}) {
		t.Errorf("selection %q", got)
	}
}

func TestListSelectionShiftWithoutAnchorStartsAtTheFocus(t *testing.T) {
	rows := numbered(6)
	m := NewListSelectionModel(true)
	m.Press(rows, "d", "b", ClickModifiers{Shift: true})
	if got := sortedKeys(m, rows); !slices.Equal(got, []string{"b", "c", "d"}) {
		t.Errorf("selection %q", got)
	}
	if a, _ := m.Anchor(); a != "b" {
		t.Errorf("anchor %q", a)
	}
	// no focus either: the clicked row is the anchor
	m = NewListSelectionModel(true)
	m.Press(rows, "d", "", ClickModifiers{Shift: true})
	if got := sortedKeys(m, rows); !slices.Equal(got, []string{"d"}) {
		t.Errorf("selection %q", got)
	}
}

func TestListSelectionRowsThatCannotBeSelected(t *testing.T) {
	rows := numbered(6)
	rows.blocked = map[int]bool{2: true, 3: true}
	m := NewListSelectionModel(true)
	m.Press(rows, "a", "", ClickModifiers{})
	if m.Press(rows, "c", "a", ClickModifiers{}) || m.Press(rows, "c", "a", ClickModifiers{Ctrl: true}) {
		t.Error("a click on a row that cannot be selected changes nothing")
	}
	m.Press(rows, "e", "a", ClickModifiers{Shift: true})
	if got := sortedKeys(m, rows); !slices.Equal(got, []string{"a", "b", "e"}) {
		t.Errorf("a range skips the rows that cannot be selected: %q", got)
	}
	m.SelectAll(rows)
	if got := sortedKeys(m, rows); !slices.Equal(got, []string{"a", "b", "e", "f"}) {
		t.Errorf("select all skips them too: %q", got)
	}
}

func TestListSelectionKeepsKeysThatAreNotShown(t *testing.T) {
	rows := numbered(5)
	m := NewListSelectionModel(true)
	m.Set("hidden1", "b", "hidden2")
	m.Press(rows, "d", "b", ClickModifiers{Ctrl: true})
	if got := sortedKeys(m, rows); !slices.Equal(got, []string{"hidden1", "hidden2", "b", "d"}) {
		t.Errorf("ctrl+click keeps the hidden keys, which come first: %q", got)
	}
	m.Press(rows, "e", "d", ClickModifiers{Shift: true})
	if got := sortedKeys(m, rows); !slices.Equal(got, []string{"hidden1", "hidden2", "b", "d", "e"}) {
		t.Errorf("shift+click keeps them: %q", got)
	}
	m.Press(rows, "a", "e", ClickModifiers{})
	if got := m.Keys(); !slices.Equal(got, []string{"a"}) {
		t.Errorf("a plain click drops them: %q", got)
	}
}

func TestListSelectionExpandTo(t *testing.T) {
	cases := []struct {
		name            string
		selected        []string
		previous, focus string
		want            []string
		changed         bool
	}{
		{"moving onto an unselected row adds it", []string{"b"}, "b", "c", []string{"b", "c"}, true},
		{"moving onto a selected row removes the one left", []string{"b", "c", "d"}, "d", "c", []string{"b", "c"}, true},
		{"the focus that did not move changes nothing", []string{"b"}, "b", "b", []string{"b"}, false},
		{"from an unselected row only the new row is added", nil, "a", "b", []string{"b"}, true},
		{"no focus", []string{"b"}, "b", "", []string{"b"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewListSelectionModel(true)
			m.Set(tc.selected...)
			if got := m.ExpandTo(tc.previous, tc.focus); got != tc.changed {
				t.Errorf("changed %v", got)
			}
			if got := m.Keys(); !slices.Equal(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("selection %q, want %q", got, tc.want)
			}
		})
	}
	single := NewListSelectionModel(false)
	single.Set("a")
	if single.ExpandTo("a", "b") {
		t.Error("a single selection does not expand")
	}
}

func TestListSelectionSingleHoldsOneKey(t *testing.T) {
	m := NewListSelectionModel(false)
	if !m.Set("a", "b", "c") || !slices.Equal(m.Keys(), []string{"c"}) {
		t.Errorf("Set keeps the last: %q", m.Keys())
	}
	m.Add("d")
	if !slices.Equal(m.Keys(), []string{"d"}) {
		t.Errorf("Add replaces: %q", m.Keys())
	}
	multi := NewListSelectionModel(true)
	multi.Set("a", "b", "c")
	multi.SetMulti(false)
	if !slices.Equal(multi.Keys(), []string{"c"}) {
		t.Errorf("turning multi off keeps the primary key: %q", multi.Keys())
	}
	if m.SelectAll(numbered(3)) {
		t.Error("select all needs multi")
	}
}

func TestListSelectionPrune(t *testing.T) {
	m := NewListSelectionModel(true)
	m.Set("a", "b", "c")
	m.SetAnchor("b")
	if !m.Prune(func(k string) bool { return k != "b" }) {
		t.Error("changed")
	}
	if !slices.Equal(m.Keys(), []string{"a", "c"}) {
		t.Errorf("selection %q", m.Keys())
	}
	if _, ok := m.Anchor(); ok {
		t.Error("a lost anchor is cleared")
	}
	if m.Primary() != "c" {
		t.Errorf("primary %q", m.Primary())
	}
	if m.Prune(func(string) bool { return true }) {
		t.Error("nothing left out")
	}
}

func TestListSelectionReportsChanges(t *testing.T) {
	m := NewListSelectionModel(true)
	rows := numbered(4)
	cases := []struct {
		name string
		do   func() bool
		want bool
	}{
		{"select", func() bool { return m.Select("a") }, true},
		{"select the same", func() bool { return m.Select("a") }, false},
		{"toggle on", func() bool { return m.Toggle("b") }, true},
		{"toggle off", func() bool { return m.Toggle("b") }, true},
		{"select all", func() bool { return m.SelectAll(rows) }, true},
		{"select all again", func() bool { return m.SelectAll(rows) }, false},
		{"clear", func() bool { return m.Clear() }, true},
		{"clear again", func() bool { return m.Clear() }, false},
		{"remove what is not selected", func() bool { return m.Remove("z") }, false},
	}
	for _, tc := range cases {
		if got := tc.do(); got != tc.want {
			t.Errorf("%s: changed %v, want %v", tc.name, got, tc.want)
		}
	}
}
