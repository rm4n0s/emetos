package settings

// categoryOutline is the group of the settings of the Outline view.
const categoryOutline = "Outline"

// symbolSettingKinds are the symbol kinds the settings outline.showXxx and breadcrumbs.showXxx name, in the order of the LSP's
// SymbolKind (the same list as core/langfeatures.SymbolKindSetting, which a test compares): the name after "show" and the kind
// as VS Code's descriptions write it.
var symbolSettingKinds = [...]struct{ name, kind string }{
	{"Files", "file"}, {"Modules", "module"}, {"Namespaces", "namespace"}, {"Packages", "package"}, {"Classes", "class"},
	{"Methods", "method"}, {"Properties", "property"}, {"Fields", "field"}, {"Constructors", "constructor"}, {"Enums", "enum"},
	{"Interfaces", "interface"}, {"Functions", "function"}, {"Variables", "variable"}, {"Constants", "constant"},
	{"Strings", "string"}, {"Numbers", "number"}, {"Booleans", "boolean"}, {"Arrays", "array"}, {"Objects", "object"},
	{"Keys", "key"}, {"Null", "null"}, {"EnumMembers", "enumMember"}, {"Structs", "struct"}, {"Events", "event"},
	{"Operators", "operator"}, {"TypeParameters", "typeParameter"},
}

// registerNavigation registers the settings of go to definition and its kin, of the Outline view, of the symbols in the
// breadcrumbs and of the model sticky scroll is built from (step 12.7).
//
// Ported from VS Code: src/vs/editor/common/config/editorOptions.ts (GotoLocation, definitionLinkOpensInPeek, stickyScroll),
// src/vs/workbench/contrib/codeEditor/browser/outline/documentSymbolsOutline.ts and
// src/vs/workbench/browser/parts/editor/breadcrumbs.ts
func registerNavigation(r *Registry) {
	multiple := func(key, what string) Setting {
		return Setting{
			Key: "editor.gotoLocation." + key, Types: []Type{String}, Default: "peek", Enum: []any{"peek", "gotoAndPeek", "goto"},
			EnumDescriptions: []string{
				"Show Peek view of the results (default)",
				"Go to the primary result and show a Peek view",
				"Go to the primary result and enable Peek-less navigation to others",
			},
			Description: "Controls the behavior the '" + what + "' commands when multiple target locations exist.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		}
	}
	alternative := func(key, what, def string) Setting {
		return Setting{
			Key: "editor.gotoLocation." + key, Types: []Type{String}, Default: def,
			Enum: []any{
				"", "editor.action.referenceSearch.trigger", "editor.action.goToReferences", "editor.action.peekImplementation",
				"editor.action.goToImplementation", "editor.action.peekTypeDefinition", "editor.action.goToTypeDefinition",
				"editor.action.peekDeclaration", "editor.action.revealDeclaration", "editor.action.peekDefinition",
				"editor.action.revealDefinitionAside", "editor.action.revealDefinition",
			},
			Description: "Alternative command id that is being executed when the result of '" + what + "' is the current location.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		}
	}
	r.MustRegister(
		multiple("multipleDefinitions", "Go to Definition"),
		multiple("multipleTypeDefinitions", "Go to Type Definition"),
		multiple("multipleDeclarations", "Go to Declaration"),
		multiple("multipleImplementations", "Go to Implementations"),
		multiple("multipleReferences", "Go to References"),
		alternative("alternativeDefinitionCommand", "Go to Definition", "editor.action.goToReferences"),
		alternative("alternativeTypeDefinitionCommand", "Go to Type Definition", "editor.action.goToReferences"),
		alternative("alternativeDeclarationCommand", "Go to Declaration", "editor.action.goToReferences"),
		alternative("alternativeImplementationCommand", "Go to Implementation", ""),
		alternative("alternativeReferenceCommand", "Go to Reference", ""),
		Setting{
			Key: "editor.definitionLinkOpensInPeek", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether the Go to Definition mouse gesture always opens the peek widget.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.stickyScroll.defaultModel", Types: []Type{String}, Default: "outlineModel",
			Enum:        []any{"outlineModel", "foldingProviderModel", "indentationModel"},
			Description: "Defines the model to use for determining which lines to stick. If the outline model does not exist, it will fall back on the folding provider model which falls back on the indentation model. This order is respected in all three cases.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "outline.icons", Types: []Type{Boolean}, Default: true,
			Description: "Render Outline Elements with Icons.",
			Scope:       ScopeWindow, Category: categoryOutline,
		},
		Setting{
			Key: "outline.collapseItems", Types: []Type{String}, Default: "alwaysExpand", Enum: []any{"alwaysCollapse", "alwaysExpand"},
			EnumDescriptions: []string{"Collapse all items when the outline is built.", "Expand all items when the outline is built."},
			Description:      "Controls whether the items of the outline are collapsed when it is built.",
			Scope:            ScopeWindow, Category: categoryOutline,
		},
		Setting{
			Key: "breadcrumbs.symbolPath", Types: []Type{String}, Default: "on", Enum: []any{"on", "off", "last"},
			EnumDescriptions: []string{
				"Show all symbols in the breadcrumbs view.",
				"Do not show symbols in the breadcrumbs view.",
				"Only show the current symbol in the breadcrumbs view.",
			},
			Description: "Controls whether and how symbols are shown in the breadcrumbs view.",
			Scope:       ScopeWindow, Category: categoryBreadcrumbs,
		},
		Setting{
			Key: "breadcrumbs.symbolSortOrder", Types: []Type{String}, Default: "position", Enum: []any{"position", "name", "type"},
			EnumDescriptions: []string{
				"Show symbol outline in file position order.",
				"Show symbol outline in alphabetical order.",
				"Show symbol outline in symbol type order.",
			},
			Description: "Controls how symbols are sorted in the breadcrumbs outline view.",
			Scope:       ScopeWindow, Category: categoryBreadcrumbs,
		},
	)
	for _, k := range symbolSettingKinds {
		r.MustRegister(
			Setting{
				Key: "breadcrumbs.show" + k.name, Types: []Type{Boolean}, Default: true,
				Description: "When enabled breadcrumbs show `" + k.kind + "`-symbols.",
				Scope:       ScopeWindow, Category: categoryBreadcrumbs,
			},
			Setting{
				Key: "outline.show" + k.name, Types: []Type{Boolean}, Default: true,
				Description: "When enabled outline shows `" + k.kind + "`-symbols.",
				Scope:       ScopeWindow, Category: categoryOutline,
			},
		)
	}
}
