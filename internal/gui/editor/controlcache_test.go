package editor

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// TestControlCacheReadsARowOnlyWhenItChanges pins the cache of the control characters of the visible rows (step 17b.4):
// the blink of the cursor repaints the layer above the text every half second and must not read the text of every row.
func TestControlCacheReadsARowOnlyWhenItChanges(t *testing.T) {
	r := newDisplayRig(t, whitespaceText, nil)
	g := newGeometry(r.vm, r.ts.Dispatcher().TextEngine())
	var c controlCache
	const ctlRow = 5 // "ctl: a\x01b\x7fc\u202ad"
	want := controlMarks("ctl: a\x01b\x7fc\u202ad")
	if len(want) != 3 {
		t.Fatalf("the fixture has %d control characters, want 3", len(want))
	}

	rows := c.update(g, 0, 6)
	if len(rows) != 7 || !slices.Equal(rows[ctlRow].marks, want) {
		t.Fatalf("first update: %d rows, marks of row %d = %v, want 7 rows and %v", len(rows), ctlRow, rows[ctlRow].marks, want)
	}
	first := &rows[ctlRow].marks[0]

	for _, tc := range []struct {
		name       string
		step       func()
		first, end int
		wantSame   bool // the marks of the row are the very ones the cache kept
		wantMarks  int
	}{
		{name: "nothing changed (a blink)", step: func() {}, first: 0, end: 6, wantSame: true, wantMarks: 3},
		{name: "again", step: func() {}, first: 0, end: 6, wantSame: true, wantMarks: 3},
		{name: "another row is edited", step: func() { insertAt(t, r, 1, 0, "// ") }, first: 0, end: 6, wantSame: false, wantMarks: 3},
		{name: "the row itself is edited", step: func() { insertAt(t, r, ctlRow, 0, "\x02") }, first: 0, end: 6, wantSame: false, wantMarks: 4},
		{name: "a scroll by a row", step: func() {}, first: 1, end: 7, wantSame: true, wantMarks: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.step()
			rows := c.update(newGeometry(r.vm, r.ts.Dispatcher().TextEngine()), tc.first, tc.end)
			got := rows[ctlRow-tc.first]
			if len(got.marks) != tc.wantMarks {
				t.Fatalf("marks of the row = %v, want %d", got.marks, tc.wantMarks)
			}
			if same := &got.marks[0] == first; same != tc.wantSame {
				t.Errorf("the marks of the row are the cached ones: %v, want %v", same, tc.wantSame)
			}
			first = &got.marks[0]
			for i, row := range rows {
				text, start := newGeometry(r.vm, r.ts.Dispatcher().TextEngine()).rowText(tc.first + i)
				if row.start != start || !slices.Equal(row.marks, controlMarks(text)) {
					t.Errorf("row %d: start %d marks %v, want start %d marks %v", tc.first+i, row.start, row.marks, start, controlMarks(text))
				}
			}
		})
	}

	t.Run("a steady frame allocates nothing", func(t *testing.T) {
		g := newGeometry(r.vm, r.ts.Dispatcher().TextEngine())
		c.update(g, 0, 6)
		c.update(g, 0, 6)
		if n := testing.AllocsPerRun(50, func() { c.update(g, 0, 6) }); n != 0 {
			t.Errorf("a frame that changes nothing allocates %v times, want 0", n)
		}
	})
}

// insertAt inserts text at a position of the document of the rig.
func insertAt(t *testing.T, r *mouseRig, line, col int, text string) {
	t.Helper()
	if _, err := r.vm.Document().Edit([]document.Edit{{Range: rg(line, col, line, col), Text: text}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
}
