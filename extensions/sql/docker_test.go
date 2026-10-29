//go:build emetos_docker

package sql

// Tests against real servers: PostgreSQL, MySQL and MariaDB, each in a throwaway container of a pinned image (extensions/sql/sqltest,
// over the harness of extensions/internal/containerapi/containerapitest). They make containers named emetos-test-<random> with the
// label dev.emetos.test=1, publish their ports on 127.0.0.1 only, and remove them when the test ends. There is no SQL Server here:
// its image starts only when the license terms are accepted, which a test must not do for the user; its driver and dialect are
// tested against fakes. Run them on their own, one engine at a time if the machine is small:
//
//	scripts/heavy.sh --alone -- go test -tags emetos_docker ./extensions/sql -run TestRealServers -v -count=1
//	scripts/heavy.sh --alone -- go test -tags emetos_docker ./extensions/sql -run TestRealServers/postgres -v -count=1

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
	"github.com/rm4n0s/emetos/extensions/sql/sqltest"
)

// server is what a test knows about one engine's container and fixture.
type server struct {
	name string
	// spec makes the container; connection the connection to it; password is the random one of this run.
	spec       func(password string) sqltest.Spec
	connection func(srv sqltest.Server) dbconn.Connection
	// setup are the statements of the fixture, each run on its own.
	setup []string
	// rootToTables is the path from the connection to the Tables folder; databases are the databases of the connection.
	rootToTables []string
	databases    []string
	views        []string
	// sleep is the statement that runs for a minute.
	sleep string
	// tlsRequire says whether the server accepts a connection that requires TLS.
	tlsRequire bool
}

var servers = []server{
	{
		name: "postgres",
		spec: func(pw string) sqltest.Spec {
			return sqltest.Spec{Image: containerapitest.ImagePostgres, Port: 5432, Env: map[string]string{"POSTGRES_PASSWORD": pw, "POSTGRES_DB": "shop"}}
		},
		connection: func(srv sqltest.Server) dbconn.Connection {
			return dbconn.Connection{ID: "pg", Name: "pg", Engine: "postgres", Scope: dbconn.ScopeUser, Host: srv.Host, Port: srv.Port, User: "postgres", Database: "shop"}
		},
		setup: []string{
			"CREATE DATABASE other",
			"CREATE SCHEMA store",
			"CREATE TABLE store.customers (id serial PRIMARY KEY, name text NOT NULL, email text UNIQUE)",
			"COMMENT ON TABLE store.customers IS 'Who buys'",
			"CREATE TABLE store.orders (id bigserial PRIMARY KEY, customer_id integer NOT NULL, placed_at timestamptz NOT NULL DEFAULT now(), note text, " +
				"CONSTRAINT orders_customer_fk FOREIGN KEY (customer_id) REFERENCES store.customers (id))",
			"CREATE INDEX orders_customer_placed ON store.orders (customer_id, placed_at)",
			"CREATE VIEW store.open_orders AS SELECT * FROM store.orders WHERE note IS NULL",
			"CREATE MATERIALIZED VIEW store.sales_by_customer AS SELECT customer_id, count(*) AS n FROM store.orders GROUP BY customer_id",
			"CREATE FUNCTION store.order_count(cid integer) RETURNS bigint LANGUAGE sql AS $$ SELECT count(*) FROM store.orders WHERE customer_id = cid $$",
			"CREATE PROCEDURE store.touch(cid integer) LANGUAGE sql AS $$ UPDATE store.orders SET note = note WHERE customer_id = cid $$",
		},
		rootToTables: []string{"shop", "store", "Tables"}, databases: []string{"other", "postgres", "shop"},
		views: []string{"open_orders", "sales_by_customer"}, sleep: "SELECT pg_sleep(60)", tlsRequire: false,
	},
	{
		name: "mysql",
		spec: func(pw string) sqltest.Spec {
			return sqltest.Spec{Image: containerapitest.ImageMySQL, Port: 3306, Env: map[string]string{"MYSQL_ROOT_PASSWORD": pw}}
		},
		connection: func(srv sqltest.Server) dbconn.Connection {
			return dbconn.Connection{ID: "my", Name: "my", Engine: "mysql", Scope: dbconn.ScopeUser, Host: srv.Host, Port: srv.Port, User: "root"}
		},
		setup:        mysqlSetup,
		rootToTables: []string{"shop", "Tables"}, databases: []string{"shop"},
		views: []string{"open_orders"}, sleep: "SELECT SLEEP(60)", tlsRequire: true,
	},
	{
		name: "mariadb",
		spec: func(pw string) sqltest.Spec {
			return sqltest.Spec{Image: containerapitest.ImageMariaDB, Port: 3306, Env: map[string]string{"MARIADB_ROOT_PASSWORD": pw}}
		},
		connection: func(srv sqltest.Server) dbconn.Connection {
			return dbconn.Connection{ID: "ma", Name: "ma", Engine: "mysql", Scope: dbconn.ScopeUser, Host: srv.Host, Port: srv.Port, User: "root"}
		},
		setup:        mysqlSetup,
		rootToTables: []string{"shop", "Tables"}, databases: []string{"shop"},
		views: []string{"open_orders"}, sleep: "SELECT SLEEP(60)", tlsRequire: true, // MariaDB 11.4 has TLS on by default
	},
}

// mysqlSetup is the fixture of MySQL and MariaDB.
var mysqlSetup = []string{
	"CREATE DATABASE shop",
	"CREATE TABLE shop.customers (id int AUTO_INCREMENT PRIMARY KEY, name varchar(100) NOT NULL, email varchar(200) UNIQUE) COMMENT = 'Who buys'",
	"CREATE TABLE shop.orders (id bigint AUTO_INCREMENT PRIMARY KEY, customer_id int NOT NULL, placed_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP, note text, " +
		"KEY orders_customer_placed (customer_id, placed_at), CONSTRAINT orders_customer_fk FOREIGN KEY (customer_id) REFERENCES shop.customers (id))",
	"CREATE VIEW shop.open_orders AS SELECT * FROM shop.orders WHERE note IS NULL",
	"CREATE FUNCTION shop.order_count(cid int) RETURNS bigint READS SQL DATA RETURN (SELECT count(*) FROM shop.orders WHERE customer_id = cid)",
	"CREATE PROCEDURE shop.touch(cid int) UPDATE shop.orders SET note = note WHERE customer_id = cid",
}

// openWhenReady opens the connection, trying again while the server starts.
func openWhenReady(t *testing.T, svc *Service, c dbconn.Connection, password string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	var err error
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err = svc.Open(ctx, c, password)
		cancel()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server did not come up: %v", err)
		}
		time.Sleep(time.Second)
	}
}

// runSetup runs the fixture on the open connection.
func (s server) runSetup(t *testing.T, svc *Service, c dbconn.Connection) {
	t.Helper()
	ctx := ctxFor(t)
	db, err := svc.DB(ctx, c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range s.setup {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func TestRealServers(t *testing.T) {
	for _, s := range servers {
		t.Run(s.name, func(t *testing.T) {
			password := sqltest.RandomPassword(t)
			srv := sqltest.Docker().Start(t, s.spec(password))
			c := s.connection(srv)
			svc := NewService()
			openWhenReady(t, svc, c, password)
			t.Cleanup(func() {
				if err := svc.Close(c.ID); err != nil {
					t.Error(err)
				}
			})
			s.runSetup(t, svc, c)

			t.Run("the explorer", func(t *testing.T) { s.explorer(t, svc, c) })
			t.Run("statements", func(t *testing.T) { s.statements(t, svc, c) })
			t.Run("read-only", func(t *testing.T) { s.readOnly(t, c, password) })
			t.Run("cancel", func(t *testing.T) { s.cancel(t, svc, c) })
			t.Run("TLS", func(t *testing.T) { s.tls(t, c, password) })
			t.Run("the Databases view", func(t *testing.T) { s.view(t, c, password) })
		})
	}
}

// explorer browses the fixture through the nodes of the explorer.
func (s server) explorer(t *testing.T, svc *Service, c dbconn.Connection) {
	if got := labels(list(t, svc, c)); !slices.Equal(got, s.databases) {
		t.Errorf("databases = %v, want %v", got, s.databases)
	}
	parent := s.rootToTables[:len(s.rootToTables)-1]
	folders := labels(list(t, svc, c, parent...))
	if !slices.Equal(folders, []string{"Tables", "Views", "Functions", "Procedures"}) {
		t.Errorf("folders = %v", folders)
	}
	if got := labels(list(t, svc, c, s.rootToTables...)); len(got) != 2 || !strings.HasPrefix(got[0], "customers") || got[1] != "orders" {
		t.Errorf("tables = %v", got)
	}
	tables := list(t, svc, c, s.rootToTables...)
	if tables[0].Tooltip != "Who buys" {
		t.Errorf("the comment of customers = %q", tables[0].Tooltip)
	}
	if got := labels(list(t, svc, c, append(slices.Clone(parent), "Views")...)); !viewsMatch(got, s.views) {
		t.Errorf("views = %v, want %v", got, s.views)
	}
	if got := labels(list(t, svc, c, append(slices.Clone(parent), "Functions")...)); len(got) != 1 || !strings.HasPrefix(got[0], "order_count: (") {
		t.Errorf("functions = %v", got)
	}
	if got := labels(list(t, svc, c, append(slices.Clone(parent), "Procedures")...)); len(got) != 1 || !strings.HasPrefix(got[0], "touch: (") {
		t.Errorf("procedures = %v", got)
	}
	orders := append(slices.Clone(s.rootToTables), "orders")
	if got := labels(list(t, svc, c, orders...)); !slices.Equal(got, []string{"Columns", "Indexes", "Foreign Keys"}) {
		t.Errorf("what is in a table = %v", got)
	}
	cols := list(t, svc, c, append(slices.Clone(orders), "Columns")...)
	var names, icons []string
	for _, n := range cols {
		names, icons = append(names, n.Label), append(icons, n.Icon)
	}
	if !slices.Equal(names, []string{"id", "customer_id", "placed_at", "note"}) || !slices.Equal(icons, []string{"key", "references", "symbol-field", "symbol-field"}) {
		t.Errorf("columns = %v icons %v", names, icons)
	}
	if !strings.Contains(cols[0].Description, "NOT NULL") || strings.Contains(cols[3].Description, "NOT NULL") {
		t.Errorf("nullability: %q and %q", cols[0].Description, cols[3].Description)
	}
	ixs := labels(list(t, svc, c, append(slices.Clone(orders), "Indexes")...))
	primaries, composite := 0, false
	for _, ix := range ixs {
		if strings.Contains(ix, "primary key") {
			primaries++
		}
		if strings.HasPrefix(ix, "orders_customer_placed: (customer_id, placed_at)") {
			composite = true
		}
	}
	if primaries != 1 || !composite {
		t.Errorf("indexes = %v", ixs)
	}
	fks := labels(list(t, svc, c, append(slices.Clone(orders), "Foreign Keys")...))
	if len(fks) != 1 || !strings.HasPrefix(fks[0], "orders_customer_fk: (customer_id) -> ") || !strings.Contains(fks[0], "customers (id)") {
		t.Errorf("foreign keys = %v", fks)
	}
	if c.Engine == "postgres" {
		// another database is browsed through a connection of its own
		if got := labels(list(t, svc, c, "other")); !slices.Contains(got, "public") {
			t.Errorf("the schemas of the other database = %v", got)
		}
		if oc, err := svc.get(c.ID); err != nil || len(oc.pools) != 2 {
			t.Errorf("pools = %v, %v", oc, err)
		}
	}
}

// viewsMatch compares the labels of the views folder with the expected names, allowing the "materialized" description.
func viewsMatch(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if name, _, _ := strings.Cut(got[i], ":"); name != want[i] {
			return false
		}
	}
	return true
}

// statements checks the statements generated from the catalog and the catalog's own definitions.
func (s server) statements(t *testing.T, svc *Service, c dbconn.Connection) {
	d, err := svc.Dialect(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	parent := s.rootToTables[:len(s.rootToTables)-1]
	orders := walk(t, svc, c, append(slices.Clone(s.rootToTables), "orders")...)
	data := orders.Data.(nodeData)
	table, err := svc.LoadTable(ctxFor(t), c.ID, dialect.KindTable, data.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Columns) != 4 || len(table.Indexes) < 2 || len(table.ForeignKeys) != 1 || !table.Columns[0].PrimaryKey || !table.Columns[1].ForeignKey {
		t.Fatalf("table = %+v", table)
	}
	for name, tt := range map[string]struct{ got, column string }{
		"select": {dialect.GenerateSelect(d, table), "customer_id"}, "insert": {dialect.GenerateInsert(d, table), "customer_id"},
		"update": {dialect.GenerateUpdate(d, table), "customer_id"}, "delete": {dialect.GenerateDelete(d, table), "id"},
	} {
		if !strings.Contains(tt.got, d.Qualified(data.Ref)) || !strings.Contains(tt.got, d.Quote(tt.column)) {
			t.Errorf("%s = %s", name, tt.got)
		}
	}
	// what the statements say is valid for the server: a SELECT of the first rows runs
	if _, _, err := svc.Rows(ctxFor(t), c.ID, data.Ref.Database, dialect.Query{SQL: d.SelectTop(data.Ref, 10)}, 0); err != nil {
		t.Errorf("%s: %v", d.SelectTop(data.Ref, 10), err)
	}
	for _, kind := range []struct {
		folder string
		kind   dialect.Kind
		name   string
	}{{"Tables", dialect.KindTable, "orders"}, {"Views", dialect.KindView, "open_orders"}, {"Functions", dialect.KindFunction, "order_count"}, {"Procedures", dialect.KindProcedure, "touch"}} {
		path := append(slices.Clone(parent), kind.folder)
		var node dbconn.Node
		for _, n := range list(t, svc, c, path...) {
			if n.Label == kind.name {
				node = n
			}
		}
		ref := node.Data.(nodeData).Ref
		create, err := svc.Create(ctxFor(t), c.ID, kind.kind, ref)
		if err != nil {
			t.Errorf("Create(%s): %v", kind.name, err)
			continue
		}
		if !strings.Contains(strings.ToLower(create), "create") || !strings.Contains(create, kind.name) || !strings.HasSuffix(create, ";\n") {
			t.Errorf("Create(%s) = %q", kind.name, create)
		}
		if drop := dialect.GenerateDrop(d, kind.kind, ref); !strings.HasPrefix(drop, "DROP ") || !strings.Contains(drop, kind.name) {
			t.Errorf("drop %s = %q", kind.name, drop)
		}
	}
}

// readOnly checks that a connection that is read-only refuses to write.
func (s server) readOnly(t *testing.T, c dbconn.Connection, password string) {
	c.ID, c.ReadOnly = c.ID+"-ro", true
	svc := NewService()
	openWhenReady(t, svc, c, password)
	defer func() {
		if err := svc.Close(c.ID); err != nil {
			t.Error(err)
		}
	}()
	db, err := svc.DB(ctxFor(t), c.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	stmt := "CREATE TABLE emetos_ro_check (a int)"
	if c.Engine == "mysql" {
		stmt = "CREATE TABLE shop.emetos_ro_check (a int)"
	}
	if _, err := db.ExecContext(ctxFor(t), stmt); err == nil {
		t.Error("a read-only connection created a table")
	}
	// every connection of the pool is read-only, not only the first one
	var sessions []*Session
	for range 3 {
		sess, err := svc.Session(ctxFor(t), c.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, sess)
		if _, err := sess.Conn().ExecContext(ctxFor(t), stmt); err == nil {
			t.Error("a connection of the pool is not read-only")
		}
	}
	for _, sess := range sessions {
		if err := sess.Close(); err != nil {
			t.Error(err)
		}
	}
	if rows, _, err := svc.Rows(ctxFor(t), c.ID, "", dialect.Query{SQL: "SELECT 1"}, 0); err != nil || len(rows) != 1 {
		t.Errorf("a read-only connection cannot read: %v %v", rows, err)
	}
}

// cancel stops a statement that would run for a minute, and uses the session afterwards.
func (s server) cancel(t *testing.T, svc *Service, c dbconn.Connection) {
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
		rows, err := sess.Conn().QueryContext(ctx, s.sleep)
		if err == nil {
			for rows.Next() {
			}
			err = rows.Err()
			if cerr := rows.Close(); err == nil {
				err = cerr
			}
		}
		result <- err
	}()
	time.Sleep(2 * time.Second) // the statement is running on the server
	if err := sess.Cancel(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil && time.Since(started) > 30*time.Second {
			t.Error("the statement ran to its end")
		}
		if elapsed := time.Since(started); elapsed > 20*time.Second {
			t.Errorf("the statement took %s to stop", elapsed)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("the statement was not cancelled")
	}
	done()
	var one int
	if err := sess.Conn().QueryRowContext(ctxFor(t), "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Errorf("the session after a cancel: %d, %v", one, err)
	}
}

// tls connects with each TLS mode that does not need a certificate to check.
func (s server) tls(t *testing.T, c dbconn.Connection, password string) {
	for _, tt := range []struct {
		mode    dbconn.TLSMode
		wantErr bool
	}{
		{dbconn.TLSDisable, false}, {dbconn.TLSPrefer, false}, {dbconn.TLSRequire, !s.tlsRequire},
	} {
		t.Run(string(tt.mode), func(t *testing.T) {
			c := c
			c.ID, c.TLS = c.ID+"-tls-"+string(tt.mode), tt.mode
			c.Options = map[string]string{OptionConnectTimeout: "10"}
			svc := NewService()
			err := svc.Open(ctxFor(t), c, password)
			if (err != nil) != tt.wantErr {
				t.Errorf("Open with %s: %v, want an error: %v", tt.mode, err, tt.wantErr)
			}
			if err == nil {
				if err := svc.Close(c.ID); err != nil {
					t.Error(err)
				}
			}
			if err != nil && strings.Contains(err.Error(), password) {
				t.Errorf("the password is in the error: %v", err)
			}
		})
	}
}

// view connects through the Databases view, with the password in an environment variable, and browses to the tables.
func (s server) view(t *testing.T, c dbconn.Connection, password string) {
	r := newRig(t)
	r.env["EMETOS_TEST_SQL_PW"] = password
	c.ID, c.PasswordEnv = "", "${env:EMETOS_TEST_SQL_PW}"
	saved := r.save(c)
	r.connect(saved)
	path := append([]string{c.Name}, s.rootToTables...)
	r.expand(path...)
	rows := r.rows()
	if !slices.Contains(rows, fmt.Sprintf("%d:orders", len(path))) {
		t.Errorf("rows = %v", rows)
	}
	if len(r.dbAPI.Inputs()) != 0 {
		t.Error("the view asked for a password although the variable has it")
	}
}
