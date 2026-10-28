package terminal

import (
	"strings"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// The prompt input model (SPEC §10.25 "Terminal IntelliSense"): the command line the user is typing at the prompt, and where the
// cursor is in it. It is read from the screen, between the place where the shell said the prompt ends (the marker of shell
// integration) and the end of the text that follows the cursor, so it is right whatever the shell does to the line (history,
// editing keys, completion), and it needs no tracking of the keys.

// promptInput is the command line being typed.
type promptInput struct {
	// value is the line (several lines when the command continues on the next one) and cursor the byte offset of the cursor in it.
	value  string
	cursor int
	// startX and startLine are where the line starts, cursorX and cursorLine where the cursor is (lines are absolute, the
	// numbering of Frame.Top). They place the list.
	startX, cursorX       int
	startLine, cursorLine int64
	// ok is false when there is no prompt to type at: no shell integration, a command that is running, the alternate screen.
	ok bool
}

// maxInputRows bounds the rows read for a command line.
const maxInputRows = 64

// readPromptInput reads the command line from the emulator. frame is a scratch Frame that is kept between calls.
func readPromptInput(t *Terminal, frame *emulator.Frame) promptInput {
	if t.si == nil || !t.si.Active() {
		return promptInput{}
	}
	em := t.Emulator()
	if em.Modes().AltScreen {
		return promptInput{}
	}
	cur := t.si.Current()
	if cur == nil || cur.executed != 0 || cur.startLine < 0 {
		return promptInput{}
	}
	curLine, curCol, _ := em.CursorPosition()
	if curLine < cur.startLine || curLine == cur.startLine && curCol < cur.startX || curLine-cur.startLine >= maxInputRows {
		return promptInput{}
	}
	first := int(cur.startLine - em.FirstLine())
	if first < 0 {
		return promptInput{}
	}
	// read the rows from the start of the line to the cursor, and the rows the text after the cursor wraps onto
	n := int(curLine-cur.startLine) + 1
	last := min(n+maxInputRows, int(em.LineCount())-first)
	em.ReadLines(first, last, frame)
	if frame.Rows < 1 || frame.Cols < 1 {
		return promptInput{}
	}
	// ReadLines fills rows 0..last-1 of the frame (the rows are the lines asked for, as many as the frame has)
	rows := min(last, frame.Rows)
	var before, after strings.Builder
	for r := 0; r < min(n, rows); r++ {
		from, to := 0, frame.Cols
		if r == 0 {
			from = cur.startX
		}
		if r == n-1 {
			to = min(curCol, frame.Cols)
		}
		appendRow(&before, frame, r, from, to)
		if r < n-1 && !frame.Wrapped[r] {
			trimTrailingSpaces(&before)
			before.WriteByte('\n')
		}
	}
	// the text after the cursor: the rest of the cursor's row and the rows it wraps onto, up to the text the shell shows as a
	// suggestion (dimmed or gray)
	var tail strings.Builder
	for r := n - 1; r < rows; r++ {
		from := 0
		if r == n-1 {
			from = min(curCol, frame.Cols)
		}
		ghost := appendRowUntilGhost(&tail, frame, r, from)
		if ghost || !frame.Wrapped[r] {
			break
		}
	}
	after.WriteString(strings.TrimRight(tail.String(), " "))
	in := promptInput{ok: true, value: before.String() + after.String(), cursor: before.Len()}
	in.cursorX, in.cursorLine = curCol, curLine
	in.startX, in.startLine = cur.startX, cur.startLine
	return in
}

// appendRow adds the text of the cells [from, to) of row r (an empty cell is a space).
func appendRow(b *strings.Builder, f *emulator.Frame, r, from, to int) {
	for x := from; x < to && x < f.Cols; x++ {
		b.WriteString(f.Text(r, x))
	}
}

func trimTrailingSpaces(b *strings.Builder) {
	s := strings.TrimRight(b.String(), " ")
	b.Reset()
	b.WriteString(s)
}

// appendRowUntilGhost adds the text of row r from a column on, and stops at the first cell that looks like a suggestion of the
// shell (dim, or the gray the suggestion plugins of zsh and fish use); it reports whether it stopped.
func appendRowUntilGhost(b *strings.Builder, f *emulator.Frame, r, from int) bool {
	for x := from; x < f.Cols; x++ {
		c := f.Cell(r, x)
		if c.Rune != 0 && isGhostStyle(f, c.Style) {
			return true
		}
		b.WriteString(f.Text(r, x))
	}
	return false
}

// isGhostStyle tells whether a style is the one a shell uses for the rest of a command line it suggests: faint text, the
// bright black of zsh-autosuggestions, or a gray.
func isGhostStyle(f *emulator.Frame, style uint16) bool {
	if style == 0 || int(style) >= len(f.Styles) {
		return false
	}
	st := f.Styles[style]
	if st.Attrs&emulator.AttrDim != 0 {
		return true
	}
	switch st.Fg.Kind() {
	case emulator.ColorIndexed:
		i := st.Fg.Index()
		return i == 8 || i >= 238 && i <= 248
	case emulator.ColorRGB:
		r, g, b := st.Fg.RGB()
		return r == g && g == b && r >= 0x50 && r <= 0xa0
	}
	return false
}
