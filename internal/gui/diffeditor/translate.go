package diffeditor

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
)

// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/diffEditorWidget.ts (translatePosition and
// mapToOtherSide)

// Side is one of the two editors of the diff editor.
type Side int

const (
	// Original is the editor on the left, which shows the text the changes start from.
	Original Side = iota
	// Modified is the editor on the right, which shows the text the changes lead to.
	Modified
)

// Other is the other side.
func (s Side) Other() Side { return 1 - s }

// String names the side.
func (s Side) String() string {
	if s == Original {
		return "original"
	}
	return "modified"
}

// TranslatePosition maps a position of one side to the range it corresponds to on the other side, through the
// changes of the diff: a position after a change moves by the lines the changes added or removed; in a change it
// goes to the start of the other side's change, or, when the change has character changes, into the one at or
// before the position. from tells which side the position is of. Lines are counted from 0 and columns are bytes,
// as the buffer's positions are.
func TranslatePosition(changes []diff.LineRangeMapping, from Side, p buffer.Position) buffer.Range {
	line := p.Line + 1
	srcOf := func(c diff.LineRangeMapping) (src, dst diff.LineRange) {
		if from == Original {
			return c.Original, c.Modified
		}
		return c.Modified, c.Original
	}
	at := func(l, col int) buffer.Position { return buffer.Position{Line: l - 1, Col: col} }
	cursor := buffer.Range{Start: p, End: p}
	idx := -1
	for i, c := range changes {
		if src, _ := srcOf(c); src.StartLine <= line {
			idx = i
		}
	}
	if idx < 0 {
		return cursor
	}
	c := changes[idx]
	src, dst := srcOf(c)
	if src.EndLine <= line {
		pos := at(line-src.EndLine+dst.EndLine, p.Col)
		return buffer.Range{Start: pos, End: pos}
	}
	if len(c.Inner) == 0 {
		pos := at(dst.StartLine, 0)
		return buffer.Range{Start: pos, End: pos}
	}
	rangeOf := func(m diff.RangeMapping) (srcRange, dstRange diff.Range) {
		if from == Original {
			return m.Original, m.Modified
		}
		return m.Modified, m.Original
	}
	toBuffer := func(pos diff.Position) buffer.Position {
		return buffer.Position{Line: pos.Line - 1, Col: pos.Column - 1}
	}
	inner := -1
	for i, m := range c.Inner {
		sr, _ := rangeOf(m)
		if toBuffer(sr.Start).IsBeforeOrEqual(p) {
			inner = i
		}
	}
	if inner < 0 {
		pos := at(p.Line-src.StartLine+1+dst.StartLine, p.Col)
		return buffer.Range{Start: pos, End: pos}
	}
	sr, dr := rangeOf(c.Inner[inner])
	if toBuffer(sr.Start).IsBeforeOrEqual(p) && p.IsBeforeOrEqual(toBuffer(sr.End)) {
		return buffer.Range{Start: toBuffer(dr.Start), End: toBuffer(dr.End)}
	}
	// after the character change: the distance from its end, in lines and, on the same line, in columns
	end := toBuffer(sr.End)
	dEnd := toBuffer(dr.End)
	var pos buffer.Position
	if p.Line == end.Line {
		pos = buffer.Position{Line: dEnd.Line, Col: dEnd.Col + p.Col - end.Col}
	} else {
		pos = buffer.Position{Line: dEnd.Line + p.Line - end.Line, Col: p.Col}
	}
	return buffer.Range{Start: pos, End: pos}
}
