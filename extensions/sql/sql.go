// Package sql is the SQL extension (SPEC §10.21, D23): "SQL Databases", id emetos.sql. It is the SQL language pack (the grammar and the
// language configuration in assets/, copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE) and the driver
// of PostgreSQL, MySQL and MariaDB, SQLite and SQL Server for the "Databases" side bar (extensions/internal/dbconn): it connects
// through database/sql with the pure Go drivers (pgx, go-sql-driver/mysql, modernc.org/sqlite, go-mssqldb), browses the schema of a
// connection in the explorer and generates statements from it. extensions/sql/dialect holds what differs between the engines.
//
// The pieces are a Hub (made once by extensions.All, shared by the window tabs: the Service that holds the open connections and the
// dbconn.Driver registered with the Databases hub) and the Extension of each window tab (the command and the events of the tab).
// Step 19.3 adds the query editors and the results panel on the Service.
package sql

import (
	"context"
	"embed"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
)

// ID is the id of the extension.
const ID = "emetos.sql"

// The ids of the commands and events of the extension.
const (
	// NewQuery opens a query editor for a connection (SPEC §10.21 "Query editors"); its argument is {"connection": id}, or none to
	// choose among the open connections.
	NewQuery = ID + ".newQuery"
	// TopicConnected and TopicDisconnected are published, in every window tab, when a SQL connection is opened or closed:
	// {"connection": id}.
	TopicConnected    = ID + ".connected"
	TopicDisconnected = ID + ".disconnected"
)

//go:embed all:assets
var assets embed.FS

// Hub is what the window tabs of Emetos share: the open connections and the driver for the Databases view. extensions.All makes one.
type Hub struct {
	db  *dbconn.Hub
	svc *Service
	drv *catalogDriver
}

// NewHub makes the hub over the hub of the Databases extension. It opens nothing.
func NewHub(db *dbconn.Hub) *Hub {
	svc := NewService()
	return &Hub{db: db, svc: svc, drv: &catalogDriver{svc: svc}}
}

// Service is where the open connections are; the query editors of step 19.3 use it.
func (h *Hub) Service() *Service { return h.svc }

// FactoryOf makes the Factory of the extension over the hub. Every call makes an extension of its own.
func FactoryOf(h *Hub) extension.Factory {
	return func() extension.Extension {
		return &Extension{hub: h, pack: langpack.New(langpack.Spec{
			ID: ID, Name: "SQL Databases", Description: "SQL: syntax highlighting, connections to PostgreSQL, MySQL and MariaDB, SQLite and SQL Server, and the explorer of their schema.",
			Assets: assets,
		})}
	}
}

// Extension is the extension in one window tab.
type Extension struct {
	hub  *Hub
	pack extension.Extension
	api  extension.API
}

// Manifest implements extension.Extension: the language pack's, with the command.
func (e *Extension) Manifest() extension.Manifest {
	m := e.pack.Manifest()
	m.Name = "SQL Databases"
	m.Description = "SQL: syntax highlighting, connections to PostgreSQL, MySQL and MariaDB, SQLite and SQL Server, and the explorer of their schema."
	m.Contributes.Commands = []extension.CommandSpec{{ID: NewQuery, Title: "New Query", Category: "SQL", Description: "Open a new SQL editor for an open connection."}}
	return m
}

// Activate implements extension.Extension: it gives the engines to the Databases view, and publishes the events of the connections.
func (e *Extension) Activate(ctx context.Context, api extension.API) error {
	if err := e.pack.Activate(ctx, api); err != nil {
		return err
	}
	e.api = api
	if err := api.Commands().Handle(NewQuery, func(_ context.Context, args any) error { e.newQuery(args); return nil }); err != nil {
		return err
	}
	unregister := e.hub.db.RegisterDriver(e.hub.drv)
	stop := e.hub.svc.Subscribe(func(id string, open bool) {
		topic := TopicDisconnected
		if open {
			topic = TopicConnected
		}
		api.Post(func() {
			if err := api.Events().Publish(topic, map[string]string{"connection": id}); err != nil {
				api.Log().Warn("an event of a connection cannot be published", "topic", topic, "err", err)
			}
		})
	})
	go func() {
		<-ctx.Done()
		stop()
		unregister()
	}()
	return nil
}

// newQuery opens a query editor for the connection the command names, or for the one the user picks among the open ones.
func (e *Extension) newQuery(args any) {
	id := ""
	switch a := args.(type) {
	case string:
		id = a
	case map[string]any:
		id, _ = a["connection"].(string)
	}
	if id != "" {
		openText(e.api, "")
		return
	}
	var open []dbconn.Connection
	for _, c := range e.hub.db.Store().List(e.api.Scope().Folder) {
		if _, drv, ok := e.hub.db.Engine(c.Engine); ok && drv == dbconn.Driver(e.hub.drv) && e.hub.svc.IsOpen(c.ID) {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		e.api.Notifications().Info("There is no open SQL connection. Connect to a database in the Databases view first.")
		return
	}
	items := make([]extension.QuickPickItem, len(open))
	for i, c := range open {
		items[i] = extension.QuickPickItem{Label: "$(database) " + c.Name, Description: describeConnection(c)}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Which connection is the query for?", Items: items}, func(i int) {
		if i >= 0 && i < len(open) {
			openText(e.api, "") // TODO(step 19.3): bind the editor to the connection and its database
		}
	})
	if err != nil {
		e.api.Log().Warn("the list of connections cannot be shown", "err", err)
	}
}

// describeConnection is the short text after the name of a connection in a list.
func describeConnection(c dbconn.Connection) string {
	switch {
	case c.File != "":
		return c.File
	case c.Host != "":
		return c.Host
	}
	return c.Engine
}
