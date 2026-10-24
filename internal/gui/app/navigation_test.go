package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// waitFor pumps the frames until a condition holds; the files are read on goroutines.
func (a *groupApp) waitFor(what string, cond func() bool) {
	a.t.Helper()
	for range 200 {
		if cond() {
			return
		}
		a.pump()
	}
	a.t.Fatalf("timed out waiting for %s; tabs %v", what, a.tabNames())
}

// tap presses and lets go of a key that has a name on the keyboard, and reports whether it was used.
func (a *groupApp) tap(k keybinding.Key) bool {
	a.t.Helper()
	return a.ts.SendKeyEvent(logicalOf(k), gluttertest.KeyOpts{Physical: canonicalPhysical[k]})
}

// hold presses modifier keys and keeps them down; release lets them go again.
func (a *groupApp) hold(keys ...services.LogicalKeyboardKey) {
	for _, k := range keys {
		a.ts.SendKeyDownEvent(k)
	}
}

func (a *groupApp) release(keys ...services.LogicalKeyboardKey) {
	for i := len(keys) - 1; i >= 0; i-- {
		a.ts.SendKeyUpEvent(keys[i])
	}
	a.pump()
}

const (
	keyCtrl  = services.LogicalKeyboardKeyControl
	keyShift = services.LogicalKeyboardKeyShift
)

// pickerActive is the label of the active row of the quick input.
func (a *groupApp) pickerActive() string {
	row, ok := a.win.QuickInput.Picker().Active()
	if !ok {
		return ""
	}
	return row.Item.Label
}

// pickerLabels are the labels of the rows of the quick input.
func (a *groupApp) pickerLabels() []string {
	var out []string
	for _, r := range a.win.QuickInput.Picker().Rows() {
		out = append(out, r.Item.Label)
	}
	return out
}

// line is the line (counted from 0) of the main cursor of the editor that is shown.
func (a *groupApp) line() int { return a.vm().Cursors().Primary().Position().Line }

// gotoLine does what the user does: Ctrl+G, the line number, Enter.
func (a *groupApp) gotoLine(n string) {
	a.t.Helper()
	a.press("ctrl+g")
	a.ts.EnterText(gluttertest.First(gluttertest.ByType[*widgets.EditableText]()), ":"+n)
	a.pump()
	a.press("enter")
	a.pump()
}

func (a *groupApp) cursorTo(line int) {
	a.t.Helper()
	a.vm().GoToPosition(buffer.Position{Line: line})
	a.pump()
}

func TestCtrlTabOpensTheEditorPickerAtThePreviousEditor(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.clickTab("b.go")
	a.clickTab("c.go") // the order of use: c, b, a
	ctl := a.win.QuickInput
	a.hold(keyCtrl)
	if !a.tap(keybinding.Tab) {
		t.Fatal("Ctrl+Tab was not used")
	}
	a.pump()
	if !ctl.Visible() || ctl.Provider().Info().Prefix != quickinput.EditorsActiveGroupPrefix {
		t.Fatalf("the quick input: visible %v", ctl.Visible())
	}
	if got := a.pickerLabels(); !slices.Equal(got, []string{"c.go", "b.go", "a.go"}) {
		t.Errorf("rows %v, want the order of use", got)
	}
	if got := a.pickerActive(); got != "b.go" {
		t.Errorf("active %s, want the editor used before the active one", got)
	}
	if a.active() != "c.go" {
		t.Errorf("the switcher opened the editor before the keys were let go: %s", a.active())
	}
	if a.ts.Exists(gluttertest.ByType[*kit.InputBox]()) && a.ts.Size(gluttertest.ByType[*kit.InputBox]()).Height > 1 {
		t.Errorf("the switcher shows an input")
	}
	// Ctrl is let go: the editor opens
	a.release(keyCtrl)
	if ctl.Visible() || a.active() != "b.go" {
		t.Errorf("visible %v active %s, want b.go", ctl.Visible(), a.active())
	}
	// and it is the editor the next Ctrl+Tab goes back from
	a.hold(keyCtrl)
	a.tap(keybinding.Tab)
	a.pump()
	if got := a.pickerLabels(); !slices.Equal(got, []string{"b.go", "c.go", "a.go"}) {
		t.Errorf("rows %v after the switch", got)
	}
	a.release(keyCtrl)
	if a.active() != "c.go" {
		t.Errorf("active %s, want c.go", a.active())
	}
	a.noErrors()
}

func TestCtrlTabAgainMovesDownTheSwitcher(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go", "d.go"}, "a.go", "b.go", "c.go", "d.go")
	a.clickTab("b.go")
	a.clickTab("c.go")
	a.clickTab("d.go") // d, c, b, a
	a.hold(keyCtrl)
	a.tap(keybinding.Tab)
	a.pump()
	if a.pickerActive() != "c.go" {
		t.Fatalf("active %s", a.pickerActive())
	}
	a.tap(keybinding.Tab)
	a.pump()
	if got := a.pickerActive(); got != "b.go" {
		t.Errorf("after the second Tab the active row is %s, want b.go", got)
	}
	// Ctrl+Shift+Tab goes up
	a.hold(keyShift)
	a.tap(keybinding.Tab)
	a.pump()
	if got := a.pickerActive(); got != "c.go" {
		t.Errorf("after Shift+Tab the active row is %s, want c.go", got)
	}
	a.release(keyShift)
	if !a.win.QuickInput.Visible() {
		t.Fatalf("Shift let go with Ctrl held chose the editor")
	}
	a.tap(keybinding.Tab)
	a.tap(keybinding.Tab) // goes round to the top of the list
	a.tap(keybinding.Tab)
	a.pump()
	if got := a.pickerActive(); got != "d.go" {
		t.Errorf("active %s, want d.go after going round", got)
	}
	a.release(keyCtrl)
	if a.active() != "d.go" || a.win.QuickInput.Visible() {
		t.Errorf("active %s", a.active())
	}
}

func TestCtrlTabPressedTwiceQuicklyMovesBeforeTheInputHasTheFocus(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.clickTab("b.go")
	a.clickTab("c.go") // c, b, a
	a.hold(keyCtrl)
	a.tap(keybinding.Tab)
	a.tap(keybinding.Tab) // no frame in between: the editor still has the focus
	a.pump()
	if got := a.pickerActive(); got != "a.go" {
		t.Errorf("active %s, want a.go", got)
	}
	a.release(keyCtrl)
	if a.active() != "a.go" {
		t.Errorf("active %s", a.active())
	}
}

func TestCtrlShiftTabStartsAtTheEditorUsedLongestAgo(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.clickTab("b.go")
	a.clickTab("c.go") // c, b, a
	a.hold(keyCtrl, keyShift)
	if !a.tap(keybinding.Tab) {
		t.Fatal("Ctrl+Shift+Tab was not used")
	}
	a.pump()
	if got := a.pickerActive(); got != "a.go" {
		t.Fatalf("active %s, want the last one", got)
	}
	a.release(keyCtrl, keyShift)
	if a.active() != "a.go" || a.win.QuickInput.Visible() {
		t.Errorf("active %s", a.active())
	}
}

func TestEditorPickerWithTheReleaseMissed(t *testing.T) {
	setup := func(t *testing.T) *groupApp {
		a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
		a.clickTab("b.go")
		a.clickTab("c.go")
		// the command runs while the keyboard holds no Ctrl: the release went somewhere else
		a.run("workbench.action.quickOpenPreviousRecentlyUsedEditorInGroup")
		if !a.win.QuickInput.Visible() || a.pickerActive() != "b.go" {
			t.Fatalf("visible %v active %q", a.win.QuickInput.Visible(), a.pickerActive())
		}
		return a
	}
	t.Run("a key without Ctrl stops the wait and Enter chooses", func(t *testing.T) {
		a := setup(t)
		a.tap(keybinding.DownArrow)
		a.pump()
		a.hold(keyCtrl)
		a.release(keyCtrl)
		if !a.win.QuickInput.Visible() || a.active() != "c.go" {
			t.Fatalf("a Ctrl that came later chose an editor: visible %v active %s", a.win.QuickInput.Visible(), a.active())
		}
		a.tap(keybinding.Enter)
		a.pump()
		if a.win.QuickInput.Visible() || a.active() != "a.go" {
			t.Errorf("visible %v active %s, want a.go", a.win.QuickInput.Visible(), a.active())
		}
	})
	t.Run("Escape closes it", func(t *testing.T) {
		a := setup(t)
		a.tap(keybinding.Escape)
		a.pump()
		if a.win.QuickInput.Visible() || a.active() != "c.go" {
			t.Errorf("visible %v active %s", a.win.QuickInput.Visible(), a.active())
		}
	})
	t.Run("a click chooses", func(t *testing.T) {
		a := setup(t)
		a.ts.Tap(gluttertest.ByKey(quickinput.RowKey(2)))
		a.pump()
		if a.win.QuickInput.Visible() || a.active() != "a.go" {
			t.Errorf("visible %v active %s", a.win.QuickInput.Visible(), a.active())
		}
	})
	t.Run("losing the window closes it", func(t *testing.T) {
		a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
		a.hold(keyCtrl)
		a.tap(keybinding.Tab)
		a.pump()
		if !a.win.QuickInput.Visible() {
			t.Fatal("not visible")
		}
		a.ts.SetViewFocus(ui.ImplicitViewID, false)
		a.pump()
		if a.win.QuickInput.Visible() {
			t.Errorf("the switcher is stuck open after the window lost the focus")
		}
		a.release(keyCtrl)
		if a.active() != "a.go" {
			t.Errorf("active %s: the release after the blur chose an editor", a.active())
		}
	})
	t.Run("losing the window with closeOnFocusLost off stops the wait only", func(t *testing.T) {
		a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
		a.setting("workbench.quickOpen.closeOnFocusLost", false)
		a.hold(keyCtrl)
		a.tap(keybinding.Tab)
		a.pump()
		a.ts.SetViewFocus(ui.ImplicitViewID, false)
		a.pump()
		a.release(keyCtrl)
		if !a.win.QuickInput.Visible() || a.active() != "a.go" {
			t.Fatalf("visible %v active %s", a.win.QuickInput.Visible(), a.active())
		}
		a.ts.SetViewFocus(ui.ImplicitViewID, true)
		a.pump()
		a.tap(keybinding.Enter)
		a.pump()
		if a.win.QuickInput.Visible() || a.active() != "b.go" {
			t.Errorf("visible %v active %s", a.win.QuickInput.Visible(), a.active())
		}
	})
}

func TestCtrlTabDoesNothingInAnEmptyGroup(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"})
	a.hold(keyCtrl)
	a.tap(keybinding.Tab)
	a.pump()
	a.release(keyCtrl)
	if a.win.QuickInput.Visible() {
		t.Errorf("the switcher opened over an empty group")
	}
}

func TestEditorPickerCloseButtonClosesTheEditorAndStaysOpen(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.run("workbench.action.showEditorsInActiveGroup")
	// the order of use: a is shown, c and b were shown before it
	if got := a.pickerLabels(); !slices.Equal(got, []string{"a.go", "c.go", "b.go"}) {
		t.Fatalf("rows %v", got)
	}
	rows := a.win.QuickInput.Picker().Rows()
	a.win.QuickInput.Trigger(rows[1], 0, quickinput.KeyMods{})
	a.pump()
	if !a.win.QuickInput.Visible() || !slices.Equal(a.pickerLabels(), []string{"a.go", "b.go"}) {
		t.Errorf("visible %v rows %v", a.win.QuickInput.Visible(), a.pickerLabels())
	}
	wantNames(t, a.tabNames(), "a.go", "b.go")
}

func TestEditorPickerShowsAllTheGroups(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go")
	a.run("workbench.action.splitEditorRight")
	a.show("c.go", false)
	ctl := a.win.QuickInput
	a.press("ctrl+k ctrl+p")
	if !ctl.Visible() || ctl.Provider().Info().Prefix != quickinput.EditorsPrefix {
		t.Fatalf("Ctrl+K Ctrl+P: visible %v", ctl.Visible())
	}
	var heads []string
	for _, r := range ctl.Picker().Rows() {
		if r.Separator != "" {
			heads = append(heads, r.Separator+":"+r.Item.Label)
		}
	}
	if !slices.Equal(heads, []string{"Group 1:a.go", "Group 2:a.go"}) {
		t.Errorf("headings %v, rows %v", heads, a.pickerLabels())
	}
	// choosing an editor of the other group shows it there
	rows := ctl.Picker().Rows()
	last := rows[len(rows)-1]
	ctl.Picker().SetActive(len(rows) - 1)
	ctl.Accept(quickinput.KeyMods{})
	a.pump()
	g := a.shell().activeGroup()
	if g.Model.ActiveEditor().Name() != last.Item.Label || g != a.shell().groupAt(2) {
		t.Errorf("active group %d editor %s, want group 2 and %s", g.ID(), g.Model.ActiveEditor().Name(), last.Item.Label)
	}
}

func TestBackAndForwardThroughEditorsAndJumps(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "long.go", "c.go"}, "a.go", "long.go", "c.go")
	a.clickTab("long.go")
	if a.win.Keys.Get("canNavigateBack") != true {
		t.Errorf("canNavigateBack = %v after two editors were shown", a.win.Keys.Get("canNavigateBack"))
	}
	a.gotoLine("100") // a jump: the line it left and the line it went to are locations
	if got := a.line(); got != 99 {
		t.Fatalf("line %d after go to line 100", got)
	}
	a.gotoLine("200")
	if got := a.line(); got != 199 {
		t.Fatalf("line %d after go to line 200", got)
	}
	// Back goes through the lines, then to the editors; the stack is not changed by going back
	steps := []struct {
		keys, tab string
		line      int
	}{
		{"ctrl+alt+-", "long.go", 99},
		{"ctrl+alt+-", "long.go", 0},
		{"ctrl+alt+-", "a.go", 0},
		{"ctrl+shift+-", "long.go", 0},
		{"ctrl+shift+-", "long.go", 99},
		{"ctrl+shift+-", "long.go", 199},
	}
	for i, s := range steps {
		a.press(s.keys)
		a.waitFor("the location", func() bool { return a.active() == s.tab && a.line() == s.line })
		if a.active() != s.tab || a.line() != s.line {
			t.Errorf("step %d (%s): %s line %d, want %s line %d", i, s.keys, a.active(), a.line(), s.tab, s.line)
		}
	}
	if a.win.Keys.Get("canNavigateForward") != false {
		t.Errorf("canNavigateForward = %v at the end of the stack", a.win.Keys.Get("canNavigateForward"))
	}
	a.noErrors()
}

func TestMovingTheCursorInSmallStepsIsOneLocation(t *testing.T) {
	a := newGroupApp(t, []string{"long.go", "b.go"}, "long.go", "b.go")
	a.clickTab("long.go")
	a.cursorTo(3)
	a.cursorTo(7)
	a.clickTab("b.go")
	a.press("ctrl+alt+-")
	a.waitFor("long.go", func() bool { return a.active() == "long.go" })
	if got := a.line(); got != 7 {
		t.Errorf("line %d, want the last place of the small moves", got)
	}
	if a.win.Keys.Get("canNavigateBack") != true {
		t.Errorf("there is a location before long.go: b.go was opened first")
	}
}

func TestGoingBackToAClosedEditorOpensItAgain(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "long.go", "c.go"}, "a.go", "long.go", "c.go")
	a.clickTab("long.go")
	a.cursorTo(150)
	a.clickTab("c.go")
	a.clickTab("long.go")
	a.run("workbench.action.closeActiveEditor") // long.go closes
	wantNames(t, a.tabNames(), "a.go", "c.go")
	// back goes to the place in long.go that was left when c.go was shown
	a.run("workbench.action.navigateBack")
	a.waitFor("long.go", func() bool { return slices.Contains(a.tabNames(), "long.go") && a.active() == "long.go" })
	if got := a.line(); got != 150 {
		t.Errorf("line %d, want 150", got)
	}
	a.noErrors()
}

func TestGoToLastEditLocation(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "long.go", "c.go"}, "a.go", "long.go", "c.go")
	a.clickTab("long.go")
	a.cursorTo(40)
	if err := a.vm().RunCommand("type", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	a.clickTab("a.go")
	a.clickTab("c.go")
	if a.win.Keys.Get("canNavigateToLastEditLocation") != true {
		t.Fatalf("canNavigateToLastEditLocation = %v", a.win.Keys.Get("canNavigateToLastEditLocation"))
	}
	a.press("ctrl+k ctrl+q")
	a.waitFor("the edit", func() bool { return a.active() == "long.go" })
	if got := a.line(); got != 40 {
		t.Errorf("line %d, want the line that was edited", got)
	}
}

func TestReopenClosedEditor(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "long.go", "c.go"}, "a.go", "long.go", "c.go")
	a.clickTab("long.go")
	a.cursorTo(120)
	a.press("ctrl+w")
	wantNames(t, a.tabNames(), "a.go", "c.go")
	if a.win.Keys.Get("canReopenClosedEditor") != true {
		t.Fatalf("canReopenClosedEditor = %v", a.win.Keys.Get("canReopenClosedEditor"))
	}
	a.press("ctrl+shift+t")
	a.waitFor("long.go", func() bool { return slices.Contains(a.tabNames(), "long.go") })
	wantNames(t, a.tabNames(), "a.go", "long.go", "c.go") // at the place it had
	if a.active() != "long.go" {
		t.Errorf("active %s", a.active())
	}
	if got := a.line(); got != 120 {
		t.Errorf("the cursor is on line %d, want 120: the view comes back", got)
	}
	if it := a.item("long.go"); it.Preview {
		t.Errorf("a reopened editor is kept")
	}
	if a.win.Keys.Get("canReopenClosedEditor") != false {
		t.Errorf("canReopenClosedEditor = %v with nothing left", a.win.Keys.Get("canReopenClosedEditor"))
	}
	// with the setting off the view does not come back
	a.press("ctrl+w")
	a.setting("workbench.editor.restoreViewState", false)
	a.press("ctrl+shift+t")
	a.waitFor("long.go again", func() bool { return slices.Contains(a.tabNames(), "long.go") })
	if got := a.line(); got != 0 {
		t.Errorf("line %d, want 0 without workbench.editor.restoreViewState", got)
	}
	a.noErrors()
}

func TestReopenClosedEditorsThatClosedTogetherComeBackTogether(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go", "d.go"}, "a.go", "b.go", "c.go", "d.go")
	a.run("workbench.action.closeOtherEditors") // b.go, c.go and d.go close in one command
	wantNames(t, a.tabNames(), "a.go")
	a.press("ctrl+shift+t")
	a.waitFor("the batch", func() bool { return len(a.tabNames()) == 4 })
	got := a.tabNames()
	slices.Sort(got)
	wantNames(t, got, "a.go", "b.go", "c.go", "d.go")
	if a.win.Keys.Get("canReopenClosedEditor") != false {
		t.Errorf("the batch is taken: canReopenClosedEditor = %v", a.win.Keys.Get("canReopenClosedEditor"))
	}
}

func TestReopenClosedEditorSkipsAFileThatIsGone(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.clickTab("c.go")
	a.press("ctrl+w") // c.go
	a.clickTab("b.go")
	a.press("ctrl+w") // b.go, the last one
	wantNames(t, a.tabNames(), "a.go")
	if err := os.Remove(a.files["b.go"]); err != nil {
		t.Fatal(err)
	}
	// the last editor to close is gone: the one before it comes back
	a.press("ctrl+shift+t")
	a.waitFor("c.go", func() bool { return slices.Contains(a.tabNames(), "c.go") })
	wantNames(t, a.tabNames(), "a.go", "c.go")
}

func TestClearEditorHistory(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	a.clickTab("b.go")
	a.press("ctrl+w")
	if a.win.Keys.Get("canNavigateBack") == nil {
		t.Fatalf("canNavigateBack unset")
	}
	a.run("workbench.action.clearEditorHistory")
	if !a.win.Dialogs.IsShowing() {
		t.Fatal("no question about clearing the history")
	}
	spec, _ := a.win.Dialogs.Current()
	if !strings.Contains(spec.Message, "history of recently opened editors") {
		t.Errorf("question %q", spec.Message)
	}
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText("Clear")))
	a.pump()
	if a.win.Keys.Get("canReopenClosedEditor") != false || a.win.Keys.Get("canNavigateBack") != false {
		t.Errorf("canReopenClosedEditor %v canNavigateBack %v", a.win.Keys.Get("canReopenClosedEditor"), a.win.Keys.Get("canNavigateBack"))
	}
	if a.win.Recent.Len() != 0 {
		t.Errorf("%d recent files", a.win.Recent.Len())
	}
}

func TestNextAndPreviousEditorGoAcrossGroups(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go")
	a.run("workbench.action.splitEditorRight")
	a.show("c.go", false) // group 2: a.go c.go
	s := a.shell()
	at := func() string {
		g := s.activeGroup()
		return strings.Join([]string{string(rune('0' + s.group.grid.Index(g) + 1)), g.Model.ActiveEditor().Name()}, ":")
	}
	if at() != "2:c.go" {
		t.Fatalf("at %s", at())
	}
	for _, step := range []struct{ id, want string }{
		{"workbench.action.nextEditor", "1:a.go"}, // past the last editor of group 2: the first of group 1
		{"workbench.action.nextEditor", "1:b.go"},
		{"workbench.action.nextEditor", "2:a.go"},
		{"workbench.action.previousEditor", "1:b.go"},
		{"workbench.action.previousEditor", "1:a.go"},
		{"workbench.action.previousEditor", "2:c.go"},
		{"workbench.action.nextEditorInGroup", "2:a.go"}, // round the group
		{"workbench.action.previousEditorInGroup", "2:c.go"},
	} {
		a.run(step.id)
		if at() != step.want {
			t.Errorf("%s: at %s, want %s", step.id, at(), step.want)
		}
	}
}

func TestOpenRecentlyUsedEditorWalksTheOrderOfUse(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.clickTab("b.go")
	a.clickTab("c.go") // c, b, a
	a.run("workbench.action.openPreviousRecentlyUsedEditorInGroup")
	if a.active() != "b.go" {
		t.Errorf("active %s, want b.go", a.active())
	}
	// the walk goes on through the list it took, and does not swap the two editors
	a.run("workbench.action.openPreviousRecentlyUsedEditorInGroup")
	if a.active() != "a.go" {
		t.Errorf("active %s, want a.go", a.active())
	}
	a.run("workbench.action.openNextRecentlyUsedEditorInGroup")
	if a.active() != "b.go" {
		t.Errorf("active %s, want b.go going back", a.active())
	}
	// choosing a tab ends the walk
	a.clickTab("a.go")
	a.run("workbench.action.openPreviousRecentlyUsedEditor")
	if a.active() != "b.go" {
		t.Errorf("active %s, want b.go", a.active())
	}
}

func TestNavigationScopeFollowsTheSetting(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	a.clickTab("b.go")
	if a.shell().nav.history.Scope() != "default" {
		t.Fatalf("scope %s", a.shell().nav.history.Scope())
	}
	a.setting("workbench.editor.navigationScope", "editorGroup")
	a.waitFor("the scope", func() bool { return a.shell().nav.history.Scope() == "editorGroup" })
}

func TestFilesThatMoveKeepTheirLocationsAndDeletedFilesLoseThem(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.clickTab("b.go")
	a.clickTab("c.go")
	oldB, oldC := a.files["b.go"], a.files["c.go"]
	a.shell().followNavigation(func(p string) (string, bool) {
		switch p {
		case oldB:
			return oldB + ".moved", false
		case oldC:
			return p, true // deleted
		}
		return p, false
	})
	var paths []string
	locs, _ := a.shell().nav.history.Locations(editorgroup.GoAll)
	for _, l := range locs {
		paths = append(paths, filepath.Base(l.Resource.Path))
	}
	if slices.Contains(paths, "c.go") || !slices.Contains(paths, "b.go.moved") || slices.Contains(paths, "b.go") {
		t.Errorf("locations %v: b.go should have moved and c.go be gone", paths)
	}
}

func TestCtrlPageDownAndPageUpSwitchTheTabs(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	for _, step := range []struct{ keys, want string }{
		{"ctrl+pagedown", "b.go"}, {"ctrl+pagedown", "c.go"}, {"ctrl+pagedown", "a.go"}, // round the single group
		{"ctrl+pageup", "c.go"}, {"ctrl+pageup", "b.go"},
	} {
		a.press(step.keys)
		if a.active() != step.want {
			t.Errorf("%s: active %s, want %s", step.keys, a.active(), step.want)
		}
	}
}

func TestReopenClosedEditorOpensInTheActiveGroup(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go")
	a.run("workbench.action.splitEditorRight")
	a.show("c.go", false) // group 2 has a.go and c.go, and is the active group
	a.run("workbench.action.closeActiveEditor")
	s := a.shell()
	// the user goes to group 1: the editor comes back there, as VS Code's history service does
	a.run("workbench.action.focusFirstEditorGroup")
	if s.activeGroup() != s.groupAt(1) {
		t.Fatalf("the active group is not group 1")
	}
	a.press("ctrl+shift+t")
	a.waitFor("c.go in group 1", func() bool { return s.groupAt(1).Model.Count() == 3 })
	if s.groupAt(2).Model.Count() != 1 {
		t.Errorf("group 2 has %d editors", s.groupAt(2).Model.Count())
	}
	a.noErrors()
}
