// Package dialect is what differs between the four SQL engines the SQL extension speaks to (SPEC §10.21 "Drivers", D23):
// PostgreSQL, MySQL and MariaDB, SQLite and SQL Server. A Dialect is only text and tables: it writes the catalog queries that list
// databases, schemas, tables, views, columns, indexes, foreign keys and routines, quotes identifiers, says how to take the first
// rows of a table, how to cancel a running statement and how to make a session read-only, and generates statements from what the
// catalog said. It opens no connection and imports no driver, so every part of it is tested without a database; the SQL extension
// runs the queries and gives the rows back to the Decode functions.
//
// # The shape of a catalog query
//
// Each query returns its columns in the order its method documents, so that a caller can read the rows by position whatever the
// engine. A value that is a yes or no is 1 or 0. A query takes the names it filters on as arguments (placeholders in the style of
// the driver), and a name that has to be written in the statement itself (the database of SQL Server's [db].sys.tables, SQLite's
// schema, a SHOW CREATE) is quoted with Quote. Nothing is ever put in a statement unquoted.
package dialect

import (
	"database/sql"
	"strconv"
	"strings"
)

// Kind is the kind of an object of a schema.
type Kind string

// The kinds of object the explorer shows.
const (
	// KindTable is a table.
	KindTable Kind = "table"
	// KindView is a view.
	KindView Kind = "view"
	// KindMaterializedView is a view whose rows are stored (PostgreSQL).
	KindMaterializedView Kind = "materialized view"
	// KindFunction is a function.
	KindFunction Kind = "function"
	// KindProcedure is a stored procedure.
	KindProcedure Kind = "procedure"
)

// Ref names an object of the catalog. Which parts an engine uses depends on its Levels.
type Ref struct {
	// Database and Schema hold the parts above the object; one of them is empty for an engine that has no such level (MySQL has no
	// schema level: its databases are what other engines call schemas).
	Database, Schema string
	// Name is the object.
	Name string
	// Args is the argument list of a routine as the catalog writes it ("integer, text"), which PostgreSQL needs to tell routines of
	// one name apart.
	Args string
	// ID is the engine's own key of the object when the name is not enough (a PostgreSQL routine's OID); empty otherwise.
	ID string
}

// Levels tell which levels of the tree an engine has above its objects.
type Levels struct {
	// Databases says that the connection reaches several databases (PostgreSQL, MySQL, SQL Server).
	Databases bool
	// Schemas says that a database has schemas (PostgreSQL, SQL Server; for SQLite the attached databases, main among them).
	Schemas bool
	// Routines says that the engine has stored functions and procedures (not SQLite).
	Routines bool
	// SeparateConnections says that a connection serves one database, so another database needs a connection of its own
	// (PostgreSQL). The other engines read the catalog of another database through the connection they have.
	SeparateConnections bool
}

// Query is a statement and its arguments.
type Query struct {
	// SQL is the statement; empty means that the engine has nothing to ask (a level it does not have).
	SQL string
	// Args are the values of its placeholders, in order.
	Args []any
	// Text is, for a query that returns one text (a definition), the index of the column that holds it.
	Text int
}

// Empty tells whether there is nothing to ask.
func (q Query) Empty() bool { return q.SQL == "" }

// CancelPlan says how a running statement is cancelled.
type CancelPlan struct {
	// ByContext says that ending the context of the call cancels the statement (the driver sends the cancel request, the attention
	// or the interrupt). When Kill is set it is the better way, because it leaves the session as it is; ByContext is then what
	// remains when Kill cannot be run.
	ByContext bool
	// SessionID is a query that returns the id of the session, which Kill needs; empty when Kill is nil.
	SessionID string
	// Kill returns the statement that cancels the running statement of the session with the id, to be run on another connection
	// (MySQL's KILL QUERY, PostgreSQL's pg_cancel_backend). Nil when ending the context is the way.
	Kill func(sessionID string) string
}

// Dialect is one engine.
type Dialect interface {
	// ID is the id of the engine, the Engine.ID of the connection: "postgres", "mysql", "sqlite", "sqlserver".
	ID() string
	// Name is the name for the user.
	Name() string
	// DriverName is the name the engine's driver registers with database/sql.
	DriverName() string
	// Levels says which levels the engine has.
	Levels() Levels

	// Quote quotes an identifier. QuoteString writes a string literal.
	Quote(identifier string) string
	QuoteString(s string) string
	// Qualified writes the name of an object as a statement uses it: PostgreSQL schema.name (a connection serves one database),
	// MySQL database.name, SQL Server database.schema.name (the database when the ref has one, as VS Code's SQL Server extension writes
	// it), SQLite name (schema.name for an attached database).
	Qualified(r Ref) string

	// Databases lists the databases: name. system adds the ones the engine keeps for itself.
	Databases(system bool) Query
	// Schemas lists the schemas of a database: name.
	Schemas(database string, system bool) Query
	// Tables lists the tables of a schema (of a database, for MySQL): name, comment.
	Tables(r Ref, system bool) Query
	// Views lists the views: name, comment, kind (Kind, as text).
	Views(r Ref) Query
	// Routines lists the functions and procedures: name, kind (Kind, as text), arguments, result type, id.
	Routines(r Ref) Query
	// Columns lists the columns of a table or view in order: name, type, nullable, default, primary key.
	Columns(r Ref) Query
	// Indexes lists the columns of the indexes of a table, in order: index, column, unique, primary, position.
	Indexes(r Ref) Query
	// ForeignKeys lists the columns of the foreign keys of a table: name, column, referenced schema, referenced table, referenced
	// column, position. Referenced schema is the database for MySQL.
	ForeignKeys(r Ref) Query
	// Definition asks the catalog for the text that creates the object (the SQL of a view, the body of a routine). It is empty for
	// a table where Create writes the statement from the columns. Wrap turns the text into a statement.
	Definition(kind Kind, r Ref) Query
	// Wrap makes the statement that creates the object from what Definition returned.
	Wrap(kind Kind, r Ref, definition string) string

	// SelectTop writes a statement that reads the first n rows of a table.
	SelectTop(r Ref, n int) string
	// Cancel says how a running statement is cancelled.
	Cancel() CancelPlan
	// ReadOnlySession lists the statements that make a session read-only, run on each new connection of a read-only connection;
	// the engine's other ways (SQLite's mode=ro, SQL Server's ApplicationIntent) are in the connection string the extension builds.
	ReadOnlySession() []string
}

// All lists the dialects in the order the engines are shown.
func All() []Dialect { return []Dialect{Postgres{}, MySQL{}, SQLite{}, SQLServer{}} }

// ByID returns the dialect with the engine id.
func ByID(id string) (Dialect, bool) {
	for _, d := range All() {
		if d.ID() == id {
			return d, true
		}
	}
	return nil, false
}

// Row is one row of a catalog query, read as text; Null marks a NULL.
type Row []sql.NullString

// text returns column i, or "" when the row is short or the value NULL.
func (r Row) text(i int) string {
	if i < 0 || i >= len(r) || !r[i].Valid {
		return ""
	}
	return r[i].String
}

// flag reads a yes or no column.
func (r Row) flag(i int) bool {
	switch strings.ToLower(strings.TrimSpace(r.text(i))) {
	case "1", "t", "true", "y", "yes":
		return true
	}
	return false
}

// number reads a whole number column; 0 when it is not one.
func (r Row) number(i int) int {
	n, err := strconv.Atoi(strings.TrimSpace(r.text(i)))
	if err != nil {
		return 0
	}
	return n
}

// Object is a table, view or routine of a listing.
type Object struct {
	Ref
	// Kind is what it is.
	Kind Kind
	// Comment is the comment of the object in the catalog, if it has one.
	Comment string
	// Result is the result type of a function.
	Result string
}

// DecodeTables reads the rows of Tables.
func DecodeTables(rows []Row, r Ref) []Object {
	out := make([]Object, 0, len(rows))
	for _, row := range rows {
		o := Object{Ref: r, Kind: KindTable, Comment: row.text(1)}
		o.Name = row.text(0)
		out = append(out, o)
	}
	return out
}

// DecodeViews reads the rows of Views.
func DecodeViews(rows []Row, r Ref) []Object {
	out := make([]Object, 0, len(rows))
	for _, row := range rows {
		o := Object{Ref: r, Kind: KindView, Comment: row.text(1)}
		if row.text(2) == string(KindMaterializedView) {
			o.Kind = KindMaterializedView
		}
		o.Name = row.text(0)
		out = append(out, o)
	}
	return out
}

// DecodeRoutines reads the rows of Routines.
func DecodeRoutines(rows []Row, r Ref) []Object {
	out := make([]Object, 0, len(rows))
	for _, row := range rows {
		o := Object{Ref: r, Kind: KindFunction, Result: row.text(3)}
		if row.text(1) == string(KindProcedure) {
			o.Kind = KindProcedure
		}
		o.Name, o.Args, o.ID = row.text(0), row.text(2), row.text(4)
		out = append(out, o)
	}
	return out
}

// Column is a column of a table or view.
type Column struct {
	Name, Type string
	// Nullable says that the column accepts NULL.
	Nullable bool
	// Default is the default expression; HasDefault tells whether there is one (an empty text is a default too).
	Default    string
	HasDefault bool
	// PrimaryKey says that the column is part of the primary key; ForeignKey that it is part of a foreign key.
	PrimaryKey, ForeignKey bool
}

// DecodeColumns reads the rows of Columns.
func DecodeColumns(rows []Row) []Column {
	out := make([]Column, 0, len(rows))
	for _, row := range rows {
		out = append(out, Column{
			Name: row.text(0), Type: row.text(1), Nullable: row.flag(2), PrimaryKey: row.flag(4),
			Default: row.text(3), HasDefault: len(row) > 3 && row[3].Valid,
		})
	}
	return out
}

// Index is an index with its columns in order.
type Index struct {
	Name    string
	Columns []string
	// Unique and Primary say what the index enforces.
	Unique, Primary bool
}

// DecodeIndexes reads the rows of Indexes, which come ordered by index and position.
func DecodeIndexes(rows []Row) []Index {
	var out []Index
	for _, row := range rows {
		name := row.text(0)
		col := row.text(1)
		if col == "" {
			col = "(expression)"
		}
		if n := len(out); n > 0 && out[n-1].Name == name {
			out[n-1].Columns = append(out[n-1].Columns, col)
			continue
		}
		out = append(out, Index{Name: name, Columns: []string{col}, Unique: row.flag(2), Primary: row.flag(3)})
	}
	return out
}

// ForeignKey is a foreign key with its columns in order.
type ForeignKey struct {
	Name    string
	Columns []string
	// Schema and Table are the referenced table (Schema is the database for MySQL), and RefColumns its columns.
	Schema, Table string
	RefColumns    []string
}

// DecodeForeignKeys reads the rows of ForeignKeys, which come ordered by key and position.
func DecodeForeignKeys(rows []Row) []ForeignKey {
	var out []ForeignKey
	for _, row := range rows {
		name := row.text(0)
		if n := len(out); n > 0 && out[n-1].Name == name {
			out[n-1].Columns = append(out[n-1].Columns, row.text(1))
			out[n-1].RefColumns = append(out[n-1].RefColumns, row.text(4))
			continue
		}
		out = append(out, ForeignKey{Name: name, Columns: []string{row.text(1)}, Schema: row.text(2), Table: row.text(3), RefColumns: []string{row.text(4)}})
	}
	return out
}

// MarkForeignKeys sets ForeignKey on the columns that a foreign key uses.
func MarkForeignKeys(cols []Column, fks []ForeignKey) {
	for _, fk := range fks {
		for _, name := range fk.Columns {
			for i := range cols {
				if cols[i].Name == name {
					cols[i].ForeignKey = true
				}
			}
		}
	}
}
