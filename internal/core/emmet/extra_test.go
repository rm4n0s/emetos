package emmet

import (
	"reflect"
	"strings"
	"testing"
)

// Tests that are not ported from Emmet: the configuration, the output of the editor, and the
// parts of the formatters that Emmet's tests leave out.

func TestNewConfigSyntaxes(t *testing.T) {
	cases := []struct {
		syntax string
		check  func(c Config) string
	}{
		{"", func(c Config) string {
			if c.Syntax != "html" {
				return "the syntax is " + c.Syntax
			}
			return ""
		}},
		{"html", func(c Config) string {
			if c.Snippets["a:blank"] == "" || c.Snippets["acronym"] == "" || c.Snippets["acr"] == "" || c.Snippets["tm"] != "" {
				return "the html snippets are not right"
			}
			return ""
		}},
		{"xsl", func(c Config) string {
			if c.Snippets["tm"] == "" || c.Snippets["tmatch"] == "" || c.Snippets["a"] == "" || c.Options.SelfClosingStyle != SelfClosingXML || c.Snippets["!!!"] != `{<?xml version="1.0" encoding="UTF-8"?>}` {
				return "the xsl configuration is not right"
			}
			return ""
		}},
		{"pug", func(c Config) string {
			if c.Snippets["!!!"] != "{doctype html}" {
				return "the pug snippets are not right"
			}
			return ""
		}},
		{"xml", func(c Config) string {
			if c.Options.SelfClosingStyle != SelfClosingXML {
				return "xml is not self-closing"
			}
			return ""
		}},
		{"xhtml", func(c Config) string {
			if c.Options.SelfClosingStyle != SelfClosingXHTML {
				return "xhtml is not self-closing like xhtml"
			}
			return ""
		}},
		{"jsx", func(c Config) string {
			if !c.Options.JSXEnabled || c.Options.MarkupAttributes["for"] != "htmlFor" || c.Options.MarkupValuePrefix["class*"] != "styles" {
				return "the jsx options are not right"
			}
			return ""
		}},
		{"vue", func(c Config) string {
			if c.Options.JSXEnabled || c.Options.MarkupAttributes["class*"] != ":class" {
				return "the vue options are not right"
			}
			return ""
		}},
		{"svelte", func(c Config) string {
			if !c.Options.JSXEnabled {
				return "svelte has no jsx mode"
			}
			return ""
		}},
		{"haml", func(c Config) string {
			if c.Variables["charset"] != "UTF-8" || c.Variables["lang"] != "en" || c.Options.Indent != "\t" || c.Options.InlineBreak != 3 {
				return "the defaults are not right"
			}
			return ""
		}},
	}
	for _, tc := range cases {
		t.Run(tc.syntax, func(t *testing.T) {
			if msg := tc.check(newConfig(t, tc.syntax)); msg != "" {
				t.Error(msg)
			}
		})
	}
}

func TestNewConfigsAreIndependent(t *testing.T) {
	a := newConfig(t, "html")
	a.SetSnippets(map[string]string{"zzz": "p"})
	a.SetVariables(map[string]string{"charset": "x"})
	b := newConfig(t, "html")
	if b.Snippets["zzz"] != "" || b.Variables["charset"] != "UTF-8" {
		t.Errorf("a change of a configuration is seen by another one")
	}
}

func TestSetOutput(t *testing.T) {
	cases := []struct {
		name                        string
		syntax, abbr                string
		indent, newline, baseIndent string
		want                        string
	}{
		{"two spaces", "html", "ul>li*2", "  ", "\n", "", "<ul>\n  <li></li>\n  <li></li>\n</ul>"},
		{"crlf", "html", "ul>li*2", "\t", "\r\n", "", "<ul>\r\n\t<li></li>\r\n\t<li></li>\r\n</ul>"},
		{"base indent", "html", "ul>li", "  ", "\n", "    ", "<ul>\n      <li></li>\n    </ul>"},
		{"nested", "html", "div>div>p", "    ", "\n", "", "<div>\n    <div>\n        <p></p>\n    </div>\n</div>"},
		{"haml", "haml", "ul>li*2", "  ", "\r\n", "", "%ul\r\n  %li \r\n  %li "},
		{"pug", "pug", "ul>li*2", " ", "\n", "", "ul\n li \n li "},
		{"slim with a text of several lines", "slim", "p{a\nb}", "  ", "\n", "", "p\n  | a\n  | b"},
		{"the variables", "html", "p[title=${indentation}${newline}]", "  ", "\r\n", "", "<p title=\"  \r\n\"></p>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newConfig(t, tc.syntax)
			cfg.SetOutput(tc.indent, tc.newline, tc.baseIndent)
			got, err := Expand(tc.abbr, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseTemplate(t *testing.T) {
	ph := func(before, name, after string) templateToken {
		return templateToken{placeholder: true, before: before, name: name, after: after}
	}
	txt := func(s string) templateToken { return templateToken{text: s} }
	cases := []struct {
		in   string
		want []templateToken
	}{
		{"", nil},
		{"text", []templateToken{txt("text")}},
		{"\n<!-- /[#ID][.CLASS] -->", []templateToken{txt("\n<!-- /"), ph("#", "ID", ""), txt(""), ph(".", "CLASS", ""), txt(" -->")}},
		{" { [%ID] }", []templateToken{txt(" { "), ph("%", "ID", ""), txt(" }")}},
		// (as Emmet's, an empty text goes before a placeholder that starts the template)
		{"[a=ID.]", []templateToken{txt(""), ph("a=", "ID", ".")}},
		{"[ID", []templateToken{txt("[ID")}},
		{"[ID [OTHER]", []templateToken{txt("[ID [OTHER]")}},
		{"[A_B-C1]x", []templateToken{txt(""), ph("", "A_B-C1", ""), txt("x")}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := parseTemplate(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestCommentsBeforeAndAfter(t *testing.T) {
	cfg := newConfig(t, "html")
	cfg.Options.CommentEnabled = true
	cfg.Options.CommentBefore = "<!-- [#ID] -->\n"
	cfg.Options.CommentTrigger = []string{"id"}
	got, err := Expand("div>p#a+p.b", cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := "<div>\n\t<!-- #a -->\n\t<p id=\"a\"></p>\n\t<!-- /#a -->\n\t<p class=\"b\"></p>\n</div>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestImplicitTags(t *testing.T) {
	cases := []struct {
		name, abbr string
		context    *Context
		want       string
	}{
		{"in a list", "ul>.a", nil, `<ul><li class="a"></li></ul>`},
		{"in an inline element", "em>.a", nil, `<em><span class="a"></span></em>`},
		{"in a table row", "tr>.a", nil, `<tr><td class="a"></td></tr>`},
		{"the name is not case sensitive", "UL>.a", nil, `<UL><li class="a"></li></UL>`},
		{"in a select", "select>.a", nil, `<select name="" id=""><option class="a"></option></select>`},
		{"in a block", "div>.a", nil, `<div><div class="a"></div></div>`},
		{"from the context", ".a", &Context{Name: "ul"}, `<li class="a"></li>`},
		{"from an inline context", ".a", &Context{Name: "span"}, `<span class="a"></span>`},
		{"a text is not a parent, the node goes next to it", "p>{x}>.a", &Context{Name: "ul"}, `<p>x<span class="a"></span></p>`},
		{"at the top level the context is the parent", "{x}>.a", &Context{Name: "ul"}, `x<li class="a"></li>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newConfig(t, "html")
			cfg.Context = tc.context
			cfg.Options.Format = false
			got, err := Expand(tc.abbr, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%q: got %q, want %q", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestMergedAttributes(t *testing.T) {
	cases := []struct{ abbr, want string }{
		{".a.b", `<div class="a b"></div>`},
		{"p.a[class=b]", `<p class="a b"></p>`},
		{"p[class=a class=b]", `<p class="a b"></p>`},
		{"p[a=1 a=2]", `<p a="2"></p>`},
		{"p[a=1 !a]", `<p></p>`},
		{"p[!a]", `<p></p>`},
		{`p[!a=""]`, `<p a=""></p>`},
		{"p[!a=1]", `<p a="1"></p>`},
		{"p[a. b]", `<p a="a" b=""></p>`},
		{"p[a={x}]", `<p a={x}></p>`},
		{"p['text']", `<p ="text"></p>`},
		{`p["one two"]`, `<p ="one two"></p>`},
		{"p[a='b']", `<p a="b"></p>`},
	}
	for _, tc := range cases {
		t.Run(tc.abbr, func(t *testing.T) {
			cfg := newConfig(t, "html")
			got, err := Expand(tc.abbr, cfg)
			if err != nil {
				t.Fatal(err)
			}
			// attributes without a name are not written
			if tc.abbr == "p['text']" || tc.abbr == `p["one two"]` {
				tc.want = "<p></p>"
			}
			if got != tc.want {
				t.Errorf("%q: got %q, want %q", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestNonASCII(t *testing.T) {
	cases := []struct{ abbr, want string }{
		{"Äb>ö", "<Äb><ö></ö></Äb>"},
		{"p{тест}", "<p>тест</p>"},
		{"p[a=日本]", `<p a="日本"></p>`},
		{"p{a b}", "<p>a b</p>"},
		{"p{à}", "<p>à</p>"},
		{"p[à=à]", `<p à="à"></p>`},
		{`p{a\}b}`, "<p>a}b</p>"},
		{`p{日\本}`, "<p>日本</p>"},
	}
	for _, tc := range cases {
		t.Run(tc.abbr, func(t *testing.T) {
			cfg := newConfig(t, "html")
			cfg.Options.Format = false
			got, err := Expand(tc.abbr, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%q: got %q, want %q", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestEmptyValuesAndFields(t *testing.T) {
	cases := []struct{ abbr, want string }{
		{"b{}", "<b></b>"},
		{"a{}", `<a href=""></a>`},
		{"p", "<p></p>"},
		{"", ""},
		{"a[href]{}", `<a href=""></a>`},
		{"p{${1:one} ${2}}", "<p>one </p>"},
		{"p{${1:one}}>b", "<p><b></b></p>"},
	}
	for _, tc := range cases {
		t.Run(tc.abbr, func(t *testing.T) {
			got, err := Expand(tc.abbr, newConfig(t, "html"))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%q: got %q, want %q", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestIndentFormatDetails(t *testing.T) {
	cases := []struct {
		name, syntax, abbr, want string
	}{
		{"haml classes (the name div is left out)", "haml", "div.a.b", ".a.b "},
		{"haml classes", "haml", "p.a.b", "%p.a.b "},
		{"haml div with only an id", "haml", "#a", "#a "},
		{"haml boolean", "haml", "input[disabled]/", `%input(type="text" disabled=true)/`},
		{"haml text with several lines and fields", "haml", "p{a\nbb}", "%p\n\ta  |\n\tbb |"},
		{"pug id and class", "pug", "div#a.b.c", "#a.b.c "},
		{"pug boolean", "pug", "input[disabled]", `input(type="text", disabled)`},
		{"pug text of several lines", "pug", "p{a\nb}", "p\n\t| a\n\t| b"},
		{"slim attributes", "slim", "a[href=x title=y]", `a href="x" title="y" `},
		{"slim self close", "slim", "br/", "br/"},
		{"slim text", "slim", "{text}+p{more}", "text\np more"},
		{"slim class with spaces", "slim", "p[class='a b']", "p.a.b "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Expand(tc.abbr, newConfig(t, tc.syntax))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%q: got %q, want %q", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestParseAndStringify(t *testing.T) {
	cfg := newConfig(t, "html")
	tree, err := Parse("ul>li*2", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Stringify(tree, cfg), "<ul>\n\t<li></li>\n\t<li></li>\n</ul>"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// an abbreviation that is parsed and converted by hand, and then resolved
	plain, err := ParseAbbreviation("ul>.a", ParserOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Children[0].Children[0].Name != "" {
		t.Errorf("the implicit tag is resolved by the parser")
	}
	Resolve(plain, cfg)
	if got := plain.Children[0].Children[0].Name; got != "li" {
		t.Errorf("the implicit tag is %q after Resolve", got)
	}

	// Convert of the tokens of the parser
	tokens, err := Tokenize("a+b")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := ParseTokens(tokens, ParserOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := stringifyNodes(Convert(stmt, ParserOptions{}).Children); got != "<a></a><b></b>" {
		t.Errorf("Convert: %s", got)
	}
}

func TestTextOnlyWrapsIntoTheDeepestNode(t *testing.T) {
	cfg := newConfig(t, "html")
	cfg.Text = TextString("hi")
	cfg.Options.Format = false
	got, err := Expand("div>p+ul>li", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if want := "<div><p></p><ul><li>hi</li></ul></div>"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// the snippets are parsed with no text
	cfg.Text = TextString("www.site.com")
	got, err = Expand("a", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `href="http://www.site.com"`) {
		t.Errorf("got %q", got)
	}
}

func TestSnippetFieldAndTextHooks(t *testing.T) {
	cfg := newConfig(t, "html")
	cfg.Options.Field = SnippetField
	cfg.Options.Text = func(text string, _, _, _ int) string { return strings.ReplaceAll(text, "$", `\$`) }
	got, err := Expand("a[href]{cost \\$ five}", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if want := `<a href="${1}">cost \$ five</a>`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBEMOptions(t *testing.T) {
	cfg := newConfig(t, "html")
	cfg.Options.BEMEnabled = true
	cases := []struct{ abbr, want string }{
		{"div.b>div.-e_m", `<div class="b"><div class="b__e b__e_m"></div></div>`},
		{"div.b>div.--e", `<div class="b"><div class="b__e"></div></div>`},
		{"div.b>.-e", `<div class="b"><div class="b__e"></div></div>`},
		{"div.b>div.c-d", `<div class="b"><div class="c-d"></div></div>`},
		{"div.b>div._m>div.-e", `<div class="b"><div class="b b_m"><div class="b__e"></div></div></div>`},
	}
	cfg.Options.Format = false
	for _, tc := range cases {
		t.Run(tc.abbr, func(t *testing.T) {
			got, err := Expand(tc.abbr, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("%q: got %q, want %q", tc.abbr, got, tc.want)
			}
		})
	}
}

// Class names are limited to the characters of element names, as in Emmet: other characters
// are an error (they can be quoted in an attribute).
func TestCharactersOfClassNames(t *testing.T) {
	_, err := Expand("p.日本", newConfig(t, "html"))
	wantRoute(t, err, "scanner.fail.UnexpectedCharacter")
	got, err := Expand(`p[class="日本"]`, newConfig(t, "html"))
	if err != nil || got != `<p class="日本"></p>` {
		t.Errorf("got %q, %v", got, err)
	}
}
