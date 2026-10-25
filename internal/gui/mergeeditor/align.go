package mergeeditor

import (
	"slices"
	"sort"

	"github.com/rm4n0s/emetos/internal/core/diff"
)

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/view/lineAlignment.ts (getAlignments) and
// src/vs/workbench/contrib/mergeEditor/browser/view/viewZones.ts (computeViewZones: the spacers of the inputs).
// This file imports no Glutter: the alignment is plain data. VS Code works with pixel heights of lines; the editor of
// Emetos aligns by whole rows (SPEC §9.1 "View zones as built"), so a spacer is a number of rows and a wrapped line
// counts as one (the inputs of a merge are not re-aligned when lines wrap, as in VS Code).

// undef marks a line that an alignment does not have (a line of an input or of the base that is not there).
const undef = -1

// LineAlignment is a place where the base and the two inputs line up: the line numbers (counted from 1) of input 1,
// the base and input 2 that are at the same row, each undef when that text has no line there.
type LineAlignment [3]int

// textLength is VS Code's TextLength: lines and the columns after the last line break.
type textLength struct{ lines, columns int }

func (l textLength) isZero() bool { return l.lines == 0 && l.columns == 0 }

func (l textLength) greaterThan(o textLength) bool {
	if l.lines != o.lines {
		return l.lines > o.lines
	}
	return l.columns > o.columns
}

// lengthBetween is the length of the text between two positions.
func lengthBetween(a, b diff.Position) textLength {
	if a.Line == b.Line {
		return textLength{0, b.Column - a.Column}
	}
	return textLength{b.Line - a.Line, b.Column - 1}
}

// addLength is the position a length after a position.
func addLength(p diff.Position, l textLength) diff.Position {
	if l.lines == 0 {
		return diff.Position{Line: p.Line, Column: p.Column + l.columns}
	}
	return diff.Position{Line: p.Line + l.lines, Column: l.columns + 1}
}

func comparePositions(a, b diff.Position) int {
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	return a.Column - b.Column
}

// Alignments are the places inside a range of the base where input 1, the base and input 2 line up, from the
// character changes of both inputs (VS Code's getAlignments): the lines that neither input changed are at the same
// row, and a line that both changed only partly lines up at the start of its changed text. The first alignment is the
// start of the range and the last its end.
func Alignments(m diff.ModifiedBaseRange) []LineAlignment {
	eq1 := equalRangeMappings(innerMappings(m.Input1Diffs), rangeOf(m.Base), rangeOf(m.Input1))
	eq2 := equalRangeMappings(innerMappings(m.Input2Diffs), rangeOf(m.Base), rangeOf(m.Input2))
	common := splitUpCommon(eq1, eq2)

	result := []LineAlignment{{m.Input1.StartLine - 1, m.Base.StartLine - 1, m.Input2.StartLine - 1}}
	isFull := func(a LineAlignment) bool { return a[0] != undef && a[1] != undef && a[2] != undef }
	// shares tells whether an alignment has a line number that another one has at the same place
	shares := func(r, a LineAlignment) bool {
		for i := range r {
			if r[i] != undef && r[i] == a[i] {
				return true
			}
		}
		return false
	}

	// One base line has either up to one full sync or up to two half syncs.
	for _, c := range common {
		a := LineAlignment{undef, c.input.Line, undef}
		if c.out1 != nil {
			a[0] = c.out1.Line
		}
		if c.out2 != nil {
			a[2] = c.out2.Line
		}
		var shouldAdd bool
		if isFull(a) {
			isNew := !slices.ContainsFunc(result, func(r LineAlignment) bool { return isFull(r) && shares(r, a) })
			if isNew {
				// remove the half syncs
				result = slices.DeleteFunc(result, func(r LineAlignment) bool { return shares(r, a) })
			}
			shouldAdd = isNew
		} else {
			shouldAdd = !slices.ContainsFunc(result, func(r LineAlignment) bool { return shares(r, a) })
		}
		if shouldAdd {
			result = append(result, a)
		} else if c.length.greaterThan(textLength{1, 0}) {
			next := LineAlignment{undef, c.input.Line + 1, undef}
			if c.out1 != nil {
				next[0] = c.out1.Line + 1
			}
			if c.out2 != nil {
				next[2] = c.out2.Line + 1
			}
			result = append(result, next)
		}
	}

	final := LineAlignment{m.Input1.EndLine, m.Base.EndLine, m.Input2.EndLine}
	result = slices.DeleteFunc(result, func(r LineAlignment) bool {
		for i := range r {
			if r[i] == final[i] {
				return true
			}
		}
		return false
	})
	return append(result, final)
}

// innerMappings are the character changes of some line changes.
func innerMappings(diffs []diff.LineRangeMapping) []diff.RangeMapping {
	var out []diff.RangeMapping
	for _, d := range diffs {
		out = append(out, d.Inner...)
	}
	return out
}

// rangeOf is the range of text that covers whole lines (VS Code's toExclusiveRange).
func rangeOf(r diff.LineRange) diff.Range {
	return diff.Range{Start: diff.Position{Line: r.StartLine, Column: 1}, End: diff.Position{Line: r.EndLine, Column: 1}}
}

// equalRangeMappings are the stretches of text that the changes leave as they are: the mappings between them, from the
// range of the input to the range of the output (VS Code's toEqualRangeMappings).
func equalRangeMappings(diffs []diff.RangeMapping, input, output diff.Range) []diff.RangeMapping {
	var result []diff.RangeMapping
	inStart, outStart := input.Start, output.Start
	for _, d := range diffs {
		eq := diff.RangeMapping{
			Original: ordered(inStart, d.Original.Start),
			Modified: ordered(outStart, d.Modified.Start),
		}
		if eq.Original.Start != eq.Original.End {
			result = append(result, eq)
		}
		inStart, outStart = d.Original.End, d.Modified.End
	}
	eq := diff.RangeMapping{Original: ordered(inStart, input.End), Modified: ordered(outStart, output.End)}
	if eq.Original.Start != eq.Original.End {
		result = append(result, eq)
	}
	return result
}

// ordered is the range between two positions, the earlier first (VS Code's Range.fromPositions).
func ordered(a, b diff.Position) diff.Range {
	if comparePositions(b, a) < 0 {
		a, b = b, a
	}
	return diff.Range{Start: a, End: b}
}

// commonRange is a stretch of the base that both lists of equal ranges (or one of them) cover: where it starts in the
// base, in input 1 and in input 2 (nil for an input that does not have it equal there), and how long it is.
type commonRange struct {
	out1, out2 *diff.Position
	input      diff.Position
	length     textLength
}

// event is the start or the end of an equal range.
type event struct {
	input    int // 0 for input 1, 1 for input 2
	start    bool
	inputPos diff.Position
	outPos   diff.Position
}

// splitUpCommon cuts the base into the stretches over which each input has an equal range or not (VS Code's
// splitUpCommonEqualRangeMappings).
func splitUpCommon(eq1, eq2 []diff.RangeMapping) []commonRange {
	var events []event
	for i, list := range [][]diff.RangeMapping{eq1, eq2} {
		for _, rm := range list {
			events = append(events,
				event{input: i, start: true, inputPos: rm.Original.Start, outPos: rm.Modified.Start},
				event{input: i, start: false, inputPos: rm.Original.End, outPos: rm.Modified.End})
		}
	}
	sort.SliceStable(events, func(a, b int) bool { return comparePositions(events[a].inputPos, events[b].inputPos) < 0 })

	var result []commonRange
	var starts [2]*diff.Position
	var last *diff.Position
	for _, ev := range events {
		if last != nil && (starts[0] != nil || starts[1] != nil) {
			length := lengthBetween(*last, ev.inputPos)
			if !length.isZero() {
				result = append(result, commonRange{input: *last, length: length, out1: clonePos(starts[0]), out2: clonePos(starts[1])})
				for i := range starts {
					if starts[i] != nil {
						p := addLength(*starts[i], length)
						starts[i] = &p
					}
				}
			}
		}
		if ev.start {
			p := ev.outPos
			starts[ev.input] = &p
		} else {
			starts[ev.input] = nil
		}
		p := ev.inputPos
		last = &p
	}
	return result
}

func clonePos(p *diff.Position) *diff.Position {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

// Spacer is a stretch of empty rows to put in an input: Rows rows before the line with the number Before (counted
// from 1), that is, after the line Before-1.
type Spacer struct {
	Before, Rows int
}

// InputSpacers are the empty rows that line up the two inputs of a merge (VS Code's ViewZoneComputer in the mixed
// layout, where the result and the base are not lined up with the inputs): through each range where an input differs
// from the base, the lines that are the same in both inputs are at the same row. The ranges are those of the model, in
// order. Spacers are cumulative: the rows added above are counted in the position of what follows, as VS Code does.
func InputSpacers(ranges []diff.ModifiedBaseRange) (input1, input2 []Spacer) {
	var added1, added2 int
	for _, m := range ranges {
		for _, a := range Alignments(m) {
			if a[0] == undef || a[2] == undef {
				continue // the alignments of the base are not used: the base is not shown
			}
			line1, line2 := a[0]+added1, a[2]+added2
			most := max(line1, line2)
			if d := most - line1; d > 0 && a[0] >= 1 {
				input1 = append(input1, Spacer{Before: a[0], Rows: d})
				added1 += d
			}
			if d := most - line2; d > 0 && a[2] >= 1 {
				input2 = append(input2, Spacer{Before: a[2], Rows: d})
				added2 += d
			}
		}
	}
	return input1, input2
}
