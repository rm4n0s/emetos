package search

import (
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// matchItem is a match of the results. Its id is the key of its node and does not change when other matches are dismissed.
type matchItem struct {
	id int
	extension.TextSearchMatch
}

// fileItem is a file with matches.
type fileItem struct {
	path, uri string
	matches   []*matchItem
}

// Name is the base name of the file and Dir the folder it is in, relative to the folder of the workspace.
func (f *fileItem) Name() string { return path.Base(f.path) }

// Dir is the folder of the file, "" for the folder of the workspace.
func (f *fileItem) Dir() string {
	if d := path.Dir(f.path); d != "." {
		return d
	}
	return ""
}

// results are the matches of a search, by file.
type results struct {
	files  []*fileItem
	byPath map[string]*fileItem
	nextID int
}

func newResults() *results { return &results{byPath: map[string]*fileItem{}} }

// add puts the matches of a file in the results; a file that is there already gets the matches too.
func (r *results) add(f extension.TextSearchFile) {
	fi := r.byPath[f.Path]
	if fi == nil {
		fi = &fileItem{path: f.Path, uri: f.URI}
		r.byPath[f.Path] = fi
		r.files = append(r.files, fi)
	}
	for _, m := range f.Matches {
		r.nextID++
		fi.matches = append(fi.matches, &matchItem{id: r.nextID, TextSearchMatch: m})
	}
}

// counts are the number of matches and of files.
func (r *results) counts() (matches, files int) {
	for _, f := range r.files {
		matches += len(f.matches)
	}
	return matches, len(r.files)
}

// empty tells whether there is no match.
func (r *results) empty() bool { return len(r.files) == 0 }

// removeMatch dismisses a match; a file left with none goes too.
func (r *results) removeMatch(f *fileItem, m *matchItem) {
	f.matches = slices.DeleteFunc(f.matches, func(x *matchItem) bool { return x == m })
	if len(f.matches) == 0 {
		r.removeFile(f)
	}
}

// removeFile dismisses a file with its matches.
func (r *results) removeFile(f *fileItem) {
	r.files = slices.DeleteFunc(r.files, func(x *fileItem) bool { return x == f })
	delete(r.byPath, f.path)
}

// removeFolder dismisses the files below a folder (a path relative to the folder of the workspace).
func (r *results) removeFolder(dir string) {
	for _, f := range slices.Clone(r.files) {
		if f.path == dir || strings.HasPrefix(f.path, dir+"/") {
			r.removeFile(f)
		}
	}
}

// Sort orders of search.sortOrder.
const (
	orderDefault         = "default"
	orderFileNames       = "fileNames"
	orderType            = "type"
	orderModified        = "modified"
	orderCountDescending = "countDescending"
	orderCountAscending  = "countAscending"
)

// sortFiles orders the files as search.sortOrder says. "modified" needs the times of the files, which the search does not give,
// so it is the default order.
func (r *results) sortFiles(order string) {
	byPath := func(a, b *fileItem) int { return comparePaths(a.path, b.path) }
	switch order {
	case orderFileNames:
		sort.SliceStable(r.files, func(i, j int) bool {
			a, b := r.files[i], r.files[j]
			if c := strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name())); c != 0 {
				return c < 0
			}
			return byPath(a, b) < 0
		})
	case orderType:
		sort.SliceStable(r.files, func(i, j int) bool {
			a, b := r.files[i], r.files[j]
			if c := strings.Compare(path.Ext(a.path), path.Ext(b.path)); c != 0 {
				return c < 0
			}
			return byPath(a, b) < 0
		})
	case orderCountDescending:
		sort.SliceStable(r.files, func(i, j int) bool {
			a, b := r.files[i], r.files[j]
			if len(a.matches) != len(b.matches) {
				return len(a.matches) > len(b.matches)
			}
			return byPath(a, b) < 0
		})
	case orderCountAscending:
		sort.SliceStable(r.files, func(i, j int) bool {
			a, b := r.files[i], r.files[j]
			if len(a.matches) != len(b.matches) {
				return len(a.matches) < len(b.matches)
			}
			return byPath(a, b) < 0
		})
	default:
		sort.SliceStable(r.files, func(i, j int) bool { return byPath(r.files[i], r.files[j]) < 0 })
	}
}

// comparePaths orders paths folder by folder, the files of a folder before its sub folders as the Explorer does not (VS Code's
// search orders by path: compareFileNames on the whole path), by lower case first.
func comparePaths(a, b string) int {
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// The data of the nodes of the tree.
type (
	// folderNode is a folder of the tree view; path is the whole path of the chain of folders it shows.
	folderNode struct {
		path  string
		label string
		files int
		count int
	}
	// fileNode is a file; the tree view shows only its name.
	fileNode struct {
		file    *fileItem
		inTree  bool
		matches int
	}
	// matchNode is a match.
	matchNode struct {
		file  *fileItem
		match *matchItem
	}
)

func fileKey(f *fileItem) string   { return "f:" + f.path }
func folderKey(p string) string    { return "d:" + p }
func matchKey(m *matchItem) string { return "m:" + strconv.Itoa(m.id) }

// treeItems makes the nodes of the tree: a list of files, or the folders with their files.
func (r *results) treeItems(treeView bool) []ui.TreeItem {
	if !treeView {
		out := make([]ui.TreeItem, 0, len(r.files))
		for _, f := range r.files {
			out = append(out, fileItemNode(f, false))
		}
		return out
	}
	root := &dirNode{children: map[string]*dirNode{}}
	for _, f := range r.files {
		n := root
		if dir := f.Dir(); dir != "" {
			for _, seg := range strings.Split(dir, "/") {
				c := n.children[seg]
				if c == nil {
					c = &dirNode{name: seg, children: map[string]*dirNode{}}
					n.children[seg] = c
					n.order = append(n.order, c)
				}
				n = c
			}
		}
		n.files = append(n.files, f)
	}
	return root.items("")
}

// fileItemNode is the node of a file with its matches below it.
func fileItemNode(f *fileItem, inTree bool) ui.TreeItem {
	node := ui.TreeItem{Key: fileKey(f), Label: f.Name(), Data: fileNode{file: f, inTree: inTree, matches: len(f.matches)}}
	node.Children = make([]ui.TreeItem, len(f.matches))
	for i, m := range f.matches {
		node.Children[i] = ui.TreeItem{Key: matchKey(m), Label: strings.TrimSpace(m.Preview), Data: matchNode{file: f, match: m}}
	}
	return node
}

// dirNode is a folder while the tree is made.
type dirNode struct {
	name     string
	children map[string]*dirNode
	order    []*dirNode // in the order the files came, which is sorted
	files    []*fileItem
}

// count is the number of files and matches below the folder.
func (d *dirNode) count() (files, matches int) {
	for _, f := range d.files {
		files++
		matches += len(f.matches)
	}
	for _, c := range d.order {
		cf, cm := c.count()
		files, matches = files+cf, matches+cm
	}
	return files, matches
}

// items makes the nodes below a folder; a folder that holds only one folder is shown with it, as one node ("src/lib"), as VS
// Code's compact folders do.
func (d *dirNode) items(prefix string) []ui.TreeItem {
	var out []ui.TreeItem
	for _, c := range d.order {
		chain, label := c, c.name
		for len(chain.files) == 0 && len(chain.order) == 1 {
			chain = chain.order[0]
			label += "/" + chain.name
		}
		full := label
		if prefix != "" {
			full = prefix + "/" + label
		}
		files, matches := chain.count()
		node := ui.TreeItem{Key: folderKey(full), Label: label, Data: folderNode{path: full, label: label, files: files, count: matches}}
		node.Children = chain.items(full)
		out = append(out, node)
	}
	for _, f := range d.files {
		out = append(out, fileItemNode(f, true))
	}
	return out
}
