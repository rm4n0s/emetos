package editor

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The light bulb and the list of code actions (SPEC §9.12 "As built (step 12.8)").
//
// Ported from VS Code: src/vs/editor/contrib/codeAction/browser/lightBulbWidget.ts, src/vs/platform/actionWidget/browser/actionList.ts
// and actionWidget.css

// Metrics of the widgets.
const (
	// lightBulbSpace is the width of space to the left of the text of a line that the light bulb needs to stand there; with less
	// the bulb goes into the glyph margin of the gutter (VS Code's 22 pixels).
	lightBulbSpace = 22.0
	lightBulbIcon  = 16.0

	actionRowHeight    = 24.0
	actionListPadding  = 4.0
	actionListRadius   = 6.0
	actionListShadow   = 8.0
	actionListMinWidth = 200.0
	actionListMaxWidth = 500.0
	actionFontSize     = 13.0
	actionHeaderSize   = 11.0
	actionBarHeight    = 22.0
	actionMaxRows      = 12
)

// lightBulbIconCode is the Codicon of the bulb.
func lightBulbIconCode(b *LightBulb) int {
	if b.Icon == LightBulbAutoFix {
		return fonts.CodiconLightbulbAutofix
	}
	return fonts.CodiconLightbulb
}

// lightBulbLayer is the light bulb at the start of the line of the cursor: at the left of the text when the line is indented by
// more than 22 pixels, else in the glyph margin of the gutter. Without room for it (no glyph margin) it is not shown.
//
// Ported from VS Code: lightBulbWidget.ts (update): VS Code also tries the lines above and below before the gutter.
func (s *editorState) lightBulbLayer(vm *ViewModel, colors kit.Colors) widgets.Widget {
	b := vm.LightBulb()
	if b == nil || s.textRO == nil || !s.textRO.Attached() || vm.viewport <= 0 {
		return nil
	}
	start := buffer.Position{Line: b.Pos.Line}
	rect := s.textRO.RectOf(start)
	if rect.Bottom < 0 || rect.Top > vm.viewport {
		return nil
	}
	lineH := math.Max(vm.opts.LineHeight, 1)
	origin := 0.0
	if vm.mm.info.Show && vm.Options().Minimap.Side == "left" {
		origin = vm.mm.info.Width
	}
	line, _ := vm.doc.Snapshot().LineContent(b.Pos.Line)
	charW := math.Max(vm.opts.FontSize*0.6, 1)
	indent := float64(indentWidth(line, vm.opts.TabSize)) * charW
	var left float64
	switch {
	case indent > lightBulbSpace:
		left = s.gutterWidth + origin + rect.Left
	case vm.opts.GlyphMargin:
		left = origin
	default:
		return nil
	}
	size := math.Min(lineH, lightBulbIcon)
	fg := colors.EditorLightBulbForeground
	if b.Icon == LightBulbAutoFix {
		fg = colors.EditorLightBulbAutoFixForeground
	}
	icon := widgets.NewCenter(widgets.NewIcon(fonts.CodiconIcon(lightBulbIconCode(b)), widgets.IconOpts{Size: foundation.Some(size), Color: foundation.Some(fg)}), widgets.CenterOpts{})
	box := widgets.NewMouseRegion(
		widgets.NewGestureDetector(widgets.NewSizedBox(icon, widgets.SizedBoxOpts{Width: foundation.Some(lineH), Height: foundation.Some(lineH)}),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: vm.LightBulbClick}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
	return widgets.NewStack([]widgets.Widget{
		widgets.NewPositioned(box, widgets.PositionedOpts{Left: foundation.Some(left), Top: foundation.Some(rect.Top)}),
	}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// indentWidth is the number of columns the white space at the start of a line takes (a tab goes to the next multiple of the tab
// size).
func indentWidth(line string, tabSize int) int {
	tabSize = max(tabSize, 1)
	col := 0
	for _, r := range line {
		switch r {
		case ' ':
			col++
		case '\t':
			col += tabSize - col%tabSize
		default:
			return col
		}
	}
	return col
}

// actionListSize estimates the size of the list from the text of its rows.
func actionListSize(l *ActionList) (width, height float64) {
	longest := 0
	for _, it := range l.Items {
		n := utf8.RuneCountInString(it.Title)
		if it.Key != "" {
			n += utf8.RuneCountInString(it.Key) + 3
		}
		longest = max(longest, n)
	}
	width = math.Min(math.Max(float64(longest)*actionFontSize*0.56+60, actionListMinWidth), actionListMaxWidth)
	rows := min(len(l.Items), actionMaxRows)
	height = float64(rows)*actionRowHeight + 2*actionListPadding + 2
	if len(l.Bar) > 0 {
		height += actionBarHeight + 4
	}
	return width, height
}

// actionListLayer is the list under the line it hangs from, above it when there is no room.
func (s *editorState) actionListLayer(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors) widgets.Widget {
	l := vm.CodeActionList()
	if l == nil || s.textRO == nil || !s.textRO.Attached() || vm.viewport <= 0 {
		return nil
	}
	rect := s.textRO.RectOf(l.Anchor)
	if rect.Bottom < 0 || rect.Top > vm.viewport {
		return nil
	}
	origin := s.gutterWidth
	if vm.mm.info.Show && vm.Options().Minimap.Side == "left" {
		origin += vm.mm.info.Width
	}
	width, height := actionListSize(l)
	left := origin + rect.Left
	if viewW, _ := vm.ViewportSize(); viewW > 0 {
		left = math.Max(math.Min(left, origin+viewW-width), 0)
	}
	pos := widgets.PositionedOpts{Left: foundation.Some(left)}
	below := vm.viewport - rect.Bottom
	if height > below && rect.Top > below {
		pos.Bottom = foundation.Some(vm.viewport - rect.Top)
	} else {
		pos.Top = foundation.Some(math.Max(rect.Bottom, 0))
	}
	list := s.actionListBox(bc, vm, colors, l, width)
	return widgets.NewStack([]widgets.Widget{widgets.NewPositioned(list, pos)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// actionText is a text of the list in the UI font.
func actionText(bc widgets.BuildContext, text string, size float64, color ui.Color, weight ui.FontWeight) widgets.Widget {
	style := kit.TextStyleOf(bc, size, color)
	if weight != 0 {
		style = style.CopyWith(painting.TextStyleOpts{FontWeight: weight})
	}
	return widgets.NewText(text, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// keyCaps splits the label of a key ("Ctrl+K Ctrl+F") into the chords NewKeyCaps draws.
func keyCaps(label string) [][]string {
	var chords [][]string
	for _, chord := range strings.Fields(label) {
		chords = append(chords, strings.Split(chord, "+"))
	}
	return chords
}

// actionListBox is the box with the rows, scrolled when there are more than twelve.
func (s *editorState) actionListBox(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors, l *ActionList, width float64) widgets.Widget {
	var rows []widgets.Widget
	for i, it := range l.Items {
		rows = append(rows, s.actionRow(bc, vm, colors, l, i, it, width-2*actionListPadding-2))
	}
	var body widgets.Widget = widgets.NewColumn(rows, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})
	if len(l.Items) > actionMaxRows {
		body = widgets.NewSizedBox(widgets.NewSingleChildScrollView(body, widgets.SingleChildScrollViewOpts{Primary: foundation.Some(false)}),
			widgets.SizedBoxOpts{Height: foundation.Some(actionMaxRows * actionRowHeight)})
	}
	children := []widgets.Widget{body}
	if len(l.Bar) > 0 {
		var links []widgets.Widget
		for _, b := range l.Bar {
			b := b
			links = append(links, widgets.NewMouseRegion(
				widgets.NewGestureDetector(actionText(bc, b.Label, 12, colors.TextLinkActiveForeground, 0),
					widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: b.Run}),
				widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click}))
		}
		children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(4.0)}),
			widgets.NewContainer(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(26, 0, 8, 0), widgets.NewRow(links, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin})),
				widgets.ContainerOpts{Height: foundation.Some(actionBarHeight), Alignment: painting.AlignmentCenterLeft,
					Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
						Border: painting.Border{Top: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.EditorHoverWidgetBorder), Width: foundation.Some(1.0)})},
					})}))
	}
	box := widgets.NewContainer(widgets.NewPadding(painting.NewEdgeInsetsAll(actionListPadding), widgets.NewColumn(children, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})),
		widgets.ContainerOpts{Width: foundation.Some(width), Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.MenuBackground),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.EditorHoverWidgetBorder), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(actionListRadius),
			BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: actionListShadow})},
		})})
	return widgets.NewMouseRegion(widgets.NewGestureDetector(box, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Basic})
}

// actionRow is a row of the list: a header, or an action with its icon, its title and its key.
func (s *editorState) actionRow(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors, l *ActionList, i int, it ActionListItem, width float64) widgets.Widget {
	if it.Header {
		return widgets.NewContainer(
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(6, 0, 12, 0), actionText(bc, it.Title, actionHeaderSize, colors.DescriptionForeground, ui.FontWeightBold)),
			widgets.ContainerOpts{Width: foundation.Some(width), Height: foundation.Some(actionRowHeight), Alignment: painting.AlignmentCenterLeft})
	}
	focused := i == l.Focus
	fg := colors.MenuForeground
	if it.Disabled {
		fg = colors.DisabledForeground
	}
	bg := ui.Color{}
	var outline ui.Color
	if focused && !it.Disabled {
		bg, outline = colors.ListHoverBackground, colors.MenuSelectionBorder
	}
	iconColor, code := fg, it.Icon
	if code == 0 {
		code = fonts.CodiconLightbulb
		if !it.Disabled {
			iconColor = colors.EditorLightBulbForeground
		}
	}
	parts := []widgets.Widget{
		widgets.NewSizedBox(widgets.NewCenter(widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(12.0), Color: foundation.Some(iconColor)}), widgets.CenterOpts{}),
			widgets.SizedBoxOpts{Width: foundation.Some(16.0), Height: foundation.Some(actionRowHeight)}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
		widgets.NewExpanded(actionText(bc, it.Title, actionFontSize, fg, 0)),
	}
	if it.Key != "" {
		parts = append(parts, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(12.0)}), kit.NewKeyCaps(bc, keyCaps(it.Key), kit.KeyCapsOpts{}))
	}
	row := widgets.NewContainer(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(6, 0, 12, 0), widgets.NewRow(parts, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
		widgets.ContainerOpts{Width: foundation.Some(width), Height: foundation.Some(actionRowHeight),
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(bg), BorderRadius: painting.NewBorderRadiusCircular(4)}),
			ForegroundDecoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(outline), Width: foundation.Some(1.0)}),
				BorderRadius: painting.NewBorderRadiusCircular(4),
			})})
	if it.Disabled {
		return row
	}
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { vm.CodeActionMenuClick(i) }}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click, OnEnter: func(*gestures.PointerEnterEvent) { vm.CodeActionMenuFocus(i) }})
}
