package kit

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The strip of the workspace tabs of the title bar (TabsOpts.TitleBar, SPEC §10.14).

func workspaceTabItems(n int) []TabItem {
	out := make([]TabItem, n)
	for i := range out {
		name := fmt.Sprintf("workspace-%02d", i)
		out[i] = TabItem{Key: name, Name: name, Title: "/w/" + name}
	}
	return out
}

func pumpTitleBar(t *testing.T, colors Colors, width float64, items []TabItem, opts TabsOpts) *gluttertest.Tester {
	t.Helper()
	opts.TitleBar = &TabsTitleBar{}
	return pumpTabs(t, colors, width, items, opts)
}

func TestTitleBarTabsAreBetween120And240Wide(t *testing.T) {
	items := []TabItem{
		{Key: "a", Name: "a"},
		{Key: "b", Name: "an-in-between-name"},
		{Key: "c", Name: "a-very-very-long-workspace-name-that-does-not-fit-in-240-pixels-at-all"},
	}
	ts := pumpTitleBar(t, DarkModern(), 900, items, TabsOpts{Active: 0})
	for i, want := range []float64{120, 0, 240} {
		got := tabWidthAt(ts, i)
		if want != 0 && got != want {
			t.Errorf("tab %d is %v wide, want %v", i, got, want)
		}
		if want == 0 && (got <= 120 || got >= 240) {
			t.Errorf("tab %d is %v wide, want between 120 and 240", i, got)
		}
	}
	if size := ts.Size(gluttertest.ByType[*Tabs]()); size.Height != TabHeight {
		t.Errorf("the strip is %v high, want %v", size.Height, TabHeight)
	}
}

func TestTitleBarTabsShrinkThenScroll(t *testing.T) {
	// ten tabs of 120 px need 1200 px; with 700 the strip scrolls
	ts := pumpTitleBar(t, DarkModern(), 700, workspaceTabItems(10), TabsOpts{Active: 0})
	for i := range 10 {
		if got := tabWidthAt(ts, i); got != 120 {
			t.Errorf("tab %d is %v wide, want 120", i, got)
		}
	}
	// the wheel scrolls it
	before := ts.TopLeft(gluttertest.ByText("workspace-09")).Dx
	ts.ScrollKindAt(ui.PointerDeviceKindMouse, ui.Offset{Dx: 300, Dy: 17}, ui.Offset{Dx: 0, Dy: 200})
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if after := ts.TopLeft(gluttertest.ByText("workspace-09")).Dx; after >= before {
		t.Errorf("the wheel did not scroll: the last tab was at %v and is at %v", before, after)
	}

	// three tabs with long names shrink together to fit 480 px, all above 120
	items := []TabItem{
		{Key: "a", Name: "a-long-workspace-name-number-one-of-three"},
		{Key: "b", Name: "a-long-workspace-name-number-two-of-three"},
		{Key: "c", Name: "a-long-workspace-name-number-three-of-three"},
	}
	ts = pumpTitleBar(t, DarkModern(), 480, items, TabsOpts{Active: 0})
	sum := 0.0
	for i := range 3 {
		w := tabWidthAt(ts, i)
		if w < 120 || w > 240 {
			t.Errorf("tab %d is %v wide", i, w)
		}
		sum += w
	}
	if sum > 480.5 || sum < 479 {
		t.Errorf("the tabs add up to %v, want the 480 they have to fit in", sum)
	}
}

func TestTitleBarTabsKeepTheActiveTabInView(t *testing.T) {
	ts := pumpTitleBar(t, DarkModern(), 600, workspaceTabItems(12), TabsOpts{Active: 11})
	for range 3 {
		ts.Pump(time.Millisecond)
	}
	if x := ts.TopLeft(gluttertest.ByText("workspace-11")).Dx; x < 0 || x > 600 {
		t.Errorf("the active tab is at %v, out of the 600 px of the strip", x)
	}
}

func TestTitleBarTabsInactiveOnTheBackgroundOfTheRow(t *testing.T) {
	colors := DarkModern()
	ts := pumpTitleBar(t, colors, 600, names("one", "two"), TabsOpts{Active: 0})
	// the active tab has the color of the active tab, the other none of its own: it shows the page behind it
	active := pixel(ts, 100, 20)
	if want := rgb(colors.TabActiveBackground); active != want {
		t.Errorf("the active tab is %v, want %v", active, want)
	}
	inactive := pixel(ts, 220, 20)
	if want := rgb(colors.EditorBackground); inactive != want {
		t.Errorf("the inactive tab is %v, want the page behind it, %v", inactive, want)
	}
	// the line along the top of the active tab is the active border, or the focus border when the theme has none
	line := pixel(ts, 100, 0)
	want := colors.TabActiveBorderTop
	if want.Alpha() == 0 {
		want = colors.FocusBorder
	}
	if line != rgb(want) {
		t.Errorf("the top line is %v, want %v", line, rgb(want))
	}
}

func TestTitleBarTabsDotAndCloseButton(t *testing.T) {
	items := []TabItem{{Key: "a", Name: "one", Dirty: true}, {Key: "b", Name: "two"}, {Key: "c", Name: "three", Dirty: true}}
	r := &tabsRecorder{}
	ts := pumpTitleBar(t, DarkModern(), 800, items, r.opts(TabsOpts{Active: 1}))
	dots := func() int { return len(ts.Widgets(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCircleFilled)))) }
	if dots() != 2 {
		t.Fatalf("%d dots, want 2", dots())
	}
	// the pointer over a dirty tab (not over its button) turns its dot into the close button
	ts.Hover(tabCenter(ts, "one"))
	ts.Pump(time.Millisecond)
	if dots() != 1 {
		t.Errorf("%d dots with the pointer over the first tab, want 1", dots())
	}
	// and the close button closes it
	closeAt := ts.Center(gluttertest.At(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconClose)), 0))
	ts.TapAt(closeAt, mouse)
	ts.Pump(time.Millisecond)
	if !slices.Equal(r.closed, []int{0}) {
		t.Errorf("closed %v", r.closed)
	}
}

func TestTitleBarTabsWithoutTabsShowNothing(t *testing.T) {
	ts := pumpTitleBar(t, DarkModern(), 400, nil, TabsOpts{Active: -1})
	if ts.Exists(gluttertest.ByType[*tabWidget]()) {
		t.Error("a tab shows in a strip with no tabs")
	}
}
