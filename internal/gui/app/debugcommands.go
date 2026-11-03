package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/jsonc"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	dbgview "github.com/rm4n0s/emetos/internal/gui/views/debug"
)

// The commands of Run and Debug (step 25.4). Each screen binds its own handlers, closures over its own workspace, in its own registry, so
// that what a command asks (a prompt, the file to open) is shown on the screen the command was asked on (D46). They all work on the debug
// service of the workspace, which the host's window and the views of its clients share.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugCommands.ts, debugViewlet.ts, callStackView.ts, variablesView.ts,
// watchExpressionsView.ts and breakpointsView.ts

// debugCtx is the context of a request of a command: the life of the window, with the screen that asked in it.
func (s *shellState) debugCtx() context.Context { return withDebugScreen(s.win.ctx, s.win) }

// goDebug runs a request of the debug service on a goroutine of its own and reports a failure on this screen: a notification. A start
// that the user cancelled is not a failure.
func (s *shellState) goDebug(what string, f func(ctx context.Context) error) {
	win := s.win
	ctx := s.debugCtx()
	go func() {
		err := f(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "DebugSaveCancelled" {
			return
		}
		win.Process.Logger.Warn("a debug request failed", "request", what, "err", err)
		win.onUI(func() { win.Notify.Error("Emetos", describe(err)) })
	}()
}

// bindDebugCommands takes over the commands of Run and Debug in the registry while the shell is on screen.
func (s *shellState) bindDebugCommands(binding *widgets.WidgetsBinding) {
	win := s.win
	st := &s.debug
	svc := win.DebugService()
	run := func(f func()) command.Handler { return func(command.Context, any) error { f(); return nil } }
	withArgs := func(f func(args any)) command.Handler {
		return func(_ command.Context, args any) error { f(args); return nil }
	}
	focusView := func(id string) command.Handler { return run(func() { s.showDebug(binding, id) }) }
	startWith := func(noDebug bool) command.Handler {
		return run(func() { s.debugStart(debug.Target{}, nil, noDebug) })
	}
	thread := func(do func(ctx context.Context, t *debug.Thread) error, what string) command.Handler {
		return withArgs(func(args any) {
			if t := s.debugThread(args); t != nil {
				s.goDebug(what, func(ctx context.Context) error { return do(ctx, t) })
			}
		})
	}
	handlers := map[string]command.Handler{
		command.ViewContainerDebug:                      focusView(""),
		"workbench.debug.action.focusVariablesView":     focusView(command.ViewDebugVariables),
		"workbench.debug.action.focusWatchView":         focusView(command.ViewDebugWatch),
		"workbench.debug.action.focusCallStackView":     focusView(command.ViewDebugCallStack),
		"workbench.debug.action.focusBreakpointsView":   focusView(command.ViewDebugBreakpoints),
		"workbench.debug.action.focusLoadedScriptsView": focusView(command.ViewDebugLoadedScripts),

		command.DebugStartCommand: startWith(false),
		command.DebugRunCommand:   startWith(true),
		command.DebugStopCommand: withArgs(func(args any) {
			sess := s.debugSession(args)
			s.goDebug("stop", func(ctx context.Context) error { return svc.Stop(ctx, sess, false, false) })
		}),
		command.DebugDisconnectCommand: withArgs(func(args any) {
			sess := s.debugSession(args)
			s.goDebug("disconnect", func(ctx context.Context) error { return svc.Stop(ctx, sess, true, false) })
		}),
		command.DebugDisconnectSuspend: withArgs(func(args any) {
			sess := s.debugSession(args)
			s.goDebug("disconnect", func(ctx context.Context) error { return svc.Stop(ctx, sess, true, true) })
		}),
		command.DebugRestartCommand: withArgs(func(args any) {
			if sess := s.debugSession(args); sess != nil {
				s.goDebug("restart", func(ctx context.Context) error { return svc.RestartSession(ctx, sess, nil) })
			}
		}),
		command.DebugContinueCommand: thread(func(ctx context.Context, t *debug.Thread) error { return t.Continue(ctx) }, "continue"),
		command.DebugStepOverCommand: thread(func(ctx context.Context, t *debug.Thread) error { return t.Next(ctx, "") }, "stepOver"),
		command.DebugStepIntoCommand: thread(func(ctx context.Context, t *debug.Thread) error { return t.StepIn(ctx, "") }, "stepInto"),
		command.DebugStepOutCommand:  thread(func(ctx context.Context, t *debug.Thread) error { return t.StepOut(ctx, "") }, "stepOut"),
		command.DebugStepBackCommand: thread(func(ctx context.Context, t *debug.Thread) error { return t.StepBack(ctx, "") }, "stepBack"),
		command.DebugReverseContinue: thread(func(ctx context.Context, t *debug.Thread) error { return t.ReverseContinue(ctx) }, "reverseContinue"),
		command.DebugTerminateThread: thread(func(ctx context.Context, t *debug.Thread) error { return t.Terminate(ctx) }, "terminateThread"),
		command.DebugPauseCommand: withArgs(func(args any) {
			sess, tid := s.debugPauseTarget(args)
			if sess != nil {
				s.goDebug("pause", func(ctx context.Context) error { return sess.Pause(ctx, tid) })
			}
		}),
		command.DebugStepIntoTarget: run(s.debugStepIntoTarget),
		command.DebugRestartFrame: withArgs(func(args any) {
			if f := s.debugFrame(args); f != nil {
				s.goDebug("restartFrame", func(ctx context.Context) error { return f.Restart(ctx) })
			}
		}),
		command.DebugFocusSession:     run(s.debugPickSession),
		command.DebugSelectSession:    run(s.debugPickSession),
		command.DebugSelectAndStart:   run(s.debugSelectAndStart),
		command.DebugConfigure:        withArgs(func(args any) { s.debugConfigure(args, false) }),
		command.DebugAddConfiguration: withArgs(func(args any) { s.debugConfigure(args, true) }),

		command.DebugCallStackTop:      run(func() { s.debugNavigate(binding, -2) }),
		command.DebugCallStackBottom:   run(func() { s.debugNavigate(binding, 2) }),
		command.DebugCallStackUp:       run(func() { s.debugNavigate(binding, -1) }),
		command.DebugCallStackDown:     run(func() { s.debugNavigate(binding, 1) }),
		command.DebugShowLoadedScripts: run(s.debugPickLoadedScript),

		command.DebugCopyStackTrace:   withArgs(s.debugCopyStackTrace),
		command.DebugCopyValue:        withArgs(s.debugCopyValue),
		command.DebugCopyEvaluatePath: withArgs(s.debugCopyEvaluatePath),
		command.DebugAddToWatch:       withArgs(s.debugAddToWatch),
		command.DebugSetVariable:      withArgs(s.debugSetValue),
		command.DebugSetExpression:    withArgs(s.debugSetValue),
		command.DebugRenameWatch: withArgs(func(args any) {
			if r, ok := s.debugRow(args, command.ViewDebugWatch); ok && r.Expression != nil {
				st.panes.Watch.Rename(r.Expression)
			}
		}),
		command.DebugRemoveWatch: withArgs(func(args any) {
			if r, ok := s.debugRow(args, command.ViewDebugWatch); ok && r.Expression != nil {
				svc.Model().RemoveWatchExpressions(r.Expression.ID())
			}
		}),
		command.DebugAddWatch:       run(func() { s.showDebug(binding, command.ViewDebugWatch); st.panes.Watch.Add("") }),
		command.DebugRemoveAllWatch: run(func() { svc.Model().RemoveWatchExpressions("") }),
		command.DebugCollapseAll:    withArgs(s.debugCollapseAll),

		command.DebugRemoveAllBreakpoints: run(func() {
			s.goDebug("removeAllBreakpoints", func(ctx context.Context) error {
				if err := svc.RemoveBreakpoints(ctx); err != nil {
					return err
				}
				return svc.RemoveFunctionBreakpoints(ctx, "")
			})
		}),
		command.DebugEnableAllBreakpoints: run(func() {
			s.goDebug("enableAllBreakpoints", func(ctx context.Context) error { return svc.EnableOrDisableBreakpoints(ctx, true, "") })
		}),
		command.DebugDisableAllBreakpoint: run(func() {
			s.goDebug("disableAllBreakpoints", func(ctx context.Context) error { return svc.EnableOrDisableBreakpoints(ctx, false, "") })
		}),
		command.DebugToggleActivated: run(func() {
			on := !svc.Model().BreakpointsActivated()
			s.goDebug("toggleActivated", func(ctx context.Context) error { return svc.SetBreakpointsActivated(ctx, on) })
		}),
		command.DebugAddFunctionBreakpoit: run(func() {
			s.showDebug(binding, command.ViewDebugBreakpoints)
			s.goDebug("addFunctionBreakpoint", func(ctx context.Context) error {
				_, err := svc.AddFunctionBreakpoint(ctx, debug.FunctionBreakpointOptions{})
				return err
			})
		}),
		command.DebugToggleBreakpointItem: withArgs(func(args any) {
			if r, ok := s.debugRow(args, command.ViewDebugBreakpoints); ok {
				st.panes.Breakpoints.Toggle(r)
			}
		}),
		command.DebugRemoveBreakpoint: withArgs(func(args any) {
			if r, ok := s.debugRow(args, command.ViewDebugBreakpoints); ok {
				st.panes.Breakpoints.Remove(r)
			}
		}),
		command.DebugEditBreakpoint: withArgs(func(args any) {
			if r, ok := s.debugRow(args, command.ViewDebugBreakpoints); ok {
				st.panes.Breakpoints.Edit(r)
			}
		}),
	}
	for id, h := range s.debugEditorHandlers(binding) {
		handlers[id] = h
	}
	for id, h := range handlers {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		st.removeCommands = append(st.removeCommands, remove)
	}
}

// ---- what a command works on

// debugRow is the row a command works on: the one it was run for (the argument of a menu), else the selected row of the view that has
// the focus, else of the given view.
func (s *shellState) debugRow(args any, fallbackView string) (dbgview.Row, bool) {
	if r, ok := args.(dbgview.Row); ok {
		return r, true
	}
	st := &s.debug
	if st.panes == nil {
		return dbgview.Row{}, false
	}
	view, _ := s.win.Keys.Get("focusedView").(string)
	if !isDebugView(view) {
		view = fallbackView
	}
	var pane interface{ Focused() (dbgview.Row, bool) }
	switch view {
	case command.ViewDebugVariables:
		pane = st.panes.Variables
	case command.ViewDebugWatch:
		pane = st.panes.Watch
	case command.ViewDebugCallStack:
		pane = st.panes.CallStack
	case command.ViewDebugBreakpoints:
		pane = st.panes.Breakpoints
	default:
		return dbgview.Row{}, false
	}
	return pane.Focused()
}

// debugCallStackArgs reads the argument a command has when it is run for a row of the Call Stack, or for an extension.
func debugCallStackArgs(args any) (dbgview.CallStackArgs, bool) {
	switch v := args.(type) {
	case dbgview.CallStackArgs:
		return v, true
	case dbgview.Row:
		if a, ok := v.Args().(dbgview.CallStackArgs); ok {
			return a, true
		}
	case map[string]any:
		var a dbgview.CallStackArgs
		a.SessionID, _ = v["sessionId"].(string)
		if n, ok := v["threadId"].(float64); ok {
			a.ThreadID = int(n)
		}
		if n, ok := v["frameId"].(float64); ok {
			a.FrameID = int(n)
		}
		return a, a.SessionID != ""
	}
	return dbgview.CallStackArgs{}, false
}

// debugSession is the session a command works on: the one of its argument, else the focused one, else the only one.
func (s *shellState) debugSession(args any) *debug.Session {
	svc := s.win.DebugService()
	if a, ok := debugCallStackArgs(args); ok {
		if sess := svc.Model().Session(a.SessionID); sess != nil {
			return sess
		}
	}
	if f := svc.Model().Focus(); f.Session != nil {
		return f.Session
	}
	if all := svc.Model().Sessions(false); len(all) == 1 {
		return all[0]
	}
	return nil
}

// debugThread is the thread a command works on: the one of its argument, else the focused one, else a stopped thread of the focused
// session.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugCommands.ts (getThreadAndRun)
func (s *shellState) debugThread(args any) *debug.Thread {
	svc := s.win.DebugService()
	if a, ok := debugCallStackArgs(args); ok && a.ThreadID != 0 {
		if sess := svc.Model().Session(a.SessionID); sess != nil {
			if t := sess.Thread(a.ThreadID); t != nil {
				return t
			}
		}
	}
	f := svc.Model().Focus()
	if f.Thread != nil {
		return f.Thread
	}
	if f.Session != nil {
		for _, t := range f.Session.Threads() {
			if t.Stopped() {
				return t
			}
		}
	}
	return nil
}

// debugPauseTarget is the session and the thread to pause: the thread of the argument, else the focused one, else the first thread of the
// focused session that runs.
func (s *shellState) debugPauseTarget(args any) (*debug.Session, int) {
	if t := s.debugThread(args); t != nil && !t.Stopped() {
		return t.Session(), t.ID
	}
	sess := s.debugSession(args)
	if sess == nil {
		return nil, 0
	}
	for _, t := range sess.Threads() {
		if !t.Stopped() {
			return sess, t.ID
		}
	}
	if th := sess.Threads(); len(th) > 0 {
		return sess, th[0].ID
	}
	return sess, 0
}

// debugFrame is the frame a command works on: the one of its argument, else the focused one.
func (s *shellState) debugFrame(args any) *debug.StackFrame {
	svc := s.win.DebugService()
	if a, ok := debugCallStackArgs(args); ok && a.FrameID != 0 {
		if t := s.debugThread(args); t != nil {
			for _, f := range t.CallStack() {
				if f.ID == a.FrameID {
					return f
				}
			}
		}
	}
	return svc.Model().Focus().Frame
}

// ---- start

// debugStart starts the selected configuration (or the one in target) on this screen. The files are saved first as debug.saveBeforeStart
// says; a start with no configuration at all asks the debuggers of the language of the active editor.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugCommands.ts (StartAction, RunAction)
func (s *shellState) debugStart(target debug.Target, launch *debug.Launch, noDebug bool) {
	svc := s.win.DebugService()
	opts := debug.SessionOptions{StartedByUser: true}
	if noDebug {
		t := true
		opts.NoDebug = &t
	}
	s.goDebug("start", func(ctx context.Context) error {
		_, err := svc.StartDebugging(ctx, launch, target, opts)
		return err
	})
}

// debugSaveBeforeStart saves the files as debug.saveBeforeStart says before a session starts (debugFilesToSave). It waits for the save on
// the screen that asked; the setting is read for the language of that screen's active editor.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugUtils.ts (saveAllBeforeDebugStart)
func (w *Workspace) debugSaveBeforeStart(ctx context.Context) error {
	screen := w.debugScreen(ctx)
	ch := make(chan bool, 1)
	screen.onUI(func() {
		s := screen.shell
		if s == nil || !s.Mounted() {
			ch <- true
			return
		}
		var active *document.Document
		lang := ""
		if e := s.activeEditor(); e != nil && e.doc != nil {
			active, lang = e.doc, e.doc.LanguageID()
		}
		mode := settings.Get[string](w.Settings, "debug.saveBeforeStart", lang)
		s.saveDocsThen(debugFilesToSave(mode, s.dirtyDocuments(), active), func(ok bool) { ch <- ok })
	})
	select {
	case ok := <-ch:
		if !ok {
			return errors.New("DebugSaveCancelled", "the files were not saved, so the debug session was not started")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// debugFilesToSave are the documents to save before a session starts. "none" saves nothing. Any other value saves the documents with
// changes that are files (Save All), and "allEditorsInActiveGroup" also the active editor's document when it is untitled (it asks for
// its file); "nonUntitledEditorsInActiveGroup" leaves untitled documents alone.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugUtils.ts (saveAllBeforeDebugStart)
func debugFilesToSave(mode string, dirty []*document.Document, active *document.Document) []*document.Document {
	if mode == "none" {
		return nil
	}
	var out []*document.Document
	for _, d := range dirty {
		if d.URI().Scheme != document.SchemeUntitled {
			out = append(out, d)
		}
	}
	if mode == "allEditorsInActiveGroup" && active != nil && active.Dirty() && active.URI().Scheme == document.SchemeUntitled {
		out = append(out, active)
	}
	return out
}

// debugSelectAndStart lists the configurations in the quick input and starts the one that is chosen; the choice becomes the selected
// configuration.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugQuickAccess.ts
func (s *shellState) debugSelectAndStart() {
	win := s.win
	svc := win.DebugService()
	launches := svc.Launches()
	many := len(launches.Launches()) > 1
	var items []quickinput.Item
	for _, c := range launches.All() {
		c := c
		item := quickinput.Item{ID: c.Name, Label: c.Name}
		if many {
			item.Description = c.Launch.Name
		}
		item.Accept = func(quickinput.KeyMods) {
			launches.Select(c.Launch, c.Name, debug.SelectOptions{})
			s.debugStart(debug.Target{Name: c.Name}, c.Launch, false)
		}
		items = append(items, item)
	}
	items = append(items, quickinput.Item{ID: "add", Label: "Add Config...", Accept: func(quickinput.KeyMods) { s.runDebugCommand(command.DebugAddConfiguration, nil) }})
	win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select the launch configuration to start", Items: items, MatchOnDescription: true})
}

// debugPickSession lists the sessions in the quick input and focuses the one that is chosen.
func (s *shellState) debugPickSession() {
	win := s.win
	svc := win.DebugService()
	var items []quickinput.Item
	for _, sess := range svc.Model().Sessions(false) {
		sess := sess
		items = append(items, quickinput.Item{ID: sess.ID(), Label: sess.Label(), Accept: func(quickinput.KeyMods) { svc.FocusStackFrame(nil, nil, sess) }})
	}
	if len(items) == 0 {
		return
	}
	win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select a Debug Session", Items: items})
}

// debugPickLoadedScript lists the sources the focused session loaded and opens the one that is chosen.
func (s *shellState) debugPickLoadedScript() {
	win := s.win
	sess := s.debugSession(nil)
	if sess == nil {
		return
	}
	ctx := s.debugCtx()
	go func() {
		sources, err := sess.LoadedSources(ctx)
		win.onUI(func() {
			if err != nil || !s.Mounted() {
				return
			}
			var items []quickinput.Item
			for _, src := range sources {
				src := src
				if src.Path == "" {
					continue
				}
				items = append(items, quickinput.Item{ID: src.Key(), Label: filepath.Base(src.Path), Description: src.Path,
					Accept: func(quickinput.KeyMods) {
						s.openPathAt(s.binding, src.Path, 0, 0, openOptions{Preview: true, TakeFocus: true})
					}})
			}
			win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select a loaded script to open", Items: items, MatchOnDescription: true})
		})
	}()
}

// debugStepIntoTarget asks the adapter for the calls on the line of the focused frame and steps into the one that is chosen.
func (s *shellState) debugStepIntoTarget() {
	win := s.win
	f := win.DebugService().Model().Focus()
	if f.Session == nil || f.Thread == nil || f.Frame == nil {
		return
	}
	ctx := s.debugCtx()
	sess, t, frame := f.Session, f.Thread, f.Frame
	go func() {
		targets, err := sess.StepInTargets(ctx, frame.ID)
		win.onUI(func() {
			if err != nil || len(targets) == 0 || !s.Mounted() {
				return
			}
			var items []quickinput.Item
			for _, tg := range targets {
				id := tg.ID
				items = append(items, quickinput.Item{ID: strconv.Itoa(id), Label: tg.Label, Accept: func(quickinput.KeyMods) {
					s.goDebug("stepInto", func(ctx context.Context) error { return sess.StepIn(ctx, t.ID, id, "") })
				}})
			}
			win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select the target to step into", Items: items})
		})
	}()
}

// debugNavigate moves the focus through the call stack of the focused thread: -1 up (towards the callers... one frame down in the list),
// +1 down, and -2 and +2 to the top and the bottom.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugCommands.ts (callStackTop, callStackBottom, callStackUp, callStackDown)
func (s *shellState) debugNavigate(binding *widgets.WidgetsBinding, step int) {
	svc := s.win.DebugService()
	f := svc.Model().Focus()
	if f.Thread == nil {
		return
	}
	frames := f.Thread.CallStack()
	if len(frames) == 0 {
		return
	}
	idx := 0
	for i, fr := range frames {
		if f.Frame != nil && fr.ID == f.Frame.ID {
			idx = i
		}
	}
	switch step {
	case -2:
		idx = 0
	case 2:
		idx = len(frames) - 1
	case -1:
		idx = max(idx-1, 0)
	case 1:
		idx = min(idx+1, len(frames)-1)
	}
	svc.FocusStackFrame(frames[idx], f.Thread, f.Thread.Session())
	s.debugShowFrame(binding, frames[idx], false)
}

// ---- copy, watch and values

// debugValueNode is the node of the row a command works on in the Variables or Watch view.
func (s *shellState) debugValueNode(args any) (dbgview.Row, bool) {
	r, ok := s.debugRow(args, command.ViewDebugVariables)
	if !ok || r.Node() == nil {
		return r, false
	}
	return r, true
}

// debugCopyValue copies the value of the variable or the watch expression of the row.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/variablesView.ts (COPY_VALUE_ID)
func (s *shellState) debugCopyValue(args any) {
	if r, ok := s.debugValueNode(args); ok {
		s.copyText(r.Node().Value())
	}
}

// debugCopyEvaluatePath copies the expression that evaluates to the variable of the row.
func (s *shellState) debugCopyEvaluatePath(args any) {
	r, ok := s.debugValueNode(args)
	if !ok {
		return
	}
	switch {
	case r.Variable != nil && r.Variable.EvaluateName != "":
		s.copyText(r.Variable.EvaluateName)
	case r.Expression != nil:
		s.copyText(r.Expression.Name)
	}
}

// debugAddToWatch adds the expression that evaluates to the variable of the row to the Watch view.
func (s *shellState) debugAddToWatch(args any) {
	r, ok := s.debugValueNode(args)
	if !ok || r.Variable == nil || r.Variable.EvaluateName == "" {
		return
	}
	s.win.DebugService().Model().AddWatchExpression(r.Variable.EvaluateName)
	s.showDebug(s.binding, "")
}

// debugSetValue opens the input that changes the value of the variable or expression of the row.
func (s *shellState) debugSetValue(args any) {
	r, ok := s.debugValueNode(args)
	if !ok {
		return
	}
	switch r.Kind {
	case dbgview.RowVariable, dbgview.RowExpression:
	default:
		return
	}
	if view, _ := s.win.Keys.Get("focusedView").(string); view == command.ViewDebugWatch || r.Expression != nil {
		s.debug.panes.Watch.SetValue(r)
		return
	}
	s.debug.panes.Variables.SetValue(r)
}

// debugCollapseAll folds the rows of the view the command was run for.
func (s *shellState) debugCollapseAll(args any) {
	view, _ := s.win.Keys.Get("focusedView").(string)
	if p := s.debugPane(view); p != nil {
		p.Model().CollapseAll()
	}
}

// debugCopyStackTrace copies the frames of the thread of the row, one per line as "name (file:line:column)".
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/callStackView.ts (COPY_STACK_TRACE_ID)
func (s *shellState) debugCopyStackTrace(args any) {
	t := s.debugThread(args)
	if t == nil {
		return
	}
	var lines []string
	for _, f := range t.CallStack() {
		lines = append(lines, f.String())
	}
	s.copyText(strings.Join(lines, "\n"))
}

// ---- launch.json

// debugConfigure opens launch.json of the selected launch. A file that does not exist is made first, with the configurations of a
// debugger (the only one, else the user picks); with add set a configuration is added to the file, too.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugConfigurationManager.ts (Launch.openConfigFile) and
// debugCommands.ts (ConfigureAction, ADD_CONFIGURATION_ID)
func (s *shellState) debugConfigure(args any, add bool) {
	win := s.win
	host := win.Debug
	svc := win.DebugService()
	if host == nil {
		return
	}
	if m, ok := args.(map[string]any); ok && m["addNew"] == true {
		add = true
	}
	folder := win.Folder
	if win.isView() && win.viewOf != nil {
		folder = win.viewOf.Folder
	}
	if folder == "" {
		return
	}
	path := debug.LaunchFilePath(folder)
	if sel := svc.Launches().Selected(); sel.Launch != nil && sel.Launch.Kind == debug.LaunchFolder && sel.Launch.Path != "" {
		path = sel.Launch.Path
	}
	_, statErr := os.Stat(path)
	exists := statErr == nil
	if exists && !add {
		s.openPathAt(s.binding, path, 0, 0, openOptions{TakeFocus: true})
		return
	}
	infos := host.Debuggers()
	if len(infos) == 0 {
		return
	}
	finish := func(typ string) {
		ctx := s.debugCtx()
		go func() {
			err := writeLaunchConfiguration(ctx, host, folder, path, typ, exists)
			win.onUI(func() {
				if err != nil {
					win.Process.Logger.Warn("launch.json cannot be written", "path", path, "err", err)
					win.Notify.Error("Emetos", describe(err))
					return
				}
				win.reloadLaunches()
				if s.Mounted() {
					s.openPathAt(s.binding, path, 0, 0, openOptions{TakeFocus: true})
				}
			})
		}()
	}
	if len(infos) == 1 {
		finish(infos[0].Type)
		return
	}
	var items []quickinput.Item
	for _, d := range infos {
		typ := d.Type
		items = append(items, quickinput.Item{ID: typ, Label: d.Label, Accept: func(quickinput.KeyMods) { finish(typ) }})
	}
	win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select a debug environment", Items: items})
}

// writeLaunchConfiguration makes launch.json with the initial configurations of a debug type, or adds the first of them to the file that
// exists. The tag is DebugLaunchWriteFailed; the errors of the JSONC edit are the package's own.
func writeLaunchConfiguration(ctx context.Context, host interface {
	InitialConfigurations(ctx context.Context, folder, typ string) ([]debug.Config, error)
}, folder, path, typ string, exists bool) error {
	cfgs, err := host.InitialConfigurations(ctx, folder, typ)
	if err != nil {
		return err
	}
	fmtOpts := jsonc.FormattingOptions{InsertSpaces: false, TabSize: 4, EOL: "\n"}
	text := "{\n\t\"version\": \"0.2.0\",\n\t\"configurations\": []\n}\n"
	if exists {
		data, err := os.ReadFile(path)
		if err != nil {
			return errors.NewErr("DebugLaunchWriteFailed", err).SetMetadata("path", path)
		}
		text = string(data)
		cfgs = cfgs[:min(len(cfgs), 1)]
	}
	for _, c := range cfgs {
		edits, err := jsonc.SetValue(text, jsonc.Path{"configurations", -1}, map[string]any(c), fmtOpts)
		if err != nil {
			return err // the edit functions of jsonc give their own errors
		}
		text, err = jsonc.ApplyEdits(text, edits)
		if err != nil {
			return err // the edit functions of jsonc give their own errors
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return errors.NewErr("DebugLaunchWriteFailed", err).SetMetadata("path", path)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return errors.NewErr("DebugLaunchWriteFailed", err).SetMetadata("path", path)
	}
	return nil
}
