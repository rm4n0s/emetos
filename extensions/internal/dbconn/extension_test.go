package dbconn

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

func TestTheManifestIsValid(t *testing.T) {
	m := (&Extension{}).Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if m.Contributes.ViewContainers[0].ID != ContainerID || m.Contributes.Views[0].Container != ContainerID {
		t.Error("the view is not in the container")
	}
	want := []string{NewConnection, EditConnection, DeleteConnection, Connect, Disconnect, Refresh, CollapseAll}
	var got []string
	for _, c := range m.Contributes.Commands {
		got = append(got, c.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %v", got)
	}
}

func TestTheViewListsTheConnectionsOfTheWorkspace(t *testing.T) {
	r := newRig(t, pgDriver())
	r.save(pgConn("beta"), PasswordChange{})
	r.save(pgConn("Alpha"), PasswordChange{})
	ws := pgConn("only here")
	ws.Scope, ws.Workspace = ScopeWorkspace, r.folder
	r.save(ws, PasswordChange{})
	other := pgConn("elsewhere")
	other.Scope, other.Workspace = ScopeWorkspace, "/another/folder"
	r.save(other, PasswordChange{})
	want := []string{"0:Alpha", "0:beta", "0:only here"}
	if got := r.rows(); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestConnectingAsksForThePasswordAndKeepsItUntilDisconnect(t *testing.T) {
	r := newRig(t, pgDriver())
	c := r.save(pgConn("prod"), PasswordChange{})
	r.run(Connect, c.ID)
	r.api.WaitFor("the password to be asked", func() bool { return r.api.LastInput() != nil })
	in := r.api.LastInput()
	if !in.Options().Password {
		t.Error("the password is asked in a box that shows it")
	}
	if !strings.Contains(in.Options().Prompt, "prod") {
		t.Errorf("prompt = %q", in.Options().Prompt)
	}
	if st := r.hub.State(c.ID).Status; st == Connected {
		t.Fatal("connected before the password was given")
	}
	in.Accept("s3cret")
	r.api.WaitFor("the connection", func() bool { return r.hub.State(c.ID).Status == Connected })
	calls := r.drv.connectCalls()
	if len(calls) != 1 || calls[0].Password != "s3cret" || calls[0].Conn.Host != "localhost" {
		t.Fatalf("the driver was called with %+v", calls)
	}
	if !r.hub.Passwords().Remembered(c.ID) {
		t.Error("a typed password is kept in memory while the connection is open")
	}
	if r.kr.Len() != 0 {
		t.Error("a password the user did not ask to save reached the keyring")
	}
	// opening it again does not ask
	r.run(Disconnect, c.ID)
	if r.hub.State(c.ID).Status != Disconnected || r.hub.Passwords().Remembered(c.ID) {
		t.Errorf("after Disconnect: %v, remembered %v", r.hub.State(c.ID), r.hub.Passwords().Remembered(c.ID))
	}
	if !slices.Contains(r.drv.closedIDs(), c.ID) {
		t.Errorf("the driver was not asked to close: %v", r.drv.closedIDs())
	}
	r.run(Connect, c.ID)
	r.api.WaitFor("the password to be asked again", func() bool { return len(r.api.Inputs()) == 2 })
}

func TestACancelledPasswordConnectsNothing(t *testing.T) {
	r := newRig(t, pgDriver())
	c := r.save(pgConn("prod"), PasswordChange{})
	r.run(Connect, c.ID)
	r.api.WaitFor("the password to be asked", func() bool { return r.api.LastInput() != nil })
	r.api.LastInput().Cancel()
	r.api.Pump()
	if len(r.drv.connectCalls()) != 0 || r.hub.State(c.ID).Status != Disconnected {
		t.Error("a cancelled password box connected")
	}
}

func TestAPasswordFromTheKeyringIsNotAsked(t *testing.T) {
	r := newRig(t, pgDriver())
	conn := pgConn("prod")
	conn.SavePassword = true
	c := r.save(conn, PasswordChange{Set: true, Value: "from-keyring"})
	if r.kr.Len() != 1 {
		t.Fatal("the password did not reach the keyring")
	}
	r.run(Connect, c.ID)
	r.api.WaitFor("the connection", func() bool { return r.hub.State(c.ID).Status == Connected })
	if r.api.LastInput() != nil {
		t.Error("the password was asked although the keyring has it")
	}
	if calls := r.drv.connectCalls(); len(calls) != 1 || calls[0].Password != "from-keyring" {
		t.Errorf("calls = %+v", calls)
	}
	r.run(Disconnect, c.ID)
	if r.kr.Len() != 1 {
		t.Error("closing the connection removed a saved password")
	}
}

func TestAPasswordFromTheEnvironment(t *testing.T) {
	r := newRig(t, pgDriver())
	r.env["PGPASSWORD"] = "from-env"
	r.env["DBHOST"] = "db.internal"
	conn := pgConn("env")
	conn.PasswordEnv, conn.Host = "${env:PGPASSWORD}", "${env:DBHOST}"
	c := r.save(conn, PasswordChange{})
	r.run(Connect, c.ID)
	r.api.WaitFor("the connection", func() bool { return r.hub.State(c.ID).Status == Connected })
	calls := r.drv.connectCalls()
	if len(calls) != 1 || calls[0].Password != "from-env" || calls[0].Conn.Host != "db.internal" {
		t.Errorf("calls = %+v", calls)
	}
	if r.api.LastInput() != nil {
		t.Error("the password was asked")
	}
	if got := fileText(t, r.hub.Store().Path()); strings.Contains(got, "from-env") || strings.Contains(got, "db.internal") {
		t.Errorf("the file holds the values of the variables: %s", got)
	}
}

func TestAVariableThatIsNotSetFailsTheConnection(t *testing.T) {
	r := newRig(t, pgDriver())
	conn := pgConn("env")
	conn.PasswordEnv = "${env:NOT_SET_ANYWHERE}"
	c := r.save(conn, PasswordChange{})
	r.run(Connect, c.ID)
	r.api.WaitFor("the failure", func() bool { return r.hub.State(c.ID).Status == Failed })
	if !strings.Contains(r.hub.State(c.ID).Err, "not set") {
		t.Errorf("state = %+v", r.hub.State(c.ID))
	}
	if len(r.drv.connectCalls()) != 0 {
		t.Error("the driver was called")
	}
}

func TestAKeyringThatFailsFallsBackToAskingTheUser(t *testing.T) {
	r := newRig(t, pgDriver())
	conn := pgConn("prod")
	conn.SavePassword = true
	c := r.save(conn, PasswordChange{})
	r.kr.Err = errors.New("KeyringFake", "the keyring is gone")
	r.run(Connect, c.ID)
	r.api.WaitFor("the password to be asked", func() bool { return r.api.LastInput() != nil })
	notes := r.api.Notes()
	if len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Message, "keyring did not answer") {
		t.Errorf("the user was not told: %+v", notes)
	}
	r.api.LastInput().Accept("typed")
	r.api.WaitFor("the connection", func() bool { return r.hub.State(c.ID).Status == Connected })
	if calls := r.drv.connectCalls(); calls[0].Password != "typed" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestAFailedConnectionShowsItsReason(t *testing.T) {
	d := pgDriver()
	d.connectErr = errors.New("DriverFake", "password authentication failed")
	r := newRig(t, d)
	c := r.save(pgConn("prod"), PasswordChange{})
	r.run(Connect, c.ID)
	r.api.WaitFor("the input", func() bool { return r.api.LastInput() != nil })
	r.api.LastInput().Accept("wrong")
	r.api.WaitFor("the failure", func() bool { return r.hub.State(c.ID).Status == Failed })
	if got := r.hub.State(c.ID).Err; got != "password authentication failed" {
		t.Errorf("Err = %q", got)
	}
	notes := r.api.Notes()
	if len(notes) == 0 || notes[len(notes)-1].Severity != "error" || !strings.Contains(notes[len(notes)-1].Message, "password authentication failed") {
		t.Errorf("notes = %+v", notes)
	}
	if r.hub.Passwords().Remembered(c.ID) {
		t.Error("a password that failed was kept")
	}
	// the row shows the reason
	if rows := r.rows(); !slices.Equal(rows, []string{"0:prod"}) {
		t.Errorf("rows = %v", rows)
	}
}

// connected makes a connected connection with a password from the environment.
func (r *rig) connected(name string) Connection {
	r.t.Helper()
	conn := pgConn(name)
	conn.PasswordEnv = "${env:PW}"
	r.env["PW"] = "x"
	c := r.save(conn, PasswordChange{})
	r.run(Connect, c.ID)
	r.api.WaitFor("the connection", func() bool { return r.hub.State(c.ID).Status == Connected })
	return c
}

func TestTheNodesOfAConnectionAreListedWhenOpened(t *testing.T) {
	r := newRig(t, pgDriver())
	c := r.connected("prod")
	v := r.ext.view
	if got := r.rows(); !slices.Equal(got, []string{"0:prod"}) {
		t.Fatalf("rows = %v", got)
	}
	if len(r.drv.listedCalls()) != 0 {
		t.Fatal("the driver listed before the row was opened")
	}
	v.tree.Expand(connKey(c.ID))
	r.api.WaitFor("the databases", func() bool { return len(r.rows()) == 3 })
	if got := r.rows(); !slices.Equal(got, []string{"0:prod", "1:app", "1:postgres"}) {
		t.Fatalf("rows = %v", got)
	}
	v.tree.Expand(childKey(connKey(c.ID), "db:app"))
	r.api.WaitFor("the tables", func() bool { return len(r.rows()) == 5 })
	if got := r.rows(); !slices.Equal(got, []string{"0:prod", "1:app", "2:users", "2:orders", "1:postgres"}) {
		t.Fatalf("rows = %v", got)
	}
	// an unrelated change keeps what was listed and does not ask again
	listed := len(r.drv.listedCalls())
	r.save(pgConn("another"), PasswordChange{})
	r.api.Pump()
	if got := r.rows(); !slices.Equal(got, []string{"0:another", "0:prod", "1:app", "2:users", "2:orders", "1:postgres"}) {
		t.Errorf("rows after another connection = %v", got)
	}
	if len(r.drv.listedCalls()) != listed {
		t.Errorf("the driver listed again: %v", r.drv.listedCalls())
	}
	// Refresh lists again
	r.run(Refresh, nil)
	r.api.WaitFor("the nodes again", func() bool { return len(r.drv.listedCalls()) > listed && len(r.rows()) == 6 })
	// Disconnect drops them
	r.run(Disconnect, c.ID)
	if got := r.rows(); !slices.Equal(got, []string{"0:another", "0:prod"}) {
		t.Errorf("rows after Disconnect = %v", got)
	}
	if len(r.ext.view.children) != 0 {
		t.Errorf("the cache keeps %d lists of a closed connection", len(r.ext.view.children))
	}
}

func TestANodeThatCannotBeListed(t *testing.T) {
	d := pgDriver()
	d.childErr = errors.New("DriverFake", "permission denied")
	r := newRig(t, d)
	c := r.connected("prod")
	r.ext.view.tree.Expand(connKey(c.ID))
	r.api.WaitFor("the warning", func() bool {
		for _, n := range r.api.Notes() {
			if n.Severity == "warn" && strings.Contains(n.Message, "permission denied") {
				return true
			}
		}
		return false
	})
	if got := r.rows(); !slices.Equal(got, []string{"0:prod"}) {
		t.Errorf("rows = %v", got)
	}
}

func TestTheMenuOfARowHasTheActionsOfTheExtension(t *testing.T) {
	d := pgDriver()
	var picked []string
	d.actions = func(c Connection, n *Node) []Action {
		if n == nil {
			return []Action{{Title: "New Query", Group: "a", Run: func() { picked = append(picked, "query:"+c.Name) }}}
		}
		return []Action{{Title: "Select Top 1000", Group: "a", Run: func() { picked = append(picked, "top:"+n.Label) }},
			{Title: "Drop", Group: "b", Run: func() {}}}
	}
	r := newRig(t, d)
	c := r.connected("prod")
	v := r.ext.view
	items := v.driverItems(c.ID, nil)
	if len(items) != 2 || !items[0].Separator || items[1].Label != "New Query" {
		t.Fatalf("items = %+v", items)
	}
	items[1].OnRun()
	n := Node{ID: "t:users", Label: "users"}
	items = v.driverItems(c.ID, &n)
	var labels []string
	for _, it := range items {
		if it.Separator {
			labels = append(labels, "-")
		} else {
			labels = append(labels, it.Label)
		}
	}
	if !slices.Equal(labels, []string{"-", "Select Top 1000", "-", "Drop"}) {
		t.Errorf("labels = %v", labels)
	}
	items[1].OnRun()
	if !slices.Equal(picked, []string{"query:prod", "top:users"}) {
		t.Errorf("picked = %v", picked)
	}
}

func TestAConnectionOfAnEngineWithNoExtensionIsShownButNotUsed(t *testing.T) {
	r := newRig(t) // no driver
	conn := pgConn("orphan")
	if _, err := r.hub.Store().Save(conn); err != nil {
		t.Fatal(err)
	}
	r.ext.view.rebuild()
	if got := r.rows(); !slices.Equal(got, []string{"0:orphan"}) {
		t.Errorf("rows = %v", got)
	}
	c := r.hub.Store().All()[0]
	r.run(Connect, c.ID)
	r.api.WaitFor("the failure", func() bool { return r.hub.State(c.ID).Status == Failed })
	r.run(EditConnection, c.ID)
	notes := r.api.Notes()
	if len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Message, "not active") {
		t.Errorf("notes = %+v", notes)
	}
	// the extension arrives
	stop := r.hub.RegisterDriver(pgDriver())
	defer stop()
	r.api.Pump()
	if len(r.hub.Engines()) != 1 {
		t.Error("the engine is not listed")
	}
}

func TestDeletingAConnection(t *testing.T) {
	r := newRig(t, pgDriver())
	conn := pgConn("prod")
	conn.SavePassword = true
	c := r.save(conn, PasswordChange{Set: true, Value: "pw"})
	r.hub.Passwords().Remember(c.ID, "typed")
	r.run(DeleteConnection, c.ID)
	notes := r.api.Notes()
	if len(notes) != 1 || len(notes[0].Actions) != 1 || notes[0].Actions[0] != "Delete" {
		t.Fatalf("notes = %+v", notes)
	}
	if _, ok := r.hub.Store().Get(c.ID); !ok {
		t.Fatal("deleted before the user said so")
	}
	r.api.RunNoteAction(0, "Delete")
	r.api.WaitFor("the delete", func() bool { _, ok := r.hub.Store().Get(c.ID); return !ok })
	if r.kr.Len() != 0 || r.hub.Passwords().Remembered(c.ID) {
		t.Errorf("the password stays: keyring %d, memory %v", r.kr.Len(), r.hub.Passwords().Remembered(c.ID))
	}
	r.api.WaitFor("the row to go", func() bool { return len(r.rows()) == 0 })
	// a keyring that fails does not keep the connection
	c2 := r.save(conn, PasswordChange{})
	r.kr.Err = errors.New("KeyringFake", "down")
	r.run(DeleteConnection, c2.ID)
	r.api.RunNoteAction(len(r.api.Notes())-1, "Delete")
	r.api.WaitFor("the second delete", func() bool { _, ok := r.hub.Store().Get(c2.ID); return !ok })
	r.api.WaitFor("the warning", func() bool {
		n := r.api.Notes()
		return strings.Contains(n[len(n)-1].Message, "did not remove its password")
	})
}

func TestCommandsAskWhichConnectionWhenGivenNone(t *testing.T) {
	r := newRig(t, pgDriver())
	r.run(EditConnection, nil)
	if n := r.api.Notes(); len(n) != 1 || !strings.Contains(n[0].Message, "no connections") {
		t.Fatalf("notes = %+v", n)
	}
	a := r.save(pgConn("alpha"), PasswordChange{})
	b := r.save(pgConn("beta"), PasswordChange{})
	r.run(Connect, nil)
	picks := r.api.Picks()
	if len(picks) != 1 || len(picks[0].Options().Items) != 2 {
		t.Fatalf("picks = %+v", picks)
	}
	if !strings.Contains(picks[0].Options().Items[1].Label, "beta") || picks[0].Options().Items[1].Description != "localhost:5432" {
		t.Errorf("items = %+v", picks[0].Options().Items)
	}
	picks[0].Choose(1)
	r.api.WaitFor("the password to be asked for beta", func() bool {
		in := r.api.LastInput()
		return in != nil && strings.Contains(in.Options().Prompt, "beta")
	})
	_ = a
	_ = b
	// an ID that is gone is said
	r.run(Connect, "gone")
	n := r.api.Notes()
	if !strings.Contains(n[len(n)-1].Message, "does not exist") {
		t.Errorf("notes = %+v", n)
	}
}

func TestTheStoreErrorIsShown(t *testing.T) {
	dir := stateHome(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/"+FileName, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, kr: &MemoryKeyring{}, folder: t.TempDir()}
	r.hub = NewHub(HubOptions{StorePath: dir + "/" + FileName, Keyring: r.kr})
	r.activate()
	if r.hub.StoreErr() == nil {
		t.Fatal("the hub did not notice the file")
	}
	ts := pumpView(t, r, 320, 120)
	if !hasText(ts, "cannot be used") {
		t.Error("the view does not say that the file is unusable")
	}
}

func TestTheFilterShowsTheMatchingNodesAndTheOnesAboveThem(t *testing.T) {
	r := newRig(t, pgDriver())
	r.env["PW"] = "x"
	conn := pgConn("prod")
	conn.PasswordEnv = "${env:PW}"
	prod := r.save(conn, PasswordChange{})
	r.run(Connect, prod.ID)
	r.api.WaitFor("the connection", func() bool { return r.hub.State(prod.ID).Status == Connected })
	r.ext.view.tree.Expand(connKey(prod.ID))
	r.api.WaitFor("the databases", func() bool { return len(r.rows()) == 3 })
	r.ext.view.tree.Expand(childKey(connKey(prod.ID), "db:app"))
	r.api.WaitFor("the tables", func() bool { return len(r.rows()) == 5 })

	tree := r.ext.view.tree
	tree.OpenFind()
	tree.SetFindPattern("ord")
	if got, want := r.rows(), []string{"0:prod", "1:app", "2:orders"}; !slices.Equal(got, want) {
		t.Errorf("filtered rows = %v, want %v", got, want)
	}
	if st := tree.Find(); st.Mode != ui.FindModeFilter {
		t.Errorf("the find does not filter: %+v", st)
	}
	tree.SetFindPattern("")
	if len(r.rows()) != 5 {
		t.Errorf("rows after the filter = %v", r.rows())
	}
	tree.CloseFind()
}

var _ = context.Background

// guardedDriver is a driver that has work open on its connections.
type guardedDriver struct {
	*fakeDriver
	work string
}

func (g *guardedDriver) OpenWork(Connection) string { return g.work }

func TestADriverWithOpenWorkIsAskedAboutBeforeItDisconnects(t *testing.T) {
	g := &guardedDriver{fakeDriver: pgDriver(), work: "An editor has a transaction open."}
	r := newRig(t)
	t.Cleanup(r.hub.RegisterDriver(g))
	c := r.save(pgConn("prod"), PasswordChange{})
	r.hub.Passwords().Remember(c.ID, "pw")
	r.run(Connect, c.ID)
	r.api.WaitFor("the connection", func() bool { return r.hub.State(c.ID).Status == Connected })

	r.run(Disconnect, c.ID)
	pick := r.api.LastPick()
	if pick == nil || pick.Options().Placeholder != g.work || len(pick.Labels()) != 2 {
		t.Fatalf("pick = %+v", pick)
	}
	if r.hub.State(c.ID).Status != Connected {
		t.Fatal("it disconnected before the answer")
	}
	pick.Choose(1) // Cancel
	if r.hub.State(c.ID).Status != Connected || len(g.closedIDs()) != 0 {
		t.Error("Cancel disconnected")
	}
	r.run(Disconnect, c.ID)
	r.api.LastPick().Choose(0)
	if r.hub.State(c.ID).Status != Disconnected || len(g.closedIDs()) != 1 {
		t.Errorf("state %v, closed %v", r.hub.State(c.ID), g.closedIDs())
	}
	// nothing open: no question
	g.work = ""
	r.hub.Passwords().Remember(c.ID, "pw")
	r.run(Connect, c.ID)
	r.api.WaitFor("the connection again", func() bool { return r.hub.State(c.ID).Status == Connected })
	picks := len(r.api.Picks())
	r.run(Disconnect, c.ID)
	if len(r.api.Picks()) != picks || r.hub.State(c.ID).Status != Disconnected {
		t.Error("a driver with nothing open asked")
	}
}
