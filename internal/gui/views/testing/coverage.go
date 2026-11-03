package testing

import (
	"cmp"
	"context"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/rm4n0s/glutter/foundation"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The Test Coverage view (SPEC §10.32 "Test Coverage", step 26.4): the files and folders of the open coverage with their percentages and
// bars, and under a file its declarations (functions), sorted by location, coverage or name.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testCoverageView.ts

// Keys of the widgets of the view, which tests find them by.
var (
	// CoverageTreeKey is the key of the tree.
	CoverageTreeKey = foundation.NewValueKey("testcoverage:tree")
)

// CoverageRowKey is the key of the widget of a row of the tree.
func CoverageRowKey(key string) foundation.Key {
	return foundation.NewValueKey("testcoverage:row:" + key)
}

// CoverageSort is the order of the rows of the view.
type CoverageSort string

// The orders (VS Code's CoverageSortOrder).
const (
	// CoverageByLocation sorts the files by path and the declarations by position, CoverageByCoverage both by total coverage (the best
	// first) and CoverageByName both by name.
	CoverageByLocation CoverageSort = "location"
	CoverageByCoverage CoverageSort = "coverage"
	CoverageByName     CoverageSort = "name"
)

// CoverageSortChoice is an entry of the picker of the order.
type CoverageSortChoice struct {
	Sort               CoverageSort
	Label, Description string
}

// CoverageSortChoices are the entries of the picker, in VS Code's order and words.
var CoverageSortChoices = []CoverageSortChoice{
	{CoverageByLocation, "Sort by Location", "Files are sorted alphabetically, declarations are sorted by position"},
	{CoverageByCoverage, "Sort by Coverage", "Files and declarations are sorted by total coverage"},
	{CoverageByName, "Sort by Name", "Files and declarations are sorted alphabetically"},
}

// CoverageKind says what a node of the tree is.
type CoverageKind int

// The kinds of node.
const (
	// CoverageFolder is a folder (or a chain of folders with one folder each), CoverageFile a file the controller reported and
	// CoverageDeclaration a function of a file.
	CoverageFolder CoverageKind = iota
	CoverageFile
	CoverageDeclaration
)

// CoverageNode is a node of the tree of the view.
type CoverageNode struct {
	Kind  CoverageKind
	Key   string
	Label string
	// URI is the file or folder, and for a declaration the file it is in.
	URI string
	// Coverage is the numbers of a file or folder.
	Coverage coretesting.FileCoverage
	// Name, Executed and Range are those of a declaration; Attr is the coverage of the statements inside it, nil when it is not known (a
	// declaration that did not run, or with no statements).
	Name     string
	Executed int
	Range    coretesting.Range
	Attr     *coretesting.FileCoverage
	// children are the declarations inside a declaration.
	children []*CoverageNode
}

// TotalPercent is the total coverage of the node, from 0 to 1; a declaration has it when its statements are known and -1 otherwise.
func (n *CoverageNode) TotalPercent() float64 {
	if n.Kind == CoverageDeclaration {
		if n.Attr == nil {
			return -1
		}
		return coretesting.TotalPercent(n.Attr.Statement, n.Attr.Branch, nil)
	}
	return n.Coverage.TotalPercent()
}

// CoverageOpts are what the view needs of its window.
type CoverageOpts struct {
	// Ctx ends what the view asked for when it is done.
	Ctx context.Context
	// Service holds the open coverage.
	Service *coretesting.CoverageService
	// Post runs a function on the UI thread, later.
	Post func(func())
	// Top writes the label of the folder the coverage starts in from its path; nil writes the last part of the path.
	Top func(path string) string
	// Icons gives the file icon theme and Language the language of a file by its path; nil for none.
	Icons    func() *theme.FileIconTheme
	Language func(path string) string
	// Percent gives testing.displayedCoveragePercent and Thresholds the value of testing.coverageBarThresholds; nil for the defaults.
	Percent    func() string
	Thresholds func() coretesting.BarThresholds
	// OnOpen is called when the user opens a file or a declaration (Enter, a double click or a click on a leaf).
	OnOpen func(*CoverageNode)
	// OnFocusChange is told when the tree gains or loses the keyboard focus.
	OnFocusChange func(has bool)
}

// Coverage is the model of the view.
type Coverage struct {
	opts  CoverageOpts
	model *kit.TreeModel
	ctl   *kit.TreeController

	sort      CoverageSort
	nodes     map[string]*CoverageNode
	shownFor  *coretesting.Coverage
	listeners map[int]func()
	seq       int
	stop      func()
	dirty     atomic.Bool
	// loads counts the loads of details, so that the answer of a stale one is dropped.
	loads int
}

// NewCoverage makes the model and follows the service.
func NewCoverage(opts CoverageOpts) *Coverage {
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	c := &Coverage{opts: opts, sort: CoverageByLocation, nodes: map[string]*CoverageNode{}, listeners: map[int]func(){}, ctl: kit.NewTreeController()}
	c.model = kit.NewTreeModel(kit.TreeModelOpts{LoadChildren: c.load})
	c.stop = opts.Service.OnChange(c.onChange)
	c.Refresh()
	return c
}

// Dispose stops following the service.
func (c *Coverage) Dispose() { c.stop() }

// onChange hands a change of the service to the UI thread, one burst at a time.
func (c *Coverage) onChange() {
	if c.dirty.Swap(true) {
		return
	}
	c.opts.Post(func() {
		c.dirty.Store(false)
		c.Refresh()
	})
}

// Model is the tree and Controller gives access to the tree widget.
func (c *Coverage) Model() *kit.TreeModel { return c.model }

// Controller gives access to the tree widget.
func (c *Coverage) Controller() *kit.TreeController { return c.ctl }

// Node is the node with the key.
func (c *Coverage) Node(key string) (*CoverageNode, bool) { n, ok := c.nodes[key]; return n, ok }

// Sort is the order of the rows.
func (c *Coverage) Sort() CoverageSort { return c.sort }

// SetSort changes the order of the rows.
func (c *Coverage) SetSort(s CoverageSort) {
	if s == c.sort {
		return
	}
	c.sort = s
	c.Refresh()
}

// OnChange registers a function called when the rows or the order change; the returned function removes it.
func (c *Coverage) OnChange(fn func()) (stop func()) {
	c.seq++
	id := c.seq
	c.listeners[id] = fn
	return func() { delete(c.listeners, id) }
}

func (c *Coverage) notify() {
	for id := 1; id <= c.seq; id++ {
		if fn, ok := c.listeners[id]; ok {
			fn()
		}
	}
}

// CollapseAll folds every node.
func (c *Coverage) CollapseAll() { c.model.CollapseAll() }

// Thresholds are the thresholds of the bars now, and Percent the way the percentage is worked out.
func (c *Coverage) Thresholds() coretesting.BarThresholds {
	if c.opts.Thresholds != nil {
		return c.opts.Thresholds()
	}
	return coretesting.DefaultBarThresholds
}

// Percent is the value of testing.displayedCoveragePercent.
func (c *Coverage) Percent() string {
	if c.opts.Percent != nil {
		return c.opts.Percent()
	}
	return coretesting.PercentTotal
}

// uriPath is the path of a file:// URI.
func uriPath(uri string) string {
	p := strings.TrimPrefix(uri, "file://")
	if un, err := url.PathUnescape(p); err == nil {
		return un
	}
	return p
}

// startNodes are the nodes the tree starts at: each root, followed down while it has one child and is not a reported file (VS Code's
// "when showing initial children, only show from the first file or tree").
func startNodes(tree []coretesting.CoverageNode) []coretesting.CoverageNode {
	var out []coretesting.CoverageNode
	for _, n := range tree {
		for !n.IsFile && len(n.Children) == 1 {
			n = n.Children[0]
		}
		out = append(out, n)
	}
	return out
}

// chainLabel joins the names of a chain of folders, leaving out the empty parts of a path.
func chainLabel(names []string) string {
	var parts []string
	for _, n := range names {
		if n != "" {
			parts = append(parts, n)
		}
	}
	if len(parts) == 0 {
		return "/"
	}
	return strings.Join(parts, "/")
}

// compare is the order of two nodes of the same kind (VS Code's Sorter).
func (c *Coverage) compare(a, b *CoverageNode) int {
	if a.Kind == CoverageDeclaration && b.Kind == CoverageDeclaration {
		switch c.sort {
		case CoverageByLocation:
			if r := cmp.Compare(a.Range.Start.Line, b.Range.Start.Line); r != 0 {
				return r
			}
			return cmp.Compare(a.Range.Start.Col, b.Range.Start.Col)
		case CoverageByName:
			return strings.Compare(a.Name, b.Name)
		}
		pa, pb := a.TotalPercent(), b.TotalPercent()
		if pa >= 0 && pb >= 0 {
			if r := cmp.Compare(pb, pa); r != 0 {
				return r
			}
		}
		if r := cmp.Compare(b.Executed, a.Executed); r != 0 {
			return r
		}
		return strings.Compare(a.Name, b.Name)
	}
	if a.Kind == CoverageDeclaration || b.Kind == CoverageDeclaration {
		return 0
	}
	if c.sort == CoverageByCoverage {
		return cmp.Compare(b.TotalPercent(), a.TotalPercent())
	}
	return strings.Compare(strings.ToLower(a.URI), strings.ToLower(b.URI))
}

// Refresh makes the rows again from the open coverage.
func (c *Coverage) Refresh() {
	c.loads++
	c.nodes = map[string]*CoverageNode{}
	sel, ok := c.opts.Service.Selected()
	if !ok {
		c.shownFor = nil
		_ = c.model.SetItems(nil)
		c.notify()
		return
	}
	fresh := c.shownFor != sel.Coverage
	c.shownFor = sel.Coverage
	var expand []string
	var build func(n coretesting.CoverageNode, top bool) *CoverageNode
	var item func(cn *CoverageNode) kit.TreeItem
	// kids holds the children of the nodes in the order of the sort, by key
	kids := map[string][]*CoverageNode{}
	build = func(n coretesting.CoverageNode, top bool) *CoverageNode {
		names := []string{n.Name}
		for !n.IsFile && len(n.Children) == 1 && !n.Children[0].IsFile {
			n = n.Children[0]
			names = append(names, n.Name)
		}
		cn := &CoverageNode{Key: n.Coverage.URI, URI: n.Coverage.URI, Coverage: n.Coverage, Label: chainLabel(names)}
		if n.IsFile {
			cn.Kind = CoverageFile
			cn.Label = n.Name
		} else {
			cn.Kind = CoverageFolder
		}
		if top && !n.IsFile {
			cn.Label = path.Base(uriPath(cn.URI))
			if c.opts.Top != nil {
				cn.Label = c.opts.Top(uriPath(cn.URI))
			}
		}
		c.nodes[cn.Key] = cn
		var list []*CoverageNode
		for _, ch := range n.Children {
			list = append(list, build(ch, false))
		}
		slices.SortStableFunc(list, c.compare)
		kids[cn.Key] = list
		if cn.Kind == CoverageFolder {
			expand = append(expand, cn.Key)
		}
		return cn
	}
	item = func(cn *CoverageNode) kit.TreeItem {
		it := kit.TreeItem{Key: cn.Key, Data: cn, Label: cn.Label}
		for _, ch := range kids[cn.Key] {
			it.Children = append(it.Children, item(ch))
		}
		if cn.Kind == CoverageFile && cn.Coverage.Declaration != nil && cn.Coverage.Declaration.Total > 0 {
			it.Lazy = true
		}
		return it
	}
	var tops []*CoverageNode
	for _, n := range startNodes(sel.Coverage.Tree()) {
		tops = append(tops, build(n, true))
	}
	slices.SortStableFunc(tops, c.compare)
	items := make([]kit.TreeItem, 0, len(tops))
	for _, t := range tops {
		items = append(items, item(t))
	}
	_ = c.model.SetItems(items)
	if fresh {
		for _, k := range expand {
			c.model.Expand(k)
		}
	}
	c.notify()
}

// load reads the declarations of a file that is expanded.
func (c *Coverage) load(key string, data any) {
	n, ok := data.(*CoverageNode)
	if !ok || n.Kind != CoverageFile {
		c.model.FailLoad(key)
		return
	}
	sel, ok := c.opts.Service.Selected()
	if !ok {
		c.model.FailLoad(key)
		return
	}
	gen, cov, uri := c.loads, sel.Coverage, n.URI
	go func() {
		d, err := cov.Details(c.opts.Ctx, uri)
		c.opts.Post(func() {
			if gen != c.loads || c.shownFor != cov {
				return
			}
			if err != nil {
				c.model.FailLoad(key)
				return
			}
			c.setDeclarations(key, n, d)
		})
	}()
}

// setDeclarations gives a file its declarations, nested by what contains what.
//
// Ported from VS Code: testCoverageView.ts (updateWithDetails, DeclarationCoverageNode)
func (c *Coverage) setDeclarations(key string, file *CoverageNode, d coretesting.Details) {
	var top []*CoverageNode
	var all []*CoverageNode
	contains := func(outer coretesting.Range, inner coretesting.Range) bool {
		return !posBefore(inner.Start, outer.Start) && !posBefore(outer.End, inner.End)
	}
	for i, de := range d.Declarations {
		n := &CoverageNode{Kind: CoverageDeclaration, Name: de.Name, Label: de.Name, URI: file.URI, Executed: de.Executed, Range: de.Location}
		// the statements inside the declaration give its own coverage
		if de.Executed > 0 {
			attr := &coretesting.FileCoverage{}
			any := false
			for _, st := range d.Statements {
				if !contains(de.Location, st.Location) {
					continue
				}
				any = true
				attr.Statement.Total++
				if st.Executed > 0 {
					attr.Statement.Covered++
				}
				for _, br := range st.Branches {
					if attr.Branch == nil {
						attr.Branch = &coretesting.Count{}
					}
					attr.Branch.Total++
					if br.Executed > 0 {
						attr.Branch.Covered++
					}
				}
			}
			if any {
				if attr.Branch == nil {
					attr.Branch = &coretesting.Count{}
				}
				n.Attr = attr
			}
		}
		n.Key = file.Key + "#" + strconv.Itoa(i)
		// nest it under the declaration before it that contains it
		arr := &top
		for {
			var parent *CoverageNode
			for _, p := range *arr {
				if contains(p.Range, de.Location) {
					parent = p
					break
				}
			}
			if parent == nil {
				break
			}
			arr = &parent.children
		}
		*arr = append(*arr, n)
		all = append(all, n)
		c.nodes[n.Key] = n
	}
	var item func(n *CoverageNode) kit.TreeItem
	item = func(n *CoverageNode) kit.TreeItem {
		it := kit.TreeItem{Key: n.Key, Data: n, Label: n.Label}
		slices.SortStableFunc(n.children, c.compare)
		for _, ch := range n.children {
			it.Children = append(it.Children, item(ch))
		}
		return it
	}
	slices.SortStableFunc(top, c.compare)
	items := make([]kit.TreeItem, 0, len(top))
	for _, n := range top {
		items = append(items, item(n))
	}
	_ = all
	if err := c.model.SetChildren(key, items); err != nil {
		c.model.FailLoad(key)
		return
	}
	c.notify()
}

func posBefore(a, b coretesting.Pos) bool {
	return a.Line < b.Line || a.Line == b.Line && a.Col < b.Col
}
