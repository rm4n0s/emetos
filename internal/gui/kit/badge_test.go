package kit

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

func TestBadgeText(t *testing.T) {
	cases := []struct {
		name  string
		count int
		opts  BadgeOpts
		want  string
	}{
		{"a number", 5, BadgeOpts{}, "5"},
		{"zero", 0, BadgeOpts{}, "0"},
		{"a big number", 12345, BadgeOpts{}, "12345"},
		{"a format", 3, BadgeOpts{Format: "{0}+"}, "3+"},
		{"a format with the count twice", 3, BadgeOpts{Format: "{0}/{0}"}, "3/3"},
	}
	for _, tc := range cases {
		b := NewBadge(tc.count, tc.opts)
		if b.Text() != tc.want || b.Count() != tc.count {
			t.Errorf("%s: %q, count %d", tc.name, b.Text(), b.Count())
		}
	}
}

func TestBadgeLook(t *testing.T) {
	for _, theme := range []struct {
		name   string
		colors Colors
	}{{"Dark Modern", DarkModern()}, {"Light Modern", LightModern()}} {
		cases := []struct {
			name          string
			count         int
			width, height float64
		}{
			{"one digit is at least 18 by 18", 3, 18, 18},
			{"two digits", 42, -1, 18},
			{"four digits are wider", 1234, -1, 18},
		}
		for _, tc := range cases {
			t.Run(theme.name+" "+tc.name, func(t *testing.T) {
				ts := pumpThemed(t, theme.colors, ui.Size{Width: 120, Height: 80}, padded(NewBadge(tc.count)))
				root := gluttertest.ByType[*Badge]()
				size := ts.Size(root)
				if tc.width > 0 && size.Width != tc.width {
					t.Errorf("width %v, want %v", size.Width, tc.width)
				}
				if size.Width < badgeMinWidth || size.Height != tc.height {
					t.Errorf("size %v", size)
				}
				box := ts.Widget(gluttertest.Descendant(root, gluttertest.ByType[*widgets.DecoratedBox]())).(*widgets.DecoratedBox)
				deco := box.Decoration().(*painting.BoxDecoration)
				if deco.Color.OrElse(ui.Color{}) != theme.colors.BadgeBackground {
					t.Errorf("background %v", deco.Color)
				}
				text := ts.Widget(gluttertest.Descendant(root, gluttertest.ByType[*widgets.Text]())).(*widgets.Text)
				if text.Style().Color.OrElse(ui.Color{}) != theme.colors.BadgeForeground {
					t.Errorf("text color %v", text.Style().Color)
				}
			})
		}
	}
}

func TestBadgeTooltip(t *testing.T) {
	ts := pumpThemed(t, DarkModern(), ui.Size{Width: 220, Height: 100}, padded(NewBadge(7, BadgeOpts{Tooltip: "{0} problems"})))
	ts.Hover(ts.Center(gluttertest.ByType[*Badge]()))
	ts.Pump(DefaultHoverDelay + time.Millisecond)
	if !ts.Exists(gluttertest.ByText("7 problems")) {
		t.Error("no tooltip")
	}
	// no tooltip without the option
	ts2 := pumpThemed(t, DarkModern(), ui.Size{Width: 220, Height: 100}, padded(NewBadge(7)))
	ts2.Hover(ts2.Center(gluttertest.ByType[*Badge]()))
	ts2.Pump(DefaultHoverDelay + time.Millisecond)
	if ts2.Exists(gluttertest.ByType[*Hover]()) {
		t.Error("a hover without a tooltip")
	}
}
