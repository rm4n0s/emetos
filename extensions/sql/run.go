package sql

import (
	"context"
	dbsql "database/sql"
	"database/sql/driver"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// The limits of what a run keeps (SPEC §10.21 "The results panel").
const (
	// DefaultMaxRows is the number of rows a result set reads before it offers Fetch More: the setting sql.maxRows.
	DefaultMaxRows = 1000
	// MaxResultBytes bounds one result set in memory: reading stops there with a note, and Fetch More offers the rest only as far as
	// this room goes.
	MaxResultBytes = 64 << 20
	// controlTimeout bounds Begin, Commit, Rollback and the rollback when a runner closes.
	controlTimeout = 15 * time.Second
)

// RunOptions say how many rows a statement reads.
type RunOptions struct {
	// MaxRows is the rows of a result set read at first and by each Fetch More; DefaultMaxRows when 0.
	MaxRows int
	// MaxBytes bounds the memory of one result set; MaxResultBytes when 0.
	MaxBytes int64
	// Started is called, on the goroutine that runs, before a statement is sent: i is its index in the statements given.
	Started func(i int, st dialect.Statement)
	// Finished is called, on the goroutine that runs, when a statement is done, with everything it gave. The result is the caller's
	// from then on, except for Fetch More on the last set.
	Finished func(r *StatementResult)
}

// ResultSet is the rows a statement returned, as the grid shows them.
type ResultSet struct {
	// Columns are the columns of the set.
	Columns []resultgrid.Column
	// Table holds the rows read so far; Fetch More appends to it.
	Table *resultgrid.Table
	// Note says why the set is not whole: the server has more rows than were read, or the set reached its bound.
	Note string

	// cur is the open cursor while the server has more rows; nil when the set is whole or its cursor was closed.
	cur *cursor
	// bytes is the estimated memory of the rows.
	bytes int64
}

// More says that the server has more rows that Fetch More can read.
func (s *ResultSet) More() bool {
	return s.cur != nil && !s.cur.closed.Load()
}

// Bytes is the estimated memory of the rows of the set.
func (s *ResultSet) Bytes() int64 { return s.bytes }

// Release drops the rows of the set and closes its cursor: the set is discarded.
func (s *ResultSet) Release() {
	if s.cur != nil {
		s.cur.close()
		s.cur = nil
	}
	s.Table = resultgrid.NewTable(s.Columns)
	s.bytes = 0
}

// cursor is the rows of a statement that has more than was read, kept open on the session until the next statement or the end.
type cursor struct {
	rows    *dbsql.Rows
	engine  string
	types   []string
	pending []resultgrid.Value
	// done ends the context of the statement; it is called when the cursor is closed.
	done   func()
	once   sync.Once
	closed atomic.Bool
	// err is the first error of closing.
	err error
}

// close closes the rows and ends the statement's context.
func (c *cursor) close() {
	c.once.Do(func() {
		c.closed.Store(true)
		c.err = c.rows.Close()
		c.done()
	})
}

// StatementResult is everything one statement gave.
type StatementResult struct {
	// Index is the place of the statement in the statements the run was given.
	Index int
	// Statement and Info are the statement and what Classify found.
	Statement dialect.Statement
	Info      dialect.Info
	// Elapsed is how long the statement took, up to the reading of the first rows.
	Elapsed time.Duration
	// Sets are the result sets, in order; none for a statement that returns no rows.
	Sets []*ResultSet
	// Affected is the number of rows a statement without a result set changed; HasAffected tells whether the engine said.
	Affected    int64
	HasAffected bool
	// Err is why the statement failed, and Cancelled that the user cancelled it (Err then says what the engine answered).
	Err       *QueryError
	Cancelled bool
}

// Runner runs statements in a session of a connection, for one query editor: the session is taken when the first statement
// runs and kept, so that a transaction, a temporary table and the settings of the session are there for the next statement. It runs
// one thing at a time. It is safe for use from several goroutines; Cancel is meant to be called from another one than Run.
type Runner struct {
	svc      *Service
	connID   string
	database string
	d        dialect.Dialect

	mu      sync.Mutex
	sess    *Session
	inTx    bool
	last    *cursor
	running bool

	cancelled atomic.Bool
}

// NewRunner makes a runner for the connection and database. It takes no session before the first statement. The errors have the tag
// SQLNotConnected.
func (s *Service) NewRunner(connID, database string) (*Runner, error) {
	d, err := s.Dialect(connID)
	if err != nil {
		return nil, err
	}
	r := &Runner{svc: s, connID: connID, database: database, d: d}
	s.mu.Lock()
	if s.runners == nil {
		s.runners = map[*Runner]struct{}{}
	}
	s.runners[r] = struct{}{}
	s.mu.Unlock()
	return r, nil
}

// OpenTransactions is the number of runners of the connection that have a transaction open: what a disconnect would roll back.
func (s *Service) OpenTransactions(connID string) int {
	s.mu.Lock()
	var rs []*Runner
	for r := range s.runners {
		if r.connID == connID {
			rs = append(rs, r)
		}
	}
	s.mu.Unlock()
	n := 0
	for _, r := range rs {
		if r.InTransaction() {
			n++
		}
	}
	return n
}

// Dispose ends the runner for good: its session is closed as Close does, and the service forgets it.
func (r *Runner) Dispose() (rolledBack bool, err error) {
	r.svc.mu.Lock()
	delete(r.svc.runners, r)
	r.svc.mu.Unlock()
	return r.Close()
}

// InTransaction tells whether a transaction is open on the session.
func (r *Runner) InTransaction() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inTx
}

// Running tells whether a statement is being run.
func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// acquire marks the runner busy; it fails when something is running already. The errors have the tag SQLRunnerBusy.
func (r *Runner) acquire() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return errors.New("SQLRunnerBusy", "a statement is running in this editor", "connection", r.connID)
	}
	r.running = true
	r.cancelled.Store(false)
	return nil
}

// release marks the runner free.
func (r *Runner) release() {
	r.mu.Lock()
	r.running = false
	r.mu.Unlock()
}

// session returns the session, taking one the first time and after one was lost. It must be called with the runner acquired.
func (r *Runner) session(ctx context.Context) (*Session, error) {
	r.mu.Lock()
	sess := r.sess
	r.mu.Unlock()
	if sess != nil {
		return sess, nil
	}
	sess, err := r.svc.Session(ctx, r.connID, r.database)
	if err != nil {
		return nil, err
	}
	if use := r.d.UseDatabase(r.database); use != "" {
		if _, err := sess.Conn().ExecContext(ctx, use); err != nil {
			_ = sess.Close() // the session never worked; the error of the USE is the one to report
			return nil, errors.NewErr("SQLUseDatabaseFailed", err).SetMetadata("connection", r.connID, "database", r.database)
		}
	}
	r.mu.Lock()
	r.sess = sess
	r.mu.Unlock()
	return sess, nil
}

// drop forgets a session that the server closed, giving the connection up. A transaction on it is gone.
func (r *Runner) drop() {
	r.mu.Lock()
	sess := r.sess
	r.sess, r.inTx = nil, false
	r.mu.Unlock()
	if sess != nil {
		_ = sess.Close() // the connection is bad already; closing it again tells nothing new
	}
}

// closeCursor closes the cursor of the last statement.
func (r *Runner) closeCursor() {
	r.mu.Lock()
	c := r.last
	r.last = nil
	r.mu.Unlock()
	if c != nil {
		c.close()
	}
}

// Cancel stops the statement that is running. It does nothing when none is.
func (r *Runner) Cancel() error {
	r.mu.Lock()
	sess, running := r.sess, r.running
	r.mu.Unlock()
	if !running || sess == nil {
		return nil
	}
	r.cancelled.Store(true)
	return sess.Cancel()
}

// Run runs the statements one after the other and returns their results. It stops at the first statement that fails or is cancelled,
// or when ctx ends; the results are those of the statements that ran, the last being the one that failed. A batch (dialect
// Statement.Batch) with a repeat count runs that many times. The result sets of the last statement keep their cursor open, so that
// Fetch More can go on; those of the others are read up to MaxRows only. The errors (as opposed to a statement's own failure, which
// is in its result) have the tags SQLRunnerBusy, SQLNotConnected, SQLSessionFailed and SQLUseDatabaseFailed.
func (r *Runner) Run(ctx context.Context, stmts []dialect.Statement, opts RunOptions) ([]*StatementResult, error) {
	if err := r.acquire(); err != nil {
		return nil, err
	}
	defer r.release()
	r.closeCursor()
	if opts.MaxRows <= 0 {
		opts.MaxRows = DefaultMaxRows
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = MaxResultBytes
	}
	sess, err := r.session(ctx)
	if err != nil {
		return nil, err
	}
	var plan []int // the indexes of the statements to send, a batch repeated as its count says
	for i := 0; i < len(stmts); {
		j := i
		for j < len(stmts) && stmts[j].Batch == stmts[i].Batch {
			j++
		}
		for rep := 0; rep < max(stmts[i].Repeat, 1); rep++ {
			for k := i; k < j; k++ {
				plan = append(plan, k)
			}
		}
		i = j
	}
	var results []*StatementResult
	for n, idx := range plan {
		if ctx.Err() != nil {
			break
		}
		st := stmts[idx]
		if opts.Started != nil {
			opts.Started(idx, st)
		}
		res := r.one(ctx, sess, idx, st, opts, n == len(plan)-1)
		results = append(results, res)
		if opts.Finished != nil {
			opts.Finished(res)
		}
		if res.Err != nil || res.Cancelled {
			break
		}
	}
	return results, nil
}

// one runs a statement and reads what it gives. A statement that the user cancelled is marked so even when the engine answers it as
// if it had finished (MySQL's KILL QUERY makes SLEEP return 1 without an error), so that the run stops there.
func (r *Runner) one(ctx context.Context, sess *Session, idx int, st dialect.Statement, opts RunOptions, last bool) *StatementResult {
	res := r.runOne(ctx, sess, idx, st, opts, last)
	if res.Err == nil && r.cancelled.Load() {
		res.Cancelled = true
		res.Err = &QueryError{Message: "The statement was cancelled.", Offset: -1}
	}
	return res
}

// runOne is one without the mark of a cancel.
func (r *Runner) runOne(ctx context.Context, sess *Session, idx int, st dialect.Statement, opts RunOptions, last bool) *StatementResult {
	info := dialect.Classify(r.d, st.Text)
	res := &StatementResult{Index: idx, Statement: st, Info: info}
	sctx, done := sess.Begin(ctx)
	start := time.Now()
	kept := false
	defer func() {
		if !kept {
			done()
		}
	}()
	conn := sess.Conn()
	if !info.Rows {
		out, err := conn.ExecContext(sctx, st.Text)
		res.Elapsed = time.Since(start)
		if err != nil {
			r.fail(res, err)
			return res
		}
		if n, err := out.RowsAffected(); err == nil {
			res.Affected, res.HasAffected = n, true
		}
		r.afterSuccess(info)
		return res
	}
	rows, err := conn.QueryContext(sctx, st.Text)
	res.Elapsed = time.Since(start)
	if err != nil {
		r.fail(res, err)
		return res
	}
	cur := &cursor{rows: rows, engine: r.d.ID(), done: done}
	for {
		set, err := r.readSet(sctx, cur, opts)
		if err != nil {
			cur.close()
			kept = true // the cursor's close ended the context
			r.fail(res, err)
			return res
		}
		if set != nil {
			res.Sets = append(res.Sets, set)
			if set.cur != nil && last {
				// database/sql closes the rows when it moves on, so the cursor stays at this set for Fetch More
				break
			}
		}
		if !rows.NextResultSet() {
			break
		}
	}
	if err := rows.Err(); err != nil {
		cur.close()
		kept = true
		r.fail(res, err)
		return res
	}
	// the cursor stays open for Fetch More only when the last set has more rows and nothing else is to run after the statement
	var keep *ResultSet
	if n := len(res.Sets); n > 0 && res.Sets[n-1].cur == cur && last {
		keep = res.Sets[n-1]
	}
	for _, s := range res.Sets {
		if s != keep && s.cur != nil {
			s.cur = nil
			s.Note = "Only the first " + strconv.Itoa(s.Table.Len()) + " rows were read."
		}
	}
	if keep != nil {
		kept = true
		r.mu.Lock()
		r.last = cur
		r.mu.Unlock()
	} else {
		cur.close()
		kept = true
	}
	r.afterSuccess(info)
	return res
}

// afterSuccess follows the transaction statements the user typed.
func (r *Runner) afterSuccess(info dialect.Info) {
	switch {
	case info.Begins:
		r.mu.Lock()
		r.inTx = true
		r.mu.Unlock()
	case info.Ends:
		r.mu.Lock()
		r.inTx = false
		r.mu.Unlock()
	}
}

// fail records the failure of a statement. A connection that the server closed is dropped.
func (r *Runner) fail(res *StatementResult, err error) {
	if r.cancelled.Load() {
		res.Cancelled = true
	}
	q := describeError(r.d.ID(), err, res.Statement.Text)
	res.Err = &q
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, dbsql.ErrConnDone) {
		r.drop()
		res.Err.Message = "The connection to the server was lost (" + res.Err.Message + "). It is opened again by the next run."
	}
}

// readSet reads the columns and the first rows of the result the cursor is at. It returns nil for a result without columns.
func (r *Runner) readSet(ctx context.Context, cur *cursor, opts RunOptions) (*ResultSet, error) {
	cols, err := cur.rows.Columns()
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, nil
	}
	ctypes, err := cur.rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	cur.types = make([]string, len(cols))
	gcols := make([]resultgrid.Column, len(cols))
	for i, c := range cols {
		cur.types[i] = ctypes[i].DatabaseTypeName()
		gcols[i] = resultgrid.Column{Name: c, Type: typeLabel(ctypes[i])}
	}
	set := &ResultSet{Columns: gcols, Table: resultgrid.NewTable(gcols)}
	cur.pending = nil
	if err := r.fill(ctx, cur, set, opts); err != nil {
		return nil, err
	}
	return set, nil
}

// fill reads up to MaxRows more rows into the set, within the bound on its bytes, and decides whether there are more.
func (r *Runner) fill(ctx context.Context, cur *cursor, set *ResultSet, opts RunOptions) error {
	dest := make([]any, len(set.Columns))
	ptrs := make([]any, len(dest))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	read := 0
	next := func() ([]resultgrid.Value, bool, error) {
		if cur.pending != nil {
			row := cur.pending
			cur.pending = nil
			return row, true, nil
		}
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		if !cur.rows.Next() {
			return nil, false, cur.rows.Err()
		}
		if err := cur.rows.Scan(ptrs...); err != nil {
			return nil, false, err
		}
		row := make([]resultgrid.Value, len(dest))
		for i, v := range dest {
			row[i] = convert(cur.engine, cur.types[i], v)
			dest[i] = nil
		}
		return row, true, nil
	}
	set.Note = ""
	for read < opts.MaxRows {
		row, ok, err := next()
		if err != nil {
			return err
		}
		if !ok {
			set.cur = nil
			return nil
		}
		var n int64
		for _, v := range row {
			n += size(v)
		}
		if set.bytes+n > opts.MaxBytes && read > 0 {
			set.cur = nil // the room is used up: the rest is not read, and there is nothing to fetch
			set.Note = "The result reached " + strconv.FormatInt(opts.MaxBytes>>20, 10) + " MB in memory; the rest is not read."
			return nil
		}
		set.Table.Append(row)
		set.bytes += n
		read++
	}
	// is there another row?
	row, ok, err := next()
	if err != nil {
		return err
	}
	if !ok {
		set.cur = nil
		return nil
	}
	cur.pending = row
	set.cur = cur
	return nil
}

// FetchMore reads the next rows of the last result set of the last run into the set. It does nothing for a set without a cursor. The
// errors have the tags SQLRunnerBusy and SQLFetchFailed.
func (r *Runner) FetchMore(ctx context.Context, set *ResultSet, opts RunOptions) error {
	if !set.More() {
		return nil
	}
	if err := r.acquire(); err != nil {
		return err
	}
	defer r.release()
	if opts.MaxRows <= 0 {
		opts.MaxRows = DefaultMaxRows
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = MaxResultBytes
	}
	cur := set.cur
	if err := r.fill(ctx, cur, set, opts); err != nil {
		cur.close()
		set.cur = nil
		r.mu.Lock()
		if r.last == cur {
			r.last = nil
		}
		r.mu.Unlock()
		return errors.NewErr("SQLFetchFailed", err).SetMetadata("connection", r.connID)
	}
	if set.cur == nil {
		r.closeCursor()
	}
	return nil
}

// control runs one of the transaction statements.
func (r *Runner) control(ctx context.Context, stmt string, inTx bool) error {
	if err := r.acquire(); err != nil {
		return err
	}
	defer r.release()
	r.closeCursor()
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	sess, err := r.session(ctx)
	if err != nil {
		return err
	}
	if _, err := sess.Conn().ExecContext(ctx, stmt); err != nil {
		q := describeError(r.d.ID(), err, stmt)
		return errors.NewErr("SQLTransactionFailed", scrubbed{err: err}).SetMetadata("connection", r.connID, "message", q.Text())
	}
	r.mu.Lock()
	r.inTx = inTx
	r.mu.Unlock()
	return nil
}

// Begin opens a transaction on the session. The errors have the tags SQLTransactionOpen, SQLRunnerBusy, SQLNotConnected and
// SQLTransactionFailed.
func (r *Runner) Begin(ctx context.Context) error {
	if r.InTransaction() {
		return errors.New("SQLTransactionOpen", "a transaction is open already", "connection", r.connID)
	}
	return r.control(ctx, r.d.Transaction().Begin, true)
}

// Commit commits the transaction. The errors have the tags SQLNoTransaction, SQLRunnerBusy and SQLTransactionFailed.
func (r *Runner) Commit(ctx context.Context) error {
	if !r.InTransaction() {
		return errors.New("SQLNoTransaction", "there is no open transaction", "connection", r.connID)
	}
	return r.control(ctx, r.d.Transaction().Commit, false)
}

// Rollback rolls the transaction back. The errors are those of Commit.
func (r *Runner) Rollback(ctx context.Context) error {
	if !r.InTransaction() {
		return errors.New("SQLNoTransaction", "there is no open transaction", "connection", r.connID)
	}
	return r.control(ctx, r.d.Transaction().Rollback, false)
}

// Close ends the session: an open transaction is rolled back first, and the connection goes back to the pool. rolledBack tells that
// there was a transaction. The runner can be used again; it takes a new session.
func (r *Runner) Close() (rolledBack bool, err error) {
	r.cancelled.Store(true)
	if r.Running() {
		_ = r.Cancel() // the statement ends with the session; a failure to cancel changes nothing
	}
	r.closeCursor()
	r.mu.Lock()
	sess, inTx := r.sess, r.inTx
	r.sess, r.inTx = nil, false
	r.mu.Unlock()
	if sess == nil {
		return false, nil
	}
	if inTx {
		ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
		defer cancel()
		if _, rerr := sess.Conn().ExecContext(ctx, r.d.Transaction().Rollback); rerr != nil {
			err = errors.NewErr("SQLTransactionFailed", scrubbed{err: rerr}).SetMetadata("connection", r.connID)
		} else {
			rolledBack = true
		}
	}
	if cerr := sess.Close(); cerr != nil && err == nil {
		err = cerr
	}
	return rolledBack, err
}

// typeLabel is the type of a column as a title shows it: the database's name for it, with the length when it has one.
func typeLabel(ct *dbsql.ColumnType) string {
	name := ct.DatabaseTypeName()
	if n, ok := ct.Length(); ok && n > 0 && n < 1<<20 {
		switch name {
		case "VARCHAR", "CHAR", "NVARCHAR", "NCHAR", "VARBINARY", "BINARY":
			return name + "(" + strconv.FormatInt(n, 10) + ")"
		}
	}
	if p, sc, ok := ct.DecimalSize(); ok && p > 0 {
		return name + "(" + strconv.FormatInt(p, 10) + "," + strconv.FormatInt(sc, 10) + ")"
	}
	return name
}
