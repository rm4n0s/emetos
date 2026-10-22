package buffer

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"
)

// checkTree verifies what the piece tree promises: AVL balance, correct heights,
// sizes and line-feed counts in every node, no empty piece, and that the pieces
// read in order give want.
func checkTree(t *testing.T, b *Buffer, want string) {
	t.Helper()
	var walk func(n *node) (h, size, lf int)
	walk = func(n *node) (int, int, int) {
		if n == nil {
			return 0, 0, 0
		}
		lh, ls, ll := walk(n.left)
		rh, rs, rl := walk(n.right)
		if d := lh - rh; d < -1 || d > 1 {
			t.Fatalf("node out of balance by %d (heights %d and %d)", d, lh, rh)
		}
		if n.p.len() <= 0 {
			t.Fatalf("empty piece %+v", n.p)
		}
		if got := strings.Count(string(n.p.bytes()), "\n"); got != n.p.lf() {
			t.Fatalf("piece says %d line feeds, has %d", n.p.lf(), got)
		}
		h, s, l := 1+max(lh, rh), ls+n.p.len()+rs, ll+n.p.lf()+rl
		if n.height != h || n.size != s || n.lf != l {
			t.Fatalf("node has height %d size %d lf %d, want %d %d %d", n.height, n.size, n.lf, h, s, l)
		}
		return h, s, l
	}
	walk(b.root)
	if got := b.String(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if b.Len() != len(want) {
		t.Fatalf("Len = %d, want %d", b.Len(), len(want))
	}
	if b.LineFeeds() != strings.Count(want, "\n") {
		t.Fatalf("LineFeeds = %d, want %d", b.LineFeeds(), strings.Count(want, "\n"))
	}
}

func TestNewAndText(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"empty", ""},
		{"one line", "hello"},
		{"lines", "a\nbb\n\nccc\n"},
		{"multibyte", "αβγ\n日本語\n😀"},
		{"longer than a chunk", strings.Repeat("line of text\n", 20000)},
		{"one huge line", strings.Repeat("é", 100000)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(tt.text)
			checkTree(t, b, tt.text)
			got, err := b.Text(len(tt.text)/3, len(tt.text)*2/3)
			if err != nil {
				t.Fatalf("Text: %v", err)
			}
			if want := tt.text[len(tt.text)/3 : len(tt.text)*2/3]; got != want {
				t.Errorf("Text = %q, want %q", got, want)
			}
		})
	}
}

func TestChunksDontSplitRunes(t *testing.T) {
	// 'é' is two bytes, so a cut at 65536 bytes would fall inside one if the text
	// started with one byte of another width.
	b := New("x" + strings.Repeat("é", 70000))
	for _, p := range originalPieces("x" + strings.Repeat("é", 70000)) {
		if p.len() > maxChunk {
			t.Errorf("piece of %d bytes, more than maxChunk", p.len())
		}
		if s := string(p.bytes()); strings.ContainsRune(s, '�') {
			t.Error("a chunk holds half a character")
		}
	}
	checkTree(t, b, "x"+strings.Repeat("é", 70000))
}

func TestEdits(t *testing.T) {
	tests := []struct {
		name string
		init string
		do   func(*Buffer) error
		want string
	}{
		{"insert in empty", "", func(b *Buffer) error { return b.Insert(0, "abc") }, "abc"},
		{"insert at start", "bc", func(b *Buffer) error { return b.Insert(0, "a") }, "abc"},
		{"insert at end", "ab", func(b *Buffer) error { return b.Insert(2, "c") }, "abc"},
		{"insert in the middle", "ac", func(b *Buffer) error { return b.Insert(1, "b") }, "abc"},
		{"insert nothing", "abc", func(b *Buffer) error { return b.Insert(1, "") }, "abc"},
		{"insert lines", "ad", func(b *Buffer) error { return b.Insert(1, "b\nc\n") }, "ab\nc\nd"},
		{"delete all", "abc", func(b *Buffer) error { return b.Delete(0, 3) }, ""},
		{"delete the start", "abc", func(b *Buffer) error { return b.Delete(0, 1) }, "bc"},
		{"delete the end", "abc", func(b *Buffer) error { return b.Delete(2, 3) }, "ab"},
		{"delete the middle", "abcde", func(b *Buffer) error { return b.Delete(1, 4) }, "ae"},
		{"delete nothing", "abc", func(b *Buffer) error { return b.Delete(2, 2) }, "abc"},
		{"delete a line feed", "a\nb", func(b *Buffer) error { return b.Delete(1, 2) }, "ab"},
		{"replace", "hello world", func(b *Buffer) error { return b.Replace(6, 11, "there") }, "hello there"},
		{"replace by nothing", "abc", func(b *Buffer) error { return b.Replace(0, 3, "") }, ""},
		{"replace nothing", "ac", func(b *Buffer) error { return b.Replace(1, 1, "b") }, "abc"},
		{"typing", "", func(b *Buffer) error {
			for i, r := range "hello" {
				if err := b.Insert(i, string(r)); err != nil {
					return err
				}
			}
			return nil
		}, "hello"},
		{"typing in the middle", "ad", func(b *Buffer) error {
			for i, r := range "bc\nx" {
				if err := b.Insert(1+i, string(r)); err != nil {
					return err
				}
			}
			return nil
		}, "abc\nxd"},
		{"insert longer than a chunk", "ab", func(b *Buffer) error {
			return b.Insert(1, strings.Repeat("é\n", 100000))
		}, "a" + strings.Repeat("é\n", 100000) + "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(tt.init)
			if err := tt.do(b); err != nil {
				t.Fatalf("edit: %v", err)
			}
			checkTree(t, b, tt.want)
		})
	}
}

func TestEditErrors(t *testing.T) {
	tests := []struct {
		name  string
		route string
		tag   string
		do    func(*Buffer) error
	}{
		{"insert before the start", "Insert", "OffsetOutOfRange", func(b *Buffer) error { return b.Insert(-1, "x") }},
		{"insert past the end", "Insert", "OffsetOutOfRange", func(b *Buffer) error { return b.Insert(4, "x") }},
		{"delete reversed", "Delete", "OffsetRangeInvalid", func(b *Buffer) error { return b.Delete(2, 1) }},
		{"delete past the end", "Delete", "OffsetRangeInvalid", func(b *Buffer) error { return b.Delete(0, 4) }},
		{"replace from before the start", "Replace", "OffsetRangeInvalid", func(b *Buffer) error { return b.Replace(-1, 1, "x") }},
		{"text reversed", "Text", "OffsetRangeInvalid", func(b *Buffer) error { _, err := b.Text(2, 1); return err }},
		{"text past the end", "Text", "OffsetRangeInvalid", func(b *Buffer) error { _, err := b.Text(0, 4); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New("abc")
			appErr, ok := errors.FromError(tt.do(b))
			if !ok {
				t.Fatal("expected *errors.Error")
			}
			if !appErr.HasRoute(tt.route) || !appErr.HasRoute(tt.tag) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tt.tag)
			}
			checkTree(t, b, "abc")
		})
	}
}

// edit is one change to the text and the plain string used as the oracle.
func randomEdit(rng *rand.Rand, b *Buffer, model string) string {
	pieces := []string{"a", "ab", "\n", "xyz\n", "é", "日本", "😀", "\n\n", strings.Repeat("w", 70000)}
	text := pieces[rng.Intn(len(pieces)-1)]
	if rng.Intn(200) == 0 {
		text = pieces[len(pieces)-1]
	}
	if rng.Intn(5) == 0 {
		text += pieces[rng.Intn(len(pieces)-1)]
	}
	// Edits may fall inside a multibyte character: the buffer works on bytes.
	start := rng.Intn(len(model) + 1)
	switch rng.Intn(4) {
	case 0: // insert
		must(b.Insert(start, text))
		return model[:start] + text + model[start:]
	case 1: // delete
		end := min(len(model), start+rng.Intn(40))
		must(b.Delete(start, end))
		return model[:start] + model[end:]
	case 2: // replace
		end := min(len(model), start+rng.Intn(40))
		must(b.Replace(start, end, text))
		return model[:start] + text + model[end:]
	}
	// type: several inserts in a row, each after the last
	for i := 0; i < 1+rng.Intn(6); i++ {
		c := pieces[rng.Intn(4)]
		must(b.Insert(start, c))
		model = model[:start] + c + model[start:]
		start += len(c)
	}
	return model
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func TestRandomEditsAgainstAString(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			model := ""
			if seed%2 == 0 {
				model = strings.Repeat("start of a line\n", rng.Intn(500))
			}
			b := New(model)
			type taken struct {
				snap Snapshot
				text string
			}
			var snaps []taken
			for i := 0; i < 250; i++ {
				model = randomEdit(rng, b, model)
				checkTree(t, b, model)
				if rng.Intn(10) == 0 {
					snaps = append(snaps, taken{b.Snapshot(), model})
				}
			}
			// Snapshots taken on the way are unchanged by everything after them.
			for i, s := range snaps {
				if got := s.snap.String(); got != s.text {
					t.Fatalf("snapshot %d changed: got %d bytes, want %d", i, len(got), len(s.text))
				}
				if s.snap.Len() != len(s.text) || s.snap.LineFeeds() != strings.Count(s.text, "\n") {
					t.Fatalf("snapshot %d has the wrong Len or LineFeeds", i)
				}
			}
		})
	}
}

func TestTextOfEveryRange(t *testing.T) {
	text := "ab\ncde\n\nfghij"
	b := New(text)
	// Pieces from several edits, so ranges cross piece borders.
	must(b.Insert(4, "XY"))
	must(b.Delete(0, 1))
	must(b.Insert(b.Len(), "\nend"))
	want := b.String()
	for start := 0; start <= len(want); start++ {
		for end := start; end <= len(want); end++ {
			got, err := b.Text(start, end)
			if err != nil || got != want[start:end] {
				t.Fatalf("Text(%d, %d) = %q, %v, want %q", start, end, got, err, want[start:end])
			}
		}
	}
}

func TestBalanceOfManyPieces(t *testing.T) {
	b := New("")
	model := ""
	rng := rand.New(rand.NewSource(7))
	// Many small scattered inserts make a tree of thousands of pieces.
	for i := 0; i < 5000; i++ {
		at := rng.Intn(len(model) + 1)
		s := string(rune('a' + i%26))
		must(b.Insert(at, s))
		model = model[:at] + s + model[at:]
	}
	checkTree(t, b, model)
	if h := height(b.root); h > 20 {
		t.Errorf("height %d for a tree of about 5000 pieces", h)
	}
	// Deleting in order and in reverse keeps it balanced.
	for b.Len() > 0 {
		at := rng.Intn(b.Len())
		end := min(b.Len(), at+1+rng.Intn(3))
		must(b.Delete(at, end))
		model = model[:at] + model[end:]
		if b.Len()%250 == 0 {
			checkTree(t, b, model)
		}
	}
	checkTree(t, b, "")
}

func TestSnapshotsKeepTheirText(t *testing.T) {
	b := New("one\ntwo")
	s1 := b.Snapshot()
	must(b.Insert(3, " and a half"))
	s2 := b.Snapshot()
	must(b.Delete(0, b.Len()))
	s3 := b.Snapshot()
	must(b.Insert(0, "fresh"))
	for _, tt := range []struct {
		name string
		snap Snapshot
		want string
	}{
		{"before the insert", s1, "one\ntwo"},
		{"after the insert", s2, "one and a half\ntwo"},
		{"after the delete", s3, ""},
	} {
		if got := tt.snap.String(); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

// Typing grows the last piece of the add buffer while snapshots made earlier
// still read the shorter piece of the same chunk, from other goroutines; run
// under -race.
func TestReadingSnapshotsWhileEditing(t *testing.T) {
	b := New("start\n")
	model := "start\n"
	var wg sync.WaitGroup
	for i := 0; i < 400; i++ {
		must(b.Insert(len(model), "ab\n"))
		model += "ab\n"
		if i%5 == 0 {
			must(b.Insert(0, "é"))
			model = "é" + model
		}
		snap, want := b.Snapshot(), model
		wg.Add(1)
		go func() {
			defer wg.Done()
			if snap.String() != want {
				t.Error("a snapshot read while the buffer changed differs from the text it was taken at")
			}
			if got, err := snap.Text(2, snap.Len()-1); err != nil || got != want[2:len(want)-1] {
				t.Errorf("Text of a snapshot: %v", err)
			}
		}()
	}
	wg.Wait()
}

func FuzzEdits(f *testing.F) {
	f.Add([]byte("hello\nworld"), []byte{0, 3, 'x', 1, 2, 4, 2, 1, 3, 'y', 'z'})
	f.Add([]byte(""), []byte{0, 0, '\n', 0, 0, 'a'})
	f.Fuzz(func(t *testing.T, init, script []byte) {
		model := strings.ToValidUTF8(string(init), "?")
		b := New(model)
		var snap Snapshot
		var snapText string
		for i := 0; i+2 < len(script); i += 3 {
			op, a, c := script[i]%3, int(script[i+1]), script[i+2]
			start := a % (len(model) + 1)
			text := string([]byte{'a' + c%26, '\n'})[:1+int(c)%2]
			switch op {
			case 0:
				must(b.Insert(start, text))
				model = model[:start] + text + model[start:]
			case 1:
				end := min(len(model), start+int(c)%9)
				must(b.Delete(start, end))
				model = model[:start] + model[end:]
			default:
				end := min(len(model), start+int(c)%9)
				must(b.Replace(start, end, text))
				model = model[:start] + text + model[end:]
			}
			checkTree(t, b, model)
			if i%2 == 0 {
				snap, snapText = b.Snapshot(), model
			}
		}
		if snapText != "" && snap.String() != snapText {
			t.Fatal("a snapshot changed")
		}
	})
}

func TestTypingGrowsOnePiece(t *testing.T) {
	b := New("")
	for i := 0; i < 3000; i++ {
		must(b.Insert(i, "x"))
	}
	if n := b.root.size; n != 3000 || b.root.left != nil || b.root.right != nil {
		t.Errorf("typing 3000 bytes made more than one piece (size %d)", n)
	}
	// Past the chunk, a new chunk is started and the text is still one tree.
	must(b.Insert(3000, strings.Repeat("y", 5000)))
	checkTree(t, b, strings.Repeat("x", 3000)+strings.Repeat("y", 5000))
}
