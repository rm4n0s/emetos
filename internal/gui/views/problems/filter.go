package problems

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/glob"
)

// FilterOptions are what narrows the markers the Problems panel shows.
type FilterOptions struct {
	// Text is the text of the filter box: words that match the message, source and code of a
	// marker and the name of its file; a leading "!" shows what does not match; patterns separated
	// by commas are globs of files (`**/*.go`, `.go`, `vendor`), and a "!" before one hides its
	// files; `@source:name` (`!@source:name` to exclude) keeps the markers of a source.
	Text string
	// Severities are the severities shown; the zero value shows none.
	Severities Severities
	// ActiveFileOnly shows only the markers of ActiveFile.
	ActiveFileOnly bool
	// ActiveFile is the absolute path of the file of the active editor, or empty.
	ActiveFile string
	// Exclude, when set, tells the files that are hidden for being excluded (files.exclude).
	Exclude func(path string) bool
}

// DefaultFilter shows every severity of every file, as the panel opens.
func DefaultFilter() FilterOptions { return FilterOptions{Severities: All} }

// sourceFilter is `@source:name` in the filter text.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersFilterOptions.ts (SOURCE_FILTER_REGEX)
var sourceFilter = regexp.MustCompile(`(?i)(!)?@source:("[^"]*"|[^\s,]+)(\s*)`)

// filter is FilterOptions parsed.
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markersFilterOptions.ts (FilterOptions)
// and src/vs/workbench/contrib/markers/browser/markersTreeViewer.ts (Filter)
type filter struct {
	opts FilterOptions
	// text is the filter text without the source filters and the leading "!", lower case, and
	// negate says it had the "!".
	text   string
	negate bool
	// includeSources and excludeSources are the sources to keep and to drop, lower case.
	includeSources, excludeSources []string
	// includes and excludes match the paths of files.
	includes, excludes []*glob.Matcher
}

// parseFilter reads the options.
func parseFilter(opts FilterOptions) *filter {
	f := &filter{opts: opts}
	text := strings.TrimSpace(opts.Text)
	for {
		loc := sourceFilter.FindStringSubmatchIndex(text)
		if loc == nil {
			break
		}
		negate := loc[2] >= 0
		source := text[loc[4]:loc[5]]
		if len(source) >= 2 && strings.HasPrefix(source, `"`) && strings.HasSuffix(source, `"`) {
			source = source[1 : len(source)-1]
		}
		if negate {
			f.excludeSources = append(f.excludeSources, strings.ToLower(source))
		} else {
			f.includeSources = append(f.includeSources, strings.ToLower(source))
		}
		text = strings.TrimSpace(text[:loc[0]] + text[loc[1]:])
	}
	f.negate = strings.HasPrefix(text, "!")
	f.text = strings.ToLower(strings.TrimSpace(strings.TrimLeft(text, "!")))
	if text == "" {
		return f
	}
	for _, part := range glob.SplitGlobAware(text, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "!") {
			if p := strings.TrimLeft(part, "!"); p != "" {
				f.excludes = append(f.excludes, patternMatchers(p)...)
			}
			continue
		}
		f.includes = append(f.includes, patternMatchers(part)...)
	}
	return f
}

// patternMatchers turn a word of the filter into the globs that match it as a file or a folder
// anywhere: ".go" is "*.go", and `p` is `**/p/**` and `**/p`.
func patternMatchers(p string) []*glob.Matcher {
	if strings.HasPrefix(p, ".") {
		p = "*" + p
	}
	return []*glob.Matcher{glob.Parse("**/"+p+"/**", glob.Options{}), glob.Parse("**/"+p, glob.Options{})}
}

func anyMatches(ms []*glob.Matcher, path string) bool {
	for _, m := range ms {
		if m.Match(path) {
			return true
		}
	}
	return false
}

// resourceVerdict is what the filter says about a file before its markers are looked at.
type resourceVerdict int

const (
	// verdictHide drops the file.
	verdictHide resourceVerdict = iota
	// verdictShowAll keeps the file and, whatever the text, its markers (the severity still counts).
	verdictShowAll
	// verdictInspect keeps the file if some marker passes.
	verdictInspect
)

// resource judges a file.
func (f *filter) resource(path string) resourceVerdict {
	if f.opts.ActiveFileOnly && path != f.opts.ActiveFile {
		return verdictHide
	}
	if f.opts.Exclude != nil && f.opts.Exclude(path) {
		return verdictHide
	}
	if anyMatches(f.excludes, path) {
		return verdictHide
	}
	if anyMatches(f.includes, path) {
		return verdictShowAll
	}
	if f.text != "" && !f.negate && strings.Contains(strings.ToLower(filepath.Base(path)), f.text) {
		return verdictShowAll
	}
	return verdictInspect
}

// marker tells whether a marker of a file that was judged verdict is shown.
func (f *filter) marker(m Marker, verdict resourceVerdict) bool {
	if !f.opts.Severities.Has(m.Severity) {
		return false
	}
	if !f.matchesSource(m.Source) {
		return false
	}
	if f.text == "" {
		return true
	}
	matched := strings.Contains(strings.ToLower(m.Message), f.text) ||
		(m.Source != "" && strings.Contains(strings.ToLower(m.Source), f.text)) ||
		(m.Code != "" && strings.Contains(strings.ToLower(m.Code), f.text))
	switch {
	case matched && !f.negate:
		return true
	case f.negate:
		// the verdict of the file decides when a glob took it in; otherwise a negated text
		// keeps what does not match
		if verdict == verdictShowAll {
			return true
		}
		return !matched
	}
	return verdict == verdictShowAll
}

// matchesSource applies the `@source:` filters to the source of a marker.
func (f *filter) matchesSource(source string) bool {
	if len(f.includeSources) == 0 && len(f.excludeSources) == 0 {
		return true
	}
	source = strings.ToLower(source)
	for _, s := range f.excludeSources {
		if source != "" && s == source {
			return false
		}
	}
	if len(f.includeSources) > 0 {
		for _, s := range f.includeSources {
			if source != "" && s == source {
				return true
			}
		}
		return false
	}
	return true
}
