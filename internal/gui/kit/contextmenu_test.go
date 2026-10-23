package kit

import (
	"fmt"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

func TestLayoutPort(t *testing.T) {
	// the cases of VS Code's layout.test.ts
	cases := []struct {
		viewport, view float64
		a              LayoutAnchor
		want           float64
	}{
		{200, 20, LayoutAnchor{Offset: 0, Size: 0, Position: LayoutBefore}, 0},
		{200, 20, LayoutAnchor{Offset: 50, Size: 0, Position: LayoutBefore}, 50},
		{200, 20, LayoutAnchor{Offset: 200, Size: 0, Position: LayoutBefore}, 180},
		{200, 20, LayoutAnchor{Offset: 0, Size: 0, Position: LayoutAfter}, 0},
		{200, 20, LayoutAnchor{Offset: 50, Size: 0, Position: LayoutAfter}, 30},
		{200, 20, LayoutAnchor{Offset: 200, Size: 0, Position: LayoutAfter}, 180},
		{200, 20, LayoutAnchor{Offset: 0, Size: 50, Position: LayoutBefore}, 50},
		{200, 20, LayoutAnchor{Offset: 50, Size: 50, Position: LayoutBefore}, 100},
		{200, 20, LayoutAnchor{Offset: 150, Size: 50, Position: LayoutBefore}, 130},
		{200, 20, LayoutAnchor{Offset: 0, Size: 50, Position: LayoutAfter}, 50},
		{200, 20, LayoutAnchor{Offset: 50, Size: 50, Position: LayoutAfter}, 30},
		{200, 20, LayoutAnchor{Offset: 150, Size: 50, Position: LayoutAfter}, 130},
		// the aligned mode puts the view's edge on the anchor's
		{200, 20, LayoutAnchor{Offset: 50, Size: 50, Position: LayoutBefore, Mode: LayoutAlign}, 50},
		{200, 20, LayoutAnchor{Offset: 190, Size: 50, Position: LayoutBefore, Mode: LayoutAlign}, 240 - 20},
	}
	for _, tc := range cases {
		if got, _ := Layout(tc.viewport, tc.view, tc.a); got != tc.want {
			t.Errorf("Layout(%v, %v, %+v) = %v, want %v", tc.viewport, tc.view, tc.a, got, tc.want)
		}
	}
	results := []struct {
		name     string
		viewport float64
		view     float64
		a        LayoutAnchor
		want     LayoutResult
	}{
		{"fits", 200, 20, LayoutAnchor{Offset: 50, Position: LayoutBefore}, LayoutOK},
		{"flips", 200, 20, LayoutAnchor{Offset: 190, Position: LayoutBefore}, LayoutFlipped},
		{"overlaps", 200, 150, LayoutAnchor{Offset: 100, Size: 20, Position: LayoutBefore}, LayoutOverlap},
		{"after fits", 200, 20, LayoutAnchor{Offset: 50, Position: LayoutAfter}, LayoutOK},
		{"after flips", 200, 20, LayoutAnchor{Offset: 5, Size: 0, Position: LayoutAfter}, LayoutFlipped},
	}
	for _, tc := range results {
		if _, got := Layout(tc.viewport, tc.view, tc.a); got != tc.want {
			t.Errorf("%s: result %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestPlaceContextMenu(t *testing.T) {
	window := ui.Size{Width: 500, Height: 400}
	menu := ui.Size{Width: 200, Height: 150}
	cases := []struct {
		name string
		at   ui.Offset
		size ui.Size
		want ui.Offset
	}{
		// the pointer is a 2 px block: the menu starts just below it, at its x
		{"in the middle", ui.Offset{Dx: 100, Dy: 100}, menu, ui.Offset{Dx: 100, Dy: 102}},
		{"at the top left corner", ui.Offset{Dx: 0, Dy: 0}, menu, ui.Offset{Dx: 0, Dy: 2}},
		{"near the right edge it flips to the left of the pointer", ui.Offset{Dx: 450, Dy: 100}, menu, ui.Offset{Dx: 252, Dy: 102}},
		{"at the right edge", ui.Offset{Dx: 499, Dy: 100}, menu, ui.Offset{Dx: 301, Dy: 102}},
		{"just fits on the right", ui.Offset{Dx: 300, Dy: 100}, menu, ui.Offset{Dx: 300, Dy: 102}},
		{"one pixel too far right flips", ui.Offset{Dx: 301, Dy: 100}, menu, ui.Offset{Dx: 103, Dy: 102}},
		{"near the bottom edge it flips above the pointer", ui.Offset{Dx: 100, Dy: 350}, menu, ui.Offset{Dx: 100, Dy: 200}},
		{"at the bottom edge", ui.Offset{Dx: 100, Dy: 399}, menu, ui.Offset{Dx: 100, Dy: 249}},
		{"just fits below", ui.Offset{Dx: 100, Dy: 248}, menu, ui.Offset{Dx: 100, Dy: 250}},
		{"at the bottom right corner it flips both ways", ui.Offset{Dx: 450, Dy: 350}, menu, ui.Offset{Dx: 252, Dy: 200}},
		{"at the top right corner", ui.Offset{Dx: 450, Dy: 0}, menu, ui.Offset{Dx: 252, Dy: 2}},
		{"at the bottom left corner", ui.Offset{Dx: 0, Dy: 350}, menu, ui.Offset{Dx: 0, Dy: 200}},
		// taller than the window: it is pushed to the top and avoids the pointer sideways
		{"taller than the window", ui.Offset{Dx: 100, Dy: 100}, ui.Size{Width: 200, Height: 500}, ui.Offset{Dx: 102, Dy: 0}},
		{"as high as the window", ui.Offset{Dx: 100, Dy: 100}, ui.Size{Width: 200, Height: 400}, ui.Offset{Dx: 102, Dy: 0}},
		{"wider than the window", ui.Offset{Dx: 100, Dy: 100}, ui.Size{Width: 600, Height: 100}, ui.Offset{Dx: 0, Dy: 102}},
		{"too high for either side but not for the window", ui.Offset{Dx: 100, Dy: 200}, ui.Size{Width: 200, Height: 300}, ui.Offset{Dx: 102, Dy: 100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlaceContextMenu(tc.at, tc.size, window); got != tc.want {
				t.Errorf("PlaceContextMenu(%v) = %v, want %v", tc.at, got, tc.want)
			}
		})
	}
}

func TestPlaceSubmenu(t *testing.T) {
	window := ui.Size{Width: 500, Height: 400}
	sub := ui.Size{Width: 150, Height: 100}
	// the item is the row of the parent (24 px) with the padding of the parent above and below
	item := func(left, top, right float64) ui.Rect { return ui.NewRectFromLTRB(left, top-4, right, top+24+4) }
	cases := []struct {
		name   string
		item   ui.Rect
		size   ui.Size
		window ui.Size
		want   ui.Offset
	}{
		{"to the right of the item, its first row level with the item", item(100, 50, 300), sub, window, ui.Offset{Dx: 300, Dy: 46}},
		{"flips to the left without room on the right", item(300, 50, 480), sub, window, ui.Offset{Dx: 150, Dy: 46}},
		{"just fits on the right", item(100, 50, 350), sub, window, ui.Offset{Dx: 350, Dy: 46}},
		{"ends at the item's bottom edge without room below", item(100, 350, 300), sub, window, ui.Offset{Dx: 300, Dy: 278}},
		{"overlaps the item by 10 px without room on either side", item(10, 50, 290), ui.Size{Width: 280, Height: 100}, ui.Size{Width: 300, Height: 400}, ui.Offset{Dx: 20, Dy: 56}},
		{"flips left and ends at the bottom edge of the item", item(300, 350, 480), sub, window, ui.Offset{Dx: 150, Dy: 278}},
		{"taller than the window starts at the top", item(100, 50, 300), ui.Size{Width: 150, Height: 500}, window, ui.Offset{Dx: 300, Dy: 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlaceSubmenu(tc.item, tc.size, tc.window); got != tc.want {
				t.Errorf("PlaceSubmenu(%v) = %v, want %v", tc.item, got, tc.want)
			}
		})
	}
}

// ctxRig is a window with a focusable area under a context menu host.
type ctxRig struct {
	ts       *gluttertest.Tester
	ctrl     *ContextMenuController
	handle   *MenuHandle
	ran      []string
	hidden   int
	areaTaps int
	area     *widgets.FocusNode
}

func newCtxRig(t *testing.T, size ui.Size, items []MenuItem, opts ContextMenuOpts) *ctxRig {
	t.Helper()
	r := &ctxRig{ctrl: NewContextMenuController(), handle: NewMenuHandle()}
	r.area = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "area"})
	t.Cleanup(r.area.Dispose)
	opts.Controller = r.ctrl
	opts.Handle = r.handle
	opts.OnRun = func(it MenuItem) { r.ran = append(r.ran, it.ID) }
	opts.OnHide = func() { r.hidden++ }
	area := widgets.NewGestureDetector(
		widgets.NewFocus(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(size.Width), Height: foundation.Some(size.Height)}),
			widgets.FocusOpts{FocusNode: r.area, Autofocus: true}),
		widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { r.areaTaps++ }})
	r.ts = pumpSized(t, size, NewContextMenu(area, opts))
	r.ts.Pump(time.Millisecond)
	return r
}

func (r *ctxRig) show(at ui.Offset, items []MenuItem) {
	r.ctrl.Show(at, items)
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
}

func (r *ctxRig) menuRect() ui.Rect {
	f := gluttertest.ByType[*Menu]()
	tl := r.ts.TopLeft(f)
	sz := r.ts.Size(f)
	return ui.NewRectFromLTWH(tl.Dx, tl.Dy, sz.Width, sz.Height)
}

func TestContextMenuShowsAtThePointer(t *testing.T) {
	r := newCtxRig(t, ui.Size{Width: 600, Height: 400}, nil, ContextMenuOpts{})
	if r.ctrl.IsShowing() {
		t.Fatal("the menu is showing at first")
	}
	r.show(ui.Offset{Dx: 120, Dy: 80}, testMenuItems())
	if !r.ctrl.IsShowing() || !r.ts.Exists(gluttertest.ByText("New File")) {
		t.Fatal("the menu did not open")
	}
	got := r.menuRect()
	if got.Left != 120 || got.Top != 82 {
		t.Errorf("the menu is at (%v, %v), want (120, 82) for a pointer at (120, 80)", got.Left, got.Top)
	}
	// mnemonics are off: no marker and no underline
	if r.ts.Exists(gluttertest.ByText("&New File")) {
		t.Error("a mnemonic marker is shown")
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestContextMenuStaysInTheWindow(t *testing.T) {
	size := ui.Size{Width: 500, Height: 400}
	points := []ui.Offset{{Dx: 5, Dy: 5}, {Dx: 495, Dy: 5}, {Dx: 5, Dy: 395}, {Dx: 495, Dy: 395}, {Dx: 250, Dy: 395}, {Dx: 495, Dy: 200}}
	for _, at := range points {
		t.Run(fmt.Sprint(at), func(t *testing.T) {
			r := newCtxRig(t, size, nil, ContextMenuOpts{})
			r.show(at, testMenuItems())
			m := r.menuRect()
			if m.Left < 0 || m.Top < 0 || m.Right > size.Width || m.Bottom > size.Height {
				t.Errorf("menu %v is not inside the %v window for a pointer at %v", m, size, at)
			}
			want := PlaceContextMenu(at, ui.Size{Width: m.Width(), Height: m.Height()}, size)
			if m.TopLeft() != want {
				t.Errorf("menu at %v, PlaceContextMenu says %v", m.TopLeft(), want)
			}
		})
	}
}

func TestContextMenuTallMenuScrolls(t *testing.T) {
	var items []MenuItem
	for i := range 40 {
		items = append(items, MenuItem{ID: fmt.Sprint(i), Label: fmt.Sprintf("Item %d", i)})
	}
	size := ui.Size{Width: 500, Height: 300}
	r := newCtxRig(t, size, nil, ContextMenuOpts{})
	r.show(ui.Offset{Dx: 50, Dy: 50}, items)
	m := r.menuRect()
	if m.Height() > size.Height-menuScreenMargin+0.5 || m.Top < 0 || m.Bottom > size.Height {
		t.Errorf("the menu %v is higher than the window less 35 px", m)
	}
	// the keyboard scrolls to the last item
	key(r.ts, services.LogicalKeyboardKeyEnd)
	r.ts.Pump(time.Millisecond)
	c := r.ts.Center(gluttertest.ByText("Item 39"))
	if c.Dy < m.Top || c.Dy > m.Bottom {
		t.Errorf("the last item is at y=%v, outside the menu %v", c.Dy, m)
	}
}

func TestContextMenuCloses(t *testing.T) {
	down, enter, esc := services.LogicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyEscape
	cases := []struct {
		name    string
		do      func(r *ctxRig)
		wantRan string
		wantTap int
	}{
		{"Escape", func(r *ctxRig) { key(r.ts, esc) }, "[]", 0},
		{"a click outside the menu, which goes on to what is under it", func(r *ctxRig) { r.ts.TapAt(ui.Offset{Dx: 400, Dy: 300}) }, "[]", 1},
		{"an item that runs by Enter", func(r *ctxRig) { key(r.ts, down); key(r.ts, enter) }, "[new]", 0},
		{"an item that runs by a click", func(r *ctxRig) { r.ts.Tap(gluttertest.ByText("Open...")) }, "[open]", 0},
		{"an item of a submenu", func(r *ctxRig) {
			r.ts.Tap(gluttertest.ByText("Recent"))
			r.ts.Pump(time.Millisecond)
			r.ts.Tap(gluttertest.ByText("Two"))
		}, "[r2]", 0},
		{"Hide", func(r *ctxRig) { r.ctrl.Hide() }, "[]", 0},
		{"the window changing size", func(r *ctxRig) { r.ts.SetViewSize(ui.Size{Width: 600, Height: 400}) }, "[]", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newCtxRig(t, ui.Size{Width: 500, Height: 400}, nil, ContextMenuOpts{})
			r.show(ui.Offset{Dx: 50, Dy: 50}, testMenuItems())
			tc.do(r)
			r.ts.Pump(time.Millisecond)
			r.ts.Pump(time.Millisecond)
			if r.ctrl.IsShowing() || r.ts.Exists(gluttertest.ByText("New File")) {
				t.Error("the menu is still open")
			}
			if r.hidden != 1 {
				t.Errorf("OnHide was called %d times, want 1", r.hidden)
			}
			if got := fmt.Sprint(r.ran); got != tc.wantRan {
				t.Errorf("ran %s, want %s", got, tc.wantRan)
			}
			if r.areaTaps != tc.wantTap {
				t.Errorf("the area under the menu got %d taps, want %d", r.areaTaps, tc.wantTap)
			}
		})
	}
}

func TestContextMenuTakesAndGivesBackTheFocus(t *testing.T) {
	r := newCtxRig(t, ui.Size{Width: 500, Height: 400}, nil, ContextMenuOpts{})
	if !r.area.HasPrimaryFocus() {
		t.Fatal("the area does not have the focus at first")
	}
	r.show(ui.Offset{Dx: 50, Dy: 50}, testMenuItems())
	if r.area.HasPrimaryFocus() {
		t.Error("the area kept the focus although the menu is open")
	}
	key(r.ts, services.LogicalKeyboardKeyArrowDown)
	if r.handle.FocusedIndex() != 0 {
		t.Errorf("the keyboard does not reach the menu: focus %d", r.handle.FocusedIndex())
	}
	key(r.ts, services.LogicalKeyboardKeyEscape)
	r.ts.Pump(time.Millisecond)
	if !r.area.HasPrimaryFocus() {
		t.Error("the focus did not go back to the area when the menu closed")
	}
}

func TestContextMenuShowAgainMovesIt(t *testing.T) {
	r := newCtxRig(t, ui.Size{Width: 500, Height: 400}, nil, ContextMenuOpts{})
	r.show(ui.Offset{Dx: 50, Dy: 50}, testMenuItems())
	r.show(ui.Offset{Dx: 200, Dy: 100}, []MenuItem{{ID: "only", Label: "Only"}})
	if r.ts.Exists(gluttertest.ByText("New File")) || !r.ts.Exists(gluttertest.ByText("Only")) {
		t.Fatal("the second menu did not replace the first")
	}
	if m := r.menuRect(); m.Left != 200 || m.Top != 102 {
		t.Errorf("menu at (%v, %v), want (200, 102)", m.Left, m.Top)
	}
}

func TestContextMenuNeedsItems(t *testing.T) {
	r := newCtxRig(t, ui.Size{Width: 500, Height: 400}, nil, ContextMenuOpts{})
	r.show(ui.Offset{Dx: 50, Dy: 50}, nil)
	r.show(ui.Offset{Dx: 50, Dy: 50}, []MenuItem{SeparatorItem()})
	if r.ctrl.IsShowing() {
		t.Error("a menu without items opened")
	}
}

func TestContextMenuOptions(t *testing.T) {
	t.Run("mnemonics", func(t *testing.T) {
		r := newCtxRig(t, ui.Size{Width: 500, Height: 400}, nil, ContextMenuOpts{EnableMnemonics: true})
		r.show(ui.Offset{Dx: 50, Dy: 50}, testMenuItems())
		key(r.ts, services.LogicalKeyboardKeyKeyX)
		if fmt.Sprint(r.ran) != "[exit]" {
			t.Errorf("ran %v", r.ran)
		}
	})
	t.Run("select first", func(t *testing.T) {
		r := newCtxRig(t, ui.Size{Width: 500, Height: 400}, nil, ContextMenuOpts{SelectFirst: true})
		r.show(ui.Offset{Dx: 50, Dy: 50}, testMenuItems())
		if r.handle.FocusedIndex() != 0 {
			t.Errorf("focus %d, want 0", r.handle.FocusedIndex())
		}
	})
	t.Run("nothing is focused by default", func(t *testing.T) {
		r := newCtxRig(t, ui.Size{Width: 500, Height: 400}, nil, ContextMenuOpts{})
		r.show(ui.Offset{Dx: 50, Dy: 50}, testMenuItems())
		if r.handle.FocusedIndex() != -1 {
			t.Errorf("focus %d, want -1", r.handle.FocusedIndex())
		}
	})
}

func TestContextMenuRegionOpensOnRightClick(t *testing.T) {
	var asked int
	var ran []string
	items := func() []MenuItem { asked++; return testMenuItems() }
	child := widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(400.0), Height: foundation.Some(300.0)})
	ts := pumpSized(t, ui.Size{Width: 500, Height: 400}, NewContextMenuRegion(child, items, ContextMenuOpts{OnRun: func(it MenuItem) { ran = append(ran, it.ID) }}))
	secondary := gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)}
	// a left click opens nothing
	ts.TapAt(ui.Offset{Dx: 100, Dy: 100}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	ts.Pump(time.Millisecond)
	if asked != 0 || ts.Exists(gluttertest.ByText("New File")) {
		t.Fatal("a left click opened the menu")
	}
	ts.TapAt(ui.Offset{Dx: 100, Dy: 100}, secondary)
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if asked != 1 || !ts.Exists(gluttertest.ByText("New File")) {
		t.Fatalf("a right click did not open the menu (items asked %d times)", asked)
	}
	m := ts.TopLeft(gluttertest.ByType[*Menu]())
	if m.Dx != 100 || m.Dy != 102 {
		t.Errorf("menu at %v, want (100, 102)", m)
	}
	ts.Tap(gluttertest.ByText("New File"))
	ts.Pump(time.Millisecond)
	if fmt.Sprint(ran) != "[new]" || ts.Exists(gluttertest.ByText("New File")) {
		t.Errorf("ran %v, or the menu stays open", ran)
	}
	// a second right click, with the menu open, opens the menu at the new place
	ts.TapAt(ui.Offset{Dx: 100, Dy: 100}, secondary)
	ts.Pump(time.Millisecond)
	ts.TapAt(ui.Offset{Dx: 380, Dy: 280}, secondary)
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	menuSize := ts.Size(gluttertest.ByType[*Menu]())
	if m, want := ts.TopLeft(gluttertest.ByType[*Menu]()), PlaceContextMenu(ui.Offset{Dx: 380, Dy: 280}, menuSize, ui.Size{Width: 500, Height: 400}); m != want {
		t.Errorf("second menu at %v, want %v (flipped to stay in the window)", m, want)
	}
	if !ts.Exists(gluttertest.ByType[*Menu]()) || asked != 3 {
		t.Errorf("items asked %d times, want 3", asked)
	}
}

func TestContextMenuRegionWithoutItems(t *testing.T) {
	child := widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(400.0), Height: foundation.Some(300.0)})
	ts := pumpSized(t, ui.Size{Width: 500, Height: 400}, NewContextMenuRegion(child, func() []MenuItem { return nil }))
	ts.TapAt(ui.Offset{Dx: 100, Dy: 100}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)})
	ts.Pump(time.Millisecond)
	if ts.Exists(gluttertest.ByType[*Menu]()) {
		t.Error("a menu opened for no items")
	}
}

func TestContextMenuRegionOpensWithTheKeyboard(t *testing.T) {
	cases := []struct {
		name string
		keys func(ts *gluttertest.Tester)
		want bool
	}{
		{"the menu key", func(ts *gluttertest.Tester) { ts.SendKeyEvent(services.LogicalKeyboardKeyContextMenu) }, true},
		{"Shift+F10", func(ts *gluttertest.Tester) {
			ts.SendKeyDownEvent(services.LogicalKeyboardKeyShiftLeft)
			ts.SendKeyEvent(services.LogicalKeyboardKeyF10)
			ts.SendKeyUpEvent(services.LogicalKeyboardKeyShiftLeft)
		}, true},
		{"F10 alone", func(ts *gluttertest.Tester) { ts.SendKeyEvent(services.LogicalKeyboardKeyF10) }, false},
		{"another key", func(ts *gluttertest.Tester) { ts.SendKeyEvent(services.LogicalKeyboardKeyKeyA) }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := widgets.NewFocusNode(widgets.FocusNodeOpts{})
			defer node.Dispose()
			// the widget with the focus is a 100 by 40 box at (20, 30)
			child := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 30, 0, 0), widgets.NewFocus(
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(100.0), Height: foundation.Some(40.0)}),
				widgets.FocusOpts{FocusNode: node, Autofocus: true}))
			ts := pumpSized(t, ui.Size{Width: 500, Height: 400}, NewContextMenuRegion(child, testMenuItemsFunc))
			ts.Pump(time.Millisecond)
			tc.keys(ts)
			ts.Pump(time.Millisecond)
			ts.Pump(time.Millisecond)
			if got := ts.Exists(gluttertest.ByType[*Menu]()); got != tc.want {
				t.Fatalf("menu open = %v, want %v", got, tc.want)
			}
			if tc.want {
				if at := ts.TopLeft(gluttertest.ByType[*Menu]()); at.Dx != 20 || at.Dy != 72 {
					t.Errorf("menu at %v, want (20, 72) under the focused box", at)
				}
			}
		})
	}
}

func testMenuItemsFunc() []MenuItem { return testMenuItems() }
