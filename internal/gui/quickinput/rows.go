package quickinput

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Ported from VS Code: src/vs/platform/quickinput/browser/quickInputList.ts (QuickPickItemElementRenderer),
// media/quickInput.css and src/vs/base/browser/ui/keybindingLabel/keybindingLabel.ts

const (
	keyCapFontSize = 11.0
	detailFontSize = 12.0
	sepFontSize    = 12.0
	// fileIconGap is the room between the icon of a file and its name.
	fileIconGap = 6.0
)

// RowKey is the key of the widget of the row at an index, which a test finds it by.
func RowKey(i int) foundation.Key { return foundation.NewValueKey(rowKey(i)) }

type rowKey int

// byteRanges turns matches in runes into ranges in bytes of the text.
func byteRanges(text string, matches []fuzzy.Match) []kit.TextRange {
	if len(matches) == 0 {
		return nil
	}
	offsets := make([]int, 0, len(text)+1)
	for i := range text {
		offsets = append(offsets, i)
	}
	offsets = append(offsets, len(text))
	out := make([]kit.TextRange, 0, len(matches))
	for _, m := range matches {
		if m.Start >= len(offsets)-1 || m.End <= m.Start {
			continue
		}
		out = append(out, kit.TextRange{Start: offsets[m.Start], End: offsets[min(m.End, len(offsets)-1)]})
	}
	return out
}

// highlighted is a line of text with its matches in bold and in the highlight color.
func highlighted(bc widgets.BuildContext, text string, matches []fuzzy.Match, fg, highlight ui.Color) widgets.Widget {
	return kit.NewHighlightedLabel(bc, text, kit.RowState{Foreground: fg, HighlightForeground: highlight, Highlights: byteRanges(text, matches)})
}

// textSpans are the spans of a text with its matches in bold and in the highlight color.
func textSpans(text string, matches []fuzzy.Match, marked *painting.TextStyle) []painting.InlineSpan {
	ranges := byteRanges(text, matches)
	var spans []painting.InlineSpan
	at := 0
	for _, r := range ranges {
		r.Start, r.End = min(max(r.Start, at), len(text)), min(r.End, len(text))
		if r.End <= r.Start {
			continue
		}
		if r.Start > at {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text[at:r.Start])}))
		}
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text[r.Start:r.End]), Style: marked}))
		at = r.End
	}
	if at < len(text) {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text[at:])}))
	}
	return spans
}

// labelAndDescription is the first line of a row: the label, then the description in a dimmer color, as
// one line of text that ends with an ellipsis when it does not fit.
func labelAndDescription(bc widgets.BuildContext, it Item, r Row, fg, meta, highlight ui.Color) widgets.Widget {
	base := kit.TextStyleOf(bc, rowFontSize, fg)
	if it.Italic {
		base = base.CopyWith(painting.TextStyleOpts{FontStyle: ui.FontStyleItalic})
	}
	marked := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(highlight), FontWeight: ui.FontWeightBold})
	spans := textSpans(it.Label, r.Label, marked)
	if it.Description != "" {
		dim := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(meta)})
		dimMarked := dim.CopyWith(painting.TextStyleOpts{Color: foundation.Some(highlight), FontWeight: ui.FontWeightBold})
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(" "), Style: dim}))
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Style: dim, Children: textSpans(it.Description, r.Description, dimMarked)}))
	}
	return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: base, Children: spans}),
		widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// highlightedSized is highlighted in a smaller size: the detail line of a row.
func highlightedSized(bc widgets.BuildContext, text string, matches []fuzzy.Match, fg, highlight ui.Color, size float64) widgets.Widget {
	if len(matches) == 0 {
		return widgets.NewText(text, widgets.TextOpts{Style: kit.TextStyleOf(bc, size, fg), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	return highlighted(bc, text, matches, fg, highlight)
}

// row is the row of the list at an index: the active row has the focus background, the row under the
// pointer the hover one.
func (s *hostState) row(bc widgets.BuildContext, colors kit.Colors, i int, r Row) widgets.Widget {
	active := i == s.picker.ActiveIndex()
	fg, highlight := colors.QuickInputForeground, colors.ListHighlightForeground
	var bg ui.Color
	switch {
	case active:
		if c := colors.QuickInputListFocusForeground; c.Alpha() > 0 {
			fg = c
		}
		highlight, bg = colors.QuickInputListFocusHighlightForeground, colors.QuickInputListFocusBackground
	case s.hover == i:
		bg = colors.ListHoverBackground
	}
	it := r.Item

	var texts []widgets.Widget
	// the label and the description are one line of text, which is cut with an ellipsis as a whole
	meta := fg
	if !active {
		meta = fg.WithAlpha(0xB3)
	}
	line := labelAndDescription(bc, it, r, fg, meta, highlight)
	texts = append(texts, widgets.NewSizedBox(widgets.NewAlign(line, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft}),
		widgets.SizedBoxOpts{Height: foundation.Some(RowHeight - 1)}))
	if it.Detail != "" {
		texts = append(texts, widgets.NewSizedBox(highlightedSized(bc, it.Detail, r.Detail, meta, highlight, detailFontSize), widgets.SizedBoxOpts{Height: foundation.Some(RowHeight - 1)}))
	}

	var cells []widgets.Widget
	iconCol := fg
	if active {
		if c := colors.QuickInputListFocusIconForeground; c.Alpha() > 0 {
			iconCol = c
		}
	}
	fileIcon, hasFileIcon := theme.FileIcon{}, false
	if it.FileIcon != nil {
		fileIcon, hasFileIcon = it.FileIcon(colors.Type)
	}
	if hasFileIcon {
		cells = append(cells, kit.NewFileIcon(fileIcon, iconCol), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(fileIconGap)}))
	} else if it.Icon != 0 {
		cells = append(cells, widgets.NewSizedBox(
			widgets.NewIcon(fonts.CodiconIcon(it.Icon), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(iconCol)}),
			widgets.SizedBoxOpts{Width: foundation.Some(16.0)}), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(5.0)}))
	}
	cells = append(cells, widgets.NewExpanded(widgets.NewColumn(texts, widgets.ColumnOpts{MainAxisAlignment: rendering.MainAxisAlignmentCenter, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})))
	if it.Keybinding != "" {
		cells = append(cells, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}), s.keyCaps(bc, colors, it.Keybinding, active || s.hover == i, fg))
		cells = append(cells, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}))
	}
	if r.Separator != "" {
		cells = append(cells,
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}),
			widgets.NewText(r.Separator, widgets.TextOpts{Style: kit.TextStyleOf(bc, sepFontSize, sepColor(colors, active, fg)), MaxLines: foundation.Some(1)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}))
	}
	for b, btn := range it.Buttons {
		if btn.AlwaysVisible || active || s.hover == i {
			icon := btn.Icon
			if btn.HoverIcon != 0 && s.hover == i {
				icon = btn.HoverIcon
			}
			cells = append(cells, kit.NewIconButton(icon, kit.IconButtonOpts{Tooltip: btn.Tooltip, OnPressed: func() { s.ctl.Trigger(r, b, s.mods()) }}))
		}
	}

	opts := painting.BoxDecorationOpts{BorderRadius: painting.NewBorderRadiusCircular(rowRadius)}
	if bg.Alpha() > 0 {
		opts.Color = foundation.Some(bg)
	}
	deco := painting.NewBoxDecoration(opts)
	body := widgets.NewContainer(
		widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, entryPadding), widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
		widgets.ContainerOpts{Decoration: deco})
	var withLine widgets.Widget = body
	if r.Border {
		withLine = widgets.NewStack([]widgets.Widget{
			body,
			widgets.NewPositioned(widgets.NewIgnorePointer(widgets.NewContainer(nil, widgets.ContainerOpts{
				Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(colors.PickerGroupBorder)})})),
				widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0), Top: foundation.Some(0.0), Height: foundation.Some(1.0)}),
		}, widgets.StackOpts{Fit: rendering.StackFitExpand})
	}
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(withLine, widgets.GestureDetectorOpts{OnTap: func() {
			s.picker.SetActive(i)
			s.ctl.Accept(s.mods())
		}}),
		widgets.MouseRegionOpts{
			Key:     RowKey(i),
			Cursor:  services.SystemMouseCursors.Click,
			OnEnter: func(*gestures.PointerEnterEvent) { s.setHover(i) },
			OnExit:  func(*gestures.PointerExitEvent) { s.setHover(-1) },
		})
}

// sepColor is the color of the label of a separator: the group color, or the text color in the active row.
func sepColor(colors kit.Colors, active bool, fg ui.Color) ui.Color {
	if active {
		return fg
	}
	return colors.PickerGroupForeground
}

// keyCaps draws the label of a keybinding as key caps (VS Code's keybinding label): each key in a small box
// with a border that is thicker below, a plus sign between the keys of a chord.
func (s *hostState) keyCaps(bc widgets.BuildContext, _ kit.Colors, label string, lit bool, fg ui.Color) widgets.Widget {
	return kit.NewKeyCaps(bc, KeyCaps(label), kit.KeyCapsOpts{FontSize: keyCapFontSize, Lit: lit, Foreground: fg})
}
