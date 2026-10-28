package terminal

// The ids of the extension (SPEC §10.25).
const (
	// ID is the id of the extension.
	ID = "emetos.terminal"
	// ViewID is the panel view, the "Terminal" tab of the panel (VS Code's container id is terminal).
	ViewID = "emetos.terminal.panel"
)

// The commands of the extension: VS Code's ids (TerminalCommandId).
const (
	CmdToggle          = "workbench.action.terminal.toggleTerminal"
	CmdNew             = "workbench.action.terminal.new"
	CmdNewWithProfile  = "workbench.action.terminal.newWithProfile"
	CmdSplit           = "workbench.action.terminal.split"
	CmdKill            = "workbench.action.terminal.kill"
	CmdFocus           = "workbench.action.terminal.focus"
	CmdFocusNext       = "workbench.action.terminal.focusNext"
	CmdFocusPrevious   = "workbench.action.terminal.focusPrevious"
	CmdFocusNextPane   = "workbench.action.terminal.focusNextPane"
	CmdFocusPrevPane   = "workbench.action.terminal.focusPreviousPane"
	CmdResizePaneLeft  = "workbench.action.terminal.resizePaneLeft"
	CmdResizePaneRight = "workbench.action.terminal.resizePaneRight"
	CmdResizePaneUp    = "workbench.action.terminal.resizePaneUp"
	CmdResizePaneDown  = "workbench.action.terminal.resizePaneDown"
	CmdCopySelection   = "workbench.action.terminal.copySelection"
	CmdPaste           = "workbench.action.terminal.paste"
	CmdPasteSelection  = "workbench.action.terminal.pasteSelection"
	CmdSelectAll       = "workbench.action.terminal.selectAll"
	CmdClear           = "workbench.action.terminal.clear"
	CmdClearSelection  = "workbench.action.terminal.clearSelection"
	CmdScrollUp        = "workbench.action.terminal.scrollUp"
	CmdScrollDown      = "workbench.action.terminal.scrollDown"
	CmdScrollUpPage    = "workbench.action.terminal.scrollUpPage"
	CmdScrollDownPage  = "workbench.action.terminal.scrollDownPage"
	CmdScrollToTop     = "workbench.action.terminal.scrollToTop"
	CmdScrollToBottom  = "workbench.action.terminal.scrollToBottom"

	// The commands of step 17d.3: find, and running text in the terminal.
	CmdFocusFind            = "workbench.action.terminal.focusFind"
	CmdHideFind             = "workbench.action.terminal.hideFind"
	CmdFindNext             = "workbench.action.terminal.findNext"
	CmdFindPrevious         = "workbench.action.terminal.findPrevious"
	CmdToggleFindRegex      = "workbench.action.terminal.toggleFindRegex"
	CmdToggleFindWholeWord  = "workbench.action.terminal.toggleFindWholeWord"
	CmdToggleFindCaseSens   = "workbench.action.terminal.toggleFindCaseSensitive"
	CmdRunSelectedText      = "workbench.action.terminal.runSelectedText"
	CmdRunActiveFile        = "workbench.action.terminal.runActiveFile"
	CmdSendSequence         = "workbench.action.terminal.sendSequence"
	CmdRename               = "workbench.action.terminal.rename"
	CmdRenameWithArgs       = "workbench.action.terminal.renameWithArgs"
	CmdSelectDefaultProfile = "workbench.action.terminal.selectDefaultShell"
	CmdSearchWorkspace      = "workbench.action.terminal.searchWorkspace"

	// The commands of step 17d.5: shell integration. Navigation, the history, the last command, sticky scroll and the quick fixes.
	CmdScrollToPrevCommand   = "workbench.action.terminal.scrollToPreviousCommand"
	CmdScrollToNextCommand   = "workbench.action.terminal.scrollToNextCommand"
	CmdSelectToPrevCommand   = "workbench.action.terminal.selectToPreviousCommand"
	CmdSelectToNextCommand   = "workbench.action.terminal.selectToNextCommand"
	CmdSelectToPrevLine      = "workbench.action.terminal.selectToPreviousLine"
	CmdSelectToNextLine      = "workbench.action.terminal.selectToNextLine"
	CmdRunRecentCommand      = "workbench.action.terminal.runRecentCommand"
	CmdGoToRecentDirectory   = "workbench.action.terminal.goToRecentDirectory"
	CmdClearPreviousHistory  = "workbench.action.terminal.clearPreviousSessionHistory"
	CmdCopyLastCommand       = "workbench.action.terminal.copyLastCommand"
	CmdCopyLastOutput        = "workbench.action.terminal.copyLastCommandOutput"
	CmdCopyLastCommandOutput = "workbench.action.terminal.copyLastCommandAndLastCommandOutput"
	CmdToggleStickyScroll    = "workbench.action.terminal.toggleStickyScroll"
	CmdShowQuickFixes        = "workbench.action.terminal.showQuickFixes"

	// The commands of step 17d.6: terminal IntelliSense (terminalContrib/suggest, VS Code's ids).
	CmdTriggerSuggest        = "workbench.action.terminal.triggerSuggest"
	CmdHideSuggest           = "workbench.action.terminal.hideSuggestWidget"
	CmdSelectNextSuggest     = "workbench.action.terminal.selectNextSuggestion"
	CmdSelectPrevSuggest     = "workbench.action.terminal.selectPrevSuggestion"
	CmdSelectNextPageSuggest = "workbench.action.terminal.selectNextPageSuggestion"
	CmdSelectPrevPageSuggest = "workbench.action.terminal.selectPrevPageSuggestion"
	CmdAcceptSuggest         = "workbench.action.terminal.acceptSelectedSuggestion"
	CmdAcceptSuggestEnter    = "workbench.action.terminal.acceptSelectedSuggestionEnter"
	CmdToggleSuggestDetails  = "workbench.action.terminal.toggleSuggestionDetails"
)

// findInFilesCommand is the id of the command of the Search extension that searchWorkspace runs (VS Code's
// workbench.action.findInFiles).
const findInFilesCommand = "workbench.action.findInFiles"

// focusAtIndex is the id of the command that focuses the n-th terminal (1 to 9).
func focusAtIndex(n int) string {
	return "workbench.action.terminal.focusAtIndex" + string(rune('0'+n))
}

// The context keys the extension sets (VS Code's TerminalContextKeyStrings). The host accepts them because the manifest
// declares them (Contributes.ContextKeys).
const (
	keyFocus                 = "terminalFocus"
	keyFocusInAny            = "terminalFocusInAny"
	keyIsOpen                = "terminalIsOpen"
	keyCount                 = "terminalCount"
	keyGroupCount            = "terminalGroupCount"
	keyProcessSupported      = "terminalProcessSupported"
	keyTextSelected          = "terminalTextSelected"
	keyTextSelectedInFocused = "terminalTextSelectedInFocused"
	keyFindVisible           = "terminalFindVisible"
	keyFindFocus             = "terminalFindFocused"
	keyFindInputFocus        = "terminalFindInputFocused"
	keySplitTerminal         = "terminalSplitTerminal"
	keySplitPaneActive       = "terminalSplitPaneActive"
	keyHasBeenCreated        = "terminalHasBeenCreated"
	keyAltBuffer             = "terminalAltBufferActive"
	keyTabsFocus             = "terminalTabsFocus"
	keyEditorFocus           = "terminalEditorFocus"
	// keyShellIntegration is true while the shell of the active terminal sends the sequences of shell integration (Emetos's own
	// key: VS Code has none; it keeps Ctrl+Shift+Up and Down scrolling a terminal that has no commands to select).
	keyShellIntegration = "terminalShellIntegrationActive"
	// keySuggestVisible is true while the suggest list is shown in the active terminal (VS Code's terminalSuggestWidgetVisible);
	// keySuggestEnabled while terminal IntelliSense is on and the shell of the active terminal sends the sequences it needs
	// (Emetos's own key, for Ctrl+Space).
	keySuggestVisible = "terminalSuggestWidgetVisible"
	keySuggestEnabled = "terminalSuggestEnabled"
)

// contextKeys lists them for the manifest.
var contextKeys = []string{
	keyFocus, keyFocusInAny, keyIsOpen, keyCount, keyGroupCount, keyProcessSupported, keyTextSelected, keyTextSelectedInFocused,
	keyFindVisible, keyFindFocus, keyFindInputFocus, keySplitTerminal, keySplitPaneActive, keyHasBeenCreated, keyAltBuffer, keyTabsFocus, keyEditorFocus,
	keyShellIntegration, keySuggestVisible, keySuggestEnabled,
}
