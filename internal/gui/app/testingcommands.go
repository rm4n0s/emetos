package app

import (
	"context"
	"fmt"
	"slices"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	testview "github.com/rm4n0s/emetos/internal/gui/views/testing"
)

// The commands of Testing (step 26.2). Each screen binds its own handlers, closures over its own workspace, in its own registry, so that
// what a command asks (a pick, the file to open) is shown on the screen the command was asked on (D46). They all work on the test
// service of the workspace, which the host's window and the views of its clients share. A run blocks until it ends, so it is started on a
// goroutine of its own; the events it makes reach the screen through the Post of the view (decision 1533).
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testExplorerActions.ts

// goTesting runs a request of the test service on a goroutine of its own and reports a failure on this screen: a notification. A run that
// was cancelled is not a failure.
func (s *shellState) goTesting(what string, f func(ctx context.Context) error) {
	win := s.win
	ctx := win.ctx
	go func() {
		err := f(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		win.Process.Logger.Warn("a testing request failed", "request", what, "err", err)
		win.onUI(func() { win.Notify.Error("Emetos", describe(err)) })
	}()
}

// runTestRequest resolves a request to the profiles that run it and starts the run.
func (s *shellState) runTestRequest(group coretesting.Group, ids, exclude []string, preserveFocus bool) {
	svc := s.win.TestingService()
	req := svc.ResolveGroup(coretesting.AmbiguousRequest{Group: group, Tests: ids, Exclude: exclude, PreserveFocus: preserveFocus})
	if len(req.Targets) == 0 {
		s.win.Notify.Info("Emetos", "There is no way to "+groupVerb(group)+" these tests.")
		return
	}
	s.beforeTestRun(func() {
		s.goTesting("runTests", func(ctx context.Context) error {
			_, err := svc.RunTests(ctx, req)
			return err
		})
	})
}

// groupVerb is what a group does, for a message.
func groupVerb(g coretesting.Group) string {
	switch g {
	case coretesting.GroupDebug:
		return "debug"
	case coretesting.GroupCoverage:
		return "run with coverage"
	}
	return "run"
}

// canRunIn tells whether a profile of the group can run the test (or the controller of the id).
func canRunIn(svc *coretesting.Service, group coretesting.Group) func(id string) bool {
	return func(id string) bool {
		c := svc.Capabilities(id)
		switch group {
		case coretesting.GroupDebug:
			return c.Debug
		case coretesting.GroupCoverage:
			return c.Coverage
		}
		return c.Run
	}
}

// testIDsOf reads the tests a command is run for: the id of a test (a string, the argument of a menu item), a list of ids, the test under
// the focus, or the selected tests.
func (s *shellState) testIDsOf(args any) []string {
	switch v := args.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []string:
		return v
	case []any:
		var out []string
		for _, x := range v {
			if id, ok := x.(string); ok {
				out = append(out, id)
			}
		}
		return out
	case map[string]any:
		if id, ok := v["id"].(string); ok && id != "" {
			return []string{id}
		}
	}
	ex := s.testing.explorer
	if ex == nil {
		return nil
	}
	var out []string
	for _, e := range ex.Selected() {
		if e.Kind != testview.KindError {
			out = append(out, e.ID)
		}
	}
	if len(out) == 0 {
		if e, ok := ex.Focused(); ok && e.Kind != testview.KindError {
			out = append(out, e.ID)
		}
	}
	return out
}

// runAll runs all the tests of the controllers that have some, in the group.
func (s *shellState) runAll(group coretesting.Group) {
	svc := s.win.TestingService()
	var ids []string
	can := canRunIn(svc, group)
	for _, c := range svc.Controllers() {
		if can(c.ID) && (svc.ItemCount(c.ID) > 0) {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		msg := "No tests found in this workspace. You may need to install a test provider extension"
		if group == coretesting.GroupDebug {
			msg = "No debuggable tests found in this workspace. You may need to install a test provider extension"
		}
		s.win.Notify.Info("Emetos", msg)
		return
	}
	s.runTestRequest(group, ids, nil, false)
}

// runVisible runs what the Test Explorer shows (the buttons of its title bar).
func (s *shellState) runVisible(group coretesting.Group) {
	ex := s.testing.explorer
	if ex == nil {
		return
	}
	include, exclude := ex.IncludeExclude(false, canRunIn(s.win.TestingService(), group))
	if len(include) == 0 {
		s.win.Notify.Info("Emetos", "No tests found in this workspace. You may need to install a test provider extension")
		return
	}
	s.runTestRequest(group, include, exclude, false)
}

// runItems runs the tests of a command's arguments.
func (s *shellState) runItems(group coretesting.Group, args any) {
	ids := s.testIDsOf(args)
	if len(ids) == 0 {
		return
	}
	s.runTestRequest(group, ids, nil, false)
}

// testsInFile are the topmost tests whose file is the path of the active editor.
func (s *shellState) testsInFile() (ids []string, ok bool) {
	svc := s.win.TestingService()
	e := s.activeEditor()
	if e == nil || e.doc == nil || e.doc.URI().Scheme != "file" {
		return nil, false
	}
	uri := explorerURI(e.doc.URI().Path)
	for _, c := range svc.Controllers() {
		svc.Walk(c.ID, func(it coretesting.Item) bool {
			if it.URI == uri {
				ids = append(ids, it.ID)
				return false
			}
			return true
		})
	}
	return ids, true
}

// testsAtCursor are the tests of the active editor's file whose range holds the line of the cursor: the ones with the smallest range.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testExplorerActions.ts (RunAtCursor)
func (s *shellState) testsAtCursor() (ids []string, ok bool) {
	svc := s.win.TestingService()
	e := s.activeEditor()
	if e == nil || e.doc == nil || e.doc.URI().Scheme != "file" {
		return nil, false
	}
	uri := explorerURI(e.doc.URI().Path)
	line := e.view.Cursors().Primary().Selection.Active.Line
	best := -1
	for _, c := range svc.Controllers() {
		svc.Walk(c.ID, func(it coretesting.Item) bool {
			if it.URI != uri || it.Range == nil || line < it.Range.Start.Line || line > it.Range.End.Line {
				return true
			}
			span := it.Range.End.Line - it.Range.Start.Line
			switch {
			case best < 0 || span < best:
				best, ids = span, []string{it.ID}
			case span == best:
				ids = append(ids, it.ID)
			}
			return true
		})
	}
	return ids, true
}

// lastRun is the newest result, if any.
func (s *shellState) lastRun() *coretesting.Result {
	if rs := s.win.TestingService().Results(); len(rs) > 0 {
		return rs[0]
	}
	return nil
}

// failedIn are the tests that failed in the results: the state of each test in the newest result that has it, oldest first (a test that
// passed since is not among them), without those below another that failed. With one result it is that result's.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testExplorerActions.ts (RunOrDebugFailedTests)
func failedIn(results ...*coretesting.Result) []string {
	failed := map[string]bool{}
	var order []string
	for i := len(results) - 1; i >= 0; i-- {
		for _, t := range results[i].Tests() {
			if t.OwnState.IsFailed() {
				if !failed[t.Item.ID] {
					order = append(order, t.Item.ID)
				}
				failed[t.Item.ID] = true
			} else {
				delete(failed, t.Item.ID)
			}
		}
	}
	var ids []string
	for _, id := range order {
		if failed[id] {
			ids = append(ids, id)
		}
	}
	return slices.DeleteFunc(slices.Clone(ids), func(id string) bool {
		return slices.ContainsFunc(ids, func(other string) bool { return coretesting.IsAncestorID(other, id) })
	})
}

// rerun runs a result's request again in the group.
func (s *shellState) rerun(r *coretesting.Result, group coretesting.Group) {
	if r == nil {
		return
	}
	req := r.Request()
	var ids []string
	for _, t := range req.Targets {
		ids = append(ids, t.TestIDs...)
	}
	if group == req.Group && len(req.Targets) > 0 {
		s.beforeTestRun(func() {
			s.goTesting("rerun", func(ctx context.Context) error {
				_, err := s.win.TestingService().RunTests(ctx, req)
				return err
			})
		})
		return
	}
	s.runTestRequest(group, ids, req.Exclude, false)
}

// bindTestingCommands takes over the commands of Testing in the registry while the shell is on screen.
func (s *shellState) bindTestingCommands(binding *widgets.WidgetsBinding) {
	win := s.win
	st := &s.testing
	svc := win.TestingService()
	ex := st.explorer
	run := func(f func()) command.Handler { return func(command.Context, any) error { f(); return nil } }
	withArgs := func(f func(args any)) command.Handler {
		return func(_ command.Context, args any) error { f(args); return nil }
	}
	group := func(g coretesting.Group, f func(coretesting.Group)) command.Handler { return run(func() { f(g) }) }
	handlers := map[string]command.Handler{
		command.ViewContainerTesting:    run(func() { s.showTesting(binding, false, false) }),
		command.TestingFocusExplorer:    run(func() { s.showTesting(binding, true, false) }),
		command.TestingFilter:           run(func() { s.showTesting(binding, true, true) }),
		command.TestingRunAll:           group(coretesting.GroupRun, s.runAll),
		command.TestingDebugAll:         group(coretesting.GroupDebug, s.runAll),
		command.TestingCoverageAll:      group(coretesting.GroupCoverage, s.runAll),
		command.TestingRunSelected:      group(coretesting.GroupRun, s.runVisible),
		command.TestingDebugSelected:    group(coretesting.GroupDebug, s.runVisible),
		command.TestingCoverageSelected: group(coretesting.GroupCoverage, s.runVisible),
		command.TestingRun:              withArgs(func(a any) { s.runItems(coretesting.GroupRun, a) }),
		command.TestingDebug:            withArgs(func(a any) { s.runItems(coretesting.GroupDebug, a) }),
		command.TestingCoverage:         withArgs(func(a any) { s.runItems(coretesting.GroupCoverage, a) }),

		command.TestingRunAtCursor:         group(coretesting.GroupRun, s.runAtCursor),
		command.TestingDebugAtCursor:       group(coretesting.GroupDebug, s.runAtCursor),
		command.TestingCoverageAtCursor:    group(coretesting.GroupCoverage, s.runAtCursor),
		command.TestingRunCurrentFile:      group(coretesting.GroupRun, s.runCurrentFile),
		command.TestingDebugCurrentFile:    group(coretesting.GroupDebug, s.runCurrentFile),
		command.TestingCoverageCurrentFile: group(coretesting.GroupCoverage, s.runCurrentFile),

		command.TestingReRunLastRun:       run(func() { s.rerun(s.lastRun(), coretesting.GroupRun) }),
		command.TestingDebugLastRun:       run(func() { s.rerun(s.lastRun(), coretesting.GroupDebug) }),
		command.TestingCoverageLastRun:    run(func() { s.rerun(s.lastRun(), coretesting.GroupCoverage) }),
		command.TestingReRunFailTests:     run(func() { s.runFailed(coretesting.GroupRun, false) }),
		command.TestingDebugFailTests:     run(func() { s.runFailed(coretesting.GroupDebug, false) }),
		command.TestingReRunFailedLastRun: run(func() { s.runFailed(coretesting.GroupRun, true) }),
		command.TestingDebugFailedLastRun: run(func() { s.runFailed(coretesting.GroupDebug, true) }),

		command.TestingShowMostRecentOutput: run(func() { s.showTestResults(true, true) }),
		command.TestingFocusResults:         run(func() { s.showTestResults(false, true) }),
		command.TestingOpenOutputPeek:       run(s.openOutputPeek),
		command.TestingGoToNextMessage:      run(func() { s.stepPeek(true) }),
		command.TestingGoToPrevMessage:      run(func() { s.stepPeek(false) }),
		command.TestingClosePeek:            run(s.closeTestPeek),
		command.TestingPeekError: withArgs(func(a any) {
			for _, id := range s.testIDsOf(a) {
				if s.peekTest(id) {
					return
				}
			}
		}),
		command.TestingRevealTest: withArgs(func(a any) {
			for _, id := range s.testIDsOf(a) {
				s.revealTest(id)
				return
			}
		}),
		command.TestingCancelRun:    run(svc.CancelAll),
		command.TestingRefreshTests: run(s.refreshTests),
		command.TestingCancelRefresh: run(func() {
			if st.cancelRefresh != nil {
				st.cancelRefresh()
			}
		}),
		command.TestingClearResults: run(svc.ClearResults),

		command.TestingCollapseAll:    run(ex.CollapseAll),
		command.TestingViewAsList:     run(func() { ex.SetMode(testview.ModeList); s.syncTestingParts() }),
		command.TestingViewAsTree:     run(func() { ex.SetMode(testview.ModeTree); s.syncTestingParts() }),
		command.TestingSortByStatus:   run(func() { ex.SetSorting(testview.SortByStatus); s.syncTestingParts() }),
		command.TestingSortByLocation: run(func() { ex.SetSorting(testview.SortByLocation); s.syncTestingParts() }),
		command.TestingSortByDuration: run(func() { ex.SetSorting(testview.SortByDuration); s.syncTestingParts() }),

		command.TestingGoToTest: withArgs(func(a any) {
			for _, id := range s.testIDsOf(a) {
				if it, ok := svc.Item(id); ok {
					s.testingGoTo(it)
					return
				}
			}
		}),
		command.TestingHideTest:   withArgs(func(a any) { ex.Hide(s.testIDsOf(a)...) }),
		command.TestingUnhideTest: withArgs(func(a any) { ex.Unhide(s.testIDsOf(a)...) }),
		command.TestingUnhideAll:  run(ex.UnhideAll),

		command.TestingToggleContinuous: withArgs(s.toggleContinuous),
		command.TestingStartContinuous: run(func() {
			s.goTesting("startContinuous", func(context.Context) error { return svc.StartContinuousGroup(coretesting.GroupRun, "") })
		}),
		command.TestingStopContinuous: run(func() { svc.StopContinuous("") }),

		command.TestingSelectDefaultProfiles: withArgs(s.selectDefaultProfiles),
		command.TestingConfigureProfile:      withArgs(s.configureProfile),
	}
	s.bindCoverageCommands(handlers)
	for id, h := range handlers {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		st.removeCommands = append(st.removeCommands, remove)
	}
}

// runAtCursor runs the test under the cursor of the active editor.
func (s *shellState) runAtCursor(group coretesting.Group) {
	ids, ok := s.testsAtCursor()
	if !ok || len(ids) == 0 {
		s.win.Notify.Info("Emetos", "No tests found at the cursor.")
		return
	}
	s.runTestRequest(group, ids, nil, true)
}

// runCurrentFile runs the tests of the active editor's file.
func (s *shellState) runCurrentFile(group coretesting.Group) {
	ids, ok := s.testsInFile()
	if !ok || len(ids) == 0 {
		s.win.Notify.Info("Emetos", "No tests found in the active file.")
		return
	}
	s.runTestRequest(group, ids, nil, true)
}

// runFailed runs again the tests that failed, in the group: those of the newest results (fromLast: of the last result only).
func (s *shellState) runFailed(group coretesting.Group, fromLast bool) {
	results := s.win.TestingService().Results()
	if len(results) == 0 {
		return
	}
	if fromLast {
		results = results[:1]
	}
	ids := failedIn(results...)
	if len(ids) == 0 {
		s.win.Notify.Info("Emetos", "No failed tests to run again.")
		return
	}
	s.runTestRequest(group, ids, nil, false)
}

// refreshTests asks the controllers to read their tests again, while the context key testing.isRefreshing says so.
func (s *shellState) refreshTests() {
	win := s.win
	st := &s.testing
	if st.refreshing.Load() {
		return
	}
	ctx, cancel := context.WithCancel(win.ctx)
	st.cancelRefresh = cancel
	st.refreshing.Store(true)
	refreshing := st.refreshing
	s.syncTestingParts()
	svc := win.TestingService()
	go func() {
		err := svc.RefreshTests(ctx, "")
		cancel()
		refreshing.Store(false)
		win.onUI(func() {
			if s.Mounted() && s.testing.explorer != nil {
				s.syncTestingParts()
			}
			if err != nil && ctx.Err() == nil && win.ctx.Err() == nil {
				win.Notify.Error("Emetos", describe(err))
			}
		})
	}()
}

// toggleContinuous turns the continuous run of the tests on or off.
func (s *shellState) toggleContinuous(args any) {
	svc := s.win.TestingService()
	for _, id := range s.testIDsOf(args) {
		if svc.ContinuousSpecificallyFor(id) {
			svc.StopContinuous(id)
			continue
		}
		s.goTesting("startContinuous", func(context.Context) error { return svc.StartContinuousGroup(coretesting.GroupRun, id) })
	}
}

// groupOf reads a group from the argument of a command: a group's number or name; the Run group otherwise.
func groupOf(args any) coretesting.Group {
	switch v := args.(type) {
	case string:
		switch v {
		case "debug":
			return coretesting.GroupDebug
		case "coverage":
			return coretesting.GroupCoverage
		}
	case float64:
		if g := coretesting.Group(int(v)); g.Valid() {
			return g
		}
	case coretesting.Group:
		return v
	}
	return coretesting.GroupRun
}

// selectDefaultProfiles lets the user pick the profile that runs the tests of a group without asking: the choice replaces the default of
// the controller of the profile.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testExplorerActions.ts (SelectDefaultTestProfiles)
func (s *shellState) selectDefaultProfiles(args any) {
	win := s.win
	svc := win.TestingService()
	g := groupOf(args)
	var items []quickinput.Item
	for _, c := range svc.Controllers() {
		for _, p := range svc.Profiles(c.ID) {
			if p.Group != g {
				continue
			}
			chosen := p
			label := p.Label
			if p.IsDefault {
				label += " (Default)"
			}
			items = append(items, quickinput.Item{ID: fmt.Sprintf("%s/%d", c.ID, p.ID), Label: label, Description: c.Label, Accept: func(quickinput.KeyMods) {
				var refs []coretesting.ProfileRef
				for _, d := range svc.GroupDefaults(g, "") {
					if d.ControllerID != chosen.ControllerID {
						refs = append(refs, d.Ref())
					}
				}
				svc.SetGroupDefaults(g, append(refs, chosen.Ref()))
			}})
		}
	}
	if len(items) == 0 {
		return
	}
	win.QuickInput.Pick(quickinput.PickOptions{Title: "Select Default Profile", Placeholder: "Select the profile to " + groupVerb(g) + " tests with", Items: items})
}

// configureProfile lets the user pick a profile that can be configured and configures it.
func (s *shellState) configureProfile(args any) {
	win := s.win
	svc := win.TestingService()
	var items []quickinput.Item
	for _, c := range svc.Controllers() {
		for _, p := range svc.Profiles(c.ID) {
			if !p.HasConfigure {
				continue
			}
			ref := p.Ref()
			items = append(items, quickinput.Item{ID: fmt.Sprintf("%s/%d", c.ID, p.ID), Label: p.Label, Description: c.Label, Accept: func(quickinput.KeyMods) {
				go func() {
					if err := svc.ConfigureProfile(ref); err != nil {
						win.onUI(func() { win.Notify.Error("Emetos", describe(err)) })
					}
				}()
			}})
		}
	}
	if len(items) == 0 {
		return
	}
	win.QuickInput.Pick(quickinput.PickOptions{Title: "Configure Test Profiles", Placeholder: "Select the profile to configure", Items: items})
}
