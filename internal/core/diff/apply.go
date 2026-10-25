package diff

import "github.com/rm4n0s/errors"

// Apply rebuilds the modified lines: the lines of original that no change
// covers, and the lines of modified that the changes map to. Changes must be a
// valid diff of the two texts (see Validate); line numbers out of range are
// clipped.
func Apply(original, modified []string, changes []LineRangeMapping) []string {
	result := make([]string, 0, len(modified))
	next := 1 // the first line of original that is not copied yet
	for _, c := range changes {
		result = append(result, lines(original, next, c.Original.StartLine)...)
		result = append(result, lines(modified, c.Modified.StartLine, c.Modified.EndLine)...)
		next = c.Original.EndLine
	}
	return append(result, lines(original, next, len(original)+1)...)
}

// lines returns the lines [start, end) (1-based) of text, clipped to it.
func lines(text []string, start, end int) []string {
	start = max(start, 1)
	end = min(end, len(text)+1)
	if start >= end {
		return nil
	}
	return text[start-1 : end-1]
}

// Validate checks that changes is a consistent diff of two texts of
// originalLines and modifiedLines lines: every range lies inside its text and
// no change is empty on both sides; the changes are sorted and do not touch
// (there is at least one unchanged line between two changes); and the
// unchanged lines before, between and after the changes are as many in the
// original as in the modified text. VS Code asserts the same.
//
// The character changes (Inner) of a change are checked as far as the line
// counts allow: their lines lie in the text (a text always has at least one
// line) and in their block, or in the line before it or after it where the
// change starts at the end of a line or ends at the start of one; a range is
// not reversed; and the ranges are sorted and do not overlap. The columns
// need the text and are not checked here.
func Validate(changes []LineRangeMapping, originalLines, modifiedLines int) error {
	prevOrig, prevMod := 1, 1 // the line after the previous change
	for i, c := range changes {
		o, m := c.Original, c.Modified
		if o.StartLine < 1 || o.StartLine > o.EndLine || o.EndLine > originalLines+1 ||
			m.StartLine < 1 || m.StartLine > m.EndLine || m.EndLine > modifiedLines+1 {
			return errors.New("DiffInvalid", "the changes are not a valid diff", "reason", "range out of its text", "change", i,
				"originalLines", originalLines, "modifiedLines", modifiedLines)
		}
		if o.IsEmpty() && m.IsEmpty() {
			return errors.New("DiffInvalid", "the changes are not a valid diff", "reason", "empty change", "change", i,
				"originalLines", originalLines, "modifiedLines", modifiedLines)
		}
		if i > 0 && (o.StartLine <= prevOrig || m.StartLine <= prevMod) {
			return errors.New("DiffInvalid", "the changes are not a valid diff", "reason", "changes overlap, touch or are not sorted", "change", i,
				"originalLines", originalLines, "modifiedLines", modifiedLines)
		}
		if o.StartLine-prevOrig != m.StartLine-prevMod {
			return errors.New("DiffInvalid", "the changes are not a valid diff", "reason", "unchanged lines before the change differ in number", "change", i,
				"originalLines", originalLines, "modifiedLines", modifiedLines)
		}
		if err := validateInner(c, i, originalLines, modifiedLines); err != nil {
			return err
		}
		prevOrig, prevMod = o.EndLine, m.EndLine
	}
	if originalLines+1-prevOrig != modifiedLines+1-prevMod {
		return errors.New("DiffInvalid", "the changes are not a valid diff", "reason", "unchanged lines after the last change differ in number", "change", len(changes),
			"originalLines", originalLines, "modifiedLines", modifiedLines)
	}
	return nil
}

// validateInner checks the character changes of the change at index i (see
// Validate).
func validateInner(c LineRangeMapping, i, originalLines, modifiedLines int) error {
	inside := func(r Range, block LineRange, lines int) bool {
		lines = max(lines, 1)
		return r.Start.Line >= max(block.StartLine-1, 1) && r.End.Line <= min(block.EndLine, lines) &&
			r.Start.Column >= 1 && r.End.Column >= 1 &&
			r.Start.Line <= lines && r.End.Line >= 1 && !r.End.isBefore(r.Start)
	}
	for j, in := range c.Inner {
		if !inside(in.Original, c.Original, originalLines) || !inside(in.Modified, c.Modified, modifiedLines) {
			return errors.New("DiffInvalid", "the changes are not a valid diff", "reason", "character change outside its change or reversed", "change", i,
				"originalLines", originalLines, "modifiedLines", modifiedLines)
		}
		if j > 0 {
			prev := c.Inner[j-1]
			if in.Original.Start.isBefore(prev.Original.End) || in.Modified.Start.isBefore(prev.Modified.End) {
				return errors.New("DiffInvalid", "the changes are not a valid diff", "reason", "character changes overlap or are not sorted", "change", i,
					"originalLines", originalLines, "modifiedLines", modifiedLines)
			}
		}
	}
	return nil
}
