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
	open := func(text string) func() { return func() { openPlayground(api, text) } }
	if n == nil {
		return []dbconn.Action{
			{Title: "New Playground", Group: groupOpen, Run: open(playgroundText(c.Name, c.Database, ""))},
		}
	}
	data, _ := n.Data.(nodeData)
	switch n.Kind {
	case kindDatabase:
		return []dbconn.Action{
			{Title: "New Playground", Group: groupOpen, Run: open(playgroundText(c.Name, data.Database, ""))},
			{Title: "Create Collection", Group: groupCreate, Run: open(createCollectionText(data.Database))},
			{Title: "Drop Database", Group: groupDanger, Run: open(dropDatabaseText(data.Database))},
		}
	case kindCollection:
		acts := []dbconn.Action{
			{Title: "New Playground", Group: groupOpen, Run: open(playgroundText(c.Name, data.Database, data.Collection))},
		}
		if data.CollKind != KindView {
			acts = append(acts, dbconn.Action{Title: "Create Index", Group: groupCreate, Run: open(createIndexText(data.Database, data.Collection))})
		}
		return append(acts, dbconn.Action{Title: "Drop Collection", Group: groupDanger, Run: open(dropCollectionText(data.Database, data.Collection))})
	case kindIndexes:
		return []dbconn.Action{
			{Title: "Create Index", Group: groupCreate, Run: open(createIndexText(data.Database, data.Collection))},
		}
	case kindIndex:
		if data.Index == nil || data.Index.Name == "_id_" {
			return nil // the index of _id cannot be dropped
		}
		name := data.Index.Name
		return []dbconn.Action{
			{Title: "Drop Index", Group: groupDanger, Run: open(dropIndexText(data.Database, data.Collection, name))},
		}
	}
	return nil
}
