package cpp

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

const ctestTree = `adds @9
divides @10
skipped @11
disabled @12
math.lists @13
odd[1]+x(2) @14
`

func TestDiscoveryMakesTheListOfTheTestsWithTheirPlaces(t *testing.T) {
	r := newCTestRig(t, ctestOptions{})
	if got := dump(r.ctl().Items()); got != ctestTree {
		t.Errorf("tree:\n%s\nwant:\n%s", got, ctestTree)
	}
	if got := r.called(); strings.Join(got, "|") != "--show-only=json-v1" {
		t.Errorf("ctest was called with %q", got)
	}
	d := find(t, r.ctl().Items(), "divides").Data()
	if d.URI != fileURI(filepath.Join(r.folder, "CMakeLists.txt")) {
		t.Errorf("the test is in %s", d.URI)
	}
	if got := find(t, r.ctl().Items(), "math.lists").Data().Description; got != "fast, math" {
		t.Errorf("description %q", got)
	}
}

func TestTheSuiteDelimiterGroupsTheTestsAndAChangeOfItFindsThemAgain(t *testing.T) {
	r := newCTestRig(t, ctestOptions{})
	r.api.SetSetting(SuiteDelimiterSetting, ".")
	r.api.Pump()
	r.settled()
	time.Sleep(30 * time.Millisecond)
	r.settled()
	want := "adds @9\ndivides @10\nskipped @11\ndisabled @12\nodd[1]+x(2) @14\nmath\n  lists @13\n" // an item that moved comes last
	if got := dump(r.ctl().Items()); got != want {
		t.Errorf("tree:\n%s\nwant:\n%s", got, want)
	}
	// a group in a request takes the tests below it, by their whole names
	n := len(r.called())
	r.run(r.items("math"), nil)
	if got := r.called()[n:]; strings.Join(got, "|") != `--output-junit -R ^(math\.lists)$` {
		t.Errorf("ctest was called with %q", got)
	}
	r.api.SetSetting(SuiteDelimiterSetting, "")
	r.api.Pump()
	r.settled()
	time.Sleep(30 * time.Millisecond)
	r.settled()
	if got := dump(r.ctl().Items()); got != "adds @9\ndivides @10\nskipped @11\ndisabled @12\nodd[1]+x(2) @14\nmath.lists @13\n" {
		t.Errorf("tree after the delimiter went:\n%s", got)
	}
}

func TestTheSourceLineOfAGeneratorAndTheLabelsOfATest(t *testing.T) {
	show := `{"kind":"ctestInfo","version":{"major":1,"minor":0},"backtraceGraph":{"commands":[],"files":["{{dir}}/CMakeLists.txt"],"nodes":[{"file":0,"line":3}]},
"tests":[{"name":"Calc.Adds","backtrace":0,"command":["{{dir}}/build/adds"],"properties":[{"name":"DEF_SOURCE_LINE","value":"{{dir}}/src/adds.c:5"},{"name":"LABELS","value":"unit"}]},
{"name":"Calc.Nowhere","command":["x"],"properties":[]}]}`
	r := newCTestRig(t, ctestOptions{fixtures: map[string]string{"show.json": show}, settings: map[string]any{SuiteDelimiterSetting: "."}})
	if got := dump(r.ctl().Items()); got != "Calc\n  Adds @5\n  Nowhere\n" {
		t.Errorf("tree:\n%s", got)
	}
	d := find(t, r.ctl().Items(), "Calc/Adds").Data()
	if d.URI != fileURI(filepath.Join(r.folder, "src/adds.c")) || d.Description != "unit" {
		t.Errorf("item %+v", d)
	}
	if u := find(t, r.ctl().Items(), "Calc/Nowhere").Data().URI; u != "" {
		t.Errorf("a test with no place is in %q", u)
	}
}

func TestAFolderWithoutABuildFolderHasNoTestsQuietlyAndRefreshTellsTheUser(t *testing.T) {
	r := newCTestRig(t, ctestOptions{noBuild: true})
	if got := dump(r.ctl().Items()); got != "" || len(r.called()) != 0 || len(r.api.Notes()) != 0 {
		t.Errorf("tree %q, calls %q, notes %v", got, r.called(), r.api.Notes())
	}
	if err := r.ctl().Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.note(); !strings.Contains(got, "not a configured CMake build folder") {
		t.Errorf("note %q", got)
	}
}

func TestWithoutCTestTheTestsAreNotFoundQuietlyAndRefreshTellsTheUser(t *testing.T) {
	r := newCTestRig(t, ctestOptions{noCTest: true})
	if len(r.api.Notes()) != 0 || dump(r.ctl().Items()) != "" {
		t.Errorf("notes %v", r.api.Notes())
	}
	if err := r.ctl().Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.note(); !strings.Contains(got, "ctest was not found") {
		t.Errorf("note %q", got)
	}
	_, err := findCTest()
	wantRoute(t, err, "findCTest.CppCTestNotFound")
}

func TestAListThatCannotBeReadKeepsTheTreeAndRefreshTellsTheUser(t *testing.T) {
	r := newCTestRig(t, ctestOptions{})
	// ctest now answers with something that is not a list
	writeFile(t, r.bin, "ctest", "#!/bin/sh\necho 'Test project: nothing to see'\nexit 1\n", 0o755)
	if err := r.ctl().Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.note(); !strings.Contains(got, "ctest printed no JSON") || !strings.Contains(got, "Test project") {
		t.Errorf("note %q", got)
	}
	if got := dump(r.ctl().Items()); got != ctestTree {
		t.Errorf("the tree changed:\n%s", got)
	}
}

func TestTheBuildFolderIsTheSettingAgainstTheWorkspaceFolder(t *testing.T) {
	for _, tc := range []struct{ setting, folder, want string }{
		{"", "/w", "/w/build"},
		{"build", "/w", "/w/build"},
		{"out/debug", "/w", "/w/out/debug"},
		{"${workspaceFolder}/build-x", "/w", "/w/build-x"},
		{"/opt/b", "/w", "/opt/b"},
		{"  ", "/w", "/w/build"},
		{"../b/", "/w/x", "/w/b"},
	} {
		if got := resolveBuildDirectory(tc.setting, tc.folder); got != tc.want {
			t.Errorf("%q in %q = %q, want %q", tc.setting, tc.folder, got, tc.want)
		}
	}
	// a project configured into another folder: the tests are found there, and a change of the setting finds them again
	r := newCTestRig(t, ctestOptions{noBuild: true, settings: map[string]any{BuildDirectorySetting: "${workspaceFolder}/nothing"}})
	if dump(r.ctl().Items()) != "" {
		t.Fatal("tests without a build folder")
	}
	writeFile(t, r.folder, "out/CTestTestfile.cmake", "#\n", 0o644)
	r.api.SetSetting(BuildDirectorySetting, "${workspaceFolder}/out")
	r.api.Pump()
	r.settled()
	time.Sleep(30 * time.Millisecond)
	r.settled()
	if got := dump(r.ctl().Items()); got != ctestTree {
		t.Errorf("tree:\n%s", got)
	}
}

func TestSavesAndFileChangesFindTheTestsAgain(t *testing.T) {
	r := newCTestRig(t, ctestOptions{})
	save := func(path string) {
		r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: fileURI(path)})
		r.settled()
		time.Sleep(30 * time.Millisecond)
		r.settled()
	}
	if len(r.called()) == 0 {
		t.Fatal("the tests were not found when the pack started")
	}
	change := func(kind, path string) {
		r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: kind, Path: path}}})
		time.Sleep(30 * time.Millisecond)
		r.settled()
	}
	for _, tc := range []struct {
		name string
		do   func()
		more bool
	}{
		{"CMakeLists.txt is saved", func() { save(filepath.Join(r.folder, "CMakeLists.txt")) }, true},
		{"a .cmake file is saved", func() { save(filepath.Join(r.folder, "cmake/tests.cmake")) }, true},
		{"a source file is saved", func() { save(filepath.Join(r.folder, "src/adds.c")) }, false},
		{"a file outside the folder is saved", func() { save("/elsewhere/CMakeLists.txt") }, false},
		{"CTestTestfile.cmake is created", func() { change(extension.FileCreated, filepath.Join(r.folder, "build/CTestTestfile.cmake")) }, true},
		{"a CMakeLists.txt is deleted", func() { change(extension.FileDeleted, filepath.Join(r.folder, "src/CMakeLists.txt")) }, true},
		{"a text file is created", func() { change(extension.FileCreated, "notes.txt") }, false},
		{"a file is changed", func() { change(extension.FileChanged, filepath.Join(r.folder, "CMakeLists.txt")) }, false},
	} {
		before := len(r.called())
		tc.do()
		if got := len(r.called()) > before; got != tc.more {
			t.Errorf("%s: found again = %v, want %v", tc.name, got, tc.more)
		}
	}
}

func TestARunReportsEveryTestWithItsMessagesAndOutput(t *testing.T) {
	r := newCTestRig(t, ctestOptions{})
	run := r.run(nil, nil)
	want := map[string]string{"adds": "passed", "divides": "failed", "skipped": "skipped", "disabled": "skipped", "math.lists": "passed", "odd[1]+x(2)": "passed"}
	got := outcomes(run)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	msgs := messagesOf(run, "divides")
	if len(msgs) != 1 || !strings.HasPrefix(msgs[0].Message, "The test failed.\n\n") || !strings.Contains(msgs[0].Message, "expected 3 but got 2") {
		t.Errorf("divides: %+v", msgs)
	}
	if out := outputOf(run, "adds"); out != "adding 40 and 2\r\n" {
		t.Errorf("the output of adds: %q", out)
	}
	if out := outputOf(run, "skipped"); !strings.Contains(out, "no display here") {
		t.Errorf("the output of skipped: %q", out)
	}
	if out := outputOf(run, ""); !strings.Contains(out, "$ ctest --output-junit ") || !strings.Contains(out, "Test #2: divides") || strings.Contains(out, "\n\n") {
		t.Errorf("the output of the run: %q", out)
	}
	if n := len(r.called()); n != 2 {
		t.Errorf("%d calls", n)
	}
	// the tests start as ctest says so, and the ones that passed are passed before the file is read
	var kinds []string
	for _, e := range run.Events() {
		if e.Item != nil && labelOf(e.Item) == "adds" && e.Kind != fakeapi.RunOutput {
			kinds = append(kinds, string(e.Kind))
		}
	}
	if strings.Join(kinds, ",") != "enqueued,started,passed" {
		t.Errorf("the states of adds: %v", kinds)
	}
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunPassed && labelOf(e.Item) == "adds" && e.Duration < 0 {
			t.Errorf("no duration for adds: %v", e.Duration)
		}
	}
}

func TestAFailureThatNamesALineOfTheSourcesIsPlacedThere(t *testing.T) {
	junit := `<?xml version="1.0"?><testsuite name="x" tests="1"><testcase name="divides" time="0.5" status="fail"><failure message="Failed"/>
<system-out>[ RUN      ] Calc.Divides
src/divides.cpp:9: Failure
Expected equality of these values
/nowhere/else.cpp:3: ignored
</system-out></testcase></testsuite>`
	r := newCTestRig(t, ctestOptions{cases: []fakeCase{listCase, {args: `--output-junit -R ^(divides)$`, junit: "junit_x.xml", code: 8}},
		fixtures: map[string]string{"junit_x.xml": junit}})
	run := r.run(r.items("divides"), nil)
	msgs := messagesOf(run, "divides")
	if len(msgs) != 1 || msgs[0].Location == nil {
		t.Fatalf("messages %+v", msgs)
	}
	loc := msgs[0].Location
	if loc.URI != fileURI(filepath.Join(r.folder, "src/divides.cpp")) || loc.Range.Start.Line != 8 || loc.Range.Start.Col != 0 || loc.Range.End.Col != len("\tint got = divide(a, b);") {
		t.Errorf("location %+v", loc)
	}
}

func TestTheTestsThatCannotRunTimeOutOrCrashAreReported(t *testing.T) {
	names := []string{"missing", "slow", "crash"}
	var tests []string
	for _, n := range names {
		tests = append(tests, `{"name":"`+n+`","command":["x"],"properties":[]}`)
	}
	show := `{"kind":"ctestInfo","version":{"major":1,"minor":0},"backtraceGraph":{"commands":[],"files":[],"nodes":[]},"tests":[` + strings.Join(tests, ",") + `]}`
	r := newCTestRig(t, ctestOptions{
		cases:    []fakeCase{listCase, {args: "--output-junit", stdout: "run_two.txt", junit: "junit_errors.xml", code: 8}},
		fixtures: map[string]string{"show.json": show},
	})
	run := r.run(nil, nil)
	got := outcomes(run)
	if got["missing"] != "errored" || got["slow"] != "failed" || got["crash"] != "failed" {
		t.Errorf("outcomes %v", got)
	}
	for name, want := range map[string]string{"missing": "CTest did not run the test: Unable to find executable.", "slow": "The test timed out.", "crash": "The test failed: SEGFAULT."} {
		msgs := messagesOf(run, name)
		if len(msgs) != 1 || !strings.HasPrefix(msgs[0].Message, want) {
			t.Errorf("%s: %+v", name, msgs)
		}
	}
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunFailed && labelOf(e.Item) == "slow" && e.Duration < time.Second {
			t.Errorf("the duration of slow is %v", e.Duration)
		}
	}
}

func TestRequestsBecomeCommandLines(t *testing.T) {
	for _, tc := range []struct {
		name    string
		include []string
		exclude []string
		want    string
	}{
		{"everything", nil, nil, `--output-junit`},
		{"one test", []string{"adds"}, nil, `--output-junit -R ^(adds)$`},
		{"two tests in the order of the request", []string{"divides", "adds"}, nil, `--output-junit -R ^(divides|adds)$`},
		{"a name with the characters of a regular expression", []string{"odd[1]+x(2)"}, nil, `--output-junit -R ^(odd\[1\]\+x\(2\))$`},
		{"a name with a dot", []string{"math.lists"}, nil, `--output-junit -R ^(math\.lists)$`},
		{"everything but two", nil, []string{"skipped", "disabled"}, `--output-junit -R ^(adds|divides|math\.lists|odd\[1\]\+x\(2\))$`},
		{"a test twice", []string{"adds", "adds"}, nil, `--output-junit -R ^(adds)$`},
		{"a test that is also excluded", []string{"adds", "divides"}, []string{"adds"}, `--output-junit -R ^(divides)$`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newCTestRig(t, ctestOptions{})
			n := len(r.called())
			r.run(r.items(tc.include...), r.items(tc.exclude...))
			if got := r.called()[n:]; strings.Join(got, "\n") != tc.want {
				t.Errorf("ctest was called with\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestEscapingMakesANameThatOnlyItselfMatches(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"adds", "adds"}, {"a.b", `a\.b`}, {"a[1]", `a\[1\]`}, {"x+y*z?", `x\+y\*z\?`}, {"(a|b)", `\(a\|b\)`}, {"^a$", `\^a\$`},
		{`back\slash`, `back\\slash`}, {"Suite/Case.Test/0", `Suite/Case\.Test/0`}, {"ünï", "ünï"}, {"", ""},
	} {
		if got := escapeRegex(tc.name); got != tc.want {
			t.Errorf("escapeRegex(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestOnlyTheTestsAskedForAreReportedAndTheLiveStatesComeFirst(t *testing.T) {
	// ctest answers the request of one test with the report of all of them
	r := newCTestRig(t, ctestOptions{cases: []fakeCase{listCase, {args: `--output-junit -R ^(adds)$`, stdout: "run_all.txt", junit: "junit_all.xml", code: 8}}})
	run := r.run(r.items("adds"), nil)
	got := outcomes(run)
	if len(got) != 1 || got["adds"] != "passed" {
		t.Errorf("reported %v", got)
	}
	if out := outputOf(run, "divides"); out != "" {
		t.Errorf("output of a test that was not asked for: %q", out)
	}
}

func TestARunOfTheOddNameAndOfTwoTests(t *testing.T) {
	r := newCTestRig(t, ctestOptions{})
	got := outcomes(r.run(r.items("odd[1]+x(2)"), nil))
	if len(got) != 1 || got["odd[1]+x(2)"] != "passed" {
		t.Errorf("reported %v", got)
	}
	got = outcomes(r.run(r.items("adds", "divides"), nil))
	if len(got) != 2 || got["adds"] != "passed" || got["divides"] != "failed" {
		t.Errorf("reported %v", got)
	}
}

func TestACtestThatWritesNoReportErrorsTheTestsWithItsOutput(t *testing.T) {
	// the case is missing, so the fake says "unexpected ctest" and exits with 2
	r := newCTestRig(t, ctestOptions{cases: []fakeCase{listCase}})
	run := r.run(r.items("adds", "divides"), nil)
	got := outcomes(run)
	if got["adds"] != "errored" || got["divides"] != "errored" {
		t.Fatalf("outcomes %v", got)
	}
	msgs := messagesOf(run, "adds")
	if len(msgs) != 1 || !strings.Contains(msgs[0].Message, "unexpected ctest") {
		t.Errorf("messages %+v", msgs)
	}
}

func TestACorruptReportErrorsTheTests(t *testing.T) {
	r := newCTestRig(t, ctestOptions{
		cases:    []fakeCase{listCase, {args: `--output-junit -R ^(adds)$`, junit: "junit_bad.xml"}},
		fixtures: map[string]string{"junit_bad.xml": "<testsuite><testcase"},
	})
	run := r.run(r.items("adds"), nil)
	if got := outcomes(run); got["adds"] != "errored" {
		t.Errorf("outcomes %v", got)
	}
}

func TestACancelStopsCTestAndItsChildren(t *testing.T) {
	r := newCTestRig(t, ctestOptions{cases: []fakeCase{listCase, {args: "--output-junit", sleep: 60}}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *fakeapi.FakeTestRun, 1)
	go func() {
		before := len(r.ctl().Runs())
		_, _ = r.profile(extension.TestKindRun).Run(ctx, nil, nil, false)
		done <- r.ctl().Runs()[before]
	}()
	pidFile := filepath.Join(filepath.Dir(r.folder), "pid")
	// the fake writes the pid file atomically; wait for a whole pid anyway
	pid := 0
	r.api.WaitFor("ctest to start", func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil || !strings.HasSuffix(string(data), "\n") {
			return false
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(data)))
		pid = n
		return err == nil && n > 0
	})
	cancel()
	select {
	case run := <-done:
		if !run.Ended() {
			t.Error("the run did not end")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the run did not end after the cancel")
	}
	r.api.WaitFor("the program to be gone", func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH })
}

func TestARequestForNothingTellsTheUserAndNoCTestIsToldWhenRunning(t *testing.T) {
	r := newCTestRig(t, ctestOptions{})
	all := r.items("adds", "divides", "skipped", "disabled", "math.lists", "odd[1]+x(2)")
	if _, err := r.profile(extension.TestKindRun).Run(context.Background(), nil, all, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.note(), "no CTest tests to run") {
		t.Errorf("note %q", r.note())
	}
	if err := os.Remove(filepath.Join(r.bin, "ctest")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.profile(extension.TestKindRun).Run(context.Background(), r.items("adds"), nil, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.note(), "ctest was not found") {
		t.Errorf("note %q", r.note())
	}
}

func TestAWindowWithoutTheTestingViewHasNoController(t *testing.T) {
	r := newCTestRig(t, ctestOptions{noTesting: true})
	if r.ext.tests != nil || r.api.TestController(TestControllerID) != nil {
		t.Error("the pack made a controller")
	}
}

func TestTheProfilesAreRunAndDebugWithNoCoverageAndNoContinuousRun(t *testing.T) {
	r := newCTestRig(t, ctestOptions{})
	var got []string
	for _, p := range r.ctl().Profiles() {
		got = append(got, p.Label()+"/"+strconv.Itoa(int(p.Kind()))+"/"+strconv.FormatBool(p.IsDefault())+"/"+strconv.FormatBool(p.SupportsContinuousRun()))
	}
	want := []string{"ctest/" + strconv.Itoa(int(extension.TestKindRun)) + "/true/false", "ctest (debug)/" + strconv.Itoa(int(extension.TestKindDebug)) + "/true/false"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("profiles %v, want %v", got, want)
	}
}

func TestResolveAndRefreshFindTheTestsAgain(t *testing.T) {
	r := newCTestRig(t, ctestOptions{})
	n := len(r.called())
	if err := r.ctl().Resolve(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := len(r.called()); got != n*2 {
		t.Errorf("%d calls after Resolve, want %d", got, n*2)
	}
	if err := r.ctl().Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(r.called()); got != n*3 {
		t.Errorf("%d calls after Refresh, want %d", got, n*3)
	}
	if got := dump(r.ctl().Items()); got != ctestTree {
		t.Errorf("tree:\n%s", got)
	}
}

func TestATestThatGoesFromTheListGoesFromTheTree(t *testing.T) {
	r := newCTestRig(t, ctestOptions{})
	r.fixtureShow(`{"kind":"ctestInfo","version":{"major":1,"minor":0},"backtraceGraph":{"commands":[],"files":[],"nodes":[]},"tests":[{"name":"adds","command":["x"],"properties":[]}]}`)
	if err := r.ctl().Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := dump(r.ctl().Items()); got != "adds\n" {
		t.Errorf("tree:\n%s", got)
	}
}

// fixtureShow changes what the fake ctest answers to --show-only (the script reads the fixture each time).
func (r *ctestRig) fixtureShow(text string) {
	r.t.Helper()
	writeFile(r.t, os.Getenv("FIX"), "show.json", text, 0o644)
}
