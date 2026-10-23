package emmet

import "testing"

// Ported from Emmet: test/format.ts

// The changes of the options that the tests use.
var (
	modInlineBreak3 = func(c *Config) { c.Options.InlineBreak = 3 }
	modInlineBreak1 = func(c *Config) { c.Options.InlineBreak = 1 }
	modInlineBreak0 = func(c *Config) { c.Options.InlineBreak = 0 }
	modXHTML        = func(c *Config) { c.Options.SelfClosingStyle = SelfClosingXHTML }
	modXML          = func(c *Config) { c.Options.SelfClosingStyle = SelfClosingXML }
	modHTMLStyle    = func(c *Config) { c.Options.SelfClosingStyle = SelfClosingHTML }
	modCompact      = func(c *Config) { c.Options.CompactBoolean = true }
	modNoCompact    = func(c *Config) { c.Options.CompactBoolean = false }
	modNoFormat     = func(c *Config) { c.Options.Format = false }
	modComment      = func(c *Config) { c.Options.CommentEnabled = true }
	modCommentAfter = func(c *Config) {
		c.Options.CommentEnabled = true
		c.Options.CommentAfter = " { [%ID] }"
	}
	modField = func(c *Config) { c.Options.Field = SnippetField }
	modJSX   = func(c *Config) {
		c.Options.MarkupAttributes = map[string]string{"class": "className", "class*": "className"}
		c.Options.MarkupValuePrefix = map[string]string{"class*": "styles"}
		c.Options.Field = func(index int, _ string, _, _, _ int) string { return "${" + itoa(index) + "}" }
	}
)

// formatCase is an abbreviation, a change of the configuration of the syntax (nil is none) and what it formats to.
type formatCase struct {
	abbr string
	mod  func(*Config)
	want string
}

// runFormat parses each abbreviation with the configuration of the syntax and writes it with the formatter.
func runFormat(t *testing.T, syntax string, format func(*Abbreviation, *Config) string, cases []formatCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.abbr, func(t *testing.T) {
			cfg := newConfig(t, syntax)
			if tc.mod != nil {
				tc.mod(&cfg)
			}
			tree, err := Parse(tc.abbr, cfg)
			if err != nil {
				t.Fatalf("%q: %v", tc.abbr, err)
			}
			if got := format(tree, &cfg); got != tc.want {
				t.Errorf("%q\n got %q\nwant %q", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestFormatHTMLBasic(t *testing.T) {
	runFormat(t, "html", formatHTML, []formatCase{
		{`div>p`, nil, "<div>\n\t<p></p>\n</div>"},
		{`div>p*3`, nil, "<div>\n\t<p></p>\n\t<p></p>\n\t<p></p>\n</div>"},
		{`div#a>p.b*2>span`, nil, "<div id=\"a\">\n\t<p class=\"b\"><span></span></p>\n\t<p class=\"b\"><span></span></p>\n</div>"},
		{`div>div>div`, nil, "<div>\n\t<div>\n\t\t<div></div>\n\t</div>\n</div>"},
		{`table>tr*2>td{item}*2`, nil, "<table>\n\t<tr>\n\t\t<td>item</td>\n\t\t<td>item</td>\n\t</tr>\n\t<tr>\n\t\t<td>item</td>\n\t\t<td>item</td>\n\t</tr>\n</table>"},
	})
}

func TestFormatHTMLInlineElements(t *testing.T) {
	runFormat(t, "html", formatHTML, []formatCase{
		{`div>a>b*3`, modXHTML, "<div>\n\t<a href=\"\">\n\t\t<b></b>\n\t\t<b></b>\n\t\t<b></b>\n\t</a>\n</div>"},
		{`p>i`, modInlineBreak3, `<p><i></i></p>`},
		{`p>i*2`, modInlineBreak3, `<p><i></i><i></i></p>`},
		{`p>i*2`, modInlineBreak1, "<p>\n\t<i></i>\n\t<i></i>\n</p>"},
		{`p>i*3`, modInlineBreak3, "<p>\n\t<i></i>\n\t<i></i>\n\t<i></i>\n</p>"},
		{`p>i*3`, modInlineBreak0, `<p><i></i><i></i><i></i></p>`},
		{`i*2`, modInlineBreak3, `<i></i><i></i>`},
		{`i*3`, modInlineBreak3, "<i></i>\n<i></i>\n<i></i>"},
		{`i{a}+i{b}`, modInlineBreak3, `<i>a</i><i>b</i>`},
		{`img[src]/+p`, modXHTML, "<img src=\"\" alt=\"\" />\n<p></p>"},
		{`div>img[src]/+p`, modXHTML, "<div>\n\t<img src=\"\" alt=\"\" />\n\t<p></p>\n</div>"},
		{`div>p+img[src]/`, modXHTML, "<div>\n\t<p></p>\n\t<img src=\"\" alt=\"\" />\n</div>"},
		{`div>p+img[src]/+p`, modXHTML, "<div>\n\t<p></p>\n\t<img src=\"\" alt=\"\" />\n\t<p></p>\n</div>"},
		{`div>p+img[src]/*2+p`, modXHTML, "<div>\n\t<p></p>\n\t<img src=\"\" alt=\"\" /><img src=\"\" alt=\"\" />\n\t<p></p>\n</div>"},
		{`div>p+img[src]/*3+p`, modXHTML, "<div>\n\t<p></p>\n\t<img src=\"\" alt=\"\" />\n\t<img src=\"\" alt=\"\" />\n\t<img src=\"\" alt=\"\" />\n\t<p></p>\n</div>"},
	})
}

func TestFormatHTMLGenerateFields(t *testing.T) {
	runFormat(t, "html", formatHTML, []formatCase{
		{`a[href]`, modField, `<a href="${1}">${2}</a>`},
		{`a[href]*2`, modField, `<a href="${1}">${2}</a><a href="${3}">${4}</a>`},
		{`{${0} ${1:foo} ${2:bar}}*2`, modField, "${1} ${2:foo} ${3:bar}\n${4} ${5:foo} ${6:bar}"},
		{`{${0} ${1:foo} ${2:bar}}*2`, nil, " foo bar\n foo bar"},
		{`ul>li*2`, modField, "<ul>\n\t<li>${1}</li>\n\t<li>${2}</li>\n</ul>"},
		{`div>img[src]/`, modField, `<div><img src="${1}" alt="${2}"></div>`},
	})
}

func TestFormatHTMLMixedContent(t *testing.T) {
	runFormat(t, "html", formatHTML, []formatCase{
		{`div{foo}`, nil, `<div>foo</div>`},
		{`div>{foo}`, nil, `<div>foo</div>`},
		{`div>{foo}+{bar}`, nil, "<div>\n\tfoo\n\tbar\n</div>"},
		{`div>{foo}+{bar}+p`, nil, "<div>\n\tfoo\n\tbar\n\t<p></p>\n</div>"},
		{`div>{foo}+{bar}+p+{foo}+{bar}+p`, nil, "<div>\n\tfoo\n\tbar\n\t<p></p>\n\tfoo\n\tbar\n\t<p></p>\n</div>"},
		{`div>{foo}+p+{bar}`, nil, "<div>\n\tfoo\n\t<p></p>\n\tbar\n</div>"},
		{`div>{foo}>p`, nil, "<div>\n\tfoo\n\t<p></p>\n</div>"},
		{`div>{<!-- ${0} -->}`, nil, `<div><!--  --></div>`},
		{`div>{<!-- ${0} -->}+p`, nil, "<div>\n\t<!--  -->\n\t<p></p>\n</div>"},
		{`div>p+{<!-- ${0} -->}`, nil, "<div>\n\t<p></p>\n\t<!--  -->\n</div>"},
		{`div>{<!-- ${0} -->}>p`, nil, "<div>\n\t<!-- <p></p> -->\n</div>"},
		{`div>{<!-- ${0} -->}*2>p`, nil, "<div>\n\t<!-- <p></p> -->\n\t<!-- <p></p> -->\n</div>"},
		{`div>{<!-- ${0} -->}>p*2`, nil, "<div>\n\t<!-- \n\t<p></p>\n\t<p></p>\n\t-->\n</div>"},
		{`div>{<!-- ${0} -->}*2>p*2`, nil, "<div>\n\t<!-- \n\t<p></p>\n\t<p></p>\n\t-->\n\t<!-- \n\t<p></p>\n\t<p></p>\n\t-->\n</div>"},
		{`div>{<!-- ${0} -->}>b`, nil, "<div>\n\t<!-- <b></b> -->\n</div>"},
		{`div>{<!-- ${0} -->}>b*2`, nil, "<div>\n\t<!-- <b></b><b></b> -->\n</div>"},
		{`div>{<!-- ${0} -->}>b*3`, nil, "<div>\n\t<!-- \n\t<b></b>\n\t<b></b>\n\t<b></b>\n\t-->\n</div>"},
		{`div>{<!-- ${0} -->}`, modField, `<div><!-- ${1} --></div>`},
		{`div>{<!-- ${0} -->}>b`, modField, "<div>\n\t<!-- <b>${1}</b> -->\n</div>"},
	})
}

func TestFormatHTMLSelfClosing(t *testing.T) {
	runFormat(t, "html", formatHTML, []formatCase{
		{`img[src]/`, modHTMLStyle, `<img src="" alt="">`},
		{`img[src]/`, modXHTML, `<img src="" alt="" />`},
		{`img[src]/`, modXML, `<img src="" alt=""/>`},
		{`div>img[src]/`, modXHTML, `<div><img src="" alt="" /></div>`},
	})
}

func TestFormatHTMLBooleanAttributes(t *testing.T) {
	runFormat(t, "html", formatHTML, []formatCase{
		{`p[b.]`, modNoCompact, `<p b="b"></p>`},
		{`p[b.]`, modCompact, `<p b></p>`},
		{`p[contenteditable]`, modCompact, `<p contenteditable></p>`},
		{`p[contenteditable]`, modNoCompact, `<p contenteditable="contenteditable"></p>`},
		{`p[contenteditable=foo]`, modCompact, `<p contenteditable="foo"></p>`},
	})
}

func TestFormatHTMLNoFormatting(t *testing.T) {
	runFormat(t, "html", formatHTML, []formatCase{
		{`div>p`, modNoFormat, `<div><p></p></div>`},
		{`div>{foo}+p+{bar}`, modNoFormat, `<div>foo<p></p>bar</div>`},
		{`div>{foo}>p`, modNoFormat, `<div>foo<p></p></div>`},
		{`div>{<!-- ${0} -->}>p`, modNoFormat, `<div><!-- <p></p> --></div>`},
	})
}

func TestFormatHTMLFormatSpecificNodes(t *testing.T) {
	runFormat(t, "html", formatHTML, []formatCase{
		{`{<!DOCTYPE html>}+html>(head>meta[charset=${charset}]/+title{${1:Document}})+body`, modField, "<!DOCTYPE html>\n<html>\n<head>\n\t<meta charset=\"UTF-8\">\n\t<title>${2:Document}</title>\n</head>\n<body>\n\t${3}\n</body>\n</html>"},
	})
}

func TestFormatHTMLComment(t *testing.T) {
	runFormat(t, "html", formatHTML, []formatCase{
		{`ul>li.item`, modComment, "<ul>\n\t<li class=\"item\"></li>\n\t<!-- /.item -->\n</ul>"},
		{`div>ul>li.item#foo`, modComment, "<div>\n\t<ul>\n\t\t<li class=\"item\" id=\"foo\"></li>\n\t\t<!-- /#foo.item -->\n\t</ul>\n</div>"},
		{`div>ul>li.item#foo`, modCommentAfter, "<div>\n\t<ul>\n\t\t<li class=\"item\" id=\"foo\"></li> { %foo }\n\t</ul>\n</div>"},
	})
}

func TestFormatHTMLJsx(t *testing.T) {
	runFormat(t, "jsx", formatHTML, []formatCase{
		{`.`, modJSX, `<div className="${1}">${2}</div>`},
		{`..`, modJSX, `<div className={${1}}>${2}</div>`},
		{`div.`, modJSX, `<div className="${1}">${2}</div>`},
		{`div..`, modJSX, `<div className={${1}}>${2}</div>`},
		{`div.a`, modJSX, `<div className="a">${1}</div>`},
		{`div..a`, modJSX, `<div className={styles.a}>${1}</div>`},
		{`div.a.b`, modJSX, `<div className="a b">${1}</div>`},
		{`div.a..b`, modJSX, `<div className="a b">${1}</div>`},
	})
}

func TestFormatHAMLBasic(t *testing.T) {
	runFormat(t, "html", formatHAML, []formatCase{
		{`div#header>ul.nav>li[title=test].nav-item*2`, nil, "#header\n\t%ul.nav\n\t\t%li.nav-item(title=\"test\") \n\t\t%li.nav-item(title=\"test\") "},
		{`li>a`, nil, "%li\n\t%a(href=\"\") "},
		{`div#foo[data-n1=v1 title=test data-n2=v2].bar`, nil, `#foo.bar(data-n1="v1" title="test" data-n2="v2") `},
		{`input[disabled. foo title=test]/`, modCompact, `%input(type="text" disabled foo="" title="test")/`},
		{`input[disabled. foo title=test]/`, modNoCompact, `%input(type="text" disabled=true foo="" title="test")/`},
	})
}

func TestFormatHAMLNodesWithText(t *testing.T) {
	runFormat(t, "html", formatHAML, []formatCase{
		{`{Text 1}`, nil, `Text 1`},
		{`span{Text 1}`, nil, `%span Text 1`},
		{`span{Text 1}>b{Text 2}`, nil, "%span Text 1\n\t%b Text 2"},
		{"span{Text 1\nText 2}>b{Text 3}", nil, "%span\n\tText 1 |\n\tText 2 |\n\t%b Text 3"},
		{"div>span{Text 1\nText 2\nText 123}>b{Text 3}", nil, "%div\n\t%span\n\t\tText 1   |\n\t\tText 2   |\n\t\tText 123 |\n\t\t%b Text 3"},
	})
}

func TestFormatHAMLGenerateFields(t *testing.T) {
	runFormat(t, "html", formatHAML, []formatCase{
		{`a[href]`, modField, `%a(href="${1}") ${2}`},
		{`a[href]*2`, modField, "%a(href=\"${1}\") ${2}\n%a(href=\"${3}\") ${4}"},
		{`{${0} ${1:foo} ${2:bar}}*2`, modField, `${1} ${2:foo} ${3:bar}${4} ${5:foo} ${6:bar}`},
		{`{${0} ${1:foo} ${2:bar}}*2`, nil, ` foo bar foo bar`},
		{`ul>li*2`, modField, "%ul\n\t%li ${1}\n\t%li ${2}"},
		{`div>img[src]/`, modField, "%div\n\t%img(src=\"${1}\" alt=\"${2}\")/"},
	})
}

func TestFormatPugBasic(t *testing.T) {
	runFormat(t, "html", formatPug, []formatCase{
		{`div#header>ul.nav>li[title=test].nav-item*2`, nil, "#header\n\tul.nav\n\t\tli.nav-item(title=\"test\") \n\t\tli.nav-item(title=\"test\") "},
		{`div#foo[data-n1=v1 title=test data-n2=v2].bar`, nil, `#foo.bar(data-n1="v1", title="test", data-n2="v2") `},
		{`input[disabled. foo title=test]`, nil, `input(type="text", disabled, foo="", title="test")`},
		{`input[disabled. foo title=test]`, modXML, `input(type="text", disabled, foo="", title="test")/`},
	})
}

func TestFormatPugNodesWithText(t *testing.T) {
	runFormat(t, "html", formatPug, []formatCase{
		{`{Text 1}`, nil, `Text 1`},
		{`span{Text 1}`, nil, `span Text 1`},
		{`span{Text 1}>b{Text 2}`, nil, "span Text 1\n\tb Text 2"},
		{"span{Text 1\nText 2}>b{Text 3}", nil, "span\n\t| Text 1\n\t| Text 2\n\tb Text 3"},
		{"div>span{Text 1\nText 2}>b{Text 3}", nil, "div\n\tspan\n\t\t| Text 1\n\t\t| Text 2\n\t\tb Text 3"},
	})
}

func TestFormatPugGenerateFields(t *testing.T) {
	runFormat(t, "html", formatPug, []formatCase{
		{`a[href]`, modField, `a(href="${1}") ${2}`},
		{`a[href]*2`, modField, "a(href=\"${1}\") ${2}\na(href=\"${3}\") ${4}"},
		{`{${0} ${1:foo} ${2:bar}}*2`, modField, `${1} ${2:foo} ${3:bar}${4} ${5:foo} ${6:bar}`},
		{`{${0} ${1:foo} ${2:bar}}*2`, nil, ` foo bar foo bar`},
		{`ul>li*2`, modField, "ul\n\tli ${1}\n\tli ${2}"},
		{`div>img[src]/`, modField, "div\n\timg(src=\"${1}\", alt=\"${2}\")"},
	})
}

func TestFormatSlimBasic(t *testing.T) {
	runFormat(t, "html", formatSlim, []formatCase{
		{`div#header>ul.nav>li[title=test].nav-item*2`, nil, "#header\n\tul.nav\n\t\tli.nav-item title=\"test\" \n\t\tli.nav-item title=\"test\" "},
		{`div#foo[data-n1=v1 title=test data-n2=v2].bar`, nil, `#foo.bar data-n1="v1" title="test" data-n2="v2" `},
	})
}

func TestFormatSlimNodesWithText(t *testing.T) {
	runFormat(t, "html", formatSlim, []formatCase{
		{`{Text 1}`, nil, `Text 1`},
		{`span{Text 1}`, nil, `span Text 1`},
		{`span{Text 1}>b{Text 2}`, nil, "span Text 1\n\tb Text 2"},
		{"span{Text 1\nText 2}>b{Text 3}", nil, "span\n\t| Text 1\n\t| Text 2\n\tb Text 3"},
		{"div>span{Text 1\nText 2}>b{Text 3}", nil, "div\n\tspan\n\t\t| Text 1\n\t\t| Text 2\n\t\tb Text 3"},
	})
}

func TestFormatSlimGenerateFields(t *testing.T) {
	runFormat(t, "html", formatSlim, []formatCase{
		{`a[href]`, modField, `a href="${1}" ${2}`},
		{`a[href]*2`, modField, "a href=\"${1}\" ${2}\na href=\"${3}\" ${4}"},
		{`{${0} ${1:foo} ${2:bar}}*2`, modField, `${1} ${2:foo} ${3:bar}${4} ${5:foo} ${6:bar}`},
		{`{${0} ${1:foo} ${2:bar}}*2`, nil, ` foo bar foo bar`},
		{`ul>li*2`, modField, "ul\n\tli ${1}\n\tli ${2}"},
		{`div>img[src]/`, modField, "div\n\timg src=\"${1}\" alt=\"${2}\"/"},
	})
}
