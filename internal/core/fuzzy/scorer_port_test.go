package fuzzy

import (
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Tests ported from VS Code: src/vs/base/test/common/fuzzyScorer.test.ts
//
// The file has 68 tests. All 68 are ported (each Go test names its JS test in a "test:" comment),
// one of them adapted:
//   - "scoreItem - cache key matches the hash of the scoring inputs" asserts the exact
//     numbers of VS Code's generic object hash (hash.ts) as keys of the cache. The Go cache is
//     keyed by a struct and a 64-bit hash of the query, so the numbers mean nothing. The test
//     checks what it is for instead (TestScoreItemCacheKeys): that every input of a scoring
//     (query, description, match mode) has its own cache entry and the empty query has none.
//
// Other changes, none of which changes what is asserted:
//   - URI.file(p) is a string path with a leading slash; fsPath is the path itself on Linux.
//   - The isWindows branches (and the Windows-only paths of "prepareQuery") are not run:
//     Emetos is for Linux.
//   - Positions are offsets in runes where VS Code's are UTF-16 offsets. The only non-ASCII
//     characters of the file are in BMP strings or in the "❌" test, which gives the same numbers.
//   - A null item of "scoreItem - matches are proper" and "invalid input" is a nil *uri, which
//     the accessor of the test turns into empty strings.
//   - "scoreItem - mutating a prepared query is reflected in the cache key" changes
//     the ExpectContiguousMatch of a piece, which the Go key hashes too.

// uri is the resource of the VS Code tests: fsPath is what the default accessor uses and path
// what the slash accessors use.
type uri struct{ fsPath, path string }

// file is URI.file: the path gets a leading slash.
func file(p string) uri {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return uri{fsPath: p, path: p}
}

// remote is a vscode-remote URI, whose path does not get a leading slash.
func remote(p string) uri { return uri{fsPath: p, path: p} }

type resourceAccessor struct{}

func (resourceAccessor) ItemLabel(r uri) string       { return path.Base(r.fsPath) }
func (resourceAccessor) ItemDescription(r uri) string { return path.Dir(r.fsPath) }
func (resourceAccessor) ItemPath(r uri) string        { return r.fsPath }

type resourceWithSlashAccessor struct{}

func (resourceWithSlashAccessor) ItemLabel(r uri) string { return path.Base(r.fsPath) }
func (resourceWithSlashAccessor) ItemDescription(r uri) string {
	return path.Clean(path.Dir(r.path))
}
func (resourceWithSlashAccessor) ItemPath(r uri) string { return path.Clean(r.path) }

// win32Normalize is win32.normalize of a posix path that is clean already.
func win32Normalize(p string) string { return strings.ReplaceAll(path.Clean(p), "/", `\`) }

type resourceWithBackslashAccessor struct{}

func (resourceWithBackslashAccessor) ItemLabel(r uri) string { return path.Base(r.fsPath) }
func (resourceWithBackslashAccessor) ItemDescription(r uri) string {
	return win32Normalize(path.Dir(r.path))
}
func (resourceWithBackslashAccessor) ItemPath(r uri) string { return win32Normalize(r.path) }

// nullAccessor gives nothing, as VS Code's NullAccessorClass.
type nullAccessor struct{}

func (nullAccessor) ItemLabel(uri) string       { return "" }
func (nullAccessor) ItemDescription(uri) string { return "" }
func (nullAccessor) ItemPath(uri) string        { return "" }

// nilSafeAccessor is the resource accessor over pointers; a nil one has no strings.
type nilSafeAccessor struct{}

func (nilSafeAccessor) ItemLabel(r *uri) string {
	if r == nil {
		return ""
	}
	return resourceAccessor{}.ItemLabel(*r)
}
func (nilSafeAccessor) ItemDescription(r *uri) string {
	if r == nil {
		return ""
	}
	return resourceAccessor{}.ItemDescription(*r)
}
func (nilSafeAccessor) ItemPath(r *uri) string {
	if r == nil {
		return ""
	}
	return resourceAccessor{}.ItemPath(*r)
}

// descriptionAccessor is resourceAccessorWithDescription.
type descriptionAccessor struct{ description string }

func (descriptionAccessor) ItemLabel(r uri) string       { return resourceAccessor{}.ItemLabel(r) }
func (a descriptionAccessor) ItemDescription(uri) string { return a.description }
func (descriptionAccessor) ItemPath(r uri) string        { return resourceAccessor{}.ItemPath(r) }

// doScore is _doScore of the test.
func doScore(target, query string, allowNonContiguousMatches *bool) Score {
	prepared := PrepareQuery(query)
	allow := !prepared.ExpectContiguousMatch
	if allowNonContiguousMatches != nil {
		allow = *allowNonContiguousMatches
	}
	return ScoreFuzzy(target, prepared.Normalized, prepared.NormalizedLowercase, allow)
}

func boolPtr(b bool) *bool { return &b }

// doScore2 is _doScore2 of the test.
func doScore2(target, query string, matchOffset int) Score2 {
	return ScoreFuzzy2(target, PrepareQuery(query), 0, matchOffset)
}

func scoreItem[T any](item T, query string, allow bool, accessor ItemAccessor[T], cache *Cache) ItemScore {
	if cache == nil {
		cache = NewCache()
	}
	return ScoreItemFuzzy(nil, item, PrepareQuery(query), allow, accessor, cache)
}

// sortByScore is items.sort((a, b) => compareItemsByScore(a, b, query, true, accessor)).
func sortByScore(items []uri, query string) []uri {
	res := slices.Clone(items)
	prepared := PrepareQuery(query)
	cache := NewCache()
	s := NewScorer()
	slices.SortStableFunc(res, func(a, b uri) int {
		return CompareItemsByFuzzyScore[uri](s, a, b, prepared, true, resourceAccessor{}, cache)
	})
	return res
}

func TestScoreFuzzy(t *testing.T) {
	// test: score (fuzzy)
	target := "HelLo-World"
	queries := []string{
		"HelLo-World", // direct case match
		"hello-world", // direct mix-case match
		"HW",          // direct case prefix (multiple)
		"hw",          // direct mix-case prefix (multiple)
		"H",           // direct case prefix
		"h",           // direct mix-case prefix
		"W",           // direct case word prefix
		"Ld",          // in-string case match (multiple)
		"ld",          // in-string mix-case match (consecutive, avoids scattered hit)
		"w",           // direct mix-case word prefix
		"L",           // in-string case match
		"l",           // in-string mix-case match
		"4",           // no match
	}
	var scores []int
	for _, q := range queries {
		scores = append(scores, doScore(target, q, boolPtr(true)).Score)
	}
	// Assert scoring order
	if !slices.IsSortedFunc(scores, func(a, b int) int { return b - a }) {
		t.Errorf("scores %v are not in descending order", scores)
	}
	if scores[len(scores)-1] != 0 {
		t.Errorf("no match scored %d", scores[len(scores)-1])
	}
}

func TestScoreNonFuzzy(t *testing.T) {
	// test: score (non fuzzy)
	target := "HelLo-World"
	f := boolPtr(false)

	if doScore(target, "HelLo-World", f).Score <= 0 {
		t.Error("full match has no score")
	}
	if got := len(doScore(target, "HelLo-World", f).Positions); got != len("HelLo-World") {
		t.Errorf("positions of full match = %d", got)
	}
	if doScore(target, "hello-world", f).Score <= 0 {
		t.Error("lower case match has no score")
	}
	if got := doScore(target, "HW", f).Score; got != 0 {
		t.Errorf("HW = %d", got)
	}
	for _, q := range []string{"h", "ello", "ld"} {
		if doScore(target, q, f).Score <= 0 {
			t.Errorf("%q has no score", q)
		}
	}
	if got := doScore(target, "eo", f).Score; got != 0 {
		t.Errorf("eo = %d", got)
	}
}

func TestScoreItemMatchesAreProper(t *testing.T) {
	// test: scoreItem - matches are proper
	var acc ItemAccessor[uri] = resourceAccessor{}

	if res := scoreItem[*uri](nil, "something", true, nilSafeAccessor{}, nil); res.Score != 0 {
		t.Errorf("nil item scored %d", res.Score)
	}

	resource := file("/xyz/some/path/someFile123.txt")
	if res := scoreItem(resource, "something", true, nullAccessor{}, nil); res.Score != 0 {
		t.Errorf("null accessor scored %d", res.Score)
	}

	// Path Identity
	identityRes := scoreItem(resource, acc.ItemPath(resource), true, acc, nil)
	if identityRes.Score == 0 {
		t.Fatal("identity has no score")
	}
	if !reflect.DeepEqual(identityRes.DescriptionMatch, []Match{{0, len(acc.ItemDescription(resource))}}) {
		t.Errorf("identity description match = %v", identityRes.DescriptionMatch)
	}
	if !reflect.DeepEqual(identityRes.LabelMatch, []Match{{0, len(acc.ItemLabel(resource))}}) {
		t.Errorf("identity label match = %v", identityRes.LabelMatch)
	}

	// Basename Prefix
	basenamePrefixRes := scoreItem(resource, "som", true, acc, nil)
	if basenamePrefixRes.Score == 0 || basenamePrefixRes.DescriptionMatch != nil ||
		!reflect.DeepEqual(basenamePrefixRes.LabelMatch, []Match{{0, 3}}) {
		t.Errorf("basename prefix = %+v", basenamePrefixRes)
	}

	// Basename Camelcase
	basenameCamelcaseRes := scoreItem(resource, "sF", true, acc, nil)
	if basenameCamelcaseRes.Score == 0 || basenameCamelcaseRes.DescriptionMatch != nil ||
		!reflect.DeepEqual(basenameCamelcaseRes.LabelMatch, []Match{{0, 1}, {4, 5}}) {
		t.Errorf("basename camelcase = %+v", basenameCamelcaseRes)
	}

	// Basename Match
	basenameRes := scoreItem(resource, "of", true, acc, nil)
	if basenameRes.Score == 0 || basenameRes.DescriptionMatch != nil ||
		!reflect.DeepEqual(basenameRes.LabelMatch, []Match{{1, 2}, {4, 5}}) {
		t.Errorf("basename match = %+v", basenameRes)
	}

	// Path Match
	pathRes := scoreItem(resource, "xyz123", true, acc, nil)
	if pathRes.Score == 0 ||
		!reflect.DeepEqual(pathRes.LabelMatch, []Match{{8, 11}}) ||
		!reflect.DeepEqual(pathRes.DescriptionMatch, []Match{{1, 4}}) {
		t.Errorf("path match = %+v", pathRes)
	}

	// Ellipsis Match
	if ellipsisRes := scoreItem(resource, "…me/path/someFile123.txt", true, acc, nil); ellipsisRes.Score == 0 {
		t.Error("ellipsis match has no score")
	}

	// No Match
	noRes := scoreItem(resource, "987", true, acc, nil)
	if noRes.Score != 0 || noRes.LabelMatch != nil || noRes.DescriptionMatch != nil {
		t.Errorf("no match = %+v", noRes)
	}

	// No Exact Match
	noExactRes := scoreItem(resource, `"sF"`, true, acc, nil)
	if noExactRes.Score != 0 || noExactRes.LabelMatch != nil || noExactRes.DescriptionMatch != nil {
		t.Errorf("no exact match = %+v", noExactRes)
	}
	if noRes.Score != noExactRes.Score {
		t.Error("no match scores differ")
	}

	// Verify Scores
	if identityRes.Score <= basenamePrefixRes.Score ||
		basenamePrefixRes.Score <= basenameRes.Score ||
		basenameRes.Score <= pathRes.Score ||
		pathRes.Score <= noRes.Score {
		t.Errorf("scores out of order: %d %d %d %d %d", identityRes.Score, basenamePrefixRes.Score, basenameRes.Score, pathRes.Score, noRes.Score)
	}
}

func TestScoreItemMultiple(t *testing.T) {
	// test: scoreItem - multiple
	resource := file("/xyz/some/path/someFile123.txt")
	acc := resourceAccessor{}

	res1 := scoreItem[uri](resource, "xyz some", true, acc, nil)
	res2 := scoreItem[uri](resource, "some xyz", true, acc, nil)
	res3 := scoreItem[uri](resource, "some xyz file file123", true, acc, nil)
	res4 := scoreItem[uri](resource, "path z y", true, acc, nil)

	tests := []struct {
		name  string
		res   ItemScore
		label []Match
		desc  []Match
	}{
		{"xyz some", res1, []Match{{0, 4}}, []Match{{1, 4}}},
		{"some xyz", res2, []Match{{0, 4}}, []Match{{1, 4}}},
		{"some xyz file file123", res3, []Match{{0, 11}}, []Match{{1, 4}}},
		{"path z y", res4, []Match{}, []Match{{2, 4}, {10, 14}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.res.Score == 0 {
				t.Fatal("no score")
			}
			if !slices.Equal(tt.res.LabelMatch, tt.label) {
				t.Errorf("label match = %v, want %v", tt.res.LabelMatch, tt.label)
			}
			if !slices.Equal(tt.res.DescriptionMatch, tt.desc) {
				t.Errorf("description match = %v, want %v", tt.res.DescriptionMatch, tt.desc)
			}
		})
	}
	if res1.Score != res2.Score {
		t.Errorf("order of the pieces changes the score: %d, %d", res1.Score, res2.Score)
	}
	if res3.Score <= res2.Score {
		t.Errorf("more pieces should score more: %d, %d", res3.Score, res2.Score)
	}
	if res4.Score >= res2.Score {
		t.Errorf("description only should score less: %d, %d", res4.Score, res2.Score)
	}
}

func TestScoreItemMultipleWithCacheYieldsDifferentResults(t *testing.T) {
	// test: scoreItem - multiple with cache yields different results
	resource := file("/xyz/some/path/someFile123.txt")
	cache := NewCache()
	res1 := scoreItem[uri](resource, "xyz sm", true, resourceAccessor{}, cache)
	if res1.Score == 0 {
		t.Fatal("xyz sm has no score")
	}

	// from the cache's perspective this should be a totally different query
	res2 := scoreItem[uri](resource, `xyz "sm"`, true, resourceAccessor{}, cache)
	if res2.Score != 0 {
		t.Errorf(`xyz "sm" scored %d`, res2.Score)
	}
}

// TestScoreItemCacheKeys ports "scoreItem - cache key matches the hash of the scoring inputs"
// (adapted: see the top of the file), "scoreItem - cache keys separate items, queries and match
// modes", "scoreItem - repeated scoring hits the cache and preserves the result" and
// "scoreItem - mutating a prepared query is reflected in the cache key".
func TestScoreItemCacheKeys(t *testing.T) {
	resource := file("/xyz/some/path/someFile123.txt")
	acc := resourceAccessor{}

	// test: scoreItem - cache key matches the hash of the scoring inputs (adapted)
	t.Run("one entry per scoring, none for the empty query", func(t *testing.T) {
		queries := []string{"some", "xyz sm", `xyz "sm"`, "", "ünïcödé", "null\x00char", "tab\there", "0", "01", "4294967295", "__proto__", "constructor", "\U0001F680", "\xed\xa0\x80"}
		descriptions := []string{"", acc.ItemDescription(resource), "\x00:[]", "\U0001F680\xed\xa0\x80"}
		for _, q := range queries {
			for _, d := range descriptions {
				for _, allow := range []bool{true, false} {
					cache := NewCache()
					scoreItem[uri](resource, q, allow, descriptionAccessor{d}, cache)
					want := 1
					if q == "" {
						want = 0
					}
					if cache.Len() != want {
						t.Errorf("query %q description %q allow %v: %d entries, want %d", q, d, allow, cache.Len(), want)
					}
				}
			}
		}
	})

	// test: scoreItem - cache keys separate items, queries and match modes
	t.Run("keys separate items, queries and match modes", func(t *testing.T) {
		cache := NewCache()

		// same label, different description
		scoreItem[uri](file("/xyz/some/path/someFile123.txt"), "some", true, acc, cache)
		scoreItem[uri](file("/xyz/other/path/someFile123.txt"), "some", true, acc, cache)

		// same item, different queries (incl. multi piece and quoted/contiguous)
		scoreItem[uri](resource, "somefile", true, acc, cache)
		scoreItem[uri](resource, "xyz sm", true, acc, cache)
		scoreItem[uri](resource, `xyz "sm"`, true, acc, cache)
		scoreItem[uri](resource, "sm xyz", true, acc, cache)

		// same item and query, different match mode
		scoreItem[uri](resource, "somefile", false, acc, cache)

		// an item without description
		scoreItem[uri](file("/someFile123.txt"), "some", true, descriptionAccessor{""}, cache)
		scoreItem[uri](file("/someFile123.txt"), "some", true, descriptionAccessor{"/"}, cache)

		if cache.Len() != 9 {
			t.Errorf("each distinct scoring input needs its own cache entry: %d entries, want 9", cache.Len())
		}
	})

	// test: scoreItem - repeated scoring hits the cache and preserves the result
	t.Run("repeated scoring hits the cache", func(t *testing.T) {
		for _, q := range []string{"somefile", "xyz sm", `xyz "sm"`, "ünïcödé"} {
			cache := NewCache()
			cold := scoreItem[uri](resource, q, true, acc, cache)
			warm := scoreItem[uri](resource, q, true, acc, cache)
			if cache.Len() != 1 {
				t.Errorf("%q: %d entries", q, cache.Len())
			}
			if !reflect.DeepEqual(cold, warm) {
				t.Errorf("%q: warm %+v != cold %+v", q, warm, cold)
			}
			if len(cold.LabelMatch) > 0 && &cold.LabelMatch[0] != &warm.LabelMatch[0] {
				t.Errorf("%q: warm scoring should return the cached result", q)
			}
		}
	})

	// test: scoreItem - mutating a prepared query is reflected in the cache key
	t.Run("mutating a prepared query changes the key", func(t *testing.T) {
		cache := NewCache()
		query := PrepareQuery("xyz sm")
		before := ScoreItemFuzzy[uri](nil, resource, query, true, acc, cache)
		if before.Score == 0 {
			t.Fatal("xyz sm has no score")
		}

		// callers own the prepared query object, so a mutated query must not reuse the previous entry
		query.Values[1].ExpectContiguousMatch = true
		after := ScoreItemFuzzy[uri](nil, resource, query, true, acc, cache)

		if cache.Len() != 2 {
			t.Errorf("%d entries, want 2", cache.Len())
		}
		want := ScoreItemFuzzy[uri](nil, resource, PrepareQuery(`xyz "sm"`), true, acc, NewCache())
		if !reflect.DeepEqual(after, want) {
			t.Errorf("after = %+v, want %+v", after, want)
		}
	})
}

func TestScoreItemInvalidInput(t *testing.T) {
	// test: scoreItem - invalid input
	// VS Code passes a null query; the Go query is a pointer.
	if res := ScoreItemFuzzy[*uri](nil, nil, nil, true, nilSafeAccessor{}, NewCache()); res.Score != 0 {
		t.Errorf("nil query scored %d", res.Score)
	}
	if res := scoreItem[*uri](nil, "null", true, nilSafeAccessor{}, nil); res.Score != 0 {
		t.Errorf("nil item scored %d", res.Score)
	}
}

// matchCase is a scoreItem test with expected matches.
type matchCase struct {
	name  string
	path  string
	query string
	// the expected matches; nil are not checked
	label, desc []Match
	noScore     bool
}

func TestScoreItemMatches(t *testing.T) {
	tests := []matchCase{
		// test: scoreItem - optimize for file paths
		// xsp is more relevant to the end of the file path even though it matches
		// fuzzy also in the beginning. we verify the more relevant match at the
		// end gets returned.
		{name: "optimize for file paths", path: "/xyz/others/spath/some/xsp/file123.txt", query: "xspfile123",
			label: []Match{{0, 7}}, desc: []Match{{23, 26}}},
		// test: scoreItem - avoid match scattering (bug #36119)
		{name: "avoid match scattering (bug #36119)", path: "projects/ui/cula/ats/target.mk", query: "tcltarget.mk",
			label: []Match{{0, 9}}},
		// test: scoreItem - prefers more compact matches
		// expect "ad" to be matched towards the end of the file because the match is more compact
		{name: "prefers more compact matches", path: "/1a111d1/11a1d1/something.txt", query: "ad",
			label: []Match{}, desc: []Match{{11, 12}, {13, 14}}},
		// test: scoreItem - proper target offset #2
		{name: "proper target offset #2", path: "ede", query: "de", label: []Match{{1, 3}}},
		// test: scoreItem - proper target offset #3
		{name: "proper target offset #3", path: "/src/vs/editor/browser/viewParts/lineNumbers/flipped-cursor-2x.svg", query: "debug",
			label: []Match{{9, 10}, {20, 21}}, desc: []Match{{9, 10}, {36, 37}, {40, 41}}},
		// test: scoreItem - proper target offset
		{name: "proper target offset", path: "etem", query: "teem", noScore: true},
		// test: scoreItem - no match unless query contained in sequence
		{name: "no match unless query contained in sequence", path: "abcde", query: "edcda", noScore: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := scoreItem[uri](file(tt.path), tt.query, true, resourceAccessor{}, nil)
			if tt.noScore {
				if res.Score != 0 {
					t.Errorf("scored %d", res.Score)
				}
				return
			}
			if res.Score == 0 {
				t.Fatal("no score")
			}
			if tt.label != nil && !slices.Equal(res.LabelMatch, tt.label) {
				t.Errorf("label match = %v, want %v", res.LabelMatch, tt.label)
			}
			if tt.desc != nil && !slices.Equal(res.DescriptionMatch, tt.desc) {
				t.Errorf("description match = %v, want %v", res.DescriptionMatch, tt.desc)
			}
			if tt.path == "/1a111d1/11a1d1/something.txt" && len(res.LabelMatch) != 0 {
				t.Errorf("label match = %v, want none", res.LabelMatch)
			}
		})
	}
}

func TestScoreItemSlashAndBackslash(t *testing.T) {
	// test: scoreItem - match if using slash or backslash (local, remote resource)
	accessors := map[string]ItemAccessor[uri]{
		"resource":  resourceAccessor{},
		"slash":     resourceWithSlashAccessor{},
		"backslash": resourceWithBackslashAccessor{},
	}
	resources := map[string]uri{
		"local":  file("abcde/super/duper"),
		"remote": remote("abcde/super/duper"),
	}
	for rn, resource := range resources {
		for an, acc := range accessors {
			for _, query := range []string{`abcde\super\duper`, "abcde/super/duper"} {
				t.Run(rn+"/"+an+"/"+query, func(t *testing.T) {
					if res := scoreItem(resource, query, true, acc, nil); res.Score == 0 {
						t.Error("no score")
					}
				})
			}
		}
	}
}

func TestScoreItemUpperCaseBonus(t *testing.T) {
	// test: scoreItem - ensure upper case bonus only applies on non-consecutive matches (bug #134723)
	acc := resourceAccessor{}
	upper := scoreItem[uri](file("ASDFasdfasdf"), "asdf", true, acc, nil).Score
	lower := scoreItem[uri](file("asdfasdfasdf"), "asdf", true, acc, nil).Score
	if lower <= upper {
		t.Errorf("all lower %d <= with upper %d", lower, upper)
	}
}

// orderCase is a compareItemsByScore test: items in one or more orders sorted by one or more
// queries must come out as want (indexes in items; when want is shorter than items only the
// first ones are checked).
type orderCase struct {
	name    string
	items   []string
	queries []string
	want    []int
	// orders are the orders the items are given in; by default as listed and reversed
	orders [][]int
}

func TestCompareItemsByScore(t *testing.T) {
	abc := []string{"/some/path/fileA.txt", "/some/path/other/fileB.txt", "/unrelated/some/path/other/fileC.txt"}
	tests := []orderCase{
		// test: compareItemsByScore - identity
		{name: "identity A", items: abc, queries: []string{"/some/path/fileA.txt"}, want: []int{0, 1, 2}},
		{name: "identity B", items: abc, queries: []string{"/some/path/other/fileB.txt"}, want: []int{1, 0, 2}},
		// test: compareFilesByScore - basename prefix
		{name: "basename prefix A", items: abc, queries: []string{"fileA.txt"}, want: []int{0, 1, 2}},
		{name: "basename prefix B", items: abc, queries: []string{"fileB.txt"}, want: []int{1, 0, 2}},
		// test: compareFilesByScore - basename camelcase
		{name: "basename camelcase A", items: abc, queries: []string{"fA"}, want: []int{0, 1, 2}},
		{name: "basename camelcase B", items: abc, queries: []string{"fB"}, want: []int{1, 0, 2}},
		// test: compareFilesByScore - basename scores
		{name: "basename scores A", items: abc, queries: []string{"fileA"}, want: []int{0, 1, 2}},
		{name: "basename scores B", items: abc, queries: []string{"fileB"}, want: []int{1, 0, 2}},
		// test: compareFilesByScore - path scores
		{name: "path scores A", items: abc, queries: []string{"pathfileA"}, want: []int{0, 1, 2}},
		{name: "path scores B", items: abc, queries: []string{"pathfileB"}, want: []int{1, 0, 2}},
		// test: compareFilesByScore - prefer shorter basenames
		{name: "prefer shorter basenames", queries: []string{"somepath"}, want: []int{0, 1, 2},
			items: []string{"/some/path/fileA.txt", "/some/path/other/fileBLonger.txt", "/unrelated/the/path/other/fileC.txt"}},
		// test: compareFilesByScore - prefer shorter basenames (match on basename)
		{name: "prefer shorter basenames (match on basename)", queries: []string{"file"}, want: []int{0, 2, 1},
			items: []string{"/some/path/fileA.txt", "/some/path/other/fileBLonger.txt", "/unrelated/the/path/other/fileC.txt"}},
		// test: compareFilesByScore - prefer shorter paths
		{name: "prefer shorter paths", items: abc, queries: []string{"somepath"}, want: []int{0, 1, 2}},
		// test: compareFilesByScore - prefer shorter paths (bug #17443)
		{name: "prefer shorter paths (bug #17443)", queries: []string{"co/te"}, want: []int{1, 0, 2},
			items:  []string{"config/test/t1.js", "config/test.js", "config/test/t2.js"},
			orders: [][]int{{0, 1, 2}}},
		// test: compareFilesByScore - prefer matches in label over description if scores are otherwise equal
		{name: "prefer matches in label over description", queries: []string{"partsquick"}, want: []int{1, 0},
			items:  []string{"parts/quick/arrow-left-dark.svg", "parts/quickopen/quickopen.ts"},
			orders: [][]int{{0, 1}}},
		// test: compareFilesByScore - prefer camel case matches
		{name: "prefer camel case matches", queries: []string{"npe", "NPE"}, want: []int{0, 1},
			items: []string{"config/test/NullPointerException.java", "config/test/nopointerexception.java"}},
		// test: compareFilesByScore - prefer more compact camel case matches
		{name: "prefer more compact camel case matches", queries: []string{"AH"}, want: []int{1, 0},
			items: []string{"config/test/openthisAnythingHandler.js", "config/test/openthisisnotsorelevantforthequeryAnyHand.js"}},
		// test: compareFilesByScore - prefer more compact matches (label)
		{name: "prefer more compact matches (label)", queries: []string{"xp"}, want: []int{1, 0},
			items: []string{"config/test/examasdaple.js", "config/test/exampleasdaasd.ts"}},
		// test: compareFilesByScore - prefer more compact matches (path)
		{name: "prefer more compact matches (path)", queries: []string{"xp"}, want: []int{1, 0},
			items: []string{"config/test/examasdaple/file.js", "config/test/exampleasdaasd/file.ts"}},
		// test: compareFilesByScore - prefer more compact matches (label and path)
		{name: "prefer more compact matches (label and path)", queries: []string{"exfile"}, want: []int{1, 0},
			items: []string{"config/example/thisfile.ts", "config/24234243244/example/file.js"}},
	}

	// test: compareFilesByScore - avoid match scattering (bug #34210)
	scatter34210 := []string{
		"node_modules1/bundle/lib/model/modules/ot1/index.js",
		"node_modules1/bundle/lib/model/modules/un1/index.js",
		"node_modules1/bundle/lib/model/modules/modu1/index.js",
		"node_modules1/bundle/lib/model/modules/oddl1/index.js",
	}
	tests = append(tests,
		orderCase{name: "avoid match scattering (bug #34210) modu1", items: scatter34210, queries: []string{"modu1/index.js"}, want: []int{2},
			orders: [][]int{{0, 1, 2, 3}, {2, 1, 0, 3}}},
		orderCase{name: "avoid match scattering (bug #34210) un1", items: scatter34210, queries: []string{"un1/index.js"}, want: []int{1},
			orders: [][]int{{0, 1, 2, 3}, {2, 1, 0, 3}}},
	)

	// the rest of the "avoid match scattering" tests and the ones after them
	bug21019 := []string{
		"app/containers/Services/NetworkData/ServiceDetails/ServiceLoad/index.js",
		"app/containers/Services/NetworkData/ServiceDetails/ServiceDistribution/index.js",
		"app/containers/Services/NetworkData/ServiceDetailTabs/ServiceTabs/StatVideo/index.js",
	}
	bug26649 := []string{
		"photobook/src/components/AddPagesButton/index.js",
		"photobook/src/components/ApprovalPageHeader/index.js",
		"photobook/src/canvasComponents/BookPage/index.js",
	}
	bug32918 := []string{
		"adsys/protected/config.php",
		"adsys/protected/framework/smarty/sysplugins/smarty_internal_config.php",
		"duowanVideo/wap/protected/config.php",
	}
	bug12095 := []string{
		"src/vs/workbench/contrib/files/common/explorerViewModel.ts",
		"src/vs/workbench/contrib/files/browser/views/explorerView.ts",
		"src/vs/workbench/contrib/files/browser/views/explorerViewer.ts",
	}
	tests = append(tests,
		// test: compareFilesByScore - avoid match scattering (bug #21019 1.)
		orderCase{name: "avoid match scattering (bug #21019 1.)", items: bug21019, queries: []string{"StatVideoindex"}, want: []int{2}, orders: [][]int{{0, 1, 2}, {2, 1, 0}}},
		// test: compareFilesByScore - avoid match scattering (bug #21019 2.)
		orderCase{name: "avoid match scattering (bug #21019 2.)", queries: []string{"reproreduxts"}, want: []int{1},
			items: []string{"src/build-helper/store/redux.ts", "src/repository/store/redux.ts"}},
		// test: compareFilesByScore - avoid match scattering (bug #26649)
		orderCase{name: "avoid match scattering (bug #26649)", items: bug26649, queries: []string{"bookpageIndex"}, want: []int{2}, orders: [][]int{{0, 1, 2}, {2, 1, 0}}},
		// test: compareFilesByScore - avoid match scattering (bug #33247)
		orderCase{name: "avoid match scattering (bug #33247)", queries: []string{"ui/icons"}, want: []int{1},
			items: []string{"ui/src/utils/constants.js", "ui/src/ui/Icons/index.js"}},
		// test: compareFilesByScore - avoid match scattering (bug #33247 comment)
		orderCase{name: "avoid match scattering (bug #33247 comment)", queries: []string{"ui/input/index"}, want: []int{1},
			items: []string{"ui/src/components/IDInput/index.js", "ui/src/ui/Input/index.js"}},
		// test: compareFilesByScore - avoid match scattering (bug #36166)
		orderCase{name: "avoid match scattering (bug #36166)", queries: []string{"djancosig"}, want: []int{1},
			items: []string{"django/contrib/sites/locale/ga/LC_MESSAGES/django.mo", "django/core/signals.py"}},
		// test: compareFilesByScore - avoid match scattering (bug #32918)
		orderCase{name: "avoid match scattering (bug #32918)", items: bug32918, queries: []string{"protectedconfig.php"}, want: []int{0, 2, 1}},
		// test: compareFilesByScore - avoid match scattering (bug #14879)
		orderCase{name: "avoid match scattering (bug #14879)", queries: []string{"gradientmain"}, want: []int{1},
			items: []string{"pkg/search/gradient/testdata/constraint_attrMatchString.yml", "cmd/gradient/main.go"}},
		// test: compareFilesByScore - avoid match scattering (bug #14727 1)
		orderCase{name: "avoid match scattering (bug #14727 1)", queries: []string{"abc"}, want: []int{1},
			items: []string{"alpha-beta-cappa.txt", "abc.txt"}},
		// test: compareFilesByScore - avoid match scattering (bug #14727 2)
		orderCase{name: "avoid match scattering (bug #14727 2)", queries: []string{"xyz"}, want: []int{1},
			items: []string{"xerxes-yak-zubba/index.js", "xyz/index.js"}},
		// test: compareFilesByScore - avoid match scattering (bug #18381)
		orderCase{name: "avoid match scattering (bug #18381)", queries: []string{"async"}, want: []int{1},
			items: []string{"AssymblyInfo.cs", "IAsynchronousTask.java"}},
		// test: compareFilesByScore - avoid match scattering (bug #35572)
		orderCase{name: "avoid match scattering (bug #35572)", queries: []string{"partisettings"}, want: []int{1},
			items: []string{
				"static/app/source/angluar/-admin/-organization/-settings/layout/layout.js",
				"static/app/source/angular/-admin/-project/-settings/_settings/settings.js"}},
		// test: compareFilesByScore - avoid match scattering (bug #36810)
		orderCase{name: "avoid match scattering (bug #36810)", queries: []string{"tipsindex.cshtml"}, want: []int{1},
			items: []string{
				"Trilby.TrilbyTV.Web.Portal/Views/Systems/Index.cshtml",
				"Trilby.TrilbyTV.Web.Portal/Areas/Admins/Views/Tips/Index.cshtml"}},
		// test: compareFilesByScore - prefer shorter hit (bug #20546)
		orderCase{name: "prefer shorter hit (bug #20546)", queries: []string{"listview"}, want: []int{1},
			items: []string{"editor/core/components/tests/list-view-spec.js", "editor/core/components/list-view.js"}},
		// test: compareFilesByScore - avoid match scattering (bug #12095)
		orderCase{name: "avoid match scattering (bug #12095)", items: bug12095, queries: []string{"filesexplorerview.ts"}, want: []int{1},
			orders: [][]int{{0, 1, 2}, {0, 2, 1}}},
		// test: compareFilesByScore - prefer case match (bug #96122)
		orderCase{name: "prefer case match (bug #96122)", queries: []string{"Lists.php"}, want: []int{1},
			items: []string{"lists.php", "lib/Lists.php"}},
		// test: compareFilesByScore - prefer shorter match (bug #103052) - foo bar
		orderCase{name: "prefer shorter match (bug #103052) foo bar", queries: []string{"foo bar", "foobar"}, want: []int{0, 1},
			items: []string{"app/emails/foo.bar.js", "app/emails/other-footer.other-bar.js"}},
		// test: compareFilesByScore - prefer shorter match (bug #103052) - payment model
		orderCase{name: "prefer shorter match (bug #103052) payment model", queries: []string{"payment model", "paymentmodel"}, want: []int{0, 1},
			items: []string{"app/components/payment/payment.model.js", "app/components/online-payments-history/online-payments-history.model.js"}},
		// test: compareFilesByScore - prefer shorter match (bug #103052) - color
		orderCase{name: "prefer shorter match (bug #103052) color", queries: []string{"color js", "colorjs"}, want: []int{0, 1},
			items: []string{"app/constants/color.js", "app/components/model/input/pick-avatar-color.js"}},
		// test: compareFilesByScore - prefer strict case prefix
		orderCase{name: "prefer strict case prefix Color", queries: []string{"Color"}, want: []int{1, 0},
			items: []string{"app/constants/color.js", "app/components/model/input/Color.js"}},
		orderCase{name: "prefer strict case prefix color", queries: []string{"color"}, want: []int{0, 1},
			items: []string{"app/constants/color.js", "app/components/model/input/Color.js"}},
		// test: compareFilesByScore - prefer prefix (bug #103052)
		orderCase{name: "prefer prefix (bug #103052)", queries: []string{"smoke main.ts"}, want: []int{0, 1},
			items: []string{"test/smoke/src/main.ts", "src/vs/editor/common/services/semantikTokensProviderStyling.ts"}},
		// test: compareFilesByScore - boost better prefix match if multiple queries are used
		orderCase{name: "boost better prefix match if multiple queries are used", queries: []string{"workbench.ts browser", "browser workbench.ts", "browser workbench", "workbench browser"}, want: []int{1, 0},
			items: []string{"src/vs/workbench/services/host/browser/browserHostService.ts", "src/vs/workbench/browser/workbench.ts"}},
		// test: compareFilesByScore - boost shorter prefix match if multiple queries are used
		orderCase{name: "boost shorter prefix match if multiple queries are used", queries: []string{"window node", "window.ts node"}, want: []int{1, 0},
			items: []string{"src/vs/workbench/node/actions/windowActions.ts", "src/vs/workbench/electron-node/window.ts"}},
		// test: compareFilesByScore - skip preference on label match when using path sep
		orderCase{name: "skip preference on label match when using path sep", queries: []string{"url/def"}, want: []int{1, 0},
			items: []string{"djangosite/ufrela/def.py", "djangosite/urls/default.py"}},
		// test: compareFilesByScore - boost shorter prefix match if multiple queries are used (#99171)
		orderCase{name: "boost shorter prefix match if multiple queries are used (#99171)", queries: []string{"m life, life m"}, want: []int{1, 0},
			items: []string{"mesh_editor_lifetime_job.h", "lifetime_job.h"}},
		// test: compareFilesByScore - boost consecutive matches in the beginning over end
		orderCase{name: "boost consecutive matches in the beginning over end", queries: []string{"notStatus"}, want: []int{1, 0},
			items: []string{"src/vs/server/node/extensionHostStatusService.ts", "src/vs/workbench/browser/parts/notifications/notificationsStatus.ts"}},
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orders := tt.orders
			if orders == nil {
				fwd := make([]int, len(tt.items))
				for i := range fwd {
					fwd[i] = i
				}
				rev := slices.Clone(fwd)
				slices.Reverse(rev)
				orders = [][]int{fwd, rev}
			}
			for _, query := range tt.queries {
				for _, order := range orders {
					var items []uri
					for _, i := range order {
						items = append(items, file(tt.items[i]))
					}
					res := sortByScore(items, query)
					for i, w := range tt.want {
						if res[i] != file(tt.items[w]) {
							t.Errorf("query %q, items %v: result %d is %s, want %s", query, order, i, res[i].fsPath, tt.items[w])
						}
					}
				}
			}
		})
	}
}

func TestPrepareQuery(t *testing.T) {
	// test: prepareQuery
	tests := []struct {
		name  string
		query string
		check func(t *testing.T, q *PreparedQuery)
	}{
		{"wildcards", " f*a ", func(t *testing.T, q *PreparedQuery) { eq(t, q.Normalized, "fa") }},
		{"ellipsis", " f…a ", func(t *testing.T, q *PreparedQuery) { eq(t, q.Normalized, "fa") }},
		{"trailing hash", "main#", func(t *testing.T, q *PreparedQuery) {
			eq(t, q.Normalized, "main")
			eq(t, q.Original, "main#")
		}},
		{"trailing star", "foo*", func(t *testing.T, q *PreparedQuery) {
			eq(t, q.Normalized, "foo")
			eq(t, q.Original, "foo*")
		}},
		{"two words", "model Tester.ts", func(t *testing.T, q *PreparedQuery) {
			eq(t, q.Original, "model Tester.ts")
			eq(t, q.OriginalLowercase, strings.ToLower("model Tester.ts"))
			eq(t, q.Normalized, "modelTester.ts")
			eq(t, q.ExpectContiguousMatch, false) // doesn't have quotes in it
		}},
		{"lowercase", "Model Tester.ts", func(t *testing.T, q *PreparedQuery) { eq(t, q.NormalizedLowercase, "modeltester.ts") }},
		{"no path separator", "ModelTester.ts", func(t *testing.T, q *PreparedQuery) { eq(t, q.ContainsPathSeparator, false) }},
		{"path separator", "Model/Tester.ts", func(t *testing.T, q *PreparedQuery) { eq(t, q.ContainsPathSeparator, true) }},
		{"quotes", `"hello"`, func(t *testing.T, q *PreparedQuery) {
			eq(t, q.ExpectContiguousMatch, true)
			eq(t, q.Normalized, "hello")
		}},
		{"with spaces", "He*llo World", func(t *testing.T, q *PreparedQuery) {
			eq(t, q.Original, "He*llo World")
			eq(t, q.Normalized, "HelloWorld")
			eq(t, q.NormalizedLowercase, strings.ToLower("HelloWorld"))
			eq(t, len(q.Values), 2)
			eq(t, q.Values[0].Original, "He*llo")
			eq(t, q.Values[0].Normalized, "Hello")
			eq(t, q.Values[0].NormalizedLowercase, strings.ToLower("Hello"))
			eq(t, q.Values[1].Original, "World")
			eq(t, q.Values[1].Normalized, "World")
			eq(t, q.Values[1].NormalizedLowercase, strings.ToLower("World"))

			restored := PiecesToQuery(q.Values)
			eq(t, restored.Original, q.Original)
			eq(t, len(restored.Values), len(q.Values))
			eq(t, restored.ContainsPathSeparator, q.ContainsPathSeparator)
		}},
		{"with spaces that are empty", " Hello   World  \t", func(t *testing.T, q *PreparedQuery) {
			eq(t, q.Original, " Hello   World  \t")
			eq(t, q.OriginalLowercase, strings.ToLower(" Hello   World  \t"))
			eq(t, q.Normalized, "HelloWorld")
			eq(t, q.NormalizedLowercase, strings.ToLower("HelloWorld"))
			eq(t, len(q.Values), 2)
			eq(t, q.Values[0].Original, "Hello")
			eq(t, q.Values[0].OriginalLowercase, strings.ToLower("Hello"))
			eq(t, q.Values[0].Normalized, "Hello")
			eq(t, q.Values[0].NormalizedLowercase, strings.ToLower("Hello"))
			eq(t, q.Values[1].Original, "World")
			eq(t, q.Values[1].OriginalLowercase, strings.ToLower("World"))
			eq(t, q.Values[1].Normalized, "World")
			eq(t, q.Values[1].NormalizedLowercase, strings.ToLower("World"))
		}},
		{"posix path", "/some/path", func(t *testing.T, q *PreparedQuery) {
			eq(t, q.PathNormalized, "/some/path")
			eq(t, q.Normalized, "/some/path")
			eq(t, q.ContainsPathSeparator, true)
		}},
		{"backslash path", `\some\path`, func(t *testing.T, q *PreparedQuery) {
			eq(t, q.PathNormalized, "/some/path")
			eq(t, q.Normalized, "/some/path")
			eq(t, q.ContainsPathSeparator, true)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.check(t, PrepareQuery(tt.query)) })
	}
}

func eq[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFuzzyScore2Matching(t *testing.T) {
	// test: fuzzyScore2 (matching)
	target := "HelLo-World"
	for _, offset := range []int{0, 3} {
		tgt := target
		if offset != 0 {
			tgt = "123" + target
		}
		res := doScore2(tgt, "HelLo-World", offset)
		if !res.Matched || res.Score == 0 || !slices.Equal(res.Matches, []Match{{offset, len(target) + offset}}) {
			t.Errorf("offset %d, HelLo-World: %+v", offset, res)
		}

		res = doScore2(tgt, "HW", offset)
		if !res.Matched || res.Score == 0 || !slices.Equal(res.Matches, []Match{{offset, 1 + offset}, {6 + offset, 7 + offset}}) {
			t.Errorf("offset %d, HW: %+v", offset, res)
		}
	}
}

func TestFuzzyScore2MultipleQueries(t *testing.T) {
	// test: fuzzyScore2 (multiple queries)
	target := "HelLo-World"

	first := doScore2(target, "HelLo", 0)
	second := doScore2(target, "World", 0)
	firstAndSecondSingleMatches := append(slices.Clone(first.Matches), second.Matches...)

	tests := []struct {
		query   string
		matches bool
	}{
		{"HelLo World", true},
		{"World HelLo", true},
		{"World HelLo World", true},
		{"World HelLo Nothing", false},
		{"More Nothing", false},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			multi := doScore2(target, tt.query, 0)
			if !tt.matches {
				if multi.Matched || len(multi.Matches) != 0 {
					t.Errorf("matched: %+v", multi)
				}
				return
			}
			if multi.Score < first.Score+second.Score {
				t.Errorf("score %d < %d + %d", multi.Score, first.Score, second.Score)
			}
			for i, m := range multi.Matches {
				if i >= len(firstAndSecondSingleMatches) || m != firstAndSecondSingleMatches[i] {
					t.Errorf("match %d = %v, singles %v", i, m, firstAndSecondSingleMatches)
				}
			}
		})
	}
}

func TestFuzzyScore2Emoji(t *testing.T) {
	// test: fuzzyScore2 (#95716)
	res := doScore2("# ❌ Wow", "❌", 0)
	if !res.Matched || len(res.Matches) == 0 {
		t.Errorf("no match: %+v", res)
	}
	// the emoji is the third rune
	if !slices.Equal(res.Matches, []Match{{2, 3}}) {
		t.Errorf("matches = %v", res.Matches)
	}
}

func TestQuotesExpectContiguousMatches(t *testing.T) {
	// test: Using quotes should expect contiguous matches match
	// missing the "i" in the query
	if got := doScore("contiguous", `"contguous"`, nil).Score; got != 0 {
		t.Errorf("missing i: %d", got)
	}
	if got := doScore("contiguous", `"contiguous"`, nil).Score; got <= 0 {
		t.Errorf("contiguous: %d", got)
	}
}

func TestQuotesHighlightContiguousIndexes(t *testing.T) {
	// test: Using quotes should highlight contiguous indexes
	score := doScore("2021-7-26.md", `"26"`, nil)
	if score.Score != 14 {
		t.Errorf("score = %d, want 14", score.Score)
	}
	// The indexes of the 2 and 6 of "26"
	if !slices.Equal(score.Positions, []int{7, 8}) {
		t.Errorf("positions = %v", score.Positions)
	}
}

func TestWorkspaceSymbolSearchWithSpecialCharacters(t *testing.T) {
	// test: Workspace symbol search with special characters (#, *)
	// Simulates the scenario where rust-analyzer uses # and * as query modifiers. The original
	// query (with special chars) should reach the language server but the normalized query
	// (without special chars) should be used for fuzzy matching
	tests := []struct {
		name       string
		query      string
		normalized string
		target     string
	}{
		{"main#", "main#", "main", "main"},
		{"foo*", "foo*", "foo", "foo"},
		{"both", "MyClass#*", "MyClass", "MyClass"},
		{"fuzzy still works", "MC#", "MC", "MyClass"},
		// Make sure leading # or # in the middle are not removed.
		{"leading hash", "#SpecialFunction", "#SpecialFunction", "#SpecialFunction"},
		// Make sure standalone # is not removed
		{"standalone hash", "#", "#", "#"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := PrepareQuery(tt.query)
			eq(t, q.Original, tt.query) // sent to the language server
			eq(t, q.Normalized, tt.normalized)
			res := ScoreFuzzy2(tt.target, PrepareQuery(tt.query), 0, 0)
			if !res.Matched || res.Score <= 0 || len(res.Matches) == 0 {
				t.Errorf("no match: %+v", res)
			}
		})
	}
}
