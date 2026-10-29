package sql

import (
	"context"
	"regexp"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// completer is the completion provider of SQL editors (SPEC §9.12 "Providers in Go", §10.21): it offers the keywords of the engine of
// the connection the editor is bound to, and the schemas, tables, views and columns of the connection's catalog, read the first time
// they are asked for and kept for a few minutes (Catalog). An editor with no connection, or one that is not open, gets the keywords of
// no engine in particular and nothing else.
type completer struct{ q *queries }

var _ extension.CompletionProvider = (*completer)(nil)

// maxSuggestions bounds the items a request gives; the suggest widget filters the rest by what is typed.
const maxSuggestions = 2000

// ProvideCompletion implements extension.CompletionProvider.
func (c *completer) ProvideCompletion(ctx context.Context, doc extension.TextDocument, pos extension.Position, _ extension.CompletionTrigger) (extension.CompletionList, error) {
	b := c.q.bindingOf(doc.URI)
	conn, ok := c.q.connectionOf(b.Connection)
	if !ok {
		return extension.CompletionList{Items: keywordItems(dialect.Postgres{})}, nil
	}
	d, ok := dialect.ByID(conn.Engine)
	if !ok {
		return extension.CompletionList{}, nil
	}
	off := offsetOf(doc.Text, pos.Line, pos.Col)
	stmt := statementAround(d, doc.Text, off)
	ctxInfo := analyze(stmt.text, off-stmt.start)
	var items []extension.CompletionItem
	cat, err := c.q.hub.svc.Catalog(conn.ID)
	if err != nil {
		// not connected: the keywords still help
		if ctxInfo.qualifier == "" {
			items = keywordItems(d)
		}
		return extension.CompletionList{Items: items}, nil
	}
	items = c.catalogItems(ctx, cat, d, b.Database, ctxInfo, stmt.text)
	if ctxInfo.qualifier == "" {
		items = append(items, keywordItems(d)...)
	}
	if len(items) > maxSuggestions {
		items = items[:maxSuggestions]
	}
	return extension.CompletionList{Items: items}, nil
}

// stmtText is the statement the cursor is in, and where it starts in the document.
type stmtText struct {
	text  string
	start int
}

// statementAround finds the statement at an offset; the whole text when it has none.
func statementAround(d dialect.Dialect, text string, off int) stmtText {
	off = min(max(off, 0), len(text))
	stmts := dialect.Split(d, text)
	st, ok := dialect.At(text, stmts, off)
	if !ok || off < st.Start {
		return stmtText{text: "", start: off}
	}
	if off <= st.End {
		return stmtText{text: st.Text, start: st.Start}
	}
	// the cursor is after the statement: still in it, with what was typed since, unless a semicolon ended it
	if strings.Contains(text[st.End:off], ";") {
		return stmtText{text: "", start: off}
	}
	return stmtText{text: text[st.Start:off], start: st.Start}
}

// cursorContext is what the text before the cursor says about what is wanted.
type cursorContext struct {
	// prefix is the word being typed, and qualifier the name before the dot that precedes it ("o" in "o.na"); empty for none.
	prefix, qualifier string
	// afterFrom says that the cursor follows FROM, JOIN, INTO or UPDATE: a table is wanted.
	afterFrom bool
}

// wordBefore returns the identifier that ends at i (quoted or not) and where it starts.
func wordBefore(s string, i int) (string, int) {
	j := i
	for j > 0 && isNameByteNoDot(s[j-1]) {
		j--
	}
	return s[j:i], j
}

// isNameByteNoDot tells whether a byte is part of a name.
func isNameByteNoDot(b byte) bool { return isNameByte(b) && b != '.' }

// qualifierBefore returns the name that ends at i: a word, or a name in quotes, without the quotes.
func qualifierBefore(s string, i int) string {
	if i > 0 {
		var open byte
		switch s[i-1] {
		case '"':
			open = '"'
		case '`':
			open = '`'
		case ']':
			open = '['
		}
		if open != 0 {
			if j := strings.LastIndexByte(s[:i-1], open); j >= 0 {
				return s[j+1 : i-1]
			}
		}
	}
	word, _ := wordBefore(s, i)
	return word
}

// keywordsAfterTable are the words after which a table is expected.
var keywordsAfterTable = map[string]bool{"FROM": true, "JOIN": true, "INTO": true, "UPDATE": true, "TABLE": true}

// analyze reads the statement up to the cursor.
func analyze(stmt string, rel int) cursorContext {
	rel = min(max(rel, 0), len(stmt))
	var c cursorContext
	c.prefix, rel = wordBefore(stmt, rel)
	if rel > 0 && stmt[rel-1] == '.' {
		c.qualifier = qualifierBefore(stmt, rel-1)
		return c
	}
	trimmed := strings.TrimRight(stmt[:rel], " \t\r\n")
	before, _ := wordBefore(trimmed, len(trimmed))
	c.afterFrom = keywordsAfterTable[strings.ToUpper(before)]
	return c
}

// referenceRE finds the tables a statement reads from or writes to, with the alias each has.
var referenceRE = regexp.MustCompile("(?i)\\b(?:from|join|update|into)\\s+((?:[\\w$]+|\"[^\"]+\"|`[^`]+`|\\[[^\\]]+\\])(?:\\s*\\.\\s*(?:[\\w$]+|\"[^\"]+\"|`[^`]+`|\\[[^\\]]+\\]))*)(?:\\s+(?:as\\s+)?([A-Za-z_][\\w$]*))?")

// reference is a table a statement mentions.
type reference struct {
	// schema and name are the parts of the table's name; alias is the name the statement gives it.
	schema, name, alias string
}

// reservedAfterTable are the words that follow a table name and are not its alias.
var reservedAfterTable = map[string]bool{
	"WHERE": true, "SET": true, "ON": true, "USING": true, "JOIN": true, "INNER": true, "LEFT": true, "RIGHT": true, "FULL": true, "CROSS": true,
	"NATURAL": true, "GROUP": true, "ORDER": true, "LIMIT": true, "HAVING": true, "UNION": true, "VALUES": true, "SELECT": true, "OFFSET": true,
	"FETCH": true, "FOR": true, "RETURNING": true, "OUTPUT": true, "WITH": true, "AS": true,
}

// references lists the tables a statement mentions.
func references(stmt string) []reference {
	var out []reference
	for _, m := range referenceRE.FindAllStringSubmatch(stmt, -1) {
		parts := strings.Split(m[1], ".")
		for i := range parts {
			parts[i] = strings.Trim(strings.TrimSpace(parts[i]), "\"`[]")
		}
		r := reference{name: parts[len(parts)-1]}
		if len(parts) > 1 {
			r.schema = parts[len(parts)-2]
		}
		if m[2] != "" && !reservedAfterTable[strings.ToUpper(m[2])] {
			r.alias = m[2]
		}
		out = append(out, r)
	}
	return out
}

// defaultSchema is the schema a table without one is looked for in.
func defaultSchema(d dialect.Dialect) string {
	switch d.ID() {
	case "postgres":
		return "public"
	case "sqlserver":
		return "dbo"
	case "sqlite":
		return "main"
	}
	return ""
}

// catalogItems are the schemas, tables, views and columns that fit the cursor.
func (c *completer) catalogItems(ctx context.Context, cat *Catalog, d dialect.Dialect, database string, cc cursorContext, stmt string) []extension.CompletionItem {
	var items []extension.CompletionItem
	schemas, err := cat.Schemas(ctx, database)
	if err != nil {
		c.q.api.Log().Debug("the schemas cannot be read for completion", "err", err)
	}
	def := defaultSchema(d)
	isSchema := func(name string) bool {
		for _, s := range schemas {
			if strings.EqualFold(s, name) {
				return true
			}
		}
		return false
	}
	objects := func(schema string) []extension.CompletionItem {
		objs, err := cat.Objects(ctx, database, schema)
		if err != nil {
			c.q.api.Log().Debug("the tables cannot be read for completion", "err", err)
			return nil
		}
		out := make([]extension.CompletionItem, 0, len(objs))
		for _, o := range objs {
			kind, detail := extension.CompletionStruct, "table"
			if o.Kind != dialect.KindTable {
				kind, detail = extension.CompletionInterface, "view"
			}
			if schema != "" {
				detail += " in " + schema
			}
			out = append(out, extension.CompletionItem{Label: o.Name, Kind: kind, Detail: detail, SortText: "1" + o.Name})
		}
		return out
	}
	columns := func(ref dialect.Ref, qualifierLabel string) []extension.CompletionItem {
		cols, err := cat.Columns(ctx, ref)
		if err != nil {
			c.q.api.Log().Debug("the columns cannot be read for completion", "err", err)
			return nil
		}
		out := make([]extension.CompletionItem, 0, len(cols))
		for _, col := range cols {
			detail := col.Type
			if col.PrimaryKey {
				detail += " · primary key"
			}
			if qualifierLabel != "" {
				detail += " · " + qualifierLabel
			}
			kind := extension.CompletionField
			if col.PrimaryKey {
				kind = extension.CompletionConstant
			}
			out = append(out, extension.CompletionItem{Label: col.Name, Kind: kind, Detail: detail, SortText: "0" + col.Name})
		}
		return out
	}
	refFor := func(schema, name string) dialect.Ref {
		ref := dialect.Ref{Database: database, Name: name}
		if d.Levels().Schemas {
			ref.Schema = schema
			if ref.Schema == "" {
				ref.Schema = def
			}
		}
		return ref
	}
	refs := references(stmt)
	switch {
	case cc.qualifier != "":
		// "schema." gives its tables; "alias." or "table." gives columns
		for _, r := range refs {
			if strings.EqualFold(r.alias, cc.qualifier) || (r.alias == "" && strings.EqualFold(r.name, cc.qualifier)) || strings.EqualFold(r.name, cc.qualifier) {
				items = append(items, columns(refFor(r.schema, r.name), r.name)...)
			}
		}
		if len(items) == 0 && isSchema(cc.qualifier) {
			items = objects(cc.qualifier)
		}
		if len(items) == 0 && !d.Levels().Schemas && strings.EqualFold(cc.qualifier, database) {
			items = objects("")
		}
		if len(items) == 0 && !isSchema(cc.qualifier) {
			// a table written without being in the statement yet
			items = columns(refFor("", cc.qualifier), cc.qualifier)
		}
	case cc.afterFrom:
		for _, s := range schemas {
			items = append(items, extension.CompletionItem{Label: s, Kind: extension.CompletionModule, Detail: "schema", SortText: "2" + s})
		}
		items = append(items, objects(def)...)
	default:
		for _, r := range refs {
			items = append(items, columns(refFor(r.schema, r.name), r.name)...)
		}
		items = append(items, objects(def)...)
		for _, s := range schemas {
			items = append(items, extension.CompletionItem{Label: s, Kind: extension.CompletionModule, Detail: "schema", SortText: "2" + s})
		}
	}
	return items
}

// keywordItems are the keywords of the engine as completion items; they come last.
func keywordItems(d dialect.Dialect) []extension.CompletionItem {
	kw := d.Keywords()
	items := make([]extension.CompletionItem, len(kw))
	for i, k := range kw {
		items[i] = extension.CompletionItem{Label: k, Kind: extension.CompletionKeyword, SortText: "9" + k}
	}
	return items
}
