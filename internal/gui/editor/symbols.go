package editor

import (
	"context"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// The document symbols of an editor (SPEC §9.12 "Symbols", "As built (step 12.7)"): the editor asks the symbol providers for the
// symbols of its document, a little after it changes, through the cache that the editors of the window share, and the Outline
// view, the breadcrumbs, sticky scroll and the pickers read them from here.

// symbolsDelay is how long the editor waits after an edit before it asks for the symbols again.
const symbolsDelay = 300 * time.Millisecond

// symbolState is what a view model knows of the symbols of its document.
type symbolState struct {
	// symbols are the last answer and version the version of the document it was made for; has says that there is one (an empty
	// answer is an answer).
	symbols []langfeatures.DocumentSymbol
	version int
	has     bool
	// gen numbers the requests; cancel ends the one in flight and cancelWait the wait before the next.
	gen        int
	cancel     context.CancelFunc
	cancelWait func()
	unsub      func()
	changed    *foundation.ChangeNotifier
}

func newSymbolState() symbolState {
	return symbolState{changed: foundation.NewChangeNotifier(), version: -1}
}

// Symbols are the symbols of the document as the providers last answered, and whether a provider answered at all (an editor
// whose language has no symbol provider has none). They are for SymbolsVersion of the document, which may be older than the
// document by the edits made since.
func (vm *ViewModel) Symbols() ([]langfeatures.DocumentSymbol, bool) {
	return vm.sym.symbols, vm.sym.has
}

// SymbolsVersion is the version of the document the symbols were made for.
func (vm *ViewModel) SymbolsVersion() int { return vm.sym.version }

// AddSymbolsListener calls fn when the symbols change.
func (vm *ViewModel) AddSymbolsListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return vm.sym.changed.AddListener(fn)
}

// RemoveSymbolsListener removes a listener of AddSymbolsListener.
func (vm *ViewModel) RemoveSymbolsListener(h foundation.ListenerHandle) {
	vm.sym.changed.RemoveListener(h)
}

// symbolFeatures are the symbol cache and the registry of the window, or nil.
func (vm *ViewModel) symbolCache() *langfeatures.SymbolCache {
	if h := vm.sg.host; h != nil {
		return h.Symbols
	}
	return nil
}

// symbolsHostChanged starts or stops following the document when the host is set.
func (vm *ViewModel) symbolsHostChanged() {
	s := &vm.sym
	if vm.symbolCache() == nil {
		vm.symbolsStop()
		if s.has {
			s.symbols, s.has, s.version = nil, false, -1
			s.changed.NotifyListeners()
		}
		return
	}
	if s.unsub == nil {
		s.unsub = vm.doc.Subscribe(func(document.ChangeEvent) { vm.symbolsSchedule(symbolsDelay) })
	}
	vm.symbolsSchedule(0)
}

// symbolsStop stops following the document and cancels what is pending.
func (vm *ViewModel) symbolsStop() {
	s := &vm.sym
	if s.unsub != nil {
		s.unsub()
		s.unsub = nil
	}
	vm.symbolsCancel()
}

// symbolsCancel ends the request in flight and the wait.
func (vm *ViewModel) symbolsCancel() {
	s := &vm.sym
	s.gen++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.cancelWait != nil {
		s.cancelWait()
		s.cancelWait = nil
	}
}

// symbolsSchedule asks for the symbols after a delay (at once for 0), replacing a request that is waiting.
func (vm *ViewModel) symbolsSchedule(delay time.Duration) {
	s := &vm.sym
	if s.cancelWait != nil {
		s.cancelWait()
		s.cancelWait = nil
	}
	if delay <= 0 || vm.afterTimer == nil {
		vm.symbolsRequest()
		return
	}
	s.cancelWait = vm.afterTimer(delay, func() {
		s.cancelWait = nil
		vm.symbolsRequest()
	})
}

// symbolsRequest asks the cache for the symbols of the document as it is now, off the UI thread. A request that is in flight is
// cancelled: its answer is for a version that is gone; an answer that comes after another request began is dropped.
func (vm *ViewModel) symbolsRequest() {
	cache := vm.symbolCache()
	if cache == nil {
		return
	}
	s := &vm.sym
	doc := vm.langDocument()
	if cached, ok := cache.Cached(doc); ok {
		if s.cancel != nil {
			s.cancel()
			s.cancel = nil
		}
		s.gen++
		vm.symbolsSet(cached, doc.Version, true)
		return
	}
	if f := vm.hoverFeatures(); f != nil && !f.Has(doc, langfeatures.FeatureDocumentSymbols) {
		if s.has {
			vm.symbolsSet(nil, doc.Version, false)
		}
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.gen++
	gen := s.gen
	base := context.Background()
	if h := vm.sg.host; h != nil && h.Ctx != nil {
		base = h.Ctx
	}
	ctx, cancel := context.WithCancel(base)
	s.cancel = cancel
	go func() {
		syms, err := cache.Get(ctx, doc)
		vm.post(func() {
			if vm.sym.gen != gen {
				return // superseded: a newer request, or the editor stopped
			}
			vm.sym.cancel = nil
			cancel()
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					if h := vm.sg.host; h != nil && h.Log != nil {
						h.Log("document symbols failed", err)
					}
				}
				return
			}
			vm.symbolsSet(syms, doc.Version, true)
		})
	}()
}

// symbolsSet takes an answer.
func (vm *ViewModel) symbolsSet(syms []langfeatures.DocumentSymbol, version int, has bool) {
	s := &vm.sym
	if s.version > version {
		return // an older version than the one held
	}
	s.symbols, s.version, s.has = syms, version, has
	s.changed.NotifyListeners()
	vm.sticky.changed.NotifyListeners()
}

// DisposeSymbols stops following the document.
func (vm *ViewModel) DisposeSymbols() { vm.symbolsStop() }

// SymbolPath is the chain of symbols that enclose the cursor, the outermost first, without the kinds keep refuses.
func (vm *ViewModel) SymbolPath(keep func(langfeatures.DocumentSymbol) bool) []*langfeatures.DocumentSymbol {
	syms, ok := vm.Symbols()
	if !ok {
		return nil
	}
	return langfeatures.SymbolPathAt(syms, vm.cursors.Primary().Position(), keep)
}

// RevealSymbol puts the cursor at the name of a symbol and scrolls it into view (the breadcrumbs and the Outline use it). The
// move is a jump of the navigation history when the host says how (the window sets it).
func (vm *ViewModel) RevealSymbol(sym *langfeatures.DocumentSymbol) {
	if sym == nil {
		return
	}
	p := vm.doc.Snapshot().ValidatePosition(sym.SelectionRange.Start)
	if h := vm.sg.host; h != nil && h.JumpTo != nil {
		h.JumpTo(vm, p)
		return
	}
	vm.GoToPosition(p)
}

// FeaturesChanged tells the editor that what the providers offer changed (a language server started or stopped): the symbols
// and the highlights are asked for again, and the keys that say what the language has are set again. It runs on the UI thread.
func (vm *ViewModel) FeaturesChanged() {
	if vm.symbolCache() != nil {
		vm.symbolsSchedule(0)
	}
	vm.updateWordHighlight(false)
	vm.sel.NotifyListeners()
}
