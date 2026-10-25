package exthost

import (
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// Info is a compiled-in extension as the host knows it: its factory and the manifest read from it.
type Info struct {
	// Manifest is what the extension declared; it is read once, when the host is made.
	Manifest extension.Manifest
	factory  extension.Factory
}

// HostOptions are what NewHost needs.
type HostOptions struct {
	// Factories are the constructors of the extensions in activation order (extensions.All).
	Factories []extension.Factory
	// Logger receives the problems of the extensions; slog.Default when nil.
	Logger *slog.Logger
	// Settings is the registry of the settings of the process: the extensions register theirs in it.
	Settings *settings.Registry
}

// Host holds the extensions of the process (SPEC §10.15): the manifests of the compiled-in extensions and the scopes
// (window tabs) they run in. It is made once. A Scope does the work; the Host answers for what is common to them.
type Host struct {
	log      *slog.Logger
	registry *settings.Registry
	infos    []*Info
	problems []error

	mu     sync.Mutex
	scopes []*Scope
	// listeners are told when the state of an extension in a scope changes or a scope opens or closes.
	listeners []*func()
	// owners tells who registered each setting key of the extensions and how many scopes hold it: a setting is registered
	// once for the process, by the first scope that needs it, and removed with the last.
	owners map[string]*settingOwner
}

// settingOwner is the extension that registered a setting and the number of activations that hold it.
type settingOwner struct {
	extension string
	holds     int
}

// NewHost reads the manifest of each factory and keeps the extensions whose manifest is valid. An extension whose
// factory or Manifest panics, or whose manifest is invalid, is left out and logged; Problems lists them.
func NewHost(opts HostOptions) *Host {
	h := &Host{log: opts.Logger, registry: opts.Settings, owners: map[string]*settingOwner{}}
	if h.log == nil {
		h.log = slog.Default()
	}
	seen := map[string]bool{}
	for i, f := range opts.Factories {
		m, err := probe(i, f)
		if err == nil {
			err = validateManifest(m, seen)
		}
		if err != nil {
			h.problems = append(h.problems, err)
			h.log.Warn("an extension is left out", "err", err)
			continue
		}
		seen[m.ID] = true
		h.infos = append(h.infos, &Info{Manifest: m, factory: f})
	}
	// the topics an extension declares must not be another's: checked once every id is known
	ids := make([]string, len(h.infos))
	for i, in := range h.infos {
		ids[i] = in.Manifest.ID
	}
	h.infos = slices.DeleteFunc(h.infos, func(in *Info) bool {
		err := checkTopicOwnership(in.Manifest, ids)
		if err != nil {
			h.problems = append(h.problems, err)
			h.log.Warn("an extension is left out", "err", err)
		}
		return err != nil
	})
	return h
}

// probe calls a factory and reads the manifest of the extension it makes. A nil factory fails with the tag
// ExtensionFactoryNil, a panic of the factory or of Manifest with ExtensionProbePanicked.
func probe(index int, f extension.Factory) (m extension.Manifest, err error) {
	if f == nil {
		return m, errors.New("ExtensionFactoryNil", "an extension factory is nil", "index", index)
	}
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("ExtensionProbePanicked", "the factory or the manifest of an extension panicked", "index", index, "panic", fmt.Sprint(r))
		}
	}()
	ext := f()
	if ext == nil {
		return m, errors.New("ExtensionFactoryNil", "an extension factory made no extension", "index", index)
	}
	return ext.Manifest(), nil
}

// Problems are the extensions that were left out, with the reason.
func (h *Host) Problems() []error { return slices.Clone(h.problems) }

// Manifests are the manifests of the extensions, in activation order.
func (h *Host) Manifests() []extension.Manifest {
	out := make([]extension.Manifest, len(h.infos))
	for i, in := range h.infos {
		out[i] = in.Manifest
	}
	return out
}

// Scopes lists the scopes that are open, in the order they were made.
func (h *Host) Scopes() []*Scope {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.scopes)
}

// Shutdown closes every scope: the context of each extension is cancelled and what it contributed is removed. Emetos
// calls it when it quits.
func (h *Host) Shutdown() {
	for _, s := range h.Scopes() {
		s.Close()
	}
}

// ScopeRow is the state of an extension in one scope.
type ScopeRow struct {
	// Scope is the name of the window tab.
	Scope string
	// State is what the extension is in the scope.
	State State
	// Failure is why it failed, for StateFailed.
	Failure *Failure
}

// Row is an extension as the Extensions page lists it.
type Row struct {
	// Manifest is what the extension declares.
	Manifest extension.Manifest
	// Disabled tells that the user turned the extension off (extensions.disabled).
	Disabled bool
	// Scopes is the state of the extension in each open scope, in the order the scopes were made.
	Scopes []ScopeRow
}

// Rows lists the extensions with their state in every scope; disabled are the ids of extensions.disabled.
func (h *Host) Rows(disabled []string) []Row {
	scopes := h.Scopes()
	rows := make([]Row, len(h.infos))
	for i, in := range h.infos {
		rows[i] = Row{Manifest: in.Manifest, Disabled: slices.Contains(disabled, in.Manifest.ID)}
		for _, s := range scopes {
			state, failure := s.status(in.Manifest.ID)
			rows[i].Scopes = append(rows[i].Scopes, ScopeRow{Scope: s.opts.Info.Name, State: state, Failure: failure})
		}
	}
	return rows
}

// holdSetting registers the setting for the extension, or counts one more hold when the extension has registered it
// already. It fails with ExtensionSettingTaken when another extension has the key; the registry's own error (a
// setting of the editor, an invalid setting) is returned as it is.
func (h *Host) holdSetting(extensionID string, s settings.Setting) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if o, ok := h.owners[s.Key]; ok {
		if o.extension != extensionID {
			return errors.New("ExtensionSettingTaken", "another extension registered this setting", "key", s.Key, "extension", extensionID, "owner", o.extension)
		}
		o.holds++
		return nil
	}
	if err := h.registry.Register(s); err != nil {
		return err
	}
	h.owners[s.Key] = &settingOwner{extension: extensionID, holds: 1}
	return nil
}

// releaseSetting drops one hold of a setting and removes it from the registry with the last.
func (h *Host) releaseSetting(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	o, ok := h.owners[key]
	if !ok {
		return
	}
	o.holds--
	if o.holds > 0 {
		return
	}
	delete(h.owners, key)
	h.registry.Unregister(key)
}

func (h *Host) addScope(s *Scope) {
	h.mu.Lock()
	h.scopes = append(h.scopes, s)
	h.mu.Unlock()
	h.changed()
}

func (h *Host) removeScope(s *Scope) {
	h.mu.Lock()
	h.scopes = slices.DeleteFunc(h.scopes, func(x *Scope) bool { return x == s })
	h.mu.Unlock()
	h.changed()
}

// OnChange calls fn when a scope opens or closes or the state of an extension in a scope changes (an activation, a
// failure, an extension turned on or off), on the thread that made the change, and returns the function that stops it.
// The Extensions page follows the host with it.
func (h *Host) OnChange(fn func()) (unsubscribe func()) {
	p := &fn
	h.mu.Lock()
	h.listeners = append(h.listeners, p)
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.listeners = slices.DeleteFunc(h.listeners, func(x *func()) bool { return x == p })
	}
}

// changed tells the listeners.
func (h *Host) changed() {
	h.mu.Lock()
	listeners := slices.Clone(h.listeners)
	h.mu.Unlock()
	for _, f := range listeners {
		(*f)()
	}
}

// targets are the scopes whose subscribers get an event that was published in src. Inside one scope that is the scope
// itself; step 11.3 adds the scopes that a link the user allowed leads to, checked here, at delivery.
func (h *Host) targets(src *Scope, publisher, topic string) []*Scope {
	_, _ = publisher, topic
	return []*Scope{src}
}

// handlerFor is the handler of a request of the topic made in src, or nil. Step 11.3 looks along the links too.
func (h *Host) handlerFor(src *Scope, topic string) *handlerEntry {
	return src.bus.handlerOf(topic)
}
