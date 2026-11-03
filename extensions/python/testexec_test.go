package python

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The runs of the controller (step 26.6) over the fake runner: the requests turned into command lines, the recorded events turned into
// states, messages and output, coverage, cancel and Debug.

// profileOf is the profile of the kind.
func profileOf(r *rig, kind extension.TestRunKind) *fakeapi.FakeTestProfile {
	r.t.Helper()
	for _, p := range r.ctl().Profiles() {
		if p.Kind() == kind {
			return p
		}
	}
	r.t.Fatalf("no profile of kind %d", kind)
	return nil
}

// runIt runs the include with the profile of the kind and returns the run the handler made.
func runIt(t *testing.T, r *rig, kind extension.TestRunKind, include, exclude []*extension.TestItem) *fakeapi.FakeTestRun {
	t.Helper()
	before := len(r.ctl().Runs())
	if _, err := profileOf(r, kind).Run(context.Background(), include, exclude, false); err != nil {
		t.Fatal(err)
	}
	runs := r.ctl().Runs()
	if len(runs) != before+1 {
		t.Fatalf("%d runs, want %d", len(runs), before+1)
	}
	return runs[before]
}

// labelOf names an item for the tests: its labels from the top joined by slashes.
func labelOf(it *extension.TestItem) string {
	var parts []string
	for x := it; x != nil; x = x.Parent() {
		parts = append(parts, x.Data().Label)
	}
	slices.Reverse(parts)
	return strings.Join(parts, "/")
}

// outcomes are the last state of each item the run reported, by the labels from the top.
func outcomes(run *fakeapi.FakeTestRun) map[string]string {
	got := map[string]string{}
	for _, e := range run.Events() {
		switch e.Kind {
		case fakeapi.RunEnqueued, fakeapi.RunStarted, fakeapi.RunPassed, fakeapi.RunFailed, fakeapi.RunErrored, fakeapi.RunSkipped:
			got[labelOf(e.Item)] = string(e.Kind)
		}
	}
	return got
}

// messagesOf are the messages of the failure of the item.
func messagesOf(run *fakeapi.FakeTestRun, label string) []extension.TestMessage {
	var msgs []extension.TestMessage
	for _, e := range run.Events() {
		if e.Item != nil && labelOf(e.Item) == label && (e.Kind == fakeapi.RunFailed || e.Kind == fakeapi.RunErrored) {
			msgs = e.Messages
		}
	}
	return msgs
}

// outputOf is the text of the output of the run, or only of the item.
func outputOf(run *fakeapi.FakeTestRun, item string) string {
	var sb strings.Builder
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunOutput && (item == "" || (e.Item != nil && labelOf(e.Item) == item)) {
			sb.WriteString(e.Text)
		}
	}
	return sb.String()
}

// runArgs are the arguments of the last run of the framework (not a discovery or a question).
func runArgs(t *testing.T, fr *fakeRunner, dir string) string {
	t.Helper()
	var last []string
	for _, c := range fr.called() {
		a := strings.Join(c.Args, " ")
		if (strings.Contains(a, "-m pytest") && !strings.Contains(a, "--collect-only")) || strings.Contains(a, "emetos_unittest.py run") {
			last = c.Args
		}
	}
	if last == nil {
		t.Fatal("no run was made")
	}
	return strings.NewReplacer(dir, "$D").Replace(strings.Join(last, " "))
}

func TestARunReportsEveryTestWithItsMessages(t *testing.T) {
	r, fr := pytestRig(t, nil)
	run := runIt(t, r, extension.TestKindRun, nil, nil)
	if !run.Ended() || run.Name() != "pytest" || !run.Persist() {
		t.Errorf("ended %v name %q persist %v", run.Ended(), run.Name(), run.Persist())
	}
	want := map[string]string{
		"tests/test_calc.py/test_double": "passed", "tests/test_calc.py/test_double_wrong": "failed",
		"tests/test_calc.py/test_sign/[3-1]": "passed", "tests/test_calc.py/test_sign/[-3--1]": "failed",
		"tests/test_calc.py/TestSign/test_positive": "skipped", "tests/test_fixtures.py/test_uses_broken": "errored",
		"tests/test_fixtures.py/test_prints": "passed",
		// not in the discovery: added when the run reports it
		"tests/test_calc.py/TestSign/test_new/[x y]": "passed",
		// a file that could not be collected
		"tests/test_syntax.py": "errored",
	}
	got := outcomes(run)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	// the unreported tests of the discovery stay queued: the host marks them skipped when the run ends
	if got["pkg/sub/test_deep.py/test_deep"] != "enqueued" {
		t.Errorf("test_deep = %q", got["pkg/sub/test_deep.py/test_deep"])
	}
	// a failure: the message, the sides of the comparison, the place (the code of the line) and the frames
	msgs := messagesOf(run, "tests/test_calc.py/test_double_wrong")
	if len(msgs) != 1 || msgs[0].Message != "assert 4 == 5\n +  where 4 = double(2)" || msgs[0].ExpectedOutput != "5" || msgs[0].ActualOutput != "4" {
		t.Fatalf("test_double_wrong: %+v", msgs)
	}
	loc := msgs[0].Location
	if loc == nil || loc.URI != fileURI(filepath.Join(r.folder, "tests/test_calc.py")) || loc.Range.Start.Line != 9 || loc.Range.Start.Col != 4 || loc.Range.End.Col != len("    assert double(2) == 5") {
		t.Errorf("location = %+v", loc)
	}
	if len(msgs[0].StackTrace) != 1 || msgs[0].StackTrace[0].Label != "test_double_wrong" || msgs[0].StackTrace[0].Position.Line != 9 {
		t.Errorf("stack = %+v", msgs[0].StackTrace)
	}
	// a fixture that failed
	if msgs := messagesOf(run, "tests/test_fixtures.py/test_uses_broken"); len(msgs) != 1 || msgs[0].Message != "RuntimeError: the fixture failed" {
		t.Errorf("test_uses_broken: %+v", msgs)
	}
	// an import error: the message at the file
	if msgs := messagesOf(run, "tests/test_syntax.py"); len(msgs) != 1 || msgs[0].Message != "SyntaxError: invalid syntax" {
		t.Errorf("test_syntax: %+v", msgs)
	}
	// the output: lines that are not events, the text before an event, the test's own output
	out := outputOf(run, "")
	for _, s := range []string{"$ python3 -m pytest -p emetos_pytest\r\n", "= test session starts =", ".\r\n", "short test summary info"} {
		if !strings.Contains(out, s) {
			t.Errorf("the output has no %q:\n%s", s, out)
		}
	}
	if strings.Contains(out, "@@EMETOS@@ {") {
		t.Errorf("an event is in the output:\n%s", out)
	}
	if !strings.Contains(outputOf(run, "tests/test_fixtures.py/test_prints"), "hello from the test\r\n") {
		t.Error("the output of the test is missing")
	}
	// durations
	var d int64
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunPassed && labelOf(e.Item) == "tests/test_calc.py/test_double" {
			d = e.Duration.Microseconds()
		}
	}
	if d != 1200 {
		t.Errorf("duration = %d us", d)
	}
	_ = fr
}

func TestARequestBecomesTheCommandLine(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		include  []string
		exclude  []string
		want     string
	}{
		{"everything", nil, nil, nil, "-m pytest -p emetos_pytest"},
		{"everything with the arguments of the settings", map[string]any{PytestArgsSetting: []any{"-x", "tests"}}, nil, nil, "-m pytest -p emetos_pytest -x tests"},
		{"a function", nil, []string{"tests/test_calc.py/test_double"}, nil, "-m pytest -p emetos_pytest $D/tests/test_calc.py::test_double"},
		{"a case", nil, []string{"tests/test_calc.py/test_sign/[3-1]"}, nil, "-m pytest -p emetos_pytest $D/tests/test_calc.py::test_sign[3-1]"},
		{"all the cases of a function", nil, []string{"tests/test_calc.py/test_sign"}, nil, "-m pytest -p emetos_pytest $D/tests/test_calc.py::test_sign"},
		{"a method", nil, []string{"tests/test_calc.py/TestSign/test_raises"}, nil, "-m pytest -p emetos_pytest $D/tests/test_calc.py::TestSign::test_raises"},
		{"a class", nil, []string{"tests/test_calc.py/TestSign"}, nil, "-m pytest -p emetos_pytest $D/tests/test_calc.py::TestSign"},
		{"a file", nil, []string{"tests/test_calc.py"}, nil, "-m pytest -p emetos_pytest $D/tests/test_calc.py"},
		{"a folder, the arguments that name it left out", map[string]any{PytestArgsSetting: []any{"tests", "-x"}}, []string{"tests"}, nil, "-m pytest -p emetos_pytest -x $D/tests"},
		{"a file and a test in it: the file", nil, []string{"tests/test_calc.py", "tests/test_calc.py/test_double"}, nil, "-m pytest -p emetos_pytest $D/tests/test_calc.py"},
		{"two tests", nil, []string{"tests/test_calc.py/test_double", "tests/test_fixtures.py/test_prints"}, nil,
			"-m pytest -p emetos_pytest $D/tests/test_calc.py::test_double $D/tests/test_fixtures.py::test_prints"},
		{"a function without one of its cases", nil, []string{"tests/test_calc.py/test_sign"}, []string{"tests/test_calc.py/test_sign/[3-1]"}, "-m pytest -p emetos_pytest $D/tests/test_calc.py::test_sign[-3--1]"},
		{"a file without a test", nil, []string{"tests/test_fixtures.py"}, []string{"tests/test_fixtures.py/test_prints"}, "-m pytest -p emetos_pytest $D/tests/test_fixtures.py::test_uses_broken"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, fr := pytestRig(t, tt.settings)
			pick := func(paths []string) []*extension.TestItem {
				var out []*extension.TestItem
				for _, p := range paths {
					out = append(out, find(t, r.ctl().Items(), p))
				}
				return out
			}
			runIt(t, r, extension.TestKindRun, pick(tt.include), pick(tt.exclude))
			if got := runArgs(t, fr, r.folder); got != tt.want {
				t.Errorf("args = %q\nwant   %q", got, tt.want)
			}
		})
	}
}

func TestARequestForUnittestBecomesNamesOfModulesClassesAndTests(t *testing.T) {
	tests := []struct {
		name    string
		include []string
		exclude []string
		want    string
	}{
		{"everything", nil, nil, "emetos_unittest.py run -s . -p *test*.py"},
		{"a method", []string{"tests/test_calc.py/TestDouble/test_ok"}, nil, "run -s . -p *test*.py tests.test_calc.TestDouble.test_ok"},
		{"a class", []string{"tests/test_calc.py/TestDouble"}, nil, "run -s . -p *test*.py tests.test_calc.TestDouble"},
		{"a file", []string{"tests/test_calc.py"}, nil, "run -s . -p *test*.py tests.test_calc"},
		{"a folder: its files", []string{"tests"}, nil, "run -s . -p *test*.py tests.test_calc"},
		{"a class without a method", []string{"tests/test_calc.py/TestDouble"}, []string{"tests/test_calc.py/TestDouble/test_wrong"}, "run -s . -p *test*.py tests.test_calc.TestDouble.test_ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, fr := unittestRig(t, nil)
			pick := func(paths []string) []*extension.TestItem {
				var out []*extension.TestItem
				for _, p := range paths {
					out = append(out, find(t, r.ctl().Items(), p))
				}
				return out
			}
			run := runIt(t, r, extension.TestKindRun, pick(tt.include), pick(tt.exclude))
			if run.Name() != "unittest" {
				t.Errorf("name = %q", run.Name())
			}
			if got := runArgs(t, fr, r.folder); !strings.HasSuffix(got, tt.want) {
				t.Errorf("args = %q\nwant the end %q", got, tt.want)
			}
		})
	}
}

func TestAUnittestRunReportsTheFailedClassFixtureOnTheClass(t *testing.T) {
	r, _ := unittestRig(t, nil)
	run := runIt(t, r, extension.TestKindRun, nil, nil)
	got := outcomes(run)
	if got["tests/test_calc.py/TestDouble/test_ok"] != "passed" || got["tests/test_calc.py/TestDouble/test_wrong"] != "failed" {
		t.Errorf("outcomes = %v", got)
	}
	if got["tests/test_calc.py/TestSetup"] != "errored" {
		t.Errorf("TestSetup = %q", got["tests/test_calc.py/TestSetup"])
	}
	if msgs := messagesOf(run, "tests/test_calc.py/TestSetup"); len(msgs) != 1 || msgs[0].Message != "RuntimeError: no database" {
		t.Errorf("TestSetup: %+v", msgs)
	}
	if msgs := messagesOf(run, "tests/test_calc.py/TestDouble/test_wrong"); len(msgs) != 1 || msgs[0].Message != "AssertionError: 4 != 5" {
		t.Errorf("test_wrong: %+v", msgs)
	}
}

func TestACoverageRunWrapsTheCommandInCoveragePyAndReportsTheFiles(t *testing.T) {
	r, fr := pytestRig(t, nil)
	run := runIt(t, r, extension.TestKindCoverage, []*extension.TestItem{find(t, r.ctl().Items(), "tests")}, nil)
	args := runArgs(t, fr, r.folder)
	want := "-m coverage run --source=$D --omit=*/site-packages/*,*/dist-packages/* -m pytest -p emetos_pytest $D/tests"
	if args != want {
		t.Errorf("args = %q, want %q", args, want)
	}
	var cmd cmdSpec
	for _, c := range fr.withArgs("coverage run") {
		cmd = c
	}
	if !slices.ContainsFunc(cmd.Env, func(e string) bool { return strings.HasPrefix(e, "COVERAGE_FILE=") }) {
		t.Error("the data file of coverage.py was not set")
	}
	if js := fr.withArgs("coverage json"); len(js) != 1 || js[0].Dir != r.folder {
		t.Errorf("coverage json: %+v", js)
	}
	var files []extension.FileCoverage
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunCoverage {
			files = append(files, e.Coverage)
		}
	}
	if len(files) != 2 || files[1].URI != fileURI(filepath.Join(r.folder, "calc.py")) || files[1].Statement != (extension.CoverageCount{Covered: 5, Total: 9}) {
		t.Fatalf("coverage = %+v", files)
	}
	// the details come back through the profile
	d, err := profileOf(r, extension.TestKindCoverage).LoadCoverage(context.Background(), files[1])
	if err != nil || len(d.Statements) != 9 {
		t.Fatalf("details = %+v, %v", d, err)
	}
}

func TestACoverageRunLeavesTheChoiceOfFilesToTheProjectsOwnConfiguration(t *testing.T) {
	for name, files := range map[string]map[string]string{
		".coveragerc":    {".coveragerc": "[run]\nbranch = True\n"},
		"pyproject.toml": {"pyproject.toml": "[tool.coverage.run]\nbranch = true\n"},
		"setup.cfg":      {"setup.cfg": "[coverage:run]\nbranch = True\n"},
	} {
		t.Run(name, func(t *testing.T) {
			fr := &fakeRunner{answer: answers(t, true, true)}
			all := map[string]string{"tests/test_calc.py": "x = 1\n"}
			for k, v := range files {
				all[k] = v
			}
			r := testRig(t, map[string]any{PytestEnabledSetting: true}, all, fr)
			r.discovered()
			runIt(t, r, extension.TestKindCoverage, nil, nil)
			if got := runArgs(t, fr, r.folder); got != "-m coverage run -m pytest -p emetos_pytest" {
				t.Errorf("args = %q", got)
			}
		})
	}
}

func TestACoverageRunWithoutCoveragePyRunsThePlainTestsAndTellsTheUser(t *testing.T) {
	fr := &fakeRunner{answer: answers(t, true, false)}
	r := testRig(t, map[string]any{PytestEnabledSetting: true}, testFolder, fr)
	r.discovered()
	run := runIt(t, r, extension.TestKindCoverage, nil, nil)
	if got := runArgs(t, fr, r.folder); got != "-m pytest -p emetos_pytest" {
		t.Errorf("args = %q", got)
	}
	r.api.Pump()
	notes := r.notes()
	if len(notes) != 1 || !strings.Contains(notes[0], "coverage.py is not installed") || !strings.Contains(notes[0], "pip install coverage") {
		t.Errorf("notes = %q", notes)
	}
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunCoverage {
			t.Error("coverage was reported")
		}
	}
}

func TestAReportThatCannotBeMadeIsToldToTheUser(t *testing.T) {
	fr := &fakeRunner{}
	base := answers(t, true, true)
	fr.answer = func(spec cmdSpec) ([]string, error) {
		if strings.Contains(strings.Join(spec.Args, " "), "coverage json") {
			return []string{"No data to report."}, failure()
		}
		return base(spec)
	}
	r := testRig(t, map[string]any{PytestEnabledSetting: true}, testFolder, fr)
	r.discovered()
	runIt(t, r, extension.TestKindCoverage, nil, nil)
	r.api.Pump()
	if notes := r.notes(); len(notes) != 1 || !strings.Contains(notes[0], "No data to report.") {
		t.Errorf("notes = %q", notes)
	}
}

func TestAProgramThatFailsWithoutEventsIsToldToTheUser(t *testing.T) {
	fr := &fakeRunner{}
	base := answers(t, true, true)
	fr.answer = func(spec cmdSpec) ([]string, error) {
		a := strings.Join(spec.Args, " ")
		if strings.Contains(a, "-m pytest") && !strings.Contains(a, "--collect-only") {
			return []string{"/usr/bin/python3: No module named pytest"}, failure()
		}
		return base(spec)
	}
	r := testRig(t, map[string]any{PytestEnabledSetting: true}, testFolder, fr)
	r.discovered()
	runIt(t, r, extension.TestKindRun, nil, nil)
	r.api.Pump()
	if notes := r.notes(); len(notes) != 1 || notes[0] != "The Python tests failed: /usr/bin/python3: No module named pytest" {
		t.Errorf("notes = %q", notes)
	}
}

func TestACancelStopsTheRun(t *testing.T) {
	fr := &fakeRunner{}
	base := answers(t, true, true)
	fr.answer = func(spec cmdSpec) ([]string, error) {
		a := strings.Join(spec.Args, " ")
		if strings.Contains(a, "-m pytest") && !strings.Contains(a, "--collect-only") {
			return nil, nil
		}
		return base(spec)
	}
	r := testRig(t, map[string]any{PytestEnabledSetting: true}, testFolder, fr)
	r.discovered()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := profileOf(r, extension.TestKindRun).Run(ctx, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	runs := r.ctl().Runs()
	if len(runs) != 1 || !runs[0].Ended() {
		t.Fatalf("runs = %d", len(runs))
	}
	r.api.Pump()
	if notes := r.notes(); len(notes) != 0 {
		t.Errorf("a cancelled run told the user: %q", notes)
	}
}

func TestTheRunCommandsStartRunsOfTheirOwn(t *testing.T) {
	r, fr := pytestRig(t, nil)
	item := find(t, r.ctl().Items(), "tests/test_calc.py/test_double")
	r.ext.tests.start(extension.TestKindRun, []*extension.TestItem{item})
	r.ext.closers.Wait()
	if got := runArgs(t, fr, r.folder); !strings.HasSuffix(got, "test_calc.py::test_double") {
		t.Errorf("args = %q", got)
	}
	runs := r.ctl().Runs()
	if len(runs) != 1 || !runs[0].Ended() {
		t.Fatalf("runs = %d", len(runs))
	}
	if r.ext.tests.last == nil || r.ext.tests.last.kind != extension.TestKindRun {
		t.Error("the last run is not kept")
	}
}

func TestDebugStartsTheDebuggerOnTheSelectedTests(t *testing.T) {
	tests := []struct {
		name    string
		unit    bool
		args    []any
		include []string
		want    []any
		module  string
	}{
		{"pytest, one function", false, nil, []string{"tests/test_calc.py/test_double"}, []any{"$D/tests/test_calc.py::test_double"}, "pytest"},
		{"pytest, everything: the arguments of the settings", false, []any{"-x", "tests"}, nil, []any{"-x", "tests"}, "pytest"},
		{"pytest, a file: the arguments that name files go", false, []any{"-x", "tests"}, []string{"tests/test_calc.py"}, []any{"-x", "$D/tests/test_calc.py"}, "pytest"},
		{"unittest, a class", true, nil, []string{"tests/test_calc.py/TestDouble"}, []any{"tests.test_calc.TestDouble"}, "unittest"},
		{"unittest, everything", true, nil, nil, []any{"discover", "-s", ".", "-p", "*test*.py"}, "unittest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r *rig
			var fr *fakeRunner
			extra := map[string]any{}
			if tt.unit {
				r, fr = unittestRig(t, extra)
			} else {
				if tt.args != nil {
					extra[PytestArgsSetting] = tt.args
				}
				r, fr = pytestRig(t, extra)
			}
			var include []*extension.TestItem
			for _, p := range tt.include {
				include = append(include, find(t, r.ctl().Items(), p))
			}
			if _, err := profileOf(r, extension.TestKindDebug).Run(context.Background(), include, nil, false); err != nil {
				t.Fatal(err)
			}
			r.api.Pump()
			r.ext.closers.Wait()
			starts := r.api.DebugStarts()
			if len(starts) != 1 {
				t.Fatalf("%d debug starts", len(starts))
			}
			cfg := starts[0].Target.Config
			if cfg["type"] != DebugType || cfg["request"] != "launch" || cfg["module"] != tt.module || cfg["cwd"] != r.folder || cfg["console"] != "internalConsole" {
				t.Errorf("config = %v", cfg)
			}
			if p, _ := cfg["purpose"].([]any); len(p) != 1 || p[0] != "debug-test" {
				t.Errorf("purpose = %v", cfg["purpose"])
			}
			var want []any
			for _, a := range tt.want {
				want = append(want, strings.ReplaceAll(a.(string), "$D", r.folder))
			}
			if got, _ := cfg["args"].([]any); !slices.Equal(got, want) {
				t.Errorf("args = %v, want %v", got, want)
			}
			// the debugger makes the results: no run was made
			if len(r.ctl().Runs()) != 0 {
				t.Error("a run was made")
			}
			_ = fr
		})
	}
}

func TestDebugOfNothingTellsTheUser(t *testing.T) {
	r, _ := pytestRig(t, nil)
	item := find(t, r.ctl().Items(), "tests/test_fixtures.py")
	rest := item.Children().All()
	if _, err := profileOf(r, extension.TestKindDebug).Run(context.Background(), []*extension.TestItem{item}, rest, false); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	if notes := r.notes(); len(notes) != 1 || notes[0] != "There are no Python tests to debug." {
		t.Errorf("notes = %q", notes)
	}
	if len(r.api.DebugStarts()) != 0 {
		t.Error("a session was started")
	}
}

func TestARunWithoutTestsTellsTheUser(t *testing.T) {
	r, _ := pytestRig(t, nil)
	item := find(t, r.ctl().Items(), "tests/test_fixtures.py")
	if _, err := profileOf(r, extension.TestKindRun).Run(context.Background(), []*extension.TestItem{item}, item.Children().All(), false); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	if notes := r.notes(); len(notes) != 1 || notes[0] != "There are no Python tests to run." {
		t.Errorf("notes = %q", notes)
	}
}

func TestTheEnvironmentOfARunHasTheHelpersAndTheEnvFile(t *testing.T) {
	fr := &fakeRunner{answer: answers(t, true, true)}
	files := map[string]string{"tests/test_calc.py": "x = 1\n", ".env": "FROM_ENV_FILE=yes\n"}
	t.Setenv("PYTHONPATH", "/existing")
	r := testRig(t, map[string]any{PytestEnabledSetting: true}, files, fr)
	r.discovered()
	runIt(t, r, extension.TestKindRun, nil, nil)
	var env []string
	for _, c := range fr.called() {
		if strings.Contains(strings.Join(c.Args, " "), "-m pytest") && !strings.Contains(strings.Join(c.Args, " "), "--collect-only") {
			env = c.Env
		}
	}
	has := func(prefix string) string {
		for _, e := range slices.Backward(env) {
			if v, ok := strings.CutPrefix(e, prefix); ok {
				return v
			}
		}
		return ""
	}
	if has("FROM_ENV_FILE=") != "yes" || has("PYTHONUNBUFFERED=") != "1" || has("PYTHONIOENCODING=") != "utf-8" || !strings.HasSuffix(has("PYTHONPATH="), string(os.PathListSeparator)+"/existing") {
		t.Errorf("env = %v", env)
	}
}

func TestRunProcessRunsInItsOwnGroupAndTagsItsFailures(t *testing.T) {
	var lines []string
	sh, err := lookSh()
	if err != nil {
		t.Skip("no sh")
	}
	err = runProcess(context.Background(), cmdSpec{Program: sh, Args: []string{"-c", "echo one; echo two >&2"}, Env: os.Environ()}, func(l string) { lines = append(lines, l) })
	if err != nil || len(lines) != 2 {
		t.Errorf("= %v, %v", lines, err)
	}
	err = runProcess(context.Background(), cmdSpec{Program: sh, Args: []string{"-c", "exit 3"}, Env: os.Environ()}, nil)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("runProcess.PythonProgramFailed") {
		t.Errorf("exit 3: %v", err)
	}
	err = runProcess(context.Background(), cmdSpec{Program: filepath.Join(t.TempDir(), "missing")}, nil)
	appErr, ok = errors.FromError(err)
	if !ok || !appErr.HasRoute("runProcess.PythonProgramStartFailed") {
		t.Errorf("missing: %v", err)
	}
}

func TestRunProcessKillsTheGroupWhenTheContextEnds(t *testing.T) {
	sh, err := lookSh()
	if err != nil {
		t.Skip("no sh")
	}
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runProcess(ctx, cmdSpec{Program: sh, Args: []string{"-c", "echo started; sleep 60"}, Env: os.Environ()}, func(string) {
			select {
			case got <- struct{}{}:
			default:
			}
		})
	}()
	<-got
	cancel()
	if err := <-done; err == nil {
		t.Error("a killed program did not fail")
	}
}

func lookSh() (string, error) {
	for _, p := range []string{"/bin/sh", "/usr/bin/sh"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("NoSh", "no sh")
}

func TestTheDebugConfigurationOfATestRunPassesTheProvidersOfTheDebugger(t *testing.T) {
	r, _ := pytestRig(t, nil)
	item := find(t, r.ctl().Items(), "tests/test_calc.py/test_double")
	if _, err := profileOf(r, extension.TestKindDebug).Run(context.Background(), []*extension.TestItem{item}, nil, false); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	r.ext.closers.Wait()
	cfg := r.api.DebugStarts()[0].Target.Config
	resolved, err := r.ext.resolveDebug(context.Background(), r.folder, cfg)
	if err != nil || resolved == nil {
		t.Fatalf("resolveDebug = %v, %v", resolved, err)
	}
	if py, _ := resolved["python"].(string); py == "" || resolved["module"] != "pytest" || resolved["purpose"] == nil {
		t.Errorf("resolved = %v", resolved)
	}
	resolved, err = r.ext.resolveDebugSubstituted(context.Background(), r.folder, resolved)
	if err != nil || resolved == nil || resolved["cwd"] != r.folder {
		t.Errorf("resolveDebugSubstituted = %v, %v", resolved, err)
	}
}
