package workbench

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// TestViewsBadges checks the model of the badges on the icons: a count is kept with its tooltip and announced once, the same
// badge again changes nothing, and 0 takes it away.
func TestViewsBadges(t *testing.T) {
	v := NewViews(testViewRegistry(), NewLayout(DefaultLayoutState()))
	changes := 0
	v.OnChange(func() { changes++ })
	if _, ok := v.Badge("scm"); ok {
		t.Fatal("a container starts with no badge")
	}
	v.SetBadge("scm", ActivityBadge{Count: 3, Tooltip: "3 pending changes"})
	if b, ok := v.Badge("scm"); !ok || b.Count != 3 || b.Tooltip != "3 pending changes" || changes != 1 {
		t.Errorf("badge %+v %v after %d changes", b, ok, changes)
	}
	v.SetBadge("scm", ActivityBadge{Count: 3, Tooltip: "3 pending changes"})
	if changes != 1 {
		t.Errorf("the same badge again announced a change: %d", changes)
	}
	v.SetBadge("scm", ActivityBadge{Count: 0})
	if _, ok := v.Badge("scm"); ok || changes != 2 {
		t.Errorf("a count of 0 takes the badge away: %v, %d changes", ok, changes)
	}
	v.SetBadge("scm", ActivityBadge{})
	if changes != 2 {
		t.Errorf("taking away a badge that is not there announced a change: %d", changes)
	}
}

// TestActivityBarBadge checks that the icon of a container with a badge shows its count (99+ above 99), in the colors of the
// activity bar badge, and that its tooltip adds the badge's.
func TestActivityBarBadge(t *testing.T) {
	h, v := viewsHarness(t, nil)
	if h.ts.Exists(gluttertest.ByText("7")) {
		t.Fatal("a badge shows before it is set")
	}
	v.SetBadge("scm", ActivityBadge{Count: 7, Tooltip: "7 pending changes"})
	h.settle()
	if !h.ts.Exists(gluttertest.ByText("7")) {
		t.Fatal("the badge does not show")
	}
	v.SetBadge("scm", ActivityBadge{Count: 120})
	h.settle()
	if !h.ts.Exists(gluttertest.ByText("99+")) {
		t.Error("a count above 99 is written 99+")
	}
	v.SetBadge("scm", ActivityBadge{Count: 7, Tooltip: "7 pending changes"})
	h.settle()
	h.ts.Hover(h.ts.Center(gluttertest.ByKey(ActivityItemKey("scm"))))
	h.ts.Pump(2 * time.Second)
	if !h.ts.Exists(gluttertest.ByText("Source Control - 7 pending changes")) {
		t.Error("the tooltip does not name the badge")
	}
	v.SetBadge("scm", ActivityBadge{})
	h.settle()
	if h.ts.Exists(gluttertest.ByText("7")) {
		t.Error("the badge stays after it was taken away")
	}
	noErrors(t, h)
}

// TestGoldenActivityBadge looks at the activity bar with the badge of the Source Control icon, dark and light.
func TestGoldenActivityBadge(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			var views *Views
			m := &menuServices{}
			h := newHarness(t, ui.Size{Width: 420, Height: 300}, th.colors(), DefaultLayoutState(), nil, viewsDemo(m, &views, kit.NewMenuBarController()))
			views.SetBadge("scm", ActivityBadge{Count: 12, Tooltip: "12 pending changes"})
			views.SetBadge("explorer", ActivityBadge{Count: 120})
			h.settle()
			testutil.MatchesGolden(h.ts, "activity_badge_"+th.name)
			noErrors(t, h)
		})
	}
}
