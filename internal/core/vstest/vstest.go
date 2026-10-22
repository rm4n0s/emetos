package vstest

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/errors"
)

// lineOf returns line number line (zero-based) of text, without its newline.
// Text uses "\n" only (SPEC §6.3). The second result is false when the text has
// fewer lines.
func lineOf(text string, line int) (string, bool) {
	if line < 0 {
		return "", false
	}
	for i := 0; ; i++ {
		end := strings.IndexByte(text, '\n')
		if i == line {
			if end < 0 {
				return text, true
			}
			return text[:end], true
		}
		if end < 0 {
			return "", false
		}
		text = text[end+1:]
	}
}

// utf16Len is the length of s in UTF-16 code units. A byte that is not valid
// UTF-8 counts as one unit, like the replacement character it decodes to.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16Units(r)
	}
	return n
}

func utf16Units(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

// ToPosition converts a VS Code position (1-based line, 1-based UTF-16 column)
// in text to an Emetos position (zero-based line, byte column). The column may
// be one past the last character of the line, as in VS Code. It fails with the
// tag VSLineOutOfRange, VSColumnOutOfRange or VSColumnSplitsPair (the column is
// between the two halves of a surrogate pair, which VS Code can't show either).
func ToPosition(text string, line, column int) (buffer.Position, error) {
	l, ok := lineOf(text, line-1)
	if !ok {
		return buffer.Position{}, errors.New("VSLineOutOfRange", "the text has no such line",
			"line", line, "lines", strings.Count(text, "\n")+1)
	}
	if column < 1 || column > utf16Len(l)+1 {
		return buffer.Position{}, errors.New("VSColumnOutOfRange", "the line has no such column",
			"line", line, "column", column, "units", utf16Len(l))
	}
	units := column - 1
	for i, r := range l {
		if units == 0 {
			return buffer.Position{Line: line - 1, Col: i}, nil
		}
		units -= utf16Units(r)
		if units < 0 {
			return buffer.Position{}, errors.New("VSColumnSplitsPair", "the column is inside a surrogate pair",
				"line", line, "column", column)
		}
	}
	return buffer.Position{Line: line - 1, Col: len(l)}, nil
}

// FromPosition converts an Emetos position in text to a VS Code position
// (1-based line, 1-based UTF-16 column). It fails with the tag LineOutOfRange,
// ColumnOutOfRange or ColumnSplitsRune (the byte column is inside a UTF-8
// sequence).
func FromPosition(text string, p buffer.Position) (line, column int, err error) {
	l, ok := lineOf(text, p.Line)
	if !ok {
		return 0, 0, errors.New("LineOutOfRange", "the text has no such line",
			"line", p.Line, "lines", strings.Count(text, "\n")+1)
	}
	if p.Col < 0 || p.Col > len(l) {
		return 0, 0, errors.New("ColumnOutOfRange", "the line has no such column",
			"line", p.Line, "col", p.Col, "bytes", len(l))
	}
	if p.Col < len(l) && !utf8.RuneStart(l[p.Col]) {
		return 0, 0, errors.New("ColumnSplitsRune", "the column is inside a UTF-8 sequence",
			"line", p.Line, "col", p.Col)
	}
	return p.Line + 1, utf16Len(l[:p.Col]) + 1, nil
}

// Pos is ToPosition for tests: it fails the test when the position is invalid.
func Pos(t testing.TB, text string, line, column int) buffer.Position {
	t.Helper()
	p, err := ToPosition(text, line, column)
	if err != nil {
		t.Fatalf("VS Code position %d:%d: %v", line, column, err)
	}
	return p
}

// Range makes the range of a VS Code range (startLine, startColumn, endLine,
// endColumn), in the order of VS Code's Range constructor. It swaps the ends
// when the end is before the start, as that constructor does.
func Range(t testing.TB, text string, startLine, startColumn, endLine, endColumn int) buffer.Range {
	t.Helper()
	return buffer.NewRange(Pos(t, text, startLine, startColumn), Pos(t, text, endLine, endColumn))
}
