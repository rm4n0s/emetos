package sql

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// runner opens a runner on the fixture.
func runnerOn(t *testing.T, mutate func(*Service, string)) (*Runner, *Service) {
	t.Helper()
	svc, c := openFixture(t, nil)
	if mutate != nil {
		mutate(svc, c.ID)
	}
	r, err := svc.NewRunner(c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r, svc
}

// runScript splits the script by the dialect of the runner and runs it.
func runScript(t *testing.T, r *Runner, script string, opts RunOptions) []*StatementResult {
	t.Helper()
	res, err := r.Run(ctxFor(t), dialect.Split(r.d, script), opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// cell reads the text of a cell of a set.
func setCell(set *ResultSet, row, col int) string {
	dst := make([]resultgrid.Value, len(set.Columns))
	set.Table.Row(row, dst)
	return dst[col].Full("NULL")
}

func TestRunningASelect(t *testing.T) {
	r, _ := runnerOn(t, nil)
	res := runScript(t, r, "SELECT id, name, born FROM authors ORDER BY id", RunOptions{})
	if len(res) != 1 || res[0].Err != nil || len(res[0].Sets) != 1 {
		t.Fatalf("results = %+v", res)
	}
	set := res[0].Sets[0]
	if got := set.Table.Len(); got != 2 {
		t.Fatalf("rows = %d", got)
	}
	if cols := set.Columns; len(cols) != 3 || cols[0].Name != "id" || cols[1].Name != "name" {
		t.Errorf("columns = %+v", cols)
	}
	if got := setCell(set, 0, 1) + "/" + setCell(set, 1, 1) + "/" + setCell(set, 1, 2); got != "Ursula/Iain/1954" {
		t.Errorf("cells = %q", got)
	}
	if set.More() || set.Note != "" {
		t.Errorf("a short result is whole: more %v, note %q", set.More(), set.Note)
	}
	if res[0].Elapsed <= 0 {
		t.Error("no elapsed time")
	}
}

func TestRowsAffectedAndMessages(t *testing.T) {
	r, _ := runnerOn(t, nil)
	res := runScript(t, r, "INSERT INTO log (line) VALUES ('a'), ('b'), ('c'); UPDATE log SET line = 'z' WHERE n > 1; DELETE FROM log WHERE n = 1", RunOptions{})
	if len(res) != 3 {
		t.Fatalf("results = %d", len(res))
	}
	for i, want := range []int64{3, 3, 1} {
		if res[i].Err != nil || !res[i].HasAffected || res[i].Affected != want || len(res[i].Sets) != 0 {
			t.Errorf("statement %d = %+v (err %v), want %d affected", i, res[i], res[i].Err, want)
		}
	}
}

func TestDDLAndAStatementWithNoRows(t *testing.T) {
	r, _ := runnerOn(t, nil)
	res := runScript(t, r, "CREATE TABLE extra (a INTEGER); PRAGMA foreign_keys = ON; DROP TABLE extra", RunOptions{})
	if len(res) != 3 {
		t.Fatalf("results = %d", len(res))
	}
	for _, x := range res {
		if x.Err != nil {
			t.Errorf("%q: %v", x.Statement.Text, x.Err.Text())
		}
	}
	if !res[0].Info.Schema || !res[2].Info.Destructive {
		t.Errorf("classification: %+v %+v", res[0].Info, res[2].Info)
	}
}

func TestAnErrorStopsTheRunAndSaysWhere(t *testing.T) {
	tests := []struct {
		name, sql string
		offset    int
		message   string
	}{
		{"a syntax error", "SELECT 1; SELECT id FORM authors", len("SELECT id FORM "), "near \"authors\""},
		{"a missing table", "SELECT * FROM nothing_here", len("SELECT * FROM "), "no such table: nothing_here"},
		{"a missing column", "SELECT nope FROM authors", len("SELECT "), "no such column: nope"},
		{"a constraint", "INSERT INTO authors (id, name) VALUES (1, 'dup')", -1, "UNIQUE constraint failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := runnerOn(t, nil)
			stmts := dialect.Split(r.d, tc.sql)
			res, err := r.Run(ctxFor(t), append(stmts, dialect.Split(r.d, "SELECT 99")...), RunOptions{})
			if err != nil {
				t.Fatal(err)
			}
			last := res[len(res)-1]
			if last.Err == nil || last.Cancelled {
				t.Fatalf("no error in %+v", last)
			}
			if len(res) != len(stmts) {
				t.Errorf("the run went on after the error: %d results for %d statements", len(res), len(stmts))
			}
			if !strings.Contains(last.Err.Message, tc.message) {
				t.Errorf("message = %q, want it to hold %q", last.Err.Message, tc.message)
			}
			if tc.offset >= 0 && last.Err.Offset != tc.offset {
				t.Errorf("offset = %d, want %d (statement %q)", last.Err.Offset, tc.offset, last.Statement.Text)
			}
			if tc.offset < 0 && last.Err.Offset != -1 {
				t.Errorf("offset = %d, want none", last.Err.Offset)
			}
		})
	}
}

func TestTheRowLimitAndFetchMore(t *testing.T) {
	r, _ := runnerOn(t, nil)
	const total = 25
	res := runScript(t, r, "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 25) SELECT i FROM n", RunOptions{MaxRows: 10})
	if len(res) != 1 || res[0].Err != nil {
		t.Fatalf("results = %+v", res)
	}
	set := res[0].Sets[0]
	if set.Table.Len() != 10 || !set.More() {
		t.Fatalf("first read: %d rows, more %v", set.Table.Len(), set.More())
	}
	if err := r.FetchMore(ctxFor(t), set, RunOptions{MaxRows: 10}); err != nil {
		t.Fatal(err)
	}
	if set.Table.Len() != 20 || !set.More() {
		t.Fatalf("second read: %d rows, more %v", set.Table.Len(), set.More())
	}
	if err := r.FetchMore(ctxFor(t), set, RunOptions{MaxRows: 10}); err != nil {
		t.Fatal(err)
	}
	if set.Table.Len() != total || set.More() {
		t.Fatalf("third read: %d rows, more %v", set.Table.Len(), set.More())
	}
	if setCell(set, 0, 0) != "1" || setCell(set, total-1, 0) != "25" {
		t.Errorf("the rows are %s .. %s", setCell(set, 0, 0), setCell(set, total-1, 0))
	}
	// the session is free again
	if res := runScript(t, r, "SELECT 1", RunOptions{}); res[0].Err != nil {
		t.Errorf("after the fetch: %v", res[0].Err.Text())
	}
}

func TestAnExactMultipleOfTheLimitHasNoMore(t *testing.T) {
	r, _ := runnerOn(t, nil)
	res := runScript(t, r, "SELECT n FROM (SELECT 1 AS n UNION ALL SELECT 2)", RunOptions{MaxRows: 2})
	if set := res[0].Sets[0]; set.Table.Len() != 2 || set.More() {
		t.Errorf("%d rows, more %v", set.Table.Len(), set.More())
	}
}

func TestOnlyTheLastStatementKeepsItsCursor(t *testing.T) {
	r, _ := runnerOn(t, nil)
	res := runScript(t, r, "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 30) SELECT i FROM n; "+
		"WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 30) SELECT i FROM n", RunOptions{MaxRows: 5})
	if len(res) != 2 {
		t.Fatalf("results = %d", len(res))
	}
	first, second := res[0].Sets[0], res[1].Sets[0]
	if first.More() || !strings.Contains(first.Note, "first 5 rows") {
		t.Errorf("first: more %v, note %q", first.More(), first.Note)
	}
	if !second.More() {
		t.Error("the last statement lost its cursor")
	}
}

func TestAResultIsBoundedInMemory(t *testing.T) {
	r, _ := runnerOn(t, nil)
	res := runScript(t, r, "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 1000) SELECT i, hex(zeroblob(100)) FROM n", RunOptions{MaxRows: 1000, MaxBytes: 4000})
	set := res[0].Sets[0]
	if set.Table.Len() >= 1000 || set.Table.Len() < 5 || set.More() || !strings.Contains(set.Note, "in memory") {
		t.Errorf("%d rows, more %v, note %q", set.Table.Len(), set.More(), set.Note)
	}
	if set.Bytes() > 4000 {
		t.Errorf("bytes = %d", set.Bytes())
	}
}

func TestTheSessionKeepsItsState(t *testing.T) {
	r, _ := runnerOn(t, nil)
	res := runScript(t, r, "CREATE TEMP TABLE scratch (a INTEGER); INSERT INTO scratch VALUES (7); SELECT a FROM scratch", RunOptions{})
	if got := res[2].Sets[0]; setCell(got, 0, 0) != "7" {
		t.Errorf("temp table = %s", setCell(got, 0, 0))
	}
	// in the next run, too
	if res := runScript(t, r, "SELECT count(*) FROM scratch", RunOptions{}); res[0].Err != nil || setCell(res[0].Sets[0], 0, 0) != "1" {
		t.Errorf("second run: %+v", res[0])
	}
}

func TestTransactions(t *testing.T) {
	r, svc := runnerOn(t, nil)
	ctx := ctxFor(t)
	count := func() string {
		res, _, err := svc.Rows(ctx, r.connID, "", dialect.Query{SQL: "SELECT count(*) FROM log"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		return res[0][0].String
	}
	if r.InTransaction() {
		t.Fatal("a transaction before Begin")
	}
	if err := r.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	if !r.InTransaction() {
		t.Error("Begin did not open a transaction")
	}
	if err := r.Begin(ctx); err == nil {
		t.Error("two transactions")
	} else {
		wantRoute(t, err, "Runner.Begin.SQLTransactionOpen")
	}
	runScript(t, r, "INSERT INTO log (line) VALUES ('in the transaction')", RunOptions{})
	if got := count(); got != "1" {
		t.Errorf("another connection sees %s rows of an open transaction, want the 1 that was committed", got)
	}
	if err := r.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if r.InTransaction() || count() != "1" {
		t.Errorf("after Rollback: in %v, rows %s", r.InTransaction(), count())
	}
	// commit
	if err := r.Begin(ctx); err != nil {
		t.Fatal(err)
	}
	runScript(t, r, "INSERT INTO log (line) VALUES ('kept')", RunOptions{})
	if err := r.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != "2" || r.InTransaction() {
		t.Errorf("after Commit: rows %s, in %v", got, r.InTransaction())
	}
	wantRoute(t, r.Commit(ctx), "Runner.Commit.SQLNoTransaction")
	wantRoute(t, r.Rollback(ctx), "Runner.Rollback.SQLNoTransaction")
}

func TestTransactionStatementsTheUserTypesAreFollowed(t *testing.T) {
	r, _ := runnerOn(t, nil)
	runScript(t, r, "BEGIN", RunOptions{})
	if !r.InTransaction() {
		t.Error("BEGIN not followed")
	}
	runScript(t, r, "INSERT INTO log (line) VALUES ('x'); SAVEPOINT a; INSERT INTO log (line) VALUES ('y'); ROLLBACK TO a", RunOptions{})
	if !r.InTransaction() {
		t.Error("ROLLBACK TO ended the transaction")
	}
	runScript(t, r, "COMMIT", RunOptions{})
	if r.InTransaction() {
		t.Error("COMMIT not followed")
	}
}

func TestClosingARunnerRollsBackItsTransaction(t *testing.T) {
	svc, c := openFixture(t, nil)
	r, err := svc.NewRunner(c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Begin(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	runScript(t, r, "INSERT INTO log (line) VALUES ('lost')", RunOptions{})
	rolled, err := r.Close()
	if err != nil || !rolled {
		t.Fatalf("Close = %v, %v", rolled, err)
	}
	rows, _, err := svc.Rows(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT count(*) FROM log"}, 0)
	if err != nil || rows[0][0].String != "1" {
		t.Errorf("rows after the close: %v, %v", rows, err)
	}
	// a runner that is closed can be used again with a new session
	if res := runScript(t, r, "SELECT 1", RunOptions{}); res[0].Err != nil {
		t.Errorf("after Close: %v", res[0].Err.Text())
	}
	if rolled, err := r.Close(); err != nil || rolled {
		t.Errorf("second Close = %v, %v", rolled, err)
	}
}

func TestCancellingALongStatement(t *testing.T) {
	r, _ := runnerOn(t, nil)
	done := make(chan []*StatementResult, 1)
	started := make(chan struct{})
	go func() {
		res, err := r.Run(context.Background(), dialect.Split(r.d, "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n) SELECT count(*) FROM n; SELECT 'never'"), RunOptions{
			Started: func(int, dialect.Statement) { close(started) },
		})
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	<-started
	waitUntil(t, "the statement to run", func() bool { return r.Running() })
	time.Sleep(100 * time.Millisecond) // let the engine get into the statement
	t0 := time.Now()
	if err := r.Cancel(); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-done:
		if len(res) != 1 || !res[0].Cancelled || res[0].Err == nil {
			t.Errorf("results = %+v", res)
		}
		if took := time.Since(t0); took > 5*time.Second {
			t.Errorf("the cancel took %v", took)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the statement did not stop")
	}
	// the session is usable
	if res := runScript(t, r, "SELECT 5", RunOptions{}); res[0].Err != nil || setCell(res[0].Sets[0], 0, 0) != "5" {
		t.Errorf("after the cancel: %+v", res[0])
	}
	if err := r.Cancel(); err != nil {
		t.Errorf("Cancel with nothing running: %v", err)
	}
}

func TestOneThingAtATime(t *testing.T) {
	r, _ := runnerOn(t, nil)
	started := make(chan struct{})
	finish := make(chan struct{})
	go func() {
		_, _ = r.Run(context.Background(), dialect.Split(r.d, "SELECT 1"), RunOptions{Started: func(int, dialect.Statement) { close(started); <-finish }})
	}()
	<-started
	_, err := r.Run(ctxFor(t), dialect.Split(r.d, "SELECT 2"), RunOptions{})
	wantRoute(t, err, "Runner.acquire.SQLRunnerBusy")
	wantRoute(t, r.Begin(ctxFor(t)), "Runner.acquire.SQLRunnerBusy")
	close(finish)
	waitUntil(t, "the first run to end", func() bool { return !r.Running() })
}

func TestAReadOnlyConnectionCannotWriteInTheEngine(t *testing.T) {
	r, _ := runnerOn(t, nil)
	_ = r
	svc, c := openFixture(t, nil)
	ro := c
	ro.ID, ro.ReadOnly = "ro1", true
	if err := svc.Open(ctxFor(t), ro, ""); err != nil {
		t.Fatal(err)
	}
	rr, err := svc.NewRunner(ro.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := rr.Close(); err != nil {
			t.Error(err)
		}
	}()
	res := runScript(t, rr, "SELECT 1; INSERT INTO log (line) VALUES ('no')", RunOptions{})
	if len(res) != 2 || res[0].Err != nil || res[1].Err == nil {
		t.Fatalf("results = %+v", res)
	}
	if !strings.Contains(strings.ToLower(res[1].Err.Message), "readonly") && !strings.Contains(strings.ToLower(res[1].Err.Message), "read-only") && !strings.Contains(strings.ToLower(res[1].Err.Message), "read only") {
		t.Errorf("the engine said %q", res[1].Err.Message)
	}
}

func TestTheValuesOfSQLiteInTheGrid(t *testing.T) {
	r, _ := runnerOn(t, nil)
	res := runScript(t, r, "SELECT 1, 2.5, 'text', NULL, x'cafe', 'x' || char(10) || 'y'", RunOptions{})
	set := res[0].Sets[0]
	kinds := make([]resultgrid.Kind, len(set.Columns))
	dst := make([]resultgrid.Value, len(set.Columns))
	set.Table.Row(0, dst)
	for i, v := range dst {
		kinds[i] = v.Kind
	}
	want := []resultgrid.Kind{resultgrid.KindInt, resultgrid.KindFloat, resultgrid.KindText, resultgrid.KindNull, resultgrid.KindBytes, resultgrid.KindText}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("column %d is kind %d, want %d", i, kinds[i], want[i])
		}
	}
}

func TestTheRunnerOfAConnectionThatIsNotOpen(t *testing.T) {
	svc := NewService()
	_, err := svc.NewRunner("nope", "")
	wantRoute(t, err, "Service.Dialect->Service.get.SQLNotConnected")
}
