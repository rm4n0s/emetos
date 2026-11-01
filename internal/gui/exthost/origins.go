package exthost

import (
	"slices"
	"sync"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// Origins (SPEC §10.18 "As built (step 24.25)", D50): one scope serves the window of the host and the views of its clients. A command a
// client runs in its view reaches the handler of the scope's extension with the view's origin in its context; while the handler runs (and
// in the callbacks that continue it) Origins().Current says so, and the quick picks, input boxes, dialogs and notifications the
// extension raises are shown on that screen. A question that nobody started stays on the host's window. This file is the
// extension.Origins of an activation, the bookkeeping of the scope, and the context keys of one screen.

// originState is what the scope knows about origins.
type originState struct {
	mu sync.Mutex
	// cur is the origin of the command being handled on the UI thread.
	cur extension.Origin
	// remembered are the origins the extensions kept under a key (Origins().Remember).
	remembered map[string]extension.Origin
	// shared are the context keys the extensions set for every screen (ContextKeys().Set) with their values, and sharedSubs the
	// functions told of each change (the views of the clients copy them).
	shared     map[string]any
	sharedSubs []*func(key string, value any, reset bool)
}

// onUI tells whether the caller is on the UI thread; a scope that has no way to know says yes.
func (s *Scope) onUI() bool { return s.opts.OnUI == nil || s.opts.OnUI() }

// currentOrigin is Origins().Current for the scope.
func (s *Scope) currentOrigin() extension.Origin {
	if !s.onUI() {
		return extension.HostOrigin
	}
	s.orig.mu.Lock()
	defer s.orig.mu.Unlock()
	return s.orig.cur
}

// withOrigin calls f with the origin as the current one. UI thread only.
func (s *Scope) withOrigin(o extension.Origin, f func()) {
	s.orig.mu.Lock()
	old := s.orig.cur
	s.orig.cur = o
	s.orig.mu.Unlock()
	defer func() {
		s.orig.mu.Lock()
		s.orig.cur = old
		s.orig.mu.Unlock()
	}()
	f()
}

// resolveOrigin is the origin a question goes to: the one the extension named, else the current one.
func (s *Scope) resolveOrigin(explicit extension.Origin) extension.Origin {
	if explicit != extension.HostOrigin {
		return explicit
	}
	return s.currentOrigin()
}

// inOrigin wraps a callback that continues the work of a command so that it runs with the origin the command had.
func (s *Scope) inOrigin(o extension.Origin, f func()) func() {
	if o == extension.HostOrigin {
		return f
	}
	return func() { s.withOrigin(o, f) }
}

type originsAPI struct{ inst *instance }

var _ extension.Origins = originsAPI{}

// Origins implements extension.API.
func (a *api) Origins() extension.Origins { return originsAPI{a.inst} }

// Current implements extension.Origins.
func (o originsAPI) Current() extension.Origin { return o.inst.scope.currentOrigin() }

// Run implements extension.Origins.
func (o originsAPI) Run(origin extension.Origin, f func()) {
	if f == nil {
		return
	}
	o.inst.scope.withOrigin(origin, f)
}

// Remember implements extension.Origins.
func (o originsAPI) Remember(key string, origin extension.Origin) {
	s := o.inst.scope
	s.orig.mu.Lock()
	defer s.orig.mu.Unlock()
	if s.orig.remembered == nil {
		s.orig.remembered = map[string]extension.Origin{}
	}
	if origin == extension.HostOrigin {
		delete(s.orig.remembered, key)
		return
	}
	s.orig.remembered[key] = origin
}

// Recall implements extension.Origins.
func (o originsAPI) Recall(key string) extension.Origin {
	s := o.inst.scope
	s.orig.mu.Lock()
	defer s.orig.mu.Unlock()
	return s.orig.remembered[key]
}

// ---- the context keys of every screen, and of one ----

// OnSharedKey calls fn on the UI thread whenever an extension sets (reset false) or resets a context key for every screen
// (ContextKeys().Set, Reset, and what the end of an activation takes away), and returns the function that stops it. The views of the clients of
// a hosted workspace copy the keys with it, so that the when-clauses of the extensions' menus, views and commands read them in the view.
func (s *Scope) OnSharedKey(fn func(key string, value any, reset bool)) (stop func()) {
	p := &fn
	s.orig.mu.Lock()
	s.orig.sharedSubs = append(s.orig.sharedSubs, p)
	s.orig.mu.Unlock()
	return func() {
		s.orig.mu.Lock()
		defer s.orig.mu.Unlock()
		s.orig.sharedSubs = slices.DeleteFunc(s.orig.sharedSubs, func(x *func(string, any, bool)) bool { return x == p })
	}
}

// SharedKeys are the context keys the extensions set for every screen, with their values now.
func (s *Scope) SharedKeys() map[string]any {
	s.orig.mu.Lock()
	defer s.orig.mu.Unlock()
	return maps0(s.orig.shared)
}

func maps0(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// setShared sets or resets a context key for every screen: the host's keys, and the views' through the subscribers. UI thread.
func (s *Scope) setShared(key string, value any, reset bool) {
	keys := s.opts.Keys
	if reset {
		keys.Reset(key)
	} else {
		keys.Set(key, value)
	}
	s.orig.mu.Lock()
	if s.orig.shared == nil {
		s.orig.shared = map[string]any{}
	}
	if reset {
		delete(s.orig.shared, key)
	} else {
		s.orig.shared[key] = value
	}
	subs := slices.Clone(s.orig.sharedSubs)
	s.orig.mu.Unlock()
	for _, f := range subs {
		(*f)(key, value, reset)
	}
}

// keysOfOrigin is the context keys of a screen, or nil when the screen is gone (or the scope has none).
func (s *Scope) keysOfOrigin(o extension.Origin) *contextkey.Scope {
	if o == extension.HostOrigin {
		return s.opts.Keys
	}
	if s.opts.OriginKeys == nil {
		return nil
	}
	return s.opts.OriginKeys(o)
}

// viewsOfOrigin is the views model of a screen: the window's, or the view of a client; nil when the view is gone.
func (s *Scope) viewsOfOrigin(o extension.Origin) *workbench.Views {
	if o == extension.HostOrigin || s.opts.OriginViews == nil {
		return s.opts.Views
	}
	return s.opts.OriginViews(o)
}

// keysOf is the context keys of the screen a widget is on: the keys of its window, or of the view of a client.
func (s *Scope) keysOf(bc widgets.BuildContext) *contextkey.Scope {
	if s.opts.KeysOf != nil {
		if k := s.opts.KeysOf(bc); k != nil {
			return k
		}
	}
	return s.opts.Keys
}

// SetFor implements extension.ContextKeys.
func (c contextAPI) SetFor(origin extension.Origin, key string, value any) error {
	in := c.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if !in.ownsContextKey(key) {
		return errors.New("ExtensionContextKeyForeign", "the key of a context key starts with the id of the extension and a dot, or is one the extension declares", "extension", in.id, "key", key)
	}
	s := in.scope
	if s.opts.Keys == nil {
		return errors.New("ExtensionContextUnavailable", "the scope has no context keys", "extension", in.id)
	}
	ok := originKey{origin, key}
	in.mu.Lock()
	first := !in.originKeys[ok]
	if in.originKeys == nil {
		in.originKeys = map[originKey]bool{}
	}
	in.originKeys[ok] = true
	in.mu.Unlock()
	if first {
		in.track(func() {
			if k := s.keysOfOrigin(origin); k != nil {
				k.Reset(key)
			}
		})
	}
	s.post(func() {
		if k := s.keysOfOrigin(origin); k != nil {
			k.Set(key, value)
			if s.opts.Views != nil {
				s.opts.Views.Changed()
			}
		}
	})
	return nil
}

// ResetFor implements extension.ContextKeys.
func (c contextAPI) ResetFor(origin extension.Origin, key string) {
	in := c.inst
	s := in.scope
	if in.isDead() || s.opts.Keys == nil || !in.ownsContextKey(key) {
		return
	}
	s.post(func() {
		if k := s.keysOfOrigin(origin); k != nil {
			k.Reset(key)
			if s.opts.Views != nil {
				s.opts.Views.Changed()
			}
		}
	})
}

// originKey is a context key on one screen.
type originKey struct {
	origin extension.Origin
	key    string
}
