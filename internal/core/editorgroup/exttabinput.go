package editorgroup

import (
	"encoding/json"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// ExtensionTabInputTypeID is the TypeID of an ExtensionTabInput.
const ExtensionTabInputTypeID = "emetos.extensionTab"

// ExtensionTabInput is the input of an editor tab that an extension contributes (SPEC §10.15 "Editor tab", step 11.6): a
// tab with no file, of a kind the extension declares, with an instance id of its own so that several can be open. Its
// title, icon and marks (dirty, busy) are set by the extension through the host; what the tab shows and the state it
// saves are the extension's, and live in the host (the input only names the tab). A dirty mark is a mark only: closing a
// dirty tab asks nothing, because the data of the tab is the extension's own and is kept in the state it saves.
type ExtensionTabInput struct {
	BaseInput
	ext, kind, id string
	title, hint   string
	icon          string
	dirty, busy   bool
	copier        func() *ExtensionTabInput
}

// NewExtensionTabInput makes the input of the tab with the instance id of a kind of an extension. icon is the name of a
// Codicon; it may be empty.
func NewExtensionTabInput(ext, kind, id, title, icon string) *ExtensionTabInput {
	return &ExtensionTabInput{ext: ext, kind: kind, id: id, title: title, icon: icon}
}

// extensionTabValue is what Serialize writes.
type extensionTabValue struct {
	Ext   string `json:"ext"`
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

// ParseExtensionTabInput makes the input again from the value Serialize gave. The error has the tag
// ExtensionTabInputInvalid.
func ParseExtensionTabInput(value string) (*ExtensionTabInput, error) {
	var v extensionTabValue
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		return nil, errors.NewErr("ExtensionTabInputInvalid", err)
	}
	if v.Ext == "" || v.Kind == "" || v.ID == "" {
		return nil, errors.New("ExtensionTabInputInvalid", "a saved extension tab needs its extension, kind and id", "value", value)
	}
	return NewExtensionTabInput(v.Ext, v.Kind, v.ID, v.Title, v.Icon), nil
}

// Extension is the id of the extension the tab belongs to, Kind the id of its kind and ID the instance.
func (e *ExtensionTabInput) Extension() string { return e.ext }

// Kind is the id of the kind of the tab.
func (e *ExtensionTabInput) Kind() string { return e.kind }

// ID is the instance id of the tab, unique in the workspace.
func (e *ExtensionTabInput) ID() string { return e.id }

// Icon is the name of the Codicon of the tab; empty for none.
func (e *ExtensionTabInput) Icon() string { return e.icon }

// Busy tells whether the extension marked the tab as busy.
func (e *ExtensionTabInput) Busy() bool { return e.busy }

// SetTitle sets the name of the tab.
func (e *ExtensionTabInput) SetTitle(title string) {
	if e.title != title {
		e.title = title
		e.Emit(InputLabel)
	}
}

// SetHint sets the text of the tooltip of the tab (the title when empty).
func (e *ExtensionTabInput) SetHint(hint string) {
	if e.hint != hint {
		e.hint = hint
		e.Emit(InputLabel)
	}
}

// SetDirty sets the dirty mark.
func (e *ExtensionTabInput) SetDirty(dirty bool) {
	if e.dirty != dirty {
		e.dirty = dirty
		e.Emit(InputDirty)
	}
}

// SetBusy sets the busy mark.
func (e *ExtensionTabInput) SetBusy(busy bool) {
	if e.busy != busy {
		e.busy = busy
		e.Emit(InputLabel)
	}
}

// SetCopier sets the function that makes another input for a copy of the tab (a split); the host sets it.
func (e *ExtensionTabInput) SetCopier(copier func() *ExtensionTabInput) { e.copier = copier }

// TypeID implements EditorInput.
func (e *ExtensionTabInput) TypeID() string { return ExtensionTabInputTypeID }

// Resource implements EditorInput: the tab shows no resource.
func (e *ExtensionTabInput) Resource() (document.URI, bool) { return document.URI{}, false }

// Name implements EditorInput.
func (e *ExtensionTabInput) Name() string { return e.title }

// Description implements EditorInput.
func (e *ExtensionTabInput) Description() string { return "" }

// Title implements EditorInput.
func (e *ExtensionTabInput) Title() string {
	if e.hint != "" {
		return e.hint
	}
	return e.title
}

// TabKey implements TabKeyed: the instance, so that two tabs with the same title are two tabs.
func (e *ExtensionTabInput) TabKey() string { return "extension-tab:" + e.id }

// Dirty implements EditorInput: the mark the extension set.
func (e *ExtensionTabInput) Dirty() bool { return e.dirty }

// Matches implements EditorInput: the same instance.
func (e *ExtensionTabInput) Matches(other EditorInput) bool {
	o, ok := other.(*ExtensionTabInput)
	return ok && o != nil && o.id == e.id
}

// CloseUnasked implements CloseUnasked: the dirty mark of an extension tab is only a mark.
func (e *ExtensionTabInput) CloseUnasked() bool { return true }

// Capabilities implements EditorInput: the tab can be split.
func (e *ExtensionTabInput) Capabilities() Capabilities { return CapCanSplit }

// Serialize implements EditorInput.
func (e *ExtensionTabInput) Serialize() (string, bool) {
	data, err := json.Marshal(extensionTabValue{Ext: e.ext, Kind: e.kind, ID: e.id, Title: e.title, Icon: e.icon})
	if err != nil {
		return "", false
	}
	return string(data), true
}

// CopyInput implements Copier: another instance of the same kind, made by the host with the state of this one. A tab
// without a host (a restored tab whose scope is gone) cannot be copied.
func (e *ExtensionTabInput) CopyInput() EditorInput {
	if e.copier == nil {
		return NewExtensionTabInput(e.ext, e.kind, e.id+"-copy", e.title, e.icon)
	}
	if c := e.copier(); c != nil {
		return c
	}
	return NewExtensionTabInput(e.ext, e.kind, e.id+"-copy", e.title, e.icon)
}
