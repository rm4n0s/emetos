package langfeatures

import (
	"context"
	"slices"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// ProviderError is the failure of one provider; the others' answers are used all the same (VS Code logs a provider that
// fails and goes on with the rest).
type ProviderError struct {
	Owner string
	Err   error
}

// result is the answer of one provider.
type result[R any] struct {
	owner string
	// provider is the provider that answered.
	provider any
	val      R
	err      error
}

// fanOut calls the providers at the same time, each on a goroutine of its own, and returns their answers in the order of
// the providers. It returns when every provider has answered; a provider that ignores ctx holds it up, which is why the
// providers of an extension are called through a guard that gives up when ctx ends.
func fanOut[P, R any](ctx context.Context, regs []Registered[P], call func(context.Context, P) (R, error)) []result[R] {
	out := make([]result[R], len(regs))
	var wg sync.WaitGroup
	for i, reg := range regs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := call(ctx, reg.Provider)
			out[i] = result[R]{owner: reg.Owner, provider: reg.Provider, val: v, err: err}
		}()
	}
	wg.Wait()
	return out
}

// errorsOf lists the failures among the answers.
func errorsOf[R any](rs []result[R]) []ProviderError {
	var out []ProviderError
	for _, r := range rs {
		if r.err != nil {
			out = append(out, ProviderError{Owner: r.owner, Err: r.err})
		}
	}
	return out
}

// CompletionQuery narrows a completion request.
type CompletionQuery struct {
	// Owners, when not nil, are the only providers asked (by owner): the ones whose lists were incomplete, when a request is
	// made again for them.
	Owners map[string]bool
}

// Completion asks the completion providers of the document. The providers of the best score are asked together; when they
// have no items and none of their lists is incomplete, the next score is asked (VS Code's groups); the items of the
// providers that answered are concatenated in the order of the providers, each marked with its provider. A request for a
// trigger character asks only the providers that have the character as a trigger (VS Code's providerFilter). Sorting is the
// suggest widget's.
func (r *Registry) Completion(ctx context.Context, doc Document, pos Position, trigger CompletionTrigger) (CompletionList, []ProviderError) {
	return r.CompletionFor(ctx, doc, pos, trigger, CompletionQuery{})
}

// CompletionFor is Completion limited by the query.
func (r *Registry) CompletionFor(ctx context.Context, doc Document, pos Position, trigger CompletionTrigger, q CompletionQuery) (CompletionList, []ProviderError) {
	var all CompletionList
	var errs []ProviderError
	for _, group := range r.completion.groups(doc) {
		group = slices.DeleteFunc(slices.Clone(group), func(reg Registered[CompletionProvider]) bool {
			if q.Owners != nil && !q.Owners[reg.Owner] {
				return true
			}
			return trigger.Kind == TriggerCharacter && !hasTrigger(reg, trigger.Character)
		})
		if len(group) == 0 {
			continue
		}
		rs := fanOut(ctx, group, func(ctx context.Context, p CompletionProvider) (CompletionList, error) {
			return p.ProvideCompletion(ctx, doc, pos, trigger)
		})
		errs = append(errs, errorsOf(rs)...)
		for _, res := range rs {
			if res.err != nil {
				continue
			}
			src, _ := res.provider.(CompletionProvider)
			for _, it := range res.val.Items {
				if it.Owner == "" {
					it.Owner = res.owner
				}
				it.source, it.IncompleteList = src, res.val.Incomplete
				all.Items = append(all.Items, it)
			}
			all.Incomplete = all.Incomplete || res.val.Incomplete
		}
		if len(all.Items) > 0 || all.Incomplete || ctx.Err() != nil {
			break
		}
	}
	return all, errs
}

// hasTrigger tells whether the provider starts a request when the character is typed.
func hasTrigger(reg Registered[CompletionProvider], ch string) bool {
	if slices.Contains(reg.Triggers, ch) {
		return true
	}
	tp, ok := reg.Provider.(interface{ CompletionTriggers() []string })
	return ok && slices.Contains(tp.CompletionTriggers(), ch)
}

// Hover asks every hover provider of the document; the answers that have contents are stacked in the order of the
// providers (VS Code orders them by provider and shows them one under the other).
func (r *Registry) Hover(ctx context.Context, doc Document, pos Position) ([]Hover, []ProviderError) {
	rs := fanOut(ctx, r.hover.ordered(doc), func(ctx context.Context, p HoverProvider) (*Hover, error) {
		return p.ProvideHover(ctx, doc, pos)
	})
	var out []Hover
	for _, res := range rs {
		if res.err != nil || res.val == nil || len(res.val.Contents) == 0 {
			continue
		}
		h := *res.val
		if h.Owner == "" {
			h.Owner = res.owner
		}
		out = append(out, h)
	}
	return out, errorsOf(rs)
}

// SignatureHelp asks the signature help providers and takes the answer of the first, in the order of the providers, that
// has signatures.
func (r *Registry) SignatureHelp(ctx context.Context, doc Document, pos Position, sc SignatureContext) (*SignatureHelp, []ProviderError) {
	rs := fanOut(ctx, r.signature.ordered(doc), func(ctx context.Context, p SignatureHelpProvider) (*SignatureHelp, error) {
		return p.ProvideSignatureHelp(ctx, doc, pos, sc)
	})
	for _, res := range rs {
		if res.err == nil && res.val != nil && len(res.val.Signatures) > 0 {
			h := *res.val
			if h.Owner == "" {
				h.Owner = res.owner
			}
			return &h, errorsOf(rs)
		}
	}
	return nil, errorsOf(rs)
}

// Locations asks every navigation provider for a kind of location (definition, declaration, type definition,
// implementation) and joins the answers, in the order of the providers, without the same place twice.
func (r *Registry) Locations(ctx context.Context, doc Document, pos Position, kind NavKind) ([]LocationLink, []ProviderError) {
	rs := fanOut(ctx, r.navigation.ordered(doc), func(ctx context.Context, p NavigationProvider) ([]LocationLink, error) {
		return p.ProvideLocations(ctx, doc, pos, kind)
	})
	type key struct {
		uri document.URI
		rng Range
	}
	seen := map[key]bool{}
	var out []LocationLink
	for _, res := range rs {
		if res.err != nil {
			continue
		}
		for _, l := range res.val {
			k := key{l.Target.URI, l.Target.Range}
			if !seen[k] {
				seen[k] = true
				out = append(out, l)
			}
		}
	}
	return out, errorsOf(rs)
}

// References asks every references provider and joins the answers without the same place twice.
func (r *Registry) References(ctx context.Context, doc Document, pos Position, includeDeclaration bool) ([]Location, []ProviderError) {
	rs := fanOut(ctx, r.references.ordered(doc), func(ctx context.Context, p ReferenceProvider) ([]Location, error) {
		return p.ProvideReferences(ctx, doc, pos, includeDeclaration)
	})
	seen := map[Location]bool{}
	var out []Location
	for _, res := range rs {
		if res.err != nil {
			continue
		}
		for _, l := range res.val {
			if !seen[l] {
				seen[l] = true
				out = append(out, l)
			}
		}
	}
	return out, errorsOf(rs)
}

// DocumentSymbols asks the symbol providers and takes the symbols of the first, in the order of the providers, that has
// any.
func (r *Registry) DocumentSymbols(ctx context.Context, doc Document) ([]DocumentSymbol, []ProviderError) {
	rs := fanOut(ctx, r.symbols.ordered(doc), func(ctx context.Context, p SymbolProvider) ([]DocumentSymbol, error) {
		return p.ProvideDocumentSymbols(ctx, doc)
	})
	for _, res := range rs {
		if res.err == nil && len(res.val) > 0 {
			return res.val, errorsOf(rs)
		}
	}
	return nil, errorsOf(rs)
}

// DocumentHighlights asks the highlight providers and takes the highlights of the first, in the order of the providers,
// that has any.
func (r *Registry) DocumentHighlights(ctx context.Context, doc Document, pos Position) ([]DocumentHighlight, []ProviderError) {
	rs := fanOut(ctx, r.highlights.ordered(doc), func(ctx context.Context, p HighlightProvider) ([]DocumentHighlight, error) {
		return p.ProvideDocumentHighlights(ctx, doc, pos)
	})
	for _, res := range rs {
		if res.err == nil && len(res.val) > 0 {
			return res.val, errorsOf(rs)
		}
	}
	return nil, errorsOf(rs)
}

// CodeActions asks every code action provider and joins the actions in the order of the providers.
func (r *Registry) CodeActions(ctx context.Context, doc Document, req CodeActionRequest) ([]CodeAction, []ProviderError) {
	rs := fanOut(ctx, r.codeActions.ordered(doc), func(ctx context.Context, p CodeActionProvider) ([]CodeAction, error) {
		return p.ProvideCodeActions(ctx, doc, req)
	})
	var out []CodeAction
	for _, res := range rs {
		if res.err != nil {
			continue
		}
		for _, a := range res.val {
			if a.Owner == "" {
				a.Owner = res.owner
			}
			out = append(out, a)
		}
	}
	return out, errorsOf(rs)
}

// Formatters lists the formatters of the document in the order of the providers.
func (r *Registry) Formatters(doc Document) []Registered[FormattingProvider] {
	return r.formatting.ordered(doc)
}

// PickFormatter chooses the formatter for the document: the one registered by defaultFormatter (editor.defaultFormatter,
// the id of an extension) when there is one, else the first of the order. ok is false when the document has no formatter;
// others says how many more there are, which a caller may tell the user (VS Code asks them to choose a default).
func (r *Registry) PickFormatter(doc Document, defaultFormatter string) (reg Registered[FormattingProvider], others int, ok bool) {
	all := r.formatting.ordered(doc)
	if len(all) == 0 {
		return reg, 0, false
	}
	for _, f := range all {
		if defaultFormatter != "" && f.Owner == defaultFormatter {
			return f, len(all) - 1, true
		}
	}
	return all[0], len(all) - 1, true
}

// FormatDocument formats the document with the formatter PickFormatter chooses. It fails with NoFormatter when the document
// has none; the formatter's own error is returned as it is.
func (r *Registry) FormatDocument(ctx context.Context, doc Document, opts FormattingOptions, defaultFormatter string) ([]TextEdit, error) {
	reg, _, ok := r.PickFormatter(doc, defaultFormatter)
	if !ok {
		return nil, errors.New("NoFormatter", "there is no formatter for the document", "uri", doc.URI.String(), "language", doc.LanguageID)
	}
	return reg.Provider.ProvideFormatting(ctx, doc, opts)
}

// FormatRange formats a range of the document with the formatter PickFormatter chooses among those that format ranges. It
// fails with NoFormatter when the document has none.
func (r *Registry) FormatRange(ctx context.Context, doc Document, rng Range, opts FormattingOptions, defaultFormatter string) ([]TextEdit, error) {
	var capable []Registered[RangeFormattingProvider]
	for _, f := range r.formatting.ordered(doc) {
		if rf, ok := f.Provider.(RangeFormattingProvider); ok {
			capable = append(capable, Registered[RangeFormattingProvider]{Owner: f.Owner, Provider: rf, Score: f.Score})
		}
	}
	if len(capable) == 0 {
		return nil, errors.New("NoFormatter", "there is no range formatter for the document", "uri", doc.URI.String(), "language", doc.LanguageID)
	}
	pick := capable[0]
	for _, f := range capable {
		if defaultFormatter != "" && f.Owner == defaultFormatter {
			pick = f
			break
		}
	}
	return pick.Provider.ProvideRangeFormatting(ctx, doc, rng, opts)
}

// PrepareRename asks the rename providers in order and answers with the first that has an answer; an error of a provider is
// a refusal and is returned as it is.
func (r *Registry) PrepareRename(ctx context.Context, doc Document, pos Position) (*PrepareRename, error) {
	for _, reg := range r.rename.ordered(doc) {
		res, err := reg.Provider.PrepareRename(ctx, doc, pos)
		if err != nil || res != nil {
			return res, err
		}
	}
	return nil, nil
}

// Rename asks the rename providers in order and answers with the edit of the first that has one.
func (r *Registry) Rename(ctx context.Context, doc Document, pos Position, newName string) (*WorkspaceEdit, error) {
	for _, reg := range r.rename.ordered(doc) {
		edit, err := reg.Provider.ProvideRename(ctx, doc, pos, newName)
		if err != nil || edit != nil {
			return edit, err
		}
	}
	return nil, nil
}
