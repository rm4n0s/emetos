package terminal

import (
	"log/slog"
	"math"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/offscreen"
	gui "github.com/rm4n0s/glutter/ui"
)

// The glyph atlas (SPEC §10.25 "Speed"): each glyph (a code point or a cluster, in a weight and a style) is rasterized once
// into an image, a page of the atlas, and the grid is drawn with one Canvas.DrawAtlas per page, the sprites tinted by the colors
// of the cells. No paragraph is laid out per row and no picture made per glyph, so the cost of a frame does not grow with the
// styles of the text.

// fontSpec is everything that decides how a glyph looks and how big a cell is; two terminals with the same one share an atlas.
type fontSpec struct {
	families      string // the families joined by a comma
	size          float64
	weight        int
	weightBold    int
	lineHeight    float64
	letterSpacing float64
	dpr           float64
}

// metrics are the size of a cell, in device pixels and in logical pixels.
type metrics struct {
	cw, ch int
	// w and h are the size of the cell in logical pixels: cw/dpr and ch/dpr.
	w, h float64
	dpr  float64
	// lineThick is the thickness of a light line in device pixels.
	lineThick int
}

// glyphAttrs are the bits of the look of a glyph besides the character.
const (
	attrBold uint8 = 1 << iota
	attrItalic
	attrWide
)

// slot is where a glyph is in the atlas. page is the index plus one (0 is a glyph not made yet).
type slot struct {
	page    uint16
	x, y    uint16
	blank   bool // the glyph draws nothing
	colored bool // the glyph has its own colors (an emoji): it is drawn untinted
}

type clusterKey struct {
	s     string
	attrs uint8
}

type runeKey struct {
	r     rune
	attrs uint8
}

// pending is a glyph that has a slot and is not rasterized yet.
type pending struct {
	text  string
	attrs uint8
	slot  *slot
}

// page is one image of the atlas.
type page struct {
	w, h int
	pix  []byte // premultiplied RGBA
	img  *gui.Image
	// the shelf allocator: the next free place in the current row and the height of the row
	x, y, rowH int
	dirty      bool
}

const pageSize = 512

// atlas holds the glyphs of one font at one size.
type atlas struct {
	fs      fontSpec
	m       metrics
	pd      *gui.PlatformDispatcher
	pages   []*page
	ascii   [4][128]slot
	runes   map[runeKey]*slot
	cluster map[clusterKey]*slot
	waiting []pending
	// the slots are pointers into slabs so that a slot made before a flush is the one the flush fills
	slabs [][]slot
	// baselines are the baseline of a cell for each look of the font (bold and italic), in device pixels below the top of the
	// cell; haveBase says which were measured.
	baselines [4]int
	haveBase  [4]bool
}

// newAtlas makes an empty atlas for the font. The cell size comes from measuring the font with the text engine.
func newAtlas(pd *gui.PlatformDispatcher, fs fontSpec) *atlas {
	a := &atlas{fs: fs, pd: pd, runes: map[runeKey]*slot{}, cluster: map[clusterKey]*slot{}}
	a.m = measureCell(pd, fs)
	return a
}

// styleOf is the font weight and style of a look.
func (fs fontSpec) styleOf(attrs uint8) (gui.FontWeight, gui.FontStyle) {
	w := fs.weight
	if attrs&attrBold != 0 {
		w = fs.weightBold
	}
	var weight gui.FontWeight
	switch {
	case w <= 150:
		weight = gui.FontWeightW100
	case w <= 250:
		weight = gui.FontWeightW200
	case w <= 350:
		weight = gui.FontWeightW300
	case w <= 450:
		weight = gui.FontWeightW400
	case w <= 550:
		weight = gui.FontWeightW500
	case w <= 650:
		weight = gui.FontWeightW600
	case w <= 750:
		weight = gui.FontWeightW700
	case w <= 850:
		weight = gui.FontWeightW800
	default:
		weight = gui.FontWeightW900
	}
	style := gui.FontStyleNormal
	if attrs&attrItalic != 0 {
		style = gui.FontStyleItalic
	}
	return weight, style
}

// textStyle is the text style of a look, white. heightMul is the line height as a multiple of the font size (0 for the font's own).
func (fs fontSpec) textStyle(attrs uint8, heightMul float64) *gui.TextStyle {
	family, fallback := "", []string(nil)
	if fams := splitFamilies(fs.families); len(fams) > 0 {
		family, fallback = fams[0], fams[1:]
	}
	size := fs.size
	weight, style := fs.styleOf(attrs)
	white := gui.NewColor(0xFFFFFFFF)
	opts := gui.TextStyleOpts{
		Color: &white, FontFamily: family, FontFamilyFallback: fallback, FontSize: &size, FontWeight: weight, FontStyle: style,
		LeadingDistribution: gui.TextLeadingDistributionEven,
	}
	if heightMul > 0 {
		opts.Height = &heightMul
	}
	if fs.letterSpacing != 0 {
		ls := fs.letterSpacing
		opts.LetterSpacing = &ls
	}
	return gui.NewTextStyle(opts)
}

// splitFamilies splits the families of a fontSpec.
func splitFamilies(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// measureCell measures a cell: the width of a character of the font and the height of its line, in whole device pixels.
func measureCell(pd *gui.PlatformDispatcher, fs fontSpec) metrics {
	dpr := fs.dpr
	if dpr <= 0 {
		dpr = 1
	}
	adv, nat := fs.size*0.6, fs.size*1.2
	fm, err := pd.FontMetrics(fs.textStyle(0, 0))
	if err != nil {
		slog.Warn("the text engine cannot measure the font of the terminal; the cell size is a guess", "err", err)
	} else if fm.ZeroAdvance > 0 && fm.Height > 0 {
		adv, nat = fm.ZeroAdvance, fm.Height
	}
	cw := max(int(math.Round(adv*dpr)), 1)
	ch := max(int(math.Ceil(nat*dpr*fs.lineHeight-1e-6)), 1)
	return metrics{cw: cw, ch: ch, w: float64(cw) / dpr, h: float64(ch) / dpr, dpr: dpr, lineThick: max(int(math.Round(dpr)), 1)}
}

// baseline is how far below the top of a cell the baseline of a look is, in device pixels: the ascent of the line of the font,
// with the line height of the cell, the way a paragraph of that line places it.
func (a *atlas) baseline(attrs uint8) int {
	k := attrs & (attrBold | attrItalic)
	if !a.haveBase[k] {
		a.haveBase[k] = true
		fm, err := a.pd.FontMetrics(a.fs.textStyle(k, a.m.h/a.fs.size))
		if err != nil {
			slog.Warn("the text engine cannot measure the font of the terminal; the baseline is a guess", "err", err)
			a.baselines[k] = int(math.Round(a.m.h * a.m.dpr * 0.8))
		} else {
			a.baselines[k] = int(math.Round(fm.Ascent * a.m.dpr))
		}
	}
	return a.baselines[k]
}

// blockWidth is the width of the block of a glyph in pixels: one cell, or two for a wide character.
func (a *atlas) blockWidth(attrs uint8) int {
	if attrs&attrWide != 0 {
		return 2 * a.m.cw
	}
	return a.m.cw
}

// newSlot returns a fresh slot that stays where it is.
func (a *atlas) newSlot() *slot {
	if n := len(a.slabs); n == 0 || len(a.slabs[n-1]) == cap(a.slabs[n-1]) {
		a.slabs = append(a.slabs, make([]slot, 0, 256))
	}
	s := &a.slabs[len(a.slabs)-1]
	*s = append(*s, slot{})
	return &(*s)[len(*s)-1]
}

// alloc finds room for a block of w by the height of a cell on the last page, making a page when there is none.
func (a *atlas) alloc(w int) (pageIdx, x, y int) {
	h := a.m.ch
	size := pageSize
	for size < w*4 || size < h*4 {
		size *= 2
	}
	if n := len(a.pages); n > 0 {
		p := a.pages[n-1]
		if p.x+w > p.w {
			p.x, p.y, p.rowH = 0, p.y+p.rowH, 0
		}
		if p.y+h <= p.h && p.x+w <= p.w {
			x, y = p.x, p.y
			p.x += w
			p.rowH = max(p.rowH, h)
			return n - 1, x, y
		}
	}
	p := &page{w: size, h: size, pix: make([]byte, size*size*4)}
	a.pages = append(a.pages, p)
	p.x, p.rowH = w, h
	return len(a.pages) - 1, 0, 0
}

// lookup returns the slot of a code point in a look. A glyph not made yet gets a place and waits for flush.
func (a *atlas) lookup(r rune, attrs uint8) *slot {
	if r >= 0 && r < 128 && attrs < 4 {
		s := &a.ascii[attrs][r]
		if s.page == 0 && !s.blank {
			a.prepare(s, string(r), attrs)
		}
		return s
	}
	k := runeKey{r, attrs}
	s := a.runes[k]
	if s == nil {
		s = a.newSlot()
		a.runes[k] = s
		a.prepare(s, string(r), attrs)
	}
	return s
}

// lookupCluster is lookup for a base character with combining marks.
func (a *atlas) lookupCluster(text string, attrs uint8) *slot {
	k := clusterKey{text, attrs}
	s := a.cluster[k]
	if s == nil {
		s = a.newSlot()
		a.cluster[k] = s
		a.prepare(s, text, attrs)
	}
	return s
}

// prepare gives a slot a place, or marks it blank, and queues its rasterization.
func (a *atlas) prepare(s *slot, text string, attrs uint8) {
	r, _ := utf8.DecodeRuneInString(text)
	if r == ' ' || r == 0 || r == 0xA0 || r == utf8.RuneError && len(text) == 1 {
		s.blank = true
		return
	}
	pi, x, y := a.alloc(a.blockWidth(attrs))
	s.page, s.x, s.y = uint16(pi+1), uint16(x), uint16(y)
	a.waiting = append(a.waiting, pending{text: text, attrs: attrs, slot: s})
}

// flush rasterizes the glyphs that wait into the pages and makes new images of the pages that changed. It reports whether
// any page changed.
func (a *atlas) flush() bool {
	if len(a.waiting) == 0 {
		return false
	}
	lt := a.m.lineThick
	// the glyphs that come from the font are rasterized together on a sheet by the software renderer
	var fromFont []pending
	for _, w := range a.waiting {
		r, _ := utf8.DecodeRuneInString(w.text)
		if len(w.text) == utf8.RuneLen(r) && isShape(r) {
			bw := a.blockWidth(w.attrs)
			m := shapeMask(r, bw, a.m.ch, lt)
			a.store(w.slot, bw, func(x, y int) (r, g, b, al uint8) { v := m.a[y*bw+x]; return v, v, v, v })
			continue
		}
		fromFont = append(fromFont, w)
	}
	if len(fromFont) > 0 {
		a.rasterize(fromFont)
	}
	a.waiting = a.waiting[:0]
	changed := false
	for _, p := range a.pages {
		if p.dirty {
			p.img = gui.NewImageFromRGBA(append([]byte(nil), p.pix...), p.w, p.h)
			p.dirty = false
			changed = true
		}
	}
	return changed
}

// store copies a block into the page of the slot and marks the slot blank when the block has no ink.
func (a *atlas) store(s *slot, bw int, px func(x, y int) (r, g, b, al uint8)) {
	p := a.pages[s.page-1]
	ink := false
	for y := 0; y < a.m.ch; y++ {
		row := p.pix[((int(s.y)+y)*p.w+int(s.x))*4:]
		for x := 0; x < bw; x++ {
			r, g, b, al := px(x, y)
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = r, g, b, al
			if al != 0 {
				ink = true
			}
		}
	}
	p.dirty = true
	s.blank = !ink
}

// rasterize asks Glutter for a sheet of the glyphs of each look (offscreen.NewGlyphSheet: the same fonts, fallback and outlines as
// a paragraph, drawn by the software renderer) and copies each glyph into its block of the atlas, placed on the baseline of the
// cell and clipped to the block. A glyph with colors of its own (an emoji) is marked colored.
func (a *atlas) rasterize(list []pending) {
	const looks = attrBold | attrItalic
	for k := uint8(0); k <= looks; k++ {
		var group []pending
		for _, w := range list {
			if w.attrs&looks == k {
				group = append(group, w)
			}
		}
		if len(group) > 0 {
			a.rasterizeLook(group, k)
		}
	}
}

// rasterizeLook is rasterize for glyphs that have the same bold and italic.
func (a *atlas) rasterizeLook(list []pending, look uint8) {
	clusters := make([]string, len(list))
	for i, w := range list {
		clusters[i] = w.text
	}
	sheet, err := offscreen.NewGlyphSheet(a.pd.TextEngine(), a.fs.textStyle(look, a.m.h/a.fs.size), a.m.dpr, clusters)
	if err != nil {
		// the sheet could not be drawn: the glyphs stay blank rather than failing the frame
		slog.Error("the glyph sheet of the terminal could not be drawn", "err", err)
		for _, w := range list {
			w.slot.blank = true
		}
		return
	}
	defer sheet.Image.Dispose()
	pix := sheet.Image.Pixels()
	sw := sheet.Image.Width()
	base := a.baseline(look)
	for i, w := range list {
		g := sheet.Glyphs[i]
		wb := a.blockWidth(w.attrs)
		// the glyph's pixels are at (ox, oy) of the block and come from the rect of the sheet
		ox, oy := int(math.Round(g.Offset.Dx)), base+int(math.Round(g.Offset.Dy))
		rx, ry := int(g.Rect.Left), int(g.Rect.Top)
		rw, rh := int(g.Rect.Width()), int(g.Rect.Height())
		a.store(w.slot, wb, func(x, y int) (r, gr, b, al uint8) {
			sx, sy := x-ox, y-oy
			if sx < 0 || sy < 0 || sx >= rw || sy >= rh {
				return 0, 0, 0, 0
			}
			o := ((ry+sy)*sw + rx + sx) * 4
			return pix[o], pix[o+1], pix[o+2], pix[o+3]
		})
		w.slot.colored = g.Colored && !w.slot.blank
	}
}
