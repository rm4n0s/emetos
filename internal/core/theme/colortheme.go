package theme

// ColorTheme is a theme with the registry its colors default from: it gives the workbench color
// of any id, from what the theme sets or else from the default of the registry for the type of
// the theme.
//
// Ported from VS Code: src/vs/workbench/services/themes/common/colorThemeData.ts (getColor, getDefault)
type ColorTheme struct {
	theme    *Theme
	registry *ColorRegistry
	set      map[string]Color
}

// NewColorTheme makes the color theme of a theme. A color the theme gives in a form that is not
// a color is red, as VS Code reads it.
func NewColorTheme(t *Theme, r *ColorRegistry) *ColorTheme {
	set := make(map[string]Color, len(t.Colors))
	for id, value := range t.Colors {
		c, ok := ParseColor(value)
		if !ok {
			c = NewColor(255, 0, 0, 1)
		}
		set[id] = c
	}
	return &ColorTheme{theme: t, registry: r, set: set}
}

// Theme is the theme the colors come from.
func (ct *ColorTheme) Theme() *Theme { return ct.theme }

// Type is the type of the theme.
func (ct *ColorTheme) Type() Type { return ct.theme.Type }

// Defines tells whether the theme sets the color.
func (ct *ColorTheme) Defines(id string) bool {
	_, ok := ct.set[id]
	return ok
}

// Color gives the color with an id: the one the theme sets, else the default of the registry for
// the theme's type. ok is false when neither is a color.
func (ct *ColorTheme) Color(id string) (Color, bool) { return ct.colorAt(id, 0) }

func (ct *ColorTheme) colorAt(id string, depth int) (Color, bool) {
	if c, ok := ct.set[id]; ok {
		return c, true
	}
	return ct.defaultColor(id, depth)
}

// Default gives the default color of the registry for an id in this theme.
func (ct *ColorTheme) Default(id string) (Color, bool) { return ct.defaultColor(id, 0) }

func (ct *ColorTheme) defaultColor(id string, depth int) (Color, bool) {
	contribution, ok := ct.registry.Lookup(id)
	if !ok {
		return Color{}, false
	}
	return resolveValue(contribution.Defaults.forType(ct.theme.Type), ct, depth)
}
