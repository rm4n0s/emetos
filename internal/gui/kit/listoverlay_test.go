package kit

import (
	"fmt"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// pumpSized shows a widget at the top left of a window of a size, with a theme that uses the test font.
func pumpSized(t *testing.T, size ui.Size, child widgets.Widget) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, size)
	ts.PumpWidget(widgets.NewWidgetsApp(DarkModern().EditorBackground, widgets.WidgetsAppOpts{
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				return NewTheme(DarkModern(), widgets.NewAlign(child, widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}), ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	ts.Pump(time.Millisecond)
	return ts
}

// listRig is a list overlay over listItems that records what it reports.
type listRig struct {
	ts        *gluttertest.Tester
	selected  []int
	dismissed int
}

func newListRig(t *testing.T, items []ListItem, opts ListOverlayOpts) *listRig {
	t.Helper()
	r := &listRig{}
	opts.OnSelect = func(i int) { r.selected = append(r.selected, i) }
	opts.OnDismiss = func() { r.dismissed++ }
	r.ts = pumpSized(t, ui.Size{Width: 400, Height: 320}, NewListOverlay(items, opts))
	return r
}

func (r *listRig) key(k services.LogicalKeyboardKey, char string) {
	r.ts.SendKeyDownEvent(k, gluttertest.KeyOpts{Character: char})
	r.ts.SendKeyUpEvent(k)
	r.ts.Pump(time.Millisecond)
}

var (
	keyDown  = services.LogicalKeyboardKeyArrowDown
	keyEnter = services.LogicalKeyboardKeyEnter
)

func TestListOverlayKeyboard(t *testing.T) {
	cases := []struct {
		name       string
		filterable bool
		keys       []string // key names; "a".."z" type a character
		selected   []int
		dismissed  int
	}{
		{"Enter chooses the focused row", false, []string{"enter"}, []int{0}, 0},
		{"Down then Enter skips the disabled row", false, []string{"down", "enter"}, []int{2}, 0},
		{"End then Enter", false, []string{"end", "enter"}, []int{4}, 0},
		{"Home after End", false, []string{"end", "home", "enter"}, []int{0}, 0},
		{"a page moves by the rows in view less one", false, []string{"pgdn", "enter"}, []int{2}, 0},
		{"two pages", false, []string{"pgdn", "pgdn", "enter"}, []int{4}, 0},
		{"Up at the top stays", false, []string{"up", "enter"}, []int{0}, 0},
		{"Escape dismisses", false, []string{"escape"}, nil, 1},
		{"Escape after moving still only dismisses", false, []string{"down", "escape", "enter"}, []int{2}, 1},
		{"the numpad Enter chooses", false, []string{"down", "numpadenter"}, []int{2}, 0},
		{"typing does not filter a list that is not filterable", false, []string{"e", "p", "enter"}, []int{0}, 0},
		{"typing filters", true, []string{"e", "p", "enter"}, []int{4}, 0},
		{"typing finds a later row", true, []string{"d", "enter"}, []int{3}, 0},
		{"no match: Enter chooses nothing", true, []string{"x", "enter"}, nil, 0},
		{"Backspace removes a character", true, []string{"x", "backspace", "enter"}, []int{0}, 0},
		{"Backspace with nothing typed is ignored", true, []string{"backspace", "enter"}, []int{0}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newListRig(t, listItems(), ListOverlayOpts{Filterable: tc.filterable, Rows: 3})
			for _, name := range tc.keys {
				switch name {
				case "enter":
					r.key(keyEnter, "")
				case "numpadenter":
					r.key(services.LogicalKeyboardKeyNumpadEnter, "")
				case "down":
					r.key(keyDown, "")
				case "up":
					r.key(services.LogicalKeyboardKeyArrowUp, "")
				case "end":
					r.key(services.LogicalKeyboardKeyEnd, "")
				case "home":
					r.key(services.LogicalKeyboardKeyHome, "")
				case "pgdn":
					r.key(services.LogicalKeyboardKeyPageDown, "")
				case "escape":
					r.key(services.LogicalKeyboardKeyEscape, "")
				case "backspace":
					r.key(services.LogicalKeyboardKeyBackspace, "")
				default:
					r.key(services.LogicalKeyboardKey(name[0]), name)
				}
			}
			if fmt.Sprint(r.selected) != fmt.Sprint(tc.selected) {
				t.Errorf("selected %v, want %v", r.selected, tc.selected)
			}
			if r.dismissed != tc.dismissed {
				t.Errorf("dismissed %d times, want %d", r.dismissed, tc.dismissed)
			}
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestListOverlayPointer(t *testing.T) {
	items := listItems()
	t.Run("a click chooses the row", func(t *testing.T) {
		r := newListRig(t, items, ListOverlayOpts{})
		r.ts.Tap(gluttertest.ByText("gamma"))
		r.ts.Pump(time.Millisecond)
		if fmt.Sprint(r.selected) != "[2]" {
			t.Errorf("selected %v", r.selected)
		}
	})
	t.Run("a disabled row cannot be chosen", func(t *testing.T) {
		r := newListRig(t, items, ListOverlayOpts{})
		r.ts.Tap(gluttertest.ByText("beta"))
		r.ts.Pump(time.Millisecond)
		if len(r.selected) != 0 {
			t.Errorf("selected %v", r.selected)
		}
	})
	t.Run("a click outside dismisses", func(t *testing.T) {
		r := newListRig(t, items, ListOverlayOpts{Width: foundation.Some(100.0)})
		r.ts.TapAt(ui.Offset{Dx: 350, Dy: 300})
		r.ts.Pump(time.Millisecond)
		if r.dismissed != 1 || len(r.selected) != 0 {
			t.Errorf("dismissed %d, selected %v", r.dismissed, r.selected)
		}
	})
	t.Run("the focus moves away: dismissed once", func(t *testing.T) {
		r := newListRig(t, items, ListOverlayOpts{})
		r.ts.Binding.FocusManager().PrimaryFocus().Unfocus()
		r.ts.Pump(time.Millisecond)
		r.key(services.LogicalKeyboardKeyEscape, "")
		if r.dismissed != 1 {
			t.Errorf("dismissed %d times", r.dismissed)
		}
	})
}

func TestListOverlayScrollsAndFocuses(t *testing.T) {
	var many []ListItem
	for i := range 12 {
		many = append(many, ListItem{Label: fmt.Sprintf("item %02d", i)})
	}
	t.Run("the wheel scrolls the rows", func(t *testing.T) {
		r := newListRig(t, many, ListOverlayOpts{Rows: 4})
		if !r.ts.Exists(gluttertest.ByText("item 03")) || r.ts.Exists(gluttertest.ByText("item 04")) {
			t.Fatal("four rows should show at first")
		}
		r.ts.ScrollAt(ui.Offset{Dx: 50, Dy: 30}, ui.Offset{Dy: 53})
		r.ts.Pump(time.Millisecond)
		if r.ts.Exists(gluttertest.ByText("item 00")) || !r.ts.Exists(gluttertest.ByText("item 04")) {
			t.Error("the wheel did not scroll")
		}
	})
	t.Run("moving the focus scrolls it into view", func(t *testing.T) {
		r := newListRig(t, many, ListOverlayOpts{Rows: 4})
		for range 6 {
			r.key(keyDown, "")
		}
		if !r.ts.Exists(gluttertest.ByText("item 06")) || r.ts.Exists(gluttertest.ByText("item 00")) {
			t.Error("the focused row is not in view")
		}
		r.key(keyEnter, "")
		if fmt.Sprint(r.selected) != "[6]" {
			t.Errorf("selected %v", r.selected)
		}
	})
	t.Run("the focus starts on the item asked for", func(t *testing.T) {
		r := newListRig(t, many, ListOverlayOpts{Rows: 4, Focused: foundation.Some(9)})
		if !r.ts.Exists(gluttertest.ByText("item 09")) {
			t.Error("item 9 is not in view")
		}
		r.key(keyEnter, "")
		if fmt.Sprint(r.selected) != "[9]" {
			t.Errorf("selected %v", r.selected)
		}
	})
	t.Run("an empty filter result says so", func(t *testing.T) {
		r := newListRig(t, many, ListOverlayOpts{Filterable: true})
		r.key(services.LogicalKeyboardKeyKeyA, "z")
		if !r.ts.Exists(gluttertest.ByText("No results")) {
			t.Error("no message")
		}
	})
}

func TestGoldenListOverlay(t *testing.T) {
	items := []ListItem{
		{Label: "main.go", Icon: fonts.CodiconSymbolFile},
		{Label: "main_test.go", Icon: fonts.CodiconSymbolFile, Description: "tests"},
		{Label: "internal", Icon: fonts.CodiconFolder},
		{Label: "vendor", Icon: fonts.CodiconFolder, Disabled: true},
		{Label: "README.md", Icon: fonts.CodiconSymbolFile},
	}
	r := newListRig(t, items, ListOverlayOpts{Filterable: true, Width: foundation.Some(240.0), Focused: foundation.Some(1)})
	r.ts.Hover(ui.Offset{Dx: 60, Dy: 22*4 + 11})
	r.ts.Pump(time.Millisecond)
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	r.ts.MatchesGolden("list_overlay")
	r.key(services.LogicalKeyboardKeyKeyA, "a")
	r.key(services.LogicalKeyboardKeyKeyI, "i")
	r.ts.MatchesGolden("list_overlay_filtered")
}
