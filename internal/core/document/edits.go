package document

import (
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/errors"
)

// Edit replaces a range with text. In a batch every range is in the text as it
// was before the batch.
type Edit struct {
	Range buffer.Range
	Text  string
}

// Change is an edit as it was applied: where it was, where its text went, and
// both texts.
type Change struct {
	// OldRange, OldOffset and OldText are the replaced text in the text before the batch.
	OldRange  buffer.Range
	OldOffset int
	OldText   string
	// NewRange, NewOffset and NewText are the inserted text in the text after the batch.
	NewRange  buffer.Range
	NewOffset int
	NewText   string
}

// Cause says why a document changed.
type Cause int

const (
	// CauseEdit is an edit.
	CauseEdit Cause = iota
	// CauseUndo is an undo.
	CauseUndo
	// CauseRedo is a redo.
	CauseRedo
	// CauseLineEnding is a change of the line ending, which changes no text.
	CauseLineEnding
	// CauseSetText is a replacement of the whole text that clears the history.
	CauseSetText
)

// ChangeEvent tells that a document changed.
type ChangeEvent struct {
	Cause Cause
	// Version and AlternativeVersion are the document's ids after the change.
	Version            int
	AlternativeVersion int
	// Changes are the replacements of the batch, in order of position.
	Changes []Change
	// Before and After are the text before and after the change.
	Before, After buffer.Snapshot
	// Selections are the selections an undo or a redo restores; nil otherwise.
	Selections []buffer.Selection
}

type listener struct{ fn func(ChangeEvent) }

// Subscribe registers fn to be called, in the goroutine that changed the
// document, after every change. The returned function removes it.
func (d *Document) Subscribe(fn func(ChangeEvent)) (unsubscribe func()) {
	l := &listener{fn: fn}
	d.listeners = append(d.listeners, l)
	return func() {
		for i, x := range d.listeners {
			if x == l {
				d.listeners = append(d.listeners[:i:i], d.listeners[i+1:]...)
				return
			}
		}
	}
}

func (d *Document) emit(ev ChangeEvent) {
	for _, l := range append([]*listener(nil), d.listeners...) {
		l.fn(ev)
	}
}

// EditKind classifies an edit for undo grouping (SPEC §7.1).
type EditKind int

const (
	// KindOther edits always start a new undo group.
	KindOther EditKind = iota
	// KindTyping is typing text.
	KindTyping
	// KindDeleteLeft is backspace.
	KindDeleteLeft
	// KindDeleteRight is the Delete key.
	KindDeleteRight
)

// EditOptions describe an edit for the undo history.
type EditOptions struct {
	// Kind decides whether the edit joins the previous undo group: edits of the
	// same kind other than KindOther join it while it is open and the cursors are
	// where it left them.
	Kind EditKind
	// Before are the selections before the edit; an undo of the group restores
	// the ones of its first edit.
	Before []buffer.Selection
	// After computes the selections after the edit from the inverse edits (whose
	// ranges are in the new text); a redo of the group restores the ones of its
	// last edit. It may be nil.
	After func(inverse []Edit) []buffer.Selection
}

// EditResult is what an edit did.
type EditResult struct {
	// Changed reports whether the text changed; a batch of no-op edits changes nothing.
	Changed bool
	// Inverse is the batch that undoes the edit when applied to the new text.
	Inverse []Edit
	// Selections are the selections After computed.
	Selections []buffer.Selection
}

// prepared is an edit with validated offsets.
type prepared struct {
	start, end int
	text       string
	index      int
}

// normalizeNewlines turns "\r\n" and "\r" in inserted text into "\n": the buffer holds only "\n".
func normalizeNewlines(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// prepare validates a batch against snap: ranges are moved into the text, text
// is normalized, edits that change nothing are dropped, the rest is sorted by
// position (equal positions keep their order), and overlapping edits are refused
// with the tag EditsOverlap.
func prepare(snap buffer.Snapshot, batch []Edit) ([]prepared, error) {
	var out []prepared
	for i, e := range batch {
		r := snap.ValidateRange(e.Range)
		p := prepared{start: snap.OffsetAt(r.Start), end: snap.OffsetAt(r.End), text: normalizeNewlines(e.Text), index: i}
		if p.start == p.end && p.text == "" {
			continue
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].start != out[j].start {
			return out[i].start < out[j].start
		}
		return out[i].end < out[j].end
	})
	for i := 1; i < len(out); i++ {
		if out[i].start < out[i-1].end {
			return nil, errors.New("EditsOverlap", "two edits of a batch overlap",
				"first", out[i-1].index, "second", out[i].index)
		}
	}
	return out, nil
}

// textChange is a replacement by byte offset in the text before its step: at
// start, oldText becomes newText.
type textChange struct {
	start   int
	oldText string
	newText string
}

// Edit applies a batch of non-overlapping edits and records it in the undo
// history. It fails with the tags DocumentReadOnly and EditsOverlap.
func (d *Document) Edit(batch []Edit, opts EditOptions) (EditResult, error) {
	if err := d.checkWritable(); err != nil {
		return EditResult{}, err
	}
	before := d.buf.Snapshot()
	edits, err := prepare(before, batch)
	if err != nil {
		return EditResult{}, err
	}
	if len(edits) == 0 {
		return EditResult{}, nil
	}
	altBefore := d.altVersion
	changes := make([]textChange, len(edits))
	for i, e := range edits {
		old, _ := before.Slice(e.start, e.end)
		changes[i] = textChange{start: e.start, oldText: old, newText: e.text}
	}
	d.applyChanges(changes)

	d.version++
	d.altVersion = d.version
	after := d.buf.Snapshot()
	inverse := inverseEdits(after, changes)
	var selections []buffer.Selection
	if opts.After != nil {
		selections = opts.After(inverse)
	}
	d.history.record(changes, altBefore, d.altVersion, d.lineEnding, d.lineEnding, opts, selections)
	d.emit(ChangeEvent{
		Cause: CauseEdit, Version: d.version, AlternativeVersion: d.altVersion,
		Changes: eventChanges(before, after, changes), Before: before, After: after,
	})
	return EditResult{Changed: true, Inverse: inverse, Selections: selections}, nil
}

// applyChanges applies ascending, non-overlapping changes to the buffer, from the
// last to the first so that the offsets of the rest stay right.
func (d *Document) applyChanges(changes []textChange) {
	d.moveDecorations(changes)
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		// The offsets were validated against this text, so this cannot fail.
		_ = d.buf.Replace(c.start, c.start+len(c.oldText), c.newText)
	}
}

// inverseChanges are the changes that undo changes, in the text after them.
func inverseChanges(changes []textChange) []textChange {
	out := make([]textChange, len(changes))
	delta := 0
	for i, c := range changes {
		out[i] = textChange{start: c.start + delta, oldText: c.newText, newText: c.oldText}
		delta += len(c.newText) - len(c.oldText)
	}
	return out
}

// inverseEdits are the edits that undo changes, with ranges in the text after them.
func inverseEdits(after buffer.Snapshot, changes []textChange) []Edit {
	inv := inverseChanges(changes)
	out := make([]Edit, len(inv))
	for i, c := range inv {
		out[i] = Edit{
			Range: buffer.Range{Start: after.PositionAt(c.start), End: after.PositionAt(c.start + len(c.oldText))},
			Text:  c.newText,
		}
	}
	return out
}

func eventChanges(before, after buffer.Snapshot, changes []textChange) []Change {
	out := make([]Change, len(changes))
	delta := 0
	for i, c := range changes {
		newStart := c.start + delta
		out[i] = Change{
			OldRange:  buffer.Range{Start: before.PositionAt(c.start), End: before.PositionAt(c.start + len(c.oldText))},
			OldOffset: c.start, OldText: c.oldText,
			NewRange:  buffer.Range{Start: after.PositionAt(newStart), End: after.PositionAt(newStart + len(c.newText))},
			NewOffset: newStart, NewText: c.newText,
		}
		delta += len(c.newText) - len(c.oldText)
	}
	return out
}

// SetLineEnding changes the line ending the file is saved with. The change is
// undoable, as in VS Code, though it changes no text.
func (d *Document) SetLineEnding(le fileio.LineEnding) error {
	if err := d.checkWritable(); err != nil {
		return err
	}
	if le == d.lineEnding || le == "" {
		return nil
	}
	before := d.buf.Snapshot()
	oldEOL, altBefore := d.lineEnding, d.altVersion
	d.lineEnding = le
	d.version++
	d.altVersion = d.version
	d.history.recordEOL(altBefore, d.altVersion, oldEOL, le)
	d.emit(ChangeEvent{Cause: CauseLineEnding, Version: d.version, AlternativeVersion: d.altVersion, Before: before, After: before})
	return nil
}

// SetText replaces the whole text, as when a file is reloaded after it changed on
// disk. The undo history is cleared, the document counts as saved, and the text is
// "\n" only.
func (d *Document) SetText(text string) {
	before := d.buf.Snapshot()
	text = normalizeNewlines(text)
	d.buf = buffer.New(text)
	d.version++
	d.altVersion = d.version
	d.savedVersion = d.altVersion
	d.history.clear()
	d.clearDecorations()
	after := d.buf.Snapshot()
	old := before.String()
	d.emit(ChangeEvent{
		Cause: CauseSetText, Version: d.version, AlternativeVersion: d.altVersion, Before: before, After: after,
		Changes: []Change{{
			OldRange: buffer.Range{End: before.PositionAt(before.Len())}, OldText: old,
			NewRange: buffer.Range{End: after.PositionAt(after.Len())}, NewText: text,
		}},
	})
}
