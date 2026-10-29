package theme

import (
	"strings"
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
		{"editorCodeLens.foreground", "#999999", "#919191", "#999999", "#292929"},
		{"scrollbar.shadow", "#000000", "#dddddd", none, none},
		{"scrollbarSlider.background", "rgba(121, 121, 121, 0.4)", "rgba(100, 100, 100, 0.4)", "rgba(111, 195, 223, 0.6)", "rgba(15, 74, 133, 0.4)"},
		{"scrollbarSlider.hoverBackground", "rgba(100, 100, 100, 0.7)", "rgba(100, 100, 100, 0.7)", "rgba(111, 195, 223, 0.8)", "rgba(15, 74, 133, 0.8)"},
		{"scrollbarSlider.activeBackground", "rgba(191, 191, 191, 0.4)", "rgba(0, 0, 0, 0.6)", "#6fc3df", "#0f4a85"},
		{"scrollbar.background", none, none, none, none},
		{"sash.hoverBorder", "#007fd4", "#0090f1", "#f38518", "#006bbd"},
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

// The colors that step 6.1 added for buttons, toolbar actions and hovers, as VS Code's registry
// (inputColors.ts, editorColors.ts, baseColors.ts, actionWidget.ts) writes their defaults.
func TestDefaultKitColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"disabledForeground", "rgba(204, 204, 204, 0.5)", "rgba(97, 97, 97, 0.5)", "#a5a5a5", "#7f7f7f"},
		{"button.secondaryForeground", "#cccccc", "#616161", "#ffffff", "#292929"},
		{"button.secondaryBackground", "#2a2d2e", "#f0f0f0", none, "#ffffff"},
		{"button.secondaryBorder", "rgba(204, 204, 204, 0.15)", "rgba(97, 97, 97, 0.15)", "#6fc3df", "#0f4a85"},
		{"button.secondaryHoverBackground", "#333637", "#ffffff", none, none},
		{"toolbar.activeBackground", "rgba(99, 102, 103, 0.31)", "rgba(166, 166, 166, 0.31)", none, none},
		{"actionBar.toggledBackground", "rgba(0, 127, 212, 0.4)", "rgba(0, 144, 241, 0.2)", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)"},
		{"editorWidget.border", "rgba(204, 204, 204, 0.2)", "rgba(97, 97, 97, 0.2)", "#6fc3df", "#0f4a85"},
		{"editorHoverWidget.background", "#252526", "#f3f3f3", "#0c141f", "#ffffff"},
		{"editorHoverWidget.foreground", "#cccccc", "#616161", "#ffffff", "#292929"},
		{"editorHoverWidget.border", "rgba(204, 204, 204, 0.2)", "rgba(97, 97, 97, 0.2)", "#6fc3df", "#0f4a85"},
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

// The colors that step 6.3 added for menus and the menu bar, as VS Code's registry (menuColors.ts,
// inputColors.ts, theme.ts) writes their defaults.
func TestDefaultMenuColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"dropdown.background", "#3c3c3c", "#ffffff", "#000000", "#ffffff"},
		{"dropdown.foreground", "#f0f0f0", "#616161", "#ffffff", "#292929"},
		{"titleBar.activeForeground", "#cccccc", "#333333", "#ffffff", "#292929"},
		{"menu.border", none, none, "#6fc3df", "#0f4a85"},
		{"menu.foreground", "#f0f0f0", "#616161", "#ffffff", "#292929"},
		{"menu.background", "#3c3c3c", "#ffffff", "#000000", "#ffffff"},
		{"menu.selectionForeground", "#ffffff", "#ffffff", none, none},
		{"menu.selectionBackground", "#04395e", "#0060c0", none, "rgba(15, 74, 133, 0.1)"},
		{"menu.selectionBorder", none, none, "#f38518", "#006bbd"},
		{"menu.separatorBackground", "rgba(204, 204, 204, 0.2)", "rgba(97, 97, 97, 0.2)", "#6fc3df", "#0f4a85"},
		{"menubar.selectionForeground", "#cccccc", "#333333", "#ffffff", "#292929"},
		{"menubar.selectionBackground", "rgba(90, 93, 94, 0.31)", "rgba(184, 184, 184, 0.31)", none, none},
		{"menubar.selectionBorder", none, none, "#f38518", "#006bbd"},
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

// The colors that step 6.4 added for the tree, as VS Code's registry (listColors.ts) writes their defaults.
func TestDefaultTreeColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"list.focusBackground", none, none, none, none},
		{"list.focusForeground", none, none, none, none},
		{"list.focusAndSelectionOutline", none, none, none, none},
		{"list.inactiveSelectionBackground", "#37373d", "#e4e6f1", none, "rgba(15, 74, 133, 0.1)"},
		{"list.inactiveSelectionForeground", none, none, none, none},
		{"list.inactiveFocusBackground", none, none, none, none},
		{"list.inactiveFocusOutline", none, none, none, none},
		{"tree.indentGuidesStroke", "#585858", "#a9a9a9", "#a9a9a9", "#a5a5a5"},
		{"tree.inactiveIndentGuidesStroke", "rgba(88, 88, 88, 0.4)", "rgba(169, 169, 169, 0.4)", "rgba(169, 169, 169, 0.4)", "rgba(165, 165, 165, 0.4)"},
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

// The colors that step 6.4b added for the multi-selection, find and drag and drop of lists and trees, as VS
// Code's registry (listColors.ts) writes their defaults.
func TestDefaultListInteractionColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"list.dropBackground", "#062f4a", "#d6ebff", none, none},
		{"list.dropBetweenBackground", "#c5c5c5", "#424242", none, none},
		{"list.highlightForeground", "#2aaaff", "#0066bf", "#f38518", "#006bbd"},
		{"list.focusHighlightForeground", "#2aaaff", "#bbe7ff", "#f38518", "#006bbd"}, // light: the theme does not set list.activeSelectionBackground here
		{"list.filterMatchBackground", "rgba(234, 92, 0, 0.33)", "rgba(234, 92, 0, 0.33)", none, none},
		{"list.filterMatchBorder", none, none, "#6fc3df", "#006bbd"},
		{"listFilterWidget.background", "#252526", "#f3f3f3", "#0c141f", "#ffffff"},
		{"listFilterWidget.outline", "rgba(0, 0, 0, 0)", "rgba(0, 0, 0, 0)", "#f38518", "#007acc"},
		{"listFilterWidget.noMatchesOutline", "#be1100", "#be1100", "#6fc3df", "#0f4a85"},
		{"listFilterWidget.shadow", "rgba(0, 0, 0, 0.36)", "rgba(0, 0, 0, 0.16)", none, none},
		{"list.errorForeground", "#f88070", "#b01011", none, none}, // step 7.4: the label of an editor whose file was deleted
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

// The colors that step 6.5 added for inputs, checkboxes, dropdowns, badges, progress bars and
// dialogs, as VS Code's registry (inputColors.ts, miscColors.ts, editorColors.ts) writes their
// defaults.
func TestDefaultInputKitColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"inputValidation.infoBackground", "#063b49", "#d6ecf2", "#000000", "#ffffff"},
		{"inputValidation.infoForeground", none, none, none, "#292929"},
		{"inputValidation.infoBorder", "#007acc", "#007acc", "#6fc3df", "#0f4a85"},
		{"inputValidation.warningBackground", "#352a05", "#f6f5d2", "#000000", "#ffffff"},
		{"inputValidation.warningForeground", none, none, none, "#292929"},
		{"inputValidation.warningBorder", "#b89500", "#b89500", "#6fc3df", "#0f4a85"},
		{"dropdown.listBackground", none, none, "#000000", "#ffffff"},
		{"dropdown.border", "#3c3c3c", "#cecece", "#6fc3df", "#0f4a85"},
		{"checkbox.background", "#3c3c3c", "#ffffff", "#000000", "#ffffff"},
		{"checkbox.selectBackground", "#252526", "#f3f3f3", "#0c141f", "#ffffff"},
		{"checkbox.foreground", "#f0f0f0", "#616161", "#ffffff", "#292929"},
		{"checkbox.border", "#3c3c3c", "#cecece", "#6fc3df", "#0f4a85"},
		{"checkbox.selectBorder", "#c5c5c5", "#424242", "#ffffff", "#292929"},
		{"badge.background", "#4d4d4d", "#c4c4c4", "#000000", "#0f4a85"},
		{"badge.foreground", "#ffffff", "#333333", "#ffffff", "#ffffff"},
		{"progressBar.background", "#0e70c0", "#0e70c0", "#6fc3df", "#0f4a85"},
		{"editorError.foreground", "#f14c4c", "#e51400", "#f48771", "#b5200d"},
		{"editorWarning.foreground", "#cca700", "#bf8803", "#ffd370", "#895503"},
		{"editorInfo.foreground", "#59a4f9", "#0063d3", "#59a4f9", "#0063d3"},
		{"problemsErrorIcon.foreground", "#f14c4c", "#e51400", "#f48771", "#b5200d"},
		{"problemsWarningIcon.foreground", "#cca700", "#bf8803", "#ffd370", "#895503"},
		{"problemsInfoIcon.foreground", "#59a4f9", "#0063d3", "#59a4f9", "#0063d3"},
		// the state icons of the container views (step 18.2), VS Code's debugColors.ts
		{"debugIcon.startForeground", "#89d185", "#388a34", "#89d185", "#388a34"},
		{"debugIcon.pauseForeground", "#75beff", "#007acc", "#75beff", "#007acc"},
		{"debugIcon.stopForeground", "#f48771", "#a1260d", "#f48771", "#a1260d"},
		{"debugIcon.restartForeground", "#89d185", "#388a34", "#89d185", "#388a34"},
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

// The disabled checkbox colors are mixes of the checkbox colors (33 % of the other color).
func TestDefaultCheckboxDisabledColors(t *testing.T) {
	registry := DefaultColorRegistry()
	for _, typ := range []Type{TypeDark, TypeLight} {
		ct := colorTheme(typ, nil, registry)
		bg, _ := ct.Color("checkbox.background")
		fg, _ := ct.Color("checkbox.foreground")
		for _, tc := range []struct {
			id   string
			a, b Color
		}{{"checkbox.disabled.background", bg, fg}, {"checkbox.disabled.foreground", fg, bg}} {
			got, ok := ct.Color(tc.id)
			if !ok || got != tc.a.Mix(tc.b, 0.33) {
				t.Errorf("%s %s = %v, want %v", typ, tc.id, got, tc.a.Mix(tc.b, 0.33))
			}
		}
	}
}

// The colors that step 6.6 added for the parts of the workbench, as VS Code's registry (theme.ts) writes their defaults.
func TestDefaultLayoutColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"statusBar.foreground", "#ffffff", "#ffffff", "#ffffff", "#292929"},
		{"statusBar.noFolderForeground", "#ffffff", "#ffffff", "#ffffff", "#292929"},
		{"statusBar.background", "#007acc", "#007acc", none, none},
		{"statusBar.noFolderBackground", "#68217a", "#68217a", none, none},
		{"statusBar.border", none, none, "#6fc3df", "#0f4a85"},
		{"statusBar.noFolderBorder", none, none, "#6fc3df", "#0f4a85"},
		{"activityBar.background", "#333333", "#2c2c2c", "#000000", "#ffffff"},
		{"activityBar.border", none, none, "#6fc3df", "#0f4a85"},
		{"sideBar.background", "#252526", "#f3f3f3", "#000000", "#ffffff"},
		{"sideBar.foreground", none, none, none, none},
		{"sideBar.border", none, none, "#6fc3df", "#0f4a85"},
		{"sideBarTitle.background", "#252526", "#f3f3f3", "#000000", "#ffffff"},
		{"sideBarTitle.foreground", none, none, none, none},
		{"sideBarTitle.border", none, none, "#6fc3df", "#0f4a85"},
		{"panel.background", "#1e1e1e", "#ffffff", "#000000", "#ffffff"},
		{"panel.border", "rgba(128, 128, 128, 0.35)", "rgba(128, 128, 128, 0.35)", "#6fc3df", "#0f4a85"},
		{"panelTitle.border", none, none, "#6fc3df", "#0f4a85"},
		{"panelTitle.activeForeground", "#e7e7e7", "#424242", "#ffffff", "#292929"},
		{"panelTitle.inactiveForeground", "rgba(231, 231, 231, 0.6)", "rgba(66, 66, 66, 0.75)", "#ffffff", "#292929"},
		{"panelTitle.activeBorder", "#e7e7e7", "#424242", "#6fc3df", "#b5200d"},
		{"titleBar.activeBackground", "#3c3c3c", "#dddddd", "#000000", "#ffffff"},
		{"titleBar.border", none, none, "#6fc3df", "#0f4a85"},
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

// The colors that step 6.12 added for the count on a tab of the panel (theme.ts).
func TestDefaultPanelBadgeColors(t *testing.T) {
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"activityBarBadge.background", "#007acc", "#007acc", "#000000", "#0f4a85"},
		{"activityBarBadge.foreground", "#ffffff", "#ffffff", "#ffffff", "#ffffff"},
		{"panelTitleBadge.background", "#007acc", "#007acc", "#000000", "#0f4a85"},
		{"panelTitleBadge.foreground", "#ffffff", "#ffffff", "#ffffff", "#ffffff"},
		{"problemsErrorIcon.foreground", "#f14c4c", "#e51400", "#f48771", "#b5200d"},
		{"problemsWarningIcon.foreground", "#cca700", "#bf8803", "#ffd370", "#895503"},
		{"problemsInfoIcon.foreground", "#59a4f9", "#0063d3", "#59a4f9", "#0063d3"},
	}
	registry := DefaultColorRegistry()
	for _, tc := range cases {
		for _, typ := range []struct {
			t    Type
			want string
		}{{TypeDark, tc.dark}, {TypeLight, tc.light}, {TypeHCDark, tc.hcDark}, {TypeHCLight, tc.hcLight}} {
			t.Run(tc.id+" "+string(typ.t), func(t *testing.T) {
				c, ok := colorTheme(typ.t, nil, registry).Color(tc.id)
				if !ok || c.String() != typ.want {
					t.Errorf("got %v (%v), want %s", c.String(), ok, typ.want)
				}
			})
		}
	}
}

// The colors that step 6.11 added for the toasts and the notification center, as VS Code's registry
// (theme.ts) writes their defaults.
func TestDefaultNotificationColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"textLink.foreground", "#3794ff", "#006ab1", "#21a6ff", "#0f4a85"},
		{"notificationCenter.border", none, none, "#6fc3df", "#0f4a85"},
		{"notificationToast.border", none, none, "#6fc3df", "#0f4a85"},
		{"notifications.foreground", "#cccccc", "#616161", "#ffffff", "#292929"},
		{"notifications.background", "#252526", "#f3f3f3", "#0c141f", "#ffffff"},
		{"notificationLink.foreground", "#3794ff", "#006ab1", "#21a6ff", "#0f4a85"},
		{"notificationCenterHeader.foreground", none, none, none, none},
		{"notificationsErrorIcon.foreground", "#f14c4c", "#e51400", "#f48771", "#b5200d"},
		{"notificationsWarningIcon.foreground", "#cca700", "#bf8803", "#ffd370", "#895503"},
		{"notificationsInfoIcon.foreground", "#59a4f9", "#0063d3", "#59a4f9", "#0063d3"},
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
	// the header is a lighter (dark) or darker (light) background; the border between notifications is the header
	for _, typ := range []Type{TypeDark, TypeLight, TypeHCDark, TypeHCLight} {
		ct := colorTheme(typ, nil, registry)
		bg, _ := ct.Color("notifications.background")
		want := bg
		switch typ {
		case TypeDark:
			want = bg.Lighten(0.3)
		case TypeLight:
			want = bg.Darken(0.05)
		}
		header, ok := ct.Color("notificationCenterHeader.background")
		border, _ := ct.Color("notifications.border")
		if !ok || header.String() != want.String() || border.String() != header.String() {
			t.Errorf("%s: header %v border %v, want %v", typ, header, border, want)
		}
	}
}

// The colors that step 6.17 added for the quick input, as VS Code's registry (quickpickColors.ts,
// listColors.ts, keybindingLabel.ts) writes their defaults.
func TestDefaultQuickInputColors(t *testing.T) {
	const none = "-"
	cases := []struct {
		id                           string
		dark, light, hcDark, hcLight string
	}{
		{"pickerGroup.foreground", "#3794ff", "#0066bf", "#ffffff", "#0f4a85"},
		{"pickerGroup.border", "#3f3f46", "#ccceDb", "#ffffff", "#0f4a85"},
		{"quickInputTitle.background", "rgba(255, 255, 255, 0.11)", "rgba(0, 0, 0, 0.06)", "#000000", "#ffffff"},
		{"quickInput.list.focusBackground", none, none, none, none},
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
				if !strings.EqualFold(got, typ.want) {
					t.Errorf("got %s, want %s", got, typ.want)
				}
			})
		}
	}
	// the colors that follow others do so
	th := colorTheme(TypeDark, nil, registry)
	for _, pair := range [][2]string{
		{"quickInput.background", "editorWidget.background"},
		{"quickInput.foreground", "editorWidget.foreground"},
		{"quickInputList.focusForeground", "list.activeSelectionForeground"},
		{"quickInputList.focusBackground", "list.activeSelectionBackground"},
		{"quickInputList.focusHighlightForeground", "list.focusHighlightForeground"},
	} {
		got, ok1 := th.Color(pair[0])
		want, ok2 := th.Color(pair[1])
		if !ok1 || !ok2 || got != want {
			t.Errorf("%s = %v (%v), want %s = %v (%v)", pair[0], got, ok1, pair[1], want, ok2)
		}
	}
}
