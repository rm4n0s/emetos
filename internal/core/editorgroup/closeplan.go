package editorgroup

// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorGroupView.ts
// (doHandleCloseConfirmation: an editor that is still open in another group is not asked about)

// CloseTarget is a set of editors of one group that are about to close.
type CloseTarget struct {
	// Group is the group the editors are in.
	Group *Group
	// Editors are the inputs of the group that close.
	Editors []EditorInput
}

// Unsaved is a dirty editor whose changes would be lost by a close.
type Unsaved struct {
	// Group is the group of the first of the editors that show the changes.
	Group *Group
	// Input is that editor.
	Input EditorInput
}

// CloseUnasked is implemented by an input whose dirty mark is only a mark: closing it never asks, because what it holds is
// kept some other way (the state an extension tab saves with the window). The editors of extension tabs are like that.
type CloseUnasked interface {
	CloseUnasked() bool
}

// PlanClose lists what a close would lose: the editors among the targets that have unsaved changes and
// whose changes no editor that stays open shows. A document that is open in two editors is lost when the
// last of them closes, so it is listed once, for the first editor of the targets (in the order of the
// targets) and not at all while an editor of it stays in any group of the grid. An editor that is not in
// its group any more is left out. Nothing is changed.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorGroupView.ts (doHandleCloseConfirmation)
func PlanClose(grid *Grid, targets []CloseTarget) []Unsaved {
	closing := map[EditorInput]bool{}
	for _, t := range targets {
		for _, e := range t.Editors {
			if t.Group != nil && t.Group.Model.ContainsStrict(e) {
				closing[e] = true
			}
		}
	}
	var staying []EditorInput
	for _, g := range grid.Groups(GridAppearance) {
		for _, e := range g.Model.Editors(Sequential, false) {
			if !closing[e] {
				staying = append(staying, e)
			}
		}
	}
	var out []Unsaved
	for _, t := range targets {
		for _, e := range t.Editors {
			if !closing[e] || !e.Dirty() || unasked(e) || shownBy(staying, e) {
				continue
			}
			listed := false
			for _, u := range out {
				if sameThing(u.Input, e) {
					listed = true
					break
				}
			}
			if !listed {
				out = append(out, Unsaved{Group: t.Group, Input: e})
			}
		}
	}
	return out
}

// unasked tells whether closing the editor never asks about its dirty mark.
func unasked(e EditorInput) bool {
	u, ok := e.(CloseUnasked)
	return ok && u.CloseUnasked()
}

// shownBy tells whether an editor of the list shows the same thing as e.
func shownBy(editors []EditorInput, e EditorInput) bool {
	for _, o := range editors {
		if sameThing(o, e) {
			return true
		}
	}
	return false
}

// sameThing tells whether two inputs show the same thing as far as unsaved changes go: they are the same editor,
// or they edit the same document (a diff editor whose modified side is a file that a text editor shows too).
func sameThing(a, b EditorInput) bool {
	if a.Matches(b) {
		return true
	}
	da := EditedDocument(a)
	return da != nil && da == EditedDocument(b)
}

// AllTargets makes the targets that close every editor of every group of the grid, in the order of the
// groups on screen. Sticky editors are included.
func AllTargets(grid *Grid) []CloseTarget {
	var out []CloseTarget
	for _, g := range grid.Groups(GridAppearance) {
		if editors := g.Model.Editors(Sequential, false); len(editors) > 0 {
			out = append(out, CloseTarget{Group: g, Editors: editors})
		}
	}
	return out
}
