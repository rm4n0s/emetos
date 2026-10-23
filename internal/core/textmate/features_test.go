package textmate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The tests of scope selectors are those of vscode-textmate (src/tests/matcher.test.ts), with
// its matcher of names that needs the names to be equal.
func TestScopeSelectors(t *testing.T) {
	equalNames := func(names, scopes []string) bool {
		last := 0
		for _, name := range names {
			found := false
			for i := last; i < len(scopes); i++ {
				if scopes[i] == name {
					last = i + 1
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	tests := []struct {
		expression string
		input      []string
		want       bool
	}{
		{"foo", []string{"foo"}, true},
		{"foo", []string{"bar"}, false},
		{"- foo", []string{"foo"}, false},
		{"- foo", []string{"bar"}, true},
		{"- - foo", []string{"bar"}, false},
		{"bar foo", []string{"foo"}, false},
		{"bar foo", []string{"bar"}, false},
		{"bar foo", []string{"bar", "foo"}, true},
		{"bar - foo", []string{"bar"}, true},
		{"bar - foo", []string{"foo", "bar"}, false},
		{"bar - foo", []string{"foo"}, false},
		{"bar, foo", []string{"foo"}, true},
		{"bar, foo", []string{"bar"}, true},
		{"bar, foo", []string{"bar", "foo"}, true},
		{"bar, -foo", []string{"bar", "foo"}, true},
		{"bar, -foo", []string{"yo"}, true},
		{"bar, -foo", []string{"foo"}, false},
		{"(foo)", []string{"foo"}, true},
		{"(foo - bar)", []string{"foo"}, true},
		{"(foo - bar)", []string{"foo", "bar"}, false},
		{"foo bar - (yo man)", []string{"foo", "bar"}, true},
		{"foo bar - (yo man)", []string{"foo", "bar", "yo"}, true},
		{"foo bar - (yo man)", []string{"foo", "bar", "yo", "man"}, false},
		{"foo bar - (yo | man)", []string{"foo", "bar", "yo", "man"}, false},
		{"foo bar - (yo | man)", []string{"foo", "bar", "yo"}, false},
		{"R:text.html - (comment.block, text.html source)", []string{"text.html", "bar", "source"}, false},
		{"text.html.php - (meta.embedded | meta.tag), L:text.html.php meta.tag, L:source.js.embedded.html", []string{"text.html.php", "bar", "source.js"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.expression, func(t *testing.T) {
			got := false
			for _, m := range parseSelector(tc.expression, equalNames) {
				got = got || m.match(tc.input)
			}
			if got != tc.want {
				t.Errorf("%q on %v = %v, want %v", tc.expression, tc.input, got, tc.want)
			}
		})
	}
}

func TestSelectorPriorities(t *testing.T) {
	tests := []struct {
		expression string
		priorities []int
	}{
		{"a", []int{0}},
		{"L:a", []int{-1}},
		{"R:a", []int{1}},
		{"L:a, R:b, c", []int{-1, 1, 0}},
		{"X:a", []int{0}},
		{"", nil},
	}
	for _, tc := range tests {
		var got []int
		for _, m := range parseSelector(tc.expression, nameMatcher) {
			got = append(got, m.priority)
		}
		if !reflect.DeepEqual(got, tc.priorities) {
			t.Errorf("%q: priorities %v, want %v", tc.expression, got, tc.priorities)
		}
	}
}

func TestNameMatcherMatchesPrefixes(t *testing.T) {
	tests := []struct {
		names, scopes string
		want          bool
	}{
		{"source.js", "source.js", true},
		{"source", "source.js", true},
		{"source.j", "source.js", false},
		{"source.js", "source", false},
		{"source.js meta", "source.js.embedded meta.block", true},
		{"meta source.js", "source.js meta.block", false},
		{"a b c", "a b", false},
		{"", "a", true},
		{"a", "", false},
	}
	for _, tc := range tests {
		names, scopes := strings.Fields(tc.names), strings.Fields(tc.scopes)
		if got := nameMatcher(names, scopes); got != tc.want {
			t.Errorf("nameMatcher(%q, %q) = %v, want %v", tc.names, tc.scopes, got, tc.want)
		}
	}
}

// whileLang is the grammar of the while tests of vscode-textmate (test-cases/suite1/fixtures/
// whileLang.plist) written as JSON.
const whileLang = `"patterns": [{"include": "#alist"}, {"include": "#blist"}],
	"repository": {
		"number": {"match": "[0-9]", "name": "number"},
		"letter": {"match": "x", "name": "letter"},
		"alist": {"begin": "A", "while": "a", "name": "alist",
			"patterns": [{"include": "#alist"}, {"include": "#blist"}, {"include": "#letter"}]},
		"blist": {"begin": "B", "while": "(^|\\G)(b)", "name": "blist", "whileCaptures": {"2": {"name": "bstart"}},
			"patterns": [{"include": "#alist"}, {"include": "#blist"}, {"include": "#number"}]}
	}`

func TestBeginWhileRulesOfVSCodeTextmate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(referenceDir(t), "suite1", "whileTests.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []testCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 9 {
		t.Fatalf("%d cases, want 9", len(cases))
	}
	g := tokenizerOf(t, "text.whileLang", grammarOf(t, "text.whileLang", whileLang))
	for _, tc := range cases {
		t.Run(tc.Desc, func(t *testing.T) {
			var state *State
			for _, tl := range tc.Lines {
				res := g.TokenizeLine(tl.Line, state, 0)
				var got []testToken
				for _, tok := range res.Tokens {
					got = append(got, testToken{Value: tl.Line[tok.Start:tok.End], Scopes: tok.Scopes.Names()})
				}
				if !reflect.DeepEqual(got, tl.Tokens) {
					t.Fatalf("line %q\n got  %v\n want %v", tl.Line, got, tl.Tokens)
				}
				state = res.State
			}
		})
	}
}

func TestBeginWhileFeatures(t *testing.T) {
	tests := []struct {
		name    string
		grammar string
		lines   []string
		want    []string
	}{
		{
			name:    "a quote lasts while lines start with >",
			grammar: `"patterns": [{"begin": "^>", "while": "^>", "name": "quote", "contentName": "text"}]`,
			lines:   []string{"> a", "> b", "c", "> d"},
			want:    []string{">|quote ;  a|quote text", ">|quote text ;  b|quote text", "c|", ">|quote ;  d|quote text"},
		},
		{
			name:    "a while pattern with a reference to the begin",
			grammar: `"patterns": [{"begin": "^(\\s*)-", "while": "^\\1  ", "name": "item"}]`,
			lines:   []string{"  - a", "    b", "  c"},
			want:    []string{"  -|item ;  a|item", "    |item ; b|item", "  c|"},
		},
		{
			name:    "captures of the while pattern",
			grammar: `"patterns": [{"begin": "^#", "while": "^(>)", "whileCaptures": {"1": {"name": "mark"}}, "name": "block"}]`,
			lines:   []string{"#", ">x", "y"},
			want:    []string{"#|block", ">|block mark ; x|block", "y|"},
		},
		{
			name:    "rules inside a while rule",
			grammar: `"patterns": [{"begin": "<<", "while": "^\\|", "name": "box", "patterns": [{"match": "\\d+", "name": "num"}]}]`,
			lines:   []string{"<< 1", "|2", "|", "3"},
			want:    []string{"<<|box ;  |box ; 1|box num", "||box ; 2|box num", "||box", "3|"},
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

func TestInjections(t *testing.T) {
	host := grammarOf(t, "source.host", `"patterns": [{"match": "\\w+", "name": "word"}],
		"injections": {
			"source.host": {"patterns": [{"match": "foo", "name": "inj.foo"}]},
			"L:source.host": {"patterns": [{"match": "bar", "name": "inj.bar.left"}]},
			"R:source.host": {"patterns": [{"match": "baz", "name": "inj.baz.right"}]},
			"source.host meta.never": {"patterns": [{"match": "qux", "name": "inj.never"}]}
		}`)
	g := tokenizerOf(t, "source.host", host)
	got := show(g, "foo bar baz qux")
	// L: before the word rule, R: after it, so baz is a word; foo starts at the same place as the
	// word rule and the injection (priority 0) comes second.
	want := "foo|word ;  | ; bar|inj.bar.left ;  | ; baz|word ;  | ; qux|word"
	if got[0] != want {
		t.Errorf("tokens:\n%s\nwant:\n%s", got[0], want)
	}
}

func TestInjectionsEarlierInTheLineWin(t *testing.T) {
	host := grammarOf(t, "source.host", `"patterns": [{"match": "[a-z]+", "name": "word"}],
		"injections": {"source.host": {"patterns": [{"match": "\\d+", "name": "num"}]}}`)
	g := tokenizerOf(t, "source.host", host)
	if got := show(g, "ab12cd")[0]; got != "ab|word ; 12|num ; cd|word" {
		t.Errorf("tokens: %s", got)
	}
}

func TestInjectionGrammarsComeFromTheRegistry(t *testing.T) {
	host := grammarOf(t, "source.host", `"patterns": [{"match": "[a-z]+", "name": "word"}]`)
	guest := grammarOf(t, "source.guest", `"injectionSelector": "L:source.host", "patterns": [{"match": "TODO", "name": "todo"}, {"include": "#x"}], "repository": {"x": {"match": "FIXME", "name": "fixme"}}`)
	other := grammarOf(t, "source.other", `"patterns": [{"match": "TODO", "name": "never"}]`)
	reg := newRegistry()
	for _, g := range []*RawGrammar{host, guest, other} {
		reg.grammars[g.ScopeName] = g
	}
	reg.injections["source.host"] = []string{"source.guest", "source.other", "source.missing"}
	g, err := NewGrammar("source.host", reg, GrammarOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := show(g, "ab TODO FIXME cd")[0]; got != "ab|word ;  | ; TODO|todo ;  | ; FIXME|fixme ;  | ; cd|word" {
		t.Errorf("tokens: %s", got)
	}
}

func TestTokenTypesAndLanguages(t *testing.T) {
	raw := grammarOf(t, "source.host", `"patterns": [
		{"match": "//.*", "name": "comment.line"},
		{"begin": "\"", "end": "\"", "name": "string.quoted", "patterns": [
			{"begin": "\\$\\{", "end": "\\}", "name": "meta.embedded.expression", "patterns": [{"match": "\\w+", "name": "variable.other"}]}]},
		{"match": "/\\w+/", "name": "string.regexp"},
		{"begin": "<css>", "end": "</css>", "name": "source.css.embedded.host", "patterns": [{"match": "[a-z]+", "name": "support.type.css"}]},
		{"match": "\\bkw\\b", "name": "keyword.control"},
		{"match": "!(\\w+)", "name": "meta.tag", "captures": {"1": {"name": "markup.raw"}}}]`)
	reg := newRegistry()
	reg.grammars[raw.ScopeName] = raw
	g, err := NewGrammar("source.host", reg, GrammarOptions{
		InitialLanguage:   1,
		EmbeddedLanguages: map[string]int{"source.css": 2, "source.css.embedded": 3},
		TokenTypes:        map[string]TokenType{"markup.raw": TokenString},
	})
	if err != nil {
		t.Fatal(err)
	}
	type tok struct {
		text string
		typ  TokenType
		lang int
	}
	tests := []struct {
		line string
		want []tok
	}{
		{"kw // c", []tok{{"kw", TokenOther, 1}, {" ", TokenOther, 1}, {"// c", TokenComment, 1}}},
		{`"a ${b} c"`, []tok{
			{`"`, TokenString, 1}, {"a ", TokenString, 1}, {"${", TokenOther, 1}, {"b", TokenOther, 1}, {"}", TokenOther, 1}, {" c", TokenString, 1}, {`"`, TokenString, 1}}},
		{"/re/ kw", []tok{{"/re/", TokenString, 1}, {" ", TokenOther, 1}, {"kw", TokenOther, 1}}},
		{"<css>ab</css>", []tok{{"<css>", TokenOther, 3}, {"ab", TokenOther, 3}, {"</css>", TokenOther, 3}}},
		{"!x", []tok{{"!", TokenOther, 1}, {"x", TokenString, 1}}},
	}
	for _, tc := range tests {
		res := g.TokenizeLine(tc.line, nil, 0)
		var got []tok
		for _, k := range res.Tokens {
			got = append(got, tok{tc.line[k.Start:k.End], k.Type, k.Language})
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q:\n got  %v\n want %v", tc.line, got, tc.want)
		}
	}
	if long := g.TokenizeLine(strings.Repeat("a", DefaultMaxLineBytes), nil, 0); long.Tokens[0].Language != 1 || long.Tokens[0].Type != TokenOther {
		t.Errorf("a long line: %+v", long.Tokens[0])
	}
}

func TestBasicAttributes(t *testing.T) {
	g := tokenizerOf(t, "test", grammarOf(t, "test", `"patterns": []`))
	tests := []struct {
		scope string
		typ   TokenType
	}{
		{"comment.line.double-slash", TokenComment},
		{"punctuation.definition.comment.begin", TokenComment},
		{"string.quoted.double", TokenString},
		{"string.regexp.js", TokenString},
		{"source.regex", TokenRegEx},
		{"meta.embedded.block.erb", TokenOther},
		{"keyword.control", tokenNotSet},
		{"commentary.text", tokenNotSet},
		{"stringy", tokenNotSet},
	}
	for _, tc := range tests {
		if got := g.basicAttributesOf(tc.scope).tokenType; got != tc.typ {
			t.Errorf("%s: type %d, want %d", tc.scope, got, tc.typ)
		}
	}
	e := newEmbeddedMatcher(map[string]int{"source.css": 2, "source.css.embedded": 3, "text": 4})
	for scope, want := range map[string]int{"source.css": 2, "source.css.x": 2, "source.css.embedded.html": 3, "source.cssx": 0, "text.html": 4, "other": 0} {
		if got := e.match(scope); got != want {
			t.Errorf("language of %s = %d, want %d", scope, got, want)
		}
	}
}
