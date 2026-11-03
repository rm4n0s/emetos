package javascript

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// byPath finds the result with the titles joined by " > ".
func byPath(t *testing.T, rep report, file, path string) testResult {
	t.Helper()
	for _, r := range rep.Results {
		if strings.HasSuffix(r.File, file) && strings.Join(r.Path, " > ") == path {
			return r
		}
	}
	t.Fatalf("no result %q of %s in %d results", path, file, len(rep.Results))
	return testResult{}
}

func TestParseJestReportReadsTheRecordedRun(t *testing.T) {
	rep, err := parseJestReport([]byte(recorded(t, "jest_report.json", "/p")))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 9 || len(rep.FileErrors) != 0 {
		t.Fatalf("%d results, %d file errors", len(rep.Results), len(rep.FileErrors))
	}
	for _, tc := range []struct {
		file, path, state string
	}{
		{"math.test.js", "math > adds", statePassed},
		{"math.test.js", "math > divides", stateFailed},
		{"math.test.js", "math > later", stateSkipped},
		{"math.test.js", "math > nested > throws on zero", statePassed},
		{"math.test.js", "math > adds 1 and 1", statePassed},
		{"math.test.js", "math > adds 2 and 3", statePassed},
		{"math.test.js", "prints and passes", statePassed},
		{"other.test.js", "other passes", statePassed},
		{"other.test.js", "other crashes", stateFailed},
	} {
		if got := byPath(t, rep, tc.file, tc.path); got.State != tc.state {
			t.Errorf("%s %s: %s, want %s", tc.file, tc.path, got.State, tc.state)
		}
	}
	div := byPath(t, rep, "math.test.js", "math > divides")
	if len(div.Failures) != 1 || div.Failures[0].Expected != "4" || div.Failures[0].Actual != "5" || div.Line != 8 ||
		!strings.HasPrefix(div.Failures[0].Text, "expect(received).toBe(expected) // Object.is equality") || !strings.Contains(div.Failures[0].Text, "Received: 5") {
		t.Errorf("divides: %+v (line %d)", div.Failures, div.Line)
	}
	if len(div.Failures[0].Frames) == 0 || !strings.HasSuffix(div.Failures[0].Frames[0].File, "src/math.test.js") || div.Failures[0].Frames[0].Line != 9 || div.Failures[0].Frames[0].Col != 27 {
		t.Errorf("frames %+v", div.Failures[0].Frames)
	}
	crash := byPath(t, rep, "other.test.js", "other crashes")
	if len(crash.Failures) != 1 || crash.Failures[0].Text != "boom" || crash.Failures[0].Expected != "" || crash.Failures[0].Frames[0].Line != 6 {
		t.Errorf("crash: %+v", crash.Failures)
	}
	if later := byPath(t, rep, "math.test.js", "math > later"); later.Duration != extension.TestNoDuration {
		t.Errorf("a skipped test has the duration %v", later.Duration)
	}
	if adds := byPath(t, rep, "math.test.js", "math > adds"); adds.Duration <= 0 || adds.Duration > time.Minute {
		t.Errorf("duration %v", adds.Duration)
	}
}

func TestParseJestReportReadsAFileThatCouldNotRun(t *testing.T) {
	rep, err := parseJestReport([]byte(recorded(t, "jest_broken_report.json", "/p")))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 0 || len(rep.FileErrors) != 1 || !strings.HasSuffix(rep.FileErrors[0].File, "src/broken.test.js") ||
		!strings.Contains(rep.FileErrors[0].Message, "Test suite failed to run") {
		t.Errorf("%+v", rep)
	}
}

func TestParseJestReportReadsVitestsReport(t *testing.T) {
	rep, err := parseJestReport([]byte(recorded(t, "vitest_report.json", "/p")))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 9 {
		t.Fatalf("%d results", len(rep.Results))
	}
	later := byPath(t, rep, "math.test.ts", "math > later")
	div := byPath(t, rep, "math.test.ts", "math > divides")
	if later.State != stateSkipped || div.State != stateFailed || div.Line != 0 || div.Duration <= 0 || div.Duration > time.Minute {
		t.Errorf("later %+v, divides %+v", later, div)
	}
	if f := div.Failures[0]; f.Text != "AssertionError: expected 5 to be 4 // Object.is equality" || len(f.Frames) < 2 ||
		f.Frames[0].File != "/p/src/math.test.ts" || f.Frames[0].Line != 10 || f.Frames[0].Col != 27 || !strings.Contains(f.Frames[1].File, "/node_modules/vitest/") {
		t.Errorf("failure %+v", f)
	}
}

func TestParseJestReportRefusesWhatIsNoReport(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"empty", ""}, {"text", "Test Suites: 1 passed"}, {"truncated", `{"testResults":[{"name":`}, {"no results", `{"success":true}`}, {"an array", `[]`},
	} {
		_, err := parseJestReport([]byte(tc.text))
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Errorf("%s: expected *errors.Error, got %T", tc.name, err)
			continue
		}
		if !appErr.HasRoute("parseJestReport.JsTestReportInvalid") {
			t.Errorf("%s: unexpected failure path: %s", tc.name, appErr.Route())
		}
	}
}

func TestFailureOfSplitsTheTextFromTheStack(t *testing.T) {
	for _, tc := range []struct {
		name, msg, text string
		frames          []frame
	}{
		{"an error", "Error: boom\n    at Object.<anonymous> (/p/a.test.js:6:9)\n    at node:internal/x:1:1",
			"boom", []frame{{Func: "Object.<anonymous>", File: "/p/a.test.js", Line: 6, Col: 9}}},
		{"colors", "\x1b[31mError: bad\x1b[39m\n    at /p/a.js:1:2", "bad", []frame{{File: "/p/a.js", Line: 1, Col: 2}}},
		{"a file URL", "AssertionError: x\n    at file:///p/b.mjs:3:4", "AssertionError: x", []frame{{File: "/p/b.mjs", Line: 3, Col: 4}}},
		{"no stack", "just text\nmore", "just text\nmore", nil},
		{"empty", "", "The test failed.", nil},
	} {
		f := failureOf(tc.msg)
		if f.Text != tc.text || len(f.Frames) != len(tc.frames) {
			t.Errorf("%s: %+v", tc.name, f)
			continue
		}
		for i, fr := range tc.frames {
			if f.Frames[i] != fr {
				t.Errorf("%s: frame %d = %+v, want %+v", tc.name, i, f.Frames[i], fr)
			}
		}
	}
}

func TestSidesOnlyOfComparisons(t *testing.T) {
	for _, tc := range []struct{ name, detail, exp, act string }{
		{"numbers", `{"matcherResult":{"name":"toBe","expected":4,"actual":5}}`, "4", "5"},
		{"strings", `{"matcherResult":{"name":"toEqual","expected":"a b","actual":"a c"}}`, "a b", "a c"},
		{"objects", `{"matcherResult":{"name":"toStrictEqual","expected":{"a":1},"actual":{"a":2}}}`, `{"a":1}`, `{"a":2}`},
		{"not a comparison", `{"matcherResult":{"name":"toBeTruthy","expected":true,"actual":false}}`, "", ""},
		{"no actual", `{"matcherResult":{"name":"toBe","expected":4}}`, "", ""},
		{"no matcher", `{}`, "", ""},
		{"not an object", `"x"`, "", ""},
	} {
		e, a := sides([]byte(tc.detail))
		if e != tc.exp || a != tc.act {
			t.Errorf("%s: %q %q", tc.name, e, a)
		}
	}
}
