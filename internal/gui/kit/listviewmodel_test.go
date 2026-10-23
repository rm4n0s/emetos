package kit

import (
	"fmt"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
)

// letterItems are items with the keys "a", "b", ... and the labels "item a", ...
func letterItems(n int) []ListViewItem {
	var out []ListViewItem
	for i := range n {
		k := string(rune('a' + i))
		out = append(out, ListViewItem{Key: k, Label: "item " + k, Data: k})
	}
	return out
}

// newListModelOf makes a model of n items in a view that shows viewRows rows.
func newListModelOf(t *testing.T, n, viewRows int, opts ...ListViewModelOpts) *ListViewModel {
	t.Helper()
	m := NewListViewModel(opts...)
	if err := m.SetItems(letterItems(n)); err != nil {
		t.Fatal(err)
	}
	m.SetViewport(0, float64(viewRows)*m.RowHeight())
	return m
}

func rowKeys(m *ListViewModel) []string {
	var out []string
	for _, r := range m.Rows() {
		out = append(out, r.Key)
	}
	return out
}

func focusKey(m *ListViewModel) string { k, _ := m.Focused(); return k }

func TestListViewModelItemErrors(t *testing.T) {
	cases := []struct {
		name  string
		items []ListViewItem
		route string
	}{
		{"an item without a key", []ListViewItem{{Key: "a"}, {}}, "ListViewModel.SetItems.ListItemKeyEmpty"},
		{"two items with the same key", []ListViewItem{{Key: "a"}, {Key: "a"}}, "ListViewModel.SetItems.ListItemKeyDuplicated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewListViewModel()
			if err := m.SetItems(letterItems(2)); err != nil {
				t.Fatal(err)
			}
			err := m.SetItems(tc.items)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if got := rowKeys(m); !slices.Equal(got, []string{"a", "b"}) {
				t.Errorf("nothing changed: %q", got)
			}
		})
	}
}

func TestListViewModelFocusKeys(t *testing.T) {
	cases := []struct {
		name   string
		start  string
		action ListAction
		want   string
	}{
		{"down from nothing", "", ListActionDown, "a"},
		{"up from nothing", "", ListActionUp, "a"},
		{"down", "b", ListActionDown, "c"},
		{"up", "b", ListActionUp, "a"},
		{"down stops at the end", "j", ListActionDown, "j"},
		{"up stops at the start", "a", ListActionUp, "a"},
		{"home", "e", ListActionFirst, "a"},
		{"end", "e", ListActionLast, "j"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newListModelOf(t, 10, 4)
			if tc.start != "" {
				m.SetFocus(tc.start)
			}
			res := m.Perform(tc.action)
			if !res.Handled {
				t.Error("handled")
			}
			if got := focusKey(m); got != tc.want {
				t.Errorf("focus %q, want %q", got, tc.want)
			}
		})
	}
	m := NewListViewModel()
	if res := m.Perform(ListActionDown); res.Handled {
		t.Error("an empty list uses no key")
	}
}

func TestListViewModelScrollsToTheFocus(t *testing.T) {
	m := newListModelOf(t, 20, 5) // 5 rows of 22 px in view
	m.SetFocus("e")
	res := m.Perform(ListActionDown) // f is row 5: below the view
	if to, ok := res.ScrollTo.Get(); !ok || to != 22 {
		t.Errorf("scroll to %v %v, want 22", to, ok)
	}
	res = m.Perform(ListActionUp) // e is in view
	if _, ok := res.ScrollTo.Get(); ok {
		t.Error("no scrolling for a row in view")
	}
	m.SetViewport(0, 110)
	res = m.Perform(ListActionLast)
	if to, ok := res.ScrollTo.Get(); !ok || to != 20*22-110 {
		t.Errorf("end scrolls to the last row: %v", to)
	}
}

func TestListViewModelPaging(t *testing.T) {
	m := newListModelOf(t, 30, 5)
	m.SetFocus("a")
	m.Perform(ListActionPageDown)
	if got := focusKey(m); got != "e" {
		t.Errorf("page down focuses the last row in view: %q", got)
	}
	res := m.Perform(ListActionPageDown) // already there: scroll a page and focus the last row of it
	if to, ok := res.ScrollTo.Get(); !ok || to != 110 {
		t.Errorf("scroll %v %v", to, ok)
	}
	if got := focusKey(m); got != "j" {
		t.Errorf("focus %q", got)
	}
	m.Perform(ListActionPageUp)
	if got := focusKey(m); got != "f" {
		t.Errorf("page up focuses the first row in view: %q", got)
	}
}

func TestListViewModelRowsOfDifferentHeights(t *testing.T) {
	heights := []float64{20, 40, 20, 60, 20, 20}
	m := NewListViewModel(ListViewModelOpts{HeightOf: func(i int) float64 { return heights[i] }})
	if err := m.SetItems(letterItems(6)); err != nil {
		t.Fatal(err)
	}
	if got := m.ContentHeight(); got != 180 {
		t.Errorf("content height %v", got)
	}
	cases := []struct {
		y    float64
		want int
	}{{-1, -1}, {0, 0}, {19.9, 0}, {20, 1}, {59, 1}, {60, 2}, {80, 3}, {139, 3}, {140, 4}, {179, 5}, {180, 6}, {500, 6}}
	for _, tc := range cases {
		if got := m.RowAt(tc.y); got != tc.want {
			t.Errorf("RowAt(%v) = %d, want %d", tc.y, got, tc.want)
		}
	}
	if m.RowTop(3) != 80 || m.RowHeightAt(3) != 60 {
		t.Errorf("row 3 at %v height %v", m.RowTop(3), m.RowHeightAt(3))
	}
	// reveal: the view is 50 high
	m.SetViewport(0, 50)
	i, to, ok := m.Reveal("d")
	if !ok || i != 3 {
		t.Fatalf("reveal: %d %v", i, ok)
	}
	if y, has := to.Get(); !has || y != 90 { // the bottom of d is 140; 140-50
		t.Errorf("scroll to %v %v", y, has)
	}
	// page down in a view that is 50 high: the last row completely in view
	m.SetViewport(0, 50)
	m.SetFocus("a")
	res := m.Perform(ListActionPageDown) // "a" is the last row completely in view (b ends at 60): a page is scrolled
	if to, ok := res.ScrollTo.Get(); !ok || to != 50 {
		t.Errorf("scroll %v %v", to, ok)
	}
	if got := focusKey(m); got != "c" {
		t.Errorf("page down: %q", got)
	}
	m.SetViewport(0, 50)
	if n := m.RowsInView(); n != 1 {
		t.Errorf("rows in view %d", n)
	}
}

func TestListViewModelClicks(t *testing.T) {
	cases := []struct {
		name  string
		multi bool
		steps []string
		want  []string
	}{
		{"click", true, []string{"c"}, []string{"c"}},
		{"ctrl click adds", true, []string{"c", "ctrl:e"}, []string{"c", "e"}},
		{"shift click", true, []string{"c", "shift:f"}, []string{"c", "d", "e", "f"}},
		{"ctrl+shift click", true, []string{"a", "ctrl:h", "ctrl+shift:f"}, []string{"a", "f", "g", "h"}},
		{"modifiers need MultiSelect", false, []string{"c", "ctrl:e", "shift:g"}, []string{"g"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newListModelOf(t, 10, 5, ListViewModelOpts{MultiSelect: tc.multi})
			for _, st := range tc.steps {
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
				res := m.Click(key, mods)
				if !res.Handled || res.Key != key {
					t.Errorf("%s: %+v", st, res)
				}
				if got := focusKey(m); got != key {
					t.Errorf("%s: focus %q", st, got)
				}
			}
			if got := m.SelectedKeys(); !slices.Equal(got, tc.want) {
				t.Errorf("selected %q, want %q", got, tc.want)
			}
			wantIdx := make([]int, len(tc.want))
			for i, k := range tc.want {
				wantIdx[i] = m.IndexOf(k)
			}
			if got := m.SelectedIndexes(); !slices.Equal(got, wantIdx) {
				t.Errorf("indexes %v, want %v", got, wantIdx)
			}
		})
	}
	m := newListModelOf(t, 3, 3)
	if res := m.Click("zz", ClickModifiers{}); res.Handled {
		t.Error("a click on no row")
	}
}

func TestListViewModelSelectionChangedFlag(t *testing.T) {
	m := newListModelOf(t, 6, 6, ListViewModelOpts{MultiSelect: true})
	cases := []struct {
		name string
		do   func() ListResult
		want bool
	}{
		{"a click on an unselected row", func() ListResult { return m.Click("b", ClickModifiers{}) }, true},
		{"a click on the selected row", func() ListResult { return m.Click("b", ClickModifiers{}) }, false},
		{"ctrl click", func() ListResult { return m.Click("d", ClickModifiers{Ctrl: true}) }, true},
		{"select all", func() ListResult { return m.Perform(ListActionSelectAll) }, true},
		{"select all again", func() ListResult { return m.Perform(ListActionSelectAll) }, false},
	}
	for _, tc := range cases {
		if got := tc.do().SelectionChanged; got != tc.want {
			t.Errorf("%s: SelectionChanged %v", tc.name, got)
		}
	}
}

// The keys of a multiple selection: Shift+Up/Down (list.expandSelectionDown, expandSelectionUp), Ctrl+A,
// Ctrl+Space and Escape.
func TestListViewModelMultiSelectKeys(t *testing.T) {
	type step struct {
		do    func(m *ListViewModel)
		want  []string
		focus string
	}
	act := func(a ListAction) func(m *ListViewModel) { return func(m *ListViewModel) { m.Perform(a) } }
	click := func(k string) func(m *ListViewModel) {
		return func(m *ListViewModel) { m.Click(k, ClickModifiers{}) }
	}
	cases := []struct {
		name  string
		steps []step
	}{
		{"shift+down extends and shift+up shrinks", []step{
			{click("c"), []string{"c"}, "c"},
			{act(ListActionExpandSelectionDown), []string{"c", "d"}, "d"},
			{act(ListActionExpandSelectionDown), []string{"c", "d", "e"}, "e"},
			{act(ListActionExpandSelectionUp), []string{"c", "d"}, "d"},
			{act(ListActionExpandSelectionUp), []string{"c"}, "c"},
			{act(ListActionExpandSelectionUp), []string{"b", "c"}, "b"},
		}},
		{"shift+down at the end changes nothing", []step{
			{click("j"), []string{"j"}, "j"},
			{act(ListActionExpandSelectionDown), []string{"j"}, "j"},
		}},
		{"shift+down from a row that is not selected selects the next row only", []step{
			{click("b"), []string{"b"}, "b"},
			{act(ListActionDown), []string{"b"}, "c"},
			{act(ListActionExpandSelectionDown), []string{"b", "d"}, "d"},
		}},
		{"select all", []step{
			{click("c"), []string{"c"}, "c"},
			{act(ListActionSelectAll), []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, "c"},
		}},
		{"ctrl+space toggles the focused row", []step{
			{click("c"), []string{"c"}, "c"},
			{act(ListActionDown), []string{"c"}, "d"},
			{act(ListActionToggleSelection), []string{"c", "d"}, "d"},
			{act(ListActionToggleSelection), []string{"c"}, "d"},
		}},
		{"escape reduces to the focused row, then clears, then clears the focus", []step{
			{click("c"), []string{"c"}, "c"},
			{act(ListActionExpandSelectionDown), []string{"c", "d"}, "d"},
			{act(ListActionExpandSelectionDown), []string{"c", "d", "e"}, "e"},
			{act(ListActionClear), []string{"e"}, "e"},
			{act(ListActionClear), nil, "e"},
			{act(ListActionClear), nil, ""},
		}},
		{"the arrow keys make the anchor", []step{
			{click("b"), []string{"b"}, "b"},
			{act(ListActionDown), []string{"b"}, "c"},
			{func(m *ListViewModel) { m.Click("f", ClickModifiers{Shift: true}) }, []string{"c", "d", "e", "f"}, "f"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newListModelOf(t, 10, 10, ListViewModelOpts{MultiSelect: true})
			for i, st := range tc.steps {
				st.do(m)
				if got := m.SelectedKeys(); !slices.Equal(got, st.want) && (len(got) != 0 || len(st.want) != 0) {
					t.Errorf("step %d: selected %q, want %q", i, got, st.want)
				}
				if got := focusKey(m); got != st.focus {
					t.Errorf("step %d: focus %q, want %q", i, got, st.focus)
				}
			}
		})
	}
}

func TestListViewModelMultiKeysNeedMultiSelect(t *testing.T) {
	m := newListModelOf(t, 5, 5)
	m.Click("b", ClickModifiers{})
	for _, a := range []ListAction{ListActionExpandSelectionDown, ListActionExpandSelectionUp, ListActionSelectAll, ListActionToggleSelection} {
		if m.Perform(a).Handled {
			t.Errorf("action %d is not used without MultiSelect", a)
		}
	}
	if got := m.SelectedKeys(); !slices.Equal(got, []string{"b"}) {
		t.Errorf("selected %q", got)
	}
}

func TestListViewModelEscapeWithoutSelectionNavigation(t *testing.T) {
	m := newListModelOf(t, 6, 6, ListViewModelOpts{MultiSelect: true, SelectionNavigation: foundation.Some(false)})
	m.Click("b", ClickModifiers{})
	m.Click("d", ClickModifiers{Ctrl: true})
	m.Perform(ListActionClear)
	if m.SelectionCount() != 0 {
		t.Errorf("Escape clears several rows: %q", m.SelectedKeys())
	}
}

func TestListViewModelEnterAndSpace(t *testing.T) {
	m := newListModelOf(t, 5, 5)
	if m.Perform(ListActionSelect).Handled {
		t.Error("Enter without a focused row")
	}
	m.SetFocus("c")
	res := m.Perform(ListActionSelect)
	if !res.Handled || !res.Select || !res.Open || res.Key != "c" || res.Index != 2 || !res.SelectionChanged {
		t.Errorf("enter: %+v", res)
	}
	m.SetFocus("d")
	res = m.Perform(ListActionToggle)
	if !res.Handled || !res.Select || res.Open || res.Key != "d" {
		t.Errorf("space: %+v", res)
	}
	if got := m.SelectedKeys(); !slices.Equal(got, []string{"d"}) {
		t.Errorf("selected %q", got)
	}
	if res := m.DoubleClick("a"); !res.Open || res.Index != 0 || focusKey(m) != "a" {
		t.Errorf("double click: %+v", res)
	}
}

func TestListViewModelSetItemsKeepsFocusAndSelectionByKey(t *testing.T) {
	m := newListModelOf(t, 6, 6, ListViewModelOpts{MultiSelect: true})
	m.Click("b", ClickModifiers{})
	m.Click("d", ClickModifiers{Ctrl: true})
	m.SetFocus("d")
	changes := 0
	m.AddListener(func() { changes++ })
	items := []ListViewItem{{Key: "d"}, {Key: "x"}, {Key: "b"}, {Key: "c"}}
	if err := m.SetItems(items); err != nil {
		t.Fatal(err)
	}
	if changes == 0 {
		t.Error("the listeners are told")
	}
	if got := rowKeys(m); !slices.Equal(got, []string{"d", "x", "b", "c"}) {
		t.Errorf("rows %q", got)
	}
	if got := m.SelectedKeys(); !slices.Equal(got, []string{"d", "b"}) {
		t.Errorf("selection by key, in the order of the rows: %q", got)
	}
	if got := focusKey(m); got != "d" || m.FocusedIndex() != 0 {
		t.Errorf("focus %q at %d", got, m.FocusedIndex())
	}
	if err := m.SetItems([]ListViewItem{{Key: "x"}}); err != nil {
		t.Fatal(err)
	}
	if m.SelectionCount() != 0 || focusKey(m) != "" {
		t.Errorf("the rows that are gone leave the selection and the focus: %q %q", m.SelectedKeys(), focusKey(m))
	}
}

func TestListViewModelFind(t *testing.T) {
	items := []ListViewItem{
		{Key: "1", Label: "main.go"}, {Key: "2", Label: "README.md"}, {Key: "3", Label: "go.mod"},
		{Key: "4", Label: "list.go"}, {Key: "5", Label: "notes.txt"},
	}
	setup := func(t *testing.T, opts ListViewModelOpts) *ListViewModel {
		m := NewListViewModel(opts)
		if err := m.SetItems(items); err != nil {
			t.Fatal(err)
		}
		m.SetViewport(0, 110)
		return m
	}
	t.Run("highlight keeps all rows and moves the focus between the matches", func(t *testing.T) {
		m := setup(t, ListViewModelOpts{})
		m.SetFocus("5")
		m.SetFindPattern("go")
		if got := rowKeys(m); len(got) != 5 {
			t.Errorf("rows %q", got)
		}
		st := m.Find()
		if !st.Open || st.Pattern != "go" || st.MatchCount != 3 || st.Total != 5 {
			t.Errorf("find %+v", st)
		}
		if focusKey(m) != "1" { // from "5" wrapping to the first match
			t.Errorf("focus %q", focusKey(m))
		}
		if r, ok := m.FindMatch("1"); !ok || !slices.Equal(r, []TextRange{{5, 7}}) {
			t.Errorf("match of main.go %v %v", r, ok)
		}
		if _, ok := m.FindMatch("2"); ok {
			t.Error("README.md has no go")
		}
		m.Perform(ListActionDown)
		if focusKey(m) != "3" {
			t.Errorf("down skips rows that do not match: %q", focusKey(m))
		}
		m.Perform(ListActionDown)
		m.Perform(ListActionDown)
		if focusKey(m) != "4" {
			t.Errorf("down stops at the last match: %q", focusKey(m))
		}
		m.Perform(ListActionUp)
		if focusKey(m) != "3" {
			t.Errorf("up: %q", focusKey(m))
		}
		m.Perform(ListActionFirst)
		m.Perform(ListActionLast)
		if focusKey(m) != "4" {
			t.Errorf("end goes to the last match: %q", focusKey(m))
		}
		// a click can focus a row that does not match
		m.Click("5", ClickModifiers{})
		if focusKey(m) != "5" {
			t.Errorf("click focuses %q", focusKey(m))
		}
	})
	t.Run("with one match the focus may go anywhere", func(t *testing.T) {
		m := setup(t, ListViewModelOpts{})
		m.SetFindPattern("notes")
		if st := m.Find(); st.MatchCount != 1 {
			t.Fatalf("matches %d", st.MatchCount)
		}
		if focusKey(m) != "5" {
			t.Errorf("focus %q", focusKey(m))
		}
		m.Perform(ListActionUp)
		if focusKey(m) != "4" {
			t.Errorf("up: %q", focusKey(m))
		}
	})
	t.Run("filter shows only the matches", func(t *testing.T) {
		m := setup(t, ListViewModelOpts{FindMode: FindModeFilter})
		m.Click("3", ClickModifiers{})
		m.SetFindPattern("go")
		if got := rowKeys(m); !slices.Equal(got, []string{"1", "3", "4"}) {
			t.Errorf("rows %q", got)
		}
		if m.IndexOf("2") != -1 || m.IndexOf("4") != 2 {
			t.Errorf("indexes: %d %d", m.IndexOf("2"), m.IndexOf("4"))
		}
		if got := m.SelectedKeys(); !slices.Equal(got, []string{"3"}) {
			t.Errorf("the selection stays %q", got)
		}
		m.SetFindPattern("read")
		if got := rowKeys(m); !slices.Equal(got, []string{"2"}) {
			t.Errorf("rows %q", got)
		}
		if m.IsSelected("3") != true {
			t.Error("a selected row that is hidden stays selected")
		}
		if got := m.SelectedKeys(); !slices.Equal(got, []string{"3"}) {
			t.Errorf("selected %q", got)
		}
		if focusKey(m) != "2" {
			t.Errorf("the focus goes to the first match: %q", focusKey(m))
		}
	})
	t.Run("the match type", func(t *testing.T) {
		m := setup(t, ListViewModelOpts{FindMode: FindModeFilter})
		m.SetFindPattern("rdm")
		if got := rowKeys(m); !slices.Equal(got, []string{"2"}) {
			t.Errorf("fuzzy: %q", got)
		}
		m.SetFindMatchType(FindMatchContiguous)
		if got := rowKeys(m); len(got) != 0 {
			t.Errorf("contiguous: %q", got)
		}
		if st := m.Find(); !st.NoResults() {
			t.Errorf("no results: %+v", st)
		}
		m.SetFindMatchType(FindMatchFuzzy)
		m.SetFindMode(FindModeHighlight)
		if got := rowKeys(m); len(got) != 5 {
			t.Errorf("highlight: %q", got)
		}
	})
	t.Run("closing the find shows everything again", func(t *testing.T) {
		m := setup(t, ListViewModelOpts{FindMode: FindModeFilter})
		m.SetFindPattern("go")
		m.SetFocus("3")
		if !m.CloseFind() {
			t.Error("a find was open")
		}
		if got := rowKeys(m); len(got) != 5 {
			t.Errorf("rows %q", got)
		}
		if st := m.Find(); st.Open || st.Pattern != "" {
			t.Errorf("find %+v", st)
		}
		if focusKey(m) != "3" {
			t.Errorf("the focus stays: %q", focusKey(m))
		}
		if m.CloseFind() {
			t.Error("nothing to close")
		}
		if _, ok := m.FindMatch("1"); ok {
			t.Error("no matches without a pattern")
		}
	})
	t.Run("the pattern follows new items", func(t *testing.T) {
		m := setup(t, ListViewModelOpts{FindMode: FindModeFilter})
		m.SetFindPattern("go")
		if err := m.SetItems(append(slices.Clone(items), ListViewItem{Key: "6", Label: "gopher"})); err != nil {
			t.Fatal(err)
		}
		if got := rowKeys(m); !slices.Equal(got, []string{"1", "3", "4", "6"}) {
			t.Errorf("rows %q", got)
		}
	})
	t.Run("an item takes its label from its data", func(t *testing.T) {
		m := NewListViewModel(ListViewModelOpts{FindMode: FindModeFilter})
		if err := m.SetItems([]ListViewItem{{Key: "x", Data: "alpha"}, {Key: "y", Data: 3}}); err != nil {
			t.Fatal(err)
		}
		m.SetFindPattern("alp")
		if got := rowKeys(m); !slices.Equal(got, []string{"x"}) {
			t.Errorf("rows %q", got)
		}
	})
}

func TestListViewModelFindStatesFormat(t *testing.T) {
	// the strings of the drop positions, for the failure messages of the callers
	for p, want := range map[DropPosition]string{DropBefore: "before", DropInside: "inside", DropAfter: "after", DropPosition(9): "DropPosition(9)"} {
		if got := fmt.Sprint(p); got != want {
			t.Errorf("%d: %q", int(p), got)
		}
	}
}
