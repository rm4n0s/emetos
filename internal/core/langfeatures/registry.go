package langfeatures

import (
	"context"
	"path/filepath"
	"slices"
	"sort"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/glob"
)

// Selector says which documents a provider is for (SPEC §9.12): a document matches when its language is one of Languages or
// its path matches Pattern (a VS Code glob over the whole path or over the path relative to Folder), and, when Schemes is
// not empty, its scheme is one of them. A selector with neither a language nor a pattern matches every document.
type Selector struct {
	Languages []string
	Pattern   string
	Schemes   []string
	// Folder is the folder a relative Pattern is matched from.
	Folder string
}

// The scores of a match, as VS Code's: a language or a pattern is more specific than "every document", and the more
// specific providers are asked first.
const (
	scoreNone     = 0
	scoreAll      = 5
	scoreSpecific = 10
)

// Score tells how well a document matches the selector: 0 for none, 5 for every document, 10 for a language or a pattern.
func (s Selector) Score(doc Document) int {
	if len(s.Schemes) > 0 && !slices.Contains(s.Schemes, doc.URI.Scheme) {
		return scoreNone
	}
	if len(s.Languages) == 0 && s.Pattern == "" {
		return scoreAll
	}
	if slices.Contains(s.Languages, doc.LanguageID) {
		return scoreSpecific
	}
	if s.Pattern != "" && doc.URI.Scheme == document.SchemeFile && s.matchesPath(doc.URI.Path) {
		return scoreSpecific
	}
	return scoreNone
}

// matchesPath matches the pattern against the path, and against the path relative to the folder.
func (s Selector) matchesPath(path string) bool {
	m := glob.Parse(s.Pattern, glob.Options{})
	if m.Match(path) {
		return true
	}
	if s.Folder != "" {
		if rel, err := filepath.Rel(s.Folder, path); err == nil && filepath.IsLocal(rel) {
			return m.Match(filepath.ToSlash(rel))
		}
	}
	return false
}

// Registered is a provider with the owner that registered it (the id of an extension, or of a language server).
type Registered[P any] struct {
	Owner    string
	Provider P
	// Score is how well the document matched the selector.
	Score int
	// Triggers are the trigger characters the provider was registered with.
	Triggers []string
}

type entry[P any] struct {
	owner    string
	sel      Selector
	p        P
	seq      int
	triggers []string
}

// list is the providers of one feature.
type list[P any] struct {
	mu      sync.RWMutex
	entries []*entry[P]
	seq     int
}

// add registers a provider and returns the function that removes it.
func (l *list[P]) add(owner string, sel Selector, p P, triggers ...string) (remove func()) {
	l.mu.Lock()
	l.seq++
	e := &entry[P]{owner: owner, sel: sel, p: p, seq: l.seq, triggers: slices.Clone(triggers)}
	l.entries = append(l.entries, e)
	l.mu.Unlock()
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.entries = slices.DeleteFunc(l.entries, func(x *entry[P]) bool { return x == e })
	}
}

// ordered lists the providers whose selector matches the document: the highest score first, and among equals the one that
// registered last first (VS Code's order).
func (l *list[P]) ordered(doc Document) []Registered[P] {
	l.mu.RLock()
	type scored struct {
		e     *entry[P]
		score int
	}
	var found []scored
	for _, e := range l.entries {
		if sc := e.sel.Score(doc); sc > 0 {
			found = append(found, scored{e, sc})
		}
	}
	l.mu.RUnlock()
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score > found[j].score
		}
		return found[i].e.seq > found[j].e.seq
	})
	out := make([]Registered[P], len(found))
	for i, f := range found {
		out[i] = Registered[P]{Owner: f.e.owner, Provider: f.e.p, Score: f.score, Triggers: f.e.triggers}
	}
	return out
}

// all lists the providers whatever the document is, in the order of ordered with equal scores: the newer registration first.
func (l *list[P]) all() []Registered[P] {
	l.mu.RLock()
	es := slices.Clone(l.entries)
	l.mu.RUnlock()
	sort.SliceStable(es, func(i, j int) bool { return es[i].seq > es[j].seq })
	out := make([]Registered[P], len(es))
	for i, e := range es {
		out[i] = Registered[P]{Owner: e.owner, Provider: e.p, Score: scoreAll, Triggers: e.triggers}
	}
	return out
}

// groups is ordered split into runs of equal score: the providers of the first run are asked together, and the next run only
// when they had nothing to say.
func (l *list[P]) groups(doc Document) [][]Registered[P] {
	var out [][]Registered[P]
	for _, r := range l.ordered(doc) {
		if n := len(out); n > 0 && out[n-1][0].Score == r.Score {
			out[n-1] = append(out[n-1], r)
			continue
		}
		out = append(out, []Registered[P]{r})
	}
	return out
}

// Registry holds the providers of every feature of one window tab (SPEC §9.12). It is safe for concurrent use. Register a
// provider with the Register methods and ask with the request methods (merge.go), which call the providers that match the
// document on goroutines of their own and merge their answers.
type Registry struct {
	completion  list[CompletionProvider]
	hover       list[HoverProvider]
	signature   list[SignatureHelpProvider]
	navigation  list[NavigationProvider]
	references  list[ReferenceProvider]
	symbols     list[SymbolProvider]
	highlights  list[HighlightProvider]
	codeActions list[CodeActionProvider]
	formatting  list[FormattingProvider]
	rename      list[RenameProvider]
	onType      list[OnTypeFormattingProvider]
	codeLens    list[CodeLensProvider]
	inlayHints  list[InlayHintProvider]
	semantic    list[SemanticTokensProvider]
	folding     list[FoldingRangeProvider]
	selection   list[SelectionRangeProvider]

	workspaceSymbols list[WorkspaceSymbolProvider]

	changeMu  sync.Mutex
	listeners map[int]func()
	refreshes map[int]func(Feature)
	nextID    int
}

// OnChange calls fn when what the providers offer changed without a registration changing: a language server that started or
// stopped (Changed). The editors ask for the symbols and the highlights again then. fn runs on the goroutine that called
// Changed. The function that stops the listener is returned.
func (r *Registry) OnChange(fn func()) (stop func()) {
	r.changeMu.Lock()
	defer r.changeMu.Unlock()
	if r.listeners == nil {
		r.listeners = map[int]func(){}
	}
	r.nextID++
	id := r.nextID
	r.listeners[id] = fn
	return func() {
		r.changeMu.Lock()
		defer r.changeMu.Unlock()
		delete(r.listeners, id)
	}
}

// Changed tells the listeners of OnChange that what the providers offer changed.
func (r *Registry) Changed() {
	r.changeMu.Lock()
	fns := make([]func(), 0, len(r.listeners))
	for _, fn := range r.listeners {
		fns = append(fns, fn)
	}
	r.changeMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// OnRefresh calls fn when a language server asks for one kind of its answers to be asked for again (the refresh requests of
// semantic tokens, code lenses and inlay hints): the feature says which. fn runs on the goroutine that called Refreshed. The
// function that stops the listener is returned.
func (r *Registry) OnRefresh(fn func(Feature)) (stop func()) {
	r.changeMu.Lock()
	defer r.changeMu.Unlock()
	if r.refreshes == nil {
		r.refreshes = map[int]func(Feature){}
	}
	r.nextID++
	id := r.nextID
	r.refreshes[id] = fn
	return func() {
		r.changeMu.Lock()
		defer r.changeMu.Unlock()
		delete(r.refreshes, id)
	}
}

// Refreshed tells the listeners of OnRefresh that the answers of a feature are stale.
func (r *Registry) Refreshed(f Feature) {
	r.changeMu.Lock()
	fns := make([]func(Feature), 0, len(r.refreshes))
	for _, fn := range r.refreshes {
		fns = append(fns, fn)
	}
	r.changeMu.Unlock()
	for _, fn := range fns {
		fn(f)
	}
}

// NewRegistry makes an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// CompletionProvider answers completion requests.
type CompletionProvider interface {
	ProvideCompletion(ctx context.Context, doc Document, pos Position, trigger CompletionTrigger) (CompletionList, error)
}

// HoverProvider answers hover requests; a nil Hover is no answer.
type HoverProvider interface {
	ProvideHover(ctx context.Context, doc Document, pos Position) (*Hover, error)
}

// SignatureHelpProvider answers signature help requests; nil is no answer.
type SignatureHelpProvider interface {
	ProvideSignatureHelp(ctx context.Context, doc Document, pos Position, sc SignatureContext) (*SignatureHelp, error)
}

// NavigationProvider answers go to definition, declaration, type definition and implementation. A provider that does not
// know the kind answers nothing.
type NavigationProvider interface {
	ProvideLocations(ctx context.Context, doc Document, pos Position, kind NavKind) ([]LocationLink, error)
}

// ReferenceProvider answers find all references.
type ReferenceProvider interface {
	ProvideReferences(ctx context.Context, doc Document, pos Position, includeDeclaration bool) ([]Location, error)
}

// SymbolProvider answers the symbols of a document.
type SymbolProvider interface {
	ProvideDocumentSymbols(ctx context.Context, doc Document) ([]DocumentSymbol, error)
}

// HighlightProvider answers the occurrences of the symbol under the cursor.
type HighlightProvider interface {
	ProvideDocumentHighlights(ctx context.Context, doc Document, pos Position) ([]DocumentHighlight, error)
}

// CodeActionProvider answers code action requests.
type CodeActionProvider interface {
	ProvideCodeActions(ctx context.Context, doc Document, req CodeActionRequest) ([]CodeAction, error)
}

// FormattingProvider formats a document. Range formatting is asked of a provider that also implements
// RangeFormattingProvider.
type FormattingProvider interface {
	ProvideFormatting(ctx context.Context, doc Document, opts FormattingOptions) ([]TextEdit, error)
}

// RangeFormattingProvider formats a range of a document.
type RangeFormattingProvider interface {
	ProvideRangeFormatting(ctx context.Context, doc Document, r Range, opts FormattingOptions) ([]TextEdit, error)
}

// RenameProvider answers rename: PrepareRename may answer nil (use the word at the position) and refuses with an error;
// ProvideRename answers the edit.
type RenameProvider interface {
	PrepareRename(ctx context.Context, doc Document, pos Position) (*PrepareRename, error)
	ProvideRename(ctx context.Context, doc Document, pos Position, newName string) (*WorkspaceEdit, error)
}

// RegisterOnTypeFormatting adds a provider of the formatting that follows a typed character.
func (r *Registry) RegisterOnTypeFormatting(owner string, sel Selector, p OnTypeFormattingProvider) (remove func()) {
	return r.onType.add(owner, sel, p)
}

// RegisterCompletion adds a completion provider; triggers are the characters that start a request when typed.
func (r *Registry) RegisterCompletion(owner string, sel Selector, p CompletionProvider, triggers ...string) (remove func()) {
	return r.completion.add(owner, sel, p, triggers...)
}

// RegisterHover adds a hover provider.
func (r *Registry) RegisterHover(owner string, sel Selector, p HoverProvider) (remove func()) {
	return r.hover.add(owner, sel, p)
}

// RegisterSignatureHelp adds a signature help provider; triggers start a request when typed.
func (r *Registry) RegisterSignatureHelp(owner string, sel Selector, p SignatureHelpProvider, triggers ...string) (remove func()) {
	return r.signature.add(owner, sel, p, triggers...)
}

// RegisterNavigation adds a provider of definitions and its kin.
func (r *Registry) RegisterNavigation(owner string, sel Selector, p NavigationProvider) (remove func()) {
	return r.navigation.add(owner, sel, p)
}

// RegisterReferences adds a references provider.
func (r *Registry) RegisterReferences(owner string, sel Selector, p ReferenceProvider) (remove func()) {
	return r.references.add(owner, sel, p)
}

// RegisterSymbols adds a provider of the symbols of a document.
func (r *Registry) RegisterSymbols(owner string, sel Selector, p SymbolProvider) (remove func()) {
	return r.symbols.add(owner, sel, p)
}

// RegisterHighlights adds a provider of document highlights.
func (r *Registry) RegisterHighlights(owner string, sel Selector, p HighlightProvider) (remove func()) {
	return r.highlights.add(owner, sel, p)
}

// RegisterCodeActions adds a code action provider.
func (r *Registry) RegisterCodeActions(owner string, sel Selector, p CodeActionProvider) (remove func()) {
	return r.codeActions.add(owner, sel, p)
}

// RegisterFormatting adds a formatter.
func (r *Registry) RegisterFormatting(owner string, sel Selector, p FormattingProvider) (remove func()) {
	return r.formatting.add(owner, sel, p)
}

// RegisterRename adds a rename provider.
func (r *Registry) RegisterRename(owner string, sel Selector, p RenameProvider) (remove func()) {
	return r.rename.add(owner, sel, p)
}

// HasCompletion tells whether a completion provider matches the document.
func (r *Registry) HasCompletion(doc Document) bool { return len(r.completion.ordered(doc)) > 0 }

// CompletionTriggers are the trigger characters of the completion providers that match the document, without duplicates.
func (r *Registry) CompletionTriggers(doc Document) []string {
	return triggersOf(r.completion.ordered(doc), func(p CompletionProvider) []string {
		if tp, ok := p.(interface{ CompletionTriggers() []string }); ok {
			return tp.CompletionTriggers()
		}
		return nil
	})
}

// SignatureTriggers are the trigger characters of the signature help providers that match the document.
func (r *Registry) SignatureTriggers(doc Document) []string {
	return triggersOf(r.signature.ordered(doc), func(p SignatureHelpProvider) []string {
		if tp, ok := p.(interface{ SignatureTriggers() []string }); ok {
			return tp.SignatureTriggers()
		}
		return nil
	})
}

// HasHover tells whether a hover provider matches the document.
func (r *Registry) HasHover(doc Document) bool { return len(r.hover.ordered(doc)) > 0 }

// HasSignatureHelp tells whether a signature help provider matches the document (the key editorHasSignatureHelpProvider).
func (r *Registry) HasSignatureHelp(doc Document) bool { return len(r.signature.ordered(doc)) > 0 }

// SignatureCharacters are the characters that start signature help and those that start it again while it shows, of the
// providers that match the document, without duplicates. Every trigger character is a retrigger character too (VS Code's
// parameter hints model); a provider that does not tell them apart (SignatureTriggers) has trigger characters only.
func (r *Registry) SignatureCharacters(doc Document) (triggers, retriggers []string) {
	add := func(into *[]string, cs ...string) {
		for _, c := range cs {
			if c != "" && !slices.Contains(*into, c) {
				*into = append(*into, c)
			}
		}
	}
	for _, reg := range r.signature.ordered(doc) {
		add(&triggers, reg.Triggers...)
		switch p := any(reg.Provider).(type) {
		case interface{ SignatureCharacters() ([]string, []string) }:
			t, rt := p.SignatureCharacters()
			add(&triggers, t...)
			add(&retriggers, rt...)
		case interface{ SignatureTriggers() []string }:
			add(&triggers, p.SignatureTriggers()...)
		}
	}
	add(&retriggers, triggers...)
	return triggers, retriggers
}

// triggersOf joins the trigger characters the providers were registered with and those they declare themselves.
func triggersOf[P any](regs []Registered[P], own func(P) []string) []string {
	var out []string
	for _, reg := range regs {
		for _, c := range append(slices.Clone(reg.Triggers), own(reg.Provider)...) {
			if !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
	}
	return out
}

// OnTypeFormattingProvider formats the text around a position after a character was typed (textDocument/onTypeFormatting).
type OnTypeFormattingProvider interface {
	// OnTypeTriggers are the characters that ask for the formatting.
	OnTypeTriggers() []string
	ProvideOnTypeFormatting(ctx context.Context, doc Document, pos Position, ch string, opts FormattingOptions) ([]TextEdit, error)
}
