package edit

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// hiddenLines hides the lines 2-3 and 6 of a text of eight lines.
func hiddenLines(line int) (first, last int, ok bool) {
	switch line {
	case 2, 3:
		return 2, 3, true
	case 6:
		return 6, 6, true
	}
	return 0, 0, false
}

func hiddenLayout(text string) (Mover, ModelLineLayout) {
	m := NewModel(buffer.New(text).Snapshot())
	l := ModelLineLayout{Model: m, TabSize: 4, Hidden: hiddenLines}
	return NewMover(m, DefaultOptions(), l), l
}

const eightLines = "aaaa\nbbbb\ncccc\ndddd\neeee\nffff\ngggg\nhhhh"

func TestMoveVerticallyOverHiddenLines(t *testing.T) {
	_, l := hiddenLayout(eightLines)
	for _, tc := range []struct {
		name    string
		from    buffer.Position
		rows    int
		want    buffer.Position
		desired int
	}{
		{"down to the fold line", buffer.Position{Line: 0, Col: 2}, 1, buffer.Position{Line: 1, Col: 2}, 2},
		{"down over a fold", buffer.Position{Line: 1, Col: 2}, 1, buffer.Position{Line: 4, Col: 2}, 2},
		{"two rows down over two folds", buffer.Position{Line: 1, Col: 2}, 3, buffer.Position{Line: 7, Col: 2}, 2},
		{"up over a fold", buffer.Position{Line: 4, Col: 3}, -1, buffer.Position{Line: 1, Col: 3}, 3},
		{"up over the last fold", buffer.Position{Line: 7, Col: 1}, -2, buffer.Position{Line: 4, Col: 1}, 1},
		{"past the last row goes to the end", buffer.Position{Line: 5, Col: 1}, 5, buffer.Position{Line: 7, Col: 4}, 1},
		{"past the first row goes to the start", buffer.Position{Line: 4, Col: 1}, -5, buffer.Position{Line: 0, Col: 0}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, desired := l.MoveVertically(tc.from, tc.rows, -1)
			if got != tc.want || desired != tc.desired {
				t.Errorf("got %v desired %d, want %v desired %d", got, desired, tc.want, tc.desired)
			}
		})
	}
}

func TestLeftAndRightOverHiddenLines(t *testing.T) {
	mv, _ := hiddenLayout(eightLines)
	for _, tc := range []struct {
		name string
		do   func(Cursor, bool) Cursor
		from buffer.Position
		want buffer.Position
	}{
		{"right from the end of the fold line", mv.Right, buffer.Position{Line: 1, Col: 4}, buffer.Position{Line: 4}},
		{"left from the line after the fold", mv.Left, buffer.Position{Line: 4}, buffer.Position{Line: 1, Col: 4}},
		{"right from a line before a shown line", mv.Right, buffer.Position{Line: 0, Col: 4}, buffer.Position{Line: 1}},
		{"left over the last fold", mv.Left, buffer.Position{Line: 7}, buffer.Position{Line: 5, Col: 4}},
		{"right in the text", mv.Right, buffer.Position{Line: 4, Col: 1}, buffer.Position{Line: 4, Col: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.do(NewCursor(tc.from), false).Position()
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHiddenLinesAtTheEndOfTheText(t *testing.T) {
	m := NewModel(buffer.New("a\nb\nc").Snapshot())
	hide := func(line int) (int, int, bool) { return 1, 2, line == 1 || line == 2 }
	mv := NewMover(m, DefaultOptions(), ModelLineLayout{Model: m, TabSize: 4, Hidden: hide})
	if got := mv.Right(NewCursor(buffer.Position{Line: 0, Col: 1}), false).Position(); got != (buffer.Position{Line: 0, Col: 1}) {
		t.Errorf("right at the end of the last shown line stays: %v", got)
	}
	l := ModelLineLayout{Model: m, TabSize: 4, Hidden: hide}
	if got, _ := l.MoveVertically(buffer.Position{Line: 0, Col: 1}, 1, -1); got != (buffer.Position{Line: 0, Col: 1}) {
		t.Errorf("down at the last shown row: %v", got)
	}
}
