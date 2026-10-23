package edit

import "github.com/rm4n0s/emetos/internal/core/buffer"

// The functions below are the selection-by-unit part of the mouse (SPEC §9.3). A double or
// triple click enters a word or a line, which is the range the selection extends from while the
// button stays down; dragging moves the other end by whole words or lines.

// RangeSelection is the selection from an initial range to a position, as VS Code's
// SingleCursorState computes it: the anchor is the start of the range, or its end when the
// position is before its start.
//
// Ported from VS Code: src/vs/editor/common/cursorCommon.ts (SingleCursorState._computeSelection)
func RangeSelection(initial buffer.Range, p buffer.Position) buffer.Selection {
	if initial.IsEmpty() || !p.IsBeforeOrEqual(initial.Start) {
		return buffer.Selection{Anchor: initial.Start, Active: p}
	}
	return buffer.Selection{Anchor: initial.End, Active: p}
}

// WordSelect is the range a double click at a position enters: the word the position touches,
// or the gap between two words when it touches none. The selection it makes has the position
// at the end of the range.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorWordOperations.ts (WordOperations.word)
func (mv Mover) WordSelect(p buffer.Position) buffer.Range {
	p = mv.Model.ValidatePosition(p)
	line := mv.Model.LineContent(p.Line)
	classes := mv.Options.words()
	prev, hasPrev := classes.PrevWord(line, p.Col)
	next, hasNext := classes.NextWord(line, p.Col)

	// touching is whether a word covers the column; a word of separators does not include the
	// column where it ends.
	touching := func(w Word) bool {
		if w.Kind == RegularWord {
			return w.Start <= p.Col && p.Col <= w.End
		}
		return w.Start <= p.Col && p.Col < w.End
	}
	var start, end int
	switch {
	case hasPrev && touching(prev):
		start, end = prev.Start, prev.End
	case hasNext && touching(next):
		start, end = next.Start, next.End
	default:
		start, end = 0, len(line)
		if hasPrev {
			start = prev.End
		}
		if hasNext {
			end = next.Start
		}
	}
	return buffer.Range{Start: posAt(p.Line, start), End: posAt(p.Line, end)}
}

// WordSelectDrag is the selection while a double click is dragged to a position: it extends
// from the word that was entered by whole words. A position inside a word reaches the word's
// far end; one on a boundary reaches the boundary.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorWordOperations.ts (WordOperations.word)
func (mv Mover) WordSelectDrag(initial buffer.Range, p buffer.Position) buffer.Selection {
	p = mv.Model.ValidatePosition(p)
	line := mv.Model.LineContent(p.Line)
	classes := mv.Options.words()
	prev, hasPrev := classes.PrevWord(line, p.Col)
	next, hasNext := classes.NextWord(line, p.Col)

	start, end := p.Col, p.Col
	switch {
	case hasPrev && prev.Kind == RegularWord && prev.Start < p.Col && p.Col < prev.End:
		start, end = prev.Start, prev.End
	case hasNext && next.Kind == RegularWord && next.Start < p.Col && p.Col < next.End:
		start, end = next.Start, next.End
	}

	col := end
	switch {
	case initial.ContainsPosition(p):
		col = initial.End.Col
	case p.IsBeforeOrEqual(initial.Start):
		col = start
		if initial.ContainsPosition(posAt(p.Line, col)) {
			col = initial.End.Col
		}
	default:
		if initial.ContainsPosition(posAt(p.Line, col)) {
			col = initial.Start.Col
		}
	}
	return RangeSelection(initial, posAt(p.Line, col))
}

// LineSelect is the range a triple click on a line enters: the whole line with its line break,
// or up to the end of the text on the last line.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorMoveCommands.ts (CursorMoveCommands.line)
func (mv Mover) LineSelect(line int) buffer.Range {
	line = min(max(line, 0), lastLine(mv.Model))
	end := posAt(line+1, 0)
	if line+1 > lastLine(mv.Model) {
		end = lineEnd(mv.Model, line)
	}
	return buffer.Range{Start: posAt(line, 0), End: end}
}

// LineSelectDrag is the selection while a triple click (or a press on a line number) is dragged
// to a position: whole lines from the line that was entered to the line of the position.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorMoveCommands.ts (CursorMoveCommands.line)
func (mv Mover) LineSelectDrag(initial buffer.Range, p buffer.Position) buffer.Selection {
	p = mv.Model.ValidatePosition(p)
	switch {
	case p.Line < initial.Start.Line:
		return RangeSelection(initial, posAt(p.Line, 0))
	case p.Line > initial.Start.Line:
		return RangeSelection(initial, mv.LineSelect(p.Line).End)
	default:
		return RangeSelection(initial, initial.End)
	}
}
