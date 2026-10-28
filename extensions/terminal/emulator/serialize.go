package emulator

import (
	"net/url"
	"strconv"
)

// Serialize writes the terminal back as escape sequences, so that a new Emulator of the same size, fed with them, draws the
// same terminal: the cells of the last scrollback lines (all of them when scrollback is negative) and of the screen with
// their styles, wide characters, clusters, hyperlinks and the wrapping of long lines, the cursor, the scroll region, the
// tab stops, the modes of SPEC §10.25 (cursor and keypad keys, autowrap, origin, insert, cursor look, mouse, focus, paste),
// the title, the working folder and the colors the program changed. When the alternate screen is shown, the main one is
// written first and the alternate one after DECSET 1049. It starts with a full reset, so that it can also be fed to a
// terminal that was used. In origin mode a cursor outside the scroll region is written on the nearest margin. Not written: the saved cursor, the character sets, a synchronized update, a pending DCS or OSC.
func (e *Emulator) Serialize(scrollback int) []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	w := &serializer{e: e}
	w.run(scrollback)
	return w.out
}

type serializer struct {
	e   *Emulator
	out []byte
	pen Style
	// link is the hyperlink being written (0 for none), as an index into the emulator's table.
	link uint16
}

func (w *serializer) str(s string) { w.out = append(w.out, s...) }

func (w *serializer) int(n int) { w.out = strconv.AppendInt(w.out, int64(n), 10) }

func (w *serializer) csi(params string, final byte) {
	w.out = append(w.out, 0x1b, '[')
	w.out = append(w.out, params...)
	w.out = append(w.out, final)
}

func (w *serializer) cup(row, col int) {
	w.out = append(w.out, 0x1b, '[')
	w.int(row + 1)
	w.out = append(w.out, ';')
	w.int(col + 1)
	w.out = append(w.out, 'H')
}

// setStyle writes the sequences that turn the pen and the hyperlink into st.
func (w *serializer) setStyle(st Style) {
	if st.Link != w.link {
		w.str("\x1b]8;")
		if st.Link != 0 {
			l := w.e.st.links[st.Link]
			if l.ID != "" {
				w.str("id=" + l.ID)
			}
			w.str(";" + l.URL)
		} else {
			w.str(";")
		}
		w.str("\x1b\\")
		w.link = st.Link
	}
	a, b := w.pen, st
	a.Link, b.Link = 0, 0
	if a != b {
		w.out = append(w.out, 0x1b, '[')
		w.out = appendSGR(w.out, b)
		w.out = append(w.out, 'm')
		w.pen = st
	}
	w.pen.Link = st.Link
}

// continues reports whether the text of l goes on in next: l autowrapped and the row after it holds something (a flag that
// a later erase left behind does not count).
func continues(l, next *line) bool {
	return l.wrapped && next != nil && !lineBlank(next)
}

// writeLine writes the cells of l. A wrapped row is written in full so that the terminal wraps by itself; the others
// without their trailing empty cells.
func (w *serializer) writeLine(l, next *line) {
	wrapped := continues(l, next)
	end := len(l.cells)
	if !wrapped {
		for end > 0 && l.cells[end-1].isPlainBlank() {
			end--
		}
	}
	for x := 0; x < end; x++ {
		c := l.cells[x]
		if c.Flags&CellTail != 0 {
			continue
		}
		if c.Flags&CellWideFill != 0 && x == len(l.cells)-1 && wrapped && next.cells[0].Flags&CellWide != 0 {
			// The wide character that follows makes the terminal leave this cell empty by itself.
			continue
		}
		w.setStyle(w.e.st.styles[c.Style])
		switch {
		case c.Flags&CellCluster != 0:
			w.str(l.clusterString(c))
		case c.Rune == 0:
			w.out = append(w.out, ' ')
		default:
			w.out = utf8AppendRune(w.out, rune(c.Rune))
		}
	}
	if end > 0 && end < len(l.cells) {
		// The empty cells after the text are the default ones, whatever the pen was when the row was made (a row that
		// scrolled in takes the pen's background).
		w.resetPen()
		w.csi("", 'K')
	}
}

func (w *serializer) resetPen() {
	if w.pen != (Style{}) || w.link != 0 {
		w.setStyle(Style{})
	}
}

func (w *serializer) run(scrollback int) {
	e := w.e
	main := e.mainScr
	w.str("\x1bc\x1b[3J")
	// The main buffer: the scrollback and then the screen, one row after the other, so that the rows that scroll off become
	// the scrollback of the new terminal.
	first := 0
	if sb := main.scrollbackLen(); scrollback >= 0 && sb > scrollback {
		first = sb - scrollback
	}
	for i := first; i < main.ring.n; i++ {
		l := main.ring.at(i)
		var next *line
		if i+1 < main.ring.n {
			next = main.ring.at(i + 1)
		}
		w.writeLine(l, next)
		if i < main.ring.n-1 && !continues(l, next) {
			w.resetPen()
			w.str("\r\n")
		}
	}
	w.resetPen()
	cur := main
	if e.modes.AltScreen {
		// The cursor of the main buffer is what DECSET 1049 saves.
		w.margins(main)
		if main.wrapNext {
			w.restorePendingWrap(main, main.y, main.y)
		} else {
			w.cup(main.y, main.x)
		}
		w.str("\x1b[?1049h")
		cur = e.altScr
		for y := 0; y < cur.rows; y++ {
			l := cur.row(y)
			w.cup(y, 0)
			w.writeLine(l, nil)
			w.resetPen()
		}
	}
	w.margins(cur)
	w.modes()
	w.metadata()
	// The cursor and the pen last.
	w.cursor(cur)
	if e.pen != (Style{}) {
		w.setStyle(e.pen)
	}
}

// restorePendingWrap puts the cursor in the state "at the last column, wrap pending" by writing the last character again.
func (w *serializer) restorePendingWrap(s *screen, abs, y int) {
	l := s.row(abs)
	x := s.cols - 1
	if l.cells[x].Flags&CellTail != 0 && x > 0 {
		x--
	}
	c := l.cells[x]
	w.cup(y, x)
	w.setStyle(w.e.st.styles[c.Style])
	switch {
	case c.Flags&CellCluster != 0:
		w.str(l.clusterString(c))
	case c.Rune == 0:
		w.out = append(w.out, ' ')
	default:
		w.out = utf8AppendRune(w.out, rune(c.Rune))
	}
	w.resetPen()
}

func (w *serializer) mode(n int, on bool) {
	w.str("\x1b[?")
	w.int(n)
	if on {
		w.out = append(w.out, 'h')
	} else {
		w.out = append(w.out, 'l')
	}
}

// modes writes the modes.
func (w *serializer) modes() {
	e := w.e
	m := e.modes
	if m.AppCursorKeys {
		w.mode(1, true)
	}
	if m.AppKeypad {
		w.str("\x1b=")
	}
	if !m.Autowrap {
		w.mode(7, false)
	}
	if m.Insert {
		w.csi("4", 'h')
	}
	if m.NewLine {
		w.csi("20", 'h')
	}
	if m.ReverseVideo {
		w.mode(5, true)
	}
	if m.ReverseWrap {
		w.mode(45, true)
	}
	if !m.CursorVisible {
		w.mode(25, false)
	}
	cs := 2 * int(m.CursorShape)
	if m.CursorBlink {
		cs++
	} else {
		cs += 2
	}
	if cs != 2 { // steady block is the start value
		w.csi(strconv.Itoa(cs)+" ", 'q')
	}
	switch m.Mouse {
	case MouseX10:
		w.mode(9, true)
	case MouseNormal:
		w.mode(1000, true)
	case MouseDrag:
		w.mode(1002, true)
	case MouseAny:
		w.mode(1003, true)
	}
	switch m.MouseEnc {
	case MouseEncUTF8:
		w.mode(1005, true)
	case MouseEncSGR:
		w.mode(1006, true)
	case MouseEncURXVT:
		w.mode(1015, true)
	}
	if m.FocusEvents {
		w.mode(1004, true)
	}
	if m.BracketedPaste {
		w.mode(2004, true)
	}
	if m.AltScroll {
		w.mode(1007, true)
	}
	if !m.MetaSendsEscape {
		w.mode(1036, false)
	}
	if m.Origin {
		w.mode(6, true)
	}
}

// margins writes the tab stops that differ from the default ones and the scroll region of a buffer (the region homes the
// cursor).
func (w *serializer) margins(s *screen) {
	// Tab stops that differ from the default ones.
	def := defaultTabs(s.cols)
	custom := false
	for i := range def {
		if def[i] != s.tabs[i] {
			custom = true
			break
		}
	}
	if custom {
		w.csi("3", 'g')
		for i, t := range s.tabs {
			if t {
				w.csi(strconv.Itoa(i+1), 'G')
				w.str("\x1bH")
			}
		}
	}
	// The scroll region (this homes the cursor).
	if s.top != 0 || s.bottom != s.rows-1 {
		w.csi(strconv.Itoa(s.top+1)+";"+strconv.Itoa(s.bottom+1), 'r')
	}
}

// metadata writes the title, the working folder and the colors the program set.
func (w *serializer) metadata() {
	e := w.e
	if e.title != "" {
		w.str("\x1b]2;" + e.title + "\x1b\\")
	}
	if e.cwd != "" {
		u := url.URL{Scheme: "file", Host: e.cwdHost, Path: e.cwd}
		w.str("\x1b]7;" + u.String() + "\x1b\\")
	}
	for i := range e.palette {
		if e.palette[i] != e.defPalette[i] {
			w.str("\x1b]4;" + strconv.Itoa(i) + ";")
			w.out = appendColorSpec(w.out, e.palette[i])
			w.str("\x1b\\")
		}
	}
	for i, c := range [3][2]uint32{{e.fg, e.defFg}, {e.bg, e.defBg}, {e.cur, e.defCur}} {
		if c[0] != c[1] {
			w.str("\x1b]" + strconv.Itoa(10+i) + ";")
			w.out = appendColorSpec(w.out, c[0])
			w.str("\x1b\\")
		}
	}
}

// cursor writes the position of the cursor (relative to the top margin in origin mode, which cannot place it outside the
// region: there it goes to the nearest margin).
func (w *serializer) cursor(s *screen) {
	abs, y := s.y, s.y
	if w.e.modes.Origin {
		abs = min(max(abs, s.top), s.bottom)
		y = abs - s.top
	}
	if s.wrapNext {
		w.restorePendingWrap(s, abs, y)
		return
	}
	w.cup(y, s.x)
}
