package find

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Ported from VS Code: src/vs/editor/contrib/find/browser/findDecorations.ts (MIT)

// The class names of the decorations of the find model, which the editor draws.
const (
	// ClassFindMatch is a match.
	ClassFindMatch = "findMatch"
	// ClassCurrentFindMatch is the match that was moved to last.
	ClassCurrentFindMatch = "currentFindMatch"
	// ClassRangeHighlight is the lines of the current match (a whole-line decoration).
	ClassRangeHighlight = "rangeHighlight"
	// ClassFindScope is a range the search is limited to (a whole-line decoration).
	ClassFindScope = "findScope"
)

// decorations are the decorations that mark the matches in the document, the current match, the
// lines of the current match and the search scopes (VS Code's FindDecorations). They are the
// document's, so they move with the edits.
type decorations struct {
	doc   *document.Document
	owner int

	matches        []int
	scopes         []int
	rangeHighlight int
	hasHighlight   bool
	highlighted    int
	hasCurrent     bool
	start          buffer.Position
}

func newDecorations(doc *document.Document, owner int, start buffer.Position) *decorations {
	return &decorations{doc: doc, owner: owner, start: start}
}

func matchOptions(class string) document.DecorationOptions {
	return document.DecorationOptions{Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: class}
}

// dispose removes all the decorations.
func (d *decorations) dispose() {
	d.doc.RemoveDecorations(d.all()...)
	d.reset()
}

// reset forgets the decorations without removing them (the document dropped them).
func (d *decorations) reset() {
	d.matches, d.scopes = nil, nil
	d.hasHighlight, d.hasCurrent = false, false
}

func (d *decorations) all() []int {
	ids := append([]int(nil), d.matches...)
	ids = append(ids, d.scopes...)
	if d.hasHighlight {
		ids = append(ids, d.rangeHighlight)
	}
	return ids
}

func (d *decorations) count() int { return len(d.matches) }

// rangeOf is where a decoration is now.
func (d *decorations) rangeOf(id int) (buffer.Range, bool) {
	dec, ok := d.doc.Decoration(id)
	return dec.Range, ok
}

// findScope is the first search scope, if there is one.
func (d *decorations) findScope() (buffer.Range, bool) {
	if len(d.scopes) == 0 {
		return buffer.Range{}, false
	}
	return d.rangeOf(d.scopes[0])
}

// findScopes are the search scopes, or nil when the search is not limited.
func (d *decorations) findScopes() []buffer.Range {
	var out []buffer.Range
	for _, id := range d.scopes {
		if r, ok := d.rangeOf(id); ok {
			out = append(out, r)
		}
	}
	return out
}

func (d *decorations) startPosition() buffer.Position { return d.start }

// setStartPosition remembers where the search goes on from and forgets the current match.
func (d *decorations) setStartPosition(p buffer.Position) {
	d.start = p
	d.setCurrentFindMatch(nil)
}

// indexOf is the number, from 1, of a match decoration (1 for one that is not known).
func (d *decorations) indexOf(id int) int {
	for i, m := range d.matches {
		if m == id {
			return i + 1
		}
	}
	return 1
}

// rangeAt is the range of the match with an index (from 0).
func (d *decorations) rangeAt(index int) (buffer.Range, bool) {
	if index < 0 || index >= len(d.matches) {
		return buffer.Range{}, false
	}
	return d.rangeOf(d.matches[index])
}

// currentMatchesPosition is the number, from 1, of the first match that a range touches, or 0.
func (d *decorations) currentMatchesPosition(r buffer.Range) int {
	for _, c := range d.doc.DecorationsIn(r, d.owner) {
		if c.Options.ClassName == ClassFindMatch || c.Options.ClassName == ClassCurrentFindMatch {
			return d.indexOf(c.ID)
		}
	}
	return 0
}

// setCurrentFindMatch makes a match the current one, marks the lines it is on and returns its number
// from 1, or 0 when it is not one of the matches (or next is nil).
func (d *decorations) setCurrentFindMatch(next *buffer.Range) int {
	newID, found, position := 0, false, 0
	if next != nil {
		for i, id := range d.matches {
			if r, ok := d.rangeOf(id); ok && r == *next {
				newID, found, position = id, true, i+1
				break
			}
		}
	}
	if !d.hasCurrent && !found {
		return position
	}
	if d.hasCurrent {
		d.doc.SetDecorationOptions(d.highlighted, matchOptions(ClassFindMatch))
		d.hasCurrent = false
	}
	if found {
		d.highlighted, d.hasCurrent = newID, true
		d.doc.SetDecorationOptions(newID, matchOptions(ClassCurrentFindMatch))
	}
	if d.hasHighlight {
		d.doc.RemoveDecorations(d.rangeHighlight)
		d.hasHighlight = false
	}
	if found {
		rng, _ := d.rangeOf(newID)
		if rng.Start.Line != rng.End.Line && rng.End.Col == 0 {
			before := rng.End.Line - 1
			text, _ := d.doc.Snapshot().LineContent(before)
			rng.End = buffer.Position{Line: before, Col: len(text)}
		}
		d.rangeHighlight = d.doc.AddDecoration(d.owner, rng, matchOptions(ClassRangeHighlight))
		d.hasHighlight = true
	}
	return position
}

// set replaces the decorations of the matches and of the search scopes.
func (d *decorations) set(matches []Match, scopes []buffer.Range) {
	add := make([]document.NewDecoration, len(matches))
	for i, m := range matches {
		add[i] = document.NewDecoration{Range: m.Range, Options: matchOptions(ClassFindMatch)}
	}
	d.matches = d.doc.DeltaDecorations(d.owner, d.matches, add)
	if d.hasHighlight {
		d.doc.RemoveDecorations(d.rangeHighlight)
		d.hasHighlight = false
	}
	d.hasCurrent = false
	d.doc.RemoveDecorations(d.scopes...)
	d.scopes = nil
	for _, s := range scopes {
		d.scopes = append(d.scopes, d.doc.AddDecoration(d.owner, s, document.DecorationOptions{ClassName: ClassFindScope}))
	}
}

// matchBeforePosition is the last match that ends at or before a position, or the last match when
// there is none; ok is false only when there are no matches.
func (d *decorations) matchBeforePosition(p buffer.Position) (buffer.Range, bool) {
	if len(d.matches) == 0 {
		return buffer.Range{}, false
	}
	for i := len(d.matches) - 1; i >= 0; i-- {
		r, ok := d.rangeOf(d.matches[i])
		if !ok || r.End.Line > p.Line {
			continue
		}
		if r.End.Line < p.Line || r.End.Col <= p.Col {
			return r, true
		}
	}
	return d.rangeOf(d.matches[len(d.matches)-1])
}

// matchAfterPosition is the first match that starts at or after a position, or the first match
// when there is none.
func (d *decorations) matchAfterPosition(p buffer.Position) (buffer.Range, bool) {
	if len(d.matches) == 0 {
		return buffer.Range{}, false
	}
	for _, id := range d.matches {
		r, ok := d.rangeOf(id)
		if !ok || r.Start.Line < p.Line {
			continue
		}
		if r.Start.Line > p.Line || r.Start.Col >= p.Col {
			return r, true
		}
	}
	return d.rangeOf(d.matches[0])
}
