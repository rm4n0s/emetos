package terminal

import (
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// gridPainter turns a frame of the emulator into the two DrawAtlas calls that draw it (SPEC §10.25 "Speed"): one for the
// backgrounds, the selection, the decorations and the cursor, with the atlas of decorations, and one per page for the glyphs,
// tinted by their colors. It keeps its slices between frames, so a frame allocates only what DrawAtlas copies. UI thread only.
type gridPainter struct {
	// the instances of the decoration pass
	dTr  []gui.RSTransform
	dRc  []gui.Rect
	dCol []gui.Color
	// the instances of the glyph pass, by page of the atlas
	gTr  [][]gui.RSTransform
	gRc  [][]gui.Rect
	gCol [][]gui.Color
	// styles is the look of each style of the frame, decided lazily; it is made again when the frame's tables change
	styles   []resolved
	stylesOf *emulator.Frame
	nStyles  int
	paint    *gui.Paint
}

func newGridPainter() *gridPainter {
	p := gui.NewPaint()
	p.SetFilterQuality(gui.FilterQualityNone)
	return &gridPainter{paint: p}
}

// shown is what a frame draws: the frame, where its first row is on the screen, the selection, the cursor and the colors.
type shown struct {
	f        *emulator.Frame
	rows     int
	cols     int
	originX  float64 // the left edge of the first column, in logical pixels
	originY  float64
	th       *theme
	cfg      *config
	focused  bool
	blinkOn  bool
	sel      selection
	cursorOn bool // the cursor is on the rows shown (the view is at the bottom)
	// marks are the find matches to color, links the cells of the link under the pointer to underline; both by absolute line
	marks, links []mark
}

// mark is a range of cells of a line to draw over its background (the find matches of step 17d.3 use it).
type mark struct {
	line   int64
	x0, x1 int
	color  uint32
}

func (g *gridPainter) styleOf(s shown, idx uint16) *resolved {
	if g.stylesOf != s.f || g.nStyles != len(s.f.Styles) || len(g.styles) != len(s.f.Styles) {
		if g.stylesOf != s.f || len(g.styles) > len(s.f.Styles) || s.f.Full {
			g.styles = append(g.styles[:0], make([]resolved, len(s.f.Styles))...)
		} else {
			g.styles = append(g.styles, make([]resolved, len(s.f.Styles)-len(g.styles))...)
		}
		g.stylesOf, g.nStyles = s.f, len(s.f.Styles)
	}
	if int(idx) >= len(g.styles) {
		idx = 0
	}
	r := &g.styles[idx]
	if !r.ready {
		*r = resolveStyle(s.f.Styles[idx], s.f, s.th, s.cfg.boldBright, s.cfg.minContrast)
	}
	return r
}

func (g *gridPainter) reset(pages int) {
	g.dTr, g.dRc, g.dCol = g.dTr[:0], g.dRc[:0], g.dCol[:0]
	for len(g.gTr) < pages {
		g.gTr, g.gRc, g.gCol = append(g.gTr, nil), append(g.gRc, nil), append(g.gCol, nil)
	}
	for i := range g.gTr {
		g.gTr[i], g.gRc[i], g.gCol[i] = g.gTr[i][:0], g.gRc[i][:0], g.gCol[i][:0]
	}
}

// decorAdd adds a sprite of the decoration atlas at a place, tinted.
func (g *gridPainter) decorAdd(sprite gui.Rect, scale float32, x, y float64, argb uint32) {
	g.dTr = append(g.dTr, gui.RSTransform{Scos: scale, Tx: float32(x), Ty: float32(y)})
	g.dRc = append(g.dRc, sprite)
	g.dCol = append(g.dCol, gui.NewColor(argb))
}

// run adds a run of n cells from column x0 of the row at y, with the sprites of a family (blocks or lines), offset by dy.
func (g *gridPainter) run(d *decor, sprites *[maxRunLog + 1]gui.Rect, s shown, x0, n int, y float64, argb uint32) {
	scale := float32(1 / d.m.dpr)
	x := s.originX + float64(x0)*d.m.w
	runSprites(n, sprites, func(sprite gui.Rect, cells int) {
		g.decorAdd(sprite, scale, x, y, argb)
		x += float64(cells) * d.m.w
	})
}

// build fills the instance lists for the rows of the frame. a is the glyph atlas and d the decoration atlas of the font.
func (g *gridPainter) build(a *atlas, d *decor, s shown) {
	g.reset(len(a.pages) + 1)
	m := a.m
	scale := float32(1 / m.dpr)
	f := s.f
	cols := min(s.cols, f.Cols)
	sel, hasSel := s.sel.normalized()
	curStyle, curShow := cursorLook(s)
	blockCursor := curShow && curStyle == "block"
	for r := 0; r < f.Rows && r < s.rows; r++ {
		y := s.originY + float64(r)*m.h
		abs := f.Top + int64(r)
		cells := f.Row(r)
		var clusters []string
		if r < len(f.Clusters) {
			clusters = f.Clusters[r]
		}
		// backgrounds, in runs of the same color
		runStart, runColor := 0, uint32(0)
		flushBg := func(end int) {
			if runColor != 0 && end > runStart {
				g.run(d, &d.block, s, runStart, end-runStart, y, runColor)
			}
		}
		for x := 0; x < cols; x++ {
			c := cells[x]
			var bg uint32
			if c.Flags&emulator.CellTail != 0 && x > 0 {
				bg = runColor // the right half of a wide character has the style of the left half
			} else {
				bg = g.styleOf(s, c.Style).bg
			}
			if bg != runColor {
				flushBg(x)
				runStart, runColor = x, bg
			}
		}
		flushBg(cols)
		// the selection and the marks over the backgrounds
		if hasSel {
			if x0, x1, ok := sel.rangeOn(abs, cols); ok {
				col := s.th.selection
				if !s.focused {
					col = s.th.selectionInact
				}
				g.run(d, &d.block, s, x0, x1-x0, y, col)
			}
		}
		for _, mk := range s.marks {
			if mk.line == abs && mk.x1 > mk.x0 {
				g.run(d, &d.block, s, min(mk.x0, cols), min(mk.x1, cols)-min(mk.x0, cols), y, mk.color)
			}
		}
		// the link under the pointer is underlined
		for _, lk := range s.links {
			if lk.line == abs && lk.x1 > lk.x0 {
				base := y + float64(m.ch-2*m.lineThick)/m.dpr
				for x := lk.x0; x < min(lk.x1, cols); x++ {
					g.decorAdd(d.line[0], scale, s.originX+float64(x)*m.w, base, s.th.fg)
				}
			}
		}
		// decorations and glyphs
		for x := 0; x < cols; x++ {
			c := cells[x]
			if c.Flags&(emulator.CellTail|emulator.CellWideFill) != 0 {
				continue
			}
			res := g.styleOf(s, c.Style)
			width := 1
			if c.Flags&emulator.CellWide != 0 {
				width = 2
			}
			cx := s.originX + float64(x)*m.w
			if res.underline != emulator.UnderlineNone || res.link {
				g.underline(d, s, res, x, width, y)
			}
			if res.strike {
				g.decorAdd(d.line[min(width-1, 1)], scale, cx, y+float64(m.ch)/2/m.dpr, res.fg)
			}
			if res.overline {
				g.decorAdd(d.line[min(width-1, 1)], scale, cx, y, res.fg)
			}
			if res.invisible || (c.Rune == 0 && c.Flags&emulator.CellCluster == 0) {
				continue
			}
			fg := res.fg
			if blockCursor && r == f.Cursor.Y && x == f.Cursor.X {
				fg = s.th.cursorText
			} else if hasSel && s.th.selectionFg>>24 != 0 {
				if x0, x1, ok := sel.rangeOn(abs, cols); ok && x >= x0 && x < x1 {
					fg = s.th.selectionFg
				}
			}
			attrs := res.attrs
			if width == 2 {
				attrs |= attrWide
			}
			var sl *slot
			if c.Flags&emulator.CellCluster != 0 {
				if int(c.Rune) < len(clusters) {
					sl = a.lookupCluster(clusters[c.Rune], attrs)
				}
			} else {
				sl = a.lookup(rune(c.Rune), attrs)
			}
			if sl == nil {
				continue
			}
			g.glyphAdd(a, sl, scale, cx, y, fg, width)
		}
	}
	g.cursor(d, s)
}

// glyphAdd adds a glyph sprite. The slot may wait for the flush: its page and place are known; only its pixels are not.
func (g *gridPainter) glyphAdd(a *atlas, sl *slot, scale float32, x, y float64, fg uint32, width int) {
	if sl.blank || sl.page == 0 {
		return
	}
	pi := int(sl.page) - 1
	for len(g.gTr) <= pi {
		g.gTr, g.gRc, g.gCol = append(g.gTr, nil), append(g.gRc, nil), append(g.gCol, nil)
	}
	g.gTr[pi] = append(g.gTr[pi], gui.RSTransform{Scos: scale, Tx: float32(x), Ty: float32(y)})
	g.gRc[pi] = append(g.gRc[pi], gui.NewRectFromLTWH(float64(sl.x), float64(sl.y), float64(width*a.m.cw), float64(a.m.ch)))
	if sl.colored {
		g.gCol[pi] = append(g.gCol[pi], gui.NewColor(0xFFFFFFFF))
	} else {
		g.gCol[pi] = append(g.gCol[pi], gui.NewColor(fg))
	}
}

// underline adds the underline of a cell of the kind of its style (a link is underlined with a single line).
func (g *gridPainter) underline(d *decor, s shown, res *resolved, x, width int, y float64) {
	m := d.m
	scale := float32(1 / m.dpr)
	kind := res.underline
	if kind == emulator.UnderlineNone {
		kind = emulator.UnderlineSingle
	}
	// the underline sits one light line above the bottom of the cell
	base := y + float64(m.ch-2*m.lineThick)/m.dpr
	cx := s.originX + float64(x)*m.w
	for i := 0; i < width; i++ {
		px := cx + float64(i)*m.w
		switch kind {
		case emulator.UnderlineSingle:
			g.decorAdd(d.line[0], scale, px, base, res.underCol)
		case emulator.UnderlineDouble:
			g.decorAdd(d.line[0], scale, px, base, res.underCol)
			g.decorAdd(d.line[0], scale, px, base-float64(2*m.lineThick)/m.dpr, res.underCol)
		case emulator.UnderlineCurly:
			g.decorAdd(d.curly, scale, px, base-float64(m.lineThick)/m.dpr, res.underCol)
		case emulator.UnderlineDotted:
			g.decorAdd(d.dotted, scale, px, base, res.underCol)
		case emulator.UnderlineDashed:
			g.decorAdd(d.dashed, scale, px, base, res.underCol)
		}
	}
}

// cursorLook decides how the cursor is drawn: its style ("block", "line", "underline", "outline"), and whether it is drawn at all.
func cursorLook(s shown) (style string, show bool) {
	f := s.f
	cur := f.Cursor
	if !s.cursorOn || !cur.Visible || cur.Y < 0 || cur.Y >= f.Rows || cur.X < 0 || cur.X >= f.Cols || cur.Y >= s.rows || cur.X >= s.cols {
		return "", false
	}
	style = s.cfg.cursorStyle
	switch cur.Shape {
	case emulator.CursorBar:
		style = "line"
	case emulator.CursorUnderline:
		style = "underline"
	}
	if !s.focused {
		style = s.cfg.cursorInactive
	} else if (s.cfg.cursorBlink || cur.Blink) && !s.blinkOn {
		return "", false
	}
	return style, style != "none" && style != ""
}

// cursor adds the cursor. A block cursor is a block of the cursor's color; the character under it is drawn in the color of the
// cursor's text by the loop of build.
func (g *gridPainter) cursor(d *decor, s shown) {
	style, show := cursorLook(s)
	if !show {
		return
	}
	f := s.f
	cur := f.Cursor
	m := d.m
	scale := float32(1 / m.dpr)
	cell := f.Cell(cur.Y, cur.X)
	width := 1
	if cell.Flags&emulator.CellWide != 0 {
		width = 2
	}
	x := s.originX + float64(cur.X)*m.w
	y := s.originY + float64(cur.Y)*m.h
	col := s.th.cursor
	k := min(width-1, 1)
	switch style {
	case "outline":
		w, h := float64(width)*m.w, m.h
		t := float64(m.lineThick) / m.dpr
		g.decorAdd(d.line[k], scale, x, y, col)
		g.decorAdd(d.line[k], scale, x, y+h-t, col)
		g.decorAdd(d.bar, scale, x, y, col)
		g.decorAdd(d.bar, scale, x+w-t, y, col)
	case "line":
		bar := d.bar
		if w := max(int(s.cfg.cursorWidth), 1); w > 1 {
			bar = gui.NewRectFromLTWH(0, 0, float64(min(w*m.lineThick, m.cw)), float64(m.ch))
		}
		g.decorAdd(bar, scale, x, y, col)
	case "underline":
		g.decorAdd(d.thick[k], scale, x, y+m.h-float64(2*m.lineThick)/m.dpr, col)
	default:
		g.decorAdd(d.block[k], scale, x, y, col)
	}
}

// draw issues the DrawAtlas calls.
func (g *gridPainter) draw(cv *gui.Canvas, a *atlas, d *decor) {
	if len(g.dTr) > 0 {
		cv.DrawAtlas(d.img, g.dTr, g.dRc, g.dCol, gui.BlendModeModulate, nil, g.paint)
	}
	for i := range g.gTr {
		if len(g.gTr[i]) == 0 || i >= len(a.pages) || a.pages[i].img == nil {
			continue
		}
		cv.DrawAtlas(a.pages[i].img, g.gTr[i], g.gRc[i], g.gCol[i], gui.BlendModeModulate, nil, g.paint)
	}
}
