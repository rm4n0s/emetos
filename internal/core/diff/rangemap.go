package diff

import (
	"math"

	"github.com/rm4n0s/errors"
)

// Ported from VS Code: src/vs/editor/common/diff/rangeMapping.ts
// (LineRangeMapping.toRangeMapping2, getLineRangeMapping, lineRangeMappingFromRangeMappings)

// toRangeMapping2 returns the range of text that the line blocks cover, as
// VS Code's LineRangeMapping.toRangeMapping2: whole lines when the end
// of the block is a line of the text, else up to the end of its last line; for
// an empty block, the line break before it. The blocks must come from a valid
// diff.
func toRangeMapping2(original, modified []string, orig, mod LineRange) (RangeMapping, error) {
	if isValidLineNumber(orig.EndLine, original) && isValidLineNumber(mod.EndLine, modified) {
		return RangeMapping{
			Original: Range{Position{orig.StartLine, 1}, Position{orig.EndLine, 1}},
			Modified: Range{Position{mod.StartLine, 1}, Position{mod.EndLine, 1}},
		}, nil
	}

	if !orig.IsEmpty() && !mod.IsEmpty() {
		return RangeMapping{
			Original: rangeFromPositions(Position{orig.StartLine, 1}, normalizePosition(Position{orig.EndLine - 1, math.MaxInt}, original)),
			Modified: rangeFromPositions(Position{mod.StartLine, 1}, normalizePosition(Position{mod.EndLine - 1, math.MaxInt}, modified)),
		}, nil
	}

	if orig.StartLine > 1 && mod.StartLine > 1 {
		return RangeMapping{
			Original: rangeFromPositions(
				normalizePosition(Position{orig.StartLine - 1, math.MaxInt}, original),
				normalizePosition(Position{orig.EndLine - 1, math.MaxInt}, original)),
			Modified: rangeFromPositions(
				normalizePosition(Position{mod.StartLine - 1, math.MaxInt}, modified),
				normalizePosition(Position{mod.EndLine - 1, math.MaxInt}, modified)),
		}, nil
	}

	return RangeMapping{}, errors.New("DiffRangeInvalid", "the line blocks are not part of a valid diff",
		"original", orig, "modified", mod, "originalLines", len(original), "modifiedLines", len(modified))
}

// rangeFromPositions makes a range from two positions, in either order.
func rangeFromPositions(a, b Position) Range {
	if b.isBefore(a) {
		a, b = b, a
	}
	return Range{a, b}
}

// normalizePosition moves a position into the text: a line before the first
// one gives the start of the text, a line after the last one the end of the
// text, and a column past the end of its line the end of the line.
func normalizePosition(p Position, content []string) Position {
	if p.Line < 1 {
		return Position{1, 1}
	}
	if p.Line > len(content) {
		return Position{len(content), len(content[len(content)-1]) + 1}
	}
	if line := content[p.Line-1]; p.Column > len(line)+1 {
		return Position{p.Line, len(line) + 1}
	}
	return p
}

func isValidLineNumber(n int, lines []string) bool { return n >= 1 && n <= len(lines) }

// lineLength returns the length in bytes of a line, or 0 when there is none.
func lineLength(lines []string, lineNumber int) int {
	if lineNumber < 1 || lineNumber > len(lines) {
		return 0
	}
	return len(lines[lineNumber-1])
}

// lineRangeMappingOf returns the line blocks that a character change touches
// (VS Code's getLineRangeMapping): a block does not include a first line that
// the change starts at the end of, nor a last line that it ends at the start
// of.
func lineRangeMappingOf(m RangeMapping, original, modified []string) LineRangeMapping {
	lineStartDelta, lineEndDelta := 0, 0

	// original: ]xxx \n <- this line is not modified
	// modified: ]xx  \n
	if m.Modified.End.Column == 1 && m.Original.End.Column == 1 &&
		m.Original.Start.Line+lineStartDelta <= m.Original.End.Line &&
		m.Modified.Start.Line+lineStartDelta <= m.Modified.End.Line {
		// This is only done when the range is not empty yet.
		lineEndDelta = -1
	}

	// original: xxx[ \n <- this line is not modified
	// modified: xxx[ \n
	if m.Modified.Start.Column-1 >= lineLength(modified, m.Modified.Start.Line) &&
		m.Original.Start.Column-1 >= lineLength(original, m.Original.Start.Line) &&
		m.Original.Start.Line <= m.Original.End.Line+lineEndDelta &&
		m.Modified.Start.Line <= m.Modified.End.Line+lineEndDelta {
		// This is only done when the range is not empty yet.
		lineStartDelta = 1
	}

	return LineRangeMapping{
		Original: LineRange{m.Original.Start.Line + lineStartDelta, m.Original.End.Line + 1 + lineEndDelta},
		Modified: LineRange{m.Modified.Start.Line + lineStartDelta, m.Modified.End.Line + 1 + lineEndDelta},
		Inner:    []RangeMapping{m},
	}
}

// lineRangeMappingFromRangeMappings groups sorted character changes into line
// blocks (VS Code's lineRangeMappingFromRangeMappings): changes whose line
// blocks overlap or touch end up in one block, with all of them as Inner.
func lineRangeMappingFromRangeMappings(alignments []RangeMapping, original, modified []string) []LineRangeMapping {
	var changes []LineRangeMapping
	var last LineRangeMapping // the previous alignment's block, not the joined block
	for i, a := range alignments {
		m := lineRangeMappingOf(a, original, modified)
		if i > 0 && (last.Original.intersectsOrTouches(m.Original) || last.Modified.intersectsOrTouches(m.Modified)) {
			c := &changes[len(changes)-1]
			c.Original = c.Original.join(m.Original)
			c.Modified = c.Modified.join(m.Modified)
			c.Inner = append(c.Inner, a)
		} else {
			changes = append(changes, m)
		}
		last = m
	}
	return changes
}
