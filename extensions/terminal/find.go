package terminal

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// The search over the scrollback of a terminal (SPEC §10.25 "Find and links"). It reads the buffer from the emulator a block of
// rows at a time, joins the rows an autowrap joined into logical lines, and searches each line, so that a match can run across
// a soft wrap. It knows no widget; the pane keeps the matches.

const (
	// findLimit is the most matches that are kept and highlighted (VS Code's XtermTerminalConstants.SearchHighlightLimit).
	findLimit = 20000
	// findBlock is how many rows are read from the emulator at a time.
	findBlock = 512
	// findMaxLine is the longest logical line that is searched, in characters; a longer one is cut there.
	findMaxLine = 20000
)

// findQuery is what the user asked for: the text, the switches and the characters that separate words.
type findQuery struct {
	text       string
	matchCase  bool
	wholeWord  bool
	regex      bool
	separators string
}

// findMatch is a match: the cell it begins on and the cell after the last one it covers (the end can be past the right edge
// of the last row by one, when the match ends there).
type findMatch struct {
	start, end pos
}

// compile makes the regular expression of a query. The text of a search that is not a regular expression is escaped; the case
// is ignored unless the query matches it. Go's regular expressions (RE2) have no lookahead or backreferences: such a pattern
// does not compile and the query finds nothing (SPEC §10.25).
func (q findQuery) compile() (*regexp.Regexp, error) {
	if q.text == "" {
		return nil, nil
	}
	expr := q.text
	if !q.regex {
		expr = regexp.QuoteMeta(q.text)
	}
	if !q.matchCase {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, errors.NewErr("TerminalFindPatternInvalid", err).SetMetadata("pattern", q.text)
	}
	return re, nil
}

// isSeparator tells whether a character ends a word: white space, or one of the word separators.
func (q findQuery) isSeparator(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune(q.separators, r)
}

// searchBuffer finds every match of the query in the buffer of the emulator (the scrollback and the screen), in order of
// position, at most limit of them; truncated says that there were more. It stops with the error of ctx when ctx ends, and
// fails with TerminalFindPatternInvalid for a pattern that does not compile.
func searchBuffer(ctx context.Context, em *emulator.Emulator, q findQuery, limit int) (matches []findMatch, truncated bool, err error) {
	re, err := q.compile()
	if err != nil {
		return nil, false, err
	}
	if re == nil {
		return nil, false, nil
	}
	var f emulator.Frame
	count := em.LineCount()
	block := findBlock
	at := 0 // the first row of the block that is read
	for at < count {
		if err := ctx.Err(); err != nil {
			return nil, false, errors.NewErr("TerminalFindCanceled", err)
		}
		n := min(block, count-at)
		em.ReadLines(at, n, &f)
		y := 0
		for y < f.Rows {
			end := y
			for end < f.Rows-1 && f.Wrapped[end] {
				end++
			}
			if f.Wrapped[end] && at+n < count {
				// the line goes on in the next block: read from the row it begins on, or a larger block when it begins this one
				break
			}
			line := buildLine(&f, y, true, findMaxLine)
			if matches, truncated = appendMatches(matches, line, re, q, limit); truncated {
				return matches, true, nil
			}
			y = end + 1
		}
		switch {
		case y == 0 && at+n < count:
			block *= 2
		default:
			at += y
			block = findBlock
		}
	}
	return matches, false, nil
}

// appendMatches adds the matches of one logical line.
func appendMatches(out []findMatch, line textLine, re *regexp.Regexp, q findQuery, limit int) ([]findMatch, bool) {
	if line.text == "" {
		return out, false
	}
	var runes []rune
	if q.wholeWord {
		runes = []rune(line.text)
	}
	for _, m := range re.FindAllStringIndex(line.text, -1) {
		if m[1] == m[0] {
			continue // an empty match is nothing to show
		}
		s, e := runeIndex(line.text, m[0]), runeIndex(line.text, m[1])
		if q.wholeWord && ((s != 0 && !q.isSeparator(runes[s-1])) || (e != len(runes) && !q.isSeparator(runes[e]))) {
			continue
		}
		if len(out) >= limit {
			return out, true
		}
		w := 1
		if line.wide[e-1] {
			w = 2
		}
		sr, sc := int(line.at[s])/line.cols, int(line.at[s])%line.cols
		er, ec := int(line.at[e-1])/line.cols, int(line.at[e-1])%line.cols
		out = append(out, findMatch{
			start: pos{line: line.first + int64(sr), col: sc},
			end:   pos{line: line.first + int64(er), col: ec + w},
		})
	}
	return out, false
}

// segments are the cells a match covers on the rows [top, top+rows) of a frame of cols columns, one mark for each row.
func (m findMatch) segments(top int64, rows, cols int, color uint32, out []mark) []mark {
	for line := max(m.start.line, top); line <= m.end.line && line < top+int64(rows); line++ {
		x0, x1 := 0, cols
		if line == m.start.line {
			x0 = m.start.col
		}
		if line == m.end.line {
			x1 = min(m.end.col, cols)
		}
		if x1 > x0 {
			out = append(out, mark{line: line, x0: x0, x1: x1, color: color})
		}
	}
	return out
}

// matchAtOrBefore is the index of the last match that starts at or before p, -1 for none.
func matchAtOrBefore(ms []findMatch, p pos) int {
	return sort.Search(len(ms), func(i int) bool { return p.before(ms[i].start) }) - 1
}

// matchAfter is the index of the first match that starts after p, -1 for none.
func matchAfter(ms []findMatch, p pos) int {
	i := sort.Search(len(ms), func(i int) bool { return p.before(ms[i].start) })
	if i == len(ms) {
		return -1
	}
	return i
}

// matchBefore is the index of the last match that starts before p, -1 for none.
func matchBefore(ms []findMatch, p pos) int {
	return sort.Search(len(ms), func(i int) bool { return !ms[i].start.before(p) }) - 1
}
