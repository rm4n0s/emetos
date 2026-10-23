package glob

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Options tune how patterns are parsed.
type Options struct {
	// TrimForExclusions simplifies patterns for use as exclusion filters while walking a
	// tree, to skip whole subtrees: "**/foo/**" then matches the folder "foo" itself, not
	// the files below it. It must not be used outside a tree walk.
	TrimForExclusions bool
	// IgnoreCase makes matching case insensitive. Linux file names are case sensitive, so
	// it is off unless the caller asks for it.
	IgnoreCase bool
}

// RelativePattern is a glob that matches the paths below Base, relative to it.
type RelativePattern struct {
	// Base is the folder the pattern is relative to.
	Base string
	// Pattern is a glob such as "*.{ts,js}", matched against the path below Base: with
	// Base "/home/work/folder" it matches "/home/work/folder/index.js" as "index.js".
	Pattern string
}

const (
	globstar = "**"
	// pathRegex is any slash or backslash; noPathRegex is any other character. Both slashes
	// separate path segments so that a Windows path matches the same patterns.
	pathRegex   = `[/\\]`
	noPathRegex = `[^/\\]`
)

// starsToRegExp is the expression for one star (any characters but a slash) or two (any
// number of segments); isLastPattern also lets the two stars match a trailing segment.
//
// Ported from VS Code: src/vs/base/common/glob.ts
func starsToRegExp(starCount int, isLastPattern bool) string {
	switch starCount {
	case 0:
		return ""
	case 1:
		return noPathRegex + "*?"
	default:
		last := ""
		if isLastPattern {
			last = "|" + pathRegex + noPathRegex + "+"
		}
		return "(?:" + pathRegex + "|" + noPathRegex + "+" + pathRegex + last + ")*?"
	}
}

// SplitGlobAware splits a pattern at splitChar, except inside braces and brackets. An empty
// pattern gives no segments; an empty tail is not a segment.
func SplitGlobAware(pattern string, splitChar rune) []string {
	if pattern == "" {
		return nil
	}
	var segments []string
	inBraces, inBrackets := false, false
	var cur strings.Builder
	for _, c := range pattern {
		switch c {
		case splitChar:
			if !inBraces && !inBrackets {
				segments = append(segments, cur.String())
				cur.Reset()
				continue
			}
		case '{':
			inBraces = true
		case '}':
			inBraces = false
		case '[':
			inBrackets = true
		case ']':
			inBrackets = false
		}
		cur.WriteRune(c)
	}
	if cur.Len() > 0 {
		segments = append(segments, cur.String())
	}
	return segments
}

// escapeRegExp escapes the characters that are special in a regular expression.
func escapeRegExp(c rune) string {
	if strings.ContainsRune(`\{}*+?|^$.[]()`, c) {
		return `\` + string(c)
	}
	return string(c)
}

// parseRegExp turns a glob into the source of a regular expression.
func parseRegExp(pattern string) string {
	if pattern == "" {
		return ""
	}
	segments := SplitGlobAware(pattern, '/')
	if !slices.ContainsFunc(segments, func(s string) bool { return s != globstar }) {
		return ".*"
	}
	var re strings.Builder
	previousWasGlobstar := false
	for index, segment := range segments {
		if segment == globstar {
			// more than one globstar after another counts as one
			if previousWasGlobstar {
				continue
			}
			re.WriteString(starsToRegExp(2, index == len(segments)-1))
		} else {
			inBraces, inBrackets := false, false
			var braceVal, bracketVal string
			for _, c := range segment {
				if c != '}' && inBraces {
					braceVal += string(c)
					continue
				}
				// A ] is only a literal as the first character of a bracket.
				if inBrackets && (c != ']' || bracketVal == "") {
					switch {
					case c == '-':
						bracketVal += "-"
					case (c == '^' || c == '!') && bracketVal == "":
						bracketVal += "^"
					case c == '/':
						// a slash is not allowed within character ranges
					default:
						bracketVal += escapeRegExp(c)
					}
					continue
				}
				switch c {
				case '{':
					inBraces = true
				case '[':
					inBrackets = true
				case '}':
					choices := SplitGlobAware(braceVal, ',')
					parts := make([]string, len(choices))
					for i, choice := range choices {
						parts[i] = parseRegExp(choice)
					}
					re.WriteString("(?:" + strings.Join(parts, "|") + ")")
					inBraces = false
					braceVal = ""
				case ']':
					re.WriteString(closeBracket(bracketVal))
					inBrackets = false
					bracketVal = ""
				case '?':
					re.WriteString(noPathRegex)
				case '*':
					re.WriteString(starsToRegExp(1, false))
				default:
					re.WriteString(escapeRegExp(c))
				}
			}
			// Add the slash the pattern was split on if more comes after, unless what comes
			// is only a trailing globstar: "some/**/*.js" must not match "something/x.js".
			if index < len(segments)-1 && (segments[index+1] != globstar || index+2 < len(segments)) {
				re.WriteString(pathRegex)
			}
		}
		previousWasGlobstar = segment == globstar
	}
	return re.String()
}

// closeBracket writes a character class. JavaScript reads "[]" as a class that matches
// nothing and "[^]" as one that matches anything; Go's regexp refuses both.
func closeBracket(val string) string {
	switch val {
	case "":
		return `[^\x00-\x{10FFFF}]`
	case "^":
		return `(?s:.)`
	}
	return "[" + val + "]"
}

// Regular expressions that find the trivial patterns, which are matched with string checks.
var (
	t1  = regexp.MustCompile(`^\*\*/\*\.[\w.-]+$`)                                             // **/*.something
	t2  = regexp.MustCompile(`^\*\*/([\w.-]+)/?$`)                                             // **/something
	t3  = regexp.MustCompile(`^\{\*\*/\*?[\w.-]+/?(,\*\*/\*?[\w.-]+/?)*\}$`)                   // {**/*.something,**/*.else}
	t32 = regexp.MustCompile(`^\{\*\*/\*?[\w.-]+(/(\*\*)?)?(,\*\*/\*?[\w.-]+(/(\*\*)?)?)*\}$`) // like t3, with an optional trailing /**
	t4  = regexp.MustCompile(`^\*\*((/[\w.-]+)+)/?$`)                                          // **/something/else
	t5  = regexp.MustCompile(`^([\w.-]+(/[\w.-]+)*)/?$`)                                       // something/else
)

// matchFunc matches a path (and its basename, when the caller has it) and returns the
// pattern that matched.
type matchFunc func(path, basename string) (pattern string, ok bool)

// pattern is a parsed glob with the information used to optimize searches.
type pattern struct {
	match matchFunc
	// basenames and patterns are set for a plain basename pattern, so that several can be
	// merged into one lookup.
	basenames []string
	patterns  []string
	// allBasenames and allPaths list the file names and paths the pattern matches, for
	// callers that search for them before they walk (VS Code's getBasenameTerms).
	allBasenames []string
	allPaths     []string
	// sibling is set for a clause of an expression that applies only when a sibling file
	// exists; name is the file name without its extension.
	sibling func(path, basename, name string, hasSibling func(string) bool) (string, bool)
}

// strOps are the string comparisons, case sensitive or not.
type strOps struct {
	ignoreCase bool
}

func (o strOps) equals(a, b string) bool {
	if o.ignoreCase {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (o strOps) endsWith(s, suffix string) bool {
	if !o.ignoreCase {
		return strings.HasSuffix(s, suffix)
	}
	if len(s) < len(suffix) {
		return false
	}
	i := len(s) - len(suffix)
	return (i == len(s) || utf8.RuneStart(s[i])) && strings.EqualFold(s[i:], suffix)
}

// parsePattern parses a glob. A nil result means the pattern can never match: it is empty
// or not valid.
func parsePattern(raw string, opts Options) *pattern {
	p := strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	ops := strOps{ignoreCase: opts.IgnoreCase}
	trimmed := trimForExclusions(p, opts)
	switch {
	case t1.MatchString(p):
		return trivia1(p[4:], p, ops)
	case t2.MatchString(trimmed):
		return trivia2(t2.FindStringSubmatch(trimmed)[1], p, ops)
	case matchesT3(p, opts):
		return trivia3(p, opts)
	case t4.MatchString(trimmed):
		return trivia4and5(t4.FindStringSubmatch(trimmed)[1][1:], p, true, ops)
	case t5.MatchString(trimmed):
		return trivia4and5(t5.FindStringSubmatch(trimmed)[1], p, false, ops)
	}
	return toRegExp(p, opts)
}

func matchesT3(p string, opts Options) bool {
	if opts.TrimForExclusions {
		return t32.MatchString(p)
	}
	return t3.MatchString(p)
}

// trimForExclusions drops the "**" of a trailing "/**" when matching for exclusions.
func trimForExclusions(p string, opts Options) string {
	if opts.TrimForExclusions && strings.HasSuffix(p, "/**") {
		return p[:len(p)-2]
	}
	return p
}

// trivia1 is "**/*.txt": the path ends with ".txt".
func trivia1(base, pat string, ops strOps) *pattern {
	return &pattern{match: func(path, _ string) (string, bool) {
		if ops.endsWith(path, base) {
			return pat, true
		}
		return "", false
	}}
}

// trivia2 is "**/some.txt": the file name is "some.txt".
func trivia2(base, pat string, ops strOps) *pattern {
	slashBase, backslashBase := "/"+base, `\`+base
	basenames := []string{base}
	return &pattern{
		match: func(path, basename string) (string, bool) {
			if basename != "" {
				return pat, ops.equals(basename, base)
			}
			if ops.equals(path, base) || ops.endsWith(path, slashBase) || ops.endsWith(path, backslashBase) {
				return pat, true
			}
			return "", false
		},
		basenames:    basenames,
		patterns:     []string{pat},
		allBasenames: basenames,
	}
}

// trivia3 is "{**/*.txt,**/*.png}": a list of the patterns above.
func trivia3(pat string, opts Options) *pattern {
	var parsed []*pattern
	for _, part := range strings.Split(pat[1:len(pat)-1], ",") {
		if p := parsePattern(part, opts); p != nil {
			parsed = append(parsed, p)
		}
	}
	parsed = aggregateBasenameMatches(parsed, pat, opts.IgnoreCase)
	switch len(parsed) {
	case 0:
		return nil
	case 1:
		return parsed[0]
	}
	out := &pattern{match: func(path, basename string) (string, bool) {
		for _, p := range parsed {
			if _, ok := p.match(path, basename); ok {
				return pat, true
			}
		}
		return "", false
	}}
	out.allBasenames, out.allPaths = collectTerms(parsed)
	return out
}

// trivia4and5 is "**/something/else" (matchPathEnds: the path ends with it) or
// "something/else" (the path is it).
func trivia4and5(target, pat string, matchPathEnds bool, ops strOps) *pattern {
	targetEnd := "/" + target
	var match matchFunc
	if matchPathEnds {
		match = func(path, _ string) (string, bool) {
			if ops.equals(path, target) || ops.endsWith(path, targetEnd) {
				return pat, true
			}
			return "", false
		}
	} else {
		match = func(path, _ string) (string, bool) {
			if ops.equals(path, target) {
				return pat, true
			}
			return "", false
		}
	}
	prefix := "./"
	if matchPathEnds {
		prefix = "*/"
	}
	return &pattern{match: match, allPaths: []string{prefix + target}}
}

// toRegExp compiles the pattern to a regular expression, or gives nil when it is not valid.
func toRegExp(pat string, opts Options) *pattern {
	src := "^" + parseRegExp(pat) + "$"
	if opts.IgnoreCase {
		src = "(?i)" + src
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil
	}
	return &pattern{match: func(path, _ string) (string, bool) {
		if re.MatchString(path) {
			return pat, true
		}
		return "", false
	}}
}

// collectTerms gathers the first non-empty list of basenames and all the paths of patterns.
func collectTerms(ps []*pattern) (basenames, paths []string) {
	for _, p := range ps {
		if basenames == nil && p.allBasenames != nil {
			basenames = p.allBasenames
		}
		paths = append(paths, p.allPaths...)
	}
	return basenames, paths
}

// aggregateBasenameMatches merges the basename patterns (two or more) into one lookup.
// result, when not empty, is the pattern reported for each; otherwise each reports its own.
func aggregateBasenameMatches(ps []*pattern, result string, ignoreCase bool) []*pattern {
	var withBase []*pattern
	for _, p := range ps {
		if p.basenames != nil {
			withBase = append(withBase, p)
		}
	}
	if len(withBase) < 2 {
		return ps
	}
	var basenames, patterns []string
	for _, p := range withBase {
		basenames = append(basenames, p.basenames...)
	}
	if result != "" {
		for range basenames {
			patterns = append(patterns, result)
		}
	} else {
		for _, p := range withBase {
			patterns = append(patterns, p.patterns...)
		}
	}
	agg := &pattern{
		match: func(path, basename string) (string, bool) {
			if basename == "" {
				basename = path[strings.LastIndexAny(path, `/\`)+1:]
			}
			for i, b := range basenames {
				if b == basename || (ignoreCase && strings.EqualFold(b, basename)) {
					return patterns[i], true
				}
			}
			return "", false
		},
		basenames: basenames, patterns: patterns, allBasenames: basenames,
	}
	out := make([]*pattern, 0, len(ps)-len(withBase)+1)
	for _, p := range ps {
		if p.basenames == nil {
			out = append(out, p)
		}
	}
	return append(out, agg)
}

// Matcher is a parsed glob pattern.
type Matcher struct {
	p *pattern
}

// Parse parses a glob pattern. The syntax is VS Code's: `*` matches any characters in a
// path segment, `?` one character, `**` any number of segments (including none), `{a,b}`
// either choice, `[0-9]` a character of a range and `[!0-9]` or `[^0-9]` one that is not in
// it. Both / and \ separate segments. A pattern that is empty or not valid matches nothing.
//
// Ported from VS Code: src/vs/base/common/glob.ts
func Parse(pattern string, opts Options) *Matcher {
	return &Matcher{p: parsePattern(pattern, opts)}
}

// ParseRelative parses a pattern that is relative to a base folder: a path matches when it
// is below Base and the rest matches the pattern.
func ParseRelative(rp RelativePattern, opts Options) *Matcher {
	p := parsePattern(rp.Pattern, opts)
	if p == nil {
		return &Matcher{}
	}
	wrapped := *p
	inner := p.match
	wrapped.match = func(path, basename string) (string, bool) {
		if !isEqualOrParent(path, rp.Base, opts.IgnoreCase) {
			return "", false // skip the matching when Base is not a parent of path
		}
		// Base may end in a separator (VS Code issue 162498).
		return inner(strings.TrimLeft(path[len(rp.Base):], "/"), basename)
	}
	return &Matcher{p: &wrapped}
}

// IsEmpty tells whether the pattern can never match: it was empty or not valid.
func (m *Matcher) IsEmpty() bool { return m.p == nil }

// Match tells whether the path matches.
func (m *Matcher) Match(path string) bool { return m.MatchBase(path, "") }

// MatchBase is Match for a caller that already knows the file name of the path, which makes
// the check of a basename pattern cheaper.
func (m *Matcher) MatchBase(path, basename string) bool {
	if m.p == nil {
		return false
	}
	_, ok := m.p.match(path, basename)
	return ok
}

// BasenameTerms are the file names the pattern is made of, when it only matches file names
// ("**/foo" gives "foo"); callers search for them before walking a tree.
func (m *Matcher) BasenameTerms() []string {
	if m.p == nil {
		return nil
	}
	return m.p.allBasenames
}

// PathTerms are the paths the pattern is made of, when it only matches path ends
// ("**/foo/bar" gives "*/foo/bar", "foo/bar" gives "./foo/bar").
func (m *Matcher) PathTerms() []string {
	if m.p == nil {
		return nil
	}
	return m.p.allPaths
}

// MatchPattern tells whether a path matches a glob pattern.
func MatchPattern(pattern, path string, opts Options) bool {
	return Parse(pattern, opts).Match(path)
}

// isEqualOrParent tells whether candidate is path or one of its parent folders.
//
// Ported from VS Code: src/vs/base/common/extpath.ts
func isEqualOrParent(path, candidate string, ignoreCase bool) bool {
	if path == candidate {
		return true
	}
	if path == "" || candidate == "" {
		return false
	}
	if strings.Contains(path, "..") || strings.Contains(candidate, "..") {
		path, candidate = normalize(path), normalize(candidate)
	}
	if len(candidate) > len(path) {
		return false
	}
	if ignoreCase {
		if !startsWithFold(path, candidate) {
			return false
		}
		if len(candidate) == len(path) {
			return true // same path, different casing
		}
		sepOffset := len(candidate)
		if candidate[len(candidate)-1] == '/' {
			sepOffset-- // the candidate already ends in a separator
		}
		return path[sepOffset] == '/'
	}
	if candidate[len(candidate)-1] != '/' {
		candidate += "/"
	}
	return strings.HasPrefix(path, candidate)
}

func startsWithFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return (len(prefix) == len(s) || utf8.RuneStart(s[len(prefix)])) && strings.EqualFold(s[:len(prefix)], prefix)
}

// normalize resolves "." and ".." segments of a slash-separated path and keeps a trailing
// slash, as the normalize of Node's posix path does.
func normalize(p string) string {
	if p == "" {
		return "."
	}
	rooted := strings.HasPrefix(p, "/")
	trailing := strings.HasSuffix(p, "/")
	var out []string
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".":
		case "..":
			if len(out) > 0 && out[len(out)-1] != ".." {
				out = out[:len(out)-1]
			} else if !rooted {
				out = append(out, "..")
			}
		default:
			out = append(out, seg)
		}
	}
	res := strings.Join(out, "/")
	if res == "" && !rooted {
		res = "."
	}
	if res != "" && trailing {
		res += "/"
	}
	if rooted {
		res = "/" + res
	}
	return res
}

// PatternsEqual tells whether two lists of patterns are the same. It is used to decide
// whether a watcher must be restarted, so it compares the text and ignores case rules.
func PatternsEqual(a, b []RelativePattern) bool {
	return slices.Equal(a, b)
}
