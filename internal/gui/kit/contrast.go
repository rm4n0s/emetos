package kit

import (
	"math"

	"github.com/rm4n0s/glutter/ui"
)

// EnsureContrast moves the foreground color until it has the contrast ratio on the background, or as much of it as black or white
// gives: the foreground gets darker or lighter, in steps of 10 %, the way that leaves it away from the background, and when that way
// ends short of the ratio the other way is tried and the better of the two wins. The ANSI colors of the Debug Console go through it
// with the ratio 4, so that the colors of the theme stay readable on the panel and on a selected row.
//
// Ported from VS Code: src/vs/base/common/color.ts (Color.ensureConstrast, reduceRelativeLuminace, increaseRelativeLuminace)
func EnsureContrast(background, foreground ui.Color, ratio float64) ui.Color {
	br, bg, bb := channels(background)
	fr, fg, fb := channels(foreground)
	bgL := relativeLuminance(br, bg, bb)
	fgL := relativeLuminance(fr, fg, fb)
	ratioOf := func(r, g, b int) float64 { return contrastRatio(bgL, relativeLuminance(r, g, b)) }
	if ratioOf(fr, fg, fb) >= ratio {
		return foreground
	}
	type rgb struct{ r, g, b int }
	reduce := func() rgb {
		r, g, b := fr, fg, fb
		for cr := ratioOf(r, g, b); cr < ratio && (r > 0 || g > 0 || b > 0); cr = ratioOf(r, g, b) {
			r -= max(0, int(math.Ceil(float64(r)*0.1)))
			g -= max(0, int(math.Ceil(float64(g)*0.1)))
			b -= max(0, int(math.Ceil(float64(b)*0.1)))
		}
		return rgb{r, g, b}
	}
	increase := func() rgb {
		r, g, b := fr, fg, fb
		for cr := ratioOf(r, g, b); cr < ratio && (r < 255 || g < 255 || b < 255); cr = ratioOf(r, g, b) {
			r = min(255, r+int(math.Ceil(float64(255-r)*0.1)))
			g = min(255, g+int(math.Ceil(float64(255-g)*0.1)))
			b = min(255, b+int(math.Ceil(float64(255-b)*0.1)))
		}
		return rgb{r, g, b}
	}
	first, second := increase, reduce
	if fgL < bgL {
		first, second = reduce, increase
	}
	a := first()
	if ratioOf(a.r, a.g, a.b) >= ratio {
		return ui.NewColorFromARGB(foreground.Alpha(), a.r, a.g, a.b)
	}
	b := second()
	if ratioOf(a.r, a.g, a.b) > ratioOf(b.r, b.g, b.b) {
		b = a
	}
	return ui.NewColorFromARGB(foreground.Alpha(), b.r, b.g, b.b)
}

func channels(c ui.Color) (r, g, b int) { return c.Red(), c.Green(), c.Blue() }

// relativeLuminance is the WCAG relative luminance of a color, rounded to 4 decimals as VS Code does.
func relativeLuminance(r, g, b int) float64 {
	comp := func(v int) float64 {
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	l := 0.2126*comp(r) + 0.7152*comp(g) + 0.0722*comp(b)
	return math.Round(l*10000) / 10000
}

func contrastRatio(l1, l2 float64) float64 {
	if l1 > l2 {
		return (l1 + 0.05) / (l2 + 0.05)
	}
	return (l2 + 0.05) / (l1 + 0.05)
}
