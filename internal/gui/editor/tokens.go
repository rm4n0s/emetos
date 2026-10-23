package editor

import (
	"sync"
	"time"

	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// tokenBudget is how long the layout of the text layer may tokenize, on the UI thread, the lines
// it is about to draw before it leaves the rest to the background tokenizer (SPEC §8.3). It is
// under the 8 ms that SPEC §14 allows a frame of scrolling: a page of Go is tokenized in about
// 6 ms, so a jump far down the file shows its first lines in colors and the rest when the
// background has caught up.
const tokenBudget = 6 * time.Millisecond

// tokenLink is what a view model knows of the tokenization of its document.
type tokenLink struct {
	tokenizer   *tokens.Tokenizer
	unsubscribe func()
	// post runs a function on the UI thread; the widget gives the binding's Post.
	post func(func())

	mu sync.Mutex
	// pending is set while a repaint is posted; from and to are the lines that changed since.
	pending  bool
	from, to int
	// first and last are the lines on the screen, as the text layer last saw them.
	first, last int
	visible     bool
}

// SetTokenizer gives the view model the tokenizer of its document, whose tokens the text layer
// draws in the colors of the theme; nil takes it away. The view model does not own it: whoever
// made it closes it. The tokenizer tells from any goroutine that lines were tokenized, and the
// view model repaints when those are on the screen.
func (vm *ViewModel) SetTokenizer(t *tokens.Tokenizer) {
	if vm.tok.unsubscribe != nil {
		vm.tok.unsubscribe()
		vm.tok.unsubscribe = nil
	}
	vm.tok.tokenizer = t
	if t != nil {
		vm.tok.unsubscribe = t.OnChange(vm.tokensChanged)
	}
	vm.NotifyListeners()
}

// Tokenizer is the tokenizer of the document, or nil.
func (vm *ViewModel) Tokenizer() *tokens.Tokenizer { return vm.tok.tokenizer }

// SetPoster gives the view model the means to run a function on the UI thread, which it needs
// to repaint when the background tokenizer is done with lines. Without it the repaint happens on
// the goroutine of the tokenizer, which only a test without a window wants.
func (vm *ViewModel) SetPoster(post func(func())) {
	vm.tok.mu.Lock()
	vm.tok.post = post
	vm.tok.mu.Unlock()
}

// poster is the function that runs a function on the UI thread, or nil; any goroutine can ask.
func (vm *ViewModel) poster() func(func()) {
	vm.tok.mu.Lock()
	defer vm.tok.mu.Unlock()
	return vm.tok.post
}

// lineTokens are the tokens of a line, or nil when there are none.
func (vm *ViewModel) lineTokens(line int) []tokens.Token {
	if vm.tok.tokenizer == nil {
		return nil
	}
	return vm.tok.tokenizer.Line(line)
}

// ensureTokens tokenizes, as far as the budget allows, up to a line that is about to be drawn,
// and records the lines that are on the screen.
func (vm *ViewModel) ensureTokens(first, last int) {
	vm.tok.mu.Lock()
	vm.tok.first, vm.tok.last, vm.tok.visible = first, last, true
	vm.tok.mu.Unlock()
	if vm.tok.tokenizer != nil {
		vm.tok.tokenizer.Ensure(last, tokenBudget)
	}
}

// tokensChanged is told by the tokenizer, on its goroutine, that lines [from, to] were
// tokenized. The repaint is posted to the UI thread once for the lines that changed meanwhile.
func (vm *ViewModel) tokensChanged(from, to int) {
	l := &vm.tok
	l.mu.Lock()
	if l.pending {
		l.from, l.to = min(l.from, from), max(l.to, to)
		l.mu.Unlock()
		return
	}
	l.pending, l.from, l.to = true, from, to
	post := l.post
	l.mu.Unlock()
	if post == nil {
		vm.flushTokens()
		return
	}
	post(vm.flushTokens)
}

// flushTokens repaints when lines that were tokenized are on the screen. It runs on the UI
// thread.
func (vm *ViewModel) flushTokens() {
	l := &vm.tok
	l.mu.Lock()
	from, to, first, last, visible := l.from, l.to, l.first, l.last, l.visible
	l.pending = false
	l.mu.Unlock()
	if visible && (to < first || from > last) {
		return
	}
	vm.NotifyListeners()
}
