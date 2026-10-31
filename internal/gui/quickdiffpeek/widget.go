package quickdiffpeek

import (
	"math"
	"strconv"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The widget of the peek: the zone under the changed lines, with the title bar over the small diff editor, framed in the color of the kind of
// the change. VS Code draws an arrow from the frame to the line too; here the frame is the zone's top and bottom border.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/quickDiffWidget.ts (QuickDiffWidget) and
// src/vs/editor/contrib/peekView/browser/peekView.ts (PeekViewWidget)

const (
	// titleFont is the font size of the title bar.
	titleFont = 13.0
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

// frameColor is the color of the frame for a change: that of the stripe of its kind.
func frameColor(colors kit.Colors, t quickdiff.ChangeType) ui.Color {
	switch t {
	case quickdiff.Add:
		return colors.EditorGutterAdded
	case quickdiff.Delete:
		return colors.EditorGutterDeleted
	}
	return colors.EditorGutterModified
}

// peekColors are the colors of the editors of the peek: the theme's with the background of the peek view.
func peekColors(c kit.Colors) kit.Colors {
	c.EditorBackground = c.PeekViewEditorBackground
	c.EditorGutterBackground = c.PeekViewEditorGutterBackground
	return c
}

// detail is the secondary heading of the title: the provider and the place of the change among its provider's changes.
func detail(s Shown) string {
	word := "change"
	if s.Total > 1 {
		word = "changes"
	}
	return s.Label + " - " + strconv.Itoa(s.Index+1) + " of " + strconv.Itoa(len(s.Changes)) + " " + word
}

// Build implements widgets.State.
func (s *frameState) Build(bc widgets.BuildContext) widgets.Widget {
	c := s.Widget().c
	shown, ok := c.Shown()
	if !ok {
		return widgets.NewSizedBox(nil)
	}
	colors := kit.ThemeOf(bc)
	head := math.Ceil(c.cfg.Editor.Options().LineHeight * 1.2)
	var body widgets.Widget = widgets.NewColoredBox(colors.PeekViewEditorBackground, widgets.NewSizedBox(nil))
	if b := c.Body(); b != nil && b.Widget != nil {
		body = kit.NewTheme(peekColors(colors), b.Widget, kit.ThemeOpts{Tokens: kit.TokenThemeOf(bc), UIFont: kit.UIFontOf(bc)})
	}
	side := painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(frameColor(colors, shown.Change.Type())), Width: foundation.Some(frameWidth)})
	// the zone is as wide as the text area, which the scrollbar lies over: the frame stops short of it, as the peek view of references does
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, kit.DefaultVerticalScrollbarSize, 0), widgets.NewContainer(
		widgets.NewColumn([]widgets.Widget{s.title(bc, colors, shown, head), widgets.NewExpanded(body)},
			widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:  foundation.Some(colors.PeekViewTitleBackground),
			Border: painting.Border{Top: side, Bottom: side},
		})}))
}

// title is the title bar: the name of the file and the heading on the left, the actions of the menu, the arrows and the close button on
// the right (VS Code puts the actions of the menu first, in reverse order, then next and previous, then close).
func (s *frameState) title(bc widgets.BuildContext, colors kit.Colors, shown Shown, height float64) widgets.Widget {
	c := s.Widget().c
	label := kit.TextStyleOf(bc, titleFont, colors.PeekViewTitleLabelForeground)
	dim := kit.TextStyleOf(bc, titleFont, colors.PeekViewTitleDescriptionForeground)
	text := widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: []painting.InlineSpan{
		painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(c.Title()), Style: label}),
		painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + detail(shown)), Style: dim}),
	}}), widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	row := []widgets.Widget{
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(16.0)}),
		widgets.NewExpanded(text),
	}
	for _, a := range c.Actions() {
		run := a.Run
		row = append(row, kit.NewIconButton(a.Icon, kit.IconButtonOpts{Tooltip: a.Tooltip, OnPressed: func() {
			run()
			c.Close()
		}}))
	}
	row = append(row,
		kit.NewIconButton(fonts.CodiconArrowDown, kit.IconButtonOpts{Tooltip: "Show Next Change", OnPressed: func() { c.Step(true) }}),
		kit.NewIconButton(fonts.CodiconArrowUp, kit.IconButtonOpts{Tooltip: "Show Previous Change", OnPressed: func() { c.Step(false) }}),
		kit.NewIconButton(fonts.CodiconClose, kit.IconButtonOpts{Tooltip: "Close", OnPressed: c.Close}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
	)
	return widgets.NewSizedBox(widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.SizedBoxOpts{Height: foundation.Some(height)})
}
