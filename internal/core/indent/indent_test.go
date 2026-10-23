package indent_test

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/indent/indenttest"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/textmate"
)

func rng(l1, c1, l2, c2 int) buffer.Range {
	return buffer.Range{Start: buffer.Position{Line: l1 - 1, Col: c1 - 1}, End: buffer.Position{Line: l2 - 1, Col: c2 - 1}}
}

func compile(t *testing.T, pattern string, opts regex.Options) *regex.Regexp {
	t.Helper()
	opts.Syntax = regex.ECMAScript
	re, err := regex.Compile(pattern, opts)
	if err != nil {
		t.Fatal(err)
	}
	return re
}

// Ported from VS Code: reference/vscode/src/vs/editor/test/common/modes/supports/onEnter.test.ts
// ('uses brackets').
func TestOnEnterUsesBrackets(t *testing.T) {
	support := indent.NewOnEnterSupport(&language.Configuration{Brackets: []language.Pair{{Open: "(", Close: ")"}, {Open: "begin", Close: "end"}}})
	tests := []struct {
		before, after string
		want          language.IndentAction
		found         bool
	}{
		{"a", "", 0, false},
		{"", "b", 0, false},
		{"(", "b", language.IndentIndent, true},
		{"a", ")", 0, false},
		{"begin", "ending", language.IndentIndent, true},
		{"abegin", "end", 0, false},
		{"begin", ")", language.IndentIndent, true},
		{"begin", "end", language.IndentOutdent, true},
		{"begin ", " end", language.IndentOutdent, true},
		{" begin", "end//as", language.IndentOutdent, true},
		{"(", ")", language.IndentOutdent, true},
		{"( ", ")", language.IndentOutdent, true},
		{"a(", ")b", language.IndentOutdent, true},
		{"(", "", language.IndentIndent, true},
		{"(", "foo", language.IndentIndent, true},
		{"begin", "foo", language.IndentIndent, true},
		{"begin", "", language.IndentIndent, true},
	}
	for _, tt := range tests {
		got, found := support.OnEnter(indent.AutoIndentAdvanced, "", tt.before, tt.after)
		if found != tt.found || (found && got.Indent != tt.want) {
			t.Errorf("OnEnter(%q, %q) = %v, %v; want %v, %v", tt.before, tt.after, got.Indent, found, tt.want, tt.found)
		}
	}
}

// Ported from VS Code: onEnter.test.ts ('Issue #121125: onEnterRules with global modifier').
func TestOnEnterRuleWithGlobalFlag(t *testing.T) {
	support := indent.NewOnEnterSupport(&language.Configuration{OnEnterRules: []language.OnEnterRule{{
		Action:     language.EnterAction{AppendText: "/// ", Indent: language.IndentOutdentOne},
		BeforeText: compile(t, `^\s*\/{3}.*$`, regex.Options{Multiline: true}),
	}}})
	for i := 0; i < 2; i++ {
		got, found := support.OnEnter(indent.AutoIndentAdvanced, "/// line", "/// line", "")
		if !found || got.Indent != language.IndentOutdentOne || got.AppendText != "/// " {
			t.Errorf("attempt %d: OnEnter = %+v, %v", i, got, found)
		}
	}
}

// Ported from VS Code: onEnter.test.ts ('uses regExpRules', 'issue #141816').
func TestOnEnterRegExpRules(t *testing.T) {
	ts := indenttest.All()[indenttest.TypeScript]
	type want struct {
		indent language.IndentAction
		append string
		remove int
	}
	N := (*want)(nil)
	w := func(a language.IndentAction, text string, remove int) *want { return &want{a, text, remove} }
	tests := []struct {
		previous, before, after string
		want                    *want
	}{
		{"", "\t/**", " */", w(language.IndentOutdent, " * ", 0)},
		{"", "\t/**", "", w(language.IndentNone, " * ", 0)},
		{"", "\t/** * / * / * /", "", w(language.IndentNone, " * ", 0)},
		{"", "\t/** /*", "", w(language.IndentNone, " * ", 0)},
		{"", "/**", "", w(language.IndentNone, " * ", 0)},
		{"", "\t/**/", "", N},
		{"", "\t/***/", "", N},
		{"", "\t/*******/", "", N},
		{"", "\t/** * * * * */", "", N},
		{"", "\t/** */", "", N},
		{"", "\t/** asdfg */", "", N},
		{"", "\t/* asdfg */", "", N},
		{"", "\t/* asdfg */", "", N},
		{"", "\t/** asdfg */", "", N},
		{"", "*/", "", N},
		{"", "\t/*", "", N},
		{"", "\t*", "", N},
		{"\t/**", "\t *", "", w(language.IndentNone, "* ", 0)},
		{"\t * something", "\t *", "", w(language.IndentNone, "* ", 0)},
		{"\t *", "\t *", "", w(language.IndentNone, "* ", 0)},
		{"", "\t */", "", w(language.IndentNone, "", 1)},
		{"", "\t * */", "", w(language.IndentNone, "", 1)},
		{"", "\t * * / * / * / */", "", N},
		{"\t/**", "\t * ", "", w(language.IndentNone, "* ", 0)},
		{"\t * something", "\t * ", "", w(language.IndentNone, "* ", 0)},
		{"\t *", "\t * ", "", w(language.IndentNone, "* ", 0)},
		{"/**", " * ", "", w(language.IndentNone, "* ", 0)},
		{" * something", " * ", "", w(language.IndentNone, "* ", 0)},
		{" *", " * asdfsfagadfg", "", w(language.IndentNone, "* ", 0)},
		{"/**", " * asdfsfagadfg * * * ", "", w(language.IndentNone, "* ", 0)},
		{" * something", " * asdfsfagadfg * * * ", "", w(language.IndentNone, "* ", 0)},
		{" *", " * asdfsfagadfg * * * ", "", w(language.IndentNone, "* ", 0)},
		{"/**", " * /*", "", w(language.IndentNone, "* ", 0)},
		{" * something", " * /*", "", w(language.IndentNone, "* ", 0)},
		{" *", " * /*", "", w(language.IndentNone, "* ", 0)},
		{"/**", " * asdfsfagadfg * / * / * /", "", w(language.IndentNone, "* ", 0)},
		{" * something", " * asdfsfagadfg * / * / * /", "", w(language.IndentNone, "* ", 0)},
		{" *", " * asdfsfagadfg * / * / * /", "", w(language.IndentNone, "* ", 0)},
		{"/**", " * asdfsfagadfg * / * / * /*", "", w(language.IndentNone, "* ", 0)},
		{" * something", " * asdfsfagadfg * / * / * /*", "", w(language.IndentNone, "* ", 0)},
		{" *", " * asdfsfagadfg * / * / * /*", "", w(language.IndentNone, "* ", 0)},
		{"", " */", "", w(language.IndentNone, "", 1)},
		{" */", " * test() {", "", w(language.IndentIndent, "", 0)},
		{"", "\t */", "", w(language.IndentNone, "", 1)},
		{"", "\t\t */", "", w(language.IndentNone, "", 1)},
		{"", "   */", "", w(language.IndentNone, "", 1)},
		{"", "     */", "", w(language.IndentNone, "", 1)},
		{"", "\t     */", "", w(language.IndentNone, "", 1)},
		{"", " *--------------------------------------------------------------------------------------------*/", "", w(language.IndentNone, "", 1)},
		{"class A {", "    * test() {", "", w(language.IndentIndent, "", 0)},
		{"", "    * test() {", "", w(language.IndentIndent, "", 0)},
		{"    ", "    * test() {", "", w(language.IndentIndent, "", 0)},
		{"class A {", "  * test() {", "", w(language.IndentIndent, "", 0)},
		{"", "  * test() {", "", w(language.IndentIndent, "", 0)},
		{"  ", "  * test() {", "", w(language.IndentIndent, "", 0)},
		// issue #141816: brackets in a regular expression
		{"", "const r = /{/;", "", N},
		{"", "const r = /{[0-9]/;", "", N},
		{"", "const r = /[a-zA-Z]{/;", "", N},
	}
	for _, tt := range tests {
		got, found := ts.OnEnter.OnEnter(indent.AutoIndentAdvanced, tt.previous, tt.before, tt.after)
		switch {
		case tt.want == nil:
			if found {
				t.Errorf("OnEnter(%q, %q, %q) = %+v, want none", tt.previous, tt.before, tt.after, got)
			}
		case !found:
			t.Errorf("OnEnter(%q, %q, %q) found nothing", tt.previous, tt.before, tt.after)
		default:
			if got.Indent != tt.want.indent || got.AppendText != tt.want.append || got.RemoveText != tt.want.remove {
				t.Errorf("OnEnter(%q, %q, %q) = %+v, want %+v", tt.previous, tt.before, tt.after, got, *tt.want)
			}
		}
	}
}

func tsText(lines []string, toks map[int][]indenttest.Tok) *indenttest.Text {
	return indenttest.NewText(indenttest.TypeScript, lines, toks)
}

// Ported from VS Code: reference/vscode/src/vs/editor/contrib/indentation/test/browser/indentationLineProcessor.test.ts
// ('Indentation Context Processor').
func TestProcessedContext(t *testing.T) {
	ix := indent.New(indenttest.All())
	other, str, comment, re := textmate.TokenOther, textmate.TokenString, textmate.TokenComment, textmate.TokenRegEx
	tests := []struct {
		name                      string
		lines                     []string
		toks                      map[int][]indenttest.Tok
		r                         buffer.Range
		before, after, previousLn string
	}{
		{
			name:  "brackets inside of string",
			lines: []string{`const someVar = "{some text}"`},
			toks:  map[int][]indenttest.Tok{0: {{Start: 0, Type: other}, {Start: 16, Type: str}, {Start: 28, Type: str}}},
			r:     rng(1, 23, 1, 23), before: `const someVar = "some`, after: ` text"`,
		},
		{
			name:  "brackets inside of comment",
			lines: []string{`const someVar2 = /*(a])*/`, `const someVar = /* [()] some other t{e}xt() */ "some text"`},
			toks: map[int][]indenttest.Tok{
				0: {{Start: 0, Type: other}, {Start: 17, Type: comment}},
				1: {{Start: 0, Type: other}, {Start: 16, Type: comment}, {Start: 46, Type: other}, {Start: 47, Type: str}},
			},
			r: rng(2, 29, 2, 35), before: `const someVar = /*  some`, after: ` text */ "some text"`, previousLn: `const someVar2 = /*a*/`,
		},
		{
			name:  "brackets inside of regex",
			lines: []string{`const someRegex2 = /(()))]/;`, `const someRegex = /()a{h}{s}[(a}87(9a9()))]/;`},
			toks: map[int][]indenttest.Tok{
				0: {{Start: 0, Type: other}, {Start: 19, Type: re}, {Start: 27, Type: other}},
				1: {{Start: 0, Type: other}, {Start: 18, Type: re}, {Start: 44, Type: other}},
			},
			r: rng(1, 25, 2, 33), before: `const someRegex2 = /`, after: `879a9/;`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, after, prev := indent.ProcessedContext(ix, tsText(tt.lines, tt.toks), tt.r)
			if before != tt.before || after != tt.after || prev != tt.previousLn {
				t.Errorf("context = %q, %q, %q; want %q, %q, %q", before, after, prev, tt.before, tt.after, tt.previousLn)
			}
		})
	}
}

// Ported from VS Code: indentationLineProcessor.test.ts ('Processed Indent Rules Support').
func TestProcessedIndentRules(t *testing.T) {
	ix := indent.New(indenttest.All())
	other, str, comment, re := textmate.TokenOther, textmate.TokenString, textmate.TokenComment, textmate.TokenRegEx
	tests := []struct {
		name  string
		lines []string
		toks  map[int][]indenttest.Tok
		check func(inc, dec, next, ignore bool) bool
		want  [3]bool
		which string
	}{
		{"should increase", []string{`const someVar = {`, `const someVar2 = "{"`, `const someVar3 = /*{*/`}, map[int][]indenttest.Tok{
			0: {{Start: 0, Type: other}},
			1: {{Start: 0, Type: other}, {Start: 17, Type: str}},
			2: {{Start: 0, Type: other}, {Start: 17, Type: comment}},
		}, func(inc, _, _, _ bool) bool { return inc }, [3]bool{true, false, false}, "increase"},
		{"should decrease", []string{`}`, `"])some text}"`, `])*/`}, map[int][]indenttest.Tok{
			0: {{Start: 0, Type: other}}, 1: {{Start: 0, Type: str}}, 2: {{Start: 0, Type: comment}},
		}, func(_, dec, _, _ bool) bool { return dec }, [3]bool{true, false, false}, "decrease"},
		{"should increase next line", []string{`if()`, `const someString = "if()"`, `const someRegex = /if()/`}, map[int][]indenttest.Tok{
			0: {{Start: 0, Type: other}},
			1: {{Start: 0, Type: other}, {Start: 19, Type: str}},
			2: {{Start: 0, Type: other}, {Start: 18, Type: re}},
		}, func(_, _, next, _ bool) bool { return next }, [3]bool{true, false, false}, "indent next line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tsText(tt.lines, tt.toks)
			for line := 0; line < 3; line++ {
				inc, dec, next, ignore := indent.ProcessedShould(ix, m, line)
				if got := tt.check(inc, dec, next, ignore); got != tt.want[line] {
					t.Errorf("line %d: should %s = %v, want %v", line+1, tt.which, got, tt.want[line])
				}
			}
		})
	}
}

func TestParseAutoIndent(t *testing.T) {
	for _, name := range []string{"none", "keep", "brackets", "advanced", "full"} {
		got, ok := indent.ParseAutoIndent(name)
		if !ok || got.String() != name {
			t.Errorf("ParseAutoIndent(%q) = %v, %v", name, got, ok)
		}
	}
	if _, ok := indent.ParseAutoIndent("bogus"); ok {
		t.Error("ParseAutoIndent accepted a value that is not a level")
	}
	if indent.AutoIndentNone >= indent.AutoIndentKeep || indent.AutoIndentAdvanced >= indent.AutoIndentFull {
		t.Error("the levels are not in order")
	}
}

func TestRulesMetadata(t *testing.T) {
	r := indenttest.All()[indenttest.TypeScript].Rules
	tests := []struct {
		text string
		want int
	}{
		{"function f() {", indent.IncreaseMask},
		{"}", indent.DecreaseMask},
		{"if (x)", indent.IndentNextLineMask},
		{" * comment", indent.UnindentMask},
		{"let a = 1", 0},
	}
	for _, tt := range tests {
		if got := r.Metadata(tt.text); got != tt.want {
			t.Errorf("Metadata(%q) = %b, want %b", tt.text, got, tt.want)
		}
	}
	var none *indent.Rules
	if none.Metadata("{") != 0 || none.ShouldIncrease("{") {
		t.Error("no rules should match nothing")
	}
}
