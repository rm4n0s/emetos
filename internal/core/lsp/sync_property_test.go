package lsp_test

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

// The parameters of the property test: it is deterministic (each seed is a run), and bounded.
const (
	propSeeds      = 6   // seeds per configuration
	propOps        = 150 // operations per run
	propMaxBytes   = 1500
	propCheckEvery = 5 // the server's copy is compared every this many operations (every operation for seed 0)
)

// pieces are what the random texts are made of: ASCII, 2-, 3- and 4-byte characters, tabs and line ends.
var pieces = []string{"a", "b", "xyz", " ", "\t", "\n", "\n", "é", "€", "😀", "中", "\r\n", ""}

func randomText(rng *rand.Rand, maxPieces int) string {
	var s string
	for n := rng.Intn(maxPieces + 1); n > 0; n-- {
		s += pieces[rng.Intn(len(pieces))]
	}
	return s
}

// randomBatch makes a batch of 1 to 3 non-overlapping edits at random places of the document.
func randomBatch(rng *rand.Rand, snap buffer.Snapshot, grow bool) []document.Edit {
	k := 1 + rng.Intn(3)
	text := snap.String()
	offsets := make([]int, 2*k)
	for i := range offsets {
		off := rng.Intn(snap.Len() + 1)
		for off < len(text) && !utf8.RuneStart(text[off]) {
			off-- // an edit starts and ends between characters
		}
		offsets[i] = off
	}
	sort.Ints(offsets)
	edits := make([]document.Edit, k)
	for i := range edits {
		text := ""
		if grow {
			text = randomText(rng, 5)
		}
		edits[i] = document.Edit{
			Range: buffer.Range{Start: snap.PositionAt(offsets[2*i]), End: snap.PositionAt(offsets[2*i+1])},
			Text:  text,
		}
	}
	return edits
}

// TestRandomEditsKeepTheServerInSync applies seeded random operations to a document (batches of edits with
// several cursors, undo, redo, replacement of the whole text, changes of the line ending) and compares the
// text the fake server rebuilt from the notifications with the document, as a server of each encoding and
// synchronization kind would hold it, for LF and CRLF documents.
func TestRandomEditsKeepTheServerInSync(t *testing.T) {
	configs := []struct{ name, caps string }{
		{"incremental utf-16", capsUTF16},
		{"incremental utf-8", capsUTF8},
		{"incremental utf-32", capsUTF32},
		{"full utf-16", capsFull},
		{"full utf-8", capsFullUTF8},
	}
	for _, cfg := range configs {
		for _, eol := range []fileio.LineEnding{fileio.LF, fileio.CRLF} {
			for seed := int64(0); seed < propSeeds; seed++ {
				name := fmt.Sprintf("%s/%q/seed %d", cfg.name, string(eol), seed)
				t.Run(name, func(t *testing.T) {
					runRandomEdits(t, cfg.caps, eol, seed)
				})
			}
		}
	}
}

func runRandomEdits(t *testing.T, caps string, eol fileio.LineEnding, seed int64) {
	h := lsptest.Start(t, lsptest.Options{Script: script(t, caps)})
	rng := rand.New(rand.NewSource(seed + 1))
	d := newDoc("/work/prop.go", strings.ReplaceAll(randomText(rng, 30), "\r\n", "\n"), eol) // a document holds "\n" only
	if _, err := h.Client.OpenDocument(d); err != nil {
		t.Fatal(err)
	}
	synced(t, h, d)
	for op := 1; op <= propOps; op++ {
		var what string
		switch r := rng.Intn(100); {
		case r < 70:
			grow := d.Snapshot().Len() < propMaxBytes || rng.Intn(4) == 0 // past the bound only shrink, mostly
			if d.Snapshot().Len() >= propMaxBytes {
				grow = false
			}
			what = "edit"
			kind := document.EditKind(rng.Intn(2)) // typing edits join the undo group
			if _, err := d.Edit(randomBatch(rng, d.Snapshot(), grow), document.EditOptions{Kind: kind}); err != nil {
				t.Fatalf("op %d: Edit: %v", op, err)
			}
		case r < 82:
			what = "undo"
			mustHistory(t, d.Undo)
		case r < 92:
			what = "redo"
			mustHistory(t, d.Redo)
		case r < 96:
			what = "set text"
			d.SetText(randomText(rng, 40))
		default:
			what = "line ending"
			next := fileio.CRLF
			if d.LineEnding() == fileio.CRLF {
				next = fileio.LF
			}
			if err := d.SetLineEnding(next); err != nil {
				t.Fatalf("op %d: SetLineEnding: %v", op, err)
			}
		}
		if seed == 0 || op%propCheckEvery == 0 || op == propOps {
			synced(t, h, d)
			if t.Failed() {
				t.Fatalf("diverged at operation %d (%s), seed %d", op, what, seed)
			}
		}
	}
}

func TestPropertyConfigurationsAreServedByTheNegotiatedEncoding(t *testing.T) {
	// A guard for the test above: the clients really negotiated what the configurations say.
	for caps, want := range map[string]lsp.Encoding{capsUTF16: lsp.UTF16, capsUTF8: lsp.UTF8, capsUTF32: lsp.UTF32, capsFull: lsp.UTF16, capsFullUTF8: lsp.UTF8} {
		h := lsptest.Start(t, lsptest.Options{Script: script(t, caps)})
		if h.Client.Encoding() != want {
			t.Errorf("%s: encoding = %v, want %v", caps, h.Client.Encoding(), want)
		}
	}
}
