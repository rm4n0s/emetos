package textmate

import (
	"testing"
	"time"
)

// fuzzGrammar has the features that can make a tokenizer loop or fail: nested begin/end and
// begin/while rules, back references, captures with patterns, includes of itself, rules that
// match nothing, a begin that takes the line break, and injections.
const fuzzGrammar = `"patterns": [
	{"include": "#quote"}, {"include": "#doc"}, {"include": "#block"}, {"include": "#empty"}, {"include": "#words"}],
	"repository": {
		"quote": {"begin": "^>", "while": "^>", "name": "quote", "patterns": [{"include": "$self"}]},
		"doc": {"begin": "<<(\\w*)\\n?", "end": "^\\1$", "name": "doc", "patterns": [{"include": "#words"}]},
		"block": {"begin": "\\(", "end": "\\)|(?=x)", "name": "block", "beginCaptures": {"0": {"name": "open"}},
			"patterns": [{"include": "$self"}, {"match": "(?=[a-z])", "name": "look"}]},
		"empty": {"match": "(?=\\s)", "name": "nothing"},
		"words": {"match": "(\\w+)(\\s*)(\\w*)", "captures": {"1": {"name": "one", "patterns": [{"match": "\\w", "name": "ch"}]}, "3": {"name": "three"}}}
	},
	"injections": {"source.fuzz - meta.none": {"patterns": [{"match": "\\d+", "name": "num"}, {"begin": "\\{", "end": "\\}", "name": "brace"}]}}`

// FuzzTokenizeLine checks that no two lines make the tokenizer panic or run for long, and that
// the tokens cover the line.
func FuzzTokenizeLine(f *testing.F) {
	for _, seed := range [][2]string{
		{"> a (b", "> c) d"}, {"<<EOF", "x EOF"}, {"((((", "))))"}, {"", ""}, {"a b c", "12 {3"}, {"\xff\xfe(", "é)"}, {"<<", ""},
	} {
		f.Add(seed[0], seed[1])
	}
	raw, err := ParseGrammar([]byte(`{"scopeName": "source.fuzz", ` + fuzzGrammar + `}`))
	if err != nil {
		f.Fatal(err)
	}
	reg := newRegistry()
	reg.grammars[raw.ScopeName] = raw
	g, err := NewGrammar("source.fuzz", reg, GrammarOptions{})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		start := time.Now()
		var state *State
		for _, line := range []string{a, b, a} {
			res := g.TokenizeLine(line, state, time.Second)
			if len(res.Tokens) == 0 || res.Tokens[0].Start != 0 || res.Tokens[len(res.Tokens)-1].End != len(line) {
				t.Fatalf("tokens do not cover %q: %+v", line, res.Tokens)
			}
			for i := 1; i < len(res.Tokens); i++ {
				if res.Tokens[i].Start != res.Tokens[i-1].End {
					t.Fatalf("tokens of %q are not contiguous: %+v", line, res.Tokens)
				}
			}
			state = res.State
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("tokenizing took %v", time.Since(start))
		}
	})
}

// FuzzParseGrammar checks that no JSON makes ParseGrammar panic.
func FuzzParseGrammar(f *testing.F) {
	for _, seed := range []string{
		`{"scopeName": "a"}`, `{"scopeName": "a", "patterns": [{"match": "x", "captures": {"1": {"name": "y"}}}]}`,
		`{"scopeName": "a", "injections": {"x": {"patterns": []}}}`, `[]`, `{"scopeName": 1}`, ``,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data string) {
		_, _ = ParseGrammar([]byte(data))
	})
}
