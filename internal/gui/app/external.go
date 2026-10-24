package app

import (
	"context"
	"path/filepath"
	"time"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// Files changed on disk (SPEC §3.1 "Files", §10.5). The watcher's batches of events reach the open documents
// here, after they were applied to the tree: a document whose file was renamed follows it; a document whose file
// an event touches is looked at (document.ExternalJob): the file is read on a goroutine, and what it found is
// applied on the UI thread. A clean document reloads and its editors keep their cursors, scroll position and
// folds; a dirty one is left alone and gets one sticky notification with Overwrite and Revert; a deleted file
// leaves the document open, struck through in its tab, and a save makes the file again.
//
// TODO(step 9.6): the "Compare" button of the notification opens the diff editor of the document and the file.
//
// Ported from VS Code: src/vs/workbench/services/textfile/common/textFileEditorModelManager.ts and
// textFileEditorModel.ts (onDidFilesChange), src/vs/workbench/services/textfile/browser/textFileService.ts (the
// decoration of a deleted editor) and src/vs/workbench/services/editor/browser/editorService.ts (handleMovedFile)

// externalRetryDelays are the waits before a look at a file that could not be read or was being written is made
// again by itself; after them only the next event of the watcher makes it.
var externalRetryDelays = []time.Duration{300 * time.Millisecond, 2 * time.Second}

// externalMaxStale bounds the looks made again at once because the document changed meanwhile.
const externalMaxStale = 3

// externalRun is a look at the file of a document that is going on.
type externalRun struct {
	// again is set when another look was asked for meanwhile: it is made when this one ends.
	again bool
}

// externalState is what the shell keeps of the files changed on disk.
type externalState struct {
	// running are the looks that are going on, one per document.
	running map[*document.Document]*externalRun
	// deferred are the documents whose look waits for a save to end.
	deferred map[*document.Document]bool
	// notes are the ids of the notifications of the documents that are conflicted (also in saveState.notes).
	notes map[*document.Document]uint64
	// tracked are the functions that stop following each document.
	tracked map[*document.Document][]func()
	// retries are the timers of the looks that are made again, and attempts how many were made.
	retries  map[*document.Document]func()
	attempts map[*document.Document]int
}

// initExternal makes the shell ready to follow the events of the watcher for the open documents.
//
// The events come from externalFileEvents (the Explorer's listener of the window), which calls externalEvents before
// it lets the history forget the files that are gone, so that a renamed file is followed first.
func (s *shellState) initExternal() {
	ex := &s.ext
	ex.running = map[*document.Document]*externalRun{}
	ex.deferred = map[*document.Document]bool{}
	ex.notes = map[*document.Document]uint64{}
	ex.tracked = map[*document.Document][]func(){}
	ex.retries = map[*document.Document]func(){}
	ex.attempts = map[*document.Document]int{}
}

// disposeExternal stops following the documents and drops the timers.
func (s *shellState) disposeExternal() {
	ex := &s.ext
	for _, stop := range ex.retries {
		stop()
	}
	ex.retries = nil
	for doc := range ex.tracked {
		s.externalUntrack(doc)
	}
}

// externalTrack makes the shell follow the external state of the document of a new editor: a conflict shows its
// notification, and a document that is clean again after a conflict is looked at again.
func (s *shellState) externalTrack(doc *document.Document) {
	ex := &s.ext
	if ex.tracked == nil || doc.URI().Scheme != document.SchemeFile {
		return
	}
	if _, ok := ex.tracked[doc]; ok {
		return
	}
	ex.tracked[doc] = []func(){
		doc.OnExternalStateChange(func(st document.ExternalState) { s.externalStateChanged(doc, st) }),
		// the changes of a conflicted document that were undone leave it clean: it has nothing to lose, so it
		// takes the file
		doc.Subscribe(func(document.ChangeEvent) {
			if doc.Conflicted() && !doc.Dirty() && s.Mounted() {
				binding := widgets.WidgetsBindingOf(s.Context())
				binding.Post(func() {
					if s.Mounted() && doc.Conflicted() && !doc.Dirty() {
						s.checkDocument(doc)
					}
				})
			}
		}),
	}
	if doc.Conflicted() {
		s.externalStateChanged(doc, doc.ExternalState())
	}
}

// externalUntrack lets go of a document that was closed.
func (s *shellState) externalUntrack(doc *document.Document) {
	ex := &s.ext
	for _, stop := range ex.tracked[doc] {
		stop()
	}
	delete(ex.tracked, doc)
	s.dismissExternalNote(doc)
	delete(ex.running, doc)
	delete(ex.deferred, doc)
	delete(ex.attempts, doc)
	if stop := ex.retries[doc]; stop != nil {
		stop()
		delete(ex.retries, doc)
	}
}

// ---- events ----

// externalEvents handles a batch of events of the watcher for the open documents, on the UI thread: the documents
// whose file was renamed follow it, and every document a path of an event touches is looked at.
func (s *shellState) externalEvents(events []workspace.Event) {
	win := PeekWindow(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	if moves := renamesOf(events); len(moves) > 0 {
		s.followPaths(binding, followMoves(moves))
	}
	for _, doc := range win.Documents.All() {
		if doc == nil || doc.URI().Scheme != document.SchemeFile {
			continue
		}
		if eventsTouch(events, doc.URI().Path) {
			s.checkDocument(doc)
		}
	}
}

// renamesOf are the moves the renames of a batch of events make, in order.
func renamesOf(events []workspace.Event) []workspace.PathMove {
	var moves []workspace.PathMove
	for _, e := range events {
		if e.Kind == workspace.EventRenamed && e.OldPath != "" && e.Path != "" && e.OldPath != e.Path {
			moves = append(moves, workspace.PathMove{Old: e.OldPath, New: e.Path})
		}
	}
	return moves
}

// followMoves gives the path an item has after the moves, one after the other (a file renamed twice in a batch
// goes to its last name). Nothing is ever gone: an item moved out of the watched folders is reported as deleted
// by the watcher, and that is looked at, not followed.
func followMoves(moves []workspace.PathMove) func(path string) (string, bool) {
	return func(path string) (string, bool) {
		for _, m := range moves {
			if path == m.Old {
				path = m.New
			} else if isBelow(path, m.Old) {
				path = filepath.Join(m.New, path[len(m.Old)+1:])
			}
		}
		return path, false
	}
}

// eventsTouch tells whether an event of the batch says something about a file: a change of the file itself, or a
// creation, a deletion, a rename or a rescan of the file or of a folder above it.
func eventsTouch(events []workspace.Event, path string) bool {
	for _, e := range events {
		switch e.Kind {
		case workspace.EventChanged:
			if e.Path == path {
				return true
			}
		case workspace.EventCreated, workspace.EventDeleted, workspace.EventRescan, workspace.EventRenamed:
			for _, p := range []string{e.Path, e.OldPath} {
				if p != "" && (p == path || isBelow(path, p)) {
					return true
				}
			}
		}
	}
	return false
}

// ---- the look ----

// checkDocument looks at the file of a document after something may have changed it: on a goroutine the file is
// statted and, when it differs from what the document has and the document has no unsaved changes, read; the
// result is applied on the UI thread (document.ApplyExternal). A look that is going on makes the next one wait,
// and a save that is going on makes it wait for the save: the file is being written then.
func (s *shellState) checkDocument(doc *document.Document) { s.checkDocumentAttempt(doc, 0) }

func (s *shellState) checkDocumentAttempt(doc *document.Document, stale int) {
	ex := &s.ext
	if !s.Mounted() || ex.running == nil || doc.URI().Scheme != document.SchemeFile {
		return
	}
	win := PeekWindow(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	if stop := ex.retries[doc]; stop != nil {
		stop()
		delete(ex.retries, doc)
	}
	if run := ex.running[doc]; run != nil {
		run.again = true
		return
	}
	if doc.SaveInFlight() {
		ex.deferred[doc] = true
		return
	}
	job, err := doc.BeginExternal(fileio.ReadOptions{DefaultLineEnding: doc.LineEnding()})
	if err != nil {
		win.Process.Logger.Debug("a document cannot be looked at", "uri", doc.URI().String(), "err", err)
		return
	}
	run := &externalRun{}
	ex.running[doc] = run
	s.Go(func(life context.Context) {
		probe := job.Probe(life, nil)
		binding.Post(func() {
			if s.Mounted() {
				s.finishExternal(binding, doc, job, probe, run, stale)
			}
		})
	})
}

// finishExternal applies what a look found, on the UI thread.
func (s *shellState) finishExternal(binding *widgets.WidgetsBinding, doc *document.Document, job *document.ExternalJob, probe document.ExternalProbe, run *externalRun, stale int) {
	ex := &s.ext
	win := PeekWindow(s.Context())
	if ex.running[doc] == run {
		delete(ex.running, doc)
	}
	if open, ok := win.Documents.Get(doc.URI()); !ok || open != doc {
		return // closed meanwhile
	}
	var views []viewSnap
	if probe.Read != nil {
		views = s.captureViews(doc)
	}
	res := doc.ApplyExternal(job, probe)
	switch res.Outcome {
	case document.ExternalReloaded:
		win.Process.Logger.Debug("a file changed on disk and was read again", "uri", doc.URI().String())
		ex.attempts[doc] = 0
		s.documentReloaded(binding, doc, views, false)
	case document.ExternalOrphaned:
		ex.attempts[doc] = 0
		if res.StateChanged && settings.Get[bool](win.Settings, "workbench.editor.closeOnFileDelete", "") {
			// VS Code keeps the editor of a file that was deleted open unless this setting is on; an editor with
			// unsaved changes never closes
			s.closeCleanEditors(doc)
		}
	case document.ExternalStale:
		if stale < externalMaxStale {
			run.again = false
			s.checkDocumentAttempt(doc, stale+1)
			return
		}
	case document.ExternalDeferred:
		ex.deferred[doc] = true
	case document.ExternalRetry:
		if res.Err != nil {
			win.Process.Logger.Warn("a file changed on disk but cannot be read", "uri", doc.URI().String(), "err", res.Err)
		}
		s.retryExternal(doc)
	default:
		ex.attempts[doc] = 0
	}
	if run.again {
		s.checkDocument(doc)
	}
}

// retryExternal makes the look again after a short wait, a few times: a file that was being written is finished by
// then, and one that could not be read may be readable. After the last wait the next event of the watcher is the
// one to act on.
func (s *shellState) retryExternal(doc *document.Document) {
	ex := &s.ext
	win := PeekWindow(s.Context())
	n := ex.attempts[doc]
	if win.after == nil || n >= len(externalRetryDelays) || ex.retries[doc] != nil {
		return
	}
	ex.attempts[doc] = n + 1
	ex.retries[doc] = win.after(externalRetryDelays[n], func() {
		delete(ex.retries, doc)
		if s.Mounted() {
			s.checkDocumentAttempt(doc, 0)
		}
	})
}

// saveSettled is told when the save of a document ended (it succeeded or failed): a look that waited for it is made.
func (s *shellState) saveSettled(doc *document.Document) {
	ex := &s.ext
	if ex.deferred[doc] {
		delete(ex.deferred, doc)
		s.checkDocument(doc)
	}
}

// ---- the state of a document ----

// externalStateChanged follows the document becoming orphaned or conflicted, or being mended: the conflict shows
// its notification (or takes it away), and the window draws its tabs and keys again.
func (s *shellState) externalStateChanged(doc *document.Document, st document.ExternalState) {
	if !s.Mounted() {
		return
	}
	if st.Conflicted {
		s.showExternalNote(doc)
	} else {
		s.dismissExternalNote(doc)
	}
	s.refreshKeys()
	s.refreshStatus()
	s.SetState(func() {})
}

// showExternalNote tells the user that the file of a document with unsaved changes changed on disk: one sticky
// notification for the document, in place of the one of a failed save, with Overwrite (save over the file) and
// Revert (drop the unsaved changes and read the file). A save of the document that fails as a conflict later
// replaces it with its own, which has the same buttons.
func (s *shellState) showExternalNote(doc *document.Document) {
	if s.saves.notes == nil {
		return // the shell is not made yet: initSave makes the notes
	}
	win := PeekWindow(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	s.dismissSaveNote(doc)
	name := doc.URI().Title()
	id := win.Process.Notify.Notify(notify.Notification{
		Severity: notify.Warning, Source: "Emetos", Sticky: true,
		Message: "The file '" + name + "' has changed on disk and you have unsaved changes.",
		Actions: []notify.Action{
			{Label: "Overwrite", Run: func() {
				if s.Mounted() {
					s.saveDocument(doc, saveRequest{ignoreConflict: true}, nil)
				}
			}},
			{Label: "Revert", Run: func() {
				if s.Mounted() {
					s.revertDocument(binding, doc, nil)
				}
			}},
			// TODO(step 9.6): a "Compare" button opens the diff editor of the document and the file on disk.
		},
	})
	s.saves.notes[doc] = id
	s.ext.notes[doc] = id
}

// dismissExternalNote takes the notification of a conflict away, unless another one of the document took its place.
func (s *shellState) dismissExternalNote(doc *document.Document) {
	ex := &s.ext
	id, ok := ex.notes[doc]
	if !ok {
		return
	}
	delete(ex.notes, doc)
	if s.saves.notes[doc] == id {
		s.dismissSaveNote(doc)
	}
}

// ---- view state ----

// viewSnap is the view state of the editor of a document, taken before its text is replaced.
type viewSnap struct {
	ed    *textEditor
	state editor.ViewState
}

// captureViews takes the view state (cursors, scroll position, folds) of the editors that show a document.
func (s *shellState) captureViews(doc *document.Document) []viewSnap {
	var out []viewSnap
	for _, e := range s.group.editors {
		if e.doc == doc && !e.disposed {
			out = append(out, viewSnap{ed: e, state: e.view.ViewState()})
		}
	}
	return out
}
