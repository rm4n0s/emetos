package app

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The tests of the context menu of a tab, of the commands that take the tab from their argument, and of
// the commands that copy a path.

var secondaryDown = gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)}

// rightClickTab opens the context menu of a tab of a group.
func (a *groupApp) rightClickTab(id int, name string) {
	a.t.Helper()
	a.ts.TapAt(center(a.tabRect(id, name)), secondaryDown)
	a.pump()
}

// openMenu is the menu that is open.
func (a *groupApp) openMenu() []kit.MenuItem {
	a.t.Helper()
	els := a.ts.Elements(gluttertest.ByType[*kit.Menu]())
	if len(els) == 0 {
		a.t.Fatal("no menu is open")
	}
	return els[0].Widget().(*kit.Menu).Items()
}

// menuRows are the labels of the rows of a menu; a separator is "-", a dimmed row ends with "~", a
// submenu with ">".
func menuRows(items []kit.MenuItem) []string {
	var out []string
	for _, it := range items {
		switch {
		case it.Separator:
			out = append(out, "-")
		case it.Submenu:
			out = append(out, kit.MenuLabelText(it.Label)+">")
		case it.Disabled:
			out = append(out, kit.MenuLabelText(it.Label)+"~")
		default:
			out = append(out, kit.MenuLabelText(it.Label))
		}
	}
	return out
}

// chooseRow runs a row of the open menu by tapping it, and lets the command run.
func (a *groupApp) chooseRow(label string) {
	a.t.Helper()
	a.ts.Tap(gluttertest.ByText(label))
	a.pump()
}

func TestTheContextMenuOfATab(t *testing.T) {
	closeRows := []string{"Close", "Close Others", "Close to the Right", "Close Saved", "Close All", "-", "Copy Path", "Copy Relative Path", "-", "Reveal in Explorer View", "-"}
	for _, tc := range []struct {
		name  string
		setup func(a *groupApp)
		click string
		want  []string
	}{
		{"a kept tab", func(a *groupApp) {}, "b.go", append(slices.Clone(closeRows), "Keep Open~", "Pin", "-", "Split Right", "Split & Move>")},
		{"the last tab has nothing to its right", func(a *groupApp) {}, "c.go", []string{
			"Close", "Close Others", "Close to the Right~", "Close Saved", "Close All", "-", "Copy Path", "Copy Relative Path", "-", "Reveal in Explorer View", "-",
			"Keep Open~", "Pin", "-", "Split Right", "Split & Move>"}},
		{"a preview tab can be kept", func(a *groupApp) { a.show("d.go", true) }, "d.go", []string{
			// the preview opens on the right of the active tab, not at the end
			"Close", "Close Others", "Close to the Right", "Close Saved", "Close All", "-", "Copy Path", "Copy Relative Path", "-", "Reveal in Explorer View", "-",
			"Keep Open", "Pin", "-", "Split Right", "Split & Move>"}},
		{"a pinned tab can be unpinned", func(a *groupApp) { a.group(1).Model.Stick(a.group(1).Model.EditorAt(1)) }, "b.go", append(slices.Clone(closeRows), "Keep Open~", "Unpin", "-", "Split Right", "Split & Move>")},
		{"a modified tab", func(a *groupApp) { a.edit("b.go") }, "b.go", []string{
			"Close", "Close Others", "Close to the Right", "Close Saved", "Close All", "-", "Save", "Save As...", "-", "Copy Path", "Copy Relative Path", "-", "Reveal in Explorer View", "-",
			"Keep Open~", "Pin", "-", "Split Right", "Split & Move>"}},
		{"splitting down", func(a *groupApp) { a.setting("workbench.editor.openSideBySideDirection", "down") }, "a.go", append(slices.Clone(closeRows), "Keep Open~", "Pin", "-", "Split Down", "Split & Move>")},
		{"no preview editors", func(a *groupApp) { a.setting("workbench.editor.enablePreview", false) }, "a.go", append(slices.Clone(closeRows), "Pin", "-", "Split Right", "Split & Move>")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go", "b.go", "c.go", "d.go"}, "a.go", "b.go", "c.go")
			tc.setup(a)
			a.rightClickTab(1, tc.click)
			items := a.openMenu()
			if got := menuRows(items); !slices.Equal(got, tc.want) {
				t.Errorf("the menu is\n %v\nwant\n %v", got, tc.want)
			}
			a.noErrors()
		})
	}
}

func TestTheContextMenuOfAnOnlyTabHasNoOthers(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.rightClickTab(1, "a.go")
	rows := menuRows(a.openMenu())
	for _, want := range []string{"Close Others~", "Close to the Right~"} {
		if !slices.Contains(rows, want) {
			t.Errorf("the menu %v has no dimmed %q", rows, want)
		}
	}
}

func TestTheSplitAndMoveSubmenu(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	a.rightClickTab(1, "a.go")
	var sub []kit.MenuItem
	for _, it := range a.openMenu() {
		if it.Submenu {
			sub = it.Items
		}
	}
	want := []string{"Split Up", "Split Down", "Split Left", "Split Right", "-", "Move Above", "Move Below", "Move Left", "Move Right"}
	if got := menuRows(sub); !slices.Equal(got, want) {
		t.Errorf("the submenu is %v, want %v", got, want)
	}
	// each row runs its command for the tab
	for _, it := range sub {
		if it.Separator {
			continue
		}
		if want := tabContext(a.group(1), 0); it.Data == nil || it.Data.(map[string]any)["editorIndex"] != want["editorIndex"] {
			t.Errorf("%s runs with %v", it.Label, it.Data)
		}
	}
}

func TestEveryRowOfTheContextMenuRunsForTheClickedTab(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.rightClickTab(1, "b.go")
	want := tabContext(a.group(1), 1)
	for _, it := range a.openMenu() {
		if it.Separator {
			continue
		}
		if it.Submenu {
			continue
		}
		got, ok := it.Data.(map[string]any)
		if !ok || got["groupId"] != want["groupId"] || got["editorIndex"] != want["editorIndex"] {
			t.Errorf("%s runs with %v, want %v", it.Label, it.Data, want)
		}
	}
}

func TestTheCloseRowsActOnTheClickedTab(t *testing.T) {
	for _, tc := range []struct {
		name  string
		click string
		row   string
		want  []string
		// the active tab afterwards
		active string
	}{
		{"Close closes the tab that was clicked, not the active one", "c.go", "Close", []string{"a.go", "b.go", "d.go", "e.go"}, "a.go"},
		{"Close Others keeps the tab that was clicked", "c.go", "Close Others", []string{"c.go"}, "c.go"},
		{"Close to the Right closes what is right of the clicked tab", "c.go", "Close to the Right", []string{"a.go", "b.go", "c.go"}, "a.go"},
		{"Close to the Right of the first tab", "a.go", "Close to the Right", []string{"a.go"}, "a.go"},
		{"Close All", "d.go", "Close All", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go", "b.go", "c.go", "d.go", "e.go"}, "a.go", "b.go", "c.go", "d.go", "e.go")
			a.clickTab("a.go") // the active tab is not the one clicked
			a.rightClickTab(1, tc.click)
			a.chooseRow(tc.row)
			a.wantTabs(1, tc.want...)
			if tc.active != "" {
				if got := a.active(); got != tc.active {
					t.Errorf("the active tab is %s, want %s", got, tc.active)
				}
			}
			a.noErrors()
		})
	}
}

func TestCloseSavedClosesTheTabsWithoutChangesOfTheClickedGroup(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.edit("b.go")
	a.rightClickTab(1, "a.go")
	a.chooseRow("Close Saved")
	a.wantTabs(1, "b.go")
	a.noErrors()
}

func TestTheCloseRowsActOnTheGroupOfTheClickedTab(t *testing.T) {
	// the active group is group 2; the menu is that of a tab of group 1
	for _, tc := range []struct {
		row    string
		want1  []string
		want2  []string
		click  string
		dirty2 bool
	}{
		{"Close Others", []string{"b.go"}, []string{"a.go", "d.go"}, "b.go", false},
		{"Close to the Right", []string{"a.go", "b.go"}, []string{"a.go", "d.go"}, "b.go", false},
		{"Close All", nil, []string{"a.go", "d.go"}, "b.go", false},
		{"Close Saved", []string{"a.go", "b.go", "c.go"}, []string{"a.go", "d.go"}, "b.go", false},
		{"Close", []string{"a.go", "c.go"}, []string{"a.go", "d.go"}, "b.go", false},
	} {
		t.Run(tc.row, func(t *testing.T) {
			a := twoGroups(t)
			a.edit("a.go") // a.go is modified in both groups
			if tc.row == "Close Saved" {
				a.edit("c.go")
			}
			if a.activeID() != 2 {
				t.Fatalf("group %d is active", a.activeID())
			}
			a.rightClickTab(1, tc.click)
			a.chooseRow(tc.row)
			want1 := tc.want1
			if tc.row == "Close Saved" {
				want1 = []string{"a.go", "c.go"} // a.go and c.go have changes
			}
			if want1 == nil {
				// the group closes with its last tab
				if a.grid().Group(1) != nil || a.grid().Count() != 1 {
					t.Errorf("the grid is %s, want group 1 closed", a.shape())
				}
			} else {
				a.wantTabs(1, want1...)
			}
			a.wantTabs(2, tc.want2...)
			a.noErrors()
		})
	}
}

func TestKeepOpenPinAndUnpinActOnTheClickedTab(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go", "d.go"}, "a.go", "b.go")
	a.show("c.go", true)
	a.show("d.go", false) // the preview c.go stays a preview; d.go is kept
	m := a.group(1).Model
	index := func(name string) editorgroup.EditorInput { return m.EditorAt(slices.Index(a.names(1), name)) }
	if m.IsPinned(index("c.go")) {
		t.Fatal("c.go is not a preview")
	}
	a.rightClickTab(1, "c.go")
	a.chooseRow("Keep Open")
	if !m.IsPinned(index("c.go")) {
		t.Error("Keep Open did not keep the tab")
	}
	a.rightClickTab(1, "b.go")
	a.chooseRow("Pin")
	if !m.IsSticky(index("b.go")) || m.IsSticky(index("c.go")) || m.IsSticky(m.ActiveEditor()) {
		t.Errorf("Pin: b.go sticky %v, c.go sticky %v", m.IsSticky(index("b.go")), m.IsSticky(index("c.go")))
	}
	a.rightClickTab(1, "b.go")
	if rows := menuRows(a.openMenu()); !slices.Contains(rows, "Unpin") || slices.Contains(rows, "Pin") {
		t.Errorf("the menu of a pinned tab: %v", rows)
	}
	a.chooseRow("Unpin")
	if m.IsSticky(index("b.go")) {
		t.Error("Unpin did not unpin the tab")
	}
	a.noErrors()
}

func TestSplitRightFromTheMenuOfATabSplitsThatTab(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.rightClickTab(1, "c.go") // the active tab is a.go
	a.chooseRow("Split Right")
	a.wantTabs(1, "a.go", "b.go", "c.go")
	a.wantTabs(2, "c.go")
	if a.activeID() != 2 {
		t.Errorf("group %d is active, want the new group", a.activeID())
	}
	a.noErrors()
}

func TestTheSplitAndMoveRowsActOnTheClickedTab(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.rightClickTab(1, "b.go")
	a.ts.Tap(gluttertest.ByText("Split & Move"))
	a.pump()
	a.chooseRow("Move Right")
	// the editor moves to a new group on its right
	a.wantTabs(1, "a.go", "c.go")
	if a.grid().Count() != 2 || a.names(a.activeID())[0] != "b.go" {
		t.Errorf("the grid is %s, the active group has %v", a.shape(), a.names(a.activeID()))
	}
	a.noErrors()
}

func TestTheMenuOfATabAfterTheTabIsGoneDoesNothing(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	// a stale context: the tab at that index and the group are not there any more
	for _, args := range []any{
		map[string]any{"groupId": 99.0, "editorIndex": 0.0},
		map[string]any{"groupId": float64(a.group(1).ID()), "editorIndex": 7.0},
	} {
		for _, id := range []string{"workbench.action.closeActiveEditor", "workbench.action.closeOtherEditors", "workbench.action.closeEditorsToTheRight",
			"workbench.action.closeEditorsInGroup", "workbench.action.closeUnmodifiedEditors", "workbench.action.keepEditor", "workbench.action.pinEditor",
			"workbench.action.unpinEditor", "workbench.action.splitEditor", "workbench.action.moveEditorToRightGroup", "copyFilePath"} {
			if err := a.runWith(id, args); err != nil {
				t.Errorf("%s with %v: %v", id, args, err)
			}
		}
	}
	a.wantTabs(1, "a.go", "b.go")
	if a.grid().Count() != 1 {
		t.Errorf("the grid is %s", a.shape())
	}
}

func TestACommandWithoutAContextActsOnTheActiveEditor(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.clickTab("b.go")
	a.run("workbench.action.closeOtherEditors")
	a.wantTabs(1, "b.go")
}

func TestCopyingThePath(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "sub/b.go", "c.go"}, "a.go", "sub/b.go", "c.go")
	a.win.Folder = filepath.Dir(a.files["a.go"])
	clipboard := func() string {
		a.t.Helper()
		text, ok := a.ts.ClipboardText()
		if !ok {
			a.t.Fatal("nothing is on the clipboard")
		}
		return text
	}
	// from the menu of a tab: the tab that was clicked, not the active one
	a.rightClickTab(1, "b.go")
	a.chooseRow("Copy Path")
	if got, want := clipboard(), a.files["sub/b.go"]; got != want {
		t.Errorf("Copy Path put %q on the clipboard, want %q", got, want)
	}
	a.rightClickTab(1, "b.go")
	a.chooseRow("Copy Relative Path")
	if got, want := clipboard(), filepath.Join("sub", "b.go"); got != want {
		t.Errorf("Copy Relative Path put %q on the clipboard, want %q", got, want)
	}

	// the commands of the active editor, by their keys
	a.clickTab("c.go")
	a.press("ctrl+k p")
	if got, want := clipboard(), a.files["c.go"]; got != want {
		t.Errorf("ctrl+k p put %q on the clipboard, want %q", got, want)
	}
	a.ts.SetClipboardText("")
	if err := a.runWith("copyRelativeFilePath", nil); err != nil {
		t.Fatal(err)
	}
	if got := clipboard(); got != "c.go" {
		t.Errorf("copyRelativeFilePath put %q on the clipboard, want c.go", got)
	}
	if err := a.runWith("copyFilePath", nil); err != nil {
		t.Fatal(err)
	}
	if got, want := clipboard(), a.files["c.go"]; got != want {
		t.Errorf("copyFilePath put %q on the clipboard, want %q", got, want)
	}
	a.noErrors()
}

func TestPathText(t *testing.T) {
	for _, tc := range []struct {
		name     string
		path     string
		folder   string
		relative bool
		want     string
	}{
		{"the full path", "/w/src/a.go", "/w", false, "/w/src/a.go"},
		{"a path in the folder", "/w/src/a.go", "/w", true, "src/a.go"},
		{"a file in the folder itself", "/w/a.go", "/w", true, "a.go"},
		{"a file outside the folder is the full path", "/other/a.go", "/w", true, "/other/a.go"},
		{"a file next to the folder is outside", "/w2/a.go", "/w", true, "/w2/a.go"},
		{"a file above the folder is outside", "/a.go", "/w", true, "/a.go"},
		{"no folder, no relative path", "/w/a.go", "", true, "/w/a.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathText(tc.path, tc.folder, tc.relative); got != tc.want {
				t.Errorf("pathText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCopyingThePathOfAnEditorWithNoFileDoesNothing(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.run("workbench.action.closeActiveEditor")
	a.ts.SetClipboardText("kept")
	a.run("copyFilePath")
	a.run("workbench.action.files.copyPathOfActiveFile")
	if got, _ := a.ts.ClipboardText(); got != "kept" {
		t.Errorf("the clipboard is %q", got)
	}
}

func TestSplitEditorsVerticallyFollowsTheSetting(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	key := func() any { return a.win.Keys.Value("splitEditorsVertically") }
	if key() != false {
		t.Errorf("splitEditorsVertically is %v by default", key())
	}
	a.setting("workbench.editor.openSideBySideDirection", "down")
	if key() != true {
		t.Errorf("splitEditorsVertically is %v with the direction down", key())
	}
}

func TestTheCommandsOfTheMenuOfATabAreRegistered(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	for _, it := range a.win.Process.Menus.All() {
		if it.Menu != command.EditorTitleContext && it.Menu != command.EditorSplitMoveSubmenu || it.Command == "" {
			continue
		}
		if _, ok := a.win.Process.Commands.Lookup(it.Command); !ok {
			t.Errorf("%s is not registered", it.Command)
		}
		// a handler is bound: running it with no context must not say the command has none
		if err := a.win.Process.Commands.Run(command.Context{Ctx: context.Background(), Source: command.SourceAPI}, it.Command, map[string]any{"groupId": 99.0}); err != nil {
			if appErr, ok := errors.FromError(err); ok && appErr.Tag == "CommandHandlerMissing" {
				t.Errorf("%s has no handler", it.Command)
			}
		}
	}
}
