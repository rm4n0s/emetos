package dialect

import (
	"database/sql"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestTheDialectsAreListedInOrderAndFoundByID(t *testing.T) {
	var ids []string
	for _, d := range All() {
		ids = append(ids, d.ID())
		got, ok := ByID(d.ID())
		if !ok || got.ID() != d.ID() {
			t.Errorf("ByID(%q) = %v, %v", d.ID(), got, ok)
		}
		if d.Name() == "" || d.DriverName() == "" {
			t.Errorf("%s has no name or driver name", d.ID())
		}
	}
	if want := []string{"postgres", "mysql", "sqlite", "sqlserver"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("dialects = %v, want %v", ids, want)
	}
	if _, ok := ByID("oracle"); ok {
		t.Error("ByID found a dialect that does not exist")
	}
}

func TestQuotingKeepsAnIdentifierOneToken(t *testing.T) {
	tests := []struct {
		dialect Dialect
		name    string
		want    string
	}{
		{Postgres{}, "users", `"users"`},
		{Postgres{}, `we"ird`, `"we""ird"`},
		{Postgres{}, "a b.c", `"a b.c"`},
		{Postgres{}, "nul\x00l", `"null"`},
		{MySQL{}, "users", "`users`"},
		{MySQL{}, "we`ird", "`we``ird`"},
		{SQLite{}, `a"; DROP TABLE x; --`, `"a""; DROP TABLE x; --"`},
		{SQLServer{}, "users", "[users]"},
		{SQLServer{}, "we]ird", "[we]]ird]"},
		{SQLServer{}, "[x]; DROP TABLE y; --", "[[x]]; DROP TABLE y; --]"},
	}
	for _, tt := range tests {
		t.Run(tt.dialect.ID()+" "+tt.name, func(t *testing.T) {
			if got := tt.dialect.Quote(tt.name); got != tt.want {
				t.Errorf("Quote(%q) = %s, want %s", tt.name, got, tt.want)
			}
		})
	}
}

func TestStringLiteralsEscapeTheQuote(t *testing.T) {
	tests := []struct {
		dialect Dialect
		in      string
		want    string
	}{
		{Postgres{}, "it's", "'it''s'"},
		{Postgres{}, `a\b`, `'a\b'`},
		{SQLite{}, "it's", "'it''s'"},
		{MySQL{}, "it's", "'it''s'"},
		{MySQL{}, `a\b`, "CONVERT(0x615c62 USING utf8mb4)"},
		{SQLServer{}, "it's", "N'it''s'"},
	}
	for _, tt := range tests {
		t.Run(tt.dialect.ID()+" "+tt.in, func(t *testing.T) {
			if got := tt.dialect.QuoteString(tt.in); got != tt.want {
				t.Errorf("QuoteString(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestQualifiedAndSelectTop(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		ref     Ref
		want    string
		top     string
	}{
		{"postgres", Postgres{}, Ref{Database: "app", Schema: "public", Name: "users"}, `"public"."users"`, `SELECT * FROM "public"."users" LIMIT 1000;`},
		{"mysql", MySQL{}, Ref{Database: "app", Name: "users"}, "`app`.`users`", "SELECT * FROM `app`.`users` LIMIT 1000;"},
		{"sqlite main", SQLite{}, Ref{Schema: "main", Name: "users"}, `"users"`, `SELECT * FROM "users" LIMIT 1000;`},
		{"sqlite attached", SQLite{}, Ref{Schema: "other", Name: "users"}, `"other"."users"`, `SELECT * FROM "other"."users" LIMIT 1000;`},
		{"sqlserver", SQLServer{}, Ref{Database: "app", Schema: "dbo", Name: "users"}, "[app].[dbo].[users]", "SELECT TOP (1000) * FROM [app].[dbo].[users];"},
		{"sqlserver without a database", SQLServer{}, Ref{Schema: "dbo", Name: "users"}, "[dbo].[users]", "SELECT TOP (1000) * FROM [dbo].[users];"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.dialect.Qualified(tt.ref); got != tt.want {
				t.Errorf("Qualified = %s, want %s", got, tt.want)
			}
			if got := tt.dialect.SelectTop(tt.ref, 1000); got != tt.top {
				t.Errorf("SelectTop = %s, want %s", got, tt.top)
			}
		})
	}
}

// placeholders counts the placeholders of a statement in the style of the dialect's driver.
func placeholders(d Dialect, sql string) int {
	switch d.ID() {
	case "postgres":
		return len(regexp.MustCompile(`\$\d+`).FindAllString(sql, -1))
	case "sqlserver":
		return len(regexp.MustCompile(`@p\d+`).FindAllString(sql, -1))
	}
	return strings.Count(sql, "?")
}

func TestCatalogQueriesTakeTheirNamesAsArguments(t *testing.T) {
	ref := Ref{Database: "app", Schema: "public", Name: "users", ID: "42"}
	for _, d := range All() {
		queries := map[string]Query{
			"Tables": d.Tables(ref, false), "Views": d.Views(ref), "Routines": d.Routines(ref), "Columns": d.Columns(ref),
			"Indexes": d.Indexes(ref), "ForeignKeys": d.ForeignKeys(ref), "Databases": d.Databases(false), "Schemas": d.Schemas("app", false),
			"Definition view": d.Definition(KindView, ref), "Definition function": d.Definition(KindFunction, ref),
		}
		for name, q := range queries {
			t.Run(d.ID()+" "+name, func(t *testing.T) {
				if q.Empty() {
					return
				}
				if got := placeholders(d, q.SQL); got != len(q.Args) {
					t.Errorf("%d placeholders, %d arguments:\n%s", got, len(q.Args), q.SQL)
				}
				for _, a := range q.Args {
					if s, ok := a.(string); ok && strings.Contains(q.SQL, s) && s != "" && len(s) > 2 {
						t.Errorf("the argument %q is also written in the statement:\n%s", s, q.SQL)
					}
				}
			})
		}
	}
}

func TestTheLevelsDecideWhichCatalogQueriesExist(t *testing.T) {
	tests := []struct {
		dialect Dialect
		levels  Levels
	}{
		{Postgres{}, Levels{Databases: true, Schemas: true, Routines: true, SeparateConnections: true}},
		{MySQL{}, Levels{Databases: true, Routines: true}},
		{SQLite{}, Levels{Schemas: true}},
		{SQLServer{}, Levels{Databases: true, Schemas: true, Routines: true}},
	}
	for _, tt := range tests {
		t.Run(tt.dialect.ID(), func(t *testing.T) {
			l := tt.dialect.Levels()
			if l != tt.levels {
				t.Errorf("levels = %+v, want %+v", l, tt.levels)
			}
			ref := Ref{Database: "d", Schema: "s", Name: "n"}
			if got := !tt.dialect.Databases(false).Empty(); got != l.Databases {
				t.Errorf("Databases query exists = %v, levels say %v", got, l.Databases)
			}
			if got := !tt.dialect.Schemas("d", false).Empty(); got != l.Schemas {
				t.Errorf("Schemas query exists = %v, levels say %v", got, l.Schemas)
			}
			if got := !tt.dialect.Routines(ref).Empty(); got != l.Routines {
				t.Errorf("Routines query exists = %v, levels say %v", got, l.Routines)
			}
			for name, q := range map[string]Query{"Tables": tt.dialect.Tables(ref, false), "Views": tt.dialect.Views(ref), "Columns": tt.dialect.Columns(ref),
				"Indexes": tt.dialect.Indexes(ref), "ForeignKeys": tt.dialect.ForeignKeys(ref)} {
				if q.Empty() {
					t.Errorf("%s has no query", name)
				}
			}
		})
	}
}

func TestTheSystemObjectsAreLeftOutUnlessAsked(t *testing.T) {
	tests := []struct {
		name   string
		query  func(system bool) Query
		marker string
	}{
		{"postgres databases", Postgres{}.Databases, "NOT datistemplate"},
		{"postgres schemas", func(s bool) Query { return Postgres{}.Schemas("", s) }, "information_schema"},
		{"mysql databases", MySQL{}.Databases, "performance_schema"},
		{"sqlite tables", func(s bool) Query { return SQLite{}.Tables(Ref{Schema: "main"}, s) }, `sqlite\_%`},
		{"sqlserver databases", SQLServer{}.Databases, "database_id > 4"},
		{"sqlserver schemas", func(s bool) Query { return SQLServer{}.Schemas("app", s) }, "INFORMATION_SCHEMA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.query(false).SQL, tt.marker) {
				t.Errorf("the list without system objects does not filter %q:\n%s", tt.marker, tt.query(false).SQL)
			}
			if strings.Contains(tt.query(true).SQL, tt.marker) {
				t.Errorf("the list with system objects still filters %q:\n%s", tt.marker, tt.query(true).SQL)
			}
		})
	}
}

func TestSQLServerReadsTheCatalogOfTheDatabaseItIsAskedFor(t *testing.T) {
	d := SQLServer{}
	q := d.Tables(Ref{Database: "we]ird", Schema: "dbo"}, false)
	if !strings.Contains(q.SQL, "[we]]ird].sys.tables") {
		t.Errorf("the database name is not quoted in the statement:\n%s", q.SQL)
	}
	if q := d.Schemas("shop", false); !strings.Contains(q.SQL, "[shop].sys.schemas") {
		t.Errorf("schemas = %s", q.SQL)
	}
	if q := d.Tables(Ref{Schema: "dbo"}, false); !strings.Contains(q.SQL, "FROM sys.tables") {
		t.Errorf("no database: %s", q.SQL)
	}
}

func TestSQLiteReadsTheSchemaItIsAskedFor(t *testing.T) {
	q := SQLite{}.Tables(Ref{Schema: `at"tached`}, false)
	if !strings.Contains(q.SQL, `"at""tached".sqlite_master`) {
		t.Errorf("the schema is not quoted in the statement:\n%s", q.SQL)
	}
	if q := (SQLite{}).Columns(Ref{Name: "t"}); !reflect.DeepEqual(q.Args, []any{"t", "main"}) {
		t.Errorf("no schema means main: %v", q.Args)
	}
}

func TestDefinitionsAreAskedForOrWrittenFromTheColumns(t *testing.T) {
	ref := Ref{Database: "app", Schema: "public", Name: "users", ID: "7"}
	tests := []struct {
		name   string
		d      Dialect
		kind   Kind
		empty  bool
		want   string
		text   int
		wrap   string
		wanted string
	}{
		{"postgres table", Postgres{}, KindTable, true, "", 0, "", ""},
		{"postgres view", Postgres{}, KindView, false, "pg_get_viewdef", 0, "SELECT 1", "CREATE OR REPLACE VIEW \"public\".\"users\" AS\nSELECT 1;\n"},
		{"postgres materialized view", Postgres{}, KindMaterializedView, false, "pg_get_viewdef", 0, "SELECT 1;", "CREATE MATERIALIZED VIEW \"public\".\"users\" AS\nSELECT 1;\n"},
		{"postgres function", Postgres{}, KindFunction, false, "pg_get_functiondef($1::oid)", 0, "CREATE FUNCTION f() ...", "CREATE FUNCTION f() ...;\n"},
		{"mysql table", MySQL{}, KindTable, false, "SHOW CREATE TABLE `app`.`users`", 1, "CREATE TABLE x (a int)", "CREATE TABLE x (a int);\n"},
		{"mysql view", MySQL{}, KindView, false, "SHOW CREATE VIEW `app`.`users`", 1, "", ";\n"},
		{"mysql function", MySQL{}, KindFunction, false, "SHOW CREATE FUNCTION `app`.`users`", 2, "", ";\n"},
		{"mysql procedure", MySQL{}, KindProcedure, false, "SHOW CREATE PROCEDURE `app`.`users`", 2, "", ";\n"},
		{"sqlite table", SQLite{}, KindTable, false, "sqlite_master", 0, "CREATE TABLE t (a)", "CREATE TABLE t (a);\n"},
		{"sqlserver table", SQLServer{}, KindTable, true, "", 0, "", ""},
		{"sqlserver view", SQLServer{}, KindView, false, "sql_modules", 0, "CREATE VIEW v AS SELECT 1;\n", "CREATE VIEW v AS SELECT 1;\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := tt.d.Definition(tt.kind, ref)
			if q.Empty() != tt.empty {
				t.Fatalf("Empty() = %v, want %v (%s)", q.Empty(), tt.empty, q.SQL)
			}
			if tt.empty {
				return
			}
			if !strings.Contains(q.SQL, tt.want) || q.Text != tt.text {
				t.Errorf("Definition = %q (text column %d), want it to contain %q (column %d)", q.SQL, q.Text, tt.want, tt.text)
			}
			if got := tt.d.Wrap(tt.kind, ref, tt.wrap); got != tt.wanted {
				t.Errorf("Wrap = %q, want %q", got, tt.wanted)
			}
		})
	}
}

func TestCancelPlans(t *testing.T) {
	tests := []struct {
		dialect   Dialect
		session   string
		kill      string
		byContext bool
	}{
		{Postgres{}, "SELECT pg_backend_pid()", "SELECT pg_cancel_backend(1234)", true},
		{MySQL{}, "SELECT CONNECTION_ID()", "KILL QUERY 1234", true},
		{SQLite{}, "", "", true},
		{SQLServer{}, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.dialect.ID(), func(t *testing.T) {
			p := tt.dialect.Cancel()
			if p.SessionID != tt.session || p.ByContext != tt.byContext {
				t.Errorf("plan = %+v", p)
			}
			if (p.Kill != nil) != (tt.kill != "") {
				t.Fatalf("Kill set = %v, want %v", p.Kill != nil, tt.kill != "")
			}
			if p.Kill != nil {
				if got := p.Kill("1234"); got != tt.kill {
					t.Errorf("Kill(1234) = %q, want %q", got, tt.kill)
				}
				if got, want := p.Kill("1; DROP TABLE x"), p.Kill("1"); got != want {
					t.Errorf("a session id that is not a number reached the statement: %q", got)
				}
			}
		})
	}
}

func TestReadOnlySessions(t *testing.T) {
	want := map[string][]string{
		"postgres":  {"SET default_transaction_read_only = on"},
		"mysql":     {"SET SESSION TRANSACTION READ ONLY"},
		"sqlite":    {"PRAGMA query_only = ON"},
		"sqlserver": nil,
	}
	for _, d := range All() {
		if got := d.ReadOnlySession(); !reflect.DeepEqual(got, want[d.ID()]) {
			t.Errorf("%s: %v, want %v", d.ID(), got, want[d.ID()])
		}
	}
}

func row(values ...any) Row {
	r := make(Row, len(values))
	for i, v := range values {
		if v != nil {
			r[i] = sql.NullString{String: v.(string), Valid: true}
		}
	}
	return r
}

func TestRowsAreDecodedByPosition(t *testing.T) {
	ref := Ref{Schema: "s"}
	objs := DecodeTables([]Row{row("a", "the a table"), row("b", nil)}, ref)
	if len(objs) != 2 || objs[0].Name != "a" || objs[0].Comment != "the a table" || objs[1].Comment != "" || objs[0].Kind != KindTable || objs[0].Schema != "s" {
		t.Errorf("tables = %+v", objs)
	}
	views := DecodeViews([]Row{row("v", nil, "view"), row("m", "x", "materialized view")}, ref)
	if views[0].Kind != KindView || views[1].Kind != KindMaterializedView || views[1].Comment != "x" {
		t.Errorf("views = %+v", views)
	}
	routines := DecodeRoutines([]Row{row("f", "function", "a integer", "text", "17"), row("p", "procedure", nil, nil, nil)}, ref)
	if routines[0].Kind != KindFunction || routines[0].Args != "a integer" || routines[0].Result != "text" || routines[0].ID != "17" || routines[1].Kind != KindProcedure {
		t.Errorf("routines = %+v", routines)
	}
	cols := DecodeColumns([]Row{row("id", "integer", "0", nil, "1"), row("name", "text", "1", "'x'", "0"), row("e", "text", "true", "", "false")})
	want := []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "name", Type: "text", Nullable: true, Default: "'x'", HasDefault: true},
		{Name: "e", Type: "text", Nullable: true, HasDefault: true},
	}
	if !reflect.DeepEqual(cols, want) {
		t.Errorf("columns = %+v, want %+v", cols, want)
	}
}

func TestIndexesAndForeignKeysGatherTheirColumns(t *testing.T) {
	ixs := DecodeIndexes([]Row{
		row("pk", "a", "1", "1", "1"), row("pk", "b", "1", "1", "2"),
		row("by_name", "name", "0", "0", "1"), row("expr", nil, "0", "0", "1"),
	})
	want := []Index{
		{Name: "pk", Columns: []string{"a", "b"}, Unique: true, Primary: true},
		{Name: "by_name", Columns: []string{"name"}},
		{Name: "expr", Columns: []string{"(expression)"}},
	}
	if !reflect.DeepEqual(ixs, want) {
		t.Errorf("indexes = %+v, want %+v", ixs, want)
	}
	fks := DecodeForeignKeys([]Row{
		row("fk1", "a", "public", "t", "x", "1"), row("fk1", "b", "public", "t", "y", "2"), row("fk2", "c", "public", "u", "z", "1"),
	})
	wantFK := []ForeignKey{
		{Name: "fk1", Columns: []string{"a", "b"}, Schema: "public", Table: "t", RefColumns: []string{"x", "y"}},
		{Name: "fk2", Columns: []string{"c"}, Schema: "public", Table: "u", RefColumns: []string{"z"}},
	}
	if !reflect.DeepEqual(fks, wantFK) {
		t.Errorf("foreign keys = %+v, want %+v", fks, wantFK)
	}
	cols := []Column{{Name: "a"}, {Name: "c"}, {Name: "d"}}
	MarkForeignKeys(cols, fks)
	if !cols[0].ForeignKey || !cols[1].ForeignKey || cols[2].ForeignKey {
		t.Errorf("marked = %+v", cols)
	}
	if got := (Row{}).number(3); got != 0 {
		t.Errorf("number of a short row = %d", got)
	}
}
