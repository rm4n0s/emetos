package decorations

import (
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// Options configure a Service; the zero value is what Emetos runs.
type Options struct {
	// MaxEntries bounds the answers kept (one for each URI and provider, "nothing" included). 250,000 by default.
	MaxEntries int
	// SlowAfter is the time an answer may take before it is logged as slow: 1 ms by default, a negative value never.
	SlowAfter time.Duration
	// SlowLimit is the number of slow answers after which the provider is not asked for URIs it has not answered: 20 by default.
	SlowLimit int
	// Logger logs the slow providers and the full cache; slog's default logger when nil.
	Logger *slog.Logger
	// Now gives the time that the answers are timed with; time.Now when nil.
	Now func() time.Time
}

// Default values of Options.
const (
	defaultMaxEntries = 250_000
	defaultSlowAfter  = time.Millisecond
	defaultSlowLimit  = 20
)

// Service is the file decorations service. Make one with New.
type Service struct {
	opts Options
	log  *slog.Logger

	mu sync.Mutex
	// roots are the trees of the URIs by the scheme and authority they start with.
	roots map[string]*node
	// providers are the registered providers, the newest first.
	providers []*Registration
	// entries is the number of answers in the trees.
	entries int
	// full says that the cache was found full and the log has said so.
	full bool
	// version counts the changes announced.
	version uint64
	// listeners are the functions OnChange registered, by id.
	listeners map[int]func(Change)
	nextID    int

	// deferred are the functions Defer was given, run when the lock is released.
	deferMu  sync.Mutex
	deferred []func()
}

// New makes a service.
func New(opts Options) *Service {
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = defaultMaxEntries
	}
	if opts.SlowAfter == 0 {
		opts.SlowAfter = defaultSlowAfter
	}
	if opts.SlowLimit <= 0 {
		opts.SlowLimit = defaultSlowLimit
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{opts: opts, log: log.With("service", "decorations"), roots: map[string]*node{}, listeners: map[int]func(Change){}}
}

// slot is the answer of a provider for the URI of a node.
type slot struct {
	reg  *Registration
	data Data
	has  bool
}

// node is a place in the tree of the URIs.
type node struct {
	parent   *node
	name     string
	children map[string]*node
	slots    []slot
	// agg is what the bubbling answers at and below the node are; nil when it has to be computed again. When a node has one, so
	// have all the nodes below it.
	agg *aggregate
}

// aggItem is the part of an answer that a folder below which it bubbled shows.
type aggItem struct {
	weight int
	color  string
}

// aggregate is what the bubbling answers at and below a node add to the folder above it.
type aggregate struct {
	// items are the distinct (weight, color) pairs, the highest weight first.
	items  []aggItem
	strike bool
	any    bool
}

// noAggregate is the aggregate of a node with no bubbling answers at or below it.
var noAggregate = &aggregate{}

func (n *node) slotOf(reg *Registration) *slot {
	for i := range n.slots {
		if n.slots[i].reg == reg {
			return &n.slots[i]
		}
	}
	return nil
}

// invalidate marks the aggregates of the node and of the nodes above it as stale.
func (n *node) invalidate() {
	for ; n != nil && n.agg != nil; n = n.parent {
		n.agg = nil
	}
}

// find is the node of the URI; with create it is made (with the nodes above it) when it is not there.
func (s *Service) find(root string, segs []string, create bool) *node {
	n := s.roots[root]
	if n == nil {
		if !create {
			return nil
		}
		n = &node{name: root}
		s.roots[root] = n
	}
	for _, seg := range segs {
		c := n.children[seg]
		if c == nil {
			if !create {
				return nil
			}
			c = &node{parent: n, name: seg}
			if n.children == nil {
				n.children = map[string]*node{}
			}
			n.children[seg] = c
		}
		n = c
	}
	return n
}

// prune removes the node and the empty nodes above it when nothing is kept in them.
func (s *Service) prune(n *node) {
	for n != nil && len(n.slots) == 0 && len(n.children) == 0 {
		p := n.parent
		if p == nil {
			delete(s.roots, n.name)
			return
		}
		delete(p.children, n.name)
		n = p
	}
}

// Defer queues fn to run when the call of the service that is going on ends and its lock is released. A provider uses it from
// Decoration, where it must not call the service, for what has to happen once the service is free again (taking an extension off).
func (s *Service) Defer(fn func()) {
	s.deferMu.Lock()
	s.deferred = append(s.deferred, fn)
	s.deferMu.Unlock()
}

// unlock releases the lock and runs what Defer queued.
func (s *Service) unlock() {
	s.mu.Unlock()
	s.deferMu.Lock()
	fns := s.deferred
	s.deferred = nil
	s.deferMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Registration is a registered provider.
type Registration struct {
	s     *Service
	p     Provider
	label string

	slow      int
	suspended bool
	disposed  bool
}

// Register adds a provider; label names it in the log. The views are told that everything might have changed. It fails with
// DecorationsProviderMissing for a nil provider.
func (s *Service) Register(label string, p Provider) (*Registration, error) {
	if p == nil {
		return nil, errors.New("DecorationsProviderMissing", "a decorations provider is needed", "label", label)
	}
	r := &Registration{s: s, p: p, label: label}
	s.mu.Lock()
	s.providers = slices.Insert(s.providers, 0, r)
	ev := s.announceLocked(Change{all: true})
	s.unlock()
	ev()
	return r, nil
}

// Dispose removes the provider and every answer it gave; the views are told which URIs changed.
func (r *Registration) Dispose() {
	s := r.s
	s.mu.Lock()
	if r.disposed {
		s.unlock()
		return
	}
	r.disposed = true
	s.providers = slices.DeleteFunc(s.providers, func(o *Registration) bool { return o == r })
	keys := s.removeAllLocked(r)
	ev := func() {}
	if len(keys) > 0 {
		ev = s.announceLocked(changeOfKeys(keys))
	}
	s.unlock()
	ev()
}

// Fire says that the decorations of the URIs may have changed: the provider is asked for them again now, and the views are told
// of those that did change, and of the folders above them. With no URI everything the provider said is forgotten (it is asked
// again for what the views show); a provider that was left out for being slow is asked again.
func (r *Registration) Fire(uris ...string) {
	s := r.s
	s.mu.Lock()
	if r.disposed {
		s.unlock()
		return
	}
	r.slow, r.suspended = 0, false
	var ch Change
	if len(uris) == 0 {
		keys := s.removeAllLocked(r)
		if len(keys) == 0 {
			s.unlock()
			return
		}
		ch = changeOfKeys(keys)
	} else {
		keys := map[string]struct{}{}
		for _, uri := range uris {
			root, segs := splitURI(uri)
			n := s.find(root, segs, true)
			if s.refetchLocked(n, r, uri) {
				addKey(keys, keyOf(root, segs))
			}
			s.prune(n)
		}
		if len(keys) == 0 {
			s.unlock()
			return
		}
		ch = Change{keys: keys}
	}
	ev := s.announceLocked(ch)
	s.unlock()
	ev()
}

// addKey adds a key and the keys of the places above it.
func addKey(keys map[string]struct{}, key string) {
	for {
		if _, ok := keys[key]; ok {
			return
		}
		keys[key] = struct{}{}
		i := strings.LastIndexByte(key, 0)
		if i < 0 {
			return
		}
		key = key[:i]
	}
}

// changeOfKeys is the change of a set of keys, which get their parents added.
func changeOfKeys(keys []string) Change {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		addKey(m, k)
	}
	return Change{keys: m}
}

// removeAllLocked forgets every answer of a provider and returns the keys of the URIs it had a decoration for.
func (s *Service) removeAllLocked(r *Registration) []string {
	var keys []string
	var walk func(n *node, key string)
	walk = func(n *node, key string) {
		for i := range n.slots {
			if n.slots[i].reg != r {
				continue
			}
			if n.slots[i].has {
				keys = append(keys, key)
			}
			n.slots = slices.Delete(n.slots, i, i+1)
			s.entries--
			n.invalidate()
			break
		}
		for name, c := range n.children {
			walk(c, key+"\x00"+name)
		}
		if len(n.slots) == 0 && len(n.children) == 0 && n.parent != nil {
			delete(n.parent.children, n.name)
		}
	}
	for root, n := range s.roots {
		walk(n, root)
		if len(n.slots) == 0 && len(n.children) == 0 {
			delete(s.roots, root)
		}
	}
	return keys
}

// refetchLocked asks the provider for the URI again and keeps the answer; it reports whether the answer is not what was kept.
func (s *Service) refetchLocked(n *node, r *Registration, uri string) bool {
	old := n.slotOf(r)
	data, has := s.askLocked(r, uri)
	if old != nil {
		if old.has == has && old.data == data {
			return false
		}
		old.data, old.has = data, has
		n.invalidate()
		return true
	}
	if !s.storeLocked(n, slot{reg: r, data: data, has: has}) {
		return has
	}
	if has {
		n.invalidate()
	}
	return has
}

// askLocked asks the provider, times the answer and leaves the provider out when it is slow too often.
func (s *Service) askLocked(r *Registration, uri string) (Data, bool) {
	start := s.opts.Now()
	data, has := r.p.Decoration(uri)
	if s.opts.SlowAfter < 0 {
		return data, has
	}
	if took := s.opts.Now().Sub(start); took > s.opts.SlowAfter {
		r.slow++
		s.log.Warn("a decorations provider answered slowly", "provider", r.label, "uri", uri, "took", took, "slowAnswers", r.slow)
		if r.slow >= s.opts.SlowLimit && !r.suspended {
			r.suspended = true
			s.log.Warn("a decorations provider is not asked for new files any more", "provider", r.label, "slowAnswers", r.slow)
		}
	}
	return data, has
}

// storeLocked adds an answer to a node, making room when the cache is full; it reports whether the answer was kept.
func (s *Service) storeLocked(n *node, sl slot) bool {
	if s.entries >= s.opts.MaxEntries {
		s.evictLocked()
		if s.entries >= s.opts.MaxEntries {
			if !s.full {
				s.full = true
				s.log.Warn("the file decorations cache is full; answers are not kept any more", "max", s.opts.MaxEntries)
			}
			return false
		}
	}
	n.slots = append(n.slots, sl)
	s.entries++
	return true
}

// evictLocked drops the "nothing" answers, which are cheap to ask again, and the nodes that are empty then.
func (s *Service) evictLocked() {
	var walk func(n *node) bool
	walk = func(n *node) (empty bool) {
		n.slots = slices.DeleteFunc(n.slots, func(sl slot) bool {
			if !sl.has {
				s.entries--
				return true
			}
			return false
		})
		for name, c := range n.children {
			if walk(c) {
				delete(n.children, name)
			}
		}
		return len(n.slots) == 0 && len(n.children) == 0
	}
	for root, n := range s.roots {
		if walk(n) {
			delete(s.roots, root)
		}
	}
}

// Get is the decoration of the URI, merged from the answers of all the providers; ok is false when none says anything. A provider
// that was not asked for the URI yet is asked now. With includeChildren (for a folder) the marks that bubbled up from the files
// below it are merged too, and a folder with such marks gets a Decoration with Bubble set.
func (s *Service) Get(uri string, includeChildren bool) (Decoration, bool) {
	root, segs := splitURI(uri)
	s.mu.Lock()
	n := s.find(root, segs, true)
	var own []Data
	var late bool
	for _, r := range s.providers {
		sl := n.slotOf(r)
		if sl == nil {
			if r.suspended {
				continue
			}
			data, has := s.askLocked(r, uri)
			if !s.storeLocked(n, slot{reg: r, data: data, has: has}) {
				if has {
					own = append(own, data)
				}
				continue
			}
			sl = &n.slots[len(n.slots)-1]
			if has {
				n.invalidate()
				late = late || data.Bubble
			}
		}
		if sl.has {
			own = append(own, sl.data)
		}
	}
	var agg *aggregate
	if includeChildren && len(n.children) > 0 {
		agg = s.childrenAggregateLocked(n)
	}
	s.prune(n)
	var ev func()
	if late {
		// a file that bubbles was found: the folders above it may show it now
		keys := map[string]struct{}{}
		addKey(keys, keyOf(root, segs))
		ev = s.announceLocked(Change{keys: keys})
	}
	s.unlock()
	if ev != nil {
		ev()
	}
	return merge(own, agg)
}

// childrenAggregateLocked merges the aggregates of the children of a node.
func (s *Service) childrenAggregateLocked(n *node) *aggregate {
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	sort.Strings(names)
	var out aggregate
	for _, name := range names {
		a := s.aggregateLocked(n.children[name])
		if !a.any {
			continue
		}
		out.any = true
		out.strike = out.strike || a.strike
		out.items = append(out.items, a.items...)
	}
	if !out.any {
		return noAggregate
	}
	out.items = normalize(out.items)
	return &out
}

// aggregateLocked is the aggregate of a node: its own bubbling answers and those of the nodes below it.
func (s *Service) aggregateLocked(n *node) *aggregate {
	if n.agg != nil {
		return n.agg
	}
	var out aggregate
	for _, r := range s.providers {
		if sl := n.slotOf(r); sl != nil && sl.has && sl.data.Bubble {
			out.any = true
			out.strike = out.strike || sl.data.Strikethrough
			out.items = append(out.items, aggItem{weight: sl.data.Weight, color: sl.data.Color})
		}
	}
	if len(n.children) > 0 {
		if below := s.childrenAggregateLocked(n); below.any {
			out.any = true
			out.strike = out.strike || below.strike
			out.items = append(out.items, below.items...)
		}
	}
	switch {
	case !out.any:
		n.agg = noAggregate
	default:
		out.items = normalize(out.items)
		n.agg = &out
	}
	return n.agg
}

// normalize drops the repeated items (the first of them stays) and orders the rest by weight, the highest first and equal weights as
// they came.
func normalize(items []aggItem) []aggItem {
	out := items[:0]
	for _, it := range items {
		if !slices.Contains(out, it) {
			out = append(out, it)
		}
	}
	slices.SortStableFunc(out, func(a, b aggItem) int { return b.weight - a.weight })
	return out
}

// merge builds the Decoration of the answers for a URI and of what bubbled up from below it.
func merge(own []Data, below *aggregate) (Decoration, bool) {
	hasBelow := below != nil && below.any
	if len(own) == 0 && !hasBelow {
		return Decoration{}, false
	}
	items := make([]aggItem, 0, len(own))
	strike := false
	for _, d := range own {
		items = append(items, aggItem{weight: d.Weight, color: d.Color})
		strike = strike || d.Strikethrough
	}
	if hasBelow {
		items = append(items, below.items...)
		strike = strike || below.strike
	}
	slices.SortStableFunc(items, func(a, b aggItem) int { return b.weight - a.weight })
	var dec Decoration
	dec.Strikethrough = strike
	for _, it := range items {
		if it.color != "" && !slices.Contains(dec.Colors, it.color) {
			dec.Colors = append(dec.Colors, it.color)
		}
	}
	if hasBelow {
		dec.Bubble, dec.Tooltip = true, BubbleTooltip
		return dec, true
	}
	sorted := slices.Clone(own)
	slices.SortStableFunc(sorted, func(a, b Data) int { return b.Weight - a.Weight })
	var letters, tooltips []string
	for _, d := range sorted {
		if d.Icon != "" {
			dec.Icon = d.Icon
			letters = nil
			break
		}
		if d.Letter != "" {
			letters = append(letters, d.Letter)
		}
	}
	dec.Badge = strings.Join(letters, ", ")
	for _, d := range sorted {
		if strings.TrimSpace(d.Tooltip) != "" && !slices.Contains(tooltips, d.Tooltip) {
			tooltips = append(tooltips, d.Tooltip)
		}
	}
	dec.Tooltip = strings.Join(tooltips, TooltipSeparator)
	return dec, true
}

// Len is the number of answers kept.
func (s *Service) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries
}

// Version counts the changes that were announced; it is a cheap way for a view to see that it has to read again.
func (s *Service) Version() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version
}

// OnChange calls fn each time decorations changed, from the goroutine that changed them, and returns the function that stops it.
func (s *Service) OnChange(fn func(Change)) (stop func()) {
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.listeners[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
}

// announceLocked counts a change and returns the function that tells the listeners, which the caller runs once the lock is released.
func (s *Service) announceLocked(ch Change) func() {
	s.version++
	fns := make([]func(Change), 0, len(s.listeners))
	ids := make([]int, 0, len(s.listeners))
	for id := range s.listeners {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		fns = append(fns, s.listeners[id])
	}
	return func() {
		for _, fn := range fns {
			fn(ch)
		}
	}
}

// Change is what a change of the decorations touched (VS Code's IResourceDecorationChangeEvent).
type Change struct {
	all  bool
	keys map[string]struct{}
}

// All says that anything may have changed.
func (c Change) All() bool { return c.all }

// Affects says whether the decoration of the URI may be another: the URI itself changed or a file below it did (the folders
// show what their files bubble).
func (c Change) Affects(uri string) bool {
	if c.all {
		return true
	}
	root, segs := splitURI(uri)
	_, ok := c.keys[keyOf(root, segs)]
	return ok
}
