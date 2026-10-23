package brackets

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// quoteSource is a fake text whose tokens follow the quotes in a line: what is between a pair of
// double quotes is a string, the rest is code. It changes as an editor does.
type quoteSource struct {
	lines []string
}

func (s *quoteSource) LineCount() int              { return len(s.lines) }
func (s *quoteSource) LineContent(line int) string { return s.lines[line] }
func (s *quoteSource) Language() int               { return 1 }
func (s *quoteSource) TokensComplete() bool        { return true }

func (s *quoteSource) LineTokens(line int) []tokens.Token {
	text := s.lines[line]
	if len(text)%7 == 3 {
		return nil // some lines have no tokens yet (as the tokens of a line follow its text)
	}
	var out []tokens.Token
	typ, start := textmate.TokenOther, 0
	for i := 0; i < len(text); i++ {
		if text[i] != '"' {
			continue
		}
		if typ == textmate.TokenOther {
			out = append(out, tokens.Token{End: int32(i), Language: 1, Type: typ})
			typ, start = textmate.TokenString, i
		} else {
			out = append(out, tokens.Token{End: int32(i + 1), Language: 1, Type: typ})
			typ, start = textmate.TokenOther, i+1
		}
	}
	_ = start
	out = append(out, tokens.Token{End: int32(len(text)), Language: 1, Type: typ})
	return out
}

// edit replaces the text between two positions and tells the index.
func (s *quoteSource) edit(ix *Index, r buffer.Range, with string) {
	before := s.lines[r.Start.Line][:r.Start.Col]
	after := s.lines[r.End.Line][r.End.Col:]
	mid := strings.Split(before+with+after, "\n")
	lines := append([]string{}, s.lines[:r.Start.Line]...)
	lines = append(lines, mid...)
	lines = append(lines, s.lines[r.End.Line+1:]...)
	s.lines = lines
	ix.Apply([]Edited{{From: r.Start.Line, To: r.End.Line, Count: len(mid)}})
}

func randomText(rng *rand.Rand, n int) string {
	const alphabet = "{}()[]<>\"ab \n\n"
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteByte(alphabet[rng.Intn(len(alphabet))])
	}
	return sb.String()
}

func randomPosition(rng *rand.Rand, s *quoteSource) buffer.Position {
	line := rng.Intn(len(s.lines))
	return buffer.Position{Line: line, Col: rng.Intn(len(s.lines[line]) + 1)}
}

func snapshotOf(ix *Index, s *quoteSource) (pairs []PairInfo, brackets, colorized []BracketInfo) {
	all := buffer.Range{End: buffer.Position{Line: len(s.lines) - 1, Col: len(s.lines[len(s.lines)-1])}}
	return ix.Pairs(all), ix.Brackets(all, false), ix.Brackets(all, true)
}

// TestIncrementalEqualsFull edits a text at random and checks that the index that followed the
// edits finds what a new one finds in the same text.
func TestIncrementalEqualsFull(t *testing.T) {
	langs := testLanguages{1: NewLanguage(1, pairs("{", "}", "[", "]", "(", ")", "<", ">"), nil, false)}
	for seed := int64(0); seed < 60; seed++ {
		rng := rand.New(rand.NewSource(seed))
		src := &quoteSource{lines: strings.Split(randomText(rng, 40+rng.Intn(120)), "\n")}
		inc := New(src, langs, true)
		for step := 0; step < 60; step++ {
			a, b := randomPosition(rng, src), randomPosition(rng, src)
			src.edit(inc, buffer.NewRange(a, b), randomText(rng, rng.Intn(8)))
			if rng.Intn(5) == 0 {
				// New tokens for a few lines.
				from := rng.Intn(len(src.lines))
				inc.Invalidate(from, from+rng.Intn(3))
			}
			if step%3 == 0 {
				// Ask in between, so that some edits come before a resolution and some after.
				snapshotOf(inc, src)
			}
			if step%5 != 0 {
				continue
			}
			full := New(src, langs, true)
			gp, gb, gc := snapshotOf(inc, src)
			wp, wb, wc := snapshotOf(full, src)
			if !reflect.DeepEqual(gp, wp) || !reflect.DeepEqual(gb, wb) || !reflect.DeepEqual(gc, wc) {
				t.Fatalf("seed %d step %d: the index that followed the edits differs from a new one\ntext: %q\npairs: %+v\nwant:  %+v", seed, step, src.lines, gp, wp)
			}
			p := randomPosition(rng, src)
			o1, c1, ok1 := inc.Match(p)
			o2, c2, ok2 := full.Match(p)
			if ok1 != ok2 || o1 != o2 || c1 != c2 {
				t.Fatalf("seed %d step %d: Match(%v) differs", seed, step, p)
			}
			e1, f1, ok1 := inc.Enclosing(p)
			e2, f2, ok2 := full.Enclosing(p)
			if ok1 != ok2 || e1 != e2 || f1 != f2 {
				t.Fatalf("seed %d step %d: Enclosing(%v) differs", seed, step, p)
			}
		}
	}
}

// TestPairsAreWhatAStackOfBracketsSays checks the pairs against a plain stack on random texts.
func TestPairsAreWhatAStackOfBracketsSays(t *testing.T) {
	lang := NewLanguage(1, pairs("{", "}", "[", "]", "(", ")"), nil, false)
	for seed := int64(0); seed < 200; seed++ {
		rng := rand.New(rand.NewSource(seed))
		text := randomText(rng, 80)
		src := plain(1, strings.ReplaceAll(strings.ReplaceAll(text, "<", "x"), ">", "y"))
		ix := newTestIndex(t, src, lang)
		type key struct{ l, c int }
		want := map[key]*key{} // opening bracket -> closing bracket, nil when unclosed
		var stack []key
		for l, line := range src.lines {
			for c := 0; c < len(line); c++ {
				switch ch := line[c]; ch {
				case '{', '[', '(':
					want[key{l, c}] = nil
					stack = append(stack, key{l, c})
				case '}', ']', ')':
					open := map[byte]byte{'}': '{', ']': '[', ')': '('}[ch]
					for i := len(stack) - 1; i >= 0; i-- {
						if src.lines[stack[i].l][stack[i].c] == open {
							k := key{l, c}
							want[stack[i]] = &k
							stack = stack[:i]
							break
						}
					}
				}
			}
		}
		got := map[key]*key{}
		for _, p := range ix.Pairs(whole(src)) {
			var cl *key
			if p.HasClose {
				cl = &key{p.Close.Start.Line, p.Close.Start.Col}
			}
			got[key{p.Open.Start.Line, p.Open.Start.Col}] = cl
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d: text %q\npairs %v\nwant  %v", seed, src.lines, got, want)
		}
	}
}

func TestEditsMoveAndInvalidate(t *testing.T) {
	src := &quoteSource{lines: []string{"{", "(", ")", "x", "}"}}
	langs := testLanguages{1: NewLanguage(1, curly, nil, false)}
	ix := New(src, langs, true)
	var from []int
	stop := ix.OnChange(func(f int) { from = append(from, f) })
	pairs := func() []string {
		var out []string
		for _, p := range ix.Pairs(buffer.Range{End: buffer.Position{Line: len(src.lines)}}) {
			out = append(out, rangeString(p.Open))
		}
		return out
	}
	if got := pairs(); !reflect.DeepEqual(got, []string{"1,1-1,2", "2,1-2,2"}) {
		t.Fatalf("pairs = %v", got)
	}
	// A line is inserted above: the pairs move down.
	src.edit(ix, buffer.Range{}, "\n")
	if got := pairs(); !reflect.DeepEqual(got, []string{"2,1-2,2", "3,1-3,2"}) {
		t.Errorf("after inserting a line: %v", got)
	}
	// The closing bracket of the outer pair is deleted: the pair loses its end, and an edit tells where.
	src.edit(ix, buffer.Range{Start: buffer.Position{Line: 5}, End: buffer.Position{Line: 5, Col: 1}}, "")
	if ps := ix.Pairs(buffer.Range{End: buffer.Position{Line: 9}}); len(ps) != 2 || ps[0].HasClose {
		t.Errorf("an unclosed pair: %+v", ps)
	}
	if !reflect.DeepEqual(from, []int{0, 5}) {
		t.Errorf("listeners were told %v", from)
	}
	stop()
	src.edit(ix, buffer.Range{}, "x")
	if len(from) != 2 {
		t.Error("a listener that was removed is not called")
	}
	// The line count that the index knows differs from the source's: it starts again.
	src.lines = append(src.lines, "(", ")")
	ix.Apply(nil)
	if ps := ix.Pairs(buffer.Range{End: buffer.Position{Line: 20}}); len(ps) != 3 {
		t.Errorf("after a change the index did not know of: %+v", ps)
	}
	ix.Splice(0, 0, 1)
	ix.Splice(-1, 0, 1) // ignored
	ix.Splice(0, 99, 1) // ignored
	ix.Splice(0, 0, 0)  // ignored
	ix.Invalidate(-3, 2)
	ix.Invalidate(1000, 2000)
	if ps := ix.Pairs(buffer.Range{End: buffer.Position{Line: 20}}); len(ps) != 3 {
		t.Errorf("after invalidations: %+v", ps)
	}
}

func TestSettingTheTextStartsAgain(t *testing.T) {
	src := plain(1, "(")
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	if ps := ix.Pairs(whole(src)); len(ps) != 1 || ps[0].HasClose {
		t.Fatalf("pairs = %+v", ps)
	}
	src.lines, src.toks = []string{"(", ")"}, [][]tokens.Token{nil, nil}
	ix.Reset()
	if ps := ix.Pairs(whole(src)); len(ps) != 1 || !ps[0].HasClose {
		t.Errorf("pairs = %+v", ps)
	}
}
