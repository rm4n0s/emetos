package scm

import (
	"slices"
	"strings"
)

// Ported from VS Code: src/vs/base/common/resourceTree.ts (ResourceTree), src/vs/workbench/contrib/scm/browser/scmViewPane.ts
// (SCMTreeSorter, SCMTreeCompressionDelegate) and src/vs/base/browser/ui/tree/compressedObjectTreeModel.ts (compress)

// Tree is the tree of folders and files of the resources of a group, for the tree view mode. It is built in one pass over the
// resources and sorts the children of a folder when they are first asked for, so a tree of 100,000 files costs what its
// expanded folders cost. A Tree belongs to one goroutine, the UI thread: it is not safe for concurrent use.
type Tree struct {
	root *Node
	coll *nameCollator
	base string
}

// Node is a folder or a file of a Tree.
type Node struct {
	name     string
	parent   *Node
	resource *Resource
	kids     []*Node
	index    map[string]*Node
	sorted   []*Node
	sortedOK bool
	named
}

// NewTree builds the tree of the resources: each is put at the folders of its path below the root folder (the root of the
// repository; "/" when it has none), and a resource outside the root at the folders of its whole path. A resource that has the
// path of another replaces it.
func NewTree(resources []Resource, root string) *Tree {
	if root == "" {
		root = "/"
	}
	t := &Tree{root: &Node{}, coll: newNameCollator(), base: root}
	for i := range resources {
		t.add(&resources[i])
	}
	return t
}

// add puts a resource in the tree.
func (t *Tree) add(r *Resource) {
	node := t.root
	rest := r.Path
	if rel, ok := relativePath(t.base, r.Path); ok {
		rest = rel
	}
	for {
		rest = strings.TrimLeft(rest, "/")
		name, after, more := strings.Cut(rest, "/")
		if name == "" && !more {
			return
		}
		child := node.index[name]
		if child == nil {
			child = &Node{name: name, parent: node}
			child.named.name = name
			if node.index == nil {
				node.index = map[string]*Node{}
			}
			node.index[name] = child
			node.kids = append(node.kids, child)
		}
		node = child
		if !more || strings.TrimLeft(after, "/") == "" {
			node.resource = r
			return
		}
		rest = after
	}
}

// Root is the root of the tree: the folder "/", which is not shown.
func (t *Tree) Root() *Node { return t.root }

// Name is the name of the node: one component of the path.
func (n *Node) Name() string { return n.name }

// Parent is the folder that holds the node; nil for the root.
func (n *Node) Parent() *Node { return n.parent }

// Path is the path of the node below the root: "src/loader.go".
func (n *Node) Path() string {
	if n.parent == nil {
		return ""
	}
	var parts []string
	for c := n; c.parent != nil; c = c.parent {
		parts = append(parts, c.name)
	}
	slices.Reverse(parts)
	return strings.Join(parts, "/")
}

// IsFolder tells whether the node has children; a node without is a file (a resource).
func (n *Node) IsFolder() bool { return len(n.kids) > 0 }

// Resource is the resource of a file node; nil for a folder.
func (n *Node) Resource() *Resource { return n.resource }

// Resources are the resources of the node and of everything below it, depth first, in the order they were put in (VS Code's
// ResourceTree.collect): what a menu command on a folder works on.
func (n *Node) Resources() []*Resource {
	var out []*Resource
	var walk func(*Node)
	walk = func(c *Node) {
		if c.resource != nil {
			out = append(out, c.resource)
		}
		for _, k := range c.kids {
			walk(k)
		}
	}
	walk(n)
	return out
}

// Entry is a row of the tree view: a file, or a folder that stands for a chain of folders when compact folders is on.
type Entry struct {
	// Node is the file or, for a chain, the last folder of it; its children are the children of the row.
	Node *Node
	// Label is the text of the row: the name, or the names of the chain joined by "/".
	Label string
	// Resource is the resource of a file row, and nil for a folder row.
	Resource *Resource
}

// IsFolder tells whether the row is a folder.
func (e Entry) IsFolder() bool { return e.Resource == nil }

// Children are the rows below a node, folders first and then files, each by compareFileNames of its name. With compact set a
// folder whose only child is a folder is joined with it and goes on (VS Code's compact folders: the first level is the head of a
// chain and the files are never joined), so "src/a/b" is one row when each has one folder in it.
func (t *Tree) Children(n *Node, compact bool) []Entry {
	if !n.sortedOK {
		kids := slices.Clone(n.kids)
		slices.SortStableFunc(kids, func(a, b *Node) int {
			if af, bf := a.IsFolder(), b.IsFolder(); af != bf {
				if af {
					return -1
				}
				return 1
			}
			return t.coll.compareNames(&a.named, &b.named)
		})
		n.sorted, n.sortedOK = kids, true
	}
	out := make([]Entry, 0, len(n.sorted))
	for _, c := range n.sorted {
		if !c.IsFolder() {
			out = append(out, Entry{Node: c, Label: c.name, Resource: c.resource})
			continue
		}
		label := c.name
		if compact {
			for len(c.kids) == 1 && c.kids[0].IsFolder() {
				c = c.kids[0]
				label += "/" + c.name
			}
		}
		out = append(out, Entry{Node: c, Label: label})
	}
	return out
}
