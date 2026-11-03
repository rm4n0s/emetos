//go:build emetos_cargo

package rust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// The controller against the real cargo (step 26.7, tag emetos_cargo): the crate testdata/tests is copied into a temporary folder and
// built there, offline, with CARGO_HOME and CARGO_TARGET_DIR in temporary folders; the crate has no dependencies. The tests are skipped
// when cargo is not installed. Run: go test -tags emetos_cargo ./extensions/rust -run Real -v

func realRig(t *testing.T, files map[string]string) *cargoRig {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is not installed")
	}
	r := newCargoRig(t, cargoOptions{real: true, files: files})
	r.ext.testDebounce = saveDebounce
	return r
}

func TestRealCargoFindsTheTestsOfEveryTarget(t *testing.T) {
	r := realRig(t, nil)
	r.found()
	if got := dump(r.ctl().Items()); got != cargoTree {
		t.Errorf("tree:\n%s\nwant:\n%s", got, cargoTree)
	}
}

func TestRealCargoRunsEveryTestAndReportsTheFailures(t *testing.T) {
	r := realRig(t, nil)
	r.found()
	run := r.run(nil, nil)
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
	msgs := messagesOf(run, "calc/lib/tests/divides")
	if len(msgs) != 1 || msgs[0].ActualOutput != "5" || msgs[0].ExpectedOutput != "4" || msgs[0].Location == nil ||
		msgs[0].Location.URI != fileURI(filepath.Join(r.folder, "src/lib.rs")) || msgs[0].Location.Range.Start.Line != 42 {
		t.Errorf("divides: %+v", msgs)
	}
	if msgs := messagesOf(run, "calc/lib/tests/thread_panics"); len(msgs) != 2 || !strings.Contains(msgs[0].Message, "worker failed") {
		t.Errorf("thread_panics: %+v", msgs)
	}
	if msgs := messagesOf(run, "calc/doc tests/divide (line 14)"); len(msgs) != 1 || msgs[0].Location == nil || msgs[0].Location.Range.Start.Line != 13 {
		t.Errorf("doc divide: %+v", msgs)
	}
	if out := outputOf(run, "calc/lib/tests/prints_and_passes"); !strings.Contains(out, "hello from a passing test") {
		t.Errorf("the output of the passing test: %q", out)
	}
}

func TestRealCargoRunsOnlyTheChosenTests(t *testing.T) {
	r := realRig(t, nil)
	r.found()
	run := r.run([]*extension.TestItem{
		find(t, r.ctl().Items(), "calc/lib/tests/adds"), find(t, r.ctl().Items(), "calc/lib/tests/nested"), find(t, r.ctl().Items(), "calc/lib/tests/later"),
	}, nil)
	got := outcomes(run)
	if len(got) != 3 || got["calc/lib/tests/adds"] != "passed" || got["calc/lib/tests/nested/deep"] != "passed" || got["calc/lib/tests/later"] != "skipped" {
		t.Errorf("reported %v", got)
	}
	// a documentation test: rustdoc's filter picks it by the name of its item
	run = r.run([]*extension.TestItem{find(t, r.ctl().Items(), "calc/doc tests/add (line 5)")}, nil)
	if got := outcomes(run); len(got) != 1 || got["calc/doc tests/add (line 5)"] != "passed" {
		t.Errorf("doc test: %v", got)
	}
}

func TestRealCargoErrorsTheTestsOfATargetThatDoesNotCompile(t *testing.T) {
	r := realRig(t, nil)
	r.found()
	broken := strings.Replace(readFileT(t, filepath.Join(r.folder, "tests/integration.rs")), "assert_eq!(calc::add(40, 2), 42);", "assert_eq!(calc::add(40, 2), missing);", 1)
	writeFile(t, r.folder, "tests/integration.rs", broken, 0o644)
	run := r.run([]*extension.TestItem{find(t, r.ctl().Items(), "calc/test integration")}, nil)
	got := outcomes(run)
	if got["calc/test integration/adds_across_the_crate"] != "errored" || got["calc/test integration/checked_divides"] != "errored" {
		t.Fatalf("reported %v", got)
	}
	msgs := messagesOf(run, "calc/test integration/adds_across_the_crate")
	if len(msgs) != 1 || !strings.Contains(msgs[0].Message, "cannot find value `missing`") || msgs[0].Location == nil || msgs[0].Location.Range.Start.Line != 2 {
		t.Errorf("messages %+v", msgs)
	}
}

// readFileT reads a file for a test.
func readFileT(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
