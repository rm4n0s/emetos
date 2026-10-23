package snippet

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// checkTree checks what must hold for the tree of every snippet, valid or not: the parent of each
// marker is the marker that holds it and no two texts are side by side.
func checkTree(t testing.TB, in string, m Marker) {
	t.Helper()
	prevText := false
	for _, c := range m.Children() {
		if c.Parent() != m {
			t.Fatalf("%q: a %s has the wrong parent", in, kind(c))
		}
		_, isText := c.(*Text)
		if isText && prevText {
			t.Fatalf("%q: two texts side by side in a %s", in, kind(m))
		}
		prevText = isText
		checkTree(t, in, c)
	}
	if ch, ok := m.(*Choice); ok {
		for _, o := range ch.Options {
			if o.Parent() != Marker(ch) {
				t.Fatalf("%q: an option has the wrong parent", in)
			}
		}
	}
}

// checkSnippet parses an input, resolves its variables and checks the properties of the result.
func checkSnippet(t testing.TB, in string) {
	t.Helper()
	first := NewParser().Parse(in, ParseOpts{InsertFinalTabstop: true})
	checkTree(t, in, first)
	// the serialized snippet reads back to a tree that is right too (it need not read as the same
	// text: like VS Code's, "$1" before a digit is written $1 and read as $10)
	checkTree(t, in, NewParser().Parse(first.ToTextmateString()))
	for _, ph := range first.Placeholders() {
		if first.Offset(ph) < 0 {
			t.Fatalf("%q: a placeholder has no offset", in)
		}
	}
	first.ResolveVariables(nameResolver("x"))
	checkTree(t, in, first)
	_ = first.Clone().ToTextmateString()
}

func FuzzParse(f *testing.F) {
	for _, tc := range textAndMarkers {
		f.Add(tc.in)
	}
	f.Add(`${1/(void$)|(.+)/${1:?-return nil;}/}`)
	f.Add(`${TM_FILENAME/^(.)|(?:-(.))|(\.js)/${1:/upcase}${2:/upcase}/g}`)
	f.Add("${1:${2:${3:$1}}} $2 ${3/x/y/}")
	f.Fuzz(func(t *testing.T, in string) {
		checkSnippet(t, in)
	})
}

// TestParseRandomSnippets runs the properties over many snippets made of the pieces of the syntax,
// so that the default test run covers what fuzzing finds.
func TestParseRandomSnippets(t *testing.T) {
	pieces := []string{
		"$", "${", "}", "{", ":", "|", ",", "/", `\`, "+", "-", "?", "1", "2", "0", "10", "foo", "TM_FILENAME", "_", " ",
		"a", "é", "日", "\n", "i", "g", "(", ")", "[", "]", "*", "$1", "${1:", "${2|a,b|}", "${v/(a)/${1:/upcase}/g}", "$0",
	}
	rng := rand.New(rand.NewPCG(1, 2))
	deadline := time.Now().Add(20 * time.Second)
	for i := range 30000 {
		var sb strings.Builder
		for range 1 + rng.IntN(14) {
			sb.WriteString(pieces[rng.IntN(len(pieces))])
		}
		checkSnippet(t, sb.String())
		if i%1000 == 0 && time.Now().After(deadline) {
			t.Fatalf("too slow: %d snippets in 20 s", i)
		}
	}
}
