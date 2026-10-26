package editor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/find"
	"github.com/rm4n0s/emetos/internal/core/theme"
)

// Ported from VS Code: src/vs/editor/browser/viewParts/minimap/minimap.ts (InnerMinimap.render,
// renderDecorations)

// MinimapColors are the colors of the minimap.
type MinimapColors struct {
	// Background is the minimap's own background (transparent: the editor's), EditorBackground
	// the editor's, Foreground the color of text with no token color and ForegroundOpacity the
	// opacity of the blocks (its alpha).
	Background, EditorBackground, Foreground, ForegroundOpacity ui.Color
	// Selection colors the selections, FindMatch the matches of the find widget and
	// SelectionOccurrence the text that equals the selection.
	Selection, FindMatch, SelectionOccurrence ui.Color
	// Error, Warning and Info mark the diagnostics.
	Error, Warning, Info ui.Color
	// Slider, SliderHover and SliderActive color the slider when the pointer is away, over the
	// minimap and pressing it.
	Slider, SliderHover, SliderActive ui.Color
}

// minimapLayer is the widget of RenderEditorMinimap.
type minimapLayer struct {
	widgets.LeafRenderObjectWidget
	vm     *ViewModel
	scroll foundation.Listenable
	colors MinimapColors
	tokens *theme.TokenTheme
}

func newMinimapLayer(vm *ViewModel, scroll foundation.Listenable, colors MinimapColors, tokens *theme.TokenTheme) *minimapLayer {
	return &minimapLayer{LeafRenderObjectWidget: widgets.NewLeafRenderObjectWidgetBase(), vm: vm, scroll: scroll, colors: colors, tokens: tokens}
}

// CreateRenderObject implements widgets.RenderObjectWidget.
func (w *minimapLayer) CreateRenderObject(widgets.BuildContext) rendering.RenderObject {
	return NewRenderEditorMinimap(w.vm, w.scroll, w.colors, w.tokens)
}

// UpdateRenderObject implements widgets.RenderObjectWidget.
func (w *minimapLayer) UpdateRenderObject(_ widgets.BuildContext, ro rendering.RenderObject) {
	ro.(*RenderEditorMinimap).Update(w.vm, w.scroll, w.colors, w.tokens)
}

// RenderEditorMinimap draws the minimap: the text as colored blocks, the marks of the selections
// and the matches over them and the slider that shows what the editor shows (SPEC §9.5). The pixels
// of the blocks are kept between frames and only the lines that changed are drawn again.
type RenderEditorMinimap struct {
	rendering.RenderBox
	vm     *ViewModel
	scroll foundation.Listenable
	colors MinimapColors
	tokens *theme.TokenTheme

	painter minimapPainter
	image   *ui.Image
	styles  *styleTable

	scrollHandle, vmHandle, selHandle, mmHandle foundation.ListenerHandle
	attached                                    bool
}

// NewRenderEditorMinimap makes the minimap of a view model.
func NewRenderEditorMinimap(vm *ViewModel, scroll foundation.Listenable, colors MinimapColors, tokens *theme.TokenTheme) *RenderEditorMinimap {
	r := &RenderEditorMinimap{vm: vm, scroll: scroll, colors: colors, tokens: tokens}
	rendering.InitRenderBox(&r.RenderBox, r)
	return r
}

// Update copies the configuration of a widget.
func (r *RenderEditorMinimap) Update(vm *ViewModel, scroll foundation.Listenable, colors MinimapColors, tokens *theme.TokenTheme) {
	if r.attached {
		if vm != r.vm {
			r.unlisten()
			r.vm = vm
			r.listen()
		}
		if scroll != r.scroll {
			r.scroll.RemoveListener(r.scrollHandle)
			r.scroll = scroll
			r.scrollHandle = scroll.AddListener(r.MarkNeedsPaint)
		}
	}
	r.vm, r.scroll, r.colors = vm, scroll, colors
	if tokens != r.tokens {
		r.tokens, r.styles = tokens, nil
	}
	r.MarkNeedsPaint()
}

func (r *RenderEditorMinimap) listen() {
	r.vmHandle = r.vm.AddListener(r.MarkNeedsPaint)
	r.selHandle = r.vm.AddSelectionListener(r.MarkNeedsPaint)
	r.mmHandle = r.vm.mm.changed.AddListener(r.MarkNeedsPaint)
}

func (r *RenderEditorMinimap) unlisten() {
	r.vm.RemoveListener(r.vmHandle)
	r.vm.RemoveSelectionListener(r.selHandle)
	r.vm.mm.changed.RemoveListener(r.mmHandle)
}

// Attach implements rendering.RenderObject.
func (r *RenderEditorMinimap) Attach(owner *rendering.PipelineOwner) {
	r.RenderBox.Attach(owner)
	r.scrollHandle = r.scroll.AddListener(r.MarkNeedsPaint)
	r.listen()
	r.attached = true
}

// Detach implements rendering.RenderObject.
func (r *RenderEditorMinimap) Detach() {
	r.scroll.RemoveListener(r.scrollHandle)
	r.unlisten()
	r.attached = false
	r.RenderBox.Detach()
}

// SizedByParent implements rendering.RenderObject.
func (r *RenderEditorMinimap) SizedByParent() bool { return true }

// IsRepaintBoundary implements rendering.RenderObject.
func (r *RenderEditorMinimap) IsRepaintBoundary() bool { return true }

// HitTestSelf implements rendering.RenderBox.
func (r *RenderEditorMinimap) HitTestSelf(ui.Offset) bool { return true }

// ComputeMinIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorMinimap) ComputeMinIntrinsicWidth(float64) float64 { return 0 }

// ComputeMaxIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorMinimap) ComputeMaxIntrinsicWidth(float64) float64 { return 0 }

// ComputeMinIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorMinimap) ComputeMinIntrinsicHeight(float64) float64 { return 0 }

// ComputeMaxIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorMinimap) ComputeMaxIntrinsicHeight(float64) float64 { return 0 }

// ComputeDryLayout implements rendering.RenderBox.
func (r *RenderEditorMinimap) ComputeDryLayout(c rendering.BoxConstraints) ui.Size {
	return c.Constrain(c.Biggest())
}

// PerformLayout implements rendering.RenderBox; the layer has nothing to lay out.
func (r *RenderEditorMinimap) PerformLayout() {}

// top is the vertical scroll position, which the editor keeps up to date before the minimap
// is told of the scroll.
func (r *RenderEditorMinimap) top() float64 { return r.vm.ScrollTop() }

// style is how the painter draws, from the layout and the colors.
func (r *RenderEditorMinimap) style() minimapStyle {
	info, c := r.vm.mm.info, r.colors
	tabSize := max(r.vm.Options().TabSize, 1)
	return minimapStyle{
		Width: info.CanvasInnerWidth, Height: info.CanvasInnerHeight, LineHeight: info.LineHeight, Scale: info.Scale, TabSize: tabSize,
		Background: rgbaOf(minimapBackgroundOf(c)), DefaultBackground: rgbaOf(c.EditorBackground), Foreground: rgbaOf(c.Foreground),
		ForegroundAlpha: uint8(c.ForegroundOpacity.Alpha()),
	}
}

// minimapBackgroundOf is the color of the minimap: its own, or the editor's when the theme gives none.
func minimapBackgroundOf(c MinimapColors) ui.Color {
	if c.Background.Alpha() > 0 {
		return c.Background
	}
	return c.EditorBackground
}

// rowData is what the painter needs to draw the line of the minimap that shows a row.
func (r *RenderEditorMinimap) rowData(snap buffer.Snapshot, row int) minimapRowData {
	proj := r.vm.Projection()
	line, start, end := proj.RowSpan(row)
	text, err := snap.LineContent(line)
	if err != nil {
		return minimapRowData{key: minimapRowKey{line: -1}}
	}
	if end < 0 || end > len(text) {
		end = len(text)
	}
	start = min(start, end)
	text = text[start:end]
	spans := r.spans(line, start, end)
	return minimapRowData{key: minimapRowKey{line: line, start: start, end: end, text: text, spans: spansKey(spans)}, text: text, spans: spans}
}

// spans are the colored parts of the bytes [start, end) of a line, as the text layer draws them.
func (r *RenderEditorMinimap) spans(line, start, end int) []span {
	tok := r.vm.Tokenizer()
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
	return r.styles.spans(lineTokens, start, end)
}

// Paint draws the minimap.
func (r *RenderEditorMinimap) Paint(context *rendering.PaintingContext, offset ui.Offset) {
	vm := r.vm
	info := vm.mm.info
	if !info.Show || info.CanvasInnerWidth <= 0 || info.CanvasInnerHeight <= 0 {
		return
	}
	layout := vm.minimapLayoutNow(r.top())
	if style := r.style(); style != r.painter.style {
		r.painter.reset(style)
		r.image = nil
	}
	proj := vm.Projection()
	snap := vm.Document().Snapshot()
	if vm.Tokenizer() != nil && layout.EndLine >= layout.StartLine {
		firstRow, lastRow := vm.minimapRowOf(layout.StartLine), vm.minimapRowOf(layout.EndLine)
		firstLine, _, _ := proj.RowSpan(firstRow)
		lastLine, _, _ := proj.RowSpan(lastRow)
		vm.ensureMinimapTokens(firstLine, lastLine)
	}
	if r.painter.frame(layout, func(line int) minimapRowData { return r.rowData(snap, vm.minimapRowOf(line)) }) || r.image == nil {
		r.image = r.painter.image()
	}
	canvas := context.Canvas()
	canvas.Save()
	dst := ui.NewRectFromLTWH(offset.Dx, offset.Dy, info.CanvasOuterWidth, info.CanvasOuterHeight)
	canvas.ClipRect(ui.NewRectFromLTWH(offset.Dx, offset.Dy, r.Size().Width, r.Size().Height))
	if r.image != nil {
		canvas.DrawImageRect(r.image, ui.NewRectFromLTWH(0, 0, float64(r.image.Width()), float64(r.image.Height())), dst, nil)
	}
	r.paintDecorations(canvas, offset, layout, snap)
	r.paintSlider(canvas, offset, layout)
	canvas.Restore()
}

// paintSlider draws the slider, over the lines of the text that are on the screen of the editor.
// With editor.minimap.showSlider "mouseover" it shows only while the pointer is over the minimap
// or the slider is dragged.
func (r *RenderEditorMinimap) paintSlider(canvas *ui.Canvas, offset ui.Offset, l minimapRenderLayout) {
	m := &r.vm.mm
	if !l.SliderNeeded || (m.in.ShowSlider != "always" && !m.hover && !m.active) {
		return
	}
	color := r.colors.Slider
	switch {
	case m.active:
		color = r.colors.SliderActive
	case m.hover:
		color = r.colors.SliderHover
	}
	canvas.DrawRect(ui.NewRectFromLTWH(offset.Dx, offset.Dy+l.SliderTop, m.info.Width, l.SliderHeight), fill(color))
}

// paintDecorations marks the selections, the matches of the find widget and the text equal to the
// selection on the lines of the minimap: a bar across the line for each line a selection covers,
// and the part of the line that each mark covers.
//
// Ported from VS Code: minimap.ts (InnerMinimap.renderDecorations)
func (r *RenderEditorMinimap) paintDecorations(canvas *ui.Canvas, offset ui.Offset, l minimapRenderLayout, snap buffer.Snapshot) {
	vm := r.vm
	info := vm.mm.info
	if l.EndLine < l.StartLine {
		return
	}
	pixel := info.CanvasOuterWidth / float64(info.CanvasInnerWidth)
	lineHeight := float64(info.LineHeight)
	firstRow, lastRow := vm.minimapRowOf(l.StartLine), vm.minimapRowOf(l.EndLine)
	proj := vm.Projection()
	firstLine, _, _ := proj.RowSpan(firstRow)
	lastLine, _, _ := proj.RowSpan(lastRow)
	span := buffer.Range{Start: buffer.Position{Line: firstLine}, End: buffer.Position{Line: lastLine + 1}}

	// the bars of the selected lines, then the marks of the matches and of the selections
	if r.colors.Selection.Alpha() > 0 {
		bar := fill(r.colors.Selection.WithAlpha(r.colors.Selection.Alpha() / 2))
		var barred = map[int]bool{}
		for _, sel := range vm.Cursors().Selections() {
			rng := sel.Range()
			for row := vm.RowOfPosition(rng.Start); row <= vm.RowOfPosition(rng.End); row++ {
				if _, _, zone := proj.RowZone(row); zone {
					continue
				}
				line := vm.minimapLineOfRow(row)
				if line < l.StartLine || line > l.EndLine || barred[line] {
					continue
				}
				barred[line] = true
				y := float64(l.yForLine(line, info.LineHeight))
				canvas.DrawRect(ui.NewRectFromLTWH(offset.Dx+minimapGutterWidth*pixel, offset.Dy+y*pixel, info.CanvasOuterWidth-minimapGutterWidth*pixel, lineHeight*pixel), bar)
			}
		}
	}
	var marks []minimapMark
	if vm.FindRevealed() {
		for _, d := range vm.doc.DecorationsIn(span, vm.owner(findOwner)) {
			if d.Options.ClassName == find.ClassFindMatch || d.Options.ClassName == find.ClassCurrentFindMatch {
				marks = append(marks, minimapMark{rng: d.Range, color: r.colors.FindMatch})
			}
		}
	}
	for _, d := range vm.occurrencesIn(selectionHighlightOwner, firstRow, lastRow) {
		marks = append(marks, minimapMark{rng: d.Range, color: r.colors.SelectionOccurrence})
	}
	marks = append(marks, vm.diagnosticMinimapMarks(firstRow, lastRow, r.colors.Error, r.colors.Warning, r.colors.Info)...)
	for _, sel := range vm.Cursors().Selections() {
		if !sel.IsEmpty() {
			marks = append(marks, minimapMark{rng: sel.Range(), color: r.colors.Selection})
		}
	}
	for _, m := range marks {
		r.paintMark(canvas, offset, l, snap, m, pixel)
	}
}

// minimapMark is a range of the document to mark in a color.
type minimapMark struct {
	rng   buffer.Range
	color ui.Color
}

// paintMark draws the part of each row a range covers, on the line of the minimap that shows the row.
func (r *RenderEditorMinimap) paintMark(canvas *ui.Canvas, offset ui.Offset, l minimapRenderLayout, snap buffer.Snapshot, m minimapMark, pixel float64) {
	vm := r.vm
	info := vm.mm.info
	if m.color.Alpha() == 0 {
		return
	}
	proj := vm.Projection()
	paint := fill(m.color)
	for row := vm.RowOfPosition(m.rng.Start); row <= vm.RowOfPosition(m.rng.End) && row < proj.RowCount(); row++ {
		mmLine := vm.minimapLineOfRow(row)
		if _, _, zone := proj.RowZone(row); zone || mmLine < l.StartLine || mmLine > l.EndLine {
			continue
		}
		line, start, end := proj.RowSpan(row)
		text, err := snap.LineContent(line)
		if err != nil {
			continue
		}
		if end < 0 || end > len(text) {
			end = len(text)
		}
		from, to := start, end
		if line == m.rng.Start.Line {
			from = max(m.rng.Start.Col, start)
		}
		if line == m.rng.End.Line {
			to = min(m.rng.End.Col, end)
		}
		if to < from {
			continue
		}
		x1 := minimapX(text[start:end], from-start, vm.Options().TabSize, info.Scale, info.CanvasInnerWidth)
		x2 := minimapX(text[start:end], to-start, vm.Options().TabSize, info.Scale, info.CanvasInnerWidth)
		y := float64(l.yForLine(mmLine, info.LineHeight))
		canvas.DrawRect(ui.NewRectFromLTWH(offset.Dx+x1*pixel, offset.Dy+y*pixel, (x2-x1)*pixel, float64(info.LineHeight)*pixel), paint)
	}
}

// minimapX is the x, in canvas pixels, of a byte column of a row's text: the gutter, then a
// block for each character, a tab as many as it takes to its tab stop, and a full-width character two.
//
// Ported from VS Code: minimap.ts (InnerMinimap.getXOffsetForPosition)
func minimapX(text string, col, tabSize, scale, canvasWidth int) float64 {
	x := minimapGutterWidth
	cw := minimapBaseCharWidth * scale
	for i, ch := range text {
		if i >= col {
			break
		}
		switch {
		case ch == '\t':
			x += max(tabSize, 1) * cw
		case isFullWidthRune(ch):
			x += 2 * cw
		default:
			x += cw
		}
		if x >= canvasWidth {
			return float64(canvasWidth)
		}
	}
	return float64(min(x, canvasWidth))
}
