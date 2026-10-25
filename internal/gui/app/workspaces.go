package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
)

// Window tabs and workspaces in one window (SPEC §10.14, D17).
//
// Workspaces is the manager of the window tabs of the process: the tabs in the order of the strip, which one is active,
// the order they were active in, and what opens, activates, moves, replaces and closes a tab. Emetos has one Glutter
// window (the implicit window of RunApp); the manager shows the tabs in it (tabsHost), and no other window is ever made.
// Everything the manager does to tabs runs on the UI thread; the list is guarded by a mutex because the settings of one
// workspace are propagated to the others from the goroutine that changed them.
//
// What the process shares: the registries of settings, commands (as definitions), menus, views and default keys, the
// themes, the languages and grammars, the snippets, the UI state store (the recently opened lists and the state files)
// and the launcher. What each workspace has of its own: a fork of the command registry, the notifications, the settings
// store (with the layer of its folder), the documents, the groups, the layout, the quick input, the watcher, the file
// index, the backups and the state of the workspace.

// TabKind is what a window tab holds. The strip, the restore, the tray and the keys work on tabs, never on folders: a
// tab of a kind that holds none behaves as the others do.
type TabKind int

const (
	// TabWorkspace is a tab that holds a workspace: a folder or none, with its profile and every service of
	// Workspace.
	TabWorkspace TabKind = iota
	// TODO(step 11.6): the kinds of the extension tabs (SPEC §10.15), which hold a widget of an extension in place of a
	// workspace.
)

// String is the name of the kind as the state file writes it.
func (k TabKind) String() string {
	switch k {
	case TabWorkspace:
		return storage.TabKindWorkspace
	}
	return "unknown"
}

// WindowTab is a tab of the window (SPEC §10.14): a workspace now, an extension from step 11.6. The tab is what the
// strip shows, what the restore brings back and what the keys switch between; its Workspace is what it holds.
type WindowTab struct {
	// seq numbers the tabs in the order they were made; it never changes, even when the tab opens another folder.
	seq int
	// id is the stable id of the tab (storage.NewTabID); it stays the same when the tab opens another folder.
	id   string
	kind TabKind
	// ws is the workspace of the tab now (opening another folder in the tab replaces it); nil for a kind that holds none.
	ws *Workspace
	// page makes the page of the shell for the workspace the tab has now.
	page func() widgets.Widget
	// key keeps the state of the widgets of the tab when the tabs are reordered or one in front of it closes.
	key *widgets.GlobalKey[*tabSlotState]
	// hosted is true for a tab the manager shows in the window; a workspace that is shown on its own (the test of a
	// workspace) is adopted as an unhosted tab, so that the lists and the quit flow know it.
	hosted bool
	// focus is the widget that had the keyboard focus in the tab when it was left, to give it back (restoreFocus).
	focus *widgets.FocusNode
	// gone is true once the tab left the list for good.
	gone bool
}

// ID is the stable id of the tab.
func (t *WindowTab) ID() string { return t.id }

// Kind is what the tab holds.
func (t *WindowTab) Kind() TabKind { return t.kind }

// Workspace is the workspace of the tab, or nil for a kind of tab that holds none.
func (t *WindowTab) Workspace() *Workspace { return t.ws }

// Workspaces is the manager of the window tabs of a process (Process.Workspaces).
type Workspaces struct {
	p *Process

	mu    sync.Mutex
	tabs  []*WindowTab // in the order of the strip
	order []*WindowTab // in the order they were active, the most recent last
	seq   int

	// Quit, when set, ends the application in place of the binding (tests count its calls). A Workspace's own Quit
	// is asked first.
	Quit func()

	// The rest is used on the UI thread only.
	ctx     context.Context
	binding *widgets.WidgetsBinding
	// host is the widget that shows the tabs; nil until it is mounted.
	host *tabsHostState
	// quitting is true from the first question of a quit until it is cancelled or the application ends.
	quitting bool
	observer *workspacesObserver
	// watch is true when the cancellation of ctx is the signal that ends the program (Run).
	watch bool
	// listeners are told when the tabs change: opened, closed, moved, activated or renamed.
	listeners []*func()
}

func newWorkspaces(p *Process) *Workspaces { return &Workspaces{p: p} }

// Tabs lists the window tabs in the order of the strip.
func (m *Workspaces) Tabs() []*WindowTab {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.tabs)
}

// All lists the workspaces of the tabs in the order of the strip.
func (m *Workspaces) All() []*Workspace {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Workspace, 0, len(m.tabs))
	for _, t := range m.tabs {
		if t.ws != nil {
			out = append(out, t.ws)
		}
	}
	return out
}

// Count is how many tabs are open.
func (m *Workspaces) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tabs)
}

// ActiveTab is the tab that is shown, or nil when there is none.
func (m *Workspaces) ActiveTab() *WindowTab {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.order) == 0 {
		return nil
	}
	return m.order[len(m.order)-1]
}

// Active is the workspace of the active tab, or nil when there is none (or it holds none).
func (m *Workspaces) Active() *Workspace {
	if t := m.ActiveTab(); t != nil {
		return t.ws
	}
	return nil
}

// IndexOf is the place of a tab in the strip, or -1.
func (m *Workspaces) IndexOf(t *WindowTab) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Index(m.tabs, t)
}

// OnChange calls fn, on the UI thread, after the tabs changed (opened, closed, moved, activated, or a tab got another
// folder or profile), and returns the function that stops it.
func (m *Workspaces) OnChange(fn func()) (unsubscribe func()) {
	p := &fn
	m.mu.Lock()
	m.listeners = append(m.listeners, p)
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.listeners = slices.DeleteFunc(m.listeners, func(x *func()) bool { return x == p })
	}
}

// changed tells the listeners that the tabs changed.
func (m *Workspaces) changed() {
	m.mu.Lock()
	listeners := slices.Clone(m.listeners)
	m.mu.Unlock()
	for _, f := range listeners {
		(*f)()
	}
}

// tabOf is the tab that holds a workspace, or nil.
func (m *Workspaces) tabOf(w *Workspace) *WindowTab {
	m.mu.Lock()
	defer m.mu.Unlock()
	return w.tab
}

// newTabID makes the id of a new tab; the tab count stands in when the system has no randomness.
func (m *Workspaces) newTabID() string {
	id, err := storage.NewTabID()
	if err != nil {
		m.p.Logger.Warn("a tab id could not be made at random", "err", err)
		m.mu.Lock()
		defer m.mu.Unlock()
		return fmt.Sprintf("%016x", m.seq)
	}
	return id
}

// insert puts a tab into the list at a place (clamped); it does not become the active one unless it is the only one.
func (m *Workspaces) insert(t *WindowTab, at int) {
	m.mu.Lock()
	m.seq++
	t.seq = m.seq
	at = max(0, min(at, len(m.tabs)))
	m.tabs = slices.Insert(m.tabs, at, t)
	if len(m.order) == 0 {
		m.order = append(m.order, t)
	} else {
		// a tab that is not active yet is the oldest of the order, so that closing the active one does not skip to it
		m.order = slices.Insert(m.order, 0, t)
	}
	m.mu.Unlock()
	if t.ws != nil {
		t.ws.tab = t
		m.followUserSettings(t.ws)
	}
}

// register adopts a workspace that is shown on its own (the shell of a workspace that no tab holds), so that the lists,
// the quit flow and the launches know it. A workspace of a tab is there already.
func (m *Workspaces) register(w *Workspace) *WindowTab {
	m.mu.Lock()
	if w.tab != nil {
		t := w.tab
		m.mu.Unlock()
		return t
	}
	m.mu.Unlock()
	t := &WindowTab{kind: TabWorkspace, ws: w, id: m.newTabID()}
	m.insert(t, len(m.Tabs()))
	m.Activate(t)
	m.syncTabs()
	return t
}

// followUserSettings makes a change of the user's settings in one workspace reach the others: each workspace has a
// store of its own (it has the layer of its folder), all over the same settings.json, and the file watcher of each
// reloads it when the file changes; this does it at once, so that a workspace with no watcher (and a change made
// through one store) is not left behind. A reload that changes nothing tells nobody, which ends the chain.
func (m *Workspaces) followUserSettings(w *Workspace) {
	stop := w.Settings.OnChange(func(e settings.ChangeEvent) {
		if !slices.Contains(e.Targets, settings.TargetUser) {
			return
		}
		profile := w.Profile()
		for _, other := range m.All() {
			// a workspace of another profile has another settings.json
			if other == w || other.Profile() != profile {
				continue
			}
			if _, err := other.Settings.Reload(m.ctxOr()); err != nil {
				m.p.Logger.Warn("reading the settings again failed", "err", err)
			}
		}
	})
	w.stops = append(w.stops, stop)
}

// unregister takes the tab of a workspace out of the list when its widgets are gone. The list of the open tabs in the
// state is not changed: the tabs that are open when the application quits stay in it (SPEC §10.10); the ones that the
// user closes leave it in removeTab.
func (m *Workspaces) unregister(w *Workspace) (last bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := w.tab
	if t == nil {
		return len(m.tabs) == 0
	}
	w.tab = nil
	m.tabs = slices.DeleteFunc(m.tabs, func(x *WindowTab) bool { return x == t })
	m.order = slices.DeleteFunc(m.order, func(x *WindowTab) bool { return x == t })
	return len(m.tabs) == 0
}

// ctxOr is the context of the manager, or a background one before the application runs.
func (m *Workspaces) ctxOr() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

// syncTabs writes the list of the open tabs and the active one to the global state.
func (m *Workspaces) syncTabs() {
	var list []storage.WorkspaceTab
	for _, t := range m.Tabs() {
		if w := t.ws; w != nil && w.stateID != "" {
			list = append(list, storage.WorkspaceTab{ID: t.id, StateID: w.stateID, Folder: w.Folder, Profile: w.Profile()})
		}
	}
	active := ""
	var folder *string
	if t := m.ActiveTab(); t != nil && t.ws != nil {
		active = t.id
		f := t.ws.Folder
		folder = &f
	}
	err := m.p.State.UpdateGlobal(m.ctxOr(), func(g *storage.GlobalState) bool {
		changed := false
		if !slices.Equal(g.WorkspaceTabs, list) {
			g.WorkspaceTabs, changed = list, true
		}
		if len(g.OpenWindows) > 0 {
			g.OpenWindows, changed = nil, true
		}
		if active != "" && g.ActiveWorkspaceTab != active {
			g.ActiveWorkspaceTab, changed = active, true
		}
		if folder != nil && (g.LastWindow == nil || g.LastWindow.Folder != *folder) {
			g.LastWindow, changed = &storage.LastWindow{Folder: *folder}, true
		}
		return changed
	})
	if err != nil {
		m.p.Logger.Warn("the global UI state could not be used", "err", err)
	}
}

// Tab is the window tab that holds the workspace, or nil for a workspace that no tab holds.
func (w *Workspace) Tab() *WindowTab { return w.tab }

// managed tells whether the workspace is shown by the manager of the tabs, not on its own.
func (w *Workspace) managed() bool { return w.tab != nil && w.tab.hosted }

// activeTab tells whether the workspace is the one the window shows. A workspace that is shown on its own is.
func (w *Workspace) activeTab() bool {
	if !w.managed() {
		return true
	}
	return w.Process.Workspaces.ActiveTab() == w.tab
}

// folderNote is told by a workspace whose state id or folder changed.
func (m *Workspaces) folderNote(w *Workspace) {
	if w.tab != nil {
		m.syncTabs()
		m.changed()
	}
}

// workspaceForFolder is the workspace that has the folder open (compared through links), or nil.
func (m *Workspaces) workspaceForFolder(folder string) *Workspace {
	if folder == "" {
		return nil
	}
	for _, w := range m.All() {
		if sameFolder(w.Folder, folder) {
			return w
		}
	}
	return nil
}

// stateIDTaken tells whether a live workspace has the state id.
func (m *Workspaces) stateIDTaken(id string) bool {
	for _, w := range m.All() {
		if w.stateID == id {
			return true
		}
	}
	return false
}

// freshEmptyID makes the id of the state of a workspace without a folder that has no state yet.
func (m *Workspaces) freshEmptyID() (string, error) {
	for range 16 {
		id, err := storage.NewEmptyWindowID()
		if err != nil {
			return "", err
		}
		if !m.stateIDTaken(id) {
			return id, nil
		}
	}
	return "", errors.New("WindowIDUnavailable", "no unused window id was found")
}

// ---- binding to the application ----

// workspacesObserver follows what the platform tells the whole application.
type workspacesObserver struct {
	widgets.WidgetsBindingObserverBase
	m *Workspaces
}

// DidRequestAppExit is a request to exit the application: the close button of the window, or a request that comes from
// the application or the platform. It is a quit, with the questions of every tab. The answer is always cancel; the
// application ends itself once every tab has agreed.
//
// TODO(step 9b.4): the close button hides the window to the tray when there is one. Glutter tells the close button
// from a quit: WindowingOwner.SetImplicitWindowDelegate gives a delegate that is asked when the user closes the first
// window (OnWindowCloseRequested, with the controller), and without one the close button is this request to exit.
// Nothing takes PlatformDispatcher.OnExitRequested over.
func (o *workspacesObserver) DidRequestAppExit() ui.AppExitResponse {
	o.m.RequestQuit(o.m.Active())
	return ui.AppExitResponseCancel
}

// bind connects the manager to the application: the requests to exit, the launcher and the controller of the window.
// It is called by the host of the tabs (and by the shell of a workspace that is shown on its own), and is safe to
// call again.
func (m *Workspaces) bind(binding *widgets.WidgetsBinding) {
	if m.binding != nil {
		return
	}
	m.binding = binding
	m.observer = &workspacesObserver{m: m}
	binding.AddObserver(m.observer)
	if l := m.p.Launcher; l != nil {
		l.attach(m, binding.Post)
	}
	if m.watch {
		m.watchSignal(m.ctx, binding)
	}
	if c, ok := m.implicitController(); ok {
		for _, w := range m.All() {
			if w.controller == nil {
				w.controller = c
			}
		}
	}
}

// watchSignal turns the cancellation of the context of Run (the signal that ends the program) into an exit that
// writes the backups and the state of every tab first, without the questions about unsaved changes.
func (m *Workspaces) watchSignal(ctx context.Context, binding *widgets.WidgetsBinding) {
	go func() {
		<-ctx.Done()
		binding.Post(func() {
			for _, w := range m.All() {
				if s := w.shell; s != nil && s.Mounted() {
					s.backupOnSignal()
					s.flushState()
				}
			}
			binding.ExitApplication(ui.AppExitTypeRequired)
		})
	}()
}

// implicitController is the controller of the window.
func (m *Workspaces) implicitController() (*widgets.RegularWindowController, bool) {
	if m.binding == nil {
		return nil, false
	}
	return m.binding.WindowingOwner().ImplicitWindowController()
}

// ---- opening tabs ----

// WorkspaceSpec says how a workspace tab opens.
type WorkspaceSpec struct {
	// Folder is the folder to open; empty for a tab without one.
	Folder string
	// TabID is the id of the tab that comes back (from the list of the open tabs); empty makes a new one.
	TabID string
	// StateID is the id of the state of a workspace without a folder that comes back (from the list of the open
	// tabs); empty makes a new one, which has no state to restore.
	StateID string
	// Profile is the name of the profile the workspace uses ("Default" for the default profile), made empty when it
	// does not exist (SPEC §10.12); empty is the profile last used with the folder, else the default profile.
	Profile string
	// Files are opened in the workspace after its saved editors, the first of them shown.
	Files []string
	// Launch is the command line of the launch that opens the workspace, for the notices of the parts it does not do
	// yet.
	Launch *ipc.OpenArgs
	// Steps are the dev commands to run once the workspace is up.
	Steps []DevStep
}

// preparedWorkspace is a workspace that is made and restored, and waits for its tab.
type preparedWorkspace struct {
	ws       *Workspace
	tabID    string
	geometry *storage.Geometry
	docs     []*document.Document
	blocked  string
	steps    []DevStep
}

// prepare makes a workspace for a spec and restores its state; nothing is on screen yet. The error has the tags of
// NewWorkspace.
func (m *Workspaces) prepare(ctx context.Context, spec WorkspaceSpec, from *Workspace) (*preparedWorkspace, error) {
	o := workspaceOptions{folder: spec.Folder, stateID: spec.StateID, from: from}
	var profileProblem error
	o.profile, profileProblem = m.workspaceProfile(ctx, spec)
	if spec.Folder == "" && o.stateID == "" {
		id, err := m.freshEmptyID()
		if err != nil {
			m.p.Logger.Warn("the workspace keeps no UI state", "err", err)
		}
		o.stateID = id
	}
	ws, err := newWorkspace(ctx, m.p, o)
	if err != nil {
		return nil, err
	}
	ws.startup = spec.Launch
	if profileProblem != nil {
		ws.Notify.Warn("Emetos", fmt.Sprintf(ProfileCreateFailedNotice, spec.Profile, describe(profileProblem)))
	}
	geometry := ws.PrepareRestore(ctx)
	docs, blocked := ws.openStartupFiles(ctx, spec.Files)
	return &preparedWorkspace{ws: ws, tabID: spec.TabID, geometry: geometry, docs: docs, blocked: blocked, steps: spec.Steps}, nil
}

// openStartupFiles opens the files of a command line in the workspace, before it is shown: a file that cannot be
// opened is reported, a file that looks binary is offered with a button in place of the editor.
func (w *Workspace) openStartupFiles(ctx context.Context, files []string) (shown []*document.Document, blocked string) {
	for _, path := range files {
		doc, err := w.Documents.OpenFile(ctx, path, document.NewOptions())
		if err != nil {
			if isBinaryError(err) {
				if len(shown) == 0 && blocked == "" {
					blocked = path
				}
				continue
			}
			w.Notify.Error("Emetos", "Cannot open "+path+": "+describe(err))
			continue
		}
		shown = append(shown, doc)
	}
	return shown, blocked
}

// pageOf makes the function that makes the page of the shell of a prepared workspace.
func (m *Workspaces) pageOf(pw *preparedWorkspace) func() widgets.Widget {
	return func() widgets.Widget {
		sh := newShell(m.ctxOr(), pw.docs, pw.steps)
		sh.blocked = pw.blocked
		return sh
	}
}

// addTab makes the tab of a prepared workspace and puts it into the list at a place.
func (m *Workspaces) addTab(pw *preparedWorkspace, at int) *WindowTab {
	id := pw.tabID
	if id == "" {
		id = m.newTabID()
	}
	t := &WindowTab{kind: TabWorkspace, ws: pw.ws, id: id, page: m.pageOf(pw), hosted: true}
	t.key = widgets.NewGlobalKey[*tabSlotState]()
	m.insert(t, at)
	m.syncTabs()
	m.refreshHost()
	m.changed()
	return t
}

// start decides which tabs open (the command line, the tabs of the last session: startupPlan), makes them all and
// returns the one that is shown first; the caller shows the window (hostWidget). The error is that of NewWorkspace,
// when no tab could be made; a tab that cannot be made when others can is reported in the notifications of the first.
func (m *Workspaces) start(ctx context.Context, opts Options) (*preparedWorkspace, error) {
	plan, active := m.p.startupPlan(ctx, opts)
	var shown *preparedWorkspace
	var firstErr error
	var failures []string
	for i, spec := range plan {
		pw, err := m.prepare(ctx, spec, nil)
		if err != nil {
			m.p.Logger.Warn("a workspace cannot be restored", "folder", spec.Folder, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			failures = append(failures, "Cannot open a workspace"+folderSuffix(spec.Folder)+": "+describe(err))
			continue
		}
		t := m.addTab(pw, len(m.Tabs()))
		if shown == nil || i == active {
			shown = pw
			m.Activate(t)
		}
	}
	if shown == nil {
		return nil, firstErr
	}
	for _, f := range failures {
		shown.ws.Notify.Error("Emetos", f)
	}
	return shown, nil
}

// Open opens a workspace in a new tab, after the active one, and makes it the active tab, or, when a tab has the folder
// already, makes that tab the active one. It must run on the UI thread. The error is that of NewWorkspace.
func (m *Workspaces) Open(spec WorkspaceSpec) (*Workspace, error) {
	return m.openFrom(spec, m.Active())
}

func (m *Workspaces) openFrom(spec WorkspaceSpec, from *Workspace) (*Workspace, error) {
	if w := m.workspaceForFolder(spec.Folder); w != nil {
		w.Raise()
		return w, nil
	}
	pw, err := m.prepare(m.ctxOr(), spec, from)
	if err != nil {
		return nil, err
	}
	at := len(m.Tabs())
	if t := m.ActiveTab(); t != nil {
		at = m.IndexOf(t) + 1
	}
	t := m.addTab(pw, at)
	if c, ok := m.implicitController(); ok {
		pw.ws.controller = c
	}
	m.Activate(t)
	return pw.ws, nil
}

// folderSuffix is " for <folder>" for a workspace with a folder.
func folderSuffix(folder string) string {
	if folder == "" {
		return ""
	}
	return " for " + folder
}

// NewTab opens a tab without a folder, with nothing in it and no state to restore, as New Workspace Tab does
// (workbench.action.newWindow). The error is that of Open.
func (m *Workspaces) NewTab() (*Workspace, error) { return m.Open(WorkspaceSpec{}) }

// ---- activating and moving tabs ----

// Activate makes a tab the active one: its workbench is shown, its tickers run, and the keyboard focus goes to what
// had it in the tab; the tab that was active stops ticking and painting, keeps everything else, and counts as having
// lost the focus for auto save (files.autoSave onWindowChange). It reports false for a tab that is not in the list.
func (m *Workspaces) Activate(t *WindowTab) bool {
	m.mu.Lock()
	if t == nil || t.gone || !slices.Contains(m.tabs, t) {
		m.mu.Unlock()
		return false
	}
	var prev *WindowTab
	if n := len(m.order); n > 0 {
		prev = m.order[n-1]
	}
	if prev == t {
		m.mu.Unlock()
		return true
	}
	m.order = append(slices.DeleteFunc(m.order, func(x *WindowTab) bool { return x == t }), t)
	m.mu.Unlock()
	if prev != nil {
		m.leave(prev)
	}
	m.syncTabs()
	m.pushTitle(t)
	m.refreshHost()
	m.restoreFocus(t)
	m.changed()
	return true
}

// ActivateIndex makes the tab at a place of the strip the active one; it reports false for a place with no tab.
func (m *Workspaces) ActivateIndex(i int) bool {
	tabs := m.Tabs()
	if i < 0 || i >= len(tabs) {
		return false
	}
	return m.Activate(tabs[i])
}

// leave is what happens to the tab that stops being the active one: where its keyboard focus was is remembered, and its
// documents are saved when files.autoSave says so on a change of window.
func (m *Workspaces) leave(t *WindowTab) {
	w := t.ws
	if w == nil {
		return
	}
	if n := w.keyNode; n != nil && n.HasFocus() && m.binding != nil {
		t.focus = m.binding.FocusManager().PrimaryFocus()
	}
	if s := w.shell; s != nil && s.Mounted() && s.autosave.ctl != nil {
		s.autosave.ctl.WindowFocusLost()
	}
}

// restoreFocus gives the keyboard focus back to what had it in a tab, once the frame that shows the tab is built (the
// tabs that are not shown are excluded from the focus, which takes it from them). A tab that never had the focus, or
// whose widget is gone, gives it to its active editor, when it has one.
func (m *Workspaces) restoreFocus(t *WindowTab) {
	if m.binding == nil {
		return
	}
	node := t.focus
	m.binding.RunPostFrame(func(time.Duration) {
		w := t.ws
		if m.ActiveTab() != t || w == nil {
			return
		}
		if node != nil && node.CanRequestFocus() && w.keyNode != nil && slices.Contains(node.Ancestors(), w.keyNode) {
			node.RequestFocus()
			return
		}
		if s := w.shell; s != nil && s.Mounted() {
			if e := s.activeEditor(); e != nil && e.focus != nil && w.keyNode != nil && !w.keyNode.HasFocus() {
				e.focus.Request()
			}
		}
	})
	m.binding.Scheduler().ScheduleFrame()
}

// pushTitle gives the window the title of the tab: the title of the workspace as the shell last made it.
func (m *Workspaces) pushTitle(t *WindowTab) {
	if t == nil || t.ws == nil || t.ws.title == "" {
		return
	}
	if c := t.ws.controller; c != nil {
		c.SetTitle(t.ws.title)
	}
}

// refreshHost makes the widget that shows the tabs build again.
func (m *Workspaces) refreshHost() {
	if h := m.host; h != nil {
		h.refresh()
	}
}

// Move puts a tab at a place of the strip (clamped to the first and the last); the active tab stays the active one. It
// reports false for a tab that is not in the list.
func (m *Workspaces) Move(t *WindowTab, to int) bool {
	m.mu.Lock()
	from := slices.Index(m.tabs, t)
	if from < 0 {
		m.mu.Unlock()
		return false
	}
	to = max(0, min(to, len(m.tabs)-1))
	if to == from {
		m.mu.Unlock()
		return true
	}
	m.tabs = slices.Delete(m.tabs, from, from+1)
	m.tabs = slices.Insert(m.tabs, to, t)
	m.mu.Unlock()
	m.syncTabs()
	m.refreshHost()
	m.changed()
	return true
}

// ---- closing ----

// closeResult is how the questions of a workspace that closes were answered.
type closeResult struct {
	// proceed is false when the user cancelled, or a request was already being answered.
	proceed bool
	// discard says that the backups of the workspace are removed (the changes were saved or given up); false keeps
	// them for the next start.
	discard bool
}

// closeAsk asks a workspace the questions of closing it, and tells done the answer; it closes nothing. A workspace
// with no shell on screen has nothing to lose.
func (w *Workspace) closeAsk(reason CloseReason, done func(closeResult)) {
	if w.closer == nil {
		done(closeResult{proceed: true})
		return
	}
	w.closer(reason, done)
}

// commitClose does what ends a workspace once its questions are answered: the backups are removed or kept, the state
// is written and the marker of the running workspace is taken away.
func (w *Workspace) commitClose(discard bool) {
	if discard {
		w.discardBackups()
	} else if t := w.Backups(); t != nil {
		t.Pause() // the editors close with the workspace: their backups stay
	}
	if w.flushState != nil {
		w.flushState()
	}
	w.endBackups()
}

// RequestClose is the workspace being asked to close, by Close Workspace Tab or Quit (SPEC §10.10, §10.14). It never
// closes anything before the questions are answered: the caller keeps the tab open.
//
// A tab closes alone with CloseReasonTab (Workspaces.Close): when no document would lose changes, or files.hotExit
// leaves them to the backups (they are written first), it closes at once; else a dialog asks whether to save them.
// Save saves them (an untitled document through Save As) and closes; Don't Save closes; Cancel, or a save that fails
// or is cancelled, keeps the tab. The last tab leaves a No Folder tab.
//
// CloseReasonQuit is the quit of the whole application: every tab is asked in turn, in the order of the strip, and the
// application ends only when all agreed; Cancel anywhere keeps every tab. A request that comes while a dialog about
// closing is open is ignored.
func (w *Workspace) RequestClose(reason CloseReason) {
	m := w.Process.Workspaces
	if reason == CloseReasonQuit {
		m.RequestQuit(w)
		return
	}
	if t := m.tabOf(w); t != nil {
		m.Close(t, nil)
	}
}

// Close closes a tab after the questions about the unsaved changes of its documents (or the backups of hot exit,
// files.hotExit), disposes its workspace, and makes the tab that was active before it the active one. The last tab
// leaves a No Folder tab: Emetos never has no tab. done (it may be nil) is told, on the UI thread, whether the tab
// closed (false: the user cancelled, or a dialog or a quit was going on).
func (m *Workspaces) Close(t *WindowTab, done func(closed bool)) {
	if done == nil {
		done = func(bool) {}
	}
	if m.quitting || t == nil || t.gone || t.ws == nil {
		done(false)
		return
	}
	t.ws.closeAsk(CloseReasonTab, func(r closeResult) {
		if !r.proceed || t.gone {
			done(false)
			return
		}
		t.ws.commitClose(r.discard)
		m.removeTab(t)
		done(true)
	})
}

// RequestQuit quits the application: every tab is asked in turn, in the order of the strip, whether it may close (the
// questions about unsaved changes, or the backups of hot exit); when all agreed the workspaces write their state and
// the application ends. Cancel in any tab stops it and keeps all of them. A second request while the questions go on
// is ignored. from is the workspace that asked.
func (m *Workspaces) RequestQuit(from *Workspace) {
	if m.quitting {
		return
	}
	tabs := m.Tabs()
	if len(tabs) == 0 {
		m.exitApplication(from)
		return
	}
	m.quitting = true
	results := make([]closeResult, len(tabs))
	var step func(i int)
	step = func(i int) {
		if i == len(tabs) {
			for j, t := range tabs {
				if t.ws != nil {
					t.ws.commitClose(results[j].discard)
				}
			}
			m.exitApplication(from)
			return
		}
		t := tabs[i]
		if t.gone || t.ws == nil {
			step(i + 1)
			return
		}
		t.ws.closeAsk(CloseReasonQuit, func(r closeResult) {
			if !r.proceed {
				m.quitting = false
				return
			}
			results[i] = r
			step(i + 1)
		})
	}
	step(0)
}

// exitApplication ends the application: the Quit function of the workspace that asked, or of the manager, or of a
// workspace that has one, else the application is asked to exit without asking the observers again. The state is
// written first, and the launcher stops answering.
func (m *Workspaces) exitApplication(from *Workspace) {
	m.quitting = true
	if err := m.p.flushStates(context.WithoutCancel(m.ctxOr())); err != nil {
		m.p.Logger.Warn("the UI state cannot be written", "err", err)
	}
	if l := m.p.Launcher; l != nil {
		l.Close()
	}
	// a function that stands for the exit (a test's) leaves the application running: it may be asked again
	seam := func(f func()) {
		f()
		m.quitting = false
	}
	if from != nil && from.Quit != nil {
		seam(from.Quit)
		return
	}
	if m.Quit != nil {
		seam(m.Quit)
		return
	}
	for _, w := range m.All() {
		if w.Quit != nil {
			seam(w.Quit)
			return
		}
	}
	for _, w := range m.All() {
		if w.exit != nil {
			w.exit()
			return
		}
	}
	if m.binding != nil {
		m.binding.ExitApplication(ui.AppExitTypeRequired)
	}
}

// removeTab takes a tab that closed out of the list and the window: its widgets are disposed with the next frame, and
// the tab leaves the list of the open tabs. A workspace without a folder is gone for good: its state is deleted. When
// it was the last tab a No Folder tab takes its place.
func (m *Workspaces) removeTab(t *WindowTab) {
	if t.gone {
		return
	}
	w := t.ws
	if w.Folder == "" {
		w.dropState = true
	}
	if m.Count() == 1 {
		// Emetos never has no tab: the new one is made first, so that the list is never empty
		if _, err := m.openFrom(WorkspaceSpec{}, w); err != nil {
			m.p.Logger.Warn("a workspace tab cannot be opened in place of the last one", "err", err)
		}
	}
	m.forget(t)
	m.syncTabs()
	if next := m.ActiveTab(); next != nil {
		m.pushTitle(next)
		m.restoreFocus(next)
	}
	m.refreshHost()
	m.changed()
	m.afterUnmount(func() {
		w.release()
		if w.dropState && w.stateID != "" {
			if err := w.stateStore().DeleteWindow(w.stateID); err != nil {
				m.p.Logger.Warn("the state of a closed workspace cannot be removed", "err", err)
			}
		}
	})
}

// forget takes a tab out of the lists and marks it gone.
func (m *Workspaces) forget(t *WindowTab) {
	m.mu.Lock()
	m.tabs = slices.DeleteFunc(m.tabs, func(x *WindowTab) bool { return x == t })
	m.order = slices.DeleteFunc(m.order, func(x *WindowTab) bool { return x == t })
	t.gone = true
	if t.ws != nil && t.ws.tab == t {
		t.ws.tab = nil
	}
	m.mu.Unlock()
}

// afterUnmount runs f after the frame that disposes the widgets of a workspace, or at once without a binding.
func (m *Workspaces) afterUnmount(f func()) {
	if m.binding == nil || m.host == nil {
		f()
		return
	}
	m.binding.RunPostFrame(func(time.Duration) { f() })
}

// release lets go of what a workspace that is gone holds: the tracker of its backups and its settings listeners.
func (w *Workspace) release() {
	b := &w.backups
	b.mu.Lock()
	tracker, stop := b.tracker, b.stopSettings
	b.tracker, b.stopSettings = nil, nil
	b.mu.Unlock()
	if stop != nil {
		stop()
	}
	if tracker != nil {
		tracker.Dispose()
	}
	for _, stop := range w.stops {
		stop()
	}
	w.stops = nil
}

// ---- replacing the folder of a tab ----

// replaceWorkspace gives a tab another workspace, made for a folder (an empty path is none): the tab stays where it
// is and everything in it is made again, as at the start: the services of the old workspace are disposed and those of
// the new one created, and the state saved for the folder is restored. The caller closed the editors of the old
// workspace (and asked about their unsaved changes). The error is that of NewWorkspace.
func (m *Workspaces) replaceWorkspace(old *Workspace, folder, profile string) (*Workspace, error) {
	t := m.tabOf(old)
	if t == nil || !t.hosted || t.ws != old {
		return nil, errors.New("WorkspaceNotInATab", "the workspace is not shown in a tab of the window")
	}
	spec := WorkspaceSpec{Folder: folder, Profile: profile}
	if folder == "" && profile == "" {
		// Close Folder leaves the profile of the workspace as it is; a folder has the profile last used with it
		spec.Profile = old.ProfileName()
	}
	pw, err := m.prepare(m.ctxOr(), spec, old)
	if err != nil {
		return nil, err
	}
	next := pw.ws
	// the new workspace takes the place of the old one in the tab
	m.mu.Lock()
	old.tab = nil
	next.tab = t
	t.ws = next
	m.mu.Unlock()
	t.page = m.pageOf(pw)
	t.focus = nil
	next.controller = old.controller
	m.followUserSettings(next)
	// what the old workspace leaves is removed once its widgets are gone: its backups were taken with its editors
	old.commitClose(true)
	m.syncTabs()
	m.pushTitle(t)
	m.refreshHost()
	m.afterUnmount(old.release)
	m.changed()
	return next, nil
}

// ---- opening a folder ----

// FolderSource says where a request to open a folder comes from.
type FolderSource int

const (
	// FolderFromApp is a folder picked inside the application: Open Folder, Open Recent, the button of the empty
	// Explorer.
	FolderFromApp FolderSource = iota
	// FolderFromLaunch is a folder given outside the application: the command line, a second Emetos handing over.
	FolderFromLaunch
)

// FolderPlacement is where a folder opens.
type FolderPlacement int

const (
	// PlaceFocus makes the tab that has the folder the active one.
	PlaceFocus FolderPlacement = iota
	// PlaceNewTab opens a new tab for the folder.
	PlaceNewTab
	// PlaceSameTab replaces the folder of the active tab (after the questions about unsaved changes).
	PlaceSameTab
)

// folderRequest is what the placement of a folder depends on.
type folderRequest struct {
	// Setting is window.openFoldersInNewWindow.
	Setting string
	Source  FolderSource
	// ForceNew and ForceReuse are --new-window and --reuse-window.
	ForceNew, ForceReuse bool
	// Open is true when a tab has the folder already, the tab that would be replaced included.
	Open bool
	// TargetEmpty is true when the tab that would be replaced has no folder and no editors.
	TargetEmpty bool
}

// placeFolder tells where a folder opens. A folder that a tab has is never opened twice (the state id is made from the
// folder): that tab becomes the active one. --new-window and --reuse-window decide next; then
// window.openFoldersInNewWindow, read as "in a new tab": "on" opens a new tab, "off" replaces the tab, and "default" a
// new tab, unless the folder is picked from within the application, or the tab that would be replaced is empty.
//
// TODO(step 9b.3): the placement rules of SPEC §10.14 (Open Folder and Open Recent follow the setting as a launch does,
// and the Ctrl key forces a new tab). Until then a folder picked in the application replaces the active tab, as it
// replaced the folder of the window.
//
// Ported from VS Code: src/vs/platform/windows/electron-main/windowsMainService.ts (doOpenFolderOrWorkspace,
// shouldOpenNewWindow) and the setting window.openFoldersInNewWindow
func placeFolder(r folderRequest) FolderPlacement {
	switch {
	case r.Open:
		return PlaceFocus
	case r.ForceNew:
		return PlaceNewTab
	case r.ForceReuse:
		return PlaceSameTab
	}
	switch r.Setting {
	case "on":
		return PlaceNewTab
	case "off":
		return PlaceSameTab
	}
	if r.Source == FolderFromApp || r.TargetEmpty {
		return PlaceSameTab
	}
	return PlaceNewTab
}

// placeFiles tells whether files open in a new tab: --new-window and --reuse-window decide, then
// window.openFilesInNewWindow ("default" opens a new tab unless the files are picked from within the application).
//
// TODO(step 9b.3): files without a folder open in the active tab (SPEC §10.14).
//
// Ported from VS Code: src/vs/platform/windows/electron-main/windowsMainService.ts (shouldOpenNewWindow) and the
// setting window.openFilesInNewWindow
func placeFiles(setting string, source FolderSource, forceNew, forceReuse bool) (newTab bool) {
	switch {
	case forceNew:
		return true
	case forceReuse:
		return false
	}
	switch setting {
	case "on":
		return true
	case "default":
		return source == FolderFromLaunch
	}
	return false
}

// emptyWorkspace tells whether the workspace has no folder and nothing open.
func (w *Workspace) emptyWorkspace() bool {
	if w.Folder != "" {
		return false
	}
	for _, d := range w.Documents.All() {
		if d != nil {
			return false
		}
	}
	return true
}

// setting reads an application setting through the store of a workspace.
func (m *Workspaces) setting(w *Workspace, key string) string {
	if w == nil {
		return ""
	}
	return settings.Get[string](w.Settings, key, "")
}

// OpenFolder opens a folder as the request of workspace `from` (the one that asked; for a launch, the active one). An
// empty path closes the folder of the workspace. done is told on the UI thread, once the questions about unsaved
// changes are answered, whether the folder is open in a tab now (true also for a tab that had it already, which becomes
// the active one); it may be nil. The workspace that gets the folder is passed to done.
func (m *Workspaces) OpenFolder(from *Workspace, path string, src FolderSource, forceNew, forceReuse bool, done func(w *Workspace)) {
	m.openFolderWith(from, path, src, forceNew, forceReuse, "", done)
}

// openFolderWith is OpenFolder for a workspace that opens with the profile of the name (see WorkspaceSpec.Profile);
// empty is the profile last used with the folder. A folder that is open already keeps the profile of its tab.
func (m *Workspaces) openFolderWith(from *Workspace, path string, src FolderSource, forceNew, forceReuse bool, profile string, done func(w *Workspace)) {
	finish := func(w *Workspace) {
		if done != nil {
			done(w)
		}
	}
	if path != "" {
		path = filepath.Clean(path)
	}
	if path == "" {
		m.replaceFolder(from, "", "", finish)
		return
	}
	if from == nil {
		from = m.Active()
	}
	elsewhere := m.workspaceForFolder(path)
	req := folderRequest{
		Setting: m.setting(from, "window.openFoldersInNewWindow"), Source: src, ForceNew: forceNew, ForceReuse: forceReuse,
		Open: elsewhere != nil, TargetEmpty: from != nil && from.emptyWorkspace(),
	}
	switch placeFolder(req) {
	case PlaceFocus:
		elsewhere.Raise()
		finish(elsewhere)
	case PlaceNewTab:
		w, err := m.openFrom(WorkspaceSpec{Folder: path, Profile: profile}, from)
		if err != nil {
			m.folderFailed(from, path, err)
			finish(nil)
			return
		}
		finish(w)
	default:
		m.replaceFolder(from, path, profile, finish)
	}
}

// folderFailed tells the user that a folder could not be opened.
func (m *Workspaces) folderFailed(from *Workspace, path string, err error) {
	m.p.Logger.Warn("cannot open the folder", "path", path, "err", err)
	if from == nil {
		from = m.Active()
	}
	if from != nil {
		from.Notify.Error("Emetos", "Cannot open the folder "+path+": "+describe(err))
	}
}

// replaceFolder makes a workspace open another folder after the questions about unsaved changes. A workspace the
// manager shows in a tab is made again (replaceWorkspace); a workspace on its own changes its folder in place
// (Workspace.SetFolder).
func (m *Workspaces) replaceFolder(w *Workspace, path, profile string, done func(*Workspace)) {
	if w == nil {
		done(nil)
		return
	}
	if sameFolder(w.Folder, path) || (path == "" && w.Folder == "") {
		if path != "" {
			w.Raise()
		}
		done(w)
		return
	}
	s := w.shell
	if s == nil || !s.Mounted() {
		done(nil)
		return
	}
	// the state of the folder that is left stays as it is while its editors close
	s.suspendState()
	s.closeAllEditors(func(closed bool) {
		if !closed {
			s.resumeState()
			done(nil)
			return
		}
		if w.managed() {
			next, err := m.replaceWorkspace(w, path, profile)
			if err != nil {
				m.folderFailed(w, path, err)
				s.resumeState()
				done(nil)
				return
			}
			done(next)
			return
		}
		if err := w.SetFolder(w.ctx, path); err != nil {
			m.folderFailed(w, path, err)
			s.resumeState()
			done(nil)
			return
		}
		if profile != "" {
			// a workspace on its own keeps its services: it moves to the profile in place
			if _, _, err := m.p.Profiles.Ensure(w.ctx, profile); err != nil {
				m.folderFailed(w, path, err)
			} else if err := w.SwitchProfile(w.ctx, profile); err != nil {
				m.p.Logger.Warn("cannot switch the profile", "profile", profile, "err", err)
			}
		}
		s.resumeState()
		done(w)
	})
}

// ---- launches ----

// HandleLaunch carries out a launch (the command line of another Emetos, SPEC §10.11) in the right tab, on the UI
// thread. The tab that gets the folder is the one that has it (it becomes the active one), else a new tab or the active
// one per window.openFoldersInNewWindow, --new-window and --reuse-window; files go to that tab, or, with no folder, to
// a new tab (--new-window, window.openFilesInNewWindow) or to the tab that has their folder open, or the active tab.
// done is told as Workspace.HandleLaunch's is; the returned function stops waiting.
//
// TODO(step 9b.3): the placement rules of SPEC §10.14 for a launch; this is the placement of step 7.8 with tabs in
// place of windows.
func (m *Workspaces) HandleLaunch(args ipc.OpenArgs, done func(LaunchOutcome)) (cancel func()) {
	cancelled := false
	var inner func()
	cancel = func() {
		cancelled = true
		if inner != nil {
			inner()
		}
	}
	finish := func(o LaunchOutcome) { done(o) }
	deliver := func(w *Workspace, a ipc.OpenArgs) {
		if cancelled {
			return
		}
		w.whenReady(func() {
			if cancelled {
				return
			}
			inner = w.HandleLaunch(a, done)
		})
	}
	active := m.Active()
	if active == nil {
		finish(LaunchOutcome{ExitCode: 1, Message: "the window is not ready"})
		return cancel
	}
	// --profile (SPEC §10.12): a tab of another profile is not changed; the launch opens a new tab with the profile,
	// unless --reuse-window says to use the tab as it is
	profile := args.Profile
	needsProfile := func(w *Workspace) bool { return profile != "" && !args.ReuseWindow && !m.usesProfile(w, profile) }
	switch {
	case args.Folder != "":
		forceNew := args.NewWindow || (needsProfile(active) && !active.emptyWorkspace())
		m.openFolderWith(active, args.Folder, FolderFromLaunch, forceNew, args.ReuseWindow, profile, func(w *Workspace) {
			if w == nil {
				// the user cancelled the questions of the tab that was replaced, or it failed: the files
				// still open in the tab that asked
				a := args
				a.Folder = ""
				deliver(active, a)
				return
			}
			w.Raise()
			a := args
			a.Folder = w.Folder
			deliver(w, a)
		})
	case len(args.Files) > 0 || len(args.Diff) > 0 || len(args.Merge) > 0:
		target := m.workspaceForFiles(active, args)
		newTab := len(args.Files) > 0 && placeFiles(m.setting(active, "window.openFilesInNewWindow"), FolderFromLaunch, args.NewWindow, args.ReuseWindow)
		if len(args.Files) > 0 && needsProfile(target) {
			newTab = true
		}
		if newTab {
			w, err := m.openFrom(WorkspaceSpec{Profile: profile}, active)
			if err != nil {
				m.p.Logger.Warn("cannot open a tab for the files", "err", err)
				active.Notify.Error("Emetos", "Cannot open a new workspace tab: "+describe(err))
			} else {
				target = w
			}
		}
		target.Raise()
		deliver(target, args)
	default:
		// no folder and no files: a new tab with --new-window or a profile the tab does not use, else the active one
		// is shown
		target := active
		if (args.NewWindow && !args.ReuseWindow) || needsProfile(active) {
			if w, err := m.openFrom(WorkspaceSpec{Profile: profile}, active); err != nil {
				active.Notify.Error("Emetos", "Cannot open a new workspace tab: "+describe(err))
			} else {
				target = w
			}
		}
		target.Raise()
		deliver(target, args)
	}
	return cancel
}

// usesProfile tells whether a workspace uses the profile of a name (the profile need not exist: then it does not).
func (m *Workspaces) usesProfile(w *Workspace, name string) bool {
	profile, ok, err := m.p.Profiles.Lookup(m.ctxOr(), name)
	return err == nil && ok && profile == w.Profile()
}

// workspaceForFiles is the workspace that opens files without a folder: the one whose folder holds the first file,
// else the active one.
func (m *Workspaces) workspaceForFiles(active *Workspace, args ipc.OpenArgs) *Workspace {
	if len(args.Files) == 0 {
		return active
	}
	path := filepath.Clean(args.Files[0].Path)
	var best *Workspace
	for _, w := range m.All() {
		f := w.Folder
		if f != "" && (path == f || strings.HasPrefix(path, f+string(filepath.Separator))) {
			if best == nil || len(f) > len(best.Folder) {
				best = w
			}
		}
	}
	if best != nil {
		return best
	}
	return active
}

// Raise makes the tab of the workspace the active one and brings the window to the front.
func (w *Workspace) Raise() {
	if m := w.Process.Workspaces; m != nil {
		if t := m.tabOf(w); t != nil {
			m.Activate(t)
		}
	}
	if c := w.controller; c != nil {
		c.Activate()
	}
}

// whenReady runs f once the shell of the workspace is on screen (at once when it is).
func (w *Workspace) whenReady(f func()) {
	if w.launcher != nil {
		f()
		return
	}
	w.onReady = append(w.onReady, f)
}

// markReady runs what waited for the shell.
func (w *Workspace) markReady() {
	ready := w.onReady
	w.onReady = nil
	for _, f := range ready {
		f()
	}
}
