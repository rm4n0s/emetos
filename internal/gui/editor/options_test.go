package editor

import (
	"context"
	"slices"
	"testing"

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
		RenderLineHighlight: "line", RoundedSelection: true, CursorBlinking: "blink",
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
		"editor.fontSize":             18.0,
		"editor.lineHeight":           1.5,
		"editor.lineNumbers":          "relative",
		"editor.lineNumbersMinChars":  3.0,
		"editor.glyphMargin":          false,
		"editor.lineDecorationsWidth": "12px",
		"editor.folding":              false,
		"editor.scrollBeyondLastLine": false,
		"editor.fontLigatures":        true,

		"editor.renderLineHighlight":              "all",
		"editor.renderLineHighlightOnlyWhenFocus": true,
		"editor.roundedSelection":                 false,
		"editor.cursorWidth":                      3.0,
		"editor.cursorBlinking":                   "solid",
	})
	got := OptionsFromSettings(store, "", "ignored", 2)
	if !slices.Equal(got.FontFamilies, []string{testutil.MonoFamily, "monospace"}) {
		t.Errorf("families: %v", got.FontFamilies)
	}
	if got.FontSize != 18 || got.LineHeight != 27 || got.TabSize != 2 || got.LineNumbers != LineNumbersRelative ||
		got.LineNumbersMinChars != 3 || got.GlyphMargin || got.LineDecorationsWidth != 12 || got.Folding || got.ScrollBeyondLastLine {
		t.Errorf("got %+v", got)
	}
	if got.RenderLineHighlight != "all" || !got.RenderLineHighlightOnlyWhenFocus || got.RoundedSelection || got.CursorWidth != 3 || got.CursorBlinking != "solid" {
		t.Errorf("cursor and highlight options: %+v", got)
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
