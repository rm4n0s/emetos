package testing

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
)

// FormatDuration is how a duration is written after a test: 0.0 to 9.9 ms with a decimal, up to a second in whole ms, then seconds with
// a decimal.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts (formatDuration)
func FormatDuration(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)
	switch {
	case ms < 10:
		return fmt.Sprintf("%.1fms", ms)
	case ms < 1000:
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.1fs", ms/1000)
}

// Summary counts the tests of the results the summary line reports on.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/common/testingProgressMessages.ts (collectTestStateCounts)
type Summary struct {
	Running                  bool
	Passed, Failed, Skipped  int
	RunSoFar, TotalWillBeRun int
	Queued, RunningTests     int
}

// Collect counts the states of the results: failed and errored tests are failed.
func Collect(running bool, results []*coretesting.Result) Summary {
	var s Summary
	s.Running = running
	var queued, run int
	for _, r := range results {
		c := r.Counts()
		s.Failed += c[coretesting.StateErrored] + c[coretesting.StateFailed]
		s.Passed += c[coretesting.StatePassed]
		s.Skipped += c[coretesting.StateSkipped]
		run += c[coretesting.StateRunning]
		queued += c[coretesting.StateQueued]
	}
	s.Queued, s.RunningTests = queued, run
	s.RunSoFar = s.Passed + s.Failed
	s.TotalWillBeRun = s.Passed + s.Failed + queued + run
	return s
}

// Percent is the share of the tests run so far that passed, as a number to show: never 100 while a test failed, 0 when none ran.
func (s Summary) Percent() float64 {
	p := 0.0
	if s.RunSoFar > 0 {
		p = float64(s.Passed) / float64(s.RunSoFar) * 100
	}
	if s.Failed > 0 {
		p = min(p, 99.9)
	}
	return p
}

// percentText is the percent with three significant digits and no zeros at the end ("60", "66.7", "5.26"); VS Code writes toPrecision(3)
// ("60.0"), the SPEC's example "(60%)" has none (decision 1546).
func percentText(p float64) string {
	var s string
	switch {
	case p >= 100:
		s = strconv.FormatFloat(p, 'f', 0, 64)
	case p >= 10:
		s = strconv.FormatFloat(p, 'f', 1, 64)
	default:
		s = strconv.FormatFloat(p, 'f', 2, 64)
	}
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// ProgressText is the line that says how the run is going: "Running tests...", "Running tests, 2/5 passed (40%)", "3/5 tests passed (60%)",
// and with skipped tests "3/5 tests passed (60%, 1 skipped)".
//
// Ported from VS Code: src/vs/workbench/contrib/testing/common/testingProgressMessages.ts (getTestProgressText)
func ProgressText(s Summary) string {
	p := percentText(s.Percent())
	if s.Running {
		switch {
		case s.RunSoFar == 0:
			return "Running tests..."
		case s.Skipped == 0:
			return fmt.Sprintf("Running tests, %d/%d passed (%s%%)", s.Passed, s.TotalWillBeRun, p)
		}
		return fmt.Sprintf("Running tests, %d/%d tests passed (%s%%, %d skipped)", s.Passed, s.TotalWillBeRun, p, s.Skipped)
	}
	if s.Skipped == 0 {
		return fmt.Sprintf("%d/%d tests passed (%s%%)", s.Passed, s.RunSoFar, p)
	}
	return fmt.Sprintf("%d/%d tests passed (%s%%, %d skipped)", s.Passed, s.RunSoFar, p, s.Skipped)
}

// BadgeCount is the number the count badge of the activity bar icon shows for the setting testing.countBadge ("failed", "passed",
// "skipped"; "off" and anything else give 0).
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts (ResultSummaryView.renderActivityBadge)
func BadgeCount(s Summary, setting string) int {
	switch setting {
	case "passed":
		return s.Passed
	case "skipped":
		return s.Skipped
	case "failed":
		return s.Failed
	}
	return 0
}

// BadgeTooltip is the hover text of the count badge.
func BadgeTooltip(setting string, n int) string {
	switch setting {
	case "passed":
		return fmt.Sprintf("%d passed tests", n)
	case "skipped":
		return fmt.Sprintf("%d skipped tests", n)
	}
	return fmt.Sprintf("%d failed tests", n)
}

// SummaryDuration is the duration at the end of the summary line: "850 ms" below a second, "2.1 s" from there (the SPEC's example; VS
// Code writes "850ms" and "2.1s" without the space, decision 1548).
func SummaryDuration(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)
	if ms < 1000 {
		return fmt.Sprintf("%.0f ms", ms)
	}
	return fmt.Sprintf("%.1f s", ms/1000)
}
