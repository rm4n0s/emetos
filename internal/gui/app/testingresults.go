package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	testview "github.com/rm4n0s/emetos/internal/gui/views/testing"
)

// Test Results and what the settings of Testing make the window do when tests run (SPEC §10.32, step 26.3): the panel view with the runs
// and their output, testing.openTesting, testing.followRunningTest with testing.alwaysRevealTestOnStateChange, testing.saveBeforeTest and
// testing.automaticallyOpenPeekView.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingProgressUiService.ts (attachAutoOpenForNewResults),
// testingExplorerView.ts (the following of the running test), testExplorerActions.ts (saveBeforeTest) and
// testResultsView/testResultsViewContent.ts

// initTestResults makes Test Results over the service.
func (s *shellState) initTestResults(binding *widgets.WidgetsBinding) {
	win := s.win
	st := &s.testing
	var mu sync.Mutex
	files := map[string]bool{}
	st.results = testview.NewResults(testview.ResultsOpts{
		Ctx: win.ctx, Service: win.TestingService(), Post: binding.Post,
		Display: func(path string) string {
			if win.Folder != "" {
				if rel, err := filepath.Rel(win.Folder, path); err == nil && !strings.HasPrefix(rel, "..") {
					return filepath.ToSlash(rel)
				}
			}
			return filepath.Base(path)
		},
		Links: &kit.AnsiLinks{
			Resolve: func(p kit.LinkPart) bool {
				if p.Kind != kit.LinkPath || !filepath.IsAbs(p.Path) {
					return false
				}
				mu.Lock()
				defer mu.Unlock()
				ok, known := files[p.Path]
				if !known {
					info, err := os.Stat(p.Path)
					ok = err == nil && !info.IsDir()
					files[p.Path] = ok
				}
				return ok
			},
			Open: func(p kit.LinkPart) {
				if p.Kind == kit.LinkPath {
					s.openPathAt(binding, p.Path, p.Line, p.Column, openOptions{Preview: true, TakeFocus: true})
				}
			},
		},
	})
	st.stops = append(st.stops, st.results.Dispose)
	st.opening = map[string]bool{}
	st.stops = append(st.stops, win.TestingService().Subscribe(func(ev coretesting.Event) { s.onTestingNotable(binding, ev) }))
}

// testResultsBody is the body of the Test Results tab of the panel.
func (s *shellState) testResultsBody() widgets.Widget {
	if s.testing.results == nil {
		return nil
	}
	return testview.NewResultsBody(s.testing.results)
}

// showTestResults opens the Test Results panel; with latest the newest run is the one shown, and with focus the tree takes the keyboard.
//
// Ported from VS Code: testExplorerActions.ts (ShowMostRecentOutputAction), testResultsView (showLatestRun)
func (s *shellState) showTestResults(latest, focus bool) {
	win := s.win
	if s.testing.results == nil {
		return
	}
	win.Views.OpenPanel(command.ViewContainerTestResults)
	win.Layout.SetPanelVisible(true)
	if latest {
		s.testing.results.ShowLatest()
	}
	if focus {
		s.afterFrame(s.binding, func() {
			if r := s.testing.results; r != nil {
				r.Controller().Focus()
			}
		})
	}
	s.SetState(func() {})
}

// onTestingNotable follows the events a setting reacts to. It runs on the goroutine that changed the model; the work is handed to the UI
// thread.
func (s *shellState) onTestingNotable(binding *widgets.WidgetsBinding, ev coretesting.Event) {
	switch ev.Kind {
	case coretesting.EventResult:
		if ev.Change != coretesting.ResultStarted || ev.Result == nil {
			return
		}
		res := ev.Result
		binding.Post(func() {
			if s.Mounted() && s.testing.results != nil {
				s.testingResultStarted(res)
			}
		})
	case coretesting.EventTest:
		if ev.Test == nil || ev.Test.Reason != coretesting.ChangeOwnState {
			return
		}
		ch := *ev.Test
		binding.Post(func() {
			if s.Mounted() && s.testing.results != nil {
				s.testingOwnStateChanged(ch)
			}
		})
	}
}

// testingResultStarted opens the testing views when a run starts, as testing.openTesting says. A run that was not asked for with a view in
// front (preserveFocus: from the editor) opens nothing.
//
// Ported from VS Code: testingProgressUiService.ts (attachAutoOpenForNewResults)
func (s *shellState) testingResultStarted(res *coretesting.Result) {
	if res.Request().PreserveFocus {
		return
	}
	switch settings.Get[string](s.win.Settings, "testing.openTesting", "") {
	case "openExplorerOnTestStart":
		s.showTesting(s.binding, false, false)
	case "openOnTestStart":
		s.showTestResults(false, false)
	case "openOnTestFailure":
		s.testing.opening[res.ID()] = true
	}
}

// testingOwnStateChanged reacts to a test that changed its own state: the results view that opens on a failure, the explorer that follows the
// running test and the peek that opens on a failure.
func (s *shellState) testingOwnStateChanged(ch coretesting.ItemChange) {
	res := s.win.TestingService().Result(ch.ResultID)
	if res == nil {
		return
	}
	t, ok := res.Test(ch.TestID)
	if !ok {
		return
	}
	state := t.OwnState
	if s.testing.opening[ch.ResultID] && state.IsFailed() {
		delete(s.testing.opening, ch.ResultID)
		s.showTestResults(false, false)
	}
	if ex := s.testing.explorer; ex != nil && settings.Get[bool](s.win.Settings, "testing.followRunningTest", "") {
		// a test that completes very fast may not enter the running state at all
		if state == coretesting.StateRunning || ch.PreviousState == coretesting.StateQueued && state.HasResult() {
			ex.Reveal(ch.TestID, settings.Get[bool](s.win.Settings, "testing.alwaysRevealTestOnStateChange", ""))
		}
	}
	if state.IsFailed() {
		s.openPeekOnFailure(res, ch.TestID)
	}
}

// beforeTestRun saves the files with changes when testing.saveBeforeTest says so, and then calls run (on the UI thread). A save that does
// not succeed stops the run.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/common/testService.ts (runResolvedTests: saveBeforeTest) and
// testExplorerActions.ts (RunAtCursor)
func (s *shellState) beforeTestRun(run func()) {
	if !settings.Get[bool](s.win.Settings, "testing.saveBeforeTest", "") {
		run()
		return
	}
	s.saveDocsThen(s.fileDocumentsWithChanges(), func(ok bool) {
		if ok {
			run()
		}
	})
}

// fileDocumentsWithChanges are the documents with unsaved changes that are files on disk (an untitled document is not saved by a run).
func (s *shellState) fileDocumentsWithChanges() []*document.Document {
	if s.group.grid == nil {
		return nil
	}
	var out []*document.Document
	for _, d := range s.dirtyDocuments() {
		if d.URI().Scheme == document.SchemeFile {
			out = append(out, d)
		}
	}
	return out
}
