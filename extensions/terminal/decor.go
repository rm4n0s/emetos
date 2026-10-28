package terminal

import (
	"math"

	gui "github.com/rm4n0s/glutter/ui"
)

// The decoration atlas: one small image of white shapes that the grid draws, tinted, with a single DrawAtlas for everything
// that is not a glyph: the backgrounds of runs of cells, the selection, the underlines, the strike-through, the cursor
// (SPEC §10.25 "Speed"). A run of n cells is drawn with sprites of 1, 2, 4 ... cells.

// maxRunLog is the largest sprite of a run: 1<<maxRunLog cells.
const maxRunLog = 6

// decor is the atlas of decorations of one cell size.
type decor struct {
	m   metrics
	img *gui.Image
	// block[k] and line[k] are the sprites of a solid block of the height of a cell and a thin line of a light line's
	// thickness, 1<<k cells wide; thick[k] is a line twice as thick, for k 0 and 1 (the underline cursor)
	block, line [maxRunLog + 1]gui.Rect
	thick       [2]gui.Rect
	// bar is a vertical line of the thickness of a light line, as high as a cell
	bar gui.Rect
	// the patterns of the underline kinds, one cell wide
	curly, dotted, dashed gui.Rect
}

// newDecor draws the atlas for the metrics.
func newDecor(m metrics) *decor {
	d := &decor{m: m}
	lt := m.lineThick
	// layout: row 0 the blocks, row 1 the thin lines, row 2 the thick lines, the bar and the patterns
	rowHeights := []int{m.ch, lt, 2 * lt, max(3*lt, 3)}
	width := m.cw * ((1 << (maxRunLog + 1)) - 1)
	if min := m.cw*2 + lt + 3*m.cw; width < min {
		width = min
	}
	height := 0
	for _, h := range rowHeights {
		height += h
	}
	pix := make([]byte, width*height*4)
	fill := func(x0, y0, x1, y1 int, v uint8) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				o := (y*width + x) * 4
				pix[o], pix[o+1], pix[o+2], pix[o+3] = v, v, v, v
			}
		}
	}
	rect := func(x, y, w, h int) gui.Rect {
		return gui.NewRectFromLTWH(float64(x), float64(y), float64(w), float64(h))
	}
	y := 0
	x := 0
	for k := 0; k <= maxRunLog; k++ {
		w := m.cw << k
		fill(x, y, x+w, y+m.ch, 255)
		d.block[k] = rect(x, y, w, m.ch)
		x += w
	}
	y += m.ch
	x = 0
	for k := 0; k <= maxRunLog; k++ {
		w := m.cw << k
		fill(x, y, x+w, y+lt, 255)
		d.line[k] = rect(x, y, w, lt)
		x += w
	}
	y += lt
	x = 0
	for k := 0; k < 2; k++ {
		w := m.cw << k
		fill(x, y, x+w, y+2*lt, 255)
		d.thick[k] = rect(x, y, w, 2*lt)
		x += w
	}
	y += 2 * lt
	// the vertical bar needs a cell's height: it is cut from the first block, whose pixels are all white
	d.bar = rect(0, 0, lt, m.ch)
	// the patterns of the underlines, one cell wide
	x = 0
	ph := max(3*lt, 3)
	for i := 0; i < m.cw; i++ {
		// curly: a wave one cell long, lt thick, between the top and the bottom of three light lines
		top := int(math.Round(float64(lt) * (1 + math.Sin(2*math.Pi*float64(i)/float64(m.cw)))))
		fill(x+i, y+top, x+i+1, y+top+lt, 255)
	}
	d.curly = rect(x, y, m.cw, ph)
	x += m.cw
	for i := 0; i < m.cw; i++ {
		if (i/max(lt, 1))%2 == 0 {
			fill(x+i, y, x+i+1, y+lt, 255)
		}
	}
	d.dotted = rect(x, y, m.cw, lt)
	x += m.cw
	for i := 0; i < m.cw; i++ {
		if i*3/max(m.cw, 1) != 1 {
			fill(x+i, y, x+i+1, y+lt, 255)
		}
	}
	d.dashed = rect(x, y, m.cw, lt)
	d.img = gui.NewImageFromRGBA(pix, width, height)
	return d
}

// runSprites calls emit with the sprite of each part of a run of n cells, from its first cell: the parts are 1<<k cells wide, the
// largest that fits first.
func runSprites(n int, sprites *[maxRunLog + 1]gui.Rect, emit func(sprite gui.Rect, cells int)) {
	for n > 0 {
		k := maxRunLog
		for 1<<k > n {
			k--
		}
		emit(sprites[k], 1<<k)
		n -= 1 << k
	}
}
