package terminal

import "math"

// Shapes: the box-drawing, block, braille and powerline characters are not taken from the font. They are drawn as shapes that
// fill the cell exactly, so that lines join across cells whatever the font and the size are, as the fast terminals do (SPEC
// §10.25 "Speed"). A shape is a coverage mask of one block (a cell, or two for a wide character) that the glyph atlas holds
// like any glyph and tints with the color of the cell.

// mask is the coverage of a block of w by h pixels, 0 to 255.
type mask struct {
	a    []uint8
	w, h int
}

func newMask(w, h int) *mask { return &mask{a: make([]uint8, w*h), w: w, h: h} }

// rect fills the pixels [x0,x1) by [y0,y1) with the coverage v (the larger of it and what is there), clipped to the mask.
func (m *mask) rect(x0, y0, x1, y1 int, v uint8) {
	x0, y0, x1, y1 = max(x0, 0), max(y0, 0), min(x1, m.w), min(y1, m.h)
	for y := y0; y < y1; y++ {
		row := m.a[y*m.w : (y+1)*m.w]
		for x := x0; x < x1; x++ {
			if row[x] < v {
				row[x] = v
			}
		}
	}
}

// isShape tells whether the rune is drawn as a shape.
func isShape(r rune) bool {
	switch {
	case r >= 0x2500 && r <= 0x259F:
		return true
	case r >= 0x2800 && r <= 0x28FF:
		return true
	case r >= 0xE0B0 && r <= 0xE0B3:
		return true
	}
	return false
}

// shapeMask draws the shape of the rune in a block of w by h pixels. lt is the thickness of a light line in pixels.
func shapeMask(r rune, w, h, lt int) *mask {
	m := newMask(w, h)
	switch {
	case r >= 0x2500 && r <= 0x257F:
		boxDrawing(m, r, lt)
	case r >= 0x2580 && r <= 0x259F:
		blockElement(m, r)
	case r >= 0x2800 && r <= 0x28FF:
		braille(m, r)
	default:
		powerline(m, r, lt)
	}
	return m
}

// The weights of an arm of a box-drawing character.
const (
	wNone = iota
	wLight
	wHeavy
	wDouble
)

// boxArms are the weights of the up, down, left and right arms of U+2500 to U+257F, in that order, one decimal digit each: 0 no
// arm, 1 light, 2 heavy, 3 double. Dashed lines are the solid ones drawn with gaps (dashes); the rounded corners U+256D to
// U+2570 and the diagonals U+2571 to U+2573 are drawn apart.
var boxArms = func() (t [128]string) {
	set := func(code int, arms string) { t[code-0x2500] = arms }
	// U+2500 to U+250B: the lines and their dashed forms
	for i, arms := range []string{"0011", "0022", "1100", "2200", "0011", "0022", "1100", "2200", "0011", "0022", "1100", "2200"} {
		set(0x2500+i, arms)
	}
	// U+250C to U+254B in the order of the Unicode chart
	for i, arms := range []string{
		"0101", "0102", "0201", "0202", // ┌ ┍ ┎ ┏
		"0110", "0120", "0210", "0220", // ┐ ┑ ┒ ┓
		"1001", "1002", "2001", "2002", // └ ┕ ┖ ┗
		"1010", "1020", "2010", "2020", // ┘ ┙ ┚ ┛
		"1101", "1102", "2101", "1201", "2201", "2102", "1202", "2202", // ├ ┝ ┞ ┟ ┠ ┡ ┢ ┣
		"1110", "1120", "2110", "1210", "2210", "2120", "1220", "2220", // ┤ ┥ ┦ ┧ ┨ ┩ ┪ ┫
		"0111", "0121", "0112", "0122", "0211", "0221", "0212", "0222", // ┬ ┭ ┮ ┯ ┰ ┱ ┲ ┳
		"1011", "1021", "1012", "1022", "2011", "2021", "2012", "2022", // ┴ ┵ ┶ ┷ ┸ ┹ ┺ ┻
		"1111", "1121", "1112", "1122", "2111", "1211", "2211", "2121", // ┼ ┽ ┾ ┿ ╀ ╁ ╂ ╃
		"2112", "1221", "1212", "2122", "1222", "2221", "2212", "2222", // ╄ ╅ ╆ ╇ ╈ ╉ ╊ ╋
	} {
		set(0x250C+i, arms)
	}
	// U+254C to U+254F: the lines with two dashes
	for i, arms := range []string{"0011", "0022", "1100", "2200"} {
		set(0x254C+i, arms)
	}
	// U+2550 to U+256C: the double lines, and the corners that mix them with light ones
	for i, arms := range []string{
		"0033", "3300", "0103", "0301", "0303", "0130", "0310", "0330", // ═ ║ ╒ ╓ ╔ ╕ ╖ ╗
		"1003", "3001", "3003", "1030", "3010", "3030", "1103", "3301", // ╘ ╙ ╚ ╛ ╜ ╝ ╞ ╟
		"3303", "1130", "3310", "3330", "0133", "0311", "0333", "1033", // ╠ ╡ ╢ ╣ ╤ ╥ ╦ ╧
		"3011", "3033", "1133", "3311", "3333", // ╨ ╩ ╪ ╫ ╬
	} {
		set(0x2550+i, arms)
	}
	// U+2574 to U+257F: the half lines
	for i, arms := range []string{
		"0010", "1000", "0001", "0100", "0020", "2000", "0002", "0200", // ╴ ╵ ╶ ╷ ╸ ╹ ╺ ╻
		"0012", "1200", "0021", "2100", // ╼ ╽ ╾ ╿
	} {
		set(0x2574+i, arms)
	}
	return t
}()

// dashes is the number of dashes of the dashed lines U+2504 to U+250B (three and four) and U+254C to U+254F (two), 0 for a
// solid line.
func dashes(r rune) int {
	switch {
	case r >= 0x2504 && r <= 0x2507:
		return 3
	case r >= 0x2508 && r <= 0x250B:
		return 4
	case r >= 0x254C && r <= 0x254F:
		return 2
	}
	return 0
}

// boxDrawing draws U+2500 to U+257F.
func boxDrawing(m *mask, r rune, lt int) {
	switch {
	case r >= 0x256D && r <= 0x2570:
		roundedCorner(m, r, lt)
		return
	case r >= 0x2571 && r <= 0x2573:
		diagonals(m, r, lt)
		return
	}
	arms := boxArms[r-0x2500]
	if arms == "" {
		return
	}
	up, down, left, right := int(arms[0]-'0'), int(arms[1]-'0'), int(arms[2]-'0'), int(arms[3]-'0')
	if n := dashes(r); n > 0 {
		dashedLine(m, up+down > 0, max(up, down, left, right), n, lt)
		return
	}
	// the arms along the width (left and right) and along the height (up and down)
	armsAlong(m, false, left, right, up, down, lt)
	armsAlong(m, true, up, down, left, right, lt)
}

// thickness of a line of a weight in pixels (a double line's bars are light).
func thickness(w, lt int) int {
	if w == wHeavy {
		return 3 * lt
	}
	return lt
}

// band is the pixels [lo, hi) a line of the weight w covers across a length of n pixels, centered.
func band(n, w, lt int) (lo, hi int) {
	t := min(thickness(w, lt), n)
	lo = (n - t) / 2
	return lo, lo + t
}

// bars are the two bars of a double line across a length of n pixels: [lo0,hi0) and [lo1,hi1).
func bars(n, lt int) (lo0, hi0, lo1, hi1 int) {
	gap := 2 * lt
	total := 2*lt + gap
	start := max((n-total)/2, 0)
	return start, start + lt, start + lt + gap, start + 2*lt + gap
}

// armsAlong draws the two arms of a box-drawing character that run along one axis. With transpose false they are the left and
// right arms and the length is the width of the mask; with transpose true they are the up and down arms and the length is the
// height. neg and pos are the weights of the arm toward the start and toward the end of the axis; pneg and ppos those of the
// arms across it, toward the start and the end of the other axis. A single or heavy arm is one bar; a double arm is two, and
// each ends where the crossing lines say: at the nearer bar of a crossing arm on its side, at the middle when the opposite
// arm continues it, and on the far bar of the crossing arm on the other side when it closes the outer corner.
func armsAlong(m *mask, transpose bool, neg, pos, pneg, ppos, lt int) {
	length, across := m.w, m.h
	if transpose {
		length, across = m.h, m.w
	}
	mid := (length + 1) / 2
	fill := func(a, b, c, d int) {
		if transpose {
			m.rect(c, a, d, b, 255)
		} else {
			m.rect(a, c, b, d, 255)
		}
	}
	// perp is where a crossing line of the weight p is along the axis: the pixels of its bars, [lo0,hi0) the nearer to the
	// start and [lo1,hi1) the nearer to the end (the same for a single line)
	perp := func(p int) (lo0, hi0, lo1, hi1 int) {
		if p == wDouble {
			return bars(length, lt)
		}
		lo, hi := band(length, p, lt)
		return lo, hi, lo, hi
	}
	type bar struct{ c, d, side int }
	barsOf := func(w int) []bar {
		if w == wDouble {
			c0, d0, c1, d1 := bars(across, lt)
			return []bar{{c0, d0, 0}, {c1, d1, 1}}
		}
		c, d := band(across, w, lt)
		return []bar{{c, d, -1}}
	}
	perps := [2]int{pneg, ppos}
	// edge is where the arm toward the start (toStart) or the end of the axis meets its bar's neighbors, along the axis
	edge := func(b bar, toStart bool, opposite int) int {
		near := func(p int) int {
			_, hi0, lo1, _ := perp(p)
			if toStart {
				return hi0
			}
			return lo1
		}
		far := func(p int) int {
			lo0, _, _, hi1 := perp(p)
			if toStart {
				return hi1
			}
			return lo0
		}
		centre := length / 2
		if toStart {
			centre = mid
		}
		if b.side < 0 {
			// one bar: it ends at the crossing lines, and at the nearer bar when one of them is double
			switch {
			case perps[0] == wDouble || perps[1] == wDouble:
				return near(wDouble)
			case perps[0] != wNone || perps[1] != wNone:
				return near(max(perps[0], perps[1]))
			}
			return centre
		}
		if p := perps[b.side]; p != wNone {
			return near(p)
		}
		if opposite != wNone {
			return centre
		}
		if o := perps[1-b.side]; o != wNone {
			return far(o)
		}
		return centre
	}
	if neg != wNone {
		for _, b := range barsOf(neg) {
			fill(0, edge(b, true, pos), b.c, b.d)
		}
	}
	if pos != wNone {
		for _, b := range barsOf(pos) {
			fill(edge(b, false, neg), length, b.c, b.d)
		}
	}
}

// dashedLine draws the dashed lines: n dashes along the axis (vertical when vertical is set), light or heavy.
func dashedLine(m *mask, vertical bool, weight, n, lt int) {
	length, across := m.w, m.h
	if vertical {
		length, across = m.h, m.w
	}
	c, d := band(across, weight, lt)
	// n dashes and n gaps, each gap about a third of a period, the first dash starting half a gap in
	period := float64(length) / float64(n)
	gap := max(int(math.Round(period/3)), 1)
	for i := 0; i < n; i++ {
		a := int(math.Round(float64(i)*period)) + gap/2
		b := int(math.Round(float64(i+1)*period)) - (gap - gap/2)
		if i == n-1 {
			b = length - gap/2
		}
		if vertical {
			m.rect(c, a, d, b, 255)
		} else {
			m.rect(a, c, b, d, 255)
		}
	}
}

// roundedCorner draws U+256D to U+2570: an arc from the middle of one edge to the middle of another, as lines of one light
// thickness, with the coverage of a pixel taken from its distance to the arc.
func roundedCorner(m *mask, r rune, lt int) {
	w, h := float64(m.w), float64(m.h)
	cx, cy := w/2, h/2
	// the corner connects the right or left edge with the bottom or top one; the arc's center is the corner of the cell
	right := r == 0x256D || r == 0x2570 // ╭ ╰ have the arm to the right
	down := r == 0x256D || r == 0x256E  // ╭ ╮ have the arm down
	// the center of the circle is at the cell edge the arms point away from
	var px, py float64
	if right {
		px = w
	}
	if down {
		py = h
	}
	rx, ry := math.Abs(px-cx), math.Abs(py-cy)
	t := float64(lt)
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			// four samples per pixel
			cover := 0
			for sy := 0; sy < 2; sy++ {
				for sx := 0; sx < 2; sx++ {
					fx, fy := float64(x)+0.25+0.5*float64(sx), float64(y)+0.25+0.5*float64(sy)
					// only the quarter of the plane between the arms
					if (right && fx > cx || !right && fx < cx) && (down && fy > cy || !down && fy < cy) {
						// the elliptical arc around (px, py)
						d := math.Hypot((fx-px)/rx, (fy-py)/ry)
						dist := math.Abs(d-1) * math.Min(rx, ry)
						if dist <= t/2 {
							cover++
						}
					}
				}
			}
			if cover > 0 {
				if v := uint8(255 * cover / 4); v > m.a[y*m.w+x] {
					m.a[y*m.w+x] = v
				}
			}
		}
	}
	// the straight parts: from the middle of the edge to where the arc starts
	if right {
		m.rect(int(cx+rx), (m.h-lt)/2, m.w, (m.h-lt)/2+lt, 255)
	} else {
		m.rect(0, (m.h-lt)/2, int(cx-rx)+1, (m.h-lt)/2+lt, 255)
	}
	if down {
		m.rect((m.w-lt)/2, int(cy+ry), (m.w-lt)/2+lt, m.h, 255)
	} else {
		m.rect((m.w-lt)/2, 0, (m.w-lt)/2+lt, int(cy-ry)+1, 255)
	}
}

// diagonals draws U+2571 to U+2573, from corner to corner.
func diagonals(m *mask, r rune, lt int) {
	line := func(x0, y0, x1, y1 float64) {
		t := float64(lt)
		dx, dy := x1-x0, y1-y0
		length := math.Hypot(dx, dy)
		for y := 0; y < m.h; y++ {
			for x := 0; x < m.w; x++ {
				cover := 0
				for sy := 0; sy < 2; sy++ {
					for sx := 0; sx < 2; sx++ {
						fx, fy := float64(x)+0.25+0.5*float64(sx), float64(y)+0.25+0.5*float64(sy)
						dist := math.Abs(dy*(fx-x0)-dx*(fy-y0)) / length
						if dist <= t/2 {
							cover++
						}
					}
				}
				if cover > 0 {
					if v := uint8(255 * cover / 4); v > m.a[y*m.w+x] {
						m.a[y*m.w+x] = v
					}
				}
			}
		}
	}
	w, h := float64(m.w), float64(m.h)
	if r == 0x2571 || r == 0x2573 {
		line(w, 0, 0, h)
	}
	if r == 0x2572 || r == 0x2573 {
		line(0, 0, w, h)
	}
}

// blockElement draws U+2580 to U+259F.
func blockElement(m *mask, r rune) {
	w, h := m.w, m.h
	frac := func(n, d, total int) int { return (total*n + d/2) / d }
	switch {
	case r == 0x2580: // upper half
		m.rect(0, 0, w, frac(1, 2, h), 255)
	case r >= 0x2581 && r <= 0x2588: // lower one eighth to the whole cell
		n := int(r - 0x2580)
		m.rect(0, h-frac(n, 8, h), w, h, 255)
	case r >= 0x2589 && r <= 0x258F: // left seven eighths down to one eighth
		n := 8 - int(r-0x2588)
		m.rect(0, 0, frac(n, 8, w), h, 255)
	case r == 0x2590: // right half
		m.rect(frac(1, 2, w), 0, w, h, 255)
	case r == 0x2591:
		m.rect(0, 0, w, h, 64)
	case r == 0x2592:
		m.rect(0, 0, w, h, 128)
	case r == 0x2593:
		m.rect(0, 0, w, h, 192)
	case r == 0x2594: // upper one eighth
		m.rect(0, 0, w, max(frac(1, 8, h), 1), 255)
	case r == 0x2595: // right one eighth
		m.rect(w-max(frac(1, 8, w), 1), 0, w, h, 255)
	default: // quadrants U+2596 to U+259F: bit 0 upper left, 1 upper right, 2 lower left, 3 lower right
		quads := [10]uint8{0b0100, 0b1000, 0b0001, 0b1101, 0b1001, 0b0111, 0b1011, 0b0010, 0b0110, 0b1110}
		q := quads[r-0x2596]
		hx, hy := frac(1, 2, w), frac(1, 2, h)
		if q&1 != 0 {
			m.rect(0, 0, hx, hy, 255)
		}
		if q&2 != 0 {
			m.rect(hx, 0, w, hy, 255)
		}
		if q&4 != 0 {
			m.rect(0, hy, hx, h, 255)
		}
		if q&8 != 0 {
			m.rect(hx, hy, w, h, 255)
		}
	}
}

// braille draws U+2800 to U+28FF: the dots of a cell of two columns by four rows. Bit 0 to 7 of the offset are dots 1, 2, 3, 7
// (the left column from the top, then the lower left) and 4, 5, 6, 8 (the right column, then the lower right).
func braille(m *mask, r rune) {
	bits := uint8(r - 0x2800)
	pos := [8][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {1, 1}, {1, 2}, {0, 3}, {1, 3}}
	w, h := float64(m.w), float64(m.h)
	radius := math.Max(math.Min(w/4, h/8)*0.8, 0.6)
	for i := 0; i < 8; i++ {
		if bits&(1<<i) == 0 {
			continue
		}
		cx := w * (0.25 + 0.5*float64(pos[i][0]))
		cy := h * (0.125 + 0.25*float64(pos[i][1]))
		for y := max(int(cy-radius)-1, 0); y < min(int(cy+radius)+2, m.h); y++ {
			for x := max(int(cx-radius)-1, 0); x < min(int(cx+radius)+2, m.w); x++ {
				cover := 0
				for sy := 0; sy < 2; sy++ {
					for sx := 0; sx < 2; sx++ {
						fx, fy := float64(x)+0.25+0.5*float64(sx), float64(y)+0.25+0.5*float64(sy)
						if math.Hypot(fx-cx, fy-cy) <= radius {
							cover++
						}
					}
				}
				if v := uint8(255 * cover / 4); v > m.a[y*m.w+x] {
					m.a[y*m.w+x] = v
				}
			}
		}
	}
}

// powerline draws the arrows of the powerline fonts, U+E0B0 to U+E0B3: a filled triangle and a chevron pointing right or left.
func powerline(m *mask, r rune, lt int) {
	w, h := float64(m.w), float64(m.h)
	right := r == 0xE0B0 || r == 0xE0B1
	filled := r == 0xE0B0 || r == 0xE0B2
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			cover := 0
			for sy := 0; sy < 2; sy++ {
				for sx := 0; sx < 2; sx++ {
					fx, fy := float64(x)+0.25+0.5*float64(sx), float64(y)+0.25+0.5*float64(sy)
					// the edge of the arrow at this height: from the left side at the top and bottom to the tip in the middle
					t := 1 - math.Abs(fy-h/2)/(h/2)
					edge := t * w
					if !right {
						edge = w - edge
					}
					in := fx <= edge
					if !right {
						in = fx >= edge
					}
					if filled {
						if in {
							cover++
						}
						continue
					}
					// the chevron is a line along the edge
					d := math.Abs(fx-edge) * h / math.Hypot(w, h)
					if d <= float64(lt)*1.0 {
						cover++
					}
				}
			}
			if cover > 0 {
				m.a[y*m.w+x] = uint8(255 * cover / 4)
			}
		}
	}
}
