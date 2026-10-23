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
	EditorRulerForeground             ui.Color
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
		EditorBackground:                  color("editor.background"),
		EditorForeground:                  color("editor.foreground"),
		EditorLineHighlightBackground:     color("editor.lineHighlightBackground"),
		EditorLineHighlightBorder:         color("editor.lineHighlightBorder"),
		EditorSelectionBackground:         color("editor.selectionBackground"),
		EditorInactiveSelectionBackground: color("editor.inactiveSelectionBackground"),
		EditorCursorForeground:            color("editorCursor.foreground"),
		EditorLineNumberForeground:        color("editorLineNumber.foreground"),
		EditorLineNumberActiveForeground:  color("editorLineNumber.activeForeground"),
		EditorGutterBackground:            color("editorGutter.background"),
		EditorWhitespaceForeground:        color("editorWhitespace.foreground"),
		EditorIndentGuideBackground1:      color("editorIndentGuide.background1"),
		EditorRulerForeground:             color("editorRuler.foreground"),
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

// Theme is the inherited widget that gives its subtree the workbench colors.
type Theme struct {
	widgets.InheritedWidget
	colors Colors
}

// NewTheme makes a Theme with the given colors for the child.
func NewTheme(colors Colors, child widgets.Widget) *Theme {
	return &Theme{InheritedWidget: widgets.NewInheritedWidgetBase(child), colors: colors}
}

// Colors are the colors the Theme gives.
func (t *Theme) Colors() Colors { return t.colors }

// UpdateShouldNotify implements widgets.InheritedWidgetInterface: dependents are rebuilt
// when a color changed.
func (t *Theme) UpdateShouldNotify(old widgets.Widget) bool {
	return t.colors != old.(*Theme).colors
}

// ThemeOf returns the colors of the nearest Theme above bc, and rebuilds the caller when
// they change. Without a Theme it returns DarkModern, so a widget can be shown on its own.
func ThemeOf(bc widgets.BuildContext) Colors {
	if t, ok := widgets.DependOn[*Theme](bc); ok {
		return t.colors
	}
	return DarkModern()
}
