package dialect

import (
	"strconv"
	"strings"
)

// Postgres is PostgreSQL, through pgx's database/sql driver.
type Postgres struct{}

// ID implements Dialect.
func (Postgres) ID() string { return "postgres" }

// Name implements Dialect.
func (Postgres) Name() string { return "PostgreSQL" }

// DriverName implements Dialect: the name pgx's stdlib package registers.
func (Postgres) DriverName() string { return "pgx" }

// Levels implements Dialect.
func (Postgres) Levels() Levels {
	return Levels{Databases: true, Schemas: true, Routines: true, SeparateConnections: true}
}

// Quote implements Dialect: "name", a quote doubled.
func (Postgres) Quote(identifier string) string { return quoteWith(identifier, '"', '"') }

// QuoteString implements Dialect: standard strings, a quote doubled. A backslash is nothing special (standard_conforming_strings
// is on since PostgreSQL 9.1).
func (Postgres) QuoteString(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\x00", ""), "'", "''") + "'"
}

// Qualified implements Dialect: "schema"."name".
func (p Postgres) Qualified(r Ref) string { return join(p, r.Schema, r.Name) }

// Databases implements Dialect. The databases a user cannot connect to are left out, and the templates unless system is asked.
func (Postgres) Databases(system bool) Query {
	sql := "SELECT datname FROM pg_database WHERE datallowconn"
	if !system {
		sql += " AND NOT datistemplate"
	}
	return Query{SQL: sql + " ORDER BY datname"}
}

// Schemas implements Dialect. The schemas the user may not use are left out, and the system ones unless system is asked.
func (Postgres) Schemas(_ string, system bool) Query {
	sql := `SELECT nspname FROM pg_namespace
WHERE nspname NOT LIKE 'pg\_toast%' AND nspname NOT LIKE 'pg\_temp\_%' AND has_schema_privilege(oid, 'USAGE')`
	if !system {
		sql += " AND nspname NOT IN ('pg_catalog', 'information_schema')"
	}
	return Query{SQL: sql + " ORDER BY nspname"}
}

// Tables implements Dialect: ordinary, partitioned and foreign tables.
func (Postgres) Tables(r Ref, _ bool) Query {
	return Query{SQL: `SELECT c.relname, obj_description(c.oid, 'pg_class')
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p', 'f')
ORDER BY c.relname`, Args: []any{r.Schema}}
}

// Views implements Dialect: views and materialized views.
func (Postgres) Views(r Ref) Query {
	return Query{SQL: `SELECT c.relname, obj_description(c.oid, 'pg_class'),
  CASE c.relkind WHEN 'm' THEN 'materialized view' ELSE 'view' END
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind IN ('v', 'm')
ORDER BY c.relname`, Args: []any{r.Schema}}
}

// Routines implements Dialect. The OID is the id: functions of one name differ by their arguments.
func (Postgres) Routines(r Ref) Query {
	return Query{SQL: `SELECT p.proname, CASE p.prokind WHEN 'p' THEN 'procedure' ELSE 'function' END,
  pg_get_function_identity_arguments(p.oid), pg_get_function_result(p.oid), p.oid::text
FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
WHERE n.nspname = $1
ORDER BY p.proname, p.oid`, Args: []any{r.Schema}}
}

// Columns implements Dialect.
func (Postgres) Columns(r Ref) Query {
	return Query{SQL: `SELECT a.attname, format_type(a.atttypid, a.atttypmod), CASE WHEN a.attnotnull THEN 0 ELSE 1 END,
  pg_get_expr(d.adbin, d.adrelid),
  CASE WHEN EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = a.attrelid AND i.indisprimary AND a.attnum = ANY (i.indkey)) THEN 1 ELSE 0 END
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`, Args: []any{r.Schema, r.Name}}
}

// Indexes implements Dialect. An expression index shows the expression where a column would be.
func (Postgres) Indexes(r Ref) Query {
	return Query{SQL: `SELECT ic.relname, pg_get_indexdef(i.indexrelid, g.n, true),
  CASE WHEN i.indisunique THEN 1 ELSE 0 END, CASE WHEN i.indisprimary THEN 1 ELSE 0 END, g.n
FROM pg_index i
JOIN pg_class ic ON ic.oid = i.indexrelid
JOIN pg_class t ON t.oid = i.indrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
CROSS JOIN LATERAL generate_series(1, i.indnkeyatts::int) AS g(n)
WHERE n.nspname = $1 AND t.relname = $2
ORDER BY ic.relname, g.n`, Args: []any{r.Schema, r.Name}}
}

// ForeignKeys implements Dialect.
func (Postgres) ForeignKeys(r Ref) Query {
	return Query{SQL: `SELECT con.conname, a.attname, fn.nspname, fc.relname, fa.attname, k.ord
FROM pg_constraint con
JOIN pg_class c ON c.oid = con.conrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_class fc ON fc.oid = con.confrelid
JOIN pg_namespace fn ON fn.oid = fc.relnamespace
CROSS JOIN LATERAL unnest(con.conkey, con.confkey) WITH ORDINALITY AS k(attnum, fattnum, ord)
JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
JOIN pg_attribute fa ON fa.attrelid = con.confrelid AND fa.attnum = k.fattnum
WHERE con.contype = 'f' AND n.nspname = $1 AND c.relname = $2
ORDER BY con.conname, k.ord`, Args: []any{r.Schema, r.Name}}
}

// Definition implements Dialect: pg_get_viewdef and pg_get_functiondef; a table is written from its columns.
func (Postgres) Definition(kind Kind, r Ref) Query {
	switch kind {
	case KindView, KindMaterializedView:
		return Query{SQL: `SELECT pg_get_viewdef(c.oid, true)
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2`, Args: []any{r.Schema, r.Name}}
	case KindFunction, KindProcedure:
		return Query{SQL: "SELECT pg_get_functiondef($1::oid)", Args: []any{r.ID}}
	}
	return Query{}
}

// Wrap implements Dialect: a view's query gets its CREATE; pg_get_functiondef is a statement already.
func (p Postgres) Wrap(kind Kind, r Ref, definition string) string {
	switch kind {
	case KindView:
		return "CREATE OR REPLACE VIEW " + p.Qualified(r) + " AS\n" + terminate(definition)
	case KindMaterializedView:
		return "CREATE MATERIALIZED VIEW " + p.Qualified(r) + " AS\n" + terminate(definition)
	}
	return terminate(definition)
}

// SelectTop implements Dialect: LIMIT.
func (p Postgres) SelectTop(r Ref, n int) string {
	return "SELECT * FROM " + p.Qualified(r) + " LIMIT " + strconv.Itoa(n) + ";"
}

// Cancel implements Dialect: pg_cancel_backend from another connection stops the statement and keeps the session (and so an open
// transaction), where ending the context would make pgx close the connection.
func (Postgres) Cancel() CancelPlan {
	return CancelPlan{
		SessionID: "SELECT pg_backend_pid()",
		Kill:      func(id string) string { return "SELECT pg_cancel_backend(" + digits(id) + ")" },
		ByContext: true,
	}
}

// ReadOnlySession implements Dialect.
func (Postgres) ReadOnlySession() []string { return []string{"SET default_transaction_read_only = on"} }
