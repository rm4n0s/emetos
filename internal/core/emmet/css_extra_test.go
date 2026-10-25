package emmet

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// The routes of the errors of the stylesheet part: every tag has a case.
func TestCSSErrorRoutes(t *testing.T) {
	cases := []struct {
		name, abbr, route, message string
		value                      bool
	}{
		{"a character that is nothing", "p10é", "TokenizeCSS.CSSUnexpectedCharacter", "Unexpected character at 4", false},
		{"a closing bracket alone", "p)", "TokenizeCSS.CSSUnexpectedBracket", "Unexpected bracket at 2", false},
		{"a field that is not closed", "p${1", "TokenizeCSS->cssGetToken->cssField.CSSFieldUnclosed", "Expecting } at 5", false},
		{"a variable that is not closed", "p${a", "cssField.CSSFieldUnclosed", "Expecting } at 5", false},
		{"a placeholder with a brace that is not closed", "p${1:{x", "cssField->cssPlaceholder.CSSPlaceholderUnclosed", "Expecting } at 7", false},
		{"a token that is nothing", "p10 ", "ParseCSSTokens.CSSUnexpectedToken", "Unexpected token at 3", false},
		{"a token that is nothing in the arguments", "f(a,b!)", "consumeArguments.CSSUnexpectedToken", "Unexpected token at 5", false},
		{"arguments nested too deep", strings.Repeat("f(", maxCSSNesting+2) + strings.Repeat(")", maxCSSNesting+2),
			"consumeValue->cssTokenScanner.consumeArguments.CSSNestingTooDeep", "nested too deep", false},
		{"a bracket in a value", ")", "TokenizeCSS.CSSUnexpectedBracket", "Unexpected bracket at 1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseCSS(tc.abbr, tc.value)
			if err == nil {
				t.Fatalf("%q: no error", tc.abbr)
			}
			wantRoute(t, err, tc.route)
			if !strings.Contains(err.Error(), tc.message) {
				t.Errorf("message %q does not have %q", err.Error(), tc.message)
			}
		})
	}

	// through Expand, with the route of the whole call
	cfg := newConfig(t, "css")
	_, err := Expand("p10é", cfg)
	wantRoute(t, err, "Expand->ExpandStylesheet->ParseStylesheet->ParseCSS->TokenizeCSS.CSSUnexpectedCharacter")
}

func TestCSSTagsAreCovered(t *testing.T) {
	tagged := map[string]func() error{
		"CSSUnexpectedCharacter": func() error { _, err := TokenizeCSS("é", false); return err },
		"CSSUnexpectedBracket":   func() error { _, err := TokenizeCSS(")", false); return err },
		"CSSFieldUnclosed":       func() error { _, err := TokenizeCSS("${1", false); return err },
		"CSSPlaceholderUnclosed": func() error { _, err := TokenizeCSS("${1:{", false); return err },
		"CSSUnexpectedToken":     func() error { _, err := ParseCSS(" ", false); return err },
		"CSSNestingTooDeep": func() error {
			_, err := ParseCSS(strings.Repeat("f(", maxCSSNesting+2), false)
			return err
		},
		"CSSSnippetValueEmpty": func() error { _, err := createSnippet("k", "x:|"); return err },
		"PreferenceValueInvalid": func() error {
			cfg := Config{Syntax: "css"}
			return cfg.ApplyPreferences(map[string]any{"css.intUnit": 3})
		},
	}
	for tag, make := range tagged {
		t.Run(tag, func(t *testing.T) {
			err := make()
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if appErr.Tag != tag {
				t.Errorf("tag %q, want %q (route %s)", appErr.Tag, tag, appErr.Route())
			}
			if !appErr.HasRoute(tag) {
				t.Errorf("route %s does not end with the tag", appErr.Route())
			}
		})
	}
}

func TestCSSSnippetValueEmptyRoute(t *testing.T) {
	_, err := createSnippet("k", "x:|")
	wantRoute(t, err, "createSnippet->parseSnippetValue.CSSSnippetValueEmpty")
}

// A snippet of the user that does not parse is left out and the user is told; it is no error.
func TestCSSBrokenSnippetsAreLeftOut(t *testing.T) {
	cfg := newConfig(t, "css")
	var warned []string
	cfg.Warn = func(message string, err error) {
		if err == nil {
			t.Error("a warning with no error")
		}
		warned = append(warned, message)
	}
	cfg.SetSnippets(map[string]string{"bad": "x:|", "worse": "y: (("})
	if got, err := Expand("m10", cfg); err != nil || got != "margin: 10px;" {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(warned) != 2 {
		t.Errorf("warnings: %v", warned)
	}
}

func TestApplyPreferences(t *testing.T) {
	cases := []struct {
		name   string
		syntax string
		prefs  map[string]any
		check  func(o Options) string
	}{
		{"units", "css", map[string]any{"css.intUnit": "pt", "css.floatUnit": "vh"}, func(o Options) string {
			if o.StylesheetIntUnit != "pt" || o.StylesheetFloatUnit != "vh" {
				return "units"
			}
			return ""
		}},
		{"aliases as an object", "css", map[string]any{"css.unitAliases": map[string]any{"q": "vmin"}}, func(o Options) string {
			if len(o.StylesheetUnitAliases) != 1 || o.StylesheetUnitAliases["q"] != "vmin" {
				return "aliases"
			}
			return ""
		}},
		{"aliases as a string", "css", map[string]any{"css.unitAliases": "e:em, p:% ,x:ex"}, func(o Options) string {
			if len(o.StylesheetUnitAliases) != 3 || o.StylesheetUnitAliases["p"] != "%" {
				return "aliases"
			}
			return ""
		}},
		{"fuzzy score", "css", map[string]any{"css.fuzzySearchMinScore": 0.5}, func(o Options) string {
			if o.StylesheetFuzzySearchMinScore != 0.5 {
				return "score"
			}
			return ""
		}},
		{"integer fuzzy score", "css", map[string]any{"css.fuzzySearchMinScore": 1}, func(o Options) string {
			if o.StylesheetFuzzySearchMinScore != 1 {
				return "score"
			}
			return ""
		}},
		{"short colors", "css", map[string]any{"css.color.short": false}, func(o Options) string {
			if o.StylesheetShortHex {
				return "short"
			}
			return ""
		}},
		{"css end and separator", "css", map[string]any{"css.propertyEnd": "", "css.valueSeparator": ":"}, func(o Options) string {
			if o.StylesheetAfter != "" || o.StylesheetBetween != ":" {
				return "css"
			}
			return ""
		}},
		{"scss reads the keys of css", "scss", map[string]any{"css.propertyEnd": "!"}, func(o Options) string {
			if o.StylesheetAfter != "!" {
				return "scss"
			}
			return ""
		}},
		{"sass reads its own", "sass", map[string]any{"sass.propertyEnd": ";", "css.propertyEnd": "!"}, func(o Options) string {
			if o.StylesheetAfter != ";" {
				return "sass"
			}
			return ""
		}},
		{"stylus reads its own", "stylus", map[string]any{"stylus.valueSeparator": " = "}, func(o Options) string {
			if o.StylesheetBetween != " = " {
				return "stylus"
			}
			return ""
		}},
		{"other syntaxes' keys and unknown keys are ignored", "css", map[string]any{"stylus.valueSeparator": "x", "bem.elementSeparator": "-", "nothing": 1}, func(o Options) string {
			if o.StylesheetBetween != ": " {
				return "ignored"
			}
			return ""
		}},
		{"nil", "css", nil, func(o Options) string { return "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newConfig(t, tc.syntax)
			if err := cfg.ApplyPreferences(tc.prefs); err != nil {
				t.Fatal(err)
			}
			if msg := tc.check(cfg.Options); msg != "" {
				t.Errorf("wrong: %s", msg)
			}
		})
	}
}

func TestApplyPreferencesErrors(t *testing.T) {
	cases := []struct {
		key   string
		value any
	}{
		{"css.intUnit", 3},
		{"css.floatUnit", true},
		{"css.unitAliases", 3},
		{"css.unitAliases", map[string]any{"r": 3}},
		{"css.unitAliases", "rem"},
		{"css.unitAliases", ":em"},
		{"css.fuzzySearchMinScore", "0.3"},
		{"css.fuzzySearchMinScore", 1.5},
		{"css.fuzzySearchMinScore", -0.1},
		{"css.color.short", "yes"},
		{"css.propertyEnd", 1},
		{"css.valueSeparator", nil},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			cfg := newConfig(t, "css")
			before := cfg.Options
			err := cfg.ApplyPreferences(map[string]any{"css.intUnit": "em", tc.key: tc.value})
			if err == nil {
				t.Fatalf("%v is accepted", tc.value)
			}
			wantRoute(t, err, "PreferenceValueInvalid")
			appErr, _ := errors.FromError(err)
			if appErr.Metadata["key"] != tc.key {
				t.Errorf("metadata %v has no key %s", appErr.Metadata, tc.key)
			}
			if cfg.Options.StylesheetIntUnit != before.StylesheetIntUnit {
				t.Error("nothing is changed when a value is not valid")
			}
		})
	}
}

func TestNewConfigTypes(t *testing.T) {
	for _, tc := range []struct {
		syntax string
		typ    SyntaxType
	}{
		{"html", TypeMarkup}, {"", TypeMarkup}, {"xml", TypeMarkup}, {"jsx", TypeMarkup}, {"pug", TypeMarkup}, {"vue", TypeMarkup},
		{"css", TypeStylesheet}, {"scss", TypeStylesheet}, {"sass", TypeStylesheet}, {"less", TypeStylesheet}, {"sss", TypeStylesheet}, {"stylus", TypeStylesheet},
	} {
		t.Run(tc.syntax, func(t *testing.T) {
			cfg := newConfig(t, tc.syntax)
			if cfg.Type != tc.typ {
				t.Errorf("type %q, want %q", cfg.Type, tc.typ)
			}
			if tc.typ == TypeStylesheet {
				if cfg.Snippets["bd"] == "" || cfg.Snippets["@m"] == "" || cfg.Snippets["@media"] == "" || cfg.Snippets["bq"] != "" {
					t.Error("the snippets are those of the stylesheets")
				}
				if cfg.Variables["lang"] == "" {
					t.Error("the variables are there")
				}
			} else if cfg.Snippets["bq"] == "" || cfg.Snippets["@m"] != "" {
				t.Error("the snippets are those of the markup")
			}
		})
	}
	if !IsStylesheetSyntax("sss") || IsStylesheetSyntax("html") || IsStylesheetSyntax("") {
		t.Error("IsStylesheetSyntax")
	}
}

// Expand takes the type from the configuration.
func TestExpandDispatchesByType(t *testing.T) {
	cfg := newConfig(t, "html")
	if got, _ := Expand("p10", cfg); got != "<p10></p10>" {
		t.Errorf("markup: %q", got)
	}
	cfg.Type = TypeStylesheet
	cfg.Snippets = newConfig(t, "css").Snippets
	if got, _ := Expand("p10", cfg); got != "padding: 10px;" {
		t.Errorf("stylesheet: %q", got)
	}
}

func TestJSToFixedAndFrac(t *testing.T) {
	cases := []struct {
		x      float64
		digits int
		fixed  string
		frac   string
	}{
		{0, 4, "0.0000", "0"},
		{1, 4, "1.0000", "1"},
		{0.5, 4, "0.5000", "0.5"},
		{10.25, 4, "10.2500", "10.25"},
		{100, 4, "100.0000", "100"},
		{-0.5, 4, "-0.5000", "-0.5"},
		{0.03125, 4, "0.0313", "0.0313"}, // an exact tie goes up, as JavaScript does (Go's own formatting gives 0.0312)
		{0.12345678, 4, "0.1235", "0.1235"},
		{0.99, 8, "0.99000000", "0.99"},
		{1.005, 2, "1.00", "1"}, // 1.005 is a little less than 1.005
		{123456.7, 0, "123457", "123457"},
		{1e21, 4, "1000000000000000000000", "1000000000000000000000"},
	}
	for _, tc := range cases {
		if got := jsToFixed(tc.x, tc.digits); got != tc.fixed {
			t.Errorf("jsToFixed(%v, %d) = %q, want %q", tc.x, tc.digits, got, tc.fixed)
		}
		if tc.digits == 4 {
			if got := frac(tc.x, 4); got != tc.frac {
				t.Errorf("frac(%v, 4) = %q, want %q", tc.x, got, tc.frac)
			}
		}
	}
	a, b := 0.1, 0.2
	if got := jsNumber(a + b); got != "0.30000000000000004" {
		t.Errorf("jsNumber = %q", got)
	}
}

func TestCSSColorOutput(t *testing.T) {
	cases := []struct {
		abbr  string
		short bool
		want  string
	}{
		{"c#", true, "color: #000;"},
		{"c#", false, "color: #000000;"},
		{"c#fc0", true, "color: #fc0;"},
		{"c#fc0", false, "color: #ffcc00;"},
		{"c#ffcc01", true, "color: #ffcc01;"},
		{"c#ffcc00aa", true, "color: #fc0;"}, // only the first six digits are read
		{"c#t", true, "color: transparent;"},
		{"c#0.5", true, "color: rgba(0, 0, 0, 0.5);"},
		{"c#f.123456789", true, "color: rgba(255, 255, 255, 0.12345679);"},
		{"c#0.", true, "color: #000;"},
	}
	for _, tc := range cases {
		t.Run(tc.abbr, func(t *testing.T) {
			cfg := newConfig(t, "css")
			cfg.Options.StylesheetShortHex = tc.short
			got, err := Expand(tc.abbr, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("Expand(%q) = %q, want %q", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestKeywordMapOrder(t *testing.T) {
	m := newKeywordMap()
	for _, k := range []string{"b", "200", "a", "100", "0100", "b", "4294967295", "1"} {
		m.set(k, &CSSToken{})
	}
	got := strings.Join(m.keys(), ",")
	// array indexes first in numeric order; 0100 and 4294967295 are not array indexes
	if want := "1,100,200,b,a,0100,4294967295"; got != want {
		t.Errorf("keys = %s, want %s", got, want)
	}
	if len(m.m) != 7 {
		t.Errorf("a key set twice is one: %d", len(m.m))
	}
}

func TestScoreMatchTable(t *testing.T) {
	cases := []struct {
		a, b    string
		partial bool
		want    float64 // -1: only checks that it is between 0 and 1
	}{
		{"aaa", "aaa", false, 1},
		{"AAA", "aaa", false, 1},
		{"baa", "aaa", false, 0},
		{"", "aaa", false, 0},
		{"a", "", true, 0},
		{"abcd", "abc", false, 0},
		{"abcd", "abc", true, -1},
		{"a", "abc", false, -1},
		{"ac", "abc", false, -1},
		{"ad", "abc", false, 0},
	}
	for _, tc := range cases {
		got := scoreMatch(tc.a, tc.b, tc.partial)
		switch {
		case tc.want == -1 && (got <= 0 || got >= 1):
			t.Errorf("scoreMatch(%q, %q, %v) = %v, want between 0 and 1", tc.a, tc.b, tc.partial, got)
		case tc.want != -1 && got != tc.want:
			t.Errorf("scoreMatch(%q, %q, %v) = %v, want %v", tc.a, tc.b, tc.partial, got, tc.want)
		}
	}
}

func TestUnmatchedPart(t *testing.T) {
	for _, tc := range []struct{ abbr, str, want string }{
		{"poas", "position", "as"},
		{"dib", "display", "b"},
		{"pos", "position", ""},
		{"", "position", ""},
		{"xyz", "abc", "xyz"},
	} {
		if got := unmatchedPart(tc.abbr, tc.str); got != tc.want {
			t.Errorf("unmatchedPart(%q, %q) = %q, want %q", tc.abbr, tc.str, got, tc.want)
		}
	}
}

// css.json is read as a whole: every snippet is a property or a plain snippet that parses.
func TestCSSAssetIsComplete(t *testing.T) {
	raw, err := readStringMap("assets/css.json")
	if err != nil {
		t.Fatal(err)
	}
	snippets := splitSnippetKeys(raw)
	if len(snippets) < 200 {
		t.Fatalf("css.json has %d snippets", len(snippets))
	}
	cfg := newConfig(t, "css")
	cfg.Warn = func(message string, err error) { t.Errorf("%s: %v", message, err) }
	list := convertSnippets(cfg.Snippets, &cfg)
	if len(list) != len(snippets) {
		t.Errorf("%d snippets converted of %d", len(list), len(snippets))
	}
	// the shorthands hold the specific properties
	var border *cssSnippet
	for _, s := range list {
		if s.key == "bd" {
			border = s
		}
	}
	if border == nil || border.kind != snippetProperty || len(border.dependencies) == 0 {
		t.Errorf("border: %+v", border)
	}
}
