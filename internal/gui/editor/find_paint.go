package editor

import (
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/find"
)

// Ported from VS Code: src/vs/editor/contrib/find/browser/findDecorations.ts and
// src/vs/editor/browser/viewParts/decorations/decorations.ts

// findDecorationsIn are the decorations of the find model in the lines between two rows.
func (vm *ViewModel) findDecorationsIn(firstRow, lastRow int) []document.Decoration {
	if !vm.FindRevealed() || vm.findw.model == nil {
		return nil
	}
	proj := vm.Projection()
	firstLine, _, _ := proj.RowSpan(firstRow)
	lastLine, _, _ := proj.RowSpan(lastRow)
	span := buffer.Range{Start: buffer.Position{Line: firstLine}, End: buffer.Position{Line: lastLine + 1}}
	return vm.doc.DecorationsIn(span, findOwner)
}

// paintFind draws the matches of the find widget: the lines of the current match and the search
// scope as whole lines, then the other matches, then the current one.
func (v overlayView) paintFind() {
	vm := v.geo.vm
	decs := vm.findDecorationsIn(v.first, v.last)
	if len(decs) == 0 {
		return
	}
	for _, class := range []string{find.ClassRangeHighlight, find.ClassFindScope} {
		for _, d := range decs {
			if d.Options.ClassName == class {
				v.paintWholeLines(d.Range, v.colors.FindRangeHighlight, v.colors.FindRangeHighlightBorder)
			}
		}
	}
	for _, class := range []string{find.ClassFindMatch, find.ClassCurrentFindMatch} {
		background, border := v.colors.FindMatchHighlight, v.colors.FindMatchHighlightBorder
		if class == find.ClassCurrentFindMatch {
			background, border = v.colors.FindMatch, v.colors.FindMatchBorder
		}
		for _, d := range decs {
			if _, _, hidden := vm.HiddenSpan(d.Range.Start.Line); !hidden && d.Options.ClassName == class {
				v.paintMatch(d.Range, background, border)
			}
		}
	}
}

// paintWholeLines fills the rows of the lines of a range across the layer.
func (v overlayView) paintWholeLines(r buffer.Range, background, border ui.Color) {
	vm := v.geo.vm
	end := r.End
	if end.Col == 0 && end.Line > r.Start.Line {
		end.Line--
	}
	first, last := vm.RowOfPosition(r.Start), vm.RowOfPosition(end)
	for row := max(first, v.first); row <= min(last, v.last); row++ {
		rect := v.rowRect(row, vm.ScrollLeft(), vm.ScrollLeft()+v.width)
		if background.Alpha() > 0 {
			v.canvas.DrawRect(rect, fill(background))
		}
		if border.Alpha() > 0 {
			v.paintOutline(rect, border)
		}
	}
}

// paintMatch fills the part of every row a match covers.
func (v overlayView) paintMatch(r buffer.Range, background, border ui.Color) {
	rows := v.geo.selectionRows(buffer.Selection{Anchor: r.Start, Active: r.End}, v.first, v.last, false)
	for _, row := range rows {
		if row.Row < v.first || row.Row > v.last {
			continue
		}
		rect := v.rowRect(row.Row, row.Left, row.Right)
		if background.Alpha() > 0 {
			v.canvas.DrawRect(rect, fill(background))
		}
		if border.Alpha() > 0 {
			v.paintOutline(rect, border)
		}
	}
}
