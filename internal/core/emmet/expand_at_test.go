package emmet

import (
	"strings"
	"testing"
)

func TestSyntaxFor(t *testing.T) {
	cases := []struct {
		name     string
		language string
		include  map[string]string
		excluded []string
		want     string
		ok       bool
	}{
		{name: "html", language: "html", want: "html", ok: true},
		{name: "css", language: "css", want: "css", ok: true},
		{name: "scss", language: "scss", want: "scss", ok: true},
		{name: "less", language: "less", want: "less", ok: true},
		{name: "sass", language: "sass", want: "sass", ok: true},
		{name: "stylus", language: "stylus", want: "stylus", ok: true},
		{name: "haml", language: "haml", want: "haml", ok: true},
		{name: "slim", language: "slim", want: "slim", ok: true},
		{name: "xml", language: "xml", want: "xml", ok: true},
		{name: "xsl", language: "xsl", want: "xsl", ok: true},
		{name: "pug is pug", language: "pug", want: "pug", ok: true},
		{name: "jade is pug", language: "jade", want: "pug", ok: true},
		{name: "sass-indented is sass", language: "sass-indented", want: "sass", ok: true},
		{name: "javascriptreact is jsx", language: "javascriptreact", want: "jsx", ok: true},
		{name: "typescriptreact is jsx", language: "typescriptreact", want: "jsx", ok: true},
		{name: "jsx-tags is jsx", language: "jsx-tags", want: "jsx", ok: true},

		// MAPPED_MODES
		{name: "php maps to html", language: "php", want: "html", ok: true},
		{name: "handlebars maps to html", language: "handlebars", want: "html", ok: true},

		// not supported
		{name: "javascript is unknown", language: "javascript"},
		{name: "go is unknown", language: "go"},
		{name: "vue is unknown", language: "vue"},
		{name: "empty language", language: ""},
		{name: "markdown is unknown even when it is not excluded", language: "markdown"},

		// emmet.includeLanguages
		{name: "include vue-html", language: "vue-html", include: map[string]string{"vue-html": "html"}, want: "html", ok: true},
		{name: "include javascript as javascriptreact", language: "javascript", include: map[string]string{"javascript": "javascriptreact"}, want: "jsx", ok: true},
		{name: "include overrides a mapped mode", language: "php", include: map[string]string{"php": "css"}, want: "css", ok: true},
		{name: "include of a value Emmet does not know", language: "vue", include: map[string]string{"vue": "nonsense"}},
		{name: "include of a value that is a syntax but no language mode", language: "vue", include: map[string]string{"vue": "pug"}},
		{name: "include is for the language only", language: "go", include: map[string]string{"vue-html": "html"}},

		// emmet.excludeLanguages
		{name: "markdown excluded", language: "markdown", excluded: []string{"markdown"}},
		{name: "html excluded", language: "html", excluded: []string{"markdown", "html"}, want: ""},
		{name: "not excluded", language: "css", excluded: []string{"markdown"}, want: "css", ok: true},
		{name: "the mapped target excluded", language: "php", excluded: []string{"html"}},
		{name: "the included language excluded", language: "vue-html", include: map[string]string{"vue-html": "html"}, excluded: []string{"vue-html"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SyntaxFor(tc.language, tc.include, tc.excluded)
			if got != tc.want || ok != tc.ok {
				t.Errorf("SyntaxFor(%q) = %q, %v; want %q, %v", tc.language, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestSettingsReaders(t *testing.T) {
	include := IncludeLanguagesFromSettings(map[string]any{"vue-html": "html", "bad": 3, "worse": nil})
	if len(include) != 1 || include["vue-html"] != "html" {
		t.Errorf("IncludeLanguagesFromSettings = %v", include)
	}
	for _, v := range []any{nil, "html", []any{"a"}, 3} {
		if got := IncludeLanguagesFromSettings(v); got != nil {
			t.Errorf("IncludeLanguagesFromSettings(%#v) = %v", v, got)
		}
	}
	excluded := ExcludedLanguagesFromSettings([]any{"markdown", 3, "html"})
	if strings.Join(excluded, ",") != "markdown,html" {
		t.Errorf("ExcludedLanguagesFromSettings = %v", excluded)
	}
	if got := ExcludedLanguagesFromSettings([]string{"a"}); len(got) != 1 {
		t.Errorf("a list of strings: %v", got)
	}
	for _, v := range []any{nil, "markdown", map[string]any{}} {
		if got := ExcludedLanguagesFromSettings(v); got != nil {
			t.Errorf("ExcludedLanguagesFromSettings(%#v) = %v", v, got)
		}
	}
}

func TestMappingForIncludedLanguages(t *testing.T) {
	got := MappingForIncludedLanguages(map[string]string{"vue-html": "html", "x": "nonsense", "php": "css"})
	want := map[string]string{"handlebars": "html", "php": "css", "vue-html": "html"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	if len(MappingForIncludedLanguages(nil)) != 2 {
		t.Error("the mapped modes alone are two")
	}
}

func TestIsAbbreviationValid(t *testing.T) {
	cases := []struct {
		syntax, abbr string
		want         bool
	}{
		// markup
		{"html", "ul>li", true},
		{"html", "div", true},
		{"html", ".item", true},
		{"html", "#main", true},
		{"html", "(a+b)*2", true},
		{"html", "[a]", true},
		{"html", "{text}", true},
		{"html", "a[href=x]", true},
		{"html", "br/", true},
		{"html", "li*3", true},
		{"html", "li.item$", true},
		{"html", "!", true},
		{"html", "!!!", true},
		{"html", "!a", false},
		{"html", "", false},
		{"html", "ul>", false},
		{"html", "ul>li+", false},
		{"html", "a>", false},
		{"html", "5", false},
		{"html", "1.5", false},
		{"html", ".5", false},
		{"html", "..", true},
		{"html", "-a", false},
		{"html", "a:", false},
		{"jsx", "Foo.Bar", true},
		// stylesheets
		{"css", "p10", true},
		{"css", "-webkit-p", true},
		{"css", "@m", true},
		{"css", "!", true},
		{"css", "#fc0", true},
		{"css", "#", false},
		{"css", "#1234567", false},
		{"css", "#xyz", false},
		{"css", "c#fc0", true},
		{"css", "div#main", false},
		{"css", "lorem#x", false},
		{"css", "foo#main", true},
		{"css", "10", false},
		{"css", ".5", false},
		{"css", "", false},
		{"scss", "m10", true},
		{"stylus", "m10", true},
	}
	for _, tc := range cases {
		t.Run(tc.syntax+" "+tc.abbr, func(t *testing.T) {
			if got := IsAbbreviationValid(tc.syntax, tc.abbr); got != tc.want {
				t.Errorf("IsAbbreviationValid(%q, %q) = %v, want %v", tc.syntax, tc.abbr, got, tc.want)
			}
		})
	}
}

func TestGetFilters(t *testing.T) {
	cases := []struct {
		text       string
		wantPos    int
		wantFilter string
	}{
		{"ul>li", 5, ""},
		{"ul>li|c", 5, "c"},
		{"ul>li|bem", 5, "bem"},
		{"ul>li|bem|c", 5, "bem,c"},
		{"ul>li|c|bem|t", 5, "c,bem,t"},
		{"ul>li|c|c|c|c", 7, "c,c,c"}, // at most three
		{"a|b", 3, ""},
		{"|c", 0, "c"},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			pos, filter := getFilters(tc.text, len(tc.text))
			if pos != tc.wantPos || filter != tc.wantFilter {
				t.Errorf("getFilters(%q) = %d, %q; want %d, %q", tc.text, pos, filter, tc.wantPos, tc.wantFilter)
			}
		})
	}
}

func TestExpandAt(t *testing.T) {
	cases := []struct {
		name  string
		req   ExpandRequest
		want  ExpandResult
		found bool
	}{
		// markup
		{"markup: a tree", ExpandRequest{Syntax: "html", TextBefore: "ul>li"},
			ExpandResult{Start: 0, End: 5, Abbreviation: "ul>li", Text: "<ul>\n\t<li>${1}</li>\n</ul>", Syntax: "html"}, true},
		{"markup: after text and white space", ExpandRequest{Syntax: "html", TextBefore: "\t<p>x</p> ul>li"},
			ExpandResult{Start: 10, End: 15, Abbreviation: "ul>li", Text: "<ul>\n\t<li>${1}</li>\n</ul>", Syntax: "html"}, true},
		{"markup: a base indentation", ExpandRequest{Syntax: "html", TextBefore: "\t\tul>li.item$*2", BaseIndent: "\t\t"},
			ExpandResult{Start: 2, End: 15, Abbreviation: "ul>li.item$*2",
				Text: "<ul>\n\t\t\t<li class=\"item1\">${1}</li>\n\t\t\t<li class=\"item2\">${2}</li>\n\t\t</ul>", Syntax: "html"}, true},
		{"markup: the indentation and line break of the editor", ExpandRequest{Syntax: "html", TextBefore: "ul>li", Indent: "  ", Newline: "\r\n"},
			ExpandResult{Start: 0, End: 5, Abbreviation: "ul>li", Text: "<ul>\r\n  <li>${1}</li>\r\n</ul>", Syntax: "html"}, true},
		{"markup: a snippet", ExpandRequest{Syntax: "html", TextBefore: "img"},
			ExpandResult{Start: 0, End: 3, Abbreviation: "img", Text: `<img src="${1}" alt="${2}">`, Syntax: "html"}, true},
		{"markup: a word is a tag", ExpandRequest{Syntax: "html", TextBefore: "foo hithere"},
			ExpandResult{Start: 4, End: 11, Abbreviation: "hithere", Text: "<hithere>${1}</hithere>", Syntax: "html"}, true},
		{"markup: the <div of html is div", ExpandRequest{Syntax: "html", TextBefore: "  <div"},
			ExpandResult{Start: 2, End: 6, Abbreviation: "div", Text: "<div>${1}</div>", Syntax: "html"}, true},
		{"markup: the < of other syntaxes is left", ExpandRequest{Syntax: "jsx", TextBefore: "<div"},
			ExpandResult{Start: 1, End: 4, Abbreviation: "div", Text: "<div>${1}</div>", Syntax: "jsx"}, true},
		{"markup: the places are bytes", ExpandRequest{Syntax: "html", TextBefore: "текст ul>li"},
			ExpandResult{Start: 11, End: 16, Abbreviation: "ul>li", Text: "<ul>\n\t<li>${1}</li>\n</ul>", Syntax: "html"}, true},
		{"markup: the comment filter", ExpandRequest{Syntax: "html", TextBefore: "ul>li.item|c"},
			ExpandResult{Start: 0, End: 12, Abbreviation: "ul>li.item", Text: "<ul>\n\t<li class=\"item\">${1}</li>\n\t<!-- /.item -->\n</ul>", Syntax: "html"}, true},
		{"markup: the bem filter", ExpandRequest{Syntax: "html", TextBefore: "div.block>.-elem|bem"},
			ExpandResult{Start: 0, End: 20, Abbreviation: "div.block>.-elem", Text: "<div class=\"block\">\n\t<div class=\"block__elem\">${1}</div>\n</div>", Syntax: "html"}, true},
		{"markup: a profile", ExpandRequest{Syntax: "html", TextBefore: "br", Profiles: map[string]any{"html": map[string]any{"selfClosingStyle": "xhtml"}}},
			ExpandResult{Start: 0, End: 2, Abbreviation: "br", Text: "<br />", Syntax: "html"}, true},
		{"markup: a variable", ExpandRequest{Syntax: "html", TextBefore: "!", Variables: map[string]string{"lang": "fr"}},
			ExpandResult{Start: 0, End: 1, Abbreviation: "!", Syntax: "html"}, true},
		{"markup: jsx closes the tags", ExpandRequest{Syntax: "jsx", TextBefore: "div.a>input"},
			ExpandResult{Start: 0, End: 11, Abbreviation: "div.a>input", Text: `<div className="a"><input type="${2:text}" /></div>`, Syntax: "jsx"}, true},
		{"markup: pug", ExpandRequest{Syntax: "pug", TextBefore: "ul>li"},
			ExpandResult{Start: 0, End: 5, Abbreviation: "ul>li", Text: "ul\n\tli ${1}", Syntax: "pug"}, true},
		{"markup: a selection", ExpandRequest{Syntax: "html", Selection: "ul>li"},
			ExpandResult{Start: 0, End: 5, Abbreviation: "ul>li", Text: "<ul>\n\t<li>${1}</li>\n</ul>", Syntax: "html"}, true},
		{"markup: a selection with a filter", ExpandRequest{Syntax: "html", Selection: "p.a|c"},
			ExpandResult{Start: 0, End: 5, Abbreviation: "p.a", Text: "<p class=\"a\">${1}</p>\n<!-- /.a -->", Syntax: "html"}, true},

		// stylesheets
		{"css: a property", ExpandRequest{Syntax: "css", TextBefore: "p10"},
			ExpandResult{Start: 0, End: 3, Abbreviation: "p10", Text: "padding: 10px;", Syntax: "css"}, true},
		{"css: after white space", ExpandRequest{Syntax: "css", TextBefore: "\tm10-20"},
			ExpandResult{Start: 1, End: 7, Abbreviation: "m10-20", Text: "margin: 10px 20px;", Syntax: "css"}, true},
		{"css: fields", ExpandRequest{Syntax: "css", TextBefore: "bd"},
			ExpandResult{Start: 0, End: 2, Abbreviation: "bd", Text: "border: ${1:1px} ${2:solid} ${3:#000};", Syntax: "css"}, true},
		{"css: a color", ExpandRequest{Syntax: "css", TextBefore: "c#fc0"},
			ExpandResult{Start: 0, End: 5, Abbreviation: "c#fc0", Text: "color: #fc0;", Syntax: "css"}, true},
		{"css: a color only", ExpandRequest{Syntax: "css", TextBefore: "#fc0"},
			ExpandResult{Start: 0, End: 4, Abbreviation: "#fc0", Text: "#fc0", Syntax: "css"}, true},
		{"css: important", ExpandRequest{Syntax: "css", TextBefore: "p0!"},
			ExpandResult{Start: 0, End: 3, Abbreviation: "p0!", Text: "padding: 0 !important;", Syntax: "css"}, true},
		{"css: an unknown name is a property", ExpandRequest{Syntax: "css", TextBefore: "xyz"},
			ExpandResult{Start: 0, End: 3, Abbreviation: "xyz", Text: "xyz: ${0};", Syntax: "css"}, true},
		{"css: an at-rule keeps its tabs and takes the base indentation", ExpandRequest{Syntax: "css", TextBefore: "    @m", BaseIndent: "    "},
			ExpandResult{Start: 4, End: 6, Abbreviation: "@m", Text: "@media ${1:screen} {\n    \t${0}\n    }", Syntax: "css"}, true},
		{"css: several properties", ExpandRequest{Syntax: "scss", TextBefore: "p10+m10", BaseIndent: "  "},
			ExpandResult{Start: 0, End: 7, Abbreviation: "p10+m10", Text: "padding: 10px;\n  margin: 10px;", Syntax: "scss"}, true},
		{"css: sass has no semicolon", ExpandRequest{Syntax: "sass", TextBefore: "p10"},
			ExpandResult{Start: 0, End: 3, Abbreviation: "p10", Text: "padding: 10px", Syntax: "sass"}, true},
		{"css: stylus has no colon", ExpandRequest{Syntax: "stylus", TextBefore: "p10"},
			ExpandResult{Start: 0, End: 3, Abbreviation: "p10", Text: "padding 10px", Syntax: "stylus"}, true},
		{"css: a $ of a variable is escaped", ExpandRequest{Syntax: "scss", TextBefore: "w$a-b"},
			ExpandResult{Start: 0, End: 5, Abbreviation: "w$a-b", Text: `width: \$a-b;`, Syntax: "scss"}, true},
		{"css: the places are bytes", ExpandRequest{Syntax: "css", TextBefore: "é p10"},
			ExpandResult{Start: 3, End: 6, Abbreviation: "p10", Text: "padding: 10px;", Syntax: "css"}, true},
		{"css: the preferences", ExpandRequest{Syntax: "css", TextBefore: "p10r", Preferences: map[string]any{"css.intUnit": "pt", "css.unitAliases": map[string]any{"r": "rem"}}},
			ExpandResult{Start: 0, End: 4, Abbreviation: "p10r", Text: "padding: 10rem;", Syntax: "css"}, true},
		{"css: the fuzzy score of VS Code", ExpandRequest{Syntax: "css", TextBefore: "foo", Preferences: map[string]any{"css.fuzzySearchMinScore": 0.9}},
			ExpandResult{Start: 0, End: 3, Abbreviation: "foo", Text: "foo: ${0};", Syntax: "css"}, true},
		{"css: a selection", ExpandRequest{Syntax: "css", Selection: "p10"},
			ExpandResult{Start: 0, End: 3, Abbreviation: "p10", Text: "padding: 10px;", Syntax: "css"}, true},

		// nothing to expand
		{"no syntax", ExpandRequest{TextBefore: "ul>li"}, ExpandResult{}, false},
		{"empty line", ExpandRequest{Syntax: "html", TextBefore: ""}, ExpandResult{}, false},
		{"a space before the cursor", ExpandRequest{Syntax: "html", TextBefore: "ul>li "}, ExpandResult{}, false},
		{"a tab before the cursor", ExpandRequest{Syntax: "css", TextBefore: "p10\t"}, ExpandResult{}, false},
		{"only an operator", ExpandRequest{Syntax: "html", TextBefore: "x = 5 +"}, ExpandResult{}, false},
		{"a number", ExpandRequest{Syntax: "html", TextBefore: "x = 1.5"}, ExpandResult{}, false},
		{"a dot and a number", ExpandRequest{Syntax: "html", TextBefore: ".5"}, ExpandResult{}, false},
		{"a closing tag", ExpandRequest{Syntax: "html", TextBefore: "<b>text</b>"}, ExpandResult{}, false},
		{"text that is not an abbreviation", ExpandRequest{Syntax: "html", TextBefore: "é"}, ExpandResult{}, false},
		{"stylesheet colon", ExpandRequest{Syntax: "css", TextBefore: "m:"}, ExpandResult{}, false},
		{"stylesheet selector colon", ExpandRequest{Syntax: "scss", TextBefore: "a:"}, ExpandResult{}, false},
		{"stylesheet selector with a tag and an id", ExpandRequest{Syntax: "css", TextBefore: "div#main"}, ExpandResult{}, false},
		{"stylesheet number", ExpandRequest{Syntax: "css", TextBefore: "10"}, ExpandResult{}, false},
		{"a selection that is no abbreviation", ExpandRequest{Syntax: "html", Selection: "hello world"}, ExpandResult{}, false},
		{"a selection with white space before", ExpandRequest{Syntax: "html", Selection: " ul"}, ExpandResult{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := ExpandAt(tc.req)
			if err != nil {
				t.Fatalf("ExpandAt: %v", err)
			}
			if ok != tc.found {
				t.Fatalf("ok = %v, want %v (result %+v)", ok, tc.found, got)
			}
			if !ok {
				return
			}
			if tc.name == "markup: a variable" {
				// the document, with the variable of the setting
				if !strings.Contains(got.Text, `<html lang="fr">`) {
					t.Errorf("the variable is not used: %q", got.Text)
				}
				got.Text = ""
			}
			if got != tc.want {
				t.Errorf("ExpandAt = %+v\nwant     %+v", got, tc.want)
			}
		})
	}
}

// With a selection, the text before the cursor is not read.
func TestExpandAtSelectionIgnoresTheLine(t *testing.T) {
	res, ok, err := ExpandAt(ExpandRequest{Syntax: "css", TextBefore: "ignored ", Selection: "m10"})
	if err != nil || !ok || res.Text != "margin: 10px;" || res.Start != 0 || res.End != 3 {
		t.Errorf("got %+v, %v, %v", res, ok, err)
	}
}

func TestExpandAtErrors(t *testing.T) {
	tooDeep := strings.Repeat("a>", maxNesting+2) + "a"
	cssDeep := strings.Repeat("f(", maxCSSNesting+2) + strings.Repeat(")", maxCSSNesting+2)
	cases := []struct {
		name, route string
		req         ExpandRequest
	}{
		{"markup nested too deep", "ExpandAt->Expand->Parse->ParseAbbreviation->ParseTokens->statements.NestingTooDeep",
			ExpandRequest{Syntax: "html", TextBefore: tooDeep}},
		{"stylesheet nested too deep", "cssTokenScanner.consumeArguments.CSSNestingTooDeep",
			ExpandRequest{Syntax: "css", TextBefore: cssDeep}},
		{"a preference that is wrong", "ExpandRequest.config->Config.ApplyPreferences->Config.ApplyPreferences.func1->prefString.PreferenceValueInvalid",
			ExpandRequest{Syntax: "css", TextBefore: "p10", Preferences: map[string]any{"css.intUnit": 3}}},
		{"a profile that is wrong", "ExpandAt->ExpandRequest.config->Config.ApplyProfiles.ProfileInvalid",
			ExpandRequest{Syntax: "html", TextBefore: "ul", Profiles: map[string]any{"html": "upper"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok, err := ExpandAt(tc.req)
			if err == nil || ok {
				t.Fatalf("ok = %v, error %v", ok, err)
			}
			wantRoute(t, err, tc.route)
		})
	}
}

func TestExpandAtDoesNotChangeTheRequest(t *testing.T) {
	prefs := map[string]any{"css.unitAliases": map[string]any{"r": "rem"}}
	vars := map[string]string{"lang": "fr"}
	req := ExpandRequest{Syntax: "css", TextBefore: "p10r", Preferences: prefs, Variables: vars}
	for range 2 {
		if _, ok, err := ExpandAt(req); err != nil || !ok {
			t.Fatalf("ok %v, error %v", ok, err)
		}
	}
	if len(prefs) != 1 || len(vars) != 1 || len(vars["lang"]) != 2 {
		t.Errorf("the request was changed: %v %v", prefs, vars)
	}
}
