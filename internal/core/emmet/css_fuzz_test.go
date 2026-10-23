package emmet

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// The properties that must hold for any input of the stylesheet parser, of the extraction and of
// ExpandAt: no panic and no hang, the tokens are in order and inside the text, and an error is one
// of this package. The inputs are bounded.

// checkCSSAbbreviation expands a stylesheet abbreviation in every syntax and scope.
func checkCSSAbbreviation(t testing.TB, in string) {
	t.Helper()
	if len(in) > maxFuzzInput {
		return
	}
	for _, value := range []bool{false, true} {
		tokens, err := TokenizeCSS(in, value)
		if err != nil {
			if errorTag(err) == "" {
				t.Fatalf("%q: the error of TokenizeCSS is not of this package: %v", in, err)
			}
			continue
		}
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

	for _, syntax := range []string{"css", "stylus"} {
		cfg, err := NewConfig(syntax)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Options.Field = SnippetField
		cfg.Options.Text = SnippetText
		for _, ctx := range []*Context{nil, {Name: ScopeSection}, {Name: ScopeProperty}, {Name: ScopeValue}, {Name: "margin"}} {
			cfg.Context = ctx
			for _, json := range []bool{false, true} {
				if json && ctx != nil {
					continue
				}
				cfg.Options.StylesheetJSON = json
				cfg.Options.StylesheetStrictMatch = json
				out, err := Expand(in, cfg)
				if err != nil {
					if errorTag(err) == "" {
						t.Fatalf("%s %q: the error is not of this package: %v", syntax, in, err)
					}
					continue
				}
				if utf8.ValidString(in) && !utf8.ValidString(out) {
					t.Fatalf("%s %q: the output is not valid text: %q", syntax, in, out)
				}
				if len(out) > 1<<20 {
					t.Fatalf("%s %q: the output is %d bytes", syntax, in, len(out))
				}
			}
		}
	}
}

// checkExtraction checks the properties of the extraction and of the expansion at a place.
func checkExtraction(t testing.TB, in string) {
	t.Helper()
	if len(in) > 2*maxFuzzInput {
		return
	}
	for _, typ := range []SyntaxType{TypeMarkup, TypeStylesheet} {
		for _, look := range []bool{false, true} {
			for _, prefix := range []string{"", "<", ">>"} {
				for _, pos := range []int{0, len(in) / 2, len(in), len(in) + 5} {
					res, ok := ExtractAbbreviation(in, pos, ExtractOptions{LookAhead: look, Type: typ, Prefix: prefix})
					if !ok {
						continue
					}
					if res.Location < 0 || res.Location > res.End || res.End > len(in) || res.Start > res.Location ||
						res.Abbreviation != in[res.Location:res.End] {
						t.Fatalf("%q pos %d: %+v is out of place", in, pos, res)
					}
				}
			}
		}
	}
	for _, syntax := range []string{"html", "jsx", "pug", "css", "stylus"} {
		res, ok, err := ExpandAt(ExpandRequest{Syntax: syntax, TextBefore: in})
		if err != nil {
			if errorTag(err) == "" {
				t.Fatalf("%s %q: the error is not of this package: %v", syntax, in, err)
			}
			continue
		}
		if ok && (res.Start < 0 || res.Start > res.End || res.End != len(in)) {
			t.Fatalf("%s %q: the range of %+v is out of place", syntax, in, res)
		}
		_, _, err = ExpandAt(ExpandRequest{Syntax: syntax, Selection: in})
		if err != nil && errorTag(err) == "" {
			t.Fatalf("%s %q: the error of a selection is not of this package: %v", syntax, in, err)
		}
		IsAbbreviationValid(syntax, in)
	}
}

func FuzzParseCSS(f *testing.F) {
	for _, seed := range []string{
		"p10", "bd1-s#fc0", "c#f.5!", "lg(to right, #0, #f00.5)", "trf-s(2, 3)", "@k-name10", "@", "m0-a", "!", "p+m", "foo$bar@bam",
		"--foo-bar", "w${1:x}", "w${", "w${1:{x", "((", "))", "a(b(c(", `"abc`, "'a'", "-", "--", "#", "#t", "#.", ".", "1.", "-.5",
		"p10 ", "ä", "p%", "gt", "c#${fff}", "scale3d(1,2,3)", "m,a", "p,,", strings.Repeat("a(", 40), strings.Repeat("p10+", 40),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		checkCSSAbbreviation(t, in)
	})
}

func FuzzExtract(f *testing.F) {
	for _, seed := range []string{
		"ul>li", "<foo>bar[a b=\"c\"]>baz", "a[b]", "div{[}+a{}", "div{}}", "foo{bar}", "p10 (m[0)", "a) b", "((a", "'", "\"", "<div", "x >>> y",
		"a[href=\"x\"] b", "{{{", "}}}", "]]]", "p10!+m10", ".5", "!!!", "é", "a\tb ", strings.Repeat("{", 50) + strings.Repeat("}", 50),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		checkExtraction(t, in)
	})
}

// TestRandomStylesheetAbbreviations runs the properties over many inputs made of the pieces of the
// syntax, so that the default test run covers what fuzzing finds.
func TestRandomStylesheetAbbreviations(t *testing.T) {
	pieces := []string{
		"p", "m", "bd", "c", "bg", "trf", "lg", "dib", "fz", "@", "@k", "10", "0", ".5", "1.", "-", "--", "+", "!", ":", ",", "(", ")", " ", "\n",
		"#", "#f", "#fc0", "#t", ".5", "%", "px", "r", "$", "$x", "${1}", "${1:x}", "${v}", "${", `"`, "'", "é", "[", "]", "{", "}", "/", "_", "foo",
	}
	rng := rand.New(rand.NewPCG(5, 6))
	deadline := time.Now().Add(10 * time.Second)
	for i := range 800 {
		var sb strings.Builder
		for range 1 + rng.IntN(16) {
			sb.WriteString(pieces[rng.IntN(len(pieces))])
		}
		in := sb.String()
		checkCSSAbbreviation(t, in)
		checkExtraction(t, in)
		if i%100 == 0 && time.Now().After(deadline) {
			t.Logf("stopped after %d abbreviations", i)
			return
		}
	}
}
