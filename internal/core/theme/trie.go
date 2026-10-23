package theme

import (
	"sort"
	"strings"
)

// trieRule is what a node of the trie says: the rule for the scopes that end here, and the
// rules that need parent scopes.
//
// Ported from vscode-textmate: src/theme.ts (ThemeTrieElementRule)
type trieRule struct {
	scopeDepth   int
	parentScopes []string
	fontStyle    FontStyle
	foreground   int
	background   int
	fontFamily   string
	fontSize     float64
	lineHeight   float64
}

func (r *trieRule) clone() *trieRule {
	c := *r
	return &c
}

func cloneRules(rules []*trieRule) []*trieRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]*trieRule, len(rules))
	for i, r := range rules {
		out[i] = r.clone()
	}
	return out
}

// acceptOverwrite merges a rule that applies to the same scope and parents into r.
func (r *trieRule) acceptOverwrite(scopeDepth int, fontStyle FontStyle, foreground, background int, fontFamily string, fontSize, lineHeight float64) {
	if r.scopeDepth <= scopeDepth {
		r.scopeDepth = scopeDepth
	}
	if fontStyle != FontStyleNotSet {
		r.fontStyle = fontStyle
	}
	if foreground != 0 {
		r.foreground = foreground
	}
	if background != 0 {
		r.background = background
	}
	if fontFamily != "" {
		r.fontFamily = fontFamily
	}
	if fontSize != 0 {
		r.fontSize = fontSize
	}
	if lineHeight != 0 {
		r.lineHeight = lineHeight
	}
}

// trieNode is a node of the trie of scope names, one level for each dot-separated part.
//
// Ported from vscode-textmate: src/theme.ts (ThemeTrieElement)
type trieNode struct {
	mainRule             *trieRule
	rulesWithParentScope []*trieRule
	children             map[string]*trieNode
}

func newTrieNode(main *trieRule, withParents []*trieRule) *trieNode {
	return &trieNode{mainRule: main, rulesWithParentScope: withParents, children: map[string]*trieNode{}}
}

// splitScope splits a scope at its first dot.
func splitScope(scope string) (head, tail string) {
	head, tail, _ = strings.Cut(scope, ".")
	return head, tail
}

// bySpecificity orders rules from the most specific: the deeper scope, then the longer parent
// scopes (a child combinator does not count), then the one with more parents.
func bySpecificity(a, b *trieRule) int {
	if a.scopeDepth != b.scopeDepth {
		return b.scopeDepth - a.scopeDepth
	}
	ai, bi := 0, 0
	for {
		if ai < len(a.parentScopes) && a.parentScopes[ai] == ">" {
			ai++
		}
		if bi < len(b.parentScopes) && b.parentScopes[bi] == ">" {
			bi++
		}
		if ai >= len(a.parentScopes) || bi >= len(b.parentScopes) {
			break
		}
		if d := len(b.parentScopes[bi]) - len(a.parentScopes[ai]); d != 0 {
			return d
		}
		ai++
		bi++
	}
	return len(b.parentScopes) - len(a.parentScopes)
}

// match gives the rules for a scope, the most specific first. The rules of the deepest node the
// scope reaches are the ones.
func (n *trieNode) match(scope string) []*trieRule {
	if scope != "" {
		head, tail := splitScope(scope)
		if child, ok := n.children[head]; ok {
			return child.match(tail)
		}
	}
	rules := make([]*trieRule, 0, len(n.rulesWithParentScope)+1)
	rules = append(rules, n.rulesWithParentScope...)
	rules = append(rules, n.mainRule)
	sort.SliceStable(rules, func(i, j int) bool { return bySpecificity(rules[i], rules[j]) < 0 })
	return rules
}

// insert adds a rule at a scope.
func (n *trieNode) insert(scopeDepth int, scope string, parentScopes []string, fontStyle FontStyle, foreground, background int, fontFamily string, fontSize, lineHeight float64) {
	if scope == "" {
		n.insertHere(scopeDepth, parentScopes, fontStyle, foreground, background, fontFamily, fontSize, lineHeight)
		return
	}
	head, tail := splitScope(scope)
	child, ok := n.children[head]
	if !ok {
		child = newTrieNode(n.mainRule.clone(), cloneRules(n.rulesWithParentScope))
		n.children[head] = child
	}
	child.insert(scopeDepth+1, tail, parentScopes, fontStyle, foreground, background, fontFamily, fontSize, lineHeight)
}

func (n *trieNode) insertHere(scopeDepth int, parentScopes []string, fontStyle FontStyle, foreground, background int, fontFamily string, fontSize, lineHeight float64) {
	if parentScopes == nil {
		n.mainRule.acceptOverwrite(scopeDepth, fontStyle, foreground, background, fontFamily, fontSize, lineHeight)
		return
	}
	for _, rule := range n.rulesWithParentScope {
		if compareStringLists(rule.parentScopes, parentScopes, false, false) == 0 {
			rule.acceptOverwrite(scopeDepth, fontStyle, foreground, background, fontFamily, fontSize, lineHeight)
			return
		}
	}
	// A new rule inherits what it does not set from the main rule.
	if fontStyle == FontStyleNotSet {
		fontStyle = n.mainRule.fontStyle
	}
	if foreground == 0 {
		foreground = n.mainRule.foreground
	}
	if background == 0 {
		background = n.mainRule.background
	}
	if fontFamily == "" {
		fontFamily = n.mainRule.fontFamily
	}
	if fontSize == 0 {
		fontSize = n.mainRule.fontSize
	}
	if lineHeight == 0 {
		lineHeight = n.mainRule.lineHeight
	}
	n.rulesWithParentScope = append(n.rulesWithParentScope, &trieRule{
		scopeDepth: scopeDepth, parentScopes: parentScopes, fontStyle: fontStyle, foreground: foreground,
		background: background, fontFamily: fontFamily, fontSize: fontSize, lineHeight: lineHeight,
	})
}
