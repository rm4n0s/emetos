package edit

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// DragAndDrop moves the text of a selection to a position, or copies it there, and selects
// the text at its new place (dragging selected text with the mouse). Dropping inside the
// selection changes nothing, except that with copy a drop at one of its ends puts a copy next
// to it. The result has the selection to put the cursor on.
//
// Ported from VS Code: src/vs/editor/contrib/dnd/browser/dragAndDropCommand.ts
func (c Commands) DragAndDrop(sel buffer.Selection, target buffer.Position, copy bool) Result {
	r := c.validRange(sel)
	target = c.Model.ValidatePosition(target)
	atEdge := target == r.Start || target == r.End
	if r.ContainsPosition(target) && (!copy || !atEdge) {
		return Result{Selections: []buffer.Selection{sel}}
	}

	text := c.textIn(r)
	insert := document.Edit{Range: buffer.Range{Start: target, End: target}, Text: text}
	var edits []document.Edit
	switch {
	case copy:
		edits = []document.Edit{insert}
	case target.IsBefore(r.Start):
		edits = []document.Edit{insert, {Range: r}}
	default:
		edits = []document.Edit{{Range: r}, insert}
	}

	sl, sc, el, ec := r.Start.Line, r.Start.Col, r.End.Line, r.End.Col
	tl, tc := target.Line, target.Col
	oneLine := sl == el
	// endCol is the column where the dropped text ends when it starts at column tc.
	endCol := func(tc int) int {
		if oneLine {
			return tc + ec - sc
		}
		return ec
	}
	var to buffer.Selection
	switch {
	case copy:
		to = buffer.Selection{Anchor: posAt(tl, tc), Active: posAt(el-sl+tl, endCol(tc))}
	case tl > el: // dragged down
		to = buffer.Selection{Anchor: posAt(tl-el+sl, tc), Active: posAt(tl, endCol(tc))}
	case tl < el: // dragged up
		to = buffer.Selection{Anchor: posAt(tl, tc), Active: posAt(tl+el-sl, endCol(tc))}
	case ec <= tc: // on the line the selection ends on, after it
		end := ec
		if oneLine {
			end = tc
		}
		to = buffer.Selection{Anchor: posAt(sl, tc-ec+sc), Active: posAt(tl, end)}
	default: // before a selection that is on one line
		to = buffer.Selection{Anchor: posAt(tl, tc), Active: posAt(tl, tc+ec-sc)}
	}
	return Result{Edits: edits, Selections: []buffer.Selection{to}, Kind: document.KindOther}
}
