package httpclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

// reqTabPaths are the files of the request tabs that are open, in order ("" for a request that is not saved).
func (r *rig) reqTabPaths() []string {
	var out []string
	for _, t := range r.ext.reqTabs {
		out = append(out, t.path)
	}
	return out
}

// openReq opens the request tab of a request of the collections (a path under collections/) and waits until it has read the files.
func (r *rig) openReq(rel string) *reqTab {
	r.t.Helper()
	r.run(CmdOpenRequest, r.col(rel))
	r.api.WaitFor("the request to be read", func() bool {
		n := len(r.ext.reqTabs)
		return n > 0 && r.ext.reqTabs[n-1].d != nil
	})
	r.settle()
	return r.ext.reqTabs[len(r.ext.reqTabs)-1]
}

// showReq shows the request tab in a window of the theme.
func (r *rig) showReq(th uitest.Theme, t *reqTab, w, h float64) *gluttertest.Tester {
	r.t.Helper()
	changed := foundation.NewChangeNotifier()
	t.tab = refreshTab{t.tab, changed}
	body := widgets.NewListenableBuilder(changed, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget { return t.build(bc) }, nil)
	return uitest.Pump(r.t, th, gui.Size{Width: w, Height: h}, body)
}

// fakeTabOf is the fake's record of the tab (its title, dirty mark and content).
func (r *rig) fakeTabOf(t *reqTab) interface {
	Dirty() bool
	Title() string
} {
	for _, tab := range r.api.EditorTabs() {
		if tab.ID() == t.tab.ID() {
			return tab
		}
	}
	r.t.Fatal("no such tab")
	return nil
}

func TestARequestFileOpensInTheRequestTab(t *testing.T) {
	r := newRig(t)
	tab := r.openReq(pCreate)
	if tab.d.method != "POST" || tab.d.url.Value() != "{{baseUrl}}/v1/users" || tab.d.bodyType != format.BodyJSON {
		t.Errorf("method %q url %q body %q", tab.d.method, tab.d.url.Value(), tab.d.bodyType)
	}
	if got := tab.d.texts[format.BodyJSON].Value(); !strings.Contains(got, "Ada Example") {
		t.Errorf("the body text = %q", got)
	}
	ft := r.fakeTabOf(tab)
	if ft.Title() != "Create user" || ft.Dirty() {
		t.Errorf("title %q dirty %v", ft.Title(), ft.Dirty())
	}
	// opening it again shows the tab that is open
	r.run(CmdOpenRequest, r.col(pCreate))
	if len(r.ext.reqTabs) != 1 {
		t.Errorf("%d request tabs", len(r.ext.reqTabs))
	}
	// the list request has a parameter, which is in the URL box
	list := r.openReq(pList)
	if list.d.url.Value() != "{{baseUrl}}/v1/users?page=1" || len(list.d.params) != 1 {
		t.Errorf("url %q params %d", list.d.url.Value(), len(list.d.params))
	}
	if list.dirty() {
		t.Error("a request is dirty as it opens")
	}
}

func TestParamsAndTheURLStayInStep(t *testing.T) {
	type row = paramRow
	for _, tc := range []struct {
		name     string
		url      string
		rows     []row
		wantRows []row
		wantURL  string
	}{
		{"a query typed in the URL becomes rows", "http://h/p?a=1&b=2", nil, []row{{"a", "1", true}, {"b", "2", true}}, "http://h/p?a=1&b=2"},
		{"the rows are replaced one for one", "http://h/p?a=9", []row{{"a", "1", true}, {"b", "2", true}}, []row{{"a", "9", true}}, "http://h/p?a=9"},
		{"a row that is off stays and is not in the URL", "http://h/p?a=1", []row{{"x", "off", false}, {"a", "1", true}}, []row{{"x", "off", false}, {"a", "1", true}}, "http://h/p?a=1"},
		{"a name without a value", "http://h/p?flag", nil, []row{{"flag", "", true}}, "http://h/p?flag"},
		{"escapes are decoded in the table and kept safe in the URL", "http://h/p?q=a%26b%20c", nil, []row{{"q", "a&b c", true}}, "http://h/p?q=a%26b c"},
		{"a plus is a space", "http://h/p?q=a+b", nil, []row{{"q", "a b", true}}, "http://h/p?q=a b"},
		{"no query, no question mark", "http://h/p", []row{{"a", "1", true}}, nil, "http://h/p"},
		{"the fragment stays", "http://h/p?a=1#top", nil, []row{{"a", "1", true}}, "http://h/p?a=1#top"},
		{"variables stay as typed", "{{baseUrl}}/p?id={{id}}", nil, []row{{"id", "{{id}}", true}}, "{{baseUrl}}/p?id={{id}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, query, _, _ := splitURL(tc.url)
			got, _ := rowsFromQuery(tc.rows, query)
			if len(got) != len(tc.wantRows) {
				t.Fatalf("rows = %+v, want %+v", got, tc.wantRows)
			}
			for i := range got {
				if got[i] != tc.wantRows[i] {
					t.Errorf("row %d = %+v, want %+v", i, got[i], tc.wantRows[i])
				}
			}
			if url := urlWithQuery(tc.url, queryOfRows(got)); url != tc.wantURL {
				t.Errorf("url = %q, want %q", url, tc.wantURL)
			}
		})
	}
}

func TestEditingTheTableOrTheURLChangesTheOther(t *testing.T) {
	r := newRig(t)
	tab := r.openReq(pList)
	// typing in the URL box changes the table
	tab.d.url.SetValue("{{baseUrl}}/v1/users?page=2&size=10")
	if len(tab.d.params) != 2 || tab.d.params[0].value.Value() != "2" || tab.d.params[1].name.Value() != "size" {
		t.Fatalf("params after the URL: %+v", tab.d.params)
	}
	if !r.fakeTabOf(tab).Dirty() {
		t.Error("the tab is not dirty")
	}
	// a value typed in the table changes the URL; a row that is off leaves it
	tab.d.params[1].value.SetValue("50")
	if got := tab.d.url.Value(); got != "{{baseUrl}}/v1/users?page=2&size=50" {
		t.Errorf("url = %q", got)
	}
	tab.toggleRow(tab.d.params[0], false, secParams)
	if got := tab.d.url.Value(); got != "{{baseUrl}}/v1/users?size=50" {
		t.Errorf("url with a row off = %q", got)
	}
	// a new row and a removed row
	tab.addRow(&tab.d.params, secParams)
	tab.d.params[2].name.SetValue("sort")
	tab.d.params[2].value.SetValue("a&b")
	if got := tab.d.url.Value(); got != "{{baseUrl}}/v1/users?size=50&sort=a%26b" {
		t.Errorf("url with a new row = %q", got)
	}
	tab.removeRow(&tab.d.params, tab.d.params[1], secParams)
	if got := tab.d.url.Value(); got != "{{baseUrl}}/v1/users?sort=a%26b" {
		t.Errorf("url after removing = %q", got)
	}
	// the file keeps the URL without its query and every row, the one that is off included
	files := tab.d.files()
	if files.Request.URL != "{{baseUrl}}/v1/users" || len(files.Request.Params) != 2 || files.Request.Params[0].Enabled {
		t.Errorf("saved request = %+v", files.Request)
	}
	// an edit that is undone leaves the tab clean
	fresh := r.openReq(pCreate)
	fresh.d.url.SetValue("{{baseUrl}}/v1/users?x=1")
	if !fresh.dirty() {
		t.Error("the edit did not make the tab dirty")
	}
	fresh.d.url.SetValue("{{baseUrl}}/v1/users")
	if fresh.dirty() || r.fakeTabOf(fresh).Dirty() {
		t.Error("the tab is dirty after the edit was undone")
	}
}

func TestBulkEdit(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want []format.Pair
	}{
		{"lines", "Accept: application/json\nX-Trace: 1", []format.Pair{{Name: "Accept", Value: "application/json", Enabled: true}, {Name: "X-Trace", Value: "1", Enabled: true}}},
		{"a hash turns a line off", "# Accept: application/json", []format.Pair{{Name: "Accept", Value: "application/json", Enabled: false}}},
		{"the first colon cuts", "Date: 12:30:00", []format.Pair{{Name: "Date", Value: "12:30:00", Enabled: true}}},
		{"blank lines and carriage returns", "\nA: 1\r\n\r\nB:\n", []format.Pair{{Name: "A", Value: "1", Enabled: true}, {Name: "B", Enabled: true}}},
		{"a name alone", "Flag", []format.Pair{{Name: "Flag", Enabled: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBulk(tc.text)
			if len(got) != len(tc.want) {
				t.Fatalf("pairs = %+v", got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("pair %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
	r := newRig(t)
	tab := r.openReq(pList)
	tab.section = secParams
	tab.toggleBulk(secParams)
	box := tab.bulkBox(secParams)
	if box.Value() != "page: 1" {
		t.Fatalf("bulk text = %q", box.Value())
	}
	box.SetValue("page: 3\nq: ada\n# off: yes")
	if len(tab.d.params) != 3 || tab.d.params[1].value.Value() != "ada" || tab.d.params[2].enabled {
		t.Fatalf("rows = %+v", tab.d.params)
	}
	if got := tab.d.url.Value(); got != "{{baseUrl}}/v1/users?page=3&q=ada" {
		t.Errorf("url = %q", got)
	}
	// the table changes the bulk text
	tab.d.params[0].value.SetValue("4")
	if got := box.Value(); got != "page: 4\nq: ada\n# off: yes" {
		t.Errorf("bulk after a table edit = %q", got)
	}
	// going back to the table
	tab.toggleBulk(secParams)
	if tab.open[secParams] {
		t.Error("the bulk editor is still open")
	}
}

// echoServer is a server on 127.0.0.1 that records what it gets and answers with JSON.
type echoServer struct {
	*httptest.Server
	mu   sync.Mutex
	got  []gotRequest
	hold chan struct{}
}

type gotRequest struct {
	method, path, query, contentType, auth string
	body                                   string
	header                                 http.Header
}

func newEchoServer(t *testing.T) *echoServer {
	t.Helper()
	s := &echoServer{hold: make(chan struct{})}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.got = append(s.got, gotRequest{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Content-Type"), r.Header.Get("Authorization"), string(body), r.Header.Clone()})
		s.mu.Unlock()
		if r.URL.Path == "/slow" {
			select {
			case <-s.hold:
			case <-r.Context().Done():
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *echoServer) requests() []gotRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]gotRequest(nil), s.got...)
}

// useServer makes {{baseUrl}} of the Dev environment the address of the server.
func (r *rig) useServer(s *echoServer) {
	r.t.Helper()
	r.run(CmdSelectEnvironment, "dev")
	if err := r.ext.values.Set(context.Background(), variables.EnvironmentOwner("dev"), "baseUrl", s.URL, false); err != nil {
		r.t.Fatal(err)
	}
}

func TestSendGoesThroughTheEngine(t *testing.T) {
	srv := newEchoServer(t)
	r := newRig(t)
	r.useServer(srv)
	tab := r.openReq(pCreate)
	// what is in the boxes is sent, not what is on disk
	tab.d.texts[format.BodyJSON].SetValue(`{"name": "Grace", "token": "{{baseUrl}}"}`)
	tab.d.headers = append(tab.d.headers, newPairBox(format.Pair{Name: "X-Trace", Value: "abc", Enabled: true}))
	tab.d.auth.typ = format.AuthBearer
	tab.d.auth.box[fToken].SetValue("secret-token")
	tab.sendRequest()
	if !tab.send.sending || !r.fakeBusy(tab) {
		t.Error("the tab is not busy while it sends")
	}
	r.settle()
	got := srv.requests()
	if len(got) != 1 {
		t.Fatalf("the server got %d requests", len(got))
	}
	g := got[0]
	if g.method != "POST" || g.path != "/v1/users" || g.contentType != "application/json" || g.auth != "Bearer secret-token" || g.header.Get("X-Trace") != "abc" {
		t.Errorf("request = %+v", g)
	}
	if want := `{"name": "Grace", "token": "` + srv.URL + `"}`; g.body != want {
		t.Errorf("body = %q, want %q", g.body, want)
	}
	if tab.send.sending || r.fakeBusy(tab) {
		t.Error("the tab is busy after the response")
	}
	if !strings.HasPrefix(tab.send.status, "201 Created") || tab.send.bad {
		t.Errorf("status = %q bad %v", tab.send.status, tab.send.bad)
	}
	// the history has it, as written (the variables not replaced, the secret masked)
	list := r.ext.hist.List()
	if len(list) != 1 || list[0].Status != 201 || list[0].Method != "POST" || !strings.Contains(list[0].URL, "{{baseUrl}}") {
		t.Fatalf("history = %+v", list)
	}
	if list[0].Request.Auth == nil || list[0].Request.Auth.Token != "********" {
		t.Errorf("the history keeps the token: %+v", list[0].Request.Auth)
	}
	// the files on disk are not touched by a send
	if strings.Contains(r.read(r.col(pCreate)), "Grace") {
		t.Error("a send wrote the request")
	}
}

// fakeBusy says whether the fake tab shows the busy mark.
func (r *rig) fakeBusy(t *reqTab) bool {
	for _, tab := range r.api.EditorTabs() {
		if tab.ID() == t.tab.ID() {
			return tab.Busy()
		}
	}
	return false
}

func TestSendWithAnErrorAndWithoutAnEnvironment(t *testing.T) {
	r := newRig(t)
	tab := r.openReq(pCreate)
	// no environment: baseUrl has no value, so the URL has no host
	tab.sendRequest()
	r.settle()
	if !tab.send.bad || tab.send.status == "" {
		t.Errorf("status = %q bad %v", tab.send.status, tab.send.bad)
	}
	if len(r.ext.hist.List()) != 1 || r.ext.hist.List()[0].Error == "" {
		t.Errorf("history = %+v", r.ext.hist.List())
	}
	// a variable without a value is told about, the request still goes
	srv := newEchoServer(t)
	tab.d.url.SetValue(srv.URL + "/x/{{missing}}")
	tab.sendRequest()
	r.settle()
	if !strings.Contains(tab.send.status, "no value for missing") {
		t.Errorf("status = %q", tab.send.status)
	}
}

func TestCancelStopsTheRequest(t *testing.T) {
	srv := newEchoServer(t)
	r := newRig(t)
	tab := r.openReq(pCreate)
	tab.d.url.SetValue(srv.URL + "/slow")
	tab.sendRequest()
	r.api.WaitFor("the server to get the request", func() bool { return len(srv.requests()) == 1 })
	r.run(CmdCancel, nil)
	if tab.send.sending || tab.send.status != "Canceled." {
		t.Errorf("sending %v status %q", tab.send.sending, tab.send.status)
	}
	close(srv.hold)
}

func TestSaveWritesTheFilesAndKeepsComments(t *testing.T) {
	const withComments = `// Creates a user.
{
  "emetosHttp": 1,
  "name": "Create user", // shown in the tab
  "method": "POST",
  "url": "{{baseUrl}}/v1/users",
  "x-note": "kept",
  "body": { "type": "json", "file": "create-user.body.json" }
}
`
	r := newRig(t, func(folder string) {
		writeFile(t, folder+"/.emetos/http/collections/shop-api/users/create-user.request.jsonc", withComments)
	})
	tab := r.openReq(pCreate)
	tab.d.url.SetValue("{{baseUrl}}/v2/users?notify=true")
	tab.setMethod("put")
	tab.d.texts[format.BodyJSON].SetValue("{\n  \"name\": \"Grace\"\n}\n")
	tab.d.auth.typ = format.AuthBearer
	tab.d.auth.box[fToken].SetValue("{{token}}")
	tab.d.timeout.SetValue("5000")
	if !tab.dirty() {
		t.Fatal("the tab is not dirty")
	}
	var saved []bool
	tab.save(func(ok bool) { saved = append(saved, ok) })
	r.settle()
	if len(saved) != 1 || !saved[0] {
		t.Fatalf("saved = %v, notes %v", saved, tab.note)
	}
	text := r.read(r.col(pCreate))
	for _, want := range []string{"// Creates a user.", "// shown in the tab", `"x-note": "kept"`, `"method": "PUT"`, `"url": "{{baseUrl}}/v2/users"`,
		`"name": "notify"`, `"token": "{{token}}"`, `"timeoutMs": 5000`} {
		if !strings.Contains(text, want) {
			t.Errorf("the file lacks %q:\n%s", want, text)
		}
	}
	if body := r.read(r.col("shop-api/users/create-user.body.json")); body != "{\n  \"name\": \"Grace\"\n}\n" {
		t.Errorf("body file = %q", body)
	}
	if tab.dirty() || r.fakeTabOf(tab).Dirty() {
		t.Error("the tab is dirty after a save")
	}
	// the tree has the new request, and the tab has not been replaced under the person
	r.api.Pump()
	if n := r.node(r.col(pCreate)); n.Request.Method != "PUT" {
		t.Errorf("the tree has %q", n.Request.Method)
	}
	// changing the kind of the body renames and removes the body file
	tab.d.bodyType = format.BodyXML
	tab.d.texts[format.BodyXML].SetValue("<a/>")
	tab.save(nil)
	r.settle()
	if r.has(r.col("shop-api/users/create-user.body.json")) || !r.has(r.col("shop-api/users/create-user.body.xml")) {
		t.Error("the body file did not follow the kind")
	}
}

func TestACleanTabFollowsTheFileAndADirtyOneKeepsItsEdits(t *testing.T) {
	r := newRig(t)
	tab := r.openReq(pCreate)
	p := r.col(pCreate)
	writeFile(t, p, `{"emetosHttp": 1, "name": "Create user", "method": "PATCH", "url": "http://h/changed"}`)
	r.ext.refresh()
	r.settle()
	r.api.WaitFor("the tab to follow the file", func() bool { r.api.Pump(); return tab.d.method == "PATCH" })
	if tab.d.url.Value() != "http://h/changed" || tab.dirty() {
		t.Errorf("url %q dirty %v", tab.d.url.Value(), tab.dirty())
	}
	tab.d.url.SetValue("http://h/mine")
	writeFile(t, p, `{"emetosHttp": 1, "name": "Create user", "method": "DELETE", "url": "http://h/theirs"}`)
	r.ext.refresh()
	r.settle()
	r.api.WaitFor("the tab to notice the file", func() bool { r.api.Pump(); return tab.diskChanged })
	if tab.d.url.Value() != "http://h/mine" || tab.d.method != "PATCH" {
		t.Errorf("the edits were replaced: %q %q", tab.d.url.Value(), tab.d.method)
	}
	// a tab whose file is deleted says so
	if err := removeFile(p); err != nil {
		t.Fatal(err)
	}
	r.ext.refresh()
	r.settle()
	r.api.WaitFor("the tab to see the delete", func() bool { r.api.Pump(); return tab.gone })
}

func TestTheTabComesBackAfterARestart(t *testing.T) {
	r := newRig(t)
	tab := r.openReq(pCreate)
	st, _ := tab.saveState()
	if s := st.(reqTabState); s.Draft != nil || s.Path != r.col(pCreate) {
		t.Errorf("a clean tab keeps %+v", s)
	}
	tab.d.url.SetValue("http://h/unsaved")
	raw := r.api.EditorTabs()[0].State()
	tab.onClose()
	back := r.api.RestoreTab(RequestKindID, "again", raw)
	_ = back
	r.api.WaitFor("the tab to come back", func() bool {
		r.api.Pump()
		n := len(r.ext.reqTabs)
		return n > 0 && r.ext.reqTabs[n-1].d != nil && r.ext.reqTabs[n-1].pending == nil
	})
	r.settle()
	again := r.ext.reqTabs[len(r.ext.reqTabs)-1]
	if again.d.url.Value() != "http://h/unsaved" || !again.dirty() {
		t.Errorf("url %q dirty %v", again.d.url.Value(), again.dirty())
	}
	if !r.fakeTabOf(again).Dirty() {
		t.Error("the restored tab is not marked dirty")
	}
}

func TestTheCloseQuestionSavesOrDropsTheEdits(t *testing.T) {
	r := newRig(t)
	tab := r.openReq(pCreate)
	ft := r.api.EditorTabs()[0]
	if ft.Content.Save == nil {
		t.Fatal("the tab has no Save, so closing it would not ask")
	}
	tab.d.url.SetValue("http://h/asked")
	if !ft.Dirty() {
		t.Fatal("not dirty")
	}
	// Save, as the dialog's Save button calls it
	var result []bool
	ft.Content.Save(func(ok bool) { result = append(result, ok) })
	r.settle()
	if len(result) != 1 || !result[0] || ft.Dirty() {
		t.Fatalf("result %v dirty %v", result, ft.Dirty())
	}
	if !strings.Contains(r.read(r.col(pCreate)), "http://h/asked") {
		t.Error("Save did not write the file")
	}
	// Save that fails (the file is read-only now) keeps the tab dirty and says false
	tab.d.url.SetValue("http://h/again")
	r.ext.store.Close()
	result = nil
	_ = removeAll(r.col("shop-api"))
	ft.Content.Save(func(ok bool) { result = append(result, ok) })
	r.settle()
	if len(result) != 1 || result[0] {
		t.Errorf("a failed save said %v", result)
	}
	if !ft.Dirty() {
		t.Error("a failed save cleared the dirty mark")
	}
}

func TestAnUnsavedRequestAsksForItsPlace(t *testing.T) {
	r := newRig(t)
	s := sentAt(time.Hour, "GET", "https://h/things", 200)
	r.record(s)
	r.run(CmdOpenHistory, map[string]any{"seq": 0})
	tab := r.ext.reqTabs[0]
	var result []bool
	tab.save(func(ok bool) { result = append(result, ok) })
	if r.api.LastPick() == nil {
		t.Fatal("no list of collections")
	}
	r.choose("Payments")
	r.answer("Things")
	if len(result) != 1 || !result[0] || !strings.HasSuffix(tab.path, "things.request.jsonc") {
		t.Fatalf("result %v path %q", result, tab.path)
	}
	if !strings.Contains(r.read(tab.path), "https://h/things") {
		t.Error("the file lacks the URL")
	}
	// cancelling the question tells the caller
	r.record(s)
	r.run(CmdOpenHistory, map[string]any{"seq": 1})
	other := r.ext.reqTabs[1]
	result = nil
	other.save(func(ok bool) { result = append(result, ok) })
	r.api.LastPick().Close()
	r.settle()
	if len(result) != 1 || result[0] {
		t.Errorf("a cancelled pick said %v", result)
	}
}
