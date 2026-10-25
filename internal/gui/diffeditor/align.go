package diffeditor

import (
	"math"

	"github.com/rm4n0s/emetos/internal/core/diff"
)

// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/components/diffEditorViewZones/diffEditorViewZones.ts
// (computeRangeAlignment and getAdditionalLineHeights). VS Code works in pixels; the editor of Emetos aligns by
// whole rows (SPEC §9.1 "View zones as built"), so a height is a number of rows and the extra height of a wrapped
// line is its rows less one. This file imports no Glutter: the alignment is plain data.

// LineExtra is a line of a side that is taller than one row because it is wrapped: Rows is the number of rows it
// has more than one.
type LineExtra struct {
	// Line is the line number, counted from 1.
	Line int
	// Rows is the rows of the line after the first.
	Rows int
}

// Alignment is a stretch of both texts that must be as tall on both sides: after it the same row of the screen
// shows the end of both ranges. Inside a change it is the change (or the part of it that inner changes cut at
// lines they share); outside the changes it is one line of each side (only where wrapped lines are taller than
// their counterparts).
type Alignment struct {
	// Original and Modified are the line ranges (counted from 1, the end excluded) the alignment covers.
	Original, Modified diff.LineRange
	// OriginalRows and ModifiedRows are the rows the ranges take, wrapped lines counted.
	OriginalRows, ModifiedRows int
	// Change is the change the alignment is part of; nil outside the changes.
	Change *diff.LineRangeMapping
}

// AlignOptions are the options of ComputeAlignment.
type AlignOptions struct {
	// Original and Modified are the wrapped lines of each side, sorted by line. Without wrapping they are empty.
	Original, Modified []LineExtra
	// InnerHunkAlignment cuts a change at the lines its character changes share between the sides: the side by
	// side view sets it (VS Code's `innerHunkAlignment`), so that a line that is changed in the middle stays next
	// to its counterpart when the hunk has more lines on one side.
	InnerHunkAlignment bool
	// OriginalLineLength gives the length in bytes of a line of the original text, counted from 1; false for a
	// line the text does not have (a diff that is older than the text). Without it every line is taken to be
	// longer than any change.
	OriginalLineLength func(line int) (int, bool)
}

// extraQueue is VS Code's ArrayQueue of line heights.
type extraQueue struct {
	items []LineExtra
	pos   int
}

func (q *extraQueue) peek() (LineExtra, bool) {
	if q.pos >= len(q.items) {
		return LineExtra{}, false
	}
	return q.items[q.pos], true
}

func (q *extraQueue) dequeue() { q.pos++ }

// takeWhile takes the items before a line and gives the rows they add up to.
func (q *extraQueue) takeWhile(before int) int {
	sum := 0
	for q.pos < len(q.items) && q.items[q.pos].Line < before {
		sum += q.items[q.pos].Rows
		q.pos++
	}
	return sum
}

// ComputeAlignment is VS Code's computeRangeAlignment: it cuts the texts into the stretches that line up. The
// changes are those of a diff (sorted, apart).
func ComputeAlignment(changes []diff.LineRangeMapping, opts AlignOptions) []Alignment {
	origQ := &extraQueue{items: opts.Original}
	modQ := &extraQueue{items: opts.Modified}
	var result []Alignment
	lastOrig, lastMod := 0, 0

	outside := func(untilOrig, untilMod int) {
		for {
			origNext, haveOrig := origQ.peek()
			modNext, haveMod := modQ.peek()
			if haveOrig && origNext.Line >= untilOrig {
				haveOrig = false
			}
			if haveMod && modNext.Line >= untilMod {
				haveMod = false
			}
			if !haveOrig && !haveMod {
				return
			}
			distOrig, distMod := math.MaxInt, math.MaxInt
			if haveOrig {
				distOrig = origNext.Line - lastOrig
			}
			if haveMod {
				distMod = modNext.Line - lastMod
			}
			switch {
			case distOrig < distMod:
				origQ.dequeue()
				modNext = LineExtra{Line: origNext.Line - lastOrig + lastMod}
			case distOrig > distMod:
				modQ.dequeue()
				origNext = LineExtra{Line: modNext.Line - lastMod + lastOrig}
			default:
				origQ.dequeue()
				modQ.dequeue()
			}
			result = append(result, Alignment{
				Original:     diff.LineRange{StartLine: origNext.Line, EndLine: origNext.Line + 1},
				Modified:     diff.LineRange{StartLine: modNext.Line, EndLine: modNext.Line + 1},
				OriginalRows: 1 + origNext.Rows, ModifiedRows: 1 + modNext.Rows,
			})
		}
	}

	for i := range changes {
		c := &changes[i]
		outside(c.Original.StartLine, c.Modified.StartLine)

		first := true
		lastModLine, lastOrigLine := c.Modified.StartLine, c.Original.StartLine
		emit := func(origExcl, modExcl int, force bool) {
			if origExcl < lastOrigLine || modExcl < lastModLine {
				return
			}
			if first {
				first = false
			} else if !force && (origExcl == lastOrigLine || modExcl == lastModLine) {
				// this would align a line that is aligned already; the final alignment is always made
				return
			}
			orig := diff.LineRange{StartLine: lastOrigLine, EndLine: origExcl}
			mod := diff.LineRange{StartLine: lastModLine, EndLine: modExcl}
			if orig.IsEmpty() && mod.IsEmpty() {
				return
			}
			result = append(result, Alignment{
				Original: orig, Modified: mod,
				OriginalRows: orig.Len() + origQ.takeWhile(origExcl), ModifiedRows: mod.Len() + modQ.takeWhile(modExcl),
				Change: c,
			})
			lastOrigLine, lastModLine = origExcl, modExcl
		}

		if opts.InnerHunkAlignment {
			for _, in := range c.Inner {
				if in.Original.Start.Column > 1 && in.Modified.Start.Column > 1 {
					// there is text that did not change before the change, on its line
					emit(in.Original.Start.Line, in.Modified.Start.Line, false)
				}
				maxColumn := math.MaxInt
				if opts.OriginalLineLength != nil {
					if n, ok := opts.OriginalLineLength(in.Original.End.Line); ok {
						maxColumn = n + 1
					}
				}
				if in.Original.End.Column < maxColumn {
					// there is text that did not change after the change, on its line
					emit(in.Original.End.Line, in.Modified.End.Line, false)
				}
			}
		}
		emit(c.Original.EndLine, c.Modified.EndLine, true)

		lastOrig, lastMod = c.Original.EndLine, c.Modified.EndLine
	}
	outside(math.MaxInt, math.MaxInt)
	return result
}

// Zone is a stretch of filler rows to put in one side: Rows empty rows after the line with the number AfterLine
// (0: above the first line).
type Zone struct {
	AfterLine, Rows int
}

// Zones turns the alignments into the filler of each side: where one side takes fewer rows than the other for the
// same stretch, it gets the difference after the last line of its stretch. Zones that come after the same line are
// one.
func Zones(alignments []Alignment) (original, modified []Zone) {
	add := func(zones []Zone, after, rows int) []Zone {
		if n := len(zones); n > 0 && zones[n-1].AfterLine == after {
			zones[n-1].Rows += rows
			return zones
		}
		return append(zones, Zone{AfterLine: after, Rows: rows})
	}
	for _, a := range alignments {
		switch delta := a.ModifiedRows - a.OriginalRows; {
		case delta > 0:
			original = add(original, a.Original.EndLine-1, delta)
		case delta < 0:
			modified = add(modified, a.Modified.EndLine-1, -delta)
		}
	}
	return original, modified
}

// RowsBefore gives, for a side, the number of rows above the line with a number (counted from 1) when the side has
// the zones: the lines before it, the extra rows of the wrapped lines among them and the filler. It is how the
// tests check that an alignment makes both sides line up.
func RowsBefore(line int, extra []LineExtra, zones []Zone) int {
	rows := line - 1 + sumRowsBefore(extra, line)
	for _, z := range zones {
		if z.AfterLine < line {
			rows += z.Rows
		}
	}
	return rows
}

func sumRowsBefore(extra []LineExtra, line int) int {
	sum := 0
	for _, e := range extra {
		if e.Line < line {
			sum += e.Rows
		}
	}
	return sum
}
