package command

import "github.com/rm4n0s/emetos/internal/core/contextkey"

// registerCodeActionCommands registers the commands of the code actions, of formatting and of rename (step 12.8), and the ones
// the widgets of the editor take their keys by: the list of actions, the rename box and the preview of a refactoring.
//
// Ported from VS Code: src/vs/editor/contrib/codeAction/browser/codeActionCommands.ts, src/vs/platform/actionWidget/browser/actionWidget.ts,
// src/vs/editor/contrib/format/browser/formatActions.ts, src/vs/editor/contrib/rename/browser/rename.ts and
// src/vs/workbench/contrib/bulkEdit/browser/preview/bulkEdit.contribution.ts
func registerCodeActionCommands(r *Registry) {
	hasActions := contextkey.And(writable(), contextkey.Has("editorHasCodeActionsProvider"))
	supports := func(kind string) contextkey.Expr {
		return contextkey.And(writable(), supportedCodeAction(kind))
	}
	menuVisible := contextkey.Has("codeActionMenuVisible")
	renameVisible := contextkey.Has("renameInputVisible")
	r.MustRegister(
		Meta{ID: "editor.action.quickFix", Title: "Quick Fix...", Precondition: hasActions},
		Meta{ID: "editor.action.codeAction", Precondition: hasActions},
		Meta{ID: "editor.action.refactor", Title: "Refactor...", Precondition: hasActions},
		Meta{ID: "editor.action.sourceAction", Title: "Source Action...", Precondition: hasActions},
		Meta{ID: "editor.action.organizeImports", Title: "Organize Imports", Precondition: supports("source.organizeImports")},
		Meta{ID: "editor.action.fixAll", Title: "Fix All", Precondition: supports("source.fixAll")},
		Meta{ID: "editor.action.autoFix", Title: "Auto Fix...", Precondition: supports("quickfix")},

		// the list of actions
		Meta{ID: "hideCodeActionWidget", Title: "Hide action widget", Precondition: menuVisible},
		Meta{ID: "selectPrevCodeAction", Title: "Select previous action", Precondition: menuVisible},
		Meta{ID: "selectNextCodeAction", Title: "Select next action", Precondition: menuVisible},
		Meta{ID: "acceptSelectedCodeAction", Title: "Accept selected action", Precondition: menuVisible},
		Meta{ID: "previewSelectedCodeAction", Title: "Preview selected action", Precondition: menuVisible},

		Meta{ID: "editor.action.formatDocument", Title: "Format Document",
			Precondition: contextkey.And(writable(), contextkey.Has("editorHasDocumentFormattingProvider"))},
		Meta{ID: "editor.action.formatSelection", Title: "Format Selection",
			Precondition: contextkey.And(writable(), contextkey.Has("editorHasDocumentSelectionFormattingProvider"))},
		Meta{ID: "editor.action.format", Title: "Format Code"},

		Meta{ID: "editor.action.rename", Title: "Rename Symbol", Precondition: contextkey.And(writable(), contextkey.Has("editorHasRenameProvider"))},
		Meta{ID: "acceptRenameInput", Precondition: renameVisible},
		Meta{ID: "acceptRenameInputWithPreview", Precondition: contextkey.And(renameVisible, contextkey.Has("config.editor.rename.enablePreview"))},
		Meta{ID: "cancelRenameInput", Precondition: renameVisible},

		// the preview of a refactoring: Emetos shows it over the editor, and focusNext and focusPrevious are its own (VS Code's tree
		// has the keys of every list)
		Meta{ID: "refactorPreview.apply", Title: "Apply Refactoring", Category: "Refactor Preview", Precondition: contextkey.Has("refactorPreview.visible")},
		Meta{ID: "refactorPreview.discard", Title: "Discard Refactoring", Category: "Refactor Preview", Precondition: contextkey.Has("refactorPreview.visible")},
		Meta{ID: "refactorPreview.toggleCheckedState", Title: "Toggle Change", Category: "Refactor Preview", Precondition: contextkey.Has("refactorPreview.visible")},
		Meta{ID: "refactorPreview.focusNext", Title: "Focus Next Change", Category: "Refactor Preview", Precondition: contextkey.Has("refactorPreview.visible")},
		Meta{ID: "refactorPreview.focusPrevious", Title: "Focus Previous Change", Category: "Refactor Preview", Precondition: contextkey.Has("refactorPreview.visible")},
	)
}

// supportedCodeAction is VS Code's contextKeyForSupportedActions: the kinds the providers answer, which the editor joins with
// spaces in the key supportedCodeAction, hold the kind as a word (or one below it).
func supportedCodeAction(kind string) contextkey.Expr {
	e, err := contextkey.Parse(`supportedCodeAction =~ /(\s|^)` + regexEscape(kind) + `\b/`)
	if err != nil {
		// the pattern is made here from the kind of an action of the program itself: a broken one is a broken build
		panic(err)
	}
	return e
}

// regexEscape escapes the dots of a kind for the pattern of a key expression.
func regexEscape(s string) string {
	out := make([]byte, 0, len(s)+2)
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, '\\')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// registerCodeActionMenus adds the rows of the context menu of the editor (Rename Symbol, Format Document, Format Selection,
// Refactor..., Source Action...) and of the Edit menu.
//
// Ported from VS Code: the contextMenuOpts of codeActionCommands.ts, formatActions.ts and rename.ts
func registerCodeActionMenus(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: EditorContext, Command: "editor.action.rename", Group: "1_modification", Order: 1.1,
			When: contextkey.And(writable(), contextkey.Has("editorHasRenameProvider"))},
		MenuItem{Menu: EditorContext, Command: "editor.action.formatDocument", Group: "1_modification", Order: 1.3,
			When: contextkey.And(writable(), contextkey.Has("editorHasDocumentFormattingProvider"))},
		MenuItem{Menu: EditorContext, Command: "editor.action.formatSelection", Group: "1_modification", Order: 1.31, When: contextkey.And(writable(), contextkey.Has("editorHasDocumentSelectionFormattingProvider"), contextkey.Has("editorHasSelection"))},
		MenuItem{Menu: EditorContext, Command: "editor.action.refactor", Group: "1_modification", Order: 2,
			When: contextkey.And(writable(), supportedCodeAction("refactor"))},
		MenuItem{Menu: EditorContext, Command: "editor.action.sourceAction", Group: "1_modification", Order: 2.1,
			When: contextkey.And(writable(), supportedCodeAction("source"))},

		// the commands the keys of the widgets run are not in the palette unless the widget is open
		MenuItem{Menu: CommandPalette, Command: "hideCodeActionWidget", When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: "selectPrevCodeAction", When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: "selectNextCodeAction", When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: "acceptSelectedCodeAction", When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: "previewSelectedCodeAction", When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: "editor.action.format", When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: "refactorPreview.focusNext", When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: "refactorPreview.focusPrevious", When: contextkey.False()},
	)
}
