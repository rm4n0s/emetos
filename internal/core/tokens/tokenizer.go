package tokens

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/textmate"
)

// DefaultSlice is how long the background tokenizer works before it looks for a newer version of
// the document and lets the other goroutines run (SPEC §8.3).
const DefaultSlice = 5 * time.Millisecond

// Token is a part of a line with one stack of scopes. The tokens of a line cover it from the
// start: each token starts where the one before ends.
type Token struct {
	// End is the byte offset in the line where the token ends.
	End int32
	// Scope is the stack of scopes of the token (see Scopes).
	Scope ScopeID
	// Language is the code of the language of the token, that of an embedded language or of the
	// document's own.
	Language uint16
	// Type is the standard type of the token: comment, string, regular expression or other.
	Type textmate.TokenType
}

// Options tune a Tokenizer.
type Options struct {
	// Slice is how long the background tokenizer works at a time; DefaultSlice when zero.
	Slice time.Duration
	// LineTimeLimit is how long one line may take; zero means no limit besides the pattern limit
	// of the regular expressions (SPEC §8.2).
	LineTimeLimit time.Duration
}

// lineInfo is what the tokenizer knows of a line. The tokens slice is never changed once it is
// set, so a reader may keep it.
type lineInfo struct {
	tokens []Token
	// end is the state at the end of the line; hasEnd is false when it is not known. After an
	// edit it is the state of the line before the edit until the line is tokenized again, which
	// is how the tokenizer sees that the lines below are still right.
	end    *textmate.State
	hasEnd bool
	// done tells that the tokens are the tokens of the text of the line, from the state that
	// the line above ends in.
	done bool
}

// Tokenizer keeps the tokens of one document (SPEC §8.3): the tokens and the end state of every
// line, which an edit invalidates from its first line on, a background loop that tokenizes the
// invalid lines in slices on snapshots of the document and drops what became stale, and the
// tokenization of the lines about to be painted, on the caller. Edits are told to it through
// the document it was made for; Line and Ensure are for the goroutine of the document, the
// background loop and the other methods may be called from any goroutine.
type Tokenizer struct {
	doc     *document.Document
	scopes  *Scopes
	opts    Options
	enabled bool
	cancel  func()

	mu          sync.Mutex
	grammar     *textmate.Grammar
	gen         uint64          // changes whenever work in progress may be stale
	snap        buffer.Snapshot // the latest text
	lines       []lineInfo
	invalidFrom int // the first line that is not done; len(lines) when all are
	listeners   map[int]func(from, to int)
	nextID      int
	slices      int
	wake        chan struct{}

	// beforeCommit is called by the background loop after it tokenized a line and before it
	// takes the lock to keep the result (tests change the document there).
	beforeCommit func()
}

// New makes the tokenizer of a document with a grammar, which may be nil for a document with no
// highlighting. A document in large file mode is not tokenized. The tokenizer follows the
// changes of the document until Close; Run starts the background loop.
func New(doc *document.Document, grammar *textmate.Grammar, opts Options) *Tokenizer {
	if opts.Slice <= 0 {
		opts.Slice = DefaultSlice
	}
	t := &Tokenizer{
		doc:       doc,
		scopes:    NewScopes(),
		opts:      opts,
		enabled:   !doc.LargeFile(),
		grammar:   grammar,
		snap:      doc.Snapshot(),
		listeners: map[int]func(int, int){},
		wake:      make(chan struct{}, 1),
	}
	t.lines = make([]lineInfo, t.snap.LineCount())
	t.cancel = doc.Subscribe(t.documentChanged)
	return t
}

// Close stops following the document.
func (t *Tokenizer) Close() { t.cancel() }

// Scopes is the table of the scope stacks of the tokens.
func (t *Tokenizer) Scopes() *Scopes { return t.scopes }

// Active tells whether the tokenizer produces tokens: it has a grammar and the document is not
// in large file mode.
func (t *Tokenizer) Active() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.activeLocked()
}

func (t *Tokenizer) activeLocked() bool { return t.enabled && t.grammar != nil }

// Slices is how many slices the background loop and Ensure have run.
func (t *Tokenizer) Slices() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.slices
}

// OnChange calls fn with the lines [from, to] (zero-based, inclusive) whose tokens changed, on
// the goroutine that tokenized them. It gives a function that stops the calls.
func (t *Tokenizer) OnChange(fn func(from, to int)) (unsubscribe func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	id := t.nextID
	t.nextID++
	t.listeners[id] = fn
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		delete(t.listeners, id)
	}
}

// notify tells the listeners that lines changed.
func (t *Tokenizer) notify(from, to int) {
	t.mu.Lock()
	listeners := make([]func(int, int), 0, len(t.listeners))
	for _, fn := range t.listeners {
		listeners = append(listeners, fn)
	}
	t.mu.Unlock()
	for _, fn := range listeners {
		fn(from, to)
	}
}

// wakeUp tells the background loop that there is work.
func (t *Tokenizer) wakeUp() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// SetGrammar changes the grammar, as when the language of the document changes: every line has
// to be tokenized again.
func (t *Tokenizer) SetGrammar(g *textmate.Grammar) {
	t.mu.Lock()
	t.gen++
	t.grammar = g
	for i := range t.lines {
		t.lines[i] = lineInfo{}
	}
	t.invalidFrom = 0
	n := len(t.lines)
	t.mu.Unlock()
	t.wakeUp()
	t.notify(0, n-1)
}

// Line gives the tokens of a line, or nil when the line has none yet (it is not tokenized, or
// there is no grammar). It never tokenizes.
func (t *Tokenizer) Line(line int) []Token {
	t.mu.Lock()
	defer t.mu.Unlock()
	if line < 0 || line >= len(t.lines) || !t.lines[line].done {
		return nil
	}
	return t.lines[line].tokens
}

// Ensure tokenizes, on the caller, the lines up to a line (zero-based), as far as the budget
// allows, so that the lines about to be painted have their colors. It reports whether the line
// is tokenized.
func (t *Tokenizer) Ensure(line int, budget time.Duration) bool {
	t.mu.Lock()
	if !t.activeLocked() || line < 0 || line >= len(t.lines) {
		t.mu.Unlock()
		return false
	}
	if t.invalidFrom > line {
		t.mu.Unlock()
		return true
	}
	if t.grammar.Compiling() {
		// Another goroutine is making the rules of the grammar (the window warms the grammars of its files before
		// it is shown; the rules of Markdown take 60 ms): the caller, which is the thread that paints, does not
		// wait for it. The background loop waits instead and tokenizes the lines when the rules are there.
		t.mu.Unlock()
		t.wakeUp()
		return false
	}
	deadline := time.Now().Add(budget)
	t.slices++
	from := t.invalidFrom
	last := from - 1
	for t.invalidFrom <= line {
		i := t.invalidFrom
		text, err := t.snap.LineContent(i)
		if err != nil {
			break
		}
		res := t.grammar.TokenizeLine(text, t.stateBefore(i), t.opts.LineTimeLimit)
		t.commitLocked(i, res)
		last = i
		if !time.Now().Before(deadline) && t.invalidFrom <= line {
			break
		}
	}
	ok := t.invalidFrom > line
	t.mu.Unlock()
	if last >= from {
		t.notify(from, last)
	}
	if !ok {
		t.wakeUp()
	}
	return ok
}

// stateBefore is the state a line starts in: the end of the line above (nil for the first line).
// The line above must be done.
func (t *Tokenizer) stateBefore(line int) *textmate.State {
	if line == 0 {
		return nil
	}
	return t.lines[line-1].end
}

// commitLocked keeps the tokens of line i, which is the first invalid line, and moves invalidFrom
// on: when the end state of the line is what it was before the edit, the lines below are still
// right, else the next line has to be tokenized again.
func (t *Tokenizer) commitLocked(i int, res textmate.LineResult) {
	old, hadEnd := t.lines[i].end, t.lines[i].hasEnd
	t.lines[i] = lineInfo{tokens: t.convert(res.Tokens), end: res.State, hasEnd: true, done: true}
	next := i + 1
	switch {
	case next >= len(t.lines):
		t.invalidFrom = len(t.lines)
	case !t.lines[next].done:
		t.invalidFrom = next
	case hadEnd && old.Equal(res.State):
		t.invalidFrom = t.nextInvalidLocked(next + 1)
	default:
		t.lines[next].done = false
		t.invalidFrom = next
	}
}

// nextInvalidLocked is the first line from a line on that is not done.
func (t *Tokenizer) nextInvalidLocked(from int) int {
	for i := from; i < len(t.lines); i++ {
		if !t.lines[i].done {
			return i
		}
	}
	return len(t.lines)
}

// convert makes the compact tokens of a line.
func (t *Tokenizer) convert(in []textmate.Token) []Token {
	out := make([]Token, len(in))
	for i, tok := range in {
		out[i] = Token{End: int32(tok.End), Scope: t.scopes.ID(tok.Scopes), Language: uint16(tok.Language), Type: tok.Type}
	}
	return out
}

// documentChanged is told every change of the document, on the goroutine of the document. The
// edited lines become invalid and the lines below keep their tokens until the end state of the
// last edited line says whether they are still right.
func (t *Tokenizer) documentChanged(ev document.ChangeEvent) {
	t.mu.Lock()
	t.gen++
	t.snap = ev.After
	if ev.Cause == document.CauseSetText {
		t.lines = make([]lineInfo, ev.After.LineCount())
		t.invalidFrom = 0
		n := len(t.lines)
		t.mu.Unlock()
		t.wakeUp()
		t.notify(0, n-1)
		return
	}
	// The changes are in order of position and apply to the text before the batch, so going from
	// the last to the first keeps the line numbers of the earlier ones.
	for i := len(ev.Changes) - 1; i >= 0; i-- {
		c := ev.Changes[i]
		t.spliceLocked(c.OldRange.Start.Line, c.OldRange.End.Line, c.NewRange.End.Line-c.NewRange.Start.Line+1)
	}
	if len(t.lines) != ev.After.LineCount() {
		// The change said less than the text did; start again.
		t.lines = make([]lineInfo, ev.After.LineCount())
		t.invalidFrom = 0
	}
	first := len(t.lines)
	if len(ev.Changes) > 0 {
		first = min(ev.Changes[0].NewRange.Start.Line, len(t.lines)-1)
	}
	if first < t.invalidFrom {
		t.invalidFrom = first
	}
	n := len(t.lines)
	active := t.activeLocked()
	t.mu.Unlock()
	if active {
		t.wakeUp()
	}
	if first < n {
		t.notify(first, first)
	}
}

// spliceLocked replaces the lines from to (zero-based, inclusive) with count new lines, which are
// not tokenized. The last new line keeps the end state of the last old line, to compare with.
func (t *Tokenizer) spliceLocked(from, to, count int) {
	if from < 0 || to >= len(t.lines) || to < from || count < 1 {
		return
	}
	old := t.lines[to]
	region := make([]lineInfo, count)
	region[count-1] = lineInfo{end: old.end, hasEnd: old.hasEnd}
	t.lines = append(t.lines[:from], append(region, t.lines[to+1:]...)...)
}

// Run is the background loop: it tokenizes the invalid lines in slices until ctx is canceled,
// looking for a newer version of the document and letting the other goroutines run between the
// slices, and waits for an edit when everything is tokenized.
func (t *Tokenizer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if t.slice() {
			runtime.Gosched()
			continue
		}
		select {
		case <-t.wake:
		case <-ctx.Done():
			return
		}
	}
}

// Settle tokenizes every invalid line on the caller, in slices as the background loop does. It is
// for callers that need the tokens of the whole document at once (tests, a search that needs
// token types).
func (t *Tokenizer) Settle() {
	for t.slice() {
	}
}

// slice tokenizes invalid lines for one slice, on the snapshot of the document it started on. A
// result is kept only when the document did not change meanwhile. It reports whether there is
// more to do.
func (t *Tokenizer) slice() bool {
	t.mu.Lock()
	if !t.activeLocked() || t.invalidFrom >= len(t.lines) {
		t.mu.Unlock()
		return false
	}
	t.slices++
	gen, snap, grammar := t.gen, t.snap, t.grammar
	i := t.invalidFrom
	state := t.stateBefore(i)
	from := i
	t.mu.Unlock()

	deadline := time.Now().Add(t.opts.Slice)
	last := -1
	for time.Now().Before(deadline) {
		text, err := snap.LineContent(i)
		if err != nil {
			return true // the snapshot is shorter than the lines: the document changed
		}
		res := grammar.TokenizeLine(text, state, t.opts.LineTimeLimit)
		if t.beforeCommit != nil {
			t.beforeCommit()
		}
		t.mu.Lock()
		if t.gen != gen {
			t.mu.Unlock()
			if last >= from {
				t.notify(from, last)
			}
			return true // stale: what was tokenized is dropped, and the next slice starts again
		}
		t.commitLocked(i, res)
		next := t.invalidFrom
		more := next < len(t.lines)
		t.mu.Unlock()
		last = i
		if !more {
			break
		}
		if next != i+1 {
			break // the next invalid line is below a line that is right: its start state is to be read again
		}
		i, state = next, res.State
	}
	if last >= from {
		t.notify(from, last)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.invalidFrom < len(t.lines)
}

// TypeIfInserting is the standard type that the text ch would have if it were inserted at the
// byte column col of a line: the line is tokenized again with ch in it, from the state the line
// above ends in, and nothing is kept. It is how the editor tells whether a quote typed there
// would end a string or begin one. It reports false when the tokenizer is off or the line above
// is not tokenized yet (see Ensure).
//
// Ported from VS Code: src/vs/editor/common/model/tokens/tokenizationTextModelPart.ts
// (getTokenTypeIfInsertingCharacter)
func (t *Tokenizer) TypeIfInserting(line, col int, ch string) (textmate.TokenType, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.activeLocked() || line < 0 || line >= len(t.lines) || (line > 0 && !t.lines[line-1].done) {
		return textmate.TokenOther, false
	}
	text, err := t.snap.LineContent(line)
	if err != nil {
		return textmate.TokenOther, false
	}
	col = min(max(col, 0), len(text))
	res := t.grammar.TokenizeLine(text[:col]+ch+text[col:], t.stateBefore(line), t.opts.LineTimeLimit)
	for _, tok := range res.Tokens {
		if col < tok.End {
			return tok.Type, true
		}
	}
	return textmate.TokenOther, true
}
