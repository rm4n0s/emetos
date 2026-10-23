package editor

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/folding"
	"github.com/rm4n0s/emetos/internal/core/wrap"
)

func TestWrappedProjection(t *testing.T) {
	// line 0 is 3 rows, line 1 one row, line 2 and 3 are hidden by a fold, line 4 two rows with an indent,
	// line 5 has no breaks
	breaks := []*wrap.Breaks{
		{Offsets: []int{10, 20, 25}},
		nil,
		{Offsets: []int{5, 9}},
		{Offsets: []int{4, 8}},
		{Offsets: []int{12, 20}, IndentLength: 4},
	}
	hidden := []folding.LineSpan{{First: 2, Last: 3}}
	cases := []struct {
		name   string
		base   *foldedProjection
		rows   int
		spans  [][3]int // line, start, end of each row
		indent []int
	}{
		{
			name: "folded",
			base: newFoldedProjection(6, 20, hidden),
			rows: 7,
			spans: [][3]int{
				{0, 0, 10}, {0, 10, 20}, {0, 20, -1}, {1, 0, -1}, {4, 0, 12}, {4, 12, -1}, {5, 0, -1},
			},
			indent: []int{0, 0, 0, 0, 0, 4, 0},
		},
		{
			name: "not folded",
			rows: 11,
			spans: [][3]int{
				{0, 0, 10}, {0, 10, 20}, {0, 20, -1}, {1, 0, -1}, {2, 0, 5}, {2, 5, -1}, {3, 0, 4}, {3, 4, -1}, {4, 0, 12}, {4, 12, -1}, {5, 0, -1},
			},
			indent: []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 4, 0},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newWrappedProjection(6, 20, c.base, breaks)
			if p.RowCount() != c.rows || p.Height() != float64(c.rows)*20 {
				t.Fatalf("%d rows, height %v; want %d", p.RowCount(), p.Height(), c.rows)
			}
			for row, want := range c.spans {
				// the last row of a line ends at -1: the end of the line
				line, start, end := p.RowSpan(row)
				if line != want[0] || start != want[1] || end != want[2] {
					t.Errorf("RowSpan(%d) = %d,%d,%d, want %v", row, line, start, end, want)
				}
				if got := p.RowIndentColumns(row); got != c.indent[row] {
					t.Errorf("RowIndentColumns(%d) = %d, want %d", row, got, c.indent[row])
				}
				if got := p.RowAt(p.RowTop(row) + 1); got != row {
					t.Errorf("RowAt(top of %d) = %d", row, got)
				}
				first, count := p.LineRows(want[0])
				if row < first || row >= first+count {
					t.Errorf("LineRows(%d) = %d,%d does not hold row %d", want[0], first, count, row)
				}
			}
			if got := p.RowAt(1e6); got != c.rows-1 {
				t.Errorf("RowAt past the end = %d", got)
			}
		})
	}
}

func TestWrappedProjectionRowMap(t *testing.T) {
	breaks := []*wrap.Breaks{{Offsets: []int{10, 20, 25}, IndentLength: 2}, nil}
	p := newWrappedProjection(3, 20, nil, breaks)
	if got := p.RowStarts(0); !slices.Equal(got, []int{0, 10, 20}) {
		t.Errorf("RowStarts(0) = %v", got)
	}
	if got := p.RowStarts(1); got != nil {
		t.Errorf("RowStarts of an unbroken line = %v", got)
	}
	if got := p.RowStarts(7); got != nil {
		t.Errorf("RowStarts of a line that is not there = %v", got)
	}
	if p.Indent(0) != 2 || p.Indent(1) != 0 || p.Indent(2) != 0 {
		t.Errorf("indents %d %d %d", p.Indent(0), p.Indent(1), p.Indent(2))
	}
	// lines past the breaks that are known are one row
	if first, count := p.LineRows(2); first != 4 || count != 1 {
		t.Errorf("LineRows(2) = %d,%d", first, count)
	}
	// no lines still has a row
	if got := newWrappedProjection(0, 20, nil, nil).RowCount(); got != 1 {
		t.Errorf("an empty document has %d rows", got)
	}
}
