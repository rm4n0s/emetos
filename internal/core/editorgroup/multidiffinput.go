package editorgroup

import (
	"encoding/json"
	"strconv"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// Ported from VS Code: src/vs/workbench/contrib/multiDiffEditor/browser/multiDiffEditorInput.ts (the label of the editor and its
// serialization: the source and the label are saved, the files are asked of the source again)

// MultiDiffInputTypeID is the TypeID of a MultiDiffInput.
const MultiDiffInputTypeID = "emetos.multiDiff"

// MultiDiffItem is one file of a multi-file diff editor (SPEC §10.30 "Diffs of changes"): its two documents and what the header of its
// section shows. It is plain data; the window turns it into a section.
type MultiDiffItem struct {
	// Original and Modified are the URIs of the two documents; an empty one is a file that does not exist on that side.
	Original, Modified string
	// Path is the path of the file for the header, with slashes: the last element is the name and the rest the folder.
	Path string
	// File is the file URI that gives the header its icon and decorations and that Open File shows; empty for none.
	File string
	// Letter, Color and Tooltip are the mark of the status at the end of the header.
	Letter, Color, Tooltip string
	// Context holds the context keys of the file's actions, and Args is what their commands run with.
	Context map[string]any
	Args    any
}

// Key identifies the item among the items of an editor: a section keeps its collapsed mark and its measured height while the same
// key comes back after the set was asked for again.
func (m MultiDiffItem) Key() string { return m.Path + "\x00" + m.Original + "\x00" + m.Modified }

// MultiDiffInput is the input of the multi-file diff editor (SPEC §10.30): the changes of many files in one scrolling editor. A set
// that an extension's provider makes is named by its Kind and Key, which is all that is saved with the window; a restored input has no
// items until the window has asked the provider (Loaded). A fixed set (no Kind) holds its items and is not saved. The input owns no
// document: the sections open and let go of theirs as they come near the viewport.
type MultiDiffInput struct {
	BaseInput
	kind, key, title string
	// fixed is the id of a fixed set, empty for the set of a provider.
	fixed  string
	items  []MultiDiffItem
	loaded bool
	// failure is the message shown in place of the files when the provider could not make them.
	failure string
}

// NewMultiDiffInput makes the input of the set of a provider: it has no items until SetItems.
func NewMultiDiffInput(kind, key, title string) *MultiDiffInput {
	return &MultiDiffInput{kind: kind, key: key, title: title}
}

// NewFixedMultiDiffInput makes the input of a fixed set; id tells it from the other fixed sets of the window.
func NewFixedMultiDiffInput(id, title string, items []MultiDiffItem) *MultiDiffInput {
	return &MultiDiffInput{fixed: id, title: title, items: items, loaded: true}
}

// multiDiffValue is what Serialize keeps of an input.
type multiDiffValue struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Title string `json:"title"`
}

// ParseMultiDiffInput makes the input of a saved set again from the value Serialize gave. The error has the tag MultiDiffInputInvalid.
func ParseMultiDiffInput(value string) (*MultiDiffInput, error) {
	var v multiDiffValue
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		return nil, errors.NewErr("MultiDiffInputInvalid", err).SetMetadata("value", value)
	}
	if v.Kind == "" || v.Key == "" {
		return nil, errors.New("MultiDiffInputInvalid", "a saved multi-file diff editor needs its kind and key", "value", value)
	}
	return NewMultiDiffInput(v.Kind, v.Key, v.Title), nil
}

// Kind and Key name the set of a provider; both are empty for a fixed set.
func (m *MultiDiffInput) Kind() string { return m.kind }

// Key is the key of the set among the sets of its kind.
func (m *MultiDiffInput) Key() string { return m.key }

// Fixed tells whether the set is a fixed list that no provider makes (and that is not saved).
func (m *MultiDiffInput) Fixed() bool { return m.fixed != "" }

// Items are the files of the set, as the provider made them last; none before the first answer.
func (m *MultiDiffInput) Items() []MultiDiffItem { return m.items }

// Loaded tells whether the provider has answered at least once (a fixed set always has).
func (m *MultiDiffInput) Loaded() bool { return m.loaded }

// Failure is the message of the provider's last failure, empty after an answer.
func (m *MultiDiffInput) Failure() string { return m.failure }

// SetItems gives the input the files the provider made. The tab shows their number.
func (m *MultiDiffInput) SetItems(items []MultiDiffItem) {
	m.items, m.loaded, m.failure = items, true, ""
	m.Emit(InputLabel)
}

// SetFailure records that the provider could not make the files; the editor shows the message.
func (m *MultiDiffInput) SetFailure(message string) {
	m.failure = message
	m.Emit(InputLabel)
}

// SetTitle sets the title of the opener's.
func (m *MultiDiffInput) SetTitle(title string) {
	if m.title != title {
		m.title = title
		m.Emit(InputLabel)
	}
}

// TypeID implements EditorInput.
func (m *MultiDiffInput) TypeID() string { return MultiDiffInputTypeID }

// Resource implements EditorInput: the editor shows no one resource.
func (m *MultiDiffInput) Resource() (document.URI, bool) { return document.URI{}, false }

// Name implements EditorInput: the title and the number of files ("Git: Changes (3 files)").
func (m *MultiDiffInput) Name() string {
	if !m.loaded {
		return m.title
	}
	n := len(m.items)
	if n == 1 {
		return m.title + " (1 file)"
	}
	return m.title + " (" + strconv.Itoa(n) + " files)"
}

// Description implements EditorInput.
func (m *MultiDiffInput) Description() string { return "" }

// Title implements EditorInput: the title without the count.
func (m *MultiDiffInput) Title() string { return m.title }

// TabKey implements TabKeyed: the set, so that two sets with the same title are two tabs.
func (m *MultiDiffInput) TabKey() string {
	if m.fixed != "" {
		return "multidiff:fixed:" + m.fixed
	}
	return "multidiff:" + m.kind + ":" + m.key
}

// Dirty implements EditorInput: the editor changes nothing.
func (m *MultiDiffInput) Dirty() bool { return false }

// Matches implements EditorInput: the same set of the same kind, or the same fixed set.
func (m *MultiDiffInput) Matches(other EditorInput) bool {
	o, ok := other.(*MultiDiffInput)
	if !ok || o == nil {
		return false
	}
	return o == m || (m.fixed == "" && o.fixed == "" && o.kind == m.kind && o.key == m.key)
}

// Capabilities implements EditorInput: the editor is shown in one group (it cannot be split).
func (m *MultiDiffInput) Capabilities() Capabilities { return 0 }

// Serialize implements EditorInput: the kind, the key and the title; a fixed set is not saved.
func (m *MultiDiffInput) Serialize() (string, bool) {
	if m.fixed != "" {
		return "", false
	}
	data, err := json.Marshal(multiDiffValue{Kind: m.kind, Key: m.key, Title: m.title})
	if err != nil {
		return "", false
	}
	return string(data), true
}
