package dbconn

import (
	"slices"
	"sync"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// hookDriver is a fakeDriver that also implements the optional interfaces Opener and Refresher.
type hookDriver struct {
	*fakeDriver

	mu        sync.Mutex
	opened    []string
	openConns []Connection
	refreshed []string
}

func (d *hookDriver) Open(c Connection, n Node) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.opened = append(d.opened, n.ID)
	d.openConns = append(d.openConns, c)
}

func (d *hookDriver) Refresh(c Connection) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshed = append(d.refreshed, c.ID)
}

func (d *hookDriver) openedIDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.opened)
}

func (d *hookDriver) refreshedIDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.refreshed)
}

func newHookRig(t *testing.T) (*rig, *hookDriver, Connection) {
	t.Helper()
	d := &hookDriver{fakeDriver: pgDriver()}
	r := newRig(t, d.fakeDriver)
	// the hub serves the engine with the driver that has the hooks
	stop := r.hub.RegisterDriver(d)
	t.Cleanup(stop)
	r.hub.drivers = slices.DeleteFunc(r.hub.drivers, func(e driverEntry) bool { return e.driver == Driver(d.fakeDriver) })
	c := r.save(pgConn("prod"), PasswordChange{})
	r.env["PW"] = "x"
	return r, d, c
}

func (r *rig) connectAndWait(c Connection) {
	r.t.Helper()
	r.run(Connect, c.ID)
	r.api.WaitFor("the password to be asked", func() bool { return r.api.LastInput() != nil })
	r.api.LastInput().Accept("pw")
	r.api.WaitFor("the connection", func() bool { return r.hub.State(c.ID).Status == Connected })
}

func TestOpeningANodeAsksTheDriverThatListedIt(t *testing.T) {
	r, d, c := newHookRig(t)
	r.connectAndWait(c)
	node := Node{ID: "t:users", Label: "users", Kind: "table"}
	r.hub.Open(c.ID, node)
	if got := d.openedIDs(); !slices.Equal(got, []string{"t:users"}) || d.openConns[0].ID != c.ID {
		t.Errorf("opened = %v", got)
	}
	// an unknown connection and a driver without Opener are nothing
	r.hub.Open("nope", node)
	plain := newRig(t, pgDriver())
	pc := plain.save(pgConn("plain"), PasswordChange{})
	plain.hub.Open(pc.ID, node)
	// a connection whose ${env:} reference is not set cannot be expanded: nothing is opened
	bad := pgConn("bad")
	bad.Host = "${env:NOT_SET_ANYWHERE}"
	badc := r.save(bad, PasswordChange{})
	r.hub.Open(badc.ID, node)
	if got := d.openedIDs(); len(got) != 1 {
		t.Errorf("opened = %v", got)
	}
}

func TestEnterOnARowOfTheViewOpensTheNode(t *testing.T) {
	r, d, c := newHookRig(t)
	r.connectAndWait(c)
	r.ext.view.tree.Expand(connKey(c.ID))
	r.api.WaitFor("the databases", func() bool { return len(r.rows()) == 3 })
	nodes := r.ext.view.children[connKey(c.ID)]
	r.ext.view.onOpen(ui.TreeNode{Key: "x", Data: nodeRow{connID: c.ID, node: nodes[0]}})
	if got := d.openedIDs(); !slices.Equal(got, []string{"db:app"}) {
		t.Errorf("opened = %v", got)
	}
	// a connection that is open is not connected again by Enter
	calls := len(d.connectCalls())
	r.ext.view.onOpen(ui.TreeNode{Key: connKey(c.ID), Data: connRow{conn: c}})
	r.api.Pump()
	if got := len(d.connectCalls()); got != calls {
		t.Errorf("connect calls = %d, want %d", got, calls)
	}
}

func TestRefreshCallsTheDriversOfTheOpenConnectionsOnly(t *testing.T) {
	r, d, c := newHookRig(t)
	closed := r.save(pgConn("closed"), PasswordChange{})
	r.connectAndWait(c)
	r.run(Refresh, nil)
	if got := d.refreshedIDs(); !slices.Equal(got, []string{c.ID}) {
		t.Errorf("refreshed = %v, want only the open connection %s (not %s)", got, c.ID, closed.ID)
	}
	// RefreshDrivers on a hub with a connection of an engine nobody serves, or one that left the file
	r.hub.RefreshDrivers()
	if got := d.refreshedIDs(); len(got) != 2 {
		t.Errorf("refreshed = %v", got)
	}
}

func TestReloadListsTheOpenRowsAgainWithoutRefreshing(t *testing.T) {
	r, d, c := newHookRig(t)
	r.connectAndWait(c)
	r.ext.view.tree.Expand(connKey(c.ID))
	r.api.WaitFor("the databases", func() bool { return len(r.rows()) == 3 })
	r.ext.view.tree.Expand(childKey(connKey(c.ID), "db:app"))
	r.api.WaitFor("the tables", func() bool { return len(r.rows()) == 5 })
	before := len(d.listedCalls())
	r.hub.Reload(c.ID)
	r.api.WaitFor("the rows to be listed again", func() bool { return len(d.listedCalls()) >= before+2 && len(r.rows()) == 5 })
	if got := d.refreshedIDs(); len(got) != 0 {
		t.Errorf("Reload called Refresh: %v", got)
	}
	if got := r.rows(); !slices.Equal(got, []string{"0:prod", "1:app", "2:users", "2:orders", "1:postgres"}) {
		t.Errorf("rows = %v", got)
	}
	// the other connections are left alone
	other := r.save(pgConn("other"), PasswordChange{})
	r.hub.Reload(other.ID)
	r.api.Pump()
}

func TestSubscribersOfReloadStopWhenTheyCancel(t *testing.T) {
	r := newRig(t, pgDriver())
	var got []string
	stop := r.hub.SubscribeReload(func(id string) { got = append(got, id) })
	r.hub.Reload("a")
	stop()
	r.hub.Reload("b")
	stop() // twice is nothing
	if !slices.Equal(got, []string{"a"}) {
		t.Errorf("got %v", got)
	}
}

func TestANodeWithATooltipHasAHover(t *testing.T) {
	d := pgDriver()
	d.tree[""][0].Tooltip = "The application database"
	r := newRig(t, d)
	c := r.save(pgConn("prod"), PasswordChange{})
	r.run(Connect, c.ID)
	r.api.WaitFor("the password to be asked", func() bool { return r.api.LastInput() != nil })
	r.api.LastInput().Accept("pw")
	r.api.WaitFor("the connection", func() bool { return r.hub.State(c.ID).Status == Connected })
	r.ext.view.tree.Expand(connKey(c.ID))
	r.api.WaitFor("the databases", func() bool { return len(r.rows()) == 3 })
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 320, Height: 120}, viewWidget(r))
	if n := ts.Count(gluttertest.ByType[*ui.Hover]()); n != 1 {
		t.Errorf("hovers = %d, want 1 (the one node with a tooltip)", n)
	}
}
