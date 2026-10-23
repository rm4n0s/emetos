package kit

import (
	"slices"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
)

// multiModel is the sample model (see sampleItems) with several selection.
func multiModel(t *testing.T, expanded ...string) *TreeModel {
	t.Helper()
	m := NewTreeModel(TreeModelOpts{MultiSelect: true})
	m.SetExpandedKeys(expanded)
	if err := m.SetItems(sampleItems()); err != nil {
		t.Fatal(err)
	}
	m.SetViewport(0, 220)
	return m
}

func selectedKeys(m *TreeModel) []string { return m.SelectedKeys() }

func TestTreeModelMultiSelectClicks(t *testing.T) {
	cases := []struct {
		name     string
		expanded []string
		steps    []string
		want     []string
	}{
		{"shift selects the rows between", []string{"a", "a2"}, []string{"a1", "shift:b"}, []string{"a1", "a2", "a2x", "a3", "b"}},
		{"a collapsed node hides its children from the range", []string{"a"}, []string{"a1", "shift:b"}, []string{"a1", "a2", "a3", "b"}},
		{"the range goes upwards too", []string{"a"}, []string{"b", "shift:a1"}, []string{"a1", "a2", "a3", "b"}},
		{"ctrl adds nodes at different depths", []string{"a", "a2"}, []string{"a1", "ctrl:a2x", "ctrl:d"}, []string{"a1", "a2x", "d"}},
		{"ctrl+shift adds the range", []string{"a", "a2"}, []string{"a1", "ctrl:d", "ctrl+shift:b"}, []string{"a1", "b", "c", "d"}},
		{"a placeholder row cannot be in a range", []string{"c"}, []string{"b", "shift:d"}, []string{"b", "c", "d"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := multiModel(t, tc.expanded...)
			for _, st := range tc.steps {
				mods, key := ClickModifiers{}, st
				switch {
				case len(st) > 11 && st[:11] == "ctrl+shift:":
					mods, key = ClickModifiers{Ctrl: true, Shift: true}, st[11:]
				case len(st) > 5 && st[:5] == "ctrl:":
					mods, key = ClickModifiers{Ctrl: true}, st[5:]
				case len(st) > 6 && st[:6] == "shift:":
					mods, key = ClickModifiers{Shift: true}, st[6:]
				}
				res := m.ClickWith(key, mods, false, false)
				if !res.Handled {
					t.Errorf("%s not handled", st)
				}
			}
			if got := selectedKeys(m); !slices.Equal(got, tc.want) {
				t.Errorf("selected %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTreeModelModifiedClickDoesNotToggle(t *testing.T) {
	m := multiModel(t)
	m.ClickWith("a", ClickModifiers{Ctrl: true}, false, false)
	if m.IsExpanded("a") {
		t.Error("ctrl+click on a folder only selects it")
	}
	m.ClickWith("b", ClickModifiers{Shift: true}, true, false) // on the twistie
	if m.IsExpanded("b") {
		t.Error("nor does shift+click on a twistie")
	}
	res := m.ClickWith("a", ClickModifiers{}, false, false)
	if !m.IsExpanded("a") || !res.Select || !res.SelectionChanged {
		t.Errorf("a plain click toggles and selects: %+v", res)
	}
	if got := selectedKeys(m); !slices.Equal(got, []string{"a"}) {
		t.Errorf("selected %q", got)
	}
	// without MultiSelect the modifiers are ignored
	single, _ := newSampleModel(t)
	single.ClickWith("a", ClickModifiers{Ctrl: true}, false, false)
	if !single.IsExpanded("a") || single.SelectionCount() != 1 {
		t.Error("modifiers need MultiSelect")
	}
}

func TestTreeModelClickResults(t *testing.T) {
	m := multiModel(t, "a")
	cases := []struct {
		name       string
		key        string
		mods       ClickModifiers
		wantSelect bool
		wantChange bool
	}{
		{"ctrl click selects", "a1", ClickModifiers{Ctrl: true}, true, true},
		{"ctrl click again deselects", "a1", ClickModifiers{Ctrl: true}, false, true},
		{"a plain click selects", "a3", ClickModifiers{}, true, true},
		{"a plain click on the selected node changes nothing", "a3", ClickModifiers{}, true, false},
	}
	for _, tc := range cases {
		res := m.ClickWith(tc.key, tc.mods, false, false)
		if res.Select != tc.wantSelect || res.SelectionChanged != tc.wantChange || res.Node.Key != tc.key {
			t.Errorf("%s: %+v", tc.name, res)
		}
	}
}

func TestTreeModelMultiSelectKeys(t *testing.T) {
	m := multiModel(t, "a", "a2")
	m.ClickWith("a1", ClickModifiers{}, false, false)
	for _, step := range []struct {
		a     TreeAction
		want  []string
		focus string
	}{
		{TreeActionExpandSelectionDown, []string{"a1", "a2"}, "a2"},
		{TreeActionExpandSelectionDown, []string{"a1", "a2", "a2x"}, "a2x"},
		{TreeActionExpandSelectionUp, []string{"a1", "a2"}, "a2"},
		{TreeActionToggleSelection, []string{"a1"}, "a2"},
		{TreeActionToggleSelection, []string{"a1", "a2"}, "a2"},
		{TreeActionSelectAll, []string{"a", "a1", "a2", "a2x", "a3", "b", "c", "d"}, "a2"},
		{TreeActionClear, []string{"a2"}, "a2"},
		{TreeActionClear, nil, "a2"},
		{TreeActionClear, nil, ""},
	} {
		res := m.Perform(step.a)
		if !res.Handled {
			t.Errorf("action %d not handled", step.a)
		}
		if got := selectedKeys(m); !slices.Equal(got, step.want) && (len(got) != 0 || len(step.want) != 0) {
			t.Errorf("action %d: selected %q, want %q", step.a, got, step.want)
		}
		if got := focusedKey(m); got != step.focus {
			t.Errorf("action %d: focus %q, want %q", step.a, got, step.focus)
		}
	}
	if res := m.Perform(TreeActionClear); res.Handled {
		t.Error("nothing to clear")
	}
}

func TestTreeModelMultiKeysNeedMultiSelect(t *testing.T) {
	m, _ := newSampleModel(t, "a")
	m.ClickWith("a1", ClickModifiers{}, false, false)
	for _, a := range []TreeAction{TreeActionExpandSelectionDown, TreeActionExpandSelectionUp, TreeActionSelectAll, TreeActionToggleSelection} {
		if m.Perform(a).Handled {
			t.Errorf("action %d needs MultiSelect", a)
		}
	}
}

func TestTreeModelSelectionSurvivesRebuilds(t *testing.T) {
	m := multiModel(t, "a", "a2")
	m.ClickWith("a1", ClickModifiers{}, false, false)
	m.ClickWith("a2x", ClickModifiers{Ctrl: true}, false, false)
	m.ClickWith("d", ClickModifiers{Ctrl: true}, false, false)
	// collapse the folder above two selected nodes: they stay selected, hidden
	m.Collapse("a")
	if got := selectedKeys(m); !slices.Equal(got, []string{"a1", "a2x", "d"}) && !slices.Equal(got, []string{"a2x", "a1", "d"}) {
		t.Errorf("selected %q", got)
	}
	if got := m.SelectionCount(); got != 3 {
		t.Errorf("count %d", got)
	}
	m.Expand("a")
	// the same items again, one of them gone
	items := sampleItems()
	items[0].Children = items[0].Children[1:] // a1 is gone
	if err := m.SetItems(items); err != nil {
		t.Fatal(err)
	}
	if got := selectedKeys(m); !slices.Equal(got, []string{"a2x", "d"}) {
		t.Errorf("after SetItems: %q", got)
	}
	// children replaced
	if err := m.SetChildren("a", []TreeItem{{Key: "n1"}, {Key: "a2", Children: []TreeItem{{Key: "a2x"}}}}); err != nil {
		t.Fatal(err)
	}
	if got := selectedKeys(m); !slices.Equal(got, []string{"a2x", "d"}) {
		t.Errorf("after SetChildren: %q", got)
	}
	if _, ok := m.Selected(); !ok {
		t.Error("a primary node is left")
	}
	// the anchor of a node that is gone does not start a range
	m.ClickWith("a2x", ClickModifiers{}, false, false)
	if err := m.SetItems([]TreeItem{{Key: "z1"}, {Key: "z2"}, {Key: "z3"}}); err != nil {
		t.Fatal(err)
	}
	m.SetFocus("z2")
	m.ClickWith("z3", ClickModifiers{Shift: true}, false, false)
	if got := selectedKeys(m); !slices.Equal(got, []string{"z2", "z3"}) {
		t.Errorf("a range starts at the focus when the anchor is gone: %q", got)
	}
}

func TestTreeModelSelectAllSkipsPlaceholders(t *testing.T) {
	m := multiModel(t, "c")
	m.Perform(TreeActionSelectAll)
	if got := selectedKeys(m); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("selected %q", got)
	}
}

func TestTreeModelSetSelectionAndMultiSelect(t *testing.T) {
	m := multiModel(t, "a")
	m.SetSelection([]string{"a1", "nope", "d", "a3"})
	if got := selectedKeys(m); !slices.Equal(got, []string{"a1", "a3", "d"}) {
		t.Errorf("selected %q", got)
	}
	if k, _ := m.Selected(); k != "a3" {
		t.Errorf("the primary node is the last given: %q", k)
	}
	m.SetMultiSelect(false)
	if got := selectedKeys(m); !slices.Equal(got, []string{"a3"}) {
		t.Errorf("turning multi off: %q", got)
	}
	if !m.IsSelected("a3") || m.IsSelected("a1") {
		t.Error("IsSelected")
	}
	if m.SetSelected("nope") {
		t.Error("an unknown node")
	}
}

// findItems is a project: folders with files, to filter.
func findItems() []TreeItem {
	return []TreeItem{
		{Key: "src", Label: "src", HasChildren: true, Children: []TreeItem{
			{Key: "src/main.go", Label: "main.go"},
			{Key: "src/util", Label: "util", Children: []TreeItem{
				{Key: "src/util/strings.go", Label: "strings.go"},
				{Key: "src/util/maps.go", Label: "maps.go"},
				{Key: "src/util/notes.txt", Label: "notes.txt"},
			}},
		}},
		{Key: "docs", Label: "docs", Children: []TreeItem{{Key: "docs/README.md", Data: "README.md"}}},
		{Key: "LICENSE", Data: "LICENSE"},
		{Key: "vendor", Label: "vendor", Lazy: true},
	}
}

func findModel(t *testing.T, mode FindMode, expanded ...string) *TreeModel {
	t.Helper()
	m := NewTreeModel(TreeModelOpts{FindMode: mode, MultiSelect: true})
	m.SetExpandedKeys(expanded)
	if err := m.SetItems(findItems()); err != nil {
		t.Fatal(err)
	}
	m.SetViewport(0, 220)
	return m
}

func TestTreeModelFindFilterKeepsAncestorsOfMatches(t *testing.T) {
	m := findModel(t, FindModeFilter)
	if got := shape(m); !slices.Equal(got, []string{"src+", "docs+", "LICENSE", "vendor+"}) {
		t.Fatalf("before the find: %q", got)
	}
	m.SetFindPattern("go")
	want := []string{"src-", "  src/main.go", "  src/util-", "    src/util/strings.go", "    src/util/maps.go"}
	if got := shape(m); !slices.Equal(got, want) {
		t.Errorf("filtered: %q, want %q", got, want)
	}
	if m.IsExpanded("src") || m.IsExpanded("src/util") {
		t.Error("the filter expands the nodes without changing the expansion state")
	}
	if st := m.Find(); st.MatchCount != 3 || st.Total != 10 {
		t.Errorf("find %+v", st)
	}
	if _, ok := m.FindMatch("src/util"); ok {
		t.Error("util does not match itself, it is only kept")
	}
	if r, ok := m.FindMatch("src/util/maps.go"); !ok || !slices.Equal(r, []TextRange{{5, 7}}) {
		t.Errorf("maps.go %v %v", r, ok)
	}
	// the focus went to the first match
	if got := focusedKey(m); got != "src/main.go" {
		t.Errorf("focus %q", got)
	}
	// up and down move between the matches, skipping the folders that are only kept
	m.Perform(TreeActionDown)
	if got := focusedKey(m); got != "src/util/strings.go" {
		t.Errorf("down: %q", got)
	}
	// a match that is a folder keeps its own matching children only
	m.SetFindPattern("util")
	if got := shape(m); !slices.Equal(got, []string{"src-", "  src/util+"}) {
		t.Errorf("a folder that matches: %q", got)
	}
	// closing restores the tree as it was
	m.CloseFind()
	if got := shape(m); !slices.Equal(got, []string{"src+", "docs+", "LICENSE", "vendor+"}) {
		t.Errorf("after the find: %q", got)
	}
}

func TestTreeModelFindFilterWithExpandedNodes(t *testing.T) {
	m := findModel(t, FindModeFilter, "src", "docs")
	m.SetFindPattern("README")
	if got := shape(m); !slices.Equal(got, []string{"docs-", "  docs/README.md"}) {
		t.Errorf("filtered: %q", got)
	}
	m.CloseFind()
	if got := shape(m); !slices.Equal(got, []string{"src-", "  src/main.go", "  src/util+", "docs-", "  docs/README.md", "LICENSE", "vendor+"}) {
		t.Errorf("after the find: %q", got)
	}
}

func TestTreeModelFindHighlightKeepsAllRows(t *testing.T) {
	m := findModel(t, FindModeHighlight, "src", "src/util")
	all := shape(m)
	m.SetFindPattern("go")
	if got := shape(m); !slices.Equal(got, all) {
		t.Errorf("rows changed: %q", got)
	}
	if st := m.Find(); st.MatchCount != 3 || st.Total != 9 {
		t.Errorf("find %+v", st) // the rows that are shown are matched: the collapsed folders' children are not
	}
	if got := focusedKey(m); got != "src/main.go" {
		t.Errorf("focus %q", got)
	}
	m.Perform(TreeActionDown)
	m.Perform(TreeActionDown)
	if got := focusedKey(m); got != "src/util/maps.go" {
		t.Errorf("down: %q", got)
	}
	m.Perform(TreeActionDown)
	if got := focusedKey(m); got != "src/util/maps.go" {
		t.Errorf("the last match: %q", got)
	}
	m.Perform(TreeActionFirst)
	if got := focusedKey(m); got != "src/main.go" {
		t.Errorf("home goes to the first match: %q", got)
	}
}

func TestTreeModelFindNoResults(t *testing.T) {
	for _, mode := range []FindMode{FindModeHighlight, FindModeFilter} {
		m := findModel(t, mode, "src")
		m.SetFindPattern("zzzz")
		st := m.Find()
		if !st.NoResults() || st.MatchCount != 0 {
			t.Errorf("mode %d: %+v", mode, st)
		}
		if mode == FindModeFilter && m.Len() != 0 {
			t.Errorf("a filter with no result shows no row: %d", m.Len())
		}
		if mode == FindModeHighlight && m.Len() == 0 {
			t.Error("a highlight keeps the rows")
		}
	}
}

func TestTreeModelFindMatchTypeAndLabels(t *testing.T) {
	m := findModel(t, FindModeFilter, "src")
	m.SetFindPattern("mgo")
	if got := shape(m); !slices.Equal(got, []string{"src-", "  src/main.go", "  src/util-", "    src/util/maps.go"}) {
		t.Errorf("fuzzy: %q", got)
	}
	m.SetFindMatchType(FindMatchContiguous)
	if m.Len() != 0 {
		t.Errorf("contiguous: %q", shape(m))
	}
	// a label that comes from the data
	m.SetFindMatchType(FindMatchFuzzy)
	m.SetFindPattern("licen")
	if got := shape(m); !slices.Equal(got, []string{"LICENSE"}) {
		t.Errorf("label from the data: %q", got)
	}
	n, _ := m.Node("LICENSE")
	if n.Label != "LICENSE" {
		t.Errorf("node label %q", n.Label)
	}
}

func TestTreeModelFindDoesNotLoadLazyNodes(t *testing.T) {
	loads := 0
	m := NewTreeModel(TreeModelOpts{FindMode: FindModeFilter, LoadChildren: func(string, any) { loads++ }})
	if err := m.SetItems(findItems()); err != nil {
		t.Fatal(err)
	}
	m.SetFindPattern("vend")
	if got := shape(m); !slices.Equal(got, []string{"vendor+"}) {
		t.Errorf("rows %q", got)
	}
	if loads != 0 {
		t.Errorf("a find reads nothing: %d loads", loads)
	}
}

func TestTreeModelSelectionStaysWhileFiltering(t *testing.T) {
	m := findModel(t, FindModeFilter, "src")
	m.ClickWith("LICENSE", ClickModifiers{}, false, false)
	m.SetFindPattern("go")
	if got := selectedKeys(m); !slices.Equal(got, []string{"LICENSE"}) {
		t.Errorf("a selected node that is filtered out stays selected: %q", got)
	}
	m.ClickWith("src/main.go", ClickModifiers{Ctrl: true}, false, false)
	if got := selectedKeys(m); !slices.Equal(got, []string{"LICENSE", "src/main.go"}) {
		t.Errorf("selected %q", got)
	}
	m.CloseFind()
	if got := selectedKeys(m); !slices.Equal(got, []string{"src/main.go", "LICENSE"}) {
		t.Errorf("after the find, in the order of the rows: %q", got)
	}
}

func TestTreeModelEscapeWithoutSelectionNavigation(t *testing.T) {
	m := NewTreeModel(TreeModelOpts{MultiSelect: true, SelectionNavigation: foundation.Some(false)})
	if err := m.SetItems(sampleItems()); err != nil {
		t.Fatal(err)
	}
	m.ClickWith("a", ClickModifiers{}, false, false)
	m.ClickWith("d", ClickModifiers{Ctrl: true}, false, false)
	m.Perform(TreeActionClear)
	if m.SelectionCount() != 0 {
		t.Errorf("selected %q", selectedKeys(m))
	}
}
