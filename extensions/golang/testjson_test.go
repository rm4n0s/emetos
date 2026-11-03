package golang

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// The events of `go test -json` (step 14.3; the runs that apply them are in testexec_test.go). The fixtures in testdata/testjson are the output of the real go test (Go 1.27) for small
// packages: mixed (a pass, a failure, a skip, a table of subtests with a failure), panic (a test that panics), build (a package whose
// test does not build; the build events of Go 1.24 and later), and by hand build_legacy (the same failure as older Go printed it), nofiles
// and passing.

// fixture reads a file of testdata/testjson with {{DIR}} replaced by the folder of the package.
func fixture(t *testing.T, name, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "testjson", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(strings.ReplaceAll(string(data), "{{DIR}}", dir)), "\n")
}

func TestOutputThatIsTooLongKeepsTheLastLines(t *testing.T) {
	var out []string
	for i := 0; i < maxTestOutputLines+50; i++ {
		out = addOutput(out, "line "+strconv.Itoa(i)+"\n")
	}
	if len(out) != maxTestOutputLines || out[0] != "line 50" || out[len(out)-1] != "line "+strconv.Itoa(maxTestOutputLines+49) {
		t.Errorf("%d lines, first %q last %q", len(out), out[0], out[len(out)-1])
	}
}

func TestAddOutputDropsTheMarkersOfGoTestAndKeepsTheLines(t *testing.T) {
	got := addOutput(nil, "=== RUN   TestA\n    a_test.go:3: boom\r\n=== NAME  TestA\n--- FAIL: TestA (0.00s)\n")
	want := []string{"    a_test.go:3: boom", "--- FAIL: TestA (0.00s)"}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestTheImportPathOfABuildEventLosesItsTestSuffix(t *testing.T) {
	for in, want := range map[string]string{"example.com/p [example.com/p.test]": "example.com/p", "example.com/p": "example.com/p", "": ""} {
		if got := stripImportPath(in); got != want {
			t.Errorf("stripImportPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnEventThatIsNotJSONFailsWithItsTag(t *testing.T) {
	for _, line := range []string{"plain text", "{broken", `{"Test":"TestX"}`} {
		_, err := parseTestEvent(line)
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("%q: expected *errors.Error, got %T", line, err)
		}
		if !appErr.HasRoute("parseTestEvent.TestEventInvalid") {
			t.Errorf("%q: unexpected failure path: %s", line, appErr.Route())
		}
	}
	ev, err := parseTestEvent(`{"Action":"pass","Package":"p","Test":"TestX","Elapsed":0.5,"OutputType":"frame"}`)
	if err != nil || ev.Action != "pass" || ev.Test != "TestX" || ev.Elapsed != 0.5 {
		t.Errorf("event = %+v, %v", ev, err)
	}
}

// outputs are the lines the events of a run printed, by "package" for the package's own and "package:test" for a test's, with the keys in
// the order they first appeared.
type outputs struct {
	keys  []string
	lines map[string][]string
}

// outputsOf reads the lines of a fixture the way the run does.
func outputsOf(fixtureLines []string) outputs {
	o := outputs{lines: map[string][]string{}}
	for _, l := range fixtureLines {
		ev, err := parseTestEvent(l)
		if err != nil || (ev.Action != "output" && ev.Action != "build-output") {
			continue
		}
		path := ev.Package
		if path == "" {
			path = stripImportPath(ev.ImportPath)
		}
		key := path
		if ev.Test != "" {
			key += ":" + ev.Test
		}
		if _, seen := o.lines[key]; !seen {
			o.keys = append(o.keys, key)
		}
		o.lines[key] = addOutput(o.lines[key], ev.Output)
	}
	return o
}
