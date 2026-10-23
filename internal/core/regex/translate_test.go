package regex

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// find compiles pattern and searches text from the rune offset from.
func find(t *testing.T, pattern string, opts Options, text string, from int) (Match, bool) {
	t.Helper()
	re, err := Compile(pattern, opts)
	if err != nil {
		t.Fatalf("Compile(%q): %v", pattern, err)
	}
	m, ok, err := re.Find([]rune(text), from)
	if err != nil {
		t.Fatalf("Find(%q): %v", pattern, err)
	}
	return m, ok
}

func TestOnigurumaPatternsMatchAsOnigurumaDoes(t *testing.T) {
	type span = Span
	tests := []struct {
		name    string
		pattern string
		opts    Options
		text    string
		from    int
		want    []span // nil for no match; the whole match first, then the groups
	}{
		// Anchors.
		{name: "start of text", pattern: `\Afoo`, text: "foo", want: []span{{0, 3}}},
		{name: "start of text not after a line break", pattern: `\Afoo`, text: "x\nfoo"},
		{name: "start of text disabled", pattern: `\Afoo`, opts: Options{DisableA: true}, text: "foo"},
		{name: "anchor position at the start", pattern: `\Gfoo`, text: "xfoo", from: 1, want: []span{{1, 4}}},
		{name: "anchor position elsewhere", pattern: `\Gfoo`, text: "xfoo"},
		{name: "anchor position disabled", pattern: `\Gfoo`, opts: Options{DisableG: true}, text: "foo"},
		{name: "start of line", pattern: `^b`, text: "a\nb", want: []span{{2, 3}}},
		{name: "start of line is not after the last line break", pattern: `^`, text: "a\n", from: 1, want: nil},
		{name: "start of line in an empty text", pattern: `^`, text: "", want: []span{{0, 0}}},
		{name: "end of line before a break", pattern: `a$`, text: "a\nb", want: []span{{0, 1}}},
		{name: "end of text", pattern: `a\z`, text: "a\n"},
		{name: "end of text or before the last break", pattern: `a\Z`, text: "a\n", want: []span{{0, 1}}},
		{name: "word boundary", pattern: `\bfoo\b`, text: "a foo b", want: []span{{2, 5}}},
		// Escapes.
		{name: "hex digit", pattern: `\h+`, text: "xyz09afAFg", want: []span{{3, 9}}},
		{name: "not a hex digit", pattern: `\H+`, text: "ab xyz", want: []span{{2, 6}}},
		{name: "hex byte", pattern: `\x41\x{42}C`, text: "ABC", want: []span{{0, 3}}},
		{name: "several code points", pattern: `\x{41 42}`, text: "AB", want: []span{{0, 2}}},
		{name: "supplementary plane", pattern: `\x{1F600}.`, text: "😀x", want: []span{{0, 2}}},
		{name: "control character", pattern: `\cA\e\t`, text: "\x01\x1b\t", want: []span{{0, 3}}},
		{name: "octal", pattern: `\012a`, text: "\na", want: []span{{0, 2}}},
		{name: "p without braces is a letter", pattern: `[\p]+`, text: "pp", want: []span{{0, 2}}},
		{name: "escaped punctuation", pattern: `\-\/\#\ \"`, text: `-/# "`, want: []span{{0, 5}}},
		{name: "line break", pattern: `a\Rb`, text: "a\r\nb", want: []span{{0, 4}}},
		{name: "any but a break", pattern: `a\Nb`, text: "a\nb"},
		{name: "any character", pattern: `a\Ob`, text: "a\nb", want: []span{{0, 3}}},
		// Dot and options.
		{name: "dot stops at a break", pattern: `a.b`, text: "a\nb"},
		{name: "m makes the dot take breaks", pattern: `(?m)a.b`, text: "a\nb", want: []span{{0, 3}}},
		{name: "m in a group", pattern: `(?m:a.)b`, text: "a\nb", want: []span{{0, 3}}},
		{name: "m turned off", pattern: `(?m)(?-m:a.b)`, text: "a\nb"},
		{name: "ignore case", pattern: `(?i)abc`, text: "xABC", want: []span{{1, 4}}},
		{name: "ignore case in a group only", pattern: `a(?i:b)c`, text: "aBc aBC", want: []span{{0, 3}}},
		{name: "ignore case from the options", pattern: `abc`, opts: Options{IgnoreCase: true}, text: "ABC", want: []span{{0, 3}}},
		{name: "ignore case turned off", pattern: `(?i)a(?-i:b)`, text: "AB Ab", want: []span{{3, 5}}},
		{name: "ignore case takes the rest of the alternatives", pattern: `a(?i)b|c`, text: "aC", want: []span{{0, 2}}},
		{name: "the rest of the alternatives is after the first part", pattern: `a(?i)b|c`, text: "C"},
		{name: "ignore case in a class", pattern: `(?i)[a-c]+`, text: "xABCx", want: []span{{1, 4}}},
		{name: "extended mode", pattern: "(?x) a b # a comment\n c", text: "abc", want: []span{{0, 3}}},
		{name: "extended mode keeps a space in a class", pattern: "(?x)[ ]a", text: " a", want: []span{{0, 2}}},
		{name: "extended mode keeps an escaped space", pattern: `(?x)a\ b`, text: "a b", want: []span{{0, 3}}},
		{name: "extended mode and a repetition", pattern: "(?x) a +", text: "aaa", want: []span{{0, 3}}},
		{name: "extended mode in a group", pattern: "(?x: a b ) c", text: "ab c", want: []span{{0, 4}}},
		{name: "comment group", pattern: `a(?#anything)b`, text: "ab", want: []span{{0, 2}}},
		// Repetition.
		{name: "possessive star", pattern: `a*+a`, text: "aaa"},
		{name: "possessive plus", pattern: `a++b`, text: "aab", want: []span{{0, 3}}},
		{name: "possessive question", pattern: `a?+a`, text: "a"},
		{name: "possessive group", pattern: `(?:ab)*+ab`, text: "abab"},
		{name: "interval then plus repeats the interval", pattern: `x{2}+`, text: "xxxxx", want: []span{{0, 4}}},
		{name: "exact interval then question is optional", pattern: `ax{2}?`, text: "axxx", want: []span{{0, 3}}},
		{name: "interval question is lazy", pattern: `ax{1,3}?`, text: "axxx", want: []span{{0, 2}}},
		{name: "interval with no minimum", pattern: `^ {,3}x`, text: "  x", want: []span{{0, 3}}},
		{name: "interval upper bound", pattern: `^ {,2}x`, text: "   x"},
		{name: "interval reversed is possessive", pattern: `a{3,1}a`, text: "aaa"},
		{name: "lazy star", pattern: `a.*?b`, text: "axbxb", want: []span{{0, 3}}},
		{name: "brace that is no interval", pattern: `a{x}`, text: "a{x}", want: []span{{0, 4}}},
		{name: "repeated repetition", pattern: `(?:a+)*b`, text: "aab", want: []span{{0, 3}}},
		// Classes.
		{name: "posix alpha and alnum", pattern: `[[:alpha:]_][[:alnum:]_]*`, text: "  héllo9 ", want: []span{{2, 8}}},
		{name: "posix negated", pattern: `[[:^alpha:]]+`, text: "ab12cd", want: []span{{2, 4}}},
		{name: "posix inside a negated class", pattern: `[^[:digit:]]+`, text: "ab12", want: []span{{0, 2}}},
		{name: "posix xdigit and space", pattern: `[[:xdigit:]]+[[:space:]]`, text: "zz1F \n", want: []span{{2, 5}}},
		{name: "posix punct", pattern: `[[:punct:]]+`, text: "a.,;b", want: []span{{1, 4}}},
		{name: "posix upper lower", pattern: `[[:upper:]][[:lower:]]+`, text: "xHello", want: []span{{1, 6}}},
		{name: "posix word and blank", pattern: `[[:word:]]+[[:blank:]]`, text: "ab \t", want: []span{{0, 3}}},
		{name: "posix cntrl graph print ascii", pattern: `[[:cntrl:]][[:graph:]][[:print:]][[:ascii:]]`, text: "\x01a b", want: []span{{0, 4}}},
		{name: "property alpha", pattern: `\p{Alpha}+`, text: "12ab", want: []span{{2, 4}}},
		{name: "property negated by caret", pattern: `\p{^Alpha}+`, text: "ab12", want: []span{{2, 4}}},
		{name: "property negated by capital", pattern: `\P{Alpha}+`, text: "ab12", want: []span{{2, 4}}},
		{name: "property category", pattern: `\p{Lu}\p{Ll}+`, text: "xÉcole", want: []span{{1, 6}}},
		{name: "property long name", pattern: `\p{Uppercase_Letter}`, text: "aB", want: []span{{1, 2}}},
		{name: "property in a class", pattern: `[\p{L}\d]+`, text: "-a1é-", want: []span{{1, 4}}},
		{name: "negated property in a class", pattern: `[\P{Alpha}x]+`, text: "ab1xc", want: []span{{2, 4}}},
		{name: "property script", pattern: `\p{Greek}+`, text: "abγδε", want: []span{{2, 5}}},
		{name: "property cased letter", pattern: `\p{LC}+`, text: "1aB2", want: []span{{1, 3}}},
		{name: "property any", pattern: `\p{Any}`, text: "\n", want: []span{{0, 1}}},
		{name: "octal in braces", pattern: `\o{101}\x4A[\x4a-\x4C]`, text: "AJK", want: []span{{0, 3}}},
		{name: "property mark", pattern: `\p{M}`, text: "é", want: []span{{1, 2}}},
		{name: "property alphabetic", pattern: `[\p{Alphabetic}@]+`, text: "-a@b-", want: []span{{1, 4}}},
		{name: "intersection with a negated class", pattern: `[a-z&&[^aeiou]]+`, text: "aeibcdo", want: []span{{3, 6}}},
		{name: "intersection of two ranges", pattern: `[a-m&&f-z]+`, text: "abfghnz", want: []span{{2, 5}}},
		{name: "negated intersection", pattern: `[^a-c&&b-d]+`, text: "bcdae", want: []span{{2, 5}}},
		{name: "nested class", pattern: `[a[bc]d]+`, text: "xabcdx", want: []span{{1, 5}}},
		{name: "nested negated class", pattern: `[a[^b]]+`, text: "ab", want: []span{{0, 1}}},
		{name: "closing bracket first is a letter", pattern: `[^]]+`, text: "ab]", want: []span{{0, 2}}},
		{name: "dash at the end", pattern: `[a-]+`, text: "-a-b", want: []span{{0, 3}}},
		{name: "escapes in a class", pattern: `[\n\t\]\\\-]+`, text: "a\n\t]\\-b", want: []span{{1, 6}}},
		{name: "backspace in a class", pattern: `[\b]`, text: "\x08", want: []span{{0, 1}}},
		{name: "range of escapes", pattern: `[\x41-\x43]+`, text: "xABCD", want: []span{{1, 4}}},
		{name: "several code points in a class", pattern: `[\x{85 2028}]+`, text: "a\u0085 ", want: []span{{1, 3}}},
		{name: "supplementary plane in a class", pattern: `[\x{1F600}-\x{1F64F}]`, text: "a😀", want: []span{{1, 2}}},
		{name: "hex digit in a class", pattern: `[\h-]+`, text: "xaF-", want: []span{{1, 4}}},
		{name: "digit and dash", pattern: `[\d-x]+`, text: "1-x", want: []span{{0, 3}}},
		{name: "a negated class takes a line break", pattern: `a[^b]c`, text: "a\nc", want: []span{{0, 3}}},
		// Groups.
		{name: "capture", pattern: `(a)(b)?(c)`, text: "ac", want: []span{{0, 2}, {0, 1}, {-1, -1}, {1, 2}}},
		{name: "named groups are numbered with the others", pattern: `(a)(?<x>b)(c)\k<x>`, text: "abcb", want: []span{{0, 4}, {0, 1}, {1, 2}, {2, 3}}},
		{name: "named group with quotes", pattern: `(?'x'a)\k'x'`, text: "aa", want: []span{{0, 2}, {0, 1}}},
		{name: "reference by number", pattern: `(a)\k<1>`, text: "aa", want: []span{{0, 2}, {0, 1}}},
		{name: "relative reference", pattern: `(a)(b)\k<-2>`, text: "aba", want: []span{{0, 3}, {0, 1}, {1, 2}}},
		{name: "reference before a digit", pattern: `(a)\10`, text: "a\x08", want: []span{{0, 2}, {0, 1}}},
		{name: "backreference", pattern: `(a+)b\1`, text: "aabaa", want: []span{{0, 5}, {0, 2}}},
		{name: "octal when there are few groups", pattern: `\101`, text: "A", want: []span{{0, 1}}},
		{name: "two digit reference with enough groups", pattern: `(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)\10`, text: "abcdefghijj", want: []span{{0, 11}, {0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5}, {5, 6}, {6, 7}, {7, 8}, {8, 9}, {9, 10}}},
		{name: "atomic group", pattern: `(?>a+)a`, text: "aaa"},
		{name: "lookahead", pattern: `a(?=b)`, text: "acab", want: []span{{2, 3}}},
		{name: "negative lookahead", pattern: `a(?!b)`, text: "abac", want: []span{{2, 3}}},
		{name: "lookbehind", pattern: `(?<=a|bc)d`, text: "xdbcd", want: []span{{4, 5}}},
		{name: "negative lookbehind", pattern: `(?<!a)d`, text: "adxd", want: []span{{3, 4}}},
		{name: "lookbehind reads before the start", pattern: `(?<=a)b`, text: "ab", from: 1, want: []span{{1, 2}}},
		{name: "conditional", pattern: `(a)?(?(1)b|c)`, text: "c ab", want: []span{{0, 1}, {-1, -1}}},
		{name: "conditional by name", pattern: `(?<q>")?x(?(<q>)")`, text: `"x"`, want: []span{{0, 3}, {0, 1}}},
		// Keep.
		{name: "keep", pattern: `foo\Kbar`, text: "foobar", want: []span{{3, 6}}},
		{name: "keep with a group", pattern: `(foo)\Kbar`, text: "foobar", want: []span{{3, 6}, {0, 3}}},
		// Calls of groups.
		{name: "call of a named group", pattern: `(?<d>\d+)-\g<d>`, text: "12-345", want: []span{{0, 6}, {0, 2}}},
		{name: "call of a group that comes later", pattern: `\g<w> (?<w>[a-z]+)`, text: "ab cd", want: []span{{0, 5}, {3, 5}}},
		{name: "call of a numbered group", pattern: `(\d)\g<1>\g<1>`, text: "123", want: []span{{0, 3}, {0, 1}}},
		{name: "relative call", pattern: `(\d)(\w)\g<-2>`, text: "1a2", want: []span{{0, 3}, {0, 1}, {1, 2}}},
		{name: "recursive call matches nested parentheses", pattern: `(\((?:[^()]++|\g<1>)*\))`, text: "x(a(b(c)d)e)y", want: []span{{1, 12}, {1, 12}}},
		{name: "recursion of the whole pattern", pattern: `\((?:[^()]|\g<0>)*\)`, text: "((a)(b))", want: []span{{0, 8}}},
		{name: "recursion is bounded", pattern: `(\((?:[^()]|\g<1>)*\))`, text: strings.Repeat("(", 20) + strings.Repeat(")", 20), want: []span{{13, 27}}},
		{name: "a call keeps the ignore case of its group", pattern: `(?<x>(?i)a)b\g<x>`, text: "bA", want: nil},
		{name: "a call keeps the ignore case of its group when on", pattern: `(?<x>(?i:a))b\g<x>`, text: "abA", want: []span{{0, 3}, {0, 1}}},
		// Absence.
		{name: "absence", pattern: `/\*(?~\*/)\*/`, text: "/* a */ b */", want: []span{{0, 7}}},
		{name: "absence as a lookahead", pattern: `\G(?=(?~\*/)$)`, text: "ab *\n", want: []span{{0, 0}}},
		{name: "absence as a lookahead that fails", pattern: `\G(?=(?~\*/)$)`, text: "ab */\n"},
		// Text.
		{name: "offsets are runes", pattern: `é+`, text: "aééb", want: []span{{1, 3}}},
		{name: "case folding of non-ASCII", pattern: `(?i)é`, text: "É", want: []span{{0, 1}}},
		{name: "search from an offset", pattern: `a`, text: "aaa", from: 2, want: []span{{2, 3}}},
		{name: "search from past the end", pattern: `a`, text: "a", from: 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, ok := find(t, tc.pattern, tc.opts, tc.text, tc.from)
			if tc.want == nil {
				if ok {
					t.Fatalf("matched %v, want no match", m.Groups)
				}
				return
			}
			if !ok {
				t.Fatalf("no match, want %v", tc.want)
			}
			for i, w := range tc.want {
				if i >= len(m.Groups) || m.Groups[i] != w {
					t.Fatalf("groups = %v, want %v", m.Groups, tc.want)
				}
			}
		})
	}
}

func TestCompileErrorsHaveATag(t *testing.T) {
	tests := []struct {
		pattern string
		tag     string
	}{
		{`(a`, "RegexUnbalancedParen"},
		{`a)`, "RegexUnbalancedParen"},
		{`(?#unclosed`, "RegexUnbalancedParen"},
		{`(?(1`, "RegexUnbalancedParen"},
		{`[a`, "RegexUnterminatedClass"},
		{`[a-z&&`, "RegexUnterminatedClass"},
		{`*a`, "RegexNothingToRepeat"},
		{`a|+`, "RegexNothingToRepeat"},
		{`\A*`, "RegexNothingToRepeat"},
		{`a\`, "RegexInvalidEscape"},
		{`\x`, "RegexInvalidEscape"},
		{`\u12`, "RegexInvalidEscape"},
		{`\x{110000}`, "RegexInvalidEscape"},
		{`\x{`, "RegexInvalidEscape"},
		{`\k`, "RegexInvalidEscape"},
		{`\p{L`, "RegexInvalidEscape"},
		{`(?z)`, "RegexInvalidGroup"},
		{`(?q:a)`, "RegexInvalidGroup"},
		{`(?(1)a|b|c)(x)`, "RegexInvalidGroup"},
		{`(?<>a)`, "RegexInvalidGroupName"},
		{`\k<a`, "RegexInvalidGroupName"},
		{`[z-a]`, "RegexInvalidRange"},
		{`\k<nope>`, "RegexUnknownGroup"},
		{`\2(a)`, "RegexUnknownGroup"},
		{`\g<9>`, "RegexUnknownGroup"},
		{`(a)\g<-2>`, "RegexUnknownGroup"},
		{`\p{Nope}`, "RegexUnknownProperty"},
		{`[[:nope:]]`, "RegexUnknownProperty"},
		{`(?W)a`, "RegexUnsupportedFeature"},
		{`\C-a`, "RegexUnsupportedFeature"},
		{`\y`, "RegexUnsupportedFeature"},
		{`(?(2)a|b)(a)`, "RegexCompileFailed"},
		{"(" + strings.Repeat("a", 3000) + ")" + strings.Repeat(`\g<1>`, 400), "RegexTooLarge"},
	}
	for _, tc := range tests {
		t.Run(tc.tag+" "+clip(tc.pattern), func(t *testing.T) {
			_, err := Compile(tc.pattern, Options{})
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("Compile") || !appErr.HasRoute(tc.tag) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tc.tag)
			}
		})
	}
	t.Run("ECMAScript", func(t *testing.T) {
		_, err := Compile(`(?<a`, Options{Syntax: ECMAScript})
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("Compile->compileECMAScript->translateECMAScript.RegexInvalidGroupName") {
			t.Errorf("err = %v", err)
		}
		_, err = Compile(`(?(2)a|b)(a)`, Options{Syntax: ECMAScript})
		appErr, ok = errors.FromError(err)
		if !ok || !appErr.HasRoute("RegexCompileFailed") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestASearchPastItsTimeIsAnError(t *testing.T) {
	re, err := Compile(`(a+)+$`, Options{Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, _, err = re.Find([]rune(strings.Repeat("a", 60)+"!"), 0)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Find.RegexTimeout") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("the search ran for %v", time.Since(start))
	}
}

func TestRegexpDescribesItself(t *testing.T) {
	re, err := Compile(`(?<a>x)(\A|\G)y`, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if re.Source() != `(?<a>x)(\A|\G)y` || re.NumGroups() != 2 || !re.UsesStartAnchor() || !re.UsesAnchorPosition() {
		t.Errorf("source %q, groups %d, A %v, G %v", re.Source(), re.NumGroups(), re.UsesStartAnchor(), re.UsesAnchorPosition())
	}
	if n, ok := re.GroupNumber("a"); !ok || n != 1 {
		t.Errorf("GroupNumber(a) = %d, %v", n, ok)
	}
	if _, ok := re.GroupNumber("b"); ok {
		t.Error("GroupNumber of a name that is not there")
	}
	if !strings.Contains(re.Translated(), `\k`) && re.Translated() == "" {
		t.Error("no translation")
	}
	plain, err := Compile(`x`, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.UsesStartAnchor() || plain.UsesAnchorPosition() {
		t.Error("anchors in a pattern that has none")
	}
}

func TestECMAScriptPatterns(t *testing.T) {
	type span = Span
	tests := []struct {
		name    string
		pattern string
		opts    Options
		text    string
		want    []span
	}{
		{name: "named groups are numbered in order", pattern: `(?<year>\d{4})-(\d{2})\k<year>`, text: "2024-102024", want: []span{{0, 11}, {0, 4}, {5, 7}}},
		{name: "a name used before its group matches nothing", pattern: `\k<a>(?<a>x)`, text: "x", want: []span{{0, 1}, {0, 1}}},
		{name: "any character class", pattern: `a[^]b`, text: "a\nb", want: []span{{0, 3}}},
		{name: "empty class never matches", pattern: `a[]b`, text: "ab"},
		{name: "ignore case", pattern: `abc`, opts: Options{Syntax: ECMAScript, IgnoreCase: true}, text: "xABC", want: []span{{1, 4}}},
		{name: "multiline anchors", pattern: `^b$`, opts: Options{Syntax: ECMAScript, Multiline: true}, text: "a\nb\nc", want: []span{{2, 3}}},
		{name: "anchors without multiline", pattern: `^b$`, text: "a\nb\nc"},
		{name: "lookbehind", pattern: `(?<=a)b`, text: "ab", want: []span{{1, 2}}},
		{name: "word characters are ASCII", pattern: `\w+`, text: "éab", want: []span{{1, 3}}},
		{name: "class with a bracket", pattern: `[\]a]+`, text: "x]a", want: []span{{1, 3}}},
		{name: "escaped group", pattern: `\(a\)`, text: "(a)", want: []span{{0, 3}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			opts.Syntax = ECMAScript
			m, ok := find(t, tc.pattern, opts, tc.text, 0)
			if tc.want == nil {
				if ok {
					t.Fatalf("matched %v, want no match", m.Groups)
				}
				return
			}
			if !ok {
				t.Fatalf("no match, want %v", tc.want)
			}
			for i, w := range tc.want {
				if i >= len(m.Groups) || m.Groups[i] != w {
					t.Fatalf("groups = %v, want %v", m.Groups, tc.want)
				}
			}
		})
	}
}

func TestTheCacheCompilesOnce(t *testing.T) {
	c := NewCache(2)
	a, err := c.Compile(`a+`, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := c.Compile(`a+`, Options{})
	if a != b {
		t.Error("the same pattern was compiled twice")
	}
	other, _ := c.Compile(`a+`, Options{IgnoreCase: true})
	if other == a {
		t.Error("options make other compilations")
	}
	_, err1 := c.Compile(`(`, Options{})
	_, err2 := c.Compile(`(`, Options{})
	if err1 == nil || err1 != err2 {
		t.Errorf("a failure is remembered too: %v, %v", err1, err2)
	}
	if c.Len() != 2 {
		t.Errorf("Len() = %d, want 2 (the oldest went)", c.Len())
	}
	if again, _ := c.Compile(`a+`, Options{}); again == a {
		t.Error("the oldest entry should have been dropped")
	}
}
