package dbconn

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// openNew opens the form of a new PostgreSQL connection and returns its tab and form.
func (r *rig) openNew() (*fakeapi.Tab, *formTab) {
	r.t.Helper()
	r.run(NewConnection, nil)
	tabs := r.api.EditorTabs()
	if len(tabs) == 0 {
		r.t.Fatal("no form tab was opened")
	}
	return tabs[len(tabs)-1], r.ext.forms[len(r.ext.forms)-1]
}

func TestNewConnectionOpensTheFormOfTheOnlyEngine(t *testing.T) {
	r := newRig(t, pgDriver())
	tab, f := r.openNew()
	if tab.Kind() != FormKindID || tab.Title() != "New PostgreSQL Connection" {
		t.Errorf("tab = %q %q", tab.Kind(), tab.Title())
	}
	if f.model.Port != "5432" || f.model.Folder != r.folder {
		t.Errorf("the form starts with port %q, folder %q", f.model.Port, f.model.Folder)
	}
	if !tab.Revealed {
		t.Error("the tab was not shown")
	}
}

func TestNewConnectionAsksWhichEngine(t *testing.T) {
	mongo := &fakeDriver{engines: []Engine{mongoEngine}}
	r := newRig(t, pgDriver(), mongo)
	r.run(NewConnection, nil)
	picks := r.api.Picks()
	if len(picks) != 1 {
		t.Fatalf("picks = %d", len(picks))
	}
	labels := picks[0].Labels()
	if len(labels) != 2 || !strings.Contains(labels[0], "MongoDB") || !strings.Contains(labels[1], "PostgreSQL") {
		t.Errorf("labels = %v", labels)
	}
	picks[0].Choose(0)
	if tabs := r.api.EditorTabs(); len(tabs) != 1 || tabs[0].Title() != "New MongoDB Connection" {
		t.Errorf("tabs = %+v", tabs)
	}
	// a named engine goes straight to its form (the command of another extension, SPEC §10.24)
	r.run(NewConnection, map[string]any{"engine": "postgres"})
	if tabs := r.api.EditorTabs(); len(tabs) != 2 || tabs[1].Title() != "New PostgreSQL Connection" {
		t.Errorf("tabs = %+v", tabs)
	}
	r.run(NewConnection, "redis")
	n := r.api.Notes()
	if !strings.Contains(n[len(n)-1].Message, "redis") {
		t.Errorf("notes = %+v", n)
	}
}

func TestNewConnectionWithNoEngineSaysSo(t *testing.T) {
	r := newRig(t)
	r.run(NewConnection, nil)
	if len(r.api.EditorTabs()) != 0 {
		t.Error("a form was opened")
	}
	if n := r.api.Notes(); len(n) != 1 || !strings.Contains(n[0].Message, "No database extension") {
		t.Errorf("notes = %+v", n)
	}
}

func fill(f *formTab, name string) {
	f.model.Name, f.model.Host, f.model.User, f.model.Database = name, "db.example.com", "me", "app"
}

func TestSavingTheFormWritesTheConnectionAndTheKeyring(t *testing.T) {
	r := newRig(t, pgDriver())
	tab, f := r.openNew()
	fill(f, "Prod")
	f.model.SavePassword = true
	f.model.SetPassword("hunter2-VERY-secret")
	f.submit()
	r.api.WaitFor("the tab to close", func() bool { return tab.Closed })
	conns := r.hub.Store().All()
	if len(conns) != 1 || conns[0].Name != "Prod" || conns[0].Host != "db.example.com" || !conns[0].SavePassword {
		t.Fatalf("connections = %+v", conns)
	}
	if pw, ok, _ := r.kr.Lookup(t.Context(), conns[0].ID); !ok || pw != "hunter2-VERY-secret" {
		t.Errorf("keyring = %q, %v", pw, ok)
	}
	if r.kr.Label(conns[0].ID) != "Emetos: Prod" {
		t.Errorf("label = %q", r.kr.Label(conns[0].ID))
	}
	for file, text := range readAll(t, r.state) {
		if strings.Contains(text, "hunter2") {
			t.Errorf("%s holds the password", file)
		}
	}
	if len(r.api.Notes()) != 0 {
		t.Errorf("notes = %+v", r.api.Notes())
	}
	if got := r.rows(); !slices.Equal(got, []string{"0:Prod"}) {
		t.Errorf("rows = %v", got)
	}
}

func TestAKeyringThatFailsLeavesThePasswordInMemoryAndSaysSo(t *testing.T) {
	r := newRig(t, pgDriver())
	r.kr.Err = errors.New("KeyringFake", "no secret service")
	tab, f := r.openNew()
	fill(f, "Prod")
	f.model.SavePassword = true
	f.model.SetPassword("hunter2-VERY-secret")
	f.submit()
	r.api.WaitFor("the tab to close", func() bool { return tab.Closed })
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "warn" || !strings.Contains(notes[0].Message, "memory for this session only") {
		t.Fatalf("notes = %+v", notes)
	}
	id := r.hub.Store().All()[0].ID
	if !r.hub.Passwords().Remembered(id) {
		t.Error("the password is not held in memory")
	}
	for file, text := range readAll(t, r.state) {
		if strings.Contains(text, "hunter2") {
			t.Errorf("%s holds the password", file)
		}
	}
	// connecting uses the password in memory and asks nothing
	r.run(Connect, id)
	r.api.WaitFor("the connection", func() bool { return r.hub.State(id).Status == Connected })
	if calls := r.drv.connectCalls(); calls[0].Password != "hunter2-VERY-secret" || r.api.LastInput() != nil {
		t.Errorf("calls = %+v, input %v", calls, r.api.LastInput())
	}
}

func TestTheFormShowsWhatIsWrongAndSavesNothing(t *testing.T) {
	r := newRig(t, pgDriver())
	tab, f := r.openNew()
	f.model.Name, f.model.Port = "x", "99999"
	f.submit()
	r.api.Pump()
	if tab.Closed || len(r.hub.Store().All()) != 0 {
		t.Error("an invalid form was saved")
	}
	if !f.showProblems || f.note.Text == "" || !f.note.Bad {
		t.Errorf("note = %+v", f.note)
	}
	if f.problemOf("host") == "" || f.problemOf("port") == "" {
		t.Error("the fields do not show their problems")
	}
}

func TestAnErrorOfTheHubIsShownInTheForm(t *testing.T) {
	r := newRig(t, pgDriver())
	tab, f := r.openNew()
	fill(f, "Prod")
	stop := r.hub.RegisterDriver(&fakeDriver{engines: nil})
	stop()
	// the engine goes away between opening and saving
	r.hub.mu.Lock()
	r.hub.drivers = nil
	r.hub.mu.Unlock()
	f.submit()
	r.api.WaitFor("the answer", func() bool { return f.note.Bad })
	if tab.Closed || !strings.Contains(f.note.Text, "no extension serves") {
		t.Errorf("note = %+v, closed %v", f.note, tab.Closed)
	}
}

func TestEditingAConnectionKeepsItsPasswordUnlessANewOneIsTyped(t *testing.T) {
	r := newRig(t, pgDriver())
	conn := pgConn("prod")
	conn.SavePassword = true
	c := r.save(conn, PasswordChange{Set: true, Value: "first"})
	r.run(EditConnection, c.ID)
	tab := r.api.EditorTabs()[0]
	f := r.ext.forms[0]
	if tab.Title() != "Edit prod" || f.model.ID != c.ID || f.model.Host != "localhost" || !f.model.HadPassword {
		t.Fatalf("title %q, model %+v", tab.Title(), f.model)
	}
	f.model.Name = "prod2"
	f.submit()
	r.api.WaitFor("the tab to close", func() bool { return tab.Closed })
	if pw, _, _ := r.kr.Lookup(t.Context(), c.ID); pw != "first" {
		t.Errorf("the password changed to %q without being typed", pw)
	}
	if got, _ := r.hub.Store().Get(c.ID); got.Name != "prod2" || len(r.hub.Store().All()) != 1 {
		t.Errorf("connection = %+v", got)
	}
	// a new password replaces it
	r.run(EditConnection, c.ID)
	f = r.ext.forms[1]
	f.model.SetPassword("second")
	f.submit()
	r.api.WaitFor("the second tab to close", func() bool { return r.api.EditorTabs()[1].Closed })
	if pw, _, _ := r.kr.Lookup(t.Context(), c.ID); pw != "second" {
		t.Errorf("the password is %q", pw)
	}
	// turning the keyring off removes the secret
	r.run(EditConnection, c.ID)
	f = r.ext.forms[2]
	f.model.SavePassword = false
	f.submit()
	r.api.WaitFor("the third tab to close", func() bool { return r.api.EditorTabs()[2].Closed })
	if r.kr.Len() != 0 {
		t.Error("a connection that stopped saving its password left it in the keyring")
	}
}

func TestTheTestButton(t *testing.T) {
	d := pgDriver()
	r := newRig(t, d)
	_, f := r.openNew()
	fill(f, "Prod")
	f.model.SetPassword("typed")
	f.test()
	r.api.WaitFor("the answer", func() bool { return !f.busy && f.note.Text != "Testing..." })
	if f.note.Bad || !strings.Contains(f.note.Text, "works") {
		t.Errorf("note = %+v", f.note)
	}
	calls := d.connectCalls()
	if len(calls) != 1 || calls[0].Password != "typed" || len(d.closes) != 1 {
		t.Errorf("calls = %+v, closes %v", calls, d.closes)
	}
	if len(r.hub.Store().All()) != 0 {
		t.Error("the test saved the connection")
	}
	d.mu.Lock()
	d.connectErr = errors.New("DriverFake", "connection refused")
	d.mu.Unlock()
	f.test()
	r.api.WaitFor("the failure", func() bool { return f.note.Bad })
	if !strings.Contains(f.note.Text, "connection refused") {
		t.Errorf("note = %+v", f.note)
	}
	// a form with problems is not tested
	f.model.Host = ""
	before := len(d.connectCalls())
	f.test()
	if len(d.connectCalls()) != before || !f.note.Bad {
		t.Error("an invalid form was tested")
	}
}

func TestTheTestButtonUsesTheSavedPasswordOfAnEditedConnection(t *testing.T) {
	r := newRig(t, pgDriver())
	conn := pgConn("prod")
	conn.SavePassword = true
	c := r.save(conn, PasswordChange{Set: true, Value: "saved"})
	r.run(EditConnection, c.ID)
	f := r.ext.forms[0]
	f.test()
	r.api.WaitFor("the answer", func() bool { return !f.busy && f.note.Text != "Testing..." })
	if calls := r.drv.connectCalls(); len(calls) != 1 || calls[0].Password != "saved" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestTheStateOfAFormTabHasNoPasswordAndComesBack(t *testing.T) {
	r := newRig(t, pgDriver())
	tab, f := r.openNew()
	fill(f, "Prod")
	f.model.Options["x"] = "y"
	f.model.SavePassword = true
	f.model.SetPassword("hunter2-VERY-secret")
	state, err := f.tabContentState(tab)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(state), "hunter2") {
		t.Fatalf("the state holds the password: %s", state)
	}
	// the window restarts: the tab is made again from its state, and the password is gone
	r2 := newRig(t, pgDriver())
	back := r2.api.RestoreTab(FormKindID, "tab1", state)
	g := r2.ext.forms[0]
	if back.Title() != "New PostgreSQL Connection" || g.model.Name != "Prod" || g.model.Host != "db.example.com" || !g.model.SavePassword || g.model.Password != "" {
		t.Errorf("restored: title %q, model %+v", back.Title(), g.model)
	}
}

// tabContentState is the state the tab saves.
func (f *formTab) tabContentState(tab *fakeapi.Tab) (json.RawMessage, error) {
	st := tab.State()
	if st == nil {
		return nil, errors.New("NoState", "the tab saved no state")
	}
	return st, nil
}

func TestAFormRestoredBeforeItsExtensionIsActive(t *testing.T) {
	r := newRig(t) // no driver yet
	state, err := json.Marshal(formOpen{Draft: Draft{Engine: "postgres", Name: "Prod", Host: "h"}})
	if err != nil {
		t.Fatal(err)
	}
	r.api.RestoreTab(FormKindID, "tab1", state)
	f := r.ext.forms[0]
	if f.model.Engine.Name != "postgres" {
		t.Errorf("engine = %+v", f.model.Engine)
	}
	stop := r.hub.RegisterDriver(pgDriver())
	defer stop()
	f.syncEngine()
	if f.model.Engine.Name != "PostgreSQL" {
		t.Errorf("after the extension came, engine = %+v", f.model.Engine)
	}
}

func TestACorruptStateIsRefused(t *testing.T) {
	r := newRig(t, pgDriver())
	_, err := r.ext.createForm(r.api.RestoreTab(FormKindID, "x", mustJSON(t, formOpen{Draft: Draft{Engine: "postgres"}})), nil)
	wantRoute(t, err, "Extension.createForm.ConnectionFormStateMissing")
	_, err = r.ext.createForm(r.api.EditorTabs()[0], []byte("{not json"))
	wantRoute(t, err, "Extension.createForm.ConnectionFormStateInvalid")
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCancelClosesTheTab(t *testing.T) {
	r := newRig(t, pgDriver())
	tab, f := r.openNew()
	fill(f, "x")
	f.tab.Close()
	if !tab.Closed || len(r.hub.Store().All()) != 0 {
		t.Error("Cancel saved or did not close")
	}
}

func TestTypingInTheFormFillsTheModelAndSavingNeedsAHost(t *testing.T) {
	r := newRig(t, pgDriver())
	tab, f := r.openNew()
	ts := showForm(t, f, 640, 1100)
	ts.EnterText(inputAt(0), "Prod")
	if f.model.Name != "Prod" || !tab.Dirty() {
		t.Fatalf("name %q, dirty %v", f.model.Name, tab.Dirty())
	}
	ts.Tap(gluttertest.ByText("Save"))
	ts.Pump(time.Millisecond)
	if tab.Closed || !ts.Exists(gluttertest.ByTextContaining("Enter the host")) {
		t.Fatal("a form without a host was saved, or does not say what is missing")
	}
	ts.EnterText(inputAt(1), "db.example.com")
	ts.EnterText(inputAt(3), "me")
	ts.EnterText(inputAt(7), "hunter2-VERY-secret")
	if f.model.Host != "db.example.com" || f.model.User != "me" || f.model.Password != "hunter2-VERY-secret" || !f.model.PasswordSet {
		t.Fatalf("model = %+v", f.model)
	}
	ts.Tap(gluttertest.ByText("Save"))
	r.api.WaitFor("the tab to close", func() bool { return tab.Closed })
	if c := r.hub.Store().All(); len(c) != 1 || c[0].Host != "db.example.com" || c[0].User != "me" {
		t.Fatalf("saved %+v", c)
	}
	for file, text := range readAll(t, r.state) {
		if strings.Contains(text, "hunter2") {
			t.Errorf("%s holds the password", file)
		}
	}
	if pw, ok := r.hub.Passwords().Remembered(r.hub.Store().All()[0].ID), true; !pw || !ok {
		t.Error("a password that was not saved to the keyring is not held for the session")
	}
	// the password the form shows is dots; it is the text the controller holds
	if f.inputs["password"].Value() != "hunter2-VERY-secret" {
		t.Error("the password field lost its text")
	}
}

func TestAPastedURLMovesItsPasswordToThePasswordField(t *testing.T) {
	r := newRig(t, &fakeDriver{engines: []Engine{mongoEngine}})
	_, f := r.openNew()
	ts := showForm(t, f, 640, 1100)
	ts.EnterText(inputAt(1), "mongodb://admin:hunter2-VERY-secret@h1:27017,h2:27017/shop")
	if strings.Contains(f.model.URL, "hunter2") || f.model.Password != "hunter2-VERY-secret" || !f.model.PasswordSet {
		t.Fatalf("URL %q, password %q", f.model.URL, f.model.Password)
	}
	if got := f.inputs["url"].Value(); got != "mongodb://admin@h1:27017,h2:27017/shop" {
		t.Errorf("the URL field shows %q", got)
	}
	if got := f.inputs["password"].Value(); got != "hunter2-VERY-secret" {
		t.Errorf("the password field shows %q", got)
	}
	if !ts.Exists(gluttertest.ByTextContaining("moved to the password field")) {
		t.Error("the form does not say that the password moved")
	}
	state, err := json.Marshal(formOpen{Draft: f.model.Draft()})
	if err != nil || strings.Contains(string(state), "hunter2") {
		t.Errorf("the state of the tab holds the password: %s (%v)", state, err)
	}
}

func TestBrowseFillsAFileField(t *testing.T) {
	r := newRig(t, &fakeDriver{engines: []Engine{sqliteEngine}})
	_, f := r.openNew()
	ts := showForm(t, f, 640, 800)
	r.api.AnswerFileChooser("/home/me/notes.db", nil)
	ts.Tap(gluttertest.ByText("Browse..."))
	r.api.WaitFor("the file", func() bool { return f.model.File != "" })
	ts.Pump(time.Millisecond)
	if f.model.File != "/home/me/notes.db" || f.inputs["file"].Value() != "/home/me/notes.db" {
		t.Errorf("file %q, field %q", f.model.File, f.inputs["file"].Value())
	}
	if len(f.inputs) > 0 && f.inputs["password"] != nil {
		t.Error("an engine with no password shows the field")
	}
}
