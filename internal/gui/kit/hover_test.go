package kit

import (
	"fmt"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

func TestPlaceHover(t *testing.T) {
	window := ui.Size{Width: 400, Height: 300}
	hover := ui.Size{Width: 100, Height: 20}
	cases := []struct {
		name    string
		target  ui.Rect
		size    ui.Size
		pos     HoverPosition
		want    ui.Offset
		wantPos HoverPosition
	}{
		{"below, left edges aligned", ui.NewRectFromLTWH(50, 40, 22, 22), hover, HoverBelow, ui.Offset{Dx: 50, Dy: 62}, HoverBelow},
		{"above, left edges aligned", ui.NewRectFromLTWH(50, 140, 22, 22), hover, HoverAbove, ui.Offset{Dx: 50, Dy: 120}, HoverAbove},
		{"below flips above at the bottom edge", ui.NewRectFromLTWH(50, 270, 22, 22), hover, HoverBelow, ui.Offset{Dx: 50, Dy: 250}, HoverAbove},
		{"above flips below at the top edge", ui.NewRectFromLTWH(50, 5, 22, 22), hover, HoverAbove, ui.Offset{Dx: 50, Dy: 27}, HoverBelow},
		{"too big for either side stays inside the window", ui.NewRectFromLTWH(50, 140, 22, 22), ui.Size{Width: 100, Height: 290}, HoverBelow, ui.Offset{Dx: 50, Dy: 10}, HoverBelow},
		{"near the right edge it moves left", ui.NewRectFromLTWH(380, 40, 20, 22), hover, HoverBelow, ui.Offset{Dx: 298, Dy: 62}, HoverBelow},
		{"wider than the window starts at the left edge", ui.NewRectFromLTWH(50, 40, 22, 22), ui.Size{Width: 500, Height: 20}, HoverBelow, ui.Offset{Dx: 0, Dy: 62}, HoverBelow},
		{"right of the target", ui.NewRectFromLTWH(50, 40, 22, 22), hover, HoverRight, ui.Offset{Dx: 72, Dy: 40}, HoverRight},
		{"right flips left when it does not fit", ui.NewRectFromLTWH(330, 40, 22, 22), hover, HoverRight, ui.Offset{Dx: 230, Dy: 40}, HoverLeft},
		{"right falls back below when neither side fits", ui.NewRectFromLTWH(30, 40, 340, 22), ui.Size{Width: 100, Height: 20}, HoverRight, ui.Offset{Dx: 30, Dy: 62}, HoverBelow},
		{"left of the target", ui.NewRectFromLTWH(250, 40, 22, 22), hover, HoverLeft, ui.Offset{Dx: 150, Dy: 40}, HoverLeft},
		{"left flips right when it does not fit", ui.NewRectFromLTWH(20, 40, 22, 22), hover, HoverLeft, ui.Offset{Dx: 42, Dy: 40}, HoverRight},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, pos := PlaceHover(tc.target, tc.size, window, tc.pos)
			if got != tc.want || pos != tc.wantPos {
				t.Errorf("got %v on side %d, want %v on side %d", got, pos, tc.want, tc.wantPos)
			}
		})
	}
}

// hoverTarget is a 40 by 22 target with a hover.
func hoverTarget(opts HoverOpts) widgets.Widget {
	return NewHover(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(40.0), Height: foundation.Some(22.0)}), opts)
}

func shown(ts *gluttertest.Tester, text string) bool { return ts.Exists(gluttertest.ByText(text)) }

func TestHoverDelay(t *testing.T) {
	target := func() widgets.Widget {
		return widgets.NewPadding(painting.NewEdgeInsetsAll(50), hoverTarget(HoverOpts{Text: "tip"}))
	}
	t.Run("shows after the delay and not before", func(t *testing.T) {
		ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, target())
		ts.Hover(ui.Offset{Dx: 70, Dy: 60})
		ts.Pump(DefaultHoverDelay - time.Millisecond)
		if shown(ts, "tip") {
			t.Fatal("shown before the delay")
		}
		ts.Pump(2 * time.Millisecond)
		if !shown(ts, "tip") {
			t.Fatal("not shown after the delay")
		}
	})
	t.Run("leaving the target hides it", func(t *testing.T) {
		ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, target())
		ts.Hover(ui.Offset{Dx: 70, Dy: 60})
		ts.Pump(DefaultHoverDelay + time.Millisecond)
		ts.Hover(ui.Offset{Dx: 250, Dy: 150})
		ts.Pump(time.Millisecond)
		if shown(ts, "tip") {
			t.Error("still shown")
		}
	})
	t.Run("leaving before the delay cancels it", func(t *testing.T) {
		ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, target())
		ts.Hover(ui.Offset{Dx: 70, Dy: 60})
		ts.Pump(DefaultHoverDelay / 2)
		ts.Hover(ui.Offset{Dx: 250, Dy: 150})
		ts.Pump(2 * DefaultHoverDelay)
		if shown(ts, "tip") {
			t.Error("shown after the pointer left")
		}
	})
	t.Run("a press on the target hides it", func(t *testing.T) {
		ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, target())
		ts.Hover(ui.Offset{Dx: 70, Dy: 60})
		ts.Pump(DefaultHoverDelay + time.Millisecond)
		ts.TapAt(ui.Offset{Dx: 70, Dy: 60})
		ts.Pump(time.Millisecond)
		if shown(ts, "tip") {
			t.Error("still shown after a click")
		}
	})
	t.Run("a delay of its own", func(t *testing.T) {
		ts := pumpSized(t, ui.Size{Width: 300, Height: 200},
			widgets.NewPadding(painting.NewEdgeInsetsAll(50), hoverTarget(HoverOpts{Text: "tip", Delay: foundation.Some(50 * time.Millisecond)})))
		ts.Hover(ui.Offset{Dx: 70, Dy: 60})
		ts.Pump(40 * time.Millisecond)
		if shown(ts, "tip") {
			t.Fatal("shown early")
		}
		ts.Pump(20 * time.Millisecond)
		if !shown(ts, "tip") {
			t.Error("not shown")
		}
	})
	t.Run("a delay of zero shows at once", func(t *testing.T) {
		ts := pumpSized(t, ui.Size{Width: 300, Height: 200},
			widgets.NewPadding(painting.NewEdgeInsetsAll(50), hoverTarget(HoverOpts{Text: "tip", Delay: foundation.Some(time.Duration(0))})))
		ts.Hover(ui.Offset{Dx: 70, Dy: 60})
		ts.Pump(time.Millisecond)
		if !shown(ts, "tip") {
			t.Error("not shown")
		}
	})
	t.Run("without content nothing shows", func(t *testing.T) {
		// Ported from VS Code: src/vs/platform/hover/test/browser/hoverService.test.ts ("should not show hover with empty content")
		ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, widgets.NewPadding(painting.NewEdgeInsetsAll(50), hoverTarget(HoverOpts{})))
		ts.Hover(ui.Offset{Dx: 70, Dy: 60})
		ts.Pump(2 * DefaultHoverDelay)
		if errs := ts.TakeErrors(); len(errs) != 0 || ts.Count(gluttertest.ByType[*widgets.CustomSingleChildLayout]()) != 0 {
			t.Errorf("errors %v, popups %d", errs, ts.Count(gluttertest.ByType[*widgets.CustomSingleChildLayout]()))
		}
	})
}

func TestHoverKeyboard(t *testing.T) {
	cases := []struct {
		name     string
		key      services.LogicalKeyboardKey
		hidesNow bool
	}{
		{"Escape", services.LogicalKeyboardKeyEscape, true},
		{"a letter", services.LogicalKeyboardKeyKeyA, true},
		{"Shift alone", services.LogicalKeyboardKeyShiftLeft, false},
		{"Control alone", services.LogicalKeyboardKeyControlLeft, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, widgets.NewPadding(painting.NewEdgeInsetsAll(50), hoverTarget(HoverOpts{Text: "tip"})))
			ts.Hover(ui.Offset{Dx: 70, Dy: 60})
			ts.Pump(DefaultHoverDelay + time.Millisecond)
			if !shown(ts, "tip") {
				t.Fatal("not shown")
			}
			ts.SendKeyDownEvent(tc.key)
			ts.SendKeyUpEvent(tc.key)
			ts.Pump(time.Millisecond)
			if shown(ts, "tip") == tc.hidesNow {
				t.Errorf("shown after the key: %v", !tc.hidesNow)
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
	t.Run("Escape during the delay cancels the hover", func(t *testing.T) {
		ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, widgets.NewPadding(painting.NewEdgeInsetsAll(50), hoverTarget(HoverOpts{Text: "tip"})))
		ts.Hover(ui.Offset{Dx: 70, Dy: 60})
		ts.Pump(DefaultHoverDelay / 2)
		key(ts, services.LogicalKeyboardKeyEscape)
		ts.Pump(DefaultHoverDelay)
		if shown(ts, "tip") {
			t.Error("the hover showed after Escape")
		}
	})
	t.Run("the key goes on to the focused widget", func(t *testing.T) {
		var got []services.LogicalKeyboardKey
		node := widgets.NewFocusNode(widgets.FocusNodeOpts{})
		defer node.Dispose()
		child := widgets.NewFocus(widgets.NewPadding(painting.NewEdgeInsetsAll(50), hoverTarget(HoverOpts{Text: "tip"})), widgets.FocusOpts{
			FocusNode: node, Autofocus: true,
			OnKeyEvent: func(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
				if _, ok := e.(*services.KeyDownEvent); ok {
					got = append(got, e.LogicalKey())
				}
				return widgets.KeyEventResultHandled
			}})
		ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, child)
		ts.Hover(ui.Offset{Dx: 70, Dy: 60})
		ts.Pump(DefaultHoverDelay + time.Millisecond)
		key(ts, services.LogicalKeyboardKeyEscape)
		if fmt.Sprint(got) != fmt.Sprint([]services.LogicalKeyboardKey{services.LogicalKeyboardKeyEscape}) || shown(ts, "tip") {
			t.Errorf("the focused widget got %v, hover shown %v", got, shown(ts, "tip"))
		}
	})
}

func TestHoverInstantAfterAnother(t *testing.T) {
	pair := func(delegate *HoverDelegate) widgets.Widget {
		return widgets.NewPadding(painting.NewEdgeInsetsAll(50), widgets.NewRow([]widgets.Widget{
			hoverTarget(HoverOpts{Text: "first", Delegate: delegate}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(20.0)}),
			hoverTarget(HoverOpts{Text: "second", Delegate: delegate}),
		}))
	}
	first, second := ui.Offset{Dx: 70, Dy: 60}, ui.Offset{Dx: 130, Dy: 60}
	cases := []struct {
		name       string
		instant    bool
		gap        time.Duration // time between leaving the first and entering the second target
		wantAtOnce bool
	}{
		{"an instant delegate shows the next one at once", true, 0, true},
		{"within the time limit", true, 150 * time.Millisecond, true},
		{"after the time limit the delay applies again", true, 250 * time.Millisecond, false},
		{"a delegate that is not instant keeps the delay", false, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delegate := NewHoverDelegate(HoverDelegateOpts{Instant: tc.instant})
			ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, pair(delegate))
			ts.Hover(first)
			ts.Pump(DefaultHoverDelay + time.Millisecond)
			if !shown(ts, "first") || !delegate.Visible() {
				t.Fatal("the first hover is not shown")
			}
			ts.Hover(ui.Offset{Dx: 250, Dy: 150})
			ts.Pump(tc.gap + time.Millisecond)
			if shown(ts, "first") {
				t.Fatal("the first hover is still shown")
			}
			ts.Hover(second)
			ts.Pump(time.Millisecond)
			if got := shown(ts, "second"); got != tc.wantAtOnce {
				t.Errorf("second hover shown at once: %v, want %v", got, tc.wantAtOnce)
			}
			ts.Pump(DefaultHoverDelay + time.Millisecond)
			if !shown(ts, "second") {
				t.Error("the second hover never showed")
			}
			if shown(ts, "first") {
				t.Error("two hovers of one delegate are shown")
			}
		})
	}
	t.Run("moving straight to the next target", func(t *testing.T) {
		delegate := NewHoverDelegate(HoverDelegateOpts{Instant: true})
		ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, pair(delegate))
		ts.Hover(first)
		ts.Pump(DefaultHoverDelay + time.Millisecond)
		ts.Hover(second)
		ts.Pump(time.Millisecond)
		if !shown(ts, "second") || shown(ts, "first") {
			t.Errorf("first %v, second %v", shown(ts, "first"), shown(ts, "second"))
		}
	})
	t.Run("the delay of the delegate", func(t *testing.T) {
		delegate := NewHoverDelegate(HoverDelegateOpts{Delay: foundation.Some(100 * time.Millisecond)})
		ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, pair(delegate))
		ts.Hover(first)
		ts.Pump(90 * time.Millisecond)
		if shown(ts, "first") {
			t.Fatal("shown early")
		}
		ts.Pump(20 * time.Millisecond)
		if !shown(ts, "first") || delegate.Delay() != 100*time.Millisecond {
			t.Errorf("shown %v, delay %v", shown(ts, "first"), delegate.Delay())
		}
	})
}

func TestHoverController(t *testing.T) {
	ctrl := NewHoverController()
	ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, widgets.NewPadding(painting.NewEdgeInsetsAll(50), hoverTarget(HoverOpts{Text: "tip", Controller: ctrl})))
	ctrl.Show()
	ts.Pump(time.Millisecond)
	if !shown(ts, "tip") || !ctrl.Visible() {
		t.Fatal("Show did not show the hover")
	}
	ctrl.Hide()
	ts.Pump(time.Millisecond)
	if shown(ts, "tip") || ctrl.Visible() {
		t.Error("Hide did not hide the hover")
	}
	// hiding before the delay is over cancels the wait
	ts.Hover(ui.Offset{Dx: 70, Dy: 60})
	ts.Pump(DefaultHoverDelay / 2)
	ctrl.Hide()
	ts.Pump(DefaultHoverDelay)
	if shown(ts, "tip") {
		t.Error("Hide did not cancel the wait")
	}
	// Ported from VS Code: src/vs/platform/hover/test/browser/hoverService.test.ts ("should cancel a delayed hover that has not been shown yet")
	// A controller of a widget that is gone does nothing.
	ts.PumpWidget(widgets.NewSizedBox(nil))
	ctrl.Show()
	ctrl.Hide()
	if ctrl.Visible() {
		t.Error("a controller of a removed hover shows it")
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestHoverRemovedWhileShown(t *testing.T) {
	delegate := NewHoverDelegate(HoverDelegateOpts{Instant: true})
	ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, widgets.NewPadding(painting.NewEdgeInsetsAll(50), hoverTarget(HoverOpts{Text: "tip", Delegate: delegate})))
	ts.Hover(ui.Offset{Dx: 70, Dy: 60})
	ts.Pump(DefaultHoverDelay + time.Millisecond)
	if !shown(ts, "tip") {
		t.Fatal("not shown")
	}
	ts.PumpWidget(widgets.NewSizedBox(nil))
	ts.Pump(time.Millisecond)
	key(ts, services.LogicalKeyboardKeyEscape)
	ts.Pump(DefaultHoverDelay)
	if delegate.Visible() || shown(ts, "tip") {
		t.Error("the hover outlived its target")
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestHoverPlacementInWindow(t *testing.T) {
	const window = 300.0
	long := "A tooltip with a long text that is wider than the room left at the right edge"
	short := "tip"
	cases := []struct {
		name      string
		text      string
		at        ui.Offset // top left of the 40 by 22 target
		pos       HoverPosition
		wantBelow bool
		wantAbove bool
		wantRight bool
	}{
		{"below in the middle", short, ui.Offset{Dx: 100, Dy: 50}, HoverBelow, true, false, false},
		{"above in the middle", short, ui.Offset{Dx: 100, Dy: 150}, HoverAbove, false, true, false},
		{"near the bottom edge it goes above", short, ui.Offset{Dx: 100, Dy: 270}, HoverBelow, false, true, false},
		{"near the top edge it goes below", short, ui.Offset{Dx: 100, Dy: 2}, HoverAbove, true, false, false},
		{"near the right edge it stays inside", long, ui.Offset{Dx: 255, Dy: 50}, HoverBelow, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := NewHoverController()
			child := widgets.NewStack([]widgets.Widget{
				widgets.NewPositioned(hoverTarget(HoverOpts{Text: tc.text, Position: tc.pos, Controller: ctrl}),
					widgets.PositionedOpts{Left: foundation.Some(tc.at.Dx), Top: foundation.Some(tc.at.Dy)}),
			}, widgets.StackOpts{Fit: 0})
			ts := pumpSized(t, ui.Size{Width: window, Height: window}, widgets.NewSizedBox(child, widgets.SizedBoxOpts{Width: foundation.Some(window), Height: foundation.Some(window)}))
			ctrl.Show()
			ts.Pump(time.Millisecond)
			popup := gluttertest.Ancestor(gluttertest.ByText(tc.text), gluttertest.ByType[*widgets.Container]())
			top, size := ts.TopLeft(gluttertest.First(popup)), ts.Size(gluttertest.First(popup))
			if top.Dx < 0 || top.Dx+size.Width > window || top.Dy < 0 || top.Dy+size.Height > window {
				t.Errorf("the hover at %v of size %v leaves the %v px window", top, size, window)
			}
			switch {
			case tc.wantBelow && top.Dy != tc.at.Dy+22:
				t.Errorf("hover top %v, want the target's bottom %v", top.Dy, tc.at.Dy+22)
			case tc.wantAbove && top.Dy+size.Height != tc.at.Dy:
				t.Errorf("hover bottom %v, want the target's top %v", top.Dy+size.Height, tc.at.Dy)
			}
			if !tc.wantRight && top.Dx != tc.at.Dx {
				t.Errorf("hover left %v, want the target's left %v", top.Dx, tc.at.Dx)
			}
			if tc.wantRight && top.Dx >= tc.at.Dx {
				t.Errorf("hover left %v should have moved left of the target's %v", top.Dx, tc.at.Dx)
			}
		})
	}
}

func TestHoverContent(t *testing.T) {
	ctrl := NewHoverController()
	content := func(bc widgets.BuildContext) widgets.Widget {
		return widgets.NewRow([]widgets.Widget{
			widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconCheck), widgets.IconOpts{}), widgets.NewText("rich", widgets.TextOpts{}),
		})
	}
	ts := pumpSized(t, ui.Size{Width: 300, Height: 200}, widgets.NewPadding(painting.NewEdgeInsetsAll(50), hoverTarget(HoverOpts{Content: content, Controller: ctrl})))
	ctrl.Show()
	ts.Pump(time.Millisecond)
	if !shown(ts, "rich") {
		t.Error("the content is not shown")
	}
}
