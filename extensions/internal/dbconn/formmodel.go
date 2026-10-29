package dbconn

import (
	"strconv"
	"strings"
)

// FormModel is the connection form without its widgets: the values of the fields as the user typed them, the checks the form
// shows next to the fields, and the Connection they make. It holds the typed password in memory, never in Draft.
type FormModel struct {
	// Engine is the engine of the connection; it cannot be changed in the form (a connection of another engine is another form).
	Engine Engine
	// ID is the ID of the connection being edited, empty for a new one.
	ID string
	// Folder is the folder of the workspace the form was opened in; a connection of the workspace scope belongs to it. Empty in a
	// window with no folder, where the scope is always the user's.
	Folder string

	Name, Host, Port, User, Database, File, URL string
	TLS                                         TLSMode
	CAFile, Color                               string
	ReadOnly                                    bool
	Scope                                       Scope
	// Options are the values of the engine's own fields.
	Options map[string]string

	// Password is what the user typed in the password field; it goes to the keyring or to memory (Hub.Save) and nowhere else.
	Password string
	// PasswordSet says that the user typed a password (an empty one included): without it an edit keeps the password it has.
	PasswordSet bool
	// SavePassword asks for the keyring.
	SavePassword bool
	// PasswordEnv is the name of the environment variable the password comes from ("PGPASSWORD"), or empty.
	PasswordEnv string
	// HadPassword says that the connection being edited has a saved password already (the form says so).
	HadPassword bool
}

// NewFormModel starts a form for a new connection of the engine.
func NewFormModel(e Engine, folder string) *FormModel {
	c := e.NewConnection()
	m := &FormModel{Engine: e, Folder: folder}
	m.load(c)
	return m
}

// EditFormModel starts a form for an existing connection.
func EditFormModel(e Engine, c Connection, folder string) *FormModel {
	m := &FormModel{Engine: e, Folder: folder, ID: c.ID}
	m.load(c)
	m.HadPassword = c.SavePassword
	return m
}

// load copies a connection into the fields.
func (m *FormModel) load(c Connection) {
	m.Name, m.Host, m.User, m.Database, m.File, m.URL = c.Name, c.Host, c.User, c.Database, c.File, c.URL
	m.Port = ""
	if c.Port != 0 {
		m.Port = strconv.Itoa(c.Port)
	}
	m.TLS, m.CAFile, m.Color, m.ReadOnly, m.Scope = c.TLS, c.CAFile, c.Color, c.ReadOnly, c.Scope
	if m.Scope == "" {
		m.Scope = ScopeUser
	}
	m.SavePassword = c.SavePassword
	if name, ok := ParseEnvRef(c.PasswordEnv); ok {
		m.PasswordEnv = name
	}
	m.Options = map[string]string{}
	for k, v := range c.Options {
		m.Options[k] = v
	}
}

// SetURL sets the URL field. A password in the text moves to the password field and is removed from the URL (SPEC §10.22): the
// text that is kept never holds one. It returns true when it took a password out.
func (m *FormModel) SetURL(raw string) (movedPassword bool) {
	clean, pass := StripURLPassword(strings.TrimSpace(raw))
	m.URL = clean
	if pass != "" {
		m.Password, m.PasswordSet = pass, true
		return true
	}
	return false
}

// SetPassword records the password typed in the form.
func (m *FormModel) SetPassword(pw string) {
	m.Password, m.PasswordSet = pw, pw != ""
}

// Connection makes the connection the form describes.
func (m *FormModel) Connection() Connection {
	c := Connection{
		ID: m.ID, Name: strings.TrimSpace(m.Name), Engine: m.Engine.ID, Scope: m.Scope, TLS: m.TLS, CAFile: strings.TrimSpace(m.CAFile),
		Color: strings.TrimSpace(m.Color), ReadOnly: m.ReadOnly, SavePassword: m.SavePassword && !m.Engine.NoPassword,
	}
	if c.Scope == ScopeWorkspace {
		c.Workspace = m.Folder
	}
	if p := strings.TrimSpace(m.Port); p != "" {
		if n, err := strconv.Atoi(p); err == nil { // a port that is not a number is a problem the form reports
			c.Port = n
		}
	}
	if m.Engine.File {
		c.File = strings.TrimSpace(m.File)
	} else {
		c.Host, c.User, c.Database, c.URL = strings.TrimSpace(m.Host), strings.TrimSpace(m.User), strings.TrimSpace(m.Database), strings.TrimSpace(m.URL)
	}
	if m.Engine.NoUser {
		c.User = ""
	}
	if m.Engine.NoDatabase {
		c.Database = ""
	}
	if m.Engine.NoTLS {
		c.TLS, c.CAFile = "", ""
	}
	if env := strings.TrimSpace(m.PasswordEnv); env != "" && !m.Engine.NoPassword {
		if _, ok := ParseEnvRef(env); ok {
			c.PasswordEnv = env
		} else {
			c.PasswordEnv = EnvRef(env)
		}
	}
	for _, f := range m.Engine.Fields {
		if v := strings.TrimSpace(m.Options[f.Key]); v != "" {
			if c.Options == nil {
				c.Options = map[string]string{}
			}
			c.Options[f.Key] = v
		}
	}
	return c
}

// PasswordChange is what to do with the password when the form is saved.
func (m *FormModel) PasswordChange() PasswordChange {
	if m.Engine.NoPassword || !m.PasswordSet {
		return PasswordChange{}
	}
	return PasswordChange{Set: true, Value: m.Password}
}

// Problem is a mistake in the form, shown next to its field.
type Problem struct {
	// Field names the field: "name", "host", "port", "file", "url", "tls", "color", "passwordEnv", or the key of an engine's field.
	Field string
	// Message says what is wrong, for the user.
	Message string
}

// Problems checks the form and lists what is wrong, in the order of the fields; an empty list means the connection is valid. The
// checks are those of Engine.Validate, said in words for the field they belong to.
func (m *FormModel) Problems() []Problem {
	var out []Problem
	add := func(field, msg string) { out = append(out, Problem{field, msg}) }
	if strings.TrimSpace(m.Name) == "" {
		add("name", "A connection needs a name.")
	}
	if m.Scope == ScopeWorkspace && m.Folder == "" {
		add("scope", "A connection of the workspace needs a folder: this window has none.")
	}
	e := m.Engine
	url := strings.TrimSpace(m.URL)
	switch {
	case e.File:
		if strings.TrimSpace(m.File) == "" {
			add("file", "Choose the database file.")
		}
	case url != "":
		scheme, _, hasScheme := strings.Cut(url, "://")
		switch {
		case !e.usesURL():
			add("url", "This engine takes no connection URL.")
		case !hasScheme || !containsFold(e.URLSchemes, scheme):
			add("url", "The URL must start with "+joinSchemes(e.URLSchemes)+".")
		case urlHasSecret(url):
			add("url", "Take the password out of the URL: it goes in the password field.")
		}
	case strings.TrimSpace(m.Host) == "":
		add("host", "Enter the host, or a connection URL.")
	}
	if p := strings.TrimSpace(m.Port); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			add("port", "The port is a number from 1 to 65535.")
		}
	}
	if m.Color != "" && !colorPattern.MatchString(strings.TrimSpace(m.Color)) {
		add("color", "A color is written #rrggbb.")
	}
	if env := strings.TrimSpace(m.PasswordEnv); env != "" {
		if _, ok := ParseEnvRef(env); !ok && !envNamePattern.MatchString(env) {
			add("passwordEnv", "Enter the name of an environment variable (PGPASSWORD).")
		}
	}
	for _, f := range e.Fields {
		v := strings.TrimSpace(m.Options[f.Key])
		switch {
		case v == "" && f.Required:
			add(f.Key, f.Label+" is required.")
		case v != "" && f.Kind == FieldNumber && !isWholeNumber(v) && !isEnvRefText(v):
			add(f.Key, f.Label+" is a whole number.")
		case v != "" && f.Kind == FieldChoice && !slicesContainsChoice(f.Choices, v):
			add(f.Key, f.Label+" must be one of its choices.")
		}
	}
	return out
}

// joinSchemes writes the schemes of an engine for a message: "mongodb:// or mongodb+srv://".
func joinSchemes(schemes []string) string {
	parts := make([]string, len(schemes))
	for i, s := range schemes {
		parts[i] = s + "://"
	}
	return strings.Join(parts, " or ")
}

// Draft is what the editor tab keeps of the form when it is closed and opened again (the state of the tab): every field but the
// password, which is never part of it.
type Draft struct {
	Engine       string            `json:"engine"`
	ID           string            `json:"id,omitempty"`
	Folder       string            `json:"folder,omitempty"`
	Name         string            `json:"name,omitempty"`
	Host         string            `json:"host,omitempty"`
	Port         string            `json:"port,omitempty"`
	User         string            `json:"user,omitempty"`
	Database     string            `json:"database,omitempty"`
	File         string            `json:"file,omitempty"`
	URL          string            `json:"url,omitempty"`
	TLS          TLSMode           `json:"tls,omitempty"`
	CAFile       string            `json:"caFile,omitempty"`
	Color        string            `json:"color,omitempty"`
	ReadOnly     bool              `json:"readOnly,omitempty"`
	Scope        Scope             `json:"scope,omitempty"`
	Options      map[string]string `json:"options,omitempty"`
	SavePassword bool              `json:"savePassword,omitempty"`
	PasswordEnv  string            `json:"passwordEnv,omitempty"`
	HadPassword  bool              `json:"hadPassword,omitempty"`
}

// Draft returns the state to keep. It has no password.
func (m *FormModel) Draft() Draft {
	opts := map[string]string{}
	for k, v := range m.Options {
		opts[k] = v
	}
	return Draft{
		Engine: m.Engine.ID, ID: m.ID, Folder: m.Folder, Name: m.Name, Host: m.Host, Port: m.Port, User: m.User, Database: m.Database,
		File: m.File, URL: m.URL, TLS: m.TLS, CAFile: m.CAFile, Color: m.Color, ReadOnly: m.ReadOnly, Scope: m.Scope, Options: opts,
		SavePassword: m.SavePassword, PasswordEnv: m.PasswordEnv, HadPassword: m.HadPassword,
	}
}

// FormFromDraft restores a form from its draft; the password is empty, as it was never kept.
func FormFromDraft(e Engine, d Draft) *FormModel {
	m := &FormModel{
		Engine: e, ID: d.ID, Folder: d.Folder, Name: d.Name, Host: d.Host, Port: d.Port, User: d.User, Database: d.Database, File: d.File,
		URL: d.URL, TLS: d.TLS, CAFile: d.CAFile, Color: d.Color, ReadOnly: d.ReadOnly, Scope: d.Scope, SavePassword: d.SavePassword,
		PasswordEnv: d.PasswordEnv, HadPassword: d.HadPassword, Options: map[string]string{},
	}
	if m.Scope == "" {
		m.Scope = ScopeUser
	}
	for k, v := range d.Options {
		m.Options[k] = v
	}
	// a draft written by hand could hold a password in the URL; the form never shows it
	m.URL, _ = StripURLPassword(m.URL)
	return m
}
