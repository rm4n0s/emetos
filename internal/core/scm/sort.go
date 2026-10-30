package scm

import (
	"bytes"
	"slices"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// SortKey is the order of the resources of a group in the list view (VS Code's ViewSortKey).
type SortKey string

// The sort keys of the list view.
const (
	// SortByPath orders by path, the files of a folder before the files of its subfolders (scm.defaultViewSortKey path).
	SortByPath SortKey = "path"
	// SortByName orders by file name.
	SortByName SortKey = "name"
	// SortByStatus orders by the tooltip of the decoration ("Deleted", "Modified"), then by path.
	SortByStatus SortKey = "status"
)

// ParseSortKey reads the value of scm.defaultViewSortKey and of the stored choice; anything but "name" and "status" is path.
func ParseSortKey(s string) SortKey {
	switch SortKey(s) {
	case SortByName, SortByStatus:
		return SortKey(s)
	}
	return SortByPath
}

// Ported from VS Code: src/vs/base/common/comparers.ts
//
// VS Code compares names with an Intl.Collator of the runtime's locale, numeric and with the sensitivity "base". Emetos uses the
// root collation of golang.org/x/text with the same options, whatever the locale of the user is. JavaScript compares strings
// by UTF-16 code units where Go compares bytes; the two differ only for characters outside the Basic Multilingual Plane.

// nameCollator makes the sort keys of names. It is for one goroutine; every sort and every tree makes its own.
type nameCollator struct {
	c   *collate.Collator
	buf collate.Buffer
}

func newNameCollator() *nameCollator {
	return &nameCollator{c: collate.New(language.Und, collate.Numeric, collate.IgnoreCase, collate.IgnoreDiacritics)}
}

// key is the sort key of a name, a slice of its own.
func (n *nameCollator) key(s string) []byte {
	return bytes.Clone(n.c.KeyFromString(&n.buf, s))
}

// named is a name with its sort key, made when it is first compared.
type named struct {
	name string
	key  []byte
}

// compareNames is compareFileNames: numeric, case and accent insensitive, and equal names that differ are told apart by their
// code points ("foo1" and "foo01").
func (n *nameCollator) compareNames(a, b *named) int {
	if a.key == nil {
		a.key = n.key(a.name)
	}
	if b.key == nil {
		b.key = n.key(b.name)
	}
	if c := bytes.Compare(a.key, b.key); c != 0 {
		return c
	}
	if a.name != b.name {
		return strings.Compare(a.name, b.name)
	}
	return 0
}

// compareFileNames compares two file names the way VS Code does: -1, 0 or 1. For many names use a nameCollator.
func compareFileNames(a, b string) int {
	n := newNameCollator()
	x, y := named{name: a}, named{name: b}
	return n.compareNames(&x, &y)
}

// sortItem is a resource in a sort.
type sortItem struct {
	r *Resource
	named
}

// comparePaths is comparePaths of VS Code with caseSensitive false: the components are compared in lower case one by one, the
// last ones with compareFileNames; a path that ends first goes first (a file before the files of the folders below it).
func (n *nameCollator) comparePaths(a, b *sortItem) int {
	la, lb := a.r.lower, b.r.lower
	i, j := 0, 0
	for {
		ea, eb := strings.IndexByte(la[i:], '/'), strings.IndexByte(lb[j:], '/')
		endA, endB := ea < 0, eb < 0
		switch {
		case endA && endB:
			return n.compareNames(&a.named, &b.named)
		case endA:
			return -1
		case endB:
			return 1
		}
		ca, cb := la[i:i+ea], lb[j:j+eb]
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
		i, j = i+ea+1, j+eb+1
	}
}

// sortResources orders resources by the key, as the list view of VS Code does (SCMTreeSorter): by file name; by status, which
// is the tooltip, then path; by path. The sort is stable.
func sortResources(resources []Resource, key SortKey) []*Resource {
	n := newNameCollator()
	store := make([]sortItem, len(resources))
	items := make([]*sortItem, len(resources))
	for i := range resources {
		r := &resources[i]
		store[i] = sortItem{r: r, named: named{name: r.name}}
		items[i] = &store[i]
	}
	switch key {
	case SortByName:
		slices.SortStableFunc(items, func(a, b *sortItem) int { return n.compareNames(&a.named, &b.named) })
	case SortByStatus:
		slices.SortStableFunc(items, func(a, b *sortItem) int {
			if ta, tb := a.r.Decorations.Tooltip, b.r.Decorations.Tooltip; ta != tb {
				return strings.Compare(ta, tb)
			}
			return n.comparePaths(a, b)
		})
	default:
		slices.SortStableFunc(items, func(a, b *sortItem) int { return n.comparePaths(a, b) })
	}
	out := make([]*Resource, len(items))
	for i := range items {
		out[i] = items[i].r
	}
	return out
}

// ComparePaths compares two paths as VS Code's comparePaths does (case insensitive): the files of a folder before those of the
// folders below it, and the names of one folder by compareFileNames. The repositories sorted by path use it.
func ComparePaths(a, b string) int {
	n := newNameCollator()
	x := sortItem{r: &Resource{lower: strings.ToLower(a)}, named: named{name: baseName(a)}}
	y := sortItem{r: &Resource{lower: strings.ToLower(b)}, named: named{name: baseName(b)}}
	return n.comparePaths(&x, &y)
}
