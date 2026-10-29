package dialect

import (
	"strings"
	"testing"
)

// fixture is a table with a composite primary key, a foreign key, a nullable column with a default and a second index. The
// foreign key points at "orders" in the same schema (the same database for MySQL, whose catalog calls it the schema).
func fixture(d Dialect, ref Ref) Table {
	target := ref.Schema
	if !d.Levels().Schemas {
		target = ref.Database
	}
	return Table{
		Ref: ref, Kind: KindTable,
		Columns: []Column{
			{Name: "order_id", Type: "integer", PrimaryKey: true, ForeignKey: true},
			{Name: "line", Type: "integer", PrimaryKey: true},
			{Name: "sku", Type: "varchar(20)", Nullable: true, HasDefault: true, Default: "'x'"},
			{Name: "qty", Type: "integer", HasDefault: true, Default: "1"},
		},
		Indexes:     []Index{{Name: "items_pkey", Columns: []string{"order_id", "line"}, Unique: true, Primary: true}, {Name: "items_sku", Columns: []string{"sku"}}},
		ForeignKeys: []ForeignKey{{Name: "fk_order", Columns: []string{"order_id"}, Schema: target, Table: "orders", RefColumns: []string{"id"}}},
	}
}

// quotes returns the pair of characters a dialect quotes with.
func quotes(d Dialect) (string, string) {
	q := d.Quote("x")
	return q[:1], q[len(q)-1:]
}

// expand writes a template for a dialect: « and » are its quotes and {T} the qualified table, {O} the qualified orders table.
func expand(d Dialect, tmpl string, table, orders Ref) string {
	open, close := quotes(d)
	return strings.NewReplacer("«", open, "»", close, "{T}", d.Qualified(table), "{O}", d.Qualified(orders)).Replace(tmpl)
}

func TestGeneratedStatementsFollowTheDialect(t *testing.T) {
	tests := []struct {
		name string
		gen  func(Dialect, Table) string
		want string
	}{
		{"select", GenerateSelect, `SELECT
    «order_id»,
    «line»,
    «sku»,
    «qty»
FROM {T};
`},
		{"insert", GenerateInsert, `INSERT INTO {T} («order_id», «line», «sku», «qty»)
VALUES (
    NULL, -- order_id integer not null
    NULL, -- line integer not null
    NULL, -- sku varchar(20)
    NULL  -- qty integer not null
);
`},
		{"update sets the columns that are not the key and matches on the key", GenerateUpdate, `UPDATE {T}
SET
    «sku» = NULL, -- sku varchar(20)
    «qty» = NULL  -- qty integer not null
WHERE «order_id» = NULL
  AND «line» = NULL;
`},
		{"delete matches on the key", GenerateDelete, `DELETE FROM {T}
WHERE «order_id» = NULL
  AND «line» = NULL;
`},
		{"create", GenerateCreate, `CREATE TABLE {T} (
    «order_id» integer NOT NULL,
    «line» integer NOT NULL,
    «sku» varchar(20) DEFAULT 'x',
    «qty» integer NOT NULL DEFAULT 1,
    CONSTRAINT «items_pkey» PRIMARY KEY («order_id», «line»),
    CONSTRAINT «fk_order» FOREIGN KEY («order_id») REFERENCES {O} («id»)
);

CREATE INDEX «items_sku» ON {T} («sku»);
`},
	}
	for _, d := range All() {
		table := Ref{Database: "shop", Schema: "public", Name: "order items"}
		orders := Ref{Database: "shop", Schema: "public", Name: "orders"}
		for _, tt := range tests {
			t.Run(d.ID()+" "+tt.name, func(t *testing.T) {
				got := tt.gen(d, fixture(d, table))
				if want := expand(d, tt.want, table, orders); got != want {
					t.Errorf("got:\n%s\nwant:\n%s", got, want)
				}
			})
		}
	}
}

func TestAnUpdateThatIsRunBeforeItIsFilledInChangesNothing(t *testing.T) {
	// the WHERE of UPDATE and DELETE compares with NULL, which is never true, and a table with no key gets a placeholder that is a
	// syntax error
	noKey := Table{Ref: Ref{Name: "t"}, Columns: []Column{{Name: "a", Type: "int", Nullable: true}, {Name: "b", Type: "int", Nullable: true}}}
	for _, d := range All() {
		for name, gen := range map[string]func(Dialect, Table) string{"update": GenerateUpdate, "delete": GenerateDelete} {
			got := gen(d, noKey)
			if !strings.Contains(got, "WHERE <condition>") {
				t.Errorf("%s %s of a table with no key has no placeholder condition:\n%s", d.ID(), name, got)
			}
		}
		if got := GenerateUpdate(d, fixture(d, Ref{Name: "t"})); !strings.Contains(got, "= NULL\n  AND") && !strings.Contains(got, "= NULL;") {
			t.Errorf("%s: %s", d.ID(), got)
		}
	}
}

func TestTablesWithoutColumnsStillGiveStatements(t *testing.T) {
	empty := Table{Ref: Ref{Schema: "s", Name: "t"}}
	for _, d := range All() {
		if got := GenerateSelect(d, empty); !strings.HasPrefix(got, "SELECT *\nFROM ") {
			t.Errorf("%s select = %q", d.ID(), got)
		}
		if got := GenerateInsert(d, empty); !strings.Contains(got, "no columns") {
			t.Errorf("%s insert = %q", d.ID(), got)
		}
	}
}

func TestPrimaryKeyOfMySQLIsNotNamed(t *testing.T) {
	tb := fixture(MySQL{}, Ref{Database: "shop", Name: "t"})
	tb.Indexes[0].Name = "PRIMARY"
	got := GenerateCreate(MySQL{}, tb)
	if !strings.Contains(got, "    PRIMARY KEY (`order_id`, `line`),") || strings.Contains(got, "CONSTRAINT `PRIMARY`") {
		t.Errorf("create = %s", got)
	}
	if !strings.Contains(got, "REFERENCES `shop`.`orders`") {
		t.Errorf("MySQL names the database of the referenced table:\n%s", got)
	}
}

func TestDropStatements(t *testing.T) {
	r := Ref{Database: "shop", Schema: "public", Name: "f", Args: "integer, text"}
	tests := []struct {
		dialect Dialect
		kind    Kind
		want    string
	}{
		{Postgres{}, KindTable, `DROP TABLE "public"."f";` + "\n"},
		{Postgres{}, KindView, `DROP VIEW "public"."f";` + "\n"},
		{Postgres{}, KindMaterializedView, `DROP MATERIALIZED VIEW "public"."f";` + "\n"},
		{Postgres{}, KindFunction, `DROP FUNCTION "public"."f"(integer, text);` + "\n"},
		{Postgres{}, KindProcedure, `DROP PROCEDURE "public"."f"(integer, text);` + "\n"},
		{MySQL{}, KindFunction, "DROP FUNCTION `shop`.`f`;\n"},
		{MySQL{}, KindTable, "DROP TABLE `shop`.`f`;\n"},
		{SQLServer{}, KindProcedure, "DROP PROCEDURE [shop].[public].[f];\n"},
		{SQLite{}, KindView, "DROP VIEW \"public\".\"f\";\n"},
	}
	for _, tt := range tests {
		t.Run(tt.dialect.ID()+" "+string(tt.kind), func(t *testing.T) {
			if got := GenerateDrop(tt.dialect, tt.kind, r); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
