package textmate

import (
	"regexp"
	"sort"
	"strings"
)

// Ported from VS Code: reference/vscode-textmate/src/grammar/basicScopesAttributeProvider.ts
// (vscode-textmate, MIT)

// basicAttributes is what a scope name says about its tokens.
type basicAttributes struct {
	language  int
	tokenType TokenType
}

// embeddedMatcher finds the language of a scope from the scope names of the embedded languages
// of a grammar.
type embeddedMatcher struct {
	languages map[string]int
	names     []string // longest first
}

// newEmbeddedMatcher makes the matcher of a map from scope names to language ids.
func newEmbeddedMatcher(languages map[string]int) *embeddedMatcher {
	m := &embeddedMatcher{languages: languages}
	for name := range languages {
		m.names = append(m.names, name)
	}
	sort.Slice(m.names, func(i, j int) bool { return m.names[i] > m.names[j] })
	return m
}

// match gives the language of a scope: that of the longest scope name of the map that is the
// scope or starts it, followed by a dot. It is 0 when there is none.
func (m *embeddedMatcher) match(scope string) int {
	for _, name := range m.names {
		if scope == name || (strings.HasPrefix(scope, name) && len(scope) > len(name) && scope[len(name)] == '.') {
			return m.languages[name]
		}
	}
	return 0
}

// standardTokenTypeRE finds the scope names that set the standard token type of what is in them.
var standardTokenTypeRE = regexp.MustCompile(`\b(comment|string|regex|meta\.embedded)\b`)

// basicAttributesOf is what the scope name says about the language and the type of the tokens
// in it.
func (g *Grammar) basicAttributesOf(scope string) basicAttributes {
	if v, ok := g.basic.Load(scope); ok {
		return v.(basicAttributes)
	}
	a := basicAttributes{language: g.embedded.match(scope), tokenType: tokenNotSet}
	switch standardTokenTypeRE.FindString(scope) {
	case "comment":
		a.tokenType = TokenComment
	case "string":
		a.tokenType = TokenString
	case "regex":
		a.tokenType = TokenRegEx
	case "meta.embedded":
		a.tokenType = TokenOther
	}
	g.basic.Store(scope, a)
	return a
}

// push adds one scope to the stack, with the language and the token type its name gives.
func (g *Grammar) push(parent *ScopeStack, name string) *ScopeStack {
	depth, language, tokenType := 1, g.initialLanguage, TokenOther
	if parent != nil {
		depth, language, tokenType = parent.depth+1, parent.language, parent.tokenType
	}
	a := g.basicAttributesOf(name)
	if a.language != 0 {
		language = a.language
	}
	if a.tokenType != tokenNotSet {
		tokenType = a.tokenType
	}
	return &ScopeStack{parent: parent, name: name, depth: depth, language: language, tokenType: tokenType}
}

// pushPath adds the scopes of a name; several scopes may be written in one name, separated by
// spaces. A name that is not there (present is false: a rule without a name) adds nothing; an
// empty name is a scope without letters.
func (g *Grammar) pushPath(s *ScopeStack, path string, present bool) *ScopeStack {
	if !present {
		return s
	}
	if !strings.Contains(path, " ") {
		return g.push(s, path)
	}
	for _, name := range strings.Split(path, " ") {
		s = g.push(s, name)
	}
	return s
}

// tokenTypeOverride sets the type of the tokens whose scopes a selector matches.
type tokenTypeOverride struct {
	match     func(scopes []string) bool
	tokenType TokenType
}

// sortedKeys gives the keys of a map in order, so that the selectors of a grammar apply in the
// same order every time.
func sortedKeys(m map[string]TokenType) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// injection is a rule that applies in the scopes its selector matches (SPEC §8.3).
type injection struct {
	selector string
	match    func(scopes []string) bool
	// priority is -1 when the injection goes before the rules of the scope, 1 when it goes after
	// them and 0 for the default, which is before.
	priority int
	ruleID   int
}

// collectInjections gathers the injections of the grammar: those it has itself, and those of
// the grammars the registry says are injected into it, ordered by priority.
func (g *Grammar) collectInjections() []injection {
	var out []injection
	add := func(selector string, desc *RawRule, repository map[string]*RawRule) {
		matchers := parseSelector(selector, nameMatcher)
		ruleID := g.compileRule(desc, repository)
		for _, m := range matchers {
			out = append(out, injection{selector: selector, match: m.match, priority: m.priority, ruleID: ruleID})
		}
	}
	for _, inj := range g.root.raw.Injections {
		add(inj.Selector, inj.Rule, g.root.repository)
	}
	for _, scope := range g.registry.Injections(g.scopeName) {
		in := g.externalGrammar(scope, nil)
		if in == nil || in.raw.InjectionSelector == "" {
			continue
		}
		add(in.raw.InjectionSelector, in.asRule(), in.repository)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].priority < out[j].priority })
	return out
}

// asRule is the grammar seen as a rule, for a grammar that is injected into another: its
// patterns are the rule.
func (in *instance) asRule() *RawRule {
	if in.rule == nil {
		in.rule = &RawRule{Patterns: in.raw.Patterns, HasPatterns: true}
	}
	return in.rule
}
