package oauth

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/oauth/fakeas"
)

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want the route %s", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Fatalf("unexpected failure path %s, want it to contain %s", appErr.Route(), route)
	}
}

// clock is a clock the tests move.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// startAS starts a fake authorization server that stops with the test.
func startAS(t *testing.T, opts fakeas.Options) *fakeas.Server {
	t.Helper()
	s, err := fakeas.Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// configFor is a configuration of the server.
func configFor(s *fakeas.Server, grant string) Config {
	return Config{Grant: grant, TokenURL: s.TokenURL, AuthURL: s.AuthURL, ClientID: "client-1", ClientSecret: "secret-1", Scope: "read write", PKCE: true}
}

// watcher is an Opener that follows the URL like a browser and remembers the redirect address of the flow.
type watcher struct {
	mu       sync.Mutex
	redirect string
	opened   int
	// then runs instead of the browser when set.
	then func(ctx context.Context, authURL string) error
}

func (w *watcher) open(ctx context.Context, authURL string) error {
	u, err := url.Parse(authURL)
	if err != nil {
		return err //nolint:wrapcheck // a test
	}
	w.mu.Lock()
	w.redirect = u.Query().Get("redirect_uri")
	w.opened++
	w.mu.Unlock()
	if w.then != nil {
		return w.then(ctx, authURL)
	}
	return fakeas.Browser(ctx, authURL)
}

// assertClosed checks that nothing listens on the redirect address any more.
func (w *watcher) assertClosed(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	redirect := w.redirect
	w.mu.Unlock()
	u, err := url.Parse(redirect)
	if err != nil || u.Host == "" {
		t.Fatalf("no redirect address was seen: %q", redirect)
	}
	if conn, err := net.DialTimeout("tcp", u.Host, time.Second); err == nil {
		_ = conn.Close()
		t.Fatalf("the loopback server still listens on %s", u.Host)
	}
}

// get fetches a URL and returns the status.
func get(ctx context.Context, rawURL string, host string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err //nolint:wrapcheck // a test
	}
	if host != "" {
		req.Host = host
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return 0, err //nolint:wrapcheck // a test
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}
