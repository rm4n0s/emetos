package settings

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/settingsTreeModels.ts (parseQuery,
// SettingsTreeSettingElement.matchesAnyId, matchesAllLanguages, the @modified tag) and
// src/vs/workbench/contrib/preferences/browser/preferencesSearch.ts (cleanFilter, SettingMatches: the key as words,
// the description and the values of an enum). The ranking is simpler than VS Code's: see Search.

// The search filters VS Code's settings editor knows. Emetos has @modified, @id: and @lang:; the others are read
// and reported as not supported.
const (
	// ModifiedFilter is the filter of the settings the file of the tab sets.
	ModifiedFilter = "@modified"
	// IDFilter is the prefix of the filter of exact setting ids.
	IDFilter = "@id:"
	// LanguageFilter is the prefix of the filter of the settings of a language.
	LanguageFilter = "@lang:"
)

// Query is a search text taken apart (VS Code's parseQuery): the filters and the free words.
type Query struct {
	// Text is what is left of the search once the filters are taken out, cleaned of quotes and colons.
	Text string
	// Words are the words of Text, in lower case.
	Words []string
	// Modified is true for @modified: only the settings the file of the tab sets.
	Modified bool
	// IDs are the ids of @id:, a comma separated list; an id ending in "*" is a prefix.
	IDs []string
	// Language is the value of the first @lang:, as it was written ("go", "Go"), or empty.
	Language string
	// Unsupported are the filters that VS Code has and Emetos has not (@tag:, @feature:, @ext:, @hasPolicy,
	// @stable), as they were written; they are ignored.
	Unsupported []string
}

// Empty tells that the query has no filter and no word: the editor then shows all the settings.
func (q Query) Empty() bool {
	return !q.Modified && len(q.IDs) == 0 && q.Language == "" && len(q.Words) == 0
}

var (
	tagRegex       = regexp.MustCompile(`(^|\s)@tag:("([^"]*)"|[^"]\S*)`)
	extensionRegex = regexp.MustCompile(`(^|\s)@ext:("([^"]*)"|[^"]\S*)?`)
	featureRegex   = regexp.MustCompile(`(^|\s)@feature:("([^"]*)"|[^"]\S*)?`)
	idRegex        = regexp.MustCompile(`(^|\s)@id:("([^"]*)"|[^"]\S*)?`)
	languageRegex  = regexp.MustCompile(`(^|\s)@lang:("([^"]*)"|[^"]\S*)?`)
	modifiedRegex  = regexp.MustCompile(`(^|\s)@modified(\s|$)`)
	bareTagRegex   = regexp.MustCompile(`(^|\s)(@hasPolicy|@stable)(\s|$)`)
)

// ParseQuery takes the filters out of the text of the search box: @modified, @id:a,b, @lang:go, and the filters
// that are not supported. The value of a filter is a word, or text between quotes.
func ParseQuery(text string) Query {
	text0 := text
	var q Query
	// element returns the value of a match of the filters with a value: quoted or not.
	value := func(m []string) string {
		if strings.HasPrefix(m[2], `"`) {
			return m[3]
		}
		return m[2]
	}
	split := func(v string) (out []string) {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
		return out
	}
	// strip takes a filter out of the text each time it is found; filters that follow each other share the space
	// between them, so a pass can miss the second one: it goes on until a pass finds nothing
	strip := func(re *regexp.Regexp, on func(match string, groups []string)) {
		for re.MatchString(text) {
			text = re.ReplaceAllStringFunc(text, func(match string) string {
				on(strings.TrimSpace(match), re.FindStringSubmatch(match))
				return " "
			})
		}
	}
	strip(tagRegex, func(match string, _ []string) { q.Unsupported = append(q.Unsupported, match) })
	strip(modifiedRegex, func(string, []string) { q.Modified = true })
	strip(bareTagRegex, func(match string, _ []string) { q.Unsupported = append(q.Unsupported, match) })
	strip(extensionRegex, func(match string, _ []string) { q.Unsupported = append(q.Unsupported, match) })
	strip(featureRegex, func(match string, _ []string) { q.Unsupported = append(q.Unsupported, match) })
	strip(idRegex, func(_ string, g []string) { q.IDs = append(q.IDs, split(value(g))...) })
	strip(languageRegex, func(_ string, g []string) {
		// only the first language counts, as in VS Code
		if langs := split(value(g)); len(langs) > 0 && q.Language == "" {
			q.Language = langs[0]
		}
	})
	original := text0
	sort.SliceStable(q.Unsupported, func(i, j int) bool {
		return strings.Index(original, q.Unsupported[i]) < strings.Index(original, q.Unsupported[j])
	})
	// preferencesSearch.cleanFilter: quotes and colons are likely to be pasted along with a setting name
	text = strings.NewReplacer(`"`, " ", ":", " ").Replace(text)
	if words := strings.Fields(strings.ToLower(text)); len(words) > 0 {
		q.Words = words
	}
	q.Text = strings.Join(strings.Fields(text), " ")
	return q
}

// LanguageResolver finds a language by what the user wrote after @lang: (its id or its name, whatever the case):
// the id, the name to show, and whether there is such a language.
type LanguageResolver func(name string) (id, display string, ok bool)

// SearchResult is what the settings editor shows for a search text.
type SearchResult struct {
	// Query is the parsed text.
	Query Query
	// Active is true when the text has a filter or a word: the editor then shows a list of results (and not the
	// Commonly Used group, which only repeats settings).
	Active bool
	// Categories are the groups with the settings found, in the order of the layout; inside a group the
	// settings are in the order of their relevance (layout order among equals).
	Categories []*Category
	// Count is the number of settings found.
	Count int
	// Language and LanguageName are the id and the name of the language of @lang:, empty without that filter or
	// with a language that does not exist.
	Language, LanguageName string
	// Hints say what part of the text was ignored or found nothing, such as an unsupported filter.
	Hints []string
}

// Summary is the line under the search box: "No Settings Found", "1 Setting Found" or "N Settings Found".
func (r SearchResult) Summary() string {
	switch r.Count {
	case 0:
		return "No Settings Found"
	case 1:
		return "1 Setting Found"
	}
	return strconv.Itoa(r.Count) + " Settings Found"
}

// Match scores of a word: where in a setting it was found.
const (
	scoreDescription      = 1
	scoreDescriptionWhole = 2
	scoreValue            = 2
	scoreKey              = 3
	scoreKeyWhole         = 4
	scoreExactID          = 1000
)

// Search finds the settings of a target for the text of the search box (SPEC §11.1).
//
// The filters narrow the settings: @modified keeps those that the file of the target sets (inside the block of
// the language with @lang:), @id: those with an id in its list, and @lang: the settings a language can set
// (ScopeLanguageOverridable), shown with the value of the block of that language; a language that does not exist
// finds nothing. The words must all be found in a setting: in its key (written as words, so "font size" finds
// editor.fontSize, and whole, without dots or case), in its title, in the values of an enum or in its description.
// A setting is scored by its words (a key or title match 3, a whole word of the key 4, a value 2, a whole word of
// the description 2, a part of it 1) and the key typed whole scores 1000. The groups keep the order of the layout and the
// settings inside a group the order of their scores (the layout order among equal ones); VS Code lists the
// results flat in the order of its match types, which a list with headers cannot show.
func (m *UIModel) Search(target Target, text string, resolve LanguageResolver) SearchResult {
	q := ParseQuery(text)
	res := SearchResult{Query: q, Active: !q.Empty() || len(q.Unsupported) > 0}
	for _, f := range q.Unsupported {
		res.Hints = append(res.Hints, f+" is not supported and was ignored.")
	}
	if q.Language != "" {
		id, name, ok := q.Language, q.Language, true
		if resolve != nil {
			id, name, ok = resolve(q.Language)
		}
		if !ok {
			res.Hints = append(res.Hints, "There is no language \""+q.Language+"\".")
			return res
		}
		res.Language, res.LanguageName = id, name
	}
	scores := map[string]int{}
	exact := strings.ToLower(strings.Join(q.Words, " "))
	keep := func(s Setting, groupID string) bool {
		if res.Language != "" && s.Scope != ScopeLanguageOverridable {
			return false
		}
		if len(q.IDs) > 0 && !matchesAnyID(q.IDs, s.Key) {
			return false
		}
		if q.Modified {
			row, ok := m.rowIn(s, target, res.Language, groupID)
			if !ok || !row.Modified {
				return false
			}
		}
		if len(q.Words) == 0 {
			return true
		}
		score, ok := scoreSetting(s, q.Words)
		if !ok {
			return false
		}
		if exact == strings.ToLower(s.Key) {
			score += scoreExactID
		}
		scores[s.Key] = score
		return true
	}
	cats := m.filterTree(target, keep)
	if res.Active {
		out := cats[:0:0]
		for _, c := range cats {
			if c.ID != CommonlyUsedID {
				out = append(out, c)
			}
		}
		cats = out
	}
	for _, c := range cats {
		c.Walk(func(c *Category) {
			sort.SliceStable(c.Keys, func(i, j int) bool { return scores[c.Keys[i]] > scores[c.Keys[j]] })
		})
		if res.Active {
			res.Count += c.Count()
		}
	}
	res.Categories = cats
	return res
}

// matchesAnyID tells whether a key is one of the ids of @id:, or starts with one that ends in "*".
func matchesAnyID(ids []string, key string) bool {
	for _, id := range ids {
		if id == key {
			return true
		}
		if prefix, ok := strings.CutSuffix(id, "*"); ok && strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// scoreSetting adds the scores of the words of a search for a setting; ok is false when a word is nowhere.
func scoreSetting(s Setting, words []string) (score int, ok bool) {
	category, label := KeyToDisplayFormat(s.Key, "")
	title := strings.ToLower(Title(category, label))
	keyWords := keyToWords(s.Key)
	keyTokens := strings.Fields(keyWords)
	keyAlnum := alphaNumeric(s.Key)
	description := strings.ToLower(s.Description)
	descTokens := strings.FieldsFunc(description, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var values []string
	for _, v := range s.Enum {
		if str, isString := v.(string); isString {
			values = append(values, strings.ToLower(str))
		}
	}
	for _, w := range words {
		best := 0
		set := func(n int) { best = max(best, n) }
		switch {
		case containsToken(keyTokens, w):
			set(scoreKeyWhole)
		case strings.Contains(keyWords, w) || strings.Contains(strings.ToLower(s.Key), w) || strings.Contains(title, w):
			set(scoreKey)
		}
		if a := alphaNumeric(w); a != "" && strings.Contains(keyAlnum, a) {
			set(scoreKey)
		}
		for _, v := range values {
			if strings.Contains(v, w) {
				set(scoreValue)
			}
		}
		switch {
		case containsToken(descTokens, w):
			set(scoreDescriptionWhole)
		case strings.Contains(description, w):
			set(scoreDescription)
		}
		if best == 0 {
			return 0, false
		}
		score += best
	}
	return score, true
}

func containsToken(tokens []string, w string) bool {
	for _, t := range tokens {
		if t == w {
			return true
		}
	}
	return false
}

var (
	keySeparators = regexp.MustCompile(`[-._]`)
	keyCamel      = regexp.MustCompile(`([a-z]+)([A-Z])`)
	keyLettersNum = regexp.MustCompile(`([A-Za-z]+)(\d+)`)
	keyNumLetters = regexp.MustCompile(`(\d+)([A-Za-z]+)`)
)

// keyToWords is the key of a setting as lower case words ("editor.fontSize" is "editor font size"): VS Code's
// SettingMatches._keyToLabel.
func keyToWords(key string) string {
	key = keySeparators.ReplaceAllString(key, " ")
	key = keyCamel.ReplaceAllString(key, "$1 $2")
	key = keyLettersNum.ReplaceAllString(key, "$1 $2")
	key = keyNumLetters.ReplaceAllString(key, "$1 $2")
	return strings.ToLower(key)
}

// alphaNumeric keeps the letters and digits of a text, in lower case ("editor.fontSize" is "editorfontsize").
func alphaNumeric(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
