package diffeditor

import (
	"strconv"
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
)

func TestChangeToGo(t *testing.T) {
	three := []diff.LineRangeMapping{change(3, 4, 3, 5), change(8, 8, 9, 10), change(11, 12, 12, 12)}
	for _, tc := range []struct {
		name    string
		changes []diff.LineRangeMapping
		line    int
		next    bool
		want    int
		wantOK  bool
	}{
		{"next from the top", three, 1, true, 0, true},
		{"next from a change goes to the one after", three, 3, true, 1, true},
		{"next from between changes", three, 5, true, 1, true},
		{"next from the line before a change", three, 8, true, 1, true},
		{"next from the last change wraps", three, 12, true, 0, true},
		{"next from below the last change wraps", three, 15, true, 0, true},
		{"next from the last line wraps even when a change is on it", []diff.LineRangeMapping{change(2, 3, 2, 3), change(20, 20, 20, 21)}, 20, true, 0, true},
		{"previous from the top wraps to the last", three, 1, false, 2, true},
		{"previous from a change goes to the one before", three, 12, false, 1, true},
		{"previous from the first change wraps to the last", three, 3, false, 2, true},
		{"previous from between changes", three, 4, false, 0, true},
		{"previous from below the last change", three, 20, false, 2, true},
		{"next with one change, before it", three[:1], 1, true, 0, true},
		{"next with one change, on it", three[:1], 3, true, 0, true},
		{"previous with one change, after it", three[:1], 9, false, 0, true},
		{"previous with one change, on it", three[:1], 3, false, 0, true},
		{"no changes, next", nil, 4, true, 0, false},
		{"no changes, previous", nil, 4, false, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ChangeToGo(tc.changes, tc.line, 20, tc.next)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("ChangeToGo = %d, %v; want %d, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestGoToChangeMovesTheCursorAndShowsIt(t *testing.T) {
	original := lines(60, func(i int) string { return "line " + strconv.Itoa(i) })
	// three changes of the modified text, far apart: lines 10, 30 and 50 (counted from 1)
	changed := map[int]string{9: "changed ten", 29: "changed thirty", 49: "changed fifty"}
	modified := lines(60, func(i int) string {
		if s, ok := changed[i]; ok {
			return s
		}
		return "line " + strconv.Itoa(i)
	})
	r := newRig(t, original, modified, rigConfig{size: ui.Size{Width: 900, Height: 300}})
	if got := len(r.model.Changes()); got != 3 {
		t.Fatalf("%d changes", got)
	}
	mod := r.model.View(Modified)
	at := func() int { return mod.Cursors().Primary().Position().Line + 1 }
	for _, step := range []struct {
		next bool
		line int
	}{{true, 10}, {true, 30}, {true, 50}, {true, 10}, {false, 50}, {false, 30}, {false, 10}} {
		if !r.model.GoToChange(step.next) {
			t.Fatal("no change")
		}
		r.settle()
		if at() != step.line || mod.Cursors().Primary().Position().Col != 0 {
			t.Fatalf("next=%v: the cursor is at line %d, want %d", step.next, at(), step.line)
		}
		// the change is in the middle of the editor: its row is within the viewport, near its center
		proj := mod.Projection()
		row := mod.RowOfPosition(mod.Cursors().Primary().Position())
		_, viewport := mod.ViewportSize()
		mid := proj.RowTop(row) + proj.RowHeight(row)/2 - mod.ScrollTop()
		if mid < 0 || mid > viewport || (mod.ScrollTop() > 0 && absDiff(mid, viewport/2) > 20) {
			t.Errorf("line %d is %v pixels from the top of a viewport of %v (scrolled %v)", step.line, mid, viewport, mod.ScrollTop())
		}
	}
	r.noErrors()
}

func absDiff(a, b float64) float64 {
	if a < b {
		return b - a
	}
	return a - b
}

func TestGoToChangeWithoutChanges(t *testing.T) {
	r := newRig(t, "same\ntext\n", "same\ntext\n", rigConfig{})
	if r.model.GoToChange(true) || r.model.GoToChange(false) {
		t.Error("went to a change of identical texts")
	}
	if !r.model.NoChanges() {
		t.Error("identical texts have changes")
	}
	if got := r.model.View(Modified).Cursors().Primary().Position(); got != (buffer.Position{}) {
		t.Errorf("the cursor moved to %v", got)
	}
}

func TestGoToChangeAtTheEndOfTheText(t *testing.T) {
	// the lines at the end of the original are deleted: the change is an empty range after the last line
	r := newRig(t, "a\nb\nc\nd\n", "a\nb\n", rigConfig{})
	ch := r.model.Changes()
	if len(ch) != 1 || !ch[0].Modified.IsEmpty() {
		t.Fatalf("changes %+v", ch)
	}
	if !r.model.GoToChange(true) {
		t.Fatal("no change")
	}
	// the start of the change is beyond the text (line 3 of a text that has an empty line 3): the cursor goes there
	if got := r.model.View(Modified).Cursors().Primary().Position(); got.Line > 2 {
		t.Errorf("cursor %v is outside the text", got)
	}
}
