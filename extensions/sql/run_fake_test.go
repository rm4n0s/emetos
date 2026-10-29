package sql

import (
	"context"
	dbsql "database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"sync"
	"testing"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// scriptSet is one result set a scripted statement returns.
type scriptSet struct {
	cols []string
	// types are the database type names of the columns; "VARCHAR" when not given.
	types []string
	rows  [][]driver.Value
	// errAt, when err is set, is the row at which reading fails with err.
	errAt int
	err   error
}

// scriptReply is what the scripted server answers to a statement that contains the text.
type scriptReply struct {
	contains string
	sets     []scriptSet
	affected int64
	err      error
}

// scriptDB is a fake server for database/sql that answers the statements it is told about, with several result sets, rows
// affected or an error, and records what it was asked. It stands in for engines the tests do not start (SQL Server).
type scriptDB struct {
	mu      sync.Mutex
	replies []scriptReply
	ran     []string
	// holdOn, when set, makes the statements that contain the text wait until a KILL QUERY arrives or their context ends; the
	// statement then fails as the engine's does when it is killed.
	holdOn string
	hold   chan struct{}
	killed sync.Once
	// quietKill makes a killed statement end without an error, as MySQL's SLEEP does when KILL QUERY interrupts it.
	quietKill bool
}

func (s *scriptDB) log() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ran...)
}

func (s *scriptDB) find(query string) (scriptReply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ran = append(s.ran, query)
	if strings.HasPrefix(query, "KILL QUERY") && s.hold != nil {
		s.killed.Do(func() { close(s.hold) })
	}
	for _, r := range s.replies {
		if strings.Contains(query, r.contains) {
			return r, true
		}
	}
	if strings.Contains(query, "CONNECTION_ID") || strings.Contains(query, "pg_backend_pid") {
		return scriptReply{sets: []scriptSet{{cols: []string{"id"}, rows: [][]driver.Value{{int64(1)}}}}}, true
	}
	return scriptReply{}, false
}

type scriptConnector struct{ db *scriptDB }

func (c scriptConnector) Connect(context.Context) (driver.Conn, error) {
	return &scriptConn{db: c.db}, nil
}
func (c scriptConnector) Driver() driver.Driver { return nil }

type scriptConn struct{ db *scriptDB }

func (c *scriptConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("FakeDB", "no prepare")
}
func (c *scriptConn) Close() error              { return nil }
func (c *scriptConn) Begin() (driver.Tx, error) { return nil, errors.New("FakeDB", "no begin") }

func (c *scriptConn) wait(ctx context.Context, query string) error {
	if c.db.hold == nil || c.db.holdOn == "" || !strings.Contains(query, c.db.holdOn) {
		return nil
	}
	select {
	case <-c.db.hold:
		if c.db.quietKill {
			return nil
		}
		return errors.New("FakeDB", "Query execution was interrupted")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *scriptConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	r, _ := c.db.find(query)
	if err := c.wait(ctx, query); err != nil {
		return nil, err
	}
	if r.err != nil {
		return nil, r.err
	}
	return &scriptRows{sets: r.sets}, nil
}

func (c *scriptConn) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	r, _ := c.db.find(query)
	if err := c.wait(ctx, query); err != nil {
		return nil, err
	}
	if r.err != nil {
		return nil, r.err
	}
	return driver.RowsAffected(r.affected), nil
}

type scriptRows struct {
	sets []scriptSet
	set  int
	row  int
}

func (r *scriptRows) cur() scriptSet {
	if r.set < len(r.sets) {
		return r.sets[r.set]
	}
	return scriptSet{}
}
func (r *scriptRows) Columns() []string { return r.cur().cols }
func (r *scriptRows) Close() error      { return nil }
func (r *scriptRows) Next(dest []driver.Value) error {
	set := r.cur()
	rows := set.rows
	if set.err != nil && r.row >= set.errAt {
		return set.err
	}
	if r.row >= len(rows) {
		return io.EOF
	}
	copy(dest, rows[r.row])
	r.row++
	return nil
}
func (r *scriptRows) HasNextResultSet() bool { return r.set+1 < len(r.sets) }
func (r *scriptRows) NextResultSet() error {
	if r.set+1 >= len(r.sets) {
		return io.EOF
	}
	r.set++
	r.row = 0
	return nil
}
func (r *scriptRows) ColumnTypeDatabaseTypeName(i int) string {
	if t := r.cur().types; i < len(t) {
		return t[i]
	}
	return "VARCHAR"
}

// openScripted puts a connection of the engine that runs on the script into a service.
func openScripted(t *testing.T, db *scriptDB, engine string) (*Service, dbconn.Connection) {
	t.Helper()
	d, ok := dialect.ByID(engine)
	if !ok {
		t.Fatalf("no dialect %q", engine)
	}
	c := dbconn.Connection{ID: "script", Name: "script", Engine: engine}
	svc := NewService()
	oc := &openConn{conn: c, target: &target{dialect: d, connector: func(context.Context, string) (driver.Connector, error) { return scriptConnector{db}, nil }}, pools: map[string]*dbsql.DB{}}
	svc.conns[c.ID] = oc
	t.Cleanup(func() { _ = svc.Close(c.ID) }) // the fake closes without error
	return svc, c
}

func TestSQLServerBatchesRunOneAfterTheOtherAndRepeat(t *testing.T) {
	db := &scriptDB{}
	svc, c := openScripted(t, db, "sqlserver")
	r, err := svc.NewRunner(c.ID, "shop")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = r.Close() }) // nothing is open
	script := "SELECT 1; SELECT 2\nGO 2\nSELECT 3\nGO\n"
	res, err := r.Run(ctxFor(t), dialect.Split(r.d, script), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(db.log(), "|")
	// the session first moves to the database, then the first batch twice, then the second
	if want := "USE [shop]|SELECT 1|SELECT 2|SELECT 1|SELECT 2|SELECT 3"; got != want {
		t.Errorf("ran %q, want %q", got, want)
	}
	if len(res) != 5 {
		t.Errorf("results = %d", len(res))
	}
}

func TestSeveralResultSetsOfOneStatement(t *testing.T) {
	db := &scriptDB{replies: []scriptReply{{contains: "EXEC report", sets: []scriptSet{
		{cols: []string{"a"}, rows: [][]driver.Value{{int64(1)}, {int64(2)}}},
		{cols: []string{"b", "c"}, types: []string{"UNIQUEIDENTIFIER", "NVARCHAR"}, rows: [][]driver.Value{{[]byte{0x6f, 0x96, 0x19, 0xff, 0x8b, 0x86, 0xd0, 0x11, 0xb4, 0x2d, 0x00, 0xc0, 0x4f, 0xc9, 0x64, 0xff}, "x"}}},
	}}}}
	svc, c := openScripted(t, db, "sqlserver")
	r, err := svc.NewRunner(c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = r.Close() }) // nothing is open
	res := runScript(t, r, "EXEC report", RunOptions{})
	if len(res) != 1 || len(res[0].Sets) != 2 {
		t.Fatalf("results = %+v", res)
	}
	if res[0].Sets[0].Table.Len() != 2 || len(res[0].Sets[1].Columns) != 2 {
		t.Errorf("sets = %+v", res[0].Sets)
	}
	if got := setCell(res[0].Sets[1], 0, 0); got != "FF19966F-868B-11D0-B42D-00C04FC964FF" {
		t.Errorf("a GUID is %q", got)
	}
}

func TestSQLServerErrorsSayTheLine(t *testing.T) {
	db := &scriptDB{replies: []scriptReply{{contains: "SELEC", err: mssql.Error{Number: 102, Message: "Incorrect syntax near 'SELEC'.", LineNo: 2}}}}
	svc, c := openScripted(t, db, "sqlserver")
	r, err := svc.NewRunner(c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = r.Close() }) // nothing is open
	res := runScript(t, r, "SELECT 1,\n  SELEC", RunOptions{})
	e := res[0].Err
	if e == nil || e.Code != "102" || !strings.Contains(e.Message, "Incorrect syntax") || e.Offset != len("SELECT 1,\n  ") || e.Length != len("SELEC") {
		t.Errorf("error = %+v", e)
	}
	if e != nil && e.Text() != "102: "+e.Message {
		t.Errorf("text = %q", e.Text())
	}
}

func TestRowsAffectedAndTheDatabaseOfASessionOnMySQL(t *testing.T) {
	db := &scriptDB{replies: []scriptReply{{contains: "UPDATE", affected: 4}}}
	svc, c := openScripted(t, db, "mysql")
	r, err := svc.NewRunner(c.ID, "shop")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = r.Close() }) // nothing is open
	res := runScript(t, r, "UPDATE t SET a = 1 WHERE b = 2", RunOptions{})
	if !res[0].HasAffected || res[0].Affected != 4 {
		t.Errorf("result = %+v", res[0])
	}
	if got := db.log()[1]; got != "USE `shop`" { // after the read of the session's id
		t.Errorf("the session started with %q", db.log())
	}
}

func TestAConnectionThatIsLostIsOpenedAgainByTheNextRun(t *testing.T) {
	db := &scriptDB{replies: []scriptReply{{contains: "BOOM", err: driver.ErrBadConn}}}
	svc, c := openScripted(t, db, "postgres")
	r, err := svc.NewRunner(c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = r.Close() }) // nothing is open
	res := runScript(t, r, "BEGIN; SELECT 'BOOM'", RunOptions{})
	if len(res) != 2 || res[1].Err == nil || !strings.Contains(res[1].Err.Message, "lost") {
		t.Fatalf("results = %+v", res)
	}
	if r.InTransaction() {
		t.Error("a transaction on a lost connection is still open")
	}
	res = runScript(t, r, "SELECT 1", RunOptions{})
	if res[0].Err != nil {
		t.Errorf("the next run: %v", res[0].Err.Text())
	}
}

func TestCancellingOnAnEngineThatKillsFromAnotherConnection(t *testing.T) {
	db := &scriptDB{holdOn: "SLEEP", hold: make(chan struct{}), replies: []scriptReply{
		{contains: "CONNECTION_ID", sets: []scriptSet{{cols: []string{"id"}, rows: [][]driver.Value{{int64(77)}}}}},
	}}
	svc, c := openScripted(t, db, "mysql")
	r, err := svc.NewRunner(c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = r.Close() }) // nothing is open
	done := make(chan []*StatementResult, 1)
	go func() {
		res, _ := r.Run(context.Background(), dialect.Split(r.d, "SELECT SLEEP(100)"), RunOptions{})
		done <- res
	}()
	waitUntil(t, "the statement to wait", func() bool {
		for _, q := range db.log() {
			if strings.Contains(q, "SLEEP") {
				return true
			}
		}
		return false
	})
	if err := r.Cancel(); err != nil {
		t.Fatal(err)
	}
	res := <-done
	if len(res) != 1 || !res[0].Cancelled || res[0].Err == nil {
		t.Fatalf("results = %+v", res)
	}
	killed := false
	for _, q := range db.log() {
		killed = killed || q == "KILL QUERY 77"
	}
	if !killed {
		t.Errorf("no KILL QUERY 77 in %v", db.log())
	}
}

func TestAStatementTheServerEndsQuietlyAfterACancelStillStopsTheRun(t *testing.T) {
	db := &scriptDB{holdOn: "SLEEP", hold: make(chan struct{}), quietKill: true, replies: []scriptReply{
		{contains: "CONNECTION_ID", sets: []scriptSet{{cols: []string{"id"}, rows: [][]driver.Value{{int64(77)}}}}},
		{contains: "SLEEP", sets: []scriptSet{{cols: []string{"s"}, rows: [][]driver.Value{{int64(1)}}}}},
	}}
	svc, c := openScripted(t, db, "mysql")
	r, err := svc.NewRunner(c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = r.Close() })
	done := make(chan []*StatementResult, 1)
	go func() {
		res, _ := r.Run(context.Background(), dialect.Split(r.d, "SELECT SLEEP(100); SELECT 'never'"), RunOptions{})
		done <- res
	}()
	waitUntil(t, "the statement to wait", func() bool {
		for _, q := range db.log() {
			if strings.Contains(q, "SLEEP") {
				return true
			}
		}
		return false
	})
	if err := r.Cancel(); err != nil {
		t.Fatal(err)
	}
	res := <-done
	if len(res) != 1 || !res[0].Cancelled || res[0].Err == nil {
		t.Fatalf("results = %+v", res)
	}
}

func TestAFailedUseOfTheDatabaseIsReported(t *testing.T) {
	db := &scriptDB{replies: []scriptReply{{contains: "USE `shop`", err: errors.New("FakeDB", "unknown database 'shop'")}}}
	svc, c := openScripted(t, db, "mysql")
	r, err := svc.NewRunner(c.ID, "shop")
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Run(ctxFor(t), dialect.Split(r.d, "SELECT 1"), RunOptions{})
	wantRoute(t, err, "Runner.session.SQLUseDatabaseFailed")
	// nothing was kept: the next try starts again
	if r.sess != nil {
		t.Error("a session without its database was kept")
	}
}

func TestATransactionStatementTheServerRefuses(t *testing.T) {
	db := &scriptDB{replies: []scriptReply{{contains: "START TRANSACTION", err: errors.New("FakeDB", "not now")}}}
	svc, c := openScripted(t, db, "mysql")
	r, err := svc.NewRunner(c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = r.Close() }) // nothing is open
	wantRoute(t, r.Begin(ctxFor(t)), "Runner.Begin->Runner.control.SQLTransactionFailed")
	if r.InTransaction() {
		t.Error("a refused BEGIN opened a transaction")
	}
	// a rollback the server refuses when the runner is closed
	db2 := &scriptDB{replies: []scriptReply{{contains: "ROLLBACK", err: errors.New("FakeDB", "gone away")}}}
	svc2, c2 := openScripted(t, db2, "postgres")
	r2, err := svc2.NewRunner(c2.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r2.Begin(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	rolled, err := r2.Close()
	if rolled {
		t.Error("a refused rollback counts as done")
	}
	wantRoute(t, err, "Runner.Close.SQLTransactionFailed")
}

func TestRowsThatBreakWhileFetchingMore(t *testing.T) {
	rows := [][]driver.Value{{int64(1)}, {int64(2)}, {int64(3)}, {int64(4)}}
	db := &scriptDB{replies: []scriptReply{{contains: "BIG", sets: []scriptSet{{cols: []string{"n"}, rows: rows, errAt: 3, err: errors.New("FakeDB", "connection reset")}}}}}
	svc, c := openScripted(t, db, "postgres")
	r, err := svc.NewRunner(c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = r.Close() }) // the cursor is closed already
	res := runScript(t, r, "SELECT 'BIG'", RunOptions{MaxRows: 2})
	set := res[0].Sets[0]
	if res[0].Err != nil || !set.More() || set.Table.Len() != 2 {
		t.Fatalf("first read: %+v, %d rows, more %v", res[0].Err, set.Table.Len(), set.More())
	}
	wantRoute(t, r.FetchMore(ctxFor(t), set, RunOptions{MaxRows: 2}), "Runner.FetchMore.SQLFetchFailed")
	if set.More() {
		t.Error("a set whose cursor broke offers more")
	}
	// the rows that broke before the first rows were read are the statement's error
	db.replies[0].sets[0].errAt = 0
	res = runScript(t, r, "SELECT 'BIG'", RunOptions{MaxRows: 2})
	if res[0].Err == nil || !strings.Contains(res[0].Err.Message, "connection reset") {
		t.Errorf("err = %+v", res[0].Err)
	}
}
