package theme

import (
	"strings"
	"testing"
)

func TestParseSemanticSelector(t *testing.T) {
	tests := []struct {
		in        string
		typ       string
		modifiers []string
		lang      string
		ok        bool
	}{
		{"function", "function", nil, "", true},
		{"variable.readonly", "variable", []string{"readonly"}, "", true},
		{"*.declaration.static:go", "*", []string{"static", "declaration"}, "go", true},
		{"class:ts", "class", nil, "ts", true},
		{"", "", nil, "", false},
	}
	for _, tt := range tests {
		sel, ok := parseSemanticSelector(tt.in)
		if ok != tt.ok || sel.typ != tt.typ || strings.Join(sel.modifiers, ",") != strings.Join(tt.modifiers, ",") || sel.language != tt.lang {
			t.Errorf("%q = %+v, %v", tt.in, sel, ok)
		}
	}
}

func TestSemanticSelectorMatch(t *testing.T) {
	// Ported from VS Code: src/vs/workbench/services/themes/test/electron-browser/tokenStyleResolving.test.ts (the scores of
	// parseTokenSelector: a modifier is 100, the type 100 less its distance from the token's, a language 10)
	tests := []struct {
		selector  string
		typ       string
		modifiers []string
		lang      string
		want      int
	}{
		{"function", "function", nil, "go", 100},
		{"*", "function", nil, "go", 0},
		{"function.declaration", "function", []string{"declaration", "static"}, "go", 200},
		{"function.async", "function", []string{"declaration"}, "go", -1},
		{"method", "function", nil, "go", -1},
		{"method", "member", nil, "go", 99},
		{"member", "member", nil, "go", 100},
		{"function:go", "function", nil, "go", 110},
		{"function:ts", "function", nil, "go", -1},
		{"*.readonly", "variable", []string{"readonly"}, "go", 100},
	}
	for _, tt := range tests {
		sel, _ := parseSemanticSelector(tt.selector)
		if got := sel.match(tt.typ, tt.modifiers, tt.lang); got != tt.want {
			t.Errorf("%q against %s%v: %d, want %d", tt.selector, tt.typ, tt.modifiers, got, tt.want)
		}
	}
}

func darkModern(t *testing.T) *TokenTheme {
	t.Helper()
	th, err := LoadBundled("Dark Modern")
	if err != nil {
		t.Fatal(err)
	}
	return th.TokenTheme(nil)
}

func TestSemanticStyleFallsBackToTheTextMateRules(t *testing.T) {
	tt := darkModern(t)
	sem := tt.Semantic()
	if sem == nil || !sem.Enabled {
		t.Fatalf("Dark Modern asks for semantic highlighting: %+v", sem)
	}
	// a token type with no rule of its own is colored as the TextMate scope that stands for it
	tests := []struct {
		typ       string
		modifiers []string
		scope     string
	}{
		{"function", nil, "entity.name.function"},
		{"method", nil, "entity.name.function.member"},
		{"type", nil, "entity.name.type"},
		{"parameter", nil, "variable.parameter"},
		{"variable", []string{"readonly"}, "variable.other.constant"},
		{"property", nil, "variable.other.property"},
		{"namespace", nil, "entity.name.namespace"},
		{"keyword", nil, "keyword.control"},
		{"enumMember", nil, "variable.other.enummember"},
	}
	for _, tt2 := range tests {
		got, ok := sem.Style(tt2.typ, tt2.modifiers, "go")
		want, wantOK := tt.Match([]string{tt2.scope})
		if !wantOK {
			t.Fatalf("Dark Modern has no rule for %s", tt2.scope)
		}
		if !ok || !strings.EqualFold(got.Foreground, tt.Color(want.Foreground)) {
			t.Errorf("%s%v = %+v, %v; the color of %s is %s", tt2.typ, tt2.modifiers, got, ok, tt2.scope, tt.Color(want.Foreground))
		}
	}
	// a type nothing stands for has no style
	if st, ok := sem.Style("label", nil, "go"); ok {
		t.Errorf("label = %+v", st)
	}
	if st, ok := sem.Style("nonsense", []string{"static"}, "go"); ok {
		t.Errorf("nonsense = %+v", st)
	}
	// the answer is the same when it comes from the cache
	a, _ := sem.Style("function", nil, "go")
	b, _ := sem.Style("function", nil, "go")
	if a != b {
		t.Error("the cache changed the style")
	}
}

func TestSemanticRulesOfTheThemeAndTheSettings(t *testing.T) {
	th := &Theme{
		Type:                 TypeDark,
		Colors:               map[string]string{},
		SemanticHighlighting: true,
		SemanticTokenColors: map[string]any{
			"function":             "#ff0000",
			"function.declaration": map[string]any{"fontStyle": "bold underline"},
			"*.deprecated":         map[string]any{"strikethrough": true},
			"newOperator":          "#00ff00",
		},
		TokenColors: []RawRule{{Scope: ScopeText("entity.name.function"), Settings: &RuleSettings{Foreground: strPtr("#112233")}},
			{Scope: ScopeText("variable"), Settings: &RuleSettings{Foreground: strPtr("#445566"), FontStyle: strPtr("italic")}}},
	}
	tt := th.TokenTheme(nil)
	sem := tt.Semantic()
	// the rule of the theme wins over the TextMate color, and a more specific rule adds the font style
	st, ok := sem.Style("function", nil, "go")
	if !ok || st.Foreground != "#FF0000" || st.Set != 0 {
		t.Errorf("function = %+v", st)
	}
	st, _ = sem.Style("function", []string{"declaration"}, "go")
	if st.Foreground != "#FF0000" || st.Set != FontStyleItalic|FontStyleBold|FontStyleUnderline|FontStyleStrikethrough || st.On != FontStyleBold|FontStyleUnderline {
		t.Errorf("function.declaration = %+v (the color of the shorter rule, the font style of the longer)", st)
	}
	if got := st.Apply(FontStyleItalic | FontStyleBold); got != FontStyleBold|FontStyleUnderline {
		t.Errorf("a font style is replaced where the rule says: %v", got)
	}
	// a type that no rule is for takes the TextMate color of its scope, and a modifier rule applies to every type
	st, ok = sem.Style("variable", []string{"deprecated"}, "go")
	// the font style "italic" of the TextMate rule says the others are off, except the one the rule of the modifier set
	if !ok || st.Foreground != "#445566" || st.On != FontStyleItalic|FontStyleStrikethrough {
		t.Errorf("deprecated variable = %+v, %v", st, ok)
	}
	if got := st.Apply(FontStyleBold); got != FontStyleItalic|FontStyleStrikethrough {
		t.Errorf("a font style is replaced where the rules say: %v", got)
	}
	// the user's rules win over the theme's
	custom := sem.WithRules(map[string]any{"function": map[string]any{"foreground": "#abcdef", "italic": true}, "bad selector:": 3, "": "#fff"})
	st, _ = custom.Style("function", nil, "go")
	if st.Foreground != "#ABCDEF" || st.Set != FontStyleItalic || st.On != FontStyleItalic {
		t.Errorf("custom function = %+v", st)
	}
	if st, _ := sem.Style("function", nil, "go"); st.Foreground != "#FF0000" {
		t.Error("the rules of the settings changed the theme's semantic theme")
	}
	if same := sem.WithRules(nil); same != sem {
		t.Error("no rules, the same semantic theme")
	}
	if !custom.Enabled {
		t.Error("the flag of the theme is kept")
	}
	// rules for a language apply to it only
	lang := sem.WithRules(map[string]any{"function:go": "#010203"})
	if st, _ := lang.Style("function", nil, "go"); st.Foreground != "#010203" {
		t.Errorf("function:go = %+v", st)
	}
	if st, _ := lang.Style("function", nil, "rust"); st.Foreground != "#FF0000" {
		t.Errorf("function in rust = %+v", st)
	}
}

func TestSemanticStyleIsZeroAndApply(t *testing.T) {
	if !(SemanticStyle{}).IsZero() || (SemanticStyle{Foreground: "#fff"}).IsZero() {
		t.Error("IsZero")
	}
	if got := (SemanticStyle{}).Apply(FontStyleNotSet); got != FontStyleNone {
		t.Errorf("apply on a style that is not set = %v", got)
	}
}

func strPtr(s string) *string { return &s }
