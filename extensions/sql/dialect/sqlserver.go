package dialect

import (
	"strconv"
)

// SQLServer is Microsoft SQL Server through go-mssqldb. The catalog of another database is read as [db].sys.tables, so one connection
// reaches all of them.
type SQLServer struct{}

// ID implements Dialect.
func (SQLServer) ID() string { return "sqlserver" }

// Name implements Dialect.
func (SQLServer) Name() string { return "SQL Server" }

// DriverName implements Dialect: the name go-mssqldb registers, which takes @p1 placeholders.
func (SQLServer) DriverName() string { return "sqlserver" }

// Levels implements Dialect.
func (SQLServer) Levels() Levels { return Levels{Databases: true, Schemas: true, Routines: true} }

// Quote implements Dialect: [name], a closing bracket doubled.
func (SQLServer) Quote(identifier string) string { return quoteWith(identifier, '[', ']') }

// QuoteString implements Dialect: N'..', a quote doubled (a Unicode literal, so that no character is lost).
func (SQLServer) QuoteString(s string) string {
	return "N'" + replaceAll(replaceAll(s, "\x00", ""), "'", "''") + "'"
}

// Qualified implements Dialect: [database].[schema].[name], with the database when the ref has one, so that a statement runs in
// whichever database its session is in (a connection to SQL Server is in one database at a time, and the explorer lists them all).
func (s SQLServer) Qualified(r Ref) string { return join(s, r.Database, r.Schema, r.Name) }

// catalog is the prefix of the catalog views of the database: [db].sys. (the database of the connection when none is given).
func (s SQLServer) catalog(r Ref) string {
	if r.Database == "" {
		return "sys."
	}
	return s.Quote(r.Database) + ".sys."
}

// Databases implements Dialect: the databases the login may use, without master, tempdb, model and msdb unless system is asked.
func (SQLServer) Databases(system bool) Query {
	sql := "SELECT name FROM sys.databases WHERE HAS_DBACCESS(name) = 1"
	if !system {
		sql += " AND database_id > 4"
	}
	return Query{SQL: sql + " ORDER BY name"}
}

// Schemas implements Dialect. The schemas of the fixed roles and of the system are left out unless system is asked.
func (s SQLServer) Schemas(database string, system bool) Query {
	sql := "SELECT name FROM " + s.catalog(Ref{Database: database}) + "schemas"
	if !system {
		sql += " WHERE name NOT IN ('sys', 'INFORMATION_SCHEMA', 'guest') AND name NOT LIKE 'db[_]%'"
	}
	return Query{SQL: sql + " ORDER BY name"}
}

// Tables implements Dialect. The comment is the MS_Description extended property.
func (s SQLServer) Tables(r Ref, _ bool) Query {
	c := s.catalog(r)
	return Query{SQL: `SELECT t.name, CAST(ep.value AS nvarchar(max))
FROM ` + c + `tables t
JOIN ` + c + `schemas s ON s.schema_id = t.schema_id
LEFT JOIN ` + c + `extended_properties ep ON ep.class = 1 AND ep.major_id = t.object_id AND ep.minor_id = 0 AND ep.name = 'MS_Description'
WHERE s.name = @p1
ORDER BY t.name`, Args: []any{r.Schema}}
}

// Views implements Dialect.
func (s SQLServer) Views(r Ref) Query {
	c := s.catalog(r)
	return Query{SQL: `SELECT v.name, CAST(ep.value AS nvarchar(max)), 'view'
FROM ` + c + `views v
JOIN ` + c + `schemas s ON s.schema_id = v.schema_id
LEFT JOIN ` + c + `extended_properties ep ON ep.class = 1 AND ep.major_id = v.object_id AND ep.minor_id = 0 AND ep.name = 'MS_Description'
WHERE s.name = @p1
ORDER BY v.name`, Args: []any{r.Schema}}
}

// Routines implements Dialect: stored procedures and the functions of every kind (scalar, table-valued, CLR). The arguments are written
// from sys.parameters (parameter 0 is the result of a scalar function).
func (s SQLServer) Routines(r Ref) Query {
	c := s.catalog(r)
	return Query{SQL: `SELECT o.name, CASE WHEN o.type IN ('P', 'PC', 'RF', 'X') THEN 'procedure' ELSE 'function' END,
  STUFF((SELECT ', ' + p.name + ' ' + t.name
         FROM ` + c + `parameters p JOIN ` + c + `types t ON t.user_type_id = p.user_type_id
         WHERE p.object_id = o.object_id AND p.parameter_id > 0 ORDER BY p.parameter_id
         FOR XML PATH(''), TYPE).value('.', 'nvarchar(max)'), 1, 2, ''),
  (SELECT t.name FROM ` + c + `parameters p JOIN ` + c + `types t ON t.user_type_id = p.user_type_id
   WHERE p.object_id = o.object_id AND p.parameter_id = 0),
  CAST(o.object_id AS varchar(20))
FROM ` + c + `objects o JOIN ` + c + `schemas s ON s.schema_id = o.schema_id
WHERE s.name = @p1 AND o.type IN ('P', 'PC', 'RF', 'X', 'FN', 'IF', 'TF', 'FS', 'FT', 'AF')
ORDER BY o.name`, Args: []any{r.Schema}}
}

// Columns implements Dialect: the type is written with its length, precision and scale.
func (s SQLServer) Columns(r Ref) Query {
	c := s.catalog(r)
	return Query{SQL: `SELECT c.name,
  t.name + CASE
    WHEN t.name IN ('varchar', 'char', 'varbinary', 'binary') THEN '(' + CASE WHEN c.max_length = -1 THEN 'max' ELSE CAST(c.max_length AS varchar(10)) END + ')'
    WHEN t.name IN ('nvarchar', 'nchar') THEN '(' + CASE WHEN c.max_length = -1 THEN 'max' ELSE CAST(c.max_length / 2 AS varchar(10)) END + ')'
    WHEN t.name IN ('decimal', 'numeric') THEN '(' + CAST(c.precision AS varchar(10)) + ',' + CAST(c.scale AS varchar(10)) + ')'
    WHEN t.name IN ('datetime2', 'datetimeoffset', 'time') THEN '(' + CAST(c.scale AS varchar(10)) + ')'
    ELSE '' END,
  CAST(c.is_nullable AS int), dc.definition,
  CASE WHEN EXISTS (SELECT 1 FROM ` + c + `index_columns ic JOIN ` + c + `indexes i ON i.object_id = ic.object_id AND i.index_id = ic.index_id
                    WHERE i.is_primary_key = 1 AND ic.object_id = c.object_id AND ic.column_id = c.column_id) THEN 1 ELSE 0 END
FROM ` + c + `columns c
JOIN ` + c + `objects o ON o.object_id = c.object_id
JOIN ` + c + `schemas s ON s.schema_id = o.schema_id
JOIN ` + c + `types t ON t.user_type_id = c.user_type_id
LEFT JOIN ` + c + `default_constraints dc ON dc.object_id = c.default_object_id
WHERE s.name = @p1 AND o.name = @p2
ORDER BY c.column_id`, Args: []any{r.Schema, r.Name}}
}

// Indexes implements Dialect: the key columns of the indexes that have a name (a heap has none).
func (s SQLServer) Indexes(r Ref) Query {
	c := s.catalog(r)
	return Query{SQL: `SELECT i.name, col.name, CAST(i.is_unique AS int), CAST(i.is_primary_key AS int), ic.key_ordinal
FROM ` + c + `indexes i
JOIN ` + c + `index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
JOIN ` + c + `columns col ON col.object_id = ic.object_id AND col.column_id = ic.column_id
JOIN ` + c + `objects o ON o.object_id = i.object_id
JOIN ` + c + `schemas s ON s.schema_id = o.schema_id
WHERE s.name = @p1 AND o.name = @p2 AND i.name IS NOT NULL AND ic.key_ordinal > 0
ORDER BY i.name, ic.key_ordinal`, Args: []any{r.Schema, r.Name}}
}

// ForeignKeys implements Dialect.
func (s SQLServer) ForeignKeys(r Ref) Query {
	c := s.catalog(r)
	return Query{SQL: `SELECT fk.name, pc.name, rs.name, ro.name, rc.name, fkc.constraint_column_id
FROM ` + c + `foreign_keys fk
JOIN ` + c + `foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
JOIN ` + c + `objects po ON po.object_id = fk.parent_object_id
JOIN ` + c + `schemas ps ON ps.schema_id = po.schema_id
JOIN ` + c + `columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
JOIN ` + c + `objects ro ON ro.object_id = fk.referenced_object_id
JOIN ` + c + `schemas rs ON rs.schema_id = ro.schema_id
JOIN ` + c + `columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
WHERE ps.name = @p1 AND po.name = @p2
ORDER BY fk.name, fkc.constraint_column_id`, Args: []any{r.Schema, r.Name}}
}

// Definition implements Dialect: the text of sys.sql_modules is the statement that made the view or routine.
func (s SQLServer) Definition(kind Kind, r Ref) Query {
	if kind == KindTable {
		return Query{}
	}
	c := s.catalog(r)
	return Query{SQL: `SELECT m.definition FROM ` + c + `sql_modules m
JOIN ` + c + `objects o ON o.object_id = m.object_id
JOIN ` + c + `schemas s ON s.schema_id = o.schema_id
WHERE s.name = @p1 AND o.name = @p2`, Args: []any{r.Schema, r.Name}}
}

// Wrap implements Dialect: the module's text is the statement.
func (SQLServer) Wrap(_ Kind, _ Ref, definition string) string { return terminate(definition) }

// SelectTop implements Dialect: TOP.
func (s SQLServer) SelectTop(r Ref, n int) string {
	return "SELECT TOP (" + strconv.Itoa(n) + ") * FROM " + s.Qualified(r) + ";"
}

// Cancel implements Dialect: the driver sends an attention to the server when the context ends and the session stays as it was.
func (SQLServer) Cancel() CancelPlan { return CancelPlan{ByContext: true} }

// ReadOnlySession implements Dialect: SQL Server has no statement for it. The connection string says ApplicationIntent=ReadOnly,
// which only tells an availability group where to route the session; the extension's own check refuses the statements that write.
func (SQLServer) ReadOnlySession() []string { return nil }
