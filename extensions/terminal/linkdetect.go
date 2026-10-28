package terminal

import (
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Ported from VS Code: src/vs/editor/common/languages/linkComputer.ts (the state machine that finds http://, https:// and
// file:// links) and src/vs/workbench/contrib/terminalContrib/links/browser/terminalLocalLinkDetector.ts (the file links).
//
// The detectors take the text of one logical line (the rows an autowrap joined) and give ranges in characters of that text;
// links.go turns them into ranges of cells and checks the files against the disk.

const (
	// maxLinkLine is the longest line the detectors look at.
	maxLinkLine = 2000
	// maxLinksInLine is the most links of a line that are checked against the file system.
	maxLinksInLine = 10
	// maxLinkLength is the longest link that is checked against the file system; maxWebLinkLength is the longest address.
	maxLinkLength    = 1024
	maxWebLinkLength = 2048
)

// webLink is an address found in a line: the characters [start, end) of the line, and the address.
type webLink struct {
	start, end int
	url        string
}

// The states of the machine that recognizes the beginning of an address.
const (
	stInvalid = iota
	stStart
	stH
	stHT
	stHTT
	stHTTP
	stF
	stFI
	stFIL
	stBeforeColon
	stAfterColon
	stAlmostThere
	stEnd
	stAccept
)

// nextState is the machine of http://, https:// and file:// (the letters in either case).
func nextState(state int, c rune) int {
	lower := c
	if c >= 'A' && c <= 'Z' {
		lower = c + 'a' - 'A'
	}
	switch state {
	case stStart:
		switch lower {
		case 'h':
			return stH
		case 'f':
			return stF
		}
	case stH:
		if lower == 't' {
			return stHT
		}
	case stHT:
		if lower == 't' {
			return stHTT
		}
	case stHTT:
		if lower == 'p' {
			return stHTTP
		}
	case stHTTP:
		switch {
		case lower == 's':
			return stBeforeColon
		case c == ':':
			return stAfterColon
		}
	case stF:
		if lower == 'i' {
			return stFI
		}
	case stFI:
		if lower == 'l' {
			return stFIL
		}
	case stFIL:
		if lower == 'e' {
			return stBeforeColon
		}
	case stBeforeColon:
		if c == ':' {
			return stAfterColon
		}
	case stAfterColon:
		if c == '/' {
			return stAlmostThere
		}
	case stAlmostThere:
		if c == '/' {
			return stEnd
		}
	}
	return stInvalid
}

// The classes of the characters of an address.
const (
	classNone = iota
	classForceTermination
	classCannotEndIn
)

// forceTermination are the characters that end an address.
const forceTermination = " \t<>'\"、。｡､，．：；‘〈「『〔（［｛｢｣｝］）〕』」〉’｀～…|"

func classOf(c rune) int {
	switch {
	case strings.ContainsRune(forceTermination, c):
		return classForceTermination
	case c == '.' || c == ',' || c == ';' || c == ':':
		return classCannotEndIn
	}
	return classNone
}

// createWebLink makes the link [begin, end) of a line: it does not end in a character that cannot end an address, nor in a
// closing bracket whose opening one is before it.
func createWebLink(line []rune, begin, end int) webLink {
	last := end - 1
	for classOf(line[last]) == classCannotEndIn {
		last--
		if last <= begin {
			break
		}
	}
	if begin > 0 {
		before, lastIn := line[begin-1], line[last]
		if (before == '(' && lastIn == ')') || (before == '[' && lastIn == ']') || (before == '{' && lastIn == '}') {
			last--
		}
	}
	return webLink{start: begin, end: last + 1, url: string(line[begin : last+1])}
}

// detectWebLinks finds the http://, https:// and file:// addresses of a line.
func detectWebLinks(text string) []webLink {
	line := []rune(text)
	var out []webLink
	state := stStart
	begin := 0
	var beginCh rune
	var openParen, openSquare, inSquare, openCurly bool
	for j := 0; j < len(line); j++ {
		c := line[j]
		reset := false
		switch state {
		case stAccept:
			var cl int
			switch c {
			case '(':
				openParen, cl = true, classNone
			case ')':
				cl = classForceTermination
				if openParen {
					cl = classNone
				}
			case '[':
				inSquare, openSquare, cl = true, true, classNone
			case ']':
				inSquare = false
				cl = classForceTermination
				if openSquare {
					cl = classNone
				}
			case '{':
				openCurly, cl = true, classNone
			case '}':
				cl = classForceTermination
				if openCurly {
					cl = classNone
				}
			case '\'', '"', '`':
				// quotes are part of an address only when another quote began it
				switch beginCh {
				case c:
					cl = classForceTermination
				case '\'', '"', '`':
					cl = classNone
				default:
					cl = classForceTermination
				}
			case '*':
				cl = classNone
				if beginCh == '*' {
					cl = classForceTermination
				}
			case ' ':
				// a space is allowed between [ and ]
				cl = classForceTermination
				if inSquare {
					cl = classNone
				}
			default:
				cl = classOf(c)
			}
			if cl == classForceTermination {
				out = append(out, createWebLink(line, begin, j))
				reset = true
			}
		case stEnd:
			var cl int
			if c == '[' {
				// the host may be an IPv6 address
				openSquare, cl = true, classNone
			} else {
				cl = classOf(c)
			}
			if cl == classForceTermination {
				reset = true
			} else {
				state = stAccept
			}
		default:
			state = nextState(state, c)
			if state == stInvalid {
				reset = true
			}
		}
		if reset {
			state = stStart
			openParen, openSquare, openCurly = false, false, false
			begin, beginCh = j+1, c
		}
	}
	if state == stAccept {
		out = append(out, createWebLink(line, begin, len(line)))
	}
	return out
}

// fileLink is a file named in a line of text: the characters [start, end) of the line to underline, the text of the link, the
// file and the line and column the text names (none when it names none).
type fileLink struct {
	start, end               int
	text                     string
	path                     string
	row, col, rowEnd, colEnd int
}

// linkEnv is what the file links need to know about the world: the working folder of the terminal, the home folder, and
// whether a path names a file the user may open. The stat function gets an absolute path.
type linkEnv struct {
	cwd, home string
	stat      func(path string) (ok bool)
}

// fallbackMatchers catch the paths the regular detection cannot, mostly those with spaces. Ported from the fallbackMatchers of
// terminalLocalLinkDetector.ts.
var fallbackMatchers = []*regexp.Regexp{
	// Python style error: File "<path>", line <line>
	regexp.MustCompile(`^ *File (?P<link>"(?P<path>.+)"(, line (?P<line>\d+))?)`),
	// Unknown tool #200166: FILE  <path>:<line>:<col>
	regexp.MustCompile(`^ +FILE +(?P<link>(?P<path>.+)(?::(?P<line>\d+)(?::(?P<col>\d+))?)?)`),
	// Some C++ compile error formats: foo bar(339) : error ...   foo bar(339,12): ...   foo bar(339, 12): ...
	regexp.MustCompile(`^(?P<link>(?P<path>.+)\((?P<line>\d+)(?:, ?(?P<col>\d+))?\)) ?:`),
	// foo/bar baz:339 : error ...   foo/bar baz:339:12: error ...
	regexp.MustCompile(`^(?P<link>(?P<path>.+):(?P<line>\d+)(?::(?P<col>\d+))?) ?:`),
	// PowerShell and cmd prompt
	regexp.MustCompile(`^(?:PS\s+)?(?P<link>(?P<path>[^>]+))>`),
	// The whole line is the path
	regexp.MustCompile(`^ *(?P<link>(?P<path>.+))`),
}

var (
	dotDotPrefix   = regexp.MustCompile(`^(\.\.[/\\])+`)
	specialEndChar = regexp.MustCompile(`[\[\]"'.]$`)
)

// resolveLinkPath turns the text of a link into the absolute path it names, without checking that it exists: ~ is the home
// folder, a file:// address is the path of its URL, a relative path is below the working folder. The line and column and the
// query string of the link are not part of the path. It reports false for a link that names nothing.
func (e linkEnv) resolveLinkPath(link string) (string, bool) {
	if strings.HasPrefix(link, "file://") {
		u, err := url.Parse(removeLinkSuffix(link))
		if err != nil || u.Path == "" {
			return "", false
		}
		return filepath.Clean(u.Path), true
	}
	p := removeLinkQueryString(removeLinkSuffix(link))
	if p == "" {
		return "", false
	}
	switch {
	case strings.HasPrefix(p, "~"):
		if e.home == "" {
			return "", false
		}
		return filepath.Join(e.home, p[1:]), true
	case strings.HasPrefix(p, "/"):
		return filepath.Clean(p), true
	}
	if e.cwd == "" {
		return "", false
	}
	return filepath.Join(e.cwd, p), true
}

// validate returns the first candidate that names a file.
func (e linkEnv) validate(candidates []string) (link, path string, ok bool) {
	for _, c := range candidates {
		if p, ok := e.resolveLinkPath(c); ok && e.stat(p) {
			return c, p, true
		}
	}
	return "", "", false
}

// detectFileLinks finds the files named in a line of text: the paths the parser finds that exist (or the fallback matchers
// when it found none).
func detectFileLinks(text string, env linkEnv) []fileLink {
	n := utf8.RuneCountInString(text)
	if text == "" || n > maxLinkLine {
		return nil
	}
	runes := []rune(text)
	var links []fileLink
	count := 0
	for _, pl := range detectLinks(text) {
		if utf8.RuneCountInString(pl.path.text) > maxLinkLength {
			continue
		}
		start := pl.path.index
		if pl.prefix != nil {
			start = pl.prefix.index
		}
		end := pl.path.end()
		if pl.suffix != nil {
			end += utf8.RuneCountInString(pl.suffix.suffix.text)
		}
		candidates := []string{pl.path.text}
		if !strings.HasPrefix(pl.path.text, "/") && !strings.HasPrefix(pl.path.text, "~") && !strings.HasPrefix(pl.path.text, "file://") {
			if dotDotPrefix.MatchString(pl.path.text) {
				candidates = append(candidates, dotDotPrefix.ReplaceAllString(pl.path.text, ""))
			}
		}
		// a candidate that ends in a character that is probably not part of the link gets another without it; the underline
		// is shortened by the characters left out, unless the link has a suffix (the underline would end in the wrong place)
		trims := map[string]int{}
		var extra []string
		for _, c := range candidates {
			prev, trim := c, 0
			removed := specialEndChar.ReplaceAllString(prev, "")
			for removed != prev {
				if pl.suffix == nil {
					trim++
				}
				extra = append(extra, removed)
				trims[removed] = trim
				prev = removed
				removed = specialEndChar.ReplaceAllString(removed, "")
			}
		}
		candidates = append(candidates, extra...)
		if link, path, ok := env.validate(candidates); ok {
			end -= trims[link]
			fl := fileLink{start: start, end: end, text: string(runes[start:min(end, len(runes))]), path: path, row: none, col: none, rowEnd: none, colEnd: none}
			if pl.suffix != nil {
				fl.text = string(runes[start:min(pl.suffix.suffix.end(), len(runes))])
				fl.row, fl.col, fl.rowEnd, fl.colEnd = pl.suffix.row, pl.suffix.col, pl.suffix.rowEnd, pl.suffix.colEnd
			}
			links = append(links, fl)
		}
		count++
		if count >= maxLinksInLine {
			break
		}
	}
	if len(links) > 0 {
		return links
	}
	for _, re := range fallbackMatchers {
		m := re.FindStringSubmatchIndex(text)
		if m == nil {
			continue
		}
		group := func(name string) string {
			i := re.SubexpIndex(name)
			if i < 0 || m[2*i] < 0 {
				return ""
			}
			return text[m[2*i]:m[2*i+1]]
		}
		link, path := group("link"), group("path")
		if link == "" || path == "" || utf8.RuneCountInString(link) > maxLinkLength {
			continue
		}
		// the link is found in the text by its first occurrence, as VS Code does
		byteStart := strings.Index(text, link)
		if byteStart < 0 {
			continue
		}
		start := runeIndex(text, byteStart)
		fl := fileLink{start: start, end: start + utf8.RuneCountInString(link), text: link, row: none, col: none, rowEnd: none, colEnd: none}
		if line := group("line"); line != "" {
			fl.row, _ = strconv.Atoi(line)
			if col := group("col"); col != "" {
				fl.col, _ = strconv.Atoi(col)
			}
		}
		if p, ok := env.resolveLinkPath(path); ok && env.stat(p) {
			fl.path = p
			// the matchers overlap (the last takes the whole line): one link for a place
			if !slices.ContainsFunc(links, func(o fileLink) bool { return o.start == fl.start && o.end == fl.end }) {
				links = append(links, fl)
			}
		}
	}
	return links
}
