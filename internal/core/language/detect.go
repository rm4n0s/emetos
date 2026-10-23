package language

import (
	"path"
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/glob"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/errors"
)

// Ported from VS Code: reference/vscode/src/vs/editor/common/services/languagesAssociations.ts and
// reference/vscode/src/vs/base/common/strings.ts (regExpLeadsToEndlessLoop) (MIT)

// association ties a language to a file name, an extension, a glob or a first line.
type association struct {
	id, mime  string
	filename  string
	extension string
	pattern   string
	// patternOnPath tells whether the pattern has a slash and so is matched on the whole path.
	patternOnPath bool
	matcher       *glob.Matcher
	firstLine     *regex.Regexp
}

// newPatternAssociation makes the association for a glob; it is matched without regard to case.
func newPatternAssociation(id, mime, pattern string) association {
	return association{
		id: id, mime: mime, pattern: pattern, patternOnPath: strings.Contains(pattern, "/"),
		matcher: glob.Parse(pattern, glob.Options{IgnoreCase: true}),
	}
}

// newFirstLineAssociation makes the association for the first line of a file. The expression
// is anchored at the start of the line; one that matches the empty text, which would make
// every file match, is left out (nil with no error), as VS Code does.
func newFirstLineAssociation(id, mime, expr string) (*association, error) {
	if !strings.HasPrefix(expr, "^") {
		expr = "^" + expr
	}
	re, err := regex.Compile(expr, regex.Options{Syntax: regex.ECMAScript})
	if err != nil {
		return nil, errors.New("FirstLineInvalid", "the first line pattern of a language does not compile", "language", id, "pattern", expr)
	}
	if leadsToEndlessLoop(expr, re) {
		return nil, nil
	}
	return &association{id: id, mime: mime, firstLine: re}, nil
}

// leadsToEndlessLoop tells whether the expression matches the empty text, except for the
// expressions that are meant to.
func leadsToEndlessLoop(source string, re *regex.Regexp) bool {
	switch source {
	case "^", "^$", "$", `^\s*$`:
		return false
	}
	_, ok, _ := re.Find(nil, 0)
	return ok
}

// SetFileAssociations sets the associations of the user, from the value of the setting
// files.associations: a map from a glob to a language id. They win over the associations of the
// languages. A pattern whose value is not a string is left out and listed in the returned
// errors (tag AssociationInvalid). Patterns with equal length are tried in alphabetical order.
func (r *Registry) SetFileAssociations(value any) []error {
	m, _ := value.(map[string]any)
	var errs []error
	var user []association
	for _, pattern := range sortedKeys(m) {
		id, ok := m[pattern].(string)
		if !ok {
			errs = append(errs, errors.New("AssociationInvalid", "the language of a file association is not a string", "pattern", pattern))
			continue
		}
		r.mu.RLock()
		mime := "text/x-" + id
		if l, has := r.languages[id]; has && len(l.MIMETypes) > 0 {
			mime = l.MIMETypes[0]
		}
		r.mu.RUnlock()
		user = append(user, newPatternAssociation(id, mime, pattern))
	}
	r.mu.Lock()
	r.user = user
	r.mu.Unlock()
	return errs
}

// sortedKeys gives the keys of a map in order.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Detect gives the id of the language of a file from its path and the text of its first line
// (the line may be empty): the associations of the user first, then those of the languages by
// file name, then glob, then extension, and the first line last. A file no language claims is
// plain text. For the same kind of match, the language registered last wins, and so does the
// longest glob or extension.
func (r *Registry) Detect(filePath, firstLine string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if filePath != "" {
		lower := strings.ToLower(filePath)
		name := path.Base(lower)
		if a := byPath(lower, name, r.user); a != nil {
			return a.id
		}
		if a := byPath(lower, name, r.platform); a != nil {
			return a.id
		}
	}
	firstLine = strings.TrimPrefix(firstLine, "\ufeff")
	if firstLine != "" {
		// The text is read up to its end of line, as a first line is.
		if i := strings.IndexAny(firstLine, "\r\n"); i >= 0 {
			firstLine = firstLine[:i]
		}
		text := []rune(firstLine)
		for i := len(r.all) - 1; i >= 0; i-- {
			a := r.all[i]
			if a.firstLine == nil {
				continue
			}
			if _, ok, _ := a.firstLine.Find(text, 0); ok {
				return a.id
			}
		}
	}
	return PlainTextID
}

// byPath finds the association for a path: an exact file name first, then the longest glob, then
// the longest extension. The associations registered last are tried first.
func byPath(lowerPath, name string, list []association) *association {
	var filename, pattern, extension *association
	for i := len(list) - 1; i >= 0; i-- {
		a := &list[i]
		if a.filename != "" && strings.EqualFold(name, a.filename) {
			filename = a
			break
		}
		if a.pattern != "" && (pattern == nil || len(a.pattern) > len(pattern.pattern)) {
			target := name
			if a.patternOnPath {
				target = lowerPath
			}
			if a.matcher.Match(target) {
				pattern = a
			}
		}
		if a.extension != "" && (extension == nil || len(a.extension) > len(extension.extension)) {
			if strings.HasSuffix(name, strings.ToLower(a.extension)) {
				extension = a
			}
		}
	}
	switch {
	case filename != nil:
		return filename
	case pattern != nil:
		return pattern
	}
	return extension
}
