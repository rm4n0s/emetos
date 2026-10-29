package extensions

// The rig of the tests of Connect to Database (SPEC §10.24, step 22.1): one container extension (Docker's or Podman's, over the
// engine the test gives) and the four extensions that answer it (SQL, MongoDB, Redis and the Databases extension), each activated
// over its own fake API, with the events between them carried the way the host carries them: what the container extension publishes
// is delivered to the others as published by it, and the requests of the database extensions are handled by the Databases extension.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/containers"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerui"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/mongodb"
	"github.com/rm4n0s/emetos/extensions/podman"
	"github.com/rm4n0s/emetos/extensions/redis"
	"github.com/rm4n0s/emetos/extensions/sql"
)

// connectRig is a container extension and the database extensions over fake APIs.
type connectRig struct {
	t *testing.T
	// hub is the hub of the connections; sql the hub of the SQL extension.
	hub *dbconn.Hub
	sql *sql.Hub
	// cont is the container extension and its API; dbs are the Databases extension's API.
	cont    *fakeapi.API
	contExt extension.Extension
	dbs     *fakeapi.API
	dbsExt  *dbconn.Extension
	// asked maps the id of a database extension to the prefills it asked the Databases extension to open.
	asked map[string][]dbconn.Prefill
	// listeners are the APIs the events are carried to, by the id of their extension.
	listeners map[string]*fakeapi.API
	// forwarded is how many events of the container extension were carried already.
	forwarded int
}

// startConnectRig activates the extensions. cfg is the configuration of a container extension with the engine given by its Dial.
func startConnectRig(t *testing.T, cfg containerui.Config) *connectRig {
	t.Helper()
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(state, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(state, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(state, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(state, "cache"))
	folder := t.TempDir()
	scope := extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: folder, ID: folder}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	r := &connectRig{t: t, asked: map[string][]dbconn.Prefill{}, listeners: map[string]*fakeapi.API{}}
	r.hub = dbconn.NewHub(dbconn.HubOptions{StorePath: filepath.Join(state, "db-connections.json"), Keyring: &dbconn.MemoryKeyring{}})
	r.sql = sql.NewHub(r.hub)

	activate := func(e extension.Extension) *fakeapi.API {
		api := fakeapi.New(t, e.Manifest(), scope)
		if err := e.Activate(ctx, api); err != nil {
			t.Fatalf("activating %s: %v", e.Manifest().ID, err)
		}
		return api
	}
	r.dbsExt = dbconn.Factory(r.hub)().(*dbconn.Extension)
	r.dbs = activate(r.dbsExt)
	for _, e := range []extension.Extension{sql.FactoryOf(r.sql)(), mongodb.FactoryOf(r.hub)(), redis.Factory(r.hub)()} {
		id := e.Manifest().ID
		api := activate(e)
		r.listeners[id] = api
		api.OnRequest(dbconn.TopicOpenForm, func(ctx context.Context, ev extension.Event, answer func(any, error)) {
			var p dbconn.Prefill
			if err := json.Unmarshal(ev.Data, &p); err != nil {
				answer(nil, err)
				return
			}
			r.asked[id] = append(r.asked[id], p)
			reply, err := r.dbs.AskAs(ctx, id, dbconn.TopicOpenForm, p)
			answer(json.RawMessage(reply.Data), err)
		})
	}
	r.contExt = containerui.New(cfg)
	r.cont = activate(r.contExt)
	return r
}

// dockerConfig and podmanConfig are the configurations of the two container extensions with the engine and the filter of the lists
// given; the lists show only what the filter lets through (a test against a real engine lists only what it made itself).
func dockerConfig(dial containerui.DialFunc, filters containerui.ListFilters) containerui.Config {
	cfg := containers.Config()
	cfg.Dial, cfg.ListFilters, cfg.Getenv = dial, filters, func(string) string { return "" }
	return cfg
}

func podmanConfig(dial containerui.DialFunc, filters containerui.ListFilters) containerui.Config {
	cfg := podman.Config()
	cfg.Dial, cfg.ListFilters, cfg.Getenv = dial, filters, func(string) string { return "" }
	return cfg
}

// fixedEngine is a Dial that returns the client the test has.
func fixedEngine(c *containerapi.Client) containerui.DialFunc {
	return func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
		return &containerapi.Found{Client: c, Source: "the test's engine"}, nil
	}
}

// carry delivers the events the container extension published since the last call to the database extensions, as published by it, and
// lets the requests they make run.
func (r *connectRig) carry() {
	r.t.Helper()
	published := r.cont.PublishedEvents()
	id := r.contExt.Manifest().ID
	for ; r.forwarded < len(published); r.forwarded++ {
		for _, api := range r.listeners {
			api.EmitAs(id, published[r.forwarded].Topic, json.RawMessage(published[r.forwarded].Data))
		}
	}
	for _, api := range r.listeners {
		api.Pump()
	}
	r.dbs.Pump()
}

// openedForm returns the draft of the last form the Databases extension opened, and false when none was.
func (r *connectRig) openedForm() (dbconn.Draft, bool) {
	r.t.Helper()
	tabs := r.dbs.EditorTabs()
	if len(tabs) == 0 {
		return dbconn.Draft{}, false
	}
	var st struct {
		Draft dbconn.Draft `json:"draft"`
	}
	if err := json.Unmarshal(tabs[len(tabs)-1].State(), &st); err != nil {
		r.t.Fatalf("the state of the form: %v", err)
	}
	return st.Draft, true
}

// runConnectDatabase runs Connect to Database of the container extension on a container (by id) as the menu does, once the extension
// has listed the containers.
func (r *connectRig) runConnectDatabase(id string) {
	r.t.Helper()
	id0 := r.contExt.Manifest().ID
	r.cont.WaitFor("the engine to answer", func() bool { return r.cont.ContextKey(id0+".connected") == true })
	r.cont.WaitFor("the containers to be listed", func() bool { return containerui.ContainersListed(r.contExt) })
	prefix := "containers"
	if id0 == podman.ID {
		prefix = "podman"
	}
	if err := r.cont.Run(prefix+".containers.connectDatabase", map[string]any{"key": "c:" + id}); err != nil {
		r.t.Fatalf("Connect to Database: %v", err)
	}
}

// connectDatabase runs Connect to Database and waits for the event it publishes, then carries it to the database extensions.
func (r *connectRig) connectDatabase(id string) {
	r.t.Helper()
	topic := r.contExt.Manifest().ID + ".connectDatabase"
	count := func() int {
		n := 0
		for _, p := range r.cont.PublishedEvents() {
			if p.Topic == topic {
				n++
			}
		}
		return n
	}
	before := count()
	r.runConnectDatabase(id)
	r.cont.WaitFor("the event of Connect to Database", func() bool { return count() > before })
	r.carry()
}
