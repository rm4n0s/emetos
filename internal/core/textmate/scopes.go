package textmate

// ScopeStack is the scopes a token is in, outermost first, as a list that shares its tail with
// the scopes it was made from. It is immutable. The nil stack has no scopes.
type ScopeStack struct {
	parent *ScopeStack
	name   string
	depth  int
	// language and tokenType are what the scopes up to this one say about the token: the
	// language of the innermost embedded language scope, and the standard token type of the
	// innermost scope that sets one.
	language  int
	tokenType TokenType
}

// TokenType is the standard type of a token, which brackets, auto-closing and comments use to
// tell code from comments and strings.
type TokenType int

// The standard token types, with the values of VS Code's StandardTokenType.
const (
	// TokenOther is a token that is none of the others.
	TokenOther TokenType = 0
	// TokenComment is a token in a comment.
	TokenComment TokenType = 1
	// TokenString is a token in a string.
	TokenString TokenType = 2
	// TokenRegEx is a token in a regular expression.
	TokenRegEx TokenType = 3
	// tokenNotSet is a scope that says nothing about the type: its parent's is kept.
	tokenNotSet TokenType = 8
)

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
