package fuzzy

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

// The benchmark of SPEC §14: fuzzy file search over 100,000 paths, per keystroke, under 30 ms.
// One operation of BenchmarkScoreItems100k is one keystroke: the query scored against every path
// with a new cache, as VS Code does for a new query. BenchmarkScoreItems100kNoCache is the same
// without the cache, which costs 16 bytes and a map insert for each path. BenchmarkSearch100k is
// the pipeline of the quick open: score the paths without a cache, then sort the ones that
// matched with a cache for them, which is what a list shows. The numbers are in
// docs/PERFORMANCE.md.

var (
	benchDirs = []string{
		"internal", "cmd", "pkg", "core", "gui", "views", "explorer", "editor", "workbench", "kit",
		"buffer", "document", "settings", "keybinding", "workspace", "storage", "language", "theme",
		"tokens", "textmate", "find", "folding", "snippet", "emmet", "testdata", "goldens", "fixtures",
		"scripts", "docs", "third_party", "assets", "fonts", "icons", "commands", "menus", "dialogs",
		"notify", "output", "problems", "terminal", "search", "scm", "debug", "extensions", "tests",
		"util", "testutil", "proof", "reference", "vscode", "src", "vs", "base", "common", "browser",
		"contrib", "files", "services", "platform", "node", "electron-node", "api", "client", "server",
	}
	benchWords = []string{
		"body", "view", "model", "state", "widget", "tree", "list", "input", "button", "label",
		"scorer", "fuzzy", "query", "cache", "match", "filter", "compare", "sort", "index", "main",
		"options", "config", "service", "manager", "provider", "handler", "registry", "factory",
		"editor", "document", "buffer", "layout", "paint", "render", "theme", "color", "font",
		"keymap", "command", "action", "menu", "dialog", "notification", "toast", "status", "title",
		"explorer", "problems", "output", "terminal", "search", "replace", "folding", "snippet",
		"tokenizer", "grammar", "language", "selection", "cursor", "undo", "history", "watcher",
		"storage", "workspace", "settings", "extension", "debug", "adapter", "session", "client",
	}
	benchExts = []string{".go", ".go", ".go", ".go", "_test.go", "_test.go", ".json", ".md", ".ts", ".txt", ".svg", ".png"}
)

// benchPaths makes n deterministic, realistic relative paths.
func benchPaths(n int) []string {
	rng := rand.New(rand.NewPCG(1, 2))
	paths := make([]string, n)
	var sb strings.Builder
	for i := range paths {
		sb.Reset()
		for d, depth := 0, 2+rng.IntN(5); d < depth; d++ {
			sb.WriteString(benchDirs[rng.IntN(len(benchDirs))])
			sb.WriteByte('/')
		}
		sb.WriteString(benchWords[rng.IntN(len(benchWords))])
		if rng.IntN(3) == 0 {
			// camel case or snake case compound names
			w := benchWords[rng.IntN(len(benchWords))]
			if rng.IntN(2) == 0 {
				sb.WriteString(strings.ToUpper(w[:1]) + w[1:])
			} else {
				sb.WriteString("_" + w)
			}
		}
		sb.WriteString(benchExts[rng.IntN(len(benchExts))])
		paths[i] = sb.String()
	}
	return paths
}

// pathAccessor scores a path string: the label is the name, the description the folder.
type pathAccessor struct{}

func (pathAccessor) ItemLabel(p string) string { return p[strings.LastIndexByte(p, '/')+1:] }
func (pathAccessor) ItemDescription(p string) string {
	if i := strings.LastIndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return ""
}
func (pathAccessor) ItemPath(p string) string { return p }

var benchQueries = []struct{ name, query string }{
	{"fuzzy edtvw", "edtvw"},
	{"path gui/exp/bo", "gui/exp/bo"},
	{"multi word view model", "view model"},
	{"no match zqxjk", "zqxjk"},
}

// scoreAll scores every path against the query and returns the number that matched.
func scoreAll(s *Scorer, paths []string, query *PreparedQuery, cache *Cache) int {
	matched := 0
	for _, p := range paths {
		if ScoreItemFuzzy[string](s, p, query, true, pathAccessor{}, cache).Score > 0 {
			matched++
		}
	}
	return matched
}

func BenchmarkScoreItems100k(b *testing.B) {
	paths := benchPaths(100_000)
	for _, bq := range benchQueries {
		b.Run(bq.name, func(b *testing.B) {
			query := PrepareQuery(bq.query)
			s := NewScorer()
			b.ReportAllocs()
			b.ResetTimer()
			matched := 0
			for range b.N {
				matched = scoreAll(s, paths, query, NewCacheSize(len(paths)))
			}
			b.ReportMetric(float64(matched), "matches")
		})
	}
}

// BenchmarkScoreItems100kNoCache is the scoring alone, without the cache (the map costs
// memory and time on its own).
func BenchmarkScoreItems100kNoCache(b *testing.B) {
	paths := benchPaths(100_000)
	for _, bq := range benchQueries {
		b.Run(bq.name, func(b *testing.B) {
			query := PrepareQuery(bq.query)
			s := NewScorer()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				scoreAll(s, paths, query, nil)
			}
		})
	}
}

func BenchmarkSearch100k(b *testing.B) {
	paths := benchPaths(100_000)
	for _, bq := range benchQueries {
		b.Run(bq.name, func(b *testing.B) {
			query := PrepareQuery(bq.query)
			s := NewScorer()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				search(s, paths, query)
			}
		})
	}
}

// search scores the paths without a cache and sorts the ones that match by the comparison of
// the quick open, with a cache that holds the matches only.
func search(s *Scorer, paths []string, query *PreparedQuery) []string {
	var matches []string
	for _, p := range paths {
		if ScoreItemFuzzy[string](s, p, query, true, pathAccessor{}, nil).Score > 0 {
			matches = append(matches, p)
		}
	}
	cache := NewCacheSize(len(matches))
	slices.SortStableFunc(matches, func(a, b string) int {
		return CompareItemsByFuzzyScore[string](s, a, b, query, true, pathAccessor{}, cache)
	})
	return matches
}

// TestScoring100kPathsWithinTarget checks the target of SPEC §14 (a keystroke over 100,000
// paths in under 30 ms) with a bound twenty times as big, so that a slow or busy machine, the
// race detector (which makes the scorer four times slower) and the coverage tool do not make it flaky: it catches a change that makes the
// scorer a lot slower. The real numbers are the benchmark's.
func TestScoring100kPathsWithinTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	const bound = 20 * 30 * time.Millisecond
	paths := benchPaths(100_000)
	s := NewScorer()
	for _, bq := range benchQueries {
		t.Run(bq.name, func(t *testing.T) {
			query := PrepareQuery(bq.query)
			best := time.Hour
			for range 3 { // the best of three: a busy machine slows single runs
				start := time.Now()
				scoreAll(s, paths, query, NewCacheSize(len(paths)))
				best = min(best, time.Since(start))
			}
			t.Logf("%s: %v for %d paths", bq.query, best, len(paths))
			if best > bound {
				t.Errorf("%v for 100,000 paths, bound %v", best, bound)
			}
		})
	}
}
