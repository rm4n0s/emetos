package sql

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

func TestTheExplorerListsTheSchemaOfASQLiteFile(t *testing.T) {
	svc, c := openFixture(t, nil)
	tests := []struct {
		name string
		path []string
		want []string
	}{
		{"the schemas", nil, []string{"main"}},
		{"the folders of a schema", []string{"main"}, []string{"Tables", "Views"}},
		{"the tables", []string{"main", "Tables"}, []string{"authors", "books", "log", "tags"}},
		{"the views", []string{"main", "Views"}, []string{"book_titles"}},
		{"what is in a table", []string{"main", "Tables", "books"}, []string{"Columns", "Indexes", "Foreign Keys"}},
		{"what is in a view", []string{"main", "Views", "book_titles"}, []string{"Columns"}},
		{"the columns", []string{"main", "Tables", "books", "Columns"}, []string{
			"id: INTEGER NOT NULL", "author_id: INTEGER NOT NULL", "title: TEXT NOT NULL", "isbn: TEXT", "price: NUMERIC",
		}},
		{"the columns of a table with a default", []string{"main", "Tables", "authors", "Columns"}, []string{
			"id: INTEGER NOT NULL", "name: TEXT NOT NULL", "born: INTEGER",
		}},
		{"the columns of a view", []string{"main", "Views", "book_titles", "Columns"}, []string{"title: TEXT", "author: TEXT"}},
		{"the indexes", []string{"main", "Tables", "books", "Indexes"}, []string{
			"books_title: (title)", "sqlite_autoindex_books_1: (isbn) unique",
		}},
		{"the foreign keys", []string{"main", "Tables", "books", "Foreign Keys"}, []string{"fk0: (author_id) -> authors (id)"}},
		{"a composite key without a rowid", []string{"main", "Tables", "tags", "Indexes"}, []string{"sqlite_autoindex_tags_1: (book_id, tag) primary key"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := labels(list(t, svc, c, tt.path...))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestColumnsShowTheirKeysAndTypes(t *testing.T) {
	svc, c := openFixture(t, nil)
	nodes := list(t, svc, c, "main", "Tables", "books", "Columns")
	icons := map[string]string{}
	for _, n := range nodes {
		icons[n.Label] = n.Icon
	}
	want := map[string]string{"id": "key", "author_id": "references", "title": "symbol-field", "isbn": "symbol-field", "price": "symbol-field"}
	if !reflect.DeepEqual(icons, want) {
		t.Errorf("icons = %v, want %v", icons, want)
	}
	for _, n := range nodes {
		switch n.Label {
		case "id":
			if !strings.Contains(n.Tooltip, "primary key") {
				t.Errorf("the tooltip of id = %q", n.Tooltip)
			}
		case "author_id":
			if !strings.Contains(n.Tooltip, "foreign key") {
				t.Errorf("the tooltip of author_id = %q", n.Tooltip)
			}
		}
	}
	authors := list(t, svc, c, "main", "Tables", "authors", "Columns")
	if !strings.Contains(authors[2].Tooltip, "default 1900") {
		t.Errorf("the tooltip of born = %q", authors[2].Tooltip)
	}
}

func TestSystemTablesAreListedOnlyWhenAsked(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*dbconn.Connection)
		want   []string
	}{
		{"hidden", nil, []string{"authors", "books", "log", "tags"}},
		{"shown", func(c *dbconn.Connection) { c.Options = map[string]string{OptionShowSystem: "true"} }, []string{"authors", "books", "log", "sqlite_sequence", "tags"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, c := openFixture(t, tt.mutate)
			if got := labels(list(t, svc, c, "main", "Tables")); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tables = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNodesKnowWhatTheyAre(t *testing.T) {
	svc, c := openFixture(t, nil)
	tests := []struct {
		path       []string
		kind       string
		icon       string
		expandable bool
	}{
		{[]string{"main"}, NodeSchema, "symbol-namespace", true},
		{[]string{"main", "Tables"}, NodeFolder, "table", true},
		{[]string{"main", "Views"}, NodeFolder, "eye", true},
		{[]string{"main", "Tables", "books"}, NodeTable, "table", true},
		{[]string{"main", "Views", "book_titles"}, NodeView, "eye", true},
		{[]string{"main", "Tables", "books", "Indexes"}, NodeFolder, "list-flat", true},
		{[]string{"main", "Tables", "books", "Indexes", "books_title"}, NodeIndex, "list-flat", false},
		{[]string{"main", "Tables", "books", "Foreign Keys", "fk0"}, NodeForeignKey, "references", false},
		{[]string{"main", "Tables", "books", "Columns", "title"}, NodeColumn, "symbol-field", false},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.path, "/"), func(t *testing.T) {
			n := walk(t, svc, c, tt.path...)
			if n.Kind != tt.kind || n.Icon != tt.icon || n.Expandable != tt.expandable {
				t.Errorf("node = kind %q icon %q expandable %v, want %q %q %v", n.Kind, n.Icon, n.Expandable, tt.kind, tt.icon, tt.expandable)
			}
		})
	}
	if nodes := list(t, svc, c, "main", "Tables", "books", "Columns", "title"); nodes != nil {
		t.Errorf("a column has children: %v", nodes)
	}
}

func TestNodesOfOtherSourcesAreRefused(t *testing.T) {
	svc, c := openFixture(t, nil)
	_, err := svc.Children(ctxFor(t), c, &dbconn.Node{ID: "x", Kind: NodeTable, Data: "not ours"})
	wantRoute(t, err, "Service.Children.SQLNodeUnknown")
	_, err = svc.Children(ctxFor(t), c, &dbconn.Node{ID: "x", Kind: NodeFolder, Data: nodeData{Folder: "nothing"}})
	wantRoute(t, err, "Service.folder.SQLNodeUnknown")
}

func TestStatementsAreGeneratedFromTheCatalog(t *testing.T) {
	svc, c := openFixture(t, nil)
	ctx := ctxFor(t)
	d := dialect.SQLite{}
	books := dialect.Ref{Schema: "main", Name: "books"}
	table, err := svc.LoadTable(ctx, c.ID, dialect.KindTable, books)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"select", dialect.GenerateSelect(d, table), `SELECT
    "id",
    "author_id",
    "title",
    "isbn",
    "price"
FROM "books";
`},
		{"update", dialect.GenerateUpdate(d, table), `UPDATE "books"
SET
    "author_id" = NULL, -- author_id INTEGER not null
    "title" = NULL, -- title TEXT not null
    "isbn" = NULL, -- isbn TEXT
    "price" = NULL  -- price NUMERIC
WHERE "id" = NULL;
`},
		{"delete", dialect.GenerateDelete(d, table), "DELETE FROM \"books\"\nWHERE \"id\" = NULL;\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", tt.got, tt.want)
			}
		})
	}
	create, err := svc.Create(ctx, c.ID, dialect.KindTable, books)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(create, "CREATE TABLE books (") || !strings.HasSuffix(create, ");\n") || !strings.Contains(create, "REFERENCES authors(id)") {
		t.Errorf("the table's own statement is not what SQLite kept:\n%s", create)
	}
	view, err := svc.Create(ctx, c.ID, dialect.KindView, dialect.Ref{Schema: "main", Name: "book_titles"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(view, "CREATE VIEW book_titles AS SELECT") {
		t.Errorf("view = %s", view)
	}
	none, err := svc.Create(ctx, c.ID, dialect.KindView, dialect.Ref{Schema: "main", Name: "missing"})
	if err != nil || !strings.Contains(none, "no definition") {
		t.Errorf("a view that is not there: %q, %v", none, err)
	}
}

func TestAReadOnlyConnectionRefusesToWrite(t *testing.T) {
	tests := []struct {
		name     string
		readOnly bool
		wantErr  bool
	}{
		{"read-write", false, false},
		{"read-only", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, c := openFixture(t, func(c *dbconn.Connection) { c.ReadOnly = tt.readOnly })
			db, err := svc.DB(ctxFor(t), c.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.ExecContext(ctxFor(t), "INSERT INTO authors (name) VALUES ('Terry')")
			if (err != nil) != tt.wantErr {
				t.Errorf("insert error = %v, want an error: %v", err, tt.wantErr)
			}
			// reading works in both
			if rows, _, err := svc.Rows(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT count(*) FROM authors"}, 0); err != nil || len(rows) != 1 {
				t.Errorf("read: %v %v", rows, err)
			}
			// the statements of the session make it read-only on every connection of the pool, not only the one the file opened
			if tt.readOnly {
				sess, err := svc.Session(ctxFor(t), c.ID, "")
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := sess.Close(); err != nil {
						t.Error(err)
					}
				}()
				var on int
				if err := sess.Conn().QueryRowContext(ctxFor(t), "PRAGMA query_only").Scan(&on); err != nil || on != 1 {
					t.Errorf("query_only = %d, %v", on, err)
				}
			}
		})
	}
}

func TestOpeningFails(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		c    dbconn.Connection
		want string
	}{
		{"a file that is not there", dbconn.Connection{ID: "a", Name: "missing", Engine: "sqlite", File: dir + "/nothing.db"}, "Service.Open.SQLConnectFailed"},
		{"a relative path", dbconn.Connection{ID: "b", Name: "relative", Engine: "sqlite", File: "fixture.db"}, "newTarget->sqliteTarget->sqliteDSN.SQLFileNotAbsolute"},
		{"an engine nobody serves", dbconn.Connection{ID: "c", Name: "oracle", Engine: "oracle"}, "newTarget.SQLEngineUnknown"},
		{"a server that is not there", dbconn.Connection{ID: "d", Name: "nobody", Engine: "postgres", Host: "127.0.0.1", Port: 1, User: "u"}, "Service.Open.SQLConnectFailed"},
		{"a URL that cannot be read", dbconn.Connection{ID: "e", Name: "bad url", Engine: "postgres", URL: "postgres://host:notaport/db"}, "resolve.SQLConnectionURLInvalid"},
		{"a CA file that is not there", dbconn.Connection{ID: "f", Name: "no ca", Engine: "mysql", Host: "127.0.0.1", Port: 1, TLS: dbconn.TLSVerifyFull, CAFile: dir + "/ca.pem"}, "rootPool.SQLTLSCAFileUnreadable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService()
			err := svc.Open(ctxFor(t), tt.c, "")
			wantRoute(t, err, tt.want)
			if svc.IsOpen(tt.c.ID) {
				t.Error("a connection that failed to open is listed as open")
			}
		})
	}
}

func TestACloseThatIsNotOpenDoesNothingAndAnUnopenedConnectionCannotBeUsed(t *testing.T) {
	svc := NewService()
	if err := svc.Close("nothing"); err != nil {
		t.Errorf("Close of a connection that is not open: %v", err)
	}
	_, err := svc.Children(ctxFor(t), dbconn.Connection{ID: "nothing"}, nil)
	wantRoute(t, err, "Service.get.SQLNotConnected")
	_, err = svc.Session(ctxFor(t), "nothing", "")
	wantRoute(t, err, "Service.get.SQLNotConnected")
	_, _, err = svc.Rows(ctxFor(t), "nothing", "", dialect.Query{SQL: "SELECT 1"}, 0)
	wantRoute(t, err, "Service.get.SQLNotConnected")
}

func TestUsingAClosedConnectionFails(t *testing.T) {
	svc, c := openFixture(t, nil)
	oc, err := svc.get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(c.ID); err != nil {
		t.Fatal(err)
	}
	_, err = oc.pool(ctxFor(t), "")
	wantRoute(t, err, "openConn.pool.SQLNotConnected")
	if svc.IsOpen(c.ID) {
		t.Error("still open")
	}
}

func TestAQueryThatFailsIsReportedWithItsRoute(t *testing.T) {
	svc, c := openFixture(t, nil)
	_, _, err := svc.Rows(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT * FROM nothing"}, 0)
	wantRoute(t, err, "Service.Rows.SQLQueryFailed")
	_, _, err = svc.Rows(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT 1 / 0, 'x' FROM authors WHERE name = ?", Args: []any{"Ursula"}}, 0)
	if err != nil {
		t.Errorf("division by zero is NULL in SQLite: %v", err)
	}
}

func TestAListingIsCutAtItsLimit(t *testing.T) {
	svc, c := openFixture(t, nil)
	rows, truncated, err := svc.Rows(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT name FROM authors ORDER BY name"}, 1)
	if err != nil || !truncated || len(rows) != 1 {
		t.Errorf("rows = %v, truncated = %v, err = %v", rows, truncated, err)
	}
	rows, truncated, err = svc.Rows(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT name FROM authors ORDER BY name"}, 2)
	if err != nil || truncated || len(rows) != 2 {
		t.Errorf("rows = %v, truncated = %v, err = %v", rows, truncated, err)
	}
	if rows, _, err := svc.Rows(ctxFor(t), c.ID, "", dialect.Query{}, 0); rows != nil || err != nil {
		t.Errorf("an empty query: %v %v", rows, err)
	}
	more := withMore([]dbconn.Node{{ID: "a"}}, true)
	if len(more) != 2 || more[1].Kind != NodeMore || more[1].Expandable {
		t.Errorf("more = %+v", more)
	}
	if got := withMore([]dbconn.Node{{ID: "a"}}, false); len(got) != 1 {
		t.Errorf("a listing that was not cut gets a row: %+v", got)
	}
}

func TestTheTextOfADefinitionComesFromItsColumn(t *testing.T) {
	svc, c := openFixture(t, nil)
	text, err := svc.Text(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT 'a', 'b', NULL", Text: 1})
	if err != nil || text != "b" {
		t.Errorf("text = %q, %v", text, err)
	}
	text, err = svc.Text(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT 'a', 'b', NULL", Text: 2})
	if err != nil || text != "" {
		t.Errorf("a NULL: %q, %v", text, err)
	}
	text, err = svc.Text(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT 'a' WHERE 0", Text: 0})
	if err != nil || text != "" {
		t.Errorf("no row: %q, %v", text, err)
	}
}

// runawayQuery never ends by itself.
const runawayQuery = "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT count(*) FROM c"

func TestCancelStopsARunningStatementAndLeavesTheSessionUsable(t *testing.T) {
	svc, c := openFixture(t, nil)
	sess, err := svc.Session(ctxFor(t), c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sess.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, done := sess.Begin(ctxFor(t))
	defer done()
	result := make(chan error, 1)
	started := time.Now()
	go func() {
		var n int
		result <- sess.Conn().QueryRowContext(ctx, runawayQuery).Scan(&n)
	}()
	time.Sleep(200 * time.Millisecond) // let the statement start
	if err := sess.Cancel(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("the statement finished")
		}
		if time.Since(started) > 20*time.Second {
			t.Errorf("the statement took %s to stop", time.Since(started))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the statement was not cancelled")
	}
	done()
	var one int
	if err := sess.Conn().QueryRowContext(context.Background(), "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Errorf("the session after a cancel: %d, %v", one, err)
	}
	// nothing is running: cancelling does nothing
	if err := sess.Cancel(); err != nil {
		t.Errorf("Cancel with nothing running: %v", err)
	}
}

func TestEveryConnectionOfAPoolKeepsTheSessionStatements(t *testing.T) {
	svc, c := openFixture(t, func(c *dbconn.Connection) { c.ReadOnly = true })
	ctx := ctxFor(t)
	var sessions []*Session
	for range 3 {
		s, err := svc.Session(ctx, c.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, s)
	}
	for _, s := range sessions {
		var on int
		if err := s.Conn().QueryRowContext(ctx, "PRAGMA query_only").Scan(&on); err != nil || on != 1 {
			t.Errorf("query_only = %d, %v", on, err)
		}
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}
}

func TestOpeningTheSameConnectionAgainReplacesTheFirst(t *testing.T) {
	svc, c := openFixture(t, nil)
	var events []bool
	stop := svc.Subscribe(func(_ string, open bool) { events = append(events, open) })
	defer stop()
	if err := svc.Open(ctxFor(t), c, ""); err != nil {
		t.Fatal(err)
	}
	if !svc.IsOpen(c.ID) || !reflect.DeepEqual(events, []bool{true}) {
		t.Errorf("open = %v, events = %v", svc.IsOpen(c.ID), events)
	}
	if err := svc.Close(c.ID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []bool{true, false}) {
		t.Errorf("events = %v", events)
	}
}
