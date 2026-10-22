package document

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// Ported from VS Code: reference/vscode/src/vs/editor/test/common/model/intervalTree.test.ts
// ('IntervalTree 2' / 'nodeAcceptEdit': 208 rows of a decoration, its
// stickiness, an edit and the expected place).
func TestAcceptEdit(t *testing.T) {
	for _, tt := range acceptEditCases {
		gotStart, gotEnd := acceptEdit(tt.start, tt.end, tt.stickiness, false, tt.editStart, tt.editEnd, tt.textLength, tt.forceMove)
		if gotStart != tt.wantStart || gotEnd != tt.wantEnd {
			t.Errorf("%s: [%d, %d) stickiness %d, edit [%d, %d) with %d bytes (force %v) = [%d, %d), want [%d, %d)",
				tt.name, tt.start, tt.end, tt.stickiness, tt.editStart, tt.editEnd, tt.textLength, tt.forceMove,
				gotStart, gotEnd, tt.wantStart, tt.wantEnd)
		}
	}
}

func TestAcceptEditCollapse(t *testing.T) {
	// An edit that covers the whole decoration collapses one that asks for it.
	for _, st := range []Stickiness{AlwaysGrowsWhenTypingAtEdges, NeverGrowsWhenTypingAtEdges, GrowsOnlyWhenTypingBefore, GrowsOnlyWhenTypingAfter} {
		s, e := acceptEdit(5, 10, st, true, 3, 12, 4, false)
		if s != 3 || e != 3 {
			t.Errorf("stickiness %d: collapsed to [%d, %d), want [3, 3)", st, s, e)
		}
		// Without the flag the start stays where the replaced text began to be
		// replaced by new text and the end goes to the end of the new text.
		s, e = acceptEdit(5, 10, st, false, 3, 12, 4, false)
		if s != 5 || e != 7 {
			t.Errorf("stickiness %d: moved to [%d, %d), want [5, 7)", st, s, e)
		}
		// An edit that covers only part does not collapse it.
		s, e = acceptEdit(5, 10, st, true, 6, 12, 4, false)
		if s == e {
			t.Errorf("stickiness %d: a partial edit collapsed the decoration", st)
		}
	}
}

// oracle is the brute-force model of VS Code's test: a list kept sorted by
// (start, end), in insertion order among equals.
type oracle struct {
	items []*oracleItem
	seq   int
}

type oracleItem struct {
	start, end int
	seq        int
	stick      Stickiness
	collapse   bool
}

func (o *oracle) insert(start, end int) *oracleItem {
	o.seq++
	it := &oracleItem{start: start, end: end, seq: o.seq}
	o.items = append(o.items, it)
	o.sort()
	return it
}

func (o *oracle) sort() {
	sort.SliceStable(o.items, func(i, j int) bool {
		a, b := o.items[i], o.items[j]
		if a.start != b.start {
			return a.start < b.start
		}
		if a.end != b.end {
			return a.end < b.end
		}
		return a.seq < b.seq
	})
}

func (o *oracle) remove(it *oracleItem) {
	for i, x := range o.items {
		if x == it {
			o.items = append(o.items[:i], o.items[i+1:]...)
			return
		}
	}
}

func (o *oracle) search(start, end int) [][2]int {
	out := make([][2]int, 0)
	for _, it := range o.items {
		if it.start <= end && it.end >= start {
			out = append(out, [2]int{it.start, it.end})
		}
	}
	return out
}

func (o *oracle) positions() [][2]int {
	out := make([][2]int, len(o.items))
	for i, it := range o.items {
		out[i] = [2]int{it.start, it.end}
	}
	return out
}

func (o *oracle) applyEdit(editStart, editEnd, textLen int) {
	for _, it := range o.items {
		it.start, it.end = acceptEdit(it.start, it.end, it.stick, it.collapse, editStart, editEnd, textLen, false)
	}
	o.sort()
}

func storePositions(s *intervalStore) [][2]int {
	out := make([][2]int, 0)
	for _, n := range s.all() {
		out = append(out, [2]int{n.absStart(), n.absEnd()})
	}
	return out
}

func storeSearch(s *intervalStore, start, end int) [][2]int {
	out := make([][2]int, 0)
	for _, n := range s.search(start, end) {
		out = append(out, [2]int{n.absStart(), n.absEnd()})
	}
	return out
}

// checkStore verifies the store's own invariants: blocks are in order, no block
// is empty or too big, every block's largest end is right, and the whole is sorted.
func checkStore(t *testing.T, s *intervalStore) {
	t.Helper()
	var prev *node
	for bi, b := range s.blocks {
		if len(b.items) == 0 || len(b.items) > 2*blockSize {
			t.Fatalf("block %d holds %d items", bi, len(b.items))
		}
		maxEnd := b.items[0].end
		for _, n := range b.items {
			if n.blk != b {
				t.Fatalf("a node of block %d points to another block", bi)
			}
			maxEnd = max(maxEnd, n.end)
			if prev != nil && less(n.absStart(), n.absEnd(), n.seq, prev.absStart(), prev.absEnd(), prev.seq) {
				t.Fatalf("out of order: [%d,%d)#%d after [%d,%d)#%d", n.absStart(), n.absEnd(), n.seq, prev.absStart(), prev.absEnd(), prev.seq)
			}
			prev = n
		}
		if b.maxEnd != maxEnd {
			t.Fatalf("block %d has maxEnd %d, want %d", bi, b.maxEnd, maxEnd)
		}
	}
}

// Ported from VS Code: intervalTree.test.ts ('generated': gen01 to gen18 and
// 'force delta overflow'). Each runs its inserts, deletes and searches on the
// store and on the brute-force oracle and compares the whole contents after
// each step.
func TestGeneratedIntervals(t *testing.T) {
	for _, tt := range generatedIntervalTests {
		t.Run(tt.name, func(t *testing.T) {
			var store intervalStore
			var or oracle
			var nodes []*node
			var items []*oracleItem
			for _, op := range tt.ops {
				switch op.kind {
				case "insert":
					n := &node{}
					store.insert(n, op.begin, op.end)
					nodes = append(nodes, n)
					items = append(items, or.insert(op.begin, op.end))
				case "delete":
					store.remove(nodes[op.id])
					or.remove(items[op.id])
					nodes[op.id], items[op.id] = nil, nil
				case "change":
					store.remove(nodes[op.id])
					store.place(nodes[op.id], op.begin, op.end)
					or.remove(items[op.id])
					items[op.id] = or.insert(op.begin, op.end)
				case "search":
					if got, want := storeSearch(&store, op.begin, op.end), or.search(op.begin, op.end); !reflect.DeepEqual(got, want) {
						t.Fatalf("search %d..%d = %v, want %v", op.begin, op.end, got, want)
					}
					continue
				}
				checkStore(t, &store)
				if got, want := storePositions(&store), or.positions(); !reflect.DeepEqual(got, want) {
					t.Fatalf("after %+v: %v, want %v", op, got, want)
				}
			}
		})
	}
}

// Ported from VS Code: intervalTree.test.ts ('searching': the example tree of
// Cormen's book and its five searches).
func TestCormenSearches(t *testing.T) {
	var store intervalStore
	for _, iv := range [][2]int{{16, 21}, {8, 9}, {25, 30}, {5, 8}, {15, 23}, {17, 19}, {26, 26}, {0, 3}, {6, 10}, {19, 20}} {
		store.insert(&node{}, iv[0], iv[1])
	}
	tests := []struct {
		name       string
		start, end int
		want       [][2]int
	}{
		{"1->2", 1, 2, [][2]int{{0, 3}}},
		{"4->8", 4, 8, [][2]int{{5, 8}, {6, 10}, {8, 9}}},
		{"10->15", 10, 15, [][2]int{{6, 10}, {15, 23}}},
		{"21->25", 21, 25, [][2]int{{15, 23}, {16, 21}, {25, 30}}},
		{"24->24", 24, 24, [][2]int{}},
	}
	for _, tt := range tests {
		if got := storeSearch(&store, tt.start, tt.end); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("cormen %s = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// Emetos's own version of VS Code's generated tests (their random generator is
// switched off in VS Code): random inserts, deletes, changes and searches on
// several hundred intervals, so blocks split and are skipped, against the oracle.
func TestRandomIntervals(t *testing.T) {
	for seed := int64(1); seed <= 12; seed++ {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			var store intervalStore
			var or oracle
			type pair struct {
				n  *node
				it *oracleItem
			}
			var live []pair
			randomRange := func(max int) (int, int) {
				b := rng.Intn(max)
				l := rng.Intn(10)
				if rng.Intn(10) < 2 {
					l = rng.Intn(max - b + 1)
				}
				return b, b + l
			}
			for step := 0; step < 1200; step++ {
				switch k := rng.Intn(10); {
				case k < 5 || len(live) < 5:
					b, e := randomRange(2000)
					n := &node{}
					store.insert(n, b, e)
					live = append(live, pair{n, or.insert(b, e)})
				case k < 7:
					i := rng.Intn(len(live))
					store.remove(live[i].n)
					or.remove(live[i].it)
					live = append(live[:i], live[i+1:]...)
				case k < 8:
					i := rng.Intn(len(live))
					b, e := randomRange(2000)
					store.remove(live[i].n)
					store.place(live[i].n, b, e)
					or.remove(live[i].it)
					live[i].it = or.insert(b, e)
				default:
					b, e := randomRange(2000)
					if got, want := storeSearch(&store, b, e), or.search(b, e); !reflect.DeepEqual(got, want) {
						t.Fatalf("step %d: search %d..%d = %v, want %v", step, b, e, got, want)
					}
				}
				if step%50 == 0 {
					checkStore(t, &store)
					if got, want := storePositions(&store), or.positions(); !reflect.DeepEqual(got, want) {
						t.Fatalf("step %d: contents differ", step)
					}
				}
			}
			checkStore(t, &store)
		})
	}
}

// Random replacements applied to the store move every decoration as acceptEdit
// does on its own, and keep the order and the invariants. About 1,000 decorations of
// all four stickiness modes make several blocks.
func TestRandomEditsMoveDecorations(t *testing.T) {
	for seed := int64(1); seed <= 6; seed++ {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			var store intervalStore
			var or oracle
			size := 3000
			for i := 0; i < 1000; i++ {
				b := rng.Intn(size)
				e := b + rng.Intn(20)
				if rng.Intn(10) == 0 {
					e = b + rng.Intn(500)
				}
				st := Stickiness(rng.Intn(4))
				collapse := rng.Intn(5) == 0
				n := &node{opts: DecorationOptions{Stickiness: st, CollapseOnReplaceEdit: collapse}}
				store.insert(n, b, e)
				it := or.insert(b, e)
				it.stick, it.collapse = st, collapse
			}
			for step := 0; step < 200; step++ {
				editStart := rng.Intn(size + 1)
				editEnd := min(size, editStart+rng.Intn(30))
				if rng.Intn(15) == 0 {
					editEnd = min(size, editStart+rng.Intn(400))
				}
				textLen := rng.Intn(20)
				store.applyEdit(editStart, editEnd, textLen, false)
				or.applyEdit(editStart, editEnd, textLen)
				size += textLen - (editEnd - editStart)
				if step%10 == 0 {
					checkStore(t, &store)
				}
				if got, want := storePositions(&store), or.positions(); !reflect.DeepEqual(got, want) {
					t.Fatalf("step %d: edit [%d,%d) with %d bytes: contents differ", step, editStart, editEnd, textLen)
				}
			}
		})
	}
}
