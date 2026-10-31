package fakeapi

import (
	"context"
	"regexp"
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The multi-file diff editor of the fake (SPEC §10.30 "Diffs of changes", step 24.17): the providers an extension registered and the
// editors it opened, in memory and with the rules of the host (internal/gui/exthost/multidiff.go). extension/contenttest runs one
// suite on this and on the host.

var reMultiDiffKind = regexp.MustCompile(extension.MultiDiffKindPattern)

// FakeMultiDiff is a provider of sets of files the extension registered: a test asks it for the files of a key, and reads what the
// extension said changed.
type FakeMultiDiff struct {
	a        *API
	p        extension.MultiDiffProvider
	changed  [][]string
	disposed bool
}

// ProvideMultiDiff implements extension.Documents with the errors of the host.
func (d documents) ProvideMultiDiff(p extension.MultiDiffProvider) (extension.MultiDiffRegistration, error) {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.scm.dead:
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	case !reMultiDiffKind.MatchString(p.Kind):
		return nil, errors.New("ExtensionMultiDiffInvalid", "the kind of a multi-file diff provider is a word", "kind", p.Kind)
	case p.Resources == nil:
		return nil, errors.New("ExtensionMultiDiffInvalid", "a multi-file diff provider needs a function that makes the files", "kind", p.Kind)
	case a.content.multiDiffs[p.Kind] != nil:
		return nil, errors.New("ExtMultiDiffKindTaken", "the window tab has a provider for this kind already", "kind", p.Kind)
	}
	if a.content.multiDiffs == nil {
		a.content.multiDiffs = map[string]*FakeMultiDiff{}
	}
	f := &FakeMultiDiff{a: a, p: p}
	a.content.multiDiffs[p.Kind] = f
	return f, nil
}

// Changed implements extension.MultiDiffRegistration: it is recorded (MultiDiffChanges).
func (f *FakeMultiDiff) Changed(keys ...string) {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	if !f.disposed {
		f.changed = append(f.changed, slices.Clone(keys))
	}
}

// Dispose implements extension.MultiDiffRegistration.
func (f *FakeMultiDiff) Dispose() {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	f.disposed = true
	if f.a.content.multiDiffs[f.p.Kind] == f {
		delete(f.a.content.multiDiffs, f.p.Kind)
	}
}

// MultiDiffChanges are the calls of Changed of the provider, in order.
func (f *FakeMultiDiff) MultiDiffChanges() [][]string {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	return slices.Clone(f.changed)
}

// MultiDiffFiles asks the provider of the kind for the files of the set with the key, as the window does when it opens the set; it
// fails with ExtMultiDiffKindUnknown when no provider has the kind.
func (a *API) MultiDiffFiles(ctx context.Context, kind, key string) (files []extension.MultiDiffResource, err error) {
	a.mu.Lock()
	f := a.content.multiDiffs[kind]
	a.mu.Unlock()
	if f == nil {
		return nil, errors.New("ExtMultiDiffKindUnknown", "no provider makes the sets of this kind", "kind", kind)
	}
	defer func() {
		if r := recover(); r != nil {
			a.scmFail("multi-file diff provider", r)
			files, err = nil, errors.NewErrf("ExtensionMultiDiffPanicked", "it panicked in the Resources of a multi-file diff provider: %v", r)
		}
	}()
	return f.p.Resources(ctx, key)
}

// OpenMultiDiff implements extension.Documents: the call is recorded (OpenedMultiDiffs).
func (d documents) OpenMultiDiff(spec extension.MultiDiffSpec) error {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.scm.dead:
		return errors.New("ExtensionDeactivated", "the extension was deactivated")
	case !spec.Valid():
		return errors.New("ExtensionMultiDiffInvalid", "a multi-file diff editor shows the set of a provider or a fixed set, and not both", "kind", spec.Kind, "key", spec.Key)
	case a.content.failOpen != nil:
		return a.content.failOpen
	case spec.Kind != "" && a.content.multiDiffs[spec.Kind] == nil:
		return errors.New("ExtMultiDiffKindUnknown", "no provider makes the sets of this kind", "kind", spec.Kind)
	}
	for i, r := range spec.Resources {
		if r.Original == "" && r.Modified == "" {
			return errors.New("ExtensionMultiDiffInvalid", "a file of a multi-file diff editor needs a document on at least one side", "index", i, "path", r.Path)
		}
	}
	a.content.multi = append(a.content.multi, spec)
	return nil
}

// OpenedMultiDiffs are the calls of OpenMultiDiff, in order.
func (a *API) OpenedMultiDiffs() []extension.MultiDiffSpec {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.content.multi)
}

// MultiDiffProviderFor is the registered provider of a kind, nil when none is.
func (a *API) MultiDiffProviderFor(kind string) *FakeMultiDiff {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.content.multiDiffs[kind]
}
