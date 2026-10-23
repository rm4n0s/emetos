package theme

import (
	"sort"

	"github.com/rm4n0s/errors"
)

// ColorEnv is what a derived color asks of the theme it is resolved in.
type ColorEnv interface {
	// Color gives the color with an id in the theme: the one the theme sets, else its default.
	Color(id string) (Color, bool)
	// Defines tells whether the theme itself sets the color.
	Defines(id string) bool
}

// ColorValue is the default of a workbench color: a color, the color of another id, or a color
// derived from others. A nil ColorValue is no color.
//
// Ported from VS Code: src/vs/platform/theme/common/colorUtils.ts (ColorValue, ColorTransform)
type ColorValue interface {
	resolve(env ColorEnv, depth int) (Color, bool)
}

// maxColorDepth is how many colors may refer to one another when a color is resolved; a longer
// chain is a loop, and the color has no value.
const maxColorDepth = 32

// resolveValue resolves an optional color value.
func resolveValue(v ColorValue, env ColorEnv, depth int) (Color, bool) {
	if v == nil || depth > maxColorDepth {
		return Color{}, false
	}
	return v.resolve(env, depth+1)
}

type literal struct{ c Color }

func (l literal) resolve(ColorEnv, int) (Color, bool) { return l.c, true }

// Lit is a color value that is the color c.
func Lit(c Color) ColorValue { return literal{c} }

// Hex is a color value written as #rgb, #rgba, #rrggbb or #rrggbbaa. A text that is not a color
// is red, as VS Code's Color.fromHex has it.
func Hex(s string) ColorValue {
	c, ok := ParseColor(s)
	if !ok {
		c = NewColor(255, 0, 0, 1)
	}
	return literal{c}
}

type reference struct{ id string }

func (r reference) resolve(env ColorEnv, depth int) (Color, bool) {
	if d, ok := env.(depthEnv); ok {
		return d.colorAt(r.id, depth)
	}
	return env.Color(r.id)
}

// depthEnv is a ColorEnv that tells how deep the references are, to stop a loop of colors that
// refer to each other.
type depthEnv interface {
	colorAt(id string, depth int) (Color, bool)
}

// Ref is a color value that is the color of another id in the theme.
func Ref(id string) ColorValue { return reference{id} }

type transform struct {
	op       string
	value    ColorValue
	other    ColorValue // background, or the color mixed with
	factor   float64
	factor2  float64
	values   []ColorValue
	ifID     string
	thenElse [2]ColorValue
}

// Transparent is the value with its alpha multiplied by factor.
func Transparent(v ColorValue, factor float64) ColorValue {
	return transform{op: "transparent", value: v, factor: factor}
}

// Darken is the value, darker by the factor.
func Darken(v ColorValue, factor float64) ColorValue {
	return transform{op: "darken", value: v, factor: factor}
}

// Lighten is the value, lighter by the factor.
func Lighten(v ColorValue, factor float64) ColorValue {
	return transform{op: "lighten", value: v, factor: factor}
}

// Mix is the value mixed with another by the ratio (0 the value, 1 the other).
func Mix(v, with ColorValue, ratio float64) ColorValue {
	return transform{op: "mix", value: v, other: with, factor: ratio}
}

// Opaque is the value made opaque over a background.
func Opaque(v, background ColorValue) ColorValue {
	return transform{op: "opaque", value: v, other: background}
}

// OneOf is the first of the values that has a color.
func OneOf(values ...ColorValue) ColorValue { return transform{op: "oneOf", values: values} }

// IfDefinedThenElse is the then value when the theme sets the color id, else the else value.
func IfDefinedThenElse(id string, then, otherwise ColorValue) ColorValue {
	return transform{op: "ifDefined", ifID: id, thenElse: [2]ColorValue{then, otherwise}}
}

// LessProminent is the value moved away from the background, so as to be less prominent, by the
// factor and made transparent by the transparency.
func LessProminent(v, background ColorValue, factor, transparency float64) ColorValue {
	return transform{op: "lessProminent", value: v, other: background, factor: factor, factor2: transparency}
}

// resolve executes the transform.
//
// Ported from VS Code: src/vs/platform/theme/common/colorUtils.ts (executeTransform)
func (t transform) resolve(env ColorEnv, depth int) (Color, bool) {
	switch t.op {
	case "darken":
		c, ok := resolveValue(t.value, env, depth)
		return c.Darken(t.factor), ok
	case "lighten":
		c, ok := resolveValue(t.value, env, depth)
		return c.Lighten(t.factor), ok
	case "transparent":
		c, ok := resolveValue(t.value, env, depth)
		return c.WithAlphaFactor(t.factor), ok
	case "mix":
		primary, ok := resolveValue(t.value, env, depth)
		if !ok {
			primary = ColorTransparent
		}
		other, ok := resolveValue(t.other, env, depth)
		if !ok {
			other = ColorTransparent
		}
		return primary.Mix(other, t.factor), true
	case "opaque":
		background, hasBackground := resolveValue(t.other, env, depth)
		c, ok := resolveValue(t.value, env, depth)
		if !hasBackground {
			return c, ok
		}
		return c.MakeOpaque(background), ok
	case "oneOf":
		for _, v := range t.values {
			if c, ok := resolveValue(v, env, depth); ok {
				return c, true
			}
		}
		return Color{}, false
	case "ifDefined":
		if env.Defines(t.ifID) {
			return resolveValue(t.thenElse[0], env, depth)
		}
		return resolveValue(t.thenElse[1], env, depth)
	case "lessProminent":
		from, ok := resolveValue(t.value, env, depth)
		if !ok {
			return Color{}, false
		}
		background, hasBackground := resolveValue(t.other, env, depth)
		if !hasBackground {
			return from.WithAlphaFactor(t.factor * t.factor2), true
		}
		if from.IsDarkerThan(background) {
			return lighterColor(from, background, t.factor).WithAlphaFactor(t.factor2), true
		}
		return darkerColor(from, background, t.factor).WithAlphaFactor(t.factor2), true
	}
	return Color{}, false
}

// ColorDefaults are the defaults of a workbench color for each theme type. A nil value is no
// default: the color is not painted unless the theme sets it.
type ColorDefaults struct {
	Dark, Light, HCDark, HCLight ColorValue
}

// AllThemes are the same default for every theme type.
func AllThemes(v ColorValue) ColorDefaults {
	return ColorDefaults{Dark: v, Light: v, HCDark: v, HCLight: v}
}

// forType gives the default for a theme type.
func (d ColorDefaults) forType(t Type) ColorValue {
	switch t {
	case TypeLight:
		return d.Light
	case TypeHCDark:
		return d.HCDark
	case TypeHCLight:
		return d.HCLight
	}
	return d.Dark
}

// ColorContribution is a workbench color that the registry knows.
type ColorContribution struct {
	// ID is the color's id in a theme's "colors": "editor.background".
	ID string
	// Description says what the color paints.
	Description string
	// Defaults are the colors of a theme that does not set it.
	Defaults ColorDefaults
	// NeedsTransparency says that the color must not be opaque, as it is painted over what it
	// marks (a selection).
	NeedsTransparency bool
	// DeprecationMessage is set for an id that a newer one replaces.
	DeprecationMessage string
}

// ColorRegistry holds the workbench colors with their defaults (SPEC §8.4). Each step that
// first paints a color registers it here, with the values of VS Code.
//
// Ported from VS Code: src/vs/platform/theme/common/colorUtils.ts (ColorRegistry)
type ColorRegistry struct {
	byID map[string]ColorContribution
}

// NewColorRegistry makes an empty registry.
func NewColorRegistry() *ColorRegistry { return &ColorRegistry{byID: map[string]ColorContribution{}} }

// Register adds a color. The error has the tag ColorInvalid when the id is empty and
// ColorAlreadyRegistered when it is taken.
func (r *ColorRegistry) Register(c ColorContribution) error {
	if c.ID == "" {
		return errors.New("ColorInvalid", "a color needs an id")
	}
	if _, taken := r.byID[c.ID]; taken {
		return errors.New("ColorAlreadyRegistered", "a color with this id is registered", "id", c.ID)
	}
	r.byID[c.ID] = c
	return nil
}

// MustRegister is Register for the colors of the program, whose registration cannot fail unless
// the list has a mistake; it panics with the error.
func (r *ColorRegistry) MustRegister(colors ...ColorContribution) {
	for _, c := range colors {
		if err := r.Register(c); err != nil {
			panic(err)
		}
	}
}

// Lookup gives the color with an id.
func (r *ColorRegistry) Lookup(id string) (ColorContribution, bool) {
	c, ok := r.byID[id]
	return c, ok
}

// Colors lists the registered colors by id.
func (r *ColorRegistry) Colors() []ColorContribution {
	out := make([]ColorContribution, 0, len(r.byID))
	for _, c := range r.byID {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
