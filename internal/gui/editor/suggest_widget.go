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

	"github.com/rm4n0s/emetos/extension/ui/markdown"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The suggest widget (SPEC §9.12 "As built (step 12.5)"): the list of items below the word being completed, above it when
// there is no room, and the details pane at the side of it.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestWidget.ts, suggestWidgetRenderer.ts, suggestWidgetDetails.ts
// and media/suggest.css

// Metrics of the widget (suggest.css, suggestWidget.ts getLayoutInfo).
const (
	suggestWidth           = 430.0
	suggestDetailsWidth    = 330.0
	suggestDetailsMinWidth = 220.0
	suggestVerticalPadding = 22.0
	suggestIconSize        = 16.0
	suggestRowPadLeft      = 2.0
	suggestRowPadRight     = 10.0
	suggestRadius          = 4.0
	suggestShadowBlur      = 8.0
	suggestMessageIndent   = 22.0
	// suggestDetailLabelRatio is the share of a row the detail may take (max-width: 70%).
	suggestDetailLabelRatio = 0.7
)

// suggestCodicons maps a kind of item (the LSP numbers) to the codepoint of its icon and the name of its color.
var suggestCodicons = map[langfeatures.CompletionKind]struct {
	code  int
	color string
}{
	1: {0xEA93, "text"}, 2: {0xEA8C, "method"}, 3: {0xEA8C, "function"}, 4: {0xEA8C, "constructor"}, 5: {0xEB5F, "field"},
	6: {0xEA88, "variable"}, 7: {0xEB5B, "class"}, 8: {0xEB61, "interface"}, 9: {0xEA8B, "module"}, 10: {0xEB65, "property"},
	11: {0xEA96, "unit"}, 12: {0xEA95, "enumerator"}, 13: {0xEA95, "enumerator"}, 14: {0xEB62, "keyword"}, 15: {0xEB66, "snippet"},
	16: {0xEB5C, "color"}, 17: {0xEA7B, "file"}, 18: {0xEA94, "reference"}, 19: {0xEA83, "folder"}, 20: {0xEB5E, "enumeratorMember"},
	21: {0xEB5D, "constant"}, 22: {0xEA91, "struct"}, 23: {0xEA86, "event"}, 24: {0xEB64, "operator"}, 25: {0xEA92, "typeParameter"},
}

// suggestRowHeight is the height of a row: editor.suggestLineHeight, else the line height of the editor (at least 8).
func (vm *ViewModel) suggestRowHeight() float64 {
	h := float64(vm.opts.Suggest.LineHeight)
	if h <= 0 {
		h = vm.opts.LineHeight
	}
	return math.Min(math.Max(h, 8), 1000)
}

// suggestFontSize is editor.suggestFontSize, else the font size of the editor.
func (vm *ViewModel) suggestFontSize() float64 {
	if s := vm.opts.Suggest.FontSize; s > 0 {
		return float64(s)
	}
	return vm.opts.FontSize
}

// suggestStyle is the text style of the widget: the font of the editor at the size of the widget.
func (vm *ViewModel) suggestStyle(color ui.Color, size float64) *painting.TextStyle {
	families := vm.opts.FontFamilies
	family, fallback := "monospace", []string(nil)
	if len(families) > 0 {
		family, fallback = families[0], families[1:]
	}
	opts := painting.TextStyleOpts{
		FontFamily: family, FontFamilyFallback: fallback, FontSize: foundation.Some(size), Color: foundation.Some(color),
		Height: foundation.Some(vm.suggestRowHeight() / size),
	}
	if w := vm.opts.FontWeight; w > 0 {
		opts.FontWeight = ui.FontWeight(w)
	}
	return painting.NewTextStyle(opts)
}

// suggestLayer is the widget over the text, or nil while there is none: the list below the word being completed (above it
// when there is no room) and the details at its side.
func (s *editorState) suggestLayer(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors) widgets.Widget {
	v := vm.SuggestView()
	if v == nil || s.textRO == nil || !s.textRO.Attached() || vm.viewport <= 0 {
		return nil
	}
	rect := s.textRO.RectOf(v.Anchor)
	// the cursor row is where the widget hangs from: it is the row of the anchor
	if rect.Bottom < 0 || rect.Top > vm.viewport {
		return nil
	}
	rowH := vm.suggestRowHeight()
	origin := s.gutterWidth
	if vm.mm.info.Show && vm.Options().Minimap.Side == "left" {
		origin += vm.mm.info.Width
	}
	viewW, _ := vm.ViewportSize()
	left := origin + rect.Left
	width := suggestWidth
	rows := 1
	if v.State == SuggestOpen || v.State == SuggestFrozen {
		rows = min(len(v.Items), suggestRows)
	} else {
		width = suggestWidth / 2
	}
	// the room below and above the line, as VS Code works it out: a cap of 12 rows, fewer when the room is short
	below := vm.viewport - rect.Bottom - suggestVerticalPadding
	above := rect.Top
	wanted := float64(rows)*rowH + 2
	placeAbove := wanted > below && above > below
	room := below
	if placeAbove {
		room = above
	}
	if wanted > room {
		rows = max(int(math.Floor((room-2)/rowH)), 1)
		wanted = float64(rows)*rowH + 2
	}
	vm.SetSuggestRows(rows)
	if viewW > 0 {
		left = math.Max(math.Min(left, origin+viewW-width), 0)
	}
	list := s.suggestList(bc, vm, colors, v, rows, width)
	pos := widgets.PositionedOpts{Left: foundation.Some(left)}
	top := math.Max(rect.Bottom, 0)
	if placeAbove {
		pos.Bottom = foundation.Some(vm.viewport - rect.Top)
		top = math.Max(rect.Top-wanted, 0)
	} else {
		pos.Top = foundation.Some(top)
	}
	children := []widgets.Widget{widgets.NewPositioned(list, pos)}
	if v.Details != nil {
		children = append(children, s.suggestDetailsPane(bc, vm, colors, v, left, top, wanted, width, placeAbove, origin, viewW))
	}
	return widgets.NewStack(children, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// suggestBox is a widget in the frame of the suggest widget.
func suggestBox(colors kit.Colors, child widgets.Widget, width, height float64) widgets.Widget {
	return widgets.NewContainer(widgets.NewClipRRect(child, widgets.ClipRRectOpts{BorderRadius: painting.NewBorderRadiusCircular(suggestRadius)}),
		widgets.ContainerOpts{
			Width: foundation.Some(width), Height: foundation.Some(height),
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color:        foundation.Some(colors.EditorSuggestWidgetBackground),
				Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.EditorSuggestWidgetBorder), Width: foundation.Some(1.0)}),
				BorderRadius: painting.NewBorderRadiusCircular(suggestRadius),
				BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: suggestShadowBlur})},
			}),
		})
}

// suggestList is the box with the rows, or with the message of a state that has no rows.
func (s *editorState) suggestList(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors, v *SuggestView, rows int, width float64) widgets.Widget {
	rowH := vm.suggestRowHeight()
	size := vm.suggestFontSize()
	fg := colors.EditorSuggestWidgetForeground
	if v.State == SuggestLoading || v.State == SuggestEmpty {
		text := "Loading..."
		if v.State == SuggestEmpty {
			text = "No suggestions."
		}
		msg := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(suggestMessageIndent, 0, 0, 0),
			widgets.NewAlign(widgets.NewText(text, widgets.TextOpts{Style: vm.suggestStyle(fg, size), MaxLines: foundation.Some(1)}),
				widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft}))
		return suggestBox(colors, msg, width, rowH+2)
	}
	top := min(max(v.Top, 0), max(len(v.Items)-rows, 0))
	var children []widgets.Widget
	for i := top; i < min(top+rows, len(v.Items)); i++ {
		children = append(children, s.suggestRow(bc, vm, colors, v, i, width-2))
	}
	list := widgets.NewColumn(children, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})
	var body widgets.Widget = list
	if len(v.Items) > rows {
		body = widgets.NewStack([]widgets.Widget{list, suggestScrollbar(colors, len(v.Items), rows, top, rowH*float64(rows))},
			widgets.StackOpts{Fit: rendering.StackFitPassthrough})
	}
	box := suggestBox(colors, body, width, float64(rows)*rowH+2)
	return widgets.NewMouseRegion(
		widgets.NewListener(box, widgets.ListenerOpts{OnPointerSignal: func(ev gestures.PointerSignalEvent) {
			if scroll, ok := ev.(*gestures.PointerScrollEvent); ok && scroll.ScrollDelta().Dy != 0 {
				step := 1
				if scroll.ScrollDelta().Dy < 0 {
					step = -1
				}
				vm.SuggestScroll(step * 3)
			}
		}}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Basic})
}

// suggestScrollbar is the thumb at the right edge of the list, when the list has more rows than it shows.
func suggestScrollbar(colors kit.Colors, total, rows, top int, height float64) widgets.Widget {
	const w = 6.0
	thumbH := math.Max(height*float64(rows)/float64(total), 12)
	y := (height - thumbH) * float64(top) / float64(max(total-rows, 1))
	return widgets.NewPositioned(
		widgets.NewSizedBox(widgets.NewColoredBox(colors.ScrollbarSliderBackground, nil), widgets.SizedBoxOpts{Width: foundation.Some(w), Height: foundation.Some(thumbH)}),
		widgets.PositionedOpts{Right: foundation.Some(0.0), Top: foundation.Some(y)})
}

// symbolIcon is the icon of the kind of an item and its color.
func symbolIcon(colors kit.Colors, kind langfeatures.CompletionKind) (code int, color ui.Color) {
	ic, ok := suggestCodicons[kind]
	if !ok {
		ic = suggestCodicons[10] // a kind without an icon is a property
	}
	return ic.code, colors.SymbolIconColor(ic.color)
}

// suggestRow is the row of the item i: its icon, its label with the characters that matched in bold, the signature after it
// and the detail at the right.
func (s *editorState) suggestRow(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors, v *SuggestView, i int, width float64) widgets.Widget {
	it := v.Items[i]
	focused := i == v.Focus
	rowH := vm.suggestRowHeight()
	size := vm.suggestFontSize()
	fg, hl := colors.EditorSuggestWidgetForeground, colors.EditorSuggestWidgetHighlightForeground
	bg := ui.Color{}
	outline := ui.Color{}
	_, iconFg := symbolIcon(colors, it.Kind)
	if focused {
		fg, hl, bg, outline = colors.EditorSuggestWidgetSelectedForeground, colors.EditorSuggestWidgetFocusHighlightForeground, colors.EditorSuggestWidgetSelectedBackground, colors.EditorSuggestWidgetFocusOutline
		iconFg = colors.EditorSuggestWidgetSelectedIconForeground
	}
	base := vm.suggestStyle(fg, size)
	var spans []painting.InlineSpan
	label := it.Label
	if it.Deprecated {
		base = base.CopyWith(painting.TextStyleOpts{Decoration: foundation.Some(ui.TextDecorationLineThrough), Color: foundation.Some(fg.WithOpacity(fg.Opacity() * 0.66))})
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(label)}))
	} else {
		spans = append(spans, labelSpans(label, it.Score.Positions, base, hl, v.State != SuggestFrozen)...)
	}
	if it.LabelDetail != "" {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(stripNewLines(it.LabelDetail)),
			Style: base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(fg.WithOpacity(fg.Opacity() * 0.6))})}))
	}
	left := widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: base, Children: spans}),
		widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})

	// the detail at the right: the description of the label details, always; the detail of an item with a plain label, on the
	// focused row while the details pane is closed
	stringLabel := it.LabelDetail == "" && it.LabelDescription == ""
	detail := stripNewLines(it.LabelDescription)
	if stringLabel {
		detail = ""
		if focused && v.Details == nil {
			detail = stripNewLines(it.Detail)
		}
	}
	if !v.Options.ShowInlineDetails {
		detail = ""
	}
	parts := []widgets.Widget{widgets.NewSizedBox(nil, widgetsWidth(suggestRowPadLeft))}
	if v.Options.ShowIcons {
		code, _ := symbolIcon(colors, it.Kind)
		parts = append(parts,
			widgets.NewSizedBox(widgets.NewCenter(widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(suggestIconSize), Color: foundation.Some(iconFg)}), widgets.CenterOpts{}),
				widgets.SizedBoxOpts{Width: foundation.Some(rowH), Height: foundation.Some(rowH)}),
			widgets.NewSizedBox(nil, widgetsWidth(4)))
	}
	parts = append(parts, widgets.NewExpanded(left))
	if detail != "" {
		dstyle := vm.suggestStyle(fg, size*0.85)
		parts = append(parts, widgets.NewSizedBox(nil, widgetsWidth(size*1.1)),
			widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(width * suggestDetailLabelRatio)}),
				widgets.NewText(detail, widgets.TextOpts{Style: dstyle, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})))
	}
	if focused && v.Details == nil && canExpand(it) {
		parts = append(parts, widgets.NewMouseRegion(
			widgets.NewGestureDetector(widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconInfo), widgets.IconOpts{Size: foundation.Some(14.0), Color: foundation.Some(fg)}),
				widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: vm.SuggestToggleDetails}),
			widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click}))
	}
	parts = append(parts, widgets.NewSizedBox(nil, widgetsWidth(suggestRowPadRight)))
	row := widgets.NewContainer(
		widgets.NewRow(parts, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.ContainerOpts{Width: foundation.Some(width), Height: foundation.Some(rowH),
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(bg)}),
			// the outline is painted over the row, not around it: a border would take 2 px of the line and clip the descenders
			ForegroundDecoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Border: painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(outline), Width: foundation.Some(1.0)}),
			})})
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { vm.SuggestAccept(i) }}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click, OnEnter: func(*gestures.PointerEnterEvent) {}})
}

func widgetsWidth(w float64) widgets.SizedBoxOpts {
	return widgets.SizedBoxOpts{Width: foundation.Some(w)}
}

// stripNewLines joins the lines of a text.
func stripNewLines(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.NewReplacer("\r\n", "", "\r", "", "\n", "").Replace(s)
}

// labelSpans splits the label into the runs of characters the filter matched (bold, in the highlight color) and the rest.
func labelSpans(label string, positions []int, base *painting.TextStyle, hl ui.Color, bold bool) []painting.InlineSpan {
	if len(positions) == 0 {
		return []painting.InlineSpan{painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(label)})}
	}
	opts := painting.TextStyleOpts{Color: foundation.Some(hl)}
	if bold {
		opts.FontWeight = ui.FontWeightBold
	}
	marked := base.CopyWith(opts)
	matched := make(map[int]bool, len(positions))
	for _, p := range positions {
		matched[p] = true
	}
	var spans []painting.InlineSpan
	var run strings.Builder
	runMarked := false
	flush := func() {
		if run.Len() == 0 {
			return
		}
		o := painting.TextSpanOpts{Text: foundation.Some(run.String())}
		if runMarked {
			o.Style = marked
		}
		spans = append(spans, painting.NewTextSpan(o))
		run.Reset()
	}
	i := 0
	for off := 0; off < len(label); i++ {
		r, w := utf8.DecodeRuneInString(label[off:])
		if m := matched[i]; m != runMarked {
			flush()
			runMarked = m
		}
		run.WriteRune(r)
		off += w
	}
	flush()
	return spans
}

// suggestDetailsPane is the pane with the detail and the documentation of the focused item. It stands at the right of the list,
// at the left when there is no room at the right, aligned with the top of the list (with its bottom when the list is above the
// line).
func (s *editorState) suggestDetailsPane(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors, v *SuggestView, left, top, listHeight, listWidth float64, above bool, origin, viewW float64) widgets.Widget {
	d := v.Details
	size := vm.suggestFontSize()
	rowH := vm.suggestRowHeight()
	var paneW float64
	right := left + listWidth
	avail := viewW + origin - right
	x := right - 1
	if avail < suggestDetailsMinWidth {
		if lw := left; lw >= suggestDetailsMinWidth {
			paneW = math.Min(suggestDetailsWidth, lw-1)
			x = left - paneW + 1
		} else {
			paneW = math.Max(suggestDetailsMinWidth, avail)
		}
	} else {
		paneW = math.Min(suggestDetailsWidth, avail)
	}
	fg := colors.EditorSuggestWidgetForeground
	var children []widgets.Widget
	textW := paneW - 2
	height := 2.0
	if d.Detail != "" {
		lines := estimateLines(d.Detail, textW-10, size*0.6)
		children = append(children, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(5, 4, 24, 4),
			widgets.NewText(d.Detail, widgets.TextOpts{Style: vm.suggestStyle(fg.WithOpacity(fg.Opacity()*0.7), size)})))
		height += float64(lines)*rowH + 8
	}
	switch {
	case d.Loading && d.Documentation == nil:
		children = append(children, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(5, 0, 5, 4), widgets.NewText("Loading...", widgets.TextOpts{Style: vm.suggestStyle(fg, size)})))
		height += rowH + 4
	case d.Documentation != nil && d.Documentation.Value != "":
		docs := d.Documentation
		lines := estimateLines(docs.Value, textW-16, 13*0.6)
		dh := float64(lines)*13*1.4 + 16
		var body widgets.Widget
		if docs.Markdown {
			body = markdown.NewView(docs.Value, markdown.Options{Compact: true, OnLink: func(dest string) {
				if vm.openURI != nil {
					vm.openURI(dest)
				}
			}})
		} else {
			body = widgets.NewPadding(painting.NewEdgeInsetsAll(8), widgets.NewText(docs.Value, widgets.TextOpts{Style: vm.suggestStyle(fg, 13)}))
		}
		children = append(children, widgets.NewExpanded(body))
		height += dh
	}
	maxH := math.Max(listHeight, 2*rowH)
	if _, vh := vm.ViewportSize(); vh > 0 {
		maxH = math.Max(vh-top-suggestVerticalPadding, 2*rowH)
		if above {
			maxH = math.Max(top+listHeight, 2*rowH)
		}
	}
	height = math.Min(math.Max(height, 2*rowH), math.Max(maxH, listHeight))
	column := widgets.NewColumn(children, widgetsColumnStretch())
	pane := suggestBox(colors, column, paneW, height)
	pos := widgets.PositionedOpts{Left: foundation.Some(math.Max(x, 0))}
	if above {
		pos.Top = foundation.Some(math.Max(top+listHeight-height, 0))
	} else {
		pos.Top = foundation.Some(top)
	}
	return widgets.NewPositioned(pane, pos)
}

func widgetsColumnStretch() widgets.ColumnOpts {
	return widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMax}
}

// estimateLines is the number of lines a text takes in a width, counting the characters of an average width.
func estimateLines(text string, width, charWidth float64) int {
	perLine := math.Max(math.Floor(width/charWidth), 1)
	lines := 0
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		lines += max(int(math.Ceil(float64(utf8.RuneCountInString(l))/perLine)), 1)
	}
	return lines
}
