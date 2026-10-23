package editorgroup

import (
	"github.com/rm4n0s/errors"
)

// MoveOptions say how an editor moves or is copied to a group (VS Code's IEditorOpenOptions).
type MoveOptions struct {
	// Index is the place of the editor in the target group, when HasIndex is set.
	Index    int
	HasIndex bool
	// Inactive keeps the editor from becoming the active editor of the target group.
	Inactive bool
	// Sticky makes the editor sticky in the target group. A move keeps the sticky state the
	// editor had; a copy never does.
	Sticky bool
}

// MoveEditor moves an editor of a group to another group. The very same input is opened in the
// target (pinned, and sticky when it was) and then closed in the source with CloseMove; when the
// target holds a matching editor already, that one is used and the input is only closed in the
// source. Within one group it moves the editor to Index.
//
// Ported from VS Code: EditorGroupView.moveEditor
func (g *Grid) MoveEditor(from *Group, editor EditorInput, to *Group, opts MoveOptions) (bool, error) {
	if !g.has(from) {
		return false, errUnknownGroup(from)
	}
	if !g.has(to) {
		return false, errUnknownGroup(to)
	}
	if !from.Model.Contains(editor) {
		return false, nil
	}
	if from == to {
		g.moveInside(from, editor, opts)
		return true, nil
	}
	sticky := opts.Sticky || from.Model.IsSticky(editor)
	to.Model.OpenEditor(editor, openOptionsFor(opts, sticky))
	from.Model.CloseEditor(editor, CloseMove, true)
	return true, nil
}

// CopyEditor shows an editor of a group in another group as well: a copy of its input is opened
// in the target (pinned). It gives the editor of the target, and false when the input cannot be
// copied (it does not implement Copier). Within one group it moves the editor to Index.
//
// Ported from VS Code: EditorGroupView.copyEditor
func (g *Grid) CopyEditor(from *Group, editor EditorInput, to *Group, opts MoveOptions) (EditorInput, bool, error) {
	if !g.has(from) {
		return nil, false, errUnknownGroup(from)
	}
	if !g.has(to) {
		return nil, false, errUnknownGroup(to)
	}
	if !from.Model.Contains(editor) {
		return nil, false, nil
	}
	if from == to {
		g.moveInside(from, editor, opts)
		return editor, true, nil
	}
	copier, ok := editor.(Copier)
	if !ok {
		return nil, false, nil
	}
	dup := copier.CopyInput()
	shown, isNew := to.Model.OpenEditor(dup, openOptionsFor(opts, opts.Sticky))
	if !isNew {
		dup.Dispose() // the target had a matching editor already
	}
	return shown, true, nil
}

func openOptionsFor(opts MoveOptions, sticky bool) OpenOptions {
	return OpenOptions{Pinned: true, Sticky: sticky, Active: !opts.Inactive, Index: opts.Index, HasIndex: opts.HasIndex}
}

// moveInside moves an editor within its group (nothing without an index) and pins it.
func (g *Grid) moveInside(grp *Group, editor EditorInput, opts MoveOptions) {
	if !opts.HasIndex {
		return
	}
	current := grp.Model.IndexOf(editor)
	existing := grp.Model.EditorAt(current)
	if existing == nil {
		return
	}
	if current != opts.Index {
		grp.Model.MoveEditor(existing, opts.Index)
		grp.Model.Pin(existing)
	}
	if opts.Sticky {
		grp.Model.Stick(existing)
	}
}

// MergeGroup moves all the editors of a group to the end of another group, and removes the group
// when it is empty then. An editor stays inactive in the target unless it is the active editor of
// the active group. It tells whether the group was removed.
//
// Ported from VS Code: EditorPart.mergeGroup
func (g *Grid) MergeGroup(grp, target *Group) (bool, error) {
	if !g.has(grp) {
		return false, errUnknownGroup(grp)
	}
	if !g.has(target) {
		return false, errUnknownGroup(target)
	}
	if grp == target {
		return false, errors.New("GroupMergeOntoItself", "a group cannot be merged into itself", "group", grp.ID())
	}
	index := target.Model.Count()
	for _, editor := range grp.Model.Editors(Sequential, false) {
		inactive := !grp.Model.IsActive(editor) || g.active != grp
		opts := MoveOptions{Inactive: inactive}
		// an editor that is sticky in the target keeps its place, or it could lose that state
		if !target.Model.Contains(editor) || !target.Model.IsSticky(editor) {
			opts.Index, opts.HasIndex = index, true
			index++
		}
		if _, err := g.MoveEditor(grp, editor, target, opts); err != nil {
			return false, err
		}
	}
	if g.has(grp) && grp.Empty() && len(g.groups) > 1 {
		g.removeEmpty(grp)
		return true, nil
	}
	return !g.has(grp), nil
}

// MergeAllGroups merges every other group into a group (workbench.action.joinAllGroups).
//
// Ported from VS Code: EditorPart.mergeAllGroups
func (g *Grid) MergeAllGroups(target *Group) error {
	if !g.has(target) {
		return errUnknownGroup(target)
	}
	for _, grp := range g.Groups(MostRecentlyActiveGroups) {
		if grp == target || !g.has(grp) {
			continue
		}
		if _, err := g.MergeGroup(grp, target); err != nil {
			return err
		}
	}
	return nil
}

// SplitGroup adds a group next to a group on the side dir, copies the active editor of the group
// into it and activates it (the commands workbench.action.splitEditorRight and its siblings). An
// editor that cannot be copied is not shown in the new group.
//
// Ported from VS Code: editorCommands.ts (splitEditor)
func (g *Grid) SplitGroup(grp *Group, dir Direction) (*Group, error) {
	nu, err := g.AddGroup(grp, dir)
	if err != nil {
		return nil, err
	}
	if active := grp.Model.ActiveEditor(); active != nil {
		if _, _, err := g.CopyEditor(grp, active, nu, MoveOptions{}); err != nil {
			return nil, err
		}
	}
	if err := g.Activate(nu); err != nil {
		return nil, err
	}
	return nu, nil
}

// Target says where editors go for MoveOrCopyTarget: the "to" of VS Code's moveActiveEditor and
// copyActiveEditor.
type Target string

// The values of Target.
const (
	TargetLeft     Target = "left"
	TargetRight    Target = "right"
	TargetUp       Target = "up"
	TargetDown     Target = "down"
	TargetFirst    Target = "first"
	TargetLast     Target = "last"
	TargetPrevious Target = "previous"
	TargetNext     Target = "next"
	TargetCenter   Target = "center"
	TargetPosition Target = "position"
)

// MoveOrCopyTarget finds the group that editors of a group move or are copied to. Left, right,
// up and down find the neighbor on that side, or add a group there; previous and next go by the
// order of the grid, and add a group (on the opposite of the preferred side, or on it) when the
// source is the first or the last group; center and position (value counts from 1) pick a group
// by its place. It gives nil when there is no such group.
//
// Ported from VS Code: editorCommands.ts (moveCopyEditorsToGroup)
func (g *Grid) MoveOrCopyTarget(source *Group, to Target, value int, preferred Direction) (*Group, error) {
	if !g.has(source) {
		return nil, errUnknownGroup(source)
	}
	side := func(dir Direction) (*Group, error) {
		if found := g.FindByDirection(source, dir, false); found != nil {
			return found, nil
		}
		return g.AddGroup(source, dir)
	}
	switch to {
	case TargetLeft:
		return side(DirLeft)
	case TargetRight:
		return side(DirRight)
	case TargetUp:
		return side(DirUp)
	case TargetDown:
		return side(DirDown)
	case TargetFirst:
		return g.FindByLocation(source, LocationFirst, false), nil
	case TargetLast:
		return g.FindByLocation(source, LocationLast, false), nil
	case TargetPrevious:
		if found := g.FindByLocation(source, LocationPrevious, false); found != nil {
			return found, nil
		}
		if preferred == DirRight {
			return g.AddGroup(source, DirLeft)
		}
		return g.AddGroup(source, DirUp)
	case TargetNext:
		if found := g.FindByLocation(source, LocationNext, false); found != nil {
			return found, nil
		}
		return g.AddGroup(source, preferred)
	case TargetCenter:
		groups := g.Groups(GridAppearance)
		if i := len(groups)/2 - 1; i >= 0 {
			return groups[i], nil
		}
		return nil, nil
	case TargetPosition:
		groups := g.Groups(GridAppearance)
		if i := value - 1; i >= 0 && i < len(groups) {
			return groups[i], nil
		}
		return nil, nil
	}
	return nil, errors.New("GroupTargetInvalid", "the target of a move or copy of editors is not known", "to", string(to))
}

// FocusIndex finds the group that the key Ctrl+n focuses: the group at the place n (counted from
// 1) in the order of the grid. The one after the last group is added, next to the last group on
// the preferred side; a place further away finds nothing.
//
// Ported from VS Code: editor.contribution.ts (the focus group by index commands)
func (g *Grid) FocusIndex(n int, preferred Direction) (*Group, error) {
	groups := g.Groups(GridAppearance)
	switch {
	case n < 1 || n > len(groups)+1:
		return nil, nil
	case n <= len(groups):
		return groups[n-1], nil
	}
	return g.AddGroup(groups[len(groups)-1], preferred)
}
