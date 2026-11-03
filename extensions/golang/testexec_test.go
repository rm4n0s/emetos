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
	"github.com/rm4n0s/emetos/extensions/golang/gotest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The runs of the controller (step 26.5) against the fake go, which prints the recorded output of the real `go test -json` of
// testdata/testjson, and the selection of a request.

// testSource makes the source of a test file in the package with the functions.
func testSource(pkg string, funcs ...string) string {
	return "package " + pkg + "\n\nimport \"testing\"\n\n" + strings.Join(funcs, "\n\n") + "\n"
}

// fixtureFiles are the modules the fixtures of testdata/testjson were recorded for, with the tests they ran.
func fixtureFiles() map[string]string {
	return map[string]string{
		"mixed/go.mod": "module example.com/mixed\n\ngo 1.21\n",
		"mixed/mixed_test.go": testSource("mixed",
			"func TestAddPasses(t *testing.T) {}", "func TestAddFails(t *testing.T) {}", "func TestSkipped(t *testing.T) {}",
			"func TestTable(t *testing.T) {\n\tt.Run(\"small\", func(t *testing.T) {})\n\tfor _, c := range cases {\n\t\tt.Run(c.name, func(t *testing.T) {})\n\t}\n}"),
		"ok/go.mod":        "module example.com/ok\n\ngo 1.21\n",
		"ok/ok_test.go":    testSource("ok", "func TestOne(t *testing.T) {}", "func TestTwo(t *testing.T) {}"),
		"panics/go.mod":    "module example.com/panics\n\ngo 1.21\n",
		"panics/p_test.go": testSource("panics", "func TestOk(t *testing.T) {}", "func TestBoom(t *testing.T) {}"),
		"broken/go.mod":    "module example.com/broken\n\ngo 1.21\n",
		"broken/b_test.go": testSource("broken", "func TestBroken(t *testing.T) {}", "func TestOther(t *testing.T) {}"),
	}
}

// runWith activates the extension with the fixture printed by go test, discovers the tests and runs the include (all the tests with none)
// with the profile of the kind. It returns the rig and the run the handler made.
func runWith(t *testing.T, fixture string, kind extension.TestRunKind, pick func(r *rig) []*extension.TestItem) (*rig, *fakeapi.FakeTestRun) {
	t.Helper()
	r := newRig(t, rigOptions{withGo: true, files: fixtureFiles(), env: []string{gotest.EnvTestFile + "=" + fixturePath(t, fixture)}})
	r.discovered()
	var include []*extension.TestItem
	if pick != nil {
		include = pick(r)
	}
	if _, err := profileOf(r, kind).Run(context.Background(), include, nil, false); err != nil {
		t.Fatal(err)
	}
	runs := r.ctl().Runs()
	if len(runs) != 1 {
		t.Fatalf("%d runs", len(runs))
	}
	r.consumed = 1
	return r, runs[0]
}

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

// find finds an item of the tree by the labels from the top, joined by slashes ("a/a_test.go/TestB/x"; no label has a slash).
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

// outcomes are the last states the run gave the items, by "Name" or "Name/sub" for a test and "package:dir" for a package.
func outcomes(run *fakeapi.FakeTestRun) map[string]string {
	got := map[string]string{}
	for _, e := range run.Events() {
		if e.Item == nil {
			continue
		}
		switch e.Kind {
		case fakeapi.RunEnqueued, fakeapi.RunStarted, fakeapi.RunPassed, fakeapi.RunFailed, fakeapi.RunErrored, fakeapi.RunSkipped:
			got[keyOf(e.Item)] = string(e.Kind)
		}
	}
	return got
}

// keyOf names an item for the tests: the name after the # of a test's id, "package:dir" for a package, "file:name" for a file.
func keyOf(it *extension.TestItem) string {
	id := it.ID()
	if i := strings.Index(id, "#"); i >= 0 {
		return id[i+1:]
	}
	if strings.HasSuffix(id, "?package") {
		return "package:" + filepath.Base(strings.TrimSuffix(id, "?package"))
	}
	return "file:" + filepath.Base(strings.TrimSuffix(id, "?file"))
}

// messagesOf are the messages of the last failed or errored event of the item.
func messagesOf(run *fakeapi.FakeTestRun, key string) []extension.TestMessage {
	var msgs []extension.TestMessage
	for _, e := range run.Events() {
		if e.Item != nil && keyOf(e.Item) == key && (e.Kind == fakeapi.RunFailed || e.Kind == fakeapi.RunErrored) {
			msgs = e.Messages
		}
	}
	return msgs
}

func TestARunOfTheMixedFixtureReportsEveryTestWithItsMessages(t *testing.T) {
	r, run := runWith(t, "mixed.jsonl", extension.TestKindRun, func(r *rig) []*extension.TestItem {
		return []*extension.TestItem{find(t, r.ctl().Items(), "mixed")}
	})
	if !run.Ended() || run.Name() != "go test" {
		t.Errorf("ended %v name %q", run.Ended(), run.Name())
	}
	want := map[string]string{
		"TestAddPasses": "passed", "TestAddFails": "failed", "TestSkipped": "skipped", "TestTable": "failed", "TestTable/small": "passed", "TestTable/wrong": "failed",
	}
	got := outcomes(run)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	// the failure of a test is its message, at the line of the test file, with the two values
	msgs := messagesOf(run, "TestAddFails")
	wantURI := fileURI(filepath.Join(r.folder, "mixed", "mixed_test.go"))
	if len(msgs) != 1 || msgs[0].Message != "Add(2, 2) = 4, want 5" || msgs[0].ExpectedOutput != "5" || msgs[0].ActualOutput != "4" ||
		msgs[0].Location == nil || msgs[0].Location.URI != wantURI || msgs[0].Location.Range.Start.Line != 13 {
		t.Errorf("messages of TestAddFails = %+v", msgs)
	}
	if msgs := messagesOf(run, "TestTable/wrong"); len(msgs) != 1 || msgs[0].ExpectedOutput != "6" || msgs[0].ActualOutput != "5" || msgs[0].Location.Range.Start.Line != 28 {
		t.Errorf("messages of TestTable/wrong = %+v", msgs)
	}
	// the parent failed because of the subtest: no message of its own
	if msgs := messagesOf(run, "TestTable"); len(msgs) != 0 {
		t.Errorf("messages of TestTable = %+v", msgs)
	}
	// the subtest the source did not show was added to the tree, next to the one it did
	if got := dump(find(t, r.ctl().Items(), "mixed").Children()); !strings.Contains(got, "TestTable @") || !strings.Contains(got, "small @") || !strings.Contains(got, "wrong\n") {
		t.Errorf("tree =\n%s", got)
	}
	// the durations, the output of a test with the line breaks of a terminal, and the command that ran
	var sawOutput, sawCommand bool
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunOutput && e.Item != nil && keyOf(e.Item) == "TestAddFails" && strings.Contains(e.Text, "want 5\r\n") {
			sawOutput = true
		}
		if e.Kind == fakeapi.RunOutput && e.Item == nil && strings.HasPrefix(e.Text, "$ go test -json -timeout 30s") {
			sawCommand = true
		}
		if e.Kind == fakeapi.RunPassed && e.Duration != 0 {
			t.Errorf("duration of %s = %v", keyOf(e.Item), e.Duration)
		}
	}
	if !sawOutput || !sawCommand {
		t.Errorf("output of the test %v, the command %v", sawOutput, sawCommand)
	}
	// the failures are in Problems, with the name of the test as the code
	r.api.Pump()
	var marks []string
	for _, ms := range r.api.Markers(TestsMarkerOwner) {
		for _, m := range ms {
			marks = append(marks, itoa(m.StartLine)+" "+m.Code)
		}
	}
	slices.Sort(marks)
	if !slices.Equal(marks, []string{"14 TestAddFails", "29 TestTable/wrong"}) {
		t.Errorf("markers = %v", marks)
	}
}

func TestARunOfAPassingPackageAndOfThePanicAndTheBuildFailure(t *testing.T) {
	t.Run("passing", func(t *testing.T) {
		_, run := runWith(t, "passing.jsonl", extension.TestKindRun, nil)
		got := outcomes(run)
		if got["TestOne"] != "passed" || got["TestTwo"] != "passed" {
			t.Errorf("outcomes = %v", got)
		}
		for _, e := range run.Events() {
			if e.Kind == fakeapi.RunPassed && keyOf(e.Item) == "TestOne" && e.Duration != 1250*time.Millisecond {
				t.Errorf("duration of TestOne = %v", e.Duration)
			}
		}
	})
	t.Run("a panic", func(t *testing.T) {
		r, run := runWith(t, "panic.jsonl", extension.TestKindRun, nil)
		got := outcomes(run)
		if got["TestOk"] != "passed" || got["TestBoom"] != "failed" {
			t.Errorf("outcomes = %v", got)
		}
		msgs := messagesOf(run, "TestBoom")
		dir := filepath.Join(r.folder, "panics")
		if len(msgs) != 1 || !strings.HasPrefix(msgs[0].Message, "panic: assignment to entry in nil map") || msgs[0].Location == nil ||
			msgs[0].Location.URI != fileURI(filepath.Join(dir, "p.go")) || msgs[0].Location.Range.Start.Line != 2 {
			t.Fatalf("messages = %+v", msgs)
		}
		var frames []string
		for _, f := range msgs[0].StackTrace {
			frames = append(frames, f.Label+" "+filepath.Base(strings.TrimPrefix(f.URI, "file://"))+":"+itoa(f.Position.Line+1))
		}
		if want := []string{"example.com/panics.Boom p.go:3", "example.com/panics.TestBoom p_test.go:9"}; !slices.Equal(frames, want) {
			t.Errorf("stack = %q, want %q", frames, want)
		}
		if got["package:panics"] != "" && got["package:panics"] != "enqueued" {
			t.Errorf("the package was told %q although a test failed", got["package:panics"])
		}
	})
	for _, fixture := range []string{"build.jsonl", "build_legacy.jsonl"} {
		t.Run(fixture, func(t *testing.T) {
			r, run := runWith(t, fixture, extension.TestKindRun, nil)
			got := outcomes(run)
			if got["package:broken"] != "errored" || got["TestBroken"] != "errored" || got["TestOther"] != "errored" {
				t.Errorf("outcomes = %v", got)
			}
			msgs := messagesOf(run, "package:broken")
			if len(msgs) != 1 || msgs[0].Message != "undefined: undefinedName" || msgs[0].Location == nil ||
				msgs[0].Location.URI != fileURI(filepath.Join(r.folder, "broken", "b_test.go")) || msgs[0].Location.Range.Start.Line != 5 || msgs[0].Location.Range.Start.Col != 11 {
				t.Errorf("messages = %+v", msgs)
			}
			r.api.Pump()
			if n := r.api.MarkerCount(); n != 1 {
				t.Errorf("%d markers, want the compiler's", n)
			}
		})
	}
}

func TestATestStillRunningWhenThePackageFailsIsErroredWithWhatThePackagePrinted(t *testing.T) {
	file := filepath.Join(t.TempDir(), "timeout.jsonl")
	text := `{"Action":"run","Package":"example.com/ok","Test":"TestOne"}
{"Action":"output","Package":"example.com/ok","Test":"TestOne","Output":"panic: test timed out after 30s\n"}
{"Action":"output","Package":"example.com/ok","Output":"FAIL\texample.com/ok\t30.002s\n"}
{"Action":"fail","Package":"example.com/ok","Elapsed":30}
`
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, rigOptions{withGo: true, files: fixtureFiles(), env: []string{gotest.EnvTestFile + "=" + file}})
	r.discovered()
	if _, err := profileOf(r, extension.TestKindRun).Run(context.Background(), []*extension.TestItem{find(t, r.ctl().Items(), "ok")}, nil, false); err != nil {
		t.Fatal(err)
	}
	run := r.ctl().Runs()[0]
	got := outcomes(run)
	if got["TestOne"] != "errored" || got["TestTwo"] != "enqueued" {
		t.Errorf("outcomes = %v", got)
	}
	if msgs := messagesOf(run, "TestOne"); len(msgs) != 1 || !strings.Contains(msgs[0].Message, "panic: test timed out after 30s") {
		t.Errorf("messages = %+v", msgs)
	}
}

func TestLinesThatAreNotEventsAreTheOutputOfTheRunAndAFailureWithoutResultsIsTold(t *testing.T) {
	file := filepath.Join(t.TempDir(), "stray.jsonl")
	if err := os.WriteFile(file, []byte("go: downloading example.com/x v1.0.0\n#stderr flag provided but not defined: -nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, rigOptions{withGo: true, files: fixtureFiles(), env: []string{gotest.EnvTestFile + "=" + file, gotest.EnvTestExit + "=2"}})
	r.discovered()
	if _, err := profileOf(r, extension.TestKindRun).Run(context.Background(), []*extension.TestItem{find(t, r.ctl().Items(), "ok")}, nil, false); err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, e := range r.ctl().Runs()[0].Events() {
		if e.Kind == fakeapi.RunOutput {
			text.WriteString(e.Text)
		}
	}
	if out := text.String(); !strings.Contains(out, "go: downloading example.com/x v1.0.0\r\n") || !strings.Contains(out, "flag provided but not defined: -nope\r\n") {
		t.Errorf("output = %q", out)
	}
	r.api.Pump()
	if sev, msg := r.lastNote(); sev != "error" || msg != "go test failed: go: downloading example.com/x v1.0.0" {
		t.Errorf("note %s %q", sev, msg)
	}
}

// selectionFiles are two packages in the module of the folder: a (TestA, TestB with the subtests x and y, BenchmarkA) and b (TestC).
func selectionFiles() map[string]string {
	return map[string]string{
		"a/a_test.go": testSource("a",
			"func TestA(t *testing.T) {}",
			"func TestB(t *testing.T) {\n\tt.Run(\"x\", func(t *testing.T) {})\n\tt.Run(\"y\", func(t *testing.T) {})\n}",
			"func BenchmarkA(b *testing.B) {}"),
		"b/b_test.go": testSource("b", "func TestC(t *testing.T) {}"),
	}
}

func TestARequestBecomesTheInvocationsOfGoTest(t *testing.T) {
	file := func(r *rig) *extension.TestItem { return find(t, r.ctl().Items(), "a/a_test.go") }
	fn := func(name string) func(r *rig) *extension.TestItem {
		return func(r *rig) *extension.TestItem { return find(t, file(r).Children(), name) }
	}
	sub := func(parent, name string) func(r *rig) *extension.TestItem {
		return func(r *rig) *extension.TestItem { return find(t, fn(parent)(r).Children(), name) }
	}
	pkg := func(name string) func(r *rig) *extension.TestItem {
		return func(r *rig) *extension.TestItem { return find(t, r.ctl().Items(), name) }
	}
	type inv struct {
		dir  string
		args []string
	}
	base := []string{"test", "-json", "-timeout", "30s"}
	tests := []struct {
		name    string
		include []func(r *rig) *extension.TestItem
		exclude []func(r *rig) *extension.TestItem
		want    []inv // dir is relative to the folder
	}{
		{"everything: the module at once", nil, nil, []inv{{".", append(slices.Clone(base), "./...")}}},
		{"a package", []func(*rig) *extension.TestItem{pkg("a")}, nil, []inv{{"a", base}}},
		{"a file: its functions, no benchmark", []func(*rig) *extension.TestItem{func(r *rig) *extension.TestItem { return file(r) }}, nil, []inv{{"a", append(slices.Clone(base), "-run", "^(TestA|TestB)$")}}},
		{"a function", []func(*rig) *extension.TestItem{fn("TestA")}, nil, []inv{{"a", append(slices.Clone(base), "-run", "^TestA$")}}},
		{"a function with subtests", []func(*rig) *extension.TestItem{fn("TestB")}, nil, []inv{{"a", append(slices.Clone(base), "-run", "^TestB$")}}},
		{"a benchmark", []func(*rig) *extension.TestItem{fn("BenchmarkA")}, nil, []inv{{"a", []string{"test", "-json", "-run=^$", "-bench", "^BenchmarkA$", "-benchmem"}}}},
		{"a subtest", []func(*rig) *extension.TestItem{sub("TestB", "x")}, nil, []inv{{"a", append(slices.Clone(base), "-run", "^TestB$/^x$")}}},
		{"a function, a benchmark and a subtest: one run for each kind", []func(*rig) *extension.TestItem{fn("TestA"), fn("BenchmarkA"), sub("TestB", "y")}, nil,
			[]inv{{"a", append(slices.Clone(base), "-run", "^TestA$")}, {"a", []string{"test", "-json", "-run=^$", "-bench", "^BenchmarkA$", "-benchmem"}}, {"a", append(slices.Clone(base), "-run", "^TestB$/^y$")}}},
		{"a function and its own subtest is the function", []func(*rig) *extension.TestItem{fn("TestB"), sub("TestB", "x")}, nil, []inv{{"a", append(slices.Clone(base), "-run", "^TestB$")}}},
		{"two packages", []func(*rig) *extension.TestItem{pkg("a"), pkg("b")}, nil, []inv{{".", append(slices.Clone(base), "./...")}}},
		{"everything but a subtest", nil, []func(*rig) *extension.TestItem{sub("TestB", "x")}, []inv{
			{"a", append(slices.Clone(base), "-run", "^TestA$")}, {"a", append(slices.Clone(base), "-run", "^TestB$/^y$")}, {"b", base}}},
		{"everything but a package", nil, []func(*rig) *extension.TestItem{pkg("b")}, []inv{{"a", base}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{withGo: true, files: selectionFiles(), env: []string{gotest.EnvTestFile + "=" + fixturePath(t, "passing.jsonl")}})
			r.discovered()
			var include, exclude []*extension.TestItem
			for _, f := range tt.include {
				include = append(include, f(r))
			}
			for _, f := range tt.exclude {
				exclude = append(exclude, f(r))
			}
			if _, err := profileOf(r, extension.TestKindRun).Run(context.Background(), include, exclude, false); err != nil {
				t.Fatal(err)
			}
			var got []inv
			for _, run := range r.goTests() {
				rel, _ := filepath.Rel(realPath(t, r.folder), realPath(t, run.Dir))
				got = append(got, inv{rel, run.Args})
			}
			if len(got) != len(tt.want) {
				t.Fatalf("runs = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i].dir != tt.want[i].dir || !slices.Equal(got[i].args, tt.want[i].args) {
					t.Errorf("run %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTheTestsOfTheRequestAreQueuedBeforeTheRunStartsAndABenchmarkOnlyWhenAskedFor(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, files: selectionFiles(), env: []string{gotest.EnvTestFile + "=" + fixturePath(t, "passing.jsonl")}})
	r.discovered()
	if _, err := profileOf(r, extension.TestKindRun).Run(context.Background(), []*extension.TestItem{find(t, r.ctl().Items(), "a")}, nil, false); err != nil {
		t.Fatal(err)
	}
	var queued []string
	for _, e := range r.ctl().Runs()[0].Events() {
		if e.Kind == fakeapi.RunEnqueued {
			queued = append(queued, keyOf(e.Item))
		}
	}
	// the leaves: TestA and the two subtests of TestB, not TestB itself and not the benchmark
	if want := []string{"TestA", "TestB/x", "TestB/y"}; !slices.Equal(queued, want) {
		t.Errorf("queued = %v, want %v", queued, want)
	}
}

func TestTheProfilesOfTheController(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true})
	var got []string
	for _, p := range r.ctl().Profiles() {
		got = append(got, p.Label()+" "+itoa(int(p.Kind()))+" "+strconv.FormatBool(p.IsDefault())+" "+strconv.FormatBool(p.SupportsContinuousRun()))
	}
	want := []string{"go test 1 true true", "go test (debug) 2 true false", "go test (coverage) 3 true false"}
	if !slices.Equal(got, want) {
		t.Errorf("profiles = %q, want %q", got, want)
	}
	if r.ctl().ID() != "go" || r.ctl().Label() != "Go" {
		t.Errorf("controller = %q %q", r.ctl().ID(), r.ctl().Label())
	}
}

func TestAChangeOfAGoFileOfThePackageStartsTheContinuousRunAgain(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, files: selectionFiles()})
	r.discovered()
	item := find(t, r.ctl().Items(), "a")
	p := profileOf(r, extension.TestKindRun)
	for uri, want := range map[string]bool{
		fileURI(filepath.Join(r.folder, "a", "a.go")):        true,
		fileURI(filepath.Join(r.folder, "a", "a_test.go")):   true,
		fileURI(filepath.Join(r.folder, "a", "go.mod")):      true,
		fileURI(filepath.Join(r.folder, "b", "b.go")):        false,
		fileURI(filepath.Join(r.folder, "a", "notes.txt")):   false,
		fileURI(filepath.Join(r.folder, "a", "sub", "s.go")): false,
		"untitled:1": false,
	} {
		if got, err := p.Triggers(item, uri); err != nil || got != want {
			t.Errorf("Triggers(%s) = %v, %v; want %v", uri, got, err, want)
		}
	}
}

func TestACommandRunStopsTheOldOneAndCancelStopsTheNewOne(t *testing.T) {
	hang := filepath.Join(t.TempDir(), "hang.jsonl")
	if err := os.WriteFile(hang, []byte(`{"Action":"run","Package":"example.com/ok","Test":"TestOne"}`+"\n#hang\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pidfile := filepath.Join(t.TempDir(), "pids")
	r := newRig(t, rigOptions{withGo: true, files: fixtureFiles(), env: []string{gotest.EnvTestFile + "=" + hang, gotest.EnvPidfile + "=" + pidfile}})
	r.discovered()
	args := map[string]any{"uri": fileURI(filepath.Join(r.folder, "ok", "ok_test.go"))}
	if err := r.api.Run(TestPackageCommand, args); err != nil {
		t.Fatal(err)
	}
	pid, child := pidsOf(t, pidfile)
	if err := os.Remove(pidfile); err != nil {
		t.Fatal(err)
	}
	// a new run stops the old one
	if err := r.api.Run(TestPackageCommand, args); err != nil {
		t.Fatal(err)
	}
	pid2, child2 := pidsOf(t, pidfile)
	first := r.testsEnded()
	requireGone(t, "the first run", pid)
	requireGone(t, "the child of the first run", child)
	if !first.Ended() {
		t.Error("the first run did not end")
	}
	// the one that was running when it was stopped: a test that had no result is not shown as running
	if outcomes(first)["TestOne"] != "started" {
		t.Errorf("outcomes = %v", outcomes(first))
	}
	// Cancel Running Tests ends the second
	if err := r.api.Run(TestCancelCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.testsEnded()
	requireGone(t, "the second run", pid2)
	requireGone(t, "the child of the second run", child2)
	r.api.Pump()
	if r.api.MarkerCount() != 0 {
		t.Errorf("%d markers after a run that was stopped", r.api.MarkerCount())
	}
}

func TestACancelOfTheHandlersContextEndsTheProgramAndTheRun(t *testing.T) {
	hang := filepath.Join(t.TempDir(), "hang.jsonl")
	if err := os.WriteFile(hang, []byte(`{"Action":"run","Package":"example.com/ok","Test":"TestOne"}`+"\n#hang\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pidfile := filepath.Join(t.TempDir(), "pids")
	r := newRig(t, rigOptions{withGo: true, files: fixtureFiles(), env: []string{gotest.EnvTestFile + "=" + hang, gotest.EnvPidfile + "=" + pidfile}})
	r.discovered()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := profileOf(r, extension.TestKindRun).Run(ctx, []*extension.TestItem{find(t, r.ctl().Items(), "ok")}, nil, false)
		done <- err
	}()
	pid, child := pidsOf(t, pidfile)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the handler did not return after the cancel")
	}
	requireGone(t, "go test", pid)
	requireGone(t, "its child", child)
	if !r.ctl().Runs()[0].Ended() {
		t.Error("the run did not end")
	}
}

func TestTheRunEndsWithTheActivation(t *testing.T) {
	hang := filepath.Join(t.TempDir(), "hang.jsonl")
	if err := os.WriteFile(hang, []byte("#hang\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pidfile := filepath.Join(t.TempDir(), "pids")
	r := newRig(t, rigOptions{withGo: true, files: fixtureFiles(), env: []string{gotest.EnvTestFile + "=" + hang, gotest.EnvPidfile + "=" + pidfile}})
	r.discovered()
	if err := r.api.Run(TestPackageCommand, map[string]any{"uri": fileURI(filepath.Join(r.folder, "ok", "ok_test.go"))}); err != nil {
		t.Fatal(err)
	}
	pid, child := pidsOf(t, pidfile)
	r.cancel()
	r.ext.wait()
	requireGone(t, "go test", pid)
	requireGone(t, "its child", child)
}

func TestTheMarkersOfTheLastRunGoWhenTheNextOneStarts(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, files: fixtureFiles(), env: []string{gotest.EnvTestFile + "=" + fixturePath(t, "mixed.jsonl")}})
	r.discovered()
	args := map[string]any{"uri": fileURI(filepath.Join(r.folder, "mixed", "mixed_test.go"))}
	if err := r.api.Run(TestPackageCommand, args); err != nil {
		t.Fatal(err)
	}
	r.testsEnded()
	r.api.Pump()
	if r.api.MarkerCount() != 2 {
		t.Fatalf("%d markers", r.api.MarkerCount())
	}
	t.Setenv(gotest.EnvTestFile, fixturePath(t, "passing.jsonl"))
	if err := r.api.Run(TestPackageCommand, args); err != nil {
		t.Fatal(err)
	}
	r.testsEnded()
	r.api.Pump()
	if r.api.MarkerCount() != 0 {
		t.Errorf("%d markers after a run that passed", r.api.MarkerCount())
	}
}

func TestTheCoverageProfileArgumentsAndTheRaceDetector(t *testing.T) {
	tests := []struct {
		name  string
		flags []string
		want  []string
	}{
		{"count mode", nil, []string{"-coverprofile=PROFILE", "-covermode=count"}},
		{"the race detector wants atomic counters", []string{"-race"}, []string{"-race", "-coverprofile=PROFILE", "-covermode=atomic"}},
		{"a mode of the user's", []string{"-covermode=set"}, []string{"-covermode=set", "-coverprofile=PROFILE"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{withGo: true, files: fixtureFiles(), settings: map[string]any{TestFlagsSetting: tt.flags},
				env: []string{gotest.EnvTestFile + "=" + fixturePath(t, "passing.jsonl")}})
			r.discovered()
			if _, err := profileOf(r, extension.TestKindCoverage).Run(context.Background(), []*extension.TestItem{find(t, r.ctl().Items(), "ok")}, nil, false); err != nil {
				t.Fatal(err)
			}
			runs := r.goTests()
			if len(runs) != 1 {
				t.Fatalf("runs = %+v", runs)
			}
			args := slices.Clone(runs[0].Args[4:]) // after test -json -timeout 30s
			for i, a := range args {
				if strings.HasPrefix(a, "-coverprofile=") {
					if !strings.HasSuffix(a, "cover0.out") {
						t.Errorf("profile = %q", a)
					}
					args[i] = "-coverprofile=PROFILE"
				}
			}
			if len(tt.flags) > 0 && tt.flags[0] == "-covermode=set" {
				args = args[:2]
			}
			if want := tt.want; !slices.Equal(args[:len(want)], want) {
				t.Errorf("arguments = %q, want %q first", args, want)
			}
			// the fake go wrote no profile: the run says nothing about coverage and the folder of the profile is gone
			for _, e := range r.ctl().Runs()[0].Events() {
				if e.Kind == fakeapi.RunCoverage {
					t.Errorf("coverage reported without a profile: %+v", e.Coverage)
				}
			}
		})
	}
}

func TestTheDebugProfileStartsTheDebuggerOnWhatTheRequestSelects(t *testing.T) {
	file := func(r *rig) *extension.TestItem { return find(t, r.ctl().Items(), "a/a_test.go") }
	tests := []struct {
		name    string
		include func(r *rig) []*extension.TestItem
		dir     string
		args    []any
		note    string
	}{
		{"a function", func(r *rig) []*extension.TestItem { return []*extension.TestItem{find(t, file(r).Children(), "TestA")} }, "a", []any{"-test.run", "^TestA$"}, ""},
		{"a subtest", func(r *rig) []*extension.TestItem {
			return []*extension.TestItem{find(t, find(t, file(r).Children(), "TestB").Children(), "y")}
		}, "a", []any{"-test.run", "^TestB$/^y$"}, ""},
		{"a benchmark", func(r *rig) []*extension.TestItem {
			return []*extension.TestItem{find(t, file(r).Children(), "BenchmarkA")}
		}, "a", []any{"-test.bench", "^BenchmarkA$", "-test.run", "a^"}, ""},
		{"a file", func(r *rig) []*extension.TestItem { return []*extension.TestItem{file(r)} }, "a", []any{"-test.run", "^(TestA|TestB)$"}, ""},
		{"a package", func(r *rig) []*extension.TestItem { return []*extension.TestItem{find(t, r.ctl().Items(), "b")} }, "b", []any{}, ""},
		{"two packages: the first, and the user is told", func(r *rig) []*extension.TestItem {
			return []*extension.TestItem{find(t, r.ctl().Items(), "a"), find(t, r.ctl().Items(), "b")}
		}, "a", []any{}, "The debugger runs the tests of one package at a time: debugging a."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := debugRig(t, rigOptions{files: selectionFiles()})
			r.discovered()
			if _, err := profileOf(r, extension.TestKindDebug).Run(context.Background(), tt.include(r), nil, false); err != nil {
				t.Fatal(err)
			}
			starts := r.debugStarts(1)
			c := starts[0].Target.Config
			if c["mode"] != "test" || c["type"] != "go" || c["program"] != filepath.Join(r.folder, tt.dir) || !slices.Equal(c["args"].([]any), tt.args) {
				t.Errorf("configuration = %v", c)
			}
			if len(r.ctl().Runs()) != 0 {
				t.Error("a debug request made a run")
			}
			if tt.note != "" {
				if _, msg := r.lastNote(); msg != tt.note {
					t.Errorf("note = %q", msg)
				}
			}
		})
	}
}

func TestACommandWithoutTheTestingViewTellsTheUser(t *testing.T) {
	r := newRigWithoutTesting(t)
	if r.ext.tests != nil {
		t.Fatal("the controller was made")
	}
	for _, id := range []string{TestCursorCommand, TestFileCommand, TestPackageCommand, TestPreviousCommand, BenchmarkCursorCommand} {
		r.focus("sample_test.go", "§"+sampleTests)
		if err := r.api.Run(id, nil); err != nil {
			t.Fatal(err)
		}
		if _, msg := r.lastNote(); !strings.Contains(msg, "needs the Testing view") {
			t.Errorf("%s: note %q", id, msg)
		}
	}
	if err := r.api.Run(TestCancelCommand, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(TestFunctionCommand, map[string]any{"uri": fileURI(filepath.Join(r.folder, "sample_test.go")), "name": "TestAdd"}); err != nil {
		t.Fatal(err)
	}
}

// newRigWithoutTesting activates the extension in a scope that has no testing service.
func newRigWithoutTesting(t *testing.T) *rig {
	t.Helper()
	return newRig(t, rigOptions{withGo: true, noTesting: true})
}
