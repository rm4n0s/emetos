package exthost

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// The views of the extensions (step 11.5): what the manifest declares is there before the extension runs, showing a view
// activates its extension, which puts its content in a box, and a failure of the content stays in that scope.

// viewsManifest declares a container of the side bar with one view, a tab of the panel, and title actions for both.
func viewsManifest(events ...string) extension.Manifest {
	m := extension.Manifest{ID: "vx", Name: "Vx", Version: "1.0.0", ActivationEvents: events}
	m.Contributes.Commands = []extension.CommandSpec{{ID: "vx.refresh", Title: "Refresh"}, {ID: "vx.stop", Title: "Stop"}}
	m.Contributes.ViewContainers = []extension.ViewContainerSpec{{ID: "vx.box", Title: "Vx Box", Icon: "checklist"}}
	m.Contributes.Views = []extension.ViewSpec{{ID: "vx.list", Name: "List", Container: "vx.box", When: "vx.ready"}}
	m.Contributes.PanelViews = []extension.PanelViewSpec{{ID: "vx.tab", Title: "Vx Tab", Icon: "terminal"}}
	m.Contributes.ViewActions = []extension.ViewActionSpec{
		{View: "vx.list", Command: "vx.refresh", Icon: "refresh", Tooltip: "Refresh the list"},
		{View: "vx.tab", Command: "vx.stop", Icon: "debug-stop", When: "vx.running", Order: 2},
		{View: "vx.tab", Command: "vx.refresh", Icon: "refresh", Order: 1},
	}
	return m
}

// viewLog records what a test extension did with its views.
type viewLog struct {
	handles map[string]extension.ViewHandle
	builds  int
	saved   []string
	restore []string
}

// textView is the content of a view that shows a text and keeps a state.
func (l *viewLog) textView(text *string, state *string) extension.ViewContent {
	return extension.ViewContent{
		Build: func(bc widgets.BuildContext) widgets.Widget {
			l.builds++
			return widgets.NewText(*text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, ui.Theme(bc).Foreground)})
		},
		SaveState: func() (any, error) {
			l.saved = append(l.saved, *state)
			return map[string]string{"filter": *state}, nil
		},
		RestoreState: func(raw json.RawMessage) error {
			var v map[string]string
			if err := json.Unmarshal(raw, &v); err != nil {
				return err
			}
			*state = v["filter"]
			l.restore = append(l.restore, *state)
			return nil
		},
	}
}

// viewTab is a tab that has the extension "vx" with a content for each of its views.
type viewTab struct {
	*rig
	*tab
	log   *viewLog
	text  string
	state string
	ts    *gluttertest.Tester
}

// newViewTab makes a rig with the extension vx and a deferred tab (the UI thread of the real window defers what an
// extension posts). setup runs in Activate after both views were provided.
func newViewTab(t *testing.T, setup func(api extension.API, vt *viewTab) error) *viewTab {
	t.Helper()
	vt := &viewTab{log: &viewLog{handles: map[string]extension.ViewHandle{}}, text: "side bar text"}
	f, _ := factory(viewsManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			for _, id := range []string{"vx.list", "vx.tab"} {
				h, err := api.Views().Provide(id, vt.log.textView(&vt.text, &vt.state))
				if err != nil {
					return err
				}
				vt.log.handles[id] = h
			}
			if err := api.Commands().Handle("vx.refresh", func(context.Context, any) error { return nil }); err != nil {
				return err
			}
			if setup != nil {
				return setup(api, vt)
			}
			return nil
		}
	})
	vt.rig = newRig(t, f)
	vt.tab = vt.newTab("one")
	vt.deferred = true
	return vt
}

// show builds the box of a view in a window of 300 by 200 and lets the activation it causes run.
func (vt *viewTab) show(id string) {
	vt.rig.t.Helper()
	vt.ts = uitest.Pump(vt.rig.t, uitest.Themes()[0], gui.Size{Width: 300, Height: 200}, vt.scope.ViewBody(id))
	vt.settle()
}

// settle runs what the extension posted and the frames that follow, as the UI thread does.
func (vt *viewTab) settle() {
	vt.rig.t.Helper()
	for i := 0; i < 3; i++ {
		vt.pump()
		vt.ts.Pump()
	}
}

func (vt *viewTab) text1(s string) bool { return vt.ts.Exists(gluttertest.ByText(s)) }

func TestTheManifestsViewsAreThereBeforeTheExtensionRuns(t *testing.T) {
	vt := newViewTab(t, nil)
	reg := vt.views.Registry()
	c, ok := reg.Container("vx.box")
	if !ok || c.Title != "Vx Box" || c.Location != command.ViewLocationSideBar || c.Icon == 0 || c.Order != extensionOrder {
		t.Errorf("the side bar container = %+v, %v", c, ok)
	}
	if got := reg.Views("vx.box", nil); len(got) != 1 || got[0].ID != "vx.list" || got[0].Name != "List" {
		t.Errorf("the views of the container = %+v", got)
	}
	tab, ok := reg.Container("vx.tab")
	if !ok || tab.Title != "Vx Tab" || tab.Location != command.ViewLocationPanel || tab.Order != extensionOrder {
		t.Errorf("the panel container = %+v, %v", tab, ok)
	}
	// the tab comes after Problems and Output, and the icon after the Explorer
	var panel []string
	for _, p := range reg.Containers(command.ViewLocationPanel) {
		panel = append(panel, p.ID)
	}
	if !slices.Equal(panel, []string{command.ViewContainerMarkers, command.ViewContainerOutput, "vx.tab"}) {
		t.Errorf("the tabs of the panel are %v", panel)
	}
	var side []string
	for _, p := range reg.Containers(command.ViewLocationSideBar) {
		side = append(side, p.ID)
	}
	if !slices.Equal(side, []string{command.ViewContainerExplorer, "vx.box"}) {
		t.Errorf("the icons of the activity bar are %v", side)
	}
	if vt.scope.State("vx") != StateInactive {
		t.Errorf("the extension is %v before a view is shown", vt.scope.State("vx"))
	}
	for _, id := range []string{extension.ShowViewCommand("vx.box"), extension.ToggleViewCommand("vx.tab")} {
		if _, ok := vt.commands.Lookup(id); !ok {
			t.Errorf("the command %s is missing", id)
		}
	}
	if meta, _ := vt.commands.Lookup(extension.ToggleViewCommand("vx.tab")); meta.Title != "Toggle Vx Tab" {
		t.Errorf("the title of the toggle command is %q", meta.Title)
	}
	if !slices.Contains(vt.menuCommands(extension.MenuView), extension.ToggleViewCommand("vx.tab")) {
		t.Error("the View menu has no entry for the panel view")
	}
	// the commands show and toggle
	if err := vt.run(extension.ShowViewCommand("vx.box")); err != nil || vt.views.Active() != "vx.box" || !vt.layout.State().SideBarVisible {
		t.Errorf("Show Vx Box: %v, active %q", err, vt.views.Active())
	}
	for want := range []bool{true, false} {
		if err := vt.run(extension.ToggleViewCommand("vx.tab")); err != nil {
			t.Fatal(err)
		}
		if got := vt.layout.State().PanelVisible && vt.views.ActivePanel() == "vx.tab"; got != (want == 0) {
			t.Errorf("after toggle %d the tab is shown = %v", want, got)
		}
	}
	// a view with a condition that does not hold is not listed
	if got := reg.Views("vx.box", vt.ctxKeys); len(got) != 0 {
		t.Errorf("the view is listed while vx.ready is not set: %+v", got)
	}
	vt.ctxKeys.Set("vx.ready", true)
	if got := reg.Views("vx.box", vt.ctxKeys); len(got) != 1 {
		t.Errorf("the view is not listed while vx.ready holds: %+v", got)
	}
}

func TestShowingAViewActivatesTheExtensionAndBuildsItsContent(t *testing.T) {
	vt := newViewTab(t, nil)
	vt.scope.Start()
	if vt.scope.State("vx") != StateInactive {
		t.Fatalf("the extension is %v before a view is shown", vt.scope.State("vx"))
	}
	vt.show("vx.list")
	if vt.scope.State("vx") != StateActive {
		t.Fatalf("the extension is %v after its view was shown: %v", vt.scope.State("vx"), vt.scope.Failure("vx"))
	}
	if !vt.text1("side bar text") {
		t.Error("the content of the view is not on screen")
	}
	// Refresh builds again with new data
	vt.text = "changed"
	vt.log.handles["vx.list"].Refresh()
	vt.settle()
	if !vt.text1("changed") || vt.text1("side bar text") {
		t.Error("Refresh did not show the new text")
	}
	// the panel view is the tab
	vt.show("vx.tab")
	if !vt.text1("changed") {
		t.Error("the panel view has no content")
	}
}

func TestTheBoxGivesTheContentTheConstraintsOfTheViewAndClipsIt(t *testing.T) {
	var got rendering.BoxConstraints
	vt := newViewTab(t, nil)
	vt.text = ""
	f, _ := factory(viewsManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			_, err := api.Views().Provide("vx.list", extension.ViewContent{Build: func(widgets.BuildContext) widgets.Widget {
				return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
					got = c
					// wider and higher than the box: it must not paint outside
					return widgets.NewOverflowBox(widgets.NewSizedBox(widgets.NewColoredBox(gui.NewColor(0xFFFF0000), widgets.NewSizedBox(nil)),
						widgets.SizedBoxOpts{Width: foundation.Some(900.0), Height: foundation.Some(900.0)}), widgets.OverflowBoxOpts{})
				})
			}})
			return err
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.deferred = true
	vt = &viewTab{rig: r, tab: tb}
	vt.show("vx.list")
	if got.MaxWidth != 300 || got.MaxHeight != 200 {
		t.Errorf("the content got the constraints %v, want 300 by 200", got)
	}
	clips := vt.ts.Elements(gluttertest.ByType[*widgets.ClipRect]())
	if len(clips) == 0 {
		t.Fatal("the box has no clip")
	}
}

func TestTheBoxKeepsTheFocusAndLetsUnhandledKeysGoOn(t *testing.T) {
	f, _ := factory(viewsManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			_, err := api.Views().Provide("vx.list", extension.ViewContent{Build: func(widgets.BuildContext) widgets.Widget {
				// the content handles the letter a, and nothing else
				return widgets.NewFocus(widgets.NewSizedBox(nil), widgets.FocusOpts{Autofocus: true, DebugLabel: "inside",
					OnKeyEvent: func(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
						if e.LogicalKey() == services.LogicalKeyboardKeyKeyA {
							return widgets.KeyEventResultHandled
						}
						return widgets.KeyEventResultIgnored
					}})
			}})
			return err
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.deferred = true
	// the workbench above the box sees the keys the content did not handle
	var seen []services.LogicalKeyboardKey
	above := widgets.NewFocus(tb.scope.ViewBody("vx.list"), widgets.FocusOpts{
		CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true),
		OnKeyEvent: func(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
			if _, down := e.(*services.KeyDownEvent); down {
				seen = append(seen, e.LogicalKey())
			}
			return widgets.KeyEventResultIgnored
		}})
	v := &viewTab{rig: r, tab: tb}
	v.ts = uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 200}, above)
	v.settle()
	if got := tb.ctxKeys.Get("focusedView"); got != "vx.list" {
		t.Errorf("focusedView = %v while the content has the focus", got)
	}
	v.ts.SendKeyDownEvent(services.LogicalKeyboardKeyKeyA)
	v.ts.SendKeyDownEvent(services.LogicalKeyboardKeyEscape)
	if !slices.Equal(seen, []services.LogicalKeyboardKey{services.LogicalKeyboardKeyEscape}) {
		t.Errorf("the workbench saw the keys %v, want only Escape (the content handled a)", seen)
	}
	// the key is reset when the view goes off screen
	v.ts.PumpWidget(widgets.NewSizedBox(nil))
	v.settle()
	if got := tb.ctxKeys.Get("focusedView"); got == "vx.list" {
		t.Errorf("focusedView stayed %v after the view went", got)
	}
}

func TestDisablingTheExtensionTakesEverythingAway(t *testing.T) {
	vt := newViewTab(t, func(api extension.API, _ *viewTab) error {
		ch, err := api.Output().Channel("Vx Log")
		if err != nil {
			return err
		}
		ch.AppendLine("started")
		if err := api.Problems().Set("lint", "file:///work/one/a.go", []extension.Marker{{Message: "bad", StartLine: 2, StartColumn: 3}}); err != nil {
			return err
		}
		return api.Context().Set("vx.ready", true)
	})
	vt.scope.Start()
	vt.show("vx.list")
	if names := vt.channels.Names(); !slices.Contains(names, "Vx Log") {
		t.Fatalf("the channel is missing: %v", names)
	}
	if n := vt.markers.Stats().Total(); n != 1 {
		t.Fatalf("%d markers", n)
	}
	vt.pump()
	if vt.ctxKeys.Get("vx.ready") != true {
		t.Fatal("the context key is not set")
	}
	vt.disabled = []string{"vx"}
	vt.scope.Refresh()
	vt.pump()
	reg := vt.views.Registry()
	if _, ok := reg.Container("vx.box"); ok {
		t.Error("the container stayed")
	}
	if _, ok := reg.Container("vx.tab"); ok {
		t.Error("the tab stayed")
	}
	if _, ok := reg.View("vx.list"); ok {
		t.Error("the view stayed")
	}
	if slices.Contains(vt.channels.Names(), "Vx Log") {
		t.Errorf("the Output channel stayed: %v", vt.channels.Names())
	}
	if n := vt.markers.Stats().Total(); n != 0 {
		t.Errorf("%d markers stayed", n)
	}
	if vt.ctxKeys.Get("vx.ready") != nil {
		t.Error("the context key stayed")
	}
	if _, ok := vt.commands.Lookup(extension.ToggleViewCommand("vx.tab")); ok {
		t.Error("the toggle command stayed")
	}
	if slices.Contains(vt.menuCommands(extension.MenuView), extension.ToggleViewCommand("vx.tab")) {
		t.Error("the View menu entry stayed")
	}
	if vt.views.Active() == "vx.box" {
		t.Error("the side bar still shows the container that went")
	}
	// turned on again, it comes back
	vt.disabled = nil
	vt.scope.Refresh()
	if _, ok := reg.Container("vx.box"); !ok {
		t.Error("the container did not come back")
	}
}

func TestPanelViewBadgeTitleActionsAndContextKeys(t *testing.T) {
	vt := newViewTab(t, nil)
	vt.scope.Start()
	vt.show("vx.tab")
	changes := 0
	vt.views.OnChange(func() { changes++ })
	if n, _ := vt.scope.PanelBadge("vx.tab"); n != 0 {
		t.Errorf("the badge starts at %d", n)
	}
	vt.log.handles["vx.tab"].SetBadge(3, "3 tasks")
	vt.pump()
	if n, tip := vt.scope.PanelBadge("vx.tab"); n != 3 || tip != "3 tasks" || changes == 0 {
		t.Errorf("badge %d %q after %d changes", n, tip, changes)
	}
	// a side bar view has no badge on its tab
	vt.log.handles["vx.list"].SetBadge(5, "x")
	vt.pump()
	if n, _ := vt.scope.PanelBadge("vx.list"); n != 0 {
		t.Errorf("a side bar view got the badge %d", n)
	}

	var ran []string
	run := func(id string) { ran = append(ran, id) }
	count := func(id string, keys contextkey.Context) int {
		w := vt.scope.ViewActions(id, keys, run)
		if w == nil {
			return 0
		}
		ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 200, Height: 30}, w)
		return ts.Count(gluttertest.ByType[*widgets.GestureDetector]())
	}
	if got := count("vx.tab", vt.ctxKeys); got == 0 {
		t.Error("the panel view has no action while vx.running is not set")
	}
	before := count("vx.tab", vt.ctxKeys)
	vt.ctxKeys.Set("vx.running", true)
	if after := count("vx.tab", vt.ctxKeys); after <= before {
		t.Errorf("setting vx.running showed %d actions, was %d", after, before)
	}
	if vt.scope.ViewActions("workbench.view.explorer", vt.ctxKeys, run) != nil {
		t.Error("the Explorer got title actions from an extension")
	}
	if vt.scope.ViewBody("workbench.view.explorer") != nil {
		t.Error("the Explorer got a box")
	}
}

func TestTheStateOfAViewIsSavedWhenItGoesOffScreenAndRestoredWhenItComesBack(t *testing.T) {
	vt := newViewTab(t, nil)
	vt.scope.Start()
	vt.show("vx.list")
	vt.state = "go"
	// the box goes off screen: its state is saved
	vt.ts.PumpWidget(widgets.NewSizedBox(nil))
	if !slices.Equal(vt.log.saved, []string{"go"}) {
		t.Fatalf("saved = %v", vt.log.saved)
	}
	states := vt.scope.ViewStates()
	if string(states["vx.list"]) != `{"filter":"go"}` {
		t.Errorf("the scope keeps %q", states["vx.list"])
	}
	// the extension forgot (it is a new box with a state of its own): the box gives it the state back
	vt.state = ""
	vt.show("vx.list")
	if !slices.Equal(vt.log.restore, []string{"go"}) || vt.state != "go" {
		t.Errorf("restored = %v, state %q", vt.log.restore, vt.state)
	}
}

func TestTheStateSurvivesARestart(t *testing.T) {
	vt := newViewTab(t, nil)
	vt.scope.SetViewStates(map[string]json.RawMessage{"vx.tab": json.RawMessage(`{"filter":"saved"}`)})
	vt.scope.Start()
	vt.show("vx.tab")
	if vt.state != "saved" {
		t.Errorf("the view got the state %q", vt.state)
	}
	// what the scope hands to the window state holds the live state of a view that is on screen
	vt.state = "later"
	if got := string(vt.scope.ViewStates()["vx.tab"]); got != `{"filter":"later"}` {
		t.Errorf("ViewStates = %s", got)
	}
	// and the state of a view that saved nothing (nil) removes the old one
	vt.log.handles["vx.tab"].Dispose()
	vt.pump()
}

func TestAViewThatSavesNothingKeepsNoState(t *testing.T) {
	f, _ := factory(viewsManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			_, err := api.Views().Provide("vx.list", extension.ViewContent{
				Build:     func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) },
				SaveState: func() (any, error) { return nil, nil },
			})
			return err
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.deferred = true
	tb.scope.SetViewStates(map[string]json.RawMessage{"vx.list": json.RawMessage(`{"old":true}`)})
	vt := &viewTab{rig: r, tab: tb}
	vt.show("vx.list")
	if got := tb.scope.ViewStates(); len(got) != 0 {
		t.Errorf("the state is %v", got)
	}
}

func TestProvideErrors(t *testing.T) {
	tests := []struct {
		name  string
		do    func(api extension.API) error
		route string
	}{
		{"a view the manifest does not declare", func(api extension.API) error {
			_, err := api.Views().Provide("vx.other", extension.ViewContent{Build: func(widgets.BuildContext) widgets.Widget { return nil }})
			return err
		}, "viewsAPI.Provide.ExtensionViewNotDeclared"},
		{"a view of another extension", func(api extension.API) error {
			_, err := api.Views().Provide("vx.list", extension.ViewContent{Build: func(widgets.BuildContext) widgets.Widget { return nil }})
			return err
		}, "viewsAPI.Provide.ExtensionViewNotDeclared"},
		{"no Build function", func(api extension.API) error {
			_, err := api.Views().Provide("ext.v", extension.ViewContent{})
			return err
		}, "viewsAPI.Provide.ExtensionViewContentInvalid"},
		{"twice", func(api extension.API) error {
			c := extension.ViewContent{Build: func(widgets.BuildContext) widgets.Widget { return nil }}
			if _, err := api.Views().Provide("ext.v", c); err != nil {
				return err
			}
			_, err := api.Views().Provide("ext.v", c)
			return err
		}, "viewsAPI.Provide.ExtensionViewProvided"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := manifest("ext")
			m.Contributes.PanelViews = []extension.PanelViewSpec{{ID: "ext.v", Title: "V"}}
			var got error
			f, _ := factory(m, func(f *fake) {
				f.activate = func(_ context.Context, api extension.API) error { got = tt.do(api); return nil }
			})
			r := newRig(t, f)
			r.newTab("one").scope.Start()
			appErr, ok := errors.FromError(got)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", got, got)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestAPanicInTheContentFailsTheExtensionInThatScopeOnly(t *testing.T) {
	tests := []struct {
		name, tag, route string
		content          func() extension.ViewContent
	}{
		{"Build panics", "ExtensionViewBuildPanicked", "Scope.failView.ExtensionViewBuildPanicked", func() extension.ViewContent {
			return extension.ViewContent{Build: func(widgets.BuildContext) widgets.Widget { panic("boom") }}
		}},
		{"a guarded builder below Build panics", "ExtensionViewWidgetPanicked", "Scope.failView.ExtensionViewWidgetPanicked", func() extension.ViewContent {
			return extension.ViewContent{Build: func(widgets.BuildContext) widgets.Widget {
				return ui.NewBuilder(func(widgets.BuildContext) widgets.Widget { panic("deep boom") })
			}}
		}},
		{"SaveState panics", "ExtensionViewStatePanicked", "Scope.callSave.func1.ExtensionViewStatePanicked", func() extension.ViewContent {
			return extension.ViewContent{
				Build:     func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) },
				SaveState: func() (any, error) { panic("save boom") },
			}
		}},
		{"RestoreState panics", "ExtensionViewStatePanicked", "Scope.restoreView.func1.ExtensionViewStatePanicked", func() extension.ViewContent {
			return extension.ViewContent{
				Build:        func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) },
				RestoreState: func(json.RawMessage) error { panic("restore boom") },
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := viewsManifest()
			f, _ := factory(m, func(f *fake) {
				f.activate = func(_ context.Context, api extension.API) error {
					_, err := api.Views().Provide("vx.list", tt.content())
					return err
				}
			})
			healthy, _ := factory(manifest("emetos.healthy"), nil)
			r := newRig(t, f, healthy)
			tb, other := r.newTab("failing"), r.newTab("other")
			tb.deferred, other.deferred = true, true
			tb.scope.SetViewStates(map[string]json.RawMessage{"vx.list": json.RawMessage(`{}`)})
			tb.scope.Start()
			other.scope.Start()
			v := &viewTab{rig: r, tab: tb}
			v.show("vx.list")
			if tt.tag == "ExtensionViewStatePanicked" && strings.Contains(tt.name, "SaveState") {
				// the state is saved when the box goes off screen
				v.ts.PumpWidget(widgets.NewSizedBox(nil))
				v.settle()
			}
			if got := tb.scope.State("vx"); got != StateFailed {
				t.Fatalf("state = %v, want failed", got)
			}
			fl := tb.scope.Failure("vx")
			appErr, ok := errors.FromError(fl.Err)
			if !ok || appErr.Tag != tt.tag || !appErr.HasRoute(tt.route) {
				t.Errorf("failure %v; want tag %s and route %s", fl.Err, tt.tag, tt.route)
			}
			if fl.BoxText != "Vx failed" {
				t.Errorf("box text = %q", fl.BoxText)
			}
			if !strings.Contains(tb.log.String(), "an extension failed") {
				t.Errorf("the log does not have the failure:\n%s", tb.log.String())
			}
			// the other tab is untouched; it did not even show a view
			if got := other.scope.State("vx"); got == StateFailed {
				t.Error("the other tab failed too")
			}
			if got := tb.scope.State("emetos.healthy"); got == StateFailed {
				t.Error("the healthy extension failed")
			}
			// the box of the failed extension says so, with a Show Log button, and the view stays declared
			if tt.tag != "ExtensionViewStatePanicked" || strings.Contains(tt.name, "Restore") {
				v.show("vx.list")
				if !v.text1("Vx failed") || !v.ts.Exists(gluttertest.ByText("Show Log")) {
					t.Error("the box does not say that the extension failed")
				}
			}
			if _, ok := tb.views.Registry().Container("vx.box"); !ok {
				t.Error("the container of a failed extension went; its box has to say what happened")
			}
			if _, ok := tb.commands.Lookup(extension.ShowViewCommand("vx.box")); ok {
				t.Error("the command of the failed extension stayed")
			}
			// turning it off removes the views; turning it on again tries once more
			tb.disabled = []string{"vx"}
			tb.scope.Refresh()
			if _, ok := tb.views.Registry().Container("vx.box"); ok {
				t.Error("the container stayed after the extension was turned off")
			}
		})
	}
}

func TestShowRevealsAndFocusesAView(t *testing.T) {
	var focused []string
	vt := newViewTab(t, nil)
	vt.scope.opts.FocusView = func(id string) { focused = append(focused, id) }
	vt.scope.Start()
	vt.show("vx.list")
	vt.views.SetCollapsed("vx.list", true)
	vt.log.handles["vx.list"].Reveal(true)
	vt.pump()
	if vt.views.Active() != "vx.box" || vt.views.IsCollapsed("vx.list") || !slices.Equal(focused, []string{"vx.list"}) {
		t.Errorf("active %q, collapsed %v, focused %v", vt.views.Active(), vt.views.IsCollapsed("vx.list"), focused)
	}
	vt.log.handles["vx.tab"].Reveal(false)
	vt.pump()
	if vt.views.ActivePanel() != "vx.tab" || !vt.layout.State().PanelVisible {
		t.Errorf("active panel %q visible %v", vt.views.ActivePanel(), vt.layout.State().PanelVisible)
	}
}

func TestTheStateIsSavedWhenTheExtensionIsTurnedOffButNotWhenItFailed(t *testing.T) {
	vt := newViewTab(t, nil)
	vt.scope.Start()
	vt.show("vx.list")
	vt.state = "kept"
	vt.disabled = []string{"vx"}
	vt.scope.Refresh()
	if got := string(vt.scope.ViewStates()["vx.list"]); got != `{"filter":"kept"}` {
		t.Errorf("the state after turning the extension off = %q", got)
	}
	// a failed extension is not asked again
	vt2 := newViewTab(t, nil)
	vt2.scope.Start()
	vt2.show("vx.list")
	vt2.log.saved = nil
	vt2.scope.fail(vt2.scope.slots[0], errors.New("Cause", "boom"), "test")
	if len(vt2.log.saved) != 0 {
		t.Errorf("a failed extension was asked for its state: %v", vt2.log.saved)
	}
}
