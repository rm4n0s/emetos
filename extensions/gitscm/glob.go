package gitscm

import (
	"regexp"
	"strings"
)

// A small glob for the names of branches (git.branchProtection): the part of picomatch's syntax that branch names use. `*` is any
// run of characters but "/", `**` any run, `?` one character but "/", `[abc]` and `[a-c]` a set (`[!a]` and `[^a]` a set that is not),
// `{a,b}` one of the alternatives, a backslash makes the next character plain, and a leading `!` turns the pattern around. A star
// does not match a name that starts with a dot, as picomatch's does not.

// matchesAnyGlob says whether the name matches at least one of the patterns; none matches nothing.
func matchesAnyGlob(patterns []string, name string) bool {
	for _, p := range patterns {
		negate := false
		for strings.HasPrefix(p, "!") {
			negate, p = !negate, p[1:]
		}
		re, ok := globRegexp(p)
		if ok && re.MatchString(name) != negate {
			return true
		}
	}
	return false
}

// globRegexp makes the regular expression of a glob; false for a pattern that cannot be one.
func globRegexp(glob string) (*regexp.Regexp, bool) {
	var b strings.Builder
	b.WriteString("^")
	depth := 0
	runes := []rune(glob)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch c {
		case '*':
			j := i
			for j+1 < len(runes) && runes[j+1] == '*' {
				j++
			}
			double := j > i
			start := i == 0 || runes[i-1] == '/' || runes[i-1] == '{' || runes[i-1] == ','
			i = j
			switch {
			case double && start && i+1 < len(runes) && runes[i+1] == '/':
				// "a/**/b" also matches "a/b"
				i++
				b.WriteString(`(?:[^./][^/]*/)*`)
			case double && start:
				b.WriteString(`(?:[^.].*)?`)
			case double:
				b.WriteString(`.*`)
			case start:
				b.WriteString(`(?:[^./][^/]*)?`)
			default:
				b.WriteString(`[^/]*`)
			}
		case '?':
			b.WriteString(`[^/]`)
		case '[':
			end := i + 1
			if end < len(runes) && (runes[end] == '!' || runes[end] == '^') {
				end++
			}
			if end < len(runes) && runes[end] == ']' {
				end++
			}
			for end < len(runes) && runes[end] != ']' {
				end++
			}
			if end >= len(runes) {
				b.WriteString(`\[`)
				continue
			}
			set := string(runes[i+1 : end])
			if strings.HasPrefix(set, "!") {
				set = "^" + set[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(set, `\`, `\\`) + "]")
			i = end
		case '{':
			depth++
			b.WriteString("(?:")
		case '}':
			if depth == 0 {
				b.WriteString(`\}`)
				continue
			}
			depth--
			b.WriteString(")")
		case ',':
			if depth > 0 {
				b.WriteString("|")
			} else {
				b.WriteString(",")
			}
		case '\\':
			if i+1 < len(runes) {
				i++
				b.WriteString(regexp.QuoteMeta(string(runes[i])))
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	if depth != 0 {
		return nil, false
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	return re, err == nil
}
