package engine

import (
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// echo answers with the method, the target and what the server got, as lines.
func echo(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	w.Header().Set("X-Content-Type", r.Header.Get("Content-Type"))
	w.Header().Set("X-Method", r.Method)
	w.Header().Set("X-Target", r.URL.RequestURI())
	_, _ = w.Write(data)
}

func TestBodies(t *testing.T) {
	dir := project(t, map[string]string{
		"a.body.json":      `{"user": "{{who}}"}`,
		"a.body.xml":       `<u>{{who}}</u>`,
		"a.body.txt":       "hello {{who}}",
		"a.body.html":      "<b>{{who}}</b>",
		"a.body.js":        "var u = '{{who}}'",
		"q.body.graphql":   "query { user(id: \"{{who}}\") { name } }",
		"q.variables.json": `{"id": "{{who}}"}`,
		"bad.variables":    `{not json`,
		"data.bin":         "\x00\x01\x02binary\xff",
		"up.txt":           "file content",
	})
	vars := resolverOf(t, map[string]string{"who": "ada"})
	form := []format.Pair{pair("a b", "c&d"), {Name: "off", Value: "x"}, pair("v", "{{who}}")}
	tests := []struct {
		name        string
		body        format.Body
		wantType    string
		wantBody    string
		bodyPrefix  bool
		wantNoBody  bool
		multipartOK bool
	}{
		{name: "json", body: format.Body{Type: format.BodyJSON, File: "a.body.json"}, wantType: "application/json", wantBody: `{"user": "ada"}`},
		{name: "xml", body: format.Body{Type: format.BodyXML, File: "a.body.xml"}, wantType: "application/xml", wantBody: `<u>ada</u>`},
		{name: "text", body: format.Body{Type: format.BodyText, File: "a.body.txt"}, wantType: "text/plain", wantBody: "hello ada"},
		{name: "html", body: format.Body{Type: format.BodyHTML, File: "a.body.html"}, wantType: "text/html", wantBody: "<b>ada</b>"},
		{name: "javascript", body: format.Body{Type: format.BodyJavaScript, File: "a.body.js"}, wantType: "application/javascript", wantBody: "var u = 'ada'"},
		{name: "none", body: format.Body{Type: format.BodyNone}, wantNoBody: true},
		{name: "graphql", body: format.Body{Type: format.BodyGraphQL, File: "q.body.graphql", Variables: "q.variables.json"}, wantType: "application/json",
			wantBody: `{"query":"query { user(id: \"ada\") { name } }","variables":{"id":"ada"}}`},
		{name: "graphql without variables", body: format.Body{Type: format.BodyGraphQL, File: "q.body.graphql"}, wantType: "application/json",
			wantBody: `{"query":"query { user(id: \"ada\") { name } }"}`},
		{name: "form", body: format.Body{Type: format.BodyForm, Fields: form}, wantType: "application/x-www-form-urlencoded", wantBody: "a+b=c%26d&v=ada"},
		{name: "binary", body: format.Body{Type: format.BodyBinary, Path: "data.bin"}, wantType: "application/octet-stream", wantBody: "\x00\x01\x02binary\xff"},
		{name: "multipart", body: format.Body{Type: format.BodyMultipart, Fields: []format.Pair{pair("who", "{{who}}"), filePair("upload", "up.txt")}}, multipartOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotType, gotBody string
			var gotLen int64
			var fields map[string]string
			var fileName, fileBody string
			srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotType, gotLen = r.Header.Get("Content-Type"), r.ContentLength
				if tt.multipartOK {
					_, params, err := mime.ParseMediaType(gotType)
					if err != nil {
						t.Errorf("Content-Type %q: %v", gotType, err)
						return
					}
					fields = map[string]string{}
					mr := multipart.NewReader(r.Body, params["boundary"])
					for {
						part, err := mr.NextPart()
						if err != nil {
							break
						}
						data, _ := io.ReadAll(part)
						if part.FileName() != "" {
							fileName, fileBody = part.FileName(), string(data)
							fields["file:"+part.FormName()] = part.Header.Get("Content-Type")
						} else {
							fields[part.FormName()] = string(data)
						}
					}
					return
				}
				data, _ := io.ReadAll(r.Body)
				gotBody = string(data)
			})
			body := tt.body
			in := Input{Request: format.Request{Method: "POST", URL: srv.URL, Body: &body}, Resolver: vars, Folder: dir, Dir: dir}
			e := newEngine(t, Config{})
			resp := send(t, e, in)
			if resp.StatusCode != 200 {
				t.Fatalf("status %d", resp.StatusCode)
			}
			switch {
			case tt.wantNoBody:
				if gotBody != "" || gotType != "" {
					t.Fatalf("body %q type %q, want none", gotBody, gotType)
				}
			case tt.multipartOK:
				if !strings.HasPrefix(gotType, "multipart/form-data; boundary=") {
					t.Fatalf("type %q", gotType)
				}
				if fields["who"] != "ada" || fileName != "up.txt" || fileBody != "file content" || fields["file:upload"] != "text/plain; charset=utf-8" {
					t.Fatalf("fields %v file %q %q", fields, fileName, fileBody)
				}
				if resp.Request.BodySize != -1 {
					t.Fatalf("a multipart body is streamed, BodySize = %d", resp.Request.BodySize)
				}
			default:
				if gotType != tt.wantType || gotBody != tt.wantBody {
					t.Fatalf("type %q body %q, want %q %q", gotType, gotBody, tt.wantType, tt.wantBody)
				}
				if gotLen != int64(len(tt.wantBody)) {
					t.Fatalf("Content-Length %d, want %d", gotLen, len(tt.wantBody))
				}
			}
		})
	}
}

func TestUserContentTypeWins(t *testing.T) {
	dir := project(t, map[string]string{"a.json": "{}"})
	var got string
	srv := newServer(t, func(_ http.ResponseWriter, r *http.Request) { got = r.Header.Get("Content-Type") })
	in := Input{Request: format.Request{Method: "POST", URL: srv.URL, Headers: []format.Pair{pair("content-type", "application/vnd.api+json")}, Body: &format.Body{Type: format.BodyJSON, File: "a.json"}}, Folder: dir}
	send(t, newEngine(t, Config{}), in)
	if got != "application/vnd.api+json" {
		t.Fatalf("Content-Type %q", got)
	}
}

func TestParamsHeadersAndDefaults(t *testing.T) {
	var target, ua, host, ae string
	var extras, custom []string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		target, ua, host, ae = r.URL.RequestURI(), r.Header.Get("User-Agent"), r.Host, r.Header.Get("Accept-Encoding")
		extras, custom = r.Header.Values("X-Multi"), r.Header.Values("X-Inherited")
	})
	disabled := pair("skip", "me")
	disabled.Enabled = false
	in := Input{
		Request: format.Request{
			Method: "get", URL: srv.URL + "/path?a=1",
			Params:  []format.Pair{pair("q", "x y"), disabled, pair("{{k}}", "{{v}}"), pair("q", "again")},
			Headers: []format.Pair{pair("X-Multi", "1"), pair("X-Multi", "2"), pair("Host", "api.example.test"), pair("X-Inherited", "own")},
		},
		Headers:  []format.Pair{pair("x-inherited", "from collection"), pair("X-Only-Inherited", "yes"), pair("X-Gone", "{{missing}}")},
		Resolver: resolverOf(t, map[string]string{"k": "key", "v": "val&"}),
	}
	resp := send(t, newEngine(t, Config{UserAgent: "Emetos/test"}), in)
	if target != "/path?a=1&q=x+y&key=val%26&q=again" {
		t.Fatalf("target %q", target)
	}
	if ua != "Emetos/test" || host != "api.example.test" || ae != "gzip, deflate" {
		t.Fatalf("User-Agent %q Host %q Accept-Encoding %q", ua, host, ae)
	}
	if strings.Join(extras, ",") != "1,2" || strings.Join(custom, ",") != "own" {
		t.Fatalf("headers %v %v", extras, custom)
	}
	if len(resp.Unresolved) != 1 || resp.Unresolved[0] != "missing" {
		t.Fatalf("Unresolved %v", resp.Unresolved)
	}
	if resp.Request.Header.Get("X-Only-Inherited") != "yes" || resp.Request.Method != "GET" {
		t.Fatalf("Sent %+v", resp.Request)
	}
}

func TestURLWithoutScheme(t *testing.T) {
	srv := newServer(t, echo)
	resp := send(t, newEngine(t, Config{}), get(strings.TrimPrefix(srv.URL, "http://")+"/x"))
	if resp.StatusCode != 200 || resp.Header.Get("X-Target") != "/x" {
		t.Fatalf("response %d %q", resp.StatusCode, resp.Header.Get("X-Target"))
	}
}

func TestPrepareErrors(t *testing.T) {
	dir := project(t, map[string]string{"bad.variables": `{not json`, "q.graphql": "query {}", "d/x": ""})
	tests := []struct {
		name  string
		req   format.Request
		auth  *format.Auth
		oauth *OAuthToken
		route string
	}{
		{name: "method", req: format.Request{Method: "GE T", URL: "http://127.0.0.1"}, route: "Engine.Prepare.EngineMethodInvalid"},
		{name: "url unparsable", req: format.Request{URL: "http://127.0.0.1:port/"}, route: "Engine.Prepare.EngineURLInvalid"},
		{name: "url scheme", req: format.Request{URL: "ftp://127.0.0.1/"}, route: "Engine.Prepare.EngineURLInvalid"},
		{name: "header name", req: format.Request{URL: "http://127.0.0.1", Headers: []format.Pair{pair("Bad Name", "x")}}, route: "Engine.Prepare->Engine.Prepare.func2.EngineHeaderInvalid"},
		{name: "header value", req: format.Request{URL: "http://127.0.0.1", Headers: []format.Pair{pair("X", "a\r\nb")}}, route: "Engine.Prepare->Engine.Prepare.func2.EngineHeaderInvalid"},
		{name: "body type", req: format.Request{URL: "http://127.0.0.1", Body: &format.Body{Type: "protobuf"}}, route: "buildBody.EngineBodyInvalid"},
		{name: "body file missing", req: format.Request{URL: "http://127.0.0.1", Body: &format.Body{Type: format.BodyJSON, File: "nope.json"}}, route: "readBodyFile.EngineBodyFileReadFailed"},
		{name: "body file outside", req: format.Request{URL: "http://127.0.0.1", Body: &format.Body{Type: format.BodyJSON, File: "../x.json"}}, route: "readBodyFile->ResolvePath.HttpPathOutsideWorkspace"},
		{name: "graphql variables", req: format.Request{URL: "http://127.0.0.1", Body: &format.Body{Type: format.BodyGraphQL, File: "q.graphql", Variables: "bad.variables"}}, route: "buildBody.EngineGraphQLVariablesInvalid"},
		{name: "binary no path", req: format.Request{URL: "http://127.0.0.1", Body: &format.Body{Type: format.BodyBinary}}, route: "buildBody.EngineBodyFileReadFailed"},
		{name: "binary missing", req: format.Request{URL: "http://127.0.0.1", Body: &format.Body{Type: format.BodyBinary, Path: "nope.bin"}}, route: "buildBody.EngineBodyFileReadFailed"},
		{name: "binary folder", req: format.Request{URL: "http://127.0.0.1", Body: &format.Body{Type: format.BodyBinary, Path: "d"}}, route: "buildBody.EngineBodyFileReadFailed"},
		{name: "binary outside", req: format.Request{URL: "http://127.0.0.1", Body: &format.Body{Type: format.BodyBinary, Path: "../x"}}, route: "buildBody->ResolvePath.HttpPathOutsideWorkspace"},
		{name: "multipart missing", req: format.Request{URL: "http://127.0.0.1", Body: &format.Body{Type: format.BodyMultipart, Fields: []format.Pair{filePair("f", "nope")}}}, route: "multipartSource.EngineMultipartFileFailed"},
		{name: "multipart folder", req: format.Request{URL: "http://127.0.0.1", Body: &format.Body{Type: format.BodyMultipart, Fields: []format.Pair{filePair("f", "d")}}}, route: "multipartSource.EngineMultipartFileFailed"},
		{name: "multipart outside", req: format.Request{URL: "http://127.0.0.1", Body: &format.Body{Type: format.BodyMultipart, Fields: []format.Pair{filePair("f", "../x")}}}, route: "multipartSource->ResolvePath.HttpPathOutsideWorkspace"},
		{name: "oauth2 no token", req: format.Request{URL: "http://127.0.0.1"}, auth: &format.Auth{Type: format.AuthOAuth2}, route: "applyAuth->Prepared.applyOAuth.EngineOAuthTokenMissing"},
		{name: "oauth2 place", req: format.Request{URL: "http://127.0.0.1"}, auth: &format.Auth{Type: format.AuthOAuth2, AddTo: "cookie"}, oauth: &OAuthToken{Value: "t"}, route: "applyAuth->Prepared.applyOAuth.EngineAuthInvalid"},
		{name: "auth type", req: format.Request{URL: "http://127.0.0.1"}, auth: &format.Auth{Type: "ntlm"}, route: "applyAuth.EngineAuthUnsupported"},
		{name: "api key no name", req: format.Request{URL: "http://127.0.0.1"}, auth: &format.Auth{Type: format.AuthAPIKey}, route: "applyAuth.EngineAuthInvalid"},
		{name: "api key place", req: format.Request{URL: "http://127.0.0.1"}, auth: &format.Auth{Type: format.AuthAPIKey, Key: "k", In: "cookie"}, route: "applyAuth.EngineAuthInvalid"},
		{name: "api key header name", req: format.Request{URL: "http://127.0.0.1"}, auth: &format.Auth{Type: format.AuthAPIKey, Key: "bad name"}, route: "applyAuth.EngineHeaderInvalid"},
	}
	e := newEngine(t, Config{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.Send(t.Context(), Input{Request: tt.req, Auth: tt.auth, OAuth: tt.oauth, Folder: dir, Dir: dir})
			wantRoute(t, err, tt.route)
		})
	}
}

func TestAuth(t *testing.T) {
	tests := []struct {
		name       string
		auth       format.Auth
		oauth      *OAuthToken
		wantHeader string
		wantQuery  string
	}{
		{name: "basic", auth: format.Auth{Type: format.AuthBasic, Username: "ada", Password: "p:w"}, wantHeader: "Authorization: Basic YWRhOnA6dw=="},
		{name: "bearer", auth: format.Auth{Type: format.AuthBearer, Token: "{{tok}}"}, wantHeader: "Authorization: Bearer secret-token"},
		{name: "api key in a header", auth: format.Auth{Type: format.AuthAPIKey, Key: "X-API-Key", Value: "k1"}, wantHeader: "X-Api-Key: k1"},
		{name: "api key in the query", auth: format.Auth{Type: format.AuthAPIKey, Key: "api key", Value: "k&1", In: "query"}, wantQuery: "api+key=k%261"},
		{name: "oauth2 in the header", auth: format.Auth{Type: format.AuthOAuth2}, oauth: &OAuthToken{Value: "at-1"}, wantHeader: "Authorization: Bearer at-1"},
		{name: "oauth2 other scheme", auth: format.Auth{Type: format.AuthOAuth2, AddTo: "header"}, oauth: &OAuthToken{Value: "at-1", Scheme: "MAC"}, wantHeader: "Authorization: MAC at-1"},
		{name: "oauth2 in the query", auth: format.Auth{Type: format.AuthOAuth2, AddTo: "query"}, oauth: &OAuthToken{Value: "a&1"}, wantQuery: "access_token=a%261"},
		{name: "none", auth: format.Auth{Type: format.AuthNone}},
		{name: "inherit that was not resolved", auth: format.Auth{Type: format.AuthInherit}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth, gotKey, query string
			srv := newServer(t, func(_ http.ResponseWriter, r *http.Request) {
				gotAuth, gotKey, query = r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"), r.URL.RawQuery
			})
			auth := tt.auth
			in := Input{Request: format.Request{Method: "GET", URL: srv.URL}, Auth: &auth, OAuth: tt.oauth, Resolver: resolverOf(t, map[string]string{"tok": "secret-token"})}
			send(t, newEngine(t, Config{}), in)
			got := ""
			switch {
			case gotAuth != "":
				got = "Authorization: " + gotAuth
			case gotKey != "":
				got = "X-Api-Key: " + gotKey
			}
			if got != tt.wantHeader || query != tt.wantQuery {
				t.Fatalf("header %q query %q, want %q %q", got, query, tt.wantHeader, tt.wantQuery)
			}
		})
	}
}

func TestRequestAuthUsedWithoutInput(t *testing.T) {
	var got string
	srv := newServer(t, func(_ http.ResponseWriter, r *http.Request) { got = r.Header.Get("Authorization") })
	in := get(srv.URL)
	in.Request.Auth = &format.Auth{Type: format.AuthBearer, Token: "own"}
	send(t, newEngine(t, Config{}), in)
	if got != "Bearer own" {
		t.Fatalf("Authorization %q", got)
	}
}

func TestTextsAreUsedInPlaceOfBodyFiles(t *testing.T) {
	dir := project(t, map[string]string{"a.body.json": `{"from": "the file"}`})
	vars := resolverOf(t, map[string]string{"who": "ada"})
	for _, tc := range []struct {
		name  string
		body  format.Body
		texts map[string]string
		want  string
	}{
		{"a text beats the file", format.Body{Type: format.BodyJSON, File: "a.body.json"}, map[string]string{"a.body.json": `{"user": "{{who}}"}`}, `{"user": "ada"}`},
		{"a name that is not there is read from the folder", format.Body{Type: format.BodyJSON, File: "a.body.json"}, map[string]string{"other": "x"}, `{"from": "the file"}`},
		{"a name with no file at all", format.Body{Type: format.BodyJSON, File: "unsaved.body.json"}, map[string]string{"unsaved.body.json": "{}"}, "{}"},
		{"graphql query and variables", format.Body{Type: format.BodyGraphQL, File: "q.graphql", Variables: "v.json"}, map[string]string{"q.graphql": "{ me }", "v.json": `{"id": 1}`}, `{"query":"{ me }","variables":{"id":1}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				got = string(data)
			})
			e := newEngine(t, Config{})
			in := Input{Request: format.Request{Method: "POST", URL: srv.URL, Body: &tc.body}, Folder: dir, Dir: dir, Resolver: vars, Texts: tc.texts}
			send(t, e, in)
			if got != tc.want {
				t.Errorf("body = %q, want %q", got, tc.want)
			}
		})
	}
}
