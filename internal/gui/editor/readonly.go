package editor

import "github.com/rm4n0s/errors"

// ReadOnly tells whether the editor refuses edits: its option ReadOnly is set (the original side of the diff
// editor), or its document is read-only. Commands that only read still work.
func (vm *ViewModel) ReadOnly() bool { return vm.opts.ReadOnly || vm.doc.ReadOnly() }

// checkEditable fails with the tag EditorReadOnly when the option ReadOnly forbids edits. A read-only document
// refuses them itself (DocumentReadOnly).
func (vm *ViewModel) checkEditable() error {
	if vm.opts.ReadOnly {
		return errors.New("EditorReadOnly", "the editor is read-only", "uri", vm.doc.URI().String())
	}
	return nil
}
