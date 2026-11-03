package format

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseEveryKeyOfARequest(t *testing.T) {
	text := `// a request
{
  "emetosHttp": 1,
  "name": "Create user",
  "description": "Makes one",
  "method": "POST",
  "url": "{{base}}/users?x=1",
  "params": [{"name": "x", "value": "1"}, {"name": "old", "value": "2", "enabled": false, "description": "unused"}],
  "headers": [{"name": "Accept", "value": "application/json",}],
  "auth": {"type": "oauth2", "grant": "authorization_code", "tokenUrl": "t", "authUrl": "a", "clientId": "c", "clientSecret": "s", "scope": "r", "audience": "u", "pkce": true, "addTo": "header"},
  "body": {"type": "multipart", "fields": [{"name": "a", "value": "b"}, {"name": "f", "file": "data/x.bin"}], "path": "p", "file": "x.body.json", "variables": "x.variables.json"},
  "settings": {"timeoutMs": 500, "followRedirects": false, "maxRedirects": 3, "verifyTls": false},
  "extract": [{"from": "body", "path": "$.id", "header": "H", "cookie": "C", "regex": "(.*)", "to": "id", "scope": "collection"}],
  "checks": [{"subject": "json", "path": "$.id", "header": "H", "op": "equals", "value": 3}],
  "futureKey": {"x": 1},
}`
	want := Request{
		Name: "Create user", Description: "Makes one", Method: "POST", URL: "{{base}}/users?x=1",
		Params:  []Pair{{Name: "x", Value: "1", Enabled: true}, {Name: "old", Value: "2", Description: "unused"}},
		Headers: []Pair{{Name: "Accept", Value: "application/json", Enabled: true}},
		Auth: &Auth{Type: AuthOAuth2, Grant: "authorization_code", TokenURL: "t", AuthURL: "a", ClientID: "c", ClientSecret: "s",
			Scope: "r", Audience: "u", PKCE: true, AddTo: "header"},
		Body: &Body{Type: BodyMultipart, File: "x.body.json", Variables: "x.variables.json", Path: "p",
			Fields: []Pair{{Name: "a", Value: "b", Enabled: true}, {Name: "f", File: "data/x.bin", Enabled: true}}},
		Settings: &Settings{TimeoutMs: ptr(500), FollowRedirects: ptr(false), MaxRedirects: ptr(3), VerifyTLS: ptr(false)},
		Extract:  []ExtractRule{{From: "body", Path: "$.id", Header: "H", Cookie: "C", Regex: "(.*)", To: "id", Scope: "collection"}},
		Checks:   []Check{{Subject: "json", Path: "$.id", Header: "H", Op: "equals", Value: 3.0}},
	}
	got, err := Parse[Request](text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse =\n%#v\nwant\n%#v", got, want)
	}
	// written new and read again, the document is the same
	out, err := Marshal(got)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	again, err := Parse[Request](out)
	if err != nil {
		t.Fatalf("Parse(Marshal): %v\n%s", err, out)
	}
	if !reflect.DeepEqual(again, want) {
		t.Errorf("round trip =\n%#v\nwant\n%#v", again, want)
	}
}

func TestParseCollectionFolderAndEnvironment(t *testing.T) {
	c, err := Parse[Collection](`{"emetosHttp": 1, "name": "API", "description": "d", "order": ["a.request.jsonc", "sub"],
		"variables": [{"name": "base", "value": "http://x"}, {"name": "tok", "secret": true, "value": "leaked"}, {"name": "off", "value": "1", "enabled": false}],
		"auth": {"type": "bearer", "token": "{{tok}}"}, "headers": [{"name": "X", "value": "y"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	want := Collection{
		Name: "API", Description: "d", Order: []string{"a.request.jsonc", "sub"},
		Variables: []Variable{{Name: "base", Value: "http://x", Enabled: true}, {Name: "tok", Enabled: true, Secret: true}, {Name: "off", Value: "1"}},
		Auth:      &Auth{Type: AuthBearer, Token: "{{tok}}"}, Headers: []Pair{{Name: "X", Value: "y", Enabled: true}},
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("collection =\n%#v\nwant\n%#v", c, want)
	}
	e, err := Parse[Environment](`{"emetosHttp": 1, "name": "Dev", "variables": [{"name": "a", "value": "1"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e, Environment{Name: "Dev", Variables: []Variable{{Name: "a", Value: "1", Enabled: true}}}) {
		t.Errorf("environment = %#v", e)
	}
}

func TestParseFailures(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		route        string
		line, column int
	}{
		{"broken syntax", "{\n  \"emetosHttp\": 1,\n  \"name\" \"x\"\n}", "Parse[...].HttpFileSyntax", 3, 10},
		{"empty", "", "HttpFileSyntax", 1, 1},
		{"not an object", "[1]", "HttpFileNotObject", 1, 1},
		{"no version", "{\"name\": \"x\"}", "HttpFileVersionMissing", 1, 1},
		{"version text", "{\"emetosHttp\": \"1\"}", "HttpFileVersionInvalid", 1, 16},
		{"version zero", "{\"emetosHttp\": 0}", "HttpFileVersionInvalid", 1, 16},
		{"version fraction", "{\"emetosHttp\": 1.5}", "HttpFileVersionInvalid", 1, 16},
		{"name is a number", "{\"emetosHttp\": 1,\n\"name\": 5}", "HttpFileValueInvalid", 2, 9},
		{"params is an object", "{\"emetosHttp\": 1,\n\"params\": {}}", "HttpFileValueInvalid", 2, 11},
		{"unknown auth type", "{\"emetosHttp\": 1,\n\"auth\": {\"type\": \"ntlm\"}}", "HttpFileValueInvalid", 2, 9},
		{"unknown body type", "{\"emetosHttp\": 1,\n\"body\": {\"type\": \"yaml\"}}", "HttpFileValueInvalid", 2, 9},
		{"body file with a folder", "{\"emetosHttp\": 1,\n\"body\": {\"type\": \"json\", \"file\": \"../x.json\"}}", "HttpFileValueInvalid", 2, 9},
		{"unknown check operation", "{\"emetosHttp\": 1,\n\"checks\": [{\"subject\": \"status\", \"op\": \"like\"}]}", "HttpFileValueInvalid", 2, 11},
		{"extract without a target", "{\"emetosHttp\": 1,\n\"extract\": [{\"from\": \"body\"}]}", "HttpFileValueInvalid", 2, 12},
		{"negative timeout", "{\"emetosHttp\": 1,\n\"settings\": {\"timeoutMs\": -1}}", "HttpFileValueInvalid", 2, 13},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse[Request](tt.text)
			appErr := routeErr(t, err, "Parse")
			wantRoute(t, err, tt.route[strings.LastIndex(tt.route, ".")+1:])
			if appErr.Metadata["line"] != tt.line || appErr.Metadata["column"] != tt.column {
				t.Errorf("position = %v:%v, want %d:%d (%v)", appErr.Metadata["line"], appErr.Metadata["column"], tt.line, tt.column, appErr.Message)
			}
		})
	}
}

func TestParseCollectionFailures(t *testing.T) {
	tests := []struct{ name, text, key string }{
		{"order with a path", `{"emetosHttp": 1, "order": ["a/b"]}`, "order"},
		{"variable without a name", `{"emetosHttp": 1, "variables": [{"value": "1"}]}`, "variables"},
		{"pair with a value and a file", `{"emetosHttp": 1, "headers": [{"name": "a", "value": "1", "file": "f"}]}`, "headers"},
		{"api key in the body", `{"emetosHttp": 1, "auth": {"type": "apikey", "in": "cookie"}}`, "auth"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse[Collection](tt.text)
			appErr := routeErr(t, err, "Parse")
			wantRoute(t, err, "HttpFileValueInvalid")
			if appErr.Metadata["key"] != tt.key {
				t.Errorf("key = %v, want %s", appErr.Metadata["key"], tt.key)
			}
		})
	}
}

func TestNewerVersionIsReadAndNeverWritten(t *testing.T) {
	text := `{"emetosHttp": 2, "name": "Future", "method": {"new": "shape"}, "url": "u"}`
	got, err := Parse[Request](text)
	wantRoute(t, err, "Parse")
	wantRoute(t, err, "HttpFileNewerVersion")
	if !IsNewer(err) {
		t.Error("IsNewer = false")
	}
	if got.Name != "Future" || got.URL != "u" {
		t.Errorf("a newer file is read as far as it can be: %#v", got)
	}
	_, err = Update(text, got)
	wantRoute(t, err, "Update")
	wantRoute(t, err, "HttpFileNewerVersion")
	if IsNewer(nil) {
		t.Error("IsNewer(nil) = true")
	}
}

func TestPositionOf(t *testing.T) {
	tests := []struct {
		text   string
		offset int
		want   Position
	}{
		{"", 0, Position{1, 1, 0}},
		{"ab\ncd", 4, Position{2, 2, 4}},
		{"é\nx", 3, Position{2, 1, 3}},
		{"ab", 99, Position{1, 3, 2}},
		{"ab", -1, Position{1, 1, 0}},
	}
	for _, tt := range tests {
		if got := PositionOf(tt.text, tt.offset); got != tt.want {
			t.Errorf("PositionOf(%q, %d) = %v, want %v", tt.text, tt.offset, got, tt.want)
		}
	}
}
