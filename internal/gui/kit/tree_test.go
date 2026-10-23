package kit

import (
	"fmt"
	"image/color"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// treeRig is a tree over a model, with a second focusable below it to take the focus away, that records
// what it renders and reports.
type treeRig struct {
	t        *testing.T
	ts       *gluttertest.Tester
	m        *TreeModel
	ctrl     *TreeController
	other    *widgets.FocusNode
	colors   Colors
	selected []string
	opened   []string
	loads    []string
	rendered map[string]int
	height   float64
}

type treeRigOpts struct {
	colors   Colors
	size     ui.Size
	items    []TreeItem
	expanded []string
	tree     TreeOpts
	model    TreeModelOpts
	// noLoader leaves the loader out, so lazy nodes stay loading.
	noLoader bool
}

func newTreeRig(t *testing.T, o treeRigOpts) *treeRig {
	t.Helper()
	r := &treeRig{t: t, colors: o.colors, rendered: map[string]int{}, ctrl: NewTreeController(), height: o.size.Height}
	if r.colors == (Colors{}) {
		r.colors = DarkModern()
	}
	if o.size == (ui.Size{}) {
		o.size = ui.Size{Width: 300, Height: 220}
		r.height = o.size.Height
	}
	if o.items == nil {
		o.items = sampleItems()
	}
	if !o.noLoader {
		o.model.LoadChildren = func(key string, _ any) { r.loads = append(r.loads, key) }
	}
	r.m = NewTreeModel(o.model)
	r.m.SetExpandedKeys(o.expanded)
	if err := r.m.SetItems(o.items); err != nil {
		t.Fatal(err)
	}
	r.other = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "other"})
	t.Cleanup(r.other.Dispose)
	opts := o.tree
	opts.Controller = r.ctrl
	opts.OnSelect = func(n TreeNode) { r.selected = append(r.selected, n.Key) }
	opts.OnOpen = func(n TreeNode) { r.opened = append(r.opened, n.Key) }
	if opts.Render == nil {
		opts.Render = func(bc widgets.BuildContext, n TreeNode, st TreeRowState) widgets.Widget {
			r.rendered[n.Key]++
			return defaultTreeRender(bc, n, st)
		}
	}
	col := widgets.NewColumn([]widgets.Widget{
		widgets.NewSizedBox(NewTree(r.m, opts), widgets.SizedBoxOpts{Width: foundation.Some(o.size.Width), Height: foundation.Some(o.size.Height)}),
		widgets.NewSizedBox(widgets.NewFocus(widgets.NewSizedBox(nil), widgets.FocusOpts{FocusNode: r.other}), widgets.SizedBoxOpts{Width: foundation.Some(10.0), Height: foundation.Some(10.0)}),
	})
	r.ts = pumpThemed(t, r.colors, ui.Size{Width: o.size.Width, Height: o.size.Height + 10}, col)
	r.settle()
	return r
}

func (r *treeRig) settle() {
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
}

// y is the vertical center of row i (rows are 22 px high).
func (r *treeRig) y(i int) float64 { return float64(i)*22 + 11 }

func (r *treeRig) tapRow(i int, x float64) {
	r.t.Helper()
	r.ts.TapAt(ui.Offset{Dx: x, Dy: r.y(i)}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.settle()
}

// twistieX is the x of the chevron of a row at a depth.
func twistieX(depth int) float64 {
	return treeBaseIndent + float64(depth)*treeIndentStep + treeTwistieShift + 8
}

func (r *treeRig) key(k services.LogicalKeyboardKey) {
	r.t.Helper()
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	r.settle()
}

func (r *treeRig) keyWith(mod, k services.LogicalKeyboardKey) {
	r.t.Helper()
	r.ts.SendKeyDownEvent(mod)
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	r.ts.SendKeyUpEvent(mod)
	r.settle()
}

func (r *treeRig) pixel(x, y int) color.NRGBA { return pixelAt(r.ts, x, y) }

func nrgba(c ui.Color) color.NRGBA {
	return color.NRGBA{R: uint8(c.Red()), G: uint8(c.Green()), B: uint8(c.Blue()), A: 255}
}

// fail makes the test fail when the tester has seen errors.
func (r *treeRig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("errors: %v", errs)
	}
}

func (r *treeRig) renderedKeys() []string {
	var out []string
	for k := range r.rendered {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

var (
	keyArrowUp    = services.LogicalKeyboardKeyArrowUp
	keyArrowDown  = services.LogicalKeyboardKeyArrowDown
	keyArrowLeft  = services.LogicalKeyboardKeyArrowLeft
	keyArrowRight = services.LogicalKeyboardKeyArrowRight
	keyHome       = services.LogicalKeyboardKeyHome
	keyEnd        = services.LogicalKeyboardKeyEnd
	keyPageUp     = services.LogicalKeyboardKeyPageUp
	keyPageDown   = services.LogicalKeyboardKeyPageDown
	keySpace      = services.LogicalKeyboardKeySpace
	keyEscape     = services.LogicalKeyboardKeyEscape
)

func TestTreeShowsTheRowsOfTheModel(t *testing.T) {
	cases := []struct {
		name     string
		expanded []string
		want     []string
	}{
		{"collapsed", nil, []string{"a", "b", "c", "d"}},
		{"expanded", []string{"a", "a2"}, []string{"a", "a1", "a2", "a2x", "a3", "b", "c", "d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTreeRig(t, treeRigOpts{expanded: tc.expanded})
			if got := r.renderedKeys(); !slices.Equal(got, tc.want) {
				t.Errorf("rendered %q, want %q", got, tc.want)
			}
			r.noErrors()
		})
	}
	t.Run("the placeholder row of a node that is loading", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{expanded: []string{"c"}})
		if !r.ts.Exists(gluttertest.ByText("Loading...")) {
			t.Error("no placeholder row")
		}
		if got := r.renderedKeys(); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
			t.Errorf("the renderer is not called for the placeholder: %q", got)
		}
		if !slices.Equal(r.loads, []string{"c"}) {
			t.Errorf("loads %q", r.loads)
		}
		// the answer
		if err := r.m.SetChildren("c", []TreeItem{{Key: "c1", Data: "first"}, {Key: "c2"}}); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if r.ts.Exists(gluttertest.ByText("Loading...")) {
			t.Error("the placeholder is gone")
		}
		if !r.ts.Exists(gluttertest.ByText("first")) {
			t.Error("the loaded child is shown with its data")
		}
		r.noErrors()
	})
	t.Run("a label for the placeholder", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{expanded: []string{"c"}, tree: TreeOpts{LoadingLabel: "Reading..."}})
		if !r.ts.Exists(gluttertest.ByText("Reading...")) {
			t.Error("no placeholder row")
		}
	})
	t.Run("the default renderer shows the data", func(t *testing.T) {
		type named struct{ n int }
		r := newTreeRig(t, treeRigOpts{items: []TreeItem{{Key: "s", Data: "text"}, {Key: "n", Data: named{3}}, {Key: "nil"}}, tree: TreeOpts{Render: func(bc widgets.BuildContext, n TreeNode, st TreeRowState) widgets.Widget {
			return defaultTreeRender(bc, n, st)
		}}})
		for _, want := range []string{"text", "{3}", "<nil>"} {
			if !r.ts.Exists(gluttertest.ByText(want)) {
				t.Errorf("no row with %q", want)
			}
		}
	})
}

func TestTreeClicks(t *testing.T) {
	t.Run("a click on a row selects it and toggles a folder", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{})
		r.tapRow(0, 150)
		if k, _ := r.m.Selected(); k != "a" {
			t.Errorf("selected %q", k)
		}
		if !r.m.IsExpanded("a") {
			t.Error("a click on a folder toggles it")
		}
		if !slices.Equal(r.selected, []string{"a"}) || len(r.opened) != 0 {
			t.Errorf("OnSelect %q OnOpen %q", r.selected, r.opened)
		}
		if k, _ := r.m.Focused(); k != "a" || !r.ctrl.HasFocus() {
			t.Errorf("the row and the tree are focused: %q %v", k, r.ctrl.HasFocus())
		}
		r.tapRow(2, 150) // a2
		if k, _ := r.m.Selected(); k != "a2" {
			t.Errorf("selected %q", k)
		}
		r.tapRow(4, 150) // a3, a leaf
		if k, _ := r.m.Selected(); k != "a3" {
			t.Errorf("selected %q", k)
		}
		if !slices.Equal(r.selected, []string{"a", "a2", "a3"}) {
			t.Errorf("OnSelect %q", r.selected)
		}
		r.noErrors()
	})
	t.Run("a click on the twistie toggles and does not select", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{})
		r.tapRow(0, twistieX(0))
		if !r.m.IsExpanded("a") {
			t.Error("the twistie toggles")
		}
		if _, ok := r.m.Selected(); ok || len(r.selected) != 0 {
			t.Error("a click on the twistie selects nothing")
		}
		if k, _ := r.m.Focused(); k != "a" {
			t.Errorf("focused %q", k)
		}
		r.tapRow(0, twistieX(0))
		if r.m.IsExpanded("a") {
			t.Error("the twistie toggles back")
		}
		// the twistie of a deeper row is further right
		r.m.Expand("a")
		r.settle()
		r.tapRow(2, twistieX(1))
		if !r.m.IsExpanded("a2") {
			t.Error("the twistie of a2 toggles")
		}
		// the place of the twistie of a leaf selects the leaf
		r.tapRow(1, twistieX(1))
		if k, _ := r.m.Selected(); k != "a1" {
			t.Errorf("selected %q", k)
		}
		r.noErrors()
	})
	t.Run("with ToggleOnClick off only the twistie toggles", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{tree: TreeOpts{ToggleOnClick: foundation.Some(false)}})
		r.tapRow(0, 150)
		if r.m.IsExpanded("a") {
			t.Error("a click on the row does not toggle")
		}
		if k, _ := r.m.Selected(); k != "a" {
			t.Errorf("selected %q", k)
		}
		r.tapRow(0, twistieX(0))
		if !r.m.IsExpanded("a") {
			t.Error("the twistie toggles")
		}
		r.noErrors()
	})
	t.Run("a double click opens the node and does not toggle a second time", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{})
		r.tapRow(3, 150) // d, a leaf: row 3
		r.tapRow(3, 150)
		if !slices.Equal(r.opened, []string{"d"}) {
			t.Errorf("OnOpen %q", r.opened)
		}
		if !slices.Equal(r.selected, []string{"d"}) {
			t.Errorf("the first click selects: %q", r.selected)
		}
		// a folder
		r.tapRow(0, 150)
		r.tapRow(0, 150)
		if !r.m.IsExpanded("a") {
			t.Error("two clicks on a folder: it was toggled once")
		}
		if !slices.Equal(r.opened, []string{"d", "a"}) {
			t.Errorf("OnOpen %q", r.opened)
		}
		// a third click is the first of a new pair
		r.tapRow(0, 150)
		if r.m.IsExpanded("a") || len(r.opened) != 2 {
			t.Errorf("third click: expanded %v opened %q", r.m.IsExpanded("a"), r.opened)
		}
		r.noErrors()
	})
	t.Run("two clicks on different rows are not a double click", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{})
		r.tapRow(3, 150)
		r.tapRow(1, 150)
		if len(r.opened) != 0 {
			t.Errorf("OnOpen %q", r.opened)
		}
	})
	t.Run("two clicks far apart in time are not a double click", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{})
		r.tapRow(3, 150)
		r.ts.Pump(treeDoubleClickInterval + 100*time.Millisecond)
		r.tapRow(3, 150)
		if len(r.opened) != 0 {
			t.Errorf("OnOpen %q", r.opened)
		}
	})
	t.Run("a double click with ToggleOnClick off toggles a folder", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{tree: TreeOpts{ToggleOnClick: foundation.Some(false)}})
		r.tapRow(0, 150)
		r.tapRow(0, 150)
		if !r.m.IsExpanded("a") || !slices.Equal(r.opened, []string{"a"}) {
			t.Errorf("expanded %v opened %q", r.m.IsExpanded("a"), r.opened)
		}
	})
	t.Run("the secondary button focuses the row and nothing else", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{})
		r.ts.TapAt(ui.Offset{Dx: 150, Dy: r.y(3)}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)})
		r.settle()
		if k, _ := r.m.Focused(); k != "d" {
			t.Errorf("focused %q", k)
		}
		if _, ok := r.m.Selected(); ok || len(r.selected) != 0 || len(r.opened) != 0 {
			t.Error("a secondary press selects and opens nothing")
		}
	})
	t.Run("a click on a lazy folder loads it", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{})
		r.tapRow(2, 150)
		if !slices.Equal(r.loads, []string{"c"}) || !r.ts.Exists(gluttertest.ByText("Loading...")) {
			t.Errorf("loads %q", r.loads)
		}
	})
}

func TestTreeKeyboard(t *testing.T) {
	r := newTreeRig(t, treeRigOpts{})
	// the tree takes the keyboard focus
	r.ctrl.Focus()
	r.settle()
	if !r.ctrl.HasFocus() {
		t.Fatal("the tree has the focus")
	}
	if k, _ := r.m.Focused(); k != "a" {
		t.Fatalf("a tree that takes the focus focuses its first row, got %q", k)
	}
	steps := []struct {
		name string
		do   func()
		foc  string
		rows []string
		sel  string
	}{
		{"right expands", func() { r.key(keyArrowRight) }, "a", []string{"a", "a1", "a2", "a3", "b", "c", "d"}, ""},
		{"right focuses the first child", func() { r.key(keyArrowRight) }, "a1", []string{"a", "a1", "a2", "a3", "b", "c", "d"}, ""},
		{"down", func() { r.key(keyArrowDown) }, "a2", nil, ""},
		{"space toggles a2", func() { r.key(keySpace) }, "a2", []string{"a", "a1", "a2", "a2x", "a3", "b", "c", "d"}, ""},
		{"down", func() { r.key(keyArrowDown) }, "a2x", nil, ""},
		{"enter selects", func() { r.key(services.LogicalKeyboardKeyEnter) }, "a2x", nil, "a2x"},
		{"left goes to the parent", func() { r.key(keyArrowLeft) }, "a2", nil, "a2x"},
		{"left collapses", func() { r.key(keyArrowLeft) }, "a2", []string{"a", "a1", "a2", "a3", "b", "c", "d"}, "a2x"},
		{"up", func() { r.key(keyArrowUp) }, "a1", nil, "a2x"},
		{"end", func() { r.key(keyEnd) }, "d", nil, "a2x"},
		{"home", func() { r.key(keyHome) }, "a", nil, "a2x"},
		{"ctrl+left collapses all", func() { r.keyWith(services.LogicalKeyboardKeyControl, keyArrowLeft) }, "a", []string{"a", "b", "c", "d"}, "a2x"},
		{"escape clears the selection", func() { r.key(keyEscape) }, "a", nil, ""},
		{"escape clears the focus", func() { r.key(keyEscape) }, "", nil, ""},
	}
	for _, s := range steps {
		s.do()
		if k, _ := r.m.Focused(); k != s.foc {
			t.Fatalf("%s: focus %q, want %q", s.name, k, s.foc)
		}
		if k, _ := r.m.Selected(); k != s.sel {
			t.Fatalf("%s: selection %q, want %q", s.name, k, s.sel)
		}
		if s.rows != nil {
			var got []string
			for _, n := range r.m.Rows() {
				got = append(got, n.Key)
			}
			if !slices.Equal(got, s.rows) {
				t.Fatalf("%s: rows %q, want %q", s.name, got, s.rows)
			}
		}
	}
	if !slices.Equal(r.selected, []string{"a2x"}) || !slices.Equal(r.opened, []string{"a2x"}) {
		t.Errorf("OnSelect %q OnOpen %q: Enter does both", r.selected, r.opened)
	}
	r.noErrors()
}

func TestTreeKeyboardIgnoresWhatItDoesNotUse(t *testing.T) {
	r := newTreeRig(t, treeRigOpts{})
	r.ctrl.Focus()
	r.settle()
	before, _ := r.m.Focused()
	for _, mod := range []services.LogicalKeyboardKey{services.LogicalKeyboardKeyShift, services.LogicalKeyboardKeyControl, services.LogicalKeyboardKeyAlt} {
		r.keyWith(mod, keyArrowDown)
		if k, _ := r.m.Focused(); k != before {
			t.Errorf("down with a modifier moved the focus to %q", k)
		}
	}
	r.key(services.LogicalKeyboardKeyKeyA)
	r.key(services.LogicalKeyboardKeyTab)
	if k, _ := r.m.Focused(); k != before {
		t.Errorf("other keys move nothing: %q", k)
	}
	// without the focus the tree gets no keys
	r.other.RequestFocus()
	r.settle()
	r.key(keyArrowDown)
	if k, _ := r.m.Focused(); k != before {
		t.Errorf("a tree without the focus moved to %q", k)
	}
	r.noErrors()
}

func TestTreeRightOnALeafGoesOn(t *testing.T) {
	// the key is not used by the tree, so a widget above the tree gets it
	var seen []services.LogicalKeyboardKey
	m := NewTreeModel()
	if err := m.SetItems(sampleItems()); err != nil {
		t.Fatal(err)
	}
	ctrl := NewTreeController()
	child := widgets.NewFocus(NewTree(m, TreeOpts{Controller: ctrl}), widgets.FocusOpts{OnKeyEvent: func(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
		if _, ok := e.(*services.KeyDownEvent); ok {
			seen = append(seen, e.LogicalKey())
		}
		return widgets.KeyEventResultIgnored
	}})
	ts := pumpThemed(t, DarkModern(), ui.Size{Width: 300, Height: 220}, widgets.NewSizedBox(child, widgets.SizedBoxOpts{Width: foundation.Some(300.0), Height: foundation.Some(220.0)}))
	ctrl.Focus()
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	m.SetFocus("d")
	for _, k := range []services.LogicalKeyboardKey{keyArrowRight, keyArrowLeft, keyArrowDown, keyEscape} {
		key(ts, k)
	}
	m.ClearSelection()
	m.ClearFocus()
	key(ts, keyEscape)
	// Right and Left on a root leaf, and Escape with nothing to clear, are not used
	want := []services.LogicalKeyboardKey{keyArrowRight, keyArrowLeft, keyEscape}
	var got []services.LogicalKeyboardKey
	for _, k := range seen {
		if k == keyArrowDown {
			continue
		}
		got = append(got, k)
	}
	if !slices.Equal(got, want) {
		t.Errorf("keys that went on to the widget above: %v, want %v", got, want)
	}
}

func TestTreeScrolling(t *testing.T) {
	// 40 roots of which "f" has 10 files each; the view is 10 rows (220 px) high
	items := make([]TreeItem, 40)
	for i := range items {
		items[i] = TreeItem{Key: fmt.Sprintf("r%02d", i)}
		if i == 30 {
			for j := range 10 {
				items[i].Children = append(items[i].Children, TreeItem{Key: fmt.Sprintf("r30/c%d", j)})
			}
		}
	}
	t.Run("reveal scrolls to a row and expands the folders above it", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{items: items})
		if r.ctrl.ViewportHeight() != 220 || r.ctrl.ScrollOffset() != 0 {
			t.Fatalf("viewport %v offset %v", r.ctrl.ViewportHeight(), r.ctrl.ScrollOffset())
		}
		if !r.ctrl.Reveal("r30/c9") {
			t.Fatal("reveal failed")
		}
		r.settle()
		if !r.m.IsExpanded("r30") {
			t.Error("the folder above is expanded")
		}
		// r30 is row 30, its children rows 31..40; c9 is row 40: its bottom (41 rows * 22 = 902) minus the view
		if got := r.ctrl.ScrollOffset(); got != 41*22-220 {
			t.Errorf("offset %v, want %v", got, 41*22-220)
		}
		// a row in view does not scroll
		r.ctrl.Reveal("r30/c3")
		r.settle()
		if got := r.ctrl.ScrollOffset(); got != 41*22-220 {
			t.Errorf("offset %v after revealing a row in view", got)
		}
		// a row above scrolls up to it
		r.ctrl.Reveal("r05")
		r.settle()
		if got := r.ctrl.ScrollOffset(); got != 5*22 {
			t.Errorf("offset %v, want %v", got, 5*22)
		}
		if r.ctrl.Reveal("zzz") {
			t.Error("a node the model does not have")
		}
		r.noErrors()
	})
	t.Run("ScrollTo and the rows that are built", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{items: items})
		r.ctrl.ScrollTo(400)
		r.settle()
		if r.ctrl.ScrollOffset() != 400 {
			t.Errorf("offset %v", r.ctrl.ScrollOffset())
		}
		if r.rendered["r19"] == 0 || r.rendered["r00"] > 1 {
			// 400 / 22 = row 18.2: row 19 is the first one fully in view
			t.Errorf("rows around the view: r19 %d r00 %d", r.rendered["r19"], r.rendered["r00"])
		}
		r.noErrors()
	})
	t.Run("the keyboard scrolls the focused row into view", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{items: items})
		r.ctrl.Focus()
		r.settle()
		r.key(keyEnd)
		if got := r.ctrl.ScrollOffset(); got != 40*22-220 {
			t.Errorf("after End: offset %v, want %v", got, 40*22-220)
		}
		r.key(keyHome)
		if r.ctrl.ScrollOffset() != 0 {
			t.Errorf("after Home: offset %v", r.ctrl.ScrollOffset())
		}
		var seen []string
		for range 4 {
			r.key(keyPageDown)
			k, _ := r.m.Focused()
			seen = append(seen, k)
		}
		// ten rows in view: the focus moves by ten, the last page stops at the end
		if !slices.Equal(seen, []string{"r09", "r19", "r29", "r39"}) {
			t.Errorf("page down: %q", seen)
		}
		for range 4 {
			r.key(keyPageUp)
		}
		if k, _ := r.m.Focused(); k != "r00" || r.ctrl.ScrollOffset() != 0 {
			t.Errorf("page up four times: %q at %v", k, r.ctrl.ScrollOffset())
		}
		// moving down past the view scrolls by the least
		for range 11 {
			r.key(keyArrowDown)
		}
		if k, _ := r.m.Focused(); k != "r11" || r.ctrl.ScrollOffset() != 2*22 {
			t.Errorf("11 downs: %q at %v", k, r.ctrl.ScrollOffset())
		}
		r.noErrors()
	})
	t.Run("the wheel scrolls", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{items: items})
		r.ts.ScrollAt(ui.Offset{Dx: 100, Dy: 100}, ui.Offset{Dy: 2 * wheelNotch})
		r.settle()
		if r.ctrl.ScrollOffset() <= 0 {
			t.Error("the wheel scrolls the tree")
		}
	})
}

func TestTreeColors(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			colors := th.colors()
			r := newTreeRig(t, treeRigOpts{colors: colors, expanded: []string{"a"}})
			// rows: 0 a, 1 a1, 2 a2, 3 a3, 4 b, 5 c, 6 d
			bg := nrgba(colors.EditorBackground)
			x := 200
			at := func(row int) color.NRGBA { return r.pixel(x, int(r.y(row))+5) }
			if got := at(1); got != bg {
				t.Errorf("a plain row is %v, want the background %v", got, bg)
			}
			// the pointer over a row
			r.ts.Hover(ui.Offset{Dx: 150, Dy: r.y(1)})
			r.settle()
			if got, want := at(1), nrgba(colors.ListHoverBackground); got != want {
				t.Errorf("a hovered row is %v, want %v", got, want)
			}
			r.ts.Hover(ui.Offset{Dx: 299, Dy: 215})
			r.settle()
			// a click selects and focuses: the active colors
			r.tapRow(3, 150)
			r.ts.Hover(ui.Offset{Dx: 299, Dy: 215})
			r.settle()
			if got, want := at(3), nrgba(colors.ListActiveSelectionBackground); got != want {
				t.Errorf("the selected row of a tree with the focus is %v, want %v", got, want)
			}
			if got, want := r.pixel(0, int(r.y(3))), nrgba(colors.ListFocusOutline); got != want {
				t.Errorf("the focus outline is %v, want %v", got, want)
			}
			if got := r.pixel(0, int(r.y(2))); got != bg {
				t.Errorf("only the focused row has an outline: %v", got)
			}
			// the focus moves to another row with the keyboard: the selection stays
			r.key(keyArrowUp)
			if got, want := at(3), nrgba(colors.ListActiveSelectionBackground); got != want {
				t.Errorf("the selection stays: %v", got)
			}
			if got := r.pixel(0, int(r.y(2))); got != nrgba(colors.ListFocusOutline) {
				t.Errorf("the outline follows the focus: %v", got)
			}
			// the focus leaves the tree: the inactive colors, no outline
			r.other.RequestFocus()
			r.settle()
			if r.ctrl.HasFocus() {
				t.Fatal("the tree lost the focus")
			}
			if got, want := at(3), nrgba(colors.ListInactiveSelectionBackground); got != want {
				t.Errorf("the selected row of a tree without the focus is %v, want %v", got, want)
			}
			if got := r.pixel(0, int(r.y(2))); got != bg {
				t.Errorf("a tree without the focus has no outline: %v", got)
			}
			// the focus comes back
			r.ctrl.Focus()
			r.settle()
			if got, want := at(3), nrgba(colors.ListActiveSelectionBackground); got != want {
				t.Errorf("the selected row after the focus came back is %v, want %v", got, want)
			}
			r.noErrors()
		})
	}
}

func TestTreeRowLook(t *testing.T) {
	colors := DarkModern()
	// a theme that sets the colors that are transparent in the bundled ones
	colors.ListFocusBackground = ui.NewColor(0xFF111111)
	colors.ListFocusForeground = ui.NewColor(0xFF222222)
	colors.ListInactiveFocusBackground = ui.NewColor(0xFF333333)
	colors.ListInactiveFocusOutline = ui.NewColor(0xFF444444)
	colors.ListInactiveSelectionForeground = ui.NewColor(0xFF555555)
	colors.ListFocusAndSelectionOutline = ui.NewColor(0xFF666666)
	zero := ui.Color{}
	cases := []struct {
		name                    string
		st                      TreeRowState
		bg, fg, outline         ui.Color
		bgIsTransparent, noLine bool
	}{
		{"plain", TreeRowState{}, zero, colors.Foreground, zero, true, true},
		{"hovered", TreeRowState{Hovered: true}, colors.ListHoverBackground, colors.Foreground, zero, false, true},
		{"selected, active", TreeRowState{Selected: true, Active: true}, colors.ListActiveSelectionBackground, colors.ListActiveSelectionForeground, zero, false, true},
		{"selected, inactive", TreeRowState{Selected: true}, colors.ListInactiveSelectionBackground, colors.ListInactiveSelectionForeground, zero, false, true},
		{"focused, active", TreeRowState{Focused: true, Active: true}, colors.ListFocusBackground, colors.ListFocusForeground, colors.ListFocusOutline, false, false},
		{"focused, inactive", TreeRowState{Focused: true}, colors.ListInactiveFocusBackground, colors.Foreground, colors.ListInactiveFocusOutline, false, false},
		{"focused and selected, active", TreeRowState{Focused: true, Selected: true, Active: true}, colors.ListActiveSelectionBackground, colors.ListActiveSelectionForeground, colors.ListFocusAndSelectionOutline, false, false},
		{"focused and selected, inactive", TreeRowState{Focused: true, Selected: true}, colors.ListInactiveSelectionBackground, colors.ListInactiveSelectionForeground, colors.ListInactiveFocusOutline, false, false},
		{"hovered and selected: the selection wins", TreeRowState{Hovered: true, Selected: true, Active: true}, colors.ListActiveSelectionBackground, colors.ListActiveSelectionForeground, zero, false, true},
		{"hovered and focused: the focus wins", TreeRowState{Hovered: true, Focused: true, Active: true}, colors.ListFocusBackground, colors.ListFocusForeground, colors.ListFocusOutline, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bg, fg, outline := rowLook(colors, tc.st)
			if bg != tc.bg || fg != tc.fg || outline != tc.outline {
				t.Errorf("rowLook = %v %v %v, want %v %v %v", bg, fg, outline, tc.bg, tc.fg, tc.outline)
			}
		})
	}
	// a focus-and-selection outline the theme leaves out is the focus outline
	colors.ListFocusAndSelectionOutline = zero
	if _, _, outline := rowLook(colors, TreeRowState{Focused: true, Selected: true, Active: true}); outline != colors.ListFocusOutline {
		t.Errorf("outline %v", outline)
	}
}

func TestTreeIndentGuides(t *testing.T) {
	colors := DarkModern()
	// rows: 0 a, 1 a1, 2 a2, 3 a2x, 4 a3, 5 b, 6 c, 7 d. a's children are at depth 1: a guide at x = 16;
	// a2x is at depth 2: guides at x = 16 and 24.
	rowY := func(r *treeRig, row int) int { return int(r.y(row)) }
	bg := nrgba(colors.EditorBackground)
	blend := func(c ui.Color) color.NRGBA {
		a := float64(c.Alpha()) / 255
		mix := func(f int, b uint8) uint8 { return uint8(float64(f)*a + float64(b)*(1-a) + 0.5) }
		return color.NRGBA{R: mix(c.Red(), bg.R), G: mix(c.Green(), bg.G), B: mix(c.Blue(), bg.B), A: 255}
	}
	near := func(a, b color.NRGBA) bool {
		d := func(x, y uint8) int {
			if x > y {
				return int(x - y)
			}
			return int(y - x)
		}
		return d(a.R, b.R) <= 2 && d(a.G, b.G) <= 2 && d(a.B, b.B) <= 2
	}
	t.Run("always", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{expanded: []string{"a", "a2"}, tree: TreeOpts{IndentGuides: TreeGuidesAlways}})
		if got, want := r.pixel(16, rowY(r, 1)), blend(colors.TreeInactiveIndentGuidesStroke); !near(got, want) {
			t.Errorf("the guide of a1 is %v, want %v", got, want)
		}
		if got := r.pixel(16, rowY(r, 0)-9); got != bg {
			t.Errorf("a root has no guide: %v", got)
		}
		if got := r.pixel(16, rowY(r, 3)); near(got, bg) {
			t.Errorf("the guide of a2x at 16 is missing: %v", got)
		}
		if got := r.pixel(24, rowY(r, 3)); near(got, bg) {
			t.Errorf("the guide of a2x at 24 is missing: %v", got)
		}
		if got := r.pixel(24, rowY(r, 1)); got != bg {
			t.Errorf("a1 has one guide only: %v", got)
		}
		// the guide is continuous from a row to the next
		if r.pixel(16, 43) != r.pixel(16, 45) {
			t.Error("the guide is broken between rows")
		}
	})
	t.Run("the active guide is the stroke color", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{expanded: []string{"a", "a2"}, tree: TreeOpts{IndentGuides: TreeGuidesAlways}})
		r.tapRow(1, 150) // a1: its parent a is the active guide
		r.other.RequestFocus()
		r.settle()
		if got, want := r.pixel(16, rowY(r, 2)), blend(colors.TreeIndentGuidesStroke); !near(got, want) {
			t.Errorf("the active guide is %v, want %v", got, want)
		}
		if got, want := r.pixel(24, rowY(r, 3)), blend(colors.TreeInactiveIndentGuidesStroke); !near(got, want) {
			t.Errorf("a guide that is not active is %v, want %v", got, want)
		}
	})
	t.Run("on hover", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{expanded: []string{"a", "a2"}})
		if got := r.pixel(16, rowY(r, 1)); got != bg {
			t.Errorf("no guide without the pointer: %v", got)
		}
		r.ts.Hover(ui.Offset{Dx: 200, Dy: 200})
		r.settle()
		if got := r.pixel(16, rowY(r, 1)); near(got, bg) {
			t.Errorf("guides with the pointer over the tree: %v", got)
		}
		// the active guide shows without the pointer
		r.tapRow(1, 150)
		r.ts.Hover(ui.Offset{Dx: 900, Dy: 900})
		r.settle()
		if got := r.pixel(16, rowY(r, 4)); !near(got, blend(colors.TreeIndentGuidesStroke)) {
			t.Errorf("the active guide without the pointer: %v", got)
		}
		if got := r.pixel(24, rowY(r, 3)); got != bg {
			t.Errorf("the others are hidden: %v", got)
		}
	})
	t.Run("none", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{expanded: []string{"a", "a2"}, tree: TreeOpts{IndentGuides: TreeGuidesNone}})
		r.tapRow(1, 150)
		if got := r.pixel(16, rowY(r, 2)); got != bg {
			t.Errorf("no guides: %v", got)
		}
	})
}

func TestTreeRebuildKeepsExpansion(t *testing.T) {
	r := newTreeRig(t, treeRigOpts{})
	r.tapRow(0, twistieX(0)) // a
	r.tapRow(2, twistieX(1)) // a2
	r.tapRow(4, 150)         // a3: select
	if got := shape(r.m); !slices.Equal(got, []string{"a-", "  a1", "  a2-", "    a2x", "  a3", "b+", "c+", "d"}) {
		t.Fatalf("rows %q", got)
	}
	// the items come again, with changes, as after a folder was read again
	items := sampleItems()
	items[0].Children = append(items[0].Children, TreeItem{Key: "a4"})
	items[0].Children[1].Children = append(items[0].Children[1].Children, TreeItem{Key: "a2y"})
	items = append(items, TreeItem{Key: "e"})
	clear(r.rendered)
	if err := r.m.SetItems(items); err != nil {
		t.Fatal(err)
	}
	r.settle()
	want := []string{"a", "a1", "a2", "a2x", "a2y", "a3", "a4", "b", "c", "d", "e"}
	r.ctrl.ScrollTo(0)
	if got := r.renderedKeys(); !slices.Equal(got, want) {
		t.Errorf("rendered %q, want %q", got, want)
	}
	if k, _ := r.m.Selected(); k != "a3" {
		t.Errorf("the selection is kept: %q", k)
	}
	// the row of a3 is selected on the screen
	if got, want := r.pixel(200, int(r.y(5))+5), nrgba(r.colors.ListActiveSelectionBackground); got != want {
		t.Errorf("a3 at its new row is %v, want %v", got, want)
	}
	// a click still lands on the right row
	r.tapRow(7, 150)
	if k, _ := r.m.Selected(); k != "b" {
		t.Errorf("selected %q", k)
	}
	r.noErrors()
}

func TestTreeFollowsANewModelAndController(t *testing.T) {
	first, second := NewTreeModel(), NewTreeModel()
	if err := first.SetItems([]TreeItem{{Key: "one", Data: "first model"}}); err != nil {
		t.Fatal(err)
	}
	if err := second.SetItems([]TreeItem{{Key: "two", Data: "second model"}}); err != nil {
		t.Fatal(err)
	}
	c1, c2 := NewTreeController(), NewTreeController()
	build := func(m *TreeModel, c *TreeController) widgets.Widget {
		return widgets.NewSizedBox(NewTree(m, TreeOpts{Controller: c}), widgets.SizedBoxOpts{Width: foundation.Some(200.0), Height: foundation.Some(100.0)})
	}
	ts := pumpThemed(t, DarkModern(), ui.Size{Width: 200, Height: 100}, build(first, c1))
	if !ts.Exists(gluttertest.ByText("first model")) {
		t.Fatal("first model")
	}
	// the widget is replaced by one with another model and controller
	ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR, NewTheme(DarkModern(), build(second, c2), ThemeOpts{UIFont: "Go Mono"})))
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByText("second model")) || ts.Exists(gluttertest.ByText("first model")) {
		t.Error("the tree shows the new model")
	}
	first.Expand("one") // a listener left on the old model would be a leak: nothing to see, nothing to fail
	// the controller moved to the new widget
	c1.Focus()
	if c1.HasFocus() {
		t.Error("the old controller is detached")
	}
	c2.Focus()
	ts.Pump(time.Millisecond)
	if !c2.HasFocus() {
		t.Error("the new controller is attached")
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
	// a controller with no tree does nothing
	lone := NewTreeController()
	lone.Focus()
	lone.ScrollTo(10)
	if lone.Reveal("x") || lone.HasFocus() || lone.ScrollOffset() != 0 || lone.ViewportHeight() != 0 {
		t.Error("a controller with no tree")
	}
}

func TestTreeLoadingNodeLooks(t *testing.T) {
	r := newTreeRig(t, treeRigOpts{noLoader: true})
	r.tapRow(2, 150) // c: loads, with no loader it stays loading
	if !r.ts.Exists(gluttertest.ByText("Loading...")) {
		t.Fatal("no placeholder")
	}
	// the placeholder row can be clicked without effect and cannot be focused
	r.tapRow(3, 150)
	if k, _ := r.m.Focused(); k != "c" {
		t.Errorf("focus %q", k)
	}
	r.key(keyArrowDown)
	if k, _ := r.m.Focused(); k != "d" {
		t.Errorf("down from the loading node skips the placeholder: %q", k)
	}
	// the load fails: the node collapses
	r.m.FailLoad("c")
	r.settle()
	if r.ts.Exists(gluttertest.ByText("Loading...")) {
		t.Error("placeholder gone")
	}
	r.noErrors()
}
