package httpclient

import (
	"context"
	"encoding/json"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/oauth"
	"github.com/rm4n0s/emetos/extensions/httpclient/oauth/fakeas"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

// The tests of OAuth 2.0 in the request tab and the runner (step 27.8). The authorization server is the fake of package fakeas on
// 127.0.0.1, the browser a fake that follows the redirect, and the keyring the memory keyring of the rig.

// tclock is a clock the tests move.
type tclock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *tclock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *tclock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// testBrowser is the browser of the tests: it counts the pages it was asked to open and follows them like a user who agrees, unless then
// is set.
type testBrowser struct {
	opened atomic.Int32
	mu     sync.Mutex
	urls   []string
	then   func(ctx context.Context, url string) error
}

func (b *testBrowser) open(ctx context.Context, url string) error {
	b.opened.Add(1)
	b.mu.Lock()
	b.urls = append(b.urls, url)
	b.mu.Unlock()
	if b.then != nil {
		return b.then(ctx, url)
	}
	return fakeas.Browser(ctx, url)
}

func (b *testBrowser) lastURL() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.urls) == 0 {
		return ""
	}
	return b.urls[len(b.urls)-1]
}

// oauthWorld is a rig whose collection "oauth" has the authorization given, over a fake authorization server and an echo server.
type oauthWorld struct {
	*rig
	as    *fakeas.Server
	echo  *echoServer
	br    *testBrowser
	clock *tclock
}

// authJSON is the "auth" of the collection of a grant for the server.
func authJSON(s *fakeas.Server, grant string) string {
	return `{"type": "oauth2", "grant": "` + grant + `", "tokenUrl": "` + s.TokenURL + `", "authUrl": "` + s.AuthURL +
		`", "clientId": "client-1", "clientSecret": "secret-1", "scope": "read write", "pkce": true}`
}

func newOAuthWorld(t *testing.T, grant string, asOpts fakeas.Options, mod func(*Options)) *oauthWorld {
	t.Helper()
	w := &oauthWorld{br: &testBrowser{}, clock: &tclock{t: testNow}}
	var err error
	if w.as, err = fakeas.Start(asOpts); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.as.Close)
	w.echo = newEchoServer(t)
	w.rig = newRigWith(t, func(o *Options) {
		o.Browser, o.Now = w.br.open, w.clock.Now
		if mod != nil {
			mod(o)
		}
	}, func(folder string) {
		dir := filepath.Join(folder, ".emetos", "http", "collections", "oauth")
		writeFile(t, filepath.Join(dir, "collection.jsonc"), `{"emetosHttp": 1, "name": "OAuth", "auth": `+authJSON(w.as, grant)+`}`)
		writeFile(t, filepath.Join(dir, "ping.request.jsonc"), `{"emetosHttp": 1, "name": "Ping", "method": "GET", "url": "{{baseUrl}}/ping"}`)
	})
	w.useServer(w.echo)
	return w
}

// send sends the request of the tab and waits for the answer.
func (w *oauthWorld) send(tab *reqTab) {
	w.t.Helper()
	tab.sendRequest()
	w.settle()
}

// ping opens the request of the collection.
func (w *oauthWorld) ping() *reqTab {
	w.t.Helper()
	return w.openReq("oauth/ping.request.jsonc")
}

func (w *oauthWorld) lastAuth() string {
	got := w.echo.requests()
	if len(got) == 0 {
		return ""
	}
	return got[len(got)-1].auth
}

// token reads the access token the keyring holds for the collection.
func (w *oauthWorld) token(grant string) (oauth.Token, bool) {
	w.t.Helper()
	st := oauth.Store{Secrets: w.kr, Folder: w.folder}
	tok, ok, err := st.Load(context.Background(), variables.CollectionOwner("oauth"), grant)
	if err != nil {
		w.t.Fatal(err)
	}
	return tok, ok
}

// everythingOnDisk is the text of every file of the project and of the folders of the user the extension may write to.
func (w *oauthWorld) everythingOnDisk() string {
	w.t.Helper()
	var sb strings.Builder
	for _, root := range []string{w.folder, os.Getenv("HOME")} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			sb.Write(data)
			return nil
		})
		if err != nil {
			w.t.Fatal(err)
		}
	}
	return sb.String()
}

func TestClientCredentialsTokenIsSentAndStaysInTheKeyring(t *testing.T) {
	w := newOAuthWorld(t, "client_credentials", fakeas.Options{ExpiresIn: 600}, nil)
	tab := w.ping()
	w.send(tab)
	auth := w.lastAuth()
	if !strings.HasPrefix(auth, "Bearer at-") || !strings.HasPrefix(tab.send.status, "201 Created") || tab.send.bad {
		t.Fatalf("auth %q status %q", auth, tab.send.status)
	}
	tok, ok := w.token("client_credentials")
	if !ok || "Bearer "+tok.AccessToken != auth || tok.Fingerprint == "" {
		t.Fatalf("the keyring has %+v (%v)", tok, ok)
	}
	// a second send reuses the token; the server was asked once
	w.send(tab)
	if w.as.Count("client_credentials") != 1 || w.lastAuth() != auth {
		t.Errorf("token calls %d, auth %q", w.as.Count("client_credentials"), w.lastAuth())
	}
	if w.br.opened.Load() != 0 {
		t.Error("the client credentials grant opened a browser")
	}
	// the token is in no file of the project, in no file of the user's folders and not in the history or the current values
	if disk := w.everythingOnDisk(); strings.Contains(disk, tok.AccessToken) {
		t.Error("the access token is in a file")
	}
	hist, _ := json.Marshal(w.ext.hist.List())
	if strings.Contains(string(hist), tok.AccessToken) {
		t.Error("the access token is in the history")
	}
	for _, key := range []string{variables.StateKey, "httpclient.activeEnvironment"} {
		if raw, ok := w.api.State().Get(key); ok && strings.Contains(string(raw), tok.AccessToken) {
			t.Errorf("the access token is in the state %q", key)
		}
	}
	if raw, _ := json.Marshal(tab.d.files()); strings.Contains(string(raw), tok.AccessToken) {
		t.Error("the access token is in the draft")
	}
	st, _ := tab.saveState()
	if raw, _ := json.Marshal(st); strings.Contains(string(raw), tok.AccessToken) {
		t.Error("the access token is in the state of the tab")
	}
}

func TestAuthorizationCodeThroughTheAuthTab(t *testing.T) {
	w := newOAuthWorld(t, "authorization_code", fakeas.Options{ExpiresIn: 600}, nil)
	tab := w.ping()

	// a send with no token opens no browser and says what to do
	w.send(tab)
	if w.br.opened.Load() != 0 || len(w.echo.requests()) != 0 {
		t.Fatalf("browser opened %d times, server got %d requests", w.br.opened.Load(), len(w.echo.requests()))
	}
	if !tab.send.bad || !strings.Contains(tab.send.status, "Get New Access Token") {
		t.Errorf("status %q", tab.send.status)
	}

	tab.section = secAuth
	ts := w.showReq(uitest.Themes()[0], tab, reqW, 520)
	ts.Pump(time.Millisecond)
	w.settle()
	ts.Pump(time.Millisecond)
	for _, want := range []string{"Get New Access Token", "Clear Token", "No token yet"} {
		if !ts.Exists(gluttertest.ByTextContaining(want, rich)) {
			t.Errorf("the Auth tab does not show %q", want)
		}
	}
	ts.Tap(gluttertest.First(gluttertest.ByText("Get New Access Token", rich)))
	if !tab.oauth.busy {
		t.Error("the tab is not waiting")
	}
	w.settle()
	ts.Pump(time.Millisecond)
	if tab.oauth.busy || !tab.oauth.found || tab.oauth.bad || tab.oauth.msg != "Got a new access token." {
		t.Fatalf("busy %v found %v msg %q", tab.oauth.busy, tab.oauth.found, tab.oauth.msg)
	}
	if w.br.opened.Load() != 1 || !strings.HasPrefix(w.br.lastURL(), w.as.AuthURL+"?") {
		t.Errorf("the browser got %d pages, last %q", w.br.opened.Load(), w.br.lastURL())
	}
	if tab.oauth.box.Value() != tab.oauth.tok.AccessToken || tab.oauth.tok.AccessToken == "" {
		t.Errorf("the box holds %q", tab.oauth.box.Value())
	}
	if !ts.Exists(gluttertest.ByTextContaining("Bearer · expires in 10 min · can be refreshed", rich)) {
		t.Error("the Auth tab does not describe the token")
	}
	for _, r := range w.as.Requests() {
		if r.Grant == "authorization_code" && (!r.PKCEOK || r.Verifier == "") {
			t.Errorf("the code was traded without a checked verifier: %+v", r)
		}
	}

	w.send(tab)
	if got := w.lastAuth(); got != "Bearer "+tab.oauth.tok.AccessToken || tab.send.bad {
		t.Errorf("auth %q status %q", got, tab.send.status)
	}
	if strings.Contains(w.everythingOnDisk(), tab.oauth.tok.AccessToken) {
		t.Error("the access token is in a file")
	}

	// Clear Token
	ts.Tap(gluttertest.First(gluttertest.ByText("Clear Token", rich)))
	w.settle()
	ts.Pump(time.Millisecond)
	if _, ok := w.token("authorization_code"); ok || tab.oauth.found || tab.oauth.box.Value() != "" {
		t.Error("the token was not cleared")
	}
	n := len(w.echo.requests())
	w.send(tab)
	if len(w.echo.requests()) != n || !tab.send.bad {
		t.Errorf("a send without a token went out: %q", tab.send.status)
	}
}

func TestTheTokenIsRefreshedAndAnExpiredRefreshTokenAsksAgain(t *testing.T) {
	w := newOAuthWorld(t, "authorization_code", fakeas.Options{ExpiresIn: 600}, nil)
	tab := w.ping()
	tab.getNewToken()
	w.settle()
	tok1, _ := w.token("authorization_code")

	w.clock.Add(11 * time.Minute)
	w.send(tab)
	if w.as.Count("refresh_token") != 1 || w.br.opened.Load() != 1 || tab.send.bad {
		t.Fatalf("refreshes %d, browser %d, status %q", w.as.Count("refresh_token"), w.br.opened.Load(), tab.send.status)
	}
	tok2, _ := w.token("authorization_code")
	if tok2.AccessToken == tok1.AccessToken || w.lastAuth() != "Bearer "+tok2.AccessToken {
		t.Errorf("sent %q, kept %q (was %q)", w.lastAuth(), tok2.AccessToken, tok1.AccessToken)
	}

	// the refresh token is no good any more
	w.clock.Add(11 * time.Minute)
	w.as.RevokeRefreshTokens()
	n := len(w.echo.requests())
	w.send(tab)
	if len(w.echo.requests()) != n || !tab.send.bad || w.br.opened.Load() != 1 {
		t.Fatalf("status %q, browser %d", tab.send.status, w.br.opened.Load())
	}
	for _, want := range []string{"refused its refresh token", "Get New Access Token"} {
		if !strings.Contains(tab.send.status, want) {
			t.Errorf("status %q lacks %q", tab.send.status, want)
		}
	}
	tab.getNewToken()
	w.settle()
	w.send(tab)
	if tab.send.bad || w.br.opened.Load() != 2 {
		t.Errorf("after signing in again: %q, browser %d", tab.send.status, w.br.opened.Load())
	}
}

func TestGetNewAccessTokenFailsClearly(t *testing.T) {
	tests := []struct {
		name  string
		setup func(w *oauthWorld)
		want  string
	}{
		{name: "consent denied", setup: func(w *oauthWorld) { w.as.Deny(true) }, want: "access_denied"},
		{name: "state mismatch", setup: func(w *oauthWorld) { w.as.BadState(true) }, want: "state"},
		{name: "the browser cannot be opened", setup: func(w *oauthWorld) {
			w.br.then = func(context.Context, string) error { return errFake }
		}, want: "the fake failed"},
		{name: "the user never comes back", setup: func(w *oauthWorld) {
			w.br.then = func(context.Context, string) error { return nil }
		}, want: "not completed in time"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newOAuthWorld(t, "authorization_code", fakeas.Options{}, func(o *Options) { o.OAuthTimeout = 80 * time.Millisecond })
			tt.setup(w)
			tab := w.ping()
			tab.getNewToken()
			w.settle()
			if tab.oauth.busy || tab.oauth.found || !tab.oauth.bad || !strings.Contains(tab.oauth.msg, tt.want) {
				t.Fatalf("busy %v found %v bad %v msg %q", tab.oauth.busy, tab.oauth.found, tab.oauth.bad, tab.oauth.msg)
			}
			if _, ok := w.token("authorization_code"); ok {
				t.Error("a token was kept")
			}
			// the loopback server is closed: its port refuses
			if u := w.br.lastURL(); u != "" {
				assertRedirectClosed(t, u)
			}
		})
	}
}

func TestCancelEndsTheWaitForTheBrowser(t *testing.T) {
	w := newOAuthWorld(t, "authorization_code", fakeas.Options{}, nil)
	waiting := make(chan struct{})
	w.br.then = func(context.Context, string) error { close(waiting); return nil }
	tab := w.ping()
	tab.getNewToken()
	<-waiting
	if !tab.oauth.busy {
		t.Fatal("not waiting")
	}
	tab.cancelToken()
	w.settle()
	if tab.oauth.busy || tab.oauth.bad || tab.oauth.msg != "Canceled." {
		t.Errorf("busy %v bad %v msg %q", tab.oauth.busy, tab.oauth.bad, tab.oauth.msg)
	}
	assertRedirectClosed(t, w.br.lastURL())
	// one wait at a time: a second press while waiting does nothing
	tab.oauth.busy = true
	tab.getNewToken()
	tab.oauth.busy = false
	if w.br.opened.Load() != 1 {
		t.Errorf("the browser was opened %d times", w.br.opened.Load())
	}
}

func TestTheCommandsGetAndClearTheToken(t *testing.T) {
	w := newOAuthWorld(t, "client_credentials", fakeas.Options{}, nil)
	tab := w.ping()
	w.run(CmdGetNewAccessToken, nil)
	if _, ok := w.token("client_credentials"); !ok || !tab.oauth.found {
		t.Fatal("no token after the command")
	}
	if got := w.notes(); len(got) == 0 || got[len(got)-1] != "info: Got a new access token." {
		t.Errorf("notes %v", got)
	}
	w.run(CmdClearAccessToken, nil)
	if _, ok := w.token("client_credentials"); ok || tab.oauth.found {
		t.Error("the token was not cleared")
	}
	// a request that does not use OAuth 2.0 says so
	tab.d.auth.typ = format.AuthNone
	w.run(CmdGetNewAccessToken, nil)
	if got := w.notes(); got[len(got)-1] != "info: This request does not use OAuth 2.0." {
		t.Errorf("notes %v", got)
	}
}

func TestTheRunnerUsesStoredTokensAndOpensNoBrowser(t *testing.T) {
	w := newOAuthWorld(t, "authorization_code", fakeas.Options{ExpiresIn: 600}, nil)
	run := w.openRunner("oauth")
	// no token: the request fails with the message, nothing is sent and no browser opens mid-run (decision 1882)
	w.runAndWait(run)
	if len(run.results) != 1 || !run.results[0].failed() || !strings.Contains(run.results[0].Err, "Get New Access Token") {
		t.Fatalf("results %+v", run.results)
	}
	if w.br.opened.Load() != 0 || len(w.echo.requests()) != 0 {
		t.Errorf("browser %d, requests %d", w.br.opened.Load(), len(w.echo.requests()))
	}
	// with a token from the Auth tab the run sends it, and refreshes it when it expired
	tab := w.ping()
	tab.getNewToken()
	w.settle()
	run.results = nil
	w.runAndWait(run)
	if len(run.results) != 1 || run.results[0].Err != "" || !strings.HasPrefix(w.lastAuth(), "Bearer at-") {
		t.Fatalf("results %+v, auth %q", run.results, w.lastAuth())
	}
	w.clock.Add(11 * time.Minute)
	run.results = nil
	w.runAndWait(run)
	if run.results[0].Err != "" || w.as.Count("refresh_token") != 1 || w.br.opened.Load() != 1 {
		t.Errorf("results %+v, refreshes %d, browser %d", run.results, w.as.Count("refresh_token"), w.br.opened.Load())
	}
}

func TestAnotherAuthorizationDoesNotUseTheToken(t *testing.T) {
	w := newOAuthWorld(t, "client_credentials", fakeas.Options{}, nil)
	tab := w.ping()
	w.send(tab)
	// the request has its own authorization with another scope: it needs its own token
	tab.d.auth.typ = format.AuthOAuth2
	tab.d.auth.grant = "client_credentials"
	for f, v := range map[string]string{fTokenURL: w.as.TokenURL, fClientID: "client-1", fClientSecret: "secret-1", fScope: "admin"} {
		tab.d.auth.box[f].SetValue(v)
	}
	first := w.lastAuth()
	w.send(tab)
	if w.as.Count("client_credentials") != 2 || w.lastAuth() == first {
		t.Errorf("token calls %d, auth %q (was %q)", w.as.Count("client_credentials"), w.lastAuth(), first)
	}
}

func TestVariablesInTheAuthorizationAreReplaced(t *testing.T) {
	w := newOAuthWorld(t, "client_credentials", fakeas.Options{}, nil)
	tab := w.ping()
	if err := w.ext.values.Set(context.Background(), variables.EnvironmentOwner("dev"), "authBase", w.as.URL, false); err != nil {
		t.Fatal(err)
	}
	tab.d.auth.typ = format.AuthOAuth2
	tab.d.auth.grant = "client_credentials"
	for f, v := range map[string]string{fTokenURL: "{{authBase}}/token", fClientID: "client-1", fClientSecret: "secret-1"} {
		tab.d.auth.box[f].SetValue(v)
	}
	w.send(tab)
	if tab.send.bad || !strings.HasPrefix(w.lastAuth(), "Bearer at-") {
		t.Errorf("status %q auth %q", tab.send.status, w.lastAuth())
	}
}

func TestADevFakeAuthorizationServerCompletesTheFlowItself(t *testing.T) {
	if !foundation.DebugMode {
		t.Skip("the dev command exists in debug builds")
	}
	r := newRig(t, func(folder string) {
		dir := filepath.Join(folder, ".emetos", "http", "collections", "oauth")
		writeFile(t, filepath.Join(dir, "collection.jsonc"), `{"emetosHttp": 1, "name": "OAuth", "auth": {"type": "oauth2", "grant": "authorization_code",
  "tokenUrl": "{{authBase}}/token", "authUrl": "{{authBase}}/authorize", "clientId": "emetos-dev", "clientSecret": "dev-secret", "scope": "read", "pkce": true}}`)
		writeFile(t, filepath.Join(dir, "ping.request.jsonc"), `{"emetosHttp": 1, "name": "Ping", "method": "GET", "url": "{{baseUrl}}/ping"}`)
	})
	r.run(CmdSelectEnvironment, "dev")
	r.run(CommandDevFakeOAuth, nil)
	base := r.ext.values.All("environment:dev")["authBase"]
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("authBase = %q", base)
	}
	tab := r.openReq("oauth/ping.request.jsonc")
	r.run(CmdGetNewAccessToken, nil)
	if !tab.oauth.found || tab.oauth.bad || tab.oauth.tok.AccessToken == "" {
		t.Fatalf("found %v msg %q", tab.oauth.found, tab.oauth.msg)
	}
	// the token is in memory: the keyring of the rig, which stands for the user's, was not used
	if _, ok, _ := r.kr.Lookup(context.Background(), (oauth.Store{Folder: r.folder}).Key(variables.CollectionOwner("oauth"), "authorization_code")); ok {
		t.Error("the dev flow used the keyring")
	}
	// the server stops with the extension
	r.cancel()
	r.api.WaitFor("the server to stop", func() bool {
		_, err := (&oauth.Client{}).ClientCredentials(context.Background(), oauth.Config{Grant: oauth.GrantClientCredentials, TokenURL: base + "/token", ClientID: "emetos-dev", ClientSecret: "dev-secret"})
		return err != nil
	})
}

func TestTheDevFakeAuthorizationServerCannotStartAfterTheEnd(t *testing.T) {
	if !foundation.DebugMode {
		t.Skip("the dev command exists in debug builds")
	}
	r := newRig(t)
	r.cancel()
	err := r.api.Run(CommandDevFakeOAuth, nil)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("HttpDevFakeOAuthFailed") {
		t.Fatalf("err = %v", err)
	}
	// without an environment it only says where it is
	r2 := newRig(t)
	r2.run(CommandDevFakeOAuth, nil)
	if got := r2.notes(); len(got) != 1 || !strings.Contains(got[0], "Fake authorization server on http://127.0.0.1:") {
		t.Errorf("notes = %v", got)
	}
}

func TestTheAuthTabReadsTheStoredToken(t *testing.T) {
	w := newOAuthWorld(t, "client_credentials", fakeas.Options{}, nil)
	tab := w.ping()
	w.send(tab)
	tab.section = secAuth
	tab.oauth.loaded = false
	ts := w.showReq(uitest.Themes()[0], tab, reqW, 520)
	ts.Pump(time.Millisecond)
	w.settle()
	ts.Pump(time.Millisecond)
	if !tab.oauth.found {
		t.Fatal("the tab did not read the token")
	}
	if tab.oauth.box.Value() != tab.oauth.tok.AccessToken || tab.oauth.tok.AccessToken == "" {
		t.Errorf("the box holds %q", tab.oauth.box.Value())
	}
}

func TestOAuthConfigOfAnAuth(t *testing.T) {
	a := &format.Auth{Type: format.AuthOAuth2, TokenURL: " {{b}}/token ", ClientID: "{{id}}", ClientSecret: " {{s}} ", Scope: "x"}
	r, err := variables.Build(context.Background(), variables.Input{Locals: map[string]string{"b": "http://h", "id": "c1", "s": "sec"}})
	if err != nil {
		t.Fatal(err)
	}
	got := oauthConfigOf(a, r)
	if got.Grant != oauth.GrantClientCredentials || got.TokenURL != "http://h/token" || got.ClientID != "c1" || got.ClientSecret != " sec " || got.Scope != "x" {
		t.Errorf("config %+v", got)
	}
	if got := oauthConfigOf(&format.Auth{TokenURL: "u"}, nil); got.TokenURL != "u" {
		t.Errorf("without a resolver: %+v", got)
	}
	if effectiveOAuth(&format.Auth{Type: format.AuthBearer}, place{auth: a}) != nil || effectiveOAuth(nil, place{auth: a}) != a || effectiveOAuth(&format.Auth{Type: format.AuthInherit}, place{}) != nil {
		t.Error("effectiveOAuth")
	}
	if oauthOwner(place{}) != "" {
		t.Error("an unsaved request has a shared owner")
	}
}

func TestTokenInfoText(t *testing.T) {
	now := testNow
	tests := []struct {
		tok  oauth.Token
		want string
	}{
		{oauth.Token{}, "Bearer · no expiry given"},
		{oauth.Token{ExpiresAt: now.Add(10*time.Second + 5*time.Minute), RefreshToken: "r"}, "Bearer · expires in 5 min · can be refreshed"},
		{oauth.Token{TokenType: "MAC", ExpiresAt: now.Add(-3 * time.Hour)}, "MAC · expired 3 h ago"},
		{oauth.Token{ExpiresAt: now.Add(20 * time.Second)}, "Bearer · expires in 20 s"},
		{oauth.Token{ExpiresAt: now.Add(72 * time.Hour)}, "Bearer · expires in 3 days"},
	}
	for _, tt := range tests {
		if got := tokenInfo(tt.tok, now); got != tt.want {
			t.Errorf("tokenInfo = %q, want %q", got, tt.want)
		}
	}
}

// assertRedirectClosed checks that nothing listens on the loopback address of an authorization URL.
func urlUnescape(s string) (string, error) { return url.QueryUnescape(s) }

func assertRedirectClosed(t *testing.T, authURL string) {
	t.Helper()
	i := strings.Index(authURL, "redirect_uri=")
	if i < 0 {
		t.Fatalf("no redirect_uri in %q", authURL)
	}
	rest := authURL[i+len("redirect_uri="):]
	if j := strings.IndexByte(rest, '&'); j >= 0 {
		rest = rest[:j]
	}
	redirect, err := urlUnescape(rest)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.TrimSuffix(strings.TrimPrefix(redirect, "http://"), "/callback")
	if conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "tcp", host); err == nil {
		_ = conn.Close()
		t.Fatalf("the loopback server still listens on %s", host)
	}
}
