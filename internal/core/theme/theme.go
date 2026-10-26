package theme

import (
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/textmate"
)

// Style is the style a theme gives to a token: the numbers of its colors in the color map of the
// theme (0 for none) and its font.
//
// Ported from vscode-textmate: src/theme.ts (StyleAttributes)
type Style struct {
	FontStyle  FontStyle
	Foreground int
	Background int
	FontFamily string
	FontSize   float64
	LineHeight float64
}

// TokenTheme gives a style to the scopes of a token, resolving the token colors of a theme as
// vscode-textmate does.
//
// Ported from vscode-textmate: src/theme.ts (Theme)
type TokenTheme struct {
	colors   *ColorMap
	defaults Style
	root     *trieNode

	mu    sync.Mutex
	cache map[string][]*trieRule

	semantic *SemanticTheme
}

// Semantic is the semantic token styling of the theme the token theme was made from; nil for a token theme that was made
// from rules alone.
func (t *TokenTheme) Semantic() *SemanticTheme { return t.semantic }

// NewTokenTheme resolves rules: the rules with no scope make the defaults, the others a trie
// that gives each scope the rule that fits it best. The rules are sorted in place.
//
// Ported from vscode-textmate: src/theme.ts (resolveParsedThemeRules)
func NewTokenTheme(rules []Rule) *TokenTheme {
	sortRules(rules)
	defaults := Style{FontStyle: FontStyleNone}
	foreground, background := "#000000", "#ffffff"
	for len(rules) >= 1 && rules[0].Scope == "" {
		d := rules[0]
		rules = rules[1:]
		if d.FontStyle != FontStyleNotSet {
			defaults.FontStyle = d.FontStyle
		}
		if d.Foreground != "" {
			foreground = d.Foreground
		}
		if d.Background != "" {
			background = d.Background
		}
		// As in vscode-textmate, these three are taken from every default rule, set or not.
		defaults.FontFamily = d.FontFamily
		defaults.FontSize = d.FontSize
		defaults.LineHeight = d.LineHeight
	}
	colors := NewColorMap()
	defaults.Foreground = colors.ID(foreground)
	defaults.Background = colors.ID(background)
	root := newTrieNode(&trieRule{fontStyle: FontStyleNotSet, fontFamily: defaults.FontFamily, fontSize: defaults.FontSize, lineHeight: defaults.LineHeight}, nil)
	for _, r := range rules {
		root.insert(0, r.Scope, r.ParentScopes, r.FontStyle, colors.ID(r.Foreground), colors.ID(r.Background), r.FontFamily, r.FontSize, r.LineHeight)
	}
	return &TokenTheme{colors: colors, defaults: defaults, root: root, cache: map[string][]*trieRule{}}
}

// Colors are the colors of the theme by number; the one at 0 is empty.
func (t *TokenTheme) Colors() []string { return t.colors.Colors() }

// Color gives the color with a number.
func (t *TokenTheme) Color(id int) string { return t.colors.Color(id) }

// Defaults are the style of the text that no rule is for.
func (t *TokenTheme) Defaults() Style { return t.defaults }

// rulesFor gives the rules of the trie for a scope name, the most specific first.
func (t *TokenTheme) rulesFor(scope string) []*trieRule {
	t.mu.Lock()
	defer t.mu.Unlock()
	if rules, ok := t.cache[scope]; ok {
		return rules
	}
	rules := t.root.match(scope)
	t.cache[scope] = rules
	return rules
}

// Match gives the style of a token with the scopes, outermost first. With no scopes it is the
// defaults. ok is false when no rule fits the scopes, which is the case when the theme has a
// rule for the scope only below some parent that the token is not in.
//
// Ported from vscode-textmate: src/theme.ts (Theme.match)
func (t *TokenTheme) Match(scopes []string) (style Style, ok bool) {
	if len(scopes) == 0 {
		return t.defaults, true
	}
	last := len(scopes) - 1
	for _, rule := range t.rulesFor(scopes[last]) {
		if scopesMatchParents(scopes[:last], rule.parentScopes) {
			return Style{rule.fontStyle, rule.foreground, rule.background, rule.fontFamily, rule.fontSize, rule.lineHeight}, true
		}
	}
	return Style{}, false
}

// Resolve gives the style of a token with the scopes, outermost first: the defaults, changed by
// the rule that fits each scope with its ancestors, from the outermost scope in. A rule that sets
// only some of the style leaves the rest as the scopes before it made it, which is how the
// tokenizer of vscode-textmate gives a token its colors.
//
// Ported from vscode-textmate: src/grammar/grammar.ts (AttributedScopeStack.mergeAttributes)
func (t *TokenTheme) Resolve(scopes []string) Style {
	style := t.defaults
	for i := range scopes {
		m, ok := t.Match(scopes[:i+1])
		if !ok {
			continue
		}
		if m.FontStyle != FontStyleNotSet {
			style.FontStyle = m.FontStyle
		}
		if m.Foreground != 0 {
			style.Foreground = m.Foreground
		}
		if m.Background != 0 {
			style.Background = m.Background
		}
		if m.FontFamily != "" {
			style.FontFamily = m.FontFamily
		}
		if m.FontSize != 0 {
			style.FontSize = m.FontSize
		}
		if m.LineHeight != 0 {
			style.LineHeight = m.LineHeight
		}
	}
	return style
}

// ResolveStack is Resolve for the scopes of a token as the tokenizer gives them.
func (t *TokenTheme) ResolveStack(scopes *textmate.ScopeStack) Style {
	return t.Resolve(scopes.Names())
}

// MatchStack is Match for the scopes of a token as the tokenizer gives them.
func (t *TokenTheme) MatchStack(scopes *textmate.ScopeStack) (Style, bool) {
	return t.Match(scopes.Names())
}

// scopesMatchParents tells whether the ancestors of a token, outermost first, satisfy the parent
// scopes of a rule, nearest first. A scope of the rule is found in the ancestors from the nearest
// one up, and ">" before it asks for the very next ancestor.
func scopesMatchParents(ancestors []string, parentScopes []string) bool {
	if len(parentScopes) == 0 {
		return true
	}
	at := len(ancestors) - 1 // the ancestor being looked at
	for i := 0; i < len(parentScopes); i++ {
		pattern := parentScopes[i]
		mustMatch := false
		if pattern == ">" {
			if i == len(parentScopes)-1 {
				return false
			}
			i++
			pattern = parentScopes[i]
			mustMatch = true
		}
		for at >= 0 {
			if scopeMatches(ancestors[at], pattern) {
				break
			}
			if mustMatch {
				return false
			}
			at--
		}
		if at < 0 {
			return false
		}
		at--
	}
	return true
}

// scopeMatches tells whether a scope name is the pattern or starts with it and a dot.
func scopeMatches(name, pattern string) bool {
	return pattern == name || (strings.HasPrefix(name, pattern) && len(name) > len(pattern) && name[len(pattern)] == '.')
}
