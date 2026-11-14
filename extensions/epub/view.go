package epub

import (
	"strconv"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/epub/book"
)

// The widget of the reader (SPEC §10.37 "EPUB"): the bar at the top (Contents, the book's title and the chapter's, A− and A+, Previous
// and Next chapter), the Contents panel on the left, and the chapter in a column at most 42 em wide, centered, in the editor's colors.

// The sizes of the view: the bar, the Contents panel, the column (in em) and the gap above a block (in em).
const (
	barHeight     = 32
	contentsWidth = 240
	columnPadding = 24
	columnEm      = 42
	blockGapEm    = 0.8
)

// build is the Build of the tab.
func (r *reader) build(widgets.BuildContext) widgets.Widget {
	return &readerView{StatefulWidget: widgets.NewStatefulWidgetBase(), r: r}
}

// readerView is the widget of a tab; its state is the tab's.
type readerView struct {
	widgets.StatefulWidget
	r *reader
}

// CreateState implements widgets.StatefulWidget.
func (w *readerView) CreateState() widgets.State { return &readerViewState{} }

// readerViewState is the state of the widget: the focus of the keys, the scroll of the chapter, the keys of its blocks (to scroll to
// one and to find the first in view) and the recognizers of the links.
type readerViewState struct {
	widgets.StateOf[*readerView]
	focus    *widgets.FocusNode
	scroll   *widgets.ScrollControllerBase
	listener foundation.ListenerHandle
	keys     []*widgets.GlobalKey[widgets.State]
	taps     []gestures.GestureRecognizer
	// shown is the chapter the blocks were built for: a new one scrolls to its target.
	shown *book.Chapter
}

// InitState implements widgets.State.
func (s *readerViewState) InitState() {
	s.Widget().r.view = s
	s.focus = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "epub"})
	s.scroll = widgets.NewScrollController()
	s.listener = s.scroll.AddListener(s.scrolled)
}

// DidUpdateWidget implements widgets.State.
func (s *readerViewState) DidUpdateWidget(old *readerView) {
	if old.r != s.Widget().r {
		if old.r.view == s {
			old.r.view = nil
		}
		s.Widget().r.view = s
	}
}

// Dispose implements widgets.State.
func (s *readerViewState) Dispose() {
	if r := s.Widget().r; r.view == s {
		r.view = nil
	}
	s.releaseTaps()
	s.scroll.RemoveListener(s.listener)
	s.scroll.Dispose()
	s.focus.Dispose()
}

// releaseTaps disposes the recognizers of the links of the last build.
func (s *readerViewState) releaseTaps() {
	for _, t := range s.taps {
		t.Dispose()
	}
	s.taps = nil
}

// changed builds the widget again.
func (s *readerViewState) changed() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// scrolled tells the tab the first block in view.
func (s *readerViewState) scrolled() {
	if i, ok := s.topBlock(); ok {
		s.Widget().r.setTopBlock(i)
	}
}

// topBlock is the first block whose bottom is below the top of the view.
func (s *readerViewState) topBlock() (int, bool) {
	if !s.scroll.HasClients() {
		return 0, false
	}
	top := s.scroll.Offset()
	for i, k := range s.keys {
		box, ok := boxOf(k)
		if !ok {
			continue
		}
		if y, ok := s.offsetInColumn(box); ok && y+box.Size().Height > top+1 {
			return i, true
		}
	}
	return 0, false
}

// boxOf is the render box of the widget that has the key.
func boxOf(k *widgets.GlobalKey[widgets.State]) (rendering.Box, bool) {
	bc := k.CurrentContext()
	if bc == nil {
		return nil, false
	}
	box, ok := bc.FindRenderObject().(rendering.Box)
	return box, ok
}

// offsetInColumn is how far down the scrolled content a block's box is: its distance from the first block, plus the padding above
// the first.
func (s *readerViewState) offsetInColumn(box rendering.Box) (float64, bool) {
	if len(s.keys) == 0 {
		return 0, false
	}
	first, ok := boxOf(s.keys[0])
	if !ok {
		return 0, false
	}
	return box.LocalToGlobal(gui.Offset{}, nil).Dy - first.LocalToGlobal(gui.Offset{}, nil).Dy + columnPadding, true
}

// scrollToTarget scrolls the block the tab asks for to the top of the view, once the chapter is laid out.
func (s *readerViewState) scrollToTarget() {
	r := s.Widget().r
	if r.target < 0 || !s.scroll.HasClients() {
		return
	}
	target := r.target
	r.target = -1
	if target == 0 || target >= len(s.keys) {
		s.scroll.JumpTo(0)
		return
	}
	if box, ok := boxOf(s.keys[target]); ok {
		if y, ok := s.offsetInColumn(box); ok {
			s.scroll.JumpTo(min(y, s.scroll.Position().MaxScrollExtent()))
		}
	}
	r.pos.Block = target
}

// Build implements widgets.State.
func (s *readerViewState) Build(bc widgets.BuildContext) widgets.Widget {
	r := s.Widget().r
	colors := ui.Theme(bc)
	var body widgets.Widget
	switch {
	case r.deleted:
		body = message(bc, "The file was deleted.", nil)
	case r.problem != nil:
		body = message(bc, problemText(r.problem), ui.NewButton("Check Again", ui.ButtonOpts{Secondary: true, OnPressed: r.load}))
	case r.book == nil:
		body = widgets.NewSizedBox(nil)
	default:
		row := []widgets.Widget{widgets.NewExpanded(s.page(bc))}
		if r.contentsShown {
			row = append([]widgets.Widget{s.contents(bc), widgets.NewContainer(nil, widgets.ContainerOpts{
				Width: foundation.Some(1.0), Color: foundation.Some(colors.WidgetBorder)})}, row...)
		}
		return widgets.NewFocus(widgets.NewColoredBox(colors.EditorBackground, widgets.NewColumn([]widgets.Widget{
			s.bar(bc),
			widgets.NewExpanded(widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})),
		})), widgets.FocusOpts{FocusNode: s.focus, Autofocus: true, OnKeyEvent: s.key})
	}
	return widgets.NewColoredBox(colors.EditorBackground, body)
}

// message is a message in the middle of the view, with a button under it.
func message(bc widgets.BuildContext, text string, button widgets.Widget) widgets.Widget {
	colors := ui.Theme(bc)
	children := []widgets.Widget{widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.Foreground), TextAlign: gui.TextAlignCenter})}
	if button != nil {
		children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(12.0)}), button)
	}
	return widgets.NewCenter(widgets.NewPadding(painting.NewEdgeInsetsAll(16),
		widgets.NewColumn(children, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})))
}

// bar is the bar above the chapter.
func (s *readerViewState) bar(bc widgets.BuildContext) widgets.Widget {
	r := s.Widget().r
	colors := ui.Theme(bc)
	title := r.book.Title
	if r.chapter != nil && r.chapter.Title != "" && r.chapter.Title != title {
		if title != "" {
			title += " — "
		}
		title += r.chapter.Title
	}
	n := len(r.book.Spine)
	size := r.x.fontSize()
	row := widgets.NewRow([]widgets.Widget{
		ui.NewIconButton("list-tree", ui.IconButtonOpts{Tooltip: "Contents", Checked: r.contentsShown, OnPressed: r.toggleContents}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
		widgets.NewExpanded(widgets.NewText(title, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.Foreground),
			MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
		ui.NewButton("A−", ui.ButtonOpts{Secondary: true, Disabled: size <= MinFontSize, OnPressed: func() { r.x.changeFontSize(-1) }}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}),
		ui.NewButton("A+", ui.ButtonOpts{Secondary: true, Disabled: size >= MaxFontSize, OnPressed: func() { r.x.changeFontSize(1) }}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
		ui.NewIconButton("arrow-left", ui.IconButtonOpts{Tooltip: "Previous Chapter", Disabled: r.pos.Chapter <= 0,
			OnPressed: func() { r.goToChapter(r.pos.Chapter-1, "") }}),
		widgets.NewText(strconv.Itoa(r.pos.Chapter+1)+" / "+strconv.Itoa(n), widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}),
		ui.NewIconButton("arrow-right", ui.IconButtonOpts{Tooltip: "Next Chapter", Disabled: r.pos.Chapter >= n-1,
			OnPressed: func() { r.goToChapter(r.pos.Chapter+1, "") }}),
	})
	return widgets.NewContainer(row, widgets.ContainerOpts{
		Height: foundation.Some(float64(barHeight)), Color: foundation.Some(colors.EditorBackground),
		Padding: painting.NewEdgeInsetsSymmetric(0, 8),
	})
}

// contents is the Contents panel.
func (s *readerViewState) contents(bc widgets.BuildContext) widgets.Widget {
	r := s.Widget().r
	colors := ui.Theme(bc)
	var body widgets.Widget
	if len(r.book.TOC) == 0 {
		body = widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 4, 12, 4), widgets.NewText("This book has no contents.",
			widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}))
	} else {
		body = ui.NewTree(r.tree, ui.TreeOpts{OnOpen: r.openEntry, OnSelect: r.openEntry,
			Render: func(bc widgets.BuildContext, node ui.TreeNode, st ui.RowState) widgets.Widget {
				return ui.NewHighlightedLabel(bc, node.Label, st)
			}})
	}
	header := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 6, 12, 6), widgets.NewText("CONTENTS",
		widgets.TextOpts{Style: ui.TextStyle(bc, 11, colors.Foreground)}))
	return widgets.NewContainer(widgets.NewColumn([]widgets.Widget{header, widgets.NewExpanded(body)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}), widgets.ContainerOpts{
		Width: foundation.Some(float64(contentsWidth)), Color: foundation.Some(colors.SideBarBackground)})
}

// page is the chapter in its column, scrolled, with Previous and Next at its end and a note under it while one is shown.
func (s *readerViewState) page(bc widgets.BuildContext) widgets.Widget {
	r := s.Widget().r
	colors := ui.Theme(bc)
	s.releaseTaps()
	var content widgets.Widget
	switch {
	case r.chapterErr != nil:
		content = message(bc, "This chapter cannot be shown: "+problemText(r.chapterErr), nil)
	case r.chapter == nil:
		content = widgets.NewSizedBox(nil)
	default:
		size := r.x.fontSize()
		rd := &renderer{s: s, bc: bc, size: size, family: r.x.fontFamily(), colors: colors}
		s.keys = make([]*widgets.GlobalKey[widgets.State], len(r.chapter.Blocks))
		children := make([]widgets.Widget, 0, len(r.chapter.Blocks)+1)
		for i, b := range r.chapter.Blocks {
			s.keys[i] = widgets.NewGlobalKey[widgets.State]()
			gap := 0.0
			if i > 0 {
				gap = size * blockGapEm
			}
			children = append(children, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, gap, 0, 0),
				widgets.NewKeyedSubtree(rd.block(b), widgets.WidgetOpts{Key: s.keys[i]})))
		}
		children = append(children, s.chapterEnd(bc))
		column := widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
		content = widgets.NewSingleChildScrollView(widgets.NewCenter(widgets.NewConstrainedBox(rendering.BoxConstraints{MaxWidth: size * columnEm, MaxHeight: inf},
			widgets.NewPadding(painting.NewEdgeInsetsAll(columnPadding), column))), widgets.SingleChildScrollViewOpts{Controller: s.scroll})
		if s.shown != r.chapter || r.target >= 0 {
			s.shown = r.chapter
			widgets.WidgetsBindingOf(bc).Scheduler().AddPostFrameCallback(func(time.Duration) { // laid out by then
				if s.Mounted() {
					s.scrollToTarget()
				}
			})
		}
	}
	if len(r.note) == 0 {
		return content
	}
	rd := &renderer{s: s, bc: bc, size: r.x.fontSize() * 0.9, family: r.x.fontFamily(), colors: colors}
	var note []widgets.Widget
	for _, b := range r.note {
		note = append(note, rd.block(b))
	}
	panel := widgets.NewContainer(widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(widgets.NewColumn(note, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})),
		ui.NewIconButton("close", ui.IconButtonOpts{Tooltip: "Close the Note", OnPressed: func() { r.note = nil; r.refresh() }}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}), widgets.ContainerOpts{
		Padding: painting.NewEdgeInsetsAll(12), Color: foundation.Some(colors.EditorWidgetBackground)})
	return widgets.NewColumn([]widgets.Widget{widgets.NewExpanded(content), panel},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// inf is an unbounded height.
const inf = 1e18

// chapterEnd is Previous and Next chapter at the end of a chapter.
func (s *readerViewState) chapterEnd(bc widgets.BuildContext) widgets.Widget {
	r := s.Widget().r
	var buttons []widgets.Widget
	if r.pos.Chapter > 0 {
		buttons = append(buttons, ui.NewButton("Previous Chapter", ui.ButtonOpts{Secondary: true, OnPressed: func() { r.goToChapter(r.pos.Chapter-1, "") }}))
	}
	if r.pos.Chapter < len(r.book.Spine)-1 {
		buttons = append(buttons, ui.NewButton("Next Chapter", ui.ButtonOpts{OnPressed: func() { r.goToChapter(r.pos.Chapter+1, "") }}))
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 32, 0, 16), widgets.NewWrap(buttons,
		widgets.WrapOpts{Alignment: rendering.WrapAlignmentCenter, Spacing: 8, RunSpacing: 8}))
}

// key is the keys of the reader: Page Up/Down and Space scroll, Left/Right change chapters, Home/End go to the ends of the chapter.
func (s *readerViewState) key(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	r := s.Widget().r
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	if kb.IsControlPressed() || kb.IsAltPressed() || kb.IsMetaPressed() || !s.scroll.HasClients() {
		return widgets.KeyEventResultIgnored
	}
	pos := s.scroll.Position()
	page := max(pos.ViewportDimension()-40, 40)
	to := func(y float64) { s.scroll.JumpTo(min(max(y, pos.MinScrollExtent()), pos.MaxScrollExtent())) }
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyPageDown:
		to(pos.Pixels() + page)
	case services.LogicalKeyboardKeyPageUp:
		to(pos.Pixels() - page)
	case services.LogicalKeyboardKeySpace:
		if kb.IsShiftPressed() {
			to(pos.Pixels() - page)
		} else {
			to(pos.Pixels() + page)
		}
	case services.LogicalKeyboardKeyArrowDown:
		to(pos.Pixels() + 40)
	case services.LogicalKeyboardKeyArrowUp:
		to(pos.Pixels() - 40)
	case services.LogicalKeyboardKeyHome:
		to(0)
	case services.LogicalKeyboardKeyEnd:
		to(pos.MaxScrollExtent())
	case services.LogicalKeyboardKeyArrowRight:
		r.goToChapter(r.pos.Chapter+1, "")
	case services.LogicalKeyboardKeyArrowLeft:
		r.goToChapter(r.pos.Chapter-1, "")
	default:
		return widgets.KeyEventResultIgnored
	}
	return widgets.KeyEventResultHandled
}

// renderer builds the widgets of the blocks of a chapter.
type renderer struct {
	s      *readerViewState
	bc     widgets.BuildContext
	size   float64
	family string
	colors ui.Colors
}

// mono is the monospace font of the platform, or the generic family (SPEC D14).
func (rd *renderer) mono() string {
	family, _ := widgets.WidgetsBindingOf(rd.bc).PlatformDispatcher().MonospaceFont()
	if family == "" {
		return "monospace"
	}
	return family
}

// style is the text style of a size relative to the text's, in a color.
func (rd *renderer) style(scale float64, color gui.Color) *painting.TextStyle {
	st := ui.TextStyle(rd.bc, rd.size*scale, color)
	opts := painting.TextStyleOpts{Height: foundation.Some(1.5)}
	if rd.family != "" {
		opts.FontFamily = rd.family
	}
	return st.CopyWith(opts)
}

// textAlign is the alignment of a block's text.
func textAlign(a book.Align) gui.TextAlign {
	switch a {
	case book.AlignCenter:
		return gui.TextAlignCenter
	case book.AlignRight:
		return gui.TextAlignRight
	}
	return gui.TextAlignStart
}

// headingScales are the sizes of the headings, h1 to h6, relative to the text.
var headingScales = [...]float64{2, 1.6, 1.3, 1.15, 1, 0.9}

// block is the widget of a block.
func (rd *renderer) block(b book.Block) widgets.Widget {
	c := rd.colors
	switch b.Kind {
	case book.Heading:
		st := rd.style(headingScales[min(max(b.Level, 1), 6)-1], c.Foreground).CopyWith(painting.TextStyleOpts{FontWeight: gui.FontWeightBold, Height: foundation.Some(1.25)})
		return rd.text(b.Inlines, st, b.Align)
	case book.Quote:
		var children []widgets.Widget
		for i, child := range b.Children {
			if i > 0 {
				children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(rd.size * blockGapEm)}))
			}
			children = append(children, rd.block(child))
		}
		return widgets.NewContainer(widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
			widgets.ContainerOpts{Padding: painting.NewEdgeInsetsFromLTRB(rd.size, 2, 0, 2), Decoration: &painting.BoxDecoration{
				Border: painting.Border{Left: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(c.TextBlockQuoteBorder), Width: foundation.Some(4.0)})},
			}})
	case book.List:
		var items []widgets.Widget
		for i, item := range b.Items {
			marker := "•"
			if b.Ordered {
				marker = strconv.Itoa(b.Start+i) + "."
			}
			var blocks []widgets.Widget
			for j, child := range item {
				if j > 0 {
					blocks = append(blocks, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(rd.size * 0.4)}))
				}
				blocks = append(blocks, rd.block(child))
			}
			items = append(items, widgets.NewRow([]widgets.Widget{
				widgets.NewSizedBox(widgets.NewText(marker, widgets.TextOpts{Style: rd.style(1, c.Foreground), TextAlign: gui.TextAlignRight}),
					widgets.SizedBoxOpts{Width: foundation.Some(rd.size * 1.6)}),
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(rd.size * 0.5)}),
				widgets.NewExpanded(widgets.NewColumn(blocks, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})),
			}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}))
		}
		return widgets.NewColumn(items, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	case book.Pre:
		st := ui.TextStyle(rd.bc, rd.size*0.85, c.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: rd.mono()})
		return widgets.NewContainer(widgets.NewSingleChildScrollView(widgets.NewText(b.Text, widgets.TextOpts{Style: st}),
			widgets.SingleChildScrollViewOpts{ScrollDirection: painting.AxisHorizontal}),
			widgets.ContainerOpts{Padding: painting.NewEdgeInsetsAll(rd.size * 0.6), Color: foundation.Some(c.TextCodeBlockBackground)})
	case book.Rule:
		return widgets.NewPadding(painting.NewEdgeInsetsSymmetric(rd.size*0.5, 0), widgets.NewContainer(nil, widgets.ContainerOpts{
			Height: foundation.Some(1.0), Color: foundation.Some(c.WidgetBorder)}))
	case book.Table:
		var rows []widgets.Widget
		for i, row := range b.Rows {
			var cells []widgets.Widget
			for _, cell := range row {
				st := rd.style(0.95, c.Foreground)
				if i == 0 {
					st = st.CopyWith(painting.TextStyleOpts{FontWeight: gui.FontWeightBold})
				}
				cells = append(cells, widgets.NewExpanded(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(rd.size*0.25, rd.size*0.4),
					widgets.NewText(cell, widgets.TextOpts{Style: st}))))
			}
			rows = append(rows, widgets.NewContainer(widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}),
				widgets.ContainerOpts{Decoration: &painting.BoxDecoration{Border: painting.Border{
					Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(c.WidgetBorder), Width: foundation.Some(1.0)})}}}))
		}
		return widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	case book.Image:
		img, ok := rd.s.Widget().r.images[b.Image.Path]
		if !ok {
			alt := b.Image.Alt
			if alt == "" {
				alt = "[image]"
			}
			return rd.text([]book.Inline{{Text: alt, Style: book.Emphasis}}, rd.style(0.9, c.DescriptionForeground), book.AlignCenter)
		}
		// no wider than the column, never enlarged
		return widgets.NewCenter(widgets.NewConstrainedBox(rendering.BoxConstraints{MaxWidth: float64(img.Width()), MaxHeight: float64(img.Height())},
			widgets.NewAspectRatio(float64(img.Width())/float64(max(img.Height(), 1)), widgets.NewRawImage(img, widgets.RawImageOpts{
				Scale: 1, Fit: painting.BoxFitContain, FilterQuality: gui.FilterQualityMedium}))))
	}
	return rd.text(b.Inlines, rd.style(1, c.Foreground), b.Align)
}

// text is the runs of a block as rich text: emphasis, strong, code, sub- and superscript, links in the link color that follow when
// clicked.
func (rd *renderer) text(runs []book.Inline, base *painting.TextStyle, align book.Align) widgets.Widget {
	r := rd.s.Widget().r
	var spans []painting.InlineSpan
	for _, run := range runs {
		st := base
		opts := painting.TextStyleOpts{}
		if run.Style&book.Emphasis != 0 {
			opts.FontStyle = gui.FontStyleItalic
		}
		if run.Style&book.Strong != 0 {
			opts.FontWeight = gui.FontWeightBold
		}
		if run.Style&(book.Sub|book.Sup) != 0 {
			opts.FontSize = foundation.Some(base.FontSize.OrElse(rd.size) * 0.75)
		}
		if run.Link != "" {
			opts.Color = foundation.Some(rd.colors.TextLinkForeground)
			opts.Decoration = foundation.Some(gui.TextDecorationUnderline)
		}
		st = st.CopyWith(opts)
		if run.Style&book.Code != 0 {
			st = st.CopyWith(painting.TextStyleOpts{FontFamily: rd.mono()})
		}
		var tap gestures.GestureRecognizer
		if run.Link != "" {
			t := gestures.NewTapGestureRecognizer(widgets.WidgetsBindingOf(rd.bc).Gestures())
			link, external := run.Link, run.External
			t.OnTap = func() { r.openLink(link, external) }
			rd.s.taps = append(rd.s.taps, t)
			tap = t
		}
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(run.Text), Style: st, Recognizer: tap}))
	}
	return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: base, Children: spans}),
		widgets.RichTextOpts{TextAlign: textAlign(align)})
}
