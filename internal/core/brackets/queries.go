package brackets

import (
	"sort"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// Ported from VS Code: reference/vscode/src/vs/editor/common/model/bracketPairsTextModelPart/bracketPairsImpl.ts and
// reference/vscode/src/vs/editor/common/model/bracketPairsTextModelPart/bracketPairsTree/bracketPairsTree.ts (MIT)

// PairInfo is a bracket pair (VS Code's BracketPairInfo).
type PairInfo struct {
	// Range goes from the start of the opening bracket to the end of the closing bracket; when
	// the pair has no closing bracket, to the end of what it holds.
	Range buffer.Range
	// Open is the range of the opening bracket.
	Open buffer.Range
	// Close is the range of the closing bracket, when HasClose is true.
	Close    buffer.Range
	HasClose bool
	// Level is the number of pairs around this one; EqualLevel counts those among them that open
	// with the same bracket.
	Level, EqualLevel int
	// OpenBracket and CloseBracket are the brackets as the language knows them; CloseBracket is
	// nil when the pair is not closed.
	OpenBracket, CloseBracket *Bracket
}

// BracketInfo is a bracket of the document (VS Code's BracketInfo).
type BracketInfo struct {
	Range buffer.Range
	// Level is the number of pairs around the bracket, or one less than the number of pairs it
	// stands in when it is a closing bracket that closes nothing. EqualLevel counts the pairs
	// around it that open with the same bracket.
	Level, EqualLevel int
	// Invalid is true for an opening bracket that is never closed and for a closing bracket that
	// closes nothing.
	Invalid bool
	Bracket *Bracket
}

// Found is a bracket found by Prev and Next.
type Found struct {
	Range   buffer.Range
	Bracket *Bracket
}

func (n *node) openRange() buffer.Range {
	return buffer.Range{Start: buffer.Position{Line: int(n.line), Col: int(n.start)}, End: buffer.Position{Line: int(n.line), Col: int(n.end)}}
}

func (n *node) startPos() buffer.Position {
	return buffer.Position{Line: int(n.line), Col: int(n.start)}
}

func (n *node) endPos() buffer.Position {
	return buffer.Position{Line: int(n.line), Col: int(n.end)}
}

// pairInfo describes the pair at index i of the list of pairs.
func (ix *Index) pairInfo(i int32) PairInfo {
	p := &ix.pairs[i]
	open := ix.nodes[p.open]
	info := PairInfo{
		Open:        open.openRange(),
		Level:       int(p.level),
		EqualLevel:  int(p.eq),
		OpenBracket: open.br,
		Range:       buffer.Range{Start: open.startPos(), End: buffer.Position{Line: int(p.endLine), Col: int(p.endCol)}},
	}
	if p.close >= 0 {
		cl := ix.nodes[p.close]
		info.Close, info.HasClose, info.CloseBracket = cl.openRange(), true, cl.br
	}
	return info
}

// pairEnd is the end of the pair at index i.
func (ix *Index) pairEnd(i int32) buffer.Position {
	return buffer.Position{Line: int(ix.pairs[i].endLine), Col: int(ix.pairs[i].endCol)}
}

// pairIndexesIn lists the pairs that touch the range, in the order of their opening brackets:
// the pairs that start before the range and reach it, which are the ones around the first pair
// that starts in it, and then all that start in it.
func (ix *Index) pairIndexesIn(r buffer.Range) []int32 {
	first := sort.Search(len(ix.pairs), func(i int) bool {
		return !ix.nodes[ix.pairs[i].open].startPos().IsBefore(r.Start)
	})
	var around []int32
	j := int32(first) - 1
	for j >= 0 && ix.pairEnd(j).IsBefore(r.Start) {
		j = ix.pairs[j].parent
	}
	for ; j >= 0; j = ix.pairs[j].parent {
		around = append(around, j)
	}
	out := make([]int32, 0, len(around))
	for k := len(around) - 1; k >= 0; k-- {
		out = append(out, around[k])
	}
	for i := first; i < len(ix.pairs); i++ {
		if r.End.IsBefore(ix.nodes[ix.pairs[i].open].startPos()) {
			break
		}
		out = append(out, int32(i))
	}
	return out
}

// Pairs returns the bracket pairs that touch the range (they may share only an end with it), in
// the order of their opening brackets.
func (ix *Index) Pairs(r buffer.Range) []PairInfo {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.ensureLocked()
	idx := ix.pairIndexesIn(r)
	out := make([]PairInfo, len(idx))
	for k, i := range idx {
		out[k] = ix.pairInfo(i)
	}
	return out
}

// Brackets returns the brackets that touch the range, in order. With onlyColorized, the
// brackets of the pairs whose levels have no colors (`<` `>` unless the configuration says
// otherwise) are left out, and the levels count only the pairs that have colors.
func (ix *Index) Brackets(r buffer.Range, onlyColorized bool) []BracketInfo {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.ensureLocked()
	first := sort.Search(len(ix.nodes), func(i int) bool {
		return !ix.nodes[i].endPos().IsBefore(r.Start)
	})
	var out []BracketInfo
	for i := first; i < len(ix.nodes); i++ {
		n := &ix.nodes[i]
		if r.End.IsBefore(n.startPos()) {
			break
		}
		switch n.pair {
		case -2:
			continue
		case -1:
			level := n.ulevel
			if onlyColorized {
				level = -1
				if n.container >= 0 {
					c := &ix.pairs[n.container]
					level = c.clevel
					if c.colorized {
						level++
					}
					level--
				}
			}
			out = append(out, BracketInfo{Range: n.openRange(), Level: int(level), Invalid: true, Bracket: n.br})
		default:
			p := &ix.pairs[n.pair]
			info := BracketInfo{Range: n.openRange(), Level: int(p.level), EqualLevel: int(p.eq), Bracket: n.br}
			if onlyColorized {
				if !p.colorized {
					continue
				}
				info.Level, info.EqualLevel = int(p.clevel), int(p.ceq)
			}
			info.Invalid = p.close < 0 && n.br.Opening
			out = append(out, info)
		}
	}
	return out
}

// Match returns the two brackets of the pair the position is at (in or next to a bracket of a
// closed pair), or false. Where two pairs touch the position, the one whose bracket starts
// later wins.
func (ix *Index) Match(pos buffer.Position) (open, closing buffer.Range, ok bool) {
	var best PairInfo
	var bestAt buffer.Range
	for _, p := range ix.Pairs(buffer.Range{Start: pos, End: pos}) {
		if !p.HasClose {
			continue
		}
		var at buffer.Range
		switch {
		case p.Open.ContainsPosition(pos):
			at = p.Open
		case p.Close.ContainsPosition(pos):
			at = p.Close
		default:
			continue
		}
		if !ok || at.Start.Compare(bestAt.Start) >= 0 {
			best, bestAt, ok = p, at, true
		}
	}
	if !ok {
		return buffer.Range{}, buffer.Range{}, false
	}
	return best.Open, best.Close, true
}

// Enclosing returns the brackets of the innermost closed pair that has the position strictly
// inside, or false.
func (ix *Index) Enclosing(pos buffer.Position) (open, closing buffer.Range, ok bool) {
	at := buffer.Range{Start: pos, End: pos}
	pairs := ix.Pairs(at)
	for i := len(pairs) - 1; i >= 0; i-- {
		if pairs[i].HasClose && pairs[i].Range.StrictContainsRange(at) {
			return pairs[i].Open, pairs[i].Close, true
		}
	}
	return buffer.Range{}, buffer.Range{}, false
}

// MatchingUp returns the opening bracket of the innermost pair around the position that the
// closing bracket text closes, in the language at the position, or false. It finds the bracket
// even when the pair is not closed.
func (ix *Index) MatchingUp(text string, pos buffer.Position) (buffer.Range, bool) {
	lang := ix.langs.Language(ix.languageAt(pos))
	closing := lang.Lookup(text)
	if closing == nil || closing.Opening {
		return buffer.Range{}, false
	}
	pairs := ix.Pairs(buffer.Range{Start: pos, End: pos})
	for i := len(pairs) - 1; i >= 0; i-- {
		if closing.Closes(pairs[i].OpenBracket) {
			return pairs[i].Open, true
		}
	}
	return buffer.Range{}, false
}

// Prev returns the last bracket that starts before the position (a closing bracket that
// closes nothing is not one), or false.
func (ix *Index) Prev(pos buffer.Position) (Found, bool) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.ensureLocked()
	end := sort.Search(len(ix.nodes), func(i int) bool { return !ix.nodes[i].startPos().IsBefore(pos) })
	for i := end - 1; i >= 0; i-- {
		if n := &ix.nodes[i]; n.pair >= 0 {
			return Found{Range: n.openRange(), Bracket: n.br}, true
		}
	}
	return Found{}, false
}

// Next returns the first bracket that ends after the position (a closing bracket that closes
// nothing is not one), or false.
func (ix *Index) Next(pos buffer.Position) (Found, bool) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.ensureLocked()
	start := sort.Search(len(ix.nodes), func(i int) bool { return pos.IsBefore(ix.nodes[i].endPos()) })
	for i := start; i < len(ix.nodes); i++ {
		if n := &ix.nodes[i]; n.pair >= 0 {
			return Found{Range: n.openRange(), Bracket: n.br}, true
		}
	}
	return Found{}, false
}

// HasUnmatchedClosingAfter reports whether a closing bracket that closes nothing and that
// would close the opening bracket text, in the language at the position, starts at or after the
// position. It answers false while the tokens are not complete: a closing bracket in a comment
// that is not tokenized yet is not one.
func (ix *Index) HasUnmatchedClosingAfter(pos buffer.Position, opening string) bool {
	lang := ix.langs.Language(ix.languageAt(pos))
	open := lang.Lookup(opening)
	if open == nil || !open.Opening {
		return false
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if !ix.src.TokensComplete() {
		return false
	}
	ix.ensureLocked()
	for _, i := range ix.unexpect {
		n := &ix.nodes[i]
		if !n.startPos().IsBefore(pos) && n.br.Closes(open) {
			return true
		}
	}
	return false
}

// Complete reports whether the tokens were complete when the pairs were last made.
func (ix *Index) Complete() bool {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.ensureLocked()
	return ix.complete
}

// languageAt is the code of the language of the token at the position, or of the document.
func (ix *Index) languageAt(pos buffer.Position) int {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if pos.Line < 0 || pos.Line >= len(ix.lines) {
		return ix.src.Language()
	}
	toks := ix.src.LineTokens(pos.Line)
	if len(toks) == 0 {
		return ix.src.Language()
	}
	for _, t := range toks {
		if pos.Col < int(t.End) {
			return int(t.Language)
		}
	}
	return int(toks[len(toks)-1].Language)
}
