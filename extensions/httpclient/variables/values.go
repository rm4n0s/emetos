package variables

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// State is the part of extension.State the current values use.
type State interface {
	Get(key string) (json.RawMessage, bool)
	Set(key string, value any) error
}

// StateKey is the key of the current values in the state of the workspace.
const StateKey = "httpclient.currentValues"

// The scopes that have current values (an extraction rule's "scope"; the globals have one value, which is theirs).
const (
	ScopeEnvironment = "environment"
	ScopeCollection  = "collection"
)

// stored is what StateKey holds: the current values of each environment and collection, by owner.
type stored struct {
	Owners map[string]map[string]string `json:"owners"`
}

// Values keeps the current values (SPEC §10.33 "Current values"): the value a variable has on this computer, apart from the
// initial value in its file. A current value is in the state of the workspace (Emetos's state, not the project). The current
// value of a secret variable is its secret: it goes to the keyring and nowhere else.
type Values struct {
	folder  string
	state   State
	secrets Secrets

	mu sync.Mutex
}

// NewValues makes the current values of the workspace folder, kept in state, with the secrets in secrets (nil: secrets are not
// kept, and their reads find nothing).
func NewValues(folder string, state State, secrets Secrets) *Values {
	return &Values{folder: folder, state: state, secrets: secrets}
}

func (v *Values) load() stored {
	var s stored
	if raw, ok := v.state.Get(StateKey); ok {
		if json.Unmarshal(raw, &s) != nil {
			s = stored{}
		}
	}
	if s.Owners == nil {
		s.Owners = map[string]map[string]string{}
	}
	return s
}

// Get returns the current value of the variable of the owner, and whether it has one.
func (v *Values) Get(owner, name string) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	val, ok := v.load().Owners[owner][name]
	return val, ok
}

// All returns the current values of the owner (a copy).
func (v *Values) All(owner string) map[string]string {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := map[string]string{}
	for k, val := range v.load().Owners[owner] {
		out[k] = val
	}
	return out
}

// Set makes value the current value of the variable of the owner. For a secret variable it is stored in the keyring (the
// state gets nothing). The state's errors (ExtensionStateInvalid, ...) and the keyring's are returned as they are; it fails with
// HttpSecretsUnavailable for a secret when there is no keyring.
func (v *Values) Set(ctx context.Context, owner, name, value string, secret bool) error {
	if secret {
		if v.secrets == nil {
			return errors.New("HttpSecretsUnavailable", "there is no keyring for the secret", "name", name)
		}
		return v.secrets.Store(ctx, SecretKey(v.folder, owner, name), secretLabel(owner, name), value)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	s := v.load()
	if s.Owners[owner] == nil {
		s.Owners[owner] = map[string]string{}
	}
	s.Owners[owner][name] = value
	return v.state.Set(StateKey, s)
}

// Reset takes the current value of the variable away ("Reset" puts the initial value back); for a secret it removes the secret.
func (v *Values) Reset(ctx context.Context, owner, name string, secret bool) error {
	if secret {
		if v.secrets == nil {
			return nil
		}
		return v.secrets.Delete(ctx, SecretKey(v.folder, owner, name))
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	s := v.load()
	if _, ok := s.Owners[owner][name]; !ok {
		return nil
	}
	delete(s.Owners[owner], name)
	if len(s.Owners[owner]) == 0 {
		delete(s.Owners, owner)
	}
	return v.state.Set(StateKey, s)
}

// ResetOwner takes every current value of the owner away (not the secrets).
func (v *Values) ResetOwner(owner string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	s := v.load()
	if _, ok := s.Owners[owner]; !ok {
		return nil
	}
	delete(s.Owners, owner)
	return v.state.Set(StateKey, s)
}

// Secret returns the secret of a variable; found is false when the keyring has none.
func (v *Values) Secret(ctx context.Context, owner, name string) (secret string, found bool, err error) {
	if v.secrets == nil {
		return "", false, nil
	}
	return v.secrets.Lookup(ctx, SecretKey(v.folder, owner, name))
}

// DeleteSecrets removes the secrets of the variables from the keyring (the owner was deleted, or the variables were).
func (v *Values) DeleteSecrets(ctx context.Context, owner string, names ...string) error {
	if v.secrets == nil {
		return nil
	}
	for _, n := range names {
		if err := v.secrets.Delete(ctx, SecretKey(v.folder, owner, n)); err != nil {
			return err
		}
	}
	return nil
}

// Persist returns the variables with the current values written as their values ("Persist" of SPEC §10.33); the caller saves
// them into the file. A variable without a current value keeps its value. It fails with HttpPersistSecret when a variable that has
// a current value is a secret: a secret is never written to a file.
func Persist(vars []format.Variable, current map[string]string) ([]format.Variable, error) {
	out := make([]format.Variable, len(vars))
	copy(out, vars)
	for i, vr := range out {
		cur, ok := current[vr.Name]
		if !ok {
			continue
		}
		if vr.Secret {
			return nil, errors.New("HttpPersistSecret", "a secret variable is never written to a file", "name", vr.Name)
		}
		out[i].Value = cur
	}
	return out, nil
}
