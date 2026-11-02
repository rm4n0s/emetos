package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// Run and Debug in the window (SPEC §10.31, step 25.4). A workspace has one debug host: the debug service over the debuggers its
// extensions contribute. This file makes the host with the options that are the window's (the prompts of a start, the terminal, the
// delays), reads the launches, saves and restores the state, and tells the host of the launch.json files that are open. The views, the
// toolbar, the status bar and the commands are the shell's (debugview.go, debugcommands.go), one set for the window of the host and one
// for each headless view of a client, all over the host's service (D46).
//
// The questions of a start are asked on the screen of whoever asked: the command handler of a screen puts the screen in the context of
// the start (withDebugScreen) and the callbacks of the service read it (decision 1181).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts, debugConfigurationManager.ts and
// src/vs/workbench/services/configurationResolver/browser/baseConfigurationResolverService.ts

// debugScreenKey is the key of the screen in the context of a start.
type debugScreenKey struct{}

// withDebugScreen puts the screen that asked for something in the context of a start, so that its prompts show there.
func withDebugScreen(ctx context.Context, screen *Workspace) context.Context {
	return context.WithValue(ctx, debugScreenKey{}, screen)
}

// debugScreen is the screen the questions of a start are asked on: the one in the context, else the window of the host.
func (w *Workspace) debugScreen(ctx context.Context) *Workspace {
	if s, ok := ctx.Value(debugScreenKey{}).(*Workspace); ok && s != nil {
		return s
	}
	if w.isView() {
		return w.viewOf
	}
	return w
}

// uiValue runs f on the UI thread of a screen and returns what it gives; on that thread it runs at once. It gives up with the error of
// the context. It is for the callbacks of the service, which run on the goroutine that started the session.
func uiValue[T any](ctx context.Context, screen *Workspace, f func() T) (T, error) {
	if screen.onUIThread() {
		return f(), nil
	}
	var out T
	done := make(chan struct{})
	screen.onUI(func() {
		out = f()
		close(done)
	})
	select {
	case <-done:
		return out, nil
	case <-ctx.Done():
		return out, ctx.Err()
	}
}

// Delays of the debug service: VS Code's.
const (
	debugStackDelay   = 420 * time.Millisecond
	debugFocusDelay   = 800 * time.Millisecond
	debugRunningDelay = 200 * time.Millisecond
	debugRestartDelay = 300 * time.Millisecond
)

// initDebug makes the debug host of a workspace. The window tab of an extension and the headless view of a workspace make none.
func (w *Workspace) initDebug() {
	host := exthost.NewDebugHost(w.ctx, exthost.DebugHostOptions{Base: w.debugOptions(), Logger: w.Process.Logger})
	w.Debug = host
	w.stops = append(w.stops, host.Close, w.followDebugTerminals(host.Service()))
}

// debugOptions are the options of the debug service that are the window's.
func (w *Workspace) debugOptions() debug.Options {
	return debug.Options{
		Logger:                  w.Process.Logger,
		Variables:               w.debugVariables,
		ConfirmConcurrent:       w.debugConfirmConcurrent,
		RunInTerminal:           w.debugRunInTerminal,
		OpenLaunchFile:          w.debugOpenLaunchFile,
		Guess:                   w.debugGuess,
		RunTask:                 w.debugRunTask,
		SaveBeforeStart:         w.debugSaveBeforeStart,
		Console:                 w.debugReplConfig,
		AutoExpandLazyVariables: func() bool { return settings.Get[string](w.Settings, "debug.autoExpandLazyVariables", "") == "on" },
		StackDelay:              debugStackDelay, FocusDelay: debugFocusDelay, RunningDelay: debugRunningDelay, RestartDelay: debugRestartDelay,
	}
}

// DebugService is the debug service of the workspace, nil where there is none.
func (w *Workspace) DebugService() *debug.Service {
	if w.Debug == nil {
		return nil
	}
	return w.Debug.Service()
}

// ---- the launches

// launchSources are the places the launches are read from: the folder, and the launch key of the workspace and the user settings.
func (w *Workspace) launchSources() debug.LaunchSources {
	src := debug.LaunchSources{Folder: w.Folder}
	if w.Folder != "" {
		src.FolderName = filepath.Base(w.Folder)
	}
	user, ws := w.Settings.Paths()
	src.UserSettingsPath, src.WorkspaceSettingsPath = user, ws
	in := w.Settings.Inspect("launch", "")
	if in.User.Ok {
		src.UserSetting = in.User.Value
	}
	if in.Workspace.Ok && !in.WorkspaceIgnored {
		src.WorkspaceSetting = in.Workspace.Value
	}
	return src
}

// reloadLaunches reads launch.json and the launch setting again (on a goroutine of its own) and tells the host about the file for its
// Problems. A file that cannot be read is logged.
func (w *Workspace) reloadLaunches() {
	svc := w.DebugService()
	if svc == nil || w.isView() {
		return
	}
	src := w.launchSources()
	go func() {
		if err := svc.Launches().Reload(w.ctx, src); err != nil {
			w.Process.Logger.Warn("launch.json cannot be read", "folder", src.Folder, "err", err)
		}
	}()
}

// followLaunches reads the launches again when launch.json or a settings file changes on disk.
func (w *Workspace) followLaunches() {
	if w.Folder == "" {
		return
	}
	launch := filepath.Join(w.Folder, ".emetos", "launch.json")
	w.stops = append(w.stops,
		w.OnFileEvents(func(events []workspace.Event) {
			for _, e := range events {
				if e.Path == launch || e.OldPath == launch || strings.HasSuffix(e.Path, filepath.Join(".emetos", "settings.json")) {
					w.onUI(w.reloadLaunches)
					return
				}
			}
		}),
		w.Settings.OnChange(func(e settings.ChangeEvent) {
			if e.Affects("launch", "") {
				w.onUI(w.reloadLaunches)
			}
		}))
}

// ---- the state

// restoreDebug puts the saved state of the debug model back (the breakpoints, the watch expressions, the choices); a state that cannot
// be used is logged and changes nothing.
func (w *Workspace) restoreDebug(raw json.RawMessage) {
	svc := w.DebugService()
	if svc == nil || len(raw) == 0 {
		return
	}
	if err := svc.RestoreState(raw); err != nil {
		w.Process.Logger.Warn("the saved state of the debugger cannot be used", "err", err)
	}
}

// debugWindowState is the state of the debug model to save with the window; nothing while there is none to save.
func (w *Workspace) debugWindowState() json.RawMessage {
	svc := w.DebugService()
	if svc == nil {
		return nil
	}
	data, err := svc.SaveState()
	if err != nil {
		w.Process.Logger.Warn("the state of the debugger cannot be saved", "err", err)
		return nil
	}
	return data
}

// ---- the questions of a start

// debugVariables gives the values the variables of a configuration read: the folder, the active editor of the screen that asked and
// its selection, the settings, the commands and the prompts (on that screen).
func (w *Workspace) debugVariables(ctx context.Context) *debug.VariableContext {
	screen := w.debugScreen(ctx)
	vc, err := uiValue(ctx, screen, screen.debugVariableContext)
	if err != nil || vc == nil {
		return &debug.VariableContext{}
	}
	vc.Command = func(ctx context.Context, id string, arg any) (any, error) {
		return screen.debugRunCommand(ctx, id, arg)
	}
	vc.PromptString = func(ctx context.Context, req debug.PromptRequest) (string, bool, error) {
		return screen.debugPrompt(ctx, req)
	}
	vc.PickString = func(ctx context.Context, req debug.PickRequest) (string, bool, error) {
		return screen.debugPick(ctx, req)
	}
	return vc
}

// debugVariableContext reads the editor and the settings; UI thread only.
func (w *Workspace) debugVariableContext() *debug.VariableContext {
	host := w
	if w.isView() {
		host = w.viewOf
	}
	vc := &debug.VariableContext{
		Setting: func(folder, key string) (any, bool) {
			v := settings.Get[any](host.Settings, key, "")
			return v, v != nil
		},
	}
	if host.Folder != "" {
		vc.Folders = []debug.Folder{{Name: filepath.Base(host.Folder), Path: host.Folder}}
	}
	if s := w.shell; s != nil && s.Mounted() {
		if e := s.activeEditor(); e != nil && e.doc != nil {
			vc.ActiveFile = e.doc.URI().Path
			sel := e.view.Cursors().Primary().Selection
			vc.Line, vc.Column = sel.Active.Line+1, sel.Active.Col+1
			if !sel.IsEmpty() {
				vc.SelectedText = e.doc.Snapshot().Text(sel.Range())
			}
		}
	}
	return vc
}

// debugRunCommand runs a command for ${command:id} and gives the value it returns; nil when it gave none (the user cancelled).
func (w *Workspace) debugRunCommand(ctx context.Context, id string, arg any) (any, error) {
	var result any
	var got bool
	_, err := uiValue(ctx, w, func() error {
		return w.Commands.Run(command.Context{Ctx: ctx, Keys: w.Keys, Source: command.SourceAPI, Result: func(v any) { result, got = v, true }}, id, arg)
	})
	if err != nil {
		return nil, err
	}
	if !got {
		return nil, nil
	}
	return result, nil
}

// debugPrompt asks for the text of a promptString input in the quick input of the screen.
func (w *Workspace) debugPrompt(ctx context.Context, req debug.PromptRequest) (string, bool, error) {
	type answer struct {
		text string
		ok   bool
	}
	ch := make(chan answer, 1)
	w.onUI(func() {
		w.QuickInput.Prompt(quickinput.PromptOptions{
			Placeholder: req.Description, Value: req.Value, Password: req.Password, IgnoreFocusOut: true,
			OnAccept: func(text string) { ch <- answer{text, true} },
			OnCancel: func() { ch <- answer{} },
		})
	})
	select {
	case a := <-ch:
		return a.text, a.ok, nil
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
}

// debugPick asks for the choice of a pickString input in the quick input of the screen.
func (w *Workspace) debugPick(ctx context.Context, req debug.PickRequest) (string, bool, error) {
	type answer struct {
		value string
		ok    bool
	}
	ch := make(chan answer, 1)
	w.onUI(func() {
		items := make([]quickinput.Item, len(req.Items))
		for i, it := range req.Items {
			value := it.Value
			items[i] = quickinput.Item{ID: it.Value, Label: it.Label, Description: it.Description, Accept: func(quickinput.KeyMods) { ch <- answer{value, true} }}
		}
		w.QuickInput.Pick(quickinput.PickOptions{
			Placeholder: req.Placeholder, Items: items, IgnoreFocusOut: true, MatchOnDescription: true,
			OnCancel: func() { ch <- answer{} },
		})
	})
	select {
	case a := <-ch:
		return a.value, a.ok, nil
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
}

// debugConfirmConcurrent asks, on the screen that asked, whether to start a second instance of a configuration that is running.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts (createSession, the question about a second instance)
func (w *Workspace) debugConfirmConcurrent(ctx context.Context, label string) bool {
	screen := w.debugScreen(ctx)
	ch := make(chan bool, 1)
	screen.onUI(func() {
		screen.Dialogs.Show(kit.DialogSpec{
			Type:    kit.DialogInfo,
			Message: "The configuration '" + label + "' is already running. Do you want to start another instance?",
			Buttons: []string{"Yes", "No"}, CancelButton: foundation.Some(1),
			OnAnswer: func(r kit.DialogResult) { ch <- !r.Cancelled && r.Button == 0 },
		})
	})
	select {
	case ok := <-ch:
		return ok
	case <-ctx.Done():
		return false
	}
}

// commandLine is the line a shell runs for a runInTerminal request: the environment as VAR=value words, then the program and its arguments,
// quoted.
func commandLine(args dap.RunInTerminalRequestArguments) string {
	var parts []string
	for k, v := range args.Env {
		if v == nil {
			continue
		}
		parts = append(parts, k+"="+shellQuote(*v))
	}
	for _, a := range args.Args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// shellQuote quotes a word for a POSIX shell when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// debugOpenLaunchFile shows launch.json of a launch after a provider asked for it (the configuration it was given needs the user's
// hand), on the screen that asked.
func (w *Workspace) debugOpenLaunchFile(ctx context.Context, l *debug.Launch, typ string) {
	screen := w.debugScreen(ctx)
	screen.onUI(func() {
		if s := screen.shell; s != nil && s.Mounted() && l.Path != "" {
			s.openPathAt(s.binding, l.Path, 0, 0, openOptions{})
		}
	})
}

// debugGuess picks the debugger of the language of the active editor when a configuration has no type (F5 with no launch.json): the only
// debugger that serves the language, else the user picks one. It gives false when there is no editor or no debugger serves its language.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugAdapterManager.ts (guessDebugger)
func (w *Workspace) debugGuess(ctx context.Context) (debug.GuessResult, bool) {
	screen := w.debugScreen(ctx)
	lang, err := uiValue(ctx, screen, func() string {
		if s := screen.shell; s != nil && s.Mounted() {
			if e := s.activeEditor(); e != nil && e.doc != nil {
				return e.doc.LanguageID()
			}
		}
		return ""
	})
	if err != nil || lang == "" || w.Debug == nil {
		return debug.GuessResult{}, false
	}
	candidates := w.Debug.DebuggersForLanguage(lang)
	switch len(candidates) {
	case 0:
		return debug.GuessResult{}, false
	case 1:
		return debug.GuessResult{Type: candidates[0].Type, Label: candidates[0].Label}, true
	}
	items := make([]debug.PickItem, len(candidates))
	for i, c := range candidates {
		items[i] = debug.PickItem{Label: c.Label, Value: c.Type}
	}
	value, ok, err := screen.debugPick(ctx, debug.PickRequest{Placeholder: "Select a debug environment", Items: items})
	if err != nil || !ok {
		return debug.GuessResult{}, false
	}
	for _, c := range candidates {
		if c.Type == value {
			return debug.GuessResult{Type: c.Type, Label: c.Label}, true
		}
	}
	return debug.GuessResult{}, false
}

// startDebug follows the files the debug launches come from and tells the host of the launch.json that is open. It runs once the folder
// of the workspace is known.
func (w *Workspace) startDebug() {
	if w.Debug == nil {
		return
	}
	w.followLaunches()
	w.followLaunchDocuments()
}

// followLaunchDocuments tells the host the text of launch.json while its document is open (the host checks it and shows the problems), and
// that it closed.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugConfigurationManager.ts (the validation of launch.json)
func (w *Workspace) followLaunchDocuments() {
	if w.Folder == "" {
		return
	}
	launch := filepath.Clean(debug.LaunchFilePath(w.Folder))
	host := w.Debug
	track := func(doc *document.Document) {
		if doc.URI().Scheme != document.SchemeFile || filepath.Clean(doc.URI().Path) != launch {
			return
		}
		send := func() { host.SetLaunchText(launch, doc.Snapshot().String()) }
		send()
		stop := doc.Subscribe(func(document.ChangeEvent) { send() })
		stopSaved := doc.OnSaved(func(document.SavedEvent) { send() })
		var closed func()
		closed = w.Documents.WatchClose(func(d *document.Document) {
			if d == doc {
				stop()
				stopSaved()
				host.ForgetLaunch(launch)
				closed()
			}
		})
	}
	w.stops = append(w.stops, w.Documents.WatchOpen(track))
	for _, doc := range w.Documents.All() {
		track(doc)
	}
}
