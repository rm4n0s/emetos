package editor

import (
	"context"
	"github.com/rm4n0s/glutter/ui"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func TestFontFamilies(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"Go Mono", []string{"Go Mono"}},
		{"'Droid Sans Mono', \"monospace\" , monospace", []string{"Droid Sans Mono", "monospace", "monospace"}},
		{" , ,", nil},
	}
	for _, tc := range cases {
		if got := FontFamilies(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("FontFamilies(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseFontFeatureSettings(t *testing.T) {
	cases := []struct {
		in   string
		want []FontFeature
	}{
		{`"liga" 0`, []FontFeature{{"liga", 0}}},
		{`'ss01', "calt" off, "dlig" on`, []FontFeature{{"ss01", 1}, {"calt", 0}, {"dlig", 1}}},
		{`"zero" 3`, []FontFeature{{"zero", 3}}},
		{`"toolong" 1, "x" 1, "liga" -1, "calt" nope, "a b c d"`, nil},
		{``, nil},
	}
	for _, tc := range cases {
		if got := ParseFontFeatureSettings(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("ParseFontFeatureSettings(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func newSettings(t *testing.T, extra map[string]any) *settings.Store {
	t.Helper()
	return testutil.Settings(t, testutil.Isolate(t), extra)
}

func TestOptionsFromSettingsDefaults(t *testing.T) {
	// A store with nothing set: the registered defaults.
	store := settings.NewStore(settings.DefaultRegistry(), settings.StoreOptions{})
	got := OptionsFromSettings(store, "go", "DejaVu Sans Mono", 8)
	if !slices.Equal(got.FontFamilies, []string{"DejaVu Sans Mono", "monospace"}) {
		t.Errorf("an empty editor.fontFamily is the desktop's monospace font, then monospace: %v", got.FontFamilies)
	}
	want := Options{
		FontFamilies: got.FontFamilies, FontSize: 14, LineHeight: 19, TabSize: 8,
		FontFeatures: []FontFeature{{"liga", 0}, {"calt", 0}},
		LineNumbers:  LineNumbersOn, LineNumbersMinChars: 5, GlyphMargin: true, LineDecorationsWidth: 10, Folding: true, ScrollBeyondLastLine: true,
		ShowFoldingControls: "mouseover", FoldingHighlight: true,
		RenderLineHighlight: "line", RoundedSelection: true, CursorBlinking: "blink",
		MultiCursorModifier: "alt", MouseStyle: "text",
		MouseWheelScrollSensitivity: 1, FastScrollSensitivity: 5, DragAndDrop: true, SelectionClipboard: true,
		MultiCursorPaste: "spread", EmptySelectionClipboard: true, TabCompletion: "off",
		Emmet:      EmmetOptions{IncludeLanguages: map[string]any{}, ExcludeLanguages: []any{"markdown"}, Variables: map[string]any{}, SyntaxProfiles: map[string]any{}},
		FontWeight: 400, CursorStyle: "line", OvertypeCursorStyle: "block", OvertypeOnPaste: true,
		MatchBrackets: "always", BracketPairColorization: true,
		AutoIndent: "full", AutoIndentOnPasteWithinString: true, Guides: indent.DefaultGuideOptions(),
		AutoClosingBrackets: "languageDefined", AutoClosingComments: "languageDefined", AutoClosingQuotes: "languageDefined",
		AutoClosingDelete: "auto", AutoClosingOvertype: "auto", AutoSurround: "languageDefined",
		CommentsInsertSpace: true, CommentsIgnoreEmptyLines: true,
		WordSeparators:                    edit.DefaultWordSeparators,
		FindSeedSearchStringFromSelection: "always", FindAutoInSelection: "never",
		FindCursorMoveOnType: true, FindLoop: true,
		WordWrap: "off", WordWrapColumn: 80, WrappingIndent: "same", WrappingStrategy: "simple", StopRenderingLineAfter: 10000,
		RenderWhitespace: "selection", RenderControlCharacters: true,
		OccurrencesHighlight: "singleFile", OccurrencesHighlightDelay: 250, SelectionHighlight: true, SelectionHighlightMaxLength: 200,
		ShowUnused: true, ShowDeprecated: true,
		Hover: HoverOptions{Enabled: "on", Delay: 300, Sticky: true, HidingDelay: 300, Above: true},
		Suggest: SuggestOptions{
			QuickSuggestions: QuickSuggestions{Other: "offWhenInlineCompletions", Comments: "off", Strings: "off"}, QuickSuggestionsDelay: 10,
			SuggestOnTriggerCharacters: true, AcceptSuggestionOnEnter: "on", AcceptSuggestionOnCommitCharacter: true, SuggestSelection: "first",
			SnippetSuggestions: "inline", WordBasedSuggestions: "offWithInlineSuggestions", InsertMode: "insert", FilterGraceful: true,
			ShowIcons: true, ShowInlineDetails: true, ShowDeprecated: true, MatchOnWordStartOnly: true, SelectionMode: "always",
		},
		Minimap:      MinimapOptions{Enabled: true, Side: "right", Size: "proportional", ShowSlider: "mouseover", Scale: 1, MaxColumn: 120},
		StickyScroll: StickyScrollOptions{Enabled: true, MaxLineCount: 5, ScrollWithEditor: true},
	}
	if !sameOptions(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if without := OptionsFromSettings(store, "", "", 0); !slices.Equal(without.FontFamilies, []string{"monospace"}) || without.TabSize != 1 {
		t.Errorf("no desktop font and a tab size of 0: %+v", without)
	}
}

func TestOptionsFromSettingsValues(t *testing.T) {
	store := newSettings(t, map[string]any{
		"editor.fontSize":                    18.0,
		"editor.lineHeight":                  1.5,
		"editor.lineNumbers":                 "relative",
		"editor.lineNumbersMinChars":         3.0,
		"editor.glyphMargin":                 false,
		"editor.lineDecorationsWidth":        "12px",
		"editor.folding":                     false,
		"editor.showFoldingControls":         "never",
		"editor.foldingHighlight":            false,
		"editor.unfoldOnClickAfterEndOfLine": true,
		"editor.scrollBeyondLastLine":        false,
		"editor.fontLigatures":               true,

		"editor.renderLineHighlight":              "all",
		"editor.renderLineHighlightOnlyWhenFocus": true,
		"editor.roundedSelection":                 false,
		"editor.cursorWidth":                      3.0,
		"editor.cursorBlinking":                   "solid",
		"editor.multiCursorModifier":              "ctrlCmd",
		"editor.columnSelection":                  true,
		"editor.mouseStyle":                       "copy",
		"editor.mouseWheelScrollSensitivity":      2.5,
		"editor.fastScrollSensitivity":            9.0,
		"editor.smoothScrolling":                  true,
		"editor.dragAndDrop":                      false,
		"editor.selectionClipboard":               false,
		"editor.multiCursorPaste":                 "full",
		"editor.emptySelectionClipboard":          false,
		"editor.fontWeight":                       "bold",
		"editor.letterSpacing":                    1.5,
		"editor.cursorStyle":                      "block-outline",
		"editor.mouseWheelZoom":                   true,

		"editor.matchBrackets":                                              "near",
		"editor.bracketPairColorization.enabled":                            false,
		"editor.bracketPairColorization.independentColorPoolPerBracketType": true,

		"editor.guides.indentation":                false,
		"editor.guides.highlightActiveIndentation": "always",
		"editor.guides.bracketPairs":               "active",
		"editor.guides.bracketPairsHorizontal":     false,
		"editor.guides.highlightActiveBracketPair": false,
		"editor.autoIndent":                        "brackets",
		"editor.autoClosingBrackets":               "beforeWhitespace",
		"editor.autoClosingComments":               "always",
		"editor.autoClosingQuotes":                 "never",
		"editor.autoClosingDelete":                 "always",
		"editor.autoClosingOvertype":               "never",
		"editor.autoSurround":                      "quotes",
		"editor.comments.insertSpace":              false,
		"editor.comments.ignoreEmptyLines":         false,
		"editor.autoIndentOnPaste":                 true,
		"editor.autoIndentOnPasteWithinString":     false,
		"editor.overtypeCursorStyle":               "underline",
		"editor.overtypeOnPaste":                   false,

		"editor.renderWhitespace":            "boundary",
		"editor.renderControlCharacters":     false,
		"editor.rulers":                      []any{80.0},
		"editor.occurrencesHighlight":        "off",
		"editor.occurrencesHighlightDelay":   500.0,
		"editor.selectionHighlight":          false,
		"editor.selectionHighlightMaxLength": 20.0,
		"editor.selectionHighlightMultiline": true,

		"editor.minimap.enabled": false, "editor.minimap.side": "left", "editor.minimap.size": "fit",
		"editor.minimap.showSlider": "always", "editor.minimap.scale": 2.0, "editor.minimap.maxColumn": 80.0,

		"editor.stickyScroll.enabled": false, "editor.stickyScroll.maxLineCount": 3.0, "editor.stickyScroll.scrollWithEditor": false,
	})
	got := OptionsFromSettings(store, "", "ignored", 2)
	if !slices.Equal(got.FontFamilies, []string{testutil.MonoFamily, "monospace"}) {
		t.Errorf("families: %v", got.FontFamilies)
	}
	if got.FontSize != 18 || got.LineHeight != 27 || got.TabSize != 2 || got.LineNumbers != LineNumbersRelative ||
		got.LineNumbersMinChars != 3 || got.GlyphMargin || got.LineDecorationsWidth != 12 || got.Folding || got.ScrollBeyondLastLine ||
		got.ShowFoldingControls != "never" || got.FoldingHighlight || !got.UnfoldOnClickAfterEndOfLine {
		t.Errorf("got %+v", got)
	}
	if got.RenderLineHighlight != "all" || !got.RenderLineHighlightOnlyWhenFocus || got.RoundedSelection || got.CursorWidth != 3 || got.CursorBlinking != "solid" {
		t.Errorf("cursor and highlight options: %+v", got)
	}
	if got.MultiCursorModifier != "ctrlCmd" || !got.ColumnSelection || got.MouseStyle != "copy" {
		t.Errorf("mouse options: %+v", got)
	}
	if got.MatchBrackets != "near" || got.BracketPairColorization || !got.IndependentColorPool {
		t.Errorf("bracket options: %+v", got)
	}
	wantGuides := indent.GuideOptions{
		Indentation: false, HighlightActiveIndentation: indent.HighlightAlways, BracketPairs: indent.PairsActive,
		BracketPairsHorizontal: indent.HorizontalNone, HighlightActiveBracketPair: false, IndependentColorPool: true,
	}
	if got.Guides != wantGuides {
		t.Errorf("guide options: %+v, want %+v", got.Guides, wantGuides)
	}
	if got.AutoIndent != "brackets" || !got.AutoIndentOnPaste || got.AutoIndentOnPasteWithinString {
		t.Errorf("auto-indent options: %+v", got)
	}
	if got.AutoClosingBrackets != "beforeWhitespace" || got.AutoClosingComments != "always" || got.AutoClosingQuotes != "never" ||
		got.AutoClosingDelete != "always" || got.AutoClosingOvertype != "never" || got.AutoSurround != "quotes" {
		t.Errorf("auto-closing options: %+v", got)
	}
	if got.OvertypeCursorStyle != "underline" || got.OvertypeOnPaste {
		t.Errorf("overtype options: %q, %v", got.OvertypeCursorStyle, got.OvertypeOnPaste)
	}
	if got.CommentsInsertSpace || got.CommentsIgnoreEmptyLines {
		t.Errorf("comment options: %+v", got)
	}
	if got.MouseWheelScrollSensitivity != 2.5 || got.FastScrollSensitivity != 9 || !got.SmoothScrolling || got.DragAndDrop || got.SelectionClipboard ||
		got.MultiCursorPaste != "full" || got.EmptySelectionClipboard {
		t.Errorf("scroll and drag options: %+v", got)
	}
	if got.FontWeight != 700 || got.LetterSpacing != 1.5 || got.CursorStyle != "block-outline" || !got.MouseWheelZoom {
		t.Errorf("font and cursor options: %+v", got)
	}
	if got.RenderWhitespace != "boundary" || got.RenderControlCharacters || !slices.Equal(got.Rulers, []ColumnRuler{{Column: 80}}) {
		t.Errorf("whitespace and rulers: %+v", got)
	}
	if got.OccurrencesHighlight != "off" || got.OccurrencesHighlightDelay != 500 || got.SelectionHighlight ||
		got.SelectionHighlightMaxLength != 20 || !got.SelectionHighlightMultiline {
		t.Errorf("highlight options: %+v", got)
	}
	if want := (MinimapOptions{Side: "left", Size: "fit", ShowSlider: "always", Scale: 2, MaxColumn: 80}); got.Minimap != want {
		t.Errorf("minimap options: %+v, want %+v", got.Minimap, want)
	}
	if want := (StickyScrollOptions{MaxLineCount: 3}); got.StickyScroll != want {
		t.Errorf("sticky scroll options: %+v, want %+v", got.StickyScroll, want)
	}
	if got.FontFeatures != nil {
		t.Errorf("ligatures on leave the font's own features: %v", got.FontFeatures)
	}
}

func TestOptionsFromSettingsLigatures(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  []FontFeature
	}{
		{"off", false, []FontFeature{{"liga", 0}, {"calt", 0}}},
		{"on", true, nil},
		{"a feature string", `"ss01", "liga" 0`, []FontFeature{{"ss01", 1}, {"liga", 0}}},
		{"a string with nothing readable is off", `nonsense`, []FontFeature{{"liga", 0}, {"calt", 0}}},
	}
	for _, tc := range cases {
		store := newSettings(t, map[string]any{"editor.fontLigatures": tc.value})
		if got := OptionsFromSettings(store, "", "", 4).FontFeatures; !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestOptionsFromSettingsDecorationsWidth(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  float64
	}{
		{"a number", 7.0, 7},
		{"a negative number", -3.0, 0},
		{"a string", "5", 5},
		{"a string in pixels", " 9px ", 9},
		{"a string that is no number", "wide", 10},
	}
	for _, tc := range cases {
		store := newSettings(t, map[string]any{"editor.lineDecorationsWidth": tc.value})
		if got := OptionsFromSettings(store, "", "", 4).LineDecorationsWidth; got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestOptionsFollowALanguageBlock(t *testing.T) {
	store := newSettings(t, nil)
	if err := store.UpdateForLanguage(context.Background(), "editor.fontSize", "markdown", 20.0, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if got := OptionsFromSettings(store, "markdown", "", 4).FontSize; got != 20 {
		t.Errorf("markdown: %v", got)
	}
	if got := OptionsFromSettings(store, "go", "", 4).FontSize; got != 14 {
		t.Errorf("go: %v", got)
	}
}

func TestParseFontWeight(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"normal", 400}, {"", 400}, {"bold", 700}, {" bold ", 700}, {"300", 300}, {"1000", 1000}, {"1", 1},
		{"0", 400}, {"1001", 400}, {"heavy", 400}, {"-5", 400},
	}
	for _, tc := range cases {
		if got := ParseFontWeight(tc.in); got != tc.want {
			t.Errorf("ParseFontWeight(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestRulers(t *testing.T) {
	cases := []struct {
		name string
		raw  []any
		want []ColumnRuler
	}{
		{"none", nil, nil},
		{"numbers", []any{100.0, 80.0}, []ColumnRuler{{Column: 80}, {Column: 100}}},
		{"objects with colors", []any{map[string]any{"column": 120.0, "color": "#ff0000"}, map[string]any{"column": 40.0}},
			[]ColumnRuler{{Column: 40}, {Column: 120, Color: ui.NewColor(0xFFFF0000)}}},
		{"a color that is not one", []any{map[string]any{"column": 10.0, "color": "red-ish"}}, []ColumnRuler{{Column: 10}}},
		{"an object with no column is column 0", []any{map[string]any{"color": "#00ff00"}}, []ColumnRuler{{Column: 0, Color: ui.NewColor(0xFF00FF00)}}},
		{"columns are clamped", []any{-5.0, 20000.0}, []ColumnRuler{{Column: 0}, {Column: 10000}}},
		{"other elements are skipped", []any{"80", true, nil, 90.0}, []ColumnRuler{{Column: 90}}},
		{"equal columns keep their order", []any{map[string]any{"column": 5.0, "color": "#111111"}, map[string]any{"column": 5.0, "color": "#222222"}},
			[]ColumnRuler{{Column: 5, Color: ui.NewColor(0xFF111111)}, {Column: 5, Color: ui.NewColor(0xFF222222)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rulers(tc.raw); !slices.Equal(got, tc.want) {
				t.Errorf("rulers(%v) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
