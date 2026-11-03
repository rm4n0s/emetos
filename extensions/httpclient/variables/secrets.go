package variables

import (
	"context"
	"strings"
)

// Secrets is where the values of secret variables live: the desktop keyring. dbconn.Keyring has the same methods, so the
// SecretService and the MemoryKeyring of the database extensions are Secrets.
type Secrets interface {
	// Store keeps the secret under the key, replacing the one it had. label is what the keyring's own tools show.
	Store(ctx context.Context, key, label, secret string) error
	// Lookup returns the secret; found is false when there is none.
	Lookup(ctx context.Context, key string) (secret string, found bool, err error)
	// Delete removes the secret; one that is not there is not an error.
	Delete(ctx context.Context, key string) error
}

// The owners of variables, which keep the secrets of two places apart.
const (
	// GlobalOwner owns the globals.
	GlobalOwner = "global"
)

// EnvironmentOwner is the owner of the variables of the environment whose key is key (the file name without its extension).
func EnvironmentOwner(key string) string { return "environment:" + key }

// CollectionOwner is the owner of the variables of the collection whose key is key (its folder name).
func CollectionOwner(key string) string { return "collection:" + key }

// SecretKey is the key of the secret of a variable in the keyring: the workspace folder, the owner and the name (SPEC §10.33).
func SecretKey(folder, owner, name string) string {
	return "http-client|" + folder + "|" + owner + "|" + name
}

// secretLabel is what the keyring's tools show for the secret.
func secretLabel(owner, name string) string {
	return "Emetos HTTP Client: " + strings.TrimSpace(owner) + " " + name
}
