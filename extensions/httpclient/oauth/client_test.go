package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/oauth/fakeas"
)

func TestConfigValidate(t *testing.T) {
	ok := Config{Grant: GrantAuthorizationCode, TokenURL: "https://as.example/token", AuthURL: "https://as.example/auth", ClientID: "c"}
	tests := []struct {
		name   string
		mutate func(*Config)
		field  string // "" for valid
	}{
		{name: "valid", mutate: func(*Config) {}},
		{name: "client credentials needs no auth URL", mutate: func(c *Config) { c.Grant, c.AuthURL = GrantClientCredentials, "" }},
		{name: "grant", mutate: func(c *Config) { c.Grant = "password" }, field: "grant"},
		{name: "token url", mutate: func(c *Config) { c.TokenURL = "ftp://x/token" }, field: "tokenUrl"},
		{name: "token url empty", mutate: func(c *Config) { c.TokenURL = "" }, field: "tokenUrl"},
		{name: "client id", mutate: func(c *Config) { c.ClientID = "" }, field: "clientId"},
		{name: "auth url", mutate: func(c *Config) { c.AuthURL = "not a url" }, field: "authUrl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ok
			tt.mutate(&c)
			err := c.Validate()
			if tt.field == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			wantRoute(t, err, "Config.Validate.OAuthConfigInvalid")
			appErr, _ := errors.FromError(err)
			if tt.field != "grant" && appErr.Metadata["field"] != tt.field {
				t.Fatalf("field %v, want %s", appErr.Metadata["field"], tt.field)
			}
		})
	}
}

func TestFingerprintFollowsTheAuthorization(t *testing.T) {
	base := Config{Grant: GrantClientCredentials, TokenURL: "https://a/token", ClientID: "c", Scope: "s"}
	same := base
	same.ClientSecret = "another secret" // the secret is not part of it
	if base.Fingerprint() != same.Fingerprint() {
		t.Error("the secret changed the fingerprint")
	}
	for _, mutate := range []func(*Config){
		func(c *Config) { c.TokenURL = "https://b/token" }, func(c *Config) { c.ClientID = "d" }, func(c *Config) { c.Scope = "t" },
		func(c *Config) { c.Audience = "api" }, func(c *Config) { c.Grant = GrantAuthorizationCode },
	} {
		c := base
		mutate(&c)
		if c.Fingerprint() == base.Fingerprint() {
			t.Errorf("%+v has the fingerprint of %+v", c, base)
		}
	}
}

func TestTokenExpiryAndScheme(t *testing.T) {
	now := newClock().Now()
	tests := []struct {
		name    string
		tok     Token
		expired bool
		scheme  string
	}{
		{name: "no lifetime", tok: Token{AccessToken: "a"}, scheme: "Bearer"},
		{name: "valid", tok: Token{ExpiresAt: now.Add(time.Hour), TokenType: "bearer"}, scheme: "Bearer"},
		{name: "inside the skew", tok: Token{ExpiresAt: now.Add(10 * time.Second), TokenType: "Bearer"}, expired: true, scheme: "Bearer"},
		{name: "ended", tok: Token{ExpiresAt: now.Add(-time.Minute), TokenType: "MAC"}, expired: true, scheme: "MAC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tok.Expired(now); got != tt.expired {
				t.Errorf("Expired = %v", got)
			}
			if got := tt.tok.Scheme(); got != tt.scheme {
				t.Errorf("Scheme = %q", got)
			}
		})
	}
}

func TestClientCredentials(t *testing.T) {
	tests := []struct {
		name      string
		opts      fakeas.Options
		expiresIn time.Duration // 0: no expiry
	}{
		{name: "with the secret", expiresIn: time.Hour},
		{name: "no lifetime", opts: fakeas.Options{ExpiresIn: -1}},
		{name: "short lifetime", opts: fakeas.Options{ExpiresIn: 90}, expiresIn: 90 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := startAS(t, tt.opts)
			clk := newClock()
			c := &Client{Now: clk.Now}
			cfg := configFor(s, GrantClientCredentials)
			tok, err := c.ClientCredentials(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			if tok.AccessToken == "" || tok.Scheme() != "Bearer" || tok.Scope != cfg.Scope || tok.Fingerprint != cfg.Fingerprint() {
				t.Errorf("token %+v", tok)
			}
			if tt.expiresIn == 0 && !tok.ExpiresAt.IsZero() {
				t.Errorf("ExpiresAt %v, want none", tok.ExpiresAt)
			}
			if tt.expiresIn != 0 && !tok.ExpiresAt.Equal(clk.Now().Add(tt.expiresIn)) {
				t.Errorf("ExpiresAt %v, want in %v", tok.ExpiresAt, tt.expiresIn)
			}
			if n := s.Count("client_credentials"); n != 1 {
				t.Errorf("%d calls", n)
			}
		})
	}
}

func TestExchangeFailures(t *testing.T) {
	respond := func(status int, ctype, body string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", ctype)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}
	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		route   string
		message string
	}{
		{name: "an error answer", handler: respond(400, "application/json", `{"error":"invalid_scope","error_description":"no such scope"}`), route: "Client.exchange.OAuthTokenRejected", message: "invalid_scope (no such scope)"},
		{name: "an error with status 200", handler: respond(200, "application/json", `{"error":"server_error"}`), route: "Client.exchange.OAuthTokenRejected", message: "server_error"},
		{name: "a status with no body", handler: respond(503, "text/plain", `down`), route: "Client.exchange.OAuthTokenRejected", message: "503"},
		{name: "a redirect is not followed", handler: func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", http.StatusFound) }, route: "Client.exchange.OAuthTokenRejected"},
		{name: "not json", handler: respond(200, "text/html", `<html>`), route: "Client.exchange.OAuthTokenResponseInvalid"},
		{name: "no access token", handler: respond(200, "application/json", `{"token_type":"bearer"}`), route: "Client.exchange.OAuthTokenResponseInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer srv.Close()
			cfg := Config{Grant: GrantClientCredentials, TokenURL: srv.URL, ClientID: "c", ClientSecret: "hunter2"}
			_, err := (&Client{}).ClientCredentials(t.Context(), cfg)
			wantRoute(t, err, tt.route)
			if !strings.Contains(err.Error(), tt.message) {
				t.Errorf("message %q lacks %q", err, tt.message)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Errorf("message mentions the secret: %q", err)
			}
		})
	}
}

func TestExchangeTransportAndCancel(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens any more
	cfg := Config{Grant: GrantClientCredentials, TokenURL: url, ClientID: "c"}
	_, err := (&Client{}).ClientCredentials(t.Context(), cfg)
	wantRoute(t, err, "Client.exchange.OAuthTokenRequestFailed")

	_, err = (&Client{}).ClientCredentials(t.Context(), Config{Grant: GrantClientCredentials, TokenURL: "http://127.0.0.1:1/\x7f", ClientID: "c"})
	wantRoute(t, err, "OAuthConfigInvalid")

	s := startAS(t, fakeas.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = (&Client{}).ClientCredentials(ctx, configFor(s, GrantClientCredentials))
	wantRoute(t, err, "Client.exchange.OAuthCanceled")
}

func TestClientSecretIsSentAsBasicAuth(t *testing.T) {
	var gotUser, gotPass string
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		_ = r.ParseForm()
		form = r.PostForm.Encode()
		_, _ = w.Write([]byte(`{"access_token":"a"}`))
	}))
	defer srv.Close()
	cfg := Config{Grant: GrantClientCredentials, TokenURL: srv.URL, ClientID: "id:1", ClientSecret: "s e", Scope: "a b", Audience: "api"}
	if _, err := (&Client{}).ClientCredentials(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if gotUser != "id%3A1" || gotPass != "s+e" {
		t.Errorf("basic auth %q %q", gotUser, gotPass)
	}
	if form != "audience=api&grant_type=client_credentials&scope=a+b" {
		t.Errorf("form %q (the client secret or id must not be in it)", form)
	}
	// a client with no secret puts its id in the form
	cfg.ClientSecret = ""
	if _, err := (&Client{}).ClientCredentials(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if gotUser != "" || !strings.Contains(form, "client_id=id%3A1") {
		t.Errorf("public client: basic %q form %q", gotUser, form)
	}
}

func TestRefresh(t *testing.T) {
	s := startAS(t, fakeas.Options{})
	clk := newClock()
	c := &Client{Now: clk.Now}
	cfg := configFor(s, GrantAuthorizationCode)
	first, err := c.AuthorizationCode(t.Context(), cfg, FlowOptions{Open: fakeas.Browser})
	if err != nil || first.RefreshToken == "" {
		t.Fatalf("first token %+v, %v", first, err)
	}
	second, err := c.Refresh(t.Context(), cfg, first)
	if err != nil {
		t.Fatal(err)
	}
	if second.AccessToken == first.AccessToken || second.RefreshToken == "" || second.RefreshToken == first.RefreshToken {
		t.Errorf("a refresh must give a new access token and the rotated refresh token: %+v", second)
	}
	// the old refresh token was rotated away: it is refused now, and that is its own failure
	_, err = c.Refresh(t.Context(), cfg, first)
	wantRoute(t, err, "Client.exchange.OAuthRefreshRejected")
	if _, err := c.Refresh(t.Context(), Config{Grant: "x"}, first); err == nil {
		t.Error("an invalid configuration was accepted")
	}

	// a server that sends no new refresh token leaves the old one in use
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"access_token":"n"}`)) }))
	defer srv.Close()
	kept, err := c.Refresh(t.Context(), Config{Grant: GrantClientCredentials, TokenURL: srv.URL, ClientID: "c"}, Token{RefreshToken: "old"})
	if err != nil || kept.RefreshToken != "old" {
		t.Errorf("kept %+v, %v", kept, err)
	}
}
