package editorgroup

// Dropping a dragged tab (SPEC §10.3): where on the tab strip of a group it goes, and which part of
// the editor area of a group (the middle, or a side that splits) the pointer is in. The pointer
// positions are plain numbers, so the rules are tested without a window.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorDropTarget.ts (positionOverlay,
// handleDrop) and src/vs/workbench/browser/parts/editor/multiEditorTabsControl.ts (computeDropTarget,
// getTabDragOverLocation, onDrop)

import (
	"github.com/rm4n0s/errors"
)

// DropZone is the part of the editor area of a group that a dragged editor is over.
type DropZone int

const (
	// DropNone is no zone: the editor cannot be dropped here.
	DropNone DropZone = iota
	// DropCenter moves the editor into the group.
	DropCenter
	// DropLeft splits the group on its left and moves the editor into the new group.
	DropLeft
	// DropRight splits the group on its right.
	DropRight
	// DropUp splits the group above.
	DropUp
	// DropDown splits the group below.
	DropDown
)

// String names the zone.
func (z DropZone) String() string {
	switch z {
	case DropNone:
		return "none"
	case DropCenter:
		return "center"
	case DropLeft:
		return "left"
	case DropRight:
		return "right"
	case DropUp:
		return "up"
	case DropDown:
		return "down"
	}
	return "DropZone(?)"
}

// Direction is the side a zone splits the group on; false for the center and for no zone.
func (z DropZone) Direction() (Direction, bool) {
	switch z {
	case DropLeft:
		return DirLeft, true
	case DropRight:
		return DirRight, true
	case DropUp:
		return DirUp, true
	case DropDown:
		return DirDown, true
	case DropNone, DropCenter:
	}
	return 0, false
}

// ZoneOptions say how the editor area is divided into zones.
type ZoneOptions struct {
	// Split lets the sides split the group (`workbench.editor.splitOnDragAndDrop`); without it the
	// whole area is the center.
	Split bool
	// PreferVertical gives the sides left and right the larger zones (`openSideBySideDirection` is
	// "right"); otherwise up and down have them.
	PreferVertical bool
}

// dropEdge is the share of the width and of the height, at each side, that makes the edge ring of an
// editor area: the pointer inside the ring offers a split.
const dropEdge = 0.1

// ZoneAt is the zone of the editor area of a size that the pointer at (x, y) in it is over. The
// middle of the area (all but a ring of a tenth of the width and the height) is the center. In the
// ring, the area is divided in thirds along the preferred axis (left, the middle, right) and the
// middle third in halves (up and down); with the other preference the thirds run from top to bottom.
//
// Ported from VS Code: editorDropTarget.ts (DropOverlay.positionOverlay)
func ZoneAt(x, y, width, height float64, o ZoneOptions) DropZone {
	if !o.Split {
		return DropCenter
	}
	edgeW, edgeH := width*dropEdge, height*dropEdge
	if x > edgeW && x < width-edgeW && y > edgeH && y < height-edgeH {
		return DropCenter
	}
	if o.PreferVertical {
		switch {
		case x < width/3:
			return DropLeft
		case x > width/3*2:
			return DropRight
		case y < height/2:
			return DropUp
		}
		return DropDown
	}
	switch {
	case y < height/3:
		return DropUp
	case y > height/3*2:
		return DropDown
	case x < width/2:
		return DropLeft
	}
	return DropRight
}

// ZoneRect is the part of an editor area of a size that a zone highlights: the whole area for the
// center and the half on its side for a split.
func ZoneRect(z DropZone, width, height float64) (left, top, w, h float64) {
	switch z {
	case DropUp:
		return 0, 0, width, height / 2
	case DropDown:
		return 0, height / 2, width, height / 2
	case DropLeft:
		return 0, 0, width / 2, height
	case DropRight:
		return width / 2, 0, width / 2, height
	case DropNone, DropCenter:
	}
	return 0, 0, width, height
}

// Span is the extent of a tab along the tab strip.
type Span struct {
	// Start and End are the left and right edges, in the same coordinates as the pointer.
	Start, End float64
}

// TabDropIndex is the place in a group a tab dropped with the pointer at x goes to: before the tab
// the pointer is over when it is in its left half (or on the middle), after it when it is in its
// right half; at the end when the pointer is past the last tab. The tabs are given left to right.
//
// Ported from VS Code: multiEditorTabsControl.ts (getTabDragOverLocation, the onDrop of the tab)
func TabDropIndex(tabs []Span, x float64) int {
	for i, t := range tabs {
		if x >= t.End {
			continue
		}
		if x <= t.Start+(t.End-t.Start)/2 {
			return i
		}
		return i + 1
	}
	return len(tabs)
}

// DropAllowed says whether an editor of a group can be dropped on the editor area of a group. An
// editor that moves (does not copy) onto its own group would leave it empty when it is the only
// one: that is refused.
//
// Ported from VS Code: editorDropTarget.ts (the check in onDragOver)
func DropAllowed(from, onto *Group, copied bool) bool {
	if from == nil || onto == nil {
		return false
	}
	return copied || from != onto || from.Model.Count() >= 2
}

// DropOnTabs drops an editor of a group on the tab strip of a group, at a place of the strip (a
// TabDropIndex): it moves there, or a copy of it is opened there when copied. Within a group the
// editor keeps the order of the other tabs (the place counts the tab itself). The target group
// becomes the active group.
//
// Ported from VS Code: multiEditorTabsControl.ts (onDrop)
func (g *Grid) DropOnTabs(from *Group, editor EditorInput, onto *Group, index int, copied bool) error {
	if !g.has(from) {
		return errUnknownGroup(from)
	}
	if !g.has(onto) {
		return errUnknownGroup(onto)
	}
	if !from.Model.Contains(editor) {
		return errors.New("DropOnTabsEditorMissing", "the dragged editor is not in its group", "group", from.ID())
	}
	if from == onto {
		if at := from.Model.IndexOf(editor); at < index {
			index--
		}
	}
	opts := MoveOptions{Index: index, HasIndex: true}
	var err error
	if copied {
		_, _, err = g.CopyEditor(from, editor, onto, opts)
	} else {
		_, err = g.MoveEditor(from, editor, onto, opts)
	}
	if err != nil {
		return err
	}
	return g.Activate(onto)
}

// DropOnZone drops an editor of a group on a zone of the editor area of a group: the center moves
// the editor into the group (a copy is opened there when copied), a side adds a group next to it and
// moves or copies the editor into that. When the editor is the last one of its group, the groups
// close when they empty, and it moves rather than copies, the group moves instead (with all it
// has). It gives the group the editor is in then and activates it; nil when nothing was done (no
// zone, or a drop that DropAllowed refuses, or one into the group the editor is in).
//
// Ported from VS Code: editorDropTarget.ts (DropOverlay.handleDrop)
func (g *Grid) DropOnZone(from *Group, editor EditorInput, onto *Group, zone DropZone, copied bool) (*Group, error) {
	if !g.has(from) {
		return nil, errUnknownGroup(from)
	}
	if !g.has(onto) {
		return nil, errUnknownGroup(onto)
	}
	if !from.Model.Contains(editor) {
		return nil, errors.New("DropOnZoneEditorMissing", "the dragged editor is not in its group", "group", from.ID())
	}
	if zone == DropNone || !DropAllowed(from, onto, copied) {
		return nil, nil
	}
	dir, split := zone.Direction()
	var target *Group
	if g.opts.CloseEmptyGroups && from.Model.Count() == 1 && split && !copied {
		if err := g.MoveGroup(from, onto, dir); err != nil {
			return nil, err
		}
		target = from
	} else {
		target = onto
		if split {
			nu, err := g.AddGroup(onto, dir)
			if err != nil {
				return nil, err
			}
			target = nu
		}
		if from == target {
			return nil, nil
		}
		var err error
		if copied {
			_, _, err = g.CopyEditor(from, editor, target, MoveOptions{})
		} else {
			_, err = g.MoveEditor(from, editor, target, MoveOptions{})
		}
		if err != nil {
			return nil, err
		}
	}
	if err := g.Activate(target); err != nil {
		return nil, err
	}
	return target, nil
}
