package keybinding

// registerNavigationKeys registers the keys of go to definition and its kin, of the peek view and of the symbol pickers (Linux).
// A rule's when is written out in full: the key's own condition and the precondition of its command.
//
// Ported from VS Code: src/vs/editor/contrib/gotoSymbol/browser/goToCommands.ts,
// src/vs/editor/contrib/gotoSymbol/browser/peek/referencesController.ts,
// src/vs/workbench/contrib/codeEditor/browser/quickaccess/gotoSymbolQuickAccess.ts and
// src/vs/workbench/contrib/search/browser/symbolsQuickAccess.ts
func registerNavigationKeys(r *Registry) {
	const focus = "editorTextFocus"
	peeking := "referenceSearchVisible"
	r.MustRegister(
		Rule{Command: "editor.action.revealDefinition", Weight: WeightEditorContrib, When: mustWhen(focus, "editorHasDefinitionProvider"), Primary: "f12"},
		Rule{Command: "editor.action.revealDefinitionAside", Weight: WeightEditorContrib, When: mustWhen(focus, "editorHasDefinitionProvider"), Primary: "ctrl+k f12"},
		Rule{Command: "editor.action.peekDefinition", Weight: WeightEditorContrib, When: mustWhen(focus, "editorHasDefinitionProvider"), Primary: "alt+f12", Secondary: []string{"ctrl+shift+f10"}},
		Rule{Command: "editor.action.goToImplementation", Weight: WeightEditorContrib, When: mustWhen(focus, "editorHasImplementationProvider"), Primary: "ctrl+f12"},
		Rule{Command: "editor.action.peekImplementation", Weight: WeightEditorContrib, When: mustWhen(focus, "editorHasImplementationProvider"), Primary: "ctrl+shift+f12"},
		Rule{Command: "editor.action.goToReferences", Weight: WeightEditorContrib, When: mustWhen(focus, "editorHasReferenceProvider"), Primary: "shift+f12"},
		Rule{Command: "editor.action.referenceSearch.trigger", Weight: WeightEditorContrib, When: mustWhen(focus, "editorHasReferenceProvider"), Primary: "shift+alt+f12"},

		// the peek view: Escape closes it, F4 and Shift+F4 go through the results, Enter opens the selected one
		Rule{Command: "closeReferenceSearch", Weight: WeightEditorContrib + 50, When: mustWhen(focus, peeking), Primary: "escape", Secondary: []string{"shift+escape"}},
		Rule{Command: "goToNextReference", Weight: WeightEditorContrib + 50, When: mustWhen(focus, peeking), Primary: "f4"},
		Rule{Command: "goToPreviousReference", Weight: WeightEditorContrib + 50, When: mustWhen(focus, peeking), Primary: "shift+f4"},
		Rule{Command: "selectNextReference", Weight: WeightEditorContrib + 50, When: mustWhen(focus, peeking, "!suggestWidgetVisible"), Primary: "down"},
		Rule{Command: "selectPreviousReference", Weight: WeightEditorContrib + 50, When: mustWhen(focus, peeking, "!suggestWidgetVisible"), Primary: "up"},
		Rule{Command: "openReference", Weight: WeightEditorContrib + 50, When: mustWhen(focus, peeking, "!suggestWidgetVisible"), Primary: "enter"},
		Rule{Command: "openReferenceToSide", Weight: WeightEditorContrib + 50, When: mustWhen(focus, peeking), Primary: "ctrl+enter"},

		Rule{Command: "workbench.action.gotoSymbol", Weight: WeightWorkbenchContrib, Primary: "ctrl+shift+o"},
		Rule{Command: "workbench.action.showAllSymbols", Weight: WeightWorkbenchContrib, Primary: "ctrl+t"},
	)
}
