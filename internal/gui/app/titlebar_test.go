package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The workspace tabs of the title bar row (step 9b.2, SPEC §10.14): the strip that the shell of the active tab gives the
// workbench, and the commands of its menus.

// editorTabs finds the strips of tabs of the editor groups (not the workspace tabs of the title bar).
func editorTabs() gluttertest.Finder {
	return gluttertest.ByPredicate("the tabs of an editor group", func(w widgets.Widget) bool {
		t, ok := w.(*kit.Tabs)
		return ok && t.TitleBar() == nil
	})
}

// inActiveTab says whether an element is in the page of the tab that is shown (every tab has a strip in its workbench;
// the ones of the tabs that are not shown are not painted and may be out of date).
func inActiveTab(e widgets.Element) bool {
	active := false
	e.VisitAncestorElements(func(a widgets.Element) bool {
		if slot, ok := a.Widget().(*tabSlot); ok {
			active = slot.active
			return false
		}
		return true
	})
	return active
}

// stripFinder finds the strip of workspace tabs that is shown.
func stripFinder() gluttertest.Finder {
	return gluttertest.ByElementPredicate("the strip of workspace tabs that is shown", func(e widgets.Element) bool {
		t, ok := e.Widget().(*kit.Tabs)
		return ok && t.TitleBar() != nil && inActiveTab(e)
	})
}

// strip is the strip of workspace tabs on screen.
func (a *multiApp) strip() *kit.Tabs {
	a.t.Helper()
	return a.ts.Widget(stripFinder()).(*kit.Tabs)
}

// stripNames are the labels of the tabs of the strip, "api — work" for a tab that has a description.
func (a *multiApp) stripNames() []string {
	a.t.Helper()
	var out []string
	for _, it := range a.strip().Items() {
		name := it.Name
		if it.Description != "" {
			name += " " + it.Description
		}
		out = append(out, name)
	}
	return out
}

// tabAt finds the label of the tab at a place of the strip.
func (a *multiApp) tabAt(i int) gluttertest.Finder {
	return gluttertest.Descendant(stripFinder(), gluttertest.ByText(a.stripNames()[i]))
}

// plusIcon finds the icon of the + button of the strip that is shown.
func plusIcon() gluttertest.Finder {
	icon := fonts.CodiconIcon(fonts.CodiconAdd)
	return gluttertest.ByElementPredicate("the + button that is shown", func(e widgets.Element) bool {
		w, ok := e.Widget().(gluttertest.IconWidget)
		return ok && w.IconValue() == icon && inActiveTab(e)
	})
}

// inStrip finds an icon in the strip that is shown.
func inStrip(icon int) gluttertest.Finder {
	return gluttertest.Descendant(stripFinder(), gluttertest.ByIcon(fonts.CodiconIcon(icon)))
}

func mouse() gluttertest.GestureOpts { return gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse} }

func TestTheStripShowsTheTabsOfTheProcessAndFollowsThem(t *testing.T) {
	a, f := tabs3(t, nil)
	if got := a.stripNames(); !slices.Equal(got, []string{"one", "two", "three"}) {
		t.Fatalf("the strip shows %q", got)
	}
	if got := a.strip().Active(); got != 2 {
		t.Errorf("the active tab is %d, want the last one, 2", got)
	}
	// every tab has its folder as its tooltip
	for i, it := range a.strip().Items() {
		if it.Title != f[i] {
			t.Errorf("tab %d has the tooltip %q, want %q", i, it.Title, f[i])
		}
	}
	a.m.ActivateIndex(0)
	a.settle()
	if got := a.strip().Active(); got != 0 {
		t.Errorf("after the first tab was activated the strip says %d", got)
	}
	// a tab that opens, moves and closes shows in the strip
	four := a.env.folder("four", "e.go")
	if _, err := a.m.Open(WorkspaceSpec{Folder: four}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if got := a.stripNames(); !slices.Equal(got, []string{"one", "four", "two", "three"}) {
		t.Errorf("a tab opens after the active one: %q", got)
	}
	a.m.Move(a.m.Tabs()[1], 3)
	a.settle()
	if got := a.stripNames(); !slices.Equal(got, []string{"one", "two", "three", "four"}) {
		t.Errorf("after a move: %q", got)
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestTabsOfTheSameNameGetTheirParentFolder(t *testing.T) {
	env := newMultiEnv(t, nil)
	work := env.folder("work/api", "a.go")
	home := env.folder("home/api", "b.go")
	a := env.run(Options{Folder: work})
	if _, err := a.m.Open(WorkspaceSpec{Folder: home}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if got := a.stripNames(); !slices.Equal(got, []string{"api — work", "api — home"}) {
		t.Errorf("the strip shows %q", got)
	}
	// the one that stays alone loses the description
	a.m.Close(a.m.Tabs()[1], nil)
	a.waitFor("the tab to go", func() bool { return a.m.Count() == 1 })
	a.settle()
	if got := a.stripNames(); !slices.Equal(got, []string{"api"}) {
		t.Errorf("after the other tab closed the strip shows %q", got)
	}
}

func TestANoFolderTabHasItsNameAndTheProfileIsInTheTooltip(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.NewTab(); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if got := a.stripNames(); !slices.Equal(got, []string{"one", "No Folder"}) {
		t.Fatalf("the strip shows %q", got)
	}
	if got := a.strip().Items()[1].Title; got != "No folder is open" {
		t.Errorf("the tooltip of the tab without a folder is %q", got)
	}
	if a.ts.Exists(inStrip(fonts.CodiconWindow)) == false {
		t.Error("the tab without a folder has no window icon")
	}
	// a workspace of another profile says so
	ctx := a.ctx
	if _, err := a.p.Profiles.Create(ctx, "Work", storage.CreateFrom(storage.DefaultProfile)); err != nil {
		t.Fatal(err)
	}
	if err := a.workspaceOf(one).SwitchProfile(ctx, "Work"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if got, want := a.strip().Items()[0].Title, one+"\nProfile: Work"; got != want {
		t.Errorf("the tooltip is %q, want %q", got, want)
	}
}

func TestTheDotOfATabFollowsTheDocumentsOfItsWorkspace(t *testing.T) {
	a, f := tabs3(t, nil)
	dirty := func() []bool {
		var out []bool
		for _, it := range a.strip().Items() {
			out = append(out, it.Dirty)
		}
		return out
	}
	if got := dirty(); !slices.Equal(got, []bool{false, false, false}) {
		t.Fatalf("dots %v", got)
	}
	// a change in a tab that is not shown shows in the strip of the one that is
	w1 := a.workspaceOf(f[0])
	a.edit(w1, filepath.Join(f[0], "a.go"))
	a.waitFor("the dot", func() bool { return slices.Equal(dirty(), []bool{true, false, false}) })
	// the same in the active tab
	w3 := a.workspaceOf(f[2])
	a.edit(w3, filepath.Join(f[2], "d.go"))
	a.waitFor("the second dot", func() bool { return slices.Equal(dirty(), []bool{true, false, true}) })
	// saving takes it away again
	if err := a.command(w1, "workbench.action.files.saveAll", nil); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the dot to go", func() bool { return slices.Equal(dirty(), []bool{false, false, true}) })
	if !a.ts.Exists(inStrip(fonts.CodiconCircleFilled)) {
		t.Error("the dirty tab shows no dot")
	}
}

func TestAClickActivatesATabAndTheCloseButtonAndTheMiddleButtonCloseIt(t *testing.T) {
	a, f := tabs3(t, nil)
	a.ts.TapAt(a.ts.Center(a.tabAt(0)), mouse())
	a.settle()
	if a.m.ActiveTab() != a.m.Tabs()[0] {
		t.Fatal("a click on the first tab did not activate it")
	}
	// the close button of the active tab
	a.ts.TapAt(a.ts.Center(gluttertest.At(inStrip(fonts.CodiconClose), 0)), mouse())
	a.waitFor("the tab to close", func() bool { return a.m.Count() == 2 })
	if got := a.folders(); !slices.Equal(got, []string{f[1], f[2]}) {
		t.Errorf("tabs %q after the close button", got)
	}
	// the middle button closes a tab that is not the active one
	a.settle()
	g := a.ts.StartGesture(a.ts.Center(a.tabAt(1)), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(4)})
	g.Up()
	a.waitFor("the tab to close", func() bool { return a.m.Count() == 1 })
	if got := a.folders(); len(got) != 1 || got[0] != f[1] {
		t.Errorf("tabs %q after the middle click", got)
	}
	// the last tab gives a No Folder tab
	g = a.ts.StartGesture(a.ts.Center(a.tabAt(0)), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(4)})
	g.Up()
	a.waitFor("the tab to be replaced", func() bool { return len(a.folders()) == 1 && a.folders()[0] == "" })
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestClosingATabWithUnsavedChangesAsksFirst(t *testing.T) {
	a, f := tabs3(t, nil)
	w1 := a.workspaceOf(f[0])
	a.edit(w1, filepath.Join(f[0], "a.go"))
	g := a.ts.StartGesture(a.ts.Center(a.tabAt(0)), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(4)})
	g.Up()
	a.waitFor("the question", func() bool { return a.dialog(w1) != "" })
	a.settle()
	a.answer(w1, "Cancel")
	if a.m.Count() != 3 {
		t.Errorf("%d tabs after Cancel, want 3", a.m.Count())
	}
}

// rightClick presses the secondary button on a tab and lets it go.
func (a *multiApp) rightClick(i int) {
	a.t.Helper()
	g := a.ts.StartGesture(a.ts.Center(a.tabAt(i)), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(2)})
	g.Up()
	a.settle()
}

// menuTitles are the titles of the items of the menu that is open.
func (a *multiApp) menuTitles() []string {
	a.t.Helper()
	var out []string
	els := a.ts.Elements(gluttertest.ByType[*kit.Menu]())
	if len(els) == 0 {
		return nil
	}
	for _, it := range els[0].Widget().(*kit.Menu).Items() {
		switch {
		case it.Separator:
			out = append(out, "-")
		case it.Disabled:
			out = append(out, it.Label+"~")
		default:
			out = append(out, it.Label)
		}
	}
	return out
}

func TestTheContextMenuOfAWorkspaceTab(t *testing.T) {
	a, _ := tabs3(t, nil)
	a.rightClick(1)
	want := []string{"Close", "Close Others", "Close to the Right", "-", "Copy Path", "Reveal in File Manager", "-", "Switch Profile..."}
	if got := a.menuTitles(); !slices.Equal(got, want) {
		t.Fatalf("the menu has %q, want %q", got, want)
	}
	// the last tab has nothing to its right
	a.ts.SendKeyDownEvent(services.LogicalKeyboardKeyEscape)
	a.ts.SendKeyUpEvent(services.LogicalKeyboardKeyEscape)
	a.settle()
	a.rightClick(2)
	if got := a.menuTitles(); len(got) < 3 || got[2] != "Close to the Right~" {
		t.Errorf("the menu of the last tab: %q", got)
	}
	a.ts.SendKeyDownEvent(services.LogicalKeyboardKeyEscape)
	a.ts.SendKeyUpEvent(services.LogicalKeyboardKeyEscape)
	a.settle()
	// a tab without a folder cannot give its path
	if _, err := a.m.NewTab(); err != nil {
		t.Fatal(err)
	}
	a.settle()
	a.rightClick(3)
	if got := a.menuTitles(); len(got) < 6 || got[4] != "Copy Path~" || got[5] != "Reveal in File Manager~" {
		t.Errorf("the menu of a tab without a folder: %q", got)
	}
}

func TestTheCommandsOfTheContextMenuActOnTheTabThatWasClicked(t *testing.T) {
	a, f := tabs3(t, nil)
	// Copy Path puts the folder of that tab, not the active one, on the clipboard
	a.rightClick(0)
	a.ts.Tap(gluttertest.ByText("Copy Path"))
	a.settle()
	var got string
	a.ts.Binding.Clipboard().GetData(a.ctx, services.ClipboardKTextPlain, func(d *services.ClipboardData, err error) {
		if err == nil && d != nil {
			got = d.Text
		}
	})
	a.settle()
	if got != f[0] {
		t.Errorf("the clipboard has %q, want %q", got, f[0])
	}
	// Reveal in File Manager opens the folder of that tab through the portal
	var mu sync.Mutex
	var opened []string
	a.workspaceOf(f[2]).OpenURI = func(_ context.Context, _ widgets.BuildContext, uri string) error {
		mu.Lock()
		defer mu.Unlock()
		opened = append(opened, uri)
		return nil
	}
	a.rightClick(1)
	a.ts.Tap(gluttertest.ByText("Reveal in File Manager"))
	a.waitFor("the portal", func() bool { mu.Lock(); defer mu.Unlock(); return len(opened) == 1 })
	if want := "file://" + f[1]; opened[0] != want {
		t.Errorf("opened %q, want %q", opened[0], want)
	}
	// Close Others keeps the tab that was clicked
	a.rightClick(1)
	a.ts.Tap(gluttertest.ByText("Close Others"))
	a.waitFor("the others to close", func() bool { return a.m.Count() == 1 })
	if got := a.folders(); !slices.Equal(got, []string{f[1]}) {
		t.Errorf("tabs %q after Close Others", got)
	}
}

func TestCloseToTheRightAndCloseWithTheCommands(t *testing.T) {
	a, f := tabs3(t, nil)
	a.rightClick(0)
	a.ts.Tap(gluttertest.ByText("Close to the Right"))
	a.waitFor("the tabs to close", func() bool { return a.m.Count() == 1 })
	if got := a.folders(); !slices.Equal(got, []string{f[0]}) {
		t.Errorf("tabs %q after Close to the Right", got)
	}
	// the commands without an argument act on the active tab
	w := a.m.Active()
	if _, err := a.m.Open(WorkspaceSpec{Folder: f[1]}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if err := a.command(a.m.Active(), "emetos.workspaceTab.close", nil); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the tab to close", func() bool { return a.m.Count() == 1 })
	if a.m.Active() != w {
		t.Error("the tab before the closed one is not the active one")
	}
}

func TestClosingOthersStopsAtTheFirstTabTheUserKeeps(t *testing.T) {
	a, f := tabs3(t, nil)
	w3 := a.workspaceOf(f[2])
	a.edit(w3, filepath.Join(f[2], "d.go"))
	// the others of the first tab are the second (clean: it closes) and the third (it asks)
	if err := a.command(a.m.Active(), "emetos.workspaceTab.closeOthers", map[string]any{"tabId": a.m.Tabs()[0].ID()}); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the question about the third tab", func() bool { return a.dialog(w3) != "" })
	if got := a.folders(); !slices.Equal(got, []string{f[0], f[2]}) {
		t.Errorf("tabs %q while the third asks", got)
	}
	a.answer(w3, "Cancel")
	if got := a.folders(); !slices.Equal(got, []string{f[0], f[2]}) {
		t.Errorf("tabs %q after Cancel", got)
	}
}

func TestThePlusButtonOpensAFolderAsANewTab(t *testing.T) {
	a, f := tabs3(t, nil)
	fd := a.ts.FakeDesktop()
	four := a.env.folder("four", "e.go")
	a.ts.TapAt(a.ts.Center(plusIcon()), mouse())
	a.settle()
	if got := a.menuTitles(); !slices.Equal(got, []string{"Workspace..."}) {
		t.Fatalf("the menu of the + button has %q", got)
	}
	a.ts.Tap(gluttertest.ByText("Workspace..."))
	a.waitFor("the folder dialog", func() bool { return fd.PendingFileChoosers() == 1 })
	fd.AnswerFileChooser(four)
	a.waitFor("the new tab", func() bool { return a.m.Count() == 4 })
	a.settle() // the strip of the tab that is shown now is built
	if got := a.folders(); !slices.Equal(got, []string{f[0], f[1], f[2], four}) {
		t.Errorf("tabs %q", got)
	}
	if a.m.Active().Folder != four {
		t.Errorf("the new tab is not the active one: %q", a.m.Active().Folder)
	}
	// a folder that has a tab already makes that tab the active one
	a.ts.TapAt(a.ts.Center(plusIcon()), mouse())
	a.settle()
	a.ts.Tap(gluttertest.ByText("Workspace..."))
	a.waitFor("the folder dialog", func() bool { return fd.PendingFileChoosers() == 1 })
	fd.AnswerFileChooser(f[0])
	a.waitFor("the first tab to be active", func() bool { return a.m.Active() != nil && a.m.Active().Folder == f[0] })
	a.settle()
	if a.m.Count() != 4 {
		t.Errorf("%d tabs, want 4", a.m.Count())
	}
	// a dialog that is cancelled changes nothing
	a.ts.TapAt(a.ts.Center(plusIcon()), mouse())
	a.settle()
	a.ts.Tap(gluttertest.ByText("Workspace..."))
	a.waitFor("the folder dialog", func() bool { return fd.PendingFileChoosers() == 1 })
	fd.CancelFileChooser()
	a.settle()
	if a.m.Count() != 4 {
		t.Errorf("%d tabs after a cancelled dialog, want 4", a.m.Count())
	}
}

func TestThePlusButtonWithoutAFileChooserReportsIt(t *testing.T) {
	a, _ := tabs3(t, nil)
	a.ts.FakeDesktop().DisableFileChooser()
	a.ts.TapAt(a.ts.Center(plusIcon()), mouse())
	a.settle()
	a.ts.Tap(gluttertest.ByText("Workspace..."))
	w := a.m.Active()
	a.waitFor("the error", func() bool {
		for _, it := range w.Notify.Items() {
			if strings.Contains(it.Message, "dialog to open a folder") {
				return true
			}
		}
		return false
	})
	if a.m.Count() != 3 {
		t.Errorf("%d tabs, want 3", a.m.Count())
	}
}

func TestTheRowSendsTheActionsOfTheWindowAndShowsTheButtons(t *testing.T) {
	a, _ := tabs3(t, nil)
	info := ui.WindowInfo{Decoration: ui.DecorationApp, Capabilities: ui.AllWindowCapabilities, Title: "Emetos", Activated: true}
	a.ts.SetViewConfiguration(func(c *ui.ViewConfiguration) { c.Window = info })
	a.ts.EnsureSemantics()
	a.settle()
	empty := ui.Offset{Dx: 700, Dy: 17}
	a.ts.DragFrom(empty, ui.Offset{Dx: 30, Dy: 5}, mouse())
	a.ts.TapAt(a.ts.Center(gluttertest.BySemanticsLabel("Maximize")), mouse())
	a.ts.TapAt(a.ts.Center(gluttertest.BySemanticsLabel("Minimize")), mouse())
	a.settle()
	var kinds []ui.WindowActionKind
	for _, act := range a.ts.WindowActions() {
		kinds = append(kinds, act.Kind)
	}
	want := []ui.WindowActionKind{ui.WindowActionMove, ui.WindowActionMaximize, ui.WindowActionMinimize}
	if !slices.Equal(kinds, want) {
		t.Errorf("the window was asked %v, want %v", kinds, want)
	}
}

func TestTheTabCommandsAreRegisteredAndFailuresHaveTags(t *testing.T) {
	a, _ := tabs3(t, nil)
	w := a.m.Active()
	for _, id := range []string{
		"emetos.workspaceTab.close", "emetos.workspaceTab.closeOthers", "emetos.workspaceTab.closeToTheRight",
		"emetos.workspaceTab.copyPath", "emetos.workspaceTab.revealInFileManager", "emetos.workspaceTab.openFolder",
	} {
		meta, ok := w.Commands.Lookup(id)
		if !ok || meta.Title == "" || meta.Category != "Workspace" {
			t.Errorf("%s: %+v, %v", id, meta, ok)
		}
	}
	// a tab id that is not a tab does nothing
	if err := a.command(w, "emetos.workspaceTab.close", map[string]any{"tabId": "ffffffffffffffff"}); err != nil {
		t.Errorf("an unknown tab: %v", err)
	}
	if a.m.Count() != 3 {
		t.Errorf("%d tabs after the close of an unknown tab", a.m.Count())
	}
}

func TestGoldenWorkspaceTabs(t *testing.T) {
	for _, th := range []struct{ name, theme string }{{"dark", "Dark Modern"}, {"light", "Light Modern"}} {
		t.Run(th.name, func(t *testing.T) {
			env := newMultiEnv(t, map[string]any{
				"window.menuBarVisibility": "compact", "workbench.colorTheme": th.theme,
				"editor.minimap.enabled": false, "breadcrumbs.enabled": false, "editor.fontFamily": testutil.MonoFamily,
			})
			work := env.folder("work/api", "main.go", "internal/server.go")
			home := env.folder("home/api", "main.go")
			notes := env.folder("notes", "main.go", "todo.txt")
			a := env.run(Options{Folder: work})
			for _, f := range []string{home, notes} {
				if _, err := a.m.Open(WorkspaceSpec{Folder: f}); err != nil {
					t.Fatal(err)
				}
				a.settle()
			}
			a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
			a.ts.SetViewSize(ui.Size{Width: 900, Height: 420})
			w := a.workspaceOf(home)
			a.edit(w, filepath.Join(home, "main.go"))
			a.m.Activate(w.Tab())
			a.settle()
			testutil.SettleTokens(a.ts)
			testutil.MatchesGolden(a.ts, "workspace_tabs_"+th.name)
			if errs := a.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestTheCompactMenuOfTheWindowRunsTheRowsOfTheMenus(t *testing.T) {
	env := newMultiEnv(t, map[string]any{"window.menuBarVisibility": "compact"})
	one := env.folder("one", "a.go")
	a := env.run(Options{Folder: one})
	menu := gluttertest.ByElementPredicate("the menu button that is shown", func(e widgets.Element) bool {
		w, ok := e.Widget().(gluttertest.IconWidget)
		return ok && w.IconValue() == fonts.CodiconIcon(fonts.CodiconMenu) && inActiveTab(e)
	})
	if a.ts.Exists(gluttertest.ByText("File")) {
		t.Fatal("the titles show with the compact menu")
	}
	a.ts.TapAt(a.ts.Center(menu), mouse())
	a.settle()
	if !a.ts.Exists(gluttertest.ByText("File")) || !a.ts.Exists(gluttertest.ByText("Go")) {
		t.Fatal("the compact menu does not list the menus")
	}
	a.ts.TapAt(a.ts.Center(gluttertest.ByText("File")), mouse())
	a.settle()
	a.ts.Tap(gluttertest.ByText("New Workspace Tab"))
	a.waitFor("the new tab", func() bool { return a.m.Count() == 2 })
	a.settle()
	if got := a.stripNames(); !slices.Equal(got, []string{"one", "No Folder"}) {
		t.Errorf("the strip shows %q", got)
	}
}

func TestEachMenuBarVisibilityInTheWindow(t *testing.T) {
	for _, tc := range []struct {
		value      string
		titles     bool
		menuButton bool
	}{{"compact", false, true}, {"classic", true, false}, {"visible", true, false}, {"toggle", false, false}, {"hidden", false, false}} {
		t.Run(tc.value, func(t *testing.T) {
			env := newMultiEnv(t, map[string]any{"window.menuBarVisibility": tc.value})
			a := env.run(Options{Folder: env.folder("one", "a.go")})
			if got := a.ts.Exists(gluttertest.ByText("File")); got != tc.titles {
				t.Errorf("the titles show = %v, want %v", got, tc.titles)
			}
			menuIcon := gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconMenu))
			if got := a.ts.Exists(menuIcon); got != tc.menuButton {
				t.Errorf("the menu button shows = %v, want %v", got, tc.menuButton)
			}
		})
	}
}
