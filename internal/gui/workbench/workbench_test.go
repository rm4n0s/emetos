package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

var testSize = ui.Size{Width: 1000, Height: 640}

func rectIs(t *testing.T, name string, got ui.Rect, left, top, right, bottom float64) {
	t.Helper()
	if got.Left != left || got.Top != top || got.Right != right || got.Bottom != bottom {
		t.Errorf("%s is at %v, want (%v, %v, %v, %v)", name, got, left, top, right, bottom)
	}
}

func noErrors(t *testing.T, h *harness) {
	t.Helper()
	if errs := h.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

func TestPartsAreLaidOutAsVSCodeDoes(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
	rectIs(t, "the menu bar", h.rect(PartMenuBar), 0, 0, 1000, 30)
	rectIs(t, "the activity bar", h.rect(PartActivityBar), 0, 30, 48, 618)
	rectIs(t, "the side bar", h.rect(PartSideBar), 48, 30, 348, 618)
	rectIs(t, "the editor area", h.rect(PartEditor), 348, 30, 1000, 618)
	rectIs(t, "the status bar", h.rect(PartStatusBar), 0, 618, 1000, 640)
	if h.shown(PartPanel) {
		t.Error("the panel is hidden until it is asked for")
	}
	noErrors(t, h)
}

func TestWithEveryPartShown(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, nil)
	rectIs(t, "the editor area", h.rect(PartEditor), 348, 30, 1000, 318)
	rectIs(t, "the panel", h.rect(PartPanel), 348, 318, 1000, 618)
	noErrors(t, h)
}

func TestToggleCommandsShowAndHideTheirParts(t *testing.T) {
	cases := []struct {
		command string
		part    Part
		shown   bool // before
	}{
		{"workbench.action.toggleSidebarVisibility", PartSideBar, true},
		{"workbench.action.togglePanel", PartPanel, false},
		{"workbench.action.toggleActivityBarVisibility", PartActivityBar, true},
		{"workbench.action.toggleStatusbarVisibility", PartStatusBar, true},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
			if h.shown(tc.part) != tc.shown {
				t.Fatalf("before: shown = %v", h.shown(tc.part))
			}
			h.run(tc.command)
			if h.shown(tc.part) == tc.shown {
				t.Errorf("after one run the part is still shown = %v", h.shown(tc.part))
			}
			h.run(tc.command)
			if h.shown(tc.part) != tc.shown {
				t.Errorf("after two runs shown = %v", h.shown(tc.part))
			}
			noErrors(t, h)
		})
	}
}

func TestTheEditorAreaTakesThePlaceOfAHiddenPart(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
	h.run("workbench.action.toggleSidebarVisibility")
	rectIs(t, "the editor area without the side bar", h.rect(PartEditor), 48, 30, 1000, 618)
	h.run("workbench.action.toggleActivityBarVisibility")
	rectIs(t, "the editor area without the activity bar", h.rect(PartEditor), 0, 30, 1000, 618)
	h.run("workbench.action.toggleStatusbarVisibility")
	rectIs(t, "the editor area without the status bar", h.rect(PartEditor), 0, 30, 1000, 640)
	// and the side bar comes back with the width it had
	h.run("workbench.action.toggleSidebarVisibility")
	rectIs(t, "the side bar again", h.rect(PartSideBar), 0, 30, 300, 640)
	noErrors(t, h)
}

func TestCommandsThatAreSettingsWriteThem(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
	h.run("workbench.action.toggleStatusbarVisibility")
	if settings.Get[bool](h.store, "workbench.statusBar.visible", "") {
		t.Error("workbench.statusBar.visible is still true")
	}
	h.run("workbench.action.toggleActivityBarVisibility")
	if got := settings.Get[string](h.store, "workbench.activityBar.location", ""); got != "hidden" {
		t.Errorf("workbench.activityBar.location = %q", got)
	}
	h.run("workbench.action.toggleActivityBarVisibility")
	if got := settings.Get[string](h.store, "workbench.activityBar.location", ""); got != "default" {
		t.Errorf("workbench.activityBar.location = %q", got)
	}
	// and a change of the setting alone changes the layout
	if err := h.store.Update(t.Context(), "workbench.statusBar.visible", true, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	h.settle()
	if !h.shown(PartStatusBar) {
		t.Error("the status bar did not come back with its setting")
	}
	if err := h.store.Update(t.Context(), "workbench.sideBar.location", "right", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	h.settle()
	rectIs(t, "the side bar on the right", h.rect(PartSideBar), 652, 30, 952, 618)
	noErrors(t, h)
}

func TestSideBarOnTheRight(t *testing.T) {
	st := DefaultLayoutState()
	st.SideBarPosition = SideBarRight
	h := newHarness(t, testSize, kit.LightModern(), st, nil, nil)
	rectIs(t, "the editor area", h.rect(PartEditor), 0, 30, 652, 618)
	rectIs(t, "the side bar", h.rect(PartSideBar), 652, 30, 952, 618)
	rectIs(t, "the activity bar", h.rect(PartActivityBar), 952, 30, 1000, 618)
	h.run("workbench.action.toggleSidebarVisibility")
	rectIs(t, "the editor area without the side bar", h.rect(PartEditor), 0, 30, 952, 618)
	noErrors(t, h)
}

func TestPanelPositions(t *testing.T) {
	cases := []struct {
		command  string
		position string
		editor   [4]float64
		panel    [4]float64
	}{
		{"workbench.action.positionPanelBottom", "bottom", [4]float64{348, 30, 1000, 318}, [4]float64{348, 318, 1000, 618}},
		{"workbench.action.positionPanelTop", "top", [4]float64{348, 330, 1000, 618}, [4]float64{348, 30, 1000, 330}},
		{"workbench.action.positionPanelLeft", "left", [4]float64{648, 30, 1000, 618}, [4]float64{348, 30, 648, 618}},
		{"workbench.action.positionPanelRight", "right", [4]float64{348, 30, 700, 618}, [4]float64{700, 30, 1000, 618}},
	}
	for _, tc := range cases {
		t.Run(tc.position, func(t *testing.T) {
			h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
			// a position other than the first one is reached from the bottom, and the bottom from the right
			if tc.position == "bottom" {
				h.run("workbench.action.positionPanelRight")
			}
			h.run(tc.command)
			e, p := tc.editor, tc.panel
			rectIs(t, "the editor area", h.rect(PartEditor), e[0], e[1], e[2], e[3])
			rectIs(t, "the panel", h.rect(PartPanel), p[0], p[1], p[2], p[3])
			if got := h.key("panelPosition"); got != tc.position {
				t.Errorf("panelPosition = %v", got)
			}
			if got := h.key("panelVisible"); got != true {
				t.Errorf("panelVisible = %v: moving the panel shows it", got)
			}
			noErrors(t, h)
		})
	}
}

func TestMovingThePanelKeepsTheEditorAreaAlive(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, nil)
	h.run("workbench.action.focusActiveEditorGroup")
	if !h.editor.HasFocus() {
		t.Fatal("the editor does not have the focus")
	}
	before := h.ts.Element(gluttertest.ByKey(PartKey(PartEditor)))
	for _, id := range []string{
		"workbench.action.positionPanelRight", "workbench.action.positionPanelLeft",
		"workbench.action.positionPanelTop", "workbench.action.positionPanelBottom",
	} {
		h.run(id)
		if !h.editor.HasFocus() {
			t.Errorf("%s: the editor lost the focus", id)
		}
		if h.ts.Element(gluttertest.ByKey(PartKey(PartEditor))) != before {
			t.Errorf("%s: the editor area was made again", id)
		}
	}
	// and when the side bar moves to the other side
	if err := h.store.Update(t.Context(), "workbench.sideBar.location", "right", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	h.settle()
	if !h.editor.HasFocus() || h.ts.Element(gluttertest.ByKey(PartKey(PartEditor))) != before {
		t.Error("the editor area was made again when the side bar moved")
	}
	noErrors(t, h)
}

func TestMaximizeAndRestoreThePanel(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, nil)
	rectIs(t, "the panel", h.rect(PartPanel), 348, 318, 1000, 618)

	h.run("workbench.action.toggleMaximizedPanel")
	if h.shown(PartEditor) {
		t.Error("the editor area is shown while the panel is maximized")
	}
	rectIs(t, "the maximized panel", h.rect(PartPanel), 348, 30, 1000, 618)
	if h.key("panelMaximized") != true || h.key("mainEditorAreaVisible") != false {
		t.Errorf("keys: panelMaximized %v, mainEditorAreaVisible %v", h.key("panelMaximized"), h.key("mainEditorAreaVisible"))
	}

	h.run("workbench.action.toggleMaximizedPanel")
	rectIs(t, "the editor area restored", h.rect(PartEditor), 348, 30, 1000, 318)
	rectIs(t, "the panel restored", h.rect(PartPanel), 348, 318, 1000, 618)
	if h.key("panelMaximized") != false || h.key("mainEditorAreaVisible") != true {
		t.Errorf("keys: panelMaximized %v, mainEditorAreaVisible %v", h.key("panelMaximized"), h.key("mainEditorAreaVisible"))
	}
	noErrors(t, h)
}

func TestTheRestoredPanelHasTheSizeItHadEvenAfterTheWindowChanged(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible, st.PanelHeight = true, 200
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, nil)
	h.run("workbench.action.toggleMaximizedPanel")
	h.ts.SetViewSize(ui.Size{Width: 1000, Height: 500})
	h.settle()
	h.run("workbench.action.toggleMaximizedPanel")
	if got := h.rect(PartPanel).Height(); got != 200 {
		t.Errorf("the restored panel is %v high, want 200", got)
	}
	if got := h.layout.State().PanelHeight; got != 200 {
		t.Errorf("the panel height of the layout is %v", got)
	}
	noErrors(t, h)
}

func TestMaximizingAHiddenPanelShowsIt(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
	h.run("workbench.action.toggleMaximizedPanel")
	rectIs(t, "the panel", h.rect(PartPanel), 348, 30, 1000, 618)
	h.run("workbench.action.togglePanel")
	rectIs(t, "the editor area with the panel hidden", h.rect(PartEditor), 348, 30, 1000, 618)
	h.run("workbench.action.togglePanel")
	if h.shown(PartEditor) {
		t.Error("the panel came back maximized (workbench.panel.opensMaximized preserve)")
	}
	noErrors(t, h)
}

func TestMaximizeIsEnabledAtEveryPositionOfACenteredPanel(t *testing.T) {
	// the precondition of the command is VS Code's: the alignment is centered, so every position can maximize
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
	for _, pos := range []string{"workbench.action.positionPanelLeft", "workbench.action.positionPanelBottom", "workbench.action.positionPanelTop"} {
		h.run(pos)
		if !h.commands.Enabled("workbench.action.toggleMaximizedPanel", h.keys) {
			t.Errorf("%s: toggleMaximizedPanel is not enabled", pos)
		}
	}
}

func TestSashesResizeTheSideBarAndThePanel(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, nil)
	sashes := gluttertest.ByType[*kit.Sash]()
	count := len(h.ts.Elements(sashes))
	if count < 3 {
		t.Fatalf("%d sashes", count)
	}
	// the side bar's sash is the second of the main row (the first is the activity bar's)
	at := ui.Offset{Dx: 348, Dy: 300}
	h.ts.DragFrom(at, ui.Offset{Dx: 60})
	h.settle()
	rectIs(t, "the side bar", h.rect(PartSideBar), 48, 30, 408, 618)
	if got := h.layout.State().SideBarWidth; got != 360 {
		t.Errorf("SideBarWidth = %v, want 360", got)
	}
	// the panel's
	h.ts.DragFrom(ui.Offset{Dx: 700, Dy: 318}, ui.Offset{Dy: -100})
	h.settle()
	rectIs(t, "the panel", h.rect(PartPanel), 408, 218, 1000, 618)
	if got := h.layout.State().PanelHeight; got != 400 {
		t.Errorf("PanelHeight = %v, want 400", got)
	}
	noErrors(t, h)
}

func TestTheSideBarCannotBeNarrowerThanItsLeast(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
	h.ts.DragFrom(ui.Offset{Dx: 348, Dy: 300}, ui.Offset{Dx: -250})
	h.settle()
	if got := h.rect(PartSideBar).Width(); got != 170 {
		t.Errorf("the side bar is %v wide, want 170", got)
	}
	noErrors(t, h)
}

func TestTheEditorAreaTakesTheResizeOfTheWindow(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, nil)
	h.ts.SetViewSize(ui.Size{Width: 1300, Height: 800})
	h.settle()
	rectIs(t, "the side bar", h.rect(PartSideBar), 48, 30, 348, 778)
	rectIs(t, "the editor area", h.rect(PartEditor), 348, 30, 1300, 478)
	rectIs(t, "the panel", h.rect(PartPanel), 348, 478, 1300, 778)
	h.ts.SetViewSize(ui.Size{Width: 700, Height: 500})
	h.settle()
	rectIs(t, "the side bar when smaller", h.rect(PartSideBar), 48, 30, 348, 478)
	rectIs(t, "the editor area when smaller", h.rect(PartEditor), 348, 30, 700, 178)
	rectIs(t, "the panel when smaller", h.rect(PartPanel), 348, 178, 700, 478)
	noErrors(t, h)
}

func TestFocusCommandsMoveTheFocusAndSetTheContextKeys(t *testing.T) {
	cases := []struct {
		command string
		part    Part
		key     string
	}{
		{"workbench.action.focusSideBar", PartSideBar, "sideBarFocus"},
		{"workbench.action.focusPanel", PartPanel, "panelFocus"},
		{"workbench.action.focusStatusBar", PartStatusBar, "statusBarFocused"},
		{"workbench.action.focusActivityBar", PartActivityBar, ""},
		{"workbench.action.focusActiveEditorGroup", PartEditor, "editorAreaFocus"},
	}
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
	h.run("workbench.action.focusActiveEditorGroup")
	if h.focused() != PartEditor || h.key("editorAreaFocus") != true {
		t.Fatalf("the editor area does not start with the focus: %v %v", h.focused(), h.key("editorAreaFocus"))
	}
	allKeys := []string{"sideBarFocus", "panelFocus", "statusBarFocused", "editorAreaFocus"}
	for _, tc := range cases {
		h.run(tc.command)
		if got := h.focused(); got != tc.part {
			t.Errorf("%s: the focus is in %v, want %v", tc.command, got, tc.part)
		}
		for _, k := range allKeys {
			if got, want := h.key(k) == true, k == tc.key; got != want {
				t.Errorf("%s: %s = %v, want %v", tc.command, k, got, want)
			}
		}
	}
	noErrors(t, h)
}

func TestFocusCommandsShowTheirParts(t *testing.T) {
	st := DefaultLayoutState()
	st.SideBarVisible = false
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, nil)
	h.run("workbench.action.focusSideBar")
	if !h.shown(PartSideBar) || h.focused() != PartSideBar {
		t.Errorf("focusSideBar: shown %v, focused %v", h.shown(PartSideBar), h.focused())
	}
	h.run("workbench.action.focusPanel")
	if !h.shown(PartPanel) || h.focused() != PartPanel {
		t.Errorf("focusPanel: shown %v, focused %v", h.shown(PartPanel), h.focused())
	}
	noErrors(t, h)
}

func TestFocusCommandsOfHiddenPartsDoNothing(t *testing.T) {
	st := DefaultLayoutState()
	st.StatusBarVisible, st.ActivityBarVisible = false, false
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, nil)
	h.run("workbench.action.focusActiveEditorGroup")
	h.run("workbench.action.focusStatusBar")
	h.run("workbench.action.focusActivityBar")
	if h.focused() != PartEditor {
		t.Errorf("the focus moved to %v", h.focused())
	}
	noErrors(t, h)
}

func TestHidingThePartThatHasTheFocusFocusesTheEditor(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, nil)
	for _, tc := range []struct{ focus, hide string }{
		{"workbench.action.focusSideBar", "workbench.action.toggleSidebarVisibility"},
		{"workbench.action.focusPanel", "workbench.action.togglePanel"},
		{"workbench.action.focusActivityBar", "workbench.action.toggleActivityBarVisibility"},
		{"workbench.action.focusStatusBar", "workbench.action.toggleStatusbarVisibility"},
	} {
		h.run(tc.focus)
		h.run(tc.hide)
		if got := h.focused(); got != PartEditor {
			t.Errorf("%s: the focus is in %v, want the editor area", tc.hide, got)
		}
		if !h.editor.HasFocus() {
			t.Errorf("%s: the editor does not have the focus", tc.hide)
		}
	}
	noErrors(t, h)
}

func TestMaximizingThePanelWhileTheEditorHasTheFocusFocusesThePanel(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, nil)
	h.run("workbench.action.focusActiveEditorGroup")
	h.run("workbench.action.toggleMaximizedPanel")
	if got := h.focused(); got != PartPanel {
		t.Errorf("the focus is in %v, want the panel", got)
	}
	noErrors(t, h)
}

func TestContextKeys(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
	want := func(name string, v any) {
		t.Helper()
		if got := h.key(name); got != v {
			t.Errorf("%s = %v, want %v", name, got, v)
		}
	}
	want("sideBarVisible", true)
	want("panelVisible", false)
	want("panelPosition", "bottom")
	want("panelAlignment", "center")
	want("panelMaximized", false)
	want("mainEditorAreaVisible", true)
	h.run("workbench.action.toggleSidebarVisibility")
	want("sideBarVisible", false)
	h.run("workbench.action.positionPanelRight")
	want("panelVisible", true)
	want("panelPosition", "right")
	h.run("workbench.action.togglePanel")
	want("panelVisible", false)
	noErrors(t, h)
}

func TestEachPartHasItsOwnScope(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
	scopes := map[Part]any{}
	for _, tc := range []struct {
		command string
		part    Part
	}{
		{"workbench.action.focusSideBar", PartSideBar}, {"workbench.action.focusStatusBar", PartStatusBar},
		{"workbench.action.focusActivityBar", PartActivityBar},
	} {
		h.run(tc.command)
		ctx := h.ts.Element(gluttertest.ByKey(PartKey(PartEditor)))
		focus := widgetsFocus(ctx)
		scope := h.scopes.ScopeOf(focus, h.keys)
		if scope == h.keys {
			t.Errorf("%s: the focus has no scope of its own", tc.command)
		}
		for p, other := range scopes {
			if other == scope {
				t.Errorf("%s and %s share a scope", tc.part, p)
			}
		}
		scopes[tc.part] = scope
		// a key set in the window shows in the scope of the part
		if got := scope.Get("sideBarVisible"); got != true {
			t.Errorf("%s: sideBarVisible = %v in its scope", tc.part, got)
		}
	}
	noErrors(t, h)
}

func TestMenuBarVisibility(t *testing.T) {
	menus := []kit.MenuBarMenu{{Label: "&File"}, {Label: "&Edit"}}
	cases := []struct {
		value  string
		height float64
	}{{"classic", 30}, {"visible", 30}, {"toggle", 0}, {"hidden", 0}}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), map[string]any{"window.menuBarVisibility": tc.value},
				func(o *Options) { o.Menus = menus })
			if got := h.rect(PartMenuBar).Height(); got != tc.height {
				t.Errorf("the menu bar area is %v high, want %v", got, tc.height)
			}
			rectIs(t, "the activity bar", h.rect(PartActivityBar), 0, tc.height, 48, 618)
			noErrors(t, h)
		})
	}
}

func TestToggleMenuBar(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, func(o *Options) { o.Menus = []kit.MenuBarMenu{{Label: "&File"}} })
	value := func() string { return settings.Get[string](h.store, "window.menuBarVisibility", "") }
	if value() != "classic" || h.rect(PartMenuBar).Height() != 30 {
		t.Fatal("the menu bar does not start classic")
	}
	h.run("workbench.action.toggleMenuBar")
	if value() != "toggle" || h.rect(PartMenuBar).Height() != 0 {
		t.Errorf("after one run: %q, %v high", value(), h.rect(PartMenuBar).Height())
	}
	h.run("workbench.action.toggleMenuBar")
	if value() != "classic" || h.rect(PartMenuBar).Height() != 30 {
		t.Errorf("after two runs: %q, %v high", value(), h.rect(PartMenuBar).Height())
	}
	noErrors(t, h)
}

func TestAltShowsTheMenuBarThatIsToggled(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), map[string]any{"window.menuBarVisibility": "toggle"},
		func(o *Options) { o.Menus = []kit.MenuBarMenu{{Label: "&File"}, {Label: "&Edit"}} })
	if h.rect(PartMenuBar).Height() != 0 {
		t.Fatal("the menu bar is shown")
	}
	h.run("workbench.actions.menubar.focus")
	if got := h.rect(PartMenuBar).Height(); got != 30 {
		t.Errorf("the menu bar area is %v high after the command that focuses it", got)
	}
	h.run("workbench.actions.menubar.focus")
	if got := h.rect(PartMenuBar).Height(); got != 0 {
		t.Errorf("the menu bar area is %v high after the second run", got)
	}
	noErrors(t, h)
}

// button finds an icon button by its tooltip.
func button(tooltip string) gluttertest.Finder {
	return gluttertest.ByPredicate("icon button "+tooltip, func(w Widget) bool {
		b, ok := w.(*kit.IconButton)
		return ok && b.Tooltip() == tooltip
	})
}

func TestPanelTitleStrip(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	var selected string
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, func(o *Options) {
		o.PanelTabs = []PanelTab{{ID: "problems", Title: "Problems"}, {ID: "output", Title: "Output"}}
		o.ActivePanel = "problems"
		o.OnSelectPanel = func(id string) { selected = id }
	})
	if err := gluttertest.FindsOneWidget(h.ts, gluttertest.ByText("PROBLEMS")); err != nil {
		t.Error(err)
	}
	h.ts.Tap(gluttertest.ByText("OUTPUT"))
	h.settle()
	if selected != "output" {
		t.Errorf("a tap on a tab selected %q", selected)
	}

	// the buttons maximize and hide the panel
	h.ts.Tap(button("Maximize Panel"))
	h.settle()
	if !h.layout.PanelMaximized() {
		t.Error("the maximize button did not maximize the panel")
	}
	if len(h.ts.Elements(button("Restore Panel"))) == 0 {
		t.Error("the maximized panel has no restore button")
	}
	h.ts.Tap(button("Restore Panel"))
	h.settle()
	if h.layout.PanelMaximized() {
		t.Error("the restore button did not restore the panel")
	}
	h.ts.Tap(button("Hide Panel"))
	h.settle()
	if h.shown(PartPanel) {
		t.Error("the close button did not hide the panel")
	}
	noErrors(t, h)
}

func TestSideBarTitleAndBodyAndStatusBarItems(t *testing.T) {
	h := newHarness(t, testSize, kit.LightModern(), DefaultLayoutState(), nil, func(o *Options) {
		o.SideBarTitle = "Explorer"
		o.StatusLeft = []Widget{statusText("main")}
		o.StatusRight = []Widget{statusText("Ln 1, Col 1")}
	})
	for _, text := range []string{"EXPLORER", "main", "Ln 1, Col 1"} {
		if err := gluttertest.FindsOneWidget(h.ts, gluttertest.ByText(text)); err != nil {
			t.Error(err)
		}
	}
	left := h.ts.Center(gluttertest.ByText("main")).Dx
	right := h.ts.Center(gluttertest.ByText("Ln 1, Col 1")).Dx
	if left > 100 || right < 900 {
		t.Errorf("the items are at %v and %v: they belong at the two ends", left, right)
	}
	noErrors(t, h)
}

func TestTheStatusBarIsTheColorOfAWindowWithoutAFolder(t *testing.T) {
	colors := kit.DarkModern()
	colors.StatusBarNoFolderBackground = ui.NewColor(0xFF68217A)
	colors.StatusBarBackground = ui.NewColor(0xFF007ACC)
	for _, tc := range []struct {
		noFolder bool
		want     ui.Color
	}{{false, ui.NewColor(0xFF007ACC)}, {true, ui.NewColor(0xFF68217A)}} {
		h := newHarness(t, testSize, colors, DefaultLayoutState(), nil, func(o *Options) { o.NoFolder = tc.noFolder })
		img := h.ts.Screenshot()
		px := img.RGBAAt(500, 630)
		got := ui.NewColor(0xFF000000 | uint32(px.R)<<16 | uint32(px.G)<<8 | uint32(px.B))
		if got != tc.want {
			t.Errorf("NoFolder %v: the status bar is %v, want %v", tc.noFolder, got, tc.want)
		}
	}
}

func TestDialogsShowOverTheWorkbenchOnce(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
	if n := len(h.ts.Elements(gluttertest.ByType[*kit.DialogHost]())); n != 1 {
		t.Fatalf("%d dialog hosts", n)
	}
	h.dialogs.Show(kit.DialogSpec{Type: kit.DialogInfo, Message: "Hello", Buttons: []string{"&&OK"}})
	h.settle()
	if err := gluttertest.FindsOneWidget(h.ts, gluttertest.ByText("Hello")); err != nil {
		t.Error(err)
	}
	// the parts below keep what they have
	if !h.shown(PartEditor) || !h.shown(PartSideBar) {
		t.Error("the parts went away under the dialog")
	}
	noErrors(t, h)
}

func TestAWorkbenchWithoutServicesStillShows(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, func(o *Options) {
		o.Commands, o.Keys, o.Scopes, o.Settings, o.Dialogs, o.Editor = nil, nil, nil, nil, nil, nil
	})
	rectIs(t, "the editor area", h.rect(PartEditor), 348, 30, 1000, 618)
	noErrors(t, h)
}

func TestTheCommandsAreGivenBack(t *testing.T) {
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, nil)
	if !h.commands.HasHandler("workbench.action.togglePanel") {
		t.Fatal("the workbench did not take its commands over")
	}
	h.ts.PumpWidget(gluttertestEmpty())
	h.ts.Pump(time.Millisecond)
	if h.commands.HasHandler("workbench.action.togglePanel") {
		t.Error("the commands are still bound after the workbench is gone")
	}
}

// groupCommands are the commands of the editor group, whose handlers the app binds.
var groupCommands = map[string]bool{
	"workbench.action.closeActiveEditor": true, "workbench.action.closeEditorsInGroup": true,
	"workbench.action.closeUnmodifiedEditors": true, "workbench.action.closeOtherEditors": true,
	"workbench.action.closeEditorsToTheRight": true, "workbench.action.closeAllEditors": true,
	"workbench.action.keepEditor": true, "workbench.action.pinEditor": true, "workbench.action.unpinEditor": true,
	"workbench.action.nextEditor": true, "workbench.action.previousEditor": true,
	"workbench.action.openNextRecentlyUsedEditorInGroup":     true,
	"workbench.action.openPreviousRecentlyUsedEditorInGroup": true,
	// the path of the file of the active editor (step 6.10; copyFilePath and copyRelativeFilePath are the app's too)
	"workbench.action.files.copyPathOfActiveFile": true,
}

// gridCommand tells whether a command is one of the grid of editor groups, which the app binds too (step
// 6.9): the splits, the moves of editors and groups, the joins, the closing of groups, the sizes, the
// layouts and the focus of a group by its place or its neighbors.
func gridCommand(id string) bool {
	for _, prefix := range []string{
		"workbench.action.splitEditor", "workbench.action.moveEditorTo", "workbench.action.newGroup",
		"workbench.action.moveActiveEditorGroup", "workbench.action.duplicateActiveEditorGroup", "workbench.action.editorLayout",
	} {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	switch id {
	case "workbench.action.joinTwoGroups", "workbench.action.joinAllGroups", "workbench.action.closeGroup",
		"workbench.action.closeEditorsAndGroup", "workbench.action.closeAllGroups", "workbench.action.closeEditorsInOtherGroups",
		"workbench.action.closeEditorInAllGroups", "workbench.action.evenEditorWidths", "workbench.action.toggleMaximizeEditorGroup",
		"workbench.action.navigateEditorGroups":
		return true
	}
	// focus first, second... last, next, previous, left, right, above and below; the workbench's own is the
	// active group
	return strings.HasPrefix(id, "workbench.action.focus") && strings.Contains(id, "Group") && id != "workbench.action.focusActiveEditorGroup"
}

// panelAppCommands are the commands of the panels that the app binds, since the views of the Problems and
// the Output are the window's (step 6.12); the workbench binds the commands that open and focus them.
var panelAppCommands = map[string]bool{
	"workbench.action.openActiveLogOutputFile": true, "workbench.output.action.clearOutput": true,
	"workbench.output.action.toggleAutoScroll":                            true,
	"workbench.actions.treeView.workbench.panel.markers.view.collapseAll": true,
	"workbench.actions.workbench.panel.markers.view.toggleErrors":         true,
	"workbench.actions.workbench.panel.markers.view.toggleWarnings":       true,
	"workbench.actions.workbench.panel.markers.view.toggleInfos":          true,
	"workbench.actions.workbench.panel.markers.view.toggleActiveFile":     true,
}

// explorerAppCommands are the commands of the Explorer, which the app binds since the view and the folder
// are the window's (step 6.14).
var explorerAppCommands = map[string]bool{
	"workbench.view.explorer": true, "workbench.explorer.fileView.focus": true, "workbench.files.action.focusOpenEditorsView": true,
	"workbench.files.action.focusFilesExplorer": true, "workbench.files.action.showActiveFileInExplorer": true,
	"workbench.files.action.refreshFilesExplorer": true, "workbench.files.action.collapseExplorerFolders": true,
	"workbench.action.files.openFolder": true, "workbench.action.closeFolder": true,
	"workbench.files.action.createFileFromExplorer": true, "workbench.files.action.createFolderFromExplorer": true,
}

// quickInputCommands are the commands of the quick input: the app binds the ones that open it (the providers need
// the shell) and the quick input host binds the one that closes it (step 6.17).
var quickInputCommands = map[string]bool{
	"workbench.action.showCommands": true, "workbench.action.quickOpen": true, "workbench.action.gotoLine": true,
	"workbench.action.gotoOffset": true, "workbench.action.openView": true, "workbench.action.quickOpenView": true,
	"workbench.action.clearCommandHistory": true, "workbench.action.closeQuickOpen": true,
}

// historyCommands are the commands of the history of locations and of the editor picker, which the app binds
// since the groups, the editors and the quick input are the window's (step 6.19).
var historyCommands = map[string]bool{
	"workbench.action.nextEditorInGroup": true, "workbench.action.previousEditorInGroup": true,
	"workbench.action.openNextRecentlyUsedEditor": true, "workbench.action.openPreviousRecentlyUsedEditor": true,
	"workbench.action.showAllEditors": true, "workbench.action.showAllEditorsByMostRecentlyUsed": true,
	"workbench.action.showEditorsInActiveGroup":                   true,
	"workbench.action.quickOpenPreviousRecentlyUsedEditor":        true,
	"workbench.action.quickOpenLeastRecentlyUsedEditor":           true,
	"workbench.action.quickOpenPreviousRecentlyUsedEditorInGroup": true,
	"workbench.action.quickOpenLeastRecentlyUsedEditorInGroup":    true,
	"workbench.action.openPreviousEditorFromHistory":              true,
	"workbench.action.quickOpenNavigateNextInEditorPicker":        true,
	"workbench.action.quickOpenNavigatePreviousInEditorPicker":    true,
	"workbench.action.navigateBack":                               true,
	"workbench.action.navigateForward":                            true,
	"workbench.action.navigateLast":                               true,
	"workbench.action.navigateBackInEditLocations":                true,
	"workbench.action.navigateForwardInEditLocations":             true,
	"workbench.action.navigatePreviousInEditLocations":            true,
	"workbench.action.navigateToLastEditLocation":                 true,
	"workbench.action.navigateBackInNavigationLocations":          true,
	"workbench.action.navigateForwardInNavigationLocations":       true,
	"workbench.action.navigatePreviousInNavigationLocations":      true,
	"workbench.action.navigateToLastNavigationLocation":           true,
	"workbench.action.reopenClosedEditor":                         true, "workbench.action.clearEditorHistory": true,
}

// statusBarAppCommands are the commands of the pickers of the status bar, which the app binds since the
// editors, the quick input and the languages are the window's (step 6.20).
var statusBarAppCommands = map[string]bool{
	"workbench.action.editor.changeLanguageMode": true, "workbench.action.editor.changeEOL": true,
	"workbench.action.editor.changeEncoding": true,
}

// saveAppCommands are the commands of saving, which the app binds since the documents are the window's (step 7.1); Toggle Auto Save writes the window's settings (step 7.3).
var saveAppCommands = map[string]bool{
	"workbench.action.files.save": true, "workbench.action.files.saveWithoutFormatting": true, "workbench.action.files.saveAs": true,
	"workbench.action.files.saveAll": true, "workbench.action.files.revert": true, "workbench.action.files.newUntitledFile": true,
	"workbench.action.toggleAutoSave": true,
}

// windowAppCommands are the commands that close the window, quit and open or pick a window, which the app binds since
// the windows, their editors and their documents are its own (steps 7.2 and 7.8).
var windowAppCommands = map[string]bool{
	"workbench.action.closeWindow": true, "workbench.action.quit": true,
	// the windows are the process's (step 7.8)
	"workbench.action.newWindow": true, "workbench.action.switchWindow": true,
}

// recentAppCommands are the commands of the recently opened folders and files, which the app binds since the folder
// and the editors of the window are its own (step 7.5).
var recentAppCommands = map[string]bool{"workbench.action.openRecent": true, "workbench.action.clearRecentFiles": true}

// settingsAppCommands are the commands that open the settings editor, which the app binds since the editor groups are its
// own (steps 8.1 and 8.2).
var settingsAppCommands = map[string]bool{
	"workbench.action.openSettings": true, "workbench.action.openSettings2": true, "workbench.action.openWorkspaceSettings": true,
	// the JSON files and the default settings (step 8.2)
	"workbench.action.openSettingsJson": true, "workbench.action.openGlobalSettings": true, "workbench.action.openRawDefaultSettings": true,
	"workbench.action.openWorkspaceSettingsFile": true,
}

func TestEveryWorkbenchCommandHasAHandler(t *testing.T) {
	// the commands that open the views of the panel are the workbench's when it has the views
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, func(o *Options) { o.Views = NewViews(command.DefaultViews(), nil) })
	var n int
	for _, meta := range h.commands.All() {
		// the commands of the editor group are bound by the app, which owns the group (step 6.8)
		if !strings.HasPrefix(meta.ID, "workbench.") || groupCommands[meta.ID] || gridCommand(meta.ID) || panelAppCommands[meta.ID] || explorerAppCommands[meta.ID] || quickInputCommands[meta.ID] || historyCommands[meta.ID] || statusBarAppCommands[meta.ID] || saveAppCommands[meta.ID] || windowAppCommands[meta.ID] || recentAppCommands[meta.ID] || settingsAppCommands[meta.ID] {
			continue
		}
		n++
		if !h.commands.HasHandler(meta.ID) {
			t.Errorf("%s has no handler", meta.ID)
		}
	}
	if n != 23 {
		t.Errorf("%d workbench commands are registered, want 23", n)
	}
}

func TestAWorkbenchWithoutTheCommandsReportsTheMistake(t *testing.T) {
	// a registry that has none of the layout commands: the framework catches the panic of the widget and reports it
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, func(o *Options) { o.Commands = command.NewRegistry() })
	errs := h.ts.TakeErrors()
	if len(errs) == 0 {
		t.Fatal("nothing was reported")
	}
	var found bool
	for _, err := range errs {
		var appErr *errors.Error
		if errors.As(err, &appErr) && appErr.Tag == "LayoutCommandMissing" {
			found = true
			if _, ok := appErr.Metadata["command"]; !ok {
				t.Errorf("the error does not say which command: %v", appErr.Metadata)
			}
		}
	}
	if !found {
		t.Errorf("no error has the tag LayoutCommandMissing: %v", errs)
	}
}

func TestPanelTabBadgeAndViewActions(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	actions := func(width float64) Widget {
		return widgets.NewSizedBox(widgets.NewText("view actions"), widgets.SizedBoxOpts{Width: foundation.Some(width), Height: foundation.Some(22.0)})
	}
	cases := []struct {
		name        string
		size        ui.Size
		badge       int
		wantBadge   string
		wantActions bool
	}{
		{"a count after the title", testSize, 7, "7", true},
		{"a long count is cut", testSize, 1500, "999+", true},
		{"no badge without a count", testSize, 0, "", true},
		{"no room for the actions", ui.Size{Width: 300, Height: 400}, 7, "7", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.size, kit.DarkModern(), st, nil, func(o *Options) {
				o.PanelTabs = []PanelTab{{ID: "problems", Title: "Problems", Badge: tc.badge, BadgeTooltip: "Total problems"}, {ID: "output", Title: "Output"}}
				o.ActivePanel = "problems"
				o.PanelActions = actions(200)
			})
			badge := h.ts.Exists(gluttertest.ByKey(PanelBadgeKey("problems")))
			if badge != (tc.wantBadge != "") {
				t.Errorf("badge shown = %v, want %v", badge, tc.wantBadge != "")
			}
			if tc.wantBadge != "" && !h.ts.Exists(gluttertest.ByText(tc.wantBadge)) {
				t.Errorf("the badge does not say %s", tc.wantBadge)
			}
			if got := h.ts.Exists(gluttertest.ByText("view actions")); got != tc.wantActions {
				t.Errorf("actions shown = %v, want %v", got, tc.wantActions)
			}
			// the buttons are there whatever the room
			if !h.ts.Exists(button("Hide Panel")) {
				t.Error("no Hide Panel button")
			}
			noErrors(t, h)
		})
	}
}

func TestPanelViewCommandsNeedViews(t *testing.T) {
	// the commands that open the views of the panel are bound only when the workbench has the views
	st := DefaultLayoutState()
	views := NewViews(command.DefaultViews(), nil)
	h := newHarness(t, testSize, kit.DarkModern(), st, nil, func(o *Options) { o.Views = views })
	views.layout = h.layout
	h.run("workbench.action.output.toggleOutput")
	if !h.layout.State().PanelVisible || views.ActivePanel() != command.ViewContainerOutput || h.key("activePanel") != command.ViewContainerOutput {
		t.Errorf("visible %v, active %q, key %v", h.layout.State().PanelVisible, views.ActivePanel(), h.key("activePanel"))
	}
	h.run("workbench.actions.view.problems")
	if views.ActivePanel() != command.ViewContainerMarkers || h.key("problemsVisibility") != true {
		t.Errorf("active %q, problemsVisibility %v", views.ActivePanel(), h.key("problemsVisibility"))
	}
	h.run("workbench.actions.view.toggleProblems")
	if h.layout.State().PanelVisible || h.key("problemsVisibility") != false {
		t.Errorf("toggleProblems left the panel visible=%v, problemsVisibility %v", h.layout.State().PanelVisible, h.key("problemsVisibility"))
	}
	noErrors(t, h)
}
