package dbconn

import (
	"context"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// FieldKind is the kind of a field of the connection form.
type FieldKind int

// The kinds of field an engine adds to the form.
const (
	// FieldText is a line of text.
	FieldText FieldKind = iota
	// FieldNumber is a whole number.
	FieldNumber
	// FieldFile is a path, with a button that opens the file dialog.
	FieldFile
	// FieldChoice is one of Choices.
	FieldChoice
	// FieldBool is a check box; its value is "true" or "false".
	FieldBool
)

// Choice is one value of a FieldChoice.
type Choice struct {
	// Value is what is stored; Label is what the form shows (the value when empty).
	Value, Label string
}

// Field is a field only one engine has, in the form after the common ones. Its value is kept in Connection.Options[Key].
type Field struct {
	// Key names the option; it must not name a secret (password, token...).
	Key string
	// Label is the caption in the form.
	Label string
	// Kind is the kind of input.
	Kind FieldKind
	// Placeholder is the hint of an empty input.
	Placeholder string
	// Default is the value of a new connection.
	Default string
	// Choices are the values of a FieldChoice.
	Choices []Choice
	// Required makes an empty value an error.
	Required bool
}

// Engine describes a kind of database to the form and to the view. A database extension gives it with its Driver.
type Engine struct {
	// ID names the engine in the connection file ("postgres", "mysql", "sqlite", "sqlserver", "mongodb", "redis").
	ID string
	// Name is the name for the user ("PostgreSQL").
	Name string
	// Icon is the name of the Codicon of the engine's connections; "database" when empty.
	Icon string
	// DefaultPort is the port of a new connection; 0 for an engine without a port.
	DefaultPort int
	// File makes the form ask for a file instead of a host, a port, a user and a database (SQLite).
	File bool
	// URLSchemes are the schemes of the connection URL the engine accepts ("mongodb", "mongodb+srv"); none means the form has no
	// URL field.
	URLSchemes []string
	// NoUser and NoDatabase leave out the field of the user, or of the database, for an engine that has none.
	NoUser, NoDatabase bool
	// NoTLS leaves out the TLS fields (SQLite).
	NoTLS bool
	// NoPassword leaves out the password (SQLite).
	NoPassword bool
	// Fields are the fields only this engine has.
	Fields []Field
}

// DisplayIcon is the Codicon of the engine.
func (e Engine) DisplayIcon() string {
	if e.Icon == "" {
		return "database"
	}
	return e.Icon
}

// usesURL tells whether the engine accepts a connection URL.
func (e Engine) usesURL() bool { return len(e.URLSchemes) > 0 }

// Validate checks a connection against the engine: the parts the engine needs are there (a file, a host or a URL, the required
// fields), the URL has a scheme the engine takes, and the common parts are valid (Connection.Check). The errors have the tags of
// Connection.Check and ConnectionEngineMismatch, ConnectionFileMissing, ConnectionHostMissing, ConnectionURLSchemeInvalid,
// ConnectionOptionMissing, ConnectionOptionInvalid and ConnectionOptionUnknown.
func (e Engine) Validate(c Connection) error {
	if err := c.Check(); err != nil {
		return err
	}
	if c.Engine != e.ID {
		return errors.New("ConnectionEngineMismatch", "the connection is of another engine", "id", c.ID, "engine", c.Engine, "want", e.ID)
	}
	switch {
	case e.File:
		if strings.TrimSpace(c.File) == "" {
			return errors.New("ConnectionFileMissing", "this engine needs a file", "id", c.ID)
		}
	case c.URL != "":
		if !e.usesURL() {
			return errors.New("ConnectionURLSchemeInvalid", "this engine takes no connection URL", "id", c.ID, "url", c.URL)
		}
		scheme, _, _ := strings.Cut(c.URL, "://")
		if !containsFold(e.URLSchemes, scheme) {
			return errors.New("ConnectionURLSchemeInvalid", "the URL has a scheme the engine does not take", "id", c.ID, "scheme", scheme)
		}
	case strings.TrimSpace(c.Host) == "":
		return errors.New("ConnectionHostMissing", "a connection needs a host", "id", c.ID)
	}
	known := map[string]Field{}
	for _, f := range e.Fields {
		known[f.Key] = f
	}
	for _, f := range e.Fields {
		v := strings.TrimSpace(c.Options[f.Key])
		if f.Required && v == "" {
			return errors.New("ConnectionOptionMissing", "a required field is empty", "id", c.ID, "option", f.Key)
		}
		if v == "" {
			continue
		}
		switch f.Kind {
		case FieldNumber:
			if !isWholeNumber(v) && !isEnvRefText(v) {
				return errors.New("ConnectionOptionInvalid", "the field takes a whole number", "id", c.ID, "option", f.Key, "value", v)
			}
		case FieldChoice:
			if !slicesContainsChoice(f.Choices, v) {
				return errors.New("ConnectionOptionInvalid", "the field takes one of its choices", "id", c.ID, "option", f.Key, "value", v)
			}
		case FieldBool:
			if v != "true" && v != "false" {
				return errors.New("ConnectionOptionInvalid", "the field is true or false", "id", c.ID, "option", f.Key, "value", v)
			}
		}
	}
	for _, k := range sortedKeys(c.Options) {
		if _, ok := known[k]; !ok {
			return errors.New("ConnectionOptionUnknown", "the engine has no such field", "id", c.ID, "option", k)
		}
	}
	return nil
}

// containsFold tells whether the list has the text, ignoring case.
func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// slicesContainsChoice tells whether one of the choices has the value.
func slicesContainsChoice(choices []Choice, v string) bool {
	for _, c := range choices {
		if c.Value == v {
			return true
		}
	}
	return false
}

// isWholeNumber tells whether the text is made of digits (with an optional minus).
func isWholeNumber(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// NewConnection returns the connection a form starts from for the engine: its default port and the defaults of its fields,
// the user scope, and no ID (the store makes it).
func (e Engine) NewConnection() Connection {
	c := Connection{Engine: e.ID, Scope: ScopeUser, Port: e.DefaultPort}
	for _, f := range e.Fields {
		if f.Default != "" {
			if c.Options == nil {
				c.Options = map[string]string{}
			}
			c.Options[f.Key] = f.Default
		}
	}
	return c
}

// Node is a row of the tree below a connection: a database, a schema, a table, a key. The extension of the engine makes the
// nodes; the view shows them.
type Node struct {
	// ID names the node among the children of its parent; the view builds its key from the IDs of the path.
	ID string
	// Label is the text; Description follows it in a dimmer color.
	Label, Description string
	// Icon is the name of a Codicon; Tooltip is the hover.
	Icon, Tooltip string
	// Expandable says that Children has something to give.
	Expandable bool
	// Kind and Data are the extension's own: what kind of node it is ("table"), and what it needs to list its children and to
	// act on it.
	Kind string
	Data any
}

// Action is an entry of the context menu of a node.
type Action struct {
	// Title is the entry.
	Title string
	// Group orders the entries: those of the same group stay together, a separator between groups.
	Group string
	// Run is called on the UI thread.
	Run func()
}

// Driver is what a database extension gives the Databases view for its engines. The methods that talk to the database take a
// context that ends when the work is no longer wanted; they run off the UI thread, and Actions runs on it.
type Driver interface {
	// Engines are the engines the driver connects to.
	Engines() []Engine
	// Connect opens the connection. password is empty when there is none. c has its ${env:} references expanded already.
	Connect(ctx context.Context, c Connection, password string) error
	// Disconnect closes the connection.
	Disconnect(c Connection) error
	// Children lists the nodes below parent; a nil parent asks for the nodes right below the connection (the databases, the
	// schemas, the keys).
	Children(ctx context.Context, c Connection, parent *Node) ([]Node, error)
	// Actions are the entries of the context menu of a node (nil for the connection itself).
	Actions(c Connection, n *Node) []Action
}

// DisconnectGuard is a Driver that has work to lose when a connection is closed (an open transaction): the view asks the user
// before it disconnects.
type DisconnectGuard interface {
	// OpenWork says in a sentence what disconnecting the connection would lose; empty when nothing.
	OpenWork(c Connection) string
}

// APIDriver is a Driver that serves every window tab of Emetos with one object, so that the entries it adds to a context menu need
// the API of the window tab the menu was opened in (to open an editor there). The view calls ActionsIn instead of Actions for such
// a driver.
type APIDriver interface {
	Driver
	// ActionsIn is Actions for the window tab of api. It runs on the UI thread.
	ActionsIn(api extension.API, c Connection, n *Node) []Action
}
