package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func TestInputModeToggleTellsTheListeners(t *testing.T) {
	m := NewInputMode()
	calls := 0
	h := m.AddListener(func() { calls++ })
	tests := []struct {
		name      string
		do        func()
		overtype  bool
		wantCalls int
	}{
		{"it starts in insert mode", func() {}, false, 0},
		{"toggle goes to overtype", m.Toggle, true, 1},
		{"setting the same mode tells nobody", func() { m.SetOvertype(true) }, true, 1},
		{"toggle goes back", m.Toggle, false, 2},
	}
	for _, tc := range tests {
		tc.do()
		if m.Overtype() != tc.overtype || calls != tc.wantCalls {
			t.Errorf("%s: overtype %v after %d calls, want %v after %d", tc.name, m.Overtype(), calls, tc.overtype, tc.wantCalls)
		}
	}
	m.RemoveListener(h)
	m.Toggle()
	if calls != 2 {
		t.Errorf("a removed listener was called: %d calls", calls)
	}
}

// TestTypingInOvertypeMode types through the input method: a character replaces the one after the cursor, and at the end
// of a line it is inserted (SPEC §9.3).
func TestTypingInOvertypeMode(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		from     []buffer.Selection
		typed    string
		want     string
		wantSels []buffer.Selection
	}{
		{"a character replaces the next one", "abcd", []buffer.Selection{cur(0, 1)}, "X", "aXcd", []buffer.Selection{cur(0, 2)}},
		{"two characters replace two", "abcd", []buffer.Selection{cur(0, 1)}, "XY", "aXYd", []buffer.Selection{cur(0, 3)}},
		{"at the end of the line it inserts", "ab\ncd", []buffer.Selection{cur(0, 2)}, "XY", "abXY\ncd", []buffer.Selection{cur(0, 4)}},
		{"it never takes the line break", "ab\ncd", []buffer.Selection{cur(0, 1)}, "XYZ", "aXYZ\ncd", []buffer.Selection{cur(0, 4)}},
		{"a two byte character is one character", "aβc", []buffer.Selection{cur(0, 1)}, "X", "aXc", []buffer.Selection{cur(0, 2)}},
		{"every cursor replaces", "abc\ndef", []buffer.Selection{cur(0, 0), cur(1, 1)}, "-", "-bc\nd-f", []buffer.Selection{cur(0, 1), cur(1, 2)}},
		{"the first character replaces the selection, the next one the character after it", "abcdef", []buffer.Selection{sel(0, 1, 0, 3)}, "XY", "aXYef", []buffer.Selection{cur(0, 3)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm, ts := inputEditor(t, tc.text, tc.from...)
			vm.InputMode().SetOvertype(true)
			for _, r := range tc.typed {
				ts.SendTextInputEvent(commit(string(r)))
			}
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

func TestTypingInInsertModeStillInserts(t *testing.T) {
	vm, ts := inputEditor(t, "abcd", cur(0, 1))
	ts.SendTextInputEvent(commit("X"))
	if got := vm.Document().Text(); got != "aXbcd" {
		t.Errorf("text %q, want aXbcd", got)
	}
	vm.InputMode().Toggle()
	vm.InputMode().Toggle()
	ts.SendTextInputEvent(commit("Y"))
	if got := vm.Document().Text(); got != "aXYbcd" {
		t.Errorf("after toggling twice: %q, want aXYbcd", got)
	}
}

func TestPasteInOvertypeMode(t *testing.T) {
	tests := []struct {
		name       string
		overtype   bool
		onPaste    bool
		clip, want string
	}{
		{"overtype with overtypeOnPaste replaces", true, true, "XY", "aXYdef"},
		{"overtype without overtypeOnPaste inserts", true, false, "XY", "aXYbcdef"},
		{"insert mode inserts whatever overtypeOnPaste says", false, true, "XY", "aXYbcdef"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newClipRig(t, "abcdef", func(o *Options) { o.OvertypeOnPaste = tc.onPaste }).at(cur(0, 1))
			r.vm.InputMode().SetOvertype(tc.overtype)
			r.ts.SetClipboardText(tc.clip)
			r.run(pasteCmd)
			if got := r.vm.Document().Text(); got != tc.want {
				t.Errorf("text %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEffectiveCursorStyle(t *testing.T) {
	tests := []struct {
		name                 string
		overtype             bool
		style, overtypeStyle string
		want                 string
	}{
		{"insert mode uses editor.cursorStyle", false, "line", "block", "line"},
		{"overtype mode uses editor.overtypeCursorStyle", true, "line", "underline", "underline"},
		{"the overtype style defaults to block", true, "line", "", "block"},
		{"overtype with the same style as insert", true, "block-outline", "block-outline", "block-outline"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := goOptions()
			opts.CursorStyle, opts.OvertypeCursorStyle = tc.style, tc.overtypeStyle
			vm := NewViewModel(newTestDoc("abc"), opts, nil)
			vm.InputMode().SetOvertype(tc.overtype)
			if got := vm.EffectiveCursorStyle(); got != tc.want {
				t.Errorf("EffectiveCursorStyle = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestOvertypeModeChangesTheCursorOnScreen checks the pixels: the line cursor of insert mode becomes the block of overtype
// mode at once, and goes back.
func TestOvertypeModeChangesTheCursorOnScreen(t *testing.T) {
	r := cursorRig(t, "line", nil)
	x0, y0 := r.cursorCell(0, 4)
	inCell := func() bool { return cursorColored(r.ts.Screenshot().RGBAAt(int(x0+r.cw-2), int(y0+2))) }
	if inCell() {
		t.Fatal("the line cursor of insert mode reaches the next column")
	}
	r.vm.InputMode().SetOvertype(true)
	r.ts.Pump(time.Millisecond)
	if !inCell() {
		t.Error("in overtype mode the cursor is not a block")
	}
	r.vm.InputMode().SetOvertype(false)
	r.ts.Pump(time.Millisecond)
	if inCell() {
		t.Error("back in insert mode the cursor is still a block")
	}
}

func TestGoldenOvertypeCursors(t *testing.T) {
	for _, style := range []string{"block", "underline", "line"} {
		t.Run(style, func(t *testing.T) {
			r := cursorRig(t, "line", func(o *Options) { o.OvertypeCursorStyle = style })
			r.vm.InputMode().SetOvertype(true)
			r.ts.Pump(time.Millisecond)
			testutil.MatchesGolden(r.ts, "cursor_overtype_"+style)
		})
	}
}

// TestEditorsShareTheInputMode: the mode belongs to whoever gave it to the editors, as VS Code's belongs to the window.
func TestEditorsShareTheInputMode(t *testing.T) {
	shared := NewInputMode()
	a := NewViewModel(newTestDoc("one"), goOptions(), nil)
	b := NewViewModel(newTestDoc("two"), goOptions(), nil)
	a.SetInputMode(shared)
	b.SetInputMode(shared)
	shared.Toggle()
	if a.EffectiveCursorStyle() != "block" || b.EffectiveCursorStyle() != "block" {
		t.Errorf("styles %q and %q, want block in both", a.EffectiveCursorStyle(), b.EffectiveCursorStyle())
	}
	b.SetInputMode(nil) // the editor goes back to a mode of its own, which is insert mode
	if b.InputMode() == shared || b.InputMode().Overtype() {
		t.Error("an editor that let go of the shared mode still follows it")
	}
	shared.Toggle()
	if a.InputMode().Overtype() {
		t.Error("the editor that follows the shared mode did not follow it back")
	}
}
