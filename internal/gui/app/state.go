package app

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The UI state of a window (SPEC §10.10): the layout, the groups with their editors and the view state of each,
// the folders the Explorer has expanded, the size and state of the window and the files the quick open lists
// first are written to the state file of the window (core/storage) as they change, and read again when the
// window opens the same folder. The recently opened folders and files belong to no window and are in the
// global state file.
//
// Saving is in two steps, both coalescing: a change only marks the state as changed, a timer of
// stateSnapshotDelay on the UI thread encodes it (the grid, the layout and the view states are read on the UI
// thread), and the store writes the bytes once its own delay has passed, in a goroutine. Closing the window
// encodes and writes at once. Restoring is done before the window is shown (PrepareRestore opens the files of the
// saved editors), and the shell builds its groups from what that found.

// stateSnapshotDelay is how long after a change the state of the window is encoded. The store then waits its own
// delay (a second) before it writes.
const stateSnapshotDelay = 250 * time.Millisecond

// defaultGeometry is the size of a window that has no saved size.
var defaultGeometry = storage.Geometry{Width: windowWidth, Height: windowHeight}

// restoredWindow is what PrepareRestore found of the saved state of a window, for the shell to build the groups
// from.
type restoredWindow struct {
	state storage.WindowState
	// docs are the documents of the saved editors by their URI, nil for a file that could not be opened.
	docs map[string]*document.Document
	// dropped is the number of saved editors that could not be opened.
	dropped int
	// backups are the backups of the window by URI, and extra the documents restored from backups that no saved
	// editor shows (they open in the active group, in the order of their URIs).
	backups map[string]storage.Backup
	extra   []*document.Document
	// restored counts the documents that came back from backups, and gone lists the names of those whose file is
	// gone.
	restored int
	gone     []string
}

// restoreMode is the value of window.restoreWindows.
func restoreMode(store *settings.Store) string {
	return settings.Get[string](store, "window.restoreWindows", "")
}

// initState works out the id of the state file of the window and notes the folder as the last one opened. It
// is called when the window is made and when its folder changes. A window whose id is unknown (the system has no
// randomness, the state folder is unusable) keeps no state.
func (w *Workspace) initState(ctx context.Context) {
	defer w.initBackups(ctx) // the backups are named by the id
	defer w.Process.Workspaces.folderNote(w)
	w.stateID = ""
	var err error
	switch {
	case w.Folder != "":
		w.stateID, err = storage.WindowID(w.Folder)
	case w.emptyID != "":
		w.stateID = w.emptyID
	default:
		w.stateID, err = w.Process.State.EmptyWindowID(ctx)
	}
	if err != nil {
		w.Process.Logger.Warn("the window keeps no UI state", "err", err)
		if w.stateID == "" {
			return
		}
	}
	folder := w.Folder
	err = w.Process.State.UpdateGlobal(ctx, func(g *storage.GlobalState) bool {
		changed := g.LastWindow == nil || g.LastWindow.Folder != folder
		g.LastWindow = &storage.LastWindow{Folder: folder}
		if folder != "" {
			changed = g.Recent.AddFolder(folder) || changed
		}
		return changed
	})
	if err != nil {
		w.Process.Logger.Warn("the global UI state could not be used", "err", err)
	}
	// the folder is used with the profile of the window now: the next window of the folder opens with it
	if folder != "" {
		if err := w.Process.Profiles.SetLastProfile(ctx, folder, w.Profile()); err != nil {
			w.Process.Logger.Warn("the profile of the folder could not be noted", "folder", folder, "err", err)
		}
	}
}

// StateID is the id of the window's UI state file (storage.WindowID of its folder, or the id of the window with
// no folder); it is empty when the window keeps no state.
func (w *Workspace) StateID() string { return w.stateID }

// AddRecentFile notes that a file was opened: the quick open lists it first, and File > Open Recent lists it.
func (w *Workspace) AddRecentFile(path string) {
	w.Recent.Add(path)
	w.updateRecent(func(r *storage.RecentlyOpened) bool { return r.AddFile(path) })
}

// RemoveRecent takes a folder or a file out of File > Open Recent.
func (w *Workspace) RemoveRecent(path string) {
	w.updateRecent(func(r *storage.RecentlyOpened) bool { return r.Remove(path) })
}

// ClearRecent empties File > Open Recent and the files the quick open lists first.
func (w *Workspace) ClearRecent() {
	w.Recent.Clear()
	w.updateRecent(func(r *storage.RecentlyOpened) bool {
		had := !r.Empty()
		r.Clear()
		return had
	})
}

func (w *Workspace) updateRecent(fn func(*storage.RecentlyOpened) bool) {
	err := w.Process.State.UpdateGlobal(w.ctx, func(g *storage.GlobalState) bool { return fn(&g.Recent) })
	if err != nil {
		w.Process.Logger.Warn("the recently opened list could not be saved", "err", err)
	}
}

// PrepareRestore reads the saved state of the window and applies what does not need the shell: the layout, the
// files the quick open lists first, the folders the Explorer expands. It opens the files of the saved editors
// (in the calling goroutine: the window is not shown yet), and the shell makes the groups of them. The saved
// editors whose files cannot be opened are dropped, and one notification says how many. It does nothing when
// window.restoreWindows is none, when the window has no saved state, and when the saved state cannot be used
// (that is logged). It returns the saved geometry of the window, or nil.
func (w *Workspace) PrepareRestore(ctx context.Context) *storage.Geometry {
	if w.stateID == "" {
		return nil
	}
	r := &restoredWindow{docs: map[string]*document.Document{}, backups: map[string]storage.Backup{}}
	w.beginBackups(ctx, r)
	geometry, found := w.restoreUIState(ctx, r)
	w.openUnshownBackups(ctx, r)
	if found || len(r.extra) > 0 {
		w.restored = r
	}
	if r.dropped > 0 {
		w.Notify.Info("Emetos", DroppedEditorsNotice(r.dropped))
	}
	w.noticeRestoredBackups(r)
	return geometry
}

// restoreUIState reads the saved state of the window and applies it (PrepareRestore); found tells that there was a
// state to restore.
func (w *Workspace) restoreUIState(ctx context.Context, r *restoredWindow) (geometry *storage.Geometry, found bool) {
	if restoreMode(w.Settings) == "none" {
		return nil, false
	}
	st, found, err := w.stateStore().LoadWindow(ctx, w.stateID)
	if err != nil {
		w.Process.Logger.Warn("the saved UI state of the window cannot be used; it starts as new", "err", err)
		return nil, false
	}
	if !found {
		return nil, false
	}
	if len(st.Layout) > 0 {
		if err := w.restoreLayout(st.Layout); err != nil {
			w.Process.Logger.Warn("the saved layout cannot be used", "err", err)
		}
		w.restoreZen()
	}
	if st.ZoomLevel != nil && w.zoomPerWindow() {
		w.SetZoomLevel(*st.ZoomLevel)
	}
	if len(st.Recent) > 0 {
		w.Recent.Replace(st.Recent)
	}
	if w.Folder != "" {
		for _, dir := range st.Explorer.ExpandedPaths(w.Folder) {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				w.pendingExpanded = append(w.pendingExpanded, dir)
			}
		}
	}
	w.geometry = st.Geometry
	r.state = st
	if st.Grid != nil {
		w.openSavedEditors(ctx, r)
	}
	return st.Geometry, true
}

// restoreLayout puts the layout of the window back as the saved JSON says: it is read over the layout the settings make,
// so a key it lacks keeps that value, and sizes and positions that cannot be are repaired by Layout.Restore. A text
// that is not the JSON of a layout has the tag LayoutStateInvalid.
func (w *Workspace) restoreLayout(raw json.RawMessage) error {
	layout := workbench.LayoutStateFromSettings(w.Settings)
	if err := json.Unmarshal(raw, &layout); err != nil {
		return errors.NewErr("LayoutStateInvalid", err)
	}
	w.Layout.Restore(layout)
	return nil
}

// DroppedEditorsNotice is what the notification of restored editors that could not be opened says.
func DroppedEditorsNotice(n int) string {
	if n == 1 {
		return "1 editor could not be restored because its file no longer exists."
	}
	return strconv.Itoa(n) + " editors could not be restored because their files no longer exist."
}

// openSavedEditors opens the document of every saved text editor, once for a file that is in several groups,
// and counts the saved editors whose file cannot be opened. A document with a backup comes back from it
// (restoreFromBackup), and so does an untitled one: without a backup it is dropped, without a word.
func (w *Workspace) openSavedEditors(ctx context.Context, r *restoredWindow) {
	var visit func(nodes []editorgroup.SerializedNode)
	visit = func(nodes []editorgroup.SerializedNode) {
		for _, n := range nodes {
			visit(n.Groups)
			if n.Model == nil {
				continue
			}
			for _, in := range n.Model.Editors {
				switch in.TypeID {
				case editorgroup.TextInputTypeID:
					w.openSavedValue(ctx, r, in.Value)
				case editorgroup.DiffInputTypeID:
					// both sides of a saved diff editor are documents like the ones of text editors; a diff that
					// cannot be read is dropped
					if orig, mod, err := editorgroup.ParseDiffInput(in.Value); err == nil {
						before := r.dropped
						w.openSavedValue(ctx, r, orig.String())
						w.openSavedValue(ctx, r, mod.String())
						r.dropped = min(r.dropped, before+1) // one editor, whichever sides are gone
					}
				case editorgroup.MergeInputTypeID:
					// the result of a saved merge editor is a document like the ones of text editors; the rest of the
					// merge is read again when the editor is made
					if _, uri, err := editorgroup.ParseMergeInput(in.Value); err == nil {
						w.openSavedValue(ctx, r, uri.String())
					}
				}
			}
		}
	}
	visit(r.state.Grid.Root)
}

// openSavedValue opens the document of a saved URI once, and counts the saved editors whose file cannot be opened.
func (w *Workspace) openSavedValue(ctx context.Context, r *restoredWindow, value string) {
	if _, tried := r.docs[value]; !tried {
		doc, err := w.openSavedDocument(ctx, r, value)
		if err != nil {
			w.Process.Logger.Info("a saved editor cannot be restored", "file", value, "err", describe(err))
		}
		r.docs[value] = doc // nil for a file that cannot be opened
	}
	// an untitled editor without a backup and a virtual one without a provider are dropped without a word
	if r.docs[value] == nil && !strings.HasPrefix(value, document.SchemeUntitled+":") && !strings.HasPrefix(value, document.SchemeVirtual+":") {
		r.dropped++
	}
}

// openSavedDocument opens the document of a saved input (the value of TextInput.Serialize: a file URI, an
// untitled one or a virtual one). A document that has a backup comes back from it.
func (w *Workspace) openSavedDocument(ctx context.Context, r *restoredWindow, value string) (*document.Document, error) {
	uri, err := document.ParseURI(value)
	if err != nil {
		return nil, err
	}
	backup, hasBackup := r.backups[value]
	switch uri.Scheme {
	case document.SchemeUntitled:
		if !hasBackup {
			return nil, errors.New("SavedUntitledWithoutBackup", "a saved untitled editor has no backup", "uri", value)
		}
		return w.restoreFromBackup(ctx, r, uri, backup)
	case document.SchemeVirtual:
		// generated again; a provider that is gone drops the editor
		return w.Documents.OpenVirtual(ctx, uri)
	}
	// a file (ParseURI knows no other scheme)
	if hasBackup {
		return w.restoreFromBackup(ctx, r, uri, backup)
	}
	return w.Documents.OpenFile(ctx, uri.Path, document.NewOptions())
}

// takeRestored gives the shell what PrepareRestore found, once.
func (w *Workspace) takeRestored() *restoredWindow {
	r := w.restored
	w.restored = nil
	return r
}

// takeExpanded gives the Explorer the folders to expand, once.
func (w *Workspace) takeExpanded() []string {
	dirs := w.pendingExpanded
	w.pendingExpanded = nil
	return dirs
}

// startupWindowConfig is the config of the window: the size the window of the last session had (windowGeometry of the
// global state), else the size the shown workspace saved (a state from before the tabs shared a window), else the
// default; maximized when it was, and full screen when it was and window.restoreFullscreen says so. win is the
// workspace of the tab that is shown first.
func startupWindowConfig(win *Workspace, saved *storage.Geometry) widgets.WindowConfig {
	geo := defaultGeometry
	switch g, _ := win.Process.State.LoadGlobal(win.ctx); {
	case restoreMode(win.Settings) != "none" && g.WindowGeometry != nil && g.WindowGeometry.Valid():
		geo = *g.WindowGeometry
	case saved != nil && saved.Valid():
		geo = *saved
	}
	cfg := widgets.WindowConfig{Title: Title, Size: ui.Size{Width: geo.Width, Height: geo.Height}}
	if restoreMode(win.Settings) == "none" {
		return cfg
	}
	switch {
	case geo.Fullscreen && settings.Get[bool](win.Settings, "window.restoreFullscreen", ""):
		cfg.State = widgets.WindowStateFullscreen
	case geo.Maximized:
		cfg.State = widgets.WindowStateMaximized
	}
	return cfg
}

// ---- the shell ----

// stateState is what the shell keeps of saving the UI state of the window.
type stateState struct {
	// ready is true once the shell has made everything the snapshot reads; changes before are not noted.
	ready bool
	// suspended stops the saving while the editors of a folder close for another folder, so that the state of
	// the folder that is left is not written over with empty groups.
	suspended bool
	// timer is the timer that encodes the state, nil when none is set.
	timer func()
	// stops end what the shell follows for the state.
	stops []func()
	// geometry is the size and state of the window as last read.
	geometry storage.Geometry
	// expanded is the expanded folders of the Explorer as last saved, to tell a change.
	expanded string
	// source is the window controller whose changes are followed, and handle its listener.
	source geometrySource
	handle foundation.ListenerHandle
	// viewStates are the saved view states of the inputs of a restored grid, until their editors exist.
	viewStates map[editorgroup.EditorInput]json.RawMessage
}

// geometrySource is the part of a window controller the shell reads the size from.
type geometrySource interface {
	ContentSize() ui.Size
	AddListener(foundation.VoidCallback) foundation.ListenerHandle
	RemoveListener(foundation.ListenerHandle)
}

// windowModes is implemented by the controller of a regular window.
type windowModes interface {
	IsMaximized() bool
	IsFullscreen() bool
}

// initState follows what the state of the window is made of, and gives the window the function that saves it
// when it closes.
func (s *shellState) initState(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	st := &s.state
	if win.stateID == "" {
		return
	}
	// the size the window comes back to when it is not maximized: the one window's (the global state), else the one
	// this workspace saved before the tabs shared one window
	switch g, _ := win.Process.State.LoadGlobal(win.ctx); {
	case g.WindowGeometry != nil && g.WindowGeometry.Valid():
		st.geometry = *g.WindowGeometry
		st.geometry.Maximized, st.geometry.Fullscreen = false, false
	case win.geometry != nil && win.geometry.Valid():
		st.geometry = *win.geometry
	}
	changed := func() { s.stateChanged() }
	st.stops = append(st.stops,
		win.Layout.OnChange(func() { binding.Post(func() { s.stateChangedIfMounted() }) }),
		win.Recent.OnChange(func() { binding.Post(func() { s.stateChangedIfMounted() }) }),
		s.group.grid.OnChange(func(editorgroup.GridEvent) { changed() }),
	)
	s.followExplorerState()
	win.flushState = s.flushState
	st.ready = true
}

func (s *shellState) stateChangedIfMounted() {
	if s.Mounted() {
		s.stateChanged()
	}
}

// disposeState saves the state of the window and stops following it.
func (s *shellState) disposeState() {
	st := &s.state
	win := s.win
	if win != nil {
		win.flushState = nil
	}
	if !st.ready {
		return
	}
	s.flushState()
	st.ready = false
	if st.timer != nil {
		st.timer()
		st.timer = nil
	}
	for _, stop := range st.stops {
		stop()
	}
	st.stops = nil
	if st.source != nil {
		st.source.RemoveListener(st.handle)
		st.source = nil
	}
}

// stateChanged notes that something the state is made of changed: the state is encoded once the snapshot delay
// has passed.
func (s *shellState) stateChanged() {
	st := &s.state
	win := s.win
	if win == nil || !st.ready || st.suspended || st.timer != nil || win.after == nil {
		return
	}
	st.timer = win.after(stateSnapshotDelay, func() {
		st.timer = nil
		if s.Mounted() {
			s.snapshotState()
		}
	})
}

// snapshotState encodes the state of the window and hands it to the store, which writes it after its delay.
func (s *shellState) snapshotState() {
	st := &s.state
	win := s.win
	if win == nil || !st.ready || st.suspended || win.stateID == "" || win.dropState {
		return
	}
	if st.timer != nil {
		st.timer()
		st.timer = nil
	}
	if err := win.stateStore().SaveWindow(win.stateID, s.windowState()); err != nil {
		win.Process.Logger.Warn("the UI state of the window cannot be saved", "err", err)
	}
	if st.geometry.Valid() {
		geo := st.geometry
		err := win.Process.State.UpdateGlobal(win.ctx, func(g *storage.GlobalState) bool {
			if g.WindowGeometry != nil && *g.WindowGeometry == geo {
				return false
			}
			g.WindowGeometry = &geo
			return true
		})
		if err != nil {
			win.Process.Logger.Warn("the global UI state cannot be saved", "err", err)
		}
	}
}

// flushState encodes the state and writes every file that waits, now. It is what closing the window does;
// it does not stop when the context of the window was cancelled (the signal that ends the program).
func (s *shellState) flushState() {
	win := s.win
	if win == nil {
		return
	}
	s.snapshotState()
	if err := win.Process.flushStates(context.WithoutCancel(win.ctx)); err != nil {
		win.Process.Logger.Warn("the UI state cannot be written", "err", err)
	}
}

// windowState reads the state of the window.
func (s *shellState) windowState() storage.WindowState {
	win := s.win
	st := storage.WindowState{Folder: win.Folder, Recent: win.Recent.List()}
	if geo := s.state.geometry; geo.Valid() {
		st.Geometry = &geo
	}
	if layout, err := json.Marshal(win.Layout.State()); err == nil {
		st.Layout = layout
	} else {
		win.Process.Logger.Warn("the layout cannot be saved", "err", err)
	}
	// the zoom is the window's own only when it differs from the setting; it is kept for window.zoomPerWindow
	if level := win.ZoomLevel(); win.zoomPerWindow() && level != win.defaultZoomLevel() {
		st.ZoomLevel = &level
	}
	grid := s.group.grid.SerializeWith(s.inputState)
	st.Grid = &grid
	if ex := s.explorer.view; ex != nil && win.Folder != "" {
		st.Explorer = &storage.ExplorerState{Expanded: storage.ExpandedRelative(win.Folder, ex.Tree().ExpandedKeys())}
	}
	return st
}

// inputState is the view state of the editor of a text input: its cursors, scroll position and folds.
func (s *shellState) inputState(input editorgroup.EditorInput) json.RawMessage {
	e := s.group.editors[input]
	if e == nil {
		return nil
	}
	data, err := json.Marshal(e.view.ViewState())
	if err != nil {
		s.win.Process.Logger.Warn("the view state of an editor cannot be saved", "err", err)
		return nil
	}
	return data
}

// viewSig is what is compared to tell that the cursors or the scroll position of an editor changed.
type viewSig struct {
	selections []buffer.Selection
	top, left  float64
}

func (a viewSig) equal(b viewSig) bool {
	return a.top == b.top && a.left == b.left && slices.Equal(a.selections, b.selections)
}

func viewSigOf(e *textEditor) viewSig {
	return viewSig{selections: slices.Clone(e.view.Cursors().Selections()), top: e.view.ScrollTop(), left: e.view.ScrollLeft()}
}

// followEditorState notes a change of the cursors or the scroll position of an editor. The view model tells of
// every change of its text, folds and layout too, so what the state holds is compared before it is noted.
func (s *shellState) followEditorState(e *textEditor) {
	if e.stopState != nil {
		return
	}
	e.stateSig = viewSigOf(e)
	check := func() {
		if sig := viewSigOf(e); !sig.equal(e.stateSig) {
			e.stateSig = sig
			s.stateChangedIfMounted()
		}
	}
	h1 := e.view.AddListener(check)
	h2 := e.view.AddSelectionListener(check)
	e.stopState = func() {
		e.view.RemoveListener(h1)
		e.view.RemoveSelectionListener(h2)
	}
}

// followExplorerState notes a change of the folders the Explorer has expanded. The tree is made again when the
// folder changes, and this is called again then.
func (s *shellState) followExplorerState() {
	ex := &s.explorer
	if ex.view == nil {
		return
	}
	tree := ex.view.Tree()
	st := &s.state
	st.expanded = expandedKey(tree.ExpandedKeys())
	handle := tree.AddListener(func() {
		if key := expandedKey(tree.ExpandedKeys()); key != st.expanded {
			st.expanded = key
			s.stateChangedIfMounted()
		}
	})
	if ex.stopState != nil {
		ex.stopState()
	}
	ex.stopState = func() { tree.RemoveListener(handle) }
}

func expandedKey(keys []string) string {
	b, _ := json.Marshal(keys)
	return string(b)
}

// trackWindow follows the size and the state of the window.
func (s *shellState) trackWindow(src geometrySource) {
	st := &s.state
	if st.source != nil {
		return
	}
	st.source = src
	st.handle = src.AddListener(func() { s.readGeometry(src) })
	s.readGeometry(src)
}

// readGeometry takes the size and the state of the window. The size counts only while the window is neither
// maximized nor full screen: that is the size it comes back to.
func (s *shellState) readGeometry(src geometrySource) {
	st := &s.state
	geo := st.geometry
	size := src.ContentSize()
	geo.Maximized, geo.Fullscreen = false, false
	if m, ok := src.(windowModes); ok {
		geo.Maximized, geo.Fullscreen = m.IsMaximized(), m.IsFullscreen()
	}
	defer s.win.fullScreenChanged(geo.Fullscreen)
	if (!geo.Maximized && !geo.Fullscreen) || geo.Width == 0 {
		if size.Width > 0 && size.Height > 0 {
			geo.Width, geo.Height = size.Width, size.Height
		}
	}
	if geo != st.geometry {
		st.geometry = geo
		s.stateChangedIfMounted()
	}
}

// suspendState stops the saving of the state until resumeState; the state as it is now is encoded first.
func (s *shellState) suspendState() {
	s.snapshotState()
	s.state.suspended = true
}

// resumeState goes on saving, under the id the window has now.
func (s *shellState) resumeState() {
	s.state.suspended = false
	s.stateChanged()
}

// restoreGrid makes the grid of the window from what PrepareRestore found, or reports that there is nothing
// to restore (nil). The inputs are made over the documents it opened; the saved view states are kept, to give to
// the editors once they exist. A document that has unsaved changes (restored from a backup) and that no editor of
// the grid shows is added to the documents that open in the active group (restoredWindow.extra); the others that
// no editor shows are closed.
func (s *shellState) restoreGrid(r *restoredWindow) *editorgroup.Grid {
	win := s.win
	if r == nil || r.state.Grid == nil {
		return nil
	}
	used := map[string]bool{}
	viewStates := map[editorgroup.EditorInput]json.RawMessage{}
	grid, err := editorgroup.RestoreGridWith(*r.state.Grid, s.gridOptions(), func(in editorgroup.SerializedInput) (editorgroup.EditorInput, bool) {
		if in.TypeID == editorgroup.DiffInputTypeID {
			return s.restoreDiff(r, in.Value, used)
		}
		if in.TypeID == editorgroup.MergeInputTypeID {
			return s.restoreMerge(r, in.Value, used)
		}
		if in.TypeID != editorgroup.TextInputTypeID {
			// an editor that is not text comes back through the restore of its kind (panes.go)
			if k, ok := kindOf(in.TypeID); ok && k.restore != nil {
				return k.restore(in.Value)
			}
			return nil, false
		}
		doc := r.docs[in.Value]
		if doc == nil {
			return nil, false
		}
		used[in.Value] = true
		input := editorgroup.NewTextInput(doc, s.inputOptions())
		if len(in.State) > 0 {
			viewStates[input] = in.State
		}
		return input, true
	})
	if err != nil {
		win.Process.Logger.Warn("the saved editors cannot be restored; the window starts with none", "err", err)
		used = map[string]bool{}
	}
	var unused []string
	for value, doc := range r.docs {
		if doc != nil && !used[value] && !slices.Contains(r.extra, doc) {
			unused = append(unused, value)
		}
	}
	slices.Sort(unused) // the order of the extra documents does not depend on the map
	for _, value := range unused {
		if doc := r.docs[value]; doc.Dirty() {
			r.extra = append(r.extra, doc)
		} else {
			win.Documents.Close(doc.URI())
		}
	}
	if err != nil {
		return nil
	}
	s.state.viewStates = viewStates
	return grid
}

// restoreEditors makes the editors of the inputs of a restored grid and gives them their view states
// (workbench.editor.restoreViewState).
func (s *shellState) restoreEditors(binding *widgets.WidgetsBinding) {
	win := s.win
	restore := settings.Get[bool](win.Settings, "workbench.editor.restoreViewState", "")
	s.nav.hold++
	defer func() {
		s.nav.hold--
		s.syncNav()
	}()
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		for _, input := range g.Model.Editors(editorgroup.Sequential, false) {
			s.ensureEditor(binding, input)
			data := s.state.viewStates[input]
			e := s.group.editors[input]
			if e == nil || !restore || len(data) == 0 {
				continue
			}
			var vs editor.ViewState
			if err := json.Unmarshal(data, &vs); err != nil {
				win.Process.Logger.Warn("the saved view state of an editor cannot be used", "file", input.Title(), "err", err)
				continue
			}
			if err := e.view.RestoreViewState(vs); err != nil {
				win.Process.Logger.Debug("not all of the view of the editor could come back", "err", describe(err))
			}
		}
	}
	s.state.viewStates = nil
}
