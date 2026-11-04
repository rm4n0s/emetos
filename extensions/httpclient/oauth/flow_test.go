package oauth

import (
	"context"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/oauth/fakeas"
)

func TestAuthorizationCodeGrant(t *testing.T) {
	tests := []struct {
		name string
		pkce bool
		// check looks at what the fake server saw.
		scope string
	}{
		{name: "with PKCE", pkce: true, scope: "read write"},
		{name: "without PKCE", pkce: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := startAS(t, fakeas.Options{})
			cfg := configFor(s, GrantAuthorizationCode)
			cfg.PKCE, cfg.Scope = tt.pkce, tt.scope
			w := &watcher{}
			var authURL string
			w.then = func(ctx context.Context, u string) error { authURL = u; return fakeas.Browser(ctx, u) }
			tok, err := (&Client{}).AuthorizationCode(t.Context(), cfg, FlowOptions{Open: w.open})
			if err != nil {
				t.Fatal(err)
			}
			if tok.AccessToken == "" || tok.RefreshToken == "" || tok.Fingerprint != cfg.Fingerprint() {
				t.Errorf("token %+v", tok)
			}
			w.assertClosed(t)

			u, _ := url.Parse(authURL)
			q := u.Query()
			if q.Get("response_type") != "code" || q.Get("client_id") != "client-1" || q.Get("scope") != tt.scope || q.Get("state") == "" {
				t.Errorf("authorization query %v", q)
			}
			if !strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") || !strings.HasSuffix(q.Get("redirect_uri"), "/callback") {
				t.Errorf("redirect_uri %q", q.Get("redirect_uri"))
			}
			reqs := s.Requests()
			last := reqs[len(reqs)-1]
			if last.Grant != "authorization_code" {
				t.Fatalf("last call %+v", last)
			}
			if tt.pkce {
				if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("code_challenge") != Challenge(last.Verifier) {
					t.Errorf("challenge %q method %q for the verifier %q", q.Get("code_challenge"), q.Get("code_challenge_method"), last.Verifier)
				}
				if !last.PKCEOK || len(last.Verifier) != 43 {
					t.Errorf("the server did not accept the verifier: %+v", last)
				}
			} else if q.Get("code_challenge") != "" || last.Verifier != "" {
				t.Errorf("PKCE was off but %q / %q was sent", q.Get("code_challenge"), last.Verifier)
			}
		})
	}
}

func TestChallengeIsRFC7636(t *testing.T) {
	// the example of RFC 7636 appendix B
	if got := Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Errorf("challenge %q", got)
	}
}

func TestAuthorizationCodeFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(s *fakeas.Server) (FlowOptions, *watcher, func() context.Context)
		route string
		text  string
	}{
		{
			name: "consent denied",
			setup: func(s *fakeas.Server) (FlowOptions, *watcher, func() context.Context) {
				s.Deny(true)
				w := &watcher{}
				return FlowOptions{Open: w.open}, w, nil
			},
			route: "Client.AuthorizationCode.OAuthAuthorizationRefused", text: "access_denied (the user denied the request)",
		},
		{
			name: "state mismatch",
			setup: func(s *fakeas.Server) (FlowOptions, *watcher, func() context.Context) {
				s.BadState(true)
				w := &watcher{}
				return FlowOptions{Open: w.open}, w, nil
			},
			route: "Client.AuthorizationCode.OAuthStateMismatch",
		},
		{
			name: "no code",
			setup: func(*fakeas.Server) (FlowOptions, *watcher, func() context.Context) {
				w := &watcher{then: func(ctx context.Context, authURL string) error {
					u, _ := url.Parse(authURL)
					q := url.Values{"state": {u.Query().Get("state")}}
					_, err := get(ctx, u.Query().Get("redirect_uri")+"?"+q.Encode(), "")
					return err
				}}
				return FlowOptions{Open: w.open}, w, nil
			},
			route: "Client.AuthorizationCode.OAuthCallbackInvalid",
		},
		{
			name: "timeout",
			setup: func(*fakeas.Server) (FlowOptions, *watcher, func() context.Context) {
				w := &watcher{then: func(context.Context, string) error { return nil }} // the user never comes back
				return FlowOptions{Open: w.open, Timeout: 50 * time.Millisecond}, w, nil
			},
			route: "Client.AuthorizationCode.OAuthTimeout",
		},
		{
			name: "canceled",
			setup: func(*fakeas.Server) (FlowOptions, *watcher, func() context.Context) {
				ctx, cancel := context.WithCancel(context.Background())
				w := &watcher{then: func(context.Context, string) error { cancel(); return nil }}
				return FlowOptions{Open: w.open}, w, func() context.Context { return ctx }
			},
			route: "Client.AuthorizationCode.OAuthCanceled",
		},
		{
			name: "the browser cannot be opened",
			setup: func(*fakeas.Server) (FlowOptions, *watcher, func() context.Context) {
				w := &watcher{then: func(context.Context, string) error { return errors.New("Fake", "no browser") }}
				return FlowOptions{Open: w.open}, w, nil
			},
			route: "Client.AuthorizationCode.OAuthBrowserFailed", text: "no browser",
		},
		{
			name: "no opener",
			setup: func(*fakeas.Server) (FlowOptions, *watcher, func() context.Context) {
				w := &watcher{redirect: "http://127.0.0.1:1/callback"} // the port is not asked: the test of the closed server is skipped below
				return FlowOptions{}, w, nil
			},
			route: "Client.AuthorizationCode.OAuthBrowserFailed",
		},
		{
			name: "the code is refused",
			setup: func(*fakeas.Server) (FlowOptions, *watcher, func() context.Context) {
				w := &watcher{then: func(ctx context.Context, authURL string) error {
					// a code the server never issued
					u, _ := url.Parse(authURL)
					q := url.Values{"state": {u.Query().Get("state")}, "code": {"forged"}}
					_, err := get(ctx, u.Query().Get("redirect_uri")+"?"+q.Encode(), "")
					return err
				}}
				return FlowOptions{Open: w.open}, w, nil
			},
			route: "Client.exchange.OAuthTokenRejected", text: "invalid_grant",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := startAS(t, fakeas.Options{})
			fo, w, ctxOf := tt.setup(s)
			ctx := t.Context()
			if ctxOf != nil {
				ctx = ctxOf()
			}
			_, err := (&Client{}).AuthorizationCode(ctx, configFor(s, GrantAuthorizationCode), fo)
			wantRoute(t, err, tt.route)
			if tt.text != "" && !strings.Contains(err.Error(), tt.text) {
				t.Errorf("message %q lacks %q", err, tt.text)
			}
			if tt.name != "no opener" {
				w.assertClosed(t) // closed on every way out
			}
		})
	}
}

func TestAuthorizationCodeSetupFailures(t *testing.T) {
	s := startAS(t, fakeas.Options{})
	cfg := configFor(s, GrantAuthorizationCode)
	t.Run("the grant", func(t *testing.T) {
		c := cfg
		c.Grant = GrantClientCredentials
		_, err := (&Client{}).AuthorizationCode(t.Context(), c, FlowOptions{Open: fakeas.Browser})
		wantRoute(t, err, "Client.AuthorizationCode.OAuthConfigInvalid")
	})
	t.Run("the configuration", func(t *testing.T) {
		c := cfg
		c.AuthURL = ""
		_, err := (&Client{}).AuthorizationCode(t.Context(), c, FlowOptions{Open: fakeas.Browser})
		wantRoute(t, err, "Config.Validate.OAuthConfigInvalid")
	})
	t.Run("random bytes for the state", func(t *testing.T) {
		_, err := (&Client{}).AuthorizationCode(t.Context(), cfg, FlowOptions{Open: fakeas.Browser, Rand: strings.NewReader("")})
		wantRoute(t, err, "Client.AuthorizationCode.OAuthRandomFailed")
	})
	t.Run("random bytes for the verifier", func(t *testing.T) {
		_, err := (&Client{}).AuthorizationCode(t.Context(), cfg, FlowOptions{Open: fakeas.Browser, Rand: io.LimitReader(strings.NewReader(strings.Repeat("x", 100)), 16)})
		wantRoute(t, err, "Client.AuthorizationCode.OAuthRandomFailed")
	})
	t.Run("listening", func(t *testing.T) {
		fail := func(context.Context) (net.Listener, error) { return nil, errors.New("FakeNoPort", "no port") }
		_, err := (&Client{}).AuthorizationCode(t.Context(), cfg, FlowOptions{Open: fakeas.Browser, Listen: fail})
		wantRoute(t, err, "Client.AuthorizationCode.OAuthListenFailed")
	})
	t.Run("an authorization URL that cannot be parsed", func(t *testing.T) {
		_, err := authorizationURL(Config{AuthURL: "http://127.0.0.1:x/\x7f"}, "r", "s", "")
		wantRoute(t, err, "authorizationURL.OAuthConfigInvalid")
	})
}

// TestCallbackIgnoresStrangers: a request for another path, with another Host header, or a second answer does not end the wait nor
// change its result.
func TestCallbackIgnoresStrangers(t *testing.T) {
	s := startAS(t, fakeas.Options{})
	w := &watcher{then: func(ctx context.Context, authURL string) error {
		u, _ := url.Parse(authURL)
		redirect := u.Query().Get("redirect_uri")
		base := strings.TrimSuffix(redirect, "/callback")
		if code, err := get(ctx, base+"/favicon.ico", ""); err != nil || code != 404 {
			t.Errorf("favicon: %d %v", code, err)
		}
		q := url.Values{"state": {u.Query().Get("state")}, "code": {"x"}}
		if code, err := get(ctx, redirect+"?"+q.Encode(), "evil.example:80"); err != nil || code != 400 {
			t.Errorf("another Host: %d %v", code, err)
		}
		return fakeas.Browser(ctx, authURL) // the real answer comes after them
	}}
	tok, err := (&Client{}).AuthorizationCode(t.Context(), configFor(s, GrantAuthorizationCode), FlowOptions{Open: w.open})
	if err != nil || tok.AccessToken == "" {
		t.Fatalf("%+v %v", tok, err)
	}
	w.assertClosed(t)
}

func TestAuthorizationURLKeepsItsQuery(t *testing.T) {
	cfg := Config{AuthURL: "https://as.example/auth?tenant=t1&prompt=login", ClientID: "c", Audience: "api"}
	got, err := authorizationURL(cfg, "http://127.0.0.1:9/callback", "st", "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(got)
	q := u.Query()
	if q.Get("tenant") != "t1" || q.Get("prompt") != "login" || q.Get("audience") != "api" || q.Get("code_challenge") != "" {
		t.Errorf("%s", got)
	}
}
