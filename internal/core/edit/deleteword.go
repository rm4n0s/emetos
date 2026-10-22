package edit

import (
	"sort"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// deleteWords deletes, at every cursor, the range that rangeOf finds. The edits are
// their own undo group, as VS Code puts an undo stop before and after them.
func (c Commands) deleteWords(sels []buffer.Selection, rangeOf func(buffer.Selection) buffer.Range) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		r := rangeOf(s)
		if r.IsEmpty() {
			continue
		}
		plans[i] = replacePlan(r, "")
	}
	return build(sels, plans, document.KindOther)
}

// lastNonBlank is the byte index of the last character at or before i that is not
// a space or a tab, or -1.
func lastNonBlank(line string, i int) int {
	for i = min(i, len(line)-1); i >= 0; i-- {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return -1
}

// firstNonBlankFrom is the byte index of the first character at or after i that is
// not a space or a tab, or the length of the line.
func firstNonBlankFrom(line string, i int) int {
	for ; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return len(line)
}

// wordLeftDeleteRange ports WordOperations.deleteWordLeft. ok is false at the
// start of the text, where there is nothing to delete.
func (c Commands) wordLeftDeleteRange(s buffer.Selection, nav WordNavigation, whitespace bool) (buffer.Range, bool) {
	if !s.IsEmpty() {
		return c.validRange(s), true
	}
	pos := c.Model.ValidatePosition(s.Active)
	if pos.Line == 0 && pos.Col == 0 {
		return buffer.Range{}, false
	}
	line := c.Model.LineContent(pos.Line)
	if whitespace {
		if last := lastNonBlank(line, pos.Col-1); last+1 < pos.Col-1 {
			return buffer.Range{Start: buffer.Position{Line: pos.Line, Col: last + 1}, End: pos}, true
		}
	}
	words := c.Options.words()
	prev, found := words.PrevWord(line, pos.Col)
	to := pos
	start := func() {
		to.Col = 0
		if pos.Col == 0 {
			to = lineEnd(c.Model, pos.Line-1)
		}
	}
	if nav == WordEnd {
		if found && pos.Col <= prev.End {
			prev, found = words.PrevWord(line, prev.Start)
		}
		if found {
			to.Col = prev.End
		} else {
			start()
		}
	} else if found {
		to.Col = prev.Start
	} else {
		start()
	}
	return buffer.RangeFromPositions(to, pos), true
}

// wordRightDeleteRange ports WordOperations.deleteWordRight. ok is false at the
// end of the text.
func (c Commands) wordRightDeleteRange(s buffer.Selection, nav WordNavigation, whitespace bool) (buffer.Range, bool) {
	if !s.IsEmpty() {
		return c.validRange(s), true
	}
	pos := c.Model.ValidatePosition(s.Active)
	line := c.Model.LineContent(pos.Line)
	last := lastLine(c.Model)
	if pos.Line == last && pos.Col == len(line) {
		return buffer.Range{}, false
	}
	if whitespace {
		if first := firstNonBlankFrom(line, pos.Col); pos.Col < first {
			return buffer.Range{Start: pos, End: buffer.Position{Line: pos.Line, Col: first}}, true
		}
	}
	words := c.Options.words()
	next, found := words.NextWord(line, pos.Col)
	to := pos
	// lineBreak is what a right delete does when no word follows on the line.
	lineBreak := func() {
		if pos.Col < len(line) || pos.Line == last {
			to.Col = len(line)
			return
		}
		to.Line++
		to.Col = len(c.Model.LineContent(to.Line))
		if w, ok := words.NextWord(c.Model.LineContent(to.Line), 0); ok {
			to.Col = w.Start
		}
	}
	if nav == WordEnd {
		if found {
			to.Col = next.End
		} else {
			lineBreak()
		}
	} else {
		if found && pos.Col >= next.Start {
			next, found = words.NextWord(line, next.End)
		}
		if found {
			to.Col = next.Start
		} else {
			lineBreak()
		}
	}
	return buffer.RangeFromPositions(pos, to), true
}

// wordDeleteRange picks the range of a word delete command.
func (c Commands) wordDeleteRange(left bool, nav WordNavigation, whitespace bool) func(buffer.Selection) buffer.Range {
	return func(s buffer.Selection) buffer.Range {
		var r buffer.Range
		var ok bool
		if left {
			r, ok = c.wordLeftDeleteRange(s, nav, whitespace)
		} else {
			r, ok = c.wordRightDeleteRange(s, nav, whitespace)
		}
		if !ok {
			return buffer.Range{}
		}
		return r
	}
}

// DeleteWordLeft is Ctrl+Backspace: it deletes back to the start of the word, or
// back over white space when there are two or more blanks before the cursor.
func (c Commands) DeleteWordLeft(sels []buffer.Selection) Result {
	return c.deleteWords(sels, c.wordDeleteRange(true, WordStart, true))
}

// DeleteWordStartLeft deletes back to the start of the word, without the white
// space rule (deleteWordStartLeft).
func (c Commands) DeleteWordStartLeft(sels []buffer.Selection) Result {
	return c.deleteWords(sels, c.wordDeleteRange(true, WordStart, false))
}

// DeleteWordEndLeft deletes back to the end of the previous word (deleteWordEndLeft).
func (c Commands) DeleteWordEndLeft(sels []buffer.Selection) Result {
	return c.deleteWords(sels, c.wordDeleteRange(true, WordEnd, false))
}

// DeleteWordRight is Ctrl+Delete: it deletes to the end of the word, or the white
// space after the cursor.
func (c Commands) DeleteWordRight(sels []buffer.Selection) Result {
	return c.deleteWords(sels, c.wordDeleteRange(false, WordEnd, true))
}

// DeleteWordStartRight deletes to the start of the next word (deleteWordStartRight).
func (c Commands) DeleteWordStartRight(sels []buffer.Selection) Result {
	return c.deleteWords(sels, c.wordDeleteRange(false, WordStart, false))
}

// DeleteWordEndRight deletes to the end of the word (deleteWordEndRight).
func (c Commands) DeleteWordEndRight(sels []buffer.Selection) Result {
	return c.deleteWords(sels, c.wordDeleteRange(false, WordEnd, false))
}

// DeleteWordPartLeft deletes back to the nearest of the word start, the word end
// and the start of the part (camelCase, snake_case, kebab-case).
//
// Ported from VS Code: cursorWordOperations.ts (WordPartOperations.deleteWordPartLeft)
func (c Commands) DeleteWordPartLeft(sels []buffer.Selection) Result {
	return c.deleteWords(sels, func(s buffer.Selection) buffer.Range {
		if !s.IsEmpty() {
			return c.validRange(s)
		}
		pos := c.Model.ValidatePosition(s.Active)
		var cands []buffer.Range
		for _, nav := range []WordNavigation{WordStart, WordEnd} {
			if r, ok := c.wordLeftDeleteRange(s, nav, true); ok {
				cands = append(cands, r)
			}
		}
		cands = append(cands, buffer.RangeFromPositions(c.mv.partLeftPosition(pos), pos))
		sort.SliceStable(cands, func(i, j int) bool { return buffer.CompareUsingEnds(cands[i], cands[j]) < 0 })
		if len(cands) < 3 {
			return buffer.Range{}
		}
		return cands[2]
	})
}

// DeleteWordPartRight deletes to the nearest of the word start, the word end and
// the end of the part.
func (c Commands) DeleteWordPartRight(sels []buffer.Selection) Result {
	return c.deleteWords(sels, func(s buffer.Selection) buffer.Range {
		if !s.IsEmpty() {
			return c.validRange(s)
		}
		pos := c.Model.ValidatePosition(s.Active)
		var cands []buffer.Range
		for _, nav := range []WordNavigation{WordStart, WordEnd} {
			if r, ok := c.wordRightDeleteRange(s, nav, true); ok {
				cands = append(cands, r)
			}
		}
		cands = append(cands, buffer.RangeFromPositions(pos, c.mv.partRightPosition(pos)))
		sort.SliceStable(cands, func(i, j int) bool { return buffer.CompareUsingStarts(cands[i], cands[j]) < 0 })
		return cands[0]
	})
}

// DeleteInsideWord deletes the word at the cursor with the blanks on one side of
// it (the blanks around the cursor when it is in white space), and the line break
// of an empty line. With onlyWord it leaves the blanks.
//
// Ported from VS Code: cursorWordOperations.ts (WordOperations.deleteInsideWord)
func (c Commands) DeleteInsideWord(sels []buffer.Selection, onlyWord bool) Result {
	return c.deleteWords(sels, func(s buffer.Selection) buffer.Range {
		if !s.IsEmpty() {
			return c.validRange(s)
		}
		pos := c.Model.ValidatePosition(s.Active)
		return c.insideWordRange(pos, onlyWord)
	})
}

func isBlankByte(line string, i int) bool { return line[i] == ' ' || line[i] == '\t' }

func (c Commands) insideWordRange(pos buffer.Position, onlyWord bool) buffer.Range {
	line := c.Model.LineContent(pos.Line)
	at := func(start, end int) buffer.Range {
		return buffer.Range{
			Start: buffer.Position{Line: pos.Line, Col: min(start, pos.Col)},
			End:   buffer.Position{Line: pos.Line, Col: max(end, pos.Col)},
		}
	}
	// In white space: delete all of it around the cursor.
	if len(line) > 0 {
		left := max(pos.Col-1, 0)
		right := min(pos.Col, len(line)-1)
		if isBlankByte(line, left) && isBlankByte(line, right) {
			for left > 0 && isBlankByte(line, left-1) {
				left--
			}
			for right+1 < len(line) && isBlankByte(line, right+1) {
				right++
			}
			return at(left, right+1)
		}
	}
	if len(line) == 0 {
		switch {
		case pos.Line > 0:
			return buffer.Range{Start: lineEnd(c.Model, pos.Line-1), End: buffer.Position{Line: pos.Line}}
		case pos.Line < lastLine(c.Model):
			return buffer.Range{Start: buffer.Position{Line: pos.Line}, End: buffer.Position{Line: pos.Line + 1}}
		}
		return buffer.Range{Start: pos, End: pos}
	}
	touches := func(w Word) bool { return w.Start <= pos.Col && pos.Col <= w.End }
	withBlanks := func(w Word) buffer.Range {
		start, end := w.Start, w.End
		if onlyWord {
			return at(start, end)
		}
		expandedRight := false
		for end < len(line) && isBlankByte(line, end) {
			expandedRight = true
			end++
		}
		if !expandedRight {
			for start > 0 && isBlankByte(line, start-1) {
				start--
			}
		}
		return at(start, end)
	}
	words := c.Options.words()
	prev, hasPrev := words.PrevWord(line, pos.Col)
	if hasPrev && touches(prev) {
		return withBlanks(prev)
	}
	next, hasNext := words.NextWord(line, pos.Col)
	if hasNext && touches(next) {
		return withBlanks(next)
	}
	switch {
	case hasPrev && hasNext:
		return at(prev.End, next.Start)
	case hasPrev:
		return at(prev.Start, prev.End)
	case hasNext:
		return at(next.Start, next.End)
	}
	return at(0, len(line))
}
