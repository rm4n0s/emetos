package emmet

import "testing"

// Ported from Emmet: test/markup.ts

var (
	modBEM       = func(c *Config) { c.Options.BEMEnabled = true }
	modBEMCustom = func(c *Config) {
		c.Options.BEMEnabled = true
		c.Options.BEMElement = "-"
		c.Options.BEMModifier = "__"
	}
	modBEMContext = func(c *Config) {
		c.Options.BEMEnabled = true
		c.Context = &Context{Name: "div", Attributes: map[string]string{"class": "bl"}}
	}
)

// markupCase is an abbreviation, a change of the configuration (nil is none) and its parsed tree in the short form.
type markupCase struct {
	abbr string
	mod  func(*Config)
	want string
}

func runMarkup(t *testing.T, syntax string, cases []markupCase) {
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
			if got := stringifyMarkup(tree.Children); got != tc.want {
				t.Errorf("%q\n got %s\nwant %s", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestMarkupImplicitTags(t *testing.T) {
	runMarkup(t, "html", []markupCase{
		{`.`, nil, `<div class=""></div>`},
		{`.foo>.bar`, nil, `<div class="foo"><div class="bar"></div></div>`},
		{`p.foo>.bar`, nil, `<p class="foo"><span class="bar"></span></p>`},
		{`ul>.item*2`, nil, `<ul><li*2@0 class="item"></li><li*2@1 class="item"></li></ul>`},
		{`table>.row>.cell`, nil, `<table><tr class="row"><td class="cell"></td></tr></table>`},
		{`{test}`, nil, `test`},
		{`.{test}`, nil, `<div class="">test</div>`},
		{`ul>.item$*2`, nil, `<ul><li*2@0 class="item1"></li><li*2@1 class="item2"></li></ul>`},
	})
}

func TestMarkupXSL(t *testing.T) {
	runMarkup(t, "xsl", []markupCase{
		{`xsl:variable[select]`, nil, `<xsl:variable select=""></xsl:variable>`},
		{`xsl:with-param[select]`, nil, `<xsl:with-param select=""></xsl:with-param>`},
		{`xsl:variable[select]>div`, nil, `<xsl:variable><div></div></xsl:variable>`},
		{`xsl:with-param[select]{foo}`, nil, `<xsl:with-param>foo</xsl:with-param>`},
	})
}

func TestMarkupLabelPreprocessor(t *testing.T) {
	runMarkup(t, "html", []markupCase{
		{`label>input`, nil, `<label><input type="${1:text}" /></label>`},
		{`label>inp`, nil, `<label><input type="${1:text}" name="${1}" /></label>`},
		{`label>span>input`, nil, `<label><span><input type="${1:text}" /></span></label>`},
		{`label+inp`, nil, `<label for=""></label><input type="${1:text}" name="${1}" id="${1}" />`},
	})
}

func TestMarkupBEMModifiers(t *testing.T) {
	runMarkup(t, "html", []markupCase{
		{`div.b_m`, modBEM, `<div class="b b_m"></div>`},
		{`div.b._m`, modBEM, `<div class="b b_m"></div>`},
		{`div.b_m1._m2`, modBEM, `<div class="b b_m1 b_m2"></div>`},
		{`div.b>div._m`, modBEM, `<div class="b"><div class="b b_m"></div></div>`},
		{`div.b>div._m1>div._m2`, modBEM, `<div class="b"><div class="b b_m1"><div class="b b_m2"></div></div></div>`},
		{`div.b>div._m1-m2`, modBEM, `<div class="b"><div class="b b_m1-m2"></div></div>`},
	})
}

func TestMarkupBEMElements(t *testing.T) {
	runMarkup(t, "html", []markupCase{
		{`div.b>div.-e`, modBEM, `<div class="b"><div class="b__e"></div></div>`},
		{`div.b>div.---e`, modBEM, `<div class="b"><div class="b__e"></div></div>`},
		{`div.b>div.-e>div.-e`, modBEM, `<div class="b"><div class="b__e"><div class="b__e"></div></div></div>`},
		{`div`, modBEM, `<div></div>`},
		{`div.b1>div.b2_m1>div.-e1+div.---e2_m2`, modBEM, `<div class="b1"><div class="b2 b2_m1"><div class="b2__e1"></div><div class="b1__e2 b1__e2_m2"></div></div></div>`},
		{`div.b>div.-m1-m2`, modBEM, `<div class="b"><div class="b__m1-m2"></div></div>`},
		{`div.b_m_o`, modBEM, `<div class="b b_m_o"></div>`},
	})
}

func TestMarkupBEMCustomizeModifier(t *testing.T) {
	runMarkup(t, "html", []markupCase{
		{`div.b_m`, modBEMCustom, `<div class="b b__m"></div>`},
		{`div.b._m`, modBEMCustom, `<div class="b b__m"></div>`},
	})
}

func TestMarkupBEMMultipleClassesAfterModifierElement(t *testing.T) {
	runMarkup(t, "html", []markupCase{
		{`div.b_m.c`, modBEM, `<div class="b b_m c"></div>`},
		{`div.b>div._m.c`, modBEM, `<div class="b"><div class="b b_m c"></div></div>`},
		{`div.b>div.-m.c`, modBEM, `<div class="b"><div class="b__m c"></div></div>`},
	})
}

func TestMarkupBEMParentContext(t *testing.T) {
	runMarkup(t, "html", []markupCase{
		{`.-e_m`, modBEMContext, `<div class="bl__e bl__e_m"></div>`},
	})
}
