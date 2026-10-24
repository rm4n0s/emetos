package kit

import (
	"fmt"
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

// dragLog records the drag callbacks of a strip.
type dragLog struct {
	events  []string
	refuse  bool
	menuAt  []ui.Offset
	menuFor []int
	active  []int
}

func (l *dragLog) opts(o TabsOpts) TabsOpts {
	o.OnActivate = func(i int) { l.active = append(l.active, i) }
	o.OnDragStart = func(i int) bool {
		l.events = append(l.events, fmt.Sprintf("start %d", i))
		return !l.refuse
	}
	o.OnDragMove = func(i int, at ui.Offset) { l.events = append(l.events, fmt.Sprintf("move %d %v,%v", i, at.Dx, at.Dy)) }
	o.OnDragEnd = func(i int, at ui.Offset, dropped bool) {
		l.events = append(l.events, fmt.Sprintf("end %d %v,%v %v", i, at.Dx, at.Dy, dropped))
	}
	o.OnContextMenu = func(i int, at ui.Offset) { l.menuFor, l.menuAt = append(l.menuFor, i), append(l.menuAt, at) }
	return o
}

func TestTabsDragCallbacks(t *testing.T) {
	press := func(ts *gluttertest.Tester, name string) *gluttertest.TestGesture {
		return ts.StartGesture(tabCenter(ts, name), mouseDown)
	}
	for _, tc := range []struct {
		name   string
		refuse bool
		do     func(t *testing.T, ts *gluttertest.Tester, from ui.Offset)
		want   []string
	}{
		{"a press and release is a click", false, func(t *testing.T, ts *gluttertest.Tester, from ui.Offset) {
			g := press(ts, "b.go")
			g.Up()
		}, nil},
		{"a move inside the slop is not a drag", false, func(t *testing.T, ts *gluttertest.Tester, from ui.Offset) {
			g := press(ts, "b.go")
			g.MoveTo(ui.Offset{Dx: from.Dx + 2, Dy: from.Dy + 2})
			g.Up()
		}, nil},
		{"a move beyond the slop starts and moves, the release drops", false, func(t *testing.T, ts *gluttertest.Tester, from ui.Offset) {
			g := press(ts, "b.go")
			g.MoveTo(ui.Offset{Dx: from.Dx + 5, Dy: from.Dy})
			g.MoveTo(ui.Offset{Dx: from.Dx + 40, Dy: from.Dy + 100})
			g.Up()
		}, []string{"start 1", "move 1 %v,%v", "move 1 %v,%v", "end 1 %v,%v true"}},
		{"a refused drag is never told again", true, func(t *testing.T, ts *gluttertest.Tester, from ui.Offset) {
			g := press(ts, "b.go")
			g.MoveTo(ui.Offset{Dx: from.Dx + 10, Dy: from.Dy})
			g.MoveTo(ui.Offset{Dx: from.Dx + 40, Dy: from.Dy})
			g.Up()
		}, []string{"start 1"}},
		{"Escape cancels, and the release after it drops nothing", false, func(t *testing.T, ts *gluttertest.Tester, from ui.Offset) {
			g := press(ts, "b.go")
			g.MoveTo(ui.Offset{Dx: from.Dx + 10, Dy: from.Dy})
			ts.SendKeyEvent(services.LogicalKeyboardKeyEscape)
			g.MoveTo(ui.Offset{Dx: from.Dx + 50, Dy: from.Dy})
			g.Up()
		}, []string{"start 1", "move 1 %v,%v", "end 1 %v,%v false"}},
		{"the system taking the pointer cancels", false, func(t *testing.T, ts *gluttertest.Tester, from ui.Offset) {
			g := press(ts, "b.go")
			g.MoveTo(ui.Offset{Dx: from.Dx + 10, Dy: from.Dy})
			g.Cancel()
		}, []string{"start 1", "move 1 %v,%v", "end 1 %v,%v false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &dragLog{refuse: tc.refuse}
			ts := pumpTabs(t, DarkModern(), 600, names("a.go", "b.go", "c.go"), log.opts(TabsOpts{Active: 0}))
			tc.do(t, ts, tabCenter(ts, "b.go"))
			ts.Pump(time.Millisecond)
			// the positions are those of the pointer; compare the shape of the events
			got := shapeOf(log.events)
			if !slices.Equal(got, shapeOf(tc.want)) {
				t.Errorf("events %v, want shape %v", log.events, tc.want)
			}
			if len(log.active) != 1 || log.active[0] != 1 {
				t.Errorf("the press activates the tab: %v", log.active)
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

// shapeOf drops the positions from the events.
func shapeOf(events []string) []string {
	out := make([]string, len(events))
	for i, e := range events {
		var verb string
		var idx int
		_, _ = fmt.Sscanf(e, "%s %d", &verb, &idx)
		out[i] = fmt.Sprintf("%s %d", verb, idx)
		if verb == "end" {
			out[i] += " " + e[len(e)-5:]
		}
	}
	return out
}

func TestTabsDragReportsWindowPositions(t *testing.T) {
	log := &dragLog{}
	ts := pumpTabs(t, DarkModern(), 600, names("a.go", "b.go", "c.go"), log.opts(TabsOpts{Active: 0}))
	from := tabCenter(ts, "b.go")
	g := ts.StartGesture(from, mouseDown)
	g.MoveTo(ui.Offset{Dx: from.Dx + 6, Dy: from.Dy})
	to := ui.Offset{Dx: 300, Dy: 200}
	g.MoveTo(to)
	g.Up()
	want := []string{
		"start 1",
		fmt.Sprintf("move 1 %v,%v", from.Dx+6, from.Dy),
		fmt.Sprintf("move 1 %v,%v", to.Dx, to.Dy),
		fmt.Sprintf("end 1 %v,%v true", to.Dx, to.Dy),
	}
	if !slices.Equal(log.events, want) {
		t.Errorf("events %v, want %v", log.events, want)
	}
}

func TestTabsDragIsFromThePrimaryButtonOnly(t *testing.T) {
	log := &dragLog{}
	ts := pumpTabs(t, DarkModern(), 600, names("a.go", "b.go"), log.opts(TabsOpts{Active: 0}))
	from := tabCenter(ts, "b.go")
	secondary := gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)}
	g := ts.StartGesture(from, secondary)
	g.MoveTo(ui.Offset{Dx: from.Dx + 30, Dy: from.Dy})
	g.Up()
	if len(log.events) != 0 {
		t.Errorf("a drag with the secondary button: %v", log.events)
	}
	if !slices.Equal(log.menuFor, []int{1}) || len(log.menuAt) != 1 || log.menuAt[0] != from {
		t.Errorf("the context menu was asked for tab %v at %v", log.menuFor, log.menuAt)
	}
	if len(log.active) != 0 {
		t.Errorf("a right click does not activate the tab: %v", log.active)
	}
	tertiary := gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.TertiaryButton)}
	g = ts.StartGesture(from, tertiary)
	g.MoveTo(ui.Offset{Dx: from.Dx + 30, Dy: from.Dy})
	g.Up()
	if len(log.events) != 0 {
		t.Errorf("a drag with the middle button: %v", log.events)
	}
}

func TestTabsDragFeedbackDrawsTheLine(t *testing.T) {
	for _, th := range goldenThemes {
		colors := th.colors()
		line := rgb(colors.TabDragAndDropBorder)
		for _, tc := range []struct {
			name  string
			index int
			// the pixels (x) that have the line and those that do not
			on, off []int
		}{
			{"before the first tab", 0, []int{0, 1}, []int{2, 60, 118}},
			{"between the first and the second", 1, []int{120, 121}, []int{0, 1, 118, 122}},
			{"between the second and the third", 2, []int{240, 241}, []int{120, 238, 242}},
			{"after the last tab", 3, []int{358, 359}, []int{0, 240, 356}},
			{"nowhere", -1, nil, []int{0, 120, 240, 359}},
		} {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				fb := NewTabDropFeedback()
				ts := pumpTabs(t, colors, 600, names("a.go", "b.go", "c.go"), TabsOpts{Active: 2, Feedback: fb})
				fb.Set(tc.index)
				ts.Pump(time.Millisecond)
				ts.Pump(time.Millisecond)
				for _, x := range tc.on {
					for _, y := range []int{2, 17, 30} {
						if got := pixel(ts, x, y); got != line {
							t.Errorf("(%d,%d) is %v, want the line %v", x, y, got, line)
						}
					}
				}
				for _, x := range tc.off {
					if got := pixel(ts, x, 17); got == line {
						t.Errorf("(%d,17) has the line", x)
					}
				}
			})
		}
	}
}

func TestTabDropFeedback(t *testing.T) {
	fb := NewTabDropFeedback()
	if fb.Index() != -1 {
		t.Errorf("a new feedback shows nothing: %d", fb.Index())
	}
	calls := 0
	fb.AddListener(func() { calls++ })
	fb.Set(2)
	fb.Set(2)
	fb.Set(-7)
	fb.Set(-1)
	if fb.Index() != -1 || calls != 2 {
		t.Errorf("index %d after %d notifications, want -1 after 2", fb.Index(), calls)
	}
}

func TestTabsController(t *testing.T) {
	c := NewTabsController()
	items := namesItems([]string{"one.go", "two.go", "three.go", "four.go", "five.go", "six.go", "seven.go", "eight.go"})
	ts := pumpTabs(t, DarkModern(), 400, items, TabsOpts{Active: 0, Controller: c})
	strip, ok := c.Bounds()
	if !ok || strip.Left != 0 || strip.Width() != 400 || strip.Height() != TabHeight {
		t.Fatalf("the strip is at %v (%v)", strip, ok)
	}
	for i := range items {
		r, ok := c.TabRect(i)
		if !ok || r.Width() < tabMinWidth || r.Height() != TabHeight {
			t.Errorf("tab %d is at %v (%v)", i, r, ok)
		}
	}
	if _, ok := c.TabRect(len(items)); ok {
		t.Error("a tab that does not exist has a place")
	}
	first, _ := c.TabRect(0)
	last, _ := c.TabRect(7)
	if first.Left != 0 || last.Left < strip.Right {
		t.Fatalf("first %v, last %v: the last tab is out of view", first, last)
	}

	// scrolling moves the tabs under the strip
	c.ScrollBy(130)
	ts.Pump(time.Millisecond)
	moved, _ := c.TabRect(0)
	if moved.Left != -130 || c.Scrolled() != 130 {
		t.Errorf("scrolled %v, the first tab is at %v", c.Scrolled(), moved.Left)
	}
	c.ScrollBy(-1000)
	ts.Pump(time.Millisecond)
	if c.Scrolled() != 0 {
		t.Errorf("scrolling stops at the start: %v", c.Scrolled())
	}
	c.ScrollBy(100000)
	ts.Pump(time.Millisecond)
	last, _ = c.TabRect(7)
	if last.Right != strip.Right {
		t.Errorf("scrolling stops at the end: the last tab ends at %v, the strip at %v", last.Right, strip.Right)
	}

	// revealing a tab scrolls just far enough
	c.ScrollBy(-100000)
	ts.Pump(time.Millisecond)
	c.Reveal(3)
	ts.Pump(time.Millisecond)
	r, _ := c.TabRect(3)
	if r.Right != strip.Right {
		t.Errorf("the fourth tab was revealed at its right edge: %v", r)
	}
	c.Reveal(0)
	ts.Pump(time.Millisecond)
	r, _ = c.TabRect(0)
	if r.Left != 0 {
		t.Errorf("the first tab was revealed at the start: %v", r)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestRevealDelta(t *testing.T) {
	strip := ui.NewRectFromLTWH(100, 0, 300, 35)
	for _, tc := range []struct {
		name string
		tab  ui.Rect
		want float64
	}{
		{"in view", ui.NewRectFromLTWH(150, 0, 120, 35), 0},
		{"exactly the strip", ui.NewRectFromLTWH(100, 0, 300, 35), 0},
		{"cut at the left", ui.NewRectFromLTWH(60, 0, 120, 35), -40},
		{"out of view at the left", ui.NewRectFromLTWH(-100, 0, 120, 35), -200},
		{"cut at the right", ui.NewRectFromLTWH(350, 0, 120, 35), 70},
		{"out of view at the right", ui.NewRectFromLTWH(600, 0, 120, 35), 320},
		{"wider than the strip shows its start", ui.NewRectFromLTWH(150, 0, 500, 35), 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RevealDelta(tc.tab, strip); got != tc.want {
				t.Errorf("RevealDelta = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTabEdgeStep(t *testing.T) {
	for _, tc := range []struct {
		name string
		x    float64
		sign int
		max  bool
	}{
		{"the middle", 250, 0, false},
		{"just inside the left edge zone", 100 + 34, -1, false},
		{"at the left edge", 100, -1, false},
		{"far past the left edge", -300, -1, true},
		{"inside the right edge zone", 400 - 30, 1, false},
		{"past the right edge", 900, 1, true},
		{"outside the zone", 100 + 36, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TabEdgeStep(tc.x, 100, 400)
			switch {
			case tc.sign == 0 && got != 0:
				t.Errorf("step %v, want none", got)
			case tc.sign < 0 && got >= 0, tc.sign > 0 && got <= 0:
				t.Errorf("step %v, want sign %d", got, tc.sign)
			case tc.max && (got != dragMaxStep && got != -dragMaxStep):
				t.Errorf("step %v, want the greatest", got)
			}
		})
	}
}

func TestBoundsKnowWhereAWidgetIsAndWhenItIsGone(t *testing.T) {
	var b Bounds
	if _, ok := b.Rect(); ok {
		t.Error("a Bounds that no probe has has a place")
	}
	probe := NewBoundsProbe(NewTabs(names("a.go"), TabsOpts{Active: 0}), &b)
	ts := pumpThemed(t, DarkModern(), ui.Size{Width: 300, Height: 100}, probe)
	ts.Pump(time.Millisecond)
	r, ok := b.Rect()
	if !ok || r.Left != 0 || r.Top != 0 || r.Height() != TabHeight {
		t.Fatalf("the place is %v (%v)", r, ok)
	}
	// the widget goes away: the place goes with it
	ts.PumpWidget(widgets.NewSizedBox(nil))
	ts.Pump(time.Millisecond)
	if r, ok := b.Rect(); ok {
		t.Errorf("a widget that is gone is at %v", r)
	}
}
