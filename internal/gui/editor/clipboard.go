package editor

import (
	"context"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// Clipboard is where an editor copies to and pastes from: the system clipboard and the primary
// selection of the desktop. Every function calls done later, on the UI thread, and never before
// it has returned (Glutter's clipboard works so).
type Clipboard interface {
	// Write puts a text on the clipboard.
	Write(text string, done func(error))
	// Read gets the text on the clipboard; it is empty when there is none.
	Read(done func(text string, err error))
	// WritePrimary puts a text on the primary selection.
	WritePrimary(text string, done func(error))
	// ReadPrimary gets the text of the primary selection.
	ReadPrimary(done func(text string, err error))
}

// glutterClipboard is the Clipboard of a Glutter binding.
type glutterClipboard struct {
	ctx context.Context
	c   *services.Clipboard
}

// NewClipboard makes the Clipboard of Glutter's clipboard service; ctx is the context of the
// requests to the desktop.
func NewClipboard(ctx context.Context, c *services.Clipboard) Clipboard {
	return glutterClipboard{ctx: ctx, c: c}
}

func readText(done func(string, error)) func(*services.ClipboardData, error) {
	return func(data *services.ClipboardData, err error) {
		if err != nil {
			done("", err)
			return
		}
		if data == nil {
			done("", nil)
			return
		}
		done(data.Text, nil)
	}
}

// Write implements Clipboard.
func (g glutterClipboard) Write(text string, done func(error)) {
	g.c.SetData(g.ctx, services.NewClipboardData(text), done)
}

// Read implements Clipboard.
func (g glutterClipboard) Read(done func(string, error)) {
	g.c.GetData(g.ctx, services.ClipboardKTextPlain, readText(done))
}

// WritePrimary implements Clipboard.
func (g glutterClipboard) WritePrimary(text string, done func(error)) {
	g.c.SetPrimaryData(g.ctx, services.NewClipboardData(text), done)
}

// ReadPrimary implements Clipboard.
func (g glutterClipboard) ReadPrimary(done func(string, error)) {
	g.c.GetPrimaryData(g.ctx, services.ClipboardKTextPlain, readText(done))
}

// ClipboardMemory remembers how the last text that an editor copied was made, so that pasting
// it back knows more than the text says: it was a whole line, it came from several cursors,
// it was a column. When something else has been copied since, the text no longer matches and
// the memory is forgotten. Editors of a window share one, so that a copy in one can be pasted
// in another.
//
// Ported from VS Code: src/vs/editor/browser/controller/editContext/clipboardUtils.ts (InMemoryClipboardMetadataManager)
type ClipboardMemory struct {
	last *copied
}

// copied is what an editor copied: the text and what it knows of it.
type copied struct {
	text               string
	fromEmptySelection bool
	pieces             []string
	block              bool
}

// NewClipboardMemory makes an empty memory.
func NewClipboardMemory() *ClipboardMemory { return &ClipboardMemory{} }

// recall returns what was known of a pasted text, or nil when it is not what was copied last.
func (m *ClipboardMemory) recall(text string) *copied {
	if m.last != nil && m.last.text == text {
		return m.last
	}
	m.last = nil
	return nil
}

// clipboardLink is what an editor needs to reach the clipboard: the clipboard, the memory of the
// copies, and where to report what fails after the command that asked has returned.
type clipboardLink struct {
	clipboard Clipboard
	memory    *ClipboardMemory
	report    func(error)
}

// SetClipboard gives the view model the clipboard of its window, the memory of the copies
// shared by the editors of the window (nil for one of its own) and where the errors of the
// requests that finish later go (nil drops them). The editor widget calls it; a nil clipboard
// takes it away.
func (vm *ViewModel) SetClipboard(c Clipboard, memory *ClipboardMemory, report func(error)) {
	if c == nil {
		vm.clip = nil
		return
	}
	if memory == nil {
		memory = NewClipboardMemory()
	}
	vm.clip = &clipboardLink{clipboard: c, memory: memory, report: report}
}

// clipboard is the link to the clipboard, or an error when the editor has none.
func (vm *ViewModel) clipboard() (*clipboardLink, error) {
	if vm.clip == nil {
		return nil, errors.New("EditorClipboardUnavailable", "the editor has no clipboard")
	}
	return vm.clip, nil
}

// failed reports an error of a request to the clipboard that finished after its command.
func (l *clipboardLink) failed(err error) {
	if err == nil || l.report == nil {
		return
	}
	if _, ok := errors.FromError(err); ok {
		l.report(err)
		return
	}
	l.report(errors.NewErr("EditorClipboardFailed", err))
}

// copyToClipboard puts the selections on the clipboard and remembers how they were made. It
// reports whether there was anything to copy.
//
// Ported from VS Code: src/vs/editor/browser/controller/editContext/clipboardUtils.ts (generateDataToCopyAndStoreInMemory)
func (vm *ViewModel) copyToClipboard(l *clipboardLink) bool {
	data := vm.commands().Copy(vm.cursors.Selections(), vm.opts.EmptySelectionClipboard)
	if data.Text == "" && data.Pieces == nil && !data.FromEmptySelection {
		return false
	}
	l.memory.last = &copied{text: data.Text, fromEmptySelection: data.FromEmptySelection, pieces: data.Pieces, block: vm.columns != nil}
	l.clipboard.Write(data.Text, l.failed)
	return true
}

// copySelection copies the selections, or the line of a cursor without one
// (editor.emptySelectionClipboard). It changes nothing in the editor, so the sessions of the
// column selection and of the occurrences go on.
func (vm *ViewModel) copySelection() error {
	vm.keepSessions, vm.skipReveal = true, true
	l, err := vm.clipboard()
	if err != nil {
		return err
	}
	vm.copyToClipboard(l)
	return nil
}

// cutSelection copies the selections and deletes them; in a read-only document it only copies.
//
// Ported from VS Code: src/vs/editor/contrib/clipboard/browser/clipboard.ts (the cut action)
func (vm *ViewModel) cutSelection() error {
	l, err := vm.clipboard()
	if err != nil {
		return err
	}
	if !vm.copyToClipboard(l) || vm.doc.ReadOnly() {
		return nil
	}
	return vm.apply(vm.commands().Cut(vm.cursors.Selections(), vm.opts.EmptySelectionClipboard))
}

// pasteClipboard pastes the text on the clipboard, when it has arrived: the paste is applied in
// the callback, on the UI thread, as one command.
func (vm *ViewModel) pasteClipboard() error {
	vm.keepSessions, vm.skipReveal = true, true
	l, err := vm.clipboard()
	if err != nil {
		return err
	}
	if vm.doc.ReadOnly() {
		return nil
	}
	l.clipboard.Read(func(text string, err error) {
		if err != nil {
			l.failed(err)
			return
		}
		if text != "" {
			l.failed(vm.PasteText(text))
		}
	})
	return nil
}

// PastePrimary pastes the primary selection of the desktop at the cursors (what the middle
// button does), when it has arrived.
func (vm *ViewModel) PastePrimary() error {
	l, err := vm.clipboard()
	if err != nil {
		return err
	}
	l.clipboard.ReadPrimary(func(text string, err error) {
		if err != nil {
			l.failed(err)
			return
		}
		if text != "" {
			l.failed(vm.PasteText(text))
		}
	})
	return nil
}

// PasteText puts a text at the cursors as a paste does, as one change that undo takes back at
// once. What the editor knows of the text, when it was the last thing it copied, makes a copied
// line go above the cursor's line, the pieces of several cursors go to as many cursors, and a
// column go down the lines; with several cursors a text of as many lines goes line by line
// (editor.multiCursorPaste).
//
// Ported from VS Code: src/vs/editor/browser/controller/editContext/native/nativeEditContext.ts (the paste handler) and
// src/vs/editor/common/cursor/cursorTypeEditOperations.ts (PasteOperation)
func (vm *ViewModel) PasteText(text string) error {
	opts := edit.PasteOptions{Spread: vm.opts.MultiCursorPaste != "full"}
	if vm.clip != nil {
		if known := vm.clip.memory.recall(text); known != nil {
			opts.OnNewLine = known.fromEmptySelection && vm.opts.EmptySelectionClipboard
			opts.Pieces = known.pieces
			opts.Block = known.block
		}
	}
	return vm.Run(func(vm *ViewModel, _ any) error {
		return vm.apply(vm.commands().Paste(vm.cursors.Selections(), text, opts))
	}, nil)
}

// selectedText is the text of the selections when every cursor has one, in the order of the
// text with a line break between them, or false when a cursor has none.
func (vm *ViewModel) selectedText() (string, bool) {
	sels := vm.cursors.Selections()
	ranges := make([]buffer.Range, len(sels))
	for i, s := range sels {
		if s.IsEmpty() {
			return "", false
		}
		ranges[i] = s.Range()
	}
	sort.SliceStable(ranges, func(i, j int) bool { return buffer.CompareUsingStarts(ranges[i], ranges[j]) < 0 })
	snap := vm.doc.Snapshot()
	parts := make([]string, len(ranges))
	for i, r := range ranges {
		parts[i] = snap.Text(r)
	}
	return strings.Join(parts, "\n"), true
}
