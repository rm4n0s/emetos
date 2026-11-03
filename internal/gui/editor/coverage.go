package editor

// The coverage of the tests in the editor (SPEC §10.32 "The editor", step 26.4): backgrounds in the gutter and on the text, and the
// counts written after the lines. The editor knows nothing of the test model: the window works out the marks. The marks are decorations
// that this editor owns, in slots of their own, so that the marks of the diff editor and the annotations of Git and the debugger stay.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/codeCoverageDecorations.ts

import (
	"strings"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// coverageOwner is the slot of the decorations of the coverage marks (see ViewModel.owner).
const coverageOwner = 17

// SetCoverageMarks replaces the background marks of the coverage: whole-line marks with a gutter color, and marks of characters. They are
// drawn with the background marks of the editor.
func (vm *ViewModel) SetCoverageMarks(marks []BackgroundMark) {
	var old []int
	for _, d := range vm.doc.DecorationsOf(vm.owner(coverageOwner)) {
		old = append(old, d.ID)
	}
	if len(old) == 0 && len(marks) == 0 {
		return
	}
	add := make([]document.NewDecoration, len(marks))
	for i, m := range marks {
		add[i] = document.NewDecoration{Range: m.Range, Options: document.DecorationOptions{
			Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: "coverageMark",
			Data: markData{whole: m.WholeLine, background: m.Background, border: m.Border, gutter: m.Gutter, backgroundID: m.BackgroundID, gutterID: m.GutterID},
		}}
	}
	vm.doc.DeltaDecorations(vm.owner(coverageOwner), old, add)
	vm.covCount = len(marks)
	vm.NotifyListeners()
}

// CoverageMarks are the marks of the coverage as they are now (their ranges moved with the edits), in order of position.
func (vm *ViewModel) CoverageMarks() []BackgroundMark {
	var out []BackgroundMark
	for _, d := range vm.doc.DecorationsOf(vm.owner(coverageOwner)) {
		if m, ok := markOf(d); ok {
			out = append(out, m)
		}
	}
	return out
}

// SetCoverageAnnotations replaces the counts written after the lines: line annotations of a group of their own, drawn after the messages
// of the tests and before the other annotations.
func (vm *ViewModel) SetCoverageAnnotations(list []LineAnnotation) {
	vm.setAnnotations(&vm.ccov, list)
}

// CoverageAnnotations are the counts of the editor as they are now, by line (counted from 0); the lines moved with the edits.
func (vm *ViewModel) CoverageAnnotations() map[int][]string {
	out := map[int][]string{}
	for id, e := range vm.ccov.entries {
		d, ok := vm.doc.Decoration(id)
		if !ok {
			continue
		}
		line := d.Range.Start.Line
		out[line] = append(out[line], strings.TrimSpace(e.Text))
	}
	return out
}

// DisposeCoverage takes the coverage off the editor.
func (vm *ViewModel) DisposeCoverage() {
	vm.SetCoverageMarks(nil)
	if len(vm.ccov.ids) > 0 {
		vm.doc.DeltaDecorations(vm.owner(annotationsOwner), vm.ccov.ids, nil)
		vm.ccov = annotationState{generation: vm.ccov.generation + 1}
		vm.NotifyListeners()
	}
}

// SetNamedColors gives the editor the means to find a theme color by its id, for the marks that name their colors; the widget sets it each
// time it is built.
func (vm *ViewModel) SetNamedColors(lookup func(id string) (ui.Color, bool)) { vm.namedColors = lookup }

// themedMark gives a mark the theme colors it names.
func (vm *ViewModel) themedMark(m BackgroundMark) BackgroundMark {
	if vm.namedColors == nil {
		return m
	}
	if m.BackgroundID != "" {
		if c, ok := vm.namedColors(m.BackgroundID); ok {
			m.Background = c
		}
	}
	if m.GutterID != "" {
		if c, ok := vm.namedColors(m.GutterID); ok {
			m.Gutter = c
		}
	}
	return m
}
