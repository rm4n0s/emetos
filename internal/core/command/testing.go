package command

import (
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// Testing in the window (SPEC §10.32, step 26.2): the ids of its view container and the Test Explorer, its commands, and the items of its
// menus. The handlers are bound by the window (internal/gui/app/testingview.go and testingcommands.go) while its view is on screen, and by
// the shell of the view of a client of a hosted workspace over the host's test service (D46). The commands of the editor, the peek, Test
// Results (step 26.3) and coverage (26.4) are registered by those steps.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testing.contribution.ts, testExplorerActions.ts, testingViewPaneContainer.ts
// and common/constants.ts

// The ids of the view container and the view of Testing.
const (
	// ViewContainerTesting is the Testing container (VS Code's Testing.ViewletId; the Test Explorer is its only view for now).
	ViewContainerTesting = "workbench.view.extension.test"
	// ViewTestExplorer is the Test Explorer view (VS Code's Testing.ExplorerViewId).
	ViewTestExplorer = "workbench.view.testing"
)

// The ids of the menus of Testing. TestingViewTitle is the title bar of the Test Explorer (VS Code's ViewTitle of the view) and
// TestingItem the buttons and the context menu of a test (VS Code's MenuId.TestItem).
const (
	// TestingViewTitle is the title bar of the Test Explorer: the buttons of group navigation and, behind "...", the other groups.
	TestingViewTitle = "TestingViewTitle"
	// TestingItem is the inline buttons (group inline) and the context menu of a test of the Test Explorer.
	TestingItem = "TestingItem"
)

// The ids of the commands of Testing (VS Code's TestCommandId).
const (
	TestingRunAll                = "testing.runAll"
	TestingDebugAll              = "testing.debugAll"
	TestingCoverageAll           = "testing.coverageAll"
	TestingRunSelected           = "testing.runSelected"
	TestingDebugSelected         = "testing.debugSelected"
	TestingCoverageSelected      = "testing.coverageSelected"
	TestingRun                   = "testing.run"
	TestingDebug                 = "testing.debug"
	TestingCoverage              = "testing.coverage"
	TestingRunAtCursor           = "testing.runAtCursor"
	TestingDebugAtCursor         = "testing.debugAtCursor"
	TestingCoverageAtCursor      = "testing.coverageAtCursor"
	TestingRunCurrentFile        = "testing.runCurrentFile"
	TestingDebugCurrentFile      = "testing.debugCurrentFile"
	TestingCoverageCurrentFile   = "testing.coverageCurrentFile"
	TestingReRunLastRun          = "testing.reRunLastRun"
	TestingDebugLastRun          = "testing.debugLastRun"
	TestingCoverageLastRun       = "testing.coverageLastRun"
	TestingReRunFailTests        = "testing.reRunFailTests"
	TestingDebugFailTests        = "testing.debugFailTests"
	TestingReRunFailedLastRun    = "testing.reRunFailedFromLastRun"
	TestingDebugFailedLastRun    = "testing.debugFailedFromLastRun"
	TestingShowMostRecentOutput  = "testing.showMostRecentOutput"
	TestingCancelRun             = "testing.cancelRun"
	TestingRefreshTests          = "testing.refreshTests"
	TestingCancelRefresh         = "testing.cancelTestRefresh"
	TestingClearResults          = "testing.clearTestResults"
	TestingCollapseAll           = "testing.collapseAll"
	TestingViewAsList            = "testing.viewAsList"
	TestingViewAsTree            = "testing.viewAsTree"
	TestingSortByStatus          = "testing.sortByStatus"
	TestingSortByLocation        = "testing.sortByLocation"
	TestingSortByDuration        = "testing.sortByDuration"
	TestingGoToTest              = "testing.editFocusedTest"
	TestingHideTest              = "testing.hideTest"
	TestingUnhideTest            = "testing.unhideTest"
	TestingUnhideAll             = "testing.unhideAllTests"
	TestingToggleContinuous      = "testing.toggleContinuousRunForTest"
	TestingStartContinuous       = "testing.startContinuousRun"
	TestingStopContinuous        = "testing.stopContinuousRun"
	TestingSelectDefaultProfiles = "testing.selectDefaultTestProfiles"
	TestingConfigureProfile      = "testing.configureProfile"
	// TestingFocusExplorer shows the Test Explorer and gives its tree the keyboard (VS Code's focus command of a view).
	TestingFocusExplorer = ViewTestExplorer + ".focus"
	// TestingFilter focuses the filter box of the Test Explorer (VS Code's TestCommandId.FilterAction).
	TestingFilter = "workbench.actions.treeView.testExplorer.filter"
)

// testingCategory is the category of the commands of the palette (VS Code's Categories.Test).
const testingCategory = "Test"

// registerTestingCommands registers the commands of Testing that the Test Explorer, the keys and the menus run.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testExplorerActions.ts and testing.contribution.ts
func registerTestingCommands(r *Registry) {
	cat := testingCategory
	r.MustRegister(
		Meta{ID: ViewContainerTesting, Title: "Show Testing", Category: "View"},
		Meta{ID: TestingFocusExplorer, Title: "Focus on Test Explorer View", Category: "View"},
		Meta{ID: TestingFilter, Title: "Filter Tests", Category: cat},
		Meta{ID: TestingRunAll, Title: "Run All Tests", Category: cat, Icon: "run-all"},
		Meta{ID: TestingDebugAll, Title: "Debug All Tests", Category: cat, Icon: "debug-alt-small"},
		Meta{ID: TestingCoverageAll, Title: "Run All Tests with Coverage", Category: cat, Icon: "run-coverage"},
		Meta{ID: TestingRunSelected, Title: "Run Tests", Category: cat, Icon: "run-all"},
		Meta{ID: TestingDebugSelected, Title: "Debug Tests", Category: cat, Icon: "debug-alt-small"},
		Meta{ID: TestingCoverageSelected, Title: "Run Tests with Coverage", Category: cat, Icon: "run-all-coverage"},
		Meta{ID: TestingRun, Title: "Run Test", Icon: "play"},
		Meta{ID: TestingDebug, Title: "Debug Test", Icon: "debug-alt-small"},
		Meta{ID: TestingCoverage, Title: "Run Test with Coverage", Icon: "run-coverage"},
		Meta{ID: TestingRunAtCursor, Title: "Run Test at Cursor", Category: cat},
		Meta{ID: TestingDebugAtCursor, Title: "Debug Test at Cursor", Category: cat},
		Meta{ID: TestingCoverageAtCursor, Title: "Run Test at Cursor with Coverage", Category: cat},
		Meta{ID: TestingRunCurrentFile, Title: "Run Tests in Current File", Category: cat},
		Meta{ID: TestingDebugCurrentFile, Title: "Debug Tests in Current File", Category: cat},
		Meta{ID: TestingCoverageCurrentFile, Title: "Run Tests with Coverage in Current File", Category: cat},
		Meta{ID: TestingReRunLastRun, Title: "Rerun Last Run", Category: cat, Icon: "debug-rerun"},
		Meta{ID: TestingDebugLastRun, Title: "Debug Last Run", Category: cat, Icon: "debug-alt-small"},
		Meta{ID: TestingCoverageLastRun, Title: "Rerun Last Run with Coverage", Category: cat},
		Meta{ID: TestingReRunFailTests, Title: "Rerun Failed Tests", Category: cat},
		Meta{ID: TestingDebugFailTests, Title: "Debug Failed Tests", Category: cat},
		Meta{ID: TestingReRunFailedLastRun, Title: "Rerun Failed Tests from Last Run", Category: cat},
		Meta{ID: TestingDebugFailedLastRun, Title: "Debug Failed Tests from Last Run", Category: cat},
		Meta{ID: TestingShowMostRecentOutput, Title: "Show Output", Category: cat, Icon: "terminal"},
		Meta{ID: TestingCancelRun, Title: "Cancel Test Run", Category: cat, Icon: "debug-stop"},
		Meta{ID: TestingRefreshTests, Title: "Refresh Tests", Category: cat, Icon: "refresh"},
		Meta{ID: TestingCancelRefresh, Title: "Cancel Test Refresh", Category: cat, Icon: "debug-stop"},
		Meta{ID: TestingClearResults, Title: "Clear All Results", Category: cat, Icon: "clear-all"},
		Meta{ID: TestingCollapseAll, Title: "Collapse All Tests", Icon: "collapse-all"},
		Meta{ID: TestingViewAsList, Title: "View as List", Icon: "list-flat"},
		Meta{ID: TestingViewAsTree, Title: "View as Tree", Icon: "list-tree"},
		Meta{ID: TestingSortByStatus, Title: "Sort by Status"},
		Meta{ID: TestingSortByLocation, Title: "Sort by Location"},
		Meta{ID: TestingSortByDuration, Title: "Sort by Duration"},
		Meta{ID: TestingGoToTest, Title: "Go to Test", Icon: "go-to-file"},
		Meta{ID: TestingHideTest, Title: "Hide Test"},
		Meta{ID: TestingUnhideTest, Title: "Unhide Test"},
		Meta{ID: TestingUnhideAll, Title: "Unhide All Tests"},
		Meta{ID: TestingToggleContinuous, Title: "Turn on Continuous Run", Icon: "eye",
			Precondition: contextkey.Or(contextkey.Has("testing.isContinuousModeOn"), contextkey.Not("testing.isParentRunningContinuously"))},
		Meta{ID: TestingStartContinuous, Title: "Start Continuous Run", Category: cat, Icon: "eye"},
		Meta{ID: TestingStopContinuous, Title: "Stop Continuous Run", Category: cat, Icon: "eye-closed"},
		Meta{ID: TestingSelectDefaultProfiles, Title: "Select Default Profile", Category: cat, Icon: "gear"},
		Meta{ID: TestingConfigureProfile, Title: "Configure Test Profiles", Category: cat, Icon: "gear"},
	)
}

// registerTestingMenus adds the command palette rows, the title bar of the Test Explorer and the buttons and context menu of a test.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testExplorerActions.ts (the menu of each action)
func registerTestingMenus(r *MenuRegistry) {
	has := func(key string) contextkey.Expr { return contextkey.Has(key) }
	hasAny := contextkey.Greater("testing.providerCount", 0)
	r.MustAdd(MenuItem{Menu: MenubarViewMenu, Command: ViewContainerTesting, Title: "Testing", MnemonicTitle: "&&Testing", Group: "3_views", Order: 3.6})
	// the command palette: a command is listed while a provider can use it
	for _, c := range []struct {
		id   string
		when contextkey.Expr
	}{
		{TestingRunAll, has("testing.hasRunnableTests")}, {TestingDebugAll, has("testing.hasDebuggableTests")},
		{TestingCoverageAll, has("testing.hasCoverableTests")},
		{TestingRunAtCursor, hasAny}, {TestingDebugAtCursor, hasAny}, {TestingCoverageAtCursor, hasAny},
		{TestingRunCurrentFile, has("testing.hasRunnableTests")}, {TestingDebugCurrentFile, has("testing.hasDebuggableTests")},
		{TestingCoverageCurrentFile, has("testing.hasCoverableTests")},
		{TestingReRunLastRun, hasAny}, {TestingDebugLastRun, hasAny}, {TestingCoverageLastRun, hasAny},
		{TestingReRunFailTests, has("testing.hasAnyResults")}, {TestingDebugFailTests, has("testing.hasAnyResults")},
		{TestingReRunFailedLastRun, has("testing.hasAnyResults")}, {TestingDebugFailedLastRun, has("testing.hasAnyResults")},
		{TestingShowMostRecentOutput, has("testing.hasAnyResults")}, {TestingCancelRun, has("testing.isRunning")},
		{TestingRefreshTests, has("testing.canRefresh")}, {TestingCancelRefresh, has("testing.isRefreshing")},
		{TestingClearResults, has("testing.hasAnyResults")},
		{TestingStartContinuous, has("testing.supportsContinuousRun")}, {TestingStopContinuous, has("testing.supportsContinuousRun")},
		{TestingSelectDefaultProfiles, has("testing.hasNonDefaultProfile")}, {TestingConfigureProfile, has("testing.hasConfigurableProfile")},
		{TestingFilter, contextkey.False()},
		{TestingRunSelected, contextkey.False()}, {TestingDebugSelected, contextkey.False()}, {TestingCoverageSelected, contextkey.False()},
		{TestingRun, contextkey.False()}, {TestingDebug, contextkey.False()}, {TestingCoverage, contextkey.False()},
		{TestingCollapseAll, contextkey.False()}, {TestingViewAsList, contextkey.False()}, {TestingViewAsTree, contextkey.False()},
		{TestingSortByStatus, contextkey.False()}, {TestingSortByLocation, contextkey.False()}, {TestingSortByDuration, contextkey.False()},
		{TestingGoToTest, contextkey.False()}, {TestingHideTest, contextkey.False()}, {TestingUnhideTest, contextkey.False()},
		{TestingUnhideAll, contextkey.False()}, {TestingToggleContinuous, contextkey.False()},
	} {
		r.MustAdd(MenuItem{Menu: CommandPalette, Command: c.id, When: c.when})
	}

	// the title bar of the Test Explorer; the order is VS Code's ActionOrder (Refresh 10, Run 11, Debug 12, Coverage 13, RunContinuous 14,
	// RunUsing 15, Collapse 16, ClearResults 17, DisplayMode 18, Sort 19, GoToTest 20, HideTest 21)
	idle := contextkey.Not("testing.isRunning")
	r.MustAdd(
		MenuItem{Menu: TestingViewTitle, Command: TestingRefreshTests, Group: "navigation", Order: 10,
			When: contextkey.And(has("testing.canRefresh"), contextkey.Not("testing.isRefreshing"))},
		MenuItem{Menu: TestingViewTitle, Command: TestingCancelRefresh, Group: "navigation", Order: 10, When: has("testing.isRefreshing")},
		MenuItem{Menu: TestingViewTitle, Command: TestingRunSelected, Group: "navigation", Order: 11, When: contextkey.And(idle, has("testing.hasRunnableTests"))},
		MenuItem{Menu: TestingViewTitle, Command: TestingCancelRun, Group: "navigation", Order: 11, When: has("testing.isRunning")},
		MenuItem{Menu: TestingViewTitle, Command: TestingDebugSelected, Group: "navigation", Order: 12, When: contextkey.And(idle, has("testing.hasDebuggableTests"))},
		MenuItem{Menu: TestingViewTitle, Command: TestingCoverageSelected, Group: "navigation", Order: 13, When: contextkey.And(idle, has("testing.hasCoverableTests"))},
		MenuItem{Menu: TestingViewTitle, Command: TestingStartContinuous, Group: "navigation", Order: 15,
			When: contextkey.And(has("testing.supportsContinuousRun"), contextkey.Not("testing.isContinuousModeOn"))},
		MenuItem{Menu: TestingViewTitle, Command: TestingStopContinuous, Group: "navigation", Order: 15,
			When: contextkey.And(has("testing.supportsContinuousRun"), has("testing.isContinuousModeOn"))},
		MenuItem{Menu: TestingViewTitle, Command: TestingShowMostRecentOutput, Group: "navigation", Order: 16, Precondition: has("testing.hasAnyResults")},
		MenuItem{Menu: TestingViewTitle, Command: TestingCollapseAll, Group: "displayAction", Order: 16},
		MenuItem{Menu: TestingViewTitle, Command: TestingClearResults, Group: "displayAction", Order: 17},
		MenuItem{Menu: TestingViewTitle, Command: TestingViewAsList, Group: "viewAs", Order: 18, When: contextkey.Equals("testing.explorerViewMode", "true"),
			Title: "View as List"},
		MenuItem{Menu: TestingViewTitle, Command: TestingViewAsTree, Group: "viewAs", Order: 18, When: contextkey.Equals("testing.explorerViewMode", "list"),
			Title: "View as Tree"},
		MenuItem{Menu: TestingViewTitle, Command: TestingSortByStatus, Group: "sortBy", Order: 19, Toggled: contextkey.Equals("testing.explorerViewSorting", "status")},
		MenuItem{Menu: TestingViewTitle, Command: TestingSortByLocation, Group: "sortBy", Order: 19, Toggled: contextkey.Equals("testing.explorerViewSorting", "location")},
		MenuItem{Menu: TestingViewTitle, Command: TestingSortByDuration, Group: "sortBy", Order: 19, Toggled: contextkey.Equals("testing.explorerViewSorting", "duration")},
	)

	// a test: the inline buttons and the context menu (VS Code's testItemInlineAndInContext puts them in both; group builtin@1 is the first
	// group of the context menu)
	both := func(cmd string, order float64, when contextkey.Expr) {
		r.MustAdd(
			MenuItem{Menu: TestingItem, Command: cmd, Group: "inline", Order: order, When: when},
			MenuItem{Menu: TestingItem, Command: cmd, Group: "builtin@1", Order: order, When: when})
	}
	both(TestingRun, 11, has("testing.hasRunnableTests"))
	both(TestingDebug, 12, has("testing.hasDebuggableTests"))
	both(TestingCoverage, 13, has("testing.hasCoverableTests"))
	r.MustAdd(
		MenuItem{Menu: TestingItem, Command: TestingGoToTest, Group: "builtin@1", Order: 20, When: has("testing.testItemHasUri")},
		MenuItem{Menu: TestingItem, Command: TestingHideTest, Group: "builtin@2", Order: 21, When: contextkey.Not("testing.testItemIsHidden")},
		MenuItem{Menu: TestingItem, Command: TestingUnhideTest, Group: "builtin@2", Order: 21, When: has("testing.testItemIsHidden")},
		MenuItem{Menu: TestingItem, Command: TestingSelectDefaultProfiles, Group: "builtin@2", Order: 22, When: has("testing.hasNonDefaultProfile")},
	)
	// the continuous-run button is last in the row, so that the others do not shift on hover (VS Code's max-int order)
	r.MustAdd(
		MenuItem{Menu: TestingItem, Command: TestingToggleContinuous, Group: "inline", Order: 1 << 30, When: has("testing.supportsContinuousRun")},
		MenuItem{Menu: TestingItem, Command: TestingToggleContinuous, Group: "builtin@1", Order: 1 << 30, When: has("testing.supportsContinuousRun"),
			ToggledTitle: "Turn off Continuous Run", Toggled: has("testing.isContinuousModeOn")},
	)
}
