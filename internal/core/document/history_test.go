package document

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/fileio"
)

func cursor(line, col int) []buffer.Selection {
	return []buffer.Selection{buffer.NewCursor(pos(line, col))}
}

// typeAt types one character at a column with the cursor before and after, as
// the typing command will.
func typeAt(t *testing.T, d *Document, col int, s string) {
	t.Helper()
	edit(t, d, []Edit{ins(0, col, s)}, EditOptions{
		Kind: KindTyping, Before: cursor(0, col),
		After: func([]Edit) []buffer.Selection { return cursor(0, col+len(s)) },
	})
}

func TestTypingJoinsOneGroup(t *testing.T) {
	d := newDoc("")
	for i, c := range "hello" {
		typeAt(t, d, i, string(c))
	}
	if g := d.history.top(); len(d.history.undo) != 1 || len(g.steps) != 1 || len(g.steps[0].changes) != 1 {
		t.Errorf("typing 5 characters made %d groups, %d steps: %+v", len(d.history.undo), len(g.steps), g)
	}
	if got := d.Text(); got != "hello" {
		t.Fatalf("text = %q", got)
	}
	res := undo(t, d)
	if d.Text() != "" || !res.Done {
		t.Errorf("one Undo left %q", d.Text())
	}
	if len(res.Selections) != 1 || res.Selections[0] != buffer.NewCursor(pos(0, 0)) {
		t.Errorf("Undo restored %+v", res.Selections)
	}
	res = redo(t, d)
	if d.Text() != "hello" || len(res.Selections) != 1 || res.Selections[0] != buffer.NewCursor(pos(0, 5)) {
		t.Errorf("Redo gave %q and %+v", d.Text(), res.Selections)
	}
}

func TestDeletingJoinsOneGroup(t *testing.T) {
	left := newDoc("abcdef")
	for col := 6; col > 3; col-- { // backspace three times from the end
		edit(t, left, []Edit{{Range: rg(0, col-1, 0, col)}}, EditOptions{Kind: KindDeleteLeft})
	}
	right := newDoc("abcdef")
	for i := 0; i < 3; i++ { // Delete three times at column 1
		edit(t, right, []Edit{{Range: rg(0, 1, 0, 2)}}, EditOptions{Kind: KindDeleteRight})
	}
	for name, d := range map[string]*Document{"backspace": left, "delete": right} {
		g := d.history.top()
		if len(d.history.undo) != 1 || len(g.steps) != 1 || len(g.steps[0].changes) != 1 {
			t.Errorf("%s: %d groups, %+v", name, len(d.history.undo), g)
		}
	}
	if left.Text() != "abc" || right.Text() != "aef" {
		t.Fatalf("texts = %q, %q", left.Text(), right.Text())
	}
	undo(t, left)
	undo(t, right)
	if left.Text() != "abcdef" || right.Text() != "abcdef" {
		t.Errorf("after Undo: %q, %q", left.Text(), right.Text())
	}
}

func TestGroupBoundaries(t *testing.T) {
	t.Run("other never joins", func(t *testing.T) {
		d := newDoc("")
		for i := 0; i < 3; i++ {
			edit(t, d, []Edit{ins(0, i, "x")}, EditOptions{Kind: KindOther})
		}
		if len(d.history.undo) != 3 {
			t.Errorf("%d groups, want 3", len(d.history.undo))
		}
	})
	t.Run("a cursor that moved starts a group", func(t *testing.T) {
		d := newDoc("0123456789")
		typeAt(t, d, 2, "a")
		typeAt(t, d, 3, "b") // right after the first: joins
		typeAt(t, d, 8, "c") // the cursor was moved: does not
		if len(d.history.undo) != 2 {
			t.Fatalf("%d groups, want 2", len(d.history.undo))
		}
		undo(t, d)
		if got := d.Text(); got != "01ab23456789" {
			t.Errorf("after one Undo: %q", got)
		}
	})
	t.Run("unknown cursors don't prevent joining", func(t *testing.T) {
		d := newDoc("")
		edit(t, d, []Edit{ins(0, 0, "a")}, EditOptions{Kind: KindTyping})
		edit(t, d, []Edit{ins(0, 1, "b")}, EditOptions{Kind: KindTyping})
		if len(d.history.undo) != 1 {
			t.Errorf("%d groups, want 1", len(d.history.undo))
		}
	})
	t.Run("PushStackElement closes the group, PopStackElement reopens it", func(t *testing.T) {
		d := newDoc("")
		typeAt(t, d, 0, "a")
		d.PushStackElement()
		typeAt(t, d, 1, "b")
		if len(d.history.undo) != 2 {
			t.Fatalf("%d groups after PushStackElement, want 2", len(d.history.undo))
		}
		d.PushStackElement()
		d.PopStackElement()
		typeAt(t, d, 2, "c")
		if len(d.history.undo) != 2 {
			t.Errorf("%d groups after PopStackElement, want 2", len(d.history.undo))
		}
		undo(t, d)
		if got := d.Text(); got != "a" {
			t.Errorf("after Undo: %q, want %q", got, "a")
		}
	})
	t.Run("Push and Pop do nothing on an empty history", func(t *testing.T) {
		d := newDoc("")
		d.PushStackElement()
		d.PopStackElement()
		if d.CanUndo() || d.CanRedo() {
			t.Error("history is not empty")
		}
	})
	t.Run("a new edit drops the redo stack", func(t *testing.T) {
		d := newDoc("")
		edit(t, d, []Edit{ins(0, 0, "a")}, EditOptions{})
		undo(t, d)
		if !d.CanRedo() {
			t.Fatal("nothing to redo")
		}
		edit(t, d, []Edit{ins(0, 0, "b")}, EditOptions{})
		if d.CanRedo() {
			t.Error("redo survived a new edit")
		}
	})
	t.Run("an undone group is closed", func(t *testing.T) {
		d := newDoc("")
		typeAt(t, d, 0, "a")
		undo(t, d)
		redo(t, d)
		typeAt(t, d, 1, "b")
		if len(d.history.undo) != 2 {
			t.Errorf("%d groups, want 2: typing after a redo starts a new group", len(d.history.undo))
		}
	})
	t.Run("a line ending change is a group of its own", func(t *testing.T) {
		d := newDoc("")
		typeAt(t, d, 0, "a")
		if err := d.SetLineEnding(fileio.CRLF); err != nil {
			t.Fatal(err)
		}
		typeAt(t, d, 1, "b")
		if len(d.history.undo) != 3 {
			t.Errorf("%d groups, want 3", len(d.history.undo))
		}
	})
	t.Run("ClearHistory", func(t *testing.T) {
		d := newDoc("")
		edit(t, d, []Edit{ins(0, 0, "a")}, EditOptions{})
		undo(t, d)
		d.ClearHistory()
		if d.CanUndo() || d.CanRedo() {
			t.Error("history survived ClearHistory")
		}
	})
	t.Run("nothing to undo or redo", func(t *testing.T) {
		d := newDoc("a")
		if res := undo(t, d); res.Done {
			t.Error("Undo did something")
		}
		if res := redo(t, d); res.Done {
			t.Error("Redo did something")
		}
	})
}

// A typed character that replaces a selection is not an insert after the previous
// one, so it is not folded into its change; the group still undoes and redoes whole.
func TestFoldingRespectsReplacements(t *testing.T) {
	d := newDoc("hello world")
	typeAt(t, d, 11, "!")
	edit(t, d, []Edit{{Range: rg(0, 0, 0, 5), Text: "J"}}, EditOptions{Kind: KindTyping, Before: cursor(0, 12), After: func([]Edit) []buffer.Selection { return cursor(0, 1) }})
	typeAt(t, d, 1, "i")
	if got := d.Text(); got != "Ji world!" {
		t.Fatalf("text = %q", got)
	}
	undo(t, d)
	if got := d.Text(); got != "hello world" {
		t.Errorf("after one Undo: %q", got)
	}
	redo(t, d)
	if got := d.Text(); got != "Ji world!" {
		t.Errorf("after Redo: %q", got)
	}
}

func TestEventsForEdits(t *testing.T) {
	d := newDoc("one\ntwo")
	var got []ChangeEvent
	unsubscribe := d.Subscribe(func(ev ChangeEvent) { got = append(got, ev) })
	edit(t, d, []Edit{{Range: rg(0, 0, 0, 3), Text: "ONE!"}, ins(1, 3, "?")}, EditOptions{})
	if len(got) != 1 {
		t.Fatalf("%d events", len(got))
	}
	ev := got[0]
	if ev.Cause != CauseEdit || ev.Version != 2 || ev.AlternativeVersion != 2 || ev.Before.String() != "one\ntwo" || ev.After.String() != "ONE!\ntwo?" {
		t.Errorf("event = %+v", ev)
	}
	want := []Change{
		{OldRange: rg(0, 0, 0, 3), OldOffset: 0, OldText: "one", NewRange: rg(0, 0, 0, 4), NewOffset: 0, NewText: "ONE!"},
		{OldRange: rg(1, 3, 1, 3), OldOffset: 7, OldText: "", NewRange: rg(1, 3, 1, 4), NewOffset: 8, NewText: "?"},
	}
	if len(ev.Changes) != 2 || ev.Changes[0] != want[0] || ev.Changes[1] != want[1] {
		t.Errorf("changes = %+v", ev.Changes)
	}
	// The snapshots of the event keep their text.
	edit(t, d, []Edit{ins(0, 0, ">")}, EditOptions{})
	if ev.After.String() != "ONE!\ntwo?" {
		t.Error("a snapshot of an event changed")
	}
	unsubscribe()
	unsubscribe() // a second call does nothing
	edit(t, d, []Edit{ins(0, 0, "<")}, EditOptions{})
	if len(got) != 2 {
		t.Errorf("%d events, want 2: the listener was not removed", len(got))
	}
	// A no-op edit sends nothing.
	d.Subscribe(func(ChangeEvent) { t.Error("event for a no-op") })
	edit(t, d, []Edit{ins(0, 0, "")}, EditOptions{})
}

func TestEventsForUndoAndRedo(t *testing.T) {
	d := newDoc("a")
	// One group of two steps: an edit, then another that doesn't fold into it.
	edit(t, d, []Edit{ins(0, 1, "b")}, EditOptions{Kind: KindTyping})
	edit(t, d, []Edit{{Range: rg(0, 0, 0, 1), Text: "X"}}, EditOptions{Kind: KindTyping})
	if len(d.history.undo) != 1 || len(d.history.undo[0].steps) != 2 {
		t.Fatalf("history = %+v", d.history.undo)
	}
	var got []ChangeEvent
	d.Subscribe(func(ev ChangeEvent) { got = append(got, ev) })
	sel := cursor(0, 7)
	d.history.undo[0].selBefore = sel
	undo(t, d)
	if len(got) != 2 {
		t.Fatalf("%d events for an undo of 2 steps", len(got))
	}
	if got[0].Cause != CauseUndo || got[0].Before.String() != "Xb" || got[0].After.String() != "ab" ||
		got[1].Before.String() != "ab" || got[1].After.String() != "a" {
		t.Errorf("events = %q->%q, %q->%q", got[0].Before.String(), got[0].After.String(), got[1].Before.String(), got[1].After.String())
	}
	if got[0].Selections != nil || len(got[1].Selections) != 1 {
		t.Errorf("selections on events: %+v, %+v", got[0].Selections, got[1].Selections)
	}
	if got[0].AlternativeVersion != d.AlternativeVersion() || got[1].AlternativeVersion != d.AlternativeVersion() || got[1].Version != d.Version() {
		t.Errorf("versions on events: %d %d, document %d %d", got[0].AlternativeVersion, got[1].Version, d.AlternativeVersion(), d.Version())
	}
	got = nil
	redo(t, d)
	if len(got) != 2 || got[0].Cause != CauseRedo || got[1].After.String() != "Xb" {
		t.Errorf("redo events = %+v", got)
	}
}

func TestEventsForLineEndingAndSetText(t *testing.T) {
	d := newDoc("one\ntwo")
	var got []ChangeEvent
	d.Subscribe(func(ev ChangeEvent) { got = append(got, ev) })
	if err := d.SetLineEnding(fileio.CRLF); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Cause != CauseLineEnding || len(got[0].Changes) != 0 || got[0].After.String() != "one\ntwo" {
		t.Errorf("line ending event = %+v", got)
	}
	undo(t, d)
	if len(got) != 2 || got[1].Cause != CauseLineEnding {
		t.Errorf("undo of a line ending sent %+v", got[1:])
	}

	edit(t, d, []Edit{ins(0, 0, "x")}, EditOptions{})
	got = nil
	d.SetText("fresh\r\ntext")
	if d.Text() != "fresh\ntext" || d.CanUndo() || d.Dirty() {
		t.Errorf("after SetText: %q, can undo %v, dirty %v", d.Text(), d.CanUndo(), d.Dirty())
	}
	if len(got) != 1 || got[0].Cause != CauseSetText || len(got[0].Changes) != 1 ||
		got[0].Changes[0].OldText != "xone\ntwo" || got[0].Changes[0].NewText != "fresh\ntext" ||
		got[0].Changes[0].NewRange != rg(0, 0, 1, 4) {
		t.Errorf("SetText event = %+v", got)
	}
}

func TestSnapshotsAreIndependentOfLaterEdits(t *testing.T) {
	d := newDoc("abc")
	s := d.Snapshot()
	edit(t, d, []Edit{ins(0, 1, "X")}, EditOptions{})
	undo(t, d)
	redo(t, d)
	if s.String() != "abc" {
		t.Errorf("snapshot = %q", s.String())
	}
}

func TestUndoRestoresOnlyWhatTheGroupChanged(t *testing.T) {
	d := newDoc("a\nb\nc")
	edit(t, d, []Edit{{Range: rg(0, 0, 0, 1), Text: "A"}}, EditOptions{})
	edit(t, d, []Edit{{Range: rg(2, 0, 2, 1), Text: "C"}}, EditOptions{})
	undo(t, d)
	if got := d.Text(); got != "A\nb\nc" {
		t.Errorf("text = %q", got)
	}
	undo(t, d)
	if got := d.Text(); got != "a\nb\nc" {
		t.Errorf("text = %q", got)
	}
	redo(t, d)
	redo(t, d)
	if got := d.Text(); got != "A\nb\nC" {
		t.Errorf("text = %q", got)
	}
}

// Ported from VS Code: cursor.test.ts ('there is a single undo stop for
// consecutive whitespaces', 'there is no undo stop after a single whitespace',
// 'inserts undo stop when typing space'): undo stops before the first space typed
// after other characters, spaces typed one after the other stay together, and the
// characters after a single space join it.
func TestTypingSpaceUndoStops(t *testing.T) {
	tests := []struct {
		name   string
		typed  string
		undone []string // the text after each Undo
	}{
		{"consecutive spaces", "ab  cd", []string{"ab  ", "ab", ""}},
		{"single space", "ab cd", []string{"ab", ""}},
		{"words", "first and interesting", []string{"first and", "first", ""}},
		{"leading space", " ab", []string{""}},
		{"space at the end", "ab ", []string{"ab", ""}},
		{"three spaces", "a   b", []string{"a   ", "a", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDoc("")
			for i := 0; i < len(tt.typed); i++ {
				typeAt(t, d, i, tt.typed[i:i+1])
			}
			if d.Text() != tt.typed {
				t.Fatalf("text = %q", d.Text())
			}
			for i, want := range tt.undone {
				undo(t, d)
				if d.Text() != want {
					t.Fatalf("after Undo %d: %q, want %q", i+1, d.Text(), want)
				}
			}
		})
	}
}

func TestTypingSpaceForSeveralCursors(t *testing.T) {
	// A batch where every edit inserts a single space is a space typed.
	d := newDoc("ab\nab")
	two := func(col int, s string) {
		edit(t, d, []Edit{ins(0, col, s), ins(1, col, s)}, EditOptions{Kind: KindTyping})
	}
	two(2, "c")
	two(3, " ")
	two(4, "d")
	for _, want := range []string{"abc\nabc", "ab\nab"} {
		undo(t, d)
		if d.Text() != want {
			t.Fatalf("Undo gave %q, want %q", d.Text(), want)
		}
	}
}
