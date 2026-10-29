package containerui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

func TestImageRepository(t *testing.T) {
	tests := []struct{ ref, want string }{
		{"postgres", "postgres"},
		{"postgres:17.6-alpine", "postgres"},
		{"docker.io/library/postgres:17.6-alpine", "postgres"},
		{"library/postgres", "postgres"},
		{"index.docker.io/library/mysql:8.4.6", "mysql"},
		{"docker.io/valkey/valkey:8", "valkey/valkey"},
		{"valkey/valkey", "valkey/valkey"},
		{"mcr.microsoft.com/mssql/server:2022-latest", "mcr.microsoft.com/mssql/server"},
		{"POSTGRES:16", "postgres"},
		{"postgres@sha256:0123456789abcdef", "postgres"},
		{"postgres:16@sha256:0123456789abcdef", "postgres"},
		{"localhost:5000/postgres", "localhost:5000/postgres"}, // a port in the registry is not a tag
		{"  redis:8.2.2 ", "redis"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			if got := imageRepository(tt.ref); got != tt.want {
				t.Errorf("imageRepository(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

func TestIsDatabaseImage(t *testing.T) {
	tests := []struct {
		image string
		want  bool
	}{
		{"postgres:16", true}, {"docker.io/library/mysql:8.4.6", true}, {"mariadb:11.4.8", true},
		{"mcr.microsoft.com/mssql/server:2022-latest", true}, {"mongo:7.0.14", true}, {"redis:8.2.2", true}, {"valkey/valkey:8", true},
		{"nginx:1.27", false}, {"postgres-exporter:1", false}, {"bitnami/postgresql:16", false}, {"mssql/server", false},
		{"redis-stack", false}, {"valkey", false}, {"", false},
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			if got := IsDatabaseImage(tt.image); got != tt.want {
				t.Errorf("IsDatabaseImage(%q) = %v, want %v", tt.image, got, tt.want)
			}
		})
	}
}

// inspectJSON writes the inspect answer of a container in the shape of Docker or of Podman: the image the container was made from
// (Config.Image for Docker, ImageName for Podman), the state, the environment and the ports ("5432/tcp" and the bindings).
func inspectJSON(t *testing.T, podman bool, image string, running bool, env []string, ports map[string][]map[string]string) []byte {
	t.Helper()
	cfg := map[string]any{"Env": env, "Image": image, "Labels": map[string]string{}}
	doc := map[string]any{
		"Id": "0123456789abcdef0123456789abcdef", "Created": "2026-10-29T10:00:00Z", "Image": "sha256:feed",
		"State": map[string]any{"Status": "running", "Running": running}, "Config": cfg,
		"NetworkSettings": map[string]any{"Ports": ports, "Networks": map[string]any{"bridge": map[string]any{}}},
	}
	if podman {
		doc["Name"], doc["ImageName"] = "db", image
	} else {
		doc["Name"] = "/db"
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func bindings(pairs ...string) []map[string]string {
	var out []map[string]string
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, map[string]string{"HostIp": pairs[i], "HostPort": pairs[i+1]})
	}
	return out
}

// Every image of SPEC §10.24 gives the engine, the host and port from the published port, and the user and database from the image's
// environment; the passwords in the environment are never in the target.
func TestDatabaseTargetOf(t *testing.T) {
	const secret = "s3cret-value-must-not-travel"
	tests := []struct {
		name       string
		podman     bool
		image      string
		env        []string
		ports      map[string][]map[string]string
		engineHost string
		want       DatabaseTarget
	}{
		{name: "postgres with nothing set", image: "postgres:17.6-alpine", env: []string{"POSTGRES_PASSWORD=" + secret, "PATH=/usr/bin"},
			ports: map[string][]map[string]string{"5432/tcp": bindings("127.0.0.1", "55432")},
			want:  DatabaseTarget{Engine: "postgres", Name: "db", Host: "127.0.0.1", Port: 55432, User: "postgres", Database: "postgres"}},
		{name: "postgres with a user and a database", image: "docker.io/library/postgres:16", env: []string{"POSTGRES_USER=app", "POSTGRES_DB=shop", "POSTGRES_PASSWORD=" + secret},
			ports: map[string][]map[string]string{"5432/tcp": bindings("127.0.0.1", "5433")},
			want:  DatabaseTarget{Engine: "postgres", Name: "db", Host: "127.0.0.1", Port: 5433, User: "app", Database: "shop"}},
		{name: "postgres with a user only names the database after it", image: "postgres:16", env: []string{"POSTGRES_USER=app"},
			ports: map[string][]map[string]string{"5432/tcp": bindings("127.0.0.1", "5433")},
			want:  DatabaseTarget{Engine: "postgres", Name: "db", Host: "127.0.0.1", Port: 5433, User: "app", Database: "app"}},
		{name: "postgres published on every address", image: "postgres:16", ports: map[string][]map[string]string{"5432/tcp": bindings("0.0.0.0", "5433", "::", "5433")},
			want: DatabaseTarget{Engine: "postgres", Name: "db", Host: "127.0.0.1", Port: 5433, User: "postgres", Database: "postgres"}},
		{name: "postgres published on an empty address", image: "postgres:16", ports: map[string][]map[string]string{"5432/tcp": bindings("", "5433")},
			want: DatabaseTarget{Engine: "postgres", Name: "db", Host: "127.0.0.1", Port: 5433, User: "postgres", Database: "postgres"}},
		{name: "postgres published on one address", image: "postgres:16", ports: map[string][]map[string]string{"5432/tcp": bindings("192.0.2.7", "5433")},
			want: DatabaseTarget{Engine: "postgres", Name: "db", Host: "192.0.2.7", Port: 5433, User: "postgres", Database: "postgres"}},
		{name: "postgres on an engine on another computer", image: "postgres:16", engineHost: "build.example.com",
			ports: map[string][]map[string]string{"5432/tcp": bindings("0.0.0.0", "5433")},
			want:  DatabaseTarget{Engine: "postgres", Name: "db", Host: "build.example.com", Port: 5433, User: "postgres", Database: "postgres"}},
		{name: "a binding without a port is skipped", image: "postgres:16", ports: map[string][]map[string]string{"5432/tcp": bindings("127.0.0.1", "", "127.0.0.1", "5434")},
			want: DatabaseTarget{Engine: "postgres", Name: "db", Host: "127.0.0.1", Port: 5434, User: "postgres", Database: "postgres"}},
		{name: "mysql as root", image: "mysql:8.4.6", env: []string{"MYSQL_ROOT_PASSWORD=" + secret, "MYSQL_DATABASE=shop"},
			ports: map[string][]map[string]string{"3306/tcp": bindings("127.0.0.1", "13306")},
			want:  DatabaseTarget{Engine: "mysql", Name: "db", Host: "127.0.0.1", Port: 13306, User: "root", Database: "shop"}},
		{name: "mysql with a user", image: "docker.io/library/mysql:8.4.6", env: []string{"MYSQL_USER=app", "MYSQL_PASSWORD=" + secret, "MYSQL_DATABASE=shop"},
			ports: map[string][]map[string]string{"3306/tcp": bindings("127.0.0.1", "13306")},
			want:  DatabaseTarget{Engine: "mysql", Name: "db", Host: "127.0.0.1", Port: 13306, User: "app", Database: "shop"}},
		{name: "mariadb with its own variables", image: "mariadb:11.4.8", env: []string{"MARIADB_USER=app", "MARIADB_DATABASE=shop", "MARIADB_ROOT_PASSWORD=" + secret},
			ports: map[string][]map[string]string{"3306/tcp": bindings("127.0.0.1", "13307")},
			want:  DatabaseTarget{Engine: "mysql", Name: "db", Host: "127.0.0.1", Port: 13307, User: "app", Database: "shop"}},
		{name: "mariadb with the variables of mysql", image: "mariadb:11.4.8", env: []string{"MYSQL_USER=app", "MYSQL_DATABASE=shop"},
			ports: map[string][]map[string]string{"3306/tcp": bindings("127.0.0.1", "13307")},
			want:  DatabaseTarget{Engine: "mysql", Name: "db", Host: "127.0.0.1", Port: 13307, User: "app", Database: "shop"}},
		{name: "mariadb as root", image: "mariadb:11.4.8", env: []string{"MARIADB_ROOT_PASSWORD=" + secret},
			ports: map[string][]map[string]string{"3306/tcp": bindings("127.0.0.1", "13307")},
			want:  DatabaseTarget{Engine: "mysql", Name: "db", Host: "127.0.0.1", Port: 13307, User: "root"}},
		{name: "sql server", image: "mcr.microsoft.com/mssql/server:2022-latest", env: []string{"MSSQL_SA_PASSWORD=" + secret, "SA_PASSWORD=" + secret},
			ports: map[string][]map[string]string{"1433/tcp": bindings("127.0.0.1", "11433")},
			want:  DatabaseTarget{Engine: "sqlserver", Name: "db", Host: "127.0.0.1", Port: 11433, User: "sa"}},
		{name: "mongo with a root user", image: "mongo:7.0.14", env: []string{"MONGO_INITDB_ROOT_USERNAME=admin", "MONGO_INITDB_ROOT_PASSWORD=" + secret, "MONGO_INITDB_DATABASE=shop"},
			ports: map[string][]map[string]string{"27017/tcp": bindings("127.0.0.1", "27018")},
			want:  DatabaseTarget{Engine: "mongodb", Name: "db", Host: "127.0.0.1", Port: 27018, User: "admin", Database: "admin"}},
		{name: "mongo without authentication", image: "mongo:7.0.14", ports: map[string][]map[string]string{"27017/tcp": bindings("127.0.0.1", "27018")},
			want: DatabaseTarget{Engine: "mongodb", Name: "db", Host: "127.0.0.1", Port: 27018}},
		{name: "redis", image: "redis:8.2.2", env: []string{"REDIS_PASSWORD=" + secret}, ports: map[string][]map[string]string{"6379/tcp": bindings("127.0.0.1", "16379")},
			want: DatabaseTarget{Engine: "redis", Name: "db", Host: "127.0.0.1", Port: 16379}},
		{name: "valkey", image: "docker.io/valkey/valkey:8", ports: map[string][]map[string]string{"6379/tcp": bindings("127.0.0.1", "16380")},
			want: DatabaseTarget{Engine: "redis", Name: "db", Host: "127.0.0.1", Port: 16380}},
		{name: "podman shapes the answer in its own way", podman: true, image: "docker.io/library/postgres:17", env: []string{"POSTGRES_USER=app"},
			ports: map[string][]map[string]string{"5432/tcp": bindings("", "5435")},
			want:  DatabaseTarget{Engine: "postgres", Name: "db", Host: "127.0.0.1", Port: 5435, User: "app", Database: "app"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := containerapi.ParseInspect(inspectJSON(t, tt.podman, tt.image, true, tt.env, tt.ports))
			if err != nil {
				t.Fatal(err)
			}
			got, err := DatabaseTargetOf(info, tt.engineHost)
			if err != nil {
				t.Fatalf("DatabaseTargetOf: %v", err)
			}
			if got != tt.want {
				t.Errorf("target = %+v, want %+v", got, tt.want)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), secret) || strings.Contains(strings.ToLower(string(raw)), "password") {
				t.Errorf("the target carries a password: %s", raw)
			}
		})
	}
}

// A container that cannot be connected to says why, each with its own tag and route.
func TestDatabaseTargetOfFailures(t *testing.T) {
	published := map[string][]map[string]string{"5432/tcp": bindings("127.0.0.1", "5433")}
	tests := []struct {
		name    string
		image   string
		running bool
		ports   map[string][]map[string]string
		route   string
		mention string
	}{
		{"an image that is not a database", "nginx:1.27", true, published, "DatabaseTargetOf.ContainerUIDatabaseImageUnknown", "nginx:1.27"},
		{"a container that is stopped", "postgres:16", false, nil, "DatabaseTargetOf.ContainerUIDatabaseNotRunning", "not running"},
		{"a port that is not published", "postgres:16", true, map[string][]map[string]string{"5432/tcp": nil}, "DatabaseTargetOf.ContainerUIDatabasePortNotPublished", "port 5432"},
		{"another port published", "postgres:16", true, map[string][]map[string]string{"80/tcp": bindings("127.0.0.1", "8080")}, "DatabaseTargetOf.ContainerUIDatabasePortNotPublished", "-p"},
		{"a published port that is no number", "redis:8.2.2", true, map[string][]map[string]string{"6379/tcp": bindings("127.0.0.1", "none")}, "DatabaseTargetOf.ContainerUIDatabasePortNotPublished", "port 6379"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := containerapi.ParseInspect(inspectJSON(t, false, tt.image, tt.running, nil, tt.ports))
			if err != nil {
				t.Fatal(err)
			}
			_, err = DatabaseTargetOf(info, "")
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if !strings.Contains(appErr.Message, tt.mention) {
				t.Errorf("message %q does not mention %q", appErr.Message, tt.mention)
			}
		})
	}
}

func TestEngineHostOf(t *testing.T) {
	tests := []struct {
		ep   containerapi.Endpoint
		want string
	}{
		{containerapi.UnixEndpoint("/run/docker.sock"), ""},
		{containerapi.Endpoint{Network: "tcp", Address: "build.example.com:2375"}, "build.example.com"},
		{containerapi.Endpoint{Network: "tcp", Address: "192.0.2.10:2376"}, "192.0.2.10"},
		{containerapi.Endpoint{Network: "tcp", Address: "[2001:db8::1]:2376"}, "2001:db8::1"},
		{containerapi.Endpoint{Network: "tcp", Address: "noport"}, "noport"},
	}
	for _, tt := range tests {
		if got := engineHostOf(tt.ep); got != tt.want {
			t.Errorf("engineHostOf(%+v) = %q, want %q", tt.ep, got, tt.want)
		}
	}
}

// addDatabase makes and starts a container of the fake engine with the environment and the published port of a database.
func addDatabase(t *testing.T, r *rig, name, image string, env []string, port uint16, hostPort uint16) string {
	t.Helper()
	ctx := context.Background()
	c := r.fake.Client()
	spec := containerapi.ContainerSpec{Name: name, Image: image, Env: env}
	if hostPort != 0 {
		spec.Ports = []containerapi.PortMapping{{HostIP: "127.0.0.1", HostPort: hostPort, ContainerPort: port}}
	}
	res, err := c.CreateContainer(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StartContainer(ctx, res.ID); err != nil {
		t.Fatal(err)
	}
	r.ext.refreshAll()
	r.wait("the list has "+name, func() bool {
		for _, ct := range r.ext.containers {
			if ct.Name() == name {
				return true
			}
		}
		return false
	})
	return res.ID
}

// Connect to Database publishes the event of the extension with the target of the container; the menu and the command offer it only
// for a running container of a known image.
func TestConnectDatabasePublishesTheEvent(t *testing.T) {
	r := newRig(t, func(t testing.TB, f *containerapitest.Fake) {
		sampleEngine(t, f)
		f.AddImage(1, "postgres:17.6-alpine")
		f.AddImage(1, "redis:8.2.2")
	})
	id := addDatabase(t, r, "orders-db", "postgres:17.6-alpine", []string{"POSTGRES_USER=app", "POSTGRES_DB=orders", "POSTGRES_PASSWORD=hunter2"}, 5432, 15432)
	redisID := addDatabase(t, r, "cache", "redis:8.2.2", nil, 6379, 0)

	if got := r.cfg.ConnectDatabaseTopic(); got != "emetos.containers.connectDatabase" {
		t.Errorf("topic = %q", got)
	}
	r.run("containers.connectDatabase", key(keyOfContainer(id)))
	r.wait("the event", func() bool { return len(r.api.PublishedEvents()) > 0 && hasTopic(r, r.cfg.ConnectDatabaseTopic()) })
	var got DatabaseTarget
	for _, p := range r.api.PublishedEvents() {
		if p.Topic == r.cfg.ConnectDatabaseTopic() {
			if err := json.Unmarshal(p.Data, &got); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(p.Data), "hunter2") {
				t.Errorf("the event carries the password: %s", p.Data)
			}
		}
	}
	want := DatabaseTarget{Engine: "postgres", Name: "orders-db", Host: "127.0.0.1", Port: 15432, User: "app", Database: "orders"}
	if got != want {
		t.Errorf("target = %+v, want %+v", got, want)
	}

	// a database whose port is not published says so and publishes nothing
	before := len(r.api.PublishedEvents())
	r.run("containers.connectDatabase", key(keyOfContainer(redisID)))
	r.wait("the note", func() bool { return r.hasNote("does not publish port 6379") })
	if len(r.api.PublishedEvents()) != before {
		t.Errorf("an event was published for a container with no published port")
	}

	// the menu: a running database has the item; a web server, and a stopped database, have not
	has := func(keys ...string) bool {
		for _, it := range r.view(secContainers).sec.menu(r.view(secContainers), keys) {
			if it.Label == "Connect to Database" {
				return true
			}
		}
		return false
	}
	if !has(keyOfContainer(id)) {
		t.Error("a running database has no Connect to Database in its menu")
	}
	if has(keyOfContainer(r.id("shop-web-1"))) {
		t.Error("a web server has Connect to Database")
	}
	if has(keyOfContainer(id), keyOfContainer(redisID)) {
		t.Error("two containers have Connect to Database")
	}
	r.run("containers.stop", key(keyOfContainer(id)))
	r.wait("stopped", func() bool { return r.state("orders-db") != "running" })
	r.settle()
	if has(keyOfContainer(id)) {
		t.Error("a stopped database has Connect to Database")
	}
}

func hasTopic(r *rig, topic string) bool {
	for _, p := range r.api.PublishedEvents() {
		if p.Topic == topic {
			return true
		}
	}
	return false
}

// Podman publishes its own topic, with the same target, from its own answer to inspect.
func TestConnectDatabaseOnPodman(t *testing.T) {
	r, _ := newPodmanRig(t, func(t testing.TB, f *containerapitest.Fake) {
		f.AddImage(1, "docker.io/library/mariadb:11.4.8")
	})
	ctx := context.Background()
	c := r.fake.Client()
	res, err := c.CreateContainer(ctx, containerapi.ContainerSpec{Name: "maria", Image: "docker.io/library/mariadb:11.4.8",
		Env: []string{"MARIADB_DATABASE=shop", "MARIADB_ROOT_PASSWORD=hunter2"}, Ports: []containerapi.PortMapping{{HostIP: "127.0.0.1", HostPort: 13306, ContainerPort: 3306}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.StartContainer(ctx, res.ID); err != nil {
		t.Fatal(err)
	}
	r.ext.refreshAll()
	r.wait("the container", func() bool { return len(r.ext.containers) == 1 })

	r.run("containers.connectDatabase", key(keyOfContainer(res.ID)))
	topic := "emetos.podman.connectDatabase"
	r.wait("the event", func() bool { return hasTopic(r, topic) })
	for _, p := range r.api.PublishedEvents() {
		if p.Topic != topic {
			continue
		}
		var got DatabaseTarget
		if err := json.Unmarshal(p.Data, &got); err != nil {
			t.Fatal(err)
		}
		if want := (DatabaseTarget{Engine: "mysql", Name: "maria", Host: "127.0.0.1", Port: 13306, User: "root", Database: "shop"}); got != want {
			t.Errorf("target = %+v, want %+v", got, want)
		}
	}
}
