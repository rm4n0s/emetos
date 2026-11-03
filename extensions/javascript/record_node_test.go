//go:build emetos_node

package javascript

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Recording of the fixtures of testdata/recorded (run by hand: EMETOS_RECORD=1 go test -tags emetos_node ./extensions/javascript -run
// TestRecord): the output of the real runners on the projects of testdata/tests, with the temporary folder replaced by {{dir}}.

func recordRun(t *testing.T, dir string, args ...string) (stdout, stderr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	tmp := t.TempDir()
	cmd.Env = append(os.Environ(), "HOME="+tmp, "TMPDIR="+tmp, "XDG_CACHE_HOME="+tmp)
	var so, se strings.Builder
	cmd.Stdout, cmd.Stderr = &so, &se
	_ = cmd.Run() // a failing test exits with 1
	return so.String(), se.String()
}

func saveRecorded(t *testing.T, dir, name, text string) {
	t.Helper()
	text = strings.ReplaceAll(text, "../../../../"+strings.TrimPrefix(toolsModules(), "/"), dir+"/node_modules")
	text = strings.ReplaceAll(text, toolsModules(), dir+"/node_modules")
	text = strings.ReplaceAll(text, dir, "{{dir}}")
	p := filepath.Join("testdata", "recorded", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRecord(t *testing.T) {
	if os.Getenv("EMETOS_RECORD") == "" {
		t.Skip("set EMETOS_RECORD=1 to record")
	}
	node := recordProject(t, "node")
	out, _ := recordRun(t, node, "node", "--test", "--test-reporter=tap", "test/math.test.js")
	saveRecorded(t, node, "node_math.tap", out)
	out, errs := recordRun(t, node, "node", "--test", "--test-reporter=tap", "test/other.test.js")
	saveRecorded(t, node, "node_other.tap", out+errs)
	lcov := filepath.Join(t.TempDir(), "l.info")
	out, _ = recordRun(t, node, "node", "--test", "--test-reporter=tap", "--test-reporter-destination=stdout", "--experimental-test-coverage", "--test-reporter=lcov", "--test-reporter-destination="+lcov, "test/math.test.js")
	saveRecorded(t, node, "node_coverage.tap", out)
	if b, err := os.ReadFile(lcov); err == nil {
		saveRecorded(t, node, "node_coverage.lcov", string(b))
	}
	out, _ = recordRun(t, node, "node", "--test", "--test-reporter=tap", "--test-name-pattern=^math adds$", "--test-name-pattern=^math nested( |$)", "test/math.test.js")
	saveRecorded(t, node, "node_filtered.tap", out)
	if err := os.WriteFile(filepath.Join(node, "test", "broken.test.js"), []byte("const { test } = require('node:test');\ntest('never' {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errs = recordRun(t, node, "node", "--test", "--test-reporter=tap", "test/broken.test.js")
	saveRecorded(t, node, "node_broken.tap", out+"\n=====STDERR=====\n"+errs)

	jest := recordProject(t, "jest")
	out, _ = recordRun(t, jest, "node_modules/.bin/jest", "--listTests")
	saveRecorded(t, jest, "jest_list.txt", out)
	rep, cov := filepath.Join(t.TempDir(), "r.json"), filepath.Join(t.TempDir(), "cov")
	out, errs = recordRun(t, jest, "node_modules/.bin/jest", "--json", "--outputFile="+rep, "--testLocationInResults", "--coverage", "--coverageReporters=json", "--coverageDirectory="+cov)
	saveRecorded(t, jest, "jest_run.txt", out+errs)
	if b, err := os.ReadFile(rep); err == nil {
		saveRecorded(t, jest, "jest_report.json", string(b))
	}
	if b, err := os.ReadFile(filepath.Join(cov, "coverage-final.json")); err == nil {
		saveRecorded(t, jest, "jest_coverage.json", string(b))
	}
	if err := os.WriteFile(filepath.Join(jest, "src", "broken.test.js"), []byte("test('never' {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep = filepath.Join(t.TempDir(), "r2.json")
	out, errs = recordRun(t, jest, "node_modules/.bin/jest", "--json", "--outputFile="+rep, "--testLocationInResults", "--runTestsByPath", "src/broken.test.js")
	saveRecorded(t, jest, "jest_broken_run.txt", out+errs)
	if b, err := os.ReadFile(rep); err == nil {
		saveRecorded(t, jest, "jest_broken_report.json", string(b))
	}
}

// recordProject copies a project of testdata/tests into a temporary folder with the tools linked in.
func recordProject(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	toolProject(t, name, dir)
	return dir
}

// TestRecordVitest records the output of the real Vitest on the project of testdata/tests/vitest.
func TestRecordVitest(t *testing.T) {
	if os.Getenv("EMETOS_RECORD") == "" {
		t.Skip("set EMETOS_RECORD=1 to record")
	}
	dir := recordProject(t, "vitest")
	out, _ := recordRun(t, dir, "node_modules/.bin/vitest", "list", "--filesOnly", "--json")
	saveRecorded(t, dir, "vitest_list.json", out)
	tmp := t.TempDir()
	rep, cov := filepath.Join(tmp, "r.json"), filepath.Join(tmp, "cov")
	_, _ = recordRun(t, dir, "node_modules/.bin/vitest", "run", "--reporter=default", "--reporter=json", "--outputFile.json="+rep, "--coverage.enabled", "--coverage.reportOnFailure", "--coverage.reporter=json", "--coverage.reportsDirectory="+cov)
	if b, err := os.ReadFile(rep); err == nil {
		saveRecorded(t, dir, "vitest_report.json", string(b))
	}
	if b, err := os.ReadFile(filepath.Join(cov, "coverage-final.json")); err == nil {
		saveRecorded(t, dir, "vitest_coverage.json", string(b))
	}
}
