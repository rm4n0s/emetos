package oauth

import (
	"context"
	"encoding/json"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

// Store keeps tokens in the keyring, one for each collection (the owner) and grant (D45). A token is never in a file or in the state.
type Store struct {
	// Secrets is the keyring; nil: nothing is kept.
	Secrets variables.Secrets
	// Folder is the workspace folder, which keeps the tokens of two projects apart.
	Folder string
}

// Key is the key of the token of an owner (a collection; "" for a request that is not in one) and a grant in the keyring.
func (s Store) Key(owner, grant string) string {
	if owner == "" {
		owner = "unsaved"
	}
	return variables.SecretKey(s.Folder, "oauth:"+owner, grant)
}

// Load reads the token; found is false when there is none. It fails with OAuthStoredTokenInvalid when what the keyring holds is not a
// token, and with the keyring's errors.
func (s Store) Load(ctx context.Context, owner, grant string) (tok Token, found bool, err error) {
	if s.Secrets == nil {
		return Token{}, false, nil
	}
	text, ok, err := s.Secrets.Lookup(ctx, s.Key(owner, grant))
	if err != nil || !ok {
		return Token{}, false, err //nolint:wrapcheck // the keyring's own error
	}
	if err := json.Unmarshal([]byte(text), &tok); err != nil || tok.AccessToken == "" {
		return Token{}, false, errors.New("OAuthStoredTokenInvalid", "the token in the keyring cannot be read; get a new one", "grant", grant)
	}
	return tok, true, nil
}

// Save keeps the token, replacing the one there was. It fails with OAuthNoKeyring when there is no keyring.
func (s Store) Save(ctx context.Context, owner, grant string, tok Token) error {
	if s.Secrets == nil {
		return errors.New("OAuthNoKeyring", "there is no keyring to keep the token in")
	}
	data, err := json.Marshal(tok)
	if err != nil {
		return errors.NewErr("OAuthStoredTokenInvalid", err)
	}
	return s.Secrets.Store(ctx, s.Key(owner, grant), "Emetos HTTP Client: OAuth 2.0 token of "+displayOwner(owner)+" ("+grant+")", string(data)) //nolint:wrapcheck // the keyring's own error
}

// Clear removes the token; none is not an error.
func (s Store) Clear(ctx context.Context, owner, grant string) error {
	if s.Secrets == nil {
		return nil
	}
	return s.Secrets.Delete(ctx, s.Key(owner, grant)) //nolint:wrapcheck // the keyring's own error
}

func displayOwner(owner string) string {
	if owner == "" {
		return "an unsaved request"
	}
	return owner
}
