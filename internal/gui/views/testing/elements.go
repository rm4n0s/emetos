package testing

import (
	"sort"
	"strings"
	"time"
	"unicode"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
)

// ViewMode is how the tests are laid out: a tree of the items as the controller made it, or a flat list of the tests that have no children
// (each with the path to it after its label).
type ViewMode string

// The view modes. VS Code's constant for the tree is the string "true" (a slip of its source), and so is the value of the context key
// testing.explorerViewMode; it is kept so that a when-clause written for VS Code works.
const (
	ModeList ViewMode = "list"
	ModeTree ViewMode = "true"
)

// Sorting is what the tests are sorted by.
type Sorting string

// The sort orders.
const (
	SortByLocation Sorting = "location"
	SortByStatus   Sorting = "status"
	SortByDuration Sorting = "duration"
)

// ListDelimiter is written between the labels of the path of a test in the list.
const ListDelimiter = " › "

// Kind says what an Element is.
type Kind int

// The kinds of elements.
const (
	// KindTest is an item of a controller's tree.
	KindTest Kind = iota + 1
	// KindController is the root of a controller, shown when there are several controllers.
	KindController
	// KindError is the row below an item that has an error.
	KindError
)

// Element is a row of the Test Explorer.
type Element struct {
	Kind Kind
	// Key is the key of the row in the tree.
	Key string
	// Item is the test (the zero value for a controller or an error row); ID is its id (the controller's id for a controller).
	Item coretesting.Item
	ID   string
	// Label is the text of the row; Description follows it (the path in the list, the item's description in the tree).
	Label, Description string
	// State is the state shown, Duration the duration shown (NoDuration, -1, for none) and Retired says that the result is outdated.
	State    coretesting.State
	Duration time.Duration
	Retired  bool
	// Busy says that the controller is working on the item: it shows as running.
	Busy bool
	// Hidden says that the user hid the test (or one above it).
	Hidden bool
	// Error is the message of an error row.
	Error string
	// Children and Parent are the rows below and above.
	Children []*Element
	Parent   *Element
}

// HasDuration reports whether a duration is to be shown.
func (e *Element) HasDuration() bool { return e.Duration >= 0 }

// Source is the test model the projections read; *coretesting.Service is one.
type Source interface {
	Controllers() []coretesting.ControllerInfo
	Children(id string) []coretesting.Item
	StateOf(id string) (*coretesting.Result, coretesting.TestResult, bool)
}

// ProjectOptions say how the elements are made.
type ProjectOptions struct {
	Mode    ViewMode
	Sorting Sorting
	// Hidden are the ids of the tests the user hid.
	Hidden map[string]bool
}

// Project makes the rows of the explorer: the roots (the items of the single controller, or the controllers), sorted. The filter is not
// applied here (Visible).
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/explorerProjections/treeProjection.ts and listProjection.ts
func Project(src Source, o ProjectOptions) []*Element {
	ctrls := src.Controllers()
	var roots []*Element
	for _, c := range ctrls {
		var tops []*Element
		if o.Mode == ModeList {
			tops = listOf(src, c.ID, nil, o)
		} else {
			for _, it := range src.Children(c.ID) {
				tops = append(tops, treeOf(src, it, nil, o))
			}
		}
		if len(tops) == 0 {
			continue
		}
		if len(ctrls) == 1 {
			roots = tops
			for _, t := range roots {
				t.Parent = nil
			}
			break
		}
		root := &Element{Kind: KindController, Key: c.ID, ID: c.ID, Label: c.Label, Children: tops, Item: coretesting.Item{ID: c.ID, ControllerID: c.ID, Label: c.Label}}
		for _, t := range tops {
			t.Parent = root
		}
		computeParent(root)
		roots = append(roots, root)
	}
	sortElements(roots, o.Sorting)
	return roots
}

// stateOf fills the state, duration and retired mark of a test from the newest result that has it.
func stateOf(src Source, e *Element, own bool) {
	e.State, e.Duration = coretesting.StateUnset, coretesting.NoDuration
	if _, tr, ok := src.StateOf(e.ID); ok {
		e.State, e.Duration, e.Retired = tr.State, tr.Duration, tr.Retired
		if own {
			e.State, e.Duration = tr.OwnState, tr.OwnDuration
			if tr.OwnState == coretesting.StateUnset {
				e.State = tr.State
			}
		}
	}
	if e.Duration < 0 {
		e.Duration = coretesting.NoDuration
	}
}

// treeOf makes the element of an item and of the items below it.
func treeOf(src Source, it coretesting.Item, parent *Element, o ProjectOptions) *Element {
	e := &Element{Kind: KindTest, Key: it.ID, ID: it.ID, Item: it, Label: it.Label, Description: it.Description, Busy: it.Busy, Parent: parent,
		Hidden: isHidden(o.Hidden, it.ID)}
	stateOf(src, e, false)
	for _, c := range src.Children(it.ID) {
		e.Children = append(e.Children, treeOf(src, c, e, o))
	}
	if it.Error != "" {
		e.Children = append(e.Children, &Element{Kind: KindError, Key: it.ID + "\x01error", ID: it.ID, Error: it.Error, Label: it.Error, Parent: e,
			Duration: coretesting.NoDuration})
	}
	return e
}

// listOf makes the flat list of the tests of a controller that have no children; the path to a test is its description.
func listOf(src Source, ctrl string, _ *Element, o ProjectOptions) []*Element {
	var out []*Element
	var walk func(it coretesting.Item, chain []string)
	walk = func(it coretesting.Item, chain []string) {
		kids := src.Children(it.ID)
		if len(kids) == 0 {
			e := &Element{Kind: KindTest, Key: it.ID, ID: it.ID, Item: it, Label: it.Label, Description: strings.Join(chain, ListDelimiter), Busy: it.Busy,
				Hidden: isHidden(o.Hidden, it.ID)}
			stateOf(src, e, true)
			if it.Error != "" {
				e.Children = []*Element{{Kind: KindError, Key: it.ID + "\x01error", ID: it.ID, Error: it.Error, Label: it.Error, Parent: e, Duration: coretesting.NoDuration}}
			}
			out = append(out, e)
			return
		}
		for _, k := range kids {
			walk(k, append(append([]string(nil), chain...), it.Label))
		}
	}
	for _, it := range src.Children(ctrl) {
		walk(it, nil)
	}
	return out
}

// computeParent gives a controller's row the state of its most important child and the sum of their durations.
func computeParent(root *Element) {
	root.State, root.Duration = coretesting.StateUnset, coretesting.NoDuration
	for _, c := range root.Children {
		if c.State.Priority() > root.State.Priority() {
			root.State = c.State
		}
		if c.Duration >= 0 {
			if root.Duration < 0 {
				root.Duration = 0
			}
			root.Duration += c.Duration
		}
	}
}

// isHidden reports whether the test or a test above it is hidden.
func isHidden(hidden map[string]bool, id string) bool {
	for _, x := range coretesting.IDsToRoot(id) {
		if hidden[x] {
			return true
		}
	}
	return false
}

// sortElements sorts the rows and the rows below them.
func sortElements(es []*Element, by Sorting) {
	sort.SliceStable(es, func(i, j int) bool { return compareElements(es[i], es[j], by) < 0 })
	for _, e := range es {
		sortElements(e.Children, by)
	}
}

// compareElements orders two siblings: an error row first, then by duration (longest first) or status (the most important first) when
// asked, then by place in a file, then by sort text or label in natural order.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts (TreeSorter)
func compareElements(a, b *Element, by Sorting) int {
	if a.Kind == KindError || b.Kind == KindError {
		ai, bi := 0, 0
		if a.Kind == KindError {
			ai = -1
		}
		if b.Kind == KindError {
			bi = 1
		}
		return ai + bi
	}
	ad, bd := max(a.Duration, 0), max(b.Duration, 0)
	if by == SortByDuration && bd != ad {
		return int(bd - ad)
	}
	if by == SortByStatus {
		if d := b.State.Priority() - a.State.Priority(); d != 0 {
			return d
		}
	}
	same := false
	if a.Item.URI != "" && a.Item.URI == b.Item.URI && a.Item.Range != nil && b.Item.Range != nil {
		same = true
		if d := a.Item.Range.Start.Line - b.Item.Range.Start.Line; d != 0 {
			return d
		}
	}
	sa, sb := a.Item.SortText, b.Item.SortText
	if same && sa == "" && sb == "" {
		return 0
	}
	if sa == "" {
		sa = a.Label
	}
	if sb == "" {
		sb = b.Label
	}
	return compareNatural(sa, sb)
}

// compareNatural compares two names the way a file manager does: case-insensitive, with runs of digits as numbers.
//
// Ported from VS Code: src/vs/base/common/comparers.ts (compareFileNames, simplified: no locale collation)
func compareNatural(a, b string) int {
	ar, br := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0
	for i < len(ar) && j < len(br) {
		if unicode.IsDigit(ar[i]) && unicode.IsDigit(br[j]) {
			si, sj := i, j
			for i < len(ar) && unicode.IsDigit(ar[i]) {
				i++
			}
			for j < len(br) && unicode.IsDigit(br[j]) {
				j++
			}
			na, nb := strings.TrimLeft(string(ar[si:i]), "0"), strings.TrimLeft(string(br[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) - len(nb)
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			continue
		}
		if ar[i] != br[j] {
			if ar[i] < br[j] {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	return (len(ar) - i) - (len(br) - j)
}

// FilterContext is what the filter needs to know besides the text.
type FilterContext struct {
	// DocumentURIs are the files the terms @doc and @openedFiles stand for.
	DocumentURIs []string
}

// Visible marks which elements the filter shows: the elements it includes, and those that have an element it includes below them. An
// element it excludes hides everything below it.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts (TestsFilter)
func Visible(roots []*Element, f *Filter, ctx FilterContext) map[*Element]bool {
	vis := map[*Element]bool{}
	var walk func(e *Element) bool
	walk = func(e *Element) bool {
		var r filterResult
		switch {
		case e.Kind == KindError:
			r = include
		case e.Hidden && !f.Has(TermHidden):
			r = exclude
		default:
			r = min(filterText(e, f), filterLocation(e, f, ctx), filterState(e, f), filterTags(e, f), filterController(e, f))
		}
		if r == exclude {
			return false
		}
		any := false
		for _, c := range e.Children {
			if walk(c) {
				any = true
			}
		}
		if r == include || any {
			vis[e] = true
			return true
		}
		return false
	}
	for _, r := range roots {
		walk(r)
	}
	return vis
}

func filterTags(e *Element, f *Filter) filterResult {
	if len(f.IncludeTags) == 0 && len(f.ExcludeTags) == 0 {
		return include
	}
	if e.Kind != KindTest {
		return inherit
	}
	ns := make([]string, len(e.Item.Tags))
	for i, t := range e.Item.Tags {
		ns[i] = NamespaceTag(e.Item.ControllerID, t)
	}
	ok := true
	if len(f.IncludeTags) > 0 {
		ok = false
		for _, t := range ns {
			if f.IncludeTags[t] {
				ok = true
			}
		}
	}
	for _, t := range ns {
		if f.ExcludeTags[t] {
			ok = false
		}
	}
	if ok {
		return include
	}
	return inherit
}

func filterController(e *Element, f *Filter) filterResult {
	if len(f.Controllers) == 0 {
		return include
	}
	ctrl := coretesting.ControllerOf(e.ID)
	if f.Controllers[ctrl] {
		return include
	}
	return exclude
}

func filterState(e *Element, f *Filter) filterResult {
	if e.Kind != KindTest && e.Kind != KindController {
		return include
	}
	switch {
	case f.Has(TermFailed):
		if e.State.IsFailed() {
			return include
		}
		return inherit
	case f.Has(TermExecuted):
		if e.State != coretesting.StateUnset {
			return include
		}
		return inherit
	}
	return include
}

func filterLocation(e *Element, f *Filter, ctx FilterContext) filterResult {
	if len(ctx.DocumentURIs) == 0 || (!f.Has(TermDoc) && !f.Has(TermOpenedFiles)) || e.Kind != KindTest {
		return include
	}
	for _, uri := range ctx.DocumentURIs {
		if hasTestInOrAboveURI(e, uri) {
			return include
		}
	}
	return inherit
}

// hasTestInOrAboveURI reports whether the test is in the file or has a test below it that is.
func hasTestInOrAboveURI(e *Element, uri string) bool {
	if e.Item.URI == uri {
		return true
	}
	for _, c := range e.Children {
		if c.Kind == KindTest && hasTestInOrAboveURI(c, uri) {
			return true
		}
	}
	return false
}

func filterText(e *Element, f *Filter) filterResult {
	if len(f.Globs) == 0 {
		return include
	}
	for x := e; x != nil; x = x.Parent {
		got := inherit
		if !f.Globs[0].Include {
			got = include
		}
		label := strings.ToLower(x.Label)
		for _, g := range f.Globs {
			match := strings.Contains(label, g.Text)
			if f.Fuzzy {
				match = fuzzyContains(label, g.Text)
			}
			if match {
				if g.Include {
					got = include
				} else {
					got = exclude
				}
			}
		}
		if got != inherit {
			return got
		}
	}
	return inherit
}
