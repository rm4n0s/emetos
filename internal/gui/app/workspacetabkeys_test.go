package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// The tests of switching and arranging the workspace tabs (step 9b.5, SPEC §10.14): the commands and their keys, the
// picker, the context keys, the menus, and dragging a tab.

// activeIndex is the place of the active tab in the strip.
func (a *multiApp) activeIndex() int { return a.m.IndexOf(a.m.ActiveTab()) }

// pressKeys sends the keys of a shortcut to the window and reports whether a key binding used them.
func (a *multiApp) pressKeys(keys string) bool {
	a.t.Helper()
	handled := sendChord(a.ts, chordOf(a.t, keys))
	a.settle()
	return handled
}

// focusActive gives the editor of the active tab the keyboard focus, so that the keys reach the dispatcher of the tab.
func (a *multiApp) focusActive() {
	a.t.Helper()
	w := a.m.Active()
	if err := a.command(w, "dev.openFile", filepath.Join(w.Folder, "a.go")); err != nil {
		// the tab has no a.go: any file of the folder will do
		a.t.Logf("no a.go in %q: %v", w.Folder, err)
	}
	a.settle()
}

func TestTheKeysAndTheCommandsThatSwitchTheTabs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		id    string
		keys  string
		start int
		want  int
	}{
		{"next", "emetos.workspaceTab.next", "ctrl+alt+pagedown", 0, 1},
		{"next wraps from the last tab to the first", "emetos.workspaceTab.next", "ctrl+alt+pagedown", 2, 0},
		{"previous", "emetos.workspaceTab.previous", "ctrl+alt+pageup", 1, 0},
		{"previous wraps from the first tab to the last", "emetos.workspaceTab.previous", "ctrl+alt+pageup", 0, 2},
		{"open1", "emetos.workspaceTab.open1", "ctrl+alt+1", 2, 0},
		{"open2", "emetos.workspaceTab.open2", "ctrl+alt+2", 0, 1},
		{"open3", "emetos.workspaceTab.open3", "ctrl+alt+3", 0, 2},
		{"open4 has no tab", "emetos.workspaceTab.open4", "ctrl+alt+4", 1, 1},
		{"open8 has no tab", "emetos.workspaceTab.open8", "ctrl+alt+8", 0, 0},
		{"open9 is the last tab", "emetos.workspaceTab.open9", "ctrl+alt+9", 0, 2},
		{"open9 on the last tab stays", "emetos.workspaceTab.open9", "ctrl+alt+9", 2, 2},
	} {
		for _, via := range []string{"key", "command"} {
			t.Run(tc.name+" by the "+via, func(t *testing.T) {
				a, _ := tabs3(t, nil)
				a.m.ActivateIndex(tc.start)
				a.settle()
				a.focusActive()
				if via == "key" {
					if !a.pressKeys(tc.keys) {
						t.Fatalf("the keys %s were not used", tc.keys)
					}
				} else if err := a.command(a.m.Active(), tc.id, nil); err != nil {
					t.Fatal(err)
				}
				if got := a.activeIndex(); got != tc.want {
					t.Errorf("the active tab is %d, want %d", got, tc.want)
				}
				if got := a.strip().Active(); got != tc.want {
					t.Errorf("the strip shows %d as the active tab, want %d", got, tc.want)
				}
				if errs := a.ts.TakeErrors(); len(errs) != 0 {
					t.Errorf("errors: %v", errs)
				}
			})
		}
	}
}

func TestTheKeysOfTheTabsAreTheDefaultOnes(t *testing.T) {
	a, _ := tabs3(t, nil)
	w := a.m.Active()
	for id, want := range map[string]string{
		"emetos.workspaceTab.next": "Ctrl+Alt+PageDown", "emetos.workspaceTab.previous": "Ctrl+Alt+PageUp",
		"emetos.workspaceTab.open1": "Ctrl+Alt+1", "emetos.workspaceTab.open5": "Ctrl+Alt+5", "emetos.workspaceTab.open9": "Ctrl+Alt+9",
		"emetos.workspaceTab.switch": "", "emetos.workspaceTab.moveLeft": "",
	} {
		if got := w.keyLabelOf(id); got != want {
			t.Errorf("%s has the key %q, want %q", id, got, want)
		}
	}
}

func TestSwitchingWithOneTabDoesNothing(t *testing.T) {
	env := newMultiEnv(t, nil)
	a := env.run(Options{Folder: env.folder("one", "a.go")})
	for _, id := range []string{"emetos.workspaceTab.next", "emetos.workspaceTab.previous", "emetos.workspaceTab.open1", "emetos.workspaceTab.open9", "emetos.workspaceTab.moveLeft", "emetos.workspaceTab.moveRight"} {
		if err := a.command(a.m.Active(), id, nil); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	if a.m.Count() != 1 || a.activeIndex() != 0 {
		t.Errorf("%d tabs, active %d", a.m.Count(), a.activeIndex())
	}
}

func TestMovingTheActiveTabLeftAndRight(t *testing.T) {
	a, f := tabs3(t, nil)
	run := func(id string, args any) {
		t.Helper()
		if err := a.command(a.m.Active(), id, args); err != nil {
			t.Fatal(err)
		}
	}
	wantOrder := func(what string, want ...string) {
		t.Helper()
		if got := a.folders(); !slices.Equal(got, want) {
			t.Errorf("%s: the tabs are %q, want %q", what, got, want)
		}
		if got := tabFoldersOf(a); !slices.Equal(got, want) {
			t.Errorf("%s: the state has %q, want %q", what, got, want)
		}
	}
	active := a.m.ActiveTab() // the third tab
	run("emetos.workspaceTab.moveRight", nil)
	wantOrder("the last tab cannot go further right", f[0], f[1], f[2])
	run("emetos.workspaceTab.moveLeft", nil)
	wantOrder("moved left", f[0], f[2], f[1])
	run("emetos.workspaceTab.moveLeft", nil)
	wantOrder("moved left again", f[2], f[0], f[1])
	run("emetos.workspaceTab.moveLeft", nil)
	wantOrder("the first tab cannot go further left", f[2], f[0], f[1])
	run("emetos.workspaceTab.moveRight", nil)
	wantOrder("moved right", f[0], f[2], f[1])
	if a.m.ActiveTab() != active || a.strip().Active() != 1 {
		t.Error("moving the tab changed the active tab")
	}
	// the argument names another tab: it moves, the active tab stays
	run("emetos.workspaceTab.moveLeft", map[string]any{"tabId": a.m.Tabs()[2].ID()})
	wantOrder("moved the tab of the argument", f[0], f[1], f[2])
	if a.m.ActiveTab() != active {
		t.Error("moving another tab changed the active tab")
	}
	if got := a.stripNames(); !slices.Equal(got, []string{"one", "two", "three"}) {
		t.Errorf("the strip shows %q", got)
	}
}

func TestTheContextKeysOfTheTabs(t *testing.T) {
	a, f := tabs3(t, nil)
	keys := func(w *Workspace) (count float64, folder, first, last bool) {
		t.Helper()
		count, _ = w.Keys.Value("workspaceTabCount").(float64)
		folder, _ = w.Keys.Value("activeWorkspaceTabHasFolder").(bool)
		first, _ = w.Keys.Value("workspaceTabIsFirst").(bool)
		last, _ = w.Keys.Value("workspaceTabIsLast").(bool)
		return
	}
	for _, w := range a.m.All() {
		if c, folder, first, last := keys(w); c != 3 || !folder || first || !last {
			t.Errorf("%q: count %v folder %v first %v last %v with the last tab active", w.Folder, c, folder, first, last)
		}
	}
	a.m.ActivateIndex(0)
	if c, folder, first, last := keys(a.m.Active()); c != 3 || !folder || !first || last {
		t.Errorf("the first tab active: count %v folder %v first %v last %v", c, folder, first, last)
	}
	// a tab without a folder opens after the active (first) one and is the active tab then: in the middle of the strip
	if _, err := a.m.NewTab(); err != nil {
		t.Fatal(err)
	}
	a.settle()
	for _, w := range a.m.All() {
		if c, folder, first, last := keys(w); c != 4 || folder || first || last {
			t.Errorf("%q: count %v folder %v first %v last %v with a No Folder tab active", w.Folder, c, folder, first, last)
		}
	}
	// a tab that closes: the last one, while the tab of f[1] is the active one
	a.m.ActivateIndex(2)
	a.m.Close(a.m.Tabs()[3], nil)
	a.waitFor("the tab to close", func() bool { return a.m.Count() == 3 })
	if a.m.Active().Folder != f[1] {
		t.Fatalf("the active tab is %q", a.m.Active().Folder)
	}
	if c, folder, first, last := keys(a.m.Active()); c != 3 || !folder || first || !last {
		t.Errorf("after the close: count %v folder %v first %v last %v", c, folder, first, last)
	}
	// the keys are what a when clause of a key or a menu reads
	a.m.ActivateIndex(0)
	for clause, want := range map[string]bool{
		"workspaceTabCount > 1": true, "workspaceTabCount == 3": true, "workspaceTabCount > 3": false,
		"activeWorkspaceTabHasFolder": true, "workspaceTabIsFirst && !workspaceTabIsLast": true,
	} {
		w := a.m.Active()
		expr, err := contextkey.Parse(clause)
		if err != nil {
			t.Fatal(err)
		}
		if got := expr.Evaluate(w.Keys); got != want {
			t.Errorf("%q is %v, want %v", clause, got, want)
		}
	}
}

func TestTheWorkspaceTabsSubmenuOfTheViewMenuFollowsTheTabs(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a := env.run(Options{Folder: one})
	rows := func() []string {
		t.Helper()
		w := a.m.Active()
		items := w.shell.menus.Items(command.MenubarWorkspaceTabsMenu, menuContext{base: w.Keys, settings: w.Settings})
		return menuRows(items)
	}
	view := func() []string {
		w := a.m.Active()
		return menuRows(w.shell.menus.Items(command.MenubarViewMenu, menuContext{base: w.Keys, settings: w.Settings}))
	}
	if got := view(); !slices.Contains(got, "Workspace Tabs>") {
		t.Errorf("the View menu is %q", got)
	}
	// one tab: nothing to switch to or to close, and nowhere to move
	want := []string{"Next Tab~", "Previous Tab~", "Switch Tab...~", "-", "Move Tab Left~", "Move Tab Right~", "-", "Close Other Tabs~", "Close Tabs to the Right~"}
	if got := rows(); !slices.Equal(got, want) {
		t.Errorf("one tab: %q, want %q", got, want)
	}
	for _, name := range []string{"two", "three"} {
		if _, err := a.m.Open(WorkspaceSpec{Folder: env.folder(name, "a.go")}); err != nil {
			t.Fatal(err)
		}
		a.settle()
	}
	a.m.ActivateIndex(0)
	a.settle()
	want = []string{"Next Tab", "Previous Tab", "Switch Tab...", "-", "Move Tab Left~", "Move Tab Right", "-", "Close Other Tabs", "Close Tabs to the Right"}
	if got := rows(); !slices.Equal(got, want) {
		t.Errorf("the first tab: %q, want %q", got, want)
	}
	a.m.ActivateIndex(2)
	a.settle()
	want = []string{"Next Tab", "Previous Tab", "Switch Tab...", "-", "Move Tab Left", "Move Tab Right~", "-", "Close Other Tabs", "Close Tabs to the Right~"}
	if got := rows(); !slices.Equal(got, want) {
		t.Errorf("the last tab: %q, want %q", got, want)
	}
	// the rows run the commands: Next Tab wraps to the first tab
	items := a.m.Active().shell.menus.Items(command.MenubarWorkspaceTabsMenu, menuContext{base: a.m.Active().Keys, settings: a.m.Active().Settings})
	a.m.Active().shell.menus.Run(items[0])
	a.settle()
	if a.activeIndex() != 0 {
		t.Errorf("the row Next Tab left the active tab at %d", a.activeIndex())
	}
}

func TestTheCommandsOfTheTabsAreInThePalette(t *testing.T) {
	a, _ := tabs3(t, nil)
	w := a.m.Active()
	titles := map[string]string{
		"emetos.workspaceTab.next": "Next Workspace Tab", "emetos.workspaceTab.previous": "Previous Workspace Tab",
		"emetos.workspaceTab.switch": "Switch Workspace Tab...", "emetos.workspaceTab.moveLeft": "Move Workspace Tab Left",
		"emetos.workspaceTab.moveRight": "Move Workspace Tab Right",
		"emetos.workspaceTab.close":     "Close Workspace Tab", "emetos.workspaceTab.closeOthers": "Close Other Workspace Tabs",
		"emetos.workspaceTab.closeToTheRight": "Close Workspace Tabs to the Right",
	}
	for id, title := range titles {
		meta, ok := w.Commands.Lookup(id)
		if !ok || meta.Title != title || meta.Category != "Workspace" {
			t.Errorf("%s: %+v, %v", id, meta, ok)
		}
	}
	// open1 ... open9 are keys only, as VS Code's openEditorAtIndex1 ... 9
	for i := 1; i <= 9; i++ {
		id := "emetos.workspaceTab.open" + string(rune('0'+i))
		if meta, ok := w.Commands.Lookup(id); !ok || meta.Title != "" {
			t.Errorf("%s: %+v, %v", id, meta, ok)
		}
	}
	if err := a.command(w, "workbench.action.showCommands", nil); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w.QuickInput.SetValue(">workspace tab")
	a.settle()
	var got []string
	for _, r := range w.QuickInput.Picker().Rows() {
		got = append(got, r.Item.Label)
	}
	for _, want := range []string{"Workspace: Next Workspace Tab", "Workspace: Previous Workspace Tab", "Workspace: Switch Workspace Tab...", "Workspace: Move Workspace Tab Left", "Workspace: Move Workspace Tab Right", "Workspace: Close Other Workspace Tabs"} {
		if !slices.Contains(got, want) {
			t.Errorf("the palette has no %q: %q", want, got)
		}
	}
	for _, row := range got {
		if strings.Contains(row, "Open") && strings.Contains(row, "Workspace Tab ") && strings.ContainsAny(row, "123456789") {
			t.Errorf("the palette lists %q", row)
		}
	}
}

// pickerRows are the rows of the quick pick of a workspace: label and description.
func pickerRows(w *Workspace) [][2]string {
	var out [][2]string
	for _, r := range w.QuickInput.Picker().Rows() {
		out = append(out, [2]string{r.Item.Label, r.Item.Description})
	}
	return out
}

func TestThePickerOfTheTabs(t *testing.T) {
	a, f := tabs3(t, nil)
	// the second tab has unsaved changes and another profile
	w2 := a.workspaceOf(f[1])
	a.edit(w2, filepath.Join(f[1], "c.go"))
	if _, err := a.p.Profiles.Create(a.ctx, "Work", storage.CreateFrom(storage.DefaultProfile)); err != nil {
		t.Fatal(err)
	}
	if err := w2.SwitchProfile(a.ctx, "Work"); err != nil {
		t.Fatal(err)
	}
	a.m.ActivateIndex(1)
	a.settle()
	if _, err := a.m.NewTab(); err != nil {
		t.Fatal(err)
	}
	a.settle()
	a.m.ActivateIndex(1) // the tab with the changes is the active one
	a.settle()
	w := a.m.Active()
	if err := a.command(w, "emetos.workspaceTab.switch", nil); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if !w.QuickInput.Visible() {
		t.Fatal("the picker is not shown")
	}
	if got := w.QuickInput.Picker().Placeholder(); got != SwitchTabPlaceholder {
		t.Errorf("the placeholder is %q", got)
	}
	home, _ := os.UserHomeDir()
	short := func(p string) string { return shortenPath(p, home, switchTabPathWidth) }
	// the active tab first, the others in the order of the strip (the tab without a folder opened after the second);
	// the dot, the profile and "current"
	want := [][2]string{
		{"two ●", "current · " + short(f[1]) + " · Profile: Work"},
		{"one", short(f[0])},
		{"No Folder", SwitchTabNoFolder},
		{"three", short(f[2])},
	}
	if got := pickerRows(w); !slices.Equal(got, want) {
		t.Errorf("the rows are\n %q\nwant\n %q", got, want)
	}
	// the tab after the active one in the picker is the active row, so that Enter switches to it
	if row, ok := w.QuickInput.Picker().Active(); !ok || row.Item.Label != "one" {
		t.Errorf("the active row is %+v", row)
	}
	// the icons are the ones of the strip
	items := w.QuickInput.Picker().Items()
	if items[0].Icon == items[2].Icon {
		t.Errorf("a tab with a folder and one without have the same icon %x", items[0].Icon)
	}

	// typing filters by name and by path with the fuzzy scorer
	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{"thr", []string{"three"}},
		{"ONE", []string{"one"}},
		{"nofold", []string{"No Folder"}},
		{"work", []string{"two ●"}}, // the profile is in the description
		{"zzzz", nil},
	} {
		w.QuickInput.Picker().SetValue(tc.filter)
		a.settle()
		var got []string
		for _, r := range w.QuickInput.Picker().Rows() {
			got = append(got, r.Item.Label)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("the filter %q shows %q, want %q", tc.filter, got, tc.want)
		}
	}
	w.QuickInput.Picker().SetValue("thr")
	a.settle()
	// accepting the row of a tab makes it the active one
	w.QuickInput.Accept(quickinput.KeyMods{})
	a.settle()
	if a.m.Active() != a.workspaceOf(f[2]) || w.QuickInput.Visible() {
		t.Errorf("Enter on the filtered row: active %q, picker shown %v", a.m.Active().Folder, w.QuickInput.Visible())
	}
	// Escape closes it with nothing changed
	if err := a.command(a.m.Active(), "emetos.workspaceTab.switch", nil); err != nil {
		t.Fatal(err)
	}
	a.settle()
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyEscape)
	a.settle()
	if a.m.Active() != a.workspaceOf(f[2]) || a.m.Active().QuickInput.Visible() {
		t.Error("Escape changed the active tab or left the picker open")
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestThePickerTellsTabsOfTheSameNameApartAndTheKeysWork(t *testing.T) {
	env := newMultiEnv(t, nil)
	work := env.folder("work/api", "a.go")
	home := env.folder("home/api", "a.go")
	a := env.run(Options{Folder: work})
	if _, err := a.m.Open(WorkspaceSpec{Folder: home}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w := a.m.Active()
	if err := a.command(w, "workbench.action.switchWindow", nil); err != nil {
		t.Fatal(err)
	}
	a.settle()
	var labels []string
	for _, r := range pickerRows(w) {
		labels = append(labels, r[0])
	}
	if !slices.Equal(labels, []string{"api", "api"}) {
		t.Errorf("the labels are %q", labels)
	}
	// Down and Enter go to the other tab with the keys of the picker
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyEnter)
	a.settle()
	if a.m.Active() != a.workspaceOf(work) {
		t.Errorf("Enter chose %q", a.m.Active().Folder)
	}
}

func TestShortenPath(t *testing.T) {
	for _, tc := range []struct {
		name, path, home string
		limit            int
		want             string
	}{
		{"the home folder", "/home/u", "/home/u", 48, "~"},
		{"under the home folder", "/home/u/work/api", "/home/u", 48, "~/work/api"},
		{"another folder stays", "/srv/work/api", "/home/u", 48, "/srv/work/api"},
		{"a name that starts like the home folder", "/home/uu/api", "/home/u", 48, "/home/uu/api"},
		{"no home folder", "/home/u/api", "", 48, "/home/u/api"},
		{"long paths lose their first folders", "/srv/one/two/three/four/five", "", 15, "…/four/five"},
		{"the last folder stays whatever its length", "/srv/one/a-very-long-name-of-a-folder", "", 10, "…/a-very-long-name-of-a-folder"},
		{"it fits exactly", "/srv/one/two", "", 12, "/srv/one/two"},
		{"a tilde path is cut after it is written", "/home/u/one/two/three/four", "/home/u", 14, "…/three/four"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortenPath(tc.path, tc.home, tc.limit); got != tc.want {
				t.Errorf("shortenPath(%q, %q, %d) = %q, want %q", tc.path, tc.home, tc.limit, got, tc.want)
			}
		})
	}
}

// ---- dragging ----

// stripRect is the place of the strip of workspace tabs that is shown.
func (a *multiApp) stripRect() ui.Rect {
	a.t.Helper()
	box, ok := a.ts.Element(stripFinder()).FindRenderObject().(rendering.Box)
	if !ok {
		a.t.Fatal("the strip has no box")
	}
	at := box.LocalToGlobal(ui.OffsetZero, nil)
	return ui.NewRectFromLTWH(at.Dx, at.Dy, box.Size().Width, box.Size().Height)
}

// tabSpan is the place of the tab at an index of the strip; the tabs of these tests are all 120 px wide.
func (a *multiApp) tabSpan(i int) ui.Rect {
	a.t.Helper()
	strip := a.stripRect()
	if want := 120 * float64(a.m.Count()); strip.Width() != want {
		a.t.Fatalf("the strip is %v px wide, want %v", strip.Width(), want)
	}
	return ui.NewRectFromLTWH(strip.Left+120*float64(i), strip.Top, 120, strip.Height())
}

// dragTab presses the tab at an index, moves the pointer past the slop of 4 px and on to x (in the window), and gives
// the gesture with the pointer still down.
func (a *multiApp) dragTab(i int, x float64) *gluttertest.TestGesture {
	a.t.Helper()
	span := a.tabSpan(i)
	from := ui.Offset{Dx: span.Left + 60, Dy: (span.Top + span.Bottom) / 2}
	g := a.ts.StartGesture(from, primaryDown)
	g.MoveTo(ui.Offset{Dx: from.Dx + 6, Dy: from.Dy})
	g.MoveTo(ui.Offset{Dx: x, Dy: from.Dy})
	a.settle()
	return g
}

func TestDraggingAWorkspaceTabToReorderTheStrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		tab  int
		// to is the x of the drop, in tab widths from the left of the strip (0.25 is the left half of the first tab)
		to   float64
		want []int // the folders after the drop, as indexes of the folders before
	}{
		{"the first tab to the right half of the second", 0, 1.75, []int{1, 0, 2}},
		{"the first tab to the end", 0, 2.9, []int{1, 2, 0}},
		{"the first tab past the end of the strip", 0, 3.6, []int{1, 2, 0}},
		{"the last tab to the start", 2, 0.2, []int{2, 0, 1}},
		{"the last tab before the second", 2, 1.2, []int{0, 2, 1}},
		{"the middle tab to the start", 1, 0.1, []int{1, 0, 2}},
		{"the middle tab to the end", 1, 2.8, []int{0, 2, 1}},
		{"a tab on its own place", 1, 1.5, []int{0, 1, 2}},
		{"a tab on the left half of the next one", 0, 1.2, []int{0, 1, 2}},
		{"a tab on the right half of the one before", 2, 1.8, []int{0, 1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, f := tabs3(t, nil)
			active := a.m.ActiveTab() // the last tab
			left := a.stripRect().Left
			g := a.dragTab(tc.tab, left+120*tc.to)
			g.Up()
			a.settle()
			var want []string
			for _, i := range tc.want {
				want = append(want, f[i])
			}
			if got := a.folders(); !slices.Equal(got, want) {
				t.Errorf("the tabs are %q, want %q", got, want)
			}
			if got := tabFoldersOf(a); !slices.Equal(got, want) {
				t.Errorf("the state has %q, want %q", got, want)
			}
			if a.m.ActiveTab() != active {
				t.Errorf("the active tab changed to %q", a.m.Active().Folder)
			}
			if got := a.strip().Active(); got != a.m.IndexOf(active) {
				t.Errorf("the strip shows %d as the active tab, want %d", got, a.m.IndexOf(active))
			}
			if errs := a.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestALineShowsWhereTheDraggedTabWouldGo(t *testing.T) {
	a, f := tabs3(t, nil)
	left := a.stripRect().Left
	line := func() int { return a.strip().Feedback().Index() }
	if got := line(); got != -1 {
		t.Fatalf("the line is at %d before a drag", got)
	}
	g := a.dragTab(0, left+120*1.75) // the right half of the second tab: before the third
	if got := line(); got != 2 {
		t.Errorf("the line is at %d, want 2", got)
	}
	g.MoveTo(ui.Offset{Dx: left + 120*2.9, Dy: g.Position().Dy}) // past the middle of the last tab: after it
	a.settle()
	if got := line(); got != 3 {
		t.Errorf("the line is at %d, want 3", got)
	}
	g.MoveTo(ui.Offset{Dx: left + 20, Dy: g.Position().Dy}) // where the tab is: it would not move
	a.settle()
	if got := line(); got != -1 {
		t.Errorf("the line is at %d over the place of the tab, want none", got)
	}
	g.MoveTo(ui.Offset{Dx: left + 120*1.2, Dy: g.Position().Dy})
	a.settle()
	if got := line(); got != -1 {
		t.Errorf("the line is at %d next to the tab, want none (it would stay)", got)
	}
	g.MoveTo(ui.Offset{Dx: left + 120*1.8, Dy: g.Position().Dy})
	a.settle()
	g.Up()
	a.settle()
	if got := line(); got != -1 {
		t.Errorf("the line stays at %d after the drop", got)
	}
	if got := a.folders(); !slices.Equal(got, []string{f[1], f[0], f[2]}) {
		t.Errorf("the tabs are %q", got)
	}
}

func TestDraggingTheActiveWorkspaceTabKeepsItActive(t *testing.T) {
	a, f := tabs3(t, nil)
	left := a.stripRect().Left
	g := a.dragTab(2, left+10) // the active tab to the start
	g.Up()
	a.settle()
	if got := a.folders(); !slices.Equal(got, []string{f[2], f[0], f[1]}) {
		t.Errorf("the tabs are %q", got)
	}
	if a.activeIndex() != 0 || a.m.Active().Folder != f[2] || a.strip().Active() != 0 {
		t.Errorf("the active tab is %d (%q)", a.activeIndex(), a.m.Active().Folder)
	}
}

func TestEscapeCancelsTheDragOfAWorkspaceTab(t *testing.T) {
	a, f := tabs3(t, nil)
	active := a.m.ActiveTab()
	left := a.stripRect().Left
	g := a.dragTab(0, left+350)
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyEscape)
	a.settle()
	// the pointer goes on and is released: nothing moves, and the pressed tab did not become the active one
	g.MoveTo(ui.Offset{Dx: left + 20, Dy: g.Position().Dy})
	g.Up()
	a.settle()
	if got := a.folders(); !slices.Equal(got, f) {
		t.Errorf("the tabs are %q after Escape, want %q", got, f)
	}
	if a.m.ActiveTab() != active {
		t.Errorf("the active tab is %q after Escape", a.m.Active().Folder)
	}
	// a drag after the cancelled one works
	g = a.dragTab(0, left+350)
	g.Up()
	a.settle()
	if got := a.folders(); !slices.Equal(got, []string{f[1], f[2], f[0]}) {
		t.Errorf("the tabs are %q after a new drag", got)
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestATabIsDraggedPastASlopOf4PixelsAndClickedBelowIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		dx   float64
		drag bool
	}{{"3 px is a click", 3, false}, {"5 px is a drag", 5, true}} {
		t.Run(tc.name, func(t *testing.T) {
			a, f := tabs3(t, nil)
			span := a.tabSpan(0)
			from := ui.Offset{Dx: span.Left + 60, Dy: (span.Top + span.Bottom) / 2}
			g := a.ts.StartGesture(from, primaryDown)
			a.settle()
			if a.activeIndex() != 2 {
				t.Errorf("the press activated the tab at once: %d", a.activeIndex())
			}
			// the pointer moves by dx, and on over the second tab (still within the slop of the first move or not)
			g.MoveTo(ui.Offset{Dx: from.Dx + tc.dx, Dy: from.Dy})
			g.Up()
			a.settle()
			if tc.drag {
				// dropped on its own place: nothing moves and nothing is activated
				if a.activeIndex() != 2 || !slices.Equal(a.folders(), f) {
					t.Errorf("after a drag: active %d, tabs %q", a.activeIndex(), a.folders())
				}
			} else if a.activeIndex() != 0 {
				t.Errorf("after a click: active %d, want 0", a.activeIndex())
			}
		})
	}
}

func TestTheOrderOfTheTabsComesBackAfterARestart(t *testing.T) {
	a, f := tabs3(t, nil)
	left := a.stripRect().Left
	a.dragTab(0, left+350).Up() // the first tab to the end
	a.settle()
	if got := a.folders(); !slices.Equal(got, []string{f[1], f[2], f[0]}) {
		t.Fatalf("the tabs are %q", got)
	}
	a.env.clock.Advance(2 * storage.DefaultStateDelay)
	b := a.env.run(Options{})
	b.waitFor("the tabs", func() bool { return b.m.Count() == 3 })
	b.settle()
	if got := b.folders(); !slices.Equal(got, []string{f[1], f[2], f[0]}) {
		t.Errorf("the restored tabs are %q, want %q", got, []string{f[1], f[2], f[0]})
	}
	if b.m.Active().Folder != f[2] {
		t.Errorf("the active tab after the restart is %q", b.m.Active().Folder)
	}
	if errs := b.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestTheTrayFollowsTheOrderOfTheTabs(t *testing.T) {
	r := runTray(t, nil, "one", "two", "three")
	a := r.multiApp
	names := func() []string {
		a.settle()
		var out []string
		for _, l := range r.menu() {
			if l == "one" || l == "two" || l == "three" {
				out = append(out, l)
			}
		}
		return out
	}
	if got := names(); !slices.Equal(got, []string{"one", "two", "three"}) {
		t.Fatalf("the tray lists %q", got)
	}
	if err := a.command(a.m.Active(), "emetos.workspaceTab.moveLeft", nil); err != nil {
		t.Fatal(err)
	}
	if got := names(); !slices.Equal(got, []string{"one", "three", "two"}) {
		t.Errorf("after moveLeft the tray lists %q", got)
	}
	left := a.stripRect().Left
	a.dragTab(0, left+350).Up()
	if got := names(); !slices.Equal(got, []string{"three", "two", "one"}) {
		t.Errorf("after the drag the tray lists %q", got)
	}
}

func TestTheRowsOfTheWorkspaceTabsSubmenuCloseTheTabsAroundTheActiveOne(t *testing.T) {
	a, f := tabs3(t, nil)
	a.m.ActivateIndex(1)
	a.settle()
	run := func(title string) {
		t.Helper()
		w := a.m.Active()
		for _, it := range w.shell.menus.Items(command.MenubarWorkspaceTabsMenu, menuContext{base: w.Keys, settings: w.Settings}) {
			if kit.MenuLabelText(it.Label) == title {
				w.shell.menus.Run(it)
				return
			}
		}
		t.Fatalf("no row %q", title)
	}
	run("Close Tabs to the Right")
	a.waitFor("the tab to close", func() bool { return a.m.Count() == 2 })
	if got := a.folders(); !slices.Equal(got, []string{f[0], f[1]}) {
		t.Errorf("the tabs are %q after Close Tabs to the Right", got)
	}
	run("Close Other Tabs")
	a.waitFor("the other tab to close", func() bool { return a.m.Count() == 1 })
	if got := a.folders(); !slices.Equal(got, []string{f[1]}) {
		t.Errorf("the tabs are %q after Close Other Tabs", got)
	}
}
