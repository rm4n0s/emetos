package settings

// registerLens registers the settings of the code lenses, the inlay hints and the semantic highlighting (step 12.9).
//
// Ported from VS Code: src/vs/editor/common/config/editorOptions.ts (codeLens, codeLensFontFamily, codeLensFontSize,
// inlayHints), src/vs/editor/common/config/editorConfigurationSchema.ts (editor.semanticHighlighting.enabled) and
// src/vs/workbench/services/themes/common/themeConfiguration.ts (editor.semanticTokenColorCustomizations)
func registerLens(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "editor.codeLens", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor shows CodeLens.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.codeLensFontFamily", Types: []Type{String}, Default: "",
			Description: "Controls the font family for CodeLens.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.codeLensFontSize", Types: []Type{Number}, Default: 0, Minimum: Float(0), Maximum: Float(100),
			Description: "Controls the font size in pixels for CodeLens. When set to 0, 90% of `#editor.fontSize#` is used.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.inlayHints.enabled", Types: []Type{String}, Default: "on",
			Enum: []any{"on", "onUnlessPressed", "offUnlessPressed", "off"},
			EnumDescriptions: []string{
				"Inlay hints are enabled",
				"Inlay hints are showing by default and hide when holding Ctrl+Alt",
				"Inlay hints are hidden by default and show when holding Ctrl+Alt",
				"Inlay hints are disabled",
			},
			Description: "Enables the inlay hints in the editor.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.inlayHints.fontSize", Types: []Type{Number}, Default: 0, Minimum: Float(0), Maximum: Float(100),
			Description: "Controls font size of inlay hints in the editor. As default the `#editor.fontSize#` is used when the configured value is less than `5` or greater than the editor font size.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.inlayHints.fontFamily", Types: []Type{String}, Default: "",
			Description: "Controls font family of inlay hints in the editor. When set to empty, the `#editor.fontFamily#` is used.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.inlayHints.padding", Types: []Type{Boolean}, Default: false,
			Description: "Enables the padding around the inlay hints in the editor.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.inlayHints.maximumLength", Types: []Type{Number}, Default: 43, Minimum: Float(0),
			Description: "Maximum overall length of inlay hints, for a single line, before they get truncated by the editor. Set to `0` to never truncate",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.semanticHighlighting.enabled", Types: []Type{Boolean, String}, Default: "configuredByTheme",
			Enum: []any{true, false, "configuredByTheme"},
			EnumDescriptions: []string{
				"Semantic highlighting enabled for all color themes.",
				"Semantic highlighting disabled for all color themes.",
				"Semantic highlighting is configured by the current color theme's `semanticHighlighting` setting.",
			},
			Description: "Controls whether the semanticHighlighting is shown for the languages that support it.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.semanticTokenColorCustomizations", Types: []Type{Object}, Default: map[string]any{},
			Description: "Overrides editor semantic token color and styles from the currently selected color theme.",
			Scope:       ScopeWindow, Category: categoryEditor,
		},
	)
}
