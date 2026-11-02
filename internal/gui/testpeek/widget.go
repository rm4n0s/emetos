package testpeek

import (
	"math"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The widget of the peek: the zone under the failing line, with the title bar over the message and its stack on the left and the small diff
// editor (or the output) on the right, framed in the color of the kind of the message. VS Code draws an arrow from the frame to the line
// too; here the frame is the zone's top and bottom border.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingOutputPeek.ts (TestResultsPeek, MessageSubject rendering) and
// src/vs/editor/contrib/peekView/browser/peekView.ts (PeekViewWidget)

const (
	// titleFont is the font size of the title bar and bodyFont the one of the message.
	titleFont = 13.0
	bodyFont  = 13.0
	// frameWidth is the width of the frame (VS Code's frameWidth of 1).
	frameWidth = 1.0
)

// frame is the widget of the zone.
type frame struct {
	widgets.StatefulWidget
	c *Controller
}

func newFrame(c *Controller) widgets.Widget {
	return &frame{StatefulWidget: widgets.NewStatefulWidgetBase(), c: c}
}

// CreateState implements widgets.StatefulWidget.
func (f *frame) CreateState() widgets.State { return &frameState{} }

type frameState struct {
	widgets.StateOf[*frame]
	handle foundation.ListenerHandle
}

// InitState follows the controller.
func (s *frameState) InitState() {
	s.handle = s.Widget().c.AddListener(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// Dispose stops following the controller.
func (s *frameState) Dispose() { s.Widget().c.RemoveListener(s.handle) }

// peekColors are the colors of the editors of the peek: the theme's with the background of the peek view.
func peekColors(c kit.Colors) kit.Colors {
	c.EditorBackground = c.PeekViewEditorBackground
	c.EditorGutterBackground = c.PeekViewEditorGutterBackground
	return c
}

// colorOf is the theme color with the id, else the fallback.
func colorOf(colors kit.Colors, id string, fallback ui.Color) ui.Color {
	if c, ok := colors.Named(id); ok && c.Alpha() != 0 {
		return c
	}
	return fallback
}

// Build implements widgets.State.
func (s *frameState) Build(bc widgets.BuildContext) widgets.Widget {
	c := s.Widget().c
	cur, ok := c.Current()
	if !ok {
		return widgets.NewSizedBox(nil)
	}
	colors := kit.ThemeOf(bc)
	head := math.Ceil(c.cfg.Editor.Options().LineHeight * 1.2)
	border, header := colorOf(colors, "testing.messagePeekBorder", colors.PeekViewBorder), colorOf(colors, "testing.messagePeekHeaderBackground", colors.PeekViewTitleBackground)
	if cur.Failure {
		border, header = colorOf(colors, "testing.peekBorder", colors.PeekViewBorder), colorOf(colors, "testing.peekHeaderBackground", colors.PeekViewTitleBackground)
	}
	side := painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(frameWidth)})
	var content widgets.Widget = widgets.NewColoredBox(colors.PeekViewEditorBackground, s.panes(bc, colors, cur))
	// the zone is as wide as the text area, which the scrollbar lies over: the frame stops short of it, as the peek view of references does
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, kit.DefaultVerticalScrollbarSize, 0), widgets.NewContainer(
		widgets.NewColumn([]widgets.Widget{
			widgets.NewColoredBox(header, s.title(bc, colors, cur, head)),
			widgets.NewExpanded(content),
		}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:  foundation.Some(colors.PeekViewTitleBackground),
			Border: painting.Border{Top: side, Bottom: side},
		})}))
}

// title is the title bar: the label of the test and where the message is on the left, the buttons and the arrows and the close button on the
// right.
func (s *frameState) title(bc widgets.BuildContext, colors kit.Colors, cur Subject, height float64) widgets.Widget {
	c := s.Widget().c
	label := kit.TextStyleOf(bc, titleFont, colors.PeekViewTitleLabelForeground)
	dim := kit.TextStyleOf(bc, titleFont, colors.PeekViewTitleDescriptionForeground)
	detail := cur.Place
	if i, n := c.Position(); n > 0 {
		if detail != "" {
			detail += " - "
		}
		detail += strconv.Itoa(i) + " of " + strconv.Itoa(n)
	}
	text := widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: []painting.InlineSpan{
		painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(cur.Title), Style: label}),
		painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + detail), Style: dim}),
	}}), widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	row := []widgets.Widget{
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(16.0)}),
		widgets.NewExpanded(text),
	}
	for _, a := range c.Actions() {
		row = append(row, kit.NewIconButton(a.Icon, kit.IconButtonOpts{Tooltip: a.Tooltip, OnPressed: a.Run}))
	}
	if cur.Output != "" {
		tip := "Show Output"
		if c.ShowOutput() {
			tip = "Show Diff"
			if !cur.HasDiff {
				tip = "Hide Output"
			}
		}
		row = append(row, kit.NewIconButton(fonts.CodiconTerminal, kit.IconButtonOpts{Tooltip: tip, OnPressed: c.ToggleOutput}))
	}
	row = append(row,
		kit.NewIconButton(fonts.CodiconArrowDown, kit.IconButtonOpts{Tooltip: "Go to Next Test Failure", OnPressed: func() { c.Step(true) }}),
		kit.NewIconButton(fonts.CodiconArrowUp, kit.IconButtonOpts{Tooltip: "Go to Previous Test Failure", OnPressed: func() { c.Step(false) }}),
		kit.NewIconButton(fonts.CodiconClose, kit.IconButtonOpts{Tooltip: "Close", OnPressed: c.Close}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
	)
	return widgets.NewSizedBox(widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.SizedBoxOpts{Height: foundation.Some(height)})
}

// panes are the two panes under the title bar: the message and the stack trace, and beside them the diff or the output when there is one.
func (s *frameState) panes(bc widgets.BuildContext, colors kit.Colors, cur Subject) widgets.Widget {
	c := s.Widget().c
	left := s.message(bc, colors, cur)
	var right widgets.Widget
	switch {
	case cur.Output != "" && (c.ShowOutput() || !cur.HasDiff && cur.Text == "" && len(cur.Stack) == 0):
		right = kit.NewAnsiText(cur.Output, kit.AnsiTextOpts{Background: foundation.Some(colors.PeekViewEditorBackground), TrimEndNewline: true})
		right = widgets.NewClipRect(widgets.NewSingleChildScrollView(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(4, 8), right)))
	case cur.HasDiff && c.Body() != nil && c.Body().Widget != nil:
		right = kit.NewTheme(peekColors(colors), c.Body().Widget, kit.ThemeOpts{Tokens: kit.TokenThemeOf(bc), UIFont: kit.UIFontOf(bc)})
	}
	if right == nil {
		return left
	}
	divider := widgets.NewSizedBox(widgets.NewColoredBox(colors.PeekViewBorder.WithOpacity(0.5), widgets.NewSizedBox(nil)), widgets.SizedBoxOpts{Width: foundation.Some(1.0)})
	return widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(left, widgets.ExpandedOpts{Flex: foundation.Some(2)}), divider, widgets.NewExpanded(right, widgets.ExpandedOpts{Flex: foundation.Some(3)}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// message is the text of the message and the frames of the stack trace, in a scrolling column.
func (s *frameState) message(bc widgets.BuildContext, colors kit.Colors, cur Subject) widgets.Widget {
	c := s.Widget().c
	fg := kit.TextStyleOf(bc, bodyFont, colors.Foreground)
	link := kit.TextStyleOf(bc, bodyFont, colors.TextLinkForeground)
	dim := kit.TextStyleOf(bc, bodyFont, colors.DescriptionForeground)
	var rows []widgets.Widget
	if cur.Text != "" {
		rows = append(rows, widgets.NewText(strings.TrimRight(cur.Text, "\n"), widgets.TextOpts{Style: fg}))
	}
	if len(cur.Stack) > 0 {
		if len(rows) > 0 {
			rows = append(rows, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(6.0)}))
		}
		rows = append(rows, widgets.NewText("Stack", widgets.TextOpts{Style: dim}))
		for _, f := range cur.Stack {
			rows = append(rows, frameRow(f, fg, link, dim, c.OpenFrame))
		}
	}
	if len(rows) == 0 {
		rows = append(rows, widgets.NewText("The test has no message.", widgets.TextOpts{Style: dim}))
	}
	col := widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart, MainAxisSize: rendering.MainAxisSizeMin})
	return widgets.NewClipRect(widgets.NewSingleChildScrollView(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(4, 12), col)))
}

// frameRow is a frame of the stack trace: its label, and its place as a link that opens the file.
func frameRow(f StackFrame, fg, link, dim *painting.TextStyle, open func(StackFrame)) widgets.Widget {
	place := ""
	if f.Path != "" {
		place = f.Path
		if f.Display != "" {
			place = f.Display
		}
		if f.Line > 0 {
			place += ":" + strconv.Itoa(f.Line)
			if f.Col > 0 {
				place += ":" + strconv.Itoa(f.Col)
			}
		}
	}
	spans := []painting.InlineSpan{painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(f.Label), Style: fg})}
	if place == "" {
		return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}))
	}
	spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  "), Style: dim}))
	spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(place), Style: link}))
	text := widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}))
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(text, widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque,
			OnTap:    func() { open(f) },
		}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
}
