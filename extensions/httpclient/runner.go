package httpclient

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/checks"
	"github.com/rm4n0s/emetos/extensions/httpclient/history"
	"github.com/rm4n0s/emetos/extensions/httpclient/rundata"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
)

// The collection runner (SPEC §10.33 "The collection runner"): this file runs a plan, the tab (runtab.go, runview.go) asks for it and
// shows the results.

// runItem is a request of a run: where it is and what it inherits from the folders and the collection, taken on the UI thread when the
// run starts so that the run reads no part of the tree.
type runItem struct {
	path, name, method string
	place              place
}

// runPlan is everything a run needs; nothing in it changes while the run goes.
type runPlan struct {
	items []runItem
	store *store.Store
	// iterations is the number of passes over the items (with a data file: the number of its rows, set when the file is read).
	iterations int
	delay      time.Duration
	stopOnFail bool
	// dataPath is the data file, "" for none; the run reads it when it starts.
	dataPath string
}

// runResult is what one request of one iteration came to.
type runResult struct {
	// Iteration is the pass this result belongs to, from 1; Item is the index of the request in the plan.
	Iteration int
	Item      int
	Name      string
	Method    string
	// URL is the URL of the final exchange, with the secrets in it masked as the history does.
	URL        string
	Status     int
	StatusText string
	Duration   time.Duration
	Size       int64
	// Err is why the request has no response, or what came with it besides its status; Canceled says that the user stopped it.
	Err      string
	Canceled bool
	Checks   []checks.Result
	// Extracted are the outcomes of the extraction rules, Notes the variables that had no value and the warnings of the engine.
	Extracted []extractOutcome
	Notes     []string
}

// failed says that the result counts as a failure for "stop on first failure": a request that got no response or ended in an error,
// or a check that failed. A status of 400 or more is a failure only when a check says so.
func (r runResult) failed() bool {
	if r.Canceled {
		return false
	}
	if r.Err != "" {
		return true
	}
	for _, c := range r.Checks {
		if !c.Passed {
			return true
		}
	}
	return false
}

// runEnd is how a run ended.
type runEnd int

// The ways a run ends.
const (
	// runCompleted: every iteration ran.
	runCompleted runEnd = iota + 1
	// runStopped: "stop on first failure" ended it.
	runStopped
	// runCanceled: the user ended it.
	runCanceled
	// runNotStarted: the data file could not be read, so nothing ran.
	runNotStarted
)

// String is the word the table and the report use.
func (r runEnd) String() string {
	switch r {
	case runCompleted:
		return "completed"
	case runStopped:
		return "stopped"
	case runCanceled:
		return "canceled"
	}
	return "not started"
}

// runner is what the goroutine of a run calls back into: results arrive in order, on the goroutine of the run.
type runner struct {
	// result gets every result; rows tells the tab how many iterations the data file gave.
	result func(runResult)
	rows   func(rundata.Table)
}

// execute runs the plan: every iteration runs every request in order, one at a time, the values extracted by one request being the
// current values the next one resolves against. It does I/O and runs off the UI thread; ctx cancels it, and no request is sent after it
// is canceled. Run sends are not recorded in the history (decision 1862). It fails with the errors of rundata.Load (HttpRunData*), in
// which case nothing ran.
func (e *Extension) execute(ctx context.Context, plan runPlan, cb runner) (runEnd, error) {
	var data *rundata.Table
	n := plan.iterations
	if plan.dataPath != "" {
		tb, err := rundata.Load(ctx, plan.dataPath)
		if err != nil {
			return runNotStarted, err
		}
		data, n = &tb, len(tb.Rows)
		cb.rows(tb)
	}
	if n < 1 {
		n = 1
	}
	for it := 1; it <= n; it++ {
		var locals map[string]string
		if data != nil {
			locals = data.Rows[it-1]
		}
		for i, item := range plan.items {
			if ctx.Err() != nil {
				return runCanceled, nil
			}
			res := e.runOne(ctx, plan, item, locals)
			res.Iteration, res.Item = it, i
			cb.result(res)
			switch {
			case res.Canceled:
				return runCanceled, nil
			case res.failed() && plan.stopOnFail:
				return runStopped, nil
			}
			if plan.delay > 0 && (i < len(plan.items)-1 || it < n) {
				select {
				case <-ctx.Done():
					return runCanceled, nil
				case <-time.After(plan.delay):
				}
			}
		}
	}
	return runCompleted, nil
}

// runOne reads one request from its files and sends it as the request tab does (runSend), with the columns of the data row as the
// variables that win over every other.
func (e *Extension) runOne(ctx context.Context, plan runPlan, item runItem, locals map[string]string) runResult {
	res := runResult{Name: item.name, Method: item.method}
	files, err := plan.store.ReadRequest(ctx, item.path)
	if err != nil {
		if ctx.Err() != nil {
			res.Err, res.Canceled = "Canceled.", true
			return res
		}
		res.Err = "Cannot read the request: " + err.Error()
		return res
	}
	out := e.runSend(ctx, sendJob{files: files, place: item.place, locals: locals})
	res.Duration, res.Extracted, res.Checks = out.duration, out.extracted, out.checkResults
	if resp := out.resp; resp != nil {
		res.Status = resp.StatusCode
		res.StatusText = strings.TrimSpace(strings.TrimPrefix(resp.Status, strconv.Itoa(resp.StatusCode)))
		res.URL = history.MaskURL(resp.URL)
		if resp.Body != nil {
			res.Size = resp.Body.Size()
			if err := resp.Body.Remove(); err != nil {
				e.api.Log().Warn("the body of a response was not removed", "err", err)
			}
		}
		if len(resp.Unresolved) > 0 {
			res.Notes = append(res.Notes, "No value for "+strings.Join(resp.Unresolved, ", "))
		}
		if len(resp.MissingSecrets) > 0 {
			res.Notes = append(res.Notes, "The secret "+strings.Join(resp.MissingSecrets, ", ")+" has no value")
		}
		res.Notes = append(res.Notes, resp.Warnings...)
	}
	if out.err != nil {
		res.Err = out.err.Error()
		if appErr, ok := errors.FromError(out.err); ok && appErr.Tag == "EngineCanceled" {
			res.Err, res.Canceled = "Canceled.", true
		}
	}
	if ctx.Err() != nil && res.Err != "" {
		res.Canceled = true
	}
	return res
}

// runTotals are the numbers under the table.
type runTotals struct {
	Requests, Errors, Passed, Failed int
	Time                             time.Duration
}

// totalsOf counts results.
func totalsOf(results []runResult) runTotals {
	var t runTotals
	for _, r := range results {
		t.Requests++
		t.Time += r.Duration
		if r.Err != "" && !r.Canceled {
			t.Errors++
		}
		p, n := checkCounts(r.Checks)
		t.Passed += p
		t.Failed += n - p
	}
	return t
}
