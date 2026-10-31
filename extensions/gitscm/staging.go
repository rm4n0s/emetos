package gitscm

import (
	"slices"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// Staging by lines (SPEC §10.30 "Stage and unstage", "The peek"): the text of the index with some of the changes of a file applied, built
// from the original text, the modified text and the changed blocks of lines, and the choice of the blocks a selection touches. The
// functions here know nothing of git: staging.go's commands (stagecmds.go) read the versions, ask the host for the blocks and write the
// result to the index.
//
// Ported from VS Code: extensions/git/src/staging.ts (applyLineChanges, toLineRanges, getModifiedRange, intersectDiffWithRange,
// invertLineChange, compareLineChanges)

// textPos is a place in a text: a line from 0 and a column in bytes from 0.
type textPos struct{ line, col int }

func (a textPos) before(b textPos) bool { return a.line < b.line || a.line == b.line && a.col < b.col }

// textRange is the text from start up to end.
type textRange struct{ start, end textPos }

// ordered is the range with its ends in order (VS Code's Range constructor does this).
func (r textRange) ordered() textRange {
	if r.end.before(r.start) {
		return textRange{r.end, r.start}
	}
	return r
}

// contains tells whether the position is in the range, its ends included.
func (r textRange) contains(p textPos) bool { return !p.before(r.start) && !r.end.before(p) }

// intersection is the part both ranges have; false when they have none (ranges that touch have an empty one).
func (r textRange) intersection(o textRange) (textRange, bool) {
	start, end := r.start, r.end
	if start.before(o.start) {
		start = o.start
	}
	if o.end.before(end) {
		end = o.end
	}
	if end.before(start) {
		return textRange{}, false
	}
	return textRange{start, end}, true
}

// textDoc is a text split into lines the way VS Code's TextDocument splits it: a line break is "\r\n", "\n" or "\r", and a text that ends
// with one has an empty last line. Columns count bytes.
type textDoc struct {
	text string
	// starts and ends are the offsets of the start of each line and of its end before its line break.
	starts, ends []int
}

// newTextDoc splits a text.
func newTextDoc(text string) *textDoc {
	d := &textDoc{text: text}
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\r':
			d.starts, d.ends = append(d.starts, start), append(d.ends, i)
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			start = i + 1
		case '\n':
			d.starts, d.ends = append(d.starts, start), append(d.ends, i)
			start = i + 1
		}
	}
	d.starts, d.ends = append(d.starts, start), append(d.ends, len(text))
	return d
}

// lineCount is the number of lines.
func (d *textDoc) lineCount() int { return len(d.starts) }

// lineEnd is the column at the end of a line, before its line break; lines outside the text are empty.
func (d *textDoc) lineEnd(line int) int {
	if line < 0 || line >= len(d.starts) {
		return 0
	}
	return d.ends[line] - d.starts[line]
}

// offset is the place in the text of a position; a position outside the text is moved into it.
func (d *textDoc) offset(line, col int) int {
	switch {
	case line < 0:
		return 0
	case line >= len(d.starts):
		return len(d.text)
	}
	return d.starts[line] + max(0, min(col, d.lineEnd(line)))
}

// slice is the text between two positions.
func (d *textDoc) slice(l1, c1, l2, c2 int) string {
	a, b := d.offset(l1, c1), d.offset(l2, c2)
	if b < a {
		a, b = b, a
	}
	return d.text[a:b]
}

// applyLineChanges is the original text with the changes applied: the lines of each change come from the modified text, the rest from the
// original. A line is counted from 1 in the changes, as in extension.LineChange.
//
// Ported from VS Code: extensions/git/src/staging.ts (applyLineChanges)
func applyLineChanges(original, modified *textDoc, diffs []extension.LineChange) string {
	var out strings.Builder
	current := 0
	for _, d := range diffs {
		isInsertion := d.OriginalEndLineNumber == 0
		isDeletion := d.ModifiedEndLineNumber == 0
		endLine := d.OriginalStartLineNumber - 1
		if isInsertion {
			endLine = d.OriginalStartLineNumber
		}
		endCol := 0
		// a deletion at the very end of the text may have removed the line break of the last line left
		if isDeletion && d.OriginalEndLineNumber == original.lineCount() {
			endLine--
			endCol = original.lineEnd(endLine)
		}
		out.WriteString(original.slice(current, 0, endLine, endCol))
		if !isDeletion {
			fromLine, fromCol := d.ModifiedStartLineNumber-1, 0
			// an insertion at the very end of the text starts after the last character of the line before, to take its line break
			if isInsertion && d.OriginalStartLineNumber == original.lineCount() {
				fromLine--
				fromCol = modified.lineEnd(fromLine)
			}
			out.WriteString(modified.slice(fromLine, fromCol, d.ModifiedEndLineNumber, 0))
		}
		if isInsertion {
			current = d.OriginalStartLineNumber
		} else {
			current = d.OriginalEndLineNumber
		}
	}
	out.WriteString(original.slice(current, 0, original.lineCount(), 0))
	return out.String()
}

// toLineRanges are the whole lines the selections touch, in order, with overlapping and adjacent ones joined (VS Code joins overlapping ones
// into their intersection, which can drop lines; the union is what the lines of a selection mean).
//
// Ported from VS Code: extensions/git/src/staging.ts (toLineRanges)
func toLineRanges(selections []extension.Selection, doc *textDoc) []textRange {
	ranges := make([]textRange, 0, len(selections))
	for _, s := range selections {
		start, end := s.Anchor, s.Active
		if end.Line < start.Line || end.Line == start.Line && end.Col < start.Col {
			start, end = end, start
		}
		ranges = append(ranges, textRange{textPos{start.Line, 0}, textPos{end.Line, doc.lineEnd(end.Line)}})
	}
	slices.SortStableFunc(ranges, func(a, b textRange) int { return a.start.line - b.start.line })
	var out []textRange
	for _, r := range ranges {
		if n := len(out); n > 0 && r.start.line <= out[n-1].end.line+1 {
			if out[n-1].end.before(r.end) {
				out[n-1].end = r.end
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// modifiedRange is the range of the modified text a change covers: its lines, and for a deletion the place where the lines were.
//
// Ported from VS Code: extensions/git/src/staging.ts (getModifiedRange)
func modifiedRange(doc *textDoc, d extension.LineChange) textRange {
	if d.ModifiedEndLineNumber != 0 {
		return textRange{textPos{d.ModifiedStartLineNumber - 1, 0}, textPos{d.ModifiedEndLineNumber - 1, doc.lineEnd(d.ModifiedEndLineNumber - 1)}}
	}
	switch {
	case d.ModifiedStartLineNumber == 0:
		return textRange{textPos{0, 0}, textPos{0, doc.lineEnd(0)}}
	case doc.lineCount() == d.ModifiedStartLineNumber:
		end := textPos{d.ModifiedStartLineNumber - 1, doc.lineEnd(d.ModifiedStartLineNumber - 1)}
		return textRange{end, end}
	}
	return textRange{textPos{d.ModifiedStartLineNumber - 1, doc.lineEnd(d.ModifiedStartLineNumber - 1)}, textPos{d.ModifiedStartLineNumber, 0}}
}

// intersectChange is the part of a change a range of the modified text covers; false when the range does not touch the change. A deletion
// is taken whole. A change with as many lines on both sides is cut line by line, any other keeps its original lines.
//
// Ported from VS Code: extensions/git/src/staging.ts (intersectDiffWithRange)
func intersectChange(doc *textDoc, d extension.LineChange, r textRange) (extension.LineChange, bool) {
	hit, ok := r.intersection(modifiedRange(doc, d))
	if !ok {
		return extension.LineChange{}, false
	}
	if d.ModifiedEndLineNumber == 0 {
		return d, true
	}
	start, end := hit.start.line+1, hit.end.line+1
	out := d
	out.ModifiedStartLineNumber, out.ModifiedEndLineNumber = start, end
	// same number of lines on both sides: assume line by line
	if d.OriginalEndLineNumber-d.OriginalStartLineNumber == d.ModifiedEndLineNumber-d.ModifiedStartLineNumber {
		delta, length := start-d.ModifiedStartLineNumber, end-start
		out.OriginalStartLineNumber = d.OriginalStartLineNumber + delta
		out.OriginalEndLineNumber = d.OriginalStartLineNumber + delta + length
	}
	return out, true
}

// selectedChanges are the changes the selected lines touch, cut to the lines (one change for each change of the file the lines touch, the
// first line range that touches it deciding).
func selectedChanges(doc *textDoc, changes []extension.LineChange, lines []textRange) []extension.LineChange {
	var out []extension.LineChange
	for _, c := range changes {
		for _, r := range lines {
			if part, ok := intersectChange(doc, c, r); ok {
				out = append(out, part)
				break
			}
		}
	}
	return out
}

// invertLineChange is the change seen from the other side.
//
// Ported from VS Code: extensions/git/src/staging.ts (invertLineChange)
func invertLineChange(d extension.LineChange) extension.LineChange {
	return extension.LineChange{
		ModifiedStartLineNumber: d.OriginalStartLineNumber, ModifiedEndLineNumber: d.OriginalEndLineNumber,
		OriginalStartLineNumber: d.ModifiedStartLineNumber, OriginalEndLineNumber: d.ModifiedEndLineNumber,
	}
}

// compareLineChanges orders changes by their place in the modified text, then in the original.
//
// Ported from VS Code: extensions/git/src/staging.ts (compareLineChanges)
func compareLineChanges(a, b extension.LineChange) int {
	for _, d := range []int{
		a.ModifiedStartLineNumber - b.ModifiedStartLineNumber, a.ModifiedEndLineNumber - b.ModifiedEndLineNumber,
		a.OriginalStartLineNumber - b.OriginalStartLineNumber, a.OriginalEndLineNumber - b.OriginalEndLineNumber,
	} {
		if d != 0 {
			return d
		}
	}
	return 0
}

// matchLineBreaks gives a text the line breaks of the version of the file it is staged over: a document's text has "\n", and an index
// version that ends its lines with "\r\n" must stay so. A text that is not made of CRLF lines is returned as it is.
func matchLineBreaks(version, text string) string {
	crlf := strings.Count(version, "\r\n")
	if crlf == 0 || crlf < strings.Count(version, "\n")-crlf {
		return text
	}
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
}
