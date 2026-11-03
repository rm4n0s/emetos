package app

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The goldens of the coverage (step 26.4), dark and light: the Test Coverage view with a file's declarations and the editor with the covered
// lines in the gutter; the editor with the inline coverage and the toolbar (covered, uncovered and partly covered lines, the counts and the
// branches); and the Explorer with the percentages.

var coverageGoldenSize = ui.Size{Width: 1000, Height: 520}

// goldenCoverage makes a rig of the theme with calc.go open, after a run with coverage.
func goldenCoverage(t *testing.T, theme string, settings map[string]any) (*testingRig, *textEditor) {
	t.Helper()
	if settings == nil {
		settings = map[string]any{}
	}
	settings["testing.openTesting"] = "neverOpen"
	r, e := coverageRig(t, testingOpts{theme: theme, size: coverageGoldenSize, now: (&stepClock{}).now, settings: settings})
	r.covered(e)
	return r, e
}

func TestGoldenCoverage(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name+" view", func(t *testing.T) {
			r, _ := goldenCoverage(t, th.theme, nil)
			r.run(command.ViewContainerTesting, nil)
			cv := r.win.shell.testing.coverage
			r.waitFor("the rows", func() bool { return cv.Model().Len() > 0 })
			cv.Model().Expand(cv.Model().Rows()[1].Key)
			r.waitFor("the declarations", func() bool { return cv.Model().Len() == 6 })
			testutil.MatchesGolden(r.ts, "coverage_view_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
		t.Run(th.name+" editor", func(t *testing.T) {
			r, e := goldenCoverage(t, th.theme, map[string]any{"testing.coverageToolbarEnabled": true})
			r.run(command.TestingToggleInlineCoverage, nil)
			r.waitFor("the inline coverage", func() bool { return len(e.view.CoverageAnnotations()) > 0 })
			testutil.MatchesGolden(r.ts, "coverage_editor_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
		t.Run(th.name+" explorer", func(t *testing.T) {
			r, _ := goldenCoverage(t, th.theme, nil)
			r.run(command.ViewContainerExplorer, nil)
			r.waitFor("the percentages", func() bool {
				_, ok := r.win.Decorations.Get(explorerURI(r.folder+"/calc.go"), false)
				return ok
			})
			testutil.MatchesGolden(r.ts, "coverage_explorer_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}
