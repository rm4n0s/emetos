package testing

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Opts are what the explorer needs of its window. Everything is called on the UI thread, except Post, which any goroutine may call.
type Opts struct {
	// Ctx ends what the explorer asked for when it is done.
	Ctx context.Context
	// Service is the test service of the workspace; the view of a client of a hosted workspace gets the host's.
	Service *coretesting.Service
	// Post runs a function on the UI thread, later.
	Post func(func())
	// DocumentURIs gives the files @doc (activeOnly) and @openedFiles stand for, as file:// URIs; none when nil.
	DocumentURIs func(activeOnly bool) []string
	// Fuzzy says whether the filter matches fuzzily (the menu of the filter toggles it); false when nil.
	Fuzzy func() bool
}

// Explorer is the Test Explorer's model: the rows of the tests as the mode, the sort order, the hidden tests and the filter make them, over
// the test service. A burst of events of the service makes the rows again once.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts (TestingExplorerViewModel)
type Explorer struct {
	opts  Opts
	model *kit.TreeModel
	ctl   *kit.TreeController
	input *kit.InputBoxController

	mode    ViewMode
	sorting Sorting
	filter  *Filter
	fuzzy   bool
	hidden  map[string]bool

	roots     []*Element
	rows      map[string]*Element
	visible   map[*Element]bool
	known     map[string]bool
	expandAll bool
	listeners []*func()

	stop  func()
	dirty atomic.Bool
}

// NewExplorer makes the explorer and follows the service.
func NewExplorer(opts Opts) *Explorer {
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	x := &Explorer{opts: opts, mode: ModeTree, sorting: SortByLocation, hidden: map[string]bool{}, rows: map[string]*Element{}, known: map[string]bool{},
		ctl: kit.NewTreeController(), input: kit.NewInputBoxController("")}
	x.model = kit.NewTreeModel(kit.TreeModelOpts{MultiSelect: true})
	x.filter = ParseFilter("", false)
	x.stop = opts.Service.Subscribe(x.onEvent)
	x.Refresh()
	return x
}

// Dispose stops following the service.
func (x *Explorer) Dispose() {
	x.stop()
	x.input.Dispose()
}

// onEvent hands the events of the service to the UI thread, one burst at a time. The events about output and coverage do not change a row.
func (x *Explorer) onEvent(ev coretesting.Event) {
	if ev.Kind == coretesting.EventCoverage {
		return
	}
	if x.dirty.Swap(true) {
		return
	}
	x.opts.Post(func() {
		x.dirty.Store(false)
		x.Refresh()
	})
}

// Model is the tree the view shows, Controller gives access to the tree widget (focus, reveal) and Input to the filter box.
func (x *Explorer) Model() *kit.TreeModel { return x.model }

// Controller returns the tree controller of the view.
func (x *Explorer) Controller() *kit.TreeController { return x.ctl }

// Input returns the filter box controller.
func (x *Explorer) Input() *kit.InputBoxController { return x.input }

// Service is the test service the explorer shows.
func (x *Explorer) Service() *coretesting.Service { return x.opts.Service }

// OnChange calls fn after the rows changed, and returns the function that stops it.
func (x *Explorer) OnChange(fn func()) func() {
	p := &fn
	x.listeners = append(x.listeners, p)
	return func() {
		if i := slices.Index(x.listeners, p); i >= 0 {
			x.listeners = slices.Delete(x.listeners, i, i+1)
		}
	}
}

func (x *Explorer) notify() {
	for _, f := range slices.Clone(x.listeners) {
		(*f)()
	}
}

// Mode is how the tests are laid out and Sorting what they are sorted by.
func (x *Explorer) Mode() ViewMode { return x.mode }

// Sorting returns the current sort order.
func (x *Explorer) Sorting() Sorting { return x.sorting }

// SetMode changes the layout.
func (x *Explorer) SetMode(m ViewMode) {
	if m != x.mode {
		x.mode = m
		x.Refresh()
	}
}

// SetSorting changes the sort order.
func (x *Explorer) SetSorting(s Sorting) {
	if s != x.sorting {
		x.sorting = s
		x.Refresh()
	}
}

// Filter is the parsed text of the filter box.
func (x *Explorer) Filter() *Filter { return x.filter }

// FilterText is the text of the filter box.
func (x *Explorer) FilterText() string { return x.filter.Text }

// SetFilterText sets the text of the filter (and of the box): the rows are made again, and all the rows that pass are opened so that the
// matches are in view.
func (x *Explorer) SetFilterText(text string) {
	if x.input.Value() != text {
		x.input.SetValue(text)
	}
	fuzzy := x.opts.Fuzzy != nil && x.opts.Fuzzy()
	if text == x.filter.Text && fuzzy == x.fuzzy {
		return
	}
	x.fuzzy = fuzzy
	x.filter = ParseFilter(text, fuzzy)
	x.expandAll = !x.filter.Empty()
	x.Refresh()
}

// ToggleTerm adds the term to the text of the filter, or takes it out.
func (x *Explorer) ToggleTerm(term string) {
	x.SetFilterText(ToggleTerm(x.filter.Text, term, !x.filter.Has(term)))
}

// Hide hides the tests; Unhide shows them again and UnhideAll every one. HasHidden reports whether any test is hidden, and IsHidden whether
// the test or one above it is.
func (x *Explorer) Hide(ids ...string) {
	for _, id := range ids {
		x.hidden[id] = true
	}
	x.Refresh()
}

// Unhide shows the tests (and the tests above them that were hidden) again.
func (x *Explorer) Unhide(ids ...string) {
	for _, id := range ids {
		for _, up := range coretesting.IDsToRoot(id) {
			delete(x.hidden, up)
		}
	}
	x.Refresh()
}

// UnhideAll shows every test.
func (x *Explorer) UnhideAll() {
	clear(x.hidden)
	x.Refresh()
}

// HasHidden reports whether the user hid a test.
func (x *Explorer) HasHidden() bool { return len(x.hidden) > 0 }

// IsHidden reports whether the test or a test above it is hidden.
func (x *Explorer) IsHidden(id string) bool { return isHidden(x.hidden, id) }

// HiddenIDs are the ids of the tests the user hid, sorted.
func (x *Explorer) HiddenIDs() []string {
	out := make([]string, 0, len(x.hidden))
	for id := range x.hidden {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// CollapseAll folds every row.
func (x *Explorer) CollapseAll() { x.model.CollapseAll() }

// Row is the element of a row of the tree.
func (x *Explorer) Row(key string) (*Element, bool) {
	e, ok := x.rows[key]
	return e, ok
}

// Element is the row of a test (or controller), shown or not.
func (x *Explorer) Element(id string) *Element {
	var find func(es []*Element) *Element
	find = func(es []*Element) *Element {
		for _, e := range es {
			if e.ID == id && e.Kind != KindError {
				return e
			}
			if f := find(e.Children); f != nil {
				return f
			}
		}
		return nil
	}
	return find(x.roots)
}

// Focused is the row that has the focus in the tree, if any.
func (x *Explorer) Focused() (*Element, bool) {
	key, ok := x.model.Focused()
	if !ok {
		return nil, false
	}
	return x.Row(key)
}

// Selected are the rows that are selected, in the order of the tree.
func (x *Explorer) Selected() []*Element {
	var out []*Element
	for _, k := range x.model.SelectedKeys() {
		if e, ok := x.Row(k); ok {
			out = append(out, e)
		}
	}
	return out
}

// Roots are the rows at the top, as projected (the filter is not applied).
func (x *Explorer) Roots() []*Element { return x.roots }

// Visible reports whether the filter shows the row.
func (x *Explorer) Visible(e *Element) bool { return x.visible[e] }

// Refresh makes the rows again from the service.
func (x *Explorer) Refresh() {
	x.roots = Project(x.opts.Service, ProjectOptions{Mode: x.mode, Sorting: x.sorting, Hidden: x.hidden})
	var docs []string
	if x.opts.DocumentURIs != nil {
		docs = x.opts.DocumentURIs(x.filter.Has(TermDoc))
	}
	x.visible = Visible(x.roots, x.filter, FilterContext{DocumentURIs: docs})
	x.rows = map[string]*Element{}
	var items []kit.TreeItem
	var fresh []string
	var build func(es []*Element, depth int) []kit.TreeItem
	build = func(es []*Element, depth int) []kit.TreeItem {
		var out []kit.TreeItem
		for _, e := range es {
			if !x.visible[e] {
				continue
			}
			x.rows[e.Key] = e
			if !x.known[e.Key] {
				x.known[e.Key] = true
				if depth == 0 && e.Kind != KindError {
					fresh = append(fresh, e.Key)
				}
			}
			out = append(out, kit.TreeItem{Key: e.Key, Data: e, Label: e.Label, Children: build(e.Children, depth+1)})
		}
		return out
	}
	items = build(x.roots, 0)
	_ = x.model.SetItems(items)
	for _, k := range fresh {
		x.model.Expand(k)
	}
	if x.expandAll {
		x.expandAll = false
		x.model.ExpandAll()
	}
	x.notify()
}

// SummaryState says how the last run went (or the live ones are going).
type SummaryState struct {
	// None says that there is no result yet.
	None bool
	// Running says that a run goes on; Dominant is the most important state of the counted tests.
	Running  bool
	Dominant coretesting.State
	Counts   Summary
	// Duration is how long the run took (so far, for a live one).
	Duration time.Duration
	Text     string
}

// statesInOrder lists the states, the most important first.
var statesInOrder = []coretesting.State{coretesting.StateRunning, coretesting.StateErrored, coretesting.StateFailed, coretesting.StateQueued,
	coretesting.StatePassed, coretesting.StateSkipped, coretesting.StateUnset}

// SummaryOf reads the summary line from the results of the service; elapsed says how long a run that goes on has taken.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts (ResultSummaryView.render)
func SummaryOf(svc *coretesting.Service, elapsed func(*coretesting.Result) time.Duration) SummaryState {
	results := svc.Results()
	if len(results) == 0 {
		return SummaryState{None: true}
	}
	var live []*coretesting.Result
	for _, r := range results {
		if !r.IsComplete() {
			live = append(live, r)
		}
	}
	if len(live) > 0 {
		s := Collect(true, live)
		last := live[len(live)-1]
		return SummaryState{Running: true, Dominant: coretesting.StateRunning, Counts: s, Duration: elapsed(last), Text: ProgressText(s)}
	}
	last := results[0]
	s := Collect(false, []*coretesting.Result{last})
	c := last.Counts()
	dom := coretesting.StateUnset
	for _, st := range statesInOrder {
		if c[st] > 0 {
			dom = st
			break
		}
	}
	return SummaryState{Dominant: dom, Counts: s, Duration: last.CompletedAt().Sub(last.StartedAt()), Text: ProgressText(s)}
}

// IncludeExclude works out which tests to run when the buttons of the title bar run what the tree shows (selectedOnly false), or what is
// selected in it: the first rows that have a majority of their runnable children shown are included, and the rows the filter hides below
// an included row are excluded. can says whether the group has a profile for a test.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts (getTreeIncludeExclude)
func (x *Explorer) IncludeExclude(selectedOnly bool, can func(id string) bool) (include, exclude []string) {
	inc := map[string]bool{}
	var order []string
	add := func(id string) {
		if !inc[id] {
			inc[id] = true
			order = append(order, id)
		}
	}
	runnable := map[*Element]bool{}
	isRunnable := func(e *Element) bool {
		v, ok := runnable[e]
		if !ok {
			v = e.Kind != KindError && can(e.ID)
			runnable[e] = v
		}
		return v
	}
	var attempt func(e *Element, already bool)
	attempt = func(e *Element, already bool) {
		if e.Kind == KindError {
			return
		}
		if !x.visible[e] {
			if already {
				exclude = append(exclude, e.ID)
			}
			return
		}
		n := 0
		for _, c := range e.Children {
			if x.visible[c] && c.Kind != KindError && isRunnable(c) {
				n++
			}
		}
		if !already && isRunnable(e) && (n == 0 || n*2 >= len(e.Children)) && n != 1 {
			add(e.ID)
			already = true
		}
		for _, c := range e.Children {
			attempt(c, already)
		}
	}
	if selectedOnly {
		if sel := x.Selected(); len(sel) > 0 {
		L:
			for _, e := range sel {
				if e.Kind == KindError {
					continue
				}
				for up := e; up != nil; up = up.Parent {
					if inc[up.ID] {
						continue L
					}
				}
				add(e.ID)
				for _, c := range e.Children {
					attempt(c, true)
				}
			}
			return order, exclude
		}
	}
	// the controllers' own ids stand for all their tests
	for _, ctrl := range x.opts.Service.Controllers() {
		if !can(ctrl.ID) {
			continue
		}
		add(ctrl.ID)
		for _, r := range x.roots {
			if r.Kind == KindController {
				if r.ID == ctrl.ID {
					for _, c := range r.Children {
						attempt(c, true)
					}
				}
			} else if coretesting.ControllerOf(r.ID) == ctrl.ID {
				attempt(r, true)
			}
		}
	}
	return order, exclude
}

// Touch tells the view to draw again, for what the rows do not show: the time a live run has taken.
func (x *Explorer) Touch() { x.notify() }
