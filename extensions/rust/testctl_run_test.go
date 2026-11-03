package rust

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

const cargoTree = `calc
  lib
    tests @33
      adds @37
      divides @42
      later @48
      nested @72
        deep @74
      prints_and_passes @67
      thread_panics @59
      zero_panics @54
  bin calc
    tests @6
      main_runs @8
  test integration
    adds_across_the_crate @2
    checked_divides @7
  doc tests
    add (line 5) @5
    divide (line 14) @14
`

func TestDiscoveryMakesTheTreeOfPackagesTargetsModulesAndTests(t *testing.T) {
	r := newCargoRig(t, cargoOptions{})
	r.found()
	if got := dump(r.ctl().Items()); got != cargoTree {
		t.Errorf("tree:\n%s\nwant:\n%s", got, cargoTree)
	}
	want := []string{
		"metadata --no-deps --format-version 1",
		"test -p calc --lib -- --list --format terse", "test -p calc --bin calc -- --list --format terse",
		"test -p calc --test integration -- --list --format terse", "test -p calc --doc -- --list --format terse",
	}
	if got := r.called(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("cargo was called with %q, want %q", got, want)
	}
	d := find(t, r.ctl().Items(), "calc/lib/tests/divides").Data()
	if d.URI != fileURI(filepath.Join(r.folder, "src/lib.rs")) {
		t.Errorf("the test is in %s", d.URI)
	}
	if find(t, r.ctl().Items(), "calc/test integration").Data().URI != fileURI(filepath.Join(r.folder, "tests/integration.rs")) {
		t.Error("the integration test target is not at its file")
	}
}

func TestAFolderWithNoCargoTomlHasNoTests(t *testing.T) {
	r := newCargoRig(t, cargoOptions{files: map[string]string{"Cargo.toml": ""}})
	r.found()
	if got := dump(r.ctl().Items()); got != "" || len(r.called()) != 0 {
		t.Errorf("tree %q, calls %q", got, r.called())
	}
}

func TestWithoutCargoTheTestsAreNotFoundQuietlyAndRefreshTellsTheUser(t *testing.T) {
	r := newCargoRig(t, cargoOptions{noCargo: true})
	r.found()
	if len(r.api.Notes()) != 0 || dump(r.ctl().Items()) != "" {
		t.Errorf("notes %v", r.api.Notes())
	}
	if err := r.ctl().Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.settled()
	if got := r.note(); !strings.Contains(got, "cargo was not found") {
		t.Errorf("note %q", got)
	}
}

func TestATargetThatDoesNotCompileIsAnErrorRowAndTheOthersStay(t *testing.T) {
	cases := append([]fakeCase(nil), listCases...)
	cases[1] = fakeCase{args: cases[1].args, file: "build_error.txt", code: 101}
	r := newCargoRig(t, cargoOptions{cases: cases})
	r.found()
	got := dump(r.ctl().Items())
	if !strings.Contains(got, "  lib !error[E0425]: cannot find value `missing` in this scope\n") || !strings.Contains(got, "    adds_across_the_crate @2") {
		t.Errorf("tree:\n%s", got)
	}
}

func TestSavesAndFileChangesFindTheTestsAgain(t *testing.T) {
	r := newCargoRig(t, cargoOptions{})
	save := func(rel string) {
		r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: fileURI(filepath.Join(r.folder, rel))})
		r.settled()
		time.Sleep(30 * time.Millisecond)
		r.settled()
	}
	if len(r.called()) == 0 {
		t.Fatal("the tests were not found when the pack started")
	}
	for _, tc := range []struct {
		name string
		do   func()
		more bool
	}{
		{"a Rust file is saved", func() { save("src/lib.rs") }, true},
		{"Cargo.toml is saved", func() { save("Cargo.toml") }, true},
		{"another file is saved", func() { save("README.md") }, false},
		{"a file outside the folder is saved", func() {
			r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: "file:///elsewhere/x.rs"})
			time.Sleep(30 * time.Millisecond)
			r.settled()
		}, false},
		{"a Rust file is created", func() {
			r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileCreated, Path: "src/new.rs"}}})
			time.Sleep(30 * time.Millisecond)
			r.settled()
		}, true},
		{"a text file is created", func() {
			r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileCreated, Path: "notes.txt"}}})
			time.Sleep(30 * time.Millisecond)
			r.settled()
		}, false},
	} {
		before := len(r.called())
		tc.do()
		if got := len(r.called()) > before; got != tc.more {
			t.Errorf("%s: found again = %v, want %v", tc.name, got, tc.more)
		}
	}
}

func TestARunReportsEveryTestWithItsMessagesAndOutput(t *testing.T) {
	r := newCargoRig(t, cargoOptions{})
	r.found()
	run := r.run(nil, nil)
	if !run.Ended() || run.Name() != "cargo test" || !run.Persist() {
		t.Errorf("ended %v name %q persist %v", run.Ended(), run.Name(), run.Persist())
	}
	want := map[string]string{
		"calc/lib/tests/adds": "passed", "calc/lib/tests/divides": "failed", "calc/lib/tests/later": "skipped", "calc/lib/tests/nested/deep": "passed",
		"calc/lib/tests/prints_and_passes": "passed", "calc/lib/tests/thread_panics": "failed", "calc/lib/tests/zero_panics": "passed",
		"calc/bin calc/tests/main_runs": "passed", "calc/test integration/adds_across_the_crate": "passed",
		"calc/test integration/checked_divides": "failed", "calc/doc tests/add (line 5)": "passed", "calc/doc tests/divide (line 14)": "failed",
	}
	got := outcomes(run)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d items reported, want %d: %v", len(got), len(want), got)
	}
	runs := r.called()[5:]
	wantRuns := []string{
		"test -p calc --lib --no-fail-fast -- --show-output", "test -p calc --bin calc --no-fail-fast -- --show-output",
		"test -p calc --test integration --no-fail-fast -- --show-output", "test -p calc --doc --no-fail-fast -- --show-output",
	}
	if strings.Join(runs, "\n") != strings.Join(wantRuns, "\n") {
		t.Errorf("runs %q, want %q", runs, wantRuns)
	}
	if out := outputOf(run, "calc/lib/tests/prints_and_passes"); !strings.Contains(out, "hello from a passing test") {
		t.Errorf("the output of the passing test is %q", out)
	}
	if out := outputOf(run, ""); !strings.HasPrefix(out, "$ cargo test -p calc --lib ") || !strings.Contains(out, "test result: FAILED. 4 passed; 2 failed") || !strings.Contains(out, "\r\n") {
		t.Errorf("the output of the run is %q", out)
	}
}

func TestTheMessagesOfFailuresHaveTheirPlacesSidesAndThreads(t *testing.T) {
	r := newCargoRig(t, cargoOptions{})
	r.found()
	run := r.run(nil, nil)
	lib := "file://" + filepath.Join(r.folder, "src/lib.rs")
	libLine := func(n int) string {
		data, err := os.ReadFile(filepath.Join(r.folder, "src/lib.rs"))
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(string(data), "\n")[n-1]
	}
	msgs := messagesOf(run, "calc/lib/tests/divides")
	if len(msgs) != 1 {
		t.Fatalf("%d messages: %+v", len(msgs), msgs)
	}
	m := msgs[0]
	if m.Message != "assertion `left == right` failed\n  left: 5\n right: 4" || m.ActualOutput != "5" || m.ExpectedOutput != "4" {
		t.Errorf("message %+v", m)
	}
	wantRange := extension.Range{Start: extension.Position{Line: 42, Col: 8}, End: extension.Position{Line: 42, Col: len(strings.TrimRight(libLine(43), " "))}}
	if m.Location == nil || m.Location.URI != lib || m.Location.Range != wantRange {
		t.Errorf("location %+v, want %s %+v", m.Location, lib, wantRange)
	}
	// a panic in a thread the test started comes first, then the test's own
	msgs = messagesOf(run, "calc/lib/tests/thread_panics")
	if len(msgs) != 2 || msgs[0].Message != "thread '<unnamed>' panicked: worker failed" || msgs[1].Message != "the worker thread panicked" ||
		msgs[0].Location == nil || msgs[0].Location.Range.Start.Line != 60 || msgs[1].Location.Range.Start.Line != 62 {
		t.Errorf("thread panic messages %+v", msgs)
	}
	// an integration test
	msgs = messagesOf(run, "calc/test integration/checked_divides")
	if len(msgs) != 1 || msgs[0].ActualOutput != "4" || msgs[0].ExpectedOutput != "5" ||
		msgs[0].Location == nil || msgs[0].Location.URI != "file://"+filepath.Join(r.folder, "tests/integration.rs") || msgs[0].Location.Range.Start.Line != 7 {
		t.Errorf("integration messages %+v", msgs)
	}
	// a documentation test is at its own line, not at the line rustdoc's generated program names
	msgs = messagesOf(run, "calc/doc tests/divide (line 14)")
	if len(msgs) != 1 || msgs[0].Location == nil || msgs[0].Location.URI != lib || msgs[0].Location.Range.Start.Line != 13 || msgs[0].ActualOutput != "5" {
		t.Errorf("doc messages %+v", msgs)
	}
	if out := outputOf(run, "calc/lib/tests/divides"); !strings.Contains(out, "panicked at src/lib.rs:43:9") {
		t.Errorf("the output of the failure is %q", out)
	}
}

func TestRequestsBecomeCommandLines(t *testing.T) {
	for _, tc := range []struct {
		name    string
		include []string
		exclude []string
		want    []string
	}{
		{"one test", []string{"calc/lib/tests/adds"}, nil, []string{"test -p calc --lib --no-fail-fast -- --show-output --exact tests::adds"}},
		{"a module with every test of the target", []string{"calc/lib/tests"}, nil, []string{"test -p calc --lib --no-fail-fast -- --show-output"}},
		{"a nested module", []string{"calc/lib/tests/nested"}, nil, []string{"test -p calc --lib --no-fail-fast -- --show-output --exact tests::nested::deep"}},
		{"a target", []string{"calc/bin calc"}, nil, []string{"test -p calc --bin calc --no-fail-fast -- --show-output"}},
		{"an integration test", []string{"calc/test integration/checked_divides"}, nil, []string{"test -p calc --test integration --no-fail-fast -- --show-output --exact checked_divides"}},
		{"a doc test is chosen by its item", []string{"calc/doc tests/divide (line 14)"}, nil, []string{"test -p calc --doc --no-fail-fast -- --show-output divide"}},
		{"two tests of two targets", []string{"calc/lib/tests/adds", "calc/bin calc/tests/main_runs"}, nil, []string{
			"test -p calc --lib --no-fail-fast -- --show-output --exact tests::adds", "test -p calc --bin calc --no-fail-fast -- --show-output"}},
		{"a target without a test", []string{"calc/lib"}, []string{"calc/lib/tests/adds", "calc/lib/tests/later"}, []string{
			"test -p calc --lib --no-fail-fast -- --show-output --exact tests::divides tests::nested::deep tests::prints_and_passes tests::thread_panics tests::zero_panics"}},
		{"everything but a target", nil, []string{"calc/lib", "calc/doc tests"}, []string{
			"test -p calc --bin calc --no-fail-fast -- --show-output", "test -p calc --test integration --no-fail-fast -- --show-output"}},
		{"a test and its target", []string{"calc/lib", "calc/lib/tests/adds"}, nil, []string{"test -p calc --lib --no-fail-fast -- --show-output"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newCargoRig(t, cargoOptions{})
			r.found()
			items := func(paths []string) []*extension.TestItem {
				var out []*extension.TestItem
				for _, p := range paths {
					out = append(out, find(t, r.ctl().Items(), p))
				}
				return out
			}
			n := len(r.called())
			r.run(items(tc.include), items(tc.exclude))
			if got := r.called()[n:]; strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("cargo was called with\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestOnlyTheTestsAskedForAreReported(t *testing.T) {
	r := newCargoRig(t, cargoOptions{})
	r.found()
	// the filter "divide" selects the other documentation tests too in a real run: here the recorded run has both
	run := r.run([]*extension.TestItem{find(t, r.ctl().Items(), "calc/doc tests/divide (line 14)")}, nil)
	got := outcomes(run)
	if len(got) != 1 || got["calc/doc tests/divide (line 14)"] != "failed" {
		t.Errorf("reported %v", got)
	}
	run = r.run([]*extension.TestItem{find(t, r.ctl().Items(), "calc/lib/tests/adds"), find(t, r.ctl().Items(), "calc/lib/tests/nested/deep"), find(t, r.ctl().Items(), "calc/lib/tests/divides")}, nil)
	got = outcomes(run)
	if got["calc/lib/tests/adds"] != "passed" || got["calc/lib/tests/divides"] != "failed" || len(got) != 3 {
		t.Errorf("reported %v", got)
	}
}

func TestATargetThatDoesNotBuildErrorsItsTestsWithTheCompilersWords(t *testing.T) {
	cases := append([]fakeCase(nil), listCases...)
	cases = append(cases, fakeCase{args: "test -p calc --lib --no-fail-fast -- --show-output", file: "build_error.txt", code: 101})
	r := newCargoRig(t, cargoOptions{cases: cases})
	r.found()
	run := r.run([]*extension.TestItem{find(t, r.ctl().Items(), "calc/lib")}, nil)
	got := outcomes(run)
	if got["calc/lib/tests/adds"] != "errored" || got["calc/lib/tests/nested/deep"] != "errored" || len(got) != 7 {
		t.Errorf("reported %v", got)
	}
	msgs := messagesOf(run, "calc/lib/tests/adds")
	if len(msgs) != 1 || !strings.HasPrefix(msgs[0].Message, "error[E0425]: cannot find value `missing`") || strings.Contains(msgs[0].Message, "Compiling") {
		t.Fatalf("messages %+v", msgs)
	}
	if l := msgs[0].Location; l == nil || l.URI != fileURI(filepath.Join(r.folder, "src/lib.rs")) || l.Range.Start.Line != 43 {
		t.Errorf("location %+v", l)
	}
}

func TestACancelStopsCargoAndItsChildren(t *testing.T) {
	cases := append([]fakeCase(nil), listCases...)
	cases = append(cases, fakeCase{args: "test -p calc --lib --no-fail-fast -- --show-output", sleep: 60})
	r := newCargoRig(t, cargoOptions{cases: cases})
	r.found()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *fakeapi.FakeTestRun, 1)
	go func() {
		before := len(r.ctl().Runs())
		_, _ = r.profile(extension.TestKindRun).Run(ctx, []*extension.TestItem{find(t, r.ctl().Items(), "calc/lib")}, nil, false)
		done <- r.ctl().Runs()[before]
	}()
	pidFile := filepath.Join(filepath.Dir(r.folder), "pid")
	r.api.WaitFor("cargo to start", func() bool { _, err := os.Stat(pidFile); return err == nil })
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
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

func TestTheExtraArgumentsOfTheSettingGoToCargo(t *testing.T) {
	cases := []fakeCase{listCases[0], listCases[1], {args: "test -p calc --bin calc --release --features x -- --list --format terse", file: "list_bin.txt"},
		{args: "test -p calc --test integration --release --features x -- --list --format terse", file: "list_test.txt"},
		{args: "test -p calc --doc --release --features x -- --list --format terse", file: "list_doc.txt"},
		{args: "test -p calc --bin calc --release --features x --no-fail-fast -- --show-output", file: "run_bin.txt"}}
	cases[1].args = "test -p calc --lib --release --features x -- --list --format terse"
	r := newCargoRig(t, cargoOptions{cases: cases, settings: map[string]any{RunnablesExtraArgsSetting: []any{"--release", "--features", "x"}}})
	r.found()
	r.run([]*extension.TestItem{find(t, r.ctl().Items(), "calc/bin calc")}, nil)
	calls := r.called()
	if calls[2] != "test -p calc --bin calc --release --features x -- --list --format terse" ||
		calls[len(calls)-1] != "test -p calc --bin calc --release --features x --no-fail-fast -- --show-output" {
		t.Errorf("calls %q", calls)
	}
}

func TestAWindowWithoutTheTestingViewHasNoController(t *testing.T) {
	r := newCargoRig(t, cargoOptions{noTesting: true})
	if r.ext.tests != nil || r.api.TestController(TestControllerID) != nil {
		t.Error("the pack made a controller")
	}
}

func TestTheProfilesAreRunAndDebugWithNoCoverage(t *testing.T) {
	r := newCargoRig(t, cargoOptions{})
	var got []string
	for _, p := range r.ctl().Profiles() {
		got = append(got, p.Label()+"/"+strconv.Itoa(int(p.Kind()))+"/"+strconv.FormatBool(p.IsDefault())+"/"+strconv.FormatBool(p.SupportsContinuousRun()))
	}
	want := []string{"cargo test/" + strconv.Itoa(int(extension.TestKindRun)) + "/true/true", "cargo test (debug)/" + strconv.Itoa(int(extension.TestKindDebug)) + "/true/false"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("profiles %v, want %v", got, want)
	}
	run := r.profile(extension.TestKindRun)
	for _, tc := range []struct {
		uri  string
		want bool
	}{{fileURI(filepath.Join(r.folder, "src/lib.rs")), true}, {fileURI(filepath.Join(r.folder, "Cargo.toml")), true},
		{fileURI(filepath.Join(r.folder, "README.md")), false}, {"file:///elsewhere/a.rs", false}} {
		if hit, _ := run.Triggers(nil, tc.uri); hit != tc.want {
			t.Errorf("trigger %s = %v", tc.uri, hit)
		}
	}
}

func TestTheFramesOfTheUsersCodeAreTheStackOfAMessage(t *testing.T) {
	cases := append([]fakeCase(nil), listCases...)
	cases = append(cases, fakeCase{args: "test -p calc --lib --no-fail-fast -- --show-output --exact tests::divides", file: "run_backtrace.txt", code: 101})
	r := newCargoRig(t, cargoOptions{cases: cases})
	r.found()
	run := r.run([]*extension.TestItem{find(t, r.ctl().Items(), "calc/lib/tests/divides")}, nil)
	msgs := messagesOf(run, "calc/lib/tests/divides")
	if len(msgs) != 1 || len(msgs[0].StackTrace) != 2 {
		t.Fatalf("messages %+v", msgs)
	}
	lib := fileURI(filepath.Join(r.folder, "src/lib.rs"))
	f0, f1 := msgs[0].StackTrace[0], msgs[0].StackTrace[1]
	if f0.Label != "calc::tests::divides" || f0.URI != lib || *f0.Position != (extension.Position{Line: 42, Col: 8}) || f1.Position.Line != 40 {
		t.Errorf("stack %+v %+v", f0, f1)
	}
}

func TestResolveAndRefreshFindTheTestsAgain(t *testing.T) {
	r := newCargoRig(t, cargoOptions{})
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
	if got := dump(r.ctl().Items()); got != cargoTree {
		t.Errorf("tree:\n%s", got)
	}
}
