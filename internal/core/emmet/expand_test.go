package emmet

import "testing"

// Ported from Emmet: test/expand.ts (the markup part)

// attrSnippets are the snippets of the test of the attributes.
var attrSnippets = map[string]string{"test": "test[!foo bar. baz={}]"}

// The changes of the configuration that the tests use (modXHTML, modComment and modField are in format_test.go).
var (
	modTextFooBar   = func(c *Config) { c.Text = TextLines("foo", "bar") }
	modTextDivs     = func(c *Config) { c.Text = TextLines("<div>line1</div>\n<div>line2</div>") }
	modAttrSnippets = func(c *Config) { c.SetSnippets(attrSnippets) }
	modAttrReverse  = func(c *Config) {
		c.Options.ReverseAttributes = true
		c.SetSnippets(attrSnippets)
	}
	modFormatLeaf     = func(c *Config) { c.Options.FormatLeafNode = true }
	modVariablesRU    = func(c *Config) { c.SetVariables(map[string]string{"charset": "ru-RU"}) }
	modCustomSnippets = func(c *Config) {
		c.SetSnippets(map[string]string{
			"link":   "link[foo=bar href]/",
			"foo":    ".foo[bar=baz]",
			"repeat": "div>ul>li{Hello World}*3",
		})
	}
	modJSXAttrs = func(c *Config) {
		c.Options.MarkupAttributes = map[string]string{"class": "className", "class*": "classStarName"}
	}
	modJSXPrefix = func(c *Config) { c.Options.MarkupValuePrefix = map[string]string{"class*": "class"} }
)

func modTextString(s string) func(*Config) { return func(c *Config) { c.Text = TextString(s) } }

// expandCase is an abbreviation, the syntax, a change of the configuration (nil is none) and what it expands to.
type expandCase struct {
	abbr   string
	syntax string
	mod    func(*Config)
	want   string
}

func runExpand(t *testing.T, cases []expandCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.abbr, func(t *testing.T) {
			cfg := newConfig(t, tc.syntax)
			if tc.mod != nil {
				tc.mod(&cfg)
			}
			got, err := Expand(tc.abbr, cfg)
			if err != nil {
				t.Fatalf("%q: %v", tc.abbr, err)
			}
			if got != tc.want {
				t.Errorf("%q\n got %q\nwant %q", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestExpandMarkupBasic(t *testing.T) {
	runExpand(t, []expandCase{
		{`input[value="text$"]*2`, "html", nil, `<input type="text" value="text1"><input type="text" value="text2">`},
		{`ul>.item$*2`, "html", nil, "<ul>\n\t<li class=\"item1\"></li>\n\t<li class=\"item2\"></li>\n</ul>"},
		{`ul>.item$*`, "html", modTextFooBar, "<ul>\n\t<li class=\"item1\">foo</li>\n\t<li class=\"item2\">bar</li>\n</ul>"},
		{`ul>.item$*2`, "html", modField, "<ul>\n\t<li class=\"item1\">${1}</li>\n\t<li class=\"item2\">${2}</li>\n</ul>"},
		{`textarea`, "html", nil, `<textarea name="" id=""></textarea>`},
	})
}

func TestExpandMarkupAttributes(t *testing.T) {
	runExpand(t, []expandCase{
		{`a.test`, "html", nil, `<a href="" class="test"></a>`},
		{`a.test`, "html", modAttrReverse, `<a class="test" href=""></a>`},
		{`test`, "html", modAttrSnippets, `<test bar="bar" baz={}></test>`},
		{`test[foo]`, "html", modAttrSnippets, `<test bar="bar" baz={}></test>`},
		{`test[baz=a foo=1]`, "html", modAttrSnippets, `<test foo="1" bar="bar" baz={a}></test>`},
		{`map`, "html", nil, `<map name=""></map>`},
		{`map[]`, "html", nil, `<map name=""></map>`},
		{`map[name="valid"]`, "html", nil, `<map name="valid"></map>`},
		{`map[href="invalid"]`, "html", nil, `<map name="" href="invalid"></map>`},
		{`data`, "html", nil, `<data value=""></data>`},
		{`data[value=5]`, "html", nil, `<data value="5"></data>`},
		{`meter`, "html", nil, `<meter value=""></meter>`},
		{`meter[min=4 max=6]`, "html", nil, `<meter value="" min="4" max="6"></meter>`},
		{`time`, "html", nil, `<time datetime=""></time>`},
		{`time[datetime=2023-07-01]`, "html", nil, `<time datetime="2023-07-01"></time>`},
		{`test`, "html", modAttrReverse, `<test bar="bar" baz={}></test>`},
		{`test[foo]`, "html", modAttrReverse, `<test bar="bar" baz={}></test>`},
		{`test[baz=a foo=1]`, "html", modAttrReverse, `<test baz={a} foo="1" bar="bar"></test>`},
	})
}

func TestExpandMarkupExpressions(t *testing.T) {
	runExpand(t, []expandCase{
		{`span{{foo}}`, "html", nil, `<span>{foo}</span>`},
		{`span{foo}`, "html", nil, `<span>foo</span>`},
		{`span[foo={bar}]`, "html", nil, `<span foo={bar}></span>`},
		{`span[foo={{bar}}]`, "html", nil, `<span foo={{bar}}></span>`},
	})
}

func TestExpandMarkupNumbering(t *testing.T) {
	runExpand(t, []expandCase{
		{`ul>li.item$@-*5`, "html", nil, "<ul>\n\t<li class=\"item5\"></li>\n\t<li class=\"item4\"></li>\n\t<li class=\"item3\"></li>\n\t<li class=\"item2\"></li>\n\t<li class=\"item1\"></li>\n</ul>"},
	})
}

func TestExpandMarkupSyntax(t *testing.T) {
	runExpand(t, []expandCase{
		{`ul>.item$*2`, "html", nil, "<ul>\n\t<li class=\"item1\"></li>\n\t<li class=\"item2\"></li>\n</ul>"},
		{`ul>.item$*2`, "slim", nil, "ul\n\tli.item1 \n\tli.item2 "},
		{`xsl:variable[name=a select=b]>div`, "xsl", nil, "<xsl:variable name=\"a\">\n\t<div></div>\n</xsl:variable>"},
	})
}

func TestExpandMarkupCustomProfile(t *testing.T) {
	runExpand(t, []expandCase{
		{`img`, "html", nil, `<img src="" alt="">`},
		{`img`, "html", modXHTML, `<img src="" alt="" />`},
	})
}

func TestExpandMarkupCustomVariables(t *testing.T) {
	runExpand(t, []expandCase{
		{`[charset=${charset}]{${charset}}`, "html", nil, `<div charset="UTF-8">UTF-8</div>`},
		{`[charset=${charset}]{${charset}}`, "html", modVariablesRU, `<div charset="ru-RU">ru-RU</div>`},
	})
}

func TestExpandMarkupCustomSnippets(t *testing.T) {
	runExpand(t, []expandCase{
		{`foo`, "html", modCustomSnippets, `<div class="foo" bar="baz"></div>`},
		{`link:css`, "html", nil, `<link rel="stylesheet" href="style.css">`},
		{`link:css`, "html", modCustomSnippets, `<link foo="bar" href="style.css">`},
		{`repeat`, "html", modCustomSnippets, "<div>\n\t<ul>\n\t\t<li>Hello World</li>\n\t\t<li>Hello World</li>\n\t\t<li>Hello World</li>\n\t</ul>\n</div>"},
		{`tarea`, "html", nil, `<textarea name="" id=""></textarea>`},
		{`tarea:c`, "html", nil, `<textarea name="" id="" cols="30"></textarea>`},
		{`tarea:r`, "html", nil, `<textarea name="" id="" rows="10"></textarea>`},
		{`tarea:cr`, "html", nil, `<textarea name="" id="" cols="30" rows="10"></textarea>`},
	})
}

func TestExpandMarkupFormatterOptions(t *testing.T) {
	runExpand(t, []expandCase{
		{`ul>.item$*2`, "html", nil, "<ul>\n\t<li class=\"item1\"></li>\n\t<li class=\"item2\"></li>\n</ul>"},
		{`ul>.item$*2`, "html", modComment, "<ul>\n\t<li class=\"item1\"></li>\n\t<!-- /.item1 -->\n\t<li class=\"item2\"></li>\n\t<!-- /.item2 -->\n</ul>"},
		{`div>p`, "html", nil, "<div>\n\t<p></p>\n</div>"},
		{`div>p`, "html", modFormatLeaf, "<div>\n\t<p>\n\t\t\n\t</p>\n</div>"},
	})
}

func TestExpandMarkupJSX(t *testing.T) {
	runExpand(t, []expandCase{
		{`div#foo.bar`, "jsx", nil, `<div id="foo" className="bar"></div>`},
		{`label[for=a]`, "jsx", nil, `<label htmlFor="a"></label>`},
		{`Foo.Bar`, "jsx", nil, `<Foo.Bar></Foo.Bar>`},
		{`div.{theme.style}`, "jsx", nil, `<div className={theme.style}></div>`},
	})
}

func TestExpandMarkupOverrideAttributes(t *testing.T) {
	runExpand(t, []expandCase{
		{`.bar`, "jsx", nil, `<div className="bar"></div>`},
		{`..bar`, "jsx", nil, `<div styleName={styles.bar}></div>`},
		{`..foo-bar`, "jsx", nil, `<div styleName={styles['foo-bar']}></div>`},
		{`.foo`, "vue", nil, `<div class="foo"></div>`},
		{`..foo`, "vue", nil, `<div :class="foo"></div>`},
	})
}

func TestExpandMarkupOverridesAttributesWithCustomConfig(t *testing.T) {
	runExpand(t, []expandCase{
		{`.foo`, "jsx", modJSXAttrs, `<div className="foo"></div>`},
		{`..foo`, "jsx", modJSXAttrs, `<div classStarName={styles.foo}></div>`},
		{`..foo`, "jsx", modJSXPrefix, `<div styleName={class.foo}></div>`},
	})
}

func TestExpandMarkupWrapWithAbbreviation(t *testing.T) {
	runExpand(t, []expandCase{
		{`div>ul`, "html", modTextDivs, "<div>\n\t<ul>\n\t\t<div>line1</div>\n\t\t<div>line2</div>\n\t</ul>\n</div>"},
		{`p`, "html", modTextString("foo\nbar"), "<p>\n\tfoo\n\tbar\n</p>"},
		{`p`, "html", func(c *Config) { c.Text = TextString(`<div>foo</div>`) }, "<p>\n\t<div>foo</div>\n</p>"},
		{`p`, "html", func(c *Config) { c.Text = TextString(`<span>foo</span>`) }, `<p><span>foo</span></p>`},
		{`p`, "html", func(c *Config) { c.Text = TextString(`foo<span>foo</span>`) }, `<p>foo<span>foo</span></p>`},
		{`p`, "html", func(c *Config) { c.Text = TextString(`foo<div>foo</div>`) }, `<p>foo<div>foo</div></p>`},
	})
}

func TestExpandMarkupWrapWithAbbreviationHref(t *testing.T) {
	runExpand(t, []expandCase{
		{`a`, "html", func(c *Config) { c.Text = TextLines(`www.google.it`) }, `<a href="http://www.google.it">www.google.it</a>`},
		{`a`, "html", func(c *Config) { c.Text = TextLines(`then www.google.it`) }, `<a href="">then www.google.it</a>`},
		{`a`, "html", func(c *Config) { c.Text = TextLines(`www.google.it`); c.Options.MarkupHref = false }, `<a href="">www.google.it</a>`},
		{`map[name="https://example.com"]`, "html", func(c *Config) { c.Text = TextLines(`some text`) }, `<map name="https://example.com">some text</map>`},
		{`map[href="https://example.com"]`, "html", func(c *Config) { c.Text = TextLines(`some text`) }, `<map name="" href="https://example.com">some text</map>`},
		{`map[name="https://example.com"]>b`, "html", func(c *Config) { c.Text = TextLines(`some text`) }, `<map name="https://example.com"><b>some text</b></map>`},
		{`a[href="https://example.com"]>b`, "html", func(c *Config) { c.Text = TextLines(`<u>some text false</u>`); c.Options.MarkupHref = false }, `<a href="https://example.com"><b><u>some text false</u></b></a>`},
		{`a[href="https://example.com"]>b`, "html", func(c *Config) { c.Text = TextLines(`<u>some text true</u>`); c.Options.MarkupHref = true }, `<a href="https://example.com"><b><u>some text true</u></b></a>`},
		{`a[href="https://example.com"]>div`, "html", func(c *Config) { c.Text = TextLines(`<p>some text false</p>`); c.Options.MarkupHref = false }, "<a href=\"https://example.com\">\n\t<div>\n\t\t<p>some text false</p>\n\t</div>\n</a>"},
		{`a[href="https://example.com"]>div`, "html", func(c *Config) { c.Text = TextLines(`<p>some text true</p>`); c.Options.MarkupHref = true }, "<a href=\"https://example.com\">\n\t<div>\n\t\t<p>some text true</p>\n\t</div>\n</a>"},
	})
}

func TestExpandMarkupClassNames(t *testing.T) {
	runExpand(t, []expandCase{
		{`div.foo/`, "html", nil, `<div class="foo">`},
		{`div.foo1/2`, "html", nil, `<div class="foo1/2"></div>`},
		{`div.foo.1/2`, "html", nil, `<div class="foo 1/2"></div>`},
	})
}

func TestExpandPugBasic(t *testing.T) {
	runExpand(t, []expandCase{
		{`!`, "pug", nil, "doctype html\nhtml(lang=\"en\")\n\thead\n\t\tmeta(charset=\"UTF-8\")\n\t\tmeta(name=\"viewport\", content=\"width=device-width, initial-scale=1.0\")\n\t\ttitle Document\n\tbody "},
	})
}
