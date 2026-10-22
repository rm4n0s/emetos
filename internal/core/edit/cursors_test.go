package edit

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func sel(al, ac, l, c int) buffer.Selection {
	return buffer.Selection{Anchor: buffer.Position{Line: al, Col: ac}, Active: buffer.Position{Line: l, Col: c}}
}

func caret(l, c int) buffer.Selection { return sel(l, c, l, c) }

func newTestCursors(sels ...buffer.Selection) *Cursors {
	cs := NewCursors(DefaultOptions())
	cs.SetSelections(sels)
	return cs
}

func TestCursorsStartWithOne(t *testing.T) {
	cs := NewCursors(DefaultOptions())
	if cs.Len() != 1 || cs.Primary().Selection != caret(0, 0) {
		t.Fatalf("a new collection has %d cursors, primary %+v", cs.Len(), cs.Primary().Selection)
	}
	cs.Set(nil)
	if cs.Len() != 1 {
		t.Errorf("setting no cursors changed the collection: %d", cs.Len())
	}
}

// Ported from VS Code: cursorCollection.normalize and the multi-cursor merge
// cases of reference/vscode/src/vs/editor/test/browser/controller/cursor.test.ts.
func TestCursorsNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   []buffer.Selection
		want []buffer.Selection
	}{
		{"apart", []buffer.Selection{caret(0, 1), caret(0, 5)}, []buffer.Selection{caret(0, 1), caret(0, 5)}},
		{"same place", []buffer.Selection{caret(1, 2), caret(1, 2)}, []buffer.Selection{caret(1, 2)}},
		{"empty touching a selection end", []buffer.Selection{sel(0, 0, 0, 3), caret(0, 3)}, []buffer.Selection{sel(0, 0, 0, 3)}},
		{"empty touching a selection start", []buffer.Selection{caret(0, 0), sel(0, 0, 0, 3)}, []buffer.Selection{sel(0, 0, 0, 3)}},
		{"two selections that only touch stay apart", []buffer.Selection{sel(0, 0, 0, 3), sel(0, 3, 0, 5)}, []buffer.Selection{sel(0, 0, 0, 3), sel(0, 3, 0, 5)}},
		{"overlapping selections are joined", []buffer.Selection{sel(0, 0, 0, 4), sel(0, 2, 0, 6)}, []buffer.Selection{sel(0, 0, 0, 6)}},
		{"contained selection", []buffer.Selection{sel(0, 0, 0, 9), sel(0, 2, 0, 3)}, []buffer.Selection{sel(0, 0, 0, 9)}},
		{"order is kept for the primary", []buffer.Selection{caret(2, 0), caret(0, 0), caret(1, 0)}, []buffer.Selection{caret(2, 0), caret(0, 0), caret(1, 0)}},
		{"primary survives a merge", []buffer.Selection{caret(2, 0), caret(0, 0), caret(2, 0)}, []buffer.Selection{caret(2, 0), caret(0, 0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newTestCursors(tt.in...).Selections()
			if !buffer.SelectionsEqual(got, tt.want) {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestCursorsNormalizeKeepsDirectionOfLastAdded(t *testing.T) {
	cs := newTestCursors(sel(0, 0, 0, 2))
	// Ctrl-dragging a right-to-left selection into the existing one: the joined
	// selection takes the direction of the one just added.
	cs.Add(CursorOf(sel(0, 5, 0, 1)))
	got := cs.Selections()
	if len(got) != 1 || got[0] != sel(0, 5, 0, 0) {
		t.Errorf("got %+v, want one selection 0:5 -> 0:0", got)
	}
}

func TestCursorsMergeCanBeOff(t *testing.T) {
	opts := DefaultOptions()
	opts.MultiCursorMergeOverlapping = false
	cs := NewCursors(opts)
	cs.SetSelections([]buffer.Selection{caret(0, 1), caret(0, 1)})
	if cs.Len() != 2 {
		t.Errorf("cursors = %d, want 2 when merging is off", cs.Len())
	}
}

func TestCursorsMapAndCancelSecondary(t *testing.T) {
	cs := newTestCursors(caret(0, 1), caret(1, 1), caret(2, 1))
	cs.Map(func(c Cursor) Cursor {
		return CursorOf(caret(c.Position().Line, 0))
	})
	if want := []buffer.Selection{caret(0, 0), caret(1, 0), caret(2, 0)}; !buffer.SelectionsEqual(cs.Selections(), want) {
		t.Fatalf("Map: %+v", cs.Selections())
	}
	// Mapping everything to one place merges them.
	cs.Map(func(Cursor) Cursor { return NewCursor(buffer.Position{Line: 4}) })
	if cs.Len() != 1 {
		t.Errorf("Map to one place left %d cursors", cs.Len())
	}
	cs = newTestCursors(caret(3, 3), caret(0, 0), caret(1, 1))
	cs.CancelSecondary()
	if cs.Len() != 1 || cs.Primary().Selection != caret(3, 3) {
		t.Errorf("CancelSecondary: %+v", cs.Selections())
	}
}

func TestCursorHistory(t *testing.T) {
	var h CursorHistory
	a, b, c := []buffer.Selection{caret(0, 0)}, []buffer.Selection{caret(1, 0)}, []buffer.Selection{caret(2, 0)}
	if _, ok := h.Undo(a); ok {
		t.Fatal("an empty history can be undone")
	}
	h.Record(a)
	h.Record(a) // the same state twice in a row is one state
	h.Record(b)
	got, ok := h.Undo(c)
	if !ok || !buffer.SelectionsEqual(got, b) {
		t.Fatalf("undo = %+v %v, want %+v", got, ok, b)
	}
	got, ok = h.Undo(b)
	if !ok || !buffer.SelectionsEqual(got, a) {
		t.Fatalf("second undo = %+v %v, want %+v", got, ok, a)
	}
	if _, ok := h.Undo(a); ok {
		t.Error("undo went past the first state")
	}
	got, ok = h.Redo(a)
	if !ok || !buffer.SelectionsEqual(got, b) {
		t.Fatalf("redo = %+v %v, want %+v", got, ok, b)
	}
	got, ok = h.Redo(b)
	if !ok || !buffer.SelectionsEqual(got, c) {
		t.Fatalf("second redo = %+v %v, want %+v", got, ok, c)
	}
	// A new move forgets what could be redone.
	h.Undo(c)
	h.Record([]buffer.Selection{caret(7, 0)})
	if _, ok := h.Redo(a); ok {
		t.Error("redo works after a new record")
	}
	h.Clear()
	if _, ok := h.Undo(b); ok {
		t.Error("undo works after Clear")
	}
}

func TestCursorHistoryIsBounded(t *testing.T) {
	var h CursorHistory
	for i := 0; i < maxCursorHistory+20; i++ {
		h.Record([]buffer.Selection{caret(i, 0)})
	}
	n := 0
	cur := []buffer.Selection{caret(-1, 0)}
	for {
		prev, ok := h.Undo(cur)
		if !ok {
			break
		}
		cur = prev
		n++
	}
	if n != maxCursorHistory {
		t.Errorf("undid %d states, want %d", n, maxCursorHistory)
	}
}

func TestPageMovement(t *testing.T) {
	lines := ""
	for i := 0; i < 100; i++ {
		lines += "line\n"
	}
	h := newHarness(t, lines)
	h.mv.Layout = ModelLineLayout{Model: h.mv.Model, TabSize: 4, Page: 10}
	h.moveTo(1, 3, false)
	h.cur = h.mv.PageDown(h.cur, false)
	h.assertCursor(11, 3)
	h.cur = h.mv.PageDown(h.cur, true)
	h.assertSelection(11, 3, 21, 3)
	h.cur = h.mv.PageUp(h.cur, false)
	h.assertCursor(1, 3) // a selection collapses to its start, then the page is moved
	h.cur = h.mv.PageUp(h.cur, false)
	h.assertCursor(1, 1) // at the first line it goes to the start
	h.moveTo(95, 3, false)
	h.cur = h.mv.PageDown(h.cur, false)
	h.assertCursor(101, 1) // and at the last line to the end
}

func TestWordPartMovement(t *testing.T) {
	h := newHarness(t, "fooBarBaz snake_case\nnext")
	var stops []int
	for i := 0; i < 8; i++ {
		h.cur = h.mv.WordPartRight(h.cur, false)
		stops = append(stops, h.cur.Position().Line*100+h.cur.Position().Col)
	}
	want := []int{3, 6, 9, 10, 15, 20, 100, 104}
	for i := range want {
		if stops[i] != want[i] {
			t.Fatalf("right stops = %v, want %v", stops, want)
		}
	}
	stops = nil
	for i := 0; i < 8; i++ {
		h.cur = h.mv.WordPartLeft(h.cur, false, false)
		stops = append(stops, h.cur.Position().Line*100+h.cur.Position().Col)
	}
	want = []int{100, 20, 16, 10, 9, 6, 3, 0}
	for i := range want {
		if stops[i] != want[i] {
			t.Fatalf("left stops = %v, want %v", stops, want)
		}
	}
	h.cur = NewCursor(buffer.Position{Col: 3})
	h.cur = h.mv.WordPartRight(h.cur, true)
	if h.cur.Selection != sel(0, 3, 0, 6) {
		t.Errorf("selecting by word part: %+v", h.cur.Selection)
	}
}

func TestExpandLine(t *testing.T) {
	h := newHarness(t, "one\ntwo\nthree")
	h.moveTo(1, 2, false)
	h.cur = h.mv.ExpandLine(h.cur)
	h.assertSelection(1, 1, 2, 1)
	h.cur = h.mv.ExpandLine(h.cur)
	h.assertSelection(1, 1, 3, 1)
	h.cur = h.mv.ExpandLine(h.cur)
	h.assertSelection(1, 1, 3, 6)
	h.cur = h.mv.ExpandLine(h.cur)
	h.assertSelection(1, 1, 3, 6)
}
