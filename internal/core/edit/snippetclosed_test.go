package edit

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// TestSnippetPairs: the cases of VS Code's _findAutoClosingPairs (cursor.ts): the pair at the end of the text of each edit of a
// snippet. Indexes are bytes; a case with non-ASCII text says so.
func TestSnippetPairs(t *testing.T) {
	lang := autoclose.NewLanguage(autoClosingConfig())
	tests := []struct {
		name   string
		texts  []string
		want   [][2]int
		wantOK bool
	}{
		{"a call", []string{"Sprintf()"}, [][2]int{{7, 8}}, true},
		{"a call with arguments in the text", []string{"f(a, b)"}, [][2]int{{1, 6}}, true},
		{"nothing after the closing character", []string{"x[0]"}, [][2]int{{1, 3}}, true},
		{"other characters after it", []string{"f() // call"}, [][2]int{{1, 2}}, true},
		{"the nearest opening before it", []string{"g(f())"}, [][2]int{{3, 5}}, true},
		{"an opening after the pair is not a pair", []string{"f(a) (b"}, [][2]int{{1, 3}}, true},
		{"the last closing character takes the pair", []string{"f() /* ) */"}, [][2]int{{1, 7}}, true},
		{"a quote", []string{"'text'"}, [][2]int{{0, 5}}, true},
		{"a quote as the only character has no opening before it", []string{"'"}, nil, false},
		{"several edits, one for each cursor", []string{"f()", "g(1)"}, [][2]int{{1, 2}, {1, 3}}, true},
		{"one edit without a pair spoils all", []string{"f()", "g"}, nil, false},
		{"a text of several lines", []string{"f(\n)"}, nil, false},
		{"an empty text", []string{""}, nil, false},
		{"no text", nil, nil, false},
		{"a closing character with no opening before it", []string{"a)"}, nil, false},
		{"a closing character that no single pair closes", []string{"a>"}, nil, false},
		{"text before the pair is not counted", []string{"é(é)"}, [][2]int{{2, 5}}, true}, // é is two bytes
	}
	for _, tt := range tests {
		got, ok := SnippetPairs(lang, tt.texts)
		if ok != tt.wantOK || !slices.Equal(got, tt.want) {
			t.Errorf("%s: %q = %v, %v; want %v, %v", tt.name, tt.texts, got, ok, tt.want, tt.wantOK)
		}
	}
	// A language with no pairs, or none at all, finds none.
	for _, l := range []*autoclose.Language{autoclose.NewLanguage(nil), nil} {
		if got, ok := SnippetPairs(l, []string{"f()"}); ok {
			t.Errorf("a language without pairs found %v", got)
		}
	}
}

// TestSnippetAutoClosed: the ranges of the pairs in the document the snippet was inserted in.
func TestSnippetAutoClosed(t *testing.T) {
	lang := autoclose.NewLanguage(autoClosingConfig())
	e := acEd(t, autoClosingConfig(), "ab fmt.Sprintf()", "x := f(1)")
	snap := e.doc.Snapshot()
	// "Sprintf()" starts at offset 7 on the first line; "f(1)" at offset 17+5 = 22 (the first line has 16 bytes and a line feed)
	got := SnippetAutoClosed(snap, lang, []string{"Sprintf()", "f(1)"}, []int{7, 22})
	want := []AutoClosed{
		{Character: buffer.Range{Start: pos(0, 15), End: pos(0, 16)}, Enclosing: buffer.Range{Start: pos(0, 14), End: pos(0, 16)}},
		{Character: buffer.Range{Start: pos(1, 8), End: pos(1, 9)}, Enclosing: buffer.Range{Start: pos(1, 6), End: pos(1, 9)}},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := SnippetAutoClosed(snap, lang, []string{"Sprintf()", "nothing"}, []int{7, 22}); got != nil {
		t.Errorf("a text without a pair gave %v", got)
	}
	if got := SnippetAutoClosed(snap, lang, []string{"f()"}, nil); got != nil {
		t.Errorf("mismatched starts gave %v", got)
	}
}
