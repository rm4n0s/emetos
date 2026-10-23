package tokens

import (
	"context"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/textmate"
)

// grammarFor makes the grammar of a bundled language.
func grammarFor(t testing.TB, id string) *textmate.Grammar {
	t.Helper()
	reg, err := language.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	g, err := reg.NewGrammar(id, regex.NewCache(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

const goSample = `package main

import "fmt"

/* a comment
   over lines */
func main() {
	s := ` + "`raw\nstring`" + `
	fmt.Println("hi", s) // done
}
`

func newDoc(text string) *document.Document {
	return document.New(document.FileURI("/test.go"), text, document.NewOptions())
}

// describe writes the tokens of a line as text|scopes, for comparing and for failures.
func describe(scopes *Scopes, line string, tokens []Token) string {
	var sb strings.Builder
	start := 0
	for _, tok := range tokens {
		sb.WriteString(line[start:tok.End])
		sb.WriteByte('|')
		sb.WriteString(strings.Join(scopes.Names(tok.Scope), " "))
		sb.WriteByte('|')
		sb.WriteString(string(rune('0' + tok.Type)))
		sb.WriteByte('\n')
		start = int(tok.End)
	}
	return sb.String()
}

// fresh tokenizes the text from scratch, with a tokenizer of its own, and describes every line.
func fresh(t testing.TB, g *textmate.Grammar, text string) []string {
	t.Helper()
	tok := New(newDoc(text), g, Options{})
	defer tok.Close()
	tok.Settle()
	return dump(t, tok, text)
}

// dump describes every line of a tokenizer that has settled.
func dump(t testing.TB, tok *Tokenizer, text string) []string {
	t.Helper()
	lines := strings.Split(text, "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		tokens := tok.Line(i)
		if tokens == nil {
			t.Fatalf("line %d has no tokens", i)
		}
		out[i] = describe(tok.Scopes(), line, tokens)
	}
	return out
}

func TestTokenizesADocument(t *testing.T) {
	g := grammarFor(t, "go")
	doc := newDoc(goSample)
	tok := New(doc, g, Options{})
	defer tok.Close()
	if !tok.Active() {
		t.Fatal("a document with a grammar is tokenized")
	}
	if tok.Line(0) != nil {
		t.Error("nothing is tokenized before the loop or Ensure runs")
	}
	tok.Settle()
	lines := strings.Split(goSample, "\n")
	// Every line equals what the grammar gives when the lines are tokenized one after the other.
	var state *textmate.State
	for i, line := range lines {
		res := g.TokenizeLine(line, state, 0)
		state = res.State
		var want strings.Builder
		start := 0
		for _, tok2 := range res.Tokens {
			want.WriteString(line[start:tok2.End])
			want.WriteByte('|')
			want.WriteString(strings.Join(tok2.Scopes.Names(), " "))
			want.WriteByte('|')
			want.WriteString(string(rune('0' + tok2.Type)))
			want.WriteByte('\n')
			start = tok2.End
		}
		if got := describe(tok.Scopes(), line, tok.Line(i)); got != want.String() {
			t.Errorf("line %d:\n got %q\nwant %q", i, got, want.String())
		}
	}
	// The comment that goes over two lines is a comment on the second line too.
	if got := describe(tok.Scopes(), lines[5], tok.Line(5)); !strings.Contains(got, "comment.block.go") || !strings.Contains(got, "|1\n") {
		t.Errorf("the second line of the comment: %q", got)
	}
	if tok.Slices() == 0 {
		t.Error("the slices are counted")
	}
}

func TestEnsureTokenizesOnTheCaller(t *testing.T) {
	var text strings.Builder
	for range 400 {
		text.WriteString("func f() { return }\n")
	}
	tok := New(newDoc(text.String()), grammarFor(t, "go"), Options{})
	defer tok.Close()
	var changed [][2]int
	tok.OnChange(func(from, to int) { changed = append(changed, [2]int{from, to}) })

	if !tok.Ensure(10, time.Second) {
		t.Fatal("ten lines are tokenized within a second")
	}
	if tok.Line(10) == nil || tok.Line(11) != nil {
		t.Errorf("Ensure goes up to the line and no further: %v %v", tok.Line(10) != nil, tok.Line(11) != nil)
	}
	if len(changed) != 1 || changed[0] != [2]int{0, 10} {
		t.Errorf("the listeners are told: %v", changed)
	}
	if !tok.Ensure(5, time.Second) {
		t.Error("a line that is done is ensured")
	}
	// A budget that runs out leaves the line for the background loop.
	if tok.Ensure(399, time.Nanosecond) {
		t.Error("a budget of a nanosecond cannot tokenize 390 lines")
	}
	if tok.Line(399) != nil {
		t.Error("line 399 is not tokenized")
	}
	tok.Settle()
	if tok.Line(399) == nil {
		t.Error("the background tokenizes the rest")
	}
	for _, line := range []int{-1, 401} {
		if tok.Ensure(line, time.Second) || tok.Line(line) != nil {
			t.Errorf("line %d does not exist", line)
		}
	}
}

func TestNoTokensWithoutAGrammarOrInLargeFileMode(t *testing.T) {
	tok := New(newDoc("package x\n"), nil, Options{})
	defer tok.Close()
	if tok.Active() || tok.Ensure(0, time.Second) || tok.Line(0) != nil {
		t.Error("no grammar, no tokens")
	}
	tok.Settle() // nothing to do
	tok.SetGrammar(grammarFor(t, "go"))
	if !tok.Active() || !tok.Ensure(0, time.Second) {
		t.Error("a grammar given later is used")
	}

	big := newDoc("package x\n")
	large := document.FromRead(document.FileURI("/big.go"), readResult("package x\n", true), document.NewOptions())
	_ = big
	tok2 := New(large, grammarFor(t, "go"), Options{})
	defer tok2.Close()
	if tok2.Active() || tok2.Ensure(0, time.Second) {
		t.Error("a document in large file mode is not tokenized")
	}
}

func TestChangingTheGrammarTokenizesAgain(t *testing.T) {
	tok := New(newDoc("# title\n\nfunc f() {}\n"), grammarFor(t, "markdown"), Options{})
	defer tok.Close()
	tok.Settle()
	markdown := dump(t, tok, "# title\n\nfunc f() {}\n")
	var notified int
	tok.OnChange(func(from, to int) { notified += to - from + 1 })
	tok.SetGrammar(grammarFor(t, "go"))
	if tok.Line(0) != nil {
		t.Error("the old tokens are gone")
	}
	if notified != 4 {
		t.Errorf("all %d lines are told to have changed: %d", 4, notified)
	}
	tok.Settle()
	golang := dump(t, tok, "# title\n\nfunc f() {}\n")
	if golang[0] == markdown[0] {
		t.Errorf("a heading is not Go: %q", golang[0])
	}
	if want := fresh(t, grammarFor(t, "go"), "# title\n\nfunc f() {}\n"); strings.Join(golang, "") != strings.Join(want, "") {
		t.Error("the tokens after the change equal a fresh tokenization")
	}
}

func TestEditsInvalidateFromTheFirstEditedLine(t *testing.T) {
	text := "package main\n\nfunc a() {}\nfunc b() {}\n"
	doc := newDoc(text)
	tok := New(doc, grammarFor(t, "go"), Options{})
	defer tok.Close()
	tok.Settle()
	// An edit in the middle of a line leaves the lines above and below as they were.
	before := [4][]Token{tok.Line(0), tok.Line(1), tok.Line(2), tok.Line(3)}
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: 2, Col: 5}, End: buffer.Position{Line: 2, Col: 6}}, Text: "zz"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if tok.Line(2) != nil {
		t.Error("the edited line has no tokens until it is tokenized again")
	}
	if tok.Line(3) == nil || &tok.Line(3)[0] != &before[3][0] || tok.Line(0) == nil {
		t.Error("the other lines keep their tokens")
	}
	tok.Settle()
	// The end state of the edited line did not change, so the line below was not tokenized again.
	if &tok.Line(3)[0] != &before[3][0] {
		t.Error("the line below was tokenized again although the state at the end of the edited line is the same")
	}
	// Opening a comment changes the state, and the lines below are tokenized again.
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: 2, Col: 0}, End: buffer.Position{Line: 2, Col: 0}}, Text: "/*"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	tok.Settle()
	if got := describe(tok.Scopes(), "func b() {}", tok.Line(3)); !strings.Contains(got, "comment.block.go") {
		t.Errorf("the line below is in the comment now: %q", got)
	}
}

func TestStaleResultsAreDropped(t *testing.T) {
	text := "package main\n\nfunc a() {}\n"
	doc := newDoc(text)
	tok := New(doc, grammarFor(t, "go"), Options{})
	defer tok.Close()
	edits := 0
	tok.beforeCommit = func() {
		if edits == 0 {
			edits++
			// The document changes while the loop has a line in hand.
			if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: 0, Col: 8}, End: buffer.Position{Line: 0, Col: 12}}, Text: "other"}}, document.EditOptions{}); err != nil {
				t.Error(err)
			}
		}
	}
	if more := tok.slice(); !more {
		t.Error("a stale slice leaves work")
	}
	if tok.Line(0) != nil {
		t.Error("the result of the stale slice was kept")
	}
	tok.beforeCommit = nil
	tok.Settle()
	want := fresh(t, grammarFor(t, "go"), doc.Text())
	if got := dump(t, tok, doc.Text()); strings.Join(got, "") != strings.Join(want, "") {
		t.Errorf("after the edit the tokens are those of the new text:\n%v\n%v", got, want)
	}
}

func TestTheLoopRunsInSlicesAndStops(t *testing.T) {
	var text strings.Builder
	for range 300 {
		text.WriteString("func f() { return 1 + 2 }\n")
	}
	tok := New(newDoc(text.String()), grammarFor(t, "go"), Options{Slice: time.Microsecond})
	defer tok.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tok.Run(ctx); close(done) }()
	deadline := time.After(10 * time.Second)
	for tok.Line(299) == nil {
		select {
		case <-deadline:
			t.Fatal("the loop did not tokenize the document")
		case <-time.After(time.Millisecond):
		}
	}
	if tok.Slices() < 3 {
		t.Errorf("a slice of a microsecond cannot do 300 lines: %d slices", tok.Slices())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop")
	}
}

func TestTheLoopTokenizesAfterAnEdit(t *testing.T) {
	doc := newDoc("package main\n")
	tok := New(doc, grammarFor(t, "go"), Options{})
	defer tok.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); tok.Run(ctx) }()
	defer func() { cancel(); wg.Wait() }()
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !ok(); {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitFor("the first tokens", func() bool { return tok.Line(0) != nil })
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: 0, Col: 12}, End: buffer.Position{Line: 0, Col: 12}}, Text: "\nvar x = 1"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor("the tokens of the new line", func() bool { return tok.Line(1) != nil })
	if got := describe(tok.Scopes(), "var x = 1", tok.Line(1)); !strings.Contains(got, "keyword.var.go") {
		t.Errorf("var is a keyword: %q", got)
	}
}

// edit is a random edit of the text, as a document edit and as the text it makes.
func randomEdit(rng *rand.Rand, doc *document.Document) document.Edit {
	snap := doc.Snapshot()
	pick := func() buffer.Position {
		line := rng.Intn(snap.LineCount())
		n, _ := snap.LineLength(line)
		return buffer.Position{Line: line, Col: rng.Intn(n + 1)}
	}
	a, b := pick(), pick()
	if b.IsBefore(a) {
		a, b = b, a
	}
	if rng.Intn(3) != 0 && a.Line == b.Line {
		// Mostly small edits inside a line.
		b.Col = min(b.Col, a.Col+rng.Intn(4))
	}
	snippets := []string{"", "x", " ", "\n", "/*", "*/", "\"", "`", "//", "{", "}", "func f() {\n", "\n\t", "return 1", "/* c */"}
	return document.Edit{Range: buffer.Range{Start: a, End: b}, Text: snippets[rng.Intn(len(snippets))]}
}

// Tokens after fuzzed edits equal a full re-tokenization (the check that goes with SPEC §8.3: an
// edit invalidates from its first line and the end states say where the tokens are right again).
func TestTokensAfterEditsEqualAFullTokenization(t *testing.T) {
	cases := []struct {
		language string
		text     string
		seeds    int
	}{
		{"go", goSample, 30},
		{"markdown", "# Title\n\n```go\nfunc f() {}\n```\n\n* item\n* item\n\n> quote\n", 30},
		{"json", "{\n\t\"a\": [1, 2, {\"b\": null}],\n\t// comment\n\t\"c\": \"x\"\n}\n", 20},
	}
	for _, tc := range cases {
		g := grammarFor(t, tc.language)
		for seed := range tc.seeds {
			rng := rand.New(rand.NewSource(int64(seed)))
			doc := newDoc(tc.text)
			tok := New(doc, g, Options{})
			tok.Settle()
			for step := range 25 {
				if _, err := doc.Edit([]document.Edit{randomEdit(rng, doc)}, document.EditOptions{}); err != nil {
					t.Fatal(err)
				}
				// Sometimes undo, which is another kind of change.
				if rng.Intn(6) == 0 {
					if _, err := doc.Undo(); err != nil {
						t.Fatal(err)
					}
				}
				if rng.Intn(2) == 0 {
					tok.Settle()
				} else {
					tok.Ensure(rng.Intn(doc.Snapshot().LineCount()), time.Second)
				}
				if step%5 == 4 || rng.Intn(4) == 0 {
					tok.Settle()
					got, want := dump(t, tok, doc.Text()), fresh(t, g, doc.Text())
					for i := range want {
						if got[i] != want[i] {
							t.Fatalf("%s seed %d step %d line %d of %q:\n got %q\nwant %q", tc.language, seed, step, i, doc.Text(), got[i], want[i])
						}
					}
				}
			}
			tok.Close()
		}
	}
}

func TestSetTextTokenizesAgain(t *testing.T) {
	g := grammarFor(t, "go")
	doc := newDoc("package a\n")
	tok := New(doc, g, Options{})
	defer tok.Close()
	tok.Settle()
	doc.SetText("package b\n\nvar x = 1\n")
	if tok.Line(0) != nil {
		t.Error("the new text has no tokens yet")
	}
	tok.Settle()
	got, want := dump(t, tok, doc.Text()), fresh(t, g, doc.Text())
	if strings.Join(got, "") != strings.Join(want, "") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScopesAreInterned(t *testing.T) {
	g := grammarFor(t, "go")
	var stacks []*textmate.ScopeStack
	var state *textmate.State
	for _, line := range []string{"package main", "var a = 1", "var b = 2"} {
		res := g.TokenizeLine(line, state, 0)
		state = res.State
		for _, tok := range res.Tokens {
			stacks = append(stacks, tok.Scopes)
		}
	}
	s := NewScopes()
	if s.ID(nil) != NoScope || s.Names(NoScope) != nil || s.Len() != 1 {
		t.Error("the empty stack is NoScope")
	}
	ids := map[string]ScopeID{}
	for _, stack := range stacks {
		id := s.ID(stack)
		key := strings.Join(stack.Names(), " ")
		if prev, ok := ids[key]; ok && prev != id {
			t.Errorf("%q has two ids: %d and %d", key, prev, id)
		}
		ids[key] = id
		if got := strings.Join(s.Names(id), " "); got != key {
			t.Errorf("names of %d: %q, want %q", id, got, key)
		}
	}
	if s.Names(ScopeID(9999)) != nil || s.Names(-1) != nil {
		t.Error("an unknown id has no names")
	}
	// Equal stacks made apart have one id.
	first := s.ID(stacks[0])
	if again := s.ID(stacks[0]); again != first {
		t.Error("an id is stable")
	}
}
