package theme

import (
	"fmt"
	"math"
	"strconv"
)

// Color is a color with an alpha, with the operations that workbench colors derive each other
// with. It follows VS Code's Color: the red, green and blue are whole numbers from 0 to 255, the
// alpha is rounded to three decimals, and lightening and darkening go through hue, saturation
// and lightness.
//
// Ported from VS Code: src/vs/base/common/color.ts
type Color struct {
	r, g, b int
	a       float64
	// hsla is the lightness form when the color was made from one; VS Code keeps it, so that
	// lightening and darkening in turn do not round to RGB in between.
	hsla *hsla
}

// hsla is a color as hue (0 to 360), saturation, lightness and alpha (0 to 1).
type hsla struct {
	h       int
	s, l, a float64
}

// roundFloat rounds as JavaScript's Math.round does (half up) to a number of decimals.
func roundFloat(n float64, decimals int) float64 {
	scale := math.Pow(10, float64(decimals))
	return math.Floor(n*scale+0.5) / scale
}

func clampInt255(n float64) int { return int(math.Min(255, math.Max(0, n))) }

// NewColor makes a color: red, green and blue are cut to 0..255 (the fraction is dropped), the
// alpha is cut to 0..1 and rounded to three decimals.
func NewColor(r, g, b, a float64) Color {
	return Color{r: clampInt255(r), g: clampInt255(g), b: clampInt255(b), a: roundFloat(math.Max(math.Min(1, a), 0), 3)}
}

// The colors that VS Code names.
var (
	// ColorWhite is #ffffff.
	ColorWhite = NewColor(255, 255, 255, 1)
	// ColorBlack is #000000.
	ColorBlack = NewColor(0, 0, 0, 1)
	// ColorLightGrey is #d3d3d3.
	ColorLightGrey = NewColor(211, 211, 211, 1)
	// ColorTransparent is black with no opacity.
	ColorTransparent = NewColor(0, 0, 0, 0)
)

// ParseColor reads #rgb, #rgba, #rrggbb or #rrggbbaa. A character that is not a hex digit counts as
// 0, as in VS Code. ok is false when the text does not start with # or has another length.
//
// Ported from VS Code: src/vs/base/common/color.ts (Color.Format.CSS.parseHex)
func ParseColor(s string) (c Color, ok bool) {
	if len(s) == 0 || s[0] != '#' {
		return Color{}, false
	}
	digit := func(i int) int {
		switch ch := s[i]; {
		case ch >= '0' && ch <= '9':
			return int(ch - '0')
		case ch >= 'a' && ch <= 'f':
			return int(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			return int(ch-'A') + 10
		}
		return 0
	}
	var v []int
	for i := 1; i < len(s); i++ {
		v = append(v, digit(i))
	}
	switch len(v) {
	case 6:
		return NewColor(float64(16*v[0]+v[1]), float64(16*v[2]+v[3]), float64(16*v[4]+v[5]), 1), true
	case 8:
		return NewColor(float64(16*v[0]+v[1]), float64(16*v[2]+v[3]), float64(16*v[4]+v[5]), float64(16*v[6]+v[7])/255), true
	case 3:
		return NewColor(float64(17*v[0]), float64(17*v[1]), float64(17*v[2]), 1), true
	case 4:
		return NewColor(float64(17*v[0]), float64(17*v[1]), float64(17*v[2]), float64(17*v[3])/255), true
	}
	return Color{}, false
}

// RGBA gives the red, green and blue (0 to 255) and the alpha (0 to 1).
func (c Color) RGBA() (r, g, b int, a float64) { return c.r, c.g, c.b, c.a }

// Equal tells whether two colors are the same in red, green, blue and alpha.
func (c Color) Equal(o Color) bool {
	return c.r == o.r && c.g == o.g && c.b == o.b && c.a == o.a
}

// ARGB is the color as 0xAARRGGBB, which is how Glutter writes a color.
func (c Color) ARGB() uint32 {
	return uint32(math.Round(c.a*255))<<24 | uint32(c.r)<<16 | uint32(c.g)<<8 | uint32(c.b)
}

// Hex writes the color as #rrggbb, or #rrggbbaa when it is not opaque.
func (c Color) Hex() string {
	if c.a == 1 {
		return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.r, c.g, c.b, int(math.Floor(c.a*255+0.5)))
}

// String writes the color as VS Code does: #rrggbb when it is opaque, else rgba(r, g, b, a).
func (c Color) String() string {
	if c.a == 1 {
		return c.Hex()
	}
	alpha := strconv.FormatFloat(math.Floor(c.a*100+0.5)/100, 'f', -1, 64)
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", c.r, c.g, c.b, alpha)
}

// IsOpaque tells whether the alpha is 1.
func (c Color) IsOpaque() bool { return c.a == 1 }

// toHSLA is HSLA.fromRGBA.
func (c Color) toHSLA() hsla {
	if c.hsla != nil {
		return *c.hsla
	}
	r, g, b := float64(c.r)/255, float64(c.g)/255, float64(c.b)/255
	maxC, minC := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	var h, s float64
	l := (minC + maxC) / 2
	chroma := maxC - minC
	if chroma > 0 {
		if l <= 0.5 {
			s = chroma / (2 * l)
		} else {
			s = chroma / (2 - 2*l)
		}
		s = math.Min(s, 1)
		switch maxC {
		case r:
			h = (g - b) / chroma
			if g < b {
				h += 6
			}
		case g:
			h = (b-r)/chroma + 2
		default:
			h = (r-g)/chroma + 4
		}
		h = math.Floor(h*60 + 0.5)
	}
	return makeHSLA(h, s, l, c.a)
}

func makeHSLA(h, s, l, a float64) hsla {
	return hsla{
		h: int(math.Max(math.Min(360, h), 0)),
		s: roundFloat(math.Max(math.Min(1, s), 0), 3),
		l: roundFloat(math.Max(math.Min(1, l), 0), 3),
		a: roundFloat(math.Max(math.Min(1, a), 0), 3),
	}
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6:
		return p + (q-p)*6*t
	case t < 1.0/2:
		return q
	case t < 2.0/3:
		return p + (q-p)*(2.0/3-t)*6
	}
	return p
}

// fromHSLA is Color(HSLA).
func fromHSLA(h hsla) Color {
	hue := float64(h.h) / 360
	var r, g, b float64
	if h.s == 0 {
		r, g, b = h.l, h.l, h.l
	} else {
		var q float64
		if h.l < 0.5 {
			q = h.l * (1 + h.s)
		} else {
			q = h.l + h.s - h.l*h.s
		}
		p := 2*h.l - q
		r, g, b = hueToRGB(p, q, hue+1.0/3), hueToRGB(p, q, hue), hueToRGB(p, q, hue-1.0/3)
	}
	c := NewColor(math.Floor(r*255+0.5), math.Floor(g*255+0.5), math.Floor(b*255+0.5), h.a)
	c.hsla = &h
	return c
}

// Lighten raises the lightness by the factor of itself (0.1 is a tenth more).
func (c Color) Lighten(factor float64) Color {
	h := c.toHSLA()
	return fromHSLA(makeHSLA(float64(h.h), h.s, h.l+h.l*factor, h.a))
}

// Darken lowers the lightness by the factor of itself.
func (c Color) Darken(factor float64) Color {
	h := c.toHSLA()
	return fromHSLA(makeHSLA(float64(h.h), h.s, h.l-h.l*factor, h.a))
}

// WithAlphaFactor multiplies the alpha by the factor; VS Code calls it transparent().
func (c Color) WithAlphaFactor(factor float64) Color {
	return NewColor(float64(c.r), float64(c.g), float64(c.b), c.a*factor)
}

// Mix mixes the color with another: a factor of 0 is this color, 1 the other, 0.5 half of each.
func (c Color) Mix(other Color, factor float64) Color {
	f := math.Min(math.Max(factor, 0), 1)
	return NewColor(
		float64(c.r)+float64(other.r-c.r)*f,
		float64(c.g)+float64(other.g-c.g)*f,
		float64(c.b)+float64(other.b-c.b)*f,
		c.a+(other.a-c.a)*f)
}

// Blend gives the color that this one makes when it is laid over a background.
func (c Color) Blend(background Color) Color {
	a := c.a + background.a*(1-c.a)
	if a < 1e-6 {
		return ColorTransparent
	}
	mix := func(top, under int) float64 {
		return float64(top)*c.a/a + float64(under)*background.a*(1-c.a)/a
	}
	return NewColor(mix(c.r, background.r), mix(c.g, background.g), mix(c.b, background.b), a)
}

// MakeOpaque gives the opaque color that looks like this one over an opaque background.
func (c Color) MakeOpaque(background Color) Color {
	if c.a == 1 || background.a != 1 {
		return c
	}
	return NewColor(
		float64(background.r)-c.a*float64(background.r-c.r),
		float64(background.g)-c.a*float64(background.g-c.g),
		float64(background.b)-c.a*float64(background.b-c.b),
		1)
}

// RelativeLuminance is the WCAG relative luminance, from 0 (black) to 1 (white).
func (c Color) RelativeLuminance() float64 {
	component := func(v int) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return roundFloat(0.2126*component(c.r)+0.7152*component(c.g)+0.0722*component(c.b), 4)
}

// IsLighterThan tells whether the color has a higher relative luminance than another.
func (c Color) IsLighterThan(o Color) bool { return c.RelativeLuminance() > o.RelativeLuminance() }

// IsDarkerThan tells whether the color has a lower relative luminance than another.
func (c Color) IsDarkerThan(o Color) bool { return c.RelativeLuminance() < o.RelativeLuminance() }

// lighterColor is Color.getLighterColor: the color, lightened toward the relative color unless it
// is lighter already. A factor of 0 is 0.5.
func lighterColor(of, relative Color, factor float64) Color {
	if of.IsLighterThan(relative) {
		return of
	}
	if factor == 0 {
		factor = 0.5
	}
	lum1, lum2 := of.RelativeLuminance(), relative.RelativeLuminance()
	return of.Lighten(factor * (lum2 - lum1) / lum2)
}

// darkerColor is Color.getDarkerColor.
func darkerColor(of, relative Color, factor float64) Color {
	if of.IsDarkerThan(relative) {
		return of
	}
	if factor == 0 {
		factor = 0.5
	}
	lum1, lum2 := of.RelativeLuminance(), relative.RelativeLuminance()
	return of.Darken(factor * (lum1 - lum2) / lum1)
}
