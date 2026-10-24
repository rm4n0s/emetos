package kit

import (
	"fmt"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// label is a text in the foreground color of the theme, in the middle of the space it has.
func label(text string, background func(Colors) ui.Color, align painting.Alignment) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		colors := ThemeOf(bc)
		return widgets.NewColoredBox(background(colors), widgets.NewAlign(
			widgets.NewPadding(painting.NewEdgeInsetsAll(6), widgets.NewText(text, widgets.TextOpts{Style: TextStyleOf(bc, 12, colors.Foreground)})),
			widgets.AlignOpts{Alignment: align}))
	})
}

func TestGoldenSplitViewHorizontal(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			colors := th.colors()
			panes := []SplitPane{
				{Child: label("Explorer", func(c Colors) ui.Color { return c.EditorWidgetBackground }, painting.AlignmentTopLeft), Size: 90, MinSize: 50},
				{Child: label("Editor", func(c Colors) ui.Color { return c.EditorBackground }, painting.AlignmentTopLeft), Size: 180, MinSize: 80, Priority: LayoutHigh},
				{Child: label("Outline", func(c Colors) ui.Color { return c.EditorWidgetBackground }, painting.AlignmentTopLeft), Size: 90, MinSize: 50, Priority: LayoutLow},
			}
			split := NewSplitView(panes, SplitViewOpts{Orientation: SplitHorizontal, SeparatorBorder: colors.WidgetBorder})
			ts := pumpThemed(t, colors, ui.Size{Width: 360, Height: 120}, widgets.NewSizedBox(split, widgets.SizedBoxOpts{Width: foundation.Some(360.0), Height: foundation.Some(120.0)}))
			// the pointer rests on the second sash
			ts.Hover(ts.Center(gluttertest.At(gluttertest.ByType[*Sash](), 1)))
			ts.Pump(DefaultSashHoverDelay + 20*time.Millisecond)
			testutil.MatchesGolden(ts, "splitview_horizontal_"+th.name)
		})
	}
}

func TestGoldenSplitViewVertical(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			colors := th.colors()
			panes := []SplitPane{
				{Child: label("Editor", func(c Colors) ui.Color { return c.EditorBackground }, painting.AlignmentTopLeft), Size: 120, MinSize: 40, Priority: LayoutHigh},
				{Child: label("Problems", func(c Colors) ui.Color { return c.EditorWidgetBackground }, painting.AlignmentTopLeft), Size: 80, MinSize: 40},
			}
			split := NewSplitView(panes, SplitViewOpts{Orientation: SplitVertical, SeparatorBorder: colors.WidgetBorder})
			ts := pumpThemed(t, colors, ui.Size{Width: 220, Height: 200}, widgets.NewSizedBox(split, widgets.SizedBoxOpts{Width: foundation.Some(220.0), Height: foundation.Some(200.0)}))
			// the sash is dragged 20 up and held
			at := ts.Center(gluttertest.ByType[*Sash]())
			g := ts.StartGesture(at, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
			g.MoveBy(ui.Offset{Dy: -20})
			ts.Pump(time.Millisecond)
			testutil.MatchesGolden(ts, "splitview_vertical_"+th.name)
			g.Up()
		})
	}
}

// rowsChild is a tall, wide child of numbered stripes, to scroll.
func rowsChild() widgets.Widget {
	rows := make([]widgets.Widget, 30)
	for i := range rows {
		bg := func(c Colors) ui.Color {
			if i%2 == 0 {
				return c.EditorBackground
			}
			return c.EditorLineHighlightBorder
		}
		rows[i] = widgets.NewSizedBox(label(fmt.Sprintf("Line %02d  the quick brown fox jumps over the lazy dog and keeps running", i+1), bg, painting.AlignmentCenterLeft),
			widgets.SizedBoxOpts{Height: foundation.Some(22.0)})
	}
	return widgets.NewSizedBox(widgets.NewColumn(rows), widgets.SizedBoxOpts{Width: foundation.Some(700.0)})
}

func TestGoldenScrollableElement(t *testing.T) {
	for _, th := range goldenThemes {
		for _, state := range []string{"hover", "active"} {
			t.Run(th.name+" "+state, func(t *testing.T) {
				colors := th.colors()
				element := NewScrollableElement(rowsChild(), ScrollableElementOpts{Axes: ScrollAxisBoth, UseShadows: true, NoSmoothScroll: true})
				ts := pumpThemed(t, colors, ui.Size{Width: 300, Height: 180}, widgets.NewSizedBox(element, widgets.SizedBoxOpts{Width: foundation.Some(300.0), Height: foundation.Some(180.0)}))
				ts.Pump(time.Millisecond)
				ts.Pump(time.Millisecond)
				// scrolled down and sideways by the wheel
				ts.ScrollAt(ui.Offset{Dx: 100, Dy: 90}, ui.Offset{Dy: 2 * wheelNotch})
				ts.ScrollAt(ui.Offset{Dx: 100, Dy: 90}, ui.Offset{Dx: 1.5 * wheelNotch})
				ts.Pump(time.Millisecond)
				ts.Pump(scrollbarRevealDuration + 20*time.Millisecond)
				// the vertical slider: the content is 660 in 180, so it is 49 long, at 100 of 480 scrolled
				slider := ui.Offset{Dx: 295, Dy: 45}
				switch state {
				case "hover":
					ts.Hover(slider)
					ts.Pump(scrollbarRevealDuration + 20*time.Millisecond)
					testutil.MatchesGolden(ts, "scrollable_hover_"+th.name)
				case "active":
					g := ts.StartGesture(slider, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
					ts.Pump(scrollbarRevealDuration + 20*time.Millisecond)
					testutil.MatchesGolden(ts, "scrollable_active_"+th.name)
					g.Up()
				}
			})
		}
	}
}
