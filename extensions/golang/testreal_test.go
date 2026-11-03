package golang

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The runs of the controller with the real go (step 26.5) on the packages of testdata/gotests: pass, fail, skip, subtests, a build
// error, a panic, coverage and a cancel. The go program is the one the run had in PATH before the tests isolated it; the build cache is
// a temporary folder shared by the tests of the run, the module cache is not used (the modules need none) and nothing is downloaded.
// The tests are skipped when go is not installed.

// realRig activates the extension over a copy of the folder of testdata/gotests with the real go.
func realRig(t *testing.T, fixture string, settings map[string]any) *rig {
	t.Helper()
	if realGoPath == "" {
		t.Skip("go is not installed")
	}
	if realTools {
		t.Skip("the real go tests keep their own environment")
	}
	for k, v := range map[string]string{
		"PATH": filepath.Dir(realGoPath), "GOCACHE": sharedGoCache, "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOFLAGS": "", "GOENV": "off",
		"GOWORK": "off", "CGO_ENABLED": "0", "TMPDIR": t.TempDir(),
	} {
		t.Setenv(k, v)
	}
	r := newRig(t, rigOptions{realGo: true, copyFrom: filepath.Join("testdata", "gotests", fixture), settings: settings})
	r.discovered()
	return r
}

// runIt runs the include (everything with none) with the profile of the kind and returns the run the handler made.
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

func TestTheRealGoRunsEveryKindOfPackageAsOneModule(t *testing.T) {
	r := realRig(t, "mod", nil)
	run := runIt(t, r, extension.TestKindRun, nil, nil)
	// the five packages are run at once
	if runs := r.goRealRuns(); runs != 1 {
		t.Errorf("%d runs of go test", runs)
	}
	want := map[string]string{
		"TestAdd": "passed", "TestTable": "passed", "TestTable/small": "passed", "TestTable/two_words": "passed", "TestTable/negative": "passed", "ExampleAdd": "passed",
		"TestPasses": "passed", "TestWrong": "failed", "TestSkipped": "skipped", "TestSubs": "failed", "TestSubs/ok": "passed", "TestSubs/bad": "failed",
		"package:build": "errored", "TestBroken": "errored", "TestOther": "errored",
		"TestFine": "passed", "TestBoom": "failed", "TestSign": "passed",
	}
	got := outcomes(run)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, ran := got["BenchmarkAdd"]; ran {
		t.Error("a benchmark was run by a package run")
	}
	dir := func(pkg, file string) string { return fileURI(filepath.Join(r.folder, pkg, file)) }
	// a failed test: the message, its values and its place
	msgs := messagesOf(run, "TestWrong")
	if len(msgs) != 1 || msgs[0].Message != "Double(2) = 4, want 5" || msgs[0].ExpectedOutput != "5" || msgs[0].ActualOutput != "4" ||
		msgs[0].Location == nil || msgs[0].Location.URI != dir("fail", "fail_test.go") || msgs[0].Location.Range.Start.Line != 13 {
		t.Errorf("TestWrong: %+v", msgs)
	}
	if msgs := messagesOf(run, "TestSubs/bad"); len(msgs) != 1 || msgs[0].Message != "the subtest failed" || msgs[0].Location.Range.Start.Line != 24 {
		t.Errorf("TestSubs/bad: %+v", msgs)
	}
	if msgs := messagesOf(run, "TestSubs"); len(msgs) != 0 {
		t.Errorf("the parent of a failed subtest has messages: %+v", msgs)
	}
	// a package that does not build: the compiler's message at its place
	msgs = messagesOf(run, "package:build")
	if len(msgs) != 1 || msgs[0].Message != "undefined: undefinedName" || msgs[0].Location == nil || msgs[0].Location.URI != dir("build", "build_test.go") ||
		msgs[0].Location.Range.Start.Line != 5 || msgs[0].Location.Range.Start.Col != 3 {
		t.Errorf("package build: %+v", msgs)
	}
	// a panic: the first frame in the module, and the frames of the module as the stack
	msgs = messagesOf(run, "TestBoom")
	if len(msgs) != 1 || !strings.HasPrefix(msgs[0].Message, "panic: assignment to entry in nil map") || msgs[0].Location == nil ||
		msgs[0].Location.URI != dir("boom", "boom.go") || msgs[0].Location.Range.Start.Line != 4 {
		t.Fatalf("TestBoom: %+v", msgs)
	}
	var frames []string
	for _, f := range msgs[0].StackTrace {
		frames = append(frames, filepath.Base(strings.TrimPrefix(f.URI, "file://")))
	}
	if !slices.Contains(frames, "boom.go") || !slices.Contains(frames, "boom_test.go") {
		t.Errorf("stack = %v", frames)
	}
	// the subtests the source did not name are in the tree, after the run
	tree := dump(find(t, r.ctl().Items(), "pass").Children())
	for _, name := range []string{"two_words\n", "negative\n", "small @"} {
		if !strings.Contains(tree, name) {
			t.Errorf("the tree of pass has no %q:\n%s", name, tree)
		}
	}
	// the failures are in Problems
	r.api.Pump()
	var marks []string
	for _, ms := range r.api.Markers(TestsMarkerOwner) {
		for _, m := range ms {
			marks = append(marks, m.Message)
		}
	}
	slices.Sort(marks)
	if len(marks) < 4 {
		t.Errorf("markers = %q", marks)
	}
}

// goRealRuns counts the runs of go test of the run: the output of the run starts with "$ go test" for each.
func (r *rig) goRealRuns() int {
	n := 0
	for _, run := range r.ctl().Runs() {
		for _, e := range run.Events() {
			if e.Kind == fakeapi.RunOutput && e.Item == nil && strings.HasPrefix(e.Text, "$ go test") {
				n++
			}
		}
	}
	return n
}

func TestTheRealGoRunsASubtestAndABenchmarkAndWhatIsNotExcluded(t *testing.T) {
	r := realRig(t, "mod", nil)
	pass := find(t, r.ctl().Items(), "pass/pass_test.go")
	// one subtest: only it runs
	run := runIt(t, r, extension.TestKindRun, []*extension.TestItem{find(t, find(t, pass.Children(), "TestTable").Children(), "small")}, nil)
	got := outcomes(run)
	if got["TestTable/small"] != "passed" || got["TestAdd"] != "" || got["TestTable/negative"] != "" {
		t.Errorf("outcomes = %v", got)
	}
	// a benchmark
	run = runIt(t, r, extension.TestKindRun, []*extension.TestItem{find(t, pass.Children(), "BenchmarkAdd")}, nil)
	if got := outcomes(run); got["BenchmarkAdd"] != "passed" || len(got) != 1 {
		t.Errorf("outcomes of the benchmark = %v", got)
	}
	var benchOutput bool
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunOutput && strings.Contains(e.Text, "ns/op") {
			benchOutput = true
		}
	}
	if !benchOutput {
		t.Error("the output of the benchmark has no ns/op")
	}
	// everything but the failing and the broken packages: the passing ones run, one go test for each
	run = runIt(t, r, extension.TestKindRun, nil, []*extension.TestItem{find(t, r.ctl().Items(), "fail"), find(t, r.ctl().Items(), "build"), find(t, r.ctl().Items(), "boom")})
	got = outcomes(run)
	if got["TestAdd"] != "passed" || got["TestSign"] != "passed" || got["TestWrong"] != "" || got["TestBroken"] != "" || got["TestBoom"] != "" {
		t.Errorf("outcomes = %v", got)
	}
}

func TestTheRealGoCoverageProfileIsReadIntoTheCoverageOfTheFiles(t *testing.T) {
	r := realRig(t, "mod", nil)
	run := runIt(t, r, extension.TestKindCoverage, []*extension.TestItem{find(t, r.ctl().Items(), "cover"), find(t, r.ctl().Items(), "pass")}, nil)
	got := outcomes(run)
	if got["TestSign"] != "passed" || got["TestAdd"] != "passed" {
		t.Errorf("outcomes = %v", got)
	}
	files := map[string]extension.FileCoverage{}
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunCoverage {
			files[filepath.Base(strings.TrimPrefix(e.Coverage.URI, "file://"))] = e.Coverage
		}
	}
	if len(files) != 2 || files["cover.go"].URI != fileURI(filepath.Join(r.folder, "cover", "cover.go")) || files["pass.go"].URI == "" {
		t.Fatalf("files = %+v", files)
	}
	cv := files["cover.go"]
	// Sign has five statements of which `return "zero"` is not tried, and Unused (one statement) is never called
	if cv.Statement.Total != 6 || cv.Statement.Covered != 4 || cv.Declaration == nil || *cv.Declaration != (extension.CoverageCount{Covered: 1, Total: 2}) {
		t.Errorf("cover.go = %+v / %+v", cv.Statement, cv.Declaration)
	}
	if pc := files["pass.go"]; pc.Statement != (extension.CoverageCount{Covered: 1, Total: 1}) {
		t.Errorf("pass.go = %+v", pc.Statement)
	}
	d, err := profileOf(r, extension.TestKindCoverage).LoadCoverage(context.Background(), cv)
	if err != nil {
		t.Fatal(err)
	}
	var missed []int
	for _, s := range d.Statements {
		if s.Executed == 0 {
			missed = append(missed, s.Location.Start.Line+1)
		}
	}
	// the block of `return "zero"` starts on line 10, and Unused's on line 17
	if !slices.Equal(missed, []int{10, 17}) {
		t.Errorf("uncovered blocks start at lines %v", missed)
	}
	if len(d.Declarations) != 2 || d.Declarations[0].Name != "Sign" || d.Declarations[0].Executed == 0 || d.Declarations[1].Name != "Unused" || d.Declarations[1].Executed != 0 {
		t.Errorf("declarations = %+v", d.Declarations)
	}
	// the profiles are gone with the run
	if left, _ := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), "emetos-gocover-*")); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

func TestACancelEndsTheRealGoTestAndTheTestItStarted(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	r := realRig(t, "slow", map[string]any{TestEnvVarsSetting: map[string]any{"EMETOS_PIDFILE": pidfile}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := profileOf(r, extension.TestKindRun).Run(ctx, nil, nil, false)
		done <- err
	}()
	var pid int
	deadline := time.Now().Add(120 * time.Second)
	for pid == 0 {
		if data, err := os.ReadFile(pidfile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if pid == 0 && time.Now().After(deadline) {
			t.Fatal("the test did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if gone(pid) {
		t.Fatal("the test is not running")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the handler did not return")
	}
	requireGone(t, "the test binary", pid)
	run := r.ctl().Runs()[0]
	if !run.Ended() || outcomes(run)["TestSleep"] != "started" {
		t.Errorf("ended %v outcomes %v", run.Ended(), outcomes(run))
	}
}
