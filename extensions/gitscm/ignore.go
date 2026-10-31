package gitscm

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ignoreRule is one line of a .gitignore file.
type ignoreRule struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
}

// parseIgnore reads the text of a .gitignore (or info/exclude) file: blank lines and comments are skipped, "!" negates, a trailing
// "/" matches folders only, a pattern with a "/" in it (not at the end) is anchored to the folder of the file, "**" matches
// any folders, "*" and "?" match inside a name, and "[...]" is a class (git help gitignore).
func parseIgnore(text string) []ignoreRule {
	var rules []ignoreRule
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			r.negate = true
			line = line[1:]
		} else if strings.HasPrefix(line, `\!`) || strings.HasPrefix(line, `\#`) {
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly = true
			line = strings.TrimRight(line, "/")
		}
		if line == "" {
			continue
		}
		anchored := strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")
		re, err := regexp.Compile(ignorePattern(line, anchored))
		if err != nil {
			continue // git skips a pattern it cannot read too
		}
		r.re = re
		rules = append(rules, r)
	}
	return rules
}

// ignorePattern turns a gitignore pattern into a regular expression over the path relative to the folder of the file.
func ignorePattern(p string, anchored bool) string {
	var b strings.Builder
	b.WriteString("^")
	if !anchored {
		b.WriteString("(?:.*/)?")
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '*' && strings.HasPrefix(p[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case c == '*' && strings.HasPrefix(p[i:], "/**") && i+3 == len(p):
			b.WriteString("/.*")
			i += 2
		case c == '*' && strings.HasPrefix(p[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			j := strings.IndexByte(p[i+1:], ']')
			if j < 0 {
				b.WriteString(`\[`)
				break
			}
			class := p[i+1 : i+1+j]
			class = strings.Replace(class, "!", "^", 1)
			b.WriteString("[" + class + "]")
			i += j + 1
		case c == '\\' && i+1 < len(p):
			i++
			b.WriteString(regexp.QuoteMeta(string(p[i])))
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return b.String()
}

// match tells what the rules say about a path relative to their folder: ignored, or un-ignored by a "!" rule, or nothing (found is false).
// The last rule that matches wins.
func matchIgnore(rules []ignoreRule, rel string, isDir bool) (ignored, found bool) {
	for i := len(rules) - 1; i >= 0; i-- {
		r := rules[i]
		if r.dirOnly && !isDir {
			continue
		}
		if r.re.MatchString(rel) {
			return !r.negate, true
		}
	}
	return false, false
}

// ignoreFile is a parsed ignore file with the time it was read.
type ignoreFile struct {
	rules []ignoreRule
	mtime time.Time
	size  int64
}

// ignoreMatcher answers "does git ignore this path?" for the paths of one work tree, from its .gitignore files and its
// info/exclude, without starting git (SPEC §10.30 "Refresh": the events of ignored paths are dropped with a matcher before they
// trigger a status). Files are read when first needed and again when they change. It does not know the global excludes file
// (core.excludesFile): a path it does not know is ignored by git still triggers one status, which is the safe side.
type ignoreMatcher struct {
	root     string
	exclude  string // the path of info/exclude
	mu       sync.Mutex
	files    map[string]*ignoreFile
	maxBytes int64
}

// newIgnoreMatcher makes the matcher of the work tree at root whose git directory is gitDir.
func newIgnoreMatcher(root, commonDir string) *ignoreMatcher {
	return &ignoreMatcher{root: root, exclude: filepath.Join(commonDir, "info", "exclude"), files: map[string]*ignoreFile{}, maxBytes: 1 << 20}
}

// load returns the rules of a file, read again when its size or time changed; a missing file has none.
func (m *ignoreMatcher) load(path string) []ignoreRule {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > m.maxBytes {
		delete(m.files, path)
		return nil
	}
	if f := m.files[path]; f != nil && f.size == info.Size() && f.mtime.Equal(info.ModTime()) {
		return f.rules
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	f := &ignoreFile{rules: parseIgnore(string(data)), mtime: info.ModTime(), size: info.Size()}
	m.files[path] = f
	return f.rules
}

// Ignored reports whether git ignores the path (absolute, below the root): the path itself or a folder above it matches an
// ignore rule and no later rule un-ignores it.
func (m *ignoreMatcher) Ignored(path string, isDir bool) bool {
	rel, err := filepath.Rel(m.root, path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	// the files that apply: the root .gitignore and info/exclude, then the .gitignore of every folder above the path
	for i := range parts {
		sub := strings.Join(parts[:i+1], "/")
		dir := i+1 < len(parts) || isDir
		if m.ignoredAt(parts, i, sub, dir) {
			return true
		}
	}
	return false
}

// ignoredAt says whether the element parts[i] (sub is the path up to it) is ignored by the rules of the files above it.
func (m *ignoreMatcher) ignoredAt(parts []string, i int, sub string, isDir bool) bool {
	ignored := false
	apply := func(rules []ignoreRule, rel string) {
		if hit, found := matchIgnore(rules, rel, isDir); found {
			ignored = hit
		}
	}
	apply(m.load(m.exclude), sub)
	apply(m.load(filepath.Join(m.root, ".gitignore")), sub)
	for j := 1; j <= i; j++ { // the .gitignore of the folder parts[:j]
		dir := strings.Join(parts[:j], "/")
		apply(m.load(filepath.Join(m.root, filepath.FromSlash(dir), ".gitignore")), strings.Join(parts[j:i+1], "/"))
	}
	return ignored
}
