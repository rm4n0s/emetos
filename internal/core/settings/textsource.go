package settings

import (
	"context"

	"github.com/rm4n0s/emetos/internal/core/jsonc"
)

// TextSource lets a Store use a document that an editor has open in place of a settings file
// (SPEC §11.1 "Settings in an open editor"). The app implements it over the documents of the
// window; a Store without one reads and writes the files.
//
// The rule is VS Code's (JSONEditingService.write): the store computes its edit against the text
// of the open document and the source applies it to the document, as one undo step. A document
// that was not dirty before the edit is saved at once, so the editor and the disk agree; a dirty
// document stays dirty and the disk is left alone until the user saves. The store reads the text
// of an open document, not the file, as the layer of its settings.
type TextSource interface {
	// OpenText gives the current text of the document that shows the file at path, and true while
	// one is open. The store calls it from any goroutine (a reload comes from the file watcher), so
	// an implementation keeps a copy of the text that it updates on the thread of the document.
	OpenText(path string) (text string, open bool)
	// EditText applies the edits to the document that shows the file at path; their offsets are in
	// the text OpenText gave. It saves the document when it was not dirty before the edit. It
	// returns false, and changes nothing, when no document is open for the path any more (the
	// store then writes the file). An error with true says that the document has the edit but could
	// not be saved. It is called on the thread that owns the documents.
	EditText(ctx context.Context, path string, edits []jsonc.Edit) (applied bool, err error)
}

// SetTextSource makes the store read and edit open documents through src; nil goes back to the
// files alone. It does not read the files again: call Reload for the layers to follow the
// documents that are open already.
func (s *Store) SetTextSource(src TextSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.source = src
}

// textSource is the source of the store, or nil.
func (s *Store) textSource() TextSource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.source
}
