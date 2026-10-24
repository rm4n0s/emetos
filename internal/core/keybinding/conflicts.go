package keybinding

import (
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// FindConflicts lists the rows of other commands that have the same key sequence as kb under a when-clause that
// can hold together with when (SPEC §11.3): the keybindings editor warns about them when the user defines a
// keybinding, and offers to overwrite them.
//
// Two when-clauses can hold together unless one of them provably excludes the other: they are the same text, one
// of them is empty, or neither implies the negation of the other (contextkey.Implies; "editorFocus" and
// "!editorFocus", or "a == 1" and "a == 2", exclude each other, "editorTextFocus" and "textInputFocus" do not). VS
// Code's editor warns of every row that has the keys whatever its when-clause, and its resolver proves inclusion
// only (KeybindingResolver.whenIsEntirelyIncluded); this is the middle: a keybinding that is shadowed by another
// is flagged, one in a place that cannot hold with it is not. A keybinding that is the beginning of another
// sequence (ctrl+k and ctrl+k ctrl+c) is not reported: only the same sequence is.
//
// command is the command the keybinding is for; its own rows are not conflicts.
//
// Ported from VS Code: src/vs/platform/keybinding/common/keybindingResolver.ts (whenIsEntirelyIncluded, the
// conflict test of the resolver)
func (m *Model) FindConflicts(kb Keybinding, when, command string) []Row {
	if len(kb) == 0 {
		return nil
	}
	var out []Row
	for _, r := range m.rows {
		if r.Command == command || !r.Bound() || !r.Keybinding.Equal(kb) {
			continue
		}
		if WhensOverlap(r.When, when) {
			out = append(out, r)
		}
	}
	return out
}

// WhensOverlap tells whether two when-clauses, written as text, can hold together (see FindConflicts). A text
// that is not a clause is compared as text.
func WhensOverlap(a, b string) bool {
	if a == b || a == "" || b == "" {
		return true
	}
	ea, errA := contextkey.Parse(a)
	eb, errB := contextkey.Parse(b)
	if errA != nil || errB != nil {
		return false
	}
	return !contextkey.Implies(ea, eb.Negate()) && !contextkey.Implies(eb, ea.Negate())
}
