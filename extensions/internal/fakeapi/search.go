package fakeapi

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The fake of the search across files (step 17e.2, SPEC §10.27): FindTextInFiles looks through the files of the folder of the
// fake with the standard library's regular expressions, so a test sees results without Emetos's engine (which an extension
// cannot import). It is simple on purpose: the include and exclude boxes match a path that contains one of their comma-separated
// words, the ignore files and the settings are not read, and the files come sorted. The engine's own rules are tested in
// internal/core/search and through the real host.

// SearchCall is a call of Workspace().FindTextInFiles.
type SearchCall struct {
	Query   extension.TextSearchQuery
	Options extension.TextSearchOptions
}

// SearchCalls are the calls of FindTextInFiles, the first first.
func (a *API) SearchCalls() []SearchCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.searches)
}

// ReplaceCalls are the files the extension passed to ReplaceInFiles, the first first.
func (a *API) ReplaceCalls() [][]extension.FileReplacement {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.replaceCalls)
}

// searchRegexp makes the regular expression of a query.
func searchRegexp(q extension.TextSearchQuery) (*regexp.Regexp, error) {
	if q.Pattern == "" {
		return nil, errors.New("FakeSearchPatternEmpty", "there is nothing to search for")
	}
	p := q.Pattern
	if !q.IsRegExp {
		p = regexp.QuoteMeta(p)
	}
	if q.IsWordMatch {
		p = `\b` + p + `\b`
	}
	cs := q.IsCaseSensitive || (q.IsSmartCase && strings.ToLower(q.Pattern) != q.Pattern)
	if !cs {
		p = "(?i)" + p
	}
	if q.IsMultiline || strings.Contains(q.Pattern, "\n") {
		p = "(?m)" + p
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, errors.NewErr("FakeSearchQueryInvalid", err).SetMetadata("pattern", q.Pattern)
	}
	return re, nil
}

// words splits the text of an include or exclude box at its commas.
func words(box string) []string {
	var out []string
	for _, w := range strings.Split(box, ",") {
		if w = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(w), "./")); w != "" {
			out = append(out, w)
		}
	}
	return out
}

func containsAny(path string, words []string) bool {
	for _, w := range words {
		if strings.Contains(path, strings.Trim(w, "*")) {
			return true
		}
	}
	return false
}

// FindTextInFiles implements extension.Workspace.
func (w workspace) FindTextInFiles(ctx context.Context, q extension.TextSearchQuery, opts extension.TextSearchOptions, onFile func(extension.TextSearchFile)) (extension.TextSearchSummary, error) {
	a := w.a
	a.mu.Lock()
	a.searches = append(a.searches, SearchCall{Query: q, Options: opts})
	folder := a.info.Folder
	unsaved := map[string]string{}
	for uri, text := range a.docs {
		unsaved[strings.TrimPrefix(uri, "file://")] = text
	}
	a.mu.Unlock()
	if folder == "" {
		return extension.TextSearchSummary{}, errors.New("FakeNoFolder", "the scope has no folder")
	}
	re, err := searchRegexp(q)
	if err != nil {
		return extension.TextSearchSummary{}, err
	}
	include, exclude := words(opts.Include), words(opts.Exclude)
	limit := opts.MaxResults
	if limit <= 0 {
		limit = 20000
	}
	var sum extension.TextSearchSummary
	var paths []string
	_ = filepath.WalkDir(folder, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if d.Type().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	})
	for p := range unsaved {
		if !slices.Contains(paths, p) && strings.HasPrefix(p, folder+"/") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		rel, _ := filepath.Rel(folder, path)
		rel = filepath.ToSlash(rel)
		if len(include) > 0 && !containsAny(rel, include) || containsAny(rel, exclude) {
			continue
		}
		text, ok := unsaved[path]
		if !ok {
			if opts.OnlyOpenEditors {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil || !utf8.Valid(data) {
				continue
			}
			text = string(data)
		}
		sum.Searched++
		f := extension.TextSearchFile{Path: rel, URI: "file://" + path}
		for _, loc := range re.FindAllStringIndex(text, -1) {
			if loc[1] <= loc[0] {
				continue
			}
			if sum.Matches >= limit {
				sum.LimitHit = true
				break
			}
			line := strings.Count(text[:loc[0]], "\n")
			ls := strings.LastIndexByte(text[:loc[0]], '\n') + 1
			le := strings.IndexByte(text[ls:], '\n')
			if le < 0 {
				le = len(text) - ls
			}
			endLine := line + strings.Count(text[loc[0]:loc[1]], "\n")
			endCol := loc[1] - ls
			if endLine != line {
				endCol = loc[1] - (strings.LastIndexByte(text[:loc[1]], '\n') + 1)
			}
			end := min(loc[1]-ls, le)
			f.Matches = append(f.Matches, extension.TextSearchMatch{
				Range:   extension.Range{Start: extension.Position{Line: line, Col: loc[0] - ls}, End: extension.Position{Line: endLine, Col: endCol}},
				Preview: text[ls : ls+le], PreviewStart: loc[0] - ls, PreviewEnd: end, Text: text[loc[0]:loc[1]],
			})
			sum.Matches++
		}
		if len(f.Matches) > 0 {
			sum.Files++
			onFile(f)
		}
		if sum.LimitHit {
			break
		}
	}
	return sum, nil
}

// fakeReplacer replaces with the standard library's expansion of $1.
type fakeReplacer struct {
	re           *regexp.Regexp
	replacement  string
	isRegExp     bool
	preserveCase bool
}

// Replace implements extension.TextReplacer.
func (r fakeReplacer) Replace(matchText string) string {
	out := r.replacement
	if r.isRegExp {
		if loc := r.re.FindStringSubmatchIndex(matchText); loc != nil {
			out = string(r.re.ExpandString(nil, r.replacement, matchText, loc))
		}
	}
	if r.preserveCase && matchText != "" {
		switch {
		case matchText == strings.ToUpper(matchText):
			out = strings.ToUpper(out)
		case matchText == strings.ToLower(matchText):
			out = strings.ToLower(out)
		case out != "" && strings.ToUpper(matchText[:1]) == matchText[:1]:
			out = strings.ToUpper(out[:1]) + out[1:]
		}
	}
	return out
}

// NewTextReplacer implements extension.Workspace.
func (w workspace) NewTextReplacer(q extension.TextSearchQuery, replacement string, preserveCase bool) (extension.TextReplacer, error) {
	re, err := searchRegexp(q)
	if err != nil {
		return nil, err
	}
	return fakeReplacer{re: re, replacement: replacement, isRegExp: q.IsRegExp, preserveCase: preserveCase}, nil
}

// ReplaceInFiles implements extension.Workspace: the files of the folder are changed on disk; a replacement whose range does not
// have the expected text is skipped.
func (w workspace) ReplaceInFiles(ctx context.Context, files []extension.FileReplacement) (extension.ReplaceReport, error) {
	a := w.a
	a.mu.Lock()
	a.replaceCalls = append(a.replaceCalls, files)
	folder := a.info.Folder
	a.mu.Unlock()
	var report extension.ReplaceReport
	if folder == "" {
		return report, errors.New("FakeNoFolder", "the scope has no folder")
	}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if !fs.ValidPath(f.Path) {
			return report, errors.New("FakePathInvalid", "the path is relative to the folder, with forward slashes and no ..", "path", f.Path)
		}
		path := filepath.Join(folder, filepath.FromSlash(f.Path))
		data, err := os.ReadFile(path)
		if err != nil {
			report.Failed = append(report.Failed, extension.ReplaceFailure{Path: f.Path, Message: err.Error()})
			continue
		}
		var edits []extension.RangeEdit
		for _, r := range f.Replacements {
			if got, err := ApplyRangeEdits(string(data), []extension.RangeEdit{{Range: r.Range, Text: ""}}); err != nil || textBetween(string(data), got) != r.Expected {
				report.Skipped++
				continue
			}
			edits = append(edits, extension.RangeEdit{Range: r.Range, Text: r.Text})
		}
		if len(edits) == 0 {
			continue
		}
		out, err := ApplyRangeEdits(string(data), edits)
		if err != nil {
			report.Failed = append(report.Failed, extension.ReplaceFailure{Path: f.Path, Message: err.Error()})
			continue
		}
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			report.Failed = append(report.Failed, extension.ReplaceFailure{Path: f.Path, Message: err.Error()})
			continue
		}
		report.Replaced += len(edits)
		report.Files = append(report.Files, f.Path)
	}
	return report, nil
}

// textBetween is the text that deleting a range took out of before: the part of before that after does not have.
func textBetween(before, after string) string {
	n := len(before) - len(after)
	i := 0
	for i < len(after) && before[i] == after[i] {
		i++
	}
	return before[i : i+n]
}
