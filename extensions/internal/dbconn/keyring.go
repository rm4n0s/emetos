package dbconn

import (
	"context"
	"sync"
)

// Keyring is where a password that the user asked to save lives: the desktop's keyring. SecretService is the one Emetos uses;
// the tests of the other pieces use MemoryKeyring.
type Keyring interface {
	// Store keeps the secret for the connection, replacing the one it had. label is what the keyring's own tools show.
	Store(ctx context.Context, connectionID, label, secret string) error
	// Lookup returns the secret of the connection; found is false when there is none. A locked keyring is unlocked first, which
	// may ask the user (the service's own prompt).
	Lookup(ctx context.Context, connectionID string) (secret string, found bool, err error)
	// Delete removes the secret of the connection; a secret that is not there is not an error.
	Delete(ctx context.Context, connectionID string) error
}

// MemoryKeyring is a Keyring that keeps the secrets in memory. It is for tests, and for the place of a keyring that fails.
type MemoryKeyring struct {
	mu      sync.Mutex
	secrets map[string]string
	labels  map[string]string
	// Err, when set, is returned by every call: a keyring that fails.
	Err error
}

var _ Keyring = (*MemoryKeyring)(nil)

// Store implements Keyring.
func (k *MemoryKeyring) Store(_ context.Context, connectionID, label, secret string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Err != nil {
		return k.Err
	}
	if k.secrets == nil {
		k.secrets, k.labels = map[string]string{}, map[string]string{}
	}
	k.secrets[connectionID], k.labels[connectionID] = secret, label
	return nil
}

// Lookup implements Keyring.
func (k *MemoryKeyring) Lookup(_ context.Context, connectionID string) (string, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Err != nil {
		return "", false, k.Err
	}
	s, ok := k.secrets[connectionID]
	return s, ok, nil
}

// Delete implements Keyring.
func (k *MemoryKeyring) Delete(_ context.Context, connectionID string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Err != nil {
		return k.Err
	}
	delete(k.secrets, connectionID)
	delete(k.labels, connectionID)
	return nil
}

// Label is the label the secret of the connection was stored with.
func (k *MemoryKeyring) Label(connectionID string) string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.labels[connectionID]
}

// Len is how many secrets it holds.
func (k *MemoryKeyring) Len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.secrets)
}
