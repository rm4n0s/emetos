package textmate

// State is where the tokenizer is at the end of a line: the rules that are open, outermost
// first, as a list that shares its tail with the states it was made from. It is immutable and
// safe to keep for as long as needed; the state to start a document from is nil.
//
// Two states are Equal when tokenizing the next line gives the same tokens from both, which is
// how re-tokenization knows it can stop.
type State struct {
	parent  *State
	ruleID  int
	depth   int
	endRule string
	hasEnd  bool
	// captured is true when the begin of the rule matched the line break, so that the next
	// line starts at the anchor position.
	captured bool
	// nameScopes are the scopes up to the name of the rule, contentScopes those inside it (with
	// the content name).
	nameScopes    *ScopeStack
	contentScopes *ScopeStack
	// enterPos and anchorPos are positions in the line being tokenized; they mean something only
	// for the line that is marked by mark, and are -1 for a line that came before.
	enterPos, anchorPos int
	mark                *lineMark
}

// lineMark identifies one tokenization of a line, so that the positions kept in the states it
// pushed are not read as positions of the next line.
type lineMark struct{ _ byte }

// newState makes a state on top of parent.
func newState(parent *State, ruleID, enterPos, anchorPos int, captured bool, endRule string, hasEnd bool, name, content *ScopeStack, mark *lineMark) *State {
	depth := 1
	if parent != nil {
		depth = parent.depth + 1
	}
	return &State{
		parent: parent, ruleID: ruleID, depth: depth, endRule: endRule, hasEnd: hasEnd,
		captured: captured, nameScopes: name, contentScopes: content,
		enterPos: enterPos, anchorPos: anchorPos, mark: mark,
	}
}

// Depth is the number of rules that are open.
func (s *State) Depth() int {
	if s == nil {
		return 0
	}
	return s.depth
}

// Scopes are the scopes at the end of the line, which the text that follows is in.
func (s *State) Scopes() *ScopeStack {
	if s == nil {
		return nil
	}
	return s.contentScopes
}

// Equal tells whether two states are the same: the same rules are open, with the same end
// patterns, in the same scopes.
func (s *State) Equal(o *State) bool {
	if s == o {
		return true
	}
	if s == nil || o == nil {
		return false
	}
	for a, b := s, o; ; a, b = a.parent, b.parent {
		if a == b {
			break
		}
		if a == nil || b == nil || a.depth != b.depth || a.ruleID != b.ruleID || a.hasEnd != b.hasEnd || a.endRule != b.endRule {
			return false
		}
	}
	return s.contentScopes.Equal(o.contentScopes)
}

// enter is the position in the line where the rule was pushed, or -1 when it was pushed in
// another line.
func (s *State) enter(mark *lineMark) int {
	if s.mark != mark {
		return -1
	}
	return s.enterPos
}

// anchor is the anchor position the rule had when it was pushed, or -1 when it was pushed in
// another line.
func (s *State) anchor(mark *lineMark) int {
	if s.mark != mark {
		return -1
	}
	return s.anchorPos
}

// hasSameRuleAs tells whether a rule of s that was pushed at the position of o is the rule of o:
// it is how a grammar that pushes a rule again without advancing is found.
func (s *State) hasSameRuleAs(o *State, mark *lineMark) bool {
	for el := s; el != nil && el.enter(mark) == o.enter(mark); el = el.parent {
		if el.ruleID == o.ruleID {
			return true
		}
	}
	return false
}

// withContentScopes is the state with other scopes inside the rule.
func (s *State) withContentScopes(content *ScopeStack) *State {
	if s.contentScopes == content {
		return s
	}
	c := *s
	c.contentScopes = content
	return &c
}

// withEndRule is the state with the end pattern of the rule resolved.
func (s *State) withEndRule(end string) *State {
	if s.hasEnd && s.endRule == end {
		return s
	}
	c := *s
	c.endRule, c.hasEnd = end, true
	return &c
}
