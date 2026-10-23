package brackets

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

const goSample = `package main

func f(a []int) {
	s := "}" // )
	/* { */
	if len(a) > 0 {
		s += ` + "`)`" + `
	}
}
`

// goFixture makes a Go document with its tokenizer and an attached index.
func goFixture(t *testing.T, text string, large bool) (*document.Document, *tokens.Tokenizer, *Index) {
	t.Helper()
	reg, err := language.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	g, err := reg.NewGrammar("go", regex.NewCache(1<<14))
	if err != nil {
		t.Fatal(err)
	}
	opts := document.NewOptions()
	opts.LanguageID = "go"
	doc := document.New(document.FileURI("/sample.go"), text, opts)
	if large {
		doc = document.FromRead(document.FileURI("/big.go"), fileio.ReadResult{Text: text, Encoding: fileio.UTF8, LineEnding: fileio.LF, LargeFile: true}, opts)
	}
	tok := tokens.New(doc, g, tokens.Options{})
	t.Cleanup(tok.Close)
	ix, stop := Attach(doc, tok, NewRegistryLanguages(reg), reg.Code("go"))
	t.Cleanup(stop)
	return doc, tok, ix
}

func TestAttachedIndexIgnoresBracketsInStringsAndComments(t *testing.T) {
	doc, tok, ix := goFixture(t, goSample, false)
	tok.Settle()
	all := buffer.Range{End: buffer.Position{Line: doc.Snapshot().LineCount()}}
	var texts []string
	for _, b := range ix.Brackets(all, false) {
		texts = append(texts, b.Bracket.Text)
	}
	// Line 3: ( [ ] ) { ; line 6: ( ) {; line 8: } ; line 9: }
	want := []string{"(", "[", "]", ")", "{", "(", ")", "{", "}", "}"}
	if len(texts) != len(want) {
		t.Fatalf("brackets = %v, want %v", texts, want)
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Fatalf("brackets = %v, want %v", texts, want)
		}
	}
	if ps := ix.Pairs(all); len(ps) != 5 {
		t.Errorf("%d pairs", len(ps))
	}
	open, closing, ok := ix.Match(buffer.Position{Line: 2, Col: 16}) // the { after the parameters
	if !ok || open.Start != (buffer.Position{Line: 2, Col: 16}) || closing.Start != (buffer.Position{Line: 8, Col: 0}) {
		t.Errorf("Match = %v %v %v", open, closing, ok)
	}
	if !ix.Complete() {
		t.Error("the tokens are complete")
	}
}

func TestAttachedIndexFollowsEditsAndTokens(t *testing.T) {
	doc, tok, ix := goFixture(t, goSample, false)
	tok.Settle()
	all := buffer.Range{End: buffer.Position{Line: 100}}
	before := len(ix.Pairs(all))
	changed := -1
	ix.OnChange(func(from int) { changed = from })

	// Closing the comment early makes the brace after it code; the tokens of the lines below
	// change after the edit, and the index must follow them.
	line := "\t/* { */"
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: 4, Col: 3}, End: buffer.Position{Line: 4, Col: 4}}, Text: "*/ {"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	_ = line
	if changed != 4 {
		t.Errorf("listeners were told line %d", changed)
	}
	tok.Settle()
	if got := len(ix.Pairs(all)); got == before {
		t.Errorf("%d pairs before and after: the new brace was not seen", got)
	}
	// Undo brings it back.
	if _, err := doc.Undo(); err != nil {
		t.Fatal(err)
	}
	tok.Settle()
	if got := len(ix.Pairs(all)); got != before {
		t.Errorf("%d pairs after undo, want %d", got, before)
	}
	// Replacing the text starts again.
	doc.SetText("(")
	tok.Settle()
	if ps := ix.Pairs(all); len(ps) != 1 || ps[0].HasClose {
		t.Errorf("pairs = %+v", ps)
	}
}

func TestAttachedIndexOfALargeFileFindsNothing(t *testing.T) {
	_, _, ix := goFixture(t, goSample, true)
	if ix.Enabled() || len(ix.Pairs(buffer.Range{End: buffer.Position{Line: 100}})) != 0 {
		t.Error("a document in large file mode has no brackets")
	}
}

func TestRegistryLanguagesAreMadeOnce(t *testing.T) {
	reg, err := language.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	l := NewRegistryLanguages(reg)
	code := reg.Code("go")
	a, b := l.Language(code), l.Language(code)
	if a != b || a.Empty() || a.Lookup("{") == nil || a.Lookup("<") != nil {
		t.Errorf("language = %+v", a)
	}
	// A language with no configuration of brackets, and a code that is none, have no brackets.
	if !l.Language(0).Empty() || !l.Language(99999).Empty() {
		t.Error("an unknown code has no brackets")
	}
	if got := l.Language(reg.Code("json")).Lookup("{"); got == nil {
		t.Error("JSON has braces")
	}
	// HTML lists "colorizedBracketPairs" as empty: its brackets have no colors, and <!-- --> is a pair.
	html := l.Language(reg.Code("html"))
	if c, o := html.Lookup("-->"), html.Lookup("<!--"); c == nil || o == nil || !c.Closes(o) || c.ClosesColorized(o) || html.Lookup("}").ClosesColorized(html.Lookup("{")) {
		t.Error("HTML has the pair <!-- --> and no colorized pairs")
	}
}
