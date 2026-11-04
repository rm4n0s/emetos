package httpclient

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/oauth"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

// OAuth 2.0 (SPEC §10.33 "OAuth 2.0"): this file connects the package oauth to the sends and to the Auth tab (reqoauth.go).

// firstAuth is a when it is set, else b.
func firstAuth(a, b *format.Auth) *format.Auth {
	if a != nil {
		return a
	}
	return b
}

// oauthOwner is the owner of the tokens of a request at place: its collection (a request that is not in one has a shared owner).
func oauthOwner(p place) string {
	if p.collection != nil {
		return p.collection.Owner
	}
	return ""
}

// oauthConfigOf is the configuration of an authorization of type oauth2 with its {{variables}} replaced.
func oauthConfigOf(a *format.Auth, res *variables.Resolver) oauth.Config {
	r := func(s string) string {
		if res == nil {
			return s
		}
		return strings.TrimSpace(res.Resolve(s).Text)
	}
	secret := func(s string) string { // not trimmed: a secret is taken as it is
		if res == nil {
			return s
		}
		return res.Resolve(s).Text
	}
	grant := a.Grant
	if grant == "" {
		grant = oauth.GrantClientCredentials
	}
	return oauth.Config{
		Grant: grant, TokenURL: r(a.TokenURL), AuthURL: r(a.AuthURL), ClientID: r(a.ClientID), ClientSecret: secret(a.ClientSecret),
		Scope: r(a.Scope), Audience: r(a.Audience), PKCE: a.PKCE,
	}
}

// oauthManager makes the manager of tokens for one operation: the requests to the authorization server follow the TLS and proxy settings of
// the editor, and the browser is the desktop's (Workspace().OpenURI) unless a test or the dev command replaced it.
func (e *Extension) oauthManager() *oauth.Manager {
	cfg := e.engineConfig()
	tr, ok := http.DefaultTransport.(*http.Transport)
	transport := http.DefaultTransport
	if ok {
		clone := tr.Clone()
		if cfg.VerifyTLS != nil && !*cfg.VerifyTLS {
			clone.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12} //nolint:gosec // the user turned the check off (httpClient.verifyTls)
		}
		if cfg.Proxy != "" {
			if u, err := url.Parse(cfg.Proxy); err == nil {
				clone.Proxy = http.ProxyURL(u)
			}
		}
		transport = clone
	}
	open := e.browser
	if open == nil {
		open = e.opts.Browser
	}
	if open == nil {
		open = e.api.Workspace().OpenURI
	}
	return &oauth.Manager{
		Client: oauth.Client{
			HTTP: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
			Now:  e.opts.Now,
		},
		Store: oauth.Store{Secrets: e.tokenKeyring(), Folder: e.folder},
		Flow:  oauth.FlowOptions{Open: open, Timeout: e.opts.OAuthTimeout},
	}
}

// oauthTokenFor gives the access token a send uses, with no browser: the stored one, refreshed when it expired. Without a usable token it
// fails with the tag OAuthTokenMissing (authorization code) after the message says what to do; this is also what the runner does
// mid-run (decision 1882). It does I/O and runs off the UI thread.
func (e *Extension) oauthTokenFor(ctx context.Context, a *format.Auth, p place, res *variables.Resolver) (oauth.Token, error) {
	return e.oauthManager().Token(ctx, oauthOwner(p), oauthConfigOf(a, res))
}

// effectiveOAuth is the authorization of type oauth2 that applies to a request with the authorization own at place, or nil.
func effectiveOAuth(own *format.Auth, p place) *format.Auth {
	a := own
	if a == nil || a.Type == "" || a.Type == format.AuthInherit {
		a = p.auth
	}
	if a != nil && a.Type == format.AuthOAuth2 {
		return a
	}
	return nil
}

// tokenKeyring is where tokens are kept: the keyring of the extension.
func (e *Extension) tokenKeyring() variables.Secrets {
	if e.oauthKeyring != nil {
		return e.oauthKeyring
	}
	return e.opts.Keyring
}
