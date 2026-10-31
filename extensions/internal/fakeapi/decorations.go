package fakeapi

import (
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The File Decorations API of the fake (SPEC §10.30, step 24.6): the providers an extension registered, in memory, with the rules
// of the host (internal/gui/exthost/decorations.go): the same errors, the same disposal with the activation, an invalid decoration
// dropped, a panic of the provider taking the extension off. extension/decorationstest runs one suite on this and on the host.

// decorationsState is the decorations state of the fake; the API's mutex guards it.
type decorationsState struct {
	// providers are the registered providers, the newest first.
	providers []*FakeDecorations
}

// FakeDecorations is a provider the extension registered: a test asks it what the views would, and reads what the extension fired.
type FakeDecorations struct {
	a        *API
	p        extension.FileDecorationProvider
	fired    [][]string
	disposed bool
}

// Decorations implements extension.API.
func (a *API) Decorations() extension.Decorations { return decorationsAPI{a} }

type decorationsAPI struct{ a *API }

// Register implements extension.Decorations with the errors of the host: ExtensionDeactivated and ExtensionDecorationsInvalid.
func (d decorationsAPI) Register(p extension.FileDecorationProvider) (extension.FileDecorations, error) {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.scm.dead {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	if p == nil {
		return nil, errors.New("ExtensionDecorationsInvalid", "a decorations provider is needed")
	}
	f := &FakeDecorations{a: a, p: p}
	a.decorations.providers = slices.Insert(a.decorations.providers, 0, f)
	return f, nil
}

// FileDecorationProviders are the providers that are registered (not disposed, and not ended with the activation), the newest first.
func (a *API) FileDecorationProviders() []*FakeDecorations {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.decorations.providers)
}

// FileDecoration is the decoration of a file as the views would get it from the newest provider that has a valid one.
func (a *API) FileDecoration(uri string) (extension.FileDecoration, bool) {
	for _, f := range a.FileDecorationProviders() {
		if d, ok := f.Decoration(uri); ok {
			return d, true
		}
	}
	return extension.FileDecoration{}, false
}

// Decoration asks the provider as the host does: an invalid decoration is not given, and a panic takes the extension off.
func (f *FakeDecorations) Decoration(uri string) (d extension.FileDecoration, ok bool) {
	f.a.mu.Lock()
	dead := f.disposed
	f.a.mu.Unlock()
	if dead {
		return extension.FileDecoration{}, false
	}
	defer func() {
		if r := recover(); r != nil {
			d, ok = extension.FileDecoration{}, false
			f.a.scmFail("file decorations provider", r)
		}
	}()
	d, ok = f.p.Decoration(uri)
	if !ok || !extension.ValidBadge(d.Badge) || d.IsEmpty() {
		return extension.FileDecoration{}, false
	}
	return d, true
}

// Fired are the calls of Fire, each with its URIs (none for "everything").
func (f *FakeDecorations) Fired() [][]string {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	return slices.Clone(f.fired)
}

// Fire implements extension.FileDecorations.
func (f *FakeDecorations) Fire(uris ...string) {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	if !f.disposed {
		f.fired = append(f.fired, slices.Clone(uris))
	}
}

// Disposed says that the provider was disposed or the activation ended.
func (f *FakeDecorations) Disposed() bool {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	return f.disposed
}

// Dispose implements extension.FileDecorations.
func (f *FakeDecorations) Dispose() {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	f.disposed = true
	f.a.decorations.providers = slices.DeleteFunc(f.a.decorations.providers, func(o *FakeDecorations) bool { return o == f })
}
