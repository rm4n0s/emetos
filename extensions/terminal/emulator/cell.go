package emulator

// Cell is one cell of the screen: 8 bytes. Rune is the code point (0 is an empty cell, drawn as a space); when the
// CellCluster flag is set it is instead the index of a string in the clusters of the row (a base character with combining
// marks). Style is an index into the table of styles of the buffer (0 is the default style).
type Cell struct {
	Rune  uint32
	Style uint16
	Flags uint16
}

// The flags of a Cell.
const (
	// CellWide marks the left half of a character that takes two cells; the cell to its right is a CellTail.
	CellWide uint16 = 1 << iota
	// CellTail marks the right half of a wide character; it holds no text.
	CellTail
	// CellCluster says that Rune is an index into the clusters of the row, not a code point.
	CellCluster
	// CellWideFill marks the empty cell that ends a wrapped row because the wide character that follows did not fit in it.
	CellWideFill
)

// ColorKind says how a Color is meant.
type ColorKind uint8

// The kinds of Color.
const (
	// ColorDefault is the default foreground or background of the terminal.
	ColorDefault ColorKind = iota
	// ColorIndexed is an entry of the 256-color palette.
	ColorIndexed
	// ColorRGB is a 24-bit color.
	ColorRGB
)

// Color is a color of a style: the default color, an entry of the palette (0 to 255) or a 24-bit color. The zero value is
// the default color.
type Color uint32

// IndexedColor is the color of palette entry i.
func IndexedColor(i uint8) Color { return Color(ColorIndexed)<<24 | Color(i) }

// RGBColor is the 24-bit color r, g, b.
func RGBColor(r, g, b uint8) Color {
	return Color(ColorRGB)<<24 | Color(r)<<16 | Color(g)<<8 | Color(b)
}

// Kind says whether c is the default color, a palette entry or a 24-bit color.
func (c Color) Kind() ColorKind { return ColorKind(c >> 24) }

// Index is the palette entry of an indexed color.
func (c Color) Index() uint8 { return uint8(c) }

// RGB is the red, green and blue of a 24-bit color.
func (c Color) RGB() (r, g, b uint8) { return uint8(c >> 16), uint8(c >> 8), uint8(c) }

// Attr is a set of text attributes of a style.
type Attr uint16

// The attributes of SGR (ECMA-48 8.3.117, ctlseqs "CSI Pm m").
const (
	AttrBold Attr = 1 << iota
	AttrDim
	AttrItalic
	AttrBlink
	AttrInverse
	AttrInvisible
	AttrStrike
	AttrOverline
)

// Underline is the kind of underline of a style.
type Underline uint8

// The underlines of SGR 4 and 21 (ctlseqs: SGR 4:Ps).
const (
	UnderlineNone Underline = iota
	UnderlineSingle
	UnderlineDouble
	UnderlineCurly
	UnderlineDotted
	UnderlineDashed
)

// Style is what a cell looks like besides its text. A hyperlink (OSC 8) is part of the style: Link is an index into the
// table of links of the frame (0 is no link).
type Style struct {
	Fg, Bg, UnderlineColor Color
	Attrs                  Attr
	Underline              Underline
	Link                   uint16
}

// Link is a hyperlink of OSC 8: the target and the optional id the program gave it.
type Link struct {
	ID, URL string
}

// MaxStyles and MaxLinks are the sizes of the tables of styles and of links, which an index of 16 bits can address.
const (
	MaxStyles = 65535
	MaxLinks  = 65535
)

// styleTable is the table of styles and links shared by the cells of a buffer. Entry 0 is the default style (links: no
// link). Entries are only appended, until compact rebuilds the tables.
type styleTable struct {
	styles  []Style
	index   map[Style]uint16
	links   []Link
	linkIdx map[Link]uint16
	// gen counts the compactions: a copy of the table is only valid for the generation it was made in.
	gen uint32
	// refused counts the styles and links the full table refused since the last compaction (0 before the first refusal).
	refused int
}

func newStyleTable() *styleTable {
	t := &styleTable{}
	t.reset(16)
	return t
}

// reset empties the table down to the default style; hint is the number of styles expected.
func (t *styleTable) reset(hint int) {
	t.styles = append(make([]Style, 0, hint+1), Style{})
	t.index = make(map[Style]uint16, hint+1)
	t.index[Style{}] = 0
	t.links = append(t.links[:0], Link{})
	t.linkIdx = map[Link]uint16{{}: 0}
}

// lookup returns the index of s, adding it; ok is false when the table is full.
func (t *styleTable) lookup(s Style) (idx uint16, ok bool) {
	if i, found := t.index[s]; found {
		return i, true
	}
	if len(t.styles) >= MaxStyles {
		return 0, false
	}
	i := uint16(len(t.styles))
	t.styles = append(t.styles, s)
	t.index[s] = i
	return i, true
}

// lookupLink returns the index of l, adding it; ok is false when the table is full.
func (t *styleTable) lookupLink(l Link) (idx uint16, ok bool) {
	if i, found := t.linkIdx[l]; found {
		return i, true
	}
	if len(t.links) >= MaxLinks {
		return 0, false
	}
	i := uint16(len(t.links))
	t.links = append(t.links, l)
	t.linkIdx[l] = i
	return i, true
}

// isBlank reports whether the cell shows nothing and has the default style.
func (c Cell) isPlainBlank() bool { return c.Rune == 0 && c.Style == 0 && c.Flags&^CellWideFill == 0 }

// quantize rounds the 24-bit colors of s to the nearest entries of the 6x6x6 color cube of the 256-color palette.
func quantize(s Style) Style {
	s.Fg, s.Bg, s.UnderlineColor = quantizeColor(s.Fg), quantizeColor(s.Bg), quantizeColor(s.UnderlineColor)
	return s
}

func quantizeColor(c Color) Color {
	if c.Kind() != ColorRGB {
		return c
	}
	r, g, b := c.RGB()
	q := func(v uint8) uint8 { return uint8((int(v) + 25) / 51) }
	return IndexedColor(16 + 36*q(r) + 6*q(g) + q(b))
}
