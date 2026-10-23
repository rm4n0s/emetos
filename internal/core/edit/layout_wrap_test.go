package edit

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// rowMap is a RowMap of fixed rows.
type rowMap struct {
	starts map[int][]int
	indent map[int]int
}

func (r rowMap) RowStarts(line int) []int { return r.starts[line] }
func (r rowMap) Indent(line int) int      { return r.indent[line] }

// A text of four lines, the second and the third wrapped into three rows and two:
//
//	line 0: "short"
//	line 1: "aaaa bbbb cccc dddd" in the rows "aaaa " "bbbb " "cccc dddd"
//	line 2: "eeeeeeee ffff" in the rows "eeeeeeee " "ffff" (the second indented by 2 columns)
//	line 3: "tail"
const wrapText = "short\naaaa bbbb cccc dddd\neeeeeeee ffff\ntail"

func wrapLayout() ModelLineLayout {
	return ModelLineLayout{
		Model: NewModel(buffer.New(wrapText).Snapshot()), TabSize: 4,
		Rows: rowMap{starts: map[int][]int{1: {0, 5, 10}, 2: {0, 9}}, indent: map[int]int{2: 2}},
	}
}

func pos(line, col int) buffer.Position { return buffer.Position{Line: line, Col: col} }

func TestMoveVerticallyOverWrappedRows(t *testing.T) {
	l := wrapLayout()
	for _, tc := range []struct {
		name    string
		from    buffer.Position
		rows    int
		desired int
		want    buffer.Position
		keep    int
	}{
		{"down into the first row of a wrapped line", pos(0, 3), 1, -1, pos(1, 3), 3},
		{"down to the next row of the same line", pos(1, 2), 1, -1, pos(1, 7), 2},
		{"and to the last row", pos(1, 7), 1, -1, pos(1, 12), 2},
		{"down out of the last row", pos(1, 12), 1, -1, pos(2, 2), 2},
		{"up to the last row of the line before", pos(2, 2), -1, -1, pos(1, 12), 2},
		{"up within a line", pos(1, 12), -1, -1, pos(1, 7), 2},
		{"two rows down", pos(1, 2), 2, -1, pos(1, 12), 2},
		{"the desired column is kept", pos(1, 3), 1, 3, pos(1, 8), 3},
		// a row that is not the last stops before its trailing space: "aaaa " has 4 characters
		{"down onto a short row", pos(1, 9), 0, -1, pos(1, 9), 4},
		{"the indent counts in the visual column", pos(2, 11), -1, -1, pos(2, 4), 4},
		{"down into an indented row", pos(1, 6), 1, -1, pos(1, 11), 1},
		{"past the last row", pos(3, 1), 3, -1, pos(3, 4), 1},
		{"past the first row", pos(0, 2), -3, -1, pos(0, 0), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, keep := l.MoveVertically(tc.from, tc.rows, tc.desired)
			if got != tc.want {
				t.Errorf("moved to %v, want %v", got, tc.want)
			}
			_ = keep
		})
	}
}

func TestMoveVerticallyIntoAnIndentedRow(t *testing.T) {
	l := wrapLayout()
	// from line 2's first row at column 5 (visual 5) down into its row at column 9, which is
	// indented by 2: visual 5 is the column 5-2=3 of the row text "ffff"
	got, _ := l.MoveVertically(pos(2, 5), 1, -1)
	if got != pos(2, 12) {
		t.Errorf("moved to %v, want (2,12)", got)
	}
	// and back up: the position (2,12) is at visual column 2+3 = 5 on its row
	got, _ = l.MoveVertically(pos(2, 12), -1, -1)
	if got != pos(2, 5) {
		t.Errorf("moved to %v, want (2,5)", got)
	}
}

func TestRowStartAndEndOfWrappedRows(t *testing.T) {
	l := wrapLayout()
	for _, tc := range []struct {
		name       string
		at         buffer.Position
		start, end buffer.Position
	}{
		{"a line of one row", pos(0, 2), pos(0, 0), pos(0, 5)},
		{"the first row", pos(1, 2), pos(1, 0), pos(1, 4)},
		{"a position at the start of a row is on that row", pos(1, 5), pos(1, 5), pos(1, 9)},
		{"the last row", pos(1, 12), pos(1, 10), pos(1, 19)},
		{"the end of the line is on the last row", pos(1, 19), pos(1, 10), pos(1, 19)},
		{"a row that ends in no space stops before its last character", pos(2, 3), pos(2, 0), pos(2, 8)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := l.RowStart(tc.at); got != tc.start {
				t.Errorf("RowStart = %v, want %v", got, tc.start)
			}
			if got := l.RowEnd(tc.at); got != tc.end {
				t.Errorf("RowEnd = %v, want %v", got, tc.end)
			}
		})
	}
}

func TestHomeAndEndOnWrappedRows(t *testing.T) {
	l := wrapLayout()
	mv := NewMover(l.Model, DefaultOptions(), l)
	c := Cursor{Selection: buffer.NewCursor(pos(1, 7))}
	if got := mv.Home(c, false).Position(); got != pos(1, 5) {
		t.Errorf("Home on a continuation row goes to the start of the row: %v", got)
	}
	if got := mv.End(c, false).Position(); got != pos(1, 9) {
		t.Errorf("End goes to the end of the row: %v", got)
	}
	first := Cursor{Selection: buffer.NewCursor(pos(1, 2))}
	if got := mv.Home(first, false).Position(); got != pos(1, 0) {
		t.Errorf("Home on the first row: %v", got)
	}
	last := Cursor{Selection: buffer.NewCursor(pos(1, 12))}
	if got := mv.End(last, false).Position(); got != pos(1, 19) {
		t.Errorf("End on the last row: %v", got)
	}
}

func TestMoveVerticallyOverWrappedRowsAndFolds(t *testing.T) {
	l := wrapLayout()
	l.Hidden = func(line int) (int, int, bool) {
		if line == 2 {
			return 2, 2, true
		}
		return 0, 0, false
	}
	// the hidden line 2 has no rows: down from the last row of line 1 goes to line 3
	got, _ := l.MoveVertically(pos(1, 12), 1, -1)
	if got != pos(3, 2) {
		t.Errorf("down over a hidden line: %v", got)
	}
	got, _ = l.MoveVertically(pos(3, 2), -1, -1)
	if got.Line != 1 || got.Col != 12 {
		t.Errorf("up over a hidden line: %v", got)
	}
}

func TestAWrapLayoutWithoutRowsIsTheOldOne(t *testing.T) {
	l := ModelLineLayout{Model: NewModel(buffer.New(wrapText).Snapshot()), TabSize: 4}
	got, _ := l.MoveVertically(pos(1, 12), 1, -1)
	if got != pos(2, 12) {
		t.Errorf("down: %v", got)
	}
	if l.RowStart(pos(1, 12)) != pos(1, 0) || l.RowEnd(pos(1, 12)) != pos(1, 19) {
		t.Error("a line is one row")
	}
}
