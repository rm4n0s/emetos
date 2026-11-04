package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// maxTokenResponse is the most of an answer of the token endpoint that is read.
const maxTokenResponse = 1 << 20

// Client asks token endpoints for tokens.
type Client struct {
	// HTTP sends the requests; a client with a timeout of 30 seconds that does not follow redirects when nil. A redirect of a token
	// endpoint is never followed: the client secret would go to another address.
	HTTP *http.Client
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// ClientCredentials asks for a token with the client credentials grant. It fails with OAuthConfigInvalid, OAuthTokenRequestFailed,
// OAuthTokenRejected and OAuthTokenResponseInvalid.
func (c *Client) ClientCredentials(ctx context.Context, cfg Config) (Token, error) {
	if err := cfg.Validate(); err != nil {
		return Token{}, err
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	addScope(form, cfg)
	return c.exchange(ctx, cfg, form, false)
}

// Refresh asks for a new access token with the refresh token of tok. The server may send a new refresh token; when it does not the old
// one is kept. It fails as ClientCredentials does, and with OAuthRefreshRejected when the server refuses the refresh token (it
// expired or was revoked): the caller asks the user again.
func (c *Client) Refresh(ctx context.Context, cfg Config, tok Token) (Token, error) {
	if err := cfg.Validate(); err != nil {
		return Token{}, err
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}}
	addScope(form, cfg)
	out, err := c.exchange(ctx, cfg, form, true)
	if err != nil {
		return Token{}, err
	}
	if out.RefreshToken == "" {
		out.RefreshToken = tok.RefreshToken
	}
	return out, nil
}

func addScope(form url.Values, cfg Config) {
	if cfg.Scope != "" {
		form.Set("scope", cfg.Scope)
	}
	if cfg.Audience != "" {
		form.Set("audience", cfg.Audience)
	}
}

// exchange posts the form to the token endpoint with the client's authentication (HTTP Basic with the secret, RFC 6749 §2.3.1; the
// client ID in the form for a client with none) and reads the token from the answer.
func (c *Client) exchange(ctx context.Context, cfg Config, form url.Values, refresh bool) (Token, error) {
	if cfg.ClientSecret == "" {
		form.Set("client_id", cfg.ClientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, errors.NewErr("OAuthTokenRequestFailed", err).SetMetadata("url", cfg.TokenURL)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if cfg.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(cfg.ClientID), url.QueryEscape(cfg.ClientSecret))
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Token{}, errors.New("OAuthCanceled", "getting the token was canceled")
		}
		return Token{}, errors.NewErr("OAuthTokenRequestFailed", err).SetMetadata("url", cfg.TokenURL)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponse))
	if err != nil {
		return Token{}, errors.NewErr("OAuthTokenRequestFailed", err).SetMetadata("url", cfg.TokenURL)
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    any    `json:"expires_in"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	decodeErr := json.Unmarshal(data, &body)
	if resp.StatusCode/100 != 2 || (decodeErr == nil && body.Error != "") {
		what := body.Error
		if what == "" {
			what = resp.Status
		}
		if refresh && body.Error == "invalid_grant" {
			return Token{}, errors.New("OAuthRefreshRejected", "the server refused the refresh token: "+describe(what, body.Description),
				"error", body.Error)
		}
		return Token{}, errors.New("OAuthTokenRejected", "the server refused the token request: "+describe(what, body.Description),
			"status", resp.StatusCode, "error", body.Error)
	}
	if decodeErr != nil {
		return Token{}, errors.New("OAuthTokenResponseInvalid", "the answer of the token endpoint is not JSON", "status", resp.StatusCode)
	}
	if body.AccessToken == "" {
		return Token{}, errors.New("OAuthTokenResponseInvalid", "the answer of the token endpoint has no access_token", "status", resp.StatusCode)
	}
	tok := Token{AccessToken: body.AccessToken, TokenType: body.TokenType, RefreshToken: body.RefreshToken, Scope: body.Scope, Fingerprint: cfg.Fingerprint()}
	if secs, ok := seconds(body.ExpiresIn); ok && secs > 0 {
		tok.ExpiresAt = c.now().Add(time.Duration(secs * float64(time.Second)))
	}
	return tok, nil
}

// describe joins an error code and its description.
func describe(code, desc string) string {
	if desc == "" {
		return code
	}
	return code + " (" + desc + ")"
}

// seconds reads expires_in, which servers send as a number or as a string.
func seconds(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		var f float64
		if err := json.Unmarshal([]byte(x), &f); err == nil {
			return f, true
		}
	}
	return 0, false
}
