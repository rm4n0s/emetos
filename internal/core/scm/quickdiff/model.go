package quickdiff

import (
	"context"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Defaults of the model, VS Code's: the recomputing waits 250 ms after the last edit (the spec of the step; VS Code's throttle is 200 ms),
// the diff may take 1000 ms, and a text over 2 MiB has no marks.
const (
	DefaultDelay     = 250 * time.Millisecond
	DefaultTimeLimit = time.Second
	DefaultMaxSize   = 2 << 20
)

// Config is the configuration of a Model.
type Config struct {
	// Post runs fn on the UI thread (the binding's Post). It is required: the results of the background work are handed over with it.
	Post func(fn func())
	// IgnoreTrimWhitespace says whether the white space at the ends of lines is ignored (scm.diffDecorationsIgnoreTrimWhitespace); it is
	// read, on the UI thread, each time the model schedules a comparison (Recompute asks again after the setting changed). Nil does not
	// ignore it.
	IgnoreTrimWhitespace func() bool
	// Delay is how long the model waits after the last edit before it compares; zero is DefaultDelay.
	Delay time.Duration
	// TimeLimit is the time limit of each diff; zero is DefaultTimeLimit.
	TimeLimit time.Duration
	// MaxSize is the size in bytes over which the text, or an original, gets no marks; zero is DefaultMaxSize.
	MaxSize int
	// Now is the clock of the diff's time limit; nil is time.Now.
	Now func() time.Time
}

// ProviderInfo describes the provider of a change.
type ProviderInfo struct {
	// ID is the id the service gave the provider; Label and Kind are the provider's.
	ID    string
	Label string
	Kind  Kind
	// Original is the URI of the document the file is compared with.
	Original string
}

// ProviderChange is a change and the provider it was found by.
type ProviderChange struct {
	Change
	// ProviderID is the id of the provider (ProviderInfo.ID).
	ProviderID string
	// Kind is the kind of the provider.
	Kind Kind
}

// Result is what a Model found for one version of the text.
type Result struct {
	// Version is the version of the document the changes are for.
	Version int
	// Providers are the providers that had an original for the file, the primary ones first.
	Providers []ProviderInfo
	// Changes are the changes of every provider in the order of their place in the text (compare). A secondary change that equals a
	// change of the primary provider is left out (VS Code keeps it for its API only).
	Changes []ProviderChange
}

// Equal tells whether two results have the same providers and changes (the version is not compared).
func (r Result) Equal(o Result) bool {
	return slices.Equal(r.Providers, o.Providers) && slices.Equal(r.Changes, o.Changes)
}

// Model follows one document: it compares its text with the originals the providers of a Service name, off the UI thread, and keeps
// the changes. Its methods are called on the UI thread, except Close, which may be called from any goroutine.
type Model struct {
	doc *document.Document
	svc *Service
	cfg Config

	mu        sync.Mutex
	gen       uint64
	timer     *time.Timer
	cancel    context.CancelFunc
	closed    bool
	uri       string
	stale     bool // the originals must be asked for again
	originals []Original

	// ui thread
	result    Result
	listeners map[int]func()
	nextID    int
	unsubs    []func()
}

// URIOf is the URI of a document as the providers are asked about it: a file is file:///path with the characters of the path that a URI
// does not allow escaped, as the extensions have them (the root of a source control, the files of a group); another document is
// its own String.
func URIOf(u document.URI) string {
	if u.Scheme == document.SchemeFile {
		return (&url.URL{Scheme: "file", Path: u.Path}).String()
	}
	return u.String()
}

// NewModel makes the model of a document and starts comparing it. The document is a file: its URI (URIOf) is what the providers are
// asked about.
func NewModel(svc *Service, doc *document.Document, cfg Config) *Model {
	if cfg.Delay <= 0 {
		cfg.Delay = DefaultDelay
	}
	if cfg.TimeLimit <= 0 {
		cfg.TimeLimit = DefaultTimeLimit
	}
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = DefaultMaxSize
	}
	m := &Model{doc: doc, svc: svc, cfg: cfg, uri: URIOf(doc.URI()), stale: true, listeners: map[int]func(){}}
	m.unsubs = append(m.unsubs,
		doc.Subscribe(func(document.ChangeEvent) { m.schedule(cfg.Delay, false) }),
		doc.OnURIChange(func(_, now document.URI) {
			m.mu.Lock()
			m.uri = URIOf(now)
			m.mu.Unlock()
			m.schedule(cfg.Delay, true)
		}),
		// a provider may be changed, and a Refresh asked, from any goroutine; the document is read on the UI thread only
		svc.Subscribe(func(ev Event) {
			m.mu.Lock()
			covered := ev.Covers(m.uri) && !m.closed
			m.mu.Unlock()
			if covered {
				cfg.Post(func() { m.schedule(cfg.Delay, true) })
			}
		}),
	)
	m.schedule(0, true)
	return m
}

// Result is the changes found last. They are for Result.Version of the document; the editor's marks move with the edits since.
func (m *Model) Result() Result {
	r := m.result
	r.Providers = slices.Clone(r.Providers)
	r.Changes = slices.Clone(r.Changes)
	return r
}

// OnChange calls fn, on the UI thread, after each result that differs from the one before, and returns the function that stops it.
func (m *Model) OnChange(fn func()) (stop func()) {
	m.nextID++
	id := m.nextID
	m.listeners[id] = fn
	return func() { delete(m.listeners, id) }
}

// Recompute compares the text with the originals now, asking the providers again.
func (m *Model) Recompute() { m.schedule(0, true) }

// Close stops the model: nothing more is computed or delivered.
func (m *Model) Close() {
	m.mu.Lock()
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	if m.timer != nil {
		m.timer.Stop()
	}
	unsubs := m.unsubs
	m.unsubs = nil
	m.mu.Unlock()
	for _, u := range unsubs {
		u()
	}
}

func (m *Model) schedule(after time.Duration, stale bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.gen++
	gen := m.gen
	if stale {
		m.stale = true
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.timer != nil {
		m.timer.Stop()
	}
	// the snapshot is taken now, on the goroutine of the document
	snap, version := m.doc.Snapshot(), m.doc.Version()
	ignore := m.cfg.IgnoreTrimWhitespace != nil && m.cfg.IgnoreTrimWhitespace()
	m.timer = time.AfterFunc(after, func() { m.run(gen, snap, version, ignore) })
}

func (m *Model) run(gen uint64, snap buffer.Snapshot, version int, ignore bool) {
	m.mu.Lock()
	if m.closed || m.gen != gen {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ask, uri, originals := m.stale, m.uri, m.originals
	m.stale = false
	m.mu.Unlock()
	defer cancel()
	if ask {
		list, err := m.svc.Originals(ctx, uri)
		if err != nil {
			m.mu.Lock()
			m.stale = true
			m.mu.Unlock()
			return
		}
		originals = list
		m.mu.Lock()
		m.originals = list
		m.mu.Unlock()
	}
	res, err := m.compute(ctx, snap, version, ignore, originals)
	if err != nil {
		return
	}
	m.cfg.Post(func() { m.deliver(gen, res) })
}

// compute compares the text with each original.
func (m *Model) compute(ctx context.Context, snap buffer.Snapshot, version int, ignore bool, originals []Original) (Result, error) {
	res := Result{Version: version}
	if len(originals) == 0 || snap.Len() > m.cfg.MaxSize {
		return res, nil
	}
	var lines []string
	for _, l := range snap.Lines(0) {
		lines = append(lines, l)
	}
	var primary, secondary []ProviderChange
	var primaryLines []string
	for _, o := range originals {
		if len(o.Text) > m.cfg.MaxSize {
			continue
		}
		orig, _ := diff.SplitLines(o.Text)
		if o.Kind == KindPrimary && primaryLines == nil {
			primaryLines = orig
		}
		changes, err := Changes(ctx, orig, lines, DiffOptions{IgnoreTrimWhitespace: ignore, TimeLimit: m.cfg.TimeLimit, Now: m.cfg.Now})
		if err != nil {
			return Result{}, err
		}
		res.Providers = append(res.Providers, ProviderInfo{ID: o.ProviderID, Label: o.Label, Kind: o.Kind, Original: o.URI})
		for _, c := range changes {
			pc := ProviderChange{Change: c, ProviderID: o.ProviderID, Kind: o.Kind}
			if o.Kind == KindPrimary {
				primary = append(primary, pc)
			} else if !duplicates(primary, primaryLines, c, orig) {
				secondary = append(secondary, pc)
			}
		}
	}
	res.Changes = append(primary, secondary...)
	slices.SortStableFunc(res.Changes, func(a, b ProviderChange) int { return Compare(a.Change, b.Change) })
	return res, nil
}

// duplicates tells whether a primary change has the modified lines of the secondary change c, as many original lines and the same
// original text: the secondary change shows nothing the primary one does not (VS Code's check in QuickDiffModel.diff).
func duplicates(primary []ProviderChange, primaryLines []string, c Change, lines []string) bool {
	for _, p := range primary {
		if p.ModifiedStart == c.ModifiedStart && p.ModifiedEnd == c.ModifiedEnd &&
			p.OriginalEnd-p.OriginalStart == c.OriginalEnd-c.OriginalStart && slices.Equal(originalLines(primaryLines, p.Change), originalLines(lines, c)) {
			return true
		}
	}
	return false
}

// originalLines are the lines of the original text a change replaces.
func originalLines(lines []string, c Change) []string {
	if c.OriginalEnd == 0 || c.OriginalStart < 1 || c.OriginalEnd > len(lines) {
		return nil
	}
	return lines[c.OriginalStart-1 : c.OriginalEnd]
}

func (m *Model) deliver(gen uint64, res Result) {
	m.mu.Lock()
	current := !m.closed && m.gen == gen
	m.mu.Unlock()
	if !current {
		return
	}
	changed := !res.Equal(m.result)
	m.result = res
	if !changed {
		return
	}
	ids := make([]int, 0, len(m.listeners))
	for id := range m.listeners {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if fn, ok := m.listeners[id]; ok {
			fn()
		}
	}
}
