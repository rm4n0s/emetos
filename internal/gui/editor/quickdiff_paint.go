package editor

import (
	"math"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
)

// The drawing of the quick diff marks in the gutter, on the overview ruler and in the minimap (see quickdiff.go).

// QuickDiffColors are the colors of the quick diff stripes in the gutter.
type QuickDiffColors struct {
	// Added, Modified and Deleted are the colors of the primary provider's changes; the Secondary ones those of a secondary provider's.
	Added, Modified, Deleted                            ui.Color
	AddedSecondary, ModifiedSecondary, DeletedSecondary ui.Color
}

func (c QuickDiffColors) of(m QuickDiffMark) ui.Color {
	switch {
	case m.Type == quickdiff.Add && m.Secondary:
		return c.AddedSecondary
	case m.Type == quickdiff.Add:
		return c.Added
	case m.Type == quickdiff.Modify && m.Secondary:
		return c.ModifiedSecondary
	case m.Type == quickdiff.Modify:
		return c.Modified
	case m.Secondary:
		return c.DeletedSecondary
	}
	return c.Deleted
}

const (
	// quickDiffStripeLeft is the space between the start of the line decorations and the stripe (VS Code's margin-left of 5px).
	quickDiffStripeLeft = 5.0
	// quickDiffTriangle is the half height of the triangle of deleted lines and its width (VS Code's border of 4px).
	quickDiffTriangle = 4.0
)

// shownMarks are the marks the editor draws: a secondary mark whose lines touch the lines of a primary one is left out (VS Code's
// "Overlap with primary quick diff changes").
func (vm *ViewModel) shownMarks() []placedMark {
	placed := vm.placedMarks()
	var primary []placedMark
	for _, p := range placed {
		if !p.Secondary {
			primary = append(primary, p)
		}
	}
	if len(primary) == len(placed) {
		return placed
	}
	out := make([]placedMark, 0, len(placed))
	for _, p := range placed {
		if p.Secondary && touchesAny(p, primary) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// span is the lines of a mark as a range of lines that ends after its last line; a deleted mark covers no line (it sits between two).
func (p placedMark) span() (start, end int) {
	if p.Type == quickdiff.Delete {
		return p.first + 1, p.first + 1
	}
	return p.first, p.last + 1
}

func touchesAny(p placedMark, others []placedMark) bool {
	ps, pe := p.span()
	for _, o := range others {
		os, oe := o.span()
		if max(ps, os) <= min(pe, oe) {
			return true
		}
	}
	return false
}

// paintQuickDiff draws the stripes of the marks in the gutter, next to the text: a bar along the rows of an added or modified change, and
// a small triangle at the bottom of the line that lines were deleted after.
func (r *RenderEditorGutter) paintQuickDiff(canvas *ui.Canvas, offset ui.Offset, top float64, first, last int, widths GutterWidths) {
	opts := r.vm.Options()
	qd := opts.QuickDiff
	if !qd.gutter() || len(r.vm.qd.ids) == 0 || (qd.Visibility == "hover" && !r.vm.fold.hoverGutter) {
		return
	}
	proj := r.vm.Projection()
	left := widths.GlyphMargin + widths.LineNumbers
	if opts.Folding {
		left += foldingControlsWidth
	}
	x := offset.Dx + left + quickDiffStripeLeft
	width := float64(qd.width())
	for _, p := range r.vm.shownMarks() {
		firstRow, lastRow := wholeLineRows(r.vm, buffer.Range{Start: buffer.Position{Line: p.first}, End: buffer.Position{Line: p.last}})
		if lastRow < first || firstRow > last {
			continue
		}
		color := r.colors.QuickDiff.of(p.QuickDiffMark)
		if color.Alpha() == 0 {
			continue
		}
		if p.Type == quickdiff.Delete {
			y := offset.Dy + proj.RowTop(lastRow) + proj.RowHeight(lastRow) - top
			path := ui.NewPath()
			path.MoveTo(x, y-quickDiffTriangle)
			path.LineTo(x+quickDiffTriangle, y)
			path.LineTo(x, y+quickDiffTriangle)
			path.Close()
			canvas.DrawPath(path, fill(color))
			continue
		}
		pattern := (p.Type == quickdiff.Add && qd.PatternAdded) || (p.Type == quickdiff.Modify && qd.PatternModified)
		// the rows of the change that are text (a view zone among them has no stripe), as runs
		runStart, runEnd := -1, -1
		flush := func() {
			if runStart < 0 {
				return
			}
			y0, y1 := offset.Dy+proj.RowTop(runStart)-top, offset.Dy+proj.RowTop(runEnd)+proj.RowHeight(runEnd)-top
			if pattern {
				paintHatch(canvas, x, y0, y1, int(width), color)
			} else {
				canvas.DrawRect(ui.NewRectFromLTWH(x, y0, width, y1-y0), fill(color))
			}
			runStart = -1
		}
		for row := max(firstRow, first); row <= min(lastRow, last); row++ {
			if _, _, zone := proj.RowZone(row); zone {
				flush()
				continue
			}
			if runStart < 0 {
				runStart = row
			}
			runEnd = row
		}
		flush()
	}
}

// paintHatch draws the stripe as VS Code's pattern does: tiles of w by w pixels with the diagonal bands of
// linear-gradient(-45deg, c 25%, bg 25%, bg 50%, c 50%, c 75%, bg 75%, bg), the bands of the color c only (the gutter is behind), the tiles
// starting at the top of the stripe.
func paintHatch(canvas *ui.Canvas, x, y0, y1 float64, w int, color ui.Color) {
	if w < 1 || y1 <= y0 {
		return
	}
	paint := fill(color)
	for py := 0; py < w; py++ {
		for px := 0; px < w; px++ {
			// t is the place of the pixel's center along the gradient line of a square tile, 0 at the corner it starts from
			t := 1 - float64(px+py+1)/float64(2*w)
			if !(t < 0.25 || (t >= 0.5 && t < 0.75)) {
				continue
			}
			for y := y0 + float64(py); y < y1; y += float64(w) {
				canvas.DrawRect(ui.NewRectFromLTWH(x+float64(px), y, 1, math.Min(1, y1-y)), paint)
			}
		}
	}
}

// quickDiffRulerMarks are the marks of the quick diff on the left lane of the overview ruler, for a ruler of a height: a run for each
// stretch of the ruler that a mark of one kind covers.
func (vm *ViewModel) quickDiffRulerMarks(height float64) []rulerMark {
	qd := vm.opts.QuickDiff
	if !qd.overview() || len(vm.qd.ids) == 0 || height <= 0 {
		return nil
	}
	total := vm.ContentHeight()
	if total <= 0 {
		return nil
	}
	lines := vm.doc.Snapshot().LineCount()
	version := vm.doc.Version()
	c := &vm.qd.ruler
	if c.valid && c.generation == vm.qd.generation && c.version == version && c.height == height && c.total == total && c.lines == lines {
		return c.marks
	}
	proj := vm.Projection()
	scale := height / total
	var marks []rulerMark
	for _, p := range vm.shownMarks() {
		firstRow, lastRow := wholeLineRows(vm, buffer.Range{Start: buffer.Position{Line: p.first}, End: buffer.Position{Line: p.last}})
		y := math.Round(proj.RowTop(firstRow) * scale)
		h := max(math.Round((proj.RowTop(lastRow)+proj.RowHeight(lastRow))*scale)-y, rulerMinMarkHeight)
		if n := len(marks); n > 0 && marks[n-1].change == p.Type && y <= marks[n-1].y+marks[n-1].height {
			marks[n-1].height = max(marks[n-1].height, y+h-marks[n-1].y)
			continue
		}
		marks = append(marks, rulerMark{y: y, height: h, lane: laneLeft, change: p.Type})
	}
	*c = rulerCache{valid: true, generation: vm.qd.generation, version: version, height: height, total: total, lines: lines, marks: marks}
	return marks
}

// quickDiffMinimapColors are the colors of the marks in the gutter of the minimap.
type quickDiffMinimapColors struct{ Added, Modified, Deleted ui.Color }

func (c quickDiffMinimapColors) of(t quickdiff.ChangeType) ui.Color {
	switch t {
	case quickdiff.Add:
		return c.Added
	case quickdiff.Modify:
		return c.Modified
	}
	return c.Deleted
}

// minimapGutterMarkWidth and minimapGutterMarkLeft are the width of a mark in the gutter of the minimap and its distance from the left
// edge, in canvas pixels (VS Code's GUTTER_DECORATION_WIDTH and the x of 2).
const (
	minimapGutterMarkWidth = 2.0
	minimapGutterMarkLeft  = 2.0
)

// paintQuickDiff draws the marks in the gutter of the minimap, along the lines of each change.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/minimap/minimap.ts (InnerMinimap.renderDecorations, MinimapPosition.Gutter)
func (r *RenderEditorMinimap) paintQuickDiff(canvas *ui.Canvas, offset ui.Offset, l minimapRenderLayout, pixel float64, firstRow, lastRow int) {
	vm := r.vm
	if !vm.opts.QuickDiff.minimap() || len(vm.qd.ids) == 0 {
		return
	}
	proj := vm.Projection()
	info := vm.mm.info
	for _, p := range vm.shownMarks() {
		color := r.colors.QuickDiff.of(p.Type)
		if color.Alpha() == 0 {
			continue
		}
		first, last := wholeLineRows(vm, buffer.Range{Start: buffer.Position{Line: p.first}, End: buffer.Position{Line: p.last}})
		if p.Type == quickdiff.Delete {
			first = last
		}
		paint := fill(color)
		for row := max(first, firstRow); row <= min(last, lastRow) && row < proj.RowCount(); row++ {
			if _, _, zone := proj.RowZone(row); zone {
				continue
			}
			mmLine := vm.minimapLineOfRow(row)
			if mmLine < l.StartLine || mmLine > l.EndLine {
				continue
			}
			y := float64(l.yForLine(mmLine, info.LineHeight))
			canvas.DrawRect(ui.NewRectFromLTWH(offset.Dx+minimapGutterMarkLeft*pixel, offset.Dy+y*pixel, minimapGutterMarkWidth*pixel, float64(info.LineHeight)*pixel), paint)
		}
	}
}
