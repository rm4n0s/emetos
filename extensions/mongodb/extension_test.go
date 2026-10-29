package mongodb

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

func TestTheManifestIsValid(t *testing.T) {
	m := (&Extension{}).Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "emetos.mongodb" || m.Name != "MongoDB" {
		t.Errorf("manifest = %s %s", m.ID, m.Name)
	}
	// two instances are two extensions (the contract), over the same servers
	hub := dbconn.NewHub(dbconn.HubOptions{NoKeyring: true, StorePath: t.TempDir() + "/c.json"})
	f := FactoryOf(hub)
	a, b := f().(*Extension), f().(*Extension)
	if a == b || a.mgr != b.mgr {
		t.Error("the instances must be distinct and share the manager of the servers")
	}
	// the setting is declared with its bounds, the command is in the palette
	var found bool
	for _, s := range m.Contributes.Settings {
		if s.Key == SchemaSampleSizeSetting && s.Type == "integer" && s.Default == 100 && *s.Minimum == 1 && *s.Maximum == 10000 {
			found = true
		}
	}
	if !found {
		t.Errorf("settings = %+v", m.Contributes.Settings)
	}
	var ids []string
	for _, c := range m.Contributes.Commands {
		if c.Title == "" {
			t.Errorf("the command %s has no title", c.ID)
		}
		ids = append(ids, c.ID)
	}
	want := []string{NewPlaygroundCommand, RunCommand, RunAllCommand, CancelCommand, SelectConnectionCommand, OpenCollectionCommand,
		EditDocumentCommand, DeleteDocumentCommand, SaveDocumentCommand, CopyDocumentCommand}
	if strings.Join(ids, " ") != strings.Join(want, " ") {
		t.Errorf("commands = %v, want %v", ids, want)
	}
	// the settings of step 20.2
	byKey := map[string]extension.SettingSpec{}
	for _, s := range m.Contributes.Settings {
		byKey[s.Key] = s
	}
	if s := byKey[PageSizeSetting]; s.Type != "integer" || s.Default != 20 || *s.Minimum != 1 || *s.Maximum != 1000 {
		t.Errorf("%s = %+v", PageSizeSetting, s)
	}
	if s := byKey[ConfirmDestructiveSetting]; s.Type != "boolean" || s.Default != true {
		t.Errorf("%s = %+v", ConfirmDestructiveSetting, s)
	}
	if len(m.Contributes.PanelViews) != 1 || m.Contributes.PanelViews[0].ID != ViewID {
		t.Errorf("panel views = %+v", m.Contributes.PanelViews)
	}
}

func TestTheLanguageOfThePlaygrounds(t *testing.T) {
	m := (&Extension{}).Manifest()
	if m.Contributes.Languages == nil {
		t.Fatal("the extension has no language")
	}
	for _, name := range []string{"package.json", "language-configuration.json", "syntaxes/mongodb.tmLanguage.json"} {
		if _, err := fs.Stat(m.Contributes.Languages, name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTheEngineIsRegisteredWhileTheExtensionIsActive(t *testing.T) {
	r := newRig(t)
	eng, _, ok := r.hub.Engine(EngineID)
	if !ok || eng.Name != "MongoDB" || eng.DefaultPort != 27017 || !slicesEqual(eng.URLSchemes, []string{"mongodb", "mongodb+srv"}) || eng.File {
		t.Fatalf("engine = %+v, %v", eng, ok)
	}
	c := r.shop()
	r.cancel() // the extension is deactivated
	for i := 0; i < 200 && r.fb.closedCount() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if _, _, ok := r.hub.Engine(EngineID); ok {
		t.Error("the engine is still registered after the extension was deactivated")
	}
	if r.fb.closedCount() != 1 {
		t.Errorf("the open server was closed %d times", r.fb.closedCount())
	}
	if st := r.hub.State(c.ID); st.Status != dbconn.Disconnected {
		t.Errorf("state = %+v", st)
	}
}

func slicesEqual(a, b []string) bool { return strings.Join(a, "\x00") == strings.Join(b, "\x00") }

func TestConnectingDialsTheServerWithTheExpandedConnectionAndItsPassword(t *testing.T) {
	r := newRig(t)
	t.Setenv("MONGO_HOST_FOR_TEST", "db.internal")
	c := r.save(dbconn.Connection{Name: "Shop", Engine: EngineID, Host: "${env:MONGO_HOST_FOR_TEST}", Port: 27017, User: "app", PasswordEnv: "${env:MONGO_PW}",
		Options: map[string]string{"authSource": "admin"}})
	r.env["MONGO_HOST_FOR_TEST"] = "db.internal"
	r.connect(c)
	dials := r.dialer.dialList()
	if len(dials) != 1 {
		t.Fatalf("%d dials", len(dials))
	}
	if d := dials[0]; d.Conn.Host != "db.internal" || d.Password != "s3cret-pw" || d.Conn.Options["authSource"] != "admin" || d.Conn.ID != c.ID {
		t.Errorf("dial = %+v", d)
	}
	// the password is in no file of Emetos
	for path, text := range readAll(t, stateRoot(t)) {
		if strings.Contains(text, "s3cret-pw") {
			t.Errorf("the password is in %s", path)
		}
	}
}

func TestAPasswordTypedForTheConnectionIsAskedAndKeptInMemory(t *testing.T) {
	r := newRig(t)
	c := r.save(dbconn.Connection{Name: "Shop", Engine: EngineID, Host: "h", Port: 27017, User: "app"})
	if err := r.dbAPI.Run(dbconn.Connect, c.ID); err != nil {
		t.Fatal(err)
	}
	r.dbAPI.WaitFor("the password box", func() bool { return len(r.dbAPI.Inputs()) == 1 })
	in := r.dbAPI.Inputs()[0]
	if !in.Options().Password {
		t.Error("the password box shows what is typed")
	}
	in.Accept("typed-secret")
	r.dbAPI.WaitFor("the connection", func() bool { return r.hub.State(c.ID).Status == dbconn.Connected })
	if d := r.dialer.dialList(); len(d) != 1 || d[0].Password != "typed-secret" {
		t.Errorf("dials = %+v", d)
	}
	for path, text := range readAll(t, stateRoot(t)) {
		if strings.Contains(text, "typed-secret") {
			t.Errorf("the password is in %s", path)
		}
	}
}

func TestAConnectionThatFailsShowsWhy(t *testing.T) {
	r := newRig(t)
	r.dialer.err = errors.New("FakeAuthFailed", "Authentication failed.")
	c := r.save(dbconn.Connection{Name: "Shop", Engine: EngineID, Host: "h", Port: 27017, PasswordEnv: "${env:MONGO_PW}"})
	if err := r.dbAPI.Run(dbconn.Connect, c.ID); err != nil {
		t.Fatal(err)
	}
	r.dbAPI.WaitFor("the failure", func() bool { return r.hub.State(c.ID).Status == dbconn.Failed })
	if st := r.hub.State(c.ID); st.Err != "Authentication failed." {
		t.Errorf("state = %+v", st)
	}
	if r.fb.closedCount() != 0 {
		t.Error("a server that never opened was closed")
	}
}

func TestDisconnectingClosesTheServerAndPublishesTheEvents(t *testing.T) {
	r := newRig(t)
	var topics []string
	sub, err := r.mgAPI.Events().Subscribe("emetos.mongodb.*", func(e extension.Event) { topics = append(topics, e.Topic) })
	if err != nil {
		t.Fatal(err)
	}
	defer sub()
	c := r.shop()
	r.mgAPI.WaitFor("the connected event", func() bool { return len(topics) == 1 })
	if err := r.dbAPI.Run(dbconn.Disconnect, c.ID); err != nil {
		t.Fatal(err)
	}
	r.mgAPI.WaitFor("the disconnected event", func() bool { return len(topics) == 2 })
	if topics[0] != ConnectedTopic || topics[1] != DisconnectedTopic {
		t.Errorf("topics = %v", topics)
	}
	r.mgAPI.WaitFor("the server to be closed", func() bool { return r.fb.closedCount() == 1 })
	if _, ok := r.ext.mgr.backend(c.ID); ok {
		t.Error("the manager still holds the server")
	}
	// disconnecting a connection that is not open does nothing
	r.ext.mgr.close(c)
	time.Sleep(20 * time.Millisecond)
	if r.fb.closedCount() != 1 {
		t.Errorf("closed %d times", r.fb.closedCount())
	}
}

func TestTestingAnOpenConnectionLeavesItOpen(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	trial := &fakeBackend{}
	r.dialer.backend = trial
	// the form's Test Connection of the edited connection: the hub connects and disconnects with the ID of the open one
	if err := r.hub.Test(context.Background(), c, "pw"); err != nil {
		t.Fatal(err)
	}
	r.mgAPI.WaitFor("the trial to be closed", func() bool { return trial.closedCount() == 1 })
	if r.fb.closedCount() != 0 {
		t.Error("the open server was closed by the test of its connection")
	}
	if b, ok := r.ext.mgr.backend(c.ID); !ok || b != Backend(r.fb) {
		t.Error("the open server is not the one the manager holds")
	}
	r.children(c, nil) // and it still answers
	// two trials at once end one after the other, the open server last
	a, b := &fakeBackend{}, &fakeBackend{}
	r.dialer.backend = a
	if err := r.ext.mgr.open(context.Background(), c, ""); err != nil {
		t.Fatal(err)
	}
	r.dialer.backend = b
	if err := r.ext.mgr.open(context.Background(), c, ""); err != nil {
		t.Fatal(err)
	}
	r.ext.mgr.close(c)
	r.mgAPI.WaitFor("the second trial to be closed", func() bool { return b.closedCount() == 1 })
	r.ext.mgr.close(c)
	r.mgAPI.WaitFor("the first trial to be closed", func() bool { return a.closedCount() == 1 })
	if r.fb.closedCount() != 0 {
		t.Error("the open server was closed before its trials")
	}
	r.ext.mgr.close(c)
	r.mgAPI.WaitFor("the open server to be closed", func() bool { return r.fb.closedCount() == 1 })
}

func TestTestConnectionOpensAndClosesWithoutSaving(t *testing.T) {
	r := newRig(t)
	c := dbconn.Connection{ID: "x", Name: "Try", Engine: EngineID, Scope: dbconn.ScopeUser, Host: "h", Port: 27017}
	if err := r.hub.Test(context.Background(), c, "pw"); err != nil {
		t.Fatal(err)
	}
	if d := r.dialer.dialList(); len(d) != 1 || d[0].Password != "pw" {
		t.Errorf("dials = %+v", d)
	}
	r.mgAPI.WaitFor("the server to be closed", func() bool { return r.fb.closedCount() == 1 })
	if _, ok := r.hub.Store().Get("x"); ok {
		t.Error("Test saved the connection")
	}
}

func TestTheFormChecksAMongoDBConnection(t *testing.T) {
	r := newRig(t)
	eng, _, _ := r.hub.Engine(EngineID)
	cases := []struct {
		name  string
		edit  func(*dbconn.Connection)
		route string // "" is valid
	}{
		{"a host and a port", func(*dbconn.Connection) {}, ""},
		{"a URL", func(c *dbconn.Connection) {
			c.Host, c.Port, c.URL = "", 0, "mongodb://h1:27017,h2:27017/shop?replicaSet=rs"
		}, ""},
		{"an SRV URL", func(c *dbconn.Connection) { c.Host, c.Port, c.URL = "", 0, "mongodb+srv://cluster0.example.net/shop" }, ""},
		{"a URL of another scheme", func(c *dbconn.Connection) { c.Host, c.URL = "", "postgres://h/db" }, "ConnectionURLSchemeInvalid"},
		{"a URL with a password", func(c *dbconn.Connection) { c.Host, c.URL = "", "mongodb://u:pw@h/db" }, "ConnectionURLHasPassword"},
		{"no host", func(c *dbconn.Connection) { c.Host = "" }, "ConnectionHostMissing"},
		{"a mechanism that is not offered", func(c *dbconn.Connection) { c.Options = map[string]string{"authMechanism": "KERBEROS"} }, "ConnectionOptionInvalid"},
		{"a read preference that is not offered", func(c *dbconn.Connection) { c.Options = map[string]string{"readPreference": "closest"} }, "ConnectionOptionInvalid"},
		{"direct connection that is not true or false", func(c *dbconn.Connection) { c.Options = map[string]string{"directConnection": "yes"} }, "ConnectionOptionInvalid"},
		{"every option", func(c *dbconn.Connection) {
			c.Options = map[string]string{"authSource": "admin", "authMechanism": "SCRAM-SHA-256", "replicaSet": "rs0", "directConnection": "true",
				"readPreference": "secondaryPreferred", "tlsCertificateKeyFile": "/etc/ssl/client.pem"}
		}, ""},
		{"an option that is not the engine's", func(c *dbconn.Connection) { c.Options = map[string]string{"sslmode": "require"} }, "ConnectionOptionUnknown"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := dbconn.Connection{ID: "x", Name: "n", Engine: EngineID, Scope: dbconn.ScopeUser, Host: "h", Port: 27017}
			tt.edit(&c)
			err := eng.Validate(c)
			if tt.route == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			wantRoute(t, err, "."+tt.route)
		})
	}
}

func TestAPasswordPastedInTheURLMovesOutOfIt(t *testing.T) {
	r := newRig(t)
	eng, _, _ := r.hub.Engine(EngineID)
	m := dbconn.NewFormModel(eng, r.folder)
	m.Name = "Atlas"
	if !m.SetURL("mongodb+srv://admin:p%40ss@cluster0.example.net/shop?retryWrites=true") {
		t.Fatal("the password was not taken out")
	}
	if m.URL != "mongodb+srv://admin@cluster0.example.net/shop?retryWrites=true" || m.Password != "p@ss" || !m.PasswordSet {
		t.Errorf("url = %q, password = %q (set %v)", m.URL, m.Password, m.PasswordSet)
	}
	m.SavePassword = true
	kept, res, err := r.hub.Save(context.Background(), m.Connection(), m.PasswordChange())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(kept.URL, "p%40ss") || strings.Contains(kept.URL, "p@ss") {
		t.Errorf("the saved URL has the password: %s", kept.URL)
	}
	if res.Saved.In == dbconn.SourceNone {
		t.Error("the password went nowhere")
	}
	for path, text := range readAll(t, stateRoot(t)) {
		if strings.Contains(text, "p@ss") || strings.Contains(text, "p%40ss") {
			t.Errorf("the password is in %s", path)
		}
	}
	// connecting uses the password from the keyring or the memory of the session, not the URL
	c, _ := r.hub.Store().Get(kept.ID)
	r.connect(c)
	if d := r.dialer.dialList(); len(d) != 1 || d[0].Password != "p@ss" || strings.Contains(d[0].Conn.URL, "ss") {
		t.Errorf("dials = %+v", d)
	}
}
