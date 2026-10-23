package theme

import (
	"testing"

	"github.com/rm4n0s/errors"
)

// colorTheme is a color theme of a type that sets the given colors.
func colorTheme(typ Type, set map[string]string, r *ColorRegistry) *ColorTheme {
	return NewColorTheme(&Theme{Type: typ, Colors: set}, r)
}

func TestRegisterColors(t *testing.T) {
	r := NewColorRegistry()
	if err := r.Register(ColorContribution{ID: "a", Defaults: AllThemes(Hex("#010203"))}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		c     ColorContribution
		route string
	}{
		{"no id", ColorContribution{}, "Register.ColorInvalid"},
		{"taken", ColorContribution{ID: "a"}, "Register.ColorAlreadyRegistered"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := r.Register(tc.c)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	if got, ok := r.Lookup("a"); !ok || got.ID != "a" {
		t.Errorf("Lookup = %+v, %v", got, ok)
	}
	if _, ok := r.Lookup("b"); ok {
		t.Error("b is not registered")
	}
	r.MustRegister(ColorContribution{ID: "0"}, ColorContribution{ID: "b"})
	var ids []string
	for _, c := range r.Colors() {
		ids = append(ids, c.ID)
	}
	if len(ids) != 3 || ids[0] != "0" || ids[1] != "a" || ids[2] != "b" {
		t.Errorf("Colors = %v: sorted by id", ids)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("MustRegister of a taken id panics")
			}
		}()
		r.MustRegister(ColorContribution{ID: "a"})
	}()
}

// The defaults of the colors that 3.1 lists, for each type of theme, as VS Code's registry
// writes them (editorColors.ts, editorColorRegistry.ts, baseColors.ts, miscColors.ts).
func TestDefaultColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"foreground", "#cccccc", "#616161", "#ffffff", "#292929"},
		{"focusBorder", "#007fd4", "#0090f1", "#f38518", "#006bbd"},
		{"contrastBorder", none, none, "#6fc3df", "#0f4a85"},
		{"contrastActiveBorder", none, none, "#f38518", "#006bbd"},
		{"widget.shadow", "rgba(0, 0, 0, 0.36)", "rgba(0, 0, 0, 0.16)", none, none},
		{"editor.background", "#1e1e1e", "#ffffff", "#000000", "#ffffff"},
		{"editor.foreground", "#bbbbbb", "#333333", "#ffffff", "#292929"},
		{"editor.selectionBackground", "#264f78", "#add6ff", "#f3f518", "#0f4a85"},
		{"editor.inactiveSelectionBackground", "rgba(38, 79, 120, 0.5)", "rgba(173, 214, 255, 0.5)", "rgba(243, 245, 24, 0.7)", "rgba(15, 74, 133, 0.5)"},
		{"editor.lineHighlightBackground", none, none, none, none},
		{"editor.lineHighlightBorder", "#282828", "#eeeeee", "#f38518", "#0f4a85"},
		{"editorCursor.foreground", "#aeafad", "#000000", "#ffffff", "#0f4a85"},
		{"editorLineNumber.foreground", "#858585", "#237893", "#ffffff", "#292929"},
		{"editorLineNumber.activeForeground", "#c6c6c6", "#0b216f", "#f38518", "#006bbd"},
		{"editorGutter.background", "#1e1e1e", "#ffffff", "#000000", "#ffffff"},
		{"editorWhitespace.foreground", "rgba(227, 228, 226, 0.16)", "rgba(51, 51, 51, 0.2)", "rgba(227, 228, 226, 0.16)", "#cccccc"},
		{"editorIndentGuide.background1", "rgba(227, 228, 226, 0.16)", "rgba(51, 51, 51, 0.2)", "rgba(227, 228, 226, 0.16)", "#cccccc"},
		{"editorIndentGuide.activeBackground1", "rgba(227, 228, 226, 0.16)", "rgba(51, 51, 51, 0.2)", "rgba(227, 228, 226, 0.16)", "#cccccc"},
		{"editorIndentGuide.background2", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)"},
		{"editorBracketPairGuide.background1", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)"},
		{"editorBracketPairGuide.activeBackground6", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)"},
		{"editorRuler.foreground", "#5a5a5a", "#d3d3d3", "#ffffff", "#292929"},
		{"scrollbar.shadow", "#000000", "#dddddd", none, none},
		{"scrollbarSlider.background", "rgba(121, 121, 121, 0.4)", "rgba(100, 100, 100, 0.4)", "rgba(111, 195, 223, 0.6)", "rgba(15, 74, 133, 0.4)"},
		{"scrollbarSlider.hoverBackground", "rgba(100, 100, 100, 0.7)", "rgba(100, 100, 100, 0.7)", "rgba(111, 195, 223, 0.8)", "rgba(15, 74, 133, 0.8)"},
		{"scrollbarSlider.activeBackground", "rgba(191, 191, 191, 0.4)", "rgba(0, 0, 0, 0.6)", "#6fc3df", "#0f4a85"},
		{"nothing.registered", none, none, none, none},
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

func TestEveryDefaultColorHasADescription(t *testing.T) {
	for _, c := range DefaultColorRegistry().Colors() {
		if c.Description == "" {
			t.Errorf("%s has no description", c.ID)
		}
	}
}

func TestColorThemeSetsColors(t *testing.T) {
	registry := DefaultColorRegistry()
	ct := colorTheme(TypeDark, map[string]string{"editor.background": "#112233", "editor.selectionBackground": "#445566", "broken": "nope"}, registry)
	if c, _ := ct.Color("editor.background"); c.Hex() != "#112233" {
		t.Errorf("a color the theme sets: %s", c)
	}
	// What derives from a color the theme sets follows it.
	if c, _ := ct.Color("editorGutter.background"); c.Hex() != "#112233" {
		t.Errorf("the gutter follows the editor background: %s", c)
	}
	if c, _ := ct.Color("editor.inactiveSelectionBackground"); c.String() != "rgba(68, 85, 102, 0.5)" {
		t.Errorf("the inactive selection follows the selection: %s", c)
	}
	if c, ok := ct.Color("broken"); !ok || c.Hex() != "#ff0000" {
		t.Errorf("a color that is not a color is red: %s %v", c, ok)
	}
	if !ct.Defines("editor.background") || ct.Defines("editorGutter.background") {
		t.Error("Defines tells what the theme itself sets")
	}
	if c, _ := ct.Default("editor.background"); c.Hex() != "#1e1e1e" {
		t.Errorf("Default ignores the theme: %s", c)
	}
	if ct.Type() != TypeDark || ct.Theme() == nil {
		t.Error("Type and Theme")
	}
	if _, ok := ct.Default("unknown"); ok {
		t.Error("an unregistered color has no default")
	}
}

func TestColorTransforms(t *testing.T) {
	r := NewColorRegistry()
	r.MustRegister(
		ColorContribution{ID: "base", Defaults: AllThemes(Hex("#808080"))},
		ColorContribution{ID: "bg", Defaults: AllThemes(Hex("#ffffff"))},
		ColorContribution{ID: "none"},
		ColorContribution{ID: "darken", Defaults: AllThemes(Darken(Ref("base"), 0.5))},
		ColorContribution{ID: "lighten", Defaults: AllThemes(Lighten(Ref("base"), 0.5))},
		ColorContribution{ID: "transparent", Defaults: AllThemes(Transparent(Ref("base"), 0.25))},
		ColorContribution{ID: "mix", Defaults: AllThemes(Mix(Hex("#000000"), Hex("#ffffff"), 0.5))},
		ColorContribution{ID: "mixmissing", Defaults: AllThemes(Mix(Ref("none"), Hex("#ffffff"), 1))},
		ColorContribution{ID: "opaque", Defaults: AllThemes(Opaque(Transparent(Hex("#000000"), 0.5), Ref("bg")))},
		ColorContribution{ID: "opaquenobg", Defaults: AllThemes(Opaque(Hex("#102030"), Ref("none")))},
		ColorContribution{ID: "oneof", Defaults: AllThemes(OneOf(Ref("none"), nil, Ref("base"), Hex("#ff0000")))},
		ColorContribution{ID: "oneofnone", Defaults: AllThemes(OneOf(Ref("none")))},
		ColorContribution{ID: "ifdefined", Defaults: AllThemes(IfDefinedThenElse("base", Hex("#00ff00"), Hex("#0000ff")))},
		ColorContribution{ID: "ifundefined", Defaults: AllThemes(IfDefinedThenElse("none", Hex("#00ff00"), Hex("#0000ff")))},
		ColorContribution{ID: "lessdark", Defaults: AllThemes(LessProminent(Hex("#202020"), Ref("bg"), 0.5, 0.5))},
		ColorContribution{ID: "lesslight", Defaults: AllThemes(LessProminent(Hex("#f0f0f0"), Hex("#000000"), 0.5, 0.5))},
		ColorContribution{ID: "lessnobg", Defaults: AllThemes(LessProminent(Hex("#202020"), Ref("none"), 0.5, 0.5))},
		ColorContribution{ID: "lessnone", Defaults: AllThemes(LessProminent(Ref("none"), Ref("bg"), 0.5, 0.5))},
		ColorContribution{ID: "loop1", Defaults: AllThemes(Ref("loop2"))},
		ColorContribution{ID: "loop2", Defaults: AllThemes(Ref("loop1"))},
		ColorContribution{ID: "dangling", Defaults: AllThemes(Ref("unregistered"))},
	)
	ct := colorTheme(TypeDark, map[string]string{"base": "#808080"}, r)
	cases := []struct{ id, want string }{
		{"darken", "#404040"}, {"lighten", "#c0c0c0"}, {"transparent", "rgba(128, 128, 128, 0.25)"},
		{"mix", "#7f7f7f"}, {"mixmissing", "#ffffff"}, {"opaque", "#7f7f7f"}, {"opaquenobg", "#102030"},
		{"oneof", "#808080"}, {"ifdefined", "#00ff00"}, {"ifundefined", "#0000ff"},
		{"lessdark", "rgba(48, 48, 48, 0.5)"}, {"lesslight", "rgba(120, 120, 120, 0.5)"}, {"lessnobg", "rgba(32, 32, 32, 0.25)"},
		{"lessnone", "-"}, {"oneofnone", "-"}, {"none", "-"}, {"loop1", "-"}, {"dangling", "-"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			c, ok := ct.Color(tc.id)
			got := "-"
			if ok {
				got = c.String()
			}
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}
