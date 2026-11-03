package editor

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The coverage in the editor (step 26.4): gutter marks and backgrounds that name their theme colors, and the counts after the lines, on the
// small Go program of the debug tests. The editor knows nothing of the test model: these tests give it marks.

func newCoverageRig(t *testing.T, themeName string) *testRig {
	t.Helper()
	var vm *ViewModel
	t.Cleanup(func() { vm.DisposeCoverage() })
	r := newTestRig(t, themeName)
	vm = r.vm
	return r
}

func lineMark(first, last int, gutterID string) BackgroundMark {
	return BackgroundMark{Range: buffer.Range{Start: buffer.Position{Line: first}, End: buffer.Position{Line: last}}, WholeLine: true, GutterID: gutterID}
}

func textMark(line, from, to int, backgroundID string) BackgroundMark {
	return BackgroundMark{Range: buffer.Range{Start: buffer.Position{Line: line, Col: from}, End: buffer.Position{Line: line, Col: to}}, BackgroundID: backgroundID}
}

func coverageMarks() []BackgroundMark {
	return []BackgroundMark{
		lineMark(4, 6, "testing.coveredGutterBackground"),
		lineMark(7, 8, "testing.uncoveredGutterBackground"),
		lineMark(9, 9, "testing.coveredGutterBackground"),
		textMark(5, 1, 20, "testing.coveredBackground"),
		textMark(7, 1, 12, "testing.uncoveredBackground"),
	}
}

func coverageNotes() []LineAnnotation {
	return []LineAnnotation{
		{Line: 4, Text: " 4x ", Color: "testing.coverCountBadgeForeground", Background: "testing.coverCountBadgeBackground", Tight: true, Hover: "`main` was executed 4 time(s)."},
		{Line: 9, Text: " 1/2 branches ", Color: "testing.coverCountBadgeForeground", Background: "testing.uncoveredBranchBackground", Tight: true},
	}
}

func TestGoldenCoverage(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{{"coverage_dark", "Dark Modern"}, {"coverage_light", "Light Modern"}} {
		t.Run(tc.theme, func(t *testing.T) {
			r := newCoverageRig(t, tc.theme)
			r.vm.SetCoverageMarks(coverageMarks())
			r.vm.SetCoverageAnnotations(coverageNotes())
			r.settle()
			noErrors(t, r.ts)
			testutil.MatchesGolden(r.ts, tc.golden)
		})
	}
}

func TestCoverageMarksFollowTheEditsAndAreNotTheBackgroundMarks(t *testing.T) {
	r := newCoverageRig(t, "Dark Modern")
	other := []BackgroundMark{lineMark(1, 1, "")}
	r.vm.SetBackgroundMarks(other)
	r.vm.SetCoverageMarks(coverageMarks())
	if got := len(r.vm.CoverageMarks()); got != 5 {
		t.Fatalf("%d coverage marks, want 5", got)
	}
	if got := len(r.vm.BackgroundMarks()); got != 1 {
		t.Errorf("%d background marks, want the 1 that was set", got)
	}
	// both are drawn: the marks of the lines of the rows
	if got := len(r.vm.marksIn(0, 12)); got != 6 {
		t.Errorf("%d marks drawn, want 6", got)
	}
	// a line inserted above moves the marks down
	r.vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{})})
	if err := r.vm.PasteText("// a line\n"); err != nil {
		t.Fatal(err)
	}
	first := r.vm.CoverageMarks()[0]
	if first.Range.Start.Line != 5 {
		t.Errorf("the first mark is at line %d after an insert above, want 5", first.Range.Start.Line)
	}
	r.vm.SetCoverageMarks(nil)
	if len(r.vm.CoverageMarks()) != 0 || len(r.vm.BackgroundMarks()) != 1 {
		t.Errorf("clearing the coverage left %d coverage marks and %d background marks", len(r.vm.CoverageMarks()), len(r.vm.BackgroundMarks()))
	}
	r.vm.SetBackgroundMarks(nil)
}

func TestCoverageMarksResolveTheirThemeColors(t *testing.T) {
	r := newCoverageRig(t, "Dark Modern")
	r.vm.SetCoverageMarks(coverageMarks())
	r.settle() // the widget gives the editor the colors of the theme
	var gutter, back int
	for _, m := range r.vm.marksIn(0, 12) {
		if m.Gutter.Alpha() != 0 {
			gutter++
		}
		if m.Background.Alpha() != 0 {
			back++
		}
	}
	if gutter != 3 || back != 2 {
		t.Errorf("%d gutter colors and %d backgrounds resolved, want 3 and 2", gutter, back)
	}
	// a color the theme does not know leaves the mark as it was
	r.vm.SetCoverageMarks([]BackgroundMark{lineMark(1, 1, "no.such.color")})
	for _, m := range r.vm.marksIn(0, 12) {
		if m.Gutter.Alpha() != 0 {
			t.Errorf("an unknown color resolved to %v", m.Gutter)
		}
	}
}

func TestCoverageCountsAreAnnotationsOfTheirOwnBeforeTheOthers(t *testing.T) {
	r := newCoverageRig(t, "Dark Modern")
	r.vm.SetLineAnnotations([]LineAnnotation{{Line: 4, Text: "blame"}})
	r.vm.SetCoverageAnnotations(coverageNotes())
	got := r.vm.CoverageAnnotations()
	if !slices.Equal(got[4], []string{"4x"}) || !slices.Equal(got[9], []string{"1/2 branches"}) || len(got) != 2 {
		t.Errorf("the counts are %v", got)
	}
	var onLine4 []string
	for _, a := range r.vm.LineAnnotations() {
		if a.Line == 4 {
			onLine4 = append(onLine4, a.Text)
		}
	}
	if !slices.Equal(onLine4, []string{" 4x ", "blame"}) {
		t.Errorf("line 4 has %q, want the count before the blame", onLine4)
	}
	r.vm.SetCoverageAnnotations(nil)
	if len(r.vm.CoverageAnnotations()) != 0 || r.vm.LineAnnotationCount() != 1 {
		t.Errorf("clearing the counts left %v and %d annotations", r.vm.CoverageAnnotations(), r.vm.LineAnnotationCount())
	}
	r.vm.SetLineAnnotations(nil)
}
