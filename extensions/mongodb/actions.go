package mongodb

import (
	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// The groups of the context menu, in the order they show.
const (
	groupOpen   = "1-open"
	groupCreate = "2-create"
	groupDanger = "3-drop"
)

var _ dbconn.APIDriver = (*driver)(nil)

// Actions implements dbconn.Driver for a caller that has no window tab: the playgrounds open in the tab of the extension that
// registered the driver.
func (d *driver) Actions(c dbconn.Connection, n *dbconn.Node) []dbconn.Action {
	return d.actions(d.e.api, c, n)
}

// ActionsIn implements dbconn.APIDriver: the playgrounds open in the window tab the menu was opened in, not in the tab of the extension
// that registered the driver (one driver serves every window tab).
func (d *driver) ActionsIn(api extension.API, c dbconn.Connection, n *dbconn.Node) []dbconn.Action {
	return d.actions(api, c, n)
}

// actions are the entries MongoDB adds to the context menu of a node of the Databases view (SPEC §10.22). The ones that change the
// server are playgrounds with the statement written: the user reads and runs them, nothing runs at once.
func (d *driver) actions(api extension.API, c dbconn.Connection, n *dbconn.Node) []dbconn.Action {
	pg := d.e.mgr.playgroundsOf(api)
	// open opens a playground in the tab of the menu, bound to the connection and the database of the node
	open := func(text string) func() { return openIn(api, pg, c, "", text) }
	if n == nil {
		return []dbconn.Action{
			{Title: "New Playground", Group: groupOpen, Run: open(playgroundText(c.Name, c.Database, ""))},
		}
	}
	data, _ := n.Data.(nodeData)
	switch n.Kind {
	case kindDatabase:
		in := func(text string) func() { return openIn(api, pg, c, data.Database, text) }
		return []dbconn.Action{
			{Title: "New Playground", Group: groupOpen, Run: in(playgroundText(c.Name, data.Database, ""))},
			{Title: "Create Collection", Group: groupCreate, Run: in(createCollectionText(data.Database))},
			{Title: "Drop Database", Group: groupDanger, Run: in(dropDatabaseText(data.Database))},
		}
	case kindCollection:
		in := func(text string) func() { return openIn(api, pg, c, data.Database, text) }
		acts := []dbconn.Action{}
		if pg != nil {
			acts = append(acts, dbconn.Action{Title: "Open Collection", Group: groupOpen, Run: func() { pg.openCollection(c, data.Database, data.Collection) }})
		}
		acts = append(acts, dbconn.Action{Title: "New Playground", Group: groupOpen, Run: in(playgroundText(c.Name, data.Database, data.Collection))})
		if data.CollKind != KindView {
			acts = append(acts, dbconn.Action{Title: "Create Index", Group: groupCreate, Run: in(createIndexText(data.Database, data.Collection))})
		}
		return append(acts, dbconn.Action{Title: "Drop Collection", Group: groupDanger, Run: in(dropCollectionText(data.Database, data.Collection))})
	case kindIndexes:
		return []dbconn.Action{
			{Title: "Create Index", Group: groupCreate, Run: openIn(api, pg, c, data.Database, createIndexText(data.Database, data.Collection))},
		}
	case kindIndex:
		if data.Index == nil || data.Index.Name == "_id_" {
			return nil // the index of _id cannot be dropped
		}
		name := data.Index.Name
		return []dbconn.Action{
			{Title: "Drop Index", Group: groupDanger, Run: openIn(api, pg, c, data.Database, dropIndexText(data.Database, data.Collection, name))},
		}
	}
	return nil
}

// openIn returns the function that opens a playground with the text in the window tab of api, bound to the connection and the database
// when the tab has playgrounds.
func openIn(api extension.API, pg *playgrounds, c dbconn.Connection, database, text string) func() {
	return func() {
		if pg != nil {
			pg.openPlayground(c, database, text)
			return
		}
		openPlayground(api, text)
	}
}
