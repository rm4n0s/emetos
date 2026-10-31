// Package annotations is the store of the line annotations that extensions put after the end of lines (SPEC §10.30 "Blame", step 24.22;
// the Editor Decorations API of the extensions): a muted text, a theme color and a hover for a line of a document. The editors read the
// annotations of their document from it and are told when a document's change.
//
// A set of annotations belongs to an owner (the extension) and a key, and holds the annotations of one document. Setting a key again
// replaces its set whatever its document was. The store keeps no text of the documents: lines are numbers, and an editor that shows
// a line the document no longer has does not draw it.
//
// It is safe for use from several goroutines. Subscribers are called from the goroutine that made the change, after the lock is
// released, and must not call back into the store while they are called from a function that holds it (they do not: no lock is held).
package annotations

import (
	"sort"
	"sync"
)

// Annotation is a text after the end of a line.
type Annotation struct {
	// Line is the line, counted from 0.
	Line int
	// Text is the text drawn after the line.
	Text string
	// Color is the id of the theme color of the text, empty for the default of the editor.
	Color string
	// Hover is the markdown of the hover, empty for none.
	Hover string
	// Link is called with the destination of a link of the hover that was clicked. Nil when the hover has no links to follow.
	Link func(dest string)
}

type set struct {
	owner, key string
	uri        string
	list       []Annotation
}

// Store holds the sets of annotations. The zero value is not ready: make one with New.
type Store struct {
	mu   sync.Mutex
	sets map[string]*set // by owner + "\x00" + key
	subs map[int]func(uris []string)
	next int
}

// New makes an empty store.
func New() *Store {
	return &Store{sets: map[string]*set{}, subs: map[int]func([]string){}}
}

func setID(owner, key string) string { return owner + "\x00" + key }

// Set replaces the set of the owner and key with the annotations of the document at uri; an empty list removes the set. The
// subscribers are told the documents whose annotations changed (the old document of the set and the new one).
func (s *Store) Set(owner, key, uri string, list []Annotation) {
	id := setID(owner, key)
	s.mu.Lock()
	old := s.sets[id]
	var changed []string
	if old != nil {
		changed = append(changed, old.uri)
	}
	if len(list) == 0 {
		delete(s.sets, id)
	} else {
		s.sets[id] = &set{owner: owner, key: key, uri: uri, list: append([]Annotation(nil), list...)}
		if old == nil || old.uri != uri {
			changed = append(changed, uri)
		}
	}
	subs := s.subscribers()
	s.mu.Unlock()
	s.tell(subs, changed)
}

// Clear removes the set of the owner and key.
func (s *Store) Clear(owner, key string) { s.Set(owner, key, "", nil) }

// ClearOwner removes every set of the owner (the end of its activation).
func (s *Store) ClearOwner(owner string) {
	s.mu.Lock()
	var changed []string
	for id, st := range s.sets {
		if st.owner == owner {
			changed = append(changed, st.uri)
			delete(s.sets, id)
		}
	}
	subs := s.subscribers()
	s.mu.Unlock()
	s.tell(subs, changed)
}

// For returns the annotations of the document at uri, sorted by line (the annotations of a line in the order of the owner and key
// of their sets).
func (s *Store) For(uri string) []Annotation {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sets []*set
	for _, st := range s.sets {
		if st.uri == uri {
			sets = append(sets, st)
		}
	}
	sort.Slice(sets, func(i, j int) bool {
		if sets[i].owner != sets[j].owner {
			return sets[i].owner < sets[j].owner
		}
		return sets[i].key < sets[j].key
	})
	var out []Annotation
	for _, st := range sets {
		out = append(out, st.list...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// Has tells whether the owner has a set with the key.
func (s *Store) Has(owner, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.sets[setID(owner, key)]
	return ok
}

// Subscribe calls fn with the URIs of the documents whose annotations changed, after each change. It returns the function that stops
// the calls.
func (s *Store) Subscribe(fn func(uris []string)) (stop func()) {
	s.mu.Lock()
	id := s.next
	s.next++
	s.subs[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
}

// subscribers copies the subscribers; the lock is held.
func (s *Store) subscribers() []func([]string) {
	out := make([]func([]string), 0, len(s.subs))
	ids := make([]int, 0, len(s.subs))
	for id := range s.subs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		out = append(out, s.subs[id])
	}
	return out
}

func (s *Store) tell(subs []func([]string), uris []string) {
	if len(uris) == 0 {
		return
	}
	for _, fn := range subs {
		fn(uris)
	}
}
