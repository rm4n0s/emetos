package markdown

import (
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Options tune a View.
type Options struct {
	// Tokenize tokenizes the text of a code block for the highlighting of its language (the id or the name of the info string
	// of the fence); false leaves the block plain. Nil highlights nothing.
	Tokenize func(language, text string) ([][]extension.Token, bool)
	// OnLink is called with the destination of a link that was clicked.
	OnLink func(dest string)
	// ResolveImage gives the path of a local file for the destination of an image, or "" when the image is not local. Nil
	// shows no image; the alt text is shown instead.
	ResolveImage func(dest string) string
	// Controller, when set, scrolls the view to a line of the source.
	Controller *Controller
}

// Controller moves a View: it scrolls to the block that a line of the source belongs to.
type Controller struct {
	scroll   kit.ScrollHandle
	viewport kit.Bounds
	blocks   []placedBlock
	// pending is the line a scroll waits for, because the view was not laid out yet.
	pending    int
	hasPending bool
}

// placedBlock is a block of the last build and where it is.
type placedBlock struct {
	span   Span
	bounds *kit.Bounds
}

// NewController makes a controller for a View.
func NewController() *Controller { return &Controller{} }

// ScrollToLine scrolls the view so that the block the line (counted from 0) belongs to is at the top: the last block that
// starts at or before the line. It waits for the view to be laid out when it is not yet.
func (c *Controller) ScrollToLine(line int) {
	view, ok := c.viewport.Rect()
	if !ok || len(c.blocks) == 0 {
		c.pending, c.hasPending = line, true
		return
	}
	c.hasPending = false
	var target *placedBlock
	for i := range c.blocks {
		if c.blocks[i].span.StartLine <= line {
			target = &c.blocks[i]
		}
	}
	if target == nil {
		c.scroll.ScrollTo(0, 0)
		return
	}
	rect, ok := target.bounds.Rect()
	if !ok {
		c.pending, c.hasPending = line, true
		return
	}
	_, top := c.scroll.Position()
	c.scroll.ScrollTo(0, max(top+rect.Top-view.Top, 0))
}

// TopLine is the first line of the source of the block at the top of the view.
func (c *Controller) TopLine() int {
	view, ok := c.viewport.Rect()
	if !ok {
		return 0
	}
	line := 0
	for _, b := range c.blocks {
		rect, ok := b.bounds.Rect()
		if !ok || rect.Top > view.Top+1 {
			break
		}
		line = b.span.StartLine
	}
	return line
}

// view is the widget of NewView.
type view struct {
	widgets.StatefulWidget
	src  string
	opts Options
}

// NewView makes the widget that shows the Markdown text src. The view keeps its scroll position when it is built again with
// another text, and scrolls with the Controller of the options.
func NewView(src string, opts Options) widgets.Widget {
	return &view{StatefulWidget: widgets.NewStatefulWidgetBase(), src: src, opts: opts}
}

// CreateState implements widgets.StatefulWidget.
func (v *view) CreateState() widgets.State { return &viewState{} }

// viewState is the state of a View: the blocks of the text, the highlighting of the code blocks and the recognizers of the
// links.
type viewState struct {
	widgets.StateOf[*view]
	src    string
	blocks []Block
	parsed bool
	// tokens caches the tokens of the code blocks by language and text.
	tokens map[string][][]extension.Token
	// taps are the recognizers of the links of the last build.
	taps []gestures.GestureRecognizer
	ctl  *Controller
}

// Dispose releases the recognizers.
func (s *viewState) Dispose() { s.releaseTaps() }

func (s *viewState) releaseTaps() {
	for _, t := range s.taps {
		t.Dispose()
	}
	s.taps = nil
}

// Build implements widgets.State.
func (s *viewState) Build(bc widgets.BuildContext) widgets.Widget {
	v := s.Widget()
	if !s.parsed || s.src != v.src {
		s.src, s.blocks, s.parsed = v.src, Parse(v.src), true
		s.tokens = map[string][][]extension.Token{}
	}
	s.releaseTaps()
	s.ctl = v.opts.Controller
	if s.ctl == nil {
		s.ctl = NewController()
	}
	r := &renderer{s: s, bc: bc, colors: kit.ThemeOf(bc), opts: v.opts}
	r.base = kit.TextStyleOf(bc, bodySize, r.colors.EditorForeground).CopyWith(painting.TextStyleOpts{Height: foundation.Some(bodyLineHeight)})
	r.mono = widgets.WidgetsBindingOf(bc).PlatformDispatcher()
	family, _ := r.mono.MonospaceFont()
	if family == "" {
		family = "monospace" // the platform does not say: the generic family (SPEC D14)
	}
	r.monoFamily = family
	s.ctl.blocks = s.ctl.blocks[:0]
	children := make([]widgets.Widget, 0, len(s.blocks))
	for i, b := range s.blocks {
		bounds := &kit.Bounds{}
		s.ctl.blocks = append(s.ctl.blocks, placedBlock{span: b.Span(), bounds: bounds})
		children = append(children, kit.NewBoundsProbe(r.block(b, i == 0), bounds))
	}
	if s.ctl.hasPending {
		ctl := s.ctl
		widgets.WidgetsBindingOf(bc).Scheduler().AddPostFrameCallback(func(time.Duration) {
			if ctl.hasPending {
				ctl.ScrollToLine(ctl.pending)
			}
		})
	}
	column := widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	body := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(pagePadding, 16, pagePadding, 40), column)
	scroll := kit.NewScrollableElement(body, kit.ScrollableElementOpts{Axes: kit.ScrollAxisVertical, Handle: &s.ctl.scroll})
	return widgets.NewColoredBox(r.colors.EditorBackground, kit.NewBoundsProbe(scroll, &s.ctl.viewport))
}

// The metrics of the preview (VS Code's markdown.css).
const (
	pagePadding    = 26.0
	bodySize       = 14.0
	bodyLineHeight = 1.6
	codeSize       = 13.0
)

// renderer builds the widgets of one build of a View.
type renderer struct {
	s          *viewState
	bc         widgets.BuildContext
	colors     kit.Colors
	opts       Options
	base       *painting.TextStyle
	mono       *ui.PlatformDispatcher
	monoFamily string
}

// block is the widget of a block. first says that it is the first block of its container, which has no space above it.
func (r *renderer) block(b Block, first bool) widgets.Widget {
	var w widgets.Widget
	switch b := b.(type) {
	case Heading:
		w = r.heading(b)
	case Paragraph:
		w = r.inlines(b.Inlines, r.base)
	case CodeBlock:
		w = r.codeBlock(b)
	case Quote:
		w = r.quote(b)
	case List:
		w = r.list(b)
	case Table:
		w = r.table(b)
	case Rule:
		w = widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 8, 0, 8), widgets.NewColoredBox(r.colors.TextSeparatorForeground, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(2.0)})))
	case HTMLBlock:
		w = widgets.NewText(strings.TrimRight(b.Text, "\n"), widgets.TextOpts{Style: r.monoStyle(r.colors.DescriptionForeground)})
	default:
		w = widgets.NewSizedBox(nil)
	}
	top := 0.0
	if !first {
		top = 10
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, top, 0, 0), w)
}

// headingSizes are the sizes of the headings 1 to 6, in em (VS Code's markdown.css).
var headingSizes = [...]float64{2, 1.5, 1.25, 1, 0.875, 0.85}

func (r *renderer) heading(h Heading) widgets.Widget {
	level := min(max(h.Level, 1), 6)
	style := r.base.CopyWith(painting.TextStyleOpts{
		FontSize: foundation.Some(bodySize * headingSizes[level-1]), FontWeight: ui.FontWeightW600,
	})
	w := r.inlines(h.Inlines, style)
	top := 14.0
	if level <= 2 {
		w = widgets.NewContainer(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 0, 4), w), widgets.ContainerOpts{
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Border: painting.Border{
				Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(r.colors.TextSeparatorForeground), Width: foundation.Some(1.0)}),
			}}),
		})
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, top, 0, 2), w)
}

// monoStyle is the style of code.
func (r *renderer) monoStyle(color ui.Color) *painting.TextStyle {
	return painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: r.monoFamily, FontSize: foundation.Some(codeSize), Color: foundation.Some(color),
		Height: foundation.Some(1.4),
	})
}

// codeBlock is a block of code on its own background, highlighted when the language has a grammar.
func (r *renderer) codeBlock(b CodeBlock) widgets.Widget {
	b.Text = expandTabs(b.Text, 4)
	fg := r.colors.EditorForeground
	style := r.monoStyle(fg)
	var span painting.InlineSpan = painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(b.Text), Style: style})
	if lines, ok := r.tokenize(b); ok {
		span = r.highlighted(b.Text, lines, style, fg)
	}
	return widgets.NewContainer(widgets.NewPadding(painting.NewEdgeInsetsAll(12), widgets.NewRichText(span)), widgets.ContainerOpts{
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color: foundation.Some(r.colors.TextCodeBlockBackground), BorderRadius: painting.NewBorderRadiusCircular(3),
		}),
	})
}

// tokenize asks for the tokens of a block, once per text and language.
func (r *renderer) tokenize(b CodeBlock) ([][]extension.Token, bool) {
	if r.opts.Tokenize == nil || b.Info == "" || b.Text == "" {
		return nil, false
	}
	key := b.Info + "\x00" + b.Text
	if lines, ok := r.s.tokens[key]; ok {
		return lines, lines != nil
	}
	lines, ok := r.opts.Tokenize(b.Info, b.Text)
	if !ok {
		lines = nil
	}
	r.s.tokens[key] = lines
	return lines, ok
}

// highlighted is the text of a code block as spans in the colors of the theme for the tokens of its lines.
func (r *renderer) highlighted(text string, lines [][]extension.Token, style *painting.TextStyle, fg ui.Color) painting.InlineSpan {
	var spans []painting.InlineSpan
	src := strings.Split(text, "\n")
	for i, line := range src {
		if i > 0 {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("\n")}))
		}
		var toks []extension.Token
		if i < len(lines) {
			toks = lines[i]
		}
		at := 0
		for _, t := range toks {
			start, end := min(max(t.Start, at), len(line)), min(t.End, len(line))
			if start > at {
				spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[at:start])}))
			}
			if end > start {
				look := kit.TokenLookOf(r.bc, t.Scopes, fg)
				ts := painting.TextStyleOpts{Color: foundation.Some(look.Color)}
				if look.Bold {
					ts.FontWeight = ui.FontWeightBold
				}
				if look.Italic {
					ts.FontStyle = ui.FontStyleItalic
				}
				spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[start:end]), Style: style.CopyWith(ts)}))
			}
			at = max(at, end)
		}
		if at < len(line) {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[at:])}))
		}
	}
	return painting.NewTextSpan(painting.TextSpanOpts{Style: style, Children: spans})
}

// quote is a block quote: a bar on the left and the blocks on their own background.
func (r *renderer) quote(q Quote) widgets.Widget {
	bar := painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(r.colors.TextBlockQuoteBorder), Width: foundation.Some(5.0)})
	return widgets.NewContainer(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(14, 4, 12, 4), r.blocks(q.Blocks)), widgets.ContainerOpts{
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color: foundation.Some(r.colors.TextBlockQuoteBackground), Border: painting.Border{Left: bar},
		}),
	})
}

// blocks stacks the blocks of a container.
func (r *renderer) blocks(bs []Block) widgets.Widget {
	children := make([]widgets.Widget, len(bs))
	for i, b := range bs {
		children[i] = r.block(b, i == 0)
	}
	return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// list is a list: a marker and the blocks of each item.
func (r *renderer) list(l List) widgets.Widget {
	rows := make([]widgets.Widget, len(l.Items))
	for i, it := range l.Items {
		var marker widgets.Widget
		switch {
		case it.Task:
			code := fonts.CodiconCheck
			if !it.Checked {
				code = fonts.CodiconCircleLarge
			}
			marker = widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 3, 0, 0), widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(bodySize + 2), Color: foundation.Some(r.colors.EditorForeground)}))
		case l.Ordered:
			marker = widgets.NewText(strconv.Itoa(l.Start+i)+".", widgets.TextOpts{Style: r.base})
		default:
			marker = widgets.NewText("•", widgets.TextOpts{Style: r.base})
		}
		content := r.blocks(it.Blocks)
		if l.Tight && len(it.Blocks) > 0 {
			content = r.tightBlocks(it.Blocks)
		}
		rows[i] = widgets.NewRow([]widgets.Widget{
			widgets.NewSizedBox(widgets.NewAlign(marker, widgets.AlignOpts{Alignment: painting.AlignmentTopRight}), widgets.SizedBoxOpts{Width: foundation.Some(26.0)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
			widgets.NewExpanded(content),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(8, 0, 0, 0), widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
}

// tightBlocks stacks the blocks of an item of a tight list: no space between its blocks.
func (r *renderer) tightBlocks(bs []Block) widgets.Widget {
	children := make([]widgets.Widget, len(bs))
	for i, b := range bs {
		children[i] = r.block(b, true)
	}
	return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// table is a table with borders, a bold header and the alignment of each column.
func (r *renderer) table(t Table) widgets.Widget {
	border := painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(r.colors.TextSeparatorForeground), Width: foundation.Some(1.0)})
	cell := func(c TableCell, col int, header bool) widgets.Widget {
		style := r.base
		if header {
			style = style.CopyWith(painting.TextStyleOpts{FontWeight: ui.FontWeightBold})
		}
		align := ui.TextAlignStart
		if col < len(t.Align) {
			switch t.Align[col] {
			case AlignCenter:
				align = ui.TextAlignCenter
			case AlignRight:
				align = ui.TextAlignRight
			}
		}
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 5, 12, 5), r.inlinesAligned(c.Inlines, style, align))
	}
	var rows []widgets.TableRow
	header := make([]widgets.Widget, len(t.Header))
	for i, c := range t.Header {
		header[i] = cell(c, i, true)
	}
	rows = append(rows, widgets.TableRow{
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(r.colors.TextPreformatBackground)}),
		Children:   header,
	})
	for _, row := range t.Rows {
		cells := make([]widgets.Widget, len(t.Header))
		for i := range cells {
			if i < len(row) {
				cells[i] = cell(row[i], i, false)
			} else {
				cells[i] = widgets.NewSizedBox(nil)
			}
		}
		rows = append(rows, widgets.TableRow{Children: cells})
	}
	tbl := widgets.NewTable(rows, widgets.TableOpts{
		DefaultColumnWidth: rendering.NewIntrinsicColumnWidth(),
		Border:             &rendering.TableBorder{Top: border, Right: border, Bottom: border, Left: border, HorizontalInside: border, VerticalInside: border},
	})
	return widgets.NewAlign(tbl, widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})
}

// inlines is the text of inline nodes in a style; images in it break it into text and images.
func (r *renderer) inlines(in []Inline, style *painting.TextStyle) widgets.Widget {
	return r.inlinesAligned(in, style, ui.TextAlignStart)
}

func (r *renderer) inlinesAligned(in []Inline, style *painting.TextStyle, align ui.TextAlign) widgets.Widget {
	var parts []widgets.Widget
	var run []Inline
	flush := func() {
		if len(run) == 0 {
			return
		}
		spans := r.spans(run, style, nil)
		run = nil
		if len(spans) == 0 {
			return
		}
		parts = append(parts, widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: style, Children: spans}), widgets.RichTextOpts{TextAlign: align}))
	}
	for _, n := range in {
		if img, ok := n.(Image); ok {
			flush()
			parts = append(parts, r.image(img))
			continue
		}
		run = append(run, n)
	}
	flush()
	switch len(parts) {
	case 0:
		return widgets.NewSizedBox(nil)
	case 1:
		return parts[0]
	}
	return widgets.NewColumn(parts, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
}

// spans turns inline nodes into spans.
//
// tap is the recognizer of the link the nodes are in, or nil: it goes on every span that has text, because a span answers
// only for itself.
func (r *renderer) spans(in []Inline, style *painting.TextStyle, tap gestures.GestureRecognizer) []painting.InlineSpan {
	var out []painting.InlineSpan
	text := func(s string, st *painting.TextStyle) {
		out = append(out, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(s), Style: st, Recognizer: tap}))
	}
	for _, n := range in {
		switch n := n.(type) {
		case Text:
			text(n.S, nil)
		case SoftBreak:
			text(" ", nil)
		case HardBreak:
			text("\n", nil)
		case Emph:
			out = append(out, painting.NewTextSpan(painting.TextSpanOpts{Style: style.CopyWith(painting.TextStyleOpts{FontStyle: ui.FontStyleItalic}), Children: r.spans(n.Children, style, tap)}))
		case Strong:
			out = append(out, painting.NewTextSpan(painting.TextSpanOpts{Style: style.CopyWith(painting.TextStyleOpts{FontWeight: ui.FontWeightBold}), Children: r.spans(n.Children, style, tap)}))
		case Strike:
			out = append(out, painting.NewTextSpan(painting.TextSpanOpts{Style: style.CopyWith(painting.TextStyleOpts{Decoration: foundation.Some(ui.TextDecorationLineThrough)}), Children: r.spans(n.Children, style, tap)}))
		case Code:
			code := r.monoStyle(r.colors.TextPreformatForeground).CopyWith(painting.TextStyleOpts{BackgroundColor: foundation.Some(r.colors.TextPreformatBackground)})
			text(n.S, code)
		case HTMLInline:
			text(n.S, r.monoStyle(r.colors.DescriptionForeground))
		case Link:
			out = append(out, r.link(n, style))
		}
	}
	return out
}

// link is a link as a span in the link color that calls OnLink when it is clicked.
func (r *renderer) link(l Link, style *painting.TextStyle) painting.InlineSpan {
	tap := gestures.NewTapGestureRecognizer(widgets.WidgetsBindingOf(r.bc).Gestures())
	dest := l.Dest
	tap.OnTap = func() {
		if r.opts.OnLink != nil {
			r.opts.OnLink(dest)
		}
	}
	r.s.taps = append(r.s.taps, tap)
	linkStyle := style.CopyWith(painting.TextStyleOpts{Color: foundation.Some(r.colors.TextLinkForeground), Decoration: foundation.Some(ui.TextDecorationUnderline)})
	return painting.NewTextSpan(painting.TextSpanOpts{Style: linkStyle, Children: r.spans(l.Children, linkStyle, tap)})
}

// image is a local image, or the alt text of one that is not.
func (r *renderer) image(img Image) widgets.Widget {
	alt := strings.TrimSpace(img.Alt)
	if alt == "" {
		alt = img.Dest
	}
	fallback := widgets.NewText("["+alt+"]", widgets.TextOpts{Style: r.base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(r.colors.DescriptionForeground)})})
	if r.opts.ResolveImage == nil {
		return fallback
	}
	path := r.opts.ResolveImage(img.Dest)
	if path == "" {
		return fallback
	}
	return widgets.NewAlign(widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(800.0), MaxHeight: foundation.Some(600.0)}),
		widgets.NewImageFile(path, widgets.ImageFileOpts{ImageOpts: widgets.ImageOpts{
			Fit: painting.BoxFitScaleDown, SemanticLabel: foundation.Some(alt),
			ErrorBuilder: func(widgets.BuildContext, error) widgets.Widget { return fallback },
		}})), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})
}

// expandTabs replaces the tabs of a text by spaces up to the next multiple of size columns, line by line: the font may have
// no glyph for a tab, and a tab is not a fixed width in a paragraph.
func expandTabs(text string, size int) string {
	if !strings.Contains(text, "\t") {
		return text
	}
	var b strings.Builder
	col := 0
	for _, c := range text {
		switch c {
		case '\t':
			n := size - col%size
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case '\n':
			b.WriteRune(c)
			col = 0
		default:
			b.WriteRune(c)
			col++
		}
	}
	return b.String()
}
