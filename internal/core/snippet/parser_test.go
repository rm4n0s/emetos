package snippet

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// Ported from VS Code: src/vs/editor/contrib/snippet/test/browser/snippetParser.test.ts
// (all the tests; the expected positions are bytes, and the ones of these tests are ASCII)

// kind names the type of a marker, as the tests of VS Code name it with instanceof.
func kind(m Marker) string {
	switch m.(type) {
	case *Text:
		return "Text"
	case *Placeholder:
		return "Placeholder"
	case *Variable:
		return "Variable"
	case *Choice:
		return "Choice"
	case *Transform:
		return "Transform"
	case *FormatString:
		return "FormatString"
	case *TextmateSnippet:
		return "Snippet"
	}
	return "?"
}

func kinds(ms []Marker) string {
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = kind(m)
	}
	return strings.Join(names, " ")
}

func ptr(s string) *string { return &s }

func parse(value string) *TextmateSnippet { return NewParser().Parse(value) }

func TestScanner(t *testing.T) {
	cases := []struct {
		in   string
		want []TokenType
	}{
		{"", []TokenType{EOF}},
		{"abc", []TokenType{VariableName, EOF}},
		{"{{abc}}", []TokenType{CurlyOpen, CurlyOpen, VariableName, CurlyClose, CurlyClose, EOF}},
		{"abc() ", []TokenType{VariableName, Format, EOF}},
		{"abc 123", []TokenType{VariableName, Format, Int, EOF}},
		{"$foo", []TokenType{Dollar, VariableName, EOF}},
		{"$foo_bar", []TokenType{Dollar, VariableName, EOF}},
		{"$foo-bar", []TokenType{Dollar, VariableName, Dash, VariableName, EOF}},
		{"${foo}", []TokenType{Dollar, CurlyOpen, VariableName, CurlyClose, EOF}},
		{"${1223:foo}", []TokenType{Dollar, CurlyOpen, Int, Colon, VariableName, CurlyClose, EOF}},
		{`\${}`, []TokenType{Backslash, Dollar, CurlyOpen, CurlyClose, EOF}},
		{"${foo/regex/format/option}", []TokenType{Dollar, CurlyOpen, VariableName, Forwardslash, VariableName, Forwardslash, VariableName, Forwardslash, VariableName, CurlyClose, EOF}},
		// not in VS Code's test: the other tokens, and multi-byte text in a Format token
		{"a+b?c|d,e", []TokenType{VariableName, Plus, VariableName, QuestionMark, VariableName, Pipe, VariableName, Comma, VariableName, EOF}},
		{"héllo wörld", []TokenType{VariableName, Format, VariableName, Format, VariableName, Format, VariableName, EOF}},
		{"日本語", []TokenType{Format, EOF}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			var s Scanner
			s.Text(tc.in)
			var got []TokenType
			for {
				tok := s.Next()
				got = append(got, tok.Type)
				if tok.Type == EOF {
					break
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("tokens %v, want %v", got, tc.want)
			}
		})
	}
	// token positions and text, with bytes
	var s Scanner
	s.Text("é$12")
	for _, want := range []struct {
		typ      TokenType
		pos, len int
		text     string
	}{{Format, 0, 2, "é"}, {Dollar, 2, 1, "$"}, {Int, 3, 2, "12"}, {EOF, 5, 0, ""}} {
		tok := s.Next()
		if tok.Type != want.typ || tok.Pos != want.pos || tok.Len != want.len || (tok.Type != EOF && s.TokenText(tok) != want.text) {
			t.Errorf("token %+v, want %+v", tok, want)
		}
	}
}

func TestParserEscaped(t *testing.T) {
	cases := []struct{ in, want string }{
		{`foo$0`, `foo\$0`},
		{`foo\$0`, `foo\\\$0`},
		{`f$1oo$0`, `f\$1oo\$0`},
		{`${1:foo}$0`, `\${1:foo\}\$0`},
		{`$`, `\$`},
	}
	for _, tc := range cases {
		if got := Escape(tc.in); got != tc.want {
			t.Errorf("Escape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParserText(t *testing.T) {
	cases := []struct{ in, want string }{
		{`$`, `$`},
		{`\\$`, `\$`},
		{`{`, `{`},
		{`\}`, `}`},
		{`\abc`, `\abc`},
		{`foo${f:\}}bar`, `foo}bar`},
		{`\{`, `\{`},
		{`I need \\\$`, `I need \$`},
		{`\`, `\`},
		{`\{{`, `\{{`},
		{`{{`, `{{`},
		{`{{dd`, `{{dd`},
		{`}}`, `}}`},
		{`ff}}`, `ff}}`},
		{`farboo`, `farboo`},
		{`far{{}}boo`, `far{{}}boo`},
		{`far{{123}}boo`, `far{{123}}boo`},
		{`far\{{123}}boo`, `far\{{123}}boo`},
		{`far{{id:bern}}boo`, `far{{id:bern}}boo`},
		{`far{{id:bern {{basel}}}}boo`, `far{{id:bern {{basel}}}}boo`},
		{`far{{id:bern {{id:basel}}}}boo`, `far{{id:bern {{id:basel}}}}boo`},
		{`far{{id:bern {{id2:basel}}}}boo`, `far{{id:bern {{id2:basel}}}}boo`},
	}
	for _, tc := range cases {
		if got := AsInsertText(tc.in); got != tc.want {
			t.Errorf("AsInsertText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// textAndMarkers are the cases of assertTextAndMarker: the insert text and the kinds of the
// markers of the top level.
var textAndMarkers = []struct{ in, text, kinds string }{
	// TM text
	{`foo${1:bar}}`, `foobar}`, "Text Placeholder Text"},
	{`foo${1:bar}${2:foo}}`, `foobarfoo}`, "Text Placeholder Placeholder Text"},
	{`foo${1:bar\}${2:foo}}`, `foobar}foo`, "Text Placeholder"},
	// placeholder
	{`farboo`, `farboo`, "Text"},
	{`far{{}}boo`, `far{{}}boo`, "Text"},
	{`far{{123}}boo`, `far{{123}}boo`, "Text"},
	{`far\{{123}}boo`, `far\{{123}}boo`, "Text"},
	// literal code
	{"far`123`boo", "far`123`boo", "Text"},
	{"far\\`123\\`boo", "far\\`123\\`boo", "Text"},
	// variables and tab stops
	{`$far-boo`, `-boo`, "Variable Text"},
	{`\$far-boo`, `$far-boo`, "Text"},
	{`far$farboo`, `far`, "Text Variable"},
	{`far${farboo}`, `far`, "Text Variable"},
	{`$123`, ``, "Placeholder"},
	{`$farboo`, ``, "Variable"},
	{`$far12boo`, ``, "Variable"},
	{`000_${far}_000`, `000__000`, "Text Variable Text"},
	{`FFF_${TM_SELECTED_TEXT}_FFF$0`, `FFF__FFF`, "Text Variable Text Placeholder"},
	// variables and placeholders with defaults
	{`${name:value}`, `value`, "Variable"},
	{`${1:value}`, `value`, "Placeholder"},
	{`${1:bar${2:foo}bar}`, `barfoobar`, "Placeholder"},
	{`${name:value`, `${name:value`, "Text"},
	{`${1:bar${2:foobar}`, `${1:barfoobar`, "Text Placeholder"},
	// variable transforms
	{`${foo///}`, ``, "Variable"},
	{`${foo/regex/format/gmi}`, ``, "Variable"},
	{`${foo/([A-Z][a-z])/format/}`, ``, "Variable"},
	{`${foo/([A-Z][a-z])/format/GMI}`, `${foo/([A-Z][a-z])/format/GMI}`, "Text"}, // invalid flags
	{`${foo/([A-Z][a-z])/format/funky}`, `${foo/([A-Z][a-z])/format/funky}`, "Text"},
	{`${foo/([A-Z][a-z]/format/}`, `${foo/([A-Z][a-z]/format/}`, "Text"}, // invalid regex
	{`${foo/m\/atch/$1/i}`, ``, "Variable"},
	{`${foo///`, `${foo///`, "Text"},
	{`${foo/regex/format/options`, `${foo/regex/format/options`, "Text"},
	// placeholder transforms
	{`${1///}`, ``, "Placeholder"},
	{`${1/regex/format/gmi}`, ``, "Placeholder"},
	{`${1/([A-Z][a-z])/format/}`, ``, "Placeholder"},
	{`${1/m\/atch/$1/i}`, ``, "Placeholder"},
	{`${1///`, `${1///`, "Text"},
	{`${1/regex/format/options`, `${1/regex/format/options`, "Text"},
	// choices
	{`${1|one,two,three|}`, `one`, "Placeholder"},
	{`${1|one|}`, `one`, "Placeholder"},
	{`${1|one1,two2|}`, `one1`, "Placeholder"},
	{`${1|one1\,two2|}`, `one1,two2`, "Placeholder"},
	{`${1|one1\|two2|}`, `one1|two2`, "Placeholder"},
	{`${1|one1\atwo2|}`, `one1\atwo2`, "Placeholder"},
	{`${1|one,two,three,|}`, `${1|one,two,three,|}`, "Text"},
	{`${1|one,`, `${1|one,`, "Text"},
	{`console.log(${1|not\, not, five, 5, 1   23|});`, `console.log(not, not);`, "Text Placeholder Text"}, // #31521
	// the empty placeholder
	{`${1:}`, ``, "Placeholder"},
	// #31599: no choice for the final tab stop
	{`${0|foo,bar|}`, `${0|foo,bar|}`, "Text"},
	{`${1|foo,bar|}`, `foo`, "Placeholder"},
	// #52931
	{`FOO${1:/bin/bash}`, `FOO/bin/bash`, "Text Placeholder"},
	// #180132
	{`${1|aaa$aaa|}bbb\$bbb`, `aaa$aaabbb$bbb`, "Placeholder Text"},
}

func TestParserTextAndMarkers(t *testing.T) {
	for _, tc := range textAndMarkers {
		t.Run(tc.in, func(t *testing.T) {
			s := parse(tc.in)
			if got := s.String(); got != tc.text {
				t.Errorf("text %q, want %q", got, tc.text)
			}
			if got := kinds(s.Children()); got != tc.kinds {
				t.Errorf("markers %q, want %q", got, tc.kinds)
			}
		})
	}
}

func TestParserMarkers(t *testing.T) {
	cases := []struct{ in, kinds string }{
		{`${foo/regex\/format/options}`, "Text"},
		{`${foo/.*/${0:fooo}/i}`, "Variable"},
		{`${foo/.*/${1}/i}`, "Variable"},
		{`${foo/.*/$1/i}`, "Variable"},
		{`${foo/.*/This-$1-encloses/i}`, "Variable"},
		{`${foo/.*/complex${1:else}/i}`, "Variable"},
		{`${foo/.*/complex${1:-else}/i}`, "Variable"},
		{`${foo/.*/complex${1:+if}/i}`, "Variable"},
		{`${foo/.*/complex${1:?if:else}/i}`, "Variable"},
		{`${foo/.*/complex${1:/upcase}/i}`, "Variable"},
		{`${1/regex\/format/options}`, "Text"},
		{`${TM_DIRECTORY/src\//$1/}`, "Variable"},         // #36715
		{`${TM_SELECTED_TEXT/a/\/$1/g}`, "Variable"},      // #37562
		{`${TM_SELECTED_TEXT/a/in\/$1ner/g}`, "Variable"}, // #37562
		{`${TM_SELECTED_TEXT/a/end\//g}`, "Variable"},     // #37562
		{`far{{}}boo`, "Text"},
		{`far{{123}}boo`, "Text"},
		{`far\{{123}}boo`, "Text"},
		{`far$0boo`, "Text Placeholder Text"},
		{`far${123}boo`, "Text Placeholder Text"},
		{`far\${123}boo`, "Text"},
		{`errorContext: ` + "`${1:err}`" + `, error: $1`, "Text Placeholder Text Placeholder"},
		{`errorContext: ` + "`${1:err}`" + `, error: ${1/err/ok/}`, "Text Placeholder Text Placeholder"},
		{`${TM_DIRECTORY/.*src[\/](.*)/$1/}`, "Variable"}, // #40570
		{"${1/(void$)|(.+)/${1:?-\treturn nil;}/}", "Placeholder"},
		{`namespace ${TM_DIRECTORY/[\/]/\\/g};`, "Text Variable Text"}, // #65412
	}
	for _, tc := range cases {
		if got := kinds(parse(tc.in).Children()); got != tc.kinds {
			t.Errorf("%q: markers %q, want %q", tc.in, got, tc.kinds)
		}
	}
}

func TestParserTMText(t *testing.T) {
	s := parse(`foo${1:bar\}${2:foo}}`)
	placeholder, ok := s.Children()[1].(*Placeholder)
	if !ok {
		t.Fatalf("%s", kinds(s.Children()))
	}
	children := placeholder.Children()
	if placeholder.Index != 1 || len(children) != 2 {
		t.Fatalf("index %d, %d children", placeholder.Index, len(children))
	}
	if _, ok := children[0].(*Text); !ok || children[0].String() != "bar}" {
		t.Errorf("first child %s %q", kind(children[0]), children[0].String())
	}
	if _, ok := children[1].(*Placeholder); !ok || children[1].String() != "foo" {
		t.Errorf("second child %s %q", kind(children[1]), children[1].String())
	}
}

func TestParserChoice(t *testing.T) {
	s := parse(`${1|one,two,three|}`)
	var walked []string
	s.Walk(func(m Marker) bool {
		walked = append(walked, kind(m))
		if c, ok := m.(*Choice); ok && len(c.Options) != 3 {
			t.Errorf("%d options", len(c.Options))
		}
		return true
	})
	if got := strings.Join(walked, " "); got != "Placeholder Choice" {
		t.Errorf("walked %q", got)
	}

	phs := s.Placeholders()
	if len(phs) != 1 || phs[0].Choice() == nil || len(phs[0].Children()) != 1 || len(phs[0].Children()[0].(*Choice).Options) != 3 {
		t.Fatalf("placeholders %v", phs)
	}
	for in, want := range map[string]string{
		`${1|one,two,three|}`:  `one`,
		`\${1|one,two,three|}`: `${1|one,two,three|}`,
		`${1\|one,two,three|}`: `${1\|one,two,three|}`,
		`${1||}`:               `${1||}`,
	} {
		if got := AsInsertText(in); got != want {
			t.Errorf("AsInsertText(%q) = %q, want %q", in, got, want)
		}
	}
	// #58494: the escapes in a choice
	if phs := parse(`${1|\,,},$,\|,\\|}`).Placeholders(); len(phs) != 1 || phs[0].Choice() == nil {
		t.Errorf("escapes in a choice: %v", phs)
	}
}

func TestMarkerToTextmateString(t *testing.T) {
	cases := []struct{ in, want string }{
		{`$1`, `$1`},
		{`\$1`, `\$1`},
		{`console.log(${1|not\, not, five, 5, 1   23|});`, `console.log(${1|not\, not, five, 5, 1   23|});`},
		{`console.log(${1|not\, not, \| five, 5, 1   23|});`, `console.log(${1|not\, not, \| five, 5, 1   23|});`},
		{`${1|cho\,ices,wi\|th,esc\\aping,chall\\\,enges|}`, `${1|cho\,ices,wi\|th,esc\\aping,chall\\\,enges|}`},
		{`this is text`, `this is text`},
		{`this ${1:is ${2:nested with $var}}`, `this ${1:is ${2:nested with ${var}}}`},
		{`this ${1:is ${2:nested with $var}}}`, `this ${1:is ${2:nested with ${var}}}\}`},
		// #55627
		{`const fileName = "${TM_FILENAME/(.*)\..+$/$1/}"`, `const fileName = "${TM_FILENAME/(.*)\..+$/${1}/}"`},
		// the other kinds of format strings and flags
		{`${1/a/${1:/upcase}-${2:+x}-${3:-y}-${4:?p:q}/ig}`, `${1/a/${1:/upcase}-${2:+x}-${3:-y}-${4:?p:q}/ig}`},
		{`${1/a/b/}`, `${1/a/b/}`},
		{`${v/a\/b/c/}`, `${v/a\/b/c/}`},
		{`${v///}`, `${v/(?:)//}`},
	}
	for _, tc := range cases {
		if got := parse(tc.in).ToTextmateString(); got != tc.want {
			t.Errorf("ToTextmateString(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
}

func TestMarkerToTextmateStringIdentity(t *testing.T) {
	var same func(t *testing.T, a, b Marker)
	same = func(t *testing.T, a, b Marker) {
		t.Helper()
		if kind(a) != kind(b) || len(a.Children()) != len(b.Children()) || a.String() != b.String() {
			t.Fatalf("%s %q (%d children) != %s %q (%d children)", kind(a), a.String(), len(a.Children()), kind(b), b.String(), len(b.Children()))
		}
		for i := range a.Children() {
			same(t, a.Children()[i], b.Children()[i])
		}
	}
	for _, in := range []string{
		`$1`, `\$1`,
		`console.log(${1|not\, not, five, 5, 1   23|});`,
		`console.log(${1|not\, not, \| five, 5, 1   23|});`,
		`this is text`,
		`this ${1:is ${2:nested with $var}}`,
		`this ${1:is ${2:nested with $var}}}`,
		`this ${1:is ${2:nested with $var}} and repeating $1`,
	} {
		first := parse(in)
		same(t, first, parse(first.ToTextmateString()))
	}
}

func TestParserRealWorld(t *testing.T) {
	marker := parse(`console.warn(${1: $TM_SELECTED_TEXT })`).Children()
	if marker[0].String() != "console.warn(" || kind(marker[1]) != "Placeholder" || marker[2].String() != ")" {
		t.Fatalf("%s", kinds(marker))
	}
	placeholder := marker[1].(*Placeholder)
	if placeholder.Index != 1 || kinds(placeholder.Children()) != "Text Variable Text" {
		t.Fatalf("index %d, %s", placeholder.Index, kinds(placeholder.Children()))
	}
	for i, want := range []string{" ", "", " "} {
		if got := placeholder.Children()[i].String(); got != want {
			t.Errorf("child %d is %q, want %q", i, got, want)
		}
	}
	v := placeholder.Children()[1].(*Variable)
	if v.Name != "TM_SELECTED_TEXT" || len(v.Children()) != 0 {
		t.Errorf("variable %q with %d children", v.Name, len(v.Children()))
	}
	marker = parse(`$TM_SELECTED_TEXT`).Children()
	if len(marker) != 1 || kind(marker[0]) != "Variable" {
		t.Errorf("%s", kinds(marker))
	}
}

func TestParserTransformExample(t *testing.T) {
	children := parse("${1:name} : ${2:type}${3/\\s:=(.*)/${1:+ :=}${1}/};\n$0").Children()
	if got := kinds(children); got != "Placeholder Text Placeholder Placeholder Text Placeholder" {
		t.Fatalf("markers %q", got)
	}
	if len(children[0].Children()) != 1 || children[0].Children()[0].String() != "name" || children[0].(*Placeholder).Transform != nil {
		t.Error("${1:name}")
	}
	if children[1].String() != " : " {
		t.Errorf("text %q", children[1].String())
	}
	if len(children[2].Children()) != 1 || children[2].Children()[0].String() != "type" {
		t.Error("${2:type}")
	}
	p3 := children[3].(*Placeholder)
	if len(p3.Children()) != 0 || p3.Transform == nil {
		t.Fatal("${3/...}")
	}
	tr := p3.Transform
	if tr.Pattern != `\s:=(.*)` || len(tr.Children()) != 2 {
		t.Fatalf("pattern %q, %d children", tr.Pattern, len(tr.Children()))
	}
	f0, ok0 := tr.Children()[0].(*FormatString)
	f1, ok1 := tr.Children()[1].(*FormatString)
	if !ok0 || !ok1 || f0.Index != 1 || f0.IfValue == nil || *f0.IfValue != " :=" || f1.Index != 1 {
		t.Errorf("format strings %+v %+v", tr.Children()[0], tr.Children()[1])
	}
	if children[4].String() != ";\n" {
		t.Errorf("text %q", children[4].String())
	}
}

func TestParserDefaultPlaceholderValues(t *testing.T) {
	for _, tc := range []struct {
		in        string
		transform bool
	}{
		{"errorContext: `${1:err}`, error:$1", false},
		{"errorContext: `${1:err}`, error:${1/err/ok/}", true},
	} {
		c := parse(tc.in).Children()
		if kinds(c) != "Text Placeholder Text Placeholder" {
			t.Fatalf("%q: %s", tc.in, kinds(c))
		}
		p1, p2 := c[1].(*Placeholder), c[3].(*Placeholder)
		if p1.Index != 1 || len(p1.Children()) != 1 || p1.Children()[0].String() != "err" || p1.Transform != nil {
			t.Errorf("%q: first placeholder", tc.in)
		}
		if p2.Index != 1 || len(p2.Children()) != 1 || p2.Children()[0].String() != "err" || (p2.Transform != nil) != tc.transform {
			t.Errorf("%q: second placeholder (transform %v)", tc.in, p2.Transform != nil)
		}
	}
}

func TestRepeatedPlaceholderInherits(t *testing.T) { // #31040
	for _, in := range []string{`${1:foo}-abc-$1`, `${1:foo}-abc-${1}`, `${1:foo}-abc-${1:bar}`, `${1}-abc-${1:foo}`} {
		if got := AsInsertText(in); got != "foo-abc-foo" {
			t.Errorf("AsInsertText(%q) = %q", in, got)
		}
	}
}

func TestParserOddCases(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"backslash escape in TM only, #16212", `Foo \\${abc}bar`, `Foo \bar`},
		{"colon in a variable, #16717", `${TM_SELECTED_TEXT:foo:bar}`, `foo:bar`},
		{"colon in a placeholder, #16717", `${1:foo:bar}`, `foo:bar`},
		{"placeholder empty right after expansion, #152553", `${1:prog}: ${2:$1.cc} - $2`, `prog: prog.cc - prog.cc`},
		{"nested, #152553", `${1:prog}: ${3:${2:$1.cc}.33} - $2 $3`, `prog: prog.cc.33 - prog.cc prog.cc.33`},
		{"cyclic references of placeholders, #152553", `${1:$2.one} <> ${2:$1.two}`, `.two.one.two.one <> .one.two.one.two`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AsInsertText(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMarkerLen(t *testing.T) {
	final := ParseOpts{InsertFinalTabstop: true}
	cases := []struct {
		in   string
		opts ParseOpts
		want []int
	}{
		{`text$0`, final, []int{4, 0}},
		{`$1text$0`, final, []int{0, 4, 0}},
		{`te$1xt$0`, final, []int{2, 0, 2, 0}},
		{"errorContext: `${1:err}`, error: $0", final, []int{15, 0, 3, 10, 0}},
		{"errorContext: `${1:err}`, error: $1$0", final, []int{15, 0, 3, 10, 0, 3, 0}},
		{`$TM_SELECTED_TEXT$0`, final, []int{0, 0}},
		{`${TM_SELECTED_TEXT:def}$0`, final, []int{0, 3, 0}},
		// bytes, not characters
		{`é$1日`, ParseOpts{}, []int{2, 0, 3}},
	}
	for _, tc := range cases {
		var got []int
		NewParser().Parse(tc.in, tc.opts).Walk(func(m Marker) bool {
			got = append(got, m.Len())
			return true
		})
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q: lengths %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParserParentNode(t *testing.T) {
	s := NewParser().Parse(`This ${1:is ${2:nested}}$0`, ParseOpts{InsertFinalTabstop: true})
	phs := s.Placeholders()
	if len(phs) != 3 || phs[0].Index != 1 || phs[1].Index != 2 {
		t.Fatalf("placeholders %v", phs)
	}
	if phs[1].Parent() != Marker(phs[0]) || phs[0].Parent() != Marker(s) {
		t.Error("parents")
	}

	s = NewParser().Parse(`${VAR:default${1:value}}$0`, ParseOpts{InsertFinalTabstop: true})
	phs = s.Placeholders()
	if len(phs) != 2 || phs[0].Index != 1 {
		t.Fatalf("placeholders %v", phs)
	}
	if _, ok := s.Children()[0].(*Variable); !ok || phs[0].Parent() != s.Children()[0] {
		t.Error("the parent of a placeholder in a variable is the variable")
	}
}

func TestEnclosingPlaceholders(t *testing.T) {
	s := NewParser().Parse(`This ${1:is ${2:nested}}$0`, ParseOpts{InsertFinalTabstop: true})
	first, second := s.Placeholders()[0], s.Placeholders()[1]
	if got := s.EnclosingPlaceholders(first); len(got) != 0 {
		t.Errorf("first: %v", got)
	}
	if got := s.EnclosingPlaceholders(second); len(got) != 1 || got[0] != first {
		t.Errorf("second: %v", got)
	}
}

func TestSnippetOffset(t *testing.T) {
	s := NewParser().Parse(`te$1xt`, ParseOpts{InsertFinalTabstop: true})
	for i, want := range []int{0, 2, 2} {
		if got := s.Offset(s.Children()[i]); got != want {
			t.Errorf("offset of child %d is %d, want %d", i, got, want)
		}
	}
	s = NewParser().Parse(`${TM_SELECTED_TEXT:def}`, ParseOpts{InsertFinalTabstop: true})
	if got := s.Offset(s.Children()[0]); got != 0 {
		t.Errorf("variable: %d", got)
	}
	if got := s.Offset(s.Children()[0].Children()[0]); got != 0 {
		t.Errorf("its default: %d", got)
	}
	if got := s.Offset(NewText("foo")); got != -1 {
		t.Errorf("a foreign marker: %d", got)
	}
	if got := s.FullLen(s.Children()[0]); got != 3 {
		t.Errorf("full length: %d", got)
	}
}

func TestSnippetPlaceholders(t *testing.T) {
	for in, want := range map[string]int{`te$1xt$0`: 2, `te$1xt$1$0`: 3, `te$1xt$2$0`: 3, `${1:bar${2:foo}bar}$0`: 3} {
		if got := len(NewParser().Parse(in, ParseOpts{InsertFinalTabstop: true}).Placeholders()); got != want {
			t.Errorf("%q: %d placeholders, want %d", in, got, want)
		}
	}
}

func TestSnippetReplace(t *testing.T) {
	opts := ParseOpts{InsertFinalTabstop: true}
	t.Run("a nested snippet with placeholders", func(t *testing.T) {
		s := NewParser().Parse(`aaa${1:bbb${2:ccc}}$0`, opts)
		if len(s.Placeholders()) != 3 || s.Placeholders()[1].Index != 2 {
			t.Fatal("placeholders")
		}
		second := s.Placeholders()[1]
		enclosing := s.EnclosingPlaceholders(second)
		if len(enclosing) != 1 || enclosing[0].Index != 1 {
			t.Fatal("enclosing")
		}
		nested := NewParser().Parse(`ddd$1eee$0`, opts)
		s.Replace(second, nested.Children())
		if s.String() != "aaabbbdddeee" {
			t.Errorf("text %q", s.String())
		}
		var indexes []int
		for _, p := range s.Placeholders() {
			indexes = append(indexes, p.Index)
		}
		if !slices.Equal(indexes, []int{1, 1, 0, 0}) {
			t.Errorf("indexes %v", indexes)
		}
		again := s.EnclosingPlaceholders(s.Placeholders()[1])
		if len(again) != 1 || again[0] != s.Placeholders()[0] || again[0].Index != 1 {
			t.Error("enclosing after the replace")
		}
	})
	t.Run("plain text", func(t *testing.T) {
		s := NewParser().Parse(`aaa${1:bbb${2:ccc}}$0`, opts)
		s.Replace(s.Placeholders()[1], NewParser().Parse(`dddeee$0`, opts).Children())
		if s.String() != "aaabbbdddeee" || len(s.Placeholders()) != 3 {
			t.Errorf("text %q, %d placeholders", s.String(), len(s.Placeholders()))
		}
	})
	t.Run("a marker that is not in the snippet is not replaced", func(t *testing.T) {
		s := NewParser().Parse(`abc`, opts)
		s.Replace(NewText("x"), []Marker{NewText("y")})
		s.Replace(s, []Marker{NewText("y")})
		if s.String() != "abc" {
			t.Errorf("text %q", s.String())
		}
	})
}

func TestPlaceholderOrder(t *testing.T) { // #28185
	cases := []struct {
		a, b int
		want int
	}{{10, 2, 1}, {2, 10, -1}, {0, 5, 1}, {5, 0, -1}, {3, 3, 0}, {0, 0, 0}}
	for _, tc := range cases {
		if got := ComparePlaceholders(NewPlaceholder(tc.a), NewPlaceholder(tc.b)); got != tc.want {
			t.Errorf("compare(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestParserDoesNotRecurseForEver(t *testing.T) { // #28983
	parse(`${1:${foo:${1}}}`)
}

func TestParserMarkersAreUnique(t *testing.T) { // #30407
	for _, in := range []string{
		"class ${1:${TM_FILENAME/(?:\\A|_)([A-Za-z0-9]+)(?:\\.rb)?/(?2::\\u$1)/g}} < ${2:Application}Controller\n  $3\nend",
		`${1:${FOO:abc$1def}}`,
	} {
		seen := map[Marker]bool{}
		parse(in).Walk(func(m Marker) bool {
			if seen[m] {
				t.Errorf("%q: a marker is in the tree twice", in)
			}
			seen[m] = true
			return true
		})
	}
}

func TestFormatStringResolve(t *testing.T) {
	cases := []struct {
		name  string
		f     *FormatString
		value string
		want  string
	}{
		{"upcase", NewFormatString(1, "upcase", nil, nil), "foo", "FOO"},
		{"downcase", NewFormatString(1, "downcase", nil, nil), "FOO", "foo"},
		{"capitalize", NewFormatString(1, "capitalize", nil, nil), "bar", "Bar"},
		{"capitalize several words", NewFormatString(1, "capitalize", nil, nil), "bar no repeat", "Bar no repeat"},
		{"pascalcase kebab", NewFormatString(1, "pascalcase", nil, nil), "bar-foo", "BarFoo"},
		{"pascalcase number", NewFormatString(1, "pascalcase", nil, nil), "bar-42-foo", "Bar42Foo"},
		{"pascalcase snake", NewFormatString(1, "pascalcase", nil, nil), "snake_AndPascalCase", "SnakeAndPascalCase"},
		{"pascalcase kebab pascal", NewFormatString(1, "pascalcase", nil, nil), "kebab-AndPascalCase", "KebabAndPascalCase"},
		{"pascalcase underscore", NewFormatString(1, "pascalcase", nil, nil), "_justPascalCase", "JustPascalCase"},
		{"camelcase kebab", NewFormatString(1, "camelcase", nil, nil), "bar-foo", "barFoo"},
		{"camelcase number", NewFormatString(1, "camelcase", nil, nil), "bar-42-foo", "bar42Foo"},
		{"camelcase snake", NewFormatString(1, "camelcase", nil, nil), "snake_AndCamelCase", "snakeAndCamelCase"},
		{"camelcase kebab camel", NewFormatString(1, "camelcase", nil, nil), "kebab-AndCamelCase", "kebabAndCamelCase"},
		{"camelcase underscore", NewFormatString(1, "camelcase", nil, nil), "_JustCamelCase", "justCamelCase"},
		{"kebabcase camel", NewFormatString(1, "kebabcase", nil, nil), "barFoo", "bar-foo"},
		{"kebabcase pascal", NewFormatString(1, "kebabcase", nil, nil), "BarFoo", "bar-foo"},
		{"kebabcase a capital first", NewFormatString(1, "kebabcase", nil, nil), "ABarFoo", "a-bar-foo"},
		{"kebabcase number", NewFormatString(1, "kebabcase", nil, nil), "bar42Foo", "bar42-foo"},
		{"kebabcase snake", NewFormatString(1, "kebabcase", nil, nil), "snake_AndPascalCase", "snake-and-pascal-case"},
		{"kebabcase kebab", NewFormatString(1, "kebabcase", nil, nil), "kebab-AndCamelCase", "kebab-and-camel-case"},
		{"kebabcase underscore", NewFormatString(1, "kebabcase", nil, nil), "_justPascalCase", "just-pascal-case"},
		{"kebabcase upper", NewFormatString(1, "kebabcase", nil, nil), "__UPCASE__", "upcase"},
		{"kebabcase upper words", NewFormatString(1, "kebabcase", nil, nil), "__BAR_FOO__", "bar-foo"},
		{"snakecase kebab", NewFormatString(1, "snakecase", nil, nil), "bar-foo", "bar_foo"},
		{"snakecase number", NewFormatString(1, "snakecase", nil, nil), "bar-42-foo", "bar_42_foo"},
		{"snakecase snake pascal", NewFormatString(1, "snakecase", nil, nil), "snake_AndPascalCase", "snake_and_pascal_case"},
		{"snakecase kebab pascal", NewFormatString(1, "snakecase", nil, nil), "kebab-AndPascalCase", "kebab_and_pascal_case"},
		{"snakecase underscore", NewFormatString(1, "snakecase", nil, nil), "_justPascalCase", "_just_pascal_case"},
		{"an unknown shorthand", NewFormatString(1, "notKnown", nil, nil), "input", "input"},
		{"if, undefined", NewFormatString(1, "", ptr("foo"), nil), "", ""},
		{"if, value", NewFormatString(1, "", ptr("foo"), nil), "bar", "foo"},
		{"else, nothing", NewFormatString(1, "", nil, ptr("foo")), "", "foo"},
		{"else, value", NewFormatString(1, "", nil, ptr("foo")), "bar", "bar"},
		{"if-else, nothing", NewFormatString(1, "", ptr("bar"), ptr("foo")), "", "foo"},
		{"if-else, value", NewFormatString(1, "", ptr("bar"), ptr("foo")), "baz", "bar"},
		// empty values stay empty whatever the shorthand
		{"upcase of nothing", NewFormatString(1, "upcase", nil, nil), "", ""},
		{"kebabcase of nothing", NewFormatString(1, "kebabcase", nil, nil), "", ""},
		// no words: the value is kept
		{"pascalcase of symbols", NewFormatString(1, "pascalcase", nil, nil), "---", "---"},
		{"camelcase of symbols", NewFormatString(1, "camelcase", nil, nil), "---", "---"},
		{"kebabcase of symbols", NewFormatString(1, "kebabcase", nil, nil), "---", "---"},
		{"capitalize a character outside ASCII", NewFormatString(1, "capitalize", nil, nil), "élan", "Élan"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.f.Resolve(tc.value); got != tc.want {
				t.Errorf("Resolve(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// mapResolver gives the variables values by name.
type mapResolver map[string]string

func (m mapResolver) Resolve(v *Variable) (string, bool) {
	value, ok := m[v.Name]
	return value, ok
}

func TestUnicodeVariableTransformations(t *testing.T) {
	resolver := mapResolver{"RUSSIAN": "одинДва", "GREEK": "έναςΔύο", "TURKISH": "istanbulLı", "JAPANESE": "こんにちは"}
	cases := []struct{ transform, variable, want string }{
		{"kebabcase", "RUSSIAN", "один-два"},
		{"kebabcase", "GREEK", "ένας-δύο"},
		{"snakecase", "RUSSIAN", "один_два"},
		{"snakecase", "GREEK", "ένας_δύο"},
		{"camelcase", "RUSSIAN", "одинДва"},
		{"camelcase", "GREEK", "έναςΔύο"},
		{"pascalcase", "RUSSIAN", "ОдинДва"},
		{"pascalcase", "GREEK", "ΈναςΔύο"},
		{"upcase", "RUSSIAN", "ОДИНДВА"},
		{"downcase", "RUSSIAN", "одиндва"},
		{"kebabcase", "TURKISH", "istanbul-lı"},
		{"pascalcase", "TURKISH", "IstanbulLı"},
		{"upcase", "JAPANESE", "こんにちは"},
		{"kebabcase", "JAPANESE", "こんにちは"},
	}
	for _, tc := range cases {
		t.Run(tc.transform+" "+tc.variable, func(t *testing.T) {
			s := parse("${" + tc.variable + "/(.*)/${1:/" + tc.transform + "}/}")
			v := s.Children()[0].(*Variable)
			v.Resolve(resolver)
			if got := v.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTransformResolve(t *testing.T) {
	t.Run("optional transforms are applied when reusing the same variable, #37702", func(t *testing.T) {
		tr, err := NewTransform(`^(.)|-(.)`, "g")
		if err != nil {
			t.Fatal(err)
		}
		tr.AppendChild(NewFormatString(1, "upcase", nil, nil))
		tr.AppendChild(NewFormatString(2, "upcase", nil, nil))
		if got := tr.Resolve("my-file-name"); got != "MyFileName" {
			t.Errorf("got %q", got)
		}
		if got := tr.Clone().(*Transform).Resolve("my-file-name"); got != "MyFileName" {
			t.Errorf("the clone: %q", got)
		}
	})
	t.Run("undefined variables in the same snippet, #51769", func(t *testing.T) {
		tr, err := NewTransform("foo", "gi")
		if err != nil {
			t.Fatal(err)
		}
		tr.AppendChild(NewText("bar"))
		if got := tr.ToTextmateString(); got != "/foo/bar/ig" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("the serialization joins the children without a comma", func(t *testing.T) {
		tr, err := NewTransform("foo", "g")
		if err != nil {
			t.Fatal(err)
		}
		tr.AppendChild(NewFormatString(1, "upcase", nil, nil))
		tr.AppendChild(NewText("_"))
		serialized := tr.ToTextmateString()
		if serialized != "/foo/${1:/upcase}_/g" {
			t.Fatalf("got %q", serialized)
		}
		if got := parse("${TM_FILENAME" + serialized + "}").ToTextmateString(); got != "${TM_FILENAME"+serialized+"}" {
			t.Errorf("after parsing: %q", got)
		}
	})
	cases := []struct {
		name, pattern, flags, format, value, want string
	}{
		{"the whole match", `.*`, "", `[$0]`, "abc", "[abc]"},
		{"first match only", `a`, "", `X`, "banana", "bXnana"},
		{"every match", `a`, "g", `X`, "banana", "bXnXnX"},
		{"ignoring case", `A`, "gi", `X`, "banana", "bXnXnX"},
		{"sticky matches at the start only", `b`, "y", `X`, "abc", "abc"},
		{"empty matches go one character on", ``, "g", `-`, "ab", "-a-b-"},
		{"an empty first match", `x*`, "", `-`, "ab", "-ab"},
		{"no match keeps the value", `z`, "g", `X`, "banana", "banana"},
		{"a group that did not match is empty", `(a)|(b)`, "g", `[$1|$2]`, "ab", "[a|][|b]"},
		{"a group past the end is empty", `a`, "g", `[$3]`, "a", "[]"},
		{"multi-byte text", `é`, "g", `e`, "éàé", "eàe"},
		{"dot matches a line break with s", `a.b`, "s", `X`, "a\nb", "X"},
		{"dot stops at a line break", `a.b`, "", `X`, "a\nb", "a\nb"},
		{"^ at every line with m", `^`, "gm", `> `, "a\nb", "> a\n> b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// the format is parsed as a snippet's: $1 and text
			p := parse("${v/" + tc.pattern + "/" + tc.format + "/" + tc.flags + "}")
			v, ok := p.Children()[0].(*Variable)
			if !ok || v.Transform == nil {
				t.Fatalf("the transform did not parse: %s", kinds(p.Children()))
			}
			if got := v.Transform.Resolve(tc.value); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewTransformErrors(t *testing.T) {
	cases := []struct{ name, pattern, flags, tag string }{
		{"an unknown flag", "a", "x", "NewTransform->Transform.init.SnippetInvalidFlags"},
		{"a flag twice", "a", "gg", "NewTransform->Transform.init.SnippetInvalidFlags"},
		{"upper case flags", "a", "G", "NewTransform->Transform.init.SnippetInvalidFlags"},
		{"u and v together", "a", "uv", "NewTransform->Transform.init.SnippetInvalidFlags"},
		{"a pattern that does not compile", "(a", "g", "NewTransform->Transform.init->Compile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTransform(tc.pattern, tc.flags)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.tag) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestTransformSource(t *testing.T) {
	cases := []struct{ pattern, want string }{
		{``, `(?:)`},
		{`a/b`, `a\/b`},
		{`a\/b`, `a\/b`},
		{`[/]`, `[/]`},
		{"a\nb", `a\nb`},
		{`(.*)\..+$`, `(.*)\..+$`},
	}
	for _, tc := range cases {
		tr := &Transform{Pattern: tc.pattern}
		if got := tr.Source(); got != tc.want {
			t.Errorf("Source(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
	}
}

func TestPlaceholderIndexTooBigIsText(t *testing.T) {
	// not in VS Code's tests: a number that is no index stays text
	in := "$99999999999999999999 and ${99999999999999999999}"
	if got := parse(in).String(); got != in {
		t.Errorf("got %q", got)
	}
}

func TestParserFinalTabstop(t *testing.T) { // #33147
	phs := NewParser().Parse(`src="$1"`, ParseOpts{InsertFinalTabstop: true}).Placeholders()
	if len(phs) != 2 || phs[0].Index != 1 || phs[1].Index != 0 {
		t.Errorf("placeholders %v", phs)
	}
	cases := []struct {
		in   string
		opts ParseOpts
		want int
	}{
		{"no tab stops", ParseOpts{InsertFinalTabstop: true}, 0},
		{"no tab stops", ParseOpts{EnforceFinalTabstop: true}, 1},
		{"$0", ParseOpts{EnforceFinalTabstop: true}, 1},
		{"$1 $0", ParseOpts{InsertFinalTabstop: true}, 2},
		{"$1", ParseOpts{}, 1},
	}
	for _, tc := range cases {
		if got := len(NewParser().Parse(tc.in, tc.opts).Placeholders()); got != tc.want {
			t.Errorf("%q %+v: %d placeholders, want %d", tc.in, tc.opts, got, tc.want)
		}
	}
}

func TestAsInsertTextAndClipboardGuess(t *testing.T) {
	for in, want := range map[string]bool{`$CLIPBOARD`: true, `${CLIPBOARD}`: true, `${CLIPBOARD/a/b/}`: true, `CLIPBOARD`: false, `$clipboard`: false, ``: false} {
		if got := GuessNeedsClipboard(in); got != want {
			t.Errorf("GuessNeedsClipboard(%q) = %v", in, got)
		}
	}
}

func TestParseFragment(t *testing.T) {
	s := NewParser().Parse(`${1:foo} `)
	added := NewParser().ParseFragment(`bar ${1} $2`, s)
	// the text before the first placeholder joins the text that the snippet ended with
	if kinds(added) != "Placeholder Text Placeholder" {
		t.Fatalf("added %s", kinds(added))
	}
	if got := s.String(); got != "foo bar foo " {
		t.Errorf("text %q", got)
	}
}

func TestToTextmateStringKeepsVSCodesAmbiguity(t *testing.T) {
	// a placeholder with no default is written $1, so that a digit that follows it reads on into
	// the number, as it does in VS Code
	s := parse(`$1` + "${2:}" + `0`)
	got := s.ToTextmateString()
	if got != `$1$20` {
		t.Errorf("got %q", got)
	}
}

func TestMirroredNestedPlaceholdersKeepTheirParents(t *testing.T) { // #58736
	s := parse(`${3:nest1 ${1:nest2 ${2:nest3}}} $3`)
	if kinds(s.Children()) != "Placeholder Text Placeholder" {
		t.Fatalf("markers %s", kinds(s.Children()))
	}
	var check func(m Marker)
	check = func(m Marker) {
		for _, c := range m.Children() {
			check(c)
		}
		if _, ok := m.(*Placeholder); !ok {
			return
		}
		found := false
		for p := m; p != nil && !found; p = p.Parent() {
			found = p.Parent() == Marker(s)
		}
		if !found {
			t.Errorf("a placeholder does not lead up to the snippet")
		}
	}
	check(s.Children()[2])
}

func TestFormatStringBranchesWithEscapes(t *testing.T) {
	cases := []struct {
		name, in string
		ifValue  string
		hasElse  bool
	}{
		{"a closing bracket in the if branch, #78883", `${TM_DIRECTORY/(.+)/${1:+import { hello \} from world}/}`, "import { hello } from world", false},
		{"backslashes in the if branch, #80394", `${CURRENT_YEAR/(.+)/${1:+\\}/}`, `\`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := parse(tc.in)
			v, ok := s.Children()[0].(*Variable)
			if len(s.Children()) != 1 || !ok || v.Transform == nil || len(v.Transform.Children()) != 1 {
				t.Fatalf("markers %s", kinds(s.Children()))
			}
			f, ok := v.Transform.Children()[0].(*FormatString)
			if !ok || f.IfValue == nil || *f.IfValue != tc.ifValue || (f.ElseValue != nil) != tc.hasElse {
				t.Errorf("format string %+v", f)
			}
		})
	}
}
