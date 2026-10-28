package kit

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

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The list of the suggest widget (SPEC §9.12 "As built (step 12.5)", §10.25 "Terminal IntelliSense"): the rows with an icon, the
// label with the characters that matched in bold, the detail at the right and the scrollbar, in the frame of the widget. The
// editor's suggest widget and the terminal's draw their lists with it; where the list stands is up to each.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestWidgetRenderer.ts and media/suggest.css

// Metrics of the widget (suggest.css, suggestWidget.ts).
const (
	// SuggestWidth is the width of the list.
	SuggestWidth = 430.0
	// SuggestRadius is the radius of the corners of the widget's frame.
	SuggestRadius = 4.0
	// SuggestVerticalPadding is the room kept between the list and the edge of the place it hangs in.
	SuggestVerticalPadding = 22.0

	suggestIconSize        = 16.0
	suggestRowPadLeft      = 2.0
	suggestRowPadRight     = 10.0
	suggestShadowBlur      = 8.0
	suggestMessageIndent   = 22.0
	suggestDetailLabelRate = 0.7 // the share of a row the detail may take (max-width: 70%)
)

// suggestKindIcons maps a kind of item (the numbers of the LSP's CompletionItemKind) to the codepoint of its icon and the name
// of its color.
var suggestKindIcons = map[int]struct {
	code  int
	color string
}{
	1: {0xEA93, "text"}, 2: {0xEA8C, "method"}, 3: {0xEA8C, "function"}, 4: {0xEA8C, "constructor"}, 5: {0xEB5F, "field"},
	6: {0xEA88, "variable"}, 7: {0xEB5B, "class"}, 8: {0xEB61, "interface"}, 9: {0xEA8B, "module"}, 10: {0xEB65, "property"},
	11: {0xEA96, "unit"}, 12: {0xEA95, "enumerator"}, 13: {0xEA95, "enumerator"}, 14: {0xEB62, "keyword"}, 15: {0xEB66, "snippet"},
	16: {0xEB5C, "color"}, 17: {0xEA7B, "file"}, 18: {0xEA94, "reference"}, 19: {0xEA83, "folder"}, 20: {0xEB5E, "enumeratorMember"},
	21: {0xEB5D, "constant"}, 22: {0xEA91, "struct"}, 23: {0xEA86, "event"}, 24: {0xEB64, "operator"}, 25: {0xEA92, "typeParameter"},
}

// SuggestRow is one row of the list.
type SuggestRow struct {
	// Label is the text of the row; Positions are the characters of it (in runes) the filter matched, drawn in bold.
	Label     string
	Positions []int
	// LabelDetail follows the label, dimmed; LabelDescription is the detail of a row with a label made of parts. Detail is the
	// detail of a row with a plain label, shown on the focused row while the details pane is closed.
	LabelDetail, LabelDescription, Detail string
	// Kind is the kind of the item (the numbers of the LSP's CompletionItemKind), which gives the icon and its color. Icon is the
	// name of a Codicon and IconColor the name of a symbol icon color ("method", "file"), which replace them when set.
	Kind      int
	Icon      string
	IconColor string
	// Deprecated draws the label struck through. CanExpand says that the row has a details pane to open.
	Deprecated, CanExpand bool
}

// SuggestListOpts are the options of NewSuggestList.
type SuggestListOpts struct {
	// Rows are the rows shown (the visible part of the items); Focus is the index of the focused one among them, or -1.
	Rows  []SuggestRow
	Focus int
	// Total is the number of items there are and Top the index of the first of Rows among them, for the scrollbar.
	Total, Top int
	// Width of the list in logical pixels (SuggestWidth when 0), RowHeight and FontSize of its text.
	Width, RowHeight, FontSize float64
	// DetailShare is the most of a row the detail at the right may take (0 is VS Code's 0.7).
	DetailShare float64
	// FontFamilies are the fonts of the text, the first one the main; FontWeight is the weight (0 for the font's own).
	FontFamilies []string
	FontWeight   int
	// ShowIcons draws the icon of each row, ShowInlineDetails the detail at the right; DetailsOpen says that the details pane
	// is open, so that the focused row shows no detail of its own; Frozen draws the matches without bold.
	ShowIcons, ShowInlineDetails, DetailsOpen, Frozen bool
	// OnAccept is called with the index among Rows of the row that was tapped; OnToggleDetails with the tap on the info icon;
	// OnScroll with the lines the wheel scrolled by (negative is up).
	OnAccept        func(i int)
	OnToggleDetails func()
	OnScroll        func(lines int)
}

// SuggestTextStyle is the text style of the widget: the font at the size, with the line height of a row.
func SuggestTextStyle(o SuggestListOpts, color ui.Color, size float64) *painting.TextStyle {
	family, fallback := "monospace", []string(nil)
	if len(o.FontFamilies) > 0 {
		family, fallback = o.FontFamilies[0], o.FontFamilies[1:]
	}
	opts := painting.TextStyleOpts{
		FontFamily: family, FontFamilyFallback: fallback, FontSize: foundation.Some(size), Color: foundation.Some(color),
		Height: foundation.Some(o.RowHeight / size),
	}
	if o.FontWeight > 0 {
		opts.FontWeight = ui.FontWeight(o.FontWeight)
	}
	return painting.NewTextStyle(opts)
}

// SuggestBox is the frame of the widget around a child: the background, the border, the rounded corners and the shadow.
func SuggestBox(colors Colors, child widgets.Widget, width, height float64) widgets.Widget {
	return widgets.NewContainer(widgets.NewClipRRect(child, widgets.ClipRRectOpts{BorderRadius: painting.NewBorderRadiusCircular(SuggestRadius)}),
		widgets.ContainerOpts{
			Width: foundation.Some(width), Height: foundation.Some(height),
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color:        foundation.Some(colors.EditorSuggestWidgetBackground),
				Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.EditorSuggestWidgetBorder), Width: foundation.Some(1.0)}),
				BorderRadius: painting.NewBorderRadiusCircular(SuggestRadius),
				BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: suggestShadowBlur})},
			}),
		})
}

// NewSuggestMessage is the box with a message in place of rows ("Loading...", "No suggestions.").
func NewSuggestMessage(colors Colors, text string, o SuggestListOpts) widgets.Widget {
	width := o.Width
	if width <= 0 {
		width = SuggestWidth
	}
	msg := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(suggestMessageIndent, 0, 0, 0),
		widgets.NewAlign(widgets.NewText(text, widgets.TextOpts{Style: SuggestTextStyle(o, colors.EditorSuggestWidgetForeground, o.FontSize), MaxLines: foundation.Some(1)}),
			widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft}))
	return SuggestBox(colors, msg, width, o.RowHeight+2)
}

// NewSuggestList builds the box with the rows. It is as high as its rows (and 2 px of border).
func NewSuggestList(colors Colors, o SuggestListOpts) widgets.Widget {
	width := o.Width
	if width <= 0 {
		width = SuggestWidth
	}
	rows := make([]widgets.Widget, 0, len(o.Rows))
	for i := range o.Rows {
		rows = append(rows, suggestRow(colors, o, i, width-2))
	}
	var body widgets.Widget = widgets.NewColumn(rows, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})
	if o.Total > len(o.Rows) {
		body = widgets.NewStack([]widgets.Widget{body, suggestScrollbar(colors, o.Total, len(o.Rows), o.Top, o.RowHeight*float64(len(o.Rows)))},
			widgets.StackOpts{Fit: rendering.StackFitPassthrough})
	}
	box := SuggestBox(colors, body, width, float64(len(o.Rows))*o.RowHeight+2)
	return widgets.NewMouseRegion(
		widgets.NewListener(box, widgets.ListenerOpts{OnPointerSignal: func(ev gestures.PointerSignalEvent) {
			if scroll, ok := ev.(*gestures.PointerScrollEvent); ok && scroll.ScrollDelta().Dy != 0 && o.OnScroll != nil {
				step := 1
				if scroll.ScrollDelta().Dy < 0 {
					step = -1
				}
				o.OnScroll(step * 3)
			}
		}}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Basic})
}

// suggestScrollbar is the thumb at the right edge of the list, when the list has more rows than it shows.
func suggestScrollbar(colors Colors, total, rows, top int, height float64) widgets.Widget {
	const w = 6.0
	thumbH := math.Max(height*float64(rows)/float64(total), 12)
	y := (height - thumbH) * float64(top) / float64(max(total-rows, 1))
	return widgets.NewPositioned(
		widgets.NewSizedBox(widgets.NewColoredBox(colors.ScrollbarSliderBackground, nil), widgets.SizedBoxOpts{Width: foundation.Some(w), Height: foundation.Some(thumbH)}),
		widgets.PositionedOpts{Right: foundation.Some(0.0), Top: foundation.Some(y)})
}

// SuggestIcon is the codepoint of the icon of a row and the color of it: the icon named by the row, else the one of its kind (a
// property for a kind without one).
func SuggestIcon(colors Colors, r SuggestRow) (code int, color ui.Color) {
	ic, ok := suggestKindIcons[r.Kind]
	if !ok {
		ic = suggestKindIcons[10]
	}
	code, name := ic.code, ic.color
	if r.Icon != "" {
		if c, found := fonts.CodiconByName(r.Icon); found {
			code = c
		}
	}
	if r.IconColor != "" {
		name = r.IconColor
	}
	return code, colors.SymbolIconColor(name)
}

// suggestRow is row i: its icon, its label with the characters that matched in bold, the label detail after it and the
// detail at the right.
func suggestRow(colors Colors, o SuggestListOpts, i int, width float64) widgets.Widget {
	r := o.Rows[i]
	focused := i == o.Focus
	size := o.FontSize
	fg, hl := colors.EditorSuggestWidgetForeground, colors.EditorSuggestWidgetHighlightForeground
	bg := ui.Color{}
	outline := ui.Color{}
	code, iconFg := SuggestIcon(colors, r)
	if focused {
		fg, hl, bg, outline = colors.EditorSuggestWidgetSelectedForeground, colors.EditorSuggestWidgetFocusHighlightForeground, colors.EditorSuggestWidgetSelectedBackground, colors.EditorSuggestWidgetFocusOutline
		iconFg = colors.EditorSuggestWidgetSelectedIconForeground
	}
	base := SuggestTextStyle(o, fg, size)
	var spans []painting.InlineSpan
	if r.Deprecated {
		base = base.CopyWith(painting.TextStyleOpts{Decoration: foundation.Some(ui.TextDecorationLineThrough), Color: foundation.Some(fg.WithOpacity(fg.Opacity() * 0.66))})
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(r.Label)}))
	} else {
		spans = append(spans, suggestLabelSpans(r.Label, r.Positions, base, hl, !o.Frozen)...)
	}
	if r.LabelDetail != "" {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(stripNewLines(r.LabelDetail)),
			Style: base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(fg.WithOpacity(fg.Opacity() * 0.6))})}))
	}
	left := widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: base, Children: spans}),
		widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})

	// the detail at the right: the description of the label details, always; the detail of a row with a plain label, on the
	// focused row while the details pane is closed
	stringLabel := r.LabelDetail == "" && r.LabelDescription == ""
	detail := stripNewLines(r.LabelDescription)
	if stringLabel {
		detail = ""
		if focused && !o.DetailsOpen {
			detail = stripNewLines(r.Detail)
		}
	}
	if !o.ShowInlineDetails {
		detail = ""
	}
	parts := []widgets.Widget{widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(suggestRowPadLeft)})}
	if o.ShowIcons {
		parts = append(parts,
			widgets.NewSizedBox(widgets.NewCenter(widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(suggestIconSize), Color: foundation.Some(iconFg)}), widgets.CenterOpts{}),
				widgets.SizedBoxOpts{Width: foundation.Some(o.RowHeight), Height: foundation.Some(o.RowHeight)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}))
	}
	parts = append(parts, widgets.NewExpanded(left))
	if detail != "" {
		dstyle := SuggestTextStyle(o, fg, size*0.85)
		share := o.DetailShare
		if share <= 0 {
			share = suggestDetailLabelRate
		}
		parts = append(parts, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(size * 1.1)}),
			widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(width * share)}),
				widgets.NewText(detail, widgets.TextOpts{Style: dstyle, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})))
	}
	if focused && !o.DetailsOpen && r.CanExpand {
		parts = append(parts, widgets.NewMouseRegion(
			widgets.NewGestureDetector(widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconInfo), widgets.IconOpts{Size: foundation.Some(14.0), Color: foundation.Some(fg)}),
				widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: o.OnToggleDetails}),
			widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click}))
	}
	parts = append(parts, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(suggestRowPadRight)}))
	row := widgets.NewContainer(
		widgets.NewRow(parts, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.ContainerOpts{Width: foundation.Some(width), Height: foundation.Some(o.RowHeight),
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(bg)}),
			// the outline is painted over the row, not around it: a border would take 2 px of the line and clip the descenders
			ForegroundDecoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Border: painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(outline), Width: foundation.Some(1.0)}),
			})})
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() {
			if o.OnAccept != nil {
				o.OnAccept(i)
			}
		}}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click, OnEnter: func(*gestures.PointerEnterEvent) {}})
}

// stripNewLines joins the lines of a text.
func stripNewLines(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.NewReplacer("\r\n", "", "\r", "", "\n", "").Replace(s)
}

// suggestLabelSpans splits the label into the runs of characters the filter matched (bold, in the highlight color) and the rest.
func suggestLabelSpans(label string, positions []int, base *painting.TextStyle, hl ui.Color, bold bool) []painting.InlineSpan {
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

// SuggestMatcher matches typed words against labels as the suggest widgets do (VS Code's fuzzyScore with a first match that may
// be weak). It keeps the tables of the scorer between calls, so that a caller scoring thousands of labels allocates little. The
// zero value is ready; a matcher is for one goroutine at a time.
type SuggestMatcher struct{ scorer fuzzy.Scorer }

// Match scores a word against a label: the score (higher is better) and the characters of the label (in runes) that matched. ok
// is false when the word does not match; an empty word matches everything with no positions.
func (m *SuggestMatcher) Match(word, label string) (score int, positions []int, ok bool) {
	if word == "" {
		return 0, nil, true
	}
	s, found := m.scorer.FuzzyScore(word, strings.ToLower(word), 0, label, strings.ToLower(label), 0, fuzzy.FuzzyScoreOptions{FirstMatchCanBeWeak: true})
	if !found {
		return 0, nil, false
	}
	return s.Score, s.Positions, true
}
