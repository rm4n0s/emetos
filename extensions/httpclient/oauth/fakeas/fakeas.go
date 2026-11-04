// Package fakeas is a small OAuth 2.0 authorization server on 127.0.0.1 for the tests of the HTTP client and for its dev proof
// (SPEC §15.2). It issues tokens for the client credentials, authorization code (with PKCE, checked) and refresh token grants. It is
// not a server for anything real: no consent page, the authorization endpoint redirects at once.
package fakeas

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/rm4n0s/errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Options configure the server; zero values give a working server for the client "client-1" with the secret "secret-1".
type Options struct {
	ClientID, ClientSecret string
	// ExpiresIn is the lifetime of an access token in seconds; 3600 when 0, and none is sent when negative.
	ExpiresIn int
	// NoRefreshToken makes the authorization code grant issue no refresh token.
	NoRefreshToken bool
	// Listen opens the listener; 127.0.0.1 and a random port when nil (a test makes it fail).
	Listen func() (net.Listener, error)
}

// Request is a call the server saw.
type Request struct {
	Path string
	// Grant is the grant_type of a call to the token endpoint.
	Grant string
	// Verifier and Challenge are the PKCE values of a token call and of an authorization call; PKCEOK says that they matched.
	Verifier, Challenge string
	PKCEOK              bool
	Code, State         string
}

// Server is the authorization server.
type Server struct {
	// URL is the base address, "http://127.0.0.1:<port>"; AuthURL and TokenURL are its endpoints.
	URL, AuthURL, TokenURL string

	opts Options
	ln   net.Listener
	srv  *http.Server

	mu       sync.Mutex
	deny     bool
	badState bool
	codes    map[string]code
	refresh  map[string]bool
	seq      int
	reqs     []Request
}

type code struct{ challenge, method, redirect, client string }

// Start starts a server on 127.0.0.1 and a random port.
func Start(opts Options) (*Server, error) {
	if opts.ClientID == "" {
		opts.ClientID, opts.ClientSecret = "client-1", "secret-1"
	}
	listen := opts.Listen
	if listen == nil {
		listen = func() (net.Listener, error) {
			return (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		} //nolint:wrapcheck // wrapped below
	}
	ln, err := listen()
	if err != nil {
		return nil, errors.NewErr("FakeASListenFailed", err)
	}
	s := &Server{opts: opts, ln: ln, codes: map[string]code{}, refresh: map[string]bool{}}
	s.URL = "http://" + ln.Addr().String()
	s.AuthURL, s.TokenURL = s.URL+"/authorize", s.URL+"/token"
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", s.authorize)
	mux.HandleFunc("/token", s.token)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

// Close stops the server.
func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.srv.Shutdown(ctx); err != nil {
		_ = s.srv.Close()
	}
}

// Deny makes the authorization endpoint answer with error=access_denied (the user refused the consent).
func (s *Server) Deny(on bool) { s.mu.Lock(); s.deny = on; s.mu.Unlock() }

// BadState makes the authorization endpoint answer with a state it was not given.
func (s *Server) BadState(on bool) { s.mu.Lock(); s.badState = on; s.mu.Unlock() }

// RevokeRefreshTokens makes every refresh token issued so far invalid (the refresh token expired).
func (s *Server) RevokeRefreshTokens() { s.mu.Lock(); s.refresh = map[string]bool{}; s.mu.Unlock() }

// Requests are the calls seen so far, oldest first.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// Count is how many calls to the token endpoint had the grant.
func (s *Server) Count(grant string) int {
	n := 0
	for _, r := range s.Requests() {
		if r.Path == "/token" && r.Grant == grant {
			n++
		}
	}
	return n
}

// Browser follows an authorization URL the way a browser does with a user who agrees: the request, the redirect to the loopback server
// and its answer. It is an oauth.Opener.
func Browser(ctx context.Context, authURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return err //nolint:wrapcheck // a fake for tests
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err //nolint:wrapcheck // a fake for tests
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close() //nolint:wrapcheck // a fake for tests
}

func (s *Server) next(prefix string) string {
	s.seq++
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + strconv.Itoa(s.seq) + "-" + hex.EncodeToString(b[:])
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	host := ""
	if err == nil {
		host = redirect.Hostname()
	}
	if q.Get("response_type") != "code" || q.Get("client_id") != s.opts.ClientID || host != "127.0.0.1" {
		http.Error(w, "invalid authorization request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := q.Get("state")
	if s.badState {
		state = "not-the-state"
	}
	rq := redirect.Query()
	rq.Set("state", state)
	rec := Request{Path: "/authorize", Challenge: q.Get("code_challenge"), State: q.Get("state")}
	if s.deny {
		rq.Set("error", "access_denied")
		rq.Set("error_description", "the user denied the request")
	} else {
		c := s.next("code-")
		s.codes[c] = code{challenge: q.Get("code_challenge"), method: q.Get("code_challenge_method"), redirect: q.Get("redirect_uri"), client: q.Get("client_id")}
		rq.Set("code", c)
		rec.Code = c
	}
	s.reqs = append(s.reqs, rec)
	redirect.RawQuery = rq.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		fail(w, http.StatusBadRequest, "invalid_request", "the form cannot be read")
		return
	}
	id, secret, basic := r.BasicAuth()
	if basic {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id = r.PostForm.Get("client_id")
	}
	if id != s.opts.ClientID || (basic && secret != s.opts.ClientSecret) || (!basic && s.opts.ClientSecret != "" && r.PostForm.Get("client_secret") != s.opts.ClientSecret) {
		fail(w, http.StatusUnauthorized, "invalid_client", "the client is not known")
		return
	}
	grant := r.PostForm.Get("grant_type")
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := Request{Path: "/token", Grant: grant}
	defer func() { s.reqs = append(s.reqs, rec) }()
	withRefresh := false
	switch grant {
	case "client_credentials":
	case "authorization_code":
		c, ok := s.codes[r.PostForm.Get("code")]
		delete(s.codes, r.PostForm.Get("code")) // a code is used once
		rec.Code, rec.Verifier, rec.Challenge = r.PostForm.Get("code"), r.PostForm.Get("code_verifier"), c.challenge
		if !ok || c.redirect != r.PostForm.Get("redirect_uri") || c.client != id {
			fail(w, http.StatusBadRequest, "invalid_grant", "the code is not valid")
			return
		}
		if c.challenge != "" {
			sum := sha256.Sum256([]byte(rec.Verifier))
			rec.PKCEOK = c.method == "S256" && rec.Verifier != "" && base64.RawURLEncoding.EncodeToString(sum[:]) == c.challenge
			if !rec.PKCEOK {
				fail(w, http.StatusBadRequest, "invalid_grant", "the code verifier does not match")
				return
			}
		}
		withRefresh = !s.opts.NoRefreshToken
	case "refresh_token":
		rt := r.PostForm.Get("refresh_token")
		if !s.refresh[rt] {
			fail(w, http.StatusBadRequest, "invalid_grant", "the refresh token is not valid")
			return
		}
		delete(s.refresh, rt) // refresh tokens rotate
		withRefresh = true
	default:
		fail(w, http.StatusBadRequest, "unsupported_grant_type", "the grant is not built")
		return
	}
	out := map[string]any{"access_token": s.next("at-"), "token_type": "Bearer"}
	if s.opts.ExpiresIn >= 0 {
		exp := s.opts.ExpiresIn
		if exp == 0 {
			exp = 3600
		}
		out["expires_in"] = exp
	}
	if withRefresh {
		rt := s.next("rt-")
		s.refresh[rt] = true
		out["refresh_token"] = rt
	}
	if sc := r.PostForm.Get("scope"); sc != "" {
		out["scope"] = sc
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

func fail(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": desc})
}
