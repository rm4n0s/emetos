package emulator

import (
	"strings"
	"sync"
	"testing"
)

func TestSnapshot(t *testing.T) {
	t.Run("the first snapshot copies everything", func(t *testing.T) {
		r := newRig(4, 10)
		r.feed("abc")
		var f Frame
		assertEq(t, "changed", r.Snapshot(&f), true)
		assertEq(t, "full", f.Full, true)
		assertEq(t, "rows", f.Rows, 4)
		assertEq(t, "text", f.RowText(0), "abc")
		assertEq(t, "cursor x", f.Cursor.X, 3)
		assertEq(t, "visible", f.Cursor.Visible, true)
	})
	t.Run("nothing changed", func(t *testing.T) {
		r := newRig(4, 10)
		var f Frame
		r.Snapshot(&f)
		assertEq(t, "changed", r.Snapshot(&f), false)
		assertEq(t, "flag", f.Changed, false)
	})
	t.Run("only the rows that changed are copied", func(t *testing.T) {
		r := newRig(4, 10)
		var f Frame
		r.Snapshot(&f)
		r.feed("\x1b[3;1Hxyz")
		assertEq(t, "changed", r.Snapshot(&f), true)
		assertEq(t, "full", f.Full, false)
		assertEq(t, "dirty", strings.Join([]string{b2s(f.Dirty[0]), b2s(f.Dirty[1]), b2s(f.Dirty[2]), b2s(f.Dirty[3])}, ""), "FFTF")
		assertEq(t, "text", f.RowText(2), "xyz")
		assertEq(t, "cursor", f.Cursor.X, 3)
	})
	t.Run("a cursor move changes the cursor only", func(t *testing.T) {
		r := newRig(4, 10)
		var f Frame
		r.Snapshot(&f)
		r.feed("\x1b[2;2H")
		assertEq(t, "changed", r.Snapshot(&f), true)
		for y, d := range f.Dirty {
			assertEq(t, "dirty "+itoa(y), d, false)
		}
		assertEq(t, "cursor", f.Cursor.Y, 1)
	})
	t.Run("a scroll marks everything", func(t *testing.T) {
		r := newRig(3, 10)
		var f Frame
		r.Snapshot(&f)
		r.feed("1\r\n2\r\n3\r\n4")
		r.Snapshot(&f)
		assertEq(t, "full", f.Full, true)
		assertEq(t, "row 0", f.RowText(0), "2")
		assertEq(t, "scrollback", f.Scrollback, 1)
		assertEq(t, "top", f.Top, int64(1))
	})
	t.Run("a resize marks everything and sizes the frame", func(t *testing.T) {
		r := newRig(3, 10)
		var f Frame
		r.Snapshot(&f)
		r.Resize(5, 7)
		r.Snapshot(&f)
		assertEq(t, "full", f.Full, true)
		assertEq(t, "rows", f.Rows, 5)
		assertEq(t, "cols", f.Cols, 7)
		assertEq(t, "cells", len(f.Cells), 35)
	})
	t.Run("the other screen marks everything", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("main")
		var f Frame
		r.Snapshot(&f)
		r.feed("\x1b[?1049hALT")
		r.Snapshot(&f)
		assertEq(t, "full", f.Full, true)
		assertEq(t, "text", f.RowText(0), "    ALT")
		assertEq(t, "alt mode", f.Modes.AltScreen, true)
		r.feed("\x1b[?1049l")
		r.Snapshot(&f)
		assertEq(t, "text back", f.RowText(0), "main")
	})
	t.Run("a new palette marks everything", func(t *testing.T) {
		r := newRig(3, 10)
		var f Frame
		r.Snapshot(&f)
		r.feed("\x1b]4;1;#123456\x07")
		r.Snapshot(&f)
		assertEq(t, "palette", f.Palette[1], uint32(0x123456))
	})
	t.Run("cursor look and modes", func(t *testing.T) {
		r := newRig(3, 10)
		var f Frame
		r.feed("\x1b[5 q\x1b[?25l\x1b[?2004h\x1b[?1000h\x1b[?1006h")
		r.Snapshot(&f)
		assertEq(t, "shape", f.Cursor.Shape, CursorBar)
		assertEq(t, "blink", f.Cursor.Blink, true)
		assertEq(t, "visible", f.Cursor.Visible, false)
		assertEq(t, "paste", f.Modes.BracketedPaste, true)
		assertEq(t, "mouse", f.Modes.Mouse, MouseNormal)
		assertEq(t, "encoding", f.Modes.MouseEnc, MouseEncSGR)
		r.feed("\x1b[2 q")
		r.Snapshot(&f)
		assertEq(t, "steady", f.Cursor.Blink, false)
		assertEq(t, "block", f.Cursor.Shape, CursorBlock)
	})
	t.Run("the pending wrap shows in the cursor", func(t *testing.T) {
		r := newRig(3, 5)
		var f Frame
		r.feed("abcde")
		r.Snapshot(&f)
		assertEq(t, "pending", f.Cursor.WrapPending, true)
		assertEq(t, "x", f.Cursor.X, 4)
	})
	t.Run("clusters and styles come with the rows", func(t *testing.T) {
		r := newRig(3, 10)
		var f Frame
		r.feed("\x1b[1;32me\u0301")
		r.Snapshot(&f)
		assertEq(t, "text", f.Text(0, 0), "e\u0301")
		assertEq(t, "style", f.StyleOf(f.Cell(0, 0)), Style{Attrs: AttrBold, Fg: IndexedColor(2)})
	})
	t.Run("synchronized output holds the frame", func(t *testing.T) {
		r := newRig(3, 10)
		var f Frame
		r.feed("old")
		r.Snapshot(&f)
		r.feed("\x1b[?2026h\x1b[2Jnew")
		assertEq(t, "changed", r.Snapshot(&f), false)
		assertEq(t, "synchronized", f.Synchronized, true)
		assertEq(t, "old picture", f.RowText(0), "old")
		r.feed("\x1b[?2026l")
		assertEq(t, "changed after", r.Snapshot(&f), true)
		assertEq(t, "synchronized after", f.Synchronized, false)
		assertEq(t, "new picture", f.RowText(0), "   new")
	})
	t.Run("a synchronized update the program left open can be ended", func(t *testing.T) {
		r := newRig(3, 10)
		var f Frame
		r.Snapshot(&f)
		r.feed("\x1b[?2026hx")
		assertEq(t, "held", r.Snapshot(&f), false)
		r.EndSynchronizedUpdate()
		assertEq(t, "released", r.Snapshot(&f), true)
		assertEq(t, "text", f.RowText(0), "x")
	})
	t.Run("a compaction rebuilds the frame", func(t *testing.T) {
		r := newRig(2, 80)
		var f Frame
		r.Snapshot(&f)
		for i := 0; i < MaxStyles+10; i++ {
			r.feed("\x1b[38;2;" + itoa(i%256) + ";" + itoa((i/256)%256) + ";" + itoa(i/65536) + "m\x1b[1;1Hx")
		}
		r.Snapshot(&f)
		assertEq(t, "full", f.Full, true)
		if int(f.Cell(0, 0).Style) >= len(f.Styles) {
			t.Errorf("style %d of %d", f.Cell(0, 0).Style, len(f.Styles))
		}
	})
}

func b2s(b bool) string {
	if b {
		return "T"
	}
	return "F"
}

// TestReadLines: the scrollback for the user interface, search and selection.
func TestReadLines(t *testing.T) {
	r := newRigSB(3, 10, 10)
	r.feed("one\r\ntwo\r\nthree\r\nfour\r\nfive")
	var f Frame
	r.ReadLines(0, 5, &f)
	assertEq(t, "rows", f.Rows, 5)
	assertEq(t, "first", f.RowText(0), "one")
	assertEq(t, "last", f.RowText(4), "five")
	assertEq(t, "full", f.Full, true)
	assertEq(t, "top", f.Top, int64(0))
	r.ReadLines(3, 10, &f)
	assertEq(t, "clipped", f.Rows, 2)
	assertEq(t, "first of the clipped", f.RowText(0), "four")
	assertEq(t, "cursor row", f.Cursor.Y, 1)
	r.ReadLines(-5, 2, &f)
	assertEq(t, "negative start", f.RowText(0), "one")
	r.ReadLines(99, 2, &f)
	assertEq(t, "past the end", f.Rows, 0)
	assertEq(t, "LineCount", r.LineCount(), 5)
	text, _ := r.LineText(2)
	assertEq(t, "LineText", text, "three")
	text, _ = r.LineText(99)
	assertEq(t, "LineText out of range", text, "")
}

func TestText(t *testing.T) {
	r := newRig(3, 5)
	r.feed("abcdefghij\r\nxy   \r\nz")
	assertEq(t, "joined wraps, split lines", r.Text(0, r.LineCount()-1), "abcdefghij\nxy\nz")
	assertEq(t, "part", r.Text(1, 1), "fghij")
}

// TestSnapshotAllocations: an idle snapshot allocates nothing, and neither does a write that changes one row followed by a
// snapshot (SPEC §14 "Terminal memory").
func TestSnapshotAllocations(t *testing.T) {
	r := newRigSB(45, 160, 10000)
	r.feed(strings.Repeat("some text on a line\r\n", 100))
	var f Frame
	r.Snapshot(&f)
	if n := testing.AllocsPerRun(200, func() { r.Snapshot(&f) }); n != 0 {
		t.Errorf("idle snapshot allocates %v times", n)
	}
	line := []byte("\x1b[40;1Hjust one more line of text\x1b[31m red")
	if n := testing.AllocsPerRun(200, func() {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
		r.Snapshot(&f)
	}); n != 0 {
		t.Errorf("write and snapshot allocate %v times", n)
	}
	if n := testing.AllocsPerRun(200, func() {
		if _, err := r.Write([]byte("\r\nscrolling line\x1b[1;32m!\x1b[m")); err != nil {
			t.Fatal(err)
		}
		r.Snapshot(&f)
	}); n != 0 {
		t.Errorf("scrolling write and snapshot allocate %v times", n)
	}
}

// TestConcurrentUse: the owner goroutine writes while the user interface takes snapshots (run with -race).
func TestConcurrentUse(t *testing.T) {
	e := New(24, 80, Options{Scrollback: 500})
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer close(done)
		for i := 0; i < 3000; i++ {
			if _, err := e.Write([]byte("line " + itoa(i) + " \x1b[1;3" + itoa(i%8) + "mcolor\x1b[0m あ\r\n")); err != nil {
				t.Error(err)
				return
			}
			if i%500 == 0 {
				e.Resize(20+i/500, 70+i/100)
			}
			if i%700 == 0 {
				e.SetScrollback(100 + i/10)
			}
		}
	}()
	go func() {
		defer wg.Done()
		var f Frame
		var lines Frame
		for {
			e.Snapshot(&f)
			e.ReadLines(0, 10, &lines)
			e.EncodeMouse(MouseEvent{}, nil)
			e.EncodeKey(KeyEvent{Key: KeyUp}, nil)
			_ = e.Serialize(50)
			_, _ = e.LineText(3)
			select {
			case <-done:
				return
			default:
			}
		}
	}()
	wg.Wait()
}

// TestTextRange: the text of a selection, from absolute line numbers that survive the scrollback dropping lines.
func TestTextRange(t *testing.T) {
	r := newRigSB(3, 10, 5)
	r.feed("alpha\r\nbeta\r\ngamma\r\ndelta\r\nepsilon wrapped!!")
	// the screen holds "delta", "epsilon wr" and "apped!!"; the scrollback "alpha", "beta", "gamma"
	first := r.FirstLine()
	for _, tc := range []struct {
		name string
		l0   int64
		c0   int
		l1   int64
		c1   int
		want string
	}{
		{"one row", first, 1, first, 4, "lph"},
		{"two rows", first, 2, first + 1, 2, "pha\nbe"},
		{"spaces at the end of a row go", first, 0, first + 1, 10, "alpha\nbeta"},
		{"wrapped rows are joined", first + 4, 0, first + 5, 7, "epsilon wrapped!!"},
		{"backwards is empty", first + 1, 3, first, 0, ""},
		{"empty", first, 2, first, 2, ""},
		{"before the buffer", first - 10, 0, first - 5, 3, ""},
		{"past the end", first + 100, 0, first + 101, 3, ""},
		{"starts before the buffer", first - 3, 0, first, 3, "alp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertEq(t, "text", r.TextRange(tc.l0, tc.c0, tc.l1, tc.c1), tc.want)
		})
	}
	// lines dropped from the scrollback move FirstLine, not the numbers of the others
	r.feed("\r\none\r\ntwo\r\nthree")
	if got := r.FirstLine(); got <= first {
		t.Fatalf("FirstLine = %d after the scrollback dropped lines, was %d", got, first)
	}
	assertEq(t, "a line that is still there", r.TextRange(first+4, 0, first+5, 7), "epsilon wrapped!!")
}
