package testing

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"
)

// Count is how many of the things of a kind were covered (VS Code's ICoverageCount).
type Count struct{ Covered, Total int }

// Percent is the share that was covered, from 0 to 1; a count with no things is fully covered.
func (c Count) Percent() float64 {
	if c.Total == 0 {
		return 1
	}
	return float64(c.Covered) / float64(c.Total)
}

// FileCoverage is the coverage of a file, or the sum of the files below a folder.
type FileCoverage struct {
	// ID identifies the file in its coverage: the model gives one when the controller gave none.
	ID string
	// URI is file:///path of the file or folder.
	URI string
	// Statement is the statement coverage; Branch and Declaration are nil when not measured.
	Statement   Count
	Branch      *Count
	Declaration *Count
	// Data is the value of the controller's own, passed back to the loader of the details.
	Data any
}

// TotalPercent is the coverage of the file as one number: the statements, the branches and the declarations counted together (the
// formula of Clover, VS Code's getTotalCoveragePercent).
func (f FileCoverage) TotalPercent() float64 {
	return TotalPercent(f.Statement, f.Branch, f.Declaration)
}

// TotalPercent is the total coverage of the three counts, from 0 to 1; with nothing to cover it is 1.
func TotalPercent(statement Count, branch, declaration *Count) float64 {
	num, den := statement.Covered, statement.Total
	if branch != nil {
		num += branch.Covered
		den += branch.Total
	}
	if declaration != nil {
		num += declaration.Covered
		den += declaration.Total
	}
	if den == 0 {
		return 1
	}
	return float64(num) / float64(den)
}

// BranchDetail is a branch of a statement and how many times it ran.
type BranchDetail struct {
	Executed int
	Location *Range
	Label    string
}

// StatementDetail is a statement and how many times it ran.
type StatementDetail struct {
	Executed int
	Location Range
	Branches []BranchDetail
}

// DeclarationDetail is a function or another declaration and how many times it ran.
type DeclarationDetail struct {
	Name     string
	Executed int
	Location Range
}

// Details are the lines of a file's coverage.
type Details struct {
	Statements   []StatementDetail
	Declarations []DeclarationDetail
}

// CoverageLoader loads the details of the coverage of a file.
type CoverageLoader func(ctx context.Context, file FileCoverage) (Details, error)

// TotalsFromDetails counts the details: the statements that ran, their branches that ran and the declarations that ran (VS Code's
// totalFromCoverageDetails; VS Code adds the statements that ran to the total, a slip that this does not copy).
func TotalsFromDetails(uri string, d Details) FileCoverage {
	fc := FileCoverage{URI: uri}
	for _, st := range d.Statements {
		fc.Statement.Total++
		if st.Executed > 0 {
			fc.Statement.Covered++
		}
		for _, br := range st.Branches {
			if fc.Branch == nil {
				fc.Branch = &Count{}
			}
			fc.Branch.Total++
			if br.Executed > 0 {
				fc.Branch.Covered++
			}
		}
	}
	for _, de := range d.Declarations {
		if fc.Declaration == nil {
			fc.Declaration = &Count{}
		}
		fc.Declaration.Total++
		if de.Executed > 0 {
			fc.Declaration.Covered++
		}
	}
	return fc
}

// Coverage is the coverage a task of a run reported: the files, and the sums for the folders above them. It is safe for concurrent use.
type Coverage struct {
	loader CoverageLoader

	mu      sync.Mutex
	root    *covNode
	files   []string // the URIs of the files, in the order they were first reported
	seq     int
	details map[string]Details // by the file's id
}

type covNode struct {
	name     string
	value    *FileCoverage
	isFile   bool
	children []*covNode
}

func newCoverage(loader CoverageLoader) *Coverage {
	return &Coverage{loader: loader, root: &covNode{}, details: map[string]Details{}}
}

// uriPath is the tree path of a URI: the scheme, the authority and the parts of the path (VS Code's treePathForUri).
func uriPath(uri string) []string {
	scheme, rest, found := strings.Cut(uri, "://")
	if !found {
		return append([]string{"", ""}, strings.Split(uri, "/")...)
	}
	authority, path, _ := strings.Cut(rest, "/")
	return append([]string{scheme, authority}, strings.Split("/"+path, "/")...)
}

// pathURI is the URI of the first n parts of a tree path.
func pathURI(path []string) string {
	switch len(path) {
	case 0:
		return ""
	case 1:
		return path[0] + "://"
	}
	return path[0] + "://" + path[1] + strings.Join(path[2:], "/")
}

func cloneCount(c *Count) *Count {
	if c == nil {
		return nil
	}
	v := *c
	return &v
}

func (n *covNode) child(name string) *covNode {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	c := &covNode{name: name}
	n.children = append(n.children, c)
	return c
}

// find returns the node at the path, or nil.
func (c *Coverage) find(path []string) *covNode {
	n := c.root
	for _, seg := range path {
		var next *covNode
		for _, ch := range n.children {
			if ch.name == seg {
				next = ch
				break
			}
		}
		if next == nil {
			return nil
		}
		n = next
	}
	return n
}

// Append adds the coverage of a file; a file reported again replaces its numbers, and the sums of the folders above it change by the
// difference (VS Code's TestCoverage.append). A file with no ID gets one.
func (c *Coverage) Append(fc FileCoverage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := uriPath(fc.URI)
	if fc.ID == "" {
		c.seq++
		fc.ID = "coverage-" + strconv.Itoa(c.seq)
	}
	var previous *FileCoverage
	if n := c.find(path); n != nil && n.value != nil {
		p := *n.value
		previous = &p
	} else {
		c.files = append(c.files, fc.URI)
	}
	delta := func(kind string, node *FileCoverage) {
		get := func(f *FileCoverage) **Count {
			switch kind {
			case "branch":
				return &f.Branch
			case "declaration":
				return &f.Declaration
			}
			return nil
		}
		newer, older := fileKind(&fc, kind), fileKind(previous, kind)
		if kind == "statement" {
			node.Statement.Covered += newer.Covered - older.Covered
			node.Statement.Total += newer.Total - older.Total
			return
		}
		slot := get(node)
		if *slot == nil {
			if cnt := optKind(&fc, kind); cnt != nil {
				*slot = cloneCount(cnt)
			}
			return
		}
		(*slot).Covered += newer.Covered - older.Covered
		(*slot).Total += newer.Total - older.Total
	}
	node := c.root
	for i, seg := range path {
		node = node.child(seg)
		if i == len(path)-1 {
			if node.value == nil {
				v := fc
				v.Branch, v.Declaration = cloneCount(fc.Branch), cloneCount(fc.Declaration)
				node.value = &v
			} else {
				node.value.ID, node.value.Statement, node.value.Data = fc.ID, fc.Statement, fc.Data
				node.value.Branch, node.value.Declaration = cloneCount(fc.Branch), cloneCount(fc.Declaration)
			}
			node.isFile = true
			delete(c.details, fc.ID)
			continue
		}
		if node.value == nil {
			v := fc
			c.seq++
			v.ID, v.URI, v.Data = "coverage-"+strconv.Itoa(c.seq), pathURI(path[:i+1]), nil
			v.Branch, v.Declaration = cloneCount(fc.Branch), cloneCount(fc.Declaration)
			node.value = &v
		} else {
			delta("statement", node.value)
			delta("branch", node.value)
			delta("declaration", node.value)
		}
	}
}

// fileKind is the count of the kind of a file, zero for a file that is not there or has none.
func fileKind(f *FileCoverage, kind string) Count {
	if cnt := optKind(f, kind); cnt != nil {
		return *cnt
	}
	return Count{}
}

func optKind(f *FileCoverage, kind string) *Count {
	if f == nil {
		return nil
	}
	switch kind {
	case "branch":
		return f.Branch
	case "declaration":
		return f.Declaration
	}
	return &f.Statement
}

// Files lists the files that were reported, in the order they were first reported.
func (c *Coverage) Files() []FileCoverage {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]FileCoverage, 0, len(c.files))
	for _, uri := range c.files {
		if n := c.find(uriPath(uri)); n != nil && n.value != nil {
			out = append(out, copyCoverage(*n.value))
		}
	}
	return out
}

func copyCoverage(f FileCoverage) FileCoverage {
	f.Branch, f.Declaration = cloneCount(f.Branch), cloneCount(f.Declaration)
	return f
}

// Computed returns the coverage of the file or folder at the URI: the numbers the controller gave for a file, and the sum of the files
// below it for a folder (VS Code's getComputedForUri).
func (c *Coverage) Computed(uri string) (FileCoverage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n := c.find(uriPath(uri)); n != nil && n.value != nil {
		return copyCoverage(*n.value), true
	}
	return FileCoverage{}, false
}

// Total is the sum of all the files, and false when there are none.
func (c *Coverage) Total() (FileCoverage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sum FileCoverage
	found := false
	for _, uri := range c.files {
		n := c.find(uriPath(uri))
		if n == nil || n.value == nil {
			continue
		}
		found = true
		sum.Statement.Covered += n.value.Statement.Covered
		sum.Statement.Total += n.value.Statement.Total
		for _, k := range []struct {
			from *Count
			to   **Count
		}{{n.value.Branch, &sum.Branch}, {n.value.Declaration, &sum.Declaration}} {
			if k.from == nil {
				continue
			}
			if *k.to == nil {
				*k.to = &Count{}
			}
			(*k.to).Covered += k.from.Covered
			(*k.to).Total += k.from.Total
		}
	}
	return sum, found
}

// CoverageNode is a file or a folder of the coverage tree.
type CoverageNode struct {
	// Name is the last part of the path, Coverage the numbers of the node and IsFile says that the controller reported the file.
	Name     string
	Coverage FileCoverage
	IsFile   bool
	Children []CoverageNode
}

// Tree is the tree of the files and folders: the nodes under the scheme and the authority of the URIs, so that a single scheme and
// authority give the folders of the workspace at the top. With several schemes or authorities there is a root for each.
func (c *Coverage) Tree() []CoverageNode {
	c.mu.Lock()
	defer c.mu.Unlock()
	var conv func(n *covNode) CoverageNode
	conv = func(n *covNode) CoverageNode {
		out := CoverageNode{Name: n.name, IsFile: n.isFile}
		if n.value != nil {
			out.Coverage = copyCoverage(*n.value)
		}
		for _, ch := range n.children {
			out.Children = append(out.Children, conv(ch))
		}
		return out
	}
	var out []CoverageNode
	for _, ch := range c.root.children {
		out = append(out, conv(ch))
	}
	return out
}

// Details loads the details of the file at the URI with the loader of the profile that measured it, once: the details are kept until
// the file is reported again. It fails with TestingCoverageUnknownFile for a URI that is no file of the coverage and with
// TestingCoverageNoLoader when the profile has no loader.
func (c *Coverage) Details(ctx context.Context, uri string) (Details, error) {
	c.mu.Lock()
	n := c.find(uriPath(uri))
	if n == nil || n.value == nil || !n.isFile {
		c.mu.Unlock()
		return Details{}, errors.New("TestingCoverageUnknownFile", "the coverage has no file with this URI", "uri", uri)
	}
	file := copyCoverage(*n.value)
	if d, ok := c.details[file.ID]; ok {
		c.mu.Unlock()
		return cloneDetails(d), nil
	}
	loader := c.loader
	c.mu.Unlock()
	if loader == nil {
		return Details{}, errors.New("TestingCoverageNoLoader", "the profile that measured the coverage cannot load details", "uri", uri)
	}
	d, err := loader(ctx, file)
	if err != nil {
		return Details{}, err
	}
	c.mu.Lock()
	if cur := c.find(uriPath(uri)); cur != nil && cur.value != nil && cur.value.ID == file.ID {
		c.details[file.ID] = cloneDetails(d)
	}
	c.mu.Unlock()
	return d, nil
}

func cloneDetails(d Details) Details {
	d.Statements = slices.Clone(d.Statements)
	d.Declarations = slices.Clone(d.Declarations)
	return d
}
