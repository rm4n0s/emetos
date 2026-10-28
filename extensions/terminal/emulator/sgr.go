package emulator

// sgr is SGR, select graphic rendition (ECMA-48 8.3.117; ctlseqs "CSI Pm m"). Parameters separated by ':' are the
// sub-parameters of ITU T.416 that ctlseqs uses for the underline styles (4:Ps) and the colors (38:2::r:g:b, 38:5:n).
func (e *Emulator) sgr() {
	p := &e.pen
	e.penOK, e.bceOK = false, false
	if e.nparams == 0 {
		e.resetPen()
		return
	}
	n := e.nparams
	for i := 0; i < n; i++ {
		v := int(e.params[i])
		if v < 0 {
			v = 0
		}
		switch {
		case v == 0:
			e.resetPen()
		case v == 1:
			p.Attrs |= AttrBold
		case v == 2:
			p.Attrs |= AttrDim
		case v == 3:
			p.Attrs |= AttrItalic
		case v == 4:
			p.Underline = UnderlineSingle
			if i+1 < n && e.colonAt(i+1) {
				i++
				switch e.params[i] {
				case 0:
					p.Underline = UnderlineNone
				case 2:
					p.Underline = UnderlineDouble
				case 3:
					p.Underline = UnderlineCurly
				case 4:
					p.Underline = UnderlineDotted
				case 5:
					p.Underline = UnderlineDashed
				}
			}
		case v == 5 || v == 6:
			p.Attrs |= AttrBlink
		case v == 7:
			p.Attrs |= AttrInverse
		case v == 8:
			p.Attrs |= AttrInvisible
		case v == 9:
			p.Attrs |= AttrStrike
		case v == 21:
			p.Underline = UnderlineDouble
		case v == 22:
			p.Attrs &^= AttrBold | AttrDim
		case v == 23:
			p.Attrs &^= AttrItalic
		case v == 24:
			p.Underline = UnderlineNone
		case v == 25:
			p.Attrs &^= AttrBlink
		case v == 27:
			p.Attrs &^= AttrInverse
		case v == 28:
			p.Attrs &^= AttrInvisible
		case v == 29:
			p.Attrs &^= AttrStrike
		case v >= 30 && v <= 37:
			p.Fg = IndexedColor(uint8(v - 30))
		case v == 38:
			i = e.extendedColor(i, &p.Fg)
		case v == 39:
			p.Fg = 0
		case v >= 40 && v <= 47:
			p.Bg = IndexedColor(uint8(v - 40))
		case v == 48:
			i = e.extendedColor(i, &p.Bg)
		case v == 49:
			p.Bg = 0
		case v == 53:
			p.Attrs |= AttrOverline
		case v == 55:
			p.Attrs &^= AttrOverline
		case v == 58:
			i = e.extendedColor(i, &p.UnderlineColor)
		case v == 59:
			p.UnderlineColor = 0
		case v >= 90 && v <= 97:
			p.Fg = IndexedColor(uint8(v - 90 + 8))
		case v >= 100 && v <= 107:
			p.Bg = IndexedColor(uint8(v - 100 + 8))
		}
		// 10 to 20 (fonts), 26, 50 to 52, 54, 56, 57, 60 to 65 are ignored.
	}
}

// resetPen is SGR 0: the default rendition. A hyperlink in force (OSC 8) is not part of the rendition and stays.
func (e *Emulator) resetPen() { e.pen = Style{Link: e.pen.Link} }

// colonAt reports whether parameter i was separated from the one before it by ':'.
func (e *Emulator) colonAt(i int) bool { return i < 32 && e.colons&(1<<uint(i)) != 0 }

// extendedColor reads the arguments of SGR 38, 48 and 58 that follow parameter i, sets *c and returns the index of the
// last parameter it used (ctlseqs: "38 ; 5 ; Ps", "38 ; 2 ; Pr ; Pg ; Pb", and the ':' forms).
func (e *Emulator) extendedColor(i int, c *Color) int {
	n := e.nparams
	if i+1 >= n {
		return i
	}
	if e.colonAt(i + 1) {
		// 38:5:n  38:2:r:g:b  38:2::r:g:b (the empty one is the color space id)
		end := i + 1
		for end+1 < n && e.colonAt(end+1) {
			end++
		}
		args := e.params[i+1 : end+1]
		switch {
		case args[0] == 5 && len(args) >= 2:
			*c = IndexedColor(clampByte(args[1]))
		case args[0] == 2 && len(args) >= 5:
			*c = RGBColor(clampByte(args[2]), clampByte(args[3]), clampByte(args[4]))
		case args[0] == 2 && len(args) == 4:
			*c = RGBColor(clampByte(args[1]), clampByte(args[2]), clampByte(args[3]))
		}
		return end
	}
	switch e.params[i+1] {
	case 5:
		if i+2 < n {
			*c = IndexedColor(clampByte(e.params[i+2]))
			return i + 2
		}
		return n - 1
	case 2:
		if i+4 < n {
			*c = RGBColor(clampByte(e.params[i+2]), clampByte(e.params[i+3]), clampByte(e.params[i+4]))
			return i + 4
		}
		return n - 1
	}
	return i + 1
}

func clampByte(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}
