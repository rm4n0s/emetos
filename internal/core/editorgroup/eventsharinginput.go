package editorgroup

import (
	"github.com/rm4n0s/emetos/internal/core/document"
)

// EventSharingInputTypeID is the TypeID of an EventSharingInput.
const EventSharingInputTypeID = "emetos.extensions.eventSharing"

// EventSharingInput is the input of the page "Extensions > Event Sharing" (SPEC §10.15, step 11.3): like the Extensions
// page it shows no resource and holds no content, so it is never dirty and closing it never asks, and opening it again
// reveals the one that is open. The links it shows belong to the process, not to the page, so Serialize has nothing to
// keep and the state of the window only records that the page was open.
type EventSharingInput struct {
	BaseInput
}

// NewEventSharingInput makes the input of an Event Sharing page.
func NewEventSharingInput() *EventSharingInput { return &EventSharingInput{} }

// TypeID implements EditorInput.
func (e *EventSharingInput) TypeID() string { return EventSharingInputTypeID }

// Resource implements EditorInput: the page shows no resource.
func (e *EventSharingInput) Resource() (document.URI, bool) { return document.URI{}, false }

// Name implements EditorInput.
func (e *EventSharingInput) Name() string { return "Event Sharing" }

// Description implements EditorInput.
func (e *EventSharingInput) Description() string { return "" }

// Title implements EditorInput.
func (e *EventSharingInput) Title() string { return "Extensions: Event Sharing" }

// Dirty implements EditorInput: the page is never dirty.
func (e *EventSharingInput) Dirty() bool { return false }

// Matches implements EditorInput: any Event Sharing input is the same page.
func (e *EventSharingInput) Matches(other EditorInput) bool {
	o, ok := other.(*EventSharingInput)
	return ok && o != nil
}

// Capabilities implements EditorInput: none (it cannot be split, saved or reverted).
func (e *EventSharingInput) Capabilities() Capabilities { return 0 }

// Serialize implements EditorInput: an empty object.
func (e *EventSharingInput) Serialize() (string, bool) { return "{}", true }
