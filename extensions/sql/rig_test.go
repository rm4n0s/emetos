package sql

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// rig is the SQL extension and the extension "Databases" activated in one workspace over fake APIs, sharing one hub, as
// extensions.All makes them.
type rig struct {
	t      *testing.T
	db     *dbconn.Hub
	hub    *Hub
	folder string
	// home is the home folder of the test, where a connection written as ~/file finds its file.
	home string

	dbExt *dbconn.Extension
	dbAPI *fakeapi.API
	ext   *Extension
	api   *fakeapi.API
	// stop ends the activation of the SQL extension.
	stop context.CancelFunc
	// env holds the variables a connection's ${env:NAME} reads, before the process's own.
	env map[string]string
}

// newRig activates the extensions in a folder of their own, with the state and configuration folders isolated.
func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", root)
	r := &rig{t: t, folder: t.TempDir(), home: root, env: map[string]string{}}
	r.db = dbconn.NewHub(dbconn.HubOptions{StorePath: filepath.Join(root, "state", "db-connections.json"), NoKeyring: true,
		LookupEnv: func(name string) (string, bool) {
			if v, ok := r.env[name]; ok {
				return v, true
			}
			return os.LookupEnv(name)
		}})
	r.hub = NewHub(r.db)
	scope := extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: r.folder, ID: r.folder}

	dbExt, ok := dbconn.Factory(r.db)().(*dbconn.Extension)
	if !ok {
		t.Fatal("the Databases factory made something else")
	}
	r.dbExt = dbExt
	r.dbAPI = fakeapi.New(t, dbExt.Manifest(), scope)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := dbExt.Activate(ctx, r.dbAPI); err != nil {
		t.Fatal(err)
	}

	r.ext = FactoryOf(r.hub)().(*Extension)
	r.api = fakeapi.New(t, r.ext.Manifest(), scope)
	sqlCtx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	r.stop = stop
	if err := r.ext.Activate(sqlCtx, r.api); err != nil {
		t.Fatal(err)
	}
	r.dbAPI.Pump()
	return r
}

// save adds a connection through the hub, as the form does, and returns it with its ID.
func (r *rig) save(c dbconn.Connection) dbconn.Connection {
	r.t.Helper()
	kept, _, err := r.db.Save(context.Background(), c, dbconn.PasswordChange{})
	if err != nil {
		r.t.Fatal(err)
	}
	r.dbAPI.Pump()
	return kept
}

// connect runs the Connect command of the Databases view and waits for the connection to open.
func (r *rig) connect(c dbconn.Connection) {
	r.t.Helper()
	if err := r.dbAPI.Run(dbconn.Connect, c.ID); err != nil {
		r.t.Fatal(err)
	}
	r.dbAPI.WaitFor("the connection "+c.Name, func() bool { return r.db.State(c.ID).Status == dbconn.Connected })
}

// expand opens the rows along a path of labels, waiting for each level to be read.
func (r *rig) expand(path ...string) {
	r.t.Helper()
	for i := range path {
		prefix := path[:i+1]
		r.dbAPI.WaitFor("the rows of "+prefix[len(prefix)-1], func() bool { return r.dbExt.ExpandTree(prefix...) })
	}
	r.dbAPI.WaitFor("the rows to be read", func() bool {
		for _, row := range r.rows() {
			if strings.HasSuffix(row, ":(loading)") {
				return false
			}
		}
		return true
	})
}

// rows are the rows of the tree.
func (r *rig) rows() []string { return r.dbExt.TreeRows() }
