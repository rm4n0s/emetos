package javascript

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestAJestRunReportsEveryTestWithItsMessagesAndOutput(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	run := r.run(RunnerJest, nil, nil)
	want := map[string]string{
		"src|math.test.js/math/adds": "passed", "src|math.test.js/math/divides": "failed", "src|math.test.js/math/later": "skipped",
		"src|math.test.js/math/nested/throws on zero": "passed", "src|math.test.js/prints and passes": "passed",
		"src|other.test.js/other passes": "passed", "src|other.test.js/other crashes": "failed",
		// the names a test.each made are only known to the run: they are added to the tree
		"src|math.test.js/math/adds 1 and 1": "passed", "src|math.test.js/math/adds 2 and 3": "passed",
	}
	got := runOutcomes(run)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d items reported, want %d: %v", len(got), len(want), got)
	}
	msgs := runMessages(run, "src|math.test.js/math/divides")
	if len(msgs) != 1 || msgs[0].ExpectedOutput != "4" || msgs[0].ActualOutput != "5" || !strings.Contains(msgs[0].Message, "Received: 5") {
		t.Fatalf("divides: %+v", msgs)
	}
	loc := msgs[0].Location
	if loc == nil || loc.URI != fileURI(filepath.Join(r.folder, "src/math.test.js")) || loc.Range.Start.Line != 8 || loc.Range.Start.Col != 26 || loc.Range.End.Col != 34 {
		t.Errorf("location %+v", loc)
	}
	if len(msgs[0].StackTrace) == 0 || msgs[0].StackTrace[0].Position.Line != 8 {
		t.Errorf("stack %+v", msgs[0].StackTrace)
	}
	crash := runMessages(run, "src|other.test.js/other crashes")
	if len(crash) != 1 || crash[0].Message != "boom" || crash[0].Location == nil || crash[0].Location.Range.Start.Line != 5 {
		t.Errorf("crash: %+v", crash)
	}
	out := runOutput(run, "")
	if !strings.HasPrefix(out, "$ jest --runTestsByPath src/math.test.js src/other.test.js\r\n") || !strings.Contains(out, "hello from a passing test") ||
		strings.Contains(out, r.folder+"/node_modules") || strings.Contains(out, "report.json") {
		t.Errorf("output %q", out)
	}
	// the items the run added are in the tree below their suite, with the place the runner gave
	added := r.item(RunnerJest, "src|math.test.js/math/adds 2 and 3")
	if added.Parent().Data().Label != "math" || added.Data().Range == nil || added.Data().Range.Start.Line != 22 {
		t.Errorf("the added item: %+v", added.Data())
	}
	if want := "$ jest --runTestsByPath"; !strings.Contains(strings.Join(r.fr.called(), "\n"), "--json --outputFile=") || !strings.Contains(out, want) {
		t.Errorf("calls %q", r.fr.called())
	}
}

func TestChosenTestsRunWithTheirNamesAsTheFilterAndOnlyTheyAreReported(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	for _, tc := range []struct {
		name    string
		include []string
		filter  string
		want    map[string]string
	}{
		{"a test", []string{"src|math.test.js/math/adds"}, "-t (^math adds$) --runTestsByPath src/math.test.js",
			map[string]string{"src|math.test.js/math/adds": "passed"}},
		{"a suite", []string{"src|math.test.js/math/nested"}, "-t (^math nested( |$)) --runTestsByPath src/math.test.js",
			map[string]string{"src|math.test.js/math/nested/throws on zero": "passed"}},
		{"tests of two files", []string{"src|math.test.js/math/divides", "src|other.test.js/other passes"}, "-t (^math divides$|^other passes$) --runTestsByPath src/math.test.js src/other.test.js",
			map[string]string{"src|math.test.js/math/divides": "failed", "src|other.test.js/other passes": "passed"}},
		{"a file whole", []string{"src|other.test.js"}, "--runTestsByPath src/other.test.js",
			map[string]string{"src|other.test.js/other passes": "passed", "src|other.test.js/other crashes": "failed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := len(r.fr.called())
			run := r.run(RunnerJest, r.items(RunnerJest, tc.include...), nil)
			calls := r.fr.called()[n:]
			if len(calls) != 1 || !strings.Contains(calls[0], tc.filter) {
				t.Errorf("calls %q, want one with %q", calls, tc.filter)
			}
			got := runOutcomes(run)
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s: %q, want %q", k, got[k], v)
				}
			}
			if len(got) != len(tc.want) {
				t.Errorf("reported %v", got)
			}
		})
	}
}

func TestWholeFilesAndFilteredFilesRunInTheirOwnProcesses(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	n := len(r.fr.called())
	run := r.run(RunnerJest, r.items(RunnerJest, "src|other.test.js", "src|math.test.js/math/adds"), nil)
	calls := r.fr.called()[n:]
	if len(calls) != 2 || !strings.HasSuffix(calls[0], "--runTestsByPath src/other.test.js") || !strings.Contains(calls[1], "-t (^math adds$) --runTestsByPath src/math.test.js") {
		t.Errorf("calls %q", calls)
	}
	if got := runOutcomes(run); len(got) != 3 {
		t.Errorf("reported %v", got)
	}
}

func TestExcludedTestsAreNotRun(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	n := len(r.fr.called())
	run := r.run(RunnerJest, r.items(RunnerJest, "src|math.test.js"), r.items(RunnerJest, "src|math.test.js/math/divides", "src|math.test.js/math/nested"))
	calls := r.fr.called()[n:]
	if len(calls) != 1 || !strings.Contains(calls[0], "-t (^math adds$|^math later$|^prints and passes$) --runTestsByPath src/math.test.js") {
		t.Errorf("calls %q", calls)
	}
	got := runOutcomes(run)
	if got["src|math.test.js/math/divides"] == "failed" || got["src|math.test.js/math/nested/throws on zero"] == "passed" || got["src|math.test.js/math/adds"] != "passed" {
		t.Errorf("reported %v", got)
	}
}

func TestAVitestRunReadsItsReport(t *testing.T) {
	r := newJSRig(t, "vitest", rigOptions{})
	run := r.run(RunnerVitest, nil, nil)
	got := runOutcomes(run)
	for k, v := range map[string]string{
		"src|math.test.ts/math/adds": "passed", "src|math.test.ts/math/divides": "failed", "src|math.test.ts/math/later": "skipped",
		"src|math.test.ts/math/adds 2 and 3": "passed", "src|other.test.ts/other crashes": "failed",
	} {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	msgs := runMessages(run, "src|math.test.ts/math/divides")
	if len(msgs) != 1 || msgs[0].Message != "AssertionError: expected 5 to be 4 // Object.is equality" || msgs[0].Location == nil || msgs[0].Location.Range.Start.Line != 9 {
		t.Errorf("divides: %+v", msgs)
	}
	for _, f := range msgs[0].StackTrace {
		if strings.Contains(f.URI, "node_modules") {
			t.Errorf("a frame of node_modules: %+v", f)
		}
	}
	if !strings.HasPrefix(runOutput(run, ""), "$ vitest run src/math.test.ts src/other.test.ts\r\n") {
		t.Errorf("output %q", runOutput(run, ""))
	}
	calls := strings.Join(r.fr.called(), "\n")
	if !strings.Contains(calls, "run --reporter=default --reporter=json --outputFile.json=") || strings.Contains(calls, "--coverage") {
		t.Errorf("calls %q", calls)
	}
}

func TestANodeRunReadsTheTAPOfEachFile(t *testing.T) {
	r := newJSRig(t, "node", rigOptions{})
	run := r.run(RunnerNode, nil, nil)
	want := map[string]string{
		"test|math.test.js/math/adds": "passed", "test|math.test.js/math/divides": "failed", "test|math.test.js/math/later": "skipped",
		"test|math.test.js/math/nested/throws on zero": "passed", "test|math.test.js/prints and passes": "passed",
		"test|other.test.js/other passes": "passed", "test|other.test.js/other crashes": "failed",
	}
	got := runOutcomes(run)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("reported %v", got)
	}
	calls := r.fr.called()
	if len(calls) != 2 || !strings.HasSuffix(calls[0], "--test --test-reporter=tap --test-reporter-destination=stdout test/math.test.js") ||
		!strings.HasSuffix(calls[1], "test/other.test.js") {
		t.Errorf("calls %q", calls)
	}
	msgs := runMessages(run, "test|math.test.js/math/divides")
	if len(msgs) != 1 || msgs[0].ExpectedOutput != "4" || msgs[0].ActualOutput != "5" || !strings.Contains(msgs[0].Message, "5 !== 4") ||
		msgs[0].Location == nil || msgs[0].Location.Range.Start.Line != 10 || msgs[0].Location.Range.Start.Col != 11 {
		t.Errorf("divides: %+v", msgs)
	}
	boom := runMessages(run, "test|other.test.js/other crashes")
	if len(boom) != 1 || boom[0].Message != "boom" || boom[0].Location.Range.Start.Line != 8 {
		t.Errorf("boom: %+v", boom)
	}
	if out := runOutput(run, ""); !strings.Contains(out, "hello from a passing test") || !strings.HasPrefix(out, "$ node --test test/math.test.js\r\n") {
		t.Errorf("output %q", out)
	}
}

func TestNodeRunsChosenTestsWithNamePatterns(t *testing.T) {
	r := newJSRig(t, "node", rigOptions{})
	n := len(r.fr.called())
	run := r.run(RunnerNode, r.items(RunnerNode, "test|math.test.js/math/adds", "test|math.test.js/math/nested"), nil)
	calls := r.fr.called()[n:]
	if len(calls) != 1 || !strings.Contains(calls[0], "--test-name-pattern=^math adds$ --test-name-pattern=^math nested( |$) test/math.test.js") {
		t.Errorf("calls %q", calls)
	}
	got := runOutcomes(run)
	if got["test|math.test.js/math/adds"] != "passed" || got["test|math.test.js/math/nested/throws on zero"] != "passed" || len(got) != 2 {
		t.Errorf("reported %v", got)
	}
}

func TestAFileThatCannotBeLoadedErrorsItsTests(t *testing.T) {
	t.Run("node with tests", func(t *testing.T) {
		r := newJSRig(t, "node", rigOptions{})
		r.fr.reports["test/other.test.js"] = "node_broken.tap"
		run := r.run(RunnerNode, r.items(RunnerNode, "test|other.test.js"), nil)
		got := runOutcomes(run)
		if got["test|other.test.js/other passes"] != "errored" || got["test|other.test.js/other crashes"] != "errored" {
			t.Fatalf("reported %v", got)
		}
		msgs := runMessages(run, "test|other.test.js/other passes")
		if len(msgs) != 1 || !strings.Contains(msgs[0].Message, "SyntaxError: missing ) after argument list") {
			t.Errorf("messages %+v", msgs)
		}
	})
	t.Run("jest", func(t *testing.T) {
		r := newJSRig(t, "jest", rigOptions{})
		r.fr.reports["--runTestsByPath"] = "jest_other_broken_report.json"
		run := r.run(RunnerJest, r.items(RunnerJest, "src|other.test.js"), nil)
		got := runOutcomes(run)
		if got["src|other.test.js/other passes"] != "errored" || got["src|other.test.js/other crashes"] != "errored" {
			t.Fatalf("reported %v", got)
		}
		if msgs := runMessages(run, "src|other.test.js/other passes"); len(msgs) != 1 || !strings.Contains(msgs[0].Message, "Test suite failed to run") || !strings.Contains(msgs[0].Message, "SyntaxError") {
			t.Errorf("messages %+v", msgs)
		}
	})
}

func TestARunThatLeavesNoReportErrorsTheTestsWithTheEndOfTheOutput(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	r.fr.fail["--runTestsByPath"] = true
	run := r.run(RunnerJest, nil, nil)
	got := runOutcomes(run)
	if got["src|math.test.js/math/adds"] != "errored" || got["src|other.test.js/other crashes"] != "errored" {
		t.Fatalf("reported %v", got)
	}
	msgs := runMessages(run, "src|math.test.js/math/adds")
	if len(msgs) != 1 || !strings.Contains(msgs[0].Message, "the fake program fails") {
		t.Errorf("messages %+v", msgs)
	}
}

func TestCoverageRunsReportTheFilesOfTheRunner(t *testing.T) {
	for _, tc := range []struct {
		project, runner, file, flag string
		statements                  int
	}{
		{"jest", RunnerJest, "src/math.js", "--coverage --coverageReporters=json --coverageDirectory=", 6},
		{"vitest", RunnerVitest, "src/math.ts", "--coverage.enabled --coverage.reportOnFailure --coverage.reporter=json --coverage.reportsDirectory=", 5},
		{"node", RunnerNode, "src/math.js", "--experimental-test-coverage --test-reporter=lcov --test-reporter-destination=", 16},
	} {
		t.Run(tc.runner, func(t *testing.T) {
			r := newJSRig(t, tc.project, rigOptions{})
			run := r.runWith(tc.runner, extension.TestKindCoverage, nil, nil)
			if !strings.Contains(strings.Join(r.fr.called(), "\n"), tc.flag) {
				t.Errorf("calls %q, want %q", r.fr.called(), tc.flag)
			}
			files := runCoverage(run)
			if len(files) == 0 {
				t.Fatalf("no coverage: %v", runOutcomes(run))
			}
			for _, f := range files {
				if f.URI != fileURI(filepath.Join(r.folder, tc.file)) {
					t.Errorf("coverage of %s (only %s is not a test)", f.URI, tc.file)
				}
			}
			if files[0].Statement.Total != tc.statements {
				t.Errorf("statements %+v", files[0].Statement)
			}
			d, err := r.profile(tc.runner, extension.TestKindCoverage).LoadCoverage(context.Background(), files[0])
			if err != nil || len(d.Statements) != tc.statements {
				t.Errorf("details: %d statements, %v", len(d.Statements), err)
			}
			if out := runOutput(run, ""); !strings.Contains(out, "--coverage") && tc.runner != RunnerNode {
				t.Errorf("the output does not name the coverage: %q", out)
			}
		})
	}
}

func TestAVitestWithoutACoverageProviderRunsWithoutCoverage(t *testing.T) {
	r := newJSRig(t, "vitest", rigOptions{noTools: true})
	// the tools of the fake project, without the coverage package
	fakeTools(t, r.folder)
	if err := os.RemoveAll(filepath.Join(r.folder, "node_modules", "@vitest")); err != nil {
		t.Fatal(err)
	}
	r.settled()
	run := r.runWith(RunnerVitest, extension.TestKindCoverage, nil, nil)
	if strings.Contains(strings.Join(r.fr.called(), "\n"), "--coverage") || len(runCoverage(run)) != 0 {
		t.Errorf("calls %q", r.fr.called())
	}
	if !strings.Contains(r.note(), "no coverage package") || !strings.Contains(r.note(), "@vitest/coverage-v8") {
		t.Errorf("note %q", r.note())
	}
}

func TestAMissingToolIsToldAndNothingRuns(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	if err := os.RemoveAll(filepath.Join(r.folder, "node_modules")); err != nil {
		t.Fatal(err)
	}
	before := len(r.fr.called())
	run := r.run(RunnerJest, nil, nil)
	if len(r.fr.called()) != before || !strings.Contains(r.note(), "Jest was not found") {
		t.Errorf("calls %q, note %q", r.fr.called()[before:], r.note())
	}
	if got := runOutcomes(run); len(got) == 0 || got["src|math.test.js/math/adds"] == "passed" {
		t.Errorf("reported %v", runOutcomes(run))
	}
}

func TestAMissingNodeIsToldAndNothingRuns(t *testing.T) {
	r := newJSRig(t, "node", rigOptions{noNode: true})
	before := len(r.fr.called())
	if _, err := r.profile(RunnerNode, extension.TestKindRun).Run(context.Background(), nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if len(r.fr.called()) != before || !r.noteContaining("node was not found") {
		t.Errorf("calls %q, notes %v", r.fr.called(), r.noteList())
	}
}

func TestNothingToRunIsToldToTheUser(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	for _, it := range r.ctl(RunnerJest).Items().All() {
		r.ctl(RunnerJest).Items().Delete(it.ID())
	}
	if _, err := r.profile(RunnerJest, extension.TestKindRun).Run(context.Background(), nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if !r.noteContaining("There are no Jest tests to run.") {
		t.Errorf("notes %v", r.noteList())
	}
}

func TestACancelledRunLeavesNoProcessAndNoReport(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	r.fr.block = "--runTestsByPath"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *extension.TestRunRequest, 1)
	go func() {
		req, _ := r.profile(RunnerJest, extension.TestKindRun).Run(ctx, nil, nil, false)
		done <- req
	}()
	<-r.fr.started
	cancel()
	<-done
	run := r.ctl(RunnerJest).Runs()[0]
	if !run.Ended() {
		t.Error("the run did not end")
	}
	for k, v := range runOutcomes(run) {
		if v == "passed" || v == "failed" || v == "errored" {
			t.Errorf("%s: %s after a cancel", k, v)
		}
	}
}

func TestThePlanOfARequest(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	tc := r.ext.tests[RunnerJest]
	for _, c := range []struct {
		name             string
		include, exclude []string
		files            string
	}{
		{"everything", nil, nil, "src/math.test.js:whole src/other.test.js:whole"},
		{"a test", []string{"src|math.test.js/math/adds"}, nil, "src/math.test.js:part[1]"},
		{"a suite and a test of it", []string{"src|math.test.js/math", "src|math.test.js/math/adds"}, nil, "src/math.test.js:part[1]"},
		{"a file and one of its tests", []string{"src|math.test.js", "src|math.test.js/math/adds"}, nil, "src/math.test.js:whole"},
		{"a file less a test", []string{"src|other.test.js"}, []string{"src|other.test.js/other crashes"}, "src/other.test.js:part[1]"},
		{"an excluded file", nil, []string{"src|other.test.js"}, "src/math.test.js:whole"},
	} {
		sels := tc.plan(&extension.TestRunRequest{Include: r.items(RunnerJest, c.include...), Exclude: r.items(RunnerJest, c.exclude...)})
		var got []string
		for _, s := range sels {
			state := "whole"
			if !s.whole {
				state = "part[" + string(rune('0'+len(s.picks))) + "]"
			}
			got = append(got, s.rel+":"+state)
		}
		if strings.Join(got, " ") != c.files {
			t.Errorf("%s: %q, want %q", c.name, got, c.files)
		}
	}
}

func TestRunnerErrorTagsAreRoutes(t *testing.T) {
	r := newJSRig(t, "jest", rigOptions{})
	_, _, err := r.ext.tests[RunnerJest].toolSpec("node", t.TempDir(), "jest")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("testCtl.toolSpec.JsTestToolNotFound") {
		t.Errorf("toolSpec: %v", err)
	}
	_, err = r.ext.listWithRunner(context.Background(), jsProject{dir: t.TempDir()}, "jest", []string{"--listTests"}, nil)
	appErr, ok = errors.FromError(err)
	if !ok || !appErr.HasRoute("javascript.listWithRunner.JsTestToolNotFound") {
		t.Errorf("listWithRunner: %v", err)
	}
	_, err = r.ext.tests[RunnerJest].debugConfiguration(t.TempDir(), nil)
	appErr, ok = errors.FromError(err)
	if !ok || !appErr.HasRoute("testCtl.debugConfiguration.JsTestToolNotFound") {
		t.Errorf("debugConfiguration: %v", err)
	}
}

func TestASuiteThatFailsForItselfErrorsTheTestsBelowItThatHaveNoResult(t *testing.T) {
	r := newJSRig(t, "node", rigOptions{})
	// a file written by hand, in Node's format: the first test passed, the second was cancelled and the suite's hook failed
	r.fr.reports["test/math.test.js"] = "node_suite_error.tap"
	run := r.run(RunnerNode, r.items(RunnerNode, "test|math.test.js"), nil)
	got := runOutcomes(run)
	if got["test|math.test.js/math/adds"] != "passed" || got["test|math.test.js/math/divides"] != "errored" {
		t.Fatalf("reported %v", got)
	}
	for _, name := range []string{"test|math.test.js/math/later", "test|math.test.js/math/nested/throws on zero"} {
		msgs := runMessages(run, name)
		if got[name] != "errored" || len(msgs) != 1 || msgs[0].Message != "the after hook failed" {
			t.Errorf("%s: %q %+v", name, got[name], msgs)
		}
	}
}
