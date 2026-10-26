package editor

import (
	"context"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// Rename (SPEC §9.12 "Rename", "As built (step 12.8)"): F2 asks the providers where the symbol at the cursor is and what to put
// in the box (prepareRename), a box over the symbol takes the new name, and Enter asks for the edit and makes it, or, with the
// key of the preview, shows the changes first. A refusal of the providers is shown as a message at the cursor.
//
// Ported from VS Code: src/vs/editor/contrib/rename/browser/rename.ts and renameWidget.ts

// RenameBox is the box over the symbol that is renamed.
type RenameBox struct {
	// Range is the symbol and Text its name; the box starts with the name selected.
	Range buffer.Range
	Text  string
	// Input is what the box holds now.
	Input string
	// Preview says that the key of the preview is offered (editor.rename.enablePreview): the box tells so under the name.
	Preview bool
	// Revision counts the times the box was opened, so that the widget selects the name once.
	Revision int

	pos     buffer.Position
	version int
}

// RenameUI is what the widget gives the view model to move the focus into the box.
type RenameUI struct {
	// Focus gives the focus to the input of the box and selects its text; Return gives it back to the editor.
	Focus, Return func()
}

// renameState is what a view model keeps for its rename.
type renameState struct {
	box      *RenameBox
	gen      int
	cancel   context.CancelFunc
	unsubDoc func()
	revision int
	changed  *foundation.ChangeNotifier
	ui       RenameUI
}

func newRenameState() renameState { return renameState{changed: foundation.NewChangeNotifier()} }

// AddRenameListener calls fn when the rename box shows, changes or goes.
func (vm *ViewModel) AddRenameListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return vm.rn.changed.AddListener(fn)
}

// RemoveRenameListener removes a listener of AddRenameListener.
func (vm *ViewModel) RemoveRenameListener(h foundation.ListenerHandle) {
	vm.rn.changed.RemoveListener(h)
}

// SetRenameUI gives the view model the widget's means to move the focus.
func (vm *ViewModel) SetRenameUI(ui RenameUI) { vm.rn.ui = ui }

// RenameBox is the box that shows now, or nil.
func (vm *ViewModel) RenameBox() *RenameBox { return vm.rn.box }

// RenameVisible tells whether the box shows (the key renameInputVisible).
func (vm *ViewModel) RenameVisible() bool { return vm.rn.box != nil }

// HasRenameProvider tells whether a rename provider matches the document (the key editorHasRenameProvider).
func (vm *ViewModel) HasRenameProvider() bool {
	f := vm.hoverFeatures()
	return f != nil && f.Has(vm.langDocument(), langfeatures.FeatureRename)
}

func (vm *ViewModel) renameChanged() {
	vm.rn.changed.NotifyListeners()
	vm.notifyKeys()
}

// refusalText is the message of a provider that refused to rename: VS Code's text for a position that has nothing to
// rename, else the reason the provider gave.
func refusalText(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		if appErr.Tag == "NotRenameable" {
			return "The element can't be renamed."
		}
		if appErr.Message != "" {
			return appErr.Message
		}
	}
	return err.Error()
}

// RenameStart starts a rename at the cursor (editor.action.rename): the providers are asked, off the UI thread, for the symbol
// there. It says whether a request was made.
//
// Ported from VS Code: rename.ts (RenameController.run)
func (vm *ViewModel) RenameStart() bool {
	h := vm.sg.host
	if h == nil || h.Features == nil || vm.ReadOnly() || !vm.HasRenameProvider() {
		return false
	}
	st := &vm.rn
	vm.RenameCancel()
	vm.CodeActionHide()
	st.gen++
	gen := st.gen
	ctx, cancel := context.WithCancel(vm.hostCtx())
	st.cancel = cancel
	doc, pos := vm.langDocument(), vm.cursors.Primary().Position()
	reg := h.Features
	go func() {
		prep, err := reg.PrepareRename(ctx, doc, pos)
		vm.post(func() { vm.renamePrepared(gen, doc, pos, prep, err) })
	}()
	return true
}

// renamePrepared takes the answer of prepareRename; it is dropped when the cursor or the text moved meanwhile.
func (vm *ViewModel) renamePrepared(gen int, doc langfeatures.Document, pos buffer.Position, prep *langfeatures.PrepareRename, err error) {
	st := &vm.rn
	if gen != st.gen {
		return
	}
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
	if vm.doc.Version() != doc.Version || vm.cursors.Primary().Position() != pos {
		return
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			vm.ShowMessage(refusalText(err))
		}
		return
	}
	snap := vm.doc.Snapshot()
	var rng buffer.Range
	var text string
	if prep != nil && !prep.DefaultBehavior {
		rng = snap.ValidateRange(prep.Range)
		text = prep.Placeholder
		if text == "" {
			text = snap.Text(rng)
		}
	} else {
		line, _ := snap.LineContent(pos.Line)
		s, e, ok := vm.wordFunc()(line, min(pos.Col, len(line)))
		if !ok {
			vm.ShowMessage("No result.")
			return
		}
		rng = buffer.Range{Start: buffer.Position{Line: pos.Line, Col: s}, End: buffer.Position{Line: pos.Line, Col: e}}
		text = line[s:e]
	}
	vm.HideHover()
	st.revision++
	st.box = &RenameBox{Range: rng, Text: text, Input: text, Preview: vm.opts.RenamePreview, Revision: st.revision, pos: pos, version: vm.doc.Version()}
	if st.unsubDoc == nil {
		st.unsubDoc = vm.doc.Subscribe(func(document.ChangeEvent) { vm.RenameCancel() })
	}
	vm.renameChanged()
	if st.ui.Focus != nil {
		st.ui.Focus()
	}
}

// renameCursorsChanged closes the box when the cursor moves away from the symbol.
func (vm *ViewModel) renameCursorsChanged() {
	if b := vm.rn.box; b != nil && vm.cursors.Primary().Position() != b.pos {
		vm.RenameCancel()
	}
}

// RenameSetInput tells the box what the user typed.
func (vm *ViewModel) RenameSetInput(text string) {
	if b := vm.rn.box; b != nil && b.Input != text {
		b.Input = text
		vm.rn.changed.NotifyListeners()
	}
}

// RenameCancel closes the box, or ends the request for the symbol, and gives the focus back to the editor.
func (vm *ViewModel) RenameCancel() {
	st := &vm.rn
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
	st.gen++
	if st.unsubDoc != nil {
		st.unsubDoc()
		st.unsubDoc = nil
	}
	if st.box == nil {
		return
	}
	st.box = nil
	vm.renameChanged()
	if st.ui.Return != nil {
		st.ui.Return()
	}
}

// RenameAccept takes the name in the box: the providers are asked for the edit, which is made at once, or shown first when
// preview is set. A name that is empty or the old one closes the box and renames nothing.
//
// Ported from VS Code: renameWidget.ts (acceptInput), rename.ts (RenameController.run)
func (vm *ViewModel) RenameAccept(preview bool) {
	st := &vm.rn
	b := st.box
	h := vm.sg.host
	if b == nil || h == nil || h.Features == nil {
		return
	}
	name := b.Input
	if name == "" || name == b.Text {
		vm.RenameCancel()
		return
	}
	preview = preview && vm.opts.RenamePreview
	vm.RenameCancel()
	gen := st.gen
	ctx, cancel := context.WithCancel(vm.hostCtx())
	st.cancel = cancel
	doc, pos, old := vm.langDocument(), b.pos, b.Text
	reg := h.Features
	go func() {
		we, err := reg.Rename(ctx, doc, pos, name)
		vm.post(func() { vm.renameEdit(gen, doc.Version, old, name, preview, we, err) })
	}()
}

// renameEdit takes the edit of a rename and makes it.
func (vm *ViewModel) renameEdit(gen, version int, old, name string, preview bool, we *langfeatures.WorkspaceEdit, err error) {
	st := &vm.rn
	if gen != st.gen {
		return
	}
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
	h := vm.sg.host
	switch {
	case err != nil:
		if errors.Is(err, context.Canceled) {
			return
		}
		if h != nil && h.Log != nil {
			h.Log("rename failed", err)
		}
		vm.ShowMessage(refusalText(err))
		return
	case we == nil:
		vm.ShowMessage("No result.")
		return
	case vm.doc.Version() != version:
		return // the text changed since the request: the edit is for the old one
	}
	we.Label = "Renaming '" + old + "' to '" + name + "'"
	if preview {
		vm.PreviewEdit(we.Label, we, nil)
		return
	}
	if h == nil || h.ApplyEdit == nil {
		return
	}
	// the cursor stays where it is, without a selection
	vm.cursors.SetSelections([]buffer.Selection{buffer.NewCursor(vm.cursors.Primary().Position())})
	if err := h.ApplyEdit(vm.hostCtx(), we); err != nil {
		vm.reportError(err)
	}
}

// renameActions are the commands of rename and of its box.
//
// Ported from VS Code: rename.ts
func renameActions(a map[string]action) {
	only := func(f func(vm *ViewModel)) action {
		return func(vm *ViewModel, _ any) error {
			vm.keepSessions, vm.skipReveal = true, true
			f(vm)
			return nil
		}
	}
	a["editor.action.rename"] = only(func(vm *ViewModel) { vm.RenameStart() })
	a["acceptRenameInput"] = only(func(vm *ViewModel) { vm.RenameAccept(false) })
	a["acceptRenameInputWithPreview"] = only(func(vm *ViewModel) { vm.RenameAccept(true) })
	a["cancelRenameInput"] = only(func(vm *ViewModel) { vm.RenameCancel() })
}
