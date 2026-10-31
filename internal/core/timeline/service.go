package timeline

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// Command is what happens when an item is opened.
type Command struct {
	// ID is the id of the command; empty is none.
	ID string
	// Title and Tooltip are the texts of the command.
	Title, Tooltip string
	// Args is the JSON-compatible argument the command is run with, or nil.
	Args any
}

// Item is one entry of a timeline (VS Code's TimelineItem, without the accessibility information and the icon URIs).
type Item struct {
	// ID is the handle of the item, unique among the items of its Source.
	ID string
	// Source is the ID of the provider the item is from; the service sets it.
	Source string
	// Label, Description and Detail are the text of the row, the dimmed text after it and the tooltip (Markdown).
	Label, Description, Detail string
	// Timestamp orders the items, newest first.
	Timestamp time.Time
	// Icon is the name of a Codicon.
	Icon string
	// ContextValue is the value of the context key timelineItem for the menus of the item.
	ContextValue string
	// Command is run when the item is opened.
	Command Command
	// Args is the JSON-compatible value the menus' commands get as the "args" of their argument.
	Args any
}

// Options say which items a provider is asked for.
type Options struct {
	// Cursor is the cursor of the previous page, empty for the first.
	Cursor string
	// Limit is the most items wanted, 0 for all.
	Limit int
}

// Page is a provider's answer.
type Page struct {
	// Items are the items of the page.
	Items []Item
	// Cursor starts the next page; empty when there is none.
	Cursor string
}

// Provider provides the history of the files of its schemes. It is called off the UI thread.
type Provider interface {
	// Provide makes a page of the history of the file at uri. ctx is cancelled when the answer is no longer wanted.
	Provide(ctx context.Context, uri string, opts Options) (Page, error)
}

// Descriptor says what a provider is.
type Descriptor struct {
	// ID is the source of the items and is unique in the service.
	ID string
	// Label is the name of the source in the filter.
	Label string
	// Schemes are the URI schemes the provider has a history for; "*" is all.
	Schemes []string
}

// Source is a provider as the filter of the view lists it.
type Source struct {
	ID, Label string
}

// ProvidersChange tells which sources were added and removed.
type ProvidersChange struct {
	Added, Removed []string
}

// Change tells that the timelines of a provider changed.
type Change struct {
	// ID is the provider.
	ID string
	// URIs are the files whose timelines changed; none means every file.
	URIs []string
}

// Result is the end of a Request.
type Result struct {
	// Page is the answer, its items sorted newest first and carrying the Source.
	Page Page
	// Err is the error of the provider, or the context's.
	Err error
}

// Request is a question to a provider that is under way.
type Request struct {
	// Source is the ID of the provider and URI the file asked about.
	Source, URI string
	// Options are what was asked.
	Options Options
	cancel  context.CancelFunc
}

// Cancel cancels the context of the provider's call. The provider may still answer; the caller ignores the answer.
func (r *Request) Cancel() { r.cancel() }

// Registration is a registered provider.
type Registration struct {
	s     *Service
	d     Descriptor
	owner string
	p     Provider
	gone  bool
}

// ID is the ID of the provider.
func (r *Registration) ID() string { return r.d.ID }

// Owner is the id of the extension that registered the provider.
func (r *Registration) Owner() string { return r.owner }

// Fire says that the timelines of the files (all of them with none) may have changed.
func (r *Registration) Fire(uris ...string) {
	s := r.s
	s.mu.Lock()
	gone := r.gone
	subs := s.changed.list()
	s.mu.Unlock()
	if gone {
		return
	}
	c := Change{ID: r.d.ID, URIs: slices.Clone(uris)}
	for _, fn := range subs {
		fn(c)
	}
}

// Dispose removes the provider.
func (r *Registration) Dispose() { r.s.unregister(r) }

// Service is the timeline service. Make one with New.
type Service struct {
	mu        sync.Mutex
	providers []*Registration
	changed   subs[Change]
	providerC subs[ProvidersChange]
	shown     subs[string]
}

// New makes a service with no provider.
func New() *Service { return &Service{} }

// subs is a list of subscribers of one kind of event, by id.
type subs[T any] struct {
	fns  map[int]func(T)
	next int
}

func (l *subs[T]) list() []func(T) {
	out := make([]func(T), 0, len(l.fns))
	ids := make([]int, 0, len(l.fns))
	for id := range l.fns {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		out = append(out, l.fns[id])
	}
	return out
}

func (l *subs[T]) subscribe(mu *sync.Mutex, fn func(T)) func() {
	mu.Lock()
	defer mu.Unlock()
	if l.fns == nil {
		l.fns = map[int]func(T){}
	}
	l.next++
	id := l.next
	l.fns[id] = fn
	return func() {
		mu.Lock()
		defer mu.Unlock()
		delete(l.fns, id)
	}
}

// OnProviders calls fn when a source is added or removed, and returns the function that stops it. A source that is removed and added
// in one event (a provider replaced) is in both lists.
func (s *Service) OnProviders(fn func(ProvidersChange)) (stop func()) {
	return s.providerC.subscribe(&s.mu, fn)
}

// OnChanged calls fn when a provider says its timelines changed.
func (s *Service) OnChanged(fn func(Change)) (stop func()) { return s.changed.subscribe(&s.mu, fn) }

// OnShow calls fn when Show asks for the timeline of a file to be shown.
func (s *Service) OnShow(fn func(uri string)) (stop func()) { return s.shown.subscribe(&s.mu, fn) }

// Show asks the view to show the timeline of a file and to keep it (VS Code's setUri). The window opens the view first.
func (s *Service) Show(uri string) {
	s.mu.Lock()
	subs := s.shown.list()
	s.mu.Unlock()
	for _, fn := range subs {
		fn(uri)
	}
}

// Register adds a provider. It fails with ExtTimelineProviderTaken when a provider has the ID.
func (s *Service) Register(owner string, d Descriptor, p Provider) (*Registration, error) {
	s.mu.Lock()
	for _, r := range s.providers {
		if r.d.ID == d.ID {
			s.mu.Unlock()
			return nil, errors.New("ExtTimelineProviderTaken", "a timeline provider has this ID already", "id", d.ID, "owner", r.owner, "extension", owner)
		}
	}
	r := &Registration{s: s, d: d, owner: owner, p: p}
	s.providers = append(s.providers, r)
	subs := s.providerC.list()
	s.mu.Unlock()
	for _, fn := range subs {
		fn(ProvidersChange{Added: []string{d.ID}})
	}
	return r, nil
}

func (s *Service) unregister(r *Registration) {
	s.mu.Lock()
	if r.gone {
		s.mu.Unlock()
		return
	}
	r.gone = true
	s.providers = slices.DeleteFunc(s.providers, func(o *Registration) bool { return o == r })
	subs := s.providerC.list()
	s.mu.Unlock()
	for _, fn := range subs {
		fn(ProvidersChange{Removed: []string{r.d.ID}})
	}
}

// Sources are the providers, in the order they were registered.
func (s *Service) Sources() []Source {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Source, len(s.providers))
	for i, r := range s.providers {
		out[i] = Source{ID: r.d.ID, Label: r.d.Label}
	}
	return out
}

// HasProvider says whether any provider is registered (the context key timelineHasProvider).
func (s *Service) HasProvider() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.providers) > 0
}

// SchemeOf is the scheme of a URI ("file" for file:///a), "" for a text that is not one.
func SchemeOf(uri string) string {
	scheme, _, ok := strings.Cut(uri, ":")
	if !ok || scheme == "" || strings.ContainsAny(scheme, "/ ") {
		return ""
	}
	return scheme
}

// Request asks a provider for a page of the timeline of the file at uri. The provider runs on a goroutine, which calls done once
// with the answer (sorted, each item with its Source). It returns nil, and calls nothing, when no provider has the ID or the provider has no
// history for the scheme of the URI.
func (s *Service) Request(parent context.Context, source, uri string, opts Options, done func(Result)) *Request {
	s.mu.Lock()
	var reg *Registration
	for _, r := range s.providers {
		if r.d.ID == source {
			reg = r
			break
		}
	}
	s.mu.Unlock()
	if reg == nil {
		return nil
	}
	scheme := SchemeOf(uri)
	if !slices.ContainsFunc(reg.d.Schemes, func(sc string) bool { return sc == "*" || sc == scheme }) {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	req := &Request{Source: source, URI: uri, Options: opts, cancel: cancel}
	go func() {
		defer cancel()
		page, err := reg.p.Provide(ctx, uri, opts)
		if err != nil {
			done(Result{Err: err})
			return
		}
		items := slices.Clone(page.Items)
		for i := range items {
			items[i].Source = source
		}
		SortItems(items)
		page.Items = items
		done(Result{Page: page})
	}()
	return req
}

// SortItems orders items newest first, ties by the ID of the source, the later first, with numbers read as numbers and case ignored
// (VS Code's localeCompare with numeric and base sensitivity); items equal in both keep their order.
func SortItems(items []Item) {
	slices.SortStableFunc(items, func(a, b Item) int {
		if c := b.Timestamp.Compare(a.Timestamp); c != 0 {
			return c
		}
		return compareNatural(b.Source, a.Source)
	})
}

// compareNatural compares two strings ignoring case, with runs of digits compared as numbers.
func compareNatural(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		if isDigit(a[0]) && isDigit(b[0]) {
			na, ra := digits(a)
			nb, rb := digits(b)
			if c := compareDigits(na, nb); c != 0 {
				return c
			}
			a, b = ra, rb
			continue
		}
		if a[0] != b[0] {
			if a[0] < b[0] {
				return -1
			}
			return 1
		}
		a, b = a[1:], b[1:]
	}
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return -1
	}
	return 1
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func digits(s string) (run, rest string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

func compareDigits(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}
