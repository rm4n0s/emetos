package emmet

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// maxFuzzInput bounds the abbreviations of the fuzz tests.
const maxFuzzInput = 300

// checkAbbreviation expands an abbreviation in every syntax and checks what must hold for any
// input: no panic, no hang (the number of nodes is bounded by the limit on repeaters), the tokens
// are in order and inside the text, and an error is one of this package.
func checkAbbreviation(t testing.TB, in string) {
	t.Helper()
	if len(in) > maxFuzzInput {
		return
	}

	tokens, err := Tokenize(in)
	if err != nil {
		if tag := errorTag(err); tag == "" {
			t.Fatalf("%q: the error of Tokenize is not of this package: %v", in, err)
		}
	} else {
		end := 0
		for _, tok := range tokens {
			if tok.Start < end || tok.End <= tok.Start || tok.End > len(in) {
				t.Fatalf("%q: token %+v is out of place (the last ended at %d)", in, tok, end)
			}
			end = tok.End
		}
		if end != len(in) {
			t.Fatalf("%q: the tokens end at %d of %d", in, end, len(in))
		}
	}

	for _, syntax := range []string{"html", "haml", "pug", "slim", "jsx", "xsl"} {
		cfg, cerr := NewConfig(syntax)
		if cerr != nil {
			t.Fatal(cerr)
		}
		cfg.MaxRepeat = 30
		cfg.Rand = fixedRand()
		cfg.Options.BEMEnabled = syntax == "html"
		cfg.Options.CommentEnabled = syntax == "html"
		cfg.Options.Field = SnippetField
		for _, text := range []*Text{nil, TextString("some text"), TextLines("a", "", "b\nc")} {
			cfg.Text = text
			out, err := Expand(in, cfg)
			if err != nil {
				if errorTag(err) == "" {
					t.Fatalf("%s %q: the error is not of this package: %v", syntax, in, err)
				}
				continue
			}
			if !utf8.ValidString(in) {
				continue
			}
			if len(out) > 1<<22 {
				t.Fatalf("%s %q: the output is %d bytes", syntax, in, len(out))
			}
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"div>p", "ul>li.item$*3", "a[href=x]{text}", "(a>b)*2+c^d", "p*", "li*{$#}", "!", "lorem10", "a.b#c[d=e f.]/", "..a",
		"[a={b}]", "a${1:x}", "a[b=${1:x}]", `a\>b`, "div{[}+a{}", "a*99999999999", "a$$$@-3*4", "a>(b>c)*3+d", "a>>", "((((", "))))", "{{{{",
		"[[[[", `a[b="c`, "a'", "ul>.item$*2", "label>input", "xsl:variable[select]>div", "Foo.Bar", "a.1/2", "ä>ö", "a b", "à[à=à]",
		strings.Repeat("a>", 40), strings.Repeat("(", 40) + "a" + strings.Repeat(")*2", 40),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		checkAbbreviation(t, in)
	})
}

// TestParseRandomAbbreviations runs the properties over many abbreviations made of the pieces of
// the syntax, so that the default test run covers what fuzzing finds.
func TestParseRandomAbbreviations(t *testing.T) {
	pieces := []string{
		"a", "div", "p", "ul", "li", "label", "input", "lorem3", ">", "+", "^", ".", "#", "/", "=", "*", "*3", "*2", "(", ")", "[", "]", "{", "}",
		"$", "$$", "$#", "@-", "@^", "${1}", "${1:x}", "${v}", "${", `"`, "'", " ", "\n", " ", `\`, "x", "1", "-", ":", "!", "_", "ä", "é", "日",
		"foo", "b.", "!c", "href", "class",
	}
	rng := rand.New(rand.NewPCG(3, 4))
	deadline := time.Now().Add(10 * time.Second)
	for i := range 2500 {
		var sb strings.Builder
		for range 1 + rng.IntN(16) {
			sb.WriteString(pieces[rng.IntN(len(pieces))])
		}
		checkAbbreviation(t, sb.String())
		if i%100 == 0 && time.Now().After(deadline) {
			t.Logf("stopped after %d abbreviations", i)
			return
		}
	}
}
