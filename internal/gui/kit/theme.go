package kit

import (
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/theme"
)

// Colors are the workbench colors the widgets read, named as VS Code names them in a color
// theme's "colors". ColorsOf takes them from a theme of the theme service.
type Colors struct {
	EditorBackground                  ui.Color
	EditorForeground                  ui.Color
	EditorLineHighlightBackground     ui.Color
	EditorLineHighlightBorder         ui.Color
	EditorSelectionBackground         ui.Color
	EditorInactiveSelectionBackground ui.Color
	EditorCursorForeground            ui.Color
	EditorLineNumberForeground        ui.Color
	EditorLineNumberActiveForeground  ui.Color
	EditorGutterBackground            ui.Color
	EditorWhitespaceForeground        ui.Color
	EditorIndentGuideBackground1      ui.Color
	// EditorIndentGuideBackground are the colors of the indent guides by level, and
	// EditorIndentGuideActiveBackground those of the active guide.
	EditorIndentGuideBackground, EditorIndentGuideActiveBackground [6]ui.Color
	// EditorBracketPairGuideBackground and EditorBracketPairGuideActiveBackground are the
	// colors of the bracket pair guides by level; a transparent one is made of the color of the
	// brackets of that level.
	EditorBracketPairGuideBackground, EditorBracketPairGuideActiveBackground [6]ui.Color
	EditorRulerForeground                                                    ui.Color
	EditorBracketMatchBackground                                             ui.Color
	EditorBracketMatchBorder                                                 ui.Color
	EditorBracketMatchForeground                                             ui.Color
	// EditorBracketHighlightForeground are the colors of the levels of brackets, from the
	// first; a level past the sixth takes the color of the one that repeats it. A transparent
	// color leaves the bracket in the color of its token.
	EditorBracketHighlightForeground [6]ui.Color
	// EditorBracketUnexpectedForeground is the color of a closing bracket that closes
	// nothing and of an opening bracket that is never closed.
	EditorBracketUnexpectedForeground ui.Color
	ScrollbarSliderBackground         ui.Color
	ScrollbarSliderHoverBackground    ui.Color
	ScrollbarSliderActiveBackground   ui.Color
	ScrollbarShadow                   ui.Color
	FocusBorder                       ui.Color
	Foreground                        ui.Color
	WidgetShadow                      ui.Color
}

// ColorsOf gives the colors of a theme: for each field, the color the theme sets or else the
// default of the color registry. A color that has neither (editor.lineHighlightBackground in
// most themes) is transparent.
func ColorsOf(ct *theme.ColorTheme) Colors {
	color := func(id string) ui.Color {
		c, ok := ct.Color(id)
		if !ok {
			return ui.NewColor(0)
		}
		return ui.NewColor(c.ARGB())
	}
	return Colors{
		EditorBackground:                       color("editor.background"),
		EditorForeground:                       color("editor.foreground"),
		EditorLineHighlightBackground:          color("editor.lineHighlightBackground"),
		EditorLineHighlightBorder:              color("editor.lineHighlightBorder"),
		EditorSelectionBackground:              color("editor.selectionBackground"),
		EditorInactiveSelectionBackground:      color("editor.inactiveSelectionBackground"),
		EditorCursorForeground:                 color("editorCursor.foreground"),
		EditorLineNumberForeground:             color("editorLineNumber.foreground"),
		EditorLineNumberActiveForeground:       color("editorLineNumber.activeForeground"),
		EditorGutterBackground:                 color("editorGutter.background"),
		EditorWhitespaceForeground:             color("editorWhitespace.foreground"),
		EditorIndentGuideBackground1:           color("editorIndentGuide.background1"),
		EditorIndentGuideBackground:            guideColors(color, "editorIndentGuide.background"),
		EditorIndentGuideActiveBackground:      guideColors(color, "editorIndentGuide.activeBackground"),
		EditorBracketPairGuideBackground:       guideColors(color, "editorBracketPairGuide.background"),
		EditorBracketPairGuideActiveBackground: guideColors(color, "editorBracketPairGuide.activeBackground"),
		EditorRulerForeground:                  color("editorRuler.foreground"),
		EditorBracketMatchBackground:           color("editorBracketMatch.background"),
		EditorBracketMatchBorder:               color("editorBracketMatch.border"),
		EditorBracketMatchForeground:           color("editorBracketMatch.foreground"),
		EditorBracketHighlightForeground: [6]ui.Color{
			color("editorBracketHighlight.foreground1"), color("editorBracketHighlight.foreground2"),
			color("editorBracketHighlight.foreground3"), color("editorBracketHighlight.foreground4"),
			color("editorBracketHighlight.foreground5"), color("editorBracketHighlight.foreground6"),
		},
		EditorBracketUnexpectedForeground: color("editorBracketHighlight.unexpectedBracket.foreground"),
		ScrollbarSliderBackground:         color("scrollbarSlider.background"),
		ScrollbarSliderHoverBackground:    color("scrollbarSlider.hoverBackground"),
		ScrollbarSliderActiveBackground:   color("scrollbarSlider.activeBackground"),
		ScrollbarShadow:                   color("scrollbar.shadow"),
		FocusBorder:                       color("focusBorder"),
		Foreground:                        color("foreground"),
		WidgetShadow:                      color("widget.shadow"),
	}
}

// DarkModern are the colors of the bundled Dark Modern theme, which a widget shown on its own
// (with no Theme above it) uses.
func DarkModern() Colors {
	t, err := theme.LoadBundled(theme.DefaultDark)
	if err != nil {
		// The theme is embedded in the program, so this is a broken build.
		panic(err)
	}
	return ColorsOf(theme.NewColorTheme(t, theme.DefaultColorRegistry()))
}

// Theme is the inherited widget that gives its subtree the workbench colors and, for the editors,
// the theme that colors tokens.
type Theme struct {
	widgets.InheritedWidget
	colors Colors
	tokens *theme.TokenTheme
}

// ThemeOpts are the options of NewTheme.
type ThemeOpts struct {
	// Tokens gives the colors and font styles of the tokens of a language (SPEC §8.4). Editors
	// below a Theme without it show their text in the editor's foreground color.
	Tokens *theme.TokenTheme
}

// NewTheme makes a Theme with the given colors for the child.
func NewTheme(colors Colors, child widgets.Widget, opts ...ThemeOpts) *Theme {
	t := &Theme{InheritedWidget: widgets.NewInheritedWidgetBase(child), colors: colors}
	if len(opts) > 0 {
		t.tokens = opts[0].Tokens
	}
	return t
}

// Colors are the colors the Theme gives.
func (t *Theme) Colors() Colors { return t.colors }

// UpdateShouldNotify implements widgets.InheritedWidgetInterface: dependents are rebuilt
// when a color or the token theme changed.
func (t *Theme) UpdateShouldNotify(old widgets.Widget) bool {
	o := old.(*Theme)
	return t.colors != o.colors || t.tokens != o.tokens
}

// ThemeOf returns the colors of the nearest Theme above bc, and rebuilds the caller when
// they change. Without a Theme it returns DarkModern, so a widget can be shown on its own.
func ThemeOf(bc widgets.BuildContext) Colors {
	if t, ok := widgets.DependOn[*Theme](bc); ok {
		return t.colors
	}
	return DarkModern()
}

// TokenThemeOf returns the token theme of the nearest Theme above bc, or nil when there is none
// or it has none, and rebuilds the caller when it changes.
func TokenThemeOf(bc widgets.BuildContext) *theme.TokenTheme {
	if t, ok := widgets.DependOn[*Theme](bc); ok {
		return t.tokens
	}
	return nil
}

// guideColors reads the six colors of a family of guide colors (id1 to id6).
func guideColors(color func(string) ui.Color, prefix string) [6]ui.Color {
	var out [6]ui.Color
	for i := range out {
		out[i] = color(prefix + string(rune('1'+i)))
	}
	return out
}
