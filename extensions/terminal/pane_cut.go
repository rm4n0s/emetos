package terminal

import (
	"strings"

	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// Cut in the context menu of a terminal (SPEC §10.25 "The UI"; Emetos's own, VS Code's terminal has none: the user asked for
// it, 2026-11-17). What a program printed cannot be taken out of the screen, so Cut applies only to a selection inside the
// command line being typed at the prompt of a shell with shell integration (the prompt input of Terminal IntelliSense). It
// copies the selection and then edits the line as the user would: the cursor keys take the shell's cursor to the end of the
// selection and Backspace deletes it, so it works with whatever line editor the shell has (readline, zle, fish) and leaves
// the undo of the line to it. Anywhere else the menu shows Cut disabled.

// inputChar is a character of the command line on the screen: its text, the cell where it starts and how many cells it takes.
type inputChar struct {
	text string
	at   pos
	w    int
}

// cutPlan is how to delete the selection from the command line: move the cursor by move characters (right when positive),
// then delete del characters before it. text is what is deleted, which Cut copies (Copy leaves out the blanks at the end of a
// selection; Cut copies what it takes out).
type cutPlan struct {
	move, del int
	text      string
}

// inputChars places the characters of a command line of one line (it may wrap onto several rows) that readPromptInput read
// into frame: the cells from the start of the line on, as many characters as make up its value.
func inputChars(in promptInput, frame *emulator.Frame) []inputChar {
	var out []inputChar
	read := 0
	for r := 0; r < frame.Rows && read < len(in.value); r++ {
		from := 0
		if r == 0 {
			from = in.startX
		}
		for x := from; x < frame.Cols && read < len(in.value); x++ {
			text := frame.Text(r, x)
			if text == "" {
				continue // the right half of a wide character
			}
			w := 1
			if x+1 < frame.Cols && frame.Cell(r, x+1).Flags&emulator.CellTail != 0 {
				w = 2
			}
			out = append(out, inputChar{text: text, at: pos{line: in.startLine + int64(r), col: x}, w: w})
			read += len(text)
		}
	}
	return out
}

// planCut tells how to delete the selection from the command line, and false when Cut does not apply: no selection, no
// prompt to type at, a command line of several lines, or a selection that is not wholly in the command line (a selection
// that runs past the end of the line's last row stops at its last character).
func (pv *paneView) planCut() (cutPlan, bool) {
	s, ok := pv.sel.normalized()
	if !ok {
		return cutPlan{}, false
	}
	var frame emulator.Frame
	in := readPromptInput(pv.term, &frame)
	if !in.ok || in.value == "" || strings.Contains(in.value, "\n") {
		return cutPlan{}, false
	}
	chars := inputChars(in, &frame)
	if len(chars) == 0 {
		return cutPlan{}, false
	}
	start, end := s.anchor, s.head
	last := chars[len(chars)-1]
	lastCell := pos{line: last.at.line, col: last.at.col + last.w - 1}
	if lastCell.before(end) && end.line == lastCell.line {
		end = lastCell
	}
	if start.before(chars[0].at) || lastCell.before(end) {
		return cutPlan{}, false
	}
	cur := pos{line: in.cursorLine, col: in.cursorX}
	first, final, cursor := -1, -1, len(chars)
	for i, c := range chars {
		if first < 0 && !(pos{line: c.at.line, col: c.at.col + c.w - 1}).before(start) {
			first = i
		}
		if !end.before(c.at) {
			final = i
		}
		if cursor == len(chars) && !c.at.before(cur) {
			cursor = i
		}
	}
	if first < 0 || final < first {
		return cutPlan{}, false
	}
	var text strings.Builder
	for _, c := range chars[first : final+1] {
		text.WriteString(c.text)
	}
	return cutPlan{move: final + 1 - cursor, del: final - first + 1, text: text.String()}, true
}

// canCut tells whether Cut applies to the selection (the menu's item is enabled).
func (pv *paneView) canCut() bool {
	_, ok := pv.planCut()
	return ok
}

// cutSelection copies the selection and deletes it from the command line; it does nothing when Cut does not apply.
func (pv *paneView) cutSelection() {
	plan, ok := pv.planCut()
	if !ok {
		return
	}
	if pv.binding != nil {
		pv.binding.Clipboard().SetData(pv.env.clipboardCtx, services.NewClipboardData(plan.text), func(error) {})
	}
	right, left := "\x1b[C", "\x1b[D"
	if pv.term.Emulator().Modes().AppCursorKeys {
		right, left = "\x1bOC", "\x1bOD"
	}
	var b strings.Builder
	switch {
	case plan.move > 0:
		b.WriteString(strings.Repeat(right, plan.move))
	case plan.move < 0:
		b.WriteString(strings.Repeat(left, -plan.move))
	}
	b.WriteString(strings.Repeat("\x7f", plan.del))
	pv.userInput([]byte(b.String()))
}
