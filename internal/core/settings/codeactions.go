package settings

// registerCodeActions registers the settings of the code actions, the formatting and the rename (step 12.8).
//
// Ported from VS Code: src/vs/editor/common/config/editorOptions.ts (lightbulb, formatOnPaste, formatOnType),
// src/vs/editor/contrib/codeAction/browser/codeActionContributions.ts, src/vs/workbench/contrib/codeActions/browser/codeActionsContribution.ts,
// src/vs/workbench/contrib/files/browser/files.contribution.ts (formatOnSave, formatOnSaveMode),
// src/vs/workbench/contrib/format/browser/formatActionsMultiple.ts (defaultFormatter) and src/vs/editor/contrib/rename/browser/rename.ts
func registerCodeActions(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "editor.lightbulb.enabled", Types: []Type{String}, Default: "onCode", Enum: []any{"off", "onCode", "on"},
			EnumDescriptions: []string{
				"Disable the code action menu.",
				"Show the code action menu when the cursor is on lines with code.",
				"Show the code action menu when the cursor is on lines with code or on empty lines.",
			},
			Description: "Enables the Code Action lightbulb in the editor.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.codeActionWidget.showHeaders", Types: []Type{Boolean}, Default: true,
			Description: "Enable/disable showing group headers in the Code Action menu.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.codeActionWidget.includeNearbyQuickFixes", Types: []Type{Boolean}, Default: true,
			Description: "Enable/disable showing nearest Quick Fix within a line when not currently on a diagnostic.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.codeActions.triggerOnFocusChange", Types: []Type{Boolean}, Default: false,
			Description: "Enable triggering `#editor.codeActionsOnSave#` when `#files.autoSave#` is set to `afterDelay`. Code Actions must be set to `always` to be triggered for window and focus changes.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.codeActionsOnSave", Types: []Type{Object, Array}, Default: map[string]any{}, ItemTypes: []Type{String},
			Description: "Run Code Actions for the editor on save. Code Actions must be specified and the editor must not be shutting down. When `#files.autoSave#` is set to `afterDelay`, Code Actions will only be run when the file is saved explicitly. Example: `\"source.organizeImports\": \"explicit\"`. A value is `always`, `explicit`, `never`, or true and false as older versions of VS Code wrote them.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.codeActionsOnSaveTimeout", Types: []Type{Number}, Default: 750, Minimum: Float(0),
			Description: "Timeout in milliseconds after which the Code Actions that are run on file save are cancelled. A save never waits for longer.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.formatOnSave", Types: []Type{Boolean}, Default: false,
			Description: "Format a file on save. A formatter must be available and the editor must not be shutting down. When `#files.autoSave#` is set to `afterDelay`, the file will only be formatted when saved explicitly.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.formatOnSaveMode", Types: []Type{String}, Default: "file", Enum: []any{"file", "modifications", "modificationsIfAvailable"},
			EnumDescriptions: []string{
				"Format the whole file.",
				"Format modifications. Requires a file on disk to compare with and a formatter that supports 'Format Selection'.",
				"Will attempt to format modifications only (requires a file on disk to compare with and a formatter that supports 'Format Selection'). If there is nothing to compare with, then the whole file will be formatted.",
			},
			Description: "Controls if format on save formats the whole file or only modifications. Only applies when `#editor.formatOnSave#` is enabled.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.formatOnSaveTimeout", Types: []Type{Number}, Default: 750, Minimum: Float(0),
			Description: "Timeout in milliseconds after which the formatting that is run on file save is cancelled. A save never waits for longer.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.formatOnType", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether the editor should automatically format the line after typing.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.formatOnPaste", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether the editor should automatically format the pasted content. A formatter must be available and the formatter should be able to format a range in a document.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.defaultFormatter", Types: []Type{String, Null}, Default: nil,
			Description: "Defines a default formatter which takes precedence over all other formatter settings. Must be the identifier of an extension contributing a formatter, such as `emetos.golang`.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.rename.enablePreview", Types: []Type{Boolean}, Default: true,
			Description: "Enable/disable the ability to preview changes before renaming.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
	)
}
