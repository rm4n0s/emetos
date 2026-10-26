package document

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/fileio"
)

// step is one applied batch: the changes by offset in the text before it, and the
// line ending before and after (they differ only for a change of line ending).
type step struct {
	changes   []textChange
	eolBefore fileio.LineEnding
	eolAfter  fileio.LineEnding
}

// group is what one undo undoes: one or more steps, the selections before the
// first and after the last, and the alternative version ids at both ends (SPEC §7.1,
// VS Code's EditStack elements).
type group struct {
	steps               []step
	altBefore, altAfter int
	selBefore, selAfter []buffer.Selection
	kind                EditKind
	typing              typingState
	closed              bool
}

// typingState tells what the last typing edit of a group was, for VS Code's rule
// that undo stops before the first space typed after other characters.
type typingState int

const (
	typedOther typingState = iota
	typedFirstSpace
	typedConsecutiveSpace
)

// typingStateOf classifies a typing edit that follows an edit of state prev
// (typedOther when there is none): a batch that only inserts single spaces is a
// first space, or a consecutive one after another space.
func typingStateOf(changes []textChange, prev typingState) typingState {
	if len(changes) == 0 {
		return typedOther
	}
	for _, c := range changes {
		if c.newText != " " {
			return typedOther
		}
	}
	if prev == typedFirstSpace || prev == typedConsecutiveSpace {
		return typedConsecutiveSpace
	}
	return typedFirstSpace
}

// history is the undo and redo stacks of a document.
type history struct {
	undo []*group
	redo []*group
}

func (h *history) top() *group {
	if len(h.undo) == 0 {
		return nil
	}
	return h.undo[len(h.undo)-1]
}

func (h *history) clear() {
	h.undo, h.redo = nil, nil
}

// record adds the step of an edit to the open group when the edit may join it, or
// to a new group, and drops the redo stack.
func (h *history) record(changes []textChange, altBefore, altAfter int, eolBefore, eolAfter fileio.LineEnding, opts EditOptions, after []buffer.Selection) {
	h.redo = nil
	s := step{changes: changes, eolBefore: eolBefore, eolAfter: eolAfter}
	typing := typedOther
	if opts.Kind == KindTyping {
		typing = typingStateOf(changes, typedOther)
	}
	if g := h.top(); g != nil && g.canJoin(opts) {
		if opts.Kind == KindTyping {
			if next := typingStateOf(changes, g.typing); g.joinsTyping(next) {
				g.appendStep(s)
				g.altAfter, g.selAfter, g.typing = altAfter, after, next
				return
			}
		} else {
			g.appendStep(s)
			g.altAfter, g.selAfter = altAfter, after
			return
		}
	}
	h.undo = append(h.undo, &group{
		steps: []step{s}, altBefore: altBefore, altAfter: altAfter,
		selBefore: opts.Before, selAfter: after, kind: opts.Kind, typing: typing,
	})
}

// recordEOL adds a change of line ending as a group of its own.
func (h *history) recordEOL(altBefore, altAfter int, eolBefore, eolAfter fileio.LineEnding) {
	h.redo = nil
	h.undo = append(h.undo, &group{
		steps:     []step{{eolBefore: eolBefore, eolAfter: eolAfter}},
		altBefore: altBefore, altAfter: altAfter, closed: true,
	})
}

// canJoin reports whether an edit with opts continues this group: it is open and the edit has Join, or
// the edit is of the same kind as the group (and not KindOther), and the cursors
// are where the group left them. Unknown cursors don't prevent it.
func (g *group) canJoin(opts EditOptions) bool {
	if opts.Join {
		return !g.closed
	}
	if g.closed || opts.Kind == KindOther || g.kind != opts.Kind {
		return false
	}
	if g.selAfter == nil || opts.Before == nil {
		return true
	}
	return buffer.SelectionsEqual(g.selAfter, opts.Before)
}

// joinsTyping reports whether typing of state next continues the group's typing:
// text typed after a single space continues it, so does a space after a space, but
// a space after other characters, and other characters after spaces, start a new
// group (VS Code's shouldPushStackElementBetween).
func (g *group) joinsTyping(next typingState) bool {
	if g.typing == typedFirstSpace {
		return true
	}
	return (g.typing == typedOther) == (next == typedOther)
}

// appendStep adds a step, folding it into the last one when it continues it: text
// typed right after text typed, a backspace right before a deletion, a Delete at
// the place of a deletion. That keeps a long run of typing a single change.
func (g *group) appendStep(s step) {
	last := &g.steps[len(g.steps)-1]
	if len(last.changes) == 1 && len(s.changes) == 1 && last.eolAfter == s.eolBefore && last.eolBefore == last.eolAfter {
		prev, next := &last.changes[0], s.changes[0]
		switch {
		case next.oldText == "" && next.start == prev.start+len(prev.newText):
			prev.newText += next.newText
			return
		case next.newText == "" && prev.newText == "" && next.start+len(next.oldText) == prev.start:
			prev.start = next.start
			prev.oldText = next.oldText + prev.oldText
			return
		case next.newText == "" && prev.newText == "" && next.start == prev.start:
			prev.oldText += next.oldText
			return
		}
	}
	g.steps = append(g.steps, s)
}
