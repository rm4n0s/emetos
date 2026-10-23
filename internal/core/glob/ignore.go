package glob

import (
	"regexp"
	"sort"
	"strings"
)

// IgnoreOptions tune an Ignore matcher.
type IgnoreOptions struct {
	// IgnoreCase makes the patterns match without regard to case (git's core.ignoreCase).
	IgnoreCase bool
}

// rule is one line of an ignore file.
type rule struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
}

// layer is the rules of one ignore file and the folder it sits in.
type layer struct {
	dir   string // slash-separated, relative to the root; "" for the root
	rules []rule
}

// Ignore decides which paths the ignore files of a tree (.gitignore, .ignore) exclude, with
// gitignore's rules (https://git-scm.com/docs/gitignore): a pattern with a slash in it (not
// only at the end) is relative to the folder of its file, any other matches at every depth;
// a slash at the end matches folders only; `!` re-includes a path an earlier pattern
// excluded; `*`, `?` and `[...]` do not match a slash; `**` matches any number of folders
// at the start, in the middle or at the end of a pattern; the last matching pattern of a
// file decides, a file in a deeper folder overrides one in a shallower folder, and a path
// below an excluded folder stays excluded, whatever the patterns say.
//
// Paths are relative to the root of the tree and use "/". An Ignore is built by adding the
// ignore files as the walk reads them, and is not safe for concurrent use while files are
// added.
type Ignore struct {
	opts   IgnoreOptions
	layers []layer // in the order they were added
	global []layer // lowest priority: .git/info/exclude and the user's global file
}

// NewIgnore makes an empty matcher: nothing is ignored.
func NewIgnore(opts IgnoreOptions) *Ignore {
	return &Ignore{opts: opts}
}

// AddFile adds the text of an ignore file found in folder dir ("" for the root, otherwise
// the folder relative to the root, such as "src/lib").
func (ig *Ignore) AddFile(dir, text string) {
	dir = strings.Trim(dir, "/")
	ig.layers = append(ig.layers, layer{dir: dir, rules: ig.parse(text)})
}

// AddGlobal adds the text of a file whose patterns apply to the whole tree with lower
// priority than every ignore file in it, such as .git/info/exclude or the user's global
// excludes file.
func (ig *Ignore) AddGlobal(text string) {
	ig.global = append(ig.global, layer{rules: ig.parse(text)})
}

// utf8BOM is the byte order mark some editors put at the start of a file.
const utf8BOM = "\xef\xbb\xbf"

// parse reads the lines of an ignore file into rules; blank lines and comments are skipped.
func (ig *Ignore) parse(text string) []rule {
	text = strings.TrimPrefix(text, utf8BOM)
	var rules []rule
	for _, line := range strings.Split(text, "\n") {
		if r, ok := ig.parseLine(strings.TrimSuffix(line, "\r")); ok {
			rules = append(rules, r)
		}
	}
	return rules
}

// parseLine reads one line. ok is false for a line that has no rule.
func (ig *Ignore) parseLine(line string) (rule, bool) {
	line = trimTrailingSpaces(line)
	if line == "" || line[0] == '#' {
		return rule{}, false
	}
	var r rule
	if line[0] == '!' {
		r.negate = true
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") && !strings.HasSuffix(line, `\/`) {
		r.dirOnly = true
		line = strings.TrimRight(line, "/")
	}
	if line == "" {
		return rule{}, false
	}
	anchored := strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	src, ok := ignoreRegExp(line, anchored)
	if !ok {
		return rule{}, false
	}
	if ig.opts.IgnoreCase {
		src = "(?i)" + src
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return rule{}, false
	}
	r.re = re
	return r, true
}

// trimTrailingSpaces removes spaces at the end of a line unless a backslash escapes them.
func trimTrailingSpaces(s string) string {
	end := len(s)
	for end > 0 && s[end-1] == ' ' {
		// count the backslashes before the space: an odd number escapes it
		n := 0
		for i := end - 2; i >= 0 && s[i] == '\\'; i-- {
			n++
		}
		if n%2 == 1 {
			break
		}
		end--
	}
	return s[:end]
}

// ignoreRegExp translates a gitignore pattern (without a leading "/" or a trailing "/") to
// a regular expression that matches the whole path relative to the folder of its file.
func ignoreRegExp(pattern string, anchored bool) (string, bool) {
	segments := strings.Split(pattern, "/")
	var b strings.Builder
	b.WriteString("^")
	if !anchored {
		b.WriteString("(?:.*/)?")
	}
	for i, seg := range segments {
		last := i == len(segments)-1
		if seg == "**" {
			switch {
			case last && i == 0:
				b.WriteString(".+") // "**" alone: everything
			case last:
				b.WriteString(".+") // "a/**": everything inside a
			default:
				b.WriteString("(?:.*/)?") // "**/" and "/**/": zero or more folders
			}
			continue
		}
		seg, ok := segmentRegExp(seg)
		if !ok {
			return "", false
		}
		b.WriteString(seg)
		if !last {
			b.WriteString("/")
		}
	}
	b.WriteString("$")
	return b.String(), true
}

// segmentRegExp translates the wildcards of one path segment.
func segmentRegExp(seg string) (string, bool) {
	var b strings.Builder
	runes := []rune(seg)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch c {
		case '\\':
			if i+1 >= len(runes) {
				return "", false // a trailing backslash is not a valid pattern
			}
			i++
			b.WriteString(regexp.QuoteMeta(string(runes[i])))
		case '*':
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		case '[':
			class, next, ok := bracketClass(runes, i)
			if !ok {
				return "", false // no closing bracket: git matches nothing
			}
			b.WriteString(class)
			i = next
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String(), true
}

// bracketClass translates the character class that starts at runes[start] == '['. It
// returns the class, the index of its closing ']', and false when there is none.
func bracketClass(runes []rune, start int) (string, int, bool) {
	i := start + 1
	var b strings.Builder
	b.WriteString("[")
	if i < len(runes) && (runes[i] == '!' || runes[i] == '^') {
		b.WriteString("^")
		i++
	}
	first := true
	for ; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == ']' && !first:
			b.WriteString("]")
			return b.String(), i, true
		case c == '[' && i+1 < len(runes) && runes[i+1] == ':':
			// a POSIX class such as [:alpha:]
			if end := indexRunes(runes, i+2, ":]"); end >= 0 {
				b.WriteString(string(runes[i : end+2]))
				i = end + 1
				first = false
				continue
			}
			b.WriteString(`\[`)
		case c == '\\' && i+1 < len(runes):
			i++
			b.WriteString(regexp.QuoteMeta(string(runes[i])))
		case c == '-' && !first && i+1 < len(runes) && runes[i+1] != ']':
			b.WriteString("-")
		case c == '/':
			// a slash is never part of a class
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
		first = false
	}
	return "", 0, false
}

// indexRunes finds sep in runes from start, as an index into runes, or -1.
func indexRunes(runes []rune, start int, sep string) int {
	s := []rune(sep)
	for i := start; i+len(s) <= len(runes); i++ {
		if string(runes[i:i+len(s)]) == sep {
			return i
		}
	}
	return -1
}

// Check tells how the patterns treat one path, ignoring its parent folders: ignored is
// whether the last matching pattern excludes it, matched whether any pattern matched. A
// walk that does not enter ignored folders can use it for every entry it meets.
func (ig *Ignore) Check(path string, isDir bool) (ignored, matched bool) {
	path = strings.Trim(path, "/")
	if path == "" {
		return false, false
	}
	// Deeper folders first; of equal depth, a file added later first (.ignore after
	// .gitignore overrides it).
	var applicable []layer
	for i := len(ig.layers) - 1; i >= 0; i-- {
		if l := ig.layers[i]; l.dir == "" || strings.HasPrefix(path, l.dir+"/") {
			applicable = append(applicable, l)
		}
	}
	sort.SliceStable(applicable, func(i, j int) bool {
		return strings.Count(applicable[i].dir, "/")+boolInt(applicable[i].dir != "") >
			strings.Count(applicable[j].dir, "/")+boolInt(applicable[j].dir != "")
	})
	for _, l := range append(applicable, ig.global...) {
		rel := path
		if l.dir != "" {
			rel = strings.TrimPrefix(path, l.dir+"/")
		}
		for i := len(l.rules) - 1; i >= 0; i-- {
			r := l.rules[i]
			if r.dirOnly && !isDir {
				continue
			}
			if r.re.MatchString(rel) {
				return !r.negate, true
			}
		}
	}
	return false, false
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Ignored tells whether a path is excluded: a pattern excludes it, or it lies below a
// folder that is excluded. isDir says whether the path is a folder.
func (ig *Ignore) Ignored(path string, isDir bool) bool {
	path = strings.Trim(path, "/")
	if path == "" {
		return false
	}
	parts := strings.Split(path, "/")
	for i := 1; i < len(parts); i++ {
		if ignored, _ := ig.Check(strings.Join(parts[:i], "/"), true); ignored {
			return true
		}
	}
	ignored, _ := ig.Check(path, isDir)
	return ignored
}
