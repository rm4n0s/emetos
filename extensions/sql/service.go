package sql

import (
	"context"
	dbsql "database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// maxListed is the most rows of a catalog listing the explorer reads; a schema with more objects shows the first ones and says so.
const maxListed = 10000

// Service holds the open SQL connections of Emetos: one pool of connections to the server for each connection of the explorer (and
// one for each database of a PostgreSQL server that is browsed), opened with the password the user gave and closed when the
// connection is disconnected. The explorer, and the query editors of step 19.3, ask it for the catalog and for sessions. It is safe
// for use from several goroutines; every method that talks to a server takes a context and blocks, so it is called off the UI thread.
type Service struct {
	mu    sync.Mutex
	conns map[string]*openConn
	subs  map[int]func(id string, open bool)
	next  int
}

// NewService makes a service with no connection open.
func NewService() *Service {
	return &Service{conns: map[string]*openConn{}, subs: map[int]func(string, bool){}}
}

// openConn is one open connection.
type openConn struct {
	conn   dbconn.Connection
	target *target

	mu     sync.Mutex
	pools  map[string]*dbsql.DB
	closed bool
}

// Subscribe calls fn after a connection is opened (open) or closed, on the goroutine that did it, and returns the function that stops
// the calls. fn must not block.
func (s *Service) Subscribe(fn func(connID string, open bool)) (cancel func()) {
	s.mu.Lock()
	id := s.next
	s.next++
	s.subs[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
}

// notify tells the subscribers.
func (s *Service) notify(id string, open bool) {
	s.mu.Lock()
	fns := make([]func(string, bool), 0, len(s.subs))
	for _, fn := range s.subs {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(id, open)
	}
}

// Open opens a connection: it makes the pool, connects once to see that the settings and the password work, and keeps the pool for
// the explorer. A connection that is open already is closed first. c has its ${env:} references expanded already. The errors have
// the tags SQLEngineUnknown, SQLFileNotAbsolute, SQLConnectionURLInvalid, SQLConnectionSettingsInvalid, SQLTLSCAFileUnreadable,
// SQLTLSCAFileInvalid, SQLTLSRootsUnavailable, SQLConnectFailed and SQLSessionInitFailed; none carries the password.
func (s *Service) Open(ctx context.Context, c dbconn.Connection, password string) error {
	t, err := newTarget(ctx, c, password)
	if err != nil {
		return err
	}
	oc := &openConn{conn: c, target: t, pools: map[string]*dbsql.DB{}}
	pingCtx, cancel := context.WithTimeout(ctx, t.timeout+5*time.Second)
	defer cancel()
	db, err := oc.pool(pingCtx, "")
	if err != nil {
		return err
	}
	if err := db.PingContext(pingCtx); err != nil {
		_ = oc.close() // the connection never opened; its failure to close is of no use to the user
		return errors.NewErr("SQLConnectFailed", scrubbed{msg: "cannot connect", password: password, err: err}).SetMetadata("connection", c.Name, "engine", c.Engine)
	}
	s.mu.Lock()
	old := s.conns[c.ID]
	s.conns[c.ID] = oc
	s.mu.Unlock()
	if old != nil {
		if err := old.close(); err != nil {
			slog.Warn("a connection did not close when it was opened again", "connection", c.Name, "err", err)
		}
	}
	s.notify(c.ID, true)
	return nil
}

// Close closes the pools of a connection. Closing one that is not open does nothing. The error is the first one a pool gave.
func (s *Service) Close(connID string) error {
	s.mu.Lock()
	oc := s.conns[connID]
	delete(s.conns, connID)
	s.mu.Unlock()
	if oc == nil {
		return nil
	}
	err := oc.close()
	s.notify(connID, false)
	return err
}

// IsOpen tells whether the connection is open.
func (s *Service) IsOpen(connID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.conns[connID]
	return ok
}

// get returns an open connection.
func (s *Service) get(connID string) (*openConn, error) {
	s.mu.Lock()
	oc := s.conns[connID]
	s.mu.Unlock()
	if oc == nil {
		return nil, errors.New("SQLNotConnected", "the connection is not open", "connection", connID)
	}
	return oc, nil
}

// Dialect returns the dialect of an open connection.
func (s *Service) Dialect(connID string) (dialect.Dialect, error) {
	oc, err := s.get(connID)
	if err != nil {
		return nil, err
	}
	return oc.target.dialect, nil
}

// pool returns the pool for a database, opening it when it is the first time. An engine whose one connection reaches every database
// (all but PostgreSQL) has the one pool.
func (oc *openConn) pool(ctx context.Context, database string) (*dbsql.DB, error) {
	key := database
	if !oc.target.dialect.Levels().SeparateConnections || database == oc.target.defaultDB {
		key = ""
	}
	oc.mu.Lock()
	defer oc.mu.Unlock()
	if oc.closed {
		return nil, errors.New("SQLNotConnected", "the connection was closed", "connection", oc.conn.ID)
	}
	if db, ok := oc.pools[key]; ok {
		return db, nil
	}
	c, err := oc.target.connector(ctx, key)
	if err != nil {
		return nil, err
	}
	db := dbsql.OpenDB(c)
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	oc.pools[key] = db
	return db, nil
}

// close closes every pool and returns the first error.
func (oc *openConn) close() error {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	oc.closed = true
	var first error
	for key, db := range oc.pools {
		if err := db.Close(); err != nil && first == nil {
			first = errors.NewErr("SQLCloseFailed", err).SetMetadata("connection", oc.conn.Name)
		}
		delete(oc.pools, key)
	}
	return first
}

// DB returns the pool of a connection for a database ("" for the one it was opened on). Another database of a PostgreSQL server is
// opened the first time it is asked for.
func (s *Service) DB(ctx context.Context, connID, database string) (*dbsql.DB, error) {
	oc, err := s.get(connID)
	if err != nil {
		return nil, err
	}
	return oc.pool(ctx, database)
}

// Rows runs a catalog query on a database and returns its rows as text; truncated says that there were more than limit rows (limit
// zero is maxListed). The errors have the tags SQLNotConnected, SQLQueryFailed and SQLRowsFailed.
func (s *Service) Rows(ctx context.Context, connID, database string, q dialect.Query, limit int) (rows []dialect.Row, truncated bool, err error) {
	if q.Empty() {
		return nil, false, nil
	}
	if limit <= 0 {
		limit = maxListed
	}
	db, err := s.DB(ctx, connID, database)
	if err != nil {
		return nil, false, err
	}
	res, err := db.QueryContext(ctx, q.SQL, q.Args...)
	if err != nil {
		return nil, false, errors.NewErr("SQLQueryFailed", err).SetMetadata("connection", connID)
	}
	defer func() {
		if cerr := res.Close(); cerr != nil && err == nil {
			err = errors.NewErr("SQLRowsFailed", cerr).SetMetadata("connection", connID)
		}
	}()
	cols, err := res.Columns()
	if err != nil {
		return nil, false, errors.NewErr("SQLRowsFailed", err).SetMetadata("connection", connID)
	}
	for res.Next() {
		if len(rows) == limit {
			return rows, true, nil
		}
		row := make(dialect.Row, len(cols))
		dest := make([]any, len(cols))
		for i := range row {
			dest[i] = &row[i]
		}
		if err := res.Scan(dest...); err != nil {
			return nil, false, errors.NewErr("SQLRowsFailed", err).SetMetadata("connection", connID)
		}
		rows = append(rows, row)
	}
	if err := res.Err(); err != nil {
		return nil, false, errors.NewErr("SQLRowsFailed", err).SetMetadata("connection", connID)
	}
	return rows, false, nil
}

// Text runs a query that returns one text (a definition) and returns it; "" when the query returns no row.
func (s *Service) Text(ctx context.Context, connID, database string, q dialect.Query) (string, error) {
	rows, _, err := s.Rows(ctx, connID, database, q, 1)
	if err != nil || len(rows) == 0 {
		return "", err
	}
	row := rows[0]
	if q.Text >= len(row) || !row[q.Text].Valid {
		return "", nil
	}
	return row[q.Text].String, nil
}

// Session is one connection of the pool kept for a series of statements (a query editor, a transaction), with the means to cancel
// the statement that is running on it.
type Session struct {
	oc       *openConn
	database string
	conn     *dbsql.Conn
	id       string

	mu     sync.Mutex
	cancel context.CancelFunc
}

// Session takes a connection of the pool for the database. It is given back with Close. For an engine whose cancel needs the id of the
// session (Cancel.SessionID) the id is read first. The errors have the tags SQLNotConnected, SQLSessionFailed and SQLQueryFailed.
func (s *Service) Session(ctx context.Context, connID, database string) (*Session, error) {
	oc, err := s.get(connID)
	if err != nil {
		return nil, err
	}
	db, err := oc.pool(ctx, database)
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, errors.NewErr("SQLSessionFailed", err).SetMetadata("connection", connID)
	}
	sess := &Session{oc: oc, database: database, conn: conn}
	if q := oc.target.dialect.Cancel().SessionID; q != "" {
		var id dbsql.NullString
		if err := conn.QueryRowContext(ctx, q).Scan(&id); err != nil {
			_ = conn.Close() // the id could not be read; that error is the one to report
			return nil, errors.NewErr("SQLQueryFailed", err).SetMetadata("connection", connID)
		}
		sess.id = id.String
	}
	return sess, nil
}

// Conn is the connection of the session.
func (s *Session) Conn() *dbsql.Conn { return s.conn }

// Begin makes the context for one statement: Cancel ends it. The function it returns ends the statement's context and must be
// called when the statement is done.
func (s *Session) Begin(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	return ctx, func() {
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
		cancel()
	}
}

// Cancel stops the statement that is running on the session. Where the dialect has a statement for it (KILL QUERY, pg_cancel_backend)
// it is run on another connection of the pool, which leaves the session as it was; if that cannot be done, or the engine has none,
// the context of the statement is ended and the driver cancels it. The error is that of the cancelling statement when the context
// could not be ended either.
func (s *Session) Cancel() error {
	plan := s.oc.target.dialect.Cancel()
	var killErr error
	if plan.Kill != nil && s.id != "" {
		ctx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		db, err := s.oc.pool(ctx, s.database)
		if err == nil {
			_, err = db.ExecContext(ctx, plan.Kill(s.id))
		}
		if err == nil {
			return nil
		}
		killErr = errors.NewErr("SQLCancelFailed", err).SetMetadata("connection", s.oc.conn.Name)
	}
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel == nil {
		return killErr
	}
	cancel()
	return nil
}

// Close gives the connection back to the pool.
func (s *Session) Close() error {
	if err := s.conn.Close(); err != nil {
		return errors.NewErr("SQLSessionFailed", err).SetMetadata("connection", s.oc.conn.Name)
	}
	return nil
}
