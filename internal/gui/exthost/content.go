package exthost

import (
	"context"
	"regexp"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The binding of extension.Documents().ProvideContent, OpenDiff, OpenDocument, Tabs and CloseTabs to the window (SPEC §10.30 "Diffs of
// changes"; after mainThreadDocumentContentProviders.ts and the vscode.diff and vscode.open commands). The window owns the registry of
// the virtual documents of its tab, the opening of editors and the groups; this file validates what an extension gives, contains the
// panics of its provider (which runs off the UI thread) and ends its registrations with the activation.

// ContentHost is the optional part of ScopeOptions.Documents that provided content needs. The window implements it.
type ContentHost interface {
	// ProvideContent registers the provider of a scheme in the registry of the window tab; the functions of the provider are
	// contained already. It fails with the tag ExtContentSchemeTaken when the window tab has the scheme.
	ProvideContent(owner string, p extension.ContentProvider) (extension.ContentRegistration, error)
	// OpenDiff opens the diff editor of two documents, making the provided ones off the UI thread. It fails with the tags of
	// document.ParseURI and with ExtContentUnknown for a URI of the content scheme that no provider makes.
	OpenDiff(spec extension.DiffSpec) error
	// OpenDocument opens a document with a title.
	OpenDocument(spec extension.OpenSpec) error
	// Tabs lists the tabs of the text editors and diff editors, in the order of the groups.
	Tabs() []extension.DocumentTab
	// CloseTabs closes the tabs and returns how many were found.
	CloseTabs(tabs []extension.DocumentTab, how extension.TabClose) int
	// ProvideMultiDiff registers the provider of a kind of sets of the multi-file diff editor; the function of the provider is
	// contained already. It fails with the tag ExtMultiDiffKindTaken when the window tab has the kind.
	ProvideMultiDiff(owner string, p extension.MultiDiffProvider) (extension.MultiDiffRegistration, error)
	// OpenMultiDiff opens the multi-file diff editor of a set, asking the provider for its files off the UI thread. It fails with
	// the tag ExtMultiDiffKindUnknown for a kind no provider has.
	OpenMultiDiff(spec extension.MultiDiffSpec) error
	// Dirty lists the URIs of the open files with unsaved changes, on the UI thread.
	Dirty() []string
	// SaveAll saves the open documents with these URIs and returns when they are written. It blocks and is called off the UI thread.
	SaveAll(ctx context.Context, uris []string) error
}

// contentHost is the ContentHost of the scope's documents, or nil with the error to return.
func (d documentsAPI) contentHost(what string) (ContentHost, error) {
	in := d.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	host, ok := in.scope.opts.Documents.(ContentHost)
	if !ok {
		return nil, errors.New("ExtensionDocumentUnavailable", "the scope cannot "+what, "extension", in.id)
	}
	return host, nil
}

var reContentScheme = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// reservedSchemes are the schemes of the core's documents; a provider cannot take them.
var reservedSchemes = map[string]bool{"file": true, "untitled": true, "emetos-virtual": true}

// ProvideContent implements extension.Documents.
func (d documentsAPI) ProvideContent(p extension.ContentProvider) (extension.ContentRegistration, error) {
	in := d.inst
	host, err := d.contentHost("show provided documents")
	if err != nil {
		return nil, err
	}
	if !reContentScheme.MatchString(p.Scheme) || reservedSchemes[p.Scheme] {
		return nil, errors.New("ExtensionContentSchemeInvalid", "the scheme of provided documents is a word of lower case letters, digits and hyphens that is not a scheme of the core", "extension", in.id, "scheme", p.Scheme)
	}
	if p.Text == nil {
		return nil, errors.New("ExtensionContentProviderInvalid", "a content provider needs a function that makes the text", "extension", in.id, "scheme", p.Scheme)
	}
	reg, err := host.ProvideContent(in.id, d.contain(p))
	if err != nil {
		return nil, err
	}
	h := &contentHandle{inst: in, reg: reg}
	if !in.track(h.Dispose) {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return h, nil
}

// contain wraps the functions of a provider so that a panic in one fails the extension (SPEC §10.15 "Containment") and is an error for the
// caller, which may run on any goroutine.
func (d documentsAPI) contain(p extension.ContentProvider) extension.ContentProvider {
	in := d.inst
	fail := func(where string, r any) error {
		failure := errors.NewErrf("ExtensionContentPanicked", "it panicked in the %s of a content provider: %v", where, r).SetMetadata("extension", in.id, "scope", in.scope.opts.Info.Name, "scheme", p.Scheme)
		in.scope.post(func() { in.scope.fail(in.slot, failure, "content provider") })
		return failure
	}
	out := p
	if p.Label != nil {
		out.Label = func(uri string) (label string) {
			defer func() {
				if r := recover(); r != nil {
					_ = fail("Label", r)
					label = ""
				}
			}()
			if in.isDead() {
				return ""
			}
			return p.Label(uri)
		}
	}
	if p.Language != nil {
		out.Language = func(uri string) (lang string) {
			defer func() {
				if r := recover(); r != nil {
					_ = fail("Language", r)
					lang = ""
				}
			}()
			if in.isDead() {
				return ""
			}
			return p.Language(uri)
		}
	}
	out.Text = func(ctx context.Context, uri string) (text string, err error) {
		defer func() {
			if r := recover(); r != nil {
				text, err = "", fail("Text", r)
			}
		}()
		if in.isDead() {
			return "", errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
		}
		return p.Text(ctx, uri)
	}
	return out
}

// contentHandle is the extension.ContentRegistration of a registered provider.
type contentHandle struct {
	inst *instance
	reg  extension.ContentRegistration
	once sync.Once
}

var _ extension.ContentRegistration = (*contentHandle)(nil)

// Changed implements extension.ContentRegistration.
func (h *contentHandle) Changed(uris ...string) {
	if h.inst.isDead() {
		return
	}
	h.reg.Changed(uris...)
}

// Dispose implements extension.ContentRegistration.
func (h *contentHandle) Dispose() { h.once.Do(h.reg.Dispose) }

// OpenDiff implements extension.Documents.
func (d documentsAPI) OpenDiff(spec extension.DiffSpec) error {
	host, err := d.contentHost("open diff editors")
	if err != nil {
		return err
	}
	if spec.Left == "" || spec.Right == "" {
		return errors.New("ExtensionDiffInvalid", "a diff needs a document on each side", "extension", d.inst.id, "left", spec.Left, "right", spec.Right)
	}
	return host.OpenDiff(spec)
}

// OpenDocument implements extension.Documents.
func (d documentsAPI) OpenDocument(spec extension.OpenSpec) error {
	host, err := d.contentHost("open documents")
	if err != nil {
		return err
	}
	if spec.URI == "" {
		return errors.New("ExtensionDocumentInvalid", "a document to open needs a URI", "extension", d.inst.id)
	}
	return host.OpenDocument(spec)
}

// Tabs implements extension.Documents: none in a scope that cannot show documents.
func (d documentsAPI) Tabs() []extension.DocumentTab {
	host, err := d.contentHost("list tabs")
	if err != nil {
		return nil
	}
	return host.Tabs()
}

// CloseTabs implements extension.Documents.
func (d documentsAPI) CloseTabs(tabs ...extension.DocumentTab) int {
	return d.CloseTabsAs(extension.TabCloseAsk, tabs...)
}

// CloseTabsAs implements extension.Documents.
func (d documentsAPI) CloseTabsAs(how extension.TabClose, tabs ...extension.DocumentTab) int {
	host, err := d.contentHost("close tabs")
	if err != nil || len(tabs) == 0 {
		return 0
	}
	return host.CloseTabs(tabs, how)
}

// Dirty implements extension.Documents: none in a scope that cannot show documents.
func (d documentsAPI) Dirty() []string {
	host, err := d.contentHost("list the documents with unsaved changes")
	if err != nil {
		return nil
	}
	return host.Dirty()
}

// SaveAll implements extension.Documents.
func (d documentsAPI) SaveAll(ctx context.Context, uris []string) error {
	host, err := d.contentHost("save documents")
	if err != nil {
		return err
	}
	if len(uris) == 0 {
		return nil
	}
	return host.SaveAll(ctx, uris)
}
