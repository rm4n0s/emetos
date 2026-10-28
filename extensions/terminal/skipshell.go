package terminal

import "slices"

// defaultSkipShell is VS Code's DEFAULT_COMMANDS_TO_SKIP_SHELL (src/vs/workbench/contrib/terminal/common/terminal.ts and the
// lists of the terminal contributions of find, history and suggest): the commands whose keys go to the workbench while a
// terminal has the focus, because they are the terminal's own or the ones a user expects to work everywhere (Ctrl+P). The
// commands the user adds to terminal.integrated.commandsToSkipShell are skipped too, and one written with a leading '-' is
// taken out of the list.
//
// Ported from VS Code: src/vs/workbench/contrib/terminal/common/terminal.ts (DEFAULT_COMMANDS_TO_SKIP_SHELL)
var defaultSkipShell = []string{
	"workbench.action.terminal.clearSelection", "workbench.action.terminal.clear", "workbench.action.terminal.copyAndClearSelection",
	"workbench.action.terminal.copySelection", "workbench.action.terminal.copySelectionAsHtml", "workbench.action.terminal.copyLastCommand",
	"workbench.action.terminal.copyLastCommandOutput", "workbench.action.terminal.copyLastCommandAndLastCommandOutput",
	"workbench.action.terminal.deleteToLineStart", "workbench.action.terminal.deleteWordLeft", "workbench.action.terminal.deleteWordRight",
	"workbench.action.terminal.focusNextPane", "workbench.action.terminal.focusNext", "workbench.action.terminal.focusPreviousPane",
	"workbench.action.terminal.focusPrevious", "workbench.action.terminal.focus", "workbench.action.terminal.sizeToContentWidth",
	"workbench.action.terminal.kill", "workbench.action.terminal.killEditor", "workbench.action.terminal.moveToEditor",
	"workbench.action.terminal.moveToLineEnd", "workbench.action.terminal.moveToLineStart", "workbench.action.terminal.moveToTerminalPanel",
	"workbench.action.terminal.newInActiveWorkspace", "workbench.action.terminal.new", "workbench.action.terminal.newInNewWindow",
	"workbench.action.terminal.paste", "workbench.action.terminal.pastePwsh", "workbench.action.terminal.pasteSelection",
	"workbench.action.terminal.resizePaneDown", "workbench.action.terminal.resizePaneLeft", "workbench.action.terminal.resizePaneRight",
	"workbench.action.terminal.resizePaneUp", "workbench.action.terminal.runActiveFile", "workbench.action.terminal.runSelectedText",
	"workbench.action.terminal.scrollDown", "workbench.action.terminal.scrollDownPage", "workbench.action.terminal.scrollToBottom",
	"workbench.action.terminal.scrollToNextCommand", "workbench.action.terminal.scrollToPreviousCommand", "workbench.action.terminal.scrollToTop",
	"workbench.action.terminal.scrollUp", "workbench.action.terminal.scrollUpPage", "workbench.action.terminal.sendSequence",
	"workbench.action.terminal.selectAll", "workbench.action.terminal.selectToNextCommand", "workbench.action.terminal.selectToNextLine",
	"workbench.action.terminal.selectToPreviousCommand", "workbench.action.terminal.selectToPreviousLine",
	"workbench.action.terminal.splitInActiveWorkspace", "workbench.action.terminal.split", "workbench.action.terminal.toggleTerminal",
	"workbench.action.terminal.focusHover", "workbench.action.terminal.stopVoice", "workbench.action.terminal.sendSignal",
	"workbench.action.tasks.rerunForActiveTerminal", "editor.action.toggleTabFocusMode", "notifications.hideList", "notifications.hideToasts",
	"workbench.action.closeQuickOpen", "workbench.action.quickOpen", "workbench.action.quickOpenPreviousEditor", "workbench.action.showCommands",
	"workbench.action.tasks.build", "workbench.action.tasks.restartTask", "workbench.action.tasks.runTask", "workbench.action.tasks.reRunTask",
	"workbench.action.tasks.showLog", "workbench.action.tasks.showTasks", "workbench.action.tasks.terminate", "workbench.action.tasks.test",
	"workbench.action.toggleFullScreen",
	"workbench.action.terminal.focusAtIndex1", "workbench.action.terminal.focusAtIndex2", "workbench.action.terminal.focusAtIndex3",
	"workbench.action.terminal.focusAtIndex4", "workbench.action.terminal.focusAtIndex5", "workbench.action.terminal.focusAtIndex6",
	"workbench.action.terminal.focusAtIndex7", "workbench.action.terminal.focusAtIndex8", "workbench.action.terminal.focusAtIndex9",
	"workbench.action.focusSecondEditorGroup", "workbench.action.focusThirdEditorGroup", "workbench.action.focusFourthEditorGroup",
	"workbench.action.focusFifthEditorGroup", "workbench.action.focusSixthEditorGroup", "workbench.action.focusSeventhEditorGroup",
	"workbench.action.focusEighthEditorGroup", "workbench.action.focusNextPart", "workbench.action.focusPreviousPart",
	"workbench.action.nextPanelView", "workbench.action.previousPanelView", "workbench.action.nextSideBarView", "workbench.action.previousSideBarView",
	"workbench.action.debug.disconnect", "workbench.action.debug.start", "workbench.action.debug.stop", "workbench.action.debug.run",
	"workbench.action.debug.restart", "workbench.action.debug.continue", "workbench.action.debug.pause", "workbench.action.debug.stepInto",
	"workbench.action.debug.stepOut", "workbench.action.debug.stepOver",
	"workbench.action.nextEditor", "workbench.action.previousEditor", "workbench.action.nextEditorInGroup", "workbench.action.previousEditorInGroup",
	"workbench.action.openNextRecentlyUsedEditor", "workbench.action.openPreviousRecentlyUsedEditor",
	"workbench.action.openNextRecentlyUsedEditorInGroup", "workbench.action.openPreviousRecentlyUsedEditorInGroup",
	"workbench.action.quickOpenPreviousRecentlyUsedEditor", "workbench.action.quickOpenLeastRecentlyUsedEditor",
	"workbench.action.quickOpenPreviousRecentlyUsedEditorInGroup", "workbench.action.quickOpenLeastRecentlyUsedEditorInGroup",
	"workbench.action.focusActiveEditorGroup", "workbench.action.focusFirstEditorGroup", "workbench.action.focusLastEditorGroup",
	"workbench.action.firstEditorInGroup", "workbench.action.lastEditorInGroup",
	"workbench.action.navigateUp", "workbench.action.navigateDown", "workbench.action.navigateRight", "workbench.action.navigateLeft",
	"workbench.action.togglePanel", "workbench.action.quickOpenView", "workbench.action.toggleMaximizedPanel",
	"workbench.action.zoomIn", "workbench.action.zoomOut", "workbench.action.zoomReset", "notification.acceptPrimaryAction", "runCommands",
	// the terminal's contributions: find, history and suggest (terminalContrib/*/common/*.ts)
	"workbench.action.terminal.focusFind", "workbench.action.terminal.hideFind", "workbench.action.terminal.findNext",
	"workbench.action.terminal.findPrevious", "workbench.action.terminal.toggleFindRegex", "workbench.action.terminal.toggleFindWholeWord",
	"workbench.action.terminal.toggleFindCaseSensitive", "workbench.action.terminal.searchWorkspace",
	"workbench.action.terminal.goToRecentDirectory", "workbench.action.terminal.runRecentCommand",
	"workbench.action.terminal.selectPrevSuggestion", "workbench.action.terminal.selectPrevPageSuggestion",
	"workbench.action.terminal.selectNextSuggestion", "workbench.action.terminal.selectNextPageSuggestion",
	"workbench.action.terminal.acceptSelectedSuggestion", "workbench.action.terminal.acceptSelectedSuggestionEnter",
	"workbench.action.terminal.hideSuggestWidget", "workbench.action.terminal.triggerSuggest",
	"workbench.action.terminal.toggleSuggestionDetails", "workbench.action.terminal.toggleSuggestionDetailsFocus",
	// Emetos: the core command that hides the panel while the terminal has the focus (Ctrl+`, see toggleKey)
	"workbench.panel.extension." + ViewID + ".toggle",
}

// skipSet is the set of commands whose keys skip the shell: the default list with the user's additions, less the commands the
// user took out with a '-' in front.
type skipSet struct {
	commands map[string]bool
}

// newSkipSet makes the set from the user's list.
func newSkipSet(user []string) skipSet {
	m := make(map[string]bool, len(defaultSkipShell)+len(user))
	for _, c := range defaultSkipShell {
		m[c] = true
	}
	for _, c := range user {
		if len(c) > 1 && c[0] == '-' {
			delete(m, c[1:])
		} else if c != "" {
			m[c] = true
		}
	}
	return skipSet{commands: m}
}

// has tells whether the key of the command goes to the workbench.
func (s skipSet) has(command string) bool { return s.commands[command] }

// sortedDefaults lists the default commands, for the tests.
func sortedDefaults() []string {
	out := slices.Clone(defaultSkipShell)
	slices.Sort(out)
	return slices.Compact(out)
}
