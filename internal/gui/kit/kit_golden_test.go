package kit

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// goldenThemes are the themes every golden of this file is made in.
var goldenThemes = []struct {
	name   string
	colors func() Colors
}{{"dark", DarkModern}, {"light", LightModern}}

func gap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

func padded(child widgets.Widget) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsAll(16), child)
}

func row(children ...widgets.Widget) widgets.Widget {
	return widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

func column(children ...widgets.Widget) widgets.Widget {
	return widgets.NewColumn(children, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
}

func vgap(h float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(h)})
}

func TestGoldenButton(t *testing.T) {
	noop := func() {}
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			content := padded(column(
				row(
					NewButton("Primary", ButtonOpts{OnPressed: noop}), gap(16),
					NewButton("Secondary", ButtonOpts{OnPressed: noop, Secondary: true}), gap(16),
					NewButton("Small", ButtonOpts{OnPressed: noop, Small: true}),
				), vgap(16),
				row(
					NewButton("Disabled", ButtonOpts{Disabled: true, OnPressed: noop}), gap(16),
					NewButton("Disabled", ButtonOpts{Disabled: true, Secondary: true, OnPressed: noop}), gap(16),
					NewButton("Add", ButtonOpts{OnPressed: noop, Icon: fonts.CodiconAdd}), gap(16),
					NewButton("Run", ButtonOpts{OnPressed: noop, Secondary: true, Icon: fonts.CodiconPlay}),
				),
			))
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 440, Height: 130}, content)
			testutil.MatchesGolden(ts, "button_"+th.name)
		})
	}
}

func TestGoldenButtonStates(t *testing.T) {
	noop := func() {}
	for _, th := range goldenThemes {
		for _, variant := range []struct {
			name      string
			secondary bool
		}{{"primary", false}, {"secondary", true}} {
			t.Run(th.name+" "+variant.name, func(t *testing.T) {
				content := padded(row(
					NewButton("Hover", ButtonOpts{OnPressed: noop, Secondary: variant.secondary}), gap(24),
					NewButton("Focus", ButtonOpts{OnPressed: noop, Secondary: variant.secondary, Autofocus: true}), gap(24),
					NewButton("Plain", ButtonOpts{OnPressed: noop, Secondary: variant.secondary}),
				))
				ts := pumpThemed(t, th.colors(), ui.Size{Width: 340, Height: 62}, content)
				ts.Hover(ts.Center(gluttertest.ByText("Hover")))
				ts.Pump(time.Millisecond)
				testutil.MatchesGolden(ts, "button_states_"+variant.name+"_"+th.name)
			})
		}
	}
}

func TestGoldenIconButton(t *testing.T) {
	noop := func() {}
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			content := padded(row(
				NewIconButton(fonts.CodiconClose, IconButtonOpts{OnPressed: noop}), gap(8),
				NewIconButton(fonts.CodiconClose, IconButtonOpts{OnPressed: noop}), gap(8),
				NewIconButton(fonts.CodiconClose, IconButtonOpts{OnPressed: noop, Autofocus: true}), gap(8),
				NewIconButton(fonts.CodiconWordWrap, IconButtonOpts{OnPressed: noop, Checked: true}), gap(8),
				NewIconButton(fonts.CodiconClose, IconButtonOpts{OnPressed: noop, Disabled: true}), gap(8),
				NewIconButton(fonts.CodiconClose, IconButtonOpts{OnPressed: noop}),
			))
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 220, Height: 54}, content)
			// the second button is under the pointer and the last one is pressed
			ts.Hover(ts.Center(gluttertest.At(gluttertest.ByType[*IconButton](), 1)))
			ts.Pump(time.Millisecond)
			testutil.MatchesGolden(ts, "iconbutton_"+th.name)
		})
	}
}

func TestGoldenActionBar(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			ctrl := NewActionBarController()
			horizontal := NewActionBar(testActions(), ActionBarOpts{Controller: ctrl})
			vertical := NewActionBar([]Action{
				{ID: "a", Label: "Open File"}, {ID: "b", Label: "Save", Disabled: true}, SeparatorAction(),
				{ID: "c", Label: "Word Wrap", Checked: true}, {ID: "d", Label: "Close"},
			}, ActionBarOpts{Orientation: ActionsVertical})
			content := padded(row(column(horizontal), gap(32), column(vertical)))
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 300, Height: 170}, content)
			ctrl.FocusIndex(3)
			ts.Pump(time.Millisecond)
			// the pointer rests on the first action of the vertical bar, after the tooltip is gone
			ts.Hover(ts.Center(gluttertest.At(gluttertest.ByType[*actionItem](), 4)))
			ts.Pump(time.Millisecond)
			testutil.MatchesGolden(ts, "actionbar_"+th.name)
		})
	}
}

func TestGoldenHover(t *testing.T) {
	for _, th := range goldenThemes {
		for _, pos := range []struct {
			name string
			at   ui.Offset
			pos  HoverPosition
		}{{"below", ui.Offset{Dx: 24, Dy: 16}, HoverBelow}, {"above", ui.Offset{Dx: 24, Dy: 80}, HoverAbove}} {
			t.Run(th.name+" "+pos.name, func(t *testing.T) {
				ctrl := NewHoverController()
				target := NewHover(NewIconButton(fonts.CodiconSave, IconButtonOpts{OnPressed: func() {}}), HoverOpts{Text: "Save (Ctrl+S)", Position: pos.pos, Compact: true, Controller: ctrl})
				child := widgets.NewStack([]widgets.Widget{
					widgets.NewPositioned(target, widgets.PositionedOpts{Left: foundation.Some(pos.at.Dx), Top: foundation.Some(pos.at.Dy)}),
				})
				ts := pumpThemed(t, th.colors(), ui.Size{Width: 200, Height: 110}, widgets.NewSizedBox(child, widgets.SizedBoxOpts{Width: foundation.Some(200.0), Height: foundation.Some(110.0)}))
				ts.Hover(ts.Center(gluttertest.ByType[*IconButton]()))
				ts.Pump(DefaultHoverDelay + time.Millisecond)
				if !strings.Contains(ts.Widget(gluttertest.ByText("Save (Ctrl+S)")).(*widgets.Text).PlainText(), "Save") {
					t.Fatal("no hover")
				}
				testutil.MatchesGolden(ts, "hover_"+pos.name+"_"+th.name)
			})
		}
	}
	// a plain hover (13 px text and 4 px of padding) with two lines
	for _, th := range goldenThemes {
		t.Run(th.name+" wrapped", func(t *testing.T) {
			ctrl := NewHoverController()
			target := NewHover(NewButton("Run", ButtonOpts{OnPressed: func() {}}), HoverOpts{
				Text: "Runs the current file in the terminal, with the arguments of the last run.", Controller: ctrl,
			})
			child := widgets.NewStack([]widgets.Widget{widgets.NewPositioned(target, widgets.PositionedOpts{Left: foundation.Some(16.0), Top: foundation.Some(16.0)})})
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 260, Height: 150}, widgets.NewSizedBox(child, widgets.SizedBoxOpts{Width: foundation.Some(260.0), Height: foundation.Some(150.0)}))
			ctrl.Show()
			ts.Pump(time.Millisecond)
			testutil.MatchesGolden(ts, "hover_text_"+th.name)
		})
	}
}
