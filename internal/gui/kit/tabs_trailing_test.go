package kit

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The trailing widget of the strip (TabsOpts.Trailing, step 29.1): the "+" button of an editor group.

func plusButton(pressed *int) widgets.Widget {
	return NewIconButton(fonts.CodiconAdd, IconButtonOpts{Tooltip: "New...", OnPressed: func() { *pressed++ }})
}

func plusOf(ts *gluttertest.Tester) ui.Rect {
	at, size := ts.TopLeft(gluttertest.ByType[*IconButton]()), ts.Size(gluttertest.ByType[*IconButton]())
	return ui.Rect{Left: at.Dx, Top: at.Dy, Right: at.Dx + size.Width, Bottom: at.Dy + size.Height}
}

func TestTheTrailingWidgetFollowsTheLastTabAndStaysWhenTheTabsOverflow(t *testing.T) {
	many := make([]string, 30)
	for i := range many {
		many[i] = "file" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".go"
	}
	tests := []struct {
		name      string
		mode      TabsMode
		items     []TabItem
		overflows bool
	}{
		{"a few tabs", TabsMultiple, names("one.go", "two.go"), false},
		{"thirty tabs", TabsMultiple, names(many...), true},
		{"no tab", TabsMultiple, nil, false},
		{"single", TabsSingle, names("one.go", "two.go"), false},
		{"single, no tab", TabsSingle, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pressed := 0
			active := 0
			if tt.items == nil {
				active = -1
			}
			ts := pumpTabs(t, goldenThemes[0].colors(), 500, tt.items, TabsOpts{Active: active, Mode: tt.mode, Trailing: plusButton(&pressed)})
			r := plusOf(ts)
			if r.Left < 0 || r.Right > 500.5 || r.Bottom <= r.Top {
				t.Fatalf("the button is at %v in a strip 500 wide", r)
			}
			if tt.overflows && 500-r.Right > 8 {
				t.Errorf("the button %v is not at the right end of the strip", r)
			}
			if !tt.overflows && tt.mode == TabsMultiple && len(tt.items) > 0 && r.Left > 400 {
				t.Errorf("the button %v is not after the last tab", r)
			}
			ts.TapAt(ts.Center(gluttertest.ByType[*IconButton]()))
			ts.Pump(time.Millisecond)
			if pressed != 1 {
				t.Errorf("the button was pressed %d times", pressed)
			}
		})
	}
}

func TestTheTrailingWidgetIsLeftOfTheActions(t *testing.T) {
	pressed := 0
	actions := func(float64) widgets.Widget {
		return widgets.NewSizedBox(widgets.NewColoredBox(ui.Color{}, nil), widgets.SizedBoxOpts{Width: foundation.Some(60.0), Height: foundation.Some(20.0)})
	}
	var many []string
	for i := range 30 {
		many = append(many, "file"+string(rune('a'+i%26))+string(rune('a'+i/26))+".go")
	}
	ts := pumpTabs(t, goldenThemes[0].colors(), 500, names(many...), TabsOpts{Active: 0, Trailing: plusButton(&pressed), Actions: actions})
	if r := plusOf(ts); r.Right > 440.5 {
		t.Errorf("the button %v overlaps the actions at the right (60 px)", r)
	}
}

func TestGoldenTabsWithTheTrailingButton(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			pressed := 0
			items := namesItems([]string{"main.go", "util.go", "README.md"})
			items[2].Preview = true
			many := namesItems([]string{"one.go", "two.go", "three.go", "four.go", "five.go", "six.go", "seven.go", "eight.go"})
			c := th.colors()
			content := padded(themed(func(bc widgets.BuildContext) widgets.Widget {
				return column(
					labelled(bc, "after the last tab", strip(560, items, TabsOpts{Active: 0, Trailing: plusButton(&pressed)})), vgap(8),
					labelled(bc, "in a group that is not the active one", strip(560, items, TabsOpts{Active: 0, Unfocused: true, Trailing: plusButton(&pressed)})), vgap(8),
					labelled(bc, "tabs that overflow", strip(360, many, TabsOpts{Active: 0, Trailing: plusButton(&pressed)})), vgap(8),
					labelled(bc, "no tab", strip(560, nil, TabsOpts{Active: -1, Trailing: plusButton(&pressed)})), vgap(8),
					labelled(bc, "single", strip(560, items, TabsOpts{Active: 0, Mode: TabsSingle, Trailing: plusButton(&pressed)})),
				)
			}))
			ts := pumpThemed(t, c, ui.Size{Width: 700, Height: 360}, content)
			ts.Pump(time.Millisecond)
			ts.Pump(time.Millisecond)
			testutil.MatchesGolden(ts, "tabs_trailing_"+th.name)
		})
	}
}
