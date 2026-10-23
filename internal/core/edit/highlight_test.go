package edit

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func hsel(l1, c1, l2, c2 int) buffer.Selection {
	return buffer.Selection{Anchor: buffer.Position{Line: l1, Col: c1}, Active: buffer.Position{Line: l2, Col: c2}}
}

func hrange(l1, c1, l2, c2 int) buffer.Range {
	return buffer.Range{Start: buffer.Position{Line: l1, Col: c1}, End: buffer.Position{Line: l2, Col: c2}}
}

func TestSelectionQuery(t *testing.T) {
	text := "foo bar foo\nFoo  \n\tbaz foo\nlong long long\néééée"
	cases := []struct {
		name string
		sels []buffer.Selection
		find SearchOptions
		opts SelectionHighlightOptions
		want OccurrenceQuery
		ok   bool
	}{
		{"a selection", []buffer.Selection{hsel(0, 0, 0, 3)}, SearchOptions{}, SelectionHighlightOptions{}, OccurrenceQuery{Text: "foo"}, true},
		{"the find switches go along", []buffer.Selection{hsel(0, 0, 0, 3)}, SearchOptions{MatchCase: true, WholeWord: true}, SelectionHighlightOptions{}, OccurrenceQuery{Text: "foo", MatchCase: true, WholeWord: true}, true},
		{"a cursor has no selection to look for", []buffer.Selection{hsel(0, 1, 0, 1)}, SearchOptions{}, SelectionHighlightOptions{}, OccurrenceQuery{}, false},
		{"no selections", nil, SearchOptions{}, SelectionHighlightOptions{}, OccurrenceQuery{}, false},
		{"spaces and tabs only", []buffer.Selection{hsel(1, 3, 1, 5)}, SearchOptions{}, SelectionHighlightOptions{}, OccurrenceQuery{}, false},
		{"a selection over two lines", []buffer.Selection{hsel(0, 8, 1, 3)}, SearchOptions{}, SelectionHighlightOptions{}, OccurrenceQuery{}, false},
		{"a selection over two lines when they are allowed", []buffer.Selection{hsel(0, 8, 1, 3)}, SearchOptions{}, SelectionHighlightOptions{Multiline: true}, OccurrenceQuery{Text: "foo\nFoo"}, true},
		{"too long", []buffer.Selection{hsel(3, 0, 3, 14)}, SearchOptions{}, SelectionHighlightOptions{MaxLength: 5}, OccurrenceQuery{}, false},
		{"as long as the limit", []buffer.Selection{hsel(3, 0, 3, 4)}, SearchOptions{}, SelectionHighlightOptions{MaxLength: 4}, OccurrenceQuery{Text: "long"}, true},
		{"no limit", []buffer.Selection{hsel(3, 0, 3, 14)}, SearchOptions{}, SelectionHighlightOptions{MaxLength: 0}, OccurrenceQuery{Text: "long long long"}, true},
		{"the same text twice", []buffer.Selection{hsel(0, 0, 0, 3), hsel(0, 8, 0, 11)}, SearchOptions{}, SelectionHighlightOptions{}, OccurrenceQuery{Text: "foo"}, true},
		{"the same text without case", []buffer.Selection{hsel(0, 0, 0, 3), hsel(1, 0, 1, 3)}, SearchOptions{}, SelectionHighlightOptions{}, OccurrenceQuery{Text: "foo"}, true},
		{"the same text with other case when case matters", []buffer.Selection{hsel(0, 0, 0, 3), hsel(1, 0, 1, 3)}, SearchOptions{MatchCase: true}, SelectionHighlightOptions{}, OccurrenceQuery{}, false},
		{"other text", []buffer.Selection{hsel(0, 0, 0, 3), hsel(0, 4, 0, 7)}, SearchOptions{}, SelectionHighlightOptions{}, OccurrenceQuery{}, false},
		{"a second selection that is empty", []buffer.Selection{hsel(0, 0, 0, 3), hsel(0, 5, 0, 5)}, SearchOptions{}, SelectionHighlightOptions{}, OccurrenceQuery{}, false},
		{"counted in characters, not bytes", []buffer.Selection{hsel(4, 0, 4, len("éééé"))}, SearchOptions{}, SelectionHighlightOptions{MaxLength: 4}, OccurrenceQuery{Text: "éééé"}, true},
	}
	c := newEd(t, DefaultOptions(), strings.Split(text, "\n")...).commands()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := c.SelectionQuery(tc.sels, tc.find, tc.opts)
			if ok != tc.ok || got != tc.want {
				t.Errorf("SelectionQuery = %+v, %v; want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestWordQuery(t *testing.T) {
	c := newEd(t, DefaultOptions(), "foo.bar baz", "  ", "naïve café").commands()
	cases := []struct {
		name string
		sel  buffer.Selection
		word buffer.Range
		ok   bool
	}{
		{"in a word", hsel(0, 5, 0, 5), hrange(0, 4, 0, 7), true},
		{"at the start of a word", hsel(0, 4, 0, 4), hrange(0, 4, 0, 7), true},
		{"at the end of a word", hsel(0, 3, 0, 3), hrange(0, 0, 0, 3), true},
		{"a selection inside a word", hsel(0, 5, 0, 6), hrange(0, 4, 0, 7), true},
		{"the whole word selected", hsel(0, 4, 0, 7), hrange(0, 4, 0, 7), true},
		{"a selection that reaches out of the word", hsel(0, 5, 0, 9), buffer.Range{}, false},
		{"a selection over two lines", hsel(0, 1, 2, 1), buffer.Range{}, false},
		{"in white space", hsel(1, 1, 1, 1), buffer.Range{}, false},
		{"a word with accents", hsel(2, 3, 2, 3), hrange(2, 0, 2, len("naïve")), true},
		{"backwards", hsel(0, 6, 0, 5), hrange(0, 4, 0, 7), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, word, ok := c.WordQuery(tc.sel)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if word != tc.word {
				t.Errorf("word = %v, want %v", word, tc.word)
			}
			if !q.MatchCase || !q.WholeWord {
				t.Errorf("the query must be case sensitive and whole word: %+v", q)
			}
		})
	}
}

func TestOccurrences(t *testing.T) {
	c := newEd(t, DefaultOptions(), "foo foobar Foo foo", "xfoo foo.foo", "").commands()
	cases := []struct {
		name string
		q    OccurrenceQuery
		want []buffer.Range
	}{
		{"whole words with case", OccurrenceQuery{Text: "foo", MatchCase: true, WholeWord: true},
			[]buffer.Range{hrange(0, 0, 0, 3), hrange(0, 15, 0, 18), hrange(1, 5, 1, 8), hrange(1, 9, 1, 12)}},
		{"whole words without case", OccurrenceQuery{Text: "foo", WholeWord: true},
			[]buffer.Range{hrange(0, 0, 0, 3), hrange(0, 11, 0, 14), hrange(0, 15, 0, 18), hrange(1, 5, 1, 8), hrange(1, 9, 1, 12)}},
		{"parts of words", OccurrenceQuery{Text: "foo", MatchCase: true},
			[]buffer.Range{hrange(0, 0, 0, 3), hrange(0, 4, 0, 7), hrange(0, 15, 0, 18), hrange(1, 1, 1, 4), hrange(1, 5, 1, 8), hrange(1, 9, 1, 12)}},
		{"nothing to find", OccurrenceQuery{Text: ""}, nil},
		{"not there", OccurrenceQuery{Text: "zzz"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.Occurrences(tc.q); !slices.Equal(got, tc.want) {
				t.Errorf("Occurrences = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOccurrencesApartFrom(t *testing.T) {
	c := newEd(t, DefaultOptions(), "foo foo foo", "foo").commands()
	q := OccurrenceQuery{Text: "foo", MatchCase: true}
	cases := []struct {
		name string
		sels []buffer.Selection
		want []buffer.Range
	}{
		{"the selected one is left out", []buffer.Selection{hsel(0, 4, 0, 7)}, []buffer.Range{hrange(0, 0, 0, 3), hrange(0, 8, 0, 11), hrange(1, 0, 1, 3)}},
		{"every selected one is left out", []buffer.Selection{hsel(0, 0, 0, 3), hsel(1, 0, 1, 3)}, []buffer.Range{hrange(0, 4, 0, 7), hrange(0, 8, 0, 11)}},
		{"a cursor in a match leaves it", []buffer.Selection{hsel(0, 5, 0, 5)}, []buffer.Range{hrange(0, 0, 0, 3), hrange(0, 4, 0, 7), hrange(0, 8, 0, 11), hrange(1, 0, 1, 3)}},
		{"a selection that overlaps a match leaves it out", []buffer.Selection{hsel(0, 2, 0, 5)}, []buffer.Range{hrange(0, 8, 0, 11), hrange(1, 0, 1, 3)}},
		{"a selection that only touches a match", []buffer.Selection{hsel(0, 3, 0, 4)}, []buffer.Range{hrange(0, 0, 0, 3), hrange(0, 4, 0, 7), hrange(0, 8, 0, 11), hrange(1, 0, 1, 3)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.OccurrencesApartFrom(q, tc.sels); !slices.Equal(got, tc.want) {
				t.Errorf("OccurrencesApartFrom = %v, want %v", got, tc.want)
			}
		})
	}
	if got := c.OccurrencesApartFrom(OccurrenceQuery{Text: "zzz"}, nil); got != nil {
		t.Errorf("no matches: %v", got)
	}
}
