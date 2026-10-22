package edit

import (
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// translate moves a position by a number of lines the way adding a cursor does: at
// the first or last line it stays where it is (VS Code's vertical with
// allowMoveOnEdgeLine false).
func (mv Mover) translate(pos buffer.Position, rows, desired int) (buffer.Position, int) {
	if target := pos.Line + rows; target < 0 || target > lastLine(mv.Model) {
		return pos, desired
	}
	return mv.Layout.MoveVertically(pos, rows, desired)
}

// addCursors returns each cursor followed by a copy of it moved by rows lines.
func (mv Mover) addCursors(cursors []Cursor, rows int) []Cursor {
	out := make([]Cursor, 0, 2*len(cursors))
	for _, c := range cursors {
		c.Selection = buffer.Selection{Anchor: mv.Model.ValidatePosition(c.Selection.Anchor), Active: mv.Model.ValidatePosition(c.Position())}
		out = append(out, c)
		active, desired := mv.translate(c.Position(), rows, c.DesiredVisualCol)
		anchor := active
		if !c.Selection.IsEmpty() {
			// A cursor keeps one column to aim for, which is the active end's: the anchor
			// aims for the column it is at.
			anchor, _ = mv.translate(c.Selection.Anchor, rows, -1)
		}
		out = append(out, Cursor{Selection: buffer.Selection{Anchor: anchor, Active: active}, DesiredVisualCol: desired})
	}
	return out
}

// AddCursorAbove adds a cursor on the line above each cursor (Shift+Alt+Up on
// Linux), keeping the shape of its selection. The cursors, the new ones included,
// are the result; those that end up in the same place merge when set into Cursors.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorMoveCommands.ts (addCursorUp).
func (mv Mover) AddCursorAbove(cursors []Cursor) []Cursor { return mv.addCursors(cursors, -1) }

// AddCursorBelow adds a cursor on the line below each cursor (Shift+Alt+Down).
func (mv Mover) AddCursorBelow(cursors []Cursor) []Cursor { return mv.addCursors(cursors, 1) }

// CursorsAtLineEnds puts a cursor at the end of each line of each selection that
// is not empty (Shift+Alt+I): at the end of every line but the last, and at the end
// of the selection when that is not at the start of a line. It returns nil when no
// selection has lines to end.
//
// Ported from VS Code: multicursor.ts (InsertCursorAtEndOfEachLineSelected).
func (c Commands) CursorsAtLineEnds(sels []buffer.Selection) []buffer.Selection {
	var out []buffer.Selection
	for _, raw := range sels {
		s := c.validSelection(raw)
		if s.IsEmpty() {
			continue
		}
		for l := s.Start().Line; l < s.End().Line; l++ {
			out = append(out, buffer.NewCursor(lineEnd(c.Model, l)))
		}
		if s.End().Col > 0 {
			out = append(out, buffer.NewCursor(s.End()))
		}
	}
	return out
}

// CursorsToBottom adds a cursor with the first selection's columns on every line
// from its first line to the last one (multicursor.ts, addCursorsToBottom).
func (c Commands) CursorsToBottom(sels []buffer.Selection) []buffer.Selection {
	if len(sels) == 0 {
		return nil
	}
	s := c.validSelection(sels[0])
	var out []buffer.Selection
	for l := s.Start().Line; l <= lastLine(c.Model); l++ {
		out = append(out, c.columnsOnLine(s, l))
	}
	return out
}

// CursorsToTop adds a cursor with the first selection's columns on every line from
// its first line to the first one (multicursor.ts, addCursorsToTop).
func (c Commands) CursorsToTop(sels []buffer.Selection) []buffer.Selection {
	if len(sels) == 0 {
		return nil
	}
	s := c.validSelection(sels[0])
	var out []buffer.Selection
	for l := s.Start().Line; l >= 0; l-- {
		out = append(out, c.columnsOnLine(s, l))
	}
	return out
}

// columnsOnLine is a selection with the start and end columns of s, moved to a line
// and kept inside it. The columns are those of the first selection's start line.
func (c Commands) columnsOnLine(s buffer.Selection, line int) buffer.Selection {
	text := c.Model.LineContent(line)
	return buffer.Selection{
		Anchor: posAt(line, snapCol(text, s.Anchor.Col)),
		Active: posAt(line, snapCol(text, s.Active.Col)),
	}
}

// SearchOptions are the find options that the occurrence commands use when the
// selection is not empty: the match case and whole word switches of the find widget
// (editor.selectedTextMatchMode "findOptions", the default). An empty selection
// always starts a case sensitive search for whole words.
type SearchOptions struct {
	// MatchCase makes the search case sensitive.
	MatchCase bool
	// WholeWord only accepts matches that are whole words.
	WholeWord bool
}

// OccurrenceSession is a series of Add Selection to Next Occurrence (Ctrl+D) and its
// relatives: it remembers what is searched for. The editor keeps it while the
// selections change only through these commands and drops it as soon as they change
// in any other way, or the editor loses focus.
//
// Ported from VS Code: src/vs/editor/contrib/multicursor/browser/multicursor.ts
// (MultiCursorSession), without the link to the find widget.
type OccurrenceSession struct {
	text      string
	matchCase bool
	wholeWord bool
	// current is a match found when the session began (the word at an empty cursor),
	// which the next command uses before it searches.
	current *buffer.Selection
}

// StartOccurrences begins a session for the primary selection, which is the first
// one. It returns nil when there is nothing to search for (an empty selection that
// is not in a word).
func (c Commands) StartOccurrences(sels []buffer.Selection, find SearchOptions) *OccurrenceSession {
	if len(sels) == 0 {
		return nil
	}
	s := c.validSelection(sels[0])
	sess := &OccurrenceSession{matchCase: find.MatchCase, wholeWord: find.WholeWord}
	if len(sels) == 1 && s.IsEmpty() {
		sess.matchCase, sess.wholeWord = true, true
	}
	if s.IsEmpty() {
		line := c.Model.LineContent(s.Active.Line)
		w, ok := c.Options.words().WordAt(line, s.Active.Col)
		if !ok {
			return nil
		}
		sess.text = line[w.Start:w.End]
		cur := buffer.Selection{Anchor: posAt(s.Active.Line, w.Start), Active: posAt(s.Active.Line, w.End)}
		sess.current = &cur
	} else {
		sess.text = c.textIn(s.Range())
	}
	return sess
}

// search makes the search of the session.
func (sess *OccurrenceSession) search(c Commands) *literalSearch {
	var words *WordClassifier
	if sess.wholeWord {
		words = c.Options.words()
	}
	return newLiteralSearch(c.Model, sess.text, sess.matchCase, words)
}

// nextMatch is the match to add or move to, searching from the end of the last
// added selection (or from its start, backwards).
func (sess *OccurrenceSession) nextMatch(c Commands, sels []buffer.Selection, backwards bool) (buffer.Selection, bool) {
	if sess.current != nil {
		m := *sess.current
		sess.current = nil
		return m, true
	}
	if sess.text == "" || len(sels) == 0 {
		return buffer.Selection{}, false
	}
	last := c.validSelection(sels[len(sels)-1])
	var r buffer.Range
	var ok bool
	if backwards {
		r, ok = sess.search(c).Previous(last.Start())
	} else {
		r, ok = sess.search(c).Next(last.End())
	}
	if !ok {
		return buffer.Selection{}, false
	}
	return buffer.Selection{Anchor: r.Start, Active: r.End}, true
}

// AddNext adds the next occurrence of the searched text after the last selection
// (Ctrl+D); ok is false when there is none.
func (sess *OccurrenceSession) AddNext(c Commands, sels []buffer.Selection) ([]buffer.Selection, bool) {
	m, ok := sess.nextMatch(c, sels, false)
	if !ok {
		return nil, false
	}
	return append(append([]buffer.Selection(nil), sels...), m), true
}

// AddPrevious adds the previous occurrence before the last selection.
func (sess *OccurrenceSession) AddPrevious(c Commands, sels []buffer.Selection) ([]buffer.Selection, bool) {
	m, ok := sess.nextMatch(c, sels, true)
	if !ok {
		return nil, false
	}
	return append(append([]buffer.Selection(nil), sels...), m), true
}

// MoveToNext replaces the last selection by the next occurrence (Ctrl+K Ctrl+D,
// "skip" an occurrence).
func (sess *OccurrenceSession) MoveToNext(c Commands, sels []buffer.Selection) ([]buffer.Selection, bool) {
	m, ok := sess.nextMatch(c, sels, false)
	if !ok {
		return nil, false
	}
	return append(append([]buffer.Selection(nil), sels[:len(sels)-1]...), m), true
}

// MoveToPrevious replaces the last selection by the previous occurrence.
func (sess *OccurrenceSession) MoveToPrevious(c Commands, sels []buffer.Selection) ([]buffer.Selection, bool) {
	m, ok := sess.nextMatch(c, sels, true)
	if !ok {
		return nil, false
	}
	return append(append([]buffer.Selection(nil), sels[:len(sels)-1]...), m), true
}

// SelectAll selects every occurrence of the searched text (Ctrl+Shift+L), in the
// ranges of scope when there are any. The primary selection becomes the occurrence
// that touches the selection the command started from.
func (sess *OccurrenceSession) SelectAll(c Commands, primary buffer.Selection, scope []buffer.Range) []buffer.Selection {
	if sess.text == "" {
		return nil
	}
	matches := sess.search(c).All(scope)
	if len(matches) == 0 {
		return nil
	}
	p := c.validSelection(primary).Range()
	for i, m := range matches {
		if _, ok := m.Intersect(p); ok {
			matches[0], matches[i] = matches[i], matches[0]
			break
		}
	}
	out := make([]buffer.Selection, len(matches))
	for i, m := range matches {
		out[i] = buffer.Selection{Anchor: m.Start, Active: m.End}
	}
	return out
}

// containSameText reports whether every selection but the first holds the text of
// the first, in the case the search uses.
func (c Commands) containSameText(sels []buffer.Selection, matchCase bool) bool {
	text := func(s buffer.Selection) string {
		t := c.textIn(c.validSelection(s).Range())
		if !matchCase {
			t = strings.ToLower(t)
		}
		return t
	}
	first := text(sels[0])
	for _, s := range sels[1:] {
		if c.validSelection(s).IsEmpty() || text(s) != first {
			return false
		}
	}
	return true
}

// expandEmptyToWord selects the word at every empty selection.
func (c Commands) expandEmptyToWord(sels []buffer.Selection) []buffer.Selection {
	out := make([]buffer.Selection, len(sels))
	for i, raw := range sels {
		s := c.validSelection(raw)
		out[i] = s
		if !s.IsEmpty() {
			continue
		}
		line := c.Model.LineContent(s.Active.Line)
		if w, ok := c.Options.words().WordAt(line, s.Active.Col); ok {
			out[i] = buffer.Selection{Anchor: posAt(s.Active.Line, w.Start), Active: posAt(s.Active.Line, w.End)}
		}
	}
	return out
}

// AddNextOccurrence is Ctrl+D. Without a session it starts one, except when there
// are several selections that do not hold the same text: then it only selects the
// word at each empty cursor. It returns the session to keep (nil if none), the new
// selections, and false when nothing changes.
//
// Ported from VS Code: multicursor.ts (MultiCursorSelectionController.addSelectionToNextFindMatch).
func (c Commands) AddNextOccurrence(sess *OccurrenceSession, sels []buffer.Selection, find SearchOptions) (*OccurrenceSession, []buffer.Selection, bool) {
	if sess == nil {
		if len(sels) > 1 && !c.containSameText(sels, find.MatchCase) {
			return nil, c.expandEmptyToWord(sels), true
		}
		if sess = c.StartOccurrences(sels, find); sess == nil {
			return nil, nil, false
		}
	}
	out, ok := sess.AddNext(c, sels)
	return sess, out, ok
}

// AddPreviousOccurrence adds the previous occurrence, starting a session if needed.
func (c Commands) AddPreviousOccurrence(sess *OccurrenceSession, sels []buffer.Selection, find SearchOptions) (*OccurrenceSession, []buffer.Selection, bool) {
	if sess == nil {
		if sess = c.StartOccurrences(sels, find); sess == nil {
			return nil, nil, false
		}
	}
	out, ok := sess.AddPrevious(c, sels)
	return sess, out, ok
}

// MoveToNextOccurrence moves the last selection to the next occurrence, starting a
// session if needed.
func (c Commands) MoveToNextOccurrence(sess *OccurrenceSession, sels []buffer.Selection, find SearchOptions) (*OccurrenceSession, []buffer.Selection, bool) {
	if sess == nil {
		if sess = c.StartOccurrences(sels, find); sess == nil {
			return nil, nil, false
		}
	}
	out, ok := sess.MoveToNext(c, sels)
	return sess, out, ok
}

// MoveToPreviousOccurrence moves the last selection to the previous occurrence.
func (c Commands) MoveToPreviousOccurrence(sess *OccurrenceSession, sels []buffer.Selection, find SearchOptions) (*OccurrenceSession, []buffer.Selection, bool) {
	if sess == nil {
		if sess = c.StartOccurrences(sels, find); sess == nil {
			return nil, nil, false
		}
	}
	out, ok := sess.MoveToPrevious(c, sels)
	return sess, out, ok
}

// SelectAllOccurrences is Ctrl+Shift+L: it starts a session if needed and selects
// every occurrence of what the primary selection (or the word at it) holds.
func (c Commands) SelectAllOccurrences(sess *OccurrenceSession, sels []buffer.Selection, find SearchOptions, scope []buffer.Range) (*OccurrenceSession, []buffer.Selection) {
	if sess == nil {
		if sess = c.StartOccurrences(sels, find); sess == nil {
			return nil, nil
		}
	}
	return sess, sess.SelectAll(c, sels[0], scope)
}
