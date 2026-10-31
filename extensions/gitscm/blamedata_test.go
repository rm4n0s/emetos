package gitscm

import (
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// hashes makes the hashes of lines.
func hashes(lines ...string) []uint64 { return lineHashes(strings.Join(lines, "\n"), false) }

func TestMapLines(t *testing.T) {
	tests := []struct {
		name      string
		head, doc []string
		want      []int
	}{
		{"the same", []string{"a", "b", "c"}, []string{"a", "b", "c"}, []int{0, 1, 2}},
		{"a line added at the start", []string{"a", "b"}, []string{"x", "a", "b"}, []int{-1, 0, 1}},
		{"a line added in the middle", []string{"a", "b", "c"}, []string{"a", "x", "b", "c"}, []int{0, -1, 1, 2}},
		{"a line added at the end", []string{"a", "b"}, []string{"a", "b", "x"}, []int{0, 1, -1}},
		{"a line deleted", []string{"a", "b", "c"}, []string{"a", "c"}, []int{0, 2}},
		{"a line changed", []string{"a", "b", "c"}, []string{"a", "B", "c"}, []int{0, -1, 2}},
		{"two blocks", []string{"a", "b", "c", "d", "e", "f"}, []string{"a", "X", "c", "d", "Y", "Z", "f"}, []int{0, -1, 2, 3, -1, -1, 5}},
		// a moved block is a common subsequence either way; Myers keeps the one that starts later in the version
		{"a block moved", []string{"a", "b", "c", "d"}, []string{"c", "d", "a", "b"}, []int{2, 3, -1, -1}},
		{"the whole text replaced", []string{"a", "b"}, []string{"x", "y", "z"}, []int{-1, -1, -1}},
		{"an empty document", []string{"a"}, []string{""}, []int{-1}},
		{"the empty last line after a line break", []string{"a", ""}, []string{"a", "b", ""}, []int{0, -1, 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := mapLines(hashes(tc.head...), hashes(tc.doc...))
			if !ok || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v (%v), want %v", got, ok, tc.want)
			}
		})
	}
}

func TestMapLinesGivesUpWhenTheDocumentIsAnotherFile(t *testing.T) {
	var head, doc []string
	for i := range 2000 {
		head = append(head, "head "+strconv.Itoa(i))
		doc = append(doc, "doc "+strconv.Itoa(i))
	}
	if _, ok := mapLines(hashes(head...), hashes(doc...)); ok {
		t.Error("two files with nothing in common were mapped")
	}
	// a few changed lines in a big file are no trouble
	big := make([]string, 50000)
	for i := range big {
		big[i] = "line " + strconv.Itoa(i)
	}
	edited := append([]string(nil), big...)
	edited[25000], edited[100] = "changed", "changed too"
	got, ok := mapLines(hashes(big...), hashes(edited...))
	if !ok || got[25000] != -1 || got[100] != -1 || got[101] != 101 || got[49999] != 49999 {
		t.Errorf("a big file with two changed lines: ok %v, %v %v %v", ok, got[25000], got[101], got[49999])
	}
}

// TestMapLinesKeepsEveryUnchangedLine edits a text at random places, with lines that are all different, and checks that the lines that were not
// touched map to where they were and the new ones to nothing (SPEC §10.30 "Blame": unchanged lines keep their commits).
func TestMapLinesKeepsEveryUnchangedLine(t *testing.T) {
	for seed := int64(1); seed <= 200; seed++ {
		rng := rand.New(rand.NewSource(seed))
		n := 1 + rng.Intn(60)
		type line struct {
			text string
			orig int // the line of the version it came from, -1 for a new one
		}
		var head []string
		var doc []line
		for i := range n {
			head = append(head, "original "+strconv.Itoa(i))
			doc = append(doc, line{head[i], i})
		}
		next := 0
		for range rng.Intn(25) {
			switch op := rng.Intn(3); {
			case op == 0 || len(doc) == 0: // insert 1 to 3 lines
				at := rng.Intn(len(doc) + 1)
				var ins []line
				for range 1 + rng.Intn(3) {
					next++
					ins = append(ins, line{"new " + strconv.Itoa(next), -1})
				}
				doc = append(doc[:at], append(ins, doc[at:]...)...)
			case op == 1: // delete 1 to 3 lines
				at := rng.Intn(len(doc))
				to := min(at+1+rng.Intn(3), len(doc))
				doc = append(doc[:at], doc[to:]...)
			default: // change a line
				at := rng.Intn(len(doc))
				next++
				doc[at] = line{"changed " + strconv.Itoa(next), -1}
			}
		}
		var texts []string
		for _, l := range doc {
			texts = append(texts, l.text)
		}
		got, ok := mapLines(hashes(head...), hashes(texts...))
		if !ok {
			t.Fatalf("seed %d: the mapping gave up", seed)
		}
		for i, l := range doc {
			if got[i] != l.orig {
				t.Fatalf("seed %d: line %d (%q) maps to %d, want %d\nhead %v\ndoc %v", seed, i, l.text, got[i], l.orig, head, texts)
			}
		}
	}
}

func TestLineHashesIgnoreWhitespaceOnlyWhenAsked(t *testing.T) {
	a, b := "\tfoo(1, 2)\r\nbar\n", "  foo( 1,2 )\nbar\n"
	if reflect.DeepEqual(lineHashes(a, false), lineHashes(b, false)) {
		t.Error("lines that differ in white space are the same")
	}
	if !reflect.DeepEqual(lineHashes(a, true), lineHashes(b, true)) {
		t.Error("lines that differ in white space only are not the same with -w")
	}
	// a \r before a line break is not a part of the line
	if !reflect.DeepEqual(lineHashes("a\r\nb", false), lineHashes("a\nb", false)) {
		t.Error("a CRLF line differs from an LF line")
	}
}

func TestTheBlameIndexNamesTheCommitOfEachLine(t *testing.T) {
	commits := []git.BlameCommit{
		{Hash: "a", Author: "Ada", Ranges: []git.LineRange{{Start: 1, End: 2}, {Start: 5, End: 5}}},
		{Hash: "b", Author: "Grace", Ranges: []git.LineRange{{Start: 3, End: 4}}},
	}
	idx := newBlameIndex(commits, "1\n2\n3\n4\n5\n", false)
	var got []string
	for l := range 7 {
		if c, ok := idx.commitOf(l); ok {
			got = append(got, c.Hash)
		} else {
			got = append(got, "-")
		}
	}
	// five lines and the empty text after the last line break, which no commit made; line 6 does not exist
	if want := []string{"a", "a", "b", "b", "a", "-", "-"}; !reflect.DeepEqual(got, want) {
		t.Errorf("commits %v, want %v", got, want)
	}
	if len(idx.hashes) != 6 {
		t.Errorf("%d line hashes", len(idx.hashes))
	}
}

func TestFormatBlame(t *testing.T) {
	v := blameValues{hash: "0123456789abcdef", hashShort: "0123456", subject: "Fix é and 日本語", authorName: "Zoë Müller", authorEmail: "z@example.com", authorDate: "1/2/2006, 3:04:05 PM", authorDateAgo: "3 days ago"}
	tests := []struct {
		template, want string
	}{
		{"${subject}, ${authorName} (${authorDateAgo})", "Fix é and 日本語, Zoë Müller (3 days ago)"},
		{"${authorName} (${authorDateAgo})", "Zoë Müller (3 days ago)"},
		{"${hash} ${hashShort} ${authorEmail} ${authorDate}", "0123456789abcdef 0123456 z@example.com 1/2/2006, 3:04:05 PM"},
		{"${unknown} and ${subject}", "${unknown} and Fix é and 日本語"},
		{"${} ${hash", "${} ${hash"},
		{"no variables", "no variables"},
		{"${subject}${subject}", "Fix é and 日本語Fix é and 日本語"},
		{"$${subject}", "$Fix é and 日本語"},
		{"${Subject}", "${Subject}"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := formatBlame(tc.template, v); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.template, got, tc.want)
		}
	}
}

func TestTruncateCountsCharactersAndAddsAnEllipsis(t *testing.T) {
	long := strings.Repeat("é", 60)
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 50, "short"},
		{strings.Repeat("a", 50), 50, strings.Repeat("a", 50)},
		{strings.Repeat("a", 51), 50, strings.Repeat("a", 50) + "…"},
		{long, 50, strings.Repeat("é", 50) + "…"},
		{"日本語日本語", 3, "日本語…"},
		{"", 3, ""},
	}
	for _, tc := range tests {
		if got := truncate(tc.in, tc.n); got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
	if shortHash("0123456789", 7) != "0123456" || shortHash("012", 7) != "012" || shortHash("0123", 0) != "0123" {
		t.Error("shortHash")
	}
}

func TestFromNow(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	sec := time.Second
	tests := []struct {
		date time.Time
		want string
	}{
		{ago(0), "now"},
		{ago(29 * sec), "now"},
		{ago(30 * sec), "30 seconds ago"},
		{ago(59 * sec), "59 seconds ago"},
		{ago(60 * sec), "1 minute ago"},
		{ago(119 * sec), "1 minute ago"},
		{ago(120 * sec), "2 minutes ago"},
		{ago(59*time.Minute + 59*sec), "59 minutes ago"},
		{ago(time.Hour), "1 hour ago"},
		{ago(23 * time.Hour), "23 hours ago"},
		{ago(24 * time.Hour), "1 day ago"},
		{ago(6 * 24 * time.Hour), "6 days ago"},
		{ago(7 * 24 * time.Hour), "1 week ago"},
		{ago(29 * 24 * time.Hour), "4 weeks ago"},
		{ago(30 * 24 * time.Hour), "1 month ago"},
		{ago(364 * 24 * time.Hour), "12 months ago"},
		{ago(365 * 24 * time.Hour), "1 year ago"},
		{ago(800 * 24 * time.Hour), "2 years ago"},
		{now.Add(10 * sec), "now"},
		{now.Add(2 * time.Hour), "in 2 hours"},
	}
	for _, tc := range tests {
		if got := fromNow(tc.date, now, true); got != tc.want {
			t.Errorf("%v before now: %q, want %q", now.Sub(tc.date), got, tc.want)
		}
	}
}

func TestHoverTextEscapesWhatMarkdownReads(t *testing.T) {
	if got := escapeMarkdown("Ada [the *great*] (1815)"); got != `Ada \[the \*great\*\] \(1815\)` {
		t.Errorf("escaped: %q", got)
	}
	link := commandLink("Open Commit", "git.viewCommit", `Open "it"`, map[string]any{"id": "abc"})
	if want := `[Open Commit](command:git.viewCommit?%5B%7B%22id%22%3A%22abc%22%7D%5D "Open 'it'")`; link != want {
		t.Errorf("link %q, want %q", link, want)
	}
	if got := encodeURIComponent("a b+c"); got != "a%20b%2Bc" {
		t.Errorf("encodeURIComponent: %q", got)
	}
}
