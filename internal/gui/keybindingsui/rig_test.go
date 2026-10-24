package keybindingsui

import (
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// pump is a time shorter than the blink of the caret, so that a golden is taken with the caret in the same state.
const pump = 10 * time.Millisecond

// rig is a keybindings editor in a window over a model that its actions change in memory: the rows come from the
// built-in keybindings and the user's, which the actions edit as keybindings.json would be.
type rig struct {
	t    *testing.T
	ts   *gluttertest.Tester
	ctl  *Controller
	keys *contextkey.Scope
	// the model's inputs
	defaults []keybinding.Item
	user     []keybinding.Item
	commands []keybinding.CommandInfo
	model    *keybinding.Model
	changed  []func()

	dialogs *kit.DialogController
	// what the editor asked for
	calls   []string
	errs    []error
	states  []editorgroup.KeybindingsViewState
	copied  []string
	opened  int
	failing error
	// reselectAfter, when set, runs after an action (the owner has edited the file and tells the editor).
	reselectAfter func()

	scopes *kit.FocusScopes
	// recorder is the recorder the editor handed over, nil while the recording mode is off: key presses go to it.
	recorder keybinding.Recorder
	records  int
}

type rigOpts struct {
	colors kit.Colors
	size   ui.Size
	// userText is the keybindings.json the rows are made with; defaults, when nil, is a small set.
	userText string
	defaults []keybinding.Item
	commands []keybinding.CommandInfo
	state    editorgroup.KeybindingsViewState
}

// smallDefaults are a few built-in keybindings the tests know.
func smallDefaults(t testing.TB) []keybinding.Item {
	t.Helper()
	item := func(key, cmd, when string) keybinding.Item {
		kb, err := keybinding.ParseKeybinding(key)
		if err != nil {
			t.Fatal(err)
		}
		var expr contextkey.Expr
		if when != "" {
			if expr, err = contextkey.Parse(when); err != nil {
				t.Fatal(err)
			}
		}
		return keybinding.NewItem(kb, cmd, nil, expr, true)
	}
	return []keybinding.Item{
		item("ctrl+c", "editor.action.clipboardCopyAction", ""),
		item("ctrl+x", "editor.action.clipboardCutAction", "textInputFocus"),
		item("ctrl+x", "other.cut", "editorFocus"),
		item("shift+alt+down", "editor.action.copyLinesDownAction", "editorTextFocus && !editorReadonly"),
		item("shift+alt+up", "editor.action.copyLinesUpAction", "editorTextFocus && !editorReadonly"),
		item("ctrl+k ctrl+c", "editor.action.addCommentLine", "editorTextFocus && !editorReadonly"),
		item("ctrl+shift+p", "workbench.action.showCommands", ""),
	}
}

func smallCommands() []keybinding.CommandInfo {
	return []keybinding.CommandInfo{
		{ID: "editor.action.clipboardCopyAction", Title: "Copy"},
		{ID: "editor.action.clipboardCutAction", Title: "Cut"},
		{ID: "other.cut", Title: "Cut Other"},
		{ID: "editor.action.copyLinesDownAction", Title: "Copy Line Down"},
		{ID: "editor.action.copyLinesUpAction", Title: "Copy Line Up"},
		{ID: "editor.action.addCommentLine", Title: "Add Line Comment"},
		{ID: "workbench.action.showCommands", Title: "Show All Commands", Category: "View"},
		{ID: "workbench.action.openSettings", Title: "Settings"},
		{ID: "cursorLeft"},
	}
}

func newRig(t *testing.T, o rigOpts) *rig {
	t.Helper()
	if o.colors == (kit.Colors{}) {
		o.colors = kit.DarkModern()
	}
	if o.size == (ui.Size{}) {
		o.size = ui.Size{Width: 1000, Height: 600}
	}
	r := &rig{t: t, ctl: NewController(), keys: contextkey.NewRoot(), dialogs: kit.NewDialogController()}
	r.defaults, r.commands = o.defaults, o.commands
	if r.defaults == nil {
		r.defaults = smallDefaults(t)
	}
	if r.commands == nil {
		r.commands = smallCommands()
	}
	r.setUser(o.userText)

	ts := testutil.New(t, o.size)
	r.ts = ts
	scopes := kit.NewFocusScopes()
	r.scopes = scopes
	ts.PumpWidget(widgets.NewWidgetsApp(o.colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(rs widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				page := widgets.NewContainer(r.editor(o, scopes), widgets.ContainerOpts{Color: foundation.Some(o.colors.EditorBackground)})
				host := kit.NewDialogHost(page, r.dialogs)
				return kit.NewTheme(o.colors, host, kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: rs}}})
		},
	}))
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	// the recording mode of the dispatcher: while a recorder is set, every key press goes to it
	binding := widgets.WidgetsBindingOf(ts.Element(gluttertest.ByType[*Editor]()))
	handle := binding.FocusManager().AddEarlyKeyEventHandler(func(e services.KeyEvent) widgets.KeyEventResult {
		chord, ok := chordOf(e, binding.Keyboard())
		if !ok || r.recorder == nil {
			return widgets.KeyEventResultIgnored
		}
		r.recorder.Record(chord)
		return widgets.KeyEventResultHandled
	})
	t.Cleanup(func() { binding.FocusManager().RemoveEarlyKeyEventHandler(handle) })
	return r
}

// setUser makes the model from the text of a keybindings.json.
func (r *rig) setUser(text string) {
	r.t.Helper()
	items, problems := keybinding.ParseUserFile(text)
	if len(problems) != 0 {
		r.t.Fatalf("problems %v", problems)
	}
	r.user = items
	r.model = keybinding.NewModel(r.defaults, r.user, r.commands)
}

// reload makes the model again and tells the editor.
func (r *rig) reload() {
	r.model = keybinding.NewModel(r.defaults, r.user, r.commands)
	for _, f := range r.changed {
		f()
	}
}

func (r *rig) editor(o rigOpts, scopes *kit.FocusScopes) widgets.Widget {
	return NewEditor(Options{
		Model: func() *keybinding.Model { return r.model },
		Subscribe: func(f func()) func() {
			r.changed = append(r.changed, f)
			return func() { r.changed = nil }
		},
		Actions: Actions{
			Define: func(row keybinding.Row, add bool, key string, overwrite []keybinding.Row) error {
				verb := "change"
				if add {
					verb = "add"
				}
				call := verb + " " + row.Command + " " + key
				for _, c := range overwrite {
					call += " overwrite:" + c.Command
				}
				r.calls = append(r.calls, call)
				r.afterAction()
				return r.failing
			},
			Remove: func(row keybinding.Row) error {
				r.calls = append(r.calls, "remove "+row.Command+" "+row.Key)
				r.afterAction()
				return r.failing
			},
			Reset: func(row keybinding.Row) error {
				r.calls = append(r.calls, "reset "+row.Command)
				r.afterAction()
				return r.failing
			},
			SetWhen: func(row keybinding.Row, when string) error {
				r.calls = append(r.calls, "when "+row.Command+" "+when)
				r.afterAction()
				return r.failing
			},
		},
		State:      o.state,
		OnState:    func(s editorgroup.KeybindingsViewState) { r.states = append(r.states, s) },
		OnOpenJSON: func() { r.opened++ },
		OnError:    func(err error) { r.errs = append(r.errs, err) },
		CopyText:   func(s string) { r.copied = append(r.copied, s) },
		Record: func(rec keybinding.Recorder) func() {
			r.recorder = rec
			r.records++
			return func() { r.recorder = nil }
		},
		KeyLabel: func(command string) string {
			return map[string]string{"keybindings.editor.defineKeybinding": "Enter", "keybindings.editor.recordSearchKeys": "Alt+K"}[command]
		},
		Dialogs: r.dialogs, Keys: r.keys, Scopes: scopes, CodeFont: testutil.MonoFamily, Controller: r.ctl,
	})
}

// chordOf is the chord of a key event, for the keys the tests press.
func chordOf(e services.KeyEvent, kb *services.HardwareKeyboard) (keybinding.Chord, bool) {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return keybinding.Chord{}, false
	}
	c := keybinding.Chord{Ctrl: kb.IsControlPressed(), Shift: kb.IsShiftPressed(), Alt: kb.IsAltPressed(), Meta: kb.IsMetaPressed()}
	k := e.LogicalKey()
	switch k {
	case services.LogicalKeyboardKeyControl, services.LogicalKeyboardKeyControlLeft:
		c.Key, c.Ctrl = keybinding.Ctrl, true
	case services.LogicalKeyboardKeyShift, services.LogicalKeyboardKeyShiftLeft:
		c.Key, c.Shift = keybinding.Shift, true
	case services.LogicalKeyboardKeyAlt, services.LogicalKeyboardKeyAltLeft:
		c.Key, c.Alt = keybinding.Alt, true
	case services.LogicalKeyboardKeyEnter:
		c.Key = keybinding.Enter
	case services.LogicalKeyboardKeyEscape:
		c.Key = keybinding.Escape
	case services.LogicalKeyboardKeyBackspace:
		c.Key = keybinding.Backspace
	default:
		id := k.KeyID()
		switch {
		case id >= 'a' && id <= 'z':
			c.Key = keybinding.KeyA + keybinding.Key(id-'a')
		case id >= 'A' && id <= 'Z':
			c.Key = keybinding.KeyA + keybinding.Key(id-'A')
		case id >= '0' && id <= '9':
			c.Key = keybinding.Digit0 + keybinding.Key(id-'0')
		default:
			return keybinding.Chord{}, false
		}
	}
	return c, true
}

// press presses and releases a key with the modifiers held.
func (r *rig) press(key services.LogicalKeyboardKey, mods ...services.LogicalKeyboardKey) {
	r.t.Helper()
	for _, m := range mods {
		r.ts.SendKeyDownEvent(m)
	}
	r.ts.SendKeyDownEvent(key)
	r.ts.SendKeyUpEvent(key)
	for i := len(mods) - 1; i >= 0; i-- {
		r.ts.SendKeyUpEvent(mods[i])
	}
	r.ts.Pump(time.Millisecond)
}

// letter is the logical key of a letter or a digit.
func letter(c rune) services.LogicalKeyboardKey { return services.LogicalKeyboardKey(c) }

var (
	ctrl  = services.LogicalKeyboardKeyControl
	shift = services.LogicalKeyboardKeyShift
	alt   = services.LogicalKeyboardKeyAlt
	enter = services.LogicalKeyboardKeyEnter
	esc   = services.LogicalKeyboardKeyEscape
)

func (r *rig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("errors: %v", errs)
	}
	if len(r.errs) != 0 {
		r.t.Errorf("the editor reported %v", r.errs)
	}
}

// row finds the widget of the row of a command (the first one when it has several) by its id.
func (r *rig) rowID(command, key string) string {
	r.t.Helper()
	for _, row := range r.model.Rows() {
		if row.Command == command && (key == "" || row.Key == key) {
			return row.ID
		}
	}
	r.t.Fatalf("no row %s %s", command, key)
	return ""
}

func (r *rig) rowFinder(command, key string) gluttertest.Finder {
	return gluttertest.ByKey(RowKey(r.rowID(command, key)))
}

func (r *rig) has(command, key string) bool {
	for _, row := range r.model.Rows() {
		if row.Command == command && (key == "" || row.Key == key) {
			return r.ts.Exists(gluttertest.ByKey(RowKey(row.ID)))
		}
	}
	return false
}

// shown lists the commands of the rows that are built, in order of the table.
func (r *rig) shown() []string {
	var out []string
	for _, row := range r.model.Fetch(r.ctl.Query()) {
		if r.ts.Exists(gluttertest.ByKey(RowKey(row.Row.ID))) {
			out = append(out, row.Row.Command+"|"+row.Row.Key)
		}
	}
	return out
}

// search puts a text in the search box.
func (r *rig) search(text string) {
	r.t.Helper()
	r.ctl.SetQuery(text)
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
}

// tapRow selects a row with a click on it.
func (r *rig) tapRow(command, key string) {
	r.t.Helper()
	r.ts.TapAt(r.ts.Center(r.rowFinder(command, key)), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
}

// key is the value of a context key where the keyboard focus is.
func (r *rig) key(name string) any {
	focus := widgets.WidgetsBindingOf(r.ts.Element(gluttertest.ByType[*Editor]())).FocusManager().PrimaryFocus()
	return r.scopes.ScopeOf(focus, r.keys).Get(name)
}

// type types the characters of a text with the keys of the keyboard, for a box that takes text.
func (r *rig) typeIn(f gluttertest.Finder, text string) {
	r.t.Helper()
	r.ts.EnterText(f, text)
	r.ts.Pump(time.Millisecond)
}

func (r *rig) afterAction() {
	if r.reselectAfter != nil {
		r.reselectAfter()
	}
}

var errFailed = errors.New("EditFailed", "the edit failed")

// textOfKey is the text of the Text widget under a key.
func textOfKey(r *rig, key string) string {
	r.t.Helper()
	w := r.ts.Widgets(gluttertest.Descendant(gluttertest.ByKey(foundation.NewValueKey(key)), gluttertest.ByType[*widgets.Text]()))
	if len(w) != 1 {
		r.t.Fatalf("%d texts under %s", len(w), key)
	}
	return w[0].(*widgets.Text).PlainText()
}

// nilBox is an empty widget, to take the editor out of the tree.
func nilBox() widgets.Widget { return widgets.NewSizedBox(nil) }

// widgetsRichText is the type of rich text widgets.
type widgetsRichText = widgets.RichText
