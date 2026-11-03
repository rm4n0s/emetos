package javascript

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The reports of the runners (SPEC §10.32 "The providers", JavaScript and TypeScript; "As built (step 26.8)"): the JSON of Jest
// (`--json --outputFile`) and of Vitest (`--reporter=json`, which has the same shape), and what both have in common with Node's TAP
// (testtap.go): a result is a test of a file, named by the titles of its suites and its own, with a state, a duration and the failures.

// The states of a result.
const (
	statePassed  = "passed"
	stateFailed  = "failed"
	stateSkipped = "skipped"
	stateErrored = "errored"
)

// frame is a line of a stack trace.
type frame struct {
	Func, File string
	Line, Col  int
}

// failure is one reason a test failed: the text, the two values of a failed comparison and the frames of the stack.
type failure struct {
	Text             string
	Expected, Actual string
	Frames           []frame
}

// testResult is what a run reported about a test.
type testResult struct {
	// File is the absolute path of the test file; Path the titles of the suites and the test.
	File string
	Path []string
	// State is one of the state constants, Duration the time it took (extension.TestNoDuration when unknown) and Line the place of the
	// test the runner gave (from 1, 0 when it gave none).
	State    string
	Duration time.Duration
	Line     int
	Failures []failure
}

// fileError is a file the runner could not run (a syntax error, a missing import): its message.
type fileError struct {
	File, Message string
}

// report is what a run reported.
type report struct {
	Results    []testResult
	FileErrors []fileError
}

// jestReport is the JSON of Jest and Vitest.
type jestReport struct {
	TestResults []jestFile `json:"testResults"`
}

type jestFile struct {
	Name             string          `json:"name"`
	Status           string          `json:"status"`
	Message          string          `json:"message"`
	AssertionResults []jestAssertion `json:"assertionResults"`
}

type jestAssertion struct {
	AncestorTitles  []string          `json:"ancestorTitles"`
	Title           string            `json:"title"`
	Status          string            `json:"status"`
	Duration        *float64          `json:"duration"`
	Location        *jestLocation     `json:"location"`
	FailureMessages []string          `json:"failureMessages"`
	FailureDetails  []json.RawMessage `json:"failureDetails"`
}

type jestLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// matcherDetail is the part of a failure detail of Jest that has the two sides of a comparison.
type matcherDetail struct {
	MatcherResult *struct {
		Expected json.RawMessage `json:"expected"`
		Actual   json.RawMessage `json:"actual"`
		Name     string          `json:"name"`
	} `json:"matcherResult"`
}

// parseJestReport reads the JSON report of Jest or Vitest. It fails with JsTestReportInvalid for text that is not one.
func parseJestReport(data []byte) (report, error) {
	var rep jestReport
	data = bytes.TrimSpace(data)
	if err := json.Unmarshal(data, &rep); err != nil {
		return report{}, errors.NewErr("JsTestReportInvalid", err)
	}
	if rep.TestResults == nil {
		return report{}, errors.New("JsTestReportInvalid", "the report has no testResults")
	}
	var out report
	for _, f := range rep.TestResults {
		if len(f.AssertionResults) == 0 && f.Status == "failed" {
			out.FileErrors = append(out.FileErrors, fileError{File: f.Name, Message: f.Message})
		}
		for _, a := range f.AssertionResults {
			r := testResult{File: f.Name, Path: append(append([]string(nil), a.AncestorTitles...), a.Title), Duration: extension.TestNoDuration}
			if a.Duration != nil {
				r.Duration = time.Duration(*a.Duration * float64(time.Millisecond))
			}
			if a.Location != nil {
				r.Line = a.Location.Line
			}
			switch a.Status {
			case "passed":
				r.State = statePassed
			case "failed":
				r.State = stateFailed
			default: // pending, skipped, todo, disabled
				r.State = stateSkipped
			}
			for i, m := range a.FailureMessages {
				fl := failureOf(m)
				if i < len(a.FailureDetails) {
					fl.Expected, fl.Actual = sides(a.FailureDetails[i])
				}
				r.Failures = append(r.Failures, fl)
			}
			if r.State == stateFailed && len(r.Failures) == 0 {
				r.Failures = []failure{{Text: "The test failed."}}
			}
			out.Results = append(out.Results, r)
		}
	}
	return out, nil
}

// sides are the two values of a failed comparison that Jest keeps in the detail of a failure, as text; empty when there are none.
func sides(detail json.RawMessage) (expected, actual string) {
	var d matcherDetail
	if json.Unmarshal(detail, &d) != nil || d.MatcherResult == nil || !equalityMatchers[d.MatcherResult.Name] {
		return "", ""
	}
	e, a := valueText(d.MatcherResult.Expected), valueText(d.MatcherResult.Actual)
	if e == "" || a == "" {
		return "", ""
	}
	return e, a
}

// equalityMatchers are the matchers whose expected and actual values are the two sides of a comparison.
var equalityMatchers = map[string]bool{
	"toBe": true, "toEqual": true, "toStrictEqual": true, "toMatchObject": true, "toHaveLength": true, "toBeCloseTo": true,
	"toHaveProperty": true, "toHaveBeenCalledTimes": true, "toBeGreaterThan": true, "toBeLessThan": true,
}

// valueText writes a JSON value as the text a diff shows: a string as it is, anything else as compact JSON; "" for a missing value.
func valueText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if raw[0] == '"' && json.Unmarshal(raw, &s) == nil {
		return s
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return string(raw)
	}
	return buf.String()
}

var (
	ansiRe     = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	frameRe    = regexp.MustCompile(`^\s*(?:at\s+)?(?:(.+?)\s+\()?((?:[a-zA-Z]:)?[^\s()]+?):(\d+):(\d+)\)?\s*$`)
	frameLikeR = regexp.MustCompile(`^\s+at\s`)
)

// stripANSI takes the color sequences out of a text.
func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// failureOf splits the message of a failure into its text and the frames of its stack: the text ends at the first line that is a frame.
func failureOf(msg string) failure {
	msg = stripANSI(msg)
	lines := strings.Split(strings.ReplaceAll(msg, "\r\n", "\n"), "\n")
	cut := len(lines)
	for i, l := range lines {
		if frameLikeR.MatchString(l) {
			cut = i
			break
		}
	}
	text := strings.TrimSpace(strings.Join(lines[:cut], "\n"))
	text = strings.TrimPrefix(text, "Error: ")
	if text == "" {
		text = "The test failed."
	}
	return failure{Text: text, Frames: parseFrames(lines[cut:])}
}

// parseFrames reads the lines of a stack trace; lines that are not frames, and frames of Node's own files, are left out.
func parseFrames(lines []string) []frame {
	var out []frame
	for _, l := range lines {
		m := frameRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		file := strings.TrimPrefix(m[2], "file://")
		if strings.HasPrefix(file, "node:") || strings.HasPrefix(file, "internal/") {
			continue
		}
		line, _ := strconv.Atoi(m[3])
		col, _ := strconv.Atoi(m[4])
		out = append(out, frame{Func: m[1], File: file, Line: line, Col: col})
	}
	return out
}
