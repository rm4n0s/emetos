package kit

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// The ANSI text of the Debug Console (SPEC §10.31): the parser of the escape sequences of a program's output. It is plain Go (no widget)
// so that it is tested as a table; AnsiText (ansitext.go) draws what it makes. The Testing views' output uses the same.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugANSIHandling.ts (handleANSIOutput, calcANSI8bitColor)

// AnsiColorKind says how an AnsiColor is given.
type AnsiColorKind int

const (
	// AnsiColorNone is no color: the default of the text.
	AnsiColorNone AnsiColorKind = iota
	// AnsiColorTheme is one of the 16 colors of the theme (terminal.ansiBlack ... terminal.ansiBrightWhite), AnsiColor.Index.
	AnsiColorTheme
	// AnsiColorRGB is a color of 8 or 24 bits, given by its components.
	AnsiColorRGB
)

// AnsiColor is the color of a part of the text.
type AnsiColor struct {
	Kind    AnsiColorKind
	Index   int
	R, G, B uint8
}

// AnsiStyle is how a part of the text looks, as the SGR sequences before it set it.
type AnsiStyle struct {
	// Fg, Bg and UnderlineColor are the colors of the text, behind it and of its underline.
	Fg, Bg, UnderlineColor AnsiColor
	Bold, Dim, Italic      bool
	// Underline and DoubleUnderline exclude each other.
	Underline, DoubleUnderline bool
	Blink, RapidBlink          bool
	Hidden, StrikeThrough      bool
	Overline                   bool
	// Superscript and Subscript exclude each other.
	Superscript, Subscript bool
	// Font is the alternate font 1 to 10 (SGR 11 to 20), 0 for the normal one.
	Font int
}

// AnsiSpan is a run of text with one style.
type AnsiSpan struct {
	// Text has no escape sequence in it.
	Text  string
	Style AnsiStyle
	// Start is where the text starts in the text of all the spans put together (the original text without its sequences), in bytes.
	Start int
}

// sgrSequence is the form of a sequence that sets graphics: anything else that ends with a known final character is hidden.
var sgrSequence = regexp.MustCompile(`^(?:[34][0-8]|9[0-7]|10[0-7]|[0-9]|2[1-5,7-9]|[34]9|5[8,9]|1[0-9])(?:;[349][0-7]|10[0-7]|[013]|[245]|[34]9)?(?:;[012]?[0-9]?[0-9])*;?m$`)

// ansiFinal tells whether a character ends an escape sequence that VS Code knows.
func ansiFinal(c byte) bool {
	switch c {
	case 'A', 'B', 'C', 'D', 'H', 'I', 'J', 'K', 'f', 'h', 'm', 'p', 's', 'u':
		return true
	}
	return false
}

// ansiParser holds the style while the text is read.
type ansiParser struct {
	style    AnsiStyle
	inverted bool
}

// ParseANSI splits a text with escape sequences into spans of one style each. A sequence that sets graphics changes the style of what
// follows; a known sequence of another kind is dropped; an escape character that starts no complete sequence stays in the text.
func ParseANSI(text string) []AnsiSpan {
	var (
		spans    []AnsiSpan
		p        ansiParser
		buf      strings.Builder
		pos      int
		printed  int // the length of the text of the spans so far
		bufStart int
	)
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		spans = append(spans, AnsiSpan{Text: buf.String(), Style: p.style, Start: bufStart})
		printed += buf.Len()
		buf.Reset()
	}
	for pos < len(text) {
		if text[pos] == 27 && pos+1 < len(text) && text[pos+1] == '[' {
			start := pos
			pos += 2
			seqStart := pos
			found := false
			for pos < len(text) {
				c := text[pos]
				pos++
				if ansiFinal(c) {
					found = true
					break
				}
			}
			if found {
				flush()
				seq := text[seqStart:pos]
				if sgrSequence.MatchString(seq) {
					p.apply(seq)
				} // any other known sequence is hidden
				continue
			}
			pos = start
		}
		if buf.Len() == 0 {
			bufStart = printed
		}
		buf.WriteByte(text[pos])
		pos++
	}
	flush()
	return spans
}

// RemoveANSI is the text without its escape sequences.
func RemoveANSI(text string) string {
	if !strings.Contains(text, "\x1b") {
		return text
	}
	var b strings.Builder
	for _, s := range ParseANSI(text) {
		b.WriteString(s.Text)
	}
	return b.String()
}

// apply sets the style from the numbers of a sequence ("1;31m").
func (p *ansiParser) apply(seq string) {
	fields := strings.Split(seq[:len(seq)-1], ";")
	var codes []int
	for _, f := range fields {
		if f == "" {
			continue
		}
		n, err := strconv.Atoi(f)
		if err != nil {
			continue
		}
		codes = append(codes, n)
	}
	if len(codes) == 0 {
		return
	}
	if c := codes[0]; c == 38 || c == 48 || c == 58 {
		// an advanced color cannot be combined with other codes; one that is not valid is ignored
		slot := p.slot(c)
		if len(codes) > 1 && codes[1] == 5 {
			p.set8Bit(codes, slot)
		} else if len(codes) > 1 && codes[1] == 2 {
			p.set24Bit(codes, slot)
		}
		return
	}
	p.setBasic(codes)
}

type colorSlot int

const (
	slotForeground colorSlot = iota
	slotBackground
	slotUnderline
)

func (p *ansiParser) slot(code int) colorSlot {
	switch code {
	case 38:
		return slotForeground
	case 48:
		return slotBackground
	}
	return slotUnderline
}

func (p *ansiParser) setColor(slot colorSlot, c AnsiColor) {
	switch slot {
	case slotForeground:
		p.style.Fg = c
	case slotBackground:
		p.style.Bg = c
	default:
		p.style.UnderlineColor = c
	}
}

// setBasic sets the formatting of the codes, in order; a color replaces the one before it, a format adds to the others.
func (p *ansiParser) setBasic(codes []int) {
	s := &p.style
	for _, code := range codes {
		switch code {
		case 0:
			*s = AnsiStyle{UnderlineColor: s.UnderlineColor}
		case 1:
			s.Bold = true
		case 2:
			s.Dim = true
		case 3:
			s.Italic = true
		case 4:
			s.Underline, s.DoubleUnderline = true, false
		case 5:
			s.Blink = true
		case 6:
			s.RapidBlink = true
		case 7:
			if !p.inverted {
				p.inverted = true
				s.Fg, s.Bg = s.Bg, s.Fg
			}
		case 8:
			s.Hidden = true
		case 9:
			s.StrikeThrough = true
		case 10:
			s.Font = 0
		case 11, 12, 13, 14, 15, 16, 17, 18, 19, 20:
			s.Font = code - 10
		case 21:
			s.Underline, s.DoubleUnderline = false, true
		case 22:
			s.Bold, s.Dim = false, false
		case 23:
			s.Italic = false
			if s.Font == 10 {
				s.Font = 0
			}
		case 24:
			s.Underline, s.DoubleUnderline = false, false
		case 25:
			s.Blink, s.RapidBlink = false, false
		case 27:
			if p.inverted {
				p.inverted = false
				s.Fg, s.Bg = s.Bg, s.Fg
			}
		case 28:
			s.Hidden = false
		case 29:
			s.StrikeThrough = false
		case 53:
			s.Overline = true
		case 55:
			s.Overline = false
		case 39:
			s.Fg = AnsiColor{}
		case 49:
			s.Bg = AnsiColor{}
		case 59:
			s.UnderlineColor = AnsiColor{}
		case 73:
			s.Superscript, s.Subscript = true, false
		case 74:
			s.Superscript, s.Subscript = false, true
		case 75:
			s.Superscript, s.Subscript = false, false
		default:
			p.setBasicColor(code)
		}
	}
}

// setBasicColor sets the dark and bright colors of the theme: 30 to 37 and 90 to 97 for the text, 40 to 47 and 100 to 107 behind it.
func (p *ansiParser) setBasicColor(code int) {
	var slot colorSlot
	var index int
	switch {
	case code >= 30 && code <= 37:
		index, slot = code-30, slotForeground
	case code >= 90 && code <= 97:
		index, slot = code-90+8, slotForeground
	case code >= 40 && code <= 47:
		index, slot = code-40, slotBackground
	case code >= 100 && code <= 107:
		index, slot = code-100+8, slotBackground
	default:
		return
	}
	p.setColor(slot, AnsiColor{Kind: AnsiColorTheme, Index: index})
}

// set24Bit sets a color of 24 bits (38;2;r;g;b); one with a component outside 0 to 255, or fewer components, is ignored.
func (p *ansiParser) set24Bit(codes []int, slot colorSlot) {
	if len(codes) < 5 {
		return
	}
	for _, c := range codes[2:5] {
		if c < 0 || c > 255 {
			return
		}
	}
	p.setColor(slot, AnsiColor{Kind: AnsiColorRGB, R: uint8(codes[2]), G: uint8(codes[3]), B: uint8(codes[4])})
}

// set8Bit sets a color of 8 bits (38;5;n): 16 to 255 are colors of the cube and the grays, 0 to 15 the colors of the theme.
func (p *ansiParser) set8Bit(codes []int, slot colorSlot) {
	if len(codes) < 3 {
		return
	}
	n := codes[2]
	if c, ok := Ansi8BitColor(n); ok {
		p.setColor(slot, c)
		return
	}
	if n < 0 || n > 15 {
		return
	}
	if slot == slotUnderline {
		p.setColor(slot, AnsiColor{Kind: AnsiColorTheme, Index: n})
		return
	}
	// one of the four basic ranges (30 to 37, 90 to 97, 40 to 47, 100 to 107)
	n += 30
	if n >= 38 {
		n += 52
	}
	if slot == slotBackground {
		n += 10
	}
	p.setBasicColor(n)
}

// Ansi8BitColor is the color of a number from 16 to 255 of the 8-bit palette: the 216 colors of the cube, then 24 grays. The 16
// before them are the theme's, and a number outside the palette has no color (false).
func Ansi8BitColor(n int) (AnsiColor, bool) {
	switch {
	case n >= 16 && n <= 231:
		n -= 16
		b := n % 6
		n = (n - b) / 6
		g := n % 6
		r := (n - g) / 6
		conv := 255.0 / 5
		return AnsiColor{Kind: AnsiColorRGB, R: uint8(math.Round(float64(r) * conv)), G: uint8(math.Round(float64(g) * conv)), B: uint8(math.Round(float64(b) * conv))}, true
	case n >= 232 && n <= 255:
		level := uint8(math.Round(float64(n-232) / 23 * 255))
		return AnsiColor{Kind: AnsiColorRGB, R: level, G: level, B: level}, true
	}
	return AnsiColor{}, false
}
