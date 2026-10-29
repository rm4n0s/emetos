package dbconn

import (
	"crypto/rand"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
)

// Scope tells which workspaces see a connection.
type Scope string

// The scopes of a connection (SPEC §10.21).
const (
	// ScopeUser is a connection every workspace sees.
	ScopeUser Scope = "user"
	// ScopeWorkspace is a connection only the workspace of one folder sees.
	ScopeWorkspace Scope = "workspace"
)

// TLSMode is how a connection uses TLS. The modes are those of PostgreSQL's sslmode, which the other engines map onto.
type TLSMode string

// The TLS modes. An empty mode is the engine's default.
const (
	// TLSDisable never uses TLS.
	TLSDisable TLSMode = "disable"
	// TLSPrefer uses TLS when the server offers it.
	TLSPrefer TLSMode = "prefer"
	// TLSRequire needs TLS and does not check the certificate.
	TLSRequire TLSMode = "require"
	// TLSVerifyCA needs TLS and checks that the CA signed the certificate.
	TLSVerifyCA TLSMode = "verify-ca"
	// TLSVerifyFull needs TLS and checks the CA and the name of the server.
	TLSVerifyFull TLSMode = "verify-full"
)

// TLSModes lists the modes in the order a form shows them.
func TLSModes() []TLSMode {
	return []TLSMode{TLSDisable, TLSPrefer, TLSRequire, TLSVerifyCA, TLSVerifyFull}
}

// Connection is one saved connection to a database. It has no password: see the package documentation.
type Connection struct {
	// ID names the connection for good; it is made when the connection is saved the first time.
	ID string `json:"id"`
	// Name is what the explorer shows.
	Name string `json:"name"`
	// Engine is the ID of the engine (Engine.ID: "postgres", "sqlite", "mongodb", "redis").
	Engine string `json:"engine"`
	// Scope says which workspaces see the connection; Workspace is the folder of a ScopeWorkspace connection.
	Scope     Scope  `json:"scope"`
	Workspace string `json:"workspace,omitempty"`
	// Host, Port, User and Database say where to connect and as whom. A text may hold ${env:NAME}.
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Database string `json:"database,omitempty"`
	// File is the file of an engine that opens one (SQLite).
	File string `json:"file,omitempty"`
	// URL is a connection URL, used instead of Host and Port by an engine that accepts one. It never carries a password.
	URL string `json:"url,omitempty"`
	// TLS and CAFile say how TLS is used.
	TLS    TLSMode `json:"tls,omitempty"`
	CAFile string  `json:"caFile,omitempty"`
	// Color tints the status bar item of an editor bound to the connection: "#rrggbb", or empty.
	Color string `json:"color,omitempty"`
	// ReadOnly makes the connection refuse statements that change data.
	ReadOnly bool `json:"readOnly,omitempty"`
	// SavePassword says that the password lives in the keyring (it is looked up by the ID of the connection).
	SavePassword bool `json:"savePassword,omitempty"`
	// PasswordEnv is "${env:NAME}" when the password comes from an environment variable; it is a reference, not a password.
	PasswordEnv string `json:"passwordEnv,omitempty"`
	// Options are the values of the fields only the engine has (Engine.Fields): the auth source of MongoDB, the key separator
	// of Redis. They never hold a secret.
	Options map[string]string `json:"options,omitempty"`
}

// NewID makes the ID of a connection: random, so that two computers that share a file never collide.
func NewID() string { return strings.ToLower(rand.Text()) }

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// secretWords are the words that mark a key of an option or of a query as a secret.
var secretWords = []string{"password", "passwd", "pwd", "secret", "token", "passphrase"}

// isSecretName tells whether a key (of an option, or of a query of a URL) names a secret.
func isSecretName(key string) bool {
	k := strings.ToLower(key)
	return slices.ContainsFunc(secretWords, func(w string) bool { return strings.Contains(k, w) })
}

// Check checks the parts of a connection that do not depend on its engine: a name, an engine, a scope, a port, a color, and that
// the connection holds no password (the URL, the options). The errors have the tags ConnectionNameMissing,
// ConnectionEngineMissing, ConnectionScopeInvalid, ConnectionWorkspaceMissing, ConnectionPortInvalid, ConnectionColorInvalid,
// ConnectionTLSInvalid, ConnectionURLHasPassword, ConnectionPasswordEnvInvalid and ConnectionOptionIsSecret.
func (c Connection) Check() error {
	switch {
	case strings.TrimSpace(c.Name) == "":
		return errors.New("ConnectionNameMissing", "a connection needs a name", "id", c.ID)
	case c.Engine == "":
		return errors.New("ConnectionEngineMissing", "a connection needs an engine", "id", c.ID, "name", c.Name)
	case c.Scope != ScopeUser && c.Scope != ScopeWorkspace:
		return errors.New("ConnectionScopeInvalid", "the scope of a connection is user or workspace", "id", c.ID, "scope", string(c.Scope))
	case c.Scope == ScopeWorkspace && c.Workspace == "":
		return errors.New("ConnectionWorkspaceMissing", "a connection of the workspace needs the folder of the workspace", "id", c.ID)
	case c.Port < 0 || c.Port > 65535:
		return errors.New("ConnectionPortInvalid", "a port is a number from 1 to 65535", "id", c.ID, "port", c.Port)
	case c.Color != "" && !colorPattern.MatchString(c.Color):
		return errors.New("ConnectionColorInvalid", "a color is #rrggbb", "id", c.ID, "color", c.Color)
	case c.TLS != "" && !slices.Contains(TLSModes(), c.TLS):
		return errors.New("ConnectionTLSInvalid", "unknown TLS mode", "id", c.ID, "tls", string(c.TLS))
	case urlHasSecret(c.URL):
		return errors.New("ConnectionURLHasPassword", "the URL of a connection must not hold a password: it goes to the keyring", "id", c.ID)
	}
	if c.PasswordEnv != "" {
		if _, ok := ParseEnvRef(c.PasswordEnv); !ok {
			return errors.New("ConnectionPasswordEnvInvalid", "the password variable is written ${env:NAME}", "id", c.ID)
		}
	}
	for _, key := range sortedKeys(c.Options) {
		if isSecretName(key) {
			return errors.New("ConnectionOptionIsSecret", "an option that holds a secret does not belong in the connection file", "id", c.ID, "option", key)
		}
	}
	return nil
}

// sortedKeys returns the keys of a map in order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// urlHasSecret tells whether a connection string carries a password: a password in its user information, or a query or a
// "Name=value;" part that names one.
func urlHasSecret(raw string) bool {
	if raw == "" {
		return false
	}
	if _, pass, found := splitUserinfo(raw); found && pass != "" {
		return true
	}
	if _, query, ok := strings.Cut(raw, "?"); ok {
		for _, pair := range strings.FieldsFunc(query, func(r rune) bool { return r == '&' || r == ';' }) {
			key, value, _ := strings.Cut(pair, "=")
			if isSecretName(key) && value != "" && !isEnvRefText(value) {
				return true
			}
		}
	}
	for _, part := range strings.Split(raw, ";") {
		key, value, ok := strings.Cut(part, "=")
		if ok && isSecretName(strings.TrimSpace(key)) && strings.TrimSpace(value) != "" && !isEnvRefText(strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

// splitUserinfo finds the user information of a URL such as mongodb://user:pass@host1,host2/db: the user, the password and
// whether there is an "@". It does not use net/url, which refuses the host lists of MongoDB.
func splitUserinfo(raw string) (user, password string, found bool) {
	_, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return "", "", false
	}
	authority := rest
	if i := strings.IndexAny(authority, "/?#"); i >= 0 {
		authority = authority[:i]
	}
	at := strings.LastIndexByte(authority, '@')
	if at < 0 {
		return "", "", false
	}
	info := authority[:at]
	u, p, _ := strings.Cut(info, ":")
	if dec, err := url.PathUnescape(u); err == nil {
		u = dec
	}
	if dec, err := url.PathUnescape(p); err == nil {
		p = dec
	}
	return u, p, true
}

// StripURLPassword takes the password out of a connection URL (SPEC §10.22: the password part of a pasted string moves to the
// keyring and is removed from the string that is kept). It returns the URL without the password (the user stays) and the
// password; password is empty and clean is the URL as it was when there was none. A password in a query (password=...) is
// taken out too.
func StripURLPassword(raw string) (clean, password string) {
	clean = raw
	if user, pass, found := splitUserinfo(raw); found && pass != "" {
		scheme, rest, _ := strings.Cut(raw, "://")
		at := strings.LastIndexByte(rest[:authorityEnd(rest)], '@')
		clean = scheme + "://"
		if user != "" {
			clean += url.PathEscape(user) + "@"
		}
		clean += rest[at+1:]
		password = pass
	}
	if before, query, ok := strings.Cut(clean, "?"); ok {
		var kept []string
		for _, pair := range strings.Split(query, "&") {
			key, value, _ := strings.Cut(pair, "=")
			if isSecretName(key) && value != "" && !isEnvRefText(value) {
				if dec, err := url.QueryUnescape(value); err == nil && password == "" {
					password = dec
				}
				continue
			}
			kept = append(kept, pair)
		}
		clean = before
		if len(kept) > 0 {
			clean += "?" + strings.Join(kept, "&")
		}
	}
	return clean, password
}

// authorityEnd is where the authority of "host..." (the part of a URL after "://") ends.
func authorityEnd(rest string) int {
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		return i
	}
	return len(rest)
}

// envRefPattern is ${env:NAME}, the whole text.
var envRefPattern = regexp.MustCompile(`^\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}$`)

// envNamePattern is the name of an environment variable.
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// envAnyPattern finds each ${env:NAME} in a text.
var envAnyPattern = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// isEnvRefText tells whether the text is, as a whole, a ${env:NAME}.
func isEnvRefText(s string) bool { return envRefPattern.MatchString(s) }

// ParseEnvRef reads "${env:NAME}" and returns NAME; false for any other text.
func ParseEnvRef(s string) (name string, ok bool) {
	m := envRefPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", false
	}
	return m[1], true
}

// EnvRef writes the reference to an environment variable: ${env:NAME}.
func EnvRef(name string) string { return "${env:" + name + "}" }

// LookupEnv is os.LookupEnv, as a type: the function the references to the environment are read with.
type LookupEnv func(name string) (string, bool)

// ExpandEnv replaces each ${env:NAME} of the text with the value of the variable. A variable that is not set fails with
// ConnectionEnvUnset (a set but empty variable is an empty text).
func ExpandEnv(s string, lookup LookupEnv) (string, error) {
	if !strings.Contains(s, "${env:") {
		return s, nil
	}
	var missing string
	out := envAnyPattern.ReplaceAllStringFunc(s, func(ref string) string {
		name := envAnyPattern.FindStringSubmatch(ref)[1]
		v, ok := lookup(name)
		if !ok && missing == "" {
			missing = name
		}
		return v
	})
	if missing != "" {
		return "", errors.New("ConnectionEnvUnset", "the environment variable is not set", "variable", missing)
	}
	return out, nil
}

// Expanded returns the connection with each ${env:NAME} of its texts replaced (host, user, database, file, URL, CA file and
// the options); the reference to the password stays, Passwords resolves it. The errors are those of ExpandEnv.
func (c Connection) Expanded(lookup LookupEnv) (Connection, error) {
	out := c
	for _, p := range []*string{&out.Host, &out.User, &out.Database, &out.File, &out.URL, &out.CAFile} {
		v, err := ExpandEnv(*p, lookup)
		if err != nil {
			return Connection{}, err
		}
		*p = v
	}
	if len(c.Options) > 0 {
		out.Options = make(map[string]string, len(c.Options))
		for k, v := range c.Options {
			ev, err := ExpandEnv(v, lookup)
			if err != nil {
				return Connection{}, err
			}
			out.Options[k] = ev
		}
	}
	return out, nil
}

// VisibleIn tells whether the workspace of the folder sees the connection: a user connection is seen everywhere, a connection
// of a workspace only by the workspace of its folder.
func (c Connection) VisibleIn(folder string) bool {
	return c.Scope == ScopeUser || (folder != "" && c.Workspace == folder)
}

// clone returns a copy that shares no map with the original.
func (c Connection) clone() Connection {
	if c.Options != nil {
		opts := make(map[string]string, len(c.Options))
		for k, v := range c.Options {
			opts[k] = v
		}
		c.Options = opts
	}
	return c
}
