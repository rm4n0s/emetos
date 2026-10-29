package dialect

import (
	"strings"
)

// Table is what the generators need to know about a table: its columns, indexes and foreign keys as the catalog listed them.
type Table struct {
	Ref
	// Kind is KindTable or one of the view kinds.
	Kind        Kind
	Columns     []Column
	Indexes     []Index
	ForeignKeys []ForeignKey
}

const indent = "    "

// columnList writes the quoted names, one per line when there are several.
func columnList(d Dialect, names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = d.Quote(n)
	}
	return strings.Join(q, ", ")
}

// primaryKey returns the names of the primary key columns in order.
func (t Table) primaryKey() []string {
	var out []string
	for _, c := range t.Columns {
		if c.PrimaryKey {
			out = append(out, c.Name)
		}
	}
	return out
}

// GenerateSelect writes SELECT with every column of the table.
func GenerateSelect(d Dialect, t Table) string {
	if len(t.Columns) == 0 {
		return "SELECT *\nFROM " + d.Qualified(t.Ref) + ";\n"
	}
	var b strings.Builder
	b.WriteString("SELECT\n")
	for i, c := range t.Columns {
		b.WriteString(indent + d.Quote(c.Name))
		if i < len(t.Columns)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("FROM " + d.Qualified(t.Ref) + ";\n")
	return b.String()
}

// comment is the text of the remark after a value in a generated statement: the name and type of the column.
func comment(c Column) string {
	s := "-- " + c.Name
	if c.Type != "" {
		s += " " + c.Type
	}
	if !c.Nullable {
		s += " not null"
	}
	return s
}

// GenerateInsert writes INSERT with a NULL for each column, each with a remark that names the column and its type.
func GenerateInsert(d Dialect, t Table) string {
	var b strings.Builder
	b.WriteString("INSERT INTO " + d.Qualified(t.Ref))
	if len(t.Columns) == 0 {
		b.WriteString("\nVALUES (); -- no columns were listed\n")
		return b.String()
	}
	names := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		names[i] = c.Name
	}
	b.WriteString(" (" + columnList(d, names) + ")\nVALUES (\n")
	for i, c := range t.Columns {
		value := "NULL"
		if i < len(t.Columns)-1 {
			value += ","
		} else {
			value += " "
		}
		b.WriteString(indent + value + " " + comment(c) + "\n")
	}
	b.WriteString(");\n")
	return b.String()
}

// GenerateUpdate writes UPDATE that sets every column that is not part of the primary key, with a WHERE on the primary key (or
// on all columns' absence, a condition to fill in). The value in the WHERE is NULL, which matches no row, so a statement that is
// run before it is filled in changes nothing.
func GenerateUpdate(d Dialect, t Table) string {
	pk := t.primaryKey()
	var sets []Column
	for _, c := range t.Columns {
		if !c.PrimaryKey {
			sets = append(sets, c)
		}
	}
	if len(sets) == 0 {
		sets = t.Columns
	}
	var b strings.Builder
	b.WriteString("UPDATE " + d.Qualified(t.Ref) + "\nSET\n")
	for i, c := range sets {
		sep := ","
		if i == len(sets)-1 {
			sep = " "
		}
		b.WriteString(indent + d.Quote(c.Name) + " = NULL" + sep + " " + comment(c) + "\n")
	}
	b.WriteString(whereClause(d, pk) + ";\n")
	return b.String()
}

// GenerateDelete writes DELETE with a WHERE on the primary key; see GenerateUpdate for the value.
func GenerateDelete(d Dialect, t Table) string {
	return "DELETE FROM " + d.Qualified(t.Ref) + "\n" + whereClause(d, t.primaryKey()) + ";\n"
}

// whereClause writes WHERE key = NULL AND ..., or a condition to write when the table has no primary key (which is a syntax error
// until it is filled in, and so cannot run by mistake).
func whereClause(d Dialect, pk []string) string {
	if len(pk) == 0 {
		return "WHERE <condition> -- the table has no primary key"
	}
	conds := make([]string, len(pk))
	for i, name := range pk {
		conds[i] = d.Quote(name) + " = NULL"
	}
	return "WHERE " + strings.Join(conds, "\n  AND ")
}

// GenerateCreate writes the CREATE TABLE statement of a table from its columns, then its other indexes. A column is its name, type,
// NOT NULL and DEFAULT as the catalog gave them; the primary key and the foreign keys follow as named constraints.
func GenerateCreate(d Dialect, t Table) string {
	var b strings.Builder
	b.WriteString("CREATE TABLE " + d.Qualified(t.Ref) + " (\n")
	var lines []string
	for _, c := range t.Columns {
		line := indent + d.Quote(c.Name) + " " + c.Type
		if !c.Nullable {
			line += " NOT NULL"
		}
		if c.HasDefault && c.Default != "" {
			line += " DEFAULT " + c.Default
		}
		lines = append(lines, line)
	}
	var pkName string
	for _, ix := range t.Indexes {
		if ix.Primary {
			pkName = ix.Name
		}
	}
	if pk := t.primaryKey(); len(pk) > 0 {
		line := indent
		if pkName != "" && pkName != "PRIMARY" {
			line += "CONSTRAINT " + d.Quote(pkName) + " "
		}
		lines = append(lines, line+"PRIMARY KEY ("+columnList(d, pk)+")")
	}
	for _, fk := range t.ForeignKeys {
		target := Ref{Database: t.Database, Schema: fk.Schema, Name: fk.Table} // a table of the same database
		if d.Levels().Databases && !d.Levels().Schemas {                       // MySQL: the referenced "schema" is a database
			target = Ref{Database: fk.Schema, Name: fk.Table}
		}
		lines = append(lines, indent+"CONSTRAINT "+d.Quote(fk.Name)+" FOREIGN KEY ("+columnList(d, fk.Columns)+") REFERENCES "+
			d.Qualified(target)+" ("+columnList(d, fk.RefColumns)+")")
	}
	b.WriteString(strings.Join(lines, ",\n") + "\n);\n")
	for _, ix := range t.Indexes {
		if ix.Primary {
			continue
		}
		unique := ""
		if ix.Unique {
			unique = "UNIQUE "
		}
		b.WriteString("\nCREATE " + unique + "INDEX " + d.Quote(ix.Name) + " ON " + d.Qualified(t.Ref) + " (" + columnList(d, ix.Columns) + ");\n")
	}
	return b.String()
}

// GenerateDrop writes the DROP statement of an object. It is only ever shown to the user, never run for them.
func GenerateDrop(d Dialect, kind Kind, r Ref) string {
	name := d.Qualified(r)
	switch kind {
	case KindView:
		return "DROP VIEW " + name + ";\n"
	case KindMaterializedView:
		return "DROP MATERIALIZED VIEW " + name + ";\n"
	case KindFunction, KindProcedure:
		word := "FUNCTION"
		if kind == KindProcedure {
			word = "PROCEDURE"
		}
		if d.ID() == "postgres" { // PostgreSQL names a routine with its argument types
			return "DROP " + word + " " + name + "(" + r.Args + ");\n"
		}
		return "DROP " + word + " " + name + ";\n"
	}
	return "DROP TABLE " + name + ";\n"
}
