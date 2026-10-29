package extensions

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/containers"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/emetos/extensions/internal/containerui"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/podman"
)

// startedContainer makes and starts a container of the fake engine and returns its id.
func startedContainer(t *testing.T, f *containerapitest.Fake, name, image string, env []string, containerPort, hostPort uint16) string {
	t.Helper()
	f.AddImage(1, image)
	spec := containerapi.ContainerSpec{Name: name, Image: image, Env: env}
	if hostPort != 0 {
		spec.Ports = []containerapi.PortMapping{{HostIP: "127.0.0.1", HostPort: hostPort, ContainerPort: containerPort}}
	}
	res, err := f.Client().CreateContainer(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Client().StartContainer(context.Background(), res.ID); err != nil {
		t.Fatal(err)
	}
	return res.ID
}

// The event of a container extension reaches the extension that has the engine, which asks the Databases extension for the form,
// prefilled; the other database extensions do nothing. Docker and Podman alike, for every image of SPEC §10.24.
func TestConnectToDatabaseOpensThePrefilledFormOfTheRightExtension(t *testing.T) {
	const secret = "never-in-a-form-or-an-event"
	tests := []struct {
		name      string
		image     string
		env       []string
		port      uint16
		asker     string
		wantTitle string
		want      dbconn.Draft
	}{
		{"postgres", "postgres:17.6-alpine", []string{"POSTGRES_USER=app", "POSTGRES_DB=orders", "POSTGRES_PASSWORD=" + secret}, 5432, "emetos.sql", "New PostgreSQL Connection",
			dbconn.Draft{Engine: "postgres", Name: "db", Host: "127.0.0.1", Port: "15000", User: "app", Database: "orders"}},
		{"mysql", "mysql:8.4.6", []string{"MYSQL_ROOT_PASSWORD=" + secret, "MYSQL_DATABASE=shop"}, 3306, "emetos.sql", "New MySQL / MariaDB Connection",
			dbconn.Draft{Engine: "mysql", Name: "db", Host: "127.0.0.1", Port: "15000", User: "root", Database: "shop"}},
		{"mariadb", "mariadb:11.4.8", []string{"MARIADB_USER=app", "MARIADB_DATABASE=shop"}, 3306, "emetos.sql", "New MySQL / MariaDB Connection",
			dbconn.Draft{Engine: "mysql", Name: "db", Host: "127.0.0.1", Port: "15000", User: "app", Database: "shop"}},
		{"sql server", "mcr.microsoft.com/mssql/server:2022-latest", []string{"MSSQL_SA_PASSWORD=" + secret}, 1433, "emetos.sql", "New SQL Server Connection",
			dbconn.Draft{Engine: "sqlserver", Name: "db", Host: "127.0.0.1", Port: "15000", User: "sa"}},
		{"mongo", "mongo:7.0.14", []string{"MONGO_INITDB_ROOT_USERNAME=root", "MONGO_INITDB_ROOT_PASSWORD=" + secret}, 27017, "emetos.mongodb", "New MongoDB Connection",
			dbconn.Draft{Engine: "mongodb", Name: "db", Host: "127.0.0.1", Port: "15000", User: "root", Database: "admin"}},
		{"redis", "redis:8.2.2", nil, 6379, "emetos.redis", "New Redis Connection",
			dbconn.Draft{Engine: "redis", Name: "db", Host: "127.0.0.1", Port: "15000"}},
		{"valkey", "valkey/valkey:8", nil, 6379, "emetos.redis", "New Redis Connection",
			dbconn.Draft{Engine: "redis", Name: "db", Host: "127.0.0.1", Port: "15000"}},
	}
	for _, engine := range []struct {
		name string
		kind containerapi.Kind
		cfg  func(containerui.DialFunc, containerui.ListFilters) containerui.Config
	}{{"Docker", containerapi.KindDocker, dockerConfig}, {"Podman", containerapi.KindPodman, podmanConfig}} {
		for _, tt := range tests {
			t.Run(engine.name+" "+tt.name, func(t *testing.T) {
				fake := containerapitest.NewFake(t, engine.kind, containerapitest.FakeOptions{})
				id := startedContainer(t, fake, "db", tt.image, tt.env, tt.port, 15000)
				r := startConnectRig(t, engine.cfg(fixedEngine(fake.Client()), containerui.ListFilters{}))

				r.connectDatabase(id)

				for ext, asked := range r.asked {
					if ext != tt.asker && len(asked) != 0 {
						t.Errorf("%s asked for a form, and the image is %s's", ext, tt.asker)
					}
				}
				if got := r.asked[tt.asker]; len(got) != 1 {
					t.Fatalf("%s asked %d times: %+v", tt.asker, len(got), got)
				}
				tabs := r.dbs.EditorTabs()
				if len(tabs) != 1 || tabs[0].Title() != tt.wantTitle {
					t.Fatalf("tabs = %d, title %q, want %q", len(tabs), titleOf(tabs), tt.wantTitle)
				}
				got, _ := r.openedForm()
				got.Folder, got.Options, got.Scope = "", nil, "" // what a new form starts with whatever the prefill
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("the form = %+v, want %+v", got, tt.want)
				}
				for _, p := range r.cont.PublishedEvents() {
					if strings.Contains(string(p.Data), secret) {
						t.Errorf("the event %s carries a password: %s", p.Topic, p.Data)
					}
				}
				if state := string(r.dbs.EditorTabs()[0].State()); strings.Contains(state, secret) {
					t.Errorf("the form's state carries a password: %s", state)
				}
				if len(r.hub.Store().All()) != 0 {
					t.Error("a connection was saved before the user pressed Save")
				}
			})
		}
	}
}

func titleOf[T interface{ Title() string }](tabs []T) string {
	if len(tabs) == 0 {
		return ""
	}
	return tabs[0].Title()
}

// A container that is not a database is not offered; one whose port is not published says so; neither opens a form.
func TestConnectToDatabaseOpensNoFormForWhatIsNotReady(t *testing.T) {
	fake := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	web := startedContainer(t, fake, "web", "nginx:1.27", nil, 80, 18080)
	closed := startedContainer(t, fake, "closed", "postgres:17.6-alpine", nil, 5432, 0)
	r := startConnectRig(t, dockerConfig(fixedEngine(fake.Client()), containerui.ListFilters{}))

	// the command on a web server offers the databases there are instead: only the one that is not ready
	r.runConnectDatabase(web)
	picks := r.cont.Picks()
	if len(picks) != 1 || len(picks[0].Labels()) != 1 || picks[0].Labels()[0] != "closed" {
		t.Fatalf("the picker offers %v", picks)
	}
	picks[0].Choose(0)
	r.cont.WaitFor("the note", func() bool { return len(r.cont.Notes()) == 1 })
	r.carry()
	if notes := r.cont.Notes(); !strings.Contains(notes[0].Message, "closed does not publish port 5432") {
		t.Errorf("notes = %+v", notes)
	}
	// the same on the container itself
	r.runConnectDatabase(closed)
	r.cont.WaitFor("the second note", func() bool { return len(r.cont.Notes()) == 2 })
	r.carry()
	if len(r.dbs.EditorTabs()) != 0 || len(r.asked) != 0 {
		t.Errorf("a form was asked for: %v", r.asked)
	}
	for _, p := range r.cont.PublishedEvents() {
		if strings.HasSuffix(p.Topic, ".connectDatabase") {
			t.Errorf("an event was published: %s", p.Topic)
		}
	}
}

// The extensions that hear Connect to Database are active from the start, because an event reaches only the extensions that are
// active, and declare the topics they listen to.
func TestTheDatabaseExtensionsListenToConnectToDatabaseFromTheStart(t *testing.T) {
	want := map[string]bool{"emetos.databases": false, "emetos.sql": true, "emetos.mongodb": true, "emetos.redis": true}
	seen := map[string]bool{}
	for _, f := range All() {
		m := f().Manifest()
		listens, ok := want[m.ID]
		if !ok {
			continue
		}
		seen[m.ID] = true
		if !slices.Contains(m.ActivationEvents, extension.OnStartup) {
			t.Errorf("%s is not activated on startup: %v", m.ID, m.ActivationEvents)
		}
		if listens {
			for _, topic := range dbconn.ContainerTopics() {
				if !slices.Contains(m.Events.Subscribes, topic) {
					t.Errorf("%s does not declare that it subscribes to %s", m.ID, topic)
				}
			}
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("%s is not in the list of extensions", id)
		}
	}
}

// The topics the database extensions listen to are the ones the container extensions publish.
func TestTheTopicsOfConnectToDatabaseAreThoseOfTheContainerExtensions(t *testing.T) {
	got := []string{containers.Config().ConnectDatabaseTopic(), podman.Config().ConnectDatabaseTopic()}
	if want := dbconn.ContainerTopics(); !slices.Equal(got, want) {
		t.Errorf("the container extensions publish %v, the database extensions listen to %v", got, want)
	}
}
