package store

import (
	"sort"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// Kind is what a Node is.
type Kind int

// The kinds of nodes.
const (
	// KindCollection is a folder of collections/ with its collection.jsonc.
	KindCollection Kind = iota + 1
	// KindFolder is a folder inside a collection, with its folder.jsonc.
	KindFolder
	// KindRequest is a .request.jsonc file.
	KindRequest
	// KindEnvironment is a file of environments/.
	KindEnvironment
)

// Node is a collection, folder, request or environment of the tree. A Node is not changed after the store handed it out.
type Node struct {
	Kind Kind
	// Path is what the operations of the store take: the folder of a collection or a folder, the file of a request or an
	// environment. It is absolute.
	Path string
	// File is the file with the document: collection.jsonc or folder.jsonc of a folder (it may not exist: HasFile), the request
	// or the environment file.
	File string
	// HasFile is false for a folder of a collection that has no descriptor file yet.
	HasFile bool
	// Key is how the node is named in the order of its parent: the file name of a request, the folder name of a folder; for a
	// collection and an environment the name of the folder and the file name without its extension.
	Key string
	// Name is the name to show: the name in the file, or the key when the file has none or cannot be read.
	Name string
	// Group is the document of a collection or folder, Request of a request, Environment of an environment. They are nil when
	// Err is set; for a file of a newer version they hold what could be read.
	Group       *format.Collection
	Request     *format.Request
	Environment *format.Environment
	// ReadOnly is true for a file of a newer version of the format (format.IsNewer); it is never written.
	ReadOnly bool
	// Err is why the file cannot be used (a syntax error with its position, a path outside the workspace, a file that cannot be
	// read), or the HttpFileNewerVersion notice when ReadOnly is set.
	Err error
	// Children are the entries of a collection or folder, in the order they are shown.
	Children []*Node
}

// Tree is the collections and environments of a workspace.
type Tree struct {
	// Collections are sorted by their folder names, Environments by their file names.
	Collections  []*Node
	Environments []*Node
}

// Find returns the node with the path (a Node.Path), or nil.
func (t *Tree) Find(path string) *Node {
	if t == nil {
		return nil
	}
	var found *Node
	t.Walk(func(n *Node) bool {
		if n.Path == path {
			found = n
			return false
		}
		return true
	})
	return found
}

// Walk calls fn for every node, a parent before its children, and stops when fn returns false.
func (t *Tree) Walk(fn func(n *Node) bool) {
	if t == nil {
		return
	}
	var walk func(ns []*Node) bool
	walk = func(ns []*Node) bool {
		for _, n := range ns {
			if !fn(n) || !walk(n.Children) {
				return false
			}
		}
		return true
	}
	if walk(t.Collections) {
		walk(t.Environments)
	}
}

// Parent returns the node whose child n is, or nil for a collection and an environment.
func (t *Tree) Parent(n *Node) *Node {
	var parent *Node
	t.Walk(func(c *Node) bool {
		for _, ch := range c.Children {
			if ch == n {
				parent = c
				return false
			}
		}
		return true
	})
	return parent
}

// sortKeys puts the keys in the order of a group: the ones its order lists, in that order (a name listed twice counts once, and one
// that is not among the keys is skipped), then the others by name.
func sortKeys(keys, order []string) []string {
	have := make(map[string]bool, len(keys))
	for _, k := range keys {
		have[k] = true
	}
	out := make([]string, 0, len(keys))
	for _, k := range order {
		if have[k] {
			out = append(out, k)
			delete(have, k)
		}
	}
	rest := make([]string, 0, len(have))
	for _, k := range keys {
		if have[k] {
			rest = append(rest, k)
			delete(have, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// stemOfKey is the stem of the key of a child: the name of a request file without its suffix, or the name of a folder.
func stemOfKey(key string) string {
	if stem, ok := format.RequestStem(key); ok {
		return stem
	}
	return key
}
