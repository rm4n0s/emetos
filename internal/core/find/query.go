package find

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: src/vs/editor/common/model/textModelSearch.ts (MIT)

// LimitFindCount is the number of matches FindMatches returns when it is not told another limit
// (VS Code's LIMIT_FIND_COUNT).
const LimitFindCount = 999

// The multiline search reads the text in windows of lines, so that a search of a document of
// millions of lines does not turn all of it into one string: a window of multilineWindow lines
// and multilineLookahead more, which a match that starts in the window may run into. A document
// of up to multilineWindow+multilineLookahead lines is one window, as in VS Code.
const (
	multilineWindow    = 5000
	multilineLookahead = 500
)

// searchTimeout is the longest a regular expression may run on a line or a window before the
// search gives up on it.
const searchTimeout = time.Second

// Params say what to search for (VS Code's SearchParams).
type Params struct {
	// Search is the text, or the pattern when IsRegex is set; an empty one finds nothing.
	Search string
	// IsRegex says that Search is an ECMAScript regular expression.
	IsRegex bool
	// MatchCase makes the search case sensitive.
	MatchCase bool
	// WordSeparators, when it is not empty, makes the search find whole words only: a match
	// must start and end next to one of these characters, a space, a line break, or the end of
	// the text (editor.wordSeparators).
	WordSeparators string
}

// Match is a match of a search.
type Match struct {
	// Range is where the match is.
	Range buffer.Range
	// Groups are the text of the match and then of each capturing group (an unmatched group is
	// ""), when the search was asked to capture them; nil otherwise.
	Groups []string
}

// Query is a search ready to run: the parameters, the compiled expression and what the search of
// a plain text needs. It is made by Params.Query and is safe for concurrent use.
type Query struct {
	params    Params
	re        *regex.Regexp
	multiline bool
	// simple is the text to look for with a plain substring search, or "" when the search needs
	// the expression.
	simple string
	words  *edit.WordClassifier
}

// Query parses the parameters (VS Code's parseSearchRequest). It returns nil when there is
// nothing to search for: an empty search, or a pattern that does not compile.
func (p Params) Query() *Query {
	if p.Search == "" {
		return nil
	}
	multiline := strings.Contains(p.Search, "\n")
	if p.IsRegex {
		multiline = IsMultilineRegexSource(p.Search)
	}
	pattern := p.Search
	if !p.IsRegex {
		pattern = escapeRegExp(pattern)
	}
	re, err := regex.Compile(pattern, regex.Options{
		Syntax: regex.ECMAScript, IgnoreCase: !p.MatchCase, Multiline: multiline, Unicode: true, Timeout: searchTimeout,
	})
	if err != nil {
		return nil
	}
	q := &Query{params: p, re: re, multiline: multiline}
	simple := !p.IsRegex && !multiline
	if simple && strings.ToLower(p.Search) != strings.ToUpper(p.Search) {
		simple = p.MatchCase // casing might make a difference
	}
	if simple {
		q.simple = p.Search
	}
	if p.WordSeparators != "" {
		q.words = edit.NewWordClassifier(p.WordSeparators)
	}
	return q
}

// Multiline says that the search can match across lines.
func (q *Query) Multiline() bool { return q.multiline }

// escapeRegExp escapes the characters that mean something in a regular expression (VS Code's
// escapeRegExpCharacters).
func escapeRegExp(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`\{}*+?|^$.[]()`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// IsMultilineRegexSource says whether a regular expression can match a line break: it has one,
// or \n, \r or \W.
func IsMultilineRegexSource(source string) bool {
	for i := 0; i < len(source); i++ {
		switch source[i] {
		case '\n':
			return true
		case '\\':
			i++
			if i >= len(source) {
				return false
			}
			if c := source[i]; c == 'n' || c == 'r' || c == 'W' {
				return true
			}
		}
	}
	return false
}

// runeText is a text as runes, which the expressions search, and the byte offset of each rune.
type runeText struct {
	text  string
	runes []rune
	// offs[i] is the byte offset of rune i (and offs[len] the length of the text); nil when every
	// rune is one byte.
	offs []int
}

func newRuneText(s string) runeText {
	t := runeText{text: s, runes: []rune(s)}
	if len(t.runes) != len(s) {
		t.offs = make([]int, 0, len(t.runes)+1)
		for i := range s {
			t.offs = append(t.offs, i)
		}
		t.offs = append(t.offs, len(s))
	}
	return t
}

// byteAt is the byte offset of a rune index.
func (t runeText) byteAt(r int) int {
	if t.offs == nil {
		return r
	}
	return t.offs[r]
}

// runeAt is the rune index of a byte offset, which must be at the start of a rune.
func (t runeText) runeAt(b int) int {
	if t.offs == nil {
		return b
	}
	return utf8.RuneCountInString(t.text[:b])
}

// groups are the strings of a match.
func (t runeText) groups(m regex.Match) []string {
	out := make([]string, len(m.Groups))
	for i, g := range m.Groups {
		if g.Start >= 0 {
			out[i] = string(t.runes[g.Start:g.End])
		}
	}
	return out
}

// searcher goes through the matches of an expression in a text (VS Code's Searcher): it skips
// the matches that are not whole words, and steps over an empty match.
type searcher struct {
	re        *regex.Regexp
	words     *edit.WordClassifier
	text      runeText
	last      int
	prevStart int
	prevLen   int
}

func (q *Query) searcher(text runeText) *searcher {
	return &searcher{re: q.re, words: q.words, text: text, prevStart: -1}
}

// reset makes the next search start at a rune index.
func (s *searcher) reset(last int) {
	s.last = last
	s.prevStart, s.prevLen = -1, 0
}

// next is the next match: its groups, the rune index where it starts and its length in runes.
func (s *searcher) next() (m regex.Match, start, length int, ok bool) {
	n := len(s.text.runes)
	for {
		if s.prevStart+s.prevLen == n {
			return regex.Match{}, 0, 0, false // reached the end of the text
		}
		found, has, err := s.re.Find(s.text.runes, s.last)
		if err != nil || !has {
			return regex.Match{}, 0, 0, false
		}
		start, length = found.Groups[0].Start, found.Groups[0].End-found.Groups[0].Start
		s.last = found.Groups[0].End
		if start == s.prevStart && length == s.prevLen {
			if length == 0 {
				// An empty match does not move the search: step over one character.
				s.last++
				continue
			}
			return regex.Match{}, 0, 0, false // the same range twice
		}
		s.prevStart, s.prevLen = start, length
		if s.words == nil || isValidMatch(s.words, s.text.runes, start, length) {
			return found, start, length, true
		}
	}
}

// leftIsWordBoundary says whether a match starts at a word boundary.
func leftIsWordBoundary(w *edit.WordClassifier, text []rune, start, length int) bool {
	if start == 0 {
		return true
	}
	before := text[start-1]
	if w.Class(before) != edit.ClassRegular || before == '\r' || before == '\n' {
		return true
	}
	return length > 0 && w.Class(text[start]) != edit.ClassRegular
}

// rightIsWordBoundary says whether a match ends at a word boundary.
func rightIsWordBoundary(w *edit.WordClassifier, text []rune, start, length int) bool {
	if start+length == len(text) {
		return true
	}
	after := text[start+length]
	if w.Class(after) != edit.ClassRegular || after == '\r' || after == '\n' {
		return true
	}
	return length > 0 && w.Class(text[start+length-1]) != edit.ClassRegular
}

// isValidMatch says whether a match is a whole word, for a search with word separators.
func isValidMatch(w *edit.WordClassifier, text []rune, start, length int) bool {
	return leftIsWordBoundary(w, text, start, length) && rightIsWordBoundary(w, text, start, length)
}

// isValidByteMatch is isValidMatch for a match of a plain text search, in bytes.
func isValidByteMatch(w *edit.WordClassifier, text string, start, length int) bool {
	if start > 0 {
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		first, _ := utf8.DecodeRuneInString(text[start:])
		if w.Class(before) == edit.ClassRegular && before != '\r' && before != '\n' &&
			(length == 0 || w.Class(first) == edit.ClassRegular) {
			return false
		}
	}
	if end := start + length; end < len(text) {
		after, _ := utf8.DecodeRuneInString(text[end:])
		last, _ := utf8.DecodeLastRuneInString(text[start:end])
		if w.Class(after) == edit.ClassRegular && after != '\r' && after != '\n' &&
			(length == 0 || w.Class(last) == edit.ClassRegular) {
			return false
		}
	}
	return true
}

// FindMatches finds the matches in a range of a text, in order, up to limit of them (the
// default LimitFindCount when it is 0 or less).
//
// Ported from VS Code: src/vs/editor/common/model/textModelSearch.ts (TextModelSearch.findMatches)
func (q *Query) FindMatches(snap buffer.Snapshot, searchRange buffer.Range, capture bool, limit int) []Match {
	if q == nil {
		return nil
	}
	if limit <= 0 {
		limit = LimitFindCount
	}
	searchRange = snap.ValidateRange(searchRange)
	if q.multiline {
		return q.multilineMatches(snap, searchRange, capture, limit)
	}
	return q.lineMatches(snap, searchRange, capture, limit)
}

// FindMatchesIn finds the matches in several ranges (the search scopes of the find widget): the
// ranges are put in order and the ones that touch are joined, and each range gives up to limit
// matches.
//
// Ported from VS Code: src/vs/editor/common/model/textModel.ts (findMatches)
func (q *Query) FindMatchesIn(snap buffer.Snapshot, ranges []buffer.Range, capture bool, limit int) []Match {
	if q == nil {
		return nil
	}
	if len(ranges) == 0 {
		ranges = []buffer.Range{{End: snap.PositionAt(snap.Len())}}
	}
	valid := make([]buffer.Range, len(ranges))
	for i, r := range ranges {
		valid[i] = snap.ValidateRange(r)
	}
	var out []Match
	for _, r := range joinRanges(valid) {
		out = append(out, q.FindMatches(snap, r, capture, limit)...)
	}
	return out
}

// joinRanges puts ranges in order of their start and joins those that touch or overlap.
func joinRanges(ranges []buffer.Range) []buffer.Range {
	sorted := append([]buffer.Range(nil), ranges...)
	for i := 1; i < len(sorted); i++ { // insertion sort: there are a few
		for j := i; j > 0 && sorted[j].Start.IsBefore(sorted[j-1].Start); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	var out []buffer.Range
	for _, r := range sorted {
		if n := len(out); n > 0 && !out[n-1].End.IsBefore(r.Start) {
			out[n-1] = out[n-1].Union(r)
			continue
		}
		out = append(out, r)
	}
	return out
}

// lineMatches searches line by line (VS Code's _doFindMatchesLineByLine).
func (q *Query) lineMatches(snap buffer.Snapshot, r buffer.Range, capture bool, limit int) []Match {
	var out []Match
	line := func(n, from, to int) bool {
		text, err := snap.LineContent(n)
		if err != nil {
			return true
		}
		if to < 0 || to > len(text) {
			to = len(text)
		}
		out = q.matchesInLine(text[from:to], n, from, out, capture, limit)
		return len(out) < limit
	}
	if r.Start.Line == r.End.Line {
		line(r.Start.Line, r.Start.Col, r.End.Col)
		return out
	}
	if !line(r.Start.Line, r.Start.Col, -1) {
		return out
	}
	for n := r.Start.Line + 1; n < r.End.Line; n++ {
		if !line(n, 0, -1) {
			return out
		}
	}
	line(r.End.Line, 0, r.End.Col)
	return out
}

// matchesInLine appends the matches in the text of a line (or of a part of it that starts at the
// byte column delta) to out, up to limit matches in all.
func (q *Query) matchesInLine(text string, lineNo, delta int, out []Match, capture bool, limit int) []Match {
	if !capture && q.simple != "" {
		for from := 0; ; {
			i := strings.Index(text[from:], q.simple)
			if i < 0 {
				return out
			}
			start := from + i
			from = start + len(q.simple)
			if q.words != nil && !isValidByteMatch(q.words, text, start, len(q.simple)) {
				continue
			}
			out = append(out, Match{Range: lineRange(lineNo, delta+start, delta+from)})
			if len(out) >= limit {
				return out
			}
		}
	}
	rt := newRuneText(text)
	s := q.searcher(rt)
	s.reset(0)
	for {
		m, start, length, ok := s.next()
		if !ok {
			return out
		}
		match := Match{Range: lineRange(lineNo, delta+rt.byteAt(start), delta+rt.byteAt(start+length))}
		if capture {
			match.Groups = rt.groups(m)
		}
		out = append(out, match)
		if len(out) >= limit {
			return out
		}
	}
}

func lineRange(line, from, to int) buffer.Range {
	return buffer.Range{Start: buffer.Position{Line: line, Col: from}, End: buffer.Position{Line: line, Col: to}}
}

// multilineMatches searches the lines of a range joined by line feeds, in windows of lines.
func (q *Query) multilineMatches(snap buffer.Snapshot, r buffer.Range, capture bool, limit int) []Match {
	var out []Match
	from, col := r.Start, 0
	for len(out) < limit {
		windowEnd, last := q.windowEnd(snap, from, r.End)
		owned := ownedLines(from, windowEnd)
		out = append(out, q.searchWindow(snap, buffer.Range{Start: from, End: windowEnd}, capture, limit-len(out), col, func(m Match) bool {
			// a match that starts after the lines of the window is left to the next one
			return last || m.Range.Start.Line <= owned
		})...)
		if last {
			break
		}
		// the next window starts at the line after the window's own lines, or where the last match
		// ended when it went further
		from, col = buffer.Position{Line: owned + 1}, 0
		if n := len(out); n > 0 && out[n-1].Range.End.Line >= from.Line {
			end := out[n-1].Range.End
			from, col = buffer.Position{Line: end.Line}, end.Col
		}
	}
	return out
}

// ownedLines is the last line whose matches belong to the window that starts at from and ends at
// windowEnd: the lines after it are the window's lookahead.
func ownedLines(from, windowEnd buffer.Position) int {
	return min(from.Line+multilineWindow-1, windowEnd.Line)
}

// windowEnd is where the window that starts at from ends, and whether it reaches the end of the
// range (it is the last one).
func (q *Query) windowEnd(snap buffer.Snapshot, from, end buffer.Position) (buffer.Position, bool) {
	limit := from.Line + multilineWindow + multilineLookahead - 1
	if limit >= end.Line {
		return end, true
	}
	return endOfLine(snap, limit), false
}

// searchWindow finds the matches in the text of a range, which is the lines of it joined by line
// feeds; the search starts at a byte offset of that text (0 for its start). It stops at the first
// match that keep, when it is not nil, turns down.
func (q *Query) searchWindow(snap buffer.Snapshot, r buffer.Range, capture bool, limit, startOffset int, keep func(Match) bool) []Match {
	base := snap.OffsetAt(r.Start)
	rt := newRuneText(snap.Text(r))
	s := q.searcher(rt)
	s.reset(rt.runeAt(min(startOffset, len(rt.text))))
	var out []Match
	for len(out) < limit {
		m, start, length, ok := s.next()
		if !ok {
			break
		}
		match := Match{Range: buffer.Range{
			Start: snap.PositionAt(base + rt.byteAt(start)),
			End:   snap.PositionAt(base + rt.byteAt(start+length)),
		}}
		if capture {
			match.Groups = rt.groups(m)
		}
		if keep != nil && !keep(match) {
			break
		}
		out = append(out, match)
	}
	return out
}

// FindNext finds the first match at or after a position, going on from the top of the text when
// there is none after it. A match that starts before the position on its line is not found
// (except by the wrap-around).
//
// Ported from VS Code: src/vs/editor/common/model/textModelSearch.ts (TextModelSearch.findNextMatch)
// and src/vs/editor/common/model/textModel.ts (findNextMatch)
func (q *Query) FindNext(snap buffer.Snapshot, start buffer.Position, capture bool) (Match, bool) {
	if q == nil {
		return Match{}, false
	}
	start = snap.ValidatePosition(start)
	if q.multiline {
		return q.nextMultiline(snap, start, capture)
	}
	if q.simple != "" {
		last := snap.LineCount() - 1
		lastText, _ := snap.LineContent(last)
		if ms := q.lineMatches(snap, buffer.Range{Start: start, End: buffer.Position{Line: last, Col: len(lastText)}}, capture, 1); len(ms) > 0 {
			return ms[0], true
		}
		text, _ := snap.LineContent(start.Line)
		if ms := q.lineMatches(snap, buffer.Range{End: buffer.Position{Line: start.Line, Col: len(text)}}, capture, 1); len(ms) > 0 {
			return ms[0], true
		}
		return Match{}, false
	}
	return q.nextLineByLine(snap, start, capture)
}

// nextLineByLine is VS Code's _doFindNextMatchLineByLine: the first match from the position to
// the end of its line, then in the lines after it, wrapping round to the lines before it.
func (q *Query) nextLineByLine(snap buffer.Snapshot, start buffer.Position, capture bool) (Match, bool) {
	count := snap.LineCount()
	first := func(n, from int) (Match, bool) {
		text, err := snap.LineContent(n)
		if err != nil {
			return Match{}, false
		}
		rt := newRuneText(text)
		s := q.searcher(rt)
		s.reset(rt.runeAt(min(from, len(text))))
		m, at, length, ok := s.next()
		if !ok {
			return Match{}, false
		}
		match := Match{Range: lineRange(n, rt.byteAt(at), rt.byteAt(at+length))}
		if capture {
			match.Groups = rt.groups(m)
		}
		return match, true
	}
	if m, ok := first(start.Line, start.Col); ok {
		return m, true
	}
	for i := 1; i <= count; i++ {
		if m, ok := first((start.Line+i)%count, 0); ok {
			return m, true
		}
	}
	return Match{}, false
}

// nextMultiline is VS Code's _doFindNextMatchMultiline over windows.
func (q *Query) nextMultiline(snap buffer.Snapshot, start buffer.Position, capture bool) (Match, bool) {
	end := snap.PositionAt(snap.Len())
	from, col := buffer.Position{Line: start.Line}, start.Col
	for {
		windowEnd, last := q.windowEnd(snap, from, end)
		owned := ownedLines(from, windowEnd)
		ms := q.searchWindow(snap, buffer.Range{Start: from, End: windowEnd}, capture, 1, col, func(m Match) bool {
			return last || m.Range.Start.Line <= owned
		})
		if len(ms) > 0 {
			return ms[0], true
		}
		if last {
			break
		}
		from, col = buffer.Position{Line: owned + 1}, 0
	}
	if start.Line != 0 || start.Col != 0 {
		return q.nextMultiline(snap, buffer.Position{}, capture) // try again from the top
	}
	return Match{}, false
}

// FindPrevious finds the last match that ends at or before a position, going on from the end of
// the text when there is none before it.
//
// Ported from VS Code: src/vs/editor/common/model/textModelSearch.ts (TextModelSearch.findPreviousMatch)
func (q *Query) FindPrevious(snap buffer.Snapshot, start buffer.Position, capture bool) (Match, bool) {
	if q == nil {
		return Match{}, false
	}
	start = snap.ValidatePosition(start)
	if q.multiline {
		return q.previousMultiline(snap, start, capture)
	}
	return q.previousLineByLine(snap, start, capture)
}

// lastInLine is the last match in the text of a line.
func (q *Query) lastInLine(text string, lineNo int, capture bool) (Match, bool) {
	rt := newRuneText(text)
	s := q.searcher(rt)
	s.reset(0)
	var best Match
	found := false
	for {
		m, at, length, ok := s.next()
		if !ok {
			return best, found
		}
		best = Match{Range: lineRange(lineNo, rt.byteAt(at), rt.byteAt(at+length))}
		if capture {
			best.Groups = rt.groups(m)
		}
		found = true
	}
}

// previousLineByLine is VS Code's _doFindPreviousMatchLineByLine.
func (q *Query) previousLineByLine(snap buffer.Snapshot, start buffer.Position, capture bool) (Match, bool) {
	count := snap.LineCount()
	text, _ := snap.LineContent(start.Line)
	if m, ok := q.lastInLine(text[:min(start.Col, len(text))], start.Line, capture); ok {
		return m, true
	}
	for i := 1; i <= count; i++ {
		n := (count + start.Line - i) % count
		text, err := snap.LineContent(n)
		if err != nil {
			continue
		}
		if m, ok := q.lastInLine(text, n, capture); ok {
			return m, true
		}
	}
	return Match{}, false
}

// previousMultiline is VS Code's _doFindPreviousMatchMultiline over windows: the last match in the
// text before the position, found in the windows of lines going backwards.
func (q *Query) previousMultiline(snap buffer.Snapshot, start buffer.Position, capture bool) (Match, bool) {
	// hi is the first line whose matches belong to a window that is still to be searched
	for hi := start.Line + 1; hi > 0; {
		lo := max(hi-multilineWindow, 0)
		windowEnd := buffer.MinPosition(start, endOfLine(snap, min(hi-1+multilineLookahead, snap.LineCount()-1)))
		from := buffer.Position{Line: lo}
		ms := q.searchWindow(snap, buffer.Range{Start: from, End: windowEnd}, capture, 10*LimitFindCount, 0, nil)
		for i := len(ms) - 1; i >= 0; i-- {
			if ms[i].Range.Start.Line < hi && !start.IsBefore(ms[i].Range.End) {
				return ms[i], true
			}
		}
		hi = lo
	}
	last := snap.PositionAt(snap.Len())
	if start != last {
		return q.previousMultiline(snap, last, capture) // try again with all the text
	}
	return Match{}, false
}

// endOfLine is the position at the end of a line.
func endOfLine(snap buffer.Snapshot, line int) buffer.Position {
	text, _ := snap.LineContent(line)
	return buffer.Position{Line: line, Col: len(text)}
}
