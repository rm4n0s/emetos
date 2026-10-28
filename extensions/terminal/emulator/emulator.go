package emulator

import (
	"sync"
	"unicode/utf8"
)

// Limits of the parser (SPEC §10.25 "Memory").
const (
	// MaxParams is the number of parameters of a sequence that are kept; more are read and dropped.
	MaxParams = 32
	// MaxOSCLength is the longest OSC string in bytes; a longer one is ignored when it ends.
	MaxOSCLength = 1 << 16
	// maxParamValue is the largest value of one parameter; a larger one is held at this value.
	maxParamValue = 1 << 20
)

// Options set up a new Emulator.
type Options struct {
	// Scrollback is the number of lines kept above the screen (terminal.integrated.scrollback; 0 keeps none).
	Scrollback int
	// Callbacks receive what the program asks of the terminal.
	Callbacks Callbacks
	// Name and Version answer the program's XTVERSION query (ctlseqs: CSI > Ps q); empty is "Emetos".
	Name, Version string
}

// Emulator is a terminal: a parser for the program's output, the two buffers (main with its scrollback, and alternate), the
// modes the program set, and the encoders of the user's keys and mouse. All its methods may be called from any goroutine
// (they take one lock), but Write and Resize are meant for the goroutine that owns the terminal and Snapshot for the UI
// thread.
type Emulator struct {
	mu sync.Mutex
	cb Callbacks

	rows, cols int
	scrollback int
	mainScr    *screen
	altScr     *screen
	scr        *screen
	st         *styleTable

	// pen is the current graphic rendition (SGR); penIdx is its index in the table, valid when penOK.
	pen    Style
	penIdx uint16
	penOK  bool
	// bceIdx is the style that erased cells get (the pen's background), valid when bceOK.
	bceIdx uint16
	bceOK  bool

	modes     Modes
	savedMode map[int]bool

	// charsets and gl are the designated character sets G0..G3 and the one in GL (VT510: ESC ( ) * + and SO/SI).
	charsets [4]charset
	gl       int
	// slow is true when the ASCII fast path cannot be used (insert mode or a character set that translates).
	slow bool

	lastRune rune

	// parser state (vt100.net/emu/dec_ansi_parser).
	state    parserState
	params   [MaxParams]int32
	nparams  int
	colons   uint32
	curParam int32
	haveCur  bool
	marker   byte
	inter    [2]byte
	ninter   int
	osc      []byte
	oscBad   bool
	oscBEL   bool
	dcsKind  dcsKind
	dcsBuf   []byte
	u8       [4]byte
	u8n      int
	u8need   int

	// palette and colors (OSC 4, 10, 11, 12).
	palette     [256]uint32
	defPalette  [256]uint32
	fg, bg, cur uint32
	defFg       uint32
	defBg       uint32
	defCur      uint32
	paletteGen  uint32
	themeSet    bool

	title, iconName string
	cwdHost, cwd    string

	name, version string

	// output of this Write: bytes for the program and events for the callbacks.
	reply  []byte
	events []event

	// change tracking for Snapshot.
	allDirty bool
	changed  bool
	// mouseX, mouseY is the cell of the last mouse move that EncodeMouse reported (mouseSeen says there is one), so that a
	// move inside the same cell is not reported again.
	mouseX, mouseY int
	mouseSeen      bool
}

// New makes an emulator of rows by cols cells.
func New(rows, cols int, opts Options) *Emulator {
	rows, cols = clampSize(rows, cols)
	sb := opts.Scrollback
	if sb < 0 {
		sb = 0
	}
	if sb > MaxScrollback {
		sb = MaxScrollback
	}
	e := &Emulator{cb: opts.Callbacks, rows: rows, cols: cols, scrollback: sb, st: newStyleTable(), name: opts.Name, version: opts.Version}
	if e.name == "" {
		e.name = "Emetos"
	}
	e.initPalette()
	e.mainScr = newScreen(rows, cols, sb, false)
	e.altScr = newScreen(rows, cols, 0, true)
	e.scr = e.mainScr
	e.resetModes()
	e.allDirty, e.changed = true, true
	return e
}

func clampSize(rows, cols int) (int, int) {
	rows = min(max(rows, 1), MaxRows)
	cols = min(max(cols, 1), MaxCols)
	return rows, cols
}

func (e *Emulator) resetModes() {
	e.modes = Modes{Autowrap: true, CursorVisible: true, MetaSendsEscape: true}
	e.charsets = [4]charset{}
	e.gl = 0
	e.slow = false
	e.savedMode = nil
}

// Size is the size of the terminal in rows and columns.
func (e *Emulator) Size() (rows, cols int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rows, e.cols
}

// Write feeds the program's output to the terminal (it implements io.Writer and never fails). The callbacks run, on the
// calling goroutine, after the terminal is unlocked.
func (e *Emulator) Write(p []byte) (int, error) {
	e.mu.Lock()
	e.parse(p)
	reply, events := e.takeOutput()
	e.mu.Unlock()
	e.deliver(reply, events)
	return len(p), nil
}

// parse runs the parser over p (the lock is held).
func (e *Emulator) parse(p []byte) {
	i := 0
	for i < len(p) {
		if e.state == stGround && e.u8n == 0 && !e.slow {
			if n := e.printText(p[i:]); n > 0 {
				i += n
				continue
			}
		}
		b := p[i]
		if b < 0x80 {
			if e.u8n > 0 {
				// A multi-byte sequence was cut by an ASCII byte: it is invalid.
				e.u8n = 0
				e.feedRune(utf8.RuneError)
			}
			e.feedByte(b)
			i++
			continue
		}
		i = e.feedUTF8(p, i)
	}
}

// feedUTF8 handles the non-ASCII byte p[i] (or the rest of a sequence cut by the end of the previous chunk) and returns the
// index of the next byte to read. The Unicode standard (section 3.9, table 3-7) says what is valid; the maximal invalid
// prefix of a sequence is replaced by one U+FFFD.
func (e *Emulator) feedUTF8(p []byte, i int) int {
	if e.u8n > 0 {
		for i < len(p) && e.u8n < e.u8need {
			b := p[i]
			if !utf8Continues(e.u8[0], e.u8n, b) {
				e.u8n = 0
				e.feedRune(utf8.RuneError)
				return i
			}
			e.u8[e.u8n] = b
			e.u8n++
			i++
		}
		if e.u8n < e.u8need {
			return i
		}
		r, _ := utf8.DecodeRune(e.u8[:e.u8need])
		e.u8n = 0
		e.feedRune(r)
		return i
	}
	b := p[i]
	need := utf8Len(b)
	if need == 0 {
		e.feedRune(utf8.RuneError)
		return i + 1
	}
	if i+need <= len(p) {
		r, sz := utf8.DecodeRune(p[i : i+need])
		if r == utf8.RuneError && sz == 1 {
			e.feedRune(utf8.RuneError)
			return i + maximalPrefix(p[i:i+need])
		}
		e.feedRune(r)
		return i + sz
	}
	// The sequence goes on in the next chunk, unless what is here is already invalid.
	rest := p[i:]
	if k := maximalPrefix(rest); k < len(rest) {
		e.feedRune(utf8.RuneError)
		return i + k
	}
	e.u8n = copy(e.u8[:], rest)
	e.u8need = need
	return len(p)
}

// utf8Len is the length of the sequence that the lead byte b starts, or 0 when b cannot start one.
func utf8Len(b byte) int {
	switch {
	case b >= 0xc2 && b < 0xe0:
		return 2
	case b >= 0xe0 && b < 0xf0:
		return 3
	case b >= 0xf0 && b < 0xf5:
		return 4
	}
	return 0
}

// utf8Continues reports whether b may be byte number pos (1-based from the lead) of a sequence that starts with lead
// (Unicode table 3-7: the second byte of E0, ED, F0 and F4 has a narrower range).
func utf8Continues(lead byte, pos int, b byte) bool {
	lo, hi := byte(0x80), byte(0xbf)
	if pos == 1 {
		switch lead {
		case 0xe0:
			lo = 0xa0
		case 0xed:
			hi = 0x9f
		case 0xf0:
			lo = 0x90
		case 0xf4:
			hi = 0x8f
		}
	}
	return b >= lo && b <= hi
}

// maximalPrefix is the number of bytes at the start of b (which begins with a lead byte) that are the start of a valid
// sequence, at least 1; it is len(b) when the whole of b is a valid prefix.
func maximalPrefix(b []byte) int {
	k := 1
	for k < len(b) && k < utf8Len(b[0]) && utf8Continues(b[0], k, b[k]) {
		k++
	}
	return k
}

// Resize changes the size of the terminal (ctlseqs: the window is resized by the user; the program learns it from
// SIGWINCH). The main buffer reflows its wrapped lines; the alternate buffer is cut or extended.
func (e *Emulator) Resize(rows, cols int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rows, cols = clampSize(rows, cols)
	if rows == e.rows && cols == e.cols {
		return
	}
	e.resize(rows, cols)
	e.allDirty, e.changed = true, true
}

// SetScrollback changes the number of lines of scrollback kept; lines beyond it are dropped from the top.
func (e *Emulator) SetScrollback(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	n = min(max(n, 0), MaxScrollback)
	e.scrollback = n
	e.mainScr.ring.setMax(e.rows + n)
	e.changed = true
}

// Scrollback is the number of lines above the screen of the main buffer.
func (e *Emulator) Scrollback() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.mainScr.scrollbackLen()
}

// Reset is a full reset (ctlseqs: RIS, ESC c): modes, character sets, colors, tab stops and the screens are set back to
// their start values. The size and the scrollback are kept.
func (e *Emulator) Reset() {
	e.mu.Lock()
	e.fullReset()
	reply, events := e.takeOutput()
	e.mu.Unlock()
	e.deliver(reply, events)
}

// fullReset is RIS (VT510 RIS; ctlseqs: ESC c). The scrollback is kept.
func (e *Emulator) fullReset() {
	e.scr = e.mainScr
	for _, s := range []*screen{e.mainScr, e.altScr} {
		for y := 0; y < s.rows; y++ {
			clearLine(s.row(y), 0)
		}
		s.x, s.y, s.wrapNext = 0, 0, false
		s.top, s.bottom = 0, s.rows-1
		s.tabs = defaultTabs(s.cols)
		s.saved = savedCursor{}
	}
	e.pen = Style{}
	e.penOK, e.bceOK = false, false
	e.resetModes()
	e.state = stGround
	e.initPalette()
	e.paletteGen++
	e.title, e.iconName = "", ""
	e.allDirty, e.changed = true, true
}

// resolveBlank returns the index of the style that erased cells get: the default style with the background of the pen
// (ctlseqs: "background color erase", the BCE capability of xterm-256color).
func (e *Emulator) bce() uint16 {
	if !e.bceOK {
		if e.pen.Bg == 0 {
			e.bceIdx = 0
		} else {
			e.bceIdx = e.intern(Style{Bg: e.pen.Bg})
		}
		e.bceOK = true
	}
	return e.bceIdx
}

// curStyle returns the index of the pen's style in the table.
func (e *Emulator) curStyle() uint16 {
	if !e.penOK {
		e.penIdx = e.intern(e.pen)
		e.penOK = true
	}
	return e.penIdx
}

// compactAfter is how many styles that do not fit may be refused between two compactions: a table that stays full of styles
// that cells still use is not rebuilt for every new style.
const compactAfter = MaxStyles

// intern returns the index of s. When the table is full the styles no cell uses are dropped first; when that does not make
// room, the colors of s are rounded to the 256-color palette, and as a last resort the default style is used.
func (e *Emulator) intern(s Style) uint16 {
	if s == (Style{}) {
		return 0
	}
	if i, ok := e.st.lookup(s); ok {
		return i
	}
	if e.st.refused == 0 || e.st.refused >= compactAfter {
		e.compactStyles()
		if i, ok := e.st.lookup(s); ok {
			return i
		}
		e.st.refused = 1
	}
	e.st.refused++
	if i, ok := e.st.lookup(quantize(s)); ok {
		return i
	}
	return 0
}

// compactStyles rebuilds the tables of styles and links with the entries the cells use.
func (e *Emulator) compactStyles() {
	old := e.st
	used := make([]bool, len(old.styles))
	for _, s := range []*screen{e.mainScr, e.altScr} {
		for i := 0; i < s.ring.n; i++ {
			for _, c := range s.ring.at(i).cells {
				used[c.Style] = true
			}
		}
	}
	live := 0
	for _, u := range used {
		if u {
			live++
		}
	}
	nt := &styleTable{gen: old.gen + 1}
	nt.reset(live)
	remap := make([]uint16, len(old.styles))
	for i, u := range used {
		if !u || i == 0 {
			continue
		}
		s := old.styles[i]
		if s.Link != 0 {
			nl, ok := nt.lookupLink(old.links[s.Link])
			if !ok {
				nl = 0
			}
			s.Link = nl
		}
		ni, ok := nt.lookup(s)
		if !ok {
			ni = 0
		}
		remap[i] = ni
	}
	for _, s := range []*screen{e.mainScr, e.altScr} {
		for i := 0; i < s.ring.n; i++ {
			l := s.ring.at(i)
			for k := range l.cells {
				l.cells[k].Style = remap[l.cells[k].Style]
			}
		}
	}
	// The pen keeps its link by value: it is looked up again in the new table.
	if e.pen.Link != 0 {
		l := old.links[e.pen.Link]
		nl, _ := nt.lookupLink(l)
		e.pen.Link = nl
	}
	*e.st = *nt
	e.penOK, e.bceOK = false, false
	e.allDirty, e.changed = true, true
}

// takeOutput hands over the reply bytes and the events of this Write (the lock is held).
func (e *Emulator) takeOutput() ([]byte, []event) {
	var reply []byte
	var events []event
	if len(e.reply) > 0 {
		reply = append([]byte(nil), e.reply...)
		e.reply = e.reply[:0]
	}
	if len(e.events) > 0 {
		events = e.events
		e.events = nil
	}
	return reply, events
}
