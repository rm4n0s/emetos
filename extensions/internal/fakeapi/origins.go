package fakeapi

import (
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/errors"
)

// The Origins API of the fake (SPEC §10.18 "As built (step 24.25)", D50): a test of an extension sets the origin a command
// "came from" with RunAs, and reads what the extension remembered and which keys it set for each screen.

type originState struct {
	cur  extension.Origin
	keys map[extension.Origin]map[string]any
	// memory is what Remember keeps; fakes that share it meet as the extensions of one scope do.
	memory *OriginMemory
}

// OriginMemory is the origins the fakes remember, which the fakes of two extensions share (ShareOrigins) as the extensions of a scope share
// them in the host.
type OriginMemory struct {
	mu sync.Mutex
	m  map[string]extension.Origin
}

// NewOriginMemory makes an empty memory.
func NewOriginMemory() *OriginMemory { return &OriginMemory{m: map[string]extension.Origin{}} }

// ShareOrigins makes the fake remember in the memory, so that what it keeps is what Recall of the fakes that share it answers.
func (a *API) ShareOrigins(m *OriginMemory) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.origins.memory = m
}

// mem is the memory the fake uses; a.mu is held.
func (a *API) mem() *OriginMemory {
	if a.origins.memory == nil {
		a.origins.memory = NewOriginMemory()
	}
	return a.origins.memory
}

// Origins implements extension.API.
func (a *API) Origins() extension.Origins { return originsAPI{a} }

type originsAPI struct{ a *API }

// Current implements extension.Origins.
func (o originsAPI) Current() extension.Origin {
	o.a.mu.Lock()
	defer o.a.mu.Unlock()
	return o.a.origins.cur
}

// Run implements extension.Origins.
func (o originsAPI) Run(origin extension.Origin, f func()) {
	o.a.mu.Lock()
	old := o.a.origins.cur
	o.a.origins.cur = origin
	o.a.mu.Unlock()
	defer func() {
		o.a.mu.Lock()
		o.a.origins.cur = old
		o.a.mu.Unlock()
	}()
	f()
}

// Remember implements extension.Origins.
func (o originsAPI) Remember(key string, origin extension.Origin) {
	o.a.mu.Lock()
	m := o.a.mem()
	o.a.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if origin == extension.HostOrigin {
		delete(m.m, key)
		return
	}
	m.m[key] = origin
}

// Recall implements extension.Origins.
func (o originsAPI) Recall(key string) extension.Origin {
	o.a.mu.Lock()
	m := o.a.mem()
	o.a.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.m[key]
}

// RunAs calls f with Origins().Current() returning origin, as a command that a client ran in its view would: the handlers of the extension
// run inside it.
func (a *API) RunAs(origin extension.Origin, f func()) { originsAPI{a}.Run(origin, f) }

// SetFor implements extension.ContextKeys.
func (c contextKeys) SetFor(origin extension.Origin, key string, value any) error {
	if !strings.HasPrefix(key, c.a.manifest.ID+".") && !slices.Contains(c.a.manifest.Contributes.ContextKeys, key) {
		return errors.New("FakeContextKeyForeign", "the key is not under the id of the extension", "key", key)
	}
	c.a.mu.Lock()
	defer c.a.mu.Unlock()
	if c.a.origins.keys == nil {
		c.a.origins.keys = map[extension.Origin]map[string]any{}
	}
	if c.a.origins.keys[origin] == nil {
		c.a.origins.keys[origin] = map[string]any{}
	}
	c.a.origins.keys[origin][key] = value
	if origin == extension.HostOrigin {
		c.a.keys[key] = value // the keys of the window of the host are the ones ContextKey reads
	}
	return nil
}

// ResetFor implements extension.ContextKeys.
func (c contextKeys) ResetFor(origin extension.Origin, key string) {
	c.a.mu.Lock()
	defer c.a.mu.Unlock()
	delete(c.a.origins.keys[origin], key)
	if origin == extension.HostOrigin {
		delete(c.a.keys, key)
	}
}

// ContextKeyFor is the value of a context key the extension set for one screen with SetFor, or nil.
func (a *API) ContextKeyFor(origin extension.Origin, key string) any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.origins.keys[origin][key]
}
