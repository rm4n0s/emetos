package glob

import (
	"sort"
	"strings"
)

// Clause is one entry of a glob expression, such as the entries of the `files.exclude`
// setting: a pattern that is on or off, or on only when a sibling file exists.
type Clause struct {
	// Pattern is the glob.
	Pattern string
	// Disabled switches the pattern off (the setting's value is false).
	Disabled bool
	// HasWhen is set for a clause with a sibling condition; When is then its text, such as
	// "$(basename).ts": the pattern applies only if a file of that name is next to the one
	// matched. $(basename) is the file name without its extension.
	HasWhen bool
	// When is the sibling condition (see HasWhen).
	When string
}

// Expression is an ordered list of clauses; the first one that matches wins.
type Expression []Clause

// ExpressionFromSetting reads the value of a setting such as files.exclude (a JSON object).
// A value of true, any other value that is not false, or an object without a string "when",
// switches the pattern on; false switches it off; an object with a string "when" is a
// sibling clause. The clauses are ordered by pattern, since a decoded object has no order.
// Values that are not an object give an empty expression.
func ExpressionFromSetting(v any) Expression {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	expr := make(Expression, 0, len(keys))
	for _, k := range keys {
		c := Clause{Pattern: k}
		switch val := obj[k].(type) {
		case bool:
			c.Disabled = !val // only the value false switches a pattern off
		case map[string]any:
			if when, ok := val["when"].(string); ok {
				c.HasWhen, c.When = true, when
			}
		}
		expr = append(expr, c)
	}
	return expr
}

// ExprMatcher is a parsed expression.
type ExprMatcher struct {
	patterns     []*pattern
	allBasenames []string
	allPaths     []string
}

// ParseExpression parses an expression. Clauses that are off, empty or not valid are left
// out.
//
// Ported from VS Code: src/vs/base/common/glob.ts (parsedExpression)
func ParseExpression(expr Expression, opts Options) *ExprMatcher {
	var parsed []*pattern
	for _, c := range expr {
		if p := parseClause(c, opts); p != nil {
			parsed = append(parsed, p)
		}
	}
	m := &ExprMatcher{patterns: aggregateBasenameMatches(parsed, "", opts.IgnoreCase)}
	m.allBasenames, m.allPaths = collectTerms(m.patterns)
	return m
}

// parseClause parses one clause, or gives nil when it can never match.
func parseClause(c Clause, opts Options) *pattern {
	if c.Disabled {
		return nil
	}
	p := parsePattern(c.Pattern, opts)
	if p == nil || !c.HasWhen {
		return p
	}
	// A sibling clause has no terms of its own: it can only be checked with the siblings.
	when := c.When
	return &pattern{
		match: p.match,
		sibling: func(path, basename, name string, hasSibling func(string) bool) (string, bool) {
			if hasSibling == nil {
				return "", false
			}
			if _, ok := p.match(path, basename); !ok {
				return "", false
			}
			if hasSibling(strings.Replace(when, "$(basename)", name, 1)) {
				return c.Pattern, true
			}
			return "", false
		},
	}
}

// IsEmpty tells whether the expression can never match.
func (m *ExprMatcher) IsEmpty() bool { return len(m.patterns) == 0 }

// BasenameTerms are the file names the expression is made of (see Matcher.BasenameTerms).
func (m *ExprMatcher) BasenameTerms() []string { return m.allBasenames }

// PathTerms are the paths the expression is made of (see Matcher.PathTerms).
func (m *ExprMatcher) PathTerms() []string { return m.allPaths }

// Match returns the first pattern of the expression that matches the path. basename may be
// empty. hasSibling tells whether a file of that name exists next to the path; without it
// the clauses with a sibling condition never match.
func (m *ExprMatcher) Match(path, basename string, hasSibling func(name string) bool) (string, bool) {
	name := ""
	for _, p := range m.patterns {
		if p.sibling == nil {
			if pat, ok := p.match(path, basename); ok {
				return pat, true
			}
			continue
		}
		if hasSibling != nil {
			if basename == "" {
				basename = baseName(path)
			}
			if name == "" {
				name = basename[:len(basename)-len(extName(path))]
			}
		}
		if pat, ok := p.sibling(path, basename, name, hasSibling); ok {
			return pat, true
		}
	}
	return "", false
}

// MatchExpression returns the first pattern of expr that matches the path.
func MatchExpression(expr Expression, path string, opts Options) (string, bool) {
	return ParseExpression(expr, opts).Match(path, "", nil)
}

// baseName is the last segment of a slash-separated path, without trailing slashes.
func baseName(p string) string {
	p = strings.TrimRight(p, "/")
	return p[strings.LastIndexByte(p, '/')+1:]
}

// extName is the extension of the last segment of the path, with the dot; a name that only
// starts with a dot has none.
func extName(p string) string {
	base := baseName(p)
	i := strings.LastIndexByte(base, '.')
	if i <= 0 || base == ".." {
		return ""
	}
	return base[i:]
}
