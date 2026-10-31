package quickdiff

import (
	"context"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/diff"
)

func linesOf(text string) []string {
	lines, _ := diff.SplitLines(text)
	return lines
}

func TestChanges(t *testing.T) {
	tests := []struct {
		name             string
		original, edited string
		ignoreTrim       bool
		want             []Change
		wantTypes        []ChangeType
	}{
		{"no change", "a\nb\nc\n", "a\nb\nc\n", false, nil, nil},
		{"an insertion at the start", "a\nb\nc\n", "x\na\nb\nc\n", false, []Change{{0, 0, 1, 1}}, []ChangeType{Add}},
		{"an insertion in the middle", "a\nb\nc\n", "a\nx\nb\nc\n", false, []Change{{1, 0, 2, 2}}, []ChangeType{Add}},
		{"an insertion of two lines at the end", "a\nb\nc\n", "a\nb\nc\nx\ny\n", false, []Change{{3, 0, 4, 5}}, []ChangeType{Add}},
		{"a deletion at the start", "a\nb\nc\n", "b\nc\n", false, []Change{{1, 1, 0, 0}}, []ChangeType{Delete}},
		{"a deletion in the middle", "a\nb\nc\n", "a\nc\n", false, []Change{{2, 2, 1, 0}}, []ChangeType{Delete}},
		{"a deletion at the end", "a\nb\nc\n", "a\nb\n", false, []Change{{3, 3, 2, 0}}, []ChangeType{Delete}},
		{"a modification", "a\nb\nc\n", "a\nB\nc\n", false, []Change{{2, 2, 2, 2}}, []ChangeType{Modify}},
		{"a modification that grows", "a\nb\nc\n", "a\nB\nB2\nc\n", false, []Change{{2, 2, 2, 3}}, []ChangeType{Modify}},
		{"two changes apart", "a\nb\nc\nd\ne\n", "A\nb\nc\nd\nE\n", false, []Change{{1, 1, 1, 1}, {5, 5, 5, 5}}, []ChangeType{Modify, Modify}},
		{"an add and a delete next to each other", "a\nb\nc\nd\n", "a\nx\nc\n", false, []Change{{2, 2, 2, 2}, {4, 4, 3, 0}}, []ChangeType{Modify, Delete}},
		{"CRLF original against an LF document", "a\r\nb\r\n", "a\nb\n", false, nil, nil},
		{"CRLF original with a change", "a\r\nb\r\nc\r\n", "a\nB\nc\n", false, []Change{{2, 2, 2, 2}}, []ChangeType{Modify}},
		{"white space at the ends of lines counts", "a\n  b\n", "a\nb  \n", false, []Change{{2, 2, 2, 2}}, []ChangeType{Modify}},
		{"white space at the ends of lines is ignored", "a\n  b\n", "a\nb  \n", true, nil, nil},
		{"an empty original", "", "a\nb\n", false, []Change{{1, 1, 1, 3}}, []ChangeType{Modify}},
		{"an empty document", "a\nb\n", "", false, []Change{{1, 3, 1, 1}}, []ChangeType{Modify}},
		{"the trailing line break is new", "a\nb\nc", "a\nb\nc\n", false, []Change{{3, 0, 4, 4}}, []ChangeType{Add}},
		{"the trailing line break is gone", "a\nb\nc\n", "a\nb\nc", false, []Change{{4, 4, 3, 0}}, []ChangeType{Delete}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Changes(context.Background(), linesOf(tt.original), linesOf(tt.edited), DiffOptions{IgnoreTrimWhitespace: tt.ignoreTrim})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("changes = %+v, want %+v", got, tt.want)
			}
			for i, c := range got {
				if c != tt.want[i] {
					t.Errorf("change %d = %+v, want %+v", i, c, tt.want[i])
				}
				if c.Type() != tt.wantTypes[i] {
					t.Errorf("change %d is %v, want %v", i, c.Type(), tt.wantTypes[i])
				}
			}
		})
	}
}

func TestChangesStopWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Changes(ctx, linesOf("a\nb"), linesOf("a\nc"), DiffOptions{})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if appErr.Tag != "DiffCancelled" || !appErr.HasRoute("computeDiff.DiffCancelled") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestChangesTimeLimit(t *testing.T) {
	// a clock that jumps a second at every look: the diff is over its limit at once and the answer is the one change that covers both texts
	now := time.Unix(0, 0)
	clock := func() time.Time { now = now.Add(time.Second); return now }
	var a, b []string
	for i := range 3000 {
		a = append(a, "line "+string(rune('a'+i%26)))
		b = append(b, "line "+string(rune('b'+i%25)))
	}
	got, err := Changes(context.Background(), a, b, DiffOptions{TimeLimit: time.Millisecond, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (Change{1, 3000, 1, 3000}) {
		t.Errorf("changes = %+v, want the one change that covers both texts", got)
	}
}

func TestChangeHelpers(t *testing.T) {
	tests := []struct {
		name       string
		c          Change
		end        int
		height     int
		intersects map[int]bool
	}{
		{"added", Change{3, 0, 4, 6}, 6, 3, map[int]bool{3: false, 4: true, 6: true, 7: false}},
		{"modified", Change{2, 3, 2, 5}, 5, 6, map[int]bool{1: false, 2: true, 5: true, 6: false}},
		{"deleted after line 5", Change{6, 7, 5, 0}, 5, 2, map[int]bool{5: true, 4: false, 6: false}},
		{"deleted at the start", Change{1, 2, 0, 0}, 1, 2, map[int]bool{1: true, 2: false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.ModifiedEndLine(); got != tt.end {
				t.Errorf("ModifiedEndLine = %d, want %d", got, tt.end)
			}
			if got := tt.c.Height(); got != tt.height {
				t.Errorf("Height = %d, want %d", got, tt.height)
			}
			for line, want := range tt.intersects {
				if got := tt.c.Intersects(line); got != want {
					t.Errorf("Intersects(%d) = %v, want %v", line, got, want)
				}
			}
		})
	}
}

func TestCompare(t *testing.T) {
	a, b := Change{1, 1, 2, 3}, Change{1, 1, 2, 4}
	if Compare(a, b) >= 0 || Compare(b, a) <= 0 || Compare(a, a) != 0 {
		t.Errorf("Compare is not an order: %d %d %d", Compare(a, b), Compare(b, a), Compare(a, a))
	}
	if Compare(Change{0, 0, 1, 1}, Change{0, 0, 2, 2}) >= 0 {
		t.Error("a change at an earlier line does not come first")
	}
}

func TestNextAndPrevious(t *testing.T) {
	changes := []Change{{0, 0, 2, 3}, {5, 5, 6, 0}, {9, 9, 10, 10}}
	tests := []struct {
		name       string
		line       int
		inclusive  bool
		next, prev int
	}{
		{"before everything", 1, false, 0, 2},
		{"on the first change", 2, false, 1, 2},
		{"inside the first change", 3, false, 1, 2},
		{"after the first change", 4, false, 1, 0},
		{"on the deleted line", 6, false, 2, 0},
		{"between the second and third", 8, false, 2, 1},
		{"on the last change", 10, false, 0, 1},
		{"after everything wraps to the first", 12, false, 0, 2},
		{"inclusive on the first change", 2, true, 0, 0},
		{"inclusive after the first", 4, true, 1, 0},
		{"inclusive on the last", 10, true, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FindNext(changes, tt.line, tt.inclusive); got != tt.next {
				t.Errorf("FindNext(%d, %v) = %d, want %d", tt.line, tt.inclusive, got, tt.next)
			}
			if got := FindPrevious(changes, tt.line, tt.inclusive); got != tt.prev {
				t.Errorf("FindPrevious(%d, %v) = %d, want %d", tt.line, tt.inclusive, got, tt.prev)
			}
		})
	}
	if FindPrevious(nil, 3, false) != -1 || FindNext(nil, 3, false) != 0 {
		t.Error("an empty list is not handled")
	}
}
