package scmgraph

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/scm/graph"
)

// ViewID is the id of the view in the Source Control container, and ViewName its title.
const (
	ViewID   = "workbench.scm.history"
	ViewName = "Graph"
)

// RowHeight is the height of a row and LaneWidth the width of a lane of the graph column.
const (
	RowHeight = 22.0
	LaneWidth = 11.0
)

// The messages of the view.
const (
	// MessageEmpty is shown while the repository has no history items (VS Code's welcome content of the view).
	MessageEmpty = "The selected source control provider does not have any source control history items."
	// MessageLoading is shown while the first page is read.
	MessageLoading = "Loading..."
	// MessageNoMatch is shown when the search finds nothing.
	MessageNoMatch = "No commits match the search."
)

// FilterKind says which references the graph shows the history of.
type FilterKind string

// The kinds of filter.
const (
	// FilterAuto shows the current branch, the one it tracks and the one it was made from.
	FilterAuto FilterKind = "auto"
	// FilterAll shows every branch, remote branch and tag.
	FilterAll FilterKind = "all"
	// FilterRefs shows the references with the ids in Filter.Refs.
	FilterRefs FilterKind = "refs"
)

// Filter is the choice of the references of the title of the view (VS Code's HistoryItemRefsFilter). The zero value is Auto.
type Filter struct {
	// Kind is the kind of filter.
	Kind FilterKind `json:"kind,omitempty"`
	// Refs are the ids of the references for FilterRefs.
	Refs []string `json:"refs,omitempty"`
}

func (f Filter) kind() FilterKind {
	if f.Kind == "" {
		return FilterAuto
	}
	return f.Kind
}

// RowKind says what a row of the list is.
type RowKind int

// The kinds of row.
const (
	// RowItem is a commit.
	RowItem RowKind = iota
	// RowIncoming and RowOutgoing are the rows of the incoming and the outgoing changes.
	RowIncoming
	// RowOutgoing is the row of the outgoing changes.
	RowOutgoing
	// RowLoadMore is the last row, which loads the next page.
	RowLoadMore
)

// Badge is a label of a row: a branch, a remote branch or a tag, or several that have the same color and icon.
type Badge struct {
	// Icon is the Codicon, Color the theme color of the background (empty for the default), Name the text shown beside the icon (only
	// for the first badge of a row, which has a color) and Count the number of references when more than one.
	Icon, Color, Name string
	Count             int
	// IDs are the references in the badge.
	IDs []string
}

// Row is a row of the list.
type Row struct {
	// Kind says what the row is.
	Kind RowKind
	// Item is the commit; for the incoming and the outgoing changes a made-up one with the subject and the author.
	Item scm.HistoryItem
	// Lane is the commit with the lanes of its row (zero for the load-more row, which has Columns).
	Lane graph.Row
	// Columns are the lanes the load-more row continues.
	Columns []graph.Node
	// Current says that the row is the commit the current branch is at.
	Current bool
	// Badges are the labels of the row.
	Badges []Badge
}

// Key names the row.
func (r Row) Key() string {
	if r.Kind == RowLoadMore {
		return "load-more"
	}
	return r.Item.ID
}

// PaneOpts are the options of NewPane. Everything is called on the UI thread.
type PaneOpts struct {
	// Ctx ends the requests when it is done.
	Ctx context.Context
	// Model holds the source controls.
	Model *scm.Model
	// Post runs a function on the UI thread, later; the answers of the providers come through it.
	Post func(func())
	// Now is the time the ages are counted from; time.Now when nil.
	Now func() time.Time
	// Location is the time zone the dates of the details are written in; the local one when nil.
	Location *time.Location
	// Logger logs a provider that failed; slog's default when nil.
	Logger *slog.Logger
	// PageSize is scm.graph.pageSize, PageOnScroll scm.graph.pageOnScroll, Badges scm.graph.badges ("filter" or "all"),
	// ShowIncoming and ShowOutgoing scm.graph.showIncomingChanges and scm.graph.showOutgoingChanges.
	PageSize                   func() int
	PageOnScroll               func() bool
	Badges                     func() string
	ShowIncoming, ShowOutgoing func() bool
	// Visible says whether the view is shown; a hidden view loads nothing.
	Visible func() bool
	// AtTop says whether the list is scrolled to the top (a silent change of references refreshes only then).
	AtTop func() bool
	// Run runs a command of the registry.
	Run func(commandID string, args any)
	// Filters are the filters saved with the window, by the storage key of the source control; FiltersChanged is told when the user
	// changes one.
	Filters        map[string]Filter
	FiltersChanged func(map[string]Filter)
	// AfterFunc calls f after d on the UI thread and returns the function that stops the call; nil runs f at once.
	AfterFunc func(d time.Duration, f func()) (stop func())
}

// repoState is what the pane keeps of a repository: the pages loaded and their lanes.
type repoState struct {
	src *scm.Source
	// loaded says that the first page came; loading that one is under way.
	loaded, loading bool
	// failed is the message of the error of the first page.
	failed string
	// refs are the references the history is of, and opts the layout's options made from them.
	refs []scm.HistoryRef
	opts graph.Options
	// refs of the repository when the pages were read.
	cur, remote, base *scm.HistoryRef
	filter            Filter
	search            string
	// items are the commits loaded, lanes their rows (without the incoming and outgoing changes) and layout the state after the last.
	items  []scm.HistoryItem
	lanes  []graph.Row
	layout graph.State
	byID   map[string]int
	// mergeBase is the common ancestor of the current branch and the one it tracks.
	mergeBase string
	// more says that there is another page; loadingMore that it is being read.
	more, loadingMore bool
	// revealing is the revision the pages are read until, for the button that goes to the current commit.
	revealing string
}

// Pane is the model of the Graph view.
type Pane struct {
	o        PaneOpts
	selected *scm.Source // nil: the active repository
	states   map[*scm.Source]*repoState
	filters  map[string]Filter
	search   string
	outdated bool
	disposed bool
	// gen counts the operations that read pages, so that the answer of a cancelled one is dropped.
	gen    int
	cancel context.CancelFunc
	// rows is the list, built after each change.
	rows    []Row
	message string
	// reveal is the key of the row the list scrolls to once.
	reveal     string
	listeners  []*func()
	stops      []func()
	stopSearch func()
}

// NewPane makes the model of the view. It follows the model of the source controls until Dispose.
func NewPane(o PaneOpts) *Pane {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Location == nil {
		o.Location = time.Local
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	p := &Pane{o: o, states: map[*scm.Source]*repoState{}, filters: map[string]Filter{}}
	for k, v := range o.Filters {
		p.filters[k] = v
	}
	p.stops = append(p.stops, o.Model.Subscribe(func(e scm.Event) { p.o.Post(func() { p.onModel(e) }) }))
	return p
}

// Dispose stops following the model and cancels the requests.
func (p *Pane) Dispose() {
	p.disposed = true
	if p.cancel != nil {
		p.cancel()
	}
	if p.stopSearch != nil {
		p.stopSearch()
	}
	for _, s := range p.stops {
		s()
	}
}

// OnChange calls fn after each change of the rows or the title, on the UI thread, and returns the function that stops it.
func (p *Pane) OnChange(fn func()) (stop func()) {
	f := &fn
	p.listeners = append(p.listeners, f)
	return func() { p.listeners = slices.DeleteFunc(p.listeners, func(o *func()) bool { return o == f }) }
}

func (p *Pane) changed() {
	for _, f := range slices.Clone(p.listeners) {
		(*f)()
	}
}

// Rows are the rows of the list.
func (p *Pane) Rows() []Row { return p.rows }

// Message is the text that stands in place of the list while there are no rows.
func (p *Pane) Message() string { return p.message }

// Outdated says that references changed in the background while the list was scrolled: the title shows "Outdated".
func (p *Pane) Outdated() bool { return p.outdated }

// TakeReveal returns the key of the row to scroll to, once.
func (p *Pane) TakeReveal() string {
	k := p.reveal
	p.reveal = ""
	return k
}

// Source is the repository the view shows; nil when none has a history.
func (p *Pane) Source() *scm.Source {
	if s := p.selected; s != nil && !s.Removed() && s.HistoryProvider() != nil {
		return s
	}
	if active, _ := p.o.Model.Active(); active != nil && active.HistoryProvider() != nil {
		return active
	}
	for _, s := range p.o.Model.Sources() {
		if s.HistoryProvider() != nil {
			return s
		}
	}
	return nil
}

// Sources are the repositories that have a history.
func (p *Pane) Sources() []*scm.Source {
	var out []*scm.Source
	for _, s := range p.o.Model.Sources() {
		if s.HistoryProvider() != nil {
			out = append(out, s)
		}
	}
	return out
}

// Selected is the repository the user picked; nil for Auto.
func (p *Pane) Selected() *scm.Source { return p.selected }

// SelectSource shows the history of a repository, or follows the active one with nil (the repository picker).
func (p *Pane) SelectSource(s *scm.Source) {
	p.selected = s
	p.Refresh()
}

// Filter is the choice of references of the shown repository.
func (p *Pane) Filter() Filter {
	if s := p.Source(); s != nil {
		return p.filters[s.StorageKey()]
	}
	return Filter{}
}

// Filters are the filters to save with the window.
func (p *Pane) Filters() map[string]Filter {
	out := map[string]Filter{}
	for k, v := range p.filters {
		if v.kind() != FilterAuto {
			out[k] = v
		}
	}
	return out
}

// SetFilter chooses the references of the shown repository and reads the history again.
func (p *Pane) SetFilter(f Filter) {
	s := p.Source()
	if s == nil {
		return
	}
	if f.kind() == FilterAuto {
		delete(p.filters, s.StorageKey())
	} else {
		p.filters[s.StorageKey()] = f
	}
	if p.o.FiltersChanged != nil {
		p.o.FiltersChanged(p.Filters())
	}
	p.Refresh()
}

// FilterLabel is the text of the button of the filter in the title, and FilterTooltip its tooltip.
//
// Ported from VS Code: scmHistoryViewPane.ts (SCMHistoryItemRefsActionViewItem)
func (p *Pane) FilterLabel() string {
	f := p.Filter()
	switch f.kind() {
	case FilterAll:
		return "All"
	case FilterRefs:
		if st := p.state(); st != nil && st.loaded && len(st.refs) == 1 {
			return st.refs[0].Name
		}
		if len(f.Refs) == 1 {
			return strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(f.Refs[0], "refs/heads/"), "refs/remotes/"), "refs/tags/")
		}
		return itoa(len(f.Refs)) + " Items"
	}
	return "Auto"
}

// FilterTooltip is the tooltip of the button of the filter.
func (p *Pane) FilterTooltip() string {
	f := p.Filter()
	switch f.kind() {
	case FilterAll:
		return "All history item references"
	case FilterRefs:
		var names []string
		for _, id := range f.Refs {
			names = append(names, strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(id, "refs/heads/"), "refs/remotes/"), "refs/tags/"))
		}
		return strings.Join(names, ", ")
	}
	s := p.Source()
	if s == nil {
		return ""
	}
	var names []string
	for _, r := range []*scm.HistoryRef{s.HistoryRefs().Current, s.HistoryRefs().Remote, s.HistoryRefs().Base} {
		if r != nil {
			names = append(names, r.Name)
		}
	}
	return strings.Join(names, ", ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Search is the text of the search box.
func (p *Pane) Search() string { return p.search }

// SetSearch searches the history for a text (the message, the author or the hash of a commit) after a pause; empty shows the history again.
func (p *Pane) SetSearch(text string) {
	if text == p.search {
		return
	}
	p.search = text
	if p.stopSearch != nil {
		p.stopSearch()
		p.stopSearch = nil
	}
	run := func() { p.stopSearch = nil; p.Refresh() }
	if p.o.AfterFunc == nil || text == "" {
		run()
		return
	}
	p.stopSearch = p.o.AfterFunc(300*time.Millisecond, run)
}

func (p *Pane) pageSize() int {
	n := 50
	if p.o.PageSize != nil {
		n = p.o.PageSize()
	}
	return min(max(n, 1), 1000)
}

func (p *Pane) state() *repoState {
	if s := p.Source(); s != nil {
		return p.states[s]
	}
	return nil
}

// ---- following the model ----

// onModel reacts to a change of the source controls.
func (p *Pane) onModel(e scm.Event) {
	if p.disposed {
		return
	}
	switch e.Kind {
	case scm.SourceRemoved:
		delete(p.states, e.Source)
		if p.selected == e.Source {
			p.selected = nil
		}
		p.Refresh()
	case scm.HistoryChanged:
		st := p.states[e.Source]
		if st != nil && st.loaded && e.Source == p.Source() {
			cur := e.Source.HistoryRefs().Current
			if (cur == nil) != (st.cur == nil) || cur != nil && st.cur != nil && cur.ID != st.cur.ID {
				p.Refresh()
				return
			}
		}
		if st == nil || !st.loaded {
			p.Refresh()
		}
	case scm.HistoryRefsChanged:
		if e.Source != p.Source() {
			delete(p.states, e.Source)
			return
		}
		if e.Refs != nil && e.Refs.Silent {
			if p.o.AtTop != nil && !p.o.AtTop() {
				p.outdated = true
				p.changed()
				return
			}
		}
		p.Refresh()
	case scm.ViewChanged:
		// the active repository may have changed
		if p.Source() != nil && p.states[p.Source()] == nil {
			p.Refresh()
		}
	}
}

// VisibilityChanged is called when the view is shown or hidden: a view that was hidden while the history changed reads it when shown.
func (p *Pane) VisibilityChanged() {
	if p.Source() == nil && p.message == MessageEmpty {
		return // nothing to read, and nothing to tell: the listeners would only ask the view again
	}
	if p.visible() {
		if st := p.state(); st == nil || (!st.loaded && !st.loading) {
			p.Refresh()
		}
	}
}

func (p *Pane) visible() bool { return p.o.Visible == nil || p.o.Visible() }

// Refresh reads the history again from the first page.
func (p *Pane) Refresh() {
	if p.disposed {
		return
	}
	p.outdated = false
	src := p.Source()
	for k := range p.states {
		if k != src {
			delete(p.states, k)
		}
	}
	if src == nil {
		p.gen++
		if p.cancel != nil {
			p.cancel()
		}
		p.rows, p.message = nil, MessageEmpty
		p.changed()
		return
	}
	if !p.visible() {
		delete(p.states, src)
		p.rows, p.message = nil, MessageLoading
		p.changed()
		return
	}
	st := &repoState{src: src, loading: true, search: p.search}
	p.states[src] = st
	p.gen++
	gen := p.gen
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(p.o.Ctx)
	p.cancel = cancel
	if len(p.rows) == 0 {
		p.message = MessageLoading
	}
	refs := src.HistoryRefs()
	filter := p.filters[src.StorageKey()]
	provider := src.HistoryProvider()
	size := p.pageSize()
	search := p.search
	p.changed()
	go func() {
		res := fetchFirst(ctx, provider, refs, filter, search, size)
		p.o.Post(func() {
			if p.disposed || gen != p.gen || p.states[src] != st {
				return
			}
			p.firstPage(st, res, filter)
		})
	}()
}

// firstResult is what is read for the first page.
type firstResult struct {
	refs      []scm.HistoryRef
	reset     bool
	items     []scm.HistoryItem
	mergeBase string
	err       error
}

// fetchFirst reads what the first page needs, off the UI thread: the references of the filter, their commits, and the common
// ancestor of the current branch and the one it tracks.
//
// Ported from VS Code: scmHistoryViewPane.ts (SCMHistoryViewModel.getHistoryItems, _resolveHistoryItemFilter)
func fetchFirst(ctx context.Context, provider scm.HistoryProvider, current scm.HistoryRefs, filter Filter, search string, size int) firstResult {
	var res firstResult
	auto := func() []scm.HistoryRef {
		var out []scm.HistoryRef
		for _, r := range []*scm.HistoryRef{current.Current, current.Remote, current.Base} {
			if r != nil {
				out = append(out, *r)
			}
		}
		return out
	}
	switch filter.kind() {
	case FilterAll:
		res.refs, res.err = provider.Refs(ctx, nil)
	case FilterRefs:
		var all []scm.HistoryRef
		all, res.err = provider.Refs(ctx, filter.Refs)
		for _, r := range all {
			if slices.Contains(filter.Refs, r.ID) {
				res.refs = append(res.refs, r)
			}
		}
		if res.err == nil && len(res.refs) == 0 {
			res.reset = true
			res.refs = auto()
		}
	default:
		res.refs = auto()
	}
	if res.err != nil {
		return res
	}
	// The common ancestor and the page are asked at once: on a long history each of them walks all of it.
	ancestor := make(chan string, 1)
	wantAncestor := search == "" && current.Current != nil && current.Remote != nil
	if wantAncestor {
		go func() {
			base, _ := provider.CommonAncestor(ctx, []string{current.Current.Name, current.Remote.Name})
			ancestor <- base
		}()
	}
	res.items, res.err = provider.Items(ctx, scm.HistoryQuery{Limit: size, RefIDs: revisions(res.refs), Filter: search})
	if wantAncestor {
		res.mergeBase = <-ancestor
	}
	return res
}

// revisions are what the history walk starts from: the commit of each reference, else its id.
func revisions(refs []scm.HistoryRef) []string {
	var out []string
	for _, r := range refs {
		v := r.Revision
		if v == "" {
			v = r.ID
		}
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// firstPage takes the first page that came.
func (p *Pane) firstPage(st *repoState, res firstResult, filter Filter) {
	st.loading = false
	if res.err != nil {
		if p.o.Ctx.Err() == nil {
			p.o.Logger.Warn("the history of a repository cannot be read", "err", res.err)
		}
		st.failed = "Cannot read the history: " + errText(res.err)
		p.rebuild()
		return
	}
	if res.reset {
		delete(p.filters, st.src.StorageKey())
		if p.o.FiltersChanged != nil {
			p.o.FiltersChanged(p.Filters())
		}
	}
	refs := st.src.HistoryRefs()
	st.loaded, st.filter, st.refs = true, filter, res.refs
	st.cur, st.remote, st.base = refs.Current, refs.Remote, refs.Base
	st.mergeBase = res.mergeBase
	st.opts = graph.Options{ColorMap: colorMap(st.refs, refs)}
	if refs.Current != nil {
		st.opts.Current = &graph.RefInfo{ID: refs.Current.ID, Revision: refs.Current.Revision}
	}
	if refs.Remote != nil {
		st.opts.Remote = &graph.RefInfo{ID: refs.Remote.ID, Revision: refs.Remote.Revision}
	}
	if refs.Base != nil {
		st.opts.Base = &graph.RefInfo{ID: refs.Base.ID, Revision: refs.Base.Revision}
	}
	st.byID = map[string]int{}
	p.addPage(st, res.items)
	p.rebuild()
}

func errText(err error) string {
	if e, ok := errors.FromError(err); ok {
		return e.Message
	}
	return err.Error()
}

// colorMap gives the references of the current branch, its remote and its base their colors, and lets the others take the color of the
// lane of their commit.
//
// Ported from VS Code: scmHistoryViewPane.ts (_getGraphColorMap), mainThreadSCM.ts (the colors of the three references)
func colorMap(refs []scm.HistoryRef, cur scm.HistoryRefs) map[string]string {
	m := map[string]string{}
	if cur.Current != nil {
		m[cur.Current.ID] = graph.RefColor
		if cur.Remote != nil {
			m[cur.Remote.ID] = graph.RemoteRefColor
		}
		if cur.Base != nil {
			m[cur.Base.ID] = graph.BaseRefColor
		}
	}
	for _, r := range refs {
		if _, ok := m[r.ID]; !ok {
			m[r.ID] = ""
		}
	}
	return m
}

// addPage lays out a page after the rows there are.
func (p *Pane) addPage(st *repoState, page []scm.HistoryItem) {
	search := st.search != ""
	items := make([]graph.Item, len(page))
	for i, it := range page {
		if search {
			// the commits that match are not a history: they have no lanes
			it.ParentIDs = nil
			page[i] = it
		}
		refs := make([]graph.Ref, len(it.Refs))
		for j, r := range it.Refs {
			refs[j] = graph.Ref{ID: r.ID}
		}
		items[i] = graph.Item{ID: it.ID, ParentIDs: it.ParentIDs, Refs: refs}
		st.byID[it.ID] = len(st.items) + i
	}
	st.items = append(st.items, page...)
	rows, next := graph.Layout(items, st.layout, st.opts)
	st.lanes = append(st.lanes, rows...)
	st.layout = next
	if search {
		st.more = len(page) >= p.pageSize()
	} else {
		st.more = len(next.Output) > 0 && len(page) > 0
	}
}

// ---- paging ----

// LoadMore reads the next page.
func (p *Pane) LoadMore() { p.loadMore("") }

// ScrolledToEnd is called when the last row comes into view: with scm.graph.pageOnScroll the next page is read.
func (p *Pane) ScrolledToEnd() {
	if p.o.PageOnScroll != nil && p.o.PageOnScroll() {
		p.loadMore("")
	}
}

// loadMore reads pages after the loaded ones, until the commit with the revision is among them when one is given.
func (p *Pane) loadMore(until string) {
	st := p.state()
	if st == nil || !st.loaded || !st.more || st.loadingMore || p.disposed {
		return
	}
	st.loadingMore = true
	st.revealing = until
	p.gen++
	gen := p.gen
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(p.o.Ctx)
	p.cancel = cancel
	provider := st.src.HistoryProvider()
	size := p.pageSize()
	q := scm.HistoryQuery{Skip: len(st.items), Limit: size, RefIDs: revisions(st.refs), Filter: st.search}
	p.rebuild()
	go func() {
		var all []scm.HistoryItem
		var err error
		for {
			var page []scm.HistoryItem
			page, err = provider.Items(ctx, q)
			all = append(all, page...)
			if err != nil || until == "" || len(page) < size || slices.ContainsFunc(all, func(it scm.HistoryItem) bool { return it.ID == until }) {
				break
			}
			q.Skip += len(page)
		}
		p.o.Post(func() {
			if p.disposed || gen != p.gen || p.states[st.src] != st {
				return
			}
			st.loadingMore = false
			if err != nil {
				if p.o.Ctx.Err() == nil {
					p.o.Logger.Warn("the next page of the history cannot be read", "err", err)
				}
				st.more = false
			} else {
				p.addPage(st, all)
				if len(all) == 0 {
					st.more = false
				}
			}
			p.rebuild()
			if until != "" {
				p.RevealCurrent()
			}
		})
	}()
}

// RevealCurrent scrolls the list to the commit the current branch is at, reading pages until it is found. It does nothing when the
// current branch is not among the references of the filter.
//
// Ported from VS Code: scmHistoryViewPane.ts (revealCurrentHistoryItem)
func (p *Pane) RevealCurrent() {
	st := p.state()
	if st == nil || !st.loaded || st.cur == nil || st.cur.Revision == "" || !p.CurrentInFilter() {
		return
	}
	if _, ok := st.byID[st.cur.Revision]; ok {
		p.reveal = st.cur.Revision
		p.changed()
		return
	}
	p.loadMore(st.cur.Revision)
}

// CurrentInFilter says whether the reference of the current branch is among the references the graph shows.
func (p *Pane) CurrentInFilter() bool {
	src := p.Source()
	if src == nil || src.HistoryRefs().Current == nil {
		return false
	}
	if f := p.Filter(); f.kind() == FilterRefs {
		return slices.Contains(f.Refs, src.HistoryRefs().Current.ID)
	}
	return true
}

// ---- the rows ----

// rebuild makes the rows from the state and tells the listeners.
func (p *Pane) rebuild() {
	st := p.state()
	switch {
	case st == nil:
		p.rows, p.message = nil, MessageEmpty
	case st.failed != "":
		p.rows, p.message = nil, st.failed
	case !st.loaded:
		p.rows, p.message = nil, MessageLoading
	default:
		p.rows = p.makeRows(st)
		p.message = ""
		if len(st.items) == 0 {
			p.rows = nil
			p.message = MessageEmpty
			if st.search != "" {
				p.message = MessageNoMatch
			}
		}
	}
	p.changed()
}

// makeRows adds the incoming and the outgoing changes to the lanes and turns them into rows.
func (p *Pane) makeRows(st *repoState) []Row {
	lanes := st.lanes
	if st.search == "" {
		in := p.o.ShowIncoming != nil && p.o.ShowIncoming() && st.remote != nil && slices.ContainsFunc(st.refs, func(r scm.HistoryRef) bool { return r.ID == st.remote.ID })
		out := p.o.ShowOutgoing != nil && p.o.ShowOutgoing() && st.cur != nil && slices.ContainsFunc(st.refs, func(r scm.HistoryRef) bool { return r.ID == st.cur.ID })
		lanes = graph.WithChanges(lanes, graph.ChangesOptions{Current: st.opts.Current, Remote: st.opts.Remote, Incoming: in, Outgoing: out, MergeBase: st.mergeBase})
	}
	idLen := 0
	if len(st.items) > 0 {
		idLen = len(st.items[0].DisplayID)
	}
	rows := make([]Row, 0, len(lanes)+1)
	badges := ""
	if p.o.Badges != nil {
		badges = p.o.Badges()
	}
	for _, l := range lanes {
		r := Row{Lane: l}
		switch l.Kind {
		case graph.KindIncoming:
			r.Kind = RowIncoming
			r.Item = scm.HistoryItem{ID: graph.IncomingID, ParentIDs: l.Item.ParentIDs, Subject: "Incoming Changes", DisplayID: strings.Repeat("0", idLen)}
			if st.remote != nil {
				r.Item.Author = st.remote.Name
			}
		case graph.KindOutgoing:
			r.Kind = RowOutgoing
			r.Item = scm.HistoryItem{ID: graph.OutgoingID, ParentIDs: l.Item.ParentIDs, Subject: "Outgoing Changes", DisplayID: strings.Repeat("0", idLen)}
			if st.cur != nil {
				r.Item.Author = st.cur.Name
			}
		default:
			r.Item = st.items[st.byID[l.Item.ID]]
			r.Current = l.Kind == graph.KindHead
			r.Badges = makeBadges(r.Item, l.Item.Refs, badges == "all")
		}
		rows = append(rows, r)
	}
	if st.more && len(rows) > 0 {
		rows = append(rows, Row{Kind: RowLoadMore, Columns: slices.Clone(st.layout.Output)})
	}
	return rows
}

// makeBadges makes the labels of a commit from its references, which the layout sorted and colored: the first one with a color shows
// its name, the others of the same color and icon are one badge with a count, and the ones without a color show only when asked (all).
//
// Ported from VS Code: scmHistoryViewPane.ts (HistoryItemRenderer._renderBadges)
func makeBadges(item scm.HistoryItem, refs []graph.Ref, all bool) []Badge {
	info := map[string]scm.HistoryRef{}
	for _, r := range item.Refs {
		info[r.ID] = r
	}
	rest := slices.Clone(refs)
	var out []Badge
	if len(rest) > 0 && rest[0].Color != "" {
		r := info[rest[0].ID]
		out = append(out, Badge{Icon: r.Icon, Color: rest[0].Color, Name: r.Name, Count: 1, IDs: []string{r.ID}})
		rest = rest[1:]
	}
	// grouped by color, in the order the colors first come, then by icon
	var colors []string
	for _, r := range rest {
		if !slices.Contains(colors, r.Color) {
			colors = append(colors, r.Color)
		}
	}
	for _, color := range colors {
		if color == "" && !all {
			continue
		}
		var icons []string
		for _, r := range rest {
			if r.Color == color && !slices.Contains(icons, info[r.ID].Icon) {
				icons = append(icons, info[r.ID].Icon)
			}
		}
		for _, icon := range icons {
			if icon == "" {
				continue
			}
			b := Badge{Icon: icon, Color: color}
			for _, r := range rest {
				if r.Color == color && info[r.ID].Icon == icon {
					b.IDs = append(b.IDs, r.ID)
				}
			}
			b.Count = len(b.IDs)
			out = append(out, b)
		}
	}
	return out
}

// ---- opening, menus ----

// Open runs the command a row is opened with: the commit, or the range of the incoming and the outgoing changes. The load-more row loads
// the next page (when pageOnScroll is off).
func (p *Pane) Open(r Row) {
	st := p.state()
	if st == nil {
		return
	}
	if r.Kind == RowLoadMore {
		if p.o.PageOnScroll == nil || !p.o.PageOnScroll() {
			p.LoadMore()
		}
		return
	}
	cmd := ""
	if prov := st.src.HistoryProvider(); prov != nil {
		cmd = prov.OpenCommand()
	}
	if cmd == "" || p.o.Run == nil {
		return
	}
	switch r.Kind {
	case RowIncoming, RowOutgoing:
		if st.cur == nil || st.remote == nil || st.mergeBase == "" {
			return
		}
		if r.Kind == RowIncoming {
			p.o.Run(cmd, scm.HistoryRangeArgs(st.src, st.mergeBase, st.remote.Revision, "Incoming Changes - "+st.cur.Name+" ↔ "+st.remote.Name))
		} else {
			p.o.Run(cmd, scm.HistoryRangeArgs(st.src, st.mergeBase, st.cur.Revision, "Outgoing Changes - "+st.remote.Name+" ↔ "+st.cur.Name))
		}
	default:
		p.o.Run(cmd, scm.HistoryMenuArgs(st.src, r.Item, ""))
	}
}

// OpenID opens the row of a commit by its id (the inline action Open Changes runs it).
func (p *Pane) OpenID(id string) {
	for _, r := range p.rows {
		if r.Kind != RowLoadMore && r.Item.ID == id {
			p.Open(r)
			return
		}
	}
}

// ItemArgs are the arguments of a command of the menus of a commit (and of one of its references).
func (p *Pane) ItemArgs(r Row, refID string) []any {
	if src := p.Source(); src != nil {
		return scm.HistoryMenuArgs(src, r.Item, refID)
	}
	return nil
}

// TitleArgs are the arguments of the commands of the title of the view: the source control.
func (p *Pane) TitleArgs() []any {
	if src := p.Source(); src != nil {
		return scm.SourceMenuArgs(src)
	}
	return nil
}

// TitleContext is the context the title of the view is evaluated in: the keys of the repository and of its current references.
//
// Ported from VS Code: scmHistoryViewPane.ts (scmProvider, scmCurrentHistoryItemRefHasRemote, ...HasBase, ...InFilter)
func (p *Pane) TitleContext() map[string]any {
	src := p.Source()
	ctx := map[string]any{"view": ViewID}
	if src == nil {
		return ctx
	}
	for k, v := range src.Context() {
		ctx[k] = v
	}
	refs := src.HistoryRefs()
	ctx["scmCurrentHistoryItemRefHasRemote"] = refs.Remote != nil
	ctx["scmCurrentHistoryItemRefHasBase"] = refs.Base != nil
	ctx["scmCurrentHistoryItemRefInFilter"] = p.CurrentInFilter()
	n := 0
	if st := p.state(); st != nil {
		n = len(st.items)
	}
	ctx["scmHistoryItemCount"] = n
	return ctx
}

// ItemContext is the context the menus of a commit are evaluated in.
func (p *Pane) ItemContext(r Row) map[string]any {
	ctx := p.TitleContext()
	has := false
	if src := p.Source(); src != nil && src.HistoryRefs().Current != nil {
		_, has = refByID(r.Item, src.HistoryRefs().Current.ID)
	}
	ctx["scmHistoryItemHasCurrentHistoryItemRef"] = has
	return ctx
}

func refByID(it scm.HistoryItem, id string) (scm.HistoryRef, bool) {
	for _, r := range it.Refs {
		if r.ID == id {
			return r, true
		}
	}
	return scm.HistoryRef{}, false
}

// Refs lists the references for the picker of the filter, off the UI thread; done is called on the UI thread.
func (p *Pane) Refs(done func([]scm.HistoryRef, error)) {
	src := p.Source()
	if src == nil {
		return
	}
	provider := src.HistoryProvider()
	ctx := p.o.Ctx
	go func() {
		refs, err := provider.Refs(ctx, nil)
		p.o.Post(func() { done(refs, err) })
	}()
}
