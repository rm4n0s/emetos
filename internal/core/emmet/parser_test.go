package emmet

import (
	"strings"
	"testing"
)

// Ported from Emmet: packages/abbreviation/test/parser.ts

// parseToString tokenizes and parses an abbreviation and writes the tree in the short form of the tests.
func parseToString(abbr string, opts ParserOptions) (string, error) {
	tokens, err := Tokenize(abbr)
	if err != nil {
		return "", err
	}
	tree, err := ParseTokens(tokens, opts)
	if err != nil {
		return "", err
	}
	return stringifyContent(tree), nil
}

type abbrCase struct{ in, want string }

func runParserCases(t *testing.T, opts ParserOptions, cases []abbrCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseToString(tc.in, opts)
			if err != nil {
				t.Fatalf("%q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("%q\n got %s\nwant %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestParserBasicAbbreviations(t *testing.T) {
	runParserCases(t, ParserOptions{}, []abbrCase{
		{`p`, `<p></p>`},
		{`p{text}`, `<p>text</p>`},
		{`h$`, `<h$></h$>`},
		{`.nav`, `<? class=nav></?>`},
		{`div.width1\/2`, `<div class=width1/2></div>`},
		{`#sample*3`, `<?*3 id=sample></?>`},
		// umlauts, https://github.com/emmetio/emmet/issues/439
		{`DatenSätze^`, `<DatenSätze></DatenSätze>`},
		// https://github.com/emmetio/emmet/issues/562: dots in attribute names
		{`li[repeat.for="todo of todoList"]`, `<li repeat.for="todo of todoList"></li>`},
		{`a>b`, `<a><b></b></a>`},
		{`a+b`, `<a></a><b></b>`},
		{`a+b>c+d`, `<a></a><b><c></c><d></d></b>`},
		{`a>b>c+e`, `<a><b><c></c><e></e></b></a>`},
		{`a>b>c^d`, `<a><b><c></c></b><d></d></a>`},
		{`a>b>c^^^^d`, `<a><b><c></c></b></a><d></d>`},
		{`a:b>c`, `<a:b><c></c></a:b>`},
		{`ul.nav[title="foo"]`, `<ul class=nav title="foo"></ul>`},
	})
}

func TestParserGroups(t *testing.T) {
	runParserCases(t, ParserOptions{}, []abbrCase{
		{`a>(b>c)+d`, `<a>(<b><c></c></b>)<d></d></a>`},
		{`(a>b)+(c>d)`, `(<a><b></b></a>)(<c><d></d></c>)`},
		{`a>((b>c)(d>e))f`, `<a>((<b><c></c></b>)(<d><e></e></d>))<f></f></a>`},
		{`a>((((b>c))))+d`, `<a>((((<b><c></c></b>))))<d></d></a>`},
		{`a>(((b>c))*4)+d`, `<a>(((<b><c></c></b>))*4)<d></d></a>`},
		{`(div>dl>(dt+dd)*2)`, `(<div><dl>(<dt></dt><dd></dd>)*2</dl></div>)`},
		{`a>()`, `<a>()</a>`},
	})
}

func TestParserAttributes(t *testing.T) {
	runParserCases(t, ParserOptions{}, []abbrCase{
		{`[].foo`, `<? class=foo></?>`},
		{`[a]`, `<? a></?>`},
		{`[a b c [d]]`, `<? a b c [d]></?>`},
		{`[a=b]`, `<? a=b></?>`},
		{`[a=b c= d=e]`, `<? a=b c d=e></?>`},
		{`[a=b.c d=тест]`, `<? a=b.c d=тест></?>`},
		{`[[a]=b (c)=d]`, `<? [a]=b (c)=d></?>`},
		// quoted attribute values
		{`[a="b"]`, `<? a="b"></?>`},
		{`[a="b" c='d' e=""]`, `<? a="b" c='d' e=""></?>`},
		{`[[a]="b" (c)='d']`, `<? [a]="b" (c)='d'></?>`},
		// mixed quoted
		{`[a="foo'bar" b='foo"bar' c="foo\"bar"]`, `<? a="foo'bar" b='foo"bar' c="foo"bar"></?>`},
		// boolean and implied attributes
		{`[a. b.]`, `<? a. b.></?>`},
		{`[!a !b.]`, `<? !a !b.></?>`},
		// default values
		{`["a.b"]`, `<? ?="a.b"></?>`},
		{`['a.b' "c=d" foo=bar "./test.html"]`, `<? ?='a.b' ?="c=d" foo=bar ?="./test.html"></?>`},
		// expressions as values
		{`[foo={1 + 2} bar={fn(1, "foo")}]`, `<? foo={1 + 2} bar={fn(1, "foo")}></?>`},
		// tab stops as unquoted values
		{`[name=${1} value=${2:test}]`, `<? name=${1} value=${2:test}></?>`},
	})
}

func TestParserMalformedAttributes(t *testing.T) {
	runParserCases(t, ParserOptions{}, []abbrCase{
		{`[a`, `<? a></?>`},
		{`[a={foo]`, `<? a={foo]></?>`},
	})
	errCases := []struct{ in, msg, tag string }{
		{`[a="foo]`, "Unclosed quote", "UnclosedQuote"},
		{`[a=b=c]`, `Unexpected "Operator" token`, "UnexpectedAttributeToken"},
	}
	for _, tc := range errCases {
		t.Run(tc.in, func(t *testing.T) {
			_, err := parseToString(tc.in, ParserOptions{})
			if err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("%q: error %v, want %q", tc.in, err, tc.msg)
			}
		})
	}
}

func TestParserElements(t *testing.T) {
	runParserCases(t, ParserOptions{}, []abbrCase{
		{`div`, `<div></div>`},
		{`div.foo`, `<div class=foo></div>`},
		{`div#foo`, `<div id=foo></div>`},
		{`div#foo.bar`, `<div id=foo class=bar></div>`},
		{`div.foo#bar`, `<div class=foo id=bar></div>`},
		{`div.foo.bar.baz`, `<div class=foo class=bar class=baz></div>`},
		{`.foo`, `<? class=foo></?>`},
		{`#foo`, `<? id=foo></?>`},
		{`.foo_bar`, `<? class=foo_bar></?>`},
		{`#foo.bar`, `<? id=foo class=bar></?>`},
		// attribute shorthands
		{`.`, `<? class></?>`},
		{`#`, `<? id></?>`},
		{`#.`, `<? id class></?>`},
		{`.#.`, `<? class id class></?>`},
		{`.a..`, `<? class=a class></?>`},
		// elements with attributes
		{`div[foo=bar]`, `<div foo=bar></div>`},
		{`div.a[b=c]`, `<div class=a b=c></div>`},
		{`div[b=c].a`, `<div b=c class=a></div>`},
		{`div[a=b][c="d"]`, `<div a=b c="d"></div>`},
		{`[b=c]`, `<? b=c></?>`},
		{`.a[b=c]`, `<? class=a b=c></?>`},
		{`[b=c].a#d`, `<? b=c class=a id=d></?>`},
		{`[b=c]a`, `<? b=c></?><a></a>`},
		// element with text
		{`div{foo}`, `<div>foo</div>`},
		{`{foo}`, `<?>foo</?>`},
		// mixed
		{`div.foo{bar}`, `<div class=foo>bar</div>`},
		{`.foo{bar}#baz`, `<? class=foo id=baz>bar</?>`},
		{`.foo[b=c]{bar}`, `<? class=foo b=c>bar</?>`},
		// repeated element
		{`div.foo*3`, `<div*3 class=foo></div>`},
		{`.foo*`, `<?* class=foo></?>`},
		{`.a[b=c]*10`, `<?*10 class=a b=c></?>`},
		{`.a*10[b=c]`, `<?*10 class=a b=c></?>`},
		{`.a*10{text}`, `<?*10 class=a>text</?>`},
		// self-closing element
		{`div/`, `<div />`},
		{`.foo/`, `<? class=foo />`},
		{`.foo[bar]/`, `<? class=foo bar />`},
		{`.foo/*3`, `<?*3 class=foo />`},
		{`.foo*3/`, `<?*3 class=foo />`},
	})
	if _, err := parseToString("/", ParserOptions{}); err == nil || !strings.Contains(err.Error(), "Unexpected character") {
		t.Errorf(`"/": error %v, want "Unexpected character"`, err)
	}
}

func TestParserJSX(t *testing.T) {
	runParserCases(t, ParserOptions{JSX: true}, []abbrCase{
		{`foo.bar`, `<foo class=bar></foo>`},
		{`Foo.bar`, `<Foo class=bar></Foo>`},
		{`Foo.Bar`, `<Foo.Bar></Foo.Bar>`},
		{`Foo.`, `<Foo class></Foo>`},
		{`Foo.Bar.baz`, `<Foo.Bar class=baz></Foo.Bar>`},
		{`Foo.Bar.Baz`, `<Foo.Bar.Baz></Foo.Bar.Baz>`},
		{`.{theme.class}`, `<? class=theme.class></?>`},
		{`#{id}`, `<? id=id></?>`},
		{`Foo.{theme.class}`, `<Foo class=theme.class></Foo>`},
	})
}

func TestParserErrors(t *testing.T) {
	for _, tc := range []struct{ in, msg string }{
		{"str?", "Unexpected character at 4"},
		{"foo,bar", "Unexpected character at 4"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			_, err := parseToString(tc.in, ParserOptions{})
			if err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Errorf("%q: error %v, want %q", tc.in, err, tc.msg)
			}
		})
	}
	runParserCases(t, ParserOptions{}, []abbrCase{
		{`foo\,bar`, `<foo,bar></foo,bar>`},
		{`foo\`, `<foo></foo>`},
	})
}

func TestParserMissingBraces(t *testing.T) {
	// no errors for missing closing braces
	runParserCases(t, ParserOptions{}, []abbrCase{
		{`div[title="test"`, `<div title="test"></div>`},
		{`div(foo`, `<div></div>(<foo></foo>)`},
		{`div{foo`, `<div>foo</div>`},
	})
}
