package editor

import (
	"math"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// zoneRig is an editor of numbered lines in the window of a mouse rig, with the layout settled.
func zoneRig(t *testing.T, lines int) *mouseRig {
	t.Helper()
	r := newMouseRig(t, linesText(lines, "word"), func(o *Options) { o.RenderLineHighlight = "all" })
	t.Cleanup(r.vm.DisposeZones)
	r.settle()
	return r
}

func (r *mouseRig) addZone(spec ZoneSpec) ZoneID {
	r.t.Helper()
	id, err := r.vm.AddZone(spec)
	if err != nil {
		r.t.Fatal(err)
	}
	r.settle()
	return id
}

func TestZoneRowsAreInTheScrollExtent(t *testing.T) {
	r := zoneRig(t, 60)
	before, rows := r.vm.ContentHeight(), r.vm.Projection().RowCount()
	r.addZone(ZoneSpec{AfterLine: 3, Rows: 4})
	if got := r.vm.ContentHeight(); got != before+4*r.lineH {
		t.Errorf("content height %v, want %v", got, before+4*r.lineH)
	}
	if got := r.vm.Projection().RowCount(); got != rows+4 {
		t.Errorf("rows %d, want %d", got, rows+4)
	}
	// the scrollbar's model sees the same extent
	if m := r.editorState(t).scroll.Metrics(); m.ContentHeight != before+4*r.lineH {
		t.Errorf("scroll metrics content height %v", m.ContentHeight)
	}
	noErrors(t, r.ts)
}

func TestRevealingALineAfterAZone(t *testing.T) {
	r := zoneRig(t, 80)
	r.addZone(ZoneSpec{AfterLine: 3, Rows: 2})
	r.addZone(ZoneSpec{AfterLine: 40, Rows: 3})
	for _, line := range []int{60, 10, 79} {
		r.vm.RevealPosition(buffer.Position{Line: line})
		r.settle()
		row := r.vm.RowOfPosition(buffer.Position{Line: line})
		want := line + 2 // the zones above the line
		if line > 39 {
			want += 3
		}
		if row != want {
			t.Fatalf("line %d is on row %d, want %d", line, row, want)
		}
		proj := r.vm.Projection()
		_, vh := r.vm.ViewportSize()
		if top, bottom := proj.RowTop(row), proj.RowTop(row)+proj.RowHeight(row); top < r.vm.ScrollTop() || bottom > r.vm.ScrollTop()+vh {
			t.Errorf("line %d (rows %v-%v) is not in the viewport %v-%v", line, top, bottom, r.vm.ScrollTop(), r.vm.ScrollTop()+vh)
		}
	}
	noErrors(t, r.ts)
}

func TestClickingOnAZoneLeavesTheCursorAlone(t *testing.T) {
	r := zoneRig(t, 30)
	id := r.addZone(ZoneSpec{AfterLine: 3, Rows: 2}) // rows 3 and 4
	var hits []ZoneHit
	r.vm.SetZonePressHandler(func(h ZoneHit) { hits = append(hits, h) })
	r.click(r.at(1, 4))
	r.want(sel(1, 4, 1, 4))

	r.click(r.at(3, 2)) // the first row of the zone
	r.click(r.at(4, 30))
	r.want(sel(1, 4, 1, 4))
	if len(hits) != 2 || hits[0] != (ZoneHit{ID: id, Index: 0, FirstRow: 3, Rows: 2}) || hits[1].Index != 1 {
		t.Errorf("zone hits %+v", hits)
	}
	// in the gutter: the line number column and the folding column
	r.click(ui.Offset{Dx: r.numberAt(3).Dx, Dy: 3*r.lineH + 5})
	r.click(ui.Offset{Dx: r.gutter - 3, Dy: 4*r.lineH + 5})
	r.want(sel(1, 4, 1, 4))
	if len(hits) != 4 {
		t.Errorf("zone hits %+v", hits)
	}

	// text below the zone is the line after it, and the line above is the line before
	r.click(r.at(5, 3))
	r.want(sel(3, 3, 3, 3))
	r.click(r.at(2, 3))
	r.want(sel(2, 3, 2, 3))
	if len(hits) != 4 {
		t.Errorf("a press on text told the zone: %+v", hits)
	}
	// a double click on a zone selects no word, and a secondary press moves no cursor
	r.clicks(r.at(3, 2), 2)
	r.want(sel(2, 3, 2, 3))
	if hit, ok := r.vm.ZoneAtY(3*r.lineH + 1); !ok || hit.ID != id {
		t.Errorf("ZoneAtY: %+v %v", hit, ok)
	}
	if _, ok := r.vm.ZoneAtY(2 * r.lineH); ok {
		t.Error("a text row is a zone")
	}
	if _, ok := r.vm.ZoneAtY(-3); ok {
		t.Error("above the document is a zone")
	}
	if _, ok := r.vm.ZoneAtY(1e6); ok {
		t.Error("below the document is a zone")
	}
	if hit, ok := r.vm.ZoneAtRow(4); !ok || hit.Index != 1 {
		t.Errorf("ZoneAtRow: %+v %v", hit, ok)
	}
	noErrors(t, r.ts)
}

func TestVerticalMovementSkipsZoneRows(t *testing.T) {
	r := zoneRig(t, 60)
	r.addZone(ZoneSpec{AfterLine: 3, Rows: 2})
	r.addZone(ZoneSpec{AfterLine: 3, Rows: 1, Ordinal: 3})
	r.addZone(ZoneSpec{AfterLine: 0, Rows: 1})
	cursorAt(r.vm, 2, 4)
	run := func(id string) {
		t.Helper()
		if err := r.vm.RunCommand(id, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
	}
	run("cursorDown")
	r.want(sel(3, 4, 3, 4))
	run("cursorUp")
	r.want(sel(2, 4, 2, 4))
	cursorAt(r.vm, 0, 0)
	run("cursorUp")
	r.want(sel(0, 0, 0, 0))

	// a page is the rows of text the viewport shows, without the rows of zones
	_, vh := r.vm.ViewportSize()
	rows := int(vh/r.lineH) - 1
	zonesInView := r.vm.Projection().ZoneRowsBefore(int(vh/r.lineH) + 1)
	run("cursorPageDown")
	if got := r.vm.Cursors().Primary().Position().Line; got != rows-zonesInView {
		t.Errorf("a page down went to line %d, want %d (%d rows, %d of them zones)", got, rows-zonesInView, rows, zonesInView)
	}
	noErrors(t, r.ts)
}

func TestDraggingAcrossAZone(t *testing.T) {
	r := zoneRig(t, 30)
	r.addZone(ZoneSpec{AfterLine: 3, Rows: 2}) // rows 3 and 4, between lines 2 and 3
	cases := []struct {
		name string
		to   ui.Offset
		want buffer.Selection
	}{
		{"to the text below", r.at(5, 3), sel(1, 2, 3, 3)},
		{"to the upper half of the zone: the line above", r.at(3, 3), sel(1, 2, 2, 3)},
		{"to the lower half of the zone: the line below", r.at(4, 3), sel(1, 2, 3, 3)},
		{"to the gutter of the zone", ui.Offset{Dx: 3, Dy: 4*r.lineH + 15}, sel(1, 2, 3, 0)},
		{"to the middle of the zone, between its rows", ui.Offset{Dx: r.at(0, 3).Dx, Dy: 4 * r.lineH}, sel(1, 2, 3, 3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r.click(r.at(0, 0))
			r.drag(r.at(1, 2), tc.to)
			r.want(tc.want)
		})
	}
	// a drag that starts above the zone and ends below selects the lines between, not the zone
	r.click(r.at(0, 0))
	r.drag(r.at(2, 1), r.at(6, 2))
	r.want(sel(2, 1, 4, 2))
	if got := r.vm.Document().Snapshot().Len(); got == 0 {
		t.Fatal("no text")
	}
	noErrors(t, r.ts)
}

func TestSelectionsAndTheCurrentLineSkipZoneRows(t *testing.T) {
	r := zoneRig(t, 30)
	r.addZone(ZoneSpec{AfterLine: 3, Rows: 2})
	geo := newGeometry(r.vm, r.ts.Dispatcher().TextEngine())
	rows := geo.selectionRows(sel(2, 0, 4, 2), 0, 20, true)
	var got []int
	for _, row := range rows {
		got = append(got, row.Row)
	}
	if len(got) != 3 || got[0] != 2 || got[1] != 5 || got[2] != 6 {
		t.Fatalf("selection rows %v, want 2 5 6", got)
	}
	// the rows on either side of the zone do not touch: the corners next to it are outer corners
	if rows[0].Start.Bottom != cornerExtern || rows[0].End.Bottom != cornerExtern || rows[1].Start.Top != cornerExtern || rows[1].End.Top != cornerExtern {
		t.Errorf("the rows around the zone are styled as neighbours: %+v %+v", rows[0], rows[1])
	}
	if rows[1].Start.Bottom == cornerExtern && rows[2].Start.Top == cornerExtern {
		t.Errorf("the rows below the zone are not styled as neighbours: %+v %+v", rows[1], rows[2])
	}

	cursorAt(r.vm, 3, 2)
	if got := cursorRows(r.vm); len(got) != 1 || got[0] != 5 {
		t.Errorf("the current line is on the rows %v, want [5]", got)
	}
	cursorAt(r.vm, 2, 2)
	if got := cursorRows(r.vm); len(got) != 1 || got[0] != 2 {
		t.Errorf("the current line is on the rows %v, want [2]", got)
	}
	noErrors(t, r.ts)
}

func TestZonesWithWrappedLines(t *testing.T) {
	r := newWrapRig(t, ui.Size{Width: 440, Height: 400}, wrapWidgetText, nil)
	t.Cleanup(r.vm.DisposeZones)
	first, count := r.vm.Projection().LineRows(0)
	if count < 2 {
		t.Fatalf("line 0 has %d rows", count)
	}
	r.addZone(ZoneSpec{AfterLine: 1, Rows: 2})
	zoneFirst := first + count
	if got, _ := r.vm.Projection().ZoneRows(1); got != zoneFirst {
		t.Fatalf("the zone is at row %d, want %d", got, zoneFirst)
	}
	r.click(r.at(1, 4)) // the second row of line 0
	r.want(sel(0, r.vm.Cursors().Primary().Position().Col, 0, r.vm.Cursors().Primary().Position().Col))
	before := r.selections()
	r.click(r.at(zoneFirst, 3))
	r.want(before...)
	r.click(r.at(zoneFirst+2, 3))
	r.want(sel(1, 3, 1, 3))
	// the current line highlight covers all the rows of the wrapped line, but not the zone
	cursorAt(r.vm, 0, 1)
	if got := cursorRows(r.vm); len(got) != count || got[len(got)-1] != first+count-1 {
		t.Errorf("rows of the current line %v, line rows %d+%d", got, first, count)
	}
	noErrors(t, r.ts)
}

func TestFoldingHidesTheZonesInTheFoldedLines(t *testing.T) {
	r := newFoldRig(t, nil)
	t.Cleanup(r.vm.DisposeZones)
	// func a is lines 0 to 5 (the fold hides 1 to 4: the closing brace stays), func b lines 6 to 8
	r.addZone(ZoneSpec{AfterLine: 3, Rows: 1})              // inside the fold: hidden
	r.addZone(ZoneSpec{AfterLine: 1, Rows: 1})              // after the line that holds the fold: hidden
	afterFold := r.addZone(ZoneSpec{AfterLine: 6, Rows: 2}) // after the closing brace, which stays
	forced := r.addZone(ZoneSpec{AfterLine: 4, Rows: 1, ShowInHiddenAreas: true})
	if got := r.vm.Projection().RowCount(); got != 10+1+1+2+1 {
		t.Fatalf("rows %d", got)
	}
	runFold(t, r.vm, "editor.fold", map[string]any{"selectionLines": []any{0.0}})
	r.ts.Pump(1)
	want := "L0.0 Z4.0 L5.0 Z3.0 Z3.1 L6.0 L7.0 L8.0 L9.0"
	if got := rowStrings(r.vm); got != want {
		t.Fatalf("folded: %s, want %s", got, want)
	}
	for _, info := range r.vm.Zones() {
		wantRows := 0
		if info.ID == afterFold || info.ID == forced {
			wantRows = info.Rows
		}
		if info.ShownRows != wantRows {
			t.Errorf("zone %d (after line %d) shows %d rows when folded, want %d", info.ID, info.AfterLine, info.ShownRows, wantRows)
		}
	}
	runFold(t, r.vm, "editor.unfoldAll", nil)
	r.ts.Pump(1)
	if got := r.vm.Projection().RowCount(); got != 10+1+1+2+1 {
		t.Errorf("unfolded: %d rows", got)
	}
	checkProjection(t, r.vm.Projection(), r.vm.baseProj)
	noErrors(t, r.ts)
}

// probeZone records the constraints the widget of a zone is given.
type probeZone struct {
	constraints rendering.BoxConstraints
}

// widget is a colored box that tells the probe how much room it has.
func (p *probeZone) widget(color ui.Color) widgets.Widget {
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		p.constraints = c
		return widgets.NewColoredBox(color, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{}))
	})
}

func TestAZoneWidgetIsLaidOutInItsRowsAndScrolls(t *testing.T) {
	r := zoneRig(t, 80)
	probe := &probeZone{}
	id := r.addZone(ZoneSpec{AfterLine: 4, Rows: 3, Widget: probe.widget(ui.NewColor(0xFF2060A0))})
	key := gluttertest.ByKey(foundation.NewValueKey(zoneKey(id)))
	textTop := r.ts.TopLeft(gluttertest.ByType[*textLayer]())
	textSize := r.ts.Size(gluttertest.ByType[*textLayer]())

	if !r.ts.Exists(key) {
		t.Fatal("the zone has no widget on screen")
	}
	if probe.constraints.MaxHeight != 3*r.lineH || probe.constraints.MaxWidth != textSize.Width {
		t.Errorf("the widget is given %v x %v, want %v x %v", probe.constraints.MaxWidth, probe.constraints.MaxHeight, textSize.Width, 3*r.lineH)
	}
	if got := r.ts.TopLeft(key); got.Dx != textTop.Dx || math.Abs(got.Dy-(textTop.Dy+4*r.lineH)) > 0.01 {
		t.Errorf("the widget is at %v, want %v", got, ui.Offset{Dx: textTop.Dx, Dy: textTop.Dy + 4*r.lineH})
	}
	// scrolling moves it with the text
	r.vm.scroller(2 * r.lineH)
	r.settle()
	if got := r.ts.TopLeft(key); math.Abs(got.Dy-(textTop.Dy+2*r.lineH)) > 0.01 {
		t.Errorf("after scrolling by two rows the widget is at %v", got)
	}
	// scrolled out of view, it is not built; scrolled back, it is there again
	r.vm.scroller(60 * r.lineH)
	r.settle()
	if r.ts.Exists(key) {
		t.Error("a widget far from the viewport is built")
	}
	r.vm.scroller(0)
	r.settle()
	if !r.ts.Exists(key) {
		t.Error("the widget did not come back")
	}
	// a taller zone gives the widget more room, a removed one takes it away
	if err := r.vm.SetZoneRows(id, 5); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if probe.constraints.MaxHeight != 5*r.lineH {
		t.Errorf("height %v after the zone grew", probe.constraints.MaxHeight)
	}
	if err := r.vm.RemoveZone(id); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if r.ts.Exists(key) {
		t.Error("the widget of a removed zone is still there")
	}
	noErrors(t, r.ts)
}

func TestAPressOnAZoneWidgetDoesNotMoveTheCursor(t *testing.T) {
	r := zoneRig(t, 30)
	pressed := 0
	button := widgets.NewGestureDetector(widgets.NewColoredBox(ui.NewColor(0xFF804020), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{})),
		widgets.GestureDetectorOpts{OnTap: func() { pressed++ }})
	r.addZone(ZoneSpec{AfterLine: 3, Rows: 2, Widget: button})
	r.click(r.at(1, 2))
	r.click(r.at(3, 5))
	r.want(sel(1, 2, 1, 2))
	if pressed != 1 {
		t.Errorf("the widget in the zone was tapped %d times", pressed)
	}
	noErrors(t, r.ts)
}

func TestMinimapRulerAndScrollbarWithZones(t *testing.T) {
	r := newMinimapRig(t, ui.Size{Width: 640, Height: 300}, minimapSample(40), nil)
	t.Cleanup(r.vm.DisposeZones)
	contentBefore := r.vm.ContentHeight()
	rowsBefore := r.vm.Projection().RowCount()
	if _, err := r.vm.AddZone(ZoneSpec{AfterLine: 10, Rows: 6}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.vm.AddZone(ZoneSpec{AfterLine: 100, Rows: 3}); err != nil {
		t.Fatal(err)
	}
	r.settle()
	proj := r.vm.Projection()
	if proj.RowCount() != rowsBefore+9 {
		t.Fatalf("rows %d", proj.RowCount())
	}
	if got := r.vm.ContentHeight(); got != contentBefore+9*19 {
		t.Errorf("content height %v, want %v", got, contentBefore+9*19)
	}
	// the minimap has a line for every row, the rows of zones blank
	if got := r.vm.minimapLineCount(); got != proj.RowCount() {
		t.Errorf("minimap lines %d, rows %d", got, proj.RowCount())
	}
	m := r.render()
	snap := r.vm.Document().Snapshot()
	for row := range proj.RowCount() {
		if _, _, zone := proj.RowZone(row); zone {
			if data := m.rowData(snap, row); data.text != "" || len(data.spans) != 0 {
				t.Errorf("the minimap row %d of a zone has text %q", row, data.text)
			}
		}
		if got := r.vm.minimapRowOf(r.vm.minimapLineOfRow(row)); got != row {
			t.Errorf("row %d is on the minimap line %d, which shows row %d", row, r.vm.minimapLineOfRow(row), got)
		}
	}
	// the slider follows the scroll through the rows with the zones
	l := r.vm.mm.layout
	if !l.SliderNeeded {
		t.Fatal("no slider")
	}
	r.vm.scroller(r.vm.ScrollTop() + 100*19)
	r.settle()
	if now := r.vm.mm.layout; now.SliderTop <= l.SliderTop {
		t.Errorf("the slider did not move: %v then %v", l.SliderTop, now.SliderTop)
	}
	// a selection over the zone is marked on the lines of text only
	selectText(r.vm, 8, 0, 14, 3)
	r.settle()
	noErrors(t, r.ts)
}

func TestOverviewRulerWithZones(t *testing.T) {
	vm, _ := newFindVM(t, linesText(100, "match"), nil)
	t.Cleanup(vm.DisposeZones)
	vm.SetViewportHeight(400)
	if _, err := vm.AddZone(ZoneSpec{AfterLine: 10, Rows: 5}); err != nil {
		t.Fatal(err)
	}
	vm.SetTextMetrics(600, 400, (100+5)*19, 0, 8)
	if err := vm.RunCommand("editor.actions.findWithArgs", map[string]any{"searchString": "match"}); err != nil {
		t.Fatal(err)
	}
	marks := vm.rulerMarks(400)
	scale := 400 / vm.ContentHeight()
	proj := vm.Projection()
	found := 0
	for _, line := range []int{5, 9, 10, 50} {
		row := vm.RowOfPosition(buffer.Position{Line: line})
		y := math.Round(proj.RowTop(row) * scale)
		for _, m := range marks {
			if m.lane == laneCenter && m.y == y {
				found++
				break
			}
		}
	}
	if found != 4 {
		t.Errorf("%d of 4 matches have their mark where the rows put it", found)
	}
	// the line after the zone is 5 rows lower than without it
	row9, row10 := vm.RowOfPosition(buffer.Position{Line: 9}), vm.RowOfPosition(buffer.Position{Line: 10})
	if row10-row9 != 6 {
		t.Errorf("lines 9 and 10 are on rows %d and %d", row9, row10)
	}
}

func TestStickyLinesWithZones(t *testing.T) {
	r := newStickyRig(t, ui.Size{Width: 640, Height: 300}, nil)
	t.Cleanup(r.vm.DisposeZones)
	// a zone above the function and one inside its body push the lines down; the scopes that stick are
	// those of the text at the top of the editor, whatever the rows in between
	for _, spec := range []ZoneSpec{{AfterLine: 2, Rows: 3}, {AfterLine: 12, Rows: 2}} {
		if _, err := r.vm.AddZone(spec); err != nil {
			t.Fatal(err)
		}
	}
	r.settle()
	r.scrollRows(12 + 3)
	got := r.vm.stickyLinesAt(r.vm.ScrollTop())
	if want := []int{2, 3, 4, 5}; len(got.Lines) != len(want) || got.Lines[0] != 2 || got.Lines[3] != 5 {
		t.Fatalf("sticky lines %v, want %v", got.Lines, want)
	}
	// scrolled to the zone inside the body, the same scopes stick
	row := r.vm.RowOfPosition(buffer.Position{Line: 13})
	r.scrollRows(float64(row))
	if got := r.vm.stickyLinesAt(r.vm.ScrollTop()); len(got.Lines) != 4 {
		t.Errorf("sticky lines at the zone in the body: %v", got.Lines)
	}
	// the widget is as high as its lines
	if h := r.layer().Size().Height; h != 4*19+stickyShadowHeight {
		t.Errorf("widget height %v", h)
	}
	noErrors(t, r.ts)
}

// zoneGoldenText has a line that wraps in the narrow editor, two functions that fold, and room for
// zones between the lines.
const zoneGoldenText = "// Package demo shows view zones: a comment that is long enough to wrap in this narrow editor.\n" +
	"func a() {\n" +
	"\tx := 1\n" +
	"\tif x > 0 {\n" +
	"\t\ty()\n" +
	"\t}\n" +
	"}\n" +
	"func b() {\n" +
	"\tz()\n" +
	"}\n" +
	"last"

// zoneGoldenEditor shows zoneGoldenText wrapped and with func b folded, with three zones: blank rows
// after the comment, a colored widget after `x := 1`, and a zone after func b's first line, which the
// fold hides.
func zoneGoldenEditor(t *testing.T, colors kit.Colors) (*gluttertest.Tester, *ViewModel) {
	t.Helper()
	opts := goOptions()
	opts.WordWrap, opts.WrappingIndent, opts.WrappingStrategy, opts.StopRenderingLineAfter = "on", "same", "simple", 10000
	opts.RenderLineHighlight = "all"
	vm := NewViewModel(newTestDoc(zoneGoldenText), opts, nil)
	t.Cleanup(vm.DisposeWrap)
	t.Cleanup(vm.DisposeZones)
	vm.StartFolding(nil, 0)
	computeFolds(t, vm)
	ts := testutil.New(t, ui.Size{Width: 520, Height: 360})
	pumpEditorColors(t, ts, vm, EditorOpts{Autofocus: true}, colors)
	r := &mouseRig{t: t, ts: ts, vm: vm}
	r.settle()
	runFold(t, vm, "editor.fold", map[string]any{"selectionLines": []any{7.0}})
	box := widgets.NewColoredBox(ui.NewColor(0xFF2D6A4F), widgets.NewCenter(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(140.0), Height: foundation.Some(30.0)})))
	for _, spec := range []ZoneSpec{
		{AfterLine: 1, Rows: 2},
		{AfterLine: 3, Rows: 3, Widget: box},
		{AfterLine: 8, Rows: 2},
	} {
		if _, err := vm.AddZone(spec); err != nil {
			t.Fatal(err)
		}
	}
	r.settle()
	return ts, vm
}

func TestGoldenZones(t *testing.T) {
	for _, tc := range []struct {
		name     string
		colors   func(*testing.T) kit.Colors
		selected bool
	}{
		{"zones_selection_dark", func(*testing.T) kit.Colors { return kit.DarkModern() }, true},
		{"zones_cursor_dark", func(*testing.T) kit.Colors { return kit.DarkModern() }, false},
		{"zones_selection_light", func(t *testing.T) kit.Colors { return bundledColors(t, "Light Modern") }, true},
		{"zones_cursor_light", func(t *testing.T) kit.Colors { return bundledColors(t, "Light Modern") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts, vm := zoneGoldenEditor(t, tc.colors(t))
			if tc.selected {
				// from func a, over the zone with the widget, into the if
				selectText(vm, 1, 3, 4, 3)
			} else {
				// a cursor on the first row of the line that wraps: its highlight covers both rows, not the zone
				cursorAt(vm, 0, 5)
			}
			vm.SelectionsChanged()
			for range 3 {
				ts.Pump(time.Millisecond)
			}
			noErrors(t, ts)
			testutil.MatchesGolden(ts, tc.name)
		})
	}
}
