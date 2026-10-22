package vstest

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/errors"
)

// conversion is one position written both ways.
type conversion struct {
	name      string
	text      string
	line, col int // VS Code: 1-based line, 1-based UTF-16 column
	want      buffer.Position
}

var conversions = []conversion{
	// ASCII: one byte, one UTF-16 unit.
	{"ascii start", "hello", 1, 1, buffer.Position{Line: 0, Col: 0}},
	{"ascii middle", "hello", 1, 3, buffer.Position{Line: 0, Col: 2}},
	{"ascii end of line", "hello", 1, 6, buffer.Position{Line: 0, Col: 5}},
	{"ascii second line", "ab\ncd\nef", 2, 2, buffer.Position{Line: 1, Col: 1}},
	{"ascii last line end", "ab\ncd\nef", 3, 3, buffer.Position{Line: 2, Col: 2}},
	{"empty text", "", 1, 1, buffer.Position{Line: 0, Col: 0}},
	{"empty line between", "a\n\nb", 2, 1, buffer.Position{Line: 1, Col: 0}},
	{"trailing newline gives an empty last line", "a\n", 2, 1, buffer.Position{Line: 1, Col: 0}},
	// Greek: two bytes, one unit.
	{"greek after one letter", "αβγ", 1, 2, buffer.Position{Line: 0, Col: 2}},
	{"greek end", "αβγ", 1, 4, buffer.Position{Line: 0, Col: 6}},
	{"greek after ascii", "aβc", 1, 3, buffer.Position{Line: 0, Col: 3}},
	// CJK: three bytes, one unit.
	{"cjk after one character", "日本語", 1, 2, buffer.Position{Line: 0, Col: 3}},
	{"cjk end", "日本語", 1, 4, buffer.Position{Line: 0, Col: 9}},
	{"cjk on the second line", "x\n日本語", 2, 3, buffer.Position{Line: 1, Col: 6}},
	// Astral plane: four bytes, two units.
	{"astral before", "a😀b", 1, 2, buffer.Position{Line: 0, Col: 1}},
	{"astral after", "a😀b", 1, 4, buffer.Position{Line: 0, Col: 5}},
	{"astral end", "a😀b", 1, 5, buffer.Position{Line: 0, Col: 6}},
	{"two astral", "😀😀", 1, 5, buffer.Position{Line: 0, Col: 8}},
	{"mixed widths", "aβ日😀", 1, 6, buffer.Position{Line: 0, Col: 10}},
	// Invalid UTF-8: one byte, one unit (the replacement character).
	{"invalid byte", "a\xffb", 1, 3, buffer.Position{Line: 0, Col: 2}},
}

func TestToPositionAndBack(t *testing.T) {
	for _, tt := range conversions {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToPosition(tt.text, tt.line, tt.col)
			if err != nil {
				t.Fatalf("ToPosition: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ToPosition = %+v, want %+v", got, tt.want)
			}
			line, col, err := FromPosition(tt.text, got)
			if err != nil {
				t.Fatalf("FromPosition: %v", err)
			}
			if line != tt.line || col != tt.col {
				t.Errorf("FromPosition = %d:%d, want %d:%d", line, col, tt.line, tt.col)
			}
		})
	}
}

func TestToPositionFailures(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		line, col int
		tag       string
	}{
		{"line zero", "abc", 0, 1, "VSLineOutOfRange"},
		{"line past the end", "abc", 2, 1, "VSLineOutOfRange"},
		{"line past the end after a newline", "abc\n", 3, 1, "VSLineOutOfRange"},
		{"column zero", "abc", 1, 0, "VSColumnOutOfRange"},
		{"column past the end", "abc", 1, 5, "VSColumnOutOfRange"},
		{"column past the end counts units", "😀", 1, 4, "VSColumnOutOfRange"},
		{"inside a surrogate pair", "a😀b", 1, 3, "VSColumnSplitsPair"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ToPosition(tt.text, tt.line, tt.col)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("ToPosition") || !appErr.HasRoute(tt.tag) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tt.tag)
			}
		})
	}
}

func TestFromPositionFailures(t *testing.T) {
	tests := []struct {
		name string
		text string
		pos  buffer.Position
		tag  string
	}{
		{"negative line", "abc", buffer.Position{Line: -1, Col: 0}, "LineOutOfRange"},
		{"line past the end", "abc", buffer.Position{Line: 1, Col: 0}, "LineOutOfRange"},
		{"negative column", "abc", buffer.Position{Line: 0, Col: -1}, "ColumnOutOfRange"},
		{"column past the end", "abc", buffer.Position{Line: 0, Col: 4}, "ColumnOutOfRange"},
		{"inside a two-byte letter", "aβ", buffer.Position{Line: 0, Col: 2}, "ColumnSplitsRune"},
		{"inside a three-byte character", "日", buffer.Position{Line: 0, Col: 2}, "ColumnSplitsRune"},
		{"inside a four-byte character", "😀", buffer.Position{Line: 0, Col: 3}, "ColumnSplitsRune"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := FromPosition(tt.text, tt.pos)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("FromPosition") || !appErr.HasRoute(tt.tag) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tt.tag)
			}
		})
	}
}

func TestRangeSwapsLikeVSCode(t *testing.T) {
	got := Range(t, "αβγ\ndef", 2, 2, 1, 3)
	want := buffer.Range{Start: buffer.Position{Line: 0, Col: 4}, End: buffer.Position{Line: 1, Col: 1}}
	if got != want {
		t.Errorf("Range = %+v, want %+v", got, want)
	}
}
