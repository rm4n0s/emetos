package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// clipRig is an editor with a fake clipboard (the tester's).
type clipRig struct {
	*mouseRig
}

func newClipRig(t *testing.T, text string, tweak func(*Options)) clipRig {
	t.Helper()
	return clipRig{newMouseRig(t, text, tweak)}
}

func (r clipRig) at(sels ...buffer.Selection) clipRig {
	r.vm.Cursors().SetSelections(sels)
	r.vm.SelectionsChanged()
	r.ts.Pump(time.Millisecond)
	return r
}

// run runs a command and lets the clipboard answer.
func (r clipRig) run(id string) {
	r.t.Helper()
	if err := r.vm.RunCommand(id, nil); err != nil {
		r.t.Fatalf("%s: %v", id, err)
	}
	r.ts.Pump(time.Millisecond)
}

func (r clipRig) clipboard() string {
	text, _ := r.ts.ClipboardText()
	return text
}

const (
	copyCmd  = "editor.action.clipboardCopyAction"
	cutCmd   = "editor.action.clipboardCutAction"
	pasteCmd = "editor.action.clipboardPasteAction"
)

func TestCopy(t *testing.T) {
	cases := []struct {
		name string
		text string
		sels []buffer.Selection
		opts func(*Options)
		want string
	}{
		{"a selection", "hello world", []buffer.Selection{sel(0, 0, 0, 5)}, nil, "hello"},
		{"across lines", "ab\ncd\nef", []buffer.Selection{sel(0, 1, 2, 1)}, nil, "b\ncd\ne"},
		{"the line of an empty selection", "ab\ncd", []buffer.Selection{cur(1, 1)}, nil, "cd\n"},
		{"several selections in the order of the text", "abc\ndef", []buffer.Selection{sel(1, 0, 1, 2), sel(0, 0, 0, 1)}, nil, "a\nde"},
		{"nothing without the setting", "ab", []buffer.Selection{cur(0, 1)}, func(o *Options) { o.EmptySelectionClipboard = false }, "kept"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newClipRig(t, tc.text, tc.opts).at(tc.sels...)
			r.ts.SetClipboardText("kept")
			r.run(copyCmd)
			if got := r.clipboard(); got != tc.want {
				t.Errorf("clipboard %q, want %q", got, tc.want)
			}
			if r.vm.Document().Text() != tc.text {
				t.Errorf("a copy changed the text: %q", r.vm.Document().Text())
			}
			noErrors(t, r.ts)
		})
	}
}

func TestCopyKeepsTheSessions(t *testing.T) {
	r := newClipRig(t, "abcd\nefgh\nijkl", nil).at(cur(0, 1))
	r.run("cursorColumnSelectDown")
	if r.vm.columns == nil {
		t.Fatal("a column selection")
	}
	r.run(copyCmd)
	if r.vm.columns == nil {
		t.Error("copying ends the column selection")
	}
	r.run("cursorColumnSelectDown")
	if len(r.selections()) != 3 {
		t.Errorf("the column selection goes on: %v", r.selections())
	}
}

func TestCut(t *testing.T) {
	cases := []struct {
		name string
		text string
		sels []buffer.Selection
		opts func(*Options)
		want string
		left string
	}{
		{"a selection", "hello world", []buffer.Selection{sel(0, 0, 0, 6)}, nil, "hello ", "world"},
		{"the line of an empty selection", "ab\ncd\nef", []buffer.Selection{cur(1, 1)}, nil, "cd\n", "ab\nef"},
		{"the last line", "ab\ncd", []buffer.Selection{cur(1, 1)}, nil, "cd\n", "ab"},
		{"nothing without the setting", "ab\ncd", []buffer.Selection{cur(1, 1)}, func(o *Options) { o.EmptySelectionClipboard = false }, "kept", "ab\ncd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newClipRig(t, tc.text, tc.opts).at(tc.sels...)
			r.ts.SetClipboardText("kept")
			r.run(cutCmd)
			if got := r.clipboard(); got != tc.want {
				t.Errorf("clipboard %q, want %q", got, tc.want)
			}
			if got := r.vm.Document().Text(); got != tc.left {
				t.Errorf("text %q, want %q", got, tc.left)
			}
		})
	}
}

func TestCutInAReadOnlyDocumentOnlyCopies(t *testing.T) {
	r := newClipRig(t, "hello", nil).at(sel(0, 0, 0, 3))
	r.vm.Document().SetReadOnly(true)
	r.run(cutCmd)
	if r.clipboard() != "hel" || r.vm.Document().Text() != "hello" {
		t.Errorf("clipboard %q, text %q", r.clipboard(), r.vm.Document().Text())
	}
}

func TestCutIsOneUndoStep(t *testing.T) {
	r := newClipRig(t, "ab\ncd", nil).at(sel(0, 0, 0, 1), sel(1, 0, 1, 1))
	r.run(cutCmd)
	if r.vm.Document().Text() != "b\nd" {
		t.Fatalf("text %q", r.vm.Document().Text())
	}
	if _, err := r.vm.Document().Undo(); err != nil {
		t.Fatal(err)
	}
	if r.vm.Document().Text() != "ab\ncd" {
		t.Errorf("undo: %q", r.vm.Document().Text())
	}
}

func TestPaste(t *testing.T) {
	cases := []struct {
		name string
		text string
		sels []buffer.Selection
		opts func(*Options)
		clip string
		want string
		at   []buffer.Selection
	}{
		{"into a caret", "abc", []buffer.Selection{cur(0, 1)}, nil, "XY", "aXYbc", []buffer.Selection{cur(0, 3)}},
		{"over a selection", "abcdef", []buffer.Selection{sel(0, 1, 0, 4)}, nil, "-", "a-ef", []buffer.Selection{cur(0, 2)}},
		{"every cursor gets the text", "ab\ncd", []buffer.Selection{cur(0, 1), cur(1, 1)}, nil, "_", "a_b\nc_d", []buffer.Selection{cur(0, 2), cur(1, 2)}},
		{"a line to each cursor with spread", "ab\ncd", []buffer.Selection{cur(0, 1), cur(1, 1)}, nil, "X\nY", "aXb\ncYd", []buffer.Selection{cur(0, 2), cur(1, 2)}},
		{"the whole text to each cursor with full", "ab\ncd", []buffer.Selection{cur(0, 1), cur(1, 1)}, func(o *Options) { o.MultiCursorPaste = "full" }, "X\nY", "aX\nYb\ncX\nYd", []buffer.Selection{cur(1, 1), cur(3, 1)}},
		{"line breaks are normalized", "ab", []buffer.Selection{cur(0, 1)}, nil, "x\r\ny", "ax\nyb", []buffer.Selection{cur(1, 1)}},
		{"an empty clipboard changes nothing", "ab", []buffer.Selection{cur(0, 1)}, nil, "", "ab", []buffer.Selection{cur(0, 1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newClipRig(t, tc.text, tc.opts).at(tc.sels...)
			r.ts.SetClipboardText(tc.clip)
			r.run(pasteCmd)
			if got := r.vm.Document().Text(); got != tc.want {
				t.Errorf("text %q, want %q", got, tc.want)
			}
			if !buffer.SelectionsEqual(r.selections(), tc.at) {
				t.Errorf("selections %v, want %v", r.selections(), tc.at)
			}
			noErrors(t, r.ts)
		})
	}
}

func TestPasteIsOneUndoStep(t *testing.T) {
	r := newClipRig(t, "ab", nil).at(cur(0, 1))
	r.ts.SetClipboardText("x\ny\nz")
	r.run(pasteCmd)
	if _, err := r.vm.Document().Undo(); err != nil {
		t.Fatal(err)
	}
	if r.vm.Document().Text() != "ab" {
		t.Errorf("undo: %q", r.vm.Document().Text())
	}
}

func TestPasteInAReadOnlyDocumentDoesNothing(t *testing.T) {
	r := newClipRig(t, "ab", nil).at(cur(0, 1))
	r.vm.Document().SetReadOnly(true)
	r.ts.SetClipboardText("x")
	r.run(pasteCmd)
	if r.vm.Document().Text() != "ab" {
		t.Errorf("text %q", r.vm.Document().Text())
	}
}

func TestCopiedLineGoesAboveTheCursor(t *testing.T) {
	r := newClipRig(t, "one\ntwo\nthree", nil).at(cur(0, 1))
	r.run(copyCmd) // the line "one\n"
	r.at(cur(2, 2))
	r.run(pasteCmd)
	if got := r.vm.Document().Text(); got != "one\ntwo\none\nthree" {
		t.Errorf("text %q", got)
	}
	r.want(cur(3, 2)) // the cursor stays in its line
	// The copy is known by its text, so the same text is a line again, whoever copied it.
	r.ts.SetClipboardText("one\n")
	r.at(cur(1, 1))
	r.run(pasteCmd)
	if got := r.vm.Document().Text(); got != "one\none\ntwo\none\nthree" {
		t.Errorf("text %q", got)
	}
}

func TestCopiedLineWithTheSettingOffIsPlain(t *testing.T) {
	r := newClipRig(t, "one\ntwo", nil).at(cur(0, 1))
	r.run(copyCmd)
	r.vm.opts.EmptySelectionClipboard = false
	r.at(cur(1, 1))
	r.run(pasteCmd)
	if got := r.vm.Document().Text(); got != "one\nt"+"one\n"+"wo" {
		t.Errorf("text %q", got)
	}
}

func TestPiecesOfSeveralCursorsGoToSeveralCursors(t *testing.T) {
	r := newClipRig(t, "ab\ncd\nef", nil).at(sel(0, 0, 0, 2), sel(2, 0, 2, 2))
	r.run(copyCmd)
	if r.clipboard() != "ab\nef" {
		t.Fatalf("clipboard %q", r.clipboard())
	}
	// Two cursors paste a piece each, also where the text has more lines than that.
	r.at(cur(1, 1), cur(1, 2))
	r.vm.opts.MultiCursorPaste = "full"
	r.run(pasteCmd)
	// The first cursor gets "ab" and the second "ef", although "full" gives the whole text.
	if got := r.vm.Document().Text(); got != "ab\ncabdef\nef" {
		t.Errorf("text %q", got)
	}
}

func TestMemoryOfACopyIsForgottenWhenSomethingElseIsCopied(t *testing.T) {
	r := newClipRig(t, "one\ntwo", nil).at(cur(0, 1))
	r.run(copyCmd)
	r.ts.SetClipboardText("other")
	r.at(cur(1, 0))
	r.run(pasteCmd)
	if got := r.vm.Document().Text(); got != "one\nothertwo" {
		t.Errorf("text %q", got)
	}
	// Even the text of the first copy is no longer known as a line.
	r.ts.SetClipboardText("one\n")
	r.at(cur(1, 3))
	r.run(pasteCmd)
	if got := r.vm.Document().Text(); got != "one\nothone\nertwo" {
		t.Errorf("text %q", got)
	}
}

func TestColumnCopyPastesAsAColumn(t *testing.T) {
	r := newClipRig(t, "abcd\nefgh\nijkl\nmnop", nil).at(cur(0, 1))
	r.run("cursorColumnSelectRight")
	r.run("cursorColumnSelectDown")
	if len(r.selections()) != 2 {
		t.Fatalf("the column selection: %v", r.selections())
	}
	r.run(copyCmd)
	if r.clipboard() != "b\nf" {
		t.Fatalf("clipboard %q", r.clipboard())
	}
	r.at(cur(2, 3))
	r.run(pasteCmd)
	if got := r.vm.Document().Text(); got != "abcd\nefgh\nijkbl\nmnofp" {
		t.Errorf("text %q", got)
	}
}

func TestNoClipboardIsAnError(t *testing.T) {
	r := newClipRig(t, "ab", nil).at(sel(0, 0, 0, 1))
	r.vm.SetClipboard(nil, nil, nil)
	for _, id := range []string{copyCmd, cutCmd, pasteCmd} {
		if err := r.vm.RunCommand(id, nil); err == nil {
			t.Errorf("%s without a clipboard", id)
		}
	}
	if err := r.vm.PastePrimary(); err == nil {
		t.Error("PastePrimary without a clipboard")
	}
	if r.vm.Document().Text() != "ab" {
		t.Errorf("text %q", r.vm.Document().Text())
	}
}

func TestPrimarySelectionFollowsTheSelection(t *testing.T) {
	r := newClipRig(t, "hello world\nsecond", nil)
	r.ts.SetPrimaryText("kept")
	r.at(sel(0, 0, 0, 5))
	if text, _ := r.ts.PrimaryText(); text != "kept" {
		t.Errorf("the primary selection is set after a delay, not at once: %q", text)
	}
	r.ts.Pump(primarySelectionDelay)
	if text, _ := r.ts.PrimaryText(); text != "hello" {
		t.Errorf("primary selection %q, want hello", text)
	}
	// The system clipboard is not touched.
	if got, ok := r.ts.ClipboardText(); ok && got != "" {
		t.Errorf("clipboard %q", got)
	}
	r.at(sel(1, 0, 1, 3), sel(0, 6, 0, 11))
	r.ts.Pump(primarySelectionDelay)
	if text, _ := r.ts.PrimaryText(); text != "world\nsec" {
		t.Errorf("primary selection %q", text)
	}
}

func TestPrimarySelectionWaitsForTheSelectionToSettle(t *testing.T) {
	r := newClipRig(t, "hello world", nil)
	r.ts.SetPrimaryText("kept")
	for end := 1; end <= 5; end++ {
		r.at(sel(0, 0, 0, end))
		r.ts.Pump(primarySelectionDelay / 4)
	}
	if text, _ := r.ts.PrimaryText(); text != "kept" {
		t.Errorf("set while the selection still changed: %q", text)
	}
	r.ts.Pump(primarySelectionDelay)
	if text, _ := r.ts.PrimaryText(); text != "hello" {
		t.Errorf("primary selection %q", text)
	}
}

func TestPrimarySelectionNeedsASelectionAtEveryCursor(t *testing.T) {
	r := newClipRig(t, "hello world", nil)
	r.ts.SetPrimaryText("kept")
	r.at(sel(0, 0, 0, 5), cur(0, 8))
	r.ts.Pump(primarySelectionDelay)
	if text, _ := r.ts.PrimaryText(); text != "kept" {
		t.Errorf("primary selection %q", text)
	}
	r.at(cur(0, 3))
	r.ts.Pump(primarySelectionDelay)
	if text, _ := r.ts.PrimaryText(); text != "kept" {
		t.Errorf("an empty selection leaves the primary selection: %q", text)
	}
}

func TestPrimarySelectionIsOffWithTheSetting(t *testing.T) {
	r := newClipRig(t, "hello", func(o *Options) { o.SelectionClipboard = false })
	r.ts.SetPrimaryText("kept")
	r.at(sel(0, 0, 0, 3))
	r.ts.Pump(primarySelectionDelay)
	if text, _ := r.ts.PrimaryText(); text != "kept" {
		t.Errorf("primary selection %q", text)
	}
}

func TestEditorsWithoutTheFocusLeaveThePrimarySelection(t *testing.T) {
	r := newClipRig(t, "hello", nil)
	r.ts.SetPrimaryText("kept")
	r.vm.SetFocused(false)
	r.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 0, 0, 3)})
	r.vm.SelectionsChanged()
	r.ts.Pump(primarySelectionDelay * 2)
	if text, _ := r.ts.PrimaryText(); text != "kept" {
		t.Errorf("primary selection %q", text)
	}
}

func TestPrimarySelectionIsNotWrittenAgainForTheSameText(t *testing.T) {
	r := newClipRig(t, "hello world", nil)
	r.at(sel(0, 0, 0, 5))
	r.ts.Pump(primarySelectionDelay)
	r.ts.SetPrimaryText("taken by another program")
	r.vm.SelectionsChanged() // the selections did not change
	r.ts.Pump(primarySelectionDelay * 2)
	if text, _ := r.ts.PrimaryText(); text != "taken by another program" {
		t.Errorf("primary selection %q", text)
	}
}

func TestCopyToTheClipboardAndPasteInAnotherEditor(t *testing.T) {
	memory := NewClipboardMemory()
	a := newMouseRigWith(t, "one\ntwo", nil, EditorOpts{Autofocus: true, ClipboardMemory: memory})
	if err := a.vm.RunCommand(copyCmd, nil); err != nil { // a line
		t.Fatal(err)
	}
	a.ts.Pump(time.Millisecond)
	text, _ := a.ts.ClipboardText()
	if text != "one\n" {
		t.Fatalf("clipboard %q", text)
	}
	// The memory is what another editor of the window sees.
	if known := memory.recall(text); known == nil || !known.fromEmptySelection {
		t.Errorf("the memory has %+v", known)
	}
}
