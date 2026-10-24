package editor

import (
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// ApplySaveParticipants makes the edits of a save to the text of the editor's document (trim trailing whitespace,
// insert the final newline, trim the final newlines; SPEC §6.3): one undo step of their own, with the cursors of
// the editor kept in place as far as the text allows. With autoSave the white space in front of a cursor stays.
// It does nothing when no participant is on or the text needs none of them.
//
// Ported from VS Code: src/vs/workbench/contrib/codeEditor/browser/saveParticipants.ts
func (vm *ViewModel) ApplySaveParticipants(p edit.SaveParticipants, autoSave bool) error {
	if !p.Any() {
		return nil
	}
	res := vm.commands().SaveParticipants(vm.cursors.Selections(), p, autoSave)
	if len(res.Edits) == 0 {
		return nil
	}
	// the edits of a save are a step of their own: the typing before and after does not join them
	vm.doc.PushStackElement()
	if err := vm.apply(res); err != nil {
		return err
	}
	vm.doc.PushStackElement()
	return nil
}
