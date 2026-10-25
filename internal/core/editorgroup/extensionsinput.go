package editorgroup

import (
	"github.com/rm4n0s/emetos/internal/core/document"
)

// ExtensionsInputTypeID is the TypeID of an ExtensionsInput.
const ExtensionsInputTypeID = "emetos.extensions"

// ExtensionsInput is the input of the page "Extensions" (SPEC §10.15, step 11.1): like the settings input it shows no
// resource and holds no content, so it is never dirty and closing it never asks, and opening it again reveals the one that
// is open. The page keeps no state of its own, so Serialize has nothing to keep and the state of the window only records
// that the page was open.
type ExtensionsInput struct {
	BaseInput
}

// NewExtensionsInput makes the input of an Extensions page.
func NewExtensionsInput() *ExtensionsInput { return &ExtensionsInput{} }

// TypeID implements EditorInput.
func (e *ExtensionsInput) TypeID() string { return ExtensionsInputTypeID }

// Resource implements EditorInput: the page shows no resource.
func (e *ExtensionsInput) Resource() (document.URI, bool) { return document.URI{}, false }

// Name implements EditorInput.
func (e *ExtensionsInput) Name() string { return "Extensions" }

// Description implements EditorInput.
func (e *ExtensionsInput) Description() string { return "" }

// Title implements EditorInput.
func (e *ExtensionsInput) Title() string { return "Extensions" }

// Dirty implements EditorInput: there is nothing to save.
func (e *ExtensionsInput) Dirty() bool { return false }

// Matches implements EditorInput: any Extensions input is the same page.
func (e *ExtensionsInput) Matches(other EditorInput) bool {
	o, ok := other.(*ExtensionsInput)
	return ok && o != nil
}

// Capabilities implements EditorInput: none (it cannot be split, saved or reverted).
func (e *ExtensionsInput) Capabilities() Capabilities { return 0 }

// Serialize implements EditorInput: an empty object, which ParseExtensionsInput accepts.
func (e *ExtensionsInput) Serialize() (string, bool) { return "{}", true }
