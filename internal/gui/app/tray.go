package app

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The system tray and the close button of the window (SPEC §10.14, step 9b.4).
//
// The tray is Glutter's SystemTray around the tabs of the window (trayHost). Its menu, tooltip and label of the hide
// item are built from the manager of the tabs again whenever the tabs change. The close button reaches Emetos as the
// close request of the delegate of the implicit window (closeDelegate, set by Workspaces.bind): it hides the window to
// the tray, or quits as the request to exit the application does.
//
// A hidden window gets no frames (the application lifecycle is Hidden), and with no frames nothing is built: the tray
// is built again before the window is hidden (hideWindow), and again once it is shown.

// The labels of the tray's menu and its tooltip.
const (
	trayOpenFolderLabel = "Open Folder..."
	trayNewTabLabel     = "New Workspace Tab"
	trayShowLabel       = "Show Emetos"
	trayHideLabel       = "Hide Emetos"
	trayQuitLabel       = "Quit"
)

// trayState is what the manager of the tabs keeps of the tray and of hiding the window.
type trayState struct {
	// host is the widget that holds the tray while it is in the tree; nil otherwise (UI thread).
	host *trayHostState
	// failed is set once the tray could not be shown (TrayUnavailable and the other errors of the tray): the close
	// button quits from then on.
	failed atomic.Bool
	// hiding is true from the close button until the window is hidden (the backups are written, then a frame is built
	// with the tray's new menu); hidePending is true from the moment the tray says the window is hidden until it is.
	hiding, hidePending bool
	// gen counts the shows, so that a hide that waits for the backups gives way to a show that came meanwhile.
	gen int
}

// trayTags are the tags of the errors of Glutter's tray that say it is not shown.
var trayTags = map[string]bool{"TrayUnavailable": true, "TrayRegisterFailed": true, "TrayDuplicate": true, "TrayIconInvalid": true}

// rootWidget is the root of the application: the tabs of the window in the system tray.
func (m *Workspaces) rootWidget() widgets.Widget {
	icon, err := trayIcon()
	if err != nil {
		m.p.Logger.Warn("the tray has no icon picture", "err", err)
	}
	return &trayHost{StatefulWidget: widgets.NewStatefulWidgetBase(), m: m, child: m.hostWidget(), icon: icon}
}

// errorReporter is the reporter of the errors of the framework: the errors that say the tray is not shown are the
// manager's to note (logged once, no notification), the others go to next.
func (m *Workspaces) errorReporter(next foundation.ErrorReporter) foundation.ErrorReporter {
	return func(err error) {
		if appErr, ok := errors.FromError(err); ok && trayTags[appErr.Tag] {
			m.trayFailure(appErr)
			return
		}
		next(err)
	}
}

// trayFailure notes that the tray is not shown. It is called on the UI thread (Glutter reports the errors of the tray
// from there). The window, when it is hidden already, is shown, so that the user is never left without it.
func (m *Workspaces) trayFailure(appErr *errors.Error) {
	if !m.tray.failed.CompareAndSwap(false, true) {
		return
	}
	if appErr.Tag == "TrayUnavailable" {
		m.p.Logger.Info("no system tray is shown on this desktop; the close button of the window quits", "err", appErr)
	} else {
		m.p.Logger.Warn("the system tray cannot be shown; the close button of the window quits", "err", appErr)
	}
	if c, ok := m.implicitController(); ok && c.IsHidden() {
		m.showWindow(c)
	}
}

// trayShown tells whether a tray is shown: it is in the tree and nothing said it cannot be shown.
func (m *Workspaces) trayShown() bool { return m.tray.host != nil && !m.tray.failed.Load() }

// closeToTray tells whether the close button hides the window: a tray is shown and window.closeToTray is on.
func (m *Workspaces) closeToTray() bool {
	if !m.trayShown() {
		return false
	}
	if w := m.Active(); w != nil {
		return settings.Get[bool](w.Settings, "window.closeToTray", "")
	}
	return true
}

// closeDelegate gets the close button of the window from Glutter, which gives it to the delegate of the implicit
// window instead of making it a request to exit the application.
type closeDelegate struct{ m *Workspaces }

// OnWindowCloseRequested implements widgets.RegularWindowControllerDelegate.
func (d closeDelegate) OnWindowCloseRequested(c *widgets.RegularWindowController) {
	d.m.closeButton(c)
}

// OnWindowDestroyed implements widgets.RegularWindowControllerDelegate.
func (closeDelegate) OnWindowDestroyed() {}

// closeButton is what the close button does: hide the window to the tray when one is shown and window.closeToTray is
// on, else quit, with the questions of every tab. It does nothing while the questions of a quit go on (a dialog is
// open) or the window is going to be hidden already.
func (m *Workspaces) closeButton(c *widgets.RegularWindowController) {
	if m.quitting || m.tray.hiding {
		return
	}
	if m.closeToTray() {
		m.hideWindow(c)
		return
	}
	m.RequestQuit(m.Active())
}

// hideWindow takes the window off the screen, to the tray: the backups of hot exit are written first, then the tray is
// built with the label "Show Emetos" (a hidden window gets no frames, so it could not be built after), and then the
// window is hidden. The workspaces keep running. A show that comes before the window is hidden cancels the hide.
func (m *Workspaces) hideWindow(c *widgets.RegularWindowController) {
	if m.tray.hiding || c.IsHidden() {
		return
	}
	m.tray.hiding = true
	gen := m.tray.gen
	m.backupForHide(func() {
		if gen != m.tray.gen || m.tray.failed.Load() {
			m.tray.hiding = false
			return
		}
		m.tray.hidePending = true
		m.refreshTray()
		finish := func() {
			m.tray.hiding, m.tray.hidePending = false, false
			if gen == m.tray.gen && !m.tray.failed.Load() {
				c.SetHidden(true)
			}
		}
		b := m.binding
		if b == nil || !b.Scheduler().FramesEnabled() {
			finish() // no frame comes to build the tray
			return
		}
		b.RunPostFrame(func(time.Duration) { finish() })
		b.Scheduler().ScheduleFrame()
	})
}

// backupForHide writes the backups of the unsaved documents of every tab at once, when files.hotExit keeps them, and
// calls done on the UI thread. The texts are read here and written on a goroutine, bounded by backupExitTimeout; a
// failure is logged and the window hides anyway (nothing is lost: Emetos keeps running with the documents).
func (m *Workspaces) backupForHide(done func()) {
	type job struct {
		w     *Workspace
		batch *storage.BackupBatch
	}
	var jobs []job
	for _, w := range m.All() {
		t := w.Backups()
		if t == nil || !t.Enabled() {
			continue
		}
		if b := t.Prepare(w.Documents.All()); b.Len() > 0 {
			jobs = append(jobs, job{w, b})
		}
	}
	if len(jobs) == 0 {
		done()
		return
	}
	write := func() []error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(m.ctxOr()), backupExitTimeout)
		defer cancel()
		var errs []error
		for _, j := range jobs {
			if err := j.batch.Write(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		return errs
	}
	finish := func(errs []error) {
		for _, err := range errs {
			m.p.Logger.Warn("the unsaved documents cannot be backed up before the window is hidden", "err", err)
		}
		done()
	}
	b := m.binding
	if b == nil {
		finish(write())
		return
	}
	go func() {
		errs := write()
		b.Post(func() { finish(errs) })
	}()
}

// windowVisible tells whether the window is on the screen (and not about to leave it): the label of the tray's hide
// item.
func (m *Workspaces) windowVisible() bool {
	if m.tray.hidePending {
		return false
	}
	c, ok := m.implicitController()
	return !ok || !c.IsHidden()
}

// refreshTray builds the tray again (a frame does it while the window is shown).
func (m *Workspaces) refreshTray() {
	if h := m.tray.host; h != nil && h.Mounted() {
		h.SetState(func() {})
	}
}

// trayClick is the click on the icon: it shows the window and brings it to the front when it is hidden or not in
// front, and hides it when it is in front.
func (m *Workspaces) trayClick() {
	c, ok := m.implicitController()
	if !ok {
		m.showWindow(nil)
		return
	}
	if c.IsHidden() || !c.IsActivated() {
		m.showWindow(c)
		return
	}
	m.hideWindow(c)
}

// trayToggle is the Show Emetos / Hide Emetos item: it shows the window, or hides it, whether it is in front or not.
func (m *Workspaces) trayToggle() {
	c, ok := m.implicitController()
	if !ok || !m.windowVisible() {
		m.showWindow(nil)
		return
	}
	m.hideWindow(c)
}

// trayShowTab is a tab of the tray's menu: the tab becomes the active one and the window is shown.
func (m *Workspaces) trayShowTab(t *WindowTab) {
	m.Activate(t)
	m.showWindow(nil)
}

// trayOpenFolder is the item Open Folder...: the folder dialog of the active tab (the command of the "+" menu of the
// strip), with the window shown.
func (m *Workspaces) trayOpenFolder() {
	w := m.Active()
	if w == nil {
		return
	}
	m.showWindow(nil)
	if err := w.runCommand("emetos.workspaceTab.openFolder", nil); err != nil {
		m.p.Logger.Warn("the tray cannot open the folder dialog", "err", err)
		w.Notify.Error("Emetos", "Cannot show the dialog to open a folder: "+describe(err))
	}
}

// trayNewTab is the item New Workspace Tab: a tab without a folder, with the window shown.
func (m *Workspaces) trayNewTab() {
	if _, err := m.NewTab(); err != nil {
		m.p.Logger.Warn("the tray cannot open a workspace tab", "err", err)
		if w := m.Active(); w != nil {
			w.Notify.Error("Emetos", "Cannot open a new workspace tab: "+describe(err))
		}
	}
	m.showWindow(nil)
}

// trayMenu is the menu of the tray: the tabs, then Open Folder... and New Workspace Tab, then Show or Hide Emetos and
// Quit. The tab of the active workspace is marked.
func (m *Workspaces) trayMenu() []desktop.TrayMenuEntry {
	var entries []desktop.TrayMenuEntry
	active := m.ActiveTab()
	tabs := m.Tabs()
	for i, tab := range m.workspaceTabs() {
		t := tabs[i]
		label := workbench.TabLabel{Name: tab.Name, Description: tab.Description}.Text()
		entries = append(entries, desktop.NewTrayMenuItem(label, func() { m.trayShowTab(t) },
			desktop.TrayMenuItemOpts{Radio: true, Checked: t == active}))
	}
	toggle := trayHideLabel
	if !m.windowVisible() {
		toggle = trayShowLabel
	}
	return append(entries,
		desktop.NewTrayMenuSeparator(),
		desktop.NewTrayMenuItem(trayOpenFolderLabel, m.trayOpenFolder),
		desktop.NewTrayMenuItem(trayNewTabLabel, m.trayNewTab),
		desktop.NewTrayMenuSeparator(),
		desktop.NewTrayMenuItem(toggle, m.trayToggle),
		desktop.NewTrayMenuItem(trayQuitLabel, func() { m.RequestQuit(m.Active()) }),
	)
}

// trayTooltipBody is the line under "Emetos" in the tooltip: how many workspaces are open.
func (m *Workspaces) trayTooltipBody() string {
	n := m.Count()
	if n == 1 {
		return "1 workspace"
	}
	return strconv.Itoa(n) + " workspaces"
}

// trayHost holds the system tray around the tabs of the window.
type trayHost struct {
	widgets.StatefulWidget
	m     *Workspaces
	child widgets.Widget
	icon  ui.WindowIcon
}

// CreateState implements widgets.StatefulWidget.
func (h *trayHost) CreateState() widgets.State { return &trayHostState{} }

type trayHostState struct {
	widgets.StateOf[*trayHost]
	stop func()
}

// InitState connects the manager to the state and follows the tabs.
func (s *trayHostState) InitState() {
	m := s.Widget().m
	m.tray.host = s
	s.stop = m.OnChange(s.refresh)
}

// Dispose lets go of the manager.
func (s *trayHostState) Dispose() {
	if s.stop != nil {
		s.stop()
	}
	if m := s.Widget().m; m.tray.host == s {
		m.tray.host = nil
	}
}

// refresh builds the tray again.
func (s *trayHostState) refresh() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// Build is the tray around the tabs: its tooltip and menu are the ones of the tabs now.
func (s *trayHostState) Build(widgets.BuildContext) widgets.Widget {
	h := s.Widget()
	m := h.m
	return desktop.NewSystemTray(h.icon, h.child, desktop.SystemTrayOpts{
		ID: AppID, Title: Title, Tooltip: Title, TooltipBody: m.trayTooltipBody(),
		OnActivate: m.trayClick, Menu: m.trayMenu(),
	})
}
