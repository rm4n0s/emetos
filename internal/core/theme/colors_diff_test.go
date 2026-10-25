package theme

import "testing"

func TestDiffEditorColors(t *testing.T) {
	r := DefaultColorRegistry()
	for _, tc := range []struct {
		name  string
		theme *ColorTheme
		id    string
		want  string // "" when the color is unset
	}{
		{"dark inserted text", colorTheme(TypeDark, nil, r), "diffEditor.insertedTextBackground", "rgba(156, 204, 44, 0.2)"},
		{"light inserted text", colorTheme(TypeLight, nil, r), "diffEditor.insertedTextBackground", "rgba(156, 204, 44, 0.25)"},
		{"dark removed text", colorTheme(TypeDark, nil, r), "diffEditor.removedTextBackground", "rgba(255, 0, 0, 0.2)"},
		{"dark inserted line", colorTheme(TypeDark, nil, r), "diffEditor.insertedLineBackground", "rgba(155, 185, 85, 0.2)"},
		{"light removed line", colorTheme(TypeLight, nil, r), "diffEditor.removedLineBackground", "rgba(255, 0, 0, 0.2)"},
		{"no inserted line color in high contrast", colorTheme(TypeHCDark, nil, r), "diffEditor.insertedLineBackground", ""},
		{"no gutter color", colorTheme(TypeDark, nil, r), "diffEditorGutter.insertedLineBackground", ""},
		{"no overview color", colorTheme(TypeDark, nil, r), "diffEditorOverview.removedForeground", ""},
		{"no outline in dark", colorTheme(TypeDark, nil, r), "diffEditor.insertedTextBorder", ""},
		{"inserted outline in high contrast dark", colorTheme(TypeHCDark, nil, r), "diffEditor.insertedTextBorder", "#33ff2e"},
		{"removed outline in high contrast light", colorTheme(TypeHCLight, nil, r), "diffEditor.removedTextBorder", "#ad0707"},
		{"no border in dark", colorTheme(TypeDark, nil, r), "diffEditor.border", ""},
		{"the border of high contrast is the contrast border", colorTheme(TypeHCLight, nil, r), "diffEditor.border", "#0f4a85"},
		{"dark diagonal fill", colorTheme(TypeDark, nil, r), "diffEditor.diagonalFill", "rgba(204, 204, 204, 0.2)"},
		{"light diagonal fill", colorTheme(TypeLight, nil, r), "diffEditor.diagonalFill", "rgba(34, 34, 34, 0.2)"},
		{"a theme sets the line color", colorTheme(TypeDark, map[string]string{"diffEditor.insertedLineBackground": "#347d3926"}, r), "diffEditor.insertedLineBackground", "rgba(52, 125, 57, 0.15)"},
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
