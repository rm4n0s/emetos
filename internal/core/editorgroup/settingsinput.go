package editorgroup

import (
	"encoding/json"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// SettingsInputTypeID is the TypeID of a SettingsInput.
const SettingsInputTypeID = "emetos.settings"

// SettingsTarget names the tab of the settings editor: the user's settings or the workspace's.
type SettingsTarget string

// The tabs of the settings editor.
const (
	// SettingsUser is the User tab.
	SettingsUser SettingsTarget = "user"
	// SettingsWorkspace is the Workspace tab.
	SettingsWorkspace SettingsTarget = "workspace"
)

// SettingsViewState is what a settings editor remembers of its view, and what Serialize keeps of the input:
// the tab, the group of the category tree that is selected and how far the list is scrolled.
type SettingsViewState struct {
	// Target is the tab; the User tab when empty.
	Target SettingsTarget `json:"target,omitempty"`
	// Category is the id of the selected group of the tree ("editor/font"); the first group when empty.
	Category string `json:"category,omitempty"`
	// Scroll is how far the list of settings is scrolled, in pixels.
	Scroll float64 `json:"scroll,omitempty"`
	// Query is the text of the search box (step 8.2 fills it from the arguments of the command).
	Query string `json:"query,omitempty"`
}

// SettingsInput is the input of the settings editor (SPEC §11.1): it shows no resource and holds no content, so it
// is never dirty and closing it never asks. There is one per group (it is a singleton as VS Code's settings
// editor input is: opening it again reveals the one that is open). The editor writes its view state into the input,
// and Serialize keeps it for the state of the window (step 7.5).
type SettingsInput struct {
	BaseInput
	state SettingsViewState
}

// NewSettingsInput makes the input of a settings editor with a view state.
func NewSettingsInput(state SettingsViewState) *SettingsInput {
	return &SettingsInput{state: state}
}

// ParseSettingsInput makes the input again from what Serialize gave. A text that is not the state of an input
// fails with the tag SettingsInputInvalid.
func ParseSettingsInput(value string) (*SettingsInput, error) {
	var st SettingsViewState
	if value != "" {
		if err := json.Unmarshal([]byte(value), &st); err != nil {
			return nil, errors.NewErr("SettingsInputInvalid", err).SetMetadata("value", value)
		}
	}
	if st.Target != SettingsUser && st.Target != SettingsWorkspace {
		st.Target = SettingsUser
	}
	return NewSettingsInput(st), nil
}

// ViewState is the state of the view the input keeps.
func (s *SettingsInput) ViewState() SettingsViewState { return s.state }

// SetViewState replaces the state of the view; the editor calls it when the user changes the tab, the category or
// the scroll position.
func (s *SettingsInput) SetViewState(st SettingsViewState) { s.state = st }

// TypeID implements EditorInput.
func (s *SettingsInput) TypeID() string { return SettingsInputTypeID }

// Resource implements EditorInput: the settings editor shows no resource.
func (s *SettingsInput) Resource() (document.URI, bool) { return document.URI{}, false }

// Name implements EditorInput.
func (s *SettingsInput) Name() string { return "Settings" }

// Description implements EditorInput.
func (s *SettingsInput) Description() string { return "" }

// Title implements EditorInput.
func (s *SettingsInput) Title() string { return "Settings" }

// Dirty implements EditorInput: the editor writes the settings as they change, there is nothing to save.
func (s *SettingsInput) Dirty() bool { return false }

// Matches implements EditorInput: any settings input is the same editor.
func (s *SettingsInput) Matches(other EditorInput) bool {
	o, ok := other.(*SettingsInput)
	return ok && o != nil
}

// Capabilities implements EditorInput: none (it cannot be split, saved or reverted).
func (s *SettingsInput) Capabilities() Capabilities { return 0 }

// Serialize implements EditorInput: the view state as JSON.
func (s *SettingsInput) Serialize() (string, bool) {
	data, err := json.Marshal(s.state)
	if err != nil {
		return "", false
	}
	return string(data), true
}
