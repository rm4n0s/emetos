package command

import "github.com/rm4n0s/emetos/internal/core/contextkey"

// ViewOutline is the Outline view of the Explorer (step 12.7).
const ViewOutline = "outline"

// registerNavigationCommands registers the commands of go to definition and its kin, of the peek view and of the symbol
// pickers (step 12.7).
//
// Ported from VS Code: src/vs/editor/contrib/gotoSymbol/browser/goToCommands.ts,
// src/vs/editor/contrib/gotoSymbol/browser/peek/referencesController.ts,
// src/vs/editor/contrib/gotoSymbol/browser/link/goToDefinitionAtPosition.ts,
// src/vs/workbench/contrib/codeEditor/browser/quickaccess/gotoSymbolQuickAccess.ts and
// src/vs/workbench/contrib/search/browser/symbolsQuickAccess.ts
func registerNavigationCommands(r *Registry) {
	has := func(feature string) contextkey.Expr { return contextkey.Has("editorHas" + feature + "Provider") }
	peeking := contextkey.Has("referenceSearchVisible")
	r.MustRegister(
		Meta{ID: "editor.action.revealDefinition", Title: "Go to Definition", Precondition: has("Definition")},
		Meta{ID: "editor.action.revealDefinitionAside", Title: "Open Definition to the Side", Precondition: has("Definition")},
		Meta{ID: "editor.action.peekDefinition", Title: "Peek Definition", Precondition: has("Definition")},
		Meta{ID: "editor.action.revealDeclaration", Title: "Go to Declaration", Precondition: has("Declaration")},
		Meta{ID: "editor.action.peekDeclaration", Title: "Peek Declaration", Precondition: has("Declaration")},
		Meta{ID: "editor.action.goToTypeDefinition", Title: "Go to Type Definition", Precondition: has("TypeDefinition")},
		Meta{ID: "editor.action.peekTypeDefinition", Title: "Peek Type Definition", Precondition: has("TypeDefinition")},
		Meta{ID: "editor.action.goToImplementation", Title: "Go to Implementations", Precondition: has("Implementation")},
		Meta{ID: "editor.action.peekImplementation", Title: "Peek Implementations", Precondition: has("Implementation")},
		Meta{ID: "editor.action.goToReferences", Title: "Go to References", Precondition: has("Reference")},
		Meta{ID: "editor.action.referenceSearch.trigger", Title: "Peek References", Precondition: has("Reference")},

		Meta{ID: "closeReferenceSearch", Precondition: peeking},
		Meta{ID: "goToNextReference", Title: "Go to Next Reference", Category: "Peek", Precondition: peeking},
		Meta{ID: "goToPreviousReference", Title: "Go to Previous Reference", Category: "Peek", Precondition: peeking},
		Meta{ID: "openReference", Precondition: peeking},
		Meta{ID: "openReferenceToSide", Precondition: peeking},
		Meta{ID: "selectNextReference", Precondition: peeking},
		Meta{ID: "selectPreviousReference", Precondition: peeking},

		Meta{ID: "workbench.action.gotoSymbol", Title: "Go to Symbol in Editor..."},
		Meta{ID: "workbench.action.showAllSymbols", Title: "Go to Symbol in Workspace...", Category: "Go to"},
		Meta{ID: "outline.focus", Title: "Focus on Outline View", Category: "View"},
		Meta{ID: "outline.collapse", Title: "Collapse All", Category: "Outline"},
		Meta{ID: "outline.followCursor", Title: "Follow Cursor", Category: "Outline"},
	)
}

// registerNavigationMenus adds the rows of go to definition and its kin to the Go menu and the context menu of the editor, with
// the Peek submenu; a row of the editor's menu is there while the language has the provider (VS Code puts the precondition of
// the command in the `when` of those rows).
//
// Ported from VS Code: src/vs/editor/contrib/gotoSymbol/browser/goToCommands.ts and
// src/vs/workbench/contrib/codeEditor/browser/quickaccess/gotoSymbolQuickAccess.ts
func registerNavigationMenus(r *MenuRegistry) {
	has := func(feature string) contextkey.Expr { return contextkey.Has("editorHas" + feature + "Provider") }
	r.MustAdd(
		MenuItem{Menu: MenubarGoMenu, Command: "workbench.action.showAllSymbols", MnemonicTitle: "Go to Symbol in &&Workspace...", Group: "3_global_nav", Order: 2},
		MenuItem{Menu: MenubarGoMenu, Command: "workbench.action.gotoSymbol", MnemonicTitle: "Go to &&Symbol in Editor...", Group: "4_symbol_nav", Order: 1},
		MenuItem{Menu: MenubarGoMenu, Command: "editor.action.revealDefinition", MnemonicTitle: "Go to &&Definition", Group: "4_symbol_nav", Order: 2, Precondition: contextkey.True()},
		MenuItem{Menu: MenubarGoMenu, Command: "editor.action.revealDeclaration", MnemonicTitle: "Go to &&Declaration", Group: "4_symbol_nav", Order: 3, Precondition: contextkey.True()},
		MenuItem{Menu: MenubarGoMenu, Command: "editor.action.goToTypeDefinition", MnemonicTitle: "Go to &&Type Definition", Group: "4_symbol_nav", Order: 3, Precondition: contextkey.True()},
		MenuItem{Menu: MenubarGoMenu, Command: "editor.action.goToImplementation", MnemonicTitle: "Go to &&Implementations", Group: "4_symbol_nav", Order: 4, Precondition: contextkey.True()},
		MenuItem{Menu: MenubarGoMenu, Command: "editor.action.goToReferences", MnemonicTitle: "Go to &&References", Group: "4_symbol_nav", Order: 5, Precondition: contextkey.True()},

		MenuItem{Menu: EditorContext, Command: "editor.action.revealDefinition", Group: "navigation", Order: 1.1, When: has("Definition")},
		MenuItem{Menu: EditorContext, Command: "editor.action.revealDeclaration", Group: "navigation", Order: 1.3, When: has("Declaration")},
		MenuItem{Menu: EditorContext, Command: "editor.action.goToTypeDefinition", Group: "navigation", Order: 1.4, When: has("TypeDefinition")},
		MenuItem{Menu: EditorContext, Command: "editor.action.goToImplementation", Group: "navigation", Order: 1.45, When: has("Implementation")},
		MenuItem{Menu: EditorContext, Command: "editor.action.goToReferences", Group: "navigation", Order: 1.45, When: has("Reference")},
		MenuItem{Menu: EditorContext, Submenu: EditorContextPeek, Title: "Peek", Group: "navigation", Order: 100,
			When: contextkey.Or(has("Definition"), has("Declaration"), has("TypeDefinition"), has("Implementation"), has("Reference"))},
		MenuItem{Menu: EditorContextPeek, Command: "editor.action.peekDefinition", Group: "peek", Order: 2, When: has("Definition")},
		MenuItem{Menu: EditorContextPeek, Command: "editor.action.peekDeclaration", Group: "peek", Order: 3, When: has("Declaration")},
		MenuItem{Menu: EditorContextPeek, Command: "editor.action.peekTypeDefinition", Group: "peek", Order: 4, When: has("TypeDefinition")},
		MenuItem{Menu: EditorContextPeek, Command: "editor.action.peekImplementation", Group: "peek", Order: 5, When: has("Implementation")},
		MenuItem{Menu: EditorContextPeek, Command: "editor.action.referenceSearch.trigger", Group: "peek", Order: 6, When: has("Reference")},

		// the commands the keys of the peek view run are not in the palette unless the peek is open
		MenuItem{Menu: CommandPalette, Command: "goToNextReference", When: contextkey.Has("referenceSearchVisible")},
		MenuItem{Menu: CommandPalette, Command: "goToPreviousReference", When: contextkey.Has("referenceSearchVisible")},
		MenuItem{Menu: CommandPalette, Command: "outline.collapse", When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: "outline.followCursor", When: contextkey.False()},
	)
}
