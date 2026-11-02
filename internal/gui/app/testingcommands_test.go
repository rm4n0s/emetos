package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	testview "github.com/rm4n0s/emetos/internal/gui/views/testing"
)

// The commands and the keys of the Test Explorer (step 26.2, SPEC §10.32 "Commands and keys"), against the fake controller of
// extensions/devtesting through the real host and model.

// runCommand runs a command, waits for the run it starts to end and gives the newest result.
func (r *testingRig) runCommand(cmd string, args any) *coretesting.Result {
	r.t.Helper()
	before := len(r.svc().Results())
	var first *coretesting.Result
	if rs := r.svc().Results(); len(rs) > 0 {
		first = rs[0]
	}
	r.run(cmd, args)
	r.waitFor("the run of "+cmd, func() bool {
		rs := r.svc().Results()
		return len(rs) > 0 && rs[0] != first && rs[0].IsComplete() && !r.svc().IsRunning() && (len(rs) > before || before == coretesting.MaxResults)
	})
	return r.svc().Results()[0]
}

// leaves are the ids (without the controller's) of the tests of a result that no other test of it is below, sorted.
func leaves(res *coretesting.Result) []string {
	var out []string
	all := res.Tests()
	for _, t := range all {
		if !slices.ContainsFunc(all, func(o coretesting.TestResult) bool { return coretesting.IsAncestorID(t.Item.ID, o.Item.ID) }) {
			out = append(out, strings.ReplaceAll(strings.TrimPrefix(t.Item.ID, devtesting_ctrl+coretesting.IDSeparator), coretesting.IDSeparator, "/"))
		}
	}
	slices.Sort(out)
	return out
}

const devtesting_ctrl = "dev.testing.fake"

// press presses the chords of a key in the window.
func (r *testingRig) press(keys string) {
	r.t.Helper()
	kb, err := keybinding.ParseKeybinding(keys)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, c := range kb {
		sendChord(r.ts, c)
	}
	r.pump()
}

func TestRunAllDebugAllAndCoverageAllRunEveryTestInTheirGroup(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	tests := []struct {
		command string
		group   coretesting.Group
	}{
		{command.TestingRunAll, coretesting.GroupRun},
		{command.TestingDebugAll, coretesting.GroupDebug},
		{command.TestingCoverageAll, coretesting.GroupCoverage},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			res := r.runCommand(tc.command, nil)
			if got := res.Request().Group; got != tc.group {
				t.Errorf("group %v, want %v", got, tc.group)
			}
			want := []string{"calc_test.go/TestAdd", "calc_test.go/TestDiv", "calc_test.go/TestRoot", "calc_test.go/TestSub", "parser_test.go/TestNested", "parser_test.go/TestNumber"}
			if got := leaves(res); !slices.Equal(got, want) {
				t.Errorf("ran %v, want %v", got, want)
			}
		})
	}
	// the states the keys report, and the badge of the icon (the failed tests, testing.countBadge's default)
	for key, want := range map[string]any{"testing.hasAnyResults": true, "testing.isRunning": false, "testing.hasRunnableTests": true,
		"testing.hasDebuggableTests": true, "testing.hasCoverableTests": true, "testing.supportsContinuousRun": true, "testing.providerCount": 1,
		"testing.explorerViewMode": "true", "testing.explorerViewSorting": "location", "testing.isPeekVisible": false} {
		if got := r.win.Keys.Get(key); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	if st, tr, ok := r.svc().StateOf(id("calc_test.go")); !ok || st == nil || tr.State != coretesting.StateFailed {
		t.Errorf("the file shows %v", tr.State)
	}
	if r.state("calc_test.go", "TestAdd") != coretesting.StatePassed || r.state("calc_test.go", "TestDiv") != coretesting.StateFailed ||
		r.state("calc_test.go", "TestRoot") != coretesting.StateSkipped {
		t.Error("the states of the tests are not the script's")
	}
	if got, _ := r.win.Views.Badge(command.ViewContainerTesting); got.Count != 2 || got.Tooltip != "2 failed tests" {
		t.Errorf("the badge is %+v", got)
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

func TestTheCountBadgeFollowsItsSetting(t *testing.T) {
	tests := []struct {
		setting string
		count   int
		tip     string
	}{
		{"failed", 2, "2 failed tests"}, {"passed", 3, "3 passed tests"}, {"skipped", 1, "1 skipped tests"}, {"off", 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.setting, func(t *testing.T) {
			r := newTestingRig(t, testingOpts{settings: map[string]any{"testing.countBadge": tc.setting}})
			r.runCommand(command.TestingRunAll, nil)
			if got, _ := r.win.Views.Badge(command.ViewContainerTesting); got.Count != tc.count || got.Tooltip != tc.tip {
				t.Errorf("the badge is %+v, want %d %q", got, tc.count, tc.tip)
			}
		})
	}
}

func TestTheButtonsOfTheTitleRunWhatTheExplorerShows(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	r.run(command.ViewContainerTesting, nil)
	tests := []struct {
		command string
		filter  string
		group   coretesting.Group
		want    []string
	}{
		{command.TestingRunSelected, "", coretesting.GroupRun, []string{"calc_test.go/TestAdd", "calc_test.go/TestDiv", "calc_test.go/TestRoot", "calc_test.go/TestSub", "parser_test.go/TestNested", "parser_test.go/TestNumber"}},
		{command.TestingRunSelected, "testadd", coretesting.GroupRun, []string{"calc_test.go/TestAdd"}},
		{command.TestingDebugSelected, "parser", coretesting.GroupDebug, []string{"parser_test.go/TestNested", "parser_test.go/TestNumber"}},
		{command.TestingCoverageSelected, "testnumber,testroot", coretesting.GroupCoverage, []string{"calc_test.go/TestRoot", "parser_test.go/TestNumber"}},
	}
	for _, tc := range tests {
		t.Run(tc.command+" "+tc.filter, func(t *testing.T) {
			r.explorer().SetFilterText(tc.filter)
			res := r.runCommand(tc.command, nil)
			if got := res.Request().Group; got != tc.group {
				t.Errorf("group %v, want %v", got, tc.group)
			}
			if got := leaves(res); !slices.Equal(got, tc.want) {
				t.Errorf("ran %v, want %v", got, tc.want)
			}
		})
	}
}

func TestACommandOfATestRunsThatTest(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	tests := []struct {
		command string
		arg     any
		group   coretesting.Group
		want    []string
	}{
		{command.TestingRun, id("calc_test.go", "TestSub"), coretesting.GroupRun, []string{"calc_test.go/TestSub"}},
		{command.TestingDebug, id("parser_test.go"), coretesting.GroupDebug, []string{"parser_test.go/TestNested", "parser_test.go/TestNumber"}},
		{command.TestingCoverage, map[string]any{"id": id("calc_test.go", "TestAdd")}, coretesting.GroupCoverage, []string{"calc_test.go/TestAdd"}},
		{command.TestingRun, []any{id("calc_test.go", "TestAdd"), id("parser_test.go", "TestNumber")}, coretesting.GroupRun, []string{"calc_test.go/TestAdd", "parser_test.go/TestNumber"}},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			res := r.runCommand(tc.command, tc.arg)
			if got := res.Request().Group; got != tc.group {
				t.Errorf("group %v, want %v", got, tc.group)
			}
			if got := leaves(res); !slices.Equal(got, tc.want) {
				t.Errorf("ran %v, want %v", got, tc.want)
			}
		})
	}
	// with no argument the command runs the test under the focus of the tree
	r.run(command.ViewContainerTesting, nil)
	r.explorer().Model().SetFocus(id("calc_test.go", "TestRoot"))
	res := r.runCommand(command.TestingRun, nil)
	if got := leaves(res); !slices.Equal(got, []string{"calc_test.go/TestRoot"}) {
		t.Errorf("ran %v for the focused row", got)
	}
}

func TestTheEditorsCommandsRunTheTestsAtTheCursorAndInTheFile(t *testing.T) {
	r := newTestingRig(t, testingOpts{open: []string{"calc_test.go"}})
	e := r.win.shell.activeEditor()
	if e == nil {
		t.Fatal("no editor")
	}
	at := func(line int) {
		e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: line, Col: 0})})
		r.pump()
	}
	tests := []struct {
		command string
		line    int
		group   coretesting.Group
		want    []string
	}{
		{command.TestingRunAtCursor, 7, coretesting.GroupRun, []string{"calc_test.go/TestSub"}},
		{command.TestingDebugAtCursor, 11, coretesting.GroupDebug, []string{"calc_test.go/TestDiv"}},
		{command.TestingCoverageAtCursor, 3, coretesting.GroupCoverage, []string{"calc_test.go/TestAdd"}},
		{command.TestingRunCurrentFile, 0, coretesting.GroupRun, []string{"calc_test.go/TestAdd", "calc_test.go/TestDiv", "calc_test.go/TestRoot", "calc_test.go/TestSub"}},
		{command.TestingDebugCurrentFile, 0, coretesting.GroupDebug, []string{"calc_test.go/TestAdd", "calc_test.go/TestDiv", "calc_test.go/TestRoot", "calc_test.go/TestSub"}},
		{command.TestingCoverageCurrentFile, 0, coretesting.GroupCoverage, []string{"calc_test.go/TestAdd", "calc_test.go/TestDiv", "calc_test.go/TestRoot", "calc_test.go/TestSub"}},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			at(tc.line)
			res := r.runCommand(tc.command, nil)
			if got := res.Request().Group; got != tc.group {
				t.Errorf("group %v, want %v", got, tc.group)
			}
			if got := leaves(res); !slices.Equal(got, tc.want) {
				t.Errorf("ran %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRerunningTheLastRunAndTheFailedTests(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	r.runCommand(command.TestingRunAll, nil)
	failed := []string{"calc_test.go/TestDiv", "parser_test.go/TestNested"}
	tests := []struct {
		command string
		group   coretesting.Group
		want    []string
	}{
		{command.TestingReRunFailTests, coretesting.GroupRun, failed},
		{command.TestingDebugFailTests, coretesting.GroupDebug, failed},
		{command.TestingReRunFailedLastRun, coretesting.GroupRun, failed},
		{command.TestingDebugFailedLastRun, coretesting.GroupDebug, failed},
		// the last run is the one of the failed tests; Rerun Last Run runs them in the Run group when the last run was a debug one (VS Code's rule)
		{command.TestingReRunLastRun, coretesting.GroupRun, failed},
		{command.TestingDebugLastRun, coretesting.GroupDebug, failed},
		{command.TestingCoverageLastRun, coretesting.GroupCoverage, failed},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			res := r.runCommand(tc.command, nil)
			if got := res.Request().Group; got != tc.group {
				t.Errorf("group %v, want %v", got, tc.group)
			}
			if got := leaves(res); !slices.Equal(got, tc.want) {
				t.Errorf("ran %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCancelingARunEndsItAndClearingTheResultsTakesThemAway(t *testing.T) {
	slow := `{"delayMs": 600000, "tests": [{"id": "f", "label": "f", "children": [{"id": "A", "label": "A", "result": "passed", "ms": 1}, {"id": "B", "label": "B", "result": "passed", "ms": 1}]}]}`
	r := newTestingRig(t, testingOpts{script: slow})
	r.run(command.TestingRunAll, nil)
	r.waitFor("the run to go", func() bool {
		return r.win.Keys.Get("testing.isRunning") == true && r.state("f", "A") == coretesting.StateRunning
	})
	if got := r.state("f", "B"); got != coretesting.StateQueued {
		t.Errorf("the second test is %v, want queued", got)
	}
	r.run(command.TestingCancelRun, nil)
	r.finished()
	r.waitFor("the keys", func() bool { return r.win.Keys.Get("testing.isRunning") == false })
	if got := r.state("f", "B"); got != coretesting.StateSkipped {
		t.Errorf("the second test is %v after the cancel, want skipped", got)
	}
	r.run(command.TestingClearResults, nil)
	r.waitFor("the results to go", func() bool { return r.win.Keys.Get("testing.hasAnyResults") == false })
	if r.svc().HasAnyResults() {
		t.Error("the results are still there")
	}
}

func TestRefreshingTheTestsAndTheOtherCommandsOfTheViewWorkWithoutAHandler(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	for _, id := range []string{command.TestingRefreshTests, command.TestingCancelRefresh, command.TestingShowMostRecentOutput, command.TestingUnhideAll,
		command.TestingFilter, command.TestingFocusExplorer} {
		r.run(id, nil)
	}
	r.waitFor("the refresh to end", func() bool { return r.win.Keys.Get("testing.isRefreshing") == false })
	if r.win.Keys.Get("testing.canRefresh") != true {
		t.Error("testing.canRefresh is not set with a controller")
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestShowOutputOpensTheOutputOfTheLastRunInTheOutputPanel(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	// the fake controller prints nothing: the command says so and writes no channel
	r.runCommand(command.TestingRunAll, nil)
	r.run(command.TestingShowMostRecentOutput, nil)
	if lines := r.win.Process.Channels.Lines(testResultsChannel); len(lines) != 0 {
		t.Errorf("the channel has %q for a run with no output", lines)
	}
	// a run with output: the lines show in the channel with their colors taken away
	live := r.svc().StartExternalRun(coretesting.ExternalRun{Request: coretesting.RunRequest{Group: coretesting.GroupRun}})
	task, err := live.AddTask(coretesting.TaskSpec{Name: "x", ControllerID: devtesting_ctrl})
	if err != nil {
		t.Fatal(err)
	}
	task.AppendOutput([]byte("\x1b[31mFAIL\x1b[0m TestDiv\r\nline two\r\n"), nil, "")
	task.End()
	r.run(command.TestingShowMostRecentOutput, nil)
	lines := r.win.Process.Channels.Lines(testResultsChannel)
	if len(lines) != 2 || lines[0] != "FAIL TestDiv" || lines[1] != "line two" {
		t.Errorf("the channel has %q", lines)
	}
	if r.win.Views.ActivePanel() != command.ViewContainerOutput {
		t.Errorf("the panel shows %q, want the Output", r.win.Views.ActivePanel())
	}
}

func TestViewModeAndSortingChangeTheRowsAndTheKeys(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	r.run(command.ViewContainerTesting, nil)
	r.runCommand(command.TestingRunAll, nil)
	rows := func() []string {
		var out []string
		for _, n := range r.explorer().Model().Rows() {
			out = append(out, n.Label)
		}
		return out
	}
	// the tree, by location: the files, each with its tests in the order of their label (no lines in the order of the controller)
	if got := rows(); len(got) != 8 || got[0] != "calc_test.go" {
		t.Fatalf("the tree has %q", got)
	}
	r.run(command.TestingViewAsList, nil)
	if got := rows(); len(got) != 6 || r.win.Keys.Get("testing.explorerViewMode") != "list" {
		t.Errorf("the list has %q, mode %v", got, r.win.Keys.Get("testing.explorerViewMode"))
	}
	for _, tc := range []struct {
		command string
		key     string
		first   string
	}{
		{command.TestingSortByStatus, "status", "TestDiv"}, // failed first: TestDiv and TestNested, by line (a_test comes first)
		{command.TestingSortByDuration, "duration", "TestDiv"},
		{command.TestingSortByLocation, "location", "TestAdd"},
	} {
		r.run(tc.command, nil)
		got := rows()
		if r.win.Keys.Get("testing.explorerViewSorting") != tc.key || got[0] != tc.first {
			t.Errorf("%s: sorting %v, first row %q", tc.command, r.win.Keys.Get("testing.explorerViewSorting"), got[0])
		}
	}
	r.run(command.TestingViewAsTree, nil)
	if got := rows(); len(got) != 8 || r.win.Keys.Get("testing.explorerViewMode") != "true" {
		t.Errorf("the tree has %q", got)
	}
	r.run(command.TestingCollapseAll, nil)
	if got := rows(); len(got) != 2 {
		t.Errorf("collapsed the tree has %q", got)
	}
}

func TestHidingTestsAndShowingThemAgain(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	r.run(command.ViewContainerTesting, nil)
	count := func() int { return r.explorer().Model().Len() }
	r.run(command.TestingHideTest, id("calc_test.go", "TestAdd"))
	if got := count(); got != 7 || !r.explorer().IsHidden(id("calc_test.go", "TestAdd")) {
		t.Errorf("%d rows after hiding a test", got)
	}
	r.run(command.TestingHideTest, id("parser_test.go"))
	if got := count(); got != 4 {
		t.Errorf("%d rows after hiding a file", got)
	}
	// @hidden shows them again, marked
	r.explorer().SetFilterText("@hidden")
	r.pump()
	if got := count(); got != 8 {
		t.Errorf("%d rows with @hidden", got)
	}
	r.explorer().SetFilterText("")
	r.run(command.TestingUnhideTest, id("parser_test.go"))
	if got := count(); got != 7 {
		t.Errorf("%d rows after unhiding a file", got)
	}
	r.run(command.TestingUnhideAll, nil)
	if got := count(); got != 8 || r.explorer().HasHidden() {
		t.Errorf("%d rows after unhiding all", got)
	}
}

func TestGoToTestOpensTheFileAtTheTest(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	r.run(command.TestingGoToTest, id("calc_test.go", "TestSub"))
	r.waitFor("the editor", func() bool {
		e := r.win.shell.activeEditor()
		return e != nil && e.doc != nil && filepath.Base(e.doc.URI().Path) == "calc_test.go"
	})
	e := r.win.shell.activeEditor()
	if line := e.view.Cursors().Primary().Selection.Active.Line; line != 6 {
		t.Errorf("the cursor is on line %d, want 6 (line 7)", line)
	}
}

func TestContinuousRuns(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	svc := r.svc()
	// one test: on, and off again
	test := id("calc_test.go", "TestAdd")
	r.run(command.TestingToggleContinuous, test)
	r.waitFor("the continuous run", func() bool {
		return svc.ContinuousSpecificallyFor(test) && r.win.Keys.Get("testing.isContinuousModeOn") == true
	})
	r.finished()
	r.run(command.TestingToggleContinuous, test)
	r.waitFor("the end of it", func() bool {
		return !svc.ContinuousSpecificallyFor(test) && r.win.Keys.Get("testing.isContinuousModeOn") == false
	})
	// all: start and stop
	r.run(command.TestingStartContinuous, nil)
	r.waitFor("all of them", func() bool { return svc.IsContinuousEnabled() })
	r.finished()
	r.run(command.TestingStopContinuous, nil)
	r.waitFor("none of them", func() bool { return !svc.IsContinuousEnabled() })
}

func TestTheProfilePickersListTheProfiles(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	r.run(command.TestingSelectDefaultProfiles, "debug")
	if !r.win.QuickInput.Visible() {
		t.Fatal("the picker of the default profile did not open")
	}
	r.win.QuickInput.Accept(quickinput.KeyMods{})
	r.pump()
	got := r.svc().GroupDefaults(coretesting.GroupDebug, "")
	if len(got) != 1 || got[0].Label != "Debug Tests" {
		t.Errorf("the defaults of Debug are %+v", got)
	}
	// nothing to configure in the fake: no picker
	r.run(command.TestingConfigureProfile, nil)
	if r.win.QuickInput.Visible() {
		t.Error("a picker of profiles to configure opened for profiles with nothing to configure")
	}
}

// The keys: the chords of VS Code, pressed in the window, run their commands.
func TestTheChordsRunTheirCommands(t *testing.T) {
	r := newTestingRig(t, testingOpts{open: []string{"calc_test.go"}})
	e := r.win.shell.activeEditor()
	e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 7, Col: 0})})
	r.pump()
	if e.focus.Request() {
		r.ts.Pump(time.Millisecond)
	}
	tests := []struct {
		keys  string
		group coretesting.Group
		want  []string
	}{
		{"ctrl+; a", coretesting.GroupRun, nil},
		{"ctrl+; ctrl+a", coretesting.GroupDebug, nil},
		{"ctrl+; ctrl+shift+a", coretesting.GroupCoverage, nil},
		{"ctrl+; c", coretesting.GroupRun, []string{"calc_test.go/TestSub"}},
		{"ctrl+; ctrl+c", coretesting.GroupDebug, []string{"calc_test.go/TestSub"}},
		{"ctrl+; ctrl+shift+c", coretesting.GroupCoverage, []string{"calc_test.go/TestSub"}},
		{"ctrl+; f", coretesting.GroupRun, []string{"calc_test.go/TestAdd", "calc_test.go/TestDiv", "calc_test.go/TestRoot", "calc_test.go/TestSub"}},
		{"ctrl+; ctrl+f", coretesting.GroupDebug, []string{"calc_test.go/TestAdd", "calc_test.go/TestDiv", "calc_test.go/TestRoot", "calc_test.go/TestSub"}},
		{"ctrl+; ctrl+shift+f", coretesting.GroupCoverage, []string{"calc_test.go/TestAdd", "calc_test.go/TestDiv", "calc_test.go/TestRoot", "calc_test.go/TestSub"}},
		{"ctrl+; e", coretesting.GroupRun, []string{"calc_test.go/TestDiv", "parser_test.go/TestNested"}},
		{"ctrl+; ctrl+e", coretesting.GroupDebug, []string{"calc_test.go/TestDiv", "parser_test.go/TestNested"}},
		{"ctrl+; l", coretesting.GroupRun, []string{"calc_test.go/TestDiv", "parser_test.go/TestNested"}},
		{"ctrl+; ctrl+l", coretesting.GroupDebug, []string{"calc_test.go/TestDiv", "parser_test.go/TestNested"}},
		{"ctrl+; ctrl+shift+l", coretesting.GroupCoverage, []string{"calc_test.go/TestDiv", "parser_test.go/TestNested"}},
	}
	// the first key runs everything, which the others rerun
	for _, tc := range tests {
		t.Run(tc.keys, func(t *testing.T) {
			var first *coretesting.Result
			if rs := r.svc().Results(); len(rs) > 0 {
				first = rs[0]
			}
			r.press(tc.keys)
			r.waitFor("the run of "+tc.keys, func() bool {
				rs := r.svc().Results()
				return len(rs) > 0 && rs[0] != first && rs[0].IsComplete() && !r.svc().IsRunning()
			})
			res := r.svc().Results()[0]
			if got := res.Request().Group; got != tc.group {
				t.Errorf("group %v, want %v", got, tc.group)
			}
			if tc.want != nil {
				if got := leaves(res); !slices.Equal(got, tc.want) {
					t.Errorf("ran %v, want %v", got, tc.want)
				}
			} else if len(leaves(res)) != 6 {
				t.Errorf("ran %v, want all six", leaves(res))
			}
		})
	}
	t.Run("ctrl+; ctrl+o shows the output", func(t *testing.T) {
		r.press("ctrl+; ctrl+o") // the fake prints nothing, the command runs without an error
	})
	t.Run("ctrl+; ctrl+x cancels", func(t *testing.T) {
		r.press("ctrl+; ctrl+x")
	})
	t.Run("ctrl+; ctrl+r refreshes", func(t *testing.T) {
		r.press("ctrl+; ctrl+r")
		r.waitFor("the refresh", func() bool { return r.win.Keys.Get("testing.isRefreshing") == false })
	})
	t.Run("alt+enter in the explorer goes to the test", func(t *testing.T) {
		r.run(command.ViewContainerTesting, nil)
		r.win.Keys.Set("focusedView", command.ViewTestExplorer)
		r.explorer().Model().SetFocus(id("parser_test.go", "TestNested"))
		r.press("alt+enter")
		r.waitFor("the editor of the test", func() bool {
			e := r.win.shell.activeEditor()
			return e != nil && e.doc != nil && filepath.Base(e.doc.URI().Path) == "parser_test.go"
		})
	})
}

func TestTheFilterBoxAndItsMenu(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	r.run(command.ViewContainerTesting, nil)
	r.runCommand(command.TestingRunAll, nil)
	s := r.win.shell
	// the menu toggles the terms in the text
	items := s.testingFilterMenu()
	var labels []string
	for _, it := range items {
		if !it.Separator {
			labels = append(labels, it.Label)
		}
	}
	want := []string{"Show Only Failed Tests", "Show Only Executed Tests", "Show in Active File Only", "Show in Opened Files Only", "Fuzzy Match", "Show Hidden Tests", "Unhide All Tests"}
	if !slices.Equal(labels, want) {
		t.Errorf("the menu has %q, want %q", labels, want)
	}
	items[0].OnRun()
	if got := r.explorer().FilterText(); got != "@failed" {
		t.Errorf("the text is %q after the first entry", got)
	}
	if r.explorer().Model().Len() != 4 {
		t.Errorf("%d rows show with @failed, want the 2 files and their failed tests", r.explorer().Model().Len())
	}
	s.testingFilterMenu()[0].OnRun()
	if got := r.explorer().FilterText(); got != "" {
		t.Errorf("the text is %q after the entry again", got)
	}
	// typing in the box filters, and the box shows the text of a filter set by code
	r.explorer().SetFilterText("testdiv")
	if got := r.explorer().Input().Value(); got != "testdiv" {
		t.Errorf("the box has %q", got)
	}
	if got := r.explorer().Model().Len(); got != 2 {
		t.Errorf("%d rows for testdiv, want the file and the test", got)
	}
	// the filter box is the command's target
	r.run(command.TestingFilter, nil)
	r.waitFor("the filter box to have the focus", func() bool { return r.explorer().Input().HasFocus() })
}

func TestTheSummaryAndTheTitleFollowTheRun(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	r.run(command.ViewContainerTesting, nil)
	if got := testview.SummaryOf(r.svc(), r.win.Process.testingElapsed); !got.None {
		t.Errorf("a summary before a run: %+v", got)
	}
	r.runCommand(command.TestingRunAll, nil)
	got := testview.SummaryOf(r.svc(), r.win.Process.testingElapsed)
	if got.None || got.Running || got.Text != "3/5 tests passed (60%, 1 skipped)" || got.Dominant != coretesting.StateFailed {
		t.Errorf("the summary is %+v", got)
	}
}
