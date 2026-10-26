package langfeatures

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// The helpers of the symbols of a document: the symbol kinds with the names the outline and breadcrumbs settings use, the
// symbol that encloses a position, the flat list the Go to Symbol picker filters, and the cache that shares one request per
// document version between the outline, the breadcrumbs, sticky scroll and the picker.
//
// Ported from VS Code: src/vs/editor/contrib/documentSymbols/browser/outlineModel.ts (getItemEnclosingPosition),
// src/vs/editor/common/languages.ts (SymbolKinds) and src/vs/editor/contrib/quickAccess/browser/gotoSymbolQuickAccess.ts
// (the order of the symbols)

// symbolKinds are the names of the symbol kinds in the order of the LSP's SymbolKind (1 is a file): the name that follows
// "show" in the settings outline.showXxx and breadcrumbs.showXxx, and the name of a kind in the picker's "@:" groups.
var symbolKinds = [...]struct{ setting, label string }{
	lsp.SymbolFile - 1:          {"Files", "files"},
	lsp.SymbolModule - 1:        {"Modules", "modules"},
	lsp.SymbolNamespace - 1:     {"Namespaces", "namespaces"},
	lsp.SymbolPackage - 1:       {"Packages", "packages"},
	lsp.SymbolClass - 1:         {"Classes", "classes"},
	lsp.SymbolMethod - 1:        {"Methods", "methods"},
	lsp.SymbolProperty - 1:      {"Properties", "properties"},
	lsp.SymbolField - 1:         {"Fields", "fields"},
	lsp.SymbolConstructor - 1:   {"Constructors", "constructors"},
	lsp.SymbolEnum - 1:          {"Enums", "enumerations"},
	lsp.SymbolInterface - 1:     {"Interfaces", "interfaces"},
	lsp.SymbolFunction - 1:      {"Functions", "functions"},
	lsp.SymbolVariable - 1:      {"Variables", "variables"},
	lsp.SymbolConstant - 1:      {"Constants", "constants"},
	lsp.SymbolString - 1:        {"Strings", "strings"},
	lsp.SymbolNumber - 1:        {"Numbers", "numbers"},
	lsp.SymbolBoolean - 1:       {"Booleans", "booleans"},
	lsp.SymbolArray - 1:         {"Arrays", "arrays"},
	lsp.SymbolObject - 1:        {"Objects", "objects"},
	lsp.SymbolKey - 1:           {"Keys", "keys"},
	lsp.SymbolNull - 1:          {"Null", "null"},
	lsp.SymbolEnumMember - 1:    {"EnumMembers", "enumeration members"},
	lsp.SymbolStruct - 1:        {"Structs", "structs"},
	lsp.SymbolEvent - 1:         {"Events", "events"},
	lsp.SymbolOperator - 1:      {"Operators", "operators"},
	lsp.SymbolTypeParameter - 1: {"TypeParameters", "type parameters"},
}

// SymbolKindCount is the number of symbol kinds.
const SymbolKindCount = len(symbolKinds)

// SymbolKindSetting is the name that follows "show" in outline.showXxx and breadcrumbs.showXxx for a symbol kind ("Functions",
// "EnumMembers"); empty for a kind that is not one.
func SymbolKindSetting(kind int) string {
	if kind < 1 || kind > len(symbolKinds) {
		return ""
	}
	return symbolKinds[kind-1].setting
}

// SymbolKindLabel is the plural name of a symbol kind that the picker groups by with "@:" ("functions", "type parameters");
// "properties" for a kind that is not one.
func SymbolKindLabel(kind int) string {
	if kind < 1 || kind > len(symbolKinds) {
		return symbolKinds[lsp.SymbolProperty-1].label
	}
	return symbolKinds[kind-1].label
}

// SymbolPathAt is the chain of symbols that enclose a position, the outermost first: at each level the first symbol whose range
// holds the position, as VS Code's outline model finds the item at a position. A symbol the keep function refuses (a kind the
// settings leave out) is not in the chain, and its children are. keep may be nil.
func SymbolPathAt(symbols []DocumentSymbol, pos Position, keep func(DocumentSymbol) bool) []*DocumentSymbol {
	var chain []*DocumentSymbol
	for level := symbols; ; {
		next := -1
		for i := range level {
			if level[i].Range.ContainsPosition(pos) {
				next = i
				break
			}
		}
		if next < 0 {
			return chain
		}
		if keep == nil || keep(level[next]) {
			chain = append(chain, &level[next])
		}
		level = level[next].Children
	}
}

// FlatSymbol is a symbol of the flat list of the Go to Symbol picker.
type FlatSymbol struct {
	Symbol *DocumentSymbol
	// Container is the names of the symbols above it joined with a dot ("Server.Start"), empty at the top.
	Container string
	// Depth is how many symbols are above it.
	Depth int
}

// FlattenSymbols lists the symbols and the symbols inside them in the order of the document (a symbol before its children).
func FlattenSymbols(symbols []DocumentSymbol) []FlatSymbol {
	var out []FlatSymbol
	var walk func(level []DocumentSymbol, container string, depth int)
	walk = func(level []DocumentSymbol, container string, depth int) {
		for i := range level {
			out = append(out, FlatSymbol{Symbol: &level[i], Container: container, Depth: depth})
			inner := level[i].Name
			if container != "" {
				inner = container + "." + level[i].Name
			}
			walk(level[i].Children, inner, depth+1)
		}
	}
	walk(symbols, "", 0)
	return out
}

// SymbolSortOrder is how a list of symbols is ordered (breadcrumbs.symbolSortOrder).
type SymbolSortOrder string

// The orders.
const (
	// SortByPosition keeps the order of the document.
	SortByPosition SymbolSortOrder = "position"
	// SortByName orders by name, ignoring case.
	SortByName SymbolSortOrder = "name"
	// SortByKind orders by kind and then by position.
	SortByKind SymbolSortOrder = "type"
)

// SortSymbols returns a copy of the symbols in an order. The copy is shallow: the children of a symbol are not sorted.
func SortSymbols(symbols []DocumentSymbol, order SymbolSortOrder) []DocumentSymbol {
	out := slices.Clone(symbols)
	switch order {
	case SortByName:
		slices.SortStableFunc(out, func(a, b DocumentSymbol) int { return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	case SortByKind:
		slices.SortStableFunc(out, func(a, b DocumentSymbol) int {
			if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
				return c
			}
			return comparePositions(a.Range.Start, b.Range.Start)
		})
	default:
		slices.SortStableFunc(out, func(a, b DocumentSymbol) int { return comparePositions(a.Range.Start, b.Range.Start) })
	}
	return out
}

func comparePositions(a, b Position) int {
	if c := cmp.Compare(a.Line, b.Line); c != 0 {
		return c
	}
	return cmp.Compare(a.Col, b.Col)
}

// maxSymbolCacheEntries is how many documents the cache keeps the symbols of; the least recently used go first.
const maxSymbolCacheEntries = 64

// SymbolCache shares the document symbols of the window between the features that need them: the Outline view, the
// breadcrumbs, sticky scroll and the picker of Go to Symbol in Editor. It keeps the symbols of the latest version of each
// document that was asked for, asks the providers once for a version however many features want it, and cancels the request of
// a version that a newer one supersedes; a feature whose document has changed since gets context.Canceled (the symbols are
// for a version that is gone). It is safe for concurrent use.
type SymbolCache struct {
	reg  *Registry
	base context.Context

	mu      sync.Mutex
	entries map[document.URI]*symbolEntry
	clock   int
}

// symbolEntry is the request for one version of one document.
type symbolEntry struct {
	version  int
	done     chan struct{}
	cancel   context.CancelFunc
	symbols  []DocumentSymbol
	errs     []ProviderError
	canceled bool
	// failed says that no provider had symbols and one failed: the answer is not kept, the next request asks again.
	failed bool
	used   int
}

// NewSymbolCache makes the cache of a registry; base ends every request of the cache (the window tab).
func NewSymbolCache(base context.Context, reg *Registry) *SymbolCache {
	return &SymbolCache{reg: reg, base: base, entries: map[document.URI]*symbolEntry{}}
}

// Get returns the symbols of the document at its version, asking the providers when no request for this version exists. The
// request is the cache's own, so one caller giving up does not stop the others; a request for a version that was superseded
// returns context.Canceled. The error is that of the first provider that failed when no provider had symbols.
func (c *SymbolCache) Get(ctx context.Context, doc Document) ([]DocumentSymbol, error) {
	e, fresh := c.entry(doc)
	if e == nil {
		// a version older than the one in the cache: nothing is kept for it
		syms, errs := c.reg.DocumentSymbols(ctx, doc)
		return syms, firstError(syms, errs)
	}
	if fresh {
		go func() {
			ectx, cancel := context.WithCancel(c.base)
			c.mu.Lock()
			e.cancel = cancel
			if e.canceled {
				cancel()
			}
			c.mu.Unlock()
			syms, errs := c.reg.DocumentSymbols(ectx, doc)
			c.mu.Lock()
			e.symbols, e.errs = syms, errs
			if ectx.Err() != nil {
				e.canceled = true
			}
			e.failed = firstError(syms, errs) != nil
			c.mu.Unlock()
			cancel()
			close(e.done)
		}()
	}
	select {
	case <-e.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.canceled {
		return nil, context.Canceled
	}
	return e.symbols, firstError(e.symbols, e.errs)
}

// Cached returns the symbols already known for the version of the document, without asking.
func (c *SymbolCache) Cached(doc Document) ([]DocumentSymbol, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[doc.URI]
	if e == nil || e.version != doc.Version {
		return nil, false
	}
	select {
	case <-e.done:
		return e.symbols, !e.canceled && !e.failed
	default:
		return nil, false
	}
}

// Forget drops what the cache knows of a document, and cancels its request.
func (c *SymbolCache) Forget(uri document.URI) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[uri]; e != nil {
		e.canceled = true
		if e.cancel != nil {
			e.cancel()
		}
		delete(c.entries, uri)
	}
}

// entry finds or makes the request for the version of the document. It returns nil for a version older than the cached one, and
// whether the entry is new, which the caller starts.
func (c *SymbolCache) entry(doc Document) (e *symbolEntry, fresh bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	if cur := c.entries[doc.URI]; cur != nil {
		switch {
		case cur.version == doc.Version && !cur.canceled && !cur.failed:
			cur.used = c.clock
			return cur, false
		case cur.version > doc.Version:
			return nil, false
		}
		cur.canceled = true
		if cur.cancel != nil {
			cur.cancel()
		}
	}
	e = &symbolEntry{version: doc.Version, done: make(chan struct{}), used: c.clock}
	c.entries[doc.URI] = e
	if len(c.entries) > maxSymbolCacheEntries {
		var oldest document.URI
		first := true
		for u, x := range c.entries {
			if u != doc.URI && (first || x.used < c.entries[oldest].used) {
				oldest, first = u, false
			}
		}
		if !first {
			if x := c.entries[oldest]; x.cancel != nil {
				x.cancel()
			}
			delete(c.entries, oldest)
		}
	}
	return e, true
}

// firstError is the error of the first provider that failed, when no provider had any symbols.
func firstError(syms []DocumentSymbol, errs []ProviderError) error {
	if len(syms) > 0 || len(errs) == 0 {
		return nil
	}
	return errs[0].Err
}
