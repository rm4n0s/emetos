package app

import (
	"slices"
	"time"

	"github.com/rm4n0s/glutter/scheduler"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// The core events of a workspace scope (SPEC §10.15 "Events", step 11.2): the documents that open, change, save and close,
// the active editor and its selections, the workspace and the window tab. The window publishes them to the scope of its
// extensions; the bus of the scope delivers them on the UI thread, the fast ones (changes, selections) once per frame.

// coreEvents follows the documents and the editors of a workspace and publishes what happens to them.
type coreEvents struct {
	w     *Workspace
	scope *exthost.Scope

	// docs and the fields below are used on the UI thread only.
	docs   map[*document.Document]*trackedDoc
	active extension.ActiveEditor
	// sel is the selections last published for the active editor, so that an unchanged cursor says nothing.
	sel     []buffer.Selection
	selURI  string
	stopped bool
	// activePending says that a look at the active editor is waiting for the UI thread's next turn.
	activePending bool
	stops         []func()
	// tabActive is whether the window tab was the active one at the last change of the tabs.
	tabActive bool
}

// trackedDoc is a document the core events follow.
type trackedDoc struct {
	uri   string // the URI last published for it
	stops []func()
}

// startCoreEvents starts to publish the core events of the workspace to its scope.
func (w *Workspace) startCoreEvents(scope *exthost.Scope) {
	ce := &coreEvents{w: w, scope: scope, docs: map[*document.Document]*trackedDoc{}}
	w.coreEv = ce
	// the documents open already are told as opened; the ones that open and close from now on are followed
	for _, doc := range w.Documents.All() {
		ce.track(doc)
	}
	ce.stops = append(ce.stops,
		w.Documents.WatchOpen(func(doc *document.Document) { w.onUI(func() { ce.track(doc) }) }),
		w.Documents.WatchClose(func(doc *document.Document) { w.onUI(func() { ce.untrack(doc) }) }))
	// the window tab: the event comes when it becomes the active one
	if m := w.Process.Workspaces; m != nil {
		ce.tabActive = w.managed() && w.activeTab()
		if ce.tabActive {
			ce.publish(extension.TopicWindowTabActivated, ce.workspaceInfo())
		}
		ce.stops = append(ce.stops, m.OnChange(ce.tabsChanged))
	}
}

// stopCoreEvents stops following the documents.
func (w *Workspace) stopCoreEvents() {
	ce := w.coreEv
	if ce == nil {
		return
	}
	w.coreEv = nil
	ce.stopped = true
	for _, stop := range ce.stops {
		stop()
	}
	for _, t := range ce.docs {
		for _, stop := range t.stops {
			stop()
		}
	}
	ce.docs = nil
}

// publish sends an event; a failure is logged (the payloads are made here, so it is a bug).
func (ce *coreEvents) publish(topic string, value any) {
	if ce.stopped {
		return
	}
	if err := ce.scope.PublishCore(topic, value); err != nil {
		ce.w.Process.Logger.Error("a core event cannot be published", "topic", topic, "err", err)
	}
}

// workspaceInfo describes the window tab of the scope.
func (ce *coreEvents) workspaceInfo() extension.WorkspaceInfo {
	return extension.WorkspaceInfo(ce.scope.Info()) // the same fields; the JSON tags are the payload's
}

// tabsChanged publishes emetos.windowTab.activated when the tab of the workspace has become the active one.
func (ce *coreEvents) tabsChanged() {
	now := ce.w.managed() && ce.w.activeTab()
	if now && !ce.tabActive {
		ce.publish(extension.TopicWindowTabActivated, ce.workspaceInfo())
	}
	ce.tabActive = now
}

// track follows a document that is open: it is published as opened, and its changes and saves are published until it is
// closed. A document that is closed already, or followed already, is left alone.
func (ce *coreEvents) track(doc *document.Document) {
	if ce.stopped || ce.docs[doc] != nil {
		return
	}
	if cur, ok := ce.w.Documents.Get(doc.URI()); !ok || cur != doc {
		return
	}
	t := &trackedDoc{uri: doc.URI().String()}
	ce.docs[doc] = t
	ce.publish(extension.TopicDocumentOpened, extension.DocumentInfo{URI: t.uri, LanguageID: doc.LanguageID(), Version: doc.Version()})
	previous := ""
	t.stops = append(t.stops,
		doc.OnURIChange(func(old, nu document.URI) {
			previous, t.uri = old.String(), nu.String()
		}),
		doc.Subscribe(func(ev document.ChangeEvent) {
			ce.publish(extension.TopicDocumentChanged, documentChange(t.uri, ev))
		}),
		doc.OnSaved(func(ev document.SavedEvent) {
			ce.publish(extension.TopicDocumentSaved, extension.DocumentSaved{URI: t.uri, Version: doc.Version(), As: ev.As, PreviousURI: previous})
			previous = ""
		}))
}

// untrack stops following a document that was closed and publishes it as closed.
func (ce *coreEvents) untrack(doc *document.Document) {
	t := ce.docs[doc]
	if t == nil {
		return
	}
	delete(ce.docs, doc)
	for _, stop := range t.stops {
		stop()
	}
	ce.publish(extension.TopicDocumentClosed, extension.DocumentClosed{URI: t.uri})
}

// documentChange is the payload of emetos.document.changed for a change of a document.
func documentChange(uri string, ev document.ChangeEvent) extension.DocumentChange {
	cause := "edit"
	switch ev.Cause {
	case document.CauseUndo:
		cause = "undo"
	case document.CauseRedo:
		cause = "redo"
	case document.CauseLineEnding:
		cause = "lineEnding"
	case document.CauseSetText:
		cause = "setText"
	}
	out := extension.DocumentChange{URI: uri, Version: ev.Version, Cause: cause, Changes: make([]extension.TextChange, len(ev.Changes))}
	for i, c := range ev.Changes {
		out.Changes[i] = extension.TextChange{Range: eventRange(c.OldRange), Text: c.NewText}
	}
	return out
}

// eventPosition and eventRange turn the positions of the buffer into those of the events.
func eventPosition(p buffer.Position) extension.Position {
	return extension.Position{Line: p.Line, Col: p.Col}
}

func eventRange(r buffer.Range) extension.Range {
	return extension.Range{Start: eventPosition(r.Start), End: eventPosition(r.End)}
}

// activeEditorChanged publishes emetos.editor.activeChanged when the text editor in front is another one (or none), and the
// selections of the new one, which a subscriber has not seen.
func (ce *coreEvents) activeEditorChanged(e *textEditor) {
	var now extension.ActiveEditor
	if e != nil && !e.disposed {
		now = extension.ActiveEditor{URI: e.doc.URI().String(), LanguageID: e.doc.LanguageID()}
	}
	if now == ce.active {
		return
	}
	ce.active = now
	ce.sel, ce.selURI = nil, ""
	ce.publish(extension.TopicEditorActiveChanged, now)
	if e != nil && !e.disposed {
		ce.selectionChanged(e)
	}
}

// selectionChanged publishes emetos.editor.selectionChanged for the active editor when its selections are not the ones
// last published.
func (ce *coreEvents) selectionChanged(e *textEditor) {
	sels := e.view.Cursors().Selections()
	uri := e.doc.URI().String()
	if uri == ce.selURI && slices.Equal(sels, ce.sel) {
		return
	}
	ce.sel, ce.selURI = slices.Clone(sels), uri
	out := extension.EditorSelection{URI: uri, Selections: make([]extension.Selection, len(sels))}
	for i, s := range sels {
		out.Selections[i] = extension.Selection{Anchor: eventPosition(s.Anchor), Active: eventPosition(s.Active)}
	}
	ce.publish(extension.TopicEditorSelectionChanged, out)
}

// coreActiveChanged tells the core events that the editors may have changed. Which text editor is in front is looked at on
// the UI thread's next turn, not at once: opening several editors in a row (the start, a restore) shows each in turn,
// and a subscriber wants to hear where it ended.
func (s *shellState) coreActiveChanged() {
	win := PeekWorkspace(s.Context())
	ce := win.coreEv
	if ce == nil || ce.activePending {
		return
	}
	ce.activePending = true
	win.onUI(func() {
		ce.activePending = false
		if ce.stopped || !s.Mounted() {
			return
		}
		ce.activeEditorChanged(s.activeEditor())
	})
}

// coreSelectionChanged tells the core events that the selections of the editor changed. Only the active editor counts,
// and only once subscribers were told that it is: the selections of an editor that is coming to the front are published
// right after emetos.editor.activeChanged.
func (s *shellState) coreSelectionChanged(e *textEditor) {
	ce := PeekWorkspace(s.Context()).coreEv
	if ce == nil || e.disposed || s.activeEditor() != e || ce.active.URI != e.doc.URI().String() {
		return
	}
	ce.selectionChanged(e)
}

// extensionFrame runs f after the next frame of the window; a workspace that is not on screen runs it at once. The fast
// events of the extension bus are delivered with it.
func (w *Workspace) extensionFrame(f func()) {
	w.onUI(func() {
		s := w.shell
		if s == nil || !s.Mounted() {
			f()
			return
		}
		afterNextFrame(widgets.WidgetsBindingOf(s.Context()).Scheduler(), f)
	})
}

// afterNextFrame runs f at the end of the frame that is scheduled; when none is, f runs at once, on the UI thread. An event of
// a key that is typed comes while the frame that handles the key is built, and this runs after that frame: asking for another
// frame to deliver it would draw the whole window a second time for every key (a held key repeats faster than a software
// renderer draws two frames), and the events have nothing to draw.
func afterNextFrame(sched *scheduler.SchedulerBinding, f func()) {
	if !sched.HasScheduledFrame() {
		f()
		return
	}
	sched.AddPostFrameCallback(func(time.Duration) { f() })
}

// extensionAfter calls f after d: with the timers of the window while it is on screen, else with a timer of the standard
// library. The timeouts of the requests of extensions use it.
func (w *Workspace) extensionAfter(d time.Duration, f func()) (stop func()) {
	if after := w.after; after != nil {
		return after(d, f)
	}
	t := time.AfterFunc(d, f)
	return func() { t.Stop() }
}
