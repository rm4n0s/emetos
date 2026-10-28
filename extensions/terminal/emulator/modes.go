package emulator

// MouseMode is which mouse events the program asked for (ctlseqs: "Mouse Tracking", DECSET 9, 1000, 1002, 1003).
type MouseMode uint8

// The mouse tracking modes.
const (
	MouseNone   MouseMode = iota
	MouseX10              // 9: button presses only
	MouseNormal           // 1000: presses and releases
	MouseDrag             // 1002: and motion while a button is down
	MouseAny              // 1003: and all motion
)

// MouseEncoding is how mouse events are written (ctlseqs: DECSET 1005, 1006, 1015).
type MouseEncoding uint8

// The mouse encodings.
const (
	MouseEncDefault MouseEncoding = iota // CSI M Cb Cx Cy, each a byte plus 32
	MouseEncUTF8                         // 1005
	MouseEncSGR                          // 1006: CSI < Cb ; Cx ; Cy M or m
	MouseEncURXVT                        // 1015: CSI Cb ; Cx ; Cy M
)

// CursorShape is the shape DECSCUSR chose.
type CursorShape uint8

// The cursor shapes (ctlseqs: CSI Ps SP q).
const (
	CursorBlock CursorShape = iota
	CursorUnderline
	CursorBar
)

// Modes are the modes the program set (and the ones that decide how keys and the mouse are sent).
type Modes struct {
	// AppCursorKeys is DECCKM (DECSET 1) and AppKeypad DECKPAM (ESC =).
	AppCursorKeys bool
	AppKeypad     bool
	// Autowrap is DECAWM (DECSET 7, on at the start); Origin is DECOM (DECSET 6); Insert is IRM (SM 4); NewLine is LNM
	// (SM 20: LF also does CR, and Enter sends CR LF).
	Autowrap bool
	Origin   bool
	Insert   bool
	NewLine  bool
	// CursorVisible is DECTCEM (DECSET 25); CursorBlink is att610 (DECSET 12) and DECSCUSR.
	CursorVisible bool
	CursorBlink   bool
	CursorShape   CursorShape
	// ReverseVideo is DECSCNM (DECSET 5): the whole screen is drawn with foreground and background swapped.
	ReverseVideo bool
	// BracketedPaste is DECSET 2004, FocusEvents DECSET 1004, SyncOutput DECSET 2026 (the UI keeps its last picture while it
	// is set), AltScroll DECSET 1007, ReverseWrap DECSET 45, MetaSendsEscape DECSET 1036 (on at the start).
	BracketedPaste  bool
	FocusEvents     bool
	SyncOutput      bool
	AltScroll       bool
	ReverseWrap     bool
	MetaSendsEscape bool
	// AltScreen says that the alternate screen is shown (DECSET 47, 1047, 1049).
	AltScreen bool
	// Mouse and MouseEnc are the tracking mode and encoding.
	Mouse    MouseMode
	MouseEnc MouseEncoding
}

type charset uint8

const (
	csASCII charset = iota
	csDECGraphics
	csUK
)

// decGraphics maps the bytes 0x2b..0x7e to the characters of the DEC Special Graphics set: 0x5f to 0x7e are the VT100's (user
// guide, table 3-9; ctlseqs: "ESC ( 0"), and 0x2b to 0x2e and 0x30 the arrows and the block that the alternate character
// set of the xterm-256color terminfo entry (acsc) lists. A byte with no character here is printed as it is.
var decGraphics = func() (t [0x7e - 0x2b + 1]rune) {
	for r, c := range map[rune]rune{
		'+': '→', ',': '←', '-': '↑', '.': '↓', '0': '▮',
		0x5f: ' ', '`': '◆', 'a': '▒', 'b': '␉', 'c': '␌', 'd': '␍', 'e': '␊', 'f': '°', 'g': '±', 'h': '␤', 'i': '␋',
		'j': '┘', 'k': '┐', 'l': '┌', 'm': '└', 'n': '┼', 'o': '⎺', 'p': '⎻', 'q': '─', 'r': '⎼', 's': '⎽', 't': '├',
		'u': '┤', 'v': '┴', 'w': '┬', 'x': '│', 'y': '≤', 'z': '≥', '{': 'π', '|': '≠', '}': '£', '~': '·',
	} {
		t[r-0x2b] = c
	}
	return t
}()

// Modes returns a copy of the modes.
func (e *Emulator) Modes() Modes {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.modes
}
