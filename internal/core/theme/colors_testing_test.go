package theme

import "testing"

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
