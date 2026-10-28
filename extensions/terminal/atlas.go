package terminal

import (
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
	factory gui.ParagraphFactory
	pages   []*page
	ascii   [4][128]slot
	runes   map[runeKey]*slot
	cluster map[clusterKey]*slot
	waiting []pending
	// the slots are pointers into slabs so that a slot made before a flush is the one the flush fills
	slabs [][]slot
}

// newAtlas makes an empty atlas for the font. The cell size comes from measuring the font with the text engine.
func newAtlas(factory gui.ParagraphFactory, fs fontSpec) *atlas {
	a := &atlas{fs: fs, factory: factory, runes: map[runeKey]*slot{}, cluster: map[clusterKey]*slot{}}
	a.m = measureCell(factory, fs)
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

// paragraph builds the paragraph of a text in the look of attrs, white, laid out on one line. heightMul is the line height as a
// multiple of the font size (0 for the font's own).
func (fs fontSpec) paragraph(factory gui.ParagraphFactory, text string, attrs uint8, heightMul float64) *gui.Paragraph {
	family, fallback := "", []string(nil)
	if fams := splitFamilies(fs.families); len(fams) > 0 {
		family, fallback = fams[0], fams[1:]
	}
	size := fs.size
	weight, style := fs.styleOf(attrs)
	pstyle := gui.NewParagraphStyle(gui.ParagraphStyleOpts{TextDirection: gui.TextDirectionLTR, FontFamily: family, FontSize: &size})
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
	b := gui.NewParagraphBuilder(factory, pstyle)
	b.PushStyle(gui.NewTextStyle(opts))
	b.AddText(text)
	p := b.Build()
	p.Layout(gui.NewParagraphConstraints(math.Inf(1)))
	return p
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
func measureCell(factory gui.ParagraphFactory, fs fontSpec) metrics {
	dpr := fs.dpr
	if dpr <= 0 {
		dpr = 1
	}
	const n = 32
	ms := make([]byte, n)
	for i := range ms {
		ms[i] = 'M'
	}
	wide := fs.paragraph(factory, string(ms), 0, 0)
	adv := wide.LongestLine() / n
	nat := wide.Height()
	if adv <= 0 || nat <= 0 {
		adv, nat = fs.size*0.6, fs.size*1.2
	}
	cw := max(int(math.Round(adv*dpr)), 1)
	ch := max(int(math.Ceil(nat*dpr*fs.lineHeight-1e-6)), 1)
	return metrics{cw: cw, ch: ch, w: float64(cw) / dpr, h: float64(ch) / dpr, dpr: dpr, lineThick: max(int(math.Round(dpr)), 1)}
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

// rasterize draws the glyphs on a sheet with the text engine, rasterizes the sheet with the software renderer and copies each
// glyph into its place in the atlas. A glyph whose pixels are not gray (an emoji) is marked colored.
//
// Glutter: there is no call that rasterizes glyphs of a font into an image, so the sheet is a recording of paragraphs drawn
// in white and read back with the software rasterizer (docs/GLUTTER-ISSUES.md). Glutter could give a glyph rasterizer or the
// sheets of its own text atlas.
func (a *atlas) rasterize(list []pending) {
	cw, ch := a.m.cw, a.m.ch
	cols := min(len(list), 32)
	rows := (len(list) + cols - 1) / cols
	bw := 2 * cw
	sw, sh := cols*bw, rows*ch
	heightMul := a.m.h / a.fs.size
	rec := gui.NewPictureRecorder()
	cv := gui.NewCanvas(rec)
	cv.Scale(a.m.dpr, a.m.dpr)
	for i, w := range list {
		p := a.fs.paragraph(a.factory, w.text, w.attrs, heightMul)
		x := float64((i%cols)*bw) / a.m.dpr
		y := float64((i/cols)*ch) / a.m.dpr
		cv.Save()
		cv.ClipRect(gui.NewRectFromLTWH(x, y, float64(a.blockWidth(w.attrs))/a.m.dpr, a.m.h))
		cv.DrawParagraph(p, gui.Offset{Dx: x, Dy: y})
		cv.Restore()
	}
	img, err := rec.EndRecording().ToImage(sw, sh, gui.ToImageOpts{Rasterizer: offscreen.NewRasterizer()})
	if err != nil {
		// the sheet could not be drawn: the glyphs stay blank rather than failing the frame
		for _, w := range list {
			w.slot.blank = true
		}
		return
	}
	pix := img.Pixels()
	for i, w := range list {
		bx, by := (i%cols)*bw, (i/cols)*ch
		wb := a.blockWidth(w.attrs)
		colored := false
		a.store(w.slot, wb, func(x, y int) (r, g, b, al uint8) {
			o := ((by+y)*sw + bx + x) * 4
			r, g, b, al = pix[o], pix[o+1], pix[o+2], pix[o+3]
			if al != 0 {
				// an emoji has color; the text engine's glyphs are white with their coverage as alpha
				if d := int(max(r, g, b)) - int(min(r, g, b)); d > 24 {
					colored = true
				}
			}
			return
		})
		w.slot.colored = colored
	}
}
