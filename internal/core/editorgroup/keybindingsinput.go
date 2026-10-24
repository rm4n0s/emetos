package editorgroup

import (
	"encoding/json"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// KeybindingsInputTypeID is the TypeID of a KeybindingsInput.
const KeybindingsInputTypeID = "emetos.keybindings"

// KeybindingsViewState is what the keybindings editor remembers of its view, and what Serialize keeps of the
// input: the text of the search and the row that was selected.
type KeybindingsViewState struct {
	// Query is the text of the search box.
	Query string `json:"query,omitempty"`
	// Command is the id of the command of the selected row, and Key the keybinding of the row as keybindings.json
	// writes it (empty for a row without one); together they find the row again.
	Command string `json:"command,omitempty"`
	Key     string `json:"key,omitempty"`
}

// KeybindingsInput is the input of the keybindings editor (SPEC §11.3, "Keyboard Shortcuts"): like the settings
// input it shows no resource and holds no content, so it is never dirty and closing it never asks, and opening it
// again reveals the one that is open. The editor writes its view state into the input, and Serialize keeps it for
// the state of the window (step 7.5).
type KeybindingsInput struct {
	BaseInput
	state KeybindingsViewState
}

// NewKeybindingsInput makes the input of a keybindings editor with a view state.
func NewKeybindingsInput(state KeybindingsViewState) *KeybindingsInput {
	return &KeybindingsInput{state: state}
}

// ParseKeybindingsInput makes the input again from what Serialize gave. A text that is not the state of an input
// fails with the tag KeybindingsInputInvalid.
func ParseKeybindingsInput(value string) (*KeybindingsInput, error) {
	var st KeybindingsViewState
	if value != "" {
		if err := json.Unmarshal([]byte(value), &st); err != nil {
			return nil, errors.NewErr("KeybindingsInputInvalid", err).SetMetadata("value", value)
		}
	}
	return NewKeybindingsInput(st), nil
}

// ViewState is the state of the view the input keeps.
func (k *KeybindingsInput) ViewState() KeybindingsViewState { return k.state }

// SetViewState replaces the state of the view; the editor calls it when the user changes the search or the row.
func (k *KeybindingsInput) SetViewState(st KeybindingsViewState) { k.state = st }

// TypeID implements EditorInput.
func (k *KeybindingsInput) TypeID() string { return KeybindingsInputTypeID }

// Resource implements EditorInput: the keybindings editor shows no resource.
func (k *KeybindingsInput) Resource() (document.URI, bool) { return document.URI{}, false }

// Name implements EditorInput.
func (k *KeybindingsInput) Name() string { return "Keyboard Shortcuts" }

// Description implements EditorInput.
func (k *KeybindingsInput) Description() string { return "" }

// Title implements EditorInput.
func (k *KeybindingsInput) Title() string { return "Keyboard Shortcuts" }

// Dirty implements EditorInput: the editor writes keybindings.json as the user edits, there is nothing to save.
func (k *KeybindingsInput) Dirty() bool { return false }

// Matches implements EditorInput: any keybindings input is the same editor.
func (k *KeybindingsInput) Matches(other EditorInput) bool {
	o, ok := other.(*KeybindingsInput)
	return ok && o != nil
}

// Capabilities implements EditorInput: none (it cannot be split, saved or reverted).
func (k *KeybindingsInput) Capabilities() Capabilities { return 0 }

// Serialize implements EditorInput: the view state as JSON.
func (k *KeybindingsInput) Serialize() (string, bool) {
	data, err := json.Marshal(k.state)
	if err != nil {
		return "", false
	}
	return string(data), true
}
