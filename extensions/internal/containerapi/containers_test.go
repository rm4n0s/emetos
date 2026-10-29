package containerapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

var bg = context.Background()

// lastRequest returns the last request the fake got with the method and a path that ends with suffix.
func lastRequest(t *testing.T, f *containerapitest.Fake, method, suffix string) containerapitest.Request {
	t.Helper()
	reqs := f.Requests()
	for i := len(reqs) - 1; i >= 0; i-- {
		if reqs[i].Method == method && strings.HasSuffix(reqs[i].Path, suffix) {
			return reqs[i]
		}
	}
	t.Fatalf("the engine got no %s ...%s; it got %v", method, suffix, reqs)
	return containerapitest.Request{}
}

func queryOf(t *testing.T, r containerapitest.Request) url.Values {
	t.Helper()
	q, err := url.ParseQuery(r.Query)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestListContainersIsTheSameForBothEngines(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			spec := containerapi.ContainerSpec{
				Name: "web", Image: containerapitest.ImageBusyBox, Cmd: []string{"sh", "-c", "echo hi"},
				Labels: map[string]string{containerapi.LabelCompose: "shop", "tier": "front"},
				Ports:  []containerapi.PortMapping{{HostIP: "127.0.0.1", HostPort: 8080, ContainerPort: 80}, {ContainerPort: 53, Protocol: "udp"}},
			}
			res, err := c.CreateContainer(bg, spec)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.ID) != 64 {
				t.Errorf("id = %q", res.ID)
			}
			if _, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "db", Image: containerapitest.ImageBusyBox, Labels: map[string]string{"tier": "back"}}); err != nil {
				t.Fatal(err)
			}
			if err := c.StartContainer(bg, res.ID); err != nil {
				t.Fatal(err)
			}
			f.AddContainer("old", containerapitest.ImageBusyBox, nil, "exited")

			running, err := c.ListContainers(bg, containerapi.ListContainersOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(running) != 1 {
				t.Fatalf("running containers = %d, want 1", len(running))
			}
			got := running[0]
			if got.ID != res.ID || got.Name() != "web" || got.State != "running" || got.Image != containerapitest.ImageBusyBox || got.Command != "sh -c echo hi" {
				t.Errorf("container = %+v", got)
			}
			if got.ComposeProject() != "shop" || got.Labels["tier"] != "front" || !got.Running() {
				t.Errorf("labels = %v", got.Labels)
			}
			if got.Created.IsZero() || time.Since(got.Created) > time.Minute {
				t.Errorf("created = %v", got.Created)
			}
			ports := append([]containerapi.Port(nil), got.Ports...)
			sort.Slice(ports, func(i, j int) bool { return ports[i].PrivatePort < ports[j].PrivatePort })
			if len(ports) != 2 || ports[0].PrivatePort != 53 || ports[0].Type != "udp" || ports[0].PublicPort == 0 || ports[1] != (containerapi.Port{IP: "127.0.0.1", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}) {
				t.Errorf("ports = %+v", ports)
			}
			if kind == containerapi.KindDocker {
				if !strings.HasPrefix(got.Status, "Up ") || !got.StartedAt.IsZero() {
					t.Errorf("Docker: status %q, started %v", got.Status, got.StartedAt)
				}
				if !strings.HasPrefix(got.ImageID, "sha256:") {
					t.Errorf("Docker image id = %q", got.ImageID)
				}
			} else if got.StartedAt.IsZero() || strings.HasPrefix(got.ImageID, "sha256:") {
				t.Errorf("Podman: started %v, image id %q", got.StartedAt, got.ImageID)
			}
			if len(got.Networks) != 1 || got.Networks[0] != "bridge" {
				t.Errorf("networks = %v", got.Networks)
			}

			all, err := c.ListContainers(bg, containerapi.ListContainersOptions{All: true})
			if err != nil || len(all) != 3 {
				t.Errorf("all = %d containers, %v; want 3", len(all), err)
			}
			byLabel, err := c.ListContainers(bg, containerapi.ListContainersOptions{All: true, Filters: containerapi.Filters{"label": {"tier=back"}}})
			if err != nil || len(byLabel) != 1 || byLabel[0].Name() != "db" {
				t.Errorf("by label = %+v, %v", byLabel, err)
			}
			byName, err := c.ListContainers(bg, containerapi.ListContainersOptions{All: true, Filters: containerapi.Filters{"name": {"ol"}}})
			if err != nil || len(byName) != 1 || byName[0].Name() != "old" {
				t.Errorf("by name = %+v, %v", byName, err)
			}
			q := queryOf(t, lastRequest(t, f, http.MethodGet, "/containers/json"))
			if q.Get("all") != "true" || q.Get("filters") != `{"name":["ol"]}` {
				t.Errorf("query = %v", q)
			}
		})
	}
}

func TestCreateContainerBodiesFollowTheEngine(t *testing.T) {
	spec := containerapi.ContainerSpec{
		Name: "app", Image: containerapitest.ImageBusyBox, Cmd: []string{"serve", "-p", "80"}, Entrypoint: []string{"/entry"},
		Env: []string{"A=1", "B=two=2"}, Labels: map[string]string{"k": "v"}, WorkingDir: "/srv", User: "1000:1000", Tty: true,
		Ports:   []containerapi.PortMapping{{HostIP: "127.0.0.1", HostPort: 8080, ContainerPort: 80}, {ContainerPort: 53, Protocol: "udp"}},
		Binds:   []string{"/host/data:/data:ro", "named:/vol"},
		Network: "backnet", AutoRemove: true,
	}
	t.Run("docker", func(t *testing.T) {
		f, c := engine(t, containerapi.KindDocker)
		f.AddImage(1)
		if _, err := c.CreateContainer(bg, spec); err != nil {
			t.Fatal(err)
		}
		r := lastRequest(t, f, http.MethodPost, "/containers/create")
		if queryOf(t, r).Get("name") != "app" {
			t.Errorf("query = %q, want the name", r.Query)
		}
		var body map[string]any
		if err := json.Unmarshal(r.Body, &body); err != nil {
			t.Fatal(err)
		}
		host, _ := body["HostConfig"].(map[string]any)
		checks := map[string]any{
			"Image": containerapitest.ImageBusyBox, "Cmd": []any{"serve", "-p", "80"}, "Entrypoint": []any{"/entry"}, "Env": []any{"A=1", "B=two=2"},
			"Labels": map[string]any{"k": "v"}, "WorkingDir": "/srv", "User": "1000:1000", "Tty": true, "OpenStdin": true,
		}
		for k, want := range checks {
			if !reflect.DeepEqual(body[k], want) {
				t.Errorf("%s = %v, want %v", k, body[k], want)
			}
		}
		if !reflect.DeepEqual(host["Binds"], []any{"/host/data:/data:ro", "named:/vol"}) || host["AutoRemove"] != true || host["NetworkMode"] != "backnet" {
			t.Errorf("HostConfig = %v", host)
		}
		bindings, _ := host["PortBindings"].(map[string]any)
		tcp, _ := bindings["80/tcp"].([]any)
		udp, _ := bindings["53/udp"].([]any)
		if len(tcp) != 1 || !reflect.DeepEqual(tcp[0], map[string]any{"HostIp": "127.0.0.1", "HostPort": "8080"}) || len(udp) != 1 || !reflect.DeepEqual(udp[0], map[string]any{"HostIp": "", "HostPort": ""}) {
			t.Errorf("PortBindings = %v", bindings)
		}
		if exposed, _ := body["ExposedPorts"].(map[string]any); len(exposed) != 2 {
			t.Errorf("ExposedPorts = %v", body["ExposedPorts"])
		}
	})
	t.Run("podman", func(t *testing.T) {
		f, c := engine(t, containerapi.KindPodman)
		s := spec
		s.Pod = "mypod"
		if _, err := c.CreateContainer(bg, s); err == nil {
			t.Fatal("creating in a pod that does not exist worked")
		}
		if _, err := c.CreatePod(bg, containerapi.PodSpec{Name: "mypod"}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.CreateContainer(bg, s); err != nil {
			t.Fatal(err)
		}
		r := lastRequest(t, f, http.MethodPost, "/containers/create")
		if r.Query != "" {
			t.Errorf("query = %q, libpod takes the name in the body", r.Query)
		}
		var body map[string]any
		if err := json.Unmarshal(r.Body, &body); err != nil {
			t.Fatal(err)
		}
		checks := map[string]any{
			"name": "app", "image": containerapitest.ImageBusyBox, "command": []any{"serve", "-p", "80"}, "entrypoint": []any{"/entry"},
			"env": map[string]any{"A": "1", "B": "two=2"}, "labels": map[string]any{"k": "v"}, "work_dir": "/srv", "user": "1000:1000",
			"terminal": true, "stdin": true, "remove": true, "pod": "mypod", "networks": map[string]any{"backnet": map[string]any{}},
		}
		for k, want := range checks {
			if !reflect.DeepEqual(body[k], want) {
				t.Errorf("%s = %v, want %v", k, body[k], want)
			}
		}
		if !reflect.DeepEqual(body["mounts"], []any{map[string]any{"type": "bind", "source": "/host/data", "destination": "/data", "options": []any{"ro"}}}) {
			t.Errorf("mounts = %v", body["mounts"])
		}
		if !reflect.DeepEqual(body["volumes"], []any{map[string]any{"Name": "named", "Dest": "/vol"}}) {
			t.Errorf("volumes = %v", body["volumes"])
		}
		pm, _ := body["portmappings"].([]any)
		if len(pm) != 2 || !reflect.DeepEqual(pm[0], map[string]any{"container_port": 80.0, "host_port": 8080.0, "host_ip": "127.0.0.1", "protocol": "tcp"}) || !reflect.DeepEqual(pm[1], map[string]any{"container_port": 53.0, "protocol": "udp"}) {
			t.Errorf("portmappings = %v", pm)
		}
	})
	t.Run("a pod on Docker is refused before anything is sent", func(t *testing.T) {
		f, c := engine(t, containerapi.KindDocker)
		f.ResetRequests()
		s := spec
		s.Pod = "mypod"
		_, err := c.CreateContainer(bg, s)
		wantRoute(t, err, "Client.CreateContainer.ContainerAPINotSupported")
		if n := len(f.Requests()); n != 0 {
			t.Errorf("%d requests were sent", n)
		}
	})
}

func TestContainerLifecycleCalls(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			res, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "life", Image: containerapitest.ImageBusyBox, Cmd: []string{"sh", "-c", "echo hi"}})
			if err != nil {
				t.Fatal(err)
			}
			id := res.ID
			state := func() string {
				t.Helper()
				info, err := c.InspectContainer(bg, id)
				if err != nil {
					t.Fatal(err)
				}
				return info.State.Status
			}
			if s := state(); s != "created" {
				t.Fatalf("state after create = %s", s)
			}
			type step struct {
				name  string
				do    func() error
				state string
				check func(t *testing.T)
			}
			steps := []step{
				{"start", func() error { return c.StartContainer(bg, id) }, "running", nil},
				{"start again is not an error", func() error { return c.StartContainer(bg, id) }, "running", nil},
				{"pause", func() error { return c.PauseContainer(bg, id) }, "paused", nil},
				{"unpause", func() error { return c.UnpauseContainer(bg, id) }, "running", nil},
				{"stop with a timeout", func() error { return c.StopContainer(bg, id, 2500*time.Millisecond) }, "exited", func(t *testing.T) {
					name := "t"
					if kind == containerapi.KindPodman {
						name = "timeout"
					}
					if got := queryOf(t, lastRequest(t, f, http.MethodPost, "/stop")).Get(name); got != "2" {
						t.Errorf("stop sent %s=%q, want 2", name, got)
					}
				}},
				{"stop again is not an error", func() error { return c.StopContainer(bg, id, 0) }, "exited", func(t *testing.T) {
					if q := queryOf(t, lastRequest(t, f, http.MethodPost, "/stop")); len(q) != 0 {
						t.Errorf("stop without a timeout sent %v", q)
					}
				}},
				{"restart", func() error { return c.RestartContainer(bg, id, time.Second) }, "running", func(t *testing.T) {
					if got := queryOf(t, lastRequest(t, f, http.MethodPost, "/restart")).Get("t"); got != "1" {
						t.Errorf("restart sent t=%q, want 1", got)
					}
				}},
				{"kill with a signal", func() error { return c.KillContainer(bg, id, "SIGUSR1") }, "exited", func(t *testing.T) {
					if got := queryOf(t, lastRequest(t, f, http.MethodPost, "/kill")).Get("signal"); got != "SIGUSR1" {
						t.Errorf("kill sent signal=%q", got)
					}
				}},
				{"start after the kill", func() error { return c.StartContainer(bg, id) }, "running", nil},
				{"kill with the default signal", func() error { return c.KillContainer(bg, id, "") }, "exited", nil},
			}
			for _, s := range steps {
				if err := s.do(); err != nil {
					t.Fatalf("%s: %v", s.name, err)
				}
				if got := state(); got != s.state {
					t.Errorf("%s: state = %s, want %s", s.name, got, s.state)
				}
				if s.check != nil {
					s.check(t)
				}
			}
			if err := c.RenameContainer(bg, id, "renamed"); err != nil {
				t.Fatal(err)
			}
			if info, _ := c.InspectContainer(bg, id); info == nil || info.Name != "renamed" {
				t.Errorf("after the rename: %+v", info)
			}
			if err := c.RemoveContainer(bg, id, containerapi.RemoveOptions{Volumes: true}); err != nil {
				t.Fatal(err)
			}
			q := queryOf(t, lastRequest(t, f, http.MethodDelete, "/"+id))
			if q.Get("v") != "true" || q.Get("force") != "" {
				t.Errorf("remove sent %v", q)
			}
			_, err = c.InspectContainer(bg, id)
			wantRoute(t, err, "Client.InspectContainer->Client.call->Client.do.ContainerAPINotFound")
		})
	}
}

func TestInspectContainerReadsBothEngines(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			_, c := engine(t, kind)
			res, err := c.CreateContainer(bg, containerapi.ContainerSpec{
				Name: "full", Image: containerapitest.ImageBusyBox, Cmd: []string{"sh", "-c", "echo hi"}, Entrypoint: []string{"/entry", "--x"},
				Env: []string{"A=1"}, Labels: map[string]string{"l": "1"}, WorkingDir: "/w", User: "me", Tty: true, Network: "bridge",
				Ports: []containerapi.PortMapping{{HostIP: "127.0.0.1", HostPort: 9090, ContainerPort: 90}},
				Binds: []string{"/etc/hostname:/h:ro"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := c.StartContainer(bg, res.ID); err != nil {
				t.Fatal(err)
			}
			info, err := c.InspectContainer(bg, res.ID)
			if err != nil {
				t.Fatal(err)
			}
			if info.ID != res.ID || info.Name != "full" || info.ImageName != containerapitest.ImageBusyBox || info.Image == "" {
				t.Errorf("identity = %+v", info)
			}
			if !info.State.Running || info.State.Paused || info.State.Status != "running" || info.State.Pid == 0 || info.State.StartedAt.IsZero() || !info.State.FinishedAt.IsZero() {
				t.Errorf("state = %+v", info.State)
			}
			if !reflect.DeepEqual(info.Env, []string{"A=1"}) || !reflect.DeepEqual(info.Cmd, []string{"sh", "-c", "echo hi"}) || info.WorkingDir != "/w" || info.User != "me" || !info.Tty || info.Labels["l"] != "1" {
				t.Errorf("config = %+v", info)
			}
			if kind == containerapi.KindPodman && !reflect.DeepEqual(info.Entrypoint, []string{"/entry", "--x"}) {
				t.Errorf("Podman writes the entrypoint as a string; got %v", info.Entrypoint)
			}
			if kind == containerapi.KindDocker && info.Entrypoint != nil {
				t.Errorf("the Docker fake writes null; got %v", info.Entrypoint)
			}
			if bind := info.Ports["90/tcp"]; len(bind) != 1 || bind[0] != (containerapi.PortBinding{HostIP: "127.0.0.1", HostPort: "9090"}) {
				t.Errorf("ports = %+v", info.Ports)
			}
			if !reflect.DeepEqual(info.Networks, []string{"bridge"}) {
				t.Errorf("networks = %v", info.Networks)
			}
			if len(info.Mounts) != 1 || info.Mounts[0] != (containerapi.Mount{Type: "bind", Source: "/etc/hostname", Destination: "/h", RW: false}) {
				t.Errorf("mounts = %+v", info.Mounts)
			}
			var generic map[string]any
			if err := json.Unmarshal(info.Raw, &generic); err != nil || generic["Id"] != res.ID {
				t.Errorf("Raw is not the engine's JSON: %v", err)
			}
			if err := c.StopContainer(bg, res.ID, 0); err != nil {
				t.Fatal(err)
			}
			info, _ = c.InspectContainer(bg, res.ID)
			if info.State.Running || info.State.FinishedAt.IsZero() {
				t.Errorf("after the stop: %+v", info.State)
			}
		})
	}
}

func TestPruneContainersOnlyTouchesWhatTheFiltersAllow(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			mine := map[string]string{"dev.emetos.test": "1"}
			a := f.AddContainer("mine-stopped-1", containerapitest.ImageBusyBox, mine, "exited")
			b := f.AddContainer("mine-stopped-2", containerapitest.ImageBusyBox, mine, "exited")
			f.AddContainer("mine-running", containerapitest.ImageBusyBox, mine, "running")
			f.AddContainer("theirs-stopped", containerapitest.ImageBusyBox, nil, "exited")
			res, err := c.PruneContainers(bg, containerapitest.LabelFilter())
			if err != nil {
				t.Fatal(err)
			}
			sort.Strings(res.Deleted)
			want := []string{a, b}
			sort.Strings(want)
			if !reflect.DeepEqual(res.Deleted, want) || res.SpaceReclaimed != 200 {
				t.Errorf("result = %+v, want %v and 200 bytes", res, want)
			}
			left, _ := c.ListContainers(bg, containerapi.ListContainersOptions{All: true})
			var names []string
			for _, x := range left {
				names = append(names, x.Name())
			}
			sort.Strings(names)
			if !reflect.DeepEqual(names, []string{"mine-running", "theirs-stopped"}) {
				t.Errorf("left = %v", names)
			}
			if q := queryOf(t, lastRequest(t, f, http.MethodPost, "/containers/prune")); q.Get("filters") != `{"label":["dev.emetos.test=1"]}` {
				t.Errorf("prune query = %v", q)
			}
			res, err = c.PruneContainers(bg, nil)
			if err != nil || len(res.Deleted) != 1 {
				t.Errorf("an unfiltered prune = %+v, %v; want the one stopped container left", res, err)
			}
		})
	}
}
