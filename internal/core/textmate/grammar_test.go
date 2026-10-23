package textmate

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/errors"
)

func TestParseGrammarErrors(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"not JSON", `{`},
		{"not an object", `[]`},
		{"no scope name", `{"patterns": []}`},
		{"scope name is a number", `{"scopeName": 3}`},
		{"patterns is not an array", `{"scopeName": "a", "patterns": 3}`},
		{"a pattern is not an object", `{"scopeName": "a", "patterns": [3]}`},
		{"repository is not an object", `{"scopeName": "a", "repository": []}`},
		{"a rule of the repository is not an object", `{"scopeName": "a", "repository": {"x": 1}}`},
		{"fileTypes is not strings", `{"scopeName": "a", "fileTypes": [1]}`},
		{"captures is not an object", `{"scopeName": "a", "patterns": [{"captures": 1}]}`},
		{"injections is not an object", `{"scopeName": "a", "injections": []}`},
		{"an injection is not an object", `{"scopeName": "a", "injections": {"x": 1}}`},
		{"applyEndPatternLast is not a boolean", `{"scopeName": "a", "patterns": [{"applyEndPatternLast": "yes"}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseGrammar([]byte(tc.json))
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("ParseGrammar") || !appErr.HasRoute("GrammarInvalid") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestParseGrammarReadsEveryField(t *testing.T) {
	raw, err := ParseGrammar([]byte(`{
		"scopeName": "source.x", "name": "X", "fileTypes": ["x", "xx"], "firstLineMatch": "^#x", "injectionSelector": "L:source.y",
		"patterns": [{"include": "#a"}, {"begin": "b", "end": "c", "name": "n", "contentName": "cn", "applyEndPatternLast": 1,
			"beginCaptures": {"1": {"name": "bc"}}, "endCaptures": {}, "patterns": []}, {"match": "m", "captures": {"0": {"name": "z"}}},
			{"begin": "w", "while": "v", "whileCaptures": {"1": {"name": "wc"}}}],
		"repository": {"a": {"match": "a"}},
		"injections": {"zz": {"match": "1"}, "aa": {"match": "2"}, "mm": {"match": "3"}},
		"unknown": 5
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if raw.ScopeName != "source.x" || raw.Name != "X" || strings.Join(raw.FileTypes, ",") != "x,xx" || raw.FirstLineMatch != "^#x" || raw.InjectionSelector != "L:source.y" {
		t.Errorf("header: %+v", raw)
	}
	if len(raw.Patterns) != 4 || raw.Patterns[0].Include != "#a" {
		t.Fatalf("patterns: %+v", raw.Patterns)
	}
	b := raw.Patterns[1]
	if !b.HasBegin || b.Begin != "b" || b.End != "c" || b.Name != "n" || b.ContentName != "cn" || !b.ApplyEndPatternLast || !b.HasPatterns ||
		b.BeginCaptures["1"].Name != "bc" || b.EndCaptures == nil || len(b.EndCaptures) != 0 {
		t.Errorf("begin rule: %+v", b)
	}
	if raw.Patterns[2].Match != "m" || raw.Patterns[2].Captures["0"].Name != "z" {
		t.Errorf("match rule: %+v", raw.Patterns[2])
	}
	if w := raw.Patterns[3]; w.While != "v" || w.WhileCaptures["1"].Name != "wc" {
		t.Errorf("while rule: %+v", w)
	}
	if raw.Repository["a"].Match != "a" {
		t.Errorf("repository: %+v", raw.Repository)
	}
	var order []string
	for _, inj := range raw.Injections {
		order = append(order, inj.Selector+"="+inj.Rule.Match)
	}
	if strings.Join(order, " ") != "zz=1 aa=2 mm=3" {
		t.Errorf("injections must keep the order of the file: %v", order)
	}
}

func TestNewGrammarNeedsTheGrammar(t *testing.T) {
	_, err := NewGrammar("source.none", firstMateRegistry{}, GrammarOptions{})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("NewGrammar.GrammarNotFound") {
		t.Errorf("err = %v", err)
	}
}

func TestScopeStacks(t *testing.T) {
	var s *ScopeStack
	if s.Len() != 0 || len(s.Names()) != 0 || s.Top() != "" || !s.Equal(nil) {
		t.Error("the nil stack has no scopes")
	}
	a := s.pushPath("a b", true).pushPath("c", true)
	if got := strings.Join(a.Names(), ","); got != "a,b,c" || a.Len() != 3 || a.Top() != "c" {
		t.Errorf("names %q, len %d, top %q", got, a.Len(), a.Top())
	}
	if a.pushPath("", false) != a {
		t.Error("no name adds no scope")
	}
	if got := strings.Join(s.pushPath("", true).Names(), ","); got != "" || s.pushPath("", true).Len() != 1 {
		t.Errorf("an empty name is a scope: %q", got)
	}
	if got := strings.Join(s.pushPath("a  b", true).Names(), ","); got != "a,,b" {
		t.Errorf("two spaces: %q", got)
	}
	b := s.push("a").push("b").push("c")
	if !a.Equal(b) || a.Equal(b.parent) || a.Equal(nil) || a.Equal(s.push("a").push("x").push("c")) {
		t.Error("stacks are equal when their scopes are")
	}
}

func TestStatesAreEqualWhenTheNextLineWouldBeTheSame(t *testing.T) {
	g := tokenizerOf(t, "test", grammarOf(t, "test", `"patterns": [
		{"begin": "/\\*", "end": "\\*/", "name": "comment"},
		{"begin": "(\\w+)<<", "end": "\\1", "name": "doc"}]`))
	state := func(lines ...string) *State {
		var s *State
		for _, l := range lines {
			s = g.TokenizeLine(l, s, 0).State
		}
		return s
	}
	tests := []struct {
		name string
		a, b *State
		want bool
	}{
		{"the same line", state("/* a"), state("/* a"), true},
		{"other text in the same comment", state("/* a"), state("/* b", "c"), true},
		{"not open and open", state("a"), state("/* a"), false},
		{"open and closed", state("/* a */"), state("/* a"), false},
		{"closed twice", state("/* a */"), state("b"), true},
		{"other end patterns", state("a<<"), state("b<<"), false},
		{"the same end pattern", state("a<<", "x"), state("a<<"), true},
		{"nil and a state", nil, state("a"), false},
		{"nil and nil", nil, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Equal(tc.b); got != tc.want {
				t.Errorf("Equal = %v, want %v", got, tc.want)
			}
			if got := tc.b.Equal(tc.a); got != tc.want {
				t.Errorf("Equal the other way = %v, want %v", got, tc.want)
			}
		})
	}
	if d := state("/* a").Depth(); d != 2 {
		t.Errorf("Depth() = %d, want 2", d)
	}
	var none *State
	if none.Depth() != 0 || none.Scopes() != nil {
		t.Error("the nil state is empty")
	}
	if got := strings.Join(state("/* a").Scopes().Names(), " "); got != "test comment" {
		t.Errorf("Scopes() = %q", got)
	}
}

func TestALongLineIsOneToken(t *testing.T) {
	reg := firstMateRegistry{"test": grammarOf(t, "test", `"patterns": [{"match": "a", "name": "a"}]`)}
	g, err := NewGrammar("test", reg, GrammarOptions{MaxLineBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	first := g.TokenizeLine("aa", nil, 0)
	long := g.TokenizeLine(strings.Repeat("a", 10), first.State, 0)
	if len(long.Tokens) != 1 || long.Tokens[0].Start != 0 || long.Tokens[0].End != 10 || long.Tokens[0].Scopes != nil {
		t.Errorf("tokens = %+v", long.Tokens)
	}
	if long.State != first.State || long.StoppedEarly {
		t.Error("a line that is not tokenized leaves the state as it was")
	}
	if got := g.TokenizeLine(strings.Repeat("a", 9), first.State, 0); len(got.Tokens) != 9 {
		t.Errorf("a line below the limit is tokenized: %d tokens", len(got.Tokens))
	}
	if g, _ := NewGrammar("test", reg, GrammarOptions{}); g.maxLineBytes != DefaultMaxLineBytes {
		t.Errorf("default limit = %d", g.maxLineBytes)
	}
}

func TestTheTimeLimitStopsALine(t *testing.T) {
	g := tokenizerOf(t, "test", grammarOf(t, "test", `"patterns": [{"match": "a", "name": "a"}]`))
	res := g.TokenizeLine(strings.Repeat("a", 5000), nil, time.Nanosecond)
	if !res.StoppedEarly {
		t.Fatal("a line must stop at its time limit")
	}
	if len(res.Tokens) == 0 || res.Tokens[len(res.Tokens)-1].End != 5000 {
		t.Errorf("the tokens must cover the line: %d tokens", len(res.Tokens))
	}
	if g.TokenizeLine("aaa", nil, time.Minute).StoppedEarly {
		t.Error("a line within its limit")
	}
}

func TestAPatternPastItsLimitMakesTheRestPlain(t *testing.T) {
	g := tokenizerOf(t, "test", grammarOf(t, "test", `"patterns": [{"match": "(a+)+$", "name": "bad"}, {"match": "x", "name": "x"}]`))
	start := time.Now()
	res := g.TokenizeLine("x"+strings.Repeat("a", 64)+"!", nil, 0)
	if !res.StoppedEarly {
		t.Fatal("the search must stop")
	}
	if len(res.Tokens) != 1 || res.Tokens[0].End != 66 {
		t.Errorf("the line is plain text from the start: %+v", res.Tokens)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("it took %v", time.Since(start))
	}
	var found bool
	for _, p := range g.Problems() {
		if appErr, ok := errors.FromError(p); ok && appErr.HasRoute("RegexTimeout") {
			found = true
		}
	}
	if !found {
		t.Errorf("problems = %v", g.Problems())
	}
}

func TestProblemsAreReportedOnce(t *testing.T) {
	g := tokenizerOf(t, "test", grammarOf(t, "test", `"patterns": [{"match": "(", "name": "a"}, {"match": "\\p{Nope}", "name": "b"},
		{"begin": "w", "while": "v"}, {"match": "ok", "name": "ok"}]`))
	for i := 0; i < 3; i++ {
		g.TokenizeLine("ok (", nil, 0)
	}
	want := map[string]bool{"RegexUnbalancedParen": false, "RegexUnknownProperty": false, "RuleWhileUnsupported": false}
	for _, p := range g.Problems() {
		appErr, ok := errors.FromError(p)
		if !ok {
			t.Fatalf("problem %v", p)
		}
		for tag := range want {
			if appErr.HasRoute(tag) {
				if want[tag] {
					t.Errorf("%s reported twice", tag)
				}
				want[tag] = true
			}
		}
	}
	for tag, seen := range want {
		if !seen {
			t.Errorf("no problem %s in %v", tag, g.Problems())
		}
	}
}

func TestAGrammarIsSafeForConcurrentUse(t *testing.T) {
	g := tokenizerOf(t, "test", grammarOf(t, "test", `"patterns": [
		{"begin": "(\\w+)<<", "end": "\\1", "name": "doc"}, {"match": "\\d+", "name": "num"}]`))
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := string(rune('a' + n))
			var state *State
			for i := 0; i < 100; i++ {
				state = g.TokenizeLine(name+"<<", state, 0).State
				res := g.TokenizeLine("12 "+name, state, 0)
				if len(res.Tokens) != 2 || state.Depth() != 2 {
					t.Errorf("tokens = %+v, depth %d", res.Tokens, state.Depth())
					return
				}
				state = g.TokenizeLine(name, state, 0).State
				if state.Depth() != 1 {
					t.Errorf("the rule must end: depth %d", state.Depth())
					return
				}
			}
		}(n)
	}
	wg.Wait()
}

func TestIncludesOfOtherGrammars(t *testing.T) {
	outer := grammarOf(t, "outer", `"patterns": [{"begin": "\\{", "end": "\\}", "name": "block", "patterns": [{"include": "inner"}, {"include": "inner#kw"}, {"include": "$base"}]}, {"match": "o", "name": "o"}]`)
	inner := grammarOf(t, "inner", `"patterns": [{"match": "i", "name": "i"}, {"include": "$self"}], "repository": {"kw": {"match": "k", "name": "kw"}}`)
	g := tokenizerOf(t, "outer", outer, inner)
	got := show(g, "{iok}")
	want := "{|block ; i|block i ; o|block o ; k|block kw ; }|block"
	if got[0] != want {
		t.Errorf("tokens:\n%s\nwant:\n%s", got[0], want)
	}
}

func TestTheScopeOfTheGrammarIsTheFirstScope(t *testing.T) {
	g := tokenizerOf(t, "source.test", grammarOf(t, "source.test", `"patterns": []`))
	res := g.TokenizeLine("abc", nil, 0)
	if len(res.Tokens) != 1 || strings.Join(res.Tokens[0].Scopes.Names(), " ") != "source.test" {
		t.Errorf("tokens = %+v", res.Tokens)
	}
	if g.ScopeName() != "source.test" {
		t.Errorf("ScopeName() = %q", g.ScopeName())
	}
}

func TestBackReferencesAreEscaped(t *testing.T) {
	src := newRegexSource(`a\1b\2c\9`, 0)
	if !src.hasBackReferences {
		t.Fatal("no back references")
	}
	got := src.resolveBackReferences("x.+ y", []regex.Capture{{Start: 0, End: 5}, {Start: 0, End: 3}, {Start: -1, End: -1}})
	if want := `ax\.\+b` + `c`; got != want {
		t.Errorf("resolved %q, want %q", got, want)
	}
	if newRegexSource(`abc`, 0).hasBackReferences || newRegexSource(``, 0).source != "" {
		t.Error("references in a pattern without any")
	}
	if got := newRegexSource(`a\zb\\zc\z`, 0).source; got != `a$(?!\n)(?<!\n)b\\zc$(?!\n)(?<!\n)` {
		t.Errorf("\\z became %q", got)
	}
	if got := escapeRegExpCharacters("a-b\\ c\n,ü"); got != "a\\-b\\\\\\ c\\\n\\,ü" {
		t.Errorf("escaped %q", got)
	}
}
