package app

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
)

func chordOf(t *testing.T, label string) keybinding.Chord {
	t.Helper()
	keys, err := keybinding.ParseKeybinding(label)
	if err != nil || len(keys) != 1 {
		t.Fatalf("keybinding %q: %v %v", label, keys, err)
	}
	return keys[0]
}

func (a *keyApp) press(t *testing.T, label string) {
	t.Helper()
	sendChord(a.ts, chordOf(t, label))
	a.ts.Pump(time.Millisecond)
	a.ts.Pump(time.Millisecond)
}

func (a *keyApp) findInput() gluttertest.Finder {
	return gluttertest.First(gluttertest.ByType[*widgets.EditableText]())
}

func TestFindFromTheKeyboard(t *testing.T) {
	a := newKeyApp(t, "alpha beta\ngamma beta\nepsilon beta zeta\n")
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 7)})

	a.press(t, "ctrl+f")
	if !a.vm.FindRevealed() {
		t.Fatal("Ctrl+F did not open the find widget")
	}
	if got := a.vm.FindState().SearchString(); got != "beta" {
		t.Errorf("the search string is seeded with the word at the cursor: %q", got)
	}
	if a.vm.Focused() {
		t.Error("the focus is still in the editor")
	}

	// Enter in the find input goes to the next match
	a.press(t, "enter")
	if got := a.selections(); len(got) != 1 || got[0] != sel(1, 6, 1, 10) {
		t.Errorf("after Enter: %v", got)
	}
	a.press(t, "shift+enter")
	if got := a.selections(); got[0] != sel(0, 6, 0, 10) {
		t.Errorf("after Shift+Enter: %v", got)
	}

	// Alt+C switches match case on while the focus is in the input
	a.press(t, "alt+c")
	if !a.vm.FindState().MatchCase() {
		t.Error("Alt+C did not switch match case on")
	}
	a.press(t, "alt+c")

	// typing in the input searches
	a.ts.EnterText(a.findInput(), "zeta")
	if got := a.vm.FindState().MatchesCount(); got != 1 {
		t.Errorf("%d matches for zeta", got)
	}

	// Escape closes the widget and gives the focus back to the editor
	a.press(t, "escape")
	if a.vm.FindRevealed() || !a.vm.Focused() {
		t.Errorf("Escape: revealed %v, editor focused %v", a.vm.FindRevealed(), a.vm.Focused())
	}

	// F3 in the editor goes to the next match of the search that was there
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 0)})
	a.press(t, "f3")
	if got := a.selections(); got[0] != sel(2, 13, 2, 17) {
		t.Errorf("after F3: %v", got)
	}
	if !a.vm.FindRevealed() {
		t.Error("F3 opens the widget when it was closed")
	}
}

func TestReplaceAndSelectAllFromTheKeyboard(t *testing.T) {
	a := newKeyApp(t, "one two one\none\n")
	a.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 0, 0, 3)})
	a.press(t, "ctrl+h")
	st := a.vm.FindState()
	if !st.IsReplaceRevealed() || st.SearchString() != "one" {
		t.Fatalf("Ctrl+H: replace row %v, search %q", st.IsReplaceRevealed(), st.SearchString())
	}
	if got := a.ts.Count(gluttertest.ByType[*widgets.EditableText]()); got != 2 {
		t.Fatalf("%d inputs", got)
	}
	// the focus is in the replace input: typing there sets the replace string, and Ctrl+Enter
	// replaces everything
	a.ts.EnterText(gluttertest.Last(gluttertest.ByType[*widgets.EditableText]()), "1")
	a.press(t, "ctrl+alt+enter")
	if got := a.text(); got != "1 two 1\n1\n" {
		t.Errorf("text after Replace All: %q", got)
	}
	// Alt+Enter selects the matches of the search
	a.ts.EnterText(a.findInput(), "1")
	a.press(t, "alt+enter")
	if got := len(a.selections()); got != 3 {
		t.Errorf("%d selections after Alt+Enter", got)
	}
	if !a.vm.Focused() {
		t.Error("the editor does not have the focus after Alt+Enter")
	}
}
