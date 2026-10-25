package brackets

import (
	"sync"

	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// Ported from VS Code: reference/vscode/src/vs/editor/common/model/bracketPairsTextModelPart/bracketPairsTree/{parser,tokenizer}.ts (MIT)

// maxDepth is how deep brackets nest before the opening brackets are taken for text: VS Code's
// parser does that at level 300, which is a depth of 150 (it counts two levels for each).
const maxDepth = 150

// Source is the text and the tokens that an Index reads. Lines count from zero and the columns
// are byte offsets.
type Source interface {
	// LineCount is the number of lines (at least one).
	LineCount() int
	// LineContent is the text of a line without its line break.
	LineContent(line int) string
	// LineTokens are the tokens of a line, or nil when the line has none yet; brackets are then
	// looked for in the whole line as if it were code in the language of the document.
	LineTokens(line int) []tokens.Token
	// Language is the code of the language of the document.
	Language() int
	// TokensComplete reports whether every line has its tokens (or the document has no
	// tokenizer), so that what the Index finds is final.
	TokensComplete() bool
}

// Languages gives the brackets of a language by its code. It must return the same *Language for
// a code every time, as brackets are told apart by identity.
type Languages interface {
	Language(code int) *Language
}

// found is a bracket in a line.
type found struct {
	start, end int32
	br         *Bracket
}

// lineEntry is what an Index knows of a line.
type lineEntry struct {
	brs   []found
	valid bool
}

// node is a bracket of the document, in the list that the resolution makes.
type node struct {
	line       int32
	start, end int32
	br         *Bracket
	// pair is the index of the pair the bracket belongs to, -1 for a closing bracket that closes
	// nothing (an unexpected one) and -2 for an opening bracket taken for text (too deep).
	pair int32
	// container is, for an unexpected closing bracket, the pair it stands in (or -1), and
	// ulevel its nesting level counting every pair.
	container int32
	ulevel    int32
}

// pairNode is a bracket pair of the document. Pairs are in the order of their opening bracket.
type pairNode struct {
	open, close int32 // indexes of the nodes; close is -1 when the pair is not closed
	parent      int32
	// endLine and endCol are the end of the pair: the end of its closing bracket or, when it has
	// none, the place where its content ends.
	endLine, endCol int32
	// level counts the pairs around this one, eq those among them with the same opening bracket;
	// clevel and ceq count only the colorized ones.
	level, eq, clevel, ceq int32
	// colorized tells that the pair has colored levels: it has no closing bracket or one that
	// closes a colorized pair.
	colorized bool
	sameAbove int32 // the nearest pair around this one with the same opening bracket, or -1
	slot      int32 // the slot of its opening bracket in the resolution
}

// Index finds the bracket pairs of a document and answers questions about them (SPEC §8.5). It
// reads the brackets of a line from its tokens (the brackets in comments, strings and regular
// expressions are not brackets), keeps them per line, forgets the lines an edit or a new token
// touches, and pairs all the brackets again when it is asked something. VS Code keeps a tree of
// the pairs and reuses the parts an edit did not touch; here the work that costs, finding the
// brackets of a line, is kept per line and the pairing is one pass over the brackets.
//
// An Index is safe for use from several goroutines: the tokenizer reports new tokens from its
// own.
type Index struct {
	mu      sync.Mutex
	src     Source
	langs   Languages
	enabled bool

	lines []lineEntry
	// The result of the resolution; resolved is false after any change.
	resolved  bool
	nodes     []node
	pairs     []pairNode
	lineStart []int32 // the first node of each line; one more at the end
	unexpect  []int32 // indexes of the unexpected closing brackets
	// Scratch space of the resolution, kept to avoid allocating it every time.
	stack    []int32
	stackBr  []*Bracket
	slots    []*Bracket
	lastOpen []int32
	complete bool // the tokens were complete when the result was made
	// dirtyFrom to dirtyTo are the lines whose tokens changed while the pairs were made (dirtyTo is below
	// dirtyFrom when there are none): see Invalidate.
	dirtyFrom, dirtyTo int
	// resolutions counts the times the pairs were made, for the tests.
	resolutions int

	listeners map[int]func(from int)
	nextID    int
}

// New makes the index of a source. A disabled index (a document in large file mode) finds
// nothing.
func New(src Source, langs Languages, enabled bool) *Index {
	ix := &Index{src: src, langs: langs, enabled: enabled, listeners: map[int]func(int){}, dirtyTo: -1}
	ix.lines = make([]lineEntry, src.LineCount())
	return ix
}

// Enabled tells whether the index looks for brackets.
func (ix *Index) Enabled() bool { return ix.enabled }

// OnChange calls fn with the first line from which brackets may have changed, after an edit or
// new tokens. Pairs that start earlier can end later, so every line below can have other
// brackets and levels. It gives a function that stops the calls.
func (ix *Index) OnChange(fn func(from int)) (unsubscribe func()) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	id := ix.nextID
	ix.nextID++
	ix.listeners[id] = fn
	return func() {
		ix.mu.Lock()
		defer ix.mu.Unlock()
		delete(ix.listeners, id)
	}
}

func (ix *Index) notify(from int) {
	ix.mu.Lock()
	fns := make([]func(int), 0, len(ix.listeners))
	for _, fn := range ix.listeners {
		fns = append(fns, fn)
	}
	ix.mu.Unlock()
	for _, fn := range fns {
		fn(from)
	}
}

// Reset forgets everything, as when the text is replaced or the language changes.
func (ix *Index) Reset() {
	ix.mu.Lock()
	ix.dirtyFrom, ix.dirtyTo = 0, -1
	ix.lines = make([]lineEntry, ix.src.LineCount())
	ix.resolved = false
	ix.mu.Unlock()
	ix.notify(0)
}

// Splice tells that the lines from to to (zero-based, inclusive) were replaced by count new
// lines.
func (ix *Index) Splice(from, to, count int) {
	ix.mu.Lock()
	ix.forgetDirtyLocked()
	ix.spliceLocked(from, to, count)
	ix.resolved = false
	ix.mu.Unlock()
	ix.notify(from)
}

func (ix *Index) spliceLocked(from, to, count int) {
	if from < 0 || to >= len(ix.lines) || to < from || count < 1 {
		return
	}
	rest := ix.lines[to+1:]
	lines := make([]lineEntry, 0, from+count+len(rest))
	lines = append(lines, ix.lines[:from]...)
	lines = append(lines, make([]lineEntry, count)...)
	lines = append(lines, rest...)
	ix.lines = lines
}

// Invalidate tells that the lines from to to (zero-based, inclusive) have other tokens or text.
//
// New tokens of lines whose pairs are made usually leave their brackets as they were: the brackets
// of a line are found in its text when it has no tokens yet, and the tokens only take away those in
// strings and comments. Making the pairs again of a whole document (10 ms for 100,000 lines) for every
// slice of the tokenizer made each frame of a scroll cost that much (step 10.1). So the lines are only
// noted here, from any goroutine (the document is not read: it belongs to the UI thread), and the next
// question finds their brackets again and makes the pairs only when some are not the same.
func (ix *Index) Invalidate(from, to int) {
	ix.mu.Lock()
	if !ix.enabled {
		ix.resolved = false
	} else {
		ix.markDirtyLocked(from, to)
	}
	ix.mu.Unlock()
	ix.notify(from)
}

// markDirtyLocked notes that the tokens of the lines from to to changed. While the pairs are made
// the range joins the one noted (a range that covers more than the tokenizer touched only costs the
// finding of brackets in the lines between); otherwise the lines are just forgotten.
func (ix *Index) markDirtyLocked(from, to int) {
	from, to = max(from, 0), min(to, len(ix.lines)-1)
	if from > to {
		return
	}
	if !ix.resolved {
		for i := from; i <= to; i++ {
			ix.lines[i] = lineEntry{}
		}
		return
	}
	if ix.dirtyTo < ix.dirtyFrom { // none noted
		ix.dirtyFrom, ix.dirtyTo = from, to
		return
	}
	ix.dirtyFrom, ix.dirtyTo = min(ix.dirtyFrom, from), max(ix.dirtyTo, to)
}

// forgetDirtyLocked forgets the lines that were noted, before the lines move or the pairs are made
// from scratch: their brackets are to be found again, with the tokens they have now.
func (ix *Index) forgetDirtyLocked() {
	for i := max(ix.dirtyFrom, 0); i <= ix.dirtyTo && i < len(ix.lines); i++ {
		ix.lines[i] = lineEntry{}
	}
	ix.dirtyFrom, ix.dirtyTo = 0, -1
}

// settleDirtyLocked finds the brackets of the lines that were noted and tells the pairs they are out
// of date when some are not what they were, or when the tokens are complete now (or no longer).
func (ix *Index) settleDirtyLocked() {
	changed := false
	for i := max(ix.dirtyFrom, 0); i <= ix.dirtyTo && i < len(ix.lines); i++ {
		e := &ix.lines[i]
		brs := ix.extractLocked(i)
		if !e.valid || !sameBrackets(e.brs, brs) {
			e.brs, e.valid = brs, true
			changed = true
		}
	}
	ix.dirtyFrom, ix.dirtyTo = 0, -1
	if changed || ix.src.TokensComplete() != ix.complete {
		ix.resolved = false
	}
}

// sameBrackets reports whether two lists of the brackets of a line are the same.
func sameBrackets(a, b []found) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Edited tells that the text changed: each change replaced the old lines from..to with count
// new ones, the changes in order of position and relative to the text before the batch.
type Edited struct {
	From, To, Count int
}

// Apply tells the index of a batch of changes and invalidates the lines they touch. The new
// line count tells whether the index kept up with the text; if it did not, it starts again.
func (ix *Index) Apply(changes []Edited) {
	ix.mu.Lock()
	ix.forgetDirtyLocked()
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		ix.spliceLocked(c.From, c.To, c.Count)
	}
	if len(ix.lines) != ix.src.LineCount() {
		ix.lines = make([]lineEntry, ix.src.LineCount())
	}
	ix.resolved = false
	first := 0
	if len(changes) > 0 {
		first = changes[0].From
	}
	ix.mu.Unlock()
	ix.notify(first)
}

// segmentBrackets appends the brackets in the text of line that lies between lo and hi.
func segmentBrackets(out []found, content string, lo, hi int, lang *Language) []found {
	if lang.Empty() {
		return out
	}
	cur := lo
	for cur < hi {
		at, b := lang.find(content[cur:hi])
		if b == nil {
			break
		}
		start := cur + at
		out = append(out, found{start: int32(start), end: int32(start + len(b.Text)), br: b})
		cur = start + len(b.Text)
	}
	return out
}

// extractLocked finds the brackets of a line. Tokens of the same kind that follow each other are
// one (VS Code does that as identifiers are sometimes split in several tokens), and only tokens
// of the standard type "other" can hold brackets: brackets in comments, strings and regular
// expressions are text.
func (ix *Index) extractLocked(line int) []found {
	content := ix.src.LineContent(line)
	toks := ix.src.LineTokens(line)
	var out []found
	if toks == nil {
		return segmentBrackets(out, content, 0, len(content), ix.langs.Language(ix.src.Language()))
	}
	lo := 0
	for i := 0; i < len(toks); {
		j := i
		for j+1 < len(toks) && toks[j+1].Scope == toks[i].Scope && toks[j+1].Language == toks[i].Language && toks[j+1].Type == toks[i].Type {
			j++
		}
		hi := min(int(toks[j].End), len(content))
		if toks[i].Type == textmate.TokenOther && lo < hi {
			out = segmentBrackets(out, content, lo, hi, ix.langs.Language(int(toks[i].Language)))
		}
		lo = max(lo, hi)
		i = j + 1
	}
	return out
}

// ensureLocked makes the pairs up to date.
func (ix *Index) ensureLocked() {
	if ix.resolved && ix.dirtyTo >= ix.dirtyFrom {
		ix.settleDirtyLocked()
	}
	if ix.resolved {
		return
	}
	ix.resolutions++
	ix.nodes = ix.nodes[:0]
	ix.pairs = ix.pairs[:0]
	ix.unexpect = ix.unexpect[:0]
	n := len(ix.lines)
	if cap(ix.lineStart) < n+1 {
		ix.lineStart = make([]int32, n+1)
	}
	ix.lineStart = ix.lineStart[:n+1]
	ix.complete = ix.src.TokensComplete()
	ix.resolved = true
	if !ix.enabled {
		for i := range ix.lineStart {
			ix.lineStart[i] = 0
		}
		return
	}

	// The open pairs, with their opening brackets next to them so that a closing bracket finds
	// the one it closes without going through the lists of nodes and pairs.
	stack := ix.stack[:0]
	stackBr := ix.stackBr[:0]
	// lastOpen[slot] is the innermost open pair whose opening bracket has the slot, which is
	// the nearest pair around a new one that opens with the same text.
	slots := ix.slots[:0]
	lastOpen := ix.lastOpen[:0]
	slotOf := func(br *Bracket) int {
		for i, s := range slots {
			if s == br {
				return i
			}
		}
		// A bracket of another language with the same text counts as the same bracket.
		for i, s := range slots {
			if s.Text == br.Text {
				slots = append(slots, br)
				lastOpen = append(lastOpen, lastOpen[i])
				return len(slots) - 1
			}
		}
		slots = append(slots, br)
		lastOpen = append(lastOpen, -1)
		return len(slots) - 1
	}
	for line := range ix.lines {
		ix.lineStart[line] = int32(len(ix.nodes))
		e := &ix.lines[line]
		if !e.valid {
			e.brs = ix.extractLocked(line)
			e.valid = true
		}
		for _, f := range e.brs {
			idx := int32(len(ix.nodes))
			nd := node{line: int32(line), start: f.start, end: f.end, br: f.br, pair: -1, container: -1}
			if f.br.Opening {
				if len(stack) >= maxDepth {
					nd.pair = -2
					ix.nodes = append(ix.nodes, nd)
					continue
				}
				slot := slotOf(f.br)
				p := pairNode{open: idx, close: -1, parent: -1, sameAbove: lastOpen[slot], level: int32(len(stack)), slot: int32(slot)}
				if len(stack) > 0 {
					p.parent = stack[len(stack)-1]
				}
				if p.sameAbove >= 0 {
					p.eq = ix.pairs[p.sameAbove].eq + 1
				}
				nd.pair = int32(len(ix.pairs))
				lastOpen[slot] = nd.pair
				ix.pairs = append(ix.pairs, p)
				stack = append(stack, nd.pair)
				stackBr = append(stackBr, f.br)
				ix.nodes = append(ix.nodes, nd)
				continue
			}
			// A closing bracket closes the innermost open pair it fits; the pairs above that one
			// stay open for good and end where the closing bracket starts.
			match := -1
			if top := len(stackBr) - 1; top >= 0 && f.br.Closes(stackBr[top]) {
				match = top // the usual case: it closes the innermost pair
			} else {
				for k := top - 1; k >= 0; k-- {
					if f.br.Closes(stackBr[k]) {
						match = k
						break
					}
				}
			}
			if match < 0 {
				if len(stack) > 0 {
					nd.container = stack[len(stack)-1]
				}
				nd.ulevel = int32(len(stack)) - 1
				ix.unexpect = append(ix.unexpect, idx)
				ix.nodes = append(ix.nodes, nd)
				continue
			}
			for k := len(stack) - 1; k >= match; k-- {
				p := &ix.pairs[stack[k]]
				lastOpen[p.slot] = p.sameAbove
				if k > match {
					p.endLine, p.endCol = int32(line), f.start
				}
			}
			p := &ix.pairs[stack[match]]
			p.close = idx
			p.endLine, p.endCol = int32(line), f.end
			nd.pair = stack[match]
			ix.nodes = append(ix.nodes, nd)
			stack, stackBr = stack[:match], stackBr[:match]
		}
	}
	ix.lineStart[n] = int32(len(ix.nodes))
	// What is still open ends with the text.
	last := n - 1
	endCol := int32(len(ix.src.LineContent(last)))
	for _, pi := range stack {
		ix.pairs[pi].endLine, ix.pairs[pi].endCol = int32(last), endCol
	}
	ix.stack, ix.stackBr, ix.slots, ix.lastOpen = stack[:0], stackBr[:0], slots[:0], lastOpen[:0]

	// The levels that count only the colorized pairs need to know which pairs are colorized,
	// which is known only once a pair is closed.
	for i := range ix.pairs {
		p := &ix.pairs[i]
		p.colorized = p.close < 0 || ix.nodes[p.close].br.ClosesColorized(ix.nodes[p.open].br)
	}
	for i := range ix.pairs {
		p := &ix.pairs[i]
		if p.parent >= 0 {
			par := &ix.pairs[p.parent]
			p.clevel = par.clevel
			if par.colorized {
				p.clevel++
			}
		}
		a := p.sameAbove
		for a >= 0 && !ix.pairs[a].colorized {
			a = ix.pairs[a].sameAbove
		}
		if a >= 0 {
			p.ceq = ix.pairs[a].ceq + 1
		}
	}
}
