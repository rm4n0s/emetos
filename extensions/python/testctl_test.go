package python

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The tree of the tests (step 26.6): folders, files, classes, functions and parametrized cases found by the helper scripts. The runs of
// the helpers are faked here: a fake runner prints the recorded lines of testdata/events (the files the helpers wrote for the format of
// helpers/emetos_pytest.py and helpers/emetos_unittest.py); the real helpers run under the tag emetos_pytest.

// fakeRunner records the programs the provider would run and answers them.
type fakeRunner struct {
	mu     sync.Mutex
	calls  []cmdSpec
	answer func(spec cmdSpec) ([]string, error)
}

// run implements testRunner.
func (f *fakeRunner) run(ctx context.Context, spec cmdSpec, line func(string)) error {
	f.mu.Lock()
	f.calls = append(f.calls, spec)
	f.mu.Unlock()
	lines, err := f.answer(spec)
	for _, l := range lines {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if line != nil {
			line(l)
		}
	}
	return err
}

// called lists the programs run so far.
func (f *fakeRunner) called() []cmdSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]cmdSpec(nil), f.calls...)
}

// withArgs are the programs run whose arguments, joined by spaces, contain the text.
func (f *fakeRunner) withArgs(text string) []cmdSpec {
	var out []cmdSpec
	for _, c := range f.called() {
		if strings.Contains(strings.Join(c.Args, " "), text) {
			out = append(out, c)
		}
	}
	return out
}

// recorded reads a file of testdata/events with {{dir}} replaced by the folder, as lines.
func recorded(t *testing.T, name, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "events", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(strings.ReplaceAll(string(data), "{{dir}}", dir), "\n"), "\n")
}

// failure is an error for a fake program that fails.
func failure() error { return errors.New("FakeProgramFailed", "the fake program failed") }

// answers makes the fake answer like the interpreter of a project: pytest and coverage.py are there or not, and the helpers print the
// recorded events.
func answers(t *testing.T, hasPytest, hasCoverage bool) func(spec cmdSpec) ([]string, error) {
	return func(spec cmdSpec) ([]string, error) {
		args := strings.Join(spec.Args, " ")
		switch {
		case args == "-c import pytest":
			if hasPytest {
				return nil, nil
			}
			return nil, failure()
		case args == "-c import coverage":
			if hasCoverage {
				return nil, nil
			}
			return nil, failure()
		case strings.Contains(args, "coverage json"):
			data, err := os.ReadFile(filepath.Join("testdata", "events", "cov_report.json"))
			if err != nil {
				t.Error(err)
				return nil, err
			}
			for i, a := range spec.Args {
				if a == "-o" {
					if err := os.WriteFile(spec.Args[i+1], []byte(strings.ReplaceAll(string(data), "/abs", spec.Dir)), 0o600); err != nil {
						t.Error(err)
					}
				}
			}
			return nil, nil
		case strings.Contains(args, "--collect-only"):
			return recorded(t, "pytest_discovery.jsonl", spec.Dir), nil
		case strings.Contains(args, "emetos_unittest.py discover"):
			return recorded(t, "unittest_discovery.jsonl", spec.Dir), nil
		case strings.Contains(args, "emetos_unittest.py run"):
			return recorded(t, "unittest_run.jsonl", spec.Dir), nil
		case strings.Contains(args, "-m pytest"):
			return recorded(t, "pytest_run.jsonl", spec.Dir), nil
		}
		t.Errorf("unexpected program: %s", args)
		return nil, failure()
	}
}

// testFolder is the files of a folder with tests.
var testFolder = map[string]string{
	"tests/test_calc.py": "import pytest\nfrom calc import double\n\n\ndef test_double():\n    assert double(2) == 4\n\n\ndef test_double_wrong():\n    assert double(2) == 5\n",
	"calc.py":            "def double(x):\n    return x * 2\n",
}

// testRig activates the pack over the folder with a fake runner and waits for the first discovery when a framework is enabled.
func testRig(t *testing.T, settings map[string]any, files map[string]string, fr *fakeRunner) *rig {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	r := newRig(t, rigOptions{settings: settings, files: files, runner: fr.run})
	if r.ext.tests == nil {
		t.Fatal("the pack made no test controller")
	}
	t.Cleanup(r.ext.wait)
	return r
}

// pytestRig is a rig with pytest enabled.
func pytestRig(t *testing.T, extra map[string]any) (*rig, *fakeRunner) {
	t.Helper()
	fr := &fakeRunner{}
	fr.answer = answers(t, true, true)
	settings := map[string]any{PytestEnabledSetting: true}
	for k, v := range extra {
		settings[k] = v
	}
	r := testRig(t, settings, testFolder, fr)
	r.discovered()
	return r, fr
}

// unittestRig is a rig with unittest enabled.
func unittestRig(t *testing.T, extra map[string]any) (*rig, *fakeRunner) {
	t.Helper()
	fr := &fakeRunner{}
	fr.answer = answers(t, true, true)
	settings := map[string]any{UnittestEnabledSetting: true}
	for k, v := range extra {
		settings[k] = v
	}
	r := testRig(t, settings, map[string]string{"tests/test_calc.py": "x = 1\n", "calc.py": ""}, fr)
	r.discovered()
	return r, fr
}

// ctl is the controller the pack made.
func (r *rig) ctl() *fakeapi.FakeTestController {
	r.t.Helper()
	c := r.api.TestController(TestControllerID)
	if c == nil {
		r.t.Fatal("the pack made no test controller")
	}
	return c
}

// discovered waits for the tests to be found.
func (r *rig) discovered() {
	r.t.Helper()
	r.api.WaitFor("the discovery of the tests", func() bool { return r.ext.tests.idle() })
}

// dump writes the items as indented lines: the label, "@line" (from 1) for an item with a place and "!text" for one with an error.
func dump(c *extension.TestItemCollection) string {
	var sb strings.Builder
	var walk func(items []*extension.TestItem, depth int)
	walk = func(items []*extension.TestItem, depth int) {
		for _, it := range items {
			d := it.Data()
			sb.WriteString(strings.Repeat("  ", depth) + d.Label)
			if d.Range != nil {
				sb.WriteString(" @" + strconv.Itoa(d.Range.Start.Line+1))
			}
			if d.Error != "" {
				sb.WriteString(" !" + d.Error)
			}
			sb.WriteString("\n")
			walk(it.Children().All(), depth+1)
		}
	}
	walk(c.All(), 0)
	return sb.String()
}

// find finds an item by the labels from the top, joined by slashes (no label has a slash).
func find(t *testing.T, c *extension.TestItemCollection, path string) *extension.TestItem {
	t.Helper()
	cur := c
	var it *extension.TestItem
	for _, label := range strings.Split(path, "/") {
		it = nil
		for _, x := range cur.All() {
			if x.Data().Label == label {
				it = x
				break
			}
		}
		if it == nil {
			t.Fatalf("no item %q (at %q) in\n%s", path, label, dump(c))
		}
		cur = it.Children()
	}
	return it
}

const pytestTree = `tests
  test_calc.py
    test_double @5
    test_double_wrong @9
    test_sign @14
      [3-1] @14
      [-3--1] @14
    TestSign @29
      test_positive @30
      test_raises @33
  test_fixtures.py
    test_uses_broken @9
    test_prints @13
  test_syntax.py !SyntaxError: invalid syntax
pkg
  sub
    test_deep.py
      test_deep @1
`

func TestDiscoveryMakesTheTreeOfFoldersFilesClassesFunctionsAndCases(t *testing.T) {
	r, fr := pytestRig(t, nil)
	if got := dump(r.ctl().Items()); got != pytestTree {
		t.Errorf("tree:\n%s\nwant:\n%s", got, pytestTree)
	}
	calls := fr.withArgs("--collect-only")
	if len(calls) != 1 {
		t.Fatalf("%d discoveries", len(calls))
	}
	c := calls[0]
	if c.Program == "" || c.Dir != r.folder || !strings.HasPrefix(strings.Join(c.Args, " "), "-m pytest -p emetos_pytest ") ||
		!strings.HasSuffix(strings.Join(c.Args, " "), "--collect-only -q --continue-on-collection-errors") {
		t.Errorf("discovery: %+v", c)
	}
	// the helper is on PYTHONPATH, in the cache folder
	var pp string
	for _, e := range c.Env {
		if v, ok := strings.CutPrefix(e, "PYTHONPATH="); ok {
			pp = v
		}
	}
	if !strings.Contains(pp, filepath.Join("emetos", "python-tests")) {
		t.Errorf("PYTHONPATH = %q", pp)
	}
	if _, err := os.Stat(filepath.Join(strings.Split(pp, string(os.PathListSeparator))[0], "emetos_pytest.py")); err != nil {
		t.Errorf("the plugin was not written: %v", err)
	}
}

func TestDiscoveryOfUnittestMakesClassesOfModules(t *testing.T) {
	r, fr := unittestRig(t, nil)
	want := "tests\n  test_calc.py\n    TestDouble @5\n      test_ok @6\n      test_wrong @9\n    TestSetup @24\n      test_never @30\n" +
		"  test_broken_import.py !ModuleNotFoundError: No module named 'nothing'\n"
	if got := dump(r.ctl().Items()); got != want {
		t.Errorf("tree:\n%s\nwant:\n%s", got, want)
	}
	calls := fr.withArgs("emetos_unittest.py discover")
	if len(calls) != 1 || strings.Join(calls[0].Args[1:], " ") != "discover -s . -p *test*.py" {
		t.Errorf("discovery: %+v", calls)
	}
}

func TestTheFrameworkIsChosenBySettingsAndByWhatTheInterpreterHas(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		pytest   bool
		want     string
	}{
		{"pytest enabled", map[string]any{PytestEnabledSetting: true}, false, frameworkPytest},
		{"unittest enabled", map[string]any{UnittestEnabledSetting: true}, true, frameworkUnittest},
		{"both enabled: pytest wins", map[string]any{PytestEnabledSetting: true, UnittestEnabledSetting: true}, false, frameworkPytest},
		{"neither, the interpreter has pytest", map[string]any{}, true, frameworkPytest},
		{"neither, no pytest", map[string]any{}, false, frameworkUnittest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fr := &fakeRunner{}
			fr.answer = answers(t, tt.pytest, true)
			r := testRig(t, tt.settings, testFolder, fr)
			// nothing is found until the view asks, unless a framework is enabled
			if _, enabled := tt.settings[PytestEnabledSetting]; !enabled && tt.settings[UnittestEnabledSetting] == nil {
				r.discovered()
				if n := len(fr.withArgs("discover")) + len(fr.withArgs("--collect-only")); n != 0 {
					t.Fatalf("%d discoveries before the view asked", n)
				}
				if err := r.ctl().Resolve(context.Background(), nil); err != nil {
					t.Fatal(err)
				}
			}
			r.discovered()
			got := frameworkUnittest
			if len(fr.withArgs("--collect-only")) > 0 {
				got = frameworkPytest
			}
			if got != tt.want {
				t.Errorf("framework = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestTheSettingsDecideWhatIsRun(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		want     string // the arguments of the discovery after the plugin
		cwd      func(r *rig) string
	}{
		{"pytest arguments", map[string]any{PytestEnabledSetting: true, PytestArgsSetting: []any{"-k", "double", "-x"}}, "-k double -x --collect-only -q --continue-on-collection-errors",
			func(r *rig) string { return r.folder }},
		{"cwd relative to the folder", map[string]any{PytestEnabledSetting: true, TestCwdSetting: "tests"}, "--collect-only -q --continue-on-collection-errors",
			func(r *rig) string { return filepath.Join(r.folder, "tests") }},
		{"cwd with the variable", map[string]any{PytestEnabledSetting: true, TestCwdSetting: "${workspaceFolder}/tests"}, "--collect-only -q --continue-on-collection-errors",
			func(r *rig) string { return filepath.Join(r.folder, "tests") }},
		{"unittest arguments", map[string]any{UnittestEnabledSetting: true, UnittestArgsSetting: []any{"-v", "-s", "tests", "-p", "check_*.py", "-t", "."}}, "discover -s tests -p check_*.py -t .",
			func(r *rig) string { return r.folder }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fr := &fakeRunner{}
			fr.answer = answers(t, true, true)
			r := testRig(t, tt.settings, testFolder, fr)
			r.discovered()
			calls := fr.called()
			if len(calls) == 0 {
				t.Fatal("nothing was run")
			}
			c := calls[0]
			args := strings.Join(c.Args, " ")
			if !strings.HasSuffix(args, tt.want) {
				t.Errorf("args = %q, want the end %q", args, tt.want)
			}
			if c.Dir != tt.cwd(r) {
				t.Errorf("dir = %q, want %q", c.Dir, tt.cwd(r))
			}
		})
	}
}

func TestUnittestDiscoveryArguments(t *testing.T) {
	tests := []struct {
		args               []string
		start, pattern, up string
	}{
		{nil, ".", "test*.py", ""},
		{[]string{"-v", "-s", ".", "-p", "*test*.py"}, ".", "*test*.py", ""},
		{[]string{"-s", "src", "-t", "."}, "src", "test*.py", "."},
		{[]string{"-ssrc", "-pcheck*.py"}, "src", "check*.py", ""},
		{[]string{"--start-directory=a/b", "--pattern=x*.py", "--top-level-directory", "a"}, "a/b", "x*.py", "a"},
		{[]string{"-s"}, ".", "test*.py", ""},
	}
	for _, tt := range tests {
		s, p, u := unittestDiscovery(tt.args)
		if s != tt.start || p != tt.pattern || u != tt.up {
			t.Errorf("%v = %q %q %q, want %q %q %q", tt.args, s, p, u, tt.start, tt.pattern, tt.up)
		}
	}
}

func TestPytestArgumentsThatNameFilesAreLeftOutOfARunOfChosenTests(t *testing.T) {
	exists := func(p string) bool { return p == "tests" || p == "tests/test_a.py" || p == "." }
	tests := []struct {
		args, want []string
	}{
		{[]string{"tests", "-x"}, []string{"-x"}},
		{[]string{"-k", "tests", "-x"}, []string{"-k", "tests", "-x"}}, // the value of -k is not a path
		{[]string{"tests/test_a.py::test_b", "-v"}, []string{"-v"}},
		{[]string{"-m", "slow", ".", "--tb=short"}, []string{"-m", "slow", "--tb=short"}},
		{[]string{"other.py"}, []string{"other.py"}}, // not a file that is there
		{nil, nil},
	}
	for _, tt := range tests {
		got := withoutTestPaths(tt.args, exists)
		if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
			t.Errorf("%v = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestResolveAndRefreshFindTheTestsAgain(t *testing.T) {
	r, fr := pytestRig(t, nil)
	before := len(fr.withArgs("--collect-only"))
	if err := r.ctl().Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.ctl().Resolve(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := len(fr.withArgs("--collect-only")); got != before+2 {
		t.Errorf("%d discoveries, want %d", got, before+2)
	}
}

func TestADiscoveryThatFindsNothingIsToldByRefreshOnly(t *testing.T) {
	fr := &fakeRunner{answer: func(spec cmdSpec) ([]string, error) {
		return []string{"ERROR: usage: pytest [options]", "pytest: error: unrecognized arguments: --nonsense"}, failure()
	}}
	r := testRig(t, map[string]any{PytestEnabledSetting: true, PytestArgsSetting: []any{"--nonsense"}}, testFolder, fr)
	r.discovered()
	if notes := r.notes(); len(notes) != 0 {
		t.Errorf("a quiet discovery told the user: %q", notes)
	}
	if err := r.ctl().Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	notes := r.notes()
	if len(notes) != 1 || !strings.Contains(notes[0], "cannot be found with pytest") || !strings.Contains(notes[0], "unrecognized arguments") {
		t.Errorf("notes = %q", notes)
	}
}

func TestChangesFindTheTestsAgain(t *testing.T) {
	saved := func(r *rig, rel string) {
		r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: fileURI(filepath.Join(r.folder, rel))})
	}
	changed := func(kind, rel string) func(r *rig) {
		return func(r *rig) {
			r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: kind, Path: rel}}})
		}
	}
	tests := []struct {
		name     string
		settings map[string]any
		do       func(r *rig)
		again    bool
	}{
		{"a saved Python file", nil, func(r *rig) { saved(r, "tests/test_calc.py") }, true},
		{"a saved file that is not Python", nil, func(r *rig) { saved(r, "README.md") }, false},
		{"a saved file outside the folder", nil, func(r *rig) {
			r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: "file:///elsewhere/test_x.py"})
		}, false},
		{"saving turned off", map[string]any{AutoDiscoverSetting: false}, func(r *rig) { saved(r, "tests/test_calc.py") }, false},
		{"a pattern that matches", map[string]any{AutoDiscoverPatternSetting: "tests/**/*.py"}, func(r *rig) { saved(r, "tests/test_calc.py") }, true},
		{"a pattern that does not", map[string]any{AutoDiscoverPatternSetting: "src/**/*.py"}, func(r *rig) { saved(r, "tests/test_calc.py") }, false},
		{"a created Python file", nil, changed(extension.FileCreated, "tests/test_new.py"), true},
		{"a deleted Python file", nil, changed(extension.FileDeleted, "tests/test_calc.py"), true},
		{"a renamed Python file", nil, func(r *rig) {
			r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileRenamed, Path: "a.txt", OldPath: "a.py"}}})
		}, true},
		{"a rescan", nil, changed(extension.FileRescan, "."), true},
		{"a changed Python file (a save tells)", nil, changed(extension.FileChanged, "tests/test_calc.py"), false},
		{"a created file that is not Python", nil, changed(extension.FileCreated, "notes.txt"), false},
		{"another pytest argument", nil, func(r *rig) { r.api.SetSetting(PytestArgsSetting, []any{"-x"}) }, true},
		{"the same settings again", nil, func(r *rig) { r.api.SetSetting(PytestEnabledSetting, true) }, false},
		{"a setting that is not about the tests", nil, func(r *rig) { r.api.SetSetting("python.languageServer", "none") }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, fr := pytestRig(t, tt.settings)
			before := len(fr.withArgs("--collect-only"))
			tt.do(r)
			r.discovered()
			got := len(fr.withArgs("--collect-only")) - before
			if (got > 0) != tt.again || got > 1 {
				t.Errorf("%d new discoveries, again = %v", got, tt.again)
			}
		})
	}
}

func TestSavesCloseTogetherMakeOneDiscovery(t *testing.T) {
	r, fr := pytestRig(t, nil)
	before := len(fr.withArgs("--collect-only"))
	r.ext.testDebounce = 100 * time.Millisecond // as a burst of saves is
	for i := 0; i < 5; i++ {
		r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: fileURI(filepath.Join(r.folder, "tests/test_calc.py"))})
	}
	r.discovered()
	if got := len(fr.withArgs("--collect-only")) - before; got != 1 {
		t.Errorf("%d discoveries for five saves", got)
	}
}

func TestADiscoveryKeepsTheItemsThatAreStillThereAndDropsTheOthers(t *testing.T) {
	r, fr := pytestRig(t, nil)
	double := find(t, r.ctl().Items(), "tests/test_calc.py/test_double")
	fr.answer = func(spec cmdSpec) ([]string, error) {
		if !strings.Contains(strings.Join(spec.Args, " "), "--collect-only") {
			return answers(t, true, true)(spec)
		}
		return []string{`@@EMETOS@@ {"type":"discovery","rootdir":"` + spec.Dir + `","tests":[{"id":"tests/test_calc.py::test_double","file":"` + spec.Dir + `/tests/test_calc.py","line":7,"classes":[],"func":"test_double","param":""}]}`}, nil
	}
	if err := r.ctl().Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := dump(r.ctl().Items()), "tests\n  test_calc.py\n    test_double @7\n"; got != want {
		t.Errorf("tree:\n%s\nwant:\n%s", got, want)
	}
	if find(t, r.ctl().Items(), "tests/test_calc.py/test_double") != double {
		t.Error("the item of a test that is still there was made again")
	}
}

func TestAWindowWithoutTheTestingViewHasNoController(t *testing.T) {
	fr := &fakeRunner{answer: answers(t, true, true)}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	r := newRigWith(t, rigOptions{settings: map[string]any{PytestEnabledSetting: true}, runner: fr.run}, func(a *fakeapi.API) { a.SetTestingUnavailable(true) })
	if r.ext.tests != nil || r.api.TestController(TestControllerID) != nil {
		t.Error("a controller was made")
	}
	r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: fileURI(filepath.Join(r.folder, "a.py"))})
	if len(fr.called()) != 0 {
		t.Errorf("programs were run: %v", fr.called())
	}
}

func TestTheProfilesAreRunDebugAndCoverage(t *testing.T) {
	r, _ := pytestRig(t, nil)
	var got []string
	for _, p := range r.ctl().Profiles() {
		got = append(got, p.Label()+":"+strconv.Itoa(int(p.Kind()))+":"+strconv.FormatBool(p.IsDefault())+":"+strconv.FormatBool(p.SupportsContinuousRun()))
	}
	want := "Run Tests:1:true:true Debug Tests:2:true:false Run Tests with Coverage:3:true:false"
	if strings.Join(got, " ") != want {
		t.Errorf("profiles = %v", got)
	}
	// a change of a Python file of the folder runs a continuous run again
	run := r.ctl().Profiles()[0]
	item := find(t, r.ctl().Items(), "tests/test_calc.py")
	for uri, want := range map[string]bool{fileURI(filepath.Join(r.folder, "calc.py")): true, fileURI(filepath.Join(r.folder, "notes.txt")): false, "file:///elsewhere/x.py": false} {
		if got, err := run.Triggers(item, uri); err != nil || got != want {
			t.Errorf("trigger %s = %v, %v", uri, got, err)
		}
	}
}

func TestParseEventTable(t *testing.T) {
	tests := []struct {
		name, line, text, typ, tag string
	}{
		{"plain output", "collected 3 items", "collected 3 items", "", ""},
		{"an event", `@@EMETOS@@ {"type":"start","id":"a::b"}`, "", "start", ""},
		{"output before the event", `.F@@EMETOS@@ {"type":"done"}`, ".F", "done", ""},
		{"not json", "@@EMETOS@@ nonsense", "", "", "PythonTestEventInvalid"},
		{"no type", `@@EMETOS@@ {"id":"a"}`, "", "", "PythonTestEventInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ev, ok, err := parseEvent(tt.line)
			if tt.tag != "" {
				appErr, isErr := errors.FromError(err)
				if !isErr || !appErr.HasRoute("parseEvent."+tt.tag) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil || text != tt.text || ev.Type != tt.typ || ok != (tt.typ != "") {
				t.Errorf("= %q %+v %v %v", text, ev, ok, err)
			}
		})
	}
}

func TestGlobMatchTable(t *testing.T) {
	tests := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.py", "a.py", true},
		{"**/*.py", "a/b/c.py", true},
		{"**/*.py", "a/b/c.txt", false},
		{"tests/**/*.py", "tests/a.py", true},
		{"tests/**/*.py", "src/a.py", false},
		{"*.py", "a/b.py", false},
		{"a/?.py", "a/b.py", true},
	}
	for _, tt := range tests {
		if got := globMatch(tt.pattern, tt.name); got != tt.want {
			t.Errorf("globMatch(%q, %q) = %v", tt.pattern, tt.name, got)
		}
	}
}

func TestTheHelpersAreWrittenToTheCacheOnceAndAgainWhenTheyChange(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	dir, err := helperDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dir, filepath.Join(cache, "emetos", "python-tests")) {
		t.Errorf("dir = %s", dir)
	}
	for _, name := range []string{"emetos_pytest.py", "emetos_unittest.py"} {
		want, _ := helperFiles.ReadFile("helpers/" + name)
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != string(want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// a file that was damaged is written again, in the same folder
	if err := os.WriteFile(filepath.Join(dir, "emetos_pytest.py"), []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := helperDir()
	if err != nil || again != dir {
		t.Fatalf("again = %s, %v", again, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "emetos_pytest.py")); string(got) == "damaged" {
		t.Error("the damaged helper was not written again")
	}
}

func TestHelperDirFailsWhenTheCacheFolderCannotBeMade(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", file) // a file, so nothing can be made below it
	_, err := helperDir()
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("helperDir.PythonTestHelperWriteFailed") {
		t.Fatalf("error = %v", err)
	}
}

func TestTheInterpreterIsRequiredToFindOrRunTests(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fr := &fakeRunner{answer: answers(t, true, true)}
	r := newRig(t, rigOptions{noPython: true, settings: map[string]any{PytestEnabledSetting: true}, runner: fr.run})
	_, err := r.ext.testInterpreter(true)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("python.testInterpreter.PythonTestNoInterpreter") {
		t.Fatalf("error = %v", err)
	}
	if notes := r.notes(); len(notes) != 1 || !strings.Contains(notes[0], "No Python interpreter") {
		t.Errorf("notes = %q", notes)
	}
	r.discovered()
	if len(fr.called()) != 0 {
		t.Errorf("programs were run: %v", fr.called())
	}
}
