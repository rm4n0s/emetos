package exthost

import (
	"context"
	"regexp"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The binding of extension.Documents().ProvideMultiDiff and OpenMultiDiff to the window (SPEC §10.30 "Diffs of changes"; after
// VS Code's IMultiDiffSourceResolverService). The window owns the editors and the saved tabs; this file validates what an extension
// gives, contains the panics of its provider (which runs off the UI thread) and ends its registrations with the activation.

var reMultiDiffKind = regexp.MustCompile(extension.MultiDiffKindPattern)

// ProvideMultiDiff implements extension.Documents.
func (d documentsAPI) ProvideMultiDiff(p extension.MultiDiffProvider) (extension.MultiDiffRegistration, error) {
	in := d.inst
	host, err := d.contentHost("show multi-file diff editors")
	if err != nil {
		return nil, err
	}
	if !reMultiDiffKind.MatchString(p.Kind) {
		return nil, errors.New("ExtensionMultiDiffInvalid", "the kind of a multi-file diff provider is a word of letters, digits, dots and hyphens that starts with a letter", "extension", in.id, "kind", p.Kind)
	}
	if p.Resources == nil {
		return nil, errors.New("ExtensionMultiDiffInvalid", "a multi-file diff provider needs a function that makes the files", "extension", in.id, "kind", p.Kind)
	}
	reg, err := host.ProvideMultiDiff(in.id, d.containMultiDiff(p))
	if err != nil {
		return nil, err
	}
	h := &multiDiffHandle{inst: in, reg: reg}
	if !in.track(h.Dispose) {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return h, nil
}

// containMultiDiff wraps the function of a provider so that a panic in it fails the extension (SPEC §10.15 "Containment") and is an
// error for the caller, which runs on a goroutine of the window.
func (d documentsAPI) containMultiDiff(p extension.MultiDiffProvider) extension.MultiDiffProvider {
	in := d.inst
	out := p
	out.Resources = func(ctx context.Context, key string) (files []extension.MultiDiffResource, err error) {
		defer func() {
			if r := recover(); r != nil {
				failure := errors.NewErrf("ExtensionMultiDiffPanicked", "it panicked in the Resources of a multi-file diff provider: %v", r).SetMetadata("extension", in.id, "scope", in.scope.opts.Info.Name, "kind", p.Kind)
				in.scope.post(func() { in.scope.fail(in.slot, failure, "multi-file diff provider") })
				files, err = nil, failure
			}
		}()
		if in.isDead() {
			return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
		}
		return p.Resources(ctx, key)
	}
	return out
}

// multiDiffHandle is the extension.MultiDiffRegistration of a registered provider.
type multiDiffHandle struct {
	inst *instance
	reg  extension.MultiDiffRegistration
	once sync.Once
}

var _ extension.MultiDiffRegistration = (*multiDiffHandle)(nil)

// Changed implements extension.MultiDiffRegistration.
func (h *multiDiffHandle) Changed(keys ...string) {
	if h.inst.isDead() {
		return
	}
	h.reg.Changed(keys...)
}

// Dispose implements extension.MultiDiffRegistration.
func (h *multiDiffHandle) Dispose() { h.once.Do(h.reg.Dispose) }

// OpenMultiDiff implements extension.Documents.
func (d documentsAPI) OpenMultiDiff(spec extension.MultiDiffSpec) error {
	host, err := d.contentHost("open multi-file diff editors")
	if err != nil {
		return err
	}
	if !spec.Valid() {
		return errors.New("ExtensionMultiDiffInvalid", "a multi-file diff editor shows the set of a provider (Kind and Key) or a fixed set (Resources), and not both", "extension", d.inst.id, "kind", spec.Kind, "key", spec.Key, "resources", len(spec.Resources))
	}
	for i, r := range spec.Resources {
		if r.Original == "" && r.Modified == "" {
			return errors.New("ExtensionMultiDiffInvalid", "a file of a multi-file diff editor needs a document on at least one side", "extension", d.inst.id, "index", i, "path", r.Path)
		}
	}
	return host.OpenMultiDiff(spec)
}
