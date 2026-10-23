package editor

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// textDrag is a drag of selected text with the mouse: the selection being dragged, once the
// pointer has been over one.
type textDrag struct {
	selection *buffer.Selection
	// pressed is the selection the press was on, if it was on one only: the one dragged as
	// soon as the pointer moves, even when its first move leaves the selection behind.
	pressed *buffer.Selection
}

// DropCaret is where dropped text would go while text is being dragged; the second result is
// false when no text is dragged or the pointer is over the dragged text itself.
func (vm *ViewModel) DropCaret() (buffer.Position, bool) { return vm.dropCaret, vm.hasDropCaret }

// setDropCaret shows or hides the drop caret.
func (vm *ViewModel) setDropCaret(p buffer.Position, show bool) {
	if show == vm.hasDropCaret && (!show || p == vm.dropCaret) {
		return
	}
	vm.dropCaret, vm.hasDropCaret = p, show
	vm.sel.NotifyListeners()
}

// dragOver follows the pointer of a text drag: the first time it is over exactly one selection
// that selection is the one dragged, and the drop caret follows the pointer anywhere but over
// the dragged text.
//
// Ported from VS Code: src/vs/editor/contrib/dnd/browser/dnd.ts (DragAndDropController._onEditorMouseDrag)
func (vm *ViewModel) dragOver(d *textDrag, target buffer.Position) {
	if d.selection == nil {
		d.selection = d.pressed
	}
	if d.selection == nil {
		if found := vm.selectionsAt(target); len(found) == 1 {
			d.selection = &found[0]
		} else {
			return
		}
	}
	vm.setDropCaret(target, !d.selection.Range().ContainsPosition(target))
}

// selectionsAt are the selections that have a position inside, the ends included.
func (vm *ViewModel) selectionsAt(p buffer.Position) []buffer.Selection {
	var found []buffer.Selection
	for _, sel := range vm.cursors.Selections() {
		if sel.Range().ContainsPosition(p) {
			found = append(found, sel)
		}
	}
	return found
}

// endDrag ends a text drag without dropping.
func (vm *ViewModel) endDrag() { vm.setDropCaret(buffer.Position{}, false) }

// drop ends a text drag at a position. Without a selection having been dragged it is the
// release of a click inside a selection: the cursor goes there (Shift extends the primary
// selection to it). Otherwise the dragged text moves there, or is copied there with copy, unless
// the position is inside the text itself.
//
// Ported from VS Code: src/vs/editor/contrib/dnd/browser/dnd.ts (DragAndDropController._onEditorMouseDrop)
func (vm *ViewModel) drop(d *textDrag, target buffer.Position, shift, copy bool) error {
	vm.endDrag()
	return vm.Run(func(vm *ViewModel, _ any) error {
		target = vm.doc.Snapshot().ValidatePosition(target)
		if d.selection == nil {
			sels := vm.cursors.Selections()
			if shift {
				sels = []buffer.Selection{{Anchor: sels[0].Anchor, Active: target}}
			} else {
				for i, sel := range sels {
					if sel.Range().ContainsPosition(target) {
						sels[i] = buffer.NewCursor(target)
					}
				}
			}
			vm.setSelections(sels)
			return nil
		}
		r := d.selection.Range()
		atEdge := target == r.Start || target == r.End
		if r.ContainsPosition(target) && (!copy || !atEdge) {
			return nil
		}
		return vm.apply(vm.commands().DragAndDrop(*d.selection, target, copy))
	}, nil)
}
