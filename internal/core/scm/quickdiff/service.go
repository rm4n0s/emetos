package quickdiff

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/scm"
)

// Kind tells how the changes of a provider are shown (VS Code's QuickDiffProviderKind).
type Kind int

const (
	// KindPrimary is the provider whose changes are the marks of a file, and whose original the format on save of modifications uses.
	KindPrimary Kind = iota
	// KindSecondary is a provider whose changes are drawn in the secondary colors, where the primary provider has none.
	KindSecondary
)

// TextFunc reads the text of the document at a URI (the original that a provider named). It runs off the UI thread.
type TextFunc func(ctx context.Context, uri string) (string, error)

// Provider says what the files below a root are compared with.
type Provider struct {
	// Owner is the extension that registered the provider.
	Owner string
	// Label names the provider ("Git Local Changes (Working Tree)").
	Label string
	// Kind is primary or secondary.
	Kind Kind
	// RootURI is the folder (file:///path) whose files the provider is asked about; empty for every file.
	RootURI string
	// Original returns the URI of the document that holds the original text of the file at uri, "" for none.
	Original func(ctx context.Context, uri string) (string, error)
}

// Original is the text a file is compared with under one provider.
type Original struct {
	// ProviderID, Label and Kind describe the provider (VS Code's QuickDiff).
	ProviderID string
	Label      string
	Kind       Kind
	// URI is the document the text was read from.
	URI string
	// Text is the original text.
	Text string
}

// Event tells the models that the providers changed or that the originals of the files below a root may have.
type Event struct {
	// Root is the folder (file:///path) the change is below; empty for every file.
	Root string
}

// Covers tells whether a file is below the root of the event.
func (e Event) Covers(uri string) bool { return e.Root == "" || within(e.Root, uri) }

// within tells whether the file uri is the folder root or below it.
func within(root, uri string) bool {
	if root == "" {
		return true
	}
	r, p := scm.PathOfURI(root), scm.PathOfURI(uri)
	r = strings.TrimSuffix(r, "/")
	return p == r || strings.HasPrefix(p, r+"/")
}

// Options are the options of a Service.
type Options struct {
	// Texts reads the text of an original. It is required.
	Texts TextFunc
	// Log is told of a provider or a read that failed, at debug level; nil logs nothing.
	Log func(msg string, args ...any)
}

type entry struct {
	id string
	Provider
}

// Service is the registry of the quick diff providers of a window. It is safe for use from several goroutines.
type Service struct {
	opts Options

	mu        sync.Mutex
	providers []*entry
	next      int
	subs      map[int]func(Event)
	nextSub   int
}

// NewService makes a service with no providers.
func NewService(opts Options) *Service {
	return &Service{opts: opts, subs: map[int]func(Event){}}
}

// Add registers a provider and returns its id and the function that removes it. The models are told that files below its root may
// have an original now.
func (s *Service) Add(p Provider) (id string, remove func()) {
	s.mu.Lock()
	s.next++
	e := &entry{id: "qd" + strconv.Itoa(s.next), Provider: p}
	s.providers = append(s.providers, e)
	s.mu.Unlock()
	s.tell(Event{Root: p.RootURI})
	var once sync.Once
	return e.id, func() {
		once.Do(func() {
			s.mu.Lock()
			if at := slices.Index(s.providers, e); at >= 0 {
				s.providers = slices.Delete(s.providers, at, at+1)
			}
			s.mu.Unlock()
			s.tell(Event{Root: p.RootURI})
		})
	}
}

// Refresh tells the models that the originals of the files below the root (every file when empty) may have changed.
func (s *Service) Refresh(rootURI string) { s.tell(Event{Root: rootURI}) }

// Subscribe calls fn, on the goroutine that changed something, after each change of the providers and each Refresh, and returns the
// function that stops it. fn must not block.
func (s *Service) Subscribe(fn func(Event)) (stop func()) {
	s.mu.Lock()
	s.nextSub++
	id := s.nextSub
	s.subs[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
}

func (s *Service) tell(ev Event) {
	s.mu.Lock()
	ids := make([]int, 0, len(s.subs))
	for id := range s.subs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	fns := make([]func(Event), len(ids))
	for i, id := range ids {
		fns[i] = s.subs[id]
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

// Count is the number of providers.
func (s *Service) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.providers)
}

// matching are the providers that hold the file, in the order they are asked: the primary ones first (the order of registration
// within a kind), as VS Code's providerComparer sorts them.
func (s *Service) matching(uri string) []*entry {
	s.mu.Lock()
	var out []*entry
	for _, e := range s.providers {
		if within(e.RootURI, uri) {
			out = append(out, e)
		}
	}
	s.mu.Unlock()
	slices.SortStableFunc(out, func(a, b *entry) int { return int(a.Kind) - int(b.Kind) })
	return out
}

// Originals asks the providers whose root holds the file for its original and reads the text of each answer. A provider that has no
// original for the file, or fails, or whose document cannot be read, is left out. It runs off the UI thread and stops when ctx does;
// the error is the error of ctx in that case.
func (s *Service) Originals(ctx context.Context, uri string) ([]Original, error) {
	return s.originals(ctx, uri, false)
}

// originals is Originals; with primaryOnly it stops at the first primary original and leaves the secondary providers unasked.
func (s *Service) originals(ctx context.Context, uri string, primaryOnly bool) ([]Original, error) {
	var out []Original
	for _, e := range s.matching(uri) {
		if primaryOnly && e.Kind != KindPrimary {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		orig, err := e.Original(ctx, uri)
		if err != nil {
			s.log("a quick diff provider failed", "provider", e.Label, "uri", uri, "err", err)
			continue
		}
		if orig == "" {
			continue
		}
		text, err := s.opts.Texts(ctx, orig)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return nil, cerr
			}
			s.log("the original of a quick diff cannot be read", "provider", e.Label, "original", orig, "err", err)
			continue
		}
		out = append(out, Original{ProviderID: e.id, Label: e.Label, Kind: e.Kind, URI: orig, Text: text})
		if primaryOnly {
			break
		}
	}
	return out, nil
}

// PrimaryOriginal is the text of the original of the first primary provider that has one for the file; false when none has. The format
// on save of modifications compares with it (SPEC §9.12).
func (s *Service) PrimaryOriginal(ctx context.Context, uri string) (string, bool, error) {
	list, err := s.originals(ctx, uri, true)
	if err != nil || len(list) == 0 {
		return "", false, err
	}
	return list[0].Text, true, nil
}

func (s *Service) log(msg string, args ...any) {
	if s.opts.Log != nil {
		s.opts.Log(msg, args...)
	}
}
