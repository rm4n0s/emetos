package sql

import (
	"context"
	dbsql "database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" // registers "sqlite", which the fixture is written with

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// fixtureSchema is the SQLite database of the tests: two tables with a foreign key, a unique column, an index, a composite key
// without a rowid, a view, and a table with AUTOINCREMENT (which makes sqlite_sequence).
const fixtureSchema = `
CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT NOT NULL, born INTEGER DEFAULT 1900);
CREATE TABLE books (
  id INTEGER PRIMARY KEY,
  author_id INTEGER NOT NULL REFERENCES authors(id),
  title TEXT NOT NULL,
  isbn TEXT UNIQUE,
  price NUMERIC
);
CREATE INDEX books_title ON books(title);
CREATE TABLE tags (book_id INTEGER NOT NULL, tag TEXT NOT NULL, PRIMARY KEY (book_id, tag)) WITHOUT ROWID;
CREATE TABLE log (n INTEGER PRIMARY KEY AUTOINCREMENT, line TEXT);
CREATE VIEW book_titles AS SELECT b.title AS title, a.name AS author FROM books b JOIN authors a ON a.id = b.author_id;
INSERT INTO authors (name, born) VALUES ('Ursula', 1929), ('Iain', 1954);
INSERT INTO books (author_id, title, isbn, price) VALUES (1, 'The Dispossessed', '111', 9.5), (2, 'Excession', '222', 7);
INSERT INTO log (line) VALUES ('first');
`

// makeFixture writes the fixture database in a temporary folder and returns its path.
func makeFixture(t *testing.T) string {
	t.Helper()
	return makeFixtureAt(t, filepath.Join(t.TempDir(), "fixture.db"))
}

// makeFixtureAt writes the fixture database at the path.
func makeFixtureAt(t *testing.T, path string) string {
	t.Helper()
	db, err := dbsql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fixtureSchema); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// sqliteConn is a connection to a SQLite file.
func sqliteConn(path string) dbconn.Connection {
	return dbconn.Connection{ID: "lite1", Name: "fixture", Engine: "sqlite", Scope: dbconn.ScopeUser, File: path}
}

// openFixture opens the fixture through a Service and closes it when the test ends.
func openFixture(t *testing.T, mutate func(*dbconn.Connection)) (*Service, dbconn.Connection) {
	t.Helper()
	c := sqliteConn(makeFixture(t))
	if mutate != nil {
		mutate(&c)
	}
	svc := NewService()
	if err := svc.Open(context.Background(), c, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := svc.Close(c.ID); err != nil {
			t.Error(err)
		}
	})
	return svc, c
}

// ctxFor returns a context that ends with the test or after a minute.
func ctxFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// wantRoute checks that err is an error of the errors package whose route contains want.
func wantRoute(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want the route %s", want)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}

// labels lists the labels of nodes, with the description after a colon when there is one.
func labels(nodes []dbconn.Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Label
		if n.Description != "" {
			out[i] += ": " + n.Description
		}
	}
	return out
}

// child finds the node with the label among the children of parent (the connection's when parent is nil) and returns it.
func child(t *testing.T, svc *Service, c dbconn.Connection, parent *dbconn.Node, label string) dbconn.Node {
	t.Helper()
	nodes, err := svc.Children(ctxFor(t), c, parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Label == label {
			return n
		}
	}
	t.Fatalf("no node %q among %v", label, labels(nodes))
	return dbconn.Node{}
}

// walk follows a path of labels from the connection and returns the node at its end.
func walk(t *testing.T, svc *Service, c dbconn.Connection, path ...string) *dbconn.Node {
	t.Helper()
	var n *dbconn.Node
	for _, label := range path {
		next := child(t, svc, c, n, label)
		n = &next
	}
	return n
}

// list returns the children of the node at the end of the path.
func list(t *testing.T, svc *Service, c dbconn.Connection, path ...string) []dbconn.Node {
	t.Helper()
	nodes, err := svc.Children(ctxFor(t), c, walk(t, svc, c, path...))
	if err != nil {
		t.Fatal(err)
	}
	return nodes
}

// waitUntil waits for a condition that another goroutine makes true.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
