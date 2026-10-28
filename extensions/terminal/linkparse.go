package terminal

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Ported from VS Code: src/vs/workbench/contrib/terminalContrib/links/browser/terminalLinkParsing.ts
//
// This file finds the places in a line of text that look like a file path, with the line and column that may follow it. It
// looks at the text only and checks nothing against the file system. The indexes are counted in characters (runes), as
// VS Code's are counted in UTF-16 units, so the two agree for the text of the tests that is not beyond the first plane.
// Only the Linux rules are ported (Emetos runs on the Linux desktop only).

// none marks a number the suffix of a link does not have.
const none = -1

// linkPart is a piece of a line: where it starts (in characters) and its text.
type linkPart struct {
	index int
	text  string
}

// end is the index after the last character of the piece.
func (p linkPart) end() int { return p.index + utf8.RuneCountInString(p.text) }

// linkSuffix is the line and column a link names after its path (foo:339:12), with the end of a range when it has one.
type linkSuffix struct {
	row, col, rowEnd, colEnd int
	suffix                   linkPart
}

// parsedLink is a path found in a line: the path, the quotes in front of it that are part of what is underlined, and the
// suffix with the line and column.
type parsedLink struct {
	path   linkPart
	prefix *linkPart
	suffix *linkSuffix
}

// jsSpace are the characters JavaScript's \s matches that Go's does not (and the ones both match, so that the class can be
// written once): the white space of ECMAScript.
const jsSpace = `\s\x{0B}\x{A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

// nbsp is the non-breaking space that the line and column clauses accept in place of a space.
const nbsp = " "

// suffixClauses are the formats of the line and column after a path, with the groups numbered by the clause (row0, col0, ...).
// They are VS Code's lineAndColumnRegexClauses; the comments of each list the forms it accepts.
func suffixClauses() []string {
	return []string{
		// foo:339  foo:339:12  foo:339:12-789  foo:339:12-341.789  foo:339.12  foo 339  foo 339:12  foo#339  foo#339:12
		// foo, 339  "foo",339  "foo",339:12  "foo",339.12-341.789
		`(?::|#| |['"],|, )(?P<row0>\d+)(?:[:.](?P<col0>\d+)(?:-(?:(?P<rowEnd0>\d+)\.)?(?P<colEnd0>\d+))?)?`,
		// "foo", line 339  "foo", line 339, col 12  "foo":line 339, column 12  "foo" on line 339  "foo", line 339, characters 12-789
		// "foo", lines 339-341, characters 12-789
		`['"]?(?:,? |: ?| on )lines? (?P<row1>\d+)(?:-(?P<rowEnd1>\d+))?(?:,? (?:col(?:umn)?|characters?) (?P<col1>\d+)(?:-(?P<colEnd1>\d+))?)?`,
		// () and [] are interchangeable: foo(339)  foo(339,12)  foo (339, 12)  foo: (339)  foo(339:12)
		`:? ?[\[\(](?P<row2>\d+)(?:(?:, ?|:)(?P<col2>\d+))?[\]\)]`,
	}
}

// suffixPattern joins the clauses and lets the non-breaking space stand for each space.
func suffixPattern(eolOnly bool) *regexp.Regexp {
	joined := strings.Join(suffixClauses(), "|")
	joined = strings.ReplaceAll(joined, " ", "["+nbsp+" ]")
	if eolOnly {
		// a suffix that must end the text: each clause ends there
		parts := strings.Split(strings.Join(suffixClauses(), "\x00"), "\x00")
		for i := range parts {
			parts[i] = strings.ReplaceAll(parts[i], " ", "["+nbsp+" ]") + "$"
		}
		joined = strings.Join(parts, "|")
	}
	return regexp.MustCompile("(" + joined + ")")
}

var (
	suffixRegexEOL = suffixPattern(true)
	suffixRegex    = suffixPattern(false)
	// linkWithSuffixPath is the path before a suffix: it may not start with white space, | < > [ ( {, and has none of the
	// first four inside it.
	linkWithSuffixPath = regexp.MustCompile(`(?P<path>(?:file:///)?[^` + jsSpace + `|<>\[({][^` + jsSpace + `|<>]*)$`)
	pathPrefixQuotes   = regexp.MustCompile(`^['"]+`)
)

// runeIndex is the index in characters of a byte offset of s.
func runeIndex(s string, byteOffset int) int { return utf8.RuneCountInString(s[:byteOffset]) }

// toSuffix reads the groups of a match of the suffix pattern; line is the text the match was found in.
func toSuffix(re *regexp.Regexp, line string, m []int) *linkSuffix {
	if m == nil {
		return nil
	}
	num := func(names ...string) int {
		for _, n := range names {
			i := re.SubexpIndex(n)
			if i >= 0 && m[2*i] >= 0 {
				v, err := strconv.Atoi(line[m[2*i]:m[2*i+1]])
				if err == nil {
					return v
				}
			}
		}
		return none
	}
	return &linkSuffix{
		row: num("row0", "row1", "row2"), col: num("col0", "col1", "col2"),
		rowEnd: num("rowEnd0", "rowEnd1", "rowEnd2"), colEnd: num("colEnd0", "colEnd1", "colEnd2"),
		suffix: linkPart{index: runeIndex(line, m[0]), text: line[m[0]:m[1]]},
	}
}

// linkSuffixOf returns the line and column at the end of a link, nil when it has none.
func linkSuffixOf(link string) *linkSuffix {
	return toSuffix(suffixRegexEOL, link, suffixRegexEOL.FindStringSubmatchIndex(link))
}

// removeLinkSuffix removes the optional suffix with the line and column.
func removeLinkSuffix(link string) string {
	s := linkSuffixOf(link)
	if s == nil {
		return link
	}
	return string([]rune(link)[:s.suffix.index])
}

// removeLinkQueryString removes any query string from the link (a ? in a UNC path does not start one).
func removeLinkQueryString(link string) string {
	start := 0
	if strings.HasPrefix(link, `\\?\`) {
		start = 4
	}
	i := strings.IndexByte(link[start:], '?')
	if i < 0 {
		return link
	}
	return link[:start+i]
}

// detectLinkSuffixes finds every suffix of a line; the matches do not overlap.
func detectLinkSuffixes(line string) []linkSuffix {
	var out []linkSuffix
	for _, m := range suffixRegex.FindAllStringSubmatchIndex(line, -1) {
		if s := toSuffix(suffixRegex, line, m); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

// detectLinks finds the paths of a line: the ones followed by a line and column first, then the others that do not overlap
// them.
func detectLinks(line string) []parsedLink {
	results := detectLinksViaSuffix(line)
	binaryInsertList(&results, detectPathsNoSuffix(line))
	return results
}

// binaryInsertList adds the links that do not overlap the ones already there, keeping the list in order of position.
func binaryInsertList(list *[]parsedLink, items []parsedLink) {
	if len(*list) == 0 {
		*list = append(*list, items...)
	}
	for _, item := range items {
		binaryInsert(list, item, 0, len(*list))
	}
}

func binaryInsert(list *[]parsedLink, item parsedLink, low, high int) {
	l := *list
	if len(l) == 0 {
		*list = append(l, item)
		return
	}
	if low > high {
		return
	}
	mid := (low + high) / 2
	if mid >= len(l) || (item.path.index < l[mid].path.index && (mid == 0 || item.path.index > l[mid-1].path.index)) {
		// it goes before the one at mid, unless it overlaps a neighbor
		if mid >= len(l) || (item.path.end() < l[mid].path.index && (mid == 0 || item.path.index > l[mid-1].path.end())) {
			l = append(l, parsedLink{})
			copy(l[mid+1:], l[mid:])
			l[mid] = item
			*list = l
		}
		return
	}
	if item.path.index > l[mid].path.index {
		binaryInsert(list, item, mid+1, high)
	} else {
		binaryInsert(list, item, low, mid-1)
	}
}

// openingBracket finds the brackets of a path that are not followed by a closing one (a match of (?<bracket>[\[\(])(?![\]\)])
// of VS Code's, written without the lookahead Go's expressions do not have): the index of each in characters.
func openingBrackets(path string) []int {
	var out []int
	r := []rune(path)
	for i, c := range r {
		if (c == '[' || c == '(') && (i+1 >= len(r) || (r[i+1] != ']' && r[i+1] != ')')) {
			out = append(out, i)
		}
	}
	return out
}

func detectLinksViaSuffix(line string) []parsedLink {
	var results []parsedLink
	runes := []rune(line)
	for _, suffix := range detectLinkSuffixes(line) {
		// a suffix followed by / is not one: 1/ in the prefixes of a git diff is a path
		if end := suffix.suffix.end(); end < len(runes) && runes[end] == '/' {
			continue
		}
		before := string(runes[:suffix.suffix.index])
		m := linkWithSuffixPath.FindStringSubmatchIndex(before)
		if m == nil {
			continue
		}
		linkStart := runeIndex(before, m[0])
		path := before[m[2*linkWithSuffixPath.SubexpIndex("path")]:]
		var prefix *linkPart
		if q := pathPrefixQuotes.FindString(path); q != "" {
			prefix = &linkPart{index: linkStart, text: q}
			path = path[len(q):]
			// no link whose path is empty
			if strings.TrimSpace(path) == "" {
				continue
			}
			// with several quotes in front, the last one may be the first of the suffix (echo "'foo' on line 1")
			if n := utf8.RuneCountInString(q); n > 1 {
				last := q[len(q)-1]
				if first := suffix.suffix.text[0]; (first == '\'' || first == '"') && last == first {
					prefix.index += n - 1
					prefix.text = string(last)
					linkStart += n - 1
				}
			}
		}
		prefixLen := 0
		if prefix != nil {
			prefixLen = utf8.RuneCountInString(prefix.text)
		}
		s := suffix
		results = append(results, parsedLink{path: linkPart{index: linkStart + prefixLen, text: path}, prefix: prefix, suffix: &s})
		// a path with an opening bracket in it also gives the path after the bracket
		pr := []rune(path)
		for _, i := range openingBrackets(path) {
			s := suffix
			results = append(results, parsedLink{
				path:   linkPart{index: linkStart + prefixLen + i + 1, text: string(pr[i+1:])},
				prefix: prefix, suffix: &s,
			})
		}
	}
	return results
}

const (
	// excluded characters of a path: '":; are in paths but are often separators; \ is left out to avoid catastrophic backtracking
	excludedPathChars      = "[^\\x00<>\\?" + jsSpace + "!`&*()'\":;\\\\]"
	excludedStartPathChars = "[^\\x00<>\\?" + jsSpace + "!`&*()\\[\\]'\":;\\\\]"
	pathPrefixClause       = `(?:\.\.?|\~|file://)`
)

// unixLocalLink matches the paths of Linux: /foo, ~/foo, ./foo, ../foo and foo/bar.
var unixLocalLink = regexp.MustCompile(`(?:(?:` + pathPrefixClause + `|(?:` + excludedStartPathChars + excludedPathChars + `*))?(?:/(?:` + excludedPathChars + `)+)+)`)

var (
	gitDiffLine = regexp.MustCompile(`^[-+]{3} [abciow12]/`)
	gitDiffText = regexp.MustCompile(`^[abciow12]/`)
)

func detectPathsNoSuffix(line string) []parsedLink {
	var results []parsedLink
	for _, m := range unixLocalLink.FindAllStringIndex(line, -1) {
		text := line[m[0]:m[1]]
		if text == "" {
			break
		}
		index := runeIndex(line, m[0])
		// the prefix of a git diff is not part of the path: --- a/foo/bar, +++ b/foo/bar, diff --git a/foo/bar b/foo/bar
		if (gitDiffLine.MatchString(line) && index == 4) || (strings.HasPrefix(line, "diff --git") && gitDiffText.MatchString(text)) {
			text = text[2:]
			index += 2
		}
		results = append(results, parsedLink{path: linkPart{index: index, text: text}})
	}
	return results
}
