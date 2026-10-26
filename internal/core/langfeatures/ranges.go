package langfeatures

import (
	"context"
	"slices"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/folding"
	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// Folding ranges and selection ranges (SPEC §9.12, "As built (step 12.9)").
//
// Ported from VS Code: src/vs/editor/contrib/folding/browser/syntaxRangeProvider.ts (collectSyntaxRanges) and
// src/vs/editor/contrib/smartSelect/browser/smartSelect.ts (the providers of the selection ranges)

// FoldingRange is a range of lines a provider says can be folded: the first line stays visible, the lines up to EndLine are
// hidden. Lines are counted from 0.
type FoldingRange struct {
	StartLine, EndLine int
	// Kind is "comment", "imports", "region" or empty.
	Kind string
	// CollapsedText is what the folded range shows after its first line instead of the ellipsis; empty for none.
	CollapsedText string
}

// FoldingRangeProvider answers the folding ranges of a document.
type FoldingRangeProvider interface {
	ProvideFoldingRanges(ctx context.Context, doc Document) ([]FoldingRange, error)
}

// RegisterFoldingRanges adds a folding range provider.
func (r *Registry) RegisterFoldingRanges(owner string, sel Selector, p FoldingRangeProvider) (remove func()) {
	return r.folding.add(owner, sel, p)
}

// FoldingRanges asks every folding range provider of the document. The answers of the providers that answered with a list, even an
// empty one, are joined as syntax ranges ranked by the order of the providers, and ok is true; it is false when no provider
// answered (none matches, or all failed or offer nothing), which is when the editor folds by indentation. Ranges that are
// not inside the document, that start after the first line or that are not at least two lines are left out.
func (r *Registry) FoldingRanges(ctx context.Context, doc Document) (ranges []folding.SyntaxRange, collapsed map[int]string, ok bool, errs []ProviderError) {
	regs := slices.DeleteFunc(r.folding.ordered(doc), func(reg Registered[FoldingRangeProvider]) bool { return !offered(reg.Provider, FeatureFoldingRanges) })
	rs := fanOut(ctx, regs, func(ctx context.Context, p FoldingRangeProvider) ([]FoldingRange, error) {
		return p.ProvideFoldingRanges(ctx, doc)
	})
	lines := doc.Text.LineCount()
	collapsed = map[int]string{}
	for rank, res := range rs {
		if res.err != nil || res.val == nil {
			continue // a failed provider, or one that answered null (VS Code: not an array), is no answer
		}
		ok = true
		for _, fr := range res.val {
			if fr.StartLine >= 0 && fr.EndLine > fr.StartLine && fr.EndLine < lines {
				ranges = append(ranges, folding.SyntaxRange{Start: fr.StartLine, End: fr.EndLine, Kind: fr.Kind, Rank: rank})
				if fr.CollapsedText != "" {
					collapsed[fr.StartLine] = fr.CollapsedText
				}
			}
		}
	}
	return ranges, collapsed, ok, errorsOf(rs)
}

// SelectionRangeProvider answers, for positions of a document, the ranges around each, from the smallest to the largest.
type SelectionRangeProvider interface {
	ProvideSelectionRanges(ctx context.Context, doc Document, positions []Position) ([][]Range, error)
}

// RegisterSelectionRanges adds a selection range provider.
func (r *Registry) RegisterSelectionRanges(owner string, sel Selector, p SelectionRangeProvider) (remove func()) {
	return r.selection.add(owner, sel, p)
}

// SelectionRanges asks every selection range provider of the document and joins their ranges for each position (sorted and
// made into steps by edit.ProvideSelectionRanges). ok is false when no provider matches the document.
func (r *Registry) SelectionRanges(ctx context.Context, doc Document, positions []Position) (SelectionRangeSet, bool, []ProviderError) {
	regs := slices.DeleteFunc(r.selection.ordered(doc), func(reg Registered[SelectionRangeProvider]) bool {
		return !offered(reg.Provider, FeatureSelectionRanges)
	})
	if len(regs) == 0 {
		return SelectionRangeSet{}, false, nil
	}
	rs := fanOut(ctx, regs, func(ctx context.Context, p SelectionRangeProvider) ([][]Range, error) {
		return p.ProvideSelectionRanges(ctx, doc, positions)
	})
	set := SelectionRangeSet{positions: slices.Clone(positions), ranges: make([][]Range, len(positions))}
	for _, res := range rs {
		if res.err != nil || len(res.val) != len(positions) {
			continue
		}
		for i, rr := range res.val {
			set.ranges[i] = append(set.ranges[i], rr...)
		}
	}
	return set, true, errorsOf(rs)
}

// SelectionRangeSet is the selection ranges the providers gave for some positions. It is an edit.SelectionRangeProvider for
// those positions, so that Expand Selection steps through the ranges of the providers and the word, bracket and line ranges
// together.
type SelectionRangeSet struct {
	positions []Position
	ranges    [][]Range
}

// Positions are the positions the set was asked for.
func (s SelectionRangeSet) Positions() []Position { return s.positions }

// SelectionRanges implements edit.SelectionRangeProvider: for a position the set was asked about, its ranges; none for others.
func (s SelectionRangeSet) SelectionRanges(_ edit.Model, positions []buffer.Position) [][]buffer.Range {
	out := make([][]buffer.Range, len(positions))
	for i, p := range positions {
		if k := slices.Index(s.positions, p); k >= 0 {
			out[i] = s.ranges[k]
		}
	}
	return out
}

// FoldingRangesFromLSP converts the answer to a folding range request. Lines are lines in every position encoding. A null
// answer is nil, an empty list is empty: the first leaves the folding to the indentation, the second means there is nothing
// to fold.
func FoldingRangesFromLSP(ranges []lsp.FoldingRange) []FoldingRange {
	if ranges == nil {
		return nil
	}
	out := make([]FoldingRange, len(ranges))
	for i, r := range ranges {
		out[i] = FoldingRange{StartLine: r.StartLine, EndLine: r.EndLine, Kind: r.Kind, CollapsedText: r.CollapsedText}
	}
	return out
}

// SelectionRangesFromLSP converts the answer to a selection range request: the chain of parents of each position becomes its
// list of ranges, the smallest first.
func SelectionRangesFromLSP(enc lsp.Encoding, doc Document, answer []lsp.SelectionRange) [][]Range {
	c := newConverter(context.Background(), enc, doc, nil)
	out := make([][]Range, len(answer))
	for i := range answer {
		for sr := &answer[i]; sr != nil; sr = sr.Parent {
			out[i] = append(out[i], c.own(sr.Range))
		}
	}
	return out
}

// ProvideFoldingRanges implements FoldingRangeProvider.
func (sp *ServerProvider) ProvideFoldingRanges(ctx context.Context, doc Document) ([]FoldingRange, error) {
	c := sp.server()
	if c == nil || !sp.Offers(FeatureFoldingRanges) {
		return nil, nil
	}
	var ranges []lsp.FoldingRange
	if err := c.Call(ctx, lsp.MethodFoldingRange, lsp.FoldingRangeParams{TextDocument: textDocument(doc)}, &ranges); err != nil {
		return nil, err
	}
	return FoldingRangesFromLSP(ranges), nil
}

// ProvideSelectionRanges implements SelectionRangeProvider.
func (sp *ServerProvider) ProvideSelectionRanges(ctx context.Context, doc Document, positions []Position) ([][]Range, error) {
	c := sp.server()
	if c == nil || !sp.Offers(FeatureSelectionRanges) {
		return nil, nil
	}
	enc := c.Encoding()
	params := lsp.SelectionRangeParams{TextDocument: textDocument(doc), Positions: make([]lsp.Position, len(positions))}
	for i, p := range positions {
		params.Positions[i] = enc.PositionToLSP(doc.Text, p)
	}
	var answer []lsp.SelectionRange
	if err := c.Call(ctx, lsp.MethodSelectionRange, params, &answer); err != nil {
		return nil, err
	}
	if len(answer) != len(positions) {
		return nil, nil
	}
	return SelectionRangesFromLSP(enc, doc, answer), nil
}

// FoldingProvider is the folding.Provider of a document whose language has folding range providers: it asks them, and the
// folding model gets their ranges; when none answers, or editor.foldingStrategy says "indentation", it is the Fallback (the
// provider by indentation and markers) that finds the regions. The document comes through the updater's Tag
// (folding.UpdaterOptions): a langfeatures.Document of the text that is being computed.
//
// Ported from VS Code: src/vs/editor/contrib/folding/browser/syntaxRangeProvider.ts (SyntaxRangeProvider.compute: the
// fallback provider is used when all the providers return null) and folding.ts (the folding strategy)
type FoldingProvider struct {
	Registry *Registry
	// Fallback finds the regions without a provider; it is also used for the strategy "indentation".
	Fallback folding.Provider
	// Limit is editor.foldingMaximumRegions.
	Limit folding.Limit
	// Strategy gives editor.foldingStrategy ("auto" or "indentation"); nil is "auto".
	Strategy func() string
	// Log is told of the providers that failed.
	Log func(message string, err error)
}

// Compute implements folding.Provider.
func (p FoldingProvider) Compute(ctx context.Context, text folding.Text, tabSize int) (*folding.Regions, error) {
	doc, ok := folding.TagOf(ctx).(Document)
	if !ok || p.Registry == nil || (p.Strategy != nil && p.Strategy() == "indentation") {
		return p.Fallback.Compute(ctx, text, tabSize)
	}
	ranges, collapsed, answered, errs := p.Registry.FoldingRanges(ctx, doc)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if p.Log != nil {
		for _, e := range errs {
			p.Log("folding range provider "+e.Owner+" failed", e.Err)
		}
	}
	if !answered {
		return p.Fallback.Compute(ctx, text, tabSize)
	}
	regions, err := folding.Sanitize(ranges, p.Limit)
	if err != nil {
		return nil, err
	}
	regions.SetCollapsedTexts(collapsed)
	return regions, nil
}
