package extension

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"
)

// The trees of test items (step 26.1): what the observer of a tree hears of the changes, and the errors of the collections.

// recorder collects the changes of a tree as "kind path".
type treeRecorder struct {
	mu  sync.Mutex
	got []string
}

func (r *treeRecorder) observe(ch TestTreeChange) {
	kind := map[TestTreeChangeKind]string{TestItemAdded: "add", TestItemRemoved: "remove", TestItemChanged: "change"}[ch.Kind]
	parent := "-"
	if ch.Parent != nil {
		parent = ch.Parent.ID()
	}
	r.mu.Lock()
	r.got = append(r.got, fmt.Sprintf("%s %s (parent %s) %q", kind, strings.Join(ch.Path, "/"), parent, ch.Data.Label))
	r.mu.Unlock()
}

func (r *treeRecorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.got
	r.got = nil
	return out
}

func mustAdd(t *testing.T, c *TestItemCollection, items ...*TestItem) {
	t.Helper()
	for _, it := range items {
		if err := c.Add(it); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTestTreeChangesAreReportedInOrder(t *testing.T) {
	cases := []struct {
		name string
		do   func(t *testing.T, root *TestItemCollection)
		want []string
	}{
		{"an item", func(t *testing.T, root *TestItemCollection) { mustAdd(t, root, NewTestItem("a", "A", "")) },
			[]string{`add a (parent -) "A"`}},
		{"a subtree made first arrives whole, the parent first", func(t *testing.T, root *TestItemCollection) {
			a := NewTestItem("a", "A", "")
			b := NewTestItem("b", "B", "")
			mustAdd(t, b.Children(), NewTestItem("c", "C", ""))
			mustAdd(t, a.Children(), b)
			mustAdd(t, root, a)
		}, []string{`add a (parent -) "A"`, `add a/b (parent a) "B"`, `add a/b/c (parent b) "C"`}},
		{"a child of an item that is in the tree", func(t *testing.T, root *TestItemCollection) {
			a := NewTestItem("a", "A", "")
			mustAdd(t, root, a)
			mustAdd(t, a.Children(), NewTestItem("b", "B", ""))
		}, []string{`add a (parent -) "A"`, `add a/b (parent a) "B"`}},
		{"an item that is not in the tree reports nothing", func(t *testing.T, _ *TestItemCollection) {
			a := NewTestItem("a", "A", "")
			a.Update(func(d *TestItemData) { d.Label = "X" })
			mustAdd(t, a.Children(), NewTestItem("b", "B", ""))
		}, nil},
		{"a change", func(t *testing.T, root *TestItemCollection) {
			a := NewTestItem("a", "A", "")
			mustAdd(t, root, a)
			a.Update(func(d *TestItemData) { d.Label = "A2"; d.Tags = []string{"t"} })
		}, []string{`add a (parent -) "A"`, `change a (parent -) "A2"`}},
		{"a delete reports the item once", func(t *testing.T, root *TestItemCollection) {
			a := NewTestItem("a", "A", "")
			mustAdd(t, root, a)
			mustAdd(t, a.Children(), NewTestItem("b", "B", ""))
			if !root.Delete("a") || root.Delete("a") {
				t.Error("Delete reports")
			}
		}, []string{`add a (parent -) "A"`, `add a/b (parent a) "B"`, `remove a (parent -) "A"`}},
		{"adding an item with the id of one that is there replaces it", func(t *testing.T, root *TestItemCollection) {
			mustAdd(t, root, NewTestItem("a", "old", ""))
			mustAdd(t, root, NewTestItem("a", "new", ""))
			if root.Size() != 1 {
				t.Errorf("size %d", root.Size())
			}
		}, []string{`add a (parent -) "old"`, `remove a (parent -) "old"`, `add a (parent -) "new"`}},
		{"adding the same item again does nothing", func(t *testing.T, root *TestItemCollection) {
			a := NewTestItem("a", "A", "")
			mustAdd(t, root, a, a)
		}, []string{`add a (parent -) "A"`}},
		{"replace keeps the ones that stay and orders the new at the end", func(t *testing.T, root *TestItemCollection) {
			a, b := NewTestItem("a", "A", ""), NewTestItem("b", "B", "")
			mustAdd(t, root, a, b)
			if err := root.Replace([]*TestItem{NewTestItem("c", "C", ""), a}); err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, it := range root.All() {
				ids = append(ids, it.ID())
			}
			if !reflect.DeepEqual(ids, []string{"a", "c"}) {
				t.Errorf("ids %v", ids)
			}
		}, []string{`add a (parent -) "A"`, `add b (parent -) "B"`, `remove b (parent -) "B"`, `add c (parent -) "C"`}},
		{"a deleted item can be added again, with its children", func(t *testing.T, root *TestItemCollection) {
			a := NewTestItem("a", "A", "")
			mustAdd(t, a.Children(), NewTestItem("b", "B", ""))
			mustAdd(t, root, a)
			root.Delete("a")
			mustAdd(t, root, a)
		}, []string{`add a (parent -) "A"`, `add a/b (parent a) "B"`, `remove a (parent -) "A"`, `add a (parent -) "A"`, `add a/b (parent a) "B"`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &treeRecorder{}
			root := NewTestTree(rec.observe)
			c.do(t, root)
			if got := rec.take(); !reflect.DeepEqual(got, c.want) {
				t.Errorf("changes\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestTestItemCollectionErrors(t *testing.T) {
	root := NewTestTree(nil)
	a := NewTestItem("a", "A", "")
	mustAdd(t, root, a)
	b := NewTestItem("b", "B", "")
	mustAdd(t, a.Children(), b)
	other := NewTestTree(nil)
	cases := []struct {
		name  string
		err   error
		route string
	}{
		{"nil", root.Add(nil), "TestItemCollection.addLocked.ExtensionTestItemInvalid"},
		{"empty id", root.Add(NewTestItem("", "x", "")), "TestItemCollection.addLocked.ExtensionTestItemInvalid"},
		{"NUL in the id", root.Add(NewTestItem("a\x00b", "x", "")), "TestItemCollection.addLocked.ExtensionTestItemInvalid"},
		{"in another collection already", other.Add(b), "TestItemCollection.addLocked.ExtensionTestItemOwned"},
		{"an item that is in a collection in another", other.Add(a), "TestItemCollection.addLocked.ExtensionTestItemOwned"},
		{"its own ancestor", b.Children().Add(a), "TestItemCollection.addLocked.ExtensionTestItemOwned"},
		{"itself", a.Children().Add(a), "TestItemCollection.addLocked.ExtensionTestItemOwned"},
		{"replace with an invalid item", root.Replace([]*TestItem{NewTestItem("", "x", "")}), "TestItemCollection.Replace.ExtensionTestItemInvalid"},
		{"replace with the same id twice", root.Replace([]*TestItem{NewTestItem("x", "x", ""), NewTestItem("x", "x", "")}), "TestItemCollection.Replace.ExtensionTestItemInvalid"},
		{"replace with an item from another collection", root.Replace([]*TestItem{b}), "TestItemCollection.Replace.ExtensionTestItemOwned"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			appErr, ok := errors.FromError(c.err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", c.err, c.err)
			}
			if !appErr.HasRoute(c.route) {
				t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), c.route)
			}
		})
	}
	if root.Size() != 1 || a.Children().Size() != 1 || other.Size() != 0 {
		t.Error("a failed call changed a collection")
	}
	if a.Parent() != nil || b.Parent() != a || root.Get("a") != a || a.Children().Get("b") != b || root.Get("zz") != nil {
		t.Error("Parent or Get")
	}
}

func TestTestItemDataIsCopied(t *testing.T) {
	it := NewTestItem("a", "A", "file:///a")
	it.Update(func(d *TestItemData) {
		d.Tags = []string{"x"}
		d.Range = &Range{End: Position{Line: 4}}
	})
	d := it.Data()
	d.Tags[0] = "changed"
	d.Range.End.Line = 99
	again := it.Data()
	if again.Tags[0] != "x" || again.Range.End.Line != 4 || again.URI != "file:///a" || again.Label != "A" {
		t.Errorf("the item's data was changed through a copy: %+v", again)
	}
}

func TestTestTreeIsSafeForConcurrentUse(t *testing.T) {
	root := NewTestTree(func(TestTreeChange) {})
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				it := NewTestItem(fmt.Sprintf("g%d-%d", g, i), "x", "")
				if err := root.Add(it); err != nil {
					t.Error(err)
					return
				}
				mustAdd(t, it.Children(), NewTestItem("c", "c", ""))
				it.Update(func(d *TestItemData) { d.Busy = true })
				_ = root.All()
				if i%2 == 0 {
					root.Delete(it.ID())
				}
			}
		}()
	}
	wg.Wait()
	if root.Size() != 400 {
		t.Errorf("size %d, want 400", root.Size())
	}
}
