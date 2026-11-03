package httpclient

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The report of a run (SPEC §10.33 "As built (step 27.7)"): Save Results writes it as JSON where the user picks. It holds what the table
// shows, with the URLs and the extracted secrets masked as the history masks them; it does not hold the values of the data file (only
// how many rows it had) and no body.

// reportVersion is the version of the report's layout.
const reportVersion = 1

type reportFile struct {
	Version     int            `json:"emetosHttpRun"`
	Name        string         `json:"name"`
	Environment string         `json:"environment,omitempty"`
	Result      string         `json:"result"`
	Iterations  int            `json:"iterations"`
	DataRows    int            `json:"dataRows,omitempty"`
	Totals      reportTotals   `json:"totals"`
	Results     []reportResult `json:"results"`
}

type reportTotals struct {
	Requests     int   `json:"requests"`
	Errors       int   `json:"errors"`
	ChecksPassed int   `json:"checksPassed"`
	ChecksFailed int   `json:"checksFailed"`
	TimeMs       int64 `json:"timeMs"`
}

type reportResult struct {
	Iteration  int             `json:"iteration"`
	Request    string          `json:"request"`
	Method     string          `json:"method"`
	URL        string          `json:"url,omitempty"`
	Status     int             `json:"status,omitempty"`
	StatusText string          `json:"statusText,omitempty"`
	TimeMs     int64           `json:"timeMs"`
	SizeBytes  int64           `json:"sizeBytes"`
	Error      string          `json:"error,omitempty"`
	Canceled   bool            `json:"canceled,omitempty"`
	Checks     []reportCheck   `json:"checks,omitempty"`
	Extracted  []reportExtract `json:"extracted,omitempty"`
	Notes      []string        `json:"notes,omitempty"`
}

type reportCheck struct {
	What     string `json:"check"`
	Expected string `json:"expected"`
	Actual   string `json:"actual,omitempty"`
	Passed   bool   `json:"passed"`
	Problem  string `json:"problem,omitempty"`
}

type reportExtract struct {
	Variable string `json:"variable"`
	Scope    string `json:"scope"`
	Value    string `json:"value,omitempty"`
	Stored   bool   `json:"stored"`
	Problem  string `json:"problem,omitempty"`
}

// reportOf makes the report of a run's results.
func reportOf(name, envName string, end runEnd, iterations, dataRows int, results []runResult) reportFile {
	tot := totalsOf(results)
	rep := reportFile{
		Version: reportVersion, Name: name, Environment: envName, Result: end.String(), Iterations: iterations, DataRows: dataRows,
		Totals:  reportTotals{Requests: tot.Requests, Errors: tot.Errors, ChecksPassed: tot.Passed, ChecksFailed: tot.Failed, TimeMs: tot.Time.Milliseconds()},
		Results: make([]reportResult, 0, len(results)),
	}
	for _, r := range results {
		x := reportResult{
			Iteration: r.Iteration, Request: r.Name, Method: r.Method, URL: r.URL, Status: r.Status, StatusText: r.StatusText,
			TimeMs: r.Duration.Milliseconds(), SizeBytes: r.Size, Error: r.Err, Canceled: r.Canceled, Notes: r.Notes,
		}
		for _, c := range r.Checks {
			x.Checks = append(x.Checks, reportCheck{What: c.What, Expected: c.Expected, Actual: c.Actual, Passed: c.Passed, Problem: c.Problem})
		}
		for _, o := range r.Extracted {
			x.Extracted = append(x.Extracted, reportExtract{Variable: strings.TrimSpace(o.Rule.To), Scope: o.Scope, Value: o.shown(), Stored: o.Stored, Problem: o.Problem})
		}
		rep.Results = append(rep.Results, x)
	}
	return rep
}

// writeReport writes the report to a file; a file that exists is replaced. It fails with
// HttpRunReportWriteFailed.
func writeReport(ctx context.Context, rep reportFile, to string) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr("HttpRunReportWriteFailed", err).SetMetadata("path", to)
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return errors.NewErr("HttpRunReportWriteFailed", err).SetMetadata("path", to)
	}
	if err := os.WriteFile(to, append(data, '\n'), 0o644); err != nil {
		return errors.NewErr("HttpRunReportWriteFailed", err).SetMetadata("path", to)
	}
	return nil
}

// reportName is the name the Save dialog starts with.
func (t *runTab) reportName() string {
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == filepath.Separator || r == 0 {
			return '-'
		}
		return r
	}, strings.TrimSpace(t.name))
	if name == "" {
		name = "run"
	}
	return name + "-results.json"
}

// saveResults asks where to write the report of the last run and writes it.
func (t *runTab) saveResults() {
	e := t.e
	if t.running || len(t.results) == 0 {
		return
	}
	rows := 0
	if t.table != nil {
		rows = len(t.table.Rows)
	}
	rep := reportOf(t.name, t.envName, t.end, t.total, rows, append([]runResult(nil), t.results...))
	name := t.reportName()
	var saved string
	e.async(func(ctx context.Context) error {
		to, err := e.api.FileChooser().Save(ctx, extension.FileChooserOptions{
			Title: "Save Results", AcceptLabel: "Save", Name: name, Folder: e.folder,
			Filters: []extension.FileFilter{{Name: "JSON", Patterns: []string{"*.json"}}},
		})
		if err != nil || to == "" {
			return err //nolint:wrapcheck // the API's own error
		}
		if err := writeReport(ctx, rep, to); err != nil {
			return err
		}
		saved = to
		return nil
	}, func(err error) {
		switch {
		case err != nil:
			e.fail("save the results", err)
		case saved != "":
			e.api.Notifications().Info("Saved the results of " + plural(len(rep.Results), "request", "requests") + " to " + saved)
		}
	})
}
