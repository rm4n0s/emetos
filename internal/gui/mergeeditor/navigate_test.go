package mergeeditor

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/diff"
)

func TestConflictToGo(t *testing.T) {
	ranges := []diff.LineRange{lr(3, 5), lr(8, 8), lr(12, 15)}
	for _, tc := range []struct {
		name string
		line int
		next bool
		want int
		ok   bool
	}{
		{"next from the top", 1, true, 0, true},
		{"next skips the conflict the cursor is in", 3, true, 1, true},
		{"next from inside a conflict", 4, true, 1, true},
		{"next from between", 6, true, 1, true},
		{"next of an empty range at the cursor's line", 8, true, 2, true},
		{"next wraps around after the last", 13, true, 0, true},
		{"next from the end of the text wraps", 40, true, 0, true},
		{"previous from the bottom", 40, false, 2, true},
		{"previous from inside the last", 13, false, 1, true},
		{"previous needs a range that ended before", 5, false, 2, true}, // lr(3,5) ends at 5: not before line 5
		{"previous from just after a conflict", 6, false, 0, true},
		{"previous of an empty range", 9, false, 1, true},
		{"previous wraps around before the first", 2, false, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ConflictToGo(ranges, tc.line, tc.next)
			if got != tc.want || ok != tc.ok {
				t.Errorf("ConflictToGo(line %d, next %v) = %d, %v; want %d, %v", tc.line, tc.next, got, ok, tc.want, tc.ok)
			}
		})
	}
	if _, ok := ConflictToGo(nil, 3, true); ok {
		t.Error("a conflict without conflicts")
	}
	if got, _ := ConflictToGo(ranges[:1], 3, true); got != 0 {
		t.Errorf("a single conflict: %d", got)
	}
}

func TestActiveConflict(t *testing.T) {
	ranges := []diff.LineRange{lr(3, 5), lr(8, 8)}
	for _, tc := range []struct {
		line int
		want int
		ok   bool
	}{{2, 0, false}, {3, 0, true}, {4, 0, true}, {5, 0, false}, {8, 1, true}, {9, 0, false}} {
		if got, ok := ActiveConflict(ranges, tc.line); got != tc.want || ok != tc.ok {
			t.Errorf("ActiveConflict(%d) = %d, %v; want %d, %v", tc.line, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRevealLines(t *testing.T) {
	for _, tc := range []struct {
		name        string
		r           diff.LineRange
		lines       int
		first, last int
	}{
		{"a range", lr(3, 6), 10, 3, 5},
		{"an empty range", lr(4, 4), 10, 4, 4},
		{"an empty range past the end", lr(11, 11), 10, 10, 10},
		{"a range that ends past the end", lr(9, 14), 10, 9, 10},
		{"an empty text", lr(1, 1), 0, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if first, last := RevealLines(tc.r, tc.lines); first != tc.first || last != tc.last {
				t.Errorf("RevealLines = %d, %d; want %d, %d", first, last, tc.first, tc.last)
			}
		})
	}
}

func TestCheckboxTop(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		top, height, viewTop, viewHt float64
		want                         float64
	}{
		{"a one-line conflict: in the middle of the line", 100, 20, 0, 500, 101},
		{"a short conflict is not moved", 0, 20, 0, 500, 1},
		{"a tall conflict: in the middle", 100, 400, 0, 600, 291},
		{"a tall conflict scrolled so that its middle is below the view: kept in view", 100, 400, 0, 200, 164},
		{"a tall conflict whose top is above the view: kept at the top of the view", -300, 400, 0, 200, 18},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CheckboxTop(tc.top, tc.height, tc.viewTop, tc.viewHt); got != tc.want {
				t.Errorf("CheckboxTop = %v, want %v", got, tc.want)
			}
		})
	}
}
