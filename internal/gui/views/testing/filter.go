package testing

import (
	"regexp"
	"strings"
)

// A filter term is one of the words the text of the filter box understands.
const (
	// TermFailed shows the tests that failed or errored, TermExecuted the tests that have a result.
	TermFailed   = "@failed"
	TermExecuted = "@executed"
	// TermDoc shows the tests of the active editor's file, TermOpenedFiles those of all the open files.
	TermDoc         = "@doc"
	TermOpenedFiles = "@openedFiles"
	// TermHidden shows the tests the user hid as well.
	TermHidden = "@hidden"
)

// Terms are the terms in the order of the filter's menu.
var Terms = []string{TermFailed, TermExecuted, TermDoc, TermOpenedFiles, TermHidden}

// TermDescriptions are the labels of the terms in the menu of the filter.
var TermDescriptions = map[string]string{
	TermFailed:      "Show Only Failed Tests",
	TermExecuted:    "Show Only Executed Tests",
	TermDoc:         "Show in Active File Only",
	TermOpenedFiles: "Show in Opened Files Only",
	TermHidden:      "Show Hidden Tests",
}

// Glob is one comma-separated word of the text: a test whose label contains it (in lower case) is included, or excluded when the word
// started with "!".
type Glob struct {
	Include bool
	Text    string
}

// Filter is the parsed text of the filter box (VS Code's TestExplorerFilterState.setText): the terms, the words, the tags and the
// controller. The zero value filters nothing.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/common/testExplorerFilterState.ts
type Filter struct {
	// Text is the text that was parsed.
	Text string
	// Terms are the special terms the text holds.
	Terms map[string]bool
	// Globs are the words left when the terms and tags are taken out.
	Globs []Glob
	// IncludeTags and ExcludeTags are the tags named with @controller:tag and !@controller:tag, as "controller\x00tag".
	IncludeTags, ExcludeTags map[string]bool
	// Controllers are the ids named with @ctrl:id (decision 1547): only the tests of these controllers show.
	Controllers map[string]bool
	// Fuzzy makes a word match when its letters are in the label in order, not only when they are next to each other.
	Fuzzy bool
}

var tagRe = regexp.MustCompile(`!?@([^ ,:]+)`)

// NamespaceTag is how a tag is told apart from the tags of other controllers: the controller's id, a NUL byte and the tag.
func NamespaceTag(controllerID, tag string) string { return controllerID + "\x00" + tag }

// ParseFilter reads the text of the filter box.
func ParseFilter(text string, fuzzy bool) *Filter {
	f := &Filter{Text: text, Fuzzy: fuzzy, Terms: map[string]bool{}, IncludeTags: map[string]bool{}, ExcludeTags: map[string]bool{},
		Controllers: map[string]bool{}}
	var glob strings.Builder
	last := 0
	for _, m := range tagRe.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[0], m[1]
		word := text[start:end]
		next := end
		isTerm := false
		for _, t := range Terms {
			if t == word {
				f.Terms[t] = true
				isTerm = true
			}
		}
		isTag := false
		if next < len(text) && text[next] == ':' {
			isTag = true
			next++
			delim := byte(' ')
			if next < len(text) && (text[next] == '"' || text[next] == '\'') {
				delim = text[next]
				next++
			}
			var tag strings.Builder
			for next < len(text) && text[next] != delim {
				if text[next] == '\\' && next+1 < len(text) {
					tag.WriteByte(text[next+1])
					next += 2
					continue
				}
				tag.WriteByte(text[next])
				next++
			}
			ctrl := text[m[2]:m[3]]
			switch {
			case ctrl == "ctrl" && !strings.HasPrefix(word, "!"):
				f.Controllers[tag.String()] = true
			case strings.HasPrefix(word, "!"):
				f.ExcludeTags[NamespaceTag(ctrl, tag.String())] = true
			default:
				f.IncludeTags[NamespaceTag(ctrl, tag.String())] = true
			}
			next++
		}
		// an @word that is no term and no tag is text (a test named "@smoke")
		if !isTerm && !isTag {
			continue
		}
		if start < last {
			continue
		}
		glob.WriteString(text[last:start])
		last = min(next, len(text))
	}
	if last < len(text) {
		glob.WriteString(text[last:])
	}
	for _, w := range splitGlobAware(strings.TrimSpace(glob.String()), ',') {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(w, "!"); ok {
			f.Globs = append(f.Globs, Glob{Include: false, Text: strings.ToLower(rest)})
		} else {
			f.Globs = append(f.Globs, Glob{Include: true, Text: strings.ToLower(w)})
		}
	}
	return f
}

// splitGlobAware splits at sep except inside braces and brackets.
func splitGlobAware(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{', '[':
			depth++
		case '}', ']':
			if depth > 0 {
				depth--
			}
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// Has reports whether the text holds the term.
func (f *Filter) Has(term string) bool { return f.Terms[term] }

// Empty reports whether the text filters nothing.
func (f *Filter) Empty() bool {
	return len(f.Terms) == 0 && len(f.Globs) == 0 && len(f.IncludeTags) == 0 && len(f.ExcludeTags) == 0 && len(f.Controllers) == 0
}

// ToggleTerm gives the text with the term added (on) or taken out (off).
//
// Ported from VS Code: src/vs/workbench/contrib/testing/common/testExplorerFilterState.ts (toggleFilteringFor)
func ToggleTerm(text, term string, on bool) string {
	text = strings.TrimSpace(text)
	has := ParseFilter(text, false).Has(term)
	switch {
	case on && !has:
		if text == "" {
			return term
		}
		return text + " " + term
	case !on && has:
		return strings.Join(strings.Fields(strings.Replace(text, term, "", 1)), " ")
	}
	return text
}

// fuzzyContains reports whether the letters of the query are in the target in order.
//
// Ported from VS Code: src/vs/base/common/strings.ts (fuzzyContains)
func fuzzyContains(target, query string) bool {
	if target == "" || query == "" || len(target) < len(query) {
		return false
	}
	t, q := []rune(strings.ToLower(target)), strings.ToLower(query)
	last := 0
	for _, r := range q {
		found := false
		for ; last < len(t); last++ {
			if t[last] == r {
				found = true
				last++
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// result of a part of the filter for an element: Exclude beats Inherit beats Include (VS Code's FilterResult, which it combines with
// Math.min).
type filterResult int

const (
	exclude filterResult = iota
	inherit
	include
)
