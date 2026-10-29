package dbconn

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

func newHub(t *testing.T, drivers ...Driver) (*Hub, *MemoryKeyring) {
	t.Helper()
	dir := stateHome(t)
	kr := &MemoryKeyring{}
	h := NewHub(HubOptions{StorePath: filepath.Join(dir, FileName), Keyring: kr, LookupEnv: func(string) (string, bool) { return "", false }})
	for _, d := range drivers {
		t.Cleanup(h.RegisterDriver(d))
	}
	return h, kr
}

func TestTheHubReadsNothingUntilItIsAsked(t *testing.T) {
	dir := stateHome(t)
	h := NewHub(HubOptions{})
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("NewHub touched the state folder: %v", err)
	}
	// the first use finds the file where the state folder of the environment says
	if got := h.Store().Path(); got != filepath.Join(dir, FileName) {
		t.Errorf("Path = %q", got)
	}
	if h.StoreErr() != nil {
		t.Errorf("StoreErr = %v", h.StoreErr())
	}
	// the state folder cannot be found: the hub has no connections and says why
	t.Setenv("XDG_STATE_HOME", "relative/path")
	t.Setenv("HOME", "")
	h2 := NewHub(HubOptions{})
	wantRoute(t, h2.StoreErr(), "StateFileDirUnknown")
	if len(h2.Store().All()) != 0 {
		t.Error("connections from nowhere")
	}
	_, err := h2.Store().Save(pgConn("x"))
	wantRoute(t, err, "StateFileDirUnknown")
}

func TestEnginesAreListedByNameOnceEach(t *testing.T) {
	a := &fakeDriver{engines: []Engine{{ID: "z", Name: "Zeta"}, {ID: "p", Name: "Postgres"}}}
	b := &fakeDriver{engines: []Engine{{ID: "p", Name: "Postgres again"}, {ID: "a", Name: "Alpha"}}}
	h, _ := newHub(t, a, b)
	var names []string
	for _, e := range h.Engines() {
		names = append(names, e.Name)
	}
	if !slices.Equal(names, []string{"Alpha", "Postgres", "Zeta"}) {
		t.Errorf("engines = %v", names)
	}
	if e, d, ok := h.Engine("p"); !ok || e.Name != "Postgres" || d != Driver(a) {
		t.Errorf("Engine(p) = %+v, %v, %v: the first driver keeps an engine", e, d, ok)
	}
	if _, _, ok := h.Engine("nope"); ok {
		t.Error("an engine nobody serves")
	}
}

func TestSubscribersAreToldOfEveryChange(t *testing.T) {
	h, _ := newHub(t)
	var n atomic.Int32
	cancel := h.Subscribe(func() { n.Add(1) })
	stop := h.RegisterDriver(pgDriver())
	if n.Load() != 1 {
		t.Errorf("registering a driver: %d calls", n.Load())
	}
	c, _, err := h.Save(t.Context(), pgConn("a"), PasswordChange{})
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Errorf("saving: %d calls", n.Load())
	}
	h.setState(c.ID, State{Status: Connecting})
	if n.Load() != 3 {
		t.Errorf("a state: %d calls", n.Load())
	}
	cancel()
	stop()
	if n.Load() != 3 {
		t.Errorf("a cancelled subscriber was called: %d", n.Load())
	}
}

func TestSavingThroughTheHub(t *testing.T) {
	h, kr := newHub(t, pgDriver())
	ctx := t.Context()
	bad := pgConn("a")
	bad.Engine = "nothing"
	_, _, err := h.Save(ctx, bad, PasswordChange{})
	wantRoute(t, err, "Hub.Save.ConnectionEngineUnknown")
	noHost := pgConn("a")
	noHost.Host = ""
	_, _, err = h.Save(ctx, noHost, PasswordChange{})
	wantRoute(t, err, "Engine.Validate.ConnectionHostMissing")
	if len(h.Store().All()) != 0 {
		t.Error("an invalid connection was kept")
	}
	// an engine with no password drops what it cannot have
	lite := &fakeDriver{engines: []Engine{sqliteEngine}}
	t.Cleanup(h.RegisterDriver(lite))
	c, res, err := h.Save(ctx, Connection{Name: "n", Engine: "sqlite", Scope: ScopeUser, File: "/tmp/a.db", SavePassword: true, PasswordEnv: "${env:X}"}, PasswordChange{Set: true, Value: "x"})
	if err != nil || c.SavePassword || c.PasswordEnv != "" || res.Saved.In != SourceNone || kr.Len() != 0 {
		t.Errorf("sqlite connection = %+v, %+v, %v, keyring %d", c, res, err, kr.Len())
	}
	// a keyring entry is removed when the connection stops saving
	conn := pgConn("b")
	conn.SavePassword = true
	b, res, err := h.Save(ctx, conn, PasswordChange{Set: true, Value: "pw"})
	if err != nil || res.Saved.In != SourceKeyring || kr.Len() != 1 {
		t.Fatalf("Save = %+v, %v, keyring %d", res, err, kr.Len())
	}
	b.SavePassword = false
	if _, res, err = h.Save(ctx, b, PasswordChange{}); err != nil || res.KeyringErr != nil || kr.Len() != 0 {
		t.Errorf("Save = %+v, %v, keyring %d", res, err, kr.Len())
	}
	// and a failing keyring is reported
	b.SavePassword = true
	if _, _, err = h.Save(ctx, b, PasswordChange{Set: true, Value: "pw"}); err != nil {
		t.Fatal(err)
	}
	kr.Err = errors.New("KeyringFake", "down")
	b.SavePassword = false
	if _, res, err = h.Save(ctx, b, PasswordChange{}); err != nil || res.KeyringErr == nil {
		t.Errorf("Save = %+v, %v", res, err)
	}
}

func TestPrepareConnectAndDisconnect(t *testing.T) {
	d := pgDriver()
	h, _ := newHub(t, d)
	ctx := t.Context()
	_, err := h.Prepare(ctx, "gone")
	wantRoute(t, err, "Hub.Prepare.ConnectionNotFound")
	orphan := pgConn("o")
	orphan.Engine = "mysql"
	if _, err := h.Store().Save(orphan); err != nil {
		t.Fatal(err)
	}
	_, err = h.Prepare(ctx, h.Store().All()[0].ID)
	wantRoute(t, err, "Hub.Prepare.ConnectionEngineUnknown")

	c, _, err := h.Save(ctx, pgConn("a"), PasswordChange{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.Prepare(ctx, c.ID)
	if err != nil || !p.Ask || p.Connection.Host != "localhost" || p.Engine.ID != "postgres" {
		t.Fatalf("Prepare = %+v, %v", p, err)
	}
	p.Password = Resolved{Password: "typed", Source: SourceMemory}
	if err := h.Connect(ctx, p, true); err != nil {
		t.Fatal(err)
	}
	if h.State(c.ID).Status != Connected || !h.Passwords().Remembered(c.ID) {
		t.Errorf("state %+v, remembered %v", h.State(c.ID), h.Passwords().Remembered(c.ID))
	}
	// a connection that is not found in the store
	wantRoute(t, h.Disconnect("gone"), "Hub.Disconnect.ConnectionNotFound")
	if err := h.Disconnect(c.ID); err != nil || h.State(c.ID).Status != Disconnected || h.Passwords().Remembered(c.ID) {
		t.Errorf("Disconnect = %v, state %+v", err, h.State(c.ID))
	}
	// a driver that fails
	d.connectErr = errors.New("DriverFake", "no route to host")
	p.Password = Resolved{Password: "x", Source: SourceMemory}
	err = h.Connect(ctx, p, true)
	if err == nil || h.State(c.ID) != (State{Status: Failed, Err: "no route to host"}) || h.Passwords().Remembered(c.ID) {
		t.Errorf("Connect = %v, state %+v", err, h.State(c.ID))
	}
}

func TestTestOpensAndClosesWithoutKeepingAState(t *testing.T) {
	d := pgDriver()
	h, _ := newHub(t, d)
	ctx := t.Context()
	good := pgConn("a")
	if err := h.Test(ctx, good, "pw"); err != nil {
		t.Fatal(err)
	}
	if calls := d.connectCalls(); len(calls) != 1 || calls[0].Password != "pw" || len(d.closes) != 1 {
		t.Errorf("calls %+v, closes %v", calls, d.closes)
	}
	unknown := good
	unknown.Engine = "nothing"
	wantRoute(t, h.Test(ctx, unknown, ""), "Hub.Test.ConnectionEngineUnknown")
	bad := good
	bad.Host = ""
	wantRoute(t, h.Test(ctx, bad, ""), "Engine.Validate.ConnectionHostMissing")
	unset := good
	unset.Database = "${env:NOPE}"
	wantRoute(t, h.Test(ctx, unset, ""), "ExpandEnv.ConnectionEnvUnset")
	d.connectErr = errors.New("DriverFake", "refused")
	if err := h.Test(ctx, good, ""); err == nil {
		t.Error("a failing driver passed the test")
	}
}

func TestChildrenAndActionsGoToTheDriver(t *testing.T) {
	d := pgDriver()
	d.actions = func(c Connection, n *Node) []Action { return []Action{{Title: c.Name}} }
	h, _ := newHub(t, d)
	ctx := t.Context()
	c, _, err := h.Save(ctx, pgConn("a"), PasswordChange{})
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := h.Children(ctx, c.ID, nil)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("Children = %v, %v", nodes, err)
	}
	if acts := h.Actions(c.ID, nil); len(acts) != 1 || acts[0].Title != "a" {
		t.Errorf("Actions = %+v", acts)
	}
	_, err = h.Children(ctx, "gone", nil)
	wantRoute(t, err, "Hub.Children.ConnectionNotFound")
	if h.Actions("gone", nil) != nil {
		t.Error("actions of a connection that is not there")
	}
	orphan := pgConn("o")
	orphan.Engine = "mysql"
	o, err := h.Store().Save(orphan)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.Children(ctx, o.ID, nil)
	wantRoute(t, err, "Hub.Children.ConnectionEngineUnknown")
	if h.Actions(o.ID, nil) != nil {
		t.Error("actions of an engine nobody serves")
	}
	env := pgConn("e")
	env.Host = "${env:NOPE}"
	e, err := h.Store().Save(env)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.Children(ctx, e.ID, nil)
	wantRoute(t, err, "ExpandEnv.ConnectionEnvUnset")
}

func TestDeletingThroughTheHub(t *testing.T) {
	d := pgDriver()
	h, kr := newHub(t, d)
	ctx := t.Context()
	conn := pgConn("a")
	conn.SavePassword = true
	c, _, err := h.Save(ctx, conn, PasswordChange{Set: true, Value: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.Prepare(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Connect(ctx, p, false); err != nil {
		t.Fatal(err)
	}
	keyringErr, err := h.Delete(ctx, c.ID)
	if err != nil || keyringErr != nil || kr.Len() != 0 || h.State(c.ID) != (State{}) || len(d.closes) != 1 {
		t.Errorf("Delete = %v, %v; keyring %d, state %+v, closes %v", keyringErr, err, kr.Len(), h.State(c.ID), d.closes)
	}
	_, err = h.Delete(ctx, c.ID)
	wantRoute(t, err, "Hub.Delete.ConnectionNotFound")
}

func TestARemovedDriverClosesItsConnections(t *testing.T) {
	d := pgDriver()
	h, _ := newHub(t)
	stop := h.RegisterDriver(d)
	ctx := context.Background()
	c, _, err := h.Save(ctx, pgConn("a"), PasswordChange{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.Prepare(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Connect(ctx, p, false); err != nil {
		t.Fatal(err)
	}
	stop()
	stop() // twice is once
	if h.State(c.ID) != (State{}) || len(d.closes) != 1 || len(h.Engines()) != 0 {
		t.Errorf("state %+v, closes %v, engines %v", h.State(c.ID), d.closes, h.Engines())
	}
}

// apiDriver is a fakeDriver whose entries need the API of the window tab.
type apiDriver struct {
	*fakeDriver
	seen []extension.API
}

// ActionsIn implements APIDriver: the entry is named after the tab it was made for.
func (d *apiDriver) ActionsIn(api extension.API, c Connection, _ *Node) []Action {
	d.seen = append(d.seen, api)
	return []Action{{Title: "in " + api.Scope().Name + " for " + c.Name}}
}

func TestAnAPIDriverIsGivenTheAPIOfTheTabTheMenuWasOpenedIn(t *testing.T) {
	d := &apiDriver{fakeDriver: pgDriver()}
	d.actions = func(Connection, *Node) []Action { return []Action{{Title: "without an API"}} }
	h, _ := newHub(t, d)
	c, _, err := h.Save(t.Context(), pgConn("a"), PasswordChange{})
	if err != nil {
		t.Fatal(err)
	}
	api := fakeapi.New(t, extension.Manifest{ID: "emetos.x", Name: "X", Version: "1"}, extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "tab two"})
	if acts := h.ActionsIn(api, c.ID, nil); len(acts) != 1 || acts[0].Title != "in tab two for a" {
		t.Errorf("ActionsIn = %+v", acts)
	}
	if acts := h.Actions(c.ID, nil); len(acts) != 1 || acts[0].Title != "without an API" {
		t.Errorf("Actions = %+v", acts)
	}
	// a driver that is not an APIDriver answers as Actions does
	plain := pgDriver()
	plain.actions = func(Connection, *Node) []Action { return []Action{{Title: "plain"}} }
	h2, _ := newHub(t, plain)
	c2, _, err := h2.Save(t.Context(), pgConn("b"), PasswordChange{})
	if err != nil {
		t.Fatal(err)
	}
	if acts := h2.ActionsIn(api, c2.ID, nil); len(acts) != 1 || acts[0].Title != "plain" {
		t.Errorf("ActionsIn of a plain driver = %+v", acts)
	}
	if h.ActionsIn(api, "gone", nil) != nil {
		t.Error("actions of a connection that is not there")
	}
	orphan := pgConn("o")
	orphan.Engine = "mysql"
	o, err := h.Store().Save(orphan)
	if err != nil {
		t.Fatal(err)
	}
	if h.ActionsIn(api, o.ID, nil) != nil {
		t.Error("actions of an engine nobody serves")
	}
}

func TestADriverRegisteredByTwoTabsKeepsItsConnectionsUntilTheLastTabGoes(t *testing.T) {
	d := pgDriver()
	h, _ := newHub(t)
	stopFirst := h.RegisterDriver(d)
	stopSecond := h.RegisterDriver(d)
	ctx := context.Background()
	c, _, err := h.Save(ctx, pgConn("a"), PasswordChange{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.Prepare(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Connect(ctx, p, false); err != nil {
		t.Fatal(err)
	}
	stopFirst()
	if h.State(c.ID).Status != Connected || len(d.closes) != 0 || len(h.Engines()) != 1 {
		t.Errorf("after the first tab: state %+v, closes %v, engines %d", h.State(c.ID), d.closes, len(h.Engines()))
	}
	stopSecond()
	if h.State(c.ID) != (State{}) || len(d.closes) != 1 || len(h.Engines()) != 0 {
		t.Errorf("after the second tab: state %+v, closes %v, engines %d", h.State(c.ID), d.closes, len(h.Engines()))
	}
}

func TestStatusNames(t *testing.T) {
	for s, want := range map[Status]string{Disconnected: "disconnected", Connecting: "connecting", Connected: "connected", Failed: "failed"} {
		if s.String() != want {
			t.Errorf("%d = %q", s, s.String())
		}
	}
}
