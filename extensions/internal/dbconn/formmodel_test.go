package dbconn

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

var (
	pgEngine = Engine{ID: "postgres", Name: "PostgreSQL", DefaultPort: 5432}

	sqliteEngine = Engine{ID: "sqlite", Name: "SQLite", File: true, NoTLS: true, NoPassword: true, NoUser: true, NoDatabase: true}

	mongoEngine = Engine{ID: "mongodb", Name: "MongoDB", DefaultPort: 27017, URLSchemes: []string{"mongodb", "mongodb+srv"}, NoDatabase: true,
		Fields: []Field{
			{Key: "authSource", Label: "Auth source", Default: "admin"},
			{Key: "mechanism", Label: "Mechanism", Kind: FieldChoice, Choices: []Choice{{Value: "SCRAM-SHA-256"}, {Value: "MONGODB-X509"}}},
			{Key: "timeout", Label: "Timeout (s)", Kind: FieldNumber},
		}}
)

func TestAFormMakesTheConnectionItDescribes(t *testing.T) {
	m := NewFormModel(pgEngine, "/work/app")
	if m.Port != "5432" || m.Scope != ScopeUser {
		t.Fatalf("a new form starts with port %q, scope %q", m.Port, m.Scope)
	}
	m.Name, m.Host, m.User, m.Database = "  Local  ", " localhost ", "me", "app"
	m.TLS, m.Color, m.ReadOnly = TLSRequire, "#336699", true
	m.Scope = ScopeWorkspace
	m.PasswordEnv = "PGPASSWORD"
	c := m.Connection()
	want := Connection{Name: "Local", Engine: "postgres", Scope: ScopeWorkspace, Workspace: "/work/app", Host: "localhost", Port: 5432, User: "me",
		Database: "app", TLS: TLSRequire, Color: "#336699", ReadOnly: true, PasswordEnv: "${env:PGPASSWORD}"}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("Connection = %+v\nwant %+v", c, want)
	}
	if problems := m.Problems(); len(problems) != 0 {
		t.Errorf("Problems = %v", problems)
	}
	if err := pgEngine.Validate(c); err != nil {
		t.Errorf("the connection of a form with no problems fails Validate: %v", err)
	}
}

func TestAFormForAFileEngineLeavesOutWhatItDoesNotHave(t *testing.T) {
	m := NewFormModel(sqliteEngine, "")
	m.Name, m.File = "Notes", " /home/me/notes.db "
	m.Host, m.User, m.Database, m.TLS, m.PasswordEnv, m.SavePassword = "ignored", "ignored", "ignored", TLSRequire, "X", true
	c := m.Connection()
	want := Connection{Name: "Notes", Engine: "sqlite", Scope: ScopeUser, File: "/home/me/notes.db"}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("Connection = %+v, want %+v", c, want)
	}
	if m.PasswordChange().Set {
		t.Error("an engine with no password has a password change")
	}
	m.SetPassword("x")
	if m.PasswordChange().Set {
		t.Error("an engine with no password has a password change")
	}
}

func TestProblemsOfAForm(t *testing.T) {
	tests := []struct {
		name  string
		e     Engine
		edit  func(m *FormModel)
		field string
	}{
		{"no name", pgEngine, func(m *FormModel) { m.Host = "h"; m.Name = "" }, "name"},
		{"no host", pgEngine, func(m *FormModel) { m.Name = "n" }, "host"},
		{"a port that is a word", pgEngine, func(m *FormModel) { m.Name, m.Host, m.Port = "n", "h", "five" }, "port"},
		{"a port that is 0", pgEngine, func(m *FormModel) { m.Name, m.Host, m.Port = "n", "h", "0" }, "port"},
		{"a port that is too big", pgEngine, func(m *FormModel) { m.Name, m.Host, m.Port = "n", "h", "70000" }, "port"},
		{"a color that is not hex", pgEngine, func(m *FormModel) { m.Name, m.Host, m.Color = "n", "h", "blue" }, "color"},
		{"a variable that is not a name", pgEngine, func(m *FormModel) { m.Name, m.Host, m.PasswordEnv = "n", "h", "my var" }, "passwordEnv"},
		{"a workspace scope without a folder", pgEngine, func(m *FormModel) { m.Name, m.Host, m.Scope = "n", "h", ScopeWorkspace }, "scope"},
		{"no file", sqliteEngine, func(m *FormModel) { m.Name = "n" }, "file"},
		{"a URL for an engine with none", pgEngine, func(m *FormModel) { m.Name, m.URL = "n", "postgres://h" }, "url"},
		{"a URL with another scheme", mongoEngine, func(m *FormModel) { m.Name, m.URL = "n", "redis://h" }, "url"},
		{"a URL with no scheme", mongoEngine, func(m *FormModel) { m.Name, m.URL = "n", "h:27017" }, "url"},
		{"a URL with a password set by hand", mongoEngine, func(m *FormModel) { m.Name, m.URL = "n", "mongodb://u:pw@h" }, "url"},
		{"a number field with a word", mongoEngine, func(m *FormModel) { m.Name, m.Host, m.Options["timeout"] = "n", "h", "soon" }, "timeout"},
		{"a choice that is not one", mongoEngine, func(m *FormModel) { m.Name, m.Host, m.Options["mechanism"] = "n", "h", "KERBEROS" }, "mechanism"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewFormModel(tt.e, "")
			tt.edit(m)
			problems := m.Problems()
			if len(problems) == 0 || problems[0].Field != tt.field || problems[0].Message == "" {
				t.Fatalf("Problems = %+v, want first on %q", problems, tt.field)
			}
			// the same mistakes are refused by the engine too, so the form and the store agree
			if err := tt.e.Validate(m.Connection()); err == nil && tt.field != "scope" && tt.field != "passwordEnv" && tt.field != "port" { // the port: the connection of a bad one has none
				t.Errorf("the engine accepts the connection of a form with the problem %q", tt.field)
			}
		})
	}
	// and a required field
	req := Engine{ID: "x", Name: "X", Fields: []Field{{Key: "k", Label: "The key", Required: true}}}
	m := NewFormModel(req, "")
	m.Name, m.Host = "n", "h"
	if p := m.Problems(); len(p) != 1 || p[0].Field != "k" || !strings.Contains(p[0].Message, "The key") {
		t.Errorf("Problems = %+v", p)
	}
}

func TestAPastedURLGivesUpItsPassword(t *testing.T) {
	m := NewFormModel(mongoEngine, "")
	if moved := m.SetURL(" mongodb://admin:s3cret@h1:27017,h2:27017/db?replicaSet=rs "); !moved {
		t.Fatal("the password was not moved")
	}
	if strings.Contains(m.URL, "s3cret") || m.URL != "mongodb://admin@h1:27017,h2:27017/db?replicaSet=rs" {
		t.Errorf("URL = %q", m.URL)
	}
	if m.Password != "s3cret" || !m.PasswordSet || !m.PasswordChange().Set || m.PasswordChange().Value != "s3cret" {
		t.Errorf("password = %q, set %v", m.Password, m.PasswordSet)
	}
	if moved := m.SetURL("mongodb://h"); moved || m.URL != "mongodb://h" {
		t.Errorf("a URL with no password: moved %v, URL %q", moved, m.URL)
	}
	if m.Password != "s3cret" {
		t.Error("a URL with no password cleared the password already typed")
	}
}

func TestTheDraftOfAFormHasNoPassword(t *testing.T) {
	m := NewFormModel(mongoEngine, "/work")
	m.Name, m.Host, m.Scope = "Prod", "h", ScopeWorkspace
	m.Options["timeout"] = "30"
	m.SetPassword("hunter2-VERY-secret")
	m.SavePassword = true
	data, err := json.Marshal(m.Draft())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "hunter2") {
		t.Fatalf("the draft holds the password: %s", data)
	}
	var d Draft
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	back := FormFromDraft(mongoEngine, d)
	if back.Password != "" || back.PasswordSet || back.Name != "Prod" || back.Options["timeout"] != "30" || back.Scope != ScopeWorkspace || !back.SavePassword {
		t.Errorf("the restored form = %+v", back)
	}
	// a draft edited by hand with a password in the URL does not show it
	d.URL = "mongodb://u:pw@h"
	if got := FormFromDraft(mongoEngine, d); strings.Contains(got.URL, "pw") {
		t.Errorf("URL = %q", got.URL)
	}
	// the type of the draft has no field for a password
	typ := reflect.TypeOf(Draft{})
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Name == "SavePassword" || f.Name == "PasswordEnv" || f.Name == "HadPassword" {
			continue
		}
		if isSecretName(f.Name) {
			t.Errorf("Draft.%s looks like a place for a secret", f.Name)
		}
	}
}

func TestEditingKeepsThePasswordUnlessANewOneIsTyped(t *testing.T) {
	c := Connection{ID: "id1", Name: "Prod", Engine: "postgres", Scope: ScopeUser, Host: "h", SavePassword: true, PasswordEnv: "${env:PW}"}
	m := EditFormModel(pgEngine, c, "")
	if m.PasswordChange().Set || !m.HadPassword || m.PasswordEnv != "PW" || m.ID != "id1" {
		t.Errorf("an edit form = %+v", m)
	}
	if got := m.Connection(); got.ID != "id1" || got.PasswordEnv != "${env:PW}" || !got.SavePassword {
		t.Errorf("Connection = %+v", got)
	}
	m.SetPassword("new")
	if pc := m.PasswordChange(); !pc.Set || pc.Value != "new" {
		t.Errorf("PasswordChange = %+v", pc)
	}
	m.SetPassword("")
	if m.PasswordChange().Set {
		t.Error("an emptied password field counts as a new password")
	}
}
