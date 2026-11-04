package oauth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// The grants that are built. The password and implicit grants are not (deprecated by OAuth 2.1).
const (
	GrantClientCredentials = "client_credentials"
	GrantAuthorizationCode = "authorization_code"
)

// Config is an authorization with its variables replaced.
type Config struct {
	Grant        string
	TokenURL     string
	AuthURL      string
	ClientID     string
	ClientSecret string
	Scope        string
	Audience     string
	// PKCE adds the S256 challenge to the authorization request (authorization code only).
	PKCE bool
}

// Validate checks what the grant needs. It fails with OAuthConfigInvalid.
func (c Config) Validate() error {
	httpURL := func(raw string) bool {
		u, err := url.Parse(raw)
		return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
	}
	switch c.Grant {
	case GrantClientCredentials, GrantAuthorizationCode:
	default:
		return errors.New("OAuthConfigInvalid", "the grant type is not built: use client credentials or authorization code", "grant", c.Grant)
	}
	if !httpURL(c.TokenURL) {
		return errors.New("OAuthConfigInvalid", "the access token URL must be an http or https address", "field", "tokenUrl")
	}
	if c.ClientID == "" {
		return errors.New("OAuthConfigInvalid", "the client ID is empty", "field", "clientId")
	}
	if c.Grant == GrantAuthorizationCode && !httpURL(c.AuthURL) {
		return errors.New("OAuthConfigInvalid", "the authorization URL must be an http or https address", "field", "authUrl")
	}
	return nil
}

// Fingerprint identifies the authorization a token was issued for (not the secret): a token is used only for the same grant, endpoints,
// client, scope and audience.
func (c Config) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{c.Grant, c.TokenURL, c.AuthURL, c.ClientID, c.Scope, c.Audience}, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// Token is what the authorization server issued.
type Token struct {
	AccessToken  string `json:"accessToken"`
	TokenType    string `json:"tokenType,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	// ExpiresAt is zero for a token that came with no lifetime: it is used until the server refuses it.
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
	Scope     string    `json:"scope,omitempty"`
	// Fingerprint is the Fingerprint of the Config the token was issued for.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// expirySkew is how long before its end a token counts as expired, so that it does not end on the way.
const expirySkew = 30 * time.Second

// Expired says whether the token has ended (or ends within the skew) at now.
func (t Token) Expired(now time.Time) bool {
	return !t.ExpiresAt.IsZero() && !now.Before(t.ExpiresAt.Add(-expirySkew))
}

// Scheme is the word before the token in an Authorization header: "Bearer" for a token of type bearer (in any case) or of no type.
func (t Token) Scheme() string {
	if t.TokenType == "" || strings.EqualFold(t.TokenType, "bearer") {
		return "Bearer"
	}
	return t.TokenType
}
