package brackets

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// fakeSource is a text with tokens that the test lays out by hand.
type fakeSource struct {
	lines    []string
	toks     [][]tokens.Token
	lang     int
	complete bool
}

func (s *fakeSource) LineCount() int                     { return len(s.lines) }
func (s *fakeSource) LineContent(line int) string        { return s.lines[line] }
func (s *fakeSource) LineTokens(line int) []tokens.Token { return s.toks[line] }
func (s *fakeSource) Language() int                      { return s.lang }
func (s *fakeSource) TokensComplete() bool               { return s.complete }

// testLanguages maps codes to languages.
type testLanguages map[int]*Language

func (m testLanguages) Language(code int) *Language { return m[code] }

// seg is a piece of text with the kind of token it is.
type seg struct {
	text string
	lang int
	typ  textmate.TokenType
}

// other is a piece of ordinary code in the language with the code.
func other(lang int, text string) seg { return seg{text, lang, textmate.TokenOther} }

// comment is a piece of comment in the language with the code.
func comment(lang int, text string) seg { return seg{text, lang, textmate.TokenComment} }

// stringSeg is a piece of string in the language with the code.
func stringSeg(lang int, text string) seg { return seg{text, lang, textmate.TokenString} }

// tokenized lays out segments as a document: each segment is a token (or several, one in each
// line), as VS Code's TokenizedDocument does.
func tokenized(lang int, segs ...seg) *fakeSource {
	s := &fakeSource{lang: lang, complete: true}
	var cur []tokens.Token
	col := 0
	var curText strings.Builder
	flush := func() {
		s.lines = append(s.lines, curText.String())
		s.toks = append(s.toks, cur)
		cur, col = nil, 0
		curText.Reset()
	}
	for _, sg := range segs {
		parts := strings.Split(sg.text, "\n")
		for i, p := range parts {
			if i > 0 {
				flush()
			}
			if p == "" {
				continue
			}
			col += len(p)
			curText.WriteString(p)
			cur = append(cur, tokens.Token{End: int32(col), Language: uint16(sg.lang), Type: sg.typ})
		}
	}
	flush()
	return s
}

// plain is a text that has no tokens: every line is code in the language of the document.
func plain(lang int, text string) *fakeSource {
	lines := strings.Split(text, "\n")
	return &fakeSource{lines: lines, toks: make([][]tokens.Token, len(lines)), lang: lang, complete: true}
}

// at makes a range from one-based lines and columns, as VS Code's tests write them.
func at(l1, c1, l2, c2 int) buffer.Range {
	return buffer.Range{Start: buffer.Position{Line: l1 - 1, Col: c1 - 1}, End: buffer.Position{Line: l2 - 1, Col: c2 - 1}}
}

// pos makes a position from a one-based line and column.
func pos(line, col int) buffer.Position { return buffer.Position{Line: line - 1, Col: col - 1} }

// markers reads a text with markers: the superscript digits ¹ and ² (VS Code's tests write the
// ends of a range with them) are taken out and their positions kept.
func markers(src string) (string, map[int]buffer.Position) {
	numbers := []rune{'⁰', '¹', '²', '³', '⁴', '⁵', '⁶', '⁷', '⁸', '⁹'}
	var sb strings.Builder
	got := map[int]buffer.Position{}
	line, col := 0, 0
	for _, r := range src {
		idx := -1
		for i, n := range numbers {
			if r == n {
				idx = i
			}
		}
		if idx >= 0 {
			got[idx] = buffer.Position{Line: line, Col: col}
			continue
		}
		sb.WriteRune(r)
		if r == '\n' {
			line, col = line+1, 0
		} else {
			col += len(string(r))
		}
	}
	return sb.String(), got
}

func newTestIndex(t testing.TB, src Source, langs ...*Language) *Index {
	t.Helper()
	m := testLanguages{}
	for _, l := range langs {
		m[l.Code()] = l
	}
	return New(src, m, true)
}

// pairs makes pairs from a list of open, close, open, close...
func pairs(texts ...string) []Pair {
	var out []Pair
	for i := 0; i+1 < len(texts); i += 2 {
		out = append(out, Pair{Open: texts[i], Close: texts[i+1]})
	}
	return out
}

var curly = pairs("{", "}", "[", "]", "(", ")")
