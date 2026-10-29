package containerui

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// connectionsJSON is what `podman system connection list --format json` prints with a rootless and a rootful engine of a machine, a
// socket of the user, and a connection over ssh.
const connectionsJSON = `[
 {"Name":"podman-machine-default","URI":"ssh://core@127.0.0.1:44571/run/user/1000/podman/podman.sock","Identity":"/home/u/.local/share/containers/podman/machine/machine","IsMachine":true,"Default":true,"ReadWrite":true},
 {"Name":"podman-machine-default-root","URI":"ssh://root@127.0.0.1:44571/run/podman/podman.sock","Identity":"/home/u/.local/share/containers/podman/machine/machine","IsMachine":true,"Default":false,"ReadWrite":true},
 {"Name":"local","URI":"unix:///run/user/1000/podman/podman.sock","Identity":"","IsMachine":false,"Default":false,"ReadWrite":true},
 {"Name":"system","URI":"unix:///run/podman/podman.sock","Identity":"","IsMachine":false,"Default":false,"ReadWrite":false}
]`

func TestParseConnections(t *testing.T) {
	for _, tt := range []struct {
		name, in string
		want     []connection
		route    string
	}{
		{"none", "", nil, ""},
		{"null", "null\n", nil, ""},
		{"empty list", "[]", []connection{}, ""},
		{"a connection without ReadWrite is read-write", `[{"Name":"a","URI":"unix:///x.sock"}]`, []connection{{Name: "a", URI: "unix:///x.sock", ReadWrite: true}}, ""},
		{"a nameless row is left out", `[{"URI":"unix:///x.sock"},{"Name":"b","URI":"tcp://h:1","Default":true,"ReadWrite":false}]`,
			[]connection{{Name: "b", URI: "tcp://h:1", Default: true}}, ""},
		{"not JSON", "{oops", nil, "parseConnections.ContainerUIConnectionsInvalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConnections(tt.in)
			if tt.route != "" {
				appErr, ok := errors.FromError(err)
				if !ok || !appErr.HasRoute(tt.route) {
					t.Fatalf("error %v, want route %s", err, tt.route)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) || (len(got) > 0 && !reflect.DeepEqual(got, tt.want)) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestRootfulAndUsable holds the reading of a connection: rootful by its socket or its name, usable unless it is over ssh.
func TestRootfulAndUsable(t *testing.T) {
	cs, err := parseConnections(connectionsJSON)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct {
		kind   string
		usable bool
	}{{"rootless", false}, {"rootful", false}, {"rootless", true}, {"rootful", true}} {
		ok, why := cs[i].usable()
		if cs[i].kind() != want.kind || ok != want.usable || (!ok && !strings.Contains(why, "ssh://")) {
			t.Errorf("%s: %s, usable %v (%s), want %v", cs[i].Name, cs[i].kind(), ok, why, want)
		}
	}
	if ok, why := (connection{URI: "npipe:////./pipe/podman"}).usable(); ok || !strings.Contains(why, "npipe://") {
		t.Errorf("npipe: %v %q", ok, why)
	}
	if ok, _ := (connection{URI: "no scheme"}).usable(); ok {
		t.Error("an address with no scheme is usable")
	}
}

func TestTheConnectionsSection(t *testing.T) {
	cli := newFakeCLI(t, "podman")
	cli.answer("system_connection", connectionsJSON, "", 0)
	fake := containerapitest.NewFake(t, containerapi.KindPodman, containerapitest.FakeOptions{})
	var hosts recorder[string]
	cfg := podmanConfig(func(_ context.Context, o containerapi.FindOptions) (*containerapi.Found, error) {
		hosts.add(o.Host)
		return &containerapi.Found{Client: fake.Client(), Source: "the fake engine"}, nil
	})
	r := startRig(t, fake, cfg)
	r.wait("the connections", func() bool { return r.ext.loads[resConnections].loaded && r.ext.status == statusConnected })
	if got := r.labels(secConnections); !reflect.DeepEqual(got, []string{"Automatic", "podman-machine-default", "podman-machine-default-root", "local", "system"}) {
		t.Fatalf("rows %v", got)
	}
	if got := cli.lines(); len(got) == 0 || got[0] != "system connection list --format json" {
		t.Errorf("the program was run as %v", got)
	}

	t.Run("a socket is used, and the engine is looked for again at its address", func(t *testing.T) {
		r.run("connections.use", key(keyConnection+"local"))
		r.wait("the new engine", func() bool { return hosts.len() == 2 && r.ext.status == statusConnected })
		if hosts.at(1) != "unix:///run/user/1000/podman/podman.sock" || r.ext.picked != "local" {
			t.Errorf("host %q picked %q", hosts.at(1), r.ext.picked)
		}
	})
	t.Run("the pick is saved with the view and restored", func(t *testing.T) {
		raw, err := r.ext.saveState()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(raw)
		if !strings.Contains(string(b), `"connection":"local"`) || !strings.Contains(string(b), `"connectionHost":"unix:///run/user/1000/podman/podman.sock"`) || strings.Contains(string(b), `"context"`) {
			t.Errorf("the state: %s", b)
		}
	})
	t.Run("a connection over ssh is refused with the reason", func(t *testing.T) {
		before := hosts.len()
		r.run("connections.use", key(keyConnection+"podman-machine-default"))
		if !r.hasNote("Cannot use the connection podman-machine-default: Emetos does not connect over ssh://") || hosts.len() != before || r.ext.picked != "local" {
			t.Errorf("notes %v, picked %q", r.notes(), r.ext.picked)
		}
	})
	t.Run("an unknown connection is refused", func(t *testing.T) {
		r.run("connections.use", key(keyConnection+"gone"))
		if !r.hasNote("There is no connection called gone.") {
			t.Errorf("notes %v", r.notes())
		}
	})
	t.Run("the CLI is pointed at the engine the view shows", func(t *testing.T) {
		if got := r.ext.explicitHost(); got != "unix:///run/user/1000/podman/podman.sock" {
			t.Errorf("explicitHost %q", got)
		}
		env := r.ext.cliEnviron()
		if env[len(env)-1] != "CONTAINER_HOST=unix:///run/user/1000/podman/podman.sock" {
			t.Errorf("the environment ends with %q", env[len(env)-1])
		}
		if got := r.ext.withEngineURL([]string{"podman", "exec", "-it", "x", "sh"}); !reflect.DeepEqual(got, []string{"podman", "--url", "unix:///run/user/1000/podman/podman.sock", "exec", "-it", "x", "sh"}) {
			t.Errorf("withEngineURL %v", got)
		}
	})
	t.Run("Automatic goes back to the search", func(t *testing.T) {
		r.run("connections.use", key(keyAutomatic))
		r.wait("the search", func() bool { return hosts.len() == 3 && r.ext.status == statusConnected })
		if hosts.at(2) != "" || r.ext.picked != "" || r.ext.pickedHost != "" || r.ext.explicitHost() != "" {
			t.Errorf("host %q picked %q", hosts.at(2), r.ext.picked)
		}
		env := r.ext.cliEnviron()
		for _, kv := range env {
			if strings.HasPrefix(kv, "CONTAINER_HOST=") {
				t.Errorf("the environment has %q", kv)
			}
		}
	})
	t.Run("the picker lists them with the reason for those that cannot be used", func(t *testing.T) {
		r.run("connections.use", nil)
		r.api.Pump()
		pick := r.api.LastPick()
		if pick == nil || !reflect.DeepEqual(pick.Labels(), []string{"Automatic", "podman-machine-default", "podman-machine-default-root", "local", "system"}) {
			t.Fatalf("pick %+v", pick)
		}
		if d := pick.Options().Items[1].Detail; !strings.Contains(d, "rootless") || !strings.Contains(d, "ssh://") {
			t.Errorf("detail %q", d)
		}
		if d := pick.Options().Items[2].Detail; !strings.HasPrefix(d, "rootful") {
			t.Errorf("detail %q", d)
		}
		pick.Choose(3)
		r.wait("the choice", func() bool { return r.ext.picked == "local" && r.ext.status == statusConnected })
	})
	t.Run("inspect opens the description", func(t *testing.T) {
		before := len(r.api.EditorTabs())
		r.run("connections.inspect", key(keyConnection+"podman-machine-default-root"))
		r.wait("the tab", func() bool { return len(r.api.EditorTabs()) > before })
		tab := r.api.EditorTabs()[before]
		var st docState
		if err := json.Unmarshal(tab.State(), &st); err != nil {
			t.Fatal(err)
		}
		if tab.Title() != "podman-machine-default-root (inspect)" || !strings.Contains(st.Text, `"Engine": "rootful"`) || !strings.Contains(st.Text, `"UsableByEmetos": false`) {
			t.Errorf("%q:\n%s", tab.Title(), st.Text)
		}
	})
}

// TestTheConnectionsOfAMissingPodmanProgram holds that a Podman with no program on PATH shows the reason in the section and still
// connects through the API.
func TestTheConnectionsOfAMissingPodmanProgram(t *testing.T) {
	t.Setenv("FAKE_CLI_DIR", "")
	fake := containerapitest.NewFake(t, containerapi.KindPodman, containerapitest.FakeOptions{})
	cfg := podmanConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
		return &containerapi.Found{Client: fake.Client(), Source: "the fake engine"}, nil
	})
	r := startRig(t, fake, cfg)
	r.wait("the lists", func() bool {
		return r.ext.loads[resConnections].loaded && r.ext.status == statusConnected && r.ext.loads[resPods].loaded
	})
	if m := r.view(secConnections).message; !strings.Contains(m, "Cannot read the connections: ") {
		t.Errorf("message %q", m)
	}
	if got := r.labels(secConnections); !reflect.DeepEqual(got, []string{"Automatic"}) {
		t.Errorf("rows %v", got)
	}
}

// TestAFailingConnectionListIsShown holds that a program that fails shows its message, and that the route of the failure is told.
func TestAFailingConnectionListIsShown(t *testing.T) {
	cli := newFakeCLI(t, "podman")
	cli.answer("system_connection", "", "Error: containers.conf is damaged", 125)
	fake := containerapitest.NewFake(t, containerapi.KindPodman, containerapitest.FakeOptions{})
	cfg := podmanConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
		return &containerapi.Found{Client: fake.Client(), Source: "the fake engine"}, nil
	})
	r := startRig(t, fake, cfg)
	r.wait("the connections", func() bool { return r.ext.loads[resConnections].loaded })
	if m := r.view(secConnections).message; !strings.Contains(m, "ended with status 125") || !strings.Contains(m, "containers.conf is damaged") {
		t.Errorf("message %q", m)
	}
	_, err := r.ext.readConnections(context.Background())
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("ext.readConnections.ContainerUIConnectionsFailed") {
		t.Errorf("error %v", err)
	}
}
