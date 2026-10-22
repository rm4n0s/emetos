package edit

import (
	"math/rand"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// offsetPosition is the position of a byte offset in text.
func offsetPosition(text string, off int) buffer.Position {
	line := strings.Count(text[:off], "\n")
	return buffer.Position{Line: line, Col: off - (strings.LastIndexByte(text[:off], '\n') + 1)}
}

func positionOffset(text string, p buffer.Position) int {
	off := 0
	for i := 0; i < p.Line; i++ {
		off += strings.IndexByte(text[off:], '\n') + 1
	}
	return off + p.Col
}

// randomText makes a text of a few short lines of characters of every width.
func randomText(r *rand.Rand) string {
	alphabet := []string{"a", "b", " ", "\n", "é", "😍", "x", "\n"}
	var sb strings.Builder
	for n := r.Intn(30); n > 0; n-- {
		sb.WriteString(alphabet[r.Intn(len(alphabet))])
	}
	return sb.String()
}

// boundaries are the byte offsets of text where a cursor may be.
func boundaries(text string) []int {
	var out []int
	for i := 0; i <= len(text); {
		out = append(out, i)
		if i == len(text) {
			break
		}
		_, w := utf8.DecodeRuneInString(text[i:])
		i += w
	}
	return out
}

// randomCarets picks distinct places for cursors, in a random order.
func randomCarets(r *rand.Rand, text string) []int {
	b := boundaries(text)
	r.Shuffle(len(b), func(i, j int) { b[i], b[j] = b[j], b[i] })
	return b[:1+r.Intn(min(len(b), 5))]
}

func caretsAt(text string, offs []int) []buffer.Selection {
	var sels []buffer.Selection
	for _, o := range offs {
		sels = append(sels, buffer.NewCursor(offsetPosition(text, o)))
	}
	return sels
}

// check compares the result of a command with the text and the cursor offsets
// that a plain string model gives.
func checkRandom(t *testing.T, seed int, before string, res Result, wantText string, wantOffs []int) {
	t.Helper()
	d := newEd(t, DefaultOptions(), before)
	if len(res.Edits) > 0 {
		if _, err := d.doc.Edit(res.Edits, editOptionsOf(res)); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
	}
	if got := d.doc.Text(); got != wantText {
		t.Fatalf("seed %d: text %q, want %q (before %q)", seed, got, wantText, before)
	}
	var got []int
	for _, s := range res.Selections {
		if s.Anchor != s.Active {
			t.Fatalf("seed %d: selection %+v is not empty", seed, s)
		}
		got = append(got, positionOffset(wantText, s.Active))
	}
	if len(got) != len(wantOffs) {
		t.Fatalf("seed %d: cursors %v, want %v", seed, got, wantOffs)
	}
	for i := range got {
		if got[i] != wantOffs[i] {
			t.Fatalf("seed %d: cursors %v, want %v (text %q, before %q)", seed, got, wantOffs, wantText, before)
		}
	}
}

func TestRandomMultiCursorTyping(t *testing.T) {
	for seed := 0; seed < 400; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		text := randomText(r)
		offs := randomCarets(r, text)
		ins := []string{"-", "xy", "é", "😍"}[r.Intn(4)]
		cmd := NewCommands(NewModel(newEd(t, DefaultOptions(), text).doc.Snapshot()), DefaultOptions())
		res := cmd.Type(caretsAt(text, offs), ins)

		sorted := append([]int(nil), offs...)
		sort.Ints(sorted)
		var sb strings.Builder
		prev := 0
		after := map[int]int{}
		for i, o := range sorted {
			sb.WriteString(text[prev:o])
			sb.WriteString(ins)
			prev = o
			after[o] = o + (i+1)*len(ins)
		}
		sb.WriteString(text[prev:])
		var want []int
		for _, o := range offs {
			want = append(want, after[o])
		}
		checkRandom(t, seed, text, res, sb.String(), want)
	}
}

func TestRandomMultiCursorDeleting(t *testing.T) {
	opts := DefaultOptions()
	opts.UseTabStops = false
	for seed := 0; seed < 400; seed++ {
		r := rand.New(rand.NewSource(int64(1000 + seed)))
		text := randomText(r)
		offs := randomCarets(r, text)
		left := r.Intn(2) == 0
		cmd := NewCommands(NewModel(newEd(t, opts, text).doc.Snapshot()), opts)
		var res Result
		if left {
			res = cmd.DeleteLeft(caretsAt(text, offs))
		} else {
			res = cmd.DeleteRight(caretsAt(text, offs))
		}
		// The string model: delete the character before or after each cursor.
		type span struct{ from, to int }
		spans := map[int]span{}
		for _, o := range offs {
			switch {
			case left && o > 0:
				_, w := utf8.DecodeLastRuneInString(text[:o])
				spans[o] = span{o - w, o}
			case !left && o < len(text):
				_, w := utf8.DecodeRuneInString(text[o:])
				spans[o] = span{o, o + w}
			default:
				spans[o] = span{o, o}
			}
		}
		sorted := append([]int(nil), offs...)
		sort.Ints(sorted)
		var sb strings.Builder
		prev, removed := 0, 0
		after := map[int]int{}
		for _, o := range sorted {
			s := spans[o]
			sb.WriteString(text[prev:s.from])
			prev = s.to
			removed += s.to - s.from
			after[o] = s.from - (removed - (s.to - s.from))
		}
		sb.WriteString(text[prev:])
		var want []int
		for _, o := range offs {
			want = append(want, after[o])
		}
		checkRandom(t, seed, text, res, sb.String(), want)
	}
}

// randomLines makes a few lines of letters, some with white space or empty.
func randomLines(r *rand.Rand) []string {
	words := []string{"", "a", "b", " c", "dd", "Ee", "é", "x y", "  ", "ß"}
	lines := make([]string, 2+r.Intn(6))
	for i := range lines {
		lines[i] = words[r.Intn(len(words))]
	}
	return lines
}

func TestRandomMoveLinesRoundTrip(t *testing.T) {
	for seed := 0; seed < 300; seed++ {
		r := rand.New(rand.NewSource(int64(5000 + seed)))
		lines := randomLines(r)
		text := strings.Join(lines, "\n")
		a := r.Intn(len(lines))
		b := a + r.Intn(len(lines)-a)
		// A selection of whole non-empty lines can be moved down and up again.
		if b+1 >= len(lines) {
			continue
		}
		allText := true
		for _, l := range lines[a : b+1] {
			if l == "" {
				allText = false
			}
		}
		if !allText || lines[b+1] == "" {
			continue
		}
		e := newEd(t, DefaultOptions(), lines...)
		e.at(sel4{a + 1, 1, b + 1, len(lines[b]) + 1})
		before := append([]buffer.Selection(nil), e.sels...)
		e.run(func(c Commands, s []buffer.Selection) Result { return c.MoveLines(s, true) })
		if e.doc.Text() == text && len(lines[b+1]) > 0 && lines[b+1] != strings.Join(lines[a:b+1], "\n") {
			t.Fatalf("seed %d: moving down changed nothing in %q", seed, text)
		}
		e.run(func(c Commands, s []buffer.Selection) Result { return c.MoveLines(s, false) })
		if e.doc.Text() != text {
			t.Fatalf("seed %d: down then up gave %q, want %q", seed, e.doc.Text(), text)
		}
		if !buffer.SelectionsEqual(e.sels, before) {
			t.Fatalf("seed %d: selection %+v, want %+v", seed, e.sels, before)
		}
	}
}

func TestRandomCopyThenDeleteLines(t *testing.T) {
	for seed := 0; seed < 300; seed++ {
		r := rand.New(rand.NewSource(int64(7000 + seed)))
		lines := randomLines(r)
		text := strings.Join(lines, "\n")
		n := r.Intn(len(lines))
		e := newEd(t, DefaultOptions(), lines...).caret(n+1, 1)
		e.run(func(c Commands, s []buffer.Selection) Result { return c.CopyLines(s, true) })
		if got := len(strings.Split(e.doc.Text(), "\n")); got != len(lines)+1 {
			t.Fatalf("seed %d: %d lines after a copy, want %d", seed, got, len(lines)+1)
		}
		// The cursor is on the copy: deleting that line gives the text back.
		e.run(deleteLines)
		if e.doc.Text() != text {
			t.Fatalf("seed %d: copy then delete gave %q, want %q", seed, e.doc.Text(), text)
		}
	}
}

func TestRandomSortLinesIsIdempotent(t *testing.T) {
	for seed := 0; seed < 300; seed++ {
		r := rand.New(rand.NewSource(int64(9000 + seed)))
		lines := randomLines(r)
		e := newEd(t, DefaultOptions(), lines...)
		all := func() *ed {
			n := len(strings.Split(e.doc.Text(), "\n"))
			return e.at(sel4{1, 1, n, len(e.line(n)) + 1})
		}
		all().run(sortCmd(false))
		once := e.doc.Text()
		if got := strings.Split(once, "\n"); len(got) != len(lines) {
			t.Fatalf("seed %d: %d lines after sorting %d", seed, len(got), len(lines))
		}
		all().run(sortCmd(false))
		if e.doc.Text() != once {
			t.Fatalf("seed %d: sorting twice gave %q, once %q", seed, e.doc.Text(), once)
		}
		if strings.HasPrefix(once, "\n") || strings.Contains(once, "\n\n") {
			// An empty line can end up last, and a selection that ends in an empty
			// line leaves it out, so the check below does not apply.
			continue
		}
		all().run(sortCmd(true))
		all().run(sortCmd(false))
		if e.doc.Text() != once {
			t.Fatalf("seed %d: descending then ascending gave %q, want %q", seed, e.doc.Text(), once)
		}
	}
}
