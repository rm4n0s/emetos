package textmate

import (
	"regexp"
	"strings"
)

// Ported from VS Code: reference/vscode-textmate/src/matcher.ts (vscode-textmate, MIT)

// scopeMatcher is a scope selector, such as "text.html - comment" or "L:source.js", made
// ready to be matched against the scopes of a token.
type scopeMatcher struct {
	// match tells whether the selector is true of the scopes, outermost first.
	match func(scopes []string) bool
	// priority is -1 for a selector that starts with L:, 1 for R: and 0 for none.
	priority int
}

// selectorToken is a token of a selector: a priority (L:, R:), a scope name, or one of ,|-().
var selectorToken = regexp.MustCompile(`([LR]:|[\w.:][\w.:\-]*|[,|\-()])`)

// selectorIdentifier is what makes a token a scope name: it has a letter, a digit, an underscore,
// a dot or a colon.
var selectorIdentifier = regexp.MustCompile(`[\w.:]+`)

// parseSelector reads a scope selector. The selector "a b" is true of scopes that have a and then
// b; "a - b" of those with a and not b; "a, b" and "a | b" of those that have either; and
// parentheses group. A selector with several alternatives separated by commas gives one matcher
// for each, which can have its own priority. matchesName tells whether the names of an
// operand are in the scopes.
func parseSelector(selector string, matchesName func(names, scopes []string) bool) []scopeMatcher {
	tokens := selectorToken.FindAllString(selector, -1)
	pos := 0
	// token is the current token, and "" when the input is finished.
	token := func() string {
		if pos >= len(tokens) {
			return ""
		}
		return tokens[pos]
	}
	next := func() { pos++ }
	isIdentifier := func(s string) bool { return s != "" && selectorIdentifier.MatchString(s) }

	var parseOperand func() func([]string) bool
	var parseConjunction func() func([]string) bool
	var parseInner func() func([]string) bool

	parseOperand = func() func([]string) bool {
		switch t := token(); {
		case t == "-":
			next()
			negated := parseOperand()
			return func(scopes []string) bool { return negated != nil && !negated(scopes) }
		case t == "(":
			next()
			inner := parseInner()
			if token() == ")" {
				next()
			}
			return inner
		case isIdentifier(t):
			var names []string
			for isIdentifier(token()) {
				names = append(names, token())
				next()
			}
			return func(scopes []string) bool { return matchesName(names, scopes) }
		}
		return nil
	}
	parseConjunction = func() func([]string) bool {
		var all []func([]string) bool
		for m := parseOperand(); m != nil; m = parseOperand() {
			all = append(all, m)
		}
		return func(scopes []string) bool {
			for _, m := range all {
				if !m(scopes) {
					return false
				}
			}
			return true
		}
	}
	parseInner = func() func([]string) bool {
		var any []func([]string) bool
		m := parseConjunction()
		for m != nil {
			any = append(any, m)
			if token() != "|" && token() != "," {
				break
			}
			for token() == "|" || token() == "," {
				next()
			}
			m = parseConjunction()
		}
		return func(scopes []string) bool {
			for _, m := range any {
				if m(scopes) {
					return true
				}
			}
			return false
		}
	}

	var results []scopeMatcher
	for token() != "" {
		priority := 0
		if t := token(); len(t) == 2 && t[1] == ':' {
			switch t[0] {
			case 'R':
				priority = 1
			case 'L':
				priority = -1
			}
			next()
		}
		results = append(results, scopeMatcher{match: parseConjunction(), priority: priority})
		if token() != "," {
			break
		}
		next()
	}
	return results
}

// scopesAreMatching tells whether the scope is the name or starts with it and a dot.
func scopesAreMatching(scope, name string) bool {
	if scope == "" {
		return false
	}
	if scope == name {
		return true
	}
	return len(scope) > len(name) && strings.HasPrefix(scope, name) && scope[len(name)] == '.'
}

// nameMatcher tells whether the scopes have the names in order, each as a scope or as the start
// of one (source.js is in source.js.embedded.html).
func nameMatcher(names, scopes []string) bool {
	if len(scopes) < len(names) {
		return false
	}
	last := 0
	for _, name := range names {
		found := false
		for i := last; i < len(scopes); i++ {
			if scopesAreMatching(scopes[i], name) {
				last = i + 1
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
