package diffeditor

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
)

func at(line, col int) buffer.Position { return buffer.Position{Line: line, Col: col} }

func TestTranslatePosition(t *testing.T) {
	// original lines 2-3 became modified line 2, and the character change inside is original columns 3-5 (bytes,
	// from 1) of line 2 against modified columns 3-8 of line 2
	withInner := change(2, 4, 2, 3, inner(2, 3, 2, 6, 2, 3, 2, 9))
	without := change(2, 4, 2, 3)
	for _, tc := range []struct {
		name    string
		changes []diff.LineRangeMapping
		from    Side
		p       buffer.Position
		want    buffer.Range
	}{
		{"before every change", []diff.LineRangeMapping{withInner}, Original, at(0, 3), buffer.Range{Start: at(0, 3), End: at(0, 3)}},
		{"no changes", nil, Modified, at(4, 2), buffer.Range{Start: at(4, 2), End: at(4, 2)}},
		{"after a change moves by the lines it removed", []diff.LineRangeMapping{withInner}, Original, at(4, 1), buffer.Range{Start: at(3, 1), End: at(3, 1)}},
		{"after a change moves by the lines it added", []diff.LineRangeMapping{without}, Modified, at(3, 1), buffer.Range{Start: at(4, 1), End: at(4, 1)}},
		{"in a change without character changes goes to its start", []diff.LineRangeMapping{without}, Original, at(2, 1), buffer.Range{Start: at(1, 0), End: at(1, 0)}},
		{"inside a character change gives the other side's", []diff.LineRangeMapping{withInner}, Original, at(1, 3), buffer.Range{Start: at(1, 2), End: at(1, 8)}},
		{"inside a character change, from the other side", []diff.LineRangeMapping{withInner}, Modified, at(1, 4), buffer.Range{Start: at(1, 2), End: at(1, 5)}},
		{"after a character change on its line", []diff.LineRangeMapping{withInner}, Original, at(1, 7), buffer.Range{Start: at(1, 10), End: at(1, 10)}},
		{"after a character change, on a later line", []diff.LineRangeMapping{withInner}, Original, at(2, 2), buffer.Range{Start: at(2, 2), End: at(2, 2)}},
		{"in a change before its first character change", []diff.LineRangeMapping{withInner}, Original, at(1, 1), buffer.Range{Start: at(1, 1), End: at(1, 1)}},
		{
			"between two changes",
			[]diff.LineRangeMapping{change(2, 3, 2, 5), change(8, 9, 10, 10)}, Original, at(5, 2), buffer.Range{Start: at(7, 2), End: at(7, 2)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TranslatePosition(tc.changes, tc.from, tc.p); got != tc.want {
				t.Errorf("TranslatePosition = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSides(t *testing.T) {
	if Original.Other() != Modified || Modified.Other() != Original || Original.String() != "original" || Modified.String() != "modified" {
		t.Errorf("sides: %v %v", Original.Other(), Modified.Other())
	}
}
