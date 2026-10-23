package workbench

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// harness is a workbench on screen with the services it needs.
type harness struct {
	t        *testing.T
	ts       *gluttertest.Tester
	layout   *Layout
	commands *command.Registry
	keys     *contextkey.Scope
	scopes   *kit.FocusScopes
	store    *settings.Store
	dialogs  *kit.DialogController
	editor   *widgets.FocusNode
	// menuBar is the controller of the menu bar, when the test gave the workbench one.
	menuBar *kit.MenuBarController
}

// editorStandIn is a focusable box with a text, standing for the editor.
func editorStandIn(node *widgets.FocusNode) widgets.Widget {
	return widgets.NewFocus(widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		colors := kit.ThemeOf(bc)
		return widgets.NewColoredBox(colors.EditorBackground, widgets.NewPadding(painting.NewEdgeInsetsAll(12),
			widgets.NewText("package main\n\nfunc main() {\n    println(\"hello\")\n}", widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.EditorForeground)})))
	}), widgets.FocusOpts{FocusNode: node, Autofocus: true, DebugLabel: "EditorStandIn"})
}

// newHarness shows a workbench of a size in a theme; tune changes the options before it is shown.
func newHarness(t *testing.T, size ui.Size, colors kit.Colors, state LayoutState, extra map[string]any, tune func(*Options)) *harness {
	t.Helper()
	paths := testutil.Isolate(t)
	// the settings decide where the side bar is and whether the activity bar and the status bar are shown,
	// so a state that differs from the defaults is given as settings
	merged := map[string]any{}
	for k, v := range extra {
		merged[k] = v
	}
	if state.SideBarPosition == SideBarRight {
		merged["workbench.sideBar.location"] = "right"
	}
	if !state.ActivityBarVisible {
		merged["workbench.activityBar.location"] = "hidden"
	}
	if !state.StatusBarVisible {
		merged["workbench.statusBar.visible"] = false
	}
	extra = merged
	h := &harness{
		t: t, layout: NewLayout(state), commands: command.DefaultRegistry(), keys: contextkey.NewRoot(),
		scopes: kit.NewFocusScopes(), dialogs: kit.NewDialogController(),
		store: testutil.Settings(t, paths, extra), editor: widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "editor"}),
	}
	opts := Options{
		Layout: h.layout, Commands: h.commands, Keys: h.keys, Scopes: h.scopes, Settings: h.store, Dialogs: h.dialogs,
		Editor: editorStandIn(h.editor),
	}
	if tune != nil {
		tune(&opts)
	}
	h.ts = testutil.New(t, size)
	h.ts.PumpWidget(widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				return kit.NewTheme(colors, New(opts), kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	h.settle()
	return h
}

// settle lets the frames, the focus changes and the posted work run.
func (h *harness) settle() {
	h.t.Helper()
	for range 4 {
		h.ts.Pump(20 * time.Millisecond)
	}
}

// run runs a command the way a key does and lets the frames pass.
func (h *harness) run(id string) {
	h.t.Helper()
	if err := h.commands.Run(command.Context{Ctx: h.t.Context(), Keys: h.keys, Source: command.SourceAPI}, id, nil); err != nil {
		h.t.Fatalf("%s: %v", id, err)
	}
	h.settle()
}

// shown says whether the widget of a part is on screen.
func (h *harness) shown(p Part) bool {
	return len(h.ts.Elements(gluttertest.ByKey(PartKey(p)))) > 0
}

// rect is the place of the widget of a part on screen.
func (h *harness) rect(p Part) ui.Rect {
	h.t.Helper()
	f := gluttertest.ByKey(PartKey(p))
	return ui.NewRectFromLTWH(h.ts.TopLeft(f).Dx, h.ts.TopLeft(f).Dy, h.ts.Size(f).Width, h.ts.Size(f).Height)
}

// key reads a context key.
func (h *harness) key(name string) any { return h.keys.Get(name) }

var goldenThemes = []struct {
	name   string
	colors func() kit.Colors
}{{"dark", kit.DarkModern}, {"light", kit.LightModern}}

// focused is the part that has the focus, or -1 when none of them has.
func (h *harness) focused() Part {
	h.t.Helper()
	ctx := h.ts.Element(gluttertest.ByKey(PartKey(PartEditor), gluttertest.FindOpts{IncludeOffstage: true}))
	for n := widgets.WidgetsBindingOf(ctx).FocusManager().PrimaryFocus(); n != nil; n = n.Parent() {
		for _, p := range partOrder {
			if n.DebugLabel() == p.String() {
				return p
			}
		}
	}
	return -1
}

// Widget is a widget, for the tests' short lists.
type Widget = widgets.Widget

// statusText is a text for a status bar item.
func statusText(text string) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 8), widgets.NewCenter(widgets.NewText(text)))
}

// widgetsFocus is the node that has the primary focus.
func widgetsFocus(ctx widgets.BuildContext) *widgets.FocusNode {
	return widgets.WidgetsBindingOf(ctx).FocusManager().PrimaryFocus()
}

// gluttertestEmpty is a page with nothing in it.
func gluttertestEmpty() widgets.Widget { return widgets.NewSizedBox(nil) }
