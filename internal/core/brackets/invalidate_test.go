package brackets

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// TestNewTokensKeepThePairsWhenTheBracketsAreTheSame pins the cost of the tokenizer reporting
// lines: tokens that leave the brackets of a line as they are do not make the pairs again (step 10.1:
// every slice of the tokenizer made the next frame pair 100,000 lines). The listeners are told either way.
func TestNewTokensKeepThePairsWhenTheBracketsAreTheSame(t *testing.T) {
	tests := []struct {
		name string
		// give changes the tokens of line 1 (the text is "(", "a )", ")" and the first resolution
		// has no tokens), and says whether the pairs must be made again.
		give         func(s *fakeSource)
		completeWas  bool // the tokens were complete when the pairs were made
		wantResolved bool
		wantPairs    int
	}{
		{
			name:         "tokens that put every bracket in code",
			give:         func(s *fakeSource) { s.toks[1] = []tokens.Token{{End: 3, Language: 1, Type: textmate.TokenOther}} },
			completeWas:  true,
			wantResolved: false,
			wantPairs:    2,
		},
		{
			name: "a closing bracket that turns out to be in a comment",
			give: func(s *fakeSource) {
				s.toks[1] = []tokens.Token{{End: 2, Language: 1, Type: textmate.TokenOther}, {End: 3, Language: 1, Type: textmate.TokenComment}}
			},
			completeWas:  true,
			wantResolved: true,
			wantPairs:    2,
		},
		{
			name:         "the tokens of the document are complete now",
			give:         func(s *fakeSource) { s.toks[1] = []tokens.Token{{End: 3, Language: 1, Type: textmate.TokenOther}} },
			completeWas:  false,
			wantResolved: true,
			wantPairs:    2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := plain(1, "(\na )\n)")
			src.complete = tt.completeWas
			ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
			if ps := ix.Pairs(whole(src)); len(ps) != 1 {
				t.Fatalf("pairs before = %+v", ps)
			}
			told := 0
			ix.OnChange(func(int) { told++ })
			before := ix.resolutions
			tt.give(src)
			src.complete = true
			ix.Invalidate(1, 1)
			_ = ix.Pairs(whole(src))
			if got := ix.resolutions != before; got != tt.wantResolved {
				t.Errorf("pairs made again = %v, want %v", got, tt.wantResolved)
			}
			if told != 1 {
				t.Errorf("listeners told %d times, want once", told)
			}
		})
	}
}

// readCounter counts what an Index reads of its source.
type readCounter struct {
	*fakeSource
	reads int
}

func (r *readCounter) LineContent(line int) string { r.reads++; return r.fakeSource.LineContent(line) }
func (r *readCounter) LineTokens(line int) []tokens.Token {
	r.reads++
	return r.fakeSource.LineTokens(line)
}
func (r *readCounter) TokensComplete() bool { r.reads++; return r.fakeSource.TokensComplete() }

// TestInvalidateReadsNothingFromTheSource pins that the tokenizer's goroutine, which calls Invalidate, does
// not touch the text (the document belongs to the UI thread): the lines are only noted, and read by the next
// question.
func TestInvalidateReadsNothingFromTheSource(t *testing.T) {
	src := &readCounter{fakeSource: plain(1, "(\na )\n)")}
	ix := newTestIndex(t, src, NewLanguage(1, curly, nil, false))
	_ = ix.Pairs(whole(src.fakeSource))
	before := src.reads
	ix.Invalidate(0, 2)
	ix.Invalidate(1, 1)
	if src.reads != before {
		t.Errorf("Invalidate read the source %d times", src.reads-before)
	}
	_ = ix.Pairs(whole(src.fakeSource))
	if src.reads == before {
		t.Error("the next question did not look at the lines")
	}
}

// TestNotedLinesFollowAnEdit pins that lines noted by Invalidate, and then moved by an edit before anyone
// asked, are found again with their new tokens (not at the places they had).
func TestNotedLinesFollowAnEdit(t *testing.T) {
	src := &quoteSource{lines: []string{"x", "( a", "b )", "y"}}
	langs := testLanguages{1: NewLanguage(1, curly, nil, false)}
	ix := New(src, langs, true)
	whole := buffer.Range{End: buffer.Position{Line: 9}}
	if ps := ix.Pairs(whole); len(ps) != 1 {
		t.Fatalf("pairs = %+v", ps)
	}
	// the closing bracket is in a string now: the tokens of its line changed, nobody asked yet
	src.lines[2] = `b ")"`
	ix.Invalidate(2, 2)
	src.edit(ix, buffer.Range{}, "z\n") // and a line is inserted above, which moves it
	ps := ix.Pairs(whole)
	if len(ps) != 1 || ps[0].HasClose {
		t.Errorf("pairs = %+v, want the one pair without its closing bracket", ps)
	}
}
