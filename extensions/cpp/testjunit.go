package cpp

import (
	"encoding/xml"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// outcome is how a test of a CTest run ended.
type outcome int

const (
	outcomePassed outcome = iota
	outcomeFailed
	// outcomeSkipped is a test that did not run on purpose: disabled, or skipped by SKIP_RETURN_CODE or SKIP_REGULAR_EXPRESSION.
	outcomeSkipped
	// outcomeNotRun is a test CTest could not run (its program was not found, a required file was missing).
	outcomeNotRun
)

// junitResult is the result of a test in the JUnit file of a run.
type junitResult struct {
	Name     string
	Outcome  outcome
	Duration time.Duration
	// Message is the reason CTest gives ("Failed", "Timeout", "SEGFAULT", "SKIP_RETURN_CODE=77", "Unable to find executable"); Output is
	// what the test printed.
	Message string
	Output  string
}

// junitSuite is the part of the file that is read.
type junitSuite struct {
	XMLName xml.Name `xml:"testsuite"`
	Cases   []struct {
		Name     string  `xml:"name,attr"`
		Time     float64 `xml:"time,attr"`
		Status   string  `xml:"status,attr"`
		Failure  *reason `xml:"failure"`
		Error    *reason `xml:"error"`
		Skipped  *reason `xml:"skipped"`
		Output   string  `xml:"system-out"`
		ErrorOut string  `xml:"system-err"`
	} `xml:"testcase"`
}

// reason is an element that says why a test did not pass.
type reason struct {
	Message string `xml:"message,attr"`
}

// readJUnit reads the file `ctest --output-junit` wrote. It fails with CppJUnitInvalid when the file is not CTest's report.
//
// A test that CTest ran and that passed has the status "run" and no element; a failure (a status other than 0, a timeout, a signal) has
// a failure element; one that CTest did not run is "notrun" with a skipped element, which is a skip when SKIP_RETURN_CODE or
// SKIP_REGULAR_EXPRESSION made it (the message starts with SKIP_) and otherwise a test that could not be run; a disabled test has the
// status "disabled".
func readJUnit(data []byte) ([]junitResult, error) {
	var suite junitSuite
	if err := xml.Unmarshal(data, &suite); err != nil {
		return nil, errors.NewErr("CppJUnitInvalid", err)
	}
	out := make([]junitResult, 0, len(suite.Cases))
	for _, c := range suite.Cases {
		res := junitResult{Name: c.Name, Output: c.Output + c.ErrorOut, Duration: time.Duration(c.Time * float64(time.Second))}
		switch {
		case c.Status == "disabled":
			res.Outcome, res.Message, res.Output = outcomeSkipped, "Disabled", ""
		case c.Failure != nil:
			res.Outcome, res.Message = outcomeFailed, c.Failure.Message
		case c.Error != nil:
			res.Outcome, res.Message = outcomeFailed, c.Error.Message
		case c.Skipped != nil || c.Status == "notrun":
			if c.Skipped != nil {
				res.Message = c.Skipped.Message
			}
			res.Outcome = outcomeNotRun
			if strings.HasPrefix(res.Message, "SKIP_") {
				res.Outcome = outcomeSkipped
			}
		}
		out = append(out, res)
	}
	return out, nil
}
