package dbconn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

func TestPrefillOfArgs(t *testing.T) {
	tests := []struct {
		name string
		args any
		want Prefill
	}{
		{"an engine as a string", "postgres", Prefill{Engine: "postgres"}},
		{"nothing", nil, Prefill{}},
		{"a number", 3, Prefill{}},
		{"every part", map[string]any{"engine": "mysql", "name": "shop-db", "host": "127.0.0.1", "port": float64(13306), "user": "root", "database": "shop"},
			Prefill{Engine: "mysql", Name: "shop-db", Host: "127.0.0.1", Port: 13306, User: "root", Database: "shop"}},
		{"a port as text", map[string]any{"engine": "redis", "port": " 6380 "}, Prefill{Engine: "redis", Port: 6380}},
		{"a port that is no port", map[string]any{"engine": "redis", "port": float64(70000)}, Prefill{Engine: "redis"}},
		{"a port with a fraction", map[string]any{"engine": "redis", "port": 12.5}, Prefill{Engine: "redis"}},
		{"a negative port", map[string]any{"engine": "redis", "port": float64(-1)}, Prefill{Engine: "redis"}},
		{"a port that is text but no number", map[string]any{"engine": "redis", "port": "http"}, Prefill{Engine: "redis"}},
		{"a password is not read", map[string]any{"engine": "postgres", "password": "hunter2", "passwordEnv": "PGPASSWORD", "user": "me"}, Prefill{Engine: "postgres", User: "me"}},
		{"a value that names a variable is left out", map[string]any{"engine": "postgres", "user": "${env:SECRET}", "database": "ok"}, Prefill{Engine: "postgres", Database: "ok"}},
		{"a control character leaves the value out", map[string]any{"engine": "postgres", "name": "a\nb", "host": "h"}, Prefill{Engine: "postgres", Host: "h"}},
		{"text that is too long is left out", map[string]any{"engine": "postgres", "name": strings.Repeat("x", maxPrefillText+1), "host": strings.Repeat("é", maxPrefillText)},
			Prefill{Engine: "postgres", Host: strings.Repeat("é", maxPrefillText)}},
		{"a value that is no text", map[string]any{"engine": "postgres", "user": 7, "host": []any{"h"}}, Prefill{Engine: "postgres"}},
		{"the spaces around a value go", map[string]any{"engine": " postgres ", "user": "  me "}, Prefill{Engine: "postgres", User: "me"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PrefillOfArgs(tt.args); got != tt.want {
				t.Errorf("PrefillOfArgs = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPrefillHasNoPlaceForAPassword(t *testing.T) {
	raw, err := json.Marshal(Prefill{Engine: "postgres", Name: "n", Host: "h", Port: 1, User: "u", Database: "d"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "pass") {
		t.Errorf("a prefill has a password field: %s", raw)
	}
}

func TestPrefillApply(t *testing.T) {
	full := Prefill{Engine: "x", Name: "box", Host: "127.0.0.1", Port: 55432, User: "app", Database: "shop"}
	tests := []struct {
		name   string
		engine Engine
		check  func(m *FormModel) string
	}{
		{"an engine with every field", pgEngine, func(m *FormModel) string {
			if m.Name != "box" || m.Host != "127.0.0.1" || m.Port != "55432" || m.User != "app" || m.Database != "shop" {
				return "the fields are not filled"
			}
			if m.Password != "" || m.PasswordSet || m.SavePassword || m.PasswordEnv != "" {
				return "the password section is not empty"
			}
			return ""
		}},
		{"an engine with no port in the prefill keeps its own", pgEngine, nil},
		{"an engine with no database", Engine{ID: "redis", Name: "Redis", DefaultPort: 6379, NoDatabase: true, NoUser: true}, func(m *FormModel) string {
			if m.Database != "" || m.User != "" || m.Port != "55432" {
				return "a field the engine does not have was filled"
			}
			return ""
		}},
		{"a file engine", sqliteEngine, func(m *FormModel) string {
			if m.Host != "" || m.Port != "" || m.Name != "box" {
				return "a file engine took a host or a port"
			}
			return ""
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewFormModel(tt.engine, "/work")
			p := full
			if tt.check == nil {
				p.Port = 0
			}
			p.Apply(m)
			if tt.check == nil {
				if m.Port != "5432" {
					t.Errorf("port = %q, want the engine's 5432", m.Port)
				}
				return
			}
			if msg := tt.check(m); msg != "" {
				t.Errorf("%s: %+v", msg, m)
			}
		})
	}
}

// The request of the database extensions opens the form prefilled, in the window tab, and saves nothing.
func TestOpenFormRequestOpensThePrefilledForm(t *testing.T) {
	r := newRig(t, pgDriver())
	ev, err := r.api.AskAs(context.Background(), "emetos.sql", TopicOpenForm, Prefill{Engine: "postgres", Name: "orders-db", Host: "127.0.0.1", Port: 15432, User: "app", Database: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	if string(ev.Data) != `{"opened":true}` {
		t.Errorf("reply = %s", ev.Data)
	}
	tabs := r.api.EditorTabs()
	if len(tabs) != 1 || tabs[0].Kind() != FormKindID || tabs[0].Title() != "New PostgreSQL Connection" || !tabs[0].Revealed {
		t.Fatalf("tabs = %+v", tabs)
	}
	m := r.ext.forms[0].model
	if m.Name != "orders-db" || m.Host != "127.0.0.1" || m.Port != "15432" || m.User != "app" || m.Database != "orders" {
		t.Errorf("the form = %+v", m)
	}
	if m.Password != "" || m.SavePassword || m.PasswordEnv != "" || m.ID != "" {
		t.Errorf("the form has more than the prefill: %+v", m)
	}
	if got := r.hub.Store().All(); len(got) != 0 {
		t.Errorf("the request saved %+v", got)
	}
	// the command with the same arguments does the same
	r.run(NewConnection, map[string]any{"engine": "postgres", "name": "second", "port": float64(15433)})
	if m := r.ext.forms[1].model; m.Name != "second" || m.Port != "15433" || m.Host != "" {
		t.Errorf("the form of the command = %+v", m)
	}
}

func TestOpenFormRequestFailures(t *testing.T) {
	tests := []struct {
		name      string
		publisher string
		payload   any
		route     string
	}{
		{"an engine no active extension has", "emetos.redis", Prefill{Engine: "redis"}, "Extension.openPrefilled.DatabasesEngineNotActive"},
		{"no engine", "emetos.sql", Prefill{Host: "h"}, "Extension.openPrefilled.DatabasesEngineNotActive"},
		{"a client of a shared workspace", extension.RelayID, Prefill{Engine: "postgres"}, "Extension.handleOpenForm.DatabasesOpenFormRefused"},
		{"a payload that is no object", "emetos.sql", "postgres", "Event.Decode.EventValueInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, pgDriver())
			_, err := r.api.AskAs(context.Background(), tt.publisher, TopicOpenForm, tt.payload)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if len(r.api.EditorTabs()) != 0 {
				t.Error("a form was opened")
			}
		})
	}
}

// requestRecorder stands for the extension "Databases" in the tests of the database extensions' side: it answers the request and
// keeps the prefills it got.
func requestRecorder(t *testing.T, api *fakeapi.API, answer error) *[]Prefill {
	t.Helper()
	var got []Prefill
	fakeapi.OnRequestValue(api, TopicOpenForm, func(_ context.Context, p Prefill) (any, error) {
		got = append(got, p)
		if answer != nil {
			return nil, answer
		}
		return map[string]bool{"opened": true}, nil
	})
	return &got
}

// A database extension asks for the form when a container extension says Connect to Database for one of its engines, and for no
// other event.
func TestHandleContainerDatabase(t *testing.T) {
	m := extension.Manifest{ID: "emetos.sql", Name: "SQL", Version: "1", Events: extension.EventDecl{Subscribes: ContainerTopics()}}
	target := map[string]any{"engine": "postgres", "name": "orders-db", "host": "127.0.0.1", "port": 15432, "user": "app", "database": "orders", "password": "hunter2"}
	tests := []struct {
		name      string
		publisher string
		topic     string
		payload   any
		want      []Prefill
	}{
		{"Docker's event for an engine of the extension", "emetos.containers", TopicContainersConnectDatabase, target,
			[]Prefill{{Engine: "postgres", Name: "orders-db", Host: "127.0.0.1", Port: 15432, User: "app", Database: "orders"}}},
		{"Podman's event", "emetos.podman", TopicPodmanConnectDatabase, map[string]any{"engine": "mysql", "port": 13306}, []Prefill{{Engine: "mysql", Port: 13306}}},
		{"an engine of another extension", "emetos.containers", TopicContainersConnectDatabase, map[string]any{"engine": "redis", "port": 6379}, nil},
		{"no engine", "emetos.containers", TopicContainersConnectDatabase, map[string]any{"port": 6379}, nil},
		{"a client of a shared workspace that publishes the topic", extension.RelayID, TopicContainersConnectDatabase, target, nil},
		{"the wrong extension on the topic", "emetos.podman", TopicContainersConnectDatabase, target, nil},
		{"any other extension", "emetos.sql", TopicPodmanConnectDatabase, target, nil},
		{"the core", extension.CorePublisher, TopicContainersConnectDatabase, target, nil},
		{"a payload that is no object", "emetos.containers", TopicContainersConnectDatabase, "postgres", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := fakeapi.New(t, m, extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: t.TempDir()})
			got := requestRecorder(t, api, nil)
			if err := HandleContainerDatabase(context.Background(), api, "postgres", "mysql"); err != nil {
				t.Fatal(err)
			}
			api.EmitAs(tt.publisher, tt.topic, tt.payload)
			api.Pump()
			if len(*got) != len(tt.want) || (len(tt.want) == 1 && (*got)[0] != tt.want[0]) {
				t.Errorf("requests = %+v, want %+v", *got, tt.want)
			}
			if len(api.Notes()) != 0 {
				t.Errorf("notes = %+v", api.Notes())
			}
		})
	}
}

func TestHandleContainerDatabaseTellsWhenTheFormCannotBeOpened(t *testing.T) {
	m := extension.Manifest{ID: "emetos.sql", Name: "SQL", Version: "1", Events: extension.EventDecl{Subscribes: ContainerTopics()}}
	api := fakeapi.New(t, m, extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: t.TempDir()})
	requestRecorder(t, api, errors.New("DatabasesTestRefused", "the form is busy"))
	if err := HandleContainerDatabase(context.Background(), api, "postgres"); err != nil {
		t.Fatal(err)
	}
	api.EmitAs("emetos.containers", TopicContainersConnectDatabase, map[string]any{"engine": "postgres"})
	api.Pump()
	if n := api.Notes(); len(n) != 1 || !strings.Contains(n[0].Message, "Cannot open the connection form: the form is busy") {
		t.Errorf("notes = %+v", n)
	}
}

// The event topics are the ones the container extensions publish.
func TestContainerTopicsAreTheOwnersOwn(t *testing.T) {
	for topic, owner := range containerTopics {
		if !strings.HasPrefix(topic, owner+".") {
			t.Errorf("%s is not under the id of its owner %s", topic, owner)
		}
	}
	if len(ContainerTopics()) != len(containerTopics) {
		t.Errorf("ContainerTopics = %v", ContainerTopics())
	}
}
