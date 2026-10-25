package app

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests of full screen, zen mode and the centered layout (step 8.5, SPEC §10.9).

// fullScreenFake is a window controller that goes to full screen when it is asked to, and tells its listeners (the shell
// follows it, as it follows the controller of Glutter), or does not answer.
type fullScreenFake struct {
	fakeWindow
	calls  []bool
	answer bool
}

func newFullScreenFake(answer bool) *fullScreenFake {
	return &fullScreenFake{fakeWindow: fakeWindow{ChangeNotifier: *foundation.NewChangeNotifier(), size: testWindowSize}, answer: answer}
}

func (f *fullScreenFake) SetFullscreen(on bool) {
	f.calls = append(f.calls, on)
	if f.answer {
		f.report(on)
	}
}

// report is the platform telling that the window is in full screen mode, or is not.
func (f *fullScreenFake) report(on bool) {
	f.fullscreen = on
	f.NotifyListeners()
}

// connect makes the app follow and drive the fake as its window.
func (a *groupApp) connect(f *fullScreenFake) {
	a.win.controller = f
	s := a.shell()
	s.state.source = nil
	s.trackWindow(f)
}

// zenApp is a window with two files open and the panel showing, with the settings the test gives it.
func zenApp(t *testing.T, extra map[string]any) (*groupApp, *fullScreenFake) {
	t.Helper()
	values := map[string]any{"editor.minimap.enabled": false, "breadcrumbs.enabled": false, "files.hotExit": "off"}
	for k, v := range extra {
		values[k] = v
	}
	win, _ := themedWindow(t, values)
	a := newGroupAppWith(t, win, testWindowSize, []string{"a.go", "b.go"}, nil, "a.go", "b.go")
	fake := newFullScreenFake(true)
	a.connect(fake)
	a.run("workbench.action.togglePanel")
	return a, fake
}

func (a *groupApp) lineNumbers() editor.LineNumbersMode {
	return a.shell().activeEditor().view.Options().LineNumbers
}

func (a *groupApp) menuBarHeight() float64 { return a.ts.Size(partOf(workbench.PartMenuBar)).Height }

func (a *groupApp) hasTabs() bool { return a.ts.Exists(editorTabs()) }

func TestZenModeHidesThePartsAndExitRestoresThem(t *testing.T) {
	a, fake := zenApp(t, nil)
	before := a.win.Layout.State()
	if !before.SideBarVisible || !before.PanelVisible || !before.ActivityBarVisible || !before.StatusBarVisible {
		t.Fatalf("the layout before: %+v", before)
	}
	if a.menuBarHeight() != workbench.TitleBarHeight || !a.hasTabs() || a.lineNumbers() != editor.LineNumbersOn {
		t.Fatalf("the window before: menu bar %v, tabs %v, line numbers %v", a.menuBarHeight(), a.hasTabs(), a.lineNumbers())
	}

	a.run("workbench.action.toggleZenMode")
	st := a.win.Layout.State()
	if !st.Zen.Active || st.SideBarVisible || st.PanelVisible || st.ActivityBarVisible || st.StatusBarVisible || !st.CenteredLayout {
		t.Errorf("zen mode did not hide the parts or center the layout: %+v", st)
	}
	if !a.win.ZenMode() || a.win.Keys.Get("inZenMode") != true || a.win.Keys.Get("isCenteredLayout") != true {
		t.Errorf("the context keys: inZenMode %v, isCenteredLayout %v", a.win.Keys.Get("inZenMode"), a.win.Keys.Get("isCenteredLayout"))
	}
	if !a.win.Notify.DoNotDisturb() {
		t.Error("do not disturb is not on in zen mode")
	}
	if !reflect.DeepEqual(fake.calls, []bool{true}) || !a.win.IsFullScreen() || a.win.Keys.Get("isFullscreen") != true {
		t.Errorf("full screen: calls %v, is %v, key %v", fake.calls, a.win.IsFullScreen(), a.win.Keys.Get("isFullscreen"))
	}
	if a.menuBarHeight() != 0 {
		t.Errorf("the menu bar is %v high in zen mode", a.menuBarHeight())
	}
	if a.lineNumbers() != editor.LineNumbersOff {
		t.Error("the editor shows its line numbers in zen mode")
	}
	if got := settings.Get[string](a.win.Settings, "editor.lineNumbers", ""); got != "on" {
		t.Errorf("zen mode changed editor.lineNumbers to %q", got)
	}
	if !a.hasTabs() {
		t.Error("zen mode hid the tabs, though zenMode.showTabs is multiple")
	}

	a.run("workbench.action.toggleZenMode")
	after := a.win.Layout.State()
	if after != before {
		t.Errorf("the layout after zen mode is %+v, want %+v", after, before)
	}
	if a.win.ZenMode() || a.win.Keys.Get("inZenMode") != false || a.win.Keys.Get("isCenteredLayout") != false {
		t.Error("the context keys of zen mode are still set")
	}
	if a.win.Notify.DoNotDisturb() {
		t.Error("do not disturb is still on")
	}
	if !reflect.DeepEqual(fake.calls, []bool{true, false}) || a.win.IsFullScreen() {
		t.Errorf("full screen: calls %v, is %v", fake.calls, a.win.IsFullScreen())
	}
	if a.menuBarHeight() != workbench.TitleBarHeight || a.lineNumbers() != editor.LineNumbersOn {
		t.Errorf("after zen mode: menu bar %v, line numbers %v", a.menuBarHeight(), a.lineNumbers())
	}
	a.noErrors()
}

func TestEachZenModeSettingOnItsOwn(t *testing.T) {
	cases := []struct {
		name   string
		extra  map[string]any
		during func(t *testing.T, a *groupApp, f *fullScreenFake)
	}{
		{name: "the activity bar stays", extra: map[string]any{"zenMode.hideActivityBar": false}, during: func(t *testing.T, a *groupApp, _ *fullScreenFake) {
			if st := a.win.Layout.State(); !st.ActivityBarVisible || st.StatusBarVisible || st.SideBarVisible {
				t.Errorf("layout %+v", st)
			}
		}},
		{name: "the status bar stays", extra: map[string]any{"zenMode.hideStatusBar": false}, during: func(t *testing.T, a *groupApp, _ *fullScreenFake) {
			if st := a.win.Layout.State(); st.ActivityBarVisible || !st.StatusBarVisible || st.SideBarVisible {
				t.Errorf("layout %+v", st)
			}
		}},
		{name: "the layout is not centered", extra: map[string]any{"zenMode.centerLayout": false}, during: func(t *testing.T, a *groupApp, _ *fullScreenFake) {
			if a.win.Layout.CenteredLayout() {
				t.Error("the layout is centered")
			}
		}},
		{name: "no full screen", extra: map[string]any{"zenMode.fullScreen": false}, during: func(t *testing.T, a *groupApp, f *fullScreenFake) {
			if len(f.calls) != 0 || a.win.IsFullScreen() {
				t.Errorf("full screen calls %v", f.calls)
			}
		}},
		{name: "no tabs", extra: map[string]any{"zenMode.showTabs": "none"}, during: func(t *testing.T, a *groupApp, _ *fullScreenFake) {
			if a.win.ShowTabs() != "none" || a.hasTabs() {
				t.Errorf("tabs: %q, %v", a.win.ShowTabs(), a.hasTabs())
			}
			if got := settings.Get[string](a.win.Settings, "workbench.editor.showTabs", ""); got != "multiple" {
				t.Errorf("zen mode changed workbench.editor.showTabs to %q", got)
			}
		}},
		{name: "a single tab", extra: map[string]any{"zenMode.showTabs": "single"}, during: func(t *testing.T, a *groupApp, _ *fullScreenFake) {
			if a.win.ShowTabs() != "single" || !a.hasTabs() {
				t.Errorf("tabs: %q, %v", a.win.ShowTabs(), a.hasTabs())
			}
		}},
		{name: "the line numbers stay", extra: map[string]any{"zenMode.hideLineNumbers": false}, during: func(t *testing.T, a *groupApp, _ *fullScreenFake) {
			if a.lineNumbers() != editor.LineNumbersOn {
				t.Error("the line numbers are off")
			}
		}},
		{name: "do not disturb is not turned on", extra: map[string]any{"zenMode.silentNotifications": false}, during: func(t *testing.T, a *groupApp, _ *fullScreenFake) {
			if a.win.Notify.DoNotDisturb() {
				t.Error("do not disturb is on")
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, fake := zenApp(t, tc.extra)
			before := a.win.Layout.State()
			a.run("workbench.action.toggleZenMode")
			tc.during(t, a, fake)
			a.run("workbench.action.toggleZenMode")
			if after := a.win.Layout.State(); after != before {
				t.Errorf("the layout after is %+v, want %+v", after, before)
			}
			if a.win.Notify.DoNotDisturb() || a.lineNumbers() != editor.LineNumbersOn || a.win.ShowTabs() != "multiple" {
				t.Error("what zen mode changed is still changed")
			}
			a.noErrors()
		})
	}
}

func TestZenModeLeavesWhatWasOnAlone(t *testing.T) {
	a, fake := zenApp(t, nil)
	// full screen, the centered layout and do not disturb were on before
	fake.report(true)
	a.win.Layout.SetCenteredLayout(true)
	a.win.Notify.SetDoNotDisturb(true)
	a.pump()
	a.run("workbench.action.toggleZenMode")
	if len(fake.calls) != 0 {
		t.Errorf("zen mode asked for full screen again: %v", fake.calls)
	}
	a.run("workbench.action.toggleZenMode")
	if len(fake.calls) != 0 || !a.win.IsFullScreen() || !a.win.Layout.CenteredLayout() || !a.win.Notify.DoNotDisturb() {
		t.Errorf("zen mode turned off what it did not turn on: calls %v, full screen %v, centered %v, dnd %v",
			fake.calls, a.win.IsFullScreen(), a.win.Layout.CenteredLayout(), a.win.Notify.DoNotDisturb())
	}
	a.noErrors()
}

func TestAPartTheUserChangedDuringZenModeStaysAsItIs(t *testing.T) {
	a, _ := zenApp(t, nil)
	a.run("workbench.action.toggleZenMode")
	a.press("ctrl+b") // shows the side bar
	a.run("workbench.action.toggleStatusbarVisibility")
	st := a.win.Layout.State()
	if !st.SideBarVisible || !st.StatusBarVisible || st.PanelVisible || st.ActivityBarVisible {
		t.Fatalf("during zen mode: %+v", st)
	}
	if got := settings.Get[bool](a.win.Settings, "workbench.statusBar.visible", ""); !got {
		t.Error("showing the status bar in zen mode wrote the setting")
	}
	a.run("workbench.action.toggleZenMode")
	st = a.win.Layout.State()
	if !st.SideBarVisible || !st.StatusBarVisible || !st.PanelVisible || !st.ActivityBarVisible || st.Zen.Active {
		t.Errorf("after zen mode: %+v", st)
	}
	// a part that the user hid again stays hidden, too
	a.run("workbench.action.toggleZenMode")
	a.press("ctrl+b")
	a.press("ctrl+b")
	a.run("workbench.action.toggleZenMode")
	if a.win.Layout.State().SideBarVisible {
		t.Error("the side bar the user showed and hid again came back")
	}
	a.noErrors()
}

func TestEscapeEscapeLeavesZenMode(t *testing.T) {
	a, _ := zenApp(t, nil)
	a.run("workbench.action.toggleZenMode")
	a.shell().activeEditor().focus.Request()
	a.pump()
	a.press("escape")
	if !a.win.ZenMode() {
		t.Fatal("a single Escape left zen mode")
	}
	if !a.win.Dispatcher.InChordMode() {
		t.Error("the first Escape is not the start of a chord")
	}
	a.press("escape")
	if a.win.ZenMode() || a.win.Layout.State().SideBarVisible == false {
		t.Errorf("Escape Escape did not leave zen mode: %+v", a.win.Layout.State())
	}
	// outside zen mode the key does nothing special
	a.shell().activeEditor().focus.Request()
	a.pump()
	if a.win.Keys.Get("inZenMode") != false {
		t.Error("inZenMode is still set")
	}
	a.noErrors()
}

func TestZenModeAndFullScreenFollowEachOther(t *testing.T) {
	t.Run("leaving full screen leaves zen mode", func(t *testing.T) {
		a, fake := zenApp(t, nil)
		before := a.win.Layout.State()
		a.run("workbench.action.toggleZenMode")
		a.press("f11")
		if !reflect.DeepEqual(fake.calls, []bool{true, false}) {
			t.Errorf("calls %v", fake.calls)
		}
		if a.win.ZenMode() || a.win.Layout.State() != before || a.win.Notify.DoNotDisturb() {
			t.Errorf("zen mode went on after full screen was left: %+v", a.win.Layout.State())
		}
		a.noErrors()
	})
	t.Run("the platform leaves full screen by itself", func(t *testing.T) {
		a, fake := zenApp(t, nil)
		a.run("workbench.action.toggleZenMode")
		fake.report(false)
		a.pump()
		if a.win.ZenMode() {
			t.Error("zen mode stays after the window left full screen")
		}
		if !reflect.DeepEqual(fake.calls, []bool{true}) {
			t.Errorf("zen mode asked for full screen to end after it ended: %v", fake.calls)
		}
	})
	t.Run("zen mode without full screen stays when F11 goes both ways", func(t *testing.T) {
		a, fake := zenApp(t, map[string]any{"zenMode.fullScreen": false})
		a.run("workbench.action.toggleZenMode")
		a.press("f11")
		fake.report(false)
		if !a.win.ZenMode() {
			t.Error("zen mode ended though it did not make the full screen")
		}
	})
	t.Run("F11 outside zen mode", func(t *testing.T) {
		a, fake := zenApp(t, nil)
		a.press("f11")
		if !a.win.IsFullScreen() || a.menuBarHeight() != 0 {
			t.Errorf("full screen %v, menu bar %v (the classic menu bar hides in full screen)", a.win.IsFullScreen(), a.menuBarHeight())
		}
		a.press("f11")
		if a.win.IsFullScreen() || a.menuBarHeight() != workbench.TitleBarHeight || !reflect.DeepEqual(fake.calls, []bool{true, false}) {
			t.Errorf("after the second F11: full screen %v, menu bar %v, calls %v", a.win.IsFullScreen(), a.menuBarHeight(), fake.calls)
		}
	})
	t.Run("the platform may refuse", func(t *testing.T) {
		a, fake := zenApp(t, nil)
		fake.answer = false
		a.run("workbench.action.toggleFullScreen")
		if a.win.IsFullScreen() || a.win.Keys.Get("isFullscreen") != false {
			t.Error("the window is in full screen though the platform did not say so")
		}
	})
}

func TestTheFullScreenRowOfTheViewMenuIsCheckedInFullScreen(t *testing.T) {
	a, fake := zenApp(t, nil)
	checked := func(id string) bool {
		t.Helper()
		it, ok := menuItem(a.shell().menus.Items(command.MenubarAppearanceMenu, menuContext{base: a.win.Keys, settings: a.win.Settings}), id)
		if !ok {
			t.Fatalf("%s is not in the Appearance menu", id)
		}
		return it.Checked
	}
	ids := []string{"workbench.action.toggleFullScreen", "workbench.action.toggleZenMode", "workbench.action.toggleCenteredLayout"}
	for _, id := range ids {
		if checked(id) {
			t.Errorf("%s is checked at the start", id)
		}
	}
	fake.report(true)
	a.win.Layout.SetCenteredLayout(true)
	a.pump()
	if !checked(ids[0]) || checked(ids[1]) || !checked(ids[2]) {
		t.Error("the rows do not follow full screen and the centered layout")
	}
	a.win.Layout.SetCenteredLayout(false)
	a.run("workbench.action.toggleZenMode")
	if !a.win.Keys.Matches(mustParseWhen(t, "inZenMode")) {
		t.Error("the key of zen mode is not set")
	}
	if !checked(ids[1]) {
		t.Error("Zen Mode is not checked in zen mode")
	}
}

func TestFullScreenNeedsAController(t *testing.T) {
	win, _ := themedWindow(t, nil)
	err := win.SetFullScreen(true)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("Workspace.SetFullScreen.FullScreenUnavailable") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	err = win.Commands.Run(command.Context{Ctx: context.Background(), Keys: win.Keys}, "workbench.action.toggleFullScreen", nil)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("FullScreenUnavailable") {
		t.Errorf("the command: %v", err)
	}
	// zen mode still works without it
	win.ToggleZenMode()
	if !win.ZenMode() || win.Layout.State().Zen.FullScreen {
		t.Errorf("zen mode without a controller: %+v", win.Layout.State().Zen)
	}
}

func TestLiveChangesOfTheZenModeSettings(t *testing.T) {
	a, _ := zenApp(t, nil)
	a.run("workbench.action.toggleZenMode")
	set := func(key string, value any) {
		t.Helper()
		if err := a.win.Settings.Update(context.Background(), key, value, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
		a.pump()
	}
	set("zenMode.hideStatusBar", false)
	if !a.win.Layout.State().StatusBarVisible {
		t.Error("the status bar does not come back")
	}
	set("zenMode.hideStatusBar", true)
	if a.win.Layout.State().StatusBarVisible {
		t.Error("the status bar does not go again")
	}
	set("zenMode.centerLayout", false)
	if a.win.Layout.CenteredLayout() {
		t.Error("the layout is still centered")
	}
	set("zenMode.hideLineNumbers", false)
	if a.lineNumbers() != editor.LineNumbersOn {
		t.Error("the line numbers do not come back")
	}
	set("zenMode.showTabs", "none")
	if a.hasTabs() {
		t.Error("the tabs do not go")
	}
	set("zenMode.silentNotifications", false)
	if a.win.Notify.DoNotDisturb() {
		t.Error("do not disturb stays")
	}
	a.run("workbench.action.toggleZenMode")
	if a.win.Layout.State().Zen.Active || a.win.Notify.DoNotDisturb() {
		t.Error("zen mode did not end")
	}
	a.noErrors()
}

func TestZenModeComesBackAfterARestart(t *testing.T) {
	cases := []struct {
		name    string
		extra   map[string]any
		wantZen bool
	}{
		{name: "zenMode.restore", wantZen: true},
		{name: "not restored", extra: map[string]any{"zenMode.restore": false}, wantZen: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newStateEnv(t, tc.extra)
			one := env.start(env.root)
			before := one.win.Layout.State()
			one.command("workbench.action.toggleZenMode", nil)
			if !one.win.ZenMode() {
				t.Fatal("zen mode did not start")
			}
			one.close() // writes the state
			if _, ok := env.savedWindow(env.root); !ok {
				t.Fatal("the state was not written")
			}
			two := env.start(env.root)
			st := two.win.Layout.State()
			if st.Zen.Active != tc.wantZen {
				t.Fatalf("zen mode is %v after the restart, want %v: %+v", st.Zen.Active, tc.wantZen, st)
			}
			if tc.wantZen {
				if st.SideBarVisible || st.StatusBarVisible || !st.CenteredLayout || !two.win.Notify.DoNotDisturb() ||
					two.win.Keys.Get("inZenMode") != true {
					t.Errorf("the restored zen mode: %+v, dnd %v", st, two.win.Notify.DoNotDisturb())
				}
				// and it ends with what it hid
				two.command("workbench.action.toggleZenMode", nil)
				st = two.win.Layout.State()
			}
			st.Zen = before.Zen
			if st != before {
				t.Errorf("the layout after zen mode is %+v, want %+v", st, before)
			}
			if two.win.Notify.DoNotDisturb() || two.win.ZenMode() {
				t.Error("zen mode or do not disturb is still on")
			}
			two.close()
		})
	}
}

func TestAZenModeThatRestoresAsksForFullScreenOnceThereIsAController(t *testing.T) {
	env := newStateEnv(t, nil)
	one := env.start(env.root)
	f1 := newFullScreenFake(true)
	one.win.controller = f1
	one.command("workbench.action.toggleZenMode", nil)
	one.close()

	two := env.start(env.root)
	if !two.win.ZenMode() {
		t.Fatal("zen mode did not come back")
	}
	f2 := newFullScreenFake(true)
	two.win.controller = f2
	two.win.controllerKnown()
	if !reflect.DeepEqual(f2.calls, []bool{true}) {
		t.Errorf("full screen asked for %v when the controller came", f2.calls)
	}
	two.close()
}

func TestZenModeBelongsToItsWindow(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.workspaceOf(one), a.workspaceOf(two)
	before := w2.Layout.State()
	if err := a.command(w1, "workbench.action.toggleZenMode", nil); err != nil {
		t.Fatal(err)
	}
	if !w1.ZenMode() || w2.ZenMode() || w2.Layout.State() != before {
		t.Errorf("zen mode of one window reached the other: %+v", w2.Layout.State())
	}
	if !w1.Notify.DoNotDisturb() || w2.Notify.DoNotDisturb() {
		t.Error("do not disturb is not the window's own")
	}
	if w2.Keys.Get("inZenMode") == true {
		t.Error("the key reached the other window")
	}
	if err := a.command(w1, "workbench.action.toggleZenMode", nil); err != nil {
		t.Fatal(err)
	}
}

func TestFullScreenThroughTheRealWindowController(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a := env.run(Options{Folder: one})
	w := a.workspaceOf(one)
	wins := a.ts.Windows()
	if len(wins) == 0 {
		t.Skip("the tester has no window")
	}
	view := wins[0].View
	if err := a.command(w, "workbench.action.toggleFullScreen", nil); err != nil {
		t.Fatal(err)
	}
	var asked bool
	for _, act := range a.ts.ViewActions(view) {
		if act.Kind == ui.WindowActionFullscreen {
			asked = true
		}
	}
	if !asked {
		t.Fatalf("the platform was not asked for full screen: %v", a.ts.ViewActions(view))
	}
	// the platform says so
	a.ts.SetWindowInfo(view, ui.WindowInfo{Activated: true, Fullscreen: true, Title: "x"})
	a.settle()
	if !w.IsFullScreen() || w.Keys.Get("isFullscreen") != true {
		t.Error("the window does not know it is in full screen")
	}
	a.ts.SetWindowInfo(view, ui.WindowInfo{Activated: true, Title: "x"})
	a.settle()
	if w.IsFullScreen() {
		t.Error("the window does not know it left full screen")
	}
}

// zenUserSettings reads the user's settings file.
func zenUserSettings(t *testing.T, paths storage.Paths) []byte {
	t.Helper()
	p, err := settings.UserSettingsPath(paths, storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestZenModeWritesNoSetting(t *testing.T) {
	win, paths := themedWindow(t, map[string]any{"editor.minimap.enabled": false})
	a := newGroupAppWith(t, win, testWindowSize, []string{"a.go"}, nil, "a.go")
	a.connect(newFullScreenFake(true))
	before := zenUserSettings(t, paths)
	a.run("workbench.action.toggleZenMode")
	a.run("workbench.action.toggleCenteredLayout")
	a.run("workbench.action.toggleZenMode")
	if after := zenUserSettings(t, paths); !bytes.Equal(before, after) {
		t.Errorf("the settings file changed:\n%s\n---\n%s", before, after)
	}
}

func mustParseWhen(t *testing.T, clause string) contextkey.Expr {
	t.Helper()
	e, err := contextkey.Parse(clause)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestCtrlKZTogglesZenMode(t *testing.T) {
	a, _ := zenApp(t, nil)
	a.press("ctrl+k z")
	if !a.win.ZenMode() {
		t.Fatal("Ctrl+K Z did not turn zen mode on")
	}
	a.press("ctrl+k z")
	if a.win.ZenMode() {
		t.Error("Ctrl+K Z did not turn zen mode off")
	}
}
