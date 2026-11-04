package oauth

import (
	"context"
	"sync"

	"github.com/rm4n0s/errors"
)

// Manager gets tokens for requests and keeps them in the Store.
type Manager struct {
	Client Client
	Store  Store
	Flow   FlowOptions

	mu sync.Mutex
}

// Stored returns the token kept for the owner and the configuration; found is false when there is none, when it was issued for another
// configuration (other endpoints, client, scope or audience) or when what is kept cannot be read.
func (m *Manager) Stored(ctx context.Context, owner string, cfg Config) (tok Token, found bool, err error) {
	tok, found, err = m.Store.Load(ctx, owner, cfg.Grant)
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "OAuthStoredTokenInvalid" {
			return Token{}, false, nil
		}
		return Token{}, false, err
	}
	if !found || tok.Fingerprint != cfg.Fingerprint() {
		return Token{}, false, nil
	}
	return tok, true, nil
}

// Token returns an access token to send with a request, and opens no browser: the kept token when it has not expired; else it is
// refreshed with its refresh token (and kept); the client credentials grant, which needs no user, asks for a new one when there is none
// or the refresh failed. The authorization code grant with no usable token fails with OAuthTokenMissing, whose message says what to do.
func (m *Manager) Token(ctx context.Context, owner string, cfg Config) (Token, error) {
	if err := cfg.Validate(); err != nil {
		return Token{}, err
	}
	m.mu.Lock() // two sends must not both use (and rotate) the same refresh token
	defer m.mu.Unlock()
	tok, found, err := m.Stored(ctx, owner, cfg)
	if err != nil {
		return Token{}, err
	}
	why := "There is no access token"
	if found {
		if !tok.Expired(m.Client.now()) {
			return tok, nil
		}
		why = "The access token has expired"
		if tok.RefreshToken != "" {
			fresh, rerr := m.Client.Refresh(ctx, cfg, tok)
			if rerr == nil {
				return fresh, m.Store.Save(ctx, owner, cfg.Grant, fresh)
			}
			if appErr, ok := errors.FromError(rerr); !ok || (appErr.Tag != "OAuthRefreshRejected" && appErr.Tag != "OAuthTokenRejected") {
				return Token{}, rerr // the server could not be asked: say so, do not go and ask the user
			}
			why = "The access token has expired and the server refused its refresh token"
		}
	}
	if cfg.Grant == GrantClientCredentials {
		fresh, cerr := m.Client.ClientCredentials(ctx, cfg)
		if cerr != nil {
			return Token{}, cerr
		}
		return fresh, m.Store.Save(ctx, owner, cfg.Grant, fresh)
	}
	return Token{}, errors.New("OAuthTokenMissing", why+": use \"Get New Access Token\" in the Auth tab to sign in again.", "grant", cfg.Grant)
}

// GetNew asks for a new token whatever is kept: the client credentials grant asks the endpoint, the authorization code grant opens the
// browser and waits for the user. The token is kept, replacing the old one. It fails as Client.AuthorizationCode does.
func (m *Manager) GetNew(ctx context.Context, owner string, cfg Config) (Token, error) {
	var tok Token
	var err error
	if cfg.Grant == GrantAuthorizationCode {
		tok, err = m.Client.AuthorizationCode(ctx, cfg, m.Flow)
	} else {
		tok, err = m.Client.ClientCredentials(ctx, cfg)
	}
	if err != nil {
		return Token{}, err
	}
	return tok, m.Store.Save(ctx, owner, cfg.Grant, tok)
}

// Clear removes the kept token of the owner and the grant.
func (m *Manager) Clear(ctx context.Context, owner string, cfg Config) error {
	return m.Store.Clear(ctx, owner, cfg.Grant)
}
