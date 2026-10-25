package mergeeditor

import (
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/view/conflictActions.ts (ActionsSource: the actions
// above a conflict in the inputs and in the result), view/viewModel.ts (acceptAll, toggleActiveConflict) and
// commands/commands.ts (merge.acceptAllInput1, merge.acceptAllInput2, mergeEditor.resetResultToBaseAndAutoMerge,
// merge.toggleActiveConflictInput1, merge.toggleActiveConflictInput2).

// ActionItem is one item of the row of actions above a conflict (VS Code's code lens): a link that does something, or a
// plain text (Run is nil) that says what the result has.
type ActionItem struct {
	// Text is what the item says, and Tooltip its hover.
	Text, Tooltip string
	// Run does what the link says; nil for a text that is no link.
	Run func()
}

// ActionSeparator is the text between the items of a row (VS Code's ` | `).
const ActionSeparator = " | "

// Actions are the items of the row above a conflict in the editor of a side. conflict is the index in
// Session().Model().Conflicts(); an index that does not exist has no items.
//
// In an input (VS Code's itemsInput1 and itemsInput2): "Accept <title>" (the input's lines in the result; with the other
// input in the result already they come after its lines), "Accept Combination (<title> First)" (both inputs, this one
// first; for a conflict both inputs changed) and nothing when the result has the lines of the input or was edited by hand.
// In the result (resultItems): what the result has ("No Changes Accepted", "Incoming", "Incoming + Current", "Manual
// Resolution"), "Remove <title>" for each input it has and, for a result edited by hand, "Reset to base". VS Code's "Ignore"
// and "Append" are not built (SPEC §9.11).
func (m *Model) Actions(s Side, conflict int) []ActionItem {
	conflicts := m.sess.Model().Conflicts()
	if conflict < 0 || conflict >= len(conflicts) {
		return nil
	}
	r := conflicts[conflict]
	if in, ok := s.Input(); ok {
		return m.inputActions(r, s, in)
	}
	return m.resultActions(r)
}

func (m *Model) inputActions(r diff.Region, s Side, in diff.Input) []ActionItem {
	if r.State.Kind == diff.StateUnrecognized || r.State.IncludesInput(in) {
		return nil
	}
	title, conflict := s.Title(), r.ConflictIndex
	items := []ActionItem{{
		Text: "Accept " + title, Tooltip: "Accept " + title + " in the result document.",
		Run: func() { m.report(m.AcceptState(conflict, r.State.WithInputValue(in, true))) },
	}}
	if r.Range.IsConflicting() {
		items = append(items, ActionItem{
			Text: "Accept Combination (" + title + " First)", Tooltip: "Accept an automatic combination of both sides in the result document.",
			Run: func() { m.report(m.AcceptState(conflict, diff.BothState(in))) },
		})
	}
	return items
}

func (m *Model) resultActions(r diff.Region) []ActionItem {
	st, conflict := r.State, r.ConflictIndex
	var items []ActionItem
	switch st.Kind {
	case diff.StateUnrecognized:
		items = append(items, ActionItem{Text: "Manual Resolution", Tooltip: "This conflict has been resolved manually."})
	case diff.StateBase:
		items = append(items, ActionItem{
			Text:    "No Changes Accepted",
			Tooltip: "The current resolution of this conflict equals the common ancestor of both the right and left changes.",
		})
	default:
		var labels string
		order := []diff.Input{diff.Input1, diff.Input2}
		if st.Kind == diff.StateBoth && st.FirstInput == diff.Input2 {
			order = []diff.Input{diff.Input2, diff.Input1}
		}
		for _, in := range order {
			if st.IncludesInput(in) {
				if labels != "" {
					labels += " + "
				}
				labels += sideOf(in).Title()
			}
		}
		items = append(items, ActionItem{Text: labels})
	}
	order := []diff.Input{diff.Input1, diff.Input2}
	if st.Kind == diff.StateBoth && st.FirstInput == diff.Input2 {
		order = []diff.Input{diff.Input2, diff.Input1}
	}
	for _, in := range order {
		if !st.IncludesInput(in) {
			continue
		}
		title := sideOf(in).Title()
		items = append(items, ActionItem{
			Text: "Remove " + title, Tooltip: "Remove " + title + " from the result document.",
			Run: func() { m.report(m.AcceptState(conflict, st.WithInputValue(in, false))) },
		})
	}
	if st.Kind == diff.StateUnrecognized {
		items = append(items, ActionItem{
			Text: "Reset to base", Tooltip: "Reset this conflict to the common ancestor of both the right and left changes.",
			Run: func() { m.report(m.AcceptState(conflict, diff.RangeState{})) },
		})
	}
	return items
}

// sideOf is the side whose editor shows an input.
func sideOf(in diff.Input) Side {
	if in == diff.Input1 {
		return Incoming
	}
	return Current
}

// report tells the owner of the model about a failure of an action that a click started.
func (m *Model) report(err error) {
	if err != nil && m.opts.OnError != nil {
		m.opts.OnError(err)
	}
}

// Accept resolves a conflict with a choice and makes it the active one (the actions above a conflict, `dev.acceptMerge`):
// one undoable edit of the result. conflict is the index in Session().Model().Conflicts().
func (m *Model) Accept(conflict int, c diff.Choice) error {
	m.activate(conflict)
	return m.sess.Accept(conflict, c)
}

// AcceptState is Accept for a state that is no choice (an input first in both).
func (m *Model) AcceptState(conflict int, st diff.RangeState) error {
	m.activate(conflict)
	return m.sess.SetState(conflict, st)
}

// AcceptAll resolves every conflict that is not handled with the lines of an input, as one undoable edit (VS Code's
// merge.acceptAllInput1 and merge.acceptAllInput2).
func (m *Model) AcceptAll(in diff.Input) error {
	choice := diff.AcceptInput1
	if in == diff.Input2 {
		choice = diff.AcceptInput2
	}
	return m.sess.AcceptAll(choice)
}

// AcceptAllCombination puts the lines of both inputs in every conflict that has not got the incoming lines (VS Code's
// mergeEditor.acceptAllCombination).
func (m *Model) AcceptAllCombination() error { return m.sess.AcceptAllCombination() }

// ResetResult puts every resolved conflict back to the lines of the base, unhandled, as one undoable edit (VS Code's
// mergeEditor.resetResultToBaseAndAutoMerge, "Reset Result").
func (m *Model) ResetResult() error { return m.sess.ResetResult() }

// ToggleActive includes the lines of an input in the result of the active conflict, or takes them out (VS Code's
// merge.toggleActiveConflictInput1 and merge.toggleActiveConflictInput2). It reports false when no conflict is active,
// the notification VS Code shows is the caller's.
func (m *Model) ToggleActive(in diff.Input) (bool, error) {
	r, ok := m.Active()
	if !ok || !r.Conflict {
		return false, nil
	}
	return true, m.sess.Toggle(r.ConflictIndex, in)
}

// activate makes a conflict the active one.
func (m *Model) activate(conflict int) {
	conflicts := m.sess.Model().Conflicts()
	if conflict >= 0 && conflict < len(conflicts) {
		m.active = conflicts[conflict].Index
	}
}

// ---- the zones of the rows ----

// actionZone is the zone that holds the row of actions of a conflict in an editor.
type actionZone struct {
	conflict int
	id       editor.ZoneID
}

// addActionZones puts the zone of the row of actions above every conflict in each of the three editors. A zone is one row
// of the editor; the zones are in the same places of both inputs, so that they stay lined up. The result zones follow
// the regions (syncActionZones).
func (m *Model) addActionZones() {
	conflicts := m.sess.Model().Conflicts()
	for i, vm := range m.vms {
		side := Side(i)
		err := vm.ChangeZones(func(c editor.ZoneChanges) error {
			for _, r := range conflicts {
				id, err := c.Add(editor.ZoneSpec{
					AfterLine: max(RangeIn(r, side).StartLine-1, 0), Rows: 1, Ordinal: -1, ShowInHiddenAreas: true,
					Widget: newActionRow(m, side, r.ConflictIndex),
				})
				if err != nil {
					return err
				}
				m.actionZones[i] = append(m.actionZones[i], actionZone{conflict: r.ConflictIndex, id: id})
			}
			return nil
		})
		m.report(err)
	}
}

// syncActionZones puts the zones of the result above the conflicts as the regions are now: a zone follows the edits of the
// document by itself, which can differ from the region by a manual edit at its edge.
func (m *Model) syncActionZones() {
	vm := m.vms[Result]
	conflicts := m.sess.Model().Conflicts()
	zones := m.actionZones[Result]
	m.report(vm.ChangeZones(func(c editor.ZoneChanges) error {
		lines := vm.Document().Snapshot().LineCount()
		for _, z := range zones {
			if z.conflict >= len(conflicts) {
				continue
			}
			after := min(max(conflicts[z.conflict].Result.StartLine-1, 0), lines)
			if spec, ok := vm.Zone(z.id); ok && spec.AfterLine != after {
				if err := c.Change(z.id, func(s *editor.ZoneSpec) { s.AfterLine = after }); err != nil {
					return err
				}
			}
		}
		return nil
	}))
}

// ActionZones are the zones that hold the rows of actions in the editor of a side: the conflict each is above, and the
// line it comes after (counted from 1; 0 above the first line).
func (m *Model) ActionZones(s Side) (conflicts, afterLines []int) {
	for _, z := range m.actionZones[s] {
		spec, ok := m.vms[s].Zone(z.id)
		if !ok {
			continue
		}
		conflicts = append(conflicts, z.conflict)
		afterLines = append(afterLines, spec.AfterLine)
	}
	return conflicts, afterLines
}
