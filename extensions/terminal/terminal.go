package terminal

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// New is the Factory of the extension.
func New() extension.Extension { return &terminals{} }

// commandSpecs are the commands the extension declares: VS Code's ids and titles (terminalActions.ts). They have no When, so
// that running one in a window tab where the extension is not active yet activates it.
func commandSpecs() []extension.CommandSpec {
	c := func(id, title string) extension.CommandSpec {
		return extension.CommandSpec{ID: id, Title: title, Category: "Terminal"}
	}
	specs := []extension.CommandSpec{
		{ID: CmdToggle, Title: "Toggle Terminal", Category: "View"},
		c(CmdNew, "Create New Terminal"), c(CmdNewWithProfile, "Create New Terminal (With Profile)..."),
		c(CmdSplit, "Split Terminal"), c(CmdKill, "Kill the Active Terminal Instance"), c(CmdFocus, "Focus Terminal"),
		c(CmdFocusNext, "Focus Next Terminal Group"), c(CmdFocusPrevious, "Focus Previous Terminal Group"),
		c(CmdFocusNextPane, "Focus Next Terminal in Terminal Group"), c(CmdFocusPrevPane, "Focus Previous Terminal in Terminal Group"),
		c(CmdResizePaneLeft, "Resize Terminal Left"), c(CmdResizePaneRight, "Resize Terminal Right"),
		c(CmdResizePaneUp, "Resize Terminal Up"), c(CmdResizePaneDown, "Resize Terminal Down"),
		c(CmdCopySelection, "Copy Selection"), c(CmdPaste, "Paste into Active Terminal"),
		c(CmdPasteSelection, "Paste Selection into Active Terminal"), c(CmdSelectAll, "Select All"), c(CmdClear, "Clear"),
		c(CmdClearSelection, "Clear Selection"), c(CmdScrollUp, "Scroll Up (Line)"), c(CmdScrollDown, "Scroll Down (Line)"),
		c(CmdScrollUpPage, "Scroll Up (Page)"), c(CmdScrollDownPage, "Scroll Down (Page)"), c(CmdScrollToTop, "Scroll to Top"),
		c(CmdScrollToBottom, "Scroll to Bottom"),
		c(CmdFocusFind, "Focus Find"), c(CmdHideFind, "Hide Find"), c(CmdFindNext, "Find Next"), c(CmdFindPrevious, "Find Previous"),
		c(CmdToggleFindRegex, "Toggle Find Using Regex"), c(CmdToggleFindWholeWord, "Toggle Find Using Whole Word"),
		c(CmdToggleFindCaseSens, "Toggle Find Using Case Sensitive"),
		c(CmdRunSelectedText, "Run Selected Text In Active Terminal"), c(CmdRunActiveFile, "Run Active File In Active Terminal"),
		c(CmdSendSequence, "Send Sequence"), c(CmdRename, "Rename..."), c(CmdRenameWithArgs, "Rename the Currently Active Terminal"),
		c(CmdSelectDefaultProfile, "Select Default Profile"), c(CmdSearchWorkspace, "Search Workspace"),
		c(CmdScrollToPrevCommand, "Scroll to Previous Command"), c(CmdScrollToNextCommand, "Scroll to Next Command"),
		c(CmdSelectToPrevCommand, "Select to Previous Command"), c(CmdSelectToNextCommand, "Select to Next Command"),
		c(CmdSelectToPrevLine, "Select to Previous Line"), c(CmdSelectToNextLine, "Select to Next Line"),
		c(CmdRunRecentCommand, "Run Recent Command..."), c(CmdGoToRecentDirectory, "Go to Recent Directory..."),
		c(CmdClearPreviousHistory, "Clear Previous Session History"),
		c(CmdCopyLastCommand, "Copy Last Command"), c(CmdCopyLastOutput, "Copy Last Command Output"),
		c(CmdCopyLastCommandOutput, "Copy Last Command and Output"),
		c(CmdToggleStickyScroll, "Toggle Sticky Scroll"), c(CmdShowQuickFixes, "Show Terminal Quick Fixes"),
	}
	for i := 1; i <= 9; i++ {
		specs = append(specs, c(focusAtIndex(i), "Focus Terminal "+string(rune('0'+i))))
	}
	return specs
}

// keybindings are the default keys of VS Code on Linux, with its when-clauses (terminalActions.ts and the clipboard
// contribution).
func keybindings() []extension.Keybinding {
	const inTerminal = keyFocus
	const normal = keyFocusInAny + " && !" + keyAltBuffer
	return []extension.Keybinding{
		{Command: CmdToggle, Key: "ctrl+`"},
		{Command: CmdNew, Key: "ctrl+shift+`"},
		{Command: CmdSplit, Key: "ctrl+shift+5", When: inTerminal},
		{Command: CmdFocusPrevPane, Key: "alt+left", When: inTerminal + " && " + keySplitPaneActive},
		{Command: CmdFocusPrevPane, Key: "alt+up", When: inTerminal + " && " + keySplitPaneActive},
		{Command: CmdFocusNextPane, Key: "alt+right", When: inTerminal + " && " + keySplitPaneActive},
		{Command: CmdFocusNextPane, Key: "alt+down", When: inTerminal + " && " + keySplitPaneActive},
		{Command: CmdResizePaneLeft, Key: "ctrl+shift+left", When: inTerminal},
		{Command: CmdResizePaneRight, Key: "ctrl+shift+right", When: inTerminal},
		{Command: CmdFocusNext, Key: "ctrl+pagedown", When: inTerminal + " && !" + keyEditorFocus},
		{Command: CmdFocusPrevious, Key: "ctrl+pageup", When: inTerminal + " && !" + keyEditorFocus},
		{Command: CmdCopySelection, Key: "ctrl+shift+c", When: keyTextSelected + " && " + inTerminal + " || " + keyTextSelectedInFocused},
		{Command: CmdPaste, Key: "ctrl+shift+v", When: inTerminal},
		{Command: CmdPasteSelection, Key: "shift+insert", When: inTerminal},
		{Command: CmdClearSelection, Key: "escape", When: keyFocusInAny + " && " + keyTextSelected + " && !" + keyFindVisible},
		{Command: CmdScrollUp, Key: "ctrl+shift+up", When: normal + " && !" + keyShellIntegration},
		{Command: CmdScrollDown, Key: "ctrl+shift+down", When: normal + " && !" + keyShellIntegration},
		{Command: CmdScrollUpPage, Key: "shift+pageup", When: normal},
		{Command: CmdScrollDownPage, Key: "shift+pagedown", When: normal},
		{Command: CmdScrollToTop, Key: "shift+home", When: normal},
		{Command: CmdScrollToBottom, Key: "shift+end", When: normal},
		// shell integration (terminalActions.ts, terminalContrib/history, terminalContrib/quickFix): VS Code binds Ctrl+Shift+Up and
		// Down to both the line scroll and the selection of commands; here the commands win when the shell sends sequences
		{Command: CmdScrollToPrevCommand, Key: "ctrl+up", When: inTerminal},
		{Command: CmdScrollToNextCommand, Key: "ctrl+down", When: inTerminal},
		{Command: CmdSelectToPrevCommand, Key: "ctrl+shift+up", When: inTerminal + " && " + keyShellIntegration},
		{Command: CmdSelectToNextCommand, Key: "ctrl+shift+down", When: inTerminal + " && " + keyShellIntegration},
		{Command: CmdRunRecentCommand, Key: "ctrl+alt+r", When: inTerminal},
		{Command: CmdGoToRecentDirectory, Key: "ctrl+g", When: inTerminal},
		{Command: CmdShowQuickFixes, Key: "ctrl+.", When: inTerminal},
		// find (terminalContrib/find/browser/terminal.find.contribution.ts): Enter in the input goes to the previous match and
		// Shift+Enter to the next, as VS Code's table has them
		{Command: CmdFocusFind, Key: "ctrl+f", When: keyFindFocus + " || " + keyFocusInAny},
		{Command: CmdHideFind, Key: "escape", When: keyFocusInAny + " && " + keyFindVisible},
		{Command: CmdHideFind, Key: "shift+escape", When: keyFocusInAny + " && " + keyFindVisible},
		{Command: CmdToggleFindRegex, Key: "alt+r", When: keyFindVisible},
		{Command: CmdToggleFindWholeWord, Key: "alt+w", When: keyFindVisible},
		{Command: CmdToggleFindCaseSens, Key: "alt+c", When: keyFindVisible},
		{Command: CmdFindNext, Key: "f3", When: keyFocusInAny + " || " + keyFindFocus},
		{Command: CmdFindNext, Key: "shift+enter", When: keyFindInputFocus},
		{Command: CmdFindPrevious, Key: "shift+f3", When: keyFocusInAny + " || " + keyFindFocus},
		{Command: CmdFindPrevious, Key: "enter", When: keyFindInputFocus},
		// Ctrl+Shift+F with text selected searches the workspace for it (the Search view's own Ctrl+Shift+F has no when-clause;
		// this one is more specific and the later of the two)
		{Command: CmdSearchWorkspace, Key: "ctrl+shift+f", When: inTerminal + " && " + keyTextSelected},
	}
}

// Manifest implements extension.Extension.
func (*terminals) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Terminal", Version: termVersion,
		Description: "Shells in the panel: VS Code's integrated terminal, with Emetos's own emulator.",
		// commands.execute: Search Workspace runs the Search extension's Find in Files (SPEC §10.19 "Privileges")
		Privileges: []string{extension.PrivilegeCommandsExecute},
		// The terminals are events and requests (SPEC §10.25), so the extension is there from the start: a client of Relay asks
		// for the list of terminals of a workspace whose panel was never opened.
		ActivationEvents: []string{extension.OnStartup},
		Events: extension.EventDecl{
			Subscribes: []string{extension.TopicEditorActiveChanged, extension.TopicEditorSelectionChanged, extension.TopicDocumentClosed, TopicClientGone},
		},
		Contributes: extension.Contributes{
			Commands:    commandSpecs(),
			Keybindings: keybindings(),
			Settings:    settingSpecs(),
			ContextKeys: contextKeys,
			PanelViews:  []extension.PanelViewSpec{{ID: ViewID, Title: "Terminal", Icon: "terminal", Order: 3}},
			// (the events below are what the extension follows to know the editor in front)
			ViewActions: []extension.ViewActionSpec{
				{View: ViewID, Command: CmdNew, Icon: "add", Tooltip: "New Terminal", Order: 1},
				{View: ViewID, Command: CmdNewWithProfile, Icon: "chevron-down", Tooltip: "Launch Profile...", Order: 2},
				{View: ViewID, Command: CmdSplit, Icon: "split-horizontal", Tooltip: "Split Terminal", Order: 3},
				{View: ViewID, Command: CmdKill, Icon: "trash", Tooltip: "Kill Terminal", Order: 4},
			},
			Menus: []extension.MenuItem{
				{Menu: extension.MenuTerminal, Command: CmdNew, Title: "New Terminal", Group: "1_create", Order: 1},
				{Menu: extension.MenuTerminal, Command: CmdSplit, Title: "Split Terminal", Group: "1_create", Order: 2},
			},
		},
	}
}

// Activate implements extension.Extension. The terminals end with the context: the window tab closing or the extension being
// turned off ends every shell.
func (t *terminals) Activate(ctx context.Context, api extension.API) error {
	t.api, t.ctx, t.folder = api, ctx, api.Scope().Folder
	if t.folder != "" {
		if real, err := filepath.EvalSymlinks(t.folder); err == nil {
			t.folder = real // links are confined to the folder as it really is
		}
	}
	if t.env.lookPath == nil {
		t.env = osEnviron()
	}
	c := readConfig(api.Settings())
	t.cfg = &c
	t.hist = newHistoryStore(t.stateDir, func() int { return t.cfg.historyLimit })
	t.menu = ui.NewContextMenuController()
	var err error
	t.handle, err = api.Views().Provide(ViewID, extension.ViewContent{
		Build:        t.build,
		AllowCommand: t.allowCommand,
		SaveState: func() (any, error) {
			t.visible, t.suppressOpen = false, false
			return nil, nil
		},
	})
	if err != nil {
		return err
	}
	api.Settings().OnChange(t.settingsChanged)
	for _, pattern := range []string{"emetos.editor.*", extension.TopicDocumentClosed} {
		if _, err := api.Events().Subscribe(pattern, t.editorEvent); err != nil {
			return err
		}
	}
	for id, h := range t.handlers() {
		if err := api.Commands().Handle(id, h); err != nil {
			return err
		}
	}
	if err := t.registerRequests(); err != nil {
		return err
	}
	t.syncKeys()
	return nil
}

// allowCommand says whether a key bound to a command is run by the window while a terminal has the focus: the commands of
// terminal.integrated.commandsToSkipShell are; every other key goes to the shell (Ctrl+W, Ctrl+B, Ctrl+R...). With
// terminal.integrated.sendKeybindingsToShell nothing but the terminal's own toggle is skipped.
func (t *terminals) allowCommand(commandID string) bool {
	if t.paneFocus == nil {
		return true // the focus is in the tabs list or the question about a paste
	}
	if t.cfg.sendKeys {
		return commandID == CmdToggle
	}
	return t.cfg.skip.has(commandID)
}

// target is the terminal a command acts on: the one with the keyboard focus, else the active one.
func (t *terminals) target() *entry {
	if t.paneFocus != nil {
		return t.paneFocus
	}
	return t.activeEntry()
}

// handlers are the handlers of the commands.
func (t *terminals) handlers() map[string]extension.CommandHandler {
	onPane := func(f func(pv *paneView)) extension.CommandHandler {
		return func(context.Context, any) error {
			if e := t.target(); e != nil {
				f(e.pv)
			}
			return nil
		}
	}
	handlers := map[string]extension.CommandHandler{
		CmdToggle:         func(context.Context, any) error { t.toggle(); return nil },
		CmdNew:            func(_ context.Context, args any) error { t.newTerminal(args); return nil },
		CmdNewWithProfile: func(context.Context, any) error { t.pickProfile(); return nil },
		CmdSplit: func(context.Context, any) error {
			if t.activeEntry() == nil {
				t.newTerminal(nil)
				return nil
			}
			if _, err := t.open(openOpts{split: true}); err != nil {
				t.api.Log().Warn("the terminal cannot be split", "err", err)
			}
			return nil
		},
		CmdKill: func(context.Context, any) error {
			if e := t.target(); e != nil {
				t.kill(e)
			}
			return nil
		},
		CmdFocus: func(context.Context, any) error {
			if e := t.activeEntry(); e != nil {
				t.focusEntry(e, true)
			} else {
				t.handle.Reveal(true)
			}
			return nil
		},
		CmdFocusNext:       func(context.Context, any) error { t.moveGroup(1); return nil },
		CmdFocusPrevious:   func(context.Context, any) error { t.moveGroup(-1); return nil },
		CmdFocusNextPane:   func(context.Context, any) error { t.movePane(1); return nil },
		CmdFocusPrevPane:   func(context.Context, any) error { t.movePane(-1); return nil },
		CmdResizePaneLeft:  func(context.Context, any) error { t.resizePane(-1); return nil },
		CmdResizePaneRight: func(context.Context, any) error { t.resizePane(1); return nil },
		// the terminals sit side by side, so there is no height to give or take
		CmdResizePaneUp:   func(context.Context, any) error { return nil },
		CmdResizePaneDown: func(context.Context, any) error { return nil },
		CmdCopySelection:  onPane(func(pv *paneView) { pv.copySelection() }),
		CmdPaste:          onPane(func(pv *paneView) { pv.pasteClipboard() }),
		CmdPasteSelection: onPane(func(pv *paneView) { pv.pastePrimary() }),
		CmdSelectAll:      onPane(func(pv *paneView) { pv.selectAll() }),
		CmdClear:          onPane(func(pv *paneView) { pv.clear() }),
		CmdClearSelection: onPane(func(pv *paneView) { pv.setSelection(selection{}) }),
		CmdScrollUp:       onPane(func(pv *paneView) { pv.scrollBy(1) }),
		CmdScrollDown:     onPane(func(pv *paneView) { pv.scrollBy(-1) }),
		CmdScrollUpPage:   onPane(func(pv *paneView) { pv.scrollBy(max(pv.rows-1, 1)) }),
		CmdScrollDownPage: onPane(func(pv *paneView) { pv.scrollBy(-max(pv.rows-1, 1)) }),
		CmdScrollToTop:    onPane(func(pv *paneView) { pv.scrollTo(pv.frame.Scrollback) }),
		CmdScrollToBottom: onPane(func(pv *paneView) { pv.scrollTo(0) }),

		CmdFocusFind:            func(context.Context, any) error { t.onFind(func(pv *paneView) { pv.showFind() }); return nil },
		CmdHideFind:             func(context.Context, any) error { t.onFind(func(pv *paneView) { pv.hideFind() }); return nil },
		CmdFindNext:             func(context.Context, any) error { t.onFind(func(pv *paneView) { pv.moveFind(false) }); return nil },
		CmdFindPrevious:         func(context.Context, any) error { t.onFind(func(pv *paneView) { pv.moveFind(true) }); return nil },
		CmdToggleFindRegex:      func(context.Context, any) error { t.onFind(func(pv *paneView) { pv.toggleFind("regex") }); return nil },
		CmdToggleFindWholeWord:  func(context.Context, any) error { t.onFind(func(pv *paneView) { pv.toggleFind("word") }); return nil },
		CmdToggleFindCaseSens:   func(context.Context, any) error { t.onFind(func(pv *paneView) { pv.toggleFind("case") }); return nil },
		CmdRunSelectedText:      func(ctx context.Context, _ any) error { t.runSelectedText(ctx); return nil },
		CmdRunActiveFile:        func(context.Context, any) error { t.runActiveFile(); return nil },
		CmdSendSequence:         func(ctx context.Context, args any) error { t.sendSequence(ctx, args); return nil },
		CmdRename:               func(context.Context, any) error { t.rename(); return nil },
		CmdRenameWithArgs:       func(_ context.Context, args any) error { t.renameWithArgs(args); return nil },
		CmdSelectDefaultProfile: func(context.Context, any) error { t.selectDefaultProfile(); return nil },
		CmdSearchWorkspace:      func(ctx context.Context, _ any) error { t.searchWorkspace(ctx); return nil },

		CmdScrollToPrevCommand:   onPane(func(pv *paneView) { pv.scrollToCommand(true) }),
		CmdScrollToNextCommand:   onPane(func(pv *paneView) { pv.scrollToCommand(false) }),
		CmdSelectToPrevCommand:   onPane(func(pv *paneView) { pv.selectToCommand(true) }),
		CmdSelectToNextCommand:   onPane(func(pv *paneView) { pv.selectToCommand(false) }),
		CmdSelectToPrevLine:      onPane(func(pv *paneView) { pv.selectLine(true) }),
		CmdSelectToNextLine:      onPane(func(pv *paneView) { pv.selectLine(false) }),
		CmdRunRecentCommand:      func(context.Context, any) error { t.runRecentCommand(); return nil },
		CmdGoToRecentDirectory:   func(context.Context, any) error { t.goToRecentDirectory(); return nil },
		CmdClearPreviousHistory:  func(context.Context, any) error { t.clearPreviousSessionHistory(); return nil },
		CmdCopyLastCommand:       func(context.Context, any) error { t.copyLast(true, false); return nil },
		CmdCopyLastOutput:        func(context.Context, any) error { t.copyLast(false, true); return nil },
		CmdCopyLastCommandOutput: func(context.Context, any) error { t.copyLast(true, true); return nil },
		CmdToggleStickyScroll:    func(context.Context, any) error { t.toggleStickyScroll(); return nil },
		CmdShowQuickFixes:        func(context.Context, any) error { t.showQuickFixes(); return nil },
	}
	for i := 1; i <= 9; i++ {
		handlers[focusAtIndex(i)] = func(context.Context, any) error { t.focusTerminalAt(i - 1); return nil }
	}
	return handlers
}

// toggle shows the terminal with the keyboard focus in it, or hides the panel when the terminal has the focus (Ctrl+`).
func (t *terminals) toggle() {
	if t.paneFocus != nil {
		t.handle.Hide()
		return
	}
	if e := t.activeEntry(); e != nil {
		t.focusEntry(e, true)
		return
	}
	t.handle.Reveal(true) // the view makes the first terminal when it is shown
}

// newTerminal is the command Create New Terminal. The argument may be the name of a profile or an object with profileName,
// cwd and name, as VS Code's.
func (t *terminals) newTerminal(args any) {
	o := openOpts{}
	switch a := args.(type) {
	case string:
		o.profile = t.profileNamed(a)
	case map[string]any:
		if n, _ := a["profileName"].(string); n != "" {
			o.profile = t.profileNamed(n)
		}
		o.dir, _ = a["cwd"].(string)
		o.name, _ = a["name"].(string)
	}
	if _, err := t.open(o); err != nil {
		t.api.Log().Warn("the terminal cannot be started", "err", err)
	}
}

// profileNamed finds a profile by its name; nil when there is none.
func (t *terminals) profileNamed(name string) *profile {
	for _, p := range t.profiles() {
		if strings.EqualFold(p.name, name) {
			p := p
			return &p
		}
	}
	return nil
}

// pickProfile is the command Create New Terminal (With Profile): the profiles in a quick pick.
func (t *terminals) pickProfile() {
	profiles := t.profiles()
	if len(profiles) == 0 {
		t.newTerminal(nil)
		return
	}
	def := t.defaultProfile()
	items := make([]extension.QuickPickItem, len(profiles))
	for i, p := range profiles {
		icon := p.icon
		if _, ok := ui.CodiconCode(icon); !ok {
			icon = "terminal"
		}
		items[i] = extension.QuickPickItem{Label: "$(" + icon + ") " + p.name, Description: p.path}
		if p.name == def.name {
			items[i].Detail = "default"
		}
	}
	err := t.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select the terminal profile to create", Items: items, MatchOnDescription: true}, func(i int) {
		if i < 0 || i >= len(profiles) {
			return
		}
		if _, err := t.open(openOpts{profile: &profiles[i]}); err != nil {
			t.api.Log().Warn("the terminal cannot be started", "err", err)
		}
	})
	if err != nil {
		t.api.Log().Warn("the profiles cannot be listed", "err", err)
	}
}

// moveGroup focuses the terminal of the next (or previous) tab of the list, around.
func (t *terminals) moveGroup(delta int) {
	n := len(t.groups)
	if n == 0 {
		return
	}
	g := t.groups[((t.cur+delta)%n+n)%n]
	t.focusEntry(g.entries[min(g.active, len(g.entries)-1)], false)
}

// movePane focuses the next (or previous) terminal of the active tab, around.
func (t *terminals) movePane(delta int) {
	g := t.activeGroup()
	if g == nil || len(g.entries) < 2 {
		return
	}
	n := len(g.entries)
	t.focusEntry(g.entries[((g.active+delta)%n+n)%n], false)
}

// resizePane moves the sash of the active pane by a step: toward the right with a positive delta. The last pane has its sash
// at the left, so the same key moves it the other way, as the key names the direction the sash goes.
func (t *terminals) resizePane(delta int) {
	g := t.activeGroup()
	if g == nil || len(g.entries) < 2 {
		return
	}
	sizes := g.split.Sizes()
	i := min(g.active, len(sizes)-1)
	if i < 0 {
		return
	}
	step := 20.0 * float64(delta)
	if i == len(sizes)-1 {
		step = -step
	}
	g.split.SetSize(i, sizes[i]+step)
}
