package kit

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
)

// sampleItems is the tree most tests use:
//
//	a          a folder with a1, a2 (a folder with a2x) and a3
//	b          an empty folder
//	c          a folder that is not read
//	d          a file
func sampleItems() []TreeItem {
	return []TreeItem{
		{Key: "a", Children: []TreeItem{
			{Key: "a1"},
			{Key: "a2", Children: []TreeItem{{Key: "a2x"}}},
			{Key: "a3"},
		}},
		{Key: "b", HasChildren: true},
		{Key: "c", Lazy: true},
		{Key: "d"},
	}
}

// loadLog records what the loader of a model was asked.
type loadLog struct{ keys []string }

func newSampleModel(t *testing.T, expanded ...string) (*TreeModel, *loadLog) {
	t.Helper()
	log := &loadLog{}
	m := NewTreeModel(TreeModelOpts{LoadChildren: func(key string, _ any) { log.keys = append(log.keys, key) }})
	m.SetExpandedKeys(expanded)
	if err := m.SetItems(sampleItems()); err != nil {
		t.Fatal(err)
	}
	return m, log
}

// shape writes the rows as text: two spaces per level, then the key and a mark: + for a node that can be
// expanded, - for one that is expanded, ~ for one that is loading and * for a placeholder.
func shape(m *TreeModel) []string {
	var out []string
	for _, n := range m.Rows() {
		mark := ""
		switch {
		case n.Placeholder:
			mark = "*"
		case n.Loading:
			mark = "~"
		case n.Expanded:
			mark = "-"
		case n.Expandable:
			mark = "+"
		}
		out = append(out, strings.Repeat("  ", n.Depth)+strings.TrimSuffix(n.Key, treePlaceholderSuffix)+mark)
	}
	return out
}

func focusedKey(m *TreeModel) string {
	k, _ := m.Focused()
	return k
}

func selectedKey(m *TreeModel) string {
	k, _ := m.Selected()
	return k
}

func TestTreeModelRows(t *testing.T) {
	cases := []struct {
		name     string
		expanded []string
		want     []string
	}{
		{"collapsed", nil, []string{"a+", "b+", "c+", "d"}},
		{"a expanded", []string{"a"}, []string{"a-", "  a1", "  a2+", "  a3", "b+", "c+", "d"}},
		{"a and a2 expanded", []string{"a", "a2"}, []string{"a-", "  a1", "  a2-", "    a2x", "  a3", "b+", "c+", "d"}},
		{"a2 expanded but a collapsed", []string{"a2"}, []string{"a+", "b+", "c+", "d"}},
		{"an empty folder expanded", []string{"b"}, []string{"a+", "b-", "c+", "d"}},
		{"a lazy folder expanded shows the placeholder", []string{"c"}, []string{"a+", "b+", "c~", "  c*", "d"}},
		{"a leaf cannot be expanded", []string{"d"}, []string{"a+", "b+", "c+", "d"}},
		{"keys the tree does not have are ignored", []string{"zzz"}, []string{"a+", "b+", "c+", "d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newSampleModel(t, tc.expanded...)
			if got := shape(m); !slices.Equal(got, tc.want) {
				t.Errorf("rows %q, want %q", got, tc.want)
			}
			for i, n := range m.Rows() {
				if n.Index != i || m.IndexOf(n.Key) != i {
					t.Errorf("row %d has index %d / IndexOf %d", i, n.Index, m.IndexOf(n.Key))
				}
			}
		})
	}
}

func TestTreeModelNodes(t *testing.T) {
	m, _ := newSampleModel(t, "a", "a2")
	n, ok := m.Node("a2x")
	if !ok || n.Depth != 2 || n.Parent != "a2" || n.Expandable {
		t.Errorf("a2x = %+v, %v", n, ok)
	}
	if _, ok := m.Node("nope"); ok {
		t.Error("an unknown key has no row")
	}
	if got := m.Ancestors("a2x"); !slices.Equal(got, []string{"a", "a2"}) {
		t.Errorf("ancestors %q", got)
	}
	if p, ok := m.Parent("a2"); !ok || p != "a" {
		t.Errorf("parent %q %v", p, ok)
	}
	if _, ok := m.Parent("a"); ok {
		t.Error("a root has no parent")
	}
	if got := m.Children("a"); !slices.Equal(got, []string{"a1", "a2", "a3"}) {
		t.Errorf("children %q", got)
	}
	if !m.Has("a2x") || m.Has("x") {
		t.Error("Has")
	}
	m.Collapse("a")
	if m.IndexOf("a2x") != -1 || !m.Has("a2x") || !m.IsExpanded("a2") {
		t.Error("a node below a collapsed one is known, not shown, and keeps its state")
	}
	if got := m.Row(99); got.Index != -1 {
		t.Errorf("Row out of range = %+v", got)
	}
}

func TestTreeModelExpansionIsKeptAcrossRebuilds(t *testing.T) {
	m, _ := newSampleModel(t, "a", "a2")
	m.SetFocus("a2x")
	m.SetSelected("a3")

	// the same items, a new node and a changed order: everything that is back is expanded
	items := sampleItems()
	items[0].Children = append([]TreeItem{{Key: "a0"}}, items[0].Children...)
	items = append([]TreeItem{{Key: "z", Children: []TreeItem{{Key: "z1"}}}}, items...)
	if err := m.SetItems(items); err != nil {
		t.Fatal(err)
	}
	want := []string{"z+", "a-", "  a0", "  a1", "  a2-", "    a2x", "  a3", "b+", "c+", "d"}
	if got := shape(m); !slices.Equal(got, want) {
		t.Errorf("rows %q, want %q", got, want)
	}
	if focusedKey(m) != "a2x" || selectedKey(m) != "a3" {
		t.Errorf("focus %q and selection %q are kept by key", focusedKey(m), selectedKey(m))
	}

	// a node that goes away and comes back is expanded again
	if err := m.SetItems([]TreeItem{{Key: "a", Children: []TreeItem{{Key: "a1"}}}}); err != nil {
		t.Fatal(err)
	}
	if got := shape(m); !slices.Equal(got, []string{"a-", "  a1"}) {
		t.Errorf("rows %q", got)
	}
	if focusedKey(m) != "a" {
		t.Errorf("the focus of a row that is gone goes to the nearest node above it that is shown, got %q", focusedKey(m))
	}
	if selectedKey(m) != "" {
		t.Errorf("the selection of a node that is gone is dropped, got %q", selectedKey(m))
	}
	if err := m.SetItems(sampleItems()); err != nil {
		t.Fatal(err)
	}
	if got := shape(m); !slices.Equal(got, []string{"a-", "  a1", "  a2-", "    a2x", "  a3", "b+", "c+", "d"}) {
		t.Errorf("rows %q", got)
	}

	// the state is a list of keys, to keep between sessions
	if got := m.ExpandedKeys(); !slices.Equal(got, []string{"a", "a2"}) {
		t.Errorf("expanded keys %q", got)
	}
	m.SetExpandedKeys([]string{"b", "a"})
	if got := shape(m); !slices.Equal(got, []string{"a-", "  a1", "  a2+", "  a3", "b-", "c+", "d"}) {
		t.Errorf("rows %q", got)
	}
}

func TestTreeModelListeners(t *testing.T) {
	m, _ := newSampleModel(t)
	calls := 0
	m.AddListener(func() { calls++ })
	steps := []struct {
		name string
		do   func()
		want int
	}{
		{"expand", func() { m.Expand("a") }, 1},
		{"expand again", func() { m.Expand("a") }, 0},
		{"a leaf cannot expand", func() { m.Expand("d") }, 0},
		{"focus", func() { m.SetFocus("a1") }, 1},
		{"the same focus", func() { m.SetFocus("a1") }, 0},
		{"select", func() { m.SetSelected("a1") }, 1},
		{"the same selection", func() { m.SetSelected("a1") }, 0},
		{"expand all tells once", func() { m.ExpandAll() }, 1},
		{"set items tells once", func() { _ = m.SetItems(sampleItems()) }, 1},
		{"a click tells once", func() { m.Click("b", false, false) }, 1},
		{"a failed SetItems tells nobody", func() { _ = m.SetItems([]TreeItem{{Key: "x"}, {Key: "x"}}) }, 0},
	}
	for _, s := range steps {
		calls = 0
		s.do()
		if calls != s.want {
			t.Errorf("%s: %d notifications, want %d", s.name, calls, s.want)
		}
	}
}

func TestTreeModelLazyLoading(t *testing.T) {
	m, log := newSampleModel(t)
	if len(log.keys) != 0 {
		t.Fatalf("nothing is asked for before an expansion: %q", log.keys)
	}
	if !m.Expand("c") {
		t.Fatal("a lazy node can be expanded")
	}
	if got := shape(m); !slices.Equal(got, []string{"a+", "b+", "c~", "  c*", "d"}) {
		t.Errorf("while loading: %q", got)
	}
	if !slices.Equal(log.keys, []string{"c"}) {
		t.Errorf("the loader was asked for %q", log.keys)
	}
	if ph := m.Row(3); !ph.Placeholder || ph.Key != PlaceholderKey("c") || ph.Parent != "c" || ph.Depth != 1 {
		t.Errorf("placeholder %+v", ph)
	}
	// a collapse and a new expansion while the reading goes on asks nothing new
	m.Collapse("c")
	m.Expand("c")
	if len(log.keys) != 1 {
		t.Errorf("the loader was asked again: %q", log.keys)
	}
	// the answer
	if err := m.SetChildren("c", []TreeItem{{Key: "c1"}, {Key: "c2", Lazy: true}}); err != nil {
		t.Fatal(err)
	}
	if got := shape(m); !slices.Equal(got, []string{"a+", "b+", "c-", "  c1", "  c2+", "d"}) {
		t.Errorf("loaded: %q", got)
	}
	// loaded nodes are not asked for again
	m.Collapse("c")
	m.Expand("c")
	if len(log.keys) != 1 {
		t.Errorf("the loader was asked again: %q", log.keys)
	}
	// and the children of a child load in turn
	m.Expand("c2")
	if !slices.Equal(log.keys, []string{"c", "c2"}) {
		t.Errorf("asked for %q", log.keys)
	}
	// an answer for a node that is gone is ignored
	if err := m.SetChildren("zzz", []TreeItem{{Key: "q"}}); err != nil {
		t.Fatal(err)
	}
	if m.Has("q") {
		t.Error("an answer for a node that is gone is ignored")
	}
}

func TestTreeModelLazyLoadingEdgeCases(t *testing.T) {
	t.Run("an empty answer leaves an empty folder", func(t *testing.T) {
		m, _ := newSampleModel(t, "c")
		if err := m.SetChildren("c", nil); err != nil {
			t.Fatal(err)
		}
		if got := shape(m); !slices.Equal(got, []string{"a+", "b+", "c-", "d"}) {
			t.Errorf("rows %q", got)
		}
		if n, _ := m.Node("c"); !n.Expandable || n.Loading {
			t.Errorf("c = %+v: an empty folder keeps its chevron", n)
		}
	})
	t.Run("a failed reading collapses the node, and expanding tries again", func(t *testing.T) {
		m, log := newSampleModel(t, "c")
		m.FailLoad("c")
		if got := shape(m); !slices.Equal(got, []string{"a+", "b+", "c+", "d"}) {
			t.Errorf("rows %q", got)
		}
		m.Expand("c")
		if !slices.Equal(log.keys, []string{"c", "c"}) {
			t.Errorf("asked for %q", log.keys)
		}
		m.FailLoad("d") // not lazy: ignored
		m.FailLoad("nope")
	})
	t.Run("a loader that answers at once never shows the placeholder", func(t *testing.T) {
		var m *TreeModel
		shown := 0
		m = NewTreeModel(TreeModelOpts{LoadChildren: func(key string, _ any) {
			if err := m.SetChildren(key, []TreeItem{{Key: key + "/x"}}); err != nil {
				t.Error(err)
			}
		}})
		m.AddListener(func() {
			for _, n := range m.Rows() {
				if n.Placeholder {
					shown++
				}
			}
		})
		if err := m.SetItems(sampleItems()); err != nil {
			t.Fatal(err)
		}
		m.Expand("c")
		if got := shape(m); !slices.Equal(got, []string{"a+", "b+", "c-", "  c/x", "d"}) {
			t.Errorf("rows %q", got)
		}
		if shown != 0 {
			t.Errorf("the placeholder was shown %d times", shown)
		}
	})
	t.Run("an expansion restored before the items arrive loads what is shown", func(t *testing.T) {
		m, log := newSampleModel(t, "c")
		if !slices.Equal(log.keys, []string{"c"}) {
			t.Errorf("asked for %q", log.keys)
		}
		// giving the items again while the reading goes on asks nothing new
		if err := m.SetItems(sampleItems()); err != nil {
			t.Fatal(err)
		}
		if len(log.keys) != 1 {
			t.Errorf("asked again: %q", log.keys)
		}
		if got := shape(m); !slices.Equal(got, []string{"a+", "b+", "c~", "  c*", "d"}) {
			t.Errorf("rows %q", got)
		}
	})
	t.Run("a lazy node below a collapsed one waits", func(t *testing.T) {
		m := NewTreeModel(TreeModelOpts{LoadChildren: func(string, any) { t.Error("loaded a node that is not shown") }})
		m.SetExpandedKeys([]string{"x/y"})
		if err := m.SetItems([]TreeItem{{Key: "x", Children: []TreeItem{{Key: "x/y", Lazy: true}}}}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("answering a node whose children are shown keeps the state of the ones that stay", func(t *testing.T) {
		m, _ := newSampleModel(t, "a", "a2")
		err := m.SetChildren("a", []TreeItem{{Key: "a2", Children: []TreeItem{{Key: "a2x"}, {Key: "a2y"}}}, {Key: "a4"}})
		if err != nil {
			t.Fatal(err)
		}
		if got := shape(m); !slices.Equal(got, []string{"a-", "  a2-", "    a2x", "    a2y", "  a4", "b+", "c+", "d"}) {
			t.Errorf("rows %q", got)
		}
	})
	t.Run("a lazy node with children given is not lazy", func(t *testing.T) {
		m := NewTreeModel(TreeModelOpts{LoadChildren: func(string, any) { t.Error("loaded") }})
		m.SetExpandedKeys([]string{"x"})
		if err := m.SetItems([]TreeItem{{Key: "x", Lazy: true, Children: []TreeItem{{Key: "y"}}}}); err != nil {
			t.Fatal(err)
		}
		if got := shape(m); !slices.Equal(got, []string{"x-", "  y"}) {
			t.Errorf("rows %q", got)
		}
	})
}

func TestTreeModelRefusesBadKeys(t *testing.T) {
	cases := []struct {
		name  string
		items []TreeItem
		set   func(m *TreeModel, items []TreeItem) error
		route string
	}{
		{"an empty key", []TreeItem{{Key: ""}}, func(m *TreeModel, i []TreeItem) error { return m.SetItems(i) }, "TreeModel.SetItems->checkItems.TreeItemKeyEmpty"},
		{"an empty key below", []TreeItem{{Key: "x", Children: []TreeItem{{Key: ""}}}}, func(m *TreeModel, i []TreeItem) error { return m.SetItems(i) }, "TreeModel.SetItems->checkItems->checkItems.TreeItemKeyEmpty"},
		{"a repeated key", []TreeItem{{Key: "x"}, {Key: "x"}}, func(m *TreeModel, i []TreeItem) error { return m.SetItems(i) }, "TreeModel.SetItems->checkItems.TreeItemKeyDuplicated"},
		{"a repeated key at another level", []TreeItem{{Key: "x", Children: []TreeItem{{Key: "x"}}}}, func(m *TreeModel, i []TreeItem) error { return m.SetItems(i) }, "TreeModel.SetItems->checkItems->checkItems.TreeItemKeyDuplicated"},
		{"children that use a key of another node", []TreeItem{{Key: "d"}}, func(m *TreeModel, i []TreeItem) error { return m.SetChildren("a", i) }, "TreeModel.SetChildren->checkItems.TreeItemKeyDuplicated"},
		{"children with an empty key", []TreeItem{{Key: ""}}, func(m *TreeModel, i []TreeItem) error { return m.SetChildren("a", i) }, "TreeModel.SetChildren->checkItems.TreeItemKeyEmpty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newSampleModel(t, "a")
			before := shape(m)
			err := tc.set(m, tc.items)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if got := shape(m); !slices.Equal(got, before) {
				t.Errorf("a refused change changed the rows: %q", got)
			}
		})
	}
	// the keys of the old children can be used again
	m, _ := newSampleModel(t, "a")
	if err := m.SetChildren("a", []TreeItem{{Key: "a1"}, {Key: "a2"}}); err != nil {
		t.Errorf("reusing the keys of the old children: %v", err)
	}
}

func TestTreeModelExpandCollapse(t *testing.T) {
	collapsed := []string{"a+", "b+", "c+", "d"}
	aOpen := []string{"a-", "  a1", "  a2+", "  a3", "b+", "c+", "d"}
	cases := []struct {
		name string
		do   func(m *TreeModel) bool
		want []string
		ret  bool
	}{
		{"expand", func(m *TreeModel) bool { return m.Expand("a") }, aOpen, true},
		{"expand a leaf", func(m *TreeModel) bool { return m.Expand("d") }, collapsed, false},
		{"expand an unknown key", func(m *TreeModel) bool { return m.Expand("q") }, collapsed, false},
		{"collapse a collapsed node", func(m *TreeModel) bool { return m.Collapse("b") }, collapsed, false},
		{"collapse a leaf", func(m *TreeModel) bool { return m.Collapse("d") }, collapsed, false},
		{"toggle", func(m *TreeModel) bool { return m.Toggle("a", false) }, aOpen, true},
		{"toggle recursively expands everything below", func(m *TreeModel) bool { return m.Toggle("a", true) },
			[]string{"a-", "  a1", "  a2-", "    a2x", "  a3", "b+", "c+", "d"}, true},
		{"toggle a leaf", func(m *TreeModel) bool { return m.Toggle("d", false) }, collapsed, false},
		{"expand all", func(m *TreeModel) bool { m.ExpandAll(); return true },
			[]string{"a-", "  a1", "  a2-", "    a2x", "  a3", "b-", "c~", "  c*", "d"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newSampleModel(t)
			if ret := tc.do(m); ret != tc.ret {
				t.Errorf("returned %v", ret)
			}
			if got := shape(m); !slices.Equal(got, tc.want) {
				t.Errorf("rows %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("collapse all", func(t *testing.T) {
		m, _ := newSampleModel(t, "a", "a2", "b")
		m.SetFocus("a2x")
		m.CollapseAll()
		if got := shape(m); !slices.Equal(got, []string{"a+", "b+", "c+", "d"}) {
			t.Errorf("rows %q", got)
		}
		if focusedKey(m) != "a" {
			t.Errorf("the focus goes to the root above it, got %q", focusedKey(m))
		}
		if len(m.ExpandedKeys()) != 0 {
			t.Errorf("expanded keys %q", m.ExpandedKeys())
		}
	})
	t.Run("collapsing a node moves a focus below it to the node, and keeps the selection", func(t *testing.T) {
		m, _ := newSampleModel(t, "a", "a2")
		m.SetFocus("a2x")
		m.SetSelected("a2x")
		m.Collapse("a")
		if focusedKey(m) != "a" || selectedKey(m) != "a2x" {
			t.Errorf("focus %q, selection %q", focusedKey(m), selectedKey(m))
		}
		m.Expand("a")
		if got := shape(m); !slices.Equal(got, []string{"a-", "  a1", "  a2-", "    a2x", "  a3", "b+", "c+", "d"}) {
			t.Errorf("rows %q", got)
		}
	})
}

func TestTreeModelReveal(t *testing.T) {
	m, _ := newSampleModel(t)
	i, ok := m.Reveal("a2x")
	if !ok || i != 3 {
		t.Errorf("Reveal(a2x) = %d, %v", i, ok)
	}
	if got := shape(m); !slices.Equal(got, []string{"a-", "  a1", "  a2-", "    a2x", "  a3", "b+", "c+", "d"}) {
		t.Errorf("rows %q", got)
	}
	if i, ok := m.Reveal("d"); !ok || i != 7 {
		t.Errorf("Reveal(d) = %d, %v", i, ok)
	}
	if i, ok := m.Reveal("c/deep"); ok || i != -1 {
		t.Errorf("a node that is not read: %d, %v", i, ok)
	}
}

func TestTreeModelActiveGuides(t *testing.T) {
	cases := []struct {
		name            string
		focus, selected string
		want            []string
	}{
		{"nothing", "", "", nil},
		{"a leaf: its parent", "a1", "", []string{"a"}},
		{"an expanded folder with children: itself", "a2", "", []string{"a2"}},
		{"a collapsed folder: its parent", "a2", "", []string{"a"}},
		{"a root leaf has no guide", "d", "", nil},
		{"focus and selection", "a1", "a2x", []string{"a", "a2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expanded := []string{"a", "a2"}
			if tc.name == "a collapsed folder: its parent" {
				expanded = []string{"a"}
			}
			m, _ := newSampleModel(t, expanded...)
			if tc.focus != "" {
				m.SetFocus(tc.focus)
			}
			if tc.selected != "" {
				m.SetSelected(tc.selected)
			}
			var got []string
			for k := range m.ActiveGuides() {
				got = append(got, k)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("active guides %q, want %q", got, tc.want)
			}
		})
	}
}

// keyCase is one key press in a situation: the expanded nodes, the focus and the selection before, the
// action, and what the model and the result say after.
type keyCase struct {
	name     string
	expanded []string
	focus    string
	selected string
	noToggle bool // ToggleOnSelect is off
	action   TreeAction
	handled  bool
	wantFoc  string
	wantSel  string
	sel, opn bool // the result says Select / Open
	rows     []string
}

func TestTreeModelKeys(t *testing.T) {
	collapsed := []string{"a+", "b+", "c+", "d"}
	aOpen := []string{"a-", "  a1", "  a2+", "  a3", "b+", "c+", "d"}
	aAndA2Open := []string{"a-", "  a1", "  a2-", "    a2x", "  a3", "b+", "c+", "d"}
	loading := []string{"a+", "b+", "c~", "  c*", "d"}
	cases := []keyCase{
		// Down
		{name: "down with no focus focuses the first row", action: TreeActionDown, handled: true, wantFoc: "a", rows: collapsed},
		{name: "down", focus: "a", action: TreeActionDown, handled: true, wantFoc: "b", rows: collapsed},
		{name: "down into the children", expanded: []string{"a"}, focus: "a", action: TreeActionDown, handled: true, wantFoc: "a1", rows: aOpen},
		{name: "down out of the children", expanded: []string{"a"}, focus: "a3", action: TreeActionDown, handled: true, wantFoc: "b", rows: aOpen},
		{name: "down on the last row stays", focus: "d", action: TreeActionDown, handled: true, wantFoc: "d", rows: collapsed},
		{name: "down skips the placeholder", expanded: []string{"c"}, focus: "c", action: TreeActionDown, handled: true, wantFoc: "d", rows: loading},
		// Up
		{name: "up with no focus focuses the first row", action: TreeActionUp, handled: true, wantFoc: "a", rows: collapsed},
		{name: "up", focus: "d", action: TreeActionUp, handled: true, wantFoc: "c", rows: collapsed},
		{name: "up on the first row stays", focus: "a", action: TreeActionUp, handled: true, wantFoc: "a", rows: collapsed},
		{name: "up from the first child goes to the parent", expanded: []string{"a"}, focus: "a1", action: TreeActionUp, handled: true, wantFoc: "a", rows: aOpen},
		{name: "up skips the placeholder", expanded: []string{"c"}, focus: "d", action: TreeActionUp, handled: true, wantFoc: "c", rows: loading},
		// Home and End
		{name: "home", expanded: []string{"a"}, focus: "d", action: TreeActionFirst, handled: true, wantFoc: "a", rows: aOpen},
		{name: "home on the first row", focus: "a", action: TreeActionFirst, handled: true, wantFoc: "a", rows: collapsed},
		{name: "end", expanded: []string{"a"}, focus: "a", action: TreeActionLast, handled: true, wantFoc: "d", rows: aOpen},
		{name: "end on the last row", focus: "d", action: TreeActionLast, handled: true, wantFoc: "d", rows: collapsed},
		{name: "end when the placeholder is last", expanded: []string{"c"}, focus: "a", action: TreeActionLast, handled: true, wantFoc: "d", rows: loading},
		// Right
		{name: "right on a collapsed folder expands it", focus: "a", action: TreeActionExpand, handled: true, wantFoc: "a", rows: aOpen},
		{name: "right on an expanded folder focuses its first child", expanded: []string{"a"}, focus: "a", action: TreeActionExpand, handled: true, wantFoc: "a1", rows: aOpen},
		{name: "right on a leaf is not used", focus: "d", action: TreeActionExpand, handled: false, wantFoc: "d", rows: collapsed},
		{name: "right on a leaf in a folder is not used", expanded: []string{"a"}, focus: "a1", action: TreeActionExpand, handled: false, wantFoc: "a1", rows: aOpen},
		{name: "right with no focus is not used", action: TreeActionExpand, handled: false, rows: collapsed},
		{name: "right on an empty folder expands it", focus: "b", action: TreeActionExpand, handled: true, wantFoc: "b", rows: []string{"a+", "b-", "c+", "d"}},
		{name: "right on an expanded empty folder does nothing", expanded: []string{"b"}, focus: "b", action: TreeActionExpand, handled: true, wantFoc: "b", rows: []string{"a+", "b-", "c+", "d"}},
		{name: "right on a lazy folder expands it and loads", focus: "c", action: TreeActionExpand, handled: true, wantFoc: "c", rows: loading},
		{name: "right on a folder that is loading does not focus the placeholder", expanded: []string{"c"}, focus: "c", action: TreeActionExpand, handled: true, wantFoc: "c", rows: loading},
		// Left
		{name: "left on an expanded folder collapses it", expanded: []string{"a"}, focus: "a", action: TreeActionCollapse, handled: true, wantFoc: "a", rows: collapsed},
		{name: "left on a child goes to the parent", expanded: []string{"a"}, focus: "a3", action: TreeActionCollapse, handled: true, wantFoc: "a", rows: aOpen},
		{name: "left on a collapsed folder in a folder goes to the parent", expanded: []string{"a"}, focus: "a2", action: TreeActionCollapse, handled: true, wantFoc: "a", rows: aOpen},
		{name: "left on an expanded inner folder collapses it", expanded: []string{"a", "a2"}, focus: "a2", action: TreeActionCollapse, handled: true, wantFoc: "a2", rows: aOpen},
		{name: "left on a collapsed root is not used", focus: "a", action: TreeActionCollapse, handled: false, wantFoc: "a", rows: collapsed},
		{name: "left on a root leaf is not used", focus: "d", action: TreeActionCollapse, handled: false, wantFoc: "d", rows: collapsed},
		{name: "left with no focus is not used", action: TreeActionCollapse, handled: false, rows: collapsed},
		{name: "left on a deep leaf goes to its parent", expanded: []string{"a", "a2"}, focus: "a2x", action: TreeActionCollapse, handled: true, wantFoc: "a2", rows: aAndA2Open},
		// Enter
		{name: "enter on a folder toggles and selects it", focus: "a", action: TreeActionSelect, handled: true, wantFoc: "a", wantSel: "a", sel: true, opn: true, rows: aOpen},
		{name: "enter on an expanded folder collapses it", expanded: []string{"a"}, focus: "a", action: TreeActionSelect, handled: true, wantFoc: "a", wantSel: "a", sel: true, opn: true, rows: collapsed},
		{name: "enter on a leaf selects it", focus: "d", action: TreeActionSelect, handled: true, wantFoc: "d", wantSel: "d", sel: true, opn: true, rows: collapsed},
		{name: "enter on a folder, toggling off, selects only", focus: "a", noToggle: true, action: TreeActionSelect, handled: true, wantFoc: "a", wantSel: "a", sel: true, opn: true, rows: collapsed},
		{name: "enter keeps another selection until it selects", focus: "d", selected: "a", action: TreeActionSelect, handled: true, wantFoc: "d", wantSel: "d", sel: true, opn: true, rows: collapsed},
		{name: "enter with no focus is not used", action: TreeActionSelect, handled: false, rows: collapsed},
		// Space
		{name: "space on a folder toggles it and does not select", focus: "a", action: TreeActionToggle, handled: true, wantFoc: "a", rows: aOpen},
		{name: "space on a folder toggles it even when toggling is off", focus: "a", noToggle: true, action: TreeActionToggle, handled: true, wantFoc: "a", rows: aOpen},
		{name: "space on a leaf selects it and does not open it", focus: "d", action: TreeActionToggle, handled: true, wantFoc: "d", wantSel: "d", sel: true, rows: collapsed},
		{name: "space with no focus is not used", action: TreeActionToggle, handled: false, rows: collapsed},
		// Ctrl+Left
		{name: "collapse all", expanded: []string{"a", "a2"}, focus: "a2x", action: TreeActionCollapseAll, handled: true, wantFoc: "a", rows: collapsed},
		{name: "collapse all with nothing open", focus: "d", action: TreeActionCollapseAll, handled: true, wantFoc: "d", rows: collapsed},
		// Escape
		{name: "escape clears the selection first", focus: "a", selected: "d", action: TreeActionClear, handled: true, wantFoc: "a", rows: collapsed},
		{name: "escape then clears the focus", focus: "a", action: TreeActionClear, handled: true, rows: collapsed},
		{name: "escape with nothing to clear is not used", action: TreeActionClear, handled: false, rows: collapsed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newSampleModel(t, tc.expanded...)
			m.SetToggleOnSelect(!tc.noToggle)
			if tc.focus != "" && !m.SetFocus(tc.focus) {
				t.Fatalf("cannot focus %q", tc.focus)
			}
			if tc.selected != "" {
				m.SetSelected(tc.selected)
			}
			res := m.Perform(tc.action)
			if res.Handled != tc.handled {
				t.Errorf("handled = %v, want %v", res.Handled, tc.handled)
			}
			if got := focusedKey(m); got != tc.wantFoc {
				t.Errorf("focus %q, want %q", got, tc.wantFoc)
			}
			wantSel := tc.wantSel
			if tc.action != TreeActionSelect && tc.action != TreeActionToggle && tc.action != TreeActionClear {
				wantSel = tc.selected
			}
			if got := selectedKey(m); got != wantSel {
				t.Errorf("selection %q, want %q", got, wantSel)
			}
			if res.Select != tc.sel || res.Open != tc.opn {
				t.Errorf("select %v open %v, want %v %v", res.Select, res.Open, tc.sel, tc.opn)
			}
			if (res.Select || res.Open) && res.Node.Key != tc.wantSel && res.Node.Key != tc.wantFoc {
				t.Errorf("result node %q", res.Node.Key)
			}
			if got := shape(m); !slices.Equal(got, tc.rows) {
				t.Errorf("rows %q, want %q", got, tc.rows)
			}
		})
	}
	t.Run("the result of enter has the node as it is after the toggle", func(t *testing.T) {
		m, _ := newSampleModel(t)
		m.SetFocus("a")
		if res := m.Perform(TreeActionSelect); !res.Node.Expanded || res.Node.Key != "a" {
			t.Errorf("node %+v", res.Node)
		}
	})
	t.Run("an empty tree uses no key", func(t *testing.T) {
		m := NewTreeModel()
		for a := TreeActionDown; a <= TreeActionCollapseAll; a++ {
			if m.Perform(a).Handled {
				t.Errorf("action %d handled by an empty tree", a)
			}
		}
	})
}

// flatItems are n leaves r00, r01, ...
func flatItems(n int) []TreeItem {
	items := make([]TreeItem, n)
	for i := range items {
		items[i] = TreeItem{Key: fmt.Sprintf("r%02d", i)}
	}
	return items
}

func TestTreeModelPagesAndScrolling(t *testing.T) {
	// 20 rows of 22 px in a view of 110 px: five rows in view, the content 440 px, the farthest scroll 330.
	cases := []struct {
		name       string
		top        float64
		height     float64
		rows       int
		focus      string
		action     TreeAction
		wantFocus  string
		wantScroll foundation.Opt[float64]
	}{
		{"page down with no focus", 0, 110, 20, "", TreeActionPageDown, "r04", foundation.None[float64]()},
		{"page down from the first row", 0, 110, 20, "r00", TreeActionPageDown, "r04", foundation.None[float64]()},
		{"page down from the last row in view scrolls a page", 0, 110, 20, "r04", TreeActionPageDown, "r09", foundation.Some(110.0)},
		{"page down again", 110, 110, 20, "r09", TreeActionPageDown, "r14", foundation.Some(220.0)},
		{"page down stops at the end of the content", 220, 110, 20, "r14", TreeActionPageDown, "r19", foundation.Some(330.0)},
		{"page down on the last row at the end does nothing", 330, 110, 20, "r19", TreeActionPageDown, "r19", foundation.None[float64]()},
		{"page down from a row near the end", 330, 110, 20, "r15", TreeActionPageDown, "r19", foundation.None[float64]()},
		{"page down from a row below the view scrolls and focuses the last row in view", 0, 110, 20, "r10", TreeActionPageDown, "r09", foundation.Some(110.0)},
		{"page down in a partial row view", 0, 120, 20, "r04", TreeActionPageDown, "r09", foundation.Some(120.0)},
		{"page down when everything fits", 0, 110, 3, "r00", TreeActionPageDown, "r02", foundation.None[float64]()},
		{"page up from the middle of the view", 0, 110, 20, "r03", TreeActionPageUp, "r00", foundation.None[float64]()},
		{"page up from the first row at the top does nothing", 0, 110, 20, "r00", TreeActionPageUp, "r00", foundation.None[float64]()},
		{"page up from the last row in view", 110, 110, 20, "r09", TreeActionPageUp, "r05", foundation.None[float64]()},
		{"page up from the first row in view scrolls a page", 110, 110, 20, "r05", TreeActionPageUp, "r00", foundation.Some(0.0)},
		{"page up with no focus", 110, 110, 20, "", TreeActionPageUp, "r05", foundation.None[float64]()},
		{"page up with a partial first row", 30, 110, 20, "r05", TreeActionPageUp, "r02", foundation.None[float64]()},
		{"down below the view scrolls the least", 0, 110, 20, "r04", TreeActionDown, "r05", foundation.Some(22.0)},
		{"down inside the view does not scroll", 0, 110, 20, "r02", TreeActionDown, "r03", foundation.None[float64]()},
		{"up above the view scrolls the least", 44, 110, 20, "r02", TreeActionUp, "r01", foundation.Some(22.0)},
		{"home scrolls to the top", 200, 110, 20, "r15", TreeActionFirst, "r00", foundation.Some(0.0)},
		{"end scrolls to the end", 0, 110, 20, "r00", TreeActionLast, "r19", foundation.Some(330.0)},
		{"a view that has not been laid out does not scroll", 0, 0, 20, "r00", TreeActionLast, "r19", foundation.None[float64]()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewTreeModel()
			if err := m.SetItems(flatItems(tc.rows)); err != nil {
				t.Fatal(err)
			}
			m.SetViewport(tc.top, tc.height)
			if tc.focus != "" {
				m.SetFocus(tc.focus)
			}
			res := m.Perform(tc.action)
			if got := focusedKey(m); got != tc.wantFocus {
				t.Errorf("focus %q, want %q", got, tc.wantFocus)
			}
			got, gotOK := res.ScrollTo.Get()
			want, wantOK := tc.wantScroll.Get()
			if gotOK != wantOK || got != want {
				t.Errorf("scroll to %v (%v), want %v (%v)", got, gotOK, want, wantOK)
			}
		})
	}
	t.Run("the pages of a tree with a placeholder at the end of the view", func(t *testing.T) {
		// rows: a, a1, a2, a3, b, c (loading), placeholder, d  -> five rows in view show up to the placeholder at index 6 with a view of 7
		m, _ := newSampleModel(t, "a", "c")
		m.SetViewport(0, 7*22)
		m.SetFocus("a")
		m.Perform(TreeActionPageDown)
		if got := focusedKey(m); got != "c" {
			t.Errorf("the last row in view is the placeholder, so the focus is on the row before it: %q", got)
		}
	})
	t.Run("a sequence of pages visits every page", func(t *testing.T) {
		m := NewTreeModel()
		if err := m.SetItems(flatItems(20)); err != nil {
			t.Fatal(err)
		}
		m.SetViewport(0, 110)
		var seen []string
		for range 6 {
			m.Perform(TreeActionPageDown)
			seen = append(seen, focusedKey(m))
		}
		if !slices.Equal(seen, []string{"r04", "r09", "r14", "r19", "r19", "r19"}) {
			t.Errorf("focus after each page down: %q", seen)
		}
		seen = nil
		for range 6 {
			m.Perform(TreeActionPageUp)
			seen = append(seen, focusedKey(m))
		}
		if !slices.Equal(seen, []string{"r15", "r10", "r05", "r00", "r00", "r00"}) {
			t.Errorf("focus after each page up: %q", seen)
		}
	})
	if rows := (func() int { m := NewTreeModel(); m.SetViewport(0, 110); return m.RowsInView() })(); rows != 5 {
		t.Errorf("rows in view %d", rows)
	}
}

func TestTreeModelClicks(t *testing.T) {
	cases := []struct {
		name      string
		expanded  []string
		noToggle  bool
		key       string
		twistie   bool
		recursive bool
		handled   bool
		wantSel   string
		sel       bool
		rows      []string
	}{
		{"a click on a folder toggles and selects it", nil, false, "a", false, false, true, "a", true, []string{"a-", "  a1", "  a2+", "  a3", "b+", "c+", "d"}},
		{"a click on an open folder collapses it", []string{"a"}, false, "a", false, false, true, "a", true, []string{"a+", "b+", "c+", "d"}},
		{"a click on a leaf selects it", nil, false, "d", false, false, true, "d", true, []string{"a+", "b+", "c+", "d"}},
		{"a click on the twistie only toggles", nil, false, "a", true, false, true, "", false, []string{"a-", "  a1", "  a2+", "  a3", "b+", "c+", "d"}},
		{"a click on a folder with toggling off only selects", nil, true, "a", false, false, true, "a", true, []string{"a+", "b+", "c+", "d"}},
		{"a click on the twistie with toggling off toggles", nil, true, "a", true, false, true, "", false, []string{"a-", "  a1", "  a2+", "  a3", "b+", "c+", "d"}},
		{"an alt click opens the whole subtree", nil, false, "a", false, true, true, "a", true, []string{"a-", "  a1", "  a2-", "    a2x", "  a3", "b+", "c+", "d"}},
		{"an alt click on the twistie of an open folder closes the subtree", []string{"a", "a2"}, false, "a", true, true, true, "", false, []string{"a+", "b+", "c+", "d"}},
		{"a click on a lazy folder loads it", nil, false, "c", false, false, true, "c", true, []string{"a+", "b+", "c~", "  c*", "d"}},
		{"a click on the placeholder is ignored", []string{"c"}, false, PlaceholderKey("c"), false, false, false, "", false, []string{"a+", "b+", "c~", "  c*", "d"}},
		{"a click on a row that is not shown is ignored", nil, false, "a1", false, false, false, "", false, []string{"a+", "b+", "c+", "d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newSampleModel(t, tc.expanded...)
			m.SetToggleOnSelect(!tc.noToggle)
			res := m.Click(tc.key, tc.twistie, tc.recursive)
			if res.Handled != tc.handled || res.Select != tc.sel {
				t.Errorf("handled %v select %v, want %v %v", res.Handled, res.Select, tc.handled, tc.sel)
			}
			if got := selectedKey(m); got != tc.wantSel {
				t.Errorf("selection %q, want %q", got, tc.wantSel)
			}
			if tc.handled && focusedKey(m) != tc.key {
				t.Errorf("focus %q, want %q", focusedKey(m), tc.key)
			}
			if got := shape(m); !slices.Equal(got, tc.rows) {
				t.Errorf("rows %q, want %q", got, tc.rows)
			}
		})
	}
	t.Run("the node of a click on a folder is the folder as it is after the toggle", func(t *testing.T) {
		m, _ := newSampleModel(t)
		if res := m.Click("a", false, false); !res.Node.Expanded {
			t.Errorf("node %+v", res.Node)
		}
	})
	doubles := []struct {
		name     string
		noToggle bool
		key      string
		rows     []string
		handled  bool
	}{
		{"a double click on a leaf opens it", false, "d", []string{"a+", "b+", "c+", "d"}, true},
		{"a double click on a folder opens it without toggling again", false, "a", []string{"a+", "b+", "c+", "d"}, true},
		{"a double click on a folder, toggling off, toggles it", true, "a", []string{"a-", "  a1", "  a2+", "  a3", "b+", "c+", "d"}, true},
		{"a double click on nothing", false, "zzz", []string{"a+", "b+", "c+", "d"}, false},
	}
	for _, tc := range doubles {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newSampleModel(t)
			m.SetToggleOnSelect(!tc.noToggle)
			res := m.DoubleClick(tc.key)
			if res.Handled != tc.handled || res.Open != tc.handled || res.Select {
				t.Errorf("result %+v", res)
			}
			if got := shape(m); !slices.Equal(got, tc.rows) {
				t.Errorf("rows %q, want %q", got, tc.rows)
			}
		})
	}
}

func TestTreeModelFocusHelpers(t *testing.T) {
	m, _ := newSampleModel(t, "a")
	if m.SetFocus("a2x") || m.SetFocus("nope") {
		t.Error("a row that is not shown cannot be focused")
	}
	m.EnsureFocus()
	if focusedKey(m) != "a" {
		t.Errorf("a tree with no focus focuses the first row, got %q", focusedKey(m))
	}
	m.ClearFocus()
	m.SetSelected("a3")
	m.EnsureFocus()
	if focusedKey(m) != "a3" {
		t.Errorf("a tree with a selected row focuses it, got %q", focusedKey(m))
	}
	m.SetFocus("d")
	m.EnsureFocus()
	if focusedKey(m) != "d" {
		t.Error("EnsureFocus keeps a focus")
	}
	if m.SetSelected("nope") {
		t.Error("an unknown node cannot be selected")
	}
	m.Expand("c")
	if m.SetFocus(PlaceholderKey("c")) {
		t.Error("the placeholder cannot be focused")
	}
	m.ClearSelection()
	m.ClearFocus()
	if _, ok := m.Selected(); ok {
		t.Error("selection cleared")
	}
	if m.FocusedIndex() != -1 {
		t.Error("focus cleared")
	}
	if got, _ := m.Data("a1"); got != nil {
		t.Errorf("data %v", got)
	}
	if d, ok := (func() (any, bool) {
		m := NewTreeModel()
		_ = m.SetItems([]TreeItem{{Key: "k", Data: 42}})
		return m.Data("k")
	})(); !ok || d != 42 {
		t.Errorf("data %v %v", d, ok)
	}
	if h := NewTreeModel(TreeModelOpts{RowHeight: foundation.Some(30.0)}).RowHeight(); h != 30 {
		t.Errorf("row height %v", h)
	}
	if h := NewTreeModel().RowHeight(); h != DefaultTreeRowHeight {
		t.Errorf("row height %v", h)
	}
}
