package langfeatures

import (
	"context"
	"slices"

	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// Code lenses and inlay hints (SPEC §9.12, "As built (step 12.9)").
//
// Ported from VS Code: src/vs/editor/contrib/codelens/browser/codelens.ts (getCodeLensModel, the order of the lenses and
// their resolve) and src/vs/editor/contrib/inlayHints/browser/inlayHints.ts (InlayHintsFragments, the order of the hints and
// their resolve)

// CodeLens is a command shown above a line. Command is nil until the lens is resolved (CanResolveCodeLens, ResolveCodeLens).
type CodeLens struct {
	// Range is the line, and the part of it, that the lens belongs to.
	Range Range
	// Command is what the lens shows and runs; nil while the lens is not resolved.
	Command *Command
	// Owner is the provider that made the lens: the id of a language server or of an extension.
	Owner string

	// source is the provider that answered with the lens, which resolves it; server is the lens as the language server sent
	// it, which the server gets back to resolve it.
	source CodeLensProvider
	server *lsp.CodeLens
}

// Resolved says whether the lens has its command.
func (l CodeLens) Resolved() bool { return l.Command != nil }

// CodeLensProvider answers the code lenses of a document.
type CodeLensProvider interface {
	ProvideCodeLenses(ctx context.Context, doc Document) ([]CodeLens, error)
}

// CodeLensResolver is a CodeLensProvider that answers lenses without their command and fills it in when the lens comes into
// view.
type CodeLensResolver interface {
	// CanResolveCodeLens tells whether the provider resolves the lens.
	CanResolveCodeLens(l CodeLens) bool
	// ResolveCodeLens gives the lens with its command.
	ResolveCodeLens(ctx context.Context, doc Document, l CodeLens) (CodeLens, error)
}

// RegisterCodeLens adds a code lens provider.
func (r *Registry) RegisterCodeLens(owner string, sel Selector, p CodeLensProvider) (remove func()) {
	return r.codeLens.add(owner, sel, p)
}

// CodeLenses asks every code lens provider of the document and joins the lenses, ordered by the place they belong to (a lens
// of an earlier provider first when two are on one line), without the lenses that are on no line.
func (r *Registry) CodeLenses(ctx context.Context, doc Document) ([]CodeLens, []ProviderError) {
	rs := fanOut(ctx, r.codeLens.ordered(doc), func(ctx context.Context, p CodeLensProvider) ([]CodeLens, error) {
		return p.ProvideCodeLenses(ctx, doc)
	})
	var out []CodeLens
	for _, res := range rs {
		if res.err != nil {
			continue
		}
		src, _ := res.provider.(CodeLensProvider)
		for _, l := range res.val {
			if l.Owner == "" {
				l.Owner = res.owner
			}
			l.source = src
			out = append(out, l)
		}
	}
	slices.SortStableFunc(out, func(a, b CodeLens) int { return comparePositions(a.Range.Start, b.Range.Start) })
	return out, errorsOf(rs)
}

// CanResolveCodeLens tells whether the provider that answered with the lens can resolve it.
func (r *Registry) CanResolveCodeLens(l CodeLens) bool {
	rv, ok := l.source.(CodeLensResolver)
	return ok && !l.Resolved() && rv.CanResolveCodeLens(l)
}

// ResolveCodeLens asks the provider that answered with the lens for its command. A lens that has its command, or whose
// provider cannot resolve it, is returned as it was.
func (r *Registry) ResolveCodeLens(ctx context.Context, doc Document, l CodeLens) (CodeLens, error) {
	rv, ok := l.source.(CodeLensResolver)
	if !ok || l.Resolved() || !rv.CanResolveCodeLens(l) {
		return l, nil
	}
	return rv.ResolveCodeLens(ctx, doc, l)
}

// InlayHintKind says what a hint is for; the numbers are the LSP's InlayHintKind.
type InlayHintKind int

// The kinds of inlay hint.
const (
	// InlayHintOther is a hint of no kind.
	InlayHintOther InlayHintKind = 0
	// InlayHintType is the type of a variable, a return value or an expression.
	InlayHintType InlayHintKind = 1
	// InlayHintParameter is the name of a parameter at a call.
	InlayHintParameter InlayHintKind = 2
)

// InlayHintPart is a part of the label of an inlay hint, with a tooltip of its own and a place to go to or a command to run.
type InlayHintPart struct {
	Value    string
	Tooltip  *Markup
	Location *Location
	Command  *Command
}

// InlayHint is a label shown between two characters of the text (a parameter name, a type) that is not part of the text.
type InlayHint struct {
	// Position is where the hint is shown, before the character at the position.
	Position Position
	// Parts is the label: one part for a plain label, several when the parts have tooltips, places or commands.
	Parts []InlayHintPart
	Kind  InlayHintKind
	// Tooltip is the tooltip of the hint as a whole.
	Tooltip *Markup
	// PaddingLeft and PaddingRight ask for room around the hint.
	PaddingLeft, PaddingRight bool
	// TextEdits are made when the hint is double clicked, in place of the hint's own commands.
	TextEdits []TextEdit
	// Owner is the provider that made the hint.
	Owner string

	// source is the provider that answered with the hint, which resolves it; server is the hint as the language server sent
	// it, which the server gets back to resolve it.
	source InlayHintProvider
	server *lsp.InlayHint
}

// Label is the text of the hint, the parts joined.
func (h InlayHint) Label() string {
	if len(h.Parts) == 1 {
		return h.Parts[0].Value
	}
	n := 0
	for _, p := range h.Parts {
		n += len(p.Value)
	}
	b := make([]byte, 0, n)
	for _, p := range h.Parts {
		b = append(b, p.Value...)
	}
	return string(b)
}

// InlayHintProvider answers the inlay hints of a range of a document.
type InlayHintProvider interface {
	ProvideInlayHints(ctx context.Context, doc Document, r Range) ([]InlayHint, error)
}

// InlayHintResolver is an InlayHintProvider that answers hints without their tooltips, places and commands and fills them in
// when the pointer rests on the hint.
type InlayHintResolver interface {
	// CanResolveInlayHint tells whether the provider resolves the hint.
	CanResolveInlayHint(h InlayHint) bool
	// ResolveInlayHint gives the hint with what the server left out.
	ResolveInlayHint(ctx context.Context, doc Document, h InlayHint) (InlayHint, error)
}

// RegisterInlayHints adds an inlay hint provider.
func (r *Registry) RegisterInlayHints(owner string, sel Selector, p InlayHintProvider) (remove func()) {
	return r.inlayHints.add(owner, sel, p)
}

// InlayHints asks every inlay hint provider of the document for the hints of a range and joins them, ordered by position (the
// hints of an earlier provider first at one place), without the hints whose label is empty.
func (r *Registry) InlayHints(ctx context.Context, doc Document, rng Range) ([]InlayHint, []ProviderError) {
	rs := fanOut(ctx, r.inlayHints.ordered(doc), func(ctx context.Context, p InlayHintProvider) ([]InlayHint, error) {
		return p.ProvideInlayHints(ctx, doc, rng)
	})
	var out []InlayHint
	for _, res := range rs {
		if res.err != nil {
			continue
		}
		src, _ := res.provider.(InlayHintProvider)
		for _, h := range res.val {
			if h.Label() == "" {
				continue
			}
			if h.Owner == "" {
				h.Owner = res.owner
			}
			h.source = src
			out = append(out, h)
		}
	}
	slices.SortStableFunc(out, func(a, b InlayHint) int { return comparePositions(a.Position, b.Position) })
	return out, errorsOf(rs)
}

// CanResolveInlayHint tells whether the provider that answered with the hint can resolve it.
func (r *Registry) CanResolveInlayHint(h InlayHint) bool {
	rv, ok := h.source.(InlayHintResolver)
	return ok && rv.CanResolveInlayHint(h)
}

// ResolveInlayHint asks the provider that answered with the hint for what it left out. A hint whose provider cannot resolve it
// is returned as it was.
func (r *Registry) ResolveInlayHint(ctx context.Context, doc Document, h InlayHint) (InlayHint, error) {
	rv, ok := h.source.(InlayHintResolver)
	if !ok || !rv.CanResolveInlayHint(h) {
		return h, nil
	}
	return rv.ResolveInlayHint(ctx, doc, h)
}

// CodeLensesFromLSP converts the answer to a code lens request; owner is the id of the server.
func CodeLensesFromLSP(enc lsp.Encoding, doc Document, owner string, lenses []lsp.CodeLens) []CodeLens {
	c := newConverter(context.Background(), enc, doc, nil)
	out := make([]CodeLens, 0, len(lenses))
	for i := range lenses {
		l := &lenses[i]
		out = append(out, CodeLens{Range: c.own(l.Range), Command: commandFromLSP(l.Command, owner), Owner: owner, server: l})
	}
	return out
}

// InlayHintsFromLSP converts the answer to an inlay hint request; owner is the id of the server, texts says how to read the
// text of the files that the labels point to.
func InlayHintsFromLSP(ctx context.Context, enc lsp.Encoding, doc Document, texts TextSource, owner string, hints []lsp.InlayHint) []InlayHint {
	c := newConverter(ctx, enc, doc, texts)
	out := make([]InlayHint, 0, len(hints))
	for i := range hints {
		out = append(out, inlayHintFrom(c, owner, &hints[i]))
	}
	return out
}

func inlayHintFrom(c *converter, owner string, h *lsp.InlayHint) InlayHint {
	out := InlayHint{
		Position: c.position(c.doc.URI, h.Position), Kind: InlayHintKind(h.Kind), Tooltip: markup(h.Tooltip),
		PaddingLeft: h.PaddingLeft, PaddingRight: h.PaddingRight, Owner: owner,
		TextEdits: TextEditsFromLSP(c.enc, c.doc, h.TextEdits), server: h,
	}
	if h.Label.Parts == nil {
		out.Parts = []InlayHintPart{{Value: h.Label.Text}}
		return out
	}
	for _, p := range h.Label.Parts {
		part := InlayHintPart{Value: p.Value, Tooltip: markup(p.Tooltip), Command: commandFromLSP(p.Command, owner)}
		if p.Location != nil {
			if loc, ok := c.location(*p.Location); ok {
				part.Location = &loc
			}
		}
		out.Parts = append(out.Parts, part)
	}
	return out
}

// ProvideCodeLenses implements CodeLensProvider.
func (sp *ServerProvider) ProvideCodeLenses(ctx context.Context, doc Document) ([]CodeLens, error) {
	c := sp.server()
	if c == nil || !sp.Offers(FeatureCodeLens) {
		return nil, nil
	}
	var lenses []lsp.CodeLens
	if err := c.Call(ctx, lsp.MethodCodeLens, lsp.CodeLensParams{TextDocument: textDocument(doc)}, &lenses); err != nil {
		return nil, err
	}
	return CodeLensesFromLSP(c.Encoding(), doc, sp.Name, lenses), nil
}

// CanResolveCodeLens implements CodeLensResolver: the server offers codeLens/resolve and sent the lens.
func (sp *ServerProvider) CanResolveCodeLens(l CodeLens) bool {
	c := sp.server()
	if c == nil || l.server == nil {
		return false
	}
	o := c.Capabilities().CodeLensProvider
	return (o != nil && o.ResolveProvider) || len(c.Registered(lsp.MethodCodeLensResolve)) > 0
}

// ResolveCodeLens implements CodeLensResolver: the lens goes back to the server, which fills in its command.
func (sp *ServerProvider) ResolveCodeLens(ctx context.Context, doc Document, l CodeLens) (CodeLens, error) {
	c := sp.server()
	if c == nil || !sp.CanResolveCodeLens(l) {
		return l, nil
	}
	var resolved lsp.CodeLens
	if err := c.Call(ctx, lsp.MethodCodeLensResolve, *l.server, &resolved); err != nil {
		return l, err
	}
	if resolved.Command != nil {
		l.Command = commandFromLSP(resolved.Command, sp.Name)
	}
	l.server = &resolved
	return l, nil
}

// ProvideInlayHints implements InlayHintProvider.
func (sp *ServerProvider) ProvideInlayHints(ctx context.Context, doc Document, r Range) ([]InlayHint, error) {
	c := sp.server()
	if c == nil || !sp.Offers(FeatureInlayHints) {
		return nil, nil
	}
	enc := c.Encoding()
	var hints []lsp.InlayHint
	params := lsp.InlayHintParams{TextDocument: textDocument(doc), Range: enc.RangeToLSP(doc.Text, r)}
	if err := c.Call(ctx, lsp.MethodInlayHint, params, &hints); err != nil {
		return nil, err
	}
	return InlayHintsFromLSP(ctx, enc, doc, sp.Texts, sp.Name, hints), nil
}

// CanResolveInlayHint implements InlayHintResolver: the server offers inlayHint/resolve and sent the hint.
func (sp *ServerProvider) CanResolveInlayHint(h InlayHint) bool {
	c := sp.server()
	if c == nil || h.server == nil {
		return false
	}
	return c.Capabilities().InlayHintProvider.Bool("resolveProvider") || len(c.Registered(lsp.MethodInlayHintResolve)) > 0
}

// ResolveInlayHint implements InlayHintResolver: the hint goes back to the server, which fills in its tooltips, places and
// commands.
func (sp *ServerProvider) ResolveInlayHint(ctx context.Context, doc Document, h InlayHint) (InlayHint, error) {
	c := sp.server()
	if c == nil || !sp.CanResolveInlayHint(h) {
		return h, nil
	}
	var resolved lsp.InlayHint
	if err := c.Call(ctx, lsp.MethodInlayHintResolve, *h.server, &resolved); err != nil {
		return h, err
	}
	got := inlayHintFrom(newConverter(ctx, c.Encoding(), doc, sp.Texts), sp.Name, &resolved)
	// the hint keeps its place, its padding and its source: only what the server left out is taken
	h.Parts, h.Tooltip, h.TextEdits, h.server = got.Parts, got.Tooltip, got.TextEdits, got.server
	return h, nil
}
