package edit

import (
	"regexp"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// Ported from VS Code: reference/vscode/src/vs/editor/test/browser/controller/cursor.test.ts
// (the tests of "autoClosingPairs", of auto-closing overtype and delete, and of surrounding)

func pair(open, closing string, notIn ...string) language.AutoClosingPair {
	return language.AutoClosingPair{Pair: language.Pair{Open: open, Close: closing}, NotIn: notIn}
}

// autoClosingConfig is VS Code's autoClosingLanguage.
func autoClosingConfig() *language.Configuration {
	return &language.Configuration{
		Comments: &language.CommentRule{BlockComment: &language.Pair{Open: "/*", Close: "*/"}},
		AutoClosingPairs: []language.AutoClosingPair{
			pair("{", "}"), pair("[", "]"), pair("(", ")"),
			pair("'", "'", "string", "comment"),
			pair("\"", "\"", "string"),
			pair("`", "`", "string", "comment"),
			pair("/**", " */", "string"),
			pair("begin", "end", "string"),
		},
	}
}

// acState is the state of the tokenizer of VS Code's autoClosingLanguage: a stack of code,
// string and block comment states.
type acState struct {
	kind   byte // 'b' base, 's' string, 'c' block comment
	char   byte // the quote of a string
	parent *acState
}

var (
	reCodeRun    = regexp.MustCompile("^[^'\"`{}/]+")
	reStringRun  = regexp.MustCompile("^[^\\\\'\"`$]+")
	reCommentRun = regexp.MustCompile(`^[^*]+`)
)

// acTokenize tokenizes one line as VS Code's test tokenizer does, from a state; adjacent tokens of
// one type are one.
func acTokenize(line string, st *acState) ([]tokens.Token, *acState) {
	if st == nil {
		st = &acState{kind: 'b'}
	}
	var out []tokens.Token
	pos := 0
	emit := func(n int, typ textmate.TokenType, next *acState) {
		pos = min(pos+n, len(line))
		if k := len(out) - 1; k >= 0 && out[k].Type == typ {
			out[k].End = int32(pos)
		} else {
			out = append(out, tokens.Token{End: int32(pos), Type: typ})
		}
		if next != nil {
			st = next
		}
	}
	for pos < len(line) {
		rest := line[pos:]
		switch st.kind {
		case 'b':
			switch {
			case reCodeRun.MatchString(rest):
				emit(len(reCodeRun.FindString(rest)), textmate.TokenOther, nil)
			case strings.ContainsAny(rest[:1], "'\"`"):
				emit(1, textmate.TokenString, &acState{kind: 's', char: rest[0], parent: st})
			case rest[0] == '{':
				emit(1, textmate.TokenOther, &acState{kind: 'b', parent: st})
			case rest[0] == '}':
				next := st.parent
				if next == nil {
					next = &acState{kind: 'b'}
				}
				emit(1, textmate.TokenOther, next)
			case strings.HasPrefix(rest, "//"):
				emit(len(rest), textmate.TokenComment, nil)
			case strings.HasPrefix(rest, "/*"):
				emit(2, textmate.TokenComment, &acState{kind: 'c', parent: st})
			default:
				emit(1, textmate.TokenOther, nil)
			}
		case 's':
			switch {
			case reStringRun.MatchString(rest):
				emit(len(reStringRun.FindString(rest)), textmate.TokenString, nil)
			case rest[0] == '\\':
				emit(2, textmate.TokenString, nil)
			case rest[0] == st.char:
				emit(1, textmate.TokenString, st.parent)
			case strings.HasPrefix(rest, "${"):
				emit(2, textmate.TokenOther, &acState{kind: 'b', parent: st})
			default:
				emit(1, textmate.TokenOther, nil)
			}
		default:
			switch {
			case reCommentRun.MatchString(rest):
				emit(len(reCommentRun.FindString(rest)), textmate.TokenString, nil)
			case strings.HasPrefix(rest, "*/"):
				emit(2, textmate.TokenComment, st.parent)
			default:
				emit(1, textmate.TokenOther, nil)
			}
		}
	}
	return out, st
}

// acSource gives the commands the tokens of the text of an ed, tokenized by acTokenize.
type acSource struct{ e *ed }

func (s acSource) lines() []string { return strings.Split(s.e.doc.Text(), "\n") }

// stateBefore is the state a line starts in.
func (s acSource) stateBefore(line int) *acState {
	var st *acState
	for i, text := range s.lines()[:line] {
		_, st = acTokenize(text, st)
		_ = i
	}
	return st
}

func (s acSource) LineTokens(line int) []tokens.Token {
	toks, _ := acTokenize(s.lines()[line], s.stateBefore(line))
	return toks
}
func (s acSource) Language() int                 { return 0 }
func (s acSource) CheapToTokenize(line int) bool { return true }

func (s acSource) TypeIfInserting(line, col int, ch string) (textmate.TokenType, bool) {
	text := s.lines()[line]
	toks, _ := acTokenize(text[:col]+ch+text[col:], s.stateBefore(line))
	for _, t := range toks {
		if col < int(t.End) {
			return t.Type, true
		}
	}
	return textmate.TokenOther, true
}

// acEd is an editor of lines in a language with auto-closing pairs. Its text has no tokens, as in
// the VS Code tests that do not register a tokenizer: all of it is code.
func acEd(t *testing.T, cfg *language.Configuration, lines ...string) *ed {
	t.Helper()
	e := newEd(t, DefaultOptions(), lines...)
	e.opts.AutoClosing = autoclose.NewLanguage(cfg)
	return e
}

// acEdTokens is acEd with the test tokenizer, which finds strings and comments.
func acEdTokens(t *testing.T, cfg *language.Configuration, lines ...string) *ed {
	t.Helper()
	e := acEd(t, cfg, lines...)
	e.opts.Tokens = acSource{e}
	return e
}

// typeAt moves the caret and types ch, returns the line and undoes it: VS Code's assertType.
func (e *ed) typeAt(line, col int, ch string) string {
	e.t.Helper()
	before := e.doc.Text()
	e.caret(line, col).typeText(ch)
	got := e.line(line)
	for e.doc.Text() != before {
		e.undo()
	}
	return got
}

// assertAutoClosing checks, for every column of the lines, whether typing ch closes: the
// annotated line has a | before the characters where it must (VS Code's extractAutoClosingSpecialColumns).
func assertAutoClosing(t *testing.T, e *ed, ch, closed string, annotated []string) {
	t.Helper()
	for i, ann := range annotated {
		n := i + 1
		text := e.line(n)
		special := map[int]bool{}
		col := 1
		for _, r := range ann {
			if r == '|' {
				special[col] = true
			} else {
				col++
			}
		}
		for col := 1; col <= len([]rune(text))+1; col++ {
			idx := len(string([]rune(text)[:col-1]))
			want := text[:idx] + ch + text[idx:]
			msg := "does not auto close"
			if special[col] {
				want = text[:idx] + closed + text[idx:]
				msg = "auto closes"
			}
			if got := e.typeAt(n, col, ch); got != want {
				t.Errorf("%s @ (%d, %d): line = %q, want %q", msg, n, col, got, want)
			}
		}
	}
}

var autoClosingText = []string{
	"var a = [];",
	"var b = `asd`;",
	"var c = 'asd';",
	"var d = \"asd\";",
	"var e = /*3*/\t3;",
	"var f = /** 3 */3;",
	"var g = (3+5);",
	"var h = { a: 'value' };",
}

func TestAutoClosingOpenParens(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), autoClosingText...)
		assertAutoClosing(t, e, "(", "()", []string{
			"var| a| |=| [|]|;|",
			"var| b| |=| |`asd|`|;|",
			"var| c| |=| |'asd|'|;|",
			"var| d| |=| |\"asd|\"|;|",
			"var| e| |=| /*3*/|\t3|;|",
			"var| f| |=| /**| 3| */3|;|",
			"var| g| |=| (3+5|)|;|",
			"var| h| |=| {| a|:| |'value|'| |}|;|",
		})
	})
	t.Run("before whitespace", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), autoClosingText...)
		e.opts.AutoClosingBrackets = autoclose.BeforeWhitespace
		assertAutoClosing(t, e, "(", "()", []string{
			"var| a| =| [|];|",
			"var| b| =| `asd`;|",
			"var| c| =| 'asd';|",
			"var| d| =| \"asd\";|",
			"var| e| =| /*3*/|\t3;|",
			"var| f| =| /**| 3| */3;|",
			"var| g| =| (3+5|);|",
			"var| h| =| {| a:| 'value'| |};|",
		})
	})
	t.Run("brackets before whitespace, quotes never", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "var a = [];")
		e.opts.AutoClosingBrackets = autoclose.BeforeWhitespace
		e.opts.AutoClosingQuotes = autoclose.Never
		assertAutoClosing(t, e, "(", "()", []string{"var| a| =| [|];|"})
		assertAutoClosing(t, e, "'", "'", []string{"var a = [];"})
	})
	t.Run("language defined characters", func(t *testing.T) {
		cfg := autoClosingConfig()
		set := "abc"
		cfg.AutoCloseBefore = &set
		cfg.AutoClosingPairs = cfg.AutoClosingPairs[:7]
		e := acEd(t, cfg, autoClosingText...)
		assertAutoClosing(t, e, "(", "()", []string{
			"v|ar |a = [|];|",
			"v|ar |b = `|asd`;|",
			"v|ar |c = '|asd';|",
			"v|ar d = \"|asd\";|",
			"v|ar e = /*3*/\t3;|",
			"v|ar f = /** 3| */3;|",
			"v|ar g = (3+5|);|",
			"v|ar h = { |a: 'v|alue' |};|",
		})
	})
	t.Run("can be disabled", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), autoClosingText...)
		e.opts.AutoClosingBrackets = autoclose.Never
		e.opts.AutoClosingQuotes = autoclose.Never
		same := make([]string, len(autoClosingText))
		copy(same, autoClosingText)
		assertAutoClosing(t, e, "(", "()", same)
		assertAutoClosing(t, e, "\"", "\"\"", same)
	})
}

func TestAutoClosingQuote(t *testing.T) {
	e := acEd(t, autoClosingConfig(), autoClosingText...)
	assertAutoClosing(t, e, "'", "''", []string{
		"var a |=| [|]|;|",
		"var b |=| `asd`|;|",
		"var c |=| 'asd'|;|",
		"var d |=| \"asd\"|;|",
		"var e |=| /*3*/|\t3;|",
		"var f |=| /**| 3 */3;|",
		"var g |=| (3+5)|;|",
		"var h |=| {| a:| 'value'| |}|;|",
	})
}

func TestAutoClosingBacktickAfterWord(t *testing.T) {
	// issue #61070
	e := acEd(t, autoClosingConfig(), "const markup = highlight")
	if got := e.typeAt(1, 25, "`"); got != "const markup = highlight``" {
		t.Errorf("line = %q", got)
	}
}

func TestAutoClosingQuoteClosingAString(t *testing.T) {
	// issue #132912
	e := acEdTokens(t, autoClosingConfig(), "const t2 = `something ${t1}")
	if got := e.typeAt(1, 28, "`"); got != "const t2 = `something ${t1}`" {
		t.Errorf("line = %q", got)
	}
}

func TestAutoClosingMultiCharacter(t *testing.T) {
	e := acEd(t, autoClosingConfig(), "begi")
	e.caret(1, 5).typeText("n")
	e.assertLine(1, "beginend")
	e.assertCaret(1, 6)

	e = acEd(t, autoClosingConfig(), "/*")
	e.caret(1, 3).typeText("*")
	e.assertLine(1, "/** */")
	e.assertCaret(1, 4)

	// autoClosingComments: never
	e = acEd(t, autoClosingConfig(), "/*")
	e.opts.AutoClosingComments = autoclose.Never
	e.caret(1, 3).typeText("*")
	e.assertLine(1, "/**")
}

func TestAutoClosingMultiCharacterUndo(t *testing.T) {
	// issue #338693: undo first removes only the auto-closed characters
	e := acEd(t, autoClosingConfig(), "begi begi")
	e.at(sel4{1, 5, 1, 5}, sel4{1, 10, 1, 10}).typeText("n")
	e.assertLine(1, "beginend beginend")
	e.assertSels(sel4{1, 6, 1, 6}, sel4{1, 15, 1, 15})
	e.undo()
	e.assertLine(1, "begin begin")
	e.assertSels(sel4{1, 6, 1, 6}, sel4{1, 12, 1, 12})
	e.undo()
	e.assertLine(1, "begi begi")
	e.assertSels(sel4{1, 5, 1, 5}, sel4{1, 10, 1, 10})
}

func beforeText(t *testing.T, pattern string) *regex.Regexp {
	t.Helper()
	re, err := regex.Compile(pattern, regex.Options{Syntax: regex.ECMAScript})
	if err != nil {
		t.Fatal(err)
	}
	return re
}

func TestAutoClosingBeforeText(t *testing.T) {
	begin := pair("begin", "end")
	begin.BeforeText = beforeText(t, `(?:^|\s)begin$`)
	cfg := &language.Configuration{AutoClosingPairs: []language.AutoClosingPair{begin}}

	e := acEd(t, cfg, "begi", "begi")
	e.at(sel4{1, 5, 1, 5}, sel4{2, 5, 2, 5}).typeText("n")
	e.assertText("beginend\nbeginend")

	e = acEd(t, cfg, "prefixbegi")
	e.caret(1, 11).typeText("n")
	e.assertText("prefixbegin")

	e = acEd(t, cfg, "prefix begi")
	e.caret(1, 12).typeText("n")
	e.assertText("prefix beginend")

	// falls back to a shorter pair
	cfg = &language.Configuration{AutoClosingPairs: []language.AutoClosingPair{begin, pair("n", "]")}}
	e = acEd(t, cfg, "prefixbegi")
	e.caret(1, 11).typeText("n")
	e.assertText("prefixbegin]")
}

func TestAutoClosingContainedPairs(t *testing.T) {
	// issue #72177
	cfg := &language.Configuration{AutoClosingPairs: []language.AutoClosingPair{
		pair("(", ")"), pair("(*", "*)"), pair("<@", "@>"), pair("<@@", "@@>"),
	}}
	e := acEd(t, cfg, "")
	e.typeText("(")
	e.assertLine(1, "()")
	e.typeText("*")
	e.assertLine(1, "(**)") // does not add the entire closing text when a part is there

	e = acEd(t, cfg, "(")
	e.caret(1, 2).typeText("*")
	e.assertLine(1, "(**)") // does add the entire closing text if it is not there

	e = acEd(t, cfg, "")
	e.typeText("<@")
	e.assertLine(1, "<@@>")
	e.typeText("@")
	e.assertLine(1, "<@@@@>") // closes before a closing text of several characters
	e.typeText("(")
	e.assertLine(1, "<@@()@@>")
}

func TestAutoClosingNotWhenEndingWithOpen(t *testing.T) {
	// issue #55314
	cfg := &language.Configuration{AutoClosingPairs: []language.AutoClosingPair{
		pair("{", "}"), pair("[", "]"), pair("(", ")"),
		pair("'", "'", "string", "comment"),
		pair("\"", "\"", "string"),
		pair("B\"", "\"", "string", "comment"),
		pair("`", "`", "string", "comment"),
		pair("/**", " */", "string"),
	}}
	e := acEd(t, cfg, "little goat", "little LAMB", "little sheep", "Big LAMB")
	for _, at := range [][2]int{{1, 4}, {2, 4}, {3, 4}, {4, 2}, {4, 3}} {
		text := e.line(at[0])
		want := text[:at[1]-1] + "\"" + text[at[1]-1:]
		if got := e.typeAt(at[0], at[1], "\""); got != want {
			t.Errorf("@ %v: line = %q, want %q", at, got, want)
		}
	}
	// issue #27937
	e = acEd(t, autoClosingConfig(), `var arr = ["b", "c"];`)
	if got := e.typeAt(1, 12, "\""); got != `var arr = [""b", "c"];` {
		t.Errorf("line = %q", got)
	}
}

// unmatchedClosing stands in for the bracket index, which has its own tests: it pairs the brackets of
// the whole text with a stack and reports whether a closing bracket that closes nothing, and that
// closes the kind of the opening bracket, is at or after the position. Lines that start with a
// quote or a comment have no brackets.
func unmatchedClosing(e *ed) func(buffer.Position, string) bool {
	return func(pos buffer.Position, opening string) bool {
		kinds := map[byte]byte{'}': '{', ']': '[', ')': '('}
		var stack []byte
		for l, text := range strings.Split(e.doc.Text(), "\n") {
			if trimmed := strings.TrimSpace(text); strings.HasPrefix(trimmed, "\"") || strings.HasPrefix(trimmed, "//") {
				continue
			}
			for i := 0; i < len(text); i++ {
				switch c := text[i]; c {
				case '{', '[', '(':
					stack = append(stack, c)
				case '}', ']', ')':
					if n := len(stack); n > 0 && stack[n-1] == kinds[c] {
						stack = stack[:n-1]
					} else if string(kinds[c]) == opening && !(buffer.Position{Line: l, Col: i}).IsBefore(pos) {
						return true
					}
				}
			}
		}
		return false
	}
}

func TestAutoClosingBalancesBrackets(t *testing.T) {
	// issue #6841
	cfg := &language.Configuration{
		Brackets:         []language.Pair{{Open: "{", Close: "}"}, {Open: "[", Close: "]"}, {Open: "(", Close: ")"}},
		AutoClosingPairs: []language.AutoClosingPair{pair("{", "}"), pair("[", "]"), pair("(", ")")},
	}
	cases := []struct {
		text      string
		line, col int
		typed     string
		want      string
	}{
		{"\n}", 1, 1, "{", "{\n}"},
		{"", 1, 1, "{", "{}"},
		{"{}", 1, 2, "{", "{{}}"},
		{"{\n", 2, 1, "{", "{\n{}"},
		{"\n]", 1, 1, "{", "{}\n]"},
		{")\n", 2, 1, "(", ")\n()"},
		{"function foo() {\n\n}", 2, 1, "{", "function foo() {\n{}\n}"},
		{"someFunction);", 1, 13, "(", "someFunction();"},
		{"someFunction;", 1, 13, "(", "someFunction();"},
		{"someFunctionsomeParam);", 1, 13, "(", "someFunction(someParam);"},
	}
	for _, tt := range cases {
		t.Run(strings.ReplaceAll(tt.text, "\n", "⏎")+tt.typed, func(t *testing.T) {
			e := acEd(t, cfg, strings.Split(tt.text, "\n")...)
			e.opts.UnmatchedClosing = unmatchedClosing(e)
			e.caret(tt.line, tt.col).typeText(tt.typed)
			e.assertText(tt.want)
		})
	}
	t.Run("always ignores the balance", func(t *testing.T) {
		e := acEd(t, cfg, "", ")")
		e.opts.AutoClosingBrackets = autoclose.Always
		e.opts.UnmatchedClosing = func(buffer.Position, string) bool { return true }
		e.caret(1, 1).typeText("(")
		e.assertText("()\n)")
	})
}

func TestAutoClosingQuotesAfterWordCharacters(t *testing.T) {
	// issue #25658
	e := acEd(t, autoClosingConfig(), "")
	typeLine := func(text string) {
		e.caret(len(strings.Split(e.doc.Text(), "\n")), 1000).typeText("\n" + text)
	}
	e.typeText("teste1 = teste' ok")
	e.assertLine(1, "teste1 = teste' ok")
	typeLine("teste2 = teste 'ok")
	e.assertLine(2, "teste2 = teste 'ok'")
	typeLine("teste3 = teste\" ok")
	e.assertLine(3, "teste3 = teste\" ok")
	typeLine("teste4 = teste \"ok")
	e.assertLine(4, "teste4 = teste \"ok\"")
	typeLine("teste '")
	e.assertLine(5, "teste ''")
	typeLine("teste \"")
	e.assertLine(6, "teste \"\"")
	typeLine("teste'")
	e.assertLine(7, "teste'")
	typeLine("teste\"")
	e.assertLine(8, "teste\"")
}

func TestAutoClosingOvertype(t *testing.T) {
	t.Run("only those characters that it inserted (issue #37315)", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "", "y=();")
		e.caret(1, 1).typeText("x=(")
		e.assertLine(1, "x=()")
		e.typeText("asd")
		e.assertLine(1, "x=(asd)")
		e.typeText(")")
		e.assertSels(sel4{1, 8, 1, 8})
		e.assertLine(1, "x=(asd)")
		e.caret(2, 4).typeText(")")
		e.assertLine(2, "y=());")
	})
	t.Run("stops once the cursor leaves the area", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "", "y=();")
		e.caret(1, 1).typeText("x=(")
		e.caret(1, 5).typeText(")")
		e.assertLine(1, "x=())")
	})
	t.Run("only once", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "", "y=();")
		e.caret(1, 1).typeText("x=(")
		e.typeText(")")
		e.assertLine(1, "x=()")
		e.caret(1, 4).typeText(")")
		e.assertLine(1, "x=())")
	})
	t.Run("remembers several instances", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "", "y=();")
		e.caret(1, 1).typeText("x=(")
		e.typeText("(")
		e.assertLine(1, "x=(())")
		e.typeText(")")
		e.assertLine(1, "x=(())")
		e.typeText(")")
		e.assertLine(1, "x=(())")
		e.assertCaret(1, 7)
	})
	t.Run("always", func(t *testing.T) {
		// issue #78833
		e := acEd(t, autoClosingConfig(), "", "y=();")
		e.opts.AutoClosingOvertype = autoclose.AlwaysAct
		e.caret(1, 1).typeText("x=(")
		e.typeText(")")
		e.assertLine(1, "x=()")
		e.caret(1, 4).typeText(")")
		e.assertLine(1, "x=()")
		e.caret(2, 4).typeText(")")
		e.assertLine(2, "y=();")
	})
	t.Run("never", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "")
		e.opts.AutoClosingOvertype = autoclose.NeverAct
		e.typeText("(")
		e.typeText(")")
		e.assertLine(1, "())")
	})
	t.Run("a quote after a backslash is not typed over (issue #53357)", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "console.log();")
		e.caret(1, 13).typeText("'")
		e.assertLine(1, "console.log('');")
		e.typeText("it")
		e.assertLine(1, "console.log('it');")
		e.typeText("\\")
		e.assertLine(1, "console.log('it\\');")
		e.typeText("'")
		e.assertLine(1, "console.log('it\\'');")
	})
	t.Run("a bracket after a backslash is typed over (issue #84998)", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "")
		e.typeText("\\")
		e.typeText("(")
		e.assertLine(1, "\\()")
		e.typeText("abc")
		e.assertLine(1, "\\(abc)")
		e.typeText("\\")
		e.assertLine(1, "\\(abc\\)")
		e.typeText(")")
		e.assertLine(1, "\\(abc\\)")
	})
	t.Run("a quote is not closed on an odd count (issue #78527)", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "std::cout << '\"' << entryMap")
		e.caret(1, 29).typeText("[")
		e.assertLine(1, "std::cout << '\"' << entryMap[]")
		e.typeText("\"")
		e.assertLine(1, "std::cout << '\"' << entryMap[\"\"]")
		e.typeText("a")
		e.assertLine(1, "std::cout << '\"' << entryMap[\"a\"]")
		e.typeText("\"")
		e.assertLine(1, "std::cout << '\"' << entryMap[\"a\"]")
		e.typeText("]")
		e.assertLine(1, "std::cout << '\"' << entryMap[\"a\"]")
	})
	t.Run("not when a selection is not empty", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "()")
		e.opts.AutoClosingOvertype = autoclose.AlwaysAct
		e.opts.AutoSurround = autoclose.SurroundNever
		e.at(sel4{1, 1, 1, 2}).typeText(")")
		e.assertLine(1, "))")
	})
}

func TestAutoClosingDelete(t *testing.T) {
	t.Run("only those characters that it inserted (issue #118270)", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "", "y=();")
		e.caret(1, 1).typeText("x=(")
		e.typeText("asd")
		e.assertLine(1, "x=(asd)")
		e.back().back().back()
		e.assertLine(1, "x=()")
		e.back()
		e.assertLine(1, "x=")
		e.caret(2, 4).back()
		e.assertLine(2, "y=);")
	})
	t.Run("always", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "y=();")
		e.opts.AutoClosingDelete = autoclose.AlwaysAct
		e.caret(1, 4).back()
		e.assertLine(1, "y=;")
	})
	t.Run("never", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "")
		e.opts.AutoClosingDelete = autoclose.NeverAct
		e.typeText("(")
		e.back()
		e.assertLine(1, ")")
	})
	t.Run("quotes", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "")
		e.typeText("'")
		e.assertLine(1, "''")
		e.back()
		e.assertLine(1, "")
		e.opts.AutoClosingQuotes = autoclose.Never
		e.typeText("'")
		e.assertLine(1, "'")
	})
	t.Run("all cursors do the same thing", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "var a = ()")
		e.at(sel4{1, 4, 1, 4}, sel4{1, 10, 1, 10}).back()
		e.assertLine(1, "va a = )")
	})
}

func TestAutoClosingDoesNotCloseWhenTheNextCharacterIsNotAllowed(t *testing.T) {
	e := acEd(t, autoClosingConfig(), "foo")
	if got := e.typeAt(1, 1, "("); got != "(foo" {
		t.Errorf("line = %q", got)
	}
	if got := e.typeAt(1, 4, "("); got != "foo()" {
		t.Errorf("line = %q", got)
	}
}

func TestAutoClosingPythonBeforeWhitespace(t *testing.T) {
	// issue #85983
	cfg := &language.Configuration{AutoClosingPairs: []language.AutoClosingPair{
		pair("{", "}"), pair("[", "]"), pair("(", ")"),
		pair("\"", "\"", "string"), pair("r\"", "\"", "string", "comment"),
		pair("'", "'", "string", "comment"), pair("r'", "'", "string", "comment"),
		pair("`", "`", "string"),
	}}
	e := acEd(t, cfg, "foo'hello'")
	e.opts.AutoClosingBrackets = autoclose.BeforeWhitespace
	if got := e.typeAt(1, 4, "("); got != "foo('hello'" {
		t.Errorf("line = %q", got)
	}
}

func TestSurroundSelection(t *testing.T) {
	t.Run("several selections, configurable", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "var a = asd")
		e.at(sel4{1, 1, 1, 4}, sel4{1, 9, 1, 12}).typeText("`")
		e.assertText("`var` a = `asd`")
		e.typeText("(")
		e.assertText("`(var)` a = `(asd)`")

		e = acEd(t, autoClosingConfig(), "var a = asd")
		e.opts.AutoSurround = autoclose.SurroundNever
		e.at(sel4{1, 1, 1, 4}).typeText("`")
		e.assertText("` a = asd")

		e = acEd(t, autoClosingConfig(), "var a = asd")
		e.opts.AutoSurround = autoclose.SurroundQuotes
		e.at(sel4{1, 1, 1, 4}).typeText("`")
		e.assertText("`var` a = asd")
		e.typeText("(")
		e.assertText("`(` a = asd")

		e = acEd(t, autoClosingConfig(), "var a = asd")
		e.opts.AutoSurround = autoclose.SurroundBrackets
		e.at(sel4{1, 1, 1, 4}).typeText("(")
		e.assertText("(var) a = asd")
		e.typeText("`")
		e.assertText("(`) a = asd")
	})
	t.Run("double bracket surrounding (bug #11476)", func(t *testing.T) {
		cfg := &language.Configuration{AutoClosingPairs: []language.AutoClosingPair{pair("(", ")")}}
		e := acEd(t, cfg, "hello")
		e.at(sel4{1, 3, 1, 5}).typeText("(")
		e.assertSels(sel4{1, 4, 1, 6})
		e.typeText("(")
		e.assertSels(sel4{1, 5, 1, 7})
		e.assertText("he((ll))o")
	})
	t.Run("an empty closing text (issue #206774)", func(t *testing.T) {
		cfg := &language.Configuration{SurroundingPairs: []language.Pair{{Open: "<", Close: ""}}}
		e := acEd(t, cfg, "hello world")
		e.at(sel4{1, 1, 1, 6}).typeText("<")
		e.assertText("<hello world")
		e.assertSels(sel4{1, 2, 1, 7})
	})
	t.Run("special handling of quotes (issue #41825)", func(t *testing.T) {
		cfg := &language.Configuration{SurroundingPairs: []language.Pair{{Open: "\"", Close: "\""}, {Open: "'", Close: "'"}}}
		e := acEd(t, cfg, "var x = 'hi';")
		e.at(sel4{1, 9, 1, 10}, sel4{1, 12, 1, 13}).typeText("\"")
		e.assertText("var x = \"hi\";")
		e.at(sel4{1, 9, 1, 10}, sel4{1, 12, 1, 13}).typeText("'")
		e.assertText("var x = 'hi';")
	})
	t.Run("white space is not surrounded", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "a  b")
		e.opts.AutoClosingOvertype = autoclose.NeverAct
		e.at(sel4{1, 2, 1, 4}).typeText("(")
		e.assertText("a(b")
	})
	t.Run("a backwards selection becomes a forward one", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "hello")
		e.at(sel4{1, 5, 1, 2}).typeText("[")
		e.assertText("h[ell]o")
		e.assertSels(sel4{1, 3, 1, 6})
	})
	t.Run("lines", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "ab", "cd")
		e.at(sel4{1, 2, 2, 2}).typeText("{")
		e.assertText("a{b\nc}d")
		e.assertSels(sel4{1, 3, 2, 2})
	})
}

func TestAutoClosedTrackerForgetsPairsOnceTheCursorLeaves(t *testing.T) {
	e := acEd(t, autoClosingConfig(), "")
	e.typeText("(")
	if n := len(e.closed.Characters()); n != 1 {
		t.Fatalf("tracked = %d, want 1", n)
	}
	e.caret(1, 1)
	if n := len(e.closed.Characters()); n != 0 {
		t.Errorf("tracked = %d after the cursor left, want 0", n)
	}
}

func TestAutoClosingWithoutLanguageDoesNothing(t *testing.T) {
	e := newEd(t, DefaultOptions(), "")
	e.typeText("(")
	e.assertText("(")
}

func TestAutoClosedTracker(t *testing.T) {
	t.Run("follows the text", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "")
		e.typeText("(")
		e.caret(1, 1).typeText("ab") // before the pair: it moves, and the cursor left it
		if n := len(e.closed.Characters()); n != 0 {
			t.Errorf("tracked = %d", n)
		}
		e = acEd(t, autoClosingConfig(), "")
		e.typeText("(")
		e.typeText("ab")
		got := e.closed.Characters()
		if len(got) != 1 || got[0].Start != (buffer.Position{Col: 3}) || got[0].End != (buffer.Position{Col: 4}) {
			t.Errorf("tracked = %v, want the closing bracket at 3..4", got)
		}
	})
	t.Run("stops when the pair becomes several lines", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "")
		e.typeText("(")
		e.insert("\n")
		if n := len(e.closed.Characters()); n != 0 {
			t.Errorf("tracked = %d after a line break inside the pair", n)
		}
	})
	t.Run("stops when a cursor is outside", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "", "")
		e.typeText("(")
		e.at(sel4{1, 2, 1, 2}, sel4{2, 1, 2, 1})
		if n := len(e.closed.Characters()); n != 0 {
			t.Errorf("tracked = %d with a cursor outside the pair", n)
		}
	})
	t.Run("clear", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "")
		e.typeText("(")
		e.closed.Clear()
		if n := len(e.closed.Characters()); n != 0 || len(e.doc.AllDecorations()) != 0 {
			t.Errorf("tracked = %d, decorations = %d", n, len(e.doc.AllDecorations()))
		}
	})
	t.Run("an empty list adds nothing", func(t *testing.T) {
		e := acEd(t, autoClosingConfig(), "")
		e.closed.Add(nil)
		e.closed.Validate(e.sels)
		if len(e.doc.AllDecorations()) != 0 {
			t.Error("decorations were added")
		}
	})
}

func TestCursorsOnChange(t *testing.T) {
	cs := NewCursors(DefaultOptions())
	n := 0
	cs.SetOnChange(func() { n++ })
	cs.Set([]Cursor{NewCursor(buffer.Position{Col: 1})})
	cs.SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Col: 2})})
	cs.Add(NewCursor(buffer.Position{Col: 5}))
	cs.Replace(1, NewCursor(buffer.Position{Col: 6}))
	cs.Map(func(c Cursor) Cursor { return c })
	cs.CancelSecondary()
	if n != 6 {
		t.Errorf("onChange called %d times, want 6", n)
	}
	cs.SetOnChange(nil)
	cs.Set([]Cursor{NewCursor(buffer.Position{})})
	if n != 6 {
		t.Error("onChange called after it was removed")
	}
}

func TestAutoClosingWithTokens(t *testing.T) {
	t.Run("a quote begins a string or ends one", func(t *testing.T) {
		e := acEdTokens(t, autoClosingConfig(), "x = ")
		if got := e.typeAt(1, 5, "'"); got != "x = ''" {
			t.Errorf("line = %q", got)
		}
		e = acEdTokens(t, autoClosingConfig(), `x = "it"`)
		if got := e.typeAt(1, 8, "'"); got != `x = "it'"` {
			t.Errorf("a quote in a string: line = %q", got)
		}
	})
	t.Run("brackets in a string or a comment do not unbalance", func(t *testing.T) {
		cfg := &language.Configuration{
			Brackets:         []language.Pair{{Open: "{", Close: "}"}},
			AutoClosingPairs: []language.AutoClosingPair{pair("{", "}")},
		}
		for _, text := range [][]string{{"", "\"}\""}, {"", "\"}\"", "// }"}} {
			e := acEdTokens(t, cfg, text...)
			e.opts.UnmatchedClosing = unmatchedClosing(e)
			e.caret(1, 1).typeText("{")
			if got := e.line(1); got != "{}" {
				t.Errorf("%q: line = %q, want {}", text, got)
			}
		}
	})
	t.Run("not when the line is not cheap to tokenize", func(t *testing.T) {
		e := acEdTokens(t, autoClosingConfig(), "")
		e.opts.Tokens = slowSource{acSource{e}}
		e.typeText("(")
		e.assertLine(1, "(")
	})
}

// slowSource is a TokenSource whose lines are never cheap to tokenize.
type slowSource struct{ acSource }

func (slowSource) CheapToTokenize(int) bool { return false }
