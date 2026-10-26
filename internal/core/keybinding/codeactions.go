package keybinding

// registerCodeActionKeys registers the keys of the code actions, of formatting and of rename, and of the widgets they open: the
// list of actions, the rename box and the preview of a refactoring (Linux). A rule's when is written out in full: the key's own
// condition and the precondition of its command.
//
// Ported from VS Code: src/vs/editor/contrib/codeAction/browser/codeActionCommands.ts, src/vs/platform/actionWidget/browser/actionWidget.ts,
// src/vs/editor/contrib/format/browser/formatActions.ts, src/vs/editor/contrib/rename/browser/rename.ts and
// src/vs/workbench/contrib/bulkEdit/browser/preview/bulkEdit.contribution.ts
func registerCodeActionKeys(r *Registry) {
	hasActions := "editorHasCodeActionsProvider"
	supports := func(kind string) string { return `supportedCodeAction =~ /(\s|^)` + kind + `\b/` }
	// the keys of the list of actions win over everything the editor binds, as VS Code gives them the weight of the editor's
	// contributions plus 1000
	const widget = WeightEditorContrib + 1000
	menu := "codeActionMenuVisible"
	const renameWeight = WeightEditorContrib + 99
	const previewWeight = WeightEditorContrib + 200
	r.MustRegister(
		Rule{Command: "editor.action.quickFix", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenWritable, hasActions), Primary: "ctrl+."},
		Rule{Command: "editor.action.refactor", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenWritable, hasActions), Primary: "ctrl+shift+r"},
		Rule{Command: "editor.action.organizeImports", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenWritable, supports(`source\.organizeImports`)), Primary: "shift+alt+o"},
		Rule{Command: "editor.action.autoFix", Weight: WeightEditorContrib, When: mustWhen(whenTextInputFocus, whenWritable, supports("quickfix")), Primary: "shift+alt+."},

		Rule{Command: "editor.action.formatDocument", Weight: WeightEditorContrib, When: mustWhen(whenEditorText, whenWritable, "editorHasDocumentFormattingProvider"), Primary: "ctrl+shift+i"},
		Rule{Command: "editor.action.formatSelection", Weight: WeightEditorContrib, When: mustWhen(whenEditorText, whenWritable, "editorHasDocumentSelectionFormattingProvider"), Primary: "ctrl+k ctrl+f"},
		Rule{Command: "editor.action.rename", Weight: WeightEditorContrib, When: mustWhen(whenEditorText, whenWritable, "editorHasRenameProvider"), Primary: "f2"},

		// the list of actions
		Rule{Command: "hideCodeActionWidget", Weight: widget, When: mustWhen(menu), Primary: "escape", Secondary: []string{"shift+escape"}},
		Rule{Command: "selectPrevCodeAction", Weight: widget, When: mustWhen(menu), Primary: "up", Secondary: []string{"pageup"}},
		Rule{Command: "selectNextCodeAction", Weight: widget, When: mustWhen(menu), Primary: "down", Secondary: []string{"pagedown"}},
		Rule{Command: "acceptSelectedCodeAction", Weight: widget, When: mustWhen(menu), Primary: "enter", Secondary: []string{"ctrl+."}},
		Rule{Command: "previewSelectedCodeAction", Weight: widget, When: mustWhen(menu), Primary: "ctrl+enter"},

		// the rename box
		Rule{Command: "acceptRenameInput", Weight: renameWeight, When: mustWhen(whenEditorFocus, "renameInputVisible"), Primary: "enter"},
		Rule{Command: "acceptRenameInputWithPreview", Weight: renameWeight, When: mustWhen(whenEditorFocus, "renameInputVisible", "config.editor.rename.enablePreview"), Primary: "ctrl+enter"},
		Rule{Command: "cancelRenameInput", Weight: renameWeight, When: mustWhen(whenEditorFocus, "renameInputVisible"), Primary: "escape", Secondary: []string{"shift+escape"}},

		// the preview of a refactoring
		Rule{Command: "refactorPreview.apply", Weight: previewWeight, When: mustWhen(whenEditorFocus, "refactorPreview.visible"), Primary: "ctrl+enter"},
		Rule{Command: "refactorPreview.discard", Weight: previewWeight, When: mustWhen(whenEditorFocus, "refactorPreview.visible"), Primary: "escape", Secondary: []string{"shift+escape"}},
		Rule{Command: "refactorPreview.toggleCheckedState", Weight: previewWeight, When: mustWhen(whenEditorFocus, "refactorPreview.visible"), Primary: "space", Secondary: []string{"enter"}},
		Rule{Command: "refactorPreview.focusNext", Weight: previewWeight, When: mustWhen(whenEditorFocus, "refactorPreview.visible"), Primary: "down"},
		Rule{Command: "refactorPreview.focusPrevious", Weight: previewWeight, When: mustWhen(whenEditorFocus, "refactorPreview.visible"), Primary: "up"},
	)
}
