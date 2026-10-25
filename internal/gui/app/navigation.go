package app

import (
	"context"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The history of locations and the closed editors of the window (SPEC §10.3): Back, Forward, Go to Last Edit
// Location, Reopen Closed Editor and the recently used editors. The data and the rules are in
// editorgroup.History; this file tells it what happens in the editors (the active editor, the selection, the
// edits, the closes), shows the locations it asks for, and binds the commands.
//
// Ported from VS Code: src/vs/workbench/services/history/browser/historyService.ts

// navState is what the shell keeps of the history.
type navState struct {
	history *editorgroup.History
	// hold counts the code that opens an editor and puts the cursor in it before it tells the history, so
	// that the history sees the editor with its cursor and not the editor first.
	hold int
	// last is the editor the history was told about last, to take the baseline of its selection when it
	// changes.
	last *textEditor
	// batchOpen is true from the first close of a command until the work the UI thread has queued runs: the
	// editors that close meanwhile are one batch, which Reopen Closed Editor brings back together.
	batchOpen bool
	batch     int
	// stopHistory and stopSettings stop following the history and the settings.
	stopHistory, stopSettings func()
}

// editorNav is what the history of locations remembers of one editor: the selection and the version of the
// document the history was told about last, and the reason of the selection change that is being made.
type editorNav struct {
	sel     buffer.Selection
	version int
	// reason is the reason of the change the code sets before it moves the cursors; zero is the user.
	reason editorgroup.SelectionReason
	handle foundation.ListenerHandle
}

// navOpener lets the history show a location and read the active editor.
type navOpener struct{ s *shellState }

// ActivePane implements editorgroup.Opener.
func (o navOpener) ActivePane() editorgroup.Pane { return o.s.activePane() }

// OpenLocation implements editorgroup.Opener.
func (o navOpener) OpenLocation(loc editorgroup.NavLocation, done func()) {
	o.s.openLocation(loc, done)
}

// navigationScope is workbench.editor.navigationScope.
func (s *shellState) navigationScope() editorgroup.NavigationScope {
	win := PeekWorkspace(s.Context())
	return editorgroup.NavigationScope(settings.Get[string](win.Settings, "workbench.editor.navigationScope", ""))
}

// initNavigation makes the history. The grid exists already.
func (s *shellState) initNavigation(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	n := &s.nav
	n.history = editorgroup.NewHistory(navOpener{s}, s.navigationScope())
	n.stopHistory = n.history.OnChange(s.refreshNavKeys)
	n.stopSettings = win.Settings.OnChange(func(e settings.ChangeEvent) {
		if e.Affects("workbench.editor.navigationScope", "") {
			binding.Post(func() {
				if s.Mounted() {
					n.history.SetScope(s.navigationScope())
				}
			})
		}
	})
}

// disposeNavigation stops following the history and the settings.
func (s *shellState) disposeNavigation() {
	n := &s.nav
	if n.stopHistory != nil {
		n.stopHistory()
	}
	if n.stopSettings != nil {
		n.stopSettings()
	}
	n.history = nil
}

// refreshNavKeys sets the context keys that tell what the history can do.
func (s *shellState) refreshNavKeys() {
	if s.nav.history == nil {
		return
	}
	win := PeekWorkspace(s.Context())
	st := s.nav.history.State()
	win.Keys.Batch(func() {
		win.Keys.Set("canNavigateBack", st.CanBack)
		win.Keys.Set("canNavigateForward", st.CanForward)
		win.Keys.Set("canNavigateBackInNavigationLocations", st.CanBackInNavigation)
		win.Keys.Set("canNavigateForwardInNavigationLocations", st.CanForwardInNavigation)
		win.Keys.Set("canNavigateToLastNavigationLocation", st.CanLastNavigation)
		win.Keys.Set("canNavigateBackInEditLocations", st.CanBackInEdits)
		win.Keys.Set("canNavigateForwardInEditLocations", st.CanForwardInEdits)
		win.Keys.Set("canNavigateToLastEditLocation", st.CanLastEdit)
		win.Keys.Set("canReopenClosedEditor", st.CanReopenClosed)
	})
}

// navKeys are the context keys refreshNavKeys sets.
var navKeys = []string{
	"canNavigateBack", "canNavigateForward", "canNavigateBackInNavigationLocations", "canNavigateForwardInNavigationLocations",
	"canNavigateToLastNavigationLocation", "canNavigateBackInEditLocations", "canNavigateForwardInEditLocations",
	"canNavigateToLastEditLocation", "canReopenClosedEditor",
}

// primarySelection is the selection of the main cursor of an editor, as the history keeps it.
func primarySelection(e *textEditor) buffer.Selection { return e.view.Cursors().Primary().Selection }

// paneOf is the editor of a group as the history sees it.
func (s *shellState) paneOf(g *editorgroup.Group, input editorgroup.EditorInput) editorgroup.Pane {
	p := editorgroup.Pane{GroupID: g.ID(), Input: input}
	if e := s.group.editors[input]; e != nil && input != nil {
		sel := primarySelection(e)
		p.SelectionAware = true
		p.Selection = &editorgroup.NavSelection{Anchor: sel.Anchor, Active: sel.Active}
	}
	return p
}

// activePane is the active editor of the active group.
func (s *shellState) activePane() editorgroup.Pane {
	g := s.activeGroup()
	return s.paneOf(g, g.Model.ActiveEditor())
}

// syncNav tells the history which editor is active, when it is another one than the last time. The selection
// the history sees is the one the editor has now; the baseline of the selection changes starts from it.
func (s *shellState) syncNav() {
	n := &s.nav
	if n.history == nil || n.hold > 0 || s.group.grid == nil {
		return
	}
	pane := s.activePane()
	if e := s.group.editors[pane.Input]; e != nil && e != n.last {
		e.nav.sel, e.nav.version = primarySelection(e), e.doc.Version()
	}
	n.last = s.group.editors[pane.Input]
	n.history.ActiveEditorChanged(pane)
}

// watchSelection follows the selection and the edits of an editor, to tell the history when they change.
func (s *shellState) watchSelection(e *textEditor) {
	e.nav.sel, e.nav.version = primarySelection(e), e.doc.Version()
	e.nav.handle = e.view.AddSelectionListener(func() {
		s.selectionNotified(e, editorgroup.ReasonUser)
		if !e.disposed && s.activeEditor() == e {
			s.refreshStatus()
		}
		s.coreSelectionChanged(e)
	})
}

// unwatchSelection stops following the selection of an editor.
func (e *textEditor) unwatchSelection() { e.view.RemoveSelectionListener(e.nav.handle) }

// selectionNotified is told that an editor's selection, focus or document may have changed. The history
// hears of a change of the active editor only: an edit (the document has a new version), or a selection
// that moved with the reason the code gave, else the user's.
func (s *shellState) selectionNotified(e *textEditor, fallback editorgroup.SelectionReason) {
	if e.disposed {
		return
	}
	cur, version := primarySelection(e), e.doc.Version()
	edited, moved := version != e.nav.version, cur != e.nav.sel
	e.nav.sel, e.nav.version = cur, version
	n := &s.nav
	if n.history == nil || n.hold > 0 || (!edited && !moved) || s.group.editors[s.activeGroup().Model.ActiveEditor()] != e {
		return
	}
	reason := fallback
	if e.nav.reason != editorgroup.ReasonNone {
		reason = e.nav.reason
	}
	if edited {
		reason = editorgroup.ReasonEdit
	}
	n.history.SelectionChanged(s.activePane(), reason)
}

// setSelection moves the cursors of an editor for a reason that is not the user's (a jump, a navigation, a
// restored view) and tells the history.
func (s *shellState) setSelection(e *textEditor, sels []buffer.Selection, reason editorgroup.SelectionReason) {
	e.nav.reason = reason
	e.view.Cursors().SetSelections(sels)
	e.view.SelectionsChanged()
	e.nav.reason = editorgroup.ReasonNone
}

// jumpTo moves the cursor of the editor the way go to line does: the place it leaves and the place it goes
// to are both locations of the navigation history.
func (s *shellState) jumpTo(e *textEditor, p buffer.Position) {
	e.nav.reason = editorgroup.ReasonJump
	e.view.GoToPosition(p)
	e.nav.reason = editorgroup.ReasonNone
}

// inputForURI is the input of a group that shows the document of a URI, or nil.
func (s *shellState) inputForURI(g *editorgroup.Group, uri document.URI) editorgroup.EditorInput {
	for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
		if res, ok := in.Resource(); ok && res == uri {
			return in
		}
	}
	return nil
}

// activateEditor makes an editor of a group the active one and the group the active group.
func (s *shellState) activateEditor(g *editorgroup.Group, input editorgroup.EditorInput) {
	g.Model.SetActive(input)
	if err := s.group.grid.Activate(g); err != nil {
		PeekWorkspace(s.Context()).Process.Logger.Error("cannot activate the group", "err", err)
	}
	s.askFocusSoon()
}

// openLocation shows a location of the history: the editor of its file in its group (the active group when
// that is gone), opened again when it was closed, with the cursor where the location says. done tells the
// history that the editor is shown; the file of a closed editor is read on a goroutine first.
func (s *shellState) openLocation(loc editorgroup.NavLocation, done func()) {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	group := s.group.grid.Group(loc.GroupID)
	if group == nil {
		group = s.activeGroup()
	}
	place := func(e *textEditor) {
		if loc.Selection == nil || e.disposed {
			return
		}
		model := edit.NewModel(e.doc.Snapshot())
		sel := buffer.Selection{Anchor: model.ValidatePosition(loc.Selection.Anchor), Active: model.ValidatePosition(loc.Selection.Active)}
		s.setSelection(e, []buffer.Selection{sel}, editorgroup.ReasonProgrammatic)
		binding.Scheduler().ScheduleFrame()
		binding.Scheduler().AddPostFrameCallback(func(time.Duration) {
			if !e.disposed {
				e.view.RevealPositionInCenter(sel.Active)
			}
		})
	}
	if !loc.HasResource {
		if loc.Input != nil && group.Model.Contains(loc.Input) {
			s.activateEditor(group, loc.Input)
		}
		done()
		return
	}
	if input := s.inputForURI(group, loc.Resource); input != nil {
		s.activateEditor(group, input)
		place(s.group.editors[input])
		done()
		return
	}
	if loc.Resource.Scheme != document.SchemeFile {
		done()
		return
	}
	path := loc.Resource.Path
	s.Go(func(life context.Context) {
		doc, err := win.Documents.Acquire(life, path, document.NewOptions())
		binding.Post(func() {
			defer done()
			if !s.Mounted() {
				if err == nil {
					win.Documents.Release(doc)
				}
				return
			}
			if err != nil {
				// the file is gone or cannot be read: there is nothing to show
				win.Process.Logger.Debug("a location of the history cannot be shown", "path", path, "err", describe(err))
				return
			}
			if s.group.grid.Group(group.ID()) != group {
				group = s.activeGroup()
			}
			s.show(binding, doc, openOptions{Preview: true, Group: group, Place: place})
		})
	})
}

// ---- the closed editors ----

// recordClosed remembers the editor that closed in a group, for Reopen Closed Editor: not one that a
// preview replaced or that moved to another group, and not one without a file.
func (s *shellState) recordClosed(g *editorgroup.Group, ch editorgroup.ChangeEvent) {
	n := &s.nav
	if n.history == nil || ch.Context == editorgroup.CloseReplace || ch.Context == editorgroup.CloseMove {
		return
	}
	t, ok := ch.Editor.(*editorgroup.TextInput)
	if !ok {
		return
	}
	uri := t.Document().URI()
	if uri.Scheme != document.SchemeFile {
		return
	}
	var state any
	if e := s.group.editors[ch.Editor]; e != nil {
		state = e.view.ViewState()
	}
	closed := n.history.Closed()
	if !n.batchOpen {
		n.batch, n.batchOpen = closed.NewBatch(), true
		widgets.WidgetsBindingOf(s.Context()).Post(func() { n.batchOpen = false })
	}
	closed.Record(editorgroup.ClosedEditor{
		Resource: uri, GroupID: g.ID(), Index: ch.Index, Sticky: ch.Sticky, Batch: n.batch, ViewState: state,
	})
	s.refreshNavKeys()
}

// forgetClosed drops what the history knows of an editor that closed for good: the editors with no file go
// away with their locations.
func (s *shellState) forgetClosed(g *editorgroup.Group, input editorgroup.EditorInput) {
	h := s.nav.history
	if h == nil {
		return
	}
	h.EditorClosed(g.ID(), input)
	if res, ok := input.Resource(); ok && res.Scheme != document.SchemeFile {
		still := false
		for _, other := range s.group.grid.Groups(editorgroup.CreationTime) {
			if other.Model.ContainsStrict(input) || (s.inputForURI(other, res) != nil && s.inputForURI(other, res) != input) {
				still = true
			}
		}
		if !still {
			h.RemoveResource(res)
		}
	}
	h.InputDisposed(input)
}

// reopenClosed brings back the editors that closed last, together: the files are read on a goroutine, then
// they open in the active group, kept, at the place they had, with the view they had. A batch of which no
// file can be opened (the files are gone) is skipped for the one before it.
//
// Ported from VS Code: src/vs/workbench/services/history/browser/historyService.ts (reopenLastClosedEditor)
func (s *shellState) reopenClosed() {
	h := s.nav.history
	if h == nil {
		return
	}
	batch := h.Closed().TakeLastBatch()
	s.refreshNavKeys()
	if len(batch) == 0 {
		return
	}
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	s.Go(func(life context.Context) {
		docs := make([]*document.Document, len(batch))
		for i, c := range batch {
			if doc, err := win.Documents.Acquire(life, c.Resource.Path, document.NewOptions()); err == nil {
				docs[i] = doc
			}
		}
		binding.Post(func() {
			if !s.Mounted() {
				for _, d := range docs {
					if d != nil {
						win.Documents.Release(d)
					}
				}
				return
			}
			opened := 0
			restore := settings.Get[bool](win.Settings, "workbench.editor.restoreViewState", "")
			for i, c := range batch {
				doc := docs[i]
				if doc == nil {
					continue
				}
				group := s.activeGroup()
				if s.inputFor(group, doc) != nil {
					win.Documents.Release(doc) // it is open in the group already
					continue
				}
				opts := openOptions{Group: group, Sticky: c.Sticky, NoMergeNotice: true}
				// the place it had, unless that is among the other kind of tabs (the pinned ones are first)
				if c.Sticky == group.Model.IsStickyAt(c.Index) {
					opts.Index, opts.HasIndex = min(c.Index, group.Model.Count()), true
				}
				if state, ok := c.ViewState.(editor.ViewState); ok && restore {
					opts.ViewState = &state
				}
				s.showAcquired(binding, doc, opts)
				opened++
			}
			s.SetState(func() { s.blocked = "" })
			s.refreshKeys()
			if opened == 0 && h.Closed().Len() > 0 {
				s.reopenClosed()
			}
		})
	})
}

// followNavigation gives the history the paths the files of a file operation went to, and drops those that
// are gone.
func (s *shellState) followNavigation(res func(path string) (string, bool)) {
	h := s.nav.history
	if h == nil {
		return
	}
	h.Follow(func(u document.URI) (document.URI, bool) {
		if u.Scheme != document.SchemeFile {
			return u, true
		}
		path, gone := res(u.Path)
		if gone {
			return u, false
		}
		return document.FileURI(path), true
	})
}

// forgetDeleted drops the files that another program deleted from the history, when they are really gone.
func (s *shellState) forgetDeleted(gone []string) {
	h := s.nav.history
	if h == nil || len(gone) == 0 {
		return
	}
	h.Follow(func(u document.URI) (document.URI, bool) {
		if u.Scheme != document.SchemeFile {
			return u, true
		}
		for _, g := range gone {
			if isUnder(u.Path, g) && !exists(u.Path) {
				return u, false
			}
		}
		return u, true
	})
}

// isUnder tells whether a path is a folder or a file in it.
func isUnder(path, folder string) bool {
	return path == folder || strings.HasPrefix(path, strings.TrimSuffix(folder, "/")+"/")
}

// ---- the commands ----

// navCommands are the commands of the history: Back, Forward and the other places, the closed editors and
// the editor history.
func (s *shellState) navCommands() map[string]command.Handler {
	h := func() *editorgroup.History { return s.nav.history }
	do := func(f func(*editorgroup.History)) command.Handler {
		return func(command.Context, any) error {
			if hist := h(); hist != nil {
				f(hist)
			}
			return nil
		}
	}
	return map[string]command.Handler{
		"workbench.action.navigateBack":                          do(func(h *editorgroup.History) { h.Back(editorgroup.GoAll) }),
		"workbench.action.navigateForward":                       do(func(h *editorgroup.History) { h.Forward(editorgroup.GoAll) }),
		"workbench.action.navigateLast":                          do(func(h *editorgroup.History) { h.Previous(editorgroup.GoAll) }),
		"workbench.action.navigateBackInEditLocations":           do(func(h *editorgroup.History) { h.Back(editorgroup.GoEdits) }),
		"workbench.action.navigateForwardInEditLocations":        do(func(h *editorgroup.History) { h.Forward(editorgroup.GoEdits) }),
		"workbench.action.navigatePreviousInEditLocations":       do(func(h *editorgroup.History) { h.Previous(editorgroup.GoEdits) }),
		"workbench.action.navigateToLastEditLocation":            do(func(h *editorgroup.History) { h.Last(editorgroup.GoEdits) }),
		"workbench.action.navigateBackInNavigationLocations":     do(func(h *editorgroup.History) { h.Back(editorgroup.GoNavigation) }),
		"workbench.action.navigateForwardInNavigationLocations":  do(func(h *editorgroup.History) { h.Forward(editorgroup.GoNavigation) }),
		"workbench.action.navigatePreviousInNavigationLocations": do(func(h *editorgroup.History) { h.Previous(editorgroup.GoNavigation) }),
		"workbench.action.navigateToLastNavigationLocation":      do(func(h *editorgroup.History) { h.Last(editorgroup.GoNavigation) }),
		"workbench.action.reopenClosedEditor":                    func(command.Context, any) error { s.reopenClosed(); return nil },
		"workbench.action.clearEditorHistory": func(command.Context, any) error {
			s.confirm(confirmSpec{
				Type: kit.DialogWarning, Message: "Do you want to clear the history of recently opened editors?",
				Detail: "This action is irreversible!", Primary: "Clear",
			}, func(confirmed, _ bool) {
				if confirmed && s.Mounted() && s.nav.history != nil {
					s.nav.history.Clear()
					PeekWorkspace(s.Context()).Recent.Clear()
				}
			})
			return nil
		},
	}
}
