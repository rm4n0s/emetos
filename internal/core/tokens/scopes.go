package tokens

import (
	"sync"

	"github.com/rm4n0s/emetos/internal/core/textmate"
)

// ScopeID names a stack of scopes that tokens carry. Equal stacks have equal ids within one Scopes;
// NoScope is the empty stack.
type ScopeID int32

// NoScope is the id of the empty scope stack, which the tokens of a line that is too long carry.
const NoScope ScopeID = 0

// Scopes interns the scope stacks of the tokens of a document, so that a token is a few numbers
// and the stack of a token is found by its id. It is safe for concurrent use.
type Scopes struct {
	mu       sync.RWMutex
	children map[scopeKey]ScopeID
	parent   []ScopeID
	name     []string
	// names caches the names of the stack of an id, built on demand and never changed.
	names [][]string
}

type scopeKey struct {
	parent ScopeID
	name   string
}

// NewScopes makes an empty table; the stack with no scopes is NoScope.
func NewScopes() *Scopes {
	return &Scopes{
		children: map[scopeKey]ScopeID{},
		parent:   []ScopeID{0},
		name:     []string{""},
		names:    [][]string{nil},
	}
}

// ID gives the id of a scope stack, adding it when it is new.
func (s *Scopes) ID(stack *textmate.ScopeStack) ScopeID {
	if stack == nil {
		return NoScope
	}
	// The stack shares its tail with the stacks it was made from, so the parents are interned
	// first; the table is a trie of the names.
	parent := s.ID(stack.Parent())
	key := scopeKey{parent: parent, name: stack.Top()}
	s.mu.RLock()
	id, ok := s.children[key]
	s.mu.RUnlock()
	if ok {
		return id
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.children[key]; ok {
		return id
	}
	id = ScopeID(len(s.name))
	s.children[key] = id
	s.parent = append(s.parent, parent)
	s.name = append(s.name, key.name)
	s.names = append(s.names, nil)
	return id
}

// Names are the scope names of a stack, outermost first. The slice is shared: it must not be
// changed. An id that is not in the table has no names.
func (s *Scopes) Names(id ScopeID) []string {
	s.mu.RLock()
	if int(id) >= len(s.name) || id < 0 {
		s.mu.RUnlock()
		return nil
	}
	if names := s.names[id]; names != nil || id == NoScope {
		s.mu.RUnlock()
		return names
	}
	s.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if names := s.names[id]; names != nil {
		return names
	}
	var rev []string
	for n := id; n != NoScope; n = s.parent[n] {
		rev = append(rev, s.name[n])
	}
	names := make([]string, len(rev))
	for i, name := range rev {
		names[len(rev)-1-i] = name
	}
	s.names[id] = names
	return names
}

// Len is the number of stacks in the table, counting the empty one.
func (s *Scopes) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.name)
}
