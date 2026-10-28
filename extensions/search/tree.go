package search

import (
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// treeState is the tree of the results and what the view remembers of it.
type treeState struct {
	model *ui.TreeModel
	ctl   *ui.TreeController
	// treeView is the mode of the results: the folders with their files, or a list of files.
	treeView bool
	// seen are the nodes the expansion policy has looked at, so that a node the user collapsed stays collapsed while the
	// results grow.
	seen map[string]bool
	// curKey is the match F4 opened last.
	curKey string
	items  []ui.TreeItem
}

func (v *view) initTree() {
	v.tree.model = ui.NewTreeModel()
	v.tree.ctl = ui.NewTreeController()
	v.tree.seen = map[string]bool{}
}

// searchStatus is how the search went, for the message above the results.
type searchStatus struct {
	running, done, cancelled, noFolder bool
	query                              extension.TextSearchQuery
	opts                               extension.TextSearchOptions
	summary                            extension.TextSearchSummary
	err                                error
	// replaced is the message after a replace: "Replaced 3 occurrences across 2 files with 'x'."
	replaced string
}

// setTreeView switches between the list of files and the tree of folders.
func (v *view) setTreeView(on bool) {
	v.tree.treeView = on
	v.tree.seen = map[string]bool{}
	v.rebuildTree()
	v.updateKeys()
	v.refreshView()
}

// rebuildTree makes the nodes of the tree from the results, keeping the expansion the user chose and giving the nodes that are new
// the expansion of search.collapseResults.
func (v *view) rebuildTree() {
	v.res.sortFiles(v.string(settingSortOrder))
	items := v.res.treeItems(v.tree.treeView)
	v.tree.items = items
	expanded := v.tree.model.ExpandedKeys()
	policy := v.string(settingCollapseResults)
	var visit func(nodes []ui.TreeItem)
	visit = func(nodes []ui.TreeItem) {
		for _, n := range nodes {
			if _, isMatch := n.Data.(matchNode); isMatch {
				continue
			}
			if !v.tree.seen[n.Key] {
				v.tree.seen[n.Key] = true
				if expandNew(policy, n) {
					expanded = append(expanded, n.Key)
				}
			}
			visit(n.Children)
		}
	}
	visit(items)
	if err := v.tree.model.SetItems(items); err != nil {
		v.api.Log().Warn("the tree of the results cannot be made", "err", err)
		return
	}
	v.tree.model.SetExpandedKeys(expanded)
}

// expandNew tells whether a node that was not there before is expanded: all of them for "alwaysExpand", the folders only for
// "alwaysCollapse", and for "auto" the files with at most ten matches.
func expandNew(policy string, n ui.TreeItem) bool {
	switch d := n.Data.(type) {
	case folderNode:
		return true
	case fileNode:
		switch policy {
		case "alwaysCollapse":
			return false
		case "auto":
			return d.matches <= 10
		}
		return true
	}
	return true
}

// collapseAll collapses or expands every node.
func (v *view) collapseAll(collapse bool) {
	if collapse {
		v.tree.model.CollapseAll()
	} else {
		v.tree.model.ExpandAll()
	}
	v.updateKeys()
	v.refreshView()
}

// anyExpanded tells whether some node of the results is expanded (so Collapse All has something to do).
func (v *view) anyExpanded() bool { return len(v.tree.model.ExpandedKeys()) > 0 }

// ---- the focused node ----

// focusedNode is the node that has the focus in the list, or the one that is selected.
func (v *view) focusedNode() (ui.TreeNode, bool) {
	m := v.tree.model
	key, ok := m.Focused()
	if !ok {
		key, ok = m.Selected()
	}
	if !ok {
		return ui.TreeNode{}, false
	}
	return m.Node(key)
}

// matchesInOrder lists the matches as the tree shows them, top to bottom.
func (v *view) matchesInOrder() []matchNode {
	var out []matchNode
	var visit func(nodes []ui.TreeItem)
	visit = func(nodes []ui.TreeItem) {
		for _, n := range nodes {
			if d, ok := n.Data.(matchNode); ok {
				out = append(out, d)
				continue
			}
			visit(n.Children)
		}
	}
	visit(v.tree.items)
	return out
}

// focusResult is F4 and Shift+F4: the next or the previous match is selected in the list, scrolled into view, and opened in a
// preview tab.
func (v *view) focusResult(delta int) {
	all := v.matchesInOrder()
	if len(all) == 0 {
		return
	}
	at := -1
	cur := v.tree.curKey
	if n, ok := v.focusedNode(); ok && v.focus.list {
		if d, ok := n.Data.(matchNode); ok {
			cur = matchKey(d.match)
		}
	}
	for i, m := range all {
		if matchKey(m.match) == cur {
			at = i
		}
	}
	var next int
	switch {
	case at < 0 && delta > 0:
		next = 0
	case at < 0:
		next = len(all) - 1
	default:
		next = (at + delta + len(all)) % len(all)
	}
	m := all[next]
	key := matchKey(m.match)
	v.tree.curKey = key
	v.tree.model.Reveal(key)
	v.tree.model.SetSelected(key)
	v.tree.model.SetFocus(key)
	v.tree.ctl.Reveal(key)
	v.openMatch(m, true)
	v.updateKeys()
	v.refreshView()
}

// openFocused opens the match, or the file, that has the focus in the list.
func (v *view) openFocused(preview bool) {
	n, ok := v.focusedNode()
	if !ok {
		return
	}
	switch d := n.Data.(type) {
	case matchNode:
		v.openMatch(d, preview)
	case fileNode:
		if len(d.file.matches) > 0 {
			v.openMatch(matchNode{file: d.file, match: d.file.matches[0]}, preview)
		}
	}
}

// openMatch shows a match in the editor: a preview tab (the tab of a click), or a tab of its own.
func (v *view) openMatch(m matchNode, preview bool) {
	line := m.match.Range.Start.Line + 1
	col := utf16Col(m.match)
	if err := v.api.Documents().Show(m.file.uri, line, col, preview); err != nil {
		v.api.Notifications().Error("Cannot open " + m.file.path + ": " + err.Error())
	}
}

// utf16Col is the column of a match in the units of Documents().Show: counted from 1, in UTF-16 units. The preview has the line
// (or the part of it around the match), so the column is exact for the lines that were not cut.
func utf16Col(m *matchItem) int {
	start := m.Range.Start.Col
	if m.PreviewStart == start || m.PreviewStart > len(m.Preview) {
		return len(utf16.Encode([]rune(m.Preview[:m.PreviewStart]))) + 1
	}
	return start + 1
}

// absPath is the absolute path of a file of the results.
func (v *view) absPath(f *fileItem) string {
	return filepath.Join(v.folder, filepath.FromSlash(f.path))
}

// ---- the keys ----

// updateKeys sets the context keys of the view to what the view is now.
func (v *view) updateKeys() {
	n, hasNode := v.focusedNode()
	var isMatch, isFile, isFolder bool
	if hasNode && v.focus.list {
		switch n.Data.(type) {
		case matchNode:
			isMatch = true
		case fileNode:
			isFile = true
		case folderNode:
			isFolder = true
		}
	}
	first := false
	if isMatch {
		if all := v.matchesInOrder(); len(all) > 0 {
			d, _ := n.Data.(matchNode)
			first = matchKey(all[0].match) == matchKey(d.match)
		}
	}
	matches, _ := v.res.counts()
	inputFocus := v.focus.search || v.focus.replace || v.focus.include || v.focus.exclude
	set := map[string]any{
		"searchViewletVisible":                    v.handle != nil,
		"searchViewletFocus":                      v.focus.view,
		"searchResultListFocused":                 v.focus.list,
		"inputBoxFocus":                           inputFocus,
		"searchInputBoxFocus":                     v.focus.search,
		"replaceInputBoxFocus":                    v.focus.replace,
		"patternIncludesInputBoxFocus":            v.focus.include,
		"patternExcludesInputBoxFocus":            v.focus.exclude,
		"replaceActive":                           v.replaceOpen,
		"hasSearchResult":                         matches > 0,
		"firstMatchFocus":                         first,
		"fileMatchOrMatchFocus":                   isMatch || isFile || isFolder,
		"fileMatchOrFolderMatchFocus":             isFile || isFolder,
		"fileMatchOrFolderMatchWithResourceFocus": isFile || isFolder,
		"fileMatchFocus":                          isFile,
		"folderMatchFocus":                        isFolder,
		"folderMatchWithResourceFocus":            isFolder,
		"matchFocus":                              isMatch,
		"viewHasSearchPattern":                    v.searchBox.Value() != "",
		"viewHasReplacePattern":                   v.replaceBox.Value() != "",
		"viewHasFilePattern":                      strings.TrimSpace(v.includeBox.Value()) != "",
		"viewHasSomeCollapsibleResult":            v.anyExpanded(),
		"inTreeView":                              v.tree.treeView,
		extKeyRunning:                             v.status.running,
	}
	for k, val := range set {
		if old, ok := v.keys[k]; ok && old == val {
			continue
		}
		v.keys[k] = val
		if err := v.api.Context().Set(k, val); err != nil {
			v.api.Log().Warn("a context key cannot be set", "key", k, "err", err)
		}
	}
}

// refreshView builds the view again, later.
func (v *view) refreshView() {
	if v.handle != nil {
		v.handle.Refresh()
	}
}
