package document

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func decoRanges(ds []Decoration) []buffer.Range {
	out := make([]buffer.Range, len(ds))
	for i, d := range ds {
		out[i] = d.Range
	}
	return out
}

func TestAddAndQueryDecorations(t *testing.T) {
	d := newDoc(lines("one two three", "four five", "six"))
	a := d.AddDecoration(1, rg(0, 0, 0, 3), DecorationOptions{ClassName: "a", Data: 42})
	b := d.AddDecoration(2, rg(0, 4, 1, 4), DecorationOptions{ClassName: "b"})
	c := d.AddDecoration(1, rg(2, 0, 2, 3), DecorationOptions{ClassName: "c"})
	if a != 1 || b != 2 || c != 3 {
		t.Errorf("ids = %d %d %d", a, b, c)
	}
	got, ok := d.Decoration(a)
	if !ok || got.Owner != 1 || got.Range != rg(0, 0, 0, 3) || got.Options.ClassName != "a" || got.Options.Data != 42 {
		t.Errorf("Decoration(a) = %+v, %v", got, ok)
	}
	if _, ok := d.Decoration(99); ok {
		t.Error("Decoration(99) found one")
	}
	if got := decoRanges(d.AllDecorations()); !reflect.DeepEqual(got, []buffer.Range{rg(0, 0, 0, 3), rg(0, 4, 1, 4), rg(2, 0, 2, 3)}) {
		t.Errorf("AllDecorations = %v", got)
	}
	tests := []struct {
		name  string
		r     buffer.Range
		owner int
		want  []int
	}{
		{"everything", rg(0, 0, 2, 3), 0, []int{a, b, c}},
		{"a range touching only the end of the first", rg(0, 3, 0, 3), 0, []int{a}},
		{"inside the second", rg(0, 8, 0, 9), 0, []int{b}},
		{"on the second line", rg(1, 0, 1, 1), 0, []int{b}},
		{"a gap", rg(1, 5, 1, 9), 0, nil},
		{"owner 1 only", rg(0, 0, 2, 3), 1, []int{a, c}},
		{"owner 2 only", rg(0, 0, 2, 3), 2, []int{b}},
		{"owner with none", rg(0, 0, 2, 3), 7, nil},
		{"an empty range at the start of the third", rg(2, 0, 2, 0), 0, []int{c}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ids []int
			for _, dec := range d.DecorationsIn(tt.r, tt.owner) {
				ids = append(ids, dec.ID)
			}
			if !reflect.DeepEqual(ids, tt.want) {
				t.Errorf("DecorationsIn = %v, want %v", ids, tt.want)
			}
		})
	}
	if got := len(d.DecorationsOf(1)); got != 2 {
		t.Errorf("DecorationsOf(1) = %d decorations", got)
	}
}

func TestDecorationRangesAreValidated(t *testing.T) {
	d := newDoc("aα日")
	id := d.AddDecoration(1, rg(-5, 0, 0, 2), DecorationOptions{}) // the end is inside "α"
	got, _ := d.Decoration(id)
	if got.Range != rg(0, 0, 0, 3) {
		t.Errorf("range = %+v, want it moved into the text and grown to whole characters", got.Range)
	}
	id = d.AddDecoration(1, rg(0, 1, 9, 9), DecorationOptions{})
	got, _ = d.Decoration(id)
	if got.Range != rg(0, 1, 0, 6) {
		t.Errorf("range = %+v", got.Range)
	}
}

func TestDeltaDecorations(t *testing.T) {
	d := newDoc("0123456789")
	ids := d.DeltaDecorations(1, nil, []NewDecoration{{Range: rg(0, 0, 0, 2)}, {Range: rg(0, 3, 0, 5)}, {Range: rg(0, 6, 0, 8)}})
	other := d.AddDecoration(2, rg(0, 1, 0, 9), DecorationOptions{})
	// Remove two of owner 1 (one by a wrong owner is ignored) and add one.
	d.DeltaDecorations(2, []int{ids[0]}, nil) // owner 2 can't remove owner 1's decoration
	if len(d.AllDecorations()) != 4 {
		t.Fatalf("a decoration was removed by the wrong owner: %d left", len(d.AllDecorations()))
	}
	fresh := d.DeltaDecorations(1, []int{ids[0], ids[2], 777}, []NewDecoration{{Range: rg(0, 9, 0, 10), Options: DecorationOptions{ClassName: "new"}}})
	if len(fresh) != 1 || fresh[0] <= other {
		t.Errorf("new ids = %v", fresh)
	}
	var got []int
	for _, dec := range d.AllDecorations() {
		got = append(got, dec.ID)
	}
	if want := []int{other, ids[1], fresh[0]}; !reflect.DeepEqual(got, want) {
		t.Errorf("decorations = %v, want %v", got, want)
	}
	d.RemoveDecorations(ids[1], 12345)
	d.RemoveDecorationsOf(1)
	if got := len(d.AllDecorations()); got != 1 {
		t.Errorf("%d decorations left, want the one of owner 2", got)
	}
	// Ids are not reused.
	if next := d.AddDecoration(1, rg(0, 0, 0, 0), DecorationOptions{}); next <= fresh[0] {
		t.Errorf("id %d was reused", next)
	}
}

// Decorations at each edge of an edit follow their stickiness (the same cases as
// the ported table, through the document): a decoration on bytes 5..10 of a line
// and an insert of one byte at its start and at its end.
func TestDecorationsAtTheEdgesOfAnInsert(t *testing.T) {
	line := "0123456789abcdefghij"
	tests := []struct {
		stick      Stickiness
		atStart    buffer.Range // after typing at the start edge
		atEnd      buffer.Range // after typing at the end edge
		insideGrow buffer.Range
	}{
		{AlwaysGrowsWhenTypingAtEdges, rg(0, 5, 0, 11), rg(0, 5, 0, 11), rg(0, 5, 0, 11)},
		{NeverGrowsWhenTypingAtEdges, rg(0, 6, 0, 11), rg(0, 5, 0, 10), rg(0, 5, 0, 11)},
		{GrowsOnlyWhenTypingBefore, rg(0, 5, 0, 11), rg(0, 5, 0, 10), rg(0, 5, 0, 11)},
		{GrowsOnlyWhenTypingAfter, rg(0, 6, 0, 11), rg(0, 5, 0, 11), rg(0, 5, 0, 11)},
	}
	for _, tt := range tests {
		for name, c := range map[string]struct {
			col  int
			want buffer.Range
		}{"start": {5, tt.atStart}, "end": {10, tt.atEnd}, "inside": {7, tt.insideGrow}} {
			t.Run(name, func(t *testing.T) {
				d := newDoc(line)
				id := d.AddDecoration(1, rg(0, 5, 0, 10), DecorationOptions{Stickiness: tt.stick})
				edit(t, d, []Edit{ins(0, c.col, "X")}, EditOptions{})
				if got, _ := d.Decoration(id); got.Range != c.want {
					t.Errorf("stickiness %d, typing at %s: %+v, want %+v", tt.stick, name, got.Range, c.want)
				}
			})
		}
	}
}

func TestDecorationsMoveWithEditsAndUndo(t *testing.T) {
	d := newDoc(lines("alpha beta", "gamma delta", "epsilon"))
	beta := d.AddDecoration(1, rg(0, 6, 0, 10), DecorationOptions{})
	delta := d.AddDecoration(1, rg(1, 6, 1, 11), DecorationOptions{Stickiness: NeverGrowsWhenTypingAtEdges})
	epsilon := d.AddDecoration(1, rg(2, 0, 2, 7), DecorationOptions{})
	edit(t, d, []Edit{ins(0, 0, "so: "), {Range: rg(1, 0, 1, 6)}, ins(0, 5, "!!")}, EditOptions{})
	// "so: alpha!! beta" / "delta" / "epsilon"
	if got, _ := d.Decoration(beta); got.Range != rg(0, 12, 0, 16) {
		t.Errorf("beta = %+v", got.Range)
	}
	if got, _ := d.Decoration(delta); got.Range != rg(1, 0, 1, 5) {
		t.Errorf("delta = %+v", got.Range)
	}
	if got, _ := d.Decoration(epsilon); got.Range != rg(2, 0, 2, 7) {
		t.Errorf("epsilon = %+v", got.Range)
	}
	undo(t, d)
	if got, _ := d.Decoration(beta); got.Range != rg(0, 6, 0, 10) {
		t.Errorf("beta after Undo = %+v", got.Range)
	}
	if got, _ := d.Decoration(delta); got.Range != rg(1, 6, 1, 11) {
		t.Errorf("delta after Undo = %+v", got.Range)
	}
	redo(t, d)
	if got, _ := d.Decoration(beta); got.Range != rg(0, 12, 0, 16) {
		t.Errorf("beta after Redo = %+v", got.Range)
	}
}

func TestDeletedDecorationsCollapseOrStay(t *testing.T) {
	d := newDoc("0123456789")
	keep := d.AddDecoration(1, rg(0, 3, 0, 6), DecorationOptions{})
	collapse := d.AddDecoration(1, rg(0, 3, 0, 6), DecorationOptions{CollapseOnReplaceEdit: true})
	edit(t, d, []Edit{{Range: rg(0, 2, 0, 8), Text: "ABCD"}}, EditOptions{})
	k, _ := d.Decoration(keep)
	c, _ := d.Decoration(collapse)
	if k.Range != rg(0, 3, 0, 6) {
		t.Errorf("kept decoration = %+v", k.Range)
	}
	if c.Range != rg(0, 2, 0, 2) {
		t.Errorf("collapsed decoration = %+v", c.Range)
	}
	// Deleting everything leaves empty decorations that are still there.
	edit(t, d, []Edit{{Range: rg(0, 0, 0, 10)}}, EditOptions{})
	if got := d.AllDecorations(); len(got) != 2 || got[0].Range != rg(0, 0, 0, 0) || got[1].Range != rg(0, 0, 0, 0) {
		t.Errorf("after deleting all: %+v", got)
	}
}

func TestSetTextClearsDecorations(t *testing.T) {
	d := newDoc("abc")
	d.AddDecoration(1, rg(0, 0, 0, 1), DecorationOptions{})
	d.SetText("new text")
	if len(d.AllDecorations()) != 0 {
		t.Error("decorations survived SetText")
	}
	// New ones work after it.
	id := d.AddDecoration(1, rg(0, 0, 0, 3), DecorationOptions{})
	if _, ok := d.Decoration(id); !ok {
		t.Error("cannot add a decoration after SetText")
	}
}

// Many decorations move with random batches of edits as the model says, through
// the whole Document: positions, order and queries.
func TestRandomDocumentDecorations(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	text := ""
	for i := 0; i < 400; i++ {
		text += "word "
		if i%7 == 0 {
			text += "\n"
		}
	}
	d := newDoc(text)
	type model struct {
		id         int
		start, end int
		stick      Stickiness
		collapse   bool
	}
	var models []*model
	snap := d.Snapshot()
	for i := 0; i < 700; i++ {
		s := rng.Intn(snap.Len() - 20)
		e := s + rng.Intn(15)
		r := buffer.Range{Start: snap.PositionAt(s), End: snap.PositionAt(e)}
		st, col := Stickiness(rng.Intn(4)), rng.Intn(6) == 0
		id := d.AddDecoration(1+i%3, r, DecorationOptions{Stickiness: st, CollapseOnReplaceEdit: col})
		got, _ := d.Decoration(id)
		models = append(models, &model{id: id, start: snap.OffsetAt(got.Range.Start), end: snap.OffsetAt(got.Range.End), stick: st, collapse: col})
	}
	for step := 0; step < 60; step++ {
		snap = d.Snapshot()
		var batch []Edit
		at := 0
		type change struct{ start, end, n int }
		var changes []change
		for k := 0; k < 1+rng.Intn(4) && at < snap.Len()-30; k++ {
			s := at + rng.Intn(snap.Len()/6)
			e := min(snap.Len(), s+rng.Intn(25))
			text := "xyz\nw"[:rng.Intn(6)]
			if e > snap.Len() || s > snap.Len() {
				break
			}
			batch = append(batch, Edit{Range: buffer.Range{Start: snap.PositionAt(s), End: snap.PositionAt(e)}, Text: text})
			changes = append(changes, change{s, e, len(text)})
			at = e + 1
		}
		if len(batch) == 0 {
			continue
		}
		edit(t, d, batch, EditOptions{})
		for k := len(changes) - 1; k >= 0; k-- {
			c := changes[k]
			for _, m := range models {
				m.start, m.end = acceptEdit(m.start, m.end, m.stick, m.collapse, c.start, c.end, c.n, false)
			}
		}
		snap = d.Snapshot()
		for _, m := range models {
			got, ok := d.Decoration(m.id)
			if !ok || snap.OffsetAt(got.Range.Start) != m.start || snap.OffsetAt(got.Range.End) != m.end {
				t.Fatalf("step %d: decoration %d = %+v, want offsets [%d, %d)", step, m.id, got.Range, m.start, m.end)
			}
		}
		if step%10 == 0 {
			checkStore(t, &d.deco.store)
		}
	}
	// A query agrees with a scan of the model.
	snap = d.Snapshot()
	for i := 0; i < 50; i++ {
		s := rng.Intn(snap.Len())
		e := min(snap.Len(), s+rng.Intn(200))
		want := 0
		for _, m := range models {
			if m.start <= e && m.end >= s {
				want++
			}
		}
		got := d.DecorationsIn(buffer.Range{Start: snap.PositionAt(s), End: snap.PositionAt(e)}, 0)
		if len(got) != want {
			t.Fatalf("query [%d, %d): %d decorations, want %d", s, e, len(got), want)
		}
	}
}

func BenchmarkDecorationsTyping(b *testing.B) {
	text := ""
	for i := 0; i < 50000; i++ {
		text += "a line of text here\n"
	}
	d := newDoc(text)
	snap := d.Snapshot()
	for i := 0; i < 20000; i++ {
		off := (i * 997) % snap.Len()
		d.AddDecoration(1, buffer.Range{Start: snap.PositionAt(off), End: snap.PositionAt(off + 3)}, DecorationOptions{})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.Edit([]Edit{ins(25000, i%10, "x")}, EditOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}
