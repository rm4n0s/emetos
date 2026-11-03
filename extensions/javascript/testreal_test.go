//go:build emetos_node

package javascript

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// The controllers against the real runners (step 26.8, tag emetos_node): the projects of testdata/tests are copied into a temporary folder
// with the tools of /var/tmp/emetos-tools/node-tests linked into its node_modules (EMETOS_TOOLS names another folder), and the system's
// node runs Node's test runner, Jest and Vitest there with a HOME, TMPDIR and cache folders of the test; nothing uses the network. A test is
// skipped when node or the tools are missing. Run: go test -tags emetos_node ./extensions/javascript -run Real -v

// realRig activates the pack over a copy of the project with the real programs.
func realRig(t *testing.T, project string) *jsRig {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("npm_config_cache", filepath.Join(tmp, "npm"))
	t.Setenv("npm_config_offline", "true")
	r := newRig(t, rigOptions{project: project, noTools: true, noNode: true, systemPath: true, prepare: func(folder string) { linkTools(t, folder) }})
	r.ext.runner = runProcess
	t.Cleanup(r.ext.wait)
	jr := &jsRig{rig: r, fr: newFakeRunner(t)}
	jr.settled()
	return jr
}

func TestRealNodeTestRunnerFindsRunsAndCoversTheTests(t *testing.T) {
	r := realRig(t, "node")
	if got := treeDump(r.ctl(RunnerNode).Items()); got != nodeTree {
		t.Errorf("tree:\n%s\nwant:\n%s", got, nodeTree)
	}
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
	msgs := runMessages(run, "test|math.test.js/math/divides")
	if len(msgs) != 1 || msgs[0].ExpectedOutput != "4" || msgs[0].ActualOutput != "5" || msgs[0].Location == nil || msgs[0].Location.Range.Start.Line != 10 ||
		msgs[0].Location.URI != fileURI(filepath.Join(r.folder, "test/math.test.js")) {
		t.Errorf("divides: %+v", msgs)
	}
	if boom := runMessages(run, "test|other.test.js/other crashes"); len(boom) != 1 || boom[0].Message != "boom" || boom[0].Location == nil || boom[0].Location.Range.Start.Line != 8 {
		t.Errorf("boom: %+v", boom)
	}
	if out := runOutput(run, ""); !strings.Contains(out, "hello from a passing test") {
		t.Errorf("output %q", out)
	}

	// chosen tests: a test and a suite by their names
	run = r.run(RunnerNode, r.items(RunnerNode, "test|math.test.js/math/adds", "test|math.test.js/math/nested"), nil)
	if got := runOutcomes(run); len(got) != 2 || got["test|math.test.js/math/adds"] != "passed" || got["test|math.test.js/math/nested/throws on zero"] != "passed" {
		t.Errorf("chosen tests reported %v", got)
	}

	// coverage: the source file with its lines, its functions and its branches; the test files are not reported
	run = r.runWith(RunnerNode, extension.TestKindCoverage, r.items(RunnerNode, "test|math.test.js"), nil)
	files := runCoverage(run)
	if len(files) != 1 || files[0].URI != fileURI(filepath.Join(r.folder, "src/math.js")) {
		t.Fatalf("coverage %+v", files)
	}
	if f := files[0]; f.Statement.Total != 16 || f.Statement.Covered != 13 || f.Declaration == nil || f.Declaration.Total != 3 || f.Declaration.Covered != 2 {
		t.Errorf("counts %+v %+v", f.Statement, f.Declaration)
	}
}

func TestRealNodeTestRunnerErrorsTheTestsOfAFileThatDoesNotLoad(t *testing.T) {
	r := realRig(t, "node")
	writeFile(t, r.folder, "test/other.test.js", "const test = require('node:test');\ntest('never', () => {});\n", 0o644)
	r.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: fileURI(filepath.Join(r.folder, "test/other.test.js"))})
	r.api.WaitFor("the tests again", func() bool { return strings.Contains(treeDump(r.ctl(RunnerNode).Items()), "never @2") })
	r.settled()
	writeFile(t, r.folder, "test/other.test.js", "const test = require('node:test');\ntest('never' {\n", 0o644)
	run := r.run(RunnerNode, r.items(RunnerNode, "test|other.test.js"), nil)
	msgs := runMessages(run, "test|other.test.js/never")
	if got := runOutcomes(run)["test|other.test.js/never"]; got != "errored" || len(msgs) != 1 || !strings.Contains(msgs[0].Message, "SyntaxError") {
		t.Errorf("outcome %q, messages %+v", got, msgs)
	}
}

func TestRealJestFindsRunsAndCoversTheTests(t *testing.T) {
	r := realRig(t, "jest")
	if got := treeDump(r.ctl(RunnerJest).Items()); got != jestTree {
		t.Errorf("tree:\n%s\nwant:\n%s", got, jestTree)
	}
	run := r.run(RunnerJest, nil, nil)
	want := map[string]string{
		"src|math.test.js/math/adds": "passed", "src|math.test.js/math/divides": "failed", "src|math.test.js/math/later": "skipped",
		"src|math.test.js/math/nested/throws on zero": "passed", "src|math.test.js/math/adds 1 and 1": "passed", "src|math.test.js/math/adds 2 and 3": "passed",
		"src|math.test.js/prints and passes": "passed", "src|other.test.js/other passes": "passed", "src|other.test.js/other crashes": "failed",
	}
	got := runOutcomes(run)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	msgs := runMessages(run, "src|math.test.js/math/divides")
	if len(msgs) != 1 || msgs[0].ExpectedOutput != "4" || msgs[0].ActualOutput != "5" || msgs[0].Location == nil || msgs[0].Location.Range.Start.Line != 8 {
		t.Errorf("divides: %+v", msgs)
	}
	if out := runOutput(run, ""); !strings.Contains(out, "hello from a passing test") {
		t.Errorf("output %q", out)
	}

	run = r.run(RunnerJest, r.items(RunnerJest, "src|math.test.js/math/nested", "src|other.test.js/other passes"), nil)
	if got := runOutcomes(run); len(got) != 2 || got["src|math.test.js/math/nested/throws on zero"] != "passed" || got["src|other.test.js/other passes"] != "passed" {
		t.Errorf("chosen tests reported %v", got)
	}

	run = r.runWith(RunnerJest, extension.TestKindCoverage, nil, nil)
	files := runCoverage(run)
	if len(files) != 1 || files[0].URI != fileURI(filepath.Join(r.folder, "src/math.js")) {
		t.Fatalf("coverage %+v", files)
	}
	if f := files[0]; f.Statement.Total != 6 || f.Statement.Covered != 5 || f.Declaration == nil || f.Declaration.Covered != 2 || f.Branch == nil {
		t.Errorf("counts %+v %+v %+v", f.Statement, f.Declaration, f.Branch)
	}
}

func TestRealJestErrorsTheTestsOfAFileThatDoesNotParse(t *testing.T) {
	r := realRig(t, "jest")
	writeFile(t, r.folder, "src/other.test.js", "test('never' {\n", 0o644)
	run := r.run(RunnerJest, r.items(RunnerJest, "src|other.test.js"), nil)
	got := runOutcomes(run)
	if got["src|other.test.js/other passes"] != "errored" {
		t.Fatalf("reported %v", got)
	}
	if msgs := runMessages(run, "src|other.test.js/other passes"); len(msgs) != 1 || !strings.Contains(msgs[0].Message, "Test suite failed to run") {
		t.Errorf("messages %+v", msgs)
	}
}

// realVitest skips the test when Vitest cannot start: vite, which Vitest needs, is a peer package that the install of the tools may lack.
func realVitest(t *testing.T) *jsRig {
	t.Helper()
	if _, err := os.Stat(filepath.Join(toolsModules(), "vite")); err != nil {
		t.Skip("vite is not installed next to vitest (npm install --prefix node-tests vite): vitest cannot start")
	}
	return realRig(t, "vitest")
}

func TestRealVitestFindsRunsAndCoversTheTypeScriptTests(t *testing.T) {
	r := realVitest(t)
	if got := treeDump(r.ctl(RunnerVitest).Items()); got != vitestTree {
		t.Errorf("tree:\n%s\nwant:\n%s", got, vitestTree)
	}
	run := r.run(RunnerVitest, nil, nil)
	want := map[string]string{
		"src|math.test.ts/math/adds": "passed", "src|math.test.ts/math/divides": "failed", "src|math.test.ts/math/later": "skipped",
		"src|math.test.ts/math/nested/throws on zero": "passed", "src|math.test.ts/math/adds 1 and 1": "passed", "src|math.test.ts/math/adds 2 and 3": "passed",
		"src|math.test.ts/prints and passes": "passed", "src|other.test.ts/other passes": "passed", "src|other.test.ts/other crashes": "failed",
	}
	got := runOutcomes(run)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	if msgs := runMessages(run, "src|math.test.ts/math/divides"); len(msgs) != 1 || msgs[0].Location == nil || msgs[0].Location.Range.Start.Line != 9 {
		t.Errorf("divides: %+v", msgs)
	}
	run = r.run(RunnerVitest, r.items(RunnerVitest, "src|math.test.ts/math/nested"), nil)
	if got := runOutcomes(run); len(got) != 1 || got["src|math.test.ts/math/nested/throws on zero"] != "passed" {
		t.Errorf("chosen tests reported %v", got)
	}
	run = r.runWith(RunnerVitest, extension.TestKindCoverage, nil, nil)
	files := runCoverage(run)
	if len(files) != 1 || files[0].URI != fileURI(filepath.Join(r.folder, "src/math.ts")) || files[0].Statement.Covered >= files[0].Statement.Total {
		t.Errorf("coverage %+v", files)
	}
}

func TestRealNodeTestRunnerRunsTypeScriptFiles(t *testing.T) {
	r := realRig(t, "node-ts")
	want := "test|shapes.test.ts\n  shapes @5\n    computes an area @6\n    describes a small square @10\n    describes a large square @14\n    draws a square @18\n"
	if got := treeDump(r.ctl(RunnerNode).Items()); got != want {
		t.Errorf("tree:\n%s\nwant:\n%s", got, want)
	}
	run := r.runWith(RunnerNode, extension.TestKindCoverage, nil, nil)
	got := runOutcomes(run)
	for k, v := range map[string]string{
		"test|shapes.test.ts/shapes/computes an area": "passed", "test|shapes.test.ts/shapes/describes a small square": "passed",
		"test|shapes.test.ts/shapes/describes a large square": "failed", "test|shapes.test.ts/shapes/draws a square": "skipped",
	} {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	msgs := runMessages(run, "test|shapes.test.ts/shapes/describes a large square")
	if len(msgs) != 1 || msgs[0].ExpectedOutput != "a big square" || msgs[0].ActualOutput != "a large square" || msgs[0].Location == nil ||
		msgs[0].Location.URI != fileURI(filepath.Join(r.folder, "test/shapes.test.ts")) || msgs[0].Location.Range.Start.Line != 14 {
		t.Errorf("messages %+v", msgs)
	}
	files := runCoverage(run)
	if len(files) != 1 || files[0].URI != fileURI(filepath.Join(r.folder, "src/shapes.ts")) || files[0].Statement.Covered >= files[0].Statement.Total {
		t.Errorf("coverage %+v", files)
	}
}
