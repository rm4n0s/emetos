package exthost

import (
	"context"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/scm"
)

// The binding of the history provider of a source control to its model (SPEC §10.30 "The Source Control Graph", step 24.20; after
// extHostSCM.ts, where a source control's historyProvider is registered with the service of the workbench). The provider belongs to a
// source control and goes with it; its functions run off the UI thread, caught at the boundary like every call into an extension
// (SPEC §10.15 "Containment").

// SetHistoryProvider implements extension.SourceControl.
func (h *scmSource) SetHistoryProvider(p extension.HistoryProvider) (extension.HistoryRegistration, error) {
	in := h.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if err := in.scope.scmOwned(in, h.src); err != nil {
		return nil, err
	}
	if p.Items == nil || p.Refs == nil || p.CommonAncestor == nil {
		return nil, errors.New("ExtensionHistoryInvalid", "a history provider needs the functions Items, Refs and CommonAncestor", "extension", in.id, "source", h.src.ProviderID())
	}
	if !in.scope.opts.HistoryGraph {
		return nil, errors.New("ExtensionHistoryUnavailable", "the scope has no Graph view", "extension", in.id)
	}
	h.mu.Lock()
	if h.disposed || h.src.Removed() {
		h.mu.Unlock()
		return nil, errors.New("ExtensionSCMDisposed", "the source control was disposed", "extension", in.id, "source", h.src.ProviderID())
	}
	if h.history != nil {
		h.mu.Unlock()
		return nil, errors.New("ExtensionHistoryDuplicate", "the source control has a history provider already", "extension", in.id, "source", h.src.ProviderID())
	}
	reg := &historyHandle{h: h}
	h.history = reg
	h.mu.Unlock()
	h.src.SetHistoryProvider(&historyProvider{inst: in, p: p, label: h.src.ProviderID()})
	if !in.track(reg.Dispose) {
		reg.Dispose()
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return reg, nil
}

// historyProvider is the scm.HistoryProvider of a provider of an extension.
type historyProvider struct {
	inst  *instance
	p     extension.HistoryProvider
	label string
}

var _ scm.HistoryProvider = (*historyProvider)(nil)

// contain runs a function of the extension; a panic fails the extension and is the error of the call.
func (hp *historyProvider) contain(what string, call func() error) (err error) {
	in := hp.inst
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionHistoryPanicked", "it panicked in the %s of a history provider: %v", what, r).SetMetadata("extension", in.id, "scope", in.scope.opts.Info.Name, "source", hp.label)
			in.scope.post(func() { in.scope.fail(in.slot, failure, "history provider") })
			err = failure
		}
	}()
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return call()
}

// Items implements scm.HistoryProvider. An item with no ID is logged and dropped.
func (hp *historyProvider) Items(ctx context.Context, q scm.HistoryQuery) (out []scm.HistoryItem, err error) {
	var items []extension.HistoryItem
	err = hp.contain("Items", func() (callErr error) {
		items, callErr = hp.p.Items(ctx, extension.HistoryOptions{Skip: q.Skip, Limit: q.Limit, RefIDs: append([]string(nil), q.RefIDs...), FilterText: q.Filter})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	out = make([]scm.HistoryItem, 0, len(items))
	for _, it := range items {
		if it.ID == "" {
			hp.inst.log.Warn("invalid history item: it has no ID", "source", hp.label, "subject", it.Subject)
			continue
		}
		out = append(out, historyItem(it))
	}
	return out, nil
}

// Refs implements scm.HistoryProvider.
func (hp *historyProvider) Refs(ctx context.Context, ids []string) (out []scm.HistoryRef, err error) {
	var refs []extension.HistoryItemRef
	err = hp.contain("Refs", func() (callErr error) {
		refs, callErr = hp.p.Refs(ctx, append([]string(nil), ids...))
		return callErr
	})
	if err != nil {
		return nil, err
	}
	for _, r := range refs {
		if r.ID != "" {
			out = append(out, historyRef(r))
		}
	}
	return out, nil
}

// CommonAncestor implements scm.HistoryProvider.
func (hp *historyProvider) CommonAncestor(ctx context.Context, refs []string) (id string, err error) {
	err = hp.contain("CommonAncestor", func() (callErr error) {
		id, callErr = hp.p.CommonAncestor(ctx, append([]string(nil), refs...))
		return callErr
	})
	return id, err
}

// OpenCommand implements scm.HistoryProvider.
func (hp *historyProvider) OpenCommand() string { return hp.p.OpenCommand }

func historyRef(r extension.HistoryItemRef) scm.HistoryRef {
	return scm.HistoryRef{ID: r.ID, Name: r.Name, Revision: r.Revision, Category: r.Category, Description: r.Description, Icon: r.Icon}
}

func historyRefPtr(r *extension.HistoryItemRef) *scm.HistoryRef {
	if r == nil || r.ID == "" {
		return nil
	}
	c := historyRef(*r)
	return &c
}

func historyRefs(list []extension.HistoryItemRef) []scm.HistoryRef {
	var out []scm.HistoryRef
	for _, r := range list {
		out = append(out, historyRef(r))
	}
	return out
}

func historyItem(it extension.HistoryItem) scm.HistoryItem {
	out := scm.HistoryItem{
		ID: it.ID, ParentIDs: append([]string(nil), it.ParentIDs...), Subject: it.Subject, Message: it.Message, DisplayID: it.DisplayID,
		Author: it.Author, AuthorEmail: it.AuthorEmail, Timestamp: it.Timestamp, Refs: historyRefs(it.References),
	}
	if out.Subject == "" {
		out.Subject, _, _ = strings.Cut(it.Message, "\n")
	}
	if it.Statistics != nil {
		out.Stats = &scm.HistoryStats{Files: it.Statistics.Files, Insertions: it.Statistics.Insertions, Deletions: it.Statistics.Deletions}
	}
	return out
}

// historyHandle is the extension.HistoryRegistration of a source control.
type historyHandle struct {
	h    *scmSource
	once sync.Once
}

var _ extension.HistoryRegistration = (*historyHandle)(nil)

// SetCurrentRefs implements extension.HistoryRegistration.
func (r *historyHandle) SetCurrentRefs(current, remote, base *extension.HistoryItemRef) {
	r.h.mutate(func(s *scm.Source) {
		s.SetHistoryRefs(scm.HistoryRefs{Current: historyRefPtr(current), Remote: historyRefPtr(remote), Base: historyRefPtr(base)})
	})
}

// RefsChanged implements extension.HistoryRegistration.
func (r *historyHandle) RefsChanged(c extension.HistoryRefsChange) {
	r.h.mutate(func(s *scm.Source) {
		s.NotifyHistoryRefs(scm.HistoryRefsChange{Added: historyRefs(c.Added), Removed: historyRefs(c.Removed), Modified: historyRefs(c.Modified), Silent: c.Silent})
	})
}

// Dispose implements extension.HistoryRegistration.
func (r *historyHandle) Dispose() {
	r.once.Do(func() {
		r.h.mu.Lock()
		if r.h.history == r {
			r.h.history = nil
		}
		r.h.mu.Unlock()
		r.h.src.SetHistoryProvider(nil)
	})
}
