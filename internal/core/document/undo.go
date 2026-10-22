package document

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// PushStackElement closes the current undo group: the next edit starts a new one
// (VS Code's pushStackElement, called when the cursor moves by other means).
func (d *Document) PushStackElement() {
	if g := d.history.top(); g != nil {
		g.closed = true
	}
}

// PopStackElement reopens the current undo group so that the next edit may join
// it (VS Code's popStackElement).
func (d *Document) PopStackElement() {
	if g := d.history.top(); g != nil {
		g.closed = false
	}
}

// ClearHistory forgets every undo and redo.
func (d *Document) ClearHistory() { d.history.clear() }

// CanUndo reports whether there is something to undo.
func (d *Document) CanUndo() bool { return len(d.history.undo) > 0 }

// CanRedo reports whether there is something to redo.
func (d *Document) CanRedo() bool { return len(d.history.redo) > 0 }

// HistoryResult is what an undo or a redo did.
type HistoryResult struct {
	// Done reports whether there was anything to undo or redo.
	Done bool
	// Selections are the selections to put back: the ones from before the undone
	// group, or from after the redone one. They may be nil.
	Selections []buffer.Selection
}

// Undo undoes the last group. The text, the line ending and the alternative
// version id go back to what they were before it. One change event is sent for
// each step of the group, in order, each with its own Before and After snapshots
// (use them, not the document's snapshot, which is the final one); the last
// carries the Selections. It fails with the tag DocumentReadOnly.
func (d *Document) Undo() (HistoryResult, error) {
	if err := d.checkWritable(); err != nil {
		return HistoryResult{}, err
	}
	n := len(d.history.undo)
	if n == 0 {
		return HistoryResult{}, nil
	}
	g := d.history.undo[n-1]
	d.history.undo = d.history.undo[:n-1]
	g.closed = true
	d.history.redo = append(d.history.redo, g)

	events := d.runSteps(g, true)
	d.altVersion = g.altBefore
	d.finish(events, g.selBefore)
	return HistoryResult{Done: true, Selections: g.selBefore}, nil
}

// Redo redoes the last undone group; see Undo. It fails with the tag DocumentReadOnly.
func (d *Document) Redo() (HistoryResult, error) {
	if err := d.checkWritable(); err != nil {
		return HistoryResult{}, err
	}
	n := len(d.history.redo)
	if n == 0 {
		return HistoryResult{}, nil
	}
	g := d.history.redo[n-1]
	d.history.redo = d.history.redo[:n-1]
	g.closed = true
	d.history.undo = append(d.history.undo, g)

	events := d.runSteps(g, false)
	d.altVersion = g.altAfter
	d.finish(events, g.selAfter)
	return HistoryResult{Done: true, Selections: g.selAfter}, nil
}

// runSteps applies the steps of a group, backward and inverted for an undo, and
// returns an event for each, with versions not yet final.
func (d *Document) runSteps(g *group, undo bool) []ChangeEvent {
	var events []ChangeEvent
	apply := func(s step) {
		before := d.buf.Snapshot()
		changes, eol := s.changes, s.eolAfter
		if undo {
			changes, eol = inverseChanges(s.changes), s.eolBefore
		}
		d.applyChanges(changes)
		if eol != "" {
			d.lineEnding = eol
		}
		d.version++
		after := d.buf.Snapshot()
		cause := CauseRedo
		if undo {
			cause = CauseUndo
		}
		ev := ChangeEvent{Cause: cause, Version: d.version, Before: before, After: after, Changes: eventChanges(before, after, changes)}
		if len(changes) == 0 {
			ev.Cause = CauseLineEnding
		}
		events = append(events, ev)
	}
	if undo {
		for i := len(g.steps) - 1; i >= 0; i-- {
			apply(g.steps[i])
		}
	} else {
		for _, s := range g.steps {
			apply(s)
		}
	}
	return events
}

// finish sends the events of an undo or a redo with the final alternative version.
func (d *Document) finish(events []ChangeEvent, selections []buffer.Selection) {
	for i := range events {
		events[i].AlternativeVersion = d.altVersion
		if i == len(events)-1 {
			events[i].Selections = selections
		}
		d.emit(events[i])
	}
}
