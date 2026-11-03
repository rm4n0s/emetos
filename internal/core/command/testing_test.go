package command

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// The commands, views and menus of Testing (step 26.2).

func TestTestingCommandsAreRegisteredWithVSCodesTitles(t *testing.T) {
	reg := DefaultRegistry()
	want := map[string]string{
		"testing.runAll": "Run All Tests", "testing.debugAll": "Debug All Tests", "testing.coverageAll": "Run All Tests with Coverage",
		"testing.runSelected": "Run Tests", "testing.debugSelected": "Debug Tests", "testing.coverageSelected": "Run Tests with Coverage",
		"testing.run": "Run Test", "testing.debug": "Debug Test", "testing.coverage": "Run Test with Coverage",
		"testing.runAtCursor": "Run Test at Cursor", "testing.debugAtCursor": "Debug Test at Cursor", "testing.coverageAtCursor": "Run Test at Cursor with Coverage",
		"testing.runCurrentFile": "Run Tests in Current File", "testing.debugCurrentFile": "Debug Tests in Current File",
		"testing.coverageCurrentFile": "Run Tests with Coverage in Current File",
		"testing.reRunLastRun":        "Rerun Last Run", "testing.debugLastRun": "Debug Last Run", "testing.coverageLastRun": "Rerun Last Run with Coverage",
		"testing.reRunFailTests": "Rerun Failed Tests", "testing.debugFailTests": "Debug Failed Tests",
		"testing.reRunFailedFromLastRun": "Rerun Failed Tests from Last Run", "testing.debugFailedFromLastRun": "Debug Failed Tests from Last Run",
		"testing.showMostRecentOutput": "Show Output", "testing.cancelRun": "Cancel Test Run", "testing.refreshTests": "Refresh Tests",
		"testing.cancelTestRefresh": "Cancel Test Refresh", "testing.clearTestResults": "Clear All Results", "testing.collapseAll": "Collapse All Tests",
		"testing.viewAsList": "View as List", "testing.viewAsTree": "View as Tree", "testing.sortByStatus": "Sort by Status",
		"testing.sortByLocation": "Sort by Location", "testing.sortByDuration": "Sort by Duration", "testing.editFocusedTest": "Go to Test",
		"testing.hideTest": "Hide Test", "testing.unhideTest": "Unhide Test", "testing.unhideAllTests": "Unhide All Tests",
		"testing.toggleContinuousRunForTest": "Turn on Continuous Run", "testing.startContinuousRun": "Start Continuous Run",
		"testing.stopContinuousRun": "Stop Continuous Run", "testing.selectDefaultTestProfiles": "Select Default Profile",
		"testing.configureProfile": "Configure Test Profiles", "workbench.view.extension.test": "Show Testing",
		"workbench.view.testing.focus": "Focus on Test Explorer View", "workbench.actions.treeView.testExplorer.filter": "Filter Tests",
	}
	for id, title := range want {
		m, ok := reg.Lookup(id)
		if !ok {
			t.Errorf("%s is not a command", id)
			continue
		}
		if m.Title != title {
			t.Errorf("%s has the title %q, want %q", id, m.Title, title)
		}
	}
}

func TestTestingContainerAndViewAreRegistered(t *testing.T) {
	r := DefaultViews()
	c, ok := r.Container(ViewContainerTesting)
	if !ok || c.Title != "Testing" || c.Icon != 0xEA79 || c.Location != ViewLocationSideBar {
		t.Fatalf("the container: %+v", c)
	}
	var debugOrder, testingOrder int
	for _, x := range r.Containers(ViewLocationSideBar) {
		switch x.ID {
		case ViewContainerDebug:
			debugOrder = x.Order
		case ViewContainerTesting:
			testingOrder = x.Order
		}
	}
	if testingOrder <= debugOrder {
		t.Errorf("Testing (%d) is not after Run and Debug (%d)", testingOrder, debugOrder)
	}
	v, ok := r.View(ViewTestExplorer)
	if !ok || v.Name != "Test Explorer" || v.Container != ViewContainerTesting {
		t.Errorf("the view: %+v", v)
	}
}

// ctx is a context for the when-clauses of the menus.
func keys(kv ...any) contextkey.MapContext {
	m := contextkey.MapContext{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func commandsOf(entries []MenuEntry) []string {
	var out []string
	for _, e := range entries {
		if e.Command != "" {
			out = append(out, e.Command)
		}
	}
	return out
}

func TestTheTitleBarOfTheTestExplorerFollowsTheState(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	tests := []struct {
		name string
		ctx  contextkey.MapContext
		want []string // the buttons: group navigation
	}{
		{"idle with everything", keys("testing.canRefresh", true, "testing.hasRunnableTests", true, "testing.hasDebuggableTests", true,
			"testing.hasCoverableTests", true, "testing.supportsContinuousRun", true, "testing.hasAnyResults", true),
			[]string{TestingRefreshTests, TestingRunSelected, TestingDebugSelected, TestingCoverageSelected, TestingStartContinuous, TestingShowMostRecentOutput}},
		{"while it runs", keys("testing.canRefresh", true, "testing.isRunning", true, "testing.hasRunnableTests", true, "testing.hasDebuggableTests", true),
			[]string{TestingRefreshTests, TestingCancelRun, TestingShowMostRecentOutput}},
		{"while it refreshes", keys("testing.canRefresh", true, "testing.isRefreshing", true, "testing.hasRunnableTests", true),
			[]string{TestingCancelRefresh, TestingRunSelected, TestingShowMostRecentOutput}},
		{"continuous mode on", keys("testing.supportsContinuousRun", true, "testing.isContinuousModeOn", true), []string{TestingStopContinuous, TestingShowMostRecentOutput}},
		{"nothing but the output button, which is there dimmed", keys(), []string{TestingShowMostRecentOutput}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.ctx
			ctx["view"] = ViewTestExplorer
			var got []string
			for _, e := range menus.Items(TestingViewTitle, ctx) {
				if e.Command != "" && e.Group == "navigation" {
					got = append(got, e.Command)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("buttons %v, want %v", got, tc.want)
			}
		})
	}
	// the other items are behind "...": the display actions, the view mode and the sort order, with the current ones checked
	ctx := keys("testing.explorerViewMode", "true", "testing.explorerViewSorting", "status", "testing.hasAnyResults", true)
	var more []string
	checked := map[string]bool{}
	for _, e := range menus.Items(TestingViewTitle, ctx) {
		if e.Command != "" && e.Group != "navigation" {
			more = append(more, e.Command)
			checked[e.Command] = e.Checked
		}
	}
	// the items of one group with one order are by title, as VS Code sorts them
	wantMore := []string{TestingCollapseAll, TestingClearResults, TestingSortByDuration, TestingSortByLocation, TestingSortByStatus, TestingViewAsList}
	if !slices.Equal(more, wantMore) {
		t.Errorf("the menu has %v, want %v", more, wantMore)
	}
	if !checked[TestingSortByStatus] || checked[TestingSortByLocation] || checked[TestingSortByDuration] {
		t.Errorf("checked: %v", checked)
	}
}

func TestTheMenuOfATestHasItsButtonsAndItsContextMenu(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	full := keys("testing.hasRunnableTests", true, "testing.hasDebuggableTests", true, "testing.hasCoverableTests", true, "testing.testItemHasUri", true,
		"testing.supportsContinuousRun", true, "testing.testItemIsHidden", false, "testing.hasNonDefaultProfile", true)
	var inline, context []string
	for _, e := range menus.Items(TestingItem, full) {
		if e.Command == "" {
			continue
		}
		if e.Group == "inline" {
			inline = append(inline, e.Command)
		} else {
			context = append(context, e.Command)
		}
	}
	if want := []string{TestingRun, TestingDebug, TestingCoverage, TestingToggleContinuous}; !slices.Equal(inline, want) {
		t.Errorf("the buttons are %v, want %v", inline, want)
	}
	if want := []string{TestingRun, TestingDebug, TestingCoverage, TestingGoToTest, TestingToggleContinuous, TestingHideTest, TestingSelectDefaultProfiles}; !slices.Equal(context, want) {
		t.Errorf("the context menu is %v, want %v", context, want)
	}
	// a hidden test offers Unhide, a test with no file no Go to Test, and with no profile there are no run buttons
	hidden := keys("testing.testItemIsHidden", true)
	if got := commandsOf(menus.Items(TestingItem, hidden)); !slices.Equal(got, []string{TestingUnhideTest}) {
		t.Errorf("a hidden test with nothing else: %v", got)
	}
}

func TestTestingIsInTheViewMenuAndThePalette(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	var found bool
	for _, e := range menus.Items(MenubarViewMenu, keys()) {
		if e.Command == ViewContainerTesting && e.Title == "Testing" {
			found = true
		}
	}
	if !found {
		t.Error("the View menu has no Testing entry")
	}
	// the palette lists Run All Tests only while a controller has a profile that runs
	listed := func(id string, ctx contextkey.MapContext) bool {
		for _, e := range menus.Items(CommandPalette, ctx) {
			if e.Command == id {
				return true
			}
		}
		return false
	}
	if listed(TestingRunAll, keys()) || !listed(TestingRunAll, keys("testing.hasRunnableTests", true)) {
		t.Error("Run All Tests is not listed as the runnable tests say")
	}
	if listed(TestingRun, keys("testing.hasRunnableTests", true)) {
		t.Error("the commands of a row are in the palette")
	}
}

// The commands of the coverage (step 26.4): VS Code's ids and titles.
func TestTestingCoverageCommandsAreRegisteredWithVSCodesTitles(t *testing.T) {
	r := DefaultRegistry()
	want := map[string]string{
		"testing.toggleInlineCoverage": "Toggle Inline Coverage", "testing.coverageToggleToolbar": "Show Test Coverage Toolbar",
		"testing.toggleCoverageInExplorer": "Toggle Coverage in Explorer", "testing.coverage.goToNextMissedLine": "Go to Next Uncovered Line",
		"testing.coverage.goToPreviousMissedLine": "Go to Previous Uncovered Line", "testing.coverage.close": "Clear Coverage",
		"testing.coverageViewChangeSorting": "Change Sort Order", "testing.coverageViewCollapseAll": "Collapse All Coverage",
		"testing.openCoverage": "Open Coverage", "workbench.view.testCoverage.focus": "Focus on Test Coverage View",
	}
	for id, title := range want {
		m, ok := r.Lookup(id)
		if !ok {
			t.Errorf("%s is not a command", id)
			continue
		}
		if m.Title != title {
			t.Errorf("%s has the title %q, want %q", id, m.Title, title)
		}
	}
}

// The Test Coverage view shows in the Testing container while a coverage report is open, after the Test Explorer.
func TestTheTestCoverageViewShowsWhileCoverageIsOpen(t *testing.T) {
	r := DefaultViews()
	v, ok := r.View(ViewTestCoverage)
	if !ok || v.Name != "Test Coverage" || v.Container != ViewContainerTesting || v.ID != "workbench.view.testCoverage" {
		t.Fatalf("the view: %+v", v)
	}
	ids := func(ctx contextkey.Context) (out []string) {
		for _, x := range r.Views(ViewContainerTesting, ctx) {
			out = append(out, x.ID)
		}
		return out
	}
	if got := ids(keys()); !slices.Equal(got, []string{ViewTestExplorer}) {
		t.Errorf("with no coverage the views are %v", got)
	}
	if got := ids(keys("testing.isTestCoverageOpen", true)); !slices.Equal(got, []string{ViewTestExplorer, ViewTestCoverage}) {
		t.Errorf("with coverage the views are %v", got)
	}
}

// The title bar of the Test Coverage view has the sort order, Collapse All and Clear Coverage, in VS Code's order; the editor's title has the
// inline toggle while the toolbar of the editor is off, and the coverage group while the file has coverage.
func TestTheMenusOfTheCoverage(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	var title []string
	for _, e := range menus.Items(TestingCoverageTitle, keys("view", ViewTestCoverage)) {
		if e.Command != "" && e.Group == "navigation" {
			title = append(title, e.Command)
		}
	}
	if want := []string{TestingCoverageChangeSorting, TestingCoverageCollapseAll, TestingCoverageClose}; !slices.Equal(title, want) {
		t.Errorf("the title bar has %v, want %v", title, want)
	}
	editorTitle := func(ctx contextkey.MapContext) (out []string) {
		for _, e := range menus.Items(EditorTitle, ctx) {
			out = append(out, e.Command)
		}
		return out
	}
	if got := editorTitle(keys()); slices.Contains(got, TestingToggleInlineCoverage) || slices.Contains(got, TestingCoverageNextMissed) {
		t.Errorf("a file with no coverage has %v", got)
	}
	details := keys("testing.hasInlineCoverageDetails", true, "testing.hasCoverageInFile", true)
	got := editorTitle(details)
	for _, id := range []string{TestingToggleInlineCoverage, TestingToggleCoverageToolbar, TestingCoverageNextMissed, TestingCoveragePreviousMissed} {
		if !slices.Contains(got, id) {
			t.Errorf("a file with coverage has %v, want %s", got, id)
		}
	}
	details["testing.coverageToolbarEnabled"] = true
	if got := editorTitle(details); slices.Contains(got, TestingToggleInlineCoverage) {
		t.Errorf("with the toolbar of the editor on the title still has the inline toggle: %v", got)
	}
}

// The commands of the coverage are in the palette only while a report is open.
func TestTheCoverageCommandsAreInThePaletteWhileCoverageIsOpen(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	in := func(ctx contextkey.MapContext, id string) bool {
		for _, e := range menus.Items(CommandPalette, ctx) {
			if e.Command == id {
				return true
			}
		}
		return false
	}
	for _, id := range []string{TestingToggleInlineCoverage, TestingCoverageClose, TestingToggleCoverageInExplorer, TestingCoverageNextMissed} {
		if in(keys(), id) || !in(keys("testing.isTestCoverageOpen", true), id) {
			t.Errorf("%s is in the palette without coverage, or not with it", id)
		}
	}
	if in(keys(), TestingOpenCoverage) || !in(keys("testing.hasAnyResults", true), TestingOpenCoverage) {
		t.Error("Open Coverage is in the palette without results, or not with them")
	}
}
