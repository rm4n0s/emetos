package editorgroup

import (
	"encoding/json"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/mergeEditorInput.ts (the name, title, description
// and dirty state of a merge editor) and src/vs/workbench/contrib/mergeEditor/browser/mergeEditorSerializer.ts

// MergeInputTypeID is the TypeID of a MergeInput.
const MergeInputTypeID = "emetos.merge"

// MergeNamePrefix is what the name of a merge editor starts with ("Merging: result.go", VS Code's
// `localize('name', "Merging: {0}", ...)`).
const MergeNamePrefix = "Merging: "

// MergeKind says where the three texts of a merge come from.
type MergeKind string

// The kinds of merge.
const (
	// MergeMarkers is a file with conflict markers: the inputs and the base are made from the markers (SPEC
	// §9.11 (a)), and the file itself is the result.
	MergeMarkers MergeKind = "markers"
	// MergeFiles is `emetos --merge <path1> <path2> <base> <result>` (SPEC §9.11 (b)): four files.
	MergeFiles MergeKind = "files"
)

// MergeSource names the files of a merge; it is what a MergeInput keeps and serializes. For a marker file only
// Result is set.
type MergeSource struct {
	// Kind is where the texts come from.
	Kind MergeKind
	// Input1 (incoming), Input2 (current) and Base are the paths of the files of a MergeFiles merge.
	Input1, Input2, Base string
	// Result is the path of the file that holds the result: the file with the markers, or the fourth path of
	// --merge.
	Result string
}

// MergeInput is the input of the merge editor (SPEC §9.11): a merge whose result is a document, which the editor
// edits. The input is dirty when the result document is, saving it saves that document, and closing it asks about
// that document as a text editor does (it is a DocumentEditor). It does not own the document: the window counts a
// use of it for as long as the editor is open. The inputs and the base of the merge are made from the files by
// the window when the editor opens; the input names them so that they can be made again when it is restored.
type MergeInput struct {
	BaseInput
	result *document.Document
	source MergeSource
	opts   TextInputOpts
	dirty  bool
	stops  []func()
}

// NewMergeInput makes the input of a merge whose result is a document. It follows the document until Dispose.
func NewMergeInput(result *document.Document, source MergeSource, opts TextInputOpts) *MergeInput {
	m := &MergeInput{result: result, source: source, opts: opts, dirty: result.Dirty()}
	m.stops = []func(){
		result.Subscribe(func(document.ChangeEvent) { m.Refresh() }),
		result.OnSaved(func(document.SavedEvent) { m.Refresh() }),
		result.OnURIChange(func(_, _ document.URI) { m.Emit(InputLabel) }),
	}
	return m
}

// Source names the files of the merge.
func (m *MergeInput) Source() MergeSource { return m.source }

// Result is the document that holds the result of the merge.
func (m *MergeInput) Result() *document.Document { return m.result }

// Refresh sends an event when the dirty mark of the result document is not the one the input last had.
func (m *MergeInput) Refresh() {
	if now := m.result.Dirty(); now != m.dirty {
		m.dirty = now
		m.Emit(InputDirty)
	}
}

// EditedDocument implements DocumentEditor: the result document.
func (m *MergeInput) EditedDocument() *document.Document { return m.result }

// TypeID implements EditorInput.
func (m *MergeInput) TypeID() string { return MergeInputTypeID }

// Resource implements EditorInput: a merge editor shows no one resource (it is not found as the editor of a
// file, and the history of locations skips it), as a compare editor does not.
func (m *MergeInput) Resource() (document.URI, bool) { return document.URI{}, false }

// Name implements EditorInput: "Merging: " and the name of the result file.
func (m *MergeInput) Name() string { return MergeNamePrefix + DocumentName(m.result) }

// Description implements EditorInput: the folder of the result file.
func (m *MergeInput) Description() string { return docDescription(m.result, m.opts) }

// Title implements EditorInput: "Merging: " and the path of the result file.
func (m *MergeInput) Title() string { return MergeNamePrefix + docTitle(m.result, m.opts) }

// TabKey implements TabKeyed: the result, so that the merges of two files are two tabs.
func (m *MergeInput) TabKey() string { return "merge:" + m.result.URI().String() }

// Dirty implements EditorInput: the result document has unsaved changes.
func (m *MergeInput) Dirty() bool { return m.dirty }

// Matches implements EditorInput: the merge of the same result.
func (m *MergeInput) Matches(other EditorInput) bool {
	o, ok := other.(*MergeInput)
	if !ok || o == nil {
		return false
	}
	return o == m || o.result.URI() == m.result.URI()
}

// Capabilities implements EditorInput: a merge editor is shown in one group (it cannot be split), and it is
// untitled while the result has no file yet.
func (m *MergeInput) Capabilities() Capabilities {
	var c Capabilities
	if m.result.ReadOnly() {
		c |= CapReadonly
	}
	if m.result.URI().Scheme == document.SchemeUntitled {
		c |= CapUntitled
	}
	return c
}

// mergeValue is what Serialize keeps of an input.
type mergeValue struct {
	Kind   MergeKind `json:"kind"`
	Input1 string    `json:"input1,omitempty"`
	Input2 string    `json:"input2,omitempty"`
	Base   string    `json:"base,omitempty"`
	Result string    `json:"result"`
}

// Serialize implements EditorInput: the kind of the merge and the files it was made from as JSON; the result is a
// URI of a file. A merge whose result is not a file cannot come back.
func (m *MergeInput) Serialize() (string, bool) {
	if m.result.URI().Scheme != document.SchemeFile {
		return "", false
	}
	v := mergeValue{Kind: m.source.Kind, Result: m.result.URI().String()}
	if m.source.Kind == MergeFiles {
		v.Input1, v.Input2, v.Base = m.source.Input1, m.source.Input2, m.source.Base
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// ParseMergeInput reads what Serialize gave: the source of the merge and the URI of the result (the window opens
// the result document and makes the input again with NewMergeInput). A value that is not JSON has the tag
// MergeInputInvalid, one without a kind or a result MergeInputIncomplete, a merge of files without all its paths
// MergeInputFilesIncomplete and a result that is not a file MergeInputResultNotFile.
func ParseMergeInput(value string) (MergeSource, document.URI, error) {
	var v mergeValue
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		return MergeSource{}, document.URI{}, errors.NewErr("MergeInputInvalid", err).SetMetadata("value", value)
	}
	if v.Result == "" || (v.Kind != MergeMarkers && v.Kind != MergeFiles) {
		return MergeSource{}, document.URI{}, errors.New("MergeInputIncomplete", "a merge editor needs a kind and the URI of its result", "value", value)
	}
	if v.Kind == MergeFiles && (v.Input1 == "" || v.Input2 == "" || v.Base == "") {
		return MergeSource{}, document.URI{}, errors.New("MergeInputFilesIncomplete", "a merge of files needs the paths of both inputs and of the base", "value", value)
	}
	uri, err := document.ParseURI(v.Result)
	if err != nil {
		return MergeSource{}, document.URI{}, err
	}
	if uri.Scheme != document.SchemeFile {
		return MergeSource{}, document.URI{}, errors.New("MergeInputResultNotFile", "the result of a merge editor is a file", "value", value)
	}
	return MergeSource{Kind: v.Kind, Input1: v.Input1, Input2: v.Input2, Base: v.Base, Result: uri.Path}, uri, nil
}

// Dispose implements EditorInput: it stops following the document.
func (m *MergeInput) Dispose() {
	for _, stop := range m.stops {
		stop()
	}
	m.stops = nil
	m.BaseInput.Dispose()
}
