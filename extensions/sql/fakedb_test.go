package sql

import (
	"context"
	dbsql "database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// fakeDB is a database/sql driver that does what a test tells it to, to reach the failures a real database does not give on demand: a
// connection that will not close, a result that breaks half way, a cancel statement the server refuses.
type fakeDB struct {
	mu sync.Mutex
	// connectErr, closeErr, queryErr and nextErr are what the connection does when asked.
	connectErr, closeErr, queryErr, nextErr error
	// failQuery makes the statements that contain the text fail with queryErr.
	failQuery string
	// rows are the rows every query returns, one text each.
	rows []string
	// ran are the statements that were run.
	ran []string
}

// connector returns the driver.Connector of the fake.
func (f *fakeDB) connector() driver.Connector { return fakeConnector2{f} }

// ranStatements returns what was run.
func (f *fakeDB) ranStatements() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ran...)
}

type fakeConnector2 struct{ db *fakeDB }

func (c fakeConnector2) Connect(context.Context) (driver.Conn, error) {
	if c.db.connectErr != nil {
		return nil, c.db.connectErr
	}
	return &fakeSQLConn{db: c.db}, nil
}

func (c fakeConnector2) Driver() driver.Driver { return nil }

type fakeSQLConn struct{ db *fakeDB }

func (c *fakeSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("FakeDB", "no prepared statements")
}
func (c *fakeSQLConn) Close() error              { return c.db.closeErr }
func (c *fakeSQLConn) Begin() (driver.Tx, error) { return nil, errors.New("FakeDB", "no transactions") }

// QueryContext implements driver.QueryerContext.
func (c *fakeSQLConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.db.mu.Lock()
	c.db.ran = append(c.db.ran, query)
	c.db.mu.Unlock()
	if c.db.queryErr != nil && (c.db.failQuery == "" || strings.Contains(query, c.db.failQuery)) {
		return nil, c.db.queryErr
	}
	return &fakeRows{db: c.db}, nil
}

// ExecContext implements driver.ExecerContext.
func (c *fakeSQLConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.db.mu.Lock()
	c.db.ran = append(c.db.ran, query)
	c.db.mu.Unlock()
	if c.db.queryErr != nil && (c.db.failQuery == "" || strings.Contains(query, c.db.failQuery)) {
		return nil, c.db.queryErr
	}
	return driver.RowsAffected(0), nil
}

type fakeRows struct {
	db *fakeDB
	at int
}

func (r *fakeRows) Columns() []string { return []string{"value"} }
func (r *fakeRows) Close() error      { return nil }

// Next implements driver.Rows: the rows, then io.EOF, or nextErr after the first row.
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.db.nextErr != nil && r.at >= 1 {
		return r.db.nextErr
	}
	if r.at >= len(r.db.rows) {
		return io.EOF
	}
	dest[0] = r.db.rows[r.at]
	r.at++
	return nil
}

// openFake puts a connection that runs on the fake into a service, as Service.Open would, with the dialect of the engine.
func openFake(t *testing.T, f *fakeDB, engine string) (*Service, dbconn.Connection) {
	t.Helper()
	d, ok := dialect.ByID(engine)
	if !ok {
		t.Fatalf("no dialect %q", engine)
	}
	c := dbconn.Connection{ID: "fake", Name: "fake", Engine: engine}
	svc := NewService()
	oc := &openConn{conn: c, target: &target{dialect: d, connector: func(context.Context, string) (driver.Connector, error) { return f.connector(), nil }}, pools: map[string]*dbsql.DB{}}
	svc.conns[c.ID] = oc
	t.Cleanup(func() { _ = svc.Close(c.ID) }) // a test that makes it fail on purpose looked at the error already
	return svc, c
}

func TestAConnectionThatWillNotCloseReportsIt(t *testing.T) {
	f := &fakeDB{closeErr: errors.New("FakeDB", "the socket is stuck"), rows: []string{"a"}}
	svc, c := openFake(t, f, "mysql")
	if _, _, err := svc.Rows(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT 1"}, 0); err != nil {
		t.Fatal(err)
	}
	wantRoute(t, svc.Close(c.ID), "openConn.close.SQLCloseFailed")
	if svc.IsOpen(c.ID) {
		t.Error("a connection that did not close stays listed")
	}
}

func TestAResultThatBreaksHalfWayIsReported(t *testing.T) {
	tests := []struct {
		name string
		f    *fakeDB
		want string
	}{
		{"a statement the server refuses", &fakeDB{queryErr: errors.New("FakeDB", "syntax error")}, "Service.Rows.SQLQueryFailed"},
		{"a row that cannot be read", &fakeDB{rows: []string{"a", "b"}, nextErr: errors.New("FakeDB", "connection reset")}, "Service.Rows.SQLRowsFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, c := openFake(t, tt.f, "postgres")
			_, _, err := svc.Rows(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT 1"}, 0)
			wantRoute(t, err, tt.want)
		})
	}
}

func TestASessionThatCannotBeTakenIsReported(t *testing.T) {
	f := &fakeDB{connectErr: errors.New("FakeDB", "too many connections")}
	svc, c := openFake(t, f, "postgres")
	_, err := svc.Session(ctxFor(t), c.ID, "")
	wantRoute(t, err, "Service.Session.SQLSessionFailed")

	// the id of the session is read first, and a failure to read it is a failed query
	f2 := &fakeDB{queryErr: errors.New("FakeDB", "no such function"), failQuery: "pg_backend_pid"}
	svc2, c2 := openFake(t, f2, "postgres")
	_, err = svc2.Session(ctxFor(t), c2.ID, "")
	wantRoute(t, err, "Service.Session.SQLQueryFailed")
}

func TestACancelThatTheServerRefusesFallsBackToTheContext(t *testing.T) {
	f := &fakeDB{rows: []string{"4242"}, queryErr: errors.New("FakeDB", "permission denied"), failQuery: "KILL QUERY"}
	svc, c := openFake(t, f, "mysql")
	sess, err := svc.Session(ctxFor(t), c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sess.Close(); err != nil {
			t.Error(err)
		}
	}()
	if sess.id != "4242" {
		t.Fatalf("session id = %q", sess.id)
	}
	// with a statement running, the context is ended and Cancel has done its job
	ctx, done := sess.Begin(ctxFor(t))
	if err := sess.Cancel(); err != nil {
		t.Errorf("Cancel with a context to end: %v", err)
	}
	if ctx.Err() == nil {
		t.Error("the context of the statement was not ended")
	}
	done()
	// with nothing running, the failure of the cancelling statement is what is reported
	wantRoute(t, sess.Cancel(), "Session.Cancel.SQLCancelFailed")
	if ran := f.ranStatements(); !strings.Contains(ran[len(ran)-1], "KILL QUERY 4242") {
		t.Errorf("ran %v", ran)
	}
}

func TestAConnectionThatCannotMakeItsPoolIsReported(t *testing.T) {
	svc := NewService()
	d := dialect.Postgres{}
	oc := &openConn{
		conn: dbconn.Connection{ID: "x", Name: "x"},
		target: &target{dialect: d, connector: func(context.Context, string) (driver.Connector, error) {
			return nil, errors.New("FakeDB", "bad settings")
		}},
		pools: map[string]*dbsql.DB{},
	}
	svc.conns["x"] = oc
	_, err := svc.DB(ctxFor(t), "x", "")
	if err == nil {
		t.Fatal("no error")
	}
	if _, ok := errors.FromError(err); !ok || err.Error() != "bad settings" {
		t.Errorf("err = %v", err)
	}
}
