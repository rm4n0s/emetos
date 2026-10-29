package dialect

import (
	"strconv"
	"strings"
)

// MySQL is MySQL and MariaDB, through go-sql-driver/mysql. A MySQL "schema" is a database, so there is no schema level.
type MySQL struct{}

// ID implements Dialect.
func (MySQL) ID() string { return "mysql" }

// Name implements Dialect.
func (MySQL) Name() string { return "MySQL / MariaDB" }

// DriverName implements Dialect.
func (MySQL) DriverName() string { return "mysql" }

// Levels implements Dialect.
func (MySQL) Levels() Levels { return Levels{Databases: true, Routines: true} }

// Quote implements Dialect: `name`, a backtick doubled.
func (MySQL) Quote(identifier string) string { return quoteWith(identifier, '`', '`') }

// QuoteString implements Dialect: a backslash and a quote are escaped (the backslash is an escape unless NO_BACKSLASH_ESCAPES is on, and
// the doubled one is a backslash in both modes only when it is on, so a string with a backslash is written with its hex form).
func (MySQL) QuoteString(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	if strings.Contains(s, `\`) {
		return "CONVERT(0x" + hexOf(s) + " USING utf8mb4)"
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// Qualified implements Dialect: `database`.`name`.
func (m MySQL) Qualified(r Ref) string { return join(m, r.Database, r.Name) }

// mysqlSystem lists the databases MySQL and MariaDB keep for themselves.
const mysqlSystem = "('information_schema', 'mysql', 'performance_schema', 'sys')"

// Databases implements Dialect.
func (MySQL) Databases(system bool) Query {
	sql := "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA"
	if !system {
		sql += " WHERE SCHEMA_NAME NOT IN " + mysqlSystem
	}
	return Query{SQL: sql + " ORDER BY SCHEMA_NAME"}
}

// Schemas implements Dialect: MySQL has no schema level.
func (MySQL) Schemas(string, bool) Query { return Query{} }

// Tables implements Dialect. MariaDB's system-versioned tables are tables too.
func (MySQL) Tables(r Ref, _ bool) Query {
	return Query{SQL: `SELECT TABLE_NAME, TABLE_COMMENT FROM information_schema.TABLES
WHERE TABLE_SCHEMA = ? AND TABLE_TYPE IN ('BASE TABLE', 'SYSTEM VERSIONED')
ORDER BY TABLE_NAME`, Args: []any{r.Database}}
}

// Views implements Dialect.
func (MySQL) Views(r Ref) Query {
	return Query{SQL: `SELECT TABLE_NAME, TABLE_COMMENT, 'view' FROM information_schema.TABLES
WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'VIEW'
ORDER BY TABLE_NAME`, Args: []any{r.Database}}
}

// Routines implements Dialect. The arguments are written from information_schema.PARAMETERS (position 0 is the result of a function).
func (MySQL) Routines(r Ref) Query {
	return Query{SQL: `SELECT r.ROUTINE_NAME, LOWER(r.ROUTINE_TYPE),
  (SELECT GROUP_CONCAT(CONCAT(IFNULL(p.PARAMETER_NAME, ''), ' ', p.DTD_IDENTIFIER) ORDER BY p.ORDINAL_POSITION SEPARATOR ', ')
   FROM information_schema.PARAMETERS p
   WHERE p.SPECIFIC_SCHEMA = r.ROUTINE_SCHEMA AND p.SPECIFIC_NAME = r.SPECIFIC_NAME AND p.ORDINAL_POSITION > 0),
  r.DTD_IDENTIFIER, ''
FROM information_schema.ROUTINES r
WHERE r.ROUTINE_SCHEMA = ?
ORDER BY r.ROUTINE_NAME`, Args: []any{r.Database}}
}

// Columns implements Dialect.
func (MySQL) Columns(r Ref) Query {
	return Query{SQL: `SELECT COLUMN_NAME, COLUMN_TYPE, IF(IS_NULLABLE = 'YES', 1, 0), COLUMN_DEFAULT, IF(COLUMN_KEY = 'PRI', 1, 0)
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
ORDER BY ORDINAL_POSITION`, Args: []any{r.Database, r.Name}}
}

// Indexes implements Dialect.
func (MySQL) Indexes(r Ref) Query {
	return Query{SQL: `SELECT INDEX_NAME, COLUMN_NAME, IF(NON_UNIQUE = 0, 1, 0), IF(INDEX_NAME = 'PRIMARY', 1, 0), SEQ_IN_INDEX
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
ORDER BY INDEX_NAME, SEQ_IN_INDEX`, Args: []any{r.Database, r.Name}}
}

// ForeignKeys implements Dialect.
func (MySQL) ForeignKeys(r Ref) Query {
	return Query{SQL: `SELECT CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME, ORDINAL_POSITION
FROM information_schema.KEY_COLUMN_USAGE
WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND REFERENCED_TABLE_NAME IS NOT NULL
ORDER BY CONSTRAINT_NAME, ORDINAL_POSITION`, Args: []any{r.Database, r.Name}}
}

// Definition implements Dialect: SHOW CREATE, which writes the whole statement.
func (m MySQL) Definition(kind Kind, r Ref) Query {
	name := m.Qualified(r)
	switch kind {
	case KindTable:
		return Query{SQL: "SHOW CREATE TABLE " + name, Text: 1}
	case KindView:
		return Query{SQL: "SHOW CREATE VIEW " + name, Text: 1}
	case KindFunction:
		return Query{SQL: "SHOW CREATE FUNCTION " + name, Text: 2}
	case KindProcedure:
		return Query{SQL: "SHOW CREATE PROCEDURE " + name, Text: 2}
	}
	return Query{}
}

// Wrap implements Dialect: SHOW CREATE gave the statement.
func (MySQL) Wrap(_ Kind, _ Ref, definition string) string { return terminate(definition) }

// SelectTop implements Dialect: LIMIT.
func (m MySQL) SelectTop(r Ref, n int) string {
	return "SELECT * FROM " + m.Qualified(r) + " LIMIT " + strconv.Itoa(n) + ";"
}

// Cancel implements Dialect: the driver closes the connection when a context ends and the server goes on with the statement, so
// KILL QUERY is run from another connection.
func (MySQL) Cancel() CancelPlan {
	return CancelPlan{
		SessionID: "SELECT CONNECTION_ID()",
		Kill:      func(id string) string { return "KILL QUERY " + digits(id) },
		ByContext: true,
	}
}

// ReadOnlySession implements Dialect.
func (MySQL) ReadOnlySession() []string { return []string{"SET SESSION TRANSACTION READ ONLY"} }

// hexOf writes the bytes of s in hex.
func hexOf(s string) string {
	const digits = "0123456789abcdef"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteByte(digits[s[i]>>4])
		b.WriteByte(digits[s[i]&15])
	}
	return b.String()
}
