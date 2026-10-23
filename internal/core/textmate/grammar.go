package textmate

import (
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/errors"
)

// Ported from VS Code: reference/vscode-textmate/src/grammar/grammar.ts and rule.ts
// (vscode-textmate, MIT)

// Registry finds the grammars that other grammars include by their scope names.
type Registry interface {
	// Grammar returns the grammar with the scope name, or nil when there is none.
	Grammar(scopeName string) *RawGrammar
}

// DefaultMaxLineBytes is the length from which a line is not tokenized (SPEC §8.3).
const DefaultMaxLineBytes = 20000

// GrammarOptions are the options of NewGrammar.
type GrammarOptions struct {
	// MaxLineBytes is the length, in bytes, from which a line is one token and not tokenized; the
	// default is DefaultMaxLineBytes.
	MaxLineBytes int
	// Cache compiles the patterns; a cache of the grammar is made when there is none.
	Cache *regex.Cache
}

// instance is a raw grammar made ready to be compiled: its repository has the rules $self and
// $base.
type instance struct {
	raw        *RawGrammar
	repository map[string]*RawRule
}

// Grammar is a grammar ready to tokenize lines. It is safe for concurrent use: the rules are
// compiled when the first line is tokenized and are not changed after that.
type Grammar struct {
	scopeName    string
	registry     Registry
	cache        *regex.Cache
	maxLineBytes int
	root         *instance

	once     sync.Once
	rootID   int
	rules    []*rule
	ids      map[*RawRule]int
	included map[string]*instance

	problemsMu sync.Mutex
	problems   []error
	seen       map[string]bool
}

// NewGrammar makes the grammar with the scope name from the registry. The error has the tag
// GrammarNotFound when the registry has no such grammar.
func NewGrammar(scopeName string, registry Registry, opts GrammarOptions) (*Grammar, error) {
	raw := registry.Grammar(scopeName)
	if raw == nil {
		return nil, errors.New("GrammarNotFound", "no grammar has this scope name", "scope", scopeName)
	}
	g := &Grammar{
		scopeName: scopeName, registry: registry, cache: opts.Cache, maxLineBytes: opts.MaxLineBytes,
		root: initGrammar(raw, nil), ids: map[*RawRule]int{}, included: map[string]*instance{},
		rules: []*rule{nil}, seen: map[string]bool{},
	}
	if g.cache == nil {
		g.cache = regex.NewCache(4096)
	}
	if g.maxLineBytes <= 0 {
		g.maxLineBytes = DefaultMaxLineBytes
	}
	return g, nil
}

// ScopeName is the scope name of the grammar.
func (g *Grammar) ScopeName() string { return g.scopeName }

// Problems are the errors of the patterns and rules of the grammar that could not be used, found
// so far: a rule that has such a pattern never matches. They are found as the rules are
// compiled, the first time a line is tokenized, and as patterns are first used.
func (g *Grammar) Problems() []error {
	g.problemsMu.Lock()
	defer g.problemsMu.Unlock()
	return append([]error(nil), g.problems...)
}

// addProblem notes an error once.
func (g *Grammar) addProblem(key string, err error) {
	g.problemsMu.Lock()
	defer g.problemsMu.Unlock()
	if g.seen[key] {
		return
	}
	g.seen[key] = true
	g.problems = append(g.problems, err)
}

// initGrammar makes the instance of a raw grammar: its repository with the rules $self (the
// patterns of the grammar, named by its scope) and $base (the grammar that is tokenized, which
// is the grammar itself for the root).
func initGrammar(raw *RawGrammar, base *RawRule) *instance {
	repository := make(map[string]*RawRule, len(raw.Repository)+2)
	for k, v := range raw.Repository {
		repository[k] = v
	}
	self := &RawRule{Patterns: raw.Patterns, HasPatterns: true, Name: raw.ScopeName}
	repository["$self"] = self
	if base != nil {
		repository["$base"] = base
	} else {
		repository["$base"] = self
	}
	return &instance{raw: raw, repository: repository}
}

// externalGrammar is the instance of the grammar with the scope name, made on first use. The
// grammar it is included from gives it its $base.
func (g *Grammar) externalGrammar(scopeName string, repository map[string]*RawRule) *instance {
	if in, ok := g.included[scopeName]; ok {
		return in
	}
	raw := g.registry.Grammar(scopeName)
	if raw == nil {
		return nil
	}
	in := initGrammar(raw, repository["$base"])
	g.included[scopeName] = in
	return in
}

// init compiles the rules from the root.
func (g *Grammar) init() {
	g.once.Do(func() {
		g.rootID = g.compileRule(g.root.repository["$self"], g.root.repository)
	})
}

// register makes the next rule.
func (g *Grammar) register() *rule {
	r := &rule{id: len(g.rules)}
	g.rules = append(g.rules, r)
	return r
}

// compileRule makes the rule of desc once, whatever the number of places that include it, and
// returns its number.
func (g *Grammar) compileRule(desc *RawRule, repository map[string]*RawRule) int {
	if id, ok := g.ids[desc]; ok {
		return id
	}
	r := g.register()
	g.ids[desc] = r.id
	r.name, r.contentName = newTemplate(desc.Name), newTemplate(desc.ContentName)
	switch {
	case desc.Match != "":
		r.kind = ruleMatch
		r.match = newRegexSource(desc.Match, r.id)
		r.captures = g.compileCaptures(desc.Captures, repository)
	case !desc.HasBegin:
		r.kind = ruleIncludeOnly
		if desc.Repository != nil {
			merged := make(map[string]*RawRule, len(repository)+len(desc.Repository))
			for k, v := range repository {
				merged[k] = v
			}
			for k, v := range desc.Repository {
				merged[k] = v
			}
			repository = merged
		}
		patterns := desc.Patterns
		if !desc.HasPatterns && desc.Include != "" {
			patterns = []*RawRule{{Include: desc.Include}}
		}
		r.patterns, r.hasMissingPatterns = g.compilePatterns(patterns, repository)
	case desc.While != "":
		// TODO(step 4.4): begin/while rules. Until then the rule is one that has patterns missing,
		// so that the rules that include it leave it out.
		r.kind = ruleIncludeOnly
		r.hasMissingPatterns = true
		g.addProblem("while:"+desc.Begin, errors.New("RuleWhileUnsupported", "begin/while rules are not supported yet", "begin", desc.Begin))
	default:
		r.kind = ruleBeginEnd
		r.begin = newRegexSource(desc.Begin, r.id)
		r.beginCaptures = g.compileCaptures(firstCaptures(desc.BeginCaptures, desc.Captures), repository)
		end := desc.End
		if end == "" {
			end = "￿"
		}
		r.end = newRegexSource(end, endRuleID)
		r.endCaptures = g.compileCaptures(firstCaptures(desc.EndCaptures, desc.Captures), repository)
		r.applyEndPatternLast = desc.ApplyEndPatternLast
		r.patterns, r.hasMissingPatterns = g.compilePatterns(desc.Patterns, repository)
	}
	return r.id
}

// firstCaptures is the specific captures when the rule has them (even an empty list), and the
// general ones otherwise.
func firstCaptures(specific, general map[string]*RawRule) map[string]*RawRule {
	if specific != nil {
		return specific
	}
	return general
}

// compileCaptures makes the rules of the captures, indexed by the group number.
func (g *Grammar) compileCaptures(captures map[string]*RawRule, repository map[string]*RawRule) []*rule {
	if len(captures) == 0 {
		return nil
	}
	max := 0
	for key := range captures {
		if n, err := strconv.Atoi(key); err == nil && n > max {
			max = n
		}
	}
	out := make([]*rule, max+1)
	for key, desc := range captures {
		n, err := strconv.Atoi(key)
		if err != nil || n < 0 {
			continue
		}
		retokenize := 0
		if desc.HasPatterns {
			retokenize = g.compileRule(desc, repository)
		}
		c := g.register()
		c.kind = ruleCapture
		c.name, c.contentName = newTemplate(desc.Name), newTemplate(desc.ContentName)
		c.retokenizeCapturedWithRuleID = retokenize
		out[n] = c
	}
	return out
}

// compilePatterns makes the rules of the patterns, resolving the includes. A pattern that
// includes something that is not there is left out.
func (g *Grammar) compilePatterns(patterns []*RawRule, repository map[string]*RawRule) (ids []int, hasMissing bool) {
	for _, pattern := range patterns {
		var id int
		if pattern.Include != "" {
			id = g.resolveInclude(pattern.Include, repository)
		} else {
			id = g.compileRule(pattern, repository)
		}
		if id == -1 {
			continue
		}
		if r := g.rules[id]; (r.kind == ruleIncludeOnly || r.kind == ruleBeginEnd) && r.hasMissingPatterns && len(r.patterns) == 0 {
			// A rule whose patterns are all missing has nothing to match.
			continue
		}
		ids = append(ids, id)
	}
	return ids, len(patterns) != len(ids)
}

// resolveInclude finds the rule an include refers to: $base, $self, #name, scope or
// scope#name. It returns -1 when there is none.
func (g *Grammar) resolveInclude(include string, repository map[string]*RawRule) int {
	switch {
	case include == "$base" || include == "$self":
		if d := repository[include]; d != nil {
			return g.compileRule(d, repository)
		}
	case strings.HasPrefix(include, "#"):
		if d := repository[include[1:]]; d != nil {
			return g.compileRule(d, repository)
		}
	default:
		scope, name, _ := strings.Cut(include, "#")
		ext := g.externalGrammar(scope, repository)
		if ext == nil {
			return -1
		}
		if name != "" {
			if d := ext.repository[name]; d != nil {
				return g.compileRule(d, ext.repository)
			}
			return -1
		}
		return g.compileRule(ext.repository["$self"], ext.repository)
	}
	return -1
}

// collectPatterns adds the patterns of a rule that stand for it in the list of its parent.
func (g *Grammar) collectPatterns(r *rule, out *[]*regexSource, seen map[int]bool) {
	switch r.kind {
	case ruleMatch:
		*out = append(*out, r.match)
	case ruleBeginEnd:
		*out = append(*out, r.begin)
	case ruleIncludeOnly:
		if seen[r.id] {
			return
		}
		seen[r.id] = true
		for _, id := range r.patterns {
			g.collectPatterns(g.rules[id], out, seen)
		}
	}
}

// compiled gives the scanner for the patterns that can match inside the rule: those of its
// patterns, and for a rule with an end, the end pattern. endRule is the end pattern completed
// with the text of the captures of the begin, when it has references to them.
func (g *Grammar) compiled(r *rule, endRule string, hasEnd bool) *compiledRule {
	r.mu.Lock()
	defer r.mu.Unlock()
	dynamic := r.kind == ruleBeginEnd && r.end.hasBackReferences && hasEnd
	switch {
	case dynamic && r.dynamic != nil && r.dynamicSource == endRule:
		return r.dynamic
	case !dynamic && r.static != nil:
		return r.static
	}
	var items []*regexSource
	if r.kind == ruleIncludeOnly || r.kind == ruleBeginEnd {
		seen := map[int]bool{}
		for _, id := range r.patterns {
			g.collectPatterns(g.rules[id], &items, seen)
		}
	} else {
		g.collectPatterns(r, &items, map[int]bool{})
	}
	if r.kind == ruleBeginEnd {
		end := r.end
		if dynamic {
			end = &regexSource{source: endRule, ruleID: endRuleID}
		}
		if r.applyEndPatternLast {
			items = append(items, end)
		} else {
			items = append([]*regexSource{end}, items...)
		}
	}
	c := g.newCompiled(items)
	if dynamic {
		r.dynamic, r.dynamicSource = c, endRule
	} else {
		r.static = c
	}
	return c
}

// newCompiled makes the scanner for patterns, and notes those that do not compile.
func (g *Grammar) newCompiled(items []*regexSource) *compiledRule {
	sources := make([]string, len(items))
	ids := make([]int, len(items))
	for i, it := range items {
		sources[i], ids[i] = it.source, it.ruleID
	}
	sc := regex.NewScanner(sources, regex.ScannerOptions{Cache: g.cache})
	for i := range sources {
		if err := sc.Err(i); err != nil {
			g.addProblem("pattern:"+sources[i], err)
		}
	}
	return &compiledRule{scanner: sc, ruleIDs: ids}
}
