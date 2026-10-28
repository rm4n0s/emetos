package terminal

import (
	"math"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// theme is the colors the terminal paints with, from the theme of the window (terminal.* colors, SPEC §10.25 "The UI"), as 0xAARRGGBB.
type theme struct {
	fg, bg                    uint32 // terminal.foreground and the background (terminal.background, else the panel's)
	cursor, cursorText        uint32 // terminalCursor.foreground and terminalCursor.background
	selection, selectionInact uint32
	selectionFg               uint32 // 0 keeps the foreground of the text
	border, tabActiveBorder   uint32
	findMatch, findHighlight  uint32
	ansi                      [16]uint32
}

// themeOf reads the colors of the window's theme.
func themeOf(c ui.Colors) theme {
	argb := func(col gui.Color) uint32 { return col.ToARGB32() }
	t := theme{
		fg: argb(c.TerminalForeground), bg: argb(c.TerminalBackground),
		cursor: argb(c.TerminalCursorForeground), cursorText: argb(c.TerminalCursorBackground),
		selection: argb(c.TerminalSelectionBackground), selectionInact: argb(c.TerminalInactiveSelectionBackground),
		selectionFg: argb(c.TerminalSelectionForeground),
		border:      argb(c.TerminalBorder), tabActiveBorder: argb(c.TerminalTabActiveBorder),
		findMatch: argb(c.TerminalFindMatchBackground), findHighlight: argb(c.TerminalFindMatchHighlightBackground),
	}
	for i := range t.ansi {
		t.ansi[i] = argb(c.TerminalAnsi[i])
	}
	if t.bg>>24 == 0 {
		t.bg = argb(c.PanelBackground)
	}
	if t.cursor>>24 == 0 {
		t.cursor = t.fg
	}
	if t.cursorText>>24 == 0 {
		t.cursorText = t.bg
	}
	t.fg |= 0xFF000000
	t.bg |= 0xFF000000
	return t
}

// rgb is the 0xRRGGBB of an opaque 0xAARRGGBB.
func rgb(argb uint32) uint32 { return argb & 0xFFFFFF }

// resolved is the look of one style of the table of the emulator, decided: the colors with inverse, dim, bold in bright colors
// and the minimum contrast applied, and the parts that are drawn besides the glyph.
type resolved struct {
	ready     bool
	fg        uint32 // 0xAARRGGBB
	bg        uint32 // 0 when the cell has the background of the terminal
	attrs     uint8  // attrBold, attrItalic
	underline emulator.Underline
	underCol  uint32
	strike    bool
	overline  bool
	invisible bool
	link      bool
}

// colorOf turns a color of a style into 0xAARRGGBB: the palette entry of an indexed one, the 24 bits of an RGB one, and def for
// the default color.
func colorOf(c emulator.Color, pal *[256]uint32, def uint32) uint32 {
	switch c.Kind() {
	case emulator.ColorIndexed:
		return 0xFF000000 | pal[c.Index()]
	case emulator.ColorRGB:
		r, g, b := c.RGB()
		return 0xFF000000 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)
	}
	return def
}

// resolveStyle decides the look of a style.
func resolveStyle(s emulator.Style, f *emulator.Frame, th *theme, boldBright bool, minContrast float64) resolved {
	defFg := 0xFF000000 | f.Fg
	defBg := 0xFF000000 | f.Bg
	if f.Fg == 0 && f.Bg == 0 {
		defFg, defBg = th.fg, th.bg
	}
	fgc := s.Fg
	if boldBright && s.Attrs&emulator.AttrBold != 0 && fgc.Kind() == emulator.ColorIndexed && fgc.Index() < 8 {
		fgc = emulator.IndexedColor(fgc.Index() + 8)
	}
	fg := colorOf(fgc, &f.Palette, defFg)
	bg := colorOf(s.Bg, &f.Palette, 0)
	if s.Attrs&emulator.AttrInverse != 0 {
		ebg := bg
		if ebg == 0 {
			ebg = defBg
		}
		fg, bg = ebg, fg
	}
	r := resolved{ready: true, underline: s.Underline, strike: s.Attrs&emulator.AttrStrike != 0, overline: s.Attrs&emulator.AttrOverline != 0,
		invisible: s.Attrs&emulator.AttrInvisible != 0, link: s.Link != 0}
	if s.Attrs&emulator.AttrBold != 0 {
		r.attrs |= attrBold
	}
	if s.Attrs&emulator.AttrItalic != 0 {
		r.attrs |= attrItalic
	}
	ebg := bg
	if ebg == 0 {
		ebg = defBg
	}
	if s.Attrs&emulator.AttrDim != 0 {
		fg = mix(fg, ebg, 0.5)
	}
	if minContrast > 1 && fg != ebg {
		fg = ensureContrast(fg, ebg, minContrast)
	}
	r.fg, r.bg = fg, bg
	r.underCol = fg
	if s.UnderlineColor.Kind() != emulator.ColorDefault {
		r.underCol = colorOf(s.UnderlineColor, &f.Palette, fg)
	}
	return r
}

// mix is the color a part of the way from a to b.
func mix(a, b uint32, t float64) uint32 {
	ch := func(sh uint) uint32 {
		x, y := float64(a>>sh&0xFF), float64(b>>sh&0xFF)
		return uint32(math.Round(x+(y-x)*t)) & 0xFF
	}
	return 0xFF000000 | ch(16)<<16 | ch(8)<<8 | ch(0)
}

// luminance is the relative luminance of a color (WCAG 2).
func luminance(c uint32) float64 {
	f := func(sh uint) float64 {
		v := float64(c>>sh&0xFF) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*f(16) + 0.7152*f(8) + 0.0722*f(0)
}

// contrast is the contrast ratio of two colors (WCAG 2).
func contrast(a, b uint32) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// ensureContrast moves the foreground toward white or black, the way away from the background, until the contrast ratio is met;
// when it cannot be met that way the other way is tried, and the better of the two wins.
func ensureContrast(fg, bg uint32, ratio float64) uint32 {
	if contrast(fg, bg) >= ratio {
		return fg
	}
	toward := func(target uint32) (uint32, float64) {
		best, bestRatio := fg, contrast(fg, bg)
		for i := 1; i <= 20; i++ {
			c := mix(fg, target, float64(i)/20)
			if r := contrast(c, bg); r > bestRatio {
				best, bestRatio = c, r
			}
			if bestRatio >= ratio {
				break
			}
		}
		return best, bestRatio
	}
	first, second := uint32(0xFFFFFFFF), uint32(0xFF000000)
	if luminance(bg) > 0.5 {
		first, second = second, first
	}
	c1, r1 := toward(first)
	if r1 >= ratio {
		return c1
	}
	c2, r2 := toward(second)
	if r2 > r1 {
		return c2
	}
	return c1
}
