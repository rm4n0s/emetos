package theme

import "testing"

func TestTabColors(t *testing.T) {
	r := DefaultColorRegistry()
	for _, tc := range []struct {
		name  string
		theme *ColorTheme
		id    string
		want  string // "" when the color is unset
	}{
		{"dark default active background is the editor's", colorTheme(TypeDark, nil, r), "tab.activeBackground", "#1e1e1e"},
		{"dark default inactive background", colorTheme(TypeDark, nil, r), "tab.inactiveBackground", "#2d2d2d"},
		{"light default inactive background", colorTheme(TypeLight, nil, r), "tab.inactiveBackground", "#ececec"},
		{"dark default active foreground", colorTheme(TypeDark, nil, r), "tab.activeForeground", "#ffffff"},
		{"dark inactive foreground is half the active one", colorTheme(TypeDark, nil, r), "tab.inactiveForeground", "rgba(255, 255, 255, 0.5)"},
		{"light inactive foreground is 70 % of the active one", colorTheme(TypeLight, nil, r), "tab.inactiveForeground", "rgba(51, 51, 51, 0.7)"},
		{"the border of tabs", colorTheme(TypeDark, nil, r), "tab.border", "#252526"},
		{"no hover background by default", colorTheme(TypeDark, nil, r), "tab.hoverBackground", ""},
		{"no top border by default", colorTheme(TypeDark, nil, r), "tab.activeBorderTop", ""},
		{"the top border of high contrast light", colorTheme(TypeHCLight, nil, r), "tab.activeBorderTop", "#b5200d"},
		{"modified border", colorTheme(TypeDark, nil, r), "tab.activeModifiedBorder", "#3399cc"},
		{"the header of tabs", colorTheme(TypeLight, nil, r), "editorGroupHeader.tabsBackground", "#f3f3f3"},
		{"no border under the tabs", colorTheme(TypeDark, nil, r), "editorGroupHeader.tabsBorder", ""},
		{"a theme sets the top border", colorTheme(TypeDark, map[string]string{"tab.activeBorderTop": "#0078d4"}, r), "tab.activeBorderTop", "#0078d4"},
		{"the unfocused top border follows the theme's", colorTheme(TypeDark, map[string]string{"tab.activeBorderTop": "#0078d4"}, r), "tab.unfocusedActiveBorderTop", "rgba(0, 120, 212, 0.5)"},
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
