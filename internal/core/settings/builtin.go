package settings

import "github.com/rm4n0s/emetos/internal/core/fileio"

// usualWordSeparators is VS Code's USUAL_WORD_SEPARATORS.
//
// Ported from VS Code: src/vs/editor/common/core/wordHelper.ts
const usualWordSeparators = "`~!@#$%^&*()-=+[{]}\\|;:'\",.<>/?"

const (
	categoryEditor = "Text Editor"
	categoryFiles  = "Files"
	// categoryWorkbench is the group of the workbench's settings; the settings of the theme are in
	// the "Workbench" group of VS Code's settings editor.
	categoryWorkbench = "Workbench"
	categoryWindow    = "Window"
	// categoryBreadcrumbs is the group of the breadcrumbs settings.
	categoryBreadcrumbs = "Breadcrumbs"
	// categoryEmmet is the group of the Emmet settings.
	categoryEmmet = "Emmet"
)

// DefaultRegistry makes a registry with the settings Emetos has. Each step that reads a
// setting adds it here (or to a function it calls) with the values of VS Code.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	registerEditor(r)
	registerFiles(r)
	registerWorkbench(r)
	registerLayout(r)
	registerEditorGroups(r)
	registerEmmet(r)
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
			Key: "editor.maxTokenizationLineLength", Types: []Type{Number}, Default: 20000,
			Description: "Lines above this length will not be tokenized for performance reasons",
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
			Key: "editor.guides.bracketPairs", Types: []Type{Boolean, String}, Default: false,
			Enum: []any{true, "active", false},
			EnumDescriptions: []string{
				"Enables bracket pair guides.",
				"Enables bracket pair guides only for the active bracket pair.",
				"Disables bracket pair guides.",
			},
			Description: "Controls whether bracket pair guides are enabled or not.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.guides.bracketPairsHorizontal", Types: []Type{Boolean, String}, Default: "active",
			Enum: []any{true, "active", false},
			EnumDescriptions: []string{
				"Enables horizontal guides as addition to vertical bracket pair guides.",
				"Enables horizontal guides only for the active bracket pair.",
				"Disables horizontal bracket pair guides.",
			},
			Description: "Controls whether horizontal bracket pair guides are enabled or not.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.guides.highlightActiveBracketPair", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor should highlight the active bracket pair.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.guides.indentation", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor should render indent guides.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.guides.highlightActiveIndentation", Types: []Type{Boolean, String}, Default: true,
			Enum: []any{true, "always", false},
			EnumDescriptions: []string{
				"Highlights the active indent guide.",
				"Highlights the active indent guide even if bracket guides are highlighted.",
				"Do not highlight the active indent guide.",
			},
			Description: "Controls whether the editor should highlight the active indent guide.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.autoIndent", Types: []Type{String}, Default: "full",
			Enum: []any{"none", "keep", "brackets", "advanced", "full"},
			EnumDescriptions: []string{
				"The editor will not insert indentation automatically.",
				"The editor will keep the current line's indentation.",
				"The editor will keep the current line's indentation and honor language defined brackets.",
				"The editor will keep the current line's indentation, honor language defined brackets and invoke special onEnterRules defined by languages.",
				"The editor will keep the current line's indentation, honor language defined brackets, invoke special onEnterRules defined by languages, and honor indentationRules defined by languages.",
			},
			Description: "Controls whether the editor should automatically adjust the indentation when users type, paste, move or indent lines.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.autoIndentOnPaste", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether the editor should automatically auto-indent the pasted content.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.autoIndentOnPasteWithinString", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor should automatically auto-indent the pasted content when pasted within a string. This takes effect when `editor.autoIndentOnPaste` is true.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.autoClosingBrackets", Types: []Type{String}, Default: "languageDefined",
			Enum: []any{"always", "languageDefined", "beforeWhitespace", "never"},
			EnumDescriptions: []string{
				"",
				"Use language configurations to determine when to autoclose brackets.",
				"Autoclose brackets only when the cursor is to the left of whitespace.",
				"",
			},
			Description: "Controls whether the editor should automatically close brackets after the user adds an opening bracket.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.autoClosingComments", Types: []Type{String}, Default: "languageDefined",
			Enum: []any{"always", "languageDefined", "beforeWhitespace", "never"},
			EnumDescriptions: []string{
				"",
				"Use language configurations to determine when to autoclose comments.",
				"Autoclose comments only when the cursor is to the left of whitespace.",
				"",
			},
			Description: "Controls whether the editor should automatically close comments after the user adds an opening comment.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.autoClosingQuotes", Types: []Type{String}, Default: "languageDefined",
			Enum: []any{"always", "languageDefined", "beforeWhitespace", "never"},
			EnumDescriptions: []string{
				"",
				"Use language configurations to determine when to autoclose quotes.",
				"Autoclose quotes only when the cursor is to the left of whitespace.",
				"",
			},
			Description: "Controls whether the editor should automatically close quotes after the user adds an opening quote.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.autoClosingDelete", Types: []Type{String}, Default: "auto",
			Enum: []any{"always", "auto", "never"},
			EnumDescriptions: []string{
				"",
				"Remove adjacent closing quotes or brackets only if they were automatically inserted.",
				"",
			},
			Description: "Controls whether the editor should remove adjacent closing quotes or brackets when deleting.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.autoClosingOvertype", Types: []Type{String}, Default: "auto",
			Enum: []any{"always", "auto", "never"},
			EnumDescriptions: []string{
				"",
				"Type over closing quotes or brackets only if they were automatically inserted.",
				"",
			},
			Description: "Controls whether the editor should type over closing quotes or brackets.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.autoSurround", Types: []Type{String}, Default: "languageDefined",
			Enum: []any{"languageDefined", "quotes", "brackets", "never"},
			EnumDescriptions: []string{
				"Use language configurations to determine when to automatically surround selections.",
				"Surround with quotes but not brackets.",
				"Surround with brackets but not quotes.",
				"",
			},
			Description: "Controls whether the editor should automatically surround selections when typing quotes or brackets.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.comments.insertSpace", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether a space character is inserted when commenting.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.comments.ignoreEmptyLines", Types: []Type{Boolean}, Default: true,
			Description: "Controls if empty lines should be ignored with toggle, add or remove actions for line comments.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.foldingMaximumRegions", Types: []Type{Integer}, Default: 5000, Minimum: Float(10), Maximum: Float(65000),
			Description: "The maximum number of foldable regions. Increasing this value may result in the editor becoming less responsive when the current source has a large number of foldable regions.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.find.cursorMoveOnType", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the cursor should jump to find matches while typing.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.find.loop", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the search automatically restarts from the beginning (or the end) when no further matches can be found.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.wordWrap", Types: []Type{String}, Default: "off",
			Enum: []any{"off", "on", "wordWrapColumn", "bounded"},
			EnumDescriptions: []string{
				"Lines will never wrap.",
				"Lines will wrap at the viewport width.",
				"Lines will wrap at `#editor.wordWrapColumn#`.",
				"Lines will wrap at the minimum of the viewport and `#editor.wordWrapColumn#`.",
			},
			Description: "Controls how lines should wrap.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.wordWrapColumn", Types: []Type{Integer}, Default: 80, Minimum: Float(1),
			Description: "Controls the wrapping column of the editor when `#editor.wordWrap#` is `wordWrapColumn` or `bounded`.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.wrappingIndent", Types: []Type{String}, Default: "same",
			Enum: []any{"none", "same", "indent", "deepIndent"},
			EnumDescriptions: []string{
				"No indentation. Wrapped lines begin at column 1.",
				"Wrapped lines get the same indentation as the parent.",
				"Wrapped lines get +1 indentation toward the parent.",
				"Wrapped lines get +2 indentation toward the parent.",
			},
			Description: "Controls the indentation of wrapped lines.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.wrappingStrategy", Types: []Type{String}, Default: "simple",
			Enum: []any{"simple", "advanced"},
			EnumDescriptions: []string{
				"Assumes that all characters are of the same width. This is a fast algorithm that works correctly for monospace fonts and certain scripts (like Latin characters) where glyphs are of equal width.",
				"Delegates wrapping points computation to the text engine. This is a slow algorithm, that might cause freezes for large files, but it works correctly in all cases.",
			},
			Description: "Controls the algorithm that computes wrapping points. Note that when in accessibility mode, advanced will be used for the best experience.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.renderWhitespace", Types: []Type{String}, Default: "selection",
			Enum: []any{"none", "boundary", "selection", "trailing", "all"},
			EnumDescriptions: []string{
				"",
				"Render whitespace characters except for single spaces between words.",
				"Render whitespace characters only on selected text.",
				"Render only trailing whitespace characters.",
				"",
			},
			Description: "Controls how the editor should render whitespace characters.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.renderControlCharacters", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor should render control characters.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.rulers", Types: []Type{Array}, Default: []any{}, ItemTypes: []Type{Number, Object},
			Description: "Render vertical rulers after a certain number of monospace characters. Use multiple values for multiple rulers. No rulers are drawn if the array is empty.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.occurrencesHighlight", Types: []Type{String}, Default: "singleFile",
			Enum: []any{"off", "singleFile", "multiFile"},
			EnumDescriptions: []string{
				"Does not highlight occurrences.",
				"Highlights occurrences only in the current file.",
				"Experimental: Highlights occurrences across all valid open files.",
			},
			Description: "Controls whether occurrences should be highlighted across open files.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.occurrencesHighlightDelay", Types: []Type{Integer}, Default: 250, Minimum: Float(0), Maximum: Float(2000),
			Description: "Controls the delay in milliseconds after which occurrences are highlighted.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.selectionHighlight", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor should highlight matches similar to the selection.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.selectionHighlightMaxLength", Types: []Type{Integer}, Default: 200, Minimum: Float(0), Maximum: Float(1000),
			Description: "Controls how many characters can be in the selection before similar matches are not highlighted. Set to zero for unlimited.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.selectionHighlightMultiline", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether the editor should highlight selection matches that span multiple lines.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.minimap.enabled", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the minimap is shown.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.minimap.side", Types: []Type{String}, Default: "right",
			Enum:        []any{"right", "left"},
			Description: "Controls the side where to render the minimap.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.minimap.size", Types: []Type{String}, Default: "proportional",
			Enum: []any{"proportional", "fill", "fit"},
			EnumDescriptions: []string{
				"The minimap has the same size as the editor contents (and might scroll).",
				"The minimap will stretch or shrink as necessary to fill the height of the editor (no scrolling).",
				"The minimap will shrink as necessary to never be larger than the editor (no scrolling).",
			},
			Description: "Controls the size of the minimap.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.minimap.showSlider", Types: []Type{String}, Default: "mouseover",
			Enum:        []any{"always", "mouseover"},
			Description: "Controls when the minimap slider is shown.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.minimap.scale", Types: []Type{Integer}, Default: 1, Enum: []any{1, 2, 3},
			Description: "Scale of content drawn in the minimap: 1, 2 or 3.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.minimap.renderCharacters", Types: []Type{Boolean}, Default: true,
			Description: "Render the actual characters on a line as opposed to color blocks. Emetos always renders color blocks.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.minimap.maxColumn", Types: []Type{Integer}, Default: 120,
			Description: "Limit the width of the minimap to render at most a certain number of columns.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.stickyScroll.enabled", Types: []Type{Boolean}, Default: true,
			Description: "Shows the nested current scopes during the scroll at the top of the editor.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.stickyScroll.maxLineCount", Types: []Type{Number}, Default: 5, Minimum: Float(1), Maximum: Float(20),
			Description: "Defines the maximum number of sticky lines to show.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.stickyScroll.scrollWithEditor", Types: []Type{Boolean}, Default: true,
			Description: "Enable scrolling of Sticky Scroll with the editor's horizontal scrollbar.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.stopRenderingLineAfter", Types: []Type{Integer}, Default: 10000, Minimum: Float(-1),
			Description: "Lines longer than this many characters are not wrapped, and a negative value never stops.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.find.seedSearchStringFromSelection", Types: []Type{String}, Default: "always",
			Enum: []any{"never", "always", "selection"},
			EnumDescriptions: []string{
				"Never seed search string from the editor selection.",
				"Always seed search string from the editor selection, including word at cursor position.",
				"Only seed search string from the editor selection.",
			},
			Description: "Controls whether the search string in the Find Widget is seeded from the editor selection.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.find.autoFindInSelection", Types: []Type{String}, Default: "never",
			Enum: []any{"never", "always", "multiline"},
			EnumDescriptions: []string{
				"Never turn on Find in Selection automatically (default).",
				"Always turn on Find in Selection automatically.",
				"Turn on Find in Selection automatically when multiple lines of content are selected.",
			},
			Description: "Controls the condition for turning on Find in Selection automatically.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.find.closeOnResult", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether the Find Widget closes when a result is found by pressing Enter.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.showFoldingControls", Types: []Type{String}, Default: "mouseover",
			Enum: []any{"always", "never", "mouseover"},
			EnumDescriptions: []string{
				"Always show the folding controls.",
				"Never show the folding controls and reduce the gutter size.",
				"Only show the folding controls when the mouse is over the gutter.",
			},
			Description: "Controls when the folding controls on the gutter are shown.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.foldingStrategy", Types: []Type{String}, Default: "auto",
			Enum: []any{"auto", "indentation"},
			EnumDescriptions: []string{
				"Use a language-specific folding strategy if available, else the indentation-based one.",
				"Use the indentation-based folding strategy.",
			},
			Description: "Controls the strategy for computing folding ranges.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.foldingHighlight", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor should highlight folded ranges.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.foldingImportsByDefault", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether the editor automatically collapses import ranges.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.unfoldOnClickAfterEndOfLine", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether clicking on the empty content after a folded line will unfold the line.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.matchBrackets", Types: []Type{String}, Default: "always",
			Enum: []any{"always", "near", "never"},
			EnumDescriptions: []string{
				"Highlight the brackets of the pair around the cursor, and the matching bracket of one next to it.",
				"Highlight the matching bracket of the one next to the cursor only.",
				"Do not highlight matching brackets.",
			},
			Description: "Highlight matching brackets.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.bracketPairColorization.enabled", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether bracket pair colorization is enabled or not. Use 'workbench.colorCustomizations' to override the bracket highlight colors.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.bracketPairColorization.independentColorPoolPerBracketType", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether each bracket type has its own independent color pool.",
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
				"The cursor fades out and in.",
				"The cursor fades out and in, staying dark longer.",
				"The cursor shrinks to nothing and grows back.",
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
			Key: "editor.tabCompletion", Types: []Type{String}, Default: "off",
			Enum: []any{"on", "off", "onlySnippets"},
			EnumDescriptions: []string{
				"Tab complete will insert the best matching suggestion when pressing tab.",
				"Disable tab completions.",
				"Tab complete snippets when their prefix match. Works best when 'quickSuggestions' aren't enabled.",
			},
			Description: "Enables tab completions.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.multiCursorPaste", Types: []Type{String}, Default: "spread",
			Enum: []any{"spread", "full"},
			EnumDescriptions: []string{
				"Each cursor pastes a single line of the text.",
				"Each cursor pastes the full text.",
			},
			Description: "Controls pasting when the line count of the pasted text matches the cursor count.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.emptySelectionClipboard", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether copying without a selection copies the current line.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.fontWeight", Types: []Type{String}, Default: "normal",
			Enum:        []any{"normal", "bold", "100", "200", "300", "400", "500", "600", "700", "800", "900"},
			Description: "Controls the font weight. Accepts \"normal\" and \"bold\" keywords or numbers between 1 and 1000.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.letterSpacing", Types: []Type{Number}, Default: 0,
			Description: "Controls the letter spacing in pixels.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.cursorStyle", Types: []Type{String}, Default: "line",
			Enum:        []any{"line", "block", "underline", "line-thin", "block-outline", "underline-thin"},
			Description: "Controls the cursor style in insert input mode.",
			Scope:       ScopeLanguageOverridable, Category: categoryEditor,
		},
		Setting{
			Key: "editor.mouseWheelZoom", Types: []Type{Boolean}, Default: false,
			Description: "Zoom the font of the editor when using mouse wheel and holding `Ctrl`.",
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
			Key: "editor.largeFileOptimizations", Types: []Type{Boolean}, Default: true,
			Description: "Special handling for large files to disable certain memory intensive features.",
			Scope:       ScopeWindow, Category: categoryFiles,
		},
		Setting{
			Key: "files.associations", Types: []Type{Object}, Default: map[string]any{},
			Description: "Configure [glob patterns](https://aka.ms/vscode-glob-patterns) of file associations to languages (for example `\"*.extension\": \"html\"`). Patterns will match on the absolute path of a file if they contain a path separator and will match on the name of the file otherwise. These have precedence over the default associations of the languages installed.",
			Scope:       ScopeWindow, Category: categoryFiles,
		},
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

// registerWorkbench registers the workbench.* settings that choose the color theme. The default
// themes have the names of Emetos's bundled themes ("Dark Modern", where VS Code says "Default
// Dark Modern"), and the value is the label or id of any theme, bundled or the user's, so the
// settings have no fixed list of values.
//
// Ported from VS Code: src/vs/workbench/services/themes/common/themeConfiguration.ts
func registerWorkbench(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "workbench.colorTheme", Types: []Type{String}, Default: "Dark Modern",
			Description: "Specifies the color theme used in the workbench when `window.autoDetectColorScheme` is not enabled.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.preferredDarkColorTheme", Types: []Type{String}, Default: "Dark Modern",
			Description: "Specifies the color theme when system color mode is dark and `window.autoDetectColorScheme` is enabled.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.preferredLightColorTheme", Types: []Type{String}, Default: "Light Modern",
			Description: "Specifies the color theme when system color mode is light and `window.autoDetectColorScheme` is enabled.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "breadcrumbs.enabled", Types: []Type{Boolean}, Default: true,
			Description: "Enable/disable navigation breadcrumbs.",
			Scope:       ScopeWindow, Category: categoryBreadcrumbs,
		},
		Setting{
			Key: "breadcrumbs.filePath", Types: []Type{String}, Default: "on", Enum: []any{"on", "off", "last"},
			EnumDescriptions: []string{
				"Show the file path in the breadcrumbs view.",
				"Do not show the file path in the breadcrumbs view.",
				"Only show the last element of the file path in the breadcrumbs view.",
			},
			Description: "Controls whether and how file paths are shown in the breadcrumbs view.",
			Scope:       ScopeWindow, Category: categoryBreadcrumbs,
		},
		Setting{
			Key: "breadcrumbs.icons", Types: []Type{Boolean}, Default: true,
			Description: "Render breadcrumb items with icons.",
			Scope:       ScopeWindow, Category: categoryBreadcrumbs,
		},
		Setting{
			Key: "window.autoDetectColorScheme", Types: []Type{Boolean}, Default: false,
			Description: "If enabled, will automatically select a color theme based on the system color mode. If the system color mode is dark, `workbench.preferredDarkColorTheme` is used, else `workbench.preferredLightColorTheme`.",
			Scope:       ScopeWindow, Category: categoryWindow,
		},
		// Not ported: VS Code's "compact" value (the menu as a button in the side bar), which
		// Emetos's menu bar does not have.
		Setting{
			Key: "window.menuBarVisibility", Types: []Type{String}, Default: "classic",
			Enum: []any{"classic", "visible", "toggle", "hidden"},
			EnumDescriptions: []string{
				"Menu is displayed at the top of the window and only hidden in full screen mode.",
				"Menu is always visible at the top of the window even in full screen mode.",
				"Menu is hidden but can be displayed at the top of the window via the Alt key.",
				"Menu is always hidden.",
			},
			Description: "Control the visibility of the menu bar. A setting of 'toggle' means that the menu bar is hidden and a single press of the Alt key will show it. A setting of 'compact' will move the menu into the side bar.",
			Scope:       ScopeApplication, Category: categoryWindow,
		},
	)
}

// DefaultWindowTitle is the default of window.title on Linux: VS Code's template, which the title
// of the window is made from (SPEC §10.1).
//
// Ported from VS Code: src/vs/workbench/browser/parts/titlebar/windowTitle.ts (defaultWindowTitle)
const DefaultWindowTitle = "${dirty}${activeEditorShort}${separator}${rootName}${separator}${profileName}${separator}${appName}"

// DefaultWindowTitleSeparator is the default of window.titleSeparator on Linux.
const DefaultWindowTitleSeparator = " - "

// windowTitleDescription is the description of window.title: the list of its variables.
const windowTitleDescription = "Controls the window title based on the current context such as the opened workspace or active editor. Variables are substituted based on the context:\n" +
	"- `${activeEditorShort}`: the file name (e.g. myFile.txt).\n" +
	"- `${activeEditorMedium}`: the path of the file relative to the workspace folder (e.g. myFolder/myFileFolder/myFile.txt).\n" +
	"- `${activeEditorLong}`: the full path of the file (e.g. /Users/Development/myFolder/myFileFolder/myFile.txt).\n" +
	"- `${activeEditorLanguageId}`: the language identifier of the active editor (e.g. typescript).\n" +
	"- `${activeFolderShort}`: the name of the folder the file is contained in (e.g. myFileFolder).\n" +
	"- `${activeFolderMedium}`: the path of the folder the file is contained in, relative to the workspace folder (e.g. myFolder/myFileFolder).\n" +
	"- `${activeFolderLong}`: the full path of the folder the file is contained in (e.g. /Users/Development/myFolder/myFileFolder).\n" +
	"- `${folderName}`: name of the workspace folder the file is contained in (e.g. myFolder).\n" +
	"- `${folderPath}`: file path of the workspace folder the file is contained in (e.g. /Users/Development/myFolder).\n" +
	"- `${rootName}`: name of the workspace with optional remote name and workspace indicator if applicable (e.g. myFolder, myRemoteFolder [SSH] or myWorkspace (Workspace)).\n" +
	"- `${rootNameShort}`: shortened name of the workspace without suffixes (e.g. myFolder, myRemoteFolder or myWorkspace).\n" +
	"- `${rootPath}`: file path of the opened workspace or folder (e.g. /Users/Development/myWorkspace).\n" +
	"- `${profileName}`: name of the profile in which the workspace is opened (e.g. Data Science (Profile)). Ignored if default profile is used.\n" +
	"- `${appName}`: e.g. VS Code.\n" +
	"- `${remoteName}`: e.g. SSH\n" +
	"- `${dirty}`: an indicator for when the active editor has unsaved changes.\n" +
	"- `${focusedView}`: the name of the view that is currently focused.\n" +
	"- `${activeRepositoryName}`: the name of the active repository (e.g. vscode).\n" +
	"- `${activeRepositoryBranchName}`: the name of the active branch in the active repository (e.g. main).\n" +
	"- `${activeEditorState}`: provides information about the state of the active editor (e.g. modified). This will be appended by default when in screen reader mode with `accessibility.windowTitleOptimized` enabled.\n" +
	"- `${separator}`: a conditional separator (\" - \") that only shows when surrounded by variables with values or static text."

// registerLayout registers the settings of the layout of the workbench and of the title of the
// window (SPEC §10.1). Not ported: the values top and bottom of workbench.activityBar.location
// are registered, but the activity bar then stays at its side (the layout has no activity bar on
// top or at the bottom).
//
// Ported from VS Code: src/vs/workbench/browser/workbench.contribution.ts
func registerLayout(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "workbench.sideBar.location", Types: []Type{String}, Default: "left", Enum: []any{"left", "right"},
			Description: "Controls the location of the primary side bar and activity bar. They can either show on the left or right of the workbench. The secondary side bar will show on the opposite side of the workbench.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.activityBar.location", Types: []Type{String}, Default: "default", Enum: []any{"default", "top", "bottom", "hidden"},
			EnumDescriptions: []string{
				"Show the Activity Bar on the side of the Primary Side Bar and on top of the Secondary Side Bar.",
				"Show the Activity Bar on top of the Primary and Secondary Side Bars.",
				"Show the Activity Bar at the bottom of the Primary and Secondary Side Bars.",
				"Hide the Activity Bar in the Primary and Secondary Side Bars.",
			},
			Description: "Controls the location of the Activity Bar relative to the Primary and Secondary Side Bars.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.statusBar.visible", Types: []Type{Boolean}, Default: true,
			Description: "Controls the visibility of the status bar at the bottom of the workbench.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.panel.defaultLocation", Types: []Type{String}, Default: "bottom", Enum: []any{"left", "bottom", "top", "right"},
			Description: "Controls the default location of the panel (Terminal, Debug Console, Output, Problems) in a new workspace. It can either show at the bottom, top, right, or left of the editor area.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.panel.opensMaximized", Types: []Type{String}, Default: "preserve", Enum: []any{"always", "never", "preserve"},
			EnumDescriptions: []string{
				"Always maximize the panel when opening it.",
				"Never maximize the panel when opening it.",
				"Open the panel to the state that it was in, before it was closed.",
			},
			Description: "Controls whether the panel opens maximized. It can either always open maximized, never open maximized, or open to the last state it was in before being closed.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "window.title", Types: []Type{String}, Default: DefaultWindowTitle,
			Description: windowTitleDescription,
			Scope:       ScopeWindow, Category: categoryWindow,
		},
		Setting{
			Key: "window.titleSeparator", Types: []Type{String}, Default: DefaultWindowTitleSeparator,
			Description: "Separator used by `window.title`.",
			Scope:       ScopeWindow, Category: categoryWindow,
		},
	)
}

// registerEditorGroups registers the settings of the editor groups and their tabs (SPEC §10.3).
// workbench.editor.tabCloseButton is not registered by VS Code any more: it migrates it to
// workbench.editor.tabActionLocation (left, right) and workbench.editor.tabActionCloseVisibility
// (off); Emetos registers the three and honors all of them. Not ported yet: tab sizing, wrapping
// tabs, the tab index, the separate row of pinned tabs and the settings of the title area that
// step 6.10 adds; the grid's own settings are here: openSideBySideDirection, closeEmptyGroups and
// splitSizing.
//
// Ported from VS Code: src/vs/workbench/browser/workbench.contribution.ts
func registerEditorGroups(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "workbench.editor.showTabs", Types: []Type{String}, Default: "multiple", Enum: []any{"multiple", "single", "none"},
			EnumDescriptions: []string{
				"Each editor is displayed as a tab in the editor title area.",
				"The active editor is displayed as a single large tab in the editor title area.",
				"The editor title area is not displayed.",
			},
			Description: "Controls whether opened editors should show as individual tabs, one single large tab or if the title area should not be shown.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.editor.enablePreview", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether preview mode is used when editors open. There is a maximum of one preview mode editor per editor group. This editor displays its filename in italics on its tab or title label and in the Open Editors view. Its contents will be replaced by the next editor opened in preview mode. Making a change in a preview mode editor will persist it, as will a double-click on its label, or the 'Keep Open' option in its label context menu. Opening a file from Explorer with a double-click persists its editor immediately.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.editor.focusRecentEditorAfterClose", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether editors are closed in most recently used order or from left to right.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.editor.openPositioning", Types: []Type{String}, Default: "right", Enum: []any{"left", "right", "first", "last"},
			Description: "Controls where editors open. Select `left` or `right` to open editors to the left or right of the currently active one. Select `first` or `last` to open editors independently from the currently active one.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.editor.tabCloseButton", Types: []Type{String}, Default: "right", Enum: []any{"left", "right", "off"},
			Description: "Controls the position of the editor's tabs close buttons, or disables them when set to 'off'. This value is ignored when `workbench.editor.showTabs` is not set to `multiple`. Deprecated in VS Code in favor of `workbench.editor.tabActionLocation` and `workbench.editor.tabActionCloseVisibility`; both are honored.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.editor.tabActionLocation", Types: []Type{String}, Default: "right", Enum: []any{"left", "right"},
			Description: "Controls the position of the editor's tabs action buttons (close, unpin). This value is ignored when `workbench.editor.showTabs` is not set to `multiple`.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.editor.tabActionCloseVisibility", Types: []Type{Boolean}, Default: true,
			Description: "Controls the visibility of the tab close action button.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.editor.pinnedTabSizing", Types: []Type{String}, Default: "normal", Enum: []any{"normal", "compact", "shrink"},
			EnumDescriptions: []string{
				"A pinned tab inherits the look of non pinned tabs.",
				"A pinned tab will show in a compact form with only icon or first letter of the editor name.",
				"A pinned tab shrinks to a compact fixed size showing parts of the editor name.",
			},
			Description: "Controls the size of pinned editor tabs. Pinned tabs are sorted to the beginning of all opened tabs and typically do not close until unpinned. This value is ignored when `workbench.editor.showTabs` is not set to `multiple`.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.editor.highlightModifiedTabs", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether a top border is drawn on tabs for editors that have unsaved changes. This value is ignored when `workbench.editor.showTabs` is not set to `multiple`.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.editor.openSideBySideDirection", Types: []Type{String}, Default: "right", Enum: []any{"right", "down"},
			Description: "Controls the default direction of editors that are opened side by side (for example, from the Explorer). By default, editors will open on the right hand side of the currently active one. If changed to `down`, the editors will open below the currently active one. This also impacts the split editor action in the editor toolbar.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.editor.closeEmptyGroups", Types: []Type{Boolean}, Default: true,
			Description: "Controls the behavior of empty editor groups when the last tab in the group is closed. When enabled, empty groups will automatically close. When disabled, empty groups will remain part of the grid.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
		Setting{
			Key: "workbench.editor.splitSizing", Types: []Type{String}, Default: "auto", Enum: []any{"auto", "distribute", "split"},
			EnumDescriptions: []string{
				"Splits the active editor group to equal parts, unless all editor groups are already in equal parts. In that case, splits all the editor groups to equal parts.",
				"Splits all the editor groups to equal parts.",
				"Splits the active editor group to equal parts.",
			},
			Description: "Controls the size of editor groups when splitting them.",
			Scope:       ScopeWindow, Category: categoryWorkbench,
		},
	)
}

// registerEmmet registers the emmet.* settings that the abbreviation expansion of core/emmet reads
// (SPEC sections 9.10.1 and 9.10.2).
//
// Ported from VS Code: extensions/emmet/package.json and extensions/emmet/package.nls.json
func registerEmmet(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "emmet.variables", Types: []Type{Object}, Default: map[string]any{},
			Description: "Variables to be used in Emmet snippets.",
			Scope:       ScopeWindow, Category: categoryEmmet,
		},
		Setting{
			Key: "emmet.syntaxProfiles", Types: []Type{Object}, Default: map[string]any{},
			Description: "Define profile for specified syntax or use your own profile with specific rules.",
			Scope:       ScopeWindow, Category: categoryEmmet,
		},
		Setting{
			Key: "emmet.includeLanguages", Types: []Type{Object}, Default: map[string]any{},
			Description: "Enable Emmet abbreviations in languages that are not supported by default. Add a mapping here between the language and Emmet supported language.\n For example: `{\"vue-html\": \"html\", \"javascript\": \"javascriptreact\"}`",
			Scope:       ScopeWindow, Category: categoryEmmet,
		},
		Setting{
			Key: "emmet.excludeLanguages", Types: []Type{Array}, ItemTypes: []Type{String}, Default: []any{"markdown"},
			Description: "An array of languages where Emmet abbreviations should not be expanded.",
			Scope:       ScopeWindow, Category: categoryEmmet,
		},
		Setting{
			Key: "emmet.triggerExpansionOnTab", Types: []Type{Boolean}, Default: false,
			Description: "When enabled, Emmet abbreviations are expanded when pressing TAB, even when completions do not show up. When disabled, completions that show up can still be accepted by pressing TAB.",
			Scope:       ScopeLanguageOverridable, Category: categoryEmmet,
		},
	)
}
