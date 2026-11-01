package fakeapi

import (
	"encoding/json"
	"sort"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// stateAPI is the extension.State of the fake: the values are kept in memory for the folder of the scope (State, SetState read and
// write them), and survive a Deactivate and a new activation of the same fake (Reactivate-like tests share the *API).

// State implements extension.API.
func (a *API) State() extension.State { return stateKeeper{a} }

type stateKeeper struct{ a *API }

// Get implements extension.State.
func (s stateKeeper) Get(key string) (json.RawMessage, bool) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	v, ok := s.a.state[key]
	return v, ok
}

// Set implements extension.State.
func (s stateKeeper) Set(key string, value any) error {
	a := s.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stateErr != nil {
		return a.stateErr
	}
	if key == "" {
		return errors.New("ExtensionStateInvalid", "a kept value needs a key")
	}
	if value == nil {
		delete(a.state, key)
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return errors.NewErr("ExtensionStateInvalid", err).SetMetadata("key", key)
	}
	if a.state == nil {
		a.state = map[string]json.RawMessage{}
	}
	a.state[key] = b
	return nil
}

// SetState keeps a value as the host would have, before the extension is activated (the choice a user made in an earlier session).
func (a *API) SetState(key string, value any) {
	if err := a.State().Set(key, value); err != nil {
		a.t.Fatalf("SetState(%q): %v", key, err)
	}
}

// StateKeys are the keys that have a value, sorted.
func (a *API) StateKeys() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	keys := make([]string, 0, len(a.state))
	for k := range a.state {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// FailState makes State().Set fail with err from now on; nil makes it work again.
func (a *API) FailState(err error) {
	a.mu.Lock()
	a.stateErr = err
	a.mu.Unlock()
}
