// Package keytree is the tree the Redis explorer shows its keys in (SPEC §10.23 "The explorer"): the keys of a database split at
// a separator, so that user:42:name sits under "user" and "42". It has no UI and no client; the explorer feeds it the keys that SCAN
// returned, page after page, and asks it for the entries below a folder, one level at a time, so a tree of very many keys is
// never turned into rows whole.
package keytree

import (
	"slices"
	"strings"
)

// Leaf is a key and what the explorer knows about it.
type Leaf struct {
	// Key is the whole key.
	Key string
	// Type is the type of the key as TYPE says it ("string", "hash", "ReJSON-RL"); empty when it is not known.
	Type string
	// TTLMillis is the time to live in milliseconds, -1 for a key with none and -2 when it is not known.
	TTLMillis int64
}

// Entry is a row below a folder: a folder, or a key.
type Entry struct {
	// Name is what the row shows: the segment of a folder, the last segment of a key (a key that ends with the separator has an
	// empty name).
	Name string
	// Path is the prefix of a folder: its segments each followed by the separator ("user:42:"), which every key below it starts with
	// and which Children takes to list what is below it. For a key it is the key.
	Path string
	// Folder tells a folder from a key.
	Folder bool
	// Count is the number of keys below a folder, at any depth; 1 for a key.
	Count int
	// Leaf is the key of a row that is not a folder.
	Leaf Leaf
}

// node is a folder: the folders and the keys directly below it.
type node struct {
	folders map[string]*node
	leaves  map[string]Leaf
	count   int
}

func newNode() *node { return &node{} }

// Tree is the keys of a database split at a separator. It is not safe for use from several goroutines.
type Tree struct {
	sep  string
	root *node
}

// New makes an empty tree that splits keys at sep. An empty separator makes a flat tree: every key is a row of the top level.
func New(sep string) *Tree { return &Tree{sep: sep, root: newNode()} }

// Separator is the separator the tree splits at.
func (t *Tree) Separator() string { return t.sep }

// Len is the number of keys in the tree.
func (t *Tree) Len() int { return t.root.count }

// split cuts a key into the segments of its folders and the name of the key.
func (t *Tree) split(key string) (folders []string, name string) {
	if t.sep == "" {
		return nil, key
	}
	parts := strings.Split(key, t.sep)
	return parts[:len(parts)-1], parts[len(parts)-1]
}

// Add puts the keys in the tree; a key that is there already is replaced (its type and TTL are new). It returns how many keys were
// new.
func (t *Tree) Add(leaves ...Leaf) (added int) {
	for _, l := range leaves {
		folders, name := t.split(l.Key)
		path := make([]*node, 0, len(folders)+1)
		n := t.root
		path = append(path, n)
		for _, f := range folders {
			if n.folders == nil {
				n.folders = map[string]*node{}
			}
			child := n.folders[f]
			if child == nil {
				child = newNode()
				n.folders[f] = child
			}
			n = child
			path = append(path, n)
		}
		if n.leaves == nil {
			n.leaves = map[string]Leaf{}
		}
		if _, there := n.leaves[name]; !there {
			added++
			for _, p := range path {
				p.count++
			}
		}
		n.leaves[name] = l
	}
	return added
}

// Remove takes a key out of the tree, and the folders that are left empty. It tells whether the key was there.
func (t *Tree) Remove(key string) bool {
	folders, name := t.split(key)
	path := []*node{t.root}
	n := t.root
	for _, f := range folders {
		child := n.folders[f]
		if child == nil {
			return false
		}
		n = child
		path = append(path, n)
	}
	if _, ok := n.leaves[name]; !ok {
		return false
	}
	delete(n.leaves, name)
	for _, p := range path {
		p.count--
	}
	for i := len(folders) - 1; i >= 0; i-- {
		parent, child := path[i], path[i+1]
		if child.count > 0 {
			break
		}
		delete(parent.folders, folders[i])
	}
	return true
}

// Get returns the key's leaf.
func (t *Tree) Get(key string) (Leaf, bool) {
	folders, name := t.split(key)
	n := t.root
	for _, f := range folders {
		if n = n.folders[f]; n == nil {
			return Leaf{}, false
		}
	}
	l, ok := n.leaves[name]
	return l, ok
}

// lookup finds the folder with the path ("" is the top). A path is the prefix of a folder, with the separator after its last segment,
// so that a folder with an empty name (the keys ":x") has the path ":" and is not the top.
func (t *Tree) lookup(path string) *node {
	if path == "" || t.sep == "" {
		return t.root
	}
	n := t.root
	for _, seg := range strings.Split(strings.TrimSuffix(path, t.sep), t.sep) {
		if n = n.folders[seg]; n == nil {
			return nil
		}
	}
	return n
}

// Children lists what is directly below the folder with the path (Entry.Path of a folder; "" is the top): the folders first, then the keys, each in natural
// order (key2 before key10). A path that is not a folder of the tree has no children.
func (t *Tree) Children(path string) []Entry {
	n := t.lookup(path)
	if n == nil {
		return nil
	}
	out := make([]Entry, 0, len(n.folders)+len(n.leaves))
	for name, f := range n.folders {
		out = append(out, Entry{Name: name, Path: path + name + t.sep, Folder: true, Count: f.count})
	}
	slices.SortFunc(out, func(a, b Entry) int { return NaturalCompare(a.Name, b.Name) })
	folders := len(out)
	for name, l := range n.leaves {
		out = append(out, Entry{Name: name, Path: l.Key, Count: 1, Leaf: l})
	}
	slices.SortFunc(out[folders:], func(a, b Entry) int {
		if c := NaturalCompare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

// CountBelow is the number of keys below the folder with the path (0 for a path that is not a folder).
func (t *Tree) CountBelow(path string) int {
	if n := t.lookup(path); n != nil {
		return n.count
	}
	return 0
}

// Keys lists every key in the tree in no particular order, for the code that needs all of them.
func (t *Tree) Keys() []string {
	out := make([]string, 0, t.root.count)
	var walk func(n *node)
	walk = func(n *node) {
		for _, l := range n.leaves {
			out = append(out, l.Key)
		}
		for _, f := range n.folders {
			walk(f)
		}
	}
	walk(t.root)
	return out
}

// NaturalCompare orders two names the way a person reads them: runs of digits as numbers (item2 before item10), the rest byte by
// byte. Names that are equal as numbers but written differently (007 and 7) are ordered by their text.
func NaturalCompare(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			si := i
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			sj := j
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na, nb := strings.TrimLeft(a[si:i], "0"), strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			if c := strings.Compare(a[si:i], b[sj:j]); c != 0 {
				return c
			}
			continue
		}
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch {
	case i < len(a):
		return 1
	case j < len(b):
		return -1
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
