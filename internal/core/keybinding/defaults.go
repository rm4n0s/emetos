package keybinding

import (
	"strconv"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// Context keys the default keybindings test. Their meaning is VS Code's EditorContextKeys.
const (
	whenTextInputFocus = "textInputFocus"
	whenEditorFocus    = "editorFocus"
	whenEditorText     = "editorTextFocus"
	whenWritable       = "!editorReadonly"
	// whenNotWindowsA11y is the condition of the word moves: VS Code leaves Ctrl+Left and
	// Ctrl+Right to the screen reader's mode on Windows only.
	whenNotWindowsA11y = "(!accessibilityModeEnabled || !isWindows)"
)

// mustWhen reads the parts of a when-clause and joins them with &&. An empty part is
// skipped.
func mustWhen(parts ...string) contextkey.Expr {
	var exprs []contextkey.Expr
	for _, p := range parts {
		if p == "" {
			continue
		}
		e, err := contextkey.Parse(p)
		if err != nil {
			panic(err)
		}
		exprs = append(exprs, e)
	}
	if len(exprs) == 0 {
		return nil
	}
	return contextkey.And(exprs...)
}

// DefaultRegistry makes a registry with the default keymap of Emetos: VS Code's keymap for
// Linux, for the commands Emetos has. VS Code registers its defaults in code, in the
// keybindings of each command's registration; each step that adds a command adds its
// binding here, with the key, secondary keys, when-clause and weight it has there. A when
// that VS Code builds from the command's key expression and its precondition is written
// out in full.
//
// Ported from VS Code: src/vs/editor/browser/coreCommands.ts,
// src/vs/editor/browser/editorExtensions.ts and the contributions under
// src/vs/editor/contrib/ (linesOperations, multicursor, smartSelect, wordOperations,
// cursorUndo, lineSelection)
func DefaultRegistry() *Registry {
	r := NewRegistry()
	registerCursorKeys(r)
	registerEditKeys(r)
	registerClipboardKeys(r)
	registerLineKeys(r)
	registerFoldingKeys(r)
	registerFindKeys(r)
	registerEmmetKeys(r)
	registerSnippetKeys(r)
	registerWordWrapKeys(r)
	registerMultiCursorKeys(r)
	registerMarkerKeys(r)
	registerLayoutKeys(r)
	registerEditorGroupKeys(r)
	registerEditorGridKeys(r)
	registerNotificationKeys(r)
	registerPanelViewKeys(r)
	registerExplorerKeys(r)
	registerSaveKeys(r)
	registerWindowKeys(r)
	registerWorkspaceTabKeys(r)
	registerSettingsKeys(r)
	registerKeybindingsEditorKeys(r)
	registerExtensionsKeys(r)
	registerThemeKeys(r)
	registerQuickInputKeys(r)
	registerStatusBarKeys(r)
	registerDiffEditorKeys(r)
	registerMergeEditorKeys(r)
	return r
}

// registerLayoutKeys registers the keys of the commands that show and focus the parts of the
// workbench: Ctrl+B and Ctrl+J toggle the side bar and the panel, Ctrl+0 focuses the side bar.
//
// Ported from VS Code: src/vs/workbench/browser/actions/layoutActions.ts,
// src/vs/workbench/browser/parts/panel/panelActions.ts,
// src/vs/workbench/browser/parts/sidebar/sidebarActions.ts,
// src/vs/workbench/browser/actions/windowActions.ts, src/vs/workbench/browser/workbench.zenMode.contribution.ts
// and src/vs/workbench/electron-browser/actions/windowActions.ts
func registerLayoutKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.action.toggleSidebarVisibility", Weight: WeightWorkbenchContrib, Primary: "ctrl+b"},
		Rule{Command: "workbench.action.togglePanel", Weight: WeightWorkbenchContrib, Primary: "ctrl+j"},
		Rule{Command: "workbench.action.focusSideBar", Weight: WeightWorkbenchContrib, Primary: "ctrl+0"},

		// full screen, zen mode and the zoom of the window (step 8.5): the keys of Linux
		Rule{Command: "workbench.action.toggleFullScreen", Weight: WeightWorkbenchContrib, Primary: "f11"},
		// Insert toggles overtype mode (Linux; VS Code's macOS key is Alt+Cmd+O)
		Rule{Command: "editor.action.toggleOvertypeInsertMode", Weight: WeightWorkbenchContrib, Primary: "insert"},
		Rule{Command: "workbench.action.toggleZenMode", Weight: WeightWorkbenchContrib, Primary: "ctrl+k z"},
		Rule{Command: "workbench.action.exitZenMode", Weight: WeightEditorContrib - 1000, When: mustWhen("inZenMode"), Primary: "escape escape"},
		Rule{Command: "workbench.action.zoomIn", Weight: WeightWorkbenchContrib, Primary: "ctrl+=", Secondary: []string{"ctrl+shift+=", "ctrl+numpad_add"}},
		Rule{Command: "workbench.action.zoomOut", Weight: WeightWorkbenchContrib, Primary: "ctrl+-", Secondary: []string{"ctrl+numpad_subtract"}},
		Rule{Command: "workbench.action.zoomReset", Weight: WeightWorkbenchContrib, Primary: "ctrl+numpad0"},
	)
}

// registerEditorGroupKeys registers the keys of the commands that close, switch and keep the editors
// of a group, and those of the history of locations. Ctrl+Tab and Ctrl+Shift+Tab open the editor picker
// (workbench.action.quickOpenPreviousRecentlyUsedEditorInGroup and quickOpenLeastRecentlyUsedEditorInGroup);
// pressed again inside the picker they move in it (quickOpenNavigateNext and PreviousInEditorPicker).
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorCommands.ts,
// src/vs/workbench/browser/parts/editor/editorActions.ts and
// src/vs/workbench/browser/parts/editor/editor.contribution.ts
func registerEditorGroupKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.action.closeActiveEditor", Weight: WeightWorkbenchContrib, Primary: "ctrl+w"},
		Rule{Command: "workbench.action.closeEditorsInGroup", Weight: WeightWorkbenchContrib, Primary: "ctrl+k w"},
		Rule{Command: "workbench.action.closeUnmodifiedEditors", Weight: WeightWorkbenchContrib, Primary: "ctrl+k u"},
		Rule{Command: "workbench.action.closeAllEditors", Weight: WeightWorkbenchContrib, Primary: "ctrl+k ctrl+w"},
		Rule{Command: "workbench.action.keepEditor", Weight: WeightWorkbenchContrib, Primary: "ctrl+k enter"},
		Rule{Command: "workbench.action.pinEditor", Weight: WeightWorkbenchContrib, When: mustWhen("!activeEditorIsPinned"), Primary: "ctrl+k shift+enter"},
		Rule{Command: "workbench.action.unpinEditor", Weight: WeightWorkbenchContrib, When: mustWhen("activeEditorIsPinned"), Primary: "ctrl+k shift+enter"},
		Rule{Command: "workbench.action.nextEditor", Weight: WeightWorkbenchContrib, Primary: "ctrl+pagedown"},
		Rule{Command: "workbench.action.previousEditor", Weight: WeightWorkbenchContrib, Primary: "ctrl+pageup"},
		Rule{Command: "workbench.action.nextEditorInGroup", Weight: WeightWorkbenchContrib, Primary: "ctrl+k ctrl+pagedown"},
		Rule{Command: "workbench.action.previousEditorInGroup", Weight: WeightWorkbenchContrib, Primary: "ctrl+k ctrl+pageup"},
		Rule{Command: "workbench.action.quickOpenPreviousRecentlyUsedEditorInGroup", Weight: WeightWorkbenchContrib,
			When: mustWhen("!activeEditorGroupEmpty"), Primary: "ctrl+tab"},
		Rule{Command: "workbench.action.quickOpenLeastRecentlyUsedEditorInGroup", Weight: WeightWorkbenchContrib,
			When: mustWhen("!activeEditorGroupEmpty"), Primary: "ctrl+shift+tab"},
		Rule{Command: "workbench.action.quickOpenNavigateNextInEditorPicker", Weight: WeightWorkbenchContrib + 50,
			When: mustWhen("inQuickOpen", "inEditorsPicker"), Primary: "ctrl+tab"},
		Rule{Command: "workbench.action.quickOpenNavigatePreviousInEditorPicker", Weight: WeightWorkbenchContrib + 50,
			When: mustWhen("inQuickOpen", "inEditorsPicker"), Primary: "ctrl+shift+tab"},
		Rule{Command: "workbench.action.showAllEditors", Weight: WeightWorkbenchContrib, Primary: "ctrl+k ctrl+p"},

		// the history of locations and the closed editors
		Rule{Command: "workbench.action.navigateBack", Weight: WeightWorkbenchContrib, When: mustWhen("canNavigateBack"), Primary: "ctrl+alt+-"},
		Rule{Command: "workbench.action.navigateForward", Weight: WeightWorkbenchContrib, When: mustWhen("canNavigateForward"), Primary: "ctrl+shift+-"},
		Rule{Command: "workbench.action.navigateToLastEditLocation", Weight: WeightWorkbenchContrib, Primary: "ctrl+k ctrl+q"},
		Rule{Command: "workbench.action.reopenClosedEditor", Weight: WeightWorkbenchContrib, Primary: "ctrl+shift+t"},

		// the paths of the file: Ctrl+Alt+C outside the editor, the chord with Ctrl+K in it (VS Code's
		// Linux keys; Ctrl+K P copies the path of the active file anywhere)
		Rule{Command: "copyFilePath", Weight: WeightWorkbenchContrib, When: mustWhen("!" + whenEditorFocus), Primary: "ctrl+alt+c"},
		Rule{Command: "copyFilePath", Weight: WeightWorkbenchContrib, When: mustWhen(whenEditorFocus), Primary: "ctrl+k ctrl+alt+c"},
		Rule{Command: "copyRelativeFilePath", Weight: WeightWorkbenchContrib, When: mustWhen("!" + whenEditorFocus), Primary: "ctrl+shift+alt+c"},
		Rule{Command: "copyRelativeFilePath", Weight: WeightWorkbenchContrib, When: mustWhen(whenEditorFocus), Primary: "ctrl+k ctrl+shift+alt+c"},
		Rule{Command: "workbench.action.files.copyPathOfActiveFile", Weight: WeightWorkbenchContrib, Primary: "ctrl+k p"},
	)
}

// registerCursorKeys registers the keys that move and select.
func registerCursorKeys(r *Registry) {
	input := func(command, primary string, secondary ...string) Rule {
		return Rule{Command: command, Weight: WeightEditorCore, When: mustWhen(whenTextInputFocus), Primary: primary, Secondary: secondary}
	}
	r.MustRegister(
		input("cursorLeft", "left"),
		input("cursorLeftSelect", "shift+left"),
		input("cursorRight", "right"),
		input("cursorRightSelect", "shift+right"),
		input("cursorUp", "up"),
		input("cursorUpSelect", "shift+up"),
		input("cursorPageUp", "pageup"),
		input("cursorPageUpSelect", "shift+pageup"),
		input("cursorDown", "down"),
		input("cursorDownSelect", "shift+down"),
		input("cursorPageDown", "pagedown"),
		input("cursorPageDownSelect", "shift+pagedown"),
		input("cursorHome", "home"),
		input("cursorHomeSelect", "shift+home"),
		input("cursorEnd", "end"),
		input("cursorEndSelect", "shift+end"),
		input("cursorTop", "ctrl+home"),
		input("cursorTopSelect", "ctrl+shift+home"),
		input("cursorBottom", "ctrl+end"),
		input("cursorBottomSelect", "ctrl+shift+end"),
		input("expandLineSelection", "ctrl+l"),
		Rule{Command: "cancelSelection", When: mustWhen(whenTextInputFocus, "editorHasSelection"), Primary: "escape", Secondary: []string{"shift+escape"}},
		// The weight one above the other core commands lets it win over cancelSelection.
		Rule{Command: "removeSecondaryCursors", Weight: WeightEditorCore + 1, When: mustWhen(whenTextInputFocus, "editorHasMultipleSelections"), Primary: "escape", Secondary: []string{"shift+escape"}},
	)
	// With editor.columnSelection on, a plain Shift+arrow selects a column.
	column := func(command, primary string) Rule {
		return Rule{Command: command, Weight: WeightEditorCore + 1, When: mustWhen(whenTextInputFocus, "editorColumnSelection"), Primary: primary}
	}
	r.MustRegister(
		column("cursorColumnSelectLeft", "shift+left"),
		column("cursorColumnSelectRight", "shift+right"),
		column("cursorColumnSelectUp", "shift+up"),
		column("cursorColumnSelectPageUp", "shift+pageup"),
		column("cursorColumnSelectDown", "shift+down"),
		column("cursorColumnSelectPageDown", "shift+pagedown"),
	)
	// Word moves are contributions, so they weigh more than the core moves.
	word := func(command, primary string) Rule {
		return Rule{Command: command, Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenNotWindowsA11y), Primary: primary}
	}
	r.MustRegister(
		word("cursorWordLeft", "ctrl+left"),
		word("cursorWordLeftSelect", "ctrl+shift+left"),
		word("cursorWordEndRight", "ctrl+right"),
		word("cursorWordEndRightSelect", "ctrl+shift+right"),
		Rule{Command: "cursorUndo", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus), Primary: "ctrl+u"},
	)
}

// registerEditKeys registers the keys that type, delete and undo.
func registerEditKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "undo", Weight: WeightEditorCore, Primary: "ctrl+z"},
		Rule{Command: "redo", Weight: WeightEditorCore, Primary: "ctrl+y", Secondary: []string{"ctrl+shift+z"}},
		Rule{Command: "editor.action.selectAll", Weight: WeightEditorCore, Primary: "ctrl+a"},
		Rule{Command: "outdent", Weight: WeightEditorCore, When: mustWhen(whenEditorText, "!editorTabMovesFocus", whenWritable), Primary: "shift+tab"},
		Rule{Command: "tab", Weight: WeightEditorCore, When: mustWhen(whenEditorText, "!editorTabMovesFocus", whenWritable), Primary: "tab"},
		Rule{Command: "deleteLeft", Weight: WeightEditorCore, When: mustWhen(whenTextInputFocus, whenWritable), Primary: "backspace", Secondary: []string{"shift+backspace"}},
		Rule{Command: "deleteRight", Weight: WeightEditorCore, When: mustWhen(whenTextInputFocus, whenWritable), Primary: "delete"},
		Rule{Command: "deleteWordLeft", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenWritable), Primary: "ctrl+backspace"},
		Rule{Command: "deleteWordRight", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenWritable), Primary: "ctrl+delete"},
	)
}

// registerLineKeys registers the keys of the commands on lines.
func registerLineKeys(r *Registry) {
	text := func(command, primary string) Rule {
		return Rule{Command: command, Weight: WeightEditorContrib, When: mustWhen(whenEditorText, whenWritable), Primary: primary}
	}
	r.MustRegister(
		text("editor.action.copyLinesUpAction", "ctrl+alt+shift+up"),
		text("editor.action.copyLinesDownAction", "ctrl+alt+shift+down"),
		text("editor.action.moveLinesUpAction", "alt+up"),
		text("editor.action.moveLinesDownAction", "alt+down"),
		text("editor.action.trimTrailingWhitespace", "ctrl+k ctrl+x"),
		text("editor.action.indentLines", "ctrl+]"),
		text("editor.action.outdentLines", "ctrl+["),
		text("editor.action.insertLineBefore", "ctrl+shift+enter"),
		text("editor.action.insertLineAfter", "ctrl+enter"),
		text("editor.action.commentLine", "ctrl+/"),
		text("editor.action.addCommentLine", "ctrl+k ctrl+c"),
		text("editor.action.removeCommentLine", "ctrl+k ctrl+u"),
		text("editor.action.blockComment", "ctrl+shift+a"),
		Rule{Command: "editor.action.deleteLines", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenWritable), Primary: "ctrl+shift+k"},
	)
}

// registerMultiCursorKeys registers the keys that add cursors and expand the selection.
func registerMultiCursorKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "editor.action.insertCursorAbove", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "shift+alt+up", Secondary: []string{"ctrl+shift+up"}},
		Rule{Command: "editor.action.insertCursorBelow", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "shift+alt+down", Secondary: []string{"ctrl+shift+down"}},
		Rule{Command: "editor.action.insertCursorAtEndOfEachLineSelected", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "shift+alt+i"},
		Rule{Command: "editor.action.addSelectionToNextFindMatch", Weight: WeightEditorContrib, When: mustWhen(whenEditorFocus), Primary: "ctrl+d"},
		Rule{Command: "editor.action.moveSelectionToNextFindMatch", Weight: WeightEditorContrib, When: mustWhen(whenEditorFocus), Primary: "ctrl+k ctrl+d"},
		Rule{Command: "editor.action.selectHighlights", Weight: WeightEditorContrib, When: mustWhen(whenEditorFocus), Primary: "ctrl+shift+l"},
		Rule{Command: "editor.action.smartSelect.expand", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "shift+alt+right"},
		Rule{Command: "editor.action.smartSelect.shrink", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "shift+alt+left"},
		Rule{Command: "editor.action.jumpToBracket", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "ctrl+shift+\\"},
		Rule{Command: "editor.action.removeBrackets", Weight: WeightEditorContrib, When: mustWhen(whenEditorText), Primary: "ctrl+alt+backspace"},
	)
}

// registerClipboardKeys registers the keys of the clipboard commands (Linux).
//
// Ported from VS Code: src/vs/editor/contrib/clipboard/browser/clipboard.ts
func registerClipboardKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "editor.action.clipboardCutAction", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus), Primary: "ctrl+x"},
		Rule{Command: "editor.action.clipboardCopyAction", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus), Primary: "ctrl+c"},
		Rule{Command: "editor.action.clipboardPasteAction", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus), Primary: "ctrl+v", Secondary: []string{"shift+insert"}},
	)
}

// registerFoldingKeys registers the keys of the folding commands.
//
// Ported from VS Code: src/vs/editor/contrib/folding/browser/folding.ts
func registerFoldingKeys(r *Registry) {
	text := func(command, primary string) Rule {
		return Rule{Command: command, Weight: WeightEditorContrib, When: mustWhen(whenEditorText, "foldingEnabled"), Primary: primary}
	}
	rules := []Rule{
		text("editor.fold", "ctrl+shift+["),
		text("editor.unfold", "ctrl+shift+]"),
		text("editor.toggleFold", "ctrl+k ctrl+l"),
		text("editor.foldRecursively", "ctrl+k ctrl+["),
		text("editor.unfoldRecursively", "ctrl+k ctrl+]"),
		text("editor.toggleFoldRecursively", "ctrl+k ctrl+shift+l"),
		text("editor.foldAll", "ctrl+k ctrl+0"),
		text("editor.unfoldAll", "ctrl+k ctrl+j"),
		text("editor.foldAllBlockComments", "ctrl+k ctrl+/"),
		text("editor.foldAllMarkerRegions", "ctrl+k ctrl+8"),
		text("editor.unfoldAllMarkerRegions", "ctrl+k ctrl+9"),
		text("editor.foldAllExcept", "ctrl+k ctrl+-"),
		text("editor.unfoldAllExcept", "ctrl+k ctrl+="),
		text("editor.createFoldingRangeFromSelection", "ctrl+k ctrl+,"),
		text("editor.removeManualFoldingRanges", "ctrl+k ctrl+."),
	}
	for level := 1; level <= 7; level++ {
		digit := strconv.Itoa(level)
		rules = append(rules, text("editor.foldLevel"+digit, "ctrl+k ctrl+"+digit))
	}
	r.MustRegister(rules...)
}

// registerFindKeys registers the keys of the find widget, VS Code's on Linux. The keys of the
// widget's own commands have the weight of an editor contribution plus 5, so that they win
// over the editor's keys while the widget is open.
//
// Ported from VS Code: src/vs/editor/contrib/find/browser/findController.ts and findModel.ts
func registerFindKeys(r *Registry) {
	const widgetWeight = WeightEditorContrib + 5
	focus := func(command, primary string, extra ...string) Rule {
		return Rule{Command: command, Weight: WeightEditorContrib, When: mustWhen(append([]string{whenEditorFocus}, extra...)...), Primary: primary}
	}
	widget := func(command, primary string, extra ...string) Rule {
		return Rule{Command: command, Weight: widgetWeight, When: mustWhen(append([]string{whenEditorFocus}, extra...)...), Primary: primary}
	}
	r.MustRegister(
		Rule{Command: "actions.find", Weight: WeightEditorContrib, Primary: "ctrl+f"},
		Rule{Command: "editor.action.startFindReplaceAction", Weight: WeightEditorContrib, Primary: "ctrl+h"},
		focus("editor.action.nextMatchFindAction", "f3"),
		focus("editor.action.nextMatchFindAction", "enter", "findInputFocussed"),
		focus("editor.action.previousMatchFindAction", "shift+f3"),
		focus("editor.action.previousMatchFindAction", "shift+enter", "findInputFocussed"),
		focus("editor.action.nextSelectionMatchFindAction", "ctrl+f3"),
		focus("editor.action.previousSelectionMatchFindAction", "ctrl+shift+f3"),
		Rule{Command: "closeFindWidget", Weight: widgetWeight, When: mustWhen(whenEditorFocus, "findWidgetVisible"), Primary: "escape", Secondary: []string{"shift+escape"}},
		widget("toggleFindCaseSensitive", "alt+c"),
		widget("toggleFindWholeWord", "alt+w"),
		widget("toggleFindRegex", "alt+r"),
		widget("toggleFindInSelection", "alt+l"),
		widget("togglePreserveCase", "alt+p"),
		widget("editor.action.replaceOne", "ctrl+shift+1", "findWidgetVisible"),
		widget("editor.action.replaceOne", "enter", "findWidgetVisible", "replaceInputFocussed"),
		widget("editor.action.replaceAll", "ctrl+alt+enter", "findWidgetVisible"),
		widget("editor.action.replaceAll", "ctrl+enter", "findWidgetVisible", "replaceInputFocussed"),
		widget("editor.action.selectAllMatches", "alt+enter", "findWidgetVisible"),
	)
}

// registerWordWrapKeys registers the key of the command that toggles word wrap: Alt+Z.
//
// Ported from VS Code: src/vs/workbench/contrib/codeEditor/browser/toggleWordWrap.ts
func registerWordWrapKeys(r *Registry) {
	r.MustRegister(Rule{Command: "editor.action.toggleWordWrap", Weight: WeightEditorContrib, When: mustWhen(whenEditorFocus), Primary: "alt+z"})
}

// registerSnippetKeys registers the keys of snippets: Tab and Shift+Tab move between the tab stops
// (above the other uses of Tab), Escape leaves the snippet, and Tab inserts the snippet whose
// prefix is before the cursor (editor.tabCompletion "onlySnippets"). VS Code builds the when of
// a key from the command's precondition; it is written out here.
//
// Ported from VS Code: src/vs/editor/contrib/snippet/browser/snippetController2.ts and
// src/vs/workbench/contrib/snippets/browser/tabCompletion.ts
func registerSnippetKeys(r *Registry) {
	const weight = WeightEditorContrib + 30
	r.MustRegister(
		Rule{Command: "jumpToNextSnippetPlaceholder", Weight: weight, When: mustWhen(whenTextInputFocus, "inSnippetMode", "hasNextTabstop"), Primary: "tab"},
		Rule{Command: "jumpToPrevSnippetPlaceholder", Weight: weight, When: mustWhen(whenTextInputFocus, "inSnippetMode", "hasPrevTabstop"), Primary: "shift+tab"},
		Rule{Command: "leaveSnippet", Weight: weight, When: mustWhen(whenTextInputFocus, "inSnippetMode"), Primary: "escape", Secondary: []string{"shift+escape"}},
		Rule{Command: "insertSnippet", Weight: WeightEditorContrib, When: mustWhen(whenEditorText, "!editorTabMovesFocus", "!inSnippetMode", "hasSnippetCompletions"), Primary: "tab"},
	)
}

// registerEmmetKeys registers Tab for Emmet: with emmet.triggerExpansionOnTab it expands the
// abbreviation before the cursor, unless text is selected (then Tab indents), a snippet is being
// filled in, or the Tab key moves the focus. It is registered before the keys of snippets, which
// have the same weight and so win over it when a snippet's prefix is before the cursor.
//
// Ported from VS Code: extensions/emmet/package.json (keybindings)
func registerEmmetKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "editor.emmet.action.expandAbbreviation", Weight: WeightEditorContrib,
			When: mustWhen(whenEditorText, whenWritable, "!editorTabMovesFocus", "!editorHasSelection", "!inSnippetMode", "config.emmet.triggerExpansionOnTab"), Primary: "tab"},
	)
}

// registerEditorGridKeys registers the keys of the commands of the grid of editor groups. Ctrl+W
// closes an empty group when there are several (VS Code's workbench.action.closeGroup), and the
// editor otherwise; the rule of the group comes last, which wins when both apply. The chord
// Ctrl+K Ctrl+M carries the precondition of its command as VS Code does. Ctrl+K Ctrl+\ is the orthogonal split only: VS Code's source gives it to the four directional
// split actions as well, which cannot all have it.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorActions.ts,
// src/vs/workbench/browser/parts/editor/editorCommands.ts
func registerEditorGridKeys(r *Registry) {
	rules := []Rule{
		{Command: "workbench.action.splitEditor", Primary: "ctrl+\\"},
		{Command: "workbench.action.splitEditorOrthogonal", Primary: "ctrl+k ctrl+\\"},
		{Command: "workbench.action.focusFirstEditorGroup", Primary: "ctrl+1"},
		{Command: "workbench.action.focusSecondEditorGroup", Primary: "ctrl+2"},
		{Command: "workbench.action.focusThirdEditorGroup", Primary: "ctrl+3"},
		{Command: "workbench.action.focusFourthEditorGroup", Primary: "ctrl+4"},
		{Command: "workbench.action.focusFifthEditorGroup", Primary: "ctrl+5"},
		{Command: "workbench.action.focusSixthEditorGroup", Primary: "ctrl+6"},
		{Command: "workbench.action.focusSeventhEditorGroup", Primary: "ctrl+7"},
		{Command: "workbench.action.focusEighthEditorGroup", Primary: "ctrl+8"},
		{Command: "workbench.action.focusLeftGroup", Primary: "ctrl+k ctrl+left"},
		{Command: "workbench.action.focusRightGroup", Primary: "ctrl+k ctrl+right"},
		{Command: "workbench.action.focusAboveGroup", Primary: "ctrl+k ctrl+up"},
		{Command: "workbench.action.focusBelowGroup", Primary: "ctrl+k ctrl+down"},
		{Command: "workbench.action.moveActiveEditorGroupLeft", Primary: "ctrl+k left"},
		{Command: "workbench.action.moveActiveEditorGroupRight", Primary: "ctrl+k right"},
		{Command: "workbench.action.moveActiveEditorGroupUp", Primary: "ctrl+k up"},
		{Command: "workbench.action.moveActiveEditorGroupDown", Primary: "ctrl+k down"},
		{Command: "workbench.action.moveEditorToPreviousGroup", Primary: "ctrl+alt+left"},
		{Command: "workbench.action.moveEditorToNextGroup", Primary: "ctrl+alt+right"},
		{Command: "workbench.action.moveEditorToFirstGroup", Primary: "shift+alt+1"},
		{Command: "workbench.action.moveEditorToLastGroup", Primary: "shift+alt+9"},
		{Command: "workbench.action.toggleMaximizeEditorGroup", When: mustWhen("editorPartMultipleEditorGroups || editorPartMaximizedEditorGroup"), Primary: "ctrl+k ctrl+m"},
		{Command: "workbench.action.closeAllGroups", Primary: "ctrl+k ctrl+shift+w"},
		{Command: "workbench.action.closeGroup", When: mustWhen("activeEditorGroupEmpty", "multipleEditorGroups"), Primary: "ctrl+w"},
	}
	for _, rule := range rules {
		rule.Weight = WeightWorkbenchContrib
		r.MustRegister(rule)
	}
}

// registerNotificationKeys registers the keys of the notification commands: Ctrl+K Ctrl+Shift+N shows the
// notification center, Escape hides it while it is visible, and hides the toasts while they are
// visible (a lower weight, so that an editor widget that takes Escape wins, and a higher one while a
// notification has the focus).
//
// Ported from VS Code: src/vs/workbench/browser/parts/notifications/notificationsCommands.ts
func registerNotificationKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "notifications.showList", Weight: WeightWorkbenchContrib, Primary: "ctrl+k ctrl+shift+n"},
		Rule{Command: "notifications.hideList", Weight: WeightWorkbenchContrib + 50, When: mustWhen("notificationCenterVisible"), Primary: "escape"},
		Rule{Command: "notifications.hideToasts", Weight: WeightWorkbenchContrib - 50, When: mustWhen("notificationToastsVisible"), Primary: "escape"},
		Rule{Command: "notifications.hideToasts", Weight: WeightWorkbenchContrib + 100, When: mustWhen("notificationToastsVisible", "notificationFocus"), Primary: "escape"},
	)
}

// registerPanelViewKeys registers the keys of the Problems and Output panels. VS Code binds the Output
// view to Ctrl+Shift+U, and to Ctrl+K Ctrl+H on Linux, where Ctrl+Shift+U is taken by the desktop.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markers.contribution.ts,
// src/vs/workbench/contrib/output/browser/output.contribution.ts
func registerPanelViewKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.actions.view.problems", Weight: WeightWorkbenchContrib, Primary: "ctrl+shift+m"},
		Rule{Command: "workbench.action.output.toggleOutput", Weight: WeightWorkbenchContrib, Primary: "ctrl+k ctrl+h"},
		Rule{Command: "problems.action.focusProblemsFromFilter", Weight: WeightWorkbenchContrib, When: mustWhen("problemsFilterFocus"), Primary: "ctrl+down"},
		Rule{Command: "problems.action.focusFilter", Weight: WeightWorkbenchContrib, When: mustWhen("focusedView == 'workbench.panel.markers.view'"), Primary: "ctrl+f"},
		Rule{Command: "problems.action.clearFilterText", Weight: WeightWorkbenchContrib, When: mustWhen("problemsFilterFocus"), Primary: "escape"},
	)
}

// registerExplorerKeys registers the keys of the Explorer: Ctrl+Shift+E shows it, Ctrl+K E focuses the Open
// Editors, and the chords that open and close the folder of the window (Linux).
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/explorerViewlet.ts and
// src/vs/workbench/browser/actions/workspaceActions.ts
func registerExplorerKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.view.explorer", Weight: WeightWorkbenchContrib, Primary: "ctrl+shift+e"},
		Rule{Command: "workbench.files.action.focusOpenEditorsView", Weight: WeightWorkbenchContrib, Primary: "ctrl+k e"},
		Rule{Command: "workbench.action.files.openFolder", Weight: WeightWorkbenchContrib, Primary: "ctrl+k ctrl+o"},
		Rule{Command: "workbench.action.closeFolder", Weight: WeightWorkbenchContrib, When: mustWhen("workbenchState != 'empty'"), Primary: "ctrl+k f"},
		// Open Recent (Linux: Ctrl+R)
		Rule{Command: "workbench.action.openRecent", Weight: WeightWorkbenchContrib, Primary: "ctrl+r"},
	)
	registerExplorerFileKeys(r)
}

// registerSaveKeys registers the keys of saving: Ctrl+S saves, Ctrl+Shift+S saves as, Ctrl+K S saves without
// formatting and Ctrl+N makes an untitled file (Linux). Save All has no key on Linux: VS Code gives it Ctrl+K S
// on Windows only.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileCommands.ts
func registerSaveKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.action.files.save", Weight: WeightWorkbenchContrib, Primary: "ctrl+s"},
		Rule{Command: "workbench.action.files.saveWithoutFormatting", Weight: WeightWorkbenchContrib, Primary: "ctrl+k s"},
		Rule{Command: "workbench.action.files.saveAs", Weight: WeightWorkbenchContrib, Primary: "ctrl+shift+s"},
		Rule{Command: "workbench.action.files.newUntitledFile", Weight: WeightWorkbenchContrib, Primary: "ctrl+n"},
	)
}

// whenFilesExplorerFocus is VS Code's FilesExplorerFocusCondition (the folder view is visible and has the
// focus, and no input has it), and whenExplorerFocus its ExplorerFocusCondition.
const (
	whenFilesExplorerFocus = "foldersViewVisible && filesExplorerFocus && !inputFocus"
	whenExplorerFocus      = "foldersViewVisible && explorerViewletFocus && !inputFocus"
)

// registerExplorerFileKeys registers the keys of the file operations of the Explorer (Linux values): F2
// renames, Delete moves to the trash (deletes for good where the trash cannot be used), Shift+Delete deletes
// for good, Ctrl+X, Ctrl+C and Ctrl+V cut, copy and paste, Escape forgets a cut, Ctrl+Enter opens to the
// side and Ctrl+Alt+R reveals the file in the file manager. The weight is VS Code's explorerCommandsWeightBonus
// (10) above the workbench's, to win over the commands of the lists.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.contribution.ts,
// src/vs/workbench/contrib/files/browser/fileCommands.ts and
// src/vs/workbench/contrib/files/electron-browser/fileActions.contribution.ts
func registerExplorerFileKeys(r *Registry) {
	const weight = WeightWorkbenchContrib + 10
	r.MustRegister(
		Rule{Command: "renameFile", Weight: weight, When: mustWhen(whenFilesExplorerFocus, "!explorerResourceIsRoot", "!explorerResourceReadonly"), Primary: "f2"},
		Rule{Command: "moveFileToTrash", Weight: weight, When: mustWhen(whenFilesExplorerFocus, "explorerResourceMoveableToTrash"), Primary: "delete"},
		Rule{Command: "deleteFile", Weight: weight, When: mustWhen(whenFilesExplorerFocus), Primary: "shift+delete"},
		Rule{Command: "deleteFile", Weight: weight, When: mustWhen(whenFilesExplorerFocus, "!explorerResourceMoveableToTrash"), Primary: "delete"},
		Rule{Command: "filesExplorer.cut", Weight: weight, When: mustWhen(whenFilesExplorerFocus, "!explorerResourceIsRoot", "!explorerResourceReadonly"), Primary: "ctrl+x"},
		Rule{Command: "filesExplorer.copy", Weight: weight, When: mustWhen(whenFilesExplorerFocus, "!explorerResourceIsRoot"), Primary: "ctrl+c"},
		Rule{Command: "filesExplorer.paste", Weight: weight, When: mustWhen(whenFilesExplorerFocus, "!explorerResourceReadonly"), Primary: "ctrl+v"},
		Rule{Command: "filesExplorer.cancelCut", Weight: weight, When: mustWhen(whenFilesExplorerFocus, "explorerResourceCut"), Primary: "escape"},
		Rule{Command: "explorer.openToSide", Weight: WeightWorkbenchContrib, When: mustWhen(whenExplorerFocus), Primary: "ctrl+enter"},
		Rule{Command: "revealFileInOS", Weight: WeightWorkbenchContrib, When: mustWhen("!" + whenEditorFocus), Primary: "ctrl+alt+r"},
	)
}

// registerQuickInputKeys registers the keys that open the quick input: Ctrl+Shift+P and F1 for the
// command palette, Ctrl+P and Ctrl+E for quick open, Ctrl+G for go to line (VS Code's Linux keys;
// Quick Open View has none on Linux), and Escape while the quick input has the focus.
//
// Ported from VS Code: src/vs/workbench/contrib/quickaccess/browser/commandsQuickAccess.ts,
// src/vs/workbench/browser/actions/quickAccessActions.ts and
// src/vs/workbench/contrib/codeEditor/browser/quickaccess/gotoLineQuickAccess.ts
func registerQuickInputKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.action.showCommands", Weight: WeightWorkbenchContrib, Primary: "ctrl+shift+p", Secondary: []string{"f1"}},
		Rule{Command: "workbench.action.quickOpen", Weight: WeightWorkbenchContrib, Primary: "ctrl+p", Secondary: []string{"ctrl+e"}},
		Rule{Command: "workbench.action.gotoLine", Weight: WeightWorkbenchContrib, Primary: "ctrl+g"},
		Rule{Command: "workbench.action.closeQuickOpen", Weight: WeightWorkbenchContrib, When: mustWhen("inQuickOpen"), Primary: "escape", Secondary: []string{"shift+escape"}},
	)
}

// registerStatusBarKeys registers the key of the language mode picker of the status bar: Ctrl+K M.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorStatus.ts
func registerStatusBarKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.action.editor.changeLanguageMode", Weight: WeightWorkbenchContrib, Primary: "ctrl+k m"},
	)
}

// registerWindowKeys registers the keys that close the window: Alt+F4 or Ctrl+Shift+W close it and Ctrl+Q quits
// (Linux).
//
// Ported from VS Code: src/vs/workbench/electron-browser/actions/windowActions.ts (CloseWindowAction) and
// src/vs/workbench/electron-browser/desktop.contribution.ts (workbench.action.quit)
func registerWindowKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.action.closeWindow", Weight: WeightWorkbenchContrib, Primary: "alt+f4", Secondary: []string{"ctrl+shift+w"}},
		Rule{Command: "workbench.action.quit", Weight: WeightWorkbenchContrib, Primary: "ctrl+q"},
		Rule{Command: "workbench.action.newWindow", Weight: WeightWorkbenchContrib, Primary: "ctrl+shift+n"},
	)
}

// registerWorkspaceTabKeys registers the keys of the workspace tabs (step 9b.5, SPEC §10.14): Ctrl+Alt+PageDown and
// Ctrl+Alt+PageUp go to the next and the previous tab, Ctrl+Alt+1 ... Ctrl+Alt+9 to the tab at that place (9 is the
// last one). They are Emetos's own: VS Code has no workspace tabs, and Alt+1 ... Alt+9 stay its openEditorAtIndex.
func registerWorkspaceTabKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "emetos.workspaceTab.next", Weight: WeightWorkbenchContrib, Primary: "ctrl+alt+pagedown"},
		Rule{Command: "emetos.workspaceTab.previous", Weight: WeightWorkbenchContrib, Primary: "ctrl+alt+pageup"},
	)
	for i := 1; i <= 9; i++ {
		r.MustRegister(Rule{Command: "emetos.workspaceTab.open" + strconv.Itoa(i), Weight: WeightWorkbenchContrib, Primary: "ctrl+alt+" + strconv.Itoa(i)})
	}
}

// registerSettingsKeys registers the key of the settings editor: Ctrl+, opens it.
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/preferences.contribution.ts
func registerSettingsKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.action.openSettings", Weight: WeightWorkbenchContrib, Primary: "ctrl+,"},
	)
}

// registerKeybindingsEditorKeys registers the keys of the keybindings editor (step 8.3): Ctrl+K Ctrl+S opens it, and
// the keys inside it work on its search box or on the row that has the focus. The editor sets the context keys
// inKeybindings, inKeybindingsSearch, keybindingsSearchHasValue, keybindingFocus and whenFocus while it has the
// focus. Reset, Show Same Keybindings, Copy Command ID and Copy Command Title have no key.
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/preferences.contribution.ts
func registerKeybindingsEditorKeys(r *Registry) {
	const (
		inEditor    = "inKeybindings"
		onRow       = "keybindingFocus"
		inSearch    = "inKeybindingsSearch"
		searchValue = "keybindingsSearchHasValue"
	)
	r.MustRegister(
		Rule{Command: "workbench.action.openGlobalKeybindings", Weight: WeightWorkbenchContrib, Primary: "ctrl+k ctrl+s"},

		Rule{Command: "keybindings.editor.searchKeybindings", Weight: WeightWorkbenchContrib, When: mustWhen(inEditor), Primary: "ctrl+f"},
		Rule{Command: "keybindings.editor.clearSearchResults", Weight: WeightWorkbenchContrib, When: mustWhen(inEditor, inSearch, searchValue), Primary: "escape"},
		Rule{Command: "keybindings.editor.recordSearchKeys", Weight: WeightWorkbenchContrib, When: mustWhen(inEditor, inSearch), Primary: "alt+k"},
		Rule{Command: "keybindings.editor.focusKeybindings", Weight: WeightWorkbenchContrib, When: mustWhen(inEditor, inSearch), Primary: "ctrl+down"},
		Rule{Command: "keybindings.editor.defineKeybinding", Weight: WeightWorkbenchContrib, When: mustWhen(inEditor, onRow, "!whenFocus"), Primary: "enter"},
		Rule{Command: "keybindings.editor.addKeybinding", Weight: WeightWorkbenchContrib, When: mustWhen(inEditor, onRow), Primary: "ctrl+k ctrl+a"},
		Rule{Command: "keybindings.editor.defineWhenExpression", Weight: WeightWorkbenchContrib, When: mustWhen(inEditor, onRow), Primary: "ctrl+k ctrl+e"},
		Rule{Command: "keybindings.editor.removeKeybinding", Weight: WeightWorkbenchContrib, When: mustWhen(inEditor, onRow, "!inputFocus"), Primary: "delete"},
		Rule{Command: "keybindings.editor.copyKeybindingEntry", Weight: WeightWorkbenchContrib, When: mustWhen(inEditor, onRow, "!whenFocus"), Primary: "ctrl+c"},
	)
}

// registerThemeKeys registers the key of the color theme picker (step 8.4): Ctrl+K Ctrl+T. The file icon theme picker
// has no key.
//
// Ported from VS Code: src/vs/workbench/contrib/themes/browser/themes.contribution.ts
func registerThemeKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.action.selectTheme", Weight: WeightWorkbenchContrib, Primary: "ctrl+k ctrl+t"},
	)
}

// registerDiffEditorKeys registers the keys of the diff editor (step 9.5): Alt+F5 and Shift+Alt+F5 go to the next and
// the previous change while a text compare editor is visible. The commands that focus a side and Switch Side have no
// key. Not built: Ctrl+K Shift+O (Open Active Diff Side) and F7/Shift+F7 of the accessible diff viewer.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/diffEditorCommands.ts
func registerDiffEditorKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.action.compareEditor.nextChange", Weight: WeightWorkbenchContrib, When: mustWhen("textCompareEditorVisible"), Primary: "alt+f5"},
		Rule{Command: "workbench.action.compareEditor.previousChange", Weight: WeightWorkbenchContrib, When: mustWhen("textCompareEditorVisible"), Primary: "shift+alt+f5"},
		// the commands that open a diff (step 9.6)
		Rule{Command: "workbench.files.action.compareWithSaved", Weight: WeightWorkbenchContrib, Primary: "ctrl+k d"},
		Rule{Command: "workbench.files.action.compareWithClipboard", Weight: WeightWorkbenchContrib, Primary: "ctrl+k c"},
	)
}

// registerMergeEditorKeys registers the key of the merge editor (step 9.8): Ctrl+Enter completes the merge while a merge
// editor is the active editor. It has the weight of the editor contributions, as in VS Code, and is registered after the
// line commands, so that it wins over Insert Line Below in the result editor.
//
// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/commands/commands.ts (AcceptMerge)
func registerMergeEditorKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "mergeEditor.acceptMerge", Weight: WeightEditorContrib, When: mustWhen("isMergeEditor"), Primary: "ctrl+enter"},
	)
}

// registerExtensionsKeys binds Ctrl+Shift+X to the page "Extensions" (step 11.1), the key VS Code gives its Extensions view.
//
// Ported from VS Code: src/vs/workbench/contrib/extensions/browser/extensions.contribution.ts
func registerExtensionsKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "workbench.extensions.action.showInstalledExtensions", Weight: WeightWorkbenchContrib, Primary: "ctrl+shift+x"},
	)
}

// registerMarkerKeys registers the keys of the navigation through the problems.
//
// Ported from VS Code: src/vs/editor/contrib/gotoError/browser/gotoError.ts
func registerMarkerKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "editor.action.marker.next", Weight: WeightEditorContrib, When: mustWhen(whenEditorFocus), Primary: "alt+f8"},
		Rule{Command: "editor.action.marker.prev", Weight: WeightEditorContrib, When: mustWhen(whenEditorFocus), Primary: "shift+alt+f8"},
		Rule{Command: "editor.action.marker.nextInFiles", Weight: WeightEditorContrib, When: mustWhen(whenEditorFocus), Primary: "f8"},
		Rule{Command: "editor.action.marker.prevInFiles", Weight: WeightEditorContrib, When: mustWhen(whenEditorFocus), Primary: "shift+f8"},
		Rule{Command: "closeMarkersNavigation", Weight: WeightEditorContrib + 50, When: mustWhen(whenEditorFocus, "markersNavigationVisible"), Primary: "escape", Secondary: []string{"shift+escape"}},
	)
}
