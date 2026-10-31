package kit

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

func TestListViewModelBlockedRows(t *testing.T) {
	blocked := func(it ListViewItem) bool { return it.Key == "b" || it.Key == "d" }
	newModel := func(t *testing.T) *ListViewModel {
		m := newListModelOf(t, 6, 6, ListViewModelOpts{MultiSelect: true, Blocked: blocked})
		return m
	}
	tests := []struct {
		name string
		do   func(m *ListViewModel)
		want string // the focused key
	}{
		{"Down skips a blocked row", func(m *ListViewModel) { m.SetFocus("a"); m.Perform(ListActionDown) }, "c"},
		{"Down skips two", func(m *ListViewModel) { m.SetFocus("c"); m.Perform(ListActionDown) }, "e"},
		{"Up skips a blocked row", func(m *ListViewModel) { m.SetFocus("c"); m.Perform(ListActionUp) }, "a"},
		{"Home", func(m *ListViewModel) { m.SetFocus("e"); m.Perform(ListActionFirst) }, "a"},
		{"End", func(m *ListViewModel) { m.SetFocus("a"); m.Perform(ListActionLast) }, "f"},
		{"a blocked row cannot be focused", func(m *ListViewModel) { m.SetFocus("a"); m.SetFocus("b") }, "a"},
		{"a click on a blocked row does nothing", func(m *ListViewModel) { m.SetFocus("a"); m.Click("d", ClickModifiers{}) }, "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(t)
			tt.do(m)
			if got := focusKey(m); got != tt.want {
				t.Errorf("focused %q, want %q", got, tt.want)
			}
			if m.IsSelected("b") || m.IsSelected("d") {
				t.Error("a blocked row is selected")
			}
		})
	}
	t.Run("select all leaves them out", func(t *testing.T) {
		m := newModel(t)
		m.Perform(ListActionSelectAll)
		if got := m.SelectedKeys(); !slices.Equal(got, []string{"a", "c", "e", "f"}) {
			t.Errorf("selected %q", got)
		}
	})
	t.Run("a range selection passes over them", func(t *testing.T) {
		m := newModel(t)
		m.Click("a", ClickModifiers{})
		m.Click("e", ClickModifiers{Shift: true})
		if got := m.SelectedKeys(); !slices.Equal(got, []string{"a", "c", "e"}) {
			t.Errorf("selected %q", got)
		}
	})
}

func TestListViewModelRefreshHeights(t *testing.T) {
	heights := []float64{20, 20, 20}
	m := NewListViewModel(ListViewModelOpts{HeightOf: func(i int) float64 { return heights[i] }})
	if err := m.SetItems(letterItems(3)); err != nil {
		t.Fatal(err)
	}
	changes := 0
	handle := m.AddListener(func() { changes++ })
	defer m.RemoveListener(handle)
	heights[1] = 50
	m.RefreshHeights()
	if m.ContentHeight() != 90 || m.RowTop(2) != 70 || m.RowHeightAt(1) != 50 {
		t.Errorf("content %v, top of c %v, height of b %v", m.ContentHeight(), m.RowTop(2), m.RowHeightAt(1))
	}
	if changes != 1 {
		t.Errorf("listeners were told %d times", changes)
	}
}

// TestListEmbeddedRows: a row that holds a widget of its own is left to it: a click on its button runs the button, selects nothing
// and moves no focus; the other rows work as before.
func TestListEmbeddedRows(t *testing.T) {
	pressed := 0
	embeddedKey := "c"
	r := newListViewRig(t, listViewRigOpts{
		model: ListViewModelOpts{Blocked: func(it ListViewItem) bool { return it.Key == embeddedKey }},
		list: ListOpts{
			Embedded: func(row ListViewRow) bool { return row.Key == embeddedKey },
			Render: func(bc widgets.BuildContext, row ListViewRow, st RowState) widgets.Widget {
				if row.Key == embeddedKey {
					return widgets.NewCenter(NewButton("Press", ButtonOpts{OnPressed: func() { pressed++ }}))
				}
				return defaultListRender(bc, row, st)
			},
		},
	})
	r.ts.Tap(gluttertest.ByText("Press"), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.settle()
	if pressed != 1 {
		t.Errorf("the button was pressed %d times", pressed)
	}
	if r.m.SelectionCount() != 0 || len(r.selected) != 0 || len(r.opened) != 0 {
		t.Errorf("the click reached the list: selected %q, OnSelect %q, OnOpen %q", r.m.SelectedKeys(), r.selected, r.opened)
	}
	if got := focusKey(r.m); got != "" {
		t.Errorf("the list focused %q for a click on the button", got)
	}
	// a hover over the embedded row does not mark it (the list draws no row there); the others still select
	r.tapRow(1)
	if !slices.Equal(r.m.SelectedKeys(), []string{"b"}) {
		t.Errorf("a click on a row selected %q", r.m.SelectedKeys())
	}
	r.focusList()
	r.key(services.LogicalKeyboardKeyArrowDown)
	if got := focusKey(r.m); got != "d" {
		t.Errorf("Down from b focused %q, want d (the embedded row is passed over)", got)
	}
	r.noErrors()
}

// TestListFocusInAnEmbeddedRow: a text box in an embedded row that takes the keyboard does not make the list focus a row.
func TestListFocusInAnEmbeddedRow(t *testing.T) {
	ctrl := NewInputBoxController("")
	t.Cleanup(ctrl.Dispose)
	r := newListViewRig(t, listViewRigOpts{
		model: ListViewModelOpts{Blocked: func(it ListViewItem) bool { return it.Key == "a" }},
		list: ListOpts{
			Embedded: func(row ListViewRow) bool { return row.Key == "a" },
			Render: func(bc widgets.BuildContext, row ListViewRow, st RowState) widgets.Widget {
				if row.Key == "a" {
					return NewInputBox(nil, nil, InputBoxOpts{Controller: ctrl})
				}
				return defaultListRender(bc, row, st)
			},
		},
	})
	ctrl.Focus()
	r.settle()
	if !ctrl.HasFocus() {
		t.Fatal("the box did not take the keyboard")
	}
	if got := focusKey(r.m); got != "" {
		t.Errorf("the list focused %q while the box has the keyboard", got)
	}
	r.noErrors()
}

// TestListKeysInAnEmbeddedTextBox: the arrows, Home and End move the caret of a text box in an embedded row; the list does not take
// them to move its own focus.
func TestListKeysInAnEmbeddedTextBox(t *testing.T) {
	ctrl := NewInputBoxController("ab\ncd")
	t.Cleanup(ctrl.Dispose)
	r := newListViewRig(t, listViewRigOpts{
		model: ListViewModelOpts{Blocked: func(it ListViewItem) bool { return it.Key == "a" }},
		list: ListOpts{
			Embedded: func(row ListViewRow) bool { return row.Key == "a" },
			Render: func(bc widgets.BuildContext, row ListViewRow, st RowState) widgets.Widget {
				if row.Key == "a" {
					return NewInputBox(nil, nil, InputBoxOpts{Controller: ctrl, MultiLine: true, MinLines: 2})
				}
				return defaultListRender(bc, row, st)
			},
		},
	})
	ctrl.Focus()
	r.settle()
	ctrl.Select(1, 1)
	r.settle()
	physical := map[services.LogicalKeyboardKey]services.PhysicalKeyboardKey{
		services.LogicalKeyboardKeyArrowDown: services.PhysicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyArrowUp: services.PhysicalKeyboardKeyArrowUp,
		services.LogicalKeyboardKeyHome: services.PhysicalKeyboardKeyHome, services.LogicalKeyboardKeyEnd: services.PhysicalKeyboardKeyEnd,
	}
	cases := []struct {
		key  services.LogicalKeyboardKey
		want int
	}{
		{services.LogicalKeyboardKeyArrowDown, 4}, {services.LogicalKeyboardKeyHome, 3}, {services.LogicalKeyboardKeyEnd, 5},
		{services.LogicalKeyboardKeyArrowUp, 2},
	}
	for _, tc := range cases {
		r.ts.SendKeyEvent(tc.key, gluttertest.KeyOpts{Physical: physical[tc.key]})
		r.settle()
		if s, e, _ := ctrl.Selection(); s != tc.want || e != tc.want {
			t.Errorf("after %v the caret is at %d..%d, want %d", tc.key.KeyLabel(), s, e, tc.want)
		}
	}
	if got := focusKey(r.m); got != "" {
		t.Errorf("a key reached the list: it focused %q", got)
	}
	r.noErrors()
}

// TestListRowsOfDifferentHeightsAtTheEnd: in a long list of rows of different heights the rows in view are at the tops the model
// gives, wherever the list is scrolled to (the layout is a binary search on the tops, not a sum over the rows before).
func TestListRowsOfDifferentHeightsAtTheEnd(t *testing.T) {
	const n = 20000
	height := func(i int) float64 { return []float64{22, 22, 36, 22, 44, 22}[i%6] }
	rows := manyItems(n)
	r := newListViewRig(t, listViewRigOpts{items: rows, size: ui.Size{Width: 300, Height: 260}, model: ListViewModelOpts{HeightOf: height}})
	want := 0.0
	for i := range n {
		want += height(i)
	}
	if got := r.m.ContentHeight(); got != want {
		t.Fatalf("content height %v, want %v", got, want)
	}
	for _, key := range []string{"k0", "k1", "k7", "k5000", "k12345", "k19999"} {
		i, _ := strconv.Atoi(strings.TrimPrefix(key, "k"))
		r.ctrl.Reveal(key)
		r.settle()
		r.settle()
		offset := r.ctrl.ScrollOffset()
		el := gluttertest.ByText(fmt.Sprintf("row %d", i))
		if !r.ts.Exists(el) {
			t.Fatalf("row %d is not built after a reveal (offset %v)", i, offset)
		}
		// the label is in the row, which starts at the top of the row less the scroll offset (the list is at the top of the window)
		top := r.ts.TopLeft(el).Dy
		rowTop := r.m.RowTop(i) - offset
		if top < rowTop || top > rowTop+height(i) {
			t.Errorf("row %d is drawn at %v, the model has its top at %v (scrolled by %v)", i, top, rowTop, offset)
		}
	}
	// a row in the middle of the view is the one the model finds at that position
	r.ctrl.ScrollTo(100000)
	r.settle()
	r.settle()
	if i := r.m.RowAt(100000 + 130); !r.ts.Exists(gluttertest.ByText(fmt.Sprintf("row %d", i))) {
		t.Errorf("row %d, in the middle of the view, is not built", i)
	}
	r.noErrors()
}

// TestListJumpToTheEndOfAHundredThousandRows: a frame that jumps to the end of 100,000 rows of different heights lays out the last
// row at its exact offset (Glutter keeps the sums of the extents and knows the scroll extent, so the jump is not cut short by an
// estimate) and takes a bounded time, which adding up the heights of the rows before each row did not.
func TestListJumpToTheEndOfAHundredThousandRows(t *testing.T) {
	if testing.Short() {
		t.Skip("a big model")
	}
	const n = 100_000
	height := func(i int) float64 { return []float64{22, 22, 36, 22, 44, 22}[i%6] }
	r := newListViewRig(t, listViewRigOpts{items: manyItems(n), size: ui.Size{Width: 300, Height: 260}, model: ListViewModelOpts{HeightOf: height}})
	total := r.m.ContentHeight()
	view := r.ctrl.ViewportHeight()
	for _, tc := range []struct {
		name   string
		offset float64
	}{
		{"the end", total},
		{"the top", 0},
		{"the end again", total},
		{"the middle", total / 2},
		{"the end from the middle", total},
	} {
		start := time.Now()
		r.ctrl.ScrollTo(tc.offset)
		r.ts.Pump(16 * time.Millisecond)
		took := time.Since(start)
		want := min(tc.offset, total-view)
		if got := r.ctrl.ScrollOffset(); got != want {
			t.Errorf("%s: scrolled to %v, want %v (the content is %v high)", tc.name, got, want, total)
		}
		if took > 2*time.Second {
			t.Errorf("%s: the frame took %v", tc.name, took)
		}
		t.Logf("%s: the frame took %v", tc.name, took)
		if tc.offset != total {
			continue
		}
		// the last row is built and ends where the content ends
		last := gluttertest.ByText(fmt.Sprintf("row %d", n-1))
		if !r.ts.Exists(last) {
			t.Fatalf("%s: the last row is not built", tc.name)
		}
		top := r.ts.TopLeft(last).Dy
		rowTop := r.m.RowTop(n-1) - r.ctrl.ScrollOffset()
		if top < rowTop || top > rowTop+height(n-1) {
			t.Errorf("%s: the last row is drawn at %v, the model has its top at %v", tc.name, top, rowTop)
		}
		if end := rowTop + height(n-1); end != view {
			t.Errorf("%s: the last row ends at %v, the view is %v high", tc.name, end, view)
		}
	}
	r.noErrors()
}

// TestListRowsFollowTheHeightsWhenTheyChange: Glutter keeps the extents it was given until the list is built again, so a change of
// the heights (rows added and removed, a row taller) must give it a new builder: the rows are placed at the new tops, and the
// scroll extent is the new height of the content.
func TestListRowsFollowTheHeightsWhenTheyChange(t *testing.T) {
	tall := map[int]float64{}
	height := func(i int) float64 {
		if h, ok := tall[i]; ok {
			return h
		}
		return 22
	}
	r := newListViewRig(t, listViewRigOpts{items: manyItems(1000), size: ui.Size{Width: 300, Height: 260}, model: ListViewModelOpts{HeightOf: height}})
	check := func(name string, rows int) {
		t.Helper()
		r.settle()
		if got := r.m.ContentHeight(); got < float64(rows)*22 {
			t.Fatalf("%s: content height %v", name, got)
		}
		// at the end the view ends where the content ends, and the last row is where the model has it; at the top so is the first
		for _, tc := range []struct {
			at, row int
		}{{rows, rows - 1}, {0, 0}} {
			offset := 0.0
			if tc.at != 0 {
				offset = r.m.ContentHeight()
			}
			r.ctrl.ScrollTo(offset)
			r.settle()
			r.settle()
			if tc.at != 0 {
				if end := r.ctrl.ScrollOffset() + r.ctrl.ViewportHeight(); end != r.m.ContentHeight() {
					t.Errorf("%s: scrolled to the end, the view ends at %v, the content at %v", name, end, r.m.ContentHeight())
				}
			}
			el := gluttertest.ByText(fmt.Sprintf("row %d", tc.row))
			if !r.ts.Exists(el) {
				t.Errorf("%s: row %d is not built", name, tc.row)
				continue
			}
			top := r.ts.TopLeft(el).Dy
			rowTop := r.m.RowTop(tc.row) - r.ctrl.ScrollOffset()
			if top < rowTop || top > rowTop+height(tc.row) {
				t.Errorf("%s: row %d is drawn at %v, the model has its top at %v", name, tc.row, top, rowTop)
			}
		}
	}
	check("at first", 1000)
	// a row grows and the model is told
	tall[999], tall[998] = 120, 80
	r.m.RefreshHeights()
	check("rows taller", 1000)
	// rows are removed and added
	if err := r.m.SetItems(manyItems(300)); err != nil {
		t.Fatal(err)
	}
	check("fewer rows", 300)
	tall[299] = 60
	if err := r.m.SetItems(manyItems(5000)); err != nil {
		t.Fatal(err)
	}
	check("more rows", 5000)
	r.noErrors()
}
