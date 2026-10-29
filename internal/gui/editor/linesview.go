package editor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// LinesView (SPEC §9.11, "Inline view"): a read-only picture of some lines of another editor's document, drawn in the
// font, the colors of the syntax and the brackets of that editor, in rows of the same height. It is the widget of the
// view zone that holds the deleted lines of the inline diff. It has no cursor and takes no selection: it is paint only.
//
// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/components/diffEditorViewZones/renderLines.ts (the
// tokens of the original model rendered as the lines of a zone, with the removed characters as decorations)

// LinesViewOpts describe a LinesView.
type LinesViewOpts struct {
	// Source is the view model whose document, tokens, options and brackets give the lines. Its projection is
	// not used: line n is row n.
	Source *ViewModel
	// FirstLine is the first line shown (counted from 0) and Count how many lines: each is one row, the rows
	// being as high as the widget is divided by Count.
	FirstLine, Count int
	// ScrollWith, when it is not nil, is the editor whose horizontal scroll position the text follows, so that
	// the lines stay under the text of the editor the zone is in.
	ScrollWith *ViewModel
	// Marks color the lines (WholeLine: the rows across the widget) and characters, in the lines of the source's
	// document (counted from 0); marks outside the lines shown are ignored.
	Marks []BackgroundMark
}

// linesView is the widget of LinesView: it reads the theme and makes the render object.
type linesView struct {
	widgets.StatelessWidget
	opts LinesViewOpts
}

// NewLinesView makes a widget that draws lines of a view model's document (see LinesViewOpts).
func NewLinesView(opts LinesViewOpts) widgets.Widget {
	return &linesView{StatelessWidget: widgets.NewStatelessWidgetBase(), opts: opts}
}

// Build implements widgets.StatelessWidget.
func (w *linesView) Build(bc widgets.BuildContext) widgets.Widget {
	colors := kit.ThemeOf(bc)
	return &linesLayer{
		LeafRenderObjectWidget: widgets.NewLeafRenderObjectWidgetBase(),
		opts:                   w.opts,
		color:                  colors.EditorForeground,
		tokens:                 kit.TokenThemeOf(bc),
		brackets:               BracketColors{Levels: colors.EditorBracketHighlightForeground, Unexpected: colors.EditorBracketUnexpectedForeground},
	}
}

// linesLayer is the widget of RenderLinesView.
type linesLayer struct {
	widgets.LeafRenderObjectWidget
	opts     LinesViewOpts
	color    ui.Color
	tokens   *theme.TokenTheme
	brackets BracketColors
}

// CreateRenderObject implements widgets.RenderObjectWidget.
func (w *linesLayer) CreateRenderObject(bc widgets.BuildContext) rendering.RenderObject {
	r := &RenderLinesView{}
	rendering.InitRenderBox(&r.RenderBox, r)
	r.Update(w.opts, textFactory(bc), w.color, w.tokens, w.brackets)
	return r
}

// UpdateRenderObject implements widgets.RenderObjectWidget.
func (w *linesLayer) UpdateRenderObject(bc widgets.BuildContext, ro rendering.RenderObject) {
	ro.(*RenderLinesView).Update(w.opts, textFactory(bc), w.color, w.tokens, w.brackets)
}

// RenderLinesView is the render object of a LinesView.
type RenderLinesView struct {
	rendering.RenderBox
	opts     LinesViewOpts
	factory  ui.ParagraphFactory
	color    ui.Color
	tokens   *theme.TokenTheme
	brackets BracketColors
	styles   *styleTable

	srcHandle, scrollHandle foundation.ListenerHandle
	attached                bool
}

// Update copies the configuration of the widget.
func (r *RenderLinesView) Update(opts LinesViewOpts, factory ui.ParagraphFactory, color ui.Color, tokens *theme.TokenTheme, brackets BracketColors) {
	rewatch := r.attached && (opts.Source != r.opts.Source || opts.ScrollWith != r.opts.ScrollWith)
	if rewatch {
		r.unwatch()
	}
	if tokens != r.tokens {
		r.styles = nil
	}
	r.opts, r.factory, r.color, r.tokens, r.brackets = opts, factory, color, tokens, brackets
	if rewatch {
		r.watch()
	}
	r.MarkNeedsPaint()
}

func (r *RenderLinesView) watch() {
	if r.opts.Source != nil {
		r.srcHandle = r.opts.Source.AddListener(r.MarkNeedsPaint)
	}
	if r.opts.ScrollWith != nil {
		r.scrollHandle = r.opts.ScrollWith.AddScrollListener(r.MarkNeedsPaint)
	}
}

func (r *RenderLinesView) unwatch() {
	if r.opts.Source != nil {
		r.opts.Source.RemoveListener(r.srcHandle)
	}
	if r.opts.ScrollWith != nil {
		r.opts.ScrollWith.RemoveScrollListener(r.scrollHandle)
	}
}

// Attach implements rendering.RenderObject.
func (r *RenderLinesView) Attach(owner *rendering.PipelineOwner) {
	r.RenderBox.Attach(owner)
	r.watch()
	r.attached = true
}

// Detach implements rendering.RenderObject.
func (r *RenderLinesView) Detach() {
	r.unwatch()
	r.attached = false
	r.RenderBox.Detach()
}

// SizedByParent implements rendering.RenderBox.
func (r *RenderLinesView) SizedByParent() bool { return true }

// HitTestSelf implements rendering.RenderBox: the lines take no pointer events (the editor under the zone sees them).
func (r *RenderLinesView) HitTestSelf(ui.Offset) bool { return false }

// ComputeMinIntrinsicWidth implements rendering.RenderBox.
func (r *RenderLinesView) ComputeMinIntrinsicWidth(float64) float64 { return 0 }

// ComputeMaxIntrinsicWidth implements rendering.RenderBox.
func (r *RenderLinesView) ComputeMaxIntrinsicWidth(float64) float64 { return 0 }

// ComputeMinIntrinsicHeight implements rendering.RenderBox.
func (r *RenderLinesView) ComputeMinIntrinsicHeight(float64) float64 { return 0 }

// ComputeMaxIntrinsicHeight implements rendering.RenderBox.
func (r *RenderLinesView) ComputeMaxIntrinsicHeight(float64) float64 { return 0 }

// ComputeDryLayout implements rendering.RenderBox: the biggest size the constraints allow.
func (r *RenderLinesView) ComputeDryLayout(c rendering.BoxConstraints) ui.Size {
	return c.Constrain(c.Biggest())
}

// PerformLayout implements rendering.RenderBox; there are no children.
func (r *RenderLinesView) PerformLayout() {}

// spansOf are the colored parts of a line, from the tokens of the source.
func (r *RenderLinesView) spansOf(line, end int) []span {
	tok := r.opts.Source.Tokenizer()
	if tok == nil || r.tokens == nil {
		return nil
	}
	lineTokens := tok.Line(line)
	if lineTokens == nil {
		return nil
	}
	if r.styles == nil || r.styles.scopes != tok.Scopes() {
		r.styles = newStyleTable(r.tokens, tok.Scopes())
	}
	return r.styles.spans(lineTokens, 0, end)
}

// Paint draws the marks and then the lines.
func (r *RenderLinesView) Paint(context *rendering.PaintingContext, offset ui.Offset) {
	src := r.opts.Source
	size := r.Size()
	if src == nil || r.opts.Count <= 0 || size.Height <= 0 {
		return
	}
	canvas := context.Canvas()
	canvas.Save()
	defer canvas.Restore()
	canvas.ClipRect(ui.NewRectFromLTWH(offset.Dx, offset.Dy, size.Width, size.Height))
	rowHeight := size.Height / float64(r.opts.Count)
	left := 0.0
	if r.opts.ScrollWith != nil {
		left = r.opts.ScrollWith.ScrollLeft()
	}
	first, last := r.opts.FirstLine, r.opts.FirstLine+r.opts.Count-1
	snap := src.Document().Snapshot()
	if src.Tokenizer() != nil {
		src.ensureOwn(last, tokenBudget)
	}
	style := newTextStyle(r.factory, src.Options(), r.color)
	geo := newGeometry(src, r.factory)
	top := func(line int) float64 { return offset.Dy + float64(line-first)*rowHeight }

	for _, m := range r.opts.Marks {
		if !m.WholeLine || m.Background.Alpha() == 0 {
			continue
		}
		from, to := max(m.Range.Start.Line, first), min(m.Range.End.Line, last)
		if from <= to {
			canvas.DrawRect(ui.NewRectFromLTRB(offset.Dx, top(from), offset.Dx+size.Width, top(to)+rowHeight), fill(m.Background))
		}
	}
	for _, m := range r.opts.Marks {
		if m.WholeLine || m.Range.Start == m.Range.End {
			continue
		}
		for line := max(m.Range.Start.Line, first); line <= min(m.Range.End.Line, last); line++ {
			from, to := 0, 0
			if text, err := snap.LineContent(line); err == nil {
				to = len(text)
			}
			if line == m.Range.Start.Line {
				from = m.Range.Start.Col
			}
			x0 := geo.x(lineRow(src, line), from)
			var x1 float64
			if line == m.Range.End.Line {
				x1 = geo.x(lineRow(src, line), m.Range.End.Col)
			} else {
				x1 = geo.x(lineRow(src, line), to) + geo.charWidth // the range goes on past the end of the line
			}
			if x1 <= x0 {
				continue
			}
			rect := ui.NewRectFromLTRB(offset.Dx+x0-left, top(line), offset.Dx+x1-left, top(line)+rowHeight)
			if m.Background.Alpha() > 0 {
				canvas.DrawRect(rect, fill(m.Background))
			}
			if m.Border.Alpha() > 0 {
				outline(canvas, rect, m.Border)
			}
		}
	}
	for line := first; line <= last; line++ {
		text, err := snap.LineContent(line)
		if err != nil {
			continue
		}
		shown := shownLength(text, src.opts.StopRenderingLineAfter)
		text = text[:shown]
		if text == "" {
			continue
		}
		spans := overlaySpans(r.spansOf(line, shown), src.bracketSpans(r.brackets, line, 0, shown))
		p := style.styledParagraph(src.Paragraphs(), text, spans)
		canvas.DrawParagraph(p, ui.Offset{Dx: offset.Dx - left, Dy: top(line)})
	}
}

// lineRow is the first row of a line in the projection of a view model.
func lineRow(vm *ViewModel, line int) int {
	row, _ := vm.Projection().LineRows(line)
	return row
}

// outline draws a one pixel border inside a rectangle.
func outline(canvas *ui.Canvas, r ui.Rect, color ui.Color) {
	paint := fill(color)
	canvas.DrawRect(ui.NewRectFromLTRB(r.Left, r.Top, r.Right, r.Top+1), paint)
	canvas.DrawRect(ui.NewRectFromLTRB(r.Left, r.Bottom-1, r.Right, r.Bottom), paint)
	canvas.DrawRect(ui.NewRectFromLTRB(r.Left, r.Top+1, r.Left+1, r.Bottom-1), paint)
	canvas.DrawRect(ui.NewRectFromLTRB(r.Right-1, r.Top+1, r.Right, r.Bottom-1), paint)
}
