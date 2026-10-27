package app

import (
	"context"
	"os"
	"slices"
	"time"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// The editor groups of the window (SPEC §10.3): a grid of groups, each with its tab strip, its
// breadcrumbs and the editor of its active tab. The data is an editorgroup.Grid whose groups hold
// editorgroup.Models; this file keeps an editor (textEditor, with a view model of its own) for each
// text input of every group and follows the events of the grid. The widgets are in gridview.go, the
// commands in groupcommands.go.

// groupState is what the shell keeps of its editor groups.
type groupState struct {
	grid *editorgroup.Grid
	// editors are the editors of the inputs of all the groups. The input of an editor that moves to
	// another group is the same one, and so is its editor.
	editors map[editorgroup.EditorInput]*textEditor
	// panes are the editors of the inputs that are not text (panes.go); an input has an editor or a pane.
	panes map[editorgroup.EditorInput]pane
	// views hold what the widgets of a group need to keep from build to build.
	views map[*editorgroup.Group]*groupView
	// focused tells which editors have the keyboard focus (the editor or its find widget).
	focused map[*textEditor]bool
	// stop follows the grid, and stopSettings the settings.
	stop, stopSettings func()
	// removeCommands take the handlers of the group commands off the registry.
	removeCommands []func()
	// focusAsked is whether a request to give the active group the keyboard focus waits for the next
	// frame.
	focusAsked bool
	// drag is the drag of a tab that goes on, or nil; dragFeed tells the layer that shows it to build
	// again (tabdrag.go).
	drag     *tabDrag
	dragFeed *dragFeed
}

// openOptions say how a document is shown in a group.
type openOptions struct {
	// Preview opens the editor as the preview editor, which the next preview replaces, when
	// workbench.editor.enablePreview is on; a navigation (the breadcrumbs, later the explorer) asks
	// for it, the command line and the other commands do not.
	Preview bool
	// Group is the group to show the document in; the active group when nil. The group becomes the
	// active group.
	Group *editorgroup.Group
	// Place, when set, runs once the editor exists (to put the cursor somewhere); the editor then
	// scrolls to the cursor.
	Place func(*textEditor)
	// Sticky opens the editor pinned (a tab with the pin icon), and Index, when HasIndex is set, at a place
	// of the tabs.
	Sticky   bool
	Index    int
	HasIndex bool
	// ViewState, when set, is given back to the editor: the cursors, the scroll position and the folds it had
	// when it was closed.
	ViewState *editor.ViewState
	// NoMergeNotice leaves out the notification that offers the merge editor for a file with conflict markers: an
	// editor that is restored or that comes back from the closed ones is not an open of the file.
	NoMergeNotice bool
	// Inactive opens the editor without making it the active one, and its group without making it the active group: the
	// editors a refactoring opened for the files it changed (VS Code leaves those inactive too).
	Inactive bool
	// FileView, when set, is the id of the file view the editor shows (an editor that is open already is switched to it);
	// otherwise a new editor gets the view workbench.editor.defaultFileViews names for its file.
	FileView string
}

// gridOptions are the settings the grid follows.
func (s *shellState) gridOptions() editorgroup.GridOptions {
	win := PeekWorkspace(s.Context())
	return editorgroup.GridOptions{
		Model: editorgroup.Options{
			OpenPositioning:             editorgroup.OpenPositioning(settings.Get[string](win.Settings, "workbench.editor.openPositioning", "")),
			FocusRecentEditorAfterClose: settings.Get[bool](win.Settings, "workbench.editor.focusRecentEditorAfterClose", ""),
		},
		CloseEmptyGroups: settings.Get[bool](win.Settings, "workbench.editor.closeEmptyGroups", ""),
		SplitSizing:      editorgroup.SplitSizing(settings.Get[string](win.Settings, "workbench.editor.splitSizing", "")),
	}
}

// sideBySideDirection is the side a split opens on (`workbench.editor.openSideBySideDirection`).
func (s *shellState) sideBySideDirection() editorgroup.Direction {
	win := PeekWorkspace(s.Context())
	if settings.Get[string](win.Settings, "workbench.editor.openSideBySideDirection", "") == "down" {
		return editorgroup.DirDown
	}
	return editorgroup.DirRight
}

// initGroup makes the grid, which starts with one group or with the groups and editors the window had
// (PrepareRestore, SPEC §10.10), and the editors for the documents the window starts with: the first is the
// active editor, and all are kept (not previews), as VS Code opens the files of the command line.
func (s *shellState) initGroup(binding *widgets.WidgetsBinding, docs []*document.Document) {
	win := PeekWorkspace(s.Context())
	g := &s.group
	// the groups the window had are made again from the saved state; the documents of the command line open on
	// top of them
	from := win.takeRestored()
	restored := s.restoreGrid(from)
	fromState := restored != nil
	if restored == nil {
		restored = editorgroup.NewGrid(s.gridOptions())
	}
	g.grid = restored
	g.editors = map[editorgroup.EditorInput]*textEditor{}
	g.panes = map[editorgroup.EditorInput]pane{}
	g.views = map[*editorgroup.Group]*groupView{}
	g.focused = map[*textEditor]bool{}
	g.dragFeed = newDragFeed()
	s.initNavigation(binding)
	g.stop = g.grid.OnChange(func(e editorgroup.GridEvent) { s.gridChanged(binding, e) })
	g.stopSettings = win.Settings.OnChange(func(e settings.ChangeEvent) {
		if e.Affects("workbench.editor", "") || e.Affects("breadcrumbs", "") || e.Affects("zenMode", "") {
			binding.Post(func() {
				if !s.Mounted() {
					return
				}
				s.group.grid.SetOptions(s.gridOptions())
				s.refreshKeys()
				s.SetState(func() {})
			})
		}
	})
	if fromState {
		s.restoreEditors(binding)
	}
	if from != nil {
		s.showRestoredBackups(binding, from.extra)
	}
	for _, doc := range docs {
		win.Documents.Retain(doc)
		opts := openOptions{}
		if st := win.startup; st != nil {
			for _, t := range st.Files {
				if t.Path == doc.URI().Path {
					opts.Place = placeAt(doc, t.Line, t.Column)
				}
			}
		}
		s.showAcquired(binding, doc, opts)
	}
	// the first document is the one that is shown
	if len(docs) > 1 {
		if first := s.inputFor(s.activeGroup(), docs[0]); first != nil {
			s.activeGroup().Model.SetActive(first)
		}
	}
	s.refreshKeys()
}

// showRestoredBackups opens the documents that came back from backups and that no saved editor shows in the active
// group, as editors that are kept; the active editor stays the one that was. The shell is not on screen yet.
func (s *shellState) showRestoredBackups(binding *widgets.WidgetsBinding, docs []*document.Document) {
	if len(docs) == 0 {
		return
	}
	win := PeekWorkspace(s.Context())
	group := s.activeGroup()
	was := group.Model.ActiveEditor()
	for _, doc := range docs {
		win.Documents.Retain(doc)
		s.showAcquired(binding, doc, openOptions{Group: group, NoMergeNotice: true})
	}
	if was != nil && group.Model.Contains(was) {
		group.Model.SetActive(was)
	}
}

// activeGroup is the active group.
func (s *shellState) activeGroup() *editorgroup.Group { return s.group.grid.Active() }

// groupAt is the group at a place of the grid, counted from 1, or nil.
func (s *shellState) groupAt(index int) *editorgroup.Group {
	groups := s.group.grid.Groups(editorgroup.GridAppearance)
	if index < 1 || index > len(groups) {
		return nil
	}
	return groups[index-1]
}

// inputFor is the input of a group that shows a document, or nil.
func (s *shellState) inputFor(group *editorgroup.Group, doc *document.Document) editorgroup.EditorInput {
	for _, e := range group.Model.Editors(editorgroup.Sequential, false) {
		if t, ok := e.(*editorgroup.TextInput); ok && t.Document() == doc {
			return e
		}
	}
	return nil
}

// activeEditor is the editor of the active tab of the active group, or nil when the group is empty.
func (s *shellState) activeEditor() *textEditor {
	if s.group.grid == nil {
		return nil
	}
	return s.group.editors[s.activeGroup().Model.ActiveEditor()]
}

// editorCount is the number of editors in all the groups.
func (s *shellState) editorCount() int {
	n := 0
	for _, g := range s.group.grid.Groups(editorgroup.CreationTime) {
		n += g.Model.Count()
	}
	return n
}

// inputOptions are what the inputs of the window need to describe their files.
func (s *shellState) inputOptions() editorgroup.TextInputOpts {
	win := PeekWorkspace(s.Context())
	opts := editorgroup.TextInputOpts{Folder: win.Folder}
	if home, err := os.UserHomeDir(); err == nil {
		opts.Home = home
	}
	return opts
}

// showAcquired opens a document, which the caller has acquired (or retained) once, in a group (the
// active one by default) and makes it the active editor of the group, and the group the active
// group. A document that the group shows already is activated (and kept, when it is not opened as a
// preview). The use of the document that the caller counted is always given back: the editor that
// shows it counts its own. The new editor is a preview editor only when asked for and
// workbench.editor.enablePreview is on; a document that has unsaved changes is kept.
func (s *shellState) showAcquired(binding *widgets.WidgetsBinding, doc *document.Document, opts openOptions) {
	win := PeekWorkspace(s.Context())
	group := opts.Group
	if group == nil {
		group = s.activeGroup()
	}
	defer win.Documents.Release(doc)
	preview := opts.Preview && settings.Get[bool](win.Settings, "workbench.editor.enablePreview", "") && !doc.Dirty()
	if path, ok := filePath(doc.URI().Scheme, doc.URI().Path); ok {
		win.AddRecentFile(path) // the quick open shows the files that were opened first
	}
	input := editorgroup.NewTextInput(doc, s.inputOptions())
	// The history hears of the editor once it has its cursor: a view that comes back or a place to show.
	s.nav.hold++
	shown, isNew := group.Model.OpenEditor(input, editorgroup.OpenOptions{
		Pinned: !preview, Active: !opts.Inactive, Sticky: opts.Sticky, Index: opts.Index, HasIndex: opts.HasIndex,
	})
	if !isNew {
		input.Dispose()
	} else if !opts.NoMergeNotice {
		s.noticeMergeConflicts(doc)
	}
	// opening an editor makes its group the active one
	if !opts.Inactive {
		if err := s.group.grid.Activate(group); err != nil {
			win.Process.Logger.Error("cannot activate the group", "err", err)
		}
	}
	e := s.group.editors[shown]
	if e != nil {
		switch {
		case opts.FileView != "":
			if slices.ContainsFunc(s.fileViewsFor(e), func(v exthost.FileViewInfo) bool { return v.ID == opts.FileView }) {
				e.fileView = opts.FileView
			}
		case isNew:
			s.applyDefaultFileView(e)
		}
	}
	if e != nil && opts.ViewState != nil {
		if err := e.view.RestoreViewState(*opts.ViewState); err != nil {
			win.Process.Logger.Debug("not all of the view of the editor could come back", "err", describe(err))
		}
		s.setSelection(e, e.view.Cursors().Selections(), editorgroup.ReasonProgrammatic)
	}
	if e != nil && opts.Place != nil {
		opts.Place(e)
	}
	s.nav.hold--
	s.syncNav()
	if e != nil && (opts.Place != nil || opts.ViewState != nil) {
		// the editor was shown already, but the cursor moved: that is a change of its selection
		s.selectionNotified(e, editorgroup.ReasonProgrammatic)
	}
	if opts.Place == nil || e == nil {
		return
	}
	binding.Scheduler().AddPostFrameCallback(func(time.Duration) {
		if !e.disposed {
			e.view.RevealPrimary()
		}
	})
}

// show is showAcquired for code that opens a document in a callback of the UI thread: it asks the
// shell to rebuild.
func (s *shellState) show(binding *widgets.WidgetsBinding, doc *document.Document, opts openOptions) {
	s.SetState(func() {
		s.blocked = ""
		s.showAcquired(binding, doc, opts)
	})
	s.refreshKeys()
}

// tabsChanged tells the strip of workspace tabs, after the frame that is going on, that what it shows of this workspace
// (whether a document has unsaved changes) may have changed.
func (s *shellState) tabsChanged(binding *widgets.WidgetsBinding) {
	m := PeekWorkspace(s.Context()).Process.Workspaces
	binding.Post(func() {
		if s.Mounted() {
			m.changed()
		}
	})
}

// gridChanged follows the grid: an editor that opens gets its editor (a split or a copy opens
// editors the shell did not ask for), one that closes lets go of its editor and its document, anything
// else rebuilds the groups and updates the context keys. Typing in a preview editor keeps it. A new
// active group gets the keyboard focus.
func (s *shellState) gridChanged(binding *widgets.WidgetsBinding, e editorgroup.GridEvent) {
	switch e.Kind {
	case editorgroup.GroupEditorsChanged:
		switch e.Change.Kind {
		case editorgroup.EditorOpen, editorgroup.EditorClose, editorgroup.EditorDirty:
			// the dot of the tab of the workspace shows whether a document has unsaved changes
			defer s.tabsChanged(binding)
		}
		switch e.Change.Kind {
		case editorgroup.EditorOpen:
			s.ensureEditor(binding, e.Change.Editor)
		case editorgroup.EditorClose:
			// an editor that moved to another group stays; one that the target had already goes
			if e.Change.Context != editorgroup.CloseMove || !s.heldByAGroup(e.Change.Editor) {
				// the editor that had the focus takes it away: the group gets it back (an empty group
				// takes it in its empty area)
				if ed := s.group.editors[e.Change.Editor]; ed != nil && s.group.focused[ed] {
					s.askFocus(binding)
				} else if p := s.group.panes[e.Change.Editor]; p != nil && p.hasFocus() {
					s.askFocus(binding)
				}
				s.recordClosed(e.Group, e.Change)
				s.forgetClosed(e.Group, e.Change.Editor)
				s.releaseEditor(e.Change.Editor)
			}
		case editorgroup.EditorDirty:
			if e.Change.Editor.Dirty() && !e.Group.Model.IsPinned(e.Change.Editor) {
				e.Group.Model.Pin(e.Change.Editor)
			}
		case editorgroup.EditorActive:
			if path, ok := fileResource(e.Change.Editor); ok {
				PeekWorkspace(s.Context()).AddRecentFile(path)
			}
			// the editor that had the focus is replaced by the new active editor of its group: the
			// keyboard focus goes to the new one, and not to any editor of another group
			for _, input := range e.Group.Model.Editors(editorgroup.Sequential, false) {
				if input == e.Change.Editor {
					continue
				}
				if ed := s.group.editors[input]; ed != nil && s.group.focused[ed] {
					s.askFocus(binding)
					break
				}
				if p := s.group.panes[input]; p != nil && p.hasFocus() {
					s.askFocus(binding)
					break
				}
			}
		case editorgroup.EditorPin, editorgroup.EditorSticky, editorgroup.EditorMove,
			editorgroup.EditorLabel, editorgroup.EditorCapabilities, editorgroup.EditorWillDispose:
		}
	case editorgroup.GroupRemoved:
		delete(s.group.views, e.Group)
		if h := s.nav.history; h != nil {
			h.GroupRemoved(e.Group.ID())
		}
	case editorgroup.ActiveGroupChanged:
		s.askFocus(binding)
	case editorgroup.GroupAdded:
		// a copy of a group arrives with its editors
		for _, input := range e.Group.Model.Editors(editorgroup.Sequential, false) {
			s.ensureEditor(binding, input)
		}
	case editorgroup.GroupMoved, editorgroup.LayoutChanged:
	}
	// the order the editors were used in changed, which ends the walk of Open Next Recently Used Editor
	if h := s.nav.history; h != nil && e.Kind != editorgroup.LayoutChanged && e.Kind != editorgroup.GroupMoved {
		h.Recent.Reset()
		h.RecentInGroup.Reset()
	}
	// another editor that becomes the active one has taken the focus from the previous one (auto save)
	switch e.Kind {
	case editorgroup.GroupEditorsChanged, editorgroup.GroupRemoved, editorgroup.ActiveGroupChanged:
		s.activeEditorChanged()
	}
	s.syncNav()
	s.refreshKeys()
	s.refreshStatus()
	s.syncOutline()
	s.coreActiveChanged()
	if s.Mounted() {
		binding.Post(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		})
	}
}

// heldByAGroup tells whether the very same input is in a group.
func (s *shellState) heldByAGroup(input editorgroup.EditorInput) bool {
	for _, g := range s.group.grid.Groups(editorgroup.CreationTime) {
		if g.Model.ContainsStrict(input) {
			return true
		}
	}
	return false
}

// ensureEditor makes the editor of a text input that has none: it counts a use of the document of its
// own.
func (s *shellState) ensureEditor(binding *widgets.WidgetsBinding, input editorgroup.EditorInput) {
	t, ok := input.(*editorgroup.TextInput)
	if !ok {
		s.ensurePane(input)
		return
	}
	if s.group.editors[input] != nil {
		return
	}
	PeekWorkspace(s.Context()).Documents.Retain(t.Document())
	s.group.editors[input] = s.newTextEditor(binding, t)
}

// releaseEditor ends the editor of a closed input: its view and background work stop, the input is
// disposed and the document is released (it closes with its last user).
func (s *shellState) releaseEditor(input editorgroup.EditorInput) {
	if p := s.group.panes[input]; p != nil {
		delete(s.group.panes, input)
		p.dispose()
		if c, ok := p.(interface{ closed() }); ok {
			c.closed()
		}
	}
	e := s.group.editors[input]
	delete(s.group.editors, input)
	if e != nil {
		if scope := PeekWorkspace(s.Context()).Extensions(); scope != nil {
			scope.ReleaseFileViews(input, "")
		}
		delete(s.group.focused, e)
		e.dispose()
		if PeekWorkspace(s.Context()).Documents.Release(e.doc) {
			s.autoSaveUntrack(e.doc)
			s.externalUntrack(e.doc)
		}
	}
	input.Dispose()
}

// refreshKeys sets the context keys of the groups in the window: activeEditor (the type of the
// active editor), activeEditorIsPinned (it is sticky: the pinned tab with the pin),
// activeEditorIsNotPreview, activeEditorGroupEmpty, multipleEditorGroups, the two keys of the
// editor part that tell there are several groups and that one is maximized, and
// splitEditorsVertically (the side-by-side direction is down), the keys of saving: activeEditorIsDirty,
// activeEditorCanRevert and dirtyWorkingCopies, and those of the compare editor: textCompareEditorVisible (a group
// shows a diff editor) and textCompareEditorActive (the active group does).
func (s *shellState) refreshKeys() {
	win := PeekWorkspace(s.Context())
	grid := s.group.grid
	group := grid.Active()
	m := group.Model
	active := m.ActiveEditor()
	win.Keys.Batch(func() {
		win.Keys.Set("multipleEditorGroups", grid.Count() > 1)
		win.Keys.Set("editorPartMultipleEditorGroups", grid.Count() > 1)
		win.Keys.Set("editorPartMaximizedEditorGroup", grid.Maximized() != nil)
		win.Keys.Set("splitEditorsVertically", s.sideBySideDirection() == editorgroup.DirDown)
		win.Keys.Set("activeEditorGroupEmpty", active == nil)
		dirty, canRevert, anyDirty := s.saveKeys()
		win.Keys.Set("activeEditorIsDirty", dirty)
		win.Keys.Set("activeEditorCanRevert", canRevert)
		win.Keys.Set("dirtyWorkingCopies", anyDirty)
		compareVisible, compareActive := s.diffKeys()
		win.Keys.Set("textCompareEditorVisible", compareVisible)
		win.Keys.Set("textCompareEditorActive", compareActive)
		if merging, result := s.mergeKeys(); merging {
			win.Keys.Set("isMergeEditor", true)
			win.Keys.Set("mergeEditorResultUri", result)
			win.Keys.Set("mergeEditorLayout", "mixed")
		} else {
			win.Keys.Reset("isMergeEditor")
			win.Keys.Reset("mergeEditorResultUri")
			win.Keys.Reset("mergeEditorLayout")
		}
		if active == nil {
			win.Keys.Reset("activeEditor")
			win.Keys.Set("activeEditorIsPinned", false)
			win.Keys.Set("activeEditorIsNotPreview", false)
			return
		}
		win.Keys.Set("activeEditor", active.TypeID())
		win.Keys.Set("activeEditorIsPinned", m.IsSticky(active))
		win.Keys.Set("activeEditorIsNotPreview", m.IsPinned(active))
	})
	s.refreshNavKeys()
}

// disposeGroup stops following the grid and the settings, ends every editor and releases the
// documents.
func (s *shellState) disposeGroup() {
	g := &s.group
	if g.grid == nil {
		return
	}
	s.stopDrag()
	if g.stop != nil {
		g.stop()
	}
	s.disposeNavigation()
	if g.stopSettings != nil {
		g.stopSettings()
	}
	for _, remove := range g.removeCommands {
		remove()
	}
	g.removeCommands = nil
	win := s.win
	for input, e := range g.editors {
		e.dispose()
		win.Documents.Release(e.doc)
		input.Dispose()
	}
	g.editors = nil
	for input, p := range g.panes {
		p.dispose()
		input.Dispose()
	}
	g.panes = nil
	g.grid.Dispose()
	for _, key := range append([]string{"activeEditor", "activeEditorIsPinned", "activeEditorIsNotPreview", "activeEditorGroupEmpty",
		"multipleEditorGroups", "editorPartMultipleEditorGroups", "editorPartMaximizedEditorGroup", "splitEditorsVertically",
		"activeEditorIsDirty", "activeEditorCanRevert", "dirtyWorkingCopies", "textCompareEditorVisible", "textCompareEditorActive",
		"isMergeEditor", "mergeEditorResultUri", "mergeEditorLayout", "resourceSelectedForCompare"}, navKeys...) {
		win.Keys.Reset(key)
	}
}

// survivors are the editors that stay when some close, the one to activate first.
func survivors(m *editorgroup.Model, closing []editorgroup.EditorInput) []editorgroup.EditorInput {
	var out []editorgroup.EditorInput
	if m.Options().FocusRecentEditorAfterClose {
		for _, e := range m.Editors(editorgroup.MostRecentlyActive, false) {
			if !containsInput(closing, e) {
				out = append(out, e)
			}
		}
		return out
	}
	// nearest on the right of the active editor, else on its left
	editors := m.Editors(editorgroup.Sequential, false)
	at := m.IndexOf(m.ActiveEditor())
	for i := at + 1; i < len(editors); i++ {
		if !containsInput(closing, editors[i]) {
			out = append(out, editors[i])
		}
	}
	for i := at - 1; i >= 0; i-- {
		if !containsInput(closing, editors[i]) {
			out = append(out, editors[i])
		}
	}
	return out
}

func containsInput(list []editorgroup.EditorInput, e editorgroup.EditorInput) bool {
	for _, x := range list {
		if x == e {
			return true
		}
	}
	return false
}

// openFile reads a file for an editor of a group (the document is acquired once; showAcquired gives
// that use back).
func (s *shellState) openFile(ctx context.Context, path string) (*document.Document, error) {
	return PeekWorkspace(s.Context()).Documents.Acquire(ctx, path, document.NewOptions())
}

// ---- focus ----

// askFocus asks for the keyboard focus to go to the active group once its widgets are on screen: the
// request is tried after each frame until it is taken (a new group is built a frame after the grid
// changed) or it has been tried a few times. While it waits, the focus is not followed (focusMoved).
func (s *shellState) askFocus(binding *widgets.WidgetsBinding) {
	if s.group.focusAsked {
		return
	}
	s.group.focusAsked = true
	s.tryFocus(binding, 0)
}

// focusTries is how many frames a request for the focus of a group waits for its widgets.
const focusTries = 6

func (s *shellState) tryFocus(binding *widgets.WidgetsBinding, attempt int) {
	scheduler := binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if !s.Mounted() {
			s.group.focusAsked = false
			return
		}
		// A quick input that opened since the request keeps the focus (the keys of the shortcut that opened it already went
		// to it; taking the focus now would close it).
		if q := PeekWorkspace(s.Context()).QuickInput; q != nil && q.Visible() {
			s.group.focusAsked = false
			return
		}
		if s.focusGroup(s.activeGroup()) || attempt >= focusTries {
			s.group.focusAsked = false
			return
		}
		s.tryFocus(binding, attempt+1)
	})
}

// focusGroup gives the keyboard focus to a group: its active editor, or its empty area. It tells whether
// the group has it, or has been asked for it: false when its widgets are not on screen yet, or when the
// editor was asked for the focus and does not have it yet (the focus moves when the frame is done, so
// the request is tried again after it: a request that is taken for done too soon lets the focus fall
// to an editor of another group when the editor that had it goes). The focus stays where it is when
// the editor, or its find widget, has it.
func (s *shellState) focusGroup(group *editorgroup.Group) bool {
	if e := s.group.editors[group.Model.ActiveEditor()]; e != nil {
		if s.group.focused[e] || e.focus.HasFocus() {
			return true
		}
		e.focus.Request()
		return false
	}
	if p := s.group.panes[group.Model.ActiveEditor()]; p != nil {
		if p.hasFocus() {
			return true
		}
		p.focus()
		return false
	}
	if v := s.group.views[group]; v != nil && v.empty != nil && v.empty.Context() != nil {
		v.empty.RequestFocus()
		return true
	}
	return false
}

// focusMoved follows the keyboard focus into a group: it becomes the active group. While a request
// to focus the active group waits for the frame, the focus is not followed: it is on its way
// (an editor that closed leaves it for any editor, which is not the choice of the user).
func (s *shellState) focusMoved(group *editorgroup.Group) {
	if !s.group.focusAsked {
		s.groupFocused(group)
	}
}

// groupFocused makes a group the active group: the user pressed in it, chose one of its tabs, or the
// focus went into it.
func (s *shellState) groupFocused(group *editorgroup.Group) {
	if s.group.grid.Group(group.ID()) != group {
		return
	}
	if err := s.group.grid.Activate(group); err != nil {
		PeekWorkspace(s.Context()).Process.Logger.Error("cannot activate the group", "err", err)
	}
}
