package editor

import (
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/theme"
)

// OptionsFromSettings reads the options of an editor from the settings for a language.
// desktopMonospace is the desktop's monospace font (PlatformDispatcher.MonospaceFont), tried
// when editor.fontFamily is empty; tabSize is the document's (its indentation settings are
// applied to the document, not read here).
//
// The font families are those of editor.fontFamily in order, then the desktop's monospace
// font when the setting is empty, then the generic monospace family (SPEC D14).
func OptionsFromSettings(store *settings.Store, languageID, desktopMonospace string, tabSize int) Options {
	families := FontFamilies(settings.Get[string](store, "editor.fontFamily", languageID))
	if len(families) == 0 && desktopMonospace != "" {
		families = append(families, desktopMonospace)
	}
	families = append(families, "monospace")
	fontSize := settings.Get[float64](store, "editor.fontSize", languageID)
	return Options{
		FontFamilies:         families,
		FontSize:             fontSize,
		FontWeight:           ParseFontWeight(settings.Get[string](store, "editor.fontWeight", languageID)),
		LetterSpacing:        settings.Get[float64](store, "editor.letterSpacing", languageID),
		LineHeight:           LineHeight(settings.Get[float64](store, "editor.lineHeight", languageID), fontSize),
		TabSize:              max(tabSize, 1),
		FontFeatures:         fontFeatures(store, languageID),
		LineNumbers:          ParseLineNumbersMode(settings.Get[string](store, "editor.lineNumbers", languageID)),
		LineNumbersMinChars:  int(settings.Get[float64](store, "editor.lineNumbersMinChars", languageID)),
		GlyphMargin:          settings.Get[bool](store, "editor.glyphMargin", languageID),
		LineDecorationsWidth: lineDecorationsWidth(store, languageID),
		Folding:              settings.Get[bool](store, "editor.folding", languageID),
		ScrollBeyondLastLine: settings.Get[bool](store, "editor.scrollBeyondLastLine", languageID),

		ShowFoldingControls:         settings.Get[string](store, "editor.showFoldingControls", languageID),
		FoldingHighlight:            settings.Get[bool](store, "editor.foldingHighlight", languageID),
		UnfoldOnClickAfterEndOfLine: settings.Get[bool](store, "editor.unfoldOnClickAfterEndOfLine", languageID),

		RenderLineHighlight:              settings.Get[string](store, "editor.renderLineHighlight", languageID),
		RenderLineHighlightOnlyWhenFocus: settings.Get[bool](store, "editor.renderLineHighlightOnlyWhenFocus", languageID),
		RoundedSelection:                 settings.Get[bool](store, "editor.roundedSelection", languageID),
		CursorWidth:                      settings.Get[float64](store, "editor.cursorWidth", languageID),
		CursorBlinking:                   settings.Get[string](store, "editor.cursorBlinking", languageID),
		CursorStyle:                      settings.Get[string](store, "editor.cursorStyle", languageID),
		OvertypeCursorStyle:              settings.Get[string](store, "editor.overtypeCursorStyle", languageID),
		OvertypeOnPaste:                  settings.Get[bool](store, "editor.overtypeOnPaste", languageID),
		MouseWheelZoom:                   settings.Get[bool](store, "editor.mouseWheelZoom", languageID),
		MultiCursorModifier:              settings.Get[string](store, "editor.multiCursorModifier", languageID),
		ColumnSelection:                  settings.Get[bool](store, "editor.columnSelection", languageID),
		MouseStyle:                       settings.Get[string](store, "editor.mouseStyle", languageID),
		MouseWheelScrollSensitivity:      settings.Get[float64](store, "editor.mouseWheelScrollSensitivity", languageID),
		FastScrollSensitivity:            settings.Get[float64](store, "editor.fastScrollSensitivity", languageID),
		SmoothScrolling:                  settings.Get[bool](store, "editor.smoothScrolling", languageID),
		DragAndDrop:                      settings.Get[bool](store, "editor.dragAndDrop", languageID),
		SelectionClipboard:               settings.Get[bool](store, "editor.selectionClipboard", languageID),
		MultiCursorPaste:                 settings.Get[string](store, "editor.multiCursorPaste", languageID),
		TabCompletion:                    settings.Get[string](store, "editor.tabCompletion", languageID),
		Emmet: EmmetOptions{
			TriggerExpansionOnTab: settings.Get[bool](store, "emmet.triggerExpansionOnTab", languageID),
			IncludeLanguages:      settings.Get[any](store, "emmet.includeLanguages", languageID),
			ExcludeLanguages:      settings.Get[any](store, "emmet.excludeLanguages", languageID),
			Variables:             settings.Get[any](store, "emmet.variables", languageID),
			SyntaxProfiles:        settings.Get[any](store, "emmet.syntaxProfiles", languageID),
		},
		EmptySelectionClipboard:       settings.Get[bool](store, "editor.emptySelectionClipboard", languageID),
		MatchBrackets:                 settings.Get[string](store, "editor.matchBrackets", languageID),
		BracketPairColorization:       settings.Get[bool](store, "editor.bracketPairColorization.enabled", languageID),
		IndependentColorPool:          settings.Get[bool](store, "editor.bracketPairColorization.independentColorPoolPerBracketType", languageID),
		Guides:                        guideOptions(store, languageID),
		AutoIndent:                    settings.Get[string](store, "editor.autoIndent", languageID),
		AutoIndentOnPaste:             settings.Get[bool](store, "editor.autoIndentOnPaste", languageID),
		AutoIndentOnPasteWithinString: settings.Get[bool](store, "editor.autoIndentOnPasteWithinString", languageID),
		AutoClosingBrackets:           settings.Get[string](store, "editor.autoClosingBrackets", languageID),
		AutoClosingComments:           settings.Get[string](store, "editor.autoClosingComments", languageID),
		AutoClosingQuotes:             settings.Get[string](store, "editor.autoClosingQuotes", languageID),
		AutoClosingDelete:             settings.Get[string](store, "editor.autoClosingDelete", languageID),
		AutoClosingOvertype:           settings.Get[string](store, "editor.autoClosingOvertype", languageID),
		AutoSurround:                  settings.Get[string](store, "editor.autoSurround", languageID),
		CommentsInsertSpace:           settings.Get[bool](store, "editor.comments.insertSpace", languageID),
		CommentsIgnoreEmptyLines:      settings.Get[bool](store, "editor.comments.ignoreEmptyLines", languageID),
		WordSeparators:                settings.Get[string](store, "editor.wordSeparators", languageID),
		WordWrap:                      settings.Get[string](store, "editor.wordWrap", languageID),
		WordWrapColumn:                int(settings.Get[float64](store, "editor.wordWrapColumn", languageID)),
		WrappingIndent:                settings.Get[string](store, "editor.wrappingIndent", languageID),
		WrappingStrategy:              settings.Get[string](store, "editor.wrappingStrategy", languageID),
		StopRenderingLineAfter:        int(settings.Get[float64](store, "editor.stopRenderingLineAfter", languageID)),
		RenderWhitespace:              settings.Get[string](store, "editor.renderWhitespace", languageID),
		RenderControlCharacters:       settings.Get[bool](store, "editor.renderControlCharacters", languageID),
		Rulers:                        rulers(settings.Get[[]any](store, "editor.rulers", languageID)),
		OccurrencesHighlight:          settings.Get[string](store, "editor.occurrencesHighlight", languageID),
		OccurrencesHighlightDelay:     int(settings.Get[float64](store, "editor.occurrencesHighlightDelay", languageID)),
		SelectionHighlight:            settings.Get[bool](store, "editor.selectionHighlight", languageID),
		SelectionHighlightMaxLength:   int(settings.Get[float64](store, "editor.selectionHighlightMaxLength", languageID)),
		SelectionHighlightMultiline:   settings.Get[bool](store, "editor.selectionHighlightMultiline", languageID),
		ShowUnused:                    settings.Get[bool](store, "editor.showUnused", languageID),
		ShowDeprecated:                settings.Get[bool](store, "editor.showDeprecated", languageID),
		Hover: HoverOptions{
			Enabled: settings.Get[string](store, "editor.hover.enabled", languageID), Delay: int(settings.Get[float64](store, "editor.hover.delay", languageID)),
			Sticky: settings.Get[bool](store, "editor.hover.sticky", languageID), HidingDelay: int(settings.Get[float64](store, "editor.hover.hidingDelay", languageID)),
			Above: settings.Get[bool](store, "editor.hover.above", languageID),
		},
		ParameterHints: ParameterHintsOptions{
			Enabled: settings.Get[bool](store, "editor.parameterHints.enabled", languageID), Cycle: settings.Get[bool](store, "editor.parameterHints.cycle", languageID),
		},
		CodeActions: CodeActionOptions{
			Lightbulb:               settings.Get[string](store, "editor.lightbulb.enabled", languageID),
			ShowHeaders:             settings.Get[bool](store, "editor.codeActionWidget.showHeaders", languageID),
			IncludeNearbyQuickFixes: settings.Get[bool](store, "editor.codeActionWidget.includeNearbyQuickFixes", languageID),
		},
		Format: FormatOptions{
			OnType:           settings.Get[bool](store, "editor.formatOnType", languageID),
			OnPaste:          settings.Get[bool](store, "editor.formatOnPaste", languageID),
			DefaultFormatter: settings.Get[string](store, "editor.defaultFormatter", languageID),
		},
		RenamePreview: settings.Get[bool](store, "editor.rename.enablePreview", languageID),
		Suggest:       suggestOptions(store, languageID),
		StickyScroll: StickyScrollOptions{
			Enabled:          settings.Get[bool](store, "editor.stickyScroll.enabled", languageID),
			MaxLineCount:     int(settings.Get[float64](store, "editor.stickyScroll.maxLineCount", languageID)),
			ScrollWithEditor: settings.Get[bool](store, "editor.stickyScroll.scrollWithEditor", languageID),
			DefaultModel:     settings.Get[string](store, "editor.stickyScroll.defaultModel", languageID),
		},
		GotoLocation: gotoLocationOptions(store, languageID),
		Lens:         lensOptions(store, languageID),

		FoldingStrategy:         settings.Get[string](store, "editor.foldingStrategy", languageID),
		FoldingImportsByDefault: settings.Get[bool](store, "editor.foldingImportsByDefault", languageID),
		Minimap: MinimapOptions{
			Enabled: settings.Get[bool](store, "editor.minimap.enabled", languageID), Side: settings.Get[string](store, "editor.minimap.side", languageID),
			Size: settings.Get[string](store, "editor.minimap.size", languageID), ShowSlider: settings.Get[string](store, "editor.minimap.showSlider", languageID),
			Scale: int(settings.Get[float64](store, "editor.minimap.scale", languageID)), MaxColumn: int(settings.Get[float64](store, "editor.minimap.maxColumn", languageID)),
		},

		FindSeedSearchStringFromSelection: settings.Get[string](store, "editor.find.seedSearchStringFromSelection", languageID),
		FindAutoInSelection:               settings.Get[string](store, "editor.find.autoFindInSelection", languageID),
		FindCloseOnResult:                 settings.Get[bool](store, "editor.find.closeOnResult", languageID),
		FindCursorMoveOnType:              settings.Get[bool](store, "editor.find.cursorMoveOnType", languageID),
		FindLoop:                          settings.Get[bool](store, "editor.find.loop", languageID),
	}
}

// ParseFontWeight reads editor.fontWeight: "normal" and "bold" are 400 and 700, a number from 1
// to 1000 is itself, and anything else is normal.
func ParseFontWeight(value string) int {
	switch value = strings.TrimSpace(value); value {
	case "", "normal":
		return 400
	case "bold":
		return 700
	}
	if n, err := strconv.Atoi(value); err == nil && n >= 1 && n <= 1000 {
		return n
	}
	return 400
}

// FontFamilies splits the value of editor.fontFamily: comma-separated names, each trimmed
// and without the quotes around a name with spaces.
func FontFamilies(value string) []string {
	var out []string
	for _, name := range strings.Split(value, ",") {
		name = strings.Trim(strings.TrimSpace(name), `'"`)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// lineDecorationsWidth reads editor.lineDecorationsWidth, a number or a string such as "10"
// or "1ch" (only the number of pixels is understood).
func lineDecorationsWidth(store *settings.Store, languageID string) float64 {
	raw := settings.Get[any](store, "editor.lineDecorationsWidth", languageID)
	switch v := raw.(type) {
	case float64:
		return max(v, 0)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), "px"), 64); err == nil {
			return max(f, 0)
		}
	}
	return 10
}

// fontFeatures reads editor.fontLigatures: false turns the ligature features off, true
// leaves the font's own, and a string is the value of CSS font-feature-settings.
func fontFeatures(store *settings.Store, languageID string) []FontFeature {
	switch v := settings.Get[any](store, "editor.fontLigatures", languageID).(type) {
	case bool:
		if v {
			return nil
		}
	case string:
		if features := ParseFontFeatureSettings(v); features != nil {
			return features
		}
	}
	return []FontFeature{{"liga", 0}, {"calt", 0}}
}

// ParseFontFeatureSettings reads a CSS font-feature-settings value such as
// `"liga" 0, 'ss01', "calt" off`: a quoted tag, then an optional integer, "on" or "off"
// (on is the default). Entries that are not understood are skipped; nil means none were.
func ParseFontFeatureSettings(s string) []FontFeature {
	var out []FontFeature
	for _, entry := range strings.Split(s, ",") {
		fields := strings.Fields(entry)
		if len(fields) == 0 || len(fields) > 2 {
			continue
		}
		tag := strings.Trim(fields[0], `'"`)
		if len(tag) != 4 {
			continue
		}
		value := 1
		if len(fields) == 2 {
			switch fields[1] {
			case "on":
			case "off":
				value = 0
			default:
				n, err := strconv.Atoi(fields[1])
				if err != nil || n < 0 {
					continue
				}
				value = n
			}
		}
		out = append(out, FontFeature{tag, value})
	}
	return out
}

// guideOptions reads editor.guides.*. A value that is not one of VS Code's is its default.
//
// Ported from VS Code: src/vs/editor/common/config/editorOptions.ts (GuideOptions.validate)
func guideOptions(store *settings.Store, languageID string) indent.GuideOptions {
	opts := indent.DefaultGuideOptions()
	opts.Indentation = settings.Get[bool](store, "editor.guides.indentation", languageID)
	opts.HighlightActiveBracketPair = settings.Get[bool](store, "editor.guides.highlightActiveBracketPair", languageID)
	opts.IndependentColorPool = settings.Get[bool](store, "editor.bracketPairColorization.independentColorPoolPerBracketType", languageID)
	switch v := settings.Get[any](store, "editor.guides.highlightActiveIndentation", languageID); v {
	case true:
		opts.HighlightActiveIndentation = indent.HighlightOn
	case false:
		opts.HighlightActiveIndentation = indent.HighlightOff
	case "always":
		opts.HighlightActiveIndentation = indent.HighlightAlways
	}
	switch v := settings.Get[any](store, "editor.guides.bracketPairs", languageID); v {
	case true:
		opts.BracketPairs = indent.PairsAll
	case "active":
		opts.BracketPairs = indent.PairsActive
	case false:
		opts.BracketPairs = indent.PairsOff
	}
	switch v := settings.Get[any](store, "editor.guides.bracketPairsHorizontal", languageID); v {
	case true:
		opts.BracketPairsHorizontal = indent.HorizontalAll
	case "active":
		opts.BracketPairsHorizontal = indent.HorizontalActive
	case false:
		opts.BracketPairsHorizontal = indent.HorizontalNone
	}
	return opts
}

// rulers reads editor.rulers: each element is a column, or an object with a column and a color.
// An element that is neither is skipped, a column is a whole number from 0 to 10,000, and the
// rulers are sorted by column.
//
// Ported from VS Code: src/vs/editor/common/config/editorOptions.ts (EditorRulers.validate)
func rulers(raw []any) []ColumnRuler {
	var out []ColumnRuler
	for _, v := range raw {
		switch e := v.(type) {
		case float64:
			out = append(out, ColumnRuler{Column: clampColumn(e)})
		case map[string]any:
			r := ColumnRuler{}
			if col, ok := e["column"].(float64); ok {
				r.Column = clampColumn(col)
			}
			if text, ok := e["color"].(string); ok {
				if c, ok := theme.ParseColor(text); ok {
					r.Color = ui.NewColor(c.ARGB())
				}
			}
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b ColumnRuler) int { return a.Column - b.Column })
	return out
}

func clampColumn(f float64) int { return min(max(int(f), 0), 10000) }

// suggestOptions reads the settings of the suggest widget.
func suggestOptions(store *settings.Store, languageID string) SuggestOptions {
	get := func(key string) bool { return settings.Get[bool](store, "editor.suggest."+key, languageID) }
	o := SuggestOptions{
		QuickSuggestions:                  ParseQuickSuggestions(settings.Get[any](store, "editor.quickSuggestions", languageID)),
		QuickSuggestionsDelay:             int(settings.Get[float64](store, "editor.quickSuggestionsDelay", languageID)),
		SuggestOnTriggerCharacters:        settings.Get[bool](store, "editor.suggestOnTriggerCharacters", languageID),
		AcceptSuggestionOnEnter:           settings.Get[string](store, "editor.acceptSuggestionOnEnter", languageID),
		AcceptSuggestionOnCommitCharacter: settings.Get[bool](store, "editor.acceptSuggestionOnCommitCharacter", languageID),
		SuggestSelection:                  settings.Get[string](store, "editor.suggestSelection", languageID),
		SnippetSuggestions:                settings.Get[string](store, "editor.snippetSuggestions", languageID),
		WordBasedSuggestions:              settings.Get[string](store, "editor.wordBasedSuggestions", languageID),
		FontSize:                          int(settings.Get[float64](store, "editor.suggestFontSize", languageID)),
		LineHeight:                        int(settings.Get[float64](store, "editor.suggestLineHeight", languageID)),
		InsertMode:                        settings.Get[string](store, "editor.suggest.insertMode", languageID),
		FilterGraceful:                    get("filterGraceful"),
		LocalityBonus:                     get("localityBonus"),
		SelectionMode:                     settings.Get[string](store, "editor.suggest.selectionMode", languageID),
		SnippetsPreventQuickSuggestions:   get("snippetsPreventQuickSuggestions"),
		ShowIcons:                         get("showIcons"),
		ShowStatusBar:                     get("showStatusBar"),
		Preview:                           get("preview"),
		ShowInlineDetails:                 get("showInlineDetails"),
		ShowDeprecated:                    get("showDeprecated"),
		MatchOnWordStartOnly:              get("matchOnWordStartOnly"),
	}
	for _, k := range settings.SuggestKinds {
		if !get("show" + k.Name) {
			for _, kind := range k.Kinds {
				if o.HiddenKinds == nil {
					o.HiddenKinds = map[int]bool{}
				}
				o.HiddenKinds[kind] = true
			}
		}
	}
	return o
}

// gotoLocationOptions reads editor.gotoLocation.* and editor.definitionLinkOpensInPeek.
func gotoLocationOptions(store *settings.Store, languageID string) GotoLocationOptions {
	o := GotoLocationOptions{DefinitionLinkOpensInPeek: settings.Get[bool](store, "editor.definitionLinkOpensInPeek", languageID)}
	for i, name := range [goKinds]string{"Definition", "Declaration", "TypeDefinition", "Implementation", "Reference"} {
		multiple := name + "s"
		if name == "Reference" {
			multiple = "References"
		}
		o.Multiple[i] = settings.Get[string](store, "editor.gotoLocation.multiple"+multiple, languageID)
		o.Alternative[i] = settings.Get[string](store, "editor.gotoLocation.alternative"+name+"Command", languageID)
	}
	return o
}
