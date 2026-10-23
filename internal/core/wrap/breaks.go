package wrap

import (
	"unicode"
	"unicode/utf8"
)

// Ported from VS Code: src/vs/editor/common/viewModel/monospaceLineBreaksComputer.ts

// Indent is how continuation rows are indented (editor.wrappingIndent).
type Indent int

const (
	// IndentNone starts continuation rows at the left edge.
	IndentNone Indent = iota
	// IndentSame indents them as the line is.
	IndentSame
	// IndentIndent indents them one tab more than the line.
	IndentIndent
	// IndentDeepIndent indents them two tabs more than the line.
	IndentDeepIndent
)

// ParseIndent reads a value of editor.wrappingIndent: "none", "same", "indent" or "deepIndent";
// anything else is "same", VS Code's default.
func ParseIndent(s string) Indent {
	switch s {
	case "none":
		return IndentNone
	case "indent":
		return IndentIndent
	case "deepIndent":
		return IndentDeepIndent
	}
	return IndentSame
}

// Default break characters of VS Code (editor.wordWrapBreakBeforeCharacters and
// editor.wordWrapBreakAfterCharacters): a row may break before one of the first and after one of
// the second, which also name where the ideographic and the kinsoku shori rules apply.
const (
	DefaultBreakBefore = "([{‘“〈《「『【〔（［｛｢£¥＄￡￥+＋"
	DefaultBreakAfter  = " \t})]?|/&.,;¢°′″‰℃、。｡､￠，．：；？！％・･ゝゞヽヾーァィゥェォッャュョヮヵヶぁぃぅぇぉっゃゅょゎゕゖㇰㇱㇲㇳㇴㇵㇶㇷㇸㇹㇺㇻㇼㇽㇾㇿ々〻ｧｨｩｪｫｬｭｮｯｰ”〉》」』】〕）］｝｣"
)

// Options are what the breaking of a line depends on.
type Options struct {
	// TabSize is the distance between tab stops, in columns.
	TabSize int
	// Column is the number of columns of a row, 1 or more.
	Column int
	// Indent is how continuation rows are indented.
	Indent Indent
	// BreakBefore and BreakAfter are the characters a row may break before and after; empty
	// strings break nowhere but at the column.
	BreakBefore, BreakAfter string
	// KeepAll keeps words of ideographs together (editor.wordBreak "keepAll").
	KeepAll bool
	// FullWidthColumns is how many columns a full-width character takes; 0 means 2.
	FullWidthColumns int
}

// Breaks says how a line is split into rows.
type Breaks struct {
	// Offsets are the byte offsets where the rows end: row i is the text from Offsets[i-1] (0
	// for the first row) to Offsets[i]; the last is the length of the line.
	Offsets []int
	// VisibleColumns are the visible columns of the line at those offsets.
	VisibleColumns []int
	// IndentLength is the number of columns continuation rows are indented by.
	IndentLength int
}

// RowCount is the number of rows.
func (b *Breaks) RowCount() int {
	if b == nil {
		return 1
	}
	return len(b.Offsets)
}

// RowStart is the byte offset where a row starts.
func (b *Breaks) RowStart(row int) int {
	if b == nil || row <= 0 {
		return 0
	}
	return b.Offsets[min(row, len(b.Offsets))-1]
}

// RowEnd is the byte offset where a row ends, or -1 for the end of the line when b is nil.
func (b *Breaks) RowEnd(row int) int {
	if b == nil {
		return -1
	}
	return b.Offsets[min(max(row, 0), len(b.Offsets)-1)]
}

// RowOf is the row that shows a byte offset of the line: an offset where a row starts is on that
// row, and the end of the line is on the last.
func (b *Breaks) RowOf(offset int) int {
	if b == nil {
		return 0
	}
	for row, end := range b.Offsets {
		if offset < end {
			return row
		}
	}
	return len(b.Offsets) - 1
}

// characterClass is how a character takes part in the choice of where to break.
type characterClass int

const (
	classNone characterClass = iota
	classBreakBefore
	classBreakAfter
	// classBreakIdeographic is for Han and Kana.
	classBreakIdeographic
)

// classifier gives the class of a character.
type classifier struct {
	before, after map[rune]bool
}

func newClassifier(before, after string) classifier {
	c := classifier{before: map[rune]bool{}, after: map[rune]bool{}}
	for _, r := range before {
		c.before[r] = true
	}
	for _, r := range after {
		c.after[r] = true
	}
	return c
}

func (c classifier) get(r rune) characterClass {
	if r < 256 {
		switch {
		case c.after[r]:
			return classBreakAfter // set last, so it wins
		case c.before[r]:
			return classBreakBefore
		}
		return classNone
	}
	// Han and Kana: CJK Unified Ideographs, Extension A, Hiragana and Katakana
	if (r >= 0x3040 && r <= 0x30FF) || (r >= 0x3400 && r <= 0x4DBF) || (r >= 0x4E00 && r <= 0x9FFF) {
		return classBreakIdeographic
	}
	switch {
	case c.after[r]:
		return classBreakAfter
	case c.before[r]:
		return classBreakBefore
	}
	return classNone
}

// isFullWidth tells whether a character is shown in two cells: CJK, Hangul and the full-width
// forms.
//
// Ported from VS Code: src/vs/base/common/strings.ts (isFullWidthCharacter)
func isFullWidth(r rune) bool {
	return (r >= 0x2E80 && r <= 0xD7AF) || (r >= 0xF900 && r <= 0xFAFF) || (r >= 0xFF01 && r <= 0xFF5E)
}

// isExtending tells whether a character has no width of its own and belongs to the one before it
// (a combining mark, the zero width joiner, a variation selector): a row never starts with one.
// VS Code gives them one column; Emetos keeps a grapheme cluster in one row.
func isExtending(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200D || (r >= 0xFE00 && r <= 0xFE0F)
}

// canBreak is the rule of kinsoku shori: not after a leading character such as an open bracket,
// not before a trailing character such as a period.
func canBreak(prev rune, prevClass characterClass, r rune, class characterClass, keepAll bool) bool {
	return r != ' ' && !isExtending(r) &&
		((prevClass == classBreakAfter && class != classBreakAfter) ||
			(prevClass != classBreakBefore && class == classBreakBefore) ||
			(!keepAll && prevClass == classBreakIdeographic && class != classBreakAfter) ||
			(!keepAll && class == classBreakIdeographic && prevClass != classBreakBefore))
}

// charWidth is the number of columns of a character that starts at a visible column.
func charWidth(r rune, visibleColumn, tabSize, fullWidth int) int {
	switch {
	case r == '\t':
		return tabSize - visibleColumn%tabSize
	case r > 0xFFFF:
		return 2
	case isExtending(r):
		return 0
	case isFullWidth(r):
		return fullWidth
	case r < 32:
		// the substitutions of the control characters are often wide
		return fullWidth
	}
	return 1
}

func tabWidth(visibleColumn, tabSize int) int { return tabSize - visibleColumn%tabSize }

// indentLength is the number of columns continuation rows are indented by.
func indentLength(line string, tabSize, column, fullWidth int, indent Indent) int {
	length := 0
	if indent == IndentNone {
		return 0
	}
	first := -1
	for i, r := range line {
		if r != ' ' && r != '\t' {
			first = i
			break
		}
	}
	if first == -1 {
		return 0
	}
	// the indentation of the line
	for i := 0; i < first; i++ {
		if line[i] == '\t' {
			length += tabWidth(length, tabSize)
		} else {
			length++
		}
	}
	// and one tab, or two, more
	extra := 0
	switch indent {
	case IndentIndent:
		extra = 1
	case IndentDeepIndent:
		extra = 2
	}
	for range extra {
		length += tabWidth(length, tabSize)
	}
	// a row that has room for nothing but the indentation sticks to the left edge
	if length+fullWidth > column {
		length = 0
	}
	return length
}

// Compute breaks a line into rows of o.Column columns, breaking at the best place before the
// column (after a space, before a bracket, between ideographs), or at the column when there is
// none. It returns nil when the line fits in one row. Tabs go to the next tab stop and a
// character of more than one code unit is never split.
//
// Ported from VS Code: src/vs/editor/common/viewModel/monospaceLineBreaksComputer.ts (createLineBreaks)
func Compute(line string, o Options) *Breaks {
	if o.Column <= 0 || len(line) <= 1 {
		return nil
	}
	tabSize := max(o.TabSize, 1)
	fullWidth := o.FullWidthColumns
	if fullWidth <= 0 {
		fullWidth = 2
	}
	cls := newClassifier(o.BreakBefore, o.BreakAfter)
	indent := indentLength(line, tabSize, o.Column, fullWidth, o.Indent)
	wrappedWidth := o.Column - indent

	var offsets, columns []int
	breakOffset, breakColumn := 0, 0
	breakingWidth := o.Column

	r, size := utf8.DecodeRuneInString(line)
	prev, prevClass := r, cls.get(r)
	visible := charWidth(r, 0, tabSize, fullWidth)
	if r > 0xFFFF {
		// a character outside the BMP is one unit that is never split
		prev, prevClass = 0xDC00, classNone
	}

	for i := size; i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		charStart := i
		i += size
		class := classNone
		if r <= 0xFFFF {
			class = cls.get(r)
		}
		w := charWidth(r, visible, tabSize, fullWidth)

		if canBreak(prev, prevClass, r, class, o.KeepAll) {
			breakOffset, breakColumn = charStart, visible
		}
		visible += w

		// does the character go over the column? Then a break is needed before it at least
		if visible > breakingWidth {
			if breakOffset == 0 || visible-breakColumn > wrappedWidth {
				// cannot break at the best place: break before the character
				breakOffset, breakColumn = charStart, visible-w
			}
			lineStart := 0
			if len(offsets) > 0 {
				lineStart = offsets[len(offsets)-1]
			}
			if breakOffset > lineStart {
				offsets = append(offsets, breakOffset)
				columns = append(columns, breakColumn)
				breakingWidth = breakColumn + wrappedWidth
				breakOffset = 0
			}
		}
		prev, prevClass = r, class
		if r > 0xFFFF {
			prev, prevClass = 0xDC00, classNone
		}
	}
	if len(offsets) == 0 {
		return nil
	}
	offsets = append(offsets, len(line))
	columns = append(columns, visible)
	return &Breaks{Offsets: offsets, VisibleColumns: columns, IndentLength: indent}
}

// IndentColumns is the number of columns continuation rows of a line are indented by, as Compute
// gives them: for a line that is broken another way (by laying the text out).
func IndentColumns(line string, o Options) int {
	fullWidth := o.FullWidthColumns
	if fullWidth <= 0 {
		fullWidth = 2
	}
	return indentLength(line, max(o.TabSize, 1), o.Column, fullWidth, o.Indent)
}
