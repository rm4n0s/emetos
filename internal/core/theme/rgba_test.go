package theme

import "testing"

func hslaColor(h int, s, l, a float64) Color { return fromHSLA(makeHSLA(float64(h), s, l, a)) }

func hex(t *testing.T, s string) Color {
	t.Helper()
	c, ok := ParseColor(s)
	if !ok {
		t.Fatalf("%q is not a color", s)
	}
	return c
}

// Ported from VS Code: src/vs/base/test/common/color.test.ts ('isLighterColor', 'getLighterColor',
// 'isDarkerColor', 'getDarkerColor')
func TestLighterAndDarker(t *testing.T) {
	color1, color2 := hslaColor(60, 1, 0.5, 1), hslaColor(0, 0, 0.753, 1)
	if !color1.IsLighterThan(color2) || !hex(t, "#770811").IsLighterThan(hex(t, "#000c18")) || !color2.IsDarkerThan(color1) {
		t.Error("lighter and darker")
	}
	cases := []struct {
		name string
		got  Color
		want hsla
	}{
		{"lighter of the lighter is itself", lighterColor(color1, color2, 0), color1.toHSLA()},
		{"lighter default", lighterColor(color2, color1, 0), makeHSLA(0, 0, 0.916, 1)},
		{"lighter 0.3", lighterColor(color2, color1, 0.3), makeHSLA(0, 0, 0.851, 1)},
		{"lighter 0.7", lighterColor(color2, color1, 0.7), makeHSLA(0, 0, 0.981, 1)},
		{"lighter 1", lighterColor(color2, color1, 1), makeHSLA(0, 0, 1, 1)},
		{"darker of the darker is itself", darkerColor(color2, color1, 0), color2.toHSLA()},
		{"darker default", darkerColor(color1, color2, 0), makeHSLA(60, 1, 0.392, 1)},
		{"darker 0.3", darkerColor(color1, color2, 0.3), makeHSLA(60, 1, 0.435, 1)},
		{"darker 0.7", darkerColor(color1, color2, 0.7), makeHSLA(60, 1, 0.349, 1)},
		{"darker 1", darkerColor(color1, color2, 1), makeHSLA(60, 1, 0.284, 1)},
		{"darker, Abyss", darkerColor(hex(t, "#770811"), hex(t, "#000c18"), 0.4), makeHSLA(355, 0.874, 0.157, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.got.toHSLA(); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Ported from VS Code: src/vs/base/test/common/color.test.ts ('luminance')
func TestLuminance(t *testing.T) {
	cases := []struct {
		r, g, b float64
		want    float64
	}{
		{0, 0, 0, 0}, {255, 255, 255, 1}, {255, 0, 0, 0.2126}, {0, 255, 0, 0.7152}, {0, 0, 255, 0.0722},
		{255, 255, 0, 0.9278}, {0, 255, 255, 0.7874}, {255, 0, 255, 0.2848}, {192, 192, 192, 0.5271},
		{128, 128, 128, 0.2159}, {128, 0, 0, 0.0459}, {128, 128, 0, 0.2003}, {0, 128, 0, 0.1544},
		{128, 0, 128, 0.0615}, {0, 128, 128, 0.17}, {0, 0, 128, 0.0156},
	}
	for _, tc := range cases {
		if got := NewColor(tc.r, tc.g, tc.b, 1).RelativeLuminance(); got != tc.want {
			t.Errorf("luminance of %v,%v,%v = %v, want %v", tc.r, tc.g, tc.b, got, tc.want)
		}
	}
}

// Ported from VS Code: src/vs/base/test/common/color.test.ts ('blending')
func TestBlend(t *testing.T) {
	cases := []struct{ top, background, want Color }{
		{NewColor(0, 0, 0, 0), NewColor(243, 34, 43, 1), NewColor(243, 34, 43, 1)},
		{NewColor(255, 255, 255, 1), NewColor(243, 34, 43, 1), NewColor(255, 255, 255, 1)},
		{NewColor(122, 122, 122, 0.7), NewColor(243, 34, 43, 1), NewColor(158, 95, 98, 1)},
		{NewColor(0, 0, 0, 0.58), NewColor(255, 255, 255, 0.33), NewColor(49, 49, 49, 0.719)},
	}
	for _, tc := range cases {
		if got := tc.top.Blend(tc.background); !got.Equal(tc.want) {
			t.Errorf("%v over %v = %v, want %v", tc.top, tc.background, got, tc.want)
		}
	}
}

// Ported from VS Code: src/vs/base/test/common/color.test.ts ('toString')
func TestColorString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"#00000000", "rgba(0, 0, 0, 0)"}, {"#00000080", "rgba(0, 0, 0, 0.5)"}, {"#000000FF", "#000000"},
		{"#FFFFFF", "#ffffff"}, {"#0a0A0a", "#0a0a0a"}, {"#010203", "#010203"},
	}
	for _, tc := range cases {
		if got := hex(t, tc.in).String(); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Ported from VS Code: src/vs/base/test/common/color.test.ts ('HSLA.toRGBA', 'HSLA.fromRGBA')
func TestHSLAConversions(t *testing.T) {
	cases := []struct {
		r, g, b int
		h       hsla
	}{
		{0, 0, 0, makeHSLA(0, 0, 0, 1)}, {255, 255, 255, makeHSLA(0, 0, 1, 1)}, {255, 0, 0, makeHSLA(0, 1, 0.5, 1)},
		{0, 255, 0, makeHSLA(120, 1, 0.5, 1)}, {0, 0, 255, makeHSLA(240, 1, 0.5, 1)}, {255, 255, 0, makeHSLA(60, 1, 0.5, 1)},
		{0, 255, 255, makeHSLA(180, 1, 0.5, 1)}, {255, 0, 255, makeHSLA(300, 1, 0.5, 1)}, {192, 192, 192, makeHSLA(0, 0, 0.753, 1)},
		{128, 128, 128, makeHSLA(0, 0, 0.502, 1)}, {128, 0, 0, makeHSLA(0, 1, 0.251, 1)}, {128, 128, 0, makeHSLA(60, 1, 0.251, 1)},
		{0, 128, 0, makeHSLA(120, 1, 0.251, 1)}, {128, 0, 128, makeHSLA(300, 1, 0.251, 1)}, {0, 128, 128, makeHSLA(180, 1, 0.251, 1)},
		{0, 0, 128, makeHSLA(240, 1, 0.251, 1)},
	}
	for _, tc := range cases {
		c := NewColor(float64(tc.r), float64(tc.g), float64(tc.b), 1)
		if got := c.toHSLA(); got != tc.h {
			t.Errorf("rgb(%d,%d,%d) to HSLA = %+v, want %+v", tc.r, tc.g, tc.b, got, tc.h)
		}
		back := fromHSLA(tc.h)
		if r, g, b, _ := back.RGBA(); r != tc.r || g != tc.g || b != tc.b {
			t.Errorf("%+v to RGBA = %d,%d,%d, want %d,%d,%d", tc.h, r, g, b, tc.r, tc.g, tc.b)
		}
	}
	// The transparent black of the first cases of the VS Code test.
	if got := NewColor(0, 0, 0, 0).toHSLA(); got != makeHSLA(0, 0, 0, 0) {
		t.Errorf("transparent black: %+v", got)
	}
}

// Ported from VS Code: src/vs/base/test/common/color.test.ts ('parseHex')
func TestParseColor(t *testing.T) {
	invalid := []string{"", "#", "#0102030", "red", "010203", "#12"}
	for _, s := range invalid {
		if _, ok := ParseColor(s); ok {
			t.Errorf("%q is not a color", s)
		}
	}
	cases := []struct {
		in      string
		r, g, b int
		a       float64
	}{
		{"#FFFFG0", 255, 255, 0, 1}, {"#FFFFg0", 255, 255, 0, 1}, {"#-FFF00", 15, 255, 0, 1},
		{"#000000", 0, 0, 0, 1}, {"#C0C0C0", 192, 192, 192, 1}, {"#0a0A0a", 10, 10, 10, 1}, {"#a0A0a0", 160, 160, 160, 1},
		{"#CFA", 204, 255, 170, 1}, {"#CFA8", 204, 255, 170, 0.533}, {"#11223344", 17, 34, 51, 0.267},
	}
	for _, tc := range cases {
		r, g, b, a := hex(t, tc.in).RGBA()
		if r != tc.r || g != tc.g || b != tc.b || a != tc.a {
			t.Errorf("%s = %d,%d,%d,%v, want %d,%d,%d,%v", tc.in, r, g, b, a, tc.r, tc.g, tc.b, tc.a)
		}
	}
}

func TestColorOperations(t *testing.T) {
	white, black := ColorWhite, ColorBlack
	if got := black.Mix(white, 0.5); !got.Equal(NewColor(127, 127, 127, 1)) {
		t.Errorf("mix: %v", got)
	}
	if got := black.Mix(white, 7); !got.Equal(white) {
		t.Errorf("mix is cut to 0..1: %v", got)
	}
	if got := NewColor(10, 20, 30, 0.5).MakeOpaque(white); !got.Equal(NewColor(132, 137, 142, 1)) {
		t.Errorf("make opaque: %v", got)
	}
	if got := NewColor(10, 20, 30, 0.5).MakeOpaque(NewColor(0, 0, 0, 0.5)); !got.Equal(NewColor(10, 20, 30, 0.5)) {
		t.Errorf("a background that is not opaque changes nothing: %v", got)
	}
	if got := white.WithAlphaFactor(0.4); got.String() != "rgba(255, 255, 255, 0.4)" {
		t.Errorf("alpha factor: %v", got)
	}
	if white.IsOpaque() != true || ColorTransparent.IsOpaque() {
		t.Error("opaque")
	}
	if got := NewColor(0x26, 0x4F, 0x78, 0.5).ARGB(); got != 0x80264F78 {
		t.Errorf("ARGB = %#x", got)
	}
	if got := hex(t, "#00000000").Hex(); got != "#00000000" {
		t.Errorf("Hex of a transparent color: %q", got)
	}
	d := hex(t, "#808080")
	if got := d.Lighten(0.5).Darken(0.5).toHSLA(); got.l <= 0 {
		t.Errorf("lighten then darken keeps a lightness: %+v", got)
	}
	if got := NewColor(300, -5, 7.9, 2); !got.Equal(NewColor(255, 0, 7, 1)) {
		t.Errorf("components are cut: %v", got)
	}
}
