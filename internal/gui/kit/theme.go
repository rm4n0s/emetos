package kit

import (
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// Colors are the workbench colors the widgets read, named as VS Code names them in a color
// theme's "colors". Step 4.8 fills them from the theme service; until then DarkModern
// gives their values.
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

// DarkModern are the values of VS Code's Dark Modern theme. The theme file
// (extensions/theme-defaults/themes/dark_modern.json, with its includes dark_plus.json and
// dark_vs.json) sets some of the colors; the others have the dark defaults of VS Code's
// color registry (src/vs/editor/common/core/editorColorRegistry.ts and
// src/vs/platform/theme/common/colors/).
func DarkModern() Colors {
	return Colors{
		EditorBackground:                  ui.NewColor(0xFF1F1F1F), // dark_modern.json
		EditorForeground:                  ui.NewColor(0xFFCCCCCC), // dark_modern.json
		EditorLineHighlightBackground:     ui.NewColor(0x00000000), // registry: none
		EditorLineHighlightBorder:         ui.NewColor(0xFF282828), // registry
		EditorSelectionBackground:         ui.NewColor(0xFF264F78), // registry
		EditorInactiveSelectionBackground: ui.NewColor(0xFF3A3D41), // dark_modern.json
		EditorCursorForeground:            ui.NewColor(0xFFAEAFAD), // registry
		EditorLineNumberForeground:        ui.NewColor(0xFF6E7681), // dark_modern.json
		EditorLineNumberActiveForeground:  ui.NewColor(0xFFCCCCCC), // dark_modern.json
		EditorGutterBackground:            ui.NewColor(0xFF1F1F1F), // registry: editor.background
		EditorWhitespaceForeground:        ui.NewColor(0x29E3E4E2), // registry: #e3e4e229
		EditorIndentGuideBackground1:      ui.NewColor(0xFF404040), // dark_modern.json
		EditorRulerForeground:             ui.NewColor(0xFF5A5A5A), // registry
		ScrollbarSliderBackground:         ui.NewColor(0x66797979), // registry: #797979 at 40%
		ScrollbarSliderHoverBackground:    ui.NewColor(0xB3646464), // registry: #646464 at 70%
		ScrollbarSliderActiveBackground:   ui.NewColor(0x66BFBFBF), // registry: #BFBFBF at 40%
		ScrollbarShadow:                   ui.NewColor(0xFF000000), // registry
		FocusBorder:                       ui.NewColor(0xFF0078D4), // dark_modern.json
		Foreground:                        ui.NewColor(0xFFCCCCCC), // dark_modern.json
		WidgetShadow:                      ui.NewColor(0x5C000000), // registry: black at 36%
	}
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
