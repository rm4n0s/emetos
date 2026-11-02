package app

import (
	"context"
	"os"
	"strings"

	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	dbgview "github.com/rm4n0s/emetos/internal/gui/views/debug"
)

// The Debug Console of a screen (SPEC §10.31 "The Debug Console", step 25.6): the panel view over the debug service of the workspace, its
// commands and context menu, the context keys it sets and what the settings debug.internalConsoleOptions and debug.console.closeOnEnd do.
// The host's window and the view of each client of a hosted workspace each make one from their own shell over the same service, so they
// show the same lines, and a line a client types is evaluated on the host (D46).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/repl.ts, debugService.ts (the openDebug and internalConsoleOptions handling
// in onDidNewSession, closeOnEnd) and debugEditorActions.ts (SelectionToReplAction)

// debugReplConfig is what the models of the consoles read of the settings when a line comes (on any goroutine).
func (w *Workspace) debugReplConfig() debug.ReplConfig {
	cfg := debug.DefaultReplConfig
	cfg.CollapseIdenticalLines = settings.Get[bool](w.Settings, "debug.console.collapseIdenticalLines", "")
	if n := int(settings.Get[float64](w.Settings, "debug.console.maximumLines", "")); n > 0 {
		cfg.MaximumLines = n
	}
	return cfg
}

// debugConsoleConfig is what the view reads of the settings.
func (s *shellState) debugConsoleConfig() dbgview.ConsoleConfig {
	st := s.win.Settings
	cfg := dbgview.ConsoleConfig{
		FontSize:                settings.Get[float64](st, "debug.console.fontSize", ""),
		FontFamily:              settings.Get[string](st, "debug.console.fontFamily", ""),
		LineHeight:              settings.Get[float64](st, "debug.console.lineHeight", ""),
		WordWrap:                settings.Get[bool](st, "debug.console.wordWrap", ""),
		HistorySuggestions:      settings.Get[bool](st, "debug.console.historySuggestions", ""),
		AcceptSuggestionOnEnter: settings.Get[string](st, "debug.console.acceptSuggestionOnEnter", "") == "on",
	}
	if cfg.FontSize <= 0 {
		cfg.FontSize = dbgview.DefaultConsoleConfig.FontSize
	}
	return cfg
}

// initDebugConsole makes the console of the screen. It is made before the screen follows the service, so that it has the new state
// of the model before the parts of the window do.
func (s *shellState) initDebugConsole(binding *widgets.WidgetsBinding) {
	win := s.win
	st := &s.debug
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	st.console = dbgview.NewConsole(dbgview.ConsoleOpts{
		Ctx: win.ctx, Service: win.DebugService(), Post: binding.Post, Logger: win.Process.Logger,
		Config: s.debugConsoleConfig, Folder: win.Folder, Home: home,
		OpenFile: func(path string, line, column int) {
			s.openPathAt(binding, path, line, column, openOptions{Preview: true, TakeFocus: true})
		},
		OpenURL: func(url string) {
			s.Go(func(life context.Context) {
				if err := win.openURI(life, url); err != nil {
					win.Process.Logger.Warn("cannot open a link of the Debug Console", "url", url, "err", err)
				}
			})
		},
		OpenSource: func(src debug.ReplSource) {
			if p := src.Source.Path; p != "" {
				s.openPathAt(binding, p, src.Line, src.Column, openOptions{Preview: true, TakeFocus: true})
			}
		},
		AfterFrame: func(f func()) { s.afterFrame(binding, f) },
	})
	st.stops = append(st.stops, st.console.Dispose)
}

// debugConsoleBody is the body of the Debug Console tab of the panel.
func (s *shellState) debugConsoleBody() widgets.Widget {
	st := &s.debug
	if st.console == nil {
		return nil
	}
	body := dbgview.NewConsoleBody(st.console, dbgview.ConsoleBodyOpts{
		Menu: s.showConsoleMenu,
		OnFocusChange: func(has bool) {
			if s.win != nil {
				s.win.Keys.Set("inDebugRepl", has)
			}
		},
	})
	return kit.NewContextMenu(body, kit.ContextMenuOpts{Controller: st.menu, OnRun: s.menus.Run, OnHide: func() {}})
}

// debugConsoleActions are the actions in the title strip of the panel while the Debug Console is the tab.
func (s *shellState) debugConsoleActions() kit.TitleActions {
	if s.debug.console == nil {
		return nil
	}
	return dbgview.NewConsoleActions(s.debug.console)
}

// showConsoleMenu opens the context menu of the console, for a row or (with ok false) for the part below the rows.
func (s *shellState) showConsoleMenu(row dbgview.ConsoleRow, ok bool, at ui.Offset) {
	win := s.win
	st := &s.debug
	if ok {
		st.menuRow = &row
	} else {
		st.menuRow = nil
	}
	ctx := menuContext{base: win.Keys, settings: win.Settings, extra: map[string]any{"view": command.ViewRepl}}
	items := s.menus.ItemsWithout(command.DebugConsoleContext, ctx, func(e command.MenuEntry) bool { return !ok && e.Command == command.DebugReplCopy })
	if len(items) == 0 {
		return
	}
	st.menu.Show(at, items)
}

// focusDebugConsole gives the keyboard to the input of the console.
func (s *shellState) focusDebugConsole() {
	if c := s.debug.console; c != nil {
		c.Input().Focus()
	}
}

// bindDebugConsoleCommands takes over the commands of the Debug Console in the registry while the screen is on screen. The commands that
// open and focus the panel are the workbench's.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/repl.ts (the actions registered at the end of the file) and debugCommands.ts
func (s *shellState) bindDebugConsoleCommands(binding *widgets.WidgetsBinding) {
	win := s.win
	st := &s.debug
	c := st.console
	run := func(f func()) command.Handler { return func(command.Context, any) error { f(); return nil } }
	handlers := map[string]command.Handler{
		command.DebugClearRepl:       run(c.Clear),
		command.DebugReplAcceptInput: run(c.Accept),
		command.DebugReplFocusFilter: run(func() { c.FilterController().Focus() }),
		command.DebugReplCollapseAll: run(c.CollapseAll),
		command.DebugReplCopyAll:     run(func() { s.copyText(c.VisibleText()) }),
		command.DebugReplCopyAllItem: run(func() { s.copyText(c.VisibleText()) }),
		command.DebugReplCopy: run(func() {
			if r := st.menuRow; r != nil {
				s.copyText(dbgview.RowText(*r))
			}
		}),
		command.DebugReplPaste: run(func() {
			editor.NewClipboard(win.ctx, binding.Clipboard()).Read(func(text string, err error) {
				if err != nil {
					win.Process.Logger.Warn("cannot read the clipboard", "err", err)
					return
				}
				if s.Mounted() && st.console != nil {
					st.console.Paste(text)
				}
			})
		}),
		command.DebugSelectRepl: func(_ command.Context, args any) error {
			if sess, ok := args.(*debug.Session); ok {
				c.Select(sess)
			}
			return nil
		},
		command.DebugToggleReplIgnore: func(cc command.Context, _ any) error {
			return win.Commands.Run(cc, command.DebugToggleRepl, nil)
		},
		command.DebugSelectionToRepl: run(func() {
			e := s.activeEditor()
			if e == nil || e.dbg == nil {
				return
			}
			sel := e.view.Cursors().Primary().Selection
			if sel.IsEmpty() {
				return
			}
			text := strings.TrimSpace(e.doc.Snapshot().Text(sel.Range()))
			if text == "" {
				return
			}
			c.Evaluate(text)
			if err := win.Commands.Run(command.Context{Ctx: win.ctx, Keys: win.Keys, Source: command.SourceMenu}, command.DebugFocusRepl, nil); err != nil {
				win.Process.Logger.Warn("the Debug Console cannot be shown", "err", err)
			}
		}),
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

// syncDebugConsoleKeys sets the keys the console reports.
func (s *shellState) syncDebugConsoleKeys() {
	win := s.win
	c := s.debug.console
	if c == nil {
		return
	}
	shown := win.Layout.State().PanelVisible && win.Views.ActivePanel() == command.ViewContainerRepl
	win.Keys.Batch(func() {
		win.Keys.Set("multiSessionRepl", c.Multiple())
		win.Keys.Set("view."+command.ViewRepl+".visible", shown)
	})
}

// debugConsoleOnSessionStart opens the Debug Console when a session starts, as debug.internalConsoleOptions (or the same key of the
// launch configuration) says.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts (the internalConsoleOptions handling in onDidNewSession)
func (s *shellState) debugConsoleOnSessionStart(ev debug.Event) {
	win := s.win
	if ev.Session == nil {
		return
	}
	opt, _ := ev.Session.Configuration()["internalConsoleOptions"].(string)
	if opt == "" {
		opt = settings.Get[string](win.Settings, "debug.internalConsoleOptions", "")
	}
	if opt == "openOnSessionStart" || (opt == "openOnFirstSessionStart" && ev.First) {
		win.Views.OpenPanel(command.ViewContainerRepl)
		win.Layout.SetPanelVisible(true)
	}
}

// debugConsoleOnSessionEnd closes the Debug Console when the last session ended and debug.console.closeOnEnd is on.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts (closeOnEnd in onDidEndSession)
func (s *shellState) debugConsoleOnSessionEnd() {
	win := s.win
	svc := win.DebugService()
	if svc == nil || !settings.Get[bool](win.Settings, "debug.console.closeOnEnd", "") {
		return
	}
	if len(svc.Model().Sessions(false)) > 0 {
		return
	}
	if win.Layout.State().PanelVisible && win.Views.ActivePanel() == command.ViewContainerRepl {
		win.Layout.SetPanelVisible(false)
	}
}

// debugOutputNotice shows what an adapter says in the category important as a notification; it is not a line of the console.
func (s *shellState) debugOutputNotice(ev debug.Event) {
	if ev.Output == nil || ev.Output.Category != "important" {
		return
	}
	if text := strings.TrimSpace(ev.Output.Output); text != "" {
		s.win.Notify.Info("Emetos", text)
	}
}
