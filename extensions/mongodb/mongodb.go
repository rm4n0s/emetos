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
}

func newManager(d Dialer) *manager {
	return &manager{dialer: d, servers: map[string]Backend{}, trials: map[string][]Backend{}}
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

// Extension is the MongoDB extension in one window tab.
type Extension struct {
	hub *dbconn.Hub
	mgr *manager

	api extension.API
	// sample is the value of mongodb.schemaSampleSize, read on the UI thread and used by the goroutines that list a schema.
	sample atomic.Int64
	drv    *driver
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
			Commands: []extension.CommandSpec{
				{ID: NewPlaygroundCommand, Title: "New Playground", Category: "MongoDB", Description: "Opens a new MongoDB playground (a .mongodb file that is not saved yet)."},
			},
			Languages: langs,
			Settings: []extension.SettingSpec{
				{Key: SchemaSampleSizeSetting, Type: "integer", Default: defaultSampleSize, Category: "MongoDB",
					Minimum: ptr(1.0), Maximum: ptr(10000.0),
					Description: "How many documents are read from a collection to show the fields it has and their types (the Schema of a collection in the Databases view)."},
			},
		},
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
	e.drv = &driver{e: e}
	unregister := e.hub.RegisterDriver(e.drv)
	go func() {
		<-ctx.Done()
		unregister()
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
	if m, ok := args.(map[string]any); ok {
		database, _ = m["database"].(string)
		collection, _ = m["collection"].(string)
		if id, _ := m["connection"].(string); id != "" {
			if c, found := e.hub.Store().Get(id); found {
				connName = c.Name
				if database == "" {
					database = c.Database
				}
			}
		}
	}
	openPlayground(e.api, playgroundText(connName, database, collection))
}

// openPlayground opens an untitled playground with the text in the window tab of api.
func openPlayground(api extension.API, text string) {
	if _, err := api.Documents().OpenUntitled(LanguageID, text); err != nil {
		api.Log().Warn("a playground could not be opened", "err", err)
		api.Notifications().Error("Cannot open a playground: " + err.Error())
	}
}
