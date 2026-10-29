package sql

import (
	"context"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// The kinds of node (dbconn.Node.Kind) the explorer makes.
const (
	// NodeDatabase, NodeSchema and NodeFolder are the levels above the objects: a database, a schema, and a group of objects of a
	// kind (Tables, Views, Functions, Procedures, Columns, Indexes, Foreign Keys).
	NodeDatabase = "database"
	NodeSchema   = "schema"
	NodeFolder   = "folder"
	// NodeTable, NodeView, NodeRoutine are objects; NodeColumn, NodeIndex and NodeForeignKey are what is inside a table.
	NodeTable      = "table"
	NodeView       = "view"
	NodeRoutine    = "routine"
	NodeColumn     = "column"
	NodeIndex      = "index"
	NodeForeignKey = "foreignKey"
	// NodeMore is the last row of a listing that was cut.
	NodeMore = "more"
)

// The folders of the explorer (nodeData.Folder).
const (
	folderTables      = "tables"
	folderViews       = "views"
	folderFunctions   = "functions"
	folderProcedures  = "procedures"
	folderColumns     = "columns"
	folderIndexes     = "indexes"
	folderForeignKeys = "foreignKeys"
)

// nodeData is the Data of a node: where it is in the catalog.
type nodeData struct {
	// Ref is the object, or the database or schema above it.
	Ref dialect.Ref
	// Folder is, for a folder, which one.
	Folder string
	// Kind is, for an object, what it is.
	Kind dialect.Kind
}

// showSystem tells whether the connection lists the databases and schemas the engine keeps for itself.
func showSystem(c dbconn.Connection) bool { return c.Options[OptionShowSystem] == "true" }

// Children lists the nodes below a connection or a node: databases or schemas, then the folders of a schema, then the objects of a
// folder, then the columns, indexes and foreign keys of a table. It reads the catalog of the server, so it blocks. The errors are
// those of Service.Rows and SQLNodeUnknown.
func (s *Service) Children(ctx context.Context, c dbconn.Connection, parent *dbconn.Node) ([]dbconn.Node, error) {
	d, err := s.Dialect(c.ID)
	if err != nil {
		return nil, err
	}
	lv := d.Levels()
	system := showSystem(c)
	if parent == nil {
		switch {
		case lv.Databases:
			return s.databases(ctx, c, d, system)
		case lv.Schemas:
			return s.schemas(ctx, c, d, dialect.Ref{}, system)
		}
		return nil, nil
	}
	switch parent.Kind {
	case NodeColumn, NodeIndex, NodeForeignKey, NodeMore, NodeRoutine:
		return nil, nil // nothing is below them
	}
	data, ok := parent.Data.(nodeData)
	if !ok {
		return nil, errors.New("SQLNodeUnknown", "the node was not made by the SQL explorer", "node", parent.ID)
	}
	switch parent.Kind {
	case NodeDatabase:
		if lv.Schemas {
			return s.schemas(ctx, c, d, data.Ref, system)
		}
		return folders(d, data.Ref), nil
	case NodeSchema:
		return folders(d, data.Ref), nil
	case NodeFolder:
		return s.folder(ctx, c, d, data)
	case NodeTable, NodeView:
		return tableFolders(data), nil
	}
	return nil, nil
}

// databases lists the databases of the server.
func (s *Service) databases(ctx context.Context, c dbconn.Connection, d dialect.Dialect, system bool) ([]dbconn.Node, error) {
	rows, truncated, err := s.Rows(ctx, c.ID, "", d.Databases(system), 0)
	if err != nil {
		return nil, err
	}
	nodes := make([]dbconn.Node, 0, len(rows)+1)
	for _, row := range rows {
		name := cell(row, 0)
		nodes = append(nodes, dbconn.Node{
			ID: "db:" + name, Label: name, Icon: "database", Expandable: true, Kind: NodeDatabase,
			Data: nodeData{Ref: dialect.Ref{Database: name}},
		})
	}
	return withMore(nodes, truncated), nil
}

// schemas lists the schemas of a database.
func (s *Service) schemas(ctx context.Context, c dbconn.Connection, d dialect.Dialect, db dialect.Ref, system bool) ([]dbconn.Node, error) {
	rows, truncated, err := s.Rows(ctx, c.ID, db.Database, d.Schemas(db.Database, system), 0)
	if err != nil {
		return nil, err
	}
	nodes := make([]dbconn.Node, 0, len(rows)+1)
	for _, row := range rows {
		name := cell(row, 0)
		ref := db
		ref.Schema = name
		nodes = append(nodes, dbconn.Node{
			ID: "schema:" + name, Label: name, Icon: "symbol-namespace", Expandable: true, Kind: NodeSchema, Data: nodeData{Ref: ref},
		})
	}
	return withMore(nodes, truncated), nil
}

// folders are the groups of objects in a schema (a database, for MySQL): the engine's routines only when it has any.
func folders(d dialect.Dialect, in dialect.Ref) []dbconn.Node {
	names := []struct{ folder, label, icon string }{
		{folderTables, "Tables", "table"}, {folderViews, "Views", "eye"},
	}
	if d.Levels().Routines {
		names = append(names, struct{ folder, label, icon string }{folderFunctions, "Functions", "symbol-method-arrow"},
			struct{ folder, label, icon string }{folderProcedures, "Procedures", "symbol-event"})
	}
	nodes := make([]dbconn.Node, len(names))
	for i, n := range names {
		nodes[i] = dbconn.Node{ID: "folder:" + n.folder, Label: n.label, Icon: n.icon, Expandable: true, Kind: NodeFolder, Data: nodeData{Ref: in, Folder: n.folder}}
	}
	return nodes
}

// tableFolders are the groups inside a table or view: columns, and for a table its indexes and foreign keys (a materialized view has
// indexes too).
func tableFolders(t nodeData) []dbconn.Node {
	mk := func(folder, label, icon string) dbconn.Node {
		return dbconn.Node{ID: "folder:" + folder, Label: label, Icon: icon, Expandable: true, Kind: NodeFolder, Data: nodeData{Ref: t.Ref, Folder: folder, Kind: t.Kind}}
	}
	nodes := []dbconn.Node{mk(folderColumns, "Columns", "symbol-field")}
	if t.Kind == dialect.KindTable || t.Kind == dialect.KindMaterializedView {
		nodes = append(nodes, mk(folderIndexes, "Indexes", "list-flat"))
	}
	if t.Kind == dialect.KindTable {
		nodes = append(nodes, mk(folderForeignKeys, "Foreign Keys", "references"))
	}
	return nodes
}

// folder lists the objects of a folder.
func (s *Service) folder(ctx context.Context, c dbconn.Connection, d dialect.Dialect, f nodeData) ([]dbconn.Node, error) {
	switch f.Folder {
	case folderTables:
		rows, truncated, err := s.Rows(ctx, c.ID, f.Ref.Database, d.Tables(f.Ref, showSystem(c)), 0)
		if err != nil {
			return nil, err
		}
		return withMore(objectNodes(dialect.DecodeTables(rows, f.Ref), NodeTable), truncated), nil
	case folderViews:
		rows, truncated, err := s.Rows(ctx, c.ID, f.Ref.Database, d.Views(f.Ref), 0)
		if err != nil {
			return nil, err
		}
		return withMore(objectNodes(dialect.DecodeViews(rows, f.Ref), NodeView), truncated), nil
	case folderFunctions, folderProcedures:
		rows, truncated, err := s.Rows(ctx, c.ID, f.Ref.Database, d.Routines(f.Ref), 0)
		if err != nil {
			return nil, err
		}
		want := dialect.KindFunction
		if f.Folder == folderProcedures {
			want = dialect.KindProcedure
		}
		var objs []dialect.Object
		for _, o := range dialect.DecodeRoutines(rows, f.Ref) {
			if o.Kind == want {
				objs = append(objs, o)
			}
		}
		return withMore(objectNodes(objs, NodeRoutine), truncated), nil
	case folderColumns:
		cols, err := s.TableColumns(ctx, c.ID, d, f.Ref)
		if err != nil {
			return nil, err
		}
		return columnNodes(cols), nil
	case folderIndexes:
		rows, _, err := s.Rows(ctx, c.ID, f.Ref.Database, d.Indexes(f.Ref), 0)
		if err != nil {
			return nil, err
		}
		return indexNodes(dialect.DecodeIndexes(rows)), nil
	case folderForeignKeys:
		rows, _, err := s.Rows(ctx, c.ID, f.Ref.Database, d.ForeignKeys(f.Ref), 0)
		if err != nil {
			return nil, err
		}
		return foreignKeyNodes(d, dialect.DecodeForeignKeys(rows)), nil
	}
	return nil, errors.New("SQLNodeUnknown", "the folder is not one the SQL explorer makes", "folder", f.Folder)
}

// TableColumns reads the columns of a table or view, with the ones a foreign key uses marked.
func (s *Service) TableColumns(ctx context.Context, connID string, d dialect.Dialect, r dialect.Ref) ([]dialect.Column, error) {
	rows, _, err := s.Rows(ctx, connID, r.Database, d.Columns(r), 0)
	if err != nil {
		return nil, err
	}
	cols := dialect.DecodeColumns(rows)
	fkRows, _, err := s.Rows(ctx, connID, r.Database, d.ForeignKeys(r), 0)
	if err != nil {
		return nil, err
	}
	dialect.MarkForeignKeys(cols, dialect.DecodeForeignKeys(fkRows))
	return cols, nil
}

// LoadTable reads everything the generators need to know about a table or view.
func (s *Service) LoadTable(ctx context.Context, connID string, kind dialect.Kind, r dialect.Ref) (dialect.Table, error) {
	d, err := s.Dialect(connID)
	if err != nil {
		return dialect.Table{}, err
	}
	t := dialect.Table{Ref: r, Kind: kind}
	if t.Columns, err = s.TableColumns(ctx, connID, d, r); err != nil {
		return dialect.Table{}, err
	}
	rows, _, err := s.Rows(ctx, connID, r.Database, d.Indexes(r), 0)
	if err != nil {
		return dialect.Table{}, err
	}
	t.Indexes = dialect.DecodeIndexes(rows)
	rows, _, err = s.Rows(ctx, connID, r.Database, d.ForeignKeys(r), 0)
	if err != nil {
		return dialect.Table{}, err
	}
	t.ForeignKeys = dialect.DecodeForeignKeys(rows)
	return t, nil
}

// Create writes the statement that creates an object: the catalog's own text when the engine keeps one, the columns, keys and indexes
// written out for a table where it does not. The errors are those of Service.Rows.
func (s *Service) Create(ctx context.Context, connID string, kind dialect.Kind, r dialect.Ref) (string, error) {
	d, err := s.Dialect(connID)
	if err != nil {
		return "", err
	}
	if q := d.Definition(kind, r); !q.Empty() {
		text, err := s.Text(ctx, connID, r.Database, q)
		if err != nil {
			return "", err
		}
		if text == "" {
			return "-- the catalog has no definition of " + d.Qualified(r) + "\n", nil
		}
		return d.Wrap(kind, r, text), nil
	}
	t, err := s.LoadTable(ctx, connID, kind, r)
	if err != nil {
		return "", err
	}
	return dialect.GenerateCreate(d, t), nil
}

// ---- nodes of objects ----

// withMore adds the row that says a listing was cut.
func withMore(nodes []dbconn.Node, truncated bool) []dbconn.Node {
	if !truncated {
		return nodes
	}
	return append(nodes, dbconn.Node{
		ID: "more", Label: "... " + strconv.Itoa(maxListed) + " shown, there are more", Icon: "info", Kind: NodeMore,
		Tooltip: "The explorer lists the first " + strconv.Itoa(maxListed) + " objects. Use a query to see the rest.",
	})
}

// cell is the text of a column of a row.
func cell(row dialect.Row, i int) string {
	if i < len(row) && row[i].Valid {
		return row[i].String
	}
	return ""
}

// objectNodes makes the rows of tables, views or routines.
func objectNodes(objs []dialect.Object, nodeKind string) []dbconn.Node {
	nodes := make([]dbconn.Node, 0, len(objs))
	for _, o := range objs {
		ref := o.Ref
		n := dbconn.Node{ID: nodeKind + ":" + o.Name, Label: o.Name, Tooltip: o.Comment, Kind: nodeKind, Data: nodeData{Ref: ref, Kind: o.Kind}}
		switch o.Kind {
		case dialect.KindTable:
			n.Icon, n.Expandable = "table", true
		case dialect.KindView:
			n.Icon, n.Expandable = "eye", true
		case dialect.KindMaterializedView:
			n.Icon, n.Expandable, n.Description = "eye", true, "materialized"
		case dialect.KindFunction, dialect.KindProcedure:
			n.ID += "(" + o.Args + ")#" + o.ID
			n.Icon = "symbol-method-arrow"
			if o.Kind == dialect.KindProcedure {
				n.Icon = "symbol-event"
			}
			n.Description = "(" + o.Args + ")"
			if o.Result != "" {
				n.Description += " -> " + o.Result
			}
			n.Tooltip = strings.TrimSpace(o.Name + n.Description + "\n" + o.Comment)
		}
		nodes = append(nodes, n)
	}
	return nodes
}

// columnNodes makes the rows of the columns of a table: a key icon for a primary key, a link for a foreign key.
func columnNodes(cols []dialect.Column) []dbconn.Node {
	nodes := make([]dbconn.Node, len(cols))
	for i, c := range cols {
		desc := c.Type
		if !c.Nullable {
			desc += " NOT NULL"
		}
		icon := "symbol-field"
		switch {
		case c.PrimaryKey:
			icon = "key"
		case c.ForeignKey:
			icon = "references"
		}
		tip := c.Name + " " + c.Type
		if c.PrimaryKey {
			tip += "\nprimary key"
		}
		if c.ForeignKey {
			tip += "\nforeign key"
		}
		if c.HasDefault && c.Default != "" {
			tip += "\ndefault " + c.Default
		}
		nodes[i] = dbconn.Node{ID: "col:" + c.Name, Label: c.Name, Description: desc, Icon: icon, Tooltip: tip, Kind: NodeColumn}
	}
	return nodes
}

// indexNodes makes the rows of the indexes of a table.
func indexNodes(ixs []dialect.Index) []dbconn.Node {
	nodes := make([]dbconn.Node, len(ixs))
	for i, ix := range ixs {
		desc := "(" + strings.Join(ix.Columns, ", ") + ")"
		switch {
		case ix.Primary:
			desc += " primary key"
		case ix.Unique:
			desc += " unique"
		}
		nodes[i] = dbconn.Node{ID: "index:" + ix.Name, Label: ix.Name, Description: desc, Icon: "list-flat", Kind: NodeIndex}
	}
	return nodes
}

// foreignKeyNodes makes the rows of the foreign keys of a table.
func foreignKeyNodes(d dialect.Dialect, fks []dialect.ForeignKey) []dbconn.Node {
	nodes := make([]dbconn.Node, len(fks))
	for i, fk := range fks {
		target := fk.Table
		if fk.Schema != "" && (d.ID() != "sqlite" || fk.Schema != "main") {
			target = fk.Schema + "." + fk.Table
		}
		desc := "(" + strings.Join(fk.Columns, ", ") + ") -> " + target + " (" + strings.Join(fk.RefColumns, ", ") + ")"
		nodes[i] = dbconn.Node{ID: "fk:" + fk.Name, Label: fk.Name, Description: desc, Icon: "references", Kind: NodeForeignKey}
	}
	return nodes
}
