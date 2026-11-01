package exthost

import (
	"slices"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The binding of extension.TerminalEnvironment to the sources of terminal variables of a scope (SPEC §10.30 "Passwords and editors", step
// 24.23). The registry belongs to the scope, so that the extension that contributes (Git) and the one that reads (the Terminal) meet in it;
// it needs nothing of a workbench, so every host has it.

// terminalSource is a registered source of variables.
type terminalSource struct {
	inst *instance
	f    extension.TerminalVariables
}

// terminalSources are the sources of a scope, in the order they were registered.
type terminalSources struct {
	mu   sync.Mutex
	list []*terminalSource
}

func (t *terminalSources) add(s *terminalSource) {
	t.mu.Lock()
	t.list = append(t.list, s)
	t.mu.Unlock()
}

func (t *terminalSources) remove(s *terminalSource) {
	t.mu.Lock()
	if i := slices.Index(t.list, s); i >= 0 {
		t.list = slices.Delete(t.list, i, i+1)
	}
	t.mu.Unlock()
}

func (t *terminalSources) snapshot() []*terminalSource {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.list)
}

// terminalEnvAPI is the extension.TerminalEnvironment of one activation.
type terminalEnvAPI struct{ inst *instance }

var _ extension.TerminalEnvironment = terminalEnvAPI{}

// TerminalEnvironment implements extension.API.
func (a *api) TerminalEnvironment() extension.TerminalEnvironment { return terminalEnvAPI{a.inst} }

// Contribute implements extension.TerminalEnvironment.
func (e terminalEnvAPI) Contribute(f extension.TerminalVariables) (func(), error) {
	in := e.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if f == nil {
		return nil, errors.New("ExtensionTerminalEnvironmentInvalid", "a source of terminal variables needs a function", "extension", in.id)
	}
	src := &terminalSource{inst: in, f: f}
	in.scope.termEnv.add(src)
	var once sync.Once
	dispose := func() { once.Do(func() { in.scope.termEnv.remove(src) }) }
	if !in.track(dispose) {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return dispose, nil
}

// Contributions implements extension.TerminalEnvironment.
func (e terminalEnvAPI) Contributions() []extension.TerminalContribution {
	var out []extension.TerminalContribution
	for _, src := range e.inst.scope.termEnv.snapshot() {
		if c, ok := src.ask(); ok {
			out = append(out, c)
		}
	}
	return out
}

// ask calls the source. A panic in it fails the extension that owns it and gives nothing; variables that cannot be in an environment are
// logged and left out.
func (s *terminalSource) ask() (c extension.TerminalContribution, ok bool) {
	in := s.inst
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionTerminalEnvironmentPanicked", "it panicked in a source of terminal variables: %v", r).SetMetadata("extension", in.id, "scope", in.scope.opts.Info.Name)
			in.scope.post(func() { in.scope.fail(in.slot, failure, "terminal variables") })
			c, ok = extension.TerminalContribution{}, false
		}
	}()
	if in.isDead() {
		return c, false
	}
	vars, description := s.f()
	valid := make(map[string]string, len(vars))
	for name, value := range vars {
		if !extension.ValidTerminalVariable(name, value) {
			in.log.Warn("a terminal variable is not valid and was left out", "name", name)
			continue
		}
		valid[name] = value
	}
	if len(valid) == 0 {
		return c, false
	}
	return extension.TerminalContribution{Extension: in.id, Name: in.slot.info.Manifest.Name, Description: description, Variables: valid}, true
}
