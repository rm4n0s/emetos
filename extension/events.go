package extension

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/rm4n0s/errors"
)

// Event is something that happened, as a subscriber or a handler receives it (SPEC §10.15 "Events"). It is a copy: the
// value was encoded to JSON when it was published, and every receiver gets bytes of its own, so nothing a receiver does
// reaches the publisher or another receiver.
type Event struct {
	// Topic is the dotted name of the event: emetos.document.opened, markdown.preview.open.
	Topic string
	// Publisher is the id of the extension that published the event, or "emetos" for the core events.
	Publisher string
	// Scope is the window tab the event was published in.
	Scope ScopeInfo
	// Seq is the sequence number of the event: it grows with every event published in the scope, so it orders them. For
	// a batch it is the number of the last value.
	Seq uint64
	// Data is the JSON of the value. For a batch it is the last value of the batch.
	Data json.RawMessage
	// Batch holds every value of the frame, oldest first, for the events of a fast topic (typing: emetos.document.changed
	// and emetos.editor.selectionChanged, and the topics an extension declares with EventSpec.Fast): they are delivered
	// once per frame with every change of that frame. It is nil for any other event, and never empty for a fast one.
	Batch []json.RawMessage
}

// Decode decodes the value of the event into v (a pointer), as json.Unmarshal does. For a batch it decodes the last
// value; see EventValues for all of them. The error has the tag EventValueInvalid.
func (e Event) Decode(v any) error {
	if err := json.Unmarshal(e.Data, v); err != nil {
		return errors.NewErr("EventValueInvalid", err).SetMetadata("topic", e.Topic)
	}
	return nil
}

// EventValue decodes the value of the event as a T.
func EventValue[T any](e Event) (T, error) {
	var out T
	err := e.Decode(&out)
	return out, err
}

// EventValues decodes every value of the event as a T: the values of a batch, oldest first, or the one value of an event
// that is not a batch.
func EventValues[T any](e Event) ([]T, error) {
	raw := e.Batch
	if raw == nil {
		raw = []json.RawMessage{e.Data}
	}
	out := make([]T, 0, len(raw))
	for _, r := range raw {
		var v T
		if err := json.Unmarshal(r, &v); err != nil {
			return nil, errors.NewErr("EventValueInvalid", err).SetMetadata("topic", e.Topic)
		}
		out = append(out, v)
	}
	return out, nil
}

// EventHandler answers a request: it receives the event, on the UI thread, and returns the value of the reply or an
// error. The reply is copied like any value. An error that it returns reaches the requester with the tag
// EventHandlerFailed and is not a failure of the extension; a panic is. ctx ends when the request is answered, times out
// or is cancelled, or when the extension is deactivated.
type EventHandler func(ctx context.Context, ev Event) (reply any, err error)

// EventReply is the function an AsyncEventHandler calls with its answer.
type EventReply func(reply any, err error)

// AsyncEventHandler answers a request later: it is called on the UI thread with the event, hands the work to a goroutine
// if it is long, and calls reply once, from any goroutine, when it is done. A second call does nothing. A request that is
// not answered in time ends with EventTimeout.
type AsyncEventHandler func(ctx context.Context, ev Event, reply EventReply)

// EventSpec declares a topic of the Manifest.
type EventSpec struct {
	// Topic is the dotted topic, such as git.status.changed. It may be one that the extension owns (under its id) or
	// one it declares to publish (ownership, below).
	Topic string
	// Fast marks a stream that is delivered once per frame as a batch (Event.Batch), as typing is.
	Fast bool
}

// EventDecl are the event topics a Manifest declares.
type EventDecl struct {
	// Publishes are the topics the extension publishes. The topics under its own id (the id and a dot) are its own
	// without being listed; list one to make it Fast, and list the topics outside its id that it may publish. No two
	// extensions may declare a topic that is under the id of a third one, or one of Emetos.
	Publishes []EventSpec
	// Handles are the topics outside its id that the extension handles with Events().Handle.
	Handles []string
	// Subscribes are the topics and patterns the extension subscribes to; the Extensions page shows them, and nothing
	// stops an extension from subscribing to others inside its scope.
	Subscribes []string
}

// Events is how extensions talk: publish and subscribe, request and reply, inside one scope (SPEC §10.15). A topic is
// dotted segments of letters, digits, hyphens and underscores (CheckTopic). Delivery is asynchronous, on the UI thread, in
// order per publisher and topic; a publisher's events are also delivered to its own subscribers.
//
// An extension publishes and handles only the topics under its own id and those it declares in the Manifest; Emetos
// publishes the core events (the Topic constants below) in every workspace scope.
type Events interface {
	// Publish copies the value (it must encode to JSON) and delivers it to the subscribers of the topic later. It fails
	// with EventTopicInvalid, EventTopicNotOwned or EventValueInvalid.
	Publish(topic string, value any) error
	// Subscribe calls fn, on the UI thread, with every event whose topic matches the pattern (MatchTopic), and returns the
	// function that stops it. The calls stop when the extension is deactivated. A panic in fn deactivates the extension. It
	// fails with EventPatternInvalid.
	Subscribe(pattern string, fn func(Event)) (cancel func(), err error)
	// Request sends the value to the handler of the topic, which may belong to any extension of the scope, and calls
	// reply once, on the UI thread, with the reply of the handler or an error: EventNoHandler when nothing handles the
	// topic, EventTimeout when the handler does not answer in time (when ctx has a deadline, then at the deadline, else
	// after 10 seconds), EventCanceled when ctx is cancelled, EventHandlerFailed with the error the handler returned,
	// EventHandlerPanicked when it panicked, EventHandlerGone when its extension was deactivated before it answered. It
	// never blocks. It fails at once, without calling reply, with EventTopicInvalid or EventValueInvalid; reply is not
	// called when the requesting extension was deactivated.
	Request(ctx context.Context, topic string, value any, reply func(Event, error)) error
	// Handle makes fn the handler of requests of the topic, which the extension owns. A topic has one handler in a scope:
	// a second Handle fails with EventHandlerTaken. It fails with EventTopicInvalid or EventTopicNotOwned.
	Handle(topic string, fn EventHandler) error
	// HandleAsync is Handle for a handler that answers later (see AsyncEventHandler).
	HandleAsync(topic string, fn AsyncEventHandler) error
}

// The core events Emetos publishes in every workspace scope (SPEC §10.15). The payload of each is a JSON object; the
// types below describe them.
const (
	// TopicDocumentOpened: a document was opened or restored in the workspace. Payload DocumentInfo.
	TopicDocumentOpened = "emetos.document.opened"
	// TopicDocumentChanged: the text of a document changed. A fast topic. Payload DocumentChange.
	TopicDocumentChanged = "emetos.document.changed"
	// TopicDocumentSaved: a document was written to disk. Payload DocumentSaved.
	TopicDocumentSaved = "emetos.document.saved"
	// TopicDocumentClosed: a document was closed. Payload DocumentClosed.
	TopicDocumentClosed = "emetos.document.closed"
	// TopicEditorActiveChanged: another text editor became the active one, or none is. Payload ActiveEditor.
	TopicEditorActiveChanged = "emetos.editor.activeChanged"
	// TopicEditorSelectionChanged: the selections of the active editor changed. A fast topic. Payload EditorSelection.
	TopicEditorSelectionChanged = "emetos.editor.selectionChanged"
	// TopicWorkspaceOpened: the workspace of the scope was opened (published once, after the extensions that start with
	// it were activated). Payload WorkspaceInfo.
	TopicWorkspaceOpened = "emetos.workspace.opened"
	// TopicWorkspaceClosed: the workspace of the scope is closing; subscribers get it before they are deactivated.
	// Payload WorkspaceInfo.
	TopicWorkspaceClosed = "emetos.workspace.closed"
	// TopicWindowTabActivated: the window tab of the scope became the one the window shows. Payload WorkspaceInfo.
	TopicWindowTabActivated = "emetos.windowTab.activated"
)

// CorePublisher is the Publisher of the core events.
const CorePublisher = "emetos"

// Position is a place in a text: a zero-based line and a zero-based column counted in bytes of the UTF-8 line (as
// Emetos's text buffer does, SPEC §6.2).
type Position struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

// Range is the text from Start up to End.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// DocumentInfo is the payload of TopicDocumentOpened.
type DocumentInfo struct {
	// URI is the document: file:///path/to/file, untitled:1.
	URI string `json:"uri"`
	// LanguageID is the language of the document: go, markdown, plaintext.
	LanguageID string `json:"languageId"`
	// Version is the version of the text.
	Version int `json:"version"`
}

// TextChange is a replacement of a range with text.
type TextChange struct {
	// Range is the replaced range, in the text as it was before the change event.
	Range Range `json:"range"`
	// Text is the text that replaced it.
	Text string `json:"text"`
}

// DocumentChange is the payload of TopicDocumentChanged.
type DocumentChange struct {
	URI string `json:"uri"`
	// Version is the version of the document after the change.
	Version int `json:"version"`
	// Cause is "edit", "undo", "redo", "lineEnding" (no text changed) or "setText" (the whole text was replaced and the
	// history cleared).
	Cause string `json:"cause"`
	// Changes are the replacements, in order of position; the ranges of all of them are in the text before the change.
	Changes []TextChange `json:"changes"`
}

// DocumentSaved is the payload of TopicDocumentSaved.
type DocumentSaved struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
	// As tells that the save was a Save As, which gave the document the URI.
	As bool `json:"as"`
	// PreviousURI is the URI the document had, for a Save As (or after a move); empty otherwise.
	PreviousURI string `json:"previousUri,omitempty"`
}

// DocumentClosed is the payload of TopicDocumentClosed.
type DocumentClosed struct {
	URI string `json:"uri"`
}

// ActiveEditor is the payload of TopicEditorActiveChanged. URI is empty when no text editor is active (the group is
// empty, or the active tab shows something other than a text).
type ActiveEditor struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId,omitempty"`
}

// Selection is a selection: the anchor is where it began and the active end is where the cursor is. An empty one is a
// cursor.
type Selection struct {
	Anchor Position `json:"anchor"`
	Active Position `json:"active"`
}

// EditorSelection is the payload of TopicEditorSelectionChanged.
type EditorSelection struct {
	URI string `json:"uri"`
	// Selections are all the selections of the editor; the first is the primary one.
	Selections []Selection `json:"selections"`
}

// WorkspaceInfo is the payload of the workspace events and of TopicWindowTabActivated.
type WorkspaceInfo struct {
	Kind   ScopeKind `json:"kind"`
	Name   string    `json:"name"`
	Folder string    `json:"folder,omitempty"`
}

// topicSegment is one segment of a topic.
var topicSegment = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// CheckTopic checks the name of a topic: one or more segments of letters, digits, hyphens and underscores joined by dots.
// The error has the tag EventTopicInvalid.
func CheckTopic(topic string) error {
	for _, seg := range strings.Split(topic, ".") {
		if !topicSegment.MatchString(seg) {
			return errors.New("EventTopicInvalid", "a topic is dotted segments of letters, digits, hyphens and underscores (markdown.preview.open)", "topic", topic)
		}
	}
	return nil
}

// CheckPattern checks a subscription pattern: a topic whose segments may be * (any one segment), and whose last
// segment may be ** (one or more segments, however many). The error has the tag EventPatternInvalid.
func CheckPattern(pattern string) error {
	segs := strings.Split(pattern, ".")
	for i, seg := range segs {
		switch {
		case seg == "*":
		case seg == "**" && i == len(segs)-1:
		case topicSegment.MatchString(seg):
		default:
			return errors.New("EventPatternInvalid", "a pattern is dotted segments of letters, digits, hyphens and underscores, where * stands for one segment and a final ** for the rest (emetos.document.*)", "pattern", pattern)
		}
	}
	return nil
}

// MatchTopic tells whether a topic matches a pattern (CheckPattern). Both must be valid; an invalid one matches nothing.
func MatchTopic(pattern, topic string) bool {
	ps, ts := strings.Split(pattern, "."), strings.Split(topic, ".")
	for i, p := range ps {
		if p == "**" && i == len(ps)-1 {
			return len(ts) > i
		}
		if i >= len(ts) || (p != "*" && p != ts[i]) {
			return false
		}
	}
	return len(ps) == len(ts)
}

// reservedIDs are the ids that Emetos keeps for its own events: no extension has one (the core events are under
// them).
var reservedIDs = map[string]bool{
	"emetos": true, "emetos.document": true, "emetos.editor": true, "emetos.workspace": true,
	"emetos.sharing": true,
}
