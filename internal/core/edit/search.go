package edit

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// literalSearch looks for a piece of text in a Model, as the multi-cursor commands
// do (VS Code's TextModelSearch with a plain search string). The find engine of
// the find widget, with regular expressions, comes with the find steps.
type literalSearch struct {
	model     Model
	pieces    []string // the search text split at its line breaks
	matchCase bool
	words     *WordClassifier // not nil: only whole words match
}

func newLiteralSearch(m Model, text string, matchCase bool, words *WordClassifier) *literalSearch {
	return &literalSearch{model: m, pieces: strings.Split(text, "\n"), matchCase: matchCase, words: words}
}

// prefixLen is the length in s of the part that equals p (ignoring case unless the
// search is case sensitive), or -1 when s does not start with p.
func (s *literalSearch) prefixLen(str, p string) int {
	if s.matchCase {
		if strings.HasPrefix(str, p) {
			return len(p)
		}
		return -1
	}
	i := 0
	for _, pr := range p {
		if i >= len(str) {
			return -1
		}
		sr, w := utf8.DecodeRuneInString(str[i:])
		if sr != pr && !sameFold(sr, pr) {
			return -1
		}
		i += w
	}
	return i
}

func sameFold(a, b rune) bool {
	for r := unicode.SimpleFold(a); r != a; r = unicode.SimpleFold(r) {
		if r == b {
			return true
		}
	}
	return false
}

// matchAt reports the range of a match that starts at a position. lineLimit is
// the length of the text of the start line that counts (a search backwards cuts
// the line at the place it starts from).
func (s *literalSearch) matchAt(line, col, lineLimit int) (buffer.Range, bool) {
	text := s.model.LineContent(line)
	if len(s.pieces) == 1 {
		if lineLimit < len(text) {
			text = text[:lineLimit]
		}
		n := s.prefixLen(text[col:], s.pieces[0])
		if n < 0 {
			return buffer.Range{}, false
		}
		r := buffer.Range{Start: posAt(line, col), End: posAt(line, col+n)}
		return r, s.isWholeWord(text, col, col+n)
	}
	// The first piece ends the line, the middle ones are whole lines, the last starts one.
	first := s.pieces[0]
	if lineLimit < len(text) {
		return buffer.Range{}, false
	}
	n := s.prefixLen(text[col:], first)
	if n < 0 || col+n != len(text) {
		return buffer.Range{}, false
	}
	if s.words != nil && !s.boundaryLeft(text, col, len(text)) {
		return buffer.Range{}, false
	}
	last := len(s.pieces) - 1
	for i := 1; i < last; i++ {
		l := line + i
		if l >= s.model.LineCount() {
			return buffer.Range{}, false
		}
		mid := s.model.LineContent(l)
		if k := s.prefixLen(mid, s.pieces[i]); k < 0 || k != len(mid) {
			return buffer.Range{}, false
		}
	}
	l := line + last
	if l >= s.model.LineCount() {
		return buffer.Range{}, false
	}
	end := s.model.LineContent(l)
	k := s.prefixLen(end, s.pieces[last])
	if k < 0 {
		return buffer.Range{}, false
	}
	if s.words != nil && !s.boundaryRight(end, 0, k) {
		return buffer.Range{}, false
	}
	return buffer.Range{Start: posAt(line, col), End: posAt(l, k)}, true
}

// isWholeWord reports whether text[start:end] is not in the middle of a word, when
// only whole words match (VS Code's isValidMatch).
func (s *literalSearch) isWholeWord(text string, start, end int) bool {
	return s.words == nil || (s.boundaryLeft(text, start, end) && s.boundaryRight(text, start, end))
}

func (s *literalSearch) boundaryLeft(text string, start, end int) bool {
	if start == 0 {
		return true
	}
	before, _ := utf8.DecodeLastRuneInString(text[:start])
	if s.words.Class(before) != ClassRegular {
		return true
	}
	if end > start {
		first, _ := utf8.DecodeRuneInString(text[start:])
		return s.words.Class(first) != ClassRegular
	}
	return false
}

func (s *literalSearch) boundaryRight(text string, start, end int) bool {
	if end == len(text) {
		return true
	}
	after, _ := utf8.DecodeRuneInString(text[end:])
	if s.words.Class(after) != ClassRegular {
		return true
	}
	if end > start {
		last, _ := utf8.DecodeLastRuneInString(text[start:end])
		return s.words.Class(last) != ClassRegular
	}
	return false
}

// firstInLine is the first match on a line that starts at or after col.
func (s *literalSearch) firstInLine(line, col int) (buffer.Range, bool) {
	text := s.model.LineContent(line)
	for c := col; c <= len(text); c++ {
		if c < len(text) && !utf8.RuneStart(text[c]) {
			continue
		}
		if r, ok := s.matchAt(line, c, len(text)); ok {
			return r, true
		}
		if s.pieces[0] == "" && len(s.pieces) == 1 {
			break
		}
	}
	return buffer.Range{}, false
}

// lastInLine is the last match on a line, looking only at the first limit bytes.
func (s *literalSearch) lastInLine(line, limit int) (buffer.Range, bool) {
	var best buffer.Range
	found := false
	text := s.model.LineContent(line)
	end := min(limit, len(text))
	for c := 0; c <= end; {
		if r, ok := s.matchAt(line, c, limit); ok {
			best, found = r, true
			// Matches do not overlap: the next one starts where this one ends on the line.
			if r.End.Line == line && r.End.Col > c {
				c = r.End.Col
				continue
			}
		}
		if c < len(text) {
			_, w := utf8.DecodeRuneInString(text[c:])
			c += w
		} else {
			c++
		}
	}
	return best, found
}

// Next is the first match at or after a position, going on from the start of the
// text when there is none after it.
func (s *literalSearch) Next(from buffer.Position) (buffer.Range, bool) {
	lines := s.model.LineCount()
	if r, ok := s.firstInLine(from.Line, from.Col); ok {
		return r, true
	}
	for i := 1; i <= lines; i++ {
		line := (from.Line + i) % lines
		if r, ok := s.firstInLine(line, 0); ok {
			return r, true
		}
	}
	return buffer.Range{}, false
}

// Previous is the last match that ends at or before a position, going on from the
// end of the text when there is none before it.
func (s *literalSearch) Previous(from buffer.Position) (buffer.Range, bool) {
	lines := s.model.LineCount()
	if r, ok := s.lastInLine(from.Line, from.Col); ok {
		return r, true
	}
	for i := 1; i <= lines; i++ {
		line := ((from.Line-i)%lines + lines) % lines
		if r, ok := s.lastInLine(line, len(s.model.LineContent(line))); ok {
			return r, true
		}
	}
	return buffer.Range{}, false
}

// All is every match in the text, or inside the ranges of scope when it is not
// empty, one after the other without overlapping.
func (s *literalSearch) All(scope []buffer.Range) []buffer.Range {
	var out []buffer.Range
	lines := s.model.LineCount()
	for line := 0; line < lines; line++ {
		col := 0
		for {
			r, ok := s.firstInLine(line, col)
			if !ok {
				break
			}
			if len(scope) == 0 || inScope(scope, r) {
				out = append(out, r)
			}
			if r.End.Line > line {
				// A match across lines: go on after it.
				line = r.End.Line
				col = r.End.Col
				if line >= lines {
					break
				}
				continue
			}
			if r.End.Col > col && r.End.Col > r.Start.Col {
				col = r.End.Col
			} else {
				col = r.Start.Col + 1
			}
			if col > len(s.model.LineContent(line)) {
				break
			}
		}
	}
	return out
}

func inScope(scope []buffer.Range, r buffer.Range) bool {
	for _, s := range scope {
		if s.ContainsRange(r) {
			return true
		}
	}
	return false
}
