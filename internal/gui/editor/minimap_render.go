package editor

import (
	"math"

	"github.com/rm4n0s/glutter/ui"
)

// Ported from VS Code: src/vs/editor/browser/viewParts/minimap/minimap.ts (InnerMinimap.renderLines,
// _renderUntouchedLines, _renderLine) and minimapCharRenderer.ts (blockRenderChar)

// minimapRGBA is a color with 8 bits per channel.
type minimapRGBA struct{ R, G, B, A uint8 }

func rgbaOf(c ui.Color) minimapRGBA {
	return minimapRGBA{uint8(c.Red()), uint8(c.Green()), uint8(c.Blue()), uint8(c.Alpha())}
}

// minimapRowKey says what a row of the minimap looks like: when two frames have the same key for a
// line the pixels of its row are copied from the older one.
type minimapRowKey struct {
	line, start, end int
	text             string
	spans            string
}

// minimapRow is the row of a line the painter has drawn: where it is in the canvas and what it
// looks like.
type minimapRow struct {
	key minimapRowKey
	dy  int
}

// minimapRowData is what the painter draws of a line of the minimap: the text of the row it
// shows and the colors of its parts.
type minimapRowData struct {
	key   minimapRowKey
	text  string
	spans []span
}

// minimapStyle is how the painter draws the lines.
type minimapStyle struct {
	// Width and Height are the size of the canvas in pixels.
	Width, Height int
	// LineHeight is the height of a line and Scale the size of a block, in pixels.
	LineHeight, Scale int
	// TabSize is the number of columns of a tab.
	TabSize int
	// Background is the color of the minimap (its alpha is its opacity), DefaultBackground the
	// editor's, Foreground the color of text that has no token color, and ForegroundAlpha the
	// opacity of the blocks, from 0 to 255 (minimap.foregroundOpacity).
	Background, DefaultBackground, Foreground minimapRGBA
	ForegroundAlpha                           uint8
}

// minimapPainter draws the lines of the minimap into pixels, and remembers which row of the canvas
// shows what, so that a frame only draws the lines that are not already on the canvas.
type minimapPainter struct {
	style minimapStyle
	// pix are the premultiplied RGBA pixels of the last frame; they are never changed once they
	// are in an image, a frame that changes something makes new ones.
	pix  []byte
	rows []minimapRow
	// startLine is the 1-based line of the first of rows.
	startLine, topPadding int
	// drawn and copied count, for the last frame that changed the pixels, the lines drawn from
	// scratch and the ones copied from the frame before.
	drawn, copied int
}

// reset forgets the last frame: the style, and so every pixel, changed.
func (p *minimapPainter) reset(style minimapStyle) {
	*p = minimapPainter{style: style}
}

// frame draws the lines of a layout and reports whether the pixels changed. row gives the data of
// the 1-based line of the minimap.
func (p *minimapPainter) frame(l minimapRenderLayout, row func(line int) minimapRowData) (changed bool) {
	st := p.style
	if st.Width <= 0 || st.Height <= 0 || st.LineHeight <= 0 {
		p.pix, p.rows = nil, nil
		return false
	}
	count := max(l.EndLine-l.StartLine+1, 0)
	data := make([]minimapRowData, count)
	same := p.pix != nil && p.startLine == l.StartLine && p.topPadding == l.TopPaddingLineCount && len(p.rows) == count
	for i := range count {
		data[i] = row(l.StartLine + i)
		if same && p.rows[i].key != data[i].key {
			same = false
		}
	}
	if same {
		return false
	}
	p.drawn, p.copied = 0, 0
	pix := make([]byte, st.Width*st.Height*4)
	fillBackground(pix, st.Background)
	background := compose(st.Background, st.DefaultBackground)

	// The old frame's rows by key, for the lines that moved.
	old := map[minimapRowKey]int{}
	for _, r := range p.rows {
		old[r.key] = r.dy
	}
	rows := make([]minimapRow, count)
	dy := l.TopPaddingLineCount * st.LineHeight
	for i, d := range data {
		rows[i] = minimapRow{key: d.key, dy: dy}
		if src, ok := old[d.key]; ok && p.pix != nil && src+st.LineHeight <= st.Height && dy+st.LineHeight <= st.Height {
			copy(pix[dy*st.Width*4:(dy+st.LineHeight)*st.Width*4], p.pix[src*st.Width*4:(src+st.LineHeight)*st.Width*4])
			p.copied++
		} else {
			p.renderLine(pix, background, dy, d)
			p.drawn++
		}
		dy += st.LineHeight
	}
	p.pix, p.rows, p.startLine, p.topPadding = pix, rows, l.StartLine, l.TopPaddingLineCount
	return true
}

// image is the picture of the last frame, or nil when there is none.
func (p *minimapPainter) image() *ui.Image {
	if p.pix == nil {
		return nil
	}
	return ui.NewImageFromRGBA(p.pix, p.style.Width, p.style.Height)
}

// fillBackground fills the pixels with the color of the minimap (premultiplied).
func fillBackground(pix []byte, c minimapRGBA) {
	a := uint32(c.A)
	r, g, b := byte(uint32(c.R)*a/255), byte(uint32(c.G)*a/255), byte(uint32(c.B)*a/255)
	for i := 0; i < len(pix); i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = r, g, b, c.A
	}
}

// compose is the color of the minimap's background over the editor's, which blocks are blended
// with: opaque.
func compose(background, over minimapRGBA) minimapRGBA {
	a := float64(background.A) / 255
	mix := func(b, o uint8) uint8 { return uint8(math.Round((float64(b)-float64(o))*a + float64(o))) }
	return minimapRGBA{mix(background.R, over.R), mix(background.G, over.G), mix(background.B, over.B), 255}
}

// renderLine draws the blocks of a line at the row of the canvas that starts at dy: a block for
// each character that is not white space, in the color of its token, a tab or a space leaves a gap
// and a full-width character takes two blocks.
//
// Ported from VS Code: minimap.ts (InnerMinimap._renderLine)
func (p *minimapPainter) renderLine(pix []byte, background minimapRGBA, dy int, d minimapRowData) {
	st := p.style
	charWidth := minimapBaseCharWidth * st.Scale
	maxDx := st.Width - charWidth
	dx := minimapGutterWidth
	col := 0
	si := 0
	for b, r := range d.text {
		if dx > maxDx {
			return
		}
		for si < len(d.spans) && d.spans[si].end <= b {
			si++
		}
		switch r {
		case '\t':
			n := st.TabSize - col%max(st.TabSize, 1)
			col += n
			dx += n * charWidth
		case ' ':
			col++
			dx += charWidth
		default:
			color := st.Foreground
			if si < len(d.spans) && d.spans[si].start <= b {
				color = rgbaOf(d.spans[si].color)
			}
			blocks := 1
			if isFullWidthRune(r) {
				blocks = 2
			}
			for range blocks {
				p.block(pix, dx, dy, color, background)
				dx += charWidth
				if dx > maxDx {
					return
				}
			}
			col += blocks
		}
	}
}

// block draws the block of a character: Scale columns wide and 2 * Scale rows high, in the color
// of the token moved towards the background by half and by the opacity of the foreground.
//
// Ported from VS Code: minimapCharRenderer.ts (blockRenderChar)
func (p *minimapPainter) block(pix []byte, dx, dy int, color, background minimapRGBA) {
	st := p.style
	w, h := minimapBaseCharWidth*st.Scale, minimapBaseCharHeight*st.Scale
	if st.LineHeight == 1 {
		h = 1
	}
	if dx+w > st.Width || dy+h > st.Height {
		return
	}
	c := 0.5 * float64(st.ForegroundAlpha) / 255
	mix := func(fg, bg uint8) float64 { return float64(bg) + (float64(fg)-float64(bg))*c }
	alpha := max(st.ForegroundAlpha, st.Background.A)
	// the canvas of VS Code rounds the channels to the nearest, ties to even
	channel := func(fg, bg uint8) byte { return byte(math.RoundToEven(mix(fg, bg) * float64(alpha) / 255)) }
	pr, pg, pb := channel(color.R, background.R), channel(color.G, background.G), channel(color.B, background.B)
	for y := range h {
		i := ((dy+y)*st.Width + dx) * 4
		for range w {
			pix[i], pix[i+1], pix[i+2], pix[i+3] = pr, pg, pb, alpha
			i += 4
		}
	}
}

// isFullWidthRune tells whether a character takes two cells: the East Asian wide and full-width
// characters.
func isFullWidthRune(r rune) bool {
	if r < 0x1100 {
		return false
	}
	return r <= 0x115F || (r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) || (r >= 0xAC00 && r <= 0xD7A3) ||
		(r >= 0xF900 && r <= 0xFAFF) || (r >= 0xFE30 && r <= 0xFE6F) || (r >= 0xFF00 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) || (r >= 0x20000 && r <= 0x3FFFD)
}
