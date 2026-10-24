package quickinput

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
)

// Ported from VS Code: src/vs/platform/quickinput/common/quickAccess.ts and
// src/vs/platform/quickinput/browser/quickAccess.ts

// HelpEntry is a line of the help (the `?` mode) about a provider.
type HelpEntry struct {
	// Prefix is what to type; the prefix of the provider when empty.
	Prefix string
	// Description says what the mode does.
	Description string
	// Command is the id of the command that opens the mode; its key is shown in the help.
	Command string
}

// ProviderInfo describes a provider.
type ProviderInfo struct {
	// Prefix is the text that selects the provider when the value starts with it. The provider
	// with the longest matching prefix wins; the provider with the empty prefix is the default
	// and is used when no other matches.
	Prefix string
	// Placeholder is the hint of the input while the provider is active.
	Placeholder string
	// ContextKey, when set, is a context key that is true while the provider is active
	// ("inCommandsPicker").
	ContextKey string
	// Help are the lines the `?` mode lists for the provider.
	Help []HelpEntry
}

// Handler is what a provider gives the controller to run its session: the controller calls it
// when the value changes, when the user accepts the active item and when the session ends.
type Handler interface {
	// ValueChanged is called with the filter (the value without the prefix, trimmed) once after
	// Provide and on every change of the value.
	ValueChanged(filter string)
	// Accept is called when the user accepts the active row (Enter or a click).
	Accept(row Row, mods KeyMods)
	// Done is called when the session ends: the picker was hidden, or another provider took
	// over. accepted tells whether the user accepted an item.
	Done(accepted bool)
}

// Provider fills a picker for a mode of the quick input.
type Provider interface {
	// Info describes the provider.
	Info() ProviderInfo
	// Provide starts a session on the picker of s. The controller shows the picker after the
	// call; the provider sets what it needs (the matching switches of the picker) and returns the
	// handler of the session.
	Provide(s *Session) Handler
}

// DefaultFilterKind says how a provider starts when it is opened with just its prefix.
type DefaultFilterKind int

// The ways a provider can start.
const (
	// DefaultFilterPreserve keeps the value as it is given.
	DefaultFilterPreserve DefaultFilterKind = iota
	// DefaultFilterLast restores the value of the last accepted pick.
	DefaultFilterLast
)

// DefaultFilterer is implemented by a provider that wants the controller to restore the last input
// (workbench.commandPalette.preserveInput).
type DefaultFilterer interface {
	// DefaultFilterValue says how the provider starts.
	DefaultFilterValue() DefaultFilterKind
}

// Registry maps prefixes to providers (VS Code's IQuickAccessRegistry).
type Registry struct {
	providers []Provider
}

// NewRegistry makes an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds a provider. Its prefix must not be registered yet.
func (r *Registry) Register(p Provider) error {
	if p == nil {
		return errors.New("QuickAccessProviderInvalid", "the provider is nil")
	}
	prefix := p.Info().Prefix
	if slices.ContainsFunc(r.providers, func(o Provider) bool { return o.Info().Prefix == prefix }) {
		return errors.New("QuickAccessPrefixDuplicate", "a provider is registered for this prefix already", "prefix", prefix)
	}
	r.providers = append(r.providers, p)
	return nil
}

// Unregister removes the provider of a prefix, if there is one.
func (r *Registry) Unregister(prefix string) {
	r.providers = slices.DeleteFunc(r.providers, func(p Provider) bool { return p.Info().Prefix == prefix })
}

// MustRegister is Register for the table of built-in providers; it panics on a failure.
func (r *Registry) MustRegister(ps ...Provider) {
	for _, p := range ps {
		if err := r.Register(p); err != nil {
			panic(err)
		}
	}
}

// Resolve finds the provider for a value: the one with the longest prefix the value starts
// with, else the default provider (the empty prefix), else nil.
func (r *Registry) Resolve(value string) Provider {
	var best Provider
	for _, p := range r.providers {
		prefix := p.Info().Prefix
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		if best == nil || len(prefix) > len(best.Info().Prefix) {
			best = p
		}
	}
	return best
}

// Providers lists the providers ordered by prefix (the default provider first).
func (r *Registry) Providers() []Provider {
	out := slices.Clone(r.providers)
	slices.SortFunc(out, func(a, b Provider) int { return cmp.Compare(a.Info().Prefix, b.Info().Prefix) })
	return out
}

// Session is what a provider sees of the controller while its picker is shown. It ends when the
// picker is hidden or another provider takes over; then its context is cancelled and the
// functions given to OnEnd run.
type Session struct {
	c        *Controller
	picker   *Picker
	ctx      context.Context
	cancel   context.CancelFunc
	prefix   string
	ended    bool
	cleanups []func()
}

// Picker is the picker the provider fills.
func (s *Session) Picker() *Picker { return s.picker }

// Context is cancelled when the session ends; a provider that works in the background passes it on.
func (s *Session) Context() context.Context { return s.ctx }

// Prefix is the prefix of the provider.
func (s *Session) Prefix() string { return s.prefix }

// Hide closes the picker without accepting an item.
func (s *Session) Hide() { s.c.hide(false) }

// Show opens the quick input again with another value, as the help does when a mode is chosen.
func (s *Session) Show(value string, opts ShowOptions) { s.c.Show(value, opts) }

// Post runs f on the UI thread, if the session is still on; a goroutine hands its results over
// with it.
func (s *Session) Post(f func()) {
	s.c.post(func() {
		if !s.ended {
			f()
		}
	})
}

// After runs f once the picker is hidden and the focus is back where it was (see ControllerOptions.After); a
// button of an item uses it to do its work after the picker closed.
func (s *Session) After(f func()) { s.c.after(f) }

// OnEnd registers a function to run when the session ends.
func (s *Session) OnEnd(f func()) { s.cleanups = append(s.cleanups, f) }

func (s *Session) end() {
	if s.ended {
		return
	}
	s.ended = true
	s.cancel()
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		s.cleanups[i]()
	}
	s.cleanups = nil
}

// BaseHandler is a Handler that does nothing, to embed.
type BaseHandler struct{}

// ValueChanged implements Handler.
func (BaseHandler) ValueChanged(string) {}

// Accept implements Handler.
func (BaseHandler) Accept(Row, KeyMods) {}

// Done implements Handler.
func (BaseHandler) Done(bool) {}
