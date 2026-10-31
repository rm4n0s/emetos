package diffeditor

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The text the toolbar in the gutter gives a command, and the toolbar itself (step 24.12). The diff editor in VS Code has no unit test of
// these: 0 ported, 0 skipped.

const gutterOriginal = "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"

// gutterModified has a modified block of three lines (2 to 4), an added block (7) and a deleted line (nine).
const gutterModified = "one\nTWO\nTHREE\nFOUR\nfive\nsix\nnew\nseven\neight\nten\n"

func TestHunkAtAndTheTextWithAHunk(t *testing.T) {
	r := newRig(t, gutterOriginal, gutterModified, rigConfig{})
	m := r.model
	if got := len(m.Changes()); got != 3 {
		t.Fatalf("%d changes: %+v", got, m.Changes())
	}
	for _, tt := range []struct {
		line int
		hunk int
		ok   bool
	}{{1, 0, false}, {2, 0, true}, {4, 0, true}, {5, 0, false}, {7, 1, true}, {8, 0, false}, {9, 0, false}, {10, 0, false}} {
		if i, ok := m.HunkAt(tt.line); ok != tt.ok || ok && i != tt.hunk {
			t.Errorf("HunkAt(%d) = %d, %v; want %d, %v", tt.line, i, ok, tt.hunk, tt.ok)
		}
	}
	for i, want := range []string{
		"one\nTWO\nTHREE\nFOUR\nfive\nsix\nseven\neight\nnine\nten\n",
		"one\ntwo\nthree\nfour\nfive\nsix\nnew\nseven\neight\nnine\nten\n",
		"one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nten\n",
	} {
		if got := m.TextWithHunk(i); got != want {
			t.Errorf("TextWithHunk(%d) = %q, want %q", i, got, want)
		}
	}
	if got := m.TextWithHunk(9); got != gutterOriginal {
		t.Errorf("TextWithHunk of a change that is not there = %q", got)
	}
}

func TestTextWithLinesCutsABlockToTheSelectedLines(t *testing.T) {
	r := newRig(t, gutterOriginal, gutterModified, rigConfig{})
	m := r.model
	tests := []struct {
		name        string
		first, last int
		want        string
		ok          bool
	}{
		{"the middle line of the block of three", 3, 3, "one\ntwo\nTHREE\nfour\nfive\nsix\nseven\neight\nnine\nten\n", true},
		{"the first two lines", 2, 3, "one\nTWO\nTHREE\nfour\nfive\nsix\nseven\neight\nnine\nten\n", true},
		{"lines around the block", 1, 5, "one\nTWO\nTHREE\nFOUR\nfive\nsix\nseven\neight\nnine\nten\n", true},
		{"the added line, which has no counterpart, whole", 7, 7, "one\ntwo\nthree\nfour\nfive\nsix\nnew\nseven\neight\nnine\nten\n", true},
		{"two blocks", 4, 7, "one\ntwo\nthree\nFOUR\nfive\nsix\nnew\nseven\neight\nnine\nten\n", true},
		{"lines nothing changed", 5, 6, "", false},
		{"the line a deletion is at", 9, 9, "", false},
	}
	for _, tt := range tests {
		got, ok := m.TextWithLines(tt.first, tt.last)
		if ok != tt.ok || got != tt.want {
			t.Errorf("%s: TextWithLines(%d, %d) = %q, %v; want %q, %v", tt.name, tt.first, tt.last, got, ok, tt.want, tt.ok)
		}
	}
}

// toolbarRig is a diff editor whose toolbars have one button each; the runs are recorded.
type toolbarRig struct {
	*rig
	hunkRuns, selectionRuns []GutterContext
}

func newToolbarRig(t *testing.T) *toolbarRig {
	t.Helper()
	tr := &toolbarRig{}
	tr.rig = newRig(t, gutterOriginal, gutterModified, rigConfig{
		hunkActions: func() []GutterAction {
			return []GutterAction{{Icon: fonts.CodiconAdd, Tooltip: "Stage Block", Run: func(c GutterContext) { tr.hunkRuns = append(tr.hunkRuns, c) }}}
		},
		selectionActions: func() []GutterAction {
			return []GutterAction{{Icon: fonts.CodiconAdd, Tooltip: "Stage Selection", Run: func(c GutterContext) { tr.selectionRuns = append(tr.selectionRuns, c) }}}
		},
	})
	return tr
}

// button finds the button with the tooltip, if the toolbar shows it.
func (tr *toolbarRig) button(tooltip string) (*kit.IconButton, bool) {
	for _, w := range tr.ts.Widgets(gluttertest.ByType[*kit.IconButton]()) {
		if b, ok := w.(*kit.IconButton); ok && b.Tooltip() == tooltip {
			return b, true
		}
	}
	return nil, false
}

// modifiedPoint is a point of the modified editor over the row of a line (from 1) in the text area, in the inline or the side by side view.
func (tr *toolbarRig) modifiedPoint(line int) ui.Offset {
	vm := tr.model.View(Modified)
	row := vm.RowOfPosition(buffer.Position{Line: line - 1})
	proj := vm.Projection()
	return ui.Offset{Dx: tr.cfg.size.Width - 120, Dy: proj.RowTop(row) - vm.ScrollTop() + proj.RowHeight(row)/2}
}

func TestTheToolbarOfAHunkAppearsOverItsLinesAndStagesTheText(t *testing.T) {
	tr := newToolbarRig(t)
	if _, ok := tr.button("Stage Block"); ok {
		t.Fatal("the toolbar shows with the pointer outside the editor")
	}
	// over a line that no change touches there is none
	tr.ts.Hover(tr.modifiedPoint(1))
	tr.settle()
	if _, ok := tr.button("Stage Block"); ok {
		t.Error("the toolbar shows over a line nothing changed")
	}
	// over the added line
	tr.ts.Hover(tr.modifiedPoint(7))
	tr.settle()
	b, ok := tr.button("Stage Block")
	if !ok {
		t.Fatal("no Stage Block button over the added line")
	}
	tr.ts.Tap(gluttertest.ByWidget(b), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	tr.settle()
	if len(tr.hunkRuns) != 1 {
		t.Fatalf("%d runs of the hunk action", len(tr.hunkRuns))
	}
	got := tr.hunkRuns[0]
	if got.OriginalURI != "file:///orig.go" || got.ModifiedURI != "file:///mod.go" {
		t.Errorf("the documents are %q and %q", got.OriginalURI, got.ModifiedURI)
	}
	if want := "one\ntwo\nthree\nfour\nfive\nsix\nnew\nseven\neight\nnine\nten\n"; got.OriginalWithModifiedChanges != want {
		t.Errorf("the text is %q, want %q", got.OriginalWithModifiedChanges, want)
	}
	// the pointer leaves
	tr.ts.Hover(ui.Offset{Dx: -10, Dy: -10})
	tr.settle()
	if _, ok := tr.button("Stage Block"); ok {
		t.Error("the toolbar stays with the pointer outside")
	}
	tr.noErrors()
}

func TestTheToolbarOfASelectionStagesTheSelectedLines(t *testing.T) {
	tr := newToolbarRig(t)
	vm := tr.model.View(Modified)
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 2}, Active: buffer.Position{Line: 2, Col: 3}}})
	vm.SelectionsChanged()
	tr.settle()
	b, ok := tr.button("Stage Selection")
	if !ok {
		t.Fatal("no Stage Selection button over a selection that touches a change")
	}
	tr.ts.Tap(gluttertest.ByWidget(b), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	tr.settle()
	if len(tr.selectionRuns) != 1 {
		t.Fatalf("%d runs of the selection action", len(tr.selectionRuns))
	}
	if want := "one\ntwo\nTHREE\nfour\nfive\nsix\nseven\neight\nnine\nten\n"; tr.selectionRuns[0].OriginalWithModifiedChanges != want {
		t.Errorf("the text is %q, want %q", tr.selectionRuns[0].OriginalWithModifiedChanges, want)
	}
	// a selection on lines that no change touches shows nothing
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 4}, Active: buffer.Position{Line: 5}}})
	vm.SelectionsChanged()
	tr.settle()
	if _, ok := tr.button("Stage Selection"); ok {
		t.Error("the toolbar shows over a selection of lines nothing changed")
	}
	tr.noErrors()
}

func TestTheToolbarIsNotThereWithoutActionsOrForAReadOnlyModifiedSide(t *testing.T) {
	r := newRig(t, gutterOriginal, gutterModified, rigConfig{})
	r.ts.Hover(ui.Offset{Dx: 500, Dy: 60})
	r.settle()
	if len(r.ts.Widgets(gluttertest.ByType[*kit.IconButton]())) != 0 {
		t.Error("a diff editor with no actions has buttons")
	}
	tr := &toolbarRig{}
	tr.rig = newRig(t, gutterOriginal, gutterModified, rigConfig{readOnlyModified: true, hunkActions: func() []GutterAction {
		return []GutterAction{{Icon: fonts.CodiconAdd, Tooltip: "Stage Block", Run: func(GutterContext) {}}}
	}})
	tr.ts.Hover(tr.modifiedPoint(2))
	tr.settle()
	if _, ok := tr.button("Stage Block"); ok {
		t.Error("a read-only modified side has a toolbar")
	}
}

func TestTheSmallDiffEditorOfAPeekIsInlineAndIgnoresNothing(t *testing.T) {
	r := newRig(t, "a\nb  \nc\n", "a\nb\nc\n", rigConfig{embedded: true, settings: map[string]any{"diffEditor.useInlineViewWhenSpaceIsLimited": false}})
	if !r.model.Embedded() || r.model.Mode() != Inline {
		t.Errorf("embedded %v, mode %v", r.model.Embedded(), r.model.Mode())
	}
	// white space at the end of a line is a change here (it is not in the diff editor of a tab, whose setting ignores it)
	if got := len(r.model.Changes()); got != 1 {
		t.Errorf("%d changes, want the trailing white space of line 2", got)
	}
	if r.model.WantedMode() != Inline {
		t.Error("an embedded diff editor wants the side by side view")
	}
	for _, side := range []Side{Original, Modified} {
		keys := r.model.View(side).ContextKeysForTest()
		if keys["isEmbeddedDiffEditor"] != true || keys["isInDiffRightEditor"] != (side == Modified) {
			t.Errorf("the keys of the %v editor are %v", side, keys)
		}
	}
	if len(r.ts.Widgets(gluttertest.ByType[*kit.IconButton]())) != 0 {
		t.Error("the small diff editor has buttons")
	}
}

func TestTheToolbarIsKeptInsideTheEditorOverTheFirstLine(t *testing.T) {
	tr := &toolbarRig{}
	tr.rig = newRig(t, gutterOriginal, "zero\n"+gutterOriginal, rigConfig{hunkActions: func() []GutterAction {
		return []GutterAction{{Icon: fonts.CodiconAdd, Tooltip: "Stage Block", Run: func(GutterContext) {}}}
	}})
	tr.ts.Hover(tr.modifiedPoint(1))
	tr.settle()
	if _, ok := tr.button("Stage Block"); !ok {
		t.Error("a row is lower than the buttons, and the toolbar of the first line is gone")
	}
}
