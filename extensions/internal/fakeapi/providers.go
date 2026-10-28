package fakeapi

import (
	"context"
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

// LocationRequest is a call of Languages().References or Implementations.
type LocationRequest struct {
	// Feature is references or implementations.
	Feature string
	// URI and Position are what the extension asked about.
	URI      string
	Position extension.Position
}

// AnswerLocations sets what Languages().References (feature "references") or Implementations ("implementations") answers: fn
// is called on the goroutine of the request with the URI and the position, and its answer reaches the extension's done function
// on the next Pump, as the window hands it over on the UI thread. Without it the feature fails with ExtensionNoProvider, as a
// language with no server does.
func (a *API) AnswerLocations(feature string, fn func(uri string, pos extension.Position) ([]extension.Location, error)) {
	a.mu.Lock()
	if a.locationAnswers == nil {
		a.locationAnswers = map[string]func(string, extension.Position) ([]extension.Location, error){}
	}
	a.locationAnswers[feature] = fn
	a.mu.Unlock()
}

// LocationRequests are the calls of References and Implementations so far, the first first.
func (a *API) LocationRequests() []LocationRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]LocationRequest(nil), a.locationRequests...)
}

// References implements extension.Languages.
func (l languages) References(ctx context.Context, uri string, pos extension.Position, done func([]extension.Location, error)) error {
	return l.locations(ctx, "references", uri, pos, done)
}

// Implementations implements extension.Languages.
func (l languages) Implementations(ctx context.Context, uri string, pos extension.Position, done func([]extension.Location, error)) error {
	return l.locations(ctx, "implementations", uri, pos, done)
}

func (l languages) locations(ctx context.Context, feature, uri string, pos extension.Position, done func([]extension.Location, error)) error {
	if done == nil {
		return errors.New("FakeRequestInvalid", "a request for locations needs a function to call with the answer", "feature", feature)
	}
	a := l.a
	a.mu.Lock()
	a.locationRequests = append(a.locationRequests, LocationRequest{Feature: feature, URI: uri, Position: pos})
	fn := a.locationAnswers[feature]
	a.mu.Unlock()
	go func() {
		var locs []extension.Location
		var err error
		switch {
		case fn == nil:
			err = errors.New("ExtensionNoProvider", "the fake has no answer for this feature", "feature", feature)
		case ctx.Err() != nil:
			err = ctx.Err()
		default:
			locs, err = fn(uri, pos)
		}
		a.Post(func() { done(locs, err) })
	}()
	return nil
}
