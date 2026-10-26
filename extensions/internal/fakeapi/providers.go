package fakeapi

import (
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The language feature providers of the fake: it records each registration (the feature, the selector and the provider)
// for a test of an extension to call.

// ProviderRegistration is a provider an extension registered with Languages().Register...Provider.
type ProviderRegistration struct {
	// Feature is completion, hover, signatureHelp, definition, references, documentSymbols, documentHighlights, codeActions,
	// formatting, rename or codeLens.
	Feature string
	// Selector is the documents the provider is for.
	Selector extension.DocumentSelector
	// Triggers are the trigger characters of completion and signature help.
	Triggers []string
	// Provider is the provider as it was given.
	Provider any
	// Disposed tells that the extension took the provider away.
	Disposed bool
}

// Providers lists the providers registered so far, in order, with the ones that were disposed of.
func (a *API) Providers() []*ProviderRegistration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*ProviderRegistration(nil), a.providers...)
}

// register records a provider and returns the function that disposes of it.
func (l languages) register(feature string, sel extension.DocumentSelector, p any, triggers []string) (func(), error) {
	if p == nil {
		return nil, errors.New("FakeProviderInvalid", "a provider needs a value", "feature", feature)
	}
	r := &ProviderRegistration{Feature: feature, Selector: sel, Triggers: triggers, Provider: p}
	l.a.mu.Lock()
	l.a.providers = append(l.a.providers, r)
	l.a.mu.Unlock()
	return func() {
		l.a.mu.Lock()
		r.Disposed = true
		l.a.mu.Unlock()
	}, nil
}

// RegisterCompletionProvider implements extension.Languages.
func (l languages) RegisterCompletionProvider(sel extension.DocumentSelector, p extension.CompletionProvider, triggers ...string) (func(), error) {
	return l.register("completion", sel, p, triggers)
}

// RegisterHoverProvider implements extension.Languages.
func (l languages) RegisterHoverProvider(sel extension.DocumentSelector, p extension.HoverProvider) (func(), error) {
	return l.register("hover", sel, p, nil)
}

// RegisterSignatureHelpProvider implements extension.Languages.
func (l languages) RegisterSignatureHelpProvider(sel extension.DocumentSelector, p extension.SignatureHelpProvider, triggers ...string) (func(), error) {
	return l.register("signatureHelp", sel, p, triggers)
}

// RegisterDefinitionProvider implements extension.Languages.
func (l languages) RegisterDefinitionProvider(sel extension.DocumentSelector, p extension.DefinitionProvider) (func(), error) {
	return l.register("definition", sel, p, nil)
}

// RegisterReferenceProvider implements extension.Languages.
func (l languages) RegisterReferenceProvider(sel extension.DocumentSelector, p extension.ReferenceProvider) (func(), error) {
	return l.register("references", sel, p, nil)
}

// RegisterDocumentSymbolProvider implements extension.Languages.
func (l languages) RegisterDocumentSymbolProvider(sel extension.DocumentSelector, p extension.DocumentSymbolProvider) (func(), error) {
	return l.register("documentSymbols", sel, p, nil)
}

// RegisterDocumentHighlightProvider implements extension.Languages.
func (l languages) RegisterDocumentHighlightProvider(sel extension.DocumentSelector, p extension.DocumentHighlightProvider) (func(), error) {
	return l.register("documentHighlights", sel, p, nil)
}

// RegisterCodeActionProvider implements extension.Languages.
func (l languages) RegisterCodeActionProvider(sel extension.DocumentSelector, p extension.CodeActionProvider) (func(), error) {
	return l.register("codeActions", sel, p, nil)
}

// RegisterFormattingProvider implements extension.Languages.
func (l languages) RegisterFormattingProvider(sel extension.DocumentSelector, p extension.FormattingProvider) (func(), error) {
	return l.register("formatting", sel, p, nil)
}

// RegisterRenameProvider implements extension.Languages.
func (l languages) RegisterRenameProvider(sel extension.DocumentSelector, p extension.RenameProvider) (func(), error) {
	return l.register("rename", sel, p, nil)
}

// RegisterCodeLensProvider implements extension.Languages.
func (l languages) RegisterCodeLensProvider(sel extension.DocumentSelector, p extension.CodeLensProvider) (func(), error) {
	return l.register("codeLens", sel, p, nil)
}

// RefreshCodeLenses implements extension.Languages: the call is counted (CodeLensRefreshes).
func (l languages) RefreshCodeLenses() {
	l.a.mu.Lock()
	l.a.lensRefreshes++
	l.a.mu.Unlock()
}

// CodeLensRefreshes is how many times the extension asked for its code lenses to be asked again.
func (a *API) CodeLensRefreshes() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lensRefreshes
}
