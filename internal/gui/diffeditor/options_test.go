package diffeditor

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

func TestAdjustOptions(t *testing.T) {
	base := editor.Options{FontSize: 14, Folding: true}
	base.Minimap.Enabled = true
	base.StickyScroll.Enabled = true
	for _, tc := range []struct {
		side     Side
		readOnly bool
	}{{Original, true}, {Modified, false}} {
		got := AdjustOptions(base, tc.side)
		if got.ReadOnly != tc.readOnly || got.Folding || got.Minimap.Enabled || got.StickyScroll.Enabled || got.FontSize != 14 {
			t.Errorf("%v: %+v", tc.side, got)
		}
	}
	if !base.Folding || !base.Minimap.Enabled || base.ReadOnly {
		t.Error("AdjustOptions changed its argument")
	}
}

func TestColorsFallBackLikeTheStyleSheet(t *testing.T) {
	dark := kit.DarkModern()
	c := ColorsOf(dark)
	// the margin and the ruler take the line background, the border the one between editor groups
	if c.InsertedGutter != dark.DiffInsertedLineBackground || c.RemovedGutter != dark.DiffRemovedLineBackground ||
		c.InsertedOverview != dark.DiffInsertedLineBackground || c.RemovedOverview != dark.DiffRemovedLineBackground ||
		c.Border != dark.EditorGroupBorder || c.Border.Alpha() == 0 {
		t.Errorf("colors %+v", c)
	}
	// a color the theme sets is used as it is
	set := ui.NewColor(0xFF123456)
	dark.DiffInsertedLineGutter, dark.DiffOverviewRemoved, dark.DiffBorder = set, set, set
	c = ColorsOf(dark)
	if c.InsertedGutter != set || c.RemovedOverview != set || c.Border != set {
		t.Errorf("colors %+v", c)
	}
}

func TestMarksOfAChange(t *testing.T) {
	doc := document.New(document.FileURI("/a.go"), "one\ntwo\nthree\nfour\n", document.NewOptions())
	snap := doc.Snapshot()
	c := Colors{InsertedLine: ui.NewColor(1), RemovedLine: ui.NewColor(2), InsertedText: ui.NewColor(3), RemovedText: ui.NewColor(4),
		InsertedGutter: ui.NewColor(5), RemovedGutter: ui.NewColor(6), InsertedTextBorder: ui.NewColor(7), RemovedTextBorder: ui.NewColor(8)}
	changes := []diff.LineRangeMapping{
		change(2, 4, 2, 3, inner(2, 2, 3, 3, 2, 1, 2, 4)), // two lines became one
		change(5, 5, 4, 5), // a line was inserted: nothing on the original side
	}
	got := Marks(changes, Modified, c, snap)
	want := []editor.BackgroundMark{
		{Range: buffer.Range{Start: at(1, 0), End: at(1, 3)}, WholeLine: true, Background: c.InsertedLine, Gutter: c.InsertedGutter},
		{Range: buffer.Range{Start: at(1, 0), End: at(1, 3)}, Background: c.InsertedText, Border: c.InsertedTextBorder},
		{Range: buffer.Range{Start: at(3, 0), End: at(3, 4)}, WholeLine: true, Background: c.InsertedLine, Gutter: c.InsertedGutter},
	}
	if len(got) != len(want) {
		t.Fatalf("marks %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mark %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	got = Marks(changes, Original, c, snap)
	if len(got) != 2 || !got[0].WholeLine || got[0].Range.Start.Line != 1 || got[0].Range.End.Line != 2 || got[0].Background != c.RemovedLine || got[0].Gutter != c.RemovedGutter ||
		got[1].Range.Start != at(1, 1) || got[1].Range.End != at(2, 2) || got[1].Border != c.RemovedTextBorder {
		t.Errorf("original marks %+v", got)
	}
	// a diff that is older than the text: lines the text does not have are left out
	if got := Marks([]diff.LineRangeMapping{change(1, 9, 1, 9)}, Modified, c, snap); len(got) != 0 {
		t.Errorf("marks of lines past the end: %+v", got)
	}
}
