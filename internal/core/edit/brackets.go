package edit

import (
	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// BracketNavigator finds the brackets next to a position (a *brackets.Index is one).
type BracketNavigator interface {
	// Next is the first bracket that ends after the position.
	Next(pos buffer.Position) (brackets.Found, bool)
	// Prev is the last bracket that starts before the position.
	Prev(pos buffer.Position) (brackets.Found, bool)
}

// BracketModel is what the bracket commands ask of the bracket structure of a document (a
// *brackets.Index is one).
type BracketModel interface {
	BracketNavigator
	// Match gives the two brackets of the pair the position is at.
	Match(pos buffer.Position) (open, closing buffer.Range, ok bool)
	// Enclosing gives the brackets of the innermost closed pair around the position.
	Enclosing(pos buffer.Position) (open, closing buffer.Range, ok bool)
}

// matchOrEnclosing is the pair the position is at or, when it is at none, the one around it.
func matchOrEnclosing(bm BracketModel, pos buffer.Position) (open, closing buffer.Range, ok bool) {
	if open, closing, ok = bm.Match(pos); ok {
		return open, closing, true
	}
	return bm.Enclosing(pos)
}

// JumpToBracket is Go to Bracket (Ctrl+Shift+\): each cursor goes to the bracket that matches
// the one at the start of its selection, or else to the closing bracket of the pair around it,
// or else to the next bracket. A selection that finds none becomes a cursor where it started.
//
// Ported from VS Code: src/vs/editor/contrib/bracketMatching/browser/bracketMatching.ts (jumpToBracket)
func JumpToBracket(bm BracketModel, sels []buffer.Selection) []buffer.Selection {
	out := make([]buffer.Selection, len(sels))
	for i, s := range sels {
		pos := s.Start()
		target := pos
		if open, closing, ok := bm.Match(pos); ok {
			switch {
			case open.ContainsPosition(pos) && !closing.ContainsPosition(pos):
				target = closing.Start
			case closing.ContainsPosition(pos):
				target = open.Start
			}
		} else if _, closing, ok := bm.Enclosing(pos); ok {
			target = closing.Start
		} else if next, ok := bm.Next(pos); ok {
			target = next.Range.Start
		}
		out[i] = buffer.NewCursor(target)
	}
	return out
}

// SelectToBracket is Select to Bracket: each selection becomes the text between the brackets
// of the pair at its start, around it, or the next one, with the brackets when selectBrackets
// is true and backwards when the cursor was on the closing bracket. A selection that finds no
// pair is dropped; it reports false when none found one.
//
// Ported from VS Code: bracketMatching.ts (selectToBracket)
func SelectToBracket(bm BracketModel, sels []buffer.Selection, selectBrackets bool) ([]buffer.Selection, bool) {
	var out []buffer.Selection
	for _, s := range sels {
		pos := s.Start()
		open, closing, ok := bm.Match(pos)
		if !ok {
			open, closing, ok = bm.Enclosing(pos)
		}
		if !ok {
			if next, found := bm.Next(pos); found {
				open, closing, ok = bm.Match(next.Range.Start)
			}
		}
		if !ok {
			continue
		}
		if closing.Start.IsBefore(open.Start) {
			open, closing = closing, open
		}
		from, to := open.End, closing.Start
		if selectBrackets {
			from, to = open.Start, closing.End
		}
		if closing.ContainsPosition(pos) {
			from, to = to, from
		}
		out = append(out, buffer.Selection{Anchor: from, Active: to})
	}
	return out, len(out) > 0
}

// RemoveBrackets is Remove Brackets: the two brackets of the pair at each cursor (or around
// it) are deleted.
//
// Ported from VS Code: bracketMatching.ts (removeBrackets)
func (c Commands) RemoveBrackets(bm BracketModel, sels []buffer.Selection) Result {
	plans := make([]plan, len(sels))
	for i, s := range sels {
		open, closing, ok := matchOrEnclosing(bm, s.Active)
		if !ok {
			continue
		}
		cur := s
		plans[i] = plan{
			edits:  []document.Edit{{Range: open}, {Range: closing}},
			custom: true,
			selection: func(m *mapper, _ []buffer.Range) buffer.Selection {
				return m.track(cur, true)
			},
		}
	}
	return build(sels, plans, document.KindOther)
}

// maxBracketSteps is how many brackets the provider of selection ranges looks at in each
// direction before it gives up (VS Code gives it 30 ms in two rounds).
const maxBracketSteps = 100000

// bracketSelectionRanges is the provider of the steps of Expand Selection that the brackets
// around a position make.
type bracketSelectionRanges struct {
	nav BracketNavigator
}

// NewBracketSelectionRanges makes the provider of the bracket steps of Expand Selection: for
// each bracket pair around a position, the text inside and the pair, and, when the pair
// spans lines, what is before it on its line or above it.
//
// Ported from VS Code: src/vs/editor/contrib/smartSelect/browser/bracketSelections.ts
func NewBracketSelectionRanges(nav BracketNavigator) SelectionRangeProvider {
	return bracketSelectionRanges{nav: nav}
}

// openingKey is the text of the opening bracket of a bracket that was found.
func openingKey(f brackets.Found) string {
	if f.Bracket.Opening {
		return f.Bracket.Text
	}
	if o := f.Bracket.Openings(); len(o) > 0 {
		return o[0].Text
	}
	return f.Bracket.Text
}

func (p bracketSelectionRanges) SelectionRanges(m Model, positions []buffer.Position) [][]buffer.Range {
	out := make([][]buffer.Range, len(positions))
	for i, position := range positions {
		// To the right: the closing brackets that close nothing in the text after the position,
		// for each kind of bracket, nearest first.
		unmatched := map[string][]buffer.Range{}
		counts := map[string]int{}
		pos := position
		for step := 0; step < maxBracketSteps; step++ {
			b, ok := p.nav.Next(pos)
			if !ok {
				break
			}
			key := openingKey(b)
			if b.Bracket.Opening {
				counts[key]++
			} else {
				val := counts[key] - 1
				counts[key] = max(0, val)
				if val < 0 {
					unmatched[key] = append(unmatched[key], b.Range)
				}
			}
			pos = b.Range.End
		}
		// To the left: an opening bracket that closes nothing before the position pairs with
		// the nearest of those closing brackets.
		var bucket []buffer.Range
		counts = map[string]int{}
		pos = position
		for step := 0; step < maxBracketSteps && len(unmatched) > 0; step++ {
			b, ok := p.nav.Prev(pos)
			if !ok {
				break
			}
			key := openingKey(b)
			if !b.Bracket.Opening {
				counts[key]++
			} else {
				val := counts[key] - 1
				counts[key] = max(0, val)
				if val < 0 {
					if list := unmatched[key]; len(list) > 0 {
						closing := list[0]
						if len(list) == 1 {
							delete(unmatched, key)
						} else {
							unmatched[key] = list[1:]
						}
						inner := buffer.NewRange(b.Range.End, closing.Start)
						outer := buffer.NewRange(b.Range.Start, closing.End)
						bucket = append(bucket, inner, outer)
						bucket = addBracketLeading(m, outer, bucket)
					}
				}
			}
			pos = b.Range.Start
		}
		out[i] = bucket
	}
	return out
}

// addBracketLeading adds the steps of a pair that spans lines that include what leads it:
//
//	xxxxxxxx {
//
//	}
//
// and
//
//	xxxxxxxx
//	{
//
//	}
func addBracketLeading(m Model, bracket buffer.Range, bucket []buffer.Range) []buffer.Range {
	if bracket.Start.Line == bracket.End.Line {
		return bucket
	}
	startLine := bracket.Start.Line
	if col := firstNonWhitespace(m.LineContent(startLine)); col >= 0 && col != bracket.Start.Col {
		bucket = append(bucket,
			buffer.NewRange(buffer.Position{Line: startLine, Col: col}, bracket.End),
			buffer.NewRange(buffer.Position{Line: startLine}, bracket.End))
	}
	if above := startLine - 1; above >= 0 {
		text := m.LineContent(above)
		col := firstNonWhitespace(text)
		// VS Code compares one-based columns: the first non-blank one, and the one after the
		// last non-blank character (0 for a blank line).
		firstCol, lastCol := col+1, 0
		if last := lastNonBlank(text, len(text)-1); last >= 0 {
			lastCol = last + 2
		}
		if col >= 0 && col == bracket.Start.Col && firstCol != lastCol {
			bucket = append(bucket,
				buffer.NewRange(buffer.Position{Line: above, Col: col}, bracket.End),
				buffer.NewRange(buffer.Position{Line: above}, bracket.End))
		}
	}
	return bucket
}
