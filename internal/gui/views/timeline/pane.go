package timeline

import (
	"context"
	"log/slog"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/timeline"
)

// ViewID is the id of the view in the Explorer container, and ViewName its title.
const (
	ViewID   = "timeline"
	ViewName = "Timeline"
)

// ItemHeight is the height of a row.
const ItemHeight = 22.0

// The messages of the view, VS Code's.
const (
	// MessageNoEditor is shown when the active editor has no file to show the timeline of.
	MessageNoEditor = "The active editor cannot provide timeline information."
	// MessageAllFiltered is shown when every source is filtered out.
	MessageAllFiltered = "All timeline sources have been filtered out."
	// MessageSomeFiltered is shown when no item is left of the sources that are not filtered out.
	MessageSomeFiltered = "No filtered timeline information was provided."
	// MessageNone is shown when no source has an item.
	MessageNone = "No timeline information was provided."
	// MessageNoSCM is appended to the message of an empty view while no source control provider exists.
	MessageNoSCM = "Source Control has not been configured."
)

// LoadMore and Loading are the texts of the row at the end of the list.
const (
	LoadMore = "Load more"
	Loading  = "Loading..."
)

// PaneOpts are the options of NewPane. Everything is called on the UI thread.
type PaneOpts struct {
	// Ctx ends the requests when it is done.
	Ctx context.Context
	// Service is the timeline service the providers are in.
	Service *timeline.Service
	// Post runs a function on the UI thread, later; the answers of the providers come through it.
	Post func(func())
	// Now is the time the ages are counted from; time.Now when nil.
	Now func() time.Time
	// Logger logs a provider that failed; slog's default when nil.
	Logger *slog.Logger
	// PageSize is timeline.pageSize: a number of items, or 0 or less for "choose by the height of the view".
	PageSize func() int
	// PageOnScroll is timeline.pageOnScroll.
	PageOnScroll func() bool
	// RenderHeight is the height of the list in pixels, for the automatic page size.
	RenderHeight func() float64
	// SCMProviders is the number of source control providers (the context key scm.providerCount).
	SCMProviders func() int
	// Visible says whether the view is shown: its container is in the side bar and the view is not folded. A hidden view loads nothing.
	Visible func() bool
	// Excluded are the sources filtered out when the pane is made; ExcludedChanged is told when the user changes the filter.
	Excluded        []string
	ExcludedChanged func(ids []string)
	// Run runs a command of the registry for an item that was opened.
	Run func(commandID string, args any)
	// AfterFunc calls f after d on the UI thread and returns the function that stops the call; the pane's debounce of 500 ms. Nil runs f
	// at once (through Post).
	AfterFunc func(d time.Duration, f func()) (stop func())
}

// Entry is an item of a row with the age that is written at the end of it.
type Entry struct {
	timeline.Item
	// Relative is the age ("3 mins") and RelativeFull the same in full words, for the accessible label.
	Relative, RelativeFull string
	// HideRelative says that the previous row has the same age, which is then not repeated.
	HideRelative bool
}

// Row is a row of the list: an item, or the "Load more" row at its end.
type Row struct {
	// Entry is the item; nil for the "Load more" row.
	*Entry
	// Loading is set on the "Load more" row while a request is under way.
	Loading bool
}

// IsLoadMore says that the row is the one at the end of the list.
func (r Row) IsLoadMore() bool { return r.Entry == nil }

// Key names the row: the source and ID of its item, "load-more" for the last row.
func (r Row) Key() string {
	if r.Entry == nil {
		return "load-more"
	}
	return r.Source + "|" + r.ID
}

// Text is the text of the row: the label of the item, or "Load more" or "Loading..." for the last row.
func (r Row) Text() string {
	if r.Entry != nil {
		return r.Label
	}
	if r.Loading {
		return Loading
	}
	return LoadMore
}

type aggregate struct {
	source       string
	items        []timeline.Item
	cursor       string
	lastRendered int
	stale        bool
}

func newAggregate(source string, page timeline.Page) *aggregate {
	return &aggregate{source: source, items: slices.Clone(page.Items), cursor: page.Cursor, lastRendered: -1}
}

func (a *aggregate) more() bool { return a.cursor != "" }

// add merges a page into the items: an item that is there already is replaced by the new one, and the new items go before the
// others when they are not older than the newest one. It tells whether the items changed.
//
// Ported from VS Code: timelinePane.ts (TimelineAggregate.add)
func (a *aggregate) add(page timeline.Page) bool {
	updated := false
	if len(page.Items) != 0 && len(a.items) != 0 {
		updated = true
		ids := map[string]bool{}
		for _, it := range page.Items {
			ids[it.ID] = true
		}
		a.items = slices.DeleteFunc(a.items, func(it timeline.Item) bool { return ids[it.ID] })
		newest := time.Time{}
		if len(a.items) > 0 {
			newest = a.items[0].Timestamp
		}
		if !page.Items[len(page.Items)-1].Timestamp.Before(newest) {
			a.items = append(slices.Clone(page.Items), a.items...)
		} else {
			a.items = append(a.items, page.Items...)
		}
	} else if len(page.Items) != 0 {
		updated = true
		a.items = append(a.items, page.Items...)
	}
	a.cursor = page.Cursor
	if updated {
		timeline.SortItems(a.items)
	}
	return updated
}

// pending is a request that has not been answered.
type pending struct {
	req    *timeline.Request
	source string
	uri    string
	opts   timeline.Options
}

// Pane is the model of the Timeline view.
type Pane struct {
	opts PaneOpts
	log  *slog.Logger

	uri       string
	active    string
	following bool
	excluded  map[string]bool

	pending   map[string]*pending
	timelines map[string]*aggregate

	maxItemCount  int
	visibleCount  int
	pendingRef    bool
	empty         bool
	message       string
	description   string
	label         string
	rows          []Row
	loadingMore   bool
	wasVisible    bool
	stopDebounce  func()
	listeners     map[int]func()
	nextListener  int
	stopProviders func()
	stopChanged   func()
	stopShow      func()
	disposed      bool
}

// NewPane makes the model. It follows the service until Dispose.
func NewPane(opts PaneOpts) *Pane {
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Visible == nil {
		opts.Visible = func() bool { return true }
	}
	p := &Pane{opts: opts, following: true, excluded: map[string]bool{}, pending: map[string]*pending{}, timelines: map[string]*aggregate{},
		empty: true, listeners: map[int]func(){}, message: MessageNoEditor}
	p.log = opts.Logger
	if p.log == nil {
		p.log = slog.Default()
	}
	for _, id := range opts.Excluded {
		p.excluded[id] = true
	}
	p.maxItemCount = p.pageSize()
	if opts.Service != nil {
		p.stopProviders = opts.Service.OnProviders(func(e timeline.ProvidersChange) { p.post(func() { p.onProvidersChanged(e) }) })
		p.stopChanged = opts.Service.OnChanged(func(c timeline.Change) { p.post(func() { p.onTimelineChanged(c) }) })
		p.stopShow = opts.Service.OnShow(func(uri string) { p.post(func() { p.SetURI(uri) }) })
	}
	return p
}

func (p *Pane) post(f func()) {
	if p.opts.Post == nil {
		f()
		return
	}
	p.opts.Post(func() {
		if !p.disposed {
			f()
		}
	})
}

// Dispose stops following the service and cancels the requests.
func (p *Pane) Dispose() {
	if p.disposed {
		return
	}
	p.disposed = true
	for _, stop := range []func(){p.stopProviders, p.stopChanged, p.stopShow, p.stopDebounce} {
		if stop != nil {
			stop()
		}
	}
	for _, pr := range p.pending {
		pr.req.Cancel()
	}
	p.pending = map[string]*pending{}
}

// OnChange calls fn after the rows, the message or the description changed, and returns the function that stops it.
func (p *Pane) OnChange(fn func()) (stop func()) {
	p.nextListener++
	id := p.nextListener
	p.listeners[id] = fn
	return func() { delete(p.listeners, id) }
}

func (p *Pane) notify() {
	ids := make([]int, 0, len(p.listeners))
	for id := range p.listeners {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if fn, ok := p.listeners[id]; ok {
			fn()
		}
	}
}

// Rows are the rows of the list, as the last refresh made them.
func (p *Pane) Rows() []Row { return p.rows }

// Message is the text shown in place of the list: why it is empty, or that it loads. Empty while there are rows.
func (p *Pane) Message() string { return p.message }

// Description is the text after the title of the view: the name of the file, with "(pinned)" while it is pinned.
func (p *Pane) Description() string { return p.description }

// URI is the file whose timeline is shown.
func (p *Pane) URI() string { return p.uri }

// Following says that the view shows the timeline of the file of the active editor.
func (p *Pane) Following() bool { return p.following }

// Excluded are the IDs of the sources that are filtered out.
func (p *Pane) Excluded() []string {
	out := make([]string, 0, len(p.excluded))
	for id := range p.excluded {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// IsExcluded says that a source is filtered out.
func (p *Pane) IsExcluded(id string) bool { return p.excluded[id] }

func (p *Pane) hasTarget() bool { return p.uri != "" }

func (p *Pane) visible() bool { return p.opts.Visible() }

func (p *Pane) pageOnScroll() bool { return p.opts.PageOnScroll == nil || p.opts.PageOnScroll() }

// pageSize is timeline.pageSize, or, when it is not a number, enough rows to fill the view.
func (p *Pane) pageSize() int {
	if p.opts.PageSize != nil {
		if n := p.opts.PageSize(); n > 0 {
			return n
		}
	}
	h := 0.0
	if p.opts.RenderHeight != nil {
		h = p.opts.RenderHeight()
	}
	extra := -1.0
	if p.pageOnScroll() {
		extra = 1
	}
	return max(20, int(math.Floor(h/ItemHeight+extra)))
}

// Reset loads the timeline again from the first page (timeline.refresh).
func (p *Pane) Reset() { p.loadTimeline(true, nil) }

// SetURI shows the timeline of a file and pins the view to it (files.openTimeline).
func (p *Pane) SetURI(uri string) { p.setURICore(uri, true) }

func (p *Pane) setURICore(uri string, disableFollowing bool) {
	if disableFollowing {
		p.following = false
	}
	p.uri = uri
	p.updateFilename(p.targetLabel())
	p.loadTimeline(true, nil)
	p.notify()
}

// SetFollowing sets whether the view follows the active editor (timeline.toggleFollowActiveEditor); following again shows the file
// of the active editor.
func (p *Pane) SetFollowing(follow bool) {
	if p.following == follow {
		return
	}
	p.following = follow
	p.updateFilename(p.label)
	if follow {
		p.onActiveEditorChanged()
	}
	p.notify()
}

// SetActive tells the pane the file of the active editor ("" for an editor that has no file).
func (p *Pane) SetActive(uri string) {
	p.active = uri
	p.onActiveEditorChanged()
}

// VisibilityChanged tells the pane that its view was shown or folded: a view that is shown loads what it missed.
func (p *Pane) VisibilityChanged() {
	vis := p.visible()
	if vis == p.wasVisible {
		return
	}
	p.wasVisible = vis
	if !vis {
		return
	}
	if !p.following {
		p.setURICore(p.uri, true)
	} else {
		p.onActiveEditorChanged()
	}
}

// Touch writes the ages again (the view got the focus).
func (p *Pane) Touch() { p.refreshDebounced() }

func (p *Pane) updateFilename(label string) {
	p.label = label
	if p.following || label == "" {
		p.description = label
	} else {
		p.description = label + " (pinned)"
	}
}

func (p *Pane) targetLabel() string {
	if p.uri == "" {
		return ""
	}
	return baseName(p.uri)
}

// baseName is the last element of the path of a URI, unescaped.
func baseName(uri string) string {
	path := uri
	if u, err := url.Parse(uri); err == nil && u.Path != "" {
		path = u.Path
	}
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		path = path[i+1:]
	}
	return path
}

// sameURI tells whether two URIs name the same file: the same scheme and path, escapes undone.
func sameURI(a, b string) bool {
	if a == b {
		return true
	}
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return false
	}
	return ua.Scheme == ub.Scheme && ua.Host == ub.Host && ua.Path == ub.Path
}

func (p *Pane) onActiveEditorChanged() {
	if !p.following || !p.visible() {
		return
	}
	uri := p.active
	if uri != "" && sameURI(uri, p.uri) {
		// the uri has not changed: make sure the caches are valid
		for _, s := range p.sources() {
			if p.excluded[s.ID] {
				continue
			}
			tl := p.timelines[s.ID]
			if tl != nil && !tl.stale {
				continue
			}
			if tl != nil {
				p.reloadTimeline(tl)
			} else {
				p.loadTimelineForSource(s.ID, uri, true, nil)
			}
		}
		return
	}
	if uri == "" && p.uri == "" && p.message != "" {
		return
	}
	p.setURICore(uri, false)
}

func (p *Pane) sources() []timeline.Source {
	if p.opts.Service == nil {
		return nil
	}
	return p.opts.Service.Sources()
}

// ToggleSource filters a source out of the view, or back in (the filter menu).
func (p *Pane) ToggleSource(id string) {
	if p.excluded[id] {
		delete(p.excluded, id)
	} else {
		p.excluded[id] = true
	}
	if p.opts.ExcludedChanged != nil {
		p.opts.ExcludedChanged(p.Excluded())
	}
	p.onExcludedChanged()
}

// SetExcluded replaces the filter (the stored value changed).
func (p *Pane) SetExcluded(ids []string) {
	p.excluded = map[string]bool{}
	for _, id := range ids {
		p.excluded[id] = true
	}
	p.onExcludedChanged()
}

func (p *Pane) onExcludedChanged() {
	var missing []string
	for _, s := range p.sources() {
		if !p.excluded[s.ID] && p.timelines[s.ID] == nil {
			missing = append(missing, s.ID)
		}
	}
	if len(missing) != 0 {
		p.loadTimeline(true, missing)
	} else {
		p.refresh()
	}
	p.notify()
}

func (p *Pane) onProvidersChanged(e timeline.ProvidersChange) {
	if len(e.Removed) != 0 {
		for _, id := range e.Removed {
			if pr := p.pending[id]; pr != nil {
				delete(p.pending, id)
				pr.req.Cancel()
			}
			delete(p.timelines, id)
		}
		p.refresh()
	}
	if len(e.Added) != 0 {
		p.loadTimeline(true, e.Added)
	}
}

func (p *Pane) onTimelineChanged(c timeline.Change) {
	if p.uri == "" {
		return
	}
	if len(c.URIs) != 0 && !slices.ContainsFunc(c.URIs, func(u string) bool { return sameURI(u, p.uri) }) {
		return
	}
	tl := p.timelines[c.ID]
	if tl == nil {
		return
	}
	if p.visible() {
		p.reloadTimeline(tl)
	} else {
		tl.stale = true
	}
}

func (p *Pane) clear(cancelPending bool) {
	p.visibleCount = 0
	p.maxItemCount = p.pageSize()
	p.timelines = map[string]*aggregate{}
	if cancelPending {
		for _, pr := range p.pending {
			pr.req.Cancel()
		}
		p.pending = map[string]*pending{}
		if !p.visible() {
			p.rows = nil
			p.empty = true
		}
	}
}

// loadTimeline asks the providers (all of them, or the sources named) for the timeline of the file. A reset forgets what is there.
func (p *Pane) loadTimeline(reset bool, sources []string) {
	if sources == nil {
		if reset {
			p.clear(true)
		}
		if p.empty && p.hasTarget() {
			p.setLoadingMessage()
		}
	}
	if !p.hasTarget() {
		p.clear(false)
		p.refresh()
		return
	}
	if !p.visible() {
		return
	}
	hasPending := false
	ids := sources
	if ids == nil {
		for _, s := range p.sources() {
			ids = append(ids, s.ID)
		}
	}
	for _, id := range ids {
		if p.loadTimelineForSource(id, p.uri, reset, nil) {
			hasPending = true
		}
	}
	if !hasPending {
		p.refresh()
	} else if p.empty {
		p.setLoadingMessage()
	}
}

// loadTimelineForSource asks one provider; it tells whether a request was started.
func (p *Pane) loadTimelineForSource(source, uri string, reset bool, opts *timeline.Options) bool {
	if p.excluded[source] {
		return false
	}
	tl := p.timelines[source]
	// paging with no more items, or enough cached ones to cover the next page: nothing to ask
	if !reset && opts != nil && opts.Cursor != "" && tl != nil && (!tl.more() || len(tl.items) > tl.lastRendered+p.pageSize()) {
		return false
	}
	var o timeline.Options
	if opts == nil {
		if !reset && tl != nil && len(tl.items) > 0 && !tl.more() {
			return false
		}
		if !reset && tl != nil {
			o.Cursor = tl.cursor
		}
		o.Limit = p.pageSize()
	} else {
		o = *opts
	}
	if old := p.pending[source]; old != nil {
		o.Cursor = old.opts.Cursor
		// the limits add up: the new request covers what the old one was asked for
		if o.Limit == 0 || old.opts.Limit == 0 {
			o.Limit = 0
		} else {
			o.Limit += old.opts.Limit
		}
		old.req.Cancel()
		delete(p.pending, source)
	}
	pr := &pending{source: source, uri: uri, opts: o}
	req := p.opts.Service.Request(p.opts.Ctx, source, uri, o, func(res timeline.Result) {
		p.post(func() { p.handleResult(pr, res) })
	})
	if req == nil {
		return false
	}
	pr.req = req
	p.pending[source] = pr
	return true
}

func (p *Pane) reloadTimeline(tl *aggregate) {
	delete(p.timelines, tl.source)
	// ask again for at least all the items there are, to keep what the user sees where it is
	p.loadTimelineForSource(tl.source, p.uri, true, &timeline.Options{Limit: max(len(tl.items), p.pageSize())})
}

func (p *Pane) handleResult(pr *pending, res timeline.Result) {
	// an answer to a request that was cancelled or replaced, or of a provider that went, is not wanted
	if p.pending[pr.source] != pr {
		return
	}
	delete(p.pending, pr.source)
	failed := res.Err != nil
	if failed && !isCancel(res.Err) {
		p.log.Warn("a timeline provider failed", "source", pr.source, "uri", pr.uri, "err", res.Err)
	}
	if failed || !sameURI(pr.uri, p.uri) {
		// VS Code refreshes only when a refresh is owed, which leaves "Loading timeline..." up when the only provider failed
		if len(p.pending) == 0 {
			p.refresh()
		}
		return
	}
	updated := false
	if tl := p.timelines[pr.source]; tl == nil {
		p.timelines[pr.source] = newAggregate(pr.source, res.Page)
		updated = true
	} else {
		updated = tl.add(res.Page)
	}
	switch {
	case updated:
		p.pendingRef = true
		// with rows on the screen and other requests to come, wait a little for them
		if p.visibleCount > 0 && len(p.pending) != 0 {
			p.refreshDebounced()
		} else {
			p.refresh()
		}
	case len(p.pending) == 0:
		p.refresh()
	}
}

func isCancel(err error) bool { return err == context.Canceled || err == context.DeadlineExceeded }

func (p *Pane) refreshDebounced() {
	if p.stopDebounce != nil {
		p.stopDebounce()
		p.stopDebounce = nil
	}
	if p.opts.AfterFunc == nil {
		p.post(p.refresh)
		return
	}
	p.stopDebounce = p.opts.AfterFunc(500*time.Millisecond, func() {
		p.stopDebounce = nil
		if !p.disposed {
			p.refresh()
		}
	})
}

// LoadMore loads the next page, or shows more of the items that are there (the "Load more" row was opened, or scrolled to).
func (p *Pane) LoadMore() {
	if p.loadingMore {
		return
	}
	p.loadingMore = true
	p.makeRows()
	p.notify()
	if len(p.pending) != 0 {
		return
	}
	p.maxItemCount = p.visibleCount + p.pageSize()
	p.loadTimeline(false, nil)
}

// ScrolledToEnd is told that the "Load more" row came into view: the next page is loaded when timeline.pageOnScroll is on.
func (p *Pane) ScrolledToEnd() {
	if p.pageOnScroll() {
		p.LoadMore()
	}
}

func (p *Pane) setLoadingMessage() {
	label := p.targetLabel()
	p.updateFilename(label)
	if label != "" {
		p.message = "Loading timeline for " + label + "..."
	} else {
		p.message = ""
	}
	p.notify()
}

// refresh makes the rows again and sets the message.
func (p *Pane) refresh() {
	if !p.visible() {
		return
	}
	p.loadingMore = false
	p.makeRows()
	p.empty = p.visibleCount == 0
	switch {
	case !p.hasTarget():
		p.updateFilename("")
		p.message = MessageNoEditor
	case p.empty:
		if len(p.pending) != 0 {
			p.setLoadingMessage()
			return
		}
		p.updateFilename(p.targetLabel())
		p.message = p.emptyMessage()
	default:
		p.updateFilename(p.targetLabel())
		p.message = ""
	}
	p.pendingRef = false
	p.notify()
}

// emptyMessage says why there is nothing to show.
func (p *Pane) emptyMessage() string {
	sources := p.sources()
	var msg string
	switch {
	case len(sources) != 0 && !slices.ContainsFunc(sources, func(s timeline.Source) bool { return !p.excluded[s.ID] }):
		msg = MessageAllFiltered
	case slices.ContainsFunc(sources, func(s timeline.Source) bool { return p.excluded[s.ID] }):
		msg = MessageSomeFiltered
	default:
		msg = MessageNone
	}
	if p.opts.SCMProviders == nil || p.opts.SCMProviders() == 0 {
		msg += " " + MessageNoSCM
	}
	return msg
}

// makeRows makes the rows from the timelines: the items of every source that is not filtered out, newest first, up to the number the
// view shows, then the "Load more" row when there are more or a request is under way.
func (p *Pane) makeRows() {
	p.rows = nil
	if !p.hasTarget() || len(p.timelines) == 0 {
		p.visibleCount = 0
		return
	}
	now := p.opts.Now()
	maxCount := p.maxItemCount
	count := 0
	more := false
	var entries []*Entry
	order := p.sourceOrder()
	if len(p.timelines) == 1 {
		var tl *aggregate
		for _, t := range p.timelines {
			tl = t
		}
		tl.lastRendered = -1
		if p.excluded[tl.source] {
			p.visibleCount = 0
			return
		}
		more = tl.more()
		for _, it := range tl.items {
			count++
			if count > maxCount {
				more = true
				break
			}
			entries = append(entries, &Entry{Item: it})
		}
		tl.lastRendered = count - 1
	} else {
		type cursor struct {
			tl   *aggregate
			next int
		}
		var cs []*cursor
		mostRecentEnd := time.Time{}
		for _, id := range order {
			tl := p.timelines[id]
			if tl == nil {
				continue
			}
			tl.lastRendered = -1
			if p.excluded[id] || tl.stale {
				continue
			}
			if tl.more() {
				more = true
				last := tl.items[min(maxCount, len(tl.items)-1)]
				if last.Timestamp.After(mostRecentEnd) {
					mostRecentEnd = last.Timestamp
				}
			}
			cs = append(cs, &cursor{tl: tl})
		}
		for {
			var best *cursor
			for _, c := range cs {
				if c.next >= len(c.tl.items) {
					continue
				}
				if best == nil || !c.tl.items[c.next].Timestamp.Before(best.tl.items[best.next].Timestamp) {
					best = c
				}
			}
			if best == nil {
				break
			}
			best.tl.lastRendered++
			it := best.tl.items[best.next]
			if !it.Timestamp.Before(mostRecentEnd) {
				count++
				if count > maxCount {
					more = true
					break
				}
				entries = append(entries, &Entry{Item: it})
			}
			best.next++
		}
	}
	p.visibleCount = count
	last := ""
	for _, e := range entries {
		e.Relative = timeline.FromNow(now, e.Timestamp, timeline.FromNowOptions{})
		e.RelativeFull = timeline.FromNow(now, e.Timestamp, timeline.FromNowOptions{FullWords: true})
		e.HideRelative = last != "" && e.Relative == last
		last = e.Relative
		p.rows = append(p.rows, Row{Entry: e})
	}
	if count > 0 {
		if more || len(p.pending) != 0 {
			p.rows = append(p.rows, Row{Loading: len(p.pending) != 0 || p.loadingMore})
		}
	}
}

// sourceOrder is the IDs of the timelines in the order of the sources, then the others by ID, so that the merge of several is stable.
func (p *Pane) sourceOrder() []string {
	var ids []string
	for _, s := range p.sources() {
		if p.timelines[s.ID] != nil {
			ids = append(ids, s.ID)
		}
	}
	var rest []string
	for id := range p.timelines {
		if !slices.Contains(ids, id) {
			rest = append(rest, id)
		}
	}
	slices.Sort(rest)
	return append(ids, rest...)
}

// ItemArgs is the argument the commands of an item get (extension.TimelineArgs): the item as {"source", "id", "uri", "contextValue",
// "label", "args"}.
func (p *Pane) ItemArgs(r Row) []any {
	if r.Entry == nil {
		return nil
	}
	return []any{map[string]any{
		extension.TimelineArgSource: r.Source, extension.TimelineArgID: r.ID, extension.TimelineArgURI: p.uri,
		extension.TimelineArgContextValue: r.ContextValue, extension.TimelineArgLabel: r.Label, extension.TimelineArgArgs: r.Args,
	}}
}

// ItemContext is the context keys the menus of an item are evaluated in, over the window's.
func (p *Pane) ItemContext(r Row) map[string]any {
	ctx := map[string]any{
		"view": ViewID, "listMultiSelection": false, "listDoubleSelection": false, "timelineCanCompareSelection": false,
	}
	if r.Entry != nil {
		ctx["timelineItem"] = r.ContextValue
	}
	return ctx
}

// Open opens a row: the "Load more" row loads, an item runs its command with the arguments of the command or, when it has none,
// those of the item.
func (p *Pane) Open(r Row) {
	if r.Entry == nil {
		p.LoadMore()
		return
	}
	c := r.Command
	if c.ID == "" || p.opts.Run == nil {
		return
	}
	args := c.Args
	if args == nil {
		args = p.ItemArgs(r)
	}
	p.opts.Run(c.ID, args)
}
