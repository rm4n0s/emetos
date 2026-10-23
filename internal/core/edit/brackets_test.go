package edit

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// textSource is the bracket source of a text with no tokens: every line is code.
type textSource struct{ lines []string }

func (s textSource) LineCount() int                { return len(s.lines) }
func (s textSource) LineContent(line int) string   { return s.lines[line] }
func (s textSource) LineTokens(int) []tokens.Token { return nil }
func (s textSource) Language() int                 { return 1 }
func (s textSource) TokensComplete() bool          { return true }

type oneLanguage struct{ l *brackets.Language }

func (o oneLanguage) Language(int) *brackets.Language { return o.l }

// bracketsOf makes the bracket structure of a text, with the brackets () {} [] of JavaScript.
func bracketsOf(text string) (*brackets.Index, Model) {
	src := textSource{lines: strings.Split(text, "\n")}
	lang := brackets.NewLanguage(1, []brackets.Pair{{Open: "(", Close: ")"}, {Open: "{", Close: "}"}, {Open: "[", Close: "]"}}, nil, false)
	return brackets.New(src, oneLanguage{lang}, true), lines(src.lines)
}

// lines is a Model over lines.
type lines []string

func (l lines) LineCount() int              { return len(l) }
func (l lines) LineContent(line int) string { return l[line] }
func (l lines) ValidatePosition(p buffer.Position) buffer.Position {
	p.Line = min(max(p.Line, 0), len(l)-1)
	p.Col = min(max(p.Col, 0), len(l[p.Line]))
	return p
}

// r1 is a range written with one-based lines and columns, as VS Code's tests write them.
func r1(l1, c1, l2, c2 int) buffer.Range { return rangeOf(l1-1, c1-1, l2-1, c2-1) }

// Ported from VS Code: smartSelect.test.ts ('getRangesToPosition' tests, with the bracket and word providers).
func TestVSCodeSmartSelectRangesToPosition(t *testing.T) {
	tests := []struct {
		name      string
		text      []string
		line, col int
		want      []buffer.Range // from the largest to the smallest
		leading   bool
	}{
		{"#1", []string{"function a(bar, foo){", "\tif (bar) {", "\t\treturn (bar + (2 * foo))", "\t}", "}"}, 3, 20, []buffer.Range{
			r1(1, 1, 5, 2), r1(1, 21, 5, 2), r1(1, 22, 5, 1), r1(2, 1, 4, 3), r1(2, 1, 4, 3), r1(2, 2, 4, 3),
			r1(2, 11, 4, 3), r1(2, 12, 4, 2), r1(3, 1, 3, 27), r1(3, 3, 3, 27), r1(3, 10, 3, 27), r1(3, 11, 3, 26),
			r1(3, 17, 3, 26), r1(3, 18, 3, 25),
		}, true},
		{"leading and trailing white space", []string{"aaa", "\tbbb", ""}, 2, 3, []buffer.Range{
			r1(1, 1, 3, 1), r1(2, 1, 2, 5), r1(2, 2, 2, 5),
		}, true},
		{"no leading and trailing white space", []string{"aaa", "\tbbb", ""}, 2, 3, []buffer.Range{
			r1(1, 1, 3, 1), r1(2, 2, 2, 5),
		}, false},
		{"#56886 skip empty lines", []string{"function a(bar, foo){", "\tif (bar) {", "", "\t}", "}"}, 3, 1, []buffer.Range{
			r1(1, 1, 5, 2), r1(1, 21, 5, 2), r1(1, 22, 5, 1), r1(2, 1, 4, 3), r1(2, 1, 4, 3), r1(2, 2, 4, 3), r1(2, 11, 4, 3), r1(2, 12, 4, 2),
		}, true},
		{"#56886 do not skip lines with only white space", []string{"function a(bar, foo){", "\tif (bar) {", " ", "\t}", "}"}, 3, 1, []buffer.Range{
			r1(1, 1, 5, 2), r1(1, 21, 5, 2), r1(1, 22, 5, 1), r1(2, 1, 4, 3), r1(2, 1, 4, 3), r1(2, 2, 4, 3), r1(2, 11, 4, 3), r1(2, 12, 4, 2),
			r1(3, 1, 3, 2), r1(3, 1, 3, 2),
		}, true},
		{"#40658 first position inside brackets selects the line inside", []string{" [ ]", " { } ", "( ) "}, 2, 3, []buffer.Range{
			r1(1, 1, 3, 5), r1(2, 1, 2, 6), r1(2, 2, 2, 5), r1(2, 3, 2, 4),
		}, true},
		{"#40658 empty brackets are revealed first", []string{" [] ", " { } ", "  ( ) "}, 1, 3, []buffer.Range{
			r1(1, 1, 3, 7), r1(1, 1, 1, 5), r1(1, 2, 1, 4), r1(1, 3, 1, 3),
		}, true},
		{"#40658 tokens before a bracket are revealed first", []string{"  [] ", " { } ", "selectthis( ) "}, 3, 11, []buffer.Range{
			r1(1, 1, 3, 15), r1(3, 1, 3, 15), r1(3, 1, 3, 14), r1(3, 1, 3, 11),
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ix, m := bracketsOf(strings.Join(tc.text, "\n"))
			ropts := SelectionRangesOptions{SelectLeadingAndTrailingWhitespace: tc.leading, SelectSubwords: true}
			// The word pattern of the JavaScript language of the test.
			opts := DefaultOptions()
			got := ProvideSelectionRanges(m, opts, []buffer.Position{r1(tc.line, tc.col, tc.line, tc.col).Start}, ropts, NewBracketSelectionRanges(ix))
			if len(got) != 1 {
				t.Fatalf("%d lists", len(got))
			}
			want := make([]buffer.Range, len(tc.want))
			for i, r := range tc.want {
				want[len(tc.want)-1-i] = r
			}
			if !reflect.DeepEqual(got[0], want) {
				t.Errorf("ranges =\n%v\nwant\n%v", got[0], want)
			}
		})
	}
}

// Ported from VS Code: smartSelect.test.ts ('bracket selection', 'bracket with leading/trailing').
func TestVSCodeBracketSelectionRanges(t *testing.T) {
	tests := []struct {
		name  string
		value string // the cursor is at |
		want  []buffer.Range
	}{
		{"empty brackets", "(|)", []buffer.Range{r1(1, 2, 1, 2), r1(1, 1, 1, 3)}},
		{"nested", "[[[](|)]]", []buffer.Range{r1(1, 6, 1, 6), r1(1, 5, 1, 7), r1(1, 3, 1, 7), r1(1, 2, 1, 8), r1(1, 2, 1, 8), r1(1, 1, 1, 9)}},
		{"with text", "[a[](|)a]", []buffer.Range{r1(1, 6, 1, 6), r1(1, 5, 1, 7), r1(1, 2, 1, 8), r1(1, 1, 1, 9)}},
		{"no bracket", "fofof|fofo", nil},
		{"after all", "[[[]()]]|", nil},
		{"before all", "|[[[]()]]", nil},
		{"edge: after the first opening", "[|[[]()]]", []buffer.Range{r1(1, 2, 1, 8), r1(1, 1, 1, 9)}},
		{"edge: before the last closing", "[[[]()]|]", []buffer.Range{r1(1, 2, 1, 8), r1(1, 1, 1, 9)}},
		{"one of several", "aaa(aaa)bbb(b|b)ccc(ccc)", []buffer.Range{r1(1, 13, 1, 15), r1(1, 12, 1, 16)}},
		{"one of several in a pair", "(aaa(aaa)bbb(b|b)ccc(ccc))", []buffer.Range{r1(1, 14, 1, 16), r1(1, 13, 1, 17), r1(1, 2, 1, 25), r1(1, 1, 1, 26)}},
		{"leading on the line", "for(a of b){\n  foo(|);\n}", []buffer.Range{
			r1(2, 7, 2, 7), r1(2, 6, 2, 8), r1(1, 13, 3, 1), r1(1, 12, 3, 2), r1(1, 1, 3, 2), r1(1, 1, 3, 2)}},
		{"leading on the line above", "for(a of b)\n{\n  foo(|);\n}", []buffer.Range{
			r1(3, 7, 3, 7), r1(3, 6, 3, 8), r1(2, 2, 4, 1), r1(2, 1, 4, 2), r1(1, 1, 4, 2), r1(1, 1, 4, 2)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			at := strings.Index(tc.value, "|")
			text := strings.Replace(tc.value, "|", "", 1)
			ix, m := bracketsOf(text)
			pos := buffer.New(text).Snapshot().PositionAt(at)
			got := NewBracketSelectionRanges(ix).SelectionRanges(m, []buffer.Position{pos})
			if !reflect.DeepEqual(got[0], tc.want) {
				t.Errorf("ranges =\n%v\nwant\n%v", got[0], tc.want)
			}
		})
	}
}

func TestBracketCommands(t *testing.T) {
	text := "f(a, [b, {c}], d) x"
	tests := []struct {
		name string
		sel  buffer.Selection
		jump buffer.Position // JumpToBracket
		// SelectToBracket with brackets and without; want is the zero Selection when none
		with, without buffer.Selection
	}{
		{"on an opening bracket", cur(0, 1), pos0(0, 16), sel0(0, 1, 0, 17), sel0(0, 2, 0, 16)},
		{"after an opening bracket", cur(0, 2), pos0(0, 16), sel0(0, 1, 0, 17), sel0(0, 2, 0, 16)},
		{"on the closing bracket", cur(0, 16), pos0(0, 1), sel0(0, 17, 0, 1), sel0(0, 16, 0, 2)},
		{"after a closing bracket", cur(0, 13), pos0(0, 5), sel0(0, 13, 0, 5), sel0(0, 12, 0, 6)},
		{"inside a pair, not on a bracket", cur(0, 3), pos0(0, 16), sel0(0, 1, 0, 17), sel0(0, 2, 0, 16)},
		{"inside the innermost pair", cur(0, 10), pos0(0, 11), sel0(0, 9, 0, 12), sel0(0, 10, 0, 11)},
		{"outside every pair: the next bracket", cur(0, 18), pos0(0, 18), buffer.Selection{}, buffer.Selection{}},
		{"before everything: the next bracket", cur(0, 0), pos0(0, 1), sel0(0, 1, 0, 17), sel0(0, 2, 0, 16)},
		{"a selection counts from its start", sel0(0, 4, 0, 12), pos0(0, 16), sel0(0, 1, 0, 17), sel0(0, 2, 0, 16)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ix, _ := bracketsOf(text)
			sels := []buffer.Selection{tc.sel}
			if got := JumpToBracket(ix, sels); len(got) != 1 || got[0] != buffer.NewCursor(tc.jump) {
				t.Errorf("JumpToBracket = %v, want a cursor at %v", got, tc.jump)
			}
			got, ok := SelectToBracket(ix, sels, true)
			if want := tc.with; (want == buffer.Selection{}) != !ok || (ok && got[0] != want) {
				t.Errorf("SelectToBracket(true) = %v %v, want %v", got, ok, want)
			}
			got, ok = SelectToBracket(ix, sels, false)
			if want := tc.without; (want == buffer.Selection{}) != !ok || (ok && got[0] != want) {
				t.Errorf("SelectToBracket(false) = %v %v, want %v", got, ok, want)
			}
		})
	}
}

// Where two pairs touch the position, the bracket that starts later wins.
func TestBracketCommandsAtTouchingPairs(t *testing.T) {
	ix, _ := bracketsOf("(a)(b)")
	sels := []buffer.Selection{cur(0, 3)}
	if got := JumpToBracket(ix, sels); got[0] != cur(0, 5) {
		t.Errorf("JumpToBracket = %v, want the end bracket of (b)", got)
	}
	if got, ok := SelectToBracket(ix, sels, true); !ok || got[0] != sel0(0, 3, 0, 6) {
		t.Errorf("SelectToBracket = %v %v", got, ok)
	}
}

func cur(line, col int) buffer.Selection { return buffer.NewCursor(pos0(line, col)) }
func pos0(line, col int) buffer.Position { return buffer.Position{Line: line, Col: col} }
func sel0(l1, c1, l2, c2 int) buffer.Selection {
	return buffer.Selection{Anchor: pos0(l1, c1), Active: pos0(l2, c2)}
}

func TestRemoveBrackets(t *testing.T) {
	tests := []struct {
		name string
		text string
		sels []sel4
		want string
		out  []sel4
	}{
		{"on a bracket", "f(a, b)", []sel4{{1, 3, 1, 3}}, "fa, b", []sel4{{1, 2, 1, 2}}},
		{"inside a pair", "f(a, [b])", []sel4{{1, 7, 1, 7}}, "f(a, b)", []sel4{{1, 6, 1, 6}}},
		{"on the closing bracket of the outer pair", "f(a, [b])", []sel4{{1, 10, 1, 10}}, "fa, [b]", []sel4{{1, 8, 1, 8}}},
		{"not in any pair", "f(a) x", []sel4{{1, 6, 1, 6}}, "f(a) x", []sel4{{1, 6, 1, 6}}},
		{"several cursors", "(a) [b]", []sel4{{1, 2, 1, 2}, {1, 6, 1, 6}}, "a b", []sel4{{1, 1, 1, 1}, {1, 3, 1, 3}}},
		{"two cursors in one pair remove it once", "(a b)", []sel4{{1, 2, 1, 2}, {1, 4, 1, 4}}, "a b", []sel4{{1, 1, 1, 1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEd(t, DefaultOptions(), tc.text).at(tc.sels...)
			ix, _ := bracketsOf(tc.text)
			e.run(func(c Commands, s []buffer.Selection) Result { return c.RemoveBrackets(ix, s) })
			e.assertText(tc.want)
			e.assertSels(tc.out...)
		})
	}
}

// lineTokens is a fake source of tokens.
type lineTokens map[int][]tokens.Token

func (l lineTokens) Line(line int) []tokens.Token { return l[line] }

func TestStringSelectionRanges(t *testing.T) {
	other, str := textmate.TokenOther, textmate.TokenString
	tok := func(end int32, typ textmate.TokenType) tokens.Token { return tokens.Token{End: end, Type: typ} }
	// x := "hello \"w\" world" + 'a'   (the string is split in several tokens, as grammars do)
	line := `x := "hello \"w\" world" + 'a'`
	src := lineTokens{0: {
		tok(5, other), tok(6, str), tok(12, str), tok(14, str), tok(15, str), tok(17, str), tok(23, str), tok(24, str), tok(27, other), tok(28, str), tok(29, str), tok(30, str),
	}}
	m := lines{line, "`raw`", "no quotes"}
	tests := []struct {
		name string
		pos  buffer.Position
		want []buffer.Range
	}{
		{"inside the text", pos0(0, 9), []buffer.Range{rangeOf(0, 6, 0, 23), rangeOf(0, 5, 0, 24)}},
		{"inside an escape", pos0(0, 13), []buffer.Range{rangeOf(0, 6, 0, 23), rangeOf(0, 5, 0, 24)}},
		{"on the opening quote", pos0(0, 5), []buffer.Range{rangeOf(0, 6, 0, 23), rangeOf(0, 5, 0, 24)}},
		{"after the closing quote", pos0(0, 24), []buffer.Range{rangeOf(0, 6, 0, 23), rangeOf(0, 5, 0, 24)}},
		{"in code", pos0(0, 2), nil},
		{"in the second string", pos0(0, 29), []buffer.Range{rangeOf(0, 28, 0, 29), rangeOf(0, 27, 0, 30)}},
		{"a line with no tokens", pos0(1, 2), nil},
		{"outside the text", pos0(9, 2), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewStringSelectionRanges(src).SelectionRanges(m, []buffer.Position{tc.pos})
			if !reflect.DeepEqual(got[0], tc.want) {
				t.Errorf("ranges = %v, want %v", got[0], tc.want)
			}
		})
	}
	// A string without quotes at its ends has only the whole string.
	src = lineTokens{2: {tok(10, str)}}
	got := NewStringSelectionRanges(src).SelectionRanges(m, []buffer.Position{pos0(2, 3)})
	if want := []buffer.Range{rangeOf(2, 0, 2, 9)}; !reflect.DeepEqual(got[0], want) {
		t.Errorf("no quotes: %v, want %v", got[0], want)
	}
}

func TestStringStepsOfExpandSelection(t *testing.T) {
	str := textmate.TokenString
	text := `x := "hello world"`
	src := lineTokens{0: {{End: 5, Type: textmate.TokenOther}, {End: 18, Type: str}}}
	m := lines{text}
	got := ProvideSelectionRanges(m, DefaultOptions(), []buffer.Position{pos0(0, 14)}, DefaultSelectionRangesOptions(), NewStringSelectionRanges(src))
	want := []buffer.Range{rangeOf(0, 12, 0, 17), rangeOf(0, 6, 0, 17), rangeOf(0, 5, 0, 18), rangeOf(0, 0, 0, 18)}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("steps = %v\nwant %v", got[0], want)
	}
}
