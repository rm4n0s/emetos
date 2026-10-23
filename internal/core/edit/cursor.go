package edit

import (
	"sort"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// Cursor is one selection and the visual column vertical moves aim for (SPEC
// §7.2): when the cursor goes up or down over a short line and then on to a long
// one, it returns to the column it started from.
type Cursor struct {
	Selection buffer.Selection
	// DesiredVisualCol is that column, or -1 when there is none and the column of
	// the cursor is used. Any move other than a vertical one clears it.
	DesiredVisualCol int
}

// NewCursor is an empty selection at p.
func NewCursor(p buffer.Position) Cursor {
	return Cursor{Selection: buffer.NewCursor(p), DesiredVisualCol: -1}
}

// CursorOf makes a cursor of a selection.
func CursorOf(s buffer.Selection) Cursor {
	return Cursor{Selection: s, DesiredVisualCol: -1}
}

// Position is the active end: where the cursor is drawn.
func (c Cursor) Position() buffer.Position { return c.Selection.Active }

// moveTo moves the active end to p and, unless selecting, the anchor too.
func (c Cursor) moveTo(selecting bool, p buffer.Position, desired int) Cursor {
	if selecting {
		return Cursor{Selection: buffer.Selection{Anchor: c.Selection.Anchor, Active: p}, DesiredVisualCol: desired}
	}
	return Cursor{Selection: buffer.NewCursor(p), DesiredVisualCol: desired}
}

// Cursors are the cursors of an editor: the primary one first, then the others in
// the order they were added (SPEC §7.2, VS Code's CursorCollection).
type Cursors struct {
	list      []Cursor
	lastAdded int
	merge     bool
}

// NewCursors makes the cursors of an editor: one at the start of the text.
func NewCursors(opts Options) *Cursors {
	return &Cursors{list: []Cursor{NewCursor(buffer.Position{})}, merge: opts.MultiCursorMergeOverlapping}
}

// Len is the number of cursors, at least 1.
func (cs *Cursors) Len() int { return len(cs.list) }

// Primary is the primary cursor.
func (cs *Cursors) Primary() Cursor { return cs.list[0] }

// All returns a copy of the cursors, the primary first.
func (cs *Cursors) All() []Cursor { return append([]Cursor(nil), cs.list...) }

// Selections returns the selections of the cursors, the primary first.
func (cs *Cursors) Selections() []buffer.Selection {
	out := make([]buffer.Selection, len(cs.list))
	for i, c := range cs.list {
		out[i] = c.Selection
	}
	return out
}

// Set replaces the cursors; the first is the primary. An empty list leaves them
// as they are.
func (cs *Cursors) Set(list []Cursor) {
	if len(list) == 0 {
		return
	}
	cs.list = append([]Cursor(nil), list...)
	cs.lastAdded = 0
	cs.Normalize()
}

// SetSelections replaces the cursors by selections.
func (cs *Cursors) SetSelections(sels []buffer.Selection) {
	list := make([]Cursor, len(sels))
	for i, s := range sels {
		list[i] = CursorOf(s)
	}
	cs.Set(list)
}

// Add adds a cursor, which becomes the last added one: when it ends up merged with
// another, its direction counts (as in Ctrl-dragging into another cursor).
func (cs *Cursors) Add(c Cursor) {
	cs.list = append(cs.list, c)
	cs.lastAdded = len(cs.list) - 1
	cs.Normalize()
}

// LastAdded is the index of the cursor added last, which the mouse keeps moving while a
// cursor is dragged out with the multi-cursor modifier; 0 when none was added.
func (cs *Cursors) LastAdded() int { return cs.lastAdded }

// Replace sets the cursor at index i, keeping which one was added last, and merges the
// cursors that meet. An index out of range changes nothing.
func (cs *Cursors) Replace(i int, c Cursor) {
	if i < 0 || i >= len(cs.list) {
		return
	}
	cs.list[i] = c
	cs.Normalize()
}

// Map moves every cursor with fn and merges the ones that meet.
func (cs *Cursors) Map(fn func(Cursor) Cursor) {
	for i, c := range cs.list {
		cs.list[i] = fn(c)
	}
	cs.Normalize()
}

// CancelSecondary removes every cursor but the primary (Escape).
func (cs *Cursors) CancelSecondary() {
	cs.list = cs.list[:1]
	cs.lastAdded = 0
}

// Normalize merges cursors that overlap, or touch when one of them is empty; it
// is VS Code's CursorCollection.normalize. Of two merged cursors the one added
// first wins and keeps its place, with the union of both selections.
func (cs *Cursors) Normalize() {
	if len(cs.list) == 1 || !cs.merge {
		return
	}
	type sorted struct {
		index int
		sel   buffer.Selection
	}
	cursors := append([]Cursor(nil), cs.list...)
	order := make([]sorted, len(cursors))
	for i, c := range cursors {
		order[i] = sorted{i, c.Selection}
	}
	sort.SliceStable(order, func(i, j int) bool {
		return buffer.CompareUsingStarts(order[i].sel.Range(), order[j].sel.Range()) < 0
	})
	for i := 0; i < len(order)-1; i++ {
		cur, next := order[i], order[i+1]
		var shouldMerge bool
		if next.sel.IsEmpty() || cur.sel.IsEmpty() {
			shouldMerge = next.sel.Start().IsBeforeOrEqual(cur.sel.End())
		} else {
			shouldMerge = next.sel.Start().IsBefore(cur.sel.End())
		}
		if !shouldMerge {
			continue
		}
		winner, loser := i, i+1
		if cur.index >= next.index {
			winner, loser = i+1, i
		}
		looserIndex, winnerIndex := order[loser].index, order[winner].index
		looserSel, winnerSel := order[loser].sel, order[winner].sel
		if looserSel != winnerSel {
			union := looserSel.Range().Union(winnerSel.Range())
			ltr := winnerSel.Direction()
			if looserIndex == cs.lastAdded {
				ltr = looserSel.Direction()
				cs.lastAdded = winnerIndex
			}
			result := buffer.SelectionFromRange(union, ltr)
			order[winner].sel = result
			cursors[winnerIndex] = CursorOf(result)
		}
		for k := range order {
			if order[k].index > looserIndex {
				order[k].index--
			}
		}
		cursors = append(cursors[:looserIndex], cursors[looserIndex+1:]...)
		order = append(order[:loser], order[loser+1:]...)
		if cs.lastAdded > looserIndex {
			cs.lastAdded--
		}
		i--
	}
	cs.list = cursors
	if cs.lastAdded >= len(cs.list) {
		cs.lastAdded = 0
	}
}

// maxCursorHistory is how many states cursor undo keeps (VS Code keeps 50).
const maxCursorHistory = 50

// CursorHistory remembers where the cursors were so that Ctrl+U (cursor undo) and
// Ctrl+Shift+U (cursor redo) can go back: states are recorded before an explicit
// move, and edits to the text forget them, since they refer to the old text.
type CursorHistory struct {
	undo [][]buffer.Selection
	redo [][]buffer.Selection
}

// Record remembers the selections before a move. Recording the same selections
// twice in a row is ignored; a new record forgets what could be redone.
func (h *CursorHistory) Record(sels []buffer.Selection) {
	if n := len(h.undo); n > 0 && buffer.SelectionsEqual(h.undo[n-1], sels) {
		return
	}
	h.undo = append(h.undo, append([]buffer.Selection(nil), sels...))
	if len(h.undo) > maxCursorHistory {
		h.undo = h.undo[1:]
	}
	h.redo = nil
}

// Undo returns the selections to go back to, given the current ones, which
// become what Redo returns. The second result is false when there is nothing to
// go back to.
func (h *CursorHistory) Undo(current []buffer.Selection) ([]buffer.Selection, bool) {
	for len(h.undo) > 0 {
		prev := h.undo[len(h.undo)-1]
		h.undo = h.undo[:len(h.undo)-1]
		if buffer.SelectionsEqual(prev, current) {
			continue
		}
		h.redo = append(h.redo, append([]buffer.Selection(nil), current...))
		return prev, true
	}
	return nil, false
}

// Redo undoes an Undo.
func (h *CursorHistory) Redo(current []buffer.Selection) ([]buffer.Selection, bool) {
	for len(h.redo) > 0 {
		next := h.redo[len(h.redo)-1]
		h.redo = h.redo[:len(h.redo)-1]
		if buffer.SelectionsEqual(next, current) {
			continue
		}
		h.undo = append(h.undo, append([]buffer.Selection(nil), current...))
		return next, true
	}
	return nil, false
}

// Clear forgets everything, as when the text is edited.
func (h *CursorHistory) Clear() {
	h.undo, h.redo = nil, nil
}
