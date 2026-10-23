package editor

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// MouseModifiers are the keys held while the mouse button goes down.
type MouseModifiers struct {
	Shift, Ctrl, Alt, Meta bool
}

// MouseEvent is a press or a drag of the primary mouse button on the text or on the line
// numbers: where it is and what the user asked for with it (SPEC §9.3).
type MouseEvent struct {
	// Position is the position of the document under the pointer.
	Position buffer.Position
	// VisualColumn is the column under the pointer counted in characters of the font, also
	// past the end of the line; a column selection reaches up to it.
	VisualColumn int
	// Clicks counts the presses in a row at the same place: 1 for a click, 2 for a double
	// click, 3 for a triple click, 4 or more select everything.
	Clicks int
	// Mods are the modifiers held when the button went down.
	Mods MouseModifiers
	// OnLineNumbers says the press started on the line numbers: the selection goes by lines.
	OnLineNumbers bool
	// Drag says the pointer moved with the button down, after the press the event came from.
	Drag bool
}

// hasMultiCursorModifier reports whether the key of editor.multiCursorModifier is held.
//
// Ported from VS Code: src/vs/editor/browser/view/viewController.ts (_hasMulticursorModifier)
func (vm *ViewModel) hasMultiCursorModifier(m MouseModifiers) bool {
	if vm.opts.MultiCursorModifier == "ctrlCmd" {
		return m.Ctrl
	}
	return m.Alt
}

// hasOtherModifier reports whether a modifier that is not the multi-cursor one is held (Shift
// does not count).
//
// Ported from VS Code: src/vs/editor/browser/view/viewController.ts (_hasNonMulticursorModifier)
func (vm *ViewModel) hasOtherModifier(m MouseModifiers) bool {
	if vm.opts.MultiCursorModifier == "ctrlCmd" {
		return m.Alt || m.Meta
	}
	return m.Ctrl || m.Meta
}

// DispatchMouse does what a press or a drag of the mouse button means for the cursors: it moves
// them, selects by word or line, adds a cursor or selects a column. It runs like a command, so
// the sessions of the column selection go on while the mouse drags.
//
// Ported from VS Code: src/vs/editor/browser/view/viewController.ts (ViewController.dispatchMouse)
func (vm *ViewModel) DispatchMouse(ev MouseEvent) error {
	return vm.Run(func(vm *ViewModel, _ any) error {
		vm.dispatchMouse(ev)
		return nil
	}, nil)
}

func (vm *ViewModel) dispatchMouse(ev MouseEvent) {
	if !ev.Drag {
		vm.history.Record(vm.cursors.Selections())
	}
	inSelection := ev.Drag || ev.Mods.Shift
	if inSelection && !ev.Drag && (ev.OnLineNumbers || ev.Clicks == 2 || ev.Clicks == 3) {
		// Shift extends the selection there is, by words or lines, from its anchor.
		anchor := vm.cursors.Primary().Selection.Anchor
		vm.mouseRange = buffer.Range{Start: anchor, End: anchor}
	}
	multi := vm.hasMultiCursorModifier(ev.Mods)
	mv := vm.mover()
	pos := ev.Position
	switch {
	case ev.OnLineNumbers:
		switch {
		case multi && inSelection:
			vm.lastCursorLineSelect(mv, pos, true)
		case multi:
			vm.createCursor(mv, pos, true)
		case inSelection:
			vm.lineSelect(mv, pos, true)
		default:
			vm.lineSelect(mv, pos, false)
		}
	case ev.Clicks >= 4:
		vm.setSelections([]buffer.Selection{{Anchor: buffer.Position{}, Active: vm.docEnd()}})
	case ev.Clicks == 3:
		switch {
		case multi:
			vm.lastCursorLineSelect(mv, pos, inSelection)
		default:
			vm.lineSelect(mv, pos, inSelection)
		}
	case ev.Clicks == 2:
		switch {
		case multi:
			vm.lastCursorWordSelect(mv, pos, ev.Drag)
		default:
			vm.wordSelect(mv, pos, inSelection)
		}
	case multi:
		switch {
		case vm.hasOtherModifier(ev.Mods):
			// Only the multi-cursor modifier alone makes cursors.
		case ev.Mods.Shift:
			vm.columnSelect(mv, pos, ev.VisualColumn)
		case ev.Drag:
			i := vm.cursors.LastAdded()
			vm.cursors.Replace(i, mv.MoveTo(vm.cursors.All()[i], pos, true))
		default:
			vm.createCursor(mv, pos, false)
		}
	case inSelection:
		if ev.Mods.Alt || vm.opts.ColumnSelection {
			vm.columnSelect(mv, pos, ev.VisualColumn)
		} else {
			vm.cursors.Set([]edit.Cursor{mv.MoveTo(vm.cursors.Primary(), pos, true)})
		}
	default:
		vm.cursors.Set([]edit.Cursor{mv.MoveTo(vm.cursors.Primary(), pos, false)})
	}
}

// docEnd is the position at the end of the text.
func (vm *ViewModel) docEnd() buffer.Position {
	snap := vm.doc.Snapshot()
	last := snap.LineCount() - 1
	text, _ := snap.LineContent(last)
	return buffer.Position{Line: last, Col: len(text)}
}

// selectionOf is the selection that goes with the range a mouse selection extends from.
func cursorOfRange(r buffer.Range) edit.Cursor {
	return edit.CursorOf(buffer.Selection{Anchor: r.Start, Active: r.End})
}

// wordSelect enters the word under a position, or goes on selecting by words when dragging.
func (vm *ViewModel) wordSelect(mv edit.Mover, pos buffer.Position, dragging bool) {
	if dragging {
		vm.cursors.Set([]edit.Cursor{edit.CursorOf(mv.WordSelectDrag(vm.mouseRange, pos))})
		return
	}
	vm.mouseRange = mv.WordSelect(pos)
	vm.cursors.Set([]edit.Cursor{cursorOfRange(vm.mouseRange)})
}

// lastCursorWordSelect is wordSelect for the cursor added last, which keeps the other ones.
func (vm *ViewModel) lastCursorWordSelect(mv edit.Mover, pos buffer.Position, dragging bool) {
	i := vm.cursors.LastAdded()
	if dragging {
		vm.cursors.Replace(i, edit.CursorOf(mv.WordSelectDrag(vm.mouseRange, pos)))
		return
	}
	vm.mouseRange = mv.WordSelect(pos)
	vm.cursors.Replace(i, cursorOfRange(vm.mouseRange))
}

// lineSelect enters the line of a position, or goes on selecting by lines when dragging.
func (vm *ViewModel) lineSelect(mv edit.Mover, pos buffer.Position, dragging bool) {
	if dragging {
		vm.cursors.Set([]edit.Cursor{edit.CursorOf(mv.LineSelectDrag(vm.mouseRange, pos))})
		return
	}
	vm.mouseRange = mv.LineSelect(pos.Line)
	vm.cursors.Set([]edit.Cursor{cursorOfRange(vm.mouseRange)})
}

// lastCursorLineSelect is lineSelect for the cursor added last, which keeps the other ones.
func (vm *ViewModel) lastCursorLineSelect(mv edit.Mover, pos buffer.Position, dragging bool) {
	i := vm.cursors.LastAdded()
	if dragging {
		vm.cursors.Replace(i, edit.CursorOf(mv.LineSelectDrag(vm.mouseRange, pos)))
		return
	}
	vm.mouseRange = mv.LineSelect(pos.Line)
	vm.cursors.Replace(i, cursorOfRange(vm.mouseRange))
}

// createCursor adds a cursor at a position (or a selection of its line), or takes the cursor
// away that has the position inside its selection, like a toggle.
//
// Ported from VS Code: src/vs/editor/browser/coreCommands.ts (CreateCursor)
func (vm *ViewModel) createCursor(mv edit.Mover, pos buffer.Position, wholeLine bool) {
	made := mv.MoveTo(vm.cursors.Primary(), pos, false)
	if wholeLine {
		vm.mouseRange = mv.LineSelect(pos.Line)
		made = cursorOfRange(vm.mouseRange)
	}
	all := vm.cursors.All()
	if len(all) > 1 {
		for i, c := range all {
			if c.Selection.Range().ContainsPosition(made.Position()) {
				vm.cursors.Set(append(all[:i:i], all[i+1:]...))
				return
			}
		}
	}
	vm.cursors.Add(made)
}

// columnSelect selects the rectangle from where the column selection started, or the
// anchor of the primary selection, to the position under the pointer.
//
// Ported from VS Code: src/vs/editor/browser/coreCommands.ts (ColumnSelect)
func (vm *ViewModel) columnSelect(mv edit.Mover, pos buffer.Position, visual int) {
	vm.sessionOwner = sessionColumns
	if vm.columns == nil {
		d := mv.ColumnSelectDataOf(vm.cursors.Primary())
		vm.columns = &d
	}
	res := mv.ColumnSelect(vm.columns.FromLine, vm.columns.FromVisual, pos.Line, visual)
	vm.cursors.Set(res.Cursors)
	vm.columns = &res.Data
}
