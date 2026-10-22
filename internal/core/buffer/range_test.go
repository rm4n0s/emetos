package buffer_test

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/vstest"
)

// rangeText has seven lines of 120 characters, long enough for every position
// the ported cases use (columns are only numbers there, so the text is plain).
var rangeText = strings.TrimSuffix(strings.Repeat(strings.Repeat("x", 120)+"\n", 7), "\n")

func rng(t *testing.T, sl, sc, el, ec int) buffer.Range {
	t.Helper()
	return vstest.Range(t, rangeText, sl, sc, el, ec)
}

func pos(t *testing.T, line, col int) buffer.Position {
	t.Helper()
	return vstest.Pos(t, rangeText, line, col)
}

// Ported from VS Code: reference/vscode/src/vs/editor/test/common/core/range.test.ts
// ('empty range', 'swap start and stop same line', 'swap start and stop',
// 'no swap same line', 'no swap'). Expected values are Emetos positions, so
// 1-based VS Code numbers appear minus one.
func TestRangeConstruction(t *testing.T) {
	tests := []struct {
		name           string
		got            buffer.Range
		startL, startC int
		endL, endC     int
		empty          bool
	}{
		{"empty range", rng(t, 1, 1, 1, 1), 0, 0, 0, 0, true},
		{"swap start and stop same line", rng(t, 1, 2, 1, 1), 0, 0, 0, 1, false},
		{"swap start and stop", rng(t, 2, 1, 1, 2), 0, 1, 1, 0, false},
		{"no swap same line", rng(t, 1, 1, 1, 2), 0, 0, 0, 1, false},
		{"no swap", rng(t, 1, 1, 2, 1), 0, 0, 1, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := buffer.Range{
				Start: buffer.Position{Line: tt.startL, Col: tt.startC},
				End:   buffer.Position{Line: tt.endL, Col: tt.endC},
			}
			if tt.got != want {
				t.Errorf("range = %+v, want %+v", tt.got, want)
			}
			if tt.got.IsEmpty() != tt.empty {
				t.Errorf("IsEmpty = %v, want %v", tt.got.IsEmpty(), tt.empty)
			}
		})
	}
}

// Ported from VS Code: range.test.ts ('compareRangesUsingEnds').
func TestCompareUsingEnds(t *testing.T) {
	tests := []struct {
		name       string
		a, b       [4]int
		wantSign   int
		wantStarts int
	}{
		{"a.start < b.start, a.end < b.end", [4]int{1, 1, 1, 3}, [4]int{1, 2, 1, 4}, -1, -1},
		{"a.start = b.start, a.end < b.end", [4]int{1, 1, 1, 3}, [4]int{1, 1, 1, 4}, -1, -1},
		{"a.start > b.start, a.end < b.end", [4]int{1, 2, 1, 3}, [4]int{1, 1, 1, 4}, -1, 1},
		{"a.start < b.start, a.end = b.end", [4]int{1, 1, 1, 4}, [4]int{1, 2, 1, 4}, -1, -1},
		{"a.start = b.start, a.end = b.end", [4]int{1, 1, 1, 4}, [4]int{1, 1, 1, 4}, 0, 0},
		{"a.start > b.start, a.end = b.end", [4]int{1, 2, 1, 4}, [4]int{1, 1, 1, 4}, 1, 1},
		{"a.start < b.start, a.end > b.end", [4]int{1, 1, 1, 5}, [4]int{1, 2, 1, 4}, 1, -1},
		{"a.start = b.start, a.end > b.end", [4]int{1, 1, 2, 4}, [4]int{1, 1, 1, 4}, 1, 1},
		{"a.start > b.start, a.end > b.end", [4]int{1, 2, 5, 1}, [4]int{1, 1, 1, 4}, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := rng(t, tt.a[0], tt.a[1], tt.a[2], tt.a[3])
			b := rng(t, tt.b[0], tt.b[1], tt.b[2], tt.b[3])
			if got := sign(buffer.CompareUsingEnds(a, b)); got != tt.wantSign {
				t.Errorf("CompareUsingEnds = %d, want %d", got, tt.wantSign)
			}
			// Emetos's own: ordering by starts, then ends.
			wantStarts := tt.wantStarts
			if wantStarts == 0 {
				wantStarts = sign(a.End.Compare(b.End))
			}
			if got := sign(buffer.CompareUsingStarts(a, b)); got != wantStarts {
				t.Errorf("CompareUsingStarts = %d, want %d", got, wantStarts)
			}
		})
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// Ported from VS Code: range.test.ts ('containsPosition').
func TestContainsPosition(t *testing.T) {
	r := rng(t, 2, 2, 5, 10)
	tests := []struct {
		line, col int
		want      bool
	}{
		{1, 3, false}, {2, 1, false}, {2, 2, true}, {2, 3, true}, {3, 1, true},
		{5, 9, true}, {5, 10, true}, {5, 11, false}, {6, 1, false},
	}
	for _, tt := range tests {
		if got := r.ContainsPosition(pos(t, tt.line, tt.col)); got != tt.want {
			t.Errorf("ContainsPosition(%d:%d) = %v, want %v", tt.line, tt.col, got, tt.want)
		}
	}
}

// Emetos's own: the strict form leaves out both ends.
func TestStrictContainsPosition(t *testing.T) {
	r := rng(t, 2, 2, 5, 10)
	tests := []struct {
		line, col int
		want      bool
	}{
		{2, 2, false}, {2, 3, true}, {3, 1, true}, {5, 9, true}, {5, 10, false}, {6, 1, false},
	}
	for _, tt := range tests {
		if got := r.StrictContainsPosition(pos(t, tt.line, tt.col)); got != tt.want {
			t.Errorf("StrictContainsPosition(%d:%d) = %v, want %v", tt.line, tt.col, got, tt.want)
		}
	}
}

// Ported from VS Code: range.test.ts ('containsRange').
func TestContainsRange(t *testing.T) {
	r := rng(t, 2, 2, 5, 10)
	tests := []struct {
		in   [4]int
		want bool
	}{
		{[4]int{1, 3, 2, 2}, false}, {[4]int{2, 1, 2, 2}, false}, {[4]int{2, 2, 5, 11}, false},
		{[4]int{2, 2, 6, 1}, false}, {[4]int{5, 9, 6, 1}, false}, {[4]int{5, 10, 6, 1}, false},
		{[4]int{2, 2, 5, 10}, true}, {[4]int{2, 3, 5, 9}, true}, {[4]int{3, 100, 4, 100}, true},
	}
	for _, tt := range tests {
		o := rng(t, tt.in[0], tt.in[1], tt.in[2], tt.in[3])
		if got := r.ContainsRange(o); got != tt.want {
			t.Errorf("ContainsRange(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// Emetos's own: the strict form needs room on both sides.
func TestStrictContainsRange(t *testing.T) {
	r := rng(t, 2, 2, 5, 10)
	if r.StrictContainsRange(r) {
		t.Error("a range strictly contains itself")
	}
	if r.StrictContainsRange(rng(t, 2, 2, 4, 1)) {
		t.Error("shared start counted as strictly inside")
	}
	if r.StrictContainsRange(rng(t, 3, 1, 5, 10)) {
		t.Error("shared end counted as strictly inside")
	}
	if !r.StrictContainsRange(rng(t, 2, 3, 5, 9)) {
		t.Error("a range with room on both sides is not strictly inside")
	}
}

// Ported from VS Code: range.test.ts ('areIntersecting').
func TestAreIntersecting(t *testing.T) {
	tests := []struct {
		a, b [4]int
		want bool
	}{
		{[4]int{2, 2, 3, 2}, [4]int{4, 2, 5, 2}, false},
		{[4]int{4, 2, 5, 2}, [4]int{2, 2, 3, 2}, false},
		{[4]int{4, 2, 5, 2}, [4]int{5, 2, 6, 2}, false},
		{[4]int{5, 2, 6, 2}, [4]int{4, 2, 5, 2}, false},
		{[4]int{2, 2, 2, 7}, [4]int{2, 4, 2, 6}, true},
		{[4]int{2, 2, 2, 7}, [4]int{2, 4, 2, 9}, true},
		{[4]int{2, 4, 2, 9}, [4]int{2, 2, 2, 7}, true},
	}
	for _, tt := range tests {
		a := rng(t, tt.a[0], tt.a[1], tt.a[2], tt.a[3])
		b := rng(t, tt.b[0], tt.b[1], tt.b[2], tt.b[3])
		if got := buffer.AreIntersecting(a, b); got != tt.want {
			t.Errorf("AreIntersecting(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// Emetos's own: ranges that share only an end touch but don't intersect.
func TestAreIntersectingOrTouching(t *testing.T) {
	tests := []struct {
		a, b [4]int
		want bool
	}{
		{[4]int{2, 2, 3, 2}, [4]int{4, 2, 5, 2}, false},
		{[4]int{4, 2, 5, 2}, [4]int{5, 2, 6, 2}, true},
		{[4]int{5, 2, 6, 2}, [4]int{4, 2, 5, 2}, true},
		{[4]int{2, 2, 2, 7}, [4]int{2, 4, 2, 6}, true},
		{[4]int{2, 2, 2, 7}, [4]int{2, 8, 2, 9}, false},
	}
	for _, tt := range tests {
		a := rng(t, tt.a[0], tt.a[1], tt.a[2], tt.a[3])
		b := rng(t, tt.b[0], tt.b[1], tt.b[2], tt.b[3])
		if got := buffer.AreIntersectingOrTouching(a, b); got != tt.want {
			t.Errorf("AreIntersectingOrTouching(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// Emetos's own, following VS Code's plusRange and intersectRanges.
func TestUnionAndIntersect(t *testing.T) {
	a, b := rng(t, 1, 3, 2, 5), rng(t, 2, 1, 4, 2)
	if got, want := a.Union(b), rng(t, 1, 3, 4, 2); got != want {
		t.Errorf("Union = %+v, want %+v", got, want)
	}
	if got, want := b.Union(a), rng(t, 1, 3, 4, 2); got != want {
		t.Errorf("Union (swapped) = %+v, want %+v", got, want)
	}
	got, ok := a.Intersect(b)
	if want := rng(t, 2, 1, 2, 5); !ok || got != want {
		t.Errorf("Intersect = %+v, %v, want %+v", got, ok, want)
	}
	if _, ok := a.Intersect(rng(t, 3, 1, 3, 4)); ok {
		t.Error("disjoint ranges intersect")
	}
	got, ok = rng(t, 1, 1, 1, 5).Intersect(rng(t, 1, 5, 1, 9))
	if want := rng(t, 1, 5, 1, 5); !ok || got != want || !got.IsEmpty() {
		t.Errorf("touching ranges: Intersect = %+v, %v, want the empty range %+v", got, ok, want)
	}
}

// Emetos's own: collapse, replace an end, spans.
func TestRangeEdits(t *testing.T) {
	r := rng(t, 2, 2, 5, 10)
	if got, want := r.CollapseToStart(), rng(t, 2, 2, 2, 2); got != want {
		t.Errorf("CollapseToStart = %+v, want %+v", got, want)
	}
	if got, want := r.CollapseToEnd(), rng(t, 5, 10, 5, 10); got != want {
		t.Errorf("CollapseToEnd = %+v, want %+v", got, want)
	}
	if got, want := r.WithEnd(pos(t, 1, 1)), rng(t, 1, 1, 2, 2); got != want {
		t.Errorf("WithEnd before the start = %+v, want the swapped %+v", got, want)
	}
	if got, want := r.WithStart(pos(t, 6, 1)), rng(t, 5, 10, 6, 1); got != want {
		t.Errorf("WithStart after the end = %+v, want the swapped %+v", got, want)
	}
	if !r.SpansMultipleLines() || rng(t, 2, 2, 2, 9).SpansMultipleLines() {
		t.Error("SpansMultipleLines is wrong")
	}
	if got, want := buffer.RangeFromPositions(pos(t, 3, 1), pos(t, 2, 1)), rng(t, 2, 1, 3, 1); got != want {
		t.Errorf("RangeFromPositions = %+v, want %+v", got, want)
	}
}
