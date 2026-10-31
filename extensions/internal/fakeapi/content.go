package fakeapi

import (
	"context"
	"regexp"
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The provided documents of the fake (SPEC §10.30 "Diffs of changes", step 24.7): the providers an extension registered, the diffs and
// documents it opened, and the tabs a test sets, in memory and with the rules of the host (internal/gui/exthost/content.go): the same
// errors, the same disposal with the activation. extension/contenttest runs one suite on this and on the host.

// contentState is the state of the fake for provided documents; the API's mutex guards it.
type contentState struct {
	providers map[string]*FakeContent
	// multiDiffs are the providers of the multi-file diff editor by kind, and multi the editors the extension opened.
	multiDiffs map[string]*FakeMultiDiff
	multi      []extension.MultiDiffSpec
	diffs      []extension.DiffSpec
	opened     []extension.OpenSpec
	tabs       []extension.DocumentTab
	closed     []string
	// failOpen makes OpenDiff and OpenDocument return this error.
	failOpen error
}

// FakeContent is a provider the extension registered: a test reads the text it makes, and what the extension said changed.
type FakeContent struct {
	a        *API
	p        extension.ContentProvider
	changed  [][]string
	disposed bool
}

var reScheme = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ProvideContent implements extension.Documents with the errors of the host.
func (d documents) ProvideContent(p extension.ContentProvider) (extension.ContentRegistration, error) {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.scm.dead:
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	case !reScheme.MatchString(p.Scheme) || p.Scheme == "file" || p.Scheme == "untitled" || p.Scheme == "emetos-virtual":
		return nil, errors.New("ExtensionContentSchemeInvalid", "the scheme of provided documents is a word that is not a scheme of the core", "scheme", p.Scheme)
	case p.Text == nil:
		return nil, errors.New("ExtensionContentProviderInvalid", "a content provider needs a function that makes the text", "scheme", p.Scheme)
	case a.content.providers[p.Scheme] != nil:
		return nil, errors.New("ExtContentSchemeTaken", "the window tab has a content provider for this scheme already", "scheme", p.Scheme)
	}
	if a.content.providers == nil {
		a.content.providers = map[string]*FakeContent{}
	}
	f := &FakeContent{a: a, p: p}
	a.content.providers[p.Scheme] = f
	return f, nil
}

// Changed implements extension.ContentRegistration: it is recorded (ContentChanges).
func (f *FakeContent) Changed(uris ...string) {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	if !f.disposed {
		f.changed = append(f.changed, slices.Clone(uris))
	}
}

// Dispose implements extension.ContentRegistration.
func (f *FakeContent) Dispose() {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	f.dispose()
}

func (f *FakeContent) dispose() {
	if f.disposed {
		return
	}
	f.disposed = true
	if f.a.content.providers[f.p.Scheme] == f {
		delete(f.a.content.providers, f.p.Scheme)
	}
}

// ContentChanges are the calls of Changed of the provider, in order.
func (f *FakeContent) ContentChanges() [][]string {
	f.a.mu.Lock()
	defer f.a.mu.Unlock()
	return slices.Clone(f.changed)
}

// ContentText asks the provider of the URI's scheme for the text, as the window does when it opens the document; it fails with
// ExtContentUnknown when no provider has the scheme.
func (a *API) ContentText(ctx context.Context, uri string) (string, error) {
	scheme, _, ok := extension.SplitContentURI(uri)
	a.mu.Lock()
	f := a.content.providers[scheme]
	a.mu.Unlock()
	if !ok || f == nil {
		return "", errors.New("ExtContentUnknown", "no content provider makes this document", "uri", uri)
	}
	return a.safeText(ctx, f, uri)
}

// safeText calls Text and contains a panic as the host does: the extension is off, and the caller gets an error.
func (a *API) safeText(ctx context.Context, f *FakeContent, uri string) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			a.scmFail("content provider", r)
			text, err = "", errors.NewErrf("ExtensionContentPanicked", "it panicked in the Text of a content provider: %v", r)
		}
	}()
	return f.p.Text(ctx, uri)
}

// ContentLabel is the label the provider of the URI gives the document ("" when there is no provider).
func (a *API) ContentLabel(uri string) string {
	scheme, _, _ := extension.SplitContentURI(uri)
	a.mu.Lock()
	f := a.content.providers[scheme]
	a.mu.Unlock()
	if f == nil || f.p.Label == nil {
		return ""
	}
	return f.p.Label(uri)
}

// ContentLanguage is the language the provider of the URI gives the document ("" when there is no provider or it says none).
func (a *API) ContentLanguage(uri string) string {
	scheme, _, _ := extension.SplitContentURI(uri)
	a.mu.Lock()
	f := a.content.providers[scheme]
	a.mu.Unlock()
	if f == nil || f.p.Language == nil {
		return ""
	}
	return f.p.Language(uri)
}

// checkSide validates a URI of a diff or document the way the host does.
func (a *API) checkSide(uri string) error {
	if scheme, _, ok := extension.SplitContentURI(uri); ok && a.content.providers[scheme] == nil {
		return errors.New("ExtContentUnknown", "no content provider makes this document", "uri", uri)
	}
	return nil
}

// OpenDiff implements extension.Documents: the call is recorded (OpenedDiffs).
func (d documents) OpenDiff(spec extension.DiffSpec) error {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.scm.dead:
		return errors.New("ExtensionDeactivated", "the extension was deactivated")
	case spec.Left == "" || spec.Right == "":
		return errors.New("ExtensionDiffInvalid", "a diff needs a document on each side", "left", spec.Left, "right", spec.Right)
	case a.content.failOpen != nil:
		return a.content.failOpen
	}
	if err := a.checkSide(spec.Left); err != nil {
		return err
	}
	if err := a.checkSide(spec.Right); err != nil {
		return err
	}
	a.content.diffs = append(a.content.diffs, spec)
	return nil
}

// OpenDocument implements extension.Documents: the call is recorded (OpenedDocuments).
func (d documents) OpenDocument(spec extension.OpenSpec) error {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.scm.dead:
		return errors.New("ExtensionDeactivated", "the extension was deactivated")
	case spec.URI == "":
		return errors.New("ExtensionDocumentInvalid", "a document to open needs a URI")
	case a.content.failOpen != nil:
		return a.content.failOpen
	}
	if err := a.checkSide(spec.URI); err != nil {
		return err
	}
	a.content.opened = append(a.content.opened, spec)
	return nil
}

// OpenedDiffs are the calls of OpenDiff, in order.
func (a *API) OpenedDiffs() []extension.DiffSpec {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.content.diffs)
}

// OpenedDocuments are the calls of OpenDocument, in order.
func (a *API) OpenedDocuments() []extension.OpenSpec {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.content.opened)
}

// FailOpen makes OpenDiff and OpenDocument fail with err; nil makes them work again.
func (a *API) FailOpen(err error) {
	a.mu.Lock()
	a.content.failOpen = err
	a.mu.Unlock()
}

// Tabs implements extension.Documents: the tabs a test set with SetTabs.
func (d documents) Tabs() []extension.DocumentTab {
	d.a.mu.Lock()
	defer d.a.mu.Unlock()
	return slices.Clone(d.a.content.tabs)
}

// SetTabs sets the tabs Documents().Tabs lists.
func (a *API) SetTabs(tabs ...extension.DocumentTab) {
	a.mu.Lock()
	a.content.tabs = slices.Clone(tabs)
	a.mu.Unlock()
}

// CloseTabs implements extension.Documents: the tabs that are in the list close (they leave it, and ClosedTabs records their ids).
func (d documents) CloseTabs(tabs ...extension.DocumentTab) int {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, t := range tabs {
		at := slices.IndexFunc(a.content.tabs, func(x extension.DocumentTab) bool { return x.ID == t.ID })
		if at < 0 {
			continue
		}
		a.content.tabs = slices.Delete(a.content.tabs, at, at+1)
		a.content.closed = append(a.content.closed, t.ID)
		n++
	}
	return n
}

// ClosedTabs are the ids of the tabs CloseTabs closed, in order.
func (a *API) ClosedTabs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.content.closed)
}

// ContentProviderFor is the registered provider of a scheme, nil when none is.
func (a *API) ContentProviderFor(scheme string) *FakeContent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.content.providers[scheme]
}
