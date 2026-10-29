package editor

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// tokenBudget is how long a frame may tokenize, on the UI thread, the lines it is about to draw before
// it leaves the rest to the background tokenizer (SPEC §8.3). It is for the whole frame: the layout of
// the text, its paint and the minimap share it (takeFrameBudget). It is well under the 8 ms that SPEC
// §14 allows a frame of scrolling, leaving room for the rest of the frame (step 10.1: each of the
// requests had a budget of 6 ms, which made the first view of unseen lines 7 to 8 ms): a jump far down
// the file shows its first lines in colors and the rest when the background has caught up.
const tokenBudget = 4 * time.Millisecond

// tokenLink is what a view model knows of the tokenization of its document.
type tokenLink struct {
	tokenizer   *tokens.Tokenizer
	unsubscribe func()
	// subID is the id of the view model's subscription to the tokenizer (Tokenizer.OnChangeCause).
	subID int
	// editing is above zero while the view model applies an edit of its own: the tokenizer tells, on the same goroutine, that
	// the edited line lost its tokens, and the view model repaints for the edit itself.
	editing atomic.Int32
	// post runs a function on the UI thread; the widget gives the binding's Post.
	post func(func())

	mu sync.Mutex
	// pending is set while a repaint is posted; from and to are the lines that changed since.
	pending  bool
	from, to int
	// first and last are the lines on the screen, as the text layer last saw them.
	first, last int
	visible     bool
	// mmFirst and mmLast are the lines the minimap draws.
	mmFirst, mmLast int
	mmVisible       bool
	// frameOpen is set from the first request for tokens of a frame until the end of that frame; frameStart
	// is when it came, and frameSpent how much time the frame has spent tokenizing on the UI thread so far.
	frameOpen  bool
	frameStart time.Time
	frameSpent time.Duration
	// budget is the frame's budget; zero means tokenBudget. Tests that check what a frame tokenizes give a
	// longer one, so that a loaded machine does not leave the lines on the screen to the background.
	budget time.Duration
}

// frameStale is how long after the first request for tokens a frame counts as over even if the function
// posted at its end did not run (a view model without a poster, or one whose poster holds the function).
const frameStale = 30 * time.Millisecond

// takeFrameBudget gives the time that one request for tokens may spend tokenizing on the UI thread: at
// most max, and no more than what is left of tokenBudget for the whole frame. The layout of the text, its
// paint and the minimap's all ask in one frame, and each one's own budget made the frame's tokenization
// the sum of them (8 ms for a page of lines nobody had tokenized). A frame ends with a function posted to
// the UI thread; a view model without a poster gives every request its whole budget.
func (vm *ViewModel) takeFrameBudget(max time.Duration) time.Duration {
	l := &vm.tok
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.post == nil {
		return max
	}
	now := time.Now()
	if !l.frameOpen || now.Sub(l.frameStart) > frameStale {
		l.frameOpen, l.frameStart, l.frameSpent = true, now, 0
		l.post(func() {
			l.mu.Lock()
			l.frameOpen = false
			l.mu.Unlock()
		})
	}
	budget := tokenBudget
	if l.budget > 0 {
		budget = l.budget
	}
	return min(max, budget-l.frameSpent)
}

// spendFrameBudget notes time spent tokenizing in the frame.
func (vm *ViewModel) spendFrameBudget(d time.Duration) {
	vm.tok.mu.Lock()
	vm.tok.frameSpent += d
	vm.tok.mu.Unlock()
}

// ensureOwn tokenizes, as Tokenizer.Ensure does, lines this view model is about to read: it is not told about them, as it would
// repaint for lines it draws in the frame it is in (a frame of the software renderer costs more than the period of a key that
// repeats); the other listeners and the other views of the document are.
func (vm *ViewModel) ensureOwn(line int, budget time.Duration) bool {
	return vm.tok.tokenizer.EnsureFor(vm.tok.subID, line, budget)
}

// ensureWithin tokenizes up to a line within the frame's budget, at most max, and says how it went as
// Tokenizer.Ensure does; it does nothing once the frame has spent its budget.
func (vm *ViewModel) ensureWithin(line int, max time.Duration) {
	budget := vm.takeFrameBudget(max)
	if budget <= 0 {
		return
	}
	t0 := time.Now()
	vm.ensureOwn(line, budget)
	vm.spendFrameBudget(time.Since(t0))
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
		vm.tok.subID, vm.tok.unsubscribe = t.OnChangeCause(vm.tokensChangedBy)
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
		budget := tokenBudget
		if vm.tok.budget > 0 {
			budget = vm.tok.budget
		}
		vm.ensureWithin(last, budget)
	}
}

// minimapTokenBudget is how long the minimap may tokenize, on the UI thread, the lines it is about
// to draw.
const minimapTokenBudget = 2 * time.Millisecond

// ensureMinimapTokens tokenizes, as far as the budget allows, up to the last line the minimap is
// about to draw, and records which lines those are.
func (vm *ViewModel) ensureMinimapTokens(first, last int) {
	vm.tok.mu.Lock()
	vm.tok.mmFirst, vm.tok.mmLast, vm.tok.mmVisible = first, last, true
	vm.tok.mu.Unlock()
	if vm.tok.tokenizer != nil {
		vm.ensureWithin(last, minimapTokenBudget)
	}
}

// tokensChangedBy is tokensChanged with the cause the tokenizer gives. The loss of the tokens of the line an edit of the view
// model itself changed needs no repaint of its own: the edit repaints.
func (vm *ViewModel) tokensChangedBy(from, to int, cause tokens.Cause) {
	if cause == tokens.CauseEdit && vm.tok.editing.Load() > 0 {
		return
	}
	vm.tokensChanged(from, to)
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
	mmFirst, mmLast, mmVisible := l.mmFirst, l.mmLast, l.mmVisible
	l.pending = false
	l.mu.Unlock()
	outside := func(a, b int) bool { return to < a || from > b }
	if visible && outside(first, last) && (!mmVisible || outside(mmFirst, mmLast)) {
		return
	}
	vm.NotifyListeners()
}
