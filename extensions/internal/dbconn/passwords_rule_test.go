package dbconn

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// The rule of D24, written first: a password is never written to a file of Emetos. These tests look at every byte the store
// writes, at the fields of the types that are written, and at what the guard refuses.

const secretWord = "hunter2-VERY-secret"

// TestAConnectionHasNoFieldForAPassword looks at the types that reach the file: no field of Connection (or of the file's
// envelope) is named like a password or a secret, and no field is a place a password could be put in by mistake except the URL
// and the options, which Check guards.
func TestAConnectionHasNoFieldForAPassword(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(Connection{}), reflect.TypeOf(fileFormat{})} {
		for i := range typ.NumField() {
			f := typ.Field(i)
			name := strings.ToLower(f.Name + " " + f.Tag.Get("json"))
			if f.Name == "PasswordEnv" || f.Name == "SavePassword" {
				continue // a reference to a variable, and a flag: neither holds a password
			}
			if isSecretName(name) {
				t.Errorf("%s.%s looks like a place for a secret", typ.Name(), f.Name)
			}
		}
	}
}

// TestNoPasswordReachesAnyFile saves connections the ways the form does, with a password in every place the user could put one,
// and looks at every file under the folders that were isolated for the test.
func TestNoPasswordReachesAnyFile(t *testing.T) {
	tests := []struct {
		name string
		do   func(t *testing.T, st *Store, pw *Passwords)
	}{
		{"saved to the keyring", func(t *testing.T, st *Store, pw *Passwords) {
			c, err := st.Save(Connection{Name: "a", Engine: "postgres", Scope: ScopeUser, Host: "h", SavePassword: true})
			if err != nil {
				t.Fatal(err)
			}
			if got := pw.Save(context.Background(), c, secretWord); got.In != SourceKeyring {
				t.Fatalf("saved in %v", got.In)
			}
		}},
		{"kept in memory", func(t *testing.T, st *Store, pw *Passwords) {
			c, err := st.Save(Connection{Name: "a", Engine: "postgres", Scope: ScopeUser, Host: "h"})
			if err != nil {
				t.Fatal(err)
			}
			pw.Save(context.Background(), c, secretWord)
		}},
		{"asked to save with no keyring that answers", func(t *testing.T, st *Store, _ *Passwords) {
			bad := NewPasswords(&MemoryKeyring{Err: errors.New("KeyringFake", "down")}, os.LookupEnv)
			c, err := st.Save(Connection{Name: "a", Engine: "postgres", Scope: ScopeUser, Host: "h", SavePassword: true})
			if err != nil {
				t.Fatal(err)
			}
			got := bad.Save(context.Background(), c, secretWord)
			if got.In != SourceMemory || got.KeyringErr == nil {
				t.Fatalf("saved = %+v", got)
			}
		}},
		{"asked to save with no keyring at all", func(t *testing.T, st *Store, _ *Passwords) {
			none := NewPasswords(nil, os.LookupEnv)
			c, err := st.Save(Connection{Name: "a", Engine: "postgres", Scope: ScopeUser, Host: "h", SavePassword: true})
			if err != nil {
				t.Fatal(err)
			}
			if got := none.Save(context.Background(), c, secretWord); got.In != SourceMemory || got.KeyringErr == nil {
				t.Fatalf("saved = %+v", got)
			}
		}},
		{"from the environment", func(t *testing.T, st *Store, _ *Passwords) {
			t.Setenv("PGPASSWORD_TEST", secretWord)
			c, err := st.Save(Connection{Name: "a", Engine: "postgres", Scope: ScopeUser, Host: "h", PasswordEnv: EnvRef("PGPASSWORD_TEST")})
			if err != nil {
				t.Fatal(err)
			}
			got, err := NewPasswords(nil, os.LookupEnv).Resolve(context.Background(), c)
			if err != nil || got.Password != secretWord || got.Source != SourceEnv {
				t.Fatalf("resolved = %+v, %v", got, err)
			}
		}},
		{"a pasted URL has its password taken out", func(t *testing.T, st *Store, _ *Passwords) {
			clean, pass := StripURLPassword("mongodb://admin:" + secretWord + "@h1:27017,h2:27017/db?replicaSet=rs")
			if pass != secretWord {
				t.Fatalf("password = %q", pass)
			}
			if _, err := st.Save(Connection{Name: "m", Engine: "mongodb", Scope: ScopeUser, URL: clean}); err != nil {
				t.Fatal(err)
			}
		}},
		{"edited and saved again", func(t *testing.T, st *Store, pw *Passwords) {
			c, err := st.Save(Connection{Name: "a", Engine: "postgres", Scope: ScopeUser, Host: "h", SavePassword: true})
			if err != nil {
				t.Fatal(err)
			}
			pw.Save(context.Background(), c, secretWord)
			c.Name = "b"
			if _, err := st.Save(c); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := stateHome(t)
			path := dir + "/" + FileName
			st := NewStore(path)
			tt.do(t, st, NewPasswords(&MemoryKeyring{}, os.LookupEnv))
			root := os.Getenv("HOME")
			for file, text := range readAll(t, root) {
				if strings.Contains(text, secretWord) {
					t.Errorf("%s holds the password", file)
				}
			}
		})
	}
}

// TestAPasswordInAConnectionIsRefused: the guard of the store refuses what would put a password in the file.
func TestAPasswordInAConnectionIsRefused(t *testing.T) {
	base := Connection{Name: "a", Engine: "postgres", Scope: ScopeUser, Host: "h"}
	tests := []struct {
		name  string
		edit  func(c *Connection)
		route string
	}{
		{"in the user information of the URL", func(c *Connection) { c.URL = "postgres://me:" + secretWord + "@h/db" }, "Connection.Check.ConnectionURLHasPassword"},
		{"in the query of the URL", func(c *Connection) { c.URL = "postgres://me@h/db?password=" + secretWord }, "Connection.Check.ConnectionURLHasPassword"},
		{"in a key=value connection string", func(c *Connection) { c.URL = "Server=h;User Id=me;Password=" + secretWord }, "Connection.Check.ConnectionURLHasPassword"},
		{"in an option", func(c *Connection) { c.Options = map[string]string{"sslPassword": secretWord} }, "Connection.Check.ConnectionOptionIsSecret"},
		{"as a token option", func(c *Connection) { c.Options = map[string]string{"authToken": secretWord} }, "Connection.Check.ConnectionOptionIsSecret"},
		{"as the password variable", func(c *Connection) { c.PasswordEnv = secretWord }, "Connection.Check.ConnectionPasswordEnvInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := stateHome(t)
			st := NewStore(dir + "/" + FileName)
			c := base
			tt.edit(&c)
			_, err := st.Save(c)
			wantRoute(t, err, tt.route)
			if _, statErr := os.Stat(dir + "/" + FileName); !os.IsNotExist(statErr) {
				t.Errorf("a refused connection wrote the file (%v)", statErr)
			}
		})
	}
}

// TestAFileWithAPasswordIsNotLoaded: a file edited by hand to carry a password in a URL is refused whole, and not overwritten.
func TestAFileWithAPasswordIsNotLoaded(t *testing.T) {
	dir := stateHome(t)
	path := dir + "/" + FileName
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	bad, err := json.Marshal(fileFormat{Version: fileVersion, Connections: []Connection{
		{ID: "x", Name: "a", Engine: "postgres", Scope: ScopeUser, Host: "h", URL: "postgres://me:" + secretWord + "@h/db"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	st := NewStore(path)
	wantRoute(t, st.Err(), "Store.read->Connection.Check.ConnectionURLHasPassword")
	if len(st.All()) != 0 {
		t.Error("the store shows the connections of a file it refused")
	}
	_, saveErr := st.Save(Connection{Name: "b", Engine: "postgres", Scope: ScopeUser, Host: "h"})
	wantRoute(t, saveErr, "Connection.Check.ConnectionURLHasPassword")
	if after, _ := os.ReadFile(path); string(after) != string(bad) {
		t.Error("the store overwrote a file it could not read")
	}
}
