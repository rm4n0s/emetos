package edit

import (
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Type types text at every cursor, replacing its selection. A single "\n" is a
// new line (NewLine). In overtype mode a typed character replaces the one after
// the cursor, or, with a selection, the selection and the characters after it
// that the text is longer than the selection by.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorTypeEditOperations.ts
// (SimpleCharacterTypeOperation, ReplaceOvertypeCommand)
func (c Commands) Type(sels []buffer.Selection, text string) Result {
	text = normalizeNewlines(text)
	if text == "\n" {
		return c.NewLine(sels)
	}
	if isSingleChar(text) {
		if res, ok := c.typeAutoIndent(sels, text); ok {
			return res
		}
	}
	plans := make([]plan, len(sels))
	for i, s := range sels {
		r := c.validRange(s)
		if c.Options.Overtype {
			r = c.overtypeRange(r, text)
		}
		plans[i] = replacePlan(r, text)
	}
	return build(sels, plans, document.KindTyping)
}

// overtypeRange grows r over the characters that text replaces in overtype mode.
// Sizes are in UTF-16 units as VS Code counts them, and never go past the line.
func (c Commands) overtypeRange(r buffer.Range, text string) buffer.Range {
	units := 0
	for _, ch := range text {
		units += 1 + int(utf8.RuneLen(ch)/4)
	}
	if !r.IsEmpty() {
		units--
	}
	line := c.Model.LineContent(r.End.Line)
	col := r.End.Col
	for units > 0 && col < len(line) {
		ch, w := utf8.DecodeRuneInString(line[col:])
		col += w
		units -= 1 + utf8.RuneLen(ch)/4
	}
	r.End.Col = col
	return r
}

// NewLine breaks the line at every cursor, replacing its selection, and starts the new
// line with the indentation that editor.autoIndent gives (Options.AutoIndent): none, that
// of the line before the cursor, or what the brackets, the on-enter rules and the
// indentation rules of the language say (SPEC §8.6.2).
//
// Ported from VS Code: cursorTypeEditOperations.ts (EnterOperation)
func (c Commands) NewLine(sels []buffer.Selection) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		plans[i] = c.enterPlan(c.validRange(s), false)
	}
	return build(sels, plans, document.KindOther)
}

// LineBreakInsert is Insert Line Break (Ctrl+O): it breaks the line at every cursor as NewLine
// does, but the cursor stays before the line break.
func (c Commands) LineBreakInsert(sels []buffer.Selection) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		plans[i] = c.enterPlan(c.validRange(s), true)
	}
	return build(sels, plans, document.KindOther)
}

// CompositionType is what an input method does while it composes: at each cursor
// with an empty selection it replaces the replacePrev characters before the cursor
// and the replaceNext after it by text, and puts the cursor positionDelta
// characters after the end of the text (negative to put it before). Characters
// are code points; a cursor with a selection is left alone.
//
// Ported from VS Code: cursorTypeEditOperations.ts (CompositionOperation)
func (c Commands) CompositionType(sels []buffer.Selection, text string, replacePrev, replaceNext, positionDelta int) Result {
	text = normalizeNewlines(text)
	plans := make([]plan, len(sels))
	for i, s := range sels {
		if !s.IsEmpty() {
			continue
		}
		pos := c.Model.ValidatePosition(s.Active)
		line := c.Model.LineContent(pos.Line)
		start, end := pos.Col, pos.Col
		for n := 0; n < replacePrev && start > 0; n++ {
			_, w := utf8.DecodeLastRuneInString(line[:start])
			start -= w
		}
		for n := 0; n < replaceNext && end < len(line); n++ {
			_, w := utf8.DecodeRuneInString(line[end:])
			end += w
		}
		// Where the cursor goes, as a column of the line with only this edit made.
		own := line[:start] + text + line[end:]
		col := start + len(text)
		for n := 0; n < positionDelta && col < len(own); n++ {
			_, w := utf8.DecodeRuneInString(own[col:])
			col += w
		}
		for n := 0; n > positionDelta && col > 0; n-- {
			_, w := utf8.DecodeLastRuneInString(own[:col])
			col -= w
		}
		shift := col - (start + len(text))
		plans[i] = plan{
			edits: []document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: pos.Line, Col: start}, End: buffer.Position{Line: pos.Line, Col: end}}, Text: text}},
			selection: func(_ *mapper, now []buffer.Range) buffer.Selection {
				return buffer.NewCursor(now[0].End.Delta(0, shift))
			},
		}
	}
	return build(sels, plans, document.KindTyping)
}

// Tab is the Tab key. A cursor on a line of white space only goes to the
// indentation of a new level (replacing the white space when the line does not
// start with it); any other cursor, and a selection inside a line, is replaced by
// spaces up to the next indent stop (or one tab); a selection of whole lines, or of
// several, indents those lines.
//
// Ported from VS Code: cursorTypeEditOperations.ts (TabOperation). On a line of white
// space only, the indentation is the one the rules of the language suggest, or one level.
func (c Commands) Tab(sels []buffer.Selection) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		r := c.validRange(s)
		line := c.Model.LineContent(r.Start.Line)
		switch {
		case s.IsEmpty():
			if isBlank(line) && c.cheapToTokenize(r.Start.Line) {
				want := c.blankLineIndent(r.Start.Line)
				if !strings.HasPrefix(line, want) {
					all := buffer.Range{Start: buffer.Position{Line: r.Start.Line}, End: buffer.Position{Line: r.Start.Line, Col: len(line)}}
					plans[i] = replacePlan(all, want)
					continue
				}
			}
			plans[i] = c.jumpToNextIndent(r)
		case r.Start.Line == r.End.Line && (r.Start.Col != 0 || r.End.Col != len(line)):
			plans[i] = c.jumpToNextIndent(r)
		default:
			plans[i] = c.shift(s, false)
		}
	}
	return build(sels, plans, document.KindOther)
}

// jumpToNextIndent replaces r by the white space that reaches the next indent stop.
func (c Commands) jumpToNextIndent(r buffer.Range) plan {
	text := "\t"
	if c.Options.InsertSpaces {
		size := c.Options.indentSize()
		line := c.Model.LineContent(r.Start.Line)
		text = strings.Repeat(" ", size-VisibleColumn(line, r.Start.Col, c.Options.tabSize())%size)
	}
	return replacePlan(r, text)
}

// Indent indents the lines the selections touch by one level (Ctrl+]).
func (c Commands) Indent(sels []buffer.Selection) Result { return c.shiftAll(sels, false) }

// Outdent takes one level of indentation off the lines the selections touch
// (Shift+Tab, Ctrl+[).
func (c Commands) Outdent(sels []buffer.Selection) Result { return c.shiftAll(sels, true) }

func (c Commands) shiftAll(sels []buffer.Selection, unshift bool) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		plans[i] = c.shift(s, unshift)
	}
	return build(sels, plans, document.KindOther)
}

// shift indents or outdents the lines of a selection.
//
// Ported from VS Code: src/vs/editor/common/commands/shiftCommand.ts, without the
// part that reads the language's on-enter rules to keep "extra spaces" of
// misaligned lines (it needs the language configuration).
func (c Commands) shift(s buffer.Selection, unshift bool) plan {
	r := c.validRange(s)
	startLine, endLine := r.Start.Line, r.End.Line
	if r.End.Col == 0 && startLine != endLine {
		endLine--
	}
	tabSize, indentSize, insertSpaces := c.Options.tabSize(), c.Options.indentSize(), c.Options.InsertSpaces
	shouldIndentEmptyLines := startLine == endLine

	var edits []document.Edit
	var lines []int
	add := func(line, from, to int, text string) {
		edits = append(edits, document.Edit{
			Range: buffer.Range{Start: buffer.Position{Line: line, Col: from}, End: buffer.Position{Line: line, Col: to}},
			Text:  text,
		})
		lines = append(lines, line)
	}
	caretAtEnd := false    // the cursor ends after the new indentation
	startStaysPut := false // the start of the selection keeps its column

	if c.Options.UseTabStops {
		caretAtEnd = s.IsEmpty() && isBlank(c.Model.LineContent(startLine))
		for line := startLine; line <= endLine; line++ {
			text := c.Model.LineContent(line)
			end := firstNonWhitespace(text)
			if unshift && (len(text) == 0 || end == 0) {
				continue
			}
			if !shouldIndentEmptyLines && !unshift && len(text) == 0 {
				continue
			}
			if end < 0 {
				end = len(text)
			}
			if unshift && end == 0 {
				continue
			}
			add(line, 0, end, shiftedIndent(text, end, tabSize, indentSize, insertSpaces, unshift))
			if line == startLine && !s.IsEmpty() {
				startStaysPut = r.Start.Col <= end
			}
		}
	} else {
		caretAtEnd = !unshift && s.IsEmpty() && len(c.Model.LineContent(startLine)) == 0
		oneIndent := "\t"
		if insertSpaces {
			oneIndent = strings.Repeat(" ", indentSize)
		}
		for line := startLine; line <= endLine; line++ {
			text := c.Model.LineContent(line)
			end := firstNonWhitespace(text)
			if unshift && (len(text) == 0 || end == 0) {
				continue
			}
			if !shouldIndentEmptyLines && !unshift && len(text) == 0 {
				continue
			}
			if end < 0 {
				end = len(text)
			}
			if unshift && end == 0 {
				continue
			}
			if unshift {
				end = min(end, indentSize)
				for i := 0; i < end; i++ {
					if text[i] == '\t' {
						end = i + 1
						break
					}
				}
				add(line, 0, end, "")
			} else {
				add(line, 0, 0, oneIndent)
				if line == startLine && !s.IsEmpty() {
					startStaysPut = r.Start.Col == 0
				}
			}
		}
	}

	return plan{edits: edits, selection: func(m *mapper, now []buffer.Range) buffer.Selection {
		if caretAtEnd {
			return buffer.NewCursor(now[0].End)
		}
		// The selection follows the edits like a decoration: a selection that is
		// not empty does not grow at its ends, a caret sticks to the character
		// before it at the end of a line and to the one after it elsewhere.
		track := func(p buffer.Position, sticksToPrevious, staysPut bool) buffer.Position {
			for k, line := range lines {
				if line != p.Line {
					continue
				}
				e := edits[k]
				col := document.TrackOffset(p.Col, sticksToPrevious, e.Range.Start.Col, e.Range.End.Col, len(e.Text))
				if staysPut && col > p.Col {
					col = p.Col
				}
				return buffer.Position{Line: now[k].Start.Line, Col: now[k].Start.Col - e.Range.Start.Col + col}
			}
			return m.old(p)
		}
		if s.IsEmpty() {
			atEnd := r.Start.Col == len(c.Model.LineContent(r.Start.Line))
			return buffer.NewCursor(track(r.Start, atEnd, false))
		}
		start := track(r.Start, false, startStaysPut)
		end := track(r.End, true, false)
		if s.Anchor.Compare(s.Active) <= 0 {
			return buffer.Selection{Anchor: start, Active: end}
		}
		return buffer.Selection{Anchor: end, Active: start}
	}}
}
