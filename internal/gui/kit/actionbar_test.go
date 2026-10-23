package kit

import (
	"fmt"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// testActions is a bar with a separator, a disabled and a checked action.
func testActions() []Action {
	return []Action{
		{ID: "new", Icon: fonts.CodiconAdd, Label: "New File"},
		{ID: "sep1", Separator: true},
		{ID: "save", Icon: fonts.CodiconSave, Label: "Save", Disabled: true},
		{ID: "wrap", Icon: fonts.CodiconWordWrap, Label: "Word Wrap", Checked: true},
		{ID: "text", Label: "Run"},
	}
}

// barRig is an action bar that records what it reports.
type barRig struct {
	ts      *gluttertest.Tester
	ctrl    *ActionBarController
	ran     []string
	escaped int
}

func newBarRig(t *testing.T, actions []Action, opts ActionBarOpts) *barRig {
	t.Helper()
	r := &barRig{ctrl: NewActionBarController()}
	opts.Controller = r.ctrl
	opts.OnRun = func(a Action) { r.ran = append(r.ran, a.ID) }
	if opts.OnEscape == nil {
		opts.OnEscape = func() { r.escaped++ }
	}
	r.ts = pumpSized(t, ui.Size{Width: 400, Height: 200}, NewActionBar(actions, opts))
	return r
}

func (r *barRig) focused() int { return r.ctrl.FocusedIndex() }

func (r *barRig) press(k services.LogicalKeyboardKey) { key(r.ts, k) }

// itemCenter is the center of the item at an index, for a horizontal bar.
func (r *barRig) itemCenter(i int) ui.Offset {
	return r.ts.Center(gluttertest.At(gluttertest.ByType[*actionItem](), i))
}

func TestActionBarClicks(t *testing.T) {
	cases := []struct {
		name string
		item int // index among the items that are not separators
		want string
	}{
		{"an icon action", 0, "[new]"},
		{"a disabled action", 1, "[]"},
		{"a checked action", 2, "[wrap]"},
		{"a text action", 3, "[text]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newBarRig(t, testActions(), ActionBarOpts{})
			r.ts.Tap(gluttertest.At(gluttertest.ByType[*actionItem](), tc.item))
			r.ts.Pump(time.Millisecond)
			if fmt.Sprint(r.ran) != tc.want {
				t.Errorf("ran %v, want %v", r.ran, tc.want)
			}
			if r.focused() != -1 {
				t.Errorf("a click moved the focus to %d", r.focused())
			}
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
	t.Run("the separator is not an item", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		if n := r.ts.Count(gluttertest.ByType[*actionItem]()); n != 4 {
			t.Errorf("%d items for 5 actions with one separator", n)
		}
	})
}

func TestActionBarKeyboard(t *testing.T) {
	left, right := services.LogicalKeyboardKeyArrowLeft, services.LogicalKeyboardKeyArrowRight
	up, down := services.LogicalKeyboardKeyArrowUp, services.LogicalKeyboardKeyArrowDown
	home, end := services.LogicalKeyboardKeyHome, services.LogicalKeyboardKeyEnd
	cases := []struct {
		name string
		opts ActionBarOpts
		keys []services.LogicalKeyboardKey
		want int // the focused action index
	}{
		{"Right skips the separator", ActionBarOpts{}, []services.LogicalKeyboardKey{right}, 2},
		{"Right twice reaches the checked action", ActionBarOpts{}, []services.LogicalKeyboardKey{right, right}, 3},
		{"Right focuses a disabled action by default", ActionBarOpts{}, []services.LogicalKeyboardKey{right}, 2},
		{"Right skips a disabled action when only enabled ones take the focus", ActionBarOpts{FocusOnlyEnabledItems: true}, []services.LogicalKeyboardKey{right}, 3},
		{"Right past the last wraps to the first", ActionBarOpts{}, []services.LogicalKeyboardKey{end, right}, 0},
		{"Left from the first wraps to the last", ActionBarOpts{}, []services.LogicalKeyboardKey{left}, 4},
		{"Left stops at the first without looping", ActionBarOpts{PreventLoopNavigation: true}, []services.LogicalKeyboardKey{left}, 0},
		{"Right stops at the last without looping", ActionBarOpts{PreventLoopNavigation: true}, []services.LogicalKeyboardKey{end, right}, 4},
		{"End", ActionBarOpts{}, []services.LogicalKeyboardKey{end}, 4},
		{"Home", ActionBarOpts{}, []services.LogicalKeyboardKey{end, home}, 0},
		{"End skips a disabled last action", ActionBarOpts{FocusOnlyEnabledItems: true}, []services.LogicalKeyboardKey{end, left}, 3},
		{"Up and Down do nothing in a horizontal bar", ActionBarOpts{}, []services.LogicalKeyboardKey{down, up}, 0},
		{"Down in a vertical bar", ActionBarOpts{Orientation: ActionsVertical}, []services.LogicalKeyboardKey{down}, 2},
		{"Up in a vertical bar wraps", ActionBarOpts{Orientation: ActionsVertical}, []services.LogicalKeyboardKey{up}, 4},
		{"Left and Right do nothing in a vertical bar", ActionBarOpts{Orientation: ActionsVertical}, []services.LogicalKeyboardKey{right, left}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newBarRig(t, testActions(), tc.opts)
			r.ctrl.Focus()
			r.ts.Pump(time.Millisecond)
			if r.focused() != 0 {
				t.Fatalf("Focus focused item %d", r.focused())
			}
			for _, k := range tc.keys {
				r.press(k)
			}
			if r.focused() != tc.want {
				t.Errorf("focused item %d, want %d", r.focused(), tc.want)
			}
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestActionBarRunsOnRelease(t *testing.T) {
	enter, space := services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeySpace
	t.Run("Enter runs the focused action when the key is released", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		r.ctrl.Focus()
		r.ts.Pump(time.Millisecond)
		r.ts.SendKeyDownEvent(enter)
		r.ts.Pump(time.Millisecond)
		if len(r.ran) != 0 {
			t.Fatalf("ran %v on the press", r.ran)
		}
		r.ts.SendKeyUpEvent(enter)
		r.ts.Pump(time.Millisecond)
		if fmt.Sprint(r.ran) != "[new]" {
			t.Errorf("ran %v", r.ran)
		}
	})
	t.Run("Space runs the action", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		r.ctrl.FocusIndex(3)
		r.ts.Pump(time.Millisecond)
		r.press(space)
		if fmt.Sprint(r.ran) != "[wrap]" {
			t.Errorf("ran %v", r.ran)
		}
	})
	t.Run("a repeat does not run it again", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		r.ctrl.Focus()
		r.ts.Pump(time.Millisecond)
		r.ts.SendKeyDownEvent(enter)
		r.ts.SendKeyRepeatEvent(enter)
		r.ts.SendKeyRepeatEvent(enter)
		r.ts.SendKeyUpEvent(enter)
		r.ts.Pump(time.Millisecond)
		if fmt.Sprint(r.ran) != "[new]" {
			t.Errorf("ran %v", r.ran)
		}
	})
	t.Run("a release without a press does nothing", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		r.ctrl.Focus()
		r.ts.Pump(time.Millisecond)
		r.ts.SendKeyUpEvent(enter)
		r.ts.Pump(time.Millisecond)
		if len(r.ran) != 0 {
			t.Errorf("ran %v", r.ran)
		}
	})
	t.Run("a disabled action does not run", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		r.ctrl.FocusIndex(2)
		r.ts.Pump(time.Millisecond)
		r.press(enter)
		if len(r.ran) != 0 {
			t.Errorf("ran %v", r.ran)
		}
	})
	t.Run("Enter without the focus in the bar does nothing", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		r.press(enter)
		if len(r.ran) != 0 {
			t.Errorf("ran %v", r.ran)
		}
	})
}

func TestActionBarEscape(t *testing.T) {
	t.Run("Escape calls OnEscape and gives the focus up", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		r.ctrl.FocusIndex(3)
		r.ts.Pump(time.Millisecond)
		r.press(services.LogicalKeyboardKeyEscape)
		if r.escaped != 1 || r.focused() != -1 {
			t.Errorf("escaped %d times, focused item %d", r.escaped, r.focused())
		}
	})
	t.Run("without OnEscape the key goes on to the widgets above", func(t *testing.T) {
		var seen int
		node := widgets.NewFocusNode(widgets.FocusNodeOpts{})
		defer node.Dispose()
		ctrl := NewActionBarController()
		bar := NewActionBar(testActions(), ActionBarOpts{Controller: ctrl})
		parent := widgets.NewFocus(bar, widgets.FocusOpts{FocusNode: node, OnKeyEvent: func(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
			if _, ok := e.(*services.KeyDownEvent); ok && e.LogicalKey() == services.LogicalKeyboardKeyEscape {
				seen++
			}
			return widgets.KeyEventResultIgnored
		}})
		ts := pumpSized(t, ui.Size{Width: 400, Height: 200}, parent)
		ctrl.Focus()
		ts.Pump(time.Millisecond)
		key(ts, services.LogicalKeyboardKeyEscape)
		if seen != 1 || ctrl.FocusedIndex() != 0 {
			t.Errorf("the parent saw Escape %d times; focused item %d", seen, ctrl.FocusedIndex())
		}
	})
	t.Run("OnEscape that moves the focus is respected", func(t *testing.T) {
		other := widgets.NewFocusNode(widgets.FocusNodeOpts{})
		defer other.Dispose()
		ctrl := NewActionBarController()
		bar := NewActionBar(testActions(), ActionBarOpts{Controller: ctrl, OnEscape: func() { other.RequestFocus() }})
		field := widgets.NewFocus(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{}), widgets.FocusOpts{FocusNode: other})
		ts := pumpSized(t, ui.Size{Width: 400, Height: 200}, widgets.NewColumn([]widgets.Widget{bar, field}))
		ctrl.Focus()
		ts.Pump(time.Millisecond)
		key(ts, services.LogicalKeyboardKeyEscape)
		if !other.HasFocus() || ctrl.FocusedIndex() != -1 {
			t.Errorf("other has focus: %v, focused item %d", other.HasFocus(), ctrl.FocusedIndex())
		}
	})
}

func TestActionBarRovingFocus(t *testing.T) {
	// A button, the bar and a button: Tab goes into the bar once, to the item that is in the tab order,
	// and leaves it with the next Tab, however many items there are.
	before := widgets.NewFocusNode(widgets.FocusNodeOpts{})
	after := widgets.NewFocusNode(widgets.FocusNodeOpts{})
	defer before.Dispose()
	defer after.Dispose()
	ctrl := NewActionBarController()
	bar := NewActionBar(testActions(), ActionBarOpts{Controller: ctrl})
	ts := pumpSized(t, ui.Size{Width: 400, Height: 200}, widgets.NewRow([]widgets.Widget{
		NewButton("Before", ButtonOpts{FocusNode: before, OnPressed: func() {}}), bar,
		NewButton("After", ButtonOpts{FocusNode: after, OnPressed: func() {}}),
	}))
	before.RequestFocus()
	ts.Pump(time.Millisecond)
	tab := services.LogicalKeyboardKeyTab
	key(ts, tab)
	if ctrl.FocusedIndex() != 0 {
		t.Fatalf("Tab went to item %d, want the first", ctrl.FocusedIndex())
	}
	key(ts, tab)
	if !after.HasFocus() || ctrl.FocusedIndex() != -1 {
		t.Fatalf("the second Tab: after has the focus %v, item %d", after.HasFocus(), ctrl.FocusedIndex())
	}
	// the arrows move inside the bar; the item that has the focus last is where Tab comes back to
	ctrl.FocusIndex(3)
	ts.Pump(time.Millisecond)
	after.RequestFocus()
	ts.Pump(time.Millisecond)
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyShiftLeft)
	ts.SendKeyDownEvent(tab)
	ts.SendKeyUpEvent(tab)
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyShiftLeft)
	ts.Pump(time.Millisecond)
	if ctrl.FocusedIndex() != 3 {
		t.Errorf("Shift+Tab went to item %d, want the one that had the focus (3)", ctrl.FocusedIndex())
	}
}

func TestActionBarTooltips(t *testing.T) {
	t.Run("an icon action shows its label after the delay", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		r.ts.Hover(r.itemCenter(0))
		r.ts.Pump(DefaultHoverDelay - time.Millisecond)
		if shown(r.ts, "New File") {
			t.Fatal("shown early")
		}
		r.ts.Pump(2 * time.Millisecond)
		if !shown(r.ts, "New File") {
			t.Error("not shown")
		}
	})
	t.Run("moving to the next item shows its tooltip at once", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		r.ts.Hover(r.itemCenter(0))
		r.ts.Pump(DefaultHoverDelay + time.Millisecond)
		r.ts.Hover(r.itemCenter(2))
		r.ts.Pump(time.Millisecond)
		if !shown(r.ts, "Word Wrap") || shown(r.ts, "New File") {
			t.Errorf("Word Wrap shown %v, New File shown %v", shown(r.ts, "Word Wrap"), shown(r.ts, "New File"))
		}
	})
	t.Run("a text action has no tooltip unless it has one", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		r.ts.Hover(r.itemCenter(3))
		r.ts.Pump(2 * DefaultHoverDelay)
		// the label is on the item; a hover would show it a second time
		if n := r.ts.Count(gluttertest.ByText("Run")); n != 1 {
			t.Errorf("the label is shown %d times", n)
		}
	})
	t.Run("an action's Tooltip", func(t *testing.T) {
		r := newBarRig(t, []Action{{ID: "run", Label: "Run", Tooltip: "Run the file (F5)"}}, ActionBarOpts{})
		r.ts.Hover(r.itemCenter(0))
		r.ts.Pump(2 * DefaultHoverDelay)
		if !shown(r.ts, "Run the file (F5)") {
			t.Error("not shown")
		}
	})
	t.Run("the keyboard shows the tooltip of the item that gets the focus", func(t *testing.T) {
		r := newBarRig(t, testActions(), ActionBarOpts{})
		r.ctrl.Focus()
		r.ts.Pump(time.Millisecond)
		r.press(services.LogicalKeyboardKeyArrowRight)
		if !shown(r.ts, "Save") {
			t.Fatal("the tooltip of the item that got the focus is not shown")
		}
		r.press(services.LogicalKeyboardKeyArrowRight)
		if shown(r.ts, "Save") || !shown(r.ts, "Word Wrap") {
			t.Errorf("Save shown %v, Word Wrap shown %v", shown(r.ts, "Save"), shown(r.ts, "Word Wrap"))
		}
	})
}

func TestActionBarLayout(t *testing.T) {
	t.Run("horizontal items are 22 px squares and a separator 9 px by 26", func(t *testing.T) {
		r := newBarRig(t, testActions()[:3], ActionBarOpts{})
		size := r.ts.Size(gluttertest.ByType[*ActionBar]())
		if size.Width != 22+9+22 || size.Height != 26 {
			t.Errorf("size %v", size)
		}
	})
	t.Run("a vertical bar stacks the items", func(t *testing.T) {
		r := newBarRig(t, testActions()[:3], ActionBarOpts{Orientation: ActionsVertical})
		size := r.ts.Size(gluttertest.ByType[*ActionBar]())
		if size.Width != 22 || size.Height != 22+1+2*4+22 {
			t.Errorf("size %v", size)
		}
	})
}

// Ported from VS Code: src/vs/base/test/browser/actionbar.test.ts ("prepareActions()"), with the
// cases of the separators that its code handles added.
func TestPrepareActions(t *testing.T) {
	a := func(id string) Action { return Action{ID: id, Label: id} }
	sep := SeparatorAction()
	ids := func(actions []Action) string {
		out := ""
		for _, x := range actions {
			if x.Separator {
				out += "|"
			} else {
				out += x.ID
			}
		}
		return out
	}
	cases := []struct {
		name string
		in   []Action
		want string
	}{
		{"VS Code's test", []Action{sep, sep, a("3"), sep, sep, a("6"), sep}, "3|6"},
		{"empty", nil, ""},
		{"only separators", []Action{sep, sep}, ""},
		{"no separators", []Action{a("1"), a("2")}, "12"},
		{"a leading separator", []Action{sep, a("1")}, "1"},
		{"a trailing separator", []Action{a("1"), sep}, "1"},
		{"a separator between", []Action{a("1"), sep, a("2")}, "1|2"},
		{"several between", []Action{a("1"), sep, sep, sep, a("2")}, "1|2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(PrepareActions(tc.in)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
