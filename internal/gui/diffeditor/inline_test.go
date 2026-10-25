package diffeditor

import (
	"context"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// inlineSettings make the rig show the inline view whatever its width.
var inlineSettings = map[string]any{"diffEditor.renderSideBySide": false}

func TestInlineZonesOfTheDeletedLines(t *testing.T) {
	tests := []struct {
		name               string
		original, modified string
		want               []InlineZone
	}{
		{"a pure deletion", "1\n2\n3\n4\n", "1\n4\n", []InlineZone{{Change: 0, AfterLine: 1, Rows: 2}}},
		{"a replacement", "1\n2\n3\n", "1\ntwo\n3\n", []InlineZone{{Change: 0, AfterLine: 1, Rows: 1}}},
		{"a replacement of more lines by fewer", "1\na\nb\nc\n5\n", "1\nx\n5\n", []InlineZone{{Change: 0, AfterLine: 1, Rows: 3}}},
		{"a replacement of fewer lines by more", "1\na\n5\n", "1\nx\ny\nz\n5\n", []InlineZone{{Change: 0, AfterLine: 1, Rows: 1}}},
		{"deleted lines at the start of the file", "gone\ngone too\nkeep\n", "keep\n", []InlineZone{{Change: 0, AfterLine: 0, Rows: 2}}},
		{"deleted lines at the end of the file", "keep\ngone\nmore\n", "keep\n", []InlineZone{{Change: 0, AfterLine: 1, Rows: 2}}},
		{"an insertion has no zone", "1\n2\n", "1\nnew\n2\n", nil},
		{
			"several changes", "1\n2\n3\n4\n5\n6\n7\n8\n",
			"1\ntwo\n3\n4\n5\n7\n8\n",
			[]InlineZone{{Change: 0, AfterLine: 1, Rows: 1}, {Change: 1, AfterLine: 5, Rows: 1}},
		},
		{
			"adjacent changes", "a\n1\nb\n2\nc\n3\nd\n4\ne\n", "a\nb\nNEW\nc\nd\ne\n",
			[]InlineZone{{Change: 0, AfterLine: 1, Rows: 1}, {Change: 1, AfterLine: 2, Rows: 1}, {Change: 2, AfterLine: 4, Rows: 1}, {Change: 3, AfterLine: 5, Rows: 1}},
		},
		{"no changes", "1\n2\n", "1\n2\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.original, tt.modified, rigConfig{settings: inlineSettings})
			r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
			got := r.model.InlineZones()
			if len(got) != len(tt.want) {
				t.Fatalf("zones %+v, want %+v (changes %+v)", got, tt.want, r.model.Changes())
			}
			for i := range got {
				// the index of the change is checked against the change's own lines, not the table
				c := r.model.Changes()[got[i].Change]
				if got[i].AfterLine != c.Modified.StartLine-1 || got[i].Rows != c.Original.Len() {
					t.Errorf("zone %d is %+v, its change is %+v", i, got[i], c)
				}
				if got[i].AfterLine != tt.want[i].AfterLine || got[i].Rows != tt.want[i].Rows {
					t.Errorf("zone %d is %+v, want %+v", i, got[i], tt.want[i])
				}
			}
			// the zones are in the modified editor; the original editor has none
			if n := len(r.model.View(Original).Zones()); n != 0 {
				t.Errorf("the original editor has %d zones", n)
			}
			mod := r.model.View(Modified)
			rows := 0
			for _, z := range tt.want {
				rows += z.Rows
			}
			if want := mod.Document().Snapshot().LineCount() + rows; mod.Projection().RowCount() != want {
				t.Errorf("the editor has %d rows, want %d", mod.Projection().RowCount(), want)
			}
			r.noErrors()
		})
	}
}

func TestTheInlineViewHasOneEditorAndTheRulerOnly(t *testing.T) {
	r := newRig(t, "a\nb\nc\n", "a\nB\nc\n", rigConfig{settings: inlineSettings})
	r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
	editors := r.ts.Widgets(gluttertest.ByType[*editor.Editor]())
	if len(editors) != 1 {
		t.Fatalf("%d editors", len(editors))
	}
	if editors[0].(*editor.Editor).ViewModel() != r.model.View(Modified) {
		t.Error("the editor is not the modified one")
	}
	if w := r.ts.Size(gluttertest.ByWidget(editors[0])).Width; w < 800 {
		t.Errorf("the editor is %v wide: it takes the width of the diff editor but the ruler", w)
	}
	r.noErrors()
}

func TestTheDeletedLinesAreDrawnByAZoneWidget(t *testing.T) {
	r := newRig(t, "keep\nold value\nkeep\n", "keep\nnew value\nkeep\n", rigConfig{settings: inlineSettings})
	r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
	zones := r.model.View(Modified).Zones()
	if len(zones) != 1 || zones[0].Widget == nil || zones[0].Rows != 1 {
		t.Fatalf("zones %+v", zones)
	}
	if zones[0].GutterGlyph == 0 || zones[0].Gutter.Alpha() == 0 {
		t.Errorf("the margin of the zone has no sign or fill: %+v", zones[0])
	}
}

func TestInlineModeFollowsTheSettingsAndTheWidth(t *testing.T) {
	type state struct {
		sideBySide, useInline bool
		width                 float64
	}
	tests := []struct {
		name        string
		state       state
		wantMode    Mode
		wantReached bool
	}{
		{"wide, both on", state{true, true, 1000}, SideBySide, false},
		{"narrow, both on", state{true, true, 800}, Inline, true},
		{"exactly the breakpoint", state{true, true, 900}, Inline, true},
		{"narrow, the narrow setting off", state{true, false, 800}, SideBySide, true},
		{"wide, side by side off", state{false, true, 1000}, Inline, false},
		{"narrow, side by side off", state{false, false, 800}, Inline, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, "a\nb\nc\n", "a\nB\nc\n", rigConfig{
				size:     ui.Size{Width: tt.state.width, Height: 300},
				settings: map[string]any{"diffEditor.renderSideBySide": tt.state.sideBySide, "diffEditor.useInlineViewWhenSpaceIsLimited": tt.state.useInline},
			})
			r.waitFor("the mode", func() bool { return r.model.Mode() == tt.wantMode })
			if got := r.model.BreakpointReached(); got != tt.wantReached {
				t.Errorf("breakpoint reached %v, want %v", got, tt.wantReached)
			}
			wantEditors := 2
			if tt.wantMode == Inline {
				wantEditors = 1
			}
			if n := len(r.ts.Widgets(gluttertest.ByType[*editor.Editor]())); n != wantEditors {
				t.Errorf("%d editors in %v mode", n, tt.wantMode)
			}
			r.noErrors()
		})
	}
}

func TestChangingTheSettingsSwitchesTheViewAndKeepsTheModifiedEditorsState(t *testing.T) {
	var text string
	for i := range 80 {
		text += "line " + string(rune('a'+i%26)) + "\n"
	}
	modified := "first\n" + text + "last\n"
	r := newRig(t, text, modified, rigConfig{size: ui.Size{Width: 1100, Height: 300}})
	mod := r.model.View(Modified)
	if r.model.Mode() != SideBySide {
		t.Fatalf("mode %v", r.model.Mode())
	}
	mod.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 30, Col: 3}, Active: buffer.Position{Line: 30, Col: 3}}})
	mod.SelectionsChanged()
	mod.ScrollTo(400)
	r.settle()
	top, cursor := mod.ScrollTop(), mod.Cursors().Primary().Position()
	modifiedElement := r.ts.Elements(gluttertest.ByType[*editor.Editor]())[1]
	set := func(key string, v any) {
		t.Helper()
		if err := r.store.Update(context.Background(), key, v, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
	}
	set("diffEditor.renderSideBySide", false)
	r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
	if got := r.ts.Elements(gluttertest.ByType[*editor.Editor]()); len(got) != 1 || got[0] != modifiedElement {
		t.Errorf("the modified editor was made again: %v, was %v", got, modifiedElement)
	}
	if got := mod.Cursors().Primary().Position(); got != cursor {
		t.Errorf("the cursor moved to %+v from %+v", got, cursor)
	}
	if mod.ScrollTop() != top {
		t.Errorf("the scroll position is %v, was %v", mod.ScrollTop(), top)
	}
	if n := len(r.ts.Widgets(gluttertest.ByType[*editor.Editor]())); n != 1 {
		t.Errorf("%d editors", n)
	}
	set("diffEditor.renderSideBySide", true)
	r.waitFor("side by side", func() bool { return r.model.Mode() == SideBySide })
	if got := r.ts.Elements(gluttertest.ByType[*editor.Editor]()); len(got) != 2 || got[1] != modifiedElement {
		t.Errorf("the modified editor was made again when the original came back: %v", got)
	}
	if got := mod.Cursors().Primary().Position(); got != cursor {
		t.Errorf("after switching back the cursor is %+v, was %+v", got, cursor)
	}
	if mod.ScrollTop() != top || r.model.View(Original).ScrollTop() != top {
		t.Errorf("after switching back the editors are at %v and %v, was %v", mod.ScrollTop(), r.model.View(Original).ScrollTop(), top)
	}
	if len(r.model.View(Modified).Zones()) != len(r.model.ZonesOf(Modified)) || len(r.model.InlineZones()) != 0 {
		t.Errorf("the inline zones are still there: %+v", r.model.InlineZones())
	}
	r.noErrors()
}

func TestTheInlineViewSwitchesBackToAlignedEditors(t *testing.T) {
	r := newRig(t, "1\n2\n3\n4\n", "1\nnew\n4\n", rigConfig{settings: inlineSettings})
	r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
	if len(r.model.View(Original).Zones()) != 0 {
		t.Error("the original editor has filler in the inline view")
	}
	if err := r.store.Update(context.Background(), "diffEditor.renderSideBySide", true, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if err := r.store.Update(context.Background(), "diffEditor.useInlineViewWhenSpaceIsLimited", false, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	r.waitFor("side by side", func() bool { return r.model.Mode() == SideBySide })
	orig, mod := r.model.View(Original), r.model.View(Modified)
	if orig.Projection().Height() != mod.Projection().Height() {
		t.Errorf("the editors are %v and %v tall", orig.Projection().Height(), mod.Projection().Height())
	}
	if len(orig.BackgroundMarks()) == 0 {
		t.Error("the original editor has no marks after the switch")
	}
	r.noErrors()
}

func TestGoToChangeInTheInlineView(t *testing.T) {
	// a deletion (nothing in the modified text), then a replacement, then an insertion
	original := "1\n2\n3\n4\n5\n6\n7\n8\n9\n"
	modified := "1\n3\n4\nfive\n6\n7\n8\nnine\nnew\n"
	r := newRig(t, original, modified, rigConfig{settings: inlineSettings})
	r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
	mod := r.model.View(Modified)
	cursorLine := func() int { return mod.Cursors().Primary().Position().Line + 1 }
	mod.Cursors().SetSelections([]buffer.Selection{{}})
	mod.SelectionsChanged()
	changes := r.model.Changes()
	var wantLines []int
	for _, c := range changes {
		wantLines = append(wantLines, c.Modified.StartLine)
	}
	if len(wantLines) < 3 {
		t.Fatalf("changes %+v", changes)
	}
	for i, want := range wantLines {
		if !r.model.GoToChange(true) {
			t.Fatal("no change")
		}
		if cursorLine() != want {
			t.Errorf("next #%d: the cursor is on line %d, want %d", i+1, cursorLine(), want)
		}
	}
	// the first change is a pure deletion: the cursor is on the line after its zone, below the zone's rows
	if wantLines[0] != 2 {
		t.Errorf("the deletion's change starts at modified line %d", wantLines[0])
	}
	row := mod.RowOfPosition(buffer.Position{Line: wantLines[0] - 1})
	if _, _, inZone := mod.Projection().RowZone(row); inZone || row != wantLines[0]-1+1 {
		t.Errorf("the line after the zone is on row %d", row)
	}
	// previous goes back and wraps
	if !r.model.GoToChange(false) || cursorLine() != wantLines[len(wantLines)-2] {
		t.Errorf("previous: line %d", cursorLine())
	}
	r.noErrors()
}

func TestTheInlineRulerMarksTheZonesAndTheInsertedLines(t *testing.T) {
	r := newRig(t, "1\n2\n3\n4\n5\n", "1\n2\nnew\n5\n", rigConfig{settings: inlineSettings})
	r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
	marks, total := r.model.RulerMarks()
	mod := r.model.View(Modified)
	if total != mod.Projection().Height() {
		t.Errorf("total %v, the editor is %v tall", total, mod.Projection().Height())
	}
	if len(marks) != 2 {
		t.Fatalf("marks %+v", marks)
	}
	// the zone of the deleted lines (3, 4) is above the inserted line, both at rows of the one editor
	if marks[0].Side != Original || marks[1].Side != Modified || marks[0].Bottom != marks[1].Top {
		t.Errorf("marks %+v", marks)
	}
	if got, want := marks[0].Bottom-marks[0].Top, 2*mod.Options().LineHeight; got != want {
		t.Errorf("the removed mark is %v tall, want %v", got, want)
	}
	r.noErrors()
}

func TestInlineZonesFollowEditsAndAreKeptWhenTheDiffIsTheSame(t *testing.T) {
	r := newRig(t, "1\n2\n3\n4\n", "1\n4\n", rigConfig{settings: inlineSettings})
	r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
	before := r.model.View(Modified).Zones()[0].ID
	// white space at the end of a line is no change: the diff is the same, and the zone is the same zone
	typeAt(t, r.mod, 0, 1, "  ")
	applied := r.model.Applied()
	r.waitFor("the new diff", func() bool { return r.model.Applied() > applied && !r.model.Computing() })
	if zones := r.model.View(Modified).Zones(); len(zones) != 1 || zones[0].ID != before {
		t.Errorf("the zone was made again: %+v (was %d)", zones, before)
	}
	// a line above the zone pushes it down, and the new diff agrees
	typeAt(t, r.mod, 0, 0, "head\n")
	applied = r.model.Applied()
	r.waitFor("the new diff", func() bool { return r.model.Applied() > applied && !r.model.Computing() })
	if z := r.model.InlineZones(); len(z) != 1 || z[0].AfterLine != 2 || z[0].Rows != 2 {
		t.Errorf("zones after the diff: %+v", z)
	}
	r.noErrors()
}

func TestInlineViewWithWordWrapKeepsOneRowForEachDeletedLine(t *testing.T) {
	long := "\tcall(first, second, third, fourth, fifth, sixth, seventh, eighth, ninth, tenth, eleventh, twelfth, more)\n"
	wrap := func(o *editor.Options) { o.WordWrap, o.WrappingIndent, o.WrappingStrategy = "on", "same", "simple" }
	r := newRig(t, "a\n"+long+"b\n", "a\nb\n", rigConfig{size: ui.Size{Width: 500, Height: 300}, settings: inlineSettings, tweak: wrap})
	r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
	zones := r.model.InlineZones()
	if len(zones) != 1 || zones[0].Rows != 1 {
		t.Fatalf("zones %+v: deleted lines are not wrapped", zones)
	}
	r.noErrors()
}

func TestDisposeTakesTheInlineZonesOff(t *testing.T) {
	r := newRig(t, "1\n2\n3\n", "1\n3\n", rigConfig{settings: inlineSettings})
	r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
	r.ts.PumpWidget(widgets.NewSizedBoxShrink(nil))
	r.model.Dispose()
	if n := len(r.model.View(Modified).Zones()); n != 0 {
		t.Errorf("%d zones after Dispose", n)
	}
}

func TestInlineGolden(t *testing.T) {
	original := `package main

import "fmt"

// greet prints a greeting.
func greet(name string) {
	fmt.Println("hello", name)
}

func add(a, b int) int {
	return a + b
}

func unused() {
	fmt.Println("this goes away")
	fmt.Println("and this too")
}

func main() {
	greet("world")
	fmt.Println(add(1, 2))
}
`
	modified := `package main

import "fmt"

// greet prints a greeting for a person.
func greet(name string) {
	fmt.Println("hello,", name)
}

func add(a, b, c int) int {
	return a + b + c
}

func main() {
	greet("world")
	fmt.Println(add(1, 2, 3))
}

func square(x int) int {
	return x * x
}
`
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t, original, modified, rigConfig{size: ui.Size{Width: 900, Height: 560}, colors: th.colors(), highlight: th.theme, settings: inlineSettings})
			r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
			r.settle()
			testutil.MatchesGolden(r.ts, "diff_inline_"+th.name)
		})
	}
}

func TestTheRulerScrollsTheInlineEditorByItsOwnHeight(t *testing.T) {
	var text, changed string
	for i := range 120 {
		text += "line " + string(rune('a'+i%26)) + "\n"
		if i == 100 {
			changed += "changed\n"
			continue
		}
		changed += "line " + string(rune('a'+i%26)) + "\n"
	}
	r := newRig(t, text, changed, rigConfig{settings: inlineSettings, size: ui.Size{Width: 900, Height: 300}})
	r.waitFor("the inline view", func() bool { return r.model.Mode() == Inline })
	mod := r.model.View(Modified)
	_, viewport := mod.ViewportSize()
	total := mod.Projection().Height()
	// a press in the middle of the ruler puts that place of the editor in the middle of the view
	r.model.ScrollToFraction(150, 300)
	if got, want := mod.ScrollTop(), total/2-viewport/2; got != want {
		t.Errorf("the editor is at %v, want %v (the whole editor is %v tall: no filler of the other side)", got, want, total)
	}
	r.noErrors()
}
