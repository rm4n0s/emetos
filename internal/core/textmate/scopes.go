package textmate

import "strings"

// ScopeStack is the scopes a token is in, outermost first, as a list that shares its tail with
// the scopes it was made from. It is immutable. The nil stack has no scopes.
type ScopeStack struct {
	parent *ScopeStack
	name   string
	depth  int
}

// push adds one scope.
func (s *ScopeStack) push(name string) *ScopeStack {
	depth := 1
	if s != nil {
		depth = s.depth + 1
	}
	return &ScopeStack{parent: s, name: name, depth: depth}
}

// pushPath adds the scopes of a name; several scopes may be written in one name, separated by
// spaces. The empty path adds nothing, and the nil path (a rule without a name) as well.
func (s *ScopeStack) pushPath(path string, present bool) *ScopeStack {
	if !present {
		return s
	}
	if !strings.Contains(path, " ") {
		return s.push(path)
	}
	for _, name := range strings.Split(path, " ") {
		s = s.push(name)
	}
	return s
}

// Len is the number of scopes.
func (s *ScopeStack) Len() int {
	if s == nil {
		return 0
	}
	return s.depth
}

// Names are the names of the scopes, outermost first.
func (s *ScopeStack) Names() []string {
	names := make([]string, s.Len())
	for n := s; n != nil; n = n.parent {
		names[n.depth-1] = n.name
	}
	return names
}

// Top is the innermost scope, or "" when there is none.
func (s *ScopeStack) Top() string {
	if s == nil {
		return ""
	}
	return s.name
}

// Equal tells whether two stacks have the same scopes.
func (s *ScopeStack) Equal(o *ScopeStack) bool {
	for {
		if s == o {
			return true
		}
		if s == nil || o == nil || s.depth != o.depth || s.name != o.name {
			return false
		}
		s, o = s.parent, o.parent
	}
}
