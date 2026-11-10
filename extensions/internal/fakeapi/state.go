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
func (a *API) State() extension.State { return stateKeeper{a: a} }

// GlobalState implements extension.API: values of their own, kept in memory like those of State.
func (a *API) GlobalState() extension.State { return stateKeeper{a: a, global: true} }

type stateKeeper struct {
	a      *API
	global bool
}

// values is the map of the keeper, made when it is written first. The lock is held.
func (s stateKeeper) values(make_ bool) map[string]json.RawMessage {
	m := &s.a.state
	if s.global {
		m = &s.a.globalState
	}
	if *m == nil && make_ {
		*m = map[string]json.RawMessage{}
	}
	return *m
}

// Get implements extension.State.
func (s stateKeeper) Get(key string) (json.RawMessage, bool) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	v, ok := s.values(false)[key]
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
		delete(s.values(false), key)
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return errors.NewErr("ExtensionStateInvalid", err).SetMetadata("key", key)
	}
	s.values(true)[key] = b
	return nil
}

// SetGlobalState keeps a value of the global state as the host would have, before the extension is activated (what an earlier
// session told the user).
func (a *API) SetGlobalState(key string, value any) {
	if err := a.GlobalState().Set(key, value); err != nil {
		a.t.Fatalf("SetGlobalState(%q): %v", key, err)
	}
}

// GlobalStateKeys are the keys of the global state that have a value, sorted.
func (a *API) GlobalStateKeys() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	keys := make([]string, 0, len(a.globalState))
	for k := range a.globalState {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
