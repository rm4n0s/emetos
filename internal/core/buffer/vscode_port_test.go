package buffer

import (
	"strings"
	"testing"
)

// vsOp is one step of a scripted test: an insert of text at a byte offset, or a
// delete of n bytes from it (VS Code's PieceTreeBase.insert and delete).
type vsOp struct {
	insert bool
	at     int
	text   string
	n      int
}

// vsScript is a scripted test: a buffer made from chunks, then the steps.
type vsScript struct {
	name   string
	chunks []string
	ops    []vsOp
}

// Ported from VS Code: reference/vscode/src/vs/editor/test/common/model/pieceTreeTextBuffer/pieceTreeTextBuffer.test.ts
// The scripted cases (vscode_scripts_test.go is converted from that file) run
// the steps on the Buffer and on a plain string. After the creation and after
// every step the Buffer must equal the string, the tree must be valid, and the
// line index must agree with the string (what VS Code's testLinesContent,
// testLineStarts and assertTreeInvariants check, and more).
func TestVSCodeScripts(t *testing.T) {
	for _, sc := range vscodeScripts {
		t.Run(sc.name, func(t *testing.T) {
			var bld Builder
			for _, c := range sc.chunks {
				bld.WriteString(c)
			}
			b := bld.Buffer()
			model := strings.Join(sc.chunks, "")
			checkAll(t, b, model)
			for i, op := range sc.ops {
				var err error
				if op.insert {
					err = b.Insert(op.at, op.text)
					if err == nil {
						model = model[:op.at] + op.text + model[op.at:]
					}
				} else {
					err = b.Delete(op.at, op.at+op.n)
					if err == nil {
						model = model[:op.at] + model[op.at+op.n:]
					}
				}
				if err != nil {
					t.Fatalf("step %d (%+v): %v", i, op, err)
				}
				checkAll(t, b, model)
			}
		})
	}
}

// vsPos writes a VS Code position (1-based line and column) as an Emetos one;
// the texts of these cases are ASCII, so columns are the same as byte columns.
func vsPos(line, col int) Position { return Position{Line: line - 1, Col: col - 1} }

// Ported from VS Code: pieceTreeTextBuffer.test.ts, suite 'prefix sum for line
// feed' ('basic', 'append', 'insert', 'delete', 'add+delete 1').
func TestVSCodePrefixSums(t *testing.T) {
	type pair struct {
		offset int
		line   int
		col    int
	}
	tests := []struct {
		name  string
		init  string
		edits func(*Buffer)
		text  string
		lines int
		pairs []pair
	}{
		{
			name: "basic", init: "1\n2\n3\n4", edits: func(*Buffer) {},
			text: "1\n2\n3\n4", lines: 4,
			pairs: []pair{{0, 1, 1}, {1, 1, 2}, {2, 2, 1}, {3, 2, 2}, {4, 3, 1}, {5, 3, 2}, {6, 4, 1}},
		},
		{
			name: "append", init: "a\nb\nc\nde", edits: func(b *Buffer) { must(b.Insert(8, "fh\ni\njk")) },
			text: "a\nb\nc\ndefh\ni\njk", lines: 6,
			pairs: []pair{{0, 1, 1}, {9, 4, 4}},
		},
		{
			name: "insert", init: "a\nb\nc\nde", edits: func(b *Buffer) { must(b.Insert(7, "fh\ni\njk")) },
			text: "a\nb\nc\ndfh\ni\njke", lines: 6,
			pairs: []pair{{6, 4, 1}, {7, 4, 2}, {8, 4, 3}, {9, 4, 4}, {12, 6, 1}, {13, 6, 2}, {14, 6, 3}},
		},
		{
			name: "delete", init: "a\nb\nc\ndefh\ni\njk", edits: func(b *Buffer) { must(b.Delete(7, 9)) },
			text: "a\nb\nc\ndh\ni\njk", lines: 6,
			pairs: []pair{{6, 4, 1}, {7, 4, 2}, {8, 4, 3}, {9, 5, 1}, {11, 6, 1}, {12, 6, 2}, {13, 6, 3}},
		},
		{
			name: "add+delete 1", init: "a\nb\nc\nde",
			edits: func(b *Buffer) { must(b.Insert(8, "fh\ni\njk")); must(b.Delete(7, 9)) },
			text:  "a\nb\nc\ndh\ni\njk", lines: 6,
			pairs: []pair{{6, 4, 1}, {7, 4, 2}, {8, 4, 3}, {9, 5, 1}, {11, 6, 1}, {12, 6, 2}, {13, 6, 3}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(tt.init)
			tt.edits(b)
			checkAll(t, b, tt.text)
			if b.LineCount() != tt.lines {
				t.Errorf("LineCount = %d, want %d", b.LineCount(), tt.lines)
			}
			for _, p := range tt.pairs {
				pos := vsPos(p.line, p.col)
				if got := b.PositionAt(p.offset); got != pos {
					t.Errorf("PositionAt(%d) = %+v, want %+v", p.offset, got, pos)
				}
				if got := b.OffsetAt(pos); got != p.offset {
					t.Errorf("OffsetAt(%+v) = %d, want %d", pos, got, p.offset)
				}
			}
		})
	}
}

// Ported from VS Code: pieceTreeTextBuffer.test.ts, suite 'get text in range'
// ('getContentInRange', 'get line content', 'get line content basic', 'get line
// content after inserts/deletes'). VS Code's getLineRawContent keeps the line
// feed, and its raw range reaches past the end of a line to take it; here that
// is the range from the start of a line to the start of the next, and
// LineContent has no line feed.
func TestVSCodeLineContent(t *testing.T) {
	t.Run("get line content", func(t *testing.T) {
		b := New("1")
		if got, _ := b.LineContent(0); got != "1" {
			t.Errorf("LineContent(0) = %q", got)
		}
		must(b.Insert(1, "2"))
		if got, _ := b.LineContent(0); got != "12" {
			t.Errorf("LineContent(0) = %q", got)
		}
		checkAll(t, b, "12")
	})
	raw := func(b *Buffer, line int) string {
		if line+1 < b.LineCount() {
			return b.Text(Range{Start: Position{line, 0}, End: Position{line + 1, 0}})
		}
		return b.Text(Range{Start: Position{line, 0}, End: Position{line, 1 << 30}})
	}
	t.Run("basic", func(t *testing.T) {
		b := New("1\n2\n3\n4")
		for i, want := range []string{"1\n", "2\n", "3\n", "4"} {
			if got := raw(b, i); got != want {
				t.Errorf("line %d = %q, want %q", i, got, want)
			}
			if got, _ := b.LineContent(i); got != strings.TrimSuffix(want, "\n") {
				t.Errorf("LineContent(%d) = %q", i, got)
			}
		}
	})
	t.Run("after inserts and deletes, and getContentInRange", func(t *testing.T) {
		b := New("a\nb\nc\nde")
		must(b.Insert(8, "fh\ni\njk"))
		must(b.Delete(7, 9))
		checkAll(t, b, "a\nb\nc\ndh\ni\njk")
		for i, want := range []string{"a\n", "b\n", "c\n", "dh\n", "i\n", "jk"} {
			if got := raw(b, i); got != want {
				t.Errorf("line %d = %q, want %q", i, got, want)
			}
		}
	})
}

// edit applies a VS Code style edit (a range of positions and a text) to b.
func edit(b *Buffer, r Range, text string) {
	must(b.Replace(b.OffsetAt(r.Start), b.OffsetAt(r.End), text))
}

// Ported from VS Code: pieceTreeTextBuffer.test.ts, suite 'snapshot' ('bug
// #45564, piece tree pieces should be immutable', 'immutable snapshot 1', '2',
// '3'). The VS Code tests go through a TextModel; here the edits are made on the
// Buffer with the same ranges, and a Snapshot stands for createSnapshot.
func TestVSCodeSnapshots(t *testing.T) {
	r := func(sl, sc, el, ec int) Range { return Range{Start: vsPos(sl, sc), End: vsPos(el, ec)} }
	t.Run("bug 45564", func(t *testing.T) {
		b := New("\n")
		edit(b, r(2, 1, 2, 1), "!")
		snapshot, snapshot1 := b.Snapshot(), b.Snapshot()
		if b.String() != snapshot.String() {
			t.Errorf("%q != %q", b.String(), snapshot.String())
		}
		edit(b, r(2, 1, 2, 2), "")
		edit(b, r(2, 1, 2, 1), "!")
		if b.String() != snapshot1.String() {
			t.Errorf("%q != %q", b.String(), snapshot1.String())
		}
	})
	t.Run("immutable snapshot 1", func(t *testing.T) {
		b := New("abc\ndef")
		snapshot := b.Snapshot()
		edit(b, r(2, 1, 2, 4), "")
		edit(b, r(1, 1, 2, 1), "abc\ndef")
		if b.String() != snapshot.String() {
			t.Errorf("%q != %q", b.String(), snapshot.String())
		}
	})
	t.Run("immutable snapshot 2", func(t *testing.T) {
		b := New("abc\ndef")
		snapshot := b.Snapshot()
		edit(b, r(2, 1, 2, 1), "!")
		edit(b, r(2, 1, 2, 2), "")
		if b.String() != snapshot.String() {
			t.Errorf("%q != %q", b.String(), snapshot.String())
		}
	})
	t.Run("immutable snapshot 3", func(t *testing.T) {
		b := New("abc\ndef")
		edit(b, r(2, 4, 2, 4), "!")
		snapshot := b.Snapshot()
		edit(b, r(2, 5, 2, 5), "!")
		if b.String() == snapshot.String() {
			t.Error("the snapshot followed the edit")
		}
		if snapshot.String() != "abc\ndef!" {
			t.Errorf("snapshot = %q", snapshot.String())
		}
	})
}
