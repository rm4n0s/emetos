package brackets

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// Ported from VS Code: reference/vscode/src/vs/editor/test/common/model/bracketPairColorizer/{tokenizer,getBracketPairsInRange}.test.ts
// and reference/vscode/src/vs/editor/test/common/model/textModelWithTokens.test.ts (MIT)

func whole(s *fakeSource) buffer.Range {
	last := len(s.lines) - 1
	return buffer.Range{End: buffer.Position{Line: last, Col: len(s.lines[last])}}
}

func TestBracketsAreFoundInTokensOfTypeOtherOnly(t *testing.T) {
	// Ported from tokenizer.test.ts 'Basic': " { } " "be" "gin end" are one token to look in, and
	// the "{" of the comment is not a bracket.
	src := tokenized(1, other(1, " { } "), other(1, "be"), other(1, "gin end"), other(1, "\n"),
		other(1, "hello"), comment(1, "{"), other(1, "}"))
	ix := newTestIndex(t, src, NewLanguage(1, pairs("{", "}", "[", "]", "(", ")", "begin", "end"), nil, false))
	var got []string
	for _, b := range ix.Brackets(whole(src), false) {
		got = append(got, b.Bracket.Text+" "+rangeString(b.Range))
	}
	want := []string{"{ 1,2-1,3", "} 1,4-1,5", "begin 1,6-1,11", "end 1,12-1,15", "} 2,7-2,8"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("brackets = %v, want %v", got, want)
	}
}

func rangeString(r buffer.Range) string {
	return strings.Join([]string{itoa(r.Start.Line + 1), itoa(r.Start.Col + 1)}, ",") + "-" + strings.Join([]string{itoa(r.End.Line + 1), itoa(r.End.Col + 1)}, ",")
}

func TestTokensOfOtherKindsHideBrackets(t *testing.T) {
	src := tokenized(1, other(1, "( "), stringSeg(1, `")"`), other(1, " ["), seg{"]", 1, textmate.TokenRegEx}, other(1, " )"))
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	var got []string
	for _, b := range ix.Brackets(whole(src), false) {
		got = append(got, b.Bracket.Text)
	}
	if want := []string{"(", "[", ")"}; !reflect.DeepEqual(got, want) {
		t.Errorf("brackets = %v, want %v", got, want)
	}
	// ( is at column 1, [ at 7 and ) at 10 (one-based): [ is never closed.
	ps := ix.Pairs(whole(src))
	if len(ps) != 2 || !ps[0].HasClose || ps[0].Close != at(1, 10, 1, 11) || ps[1].HasClose || ps[1].Open != at(1, 7, 1, 8) {
		t.Errorf("pairs = %+v", ps)
	}
}

func TestLinesWithoutTokensAreCodeInTheLanguageOfTheDocument(t *testing.T) {
	src := plain(1, "{ [ ] }\n(")
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	got := ix.Pairs(whole(src))
	if len(got) != 3 || got[0].Range != at(1, 1, 1, 8) || got[1].Range != at(1, 3, 1, 6) || got[2].HasClose {
		t.Errorf("pairs = %+v", got)
	}
}

// pairJSON is the form of a pair in the tests of getBracketPairsInRange.
type pairJSON struct {
	Level int
	Open  buffer.Range
	Close *buffer.Range
}

func pairsJSON(ps []PairInfo) []pairJSON {
	if len(ps) == 0 {
		return nil
	}
	out := make([]pairJSON, len(ps))
	for i, p := range ps {
		out[i] = pairJSON{Level: p.Level, Open: p.Open}
		if p.HasClose {
			c := p.Close
			out[i].Close = &c
		}
	}
	return out
}

func rp(c buffer.Range) *buffer.Range { return &c }

// colorizedLanguage is the language of those tests: `<` `>` are brackets, and the colorized
// pairs are {} [] ().
func colorizedLanguage() *Language {
	return NewLanguage(1, pairs("<", ">"), pairs("{", "}", "[", "]", "(", ")"), true)
}

func marked(t *testing.T, text string) (*fakeSource, map[int]buffer.Position) {
	t.Helper()
	clean, marks := markers(text)
	return tokenized(1, other(1, clean)), marks
}

func TestGetBracketPairsInRange(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []pairJSON
	}{
		{"Basic 1", "{ ( [] ¹ ) [ ² { } ] () } []", []pairJSON{
			{0, at(1, 1, 1, 2), rp(at(1, 23, 1, 24))},
			{1, at(1, 3, 1, 4), rp(at(1, 9, 1, 10))},
			{1, at(1, 11, 1, 12), rp(at(1, 18, 1, 19))},
		}},
		{"Basic 2", "{ ( [] ¹ ²) [  { } ] () } []", []pairJSON{
			{0, at(1, 1, 1, 2), rp(at(1, 23, 1, 24))},
			{1, at(1, 3, 1, 4), rp(at(1, 9, 1, 10))},
		}},
		{"Basic Empty", "¹ ² { ( [] ) [  { } ] () } []", nil},
		{"Basic All", "¹ { ( [] ) [  { } ] () } [] ²", []pairJSON{
			{0, at(1, 2, 1, 3), rp(at(1, 23, 1, 24))},
			{1, at(1, 4, 1, 5), rp(at(1, 9, 1, 10))},
			{2, at(1, 6, 1, 7), rp(at(1, 7, 1, 8))},
			{1, at(1, 11, 1, 12), rp(at(1, 18, 1, 19))},
			{2, at(1, 14, 1, 15), rp(at(1, 16, 1, 17))},
			{1, at(1, 20, 1, 21), rp(at(1, 21, 1, 22))},
			{0, at(1, 25, 1, 26), rp(at(1, 26, 1, 27))},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, marks := marked(t, tc.text)
			ix := newTestIndex(t, src, colorizedLanguage())
			got := pairsJSON(ix.Pairs(buffer.Range{Start: marks[1], End: marks[2]}))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("pairs = %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

type bracketJSON struct {
	Level, Eq int
	Range     buffer.Range
	Invalid   bool
}

func bracketsJSON(bs []BracketInfo) []bracketJSON {
	out := make([]bracketJSON, len(bs))
	for i, b := range bs {
		out[i] = bracketJSON{b.Level, b.EqualLevel, b.Range, b.Invalid}
	}
	return out
}

func TestGetBracketsInRange(t *testing.T) {
	src, marks := marked(t, "¹ { [ ( [ [ (  ) ] ] ) ] } { } ²")
	ix := newTestIndex(t, src, colorizedLanguage())
	got := ix.Brackets(buffer.Range{Start: marks[1], End: marks[2]}, false)
	type lvl struct {
		level, eq int
		r         buffer.Range
	}
	want := []lvl{
		{0, 0, at(1, 2, 1, 3)}, {1, 0, at(1, 4, 1, 5)}, {2, 0, at(1, 6, 1, 7)}, {3, 1, at(1, 8, 1, 9)},
		{4, 2, at(1, 10, 1, 11)}, {5, 1, at(1, 12, 1, 13)}, {5, 1, at(1, 15, 1, 16)}, {4, 2, at(1, 17, 1, 18)},
		{3, 1, at(1, 19, 1, 20)}, {2, 0, at(1, 21, 1, 22)}, {1, 0, at(1, 23, 1, 24)}, {0, 0, at(1, 25, 1, 26)},
		{0, 0, at(1, 27, 1, 28)}, {0, 0, at(1, 29, 1, 30)},
	}
	var have []lvl
	for _, b := range got {
		have = append(have, lvl{b.Level, b.EqualLevel, b.Range})
	}
	if !reflect.DeepEqual(have, want) {
		t.Errorf("brackets = %+v\nwant %+v", have, want)
	}
}

func TestErrorBracketsAreInvalid(t *testing.T) {
	src, marks := marked(t, "¹ { () ] ² ")
	ix := newTestIndex(t, src, colorizedLanguage())
	got := bracketsJSON(ix.Brackets(buffer.Range{Start: marks[1], End: marks[2]}, false))
	want := []bracketJSON{
		{0, 0, at(1, 2, 1, 3), true},
		{1, 0, at(1, 4, 1, 5), false},
		{1, 0, at(1, 5, 1, 6), false},
		{0, 0, at(1, 7, 1, 8), true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("brackets = %+v\nwant %+v", got, want)
	}
}

func TestColorizedBracketsAgainstBrackets(t *testing.T) {
	src, marks := marked(t, "¹ {} [<()>] <{>} ²")
	ix := newTestIndex(t, src, colorizedLanguage())
	r := buffer.Range{Start: marks[1], End: marks[2]}
	type lvl struct {
		level int
		r     buffer.Range
	}
	check := func(only bool, want []lvl) {
		t.Helper()
		var have []lvl
		for _, b := range ix.Brackets(r, only) {
			have = append(have, lvl{b.Level, b.Range})
			if b.EqualLevel != 0 {
				t.Errorf("only=%v: equal level of %v is %d", only, b.Range, b.EqualLevel)
			}
		}
		if !reflect.DeepEqual(have, want) {
			t.Errorf("only=%v: brackets = %+v\nwant %+v", only, have, want)
		}
	}
	check(true, []lvl{{0, at(1, 2, 1, 3)}, {0, at(1, 3, 1, 4)}, {0, at(1, 5, 1, 6)}, {1, at(1, 7, 1, 8)}, {1, at(1, 8, 1, 9)},
		{0, at(1, 10, 1, 11)}, {0, at(1, 13, 1, 14)}, {-1, at(1, 15, 1, 16)}})
	check(false, []lvl{{0, at(1, 2, 1, 3)}, {0, at(1, 3, 1, 4)}, {0, at(1, 5, 1, 6)}, {1, at(1, 6, 1, 7)}, {2, at(1, 7, 1, 8)}, {2, at(1, 8, 1, 9)},
		{1, at(1, 9, 1, 10)}, {0, at(1, 10, 1, 11)}, {0, at(1, 12, 1, 13)}, {1, at(1, 13, 1, 14)}, {0, at(1, 14, 1, 15)}, {-1, at(1, 15, 1, 16)}})
}

func TestUnmatchedClosingBracketsAfterAPosition(t *testing.T) {
	src := tokenized(1, other(1, "} text }"))
	ix := newTestIndex(t, src, NewLanguage(1, pairs("{", "}", "[", "]"), nil, false))
	got := []bool{
		ix.HasUnmatchedClosingAfter(pos(1, 1), "{"),
		ix.HasUnmatchedClosingAfter(pos(1, 2), "{"),
		ix.HasUnmatchedClosingAfter(pos(1, 9), "{"),
		ix.HasUnmatchedClosingAfter(pos(1, 1), "["),
		ix.HasUnmatchedClosingAfter(pos(1, 1), "nope"),
		ix.HasUnmatchedClosingAfter(pos(1, 1), "}"),
	}
	if want := []bool{true, true, false, false, false, false}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestUnmatchedClosingBracketQueryWaitsForTokens(t *testing.T) {
	src := tokenized(1, stringSeg(1, `"}"`), other(1, "}"))
	src.complete = false
	ix := newTestIndex(t, src, NewLanguage(1, pairs("{", "}"), nil, false))
	before := ix.HasUnmatchedClosingAfter(pos(1, 1), "{")
	src.complete = true
	after := ix.HasUnmatchedClosingAfter(pos(1, 1), "{")
	// The text is replaced and its tokens have not arrived.
	src.lines, src.toks, src.complete = []string{`x"}"}`}, [][]tokens.Token{nil}, false
	ix.Reset()
	replaced := ix.HasUnmatchedClosingAfter(pos(1, 1), "{")
	if want := []bool{false, true, false}; !reflect.DeepEqual([]bool{before, after, replaced}, want) {
		t.Errorf("got %v, want %v", []bool{before, after, replaced}, want)
	}
	if ix.Complete() {
		t.Error("the pairs were made without complete tokens")
	}
}

func TestUnmatchedClosingBracketsAreScopedToTheirLanguage(t *testing.T) {
	src := tokenized(1, other(1, "x"), other(2, "}"))
	ix := newTestIndex(t, src, NewLanguage(1, pairs("{", "}"), nil, false), NewLanguage(2, pairs("{", "}"), nil, false))
	got := []bool{ix.HasUnmatchedClosingAfter(pos(1, 1), "{"), ix.HasUnmatchedClosingAfter(pos(1, 2), "{")}
	if want := []bool{false, true}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func assertMatch(t *testing.T, ix *Index, p buffer.Position, want [2]buffer.Range) {
	t.Helper()
	open, closing, ok := ix.Match(p)
	if !ok {
		t.Errorf("no match at %v, want %v", p, want)
		return
	}
	got := [2]buffer.Range{open, closing}
	if buffer.CompareUsingStarts(got[0], got[1]) > 0 {
		got[0], got[1] = got[1], got[0]
	}
	if buffer.CompareUsingStarts(want[0], want[1]) > 0 {
		want[0], want[1] = want[1], want[0]
	}
	if got != want {
		t.Errorf("match at %v = %v, want %v", p, got, want)
	}
}

func assertNoMatch(t *testing.T, ix *Index, p buffer.Position) {
	t.Helper()
	if open, closing, ok := ix.Match(p); ok {
		t.Errorf("match at %v = %v %v, want none", p, open, closing)
	}
}

func TestBracketMatching1(t *testing.T) {
	src := plain(1, ")]}{[(\n)]}{[(")
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	for _, p := range []buffer.Position{pos(1, 1), pos(1, 2), pos(1, 3), pos(2, 5), pos(2, 6), pos(2, 7)} {
		assertNoMatch(t, ix, p)
	}
	assertMatch(t, ix, pos(1, 4), [2]buffer.Range{at(1, 4, 1, 5), at(2, 3, 2, 4)})
	assertMatch(t, ix, pos(1, 5), [2]buffer.Range{at(1, 5, 1, 6), at(2, 2, 2, 3)})
	assertMatch(t, ix, pos(1, 6), [2]buffer.Range{at(1, 6, 1, 7), at(2, 1, 2, 2)})
	assertMatch(t, ix, pos(1, 7), [2]buffer.Range{at(1, 6, 1, 7), at(2, 1, 2, 2)})
	assertMatch(t, ix, pos(2, 1), [2]buffer.Range{at(2, 1, 2, 2), at(1, 6, 1, 7)})
	assertMatch(t, ix, pos(2, 2), [2]buffer.Range{at(2, 2, 2, 3), at(1, 5, 1, 6)})
	assertMatch(t, ix, pos(2, 3), [2]buffer.Range{at(2, 3, 2, 4), at(1, 4, 1, 5)})
	assertMatch(t, ix, pos(2, 4), [2]buffer.Range{at(2, 3, 2, 4), at(1, 4, 1, 5)})
}

func TestBracketMatching2(t *testing.T) {
	src := plain(1, "var bar = {\nfoo: {\n}, bar: {hallo: [{\n}, {\n}]}}")
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	brackets := []struct {
		at   buffer.Position
		a, b buffer.Range
	}{
		{pos(1, 11), at(1, 11, 1, 12), at(5, 4, 5, 5)}, {pos(1, 12), at(1, 11, 1, 12), at(5, 4, 5, 5)},
		{pos(2, 6), at(2, 6, 2, 7), at(3, 1, 3, 2)}, {pos(2, 7), at(2, 6, 2, 7), at(3, 1, 3, 2)},
		{pos(3, 1), at(3, 1, 3, 2), at(2, 6, 2, 7)}, {pos(3, 2), at(3, 1, 3, 2), at(2, 6, 2, 7)},
		{pos(3, 9), at(3, 9, 3, 10), at(5, 3, 5, 4)}, {pos(3, 10), at(3, 9, 3, 10), at(5, 3, 5, 4)},
		{pos(3, 17), at(3, 17, 3, 18), at(5, 2, 5, 3)},
		{pos(3, 18), at(3, 18, 3, 19), at(4, 1, 4, 2)}, {pos(3, 19), at(3, 18, 3, 19), at(4, 1, 4, 2)},
		{pos(4, 1), at(4, 1, 4, 2), at(3, 18, 3, 19)}, {pos(4, 2), at(4, 1, 4, 2), at(3, 18, 3, 19)},
		{pos(4, 4), at(4, 4, 4, 5), at(5, 1, 5, 2)}, {pos(4, 5), at(4, 4, 4, 5), at(5, 1, 5, 2)},
		{pos(5, 1), at(5, 1, 5, 2), at(4, 4, 4, 5)},
		{pos(5, 2), at(5, 2, 5, 3), at(3, 17, 3, 18)},
		{pos(5, 3), at(5, 3, 5, 4), at(3, 9, 3, 10)},
		{pos(5, 4), at(5, 4, 5, 5), at(1, 11, 1, 12)}, {pos(5, 5), at(5, 4, 5, 5), at(1, 11, 1, 12)},
	}
	isBracket := map[buffer.Position]bool{}
	for _, b := range brackets {
		assertMatch(t, ix, b.at, [2]buffer.Range{b.a, b.b})
		isBracket[b.at] = true
	}
	for line, text := range src.lines {
		for col := 0; col <= len(text); col++ {
			if p := (buffer.Position{Line: line, Col: col}); !isBracket[p] {
				assertNoMatch(t, ix, p)
			}
		}
	}
}

func TestBracketMatching3WithWords(t *testing.T) {
	src := plain(1, strings.Join([]string{
		"begin", "    loop", "        if then", "        end if;", "    end loop;", "end;", "",
		"begin", "    loop", "        if then", "        end ifa;", "    end loop;", "end;",
	}, "\n"))
	ix := newTestIndex(t, src, NewLanguage(1, pairs("if", "end if", "loop", "end loop", "begin", "end"), nil, false))
	assertNoMatch(t, ix, pos(10, 9)) // <if> ... <end ifa> is not matched
	assertMatch(t, ix, pos(3, 9), [2]buffer.Range{at(3, 9, 3, 11), at(4, 9, 4, 15)})
	assertMatch(t, ix, pos(4, 9), [2]buffer.Range{at(4, 9, 4, 15), at(3, 9, 3, 11)})
	assertMatch(t, ix, pos(2, 5), [2]buffer.Range{at(2, 5, 2, 9), at(5, 5, 5, 13)})
	assertMatch(t, ix, pos(5, 5), [2]buffer.Range{at(5, 5, 5, 13), at(2, 5, 2, 9)})
	assertMatch(t, ix, pos(1, 1), [2]buffer.Range{at(1, 1, 1, 6), at(6, 1, 6, 4)})
	assertMatch(t, ix, pos(6, 1), [2]buffer.Range{at(6, 1, 6, 4), at(1, 1, 1, 6)})
}

func TestBracketMatching4ClosingBracketsThatContainEachOther(t *testing.T) {
	src := plain(1, "recordbegin\n  simplerecordbegin\n  endrecord\nendrecord")
	ix := newTestIndex(t, src, NewLanguage(1, pairs("recordbegin", "endrecord", "simplerecordbegin", "endrecord"), nil, false))
	assertMatch(t, ix, pos(1, 1), [2]buffer.Range{at(1, 1, 1, 12), at(4, 1, 4, 10)})
	assertMatch(t, ix, pos(4, 1), [2]buffer.Range{at(4, 1, 4, 10), at(1, 1, 1, 12)})
	assertMatch(t, ix, pos(2, 3), [2]buffer.Range{at(2, 3, 2, 20), at(3, 3, 3, 12)})
	assertMatch(t, ix, pos(3, 3), [2]buffer.Range{at(3, 3, 3, 12), at(2, 3, 2, 20)})
}

func TestBracketMatchingIssue11856(t *testing.T) {
	src := plain(1, "sequence \"outer\"\n     sequence \"inner\"\n     endsequence\nendsequence")
	ix := newTestIndex(t, src, NewLanguage(1, pairs("sequence", "endsequence", "feature", "endfeature"), nil, false))
	assertMatch(t, ix, pos(3, 9), [2]buffer.Range{at(2, 6, 2, 14), at(3, 6, 3, 17)})
}

// tokensAt lays out a line as the VS Code tests do, with the offsets where tokens start.
func tokensAt(text string, starts []int, langs []int, types []textmate.TokenType) []tokens.Token {
	out := make([]tokens.Token, len(starts))
	for i := range starts {
		end := len(text)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		out[i] = tokens.Token{End: int32(end), Language: uint16(langs[i]), Type: types[i]}
	}
	return out
}

func TestBracketMatchingIssue95843ChoosesTheLanguageOfTheToken(t *testing.T) {
	src := &fakeSource{lang: 1, complete: true, lines: []string{"function f() {", "  return <p>{true}</p>;", "}"}}
	o := textmate.TokenOther
	src.toks = [][]tokens.Token{
		tokensAt(src.lines[0], []int{0, 8, 9, 10, 11, 12, 13}, []int{1, 1, 1, 1, 1, 1, 1}, []textmate.TokenType{o, o, o, o, o, o, o}),
		tokensAt(src.lines[1], []int{0, 2, 8, 9, 10, 11, 12, 13, 17, 18, 20, 21, 22}, []int{1, 1, 1, 2, 2, 2, 2, 1, 2, 2, 2, 2, 2}, []textmate.TokenType{o, o, o, o, o, o, o, o, o, o, o, o, o}),
		tokensAt(src.lines[2], []int{0}, []int{1}, []textmate.TokenType{o}),
	}
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false), NewLanguage(2, curly, nil, false))
	open, closing, ok := ix.Match(pos(2, 14))
	if !ok || open != at(2, 13, 2, 14) || closing != at(2, 18, 2, 19) {
		t.Errorf("match = %v %v %v", open, closing, ok)
	}
}

func TestBracketMatchingIssue88075IgnoresStringsInTemplates(t *testing.T) {
	src := &fakeSource{lang: 1, complete: true, lines: []string{"function hello() {", "    console.log(`${100}`);", "}"}}
	o, s := textmate.TokenOther, textmate.TokenString
	src.toks = [][]tokens.Token{
		tokensAt(src.lines[0], []int{0}, []int{1}, []textmate.TokenType{o}),
		tokensAt(src.lines[1], []int{0, 16, 19, 22, 24}, []int{1, 1, 1, 1, 1}, []textmate.TokenType{o, s, o, s, o}),
		tokensAt(src.lines[2], []int{0}, []int{1}, []textmate.TokenType{o}),
	}
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	assertNoMatch(t, ix, pos(2, 23))
	assertNoMatch(t, ix, pos(2, 20))
}

func TestPrevAndNextBracket(t *testing.T) {
	// Ported from textModelWithTokens.test.ts 'brackets1' (testBrackets): from every position, the
	// previous bracket is the last one that starts before it and the next the first one that
	// ends after it.
	text := "if (a == 3) { return (7 * (a + 5)); }"
	src := plain(1, text)
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	var starts []int
	for i, c := range text {
		if strings.ContainsRune("{}[]()", c) {
			starts = append(starts, i)
		}
	}
	for col := 0; col <= len(text); col++ {
		p := buffer.Position{Col: col}
		prev, hasPrev := ix.Prev(p)
		next, hasNext := ix.Next(p)
		wantPrev, wantNext := -1, -1
		for _, s := range starts {
			if s < col {
				wantPrev = s
			}
			if wantNext < 0 && s+1 > col {
				wantNext = s
			}
		}
		if hasPrev != (wantPrev >= 0) || (hasPrev && prev.Range.Start.Col != wantPrev) {
			t.Errorf("Prev(%d) = %v %v, want %d", col, prev, hasPrev, wantPrev)
		}
		if hasNext != (wantNext >= 0) || (hasNext && next.Range.Start.Col != wantNext) {
			t.Errorf("Next(%d) = %v %v, want %d", col, next, hasNext, wantNext)
		}
	}
}

func TestEnclosingAndMatchingUp(t *testing.T) {
	src := plain(1, "f(a, [b, {c}], d) ]")
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	tests := []struct {
		col        int
		open, clos buffer.Range
		ok         bool
	}{
		{col: 0}, // before everything
		{col: 1}, // between f and ( : not inside
		{col: 2, open: at(1, 2, 1, 3), clos: at(1, 17, 1, 18), ok: true},
		{col: 7, open: at(1, 6, 1, 7), clos: at(1, 13, 1, 14), ok: true}, // right after [ : strictly inside
		{col: 11, open: at(1, 10, 1, 11), clos: at(1, 12, 1, 13), ok: true},
		{col: 18}, // after the pair
	}
	for _, tc := range tests {
		open, closing, ok := ix.Enclosing(buffer.Position{Col: tc.col})
		if ok != tc.ok || (ok && (open != tc.open || closing != tc.clos)) {
			t.Errorf("Enclosing(%d) = %v %v %v, want %v %v %v", tc.col, open, closing, ok, tc.open, tc.clos, tc.ok)
		}
	}
	if r, ok := ix.MatchingUp(")", pos(1, 5)); !ok || r != at(1, 2, 1, 3) {
		t.Errorf("MatchingUp(')') = %v %v", r, ok)
	}
	if r, ok := ix.MatchingUp("}", pos(1, 11)); !ok || r != at(1, 10, 1, 11) {
		t.Errorf("MatchingUp('}') = %v %v", r, ok)
	}
	if _, ok := ix.MatchingUp("]", pos(1, 3)); ok {
		t.Error("] has no [ around column 3")
	}
	if _, ok := ix.MatchingUp("x", pos(1, 3)); ok {
		t.Error("x is not a bracket")
	}
	if _, ok := ix.MatchingUp("(", pos(1, 3)); ok {
		t.Error("( is not a closing bracket")
	}
}

func TestAnUnclosedPairIsFoundAndEndsWhereItsContentEnds(t *testing.T) {
	src := plain(1, "{ ( [ ) }")
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	got := ix.Pairs(whole(src))
	want := []PairInfo{
		{Range: at(1, 1, 1, 10), Open: at(1, 1, 1, 2), Close: at(1, 9, 1, 10), HasClose: true},
		{Range: at(1, 3, 1, 8), Open: at(1, 3, 1, 4), Close: at(1, 7, 1, 8), HasClose: true},
		{Range: at(1, 5, 1, 7), Open: at(1, 5, 1, 6)}, // [ is never closed: it ends where ) starts
	}
	for i := range got {
		got[i].OpenBracket, got[i].CloseBracket, got[i].Level, got[i].EqualLevel = nil, nil, 0, 0
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pairs = %+v\nwant %+v", got, want)
	}
	if _, _, ok := ix.Match(pos(1, 5)); ok {
		t.Error("an unclosed bracket matches nothing")
	}
}

func TestDeeplyNestedOpeningBracketsAreText(t *testing.T) {
	src := plain(1, strings.Repeat("(", 200)+strings.Repeat(")", 200))
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	ps := ix.Pairs(whole(src))
	if len(ps) != maxDepth || ps[maxDepth-1].Level != maxDepth-1 {
		t.Fatalf("%d pairs", len(ps))
	}
	bs := ix.Brackets(whole(src), false)
	// 150 openings are pairs, the 50 after them are text; the closings close 150 pairs, and the
	// last 50 close nothing.
	if len(bs) != 2*maxDepth+50 {
		t.Errorf("%d brackets", len(bs))
	}
}

func TestLevelsOfEqualBrackets(t *testing.T) {
	src := plain(1, "( [ ( { ( ) } ) ] ) ( )")
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	var got [][2]int
	for _, p := range ix.Pairs(whole(src)) {
		got = append(got, [2]int{p.Level, p.EqualLevel})
	}
	if want := [][2]int{{0, 0}, {1, 0}, {2, 1}, {3, 0}, {4, 2}, {0, 0}}; !reflect.DeepEqual(got, want) {
		t.Errorf("levels = %v, want %v", got, want)
	}
}

func TestADisabledIndexFindsNothing(t *testing.T) {
	src := plain(1, "{ }")
	ix := New(src, testLanguages{1: NewLanguage(1, curly, nil, false)}, false)
	if ix.Enabled() || len(ix.Pairs(whole(src))) != 0 || len(ix.Brackets(whole(src), false)) != 0 {
		t.Error("a disabled index finds nothing")
	}
	if _, ok := ix.Prev(pos(1, 3)); ok {
		t.Error("Prev")
	}
	if ix.HasUnmatchedClosingAfter(pos(1, 1), "{") {
		t.Error("HasUnmatchedClosingAfter")
	}
}
