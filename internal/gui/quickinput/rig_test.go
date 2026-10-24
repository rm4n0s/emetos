package quickinput

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// rig is a quick input over a stand-in for the editor, with the registry of the commands of Emetos,
// the default keymap and the providers of the command palette, help and views.
type rig struct {
	t        *testing.T
	ts       *gluttertest.Tester
	ctl      *Controller
	registry *Registry
	commands *command.Registry
	menus    *command.MenuRegistry
	views    *command.ViewRegistry
	keys     *contextkey.Scope
	resolver *keybinding.Resolver
	history  *CommandsHistory
	editor   *widgets.FocusNode
	// ran are the commands the palette ran, in order.
	ran []string
	// opened are the views and containers the views mode opened.
	opened []string
	// goTo is the fake editor of the go to line modes.
	goTo *fakeEditor
	// scopes is where the host registers the scope of the input.
	scopes *kit.FocusScopes
}

// newRig shows a quick input in a window of a size and a theme.
func newRig(t *testing.T, size ui.Size, colors kit.Colors) *rig {
	t.Helper()
	r := &rig{
		t: t, registry: NewRegistry(), commands: command.DefaultRegistry(), views: command.DefaultViews(),
		keys: contextkey.NewRoot(), history: NewCommandsHistory(50), scopes: kit.NewFocusScopes(),
		editor: widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "editor"}), goTo: newFakeEditor("one\ntwo\n\tthree\nfour"),
	}
	r.menus = command.DefaultMenus(r.commands)
	r.resolver = keybinding.NewResolver(keybinding.DefaultRegistry().Defaults(), nil, nil)
	for _, m := range r.commands.All() {
		if m.Title != "" {
			id := m.ID
			if _, err := r.commands.SetHandler(id, func(command.Context, any) error { r.ran = append(r.ran, id); return nil }); err != nil {
				t.Fatal(err)
			}
		}
	}
	r.ctl = NewController(ControllerOptions{
		Registry: r.registry,
		Invoker:  func() contextkey.Context { return r.keys },
		After: func(f func()) {
			// the command runs after the frames that give the focus back
			r.ts.Pump(20 * time.Millisecond)
			r.ts.Pump(20 * time.Millisecond)
			f()
		},
	})
	r.registry.MustRegister(
		NewCommandsProvider(CommandsOptions{
			Commands: r.commands, Menus: r.menus, Context: r.ctl.InvokerContext, History: r.history,
			Keybinding: func(id string, ctx contextkey.Context) string {
				if kb, ok := r.resolver.LookupPrimaryKeybinding(id, ctx, false); ok && kb.Keybinding != nil {
					return kb.Keybinding.Label()
				}
				return ""
			},
			Run: func(id string) error {
				return r.commands.Run(command.Context{Ctx: t.Context(), Keys: r.keys}, id, nil)
			},
		}),
		NewHelpProvider(r.registry, func(id string) string {
			if kb, ok := r.resolver.LookupPrimaryKeybinding(id, r.keys, false); ok && kb.Keybinding != nil {
				return kb.Keybinding.Label()
			}
			return ""
		}),
		NewGoToLineProvider(GoToLinePrefix, func() GoToLineEditor { return r.goTo }),
		NewGoToLineProvider(GoToOffsetPrefix, func() GoToLineEditor { return r.goTo }),
		NewViewsProvider(ViewsOptions{
			Views:         r.views,
			Context:       r.ctl.InvokerContext,
			OpenContainer: func(id string) { r.opened = append(r.opened, "container:"+id) },
			OpenView:      func(id string) { r.opened = append(r.opened, "view:"+id) },
			Channels:      func() []string { return []string{"Emetos", "Tasks"} },
			OpenChannel:   func(name string) { r.opened = append(r.opened, "channel:"+name) },
		}),
	)
	r.ts = testutil.New(t, size)
	r.ts.PumpWidget(widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				host := NewHost(r.standIn(), HostOptions{Controller: r.ctl, Keys: r.keys, Scopes: r.scopes, Commands: r.commands, TopInset: 30})
				return kit.NewTheme(colors, host, kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	r.settle()
	return r
}

// standIn is a focusable page with some text, standing for the editor.
func (r *rig) standIn() widgets.Widget {
	return widgets.NewFocus(widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		colors := kit.ThemeOf(bc)
		return widgets.NewColoredBox(colors.EditorBackground, widgets.NewPadding(painting.NewEdgeInsetsAll(12),
			widgets.NewText("package main\n\nfunc main() {\n    println(\"hello\")\n}", widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.EditorForeground)})))
	}), widgets.FocusOpts{FocusNode: r.editor, Autofocus: true, DebugLabel: "EditorStandIn"})
}

// settle lets the frames, the focus changes and the posted work run.
func (r *rig) settle() {
	r.t.Helper()
	for range 4 {
		r.ts.Pump(20 * time.Millisecond)
	}
}

// show opens the quick input with a value and lets the frames pass.
func (r *rig) show(value string, opts ShowOptions) {
	r.t.Helper()
	r.ctl.Show(value, opts)
	r.settle()
}

// primary is the node that has the focus.
func (r *rig) primary() *widgets.FocusNode {
	ctx := r.ts.Element(gluttertest.ByType[*Host]())
	return widgets.WidgetsBindingOf(ctx).FocusManager().PrimaryFocus()
}

// labels are the labels of the rows of the picker.
func (r *rig) labels() []string {
	var out []string
	for _, row := range r.ctl.Picker().Rows() {
		out = append(out, row.Item.Label)
	}
	return out
}

// press sends a key press and lets the frames pass.
func (r *rig) press(key services.LogicalKeyboardKey) {
	r.t.Helper()
	r.ts.SendKeyEvent(key)
	r.settle()
}

// typeText types text into the focused input.
func (r *rig) typeText(text string) {
	r.t.Helper()
	r.ts.EnterText(gluttertest.ByType[*widgets.EditableText](), r.ctl.Picker().Value()+text)
	r.settle()
}

// fakeEditor is an editor for the go to line modes that records what they do.
type fakeEditor struct {
	lines       []string
	revealed    [][2]int
	highlighted bool
	went        [][2]int
	restored    int
	cleared     int
}

func newFakeEditor(text string) *fakeEditor { return &fakeEditor{lines: strings.Split(text, "\n")} }

func (e *fakeEditor) LineCount() int { return len(e.lines) }
func (e *fakeEditor) LineContent(line int) string {
	if line < 1 || line > len(e.lines) {
		return ""
	}
	return e.lines[line-1]
}
func (e *fakeEditor) TabSize() int { return 4 }
func (e *fakeEditor) Length() int  { return len(strings.Join(e.lines, "\n")) }
func (e *fakeEditor) PositionAt(offset int) (int, int) {
	for i, l := range e.lines {
		if offset <= len(l) {
			return i + 1, offset + 1
		}
		offset -= len(l) + 1
	}
	last := len(e.lines)
	return last, len(e.lines[last-1]) + 1
}
func (e *fakeEditor) Reveal(line, col int) {
	e.revealed = append(e.revealed, [2]int{line, col})
	e.highlighted = true
}
func (e *fakeEditor) ClearHighlight()  { e.highlighted = false; e.cleared++ }
func (e *fakeEditor) Go(line, col int) { e.went = append(e.went, [2]int{line, col}) }
func (e *fakeEditor) Restore()         { e.restored++ }
