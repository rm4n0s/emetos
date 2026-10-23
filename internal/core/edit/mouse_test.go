package edit

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func mouseMover(text string) Mover {
	return NewMover(NewModel(buffer.New(text).Snapshot()), DefaultOptions(), nil)
}

func rng(l1, c1, l2, c2 int) buffer.Range {
	return buffer.Range{Start: posAt(l1, c1), End: posAt(l2, c2)}
}

func TestWordSelect(t *testing.T) {
	const text = "foo.bar  baz\n   \nlast"
	cases := []struct {
		name string
		at   buffer.Position
		want buffer.Range
	}{
		{"inside a word", posAt(0, 1), rng(0, 0, 0, 3)},
		{"at the start of a word", posAt(0, 4), rng(0, 4, 0, 7)},
		{"at the end of a word the word wins over a separator", posAt(0, 3), rng(0, 0, 0, 3)},
		{"on the separator after a word", posAt(0, 3), rng(0, 0, 0, 3)},
		{"between two spaces is the gap", posAt(0, 8), rng(0, 7, 0, 9)},
		{"after the last word of a line", posAt(0, 12), rng(0, 9, 0, 12)},
		{"in a blank line", posAt(1, 1), rng(1, 0, 1, 3)},
		{"on the last line", posAt(2, 2), rng(2, 0, 2, 4)},
		{"past the end of the text is clamped", posAt(9, 99), rng(2, 0, 2, 4)},
	}
	mv := mouseMover(text)
	for _, tc := range cases {
		if got := mv.WordSelect(tc.at); got != tc.want {
			t.Errorf("%s: WordSelect(%v) = %v, want %v", tc.name, tc.at, got, tc.want)
		}
	}
}

func TestWordSelectSeparators(t *testing.T) {
	mv := mouseMover("a ... b")
	cases := []struct {
		name string
		at   buffer.Position
		want buffer.Range
	}{
		{"inside a run of separators", posAt(0, 3), rng(0, 2, 0, 5)},
		{"at the start of a run", posAt(0, 2), rng(0, 2, 0, 5)},
		{"the end of a run is the gap before the next word", posAt(0, 5), rng(0, 5, 0, 6)},
		{"empty line gives an empty range", posAt(0, 0), rng(0, 0, 0, 1)},
	}
	for _, tc := range cases {
		if got := mv.WordSelect(tc.at); got != tc.want {
			t.Errorf("%s: WordSelect(%v) = %v, want %v", tc.name, tc.at, got, tc.want)
		}
	}
	empty := mouseMover("")
	if got := empty.WordSelect(posAt(0, 0)); got != rng(0, 0, 0, 0) {
		t.Errorf("empty text: %v", got)
	}
}

func TestWordSelectDrag(t *testing.T) {
	mv := mouseMover("one two three four")
	initial := rng(0, 4, 0, 7) // "two"
	cases := []struct {
		name string
		to   buffer.Position
		want buffer.Selection
	}{
		{"inside the initial word keeps it", posAt(0, 5), buffer.Selection{Anchor: posAt(0, 4), Active: posAt(0, 7)}},
		{"inside a word to the right takes its end", posAt(0, 10), buffer.Selection{Anchor: posAt(0, 4), Active: posAt(0, 13)}},
		{"on a boundary to the right stays there", posAt(0, 14), buffer.Selection{Anchor: posAt(0, 4), Active: posAt(0, 14)}},
		{"inside a word to the left takes its start", posAt(0, 1), buffer.Selection{Anchor: posAt(0, 7), Active: posAt(0, 0)}},
		{"at the start of the initial word keeps it", posAt(0, 4), buffer.Selection{Anchor: posAt(0, 4), Active: posAt(0, 7)}},
	}
	for _, tc := range cases {
		if got := mv.WordSelectDrag(initial, tc.to); got != tc.want {
			t.Errorf("%s: WordSelectDrag(%v) = %v, want %v", tc.name, tc.to, got, tc.want)
		}
	}
}

func TestLineSelect(t *testing.T) {
	mv := mouseMover("ab\ncd\nef")
	cases := []struct {
		name string
		line int
		want buffer.Range
	}{
		{"first line", 0, rng(0, 0, 1, 0)},
		{"middle line", 1, rng(1, 0, 2, 0)},
		{"last line stops at the end of the text", 2, rng(2, 0, 2, 2)},
		{"beyond the text", 7, rng(2, 0, 2, 2)},
		{"before the text", -3, rng(0, 0, 1, 0)},
	}
	for _, tc := range cases {
		if got := mv.LineSelect(tc.line); got != tc.want {
			t.Errorf("%s: LineSelect(%d) = %v, want %v", tc.name, tc.line, got, tc.want)
		}
	}
}

func TestLineSelectDrag(t *testing.T) {
	mv := mouseMover("a\nb\nc\nd\ne")
	initial := mv.LineSelect(2)
	cases := []struct {
		name string
		to   buffer.Position
		want buffer.Selection
	}{
		{"the same line keeps the line", posAt(2, 0), buffer.Selection{Anchor: posAt(2, 0), Active: posAt(3, 0)}},
		{"lines below", posAt(3, 0), buffer.Selection{Anchor: posAt(2, 0), Active: posAt(4, 0)}},
		{"to the last line", posAt(4, 0), buffer.Selection{Anchor: posAt(2, 0), Active: posAt(4, 1)}},
		{"lines above", posAt(1, 1), buffer.Selection{Anchor: posAt(3, 0), Active: posAt(1, 0)}},
		{"the first line", posAt(0, 0), buffer.Selection{Anchor: posAt(3, 0), Active: posAt(0, 0)}},
	}
	for _, tc := range cases {
		if got := mv.LineSelectDrag(initial, tc.to); got != tc.want {
			t.Errorf("%s: LineSelectDrag(%v) = %v, want %v", tc.name, tc.to, got, tc.want)
		}
	}
}

func TestRangeSelection(t *testing.T) {
	r := rng(1, 2, 1, 5)
	cases := []struct {
		name string
		r    buffer.Range
		to   buffer.Position
		want buffer.Selection
	}{
		{"after the range", r, posAt(2, 0), buffer.Selection{Anchor: posAt(1, 2), Active: posAt(2, 0)}},
		{"before the range", r, posAt(0, 0), buffer.Selection{Anchor: posAt(1, 5), Active: posAt(0, 0)}},
		{"inside the range", r, posAt(1, 3), buffer.Selection{Anchor: posAt(1, 2), Active: posAt(1, 3)}},
		{"an empty range anchors at itself", rng(1, 2, 1, 2), posAt(0, 0), buffer.Selection{Anchor: posAt(1, 2), Active: posAt(0, 0)}},
	}
	for _, tc := range cases {
		if got := RangeSelection(tc.r, tc.to); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
