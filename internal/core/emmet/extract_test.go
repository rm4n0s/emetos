package emmet

import (
	"strings"
	"testing"
)

// Ports of reference/emmet/test/extract-abbreviation.ts. Emmet counts UTF-16 units and Emetos bytes,
// so the places after Cyrillic text differ (a Cyrillic letter is two bytes).

// extractAt is the helper of Emmet's tests: the | in the text is the position.
func extractAt(abbr string, opts ExtractOptions) (Extracted, bool) {
	caret := strings.IndexByte(abbr, '|')
	if caret != -1 {
		abbr = abbr[:caret] + abbr[caret+1:]
	} else {
		caret = len(abbr)
	}
	return ExtractAbbreviation(abbr, caret, opts)
}

// extractWant makes the result Emmet's tests expect: the end follows from the abbreviation.
func extractWant(abbreviation string, location int, start ...int) Extracted {
	s := location
	if len(start) > 0 {
		s = start[0]
	}
	return Extracted{Abbreviation: abbreviation, Location: location, Start: s, End: location + len(abbreviation)}
}

func TestExtractAbbreviation(t *testing.T) {
	markup := DefaultExtractOptions()
	stylesheet := markup
	stylesheet.Type = TypeStylesheet
	prefixed := func(prefix string) ExtractOptions {
		o := markup
		o.Prefix = prefix
		return o
	}
	long := "table>(tr.prefix-intro>td*1)+(tr.prefix-pro-con>th*1+td*3)+(tr.prefix-key-specs>th[colspan=2]*1+td[colspan=2]*3)+(tr.prefix-key-find-online>th[colspan=2]*1+td*2)"

	cases := []struct {
		group, text string
		opts        ExtractOptions
		want        Extracted
		found       bool
	}{
		// basic
		{"basic", ".bar", markup, extractWant(".bar", 0), true},
		{"basic", ".foo .bar", markup, extractWant(".bar", 5), true},
		{"basic", ".foo @bar", markup, extractWant("@bar", 5), true},
		{"basic", ".foo img/", markup, extractWant("img/", 5), true},
		{"basic", "текстdiv", markup, extractWant("div", 10), true},
		{"basic", `foo div[foo="текст" bar=текст2]`, markup, extractWant(`div[foo="текст" bar=текст2]`, 4), true},
		{"basic", long, markup, extractWant(long, 0), true},

		// abbreviation with operators
		{"operators", "a foo+bar.baz", markup, extractWant("foo+bar.baz", 2), true},
		{"operators", "a foo>bar+baz*3", markup, extractWant("foo>bar+baz*3", 2), true},

		// abbreviation with attributes
		{"attributes", "a foo[bar|]", markup, extractWant("foo[bar]", 2), true},
		{"attributes", `a foo[bar="baz" a b]`, markup, extractWant(`foo[bar="baz" a b]`, 2), true},
		{"attributes", "foo bar[a|] baz", markup, extractWant("bar[a]", 4), true},

		// tag test
		{"tag", `<foo>bar[a b="c"]>baz`, markup, extractWant(`bar[a b="c"]>baz`, 5), true},
		{"tag", "foo>bar", markup, extractWant("foo>bar", 0), true},
		{"tag", "<foo>bar", markup, extractWant("bar", 5), true},
		{"tag", `<foo>bar[a="d" b="c"]>baz`, markup, extractWant(`bar[a="d" b="c"]>baz`, 5), true},

		// stylesheet abbreviation
		{"stylesheet", "foo{bar|}", markup, extractWant("foo{bar}", 0), true},
		{"stylesheet", "foo{bar|}", stylesheet, extractWant("bar", 4), true},

		// prefixed extract
		{"prefix", `<foo>bar[a b="c"]>baz`, markup, extractWant(`bar[a b="c"]>baz`, 5), true},
		{"prefix", `<foo>bar[a b="c"]>baz`, prefixed("<"), extractWant(`foo>bar[a b="c"]>baz`, 1, 0), true},
		{"prefix", `<foo>bar[a b="<"]>baz`, prefixed("<"), extractWant(`foo>bar[a b="<"]>baz`, 1, 0), true},
		{"prefix", "<foo>bar{<}>baz", prefixed("<"), extractWant("foo>bar{<}>baz", 1, 0), true},
		{"prefix", `foo>>>bar[a b="c"]>baz`, prefixed(">>>"), extractWant(`bar[a b="c"]>baz`, 6, 3), true},
		{"prefix", `<foo>bar[a b="c"]>baz`, prefixed("&&"), Extracted{}, false},

		// brackets inside curly braces
		{"braces", "foo div{[}+a{}", markup, extractWant("div{[}+a{}", 4), true},
		{"braces", "div{}}", markup, Extracted{}, false},
		{"braces", "div{{}", markup, extractWant("{}", 4), true},
	}
	for _, tc := range cases {
		t.Run(tc.group+"/"+tc.text, func(t *testing.T) {
			got, ok := extractAt(tc.text, tc.opts)
			if ok != tc.found || got != tc.want {
				t.Errorf("extract(%q) = %+v, %v; want %+v, %v", tc.text, got, ok, tc.want, tc.found)
			}
		})
	}
}

func TestExtractIsAtHTMLTag(t *testing.T) {
	html := func(s string) bool { return isAtHTMLTag(&backwardScanner{text: s, pos: len(s)}) }
	valid := []string{
		// simple tag
		"<div>", "<div/>", "<div />", "</div>",
		// tag with attributes
		`<div foo="bar">`, "<div foo=bar>", "<div foo>", `<div a="b" c=d>`, "<div a=b c=d>", "<div a=^b$ c=d>", "<div a=b c=^%d]$>",
		"<div title=привет>", "<div title=привет123>", "<foo-bar>",
	}
	invalid := []string{"div>", "<div", "<div привет>", "<div =bar>", "<div foo=>", "[a=b c=d]>", "div[a=b c=d]>"}
	for _, s := range valid {
		t.Run("valid "+s, func(t *testing.T) {
			if !html(s) {
				t.Errorf("%q is a tag", s)
			}
		})
	}
	for _, s := range invalid {
		t.Run("invalid "+s, func(t *testing.T) {
			if html(s) {
				t.Errorf("%q is no tag", s)
			}
		})
	}
}

func TestExtractConsumeQuoted(t *testing.T) {
	cases := []struct {
		text    string
		ok      bool
		wantPos int
	}{
		{` "foo"`, true, 1},
		{`"foo"`, true, 0},
		{`""`, true, 0},
		{`"a\"b"`, true, 0},
		{"foo", false, 3}, // does not eat anything
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			sc := &backwardScanner{text: tc.text, pos: len(tc.text)}
			if ok := consumeQuoted(sc); ok != tc.ok || sc.pos != tc.wantPos {
				t.Errorf("consumeQuoted(%q) = %v at %d, want %v at %d", tc.text, ok, sc.pos, tc.ok, tc.wantPos)
			}
		})
	}
}

// Behavior of the options and of the limit, which Emmet's tests do not have.
func TestExtractOptions(t *testing.T) {
	look := DefaultExtractOptions()
	noLook := look
	noLook.LookAhead = false
	css := look
	css.Type = TypeStylesheet

	cases := []struct {
		name, text string
		pos        int
		opts       ExtractOptions
		want       string
		found      bool
	}{
		{"look ahead takes the closing bracket", "a[b]", 3, look, "a[b]", true},
		{"no look ahead stops at the open bracket", "a[b]", 3, noLook, "b", true},
		{"look ahead takes a quote and brackets", `a[b="c"]`, 6, look, `a[b="c"]`, true},
		{"position is clamped", "ul>li", 100, look, "ul>li", true},
		{"negative position is clamped", "ul>li", -3, look, "", false},
		{"leading operators are removed", "x ++>a", 6, look, "a", true},
		{"nothing before the position", "  ", 2, look, "", false},
		{"stylesheet balances parentheses only", "p10 (m[0)", 9, css, "", false},
		{"stylesheet: a color", "div c#fc0", 9, css, "c#fc0", true},
		{"unbalanced closing bracket", "a)", 2, noLook, "", false},
		{"a word with a non-ASCII byte in it stops", "aéb", 4, look, "b", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractAbbreviation(tc.text, tc.pos, tc.opts)
			if ok != tc.found || got.Abbreviation != tc.want {
				t.Errorf("ExtractAbbreviation(%q, %d) = %+v, %v; want %q, %v", tc.text, tc.pos, got, ok, tc.want, tc.found)
			}
		})
	}
}

func TestExtractLimit(t *testing.T) {
	opts := DefaultExtractOptions()
	// An abbreviation of exactly the limit is found, a longer one is not
	atLimit := strings.Repeat("a", MaxExtractLength)
	if got, ok := ExtractAbbreviation(atLimit, len(atLimit), opts); !ok || len(got.Abbreviation) != MaxExtractLength {
		t.Errorf("an abbreviation of the limit: %v, %d", ok, len(got.Abbreviation))
	}
	tooLong := "x " + strings.Repeat("a", MaxExtractLength+1)
	if got, ok := ExtractAbbreviation(tooLong, len(tooLong), opts); ok {
		t.Errorf("an abbreviation over the limit is found: %d bytes", len(got.Abbreviation))
	}
	// A line a megabyte long of closing braces is bounded and quick
	big := strings.Repeat("}", 1<<20)
	if _, ok := ExtractAbbreviation(big, len(big), opts); ok {
		t.Error("braces are found")
	}
}
