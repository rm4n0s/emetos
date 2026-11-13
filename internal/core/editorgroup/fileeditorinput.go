package editorgroup

import (
	"encoding/json"
	"path/filepath"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// FileEditorInputTypeID is the TypeID of a FileEditorInput.
const FileEditorInputTypeID = "emetos.fileEditor"

// FileEditorInput is the input of a file shown in a file editor of an extension (SPEC §10.37 "File editors", step 36.1): the tab of a
// file (its name as the title, its path as the tooltip, a file resource for Copy Path and the reveals) whose content an extension
// makes. It is never dirty: the file editors only show. Two inputs of the same file are the same tab, whatever their editor. The state
// the editor saves (its zoom, its page) is kept in the serialized value, read through the state function the window sets.
type FileEditorInput struct {
	BaseInput
	path, editor string
	// state is the state the input was restored with; stateOf, when set, reads the state of the editor now.
	state   json.RawMessage
	stateOf func() json.RawMessage
}

// NewFileEditorInput makes the input of the file at path shown in the file editor with the id.
func NewFileEditorInput(path, editorID string) *FileEditorInput {
	return &FileEditorInput{path: path, editor: editorID}
}

// fileEditorValue is what Serialize writes.
type fileEditorValue struct {
	Path   string          `json:"path"`
	Editor string          `json:"editor"`
	State  json.RawMessage `json:"state,omitempty"`
}

// ParseFileEditorInput makes the input again from the value Serialize gave, with the state it held. The error has the tag
// FileEditorInputInvalid.
func ParseFileEditorInput(value string) (*FileEditorInput, error) {
	var v fileEditorValue
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		return nil, errors.NewErr("FileEditorInputInvalid", err).SetMetadata("value", value)
	}
	if !filepath.IsAbs(v.Path) || v.Editor == "" {
		return nil, errors.New("FileEditorInputInvalid", "a saved file editor tab needs the absolute path of its file and the id of its editor", "value", value)
	}
	in := NewFileEditorInput(filepath.Clean(v.Path), v.Editor)
	in.state = v.State
	return in, nil
}

// Path is the path of the file.
func (e *FileEditorInput) Path() string { return e.path }

// Editor is the id of the file editor.
func (e *FileEditorInput) Editor() string { return e.editor }

// State is the state the editor saved: what the state function gives, else what the input was restored with.
func (e *FileEditorInput) State() json.RawMessage {
	if e.stateOf != nil {
		if s := e.stateOf(); s != nil {
			return s
		}
	}
	return e.state
}

// RestoredState is the state the input was made with (restored, or copied), which the editor starts from.
func (e *FileEditorInput) RestoredState() json.RawMessage { return e.state }

// SetState sets the state the editor starts from (a copy of another tab's).
func (e *FileEditorInput) SetState(state json.RawMessage) { e.state = state }

// SetStateFunc sets the function that reads the state of the editor; the window sets it.
func (e *FileEditorInput) SetStateFunc(f func() json.RawMessage) { e.stateOf = f }

// TypeID implements EditorInput.
func (e *FileEditorInput) TypeID() string { return FileEditorInputTypeID }

// Resource implements EditorInput: the file.
func (e *FileEditorInput) Resource() (document.URI, bool) { return document.FileURI(e.path), true }

// Name implements EditorInput: the name of the file.
func (e *FileEditorInput) Name() string { return filepath.Base(e.path) }

// Description implements EditorInput: the folder of the file.
func (e *FileEditorInput) Description() string { return filepath.Dir(e.path) }

// Title implements EditorInput: the path.
func (e *FileEditorInput) Title() string { return e.path }

// Dirty implements EditorInput: a file editor only shows.
func (e *FileEditorInput) Dirty() bool { return false }

// Matches implements EditorInput: an input of the same file, in any file editor.
func (e *FileEditorInput) Matches(other EditorInput) bool {
	o, ok := other.(*FileEditorInput)
	return ok && o != nil && o.path == e.path
}

// Capabilities implements EditorInput: the tab can be split.
func (e *FileEditorInput) Capabilities() Capabilities { return CapCanSplit }

// Serialize implements EditorInput: the path, the editor and its state.
func (e *FileEditorInput) Serialize() (string, bool) {
	data, err := json.Marshal(fileEditorValue{Path: e.path, Editor: e.editor, State: e.State()})
	if err != nil {
		return "", false
	}
	return string(data), true
}

// CopyInput implements Copier: the same file in the same editor, starting with the state of this one.
func (e *FileEditorInput) CopyInput() EditorInput {
	c := NewFileEditorInput(e.path, e.editor)
	c.state = e.State()
	return c
}
