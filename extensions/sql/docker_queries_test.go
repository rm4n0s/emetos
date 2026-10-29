//go:build emetos_docker

package sql

// The query editors against real servers (step 19.3): PostgreSQL, MySQL and MariaDB, each in a throwaway container of a pinned image
// (extensions/sql/sqltest on the harness of step 18.1, the rules of docker_test.go). SQL Server is not here and is never started in a
// test: its image needs ACCEPT_EULA=Y, which would accept Microsoft's licence terms on the user's behalf. Its splitting (GO), its classification and its
// execution are tested against fakes (run_fake_test.go, dialect/split_test.go).
//
//	scripts/heavy.sh --alone -- go test -tags emetos_docker ./extensions/sql -run TestRealServerQueries -v -count=1
//	scripts/heavy.sh --alone -- go test -tags emetos_docker ./extensions/sql -run TestRealServerQueries/mysql -v -count=1

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
	"github.com/rm4n0s/emetos/extensions/sql/sqltest"
)

// typesTable is the statement that makes a table with one column of each kind the grid shows, and what to read back.
func (s server) typesTable() (create, insert string, wantKinds []resultgrid.Kind, wantText []string) {
	switch s.name {
	case "postgres":
		return "CREATE TABLE store.kinds (i bigint, f double precision, n numeric(10,2), t text, b boolean, d timestamptz, j jsonb, raw bytea, u uuid, nul text)",
			"INSERT INTO store.kinds VALUES (7, 2.5, 12.50, 'héllo; world', true, '2026-10-29 06:07:08+00', '{\"a\": [1, 2]}', '\\xcafe', '0a1b2c3d-0000-4000-8000-0123456789ab', NULL)",
			[]resultgrid.Kind{resultgrid.KindInt, resultgrid.KindFloat, resultgrid.KindText, resultgrid.KindText, resultgrid.KindBool, resultgrid.KindTime, resultgrid.KindJSON, resultgrid.KindBytes, resultgrid.KindText, resultgrid.KindNull},
			[]string{"7", "2.5", "12.50", "héllo; world", "true", "2026-10-29T06:07:08Z", `{"a": [1, 2]}`, "0xcafe", "0a1b2c3d-0000-4000-8000-0123456789ab", "NULL"}
	}
	kinds := []resultgrid.Kind{resultgrid.KindInt, resultgrid.KindFloat, resultgrid.KindText, resultgrid.KindText, resultgrid.KindInt, resultgrid.KindTime, resultgrid.KindJSON, resultgrid.KindBytes, resultgrid.KindText, resultgrid.KindNull}
	if s.name == "mariadb" {
		kinds[6] = resultgrid.KindText // MariaDB's JSON is an alias of LONGTEXT: the server does not say it is JSON
	}
	return "CREATE TABLE shop.kinds (i bigint, f double, n decimal(10,2), t text, b boolean, d datetime, j json, raw blob, u char(36), nul text)",
		"INSERT INTO shop.kinds VALUES (7, 2.5, 12.50, 'héllo; world', true, '2026-10-29 06:07:08', '{\"a\": [1, 2]}', x'cafe', '0a1b2c3d-0000-4000-8000-0123456789ab', NULL)",
		kinds,
		[]string{"7", "2.5", "12.50", "héllo; world", "1", "2026-10-29T06:07:08Z", `{"a": [1, 2]}`, "0xcafe", "0a1b2c3d-0000-4000-8000-0123456789ab", "NULL"}
}

func TestRealServerQueries(t *testing.T) {
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
			database := "shop"

			newRunner := func(t *testing.T) *Runner {
				t.Helper()
				r, err := svc.NewRunner(c.ID, database)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := r.Dispose(); err != nil {
						t.Error(err)
					}
				})
				return r
			}
			script := func(t *testing.T, r *Runner, text string, opts RunOptions) []*StatementResult {
				t.Helper()
				res, err := r.Run(ctxFor(t), dialect.Split(r.d, text), opts)
				if err != nil {
					t.Fatal(err)
				}
				return res
			}

			t.Run("selects, writes and the values of every kind", func(t *testing.T) {
				r := newRunner(t)
				create, insert, wantKinds, wantText := s.typesTable()
				res := script(t, r, create+";\n"+insert+";\nSELECT * FROM "+s.rel("kinds")+";\nUPDATE "+s.rel("kinds")+" SET i = 8 WHERE i = 7;", RunOptions{})
				if len(res) != 4 {
					t.Fatalf("results = %d", len(res))
				}
				for _, x := range res {
					if x.Err != nil {
						t.Fatalf("%q: %s", x.Statement.Text, x.Err.Text())
					}
				}
				if !res[1].HasAffected || res[1].Affected != 1 || !res[3].HasAffected || res[3].Affected != 1 {
					t.Errorf("rows affected: %+v %+v", res[1], res[3])
				}
				set := res[2].Sets[0]
				dst := make([]resultgrid.Value, len(set.Columns))
				set.Table.Row(0, dst)
				for i, v := range dst {
					if v.Kind != wantKinds[i] || v.Full("NULL") != wantText[i] {
						t.Errorf("column %s: kind %d text %q, want kind %d text %q", set.Columns[i].Name, v.Kind, v.Full("NULL"), wantKinds[i], wantText[i])
					}
				}
			})

			t.Run("an error says where", func(t *testing.T) {
				r := newRunner(t)
				res := script(t, r, "SELECT 1;\nSELECT id FORM "+s.rel("orders")+"", RunOptions{})
				if len(res) != 2 || res[1].Err == nil {
					t.Fatalf("results = %+v", res)
				}
				e := res[1].Err
				if e.Offset < 0 || e.Code == "" || e.Message == "" {
					t.Errorf("error = %+v", e)
				}
				if got := res[1].Statement.Text[e.Offset:]; !strings.HasPrefix(got, "FORM") && !strings.HasPrefix(got, s.rel("orders")) {
					t.Errorf("the error is at %q", got)
				}
				// the session is usable after an error
				if after := script(t, r, "SELECT 2", RunOptions{}); after[0].Err != nil {
					t.Errorf("after the error: %s", after[0].Err.Text())
				}
			})

			t.Run("the database of the session", func(t *testing.T) {
				r := newRunner(t)
				q := "SELECT current_database()"
				if s.name != "postgres" {
					q = "SELECT DATABASE()"
				}
				res := script(t, r, q, RunOptions{})
				if res[0].Err != nil || setCell(res[0].Sets[0], 0, 0) != "shop" {
					t.Errorf("database = %+v", res[0])
				}
			})

			t.Run("the row limit and Fetch More", func(t *testing.T) {
				r := newRunner(t)
				q := "SELECT g FROM generate_series(1, 2500) AS g"
				if s.name != "postgres" {
					// MySQL stops a recursive query at a depth of 1000: the numbers come from a product of digits
					digits := "(SELECT 0 AS n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9)"
					q = "SELECT g FROM (SELECT a.n + 10 * b.n + 100 * c.n + 1000 * d.n + 1 AS g FROM " + digits + " a CROSS JOIN " + digits + " b CROSS JOIN " + digits + " c CROSS JOIN " + digits + " d) t WHERE g <= 2500 ORDER BY g"
				}
				res := script(t, r, q, RunOptions{MaxRows: 1000})
				if res[0].Err != nil || len(res[0].Sets) != 1 {
					t.Fatalf("first read: %+v", res[0])
				}
				set := res[0].Sets[0]
				if set.Table.Len() != 1000 || !set.More() {
					t.Fatalf("first read: rows %d more %v", set.Table.Len(), set.More())
				}
				for _, want := range []int{2000, 2500} {
					if err := r.FetchMore(ctxFor(t), set, RunOptions{MaxRows: 1000}); err != nil {
						t.Fatal(err)
					}
					if set.Table.Len() != want {
						t.Fatalf("rows = %d, want %d", set.Table.Len(), want)
					}
				}
				if set.More() || setCell(set, 2499, 0) != "2500" {
					t.Errorf("more %v, last row %s", set.More(), setCell(set, 2499, 0))
				}
			})

			t.Run("transactions", func(t *testing.T) {
				r := newRunner(t)
				ctx := ctxFor(t)
				count := func() string {
					rows, _, err := svc.Rows(ctx, c.ID, database, dialect.Query{SQL: "SELECT count(*) FROM " + s.rel("customers")}, 0)
					if err != nil {
						t.Fatal(err)
					}
					return rows[0][0].String
				}
				before := count()
				if err := r.Begin(ctx); err != nil {
					t.Fatal(err)
				}
				if res := script(t, r, "INSERT INTO "+s.rel("customers")+" (name) VALUES ('rolled back')", RunOptions{}); res[0].Err != nil {
					t.Fatal(res[0].Err.Text())
				}
				if got := count(); got != before {
					t.Errorf("another connection sees %s rows of an open transaction, want %s", got, before)
				}
				if err := r.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
				if got := count(); got != before {
					t.Errorf("after the rollback: %s, want %s", got, before)
				}
				if err := r.Begin(ctx); err != nil {
					t.Fatal(err)
				}
				script(t, r, "INSERT INTO "+s.rel("customers")+" (name) VALUES ('kept')", RunOptions{})
				if err := r.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				if got := count(); got == before {
					t.Errorf("after the commit: %s, want one more than %s", got, before)
				}
				// a transaction the user types is followed too, and closing the runner rolls back what is open
				script(t, r, s.beginText()+";\nINSERT INTO "+s.rel("customers")+" (name) VALUES ('typed')", RunOptions{})
				if !r.InTransaction() {
					t.Error("the transaction the user typed is not followed")
				}
				rolled, err := r.Close()
				if err != nil || !rolled {
					t.Errorf("Close = %v, %v", rolled, err)
				}
				rows, _, err := svc.Rows(ctx, c.ID, database, dialect.Query{SQL: "SELECT count(*) FROM " + s.rel("customers") + " WHERE name = 'typed'"}, 0)
				if err != nil || rows[0][0].String != "0" {
					t.Errorf("typed rows = %v, %v", rows, err)
				}
			})

			t.Run("cancel", func(t *testing.T) {
				r := newRunner(t)
				if err := r.Begin(ctxFor(t)); err != nil {
					t.Fatal(err)
				}
				done := make(chan []*StatementResult, 1)
				started := time.Now()
				go func() {
					res, err := r.Run(context.Background(), dialect.Split(r.d, s.sleep+"; SELECT 'never'"), RunOptions{})
					if err != nil {
						t.Error(err)
					}
					done <- res
				}()
				time.Sleep(2 * time.Second) // the statement is running on the server
				if err := r.Cancel(); err != nil {
					t.Fatal(err)
				}
				select {
				case res := <-done:
					if len(res) != 1 || !res[0].Cancelled || res[0].Err == nil {
						t.Errorf("results = %+v", res)
					}
					if took := time.Since(started); took > 20*time.Second {
						t.Errorf("the statement took %s to stop", took)
					}
				case <-time.After(40 * time.Second):
					t.Fatal("the statement was not cancelled")
				}
				// the session, and the transaction it holds, are as they were (PostgreSQL aborts the transaction: only a rollback is left)
				if !r.InTransaction() {
					t.Error("the cancel ended the transaction")
				}
				if err := r.Rollback(ctxFor(t)); err != nil {
					t.Fatal(err)
				}
				if res := script(t, r, "SELECT 5", RunOptions{}); res[0].Err != nil || setCell(res[0].Sets[0], 0, 0) != "5" {
					t.Errorf("after the cancel: %+v", res[0])
				}
			})

			t.Run("a table that changes between two reads", func(t *testing.T) {
				r := newRunner(t)
				res := script(t, r, "CREATE TABLE "+s.rel("shifting")+" (a int);\nINSERT INTO "+s.rel("shifting")+" VALUES (1);\nSELECT * FROM "+s.rel("shifting")+";\nALTER TABLE "+s.rel("shifting")+" ADD COLUMN b int;\nSELECT * FROM "+s.rel("shifting"), RunOptions{})
				for _, x := range res {
					if x.Err != nil {
						t.Fatalf("%q: %s", x.Statement.Text, x.Err.Text())
					}
				}
				if len(res) != 5 || len(res[4].Sets[0].Columns) != 2 {
					t.Errorf("results = %d", len(res))
				}
			})

			t.Run("a read-only connection", func(t *testing.T) {
				ro := c
				ro.ID, ro.ReadOnly = "ro", true
				if err := svc.Open(ctxFor(t), ro, password); err != nil {
					t.Fatal(err)
				}
				rr, err := svc.NewRunner(ro.ID, database)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := rr.Dispose(); err != nil {
						t.Error(err)
					}
				}()
				res := script(t, rr, "SELECT 1; INSERT INTO "+s.rel("customers")+" (name) VALUES ('no')", RunOptions{})
				if len(res) != 2 || res[0].Err != nil || res[1].Err == nil {
					t.Fatalf("results = %+v", res)
				}
				if !strings.Contains(strings.ToLower(res[1].Err.Message), "read-only") && !strings.Contains(strings.ToLower(res[1].Err.Message), "read only") {
					t.Errorf("the server said %q", res[1].Err.Message)
				}
			})

			t.Run("several result sets", func(t *testing.T) {
				if s.name == "postgres" {
					t.Skip("a PostgreSQL statement has one result set")
				}
				r := newRunner(t)
				res := script(t, r, "DELIMITER //\nCREATE PROCEDURE shop.two() BEGIN SELECT 1 AS a; SELECT 'x' AS b, 2 AS c; END//\nDELIMITER ;\nCALL shop.two()", RunOptions{})
				last := res[len(res)-1]
				if last.Err != nil || len(last.Sets) != 2 || len(last.Sets[1].Columns) != 2 {
					t.Fatalf("last = %+v (%v)", last, last.Err)
				}
			})

			t.Run("in the editor, through the commands", func(t *testing.T) {
				e := newRig(t)
				e.env["EMETOS_TEST_SQL_PW"] = password
				conn := c
				conn.ID, conn.PasswordEnv = "", "${env:EMETOS_TEST_SQL_PW}"
				saved := e.save(conn)
				e.connect(saved)
				uri, err := e.api.Documents().OpenUntitled("sql", "SELECT name FROM "+s.rel("customers")+" ORDER BY id;\nSELEC 1")
				if err != nil {
					t.Fatal(err)
				}
				er := &editorRig{rig: e, conn: saved, uri: uri}
				e.ext.q.bindEditor(uri, binding{Connection: saved.ID, Database: database})
				er.activate(nil)
				rec := er.run(RunAllQueries)
				if len(rec.steps) != 2 || !rec.failed() || len(rec.sets) != 1 {
					t.Fatalf("steps %d, failed %v, sets %d", len(rec.steps), rec.failed(), len(rec.sets))
				}
				if shown := e.api.ShownDocuments(); len(shown) != 1 || shown[0].Line != 2 {
					t.Errorf("shown = %+v", shown)
				}
			})
		})
	}
}

// rel is the name of a table of the fixture as a statement writes it: PostgreSQL's tables are in the schema store of the database shop,
// MySQL's in the database shop.
func (s server) rel(name string) string {
	if s.name == "postgres" {
		return "store." + name
	}
	return "shop." + name
}

// beginText is the statement that starts a transaction, as a person types it for the engine.
func (s server) beginText() string {
	if s.name == "postgres" {
		return "BEGIN"
	}
	return "START TRANSACTION"
}
