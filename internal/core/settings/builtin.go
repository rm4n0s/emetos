package settings

import "github.com/rm4n0s/emetos/internal/core/fileio"

// usualWordSeparators is VS Code's USUAL_WORD_SEPARATORS.
//
// Ported from VS Code: src/vs/editor/common/core/wordHelper.ts
const usualWordSeparators = "`~!@#$%^&*()-=+[{]}\\|;:'\",.<>/?"

const (
	categoryEditor = "Text Editor"
	categoryFiles  = "Files"
)

// DefaultRegistry makes a registry with the settings Emetos has. Each step that reads a
// setting adds it here (or to a function it calls) with the values of VS Code.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	registerEditor(r)
	registerFiles(r)
	return r
}

// registerEditor registers the editor.* settings.
//
// Ported from VS Code: src/vs/editor/common/config/editorConfigurationSchema.ts and
// src/vs/editor/common/config/editorOptions.ts
func registerEditor(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "editor.tabSize", Types: []Type{Number}, Default: 4, Minimum: Float(1), Maximum: Float(100),
			Description: "The number of spaces a tab is equal to. This setting is overridden based on the file contents when `editor.detectIndentation` is on.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.insertSpaces", Types: []Type{Boolean}, Default: true,
			Description: "Insert spaces when pressing `Tab`. This setting is overridden based on the file contents when `editor.detectIndentation` is on.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.detectIndentation", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether `editor.tabSize` and `editor.insertSpaces` will be automatically detected when a file is opened based on the file contents.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.wordSeparators", Types: []Type{String}, Default: usualWordSeparators,
			Description: "Characters that will be used as word separators when doing word related navigations or operations.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		// VS Code's default is the platform's list of fonts. Emetos defaults to the desktop's
		// monospace font (SPEC D14), which an empty value stands for.
		Setting{
			Key: "editor.fontFamily", Types: []Type{String}, Default: "",
			Description: "Controls the font family. A comma-separated list is tried in order; empty means the desktop's monospace font.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.fontSize", Types: []Type{Number}, Default: 14, Minimum: Float(6), Maximum: Float(100),
			Description: "Controls the font size in pixels.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.lineHeight", Types: []Type{Number}, Default: 0, Minimum: Float(0), Maximum: Float(150),
			Description: "Controls the line height. Use 0 to automatically compute the line height from the font size. Values between 0 and 8 will be used as a multiplier with the font size. Values greater than or equal to 8 will be used as effective values.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.fontLigatures", Types: []Type{Boolean, String}, Default: false,
			Description: "Configures font ligatures or font features. Can be either a boolean to enable/disable ligatures or a string for the value of the CSS 'font-feature-settings' property.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.lineNumbers", Types: []Type{String}, Default: "on",
			Enum: []any{"off", "on", "relative", "interval"},
			EnumDescriptions: []string{
				"Line numbers are not rendered.",
				"Line numbers are rendered as absolute number.",
				"Line numbers are rendered as distance in lines to cursor position.",
				"Line numbers are rendered every 10 lines.",
			},
			Description: "Controls the display of line numbers.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.glyphMargin", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor should render the vertical glyph margin. Glyph margin is mostly used for debugging.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.lineDecorationsWidth", Types: []Type{Number, String}, Default: 10,
			Description: "Controls the width of line decorations in the gutter, in pixels.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.folding", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor has code folding enabled.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.scrollBeyondLastLine", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor will scroll beyond the last line.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.renderLineHighlight", Types: []Type{String}, Default: "line",
			Enum: []any{"none", "gutter", "line", "all"},
			EnumDescriptions: []string{
				"The current line is not highlighted.",
				"The current line is highlighted in the gutter.",
				"The current line is highlighted in the text.",
				"The current line is highlighted in the gutter and in the text.",
			},
			Description: "Controls how the editor highlights the current line.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.renderLineHighlightOnlyWhenFocus", Types: []Type{Boolean}, Default: false,
			Description: "Controls if the editor highlights the current line only when the editor is focused.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.roundedSelection", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether selections have rounded corners.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.cursorWidth", Types: []Type{Integer}, Default: 0, Minimum: Float(0),
			Description: "Controls the width of the cursor in pixels; 0 means 2 pixels.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.cursorBlinking", Types: []Type{String}, Default: "blink",
			Enum: []any{"blink", "smooth", "phase", "expand", "solid"},
			EnumDescriptions: []string{
				"The cursor is shown and hidden every half second.",
				"Emetos blinks like \"blink\" (the fading animation of VS Code is not implemented).",
				"Emetos blinks like \"blink\" (the fading animation of VS Code is not implemented).",
				"Emetos blinks like \"blink\" (the growing animation of VS Code is not implemented).",
				"The cursor does not blink.",
			},
			Description: "Controls the cursor animation style.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.multiCursorModifier", Types: []Type{String}, Default: "alt",
			Enum: []any{"ctrlCmd", "alt"},
			EnumDescriptions: []string{
				"Maps to Control on Windows and Linux.",
				"Maps to Alt on Windows and Linux.",
			},
			Description: "The modifier to be used to add multiple cursors with the mouse. Go To Definition and Open Link mouse gestures will adapt such that they do not conflict with the multicursor modifier.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.columnSelection", Types: []Type{Boolean}, Default: false,
			Description: "Enable that the selection with the mouse and keys is doing column selection.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.mouseStyle", Types: []Type{String}, Default: "text",
			Enum:        []any{"text", "default", "copy"},
			Description: "Controls the mouse pointer style when over the editor content.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.mouseWheelScrollSensitivity", Types: []Type{Number}, Default: 1,
			Description: "A multiplier to be used on the `deltaX` and `deltaY` of mouse wheel scroll events.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.fastScrollSensitivity", Types: []Type{Number}, Default: 5,
			Description: "Scrolling speed multiplier when pressing `Alt`.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.smoothScrolling", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether the editor will scroll using an animation.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.dragAndDrop", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor should allow moving selections via drag and drop.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.selectionClipboard", Types: []Type{Boolean}, Default: true,
			Description: "Controls if the Linux primary clipboard should be supported.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.lineNumbersMinChars", Types: []Type{Integer}, Default: 5, Minimum: Float(1), Maximum: Float(300),
			Description: "Controls the minimal number of characters the line numbers take; the gutter is wide enough for the number of the last line.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
	)
}

// registerFiles registers the files.* settings.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/files.contribution.ts
func registerFiles(r *Registry) {
	var ids []any
	var labels []string
	for _, e := range fileio.Encodings() {
		ids = append(ids, e.ID)
		labels = append(labels, e.LabelLong)
	}
	r.MustRegister(
		Setting{
			Key: "files.encoding", Types: []Type{String}, Default: "utf8", Enum: ids, EnumDescriptions: labels,
			Description: "The default character set encoding to use when reading and writing files. This setting can also be configured per language.",
			Scope:       ScopeLanguageOverridable, Category: categoryFiles,
		},
		Setting{
			Key: "files.autoGuessEncoding", Types: []Type{Boolean}, Default: false,
			Description: "When enabled, the editor will attempt to guess the character set encoding when opening files. This setting can also be configured per language. Note, this setting is not respected by text search. Only `files.encoding` is respected.",
			Scope:       ScopeLanguageOverridable, Category: categoryFiles,
		},
		Setting{
			Key: "files.eol", Types: []Type{String}, Default: "auto", Enum: []any{"\n", "\r\n", "auto"},
			EnumDescriptions: []string{"LF", "CRLF", "Uses operating system specific end of line character."},
			Description:      "The default end of line character.",
			Scope:            ScopeLanguageOverridable, Category: categoryFiles,
		},
	)
}
