package find

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: src/vs/editor/contrib/find/test/browser/replacePattern.test.ts
//
// The JavaScript expectations of "replace has JavaScript semantics" (which VS Code computes with
// String.replace) are written out.

// piece builders, as the test writes ReplacePiece.staticValue, matchIndex and caseOps
func staticPiece(s string) replacePiece { return replacePiece{static: s, isStatic: true} }
func groupPiece(i int) replacePiece     { return replacePiece{group: i} }
func casePiece(i int, ops ...rune) replacePiece {
	return replacePiece{group: i, caseOps: ops}
}

// patternOf is VS Code's ReplacePattern constructor: no pieces or one static piece is a static value.
func patternOf(pieces ...replacePiece) ReplacePattern {
	switch {
	case len(pieces) == 0:
		return ReplacePattern{}
	case len(pieces) == 1 && pieces[0].isStatic:
		return ReplacePattern{static: pieces[0].static}
	}
	return ReplacePattern{pieces: pieces}
}

func TestParseReplaceStringVSCode(t *testing.T) {
	s, g := staticPiece, groupPiece
	cases := []struct {
		in   string
		want []replacePiece
	}{
		// no backslash => no treatment
		{"hello", []replacePiece{s("hello")}},
		// \t => TAB, \n => LF
		{"\\thello", []replacePiece{s("\thello")}},
		{"h\\tello", []replacePiece{s("h\tello")}},
		{"hello\\t", []replacePiece{s("hello\t")}},
		{"\\nhello", []replacePiece{s("\nhello")}},
		// \\t => \t
		{"\\\\thello", []replacePiece{s("\\thello")}},
		{"h\\\\tello", []replacePiece{s("h\\tello")}},
		{"hello\\\\t", []replacePiece{s("hello\\t")}},
		// \\\t => \TAB
		{"\\\\\\thello", []replacePiece{s("\\\thello")}},
		// \\\\t => \\t
		{"\\\\\\\\thello", []replacePiece{s("\\\\thello")}},
		// \ at the end, with an unknown char or a back reference => no treatment
		{"hello\\", []replacePiece{s("hello\\")}},
		{"hello\\x", []replacePiece{s("hello\\x")}},
		{"hello\\0", []replacePiece{s("hello\\0")}},

		{"hello$&", []replacePiece{s("hello"), g(0)}},
		{"hello$0", []replacePiece{s("hello"), g(0)}},
		{"hello$02", []replacePiece{s("hello"), g(0), s("2")}},
		{"hello$1", []replacePiece{s("hello"), g(1)}},
		{"hello$2", []replacePiece{s("hello"), g(2)}},
		{"hello$9", []replacePiece{s("hello"), g(9)}},
		{"$9hello", []replacePiece{g(9), s("hello")}},

		{"hello$12", []replacePiece{s("hello"), g(12)}},
		{"hello$99", []replacePiece{s("hello"), g(99)}},
		{"hello$99a", []replacePiece{s("hello"), g(99), s("a")}},
		{"hello$1a", []replacePiece{s("hello"), g(1), s("a")}},
		{"hello$100", []replacePiece{s("hello"), g(10), s("0")}},
		{"hello$100a", []replacePiece{s("hello"), g(10), s("0a")}},
		{"hello$10a0", []replacePiece{s("hello"), g(10), s("a0")}},
		{"hello$$", []replacePiece{s("hello$")}},
		{"hello$$0", []replacePiece{s("hello$0")}},

		{"hello$`", []replacePiece{s("hello$`")}},
		{"hello$'", []replacePiece{s("hello$'")}},

		// case modifiers
		{"hello\\U$1", []replacePiece{s("hello"), casePiece(1, 'U')}},
		{"hello\\u$1", []replacePiece{s("hello"), casePiece(1, 'u')}},
		{"hello\\L$1", []replacePiece{s("hello"), casePiece(1, 'L')}},
		{"hello\\l$1", []replacePiece{s("hello"), casePiece(1, 'l')}},
		{"hello$1\\u\\u\\U$4goodbye", []replacePiece{s("hello"), g(1), casePiece(4, 'u', 'u', 'U'), s("goodbye")}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got, want := ParseReplaceString(tc.in), patternOf(tc.want...); !reflect.DeepEqual(got, want) {
				t.Errorf("parsed %+v, want %+v", got, want)
			}
		})
	}
	if got := ParseReplaceString(""); !reflect.DeepEqual(got, ReplacePattern{}) || got.HasReplacementPatterns() {
		t.Errorf("an empty replace string is static: %+v", got)
	}
}

// exec is the groups of the first match of an ECMAScript regular expression in a text, as
// RegExp.exec gives them (an unmatched group is "").
func exec(t *testing.T, target, pattern string) []string {
	t.Helper()
	re, err := regex.Compile(pattern, regex.Options{Syntax: regex.ECMAScript})
	if err != nil {
		t.Fatal(err)
	}
	rt := newRuneText(target)
	m, ok, err := re.Find(rt.runes, 0)
	if err != nil || !ok {
		t.Fatalf("%q does not match %q (%v)", pattern, target, err)
	}
	return rt.groups(m)
}

func TestReplaceStringsVSCode(t *testing.T) {
	cases := []struct{ name, target, search, replace, want string }{
		// case modifiers
		{"upper", "func privateFunc(", `func (\w+)\(`, `func \U$1(`, "func PRIVATEFUNC("},
		{"upper first", "func privateFunc(", `func (\w+)\(`, `func \u$1(`, "func PrivateFunc("},
		{"lower", "func privateFunc(", `func (\w+)\(`, `func \L$1(`, "func privatefunc("},
		{"lower first", "func PrivateFunc(", `func (\w+)\(`, `func \l$1(`, "func privateFunc("},
		{"several", "hellogooDbye", `hello(\w+)`, `hello\u\u\l\l\U$1`, "helloGOodBYE"},

		// JavaScript semantics
		{"js: static", "hi", `hi`, `hello`, "hello"},
		{"js: tab", "hi", `hi`, `\t`, "\t"},
		{"js: lf", "hi", `hi`, `\n`, "\n"},
		{"js: backslash t", "hi", `hi`, `\\t`, `\t`},
		{"js: backslash n", "hi", `hi`, `\\n`, `\n`},
		{"js: $&", "hi", `hi`, `hello$&`, "hellohi"},
		{"js: $0", "hi", `hi`, `hello$0`, "hellohi"},
		{"js: $&1", "hi", `hi`, `hello$&1`, "hellohi1"},
		{"js: $01", "hi", `hi`, `hello$01`, "hellohi1"},
		// $nn is a group only when the expression has it
		{"js: $10 with one group", "hi", `(hi)`, `hello$10`, "hellohi0"},
		{"js: $10 with ten groups", "hi", `(hi)()()()()()()()()()`, `hello$10`, "hello"},
		{"js: $100", "hi", `(hi)`, `hello$100`, "hellohi00"},
		{"js: $20", "hi", `(hi)`, `hello$20`, "hello$20"},

		// a complete match
		{"complete: static", "bla", `bla`, `hello`, "hello"},
		{"complete: group", "bla", `(bla)`, `hello`, "hello"},
		{"complete: $0", "bla", `(bla)`, `hello$0`, "hellobla"},
		{"complete: require 1", "let fs = require('fs')", `let\s+(\w+)\s*=\s*require\s*\(\s*['"]([\w\.\-/]+)\s*['"]\s*\)\s*`, `import * as $1 from '$2';`, "import * as fs from 'fs';"},
		{"complete: require 2", "let something = require('fs')", `let\s+(\w+)\s*=\s*require\s*\(\s*['"]([\w\.\-/]+)\s*['"]\s*\)\s*`, `import * as $1 from '$2';`, "import * as something from 'fs';"},
		{"complete: require 3", "let something = require('fs')", `let\s+(\w+)\s*=\s*require\s*\(\s*['"]([\w\.\-/]+)\s*['"]\s*\)\s*`, `import * as $1 from '$1';`, "import * as something from 'something';"},
		{"complete: require 4", "let something = require('fs')", `let\s+(\w+)\s*=\s*require\s*\(\s*['"]([\w\.\-/]+)\s*['"]\s*\)\s*`, `import * as $2 from '$1';`, "import * as fs from 'something';"},
		{"complete: require 5", "let something = require('fs')", `let\s+(\w+)\s*=\s*require\s*\(\s*['"]([\w\.\-/]+)\s*['"]\s*\)\s*`, `import * as $0 from '$0';`, "import * as let something = require('fs') from 'let something = require('fs')';"},
		{"complete: for", "for ()", `for(.*)`, `cat$1`, "cat ()"},
		// issue #18111
		{"complete: #18111", "HRESULT OnAmbientPropertyChange(DISPID   dispid);", `\b\s{3}\b`, ` `, " "},

		// a match that is a part of the text
		{"part: static", "this is a bla text", `bla`, `hello`, "hello"},
		{"part: lookahead", "this is a bla text", `this(?=.*bla)`, `that`, "that"},
		{"part: $1at", "this is a bla text", `(th)is(?=.*bla)`, `$1at`, "that"},
		{"part: $1e", "this is a bla text", `(th)is(?=.*bla)`, `$1e`, "the"},
		{"part: $1ere", "this is a bla text", `(th)is(?=.*bla)`, `$1ere`, "there"},
		{"part: $1", "this is a bla text", `(th)is(?=.*bla)`, `$1`, "th"},
		{"part: ma$1", "this is a bla text", `(th)is(?=.*bla)`, `ma$1`, "math"},
		{"part: ma$1s", "this is a bla text", `(th)is(?=.*bla)`, `ma$1s`, "maths"},
		{"part: $0", "this is a bla text", `(th)is(?=.*bla)`, `$0`, "this"},
		{"part: $0$1", "this is a bla text", `(th)is(?=.*bla)`, `$0$1`, "thisth"},
		{"part: end lookahead", "this is a bla text", `bla(?=\stext$)`, `foo`, "foo"},
		{"part: f$1", "this is a bla text", `b(la)(?=\stext$)`, `f$1`, "fla"},
		{"part: f$0", "this is a bla text", `b(la)(?=\stext$)`, `f$0`, "fbla"},
		{"part: $0ah", "this is a bla text", `b(la)(?=\stext$)`, `$0ah`, "blaah"},

		// issue #19740: a group that did not match is empty, not undefined
		{"#19740", "abcd", `a(z)?`, `a{$1}`, "a{}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseReplaceString(tc.replace).Build(exec(t, tc.target, tc.search), false)
			if got != tc.want {
				t.Errorf("%q.replace(/%s/, %q) = %q, want %q", tc.target, tc.search, tc.replace, got, tc.want)
			}
		})
	}
}

func TestBuildReplaceStringWithCasePreservedVSCode(t *testing.T) {
	cases := []struct {
		groups  []string
		replace string
		want    string
	}{
		{[]string{"abc"}, "Def", "def"},
		{[]string{"Abc"}, "Def", "Def"},
		{[]string{"ABC"}, "Def", "DEF"},
		{[]string{"abc", "Abc"}, "Def", "def"},
		{[]string{"Abc", "abc"}, "Def", "Def"},
		{[]string{"ABC", "abc"}, "Def", "DEF"},
		{[]string{"aBc", "abc"}, "Def", "def"},
		{[]string{"AbC"}, "Def", "Def"},
		{[]string{"aBC"}, "Def", "def"},
		{[]string{"aBc"}, "DeF", "deF"},
		{[]string{"Foo-Bar"}, "newfoo-newbar", "Newfoo-Newbar"},
		{[]string{"Foo-Bar-Abc"}, "newfoo-newbar-newabc", "Newfoo-Newbar-Newabc"},
		{[]string{"Foo-Bar-abc"}, "newfoo-newbar", "Newfoo-newbar"},
		{[]string{"foo-Bar"}, "newfoo-newbar", "newfoo-Newbar"},
		{[]string{"foo-BAR"}, "newfoo-newbar", "newfoo-NEWBAR"},
		{[]string{"foO-BAR"}, "NewFoo-NewBar", "newFoo-NEWBAR"},
		{[]string{"Foo_Bar"}, "newfoo_newbar", "Newfoo_Newbar"},
		{[]string{"Foo_Bar_Abc"}, "newfoo_newbar_newabc", "Newfoo_Newbar_Newabc"},
		{[]string{"Foo_Bar_abc"}, "newfoo_newbar", "Newfoo_newbar"},
		{[]string{"Foo_Bar-abc"}, "newfoo_newbar-abc", "Newfoo_newbar-abc"},
		{[]string{"foo_Bar"}, "newfoo_newbar", "newfoo_Newbar"},
		{[]string{"Foo_BAR"}, "newfoo_newbar", "Newfoo_NEWBAR"},
		// the last case of the "preserve case" test, which differs from the one above in its input
		{[]string{"foo_BAR"}, "newfoo_newbar", "newfoo_NEWBAR"},
	}
	for _, tc := range cases {
		t.Run(tc.groups[0]+"→"+tc.replace, func(t *testing.T) {
			if got := buildReplaceStringWithCasePreserved(tc.groups, tc.replace); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			// "preserve case": the same through a parsed replace string
			if got := ParseReplaceString(tc.replace).Build(tc.groups, true); got != tc.want {
				t.Errorf("parsed: got %q, want %q", got, tc.want)
			}
		})
	}
	if got := StaticReplacePattern("x").Build(nil, true); got != "x" {
		t.Errorf("with no groups the replacement is as it is: %q", got)
	}
}
