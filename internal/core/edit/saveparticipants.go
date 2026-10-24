package edit

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// SaveParticipants say which of the edits that a save makes to the text are on (SPEC §6.3): the settings
// files.trimTrailingWhitespace, files.insertFinalNewline and files.trimFinalNewlines of the language of the
// document.
type SaveParticipants struct {
	TrimTrailingWhitespace bool
	InsertFinalNewline     bool
	TrimFinalNewlines      bool
}

// Any tells whether a participant is on.
func (p SaveParticipants) Any() bool {
	return p.TrimTrailingWhitespace || p.InsertFinalNewline || p.TrimFinalNewlines
}

// SaveParticipants is what a save does to the text before it is written, as VS Code's TrimWhitespaceParticipant,
// FinalNewLineParticipant and TrimFinalNewLinesParticipant do one after the other: trailing white space is
// trimmed from every line (strings and regular expressions included), a final line break is added when the last
// line has text (the buffer holds "\n"; the file's line ending is written when it is saved), and the empty lines
// after the final line break are removed. VS Code applies them as three edits; this makes the result of the three
// as one batch, which is one step of the undo history.
//
// With autoSave (files.autoSave, SaveReason.AUTO) the white space in front of a cursor stays, so that the
// cursor does not jump while the user types, and no line up to the one of the cursor furthest down is removed. The
// selections of the result are the selections given, kept inside the text that is left.
//
// Not built: files.trimTrailingWhitespaceInRegexAndStrings (it is always on, which is its default) and the
// lines of a snippet session.
//
// Ported from VS Code: src/vs/workbench/contrib/codeEditor/browser/saveParticipants.ts
func (c Commands) SaveParticipants(sels []buffer.Selection, p SaveParticipants, autoSave bool) Result {
	n := c.Model.LineCount()
	// cursorCol is the rightmost cursor of each line of an automatic save, and lastCursorLine the lowest one.
	cursorCol := map[int]int{}
	lastCursorLine := -1
	if autoSave {
		for _, s := range sels {
			pos := c.validSelection(s).Active
			cursorCol[pos.Line] = max(cursorCol[pos.Line], pos.Col)
			lastCursorLine = max(lastCursorLine, pos.Line)
		}
	}
	// keep[i] is how much of line i stays after the trimming of white space.
	keep := make([]int, n)
	for i := range keep {
		text := c.Model.LineContent(i)
		keep[i] = len(text)
		if p.TrimTrailingWhitespace {
			keep[i] = lastNonBlank(text, len(text)-1) + 1
			if col, ok := cursorCol[i]; ok {
				keep[i] = min(max(keep[i], col), len(text))
			}
		}
	}
	lastLen := len(c.Model.LineContent(n - 1))

	insert := p.InsertFinalNewline && lastNonBlank(c.Model.LineContent(n - 1)[:keep[n-1]], keep[n-1]-1) >= 0

	// firstDeleted is the first line whose text and line break go with the trimming of the final new lines
	// (n when none does).
	firstDeleted := n
	if p.TrimFinalNewlines && n > 1 && !insert {
		lastNonEmpty := -1
		for i := n - 1; i >= 0; i-- {
			if keep[i] > 0 {
				lastNonEmpty = i
				break
			}
		}
		from := max(lastNonEmpty+1, lastCursorLine+1)
		// the range starts in front of line `from`, and ends at the end of the last line: it is empty
		// when `from` is the last line and that has no text
		if from < n-1 || (from == n-1 && lastLen > 0) {
			firstDeleted = from
		}
	}

	var edits []document.Edit
	for i := 0; i < min(firstDeleted, n); i++ {
		if keep[i] == len(c.Model.LineContent(i)) && (!insert || i != n-1) {
			continue
		}
		end := len(c.Model.LineContent(i))
		text := ""
		if insert && i == n-1 {
			text = "\n"
		}
		edits = append(edits, document.Edit{Range: rangeOf(i, keep[i], i, end), Text: text})
	}
	if firstDeleted < n {
		edits = append(edits, document.Edit{Range: rangeOf(firstDeleted, 0, n-1, lastLen)})
	}
	if len(edits) == 0 {
		return Result{Kind: document.KindOther}
	}

	// the lengths of the lines of the text after the edits
	var after []int
	switch {
	case firstDeleted < n:
		after = append(append(after, keep[:firstDeleted]...), 0)
	case insert:
		after = append(append(after, keep...), 0)
	default:
		after = keep
	}
	out := make([]buffer.Selection, len(sels))
	clamp := func(p buffer.Position) buffer.Position {
		p = c.Model.ValidatePosition(p)
		p.Line = min(p.Line, len(after)-1)
		p.Col = min(p.Col, after[p.Line])
		return p
	}
	for i, s := range sels {
		out[i] = buffer.Selection{Anchor: clamp(s.Anchor), Active: clamp(s.Active)}
	}
	return Result{Edits: edits, Selections: out, Kind: document.KindOther}
}
