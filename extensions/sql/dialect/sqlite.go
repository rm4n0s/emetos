package dialect

import (
	"strconv"
	"strings"
)

// SQLite is SQLite through the pure Go driver modernc.org/sqlite. A file is one database; its "schemas" are the databases attached to
// the connection, main among them.
type SQLite struct{}

// mainSchema is the name of the file the connection opened.
const mainSchema = "main"

// ID implements Dialect.
func (SQLite) ID() string { return "sqlite" }

// Name implements Dialect.
func (SQLite) Name() string { return "SQLite" }

// DriverName implements Dialect.
func (SQLite) DriverName() string { return "sqlite" }

// Levels implements Dialect: no databases, schemas (attached files), no routines.
func (SQLite) Levels() Levels { return Levels{Schemas: true} }

// Quote implements Dialect: "name", a quote doubled.
func (SQLite) Quote(identifier string) string { return quoteWith(identifier, '"', '"') }

// QuoteString implements Dialect.
func (SQLite) QuoteString(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\x00", ""), "'", "''") + "'"
}

// Qualified implements Dialect: "name", or "schema"."name" for an attached database.
func (s SQLite) Qualified(r Ref) string {
	if r.Schema == mainSchema {
		return join(s, r.Name)
	}
	return join(s, r.Schema, r.Name)
}

// schema is the schema of a ref, main when it has none.
func (SQLite) schema(r Ref) string {
	if r.Schema == "" {
		return mainSchema
	}
	return r.Schema
}

// Databases implements Dialect: a file has no databases level.
func (SQLite) Databases(bool) Query { return Query{} }

// Schemas implements Dialect: the attached databases.
func (SQLite) Schemas(string, bool) Query {
	return Query{SQL: "SELECT name FROM pragma_database_list ORDER BY seq"}
}

// master is the table of the catalog of a schema.
func (s SQLite) master(r Ref) string { return s.Quote(s.schema(r)) + ".sqlite_master" }

// Tables implements Dialect. The tables SQLite keeps for itself (sqlite_sequence, sqlite_stat1) are left out unless system is asked.
func (s SQLite) Tables(r Ref, system bool) Query {
	sql := "SELECT name, NULL FROM " + s.master(r) + " WHERE type = 'table'"
	if !system {
		sql += ` AND name NOT LIKE 'sqlite\_%' ESCAPE '\'`
	}
	return Query{SQL: sql + " ORDER BY name"}
}

// Views implements Dialect.
func (s SQLite) Views(r Ref) Query {
	return Query{SQL: "SELECT name, NULL, 'view' FROM " + s.master(r) + " WHERE type = 'view' ORDER BY name"}
}

// Routines implements Dialect: SQLite has none.
func (SQLite) Routines(Ref) Query { return Query{} }

// Columns implements Dialect with the table-valued form of PRAGMA table_info. A column of the primary key counts as NOT NULL, which is
// what an INTEGER PRIMARY KEY is although table_info says otherwise.
func (s SQLite) Columns(r Ref) Query {
	return Query{SQL: `SELECT name, type, CASE WHEN "notnull" = 0 AND pk = 0 THEN 1 ELSE 0 END, dflt_value, CASE WHEN pk > 0 THEN 1 ELSE 0 END
FROM pragma_table_info(?, ?) ORDER BY cid`, Args: []any{r.Name, s.schema(r)}}
}

// Indexes implements Dialect: index_list gives the indexes, index_info their columns.
func (s SQLite) Indexes(r Ref) Query {
	return Query{SQL: `SELECT il.name, ii.name, il."unique", CASE WHEN il.origin = 'pk' THEN 1 ELSE 0 END, ii.seqno
FROM pragma_index_list(?, ?) AS il, pragma_index_info(il.name, ?) AS ii
ORDER BY il.name, ii.seqno`, Args: []any{r.Name, s.schema(r), s.schema(r)}}
}

// ForeignKeys implements Dialect. SQLite does not name them, so they are fk0, fk1... in the order it lists them.
func (s SQLite) ForeignKeys(r Ref) Query {
	return Query{SQL: `SELECT 'fk' || id, "from", ?, "table", "to", seq
FROM pragma_foreign_key_list(?, ?) ORDER BY id, seq`, Args: []any{s.schema(r), r.Name, s.schema(r)}}
}

// Definition implements Dialect: the sql column of the catalog is the statement that made the object.
func (s SQLite) Definition(_ Kind, r Ref) Query {
	return Query{SQL: "SELECT sql FROM " + s.master(r) + " WHERE name = ?", Args: []any{r.Name}}
}

// Wrap implements Dialect: the catalog kept the statement.
func (SQLite) Wrap(_ Kind, _ Ref, definition string) string { return terminate(definition) }

// SelectTop implements Dialect: LIMIT.
func (s SQLite) SelectTop(r Ref, n int) string {
	return "SELECT * FROM " + s.Qualified(r) + " LIMIT " + strconv.Itoa(n) + ";"
}

// Cancel implements Dialect: the driver interrupts the statement when the context ends.
func (SQLite) Cancel() CancelPlan { return CancelPlan{ByContext: true} }

// ReadOnlySession implements Dialect. The connection string says mode=ro as well.
func (SQLite) ReadOnlySession() []string { return []string{"PRAGMA query_only = ON"} }
