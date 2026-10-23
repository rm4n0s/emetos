package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// inputEditor shows a text in an editor that has the focus, with its input connected.
func inputEditor(t *testing.T, text string, sels ...buffer.Selection) (*ViewModel, *gluttertest.Tester) {
	t.Helper()
	vm := NewViewModel(newTestDoc(text), goOptions(), nil)
	if len(sels) > 0 {
		vm.Cursors().SetSelections(sels)
	}
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 400})
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	return vm, ts
}

func lastState(t *testing.T, ts *gluttertest.Tester) ui.TextInputState {
	t.Helper()
	states := ts.TextInputStates()
	if len(states) == 0 {
		t.Fatal("the input method was never told anything")
	}
	return states[len(states)-1]
}

func commit(text string) ui.TextInputEvent {
	return ui.TextInputEvent{Commit: text, PreeditCursorBegin: -1, PreeditCursorEnd: -1}
}

func TestFocusConnectsTheInputMethodToTheCurrentLine(t *testing.T) {
	vm, ts := inputEditor(t, "first\nsecond line\nthird", sel(1, 2, 1, 6))
	st := lastState(t, ts)
	if !st.Enabled || st.Text != "second line" || st.SelectionAnchor != 2 || st.SelectionCursor != 6 {
		t.Errorf("state %+v", st)
	}
	// The window follows the primary cursor.
	if err := vm.RunCommand("cursorUp", nil); err != nil {
		t.Fatal(err)
	}
	st = lastState(t, ts)
	if st.Text != "first" {
		t.Errorf("after moving up the method has %q", st.Text)
	}
	// A selection over several lines is a caret at its active end.
	vm.Cursors().SetSelections([]buffer.Selection{sel(0, 1, 2, 3)})
	vm.SelectionsChanged()
	st = lastState(t, ts)
	if st.Text != "third" || st.SelectionAnchor != 3 || st.SelectionCursor != 3 {
		t.Errorf("multi-line selection: %+v", st)
	}
	noErrors(t, ts)
}

func TestInputMethodFollowsEdits(t *testing.T) {
	vm, ts := inputEditor(t, "abc", cur(0, 3))
	if err := vm.RunCommand("type", "d"); err != nil {
		t.Fatal(err)
	}
	if st := lastState(t, ts); st.Text != "abcd" || st.SelectionCursor != 4 {
		t.Errorf("state %+v", st)
	}
}

func TestCommitTypesAtTheCursor(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		from     []buffer.Selection
		event    ui.TextInputEvent
		want     string
		wantSels []buffer.Selection
	}{
		{"a character", "ab", []buffer.Selection{cur(0, 1)}, commit("X"), "aXb", []buffer.Selection{cur(0, 2)}},
		{"a word of two byte characters", "ab", []buffer.Selection{cur(0, 2)}, commit("γειά"), "abγειά", []buffer.Selection{cur(0, 10)}},
		{"cjk", "x", []buffer.Selection{cur(0, 0)}, commit("日本語"), "日本語x", []buffer.Selection{cur(0, 9)}},
		{"emoji", "a", []buffer.Selection{cur(0, 1)}, commit("😀"), "a😀", []buffer.Selection{cur(0, 5)}},
		{"over a selection", "abcd", []buffer.Selection{sel(0, 1, 0, 3)}, commit("X"), "aXd", []buffer.Selection{cur(0, 2)}},
		{"at every cursor", "ab\ncd\nef", []buffer.Selection{cur(0, 1), cur(1, 1), cur(2, 2)}, commit("-"), "a-b\nc-d\nef-", []buffer.Selection{cur(0, 2), cur(1, 2), cur(2, 3)}},
		{"over every selection", "abc\ndef", []buffer.Selection{sel(0, 0, 0, 2), sel(1, 1, 1, 3)}, commit("Z"), "Zc\ndZ", []buffer.Selection{cur(0, 1), cur(1, 2)}},
		{"a new line keeps the indentation", "  ab", []buffer.Selection{cur(0, 4)}, commit("\n"), "  ab\n  ", []buffer.Selection{cur(1, 2)}},
		{"a new line in the middle", "abcd", []buffer.Selection{cur(0, 2)}, commit("\n"), "ab\ncd", []buffer.Selection{cur(1, 0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm, ts := inputEditor(t, tc.text, tc.from...)
			ts.SendTextInputEvent(tc.event)
			if got := vm.Document().Text(); got != tc.want {
				t.Errorf("text %q, want %q", got, tc.want)
			}
			if got := vm.Cursors().Selections(); !buffer.SelectionsEqual(got, tc.wantSels) {
				t.Errorf("selections %v, want %v", got, tc.wantSels)
			}
			if _, composing := vm.Preedit(); composing {
				t.Error("a commit leaves no preedit")
			}
			noErrors(t, ts)
		})
	}
}

func TestDeletingAroundTheCursor(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		from     []buffer.Selection
		event    ui.TextInputEvent
		want     string
		wantSels []buffer.Selection
	}{
		{"before the cursor", "abc", []buffer.Selection{cur(0, 2)}, ui.TextInputEvent{DeleteBefore: 1}, "ac", []buffer.Selection{cur(0, 1)}},
		{"a two byte character before", "aβc", []buffer.Selection{cur(0, 3)}, ui.TextInputEvent{DeleteBefore: 2}, "ac", []buffer.Selection{cur(0, 1)}},
		{"after the cursor", "abc", []buffer.Selection{cur(0, 1)}, ui.TextInputEvent{DeleteAfter: 1}, "ac", []buffer.Selection{cur(0, 1)}},
		{"around the cursor", "abcde", []buffer.Selection{cur(0, 2)}, ui.TextInputEvent{DeleteBefore: 1, DeleteAfter: 2}, "ae", []buffer.Selection{cur(0, 1)}},
		{"at every cursor", "abc\ndef", []buffer.Selection{cur(0, 2), cur(1, 2)}, ui.TextInputEvent{DeleteBefore: 1}, "ac\ndf", []buffer.Selection{cur(0, 1), cur(1, 1)}},
		{"a replacement of what is before the cursor", "abc", []buffer.Selection{cur(0, 3)}, ui.TextInputEvent{DeleteBefore: 2, Commit: "X", PreeditCursorBegin: -1, PreeditCursorEnd: -1}, "aX", []buffer.Selection{cur(0, 2)}},
		{"the selection", "abcd", []buffer.Selection{sel(0, 1, 0, 3)}, ui.TextInputEvent{DeleteSelection: true}, "ad", []buffer.Selection{cur(0, 1)}},
		{"before the start of the line", "ab", []buffer.Selection{cur(0, 0)}, ui.TextInputEvent{DeleteBefore: 3}, "ab", []buffer.Selection{cur(0, 0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm, ts := inputEditor(t, tc.text, tc.from...)
			ts.SendTextInputEvent(tc.event)
			if got := vm.Document().Text(); got != tc.want {
				t.Errorf("text %q, want %q", got, tc.want)
			}
			if got := vm.Cursors().Selections(); !buffer.SelectionsEqual(got, tc.wantSels) {
				t.Errorf("selections %v, want %v", got, tc.wantSels)
			}
			noErrors(t, ts)
		})
	}
}

func TestPreeditIsShownAndNotTyped(t *testing.T) {
	vm, ts := inputEditor(t, "hello world", cur(0, 6))
	ts.SendTextInputEvent(ui.TextInputEvent{Preedit: "nihao", PreeditCursorBegin: 2, PreeditCursorEnd: 5})
	p, ok := vm.Preedit()
	if !ok || p != (Preedit{Text: "nihao", Begin: 2, End: 5}) {
		t.Fatalf("preedit %+v, %v", p, ok)
	}
	if vm.Document().Text() != "hello world" {
		t.Errorf("the preedit entered the document: %q", vm.Document().Text())
	}
	if vm.Document().CanUndo() {
		t.Error("the preedit entered the undo history")
	}
	// A new preedit replaces the old one.
	ts.SendTextInputEvent(ui.TextInputEvent{Preedit: "ni", PreeditCursorBegin: -1, PreeditCursorEnd: -1})
	if p, _ := vm.Preedit(); p != (Preedit{Text: "ni", Begin: 2, End: 2}) {
		t.Errorf("second preedit %+v", p)
	}
	// The commit replaces the preedit by the text.
	ts.SendTextInputEvent(ui.TextInputEvent{Commit: "你", PreeditCursorBegin: -1, PreeditCursorEnd: -1})
	if _, ok := vm.Preedit(); ok {
		t.Error("the commit ends the preedit")
	}
	if vm.Document().Text() != "hello 你world" || vm.Cursors().Primary().Selection != cur(0, 9) {
		t.Errorf("text %q cursor %v", vm.Document().Text(), vm.Cursors().Primary().Selection)
	}
	// A commit and a new preedit in one event: the commit is typed, the preedit goes on.
	ts.SendTextInputEvent(ui.TextInputEvent{Commit: "好", Preedit: "ma", PreeditCursorBegin: 1, PreeditCursorEnd: 1})
	if p, _ := vm.Preedit(); p.Text != "ma" || vm.Document().Text() != "hello 你好world" {
		t.Errorf("preedit %+v text %q", p, vm.Document().Text())
	}
	// Cancelling the composition (an empty preedit) removes it.
	ts.SendTextInputEvent(ui.TextInputEvent{PreeditCursorBegin: -1, PreeditCursorEnd: -1})
	if _, ok := vm.Preedit(); ok || vm.Document().Text() != "hello 你好world" {
		t.Errorf("after cancelling: %q", vm.Document().Text())
	}
	noErrors(t, ts)
}

func TestPreeditOverASelectionReplacesIt(t *testing.T) {
	vm, ts := inputEditor(t, "abcd", sel(0, 1, 0, 3))
	ts.SendTextInputEvent(ui.TextInputEvent{Preedit: "x", PreeditCursorBegin: -1, PreeditCursorEnd: -1})
	if vm.Document().Text() != "ad" {
		t.Errorf("the selection goes when composing starts: %q", vm.Document().Text())
	}
	if p, ok := vm.Preedit(); !ok || p.Text != "x" {
		t.Errorf("preedit %+v", p)
	}
	noErrors(t, ts)
}

func TestPreeditEndsWhenTheEditorLosesTheFocus(t *testing.T) {
	vm, ts := inputEditor(t, "abc", cur(0, 3))
	ts.SendTextInputEvent(ui.TextInputEvent{Preedit: "x", PreeditCursorBegin: -1, PreeditCursorEnd: -1})
	ts.Binding.FocusManager().PrimaryFocus().Unfocus()
	ts.Pump(time.Millisecond)
	if _, ok := vm.Preedit(); ok {
		t.Error("the preedit outlives the focus")
	}
	if st := lastState(t, ts); st.Enabled {
		t.Errorf("the input method is still on: %+v", st)
	}
	noErrors(t, ts)
}

func TestTypingWithKeys(t *testing.T) {
	vm, ts := inputEditor(t, "", cur(0, 0))
	typed := []struct {
		logical  services.LogicalKeyboardKey
		physical services.PhysicalKeyboardKey
		char     string
	}{
		{'h', services.PhysicalKeyboardKeyKeyH, "h"},
		{'i', services.PhysicalKeyboardKeyKeyI, "i"},
		{services.LogicalKeyboardKeySpace, services.PhysicalKeyboardKeySpace, " "},
		{0x3b1, services.PhysicalKeyboardKeyKeyA, "α"},
		{0x3b2, services.PhysicalKeyboardKeyKeyB, "β"},
		{'$', services.PhysicalKeyboardKeyDigit4, "$"},
	}
	for _, k := range typed {
		ts.SendKeyEvent(k.logical, gluttertest.KeyOpts{Physical: k.physical, Character: k.char})
	}
	if got := vm.Document().Text(); got != "hi αβ$" {
		t.Errorf("text %q", got)
	}
	ts.SendKeyEvent(services.LogicalKeyboardKeyEnter, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyEnter})
	ts.SendKeyEvent('x', gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyKeyX, Character: "x"})
	if got := vm.Document().Text(); got != "hi αβ$\nx" {
		t.Errorf("after Enter and x: %q", got)
	}
	// Control and Alt combinations, and keys without a character, type nothing.
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	ts.SendKeyEvent('y', gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyKeyY, Character: "y"})
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	ts.SendKeyEvent(services.LogicalKeyboardKeyArrowLeft, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyArrowLeft})
	if got := vm.Document().Text(); got != "hi αβ$\nx" {
		t.Errorf("non-printing keys typed: %q", got)
	}
	// Typing is one undo step while it goes on.
	if err := vm.RunCommand("undo", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); strings.Contains(got, "x") {
		t.Errorf("undo left %q", got)
	}
	noErrors(t, ts)
}

func TestTypingIntoAReadOnlyDocumentIsReported(t *testing.T) {
	vm := NewViewModel(newTestDoc("abc"), goOptions(), nil)
	vm.Document().SetReadOnly(true)
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 3)})
	var got []error
	ts := testutilNew(t)
	pumpEditorWith(t, ts, vm, EditorOpts{Autofocus: true, OnError: func(err error) { got = append(got, err) }})
	ts.Pump(time.Millisecond)
	ts.SendTextInputEvent(commit("x"))
	if vm.Document().Text() != "abc" {
		t.Errorf("text %q", vm.Document().Text())
	}
	if len(got) != 1 {
		t.Fatalf("%d errors reported", len(got))
	}
	appErr, ok := errors.FromError(got[0])
	if !ok || !strings.HasSuffix(appErr.Route(), "DocumentReadOnly") {
		t.Errorf("error %v", got[0])
	}
	// The input method has the text the document still has.
	if st := lastState(t, ts); st.Text != "abc" {
		t.Errorf("state %+v", st)
	}
}

func TestCaretRectFollowsTheCursor(t *testing.T) {
	var lines strings.Builder
	for range 200 {
		lines.WriteString("line of text here\n")
	}
	vm, ts := inputEditor(t, lines.String(), cur(2, 5))
	gutter := vm.GutterWidths(ts.Dispatcher().TextEngine()).Total()
	cw := newGeometry(vm, ts.Dispatcher().TextEngine()).charWidth

	r := lastState(t, ts).CaretRect
	if !near(r.Left, gutter+5*cw) || !near(r.Top, 2*19) || !near(r.Height(), 19) {
		t.Errorf("caret rect %v, want left %v top %v", r, gutter+5*cw, 2*19)
	}
	// Moving the cursor moves the rect.
	if err := vm.RunCommand("cursorDown", nil); err != nil {
		t.Fatal(err)
	}
	ts.Pump(time.Millisecond)
	if r := lastState(t, ts).CaretRect; !near(r.Top, 3*19) {
		t.Errorf("after cursorDown: %v", r)
	}
	// Scrolling moves it too (the rect is in the window's coordinates).
	ts.Scroll(gluttertest.ByType[*Editor](), ui.Offset{Dy: 38})
	ts.Pump(100 * time.Millisecond)
	ts.Pump(time.Millisecond)
	if r := lastState(t, ts).CaretRect; !near(r.Top, 3*19-38) {
		t.Errorf("after scrolling by 38: %v", r)
	}
	// While composing the rect is at the method's cursor inside the preedit.
	vm.Cursors().SetSelections([]buffer.Selection{cur(5, 0)})
	vm.SelectionsChanged()
	ts.SendTextInputEvent(ui.TextInputEvent{Preedit: "abcd", PreeditCursorBegin: 2, PreeditCursorEnd: 2})
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if r := lastState(t, ts).CaretRect; !near(r.Left, gutter+2*cw) {
		t.Errorf("inside the preedit: %v, want left %v", r, gutter+2*cw)
	}
	noErrors(t, ts)
}

func TestGoldenPreedit(t *testing.T) {
	vm, ts := inputEditor(t, "func main() {\n\tprintln(\"hello \")\n}\n", cur(1, 17))
	ts.SendTextInputEvent(ui.TextInputEvent{Preedit: "shijie", PreeditCursorBegin: 0, PreeditCursorEnd: 3})
	ts.Pump(time.Millisecond)
	if _, ok := vm.Preedit(); !ok {
		t.Fatal("no preedit")
	}
	noErrors(t, ts)
	ts.MatchesGolden("preedit")
}

func TestAlignedAffixes(t *testing.T) {
	cases := []struct {
		old, now string
		from, to int
		text     string
	}{
		{"abc", "abXc", 2, 2, "X"},
		{"abc", "ac", 1, 2, ""},
		{"abc", "abc", 3, 3, ""},
		{"", "abc", 0, 0, "abc"},
		{"abc", "", 0, 3, ""},
		{"abcd", "aXd", 1, 3, "X"},
		{"aaa", "aaaa", 3, 3, "a"},
		// a character is never cut: β is CE B2, γ is CE B3, they share the first byte
		{"aβb", "aγb", 1, 3, "γ"},
		{"β", "γβ", 0, 0, "γ"},
		{"日本", "日語本", 3, 3, "語"},
		{"x😀y", "xy", 1, 5, ""},
	}
	for _, tc := range cases {
		from, to, text := alignedAffixes(tc.old, tc.now)
		if from != tc.from || to != tc.to || text != tc.text {
			t.Errorf("%q → %q: [%d,%d) %q, want [%d,%d) %q", tc.old, tc.now, from, to, text, tc.from, tc.to, tc.text)
		}
		// The stretch really turns old into now.
		if got := tc.old[:from] + text + tc.old[to:]; got != tc.now {
			t.Errorf("%q → %q: the stretch gives %q", tc.old, tc.now, got)
		}
	}
}
