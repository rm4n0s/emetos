package editorgroup

import (
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// TextInputTypeID is the TypeID of a TextInput.
const TextInputTypeID = "emetos.textEditor"

// TextInputOpts are the options of NewTextInput.
type TextInputOpts struct {
	// Folder is the folder open in the window; the description of a file below it is its folder
	// relative to it, and a file elsewhere is described by its folder.
	Folder string
	// Home is the home folder of the user, which a description writes as "~".
	Home string
}

// TextInput is the input of a text editor: a document. Documents are shared (the same file shown
// in two groups is one document with two inputs, each with the view state of its own, which the
// window keeps), so the input does not own the document: whoever opens it releases it.
type TextInput struct {
	BaseInput
	doc   *document.Document
	opts  TextInputOpts
	dirty bool
	stop  func()
	// stopURI stops following the document's URI.
	stopURI func()
}

// NewTextInput makes the input of a document. It follows the document's dirty mark until Dispose.
func NewTextInput(doc *document.Document, opts TextInputOpts) *TextInput {
	t := &TextInput{doc: doc, opts: opts, dirty: doc.Dirty()}
	t.stop = doc.Subscribe(func(document.ChangeEvent) { t.Refresh() })
	// a renamed or moved file gives the tab another name, description and title
	t.stopURI = doc.OnURIChange(func(_, _ document.URI) { t.Emit(InputLabel) })
	return t
}

// Document is the document the input shows.
func (t *TextInput) Document() *document.Document { return t.doc }

// Refresh sends an event when the dirty mark of the document is not the one the input last had.
// The input calls it after every change of the document; the code that saves a document calls it
// too, since a save changes no text.
func (t *TextInput) Refresh() {
	if d := t.doc.Dirty(); d != t.dirty {
		t.dirty = d
		t.Emit(InputDirty)
	}
}

// TypeID implements EditorInput.
func (t *TextInput) TypeID() string { return TextInputTypeID }

// Resource implements EditorInput.
func (t *TextInput) Resource() (document.URI, bool) { return t.doc.URI(), true }

// Name implements EditorInput: the name of the file, or Untitled-n.
func (t *TextInput) Name() string { return t.doc.URI().Title() }

// Description implements EditorInput: the folder of the file, relative to the folder of the
// window when it is below it, else with the home folder written "~". It is empty for a document
// that has no file.
func (t *TextInput) Description() string {
	uri := t.doc.URI()
	if uri.Scheme != document.SchemeFile {
		return ""
	}
	dir := filepath.Dir(uri.Path)
	if t.opts.Folder != "" {
		if rel, err := filepath.Rel(t.opts.Folder, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if rel == "." {
				return ""
			}
			return rel
		}
	}
	return tilde(dir, t.opts.Home)
}

// tilde writes a path inside the home folder with "~".
func tilde(path, home string) string {
	if home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, strings.TrimSuffix(home, "/")+"/"); ok {
		return "~/" + rest
	}
	return path
}

// Title implements EditorInput: the full path, with the home folder written "~".
func (t *TextInput) Title() string {
	uri := t.doc.URI()
	if uri.Scheme != document.SchemeFile {
		return uri.Title()
	}
	return tilde(uri.Path, t.opts.Home)
}

// Dirty implements EditorInput.
func (t *TextInput) Dirty() bool { return t.dirty }

// Matches implements EditorInput: another text input of the same document.
func (t *TextInput) Matches(other EditorInput) bool {
	o, ok := other.(*TextInput)
	if !ok || o == nil {
		return false
	}
	return o == t || o.doc.URI() == t.doc.URI()
}

// Capabilities implements EditorInput.
func (t *TextInput) Capabilities() Capabilities {
	c := CapCanSplit
	if t.doc.ReadOnly() {
		c |= CapReadonly
	}
	if t.doc.URI().Scheme == document.SchemeUntitled {
		c |= CapUntitled
	}
	return c
}

// Serialize implements EditorInput: the URI of a file. A document that is not a file is not
// restored.
func (t *TextInput) Serialize() (string, bool) {
	uri := t.doc.URI()
	if uri.Scheme != document.SchemeFile {
		return "", false
	}
	return uri.String(), true
}

// CopyInput implements Copier: another input for the same document, with the same options.
func (t *TextInput) CopyInput() EditorInput { return NewTextInput(t.doc, t.opts) }

// Dispose implements EditorInput: it stops following the document.
func (t *TextInput) Dispose() {
	if t.stop != nil {
		t.stop()
		t.stop = nil
	}
	if t.stopURI != nil {
		t.stopURI()
		t.stopURI = nil
	}
	t.BaseInput.Dispose()
}
