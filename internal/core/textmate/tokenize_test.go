package textmate

import (
	"fmt"
	"strings"
	"testing"
)

// grammarOf parses a grammar written as JSON.
func grammarOf(t *testing.T, scope, body string) *RawGrammar {
	t.Helper()
	raw, err := ParseGrammar([]byte(`{"scopeName": "` + scope + `", ` + body + `}`))
	if err != nil {
		t.Fatalf("grammar %s: %v", scope, err)
	}
	return raw
}

// tokenizerOf makes the grammar with the scope from the grammars given.
func tokenizerOf(t *testing.T, scope string, grammars ...*RawGrammar) *Grammar {
	t.Helper()
	reg := firstMateRegistry{}
	for _, g := range grammars {
		reg[g.ScopeName] = g
	}
	g, err := NewGrammar(scope, reg, GrammarOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// show writes the tokens of lines as "text|scope scope" per token, one line per line of text.
func show(g *Grammar, lines ...string) []string {
	var out []string
	var state *State
	for _, line := range lines {
		res := g.TokenizeLine(line, state, 0)
		var parts []string
		for _, tok := range res.Tokens {
			names := tok.Scopes.Names()
			parts = append(parts, fmt.Sprintf("%s|%s", line[tok.Start:tok.End], strings.Join(names[1:], " ")))
		}
		out = append(out, strings.Join(parts, " ; "))
		state = res.State
	}
	return out
}

func TestTokenizerFeatures(t *testing.T) {
	tests := []struct {
		name    string
		grammar string
		lines   []string
		want    []string
	}{
		{
			name:    "a match rule",
			grammar: `"patterns": [{"match": "\\bfoo\\b", "name": "kw"}]`,
			lines:   []string{"a foo b"},
			want:    []string{"a | ; foo|kw ;  b|"},
		},
		{
			name:    "an empty line has one empty token",
			grammar: `"patterns": [{"match": "x", "name": "kw"}]`,
			lines:   []string{""},
			want:    []string{"|"},
		},
		{
			name:    "begin and end span lines",
			grammar: `"patterns": [{"begin": "/\\*", "end": "\\*/", "name": "comment", "contentName": "inside"}]`,
			lines:   []string{"a /* b", "c", "d */ e"},
			want:    []string{"a | ; /*|comment ;  b|comment inside", "c|comment inside", "d |comment inside ; */|comment ;  e|"},
		},
		{
			name:    "an unnamed rule adds no scope",
			grammar: `"patterns": [{"begin": "\\(", "end": "\\)", "patterns": [{"match": "x", "name": "x"}]}]`,
			lines:   []string{"(axb)"},
			want:    []string{"(| ; a| ; x|x ; b| ; )|"},
		},
		{
			name:    "a name with several scopes",
			grammar: `"patterns": [{"match": "x", "name": "a.b c.d"}]`,
			lines:   []string{"x"},
			want:    []string{"x|a.b c.d"},
		},
		{
			name:    "names take the text of captures",
			grammar: `"patterns": [{"match": "(\\w+)=(\\w+)", "name": "kv.$1.${2:/upcase}.${1:/downcase}"}]`,
			lines:   []string{"Key=val"},
			want:    []string{"Key=val|kv.Key.VAL.key"},
		},
		{
			name:    "leading dots of captured text are taken off",
			grammar: `"patterns": [{"match": "(\\.+\\w+)", "name": "tag.$1"}]`,
			lines:   []string{"..ab"},
			want:    []string{"..ab|tag.ab"},
		},
		{
			name:    "a capture that is not there stays as written",
			grammar: `"patterns": [{"match": "x", "name": "a.$3"}]`,
			lines:   []string{"x"},
			want:    []string{"x|a.$3"},
		},
		{
			name:    "captures give scopes to groups",
			grammar: `"patterns": [{"match": "(a)(b(c))", "captures": {"1": {"name": "one"}, "2": {"name": "two"}, "3": {"name": "three"}}}]`,
			lines:   []string{"abc"},
			want:    []string{"a|one ; b|two ; c|two three"},
		},
		{
			name:    "captures of begin and end",
			grammar: `"patterns": [{"begin": "(<)", "end": "(>)", "beginCaptures": {"1": {"name": "open"}}, "endCaptures": {"1": {"name": "close"}}, "name": "tag"}]`,
			lines:   []string{"<a>"},
			want:    []string{"<|tag open ; a|tag ; >|tag close"},
		},
		{
			name:    "captures is the default of beginCaptures and endCaptures",
			grammar: `"patterns": [{"begin": "(<)", "end": "(>)", "captures": {"1": {"name": "edge"}}}]`,
			lines:   []string{"<a>"},
			want:    []string{"<|edge ; a| ; >|edge"},
		},
		{
			name:    "empty beginCaptures hides captures",
			grammar: `"patterns": [{"begin": "(<)", "end": "(>)", "beginCaptures": {}, "captures": {"1": {"name": "edge"}}}]`,
			lines:   []string{"<a>"},
			want:    []string{"<| ; a| ; >|edge"},
		},
		{
			name: "a capture with patterns is tokenized again",
			grammar: `"patterns": [{"match": "x(\\d+)y", "captures": {"1": {"name": "num", "patterns": [{"match": "\\d", "name": "digit"}]}}}],
				"repository": {}`,
			lines: []string{"x12y"},
			want:  []string{"x| ; 1|num digit ; 2|num digit ; y|"},
		},
		{
			name:    "an end that refers to the begin",
			grammar: `"patterns": [{"begin": "(['\"])", "end": "\\1", "name": "string"}]`,
			lines:   []string{`'a"b' "c'd"`},
			want:    []string{`'|string ; a"b|string ; '|string ;  | ; "|string ; c'd|string ; "|string`},
		},
		{
			name:    "text captured in an end is escaped",
			grammar: `"patterns": [{"begin": "(\\*+)", "end": "\\1", "name": "stars"}]`,
			lines:   []string{`** a * b **`},
			want:    []string{`**|stars ;  a * b |stars ; **|stars`},
		},
		{
			name:    "applyEndPatternLast",
			grammar: `"patterns": [{"begin": "a", "end": "b", "applyEndPatternLast": true, "patterns": [{"match": "b+", "name": "inner"}]}]`,
			lines:   []string{"abbb"},
			want:    []string{"a| ; bbb|inner"},
		},
		{
			name:    "the end goes first by default",
			grammar: `"patterns": [{"begin": "a", "end": "b", "patterns": [{"match": "b+", "name": "inner"}]}]`,
			lines:   []string{"abbb"},
			want:    []string{"a| ; b| ; bb|"},
		},
		{
			name:    "a begin that has no end continues to the end of the text",
			grammar: `"patterns": [{"begin": "\"", "name": "string"}]`,
			lines:   []string{`"a`, `b`},
			want:    []string{`"|string ; a|string`, `b|string`},
		},
		{
			name:    "the anchor position is the end of begin",
			grammar: `"patterns": [{"begin": "<<", "end": "\\Gx|y", "name": "r"}]`,
			lines:   []string{"<<xy"},
			want:    []string{"<<|r ; x|r ; y|"},
		},
		{
			name:    "include of a repository rule",
			grammar: `"patterns": [{"include": "#word"}], "repository": {"word": {"match": "\\w+", "name": "word"}}`,
			lines:   []string{"ab cd"},
			want:    []string{"ab|word ;  | ; cd|word"},
		},
		{
			name:    "include of self inside a rule",
			grammar: `"patterns": [{"begin": "\\(", "end": "\\)", "name": "paren", "patterns": [{"include": "$self"}]}, {"match": "x", "name": "x"}]`,
			lines:   []string{"(x(x))"},
			want:    []string{"(|paren ; x|paren x ; (|paren paren ; x|paren paren x ; )|paren paren ; )|paren"},
		},
		{
			name:    "include of a rule that has only an include",
			grammar: `"patterns": [{"include": "#alias"}], "repository": {"alias": {"include": "#word"}, "word": {"match": "w", "name": "w"}}`,
			lines:   []string{"w"},
			want:    []string{"w|w"},
		},
		{
			name:    "a repository inside a rule",
			grammar: `"patterns": [{"patterns": [{"include": "#local"}], "repository": {"local": {"match": "l", "name": "local"}}}]`,
			lines:   []string{"l"},
			want:    []string{"l|local"},
		},
		{
			name:    "an include that is not there is left out",
			grammar: `"patterns": [{"include": "#nothing"}, {"include": "other.scope"}, {"include": "other.scope#rule"}, {"match": "a", "name": "a"}]`,
			lines:   []string{"a"},
			want:    []string{"a|a"},
		},
		{
			name:    "a rule whose patterns are all missing is left out",
			grammar: `"patterns": [{"begin": "x", "end": "y", "patterns": [{"include": "#nothing"}]}, {"match": "x", "name": "plain"}]`,
			lines:   []string{"x"},
			want:    []string{"x|plain"},
		},
		{
			name:    "a pattern that does not compile never matches",
			grammar: `"patterns": [{"match": "(unclosed", "name": "bad"}, {"match": "ok", "name": "ok"}]`,
			lines:   []string{"(unclosed ok"},
			want:    []string{"(unclosed | ; ok|ok"},
		},
		{
			name:    "a rule that matches nothing and does not advance",
			grammar: `"patterns": [{"match": "(?=a)", "name": "look"}, {"match": "a", "name": "a"}]`,
			lines:   []string{"ba"},
			want:    []string{"b| ; a|"},
		},
		{
			name:    "a begin that matches the line break anchors the next line",
			grammar: `"patterns": [{"begin": "<<\\n", "end": "\\G(x)", "name": "doc", "endCaptures": {"1": {"name": "ex"}}}]`,
			lines:   []string{"<<", "xy"},
			want:    []string{"<<|doc", "x|doc ex ; y|"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := tokenizerOf(t, "test", grammarOf(t, "test", tc.grammar))
			got := show(g, tc.lines...)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("tokens:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}
