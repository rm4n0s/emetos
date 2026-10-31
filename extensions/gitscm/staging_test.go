package gitscm

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// applyLineChanges against a model of its own meaning and against git apply --cached (step 24.12). VS Code has no unit test for staging.ts's
// applyLineChanges (its test of the staging commands needs the extension host): 0 cases ported, 0 skipped; the cases below are written
// from the function's rules.

// lineChange makes a change from the four numbers, in the order of the struct.
func lineChange(os, oe, ms, me int) extension.LineChange {
	return extension.LineChange{OriginalStartLineNumber: os, OriginalEndLineNumber: oe, ModifiedStartLineNumber: ms, ModifiedEndLineNumber: me}
}

// changesOf is the changes of the fake API's diff.
func changesOf(t *testing.T, original, modified string) []extension.LineChange {
	t.Helper()
	api := fakeapi.New(t, extension.Manifest{ID: "test.staging"}, extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "w", Folder: t.TempDir(), ID: "w"})
	changes, err := api.Documents().LineChanges(context.Background(), original, modified)
	if err != nil {
		t.Fatal(err)
	}
	return changes
}

// modelApply is the text with the changes applied, worked out by lines: the original lines with each chosen change's lines replaced by
// the modified ones (the lines of a text that ends with a line break have an empty last line, so the line break comes back by itself).
func modelApply(original, modified string, chosen []extension.LineChange) string {
	orig, mod := strings.Split(original, "\n"), strings.Split(modified, "\n")
	var out []string
	at := 0 // the next original line, from 0
	for _, c := range chosen {
		from := c.OriginalStartLineNumber - 1
		if c.OriginalEndLineNumber == 0 {
			from = c.OriginalStartLineNumber
		}
		out = append(out, orig[at:from]...)
		if c.ModifiedEndLineNumber != 0 {
			out = append(out, mod[c.ModifiedStartLineNumber-1:c.ModifiedEndLineNumber]...)
		}
		if c.OriginalEndLineNumber == 0 {
			at = c.OriginalStartLineNumber
		} else {
			at = c.OriginalEndLineNumber
		}
	}
	out = append(out, orig[at:]...)
	return strings.Join(out, "\n")
}

func TestApplyLineChangesTable(t *testing.T) {
	const original = "one\ntwo\nthree\nfour\nfive\n"
	tests := []struct {
		name     string
		original string
		modified string
		pick     []int // the indexes of the changes applied; nil for all
		want     string
	}{
		{"an insertion in the middle", original, "one\ntwo\nnew\nthree\nfour\nfive\n", nil, "one\ntwo\nnew\nthree\nfour\nfive\n"},
		{"an insertion at the start", original, "new\none\ntwo\nthree\nfour\nfive\n", nil, "new\none\ntwo\nthree\nfour\nfive\n"},
		{"an insertion at the end", original, original + "six\n", nil, original + "six\n"},
		{"a deletion in the middle", original, "one\ntwo\nfour\nfive\n", nil, "one\ntwo\nfour\nfive\n"},
		{"a deletion at the start", original, "two\nthree\nfour\nfive\n", nil, "two\nthree\nfour\nfive\n"},
		{"a deletion at the end", original, "one\ntwo\nthree\nfour\n", nil, "one\ntwo\nthree\nfour\n"},
		{"a modification", original, "one\nTWO\nthree\nfour\nfive\n", nil, "one\nTWO\nthree\nfour\nfive\n"},
		{"a modification that grows", original, "one\nTWO\nTWO!\nthree\nfour\nfive\n", nil, "one\nTWO\nTWO!\nthree\nfour\nfive\n"},
		{"a modification that shrinks", original, "one\nTWO\nfive\n", nil, "one\nTWO\nfive\n"},
		{"the first of three changes", original, "ONE\ntwo\nTHREE\nfour\nFIVE\n", []int{0}, "ONE\ntwo\nthree\nfour\nfive\n"},
		{"the middle of three changes", original, "ONE\ntwo\nTHREE\nfour\nFIVE\n", []int{1}, "one\ntwo\nTHREE\nfour\nfive\n"},
		{"the last of three changes", original, "ONE\ntwo\nTHREE\nfour\nFIVE\n", []int{2}, "one\ntwo\nthree\nfour\nFIVE\n"},
		{"the first and the last of three", original, "ONE\ntwo\nTHREE\nfour\nFIVE\n", []int{0, 2}, "ONE\ntwo\nthree\nfour\nFIVE\n"},
		{"no change at all", original, "ONE\ntwo\nTHREE\nfour\nFIVE\n", []int{}, original},
		{"an insertion and a deletion", original, "one\nnew\ntwo\nfour\nfive\n", nil, "one\nnew\ntwo\nfour\nfive\n"},
		{"no line break at the end of the original", "a\nb", "a\nb\nc", nil, "a\nb\nc"},
		{"no line break at the end of the modified text", "a\nb\n", "a\nb\nc", nil, "a\nb\nc"},
		{"the last line removed with its line break kept off", "a\nb", "a", nil, "a"},
		{"a line added to a text without a final line break", "a", "a\nb", nil, "a\nb"},
		{"an empty original", "", "a\nb\n", nil, "a\nb\n"},
		{"an empty modified text", "a\nb\n", "", nil, ""},
		{"both empty", "", "", nil, ""},
		{"a text that is a line break", "\n", "a\n", nil, "a\n"},
		{"non-ASCII text", "héllo\nwörld\n", "héllo\nwörld!\nüber\n", nil, "héllo\nwörld!\nüber\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changes := changesOf(t, tc.original, tc.modified)
			chosen := changes
			if tc.pick != nil {
				chosen = nil
				for _, i := range tc.pick {
					chosen = append(chosen, changes[i])
				}
			}
			got := applyLineChanges(newTextDoc(tc.original), newTextDoc(tc.modified), chosen)
			if got != tc.want {
				t.Errorf("applyLineChanges(%q, %q, %v) = %q, want %q", tc.original, tc.modified, chosen, got, tc.want)
			}
			if model := modelApply(tc.original, tc.modified, chosen); model != tc.want {
				t.Errorf("the model gives %q, the table %q", model, tc.want)
			}
		})
	}
}

func TestApplyLineChangesKeepsTheLineBreaksOfEachSide(t *testing.T) {
	// the original uses CRLF; the lines taken from the modified text have the line breaks it has, and matchLineBreaks gives them CRLF
	original := "a\r\nb\r\nc\r\n"
	modified := "a\nB\nc\n"
	chosen := changesOf(t, original, modified)
	got := applyLineChanges(newTextDoc(original), newTextDoc(matchLineBreaks(original, modified)), chosen)
	if want := "a\r\nB\r\nc\r\n"; got != want {
		t.Errorf("= %q, want %q", got, want)
	}
	if got := matchLineBreaks("a\nb\n", "x\ny\n"); got != "x\ny\n" {
		t.Errorf("matchLineBreaks over an LF version = %q", got)
	}
	if got := matchLineBreaks("a\r\nb\nc\n", "x\ny\n"); got != "x\ny\n" {
		t.Errorf("matchLineBreaks over a version mostly LF = %q", got)
	}
}

func TestApplyLineChangesRandomTextsAgainstTheModel(t *testing.T) {
	rng := rand.New(rand.NewSource(24))
	words := []string{"a", "b", "c", "d", "e", "f", "g", ""}
	text := func() string {
		n := rng.Intn(9)
		lines := make([]string, n)
		for i := range lines {
			lines[i] = words[rng.Intn(len(words))]
		}
		out := strings.Join(lines, "\n")
		if rng.Intn(2) == 0 {
			out += "\n"
		}
		return out
	}
	for i := 0; i < 400; i++ {
		original, modified := text(), text()
		changes := changesOf(t, original, modified)
		var chosen []extension.LineChange
		for _, c := range changes {
			if rng.Intn(2) == 0 {
				chosen = append(chosen, c)
			}
		}
		got := applyLineChanges(newTextDoc(original), newTextDoc(modified), chosen)
		if want := modelApply(original, modified, chosen); got != want {
			t.Fatalf("case %d: original %q modified %q chosen %v: got %q, model %q", i, original, modified, chosen, got, want)
		}
		if all := applyLineChanges(newTextDoc(original), newTextDoc(modified), changes); all != modified {
			t.Fatalf("case %d: all the changes of %q to %q give %q", i, original, modified, all)
		}
		if none := applyLineChanges(newTextDoc(original), newTextDoc(modified), nil); none != original {
			t.Fatalf("case %d: no change of %q gives %q", i, original, none)
		}
	}
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// hunk is a hunk of a patch with no context: the numbers of its header and its lines.
type hunk struct {
	a, b, c, d int
	lines      string
}

// patchOf is the patch of the hunks chosen, whose new positions are those they have when only the chosen ones are applied.
func patchOf(header string, hunks []hunk) string {
	var out strings.Builder
	out.WriteString(header)
	delta := 0
	for _, h := range hunks {
		c := h.a + delta
		if h.b == 0 || h.d != 0 && h.b == 0 {
			c++
		}
		if h.d == 0 {
			c = h.a + delta
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n%s", h.a, h.b, c, h.d, h.lines)
		delta += h.d - h.b
	}
	return out.String()
}

// gitChanges are the changes git itself finds with -U0, and the patch of each: the header lines and the hunks.
func gitChanges(t *testing.T, repo *gittest.Repo, rel string) (changes []extension.LineChange, header string, hunks []hunk) {
	t.Helper()
	diff := repo.Git("diff", "-U0", "--no-color", "--no-ext-diff", "--", rel)
	var cur []string
	var curHunk hunk
	flush := func() {
		if cur != nil {
			curHunk.lines = strings.Join(cur, "\n") + "\n"
			hunks = append(hunks, curHunk)
		}
	}
	var head []string
	for _, line := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			flush()
			cur = []string{}
			num := func(s string, def int) int {
				if s == "" {
					return def
				}
				n, err := strconv.Atoi(s)
				if err != nil {
					t.Fatal(err)
				}
				return n
			}
			a, b, c, d := num(m[1], 0), num(m[2], 1), num(m[3], 0), num(m[4], 1)
			curHunk = hunk{a: a, b: b, c: c, d: d}
			ch := extension.LineChange{OriginalStartLineNumber: a, OriginalEndLineNumber: a + b - 1, ModifiedStartLineNumber: c, ModifiedEndLineNumber: c + d - 1}
			if b == 0 {
				ch.OriginalEndLineNumber = 0
			}
			if d == 0 {
				ch.ModifiedEndLineNumber = 0
			}
			changes = append(changes, ch)
			continue
		}
		if cur == nil {
			head = append(head, line)
		} else {
			cur = append(cur, line)
		}
	}
	flush()
	return changes, strings.Join(head, "\n") + "\n", hunks
}

func TestApplyLineChangesAgainstGitApply(t *testing.T) {
	e := gittest.NewEnv(t)
	tests := []struct {
		name     string
		original string
		modified string
	}{
		{"insertions, a deletion and a modification", "1\n2\n3\n4\n5\n6\n7\n8\n", "0\n1\n2\nthree\n5\n6\n6.5\n8\n9\n"},
		{"changes at both ends", "1\n2\n3\n", "a\n2\nc\n"},
		{"adjacent changes far apart", "1\n2\n3\n4\n5\n6\n7\n8\n9\n", "1\nB\n3\n4\n5\n6\n7\nH\n9\n"},
		{"the file shrinks to one line", "1\n2\n3\n4\n", "3\n"},
		{"the file grows from one line", "1\n", "1\n2\n3\n4\n"},
		{"non-ASCII text and a path with a space", "héllo\nwörld\nend\n", "héllo\nwörld!\nend\nüber\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := e.Init(e.UniqueName("apply"))
			const rel = "sub dir/file.txt"
			repo.Write(rel, tc.original)
			repo.Commit("Add the file")
			repo.Write(rel, tc.modified)
			changes, header, hunks := gitChanges(t, repo, rel)
			if len(changes) == 0 || len(changes) > 5 {
				t.Fatalf("git found %d changes", len(changes))
			}
			// every subset of the changes: git applies the hunks of the subset to the index, applyLineChanges builds the same text
			for mask := 0; mask < 1<<len(changes); mask++ {
				var chosen []extension.LineChange
				var picked []hunk
				for i := range changes {
					if mask&(1<<i) != 0 {
						chosen = append(chosen, changes[i])
						picked = append(picked, hunks[i])
					}
				}
				patch := patchOf(header, picked)
				repo.Git("reset", "-q")
				if len(chosen) > 0 {
					if _, stderr, err := e.GitInput(repo.Dir, patch, "apply", "--cached", "--unidiff-zero", "-"); err != nil {
						t.Fatalf("git apply of subset %b: %v\n%s\n%s", mask, err, stderr, patch)
					}
				}
				got := applyLineChanges(newTextDoc(tc.original), newTextDoc(tc.modified), chosen)
				// the object ids tell the texts apart to the last byte (the helpers of the harness trim the output)
				gotID, _, err := e.GitInput(repo.Dir, got, "hash-object", "--stdin")
				if err != nil {
					t.Fatal(err)
				}
				if wantID := repo.Git("rev-parse", ":"+rel); gotID != wantID {
					t.Errorf("subset %b of %v: applyLineChanges = %q, but git apply --cached made %q", mask, chosen, got, repo.Git("show", ":"+rel))
				}
			}
		})
	}
}

func TestTextDocSplitsLikeVSCode(t *testing.T) {
	tests := []struct {
		text  string
		lines int
		ends  []int
	}{
		{"", 1, []int{0}},
		{"a", 1, []int{1}},
		{"a\n", 2, []int{1, 0}},
		{"a\r\nb", 2, []int{1, 1}},
		{"a\rb\n\n", 4, []int{1, 1, 0, 0}},
		{"héllo\nx", 2, []int{6, 1}},
	}
	for _, tc := range tests {
		d := newTextDoc(tc.text)
		var ends []int
		for i := 0; i < d.lineCount(); i++ {
			ends = append(ends, d.lineEnd(i))
		}
		if d.lineCount() != tc.lines || !slices.Equal(ends, tc.ends) {
			t.Errorf("%q: %d lines ending at %v, want %d at %v", tc.text, d.lineCount(), ends, tc.lines, tc.ends)
		}
	}
	d := newTextDoc("ab\ncd\n")
	for _, tc := range []struct {
		l1, c1, l2, c2 int
		want           string
	}{
		{0, 0, 0, 2, "ab"}, {0, 1, 1, 1, "b\nc"}, {1, 0, 2, 0, "cd\n"}, {0, 0, 9, 0, "ab\ncd\n"}, {1, 99, 0, 0, "ab\ncd"}, {-1, 0, 0, 1, "a"},
	} {
		if got := d.slice(tc.l1, tc.c1, tc.l2, tc.c2); got != tc.want {
			t.Errorf("slice(%d,%d,%d,%d) = %q, want %q", tc.l1, tc.c1, tc.l2, tc.c2, got, tc.want)
		}
	}
}

func TestToLineRanges(t *testing.T) {
	doc := newTextDoc("aa\nbb\ncc\ndd\nee\nff\n")
	sel := func(l1, c1, l2, c2 int) extension.Selection {
		return extension.Selection{Anchor: extension.Position{Line: l1, Col: c1}, Active: extension.Position{Line: l2, Col: c2}}
	}
	tests := []struct {
		name string
		sels []extension.Selection
		want []textRange
	}{
		{"a cursor is its line", []extension.Selection{sel(1, 1, 1, 1)}, []textRange{{textPos{1, 0}, textPos{1, 2}}}},
		{"a backward selection", []extension.Selection{sel(2, 1, 1, 0)}, []textRange{{textPos{1, 0}, textPos{2, 2}}}},
		{"overlapping selections join", []extension.Selection{sel(0, 0, 2, 1), sel(1, 0, 3, 1)}, []textRange{{textPos{0, 0}, textPos{3, 2}}}},
		{"adjacent lines join", []extension.Selection{sel(0, 0, 0, 1), sel(1, 0, 1, 1)}, []textRange{{textPos{0, 0}, textPos{1, 2}}}},
		{"far lines stay apart, in order", []extension.Selection{sel(4, 0, 4, 0), sel(0, 0, 0, 0)}, []textRange{{textPos{0, 0}, textPos{0, 2}}, {textPos{4, 0}, textPos{4, 2}}}},
		{"one selection inside another", []extension.Selection{sel(0, 0, 3, 0), sel(1, 0, 1, 1)}, []textRange{{textPos{0, 0}, textPos{3, 2}}}},
	}
	for _, tc := range tests {
		if got := toLineRanges(tc.sels, doc); !slices.Equal(got, tc.want) {
			t.Errorf("%s: toLineRanges = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSelectedChangesAreCutToTheSelectedLines(t *testing.T) {
	original := "1\n2\n3\n4\n5\n6\n"
	modified := "1\nB\nC\nD\n5\n6\n"
	doc := newTextDoc(modified)
	changes := changesOf(t, original, modified)
	if len(changes) != 1 || changes[0] != lineChange(2, 4, 2, 4) {
		t.Fatalf("changes = %v", changes)
	}
	tests := []struct {
		name  string
		lines []textRange
		want  []extension.LineChange
	}{
		{"the middle line of a block of three", []textRange{{textPos{2, 0}, textPos{2, 1}}}, []extension.LineChange{lineChange(3, 3, 3, 3)}},
		{"the first two lines", []textRange{{textPos{1, 0}, textPos{2, 1}}}, []extension.LineChange{lineChange(2, 3, 2, 3)}},
		{"lines around the block", []textRange{{textPos{0, 0}, textPos{5, 0}}}, []extension.LineChange{lineChange(2, 4, 2, 4)}},
		{"a line that is not changed", []textRange{{textPos{5, 0}, textPos{5, 1}}}, nil},
	}
	for _, tc := range tests {
		got := selectedChanges(doc, changes, tc.lines)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
		if len(got) > 0 {
			if applied := applyLineChanges(newTextDoc(original), doc, got); applied == original {
				t.Errorf("%s: staging %v changed nothing", tc.name, got)
			}
		}
	}
	// a block with more lines on one side is taken whole
	changes = changesOf(t, "1\n2\n3\n", "1\nB\nC\nD\n3\n")
	if got := selectedChanges(newTextDoc("1\nB\nC\nD\n3\n"), changes, []textRange{{textPos{2, 0}, textPos{2, 1}}}); !slices.Equal(got, []extension.LineChange{lineChange(2, 2, 3, 3)}) {
		t.Errorf("a block of 1 and 3 lines cut at one line = %v", got)
	}
	// a deletion is taken whole by a selection at the place
	changes = changesOf(t, "1\n2\n3\n", "1\n3\n")
	if got := selectedChanges(newTextDoc("1\n3\n"), changes, []textRange{{textPos{0, 0}, textPos{0, 1}}}); !slices.Equal(got, changes) {
		t.Errorf("a deletion = %v, want %v", got, changes)
	}
}

func TestInvertAndCompareLineChanges(t *testing.T) {
	c := lineChange(1, 2, 3, 0)
	if got := invertLineChange(c); got != lineChange(3, 0, 1, 2) {
		t.Errorf("invert = %v", got)
	}
	if invertLineChange(invertLineChange(c)) != c {
		t.Error("inverting twice changes the change")
	}
	list := []extension.LineChange{lineChange(5, 5, 6, 6), lineChange(1, 1, 1, 1), lineChange(2, 0, 2, 2), lineChange(1, 1, 1, 2)}
	slices.SortFunc(list, compareLineChanges)
	want := []extension.LineChange{lineChange(1, 1, 1, 1), lineChange(1, 1, 1, 2), lineChange(2, 0, 2, 2), lineChange(5, 5, 6, 6)}
	if !slices.Equal(list, want) {
		t.Errorf("sorted = %v, want %v", list, want)
	}
	if compareLineChanges(c, c) != 0 {
		t.Error("a change differs from itself")
	}
}

func TestReplaceEditIsTheSmallestEdit(t *testing.T) {
	tests := []struct{ oldText, newText, want string }{
		{"a\nb\nc\n", "a\nB\nc\n", "1:0-1:1 -> B"},
		{"a\nb\nc\n", "a\nc\n", "1:0-2:0 -> "},
		{"a\nc\n", "a\nb\nc\n", "1:0-1:0 -> b\n"},
		{"héllo\nx\n", "hällo\nx\n", "0:1-0:3 -> ä"},
		{"same", "same", ""},
		{"a", "", "0:0-0:1 -> "},
		{"", "a", "0:0-0:0 -> a"},
	}
	for _, tc := range tests {
		edit, ok := replaceEdit(tc.oldText, tc.newText)
		got := ""
		if ok {
			got = fmt.Sprintf("%d:%d-%d:%d -> %s", edit.Range.Start.Line, edit.Range.Start.Col, edit.Range.End.Line, edit.Range.End.Col, edit.Text)
		}
		if got != tc.want {
			t.Errorf("replaceEdit(%q, %q) = %q, want %q", tc.oldText, tc.newText, got, tc.want)
		}
		if ok {
			if out, err := fakeapi.ApplyRangeEdits(tc.oldText, []extension.RangeEdit{edit}); err != nil || out != tc.newText {
				t.Errorf("applying the edit of %q gives %q, %v; want %q", tc.oldText, out, err, tc.newText)
			}
		}
	}
}
