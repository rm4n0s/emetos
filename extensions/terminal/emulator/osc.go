package emulator

import (
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxTitle is the longest title or icon name that is kept, and maxURL the longest hyperlink (ctlseqs does not give a limit;
// browsers accept 2083 bytes).
const (
	maxTitle = 4096
	maxURL   = 2083
)

// startOSC begins an operating system command (ECMA-48 8.3.89; dec_ansi_parser: osc_start).
func (e *Emulator) startOSC() {
	e.osc = e.osc[:0]
	e.oscBad = false
	e.oscBEL = false
	e.state = stOSC
}

func (e *Emulator) oscPut(b byte) {
	if len(e.osc) >= MaxOSCLength {
		e.oscBad = true
		return
	}
	e.osc = append(e.osc, b)
}

// oscEnd runs the OSC string that was collected (dec_ansi_parser: osc_end; ctlseqs "Operating System Commands").
func (e *Emulator) oscEnd() {
	if e.oscBad {
		return
	}
	data := string(e.osc)
	code, rest, hasRest := strings.Cut(data, ";")
	n, err := strconv.Atoi(code)
	if err != nil || n < 0 {
		return
	}
	if !hasRest {
		rest = ""
	}
	e.changed = true
	switch n {
	case 0: // set icon name and window title
		e.setTitle(rest)
		e.setIcon(rest)
	case 1: // icon name
		e.setIcon(rest)
	case 2: // window title
		e.setTitle(rest)
	case 4: // change or query a palette color
		e.oscPalette(rest)
	case 7: // the working folder (the shell integration of xterm and VTE: a file URL)
		e.oscCwd(rest)
	case 8: // hyperlink (the specification by the authors of VTE, adopted by ctlseqs)
		e.oscHyperlink(rest)
	case 10, 11, 12: // default foreground, background and cursor colors
		e.oscDynamicColors(n, rest)
	case 52: // clipboard
		sel, data, _ := strings.Cut(rest, ";")
		e.events = append(e.events, event{kind: evClipboard, s: sel, t: data})
	case 104: // reset palette colors
		e.oscResetPalette(rest)
	case 110, 111, 112: // reset the dynamic colors
		switch n {
		case 110:
			e.fg = e.defFg
		case 111:
			e.bg = e.defBg
		case 112:
			e.cur = e.defCur
		}
		e.colorsChanged()
	default:
		s := e.scr
		e.events = append(e.events, event{kind: evOSC, osc: OSCEvent{
			Code: n, Data: rest, Line: s.absLine(s.y), Col: s.x, Alt: e.scr.alt,
		}})
	}
}

func (e *Emulator) colorsChanged() {
	e.paletteGen++
	e.allDirty = true
	e.events = append(e.events, event{kind: evPalette})
}

func sanitizeTitle(s string) string {
	if len(s) > maxTitle {
		s = s[:maxTitle]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

func (e *Emulator) setTitle(s string) {
	e.title = sanitizeTitle(s)
	e.events = append(e.events, event{kind: evTitle, s: e.title})
}

func (e *Emulator) setIcon(s string) {
	e.iconName = sanitizeTitle(s)
	e.events = append(e.events, event{kind: evIcon, s: e.iconName})
}

// Title is the last window title the program set.
func (e *Emulator) Title() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.title
}

// oscCwd handles OSC 7;file://host/path.
func (e *Emulator) oscCwd(s string) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "file" {
		return
	}
	e.cwdHost, e.cwd = u.Hostname(), u.Path
	e.events = append(e.events, event{kind: evCwd, s: e.cwdHost, t: e.cwd})
}

// WorkingFolder is the last folder the shell reported with OSC 7 (empty if none).
func (e *Emulator) WorkingFolder() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cwd
}

// oscHyperlink handles OSC 8 ; params ; URI. An empty URI ends the link.
func (e *Emulator) oscHyperlink(s string) {
	params, uri, ok := strings.Cut(s, ";")
	if !ok {
		return
	}
	if uri == "" {
		e.pen.Link = 0
		e.penOK = false
		return
	}
	if len(uri) > maxURL {
		e.pen.Link = 0
		e.penOK = false
		return
	}
	var id string
	for _, kv := range strings.Split(params, ":") {
		if v, found := strings.CutPrefix(kv, "id="); found {
			id = v
		}
	}
	l := Link{ID: id, URL: uri}
	idx, found := e.st.lookupLink(l)
	if !found && (e.st.refused == 0 || e.st.refused >= compactAfter) {
		e.compactStyles()
		idx, found = e.st.lookupLink(l)
		if !found {
			e.st.refused = 1
		}
	}
	if !found {
		e.st.refused++
		idx = 0
	}
	e.pen.Link = idx
	e.penOK, e.bceOK = false, false
}

// parseColor reads an X color specification (ctlseqs "Color Names": rgb:r/g/b with 1 to 4 hex digits each, and #rgb forms).
func parseColor(s string) (rgb uint32, ok bool) {
	if v, found := strings.CutPrefix(s, "rgb:"); found {
		parts := strings.Split(v, "/")
		if len(parts) != 3 {
			return 0, false
		}
		var c [3]uint32
		for i, p := range parts {
			if len(p) == 0 || len(p) > 4 {
				return 0, false
			}
			x, err := strconv.ParseUint(p, 16, 16)
			if err != nil {
				return 0, false
			}
			// Scale a value of 1 to 4 digits to 8 bits.
			switch len(p) {
			case 1:
				x = x * 0xff / 0xf
			case 2:
			case 3:
				x = x * 0xff / 0xfff
			case 4:
				x = x * 0xff / 0xffff
			}
			c[i] = uint32(x)
		}
		return c[0]<<16 | c[1]<<8 | c[2], true
	}
	if v, found := strings.CutPrefix(s, "#"); found {
		if len(v)%3 != 0 || len(v) == 0 || len(v) > 12 {
			return 0, false
		}
		d := len(v) / 3
		var c [3]uint32
		for i := 0; i < 3; i++ {
			x, err := strconv.ParseUint(v[i*d:(i+1)*d], 16, 16)
			if err != nil {
				return 0, false
			}
			switch d {
			case 1:
				x = x * 0xff / 0xf
			case 3:
				x = x * 0xff / 0xfff
			case 4:
				x = x * 0xff / 0xffff
			}
			c[i] = uint32(x)
		}
		return c[0]<<16 | c[1]<<8 | c[2], true
	}
	return 0, false
}

// appendColorSpec writes rgb as "rgb:rrrr/gggg/bbbb" (ctlseqs: the reply of a color query).
func appendColorSpec(dst []byte, rgb uint32) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, "rgb:"...)
	for i, shift := 0, 16; i < 3; i, shift = i+1, shift-8 {
		v := (rgb >> uint(shift)) & 0xff
		if i > 0 {
			dst = append(dst, '/')
		}
		// A byte v is sent as the 16-bit value v*257, which is the byte repeated.
		dst = append(dst, hex[v>>4], hex[v&15], hex[v>>4], hex[v&15])
	}
	return dst
}

func (e *Emulator) oscTerminator() string {
	if e.oscBEL {
		return "\a"
	}
	return "\x1b\\"
}

// oscPalette handles OSC 4 ; c ; spec ; c ; spec ... ("?" asks for the color).
func (e *Emulator) oscPalette(s string) {
	parts := strings.Split(s, ";")
	changed := false
	for i := 0; i+1 < len(parts); i += 2 {
		c, err := strconv.Atoi(parts[i])
		if err != nil || c < 0 || c > 255 {
			continue
		}
		if parts[i+1] == "?" {
			e.reply = append(e.reply, "\x1b]4;"...)
			e.reply = strconv.AppendInt(e.reply, int64(c), 10)
			e.reply = append(e.reply, ';')
			e.reply = appendColorSpec(e.reply, e.palette[c])
			e.reply = append(e.reply, e.oscTerminator()...)
			continue
		}
		if rgb, ok := parseColor(parts[i+1]); ok {
			e.palette[c] = rgb
			changed = true
		}
	}
	if changed {
		e.colorsChanged()
	}
}

// oscResetPalette handles OSC 104 (ctlseqs: "reset color number c"; without arguments, all).
func (e *Emulator) oscResetPalette(s string) {
	if s == "" {
		e.palette = e.defPalette
	} else {
		for _, p := range strings.Split(s, ";") {
			if c, err := strconv.Atoi(p); err == nil && c >= 0 && c <= 255 {
				e.palette[c] = e.defPalette[c]
			}
		}
	}
	e.colorsChanged()
}

// oscDynamicColors handles OSC 10, 11 and 12 (and, as ctlseqs says, the further values go to the next codes).
func (e *Emulator) oscDynamicColors(code int, s string) {
	changed := false
	for k, spec := range strings.Split(s, ";") {
		c := code + k
		if c > 12 {
			break
		}
		var target *uint32
		switch c {
		case 10:
			target = &e.fg
		case 11:
			target = &e.bg
		case 12:
			target = &e.cur
		}
		if spec == "?" {
			e.reply = append(e.reply, "\x1b]"...)
			e.reply = strconv.AppendInt(e.reply, int64(c), 10)
			e.reply = append(e.reply, ';')
			e.reply = appendColorSpec(e.reply, *target)
			e.reply = append(e.reply, e.oscTerminator()...)
			continue
		}
		if rgb, ok := parseColor(spec); ok {
			*target = rgb
			changed = true
		}
	}
	if changed {
		e.colorsChanged()
	}
}

// initPalette sets the 256 colors to xterm's defaults (the 16 of its resources, the 6x6x6 cube with the levels 0, 95, 135,
// 175, 215, 255 and the 24 grays 8 + 10i; ctlseqs "CSI Pm m" and the 256colres script) unless the host chose others.
func (e *Emulator) initPalette() {
	base := [16]uint32{
		0x000000, 0xcd0000, 0x00cd00, 0xcdcd00, 0x0000ee, 0xcd00cd, 0x00cdcd, 0xe5e5e5,
		0x7f7f7f, 0xff0000, 0x00ff00, 0xffff00, 0x5c5cff, 0xff00ff, 0x00ffff, 0xffffff,
	}
	if e.themeSet {
		// The host set a theme: keep it.
		e.palette = e.defPalette
		e.fg, e.bg, e.cur = e.defFg, e.defBg, e.defCur
		return
	}
	copy(e.defPalette[:16], base[:])
	levels := [6]uint32{0, 95, 135, 175, 215, 255}
	for i := 0; i < 216; i++ {
		e.defPalette[16+i] = levels[i/36]<<16 | levels[(i/6)%6]<<8 | levels[i%6]
	}
	for i := 0; i < 24; i++ {
		v := uint32(8 + 10*i)
		e.defPalette[232+i] = v<<16 | v<<8 | v
	}
	e.defFg, e.defBg, e.defCur = 0xe5e5e5, 0x000000, 0xe5e5e5
	e.palette = e.defPalette
	e.fg, e.bg, e.cur = e.defFg, e.defBg, e.defCur
}

// SetTheme sets the 16 ANSI colors and the default foreground, background and cursor colors the terminal shows and reports
// to the program (OSC 4, 10, 11, 12 queries). The program's own changes (OSC 4 and the others) are reset.
func (e *Emulator) SetTheme(ansi [16]uint32, fg, bg, cursor uint32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.themeSet = true
	copy(e.defPalette[:16], ansi[:])
	e.defFg, e.defBg, e.defCur = fg, bg, cursor
	e.palette = e.defPalette
	e.fg, e.bg, e.cur = fg, bg, cursor
	e.paletteGen++
	e.allDirty, e.changed = true, true
}

// dcsHook starts a device control string (dec_ansi_parser: hook): only DECRQSS is understood (VT510 DECRQSS).
func (e *Emulator) dcsHook(f byte) {
	e.dcsKind = dcsNone
	e.dcsBuf = e.dcsBuf[:0]
	if f == 'q' && e.hasInter('$') && e.marker == 0 {
		e.dcsKind = dcsRQSS
	}
}

// dcsUnhook ends a device control string and answers DECRQSS (ctlseqs: DCS $ q Pt ST): `DCS 1 $ r Pt ST` for a setting it
// knows (SGR "m", DECSTBM "r", DECSCUSR " q"), `DCS 0 $ r ST` otherwise.
func (e *Emulator) dcsUnhook() {
	if e.dcsKind != dcsRQSS {
		return
	}
	s := e.scr
	var body string
	switch string(e.dcsBuf) {
	case "m":
		body = e.sgrString() + "m"
	case "r":
		body = strconv.Itoa(s.top+1) + ";" + strconv.Itoa(s.bottom+1) + "r"
	case " q":
		n := 2 * int(e.modes.CursorShape)
		if e.modes.CursorBlink {
			n++
		} else {
			n += 2
		}
		body = strconv.Itoa(n) + " q"
	default:
		e.reply = append(e.reply, "\x1bP0$r\x1b\\"...)
		return
	}
	e.reply = append(e.reply, "\x1bP1$r"...)
	e.reply = append(e.reply, body...)
	e.reply = append(e.reply, "\x1b\\"...)
}

// sgrString writes the pen as the parameters of an SGR sequence that sets it (always starting with 0).
func (e *Emulator) sgrString() string { return string(appendSGR(nil, e.pen)) }

// appendSGR appends the parameters of an SGR sequence that sets the style p from the default rendition (they start with 0).
func appendSGR(b []byte, p Style) []byte {
	b = append(b, '0')
	add := func(s string) { b = append(b, ';'); b = append(b, s...) }
	if p.Attrs&AttrBold != 0 {
		add("1")
	}
	if p.Attrs&AttrDim != 0 {
		add("2")
	}
	if p.Attrs&AttrItalic != 0 {
		add("3")
	}
	switch p.Underline {
	case UnderlineSingle:
		add("4")
	case UnderlineDouble:
		add("4:2")
	case UnderlineCurly:
		add("4:3")
	case UnderlineDotted:
		add("4:4")
	case UnderlineDashed:
		add("4:5")
	}
	if p.Attrs&AttrBlink != 0 {
		add("5")
	}
	if p.Attrs&AttrInverse != 0 {
		add("7")
	}
	if p.Attrs&AttrInvisible != 0 {
		add("8")
	}
	if p.Attrs&AttrStrike != 0 {
		add("9")
	}
	if p.Attrs&AttrOverline != 0 {
		add("53")
	}
	b = appendColorSGR(b, p.Fg, 30, 90, 38)
	b = appendColorSGR(b, p.Bg, 40, 100, 48)
	b = appendColorSGR(b, p.UnderlineColor, 0, 0, 58)
	return b
}

// appendColorSGR appends the SGR parameters that select c: base (30 or 40) and bright (90 or 100) for the first 16 colors
// of foreground and background, the 38/48/58 forms otherwise.
func appendColorSGR(b []byte, c Color, base, bright, ext int) []byte {
	switch c.Kind() {
	case ColorIndexed:
		i := int(c.Index())
		switch {
		case base != 0 && i < 8:
			b = append(b, ';')
			b = strconv.AppendInt(b, int64(base+i), 10)
		case base != 0 && i < 16:
			b = append(b, ';')
			b = strconv.AppendInt(b, int64(bright+i-8), 10)
		default:
			b = append(b, ';')
			b = strconv.AppendInt(b, int64(ext), 10)
			b = append(b, ";5;"...)
			b = strconv.AppendInt(b, int64(i), 10)
		}
	case ColorRGB:
		r, g, bl := c.RGB()
		b = append(b, ';')
		b = strconv.AppendInt(b, int64(ext), 10)
		b = append(b, ";2;"...)
		b = strconv.AppendInt(b, int64(r), 10)
		b = append(b, ';')
		b = strconv.AppendInt(b, int64(g), 10)
		b = append(b, ';')
		b = strconv.AppendInt(b, int64(bl), 10)
	}
	return b
}
