package fakeapi

import (
	"context"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The history API of the fake (SPEC §10.30 "The Source Control Graph", step 24.20), with the rules of the host (internal/gui/exthost/
// history.go): the same errors, the same disposal with the source control and the activation, the same containment of a panic.

// FakeHistory is the history provider the extension set on a source control.
type FakeHistory struct {
	src      *FakeSource
	p        extension.HistoryProvider
	disposed bool

	current, remote, base *extension.HistoryItemRef
	changes               []extension.HistoryRefsChange
}

var _ extension.HistoryRegistration = (*FakeHistory)(nil)

// SetHistoryProvider implements extension.SourceControl with the errors of the host: ExtensionDeactivated, ExtensionHistoryInvalid,
// ExtensionHistoryUnavailable, ExtensionSCMDisposed and ExtensionHistoryDuplicate.
func (s *FakeSource) SetHistoryProvider(p extension.HistoryProvider) (extension.HistoryRegistration, error) {
	a := s.a
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.scm.dead:
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	case p.Items == nil || p.Refs == nil || p.CommonAncestor == nil:
		return nil, errors.New("ExtensionHistoryInvalid", "a history provider needs the functions Items, Refs and CommonAncestor", "source", s.id)
	case a.historyUnavailable:
		return nil, errors.New("ExtensionHistoryUnavailable", "the scope has no Graph view")
	case s.disposed:
		return nil, errors.New("ExtensionSCMDisposed", "the source control was disposed", "source", s.id)
	case s.history != nil && !s.history.disposed:
		return nil, errors.New("ExtensionHistoryDuplicate", "the source control has a history provider already", "source", s.id)
	}
	s.history = &FakeHistory{src: s, p: p}
	return s.history, nil
}

// History is the history provider of the source control, or nil when it has none (or it was disposed).
func (s *FakeSource) History() *FakeHistory {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.history == nil || s.history.disposed {
		return nil
	}
	return s.history
}

// SetHistoryUnavailable makes SetHistoryProvider fail as the host does in a scope that has no Graph view (a host without a workbench),
// with the tag ExtensionHistoryUnavailable.
func (a *API) SetHistoryUnavailable(unavailable bool) {
	a.mu.Lock()
	a.historyUnavailable = unavailable
	a.mu.Unlock()
}

// contain runs a function of the provider as the host does: a panic takes the extension off and is the error of the call.
func (h *FakeHistory) contain(what string, call func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			h.src.a.scmFail("history provider", r)
			err = errors.NewErrf("ExtensionHistoryPanicked", "it panicked in the %s of a history provider: %v", what, r)
		}
	}()
	return call()
}

// Items asks the provider for a page, as the Graph does.
func (h *FakeHistory) Items(ctx context.Context, o extension.HistoryOptions) (items []extension.HistoryItem, err error) {
	err = h.contain("Items", func() (callErr error) { items, callErr = h.p.Items(ctx, o); return callErr })
	return items, err
}

// Refs asks the provider for the references.
func (h *FakeHistory) Refs(ctx context.Context, ids []string) (refs []extension.HistoryItemRef, err error) {
	err = h.contain("Refs", func() (callErr error) { refs, callErr = h.p.Refs(ctx, ids); return callErr })
	return refs, err
}

// CommonAncestor asks the provider for the common ancestor.
func (h *FakeHistory) CommonAncestor(ctx context.Context, refs []string) (id string, err error) {
	err = h.contain("CommonAncestor", func() (callErr error) { id, callErr = h.p.CommonAncestor(ctx, refs); return callErr })
	return id, err
}

// OpenCommand is the command the Graph runs when a row is opened.
func (h *FakeHistory) OpenCommand() string { return h.p.OpenCommand }

// CurrentRefs are the references the extension set last: the current branch, the one it tracks and the base.
func (h *FakeHistory) CurrentRefs() (current, remote, base *extension.HistoryItemRef) {
	h.src.a.mu.Lock()
	defer h.src.a.mu.Unlock()
	return h.current, h.remote, h.base
}

// RefsChanges are the changes of references the extension told, in order.
func (h *FakeHistory) RefsChanges() []extension.HistoryRefsChange {
	h.src.a.mu.Lock()
	defer h.src.a.mu.Unlock()
	return append([]extension.HistoryRefsChange(nil), h.changes...)
}

// Disposed tells whether the provider was disposed, or went with its source control or the activation.
func (h *FakeHistory) Disposed() bool {
	h.src.a.mu.Lock()
	defer h.src.a.mu.Unlock()
	return h.disposed || h.src.disposed
}

// SetCurrentRefs implements extension.HistoryRegistration.
func (h *FakeHistory) SetCurrentRefs(current, remote, base *extension.HistoryItemRef) {
	h.src.a.mu.Lock()
	defer h.src.a.mu.Unlock()
	if h.disposed || !h.src.live() {
		return
	}
	h.current, h.remote, h.base = cloneRef(current), cloneRef(remote), cloneRef(base)
}

func cloneRef(r *extension.HistoryItemRef) *extension.HistoryItemRef {
	if r == nil {
		return nil
	}
	c := *r
	return &c
}

// RefsChanged implements extension.HistoryRegistration.
func (h *FakeHistory) RefsChanged(c extension.HistoryRefsChange) {
	h.src.a.mu.Lock()
	defer h.src.a.mu.Unlock()
	if h.disposed || !h.src.live() {
		return
	}
	h.changes = append(h.changes, c)
}

// Dispose implements extension.HistoryRegistration.
func (h *FakeHistory) Dispose() {
	h.src.a.mu.Lock()
	defer h.src.a.mu.Unlock()
	h.disposed = true
}
