package exthost

import (
	"context"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
)

// The binding of the quick diff providers of an extension to the registry of the window tab (SPEC §10.30 "Quick diff", step 24.11; after
// extHostSCM.ts, where a source control's quickDiffProvider and secondaryQuickDiffProvider are registered with the service of the
// workbench). A provider belongs to a source control: it is asked about the files below the source control's root and goes with it.
// Its function runs off the UI thread, caught at the boundary like every call into an extension (SPEC §10.15 "Containment").

// AddQuickDiffProvider implements extension.SourceControl.
func (h *scmSource) AddQuickDiffProvider(p extension.QuickDiffProvider) (extension.QuickDiffRegistration, error) {
	in := h.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if err := in.scope.scmOwned(in, h.src); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Label) == "" || p.Original == nil || (p.Kind != extension.QuickDiffPrimary && p.Kind != extension.QuickDiffSecondary) || h.src.RootURI() == "" {
		return nil, errors.New("ExtensionQuickDiffInvalid", "a quick diff provider needs a label, a function that finds the original, a known kind and a source control with a root", "extension", in.id, "label", p.Label, "root", h.src.RootURI())
	}
	svc := in.scope.opts.QuickDiff
	if svc == nil {
		return nil, errors.New("ExtensionQuickDiffUnavailable", "the scope has no editors to compare", "extension", in.id)
	}
	h.mu.Lock()
	if h.disposed || h.src.Removed() {
		h.mu.Unlock()
		return nil, errors.New("ExtensionSCMDisposed", "the source control was disposed", "extension", in.id, "source", h.src.ProviderID())
	}
	if h.quickDiffKinds[p.Kind] {
		h.mu.Unlock()
		return nil, errors.New("ExtensionQuickDiffDuplicate", "the source control has a quick diff provider of this kind already", "extension", in.id, "source", h.src.ProviderID(), "label", p.Label)
	}
	if h.quickDiffKinds == nil {
		h.quickDiffKinds = map[extension.QuickDiffKind]bool{}
	}
	h.quickDiffKinds[p.Kind] = true
	kind := quickdiff.KindPrimary
	if p.Kind == extension.QuickDiffSecondary {
		kind = quickdiff.KindSecondary
	}
	_, remove := svc.Add(quickdiff.Provider{Owner: in.id, Label: p.Label, Kind: kind, RootURI: h.src.RootURI(), Original: h.containOriginal(p)})
	reg := &quickDiffHandle{h: h, kind: p.Kind, remove: remove}
	h.quickDiffRegs = append(h.quickDiffRegs, reg)
	h.mu.Unlock()
	if !in.track(reg.Dispose) { // the activation ended meanwhile: track disposed the provider
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return reg, nil
}

// containOriginal wraps the function of a provider: a panic fails the extension and is an error for the caller, which is a goroutine of
// the host.
func (h *scmSource) containOriginal(p extension.QuickDiffProvider) func(context.Context, string) (string, error) {
	in := h.inst
	return func(ctx context.Context, uri string) (original string, err error) {
		defer func() {
			if r := recover(); r != nil {
				original = ""
				failure := errors.NewErrf("ExtensionQuickDiffPanicked", "it panicked in the Original function of a quick diff provider: %v", r).SetMetadata("extension", in.id, "scope", in.scope.opts.Info.Name, "label", p.Label)
				in.scope.post(func() { in.scope.fail(in.slot, failure, "quick diff provider") })
				err = failure
			}
		}()
		if in.isDead() {
			return "", errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
		}
		return p.Original(ctx, uri)
	}
}

// RefreshQuickDiff implements extension.SourceControl.
func (h *scmSource) RefreshQuickDiff() {
	h.mutate(func(s *scm.Source) {
		if svc := h.inst.scope.opts.QuickDiff; svc != nil {
			svc.Refresh(s.RootURI())
		}
	})
}

// quickDiffHandle is the extension.QuickDiffRegistration of a provider.
type quickDiffHandle struct {
	h      *scmSource
	kind   extension.QuickDiffKind
	remove func()
	done   bool
}

var _ extension.QuickDiffRegistration = (*quickDiffHandle)(nil)

// Dispose implements extension.QuickDiffRegistration.
func (r *quickDiffHandle) Dispose() {
	r.h.mu.Lock()
	defer r.h.mu.Unlock()
	r.disposeLocked()
}

// disposeLocked takes the provider away; the source control's lock is held.
func (r *quickDiffHandle) disposeLocked() {
	if r.done {
		return
	}
	r.done = true
	delete(r.h.quickDiffKinds, r.kind)
	r.remove()
}

// QuickDiffOriginal implements extension.Documents.
func (d documentsAPI) QuickDiffOriginal(ctx context.Context, uri string) (string, bool, error) {
	in := d.inst
	if in.isDead() {
		return "", false, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	svc := in.scope.opts.QuickDiff
	if svc == nil {
		return "", false, errors.New("ExtensionQuickDiffUnavailable", "the scope has no editors to compare", "extension", in.id)
	}
	return svc.PrimaryOriginal(ctx, uri)
}
