package redis

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// rig is the extension activated over the fake API, with a hub that has a fake Redis behind a fake dialer and a folder of its own.
type rig struct {
	t      *testing.T
	hub    *dbconn.Hub
	dial   *fakeDialer
	store  *fakeServer
	api    *fakeapi.API
	ext    *Extension
	cancel context.CancelFunc
	folder string
	conn   dbconn.Connection
	// password is the one the connection is opened with, for a server that wants one.
	password string
}

// isolate points every XDG folder of the test at a folder of its own.
func isolate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", root)
	return root
}

// newRig activates the extension and saves a connection to the fake server. store is the server behind it (the fixture when nil).
func newRig(t *testing.T, store *fakeServer, mods ...func(*dbconn.Connection)) *rig {
	t.Helper()
	root := isolate(t)
	if store == nil {
		store = fixtureServer()
	}
	r := &rig{t: t, store: store, folder: t.TempDir()}
	r.hub = dbconn.NewHub(dbconn.HubOptions{StorePath: filepath.Join(root, "db-connections.json"), NoKeyring: true})
	r.dial = &fakeDialer{servers: map[string]*fakeServer{"redis.local:6379": store}}
	e := &Extension{hub: r.hub, dialer: r.dial}
	r.api = fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: r.folder, ID: r.folder})
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	t.Cleanup(cancel)
	if err := e.Activate(ctx, r.api); err != nil {
		t.Fatal(err)
	}
	r.ext = e
	c := dbconn.Connection{Name: "cache", Engine: EngineID, Scope: dbconn.ScopeUser, Host: "redis.local", Port: 6379,
		Options: map[string]string{OptSeparator: ":"}}
	for _, m := range mods {
		m(&c)
	}
	saved, _, err := r.hub.Save(context.Background(), c, dbconn.PasswordChange{})
	if err != nil {
		t.Fatal(err)
	}
	r.conn = saved
	return r
}

// connect opens the connection through the hub, as the Databases view does.
func (r *rig) connect() {
	r.t.Helper()
	p, err := r.hub.Prepare(context.Background(), r.conn.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	p.Password = dbconn.Resolved{Password: r.password, Source: dbconn.SourceMemory}
	if err := r.hub.Connect(context.Background(), p, false); err != nil {
		r.t.Fatal(err)
	}
}

// children lists the nodes below a node (nil: the databases) through the hub.
func (r *rig) children(parent *dbconn.Node) []dbconn.Node {
	r.t.Helper()
	nodes, err := r.hub.Children(context.Background(), r.conn.ID, parent)
	if err != nil {
		r.t.Fatal(err)
	}
	return nodes
}

// find returns the node with the label among nodes.
func find(t *testing.T, nodes []dbconn.Node, label string) dbconn.Node {
	t.Helper()
	for _, n := range nodes {
		if n.Label == label {
			return n
		}
	}
	t.Fatalf("no node %q among %q", label, labels(nodes))
	return dbconn.Node{}
}

func labels(nodes []dbconn.Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Label
	}
	return out
}

// actionTitles are the titles of the context menu of a node.
func (r *rig) actionTitles(n *dbconn.Node) []string {
	var out []string
	for _, a := range r.hub.Actions(r.conn.ID, n) {
		out = append(out, a.Title)
	}
	return out
}

// action runs the entry of the context menu of a node with the title.
func (r *rig) action(n *dbconn.Node, title string) {
	r.t.Helper()
	for _, a := range r.hub.Actions(r.conn.ID, n) {
		if a.Title == title {
			a.Run()
			r.api.Pump()
			return
		}
	}
	r.t.Fatalf("no menu entry %q among %q", title, r.actionTitles(n))
}

// settleNote waits for a notification that contains the text.
func (r *rig) waitNote(text string) fakeapi.Note {
	r.t.Helper()
	var found fakeapi.Note
	r.api.WaitFor("a notification with "+text, func() bool {
		for _, n := range r.api.Notes() {
			if strings.Contains(n.Message, text) {
				found = n
				return true
			}
		}
		return false
	})
	return found
}

// tabs are the key editor tabs the extension made, in order.
func (r *rig) keyTabs() []*keyTab { return slices.Clone(r.ext.keyTabs) }

// openKeyTab opens a key and waits until it is loaded.
func (r *rig) openKeyTab(db int, key string) *keyTab {
	r.t.Helper()
	r.ext.openKey(KeyRef{Conn: r.conn.ID, DB: db, Key: key})
	var tab *keyTab
	r.api.WaitFor("the key editor of "+key, func() bool {
		for _, t := range r.ext.keyTabs {
			if t.ref.Key == key && t.loaded && !t.loading {
				tab = t
				return true
			}
		}
		return false
	})
	return tab
}
