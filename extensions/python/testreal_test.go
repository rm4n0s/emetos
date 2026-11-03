//go:build emetos_pytest

package python

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The helper scripts, pytest, unittest and coverage.py for real (SPEC §10.32 "Tests", D43; step 26.6): the provider runs the helpers with
// the interpreter of the tools folder on copies of testdata/pytests and testdata/unitests. They are skipped when the interpreter, or its
// pytest or coverage.py, is not there. Python gets a temporary HOME and cache folder and never writes bytecode, so nothing lands in
// testdata.
//
//	go test -tags emetos_pytest ./extensions/python -run Real -v

// toolsPython is the interpreter of the venv that scripts/fetch-tools.sh python-tools makes (with pytest and coverage.py).
const toolsPython = "/var/tmp/emetos-tools/venv/bin/python"

// realRig activates the pack over a copy of the fixture with the interpreter of the tools folder.
func realRig(t *testing.T, fixture string, settings map[string]any, extra map[string]string) *rig {
	t.Helper()
	if st, err := os.Stat(toolsPython); err != nil || st.IsDir() {
		t.Skipf("the interpreter of the tools folder is not at %s (scripts/fetch-tools.sh python-tools)", toolsPython)
	}
	for _, m := range []string{"pytest", "coverage"} {
		if _, err := os.Stat(filepath.Join("/var/tmp/emetos-tools/venv/lib", "python3.14", "site-packages", m)); err != nil {
			t.Skipf("the venv has no %s", m)
		}
	}
	files := map[string]string{}
	root := filepath.Join("testdata", fixture)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range extra {
		files[k] = v
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PYTHONDONTWRITEBYTECODE", "1")
	t.Setenv("PYTHONHASHSEED", "0")
	all := map[string]any{DefaultInterpreterSetting: toolsPython}
	for k, v := range settings {
		all[k] = v
	}
	r := newRig(t, rigOptions{noPython: true, settings: all, files: files})
	if r.ext.tests == nil {
		t.Fatal("the pack made no test controller")
	}
	t.Cleanup(r.ext.wait)
	// the rig's HOME is a temporary folder; the interpreter is found from the setting
	return r
}

// state is the final state of an item of a run by its labels from the top.
func state(run *fakeapi.FakeTestRun, label string) string { return outcomes(run)[label] }

// stripLines removes the " @line" of a dump.
func stripLines(dump string) string {
	var out []string
	for _, l := range strings.Split(dump, "\n") {
		if i := strings.Index(l, " @"); i >= 0 {
			l = l[:i]
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func TestRealPytestDiscoversFoldersFilesClassesFunctionsAndCases(t *testing.T) {
	r := realRig(t, "pytests", map[string]any{PytestEnabledSetting: true}, map[string]string{"tests/test_syntax.py": "def (:\n"})
	r.discovered()
	got := dump(r.ctl().Items())
	want := `tests
  test_calc.py
    test_double
    test_double_wrong
    test_sign
      [3-1]
      [-3--1]
      [0-0]
      [5--1]
    test_skipped
    test_known_bug
    TestSign
      test_positive
      test_raises
  test_fixtures.py
    test_uses_broken
    test_prints`
	if !strings.HasPrefix(stripLines(got), want) {
		t.Fatalf("tree:\n%s", got)
	}
	if !strings.Contains(got, "  test_syntax.py !SyntaxError: invalid syntax\n") {
		t.Errorf("no error row for the file that does not compile:\n%s", got)
	}
	if !strings.Contains(got, "test_unit.py\n    TestInUnittest") {
		t.Errorf("unittest classes are not found by pytest:\n%s", got)
	}
	// the places
	if d := find(t, r.ctl().Items(), "tests/test_calc.py/test_double").Data(); d.Range == nil || d.Range.Start.Line != 4 {
		t.Errorf("test_double = %+v", d.Range)
	}
	if d := find(t, r.ctl().Items(), "tests/test_calc.py/TestSign/test_raises").Data(); d.Range == nil || d.Range.Start.Line < 30 {
		t.Errorf("test_raises = %+v", d.Range)
	}
}

func TestRealPytestRunsEveryKindOfTest(t *testing.T) {
	r := realRig(t, "pytests", map[string]any{PytestEnabledSetting: true}, nil)
	r.discovered()
	run := runIt(t, r, extension.TestKindRun, nil, nil)
	want := map[string]string{
		"tests/test_calc.py/test_double": "passed", "tests/test_calc.py/test_double_wrong": "failed",
		"tests/test_calc.py/test_sign/[3-1]": "passed", "tests/test_calc.py/test_sign/[-3--1]": "passed", "tests/test_calc.py/test_sign/[0-0]": "passed",
		"tests/test_calc.py/test_sign/[5--1]": "failed", "tests/test_calc.py/test_skipped": "skipped", "tests/test_calc.py/test_known_bug": "skipped",
		"tests/test_calc.py/TestSign/test_positive": "passed", "tests/test_calc.py/TestSign/test_raises": "passed",
		"tests/test_fixtures.py/test_uses_broken": "errored", "tests/test_fixtures.py/test_prints": "passed",
		"tests/test_unit.py/TestInUnittest/test_ok": "passed", "tests/test_unit.py/TestInUnittest/test_bad": "failed",
	}
	got := outcomes(run)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	// the failed assertion: message, the two sides, the place, the frame
	msgs := messagesOf(run, "tests/test_calc.py/test_double_wrong")
	if len(msgs) != 1 || !strings.Contains(msgs[0].Message, "assert 4 == 5") || msgs[0].ExpectedOutput != "5" || msgs[0].ActualOutput != "4" {
		t.Fatalf("test_double_wrong: %+v", msgs)
	}
	loc := msgs[0].Location
	if loc == nil || loc.URI != fileURI(filepath.Join(r.folder, "tests", "test_calc.py")) || loc.Range.Start.Line != 9 || loc.Range.Start.Col != 4 {
		t.Errorf("location = %+v", loc)
	}
	if len(msgs[0].StackTrace) == 0 || msgs[0].StackTrace[0].Label != "test_double_wrong" || msgs[0].StackTrace[0].Position.Line != 9 {
		t.Errorf("stack = %+v", msgs[0].StackTrace)
	}
	// a parametrized case: its own side values
	if msgs := messagesOf(run, "tests/test_calc.py/test_sign/[5--1]"); len(msgs) != 1 || msgs[0].ActualOutput != "1" || msgs[0].ExpectedOutput != "-1" {
		t.Errorf("[5--1]: %+v", msgs)
	}
	// a fixture that raises: an error at the fixture
	msgs = messagesOf(run, "tests/test_fixtures.py/test_uses_broken")
	if len(msgs) != 1 || !strings.Contains(msgs[0].Message, "the fixture failed") || msgs[0].Location == nil || msgs[0].Location.Range.Start.Line != 5 {
		t.Errorf("test_uses_broken: %+v", msgs)
	}
	// a unittest class run by pytest
	if msgs := messagesOf(run, "tests/test_unit.py/TestInUnittest/test_bad"); len(msgs) != 1 || !strings.Contains(msgs[0].Message, "2 != 3") {
		t.Errorf("test_bad: %+v", msgs)
	}
	// the output of the test, the durations, the summary of pytest
	if !strings.Contains(outputOf(run, "tests/test_fixtures.py/test_prints"), "hello from the test") {
		t.Error("the output of test_prints is missing")
	}
	if !strings.Contains(outputOf(run, ""), "passed") {
		t.Errorf("no summary of pytest:\n%s", outputOf(run, ""))
	}
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunPassed && labelOf(e.Item) == "tests/test_calc.py/test_double" && e.Duration <= 0 {
			t.Errorf("duration = %v", e.Duration)
		}
	}
}

func TestRealPytestRunsTheChosenTestsOnly(t *testing.T) {
	r := realRig(t, "pytests", map[string]any{PytestEnabledSetting: true}, nil)
	r.discovered()
	items := r.ctl().Items()
	tests := []struct {
		name    string
		include []string
		exclude []string
		want    map[string]string
	}{
		{"a function", []string{"tests/test_calc.py/test_double"}, nil, map[string]string{"tests/test_calc.py/test_double": "passed"}},
		{"a case", []string{"tests/test_calc.py/test_sign/[5--1]"}, nil, map[string]string{"tests/test_calc.py/test_sign/[5--1]": "failed"}},
		{"a class", []string{"tests/test_calc.py/TestSign"}, nil, map[string]string{"tests/test_calc.py/TestSign/test_positive": "passed", "tests/test_calc.py/TestSign/test_raises": "passed"}},
		{"a file without two cases", []string{"tests/test_fixtures.py"}, []string{"tests/test_fixtures.py/test_uses_broken"}, map[string]string{"tests/test_fixtures.py/test_prints": "passed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pick := func(paths []string) []*extension.TestItem {
				var out []*extension.TestItem
				for _, p := range paths {
					out = append(out, find(t, items, p))
				}
				return out
			}
			run := runIt(t, r, extension.TestKindRun, pick(tt.include), pick(tt.exclude))
			got := map[string]string{}
			for k, v := range outcomes(run) {
				if v != "enqueued" && v != "started" {
					got[k] = v
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("results = %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestRealPytestReportsAFileThatDoesNotCompileDuringARun(t *testing.T) {
	r := realRig(t, "pytests", map[string]any{PytestEnabledSetting: true}, map[string]string{"tests/test_syntax.py": "def (:\n"})
	r.discovered()
	run := runIt(t, r, extension.TestKindRun, []*extension.TestItem{find(t, r.ctl().Items(), "tests/test_syntax.py")}, nil)
	if state(run, "tests/test_syntax.py") != "errored" {
		t.Errorf("outcomes = %v", outcomes(run))
	}
	msgs := messagesOf(run, "tests/test_syntax.py")
	if len(msgs) != 1 || !strings.Contains(msgs[0].Message, "SyntaxError") {
		t.Errorf("messages = %+v", msgs)
	}
}

func TestRealPytestMeasuresCoverageWithCoveragePy(t *testing.T) {
	r := realRig(t, "pytests", map[string]any{PytestEnabledSetting: true}, nil)
	r.discovered()
	run := runIt(t, r, extension.TestKindCoverage, nil, nil)
	if state(run, "tests/test_calc.py/test_double") != "passed" {
		t.Fatalf("outcomes = %v\n%s", outcomes(run), outputOf(run, ""))
	}
	var calc *extension.FileCoverage
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunCoverage && e.Coverage.URI == fileURI(filepath.Join(r.folder, "calc.py")) {
			c := e.Coverage
			calc = &c
		}
	}
	if calc == nil {
		t.Fatalf("no coverage for calc.py:\n%s", outputOf(run, ""))
	}
	// ten statements, the body of unused() is the one that did not run
	if calc.Statement != (extension.CoverageCount{Covered: 9, Total: 10}) {
		t.Errorf("statements = %+v", calc.Statement)
	}
	d, err := profileOf(r, extension.TestKindCoverage).LoadCoverage(context.Background(), *calc)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range d.Statements {
		line := s.Location.Start.Line + 1
		if (line == 14) != (s.Executed == 0) {
			t.Errorf("line %d executed %d", line, s.Executed)
		}
	}
	for _, x := range d.Declarations {
		if (x.Name == "unused") != (x.Executed == 0) {
			t.Errorf("function %s executed %d", x.Name, x.Executed)
		}
	}
	// the data of coverage.py is in a temporary folder, not in the project
	for _, name := range []string{".coverage", "coverage.json"} {
		if _, err := os.Stat(filepath.Join(r.folder, name)); err == nil {
			t.Errorf("%s was left in the project", name)
		}
	}
}

func TestRealPytestCancelStopsTheRunAndItsProcesses(t *testing.T) {
	slow := "import os, time\n\n\ndef test_slow():\n    with open('pid', 'w') as f:\n        f.write(str(os.getpid()))\n    time.sleep(120)\n"
	r := realRig(t, "pytests", map[string]any{PytestEnabledSetting: true}, map[string]string{"tests/test_slow.py": slow})
	r.discovered()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = profileOf(r, extension.TestKindRun).Run(ctx, []*extension.TestItem{find(t, r.ctl().Items(), "tests/test_slow.py")}, nil, false)
	}()
	var pid int
	deadline := time.Now().Add(60 * time.Second)
	for pid == 0 && time.Now().Before(deadline) {
		if data, err := os.ReadFile(filepath.Join(r.folder, "pid")); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the slow test never started")
	}
	start := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the run did not end after the cancel")
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("the cancel took %v", time.Since(start))
	}
	gone := false
	for i := 0; i < 100 && !gone; i++ {
		gone = syscall.Kill(pid, 0) == syscall.ESRCH
		time.Sleep(50 * time.Millisecond)
	}
	if !gone {
		t.Errorf("the test process %d is still running", pid)
	}
}

func TestRealUnittestDiscoversAndRunsTheClasses(t *testing.T) {
	r := realRig(t, "unitests", map[string]any{UnittestEnabledSetting: true}, map[string]string{"tests/test_broken_import.py": "import nothing_here_at_all\n"})
	r.discovered()
	got := dump(r.ctl().Items())
	for _, s := range []string{"test_calc.py\n    TestDouble", "      test_ok", "      test_subtests", "    TestSetup\n      test_never", "test_broken_import.py !", "ModuleNotFoundError"} {
		if !strings.Contains(stripLines(got), s) {
			t.Errorf("the tree has no %q:\n%s", s, got)
		}
	}
	run := runIt(t, r, extension.TestKindRun, nil, nil)
	want := map[string]string{
		"tests/test_calc.py/TestDouble/test_ok": "passed", "tests/test_calc.py/TestDouble/test_wrong": "failed", "tests/test_calc.py/TestDouble/test_error": "errored",
		"tests/test_calc.py/TestDouble/test_skipped": "skipped", "tests/test_calc.py/TestDouble/test_subtests": "failed", "tests/test_calc.py/TestSetup": "errored",
	}
	outs := outcomes(run)
	for k, v := range want {
		if outs[k] != v {
			t.Errorf("%s = %q, want %q", k, outs[k], v)
		}
	}
	msgs := messagesOf(run, "tests/test_calc.py/TestDouble/test_wrong")
	if len(msgs) != 1 || !strings.Contains(msgs[0].Message, "4 != 5") || msgs[0].Location == nil || msgs[0].Location.Range.Start.Line != 10 || len(msgs[0].StackTrace) == 0 {
		t.Errorf("test_wrong: %+v", msgs)
	}
	if msgs := messagesOf(run, "tests/test_calc.py/TestDouble/test_error"); len(msgs) != 1 || !strings.Contains(msgs[0].Message, "ValueError: boom") {
		t.Errorf("test_error: %+v", msgs)
	}
	if msgs := messagesOf(run, "tests/test_calc.py/TestDouble/test_subtests"); len(msgs) != 1 || !strings.Contains(msgs[0].Message, "n=2") {
		t.Errorf("test_subtests: %+v", msgs)
	}
	if msgs := messagesOf(run, "tests/test_calc.py/TestSetup"); len(msgs) != 1 || !strings.Contains(msgs[0].Message, "no database") {
		t.Errorf("TestSetup: %+v", msgs)
	}
	// one test of the class by its name
	run = runIt(t, r, extension.TestKindRun, []*extension.TestItem{find(t, r.ctl().Items(), "tests/test_calc.py/TestDouble/test_ok")}, nil)
	if outs := outcomes(run); outs["tests/test_calc.py/TestDouble/test_ok"] != "passed" || outs["tests/test_calc.py/TestDouble/test_wrong"] != "" {
		t.Errorf("outcomes = %v", outs)
	}
}

func TestRealUnittestMeasuresCoverage(t *testing.T) {
	r := realRig(t, "unitests", map[string]any{UnittestEnabledSetting: true}, nil)
	r.discovered()
	run := runIt(t, r, extension.TestKindCoverage, []*extension.TestItem{find(t, r.ctl().Items(), "tests/test_calc.py/TestDouble/test_ok")}, nil)
	var calc *extension.FileCoverage
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunCoverage && e.Coverage.URI == fileURI(filepath.Join(r.folder, "calc.py")) {
			c := e.Coverage
			calc = &c
		}
	}
	if calc == nil || calc.Statement.Total != 10 || calc.Statement.Covered >= 10 || calc.Statement.Covered < 3 {
		t.Fatalf("coverage = %+v\n%s", calc, outputOf(run, ""))
	}
}
