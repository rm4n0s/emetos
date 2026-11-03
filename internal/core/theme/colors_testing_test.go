package theme

import (
	"strings"
	"testing"
)

// The colors of the Test Explorer (step 26.2): the icons of the states and their outdated variants, as VS Code's theme.ts has them.
func TestTestingIconColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"list.warningForeground", "#cca700", "#855f00", none, none},
		{"testing.iconPassed", "#73c991", "#73c991", "#73c991", "#73c991"},
		{"testing.runAction", "#73c991", "#73c991", "#73c991", "#73c991"},
		{"testing.iconUnset", "#848484", "#848484", "#848484", "#848484"},
		{"testing.iconSkipped", "#848484", "#848484", "#848484", "#848484"},
		{"testing.iconFailed", "#f88070", "#b01011", none, none},
		{"testing.iconErrored", "#f88070", "#b01011", none, none},
		{"testing.iconQueued", "#cca700", "#855f00", none, none},
		// the retired color is the color at 70% of its opacity
		{"testing.iconPassed.retired", "rgba(115, 201, 145, 0.7)", "rgba(115, 201, 145, 0.7)", "rgba(115, 201, 145, 0.7)", "rgba(115, 201, 145, 0.7)"},
		{"testing.iconFailed.retired", "rgba(248, 128, 112, 0.7)", "rgba(176, 16, 17, 0.7)", none, none},
	}
	registry := DefaultColorRegistry()
	for _, tc := range cases {
		for _, typ := range []struct {
			t    Type
			want string
		}{{TypeDark, tc.dark}, {TypeLight, tc.light}, {TypeHCDark, tc.hcDark}, {TypeHCLight, tc.hcLight}} {
			t.Run(tc.id+" "+string(typ.t), func(t *testing.T) {
				c, ok := colorTheme(typ.t, nil, registry).Color(tc.id)
				got := none
				if ok {
					got = c.String()
				}
				if got != typ.want {
					t.Errorf("got %s, want %s", got, typ.want)
				}
			})
		}
	}
}

// The colors of the editor and the peek of Testing (step 26.3): VS Code's ids and defaults (theme.ts and miscColors.ts).
func TestTestingEditorColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"activityErrorBadge.background", "#f14c4c", "#e51400", "#f14c4c", "#e51400"},
		{"activityErrorBadge.foreground", "#ffffff", "#ffffff", "#ffffff", "#ffffff"},
		{"testing.peekBorder", "#f14c4c", "#e51400", "#6fc3df", "#0f4a85"},
		{"testing.messagePeekBorder", "#59a4f9", "#0063d3", "#6fc3df", "#0f4a85"},
		{"testing.peekHeaderBackground", "rgba(241, 76, 76, 0.1)", "rgba(229, 20, 0, 0.1)", none, none},
		{"testing.messagePeekHeaderBackground", "rgba(89, 164, 249, 0.1)", "rgba(0, 99, 211, 0.1)", none, none},
		{"testing.message.error.badgeBackground", "#f14c4c", "#e51400", "#f14c4c", "#e51400"},
		{"testing.message.error.badgeBorder", "#f14c4c", "#e51400", "#f14c4c", "#e51400"},
		{"testing.message.error.badgeForeground", "#ffffff", "#ffffff", "#ffffff", "#ffffff"},
		{"testing.message.error.lineBackground", none, none, none, none},
		{"testing.message.info.lineBackground", none, none, none, none},
	}
	registry := DefaultColorRegistry()
	for _, tc := range cases {
		for _, typ := range []struct {
			t    Type
			want string
		}{{TypeDark, tc.dark}, {TypeLight, tc.light}, {TypeHCDark, tc.hcDark}, {TypeHCLight, tc.hcLight}} {
			t.Run(tc.id+" "+string(typ.t), func(t *testing.T) {
				c, ok := colorTheme(typ.t, nil, registry).Color(tc.id)
				got := none
				if ok {
					got = c.String()
				}
				if got != typ.want {
					t.Errorf("got %s, want %s", got, typ.want)
				}
			})
		}
	}
	// the info decoration is the foreground of the editor at half its opacity
	if c, ok := colorTheme(TypeDark, nil, registry).Color("testing.message.info.decorationForeground"); !ok || !strings.Contains(c.String(), "0.5)") {
		t.Errorf("the info decoration color is %v", c)
	}
}

// The colors of the coverage (step 26.4): VS Code's ids and defaults (theme.ts and chartsColors.ts); the gutter colors are the diff colors
// at a fraction of their opacity, and the high contrast themes use the chart colors.
func TestTestingCoverageColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"charts.red", "#f14c4c", "#e51400", "#f48771", "#b5200d"},
		{"charts.yellow", "#cca700", "#bf8803", "#ffd370", "#895503"},
		{"charts.green", "#89d185", "#388a34", "#89d185", "#374e06"},
		{"testing.coveredBackground", "rgba(156, 204, 44, 0.2)", "rgba(156, 204, 44, 0.25)", none, none},
		{"testing.coveredBorder", "rgba(156, 204, 44, 0.15)", "rgba(156, 204, 44, 0.19)", "#6fc3df", "#0f4a85"},
		{"testing.coveredGutterBackground", "rgba(156, 204, 44, 0.12)", "rgba(156, 204, 44, 0.15)", "#89d185", "#374e06"},
		{"testing.uncoveredBranchBackground", "#781212", "#ff9999", none, none},
		{"testing.uncoveredBackground", "rgba(255, 0, 0, 0.2)", "rgba(255, 0, 0, 0.2)", none, none},
		{"testing.uncoveredBorder", "rgba(255, 0, 0, 0.15)", "rgba(255, 0, 0, 0.15)", "#6fc3df", "#0f4a85"},
		{"testing.uncoveredGutterBackground", "rgba(255, 0, 0, 0.3)", "rgba(255, 0, 0, 0.3)", "#f48771", "#b5200d"},
		{"testing.coverCountBadgeBackground", "#4d4d4d", "#c4c4c4", "#000000", "#0f4a85"},
		{"testing.coverCountBadgeForeground", "#ffffff", "#333333", "#ffffff", "#ffffff"},
	}
	registry := DefaultColorRegistry()
	for _, tc := range cases {
		for _, typ := range []struct {
			t    Type
			want string
		}{{TypeDark, tc.dark}, {TypeLight, tc.light}, {TypeHCDark, tc.hcDark}, {TypeHCLight, tc.hcLight}} {
			t.Run(tc.id+" "+string(typ.t), func(t *testing.T) {
				c, ok := colorTheme(typ.t, nil, registry).Color(tc.id)
				got := none
				if ok {
					got = c.String()
				}
				if got != typ.want {
					t.Errorf("got %s, want %s", got, typ.want)
				}
			})
		}
	}
}
