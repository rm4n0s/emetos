package editor

import (
	"slices"

	"github.com/rm4n0s/glutter/ui"
)

// Line blocks (SPEC §9.1): a border around the rows of some lines, across the editor. The merge editor outlines
// its conflicts with them (step 9.7); an empty block (no lines) is a line between two lines, which is how an
// unhandled conflict whose base is empty shows in the result. A block is given by line numbers, not kept as a
// decoration of the document: the caller moves its blocks as its model changes.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/blockDecorations/blockDecorations.ts (the `blockClassName`
// decorations, drawn as a box with the border of the style sheet)

// LineBlock is a border around the lines [StartLine, EndLine) of an editor, counted from 0 as the lines of the
// document are; the two are equal for a block with no line, which is drawn as a line at the top of StartLine (at the
// end of the text when the line is past it).
type LineBlock struct {
	// StartLine is the first line and EndLine the line after the last.
	StartLine, EndLine int
	// Color is the color of the border and Width its width in pixels (1 when zero or less).
	Color ui.Color
	Width float64
}

// SetLineBlocks replaces the line blocks of the editor. The editor does not move them when the text changes.
func (vm *ViewModel) SetLineBlocks(blocks []LineBlock) {
	if slices.Equal(blocks, vm.blocks) {
		return
	}
	vm.blocks = slices.Clone(blocks)
	vm.NotifyListeners()
}

// LineBlocks are the line blocks of the editor.
func (vm *ViewModel) LineBlocks() []LineBlock { return slices.Clone(vm.blocks) }

// blockRect is where a block is, as the top and the bottom of its box in the document; ok is false for a block
// that has no line to be on in an empty text.
func (vm *ViewModel) blockRect(b LineBlock) (top, bottom float64, ok bool) {
	proj := vm.Projection()
	lines := vm.doc.Snapshot().LineCount()
	if lines == 0 {
		return 0, 0, false
	}
	width := max(b.Width, 1)
	if b.EndLine <= b.StartLine {
		// no line: a line between two lines
		var y float64
		if b.StartLine >= lines {
			first, count := proj.LineRows(lines - 1)
			last := first + max(count, 1) - 1
			y = proj.RowTop(last) + proj.RowHeight(last)
		} else {
			first, _ := proj.LineRows(b.StartLine)
			y = proj.RowTop(first)
		}
		return y - width, y + width, true
	}
	start, end := min(max(b.StartLine, 0), lines-1), min(b.EndLine, lines)
	first, _ := proj.LineRows(start)
	lastFirst, count := proj.LineRows(end - 1)
	last := lastFirst + max(count, 1) - 1
	return proj.RowTop(first), proj.RowTop(last) + proj.RowHeight(last), true
}

// paintLineBlocks draws the borders of the blocks that reach the visible rows.
func (v overlayView) paintLineBlocks() {
	vm := v.geo.vm
	if len(vm.blocks) == 0 {
		return
	}
	for _, b := range vm.blocks {
		if b.Color.Alpha() == 0 {
			continue
		}
		top, bottom, ok := vm.blockRect(b)
		if !ok || bottom < v.geo.proj.RowTop(v.first) || top > v.geo.proj.RowTop(v.last)+v.geo.proj.RowHeight(v.last) {
			continue
		}
		width := max(b.Width, 1)
		left, right := v.origin.Dx+vm.ScrollLeft(), v.origin.Dx+vm.ScrollLeft()+v.width
		top, bottom = v.origin.Dy+top, v.origin.Dy+bottom
		paint := fill(b.Color)
		v.canvas.DrawRect(ui.NewRectFromLTRB(left, top, right, top+width), paint)
		v.canvas.DrawRect(ui.NewRectFromLTRB(left, bottom-width, right, bottom), paint)
		if b.EndLine > b.StartLine {
			v.canvas.DrawRect(ui.NewRectFromLTRB(left, top+width, left+width, bottom-width), paint)
			v.canvas.DrawRect(ui.NewRectFromLTRB(right-width, top+width, right, bottom-width), paint)
		}
	}
}
