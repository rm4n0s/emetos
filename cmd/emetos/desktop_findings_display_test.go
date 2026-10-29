//go:build emetos_display

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The findings of the desktop pass that step 17b.6 fixes (docs/DESKTOP-PASS.md D4, D6, D10, D11); D5 is in
// desktop_fastkeys_display_test.go.

// cutTexts are the texts on screen that an ellipsis or a clip shortens, among those that pass keep.
func cutTexts(a *display.App, keep func(app.DevText) bool) []app.DevText {
	var cut []app.DevText
	for _, tx := range a.Texts() {
		if tx.Cut && keep(tx) {
			cut = append(cut, tx)
		}
	}
	return cut
}

// TestDesktopWindowTabLabelIsNotCutWhenThereIsRoom (D11): the label of a window tab is cut only when the strip has no room for it.
func TestDesktopWindowTabLabelIsNotCutWhenThereIsRoom(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "a.txt"}, Project: diffProject})
	a.Settle()
	a.Click(186, 18) // the plus button
	a.WaitText("Relay")
	a.ClickText("Relay")
	a.Eventually("a second window tab", func() (bool, string) {
		w := a.Workbench().WindowTabs
		return len(w) == 2 && w[1].Active, a.Dump()
	})
	a.Settle()
	shot(t, a, "wtabs-label")
	if cut := cutTexts(a, inRect(0, 0, 1280, 36)); len(cut) != 0 {
		t.Errorf("the strip of the window tabs has room, and these are cut: %+v", cut)
	}
}

// TestDesktopHoverHidesWhenTheContextMenuOpens (D6): the hover of a word goes away when a right click opens the context menu, and
// does not come back while the menu is open (VS Code hides the hover on a mouse press).
func TestDesktopHoverHidesWhenTheContextMenuOpens(t *testing.T) {
	a := startGoApp(t, goplsOptions{})
	// the pointer comes to the word main and the right button is pressed a moment later, before the hover is shown: the hover that
	// was on its way came after the menu and stayed beside it
	a.RightClick(editorColumnX(7), editorLine(5))
	a.WaitText("Select All")
	a.Settle()
	// the delay of the hover is 300 ms: it must not come after the menu opened
	time.Sleep(900 * time.Millisecond)
	a.Settle()
	if a.Editor().HoverVisible {
		t.Error("the hover came back while the context menu is open")
	}
}

// TestDesktopPanelTabsAreWholeAndOverflow (D4): the title of the panel keeps its tabs whole at 1280 and 1024 px, with a count on
// Problems, the filter of the Problems keeps its room beside them (the actions of a view are measured first, decision 370), and
// the tabs that do not fit go into the menu of a "..." button, which lists them.
func TestDesktopPanelTabsAreWholeAndOverflow(t *testing.T) {
	a := startGoApp(t, goplsOptions{Diagnostics: true})
	a.Press("ctrl+shift+m")
	a.WaitText("undefined: fmtt")
	tabRow := func() (float64, bool) {
		tx, ok := a.Text("PROBLEMS")
		return tx.Y, ok
	}
	// the tabs of the title strip: the upper-case titles on the row of PROBLEMS, and the "..." button's icon
	strip := func() (tabs []string, overflow bool, cut []app.DevText) {
		y, ok := tabRow()
		if !ok {
			t.Fatal("no PROBLEMS tab on screen")
		}
		for _, tx := range a.Texts() {
			if tx.Y < y-6 || tx.Y > y+6 || tx.X < 348 {
				continue
			}
			switch {
			case tx.Text == "\uea7c":
				overflow = true
			case tx.Text != "" && tx.Text == strings.ToUpper(tx.Text) && strings.ContainsAny(tx.Text, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"):
				tabs = append(tabs, tx.Text)
				if tx.Cut {
					cut = append(cut, tx)
				}
			}
		}
		return tabs, overflow, cut
	}
	for _, width := range []int{1280, 1024} {
		a.ResizeWindow(width, 800)
		shot(t, a, "panel-tabs-"+itoa(width))
		tabs, overflow, cut := strip()
		if len(cut) != 0 {
			t.Errorf("at %d px these texts of the panel's title are cut: %+v", width, cut)
		}
		// The actions of the view keep their room and the tabs get what is left (decision 370): with the tabs of every extension
		// that adds a panel view (Go, Redis, Containers, Podman...) some go into the "..." menu, which is expected; what matters is
		// that the ones that show are whole (checked above), that the active tab is one of them and that the filter of the Problems
		// has the room VS Code gives it (150 px at least) on the row of the tabs, clear of them.
		if len(tabs) < 1 || !contains(tabs, "PROBLEMS") {
			t.Errorf("at %d px the tabs are %v: the active tab PROBLEMS must show", width, tabs)
		}
		y, _ := tabRow()
		var filter *app.DevInput
		for _, in := range a.Inputs() {
			if in.Y > y-18 && in.Y < y+18 {
				filter = &in
				break
			}
		}
		if filter == nil {
			t.Errorf("at %d px the filter of the Problems is not in the title strip (tabs %v, overflow %v)", width, tabs, overflow)
			continue
		}
		if filter.Width < 150 {
			t.Errorf("at %d px the filter is %.1f px wide, VS Code gives it 150 at least", width, filter.Width)
		}
		for _, tx := range a.Texts() {
			if tx.Y > y-6 && tx.Y < y+6 && tx.X >= 348 && tx.X < filter.X && tx.X+tx.Width > filter.X+0.5 {
				t.Errorf("at %d px the text %q (%.1f to %.1f) runs over the filter that starts at %.1f", width, tx.Text, tx.X, tx.X+tx.Width, filter.X)
			}
		}
	}
	// a narrow window: some tabs go into the menu, those that show are whole
	all, _, _ := strip()
	a.ResizeWindow(760, 800)
	shot(t, a, "panel-tabs-narrow")
	tabs, overflow, cut := strip()
	if len(cut) != 0 {
		t.Errorf("at 760 px these texts of the panel's title are cut: %+v", cut)
	}
	if !overflow || len(tabs) >= len(all) {
		t.Fatalf("at 760 px the tabs %v (of %v) with overflow %v: want some of them in a menu", tabs, all, overflow)
	}
	more, ok := a.Text("")
	if !ok {
		t.Fatal("no \"...\" button")
	}
	a.Click(more.X+more.Width/2, more.Y+more.Height/2)
	hidden := 0
	for _, title := range all {
		if !contains(tabs, title) {
			hidden++
			// the menu shows a hidden tab with its own capitalization
			a.WaitText(strings.Title(strings.ToLower(title))) //nolint:staticcheck // ASCII titles
		}
	}
	shot(t, a, "panel-tabs-menu")
	if hidden == 0 {
		t.Error("no tab is hidden")
	}
}
