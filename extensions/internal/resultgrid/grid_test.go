package resultgrid

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// gridRig is a grid on screen, with the text it copied and the fetches it asked for.
type gridRig struct {
	t       *testing.T
	g       *Grid
	ts      *gluttertest.Tester
	copied  []string
	fetches int
}

func newGridRig(t *testing.T, th uitest.Theme, size gui.Size, src Source, tune ...func(*Options)) *gridRig {
	t.Helper()
	r := &gridRig{t: t}
	opts := Options{OnCopy: func(s string) { r.copied = append(r.copied, s) }, OnFetchMore: func() { r.fetches++ }}
	for _, f := range tune {
		f(&opts)
	}
	r.g = New(opts)
	t.Cleanup(r.g.Dispose)
	r.g.SetSource(src)
	r.ts = uitest.Pump(t, th, size, r.g.Widget())
	return r
}

func (r *gridRig) pump() { r.ts.Pump(50 * time.Millisecond) }

// at is the place in the window of a cell: the middle of the cell, one row below the header.
func (r *gridRig) at(row, col int) gui.Offset {
	x := gutterWidth
	for c := range col {
		x += r.g.model.Width(c)
	}
	x += r.g.model.Width(col) / 2
	return gui.Offset{Dx: x, Dy: headerHeight + float64(row)*r.g.opts.RowHeight + r.g.opts.RowHeight/2}
}

// headerAt is the place of the title of a column.
func (r *gridRig) headerAt(col int) gui.Offset {
	p := r.at(0, col)
	return gui.Offset{Dx: p.Dx - r.g.model.Width(col)/2 + 20, Dy: headerHeight / 2}
}

var mouse = gluttertest.GestureOpts{Kind: gui.PointerDeviceKindMouse}

func (r *gridRig) click(at gui.Offset) {
	r.t.Helper()
	r.ts.TapAt(at, mouse)
	r.pump()
}

func (r *gridRig) rightClick(at gui.Offset) {
	r.t.Helper()
	r.ts.TapAt(at, gluttertest.GestureOpts{Kind: gui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)})
	r.pump()
}

func (r *gridRig) key(k services.LogicalKeyboardKey, mods ...services.LogicalKeyboardKey) {
	r.t.Helper()
	for _, m := range mods {
		r.ts.SendKeyDownEvent(m)
	}
	r.ts.SendKeyEvent(k)
	for i := len(mods) - 1; i >= 0; i-- {
		r.ts.SendKeyUpEvent(mods[i])
	}
	r.pump()
}

func (r *gridRig) has(text string) bool { return r.ts.Exists(gluttertest.ByTextContaining(text)) }

var dark = uitest.Themes()[0]

func TestTheGridShowsTheTitlesAndTheRows(t *testing.T) {
	r := newGridRig(t, dark, gui.Size{Width: 700, Height: 200}, people())
	for _, want := range []string{"id", "name", "score", "born", "Alice", "carol", "9.25", "1985-01-02T00:00:00Z", "NULL", "4 rows"} {
		if !r.has(want) {
			t.Errorf("%q is not on screen", want)
		}
	}
	if !r.has("line one line two") {
		t.Error("a text with a line break is not shown on one line")
	}
	if !r.has("1") || !r.has("4") {
		t.Error("the row numbers are missing")
	}
}

func TestClickingSelectsCellsRowsAndAll(t *testing.T) {
	r := newGridRig(t, dark, gui.Size{Width: 700, Height: 200}, people())
	m := r.g.model
	r.click(r.at(1, 1))
	if sel := m.Selection(); sel.Kind != SelCells || sel.Focus != (Cell{1, 1}) {
		t.Fatalf("selection = %+v", sel)
	}
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyShiftLeft)
	r.click(r.at(3, 2))
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyShiftLeft)
	if sel := m.Selection(); sel.Anchor != (Cell{1, 1}) || sel.Focus != (Cell{3, 2}) || m.SelectedCells() != 6 {
		t.Fatalf("selection = %+v, %d cells", sel, m.SelectedCells())
	}
	if !r.has("6 cells selected") {
		t.Error("the footer does not say what is selected")
	}
	// the number of a row selects the row
	r.click(gui.Offset{Dx: 20, Dy: r.at(2, 0).Dy})
	if sel := m.Selection(); sel.Kind != SelRows || sel.Anchor.Row != 2 {
		t.Errorf("selection = %+v", sel)
	}
	// the corner selects all
	r.click(gui.Offset{Dx: 20, Dy: headerHeight / 2})
	if m.Selection().Kind != SelAll {
		t.Errorf("selection = %+v", m.Selection())
	}
}

func TestClickingATitleSortsTheColumn(t *testing.T) {
	r := newGridRig(t, dark, gui.Size{Width: 700, Height: 200}, people())
	m := r.g.model
	r.click(r.headerAt(0))
	if col, dir := m.SortState(); col != 0 || dir != SortAsc {
		t.Fatalf("sort = %d, %v", col, dir)
	}
	if got := column(m, 0); got[0] != "1" || got[3] != "10" {
		t.Errorf("column = %v", got)
	}
	if !r.has("sorted by id ascending") {
		t.Error("the footer does not say how the rows are sorted")
	}
	r.click(r.headerAt(0))
	if _, dir := m.SortState(); dir != SortDesc || column(m, 0)[0] != "10" {
		t.Errorf("not descending: %v", column(m, 0))
	}
	r.click(r.headerAt(0))
	if _, dir := m.SortState(); dir != SortNone || column(m, 0)[0] != "3" {
		t.Errorf("not back: %v", column(m, 0))
	}
}

func TestDraggingTheEdgeOfATitleResizesTheColumnAndTwoPressesFitIt(t *testing.T) {
	r := newGridRig(t, dark, gui.Size{Width: 900, Height: 200}, people())
	m := r.g.model
	before := m.Width(1)
	edge := gui.Offset{Dx: gutterWidth + m.Width(0) + m.Width(1) - 1, Dy: headerHeight / 2}
	g := r.ts.StartGesture(edge, mouse)
	g.MoveBy(gui.Offset{Dx: 60})
	g.Up()
	r.pump()
	if got := m.Width(1); got != before+60 {
		t.Fatalf("width = %v, want %v", got, before+60)
	}
	g = r.ts.StartGesture(gui.Offset{Dx: gutterWidth + m.Width(0) + m.Width(1) - 1, Dy: headerHeight / 2}, mouse)
	g.MoveBy(gui.Offset{Dx: -500})
	g.Up()
	r.pump()
	if got := m.Width(1); got != MinColumnWidth {
		t.Errorf("width = %v, want the minimum %v", got, MinColumnWidth)
	}
	// two quick presses fit the column to what it holds
	edge = gui.Offset{Dx: gutterWidth + m.Width(0) + m.Width(1) - 1, Dy: headerHeight / 2}
	r.ts.TapAt(edge, mouse)
	r.ts.Pump(100 * time.Millisecond)
	r.ts.TapAt(edge, mouse)
	r.pump()
	if got := m.Width(1); got <= MinColumnWidth {
		t.Errorf("width after two presses = %v", got)
	}
	if col, _ := m.SortState(); col != -1 {
		t.Error("a press on the edge sorted the column")
	}
}

func TestKeysMoveSelectAndCopy(t *testing.T) {
	r := newGridRig(t, dark, gui.Size{Width: 700, Height: 200}, people())
	m := r.g.model
	r.click(r.at(0, 0))
	r.key(services.LogicalKeyboardKeyArrowDown)
	r.key(services.LogicalKeyboardKeyArrowRight)
	if f, _ := m.Focus(); f != (Cell{1, 1}) {
		t.Fatalf("focus = %+v", f)
	}
	r.key(services.LogicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyShiftLeft)
	if m.SelectedCells() != 2 {
		t.Errorf("shift+down selected %d cells", m.SelectedCells())
	}
	r.key(services.LogicalKeyboardKeyKeyC, services.LogicalKeyboardKeyControlLeft)
	if len(r.copied) != 1 || r.copied[0] != "Alice\nbob" {
		t.Fatalf("copied = %q", r.copied)
	}
	r.key(services.LogicalKeyboardKeyKeyC, services.LogicalKeyboardKeyControlLeft, services.LogicalKeyboardKeyShiftLeft)
	if len(r.copied) != 2 || r.copied[1] != "name\nAlice\nbob" {
		t.Errorf("copied = %q", r.copied)
	}
	r.key(services.LogicalKeyboardKeyKeyA, services.LogicalKeyboardKeyControlLeft)
	if m.Selection().Kind != SelAll {
		t.Error("Ctrl+A did not select all")
	}
	r.key(services.LogicalKeyboardKeyKeyC, services.LogicalKeyboardKeyControlLeft)
	if got := r.copied[2]; !strings.HasPrefix(got, "3\tcarol\t7.5\t") || strings.Count(got, "\n") != 4 {
		t.Errorf("copied everything = %q", got)
	}
	r.key(services.LogicalKeyboardKeyEscape)
	if m.Selection().Kind != SelNone {
		t.Error("Escape did not clear")
	}
	r.key(services.LogicalKeyboardKeyArrowDown)
	r.key(services.LogicalKeyboardKeyEnd, services.LogicalKeyboardKeyControlLeft)
	if f, _ := m.Focus(); f != (Cell{3, 4}) {
		t.Errorf("Ctrl+End = %+v", f)
	}
	r.key(services.LogicalKeyboardKeyHome)
	if f, _ := m.Focus(); f != (Cell{3, 0}) {
		t.Errorf("Home = %+v", f)
	}
	r.key(services.LogicalKeyboardKeySpace, services.LogicalKeyboardKeyControlLeft)
	if m.Selection().Kind != SelColumns {
		t.Errorf("Ctrl+Space = %+v", m.Selection())
	}
	r.key(services.LogicalKeyboardKeySpace, services.LogicalKeyboardKeyShiftLeft)
	if m.Selection().Kind != SelRows {
		t.Errorf("Shift+Space = %+v", m.Selection())
	}
}

func TestTheContextMenuCopiesWhatItNames(t *testing.T) {
	r := newGridRig(t, dark, gui.Size{Width: 700, Height: 300}, people())
	r.rightClick(r.at(1, 1))
	for _, item := range []string{"Copy Cell", "Copy Row", "Copy Row with Headers", "Copy Column", "Select All"} {
		if !r.has(item) {
			t.Errorf("the menu has no %q", item)
		}
	}
	r.ts.Tap(gluttertest.ByText("Copy Row with Headers"))
	r.pump()
	if len(r.copied) != 1 || !strings.HasPrefix(r.copied[0], "id\tname\tscore\tborn\tnote\n1\tAlice\t9.25\t") {
		t.Fatalf("copied = %q", r.copied)
	}
	if got := r.g.model.Selection(); got.Kind != SelCells || got.Focus != (Cell{1, 1}) {
		t.Errorf("the right click did not select the cell: %+v", got)
	}
	// a right click inside a selection keeps it
	r.click(r.at(0, 0))
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyShiftLeft)
	r.click(r.at(2, 2))
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyShiftLeft)
	r.rightClick(r.at(1, 1))
	if r.g.model.SelectedCells() != 9 || !r.has("Copy Selection") {
		t.Errorf("selected %d cells, menu %v", r.g.model.SelectedCells(), r.has("Copy Selection"))
	}
	r.ts.Tap(gluttertest.ByText("Copy Selection"))
	r.pump()
	if got := r.copied[len(r.copied)-1]; got != "3\tcarol\t7.5\n1\tAlice\t9.25\n2\tbob\tNULL" {
		t.Errorf("copied = %q", got)
	}
	// the menu of a title
	r.rightClick(r.headerAt(1))
	if !r.has("Sort Descending") || !r.has("Copy Column with Header") {
		t.Error("the menu of a title is not shown")
	}
	r.ts.Tap(gluttertest.ByText("Sort Descending"))
	r.pump()
	if col, dir := r.g.model.SortState(); col != 1 || dir != SortDesc {
		t.Errorf("sort = %d, %v", col, dir)
	}
}

func TestFetchMoreShowsWhileThereAreMoreRows(t *testing.T) {
	tbl := people()
	r := newGridRig(t, dark, gui.Size{Width: 700, Height: 200}, tbl)
	if r.has("Fetch More") {
		t.Error("the button shows with nothing more to fetch")
	}
	r.g.SetMore(true)
	r.pump()
	if !r.has("Fetch More") {
		t.Fatal("no button")
	}
	r.ts.Tap(gluttertest.ByText("Fetch More"))
	if r.fetches != 1 {
		t.Errorf("fetches = %d", r.fetches)
	}
	tbl.Append([]Value{Int(5), Text("erin")})
	r.g.Appended()
	r.g.SetMore(false)
	r.pump()
	if r.has("Fetch More") || !r.has("5 rows") || !r.has("erin") {
		t.Errorf("after the fetch: button %v, 5 rows %v", r.has("Fetch More"), r.has("5 rows"))
	}
	// a grid with no OnFetchMore never shows the button
	r2 := newGridRig(t, dark, gui.Size{Width: 700, Height: 200}, people(), func(o *Options) { o.OnFetchMore = nil })
	r2.g.SetMore(true)
	r2.pump()
	if r2.has("Fetch More") {
		t.Error("a button with no handler")
	}
}

func TestAnEmptyResult(t *testing.T) {
	r := newGridRig(t, dark, gui.Size{Width: 400, Height: 120}, NewTable([]Column{{Name: "a"}, {Name: "b"}}))
	if !r.has("No rows.") || !r.has("0 rows") || !r.has("a") {
		t.Error("an empty result does not say so")
	}
}

func TestHundredThousandRowsBuildOnlyTheRowsInView(t *testing.T) {
	r := newGridRig(t, dark, gui.Size{Width: 700, Height: 400}, numbered(100_000))
	inView := 17
	if built := r.g.RowsBuilt(); built == 0 || built > 3*inView {
		t.Fatalf("the first frame built %d rows, %d are in view", built, inView)
	}
	if !r.has("100,000 rows") || !r.has("row 0") || r.has("row 100") {
		t.Error("the first rows are not shown, or too many are")
	}
	// the wheel moves through the rows; each step builds only what comes into view
	before := r.g.RowsBuilt()
	for range 20 {
		r.ts.ScrollAt(gui.Offset{Dx: 300, Dy: 200}, gui.Offset{Dy: 530})
		r.pump()
	}
	if built := r.g.RowsBuilt() - before; built > 20*(inView+8) {
		t.Errorf("20 wheel steps built %d rows", built)
	}
	if top := r.g.vCtl.Offset(); top < 20*400 {
		t.Errorf("the wheel scrolled to %v after 20 notches", top)
	}
	if r.has("row 0") {
		t.Error("the first row is still in view after the wheel")
	}
	// the end of the result is reached by jumping, without building what is between
	before = r.g.RowsBuilt()
	r.key(services.LogicalKeyboardKeyArrowDown) // starts the selection at the first cell
	r.key(services.LogicalKeyboardKeyEnd, services.LogicalKeyboardKeyControlLeft)
	if f, _ := r.g.model.Focus(); f.Row != 99_999 {
		t.Fatalf("focus = %+v", f)
	}
	if built := r.g.RowsBuilt() - before; built > 8*inView {
		t.Errorf("jumping to the end built %d rows", built)
	}
	if !r.has("row 99999") {
		t.Error("the last row is not in view after Ctrl+End")
	}
}

func TestSortingAHugeResultStaysOnTheModel(t *testing.T) {
	m := NewModel(ModelOptions{})
	m.SetSource(numbered(100_000))
	start := time.Now()
	m.SortBy(0, SortDesc)
	if got := m.CopyCell(0, 0); got != "99999" {
		t.Errorf("first = %q", got)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("sorting 100,000 rows took %v", took)
	}
}

func TestGoldenTheGrid(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			tbl := NewTable([]Column{{Name: "id", Type: "integer"}, {Name: "name", Type: "text"}, {Name: "score", Type: "double precision"}, {Name: "born", Type: "date"}, {Name: "avatar", Type: "bytea"}, {Name: "bio", Type: "text"}})
			day := func(y int, m time.Month, d int) Value { return Time(time.Date(y, m, d, 0, 0, 0, 0, time.UTC)) }
			png := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, 3000)...)
			long := Text(strings.Repeat("The quick brown fox jumps over the lazy dog. ", 12))
			tbl.Append(
				[]Value{Int(1), Text("Alice"), Float(9.25), day(1985, 1, 2), Bytes(png), long},
				[]Value{Int(2), Text("Bob"), Null(), day(2001, 12, 31), Null(), Text("multi\nline\ntext")},
				[]Value{Int(3), Null(), Float(-1.5e-7), Null(), Bytes([]byte{0xde, 0xad, 0xbe, 0xef}), Text("")},
				[]Value{Int(1_000_000_007), Text("Ünïcödé ñandú"), Float(100), day(1969, 7, 20), Null(), Null()},
				[]Value{Int(5), Text("Eve"), Float(0.1), day(2030, 2, 28), Bytes([]byte{1}), Text("x")},
			)
			r := newGridRig(t, th, gui.Size{Width: 1240, Height: 220}, tbl)
			r.g.SetMore(true)
			r.click(r.at(1, 1))
			r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyShiftLeft)
			r.click(r.at(2, 2))
			r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyShiftLeft)
			r.click(r.headerAt(3))
			r.click(r.at(3, 3))
			r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyShiftLeft)
			r.click(r.at(4, 5))
			r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyShiftLeft)
			uitest.MatchesGolden(r.ts, "grid_"+th.Name)
		})
	}
}

// TestTheTextOfASelectionThatLostTheFocusStaysVisible: the themes define no list.inactiveSelectionForeground, so the text of a
// selected cell must keep its own color when the grid is not focused (a button next to the grid takes the focus).
func TestTheTextOfASelectionThatLostTheFocusStaysVisible(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newGridRig(t, th, gui.Size{Width: 700, Height: 200}, people())
			r.click(r.at(1, 1))
			r.g.focus.Unfocus()
			r.pump()
			if r.g.model.Selection().Kind != SelCells {
				t.Fatal("the selection was lost")
			}
			text := r.ts.Widget(gluttertest.ByText("Alice")).(*widgets.Text)
			if c := text.Style().Color.OrElse(gui.Color{}); c.Alpha() == 0 {
				t.Errorf("the text of the selected cell is transparent: %v", c)
			}
		})
	}
}
