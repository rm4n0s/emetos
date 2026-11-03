package httpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/rundata"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

// The collection `flow` of these tests logs in, reads the profile with the token the login gave, and logs out; a folder `admin` has one
// more request.
const (
	flowCollection = `{"emetosHttp": 1, "name": "Flow", "order": ["login.request.jsonc", "profile.request.jsonc", "logout.request.jsonc", "admin"],
  "variables": [{"name": "user", "value": "nobody"}]}`
	flowLogin = `{"emetosHttp": 1, "name": "Login", "method": "POST", "url": "{{baseUrl}}/login",
  "headers": [{"name": "X-User", "value": "{{user}}"}],
  "extract": [{"from": "body", "path": "$.token", "to": "token"}],
  "checks": [{"subject": "status", "op": "equals", "value": 200}]}`
	flowProfile = `{"emetosHttp": 1, "name": "Profile", "method": "GET", "url": "{{baseUrl}}/profile/{{token}}",
  "headers": [{"name": "X-User", "value": "{{user}}"}],
  "checks": [{"subject": "status", "op": "equals", "value": 200}, {"subject": "json", "path": "$.name", "op": "exists"}]}`
	flowLogout = `{"emetosHttp": 1, "name": "Logout", "method": "DELETE", "url": "{{baseUrl}}/logout/{{token}}",
  "headers": [{"name": "X-User", "value": "{{user}}"}]}`
	flowAudit = `{"emetosHttp": 1, "name": "Audit", "method": "GET", "url": "{{baseUrl}}/audit", "headers": [{"name": "X-User", "value": "{{user}}"}]}`
)

// flowCall is a request the flow server got.
type flowCall struct{ method, path, user string }

// flowServer is the server the runner tests send to: /login gives a token for the X-User header, /profile/<token> the name behind it.
type flowServer struct {
	*httptest.Server
	mu    sync.Mutex
	calls []flowCall
	// hold, when set, makes the request for its path wait until the test lets it go (or the client leaves); reached is told it arrived.
	hold    string
	release chan struct{}
	reached chan struct{}
}

func newFlowServer(t *testing.T) *flowServer {
	t.Helper()
	s := &flowServer{release: make(chan struct{}), reached: make(chan struct{}, 16)}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("X-User")
		s.mu.Lock()
		s.calls = append(s.calls, flowCall{r.Method, r.URL.Path, user})
		hold := s.hold != "" && strings.HasPrefix(r.URL.Path, s.hold)
		s.mu.Unlock()
		if hold {
			s.reached <- struct{}{}
			select {
			case <-s.release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/login":
			_, _ = w.Write([]byte(`{"token":"tok-` + strings.ReplaceAll(user, " ", "_") + `"}`))
		case strings.HasPrefix(r.URL.Path, "/profile/"):
			if r.URL.Path != "/profile/tok-"+strings.ReplaceAll(user, " ", "_") {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"wrong token"}`))
				return
			}
			_, _ = w.Write([]byte(`{"name":"` + user + `"}`))
		case strings.HasPrefix(r.URL.Path, "/logout/"):
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *flowServer) got() []flowCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]flowCall(nil), s.calls...)
}

func (s *flowServer) paths() []string {
	var out []string
	for _, c := range s.got() {
		out = append(out, c.path)
	}
	return out
}

// flowRig activates the extension over a project with the flow collection and Dev pointed at the server; requests are replaced by
// replace (a file name of the collection to its text).
func flowRig(t *testing.T, srv *flowServer, replace map[string]string) *rig {
	t.Helper()
	files := map[string]string{
		"collection.jsonc": flowCollection, "login.request.jsonc": flowLogin, "profile.request.jsonc": flowProfile,
		"logout.request.jsonc": flowLogout, "admin/audit.request.jsonc": flowAudit,
		"admin/folder.jsonc": `{"emetosHttp": 1, "name": "Admin"}`,
	}
	for k, v := range replace {
		files[k] = v
	}
	r := newRig(t, func(folder string) {
		for name, text := range files {
			if text != "" {
				writeFile(t, filepath.Join(folder, ".emetos", "http", "collections", "flow", filepath.FromSlash(name)), text)
			}
		}
	})
	r.useBase(srv.URL)
	return r
}

// openRunner opens the runner of a collection or folder of the project.
func (r *rig) openRunner(rel string) *runTab {
	r.t.Helper()
	r.run(CmdRunCollection, r.col(rel))
	return r.ext.runTabs[len(r.ext.runTabs)-1]
}

// runAndWait presses Run and waits until the run ended.
func (r *rig) runAndWait(t *runTab) {
	r.t.Helper()
	t.start()
	r.api.WaitFor("the run to end", func() bool { return !t.running && r.ext.pending.Load() == 0 })
	r.settle()
}

func names(results []runResult) []string {
	var out []string
	for _, x := range results {
		out = append(out, x.Name)
	}
	return out
}

func TestTheRunnerCarriesValuesFromOneRequestToTheNext(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow")
	if got := len(tab.requests()); got != 4 {
		t.Fatalf("the tab lists %d requests", got)
	}
	r.runAndWait(tab)
	if got := strings.Join(srv.paths(), " "); got != "/login /profile/tok-nobody /logout/tok-nobody /audit" {
		t.Errorf("the server got %s", got)
	}
	if got := strings.Join(names(tab.results), ","); got != "Login,Profile,Logout,Admin / Audit" {
		t.Errorf("results = %s", got)
	}
	for i, want := range []int{200, 200, 204, 200} {
		if tab.results[i].Status != want || tab.results[i].failed() {
			t.Errorf("result %d = %+v", i, tab.results[i])
		}
	}
	if x := tab.results[0].Extracted; len(x) != 1 || x[0].Value != "tok-nobody" || !x[0].Stored {
		t.Errorf("extracted = %+v", x)
	}
	if tab.end != runCompleted || tab.total != 1 || tab.running {
		t.Errorf("end %v total %d running %v", tab.end, tab.total, tab.running)
	}
	if got := r.ext.values.All(variables.EnvironmentOwner("dev"))["token"]; got != "tok-nobody" {
		t.Errorf("the current value of token = %q", got)
	}
	// the sends of a run are not in the history (decision 1862)
	if n := len(r.ext.hist.List()); n != 0 {
		t.Errorf("the history has %d entries", n)
	}
	// and no file of the project changed
	if strings.Contains(r.read(r.col("flow/login.request.jsonc")), "tok-") {
		t.Error("a run wrote a request")
	}
	if r.fakeRunnerBusy(tab) {
		t.Error("the tab is busy after the run")
	}
}

func TestARunOfAFolderRunsItsRequestsOnly(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow/admin")
	r.runAndWait(tab)
	if got := strings.Join(srv.paths(), " "); got != "/audit" {
		t.Errorf("the server got %s", got)
	}
}

func TestOnlyTheCheckedRequestsRun(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow")
	tab.toggle(r.col("flow/login.request.jsonc"), false)
	tab.toggle(r.col("flow/admin/audit.request.jsonc"), false)
	// without the login there is no token: the profile asks for /profile/{{token}}
	r.runAndWait(tab)
	if got := strings.Join(srv.paths(), " "); got != "/profile/{{token}} /logout/{{token}}" && got != "/profile/%7B%7Btoken%7D%7D /logout/%7B%7Btoken%7D%7D" {
		t.Errorf("the server got %s", got)
	}
	if len(tab.results) != 2 || len(tab.results[0].Notes) == 0 || !strings.Contains(tab.results[0].Notes[0], "token") {
		t.Errorf("results = %+v", tab.results)
	}
	tab.setAll(false)
	tab.start()
	if tab.running || !strings.Contains(tab.note.Text, "at least one") {
		t.Errorf("running %v, note %q", tab.running, tab.note.Text)
	}
	tab.setAll(true)
	if got := len(tab.selected()); got != 4 {
		t.Errorf("%d are checked", got)
	}
}

func TestTheRunnerUsesItsOwnEnvironment(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow")
	if tab.envKey != "dev" {
		t.Fatalf("the runner starts with %q, the active environment is dev", tab.envKey)
	}
	// no environment: {{baseUrl}} has no value, so nothing can be sent
	tab.envKey = ""
	r.runAndWait(tab)
	if len(srv.got()) != 0 || len(tab.results) != 4 {
		t.Fatalf("server %d, results %d", len(srv.got()), len(tab.results))
	}
	for _, x := range tab.results {
		if x.Err == "" || !x.failed() {
			t.Errorf("result %+v", x)
		}
	}
	if tab.envName != "" || totalsOf(tab.results).Errors != 4 {
		t.Errorf("env %q totals %+v", tab.envName, totalsOf(tab.results))
	}
	// the other environment is not the active one, and it is the one that receives the extracted values
	tab.envKey = "staging"
	if err := r.ext.values.Set(context.Background(), variables.EnvironmentOwner("staging"), "baseUrl", srv.URL, false); err != nil {
		t.Fatal(err)
	}
	r.runAndWait(tab)
	if got := r.ext.values.All(variables.EnvironmentOwner("staging"))["token"]; got != "tok-nobody" {
		t.Errorf("staging token = %q", got)
	}
	if got := r.ext.values.All(variables.EnvironmentOwner("dev"))["token"]; got != "" {
		t.Errorf("dev token = %q", got)
	}
	if r.active() != "dev" {
		t.Errorf("the active environment changed to %q", r.active())
	}
}

// Iterations with data files: a CSV with a BOM, quotes and commas, a JSON array of objects, and the iterations box.
func TestIterationsFollowTheRowsOfADataFile(t *testing.T) {
	for _, tc := range []struct {
		name, file, text string
		want             []string
	}{
		{"csv with a byte order mark, quotes and commas", "users.csv", "\xef\xbb\xbfuser,role\r\n\"Ada, Countess\",admin\r\nGrace,\"dev \"\"ops\"\"\"\r\n", []string{"Ada, Countess", "Grace"}},
		{"json array of objects", "users.json", `[{"user": "Linus", "n": 1}, {"user": "Margaret", "n": 2}, {"user": "Ken"}]`, []string{"Linus", "Margaret", "Ken"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFlowServer(t)
			r := flowRig(t, srv, nil)
			writeFile(t, filepath.Join(r.folder, "data", tc.file), tc.text)
			tab := r.openRunner("flow/admin")
			tab.iterations.SetValue("7") // ignored: one iteration for each row
			tab.data.SetValue("data/" + tc.file)
			tab.dataEdited()
			tab.loadData()
			r.settle()
			if tab.table == nil || len(tab.table.Rows) != len(tc.want) {
				t.Fatalf("table = %+v, error %q", tab.table, tab.dataErr)
			}
			r.runAndWait(tab)
			var users []string
			for _, c := range srv.got() {
				users = append(users, c.user)
			}
			if strings.Join(users, "|") != strings.Join(tc.want, "|") {
				t.Errorf("users = %q, want %q", users, tc.want)
			}
			if tab.total != len(tc.want) || len(tab.results) != len(tc.want) || tab.end != runCompleted {
				t.Errorf("total %d results %d end %v", tab.total, len(tab.results), tab.end)
			}
			for i, x := range tab.results {
				if x.Iteration != i+1 {
					t.Errorf("result %d is of iteration %d", i, x.Iteration)
				}
			}
		})
	}
}

func TestADataFileRowWinsOverTheOtherVariablesAndTheFlowStillCarriesValues(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	// the environment, the collection and a global all define `user`; the row wins
	if err := r.ext.values.Set(context.Background(), variables.EnvironmentOwner("dev"), "user", "from-environment", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(r.folder, "rows.csv"), "user\nAda\nGrace\n")
	tab := r.openRunner("flow")
	tab.skip[r.col("flow/admin/audit.request.jsonc")] = true
	tab.data.SetValue("rows.csv")
	r.runAndWait(tab)
	want := "/login /profile/tok-Ada /logout/tok-Ada /login /profile/tok-Grace /logout/tok-Grace"
	if got := strings.Join(srv.paths(), " "); got != want {
		t.Errorf("paths = %s", got)
	}
	for _, c := range srv.got() {
		if c.user == "from-environment" || c.user == "nobody" {
			t.Errorf("call %+v used another value of user", c)
		}
	}
	if tot := totalsOf(tab.results); tot.Failed != 0 || tot.Passed != 6 || tot.Requests != 6 {
		t.Errorf("totals = %+v", tot)
	}
}

func TestABadDataFileStopsTheRunBeforeAnyRequest(t *testing.T) {
	for _, tc := range []struct {
		name, file, text, route string
	}{
		{"csv with a short row", "bad.csv", "user,role\nAda\n", "HttpRunDataCSVInvalid"},
		{"json that is not an array", "bad.json", `{"user": "Ada"}`, "HttpRunDataJSONShape"},
		{"a format that is not read", "rows.txt", "user\nAda\n", "HttpRunDataFormat"},
		{"a file that is not there", "missing.csv", "", "HttpRunDataRead"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFlowServer(t)
			r := flowRig(t, srv, nil)
			if tc.text != "" {
				writeFile(t, filepath.Join(r.folder, tc.file), tc.text)
			}
			tab := r.openRunner("flow")
			tab.data.SetValue(tc.file)
			r.runAndWait(tab)
			if len(srv.got()) != 0 || len(tab.results) != 0 || tab.end != runNotStarted {
				t.Fatalf("server %d, results %d, end %v", len(srv.got()), len(tab.results), tab.end)
			}
			if !tab.note.Bad || !strings.Contains(tab.note.Text, "did not start") || tab.dataErr == "" {
				t.Errorf("note %+v, data error %q", tab.note, tab.dataErr)
			}
			// the error of the run is the one of the data package, with its route
			_, err := rundata.Load(context.Background(), filepath.Join(r.folder, tc.file))
			if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute(tc.route) {
				t.Errorf("error = %v", err)
			}
		})
	}
}

func TestStopOnTheFirstFailure(t *testing.T) {
	failing := strings.Replace(flowProfile, `"value": 200`, `"value": 201`, 1)
	for _, tc := range []struct {
		name  string
		stop  bool
		paths string
		end   runEnd
	}{
		{"stops", true, "/login /profile/tok-nobody", runStopped},
		{"goes on", false, "/login /profile/tok-nobody /logout/tok-nobody /audit", runCompleted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newFlowServer(t)
			r := flowRig(t, srv, map[string]string{"profile.request.jsonc": failing})
			tab := r.openRunner("flow")
			tab.stop = tc.stop
			r.runAndWait(tab)
			if got := strings.Join(srv.paths(), " "); got != tc.paths {
				t.Errorf("paths = %s", got)
			}
			if tab.end != tc.end {
				t.Errorf("end = %v", tab.end)
			}
			if !tab.results[1].failed() || tab.results[1].Checks[0].Passed {
				t.Errorf("the profile did not fail: %+v", tab.results[1])
			}
		})
	}
}

func TestAStatusOfErrorIsNotAFailureUnlessACheckSaysSo(t *testing.T) {
	srv := newFlowServer(t)
	// the token is wrong, so the profile gets 403; it has a check on the status, so it fails; the logout has none
	r := flowRig(t, srv, map[string]string{"profile.request.jsonc": strings.Replace(flowProfile, "/profile/{{token}}", "/profile/wrong", 1)})
	tab := r.openRunner("flow")
	tab.stop = true
	tab.skip[r.col("flow/admin/audit.request.jsonc")] = true
	r.runAndWait(tab)
	if tab.end != runStopped || len(tab.results) != 2 || tab.results[1].Status != 403 {
		t.Fatalf("end %v results %+v", tab.end, tab.results)
	}
	if (runResult{Status: 500}).failed() {
		t.Error("a status of 500 with no check counts as a failure")
	}
	if !(runResult{Err: "boom"}).failed() || (runResult{Err: "Canceled.", Canceled: true}).failed() {
		t.Error("an error fails, a cancellation does not")
	}
}

func TestCancelStopsTheRunAndNoRequestIsSentAfterwards(t *testing.T) {
	srv := newFlowServer(t)
	srv.hold = "/profile/"
	r := flowRig(t, srv, nil)
	writeFile(t, filepath.Join(r.folder, "rows.csv"), "user\nAda\nGrace\n")
	tab := r.openRunner("flow")
	tab.data.SetValue("rows.csv")
	tab.start()
	if !tab.running || !r.fakeRunnerBusy(tab) {
		t.Fatal("the tab is not running")
	}
	select {
	case <-srv.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("the profile never arrived")
	}
	tab.cancelRun()
	r.api.WaitFor("the run to end", func() bool { return !tab.running && r.ext.pending.Load() == 0 })
	r.settle()
	close(srv.release)
	if got := strings.Join(srv.paths(), " "); got != "/login /profile/tok-Ada" {
		t.Errorf("the server got %s", got)
	}
	if tab.end != runCanceled || len(tab.results) != 2 {
		t.Fatalf("end %v results %+v", tab.end, tab.results)
	}
	last := tab.results[1]
	if !last.Canceled || last.failed() || last.Iteration != 1 {
		t.Errorf("last = %+v", last)
	}
	if tot := totalsOf(tab.results); tot.Errors != 0 {
		t.Errorf("a cancellation counts as an error: %+v", tot)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(srv.got()); n != 2 {
		t.Errorf("the server got %d requests", n)
	}
	// cancel with nothing running does nothing
	tab.cancelRun()
}

func TestCancelEndsTheWaitBetweenTwoRequests(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow")
	tab.delay.SetValue("60000")
	tab.start()
	r.api.WaitFor("the first result", func() bool { return len(tab.results) == 1 })
	began := time.Now()
	tab.cancelRun()
	r.api.WaitFor("the run to end", func() bool { return !tab.running && r.ext.pending.Load() == 0 })
	if time.Since(began) > 5*time.Second || tab.end != runCanceled || len(srv.got()) != 1 {
		t.Errorf("took %v, end %v, %d requests", time.Since(began), tab.end, len(srv.got()))
	}
}

func TestTheDelayIsWaitedBetweenRequests(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow")
	tab.skip[r.col("flow/admin/audit.request.jsonc")] = true
	tab.delay.SetValue("80")
	began := time.Now()
	r.runAndWait(tab)
	if took := time.Since(began); took < 160*time.Millisecond {
		t.Errorf("three requests with a delay of 80 ms took %v", took)
	}
	if len(tab.results) != 3 {
		t.Errorf("%d results", len(tab.results))
	}
}

func TestTheBoxesOfTheRunnerAreChecked(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow")
	for _, tc := range []struct{ iterations, delay, want string }{
		{"0", "0", "Iterations"}, {"x", "0", "Iterations"}, {"10001", "0", "Iterations"}, {"2", "-1", "The delay"}, {"2", "1.5", "The delay"},
	} {
		tab.iterations.SetValue(tc.iterations)
		tab.delay.SetValue(tc.delay)
		tab.start()
		if tab.running || !tab.note.Bad || !strings.HasPrefix(tab.note.Text, tc.want) {
			t.Errorf("%q %q: running %v, note %+v", tc.iterations, tc.delay, tab.running, tab.note)
		}
	}
	if len(srv.got()) != 0 {
		t.Error("a request was sent")
	}
}

func TestAnUnreadableRequestIsListedButNotRun(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, map[string]string{"logout.request.jsonc": `{"emetosHttp": 1, "name": "Logout", "method": `})
	tab := r.openRunner("flow")
	var broken int
	for _, q := range tab.requests() {
		if !runnable(q.node) {
			broken++
			if problemOf(q.node) == "" {
				t.Error("no reason for the request that cannot run")
			}
		}
	}
	if broken != 1 || len(tab.selected()) != 3 {
		t.Fatalf("%d cannot run, %d selected", broken, len(tab.selected()))
	}
	r.runAndWait(tab)
	if got := strings.Join(srv.paths(), " "); got != "/login /profile/tok-nobody /audit" {
		t.Errorf("paths = %s", got)
	}
}

func TestTheRunnerKeepsItsChoicesInTheStateOfTheWindow(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow")
	tab.envKey = "staging"
	tab.iterations.SetValue("3")
	tab.delay.SetValue("25")
	tab.stop = true
	tab.data.SetValue("rows.csv")
	tab.toggle(r.col("flow/logout.request.jsonc"), false)
	raw, err := tab.saveState()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var st runTabState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if st.Path != r.col("flow") || st.Env == nil || *st.Env != "staging" || st.Iterations != 3 || st.DelayMs != 25 || !st.Stop || st.Data != "rows.csv" ||
		len(st.Skip) != 1 || st.Skip[0] != "logout.request.jsonc" {
		t.Errorf("state = %s", data)
	}
	// a tab that comes back from that state
	for _, x := range append([]*runTab(nil), r.ext.runTabs...) {
		x.onClose()
	}
	back, err := r.ext.createRunnerTab(tab.tab, data)
	if err != nil {
		t.Fatal(err)
	}
	if back.SaveState == nil {
		t.Fatal("no state saved")
	}
	again := r.ext.runTabs[len(r.ext.runTabs)-1]
	if again.envKey != "staging" || again.iterations.Value() != "3" || again.delay.Value() != "25" || !again.stop || !again.skip[r.col("flow/logout.request.jsonc")] {
		t.Errorf("restored = %+v", again)
	}
	// results are not part of it
	if len(again.results) != 0 {
		t.Error("results came back")
	}
	for _, bad := range []string{``, `{}`, `{"path": 7}`} {
		_, err := r.ext.createRunnerTab(tab.tab, json.RawMessage(bad))
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("%q: %v", bad, err)
		}
		want := "HttpRunnerTabStateMissing"
		if bad == `{"path": 7}` {
			want = "HttpRunnerTabStateInvalid"
		}
		if !appErr.HasRoute(want) {
			t.Errorf("%q: %s", bad, appErr.Route())
		}
	}
}

func TestTheRunCollectionCommandOpensOneRunnerForEachGroup(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	r.run(CmdRunCollection, r.col("flow"))
	r.run(CmdRunCollection, map[string]any{"path": r.col("flow")})
	if len(r.ext.runTabs) != 1 {
		t.Fatalf("%d runner tabs", len(r.ext.runTabs))
	}
	r.run(CmdRunCollection, r.col("flow/admin"))
	if len(r.ext.runTabs) != 2 {
		t.Fatalf("%d runner tabs", len(r.ext.runTabs))
	}
	if got := r.fakeRunnerTitle(r.ext.runTabs[1]); got != "Run Admin" {
		t.Errorf("title = %q", got)
	}
	// a request is not a group
	r.run(CmdRunCollection, r.col("flow/login.request.jsonc"))
	if len(r.ext.runTabs) != 2 || len(r.notes()) == 0 {
		t.Errorf("tabs %d, notes %v", len(r.ext.runTabs), r.notes())
	}
	// closing the tab drops it
	r.ext.runTabs[0].onClose()
	if len(r.ext.runTabs) != 1 {
		t.Errorf("%d runner tabs", len(r.ext.runTabs))
	}
}

func TestClosingTheTabInTheMiddleOfARunCancelsIt(t *testing.T) {
	srv := newFlowServer(t)
	srv.hold = "/profile/"
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow")
	tab.start()
	select {
	case <-srv.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("the profile never arrived")
	}
	tab.onClose()
	r.api.WaitFor("the run to end", func() bool { return r.ext.pending.Load() == 0 })
	if got := strings.Join(srv.paths(), " "); got != "/login /profile/tok-nobody" {
		t.Errorf("the server got %s", got)
	}
}

func TestSaveResultsWritesAJSONReport(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, map[string]string{"profile.request.jsonc": strings.Replace(flowProfile, `"path": "$.name", "op": "exists"`, `"path": "$.name", "op": "equals", "value": "someone else"`, 1)})
	writeFile(t, filepath.Join(r.folder, "rows.json"), `[{"user": "Ada", "password": "s3cret"}, {"user": "Grace", "password": "hunter2"}]`)
	tab := r.openRunner("flow")
	tab.skip[r.col("flow/admin/audit.request.jsonc")] = true
	tab.data.SetValue("rows.json")
	r.runAndWait(tab)
	to := filepath.Join(t.TempDir(), "out.json")
	r.api.AnswerFileChooser(to, nil)
	tab.saveResults()
	r.settle()
	calls := r.api.FileChooserCalls()
	if len(calls) != 1 || !calls[0].Options.Save || calls[0].Options.Name != "Flow-results.json" {
		t.Fatalf("calls = %+v", calls)
	}
	var rep reportFile
	data, err := os.ReadFile(to)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if rep.Version != 1 || rep.Name != "Flow" || rep.Environment != "Dev" || rep.Result != "completed" || rep.Iterations != 2 || rep.DataRows != 2 {
		t.Errorf("report = %+v", rep)
	}
	if rep.Totals.Requests != 6 || rep.Totals.ChecksPassed != 4 || rep.Totals.ChecksFailed != 2 || rep.Totals.Errors != 0 {
		t.Errorf("totals = %+v", rep.Totals)
	}
	if len(rep.Results) != 6 || rep.Results[0].Request != "Login" || rep.Results[0].Iteration != 1 || rep.Results[3].Iteration != 2 || rep.Results[1].Status != 200 {
		t.Fatalf("results = %+v", rep.Results)
	}
	if x := rep.Results[0].Extracted; len(x) != 1 || x[0].Variable != "token" || x[0].Value != "tok-Ada" || !x[0].Stored {
		t.Errorf("extracted = %+v", x)
	}
	if c := rep.Results[1].Checks; len(c) != 2 || !c[0].Passed || c[1].Passed || c[1].Actual != "Ada" {
		t.Errorf("checks = %+v", c)
	}
	// the values of the data file are not in the report
	for _, secret := range []string{"s3cret", "hunter2"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("the report holds %q", secret)
		}
	}
	// a dialog that is canceled writes nothing, one that fails says so
	r.api.AnswerFileChooser("", nil)
	tab.saveResults()
	r.settle()
	before := len(r.notes())
	r.api.AnswerFileChooser(filepath.Join(t.TempDir(), "missing", "out.json"), nil)
	tab.saveResults()
	r.settle()
	if notes := r.notes(); len(notes) != before+1 || !strings.HasPrefix(notes[len(notes)-1], "error: Cannot save the results") {
		t.Errorf("notes = %q", notes)
	}
	// nothing to save while a run is going or before one
	empty := r.openRunner("flow/admin")
	n := len(r.api.FileChooserCalls())
	empty.saveResults()
	if len(r.api.FileChooserCalls()) != n {
		t.Error("the dialog was asked for with no results")
	}
}

func TestTheSecretsOfARunAreMaskedInTheReport(t *testing.T) {
	out := t.TempDir()
	rep := reportOf("Flow", "", runStopped, 1, 0, []runResult{
		{Iteration: 1, Name: "Login", Method: "GET", URL: "http://x/y?api_key=********", Status: 200, Extracted: []extractOutcome{{Secret: true, Stored: true}}},
	})
	if rep.Result != "stopped" {
		t.Errorf("result = %q", rep.Result)
	}
	if rep.Results[0].Extracted[0].Value != "********" {
		t.Errorf("a secret is not masked in the report: %+v", rep.Results[0].Extracted[0])
	}
	// the write fails with its own tag
	err := writeReport(context.Background(), rep, filepath.Join(out, "no", "such", "dir", "r.json"))
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("writeReport.HttpRunReportWriteFailed") {
		t.Errorf("error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err = writeReport(canceled, rep, filepath.Join(out, "r.json"))
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("writeReport.HttpRunReportWriteFailed") {
		t.Errorf("error = %v", err)
	}
}

func TestTheRunEndWords(t *testing.T) {
	for end, want := range map[runEnd]string{runCompleted: "completed", runStopped: "stopped", runCanceled: "canceled", runNotStarted: "not started"} {
		if end.String() != want {
			t.Errorf("%d = %q", end, end.String())
		}
	}
}

// fakeRunnerBusy says whether the fake tab shows the busy mark.
func (r *rig) fakeRunnerBusy(t *runTab) bool {
	for _, tab := range r.api.EditorTabs() {
		if tab.ID() == t.tab.ID() {
			return tab.Busy()
		}
	}
	return false
}

// fakeRunnerTitle is the title of the fake tab.
func (r *rig) fakeRunnerTitle(t *runTab) string {
	for _, tab := range r.api.EditorTabs() {
		if tab.ID() == t.tab.ID() {
			return tab.Title()
		}
	}
	return ""
}

func TestTheCommandCanFillInTheTabAndStartTheRun(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	writeFile(t, filepath.Join(r.folder, "rows.csv"), "user\nAda\nGrace\n")
	args := map[string]any{"path": r.col("flow/admin"), "data": "rows.csv", "stop": true, "delayMs": 1.0, "environment": "dev", "start": true}
	r.run(CmdRunCollection, args)
	tab := r.ext.runTabs[0]
	r.api.WaitFor("the run to end", func() bool { return !tab.running && r.ext.pending.Load() == 0 })
	if !tab.stop || tab.delay.Value() != "1" || tab.envKey != "dev" || len(tab.results) != 2 || tab.end != runCompleted {
		t.Errorf("stop %v delay %q env %q results %d end %v", tab.stop, tab.delay.Value(), tab.envKey, len(tab.results), tab.end)
	}
	// the tab that is open is filled in again, and the same arguments as JSON do the same
	raw, err := json.Marshal(map[string]any{"path": r.col("flow/admin"), "iterations": 2, "data": "", "start": true})
	if err != nil {
		t.Fatal(err)
	}
	r.run(CmdRunCollection, json.RawMessage(raw))
	r.api.WaitFor("the second run to end", func() bool { return !tab.running && r.ext.pending.Load() == 0 })
	if len(r.ext.runTabs) != 1 || len(tab.results) != 2 || tab.total != 2 || tab.data.Value() != "" {
		t.Errorf("tabs %d results %d total %d data %q", len(r.ext.runTabs), len(tab.results), tab.total, tab.data.Value())
	}
}
