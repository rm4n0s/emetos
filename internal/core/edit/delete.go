package edit

import (
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// leftDeleteOffset is the byte column that Backspace goes back to from col in
// line: one code point, or the whole emoji it ends (with its modifiers, variation
// selector or keycap, and the zero-width joiner that links it to the emoji before).
//
// Ported from VS Code: src/vs/base/common/strings.ts (getLeftDeleteOffset)
func leftDeleteOffset(line string, col int) int {
	if col <= 0 {
		return 0
	}
	if off, ok := offsetBeforeLastEmojiComponent(line, col); ok {
		return off
	}
	_, w := utf8.DecodeLastRuneInString(line[:col])
	return col - w
}

func offsetBeforeLastEmojiComponent(line string, col int) (int, bool) {
	prev := func() rune {
		r, w := utf8.DecodeLastRuneInString(line[:col])
		col -= w
		return r
	}
	r := prev()
	for (0x1F3FB <= r && r <= 0x1F3FF) || r == 0xFE0F || r == 0x20E3 {
		if col == 0 {
			return 0, false
		}
		r = prev()
	}
	if !isEmojiImprecise(r) {
		return 0, false
	}
	result := col
	if result > 0 && prev() == 0x200D {
		result = col
	}
	return result, true
}

// leftDeleteRange is what Backspace deletes at a cursor: the selection, or the
// white space back to the previous indent stop when the cursor is in the
// indentation (with UseTabStops), or one character, or the line break.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorDeleteOperations.ts
func (c Commands) leftDeleteRange(s buffer.Selection) buffer.Range {
	if !s.IsEmpty() {
		return c.validRange(s)
	}
	pos := c.Model.ValidatePosition(s.Active)
	line := c.Model.LineContent(pos.Line)
	if c.Options.UseTabStops && pos.Col > 0 {
		last := firstNonWhitespace(line)
		if last < 0 {
			last = len(line)
		}
		if pos.Col <= last {
			from := VisibleColumn(line, pos.Col, c.Options.tabSize())
			to := ColumnFromVisible(line, PrevIndentTabStop(from, c.Options.indentSize()), c.Options.tabSize())
			return buffer.Range{Start: buffer.Position{Line: pos.Line, Col: to}, End: pos}
		}
	}
	switch {
	case pos.Col > 0:
		return buffer.Range{Start: buffer.Position{Line: pos.Line, Col: leftDeleteOffset(line, pos.Col)}, End: pos}
	case pos.Line > 0:
		return buffer.Range{Start: lineEnd(c.Model, pos.Line-1), End: pos}
	}
	return buffer.Range{Start: pos, End: pos}
}

// rightDeleteRange is what the Delete key deletes at a cursor: the selection, or
// the grapheme after it, or the line break.
func (c Commands) rightDeleteRange(s buffer.Selection) buffer.Range {
	if !s.IsEmpty() {
		return c.validRange(s)
	}
	pos := c.Model.ValidatePosition(s.Active)
	return buffer.Range{Start: pos, End: c.mv.rightOf(pos)}
}

// deleteRanges deletes a range at every cursor; the cursor ends where the range
// started. The edits join the undo group of their kind, unless one of them
// crosses a line break, which always starts a group.
func (c Commands) deleteRanges(sels []buffer.Selection, kind document.EditKind, rangeOf func(buffer.Selection) buffer.Range) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		r := rangeOf(s)
		if r.IsEmpty() {
			continue
		}
		if r.SpansMultipleLines() {
			kind = document.KindOther
		}
		plans[i] = replacePlan(r, "")
	}
	return build(sels, plans, kind)
}

// DeleteLeft is Backspace at every cursor.
func (c Commands) DeleteLeft(sels []buffer.Selection) Result {
	return c.deleteRanges(sels, document.KindDeleteLeft, c.leftDeleteRange)
}

// DeleteRight is the Delete key at every cursor.
func (c Commands) DeleteRight(sels []buffer.Selection) Result {
	return c.deleteRanges(sels, document.KindDeleteRight, c.rightDeleteRange)
}
