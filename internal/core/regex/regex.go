package regex

import (
	"sync"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/rm4n0s/errors"
)

// Syntax is the dialect a pattern is written in.
type Syntax int

const (
	// Oniguruma is the syntax of TextMate grammars (SPEC §8.2): Oniguruma's default syntax with
	// every group capturing, translated to regexp2's.
	Oniguruma Syntax = iota
	// ECMAScript is JavaScript's syntax, for find (SPEC §9.8) and for the regular expressions of
	// language configurations (SPEC §8.1).
	ECMAScript
)

// Options are the options of Compile.
type Options struct {
	// Syntax is the dialect of the pattern; the default is Oniguruma.
	Syntax Syntax
	// IgnoreCase makes the pattern match without regard to case (the i flag).
	IgnoreCase bool
	// Multiline makes ^ and $ match at every line (the m flag of ECMAScript; Oniguruma's
	// anchors always do).
	Multiline bool
	// DotAll makes the dot match a line break (the s flag of ECMAScript; Oniguruma's (?m) is
	// written in the pattern).
	DotAll bool
	// Unicode turns on the u flag of ECMAScript.
	Unicode bool
	// DisableA makes \A never match: the tokenizer asks for it on every line but the first.
	DisableA bool
	// DisableG makes \G never match: the tokenizer asks for it when the search does not start
	// at the anchor position.
	DisableG bool
	// Timeout is the longest a search may run, or 0 for no limit.
	Timeout time.Duration
}

// Regexp is a compiled pattern. It is safe for concurrent use.
type Regexp struct {
	re         *regexp2.Regexp
	source     string
	translated string
	groups     int
	names      map[string]int
	keep       int
	usesA      bool
	usesG      bool
}

// Span is a range of a text, in runes. An unmatched group has the span {-1, -1}.
type Span struct {
	Start, End int
}

// Match is the result of a search.
type Match struct {
	// Groups holds the whole match first and then each capturing group, in the order of their
	// opening parentheses.
	Groups []Span
}

// Compile compiles pattern. A pattern that cannot be compiled has an error whose tag says why:
// RegexUnbalancedParen, RegexUnterminatedClass, RegexNothingToRepeat, RegexInvalidEscape,
// RegexInvalidGroup, RegexInvalidGroupName, RegexInvalidRange, RegexUnknownGroup,
// RegexUnknownProperty, RegexUnsupportedFeature, RegexTooLarge or RegexCompileFailed.
func Compile(pattern string, opts Options) (*Regexp, error) {
	if opts.Syntax == ECMAScript {
		return compileECMAScript(pattern, opts)
	}
	p, root, err := parseOnig(pattern, opts.IgnoreCase)
	if err != nil {
		return nil, err
	}
	translated, err := translateOnig(p, root, opts.DisableA, opts.DisableG)
	if err != nil {
		return nil, err
	}
	// Oniguruma's ^ and $ are anchors of lines; its ignore-case flag is in the pattern.
	var ro regexp2.RegexOptions = regexp2.Multiline
	if opts.IgnoreCase {
		ro |= regexp2.IgnoreCase
	}
	re, err := regexp2.Compile(translated, ro)
	if err != nil {
		return nil, errors.NewErr("RegexCompileFailed", err).SetMetadata("pattern", pattern, "translated", clip(translated))
	}
	r := &Regexp{re: re, source: pattern, translated: translated, groups: p.groups, usesA: p.usesA, usesG: p.usesG}
	if opts.Timeout > 0 {
		re.MatchTimeout = opts.Timeout
	}
	r.names = map[string]int{}
	for name, idx := range p.names {
		r.names[name] = idx[len(idx)-1]
	}
	if p.usesKeep {
		r.keep = re.GroupNumberFromName(keepGroupName)
	}
	return r, nil
}

// clip shortens text for an error.
func clip(s string) string {
	if len(s) > 400 {
		return s[:400] + "..."
	}
	return s
}

// Source is the pattern as it was given to Compile.
func (r *Regexp) Source() string { return r.source }

// Translated is the pattern in regexp2's syntax.
func (r *Regexp) Translated() string { return r.translated }

// NumGroups is the number of capturing groups of the pattern.
func (r *Regexp) NumGroups() int { return r.groups }

// GroupNumber is the number of the group that has the name. A name used by several groups
// gives the last.
func (r *Regexp) GroupNumber(name string) (int, bool) {
	n, ok := r.names[name]
	return n, ok
}

// UsesStartAnchor tells whether the pattern has \A.
func (r *Regexp) UsesStartAnchor() bool { return r.usesA }

// UsesAnchorPosition tells whether the pattern has \G.
func (r *Regexp) UsesAnchorPosition() bool { return r.usesG }

// Find searches text, as runes, from the rune offset from. \G matches at from, and a lookbehind
// reads the text before it. The result is false when nothing matches. A search that runs past the
// timeout of the options has an error with the tag RegexTimeout.
func (r *Regexp) Find(text []rune, from int) (Match, bool, error) {
	if from < 0 || from > len(text) {
		return Match{}, false, nil
	}
	m, err := r.re.FindRunesMatchStartingAt(text, from)
	if err != nil {
		// A search fails only by running past its time limit.
		return Match{}, false, errors.New("RegexTimeout", "the search ran past its time limit", "pattern", clip(r.source), "timeout", r.re.MatchTimeout.String())
	}
	if m == nil {
		return Match{}, false, nil
	}
	groups := make([]Span, r.groups+1)
	for i := range groups {
		g := m.GroupByNumber(i)
		if g == nil || len(g.Captures) == 0 {
			groups[i] = Span{-1, -1}
			continue
		}
		groups[i] = Span{g.Index, g.Index + g.Length}
	}
	if r.keep > 0 {
		if k := m.GroupByNumber(r.keep); k != nil && len(k.Captures) > 0 {
			groups[0].Start = k.Index
		}
	}
	return Match{Groups: groups}, true, nil
}

// Cache remembers compiled patterns, failures included, so that a pattern that is used again is
// not translated again. It is safe for concurrent use.
type Cache struct {
	mu      sync.Mutex
	max     int
	entries map[cacheKey]cacheEntry
	order   []cacheKey
}

// cacheKey identifies a compilation.
type cacheKey struct {
	pattern string
	opts    Options
}

// cacheEntry is a compilation and its error.
type cacheEntry struct {
	re  *Regexp
	err error
}

// NewCache makes a cache that holds up to max compilations; the oldest goes first.
func NewCache(max int) *Cache {
	return &Cache{max: max, entries: map[cacheKey]cacheEntry{}}
}

// Compile is regex.Compile through the cache.
func (c *Cache) Compile(pattern string, opts Options) (*Regexp, error) {
	key := cacheKey{pattern, opts}
	c.mu.Lock()
	e, ok := c.entries[key]
	c.mu.Unlock()
	if ok {
		return e.re, e.err
	}
	re, err := Compile(pattern, opts)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; !ok {
		if len(c.order) >= c.max && len(c.order) > 0 {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
		c.entries[key] = cacheEntry{re, err}
		c.order = append(c.order, key)
	}
	return re, err
}

// Len is the number of compilations in the cache.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
