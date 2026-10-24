package theme

import "testing"

func TestStatusBarItemColors(t *testing.T) {
	r := DefaultColorRegistry()
	for _, tc := range []struct {
		name  string
		theme *ColorTheme
		id    string
		want  string // "" when the color is unset
	}{
		{"dark hover", colorTheme(TypeDark, nil, r), "statusBarItem.hoverBackground", "rgba(255, 255, 255, 0.12)"},
		{"light hover is black", colorTheme(TypeLight, nil, r), "statusBarItem.hoverBackground", "rgba(0, 0, 0, 0.12)"},
		{"high contrast dark hover", colorTheme(TypeHCDark, nil, r), "statusBarItem.hoverBackground", "#000000"},
		{"dark pressed", colorTheme(TypeDark, nil, r), "statusBarItem.activeBackground", "rgba(255, 255, 255, 0.18)"},
		{"high contrast light pressed", colorTheme(TypeHCLight, nil, r), "statusBarItem.activeBackground", "rgba(0, 0, 0, 0.18)"},
		{"compact hover", colorTheme(TypeDark, nil, r), "statusBarItem.compactHoverBackground", "rgba(255, 255, 255, 0.12)"},
		{"prominent background", colorTheme(TypeDark, nil, r), "statusBarItem.prominentBackground", "rgba(0, 0, 0, 0.5)"},
		{"error foreground is white", colorTheme(TypeDark, nil, r), "statusBarItem.errorForeground", "#ffffff"},
		{"high contrast light error background", colorTheme(TypeHCLight, nil, r), "statusBarItem.errorBackground", "#b5200d"},
		{"high contrast dark error background is unset", colorTheme(TypeHCDark, nil, r), "statusBarItem.errorBackground", ""},
		{"warning background of high contrast light", colorTheme(TypeHCLight, nil, r), "statusBarItem.warningBackground", "#895503"},
		{"hover foreground follows the status bar's", colorTheme(TypeDark, map[string]string{"statusBar.foreground": "#cccccc"}, r), "statusBarItem.hoverForeground", "#cccccc"},
		{"a theme sets the hover", colorTheme(TypeDark, map[string]string{"statusBarItem.hoverBackground": "#F1F1F133"}, r), "statusBarItem.hoverBackground", "rgba(241, 241, 241, 0.2)"},
		{"error hover follows the hover", colorTheme(TypeDark, nil, r), "statusBarItem.errorHoverBackground", "rgba(255, 255, 255, 0.12)"},
		{"error foreground of messages", colorTheme(TypeLight, nil, r), "errorForeground", "#e51400"},
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
	// the backgrounds of the error and warning kinds are darker versions of their text colors
	for _, id := range []string{"statusBarItem.errorBackground", "statusBarItem.warningBackground"} {
		if c, ok := colorTheme(TypeDark, nil, r).Color(id); !ok || c.String() == "" {
			t.Errorf("%s has no default in the dark theme", id)
		}
	}
}
