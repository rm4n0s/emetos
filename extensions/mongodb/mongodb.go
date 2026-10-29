package mongodb

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// The ids of the extension (SPEC §10.22).
const (
	// ID is the id of the extension.
	ID = "emetos.mongodb"
	// EngineID is the engine this extension registers with the shared view; it is the "engine" of a connection in the file.
	EngineID = "mongodb"
	// LanguageID is the language of the playgrounds (the .mongodb files).
	LanguageID = "mongodb"
	// NewPlaygroundCommand opens an untitled playground. It takes no argument, or {"connection": id, "database": name,
	// "collection": name} to start from a place of the explorer.
	NewPlaygroundCommand = "mongodb.newPlayground"
	// SchemaSampleSizeSetting is how many documents are read to sample the schema of a collection.
	SchemaSampleSizeSetting = "mongodb.schemaSampleSize"

	// ConnectedTopic and DisconnectedTopic are the events of a connection that opened or closed; the data is {"connection": id}.
	ConnectedTopic    = ID + ".connected"
	DisconnectedTopic = ID + ".disconnected"

	defaultSampleSize = 100
)

//go:embed all:assets
var assets embed.FS

// Options configure the extension. The zero value is the real one.
type Options struct {
	// Dialer opens the servers; the official driver when nil. A test gives a fake.
	Dialer Dialer
}

// FactoryOf makes the Factory of the extension over the hub that the database extensions share (extensions.All makes one hub and
// gives it to each). Every instance the factory makes (one for each window tab) shares the servers that are open.
func FactoryOf(hub *dbconn.Hub, opts ...Options) extension.Factory {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Dialer == nil {
		o.Dialer = driverDialer{}
	}
	m := newManager(o.Dialer)
	return func() extension.Extension { return &Extension{hub: hub, mgr: m} }
}

// manager holds the servers that are open, by the ID of their connection. It is safe for use from several goroutines.
type manager struct {
	dialer Dialer

	mu      sync.Mutex
	servers map[string]Backend
	// trials are the servers of a Test Connection of a connection that is open: Hub.Test connects and disconnects with the ID of the
	// connection, and must not take the open server away.
	trials map[string][]Backend
	// tabs are the playgrounds of the window tabs, by scope.
	tabs map[extension.ScopeInfo]*playgrounds
}

func newManager(d Dialer) *manager {
	return &manager{dialer: d, servers: map[string]Backend{}, trials: map[string][]Backend{}, tabs: map[extension.ScopeInfo]*playgrounds{}}
}

// open dials the server and keeps it. The hub never connects a connection that is open, so a connect for an ID that is open is a
// Test Connection (of an edited connection): its server is kept apart and the next close of the ID ends it, not the open one.
func (m *manager) open(ctx context.Context, c dbconn.Connection, password string) error {
	b, err := m.dialer.Dial(ctx, c, password)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if _, open := m.servers[c.ID]; open {
		m.trials[c.ID] = append(m.trials[c.ID], b)
	} else {
		m.servers[c.ID] = b
	}
	m.mu.Unlock()
	return nil
}

// backend returns the open server of a connection.
func (m *manager) backend(id string) (Backend, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.servers[id]
	return b, ok
}

// take removes a server of a connection from the manager and returns it (a trial of Test Connection first); false for a connection that
// is not open.
func (m *manager) take(id string) (Backend, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.trials[id]; len(t) > 0 {
		b := t[len(t)-1]
		if len(t) == 1 {
			delete(m.trials, id)
		} else {
			m.trials[id] = t[:len(t)-1]
		}
		return b, true
	}
	b, ok := m.servers[id]
	delete(m.servers, id)
	return b, ok
}

// close ends the connection to the server: the connection counts as closed at once, and the driver is told in the background, so that
// a server that does not answer cannot hold the UI thread (Hub.Disconnect runs on it). Closing a connection that is not open does
// nothing.
func (m *manager) close(c dbconn.Connection) {
	if b, ok := m.take(c.ID); ok {
		go closeBackend(b, c.Name)
	}
}

// closeBackend closes a server nobody will use again and logs what it could not do.
func closeBackend(b Backend, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Close(ctx); err != nil {
		slog.Warn("a MongoDB connection did not close", "connection", name, "err", err)
	}
}

// changed tells the window tabs that a connection was opened or closed.
func (m *manager) changed() {
	m.mu.Lock()
	tabs := make([]*playgrounds, 0, len(m.tabs))
	for _, pg := range m.tabs {
		tabs = append(tabs, pg)
	}
	m.mu.Unlock()
	for _, pg := range tabs {
		pg.api.Post(pg.connectionChanged)
	}
}

// playgroundsOf returns the playgrounds of the window tab of the API; nil for a tab that has none.
func (m *manager) playgroundsOf(api extension.API) *playgrounds {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tabs[api.Scope()]
}

// Extension is the MongoDB extension in one window tab.
type Extension struct {
	hub *dbconn.Hub
	mgr *manager

	api extension.API
	// sample is the value of mongodb.schemaSampleSize, read on the UI thread and used by the goroutines that list a schema.
	sample atomic.Int64
	drv    *driver
	pg     *playgrounds
}

// Manifest implements extension.Extension.
func (*Extension) Manifest() extension.Manifest {
	langs, err := fs.Sub(assets, "assets")
	if err != nil {
		langs = nil // an embedded folder that is not there is a mistake of the build; the extension then has no language
	}
	return extension.Manifest{
		ID: ID, Name: "MongoDB", Version: "0.1.0",
		Description:      "Connect to MongoDB, browse its databases, collections, indexes and schema, and write playgrounds.",
		ActivationEvents: []string{extension.OnStartup},
		Contributes: extension.Contributes{
			Commands:  commands(),
			Languages: langs,
			Keybindings: []extension.Keybinding{
				{Command: RunCommand, Key: "ctrl+enter", When: "editorTextFocus && editorLangId == " + LanguageID},
				{Command: RunAllCommand, Key: "ctrl+shift+enter", When: "editorTextFocus && editorLangId == " + LanguageID},
				{Command: CancelCommand, Key: "escape", When: "editorTextFocus && editorLangId == " + LanguageID + " && " + RunningKey},
				{Command: SaveDocumentCommand, Key: "ctrl+s", When: "editorTextFocus && " + EditingKey},
			},
			Menus: []extension.MenuItem{
				{Menu: extension.MenuEditorContext, Command: RunCommand, Group: "1_mongodb", Order: 1, When: "editorLangId == " + LanguageID},
				{Menu: extension.MenuEditorContext, Command: RunAllCommand, Group: "1_mongodb", Order: 2, When: "editorLangId == " + LanguageID},
				{Menu: extension.MenuEditorContext, Command: SelectConnectionCommand, Group: "1_mongodb", Order: 3, When: "editorLangId == " + LanguageID},
				{Menu: extension.MenuEditorContext, Command: SaveDocumentCommand, Group: "1_mongodb", Order: 4, When: EditingKey},
			},
			PanelViews: []extension.PanelViewSpec{{ID: ViewID, Title: "MongoDB Results", Icon: "table", Order: 5}},
			ViewActions: []extension.ViewActionSpec{
				{View: ViewID, Command: CancelCommand, Icon: "debug-stop", Tooltip: "Cancel Statement", When: RunningKey, Order: 1},
				{View: ViewID, Command: RunCommand, Icon: "play", Tooltip: "Run Statement", When: "!" + RunningKey, Order: 1},
			},
			Settings: []extension.SettingSpec{
				{Key: SchemaSampleSizeSetting, Type: "integer", Default: defaultSampleSize, Category: "MongoDB",
					Minimum: ptr(1.0), Maximum: ptr(10000.0),
					Description: "How many documents are read from a collection to show the fields it has and their types (the Schema of a collection in the Databases view)."},
				{Key: PageSizeSetting, Type: "integer", Default: defaultPageSize, Category: "MongoDB", Minimum: ptr(1.0), Maximum: ptr(1000.0),
					Description: "How many documents a cursor gives at first and each time More is pressed in the results."},
				{Key: ConfirmDestructiveSetting, Type: "boolean", Default: true, Category: "MongoDB",
					Description: "Ask before running drop, dropDatabase, deleteMany({}) and updateMany({}, ...)."},
			},
		},
	}
}

// commands are the commands of the extension.
func commands() []extension.CommandSpec {
	inPlayground := "editorLangId == " + LanguageID
	return []extension.CommandSpec{
		{ID: NewPlaygroundCommand, Title: "New Playground", Category: "MongoDB", Description: "Opens a new MongoDB playground (a .mongodb file that is not saved yet)."},
		{ID: RunCommand, Title: "Run Statement", Category: "MongoDB", Description: "Run the selection, or the statement under the cursor, of the playground.", When: inPlayground},
		{ID: RunAllCommand, Title: "Run All Statements", Category: "MongoDB", Description: "Run every statement of the playground, in order.", When: inPlayground},
		{ID: CancelCommand, Title: "Cancel Statement", Category: "MongoDB", Description: "Stop the statement that is running.", When: RunningKey},
		{ID: SelectConnectionCommand, Title: "Select Connection for This Playground", Category: "MongoDB", Description: "Choose the connection and the database this playground runs on.", When: inPlayground},
		{ID: OpenCollectionCommand, Title: "Open Collection", Category: "MongoDB", Description: "Show the first documents of a collection in the results; the argument is {connection, database, collection}."},
		{ID: EditDocumentCommand, Title: "Edit Document", Category: "MongoDB", Description: "Open the selected document of the results as Extended JSON."},
		{ID: DeleteDocumentCommand, Title: "Delete Document", Category: "MongoDB", Description: "Delete the selected document of the results from its collection, after asking."},
		{ID: SaveDocumentCommand, Title: "Save Document to the Server", Category: "MongoDB", Description: "Replace the document on the server with the text of this editor, after showing the fields that changed.", When: EditingKey},
		{ID: CopyDocumentCommand, Title: "Copy Document", Category: "MongoDB", Description: "Copy the selected document of the results, or all of them, as Extended JSON."},
	}
}

// ptr returns a pointer to a copy of v.
func ptr[T any](v T) *T { return &v }

// Activate implements extension.Extension: it registers the driver of the engine with the shared hub.
func (e *Extension) Activate(ctx context.Context, api extension.API) error {
	e.api = api
	e.readSampleSize()
	api.Settings().OnChange(e.readSampleSize)
	if err := api.Commands().Handle(NewPlaygroundCommand, func(_ context.Context, args any) error {
		e.newPlaygroundCommand(args)
		return nil
	}); err != nil {
		return err
	}
	pg := newPlaygrounds(e, api)
	e.pg = pg
	if err := pg.start(); err != nil {
		return err
	}
	e.mgr.mu.Lock()
	e.mgr.tabs[api.Scope()] = pg
	e.mgr.mu.Unlock()
	e.drv = &driver{e: e}
	unregister := e.hub.RegisterDriver(e.drv)
	go func() {
		<-ctx.Done()
		unregister()
		e.mgr.mu.Lock()
		delete(e.mgr.tabs, api.Scope())
		e.mgr.mu.Unlock()
		pg.stop()
	}()
	return nil
}

// readSampleSize reads mongodb.schemaSampleSize on the UI thread.
func (e *Extension) readSampleSize() {
	n := int64(defaultSampleSize)
	if v, err := e.api.Settings().Get(SchemaSampleSizeSetting); err == nil {
		if f, ok := v.(float64); ok && f >= 1 && f <= 10000 {
			n = int64(f)
		}
	}
	e.sample.Store(n)
}

// sampleSize is how many documents a schema reads.
func (e *Extension) sampleSize() int { return int(e.sample.Load()) }

// publish sends an event of the extension on the UI thread.
func (e *Extension) publish(topic string, data any) {
	e.api.Post(func() {
		if err := e.api.Events().Publish(topic, data); err != nil {
			e.api.Log().Warn("an event could not be published", "topic", topic, "err", err)
		}
	})
}

// newPlaygroundCommand runs mongodb.newPlayground.
func (e *Extension) newPlaygroundCommand(args any) {
	var connName, database, collection string
	var conn dbconn.Connection
	if m, ok := args.(map[string]any); ok {
		database, _ = m["database"].(string)
		collection, _ = m["collection"].(string)
		if id, _ := m["connection"].(string); id != "" {
			if c, found := e.hub.Store().Get(id); found {
				conn, connName = c, c.Name
				if database == "" {
					database = c.Database
				}
			}
		}
	}
	text := playgroundText(connName, database, collection)
	if conn.ID != "" && e.pg != nil {
		e.pg.openPlayground(conn, database, text)
		return
	}
	openPlayground(e.api, text)
}

// openPlayground opens an untitled playground with the text in the window tab of api.
func openPlayground(api extension.API, text string) {
	if _, err := api.Documents().OpenUntitled(LanguageID, text); err != nil {
		api.Log().Warn("a playground could not be opened", "err", err)
		api.Notifications().Error("Cannot open a playground: " + err.Error())
	}
}
