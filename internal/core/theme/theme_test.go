package theme

import (
	"reflect"
	"strings"
	"testing"
)

func str(s string) *string { return &s }

// setting is a rule of a raw theme with a scope as a string ("" for none).
func setting(scope string, fontStyle, foreground, background *string) RawRule {
	r := RawRule{Settings: &RuleSettings{FontStyle: fontStyle, Foreground: foreground, Background: background}}
	if scope != "" {
		r.Scope = ScopeText(scope)
	}
	return r
}

func colorsOf(fg, bg string) RawRule { return setting("", nil, str(fg), str(bg)) }

// themeOf resolves raw rules the way Theme.createFromRawTheme does.
func themeOf(rules ...RawRule) *TokenTheme { return NewTokenTheme(ParseRules(rules)) }

// foregroundOf is the foreground color of the style that the scopes get, or "" for none, and
// whether any rule fit.
func foregroundOf(t *TokenTheme, scopes ...string) (string, bool) {
	style, ok := t.Match(scopes)
	if !ok {
		return "", false
	}
	return t.Color(style.Foreground), true
}

// Ported from vscode-textmate: src/tests/themes.test.ts ('Theme matching gives higher priority ...')
func TestMatchingPriority(t *testing.T) {
	cases := []struct {
		name   string
		rules  []RawRule
		scopes []string
		want   string
	}{
		{"deeper matches", []RawRule{
			setting("", nil, str("#100000"), str("#200000")),
			setting("punctuation.definition.string.begin.html", nil, str("#300000"), nil),
			setting("meta.tag punctuation.definition.string", nil, str("#400000"), nil),
		}, []string{"punctuation.definition.string.begin.html"}, "#300000"},
		{"parent matches 1", []RawRule{
			setting("", nil, str("#100000"), str("#200000")),
			setting("c a", nil, str("#300000"), nil),
			setting("d a.b", nil, str("#400000"), nil),
			setting("a", nil, str("#500000"), nil),
		}, []string{"d", "a.b"}, "#400000"},
		{"parent matches 2", []RawRule{
			setting("", nil, str("#100000"), str("#200000")),
			setting("meta.tag entity", nil, str("#300000"), nil),
			setting("meta.selector.css entity.name.tag", nil, str("#400000"), nil),
			setting("entity", nil, str("#500000"), nil),
		}, []string{"text.html.cshtml", "meta.tag.structure.any.html", "entity.name.tag.structure.any.html"}, "#300000"},
		{"microsoft/vscode#23460", []RawRule{
			colorsOf("#aec2e0", "#14191f"),
			setting("meta.structure.dictionary.json string.quoted.double.json", nil, str("#FF410D"), nil),
			setting("meta.structure.dictionary.json string.quoted.double.json", nil, str("#ffffff"), nil),
			setting("meta.structure.dictionary.value.json string.quoted.double.json", nil, str("#FF410D"), nil),
		}, []string{"source.json", "meta.structure.dictionary.json", "meta.structure.dictionary.value.json", "string.quoted.double.json"}, "#FF410D"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := foregroundOf(themeOf(tc.rules...), tc.scopes...)
			if !ok || got != tc.want {
				t.Errorf("got %q (%v), want %q", got, ok, tc.want)
			}
		})
	}
}

// Ported from vscode-textmate: src/tests/themes.test.ts (suite 'Theme matching can match')
func TestMatchingCan(t *testing.T) {
	theme := themeOf(
		colorsOf("#F8F8F2", "#272822"),
		setting("source, something", nil, nil, str("#100000")),
		RawRule{Scope: ScopeList("bar", "baz"), Settings: &RuleSettings{Background: str("#200000")}},
		setting("source.css selector bar", str("bold"), nil, nil),
		setting("constant", str("italic"), str("#300000"), nil),
		setting("constant.numeric", nil, str("#400000"), nil),
		setting("constant.numeric.hex", str("bold"), nil, nil),
		setting("constant.numeric.oct", str("bold italic underline"), nil, nil),
		setting("constant.numeric.dec", str(""), str("#500000"), nil),
		setting("storage.object.bar", str(""), str("#600000"), nil),
	)
	type want struct{ fontStyle, foreground, background string }
	cases := []struct {
		name   string
		scopes []string
		want   want
	}{
		{"simpleMatch1", []string{"source"}, want{"not set", "", "#100000"}},
		{"simpleMatch2", []string{"source.ts"}, want{"not set", "", "#100000"}},
		{"simpleMatch3", []string{"source.tss"}, want{"not set", "", "#100000"}},
		{"simpleMatch4", []string{"something"}, want{"not set", "", "#100000"}},
		{"simpleMatch5", []string{"something.ts"}, want{"not set", "", "#100000"}},
		{"simpleMatch6", []string{"something.tss"}, want{"not set", "", "#100000"}},
		{"simpleMatch7", []string{"baz"}, want{"not set", "", "#200000"}},
		{"simpleMatch8", []string{"baz.ts"}, want{"not set", "", "#200000"}},
		{"simpleMatch9", []string{"baz.tss"}, want{"not set", "", "#200000"}},
		{"simpleMatch10", []string{"constant"}, want{"italic", "#300000", ""}},
		{"simpleMatch11", []string{"constant.string"}, want{"italic", "#300000", ""}},
		{"simpleMatch12", []string{"constant.hex"}, want{"italic", "#300000", ""}},
		{"simpleMatch13", []string{"constant.numeric"}, want{"italic", "#400000", ""}},
		{"simpleMatch14", []string{"constant.numeric.baz"}, want{"italic", "#400000", ""}},
		{"simpleMatch15", []string{"constant.numeric.hex"}, want{"bold", "#400000", ""}},
		{"simpleMatch16", []string{"constant.numeric.hex.baz"}, want{"bold", "#400000", ""}},
		{"simpleMatch17", []string{"constant.numeric.oct"}, want{"italic bold underline", "#400000", ""}},
		{"simpleMatch18", []string{"constant.numeric.oct.baz"}, want{"italic bold underline", "#400000", ""}},
		{"simpleMatch19", []string{"constant.numeric.dec"}, want{"none", "#500000", ""}},
		{"simpleMatch20", []string{"constant.numeric.dec.baz"}, want{"none", "#500000", ""}},
		{"simpleMatch21", []string{"storage.object.bar"}, want{"none", "#600000", ""}},
		{"simpleMatch22", []string{"storage.object.bar.baz"}, want{"none", "#600000", ""}},
		{"simpleMatch23", []string{"storage.object.bart"}, want{"not set", "", ""}},
		{"simpleMatch24", []string{"storage.object"}, want{"not set", "", ""}},
		{"simpleMatch25", []string{"storage"}, want{"not set", "", ""}},
		{"defaultMatch1", []string{""}, want{"not set", "", ""}},
		{"defaultMatch2", []string{"bazz"}, want{"not set", "", ""}},
		{"defaultMatch3", []string{"asdfg"}, want{"not set", "", ""}},
		{"multiMatch1", []string{"bar"}, want{"not set", "", "#200000"}},
		{"multiMatch2", []string{"source.css", "selector", "bar"}, want{"bold", "", "#200000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			style, ok := theme.Match(tc.scopes)
			if !ok {
				t.Fatal("no style")
			}
			got := want{style.FontStyle.String(), theme.Color(style.Foreground), theme.Color(style.Background)}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMatchWithoutScopesIsTheDefaults(t *testing.T) {
	theme := themeOf(colorsOf("#F8F8F2", "#272822"), setting("a", nil, str("#111111"), nil))
	style, ok := theme.Match(nil)
	if !ok || style != theme.Defaults() {
		t.Errorf("Match(nil) = %+v, %v", style, ok)
	}
	if theme.Color(style.Foreground) != "#F8F8F2" || theme.Color(style.Background) != "#272822" {
		t.Errorf("defaults: %q on %q", theme.Color(style.Foreground), theme.Color(style.Background))
	}
	if theme.Color(0) != "" || theme.Color(99) != "" || theme.Color(-1) != "" {
		t.Error("a number that is no color has no color")
	}
}

// Ported from vscode-textmate: src/tests/themes.test.ts ('Theme parsing can parse')
func TestParseRules(t *testing.T) {
	got := ParseRules([]RawRule{
		colorsOf("#F8F8F2", "#272822"),
		setting("source, something", nil, nil, str("#100000")),
		{Scope: ScopeList("bar", "baz"), Settings: &RuleSettings{Background: str("#010000")}},
		setting("source.css selector bar", str("bold"), nil, nil),
		setting("constant", str("italic"), str("#ff0000"), nil),
		setting("constant.numeric", nil, str("#00ff00"), nil),
		setting("constant.numeric.hex", str("bold"), nil, nil),
		setting("constant.numeric.oct", str("bold italic underline"), nil, nil),
		setting("constant.numeric.bin", str("bold strikethrough"), nil, nil),
		setting("constant.numeric.dec", str(""), str("#0000ff"), nil),
		setting("foo", str(""), str("#CFA"), nil),
	})
	want := []Rule{
		{Scope: "", Index: 0, FontStyle: FontStyleNotSet, Foreground: "#F8F8F2", Background: "#272822"},
		{Scope: "source", Index: 1, FontStyle: FontStyleNotSet, Background: "#100000"},
		{Scope: "something", Index: 1, FontStyle: FontStyleNotSet, Background: "#100000"},
		{Scope: "bar", Index: 2, FontStyle: FontStyleNotSet, Background: "#010000"},
		{Scope: "baz", Index: 2, FontStyle: FontStyleNotSet, Background: "#010000"},
		{Scope: "bar", ParentScopes: []string{"selector", "source.css"}, Index: 3, FontStyle: FontStyleBold},
		{Scope: "constant", Index: 4, FontStyle: FontStyleItalic, Foreground: "#ff0000"},
		{Scope: "constant.numeric", Index: 5, FontStyle: FontStyleNotSet, Foreground: "#00ff00"},
		{Scope: "constant.numeric.hex", Index: 6, FontStyle: FontStyleBold},
		{Scope: "constant.numeric.oct", Index: 7, FontStyle: FontStyleBold | FontStyleItalic | FontStyleUnderline},
		{Scope: "constant.numeric.bin", Index: 8, FontStyle: FontStyleBold | FontStyleStrikethrough},
		{Scope: "constant.numeric.dec", Index: 9, FontStyle: FontStyleNone, Foreground: "#0000ff"},
		{Scope: "foo", Index: 10, FontStyle: FontStyleNone, Foreground: "#CFA"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

// Ported from vscode-textmate: src/tests/themes.test.ts ('Theme resolving issue #38' and '#35')
func TestParseRulesIssues(t *testing.T) {
	t.Run("#38 ignores rules with invalid colors", func(t *testing.T) {
		got := ParseRules([]RawRule{
			colorsOf("#cccccc", "#222222"),
			setting("variable", str(""), nil, nil),
			setting("variable.parameter", str("italic"), str(""), nil),
			setting("support.other.variable", str(""), nil, nil),
			setting("variable.other", str("normal"), str(""), nil),
			setting("variable.parameter.function.coffee", str("italic"), str("#F9D423"), nil),
		})
		want := []Rule{
			{Scope: "", Index: 0, FontStyle: FontStyleNotSet, Foreground: "#cccccc", Background: "#222222"},
			{Scope: "variable", Index: 1, FontStyle: FontStyleNone},
			{Scope: "variable.parameter", Index: 2, FontStyle: FontStyleItalic},
			{Scope: "support.other.variable", Index: 3, FontStyle: FontStyleNone},
			{Scope: "variable.other", Index: 4, FontStyle: FontStyleNone},
			{Scope: "variable.parameter.function.coffee", Index: 5, FontStyle: FontStyleItalic, Foreground: "#F9D423"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})
	t.Run("#35 trailing comma in a tmTheme scope selector", func(t *testing.T) {
		scope := strings.Join([]string{
			"meta.at-rule.return.scss,", "meta.at-rule.return.scss punctuation.definition,",
			"meta.at-rule.else.scss,", "meta.at-rule.else.scss punctuation.definition,",
			"meta.at-rule.if.scss,", "meta.at-rule.if.scss punctuation.definition,",
		}, "\n")
		got := ParseRules([]RawRule{colorsOf("#EFEFEF", "#25292C"), setting(scope, nil, str("#CC7832"), nil)})
		want := []Rule{
			{Scope: "", Index: 0, FontStyle: FontStyleNotSet, Foreground: "#EFEFEF", Background: "#25292C"},
			{Scope: "meta.at-rule.return.scss", Index: 1, FontStyle: FontStyleNotSet, Foreground: "#CC7832"},
			{Scope: "punctuation.definition", ParentScopes: []string{"meta.at-rule.return.scss"}, Index: 1, FontStyle: FontStyleNotSet, Foreground: "#CC7832"},
			{Scope: "meta.at-rule.else.scss", Index: 1, FontStyle: FontStyleNotSet, Foreground: "#CC7832"},
			{Scope: "punctuation.definition", ParentScopes: []string{"meta.at-rule.else.scss"}, Index: 1, FontStyle: FontStyleNotSet, Foreground: "#CC7832"},
			{Scope: "meta.at-rule.if.scss", Index: 1, FontStyle: FontStyleNotSet, Foreground: "#CC7832"},
			{Scope: "punctuation.definition", ParentScopes: []string{"meta.at-rule.if.scss"}, Index: 1, FontStyle: FontStyleNotSet, Foreground: "#CC7832"},
		}
		// The selectors of the string keep the line breaks between them, as vscode-textmate keeps
		// them (it splits at the commas only and trims each selector).
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})
}

// Ported from vscode-textmate: src/tests/themes.test.ts ('Theme resolving strcmp works' and 'strArrCmp works')
func TestComparisons(t *testing.T) {
	strs := []string{"bar", "z", "zu", "a", "ab", ""}
	rules := make([]Rule, len(strs))
	for i, s := range strs {
		rules[i] = Rule{Scope: s}
	}
	sortRules(rules)
	var got []string
	for _, r := range rules {
		got = append(got, r.Scope)
	}
	if want := []string{"", "a", "ab", "bar", "z", "zu"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sorted scopes %q, want %q", got, want)
	}

	cases := []struct {
		name       string
		a, b       []string
		aNil, bNil bool
		want       int
	}{
		{"001", nil, nil, true, true, 0},
		{"002", nil, []string{}, true, false, -1},
		{"003", nil, []string{"a"}, true, false, -1},
		{"004", []string{}, nil, false, true, 1},
		{"005", []string{"a"}, nil, false, true, 1},
		{"006", []string{}, []string{}, false, false, 0},
		{"007", []string{}, []string{"a"}, false, false, -1},
		{"008", []string{"a"}, []string{}, false, false, 1},
		{"009", []string{"a"}, []string{"a"}, false, false, 0},
		{"010", []string{"a", "b"}, []string{"a"}, false, false, 1},
		{"011", []string{"a"}, []string{"a", "b"}, false, false, -1},
		{"012", []string{"a", "b"}, []string{"a", "b"}, false, false, 0},
		{"013", []string{"a", "b"}, []string{"a", "c"}, false, false, -1},
		{"014", []string{"a", "c"}, []string{"a", "b"}, false, false, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := compareStringLists(tc.a, tc.b, tc.aNil, tc.bNil)
			if (got < 0) != (tc.want < 0) || (got > 0) != (tc.want > 0) {
				t.Errorf("got %d, want sign of %d", got, tc.want)
			}
		})
	}
}

// expectedRule and expectedNode build the tries that the resolving tests of vscode-textmate
// expect.
func expectedRule(depth int, parents []string, fontStyle FontStyle, foreground, background int) *trieRule {
	return &trieRule{scopeDepth: depth, parentScopes: parents, fontStyle: fontStyle, foreground: foreground, background: background}
}

func expectedNode(main *trieRule, withParents []*trieRule, children map[string]*trieNode) *trieNode {
	n := newTrieNode(main, withParents)
	for k, v := range children {
		n.children[k] = v
	}
	return n
}

// Ported from vscode-textmate: src/tests/themes.test.ts ('Theme resolving ...')
func TestResolving(t *testing.T) {
	// ids numbers the colors in the order a test names them, as ColorMap.getId does.
	type colors []string
	cases := []struct {
		name     string
		rules    []Rule
		colors   colors
		defaults Style
		root     func(id func(string) int) *trieNode
	}{
		{
			name: "always has defaults", rules: nil, colors: colors{"#000000", "#ffffff"},
			defaults: Style{FontStyle: FontStyleNone},
			root: func(func(string) int) *trieNode {
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, nil)
			},
		},
		{
			name: "respects incoming defaults 1", rules: []Rule{{Index: -1, FontStyle: FontStyleNotSet}}, colors: colors{"#000000", "#ffffff"},
			defaults: Style{FontStyle: FontStyleNone},
			root: func(func(string) int) *trieNode {
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, nil)
			},
		},
		{
			name: "respects incoming defaults 2", rules: []Rule{{Index: -1, FontStyle: FontStyleNone}}, colors: colors{"#000000", "#ffffff"},
			defaults: Style{FontStyle: FontStyleNone},
			root: func(func(string) int) *trieNode {
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, nil)
			},
		},
		{
			name: "respects incoming defaults 3", rules: []Rule{{Index: -1, FontStyle: FontStyleBold}}, colors: colors{"#000000", "#ffffff"},
			defaults: Style{FontStyle: FontStyleBold},
			root: func(func(string) int) *trieNode {
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, nil)
			},
		},
		{
			name: "respects incoming defaults 4", rules: []Rule{{Index: -1, FontStyle: FontStyleNotSet, Foreground: "#ff0000"}}, colors: colors{"#ff0000", "#ffffff"},
			defaults: Style{FontStyle: FontStyleNone},
			root: func(func(string) int) *trieNode {
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, nil)
			},
		},
		{
			name: "respects incoming defaults 5", rules: []Rule{{Index: -1, FontStyle: FontStyleNotSet, Background: "#ff0000"}}, colors: colors{"#000000", "#ff0000"},
			defaults: Style{FontStyle: FontStyleNone},
			root: func(func(string) int) *trieNode {
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, nil)
			},
		},
		{
			name: "can merge incoming defaults",
			rules: []Rule{
				{Index: -1, FontStyle: FontStyleNotSet, Background: "#ff0000"},
				{Index: -1, FontStyle: FontStyleNotSet, Foreground: "#00ff00"},
				{Index: -1, FontStyle: FontStyleBold},
			},
			colors:   colors{"#00ff00", "#ff0000"},
			defaults: Style{FontStyle: FontStyleBold},
			root: func(func(string) int) *trieNode {
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, nil)
			},
		},
		{
			name: "defaults are inherited",
			rules: []Rule{
				{Index: -1, FontStyle: FontStyleNotSet, Foreground: "#F8F8F2", Background: "#272822"},
				{Scope: "var", Index: -1, FontStyle: FontStyleNotSet, Foreground: "#ff0000"},
			},
			colors:   colors{"#F8F8F2", "#272822", "#ff0000"},
			defaults: Style{FontStyle: FontStyleNone},
			root: func(id func(string) int) *trieNode {
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, map[string]*trieNode{
					"var": expectedNode(expectedRule(1, nil, FontStyleNotSet, id("#ff0000"), 0), nil, nil),
				})
			},
		},
		{
			name: "same rules get merged",
			rules: []Rule{
				{Index: -1, FontStyle: FontStyleNotSet, Foreground: "#F8F8F2", Background: "#272822"},
				{Scope: "var", Index: 1, FontStyle: FontStyleBold},
				{Scope: "var", Index: 0, FontStyle: FontStyleNotSet, Foreground: "#ff0000"},
			},
			colors:   colors{"#F8F8F2", "#272822", "#ff0000"},
			defaults: Style{FontStyle: FontStyleNone},
			root: func(id func(string) int) *trieNode {
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, map[string]*trieNode{
					"var": expectedNode(expectedRule(1, nil, FontStyleBold, id("#ff0000"), 0), nil, nil),
				})
			},
		},
		{
			name: "rules are inherited 1",
			rules: []Rule{
				{Index: -1, FontStyle: FontStyleNotSet, Foreground: "#F8F8F2", Background: "#272822"},
				{Scope: "var", Index: -1, FontStyle: FontStyleBold, Foreground: "#ff0000"},
				{Scope: "var.identifier", Index: -1, FontStyle: FontStyleNotSet, Foreground: "#00ff00"},
			},
			colors:   colors{"#F8F8F2", "#272822", "#ff0000", "#00ff00"},
			defaults: Style{FontStyle: FontStyleNone},
			root: func(id func(string) int) *trieNode {
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, map[string]*trieNode{
					"var": expectedNode(expectedRule(1, nil, FontStyleBold, id("#ff0000"), 0), nil, map[string]*trieNode{
						"identifier": expectedNode(expectedRule(2, nil, FontStyleBold, id("#00ff00"), 0), nil, nil),
					}),
				})
			},
		},
		{
			name: "rules are inherited 2",
			rules: []Rule{
				{Index: -1, FontStyle: FontStyleNotSet, Foreground: "#F8F8F2", Background: "#272822"},
				{Scope: "var", Index: -1, FontStyle: FontStyleBold, Foreground: "#ff0000"},
				{Scope: "var.identifier", Index: -1, FontStyle: FontStyleNotSet, Foreground: "#00ff00"},
				{Scope: "constant", Index: 4, FontStyle: FontStyleItalic, Foreground: "#100000"},
				{Scope: "constant.numeric", Index: 5, FontStyle: FontStyleNotSet, Foreground: "#200000"},
				{Scope: "constant.numeric.hex", Index: 6, FontStyle: FontStyleBold},
				{Scope: "constant.numeric.oct", Index: 7, FontStyle: FontStyleBold | FontStyleItalic | FontStyleUnderline},
				{Scope: "constant.numeric.dec", Index: 8, FontStyle: FontStyleNone, Foreground: "#300000"},
			},
			// The color numbers follow the order the rules are inserted in: by scope.
			colors:   colors{"#F8F8F2", "#272822", "#100000", "#200000", "#300000", "#ff0000", "#00ff00"},
			defaults: Style{FontStyle: FontStyleNone},
			root: func(id func(string) int) *trieNode {
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, map[string]*trieNode{
					"var": expectedNode(expectedRule(1, nil, FontStyleBold, id("#ff0000"), 0), nil, map[string]*trieNode{
						"identifier": expectedNode(expectedRule(2, nil, FontStyleBold, id("#00ff00"), 0), nil, nil),
					}),
					"constant": expectedNode(expectedRule(1, nil, FontStyleItalic, id("#100000"), 0), nil, map[string]*trieNode{
						"numeric": expectedNode(expectedRule(2, nil, FontStyleItalic, id("#200000"), 0), nil, map[string]*trieNode{
							"hex": expectedNode(expectedRule(3, nil, FontStyleBold, id("#200000"), 0), nil, nil),
							"oct": expectedNode(expectedRule(3, nil, FontStyleBold|FontStyleItalic|FontStyleUnderline, id("#200000"), 0), nil, nil),
							"dec": expectedNode(expectedRule(3, nil, FontStyleNone, id("#300000"), 0), nil, nil),
						}),
					}),
				})
			},
		},
		{
			name: "rules with parent scopes",
			rules: []Rule{
				{Index: -1, FontStyle: FontStyleNotSet, Foreground: "#F8F8F2", Background: "#272822"},
				{Scope: "var", Index: -1, FontStyle: FontStyleBold, Foreground: "#100000"},
				{Scope: "var.identifier", Index: -1, FontStyle: FontStyleNotSet, Foreground: "#200000"},
				{Scope: "var", ParentScopes: []string{"source.css"}, Index: 1, FontStyle: FontStyleItalic, Foreground: "#300000"},
				{Scope: "var", ParentScopes: []string{"source.css"}, Index: 2, FontStyle: FontStyleUnderline},
			},
			colors:   colors{"#F8F8F2", "#272822", "#100000", "#300000", "#200000"},
			defaults: Style{FontStyle: FontStyleNone},
			root: func(id func(string) int) *trieNode {
				withParent := func() []*trieRule {
					return []*trieRule{expectedRule(1, []string{"source.css"}, FontStyleUnderline, id("#300000"), 0)}
				}
				return expectedNode(expectedRule(0, nil, FontStyleNotSet, 0, 0), nil, map[string]*trieNode{
					"var": expectedNode(expectedRule(1, nil, FontStyleBold, id("#100000"), 0), withParent(), map[string]*trieNode{
						"identifier": expectedNode(expectedRule(2, nil, FontStyleBold, id("#200000"), 0), withParent(), nil),
					}),
				})
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			theme := NewTokenTheme(tc.rules)
			// The colors are numbered as the trie meets them; the test gives the order.
			want := NewColorMap()
			for _, c := range tc.colors {
				want.ID(c)
			}
			if !reflect.DeepEqual(theme.colors, want) {
				t.Errorf("colors %v, want %v", theme.colors.Colors(), want.Colors())
			}
			defaults := tc.defaults
			defaults.Foreground, defaults.Background = want.ID(tc.colors[0]), want.ID(tc.colors[1])
			if tc.name == "defaults are inherited" || tc.name == "same rules get merged" || tc.name == "rules are inherited 1" ||
				tc.name == "rules are inherited 2" || tc.name == "rules with parent scopes" {
				defaults.Foreground, defaults.Background = want.ID("#F8F8F2"), want.ID("#272822")
			}
			if theme.defaults != defaults {
				t.Errorf("defaults %+v, want %+v", theme.defaults, defaults)
			}
			if got, wantRoot := theme.root, tc.root(want.ID); !reflect.DeepEqual(got, wantRoot) {
				t.Errorf("trie differs:\n got %s\nwant %s", dumpNode(got), dumpNode(wantRoot))
			}
		})
	}
}

// dumpNode writes a trie for a failure message.
func dumpNode(n *trieNode) string {
	var sb strings.Builder
	var walk func(n *trieNode, indent string)
	walk = func(n *trieNode, indent string) {
		sb.WriteString(indent + "main " + dumpRule(n.mainRule) + "\n")
		for _, r := range n.rulesWithParentScope {
			sb.WriteString(indent + "parents " + dumpRule(r) + "\n")
		}
		for k, c := range n.children {
			sb.WriteString(indent + k + ":\n")
			walk(c, indent+"  ")
		}
	}
	walk(n, "")
	return sb.String()
}

func dumpRule(r *trieRule) string {
	return strings.Join([]string{r.fontStyle.String(), strings.Join(r.parentScopes, "<")}, "|")
}

// Ported from vscode-textmate: src/tests/themes.test.ts ('Theme resolving a rule with child combinator')
func TestChildCombinator(t *testing.T) {
	theme := themeOf(
		setting("", nil, str("#100000"), nil),
		setting("b a", nil, str("#200000"), nil),
		setting("b > a", nil, str("#300000"), nil),
		setting("c > b > a", nil, str("#400000"), nil),
		setting("a", nil, str("#500000"), nil),
	)
	cases := []struct {
		scopes []string
		want   string
	}{
		{[]string{"b", "a"}, "#300000"},
		{[]string{"b", "c", "a"}, "#200000"},
		{[]string{"c", "b", "a"}, "#400000"},
		{[]string{"c", "b", "d", "a"}, "#200000"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.scopes, " "), func(t *testing.T) {
			if got, _ := foregroundOf(theme, tc.scopes...); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	// A child combinator that ends a selector never matches.
	bad := themeOf(setting("", nil, str("#100000"), nil), setting("a >", nil, str("#200000"), nil))
	if got, _ := foregroundOf(bad, "x", "a"); got == "#200000" {
		t.Error("a selector that ends with > matched")
	}
}

// Ported from vscode-textmate: src/tests/themes.test.ts ('Theme resolving should give deeper scopes higher specificity (#233)')
func TestDeeperScopesAreMoreSpecific(t *testing.T) {
	theme := themeOf(
		setting("", nil, str("#100000"), nil),
		setting("y.z a.b", nil, str("#200000"), nil),
		setting("x y a.b", nil, str("#300000"), nil),
	)
	cases := []struct {
		name   string
		scopes []string
		want   string
	}{
		{"x a.b", []string{"x", "a.b"}, ""},
		{"y a.b", []string{"y", "a.b"}, ""},
		{"y.z a", []string{"y.z", "a"}, ""},
		{"x y a.b", []string{"x", "y", "a.b"}, "#300000"},
		{"x y.z a.b", []string{"x", "y.z", "a.b"}, "#200000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			style, ok := theme.Match(tc.scopes)
			got := ""
			if ok {
				got = theme.Color(style.Foreground)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFontStyleString(t *testing.T) {
	cases := []struct {
		style FontStyle
		want  string
	}{
		{FontStyleNotSet, "not set"},
		{FontStyleNone, "none"},
		{FontStyleItalic, "italic"},
		{FontStyleBold | FontStyleItalic, "italic bold"},
		{FontStyleUnderline | FontStyleStrikethrough, "underline strikethrough"},
	}
	for _, tc := range cases {
		if got := tc.style.String(); got != tc.want {
			t.Errorf("%d: %q, want %q", tc.style, got, tc.want)
		}
	}
}

func TestHexColors(t *testing.T) {
	for color, want := range map[string]bool{
		"#fff": true, "#ffff": true, "#ffffff": true, "#ffffffff": true, "#FfA": true,
		"": false, "#": false, "fff": false, "#ff": false, "#fffff": false, "#gggggg": false, "#fffffff": false, "red": false,
	} {
		if got := isValidHexColor(color); got != want {
			t.Errorf("isValidHexColor(%q) = %v", color, got)
		}
	}
	m := NewColorMap()
	if m.ID("") != 0 || m.ID("#abc") != 1 || m.ID("#ABC") != 1 || m.ID("#def") != 2 {
		t.Error("colors are numbered in order of first use, case-insensitively")
	}
	if !reflect.DeepEqual(m.Colors(), []string{"", "#ABC", "#DEF"}) {
		t.Errorf("colors: %v", m.Colors())
	}
}
