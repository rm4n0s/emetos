package sql

import (
	"context"
	"encoding/json"
	"github.com/rm4n0s/errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

func TestTheManifestIsALanguagePackWithACommand(t *testing.T) {
	m := FactoryOf(NewHub(dbconn.NewHub(dbconn.HubOptions{NoKeyring: true})))().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "emetos.sql" || m.Name != "SQL Databases" || m.Contributes.Languages == nil {
		t.Errorf("manifest = %+v", m)
	}
	if len(m.ActivationEvents) != 1 || m.ActivationEvents[0] != extension.OnStartup {
		t.Errorf("activation = %v", m.ActivationEvents)
	}
	if len(m.Contributes.Commands) != 1 || m.Contributes.Commands[0].ID != NewQuery {
		t.Errorf("commands = %+v", m.Contributes.Commands)
	}
	f := FactoryOf(NewHub(dbconn.NewHub(dbconn.HubOptions{NoKeyring: true})))
	if f() == f() {
		t.Error("the factory made the same extension twice")
	}
}

func TestTheEnginesAreInTheListOfTheNewConnectionForm(t *testing.T) {
	r := newRig(t)
	var names []string
	for _, e := range r.db.Engines() {
		names = append(names, e.Name)
	}
	if want := []string{"MySQL / MariaDB", "PostgreSQL", "SQL Server", "SQLite"}; !reflect.DeepEqual(names, want) {
		t.Errorf("engines = %v, want %v", names, want)
	}
	// the hub reads no connection file before a view is shown
	if err := r.db.StoreErr(); err != nil {
		t.Errorf("store: %v", err)
	}
}

func TestTheExplorerShowsASQLiteFileThroughTheDatabasesView(t *testing.T) {
	r := newRig(t)
	c := r.save(sqliteConn(makeFixture(t)))
	r.connect(c)
	if got, want := r.rows(), []string{"0:fixture"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	r.expand("fixture", "main", "Tables")
	want := []string{"0:fixture", "1:main", "2:Tables", "3:authors", "3:books", "3:log", "3:tags", "2:Views"}
	if got := r.rows(); !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	r.expand("fixture", "main", "Tables", "books", "Columns")
	got := r.rows()
	for _, wantRow := range []string{"4:Columns", "5:id", "5:author_id", "5:title", "4:Indexes", "4:Foreign Keys"} {
		found := false
		for _, row := range got {
			found = found || row == wantRow
		}
		if !found {
			t.Errorf("no row %q in %v", wantRow, got)
		}
	}
	if len(r.dbAPI.Inputs()) != 0 {
		t.Error("a SQLite file asked for a password")
	}
}

func TestDisconnectingClosesThePoolsAndCollapsesTheTree(t *testing.T) {
	r := newRig(t)
	c := r.save(sqliteConn(makeFixture(t)))
	r.connect(c)
	r.expand("fixture", "main")
	if !r.hub.Service().IsOpen(c.ID) {
		t.Fatal("not open")
	}
	if err := r.dbAPI.Run(dbconn.Disconnect, c.ID); err != nil {
		t.Fatal(err)
	}
	r.dbAPI.Pump()
	if r.hub.Service().IsOpen(c.ID) || r.db.State(c.ID).Status != dbconn.Disconnected {
		t.Error("the connection is still open")
	}
	if got := r.rows(); !reflect.DeepEqual(got, []string{"0:fixture"}) {
		t.Errorf("rows = %v", got)
	}
}

func TestAConnectionThatFailsIsShownAsFailed(t *testing.T) {
	r := newRig(t)
	c := r.save(dbconn.Connection{Name: "gone", Engine: "sqlite", Scope: dbconn.ScopeUser, File: r.folder + "/gone.db"})
	if err := r.dbAPI.Run(dbconn.Connect, c.ID); err != nil {
		t.Fatal(err)
	}
	r.dbAPI.WaitFor("the failure", func() bool { return r.db.State(c.ID).Status == dbconn.Failed })
	if st := r.db.State(c.ID); !strings.Contains(st.Err, "cannot connect") {
		t.Errorf("state = %+v", st)
	}
	if notes := r.dbAPI.Notes(); len(notes) == 0 || notes[len(notes)-1].Severity == "" {
		t.Errorf("notes = %+v", notes)
	}
}

// publishedTopics lists the topics and connection IDs of the events a fake API recorded.
func publishedTopics(t *testing.T, api *fakeapi.API) []string {
	t.Helper()
	var out []string
	for _, p := range api.PublishedEvents() {
		var v struct{ Connection string }
		if err := json.Unmarshal(p.Data, &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, p.Topic+" "+v.Connection)
	}
	return out
}

func TestEventsTellWhenAConnectionOpensAndCloses(t *testing.T) {
	r := newRig(t)
	c := r.save(sqliteConn(makeFixture(t)))
	r.connect(c)
	r.api.Pump()
	if got, want := publishedTopics(t, r.api), []string{"emetos.sql.connected " + c.ID}; !reflect.DeepEqual(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	if err := r.dbAPI.Run(dbconn.Disconnect, c.ID); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	if got, want := publishedTopics(t, r.api), []string{"emetos.sql.connected " + c.ID, "emetos.sql.disconnected " + c.ID}; !reflect.DeepEqual(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	// a payload carries the ID and nothing else: no name, no host, no password
	for _, p := range r.api.PublishedEvents() {
		if string(p.Data) != `{"connection":"`+c.ID+`"}` {
			t.Errorf("payload = %s", p.Data)
		}
	}
}

// node finds a node of the explorer by its path of labels.
func (r *rig) node(c dbconn.Connection, path ...string) *dbconn.Node {
	r.t.Helper()
	svc := r.hub.Service()
	var n *dbconn.Node
	for _, label := range path {
		next := child(r.t, svc, c, n, label)
		n = &next
	}
	return n
}

// titles lists the titles of the entries of a context menu.
func titles(actions []dbconn.Action) []string {
	out := make([]string, len(actions))
	for i, a := range actions {
		out[i] = a.Title
	}
	return out
}

// run presses the entry with the title.
func run(t *testing.T, actions []dbconn.Action, title string) {
	t.Helper()
	for _, a := range actions {
		if a.Title == title {
			a.Run()
			return
		}
	}
	t.Fatalf("no entry %q in %v", title, titles(actions))
}

func TestTheContextMenusOfTheExplorer(t *testing.T) {
	r := newRig(t)
	c := r.save(sqliteConn(makeFixture(t)))
	r.connect(c)
	tests := []struct {
		name string
		path []string
		want []string
	}{
		{"the connection", nil, []string{"New Query"}},
		{"a schema", []string{"main"}, []string{"New Query"}},
		{"a folder", []string{"main", "Tables"}, nil},
		{"a table", []string{"main", "Tables", "books"}, []string{
			"New Query", "Select Top 1000", "Generate SELECT Statement", "Generate INSERT Statement", "Generate UPDATE Statement",
			"Generate DELETE Statement", "Generate CREATE Statement", "Drop Table (as text)",
		}},
		{"a view", []string{"main", "Views", "book_titles"}, []string{"New Query", "Select Top 1000", "Generate SELECT Statement", "Generate CREATE Statement", "Drop View (as text)"}},
		{"a column", []string{"main", "Tables", "books", "Columns", "id"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n *dbconn.Node
			if tt.path != nil {
				n = r.node(c, tt.path...)
			}
			if got := titles(r.db.ActionsIn(r.api, c.ID, n)); !slices.Equal(got, tt.want) {
				t.Errorf("entries = %v, want %v", got, tt.want)
			}
		})
	}
	// the groups keep queries, generators and drops together
	groups := map[string]string{}
	for _, a := range r.db.ActionsIn(r.api, c.ID, r.node(c, "main", "Tables", "books")) {
		groups[a.Title] = a.Group
	}
	if groups["New Query"] != groups["Select Top 1000"] || groups["Generate SELECT Statement"] == groups["Select Top 1000"] || groups["Drop Table (as text)"] == groups["Generate CREATE Statement"] {
		t.Errorf("groups = %v", groups)
	}
}

func TestTheEntriesOpenStatementsInAnSQLEditor(t *testing.T) {
	r := newRig(t)
	c := r.save(sqliteConn(makeFixture(t)))
	r.connect(c)
	actions := r.db.ActionsIn(r.api, c.ID, r.node(c, "main", "Tables", "books"))
	tests := []struct {
		title string
		want  string
	}{
		{"New Query", ""},
		{"Select Top 1000", "SELECT * FROM \"books\" LIMIT 1000;\n"},
		{"Generate SELECT Statement", "SELECT\n    \"id\",\n    \"author_id\",\n    \"title\",\n    \"isbn\",\n    \"price\"\nFROM \"books\";\n"},
		{"Generate DELETE Statement", "DELETE FROM \"books\"\nWHERE \"id\" = NULL;\n"},
		{"Drop Table (as text)", "DROP TABLE \"books\";\n"},
	}
	for i, tt := range tests {
		run(t, actions, tt.title)
		r.api.WaitFor(tt.title, func() bool { return len(r.api.UntitledDocuments()) == i+1 })
		doc := r.api.UntitledDocuments()[i]
		if doc.LanguageID != "sql" || doc.Text != tt.want {
			t.Errorf("%s opened %q in %q, want %q in sql", tt.title, doc.Text, doc.LanguageID, tt.want)
		}
	}
	// INSERT and CREATE are written from several queries: the same editor, the text read from the catalog
	run(t, actions, "Generate INSERT Statement")
	run(t, actions, "Generate CREATE Statement")
	r.api.WaitFor("two more editors", func() bool { return len(r.api.UntitledDocuments()) == len(tests)+2 })
	texts := []string{r.api.UntitledDocuments()[len(tests)].Text, r.api.UntitledDocuments()[len(tests)+1].Text}
	slices.Sort(texts)
	if !strings.HasPrefix(texts[0], "CREATE TABLE books (") || !strings.HasPrefix(texts[1], "INSERT INTO \"books\" (\"id\"") {
		t.Errorf("texts = %q", texts)
	}
}

func TestAnEditorThatCannotBeOpenedIsReported(t *testing.T) {
	r := newRig(t)
	c := r.save(sqliteConn(makeFixture(t)))
	r.connect(c)
	r.api.FailUntitled(errors.New("WindowFake", "the window tab is not on screen"))
	actions := r.db.ActionsIn(r.api, c.ID, r.node(c, "main", "Tables", "books"))
	run(t, actions, "Drop Table (as text)")
	notes := r.api.Notes()
	if len(notes) == 0 || notes[len(notes)-1].Severity != "error" || !strings.Contains(notes[len(notes)-1].Message, "not on screen") {
		t.Errorf("notes = %+v", notes)
	}
	// a statement that cannot be read from the catalog is reported too
	r.api.FailUntitled(nil)
	if err := r.db.Disconnect(c.ID); err != nil {
		t.Fatal(err)
	}
	run(t, actions, "Generate SELECT Statement")
	r.api.WaitFor("the failure", func() bool {
		notes := r.api.Notes()
		return len(notes) > 1 && strings.Contains(notes[len(notes)-1].Message, "Cannot read the columns of books")
	})
	if len(r.api.UntitledDocuments()) != 0 {
		t.Errorf("a failed statement opened an editor: %v", r.api.UntitledDocuments())
	}
}

func TestNewQueryAsksWhichConnectionWhenThereAreSeveral(t *testing.T) {
	r := newRig(t)
	if err := r.api.Run(NewQuery, nil); err != nil {
		t.Fatal(err)
	}
	if notes := r.api.Notes(); len(notes) != 1 || !strings.Contains(notes[0].Message, "no open SQL connection") {
		t.Errorf("notes = %+v", notes)
	}
	a := r.save(sqliteConn(makeFixture(t)))
	b := sqliteConn(makeFixture(t))
	b.ID, b.Name = "", "second"
	b = r.save(b)
	r.connect(a)
	r.connect(b)
	if err := r.api.Run(NewQuery, nil); err != nil {
		t.Fatal(err)
	}
	pick := r.api.LastPick()
	if pick == nil || len(pick.Labels()) != 2 || pick.Labels()[0] != "$(database) fixture" {
		t.Fatalf("pick = %+v", pick)
	}
	pick.Choose(1)
	if docs := r.api.UntitledDocuments(); len(docs) != 1 || docs[0].LanguageID != "sql" {
		t.Errorf("documents = %+v", docs)
	}
	// the connection can be named in the arguments, and then nothing is asked
	if err := r.api.Run(NewQuery, map[string]any{"connection": a.ID}); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(NewQuery, a.ID); err != nil {
		t.Fatal(err)
	}
	if len(r.api.UntitledDocuments()) != 3 || len(r.api.Picks()) != 1 {
		t.Errorf("documents = %d, picks = %d", len(r.api.UntitledDocuments()), len(r.api.Picks()))
	}
}

func TestTwoWindowTabsShareTheConnectionsAndEachOpensEditorsInItsOwn(t *testing.T) {
	r := newRig(t)
	c := r.save(sqliteConn(makeFixture(t)))
	r.connect(c)
	// a second window tab: its own API, the same hub
	second := FactoryOf(r.hub)().(*Extension)
	scope := extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "other", Folder: t.TempDir(), ID: "other"}
	api2 := fakeapi.New(t, second.Manifest(), scope)
	ctx2, stop2 := context.WithCancel(context.Background())
	t.Cleanup(stop2)
	if err := second.Activate(ctx2, api2); err != nil {
		t.Fatal(err)
	}
	node := r.node(c, "main", "Tables", "books")
	run(t, r.db.ActionsIn(api2, c.ID, node), "Drop Table (as text)")
	if len(api2.UntitledDocuments()) != 1 || len(r.api.UntitledDocuments()) != 0 {
		t.Errorf("the editor opened in the wrong tab: second %d, first %d", len(api2.UntitledDocuments()), len(r.api.UntitledDocuments()))
	}
	// closing the first tab keeps the engines and the open connection for the second
	r.stop()
	waitUntil(t, "the first tab to be gone", func() bool { return len(r.db.Engines()) == 4 && r.hub.Service().IsOpen(c.ID) })
	if r.db.State(c.ID).Status != dbconn.Connected {
		t.Errorf("state = %+v", r.db.State(c.ID))
	}
	// closing the last one closes the connection and takes the engines away
	stop2()
	waitUntil(t, "the extension to go", func() bool { return len(r.db.Engines()) == 0 && !r.hub.Service().IsOpen(c.ID) })
}
