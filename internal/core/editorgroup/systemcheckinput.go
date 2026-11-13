package editorgroup

import (
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// SystemCheckInputTypeID is the TypeID of a SystemCheckInput.
const SystemCheckInputTypeID = "emetos.systemCheck"

// SystemCheckInput is the input of the page "System Check" (SPEC §10.36 "The Check System page", step 34.5): like the Extensions
// page it shows no resource and holds no content, so it is never dirty and closing it never asks, and opening it again reveals the one
// that is open. The results it shows belong to the window, so Serialize has nothing to keep and the state of the window only records
// that the page was open.
type SystemCheckInput struct {
	BaseInput
}

// NewSystemCheckInput makes the input of a System Check page.
func NewSystemCheckInput() *SystemCheckInput { return &SystemCheckInput{} }

// ParseSystemCheckInput reads what Serialize wrote: a JSON object (its fields are ignored, so a later version may add some). Anything
// else is SystemCheckInputInvalid.
func ParseSystemCheckInput(value string) (*SystemCheckInput, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &fields); err != nil {
		return nil, errors.NewErr("SystemCheckInputInvalid", err).SetMetadata("value", value)
	}
	if fields == nil {
		return nil, errors.New("SystemCheckInputInvalid", "a saved System Check page is a JSON object", "value", value)
	}
	return NewSystemCheckInput(), nil
}

// TypeID implements EditorInput.
func (e *SystemCheckInput) TypeID() string { return SystemCheckInputTypeID }

// Resource implements EditorInput: the page shows no resource.
func (e *SystemCheckInput) Resource() (document.URI, bool) { return document.URI{}, false }

// Name implements EditorInput.
func (e *SystemCheckInput) Name() string { return "System Check" }

// Description implements EditorInput.
func (e *SystemCheckInput) Description() string { return "" }

// Title implements EditorInput.
func (e *SystemCheckInput) Title() string { return "System Check" }

// Dirty implements EditorInput: the page is never dirty.
func (e *SystemCheckInput) Dirty() bool { return false }

// Matches implements EditorInput: any System Check input is the same page.
func (e *SystemCheckInput) Matches(other EditorInput) bool {
	o, ok := other.(*SystemCheckInput)
	return ok && o != nil
}

// Capabilities implements EditorInput: none (it cannot be split, saved or reverted).
func (e *SystemCheckInput) Capabilities() Capabilities { return 0 }

// Serialize implements EditorInput: an empty object, which ParseSystemCheckInput accepts.
func (e *SystemCheckInput) Serialize() (string, bool) { return "{}", true }
