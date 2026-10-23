package edit

import (
	"sort"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// AutoClosedTracker keeps the closing characters that the editor typed (Result.AutoClosed) as
// decorations of a document, so that they move with the text, and drops them once the cursors
// leave the pair (SPEC §8.6.4). Overtyping and deleting them read Characters.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursor.ts (AutoClosedAction)
type AutoClosedTracker struct {
	doc     *document.Document
	owner   int
	actions []autoClosedAction
}

// autoClosedAction is the closing characters one command typed, with the pairs around them.
type autoClosedAction struct {
	characters, enclosing []int // decoration ids
}

// NewAutoClosedTracker makes a tracker whose decorations belong to owner (1 or more).
func NewAutoClosedTracker(doc *document.Document, owner int) *AutoClosedTracker {
	return &AutoClosedTracker{doc: doc, owner: owner}
}

// Add starts tracking what a result typed. The cursors must have been set to the selections of
// the result before: they are not tested until they change.
func (t *AutoClosedTracker) Add(closed []AutoClosed) {
	if len(closed) == 0 {
		return
	}
	opts := document.DecorationOptions{Stickiness: document.NeverGrowsWhenTypingAtEdges}
	chars := make([]document.NewDecoration, len(closed))
	enclosing := make([]document.NewDecoration, len(closed))
	for i, c := range closed {
		chars[i] = document.NewDecoration{Range: c.Character, Options: opts}
		enclosing[i] = document.NewDecoration{Range: c.Enclosing, Options: opts}
	}
	t.actions = append(t.actions, autoClosedAction{
		characters: t.doc.DeltaDecorations(t.owner, nil, chars),
		enclosing:  t.doc.DeltaDecorations(t.owner, nil, enclosing),
	})
}

// Characters are the ranges of the tracked closing characters now.
func (t *AutoClosedTracker) Characters() []buffer.Range {
	var out []buffer.Range
	for _, a := range t.actions {
		for _, id := range a.characters {
			if d, ok := t.doc.Decoration(id); ok {
				out = append(out, d.Range)
			}
		}
	}
	return out
}

// Validate stops tracking the pairs that the selections are not inside of any more: every
// selection must be strictly inside a pair of the action, and no pair may have become several
// lines long.
func (t *AutoClosedTracker) Validate(sels []buffer.Selection) {
	if len(t.actions) == 0 {
		return
	}
	kept := t.actions[:0]
	for _, a := range t.actions {
		if t.valid(a, sels) {
			kept = append(kept, a)
		} else {
			t.doc.RemoveDecorations(a.characters...)
			t.doc.RemoveDecorations(a.enclosing...)
		}
	}
	t.actions = kept
}

func (t *AutoClosedTracker) valid(a autoClosedAction, sels []buffer.Selection) bool {
	var enclosing []buffer.Range
	for _, id := range a.enclosing {
		d, ok := t.doc.Decoration(id)
		if !ok {
			continue
		}
		if d.Range.Start.Line != d.Range.End.Line {
			return false
		}
		enclosing = append(enclosing, d.Range)
	}
	ranges := make([]buffer.Range, len(sels))
	for i, s := range sels {
		ranges[i] = buffer.Range{Start: s.Start(), End: s.End()}
	}
	sort.SliceStable(enclosing, func(i, j int) bool { return buffer.CompareUsingStarts(enclosing[i], enclosing[j]) < 0 })
	sort.SliceStable(ranges, func(i, j int) bool { return buffer.CompareUsingStarts(ranges[i], ranges[j]) < 0 })
	for i, r := range ranges {
		if i >= len(enclosing) || !enclosing[i].StrictContainsRange(r) {
			return false
		}
	}
	return true
}

// Clear stops tracking everything.
func (t *AutoClosedTracker) Clear() {
	for _, a := range t.actions {
		t.doc.RemoveDecorations(a.characters...)
		t.doc.RemoveDecorations(a.enclosing...)
	}
	t.actions = nil
}
