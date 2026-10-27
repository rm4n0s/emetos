package quickinput

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

func TestHostShowsAndHides(t *testing.T) {
	r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	if r.ctl.Visible() {
		t.Fatal("visible at the start")
	}
	r.show(">", ShowOptions{})
	if !r.ctl.Visible() || len(r.labels()) == 0 {
		t.Fatalf("visible %v, labels %v", r.ctl.Visible(), r.labels())
	}
	if got := r.scopes.ScopeOf(r.primary(), r.keys).Get("inQuickOpen"); got != true {
		t.Errorf("inQuickOpen = %v", got)
	}
	r.press(services.LogicalKeyboardKeyEscape)
	if r.ctl.Visible() {
		t.Errorf("still visible")
	}
	if r.primary() != r.editor {
		t.Errorf("focus did not return to the editor: %v", r.primary())
	}
}

// scopeKey reads a context key in the scope of the focused node.
func (r *rig) scopeKey(key string) any { return r.scopes.ScopeOf(r.primary(), r.keys).Get(key) }

func TestHostCommandsModeFiltersAndHighlights(t *testing.T) {
	r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show(">", ShowOptions{})
	if r.primary().DebugLabel() != "InputBox" {
		t.Fatalf("the input does not have the focus: %v", r.primary().DebugLabel())
	}
	all := len(r.labels())
	if all < 100 {
		t.Fatalf("only %d commands listed", all)
	}
	for _, key := range []string{"inQuickOpen", "inQuickInput", "inputFocus", "inCommandsPicker"} {
		if r.scopeKey(key) != true {
			t.Errorf("%s = %v", key, r.scopeKey(key))
		}
	}
	if r.scopeKey("quickInputType") != "quickPick" {
		t.Errorf("quickInputType = %v", r.scopeKey("quickInputType"))
	}
	r.typeText("go to line")
	got := r.labels()
	if len(got) == 0 || got[0] != "Go to Line/Column..." {
		t.Fatalf("rows %v", got)
	}
	first := r.ctl.Picker().Rows()[0]
	if len(first.Label) == 0 || first.Item.Keybinding != "Ctrl+G" {
		t.Errorf("highlights %v key %q", first.Label, first.Item.Keybinding)
	}
	// the text of the row is drawn, and so are the key caps
	if !r.ts.Exists(gluttertest.ByKey(RowKey(0))) {
		t.Errorf("no row on screen")
	}
	if !r.ts.Exists(gluttertest.ByText("Ctrl")) || !r.ts.Exists(gluttertest.ByText("G")) {
		t.Errorf("the key caps of Ctrl+G are not on screen")
	}
	r.typeText("zzzz")
	if got := r.labels(); !reflect.DeepEqual(got, []string{"No matching commands"}) {
		t.Errorf("rows %v", got)
	}
}

func TestHostArrowKeysAndEnterRunTheCommandAfterTheFocusIsBack(t *testing.T) {
	r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	var focusAtRun *widgets.FocusNode
	for _, m := range r.commands.All() {
		if m.ID == "workbench.action.gotoOffset" {
			id := m.ID
			if _, err := r.commands.SetHandler(id, func(command.Context, any) error {
				focusAtRun = r.primary()
				r.ran = append(r.ran, id)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	r.show(">", ShowOptions{})
	r.typeText("go to")
	labels := r.labels()
	if len(labels) < 2 {
		t.Fatalf("rows %v", labels)
	}
	// Down moves to the second row, Up goes back, Up from the first wraps to the last
	r.press(services.LogicalKeyboardKeyArrowDown)
	if r.ctl.Picker().ActiveIndex() != 1 {
		t.Errorf("active %d after Down", r.ctl.Picker().ActiveIndex())
	}
	r.press(services.LogicalKeyboardKeyArrowUp)
	r.press(services.LogicalKeyboardKeyArrowUp)
	if r.ctl.Picker().ActiveIndex() != len(labels)-1 {
		t.Errorf("active %d after Up from the first", r.ctl.Picker().ActiveIndex())
	}
	for r.ctl.Picker().Rows()[r.ctl.Picker().ActiveIndex()].Item.ID != "workbench.action.gotoOffset" {
		r.press(services.LogicalKeyboardKeyArrowUp)
		if len(r.ran) > 100 {
			t.Fatal("never reached the command")
		}
	}
	r.press(services.LogicalKeyboardKeyEnter)
	if r.ctl.Visible() {
		t.Errorf("still visible")
	}
	if !reflect.DeepEqual(r.ran, []string{"workbench.action.gotoOffset"}) {
		t.Fatalf("ran %v", r.ran)
	}
	if focusAtRun != r.editor {
		t.Errorf("the command ran with the focus in %v, not in the editor", focusAtRun)
	}
	if r.history.Peek("workbench.action.gotoOffset") == 0 {
		t.Errorf("the command is not in the history")
	}
}

func TestHostEscapeHidesAndRestoresTheFocus(t *testing.T) {
	r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.LightModern())
	r.show(">", ShowOptions{})
	r.press(services.LogicalKeyboardKeyEscape)
	if r.ctl.Visible() || r.primary() != r.editor || len(r.ran) != 0 {
		t.Errorf("visible %v focus %v ran %v", r.ctl.Visible(), r.primary(), r.ran)
	}
	if r.ts.Exists(gluttertest.ByType[*kit.InputBox]()) {
		t.Errorf("the input is still on screen")
	}
	// it opens again, and the closeQuickOpen command closes it
	r.show(">", ShowOptions{})
	if err := r.commands.Run(command.Context{Ctx: t.Context(), Keys: r.keys}, "workbench.action.closeQuickOpen", nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if r.ctl.Visible() || r.primary() != r.editor {
		t.Errorf("visible %v focus %v", r.ctl.Visible(), r.primary())
	}
}

func TestHostClickOnARowRunsIt(t *testing.T) {
	r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show(">", ShowOptions{})
	r.typeText("go to line")
	r.ts.Tap(gluttertest.ByKey(RowKey(0)))
	r.settle()
	if r.ctl.Visible() || !reflect.DeepEqual(r.ran, []string{"workbench.action.gotoLine"}) {
		t.Errorf("visible %v ran %v", r.ctl.Visible(), r.ran)
	}
}

func TestHostClickOutsideAndFocusLossHide(t *testing.T) {
	t.Run("a click outside", func(t *testing.T) {
		r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
		r.show(">", ShowOptions{})
		r.ts.TapAt(ui.Offset{Dx: 40, Dy: 400})
		r.settle()
		if r.ctl.Visible() {
			t.Errorf("still visible")
		}
	})
	t.Run("the focus moving away", func(t *testing.T) {
		r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
		r.show(">", ShowOptions{})
		r.editor.RequestFocus()
		r.settle()
		if r.ctl.Visible() {
			t.Errorf("still visible")
		}
	})
}

func TestHostGoToLineMovesTheEditorWhileTypingAndEscapeRestores(t *testing.T) {
	r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show(":", ShowOptions{})
	if !r.ts.Exists(gluttertest.ByText("Type a line number to go to (from 1 to 4).", gluttertest.FindOpts{FindRichText: true})) {
		t.Errorf("the prompt is not on screen")
	}
	r.typeText("3")
	if !reflect.DeepEqual(r.goTo.revealed, [][2]int{{3, 1}}) || !r.goTo.highlighted {
		t.Errorf("revealed %v highlighted %v", r.goTo.revealed, r.goTo.highlighted)
	}
	if !r.ts.Exists(gluttertest.ByText("Press 'Enter' to go to line 3 or enter colon : to add a column number.", gluttertest.FindOpts{FindRichText: true})) {
		t.Errorf("the label of the line is not on screen")
	}
	r.press(services.LogicalKeyboardKeyEscape)
	if r.goTo.restored != 1 || r.goTo.highlighted || len(r.goTo.went) != 0 {
		t.Errorf("restored %d highlighted %v went %v", r.goTo.restored, r.goTo.highlighted, r.goTo.went)
	}
	// Enter goes to the position
	r.show(":", ShowOptions{})
	r.typeText("2:2")
	r.press(services.LogicalKeyboardKeyEnter)
	if r.ctl.Visible() || !reflect.DeepEqual(r.goTo.went, [][2]int{{2, 2}}) || r.primary() != r.editor {
		t.Errorf("visible %v went %v focus %v", r.ctl.Visible(), r.goTo.went, r.primary())
	}
}

func TestHostViewsMode(t *testing.T) {
	r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("view ", ShowOptions{})
	if r.labels()[0] != "Explorer" || r.ctl.Picker().Rows()[0].Separator != "Side Bar" {
		t.Fatalf("rows %v", r.labels())
	}
	if r.scopeKey("inViewsPicker") != true {
		t.Errorf("inViewsPicker = %v", r.scopeKey("inViewsPicker"))
	}
	r.typeText("prob")
	r.press(services.LogicalKeyboardKeyEnter)
	if r.ctl.Visible() || !reflect.DeepEqual(r.opened, []string{"container:workbench.panel.markers"}) {
		t.Errorf("visible %v opened %v", r.ctl.Visible(), r.opened)
	}
}

func TestHostHelpModeListsProvidersAndChoosingInsertsThePrefix(t *testing.T) {
	r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.show("?", ShowOptions{})
	got := r.labels()
	if !reflect.DeepEqual(got, []string{":", "::", ">", "view "}) {
		t.Fatalf("rows %v", got)
	}
	r.press(services.LogicalKeyboardKeyArrowDown)
	r.press(services.LogicalKeyboardKeyArrowDown)
	r.press(services.LogicalKeyboardKeyEnter)
	if !r.ctl.Visible() || r.ctl.Picker().Value() != ">" || r.ctl.Provider().Info().Prefix != ">" {
		t.Fatalf("visible %v value %q", r.ctl.Visible(), r.ctl.Picker().Value())
	}
	// the input shows the prefix and the commands are listed
	if len(r.labels()) < 100 {
		t.Errorf("%d commands", len(r.labels()))
	}
}

func TestHostEmptyValueShowsTheFallbackPlaceholder(t *testing.T) {
	r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.ctl.Configure(ControllerOptions{FallbackPlaceholder: "Type '?' for help"})
	r.show("", ShowOptions{})
	if !r.ts.Exists(gluttertest.ByText("Type '?' for help")) || len(r.labels()) != 0 {
		t.Errorf("placeholder %q rows %v", r.ctl.Picker().Placeholder(), r.labels())
	}
	// typing a prefix starts a mode
	r.typeText(">")
	if r.ctl.Provider() == nil || r.ctl.Provider().Info().Prefix != ">" {
		t.Errorf("provider %v", r.ctl.Provider())
	}
}

func TestHostScrollsToTheActiveRowAndWithTheWheel(t *testing.T) {
	r := newRig(t, ui.Size{Width: 800, Height: 300}, kit.DarkModern())
	r.show(">", ShowOptions{})
	rows := len(r.labels())
	for range 12 {
		r.press(services.LogicalKeyboardKeyArrowDown)
	}
	if r.ctl.Picker().ActiveIndex() != 12 {
		t.Fatalf("active %d", r.ctl.Picker().ActiveIndex())
	}
	// the list scrolled, so that the active row is on screen
	if !r.ts.Exists(gluttertest.ByKey(RowKey(12))) || r.ts.Exists(gluttertest.ByKey(RowKey(0))) {
		t.Errorf("row 12 shown %v, row 0 shown %v", r.ts.Exists(gluttertest.ByKey(RowKey(12))), r.ts.Exists(gluttertest.ByKey(RowKey(0))))
	}
	r.press(services.LogicalKeyboardKeyPageDown)
	if r.ctl.Picker().ActiveIndex() <= 12 {
		t.Errorf("active %d after Page Down", r.ctl.Picker().ActiveIndex())
	}
	// Ctrl+End goes to the last row; typing starts at the top again
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.ts.SendKeyEvent(services.LogicalKeyboardKeyEnd)
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	r.settle()
	if r.ctl.Picker().ActiveIndex() != rows-1 {
		t.Errorf("active %d, want the last (%d)", r.ctl.Picker().ActiveIndex(), rows-1)
	}
	r.typeText("line")
	if !r.ts.Exists(gluttertest.ByKey(RowKey(0))) {
		t.Errorf("the list is not at the top after typing")
	}
}

func TestHostRemoveFromRecentlyUsedButton(t *testing.T) {
	r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.history.Push("workbench.action.gotoLine")
	r.show(">", ShowOptions{})
	row := r.ctl.Picker().Rows()[0]
	if row.Item.ID != "workbench.action.gotoLine" || row.Separator != "recently used" {
		t.Fatalf("first row %q %q", row.Item.ID, row.Separator)
	}
	// the button is on the active row
	r.ts.Tap(gluttertest.ByType[*kit.IconButton]())
	r.settle()
	if r.history.Peek("workbench.action.gotoLine") != 0 || r.ctl.Picker().Rows()[0].Item.ID == "workbench.action.gotoLine" || !r.ctl.Visible() {
		t.Errorf("history %d first %q visible %v", r.history.Peek("workbench.action.gotoLine"), r.ctl.Picker().Rows()[0].Item.ID, r.ctl.Visible())
	}
}

// TestHostTakesTheKeysTypedBeforeItsFirstFrame is the regression test of the keys that went to the editor after Ctrl+P: the
// input box is built in the frame after the quick input is shown, and a key that comes first went to the editor, which had
// the focus. The keys go to the quick input from the moment it is shown, through the platform's key path (a key with its
// character), and are in the input box when it appears.
func TestHostTakesTheKeysTypedBeforeItsFirstFrame(t *testing.T) {
	typeKey := func(r *rig, c rune) {
		logical := services.LogicalKeyboardKey(c)
		r.ts.SendKeyDownEvent(logical, gluttertest.KeyOpts{Character: string(c)})
		r.ts.SendKeyUpEvent(logical)
	}
	for _, tc := range []struct {
		name  string
		start string
		keys  func(r *rig)
		want  string
	}{
		{"characters", "", func(r *rig) {
			for _, c := range "app.go" {
				typeKey(r, c)
			}
		}, "app.go"},
		{"after a prefix", ">", func(r *rig) {
			for _, c := range "abc" {
				typeKey(r, c)
			}
			r.ts.SendKeyEvent(services.LogicalKeyboardKeyBackspace)
		}, ">ab"},
		{"backspace on an empty value", "", func(r *rig) {
			r.ts.SendKeyEvent(services.LogicalKeyboardKeyBackspace)
			typeKey(r, 'x')
		}, "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
			r.ctl.Show(tc.start, ShowOptions{})
			// no frame has run and no microtask: the input box does not exist, and the focus change has not been applied
			tc.keys(r)
			if got := r.ctl.Picker().Value(); got != tc.want {
				t.Errorf("before the first frame the value is %q, want %q", got, tc.want)
			}
			r.settle()
			if got := r.ctl.Picker().Value(); got != tc.want {
				t.Errorf("after the frames the value is %q, want %q", got, tc.want)
			}
			if !r.inputField().FocusNode().HasPrimaryFocus() {
				t.Errorf("the input box has no focus after the frames")
			}
			if got := r.inputField().Controller().Text(); got != tc.want {
				t.Errorf("the input box shows %q, want %q", got, tc.want)
			}
			// typing goes on in the input box
			typeKey(r, 'z')
			r.settle()
			if got := r.inputField().Controller().Text(); got != tc.want+"z" {
				t.Errorf("typing after the frames: the input box shows %q, want %q", got, tc.want+"z")
			}
		})
	}
}

// inputField is the text field of the quick input that is on screen.
func (r *rig) inputField() *widgets.EditableText {
	return r.ts.Widget(gluttertest.ByType[*widgets.EditableText]()).(*widgets.EditableText)
}
