package containerui

import (
	"net"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// Connect to Database (SPEC §10.24 "A container's database"): a running container of an image the database extensions know has the
// action Connect to Database, which publishes the event ConnectDatabaseTopic with the DatabaseTarget made from the container's
// inspect: the published port and what the image's environment says about the user and the database. The password is never read:
// POSTGRES_PASSWORD, MYSQL_ROOT_PASSWORD, MONGO_INITDB_ROOT_PASSWORD, MSSQL_SA_PASSWORD and the like are not looked at, and the target
// has no field that could hold one. The database extension of the engine opens its connection form with the target (extensions/internal/dbconn).

// ConnectDatabaseTopic is the topic of the event Connect to Database publishes: the id of the extension and ".connectDatabase"
// (emetos.containers.connectDatabase, emetos.podman.connectDatabase).
func (c Config) ConnectDatabaseTopic() string { return c.ID + ".connectDatabase" }

// DatabaseTarget is the payload of the event: the engine of the database extensions and where and as whom to connect.
type DatabaseTarget struct {
	// Engine is the id of the engine: postgres, mysql (also for MariaDB), sqlserver, mongodb or redis (also for Valkey).
	Engine string `json:"engine"`
	// Name is the name of the container, which names the connection.
	Name string `json:"name"`
	// Host and Port are where the server's port is published.
	Host string `json:"host"`
	Port int    `json:"port"`
	// User and Database are the user and the database the image creates; empty when the image has no such thing.
	User     string `json:"user,omitempty"`
	Database string `json:"database,omitempty"`
}

// databaseKind is an image the database extensions know.
type databaseKind struct {
	// repository is the image without the registry, the tag and the digest ("postgres", "valkey/valkey").
	repository string
	// engine is the engine of the database extensions that connects to it.
	engine string
	// port is the port the server listens on in the container.
	port uint16
	// target fills the user and the database from the environment of the container.
	target func(env map[string]string) (user, database string)
}

// first returns the value of the first variable that is set and not empty.
func first(env map[string]string, names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(env[n]); v != "" {
			return v
		}
	}
	return ""
}

// appUser is the connection of the images of MySQL and MariaDB: the user the image creates when it is told to (MYSQL_USER), else root,
// and the database it creates (MYSQL_DATABASE).
func appUser(userVars, dbVars []string) func(map[string]string) (string, string) {
	return func(env map[string]string) (string, string) {
		user := first(env, userVars...)
		if user == "" {
			user = "root"
		}
		return user, first(env, dbVars...)
	}
}

// databaseKinds are the images of SPEC §10.24, in the order they are looked for.
var databaseKinds = []databaseKind{
	{repository: "postgres", engine: "postgres", port: 5432, target: func(env map[string]string) (string, string) {
		user := first(env, "POSTGRES_USER")
		if user == "" {
			user = "postgres"
		}
		db := first(env, "POSTGRES_DB")
		if db == "" {
			db = user // the image names the database after the user
		}
		return user, db
	}},
	{repository: "mysql", engine: "mysql", port: 3306, target: appUser([]string{"MYSQL_USER"}, []string{"MYSQL_DATABASE"})},
	{repository: "mariadb", engine: "mysql", port: 3306, target: appUser([]string{"MARIADB_USER", "MYSQL_USER"}, []string{"MARIADB_DATABASE", "MYSQL_DATABASE"})},
	{repository: "mcr.microsoft.com/mssql/server", engine: "sqlserver", port: 1433, target: func(map[string]string) (string, string) {
		return "sa", "" // the image's administrator; the password (MSSQL_SA_PASSWORD) is not read
	}},
	{repository: "mongo", engine: "mongodb", port: 27017, target: func(env map[string]string) (string, string) {
		user := first(env, "MONGO_INITDB_ROOT_USERNAME")
		if user == "" {
			return "", "" // the image starts without authentication
		}
		return user, "admin" // the root user is created in the admin database, which is where it logs in
	}},
	{repository: "redis", engine: "redis", port: 6379},
	{repository: "valkey/valkey", engine: "redis", port: 6379},
}

// registryPrefixes are the names of Docker Hub that an image may be written with; the official images are in the library.
var registryPrefixes = []string{"docker.io/", "index.docker.io/", "registry-1.docker.io/", "registry.hub.docker.com/"}

// imageRepository returns the repository of an image reference without the registry of Docker Hub, the library, the tag and the
// digest, in lower case: "docker.io/library/postgres:17.6-alpine" is "postgres", "mcr.microsoft.com/mssql/server:2022-latest" stays
// "mcr.microsoft.com/mssql/server".
func imageRepository(ref string) string {
	ref = strings.ToLower(strings.TrimSpace(ref))
	ref, _, _ = strings.Cut(ref, "@")
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	for _, p := range registryPrefixes {
		if rest, ok := strings.CutPrefix(ref, p); ok {
			ref = rest
			break
		}
	}
	return strings.TrimPrefix(ref, "library/")
}

// databaseKindOf returns the kind of database an image holds.
func databaseKindOf(image string) (databaseKind, bool) {
	repo := imageRepository(image)
	for _, k := range databaseKinds {
		if k.repository == repo {
			return k, true
		}
	}
	return databaseKind{}, false
}

// IsDatabaseImage tells whether an image is one the database extensions know (a menu item is offered for such containers only).
func IsDatabaseImage(image string) bool {
	_, ok := databaseKindOf(image)
	return ok
}

// environmentOf reads the "NAME=value" lines of a container's environment.
func environmentOf(lines []string) map[string]string {
	env := make(map[string]string, len(lines))
	for _, l := range lines {
		if name, value, ok := strings.Cut(l, "="); ok {
			env[name] = value
		}
	}
	return env
}

// engineHostOf is the host of a TCP engine (a Docker host on another computer), where its published ports are; empty for a socket.
func engineHostOf(ep containerapi.Endpoint) string {
	if ep.Network != "tcp" {
		return ""
	}
	host, _, err := net.SplitHostPort(ep.Address)
	if err != nil {
		return ep.Address
	}
	return host
}

// loopback is the address a port that is published on every address is reached by on this computer.
const loopback = "127.0.0.1"

// DatabaseTargetOf makes the target of Connect to Database from the inspect of a container. engineHost is the host of the engine when
// it is on another computer (tcp://), else empty: a port published on every address is reached there, otherwise on this computer's
// loopback address. It fails with ContainerUIDatabaseImageUnknown (the image is none of SPEC §10.24's), ContainerUIDatabaseNotRunning
// and ContainerUIDatabasePortNotPublished (the server's port is not published on the host).
func DatabaseTargetOf(info *containerapi.ContainerInspect, engineHost string) (DatabaseTarget, error) {
	kind, ok := databaseKindOf(info.ImageName)
	if !ok {
		return DatabaseTarget{}, errors.New("ContainerUIDatabaseImageUnknown", "the image "+info.ImageName+" is not one of the databases Emetos knows", "container", info.Name, "image", info.ImageName)
	}
	if !info.State.Running {
		return DatabaseTarget{}, errors.New("ContainerUIDatabaseNotRunning", info.Name+" is not running: start it first", "container", info.Name)
	}
	host, port, ok := publishedAddress(info.Ports[strconv.Itoa(int(kind.port))+"/tcp"], engineHost)
	if !ok {
		return DatabaseTarget{}, errors.New("ContainerUIDatabasePortNotPublished",
			info.Name+" does not publish port "+strconv.Itoa(int(kind.port))+" on the host: run it with -p to connect from here", "container", info.Name, "port", int(kind.port))
	}
	t := DatabaseTarget{Engine: kind.engine, Name: info.Name, Host: host, Port: port}
	if kind.target != nil {
		t.User, t.Database = kind.target(environmentOf(info.Env))
	}
	return t, nil
}

// publishedAddress finds where the first binding with a port is reached: its address, or when that is every address, the engine's host
// or the loopback address.
func publishedAddress(bindings []containerapi.PortBinding, engineHost string) (host string, port int, ok bool) {
	for _, b := range bindings {
		n, err := strconv.Atoi(b.HostPort)
		if err != nil || n < 1 || n > 65535 {
			continue
		}
		host = b.HostIP
		switch host {
		case "", "0.0.0.0", "::":
			host = loopback
			if engineHost != "" {
				host = engineHost
			}
		}
		return host, n, true
	}
	return "", 0, false
}

// connectDatabase publishes the event of Connect to Database for a container: it reads the container's inspect off the UI thread and
// says why when the container cannot be connected to.
func (e *ext) connectDatabase(c containerapi.Container) {
	client, ok := e.ready()
	if !ok {
		return
	}
	engineHost := engineHostOf(client.Endpoint())
	go func() {
		info, err := client.InspectContainer(e.ctx, c.ID)
		var target DatabaseTarget
		if err == nil {
			target, err = DatabaseTargetOf(info, engineHost)
		}
		e.api.Post(func() {
			if err != nil {
				e.api.Notifications().Warn("Cannot connect to the database: " + reasonOf(err))
				return
			}
			if err := e.api.Events().Publish(e.cfg.ConnectDatabaseTopic(), target); err != nil {
				e.api.Log().Warn("the event of Connect to Database cannot be published", "err", err)
			}
		})
	}()
}
