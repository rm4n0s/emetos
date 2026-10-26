package extension

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
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
	// CrossTab are the topic patterns the extension may receive from, or ask of, other window tabs (SPEC §10.15 "Event
	// sharing"). Only topics within these ever cross a link, and only these are ever asked about: a subscription or a
	// request with a topic outside them fails with EventCrossTabNotDeclared.
	CrossTab []string
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
	//
	// With FromOtherScopes or FromScopes the extension also hears the events of other window tabs, along the links the
	// user allowed (SPEC §10.15 "Event sharing"); the pattern must be among the CrossTab topics of the Manifest or
	// Subscribe fails with EventCrossTabNotDeclared. The events of the own scope are always delivered.
	Subscribe(pattern string, fn func(Event), opts ...SubscribeOption) (cancel func(), err error)
	// Request sends the value to the handler of the topic, which may belong to any extension of the scope, and calls
	// reply once, on the UI thread, with the reply of the handler or an error: EventNoHandler when nothing handles the
	// topic, EventTimeout when the handler does not answer in time (when ctx has a deadline, then at the deadline, else
	// after 10 seconds), EventCanceled when ctx is cancelled, EventHandlerFailed with the error the handler returned,
	// EventHandlerPanicked when it panicked, EventHandlerGone when its extension was deactivated before it answered. It
	// never blocks. It fails at once, without calling reply, with EventTopicInvalid or EventValueInvalid; reply is not
	// called when the requesting extension was deactivated.
	//
	// With ToOtherScopes or ToScopes the handler is looked for in those window tabs, not in the own scope, and the
	// request goes along the link that lets that tab's events reach this one (SPEC §10.15 "Event sharing"); the topic
	// must be among the CrossTab topics of the Manifest (EventCrossTabNotDeclared). When no link exists the user is
	// asked once; the reply may then carry EventSharingDenied, EventSharingRefused, EventSharingPrivate or
	// EventSharingRevoked.
	Request(ctx context.Context, topic string, value any, reply func(Event, error), opts ...RequestOption) error
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
	// TopicFilesChanged: files of the folder of the workspace were created, changed, deleted or renamed, as the watcher
	// of the folder saw it, or the list of files that Workspace().Files gives changed. Payload FilesChanged. It is not
	// published for a workspace with no folder.
	TopicFilesChanged = "emetos.files.changed"
	// TopicSharingRevoked: a link that let the events of another window tab reach this one was removed, or that tab
	// became private. It is published in the receiving tab. Payload SharingRevoked.
	TopicSharingRevoked = "emetos.sharing.revoked"
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

// The kinds of FileChange.
const (
	// FileCreated: a new file. FileChanged: a file whose content changed. FileDeleted: a file or a folder that is gone.
	FileCreated = "created"
	FileChanged = "changed"
	FileDeleted = "deleted"
	// FileRenamed: an item moved from OldPath to Path.
	FileRenamed = "renamed"
	// FileRescan: events were lost below Path (or a folder was deleted or renamed): read everything below it again.
	FileRescan = "rescan"
	// FileListed: the list of files that Workspace().Files gives changed (the walk of the folder found more files or
	// ended): read it again. Path is empty.
	FileListed = "listed"
)

// FileChange is one change of a file or folder of the workspace.
type FileChange struct {
	// Kind is one of FileCreated, FileChanged, FileDeleted, FileRenamed, FileRescan and FileListed.
	Kind string `json:"kind"`
	// Path is the path relative to the folder, with forward slashes ("." for the folder itself).
	Path string `json:"path,omitempty"`
	// OldPath is where a renamed item was, in the same form.
	OldPath string `json:"oldPath,omitempty"`
}

// FilesChanged is the payload of TopicFilesChanged: what the watcher reported in one go, at most MaxFileChanges of it
// (a larger batch is one FileRescan of ".").
type FilesChanged struct {
	Changes []FileChange `json:"changes"`
}

// MaxFileChanges is the most changes a FilesChanged carries.
const MaxFileChanges = 500

// WorkspaceInfo is the payload of the workspace events and of TopicWindowTabActivated.
type WorkspaceInfo struct {
	Kind   ScopeKind `json:"kind"`
	Name   string    `json:"name"`
	Folder string    `json:"folder,omitempty"`
	// WindowTab is the id of the kind of an extension's window tab, empty for a workspace.
	WindowTab string `json:"windowTab,omitempty"`
	// ID names the scope in the links between window tabs (ScopeInfo.ID); empty for a tab that has no folder.
	ID string `json:"id,omitempty"`
}

// The reasons of a SharingRevoked.
const (
	// RevokedLinkRemoved: the user removed the link (Extensions > Event Sharing).
	RevokedLinkRemoved = "removed"
	// RevokedPrivate: the tab the events came from was made private.
	RevokedPrivate = "private"
)

// SharingRevoked is the payload of TopicSharingRevoked: the events of the scope From no longer reach this tab.
type SharingRevoked struct {
	// From is the ID of the scope the events came from, FromName its name.
	From     string `json:"from"`
	FromName string `json:"fromName,omitempty"`
	// Topics are the patterns the link carried; empty when it carried every topic.
	Topics []string `json:"topics,omitempty"`
	// Reason is RevokedLinkRemoved or RevokedPrivate.
	Reason string `json:"reason"`
}

// ScopeFilter says which other window tabs an operation reaches: all of them, or the ones with the listed IDs
// (ScopeInfo.ID). The zero value reaches none.
type ScopeFilter struct {
	// Others reaches every other window tab.
	Others bool
	// IDs reaches the window tabs with these IDs.
	IDs []string
}

// Any tells whether the filter reaches any other scope at all.
func (f ScopeFilter) Any() bool { return f.Others || len(f.IDs) > 0 }

// Matches tells whether the filter reaches the scope with the id.
func (f ScopeFilter) Matches(id string) bool {
	return id != "" && (f.Others || slices.Contains(f.IDs, id))
}

// SubscribeOptions are what the SubscribeOption values set.
type SubscribeOptions struct {
	// From are the other scopes whose events the subscription hears too.
	From ScopeFilter
}

// SubscribeOption changes a Subscribe: FromOtherScopes, FromScopes.
type SubscribeOption func(*SubscribeOptions)

// FromOtherScopes makes a subscription hear the events of every other window tab that is linked to this one (SPEC
// §10.15 "Event sharing"), besides those of its own.
func FromOtherScopes() SubscribeOption { return func(o *SubscribeOptions) { o.From.Others = true } }

// FromScopes makes a subscription hear the events of the window tabs with these IDs (ScopeInfo.ID, Event.Scope.ID) that
// are linked to this one, besides those of its own.
func FromScopes(ids ...string) SubscribeOption {
	return func(o *SubscribeOptions) { o.From.IDs = append(o.From.IDs, ids...) }
}

// RequestOptions are what the RequestOption values set.
type RequestOptions struct {
	// To are the other scopes whose handler answers; the zero value is the own scope.
	To ScopeFilter
}

// RequestOption changes a Request: ToOtherScopes, ToScopes.
type RequestOption func(*RequestOptions)

// ToOtherScopes sends a request to the handler of its topic in another window tab, the first (in the order the tabs
// were opened) that has one and is linked to this one; the own scope is not looked at.
func ToOtherScopes() RequestOption { return func(o *RequestOptions) { o.To.Others = true } }

// ToScopes sends a request to the handler of its topic in one of the window tabs with these IDs.
func ToScopes(ids ...string) RequestOption {
	return func(o *RequestOptions) { o.To.IDs = append(o.To.IDs, ids...) }
}

// ReadOptions are what the ReadOption values set.
type ReadOptions struct {
	// Scope is the ID of the window tab to read from; empty is the own scope.
	Scope string
}

// ReadOption changes a Documents().Read: FromScope.
type ReadOption func(*ReadOptions)

// FromScope reads the document from the window tab with the ID (ScopeInfo.ID) instead of the own one. It needs a link
// that lets the document events of that tab (TopicDocumentChanged) reach this one, and the tab must not be private; the
// Manifest must list TopicDocumentChanged among its CrossTab topics. The user is not asked: a read without a link fails
// with ExtensionReadNotShared.
func FromScope(id string) ReadOption { return func(o *ReadOptions) { o.Scope = id } }

// PatternWithin tells whether every topic that pattern matches is matched by one of the patterns in list: the pattern is
// within the list. A topic is a pattern that matches itself. Invalid patterns are within nothing.
func PatternWithin(pattern string, list []string) bool {
	if CheckPattern(pattern) != nil {
		return false
	}
	return slices.ContainsFunc(list, func(outer string) bool { return patternWithin(pattern, outer) })
}

// patternWithin tells whether the pattern matches no topic that outer does not match.
func patternWithin(pattern, outer string) bool {
	if CheckPattern(outer) != nil {
		return false
	}
	ps, outerSegs := strings.Split(pattern, "."), strings.Split(outer, ".")
	for i, o := range outerSegs {
		if o == "**" && i == len(outerSegs)-1 {
			return len(ps) > i
		}
		if i >= len(ps) || ps[i] == "**" {
			return false
		}
		if o != "*" && ps[i] != o {
			return false
		}
	}
	return len(ps) == len(outerSegs)
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
	"emetos.sharing": true, "emetos.files": true,
}
