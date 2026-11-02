package app

import (
	"os"
	"slices"
	"sync/atomic"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	dbgview "github.com/rm4n0s/emetos/internal/gui/views/debug"
	"github.com/rm4n0s/emetos/internal/gui/views/welcome"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The Run and Debug view of a screen (SPEC §10.31, step 25.4): the container with its views, the toolbar, the status bar color and the launch
// item, over the debug service of the workspace. The host's window and the view of each client of a hosted workspace (D46) each have one of
// these, built from their own shell over the same service, so they show the same sessions; what a screen asks (a start, a prompt) is
// answered on that screen. The service and its callbacks are in debug.go, the commands in debugcommands.go.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugViewlet.ts, debugToolBar.ts, debugStatus.ts,
// statusbarColorProvider.ts and debugContextKeys (debugService.ts)

// debugShell is what the shell keeps of the Run and Debug view.
type debugShell struct {
	panes *dbgview.Panes
	// menu opens the context menus of the views and the menu behind the toolbar's buttons.
	menu *kit.ContextMenuController
	// stops end what the view follows, and removeCommands give its commands back.
	stops          []func()
	removeCommands []func()
	// focus tells which views of the container have the keyboard focus.
	focus map[string]bool
	// status is the launch item of the status bar.
	status *workbench.StatusAccessor
	// toolbarX is where the center of the floating toolbar is across the editor area.
	toolbarX float64
	// debugging is what the status bar shows now, to tell a change.
	debugging bool
	// stopped is the thread whose stop the window has shown, so that a stop is shown once.
	stopped *debug.Thread
}

// debugSettingsKeys are the settings the view reads while it draws or acts.
var debugSettingsKeys = []string{"debug.showVariableTypes", "debug.toolBarLocation", "debug.showInStatusBar", "debug.enableStatusBarColor",
	"debug.breakpointsView.presentation", "debug.hideLauncherWhileDebugging", "debug.showSubSessionsInToolBar"}

// debugContainerVisible tells whether the container of Run and Debug is the one shown in the side bar.
func (s *shellState) debugContainerVisible() bool {
	return s.win.Views.Active() == command.ViewContainerDebug
}

// debugViewVisible tells whether a view of the container is on screen: its container is open, and the view is neither folded nor hidden. A
// view that is not asks the adapter for nothing.
func (s *shellState) debugViewVisible(viewID string) bool {
	v := s.win.Views
	return v.Active() == command.ViewContainerDebug && !v.IsCollapsed(viewID) && !v.IsHidden(viewID)
}

// initDebug makes the views of Run and Debug over the debug service and follows it.
func (s *shellState) initDebug(binding *widgets.WidgetsBinding) {
	win := s.win
	svc := win.DebugService()
	if svc == nil {
		return
	}
	st := &s.debug
	st.focus = map[string]bool{}
	st.menu = kit.NewContextMenuController()
	st.toolbarX = dbgview.DefaultToolbarX
	st.panes = dbgview.NewPanes(dbgview.Opts{
		Ctx: win.ctx, Service: svc, Post: binding.Post, Logger: win.Process.Logger,
		ShowTypes:    func() bool { return settings.Get[bool](win.Settings, "debug.showVariableTypes", "") },
		Presentation: func() string { return settings.Get[string](win.Settings, "debug.breakpointsView.presentation", "") },
		Visible:      s.debugViewVisible,
	})
	st.panes.Breakpoints.SetFolder(win.Folder)
	st.stops = append(st.stops, st.panes.Dispose)
	// pending is set while a burst of events waits to be applied. It is not a field of s.debug: the model may still be telling an event
	// on its goroutine while disposeDebug clears s.debug on the UI thread.
	pending := new(atomic.Bool)
	st.stops = append(st.stops, svc.Subscribe(func(ev debug.Event) { s.onDebugEvent(binding, pending, ev) }))
	st.stops = append(st.stops, win.Views.OnChange(func() {
		binding.Post(func() {
			if s.Mounted() && s.debug.panes != nil {
				s.debug.panes.VisibilityChanged()
			}
		})
	}))
	st.stops = append(st.stops, win.Settings.OnChange(func(e settings.ChangeEvent) {
		for _, key := range debugSettingsKeys {
			if e.Affects(key, "") {
				binding.Post(func() {
					if s.Mounted() && s.debug.panes != nil {
						s.debug.panes.Refresh()
						s.syncDebugParts()
					}
				})
				return
			}
		}
	}))
	s.bindDebugCommands(binding)
	s.bindDevDebug()
	s.syncDebugParts()
	if !win.isView() {
		// the launches are read after the saved state was put back (restoreDebug), so that the saved choice of configuration applies
		win.reloadLaunches()
	}
}

// disposeDebug ends the views and gives the commands back.
func (s *shellState) disposeDebug() {
	st := &s.debug
	for _, stop := range st.stops {
		stop()
	}
	for _, remove := range st.removeCommands {
		remove()
	}
	if st.status != nil {
		st.status.Dispose()
	}
	*st = debugShell{}
}

// resetDebugParts takes the keys of the view away; the panels have stopped following the views of the window by then.
func (s *shellState) resetDebugParts() {
	win := s.win
	if win == nil || win.DebugService() == nil {
		return
	}
	win.Keys.Batch(func() {
		for _, key := range debugContextKeys {
			win.Keys.Reset(key)
		}
	})
	win.Views.SetBadge(command.ViewContainerDebug, workbench.ActivityBadge{})
}

// onDebugEvent follows the service: a burst of events is applied once on the UI thread, and a stop is shown (the source opens) at once.
func (s *shellState) onDebugEvent(binding *widgets.WidgetsBinding, pending *atomic.Bool, ev debug.Event) {
	switch ev.Kind {
	case debug.EventStopped:
		binding.Post(func() {
			if s.Mounted() && s.debug.panes != nil {
				s.debugStopped(binding, ev)
			}
		})
	case debug.EventSessionStarted:
		binding.Post(func() {
			if s.Mounted() && s.debug.panes != nil {
				s.debugSessionStarted(ev)
			}
		})
	}
	if pending.Swap(true) {
		return
	}
	binding.Post(func() {
		pending.Store(false)
		if s.Mounted() && s.debug.panes != nil {
			s.syncDebugParts()
			if ev.Kind.AffectsState() {
				s.stateChangedIfMounted()
			}
		}
	})
}

// debugSessionStarted opens Run and Debug when a session starts, as debug.openDebug says.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts (the openDebug setting in onDidNewSession)
func (s *shellState) debugSessionStarted(ev debug.Event) {
	win := s.win
	if ev.Session != nil && ev.Session.Options().SuppressDebugView {
		return
	}
	if settings.Get[string](win.Settings, "debug.openDebug", "") == "openOnSessionStart" {
		win.Views.Open(command.ViewContainerDebug)
	}
}

// debugStopped shows a stop: Run and Debug opens (debug.openDebug openOnDebugBreak), and the source of the top frame opens in the
// editor when debug.focusEditorOnBreak says so. The window is not brought to the front (debug.focusWindowOnBreak): Glutter gives a
// window no way to ask for the focus of the desktop (decision 1196).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugSession.ts and debugService.ts (the handling of a stopped thread)
func (s *shellState) debugStopped(binding *widgets.WidgetsBinding, ev debug.Event) {
	win := s.win
	if ev.Thread == nil || ev.Thread == s.debug.stopped && ev.Session == nil {
		return
	}
	sess := ev.Thread.Session()
	if !sess.Options().SuppressDebugView {
		switch settings.Get[string](win.Settings, "debug.openDebug", "") {
		case "openOnDebugBreak", "openOnSessionStart":
			win.Views.Open(command.ViewContainerDebug)
		}
	}
	if !settings.Get[bool](win.Settings, "debug.focusEditorOnBreak", "") {
		return
	}
	if f := win.DebugService().Model().Focus(); f.Frame != nil && f.Frame.Thread == ev.Thread {
		s.debugShowFrame(binding, f.Frame, true)
	} else if top := ev.Thread.TopStackFrame(); top != nil {
		s.debugShowFrame(binding, top, true)
	}
}

// debugShowFrame opens the source of a frame at its line, when its file is there. A source the adapter serves (it has no file) is left to step 25.5, which shows
// those in a read-only editor (decision 1197).
func (s *shellState) debugShowFrame(binding *widgets.WidgetsBinding, f *debug.StackFrame, preserveFocus bool) {
	if f == nil || f.Source.Path == "" || !f.Source.Available {
		return
	}
	// a source that is not on this computer (the frames of the runtime of a remote debuggee) is not opened: the row says where it is
	if _, err := os.Stat(f.Source.Path); err != nil {
		return
	}
	s.openPathAt(binding, f.Source.Path, f.Range.Line, f.Range.Column, openOptions{Preview: true, PreserveFocus: preserveFocus, TakeFocus: !preserveFocus})
}

// ---- context keys

// debugContextKeys are the context keys the view sets.
var debugContextKeys = []string{"debugState", "inDebugMode", "debugType", "debugConfigurationType", "debugUx", "hasDebugged",
	"debuggersAvailable", "debugExtensionAvailable", "debuggerInterestedInActiveEditor", "focusedSessionIsAttach", "focusedSessionIsNoDebug",
	"restartFrameSupported", "stackFrameSupportsRestart", "jumpToCursorSupported", "stepIntoTargetsSupported", "breakpointsExist",
	"watchExpressionsExist", "loadedScriptsSupported", "multiSessionDebug", "threadHasMultipleStackFrames", "terminateDebuggeeSupported",
	"suspendDebuggeeSupported", "terminateThreadsSupported", "debugSetVariableSupported", "debugSetExpressionSupported", "stepBackSupported",
	"breakpointsDeactivated", "callStackItemType", "watchItemType", "breakpointItemType", "variableEvaluateNamePresent", "variableIsReadonly",
	"expressionSelected", "breakpointInputFocused", "variablesFocused", "watchExpressionsFocused", "callStackFocused", "breakpointsFocused",
	"loadedScriptsFocused", "debugViewletFocus"}

// syncDebugParts applies the state of the debugger to the parts of the window that show it: the context keys, the badge of the activity
// bar icon, the status bar (its color and the launch item) and the buttons of the container.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts (the context keys), debugStatus.ts and
// statusbarColorProvider.ts
func (s *shellState) syncDebugParts() {
	win := s.win
	svc := win.DebugService()
	if svc == nil || s.debug.panes == nil {
		return
	}
	m := svc.Model()
	state := svc.State()
	f := m.Focus()
	sessions := m.Sessions(false)
	sel := svc.Launches().Selected()
	debuggers := 0
	if win.Debug != nil {
		debuggers = len(win.Debug.Debuggers())
	}
	var caps debugCaps
	if f.Session != nil {
		caps = capsOf(f.Session)
	}
	multipleFrames := f.Thread != nil && len(f.Thread.CallStack()) > 1
	breakpoints := len(m.Breakpoints(debug.BreakpointFilter{})) + len(m.FunctionBreakpoints())
	ux := "simple"
	if (state != debug.Inactive && state != debug.Initializing) || (debuggers > 0 && sel.Name != "") {
		ux = "default"
	}
	interested := false
	if win.Debug != nil && s.Mounted() {
		if e := s.activeEditor(); e != nil && e.doc != nil {
			interested = len(win.Debug.DebuggersForLanguage(e.doc.LanguageID())) > 0 ||
				win.Debug.AllowsBreakpoints(e.doc.LanguageID(), settings.Get[bool](win.Settings, "debug.allowBreakpointsEverywhere", ""))
		}
	}
	debugType := ""
	attach, noDebug := false, false
	if f.Session != nil {
		debugType = f.Session.Type()
		attach = f.Session.IsAttach()
		noDebug = f.Session.Configuration().NoDebug()
	}
	frameRestart := f.Frame != nil && f.Frame.CanRestart
	win.Keys.Batch(func() {
		k := win.Keys
		k.Set("debugState", state.String())
		k.Set("inDebugMode", state != debug.Inactive)
		k.Set("debugType", debugType)
		k.Set("debugConfigurationType", sel.Type)
		k.Set("debugUx", ux)
		k.Set("hasDebugged", svc.HasDebugged())
		k.Set("debuggersAvailable", debuggers > 0)
		k.Set("debugExtensionAvailable", debuggers > 0)
		k.Set("debuggerInterestedInActiveEditor", interested)
		k.Set("focusedSessionIsAttach", attach)
		k.Set("focusedSessionIsNoDebug", noDebug)
		k.Set("restartFrameSupported", caps.restartFrame)
		k.Set("stackFrameSupportsRestart", frameRestart)
		k.Set("jumpToCursorSupported", caps.gotoTargets)
		k.Set("stepIntoTargetsSupported", caps.stepInTargets)
		k.Set("breakpointsExist", breakpoints > 0)
		k.Set("watchExpressionsExist", len(m.WatchExpressions()) > 0)
		k.Set("loadedScriptsSupported", caps.loadedSources)
		k.Set("multiSessionDebug", len(sessions) > 1)
		k.Set("threadHasMultipleStackFrames", multipleFrames)
		k.Set("terminateDebuggeeSupported", caps.terminateDebuggee)
		k.Set("suspendDebuggeeSupported", caps.suspendDebuggee)
		k.Set("terminateThreadsSupported", caps.terminateThreads)
		k.Set("debugSetVariableSupported", caps.setVariable)
		k.Set("debugSetExpressionSupported", caps.setExpression)
		k.Set("stepBackSupported", caps.stepBack)
		k.Set("breakpointsDeactivated", !m.BreakpointsActivated())
	})
	s.syncDebugRowKeys()
	// the status bar takes the colors of a session
	debugging := settings.Get[bool](win.Settings, "debug.enableStatusBarColor", "") && dbgview.InDebugMode(state, sessions)
	s.debug.debugging = debugging
	s.syncDebugStatus(svc, state)
	s.SetState(func() {})
}

// debugCaps are the capabilities of the focused session that the keys report.
type debugCaps struct {
	restartFrame, gotoTargets, stepInTargets, loadedSources, terminateDebuggee, suspendDebuggee, terminateThreads, setVariable, setExpression, stepBack bool
}

// capsOf reads the capabilities of a session as the keys want them.
func capsOf(sess *debug.Session) debugCaps {
	c := sess.Capabilities()
	return debugCaps{
		restartFrame: c.SupportsRestartFrame, gotoTargets: c.SupportsGotoTargetsRequest, stepInTargets: c.SupportsStepInTargetsRequest,
		loadedSources: c.SupportsLoadedSourcesRequest, terminateDebuggee: c.SupportTerminateDebuggee || c.SupportsTerminateRequest,
		suspendDebuggee: c.SupportSuspendDebuggee, terminateThreads: c.SupportsTerminateThreadsRequest, setVariable: c.SupportsSetVariable,
		setExpression: c.SupportsSetExpression, stepBack: c.SupportsStepBack,
	}
}

// syncDebugRowKeys sets the keys of the row the focused view selects.
func (s *shellState) syncDebugRowKeys() {
	win := s.win
	st := &s.debug
	if st.panes == nil {
		return
	}
	viewID, _ := win.Keys.Get("focusedView").(string)
	var item map[string]any
	call, watch, bp := "", "", ""
	switch viewID {
	case command.ViewDebugCallStack:
		if r, ok := st.panes.CallStack.Focused(); ok {
			call = r.ItemType()
			item = s.debugRowKeys(viewID, r)
		}
	case command.ViewDebugWatch:
		if r, ok := st.panes.Watch.Focused(); ok {
			watch = r.ItemType()
			item = s.debugRowKeys(viewID, r)
		}
	case command.ViewDebugVariables:
		if r, ok := st.panes.Variables.Focused(); ok {
			item = s.debugRowKeys(viewID, r)
		}
	case command.ViewDebugBreakpoints:
		if r, ok := st.panes.Breakpoints.Focused(); ok {
			bp = r.ItemType()
		}
	}
	win.Keys.Batch(func() {
		win.Keys.Set("callStackItemType", call)
		win.Keys.Set("watchItemType", watch)
		win.Keys.Set("breakpointItemType", bp)
		win.Keys.Set("variableEvaluateNamePresent", item["variableEvaluateNamePresent"] == true)
		win.Keys.Set("variableIsReadonly", item["variableIsReadonly"] == true)
		win.Keys.Set("expressionSelected", st.panes.Watch.Editing() || st.panes.Variables.Editing())
		win.Keys.Set("breakpointInputFocused", st.panes.Breakpoints.Editing())
	})
}

// debugRowKeys are the keys of a row, for the menu of the row and for the keys of the focused row.
func (s *shellState) debugRowKeys(viewID string, r dbgview.Row) map[string]any {
	extra := map[string]any{"view": viewID}
	switch r.Kind {
	case dbgview.RowSession:
		extra["callStackItemType"] = "session"
		extra["focusedSessionIsAttach"] = r.Session.IsAttach()
		caps := capsOf(r.Session)
		extra["terminateDebuggeeSupported"] = caps.terminateDebuggee
		extra["suspendDebuggeeSupported"] = caps.suspendDebuggee
	case dbgview.RowThread:
		extra["callStackItemType"] = "thread"
		extra["threadHasMultipleStackFrames"] = len(r.Thread.CallStack()) > 1
		extra["terminateThreadsSupported"] = capsOf(r.Thread.Session()).terminateThreads
	case dbgview.RowFrame:
		extra["callStackItemType"] = "stackFrame"
		extra["stackFrameSupportsRestart"] = r.Frame.CanRestart
		extra["restartFrameSupported"] = capsOf(r.Frame.Thread.Session()).restartFrame
	case dbgview.RowExpression:
		extra["watchItemType"] = "expression"
		extra["variableEvaluateNamePresent"] = true
		extra["variableIsReadonly"] = false
	case dbgview.RowVariable:
		extra["watchItemType"] = "variable"
		extra["variableEvaluateNamePresent"] = r.Variable.EvaluateName != ""
		extra["variableIsReadonly"] = isReadonly(r.Variable.PresentationHint())
	case dbgview.RowScope:
		extra["watchItemType"] = "variable"
		extra["variableEvaluateNamePresent"] = false
		extra["variableIsReadonly"] = true
	case dbgview.RowBreakpoint, dbgview.RowFunctionBreakpoint, dbgview.RowException:
		extra["breakpointItemType"] = r.ItemType()
	}
	return extra
}

// ---- the views

// debugBody is the body of a view of the container, or nil for a view that is not one of Run and Debug.
func (s *shellState) debugBody(viewID string) widgets.Widget {
	st := &s.debug
	if st.panes == nil {
		return nil
	}
	var pane dbgview.TreePane
	opts := dbgview.BodyOpts{Menu: func(r dbgview.Row, ok bool, at ui.Offset) { s.showDebugMenu(viewID, r, ok, at) }}
	switch viewID {
	case command.ViewDebugWelcome:
		return s.debugWelcome()
	case command.ViewDebugVariables:
		pane = st.panes.Variables
		opts.Select = func(r dbgview.Row) { s.debugRowSelected(viewID, r) }
		opts.Open = func(r dbgview.Row) {
			if r.Kind == dbgview.RowVariable {
				st.panes.Variables.SetValue(r)
			}
		}
	case command.ViewDebugWatch:
		pane = st.panes.Watch
		opts.Select = func(r dbgview.Row) { s.debugRowSelected(viewID, r) }
		opts.Open = func(r dbgview.Row) {
			switch r.Kind {
			case dbgview.RowExpression:
				st.panes.Watch.Rename(r.Expression)
			case dbgview.RowVariable:
				st.panes.Watch.SetValue(r)
			}
		}
		opts.Actions = func(r dbgview.Row) []kit.Action { return s.debugInlineActions(command.DebugWatchContext, viewID, r) }
		opts.RunAction = s.runDebugRowAction
	case command.ViewDebugCallStack:
		pane = st.panes.CallStack
		opts.Select = func(r dbgview.Row) {
			st.panes.CallStack.Select(r)
			s.debugRowSelected(viewID, r)
			if r.Kind == dbgview.RowFrame {
				s.debugShowFrame(s.binding, r.Frame, true)
			}
		}
		opts.Open = func(r dbgview.Row) {
			if r.Kind == dbgview.RowFrame {
				s.debugShowFrame(s.binding, r.Frame, false)
			}
		}
		opts.Actions = func(r dbgview.Row) []kit.Action {
			return s.debugInlineActions(command.DebugCallStackContext, viewID, r)
		}
		opts.RunAction = s.runDebugRowAction
	case command.ViewDebugBreakpoints:
		pane = st.panes.Breakpoints
		opts.Select = func(r dbgview.Row) { s.debugRowSelected(viewID, r) }
		opts.Open = func(r dbgview.Row) { s.debugOpenBreakpoint(r) }
		opts.Actions = func(r dbgview.Row) []kit.Action {
			return s.debugInlineActions(command.DebugBreakpointsContext, viewID, r)
		}
		opts.RunAction = s.runDebugRowAction
	case command.ViewDebugLoadedScripts:
		pane = st.panes.Loaded
		opts.Select = func(r dbgview.Row) { s.debugRowSelected(viewID, r) }
		opts.Open = func(r dbgview.Row) {
			if r.Kind == dbgview.RowSource && r.Source.Path != "" {
				s.openPathAt(s.binding, r.Source.Path, 0, 0, openOptions{Preview: true, TakeFocus: true})
			}
		}
	default:
		return nil
	}
	return kit.NewContextMenu(dbgview.NewBody(pane, opts), kit.ContextMenuOpts{
		Controller: st.menu, OnRun: s.menus.Run,
		OnHide: func() {},
	})
}

// debugRowSelected notes the row the user selected: the keys of the row follow it.
func (s *shellState) debugRowSelected(viewID string, r dbgview.Row) {
	s.win.Keys.Set("focusedView", viewID)
	s.syncDebugRowKeys()
}

// debugOpenBreakpoint opens the file of a breakpoint at its line, and edits a function breakpoint or an exception filter.
func (s *shellState) debugOpenBreakpoint(r dbgview.Row) {
	switch r.Kind {
	case dbgview.RowBreakpoint:
		s.openPathAt(s.binding, r.Breakpoint.Path, r.Breakpoint.Line, r.Breakpoint.Column, openOptions{Preview: true, TakeFocus: true})
	case dbgview.RowFunctionBreakpoint, dbgview.RowException:
		s.debug.panes.Breakpoints.Edit(r)
	}
}

// debugWelcome is the content of the container while there is nothing to configure (debugUx is simple).
func (s *shellState) debugWelcome() widgets.Widget {
	win := s.win
	debuggers := 0
	if win.Debug != nil {
		debuggers = len(win.Debug.Debuggers())
	}
	state := dbgview.WelcomeState{
		DebuggersAvailable: debuggers > 0, ExtensionAvailable: debuggers > 0,
		InterestedInActiveEditor: win.Keys.Get("debuggerInterestedInActiveEditor") == true,
		NoFolder:                 win.Folder == "",
	}
	return welcome.New(welcome.Opts{Pieces: dbgview.WelcomePieces(state), OnLink: s.welcomeLink})
}

// debugInlineActions are the buttons that show on a row while the pointer is over it: the items of the group inline of the menu of the
// view, in the context of the row.
func (s *shellState) debugInlineActions(menu, viewID string, r dbgview.Row) []kit.Action {
	win := s.win
	extra := s.debugRowKeys(viewID, r)
	var out []kit.Action
	for _, e := range win.Menus.Items(menu, menuContext{base: win.Keys, settings: win.Settings, extra: extra}) {
		if e.Command == "" || e.Group != "inline" {
			continue
		}
		code, _ := fonts.CodiconByName(e.Icon)
		out = append(out, kit.Action{ID: e.Command, Label: e.Title, Icon: code, Disabled: e.Disabled, Color: dbgview.IconColor(kit.DarkModern(), e.Command)})
	}
	return out
}

// runDebugRowAction runs the command of an inline button of a row with the arguments of the row.
func (s *shellState) runDebugRowAction(r dbgview.Row, id string) {
	s.runDebugCommand(id, r.Args())
}

// showDebugMenu opens the context menu of a row of a view; below the rows it is the menu of the whole view.
func (s *shellState) showDebugMenu(viewID string, r dbgview.Row, ok bool, at ui.Offset) {
	win := s.win
	menu := ""
	switch viewID {
	case command.ViewDebugCallStack:
		menu = command.DebugCallStackContext
	case command.ViewDebugVariables:
		menu = command.DebugVariablesContext
	case command.ViewDebugWatch:
		menu = command.DebugWatchContext
	case command.ViewDebugBreakpoints:
		menu = command.DebugBreakpointsContext
	case command.ViewDebugLoadedScripts:
		menu = command.DebugLoadedScriptsContext
	default:
		return
	}
	extra := map[string]any{"view": viewID}
	var args any
	if ok {
		extra = s.debugRowKeys(viewID, r)
		args = r.Args()
	}
	ctx := menuContext{base: win.Keys, settings: win.Settings, extra: extra}
	items := s.menus.ItemsWithout(menu, ctx, func(e command.MenuEntry) bool { return e.Group == "inline" })
	if ok {
		items = withDebugArgs(items, args)
	}
	if len(items) == 0 {
		return
	}
	s.debug.menu.Show(at, items)
}

// withDebugArgs gives the items the arguments of the row they are shown for.
func withDebugArgs(items []kit.MenuItem, args any) []kit.MenuItem {
	out := slices.Clone(items)
	for i, it := range out {
		switch {
		case it.Submenu:
			it.Items = withDebugArgs(it.Items, args)
		case !it.Separator && it.ID != "" && it.Data == nil:
			it.Data = args
		}
		out[i] = it
	}
	return out
}

// debugViewActions are the buttons in the header of a view: the items of group navigation of the menu of the view titles that hold for it,
// and "..." with the other items.
func (s *shellState) debugViewActions(viewID string) widgets.Widget {
	win := s.win
	if s.debug.panes == nil {
		return nil
	}
	extra := map[string]any{"view": viewID}
	ctx := menuContext{base: win.Keys, settings: win.Settings, extra: extra}
	var buttons []kit.Action
	for _, e := range win.Menus.Items(command.DebugViewTitle, ctx) {
		if e.Command == "" || e.Group != "navigation" {
			continue
		}
		code, _ := fonts.CodiconByName(e.Icon)
		buttons = append(buttons, kit.Action{ID: e.Command, Label: e.Title, Icon: code, Disabled: e.Disabled})
	}
	more := s.menus.ItemsWithout(command.DebugViewTitle, ctx, func(e command.MenuEntry) bool { return e.Group == "navigation" })
	cells := []widgets.Widget{}
	if len(buttons) > 0 {
		cells = append(cells, kit.NewActionBar(buttons, kit.ActionBarOpts{OnRun: func(a kit.Action) { s.runDebugCommand(a.ID, nil) }}))
	}
	if len(more) > 0 {
		cells = append(cells, widgets.NewGestureDetector(
			widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconEllipsis), widgets.IconOpts{Size: foundation.Some(16.0)}),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTapDown: func(d gestures.TapDownDetails) {
				s.debug.menu.Show(ui.Offset{Dx: d.GlobalPosition.Dx, Dy: d.GlobalPosition.Dy + 10}, more)
			}}))
	}
	if len(cells) == 0 {
		return nil
	}
	return widgets.NewRow(cells, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// runDebugCommand runs a command of Run and Debug for a button, on this screen.
func (s *shellState) runDebugCommand(id string, args any) {
	win := s.win
	if err := win.Commands.Run(command.Context{Ctx: win.ctx, Keys: win.Keys, Source: command.SourceMenu}, id, args); err != nil {
		win.Process.Logger.Warn("a command of Run and Debug failed", "command", id, "err", err)
	}
}

// debugViewFocused follows the focus into the views of the container.
func (s *shellState) debugViewFocused(viewID string, has bool) {
	win := s.win
	st := &s.debug
	if st.focus == nil {
		return
	}
	st.focus[viewID] = has
	if has {
		win.Keys.Set("focusedView", viewID)
	} else if win.Keys.Get("focusedView") == viewID {
		win.Keys.Set("focusedView", "")
	}
	any := false
	for _, v := range st.focus {
		any = any || v
	}
	win.Keys.Batch(func() {
		win.Keys.Set("debugViewletFocus", any)
		win.Keys.Set("variablesFocused", st.focus[command.ViewDebugVariables])
		win.Keys.Set("watchExpressionsFocused", st.focus[command.ViewDebugWatch])
		win.Keys.Set("callStackFocused", st.focus[command.ViewDebugCallStack])
		win.Keys.Set("breakpointsFocused", st.focus[command.ViewDebugBreakpoints])
		win.Keys.Set("loadedScriptsFocused", st.focus[command.ViewDebugLoadedScripts])
	})
	s.syncDebugRowKeys()
}

// isDebugView tells whether a view is one of Run and Debug.
func isDebugView(id string) bool {
	switch id {
	case command.ViewDebugWelcome, command.ViewDebugVariables, command.ViewDebugWatch, command.ViewDebugCallStack,
		command.ViewDebugBreakpoints, command.ViewDebugLoadedScripts:
		return true
	}
	return false
}

// isReadonly tells whether the presentation hint of a variable says it cannot be changed.
func isReadonly(h *dap.VariablePresentationHint) bool {
	return h != nil && slices.Contains(h.Attributes, "readOnly")
}

// showDebug shows the container, with a view's body focused when focusView is set.
func (s *shellState) showDebug(binding *widgets.WidgetsBinding, focusView string) {
	win := s.win
	win.Views.Open(command.ViewContainerDebug)
	if focusView != "" {
		win.Views.SetHidden(focusView, false)
		win.Views.SetCollapsed(focusView, false)
		s.afterFrame(binding, func() {
			if s.debug.panes == nil {
				return
			}
			if p := s.debugPane(focusView); p != nil {
				p.Controller().Focus()
			}
		})
	}
}

// debugPane is the pane of a view of the container.
func (s *shellState) debugPane(viewID string) dbgview.TreePane {
	p := s.debug.panes
	if p == nil {
		return nil
	}
	switch viewID {
	case command.ViewDebugVariables:
		return p.Variables
	case command.ViewDebugWatch:
		return p.Watch
	case command.ViewDebugCallStack:
		return p.CallStack
	case command.ViewDebugBreakpoints:
		return p.Breakpoints
	case command.ViewDebugLoadedScripts:
		return p.Loaded
	}
	return nil
}
