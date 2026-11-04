package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/rm4n0s/errors"
)

// Opener shows the authorization page to the user: the desktop's browser in the editor (Workspace().OpenURI), a fake in the tests.
type Opener func(ctx context.Context, url string) error

// DefaultTimeout is how long the loopback server waits for the user.
const DefaultTimeout = 5 * time.Minute

// CallbackPath is the path of the redirect URI on the loopback address.
const CallbackPath = "/callback"

// FlowOptions are the parts of the authorization code flow a test or the editor sets.
type FlowOptions struct {
	// Open shows the authorization URL to the user; required.
	Open Opener
	// Timeout is how long to wait for the redirect; DefaultTimeout when 0.
	Timeout time.Duration
	// Rand gives the bytes of the state and the PKCE verifier; crypto/rand when nil.
	Rand io.Reader
	// Listen opens the loopback listener; the tests replace it to make listening fail. It listens on 127.0.0.1 and port 0 when nil.
	Listen func(ctx context.Context) (net.Listener, error)
}

// callback is what the redirect carried, checked for its state; AuthorizationCode makes the error of it (so that the route of the error
// is the flow's, not the server goroutine's).
type callback struct {
	code        string
	stateOK     bool
	errCode     string
	description string
}

// AuthorizationCode runs the authorization code grant: it listens on 127.0.0.1 and a random port, opens the authorization URL (with
// the S256 PKCE challenge when cfg.PKCE) and waits for the redirect, checks its state, closes the server, and trades the code for a token.
// The server lives only while it waits: it is closed when the redirect arrives, when the user cancels (ctx), when the time is out and
// when the flow fails. It fails with OAuthConfigInvalid, OAuthRandomFailed, OAuthListenFailed, OAuthBrowserFailed, OAuthStateMismatch,
// OAuthAuthorizationRefused, OAuthCallbackInvalid, OAuthTimeout and OAuthCanceled, and with the errors of Client.ClientCredentials.
func (c *Client) AuthorizationCode(ctx context.Context, cfg Config, fo FlowOptions) (Token, error) {
	if err := cfg.Validate(); err != nil {
		return Token{}, err
	}
	if cfg.Grant != GrantAuthorizationCode {
		return Token{}, errors.New("OAuthConfigInvalid", "the grant is not the authorization code grant", "grant", cfg.Grant)
	}
	src := fo.Rand
	if src == nil {
		src = rand.Reader
	}
	state, err := randomText(src, 16)
	if err != nil {
		return Token{}, errors.NewErr("OAuthRandomFailed", err)
	}
	verifier := ""
	if cfg.PKCE {
		if verifier, err = randomText(src, 32); err != nil {
			return Token{}, errors.NewErr("OAuthRandomFailed", err)
		}
	}
	listen := fo.Listen
	if listen == nil {
		listen = func(ctx context.Context) (net.Listener, error) {
			return (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0") //nolint:wrapcheck // wrapped below
		}
	}
	ln, err := listen(ctx)
	if err != nil {
		return Token{}, errors.NewErr("OAuthListenFailed", err)
	}
	redirect := "http://" + ln.Addr().String() + CallbackPath
	got := make(chan callback, 1)
	srv := &http.Server{Handler: callbackHandler(ln.Addr().String(), state, got), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	closed := false
	closeServer := func() {
		if closed {
			return
		}
		closed = true
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			_ = srv.Close() // a connection that does not finish in time is cut
		}
		_ = ln.Close() // Serve may not have started yet; then nothing else would close the listener
	}
	defer closeServer()

	authURL, err := authorizationURL(cfg, redirect, state, verifier)
	if err != nil {
		return Token{}, err
	}
	if fo.Open == nil {
		return Token{}, errors.New("OAuthBrowserFailed", "there is no way to open the browser")
	}
	if err := fo.Open(ctx, authURL); err != nil {
		return Token{}, errors.NewErr("OAuthBrowserFailed", err).SetMetadata("url", authURL)
	}
	timeout := fo.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var cb callback
	select {
	case cb = <-got:
	case <-ctx.Done():
		return Token{}, errors.New("OAuthCanceled", "getting the token was canceled")
	case <-timer.C:
		return Token{}, errors.New("OAuthTimeout", "the authorization was not completed in time", "seconds", int(timeout/time.Second))
	}
	closeServer()
	switch {
	case !cb.stateOK:
		return Token{}, errors.New("OAuthStateMismatch", "the answer of the authorization server does not carry the state that was sent; it was ignored")
	case cb.errCode != "":
		return Token{}, errors.New("OAuthAuthorizationRefused", "the authorization server refused: "+describe(cb.errCode, cb.description), "error", cb.errCode)
	case cb.code == "":
		return Token{}, errors.New("OAuthCallbackInvalid", "the answer of the authorization server has no code")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {cb.code}, "redirect_uri": {redirect}}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return c.exchange(ctx, cfg, form, false)
}

// randomText is n random bytes in base64url without padding (RFC 7636 §4.1: a verifier of 32 bytes is 43 characters).
func randomText(src io.Reader, n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(src, b); err != nil {
		return "", err //nolint:wrapcheck // the caller wraps it with its tag
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Challenge is the S256 code challenge of a verifier (RFC 7636 §4.2).
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorizationURL is the URL of the authorization page; what the URL of the configuration already has in its query is kept.
func authorizationURL(cfg Config, redirect, state, verifier string) (string, error) {
	u, err := url.Parse(cfg.AuthURL)
	if err != nil {
		return "", errors.NewErr("OAuthConfigInvalid", err).SetMetadata("field", "authUrl")
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirect)
	q.Set("state", state)
	if cfg.Scope != "" {
		q.Set("scope", cfg.Scope)
	}
	if cfg.Audience != "" {
		q.Set("audience", cfg.Audience)
	}
	if verifier != "" {
		q.Set("code_challenge", Challenge(verifier))
		q.Set("code_challenge_method", "S256")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// callbackHandler receives the redirect. Only /callback on the loopback address counts (a request with another Host header, which a
// page that rebinds a name to 127.0.0.1 would make, is refused); other paths (a browser asking for /favicon.ico) are answered and ignored.
func callbackHandler(addr, state string, got chan<- callback) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != addr {
			http.Error(w, "wrong host", http.StatusBadRequest)
			return
		}
		if r.URL.Path != CallbackPath {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		cb := callback{
			code:        q.Get("code"),
			stateOK:     subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) == 1,
			errCode:     q.Get("error"),
			description: q.Get("error_description"),
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if !cb.stateOK || cb.errCode != "" || cb.code == "" {
			reason := "The answer did not carry the state that was sent."
			if cb.stateOK {
				reason = "The authorization server did not give a code."
				if cb.errCode != "" {
					reason = "The authorization server refused: " + html.EscapeString(describe(cb.errCode, cb.description)) + "."
				}
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, page("Authorization failed", reason+" Go back to Emetos."))
		} else {
			_, _ = io.WriteString(w, page("Authorized", "Emetos received the authorization. You can close this tab."))
		}
		select {
		case got <- cb:
		default: // a second answer: the first one counts
		}
	})
}

func page(title, text string) string {
	return "<!doctype html><meta charset=utf-8><title>" + title + "</title><body style=\"font-family:sans-serif;margin:3em\"><h2>" + title + "</h2><p>" + text + "</p>"
}
