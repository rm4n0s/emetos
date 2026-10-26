package langfeatures

import (
	"context"
	"slices"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// WorkspaceSymbol is a symbol of the workspace that Go to Symbol in Workspace lists (workspace/symbol). Kind is the LSP's
// SymbolKind.
type WorkspaceSymbol struct {
	Name          string
	ContainerName string
	Kind          int
	Deprecated    bool
	// Location is where the symbol is. Its range is the start of the file when Unresolved is true.
	Location Location
	// Unresolved says that the server left the range of the symbol to workspaceSymbol/resolve.
	Unresolved bool
	// Owner is the provider that made the symbol: the id of a language server or of an extension.
	Owner string

	// source is the provider that answered, which resolves the symbol; server is the symbol as the server sent it.
	source WorkspaceSymbolProvider
	server *lsp.WorkspaceSymbol
}

// WorkspaceSymbolProvider answers the symbols of the whole workspace for a query (VS Code's WorkspaceSymbolProvider); it is
// asked for every query typed after # in the quick open.
type WorkspaceSymbolProvider interface {
	ProvideWorkspaceSymbols(ctx context.Context, query string) ([]WorkspaceSymbol, error)
}

// WorkspaceSymbolResolver is a provider that leaves the location of its symbols to be asked for when one is chosen
// (workspaceSymbol/resolve).
type WorkspaceSymbolResolver interface {
	// CanResolveSymbol tells whether the provider can resolve the symbol.
	CanResolveSymbol(sym WorkspaceSymbol) bool
	// ResolveWorkspaceSymbol returns the symbol with its range.
	ResolveWorkspaceSymbol(ctx context.Context, sym WorkspaceSymbol) (WorkspaceSymbol, error)
}

// RegisterWorkspaceSymbols adds a provider of the symbols of the workspace. It has no selector: it is asked whatever
// document is open.
func (r *Registry) RegisterWorkspaceSymbols(owner string, p WorkspaceSymbolProvider) (remove func()) {
	return r.workspaceSymbols.add(owner, Selector{}, p)
}

// WorkspaceSymbols asks every workspace symbol provider for the symbols that match the query and joins the answers in the
// order of the providers.
func (r *Registry) WorkspaceSymbols(ctx context.Context, query string) ([]WorkspaceSymbol, []ProviderError) {
	regs := r.workspaceSymbols.all()
	rs := make([]result[[]WorkspaceSymbol], len(regs))
	var wg sync.WaitGroup
	for i, reg := range regs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := reg.Provider.ProvideWorkspaceSymbols(ctx, query)
			rs[i] = result[[]WorkspaceSymbol]{owner: reg.Owner, provider: reg.Provider, val: v, err: err}
		}()
	}
	wg.Wait()
	var out []WorkspaceSymbol
	for _, res := range rs {
		if res.err != nil {
			continue
		}
		src, _ := res.provider.(WorkspaceSymbolProvider)
		for _, s := range res.val {
			if s.Owner == "" {
				s.Owner = res.owner
			}
			s.source = src
			out = append(out, s)
		}
	}
	return out, errorsOf(rs)
}

// CanResolveWorkspaceSymbol tells whether the provider that answered with the symbol can resolve its range.
func (r *Registry) CanResolveWorkspaceSymbol(sym WorkspaceSymbol) bool {
	rv, ok := sym.source.(WorkspaceSymbolResolver)
	return ok && rv.CanResolveSymbol(sym)
}

// ResolveWorkspaceSymbol asks the provider that answered with the symbol for its range. A symbol that has one, or whose
// provider cannot resolve, is returned as it is.
func (r *Registry) ResolveWorkspaceSymbol(ctx context.Context, sym WorkspaceSymbol) (WorkspaceSymbol, error) {
	rv, ok := sym.source.(WorkspaceSymbolResolver)
	if !sym.Unresolved || !ok || !rv.CanResolveSymbol(sym) {
		return sym, nil
	}
	return rv.ResolveWorkspaceSymbol(ctx, sym)
}

// Feature names a feature the editor asks whether a provider offers, for its context keys and the entries of its menus
// (VS Code's editorHasDefinitionProvider and its kin).
type Feature int

// The features.
const (
	FeatureDefinition Feature = iota
	FeatureDeclaration
	FeatureTypeDefinition
	FeatureImplementation
	FeatureReferences
	FeatureDocumentSymbols
	FeatureDocumentHighlights
	FeatureWorkspaceSymbols
)

// featureOffers is implemented by a provider that serves several features and may not offer all of them: a language server
// registers its provider for every feature and offers those its capabilities name. A provider without it offers the feature
// it was registered for.
type featureOffers interface {
	Offers(f Feature) bool
}

// offered tells whether a provider offers a feature.
func offered(p any, f Feature) bool {
	o, ok := p.(featureOffers)
	return !ok || o.Offers(f)
}

// Has tells whether a provider that matches the document offers the feature (the key editorHasDefinitionProvider and its kin).
func (r *Registry) Has(doc Document, f Feature) bool {
	switch f {
	case FeatureDefinition, FeatureDeclaration, FeatureTypeDefinition, FeatureImplementation:
		return slices.ContainsFunc(r.navigation.ordered(doc), func(reg Registered[NavigationProvider]) bool { return offered(reg.Provider, f) })
	case FeatureReferences:
		return slices.ContainsFunc(r.references.ordered(doc), func(reg Registered[ReferenceProvider]) bool { return offered(reg.Provider, f) })
	case FeatureDocumentSymbols:
		return slices.ContainsFunc(r.symbols.ordered(doc), func(reg Registered[SymbolProvider]) bool { return offered(reg.Provider, f) })
	case FeatureDocumentHighlights:
		return slices.ContainsFunc(r.highlights.ordered(doc), func(reg Registered[HighlightProvider]) bool { return offered(reg.Provider, f) })
	case FeatureWorkspaceSymbols:
		return slices.ContainsFunc(r.workspaceSymbols.all(), func(reg Registered[WorkspaceSymbolProvider]) bool { return offered(reg.Provider, f) })
	}
	return false
}
