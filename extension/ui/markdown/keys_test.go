package markdown

import (
	"strings"
	"testing"
)

func TestBlockKeysIdentifyContentNotPlace(t *testing.T) {
	a := blockKeys(Parse("# Title\n\nfirst paragraph\n\n---\n\nsecond paragraph\n\n---\n"))
	// the same blocks after two lines were added above: the same keys
	b := blockKeys(Parse("added\n\nlines\n\n# Title\n\nfirst paragraph\n\n---\n\nsecond paragraph\n\n---\n"))
	if len(a) != 5 || len(b) != 7 {
		t.Fatalf("%d and %d blocks", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i+2] {
			t.Errorf("block %d: %v after the edit is %v", i, a[i], b[i+2])
		}
	}
	// the two rules have the same content and different keys
	if a[2].sum != a[4].sum || a[2] == a[4] || a[2].nth != 0 || a[4].nth != 1 {
		t.Errorf("the two rules: %v and %v", a[2], a[4])
	}
	seen := map[blockKey]bool{}
	for _, k := range a {
		if seen[k] {
			t.Errorf("key %v twice", k)
		}
		seen[k] = true
	}
}

func TestBlockKeysChangeWithWhatTheBlockShows(t *testing.T) {
	base := "text with **bold** and [a link](a.md) and `code` and ![alt](x.png)"
	variants := []string{
		base,
		strings.Replace(base, "bold", "bolder", 1),
		strings.Replace(base, "**bold**", "*bold*", 1),
		strings.Replace(base, "a.md", "b.md", 1),
		strings.Replace(base, "`code`", "`cod`", 1),
		strings.Replace(base, "x.png", "y.png", 1),
		strings.Replace(base, "alt", "alt2", 1),
		"# " + base,
		"## " + base,
		"> " + base,
		"- " + base,
		"1. " + base,
		"- [ ] " + base,
		"- [x] " + base,
		"```go\n" + base + "\n```",
		"```rust\n" + base + "\n```",
		"    " + base,
		"<div>" + base + "</div>\n",
		"| a | b |\n|---|---|\n| " + base + " | c |",
		"| a | b |\n|:--|---|\n| " + base + " | c |",
	}
	seen := map[blockKey]int{}
	for i, v := range variants {
		blocks := Parse(v)
		if len(blocks) != 1 {
			t.Fatalf("variant %d has %d blocks: %q", i, len(blocks), v)
		}
		k := blockKeys(blocks)[0]
		if j, ok := seen[k]; ok {
			t.Errorf("variants %d and %d share the key %v:\n%q\n%q", j, i, k, variants[j], v)
		}
		seen[k] = i
	}
	// the lines the block came from are not part of its key
	if blockKeys(Parse("a\n\n\n\nb"))[1] != blockKeys(Parse("a\n\nb"))[1] {
		t.Error("blank lines between blocks changed a key")
	}
}
