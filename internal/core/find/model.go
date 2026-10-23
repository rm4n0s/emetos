package find

import (
	"slices"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: src/vs/editor/contrib/find/browser/findModel.ts,
// src/vs/editor/contrib/find/browser/replaceAllCommand.ts and
// src/vs/editor/common/commands/replaceCommand.ts (MIT)

// researchDelay is how long a search of a document that is too large to be searched while it is
// typed in waits after the last change of the state; updateDecorationsDelay is how long the matches
// wait after an edit of the document before they are searched again.
const (
	researchDelay         = 240 * time.Millisecond
	updateDecorationsWait = 100 * time.Millisecond
)

// tooLargeForSyncing is the size of a document above which a change of the state waits for researchDelay
// before the search runs.
const tooLargeForSyncing = 50 << 20

// Host is the editor the find model works for.
type Host interface {
	// Selection is the primary selection of the editor.
	Selection() buffer.Selection
	// SetSelection and SetSelections move the cursors.
	SetSelection(buffer.Selection)
	// SetSelections gives the editor all its selections, the first being the primary one.
	SetSelections([]buffer.Selection)
	// Reveal scrolls to a range, to the center of the editor when it is outside the viewport.
	Reveal(buffer.Range)
	// WordSeparators is editor.wordSeparators.
	WordSeparators() string
	// CursorMoveOnType is editor.find.cursorMoveOnType: typing the search string moves the cursor to
	// the first match after it.
	CursorMoveOnType() bool
	// After runs fn once, on the thread of the editor, after d; the returned function cancels it.
	After(d time.Duration, fn func()) (cancel func())
}

// Model is the find model of an editor (VS Code's FindModelBoundToEditorModel): it keeps the
// matches of the search string of a State marked in the document, moves the cursor from match to
// match and replaces. It must be used on the thread of the editor.
type Model struct {
	host  Host
	doc   *document.Document
	state *State
	dec   *decorations
	owner int

	unsubscribeState, unsubscribeDoc func()
	cancelUpdate, cancelStart        func()
	ignoreContentChanged             bool
	disposed                         bool

	// query is the compiled search of the last params.
	queryParams Params
	query       *Query
}

// NewModel makes the model of a state in an editor, and searches the document as the state says.
// owner (1 or more) is the owner of the document decorations the model makes.
func NewModel(host Host, doc *document.Document, state *State, owner int) *Model {
	m := &Model{host: host, doc: doc, state: state, owner: owner}
	m.dec = newDecorations(doc, owner, host.Selection().Active)
	m.unsubscribeDoc = doc.Subscribe(m.documentChanged)
	m.unsubscribeState = state.OnChange(m.stateChanged)
	m.research(false, Opt[[]buffer.Range]{Value: state.SearchScope(), Set: true})
	return m
}

// Dispose removes the decorations of the model and stops following the state and the document.
func (m *Model) Dispose() {
	m.disposed = true
	m.unsubscribeState()
	m.unsubscribeDoc()
	m.cancelTimers()
	m.dec.dispose()
}

func (m *Model) cancelTimers() {
	if m.cancelUpdate != nil {
		m.cancelUpdate()
		m.cancelUpdate = nil
	}
	if m.cancelStart != nil {
		m.cancelStart()
		m.cancelStart = nil
	}
}

// CursorMoved tells the model that the cursor moved. A move the user made (a click, an undo or a
// redo, not typing) is where the search goes on from.
func (m *Model) CursorMoved(byUser bool) {
	if m.disposed || !byUser {
		return
	}
	m.dec.setStartPosition(m.host.Selection().Active)
}

// documentChanged follows an edit of the document: the matches are searched again soon.
func (m *Model) documentChanged(e document.ChangeEvent) {
	if m.disposed || m.ignoreContentChanged {
		return
	}
	if e.Cause == document.CauseSetText {
		m.dec.reset() // the document dropped the decorations
	}
	m.dec.setStartPosition(m.host.Selection().Active)
	if m.cancelUpdate != nil {
		m.cancelUpdate()
	}
	m.cancelUpdate = m.host.After(updateDecorationsWait, func() {
		m.cancelUpdate = nil
		if !m.disposed {
			m.research(false, Opt[[]buffer.Range]{})
		}
	})
}

// stateChanged follows a change of the state.
func (m *Model) stateChanged(e ChangeEvent) {
	if m.disposed {
		return
	}
	if !e.SearchString && !e.IsReplaceRevealed && !e.IsRegex && !e.WholeWord && !e.MatchCase && !e.SearchScope {
		return
	}
	scope := Opt[[]buffer.Range]{}
	if e.SearchScope {
		scope = Opt[[]buffer.Range]{Value: m.state.SearchScope(), Set: true}
	}
	if m.doc.Snapshot().Len() > tooLargeForSyncing {
		if m.cancelStart != nil {
			m.cancelStart()
		}
		m.cancelStart = m.host.After(researchDelay, func() {
			m.cancelStart = nil
			if !m.disposed {
				m.research(e.MoveCursor, scope)
			}
		})
		return
	}
	m.research(e.MoveCursor, scope)
}

// params are the parameters of the search of the state now.
func (m *Model) params() Params {
	p := Params{Search: m.state.SearchString(), IsRegex: m.state.IsRegex(), MatchCase: m.state.MatchCase()}
	if m.state.WholeWord() {
		p.WordSeparators = m.host.WordSeparators()
	}
	return p
}

// currentQuery is the compiled search of the state now.
func (m *Model) currentQuery() *Query {
	p := m.params()
	if m.query == nil || m.queryParams != p {
		m.queryParams, m.query = p, p.Query()
	}
	return m.query
}

// searchRange is the range to search for a search scope, or all the text.
func (m *Model) searchRange(scope *buffer.Range) buffer.Range {
	if scope != nil {
		return *scope
	}
	return buffer.Range{End: m.doc.Snapshot().PositionAt(m.doc.Snapshot().Len())}
}

// research finds the matches again and marks them. newScope is the new search scope when it is Set;
// otherwise the scopes there are are searched again, as they moved with the edits.
func (m *Model) research(moveCursor bool, newScope Opt[[]buffer.Range]) {
	snap := m.doc.Snapshot()
	var scopes []buffer.Range
	if newScope.Set {
		scopes = newScope.Value
	} else {
		scopes = m.dec.findScopes()
	}
	for i, s := range scopes {
		if s.Start.Line != s.End.Line {
			end := s.End.Line
			if s.End.Col == 0 {
				end--
			}
			text, _ := snap.LineContent(end)
			scopes[i] = buffer.Range{Start: buffer.Position{Line: s.Start.Line}, End: buffer.Position{Line: end, Col: len(text)}}
		}
	}
	matches := m.findMatches(scopes, false, MatchesLimit)
	m.dec.set(matches, scopes)

	selection := m.host.Selection()
	position := m.dec.currentMatchesPosition(selection.Range())
	if position == 0 && len(matches) > 0 {
		// The selection is not on a match: the match nearest after it from the top of the document.
		after := len(matches)
		for i, match := range matches {
			if compareRangesUsingStarts(match.Range, selection.Range()) >= 0 {
				after = i
				break
			}
		}
		if after > 0 {
			position = after
		}
	}
	m.state.ChangeMatchInfo(position, m.dec.count(), nil)
	if moveCursor && m.host.CursorMoveOnType() {
		m.moveToNext(m.dec.startPosition())
	}
}

// compareRangesUsingStarts orders ranges by their start, then by their end.
func compareRangesUsingStarts(a, b buffer.Range) int {
	if c := a.Start.Compare(b.Start); c != 0 {
		return c
	}
	return a.End.Compare(b.End)
}

func (m *Model) hasMatches() bool { return m.state.MatchesCount() > 0 }

// cannotFind says that there is nothing to move to; with a search scope it shows the scope, as a
// reminder that the search is limited.
func (m *Model) cannotFind() bool {
	if m.hasMatches() {
		return false
	}
	if scope, ok := m.dec.findScope(); ok {
		m.host.Reveal(scope)
	}
	return true
}

// setCurrentFindMatch makes a match the current one and selects it.
func (m *Model) setCurrentFindMatch(match buffer.Range) {
	position := m.dec.setCurrentFindMatch(&match)
	m.state.ChangeMatchInfo(position, m.dec.count(), &match)
	m.host.SetSelection(buffer.Selection{Anchor: match.Start, Active: match.End})
	m.host.Reveal(match)
}

// usesLineStops says that the search string is a regular expression with ^ or $ in it.
func (m *Model) usesLineStops() bool {
	s := m.state.SearchString()
	return m.state.IsRegex() && (slices.Contains([]byte(s), '^') || slices.Contains([]byte(s), '$'))
}

// prevSearchPosition is the position to search before when the match before a position is empty
// and at it: one character back, or the end of the line before.
func (m *Model) prevSearchPosition(before buffer.Position) buffer.Position {
	snap := m.doc.Snapshot()
	line, col := before.Line, before.Col
	if m.usesLineStops() || col == 0 {
		if line == 0 {
			line = snap.LineCount() - 1
		} else {
			line--
		}
		return endOfLine(snap, line)
	}
	text, _ := snap.LineContent(line)
	_, size := utf8.DecodeLastRuneInString(text[:min(col, len(text))])
	return buffer.Position{Line: line, Col: col - size}
}

// nextSearchPosition is the position to search after when the match after a position is empty and
// at it: one character on, or the start of the line after.
func (m *Model) nextSearchPosition(after buffer.Position) buffer.Position {
	snap := m.doc.Snapshot()
	line, col := after.Line, after.Col
	text, _ := snap.LineContent(line)
	if m.usesLineStops() || col >= len(text) {
		if line == snap.LineCount()-1 {
			return buffer.Position{}
		}
		return buffer.Position{Line: line + 1}
	}
	_, size := utf8.DecodeRuneInString(text[col:])
	return buffer.Position{Line: line, Col: col + size}
}

func isEmptyAt(r buffer.Range, p buffer.Position) bool { return r.IsEmpty() && r.Start == p }

// moveToPrev moves to the match before a position.
func (m *Model) moveToPrev(before buffer.Position, isRecursed bool) {
	if !m.state.CanNavigateBack() {
		// beyond the first match: go back to the first one instead of doing nothing
		if next, ok := m.dec.matchAfterPosition(before); ok {
			m.setCurrentFindMatch(next)
		}
		return
	}
	if m.dec.count() < MatchesLimit {
		prev, ok := m.dec.matchBeforePosition(before)
		if ok && isEmptyAt(prev, before) {
			before = m.prevSearchPosition(before)
			prev, ok = m.dec.matchBeforePosition(before)
		}
		if ok {
			m.setCurrentFindMatch(prev)
		}
		return
	}
	if m.cannotFind() {
		return
	}
	var scope *buffer.Range
	if s, ok := m.dec.findScope(); ok {
		scope = &s
	}
	searchRange := m.searchRange(scope)
	if searchRange.End.IsBefore(before) {
		before = searchRange.End
	}
	if before.IsBefore(searchRange.Start) {
		before = searchRange.End
	}
	snap := m.doc.Snapshot()
	position := before
	prev, ok := m.currentQuery().FindPrevious(snap, position, false)
	if ok && isEmptyAt(prev.Range, position) {
		// stuck at this position
		position = m.prevSearchPosition(position)
		prev, ok = m.currentQuery().FindPrevious(snap, position, false)
	}
	if !ok {
		return // there is one match and the selection is on it
	}
	if !isRecursed && !searchRange.ContainsRange(prev.Range) {
		m.moveToPrev(prev.Range.Start, true)
		return
	}
	m.setCurrentFindMatch(prev.Range)
}

// MoveToPrevMatch selects the match before the selection.
func (m *Model) MoveToPrevMatch() { m.moveToPrev(m.host.Selection().Start(), false) }

// moveToNext moves to the match after a position.
func (m *Model) moveToNext(after buffer.Position) {
	if !m.state.CanNavigateForward() {
		// beyond the last match: go back to the last one instead of doing nothing
		if prev, ok := m.dec.matchBeforePosition(after); ok {
			m.setCurrentFindMatch(prev)
		}
		return
	}
	if m.dec.count() < MatchesLimit {
		next, ok := m.dec.matchAfterPosition(after)
		if ok && isEmptyAt(next, after) {
			after = m.nextSearchPosition(after)
			next, ok = m.dec.matchAfterPosition(after)
		}
		if ok {
			m.setCurrentFindMatch(next)
		}
		return
	}
	if next, ok := m.nextMatch(after, false, true, false); ok {
		m.setCurrentFindMatch(next.Range)
	}
}

// nextMatch searches for the match after a position, within the search scope.
func (m *Model) nextMatch(after buffer.Position, capture, forceMove, isRecursed bool) (Match, bool) {
	if m.cannotFind() {
		return Match{}, false
	}
	var scope *buffer.Range
	if s, ok := m.dec.findScope(); ok {
		scope = &s
	}
	searchRange := m.searchRange(scope)
	if searchRange.End.IsBefore(after) {
		after = searchRange.Start
	}
	if after.IsBefore(searchRange.Start) {
		after = searchRange.Start
	}
	snap := m.doc.Snapshot()
	position := after
	next, ok := m.currentQuery().FindNext(snap, position, capture)
	if forceMove && ok && isEmptyAt(next.Range, position) {
		// stuck at this position
		position = m.nextSearchPosition(position)
		next, ok = m.currentQuery().FindNext(snap, position, capture)
	}
	if !ok {
		return Match{}, false // there is one match and the selection is on it
	}
	if !isRecursed && !searchRange.ContainsRange(next.Range) {
		return m.nextMatch(next.Range.End, capture, forceMove, true)
	}
	return next, true
}

// MoveToNextMatch selects the match after the selection.
func (m *Model) MoveToNextMatch() { m.moveToNext(m.host.Selection().End()) }

// MoveToMatch selects the match with an index (from 0).
func (m *Model) MoveToMatch(index int) {
	if r, ok := m.dec.rangeAt(index); ok {
		m.setCurrentFindMatch(r)
	}
}

// replacePattern is the pattern of the replace string.
func (m *Model) replacePattern() ReplacePattern {
	if m.state.IsRegex() {
		return ParseReplaceString(m.state.ReplaceString())
	}
	return StaticReplacePattern(m.state.ReplaceString())
}

// Replace replaces the match the selection is on, or, when it is on none, selects the next one.
// It fails when the document cannot be edited.
func (m *Model) Replace() error {
	if !m.hasMatches() {
		return nil
	}
	pattern := m.replacePattern()
	selection := m.host.Selection()
	next, ok := m.nextMatch(selection.Start(), true, false, false)
	if !ok {
		return nil
	}
	if selection.Range() != next.Range {
		m.dec.setStartPosition(selection.Active)
		m.setCurrentFindMatch(next.Range)
		return nil
	}
	// the selection is on a match: replace it
	text := pattern.Build(next.Groups, m.state.PreserveCase())
	err := m.execute([]document.Edit{{Range: selection.Range(), Text: text}}, func(inverse []document.Edit) []buffer.Selection {
		return []buffer.Selection{buffer.NewCursor(inverse[0].Range.End)}
	})
	if err != nil {
		return err
	}
	m.dec.setStartPosition(buffer.Position{Line: selection.Start().Line, Col: selection.Start().Col + len(text)})
	m.research(true, Opt[[]buffer.Range]{})
	return nil
}

// findMatches finds the matches in the search scopes (or all the text).
func (m *Model) findMatches(scopes []buffer.Range, capture bool, limit int) []Match {
	ranges := make([]buffer.Range, 0, max(len(scopes), 1))
	if len(scopes) == 0 {
		ranges = append(ranges, m.searchRange(nil))
	}
	ranges = append(ranges, scopes...)
	return m.currentQuery().FindMatchesIn(m.doc.Snapshot(), ranges, capture, limit)
}

// ReplaceAll replaces every match. It fails when the document cannot be edited.
func (m *Model) ReplaceAll() error {
	if !m.hasMatches() {
		return nil
	}
	var err error
	if scopes := m.dec.findScopes(); scopes == nil && m.state.MatchesCount() >= MatchesLimit {
		err = m.largeReplaceAll()
	} else {
		err = m.regularReplaceAll(scopes)
	}
	m.research(false, Opt[[]buffer.Range]{})
	return err
}

// regularReplaceAll replaces every match, even those past the limit of the marked ones, with one
// edit for each (those that touch are one).
func (m *Model) regularReplaceAll(scopes []buffer.Range) error {
	pattern := m.replacePattern()
	capture := pattern.HasReplacementPatterns() || m.state.PreserveCase()
	matches := m.findMatches(scopes, capture, 1<<30)
	edits := make([]document.Edit, len(matches))
	for i, match := range matches {
		edits[i] = document.Edit{Range: match.Range, Text: pattern.Build(match.Groups, m.state.PreserveCase())}
	}
	return m.replaceEdits(edits)
}

// replaceEdits applies the edits of a replace all in order of position, joining those that touch,
// and keeps the selection of the editor where it was in the text.
func (m *Model) replaceEdits(edits []document.Edit) error {
	if len(edits) == 0 {
		return nil
	}
	slices.SortStableFunc(edits, func(a, b document.Edit) int { return compareRangesUsingStarts(a.Range, b.Range) })
	merged := []document.Edit{edits[0]}
	for _, e := range edits[1:] {
		last := &merged[len(merged)-1]
		if last.Range.End == e.Range.Start {
			last.Range.End = e.Range.End
			last.Text += e.Text
			continue
		}
		merged = append(merged, e)
	}
	return m.executeTracked(merged)
}

// largeReplaceAll replaces every match of a search of a document with more matches than are marked:
// the whole text is replaced at once, with ^ and $ matching at every line.
func (m *Model) largeReplaceAll() error {
	query := m.currentQuery()
	if query == nil {
		return nil
	}
	re, err := regex.Compile(query.re.Source(), regex.Options{
		Syntax: regex.ECMAScript, IgnoreCase: !m.state.MatchCase(), Multiline: true, Unicode: true, Timeout: searchTimeout,
	})
	if err != nil {
		return err
	}
	forced := *query
	forced.re = re
	snap := m.doc.Snapshot()
	full := buffer.Range{End: snap.PositionAt(snap.Len())}
	text := snap.String()
	rt := newRuneText(text)
	pattern := m.replacePattern()
	preserve := m.state.PreserveCase()
	var out []byte
	last := 0
	s := forced.searcher(rt)
	s.reset(0)
	for {
		match, start, length, ok := s.next()
		if !ok {
			break
		}
		out = append(out, text[rt.byteAt(last):rt.byteAt(start)]...)
		var groups []string
		if pattern.HasReplacementPatterns() || preserve {
			groups = rt.groups(match)
		}
		out = append(out, pattern.Build(groups, preserve)...)
		last = start + length
	}
	out = append(out, text[rt.byteAt(last):]...)
	return m.executeTracked([]document.Edit{{Range: full, Text: string(out)}})
}

// SelectAllMatches selects every match; the match that is the selection stays the primary one.
func (m *Model) SelectAllMatches() {
	if !m.hasMatches() {
		return
	}
	matches := m.findMatches(m.dec.findScopes(), false, 1<<30)
	selections := make([]buffer.Selection, len(matches))
	for i, match := range matches {
		selections[i] = buffer.Selection{Anchor: match.Range.Start, Active: match.Range.End}
	}
	current := m.host.Selection()
	for i, s := range selections {
		if s.Range() == current.Range() {
			selections = append([]buffer.Selection{current}, append(selections[:i:i], selections[i+1:]...)...)
			break
		}
	}
	m.host.SetSelections(selections)
}

// execute applies the edits of a command as one undo step of the document, without the model
// researching because of them, and moves the selection to what after says.
func (m *Model) execute(edits []document.Edit, after func(inverse []document.Edit) []buffer.Selection) error {
	m.ignoreContentChanged = true
	defer func() { m.ignoreContentChanged = false }()
	res, err := m.doc.Edit(edits, document.EditOptions{
		Kind: document.KindOther, Before: []buffer.Selection{m.host.Selection()}, After: after,
	})
	if err != nil {
		return err
	}
	if len(res.Selections) > 0 {
		m.host.SetSelections(res.Selections)
	}
	return nil
}

// executeTracked applies edits as one undo step and keeps the selection of the editor in the
// text it was in: it is tracked as a decoration through the edits.
func (m *Model) executeTracked(edits []document.Edit) error {
	selection := m.host.Selection()
	stickiness := document.AlwaysGrowsWhenTypingAtEdges
	if selection.IsEmpty() {
		stickiness = document.GrowsOnlyWhenTypingAfter
	}
	id := m.doc.AddDecoration(m.owner, selection.Range(), document.DecorationOptions{Stickiness: stickiness})
	defer m.doc.RemoveDecorations(id)
	tracked := func([]document.Edit) []buffer.Selection {
		d, ok := m.doc.Decoration(id)
		if !ok {
			return nil
		}
		if selection.Anchor.IsBeforeOrEqual(selection.Active) {
			return []buffer.Selection{{Anchor: d.Range.Start, Active: d.Range.End}}
		}
		return []buffer.Selection{{Anchor: d.Range.End, Active: d.Range.Start}}
	}
	return m.execute(edits, tracked)
}
