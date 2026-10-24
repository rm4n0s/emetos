package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
)

// Several windows in one process (SPEC §5.1, §10.10, §10.11).
//
// Windows is the manager of the windows of the process: the list of them in the order they were made, which one was
// active last, and what opens, replaces and closes a window. The first window is the one Glutter makes for RunApp
// (the "implicit" window); the others are regular windows made with a controller and shown by the WindowManager
// widget that is the root of the app. Everything the manager does to windows runs on the UI thread; the list is
// guarded by a mutex because the settings of one window are propagated to the others from the goroutine that
// changed them.
//
// What the process shares: the registries of settings, commands (as definitions), menus, views and default keys,
// the themes, the languages and grammars, the snippets, the UI state store (the recently opened lists and the
// state files) and the launcher. What each window has of its own: a fork of the command registry, the
// notifications, the settings store (with the layer of its folder), the documents, the groups, the layout, the quick
// input, the watcher, the file index, the backups and the state of the window.

// Texts of the notices about windows.
const (
	// WindowsUnavailableNotice is what a request for a new window says when the windows cannot be made (the app is not
	// running under the window manager, as in a test of a widget).
	WindowsUnavailableNotice = "A new window cannot be opened here."
)

// windowEntry is a window in the list of the manager.
type windowEntry struct {
	// seq numbers the windows in the order they were made; it never changes, even when the window opens another folder.
	seq int
	// win is the window now; opening another folder in the same Glutter window replaces it.
	win *Window
	// implicit is true for the first window of the process, the one Glutter made for RunApp.
	implicit bool
	// controller is the controller of the Glutter window; nil for a window that the manager does not host (the
	// tests of a window on its own).
	controller widgets.BaseWindowController
	// reg is the entry of the registry of the WindowManager of a regular window.
	reg *widgets.WindowEntry
	// host is the widget that shows the window; nil until it is mounted.
	host *windowHostState
	// page makes the page of the shell for the window the entry has now.
	page func() widgets.Widget
	// retired is true for the implicit window after its window was closed while others stay: its content is gone and
	// its Glutter window is hidden.
	retired bool
	// gone is true once the window left the list for good.
	gone bool
}

// view is the id of the view of the window.
func (e *windowEntry) view() ui.ViewID {
	if e.controller != nil {
		return e.controller.RootView().ViewID()
	}
	if e.implicit || e.host == nil {
		return ui.ImplicitViewID
	}
	return 0
}

// Windows is the manager of the windows of a process (Process.Windows).
type Windows struct {
	p *Process

	mu    sync.Mutex
	list  []*windowEntry // in the order the windows were made
	order []*windowEntry // in the order they were active, the most recent last
	seq   int

	// Quit, when set, ends the application in place of the binding (tests count its calls). A Window's own Quit
	// is asked first.
	Quit func()

	// The rest is used on the UI thread only.
	ctx      context.Context
	binding  *widgets.WidgetsBinding
	registry *widgets.WindowRegistry
	// pending are the windows to open once the first frame is up (the other windows of a restore).
	pending []WindowSpec
	// quitting is true from the first question of a quit until it is cancelled or the application ends.
	quitting bool
	observer *windowsObserver
	// hooked is true once the platform's close requests are handled.
	hooked bool
	// watch is true when the cancellation of ctx is the signal that ends the program (Run).
	watch bool
	// startActive is the folder of the window that is the active one again once the windows of a restore are open.
	startActive    string
	hasStartActive bool
}

func newWindows(p *Process) *Windows { return &Windows{p: p} }

// All lists the windows of the process in the order they were made.
func (m *Windows) All() []*Window {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Window, len(m.list))
	for i, e := range m.list {
		out[i] = e.win
	}
	return out
}

// Count is how many windows are open.
func (m *Windows) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.list)
}

// Active is the window that was active last, or nil when there is none.
func (m *Windows) Active() *Window {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.order) == 0 {
		return nil
	}
	return m.order[len(m.order)-1].win
}

// entries is a copy of the list.
func (m *Windows) entries() []*windowEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.list)
}

// others tells whether windows other than w are open.
func (m *Windows) others(w *Window) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.list {
		if e.win != w {
			return true
		}
	}
	return false
}

// register puts a window in the list, the first time the shell of the window is made; it becomes the active one.
func (m *Windows) register(w *Window) *windowEntry {
	m.mu.Lock()
	if w.entry != nil {
		e := w.entry
		m.mu.Unlock()
		return e
	}
	m.seq++
	e := &windowEntry{seq: m.seq, win: w}
	w.entry = e
	m.list = append(m.list, e)
	m.order = append(m.order, e)
	m.mu.Unlock()
	m.followUserSettings(w)
	m.syncOpenWindows()
	return e
}

// followUserSettings makes a change of the user's settings in one window reach the others: each window has a store
// of its own (it has the layer of its folder), all over the same settings.json, and the file watcher of each reloads
// it when the file changes; this does it at once, so that a window with no watcher (and a change made through
// one window's store) is not left behind. A reload that changes nothing tells nobody, which ends the chain.
func (m *Windows) followUserSettings(w *Window) {
	stop := w.Settings.OnChange(func(e settings.ChangeEvent) {
		if !slices.Contains(e.Targets, settings.TargetUser) {
			return
		}
		for _, other := range m.entries() {
			if other.win == w {
				continue
			}
			if _, err := other.win.Settings.Reload(m.ctxOr()); err != nil {
				m.p.Logger.Warn("reading the settings again failed", "err", err)
			}
		}
	})
	w.stops = append(w.stops, stop)
}

// unregister takes a window out of the list when its widgets are gone. The list of the open windows in the state
// is not changed: the windows that are open when the application quits stay in it (SPEC §10.10); the ones that
// the user closes leave it in removeWindow.
func (m *Windows) unregister(w *Window) (last bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := w.entry
	if e == nil {
		return len(m.list) == 0
	}
	w.entry = nil
	m.list = slices.DeleteFunc(m.list, func(x *windowEntry) bool { return x == e })
	m.order = slices.DeleteFunc(m.order, func(x *windowEntry) bool { return x == e })
	return len(m.list) == 0
}

// forget takes the entry of a window that closed out of the lists and marks it gone.
func (m *Windows) forget(e *windowEntry) {
	m.mu.Lock()
	m.list = slices.DeleteFunc(m.list, func(x *windowEntry) bool { return x == e })
	m.order = slices.DeleteFunc(m.order, func(x *windowEntry) bool { return x == e })
	e.gone = true
	if e.win != nil {
		e.win.entry = nil
	}
	m.mu.Unlock()
}

// setActive makes a window the active one and notes its folder as the one of the last window.
func (m *Windows) setActive(w *Window) {
	m.mu.Lock()
	e := w.entry
	if e == nil || e.gone {
		m.mu.Unlock()
		return
	}
	if n := len(m.order); n > 0 && m.order[n-1] == e {
		m.mu.Unlock()
		return
	}
	m.order = append(slices.DeleteFunc(m.order, func(x *windowEntry) bool { return x == e }), e)
	m.mu.Unlock()
	folder := w.Folder
	err := m.p.State.UpdateGlobal(m.ctxOr(), func(g *storage.GlobalState) bool {
		if g.LastWindow != nil && g.LastWindow.Folder == folder {
			return false
		}
		g.LastWindow = &storage.LastWindow{Folder: folder}
		return true
	})
	if err != nil {
		m.p.Logger.Warn("the global UI state could not be used", "err", err)
	}
}

// ctxOr is the context of the manager, or a background one before the application runs.
func (m *Windows) ctxOr() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

// syncOpenWindows writes the list of the open windows to the global state.
func (m *Windows) syncOpenWindows() {
	var list []storage.OpenWindow
	for _, e := range m.entries() {
		if id := e.win.stateID; id != "" {
			list = append(list, storage.OpenWindow{ID: id, Folder: e.win.Folder})
		}
	}
	err := m.p.State.UpdateGlobal(m.ctxOr(), func(g *storage.GlobalState) bool {
		if slices.Equal(g.OpenWindows, list) {
			return false
		}
		g.OpenWindows = list
		return true
	})
	if err != nil {
		m.p.Logger.Warn("the global UI state could not be used", "err", err)
	}
}

// managed tells whether the window is shown by the window manager of the process (Windows), not on its own.
func (w *Window) managed() bool { return w.entry != nil && w.entry.host != nil }

// folderNote is told by a window whose state id or folder changed.
func (m *Windows) folderNote(w *Window) {
	if w.entry != nil {
		m.syncOpenWindows()
	}
}

// windowForFolder is the window that has the folder open (compared through links), or nil.
func (m *Windows) windowForFolder(folder string) *Window {
	if folder == "" {
		return nil
	}
	for _, e := range m.entries() {
		if sameFolder(e.win.Folder, folder) {
			return e.win
		}
	}
	return nil
}

// windowOfView is the window that shows a view.
func (m *Windows) windowOfView(view ui.ViewID) *Window {
	for _, e := range m.entries() {
		if e.view() == view && !e.retired {
			return e.win
		}
	}
	return nil
}

// stateIDTaken tells whether a live window has the state id.
func (m *Windows) stateIDTaken(id string) bool {
	for _, e := range m.entries() {
		if e.win.stateID == id {
			return true
		}
	}
	return false
}

// freshEmptyID makes the id of the state of a window without a folder that has no state yet.
func (m *Windows) freshEmptyID() (string, error) {
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

// windowsObserver follows what the platform tells the whole application.
type windowsObserver struct {
	widgets.WidgetsBindingObserverBase
	m *Windows
}

// DidChangeViewFocus makes the window whose view got the keyboard focus the active one.
func (o *windowsObserver) DidChangeViewFocus(e ui.ViewFocusEvent) {
	if e.State != ui.ViewFocusStateFocused {
		return
	}
	if w := o.m.windowOfView(e.ViewID); w != nil {
		o.m.setActive(w)
	}
}

// DidRequestAppExit is a request to exit the application that comes from the application or the platform (not the
// close button of a window): it is a quit, with the questions of every window. The answer is always cancel; the
// application ends itself once every window has agreed.
func (o *windowsObserver) DidRequestAppExit() ui.AppExitResponse {
	o.m.RequestQuit(o.m.Active())
	return ui.AppExitResponseCancel
}

// bind connects the manager to the application: the close requests of the windows, the focus, the launcher and the
// windows that wait for the first frame. It is called by the first window that is shown (with the registry of the
// WindowManager above it, when there is one), and is safe to call again.
func (m *Windows) bind(binding *widgets.WidgetsBinding, registry *widgets.WindowRegistry) {
	if registry != nil && m.registry == nil {
		m.registry = registry
	}
	if m.binding != nil {
		return
	}
	m.binding = binding
	d := binding.PlatformDispatcher()
	// Glutter: the request to close the first (implicit) window is not handed to a controller's delegate; it is
	// given to PlatformDispatcher.OnExitRequested with the view ID, and the binding turns that into a request to exit
	// the app, whose observers get no view ID. The manager takes the hook over to tell the windows apart, and
	// answers the requests of the other views as the binding does. Glutter should hand the close request of the
	// implicit window to its controller's delegate (docs/GLUTTER-ISSUES.md).
	prev := d.OnExitRequested
	d.OnExitRequested = func(view ui.ViewID) ui.AppExitResponse {
		if w := m.windowOfView(view); w != nil {
			w.RequestClose(CloseReasonWindow)
			return ui.AppExitResponseCancel
		}
		if prev != nil {
			return prev(view)
		}
		return ui.AppExitResponseExit
	}
	m.hooked = true
	m.observer = &windowsObserver{m: m}
	binding.AddObserver(m.observer)
	if l := m.p.Launcher; l != nil {
		l.attach(m, binding.Post)
	}
	if m.watch {
		m.watchSignal(m.ctx, binding)
	}
	for _, e := range m.entries() {
		if e.implicit && e.controller == nil {
			if c, ok := m.implicitController(); ok {
				e.controller = c
				e.win.controller = c
			}
		}
	}
	if len(m.pending) > 0 || m.hasStartActive {
		binding.RunPostFrame(func(time.Duration) { m.openPending() })
	}
}

// watchSignal turns the cancellation of the context of Run (the signal that ends the program) into an exit that
// writes the backups and the state of every window first, without the questions about unsaved changes.
func (m *Windows) watchSignal(ctx context.Context, binding *widgets.WidgetsBinding) {
	go func() {
		<-ctx.Done()
		binding.Post(func() {
			for _, e := range m.entries() {
				if s := e.win.shell; s != nil && s.Mounted() {
					s.backupOnSignal()
					s.flushState()
				}
			}
			binding.ExitApplication(ui.AppExitTypeRequired)
		})
	}()
}

// ---- opening windows ----

// WindowSpec says how a window opens.
type WindowSpec struct {
	// Folder is the folder to open; empty for a window without one.
	Folder string
	// StateID is the id of the state of a window without a folder that comes back (from the list of the open
	// windows); empty makes a new one, which has no state to restore.
	StateID string
	// Files are opened in the window after its saved editors, the first of them shown.
	Files []string
	// Launch is the command line of the launch that opens the window, for the notices of the parts it does not do yet.
	Launch *ipc.OpenArgs
	// Steps are the dev commands to run once the window is up.
	Steps []DevStep
}

// preparedWindow is a window that is made and restored, and waits for its Glutter window.
type preparedWindow struct {
	win      *Window
	geometry *storage.Geometry
	docs     []*document.Document
	blocked  string
	steps    []DevStep
}

// prepare makes a window for a spec and restores its state; nothing is on screen yet. The error has the tags of
// NewWindow.
func (m *Windows) prepare(ctx context.Context, spec WindowSpec, from *Window) (*preparedWindow, error) {
	o := windowOptions{folder: spec.Folder, stateID: spec.StateID, from: from}
	if spec.Folder == "" && o.stateID == "" {
		id, err := m.freshEmptyID()
		if err != nil {
			m.p.Logger.Warn("the window keeps no UI state", "err", err)
		}
		o.stateID = id
	}
	win, err := newWindow(ctx, m.p, o)
	if err != nil {
		return nil, err
	}
	win.startup = spec.Launch
	geometry := win.PrepareRestore(ctx)
	docs, blocked := win.openStartupFiles(ctx, spec.Files)
	return &preparedWindow{win: win, geometry: geometry, docs: docs, blocked: blocked, steps: spec.Steps}, nil
}

// openStartupFiles opens the files of a command line in the window, before it is shown: a file that cannot be
// opened is reported, a file that looks binary is offered with a button in place of the editor.
func (w *Window) openStartupFiles(ctx context.Context, files []string) (shown []*document.Document, blocked string) {
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

// newEntry makes the entry of a prepared window and puts it in the list.
func (m *Windows) newEntry(pw *preparedWindow, implicit bool) *windowEntry {
	e := m.register(pw.win)
	e.implicit = implicit
	e.page = func() widgets.Widget {
		sh := newShell(m.ctxOr(), pw.docs, pw.steps)
		sh.blocked = pw.blocked
		return sh
	}
	return e
}

// start decides which windows open (the command line, the windows of the last session: startupPlan), makes the first
// one and notes the others to open with the first frame; the caller shows the first one (hostWidget). The error is
// that of NewWindow.
func (m *Windows) start(ctx context.Context, opts Options) (*windowEntry, *preparedWindow, error) {
	plan, last := m.p.startupPlan(ctx, opts)
	first, err := m.prepare(ctx, plan[0], nil)
	if err != nil {
		return nil, nil, err
	}
	entry := m.newEntry(first, true)
	m.pending = plan[1:]
	m.startActive, m.hasStartActive = last, true
	return entry, first, nil
}

// Open opens a window for the spec and returns it, or the window that has the folder already, which comes to the
// front. The window is shown with the next frame. It must run on the UI thread, with the application running. The
// error has the tags WindowsUnavailable and WindowCreateFailed, and those of NewWindow.
func (m *Windows) Open(spec WindowSpec) (*Window, error) {
	return m.openFrom(spec, m.Active())
}

func (m *Windows) openFrom(spec WindowSpec, from *Window) (*Window, error) {
	if m.binding == nil || m.registry == nil {
		return nil, errors.New("WindowsUnavailable", "the windows can only be opened while the application runs under its window manager")
	}
	if w := m.windowForFolder(spec.Folder); w != nil {
		w.Raise()
		return w, nil
	}
	pw, err := m.prepare(m.ctxOr(), spec, from)
	if err != nil {
		return nil, err
	}
	e := m.newEntry(pw, false)
	size, state := m.newWindowSize(pw.geometry, from)
	controller, err := m.binding.WindowingOwner().CreateRegularWindowController(size, widgets.RegularWindowControllerOpts{
		Title:    Title,
		Delegate: &windowDelegate{m: m, e: e},
	})
	if err != nil {
		m.forget(e)
		pw.win.release()
		pw.win.endBackups()
		return nil, errors.NewErr("WindowCreateFailed", err)
	}
	e.controller = controller
	pw.win.controller = controller
	e.reg = widgets.NewWindowEntry(controller, func(widgets.BuildContext) widgets.Widget { return newWindowHost(m, e) })
	if state.Maximized {
		controller.SetMaximized(true)
	} else if state.Fullscreen {
		controller.SetFullscreen(true)
	}
	m.registry.Register(e.reg)
	m.syncOpenWindows()
	m.setActive(pw.win)
	return pw.win, nil
}

// newWindowSize is the size a new window opens with: the saved size of its state, else what
// window.newWindowDimensions says, with the state (maximized or full screen) the window comes in. Wayland lets
// the compositor place windows, so "inherit" and "offset" are the same: the size of the active window.
//
// Ported from VS Code: src/vs/platform/windows/electron-main/windowsStateHandler.ts (getNewWindowState) and the
// setting window.newWindowDimensions
func (m *Windows) newWindowSize(saved *storage.Geometry, from *Window) (ui.Size, storage.Geometry) {
	if saved != nil && saved.Valid() {
		return ui.Size{Width: saved.Width, Height: saved.Height}, storage.Geometry{Maximized: saved.Maximized}
	}
	size := ui.Size{Width: defaultGeometry.Width, Height: defaultGeometry.Height}
	if from == nil {
		return size, storage.Geometry{}
	}
	switch settings.Get[string](from.Settings, "window.newWindowDimensions", "") {
	case "inherit", "offset":
		if e := from.entry; e != nil {
			if cs := e.currentSize(); cs.Width >= 1 && cs.Height >= 1 {
				size = cs
			}
		}
	case "maximized":
		return size, storage.Geometry{Maximized: true}
	case "fullscreen":
		return size, storage.Geometry{Fullscreen: true}
	}
	return size, storage.Geometry{}
}

// currentSize is the content size of the window of the entry, zero when unknown.
func (e *windowEntry) currentSize() ui.Size {
	if e.controller != nil {
		return e.controller.ContentSize()
	}
	return ui.Size{}
}

// openPending opens the windows that waited for the first frame; one that fails is reported in the first window.
func (m *Windows) openPending() {
	specs := m.pending
	m.pending = nil
	for _, spec := range specs {
		if _, err := m.Open(spec); err != nil {
			m.p.Logger.Warn("a window cannot be restored", "folder", spec.Folder, "err", err)
			if w := m.Active(); w != nil {
				w.Notify.Error("Emetos", "Cannot open a window"+folderSuffix(spec.Folder)+": "+describe(err))
			}
		}
	}
	// the window that was active last is the active one again
	if m.hasStartActive {
		m.hasStartActive = false
		if w := m.windowForFolder(m.startActive); w != nil {
			m.setActive(w)
		}
	}
}

// folderSuffix is " for <folder>" for a window with a folder.
func folderSuffix(folder string) string {
	if folder == "" {
		return ""
	}
	return " for " + folder
}

// NewWindow opens a window without a folder, with nothing in it and no state to restore, as New Window does
// (workbench.action.newWindow). The error is that of Open.
func (m *Windows) NewWindow() (*Window, error) { return m.Open(WindowSpec{}) }

// windowDelegate hands the platform's close request of a regular window to the window.
type windowDelegate struct {
	widgets.RegularWindowControllerDelegateBase
	m *Windows
	e *windowEntry
}

// OnWindowCloseRequested asks the window to close; the window destroys the Glutter window itself after the
// questions about unsaved changes are answered.
func (d *windowDelegate) OnWindowCloseRequested(*widgets.RegularWindowController) {
	if !d.e.gone {
		d.e.win.RequestClose(CloseReasonWindow)
	}
}

// OnWindowDestroyed takes the window out of the lists, when the platform destroyed it by itself.
func (d *windowDelegate) OnWindowDestroyed() {
	if !d.e.gone {
		d.m.removeWindow(d.e)
	}
}

// ---- the widget that shows a window ----

// windowHost shows the window of an entry in its Glutter window. When the entry gets another Window (opening a
// folder in the same window) the subtree is made again, so that the shell of the old window is disposed and the new
// one made, as if the window had opened.
type windowHost struct {
	widgets.StatefulWidget
	m *Windows
	e *windowEntry
}

func newWindowHost(m *Windows, e *windowEntry) *windowHost {
	return &windowHost{StatefulWidget: widgets.NewStatefulWidgetBase(), m: m, e: e}
}

// CreateState implements widgets.StatefulWidget.
func (h *windowHost) CreateState() widgets.State { return &windowHostState{} }

type windowHostState struct {
	widgets.StateOf[*windowHost]
}

// InitState connects the entry to the state.
func (s *windowHostState) InitState() { s.Widget().e.host = s }

// DidChangeDependencies binds the manager to the application the first time it has the registry of windows.
func (s *windowHostState) DidChangeDependencies() {
	w := s.Widget()
	w.m.bind(widgets.WidgetsBindingOf(s.Context()), widgets.WindowRegistryMaybeOf(s.Context()))
}

// Dispose lets go of the entry.
func (s *windowHostState) Dispose() {
	if e := s.Widget().e; e.host == s {
		e.host = nil
	}
}

// Build shows the page of the window, or nothing for a window whose content is gone.
func (s *windowHostState) Build(widgets.BuildContext) widgets.Widget {
	e := s.Widget().e
	if e.retired {
		return widgets.NewSizedBoxShrink(nil)
	}
	return widgets.NewKeyedSubtree(newAppWithPage(e.win, e.page), widgets.WidgetOpts{Key: foundation.NewObjectKey(e.win)})
}

// refresh makes the host build its subtree again.
func (s *windowHostState) refresh() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// hostWidget is the root of the application: the window manager around the host of the first window.
func (m *Windows) hostWidget(first *windowEntry) widgets.Widget {
	return widgets.NewWindowManager(newWindowHost(m, first))
}

// ---- closing ----

// closeResult is how the questions of a window that closes were answered.
type closeResult struct {
	// proceed is false when the user cancelled, or a request was already being answered.
	proceed bool
	// discard says that the backups of the window are removed (the changes were saved or given up); false keeps
	// them for the next start.
	discard bool
}

// closeAsk asks a window the questions of closing it, and tells done the answer; it closes nothing. A window with
// no shell on screen has nothing to lose.
func (w *Window) closeAsk(reason CloseReason, done func(closeResult)) {
	if w.closer == nil {
		done(closeResult{proceed: true})
		return
	}
	w.closer(reason, done)
}

// commitClose does what ends a window once its questions are answered: the backups are removed or kept, the state
// is written and the marker of the running window is taken away.
func (w *Window) commitClose(discard bool) {
	if discard {
		w.discardBackups()
	} else if t := w.Backups(); t != nil {
		t.Pause() // the editors close with the window: their backups stay
	}
	if w.flushState != nil {
		w.flushState()
	}
	w.endBackups()
}

// RequestClose is the window being asked to close, by the platform, Close Window or Quit (SPEC §10.10, §10.11).
// It never closes anything before the questions are answered: the caller keeps the window open.
//
// A window closes alone with CloseReasonWindow: when no document would lose changes, or files.hotExit leaves
// them to the backups (they are written first), it closes at once; else a dialog asks whether to save them. Save
// saves them (an untitled document through Save As) and closes; Don't Save closes; Cancel, or a save that fails or
// is cancelled, keeps the window. A window that closes while others stay is destroyed with its controller; the
// last one ends the application (Quit is called, or the application is asked to exit).
//
// CloseReasonQuit is the quit of the whole application: every window is asked in turn, in the order they were made,
// and the application ends only when all agreed; Cancel anywhere keeps every window. A request that comes while a
// dialog about closing is open is ignored.
func (w *Window) RequestClose(reason CloseReason) {
	m := w.Process.Windows
	if reason == CloseReasonQuit {
		m.RequestQuit(w)
		return
	}
	m.requestWindowClose(w)
}

// requestWindowClose closes one window.
func (m *Windows) requestWindowClose(w *Window) {
	if m.quitting {
		return
	}
	w.closeAsk(CloseReasonWindow, func(r closeResult) {
		if !r.proceed {
			return
		}
		m.finishClose(w, r.discard)
	})
}

// finishClose ends a window whose questions were answered: the application when it is the last one, else the window.
func (m *Windows) finishClose(w *Window, discard bool) {
	w.commitClose(discard)
	if !m.others(w) {
		m.exitApplication(w)
		return
	}
	if e := w.entry; e != nil {
		m.removeWindow(e)
	}
}

// RequestQuit quits the application: every window is asked in turn, in the order they were made, whether it may
// close (the questions about unsaved changes, or the backups of hot exit); when all agreed the windows write their
// state and the application ends. Cancel in any window stops it and keeps all of them. A second request while the
// questions go on is ignored. from is the window that asked.
func (m *Windows) RequestQuit(from *Window) {
	if m.quitting {
		return
	}
	entries := m.entries()
	if len(entries) == 0 {
		m.exitApplication(from)
		return
	}
	m.quitting = true
	results := make([]closeResult, len(entries))
	var step func(i int)
	step = func(i int) {
		if i == len(entries) {
			for j, e := range entries {
				e.win.commitClose(results[j].discard)
			}
			m.exitApplication(from)
			return
		}
		e := entries[i]
		if e.gone {
			step(i + 1)
			return
		}
		e.win.closeAsk(CloseReasonQuit, func(r closeResult) {
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

// exitApplication ends the application: the Quit function of the window that asked, or of the manager, or of a
// window that has one, else the application is asked to exit without asking the observers again. The state is
// written first, and the launcher stops answering.
func (m *Windows) exitApplication(from *Window) {
	m.quitting = true
	if err := m.p.State.Flush(context.WithoutCancel(m.ctxOr())); err != nil {
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
	for _, e := range m.entries() {
		if e.win.Quit != nil {
			seam(e.win.Quit)
			return
		}
	}
	for _, e := range m.entries() {
		if e.win.exit != nil {
			e.win.exit()
			return
		}
	}
	if m.binding != nil {
		m.binding.ExitApplication(ui.AppExitTypeRequired)
	}
}

// removeWindow takes a window that closed, while others stay, out of the lists and the screen: its Glutter window
// is destroyed (the implicit one is hidden, since Glutter cannot drop it), its widgets are disposed with the next
// frame, and the window leaves the list of the open windows. A window without a folder is gone for good: its state
// is deleted.
func (m *Windows) removeWindow(e *windowEntry) {
	if e.gone {
		return
	}
	w := e.win
	if w.Folder == "" {
		w.dropState = true
	}
	m.forget(e)
	m.syncOpenWindows()
	if next := m.Active(); next != nil {
		m.setActive(next)
	}
	switch {
	case e.host == nil && e.controller == nil:
		// a window of a test that nobody hosts: its widgets are the test's
	case e.implicit:
		e.retired = true
		if c, ok := m.implicitController(); ok {
			c.SetHidden(true)
		}
		if e.host != nil {
			e.host.refresh()
		}
	default:
		if m.registry != nil && e.reg != nil {
			m.registry.Unregister(e.reg)
		}
		if rc, ok := e.controller.(*widgets.RegularWindowController); ok {
			rc.Destroy()
		}
	}
	m.afterUnmount(func() {
		w.release()
		if w.dropState && w.stateID != "" {
			if err := m.p.State.DeleteWindow(w.stateID); err != nil {
				m.p.Logger.Warn("the state of a closed window cannot be removed", "err", err)
			}
		}
	})
}

// implicitController is the controller of the first window.
func (m *Windows) implicitController() (*widgets.RegularWindowController, bool) {
	if m.binding == nil {
		return nil, false
	}
	return m.binding.WindowingOwner().ImplicitWindowController()
}

// afterUnmount runs f after the frame that disposes the widgets of a window, or at once without a binding.
func (m *Windows) afterUnmount(f func()) {
	if m.binding == nil {
		f()
		return
	}
	m.binding.RunPostFrame(func(time.Duration) { f() })
}

// release lets go of what a window that is gone holds: the tracker of its backups and its settings listeners.
func (w *Window) release() {
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

// ---- replacing the folder of a window ----

// replaceWindow gives the entry of a window another window, made for a folder (an empty path is none): the Glutter
// window stays and everything in it is made again, as at the start: the services of the old window are disposed
// and those of the new one created, and the state saved for the folder is restored. The caller closed the editors
// of the old window (and asked about their unsaved changes). The error is that of NewWindow.
func (m *Windows) replaceWindow(old *Window, folder string) (*Window, error) {
	e := old.entry
	if e == nil || e.host == nil {
		return nil, errors.New("WindowsUnavailable", "the window is not shown by the window manager")
	}
	spec := WindowSpec{Folder: folder}
	pw, err := m.prepare(m.ctxOr(), spec, old)
	if err != nil {
		return nil, err
	}
	next := pw.win
	// the new window takes the place of the old one in the lists
	m.mu.Lock()
	old.entry = nil
	next.entry = e
	e.win = next
	m.mu.Unlock()
	e.page = func() widgets.Widget {
		sh := newShell(m.ctxOr(), pw.docs, pw.steps)
		sh.blocked = pw.blocked
		return sh
	}
	// what the old window leaves is removed once its widgets are gone: its backups were taken with its editors
	old.commitClose(true)
	m.syncOpenWindows()
	host := e.host
	host.refresh()
	m.afterUnmount(old.release)
	m.setActive(next)
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
	// PlaceFocus brings the window that has the folder to the front.
	PlaceFocus FolderPlacement = iota
	// PlaceNewWindow opens a new window for the folder.
	PlaceNewWindow
	// PlaceSameWindow replaces the folder of the window (after the questions about unsaved changes).
	PlaceSameWindow
)

// folderRequest is what the placement of a folder depends on.
type folderRequest struct {
	// Setting is window.openFoldersInNewWindow.
	Setting string
	Source  FolderSource
	// ForceNew and ForceReuse are --new-window and --reuse-window.
	ForceNew, ForceReuse bool
	// Open is true when a window has the folder already, the window that would be replaced included.
	Open bool
	// TargetEmpty is true when the window that would be replaced has no folder and no editors.
	TargetEmpty bool
}

// placeFolder tells where a folder opens. A folder that a window has is never opened twice (the window id is made
// from the folder): that window comes to the front. --new-window and --reuse-window decide next; then
// window.openFoldersInNewWindow: "on" opens a new window, "off" replaces the window, and "default" a new window,
// unless the folder is picked from within the application, or the window that would be replaced is empty.
//
// Ported from VS Code: src/vs/platform/windows/electron-main/windowsMainService.ts (doOpenFolderOrWorkspace,
// shouldOpenNewWindow) and the setting window.openFoldersInNewWindow
func placeFolder(r folderRequest) FolderPlacement {
	switch {
	case r.Open:
		return PlaceFocus
	case r.ForceNew:
		return PlaceNewWindow
	case r.ForceReuse:
		return PlaceSameWindow
	}
	switch r.Setting {
	case "on":
		return PlaceNewWindow
	case "off":
		return PlaceSameWindow
	}
	if r.Source == FolderFromApp || r.TargetEmpty {
		return PlaceSameWindow
	}
	return PlaceNewWindow
}

// placeFiles tells whether files open in a new window: --new-window and --reuse-window decide, then
// window.openFilesInNewWindow ("default" opens a new window unless the files are picked from within the
// application).
//
// Ported from VS Code: src/vs/platform/windows/electron-main/windowsMainService.ts (shouldOpenNewWindow) and the
// setting window.openFilesInNewWindow
func placeFiles(setting string, source FolderSource, forceNew, forceReuse bool) (newWindow bool) {
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

// emptyWindow tells whether the window has no folder and nothing open.
func (w *Window) emptyWindow() bool {
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

// setting reads an application setting through the store of a window.
func (m *Windows) setting(w *Window, key string) string {
	if w == nil {
		return ""
	}
	return settings.Get[string](w.Settings, key, "")
}

// OpenFolder opens a folder as the request of window `from` (the window that asked; for a launch, the active one).
// An empty path closes the folder of the window. done is told on the UI thread, once the questions about unsaved
// changes are answered, whether the folder is open in a window now (true also for a window that had it already, which
// comes to the front); it may be nil. The window that gets the folder is passed to done.
func (m *Windows) OpenFolder(from *Window, path string, src FolderSource, forceNew, forceReuse bool, done func(w *Window)) {
	finish := func(w *Window) {
		if done != nil {
			done(w)
		}
	}
	if path != "" {
		path = filepath.Clean(path)
	}
	if path == "" {
		m.replaceFolder(from, "", finish)
		return
	}
	if from == nil {
		from = m.Active()
	}
	elsewhere := m.windowForFolder(path)
	req := folderRequest{
		Setting: m.setting(from, "window.openFoldersInNewWindow"), Source: src, ForceNew: forceNew, ForceReuse: forceReuse,
		Open: elsewhere != nil, TargetEmpty: from != nil && from.emptyWindow(),
	}
	switch placeFolder(req) {
	case PlaceFocus:
		elsewhere.Raise()
		finish(elsewhere)
	case PlaceNewWindow:
		w, err := m.openFrom(WindowSpec{Folder: path}, from)
		if err != nil {
			m.folderFailed(from, path, err)
			finish(nil)
			return
		}
		finish(w)
	default:
		m.replaceFolder(from, path, finish)
	}
}

// folderFailed tells the user that a folder could not be opened.
func (m *Windows) folderFailed(from *Window, path string, err error) {
	m.p.Logger.Warn("cannot open the folder", "path", path, "err", err)
	if from == nil {
		from = m.Active()
	}
	if from != nil {
		from.Notify.Error("Emetos", "Cannot open the folder "+path+": "+describe(err))
	}
}

// replaceFolder makes a window open another folder after the questions about unsaved changes. A window the manager
// hosts is made again (replaceWindow); a window on its own changes its folder in place (Window.SetFolder).
func (m *Windows) replaceFolder(w *Window, path string, done func(*Window)) {
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
		if w.entry != nil && w.entry.host != nil {
			next, err := m.replaceWindow(w, path)
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
		s.resumeState()
		done(w)
	})
}

// ---- launches ----

// HandleLaunch carries out a launch (the command line of another Emetos, SPEC §10.11) in the right window, on the
// UI thread. The window that gets the folder is the one that has it (it comes to the front), else a new window or the
// last active one per window.openFoldersInNewWindow, --new-window and --reuse-window; files go to that window, or,
// with no folder, to a new window (--new-window, window.openFilesInNewWindow) or to the window that has their
// folder open, or the last active window. done is told as Window.HandleLaunch's is; the returned function stops
// waiting.
func (m *Windows) HandleLaunch(args ipc.OpenArgs, done func(LaunchOutcome)) (cancel func()) {
	cancelled := false
	var inner func()
	cancel = func() {
		cancelled = true
		if inner != nil {
			inner()
		}
	}
	finish := func(o LaunchOutcome) { done(o) }
	deliver := func(w *Window, a ipc.OpenArgs) {
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
	switch {
	case args.Folder != "":
		m.OpenFolder(active, args.Folder, FolderFromLaunch, args.NewWindow, args.ReuseWindow, func(w *Window) {
			if w == nil {
				// the user cancelled the questions of the window that was replaced, or it failed: the files
				// still open in the window that asked
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
		target := m.windowForFiles(active, args)
		if len(args.Files) > 0 && placeFiles(m.setting(active, "window.openFilesInNewWindow"), FolderFromLaunch, args.NewWindow, args.ReuseWindow) {
			w, err := m.openFrom(WindowSpec{}, active)
			if err != nil {
				m.p.Logger.Warn("cannot open a window for the files", "err", err)
				active.Notify.Error("Emetos", "Cannot open a new window: "+describe(err))
			} else {
				target = w
			}
		}
		target.Raise()
		deliver(target, args)
	default:
		// no folder and no files: a new window with --new-window, else the active one comes to the front
		target := active
		if args.NewWindow && !args.ReuseWindow {
			if w, err := m.openFrom(WindowSpec{}, active); err != nil {
				active.Notify.Error("Emetos", "Cannot open a new window: "+describe(err))
			} else {
				target = w
			}
		}
		target.Raise()
		deliver(target, args)
	}
	return cancel
}

// windowForFiles is the window that opens files without a folder: the one whose folder holds the first file, else
// the active one.
func (m *Windows) windowForFiles(active *Window, args ipc.OpenArgs) *Window {
	if len(args.Files) == 0 {
		return active
	}
	path := filepath.Clean(args.Files[0].Path)
	var best *Window
	for _, e := range m.entries() {
		f := e.win.Folder
		if f != "" && (path == f || strings.HasPrefix(path, f+string(filepath.Separator))) {
			if best == nil || len(f) > len(best.Folder) {
				best = e.win
			}
		}
	}
	if best != nil {
		return best
	}
	return active
}

// Raise brings the window to the front and makes it the active one.
func (w *Window) Raise() {
	if w.Process.Windows != nil {
		w.Process.Windows.setActive(w)
	}
	c := any(w.controller)
	if w.entry != nil && w.entry.controller != nil {
		c = w.entry.controller
	}
	if a, ok := c.(interface{ Activate() }); ok {
		a.Activate()
	}
}

// whenReady runs f once the shell of the window is on screen (at once when it is).
func (w *Window) whenReady(f func()) {
	if w.launcher != nil {
		f()
		return
	}
	w.onReady = append(w.onReady, f)
}

// markReady runs what waited for the shell.
func (w *Window) markReady() {
	ready := w.onReady
	w.onReady = nil
	for _, f := range ready {
		f()
	}
}
