package references

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The model of a search: the locations by file, in order (VS Code's ReferencesModel, FileItem and ReferenceItem).
//
// Ported from VS Code: extensions/references-view/src/references/model.ts

// fileItem is a file with the locations in it, the first first.
type fileItem struct {
	uri   string
	refs  []*refItem
	model *model
}

// refItem is one location.
type refItem struct {
	loc  extension.Location
	file *fileItem
}

// item is a file or a location: *fileItem or *refItem.
type item interface{ key() string }

// key is the key of the node of the tree.
func (f *fileItem) key() string { return "f:" + f.uri }

// key is the key of the node of the tree.
func (r *refItem) key() string {
	l := r.loc.Range
	return "r:" + r.loc.URI + "@" + strconv.Itoa(l.Start.Line) + ":" + strconv.Itoa(l.Start.Col) + "-" + strconv.Itoa(l.End.Line) + ":" + strconv.Itoa(l.End.Col)
}

// model holds the files of a search that have locations.
type model struct {
	files []*fileItem
}

// newModel sorts the locations by file and position, and groups them by file.
func newModel(locs []extension.Location) *model {
	sorted := slices.Clone(locs)
	slices.SortStableFunc(sorted, func(a, b extension.Location) int {
		if c := strings.Compare(a.URI, b.URI); c != 0 {
			return c
		}
		return cmp.Or(cmp.Compare(a.Range.Start.Line, b.Range.Start.Line), cmp.Compare(a.Range.Start.Col, b.Range.Start.Col))
	})
	m := &model{}
	var last *fileItem
	for _, l := range sorted {
		if last == nil || last.uri != l.URI {
			last = &fileItem{uri: l.URI, model: m}
			m.files = append(m.files, last)
		}
		last.refs = append(last.refs, &refItem{loc: l, file: last})
	}
	return m
}

// total is the number of locations.
func (m *model) total() int {
	n := 0
	for _, f := range m.files {
		n += len(f.refs)
	}
	return n
}

// message is the line over the tree: "17 results in 6 files".
func (m *model) message() string {
	if len(m.files) == 0 {
		return "No results."
	}
	total, files := m.total(), len(m.files)
	switch {
	case total == 1 && files == 1:
		return "1 result in 1 file"
	case total == 1:
		return "1 result in " + strconv.Itoa(files) + " files"
	case files == 1:
		return strconv.Itoa(total) + " results in 1 file"
	}
	return strconv.Itoa(total) + " results in " + strconv.Itoa(files) + " files"
}

// location is the place an item stands for: the range of a location, the first location of a file.
func (m *model) location(it item) extension.Location {
	switch v := it.(type) {
	case *refItem:
		return v.loc
	case *fileItem:
		if len(v.refs) > 0 {
			return extension.Location{URI: v.uri, Range: v.refs[0].loc.Range}
		}
		return extension.Location{URI: v.uri}
	}
	return extension.Location{}
}

// nearest is the item to select for a request position: the location that holds the position, else the first one after it
// (or the last one before it) in the same file, else the first location of the file whose URI shares the longest prefix with
// the document's.
func (m *model) nearest(uri string, pos extension.Position) item {
	if len(m.files) == 0 {
		return nil
	}
	for _, f := range m.files {
		if f.uri != uri {
			continue
		}
		for _, r := range f.refs {
			if rangeContains(r.loc.Range, pos) {
				return r
			}
		}
		var lastBefore *refItem
		for _, r := range f.refs {
			if after(r.loc.Range.End, pos) {
				return r
			}
			lastBefore = r
		}
		if lastBefore != nil {
			return lastBefore
		}
		break
	}
	best, bestLen := 0, prefixLen(m.files[0].uri, uri)
	for i := 1; i < len(m.files); i++ {
		if n := prefixLen(m.files[i].uri, uri); n > bestLen {
			best, bestLen = i, n
		}
	}
	if len(m.files[best].refs) == 0 {
		return m.files[best]
	}
	return m.files[best].refs[0]
}

// rangeContains tells whether the position is in the range, its ends included.
func rangeContains(r extension.Range, p extension.Position) bool {
	return !before(p, r.Start) && !after(p, r.End)
}

// before and after compare two positions.
func before(a, b extension.Position) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Col < b.Col)
}

func after(a, b extension.Position) bool { return before(b, a) }

// prefixLen is how many bytes two strings start with in common.
func prefixLen(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// next is the item after it, wrapping round from the last to the first; the same item when there is nothing else.
func (m *model) next(it item) item { return m.move(it, 1) }

// previous is the item before it, wrapping round.
func (m *model) previous(it item) item { return m.move(it, -1) }

// move is next and previous: from a file to the first (or last) location of the next (or previous) file, from a location to
// the next (or previous) one of its file and over the edge to the next (or previous) file.
func (m *model) move(it item, delta int) item {
	if len(m.files) == 0 {
		return it
	}
	shift := func(f *fileItem) *fileItem {
		i := slices.Index(m.files, f)
		return m.files[((i+delta)%len(m.files)+len(m.files))%len(m.files)]
	}
	edge := func(f *fileItem) item {
		if len(f.refs) == 0 {
			return f
		}
		if delta > 0 {
			return f.refs[0]
		}
		return f.refs[len(f.refs)-1]
	}
	switch v := it.(type) {
	case *fileItem:
		return edge(shift(v))
	case *refItem:
		i := slices.Index(v.file.refs, v) + delta
		switch {
		case i < 0:
			return lastOf(shift(v.file))
		case i >= len(v.file.refs):
			return firstOf(shift(v.file))
		}
		return v.file.refs[i]
	}
	return it
}

// firstOf and lastOf are the first and the last location of a file (the file itself when it has none).
func firstOf(f *fileItem) item {
	if len(f.refs) == 0 {
		return f
	}
	return f.refs[0]
}

func lastOf(f *fileItem) item {
	if len(f.refs) == 0 {
		return f
	}
	return f.refs[len(f.refs)-1]
}

// remove takes an item out of the model: a file with its locations, or a location (and its file when it was the last one in
// it).
func (m *model) remove(it item) {
	switch v := it.(type) {
	case *fileItem:
		m.files = slices.DeleteFunc(m.files, func(f *fileItem) bool { return f == v })
	case *refItem:
		v.file.refs = slices.DeleteFunc(v.file.refs, func(r *refItem) bool { return r == v })
		if len(v.file.refs) == 0 {
			m.files = slices.DeleteFunc(m.files, func(f *fileItem) bool { return f == v.file })
		}
	}
}

// find returns the item with the key of a node.
func (m *model) find(key string) item {
	for _, f := range m.files {
		if f.key() == key {
			return f
		}
		for _, r := range f.refs {
			if r.key() == key {
				return r
			}
		}
	}
	return nil
}
