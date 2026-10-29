package mongodb

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// fakeBackend is a server that answers from tables.
type fakeBackend struct {
	mu sync.Mutex

	dbs      []DatabaseInfo
	colls    map[string][]CollectionInfo // by database
	counts   map[string]int64            // by "db.coll"
	countErr map[string]error
	indexes  map[string][]IndexInfo
	samples  map[string][]bson.D
	// err, when set, is what every call returns.
	err error
	// block, when set, holds the calls of EstimatedCount until it is closed.
	block chan struct{}

	calls       []string
	sampleSizes []int
	closed      int

	// exec, when set, answers Exec; execs records the calls and page sizes it received.
	exec  func(c Call, pageSize int) (Reply, error)
	execs []execCall
}

// execCall is a call Exec received.
type execCall struct {
	Call     Call
	PageSize int
}

func key(db, coll string) string { return db + "." + coll }

func (f *fakeBackend) note(call string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return f.err
}

func (f *fakeBackend) Databases(context.Context) ([]DatabaseInfo, error) {
	if err := f.note("databases"); err != nil {
		return nil, err
	}
	return append([]DatabaseInfo(nil), f.dbs...), nil
}

func (f *fakeBackend) Collections(_ context.Context, db string) ([]CollectionInfo, error) {
	if err := f.note("collections " + db); err != nil {
		return nil, err
	}
	return append([]CollectionInfo(nil), f.colls[db]...), nil
}

func (f *fakeBackend) EstimatedCount(ctx context.Context, db, coll string) (int64, error) {
	if err := f.note("count " + key(db, coll)); err != nil {
		return 0, err
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	if err := f.countErr[key(db, coll)]; err != nil {
		return 0, err
	}
	return f.counts[key(db, coll)], nil
}

func (f *fakeBackend) Indexes(_ context.Context, db, coll string) ([]IndexInfo, error) {
	if err := f.note("indexes " + key(db, coll)); err != nil {
		return nil, err
	}
	return append([]IndexInfo(nil), f.indexes[key(db, coll)]...), nil
}

func (f *fakeBackend) Sample(_ context.Context, db, coll string, n int) ([]bson.D, error) {
	if err := f.note("sample " + key(db, coll)); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.sampleSizes = append(f.sampleSizes, n)
	f.mu.Unlock()
	return append([]bson.D(nil), f.samples[key(db, coll)]...), nil
}

func (f *fakeBackend) Exec(ctx context.Context, c Call, pageSize int) (Reply, error) {
	if err := f.note("exec " + string(c.Kind)); err != nil {
		return Reply{}, err
	}
	f.mu.Lock()
	f.execs = append(f.execs, execCall{c, pageSize})
	fn := f.exec
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Reply{}, err
	}
	if fn == nil {
		return Reply{}, nil
	}
	return fn(c, pageSize)
}

// execList returns the calls Exec received.
func (f *fakeBackend) execList() []execCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]execCall(nil), f.execs...)
}

// fakeCursor is a cursor over documents in memory.
type fakeCursor struct {
	mu     sync.Mutex
	docs   []bson.D
	closed bool
	reads  []int
	err    error
}

func (c *fakeCursor) Next(_ context.Context, n int) ([]bson.D, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads = append(c.reads, n)
	if c.err != nil {
		c.closed = true
		return nil, true, c.err
	}
	n = min(n, len(c.docs))
	out := c.docs[:n]
	c.docs = c.docs[n:]
	if len(c.docs) == 0 {
		c.closed = true
		return out, true, nil
	}
	return out, false, nil
}

func (c *fakeCursor) Close(context.Context) error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func (c *fakeCursor) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (f *fakeBackend) Close(context.Context) error {
	f.mu.Lock()
	f.closed++
	f.mu.Unlock()
	return nil
}

func (f *fakeBackend) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeBackend) closedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// fixtureBackend is a server with a shop and the databases of the server itself.
func fixtureBackend() *fakeBackend {
	ts := int32(3600)
	return &fakeBackend{
		dbs: []DatabaseInfo{
			{Name: "local", SizeOnDisk: 73728},
			{Name: "shop", SizeOnDisk: 12_582_912},
			{Name: "admin", SizeOnDisk: 40960},
			{Name: "Archive", Empty: true},
			{Name: "config", SizeOnDisk: 98304},
		},
		colls: map[string][]CollectionInfo{
			"shop": {
				{Name: "orders", Kind: KindCollection},
				{Name: "system.views", Kind: KindCollection},
				{Name: "customers", Kind: KindCollection},
				{Name: "open_orders", Kind: KindView, ViewOn: "orders"},
				{Name: "events", Kind: KindTimeSeries},
			},
		},
		counts: map[string]int64{"shop.orders": 123456, "shop.customers": 1, "shop.events": 0, "shop.system.views": 1},
		indexes: map[string][]IndexInfo{
			"shop.orders": {
				{Name: "_id_", Keys: bson.D{{Key: "_id", Value: int32(1)}}},
				{Name: "sku_1_created_-1", Keys: bson.D{{Key: "sku", Value: int32(1)}, {Key: "created", Value: int32(-1)}}, Unique: true, Sparse: true},
				{Name: "created_ttl", Keys: bson.D{{Key: "created", Value: int32(1)}}, TTLSeconds: &ts},
				{Name: "text", Keys: bson.D{{Key: "_fts", Value: "text"}, {Key: "_ftsx", Value: int32(1)}}},
			},
		},
		samples: map[string][]bson.D{
			"shop.orders": {
				{{Key: "_id", Value: bson.ObjectID{1}}, {Key: "sku", Value: "A1"}, {Key: "qty", Value: int32(2)},
					{Key: "customer", Value: bson.D{{Key: "name", Value: "Ann"}, {Key: "address", Value: bson.D{{Key: "city", Value: "Athens"}}}}},
					{Key: "tags", Value: bson.A{"new", "gift"}}, {Key: "note", Value: nil}},
				{{Key: "_id", Value: bson.ObjectID{2}}, {Key: "sku", Value: "B2"}, {Key: "qty", Value: int64(5)},
					{Key: "customer", Value: bson.D{{Key: "name", Value: "Bob"}}},
					{Key: "lines", Value: bson.A{bson.D{{Key: "sku", Value: "B2"}, {Key: "n", Value: 1.5}}}}, {Key: "note", Value: "rush"}},
			},
			"shop.customers": nil,
		},
	}
}

// fakeDialer returns a backend and records what it was asked.
type fakeDialer struct {
	mu      sync.Mutex
	backend *fakeBackend
	err     error
	dials   []dialCall
}

type dialCall struct {
	Conn     dbconn.Connection
	Password string
}

func (d *fakeDialer) Dial(_ context.Context, c dbconn.Connection, password string) (Backend, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dials = append(d.dials, dialCall{c, password})
	if d.err != nil {
		return nil, d.err
	}
	return d.backend, nil
}

func (d *fakeDialer) dialList() []dialCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]dialCall(nil), d.dials...)
}

// rig is the Databases extension and the MongoDB extension activated over the fake API, with a hub of their own (no keyring, a state
// folder in a temporary folder).
type rig struct {
	t      *testing.T
	hub    *dbconn.Hub
	dialer *fakeDialer
	fb     *fakeBackend
	dbAPI  *fakeapi.API
	mgAPI  *fakeapi.API
	ext    *Extension
	env    map[string]string
	cancel context.CancelFunc
	folder string
}

func stateHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", root)
	return filepath.Join(root, "state", "emetos")
}

func newRig(t *testing.T) *rig {
	t.Helper()
	state := stateHome(t)
	r := &rig{t: t, fb: fixtureBackend(), env: map[string]string{"MONGO_PW": "s3cret-pw"}, folder: t.TempDir()}
	r.dialer = &fakeDialer{backend: r.fb}
	r.hub = dbconn.NewHub(dbconn.HubOptions{
		StorePath: filepath.Join(state, dbconn.FileName), NoKeyring: true,
		LookupEnv: func(k string) (string, bool) { v, ok := r.env[k]; return v, ok },
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	t.Cleanup(cancel)
	scope := extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: r.folder, ID: r.folder}

	r.ext = FactoryOf(r.hub, Options{Dialer: r.dialer})().(*Extension)
	r.mgAPI = fakeapi.New(t, r.ext.Manifest(), scope)
	if err := r.ext.Activate(ctx, r.mgAPI); err != nil {
		t.Fatal(err)
	}
	dbExt := dbconn.Factory(r.hub)()
	r.dbAPI = fakeapi.New(t, dbExt.Manifest(), scope)
	if err := dbExt.Activate(ctx, r.dbAPI); err != nil {
		t.Fatal(err)
	}
	return r
}

// save adds a connection to the hub as the form does.
func (r *rig) save(c dbconn.Connection) dbconn.Connection {
	r.t.Helper()
	if c.Scope == "" {
		c.Scope = dbconn.ScopeUser
	}
	kept, _, err := r.hub.Save(context.Background(), c, dbconn.PasswordChange{})
	if err != nil {
		r.t.Fatal(err)
	}
	return kept
}

// shop saves a connection to the fixture server, with its password from the environment so that nothing is asked, and connects.
func (r *rig) shop() dbconn.Connection {
	r.t.Helper()
	c := r.save(dbconn.Connection{Name: "Shop", Engine: EngineID, Host: "db.example.com", Port: 27017, User: "app", PasswordEnv: "${env:MONGO_PW}"})
	r.connect(c)
	return c
}

// connect runs the command of the Databases extension and waits for the connection to open.
func (r *rig) connect(c dbconn.Connection) {
	r.t.Helper()
	if err := r.dbAPI.Run(dbconn.Connect, c.ID); err != nil {
		r.t.Fatal(err)
	}
	r.dbAPI.WaitFor("the connection "+c.Name, func() bool { return r.hub.State(c.ID).Status == dbconn.Connected })
}

// children lists what is below a node through the hub, as the view does.
func (r *rig) children(c dbconn.Connection, parent *dbconn.Node) []dbconn.Node {
	r.t.Helper()
	nodes, err := r.hub.Children(context.Background(), c.ID, parent)
	if err != nil {
		r.t.Fatalf("Children: %v", err)
	}
	return nodes
}

// find returns the node with the label among the nodes.
func find(t *testing.T, nodes []dbconn.Node, label string) dbconn.Node {
	t.Helper()
	for _, n := range nodes {
		if n.Label == label {
			return n
		}
	}
	t.Fatalf("no node %q in %v", label, labels(nodes))
	return dbconn.Node{}
}

func labels(nodes []dbconn.Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Label
	}
	return out
}

// wantRoute checks the error is one of this editor's with the route.
func wantRoute(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want the route %s", want)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}

// stateRoot is the folder the tests keep the state of Emetos in (stateHome made it).
func stateRoot(t *testing.T) string {
	t.Helper()
	return filepath.Dir(filepath.Join(os.Getenv("XDG_STATE_HOME"), "emetos"))
}

// readAll returns the content of every file below dir, for the tests that look for a password in what was written.
func readAll(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out[path] = string(data)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}
