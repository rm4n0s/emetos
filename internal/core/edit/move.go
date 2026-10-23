package edit

import (
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// Mover moves cursors over a text: character, vertical, line, word, page and
// document moves of SPEC §3.1, each as a pure function from a cursor to a cursor
// (SPEC §7.2). With selecting, only the active end moves and the anchor stays;
// without, a selection collapses first as VS Code's does.
type Mover struct {
	Model   Model
	Options Options
	// Layout says how lines are laid out in rows; nil means one row per line.
	Layout LineLayout
}

// NewMover makes a Mover; layout may be nil.
func NewMover(m Model, o Options, layout LineLayout) Mover {
	if layout == nil {
		layout = ModelLineLayout{Model: m, TabSize: o.tabSize()}
	}
	return Mover{Model: m, Options: o, Layout: layout}
}

// MoveTo moves to a position, which is moved into the text first (a click).
func (mv Mover) MoveTo(c Cursor, p buffer.Position, selecting bool) Cursor {
	return c.moveTo(selecting, mv.Model.ValidatePosition(p), -1)
}

// leftOf is the position one character to the left, or the end of the line above.
func (mv Mover) leftOf(p buffer.Position) buffer.Position {
	if p.Col > 0 {
		return buffer.Position{Line: p.Line, Col: PrevGrapheme(mv.Model.LineContent(p.Line), p.Col)}
	}
	if p.Line > 0 {
		above := p.Line - 1
		if first, _, hidden := hiddenSpan(mv.Layout, above); hidden {
			above = first - 1 // a folded range is one step: the cursor goes to the line that folds it
		}
		return lineEnd(mv.Model, above)
	}
	return p
}

// rightOf is the position one character to the right, or the start of the next line.
func (mv Mover) rightOf(p buffer.Position) buffer.Position {
	line := mv.Model.LineContent(p.Line)
	if p.Col < len(line) {
		return buffer.Position{Line: p.Line, Col: NextGrapheme(line, p.Col)}
	}
	next := p.Line + 1
	if _, end, hidden := hiddenSpan(mv.Layout, next); hidden {
		next = end + 1
	}
	if next <= lastLine(mv.Model) {
		return buffer.Position{Line: next}
	}
	return p
}

// Left moves one character left. A selection that is not being extended collapses
// to its start instead.
func (mv Mover) Left(c Cursor, selecting bool) Cursor {
	if !c.Selection.IsEmpty() && !selecting {
		return c.moveTo(false, c.Selection.Start(), -1)
	}
	return c.moveTo(selecting, mv.leftOf(mv.Model.ValidatePosition(c.Position())), -1)
}

// Right moves one character right. A selection that is not being extended
// collapses to its end instead.
func (mv Mover) Right(c Cursor, selecting bool) Cursor {
	if !c.Selection.IsEmpty() && !selecting {
		return c.moveTo(false, c.Selection.End(), -1)
	}
	return c.moveTo(selecting, mv.rightOf(mv.Model.ValidatePosition(c.Position())), -1)
}

// vertical moves a number of rows.
func (mv Mover) vertical(c Cursor, selecting bool, rows int) Cursor {
	pos := c.Position()
	if !c.Selection.IsEmpty() && !selecting {
		if rows < 0 {
			pos = c.Selection.Start()
		} else {
			pos = c.Selection.End()
		}
	}
	next, desired := mv.Layout.MoveVertically(mv.Model.ValidatePosition(pos), rows, c.DesiredVisualCol)
	return c.moveTo(selecting, next, desired)
}

// Up moves one row up, keeping the desired visual column.
func (mv Mover) Up(c Cursor, selecting bool) Cursor { return mv.vertical(c, selecting, -1) }

// Down moves one row down, keeping the desired visual column.
func (mv Mover) Down(c Cursor, selecting bool) Cursor { return mv.vertical(c, selecting, 1) }

// PageUp moves a page up.
func (mv Mover) PageUp(c Cursor, selecting bool) Cursor {
	return mv.vertical(c, selecting, -mv.Layout.PageRows())
}

// PageDown moves a page down.
func (mv Mover) PageDown(c Cursor, selecting bool) Cursor {
	return mv.vertical(c, selecting, mv.Layout.PageRows())
}

// firstNonWhitespace is the byte column of the first character that is not a
// space or a tab, or -1 when the line has none.
func firstNonWhitespace(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return -1
}

// Home is the Home key: on a row that continues a wrapped line it goes to the
// start of the row; otherwise it goes to the first character that is not white
// space, and when already there, to the start of the line (smart home).
func (mv Mover) Home(c Cursor, selecting bool) Cursor {
	pos := mv.Model.ValidatePosition(c.Position())
	if rs := mv.Layout.RowStart(pos); rs.Col > 0 {
		return c.moveTo(selecting, rs, -1)
	}
	first := firstNonWhitespace(mv.Model.LineContent(pos.Line))
	if first < 0 {
		first = 0
	}
	col := first
	if pos.Col == first {
		col = 0
	}
	return c.moveTo(selecting, buffer.Position{Line: pos.Line, Col: col}, -1)
}

// End is the End key: the end of the row.
func (mv Mover) End(c Cursor, selecting bool) Cursor {
	pos := mv.Model.ValidatePosition(c.Position())
	return c.moveTo(selecting, mv.Layout.RowEnd(pos), -1)
}

// DocStart is Ctrl+Home.
func (mv Mover) DocStart(c Cursor, selecting bool) Cursor {
	return c.moveTo(selecting, buffer.Position{}, -1)
}

// DocEnd is Ctrl+End.
func (mv Mover) DocEnd(c Cursor, selecting bool) Cursor {
	return c.moveTo(selecting, lineEnd(mv.Model, lastLine(mv.Model)), -1)
}

// WordNavigation says which stops word movement makes.
type WordNavigation int

const (
	// WordStart stops at the start of words (cursorWordStartLeft and Right).
	WordStart WordNavigation = iota
	// WordStartFast is WordStart, but going left it steps over a single separator
	// followed by a word (cursorWordLeft).
	WordStartFast
	// WordEnd stops at the end of words (cursorWordEndLeft and Right).
	WordEnd
	// WordAccessibility stops at the start of words and skips separators only.
	WordAccessibility
)

// oneUnit reports whether s is one character that VS Code counts as one UTF-16
// unit, which is what its "single separator" tests look at.
func oneUnit(s string) bool {
	r, w := utf8.DecodeRuneInString(s)
	return w == len(s) && w > 0 && r < 0x10000
}

// wordLeftPosition ports VS Code's WordOperations.moveWordLeft.
func (mv Mover) wordLeftPosition(pos buffer.Position, nav WordNavigation, multi bool) buffer.Position {
	line, col := pos.Line, pos.Col
	if col == 0 && line > 0 {
		line--
		col = len(mv.Model.LineContent(line))
	}
	content := mv.Model.LineContent(line)
	words := mv.Options.words()
	prev, ok := words.PrevWord(content, col)
	switch nav {
	case WordStart:
		if ok {
			return buffer.Position{Line: line, Col: prev.Start}
		}
		return buffer.Position{Line: line}
	case WordStartFast:
		if !multi && ok && prev.Kind == SeparatorWord && oneUnit(content[prev.Start:prev.End]) && prev.NextClass == ClassRegular {
			prev, ok = words.PrevWord(content, prev.Start)
		}
		if ok {
			return buffer.Position{Line: line, Col: prev.Start}
		}
		return buffer.Position{Line: line}
	case WordAccessibility:
		for ok && prev.Kind == SeparatorWord {
			prev, ok = words.PrevWord(content, prev.Start)
		}
		if ok {
			return buffer.Position{Line: line, Col: prev.Start}
		}
		return buffer.Position{Line: line}
	}
	// Stopping at the ends of words.
	if ok && col <= prev.End {
		prev, ok = words.PrevWord(content, prev.Start)
	}
	if ok {
		return buffer.Position{Line: line, Col: prev.End}
	}
	return buffer.Position{Line: line}
}

// wordRightPosition ports VS Code's WordOperations.moveWordRight.
func (mv Mover) wordRightPosition(pos buffer.Position, nav WordNavigation) buffer.Position {
	line, col := pos.Line, pos.Col
	movedDown := false
	if col == len(mv.Model.LineContent(line)) && line < lastLine(mv.Model) {
		movedDown = true
		line++
		col = 0
	}
	content := mv.Model.LineContent(line)
	words := mv.Options.words()
	next, ok := words.NextWord(content, col)
	switch nav {
	case WordEnd:
		if ok && next.Kind == SeparatorWord && oneUnit(content[next.Start:next.End]) && next.NextClass == ClassRegular {
			next, ok = words.NextWord(content, next.End)
		}
		if ok {
			col = next.End
		} else {
			col = len(content)
		}
	case WordAccessibility:
		if movedDown {
			// Pretend the cursor is before the first character, so a word that starts there is found.
			col = -1
		}
		for ok && (next.Kind == SeparatorWord || next.Start <= col) {
			next, ok = words.NextWord(content, next.End)
		}
		if ok {
			col = next.Start
		} else {
			col = len(content)
		}
	default:
		if ok && !movedDown && col >= next.Start {
			next, ok = words.NextWord(content, next.End)
		}
		if ok {
			col = next.Start
		} else {
			col = len(content)
		}
	}
	return buffer.Position{Line: line, Col: col}
}

// WordLeft moves to the previous word stop; multi says there is more than one
// cursor, which changes where WordStartFast stops so that they stay together.
func (mv Mover) WordLeft(c Cursor, selecting bool, nav WordNavigation, multi bool) Cursor {
	return c.moveTo(selecting, mv.wordLeftPosition(mv.Model.ValidatePosition(c.Position()), nav, multi), -1)
}

// WordRight moves to the next word stop.
func (mv Mover) WordRight(c Cursor, selecting bool, nav WordNavigation) Cursor {
	return c.moveTo(selecting, mv.wordRightPosition(mv.Model.ValidatePosition(c.Position()), nav), -1)
}

// partLeftPosition is the part move on its own (_moveWordPartLeft): to the start
// of the previous word part, to the end of the line above at the start of a line.
func (mv Mover) partLeftPosition(pos buffer.Position) buffer.Position {
	switch {
	case pos.Col > 0:
		pos.Col = PrevWordPart(mv.Model.LineContent(pos.Line), pos.Col)
	case pos.Line > 0:
		pos = lineEnd(mv.Model, pos.Line-1)
	}
	return pos
}

// partRightPosition is the part move on its own (_moveWordPartRight): to the end
// of the next word part, to the start of the line below at the end of a line.
func (mv Mover) partRightPosition(pos buffer.Position) buffer.Position {
	line := mv.Model.LineContent(pos.Line)
	switch {
	case pos.Col < len(line):
		pos.Col = NextWordPart(line, pos.Col)
	case pos.Line < lastLine(mv.Model):
		pos = buffer.Position{Line: pos.Line + 1}
	}
	return pos
}

// WordPartLeft moves to the previous stop of snake_case, camelCase and kebab-case
// parts, which is also never past the previous word start or word end (VS Code's
// WordPartOperations.moveWordPartLeft takes the nearest of the three).
func (mv Mover) WordPartLeft(c Cursor, selecting bool, multi bool) Cursor {
	pos := mv.Model.ValidatePosition(c.Position())
	best := mv.partLeftPosition(pos)
	for _, nav := range []WordNavigation{WordStart, WordEnd} {
		if p := mv.wordLeftPosition(pos, nav, multi); p.Compare(best) > 0 {
			best = p
		}
	}
	return c.moveTo(selecting, best, -1)
}

// WordPartRight moves to the next stop of word parts, never past the next word
// start or word end (WordPartOperations.moveWordPartRight).
func (mv Mover) WordPartRight(c Cursor, selecting bool) Cursor {
	pos := mv.Model.ValidatePosition(c.Position())
	best := mv.partRightPosition(pos)
	for _, nav := range []WordNavigation{WordStart, WordEnd} {
		if p := mv.wordRightPosition(pos, nav); p.Compare(best) < 0 {
			best = p
		}
	}
	return c.moveTo(selecting, best, -1)
}

// SelectAll selects the whole text.
func (mv Mover) SelectAll() Cursor {
	return CursorOf(buffer.Selection{Active: lineEnd(mv.Model, lastLine(mv.Model))})
}

// ExpandLine selects the lines the cursor's selection touches, whole, up to the
// start of the line after (Ctrl+L); on the last line it ends at the end of the text.
func (mv Mover) ExpandLine(c Cursor) Cursor {
	startLine := c.Selection.Start().Line
	endLine := c.Selection.End().Line
	end := lineEnd(mv.Model, lastLine(mv.Model))
	if endLine != lastLine(mv.Model) {
		end = buffer.Position{Line: endLine + 1}
	}
	return CursorOf(buffer.Selection{Anchor: buffer.Position{Line: startLine}, Active: end})
}
