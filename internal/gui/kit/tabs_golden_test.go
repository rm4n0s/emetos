package kit

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// goldenTabs is a strip of every kind of tab: active, inactive, preview (italic), pinned, modified,
// two with the same name and a long one.
func goldenTabs() []TabItem {
	return []TabItem{
		{Key: "1", Name: "main.go", Title: "/w/main.go"},
		{Key: "2", Name: "util.go", Title: "/w/util.go"},
		{Key: "3", Name: "README.md", Title: "/w/README.md", Preview: true},
		{Key: "4", Name: "go.mod", Title: "/w/go.mod", Sticky: true},
		{Key: "5", Name: "app.go", Title: "/w/app.go", Dirty: true},
		{Key: "6", Name: "index.ts", Description: "src/a", Title: "/w/src/a/index.ts"},
		{Key: "7", Name: "index.ts", Description: "src/b", Title: "/w/src/b/index.ts"},
	}
}

func strip(width float64, items []TabItem, opts TabsOpts) widgets.Widget {
	return widgets.NewSizedBox(NewTabs(items, opts), widgets.SizedBoxOpts{Width: foundation.Some(width)})
}

func TestGoldenTabs(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name+" kinds", func(t *testing.T) {
			// go.mod is pinned, so it sits first
			items := goldenTabs()
			items[0], items[3] = items[3], items[0]
			items[0].Icon = widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconFileCode), widgets.IconOpts{Size: foundation.Some(16.0)})
			items = append(items, TabItem{Key: "8", Name: "a-rather-long-name.test.ts", Title: "/w/a-rather-long-name.test.ts"})
			ts := pumpTabs(t, th.colors(), 1000, items, TabsOpts{Active: 1})
			ts.MatchesGolden("tabs_" + th.name)
		})
		t.Run(th.name+" states", func(t *testing.T) {
			c := th.colors()
			content := padded(themed(func(bc widgets.BuildContext) widgets.Widget {
				return column(
					labelled(bc, "dirty tab under the pointer", strip(660, goldenTabs()[:5], TabsOpts{Active: 0})), vgap(8),
					labelled(bc, "group that is not the active one", strip(660, goldenTabs()[:5], TabsOpts{Active: 0, Unfocused: true})), vgap(8),
					labelled(bc, "modified tabs highlighted, close button at the left", strip(660, goldenTabs()[:5], TabsOpts{Active: 0, HighlightModified: true, CloseButton: TabCloseLeft})), vgap(8),
					labelled(bc, "pinned tabs compact and shrunk, no close buttons", column(
						strip(560, []TabItem{{Key: "a", Name: "main.go", Sticky: true}, {Key: "b", Name: "util.go", Sticky: true, Dirty: true}, {Key: "c", Name: "app.go"}}, TabsOpts{Active: 2, PinnedSizing: PinnedCompact, CloseButton: TabCloseOff}),
						vgap(4),
						strip(560, []TabItem{{Key: "a", Name: "main.go", Sticky: true}, {Key: "b", Name: "util.go", Sticky: true, Dirty: true}, {Key: "c", Name: "app.go"}}, TabsOpts{Active: 2, PinnedSizing: PinnedShrink}),
					)),
				)
			}))
			ts := pumpThemed(t, c, ui.Size{Width: 800, Height: 360}, content)
			ts.Pump(time.Millisecond)
			// the pointer rests on the dot of the modified tab of the first strip
			ts.Hover(ts.Center(gluttertest.At(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCircleFilled)), 0)))
			ts.Pump(time.Millisecond)
			ts.MatchesGolden("tabs_states_" + th.name)
		})
		t.Run(th.name+" overflow", func(t *testing.T) {
			names := []string{"one.go", "two.go", "three.go", "four.go", "five.go", "six.go", "seven.go", "eight.go"}
			ts := pumpTabs(t, th.colors(), 400, namesItems(names), TabsOpts{Active: 7})
			ts.Pump(time.Millisecond)
			ts.Pump(time.Millisecond)
			// the pointer over the strip shows its scrollbar
			ts.Hover(ui.Offset{Dx: 200, Dy: 17})
			ts.Pump(scrollbarRevealDuration + 20*time.Millisecond)
			ts.MatchesGolden("tabs_overflow_" + th.name)
		})
		t.Run(th.name+" single", func(t *testing.T) {
			items := goldenTabs()
			items[4].Description = "src"
			ts := pumpTabs(t, th.colors(), 500, items, TabsOpts{Active: 4, Mode: TabsSingle})
			ts.MatchesGolden("tabs_single_" + th.name)
		})
	}
}

func namesItems(ns []string) []TabItem { return names(ns...) }

// TestGoldenTabDrop looks at the line that shows where a dragged tab goes: between two tabs, and
// after the last one.
func TestGoldenTabDrop(t *testing.T) {
	for _, th := range goldenThemes {
		for _, tc := range []struct {
			name  string
			index int
		}{{"between", 2}, {"after the last", 5}} {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				items := goldenTabs()[:4]
				items = append(items, TabItem{Key: "9", Name: "dragged.go", Title: "/w/dragged.go"})
				fb := NewTabDropFeedback()
				ts := pumpTabs(t, th.colors(), 640, items, TabsOpts{Active: 1, Feedback: fb})
				fb.Set(tc.index)
				ts.Pump(time.Millisecond)
				ts.Pump(time.Millisecond)
				name := "tabs_drop_between_"
				if tc.index == 5 {
					name = "tabs_drop_after_"
				}
				ts.MatchesGolden(name + th.name)
			})
		}
	}
}
