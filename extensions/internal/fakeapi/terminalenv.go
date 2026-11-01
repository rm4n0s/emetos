package fakeapi

import (
	"slices"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The Terminal Environment API of the fake (SPEC §10.30 "Passwords and editors", step 24.23): the sources of terminal variables an extension
// registered, with the rules of the host (internal/gui/exthost/terminalenv.go): the same errors, the same disposal with the activation, a
// variable that cannot be in an environment left out, a panic in a source taking the extension off. The fakes of two extensions meet in a
// TerminalSources they share (ShareTerminalEnvironment), as the extensions of a window tab meet in the scope.

// TerminalSources is the registry of the sources of terminal variables that fakes can share, as the scope shares it in the host.
type TerminalSources struct {
	mu   sync.Mutex
	list []*terminalSource
}

// NewTerminalSources makes an empty registry.
func NewTerminalSources() *TerminalSources { return &TerminalSources{} }

type terminalSource struct {
	a *API
	f extension.TerminalVariables
}

// TerminalEnvironment implements extension.API.
func (a *API) TerminalEnvironment() extension.TerminalEnvironment { return terminalEnvironmentAPI{a} }

// ShareTerminalEnvironment makes the fake use the registry of sources, so that what it contributes reaches the Contributions of the
// fakes that share it. A fake has a registry of its own until then; what it contributed already moves to the shared one.
func (a *API) ShareTerminalEnvironment(s *TerminalSources) {
	old := a.sources()
	if old == s {
		return
	}
	old.mu.Lock()
	var mine, others []*terminalSource
	for _, src := range old.list {
		if src.a == a {
			mine = append(mine, src)
		} else {
			others = append(others, src)
		}
	}
	old.list = others
	old.mu.Unlock()
	s.mu.Lock()
	s.list = append(s.list, mine...)
	s.mu.Unlock()
	a.termMu.Lock()
	a.termSources = s
	a.termMu.Unlock()
}

// sources is the registry the fake uses.
func (a *API) sources() *TerminalSources {
	a.termMu.Lock()
	defer a.termMu.Unlock()
	if a.termSources == nil {
		a.termSources = NewTerminalSources()
	}
	return a.termSources
}

type terminalEnvironmentAPI struct{ a *API }

// Contribute implements extension.TerminalEnvironment with the errors of the host: ExtensionDeactivated and
// ExtensionTerminalEnvironmentInvalid.
func (e terminalEnvironmentAPI) Contribute(f extension.TerminalVariables) (func(), error) {
	a := e.a
	a.mu.Lock()
	dead := a.scm.dead
	a.mu.Unlock()
	if dead {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	if f == nil {
		return nil, errors.New("ExtensionTerminalEnvironmentInvalid", "a source of terminal variables needs a function")
	}
	reg := a.sources()
	src := &terminalSource{a: a, f: f}
	reg.mu.Lock()
	reg.list = append(reg.list, src)
	reg.mu.Unlock()
	var once sync.Once
	dispose := func() {
		once.Do(func() {
			reg := a.sources() // the registry now: the fake may have joined a shared one since
			reg.mu.Lock()
			if i := slices.Index(reg.list, src); i >= 0 {
				reg.list = slices.Delete(reg.list, i, i+1)
			}
			reg.mu.Unlock()
		})
	}
	a.termMu.Lock()
	a.termDisposers = append(a.termDisposers, dispose)
	a.termMu.Unlock()
	return dispose, nil
}

// Contributions implements extension.TerminalEnvironment.
func (e terminalEnvironmentAPI) Contributions() []extension.TerminalContribution {
	reg := e.a.sources()
	reg.mu.Lock()
	list := slices.Clone(reg.list)
	reg.mu.Unlock()
	var out []extension.TerminalContribution
	for _, src := range list {
		if c, ok := src.ask(); ok {
			out = append(out, c)
		}
	}
	return out
}

func (s *terminalSource) ask() (c extension.TerminalContribution, ok bool) {
	a := s.a
	defer func() {
		if r := recover(); r != nil {
			a.mu.Lock()
			a.scm.dead, a.scm.failed = true, true
			a.mu.Unlock()
			c, ok = extension.TerminalContribution{}, false
		}
	}()
	a.mu.Lock()
	dead := a.scm.dead
	a.mu.Unlock()
	if dead {
		return c, false
	}
	vars, description := s.f()
	valid := make(map[string]string, len(vars))
	for name, value := range vars {
		if extension.ValidTerminalVariable(name, value) {
			valid[name] = value
		}
	}
	if len(valid) == 0 {
		return c, false
	}
	return extension.TerminalContribution{Extension: a.manifest.ID, Name: a.manifest.Name, Description: description, Variables: valid}, true
}

// TerminalSourceCount is how many sources of terminal variables the registry of the fake holds, from all the fakes that share it.
func (a *API) TerminalSourceCount() int {
	reg := a.sources()
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return len(reg.list)
}
