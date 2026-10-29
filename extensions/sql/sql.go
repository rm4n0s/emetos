// Package sql is the SQL extension (SPEC §10.21, D23): "SQL Databases", id emetos.sql. It is the SQL language pack (the grammar and the
// language configuration in assets/, copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE) and the driver
// of PostgreSQL, MySQL and MariaDB, SQLite and SQL Server for the "Databases" side bar (extensions/internal/dbconn): it connects
// through database/sql with the pure Go drivers (pgx, go-sql-driver/mysql, modernc.org/sqlite, go-mssqldb), browses the schema of a
// connection in the explorer and generates statements from it. extensions/sql/dialect holds what differs between the engines.
//
// The pieces are a Hub (made once by extensions.All, shared by the window tabs: the Service that holds the open connections and the
// dbconn.Driver registered with the Databases hub) and the Extension of each window tab (the command and the events of the tab).
// The query editors, the results panel and the safety checks (SPEC §10.21 "Query editors", "The results panel", "Safety") are the
// queries of each window tab (queries.go, panel.go, complete.go, export.go): a Runner (run.go) keeps a session of the connection for
// each SQL editor, and dialect.Split and dialect.Classify cut a script into statements and say which only read.
package sql

import (
	"context"
	"embed"
	"sync"

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

	mu   sync.Mutex
	tabs map[extension.ScopeInfo]*queries
}

// NewHub makes the hub over the hub of the Databases extension. It opens nothing.
func NewHub(db *dbconn.Hub) *Hub {
	svc := NewService()
	h := &Hub{db: db, svc: svc, tabs: map[extension.ScopeInfo]*queries{}}
	h.drv = &catalogDriver{svc: svc, hub: h}
	return h
}

// queriesOf returns the query editors of the window tab of the API; nil for a tab that has none.
func (h *Hub) queriesOf(api extension.API) *queries {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tabs[api.Scope()]
}

// Service is where the open connections are; the explorer and the query editors use it.
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
	q    *queries
}

// Manifest implements extension.Extension: the language pack's, with the command.
func (e *Extension) Manifest() extension.Manifest {
	m := e.pack.Manifest()
	m.Name = "SQL Databases"
	m.Description = "SQL: syntax highlighting, connections to PostgreSQL, MySQL and MariaDB, SQLite and SQL Server, and the explorer of their schema."
	m.Contributes.Commands = commands()
	m.Contributes.Keybindings = []extension.Keybinding{
		{Command: RunQuery, Key: "ctrl+enter", When: "editorTextFocus && editorLangId == sql"},
		{Command: RunAllQueries, Key: "ctrl+shift+enter", When: "editorTextFocus && editorLangId == sql"},
		{Command: CancelQuery, Key: "escape", When: "editorTextFocus && editorLangId == sql && " + RunningKey},
	}
	m.Contributes.Menus = []extension.MenuItem{
		{Menu: extension.MenuEditorContext, Command: RunQuery, Group: "1_sql", Order: 1, When: "editorLangId == sql"},
		{Menu: extension.MenuEditorContext, Command: RunAllQueries, Group: "1_sql", Order: 2, When: "editorLangId == sql"},
		{Menu: extension.MenuEditorContext, Command: ChooseConnection, Group: "1_sql", Order: 3, When: "editorLangId == sql"},
	}
	m.Contributes.PanelViews = []extension.PanelViewSpec{{ID: ViewID, Title: "Query Results", Icon: "table", Order: 4}}
	m.Contributes.ViewActions = []extension.ViewActionSpec{
		{View: ViewID, Command: CancelQuery, Icon: "debug-stop", Tooltip: "Cancel Query", When: RunningKey, Order: 1},
		{View: ViewID, Command: RunQuery, Icon: "play", Tooltip: "Run Query", When: "!" + RunningKey, Order: 1},
	}
	m.Contributes.Settings = []extension.SettingSpec{
		{Key: SettingMaxRows, Type: "integer", Default: DefaultMaxRows, Minimum: ptr(1.0), Maximum: ptr(1000000.0), Category: "SQL",
			Description: "The rows a result reads at first and each time Fetch More is pressed."},
		{Key: SettingConfirmDestructive, Type: "boolean", Default: true, Category: "SQL",
			Description: "Ask before running DROP, TRUNCATE, ALTER, and a DELETE or UPDATE without WHERE."},
	}
	m.Contributes.ContextKeys = nil
	m.Events.Subscribes = append(m.Events.Subscribes, dbconn.ContainerTopics()...)
	return m
}

// ptr returns a pointer to a copy of v.
func ptr[T any](v T) *T { return &v }

// commands are the commands of the extension.
func commands() []extension.CommandSpec {
	return []extension.CommandSpec{
		{ID: NewQuery, Title: "New Query", Category: "SQL", Description: "Open a new SQL editor for an open connection."},
		{ID: RunQuery, Title: "Run Query", Category: "SQL", Description: "Run the selection, or the statement under the cursor.", When: "editorLangId == sql"},
		{ID: RunAllQueries, Title: "Run All Queries", Category: "SQL", Description: "Run every statement of the editor.", When: "editorLangId == sql"},
		{ID: CancelQuery, Title: "Cancel Query", Category: "SQL", Description: "Stop the statement that is running.", When: RunningKey},
		{ID: BeginTransaction, Title: "Begin Transaction", Category: "SQL", Description: "Start a transaction in the session of this editor.", When: "editorLangId == sql"},
		{ID: CommitTransaction, Title: "Commit Transaction", Category: "SQL", Description: "Commit the transaction of this editor.", When: InTransactionKey},
		{ID: RollbackTransaction, Title: "Rollback Transaction", Category: "SQL", Description: "Roll back the transaction of this editor.", When: InTransactionKey},
		{ID: ChooseConnection, Title: "Select Connection for This Editor", Category: "SQL", Description: "Choose the connection and the database this editor runs on.", When: "editorLangId == sql"},
		{ID: ExportResult, Title: "Export Result", Category: "SQL", Description: "Write the result that is shown to a CSV or JSON file, or as SQL INSERT statements."},
	}
}

// Activate implements extension.Extension: it gives the engines to the Databases view, starts the query editors of the tab, and
// publishes the events of the connections.
func (e *Extension) Activate(ctx context.Context, api extension.API) error {
	if err := e.pack.Activate(ctx, api); err != nil {
		return err
	}
	e.api = api
	q := newQueries(e.hub, api)
	e.q = q
	if err := api.Commands().Handle(NewQuery, func(_ context.Context, args any) error { q.newQuery(args); return nil }); err != nil {
		return err
	}
	if err := q.start(); err != nil {
		return err
	}
	e.hub.mu.Lock()
	e.hub.tabs[api.Scope()] = q
	e.hub.mu.Unlock()
	// Connect to Database on a container of PostgreSQL, MySQL, MariaDB or SQL Server (SPEC §10.24)
	if err := dbconn.HandleContainerDatabase(ctx, api, "postgres", "mysql", "sqlserver"); err != nil {
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
			q.connectionChanged(id)
		})
	})
	go func() {
		<-ctx.Done()
		stop()
		unregister()
		e.hub.mu.Lock()
		delete(e.hub.tabs, api.Scope())
		e.hub.mu.Unlock()
		q.stop()
	}()
	return nil
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
