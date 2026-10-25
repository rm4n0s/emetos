package app

import (
	"context"
	"os"
	"time"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// quickInputPlaceholder is the hint of the quick input while no mode takes the text (the file mode, which is
// the default mode, takes every text that no prefix claims, so the hint shows only for a provider that is gone).
const quickInputPlaceholder = "Type '?' to get help on the actions you can take from here."

// initQuickInput connects the quick input of the window to the shell: it gives the controller the hooks
// that need the widgets, registers the providers of the modes that need the shell (the command palette,
// go to line, the views, help) and takes over the commands that open them.
//
// Ported from VS Code: src/vs/workbench/contrib/quickaccess/browser/quickAccess.contribution.ts,
// src/vs/workbench/browser/actions/quickAccessActions.ts and
// src/vs/workbench/contrib/codeEditor/browser/quickaccess/gotoLineQuickAccess.ts
func (s *shellState) initQuickInput(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	s.quickWin = win
	ctl := win.QuickInput
	ctl.Configure(quickinput.ControllerOptions{
		Post: binding.Post,
		// the command of an accepted item runs two frames after the picker hid, when the focus is back in the
		// widget that had it (an editor binds its commands while it has the focus)
		After: func(f func()) {
			scheduler := binding.Scheduler()
			scheduler.AddPostFrameCallback(func(time.Duration) {
				scheduler.ScheduleFrame()
				scheduler.AddPostFrameCallback(func(time.Duration) {
					if s.Mounted() {
						f()
					}
				})
			})
			scheduler.ScheduleFrame()
		},
		Invoker:             func() contextkey.Context { return s.editorMenuContext() },
		FallbackPlaceholder: quickInputPlaceholder,
	})
	keyLabel := func(id string, ctx contextkey.Context) string {
		if kb, ok := win.Keybindings.LookupPrimaryKeybinding(id, ctx, false); ok && kb.Keybinding != nil {
			return kb.Keybinding.Label()
		}
		return ""
	}
	helpKeys := func(id string) string { return keyLabel(id, ctl.InvokerContext()) }
	editorCommands := map[string]bool{}
	for _, id := range editor.CommandIDs() {
		editorCommands[id] = true
	}
	reg := win.QuickAccess
	providers := []quickinput.Provider{
		quickinput.NewHelpProvider(reg, helpKeys),
		quickinput.NewCommandsProvider(quickinput.CommandsOptions{
			Commands: win.Commands, Menus: win.Menus, Context: ctl.InvokerContext, Keybinding: keyLabel,
			History: win.CommandHistory,
			Available: func(id string, ctx contextkey.Context) bool {
				return !editorCommands[id] || ctx.Value("activeEditor") != nil
			},
			HistoryLimit: func() int { return settings.Get[int](win.Settings, "workbench.commandPalette.history", "") },
			PreserveInput: func() bool {
				return settings.Get[bool](win.Settings, "workbench.commandPalette.preserveInput", "")
			},
			Run: func(id string) error {
				// an editor binds its commands while it has the focus: it gets it back before its command runs
				if e := s.activeEditor(); e != nil && editorCommands[id] {
					e.focus.Request()
				}
				return win.Commands.Run(command.Context{Ctx: win.ctx, Keys: ctl.InvokerContext(), Source: command.SourcePalette}, id, nil)
			},
			OnError: func(id string, err error) {
				title := id
				if m, ok := win.Commands.Lookup(id); ok && m.Title != "" {
					title = m.Title
				}
				win.Process.Logger.Warn("command run by the palette failed", "command", id, "error", describe(err))
				win.Notify.Error("Emetos", "Command '"+title+"' resulted in an error: "+describe(err))
			},
		}),
		quickinput.NewFilesProvider(s.filesOptions(binding)),
		quickinput.NewGoToLineProvider(quickinput.GoToLinePrefix, s.goToLineEditor),
		quickinput.NewGoToLineProvider(quickinput.GoToOffsetPrefix, s.goToLineEditor),
		quickinput.NewViewsProvider(quickinput.ViewsOptions{
			Views: win.Views.Registry(), Context: ctl.InvokerContext,
			OpenContainer: s.openViewContainer, OpenView: s.openView,
			Channels: func() []string {
				if win.Output == nil {
					return nil
				}
				return win.Output.Channels()
			},
			OpenChannel: func(name string) {
				if win.Output != nil {
					win.Output.SetActive(name)
				}
				s.openViewContainer(command.ViewContainerOutput)
			},
		}),
	}
	providers = append(providers, s.editorProviders()...)
	for _, p := range providers {
		if err := reg.Register(p); err != nil {
			win.Process.Logger.Error("cannot register a quick access provider", "err", err)
			continue
		}
		s.quickPrefixes = append(s.quickPrefixes, p.Info().Prefix)
	}
	s.bindQuickInputCommands()
	// the list of files of the folder is walked in the background while the window shows
	s.syncFileIndex()
	s.stopFolderWatch = win.OnFolderChange(s.syncFileIndex)
}

// syncFileIndex starts the walk of the folder of the window when it has not started, and stops the walk of
// the folder that was closed.
func (s *shellState) syncFileIndex() {
	win := PeekWorkspace(s.Context())
	idx := win.FileIndex()
	if idx == s.fileIndex {
		return
	}
	if old := s.fileIndex; old != nil {
		go old.Close() // stopping waits for a folder that is being read
	}
	s.fileIndex = idx
	if idx != nil {
		idx.Start(s.Lifetime())
	}
}

// filesOptions connect the file mode of the quick input to the window.
func (s *shellState) filesOptions(binding *widgets.WidgetsBinding) quickinput.FilesOptions {
	win := PeekWorkspace(s.Context())
	home, _ := os.UserHomeDir()
	return quickinput.FilesOptions{
		Search: func(ctx context.Context, filter string) (workspace.QuickOpenResult, error) {
			return workspace.QuickOpen(ctx, win.FileIndex(), win.Recent.List(), filter, workspace.QuickOpenOptions{
				IncludeHistory:   settings.Get[bool](win.Settings, "search.quickOpen.includeHistory", ""),
				HistoryByRecency: settings.Get[string](win.Settings, "search.quickOpen.history.filterSortOrder", "") == "recency",
				Home:             home,
			})
		},
		OnChange: func(f func()) func() {
			idx := win.FileIndex()
			if idx == nil {
				return func() {}
			}
			return idx.OnChange(func(workspace.IndexEvent) { f() })
		},
		Open: func(req quickinput.FileOpen) {
			if !s.Mounted() {
				return
			}
			opts := openOptions{Preview: !req.Pinned}
			if req.Side {
				opts.Group = s.sideGroup()
			}
			s.openPathAt(binding, req.Path, req.Line, req.Column, opts)
		},
		Icon: func(path, name string, typ theme.Type) (theme.FileIcon, bool) {
			th := win.FileIcons()
			if th == nil {
				return theme.FileIcon{}, false
			}
			return th.IconFor(theme.FileIconQuery{Kind: theme.FileIconFile, Name: name, LanguageID: win.languageOf(path), Type: typ})
		},
		OpenPinned: func() bool {
			return !settings.Get[bool](win.Settings, "workbench.editor.enablePreviewFromQuickOpen", "") ||
				!settings.Get[bool](win.Settings, "workbench.editor.enablePreview", "")
		},
		SideDirection: func() string {
			return settings.Get[string](win.Settings, "workbench.editor.openSideBySideDirection", "")
		},
		RemoveRecent: win.Recent.Remove,
		OnError: func(err error) {
			win.Process.Logger.Warn("the file search of the quick open failed", "err", err)
		},
	}
}

// disposeQuickInput takes the providers and the commands of the shell away.
func (s *shellState) disposeQuickInput() {
	// the window is the one the providers were registered with: the element is not active any more
	// while it is disposed, and Glutter does not allow looking up an ancestor then
	win := s.quickWin
	if win == nil {
		return
	}
	s.endThemePicks()
	for _, prefix := range s.quickPrefixes {
		win.QuickAccess.Unregister(prefix)
	}
	s.quickPrefixes = nil
	if s.stopFolderWatch != nil {
		s.stopFolderWatch()
		s.stopFolderWatch = nil
	}
	if s.fileIndex != nil {
		s.fileIndex.Close()
		s.fileIndex = nil
	}
	for _, f := range s.removeQuickCommands {
		f()
	}
	s.removeQuickCommands = nil
}

// bindQuickInputCommands takes over the commands that open the quick input in a mode.
func (s *shellState) bindQuickInputCommands() {
	win := PeekWorkspace(s.Context())
	ctl := win.QuickInput
	show := func(prefix string) command.Handler {
		return func(command.Context, any) error { ctl.Show(prefix, quickinput.ShowOptions{}); return nil }
	}
	handlers := map[string]command.Handler{
		quickinput.ShowCommandsCommand:  show(quickinput.CommandsPrefix),
		quickinput.GoToLineCommand:      show(quickinput.GoToLinePrefix),
		quickinput.GoToOffsetCommand:    show(quickinput.GoToOffsetPrefix),
		quickinput.OpenViewCommand:      show(quickinput.ViewsPrefix),
		quickinput.QuickOpenViewCommand: show(quickinput.ViewsPrefix),
		// the argument is the text to start with, as a string or as {"value": "..."}: it is taken as it is
		"workbench.action.quickOpen": func(_ command.Context, args any) error {
			value, given := args.(string)
			if m, ok := args.(map[string]any); ok {
				value, given = m["value"].(string)
			}
			ctl.Show(value, quickinput.ShowOptions{PreserveValue: given})
			return nil
		},
		quickinput.ClearHistoryCommand: func(command.Context, any) error { win.CommandHistory.Clear(); return nil },
	}
	for id, h := range handlers {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		s.removeQuickCommands = append(s.removeQuickCommands, remove)
	}
}

// openViewContainer shows a view container of the side bar or of the panel and gives it the focus.
func (s *shellState) openViewContainer(id string) {
	win := PeekWorkspace(s.Context())
	c, ok := win.Views.Registry().Container(id)
	if !ok {
		return
	}
	run := func(cmd string) {
		if err := win.Commands.Run(command.Context{Ctx: win.ctx, Keys: win.Keys}, cmd, nil); err != nil {
			win.Process.Logger.Warn("cannot focus a view", "command", cmd, "error", describe(err))
		}
	}
	if c.Location == command.ViewLocationPanel {
		win.Views.OpenPanel(id)
		run("workbench.action.focusPanel")
		binding := widgets.WidgetsBindingOf(s.Context())
		binding.Scheduler().ScheduleFrame()
		binding.Scheduler().AddPostFrameCallback(func(time.Duration) {
			if s.Mounted() {
				s.focusPanelView(id)
			}
		})
		return
	}
	win.Views.Open(id)
	run("workbench.action.focusSideBar")
}

// openView shows a view, unfolding it, and its container, and gives them the focus.
func (s *shellState) openView(id string) {
	win := PeekWorkspace(s.Context())
	v, ok := win.Views.Registry().View(id)
	if !ok {
		return
	}
	win.Views.SetHidden(id, false)
	win.Views.SetCollapsed(id, false)
	s.openViewContainer(v.Container)
}

// goToLineEditor is the editor the go to line modes work on: the active text editor, or nil.
func (s *shellState) goToLineEditor() quickinput.GoToLineEditor {
	e := s.activeEditor()
	if e == nil || e.view == nil {
		return nil
	}
	win := PeekWorkspace(s.Context())
	return &goToLine{e: e, saved: e.view.ViewState(), jump: func(p buffer.Position) { s.jumpTo(e, p) }, log: func(err error) {
		win.Process.Logger.Warn("cannot restore the view of the editor", "error", describe(err))
	}}
}

// goToLine adapts the view model of the active editor to what the go to line modes need. Lines and columns
// count from 1 and a column is a byte column.
type goToLine struct {
	e     *textEditor
	saved editor.ViewState
	log   func(error)
	jump  func(buffer.Position)
}

func (g *goToLine) snap() buffer.Snapshot { return g.e.doc.Snapshot() }

func (g *goToLine) LineCount() int { return g.snap().LineCount() }

func (g *goToLine) LineContent(line int) string {
	text, err := g.snap().LineContent(line - 1)
	if err != nil {
		return ""
	}
	return text
}

func (g *goToLine) TabSize() int {
	tab, _ := g.e.doc.Indentation()
	return tab
}

func (g *goToLine) Length() int { return g.snap().Len() }

func (g *goToLine) PositionAt(offset int) (line, col int) {
	p := g.snap().PositionAt(offset)
	return p.Line + 1, p.Col + 1
}

func (g *goToLine) Reveal(line, col int) {
	g.e.view.RevealPositionInCenter(buffer.Position{Line: line - 1, Col: col - 1})
	g.e.view.SetRangeHighlight(line - 1)
}

func (g *goToLine) ClearHighlight() { g.e.view.ClearRangeHighlight() }

// Go moves the cursor, as a jump: the place it leaves and the place it goes to are locations of the history.
func (g *goToLine) Go(line, col int) {
	g.jump(buffer.Position{Line: line - 1, Col: col - 1})
}

func (g *goToLine) Restore() {
	// the folds that cannot come back are dropped; the cursors and the scroll come back
	if err := g.e.view.RestoreView(g.saved); err != nil {
		g.log(err)
	}
}
