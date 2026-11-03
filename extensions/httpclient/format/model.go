package format

import (
	"bytes"
	"encoding/json"
)

// Version is the version of the format this package reads and writes (the value of the key "emetosHttp").
const Version = 1

// The folders and file names of a project (SPEC §10.33).
const (
	// Dir is the folder of the HTTP client in the workspace folder.
	Dir = ".emetos/http"
	// CollectionsDir and EnvironmentsDir are the folders of Dir that hold the collections and the environments.
	CollectionsDir  = "collections"
	EnvironmentsDir = "environments"
	// CollectionFile and FolderFile are the files that describe a collection and a folder of one.
	CollectionFile = "collection.jsonc"
	FolderFile     = "folder.jsonc"
	// RequestSuffix and EnvironmentSuffix end the file name of a request and of an environment.
	RequestSuffix     = ".request.jsonc"
	EnvironmentSuffix = ".jsonc"
)

// Pair is a name and a value of the tables of the request tab (a parameter, a header, a form field) and may have a description.
// A multipart field that sends a file has File and no Value.
type Pair struct {
	Name        string
	Value       string
	File        string
	Description string
	// Enabled is true unless the file says "enabled": false.
	Enabled bool
}

type pairJSON struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	File        string `json:"file,omitempty"`
	Description string `json:"description,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

// MarshalJSON writes the pair with "enabled" only when it is false, and "value" unless the pair is a file.
func (p Pair) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"name":`)
	if err := writeJSONString(&buf, p.Name); err != nil {
		return nil, err
	}
	if p.File == "" {
		buf.WriteString(`,"value":`)
		if err := writeJSONString(&buf, p.Value); err != nil {
			return nil, err
		}
	} else {
		buf.WriteString(`,"file":`)
		if err := writeJSONString(&buf, p.File); err != nil {
			return nil, err
		}
	}
	if p.Description != "" {
		buf.WriteString(`,"description":`)
		if err := writeJSONString(&buf, p.Description); err != nil {
			return nil, err
		}
	}
	if !p.Enabled {
		buf.WriteString(`,"enabled":false`)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON reads a pair; a missing "enabled" is true.
func (p *Pair) UnmarshalJSON(data []byte) error {
	var j pairJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err //nolint:wrapcheck // the caller of Decode gives the error its tag and its place
	}
	*p = Pair{Name: j.Name, Value: j.Value, File: j.File, Description: j.Description, Enabled: j.Enabled == nil || *j.Enabled}
	return nil
}

// Variable is a variable of an environment or a collection. The value of a secret variable is never written to a file (D45).
type Variable struct {
	Name string
	// Value is the initial value; it is empty for a secret.
	Value string
	// Enabled is true unless the file says "enabled": false.
	Enabled bool
	// Secret says that the value is in the keyring.
	Secret bool
}

type variableJSON struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Enabled *bool  `json:"enabled,omitempty"`
	Secret  bool   `json:"secret,omitempty"`
}

// MarshalJSON writes the variable: no "value" for a secret, "enabled" only when false, "secret" only when true.
func (v Variable) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"name":`)
	if err := writeJSONString(&buf, v.Name); err != nil {
		return nil, err
	}
	if !v.Secret {
		buf.WriteString(`,"value":`)
		if err := writeJSONString(&buf, v.Value); err != nil {
			return nil, err
		}
	}
	if !v.Enabled {
		buf.WriteString(`,"enabled":false`)
	}
	if v.Secret {
		buf.WriteString(`,"secret":true`)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON reads a variable. A value in the file of a secret variable is dropped: it is not used and the next save
// removes it.
func (v *Variable) UnmarshalJSON(data []byte) error {
	var j variableJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err //nolint:wrapcheck // the caller of Decode gives the error its tag and its place
	}
	*v = Variable{Name: j.Name, Value: j.Value, Enabled: j.Enabled == nil || *j.Enabled, Secret: j.Secret}
	if v.Secret {
		v.Value = ""
	}
	return nil
}

func writeJSONString(buf *bytes.Buffer, s string) error {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err //nolint:wrapcheck // a string always encodes; MarshalJSON's caller wraps it
	}
	buf.Truncate(buf.Len() - 1) // the line break of Encode
	return nil
}

// The types of an Auth.
const (
	AuthInherit = "inherit"
	AuthNone    = "none"
	AuthBasic   = "basic"
	AuthBearer  = "bearer"
	AuthAPIKey  = "apikey"
	AuthDigest  = "digest"
	AuthOAuth2  = "oauth2"
)

// Auth is the authorization of a collection, folder or request. Which fields count depends on Type.
type Auth struct {
	Type         string `json:"type"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
	Token        string `json:"token,omitempty"`
	Key          string `json:"key,omitempty"`
	Value        string `json:"value,omitempty"`
	In           string `json:"in,omitempty"`
	Grant        string `json:"grant,omitempty"`
	TokenURL     string `json:"tokenUrl,omitempty"`
	AuthURL      string `json:"authUrl,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Audience     string `json:"audience,omitempty"`
	PKCE         bool   `json:"pkce,omitempty"`
	AddTo        string `json:"addTo,omitempty"`
}

// The types of a Body.
const (
	BodyNone       = "none"
	BodyJSON       = "json"
	BodyXML        = "xml"
	BodyText       = "text"
	BodyHTML       = "html"
	BodyJavaScript = "javascript"
	BodyGraphQL    = "graphql"
	BodyForm       = "form"
	BodyMultipart  = "multipart"
	BodyBinary     = "binary"
)

// Body is the body of a request. The text of a raw body is in the file named File, next to the request file; the variables of a
// GraphQL body are in the file named Variables.
type Body struct {
	Type      string `json:"type"`
	File      string `json:"file,omitempty"`
	Variables string `json:"variables,omitempty"`
	Fields    []Pair `json:"fields,omitempty"`
	Path      string `json:"path,omitempty"`
}

// Settings are the per-request settings; a nil value means the default of the setting of the editor.
type Settings struct {
	TimeoutMs       *int  `json:"timeoutMs,omitempty"`
	FollowRedirects *bool `json:"followRedirects,omitempty"`
	MaxRedirects    *int  `json:"maxRedirects,omitempty"`
	VerifyTLS       *bool `json:"verifyTls,omitempty"`
}

// ExtractRule takes a value out of a response into a variable.
type ExtractRule struct {
	From   string `json:"from"`
	Path   string `json:"path,omitempty"`
	Header string `json:"header,omitempty"`
	Cookie string `json:"cookie,omitempty"`
	Regex  string `json:"regex,omitempty"`
	To     string `json:"to"`
	Scope  string `json:"scope,omitempty"`
}

// Check is an assertion about a response.
type Check struct {
	Subject string `json:"subject"`
	Path    string `json:"path,omitempty"`
	Header  string `json:"header,omitempty"`
	Op      string `json:"op"`
	Value   any    `json:"value,omitempty"`
}

// Collection is collection.jsonc, and (as Folder) folder.jsonc of a folder of a collection.
type Collection struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Order lists the entries (requests by file name, folders by folder name) in the order they are shown.
	Order     []string   `json:"order,omitempty"`
	Variables []Variable `json:"variables,omitempty"`
	Auth      *Auth      `json:"auth,omitempty"`
	Headers   []Pair     `json:"headers,omitempty"`
}

// Folder is folder.jsonc: it has the keys of a collection (a folder has variables too: SPEC §10.33 "As built (step 27.1)").
type Folder = Collection

// Request is a .request.jsonc file.
type Request struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Method      string        `json:"method"`
	URL         string        `json:"url"`
	Params      []Pair        `json:"params,omitempty"`
	Headers     []Pair        `json:"headers,omitempty"`
	Auth        *Auth         `json:"auth,omitempty"`
	Body        *Body         `json:"body,omitempty"`
	Settings    *Settings     `json:"settings,omitempty"`
	Extract     []ExtractRule `json:"extract,omitempty"`
	Checks      []Check       `json:"checks,omitempty"`
}

// Environment is a file of environments/.
type Environment struct {
	Name      string     `json:"name"`
	Variables []Variable `json:"variables,omitempty"`
}
