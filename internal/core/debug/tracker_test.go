package debug_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/document"
)

func newDoc(path, text string) *document.Document {
	return document.New(document.FileURI(path), text, document.NewOptions())
}

func numberedLines(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  line %d\n", i)
	}
	return b.String()
}

func requestedLines(svc *debug.Service, path string) map[string]int {
	out := map[string]int{}
	for _, b := range svc.Model().Breakpoints(debug.BreakpointFilter{OriginalPath: path}) {
		out[b.ID] = b.RequestedLine
	}
	return out
}

func TestTrackerFollowsEdits(t *testing.T) {
	ctx := context.Background()
	const path = "/p/main.go"
	svc := newLaunchService(t, debug.Options{})
	doc := newDoc(path, "  one\n\ttwo\nthree\n\n  five\n")
	added, err := svc.AddBreakpoints(ctx, path, []debug.BreakpointData{{Line: 1}, {Line: 2, Column: 3}, {Line: 4}, {Line: 5, Condition: "c"}})
	if err != nil {
		t.Fatal(err)
	}
	tr := debug.NewTracker(svc)
	tr.Track(path, doc)
	if n := len(doc.AllDecorations()); n != 4 {
		t.Fatalf("decorations = %d, want 4", n)
	}
	// The decoration covers one character at the first non-blank one, or at the column.
	decos := doc.AllDecorations()
	wantRanges := []buffer.Range{
		{Start: buffer.Position{Line: 0, Col: 2}, End: buffer.Position{Line: 0, Col: 3}},
		{Start: buffer.Position{Line: 1, Col: 2}, End: buffer.Position{Line: 1, Col: 3}}, // column 3 of "\ttwo" is "w": byte 2
		{Start: buffer.Position{Line: 3, Col: 0}, End: buffer.Position{Line: 3, Col: 0}}, // an empty line has no character
		{Start: buffer.Position{Line: 4, Col: 2}, End: buffer.Position{Line: 4, Col: 3}},
	}
	for i, d := range decos {
		if d.Range != wantRanges[i] {
			t.Errorf("decoration %d = %v, want %v", i, d.Range, wantRanges[i])
		}
	}

	// Insert two lines at the top: all move down by two.
	edit := func(r buffer.Range, text string) {
		t.Helper()
		if _, err := doc.Edit([]document.Edit{{Range: r, Text: text}}, document.EditOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	edit(buffer.Range{}, "a\nb\n")
	moved, err := tr.Sync(ctx, path)
	if err != nil || moved != 4 {
		t.Fatalf("Sync = %d, %v", moved, err)
	}
	for i, bp := range svc.Model().Breakpoints(debug.BreakpointFilter{}) {
		if want := []int{3, 4, 6, 7}[i]; bp.RequestedLine != want {
			t.Errorf("breakpoint %d is at line %d, want %d", i, bp.RequestedLine, want)
		}
	}
	// A breakpoint with a column keeps following its character.
	bps := svc.Model().Breakpoints(debug.BreakpointFilter{})
	if bps[1].RequestedColumn != 3 {
		t.Errorf("column = %d", bps[1].RequestedColumn)
	}
	edit(buffer.Range{Start: buffer.Position{Line: 3, Col: 0}, End: buffer.Position{Line: 3, Col: 0}}, "ééé")
	if _, err := tr.Sync(ctx, path); err != nil {
		t.Fatal(err)
	}
	bps = svc.Model().Breakpoints(debug.BreakpointFilter{})
	// "ééé" is 3 UTF-16 units and 6 bytes: the column moved by 3.
	if bps[1].RequestedColumn != 6 || bps[1].RequestedLine != 4 {
		t.Errorf("column after typing before it = line %d column %d, want 4:6", bps[1].RequestedLine, bps[1].RequestedColumn)
	}
	// Nothing moved: Sync reports 0.
	if moved, err := tr.Sync(ctx, path); err != nil || moved != 0 {
		t.Errorf("Sync again = %d, %v", moved, err)
	}

	// A breakpoint added after Track gets a decoration; a removed one loses its decoration.
	more, err := svc.AddBreakpoints(ctx, path, []debug.BreakpointData{{Line: 2}})
	if err != nil || len(more) != 1 {
		t.Fatal(err)
	}
	if err := svc.RemoveBreakpoints(ctx, added[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Sync(ctx, path); err != nil {
		t.Fatal(err)
	}
	if n := len(doc.AllDecorations()); n != 4 {
		t.Errorf("decorations after add and remove = %d, want 4", n)
	}
	// A breakpoint past the end of the document has no decoration, and does not break anything.
	if _, err := svc.AddBreakpoints(ctx, path, []debug.BreakpointData{{Line: 500}}); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Sync(ctx, path); err != nil {
		t.Fatal(err)
	}
	if n := len(doc.AllDecorations()); n != 4 {
		t.Errorf("decorations with a breakpoint past the end = %d, want 4", n)
	}

	// Untrack removes the decorations.
	tr.Untrack(path)
	if n := len(doc.AllDecorations()); n != 0 {
		t.Errorf("decorations after Untrack = %d", n)
	}
	if moved, err := tr.Sync(ctx, path); err != nil || moved != 0 {
		t.Errorf("Sync of an untracked path = %d, %v", moved, err)
	}
}

func TestTrackerMarksTheDocumentDirty(t *testing.T) {
	ctx := context.Background()
	const path = "/p/dirty.go"
	svc := newLaunchService(t, debug.Options{})
	doc := newDoc(path, "a\nb\nc\n")
	bps, err := svc.AddBreakpoints(ctx, path, []debug.BreakpointData{{Line: 2}})
	if err != nil {
		t.Fatal(err)
	}
	// An adapter verified it, then the file changed.
	tr := debug.NewTracker(svc)
	tr.Track(path, doc)
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "x"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Sync(ctx, path); err != nil {
		t.Fatal(err)
	}
	bp, _ := svc.Model().Breakpoint(bps[0].ID)
	if bp.Message != "Unverified breakpoint. File is modified, please restart debug session." {
		t.Errorf("message of a breakpoint in a modified file = %q", bp.Message)
	}
}

// refBreakpoints follows the lines of breakpoints through the line-level edits of the random test without a document: the oracle.
type refBreakpoints struct {
	ids   []string // in the order the model keeps them: by line, ties by age
	lines map[string]int
}

func (r *refBreakpoints) normalize() {
	sort.SliceStable(r.ids, func(i, j int) bool { return r.lines[r.ids[i]] < r.lines[r.ids[j]] })
	seen := map[int]bool{}
	kept := r.ids[:0]
	for _, id := range r.ids {
		if seen[r.lines[id]] {
			delete(r.lines, id)
			continue
		}
		seen[r.lines[id]] = true
		kept = append(kept, id)
	}
	r.ids = kept
}

func TestTrackerRandomEditsMatchTheOracle(t *testing.T) {
	ctx := context.Background()
	const path = "/p/random.go"
	const seeds, steps, lineCount = 40, 30, 40
	for seed := uint64(1); seed <= seeds; seed++ {
		rng := rand.New(rand.NewPCG(seed, 99))
		svc := newLaunchService(t, debug.Options{})
		doc := newDoc(path, numberedLines(lineCount))
		var data []debug.BreakpointData
		for _, l := range rng.Perm(lineCount)[:6] {
			data = append(data, debug.BreakpointData{Line: l + 1})
		}
		added, err := svc.AddBreakpoints(ctx, path, data)
		if err != nil {
			t.Fatal(err)
		}
		ref := &refBreakpoints{lines: map[string]int{}}
		for _, b := range svc.Model().Breakpoints(debug.BreakpointFilter{}) {
			ref.ids = append(ref.ids, b.ID)
			ref.lines[b.ID] = b.RequestedLine - 1
		}
		_ = added
		tr := debug.NewTracker(svc)
		tr.Track(path, doc)

		for step := 0; step < steps; step++ {
			lines := doc.Snapshot().LineCount() // the last line is empty (the text ends with a line feed)
			var desc string
			switch op := rng.IntN(4); op {
			case 0: // insert lines before a line
				at, n := rng.IntN(lines), 1+rng.IntN(3)
				desc = fmt.Sprintf("insert %d lines at %d", n, at)
				_, err = doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: at}, End: buffer.Position{Line: at}}, Text: strings.Repeat("  new\n", n)}}, document.EditOptions{})
				for id, l := range ref.lines {
					if l >= at {
						ref.lines[id] = l + n
					}
				}
			case 1: // delete whole lines
				if lines < 4 {
					continue
				}
				i := rng.IntN(lines - 2)
				j := i + rng.IntN(min(3, lines-2-i))
				desc = fmt.Sprintf("delete lines %d-%d", i, j)
				_, err = doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: i}, End: buffer.Position{Line: j + 1}}, Text: ""}}, document.EditOptions{})
				for id, l := range ref.lines {
					switch {
					case l > j:
						ref.lines[id] = l - (j - i + 1)
					case l >= i:
						ref.lines[id] = i
					}
				}
			case 2: // type at the start of a line: no line moves
				at := rng.IntN(lines)
				desc = fmt.Sprintf("type at the start of %d", at)
				_, err = doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: at}, End: buffer.Position{Line: at}}, Text: "x"}}, document.EditOptions{})
			case 3: // join a line with the next
				if lines < 3 {
					continue
				}
				at := rng.IntN(lines - 2)
				content, lerr := doc.Snapshot().LineContent(at)
				if lerr != nil {
					t.Fatal(lerr)
				}
				desc = fmt.Sprintf("join %d and %d", at, at+1)
				_, err = doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: at, Col: len(content)}, End: buffer.Position{Line: at + 1}}, Text: ""}}, document.EditOptions{})
				for id, l := range ref.lines {
					if l > at {
						ref.lines[id] = l - 1
					}
				}
			}
			if err != nil {
				t.Fatalf("seed %d step %d (%s): %v", seed, step, desc, err)
			}
			ref.normalize()
			if _, err := tr.Sync(ctx, path); err != nil {
				t.Fatal(err)
			}
			got := requestedLines(svc, path)
			if len(got) != len(ref.lines) {
				t.Fatalf("seed %d step %d (%s): %d breakpoints, want %d\n got %v\nwant %v", seed, step, desc, len(got), len(ref.lines), got, ref.lines)
			}
			for id, l := range ref.lines {
				if got[id] != l+1 {
					t.Fatalf("seed %d step %d (%s): breakpoint %s is at line %d, want %d\n got %v\nwant %v", seed, step, desc, id, got[id], l+1, got, ref.lines)
				}
			}
			if n := len(doc.AllDecorations()); n != len(got) {
				t.Fatalf("seed %d step %d (%s): %d decorations for %d breakpoints", seed, step, desc, n, len(got))
			}
		}
	}
}
