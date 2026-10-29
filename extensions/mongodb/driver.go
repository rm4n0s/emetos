package mongodb

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/mongodb/playground"
)

// The kinds of the nodes of the explorer (dbconn.Node.Kind).
const (
	kindDatabase   = "database"
	kindCollection = "collection"
	kindIndexes    = "indexes"
	kindSchema     = "schema"
	kindIndex      = "index"
	kindField      = "field"
	kindNote       = "note"
)

// nodeData is what a node of the explorer remembers (dbconn.Node.Data): where it is, and what it needs to list its children and to
// act on it.
type nodeData struct {
	Database, Collection string
	CollKind             CollectionKind
	Index                *IndexInfo
	Field                *SchemaField
	// Of is the number of documents the percentage of a field is of.
	Of int
}

// driver is the dbconn.Driver of the engine "mongodb" in one window tab.
type driver struct{ e *Extension }

// Engines implements dbconn.Driver.
func (*driver) Engines() []dbconn.Engine { return []dbconn.Engine{engine()} }

// Connect implements dbconn.Driver.
func (d *driver) Connect(ctx context.Context, c dbconn.Connection, password string) error {
	if err := d.e.mgr.open(ctx, c, password); err != nil {
		return err
	}
	d.e.publish(ConnectedTopic, map[string]any{"connection": c.ID})
	return nil
}

// Disconnect implements dbconn.Driver.
func (d *driver) Disconnect(c dbconn.Connection) error {
	d.e.mgr.close(c)
	d.e.publish(DisconnectedTopic, map[string]any{"connection": c.ID})
	return nil
}

// Children implements dbconn.Driver: the databases below a connection, the collections below a database, the Indexes and the Schema
// below a collection, and what is below those.
func (d *driver) Children(ctx context.Context, c dbconn.Connection, parent *dbconn.Node) ([]dbconn.Node, error) {
	b, ok := d.e.mgr.backend(c.ID)
	if !ok {
		return nil, errors.New("MongoNotConnected", "the connection is not open", "connection", c.Name, "id", c.ID)
	}
	if parent == nil {
		return databaseNodes(ctx, b)
	}
	data, _ := parent.Data.(nodeData)
	switch parent.Kind {
	case kindDatabase:
		return collectionNodes(ctx, b, data.Database)
	case kindCollection:
		return collectionGroups(data), nil
	case kindIndexes:
		return indexNodes(ctx, b, data)
	case kindSchema:
		return schemaNodes(ctx, b, data, d.e.sampleSize())
	case kindField:
		nodes := fieldNodes(data.Field.Children, data.Field.Count, data.Database, data.Collection)
		if data.Field.MoreFields {
			nodes = append(nodes, note("more-fields", fmt.Sprintf("More than %d fields: the others are not shown", schemaMaxFields)))
		}
		return nodes, nil
	}
	return nil, nil
}

// systemDatabases are the databases the server keeps for itself; they are listed after the others.
var systemDatabases = map[string]bool{"admin": true, "config": true, "local": true}

// databaseNodes lists the databases of the server, the user's before the server's own.
func databaseNodes(ctx context.Context, b Backend) ([]dbconn.Node, error) {
	dbs, err := b.Databases(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(dbs, func(i, j int) bool {
		si, sj := systemDatabases[dbs[i].Name], systemDatabases[dbs[j].Name]
		if si != sj {
			return sj
		}
		return strings.ToLower(dbs[i].Name) < strings.ToLower(dbs[j].Name)
	})
	nodes := make([]dbconn.Node, len(dbs))
	for i, db := range dbs {
		desc := FormatBytes(db.SizeOnDisk)
		if db.Empty {
			desc = "empty"
		}
		nodes[i] = dbconn.Node{
			ID: db.Name, Label: db.Name, Description: desc, Icon: "database", Expandable: true, Kind: kindDatabase,
			Tooltip: db.Name + ": " + desc + " on disk",
			Data:    nodeData{Database: db.Name},
		}
	}
	return nodes, nil
}

// countWorkers is how many collections have their documents counted at once.
const countWorkers = 8

// countLimit is the number of collections after which no document count is asked for, so that a database with thousands of
// collections lists at once.
const countLimit = 500

// collectionNodes lists the collections of a database with their estimated document counts. The counts come from the server's
// metadata; a count that fails is left out, not an error of the listing.
func collectionNodes(ctx context.Context, b Backend, database string) ([]dbconn.Node, error) {
	colls, err := b.Collections(ctx, database)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(colls, func(i, j int) bool {
		si, sj := strings.HasPrefix(colls[i].Name, "system."), strings.HasPrefix(colls[j].Name, "system.")
		if si != sj {
			return sj
		}
		return strings.ToLower(colls[i].Name) < strings.ToLower(colls[j].Name)
	})
	counts := make([]int64, len(colls))
	known := make([]bool, len(colls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, countWorkers)
	for i, c := range colls {
		if c.Kind == KindView || i >= countLimit {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if n, err := b.EstimatedCount(ctx, database, c.Name); err == nil {
				counts[i], known[i] = n, true
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("MongoListingCancelled", err).SetMetadata("database", database)
	}
	nodes := make([]dbconn.Node, len(colls))
	for i, c := range colls {
		n := dbconn.Node{
			ID: c.Name, Label: c.Name, Expandable: true, Kind: kindCollection,
			Data: nodeData{Database: database, Collection: c.Name, CollKind: c.Kind},
		}
		switch c.Kind {
		case KindView:
			n.Icon, n.Description = "eye", "view on "+c.ViewOn
			n.Tooltip = c.Name + ": a view on " + c.ViewOn
		case KindTimeSeries:
			n.Icon, n.Description = "graph", "time series"
			n.Tooltip = c.Name + ": a time series collection"
		default:
			n.Icon = "table"
		}
		if known[i] {
			if n.Description != "" {
				n.Description += ", "
			}
			n.Description += FormatCount(counts[i])
			n.Tooltip = c.Name + ": about " + FormatCount(counts[i]) + " (estimated from the metadata of the collection)"
		}
		nodes[i] = n
	}
	return nodes, nil
}

// collectionGroups are the two groups below a collection: its indexes and its schema. A view has no indexes.
func collectionGroups(data nodeData) []dbconn.Node {
	var nodes []dbconn.Node
	if data.CollKind != KindView {
		nodes = append(nodes, dbconn.Node{
			ID: "indexes", Label: "Indexes", Icon: "list-ordered", Expandable: true, Kind: kindIndexes, Data: data,
			Tooltip: "The indexes of " + data.Collection,
		})
	}
	nodes = append(nodes, dbconn.Node{
		ID: "schema", Label: "Schema", Icon: "type-hierarchy", Expandable: true, Kind: kindSchema, Data: data,
		Tooltip: "The fields of " + data.Collection + " and their types, from a sample of its documents",
	})
	return nodes
}

// indexNodes lists the indexes of a collection.
func indexNodes(ctx context.Context, b Backend, data nodeData) ([]dbconn.Node, error) {
	idx, err := b.Indexes(ctx, data.Database, data.Collection)
	if err != nil {
		return nil, err
	}
	nodes := make([]dbconn.Node, 0, len(idx))
	for i := range idx {
		ix := idx[i]
		var flags []string
		if ix.Unique {
			flags = append(flags, "unique")
		}
		if ix.Sparse {
			flags = append(flags, "sparse")
		}
		if ix.TTLSeconds != nil {
			flags = append(flags, fmt.Sprintf("TTL %d s", *ix.TTLSeconds))
		}
		desc := playground.Format(ix.Keys)
		if len(flags) > 0 {
			desc += "  " + strings.Join(flags, ", ")
		}
		d := data
		d.Index = &ix
		nodes = append(nodes, dbconn.Node{
			ID: ix.Name, Label: ix.Name, Description: desc, Icon: "key", Kind: kindIndex, Data: d,
			Tooltip: ix.Name + " on " + playground.Format(ix.Keys),
		})
	}
	if len(nodes) == 0 {
		nodes = append(nodes, note("no-indexes", "No indexes"))
	}
	return nodes, nil
}

// note is a row that says something, not a thing of the server.
func note(id, text string) dbconn.Node {
	return dbconn.Node{ID: id, Label: text, Icon: "info", Kind: kindNote}
}

// schemaNodes samples the collection and lists the fields found.
func schemaNodes(ctx context.Context, b Backend, data nodeData, sample int) ([]dbconn.Node, error) {
	if sample <= 0 {
		sample = defaultSampleSize
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	docs, err := b.Sample(ctx, data.Database, data.Collection, sample)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return []dbconn.Node{note("no-documents", "No documents to sample")}, nil
	}
	s := InferSchema(docs)
	nodes := fieldNodes(s.Fields, s.Documents, data.Database, data.Collection)
	if s.MoreFields {
		nodes = append(nodes, note("more-fields", fmt.Sprintf("More than %d fields: the others are not shown", schemaMaxFields)))
	}
	return nodes, nil
}

// fieldNodes makes the nodes of fields; the percentage of each is of parentCount documents.
func fieldNodes(fields []*SchemaField, parentCount int, database, collection string) []dbconn.Node {
	nodes := make([]dbconn.Node, 0, len(fields))
	for _, f := range fields {
		types := f.TypesText()
		desc := types
		if p := f.Percent(parentCount); p < 100 {
			desc += fmt.Sprintf("  %d%%", p)
		}
		n := dbconn.Node{
			ID: f.Name, Label: f.Name, Description: desc, Icon: typeIcon(f.primaryType()), Kind: kindField, Expandable: len(f.Children) > 0,
			Tooltip: f.Path + ": " + types + fmt.Sprintf(" (in %d of %d documents of the sample)", f.Count, parentCount),
			Data:    nodeData{Database: database, Collection: collection, Field: f, Of: parentCount},
		}
		nodes = append(nodes, n)
	}
	return nodes
}

// typeIcon is the Codicon of a BSON type.
func typeIcon(t string) string {
	switch t {
	case "string":
		return "symbol-string"
	case "int", "long", "double", "decimal":
		return "symbol-numeric"
	case "bool":
		return "symbol-boolean"
	case "array":
		return "symbol-array"
	case "object":
		return "symbol-structure"
	case "date", "timestamp":
		return "calendar"
	case "objectId", "uuid":
		return "key"
	case "binData":
		return "file-binary"
	case "regex":
		return "regex"
	case "null":
		return "circle-slash"
	}
	return "symbol-misc"
}

// FormatBytes writes a size in bytes as the explorer shows it: "512 B", "1.5 KiB", "12.0 MiB".
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

// FormatCount writes a number of documents with thousands separators and the word: "1 document", "12,345 documents".
func FormatCount(n int64) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if n == 1 {
		return s + " document"
	}
	return s + " documents"
}
