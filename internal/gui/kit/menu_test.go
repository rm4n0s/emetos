package kit

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// testMenuItems has a separator, a disabled item, a checked one, a submenu and mnemonics. In the
// cleaned list the indexes are: 0 New, 1 Open, 2 separator, 3 Save (disabled), 4 Word Wrap
// (checked), 5 Recent (a submenu), 6 Exit.
func testMenuItems() []MenuItem {
	return []MenuItem{
		{ID: "new", Label: "&New File", Keybinding: "Ctrl+N"},
		{ID: "open", Label: "&Open...", Keybinding: "Ctrl+O"},
		SeparatorItem(),
		{ID: "save", Label: "&Save", Disabled: true, Keybinding: "Ctrl+S"},
		{ID: "wrap", Label: "Word &Wrap", Checked: true},
		SubmenuItem("&Recent", []MenuItem{{ID: "r1", Label: "One"}, {ID: "r2", Label: "Two"}, {ID: "r3", Label: "T&hree"}}),
		{ID: "exit", Label: "E&xit"},
	}
}

// menuRig is a menu that records what it reports.
type menuRig struct {
	ts        *gluttertest.Tester
	handle    *MenuHandle
	ran       []string
	cancelled int
}

func newMenuRig(t *testing.T, items []MenuItem, opts MenuOpts) *menuRig {
	t.Helper()
	r := &menuRig{handle: NewMenuHandle()}
	opts.Handle = r.handle
	opts.OnRun = func(it MenuItem) { r.ran = append(r.ran, it.ID) }
	opts.OnCancel = func() { r.cancelled++ }
	opts.Autofocus = true
	r.ts = pumpSized(t, ui.Size{Width: 500, Height: 400}, NewMenu(items, opts))
	r.ts.Pump(time.Millisecond) // the focus is requested after the first frame
	return r
}

func (r *menuRig) press(keys ...services.LogicalKeyboardKey) {
	for _, k := range keys {
		key(r.ts, k)
	}
}

// rowCenter is the center of the row that shows a label.
func (r *menuRig) center(label string) ui.Offset { return r.ts.Center(gluttertest.ByText(label)) }

func (r *menuRig) path() string { return fmt.Sprint(r.handle.Path()) }

func TestMenuKeyboardNavigation(t *testing.T) {
	down, up := services.LogicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyArrowUp
	home, end := services.LogicalKeyboardKeyHome, services.LogicalKeyboardKeyEnd
	pgUp, pgDown := services.LogicalKeyboardKeyPageUp, services.LogicalKeyboardKeyPageDown
	cases := []struct {
		name string
		keys []services.LogicalKeyboardKey
		want string
	}{
		{"nothing is focused at first", nil, "[-1]"},
		{"Down focuses the first item", []services.LogicalKeyboardKey{down}, "[0]"},
		{"Up focuses the last item", []services.LogicalKeyboardKey{up}, "[6]"},
		{"Down skips the separator", []services.LogicalKeyboardKey{down, down, down}, "[4]"},
		{"Down skips the disabled item", []services.LogicalKeyboardKey{down, down, down}, "[4]"},
		{"Down reaches the submenu item", []services.LogicalKeyboardKey{down, down, down, down}, "[5]"},
		{"Down past the last wraps to the first", []services.LogicalKeyboardKey{down, down, down, down, down, down}, "[0]"},
		{"Up from the first wraps to the last", []services.LogicalKeyboardKey{down, up}, "[6]"},
		{"Up skips the disabled item and the separator", []services.LogicalKeyboardKey{down, down, down, up, up}, "[0]"},
		{"Home", []services.LogicalKeyboardKey{up, home}, "[0]"},
		{"End", []services.LogicalKeyboardKey{down, end}, "[6]"},
		{"PageUp goes to the first item on Linux", []services.LogicalKeyboardKey{up, pgUp}, "[0]"},
		{"PageDown goes to the last item on Linux", []services.LogicalKeyboardKey{down, pgDown}, "[6]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMenuRig(t, testMenuItems(), MenuOpts{})
			r.press(tc.keys...)
			if got := r.path(); got != tc.want {
				t.Errorf("focus path %s, want %s", got, tc.want)
			}
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestMenuSelectFirst(t *testing.T) {
	r := newMenuRig(t, testMenuItems(), MenuOpts{SelectFirst: true})
	if got := r.path(); got != "[0]" {
		t.Errorf("focus path %s, want [0]", got)
	}
}

func TestMenuRunKeys(t *testing.T) {
	down, enter, space := services.LogicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeySpace
	numEnter := services.LogicalKeyboardKeyNumpadEnter
	cases := []struct {
		name string
		keys []services.LogicalKeyboardKey
		want string
	}{
		{"Enter runs the focused item", []services.LogicalKeyboardKey{down, enter}, "[new]"},
		{"Space runs it", []services.LogicalKeyboardKey{down, down, space}, "[open]"},
		{"the numpad's Enter runs it", []services.LogicalKeyboardKey{down, numEnter}, "[new]"},
		{"Enter with nothing focused does nothing", []services.LogicalKeyboardKey{enter}, "[]"},
		{"the checked item runs", []services.LogicalKeyboardKey{down, down, down, enter}, "[wrap]"},
		{"Enter on a submenu opens it and runs nothing", []services.LogicalKeyboardKey{down, down, down, down, enter}, "[]"},
		{"Enter in a submenu runs its item", []services.LogicalKeyboardKey{down, down, down, down, enter, enter}, "[r1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMenuRig(t, testMenuItems(), MenuOpts{})
			r.press(tc.keys...)
			if got := fmt.Sprint(r.ran); got != tc.want {
				t.Errorf("ran %s, want %s", got, tc.want)
			}
		})
	}
}

func TestMenuItemOnRun(t *testing.T) {
	var own []string
	items := []MenuItem{{ID: "a", Label: "A", OnRun: func() { own = append(own, "a") }}}
	r := newMenuRig(t, items, MenuOpts{})
	r.press(services.LogicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyEnter)
	if fmt.Sprint(own) != "[a]" || fmt.Sprint(r.ran) != "[a]" {
		t.Errorf("own %v, menu %v", own, r.ran)
	}
}

func TestMenuSubmenuKeyboard(t *testing.T) {
	down, right, left := services.LogicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyArrowRight, services.LogicalKeyboardKeyArrowLeft
	esc, enter := services.LogicalKeyboardKeyEscape, services.LogicalKeyboardKeyEnter
	toRecent := []services.LogicalKeyboardKey{down, down, down, down} // focuses Recent
	cases := []struct {
		name          string
		keys          []services.LogicalKeyboardKey
		wantPath      string
		wantOpen      int
		wantCancelled int
		wantRan       string
	}{
		{"Right opens the submenu with its first item focused", append(toRecent, right), "[5 0]", 5, 0, "[]"},
		{"Enter opens it too", append(toRecent, enter), "[5 0]", 5, 0, "[]"},
		{"Down moves inside the submenu", append(toRecent, right, down), "[5 1]", 5, 0, "[]"},
		{"the submenu wraps around by itself", append(toRecent, right, down, down, down), "[5 0]", 5, 0, "[]"},
		{"Left closes the submenu", append(toRecent, right, left), "[5]", -1, 0, "[]"},
		{"Escape closes the submenu and not the menu", append(toRecent, right, esc), "[5]", -1, 0, "[]"},
		{"after closing, Down continues in the menu", append(toRecent, right, left, down), "[6]", -1, 0, "[]"},
		{"Escape in the menu cancels it", []services.LogicalKeyboardKey{down, esc}, "[0]", -1, 1, "[]"},
		{"Right on an item without a submenu does nothing", []services.LogicalKeyboardKey{down, right}, "[0]", -1, 0, "[]"},
		{"Left in the menu does nothing", []services.LogicalKeyboardKey{down, left}, "[0]", -1, 0, "[]"},
		{"an item of the submenu runs and the submenu closes", append(toRecent, right, down, enter), "[5]", -1, 0, "[r2]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMenuRig(t, testMenuItems(), MenuOpts{})
			r.press(tc.keys...)
			if got := r.path(); got != tc.wantPath {
				t.Errorf("focus path %s, want %s", got, tc.wantPath)
			}
			if got := r.handle.OpenSubmenu(); got != tc.wantOpen {
				t.Errorf("open submenu %d, want %d", got, tc.wantOpen)
			}
			if r.cancelled != tc.wantCancelled {
				t.Errorf("cancelled %d times, want %d", r.cancelled, tc.wantCancelled)
			}
			if got := fmt.Sprint(r.ran); got != tc.wantRan {
				t.Errorf("ran %s, want %s", got, tc.wantRan)
			}
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestMenuMnemonics(t *testing.T) {
	letter := func(c string) services.LogicalKeyboardKey {
		return map[string]services.LogicalKeyboardKey{
			"n": services.LogicalKeyboardKeyKeyN, "o": services.LogicalKeyboardKeyKeyO, "s": services.LogicalKeyboardKeyKeyS,
			"w": services.LogicalKeyboardKeyKeyW, "r": services.LogicalKeyboardKeyKeyR, "x": services.LogicalKeyboardKeyKeyX,
			"z": services.LogicalKeyboardKeyKeyZ, "h": services.LogicalKeyboardKeyKeyH,
		}[c]
	}
	cases := []struct {
		name     string
		mnemonic bool
		keys     []string
		wantRan  string
		wantPath string
	}{
		{"the mnemonic runs the item", true, []string{"n"}, "[new]", "[0]"},
		{"another one", true, []string{"x"}, "[exit]", "[6]"},
		{"a mnemonic in the middle of a label", true, []string{"w"}, "[wrap]", "[4]"},
		{"a disabled item does not take its mnemonic", true, []string{"s"}, "[]", "[-1]"},
		{"a key without an item does nothing", true, []string{"z"}, "[]", "[-1]"},
		{"the mnemonic of a submenu opens it", true, []string{"r"}, "[]", "[5 0]"},
		{"the mnemonic in a submenu runs its item", true, []string{"r", "h"}, "[r3]", "[5]"},
		{"mnemonics off", false, []string{"n"}, "[]", "[-1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMenuRig(t, testMenuItems(), MenuOpts{EnableMnemonics: tc.mnemonic})
			for _, c := range tc.keys {
				k := letter(c)
				r.ts.SendKeyDownEvent(k)
				r.ts.SendKeyUpEvent(k)
				r.ts.Pump(time.Millisecond)
			}
			if got := fmt.Sprint(r.ran); got != tc.wantRan {
				t.Errorf("ran %s, want %s", got, tc.wantRan)
			}
			if got := r.path(); got != tc.wantPath {
				t.Errorf("focus path %s, want %s", got, tc.wantPath)
			}
		})
	}
}

func TestMenuMnemonicsSharedByItems(t *testing.T) {
	items := []MenuItem{{ID: "a1", Label: "&Alpha"}, {ID: "a2", Label: "&Apple"}, {ID: "b", Label: "&Beta"}, {ID: "a3", Label: "&Avocado", Disabled: true}}
	r := newMenuRig(t, items, MenuOpts{EnableMnemonics: true})
	var seen []int
	for range 4 {
		r.press(services.LogicalKeyboardKeyKeyA)
		seen = append(seen, r.handle.FocusedIndex())
	}
	// the key only focuses while several items share it, and cycles through the enabled ones
	if !reflect.DeepEqual(seen, []int{0, 1, 0, 1}) || len(r.ran) != 0 {
		t.Errorf("focused %v, ran %v; want [0 1 0 1] and nothing run", seen, r.ran)
	}
	r.press(services.LogicalKeyboardKeyKeyB)
	if fmt.Sprint(r.ran) != "[b]" {
		t.Errorf("ran %v, want [b]", r.ran)
	}
}

func TestMenuMnemonicKeyNeedsNoControl(t *testing.T) {
	r := newMenuRig(t, testMenuItems(), MenuOpts{EnableMnemonics: true})
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.press(services.LogicalKeyboardKeyKeyN)
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	if len(r.ran) != 0 {
		t.Errorf("Ctrl+N ran %v", r.ran)
	}
}

func TestMenuPointer(t *testing.T) {
	t.Run("a click runs the item", func(t *testing.T) {
		r := newMenuRig(t, testMenuItems(), MenuOpts{})
		r.ts.Tap(gluttertest.ByText("Open..."))
		r.ts.Pump(time.Millisecond)
		if fmt.Sprint(r.ran) != "[open]" {
			t.Errorf("ran %v", r.ran)
		}
	})
	t.Run("a click on a disabled item does nothing", func(t *testing.T) {
		r := newMenuRig(t, testMenuItems(), MenuOpts{})
		r.ts.Tap(gluttertest.ByText("Save"))
		r.ts.Pump(time.Millisecond)
		if len(r.ran) != 0 {
			t.Errorf("ran %v", r.ran)
		}
	})
	t.Run("a click on a submenu item opens it and runs nothing", func(t *testing.T) {
		r := newMenuRig(t, testMenuItems(), MenuOpts{})
		r.ts.Tap(gluttertest.ByText("Recent"))
		r.ts.Pump(time.Millisecond)
		if len(r.ran) != 0 || r.handle.OpenSubmenu() != 5 || r.path() != "[5 0]" {
			t.Errorf("ran %v, open %d, path %s", r.ran, r.handle.OpenSubmenu(), r.path())
		}
	})
	t.Run("a click on an item in the submenu runs it", func(t *testing.T) {
		r := newMenuRig(t, testMenuItems(), MenuOpts{})
		r.ts.Tap(gluttertest.ByText("Recent"))
		r.ts.Pump(time.Millisecond)
		r.ts.Tap(gluttertest.ByText("Two"))
		r.ts.Pump(time.Millisecond)
		if fmt.Sprint(r.ran) != "[r2]" {
			t.Errorf("ran %v", r.ran)
		}
	})
}

func TestMenuHoverFocus(t *testing.T) {
	r := newMenuRig(t, testMenuItems(), MenuOpts{})
	steps := []struct {
		name string
		do   func()
		want string
	}{
		{"moving over an item focuses it", func() { r.ts.Hover(r.center("New File")); r.ts.Hover(r.center("New File").Add(ui.Offset{Dx: 1})) }, "[0]"},
		{"moving to another item", func() { r.ts.Hover(r.center("Open...")) }, "[1]"},
		{"a disabled item takes the highlight off", func() { r.ts.Hover(r.center("Save")) }, "[-1]"},
		{"the checked item", func() { r.ts.Hover(r.center("Word Wrap")) }, "[4]"},
		{"a pointer that rests does not change the focus", func() {
			r.ts.Hover(r.center("Word Wrap").Add(ui.Offset{Dx: 0, Dy: 0}))
			r.ts.Hover(r.center("Word Wrap"))
		}, "[4]"},
		{"the pointer leaving the menu takes the highlight off", func() { r.ts.Hover(ui.Offset{Dx: 450, Dy: 390}) }, "[-1]"},
	}
	for _, st := range steps {
		st.do()
		r.ts.Pump(time.Millisecond)
		if got := r.path(); got != st.want {
			t.Errorf("%s: focus path %s, want %s", st.name, got, st.want)
		}
	}
}

// A menu that appears under a pointer that rests must not focus the item under it (VS Code's
// #110594 and #148158): the pointer has to move.
func TestMenuStationaryPointerDoesNotChangeFocus(t *testing.T) {
	r := newMenuRig(t, testMenuItems(), MenuOpts{SelectFirst: true})
	at := r.center("Open...")
	r.ts.Hover(at) // the first event moves the pointer into the menu...
	r.ts.Pump(time.Millisecond)
	if got := r.path(); got != "[1]" {
		t.Fatalf("focus path %s, want [1]", got)
	}
	r.press(services.LogicalKeyboardKeyArrowUp)
	// ...and an event that does not move it leaves the keyboard's focus alone
	r.ts.Hover(at)
	r.ts.Pump(time.Millisecond)
	if got := r.path(); got != "[0]" {
		t.Errorf("a stationary hover moved the focus to %s, want [0]", got)
	}
}

func TestMenuSubmenuHoverDelays(t *testing.T) {
	r := newMenuRig(t, testMenuItems(), MenuOpts{})
	r.ts.Hover(r.center("Recent"))
	r.ts.Hover(r.center("Recent").Add(ui.Offset{Dx: 1}))
	r.ts.Pump(MenuSubmenuShowDelay - time.Millisecond)
	if r.handle.OpenSubmenu() != -1 {
		t.Fatal("the submenu opened before the delay")
	}
	r.ts.Pump(2 * time.Millisecond)
	if r.handle.OpenSubmenu() != 5 {
		t.Fatal("the submenu did not open after 250 ms")
	}
	if !r.ts.Exists(gluttertest.ByText("Two")) {
		t.Error("the submenu does not show its items")
	}
	// moving the pointer into the submenu keeps it open
	r.ts.Hover(r.center("One"))
	r.ts.Hover(r.center("One").Add(ui.Offset{Dx: 1}))
	r.ts.Pump(MenuSubmenuHideDelay + time.Second)
	if r.handle.OpenSubmenu() != 5 {
		t.Error("the submenu closed while the pointer was in it")
	}
	// and moving it to another item of the parent closes it after 750 ms
	r.ts.Hover(r.center("Exit"))
	r.ts.Hover(r.center("Exit").Add(ui.Offset{Dx: 1}))
	r.ts.Pump(MenuSubmenuHideDelay - 10*time.Millisecond)
	if r.handle.OpenSubmenu() != 5 {
		t.Fatal("the submenu closed before 750 ms")
	}
	r.ts.Pump(20 * time.Millisecond)
	if r.handle.OpenSubmenu() != -1 || r.ts.Exists(gluttertest.ByText("Two")) {
		t.Error("the submenu stayed open 750 ms after the pointer left its item")
	}
	if r.handle.FocusedIndex() != 6 {
		t.Errorf("focus %d, want 6", r.handle.FocusedIndex())
	}
}

func TestMenuSubmenuHoverCancelledByLeaving(t *testing.T) {
	r := newMenuRig(t, testMenuItems(), MenuOpts{})
	r.ts.Hover(r.center("Recent"))
	r.ts.Hover(r.center("Recent").Add(ui.Offset{Dx: 1}))
	r.ts.Pump(100 * time.Millisecond)
	r.ts.Hover(r.center("Exit"))
	r.ts.Pump(time.Second)
	if r.handle.OpenSubmenu() != -1 {
		t.Error("the submenu opened although the pointer left its item before the delay")
	}
}

func TestMenuSubmenuSwitch(t *testing.T) {
	items := []MenuItem{
		SubmenuItem("A", []MenuItem{{ID: "a1", Label: "A one"}}),
		SubmenuItem("B", []MenuItem{{ID: "b1", Label: "B one"}}),
	}
	r := newMenuRig(t, items, MenuOpts{})
	r.ts.Tap(gluttertest.ByText("A"))
	r.ts.Pump(time.Millisecond)
	if !r.ts.Exists(gluttertest.ByText("A one")) {
		t.Fatal("the first submenu is not open")
	}
	r.ts.Tap(gluttertest.ByText("B"))
	r.ts.Pump(time.Millisecond)
	if r.ts.Exists(gluttertest.ByText("A one")) || !r.ts.Exists(gluttertest.ByText("B one")) {
		t.Error("opening the second submenu did not replace the first")
	}
}

func TestMenuEmptySubmenu(t *testing.T) {
	r := newMenuRig(t, []MenuItem{SubmenuItem("Empty", nil)}, MenuOpts{})
	r.ts.Tap(gluttertest.ByText("Empty"))
	r.ts.Pump(time.Millisecond)
	if !r.ts.Exists(gluttertest.ByText("(empty)")) {
		t.Fatal("an empty submenu shows no placeholder")
	}
	// the placeholder cannot be focused or run
	r.press(services.LogicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyEnter)
	if len(r.ran) != 0 || r.path() != "[0 -1]" {
		t.Errorf("ran %v, focus path %s", r.ran, r.path())
	}
}

func TestMenuCleansSeparators(t *testing.T) {
	items := []MenuItem{SeparatorItem(), {ID: "a", Label: "A"}, SeparatorItem(), SeparatorItem(), {ID: "b", Label: "B"}, SeparatorItem()}
	r := newMenuRig(t, items, MenuOpts{})
	if n := len(r.ts.Widget(gluttertest.ByType[*Menu]()).(*Menu).Items()); n != 6 {
		t.Fatalf("Items() has %d, the caller's list must stay as given", n)
	}
	r.press(services.LogicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyArrowDown)
	// A, one separator, B: two downs reach B at index 2
	if got := r.path(); got != "[2]" {
		t.Errorf("focus path %s, want [2]", got)
	}
}

func TestMenuTextAndState(t *testing.T) {
	r := newMenuRig(t, testMenuItems(), MenuOpts{})
	for _, label := range []string{"New File", "Ctrl+N", "Open...", "Save", "Word Wrap", "Recent", "Exit"} {
		if !r.ts.Exists(gluttertest.ByText(label)) {
			t.Errorf("no %q", label)
		}
	}
	if r.ts.Exists(gluttertest.ByText("&New File")) {
		t.Error("the mnemonic marker is shown with the mnemonics off")
	}
}

func TestMenuMnemonicLabelsAreUnderlined(t *testing.T) {
	r := newMenuRig(t, testMenuItems(), MenuOpts{EnableMnemonics: true})
	txt := r.ts.Widget(gluttertest.ByText("New File")).(*widgets.Text)
	if txt.PlainText() != "New File" {
		t.Errorf("text %q", txt.PlainText())
	}
	if txt.TextSpan() == nil {
		t.Error("the label with a mnemonic is plain text, nothing is underlined")
	}
}

func TestMenuScrollsWhenTooTall(t *testing.T) {
	var items []MenuItem
	for i := range 30 {
		items = append(items, MenuItem{ID: fmt.Sprint(i), Label: fmt.Sprintf("Item %d", i)})
	}
	r := newMenuRig(t, items, MenuOpts{MaxHeight: foundation.Some(200.0)})
	size := r.ts.Size(gluttertest.ByType[*Menu]())
	if size.Height > 200 {
		t.Errorf("the menu is %v px high, past its limit of 200", size.Height)
	}
	if size.Width < menuMinWidth || size.Width > 300 {
		t.Errorf("the scrolling menu is %v px wide, it should be as wide as its widest item (at least 160)", size.Width)
	}
	r.press(services.LogicalKeyboardKeyEnd)
	r.ts.Pump(time.Millisecond)
	// the last item is in view after End
	last := r.ts.Center(gluttertest.ByText("Item 29"))
	if last.Dy < 0 || last.Dy > 200 {
		t.Errorf("the last item is at y=%v, out of the view", last.Dy)
	}
}

// A submenu does not open for a pointer that does not move (VS Code's #110594 and #148158).
func TestMenuStationaryPointerDoesNotOpenSubmenu(t *testing.T) {
	r := newMenuRig(t, testMenuItems(), MenuOpts{})
	at := r.center("Recent")
	// the pointer moves onto the item, the submenu opens after the delay and is closed by Left
	r.ts.Hover(at)
	r.ts.Pump(MenuSubmenuShowDelay + time.Millisecond)
	if r.handle.OpenSubmenu() != 5 {
		t.Fatal("the submenu did not open")
	}
	r.press(services.LogicalKeyboardKeyArrowLeft)
	if r.handle.OpenSubmenu() != -1 {
		t.Fatal("Left did not close the submenu")
	}
	// events that do not move the pointer do not open it again
	r.ts.Hover(at)
	r.ts.Hover(at)
	r.ts.Pump(time.Second)
	if r.handle.OpenSubmenu() != -1 {
		t.Error("a pointer that rests opened the submenu")
	}
	// leaving the item and coming back does (the timer starts again for each visit)
	r.ts.Hover(r.center("Exit"))
	r.ts.Hover(at)
	r.ts.Pump(MenuSubmenuShowDelay + time.Millisecond)
	if r.handle.OpenSubmenu() != 5 {
		t.Error("a pointer that came back did not open the submenu")
	}
}

func TestMenuKeepsFocusedItemWhenItemsChange(t *testing.T) {
	var items = foundation.NewValueNotifier(testMenuItems())
	handle := NewMenuHandle()
	ts := pumpSized(t, ui.Size{Width: 500, Height: 400}, widgets.NewValueListenableBuilder[[]MenuItem](items, func(_ widgets.BuildContext, it []MenuItem, _ widgets.Widget) widgets.Widget {
		return NewMenu(it, MenuOpts{Handle: handle, Autofocus: true})
	}, nil))
	ts.Pump(time.Millisecond)
	key(ts, services.LogicalKeyboardKeyArrowDown)
	key(ts, services.LogicalKeyboardKeyArrowDown)
	if handle.FocusedIndex() != 1 {
		t.Fatalf("focus %d", handle.FocusedIndex())
	}
	// the focused item becomes disabled: the focus goes away; a new list keeps the index otherwise
	next := testMenuItems()
	next[1].Disabled = true
	items.SetValue(next)
	ts.Pump(time.Millisecond)
	if handle.FocusedIndex() != -1 {
		t.Errorf("focus %d after the item was disabled, want -1", handle.FocusedIndex())
	}
	key(ts, services.LogicalKeyboardKeyArrowDown)
	key(ts, services.LogicalKeyboardKeyArrowDown)
	if handle.FocusedIndex() != 4 {
		t.Errorf("focus %d, want 4 (the disabled items are skipped)", handle.FocusedIndex())
	}
	items.SetValue(testMenuItems())
	ts.Pump(time.Millisecond)
	if handle.FocusedIndex() != 4 {
		t.Errorf("focus %d, want 4 kept", handle.FocusedIndex())
	}
}

// textColor is the color of the text of a label.
func textColor(t *testing.T, ts *gluttertest.Tester, label string) ui.Color {
	t.Helper()
	txt := ts.Widget(gluttertest.ByText(label)).(*widgets.Text)
	style := txt.Style()
	if style == nil && txt.TextSpan() != nil {
		style = txt.TextSpan().(interface{ Style() *painting.TextStyle }).Style()
	}
	c, ok := style.Color.Get()
	if !ok {
		t.Fatalf("%q has no color", label)
	}
	return c
}

func TestMenuTextColors(t *testing.T) {
	c := DarkModern()
	r := newMenuRig(t, testMenuItems(), MenuOpts{})
	r.press(services.LogicalKeyboardKeyArrowDown) // New File is focused
	cases := []struct {
		label string
		want  ui.Color
	}{
		{"New File", c.MenuSelectionForeground},
		{"Ctrl+N", c.MenuSelectionForeground}, // the keybinding of the focused item is not dimmed
		{"Open...", c.MenuForeground},
		{"Ctrl+O", c.MenuForeground.WithOpacity(0.7)},
		{"Save", c.DisabledForeground},
		{"Ctrl+S", c.MenuForeground.WithOpacity(0.4)},
		{"Word Wrap", c.MenuForeground},
		{"Recent", c.MenuForeground},
	}
	for _, tc := range cases {
		if got := textColor(t, r.ts, tc.label); got != tc.want {
			t.Errorf("%q is in %v, want %v", tc.label, got, tc.want)
		}
	}
}
