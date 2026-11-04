package engine

import (
	"net/http"
	"net/url"

	"github.com/rm4n0s/errors"
)

// OAuthToken is an access token to send with a request (OAuth 2.0, SPEC §10.33).
type OAuthToken struct {
	// Value is the access token.
	Value string
	// Scheme is the word before it in the Authorization header; "Bearer" when empty.
	Scheme string
}

// applyOAuth puts the token where addTo says: the Authorization header ("header", the default) or the query parameter access_token.
func (p *Prepared) applyOAuth(addTo string, tok *OAuthToken) error {
	if tok == nil || tok.Value == "" {
		return errors.New("EngineOAuthTokenMissing", "the request needs an OAuth 2.0 access token and has none; use \"Get New Access Token\" in the Auth tab")
	}
	switch addTo {
	case "", "header":
		scheme := tok.Scheme
		if scheme == "" {
			scheme = "Bearer"
		}
		p.Header.Set("Authorization", scheme+" "+tok.Value)
		addOnce(&p.authHeaders, http.CanonicalHeaderKey("Authorization"))
	case "query":
		addQuery(p.URL, []string{url.QueryEscape("access_token") + "=" + url.QueryEscape(tok.Value)})
	default:
		return errors.New("EngineAuthInvalid", "the OAuth 2.0 token goes in \"header\" or \"query\"", "addTo", addTo)
	}
	return nil
}
