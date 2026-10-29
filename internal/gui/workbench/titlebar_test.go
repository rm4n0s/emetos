package workbench

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// stripRig is a workbench whose title bar has workspace tabs, and the calls the user's actions made.
type stripRig struct {
	*harness
	bar   *TitleBarOptions
	calls []string
}

// threeTabs are a folder, a folder of the same name with unsaved changes and a tab without a folder.
func threeTabs() []WorkspaceTab {
	labels := LabelWorkspaceTabs([]string{"/home/me/work/api", "/home/me/home/api", ""})
	return []WorkspaceTab{
		{ID: "t1", Name: labels[0].Name, Description: labels[0].Description, Tooltip: "/home/me/work/api", Folder: true},
		{ID: "t2", Name: labels[1].Name, Description: labels[1].Description, Tooltip: "/home/me/home/api\nProfile: work", Folder: true, Dirty: true},
		{ID: "t3", Name: labels[2].Name, Tooltip: "No folder is open"},
	}
}

func newStripRig(t *testing.T, tabs []WorkspaceTab, active int, extra map[string]any, tune func(*Options)) *stripRig {
	t.Helper()
	r := &stripRig{}
	r.bar = &TitleBarOptions{
		Tabs: tabs, Active: active,
		OnActivate: func(i int) { r.calls = append(r.calls, fmt.Sprintf("activate %d", i)) },
		OnClose:    func(i int) { r.calls = append(r.calls, fmt.Sprintf("close %d", i)) },
		ContextItems: func(i int) []kit.MenuItem {
			r.calls = append(r.calls, fmt.Sprintf("menu of %d", i))
			return []kit.MenuItem{{ID: "emetos.workspaceTab.close", Label: "Close", Data: i}, kit.SeparatorItem(), {ID: "emetos.workspaceTab.copyPath", Label: "Copy Path", Data: i}}
		},
		NewItems: func() []kit.MenuItem {
			return []kit.MenuItem{{ID: "emetos.workspaceTab.openFolder", Label: "Workspace..."}}
		},
		OnRunItem: func(it kit.MenuItem) { r.calls = append(r.calls, fmt.Sprintf("run %s %v", it.ID, it.Data)) },
	}
	r.harness = newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), extra, func(o *Options) {
		o.TitleBar = r.bar
		o.Menus = []kit.MenuBarMenu{{Label: "&File", Items: []kit.MenuItem{{ID: "a", Label: "Alpha"}}}, {Label: "&Edit", Items: []kit.MenuItem{{ID: "b", Label: "Beta"}}}}
		if tune != nil {
			tune(o)
		}
	})
	return r
}

// window is a window the user can do everything with, not maximized (so that a double click maximizes it).
var floatingWindow = ui.WindowInfo{
	Decoration: ui.DecorationApp, Capabilities: ui.AllWindowCapabilities, Title: "Emetos", Activated: true,
}

func (r *stripRig) setWindow(info ui.WindowInfo) {
	r.ts.SetViewConfiguration(func(c *ui.ViewConfiguration) { c.Window = info })
	r.settle()
}

func (r *stripRig) mouse() gluttertest.GestureOpts {
	return gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse}
}

func TestStripShowsTheTabsWithTheirLabels(t *testing.T) {
	r := newStripRig(t, threeTabs(), 1, nil, nil)
	for _, text := range []string{"api — work", "api — home", "No Folder"} {
		if !r.ts.Exists(gluttertest.ByText(text)) {
			t.Errorf("the row shows no %q", text)
		}
	}
	// two tabs have the same name; the strip tells them apart by the description
	if n := len(r.ts.Widgets(gluttertest.ByTextContaining("api"))); n != 2 {
		t.Errorf("%d tabs are named api, want 2", n)
	}
	// icons: folders have root-folder, the other tab window
	if n := len(r.ts.Widgets(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconRootFolder)))); n != 2 {
		t.Errorf("%d root-folder icons, want 2", n)
	}
	if !r.ts.Exists(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconWindow))) {
		t.Error("the tab without a folder has no window icon")
	}
	// the dirty tab shows a dot, and the active tab the close button
	if !r.ts.Exists(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCircleFilled))) {
		t.Error("the dirty tab shows no dot")
	}
	noErrors(t, r.harness)
}

func TestStripCloseButtonOfTheActiveAndTheHoveredTab(t *testing.T) {
	r := newStripRig(t, threeTabs(), 0, nil, nil)
	closeIcon := gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconClose))
	visible := func() int {
		n := 0
		for _, el := range r.ts.Elements(closeIcon) {
			if o, ok := findOpacity(el); !ok || o > 0 {
				n++
			}
		}
		return n
	}
	if got := visible(); got != 1 {
		t.Errorf("%d close buttons show, want the active tab's only", got)
	}
	// the pointer over the dirty tab turns its dot into the close button
	r.ts.Hover(r.ts.Center(gluttertest.ByText("api — home")))
	r.settle()
	if r.ts.Exists(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCircleFilled))) {
		t.Error("the dot stays while the pointer is over the dirty tab")
	}
	if got := visible(); got != 2 {
		t.Errorf("%d close buttons show with the pointer over the dirty tab, want 2", got)
	}
	// and back to the dot when it leaves
	r.ts.Hover(ui.Offset{Dx: 900, Dy: 300})
	r.settle()
	if !r.ts.Exists(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCircleFilled))) {
		t.Error("the dot does not come back")
	}
	noErrors(t, r.harness)
}

// findOpacity reads the opacity of the Opacity widget above an element.
func findOpacity(el widgets.Element) (float64, bool) {
	var found float64
	ok := false
	el.VisitAncestorElements(func(a widgets.Element) bool {
		if op, is := a.Widget().(*widgets.Opacity); is {
			found, ok = op.Opacity(), true
			return false
		}
		return true
	})
	return found, ok
}

func TestStripActivatesClosesAndMiddleCloses(t *testing.T) {
	r := newStripRig(t, threeTabs(), 0, nil, nil)
	r.ts.TapAt(r.ts.Center(gluttertest.ByText("No Folder")), r.mouse())
	r.settle()
	if !reflect.DeepEqual(r.calls, []string{"activate 2"}) {
		t.Errorf("a click on a tab: %v", r.calls)
	}
	r.calls = nil
	// the close button of the active tab
	closeIcon := gluttertest.At(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconClose)), 0)
	r.ts.TapAt(r.ts.Center(closeIcon), r.mouse())
	r.settle()
	if !reflect.DeepEqual(r.calls, []string{"close 0"}) {
		t.Errorf("a click on the close button: %v", r.calls)
	}
	r.calls = nil
	// the middle button closes a tab without activating it
	mid := r.ts.Center(gluttertest.ByText("api — home"))
	g := r.ts.StartGesture(mid, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(4)})
	g.Up()
	r.settle()
	if !reflect.DeepEqual(r.calls, []string{"close 1"}) {
		t.Errorf("a middle click: %v", r.calls)
	}
	noErrors(t, r.harness)
}

func TestStripContextMenuOfATab(t *testing.T) {
	r := newStripRig(t, threeTabs(), 0, nil, func(o *Options) {})
	r.setWindow(floatingWindow)
	g := r.ts.StartGesture(r.ts.Center(gluttertest.ByText("api — home")), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(2)})
	g.Up()
	r.settle()
	if !r.ts.Exists(gluttertest.ByText("Close")) || !r.ts.Exists(gluttertest.ByText("Copy Path")) {
		t.Fatalf("the context menu is not shown; calls %v", r.calls)
	}
	if !slices.Contains(r.calls, "menu of 1") {
		t.Errorf("the menu is of another tab: %v", r.calls)
	}
	// the window menu must not open over the tabs
	for _, a := range r.ts.WindowActions() {
		if a.Kind == ui.WindowActionShowMenu {
			t.Errorf("a right click on a tab also asked for the window menu: %v", r.ts.WindowActions())
		}
	}
	r.ts.Tap(gluttertest.ByText("Copy Path"))
	r.settle()
	if got := r.calls[len(r.calls)-1]; got != "run emetos.workspaceTab.copyPath 1" {
		t.Errorf("the item ran as %q", got)
	}
	noErrors(t, r.harness)
}

func TestStripPlusOpensTheMenuOfNewTabs(t *testing.T) {
	r := newStripRig(t, threeTabs(), 0, nil, nil)
	plus := gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconAdd))
	r.ts.TapAt(r.ts.Center(plus), r.mouse())
	r.settle()
	if !r.ts.Exists(gluttertest.ByText("Workspace...")) {
		t.Fatal("the + button does not open the menu")
	}
	// the menu is under the button
	btn := r.ts.Center(plus)
	if y := r.ts.TopLeft(gluttertest.ByText("Workspace...")).Dy; y < btn.Dy {
		t.Errorf("the menu is at %v, above the button at %v", y, btn.Dy)
	}
	r.ts.Tap(gluttertest.ByText("Workspace..."))
	r.settle()
	if got := r.calls; len(got) == 0 || got[len(got)-1] != "run emetos.workspaceTab.openFolder <nil>" {
		t.Errorf("calls %v", got)
	}
	noErrors(t, r.harness)
}

func TestTheRowMovesMaximizesAndShowsTheWindowMenuOnTheEmptyPart(t *testing.T) {
	r := newStripRig(t, threeTabs(), 0, nil, nil)
	r.setWindow(floatingWindow)
	empty := ui.Offset{Dx: 700, Dy: 17}

	r.ts.DragFrom(empty, ui.Offset{Dx: 40, Dy: 10}, r.mouse())
	r.settle()
	if got := r.ts.WindowActions(); len(got) != 1 || got[0].Kind != ui.WindowActionMove {
		t.Errorf("a drag of the empty part: %v", got)
	}

	r.ts.TapAt(empty, r.mouse())
	r.ts.Pump(50 * 1e6)
	r.ts.TapAt(empty, r.mouse())
	r.settle()
	if got := lastKind(r.ts.WindowActions()); got != ui.WindowActionMaximize {
		t.Errorf("a double click on the empty part asked for %v", got)
	}

	g := r.ts.StartGesture(empty, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(2)})
	g.Up()
	r.settle()
	if got := lastKind(r.ts.WindowActions()); got != ui.WindowActionShowMenu {
		t.Errorf("a right click on the empty part asked for %v", got)
	}
	noErrors(t, r.harness)
}

func TestDraggingATabDoesNotMoveTheWindow(t *testing.T) {
	r := newStripRig(t, threeTabs(), 0, nil, nil)
	r.setWindow(floatingWindow)
	r.ts.DragFrom(r.ts.Center(gluttertest.ByText("No Folder")), ui.Offset{Dx: 60, Dy: 4}, r.mouse())
	r.settle()
	for _, a := range r.ts.WindowActions() {
		if a.Kind == ui.WindowActionMove {
			t.Errorf("a drag of a tab moves the window: %v", r.ts.WindowActions())
		}
	}
	// a double click on a tab does not maximize the window either
	at := r.ts.Center(gluttertest.ByText("No Folder"))
	r.ts.TapAt(at, r.mouse())
	r.ts.Pump(50 * 1e6)
	r.ts.TapAt(at, r.mouse())
	r.settle()
	if len(r.ts.WindowActions()) != 0 {
		t.Errorf("a double click on a tab asked for %v", r.ts.WindowActions())
	}
	noErrors(t, r.harness)
}

func lastKind(a []ui.WindowAction) ui.WindowActionKind {
	if len(a) == 0 {
		return ui.WindowActionKind(-1)
	}
	return a[len(a)-1].Kind
}

func TestTheWindowButtonsFollowTheCapabilities(t *testing.T) {
	r := newStripRig(t, threeTabs(), 0, nil, nil)
	r.ts.EnsureSemantics() // the buttons of the window are found by their labels
	r.settle()
	buttons := func() int {
		n := 0
		for _, label := range []string{"Minimize", "Maximize", "Restore", "Close"} {
			if r.ts.Exists(gluttertest.BySemanticsLabel(label)) {
				n++
			}
		}
		return n
	}
	if got := buttons(); got != 3 {
		t.Errorf("%d window buttons, want 3 (minimize, restore, close)", got)
	}
	info := floatingWindow
	info.Capabilities = ui.WindowCapabilities{}
	r.setWindow(info)
	if got := buttons(); got != 1 {
		t.Errorf("%d window buttons when the compositor allows none, want the close button only", got)
	}
	r.ts.TapAt(r.ts.Center(gluttertest.BySemanticsLabel("Close")), r.mouse())
	r.settle()
	if got := lastKind(r.ts.WindowActions()); got != ui.WindowActionClose {
		t.Errorf("the close button asked for %v", got)
	}
	noErrors(t, r.harness)
}

func TestStripShrinksTheTabsThenScrolls(t *testing.T) {
	var tabs []WorkspaceTab
	for i := range 12 {
		name := fmt.Sprintf("workspace-%02d", i)
		tabs = append(tabs, WorkspaceTab{ID: name, Name: name, Tooltip: "/w/" + name, Folder: true})
	}
	r := newStripRig(t, tabs, 0, nil, nil)
	// twelve tabs of 120 px do not fit in a window of 1000 px less the menu and the buttons
	last := r.ts.TopLeft(gluttertest.ByText("workspace-11"))
	if last.Dx < 1000 {
		t.Errorf("the last tab is at %v: all twelve fit, so they did not stay at 120", last.Dx)
	}
	// the wheel scrolls the strip
	before := r.ts.TopLeft(gluttertest.ByText("workspace-00")).Dx
	r.ts.ScrollKindAt(ui.PointerDeviceKindMouse, ui.Offset{Dx: 400, Dy: 17}, ui.Offset{Dx: 0, Dy: 300})
	r.settle()
	after := r.ts.TopLeft(gluttertest.ByText("workspace-00")).Dx
	if after >= before {
		t.Errorf("the wheel did not scroll the strip: %v before, %v after", before, after)
	}
	noErrors(t, r.harness)
}

func TestStripKeepsTheActiveTabInView(t *testing.T) {
	var tabs []WorkspaceTab
	for i := range 12 {
		name := fmt.Sprintf("workspace-%02d", i)
		tabs = append(tabs, WorkspaceTab{ID: name, Name: name, Tooltip: "/w/" + name, Folder: true})
	}
	r := newStripRig(t, tabs, 11, nil, nil)
	r.settle()
	at := r.ts.TopLeft(gluttertest.ByText("workspace-11"))
	if at.Dx < 0 || at.Dx > 1000 {
		t.Errorf("the active tab is at %v, out of the window", at.Dx)
	}
	noErrors(t, r.harness)
}

func TestTitleBarInZenModeKeepsOnlyTheWindowButtons(t *testing.T) {
	r := newStripRig(t, threeTabs(), 0, nil, nil)
	r.ts.EnsureSemantics()
	r.layout.EnterZen(false, false)
	r.settle()
	if r.ts.Exists(gluttertest.ByText("No Folder")) {
		t.Error("the tabs show in zen mode")
	}
	if r.ts.Exists(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconMenu))) {
		t.Error("the menu button shows in zen mode")
	}
	if !r.ts.Exists(gluttertest.BySemanticsLabel("Close")) {
		t.Error("the window has no close button in zen mode")
	}
	noErrors(t, r.harness)
}

func TestFullScreenHasNoTitleBarAndTheMenuWaitsForAlt(t *testing.T) {
	r := newStripRig(t, threeTabs(), 0, map[string]any{"window.menuBarVisibility": "classic"}, func(o *Options) { o.FullScreen = true })
	info := testutil.WindowInfo
	info.Fullscreen = true
	r.setWindow(info)
	r.ts.EnsureSemantics()
	r.settle()
	if r.ts.Exists(gluttertest.ByText("No Folder")) || r.ts.Exists(gluttertest.BySemanticsLabel("Close")) {
		t.Error("the title bar shows in full screen mode")
	}
	if r.ts.Exists(gluttertest.ByText("File")) {
		t.Error("the menu shows in full screen mode before the Alt key")
	}
	rectIs(t, "the activity bar", r.rect(PartActivityBar), 0, 0, 48, 618)
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyAltLeft)
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyAltLeft)
	r.settle()
	if !r.ts.Exists(gluttertest.ByText("File")) {
		t.Fatal("the Alt key does not show the menu")
	}
	rectIs(t, "the activity bar", r.rect(PartActivityBar), 0, 30, 48, 618)
	noErrors(t, r.harness)
}

func TestCompactMenuOpensWithTheMenusAsSubmenus(t *testing.T) {
	r := newStripRig(t, threeTabs(), 0, nil, nil)
	icon := gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconMenu))
	if !r.ts.Exists(icon) || r.ts.Exists(gluttertest.ByText("File")) {
		t.Fatal("the compact menu is not a button")
	}
	r.ts.TapAt(r.ts.Center(icon), r.mouse())
	r.settle()
	if !r.ts.Exists(gluttertest.ByText("File")) || !r.ts.Exists(gluttertest.ByText("Edit")) {
		t.Error("the menu does not list the menus")
	}
	noErrors(t, r.harness)
}

func codiconMenu() any { return fonts.CodiconIcon(fonts.CodiconMenu) }
func codiconAdd() any  { return fonts.CodiconIcon(fonts.CodiconAdd) }

func TestTheTabOfAnExtensionHasTheIconOfItsKind(t *testing.T) {
	note, ok := fonts.CodiconByName("note")
	if !ok {
		t.Fatal("no codicon note")
	}
	tabs := []WorkspaceTab{
		{ID: "t1", Name: "emetos", Folder: true}, {ID: "t2", Name: "No folder"}, {ID: "t3", Name: "Remote Work", IconCode: note, Folder: true},
	}
	r := newStripRig(t, tabs, 2, nil, nil)
	for _, c := range []struct {
		name string
		code int
		want bool
	}{{"the icon of the kind", note, true}, {"the icon of a folder", fonts.CodiconRootFolder, true}, {"the icon of a window", fonts.CodiconWindow, true}} {
		if got := r.ts.Exists(gluttertest.ByIcon(fonts.CodiconIcon(c.code))); got != c.want {
			t.Errorf("%s: shown %v, want %v", c.name, got, c.want)
		}
	}
	// the icon of the kind wins over the folder: the third tab has no folder icon, so there is one folder icon only
	if n := len(r.ts.Elements(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconRootFolder)))); n != 1 {
		t.Errorf("%d folder icons, want 1 (the tab of the extension has its own)", n)
	}
}

func TestTheControllerOpensTheNewMenuFromOutside(t *testing.T) {
	c := NewNewMenuController()
	if c.Open() {
		t.Error("a controller that is not attached opened a menu")
	}
	var nilController *NewMenuController
	if nilController.Open() {
		t.Error("a nil controller opened a menu")
	}
	r := newStripRig(t, threeTabs(), 0, nil, func(o *Options) { o.TitleBar.NewMenu = c })
	if r.ts.Exists(gluttertest.ByText("Workspace...")) {
		t.Fatal("the menu is open before it was asked for")
	}
	if !c.Open() {
		t.Fatal("the controller of a strip that is on screen did not open the menu")
	}
	r.settle()
	if !r.ts.Exists(gluttertest.ByText("Workspace...")) {
		t.Error("the menu is not on screen")
	}
}

func TestEditorOnlyShowsTheEditorAreaUnderTheTitleBar(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, func(o *Options) {
		demo(o)
		o.EditorOnly = true
		o.TitleBar = &TitleBarOptions{Tabs: threeTabs(), Active: 0}
	})
	for _, p := range []Part{PartActivityBar, PartSideBar, PartPanel, PartStatusBar} {
		if h.shown(p) {
			t.Errorf("the %v is on screen", p)
		}
	}
	if !h.shown(PartEditor) {
		t.Fatal("the editor area is not on screen")
	}
	if got, want := h.rect(PartEditor).Width(), testSize.Width; got != want {
		t.Errorf("the area is %v wide, want the whole window (%v)", got, want)
	}
	// the layout commands change the layout and show nothing
	h.run("workbench.action.toggleSidebarVisibility")
	h.run("workbench.action.togglePanel")
	h.run("workbench.action.toggleStatusbarVisibility")
	for _, p := range []Part{PartActivityBar, PartSideBar, PartPanel, PartStatusBar} {
		if h.shown(p) {
			t.Errorf("the %v is on screen after its command", p)
		}
	}
	// and the settings of the layout are not followed
	if err := h.store.Update(h.t.Context(), "workbench.statusBar.visible", true, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	h.settle()
	if h.shown(PartStatusBar) {
		t.Error("the status bar came back with the setting")
	}
	noErrors(t, h)
}
