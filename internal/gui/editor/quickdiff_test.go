package editor

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
)

func quickDiffDoc(lines int) *ViewModel {
	var b strings.Builder
	for i := range lines {
		fmt.Fprintf(&b, "line %d\n", i+1)
	}
	return NewViewModel(newTestDoc(b.String()), testOptions(), nil)
}

func TestQuickDiffMarksFollowTheEdits(t *testing.T) {
	vm := quickDiffDoc(20)
	vm.SetQuickDiffMarks([]QuickDiffMark{
		{Type: quickdiff.Add, Start: 3, End: 4},
		{Type: quickdiff.Modify, Start: 8, End: 10},
		{Type: quickdiff.Delete, Start: 14, End: 0},
		{Type: quickdiff.Modify, Start: 18, End: 18, Secondary: true},
	})
	if vm.QuickDiffCount() != 4 {
		t.Fatalf("%d marks, want 4", vm.QuickDiffCount())
	}
	want := func(label string, marks ...QuickDiffMark) {
		t.Helper()
		got := vm.QuickDiffMarks()
		if len(got) != len(marks) {
			t.Fatalf("%s: marks = %+v, want %+v", label, got, marks)
		}
		for i := range marks {
			if got[i] != marks[i] {
				t.Errorf("%s: mark %d = %+v, want %+v", label, i, got[i], marks[i])
			}
		}
	}
	want("as set",
		QuickDiffMark{Type: quickdiff.Add, Start: 3, End: 4},
		QuickDiffMark{Type: quickdiff.Modify, Start: 8, End: 10},
		QuickDiffMark{Type: quickdiff.Delete, Start: 14, End: 0},
		QuickDiffMark{Type: quickdiff.Modify, Start: 18, End: 18, Secondary: true})
	insert := func(line int, text string) {
		t.Helper()
		at := buffer.Position{Line: line}
		if _, err := vm.Document().Edit([]document.Edit{{Range: buffer.Range{Start: at, End: at}, Text: text}}, document.EditOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	// two lines inserted above everything push every mark down by two
	insert(0, "x\ny\n")
	want("after two lines at the top",
		QuickDiffMark{Type: quickdiff.Add, Start: 5, End: 6},
		QuickDiffMark{Type: quickdiff.Modify, Start: 10, End: 12},
		QuickDiffMark{Type: quickdiff.Delete, Start: 16, End: 0},
		QuickDiffMark{Type: quickdiff.Modify, Start: 20, End: 20, Secondary: true})
	// a line inside the modified block makes it longer
	insert(11, "inside\n")
	got := vm.QuickDiffMarks()
	if got[1].Start != 10 || got[1].End != 13 {
		t.Errorf("a line inside the modified block: %+v, want lines 10 to 13", got[1])
	}
	// the marks go with DisposeQuickDiff
	vm.DisposeQuickDiff()
	if vm.QuickDiffCount() != 0 || len(vm.QuickDiffMarks()) != 0 || len(vm.Document().DecorationsOf(vm.owner(quickDiffOwner))) != 0 {
		t.Error("the marks are still there")
	}
}

func TestQuickDiffMarksAreReplacedAndMarksOutsideTheTextAreDropped(t *testing.T) {
	vm := quickDiffDoc(5)
	vm.SetQuickDiffMarks([]QuickDiffMark{
		{Type: quickdiff.Add, Start: 2, End: 2},
		{Type: quickdiff.Add, Start: 40, End: 41},   // no such lines
		{Type: quickdiff.Modify, Start: 4, End: 99}, // ends outside
		{Type: quickdiff.Delete, Start: 0, End: 0},  // at the start of the text: line 1
	})
	got := vm.QuickDiffMarks()
	if len(got) != 2 || got[0].Start != 1 || got[0].Type != quickdiff.Delete || got[1].Start != 2 {
		t.Fatalf("marks = %+v, want a deletion at line 1 and the added line 2", got)
	}
	vm.SetQuickDiffMarks([]QuickDiffMark{{Type: quickdiff.Modify, Start: 3, End: 3}})
	if got := vm.QuickDiffMarks(); len(got) != 1 || got[0].Type != quickdiff.Modify {
		t.Errorf("marks = %+v, want the one that replaced them", got)
	}
	vm.SetQuickDiffMarks(nil)
	if vm.QuickDiffCount() != 0 {
		t.Errorf("%d marks after an empty list", vm.QuickDiffCount())
	}
	// nothing changes when there was nothing and there is nothing
	vm.SetQuickDiffMarks(nil)
}

func TestQuickDiffGoTo(t *testing.T) {
	marks := []QuickDiffMark{
		{Type: quickdiff.Add, Start: 3, End: 4},
		{Type: quickdiff.Delete, Start: 9, End: 0},
		{Type: quickdiff.Modify, Start: 14, End: 16},
	}
	tests := []struct {
		name       string
		cursorLine int // from 0
		next       bool
		want       int // the line the cursor goes to, from 0
	}{
		{"next from the top", 0, true, 2},
		{"next from the first change", 2, true, 8},
		{"next from inside a change", 3, true, 8},
		{"next from between", 10, true, 13},
		{"next from the last change wraps to the first", 13, true, 2},
		{"next from after the last wraps", 18, true, 2},
		{"previous from the bottom", 18, false, 13},
		{"previous from the last change", 13, false, 8},
		{"previous from inside the last change", 14, false, 8},
		{"previous from the first change wraps to the last", 2, false, 13},
		{"previous from the top wraps to the last", 0, false, 13},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := quickDiffDoc(20)
			vm.SetQuickDiffMarks(marks)
			vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: tt.cursorLine, Col: 2})})
			if !vm.QuickDiffGoTo(tt.next) {
				t.Fatal("QuickDiffGoTo found nothing")
			}
			if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: tt.want}) {
				t.Errorf("cursor = %+v, want line %d column 0", got, tt.want)
			}
		})
	}
	// the commands do the same, and nothing without marks
	vm := quickDiffDoc(20)
	if vm.QuickDiffGoTo(true) {
		t.Error("QuickDiffGoTo moved in an editor without marks")
	}
	for _, id := range []string{"workbench.action.editor.nextChange", "workbench.action.editor.previousChange"} {
		if err := vm.RunCommand(id, nil); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	vm.SetQuickDiffMarks(marks)
	if err := vm.RunCommand("workbench.action.editor.nextChange", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Cursors().Primary().Position(); got.Line != 2 {
		t.Errorf("next change: cursor at %+v, want line 2", got)
	}
	if err := vm.RunCommand("workbench.action.editor.previousChange", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Cursors().Primary().Position(); got.Line != 13 {
		t.Errorf("previous change: cursor at %+v, want line 13 (wrapped)", got)
	}
}

func TestSecondaryMarksThatTouchPrimaryOnesAreNotShown(t *testing.T) {
	vm := quickDiffDoc(30)
	vm.SetQuickDiffMarks([]QuickDiffMark{
		{Type: quickdiff.Modify, Start: 5, End: 7},
		{Type: quickdiff.Add, Start: 7, End: 8, Secondary: true},   // shares line 7
		{Type: quickdiff.Add, Start: 8, End: 9, Secondary: true},   // starts right after: touches
		{Type: quickdiff.Add, Start: 11, End: 12, Secondary: true}, // a line apart
		{Type: quickdiff.Delete, Start: 4, End: 0, Secondary: true},
		{Type: quickdiff.Delete, Start: 20, End: 0},
		{Type: quickdiff.Modify, Start: 20, End: 20, Secondary: true},
	})
	var starts []int
	var secondary int
	for _, p := range vm.shownMarks() {
		starts = append(starts, p.first+1)
		if p.Secondary {
			secondary++
		}
	}
	// shown: the primary modification (5), the secondary addition at 11, the primary deletion after 20 and the secondary line 20, which
	// touches nothing but the deleted position after it ... the deleted mark sits between 20 and 21, so line 20 touches it
	want := []int{5, 11, 20}
	if len(starts) != len(want) {
		t.Fatalf("shown starts = %v (secondary %d), want %v", starts, secondary, want)
	}
	for i := range want {
		if starts[i] != want[i] {
			t.Errorf("shown starts = %v, want %v", starts, want)
		}
	}
	if secondary != 1 {
		t.Errorf("%d secondary marks shown, want 1", secondary)
	}
}

// TestQuickDiffRandomEditsKeepMarksInTheText edits a document at random and checks that the marks always point into the text and stay in order.
func TestQuickDiffRandomEditsKeepMarksInTheText(t *testing.T) {
	for seed := int64(1); seed <= 6; seed++ {
		rng := rand.New(rand.NewSource(seed))
		vm := quickDiffDoc(60)
		var marks []QuickDiffMark
		for line := 2; line < 58; line += 3 + rng.Intn(4) {
			switch rng.Intn(3) {
			case 0:
				marks = append(marks, QuickDiffMark{Type: quickdiff.Add, Start: line, End: line + rng.Intn(2)})
			case 1:
				marks = append(marks, QuickDiffMark{Type: quickdiff.Modify, Start: line, End: line + rng.Intn(2)})
			default:
				marks = append(marks, QuickDiffMark{Type: quickdiff.Delete, Start: line})
			}
		}
		vm.SetQuickDiffMarks(marks)
		doc := vm.Document()
		for range 80 {
			snap := doc.Snapshot()
			line := rng.Intn(snap.LineCount())
			text, _ := snap.LineContent(line)
			at := buffer.Position{Line: line, Col: rng.Intn(len(text) + 1)}
			e := document.Edit{Range: buffer.Range{Start: at, End: at}, Text: "ab\ncd"}
			if rng.Intn(2) == 0 && line+1 < snap.LineCount() {
				e = document.Edit{Range: buffer.Range{Start: at, End: buffer.Position{Line: line + 1, Col: 0}}}
			}
			if _, err := doc.Edit([]document.Edit{e}, document.EditOptions{}); err != nil {
				t.Fatal(err)
			}
			count := doc.Snapshot().LineCount()
			prev := 0
			for _, m := range vm.QuickDiffMarks() {
				if m.Start < 1 || m.Start > count || (m.Type != quickdiff.Delete && (m.End < m.Start || m.End > count)) {
					t.Fatalf("seed %d: mark %+v is outside the text of %d lines", seed, m, count)
				}
				if m.Start < prev {
					t.Fatalf("seed %d: marks are not in order: %+v", seed, vm.QuickDiffMarks())
				}
				prev = m.Start
			}
		}
	}
}
