package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
)

// The new-editor button of the tab strip (step 29.1, SPEC §10.3).

// plusRect is the place of the "+" of the strip of a group.
func (a *groupApp) plusRect(id int) ui.Rect {
	a.t.Helper()
	r, ok := a.shell().view(a.group(id)).plus.Rect()
	if !ok {
		a.t.Fatalf("group %d has no + button", id)
	}
	return r
}

// hasPlus tells whether the strip of a group has the button.
func (a *groupApp) hasPlus(id int) bool {
	_, ok := a.shell().view(a.group(id)).plus.Rect()
	return ok && a.ts.Exists(iconButton(newEditorTooltip))
}

// clickPlus presses the + of a group.
func (a *groupApp) clickPlus(id int) {
	a.t.Helper()
	a.ts.TapAt(center(a.plusRect(id)))
	a.pump()
}

func TestThePlusButtonShowsWithTheTabsAndInAnEmptyGroup(t *testing.T) {
	tests := []struct {
		name  string
		show  string
		open  []string
		close bool
		want  bool
	}{
		{"multiple tabs", "multiple", []string{"a.go", "b.go"}, false, true},
		{"one tab, single", "single", []string{"a.go", "b.go"}, false, true},
		{"no tabs", "none", []string{"a.go", "b.go"}, false, false},
		{"an empty group", "multiple", nil, false, true},
		{"a group that was emptied", "multiple", []string{"a.go"}, true, true},
		{"an empty group, single", "single", nil, false, true},
		{"an empty group, none", "none", nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go", "b.go"}, tt.open...)
			a.setting("workbench.editor.showTabs", tt.show)
			if tt.close {
				a.run("workbench.action.closeAllEditors")
			}
			if got := a.hasPlus(1); got != tt.want {
				t.Errorf("the + is there: %v, want %v", got, tt.want)
			}
			a.noErrors()
		})
	}
}

func TestThePlusButtonFollowsTheLastTab(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	last, plus := a.tabRect(1, "b.go"), a.plusRect(1)
	if gap := plus.Left - last.Right; gap < -0.5 || gap > 12 {
		t.Errorf("the + starts %.1f px after the last tab (%v, %v)", gap, last, plus)
	}
	if plus.Right > a.strip(1).Right {
		t.Errorf("the + %v is outside the strip %v", plus, a.strip(1))
	}
}

func TestThePlusButtonStaysInViewWhenTheTabsOverflow(t *testing.T) {
	var names []string
	for i := range 30 {
		names = append(names, "file"+string(rune('a'+i%26))+string(rune('a'+i/26))+".go")
	}
	a := newGroupApp(t, names, names...)
	strip, plus := a.strip(1), a.plusRect(1)
	if plus.Left < strip.Left || plus.Right > strip.Right+0.5 {
		t.Fatalf("the + %v is outside the strip %v with 30 tabs", plus, strip)
	}
	// it is at the right end of the strip, and the tabs scroll to its left
	if strip.Right-plus.Right > 40 {
		t.Errorf("the + %v is not at the right end of the strip %v", plus, strip)
	}
	a.clickPlus(1)
	if rows := menuRows(a.openMenu()); !slices.Equal(rows, []string{"New Text File"}) {
		t.Errorf("the menu is %v", rows)
	}
}

func TestThePlusButtonOpensItsMenuAndEscapeClosesIt(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.clickPlus(1)
	if rows := menuRows(a.openMenu()); !slices.Equal(rows, []string{"New Text File"}) {
		t.Fatalf("the menu is %v", rows)
	}
	// the menu is under the button
	if !a.tap(keybinding.Escape) {
		t.Error("Escape was not used")
	}
	a.pump()
	if n := len(a.ts.Elements(gluttertest.ByText("New Text File"))); n != 0 {
		t.Errorf("the menu is still open (%d rows)", n)
	}
	a.waitFor("the keyboard to go back to the editor", func() bool { return a.editorOf(1).focus.HasFocus() })
	a.noErrors()
}

func TestNewTextFileOfThePlusOpensInTheClickedGroup(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	a.run("workbench.action.splitEditorRight")
	if got := a.ids(); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("groups %v", got)
	}
	// group 2 is the active one; the button of group 1 is pressed
	a.clickPlus(1)
	a.chooseRow("New Text File")
	if got := a.names(1); !slices.Contains(got, "Untitled-1") || slices.Contains(a.names(2), "Untitled-1") {
		t.Errorf("group 1 has %v, group 2 has %v", got, a.names(2))
	}
	if a.activeID() != 1 {
		t.Errorf("the active group is %d", a.activeID())
	}
	a.waitFor("the new file to have the keyboard", func() bool { return a.editorOf(1).focus.HasFocus() })
	// and the button of the other group opens its own
	a.clickPlus(2)
	a.chooseRow("New Text File")
	if !slices.Contains(a.names(2), "Untitled-2") || a.activeID() != 2 {
		t.Errorf("group 2 has %v, the active group is %d", a.names(2), a.activeID())
	}
	a.noErrors()
}

func TestNewTextFileInAnEmptyGroup(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"})
	a.clickPlus(1)
	a.chooseRow("New Text File")
	if got := a.names(1); !slices.Equal(got, []string{"Untitled-1"}) {
		t.Errorf("the group has %v", got)
	}
}

func TestNewUntitledFileHonorsTheGroupOfItsArgument(t *testing.T) {
	tests := []struct {
		name   string
		args   any
		want   int // the group that gets the file
		active int
	}{
		{"no argument, the active group", nil, 2, 2},
		{"a group", map[string]any{"groupId": 1.0}, 1, 1},
		{"a group and a language", map[string]any{"groupId": 1.0, "languageId": "go"}, 1, 1},
		{"a group that is gone", map[string]any{"groupId": 9.0}, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
			a.run("workbench.action.splitEditorRight") // group 2 is active
			if err := a.runWith("workbench.action.files.newUntitledFile", tt.args); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(a.names(tt.want), "Untitled-1") {
				t.Errorf("group 1 has %v, group 2 has %v", a.names(1), a.names(2))
			}
			if a.activeID() != tt.active {
				t.Errorf("the active group is %d, want %d", a.activeID(), tt.active)
			}
		})
	}
}

func TestTheTabsNewMenuHasTheRowsOfTheCoreAndOfExtensions(t *testing.T) {
	env, st := exEnv(t, nil)
	r := env.startEx(st)
	r.command("workbench.action.splitEditorRight", nil)
	s := r.shell()
	groups := s.group.grid.Groups(editorgroup.GridAppearance)
	if len(groups) != 2 {
		t.Fatalf("%d groups", len(groups))
	}
	items := s.tabsNewItems(groups[0])
	var got []string
	for _, it := range items {
		if it.Separator {
			got = append(got, "-")
		} else {
			got = append(got, strings.TrimSpace(it.Label))
		}
	}
	// the core's row first, then a separator and the rows of the extensions; the row whose when-clause does not hold is hidden
	if want := []string{"New Text File", "-", "New Card"}; !slices.Equal(got, want) {
		t.Fatalf("the rows are %v, want %v", got, want)
	}
	// a row runs with the group of the strip it came from
	for _, it := range items {
		if it.Separator {
			continue
		}
		want := map[string]any{"groupId": float64(groups[0].ID())}
		if m, ok := it.Data.(map[string]any); !ok || m["groupId"] != want["groupId"] {
			t.Errorf("%q runs with %v, want %v", it.Label, it.Data, want)
		}
	}
	// the card opens in the group of the button that was pressed
	r.command("ex.newCard", map[string]any{"groupId": float64(groups[1].ID())})
	if got := r.layout(); len(got) != 2 || !strings.Contains(got[1], "Card c1") {
		t.Errorf("the card is not in group 2: %v", got)
	}
}

func TestOpeningTheTabsNewMenuFromADevCommand(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	if err := a.runWith("dev.openMenu", map[string]any{"id": command.EditorTabsNew, "groupId": 1.0}); err != nil {
		t.Fatal(err)
	}
	if rows := menuRows(a.openMenu()); !slices.Equal(rows, []string{"New Text File"}) {
		t.Errorf("the menu is %v", rows)
	}
	// a group that is not on screen has no button
	a.run("workbench.action.closeAllEditors")
	a.setting("workbench.editor.showTabs", "none")
	err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, "dev.openMenu", map[string]any{"id": command.EditorTabsNew})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("devOpenMenu.DevOpenTabsNewMenuUnavailable") {
		t.Errorf("error = %v", err)
	}
}
