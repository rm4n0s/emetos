package fuzzy

import (
	"reflect"
	"testing"
	"time"
)

// Ported from VS Code: src/vs/base/test/common/filters.test.ts

func m(pairs ...int) []Match {
	out := []Match{}
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Match{Start: pairs[i], End: pairs[i+1]})
	}
	return out
}

type filterCase struct {
	word, target string
	// want is nil when the filter must not match; anyMatch accepts any matches.
	want     []Match
	noMatch  bool
	anyMatch bool
}

func runFilter(t *testing.T, name string, f Filter, cases []filterCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(name+"/"+tc.word+"|"+tc.target, func(t *testing.T) {
			got, ok := f(tc.word, tc.target)
			if tc.noMatch {
				if ok {
					t.Fatalf("%q matched %q: %v", tc.word, tc.target, got)
				}
				return
			}
			if !ok {
				t.Fatalf("%q did not match %q", tc.word, tc.target)
			}
			if !tc.anyMatch && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("matches %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOr(t *testing.T) {
	counters := [2]int{}
	mk := func(i int, r bool) Filter {
		return func(string, string) ([]Match, bool) { counters[i]++; return nil, r }
	}
	tests := []struct {
		name  string
		a, b  bool
		want  [2]int
		match bool
	}{
		{"none", false, false, [2]int{1, 1}, false},
		{"first", true, false, [2]int{1, 0}, true},
		{"both", true, true, [2]int{1, 0}, true},
		{"second", false, true, [2]int{1, 1}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counters = [2]int{}
			_, ok := Or(mk(0, tc.a), mk(1, tc.b))("x", "x")
			if ok != tc.match || counters != tc.want {
				t.Errorf("match %v counters %v, want %v %v", ok, counters, tc.match, tc.want)
			}
		})
	}
}

func TestMatchesCamelCase(t *testing.T) {
	runFilter(t, "camel", MatchesCamelCase, []filterCase{
		{word: "", target: "", noMatch: true},
		{word: "", target: "anything", want: m()},
		{word: "alpha", target: "alpha", want: m(0, 5)},
		{word: "AlPhA", target: "alpha", want: m(0, 5)},
		{word: "alpha", target: "alphasomething", want: m(0, 5)},
		{word: "alpha", target: "alp", noMatch: true},
		{word: "c", target: "CamelCaseRocks", want: m(0, 1)},
		{word: "cc", target: "CamelCaseRocks", want: m(0, 1, 5, 6)},
		{word: "ccr", target: "CamelCaseRocks", want: m(0, 1, 5, 6, 9, 10)},
		{word: "cacr", target: "CamelCaseRocks", want: m(0, 2, 5, 6, 9, 10)},
		{word: "cacar", target: "CamelCaseRocks", want: m(0, 2, 5, 7, 9, 10)},
		{word: "ccarocks", target: "CamelCaseRocks", want: m(0, 1, 5, 7, 9, 14)},
		{word: "cr", target: "CamelCaseRocks", want: m(0, 1, 9, 10)},
		{word: "fba", target: "FooBarAbe", want: m(0, 1, 3, 5)},
		{word: "fbar", target: "FooBarAbe", want: m(0, 1, 3, 6)},
		{word: "fbara", target: "FooBarAbe", want: m(0, 1, 3, 7)},
		{word: "fbaa", target: "FooBarAbe", want: m(0, 1, 3, 5, 6, 7)},
		{word: "fbaab", target: "FooBarAbe", want: m(0, 1, 3, 5, 6, 8)},
		{word: "c2d", target: "canvasCreation2D", want: m(0, 1, 14, 16)},
		{word: "cce", target: "_canvasCreationEvent", want: m(1, 2, 7, 8, 15, 16)},
		{word: "Debug Console", target: "Open: Debug Console", anyMatch: true},
		{word: "Debug console", target: "Open: Debug Console", anyMatch: true},
		{word: "debug console", target: "Open: Debug Console", anyMatch: true},
	})
}

func TestMatchesSubstrings(t *testing.T) {
	runFilter(t, "contiguous", MatchesContiguousSubString, []filterCase{
		{word: "cela", target: "cancelAnimationFrame()", want: m(3, 7)},
	})
	runFilter(t, "base", MatchesBaseContiguousSubString, []filterCase{
		{word: "cela", target: "cancelAnimationFrame()", want: m(3, 7)},
		{word: "cafe", target: "café", want: m(0, 4)},
		{word: "cafe", target: "caféBar", want: m(0, 4)},
		{word: "resume", target: "résumé", want: m(0, 6)},
		{word: "naïve", target: "naïve", want: m(0, 5)},
		{word: "naive", target: "naïve", want: m(0, 5)},
		{word: "aeou", target: "àéöü", want: m(0, 4)},
	})
	runFilter(t, "substring", MatchesSubString, []filterCase{
		{word: "cmm", target: "cancelAnimationFrame()", want: m(0, 1, 9, 10, 18, 19)},
		{word: "abc", target: "abcabc", want: m(0, 3)},
		{word: "abc", target: "aaabbbccc", want: m(0, 1, 3, 4, 6, 7)},
		{word: "aaaaaaaaaaaaaaaaaaaax", target: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", noMatch: true},
	})
}

func TestMatchesWords(t *testing.T) {
	words := func(w, tg string) ([]Match, bool) { return MatchesWords(w, tg, false) }
	runFilter(t, "words", words, []filterCase{
		{word: "alpha", target: "alpha", want: m(0, 5)},
		{word: "alpha", target: "alphasomething", want: m(0, 5)},
		{word: "alpha", target: "alp", noMatch: true},
		{word: "a", target: "alpha", want: m(0, 1)},
		{word: "x", target: "alpha", noMatch: true},
		{word: "A", target: "alpha", want: m(0, 1)},
		{word: "AlPh", target: "alPHA", want: m(0, 4)},
		{word: "Debug Console", target: "Open: Debug Console", anyMatch: true},
		{word: "gp", target: "Git: Pull", want: m(0, 1, 5, 6)},
		{word: "g p", target: "Git: Pull", want: m(0, 1, 5, 6)},
		{word: "gipu", target: "Git: Pull", want: m(0, 2, 5, 7)},
		{word: "gp", target: "Category: Git: Pull", want: m(10, 11, 15, 16)},
		{word: "gipu", target: "Category: Git: Pull", want: m(10, 12, 15, 17)},
		{word: "it", target: "Git: Pull", noMatch: true},
		{word: "ll", target: "Git: Pull", noMatch: true},
		{word: "git: プル", target: "git: プル", want: m(0, 7)},
		{word: "git プル", target: "git: プル", want: m(0, 3, 5, 7)},
		{word: "öäk", target: "Öhm: Älles Klar", want: m(0, 1, 5, 6, 11, 12)},
		{word: "C++", target: "C/C++: command", want: m(2, 5)},
		{word: ".", target: ":", want: m()},
		{word: ".", target: ".", want: m(0, 1)},
		{word: "bar", target: "foo-bar", anyMatch: true},
		{word: "bar test", target: "foo-bar test", anyMatch: true},
		{word: "fbt", target: "foo-bar test", anyMatch: true},
		{word: "bar test", target: "foo-bar (test)", anyMatch: true},
		{word: "foo bar", target: "foo (bar)", anyMatch: true},
		{word: "bar est", target: "foo-bar test", noMatch: true},
		{word: "fo ar", target: "foo-bar test", noMatch: true},
		{word: "for", target: "foo-bar test", noMatch: true},
		{word: "foo bar", target: "foo-bar", anyMatch: true},
		{word: "foo bar", target: "123 foo-bar 456", anyMatch: true},
		{word: "foo-bar", target: "foo bar", anyMatch: true},
		{word: "foo:bar", target: "foo:bar", anyMatch: true},
	})
	t.Run("contiguous", func(t *testing.T) {
		if got, ok := MatchesWords("pul", "Git: Pull", true); !ok || !reflect.DeepEqual(got, m(5, 8)) {
			t.Errorf("pul: %v %v", got, ok)
		}
		if _, ok := MatchesWords("gipu", "Category: Git: Pull", true); ok {
			t.Errorf("gipu matched contiguously")
		}
	})
	t.Run("performance", func(t *testing.T) {
		target := "editor.action.editor.action.editor.action.editor.action.editor.action.editor.action.x"
		start := time.Now()
		MatchesWords("editor.action.z", target, false)
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("took %v", d)
		}
	})
}

func TestMatchesFuzzy(t *testing.T) {
	f := func(w, tg string) ([]Match, bool) { return MatchesFuzzy(w, tg, true) }
	runFilter(t, "fuzzy", f, []filterCase{
		{word: "sn", target: "Insert Snippet", want: m(7, 9)},
		{word: "is", target: "Insert Snippet", want: m(0, 1, 7, 8)},
		{word: "snt", target: "Insert Snippet", want: m(2, 3, 8, 9, 13, 14)},
		{word: "ins*pet", target: "Insert Snippet", want: m(0, 14)},
		{word: "xyz", target: "Insert Snippet", noMatch: true},
		{word: "", target: "Insert Snippet", want: m(0, 0)},
	})
	contiguous := func(w, tg string) ([]Match, bool) { return MatchesFuzzy(w, tg, false) }
	runFilter(t, "contiguous", contiguous, []filterCase{
		{word: "snt", target: "Insert Snippet", noMatch: true},
	})
}

func TestMatchesCommandWords(t *testing.T) {
	runFilter(t, "command", MatchesCommandWords, []filterCase{
		{word: "line", target: "Go to Line/Column...", want: m(6, 10)},
		{word: "gtl", target: "Go to Line/Column...", want: m(0, 1, 3, 4, 6, 7)},
		{word: "toggle word", target: "View: Toggle Word Wrap", want: m(6, 17)},
		{word: "zzz", target: "View: Toggle Word Wrap", noMatch: true},
	})
}

func TestFuzzyContains(t *testing.T) {
	tests := []struct {
		target, query string
		want          bool
	}{
		{"Side Bar", "sbr", true},
		{"Side Bar", "bs", false},
		{"Side Bar", "", false},
		{"", "a", false},
		{"ab", "abc", false},
		{"İstanbul", "ist", true},
	}
	for _, tc := range tests {
		t.Run(tc.target+"|"+tc.query, func(t *testing.T) {
			if got := FuzzyContains(tc.target, tc.query); got != tc.want {
				t.Errorf("FuzzyContains(%q, %q) = %v", tc.target, tc.query, got)
			}
		})
	}
}

func TestCompareAnythingAndCollate(t *testing.T) {
	tests := []struct {
		a, b, look string
		want       int
	}{
		{"snippet", "insert snippet", "snip", -1},
		{"insert snippet", "snippet", "snip", 1},
		{"b", "a", "x", 1},
		{"a", "a", "x", 0},
	}
	for _, tc := range tests {
		t.Run(tc.a+"|"+tc.b, func(t *testing.T) {
			got := CompareAnything(tc.a, tc.b, tc.look)
			if (got < 0) != (tc.want < 0) || (got > 0) != (tc.want > 0) {
				t.Errorf("CompareAnything = %d, want sign of %d", got, tc.want)
			}
		})
	}
	if Collate("a", "B") >= 0 || Collate("B", "a") <= 0 || Collate("a", "a") != 0 {
		t.Errorf("Collate order wrong")
	}
}
