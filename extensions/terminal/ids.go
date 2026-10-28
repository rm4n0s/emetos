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
)

// contextKeys lists them for the manifest.
var contextKeys = []string{
	keyFocus, keyFocusInAny, keyIsOpen, keyCount, keyGroupCount, keyProcessSupported, keyTextSelected, keyTextSelectedInFocused,
	keyFindVisible, keyFindFocus, keyFindInputFocus, keySplitTerminal, keySplitPaneActive, keyHasBeenCreated, keyAltBuffer, keyTabsFocus, keyEditorFocus,
}
