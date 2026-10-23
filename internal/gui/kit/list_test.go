package kit

import (
	"fmt"
	"image/color"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// dropCall is what OnDrop was told.
type dropCall struct {
	target string
	pos    DropPosition
	keys   []string
}

// listViewRig is a list over a model, with a second focusable below it to take the focus away, that records what
// it renders and reports.
type listViewRig struct {
	t          *testing.T
	ts         *gluttertest.Tester
	m          *ListViewModel
	ctrl       *ListController
	other      *widgets.FocusNode
	colors     Colors
	selected   []string
	opened     []string
	selections [][]string
	indexes    [][]int
	drops      []dropCall
	// canDrop is what CanDrop answers: refuse the target "x", accept the rest.
	rendered map[string]int
}

type listViewRigOpts struct {
	colors Colors
	size   ui.Size
	items  []ListViewItem
	model  ListViewModelOpts
	list   ListOpts
	// dnd makes the rows draggable; every drop is accepted unless refuse names the target.
	dnd    bool
	refuse string
}

func newListViewRig(t *testing.T, o listViewRigOpts) *listViewRig {
	t.Helper()
	r := &listViewRig{t: t, colors: o.colors, rendered: map[string]int{}, ctrl: NewListController()}
	if r.colors == (Colors{}) {
		r.colors = DarkModern()
	}
	if o.size == (ui.Size{}) {
		o.size = ui.Size{Width: 300, Height: 220}
	}
	if o.items == nil {
		o.items = letterItems(30)
	}
	r.m = NewListViewModel(o.model)
	if err := r.m.SetItems(o.items); err != nil {
		t.Fatal(err)
	}
	r.other = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "other"})
	t.Cleanup(r.other.Dispose)
	opts := o.list
	opts.Controller = r.ctrl
	opts.OnSelect = func(row ListViewRow) { r.selected = append(r.selected, row.Key) }
	opts.OnOpen = func(row ListViewRow) { r.opened = append(r.opened, row.Key) }
	opts.OnSelectionChange = func(keys []string, idx []int) {
		r.selections = append(r.selections, keys)
		r.indexes = append(r.indexes, idx)
	}
	if o.dnd {
		opts.DragAndDrop = &DragAndDrop{
			CanDrop: func(target string, _ DropPosition, _ []string) DropEffect {
				if target == o.refuse && o.refuse != "" {
					return DropEffectNone
				}
				return DropEffectMove
			},
			OnDrop: func(target string, pos DropPosition, keys []string) {
				r.drops = append(r.drops, dropCall{target, pos, slices.Clone(keys)})
			},
		}
	}
	if opts.Render == nil {
		opts.Render = func(bc widgets.BuildContext, row ListViewRow, st RowState) widgets.Widget {
			r.rendered[row.Key]++
			return defaultListRender(bc, row, st)
		}
	}
	col := widgets.NewColumn([]widgets.Widget{
		widgets.NewSizedBox(NewList(r.m, opts), widgets.SizedBoxOpts{Width: foundation.Some(o.size.Width), Height: foundation.Some(o.size.Height)}),
		widgets.NewSizedBox(widgets.NewFocus(widgets.NewSizedBox(nil), widgets.FocusOpts{FocusNode: r.other}), widgets.SizedBoxOpts{Width: foundation.Some(10.0), Height: foundation.Some(10.0)}),
	})
	r.ts = pumpThemed(t, r.colors, ui.Size{Width: o.size.Width, Height: o.size.Height + 10}, col)
	r.settle()
	return r
}

func (r *listViewRig) settle() {
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
}

// y is the vertical center of row i when the list is not scrolled and has no find box (rows are 22 px high).
func (r *listViewRig) y(i int) float64 { return float64(i)*22 + 11 }

func (r *listViewRig) tapRow(i int) {
	r.t.Helper()
	r.ts.TapAt(ui.Offset{Dx: 100, Dy: r.y(i)}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.settle()
}

// tapWith taps a row with modifier keys held.
func (r *listViewRig) tapWith(i int, mods ...services.LogicalKeyboardKey) {
	r.t.Helper()
	for _, k := range mods {
		r.ts.SendKeyDownEvent(k)
	}
	r.tapRow(i)
	for _, k := range slices.Backward(mods) {
		r.ts.SendKeyUpEvent(k)
	}
}

func (r *listViewRig) key(k services.LogicalKeyboardKey) {
	r.t.Helper()
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	r.settle()
}

func (r *listViewRig) keyWith(k services.LogicalKeyboardKey, mods ...services.LogicalKeyboardKey) {
	r.t.Helper()
	for _, m := range mods {
		r.ts.SendKeyDownEvent(m)
	}
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	for _, m := range slices.Backward(mods) {
		r.ts.SendKeyUpEvent(m)
	}
	r.settle()
}

// typeText types the characters of a text, each a key of its own.
func (r *listViewRig) typeText(text string) {
	r.t.Helper()
	for _, c := range text {
		logical := services.LogicalKeyboardKey(c)
		if c == ' ' {
			logical = services.LogicalKeyboardKeySpace
		}
		r.ts.SendKeyDownEvent(logical, gluttertest.KeyOpts{Character: string(c)})
		r.ts.SendKeyUpEvent(logical)
	}
	r.settle()
}

func (r *listViewRig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("errors: %v", errs)
	}
}

func (r *listViewRig) renderedKeys() []string {
	var out []string
	for k := range r.rendered {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func (r *listViewRig) focusList() {
	r.ctrl.Focus()
	r.settle()
}

func manyItems(n int) []ListViewItem {
	out := make([]ListViewItem, n)
	for i := range out {
		out[i] = ListViewItem{Key: fmt.Sprintf("k%d", i), Label: fmt.Sprintf("row %d", i)}
	}
	return out
}

var (
	keyControl = services.LogicalKeyboardKeyControl
	keyShift   = services.LogicalKeyboardKeyShift
	keyA       = services.LogicalKeyboardKeyKeyA
)

func TestListBuildsOnlyTheRowsInView(t *testing.T) {
	r := newListViewRig(t, listViewRigOpts{items: manyItems(2000)})
	keys := r.renderedKeys()
	if len(keys) < 10 || len(keys) > 30 {
		t.Errorf("rendered %d rows: %q", len(keys), keys)
	}
	if !slices.Contains(keys, "k0") || !slices.Contains(keys, "k9") || slices.Contains(keys, "k200") {
		t.Errorf("rendered %q", keys)
	}
	clear(r.rendered)
	r.ctrl.ScrollTo(22 * 1000)
	r.settle()
	keys = r.renderedKeys()
	if !slices.Contains(keys, "k1000") || !slices.Contains(keys, "k1009") || slices.Contains(keys, "k0") || len(keys) > 40 {
		t.Errorf("after scrolling: %d rows, %q", len(keys), keys)
	}
	if got := r.ctrl.ScrollOffset(); got != 22000 {
		t.Errorf("offset %v", got)
	}
	if h := r.ctrl.ViewportHeight(); h != 220 {
		t.Errorf("viewport %v", h)
	}
	r.noErrors()
}

func TestListRowsOfDifferentHeights(t *testing.T) {
	heights := func(i int) float64 {
		if i%2 == 0 {
			return 22
		}
		return 44
	}
	r := newListViewRig(t, listViewRigOpts{items: manyItems(500), model: ListViewModelOpts{HeightOf: heights}})
	keys := r.renderedKeys()
	if len(keys) > 25 || !slices.Contains(keys, "k0") || slices.Contains(keys, "k100") {
		t.Errorf("rendered %q", keys)
	}
	// row 3 spans 22+44+22 = 88 .. 132: a click at y 100 hits it
	r.ts.TapAt(ui.Offset{Dx: 100, Dy: 100}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.settle()
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"k3"}) {
		t.Errorf("selected %q", got)
	}
	r.noErrors()
}

func TestListClicks(t *testing.T) {
	t.Run("a click selects and focuses the row", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{})
		r.tapRow(2)
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"c"}) {
			t.Errorf("selected %q", got)
		}
		if got := focusKey(r.m); got != "c" || !r.ctrl.HasFocus() {
			t.Errorf("focus %q %v", got, r.ctrl.HasFocus())
		}
		if !slices.Equal(r.selected, []string{"c"}) || len(r.opened) != 0 {
			t.Errorf("OnSelect %q OnOpen %q", r.selected, r.opened)
		}
		if len(r.selections) != 1 || !slices.Equal(r.selections[0], []string{"c"}) || !slices.Equal(r.indexes[0], []int{2}) {
			t.Errorf("OnSelectionChange %q %v", r.selections, r.indexes)
		}
		r.tapRow(2) // the same row again: nothing changed
		if len(r.selections) != 1 {
			t.Errorf("no change: %q", r.selections)
		}
		r.noErrors()
	})
	t.Run("a double click opens", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{})
		r.tapRow(3)
		r.tapRow(3)
		if !slices.Equal(r.opened, []string{"d"}) {
			t.Errorf("OnOpen %q", r.opened)
		}
	})
	t.Run("ctrl and shift with MultiSelect", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{list: ListOpts{MultiSelect: true}})
		r.tapRow(1)
		r.tapWith(3, keyControl)
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"b", "d"}) {
			t.Errorf("ctrl: %q", got)
		}
		r.tapWith(6, keyShift)
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"b", "d", "e", "f", "g"}) {
			t.Errorf("shift: %q", got)
		}
		r.tapWith(3, keyControl)
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"b", "e", "f", "g"}) {
			t.Errorf("ctrl toggles off: %q", got)
		}
		want := [][]string{{"b"}, {"b", "d"}, {"b", "d", "e", "f", "g"}, {"b", "e", "f", "g"}}
		if len(r.selections) != len(want) {
			t.Fatalf("OnSelectionChange %q", r.selections)
		}
		for i, w := range want {
			if !slices.Equal(r.selections[i], w) {
				t.Errorf("event %d: %q, want %q", i, r.selections[i], w)
			}
		}
		if !slices.Equal(r.indexes[3], []int{1, 4, 5, 6}) {
			t.Errorf("indexes %v", r.indexes[3])
		}
		// ctrl+shift adds a range
		r.tapRow(7)
		r.tapWith(9, keyControl, keyShift)
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"h", "i", "j"}) {
			t.Errorf("ctrl+shift: %q", got)
		}
		// ctrl+shift adds the range from the ctrl click to the selection
		r.tapRow(2)
		r.tapWith(5, keyControl)
		r.tapWith(8, keyControl, keyShift)
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"c", "f", "g", "h", "i"}) {
			t.Errorf("ctrl+shift adds: %q", got)
		}
		r.noErrors()
	})
	t.Run("modifiers are ignored without MultiSelect", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{})
		r.tapRow(1)
		r.tapWith(3, keyControl)
		r.tapWith(5, keyShift)
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"f"}) {
			t.Errorf("selected %q", got)
		}
	})
}

func TestListKeys(t *testing.T) {
	t.Run("the arrows, home, end and the pages", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{items: manyItems(100)})
		r.focusList()
		if got := focusKey(r.m); got != "k0" {
			t.Errorf("a list that takes the focus focuses the first row: %q", got)
		}
		r.key(keyArrowDown)
		r.key(keyArrowDown)
		if got := focusKey(r.m); got != "k2" {
			t.Errorf("down: %q", got)
		}
		r.key(keyArrowUp)
		if got := focusKey(r.m); got != "k1" {
			t.Errorf("up: %q", got)
		}
		r.key(keyEnd)
		if got := focusKey(r.m); got != "k99" {
			t.Errorf("end: %q", got)
		}
		if got := r.ctrl.ScrollOffset(); got != 100*22-220 {
			t.Errorf("the focus is scrolled into view: %v", got)
		}
		r.key(keyHome)
		if got := focusKey(r.m); got != "k0" || r.ctrl.ScrollOffset() != 0 {
			t.Errorf("home: %q at %v", got, r.ctrl.ScrollOffset())
		}
		r.key(keyPageDown)
		if got := focusKey(r.m); got != "k9" {
			t.Errorf("page down: %q", got)
		}
		r.key(keyPageDown)
		if got := focusKey(r.m); got != "k19" {
			t.Errorf("page down again: %q", got)
		}
		r.key(keyPageUp)
		if got := focusKey(r.m); got != "k10" {
			t.Errorf("page up: %q", got)
		}
		r.noErrors()
	})
	t.Run("enter and space", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{})
		r.focusList()
		r.key(keyArrowDown)
		r.key(keyEnter)
		if !slices.Equal(r.selected, []string{"b"}) || !slices.Equal(r.opened, []string{"b"}) {
			t.Errorf("enter: %q %q", r.selected, r.opened)
		}
		r.key(keyArrowDown)
		r.key(keySpace)
		if !slices.Equal(r.selected, []string{"b", "c"}) || !slices.Equal(r.opened, []string{"b"}) {
			t.Errorf("space: %q %q", r.selected, r.opened)
		}
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"c"}) {
			t.Errorf("selected %q", got)
		}
	})
	t.Run("escape clears the selection and then the focus, and is not used when there is none", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{})
		r.tapRow(2)
		r.key(keyEscape)
		if r.m.SelectionCount() != 0 || focusKey(r.m) != "c" {
			t.Errorf("selection %q focus %q", r.m.SelectedKeys(), focusKey(r.m))
		}
		r.key(keyEscape)
		if focusKey(r.m) != "" {
			t.Errorf("focus %q", focusKey(r.m))
		}
		if r.ts.SendKeyDownEvent(keyEscape) {
			t.Error("nothing to clear: the key goes on")
		}
	})
	t.Run("keys with alt or meta are not used", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{})
		r.focusList()
		for _, mod := range []services.LogicalKeyboardKey{services.LogicalKeyboardKeyAlt, services.LogicalKeyboardKeyMeta} {
			r.ts.SendKeyDownEvent(mod)
			if r.ts.SendKeyDownEvent(keyArrowDown) {
				t.Errorf("%v+Down", mod)
			}
			r.ts.SendKeyUpEvent(keyArrowDown)
			r.ts.SendKeyUpEvent(mod)
		}
		if got := focusKey(r.m); got != "a" {
			t.Errorf("focus %q", got)
		}
	})
}

func TestListMultiSelectKeys(t *testing.T) {
	r := newListViewRig(t, listViewRigOpts{list: ListOpts{MultiSelect: true}})
	r.tapRow(2)
	r.keyWith(keyArrowDown, keyShift)
	r.keyWith(keyArrowDown, keyShift)
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"c", "d", "e"}) {
		t.Errorf("shift+down: %q", got)
	}
	r.keyWith(keyArrowUp, keyShift)
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"c", "d"}) {
		t.Errorf("shift+up: %q", got)
	}
	r.keyWith(keyA, keyControl)
	if r.m.SelectionCount() != 30 {
		t.Errorf("ctrl+a: %d", r.m.SelectionCount())
	}
	r.key(keyEscape)
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"d"}) {
		t.Errorf("escape: %q", got)
	}
	r.key(keyArrowDown)
	r.keyWith(keySpace, keyControl)
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"d", "e"}) {
		t.Errorf("ctrl+space: %q", got)
	}
	r.key(keyArrowDown)
	r.keyWith(keyEnter, keyControl, keyShift)
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"d", "e", "f"}) {
		t.Errorf("ctrl+shift+enter: %q", got)
	}
	if len(r.selections) == 0 {
		t.Error("OnSelectionChange")
	}
	// the keys are not used without MultiSelect
	single := newListViewRig(t, listViewRigOpts{})
	single.tapRow(2)
	single.keyWith(keyA, keyControl)
	single.keyWith(keyArrowDown, keyShift)
	if got := single.m.SelectedKeys(); !slices.Equal(got, []string{"c"}) || focusKey(single.m) != "c" {
		t.Errorf("the keys of several rows need MultiSelect: %q, focus %q", got, focusKey(single.m))
	}
	single.noErrors()
	r.noErrors()
}

func TestListTypeToFind(t *testing.T) {
	items := []ListViewItem{
		{Key: "1", Label: "main.go"}, {Key: "2", Label: "README.md"}, {Key: "3", Label: "go.mod"},
		{Key: "4", Label: "list.go"}, {Key: "5", Label: "notes.txt"},
	}
	t.Run("typing starts the find and highlights", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{items: items})
		r.focusList()
		r.typeText("go")
		st := r.m.Find()
		if !st.Open || !st.Typing || st.Pattern != "go" || st.MatchCount != 3 {
			t.Errorf("find %+v", st)
		}
		if !r.ts.Exists(gluttertest.ByText("Type to search")) && r.m.Find().Pattern == "" {
			t.Error("no find box")
		}
		if got := r.m.SelectedKeys(); len(got) != 0 {
			t.Errorf("typing selects nothing: %q", got)
		}
		if got := focusKey(r.m); got != "1" {
			t.Errorf("focus %q", got)
		}
		r.key(keyArrowDown)
		if got := focusKey(r.m); got != "3" {
			t.Errorf("down goes to the next match: %q", got)
		}
		// a space is typed while typing
		r.typeText(" ")
		if r.m.Find().Pattern != "go " {
			t.Errorf("pattern %q", r.m.Find().Pattern)
		}
		r.key(services.LogicalKeyboardKeyBackspace)
		r.key(services.LogicalKeyboardKeyBackspace)
		if r.m.Find().Pattern != "g" {
			t.Errorf("pattern %q", r.m.Find().Pattern)
		}
		// enter ends the typing and selects the focused row
		r.key(keyEnter)
		if r.m.Find().Typing || !r.m.Find().Open {
			t.Errorf("find %+v", r.m.Find())
		}
		if len(r.selected) != 1 {
			t.Errorf("selected %q", r.selected)
		}
		// space is a selection key again; a letter types again
		r.key(keySpace)
		if r.m.Find().Pattern != "g" {
			t.Errorf("space while not typing: %q", r.m.Find().Pattern)
		}
		r.typeText("o")
		if r.m.Find().Pattern != "go" || !r.m.Find().Typing {
			t.Errorf("find %+v", r.m.Find())
		}
		// escape closes the find
		r.key(keyEscape)
		if r.m.Find().Open || r.m.Find().Pattern != "" {
			t.Errorf("find %+v", r.m.Find())
		}
		if !r.ts.Exists(gluttertest.ByText("main.go")) {
			t.Error("rows are there")
		}
		r.noErrors()
	})
	t.Run("filter hides the rows that do not match", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{items: items, model: ListViewModelOpts{FindMode: FindModeFilter}})
		r.focusList()
		clear(r.rendered)
		r.typeText("go")
		if got := r.renderedKeys(); !slices.Equal(got, []string{"1", "3", "4"}) {
			t.Errorf("rows built after the filter: %q", got)
		}
		if r.ts.Exists(gluttertest.ByText("README.md")) || r.ts.Exists(gluttertest.ByText("notes.txt")) {
			t.Error("rows that do not match are hidden")
		}
		// a click on the second row hits the second match
		r.ts.TapAt(ui.Offset{Dx: 100, Dy: findBoxHeightOf(r) + r.y(1)}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
		r.settle()
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"3"}) {
			t.Errorf("selected %q", got)
		}
		if r.m.Find().Typing {
			t.Error("a click ends the typing")
		}
		r.noErrors()
	})
	t.Run("no results", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{items: items})
		r.focusList()
		r.typeText("zzz")
		if !r.ts.Exists(gluttertest.ByText("No results found.")) {
			t.Error("no message")
		}
		r.key(services.LogicalKeyboardKeyBackspace)
		r.key(services.LogicalKeyboardKeyBackspace)
		r.key(services.LogicalKeyboardKeyBackspace)
		if r.ts.Exists(gluttertest.ByText("No results found.")) {
			t.Error("the message is gone")
		}
		r.noErrors()
	})
	t.Run("the toggles and the close button", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{items: items})
		r.focusList()
		r.typeText("go")
		// the filter toggle is the second-to-last icon of the box
		toggles := gluttertest.ByType[*Toggle]()
		if r.ts.Count(toggles) != 2 {
			t.Fatalf("toggles: %d", r.ts.Count(toggles))
		}
		r.ts.Tap(gluttertest.First(toggles), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
		r.settle()
		if r.m.Find().Mode != FindModeFilter || r.m.Len() != 3 {
			t.Errorf("find %+v rows %d", r.m.Find(), r.m.Len())
		}
		r.ts.Tap(gluttertest.Last(toggles), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
		r.settle()
		if r.m.Find().MatchType != FindMatchContiguous {
			t.Errorf("find %+v", r.m.Find())
		}
		if !r.ctrl.HasFocus() {
			t.Error("the list keeps the focus")
		}
		r.ts.Tap(gluttertest.ByType[*IconButton](), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
		r.settle()
		if r.m.Find().Open || r.m.Len() != 5 || !r.ctrl.HasFocus() {
			t.Errorf("find %+v rows %d focus %v", r.m.Find(), r.m.Len(), r.ctrl.HasFocus())
		}
		r.noErrors()
	})
	t.Run("typing can be turned off", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{items: items, list: ListOpts{TypeToFind: foundation.Some(false)}})
		r.focusList()
		r.typeText("go")
		if r.m.Find().Open {
			t.Error("find is off")
		}
	})
	t.Run("control characters and shortcuts do not type", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{items: items})
		r.focusList()
		r.ts.SendKeyDownEvent(keyControl)
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyKeyC, gluttertest.KeyOpts{Character: "c"})
		r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyKeyC)
		r.ts.SendKeyUpEvent(keyControl)
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyTab, gluttertest.KeyOpts{Character: "\t"})
		if r.m.Find().Open {
			t.Error("a shortcut is not typing")
		}
	})
}

// findBoxHeightOf is the height of the find box above the rows: the rows are pushed down by it.
func findBoxHeightOf(r *listViewRig) float64 {
	r.t.Helper()
	return r.ts.TopLeft(gluttertest.ByType[*ScrollableElement]()).Dy
}

func TestListFocusAndHoverColors(t *testing.T) {
	for _, active := range []bool{true, false} {
		r := newListViewRig(t, listViewRigOpts{})
		r.tapRow(1)
		if !active {
			r.other.RequestFocus()
			r.settle()
		}
		want := r.colors.ListActiveSelectionBackground
		if !active {
			want = r.colors.ListInactiveSelectionBackground
		}
		if got := r.pixel(250, int(r.y(1))); got != nrgba(want) {
			t.Errorf("active %v: selected row %v, want %v", active, got, nrgba(want))
		}
	}
}

func (r *listViewRig) pixel(x, y int) color.NRGBA { return pixelAt(r.ts, x, y) }

// mouseDown is a mouse with its primary button held, as the pointer of a drag is.
var mouseDown = gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.PrimaryButton)}

// drag presses the mouse on a row and moves it to a position, with a first small move that starts the drag.
func (r *listViewRig) drag(row int, to ui.Offset) *gluttertest.TestGesture {
	r.t.Helper()
	g := r.ts.StartGesture(ui.Offset{Dx: 100, Dy: r.y(row)}, mouseDown)
	g.MoveTo(ui.Offset{Dx: 100, Dy: r.y(row) + dragSlop + 2})
	g.MoveTo(to)
	r.settle()
	return g
}

func TestListDragAndDrop(t *testing.T) {
	t.Run("a drag shows the label and the row it is over, and drops there", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{dnd: true})
		g := r.drag(1, ui.Offset{Dx: 120, Dy: r.y(4)})
		if !r.ts.Exists(gluttertest.ByText("item b")) || r.ts.Count(gluttertest.ByText("item b")) != 2 {
			t.Errorf("the label of the drag: %d texts", r.ts.Count(gluttertest.ByText("item b")))
		}
		if got := r.pixel(250, int(r.y(4))); got != nrgba(r.colors.ListDropBackground) {
			t.Errorf("the row under the drag is %v, want %v", got, nrgba(r.colors.ListDropBackground))
		}
		if got := r.pixel(250, int(r.y(5))); got == nrgba(r.colors.ListDropBackground) {
			t.Error("only that row")
		}
		g.Up()
		r.settle()
		want := []dropCall{{"e", DropInside, []string{"b"}}}
		if !slices.EqualFunc(r.drops, want, func(a, b dropCall) bool {
			return a.target == b.target && a.pos == b.pos && slices.Equal(a.keys, b.keys)
		}) {
			t.Errorf("drops %+v, want %+v", r.drops, want)
		}
		if r.ts.Count(gluttertest.ByText("item b")) != 1 {
			t.Error("the drag image is gone")
		}
		if len(r.selected) != 0 || r.m.SelectionCount() != 0 {
			t.Errorf("a drag is not a click: %q %q", r.selected, r.m.SelectedKeys())
		}
		r.noErrors()
	})
	t.Run("the positions before and after", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			dy     float64
			want   DropPosition
			lineAt int
		}{
			{"before", -8, DropBefore, 4 * 22},
			{"after", 8, DropAfter, 5*22 - 1},
		} {
			r := newListViewRig(t, listViewRigOpts{dnd: true})
			g := r.drag(1, ui.Offset{Dx: 120, Dy: r.y(4) + tc.dy})
			if got := r.pixel(150, tc.lineAt); got != nrgba(r.colors.ListDropBetweenBackground) {
				t.Errorf("%s: the line is %v, want %v", tc.name, got, nrgba(r.colors.ListDropBetweenBackground))
			}
			if got := r.pixel(150, int(r.y(4))); got == nrgba(r.colors.ListDropBackground) {
				t.Errorf("%s: the row is not highlighted", tc.name)
			}
			g.Up()
			r.settle()
			if len(r.drops) != 1 || r.drops[0].target != "e" || r.drops[0].pos != tc.want {
				t.Errorf("%s: drops %+v", tc.name, r.drops)
			}
		}
	})
	t.Run("below the rows is the root", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{dnd: true, items: letterItems(3)})
		g := r.drag(0, ui.Offset{Dx: 120, Dy: 150})
		if got := r.pixel(250, 150); got != nrgba(r.colors.ListDropBackground) {
			t.Errorf("the whole list is the target: %v", got)
		}
		g.Up()
		r.settle()
		if len(r.drops) != 1 || r.drops[0].target != "" || r.drops[0].pos != DropInside {
			t.Errorf("drops %+v", r.drops)
		}
	})
	t.Run("a target that refuses shows nothing and drops nothing", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{dnd: true, refuse: "e"})
		g := r.drag(1, ui.Offset{Dx: 120, Dy: r.y(4)})
		if got := r.pixel(250, int(r.y(4))); got == nrgba(r.colors.ListDropBackground) {
			t.Error("a row that refuses is not highlighted")
		}
		g.Up()
		r.settle()
		if len(r.drops) != 0 {
			t.Errorf("drops %+v", r.drops)
		}
	})
	t.Run("escape cancels the drag", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{dnd: true})
		g := r.drag(1, ui.Offset{Dx: 120, Dy: r.y(4)})
		r.key(keyEscape)
		if r.ts.Count(gluttertest.ByText("item b")) != 1 {
			t.Error("the drag image is gone")
		}
		if got := r.pixel(250, int(r.y(4))); got == nrgba(r.colors.ListDropBackground) {
			t.Error("no feedback after Escape")
		}
		g.MoveTo(ui.Offset{Dx: 120, Dy: r.y(5)})
		r.settle()
		if got := r.pixel(250, int(r.y(5))); got == nrgba(r.colors.ListDropBackground) {
			t.Error("the drag does not start again")
		}
		g.Up()
		r.settle()
		if len(r.drops) != 0 {
			t.Errorf("drops %+v", r.drops)
		}
		// the next drag works
		g = r.drag(2, ui.Offset{Dx: 120, Dy: r.y(5)})
		g.Up()
		r.settle()
		if len(r.drops) != 1 || r.drops[0].target != "f" || !slices.Equal(r.drops[0].keys, []string{"c"}) {
			t.Errorf("drops %+v", r.drops)
		}
		r.noErrors()
	})
	t.Run("a selected row drags the selection", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{dnd: true, list: ListOpts{MultiSelect: true}})
		r.tapRow(1)
		r.tapWith(2, keyControl)
		g := r.drag(2, ui.Offset{Dx: 120, Dy: r.y(6)})
		if !r.ts.Exists(gluttertest.ByText("2 items")) {
			t.Error("the label of a drag of two rows")
		}
		g.Up()
		r.settle()
		if len(r.drops) != 1 || !slices.Equal(r.drops[0].keys, []string{"b", "c"}) {
			t.Errorf("drops %+v", r.drops)
		}
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"b", "c"}) {
			t.Errorf("the selection stays: %q", got)
		}
		// a row that is not selected is dragged alone
		g = r.drag(4, ui.Offset{Dx: 120, Dy: r.y(7)})
		g.Up()
		r.settle()
		if len(r.drops) != 2 || !slices.Equal(r.drops[1].keys, []string{"e"}) {
			t.Errorf("drops %+v", r.drops)
		}
	})
	t.Run("CanDrag and DragLabel", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{dnd: true})
		_ = r
		var dropped []dropCall
		dnd := &DragAndDrop{
			CanDrag:   func(keys []string) bool { return keys[0] != "c" },
			DragLabel: func(keys []string) string { return "moving " + keys[0] },
			CanDrop:   func(string, DropPosition, []string) DropEffect { return DropEffectCopy },
			OnDrop: func(target string, pos DropPosition, keys []string) {
				dropped = append(dropped, dropCall{target, pos, keys})
			},
		}
		r2 := newListViewRig(t, listViewRigOpts{list: ListOpts{DragAndDrop: dnd}})
		g := r2.drag(2, ui.Offset{Dx: 120, Dy: r2.y(5)})
		g.Up()
		r2.settle()
		if len(dropped) != 0 {
			t.Errorf("the row c cannot be dragged: %+v", dropped)
		}
		g = r2.drag(1, ui.Offset{Dx: 120, Dy: r2.y(5)})
		if !r2.ts.Exists(gluttertest.ByText("moving b")) {
			t.Error("the label comes from DragLabel")
		}
		g.Up()
		r2.settle()
		if len(dropped) != 1 || dropped[0].target != "f" {
			t.Errorf("dropped %+v", dropped)
		}
	})
	t.Run("a press that moves less than the slop is a click", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{dnd: true})
		g := r.ts.StartGesture(ui.Offset{Dx: 100, Dy: r.y(2)}, mouseDown)
		g.MoveTo(ui.Offset{Dx: 101, Dy: r.y(2) + 1})
		g.Up()
		r.settle()
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"c"}) || len(r.drops) != 0 {
			t.Errorf("selected %q drops %+v", got, r.drops)
		}
	})
	t.Run("without DragAndDrop a drag is nothing", func(t *testing.T) {
		r := newListViewRig(t, listViewRigOpts{})
		g := r.drag(1, ui.Offset{Dx: 120, Dy: r.y(4)})
		g.Up()
		r.settle()
		if r.ts.Count(gluttertest.ByText("item b")) != 1 {
			t.Error("no drag image")
		}
	})
}

func TestListDragScrollsNearTheEdges(t *testing.T) {
	r := newListViewRig(t, listViewRigOpts{dnd: true, items: manyItems(200)})
	g := r.drag(1, ui.Offset{Dx: 120, Dy: 215}) // 5 px from the bottom of a view that is 220 high: 9 px per tick
	if got := r.ctrl.ScrollOffset(); got != 0 {
		t.Errorf("nothing before the first tick: %v", got)
	}
	for range 5 {
		r.ts.Pump(dragScrollInterval)
	}
	down := r.ctrl.ScrollOffset()
	if down < 40 || down > 60 {
		t.Errorf("after 5 ticks at 9 px: %v", down)
	}
	// the target follows the content: the pointer rests on another row
	g.Up()
	r.settle()
	if len(r.drops) != 1 || r.drops[0].target == "" || r.drops[0].target == "k5" {
		t.Errorf("drops %+v", r.drops)
	}
	r.noErrors()

	// at the top
	r = newListViewRig(t, listViewRigOpts{dnd: true, items: manyItems(200)})
	r.ctrl.ScrollTo(1000)
	r.settle()
	g = r.drag(5, ui.Offset{Dx: 120, Dy: 4})
	for range 4 {
		r.ts.Pump(dragScrollInterval)
	}
	if up := r.ctrl.ScrollOffset(); up > 1000-4*8 {
		t.Errorf("scrolled up from 1000 to %v", up)
	}
	// away from the edge the scrolling stops
	g.MoveTo(ui.Offset{Dx: 120, Dy: 110})
	stopped := r.ctrl.ScrollOffset()
	for range 4 {
		r.ts.Pump(dragScrollInterval)
	}
	if got := r.ctrl.ScrollOffset(); got != stopped {
		t.Errorf("still scrolling: %v -> %v", stopped, got)
	}
	g.Up()
	r.settle()
	r.noErrors()
}

func TestListControllerReveal(t *testing.T) {
	r := newListViewRig(t, listViewRigOpts{items: manyItems(500)})
	if r.ctrl.Reveal("nope") {
		t.Error("a key the list does not have")
	}
	if !r.ctrl.Reveal("k300") {
		t.Fatal("reveal")
	}
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
	want := float64(301*22) - 220 // the row is brought in at the bottom with the least scrolling
	if got := r.ctrl.ScrollOffset(); got != want {
		t.Errorf("offset %v, want %v", got, want)
	}
	if !slices.Contains(r.renderedKeys(), "k300") {
		t.Error("the row is built")
	}
	r.ctrl.Focus()
	r.settle()
	if !r.ctrl.HasFocus() {
		t.Error("focus")
	}
	r.noErrors()
}
