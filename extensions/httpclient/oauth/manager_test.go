package oauth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/oauth/fakeas"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

func newManager(t *testing.T, s *fakeas.Server) (*Manager, *dbconn.MemoryKeyring, *clock) {
	t.Helper()
	kr := &dbconn.MemoryKeyring{}
	clk := newClock()
	return &Manager{Client: Client{Now: clk.Now}, Store: Store{Secrets: kr, Folder: "/project"}, Flow: FlowOptions{Open: fakeas.Browser}}, kr, clk
}

func TestManagerClientCredentials(t *testing.T) {
	s := startAS(t, fakeas.Options{ExpiresIn: 600})
	m, _, clk := newManager(t, s)
	cfg := configFor(s, GrantClientCredentials)
	ctx := t.Context()

	first, err := m.Token(ctx, "collection:shop", cfg)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Token(ctx, "collection:shop", cfg)
	if err != nil || again.AccessToken != first.AccessToken || s.Count("client_credentials") != 1 {
		t.Fatalf("a valid token is reused: %+v %v, %d calls", again, err, s.Count("client_credentials"))
	}
	if other, err := m.Token(ctx, "collection:other", cfg); err != nil || other.AccessToken == first.AccessToken {
		t.Errorf("another collection has its own token: %+v %v", other, err)
	}
	clk.Add(11 * time.Minute) // expired and there is no refresh token: ask again, no user needed
	renewed, err := m.Token(ctx, "collection:shop", cfg)
	if err != nil || renewed.AccessToken == first.AccessToken {
		t.Fatalf("renewed %+v %v", renewed, err)
	}
	stored, found, err := m.Stored(ctx, "collection:shop", cfg)
	if err != nil || !found || stored.AccessToken != renewed.AccessToken {
		t.Errorf("stored %+v %v %v", stored, found, err)
	}
	// another scope is another authorization: the token does not apply
	changed := cfg
	changed.Scope = "admin"
	if _, found, _ := m.Stored(ctx, "collection:shop", changed); found {
		t.Error("a token was found for another scope")
	}
}

func TestManagerAuthorizationCodeRefresh(t *testing.T) {
	s := startAS(t, fakeas.Options{ExpiresIn: 600})
	m, _, clk := newManager(t, s)
	cfg := configFor(s, GrantAuthorizationCode)
	ctx := t.Context()

	// no token: a send never opens a browser; the message says what to do
	_, err := m.Token(ctx, "collection:shop", cfg)
	wantRoute(t, err, "Manager.Token.OAuthTokenMissing")
	if !strings.Contains(err.Error(), "Get New Access Token") || !strings.Contains(err.Error(), "no access token") {
		t.Errorf("message %q", err)
	}

	got, err := m.GetNew(ctx, "collection:shop", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if tok, err := m.Token(ctx, "collection:shop", cfg); err != nil || tok.AccessToken != got.AccessToken {
		t.Fatalf("the new token is used: %+v %v", tok, err)
	}

	clk.Add(11 * time.Minute) // expired: refreshed with the refresh token, no browser
	refreshed, err := m.Token(ctx, "collection:shop", cfg)
	if err != nil || refreshed.AccessToken == got.AccessToken {
		t.Fatalf("refreshed %+v %v", refreshed, err)
	}
	if s.Count("refresh_token") != 1 || s.Count("authorization_code") != 1 {
		t.Errorf("refresh %d, code %d", s.Count("refresh_token"), s.Count("authorization_code"))
	}
	if stored, _, _ := m.Stored(ctx, "collection:shop", cfg); stored.AccessToken != refreshed.AccessToken || stored.RefreshToken == got.RefreshToken {
		t.Errorf("the refreshed token and the rotated refresh token are kept: %+v", stored)
	}

	// the refresh token expired: the user is asked again, with a clear message
	clk.Add(11 * time.Minute)
	s.RevokeRefreshTokens()
	_, err = m.Token(ctx, "collection:shop", cfg)
	wantRoute(t, err, "Manager.Token.OAuthTokenMissing")
	if !strings.Contains(err.Error(), "refused its refresh token") || !strings.Contains(err.Error(), "Get New Access Token") {
		t.Errorf("message %q", err)
	}
	// and asking again works
	if _, err := m.GetNew(ctx, "collection:shop", cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Token(ctx, "collection:shop", cfg); err != nil {
		t.Errorf("after signing in again: %v", err)
	}
}

func TestManagerExpiredWithoutRefreshToken(t *testing.T) {
	s := startAS(t, fakeas.Options{ExpiresIn: 600, NoRefreshToken: true})
	m, _, clk := newManager(t, s)
	cfg := configFor(s, GrantAuthorizationCode)
	if _, err := m.GetNew(t.Context(), "", cfg); err != nil {
		t.Fatal(err)
	}
	clk.Add(time.Hour)
	_, err := m.Token(t.Context(), "", cfg)
	wantRoute(t, err, "Manager.Token.OAuthTokenMissing")
	if !strings.Contains(err.Error(), "has expired") {
		t.Errorf("message %q", err)
	}
}

func TestManagerRefreshWhenServerIsDown(t *testing.T) {
	s := startAS(t, fakeas.Options{ExpiresIn: 600})
	m, _, clk := newManager(t, s)
	cfg := configFor(s, GrantAuthorizationCode)
	if _, err := m.GetNew(t.Context(), "c", cfg); err != nil {
		t.Fatal(err)
	}
	clk.Add(time.Hour)
	s.Close()
	// the server cannot be reached: that is said as it is, the user is not sent to sign in again
	_, err := m.Token(t.Context(), "c", cfg)
	wantRoute(t, err, "Client.exchange.OAuthTokenRequestFailed")
}

func TestManagerKeyringAndConfigErrors(t *testing.T) {
	s := startAS(t, fakeas.Options{})
	m, kr, _ := newManager(t, s)
	cfg := configFor(s, GrantClientCredentials)
	ctx := t.Context()

	bad := cfg
	bad.TokenURL = ""
	_, err := m.Token(ctx, "c", bad)
	wantRoute(t, err, "Config.Validate.OAuthConfigInvalid")

	kr.Err = errors.New("FakeKeyringFailed", "locked")
	_, err = m.Token(ctx, "c", cfg)
	wantRoute(t, err, "FakeKeyringFailed")
	if _, err := m.GetNew(ctx, "c", cfg); err == nil {
		t.Error("a token that cannot be kept was reported as kept")
	}
	if err := m.Clear(ctx, "c", cfg); err == nil {
		t.Error("clearing with a failing keyring reported success")
	}
	kr.Err = nil

	// a keyring entry that is not a token is as good as none
	if err := kr.Store(ctx, m.Store.Key("c", cfg.Grant), "x", "{not json"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := m.Stored(ctx, "c", cfg); found || err != nil {
		t.Errorf("found %v, err %v", found, err)
	}
	if tok, err := m.Token(ctx, "c", cfg); err != nil || tok.AccessToken == "" {
		t.Errorf("a fresh token replaces it: %+v %v", tok, err)
	}
}

func TestManagerClear(t *testing.T) {
	s := startAS(t, fakeas.Options{})
	m, kr, _ := newManager(t, s)
	cfg := configFor(s, GrantClientCredentials)
	if _, err := m.GetNew(t.Context(), "c", cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := kr.Lookup(t.Context(), m.Store.Key("c", cfg.Grant)); !ok {
		t.Fatal("the token is not in the keyring")
	}
	if err := m.Clear(t.Context(), "c", cfg); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := m.Stored(t.Context(), "c", cfg); found {
		t.Error("the token is still there")
	}
	if err := m.Clear(t.Context(), "c", cfg); err != nil {
		t.Errorf("clearing none: %v", err)
	}
}

func TestStore(t *testing.T) {
	ctx := context.Background()
	kr := &dbconn.MemoryKeyring{}
	st := Store{Secrets: kr, Folder: "/p"}
	if k1, k2 := st.Key("collection:a", "client_credentials"), st.Key("collection:a", "authorization_code"); k1 == k2 || !strings.HasPrefix(k1, "http-client|/p|oauth:collection:a|") {
		t.Errorf("keys %q %q", k1, k2)
	}
	if st.Key("", "g") == st.Key("collection:x", "g") || (Store{Folder: "/q"}).Key("", "g") == st.Key("", "g") {
		t.Error("keys of different owners or folders clash")
	}
	if err := kr.Store(ctx, st.Key("o", "g"), "x", `{"accessToken":""}`); err != nil {
		t.Fatal(err)
	}
	_, _, err := st.Load(ctx, "o", "g")
	wantRoute(t, err, "Store.Load.OAuthStoredTokenInvalid")

	none := Store{}
	if _, found, err := none.Load(ctx, "o", "g"); found || err != nil {
		t.Errorf("no keyring: %v %v", found, err)
	}
	wantRoute(t, none.Save(ctx, "o", "g", Token{AccessToken: "a"}), "Store.Save.OAuthNoKeyring")
	if err := none.Clear(ctx, "o", "g"); err != nil {
		t.Error(err)
	}
	want := Token{AccessToken: "a", RefreshToken: "r", TokenType: "Bearer", ExpiresAt: time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC), Fingerprint: "f"}
	if err := st.Save(ctx, "o", "g", want); err != nil {
		t.Fatal(err)
	}
	got, found, err := st.Load(ctx, "o", "g")
	if err != nil || !found || got != want {
		t.Errorf("round trip: %+v %v %v", got, found, err)
	}
}
