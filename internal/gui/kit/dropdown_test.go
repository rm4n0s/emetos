package kit

import (
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

var fruit = []DropdownOption{
	{Label: "Apple"}, {Label: "Banana"}, {Label: "Cherry", Description: "red"}, {Label: "Date", Disabled: true}, {Label: "Elderberry"}, {Label: "Apricot"},
}

// dropdownRig is a dropdown that follows the choices of the user.
type dropdownRig struct {
	t        *testing.T
	ts       *gluttertest.Tester
	node     *widgets.FocusNode
	selected int
	chosen   []int
}

func newDropdownRig(t *testing.T, colors Colors, size ui.Size, fill func(o *DropdownOpts), top ...float64) *dropdownRig {
	t.Helper()
	r := &dropdownRig{t: t, node: widgets.NewFocusNode(), selected: 1}
	t.Cleanup(r.node.Dispose)
	content := widgets.NewStatefulBuilder(func(_ widgets.BuildContext, setState widgets.StateSetter) widgets.Widget {
		o := DropdownOpts{
			Selected: r.selected, FocusNode: r.node, Width: foundation.Some(160.0),
			OnChanged: func(i int) {
				r.chosen = append(r.chosen, i)
				setState(func() { r.selected = i })
			},
		}
		if fill != nil {
			fill(&o)
		}
		var above float64
		if len(top) > 0 {
			above = top[0]
		}
		return padded(column(vgap(above), NewDropdown(fruit, o)))
	})
	r.ts = pumpThemed(t, colors, size, content)
	return r
}

func (r *dropdownRig) press(k services.LogicalKeyboardKey) {
	r.t.Helper()
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	r.ts.Pump(time.Millisecond)
}

func (r *dropdownRig) typeLetter(ch string, k services.LogicalKeyboardKey) {
	r.t.Helper()
	r.ts.SendKeyDownEvent(k, gluttertest.KeyOpts{Character: ch})
	r.ts.SendKeyUpEvent(k)
	r.ts.Pump(time.Millisecond)
}

// option is the finder of the row of an option in the open list.
func option(label string) gluttertest.Finder {
	return gluttertest.Descendant(gluttertest.ByType[*ListOverlay](), gluttertest.ByText(label))
}

func (r *dropdownRig) isOpen() bool { return r.ts.Exists(gluttertest.ByType[*ListOverlay]()) }

func (r *dropdownRig) focusField() {
	r.t.Helper()
	r.node.RequestFocus()
	r.ts.Pump(time.Millisecond)
	if !r.node.HasPrimaryFocus() {
		r.t.Fatal("the field did not take the focus")
	}
}

func (r *dropdownRig) fieldLabel() string {
	f := gluttertest.Descendant(gluttertest.ByType[*Dropdown](), gluttertest.ByType[*widgets.Text]())
	return r.ts.Widget(gluttertest.First(f)).(*widgets.Text).PlainText()
}

func (r *dropdownRig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("errors: %v", errs)
	}
}

var dropdownSize = ui.Size{Width: 240, Height: 300}

func TestDropdownShowsTheSelectedOption(t *testing.T) {
	cases := []struct {
		name     string
		selected int
		want     string
	}{{"the first", 0, "Apple"}, {"the second", 1, "Banana"}, {"none", -1, "Pick one"}, {"past the options", 9, "Pick one"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newDropdownRig(t, DarkModern(), dropdownSize, func(o *DropdownOpts) { o.Selected = tc.selected; o.Placeholder = "Pick one" })
			if got := r.fieldLabel(); got != tc.want {
				t.Errorf("label %q, want %q", got, tc.want)
			}
			if r.isOpen() {
				t.Error("open at first")
			}
			r.noErrors()
		})
	}
}

func TestDropdownMouse(t *testing.T) {
	r := newDropdownRig(t, DarkModern(), dropdownSize, nil)
	// a click opens the list below the field and gives the field the focus
	r.ts.Tap(gluttertest.ByType[*Dropdown]())
	r.ts.Pump(time.Millisecond)
	if !r.isOpen() {
		t.Fatal("a click did not open the list")
	}
	field := r.ts.TopLeft(gluttertest.ByType[*Dropdown]())
	list := r.ts.TopLeft(gluttertest.ByType[*ListOverlay]())
	if list.Dy < field.Dy+dropdownHeight-1 || list.Dx != field.Dx {
		t.Errorf("list at %v, field at %v", list, field)
	}
	if w := r.ts.Size(gluttertest.ByType[*ListOverlay]()).Width; w != 160 {
		t.Errorf("the list is %v wide, the field 160", w)
	}
	// a click on an option chooses it and closes the list
	r.ts.Tap(option("Cherry"))
	r.ts.Pump(time.Millisecond)
	if r.isOpen() || !slices.Equal(r.chosen, []int{2}) || r.fieldLabel() != "Cherry" {
		t.Errorf("open %v, chosen %v, label %q", r.isOpen(), r.chosen, r.fieldLabel())
	}
	if !r.node.HasPrimaryFocus() {
		t.Error("the focus did not go back to the field")
	}
	// choosing the selected option is not a change; a disabled option cannot be chosen
	r.ts.Tap(gluttertest.ByType[*Dropdown]())
	r.ts.Pump(time.Millisecond)
	r.ts.Tap(option("Cherry"))
	r.ts.Pump(time.Millisecond)
	r.ts.Tap(gluttertest.ByType[*Dropdown]())
	r.ts.Pump(time.Millisecond)
	r.ts.Tap(option("Date"))
	r.ts.Pump(time.Millisecond)
	if !slices.Equal(r.chosen, []int{2}) {
		t.Errorf("chosen %v", r.chosen)
	}
	// a second click on the field closes the list
	if !r.isOpen() {
		r.ts.Tap(gluttertest.ByType[*Dropdown]())
		r.ts.Pump(time.Millisecond)
	}
	r.ts.Tap(gluttertest.ByType[*Dropdown]())
	r.ts.Pump(time.Millisecond)
	if r.isOpen() {
		t.Error("a second click on the field left the list open")
	}
	// a click elsewhere closes it too
	r.ts.Tap(gluttertest.ByType[*Dropdown]())
	r.ts.Pump(time.Millisecond)
	r.ts.TapAt(ui.Offset{Dx: 230, Dy: 290})
	r.ts.Pump(time.Millisecond)
	if r.isOpen() {
		t.Error("a click outside left the list open")
	}
	r.noErrors()
}

func TestDropdownKeyboard(t *testing.T) {
	down, up := services.LogicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyArrowUp
	enter, esc, space := services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyEscape, services.LogicalKeyboardKeySpace
	cases := []struct {
		name   string
		act    func(r *dropdownRig)
		open   bool
		chosen []int
		focus  bool // the field has the focus at the end
	}{
		{"Enter opens", func(r *dropdownRig) { r.press(enter) }, true, nil, false},
		{"Space opens", func(r *dropdownRig) { r.press(space) }, true, nil, false},
		{"Down opens", func(r *dropdownRig) { r.press(down) }, true, nil, false},
		{"Up opens", func(r *dropdownRig) { r.press(up) }, true, nil, false},
		{"Down then Enter chooses the next option", func(r *dropdownRig) { r.press(down); r.press(down); r.press(enter) }, false, []int{2}, true},
		{"Up then Enter chooses the previous option", func(r *dropdownRig) { r.press(down); r.press(up); r.press(enter) }, false, []int{0}, true},
		{"Down skips a disabled option", func(r *dropdownRig) {
			r.press(down)
			r.press(down)
			r.press(down)
			r.press(enter)
		}, false, []int{4}, true},
		{"Enter on the selected option changes nothing", func(r *dropdownRig) { r.press(down); r.press(enter) }, false, nil, true},
		{"Escape closes and gives the focus back", func(r *dropdownRig) { r.press(down); r.press(down); r.press(esc) }, false, nil, true},
		{"Escape on the closed field does nothing", func(r *dropdownRig) { r.press(esc) }, false, nil, true},
		{"End goes to the last option", func(r *dropdownRig) {
			r.press(down)
			r.press(services.LogicalKeyboardKeyEnd)
			r.press(enter)
		}, false, []int{5}, true},
		{"Home goes to the first option", func(r *dropdownRig) {
			r.press(down)
			r.press(services.LogicalKeyboardKeyHome)
			r.press(enter)
		}, false, []int{0}, true},
		{"a letter selects the next option that starts with it", func(r *dropdownRig) {
			r.typeLetter("e", services.LogicalKeyboardKeyKeyE)
		}, false, []int{4}, true},
		{"a letter in upper case", func(r *dropdownRig) { r.typeLetter("A", services.LogicalKeyboardKeyKeyA) }, false, []int{5}, true},
		{"a letter that starts no option", func(r *dropdownRig) { r.typeLetter("z", services.LogicalKeyboardKeyKeyZ) }, false, nil, true},
		{"a letter goes round: Apple comes after Apricot", func(r *dropdownRig) {
			r.typeLetter("a", services.LogicalKeyboardKeyKeyA)
			r.typeLetter("a", services.LogicalKeyboardKeyKeyA)
		}, false, []int{5, 0}, true},
		{"a letter does not pick a disabled option", func(r *dropdownRig) { r.typeLetter("d", services.LogicalKeyboardKeyKeyD) }, false, nil, true},
		{"a letter in the open list moves the focus", func(r *dropdownRig) {
			r.press(down)
			r.typeLetter("e", services.LogicalKeyboardKeyKeyE)
			r.press(enter)
		}, false, []int{4}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newDropdownRig(t, DarkModern(), dropdownSize, nil)
			r.focusField()
			tc.act(r)
			if r.isOpen() != tc.open {
				t.Errorf("open %v, want %v", r.isOpen(), tc.open)
			}
			if !slices.Equal(r.chosen, tc.chosen) {
				t.Errorf("chosen %v, want %v", r.chosen, tc.chosen)
			}
			if tc.focus && !r.node.HasPrimaryFocus() {
				t.Error("the field does not have the focus")
			}
			r.noErrors()
		})
	}
}

func TestDropdownDisabled(t *testing.T) {
	r := newDropdownRig(t, DarkModern(), dropdownSize, func(o *DropdownOpts) { o.Disabled = true })
	r.node.RequestFocus()
	r.ts.Pump(time.Millisecond)
	if r.node.HasPrimaryFocus() {
		t.Error("a disabled dropdown took the focus")
	}
	r.ts.Tap(gluttertest.ByType[*Dropdown]())
	r.ts.Pump(time.Millisecond)
	r.press(services.LogicalKeyboardKeyEnter)
	if r.isOpen() || len(r.chosen) != 0 {
		t.Errorf("open %v, chosen %v", r.isOpen(), r.chosen)
	}
	if !r.ts.Exists(gluttertest.Descendant(gluttertest.ByType[*Dropdown](), gluttertest.ByType[*widgets.Opacity]())) {
		t.Error("a disabled dropdown is not dimmed")
	}
	r.noErrors()
}

func TestDropdownListPlacement(t *testing.T) {
	// near the bottom of the window the list opens above the field
	r := newDropdownRig(t, DarkModern(), ui.Size{Width: 240, Height: 190}, func(o *DropdownOpts) { o.Rows = 4 }, 100)
	r.ts.Tap(gluttertest.ByType[*Dropdown]())
	r.ts.Pump(time.Millisecond)
	list := r.ts.Size(gluttertest.ByType[*ListOverlay]())
	if list.Height != 4*listRowHeight+2 {
		t.Errorf("the list is %v high, want 4 rows", list.Height)
	}
	field := r.ts.TopLeft(gluttertest.ByType[*Dropdown]())
	// the field ends at 138 of 190: no room for 90 px under it, and room above it
	if top := r.ts.TopLeft(gluttertest.ByType[*ListOverlay]()).Dy; top+list.Height > field.Dy+1 {
		t.Errorf("the list is at %v..%v, over a field at %v it should end above", top, top+list.Height, field.Dy)
	}
	r.noErrors()
}

func TestDropdownColors(t *testing.T) {
	for _, theme := range []struct {
		name   string
		colors Colors
	}{{"Dark Modern", DarkModern()}, {"Light Modern", LightModern()}} {
		c := theme.colors
		t.Run(theme.name, func(t *testing.T) {
			r := newDropdownRig(t, c, dropdownSize, nil)
			root := gluttertest.ByType[*Dropdown]()
			if bg, border := backgroundOf(t, r.ts, root), borderOf(t, r.ts, root); bg != c.DropdownBackground || border != c.DropdownBorder {
				t.Errorf("field %v %v, want %v %v", bg, border, c.DropdownBackground, c.DropdownBorder)
			}
			r.focusField()
			if border := borderOf(t, r.ts, root); border != c.FocusBorder {
				t.Errorf("focused border %v, want %v", border, c.FocusBorder)
			}
			r.press(services.LogicalKeyboardKeyArrowDown)
			list := gluttertest.ByType[*ListOverlay]()
			want := c.DropdownListBackground
			if want.Alpha() == 0 {
				want = c.DropdownBackground
			}
			if bg := backgroundOf(t, r.ts, list); bg != want {
				t.Errorf("list background %v, want %v", bg, want)
			}
			r.noErrors()
		})
	}
}
