package httpclient

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/checks"
	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

// The extraction rules and the checks, from the Send of the request tab to the values and the results (SPEC §10.33 "Extraction and
// checks"). Every request goes to a server on 127.0.0.1.

// loginServer answers every request with 201, JSON, a header and a cookie, and records the paths it was asked for.
type loginServer struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func newLoginServer(t *testing.T) *loginServer {
	t.Helper()
	s := &loginServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Session-Token", "sess-4711")
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "cookie-abc", Path: "/"})
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":17,"data":{"token":"jwt-xyz"},"items":[{"id":5},{"id":6}],"ok":true}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *loginServer) asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// projectFiles reads every file under .emetos of the folder, by path.
func projectFiles(t *testing.T, folder string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(filepath.Join(folder, ".emetos"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(path)
		out[path] = string(data)
		return rerr
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameFiles(t *testing.T, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Errorf("the project had %d files and has %d", len(before), len(after))
	}
	for p, text := range before {
		if after[p] != text {
			t.Errorf("%s changed:\n%s", p, after[p])
		}
	}
}

// sendWith sets the rules and the checks of the tab and sends it.
func (r *rig) sendWith(tab *reqTab, rules []format.ExtractRule, list []format.Check) {
	r.t.Helper()
	tab.setRules(rules, list)
	tab.sendRequest()
	r.settle()
	if tab.send.resp == nil {
		r.t.Fatalf("no response: %q", tab.send.status)
	}
}

func globalValue(t *testing.T, r *rig, name string) (variables.Entry, bool) {
	t.Helper()
	path, err := variables.GlobalsPath()
	if err != nil {
		t.Fatal(err)
	}
	entries, _, err := variables.NewGlobals(path, r.kr).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return variables.Entry{}, false
}

func TestEachSourceAndScopeOfAnExtractionRule(t *testing.T) {
	devOwner, shopOwner := variables.EnvironmentOwner("dev"), variables.CollectionOwner("shop-api")
	for _, tc := range []struct {
		name  string
		rule  format.ExtractRule
		owner string // "" for a global
		var_  string
		want  string
	}{
		{"a number of the body into the environment", format.ExtractRule{From: "body", Path: "$.id", To: "userId", Scope: "environment"}, devOwner, "userId", "17"},
		{"no scope is the environment", format.ExtractRule{From: "body", Path: "$.data.token", To: "token"}, devOwner, "token", "jwt-xyz"},
		{"the body into the collection", format.ExtractRule{From: "body", Path: "$.items[*].id", To: "firstItem", Scope: "collection"}, shopOwner, "firstItem", "5"},
		{"a header into the environment", format.ExtractRule{From: "header", Header: "x-session-token", To: "sess", Scope: "environment"}, devOwner, "sess", "sess-4711"},
		{"a header with a regex into the collection", format.ExtractRule{From: "header", Header: "X-Session-Token", Regex: `sess-(\d+)`, To: "sessNo", Scope: "collection"}, shopOwner, "sessNo", "4711"},
		{"the status into the collection", format.ExtractRule{From: "status", To: "code", Scope: "collection"}, shopOwner, "code", "201"},
		{"a cookie into the environment", format.ExtractRule{From: "cookie", Cookie: "session", To: "cookieValue", Scope: "environment"}, devOwner, "cookieValue", "cookie-abc"},
		{"a regex over the body", format.ExtractRule{From: "body", Regex: `"token":"([^"]+)"`, To: "rx", Scope: "environment"}, devOwner, "rx", "jwt-xyz"},
		{"a variable into the globals", format.ExtractRule{From: "body", Path: "$.id", To: "globalUser", Scope: "global"}, "", "globalUser", "17"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newLoginServer(t)
			r := newRig(t)
			r.useBase(srv.URL)
			tab := r.openReq(pList)
			before := projectFiles(t, r.folder)
			r.sendWith(tab, []format.ExtractRule{tc.rule}, nil)
			var got string
			var ok bool
			if tc.owner == "" {
				var e variables.Entry
				e, ok = globalValue(t, r, tc.var_)
				got = e.Value
			} else {
				got, ok = r.ext.values.Get(tc.owner, tc.var_)
			}
			if !ok || got != tc.want {
				t.Errorf("%s = %q (%v), want %q", tc.var_, got, ok, tc.want)
			}
			if len(tab.send.extracted) != 1 || !tab.send.extracted[0].Stored || tab.send.extracted[0].Problem != "" {
				t.Errorf("outcome = %+v", tab.send.extracted)
			}
			// nothing went into a file of the project
			sameFiles(t, before, projectFiles(t, r.folder))
			// and the value is used by the next request
			tab.d.url.SetValue("{{baseUrl}}/v1/{{" + tc.var_ + "}}")
			tab.sendRequest()
			r.settle()
			paths := srv.asked()
			if want := "/v1/" + tc.want; paths[len(paths)-1] != want {
				t.Errorf("the next request went to %q, want %q", paths[len(paths)-1], want)
			}
		})
	}
}

func TestAnExtractedValueOfASecretVariableGoesToTheKeyring(t *testing.T) {
	collectionWithSecret := `{"emetosHttp": 1, "name": "Shop API", "variables": [{"name": "collToken", "secret": true}]}`
	r := newRig(t, func(folder string) {
		writeFile(t, folder+"/.emetos/http/collections/shop-api/collection.jsonc", collectionWithSecret)
	})
	srv := newLoginServer(t)
	r.useBase(srv.URL)
	// a secret global
	path, err := variables.GlobalsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := variables.NewGlobals(path, r.kr).Save(context.Background(), []variables.Entry{{Name: "globalSecret", Value: "old", Enabled: true, Secret: true}}); err != nil {
		t.Fatal(err)
	}
	tab := r.openReq(pList)
	before := projectFiles(t, r.folder)
	r.sendWith(tab, []format.ExtractRule{
		{From: "body", Path: "$.data.token", To: "apiKey", Scope: "environment"},   // Dev declares apiKey as a secret
		{From: "body", Path: "$.data.token", To: "collToken", Scope: "collection"}, // the collection declares collToken as a secret
		{From: "body", Path: "$.data.token", To: "globalSecret", Scope: "global"},  // so does the globals file
		{From: "body", Path: "$.data.token", To: "plain", Scope: "environment"},    // not declared: not a secret
	}, nil)
	for _, tc := range []struct{ owner, name string }{
		{variables.EnvironmentOwner("dev"), "apiKey"},
		{variables.CollectionOwner("shop-api"), "collToken"},
		{variables.GlobalOwner, "globalSecret"},
	} {
		key := variables.SecretKey(r.folder, tc.owner, tc.name)
		if tc.owner == variables.GlobalOwner {
			key = variables.SecretKey("", tc.owner, tc.name)
		}
		got, found, err := r.kr.Lookup(context.Background(), key)
		if err != nil || !found || got != "jwt-xyz" {
			t.Errorf("the keyring has %q (%v, %v) for %s", got, found, err, tc.name)
		}
		if _, in := r.ext.values.Get(tc.owner, tc.name); in {
			t.Errorf("%s is in the state as well", tc.name)
		}
	}
	if v, ok := r.ext.values.Get(variables.EnvironmentOwner("dev"), "plain"); !ok || v != "jwt-xyz" {
		t.Errorf("plain = %q, %v", v, ok)
	}
	for i, o := range tab.send.extracted {
		wantSecret := i < 3
		if o.Secret != wantSecret || !o.Stored {
			t.Errorf("outcome %d = %+v", i, o)
		}
		if wantSecret && o.shown() != "********" {
			t.Errorf("outcome %d shows %q", i, o.shown())
		}
	}
	// no file of the project, and no state, holds the secret
	sameFiles(t, before, projectFiles(t, r.folder))
	for p, text := range projectFiles(t, r.folder) {
		if strings.Contains(text, "jwt-xyz") {
			t.Errorf("%s holds the secret", p)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "jwt-xyz") {
		t.Error("the globals file holds the secret")
	}
	if e, _ := globalValue(t, r, "globalSecret"); !e.Secret || e.Value != "jwt-xyz" {
		t.Errorf("the global = %+v", e)
	}
	if raw, ok := r.api.State().Get(variables.StateKey); ok && strings.Contains(string(raw), "jwt-xyz") && strings.Contains(string(raw), "apiKey") {
		t.Errorf("the state holds a secret: %s", raw)
	}
}

func TestARuleThatFindsNothingOrCannotStoreLeavesTheVariable(t *testing.T) {
	r := newRig(t)
	srv := newLoginServer(t)
	r.useBase(srv.URL)
	if err := r.ext.values.Set(context.Background(), variables.EnvironmentOwner("dev"), "keep", "old", false); err != nil {
		t.Fatal(err)
	}
	tab := r.openReq(pList)
	r.sendWith(tab, []format.ExtractRule{
		{From: "body", Path: "$.nothing", To: "keep"},
		{From: "header", Header: "X-Nothing", To: "keep2"},
		{From: "body", Path: "$.id", To: "ok"},
		{From: "body", Path: "$.id", To: "x", Scope: "collection"}, // pList is in shop-api: this one works
	}, nil)
	if v, _ := r.ext.values.Get(variables.EnvironmentOwner("dev"), "keep"); v != "old" {
		t.Errorf("keep = %q", v)
	}
	want := []struct {
		stored  bool
		problem string
	}{{false, "matches nothing"}, {false, "no such header"}, {true, ""}, {true, ""}}
	for i, w := range want {
		o := tab.send.extracted[i]
		if o.Stored != w.stored || !strings.Contains(o.Problem, w.problem) || (w.problem != "" && o.OK()) {
			t.Errorf("outcome %d = %+v", i, o)
		}
	}
	notes := tab.respNotes()
	if !strings.Contains(notes, "not extracted into keep") {
		t.Errorf("the notes of the pane = %q", notes)
	}
	// the problems of one send are gone with the next
	tab.setRules(nil, nil)
	tab.sendRequest()
	r.settle()
	if len(tab.send.extracted) != 0 || strings.Contains(tab.respNotes(), "not extracted") {
		t.Error("the outcomes of the last send are still there")
	}
}

func TestWhereAValueCannotBeStoredTheRuleSaysSo(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		p     place
		scope string
		route string
	}{
		{"no environment", place{}, "environment", "Extension.storeExtracted.HttpExtractNoEnvironment"},
		{"no collection", place{}, "collection", "Extension.storeExtracted.HttpExtractNoCollection"},
		{"an unknown scope", place{}, "region", "Extension.storeExtracted.HttpExtractScopeUnknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := &extractOutcome{Scope: tc.scope, Extracted: checks.Extracted{Rule: format.ExtractRule{To: "v"}, Value: "1"}}
			err := r.ext.storeExtracted(ctx, tc.p, o)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	// through a send: a request that is not in a collection, and no environment
	srv := newLoginServer(t)
	tab := r.openReq(pList)
	tab.d.url.SetValue(srv.URL + "/x")
	r.sendWith(tab, []format.ExtractRule{{From: "status", To: "code", Scope: "environment"}}, nil)
	if o := tab.send.extracted[0]; o.Stored || !strings.Contains(o.Problem, "no environment is selected") {
		t.Errorf("outcome = %+v", o)
	}
}

func TestAValueExtractedByOneRequestIsUsedByTheNext(t *testing.T) {
	srv := newLoginServer(t)
	r := newRig(t)
	r.useBase(srv.URL)
	login := r.openReq(pCreate)
	r.sendWith(login, []format.ExtractRule{{From: "body", Path: "$.id", To: "userId"}}, nil)
	next := r.openReq(pList)
	next.d.url.SetValue("{{baseUrl}}/v1/users/{{userId}}")
	next.sendRequest()
	r.settle()
	paths := srv.asked()
	if len(paths) != 2 || paths[1] != "/v1/users/17" {
		t.Errorf("paths = %v", paths)
	}
}

func TestChecksRunAfterASendAndShowTheirResults(t *testing.T) {
	srv := newLoginServer(t)
	r := newRig(t)
	r.useBase(srv.URL)
	tab := r.openReq(pList)
	r.sendWith(tab, nil, []format.Check{
		{Subject: "status", Op: "equals", Value: 201.0},
		{Subject: "json", Path: "$.data.token", Op: "equals", Value: "wrong"},
		{Subject: "header", Header: "X-Session-Token", Op: "matches", Value: `^sess-\d+$`},
		{Subject: "json", Path: "$.missing", Op: "equals", Value: 1.0},
	})
	res := tab.send.checkResults
	if len(res) != 4 {
		t.Fatalf("%d results", len(res))
	}
	for i, want := range []bool{true, false, true, false} {
		if res[i].Passed != want {
			t.Errorf("check %d passed = %v: %+v", i, res[i].Passed, res[i])
		}
	}
	if res[1].Actual != "jwt-xyz" || res[1].Expected != `equals "wrong"` || res[1].What != "json $.data.token" {
		t.Errorf("failed check = %+v", res[1])
	}
	if res[3].Problem == "" {
		t.Errorf("a path that matches nothing has no reason: %+v", res[3])
	}
	if got := tab.checksLabel(); got != "Checks (2/4)" {
		t.Errorf("label = %q", got)
	}
	// a new Send takes the results away until the response is there
	tab.setRules(nil, nil)
	tab.sendRequest()
	r.settle()
	if got := tab.checksLabel(); got != "Checks" || len(tab.send.checkResults) != 0 {
		t.Errorf("label = %q, results %d", got, len(tab.send.checkResults))
	}
}

func TestAChecksTabInTheResponseShowsEveryCheck(t *testing.T) {
	r, tab := reqRig(t, pCreate)
	r.respondWithRules(tab, jsonResponse(), nil, []format.Check{
		{Subject: "status", Op: "equals", Value: 200.0},
		{Subject: "json", Path: "$.name", Op: "equals", Value: "Grace"},
	})
	ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
	ts.Pump(time.Millisecond)
	ts.Tap(gluttertest.Last(gluttertest.ByTextContaining("Checks (1/2)", rich)))
	ts.Pump(time.Millisecond)
	if tab.pane.tab != respChecks {
		t.Fatalf("the pane is on %q", tab.pane.tab)
	}
	for _, want := range []string{"1 passed, 1 failed", "Passed", "Failed", "equals \"Grace\"", "Ada Example", "json $.name"} {
		if !ts.Exists(gluttertest.ByTextContaining(want, rich)) {
			t.Errorf("the Checks tab does not show %q", want)
		}
	}
}

// useBase makes {{baseUrl}} of the Dev environment the address.
func (r *rig) useBase(url string) {
	r.t.Helper()
	r.run(CmdSelectEnvironment, "dev")
	if err := r.ext.values.Set(context.Background(), variables.EnvironmentOwner("dev"), "baseUrl", url, false); err != nil {
		r.t.Fatal(err)
	}
}

// respondWithRules hands a response to the tab with the outcomes of the rules and the results of the checks, as the end of a send does
// (the values are not stored: the outcomes say they were).
func (r *rig) respondWithRules(tab *reqTab, resp *engine.Response, rules []format.ExtractRule, list []format.Check) {
	r.t.Helper()
	out := sendOutcome{resp: resp, duration: 118 * time.Millisecond, checkResults: checks.Run(resp, list)}
	for _, x := range checks.Extract(resp, rules) {
		o := extractOutcome{Extracted: x, Scope: x.Rule.Scope, Stored: x.OK()}
		if o.Scope == "" {
			o.Scope = scopeEnvironment
		}
		out.extracted = append(out.extracted, o)
	}
	tab.setRules(rules, list)
	tab.finishSend(sendJob{files: tab.d.files()}, out)
}

// recordOutcomes gives the tab the outcomes of the rules and the results of the checks of a response without showing the response, so
// that the forms can be looked at with what the last send made of them.
func (r *rig) recordOutcomes(tab *reqTab, resp *engine.Response, rules []format.ExtractRule, list []format.Check) {
	r.t.Helper()
	tab.setRules(rules, list)
	tab.send.checkResults = checks.Run(resp, list)
	for _, x := range checks.Extract(resp, rules) {
		o := extractOutcome{Extracted: x, Scope: x.Rule.Scope, Stored: x.OK()}
		if o.Scope == "" {
			o.Scope = scopeEnvironment
		}
		tab.send.extracted = append(tab.send.extracted, o)
	}
}

func TestAnEnvironmentEditorWithoutEditsShowsTheValuesASendExtracted(t *testing.T) {
	srv := newLoginServer(t)
	r := newRig(t)
	r.useBase(srv.URL)
	env, _ := r.openEnv("dev")
	tab := r.openReq(pList)
	r.sendWith(tab, []format.ExtractRule{{From: "body", Path: "$.id", To: "debug"}, {From: "status", To: "undeclared"}}, nil)
	// the editor lists the variables the file declares: debug is declared, so its row has the value the send extracted
	var baseRow *envRow
	for _, row := range env.rows {
		if row.name.Value() == "debug" {
			baseRow = row
		}
	}
	if baseRow == nil || baseRow.current.Value() != "17" {
		t.Fatalf("the editor does not show the extracted value of debug: %+v", baseRow)
	}
	if v, ok := r.ext.values.Get(variables.EnvironmentOwner("dev"), "undeclared"); !ok || v != "201" {
		t.Errorf("undeclared = %q, %v", v, ok)
	}
}
