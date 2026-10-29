package dbconn

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// fakeDriver is the Driver of a database extension, for the tests: it records what it was asked and answers from tables.
type fakeDriver struct {
	engines []Engine

	mu         sync.Mutex
	connectErr error
	connects   []connectCall
	closes     []string
	// open holds the IDs of the connections that are open.
	open map[string]bool
	// tree maps the ID of a parent node ("" for the connection) to its children.
	tree     map[string][]Node
	childErr error
	listed   []string
	actions  func(c Connection, n *Node) []Action
	// gate, when set, blocks Connect until it is closed.
	gate chan struct{}
}

type connectCall struct {
	Conn     Connection
	Password string
}

func (d *fakeDriver) Engines() []Engine { return d.engines }

func (d *fakeDriver) Connect(ctx context.Context, c Connection, password string) error {
	d.mu.Lock()
	d.connects = append(d.connects, connectCall{c, password})
	gate, err := d.gate, d.connectErr
	d.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err != nil {
		return err
	}
	d.mu.Lock()
	if d.open == nil {
		d.open = map[string]bool{}
	}
	d.open[c.ID] = true
	d.mu.Unlock()
	return nil
}

func (d *fakeDriver) Disconnect(c Connection) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closes = append(d.closes, c.ID)
	delete(d.open, c.ID)
	return nil
}

func (d *fakeDriver) Children(_ context.Context, c Connection, parent *Node) ([]Node, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	id := ""
	if parent != nil {
		id = parent.ID
	}
	d.listed = append(d.listed, c.ID+"/"+id)
	if d.childErr != nil {
		return nil, d.childErr
	}
	return append([]Node(nil), d.tree[id]...), nil
}

func (d *fakeDriver) Actions(c Connection, n *Node) []Action {
	if d.actions == nil {
		return nil
	}
	return d.actions(c, n)
}

// listedCalls returns what the driver was asked to list, as "<connection>/<parent>".
func (d *fakeDriver) listedCalls() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.listed...)
}

// closedIDs returns the IDs of the connections the driver was asked to close.
func (d *fakeDriver) closedIDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.closes...)
}

func (d *fakeDriver) connectCalls() []connectCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]connectCall(nil), d.connects...)
}

// pgDriver is a driver with PostgreSQL and a tree of a database, a schema and tables.
func pgDriver() *fakeDriver {
	return &fakeDriver{
		engines: []Engine{pgEngine},
		tree: map[string][]Node{
			"": {{ID: "db:app", Label: "app", Icon: "database", Expandable: true, Kind: "database"},
				{ID: "db:postgres", Label: "postgres", Icon: "database", Expandable: true, Kind: "database"}},
			"db:app": {{ID: "t:users", Label: "users", Description: "12 rows", Icon: "table", Kind: "table"},
				{ID: "t:orders", Label: "orders", Icon: "table", Kind: "table"}},
		},
	}
}

// rig is the extension "Databases" activated over the fake API with a hub that has a keyring in memory and a folder of its own.
type rig struct {
	t      *testing.T
	hub    *Hub
	kr     *MemoryKeyring
	drv    *fakeDriver
	api    *fakeapi.API
	ext    *Extension
	folder string
	state  string
	env    map[string]string
}

func newRig(t *testing.T, drivers ...*fakeDriver) *rig {
	t.Helper()
	dir := stateHome(t)
	folder := t.TempDir()
	r := &rig{t: t, kr: &MemoryKeyring{}, folder: folder, state: dir, env: map[string]string{}}
	r.hub = NewHub(HubOptions{
		StorePath: filepath.Join(dir, FileName), Keyring: r.kr,
		LookupEnv: func(k string) (string, bool) { v, ok := r.env[k]; return v, ok },
	})
	for _, d := range drivers {
		r.drv = d
		stop := r.hub.RegisterDriver(d)
		t.Cleanup(stop)
	}
	r.activate()
	return r
}

// activate activates the extension in the workspace of the rig (again, to see what a restart does).
func (r *rig) activate() {
	r.t.Helper()
	e := &Extension{hub: r.hub}
	api := fakeapi.New(r.t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: r.folder, ID: r.folder})
	ctx, cancel := context.WithCancel(context.Background())
	r.t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		r.t.Fatal(err)
	}
	r.api, r.ext = api, e
}

// save adds a connection through the hub, as the form does.
func (r *rig) save(c Connection, pw PasswordChange) Connection {
	r.t.Helper()
	kept, _, err := r.hub.Save(context.Background(), c, pw)
	if err != nil {
		r.t.Fatal(err)
	}
	r.api.Pump()
	return kept
}

// run runs a command of the extension.
func (r *rig) run(id string, args any) {
	r.t.Helper()
	if err := r.api.Run(id, args); err != nil {
		r.t.Fatalf("running %s: %v", id, err)
	}
	r.api.Pump()
}

// rows are the labels of the rows of the tree, indented by depth.
func (r *rig) rows() []string {
	var out []string
	for _, n := range r.ext.view.tree.Rows() {
		label := n.Key
		switch d := n.Data.(type) {
		case connRow:
			label = d.conn.Name
		case nodeRow:
			label = d.node.Label
		}
		if n.Placeholder {
			label = "(loading)"
		}
		out = append(out, string(rune('0'+n.Depth))+":"+label)
	}
	return out
}

func pgConn(name string) Connection {
	return Connection{Name: name, Engine: "postgres", Scope: ScopeUser, Host: "localhost", Port: 5432, User: "me", Database: "app"}
}

func fileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
