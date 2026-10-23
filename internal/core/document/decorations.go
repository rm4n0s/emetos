package document

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// Decoration is a range of the document that moves with its edits (SPEC §7.1):
// find matches, bracket highlights, snippet tab stops, auto-closed characters,
// diff changes, folded regions, diagnostics.
type Decoration struct {
	// ID identifies the decoration in its document; it is never reused.
	ID int
	// Owner is the id of the feature that made it; owners are 1 or more.
	Owner int
	// Range is where the decoration is now.
	Range   buffer.Range
	Options DecorationOptions
}

// NewDecoration describes a decoration to add.
type NewDecoration struct {
	Range   buffer.Range
	Options DecorationOptions
}

// decorations is the decorations of a document.
type decorations struct {
	store  intervalStore
	byID   map[int]*node
	nextID int
}

// AddDecoration adds a decoration for an owner (1 or more) and returns its id.
// The range is moved into the text like an edit's.
func (d *Document) AddDecoration(owner int, r buffer.Range, opts DecorationOptions) int {
	return d.DeltaDecorations(owner, nil, []NewDecoration{{Range: r, Options: opts}})[0]
}

// DeltaDecorations removes decorations by id and adds new ones for an owner in one
// step, as VS Code's deltaDecorations does; it returns the new ids in order. An id
// that is not a decoration of this owner is ignored.
func (d *Document) DeltaDecorations(owner int, remove []int, add []NewDecoration) []int {
	deco := &d.deco
	if deco.byID == nil {
		deco.byID = map[int]*node{}
	}
	for _, id := range remove {
		if n, ok := deco.byID[id]; ok && n.owner == owner {
			deco.store.remove(n)
			delete(deco.byID, id)
		}
	}
	snap := d.buf.Snapshot()
	ids := make([]int, len(add))
	for i, a := range add {
		r := snap.ValidateRange(a.Range)
		deco.nextID++
		n := &node{id: deco.nextID, owner: owner, opts: a.Options}
		deco.store.insert(n, snap.OffsetAt(r.Start), snap.OffsetAt(r.End))
		deco.byID[n.id] = n
		ids[i] = n.id
	}
	return ids
}

// SetDecorationOptions changes the options of a decoration, which keeps its place. The stickiness
// does not change (it is read when the decoration is added). It reports whether there is a
// decoration with the id.
func (d *Document) SetDecorationOptions(id int, opts DecorationOptions) bool {
	n, ok := d.deco.byID[id]
	if !ok {
		return false
	}
	opts.Stickiness = n.opts.Stickiness
	n.opts = opts
	return true
}

// ChangeDecorationOptions replaces the options of a decoration, the stickiness too (VS Code's
// changeDecorationOptions): the decoration keeps its place, and the new stickiness decides how it
// moves with the next edits. It reports whether there is a decoration with the id.
func (d *Document) ChangeDecorationOptions(id int, opts DecorationOptions) bool {
	n, ok := d.deco.byID[id]
	if !ok {
		return false
	}
	n.opts = opts
	return true
}

// RemoveDecorations removes decorations by id, whoever owns them.
func (d *Document) RemoveDecorations(ids ...int) {
	for _, id := range ids {
		if n, ok := d.deco.byID[id]; ok {
			d.deco.store.remove(n)
			delete(d.deco.byID, id)
		}
	}
}

// RemoveDecorationsOf removes every decoration of an owner.
func (d *Document) RemoveDecorationsOf(owner int) {
	for _, dec := range d.DecorationsOf(owner) {
		d.RemoveDecorations(dec.ID)
	}
}

// Decoration returns the decoration with an id.
func (d *Document) Decoration(id int) (Decoration, bool) {
	n, ok := d.deco.byID[id]
	if !ok {
		return Decoration{}, false
	}
	return d.publicDecoration(d.buf.Snapshot(), n), true
}

// DecorationsIn returns the decorations that overlap a range, in order of
// position (a decoration that only touches an end of the range is included, and
// so is an empty one at it). With an owner of 1 or more only that owner's are
// returned; 0 means every owner.
func (d *Document) DecorationsIn(r buffer.Range, owner int) []Decoration {
	snap := d.buf.Snapshot()
	r = snap.ValidateRange(r)
	found := d.deco.store.search(snap.OffsetAt(r.Start), snap.OffsetAt(r.End))
	return d.publicDecorations(snap, found, owner)
}

// DecorationsOf returns every decoration of an owner, in order of position.
func (d *Document) DecorationsOf(owner int) []Decoration {
	return d.publicDecorations(d.buf.Snapshot(), d.deco.store.all(), owner)
}

// AllDecorations returns every decoration, in order of position.
func (d *Document) AllDecorations() []Decoration {
	return d.publicDecorations(d.buf.Snapshot(), d.deco.store.all(), 0)
}

func (d *Document) publicDecorations(snap buffer.Snapshot, nodes []*node, owner int) []Decoration {
	out := make([]Decoration, 0, len(nodes))
	for _, n := range nodes {
		if owner == 0 || n.owner == owner {
			out = append(out, d.publicDecoration(snap, n))
		}
	}
	return out
}

func (d *Document) publicDecoration(snap buffer.Snapshot, n *node) Decoration {
	return Decoration{
		ID: n.id, Owner: n.owner, Options: n.opts,
		Range: buffer.Range{Start: snap.PositionAt(n.absStart()), End: snap.PositionAt(n.absEnd())},
	}
}

// moveDecorations moves the decorations for changes, which are in order of
// position in the text before them. The last is done first so that the offsets
// of the others stay right.
func (d *Document) moveDecorations(changes []textChange) {
	if len(d.deco.byID) == 0 {
		return
	}
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		d.deco.store.applyEdit(c.start, c.start+len(c.oldText), len(c.newText), false)
	}
}

// clearDecorations removes every decoration.
func (d *Document) clearDecorations() {
	d.deco.store = intervalStore{}
	d.deco.byID = nil
}

// TrackOffset is where the offset p ends up when the bytes [editStart, editEnd)
// are replaced by textLen bytes: the rule decorations follow, for a single point.
// A point at the edit's start or end stays before the inserted text when
// sticksToPrevious, and moves after it otherwise; a point inside the replaced
// bytes keeps its offset while that is inside the new text, and else goes to the
// end of the new text.
func TrackOffset(p int, sticksToPrevious bool, editStart, editEnd, textLen int) int {
	st := GrowsOnlyWhenTypingAfter
	if sticksToPrevious {
		st = GrowsOnlyWhenTypingBefore
	}
	got, _ := acceptEdit(p, p, st, false, editStart, editEnd, textLen, false)
	return got
}
