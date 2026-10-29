package dbconn

import (
	"testing"
)

func TestCheckRefusesWhatACommonPartGetsWrong(t *testing.T) {
	ok := Connection{ID: "x", Name: "n", Engine: "postgres", Scope: ScopeUser, Host: "h", Port: 5432}
	tests := []struct {
		name  string
		edit  func(c *Connection)
		route string // "" for valid
	}{
		{"a valid connection", func(*Connection) {}, ""},
		{"no name", func(c *Connection) { c.Name = "  " }, "Connection.Check.ConnectionNameMissing"},
		{"no engine", func(c *Connection) { c.Engine = "" }, "Connection.Check.ConnectionEngineMissing"},
		{"an unknown scope", func(c *Connection) { c.Scope = "everyone" }, "Connection.Check.ConnectionScopeInvalid"},
		{"a workspace scope without a folder", func(c *Connection) { c.Scope = ScopeWorkspace }, "Connection.Check.ConnectionWorkspaceMissing"},
		{"a workspace scope with a folder", func(c *Connection) { c.Scope, c.Workspace = ScopeWorkspace, "/work" }, ""},
		{"a negative port", func(c *Connection) { c.Port = -1 }, "Connection.Check.ConnectionPortInvalid"},
		{"a port above 65535", func(c *Connection) { c.Port = 65536 }, "Connection.Check.ConnectionPortInvalid"},
		{"no port", func(c *Connection) { c.Port = 0 }, ""},
		{"a color that is not hex", func(c *Connection) { c.Color = "red" }, "Connection.Check.ConnectionColorInvalid"},
		{"a hex color", func(c *Connection) { c.Color = "#A0b1C2" }, ""},
		{"an unknown TLS mode", func(c *Connection) { c.TLS = "yes" }, "Connection.Check.ConnectionTLSInvalid"},
		{"a TLS mode", func(c *Connection) { c.TLS = TLSVerifyFull }, ""},
		{"a reference to a variable", func(c *Connection) { c.PasswordEnv = "${env:PGPASSWORD}" }, ""},
		{"a password variable that is not a reference", func(c *Connection) { c.PasswordEnv = "PGPASSWORD" }, "Connection.Check.ConnectionPasswordEnvInvalid"},
		{"an environment reference in the URL query", func(c *Connection) { c.URL = "postgres://me@h/db?password=${env:PW}" }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ok
			tt.edit(&c)
			err := c.Check()
			if tt.route == "" {
				if err != nil {
					t.Fatalf("Check = %v", err)
				}
				return
			}
			wantRoute(t, err, tt.route)
		})
	}
}

func TestStripURLPassword(t *testing.T) {
	tests := []struct {
		raw, clean, password string
	}{
		{"mongodb://admin:s3cret@h1:27017,h2:27017/db?replicaSet=rs", "mongodb://admin@h1:27017,h2:27017/db?replicaSet=rs", "s3cret"},
		{"mongodb+srv://u:p%40ss@cluster.example.net/db", "mongodb+srv://u@cluster.example.net/db", "p@ss"},
		{"redis://:onlypass@localhost:6379/0", "redis://localhost:6379/0", "onlypass"},
		{"redis://user:pw@localhost", "redis://user@localhost", "pw"},
		{"postgres://me@h/db?sslmode=require", "postgres://me@h/db?sslmode=require", ""},
		{"postgres://me@h/db?password=abc&sslmode=require", "postgres://me@h/db?sslmode=require", "abc"},
		{"postgres://me@h/db?password=abc", "postgres://me@h/db", "abc"},
		{"postgres://me@h/db?password=${env:PW}", "postgres://me@h/db?password=${env:PW}", ""},
		{"mongodb://h/db", "mongodb://h/db", ""},
		{"not a url", "not a url", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			clean, password := StripURLPassword(tt.raw)
			if clean != tt.clean || password != tt.password {
				t.Errorf("StripURLPassword(%q) = %q, %q; want %q, %q", tt.raw, clean, password, tt.clean, tt.password)
			}
			if urlHasSecret(clean) {
				t.Errorf("the clean URL %q still holds a secret", clean)
			}
		})
	}
}

func TestParseEnvRef(t *testing.T) {
	tests := []struct {
		in   string
		name string
		ok   bool
	}{
		{"${env:PGPASSWORD}", "PGPASSWORD", true},
		{" ${env:A_1} ", "A_1", true},
		{"${env:}", "", false},
		{"${env:1A}", "", false},
		{"$env:A", "", false},
		{"${ENV:A}", "", false},
		{"x${env:A}", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		name, ok := ParseEnvRef(tt.in)
		if name != tt.name || ok != tt.ok {
			t.Errorf("ParseEnvRef(%q) = %q, %v; want %q, %v", tt.in, name, ok, tt.name, tt.ok)
		}
	}
	if got := EnvRef("X"); got != "${env:X}" {
		t.Errorf("EnvRef = %q", got)
	}
}

func TestExpandEnvAndExpanded(t *testing.T) {
	env := map[string]string{"HOST": "db.example.com", "EMPTY": "", "USER": "me"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	tests := []struct {
		in    string
		want  string
		route string
	}{
		{"plain", "plain", ""},
		{"${env:HOST}", "db.example.com", ""},
		{"a-${env:HOST}-${env:USER}", "a-db.example.com-me", ""},
		{"${env:EMPTY}", "", ""},
		{"${env:NOPE}", "", "ExpandEnv.ConnectionEnvUnset"},
		{"x ${env:HOST} ${env:NOPE}", "", "ExpandEnv.ConnectionEnvUnset"},
	}
	for _, tt := range tests {
		got, err := ExpandEnv(tt.in, lookup)
		if tt.route != "" {
			wantRoute(t, err, tt.route)
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ExpandEnv(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	c := Connection{Host: "${env:HOST}", User: "${env:USER}", Options: map[string]string{"a": "${env:HOST}"}, PasswordEnv: "${env:PW}"}
	got, err := c.Expanded(lookup)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "db.example.com" || got.User != "me" || got.Options["a"] != "db.example.com" {
		t.Errorf("Expanded = %+v", got)
	}
	if got.PasswordEnv != "${env:PW}" {
		t.Error("the reference to the password must stay: Passwords resolves it")
	}
	if c.Options["a"] != "${env:HOST}" {
		t.Error("Expanded changed the original")
	}
	_, err = Connection{Database: "${env:NOPE}"}.Expanded(lookup)
	wantRoute(t, err, "ExpandEnv.ConnectionEnvUnset")
	_, err = Connection{Options: map[string]string{"k": "${env:NOPE}"}}.Expanded(lookup)
	wantRoute(t, err, "ExpandEnv.ConnectionEnvUnset")
}

func TestVisibleIn(t *testing.T) {
	user := Connection{Scope: ScopeUser}
	ws := Connection{Scope: ScopeWorkspace, Workspace: "/work/a"}
	tests := []struct {
		c      Connection
		folder string
		want   bool
	}{
		{user, "", true}, {user, "/work/a", true}, {ws, "/work/a", true}, {ws, "/work/b", false}, {ws, "", false},
	}
	for _, tt := range tests {
		if got := tt.c.VisibleIn(tt.folder); got != tt.want {
			t.Errorf("%+v.VisibleIn(%q) = %v, want %v", tt.c, tt.folder, got, tt.want)
		}
	}
}

func TestEngineValidate(t *testing.T) {
	pg := Engine{ID: "postgres", Name: "PostgreSQL", DefaultPort: 5432}
	lite := Engine{ID: "sqlite", Name: "SQLite", File: true, NoTLS: true}
	mongo := Engine{ID: "mongodb", Name: "MongoDB", DefaultPort: 27017, URLSchemes: []string{"mongodb", "mongodb+srv"}, Fields: []Field{
		{Key: "authSource", Label: "Auth source"},
		{Key: "mechanism", Label: "Mechanism", Kind: FieldChoice, Choices: []Choice{{Value: "SCRAM"}, {Value: "X509"}}},
		{Key: "timeout", Label: "Timeout", Kind: FieldNumber},
		{Key: "direct", Label: "Direct", Kind: FieldBool},
		{Key: "set", Label: "Replica set", Required: true},
	}}
	base := func(e Engine) Connection { return Connection{Name: "n", Engine: e.ID, Scope: ScopeUser} }
	tests := []struct {
		name  string
		e     Engine
		edit  func(c *Connection)
		route string
	}{
		{"a server", pg, func(c *Connection) { c.Host = "h" }, ""},
		{"a server without a host", pg, func(*Connection) {}, "Engine.Validate.ConnectionHostMissing"},
		{"another engine", pg, func(c *Connection) { c.Host, c.Engine = "h", "mysql" }, "Engine.Validate.ConnectionEngineMismatch"},
		{"a URL for an engine with none", pg, func(c *Connection) { c.URL = "postgres://h/db" }, "Engine.Validate.ConnectionURLSchemeInvalid"},
		{"a file", lite, func(c *Connection) { c.File = "/tmp/a.db" }, ""},
		{"no file", lite, func(*Connection) {}, "Engine.Validate.ConnectionFileMissing"},
		{"a URL with its scheme", mongo, func(c *Connection) { c.URL = "mongodb+srv://u@h/db"; c.Options = map[string]string{"set": "rs"} }, ""},
		{"a URL with another scheme", mongo, func(c *Connection) { c.URL = "redis://h"; c.Options = map[string]string{"set": "rs"} }, "Engine.Validate.ConnectionURLSchemeInvalid"},
		{"a missing required field", mongo, func(c *Connection) { c.Host = "h" }, "Engine.Validate.ConnectionOptionMissing"},
		{"a choice that is not one", mongo, func(c *Connection) { c.Host = "h"; c.Options = map[string]string{"set": "rs", "mechanism": "KERBEROS"} }, "Engine.Validate.ConnectionOptionInvalid"},
		{"a number that is not one", mongo, func(c *Connection) { c.Host = "h"; c.Options = map[string]string{"set": "rs", "timeout": "soon"} }, "Engine.Validate.ConnectionOptionInvalid"},
		{"a number from the environment", mongo, func(c *Connection) { c.Host = "h"; c.Options = map[string]string{"set": "rs", "timeout": "${env:T}"} }, ""},
		{"a bool that is not one", mongo, func(c *Connection) { c.Host = "h"; c.Options = map[string]string{"set": "rs", "direct": "maybe"} }, "Engine.Validate.ConnectionOptionInvalid"},
		{"an option the engine does not have", mongo, func(c *Connection) { c.Host = "h"; c.Options = map[string]string{"set": "rs", "extra": "1"} }, "Engine.Validate.ConnectionOptionUnknown"},
		{"every field filled", mongo, func(c *Connection) {
			c.Host = "h"
			c.Options = map[string]string{"set": "rs", "mechanism": "X509", "timeout": "30", "direct": "true", "authSource": "admin"}
		}, ""},
		{"a common part that is wrong", pg, func(c *Connection) { c.Host = "h"; c.Port = 99999 }, "Connection.Check.ConnectionPortInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base(tt.e)
			tt.edit(&c)
			err := tt.e.Validate(c)
			if tt.route == "" {
				if err != nil {
					t.Fatalf("Validate = %v", err)
				}
				return
			}
			wantRoute(t, err, tt.route)
		})
	}
	if c := mongo.NewConnection(); c.Port != 27017 || c.Scope != ScopeUser || c.Engine != "mongodb" {
		t.Errorf("NewConnection = %+v", c)
	}
	withDefault := Engine{ID: "r", Fields: []Field{{Key: "sep", Default: ":"}}}
	if c := withDefault.NewConnection(); c.Options["sep"] != ":" {
		t.Errorf("the default of a field is not in the new connection: %+v", c)
	}
	if pg.DisplayIcon() != "database" || (Engine{Icon: "x"}).DisplayIcon() != "x" {
		t.Error("DisplayIcon")
	}
}

func TestNewIDIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := NewID()
		if id == "" || seen[id] {
			t.Fatalf("id %q repeated or empty", id)
		}
		seen[id] = true
	}
}
