package kit

import (
	"regexp"
	"strconv"
	"strings"
)

// The links in the output of a program (SPEC §10.31): a web address, or a path with a line and a column, in a line of text. The
// detection is VS Code's; what a link opens, and whether a path really is a file, is up to the owner of the text (AnsiLinks).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/linkDetector.ts (detectLinks, the regular expressions)

// LinkKind says what a LinkPart is.
type LinkKind int

const (
	// LinkText is text that is no link.
	LinkText LinkKind = iota
	// LinkWeb is an address with a scheme (https://, file://, data:) or one that starts with www.
	LinkWeb
	// LinkPath is a path on the computer, absolute or relative, with a line and a column after it when the text has them.
	LinkPath
)

// LinkPart is a part of a line: a text, or a link.
type LinkPart struct {
	Kind  LinkKind
	Value string
	// Path, Line and Column are the parts of a link of kind LinkPath (Line and Column are 0 when the text has none).
	Path         string
	Line, Column int
	// Index is where the part starts in the line, in bytes.
	Index int
}

const linkControlCodes = `\x00-\x20\x{7f}-\x{9f}`

var (
	webLinkRegex = regexp.MustCompile(`(?:[a-zA-Z][a-zA-Z0-9+.-]{2,}://|data:|www\.)[^\s` + linkControlCodes + `"]{2,}[^\s` + linkControlCodes + `"')}\],:;.!?]`)
	// posixPath is a path that may start with ~ or dots, then a line and a column (":12:34", ":line 6", ":line 6:10").
	pathLinkRegex = regexp.MustCompile("((?:~|\\.+)?(?:/[\\w\\s.@\\-()\\[\\]{}!#$%^&'`~+=]+)+)(?::(?:line\\s+)?([\\d]+))?(?::([\\d]+))?")
	// lineColumnRegex finds the ":12:34" at the end of the path of an address.
	lineColumnRegex = regexp.MustCompile(`:(?:line\s+)?([\d]+)(?::([\d]+))?$`)
)

// MaxLinkLineLength is the length of a line past which no link is looked for (VS Code's MAX_LENGTH).
const MaxLinkLineLength = 2000

// DetectLinks splits the text into links and the text between them, in order. The text is one line: use SplitLinkLines for the text of an
// output. A line longer than MaxLinkLineLength has no links.
func DetectLinks(text string) []LinkPart {
	if len(text) > MaxLinkLineLength {
		return []LinkPart{{Kind: LinkText, Value: text}}
	}
	var out []LinkPart
	var split func(text string, regex, base int)
	split = func(text string, regex, base int) {
		if regex > 1 {
			out = append(out, LinkPart{Kind: LinkText, Value: text, Index: base})
			return
		}
		re := webLinkRegex
		if regex == 1 {
			re = pathLinkRegex
		}
		cur := 0
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			if before := text[cur:m[0]]; before != "" {
				split(before, regex+1, base+cur)
			}
			part := LinkPart{Kind: LinkWeb, Value: text[m[0]:m[1]], Index: base + m[0]}
			if regex == 1 {
				part.Kind = LinkPath
				group := func(i int) string {
					if m[2*i] < 0 {
						return ""
					}
					return text[m[2*i]:m[2*i+1]]
				}
				part.Path = group(1)
				part.Line, _ = strconv.Atoi(group(2))
				part.Column, _ = strconv.Atoi(group(3))
			}
			out = append(out, part)
			cur = m[1]
		}
		if after := text[cur:]; after != "" {
			split(after, regex+1, base+cur)
		}
	}
	split(text, 0, 0)
	return out
}

// SplitLinkLines splits a text at its line feeds, keeping them at the end of each line; a link never spans lines. The empty string
// after a last line feed is not a line.
func SplitLinkLines(text string) []string {
	lines := strings.SplitAfter(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// WebLinkPosition cuts the ":line:column" off the end of an address (an address that ends like "foo.js:12:3" reveals that place of the
// file) and returns the address without it with the line and the column, or the address as it is with zeros.
func WebLinkPosition(address string) (rest string, line, column int) {
	m := lineColumnRegex.FindStringSubmatchIndex(address)
	if m == nil {
		return address, 0, 0
	}
	line, _ = strconv.Atoi(address[m[2]:m[3]])
	if m[4] >= 0 {
		column, _ = strconv.Atoi(address[m[4]:m[5]])
	}
	return address[:m[0]], line, column
}
