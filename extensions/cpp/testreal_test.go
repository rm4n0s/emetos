//go:build emetos_ctest

package cpp

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The controller against the real cmake and ctest (step 26.9, tag emetos_ctest): the project testdata/tests is copied into a temporary
// folder, configured and built there (no network: the project has no dependencies). The tests are skipped when cmake, ctest or a C and
// a C++ compiler are missing. Run: go test -tags emetos_ctest ./extensions/cpp -run Real -v

func realRig(t *testing.T) *ctestRig {
	t.Helper()
	for _, tool := range []string{"cmake", "ctest", "cc", "c++"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not installed")
		}
	}
	r := newCTestRig(t, ctestOptions{real: true})
	r.ext.testDebounce = saveDebounce
	return r
}

func TestRealCTestFindsTheTestsOfTheProject(t *testing.T) {
	r := realRig(t)
	if got := dump(r.ctl().Items()); got != ctestTree {
		t.Errorf("tree:\n%s\nwant:\n%s", got, ctestTree)
	}
	if d := find(t, r.ctl().Items(), "math.lists").Data(); d.Description != "fast, math" || d.URI != fileURI(filepath.Join(r.folder, "CMakeLists.txt")) {
		t.Errorf("item %+v", d)
	}
	if err := r.ctl().Refresh(r.t.Context()); err != nil || len(r.api.Notes()) != 0 {
		t.Errorf("refresh: %v, notes %v", err, r.api.Notes())
	}
}

func TestRealCTestRunsEveryTestAndReportsTheStates(t *testing.T) {
	r := realRig(t)
	run := r.run(nil, nil)
	want := map[string]string{"adds": "passed", "divides": "failed", "skipped": "skipped", "disabled": "skipped", "math.lists": "passed", "odd[1]+x(2)": "passed"}
	got := outcomes(run)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	msgs := messagesOf(run, "divides")
	if len(msgs) != 1 || !strings.HasPrefix(msgs[0].Message, "The test failed.") || !strings.Contains(msgs[0].Message, "expected 3 but got 2") {
		t.Errorf("divides: %+v", msgs)
	}
	if out := outputOf(run, "adds"); out != "adding 40 and 2\r\n" {
		t.Errorf("the output of adds: %q", out)
	}
	if out := outputOf(run, "skipped"); !strings.Contains(out, "no display here") {
		t.Errorf("the output of skipped: %q", out)
	}
	if out := outputOf(run, ""); !strings.Contains(out, "Test #2: divides") {
		t.Errorf("the output of the run: %q", out)
	}
}

func TestRealCTestRunsOnlyTheChosenTestsEvenWithOddNames(t *testing.T) {
	r := realRig(t)
	run := r.run(r.items("odd[1]+x(2)", "math.lists", "disabled"), nil)
	got := outcomes(run)
	if len(got) != 3 || got["odd[1]+x(2)"] != "passed" || got["math.lists"] != "passed" || got["disabled"] != "skipped" {
		t.Errorf("reported %v", got)
	}
}

func TestRealCTestGivesTheDebuggerTheCommandOfTheTest(t *testing.T) {
	r := realRig(t)
	cfgs := r.debugOf(r.items("divides"), nil)
	build := filepath.Join(r.folder, "build")
	if len(cfgs) != 1 || cfgs[0]["program"] != filepath.Join(build, "divides") || cfgs[0]["cwd"] != build {
		t.Errorf("configurations %+v", cfgs)
	}
}
