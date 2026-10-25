package theme

import "testing"

func TestMergeEditorColors(t *testing.T) {
	r := DefaultColorRegistry()
	for _, tc := range []struct {
		name  string
		theme *ColorTheme
		id    string
		want  string // "" when the color is unset
	}{
		{"dark change", colorTheme(TypeDark, nil, r), "mergeEditor.change.background", "rgba(155, 185, 85, 0.2)"},
		{"dark word change", colorTheme(TypeDark, nil, r), "mergeEditor.change.word.background", "rgba(156, 204, 44, 0.2)"},
		{"light word change", colorTheme(TypeLight, nil, r), "mergeEditor.change.word.background", "rgba(156, 204, 44, 0.4)"},
		{"high contrast light word change", colorTheme(TypeHCLight, nil, r), "mergeEditor.change.word.background", "rgba(156, 204, 44, 0.4)"},
		{"dark base change", colorTheme(TypeDark, nil, r), "mergeEditor.changeBase.background", "#4b1818"},
		{"light base word change", colorTheme(TypeLight, nil, r), "mergeEditor.changeBase.word.background", "#ffa3a3"},
		{"dark unhandled unfocused border", colorTheme(TypeDark, nil, r), "mergeEditor.conflict.unhandledUnfocused.border", "rgba(255, 166, 0, 0.48)"},
		{"light unhandled unfocused border", colorTheme(TypeLight, nil, r), "mergeEditor.conflict.unhandledUnfocused.border", "#ffa600"},
		{"unhandled focused border", colorTheme(TypeDark, nil, r), "mergeEditor.conflict.unhandledFocused.border", "#ffa600"},
		{"handled unfocused border", colorTheme(TypeLight, nil, r), "mergeEditor.conflict.handledUnfocused.border", "rgba(134, 134, 134, 0.29)"},
		{"handled focused border", colorTheme(TypeDark, nil, r), "mergeEditor.conflict.handledFocused.border", "rgba(193, 193, 193, 0.8)"},
		{"handled ruler", colorTheme(TypeDark, nil, r), "mergeEditor.conflict.handled.minimapOverViewRuler", "rgba(173, 172, 168, 0.93)"},
		{"unhandled ruler", colorTheme(TypeDark, nil, r), "mergeEditor.conflict.unhandled.minimapOverViewRuler", "#fcba03"},
		{"conflicting lines", colorTheme(TypeDark, nil, r), "mergeEditor.conflictingLines.background", "rgba(255, 234, 0, 0.28)"},
		{"input 1 is the current header made 40% opaque", colorTheme(TypeDark, nil, r), "mergeEditor.conflict.input1.background", "rgba(64, 200, 174, 0.2)"},
		{"input 2 is the incoming header made 40% opaque", colorTheme(TypeLight, nil, r), "mergeEditor.conflict.input2.background", "rgba(64, 166, 255, 0.2)"},
		{"current header", colorTheme(TypeDark, nil, r), "merge.currentHeaderBackground", "rgba(64, 200, 174, 0.5)"},
		{"incoming header", colorTheme(TypeLight, nil, r), "merge.incomingHeaderBackground", "rgba(64, 166, 255, 0.5)"},
		{"common header", colorTheme(TypeDark, nil, r), "merge.commonHeaderBackground", "rgba(96, 96, 96, 0.4)"},
		{"current content", colorTheme(TypeDark, nil, r), "merge.currentContentBackground", "rgba(64, 200, 174, 0.2)"},
		{"no current header in high contrast", colorTheme(TypeHCDark, nil, r), "merge.currentHeaderBackground", ""},
		{"no merge border in dark", colorTheme(TypeDark, nil, r), "merge.border", ""},
		{"merge border in high contrast dark", colorTheme(TypeHCDark, nil, r), "merge.border", "#c3df6f"},
		{"merge border in high contrast light", colorTheme(TypeHCLight, nil, r), "merge.border", "#007acc"},
		{"a theme sets a border", colorTheme(TypeDark, map[string]string{"mergeEditor.conflict.unhandledFocused.border": "#ff0000"}, r), "mergeEditor.conflict.unhandledFocused.border", "#ff0000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := tc.theme.Color(tc.id)
			got := ""
			if ok {
				got = c.String()
			}
			if got != tc.want {
				t.Errorf("%s = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}
