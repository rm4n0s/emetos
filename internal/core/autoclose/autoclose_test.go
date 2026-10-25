package autoclose

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/language/languagetest"
	"github.com/rm4n0s/emetos/internal/core/textmate"
)

func TestParseSettingValues(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Strategy
		ok   bool
	}{
		{"languageDefined", LanguageDefined, true}, {"beforeWhitespace", BeforeWhitespace, true},
		{"always", Always, true}, {"never", Never, true}, {"", LanguageDefined, false}, {"auto", LanguageDefined, false},
	} {
		got, ok := ParseStrategy(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseStrategy(%q) = %v, %v", tc.in, got, ok)
		}
		if ok && got.String() != tc.in {
			t.Errorf("%v.String() = %q, want %q", got, got.String(), tc.in)
		}
	}
	for _, tc := range []struct {
		in   string
		want Behavior
		ok   bool
	}{{"auto", Auto, true}, {"always", AlwaysAct, true}, {"never", NeverAct, true}, {"x", Auto, false}} {
		got, ok := ParseBehavior(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseBehavior(%q) = %v, %v", tc.in, got, ok)
		}
		if ok && got.String() != tc.in {
			t.Errorf("%v.String() = %q, want %q", got, got.String(), tc.in)
		}
	}
	for _, tc := range []struct {
		in   string
		want Surround
		ok   bool
	}{
		{"languageDefined", SurroundLanguageDefined, true}, {"quotes", SurroundQuotes, true},
		{"brackets", SurroundBrackets, true}, {"never", SurroundNever, true}, {"x", SurroundLanguageDefined, false},
	} {
		got, ok := ParseSurround(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseSurround(%q) = %v, %v", tc.in, got, ok)
		}
		if ok && got.String() != tc.in {
			t.Errorf("%v.String() = %q, want %q", got, got.String(), tc.in)
		}
	}
}

func pair(open, closing string, notIn ...string) language.AutoClosingPair {
	return language.AutoClosingPair{Pair: language.Pair{Open: open, Close: closing}, NotIn: notIn}
}

func opens(pairs []*Pair) []string {
	var out []string
	for _, p := range pairs {
		out = append(out, p.Open)
	}
	return out
}

func TestLanguagePairs(t *testing.T) {
	c := &language.Configuration{AutoClosingPairs: []language.AutoClosingPair{
		pair("(", ")"), pair("(*", "*)"), pair("'", "'", "string"), pair("begin", "end"),
	}}
	l := NewLanguage(c)
	if got := opens(l.OpenByEnd("(")); !slices.Equal(got, []string{"("}) {
		t.Errorf("OpenByEnd(\"(\") = %v", got)
	}
	if got := opens(l.OpenByEnd("*")); !slices.Equal(got, []string{"(*"}) {
		t.Errorf("OpenByEnd(\"*\") = %v", got)
	}
	if got := opens(l.OpenByStart("(")); !slices.Equal(got, []string{"(", "(*"}) {
		t.Errorf("OpenByStart(\"(\") = %v", got)
	}
	if got := opens(l.CloseByStart("*")); !slices.Equal(got, []string{"(*"}) {
		t.Errorf("CloseByStart(\"*\") = %v", got)
	}
	if got := opens(l.CloseByEnd(")")); !slices.Equal(got, []string{"(", "(*"}) {
		t.Errorf("CloseByEnd(\")\") = %v", got)
	}
	if !l.IsCloseSingleChar(")") || !l.IsCloseSingleChar("'") || l.IsCloseSingleChar("*)") || l.IsCloseSingleChar("d") {
		t.Error("IsCloseSingleChar is wrong")
	}
	if l.Empty() {
		t.Error("a language with pairs is not empty")
	}
}

func TestPairsFallBackToBrackets(t *testing.T) {
	l := NewLanguage(&language.Configuration{Brackets: []language.Pair{{Open: "[", Close: "]"}}})
	if got := opens(l.OpenByEnd("[")); !slices.Equal(got, []string{"["}) {
		t.Errorf("pairs from the brackets = %v", got)
	}
	if c, ok := l.SurroundingClose("["); !ok || c != "]" {
		t.Errorf("surrounding pairs default to the auto-closing pairs: %q, %v", c, ok)
	}
	// An empty list of auto-closing pairs is a list: no pair, not the brackets.
	l = NewLanguage(&language.Configuration{AutoClosingPairs: []language.AutoClosingPair{}, Brackets: []language.Pair{{Open: "[", Close: "]"}}})
	if got := l.OpenByEnd("["); len(got) != 0 {
		t.Errorf("pairs = %v, want none", got)
	}
}

func TestSurroundingPairs(t *testing.T) {
	l := NewLanguage(&language.Configuration{
		AutoClosingPairs: []language.AutoClosingPair{pair("(", ")")},
		SurroundingPairs: []language.Pair{{Open: "<", Close: ""}, {Open: "\"", Close: "\""}},
	})
	if c, ok := l.SurroundingClose("<"); !ok || c != "" {
		t.Errorf("SurroundingClose(<) = %q, %v", c, ok)
	}
	if _, ok := l.SurroundingClose("("); ok {
		t.Error("the auto-closing pairs are not surrounding pairs when the language lists some")
	}
}

func TestNilLanguageIsEmpty(t *testing.T) {
	var l *Language
	if !l.Empty() || len(l.OpenByEnd("(")) != 0 || len(l.OpenByStart("(")) != 0 || len(l.CloseByStart(")")) != 0 ||
		len(l.CloseByEnd(")")) != 0 || l.IsCloseSingleChar(")") || l.BlockCommentStart() != "" {
		t.Error("a nil language must close nothing")
	}
	if _, ok := l.SurroundingClose("("); ok {
		t.Error("a nil language surrounds nothing")
	}
	if !NewLanguage(nil).Empty() {
		t.Error("no configuration, no pairs")
	}
	if !l.ShouldCloseBefore(LanguageDefined, false)(";") || l.ShouldCloseBefore(LanguageDefined, false)("a") {
		t.Error("a nil language closes before the default characters")
	}
	if l.ShouldCloseBefore(LanguageDefined, true)("`") {
		t.Error("quotes are not closed before a backtick by default")
	}
}

func TestShouldCloseBefore(t *testing.T) {
	set := "abc"
	l := NewLanguage(&language.Configuration{AutoCloseBefore: &set, AutoClosingPairs: []language.AutoClosingPair{pair("(", ")")}})
	for _, tc := range []struct {
		strategy  Strategy
		quotes    bool
		ch        string
		want      bool
		describes string
	}{
		{LanguageDefined, false, "a", true, "listed by the language"},
		{LanguageDefined, true, "b", true, "listed by the language, for quotes"},
		{LanguageDefined, false, ";", false, "not listed by the language"},
		{BeforeWhitespace, false, " ", true, "space"},
		{BeforeWhitespace, false, "\t", true, "tab"},
		{BeforeWhitespace, false, "a", false, "letter"},
		{Always, false, "x", true, "always"},
		{Never, false, " ", false, "never"},
	} {
		if got := l.ShouldCloseBefore(tc.strategy, tc.quotes)(tc.ch); got != tc.want {
			t.Errorf("%v %q (%s) = %v, want %v", tc.strategy, tc.ch, tc.describes, got, tc.want)
		}
	}
	d := NewLanguage(&language.Configuration{AutoClosingPairs: []language.AutoClosingPair{pair("(", ")")}})
	if !d.ShouldCloseBefore(LanguageDefined, false)("`") || d.ShouldCloseBefore(LanguageDefined, true)("`") {
		t.Error("the default set of brackets has the backtick, that of quotes has not")
	}
	if !d.ShouldCloseBefore(LanguageDefined, true)("}") {
		t.Error("the default set of quotes has }")
	}
}

func TestPairConditions(t *testing.T) {
	l := NewLanguage(&language.Configuration{AutoClosingPairs: []language.AutoClosingPair{
		pair("'", "'", "string", "comment"), pair("\"", "\"", "string"), pair("(", ")"), pair("/", "/", "regex"),
	}})
	for _, tc := range []struct {
		open string
		typ  textmate.TokenType
		want bool
	}{
		{"'", textmate.TokenOther, true}, {"'", textmate.TokenString, false}, {"'", textmate.TokenComment, false},
		{"\"", textmate.TokenComment, true}, {"\"", textmate.TokenString, false},
		{"(", textmate.TokenString, true}, {"/", textmate.TokenRegEx, false}, {"/", textmate.TokenOther, true},
	} {
		p := l.OpenByEnd(tc.open)[0]
		if got := p.OK(tc.typ); got != tc.want {
			t.Errorf("%q OK(%v) = %v, want %v", tc.open, tc.typ, got, tc.want)
		}
	}
	if p := l.OpenByEnd("(")[0]; p.Conditional() || p.Neutral() != "" {
		t.Error("a pair with no conditions asks nothing of the tokenizer")
	}
	// The neutral character is in neither text of the pair.
	if p := l.OpenByEnd("'")[0]; !p.Conditional() || p.Neutral() != "0" {
		t.Errorf("Neutral() = %q", p.Neutral())
	}
	digits := NewLanguage(&language.Configuration{AutoClosingPairs: []language.AutoClosingPair{pair("0123456789", "ab", "string")}})
	if got := digits.OpenByEnd("9")[0].Neutral(); got != "c" {
		t.Errorf("Neutral() = %q, want the first letter not in the pair", got)
	}
}

func TestBlockCommentStart(t *testing.T) {
	l := NewLanguage(&language.Configuration{Comments: &language.CommentRule{BlockComment: &language.Pair{Open: "/*", Close: "*/"}}})
	if got := l.BlockCommentStart(); got != "/*" {
		t.Errorf("BlockCommentStart() = %q", got)
	}
	if got := NewLanguage(&language.Configuration{Comments: &language.CommentRule{LineComment: "//"}}).BlockCommentStart(); got != "" {
		t.Errorf("BlockCommentStart() = %q, want none", got)
	}
}

func TestBeforeText(t *testing.T) {
	p := newPair(pair("begin", "end"))
	if !p.BeforeTextMatches("anything") {
		t.Error("a pair without beforeText matches any text")
	}
}

func TestRegistryLanguages(t *testing.T) {
	reg, err := languagetest.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	langs := NewRegistryLanguages(reg)
	code := reg.Code("go")
	l := langs.Language(code)
	if l.Empty() || len(l.OpenByEnd("{")) == 0 {
		t.Error("Go closes braces")
	}
	if langs.Language(code) != l {
		t.Error("a language is made once")
	}
	if c, ok := l.SurroundingClose("("); !ok || c != ")" {
		t.Errorf("Go surrounds with parentheses: %q, %v", c, ok)
	}
	if !langs.Language(reg.Code("plaintext")).Empty() && len(langs.Language(reg.Code("plaintext")).OpenByEnd("(")) > 0 {
		t.Error("plain text closes nothing")
	}
}
