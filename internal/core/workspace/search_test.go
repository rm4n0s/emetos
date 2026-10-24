package workspace

import (
	"context"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
)

var searchPaths = []string{
	"src/server/server.go",
	"src/server/util.go",
	"docs/server.md",
	"internal/serverless/handler.go",
	"src/main.go",
	"main_test.go",
	"cmd/emetos/main.go",
	"src/ServerHandler.go",
	"README.md",
	"src/a/b/c/deep_file.txt",
}

func paths(hits []FileHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Path
	}
	return out
}

func TestSearchPathsOrder(t *testing.T) {
	tests := []struct {
		name  string
		query string
		first []string // the first results, in order
		count int      // the number of results, -1 for any
	}{
		{"exact file name first", "server.go", []string{"src/server/server.go"}, -1},
		{"the whole path is an identity match", "src/server/util.go", []string{"src/server/util.go"}, 1},
		{"a prefix of the name over a match inside it", "main", []string{"src/main.go", "cmd/emetos/main.go", "main_test.go"}, -1},
		{"a shorter name wins among prefix matches", "server", []string{"docs/server.md"}, -1},
		{"camel case initials", "sh", []string{"src/ServerHandler.go"}, -1},
		// equal scores: the fallback comparison of VS Code orders the names (util.go before server.go here)
		{"a path query", "src/ser", []string{"src/server/util.go", "src/server/server.go", "src/ServerHandler.go"}, 3},
		{"several words must all match", "server util", []string{"src/server/util.go"}, 1},
		{"folder and file name", "emetos main", []string{"cmd/emetos/main.go"}, 1},
		{"no match", "zzzq", nil, 0},
		{"an empty query lists nothing", "", nil, 0},
		{"white space only", "   ", nil, 0},
		{"case is ignored", "README", []string{"README.md"}, 1},
		{"a quoted query matches contiguously", `"util"`, []string{"src/server/util.go"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hits, err := SearchPaths(context.Background(), searchPaths, tc.query, SearchOptions{})
			if err != nil {
				t.Fatal(err)
			}
			got := paths(hits)
			if tc.count >= 0 && len(got) != tc.count {
				t.Errorf("%d results %q, want %d", len(got), got, tc.count)
			}
			if len(got) < len(tc.first) || !slices.Equal(got[:len(tc.first)], tc.first) {
				t.Errorf("results %q, want to start with %q", got, tc.first)
			}
			for i := 1; i < len(hits); i++ {
				if hits[i-1].Score < hits[i].Score && hits[i-1].Score != fuzzy.PathIdentityScore {
					t.Errorf("scores out of order: %d then %d", hits[i-1].Score, hits[i].Score)
				}
			}
		})
	}
}

func TestSearchPathsHighlights(t *testing.T) {
	hits, err := SearchPaths(context.Background(), searchPaths, "srvr", SearchOptions{})
	if err != nil || len(hits) == 0 {
		t.Fatalf("%v %v", hits, err)
	}
	h := hits[0]
	if len(h.LabelMatch)+len(h.DescriptionMatch) == 0 {
		t.Errorf("no highlights: %+v", h)
	}
	for _, m := range h.LabelMatch {
		if m.End > len([]rune(h.Path[strings.LastIndexByte(h.Path, '/')+1:])) {
			t.Errorf("a match past the name: %+v", h)
		}
	}
}

func TestSearchPathsSkip(t *testing.T) {
	hits, err := SearchPaths(context.Background(), searchPaths, "server", SearchOptions{Skip: func(p string) bool { return p == "docs/server.md" }})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(paths(hits), "docs/server.md") || len(hits) == 0 {
		t.Errorf("results %q", paths(hits))
	}
}

// generated makes n deterministic paths.
func generated(n int, seed uint64) []string {
	rng := rand.New(rand.NewPCG(seed, 7))
	dirs := []string{"src", "internal", "cmd", "pkg", "view", "model", "editor", "core", "gui", "util"}
	words := []string{"body", "view", "model", "state", "widget", "tree", "list", "input", "button", "label", "scorer", "fuzzy"}
	out := make([]string, n)
	for i := range out {
		var sb strings.Builder
		for range 1 + rng.IntN(4) {
			sb.WriteString(dirs[rng.IntN(len(dirs))])
			sb.WriteByte('/')
		}
		sb.WriteString(words[rng.IntN(len(words))])
		fmt.Fprint(&sb, i%97)
		sb.WriteString([]string{".go", ".md", "_test.go", ".json"}[rng.IntN(4)])
		out[i] = sb.String()
	}
	return out
}

func TestSearchPathsIsDeterministicAndCapped(t *testing.T) {
	all := generated(3*minChunk+123, 1)
	for _, query := range []string{"view", "mdl", "src/vw", "e", "state button"} {
		t.Run(query, func(t *testing.T) {
			var base []FileHit
			for _, workers := range []int{1, 2, 3, 8} {
				hits, err := SearchPaths(context.Background(), all, query, SearchOptions{Workers: workers})
				if err != nil {
					t.Fatal(err)
				}
				if len(hits) > DefaultMaxResults {
					t.Errorf("%d results, the cap is %d", len(hits), DefaultMaxResults)
				}
				if workers == 1 {
					base = hits
					continue
				}
				if !reflect.DeepEqual(hits, base) {
					t.Errorf("%d workers give other results than one", workers)
				}
			}
			again, _ := SearchPaths(context.Background(), all, query, SearchOptions{Workers: 1})
			if !reflect.DeepEqual(again, base) {
				t.Error("two runs differ")
			}
			// a smaller cap is the start of the full list
			small, err := SearchPaths(context.Background(), all, query, SearchOptions{Max: 7})
			if err != nil {
				t.Fatal(err)
			}
			if want := base[:min(7, len(base))]; !slices.Equal(paths(small), paths(want)) {
				t.Errorf("top 7 %q, want %q", paths(small), paths(want))
			}
		})
	}
}

func TestSearchPathsMatchesTheFullSort(t *testing.T) {
	// the top of the list is what sorting every match with VS Code's comparison gives
	all := generated(2000, 3)
	query := "vw"
	prepared := fuzzy.PrepareQuery(query)
	s := fuzzy.NewScorer()
	var want []string
	for _, p := range all {
		if fuzzy.ScoreItemFuzzy[string](s, p, prepared, true, pathAccessor{}, nil).Score > 0 {
			want = append(want, p)
		}
	}
	cache := fuzzy.NewCache()
	slices.SortStableFunc(want, func(a, b string) int {
		return fuzzy.CompareItemsByFuzzyScore[string](s, a, b, prepared, true, pathAccessor{}, cache)
	})
	got, err := SearchPaths(context.Background(), all, query, SearchOptions{Max: 40})
	if err != nil {
		t.Fatal(err)
	}
	for i, h := range got {
		if i >= len(want) {
			break
		}
		// equal paths may repeat in the generated list; compare scores of the same rank
		if fuzzy.CompareItemsByFuzzyScore[string](s, h.Path, want[i], prepared, true, pathAccessor{}, cache) != 0 {
			t.Fatalf("rank %d: %q, the full sort has %q", i, h.Path, want[i])
		}
	}
}

func TestSearchPathsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, workers := range []int{1, 4} {
		_, err := SearchPaths(ctx, generated(4*minChunk, 2), "view", SearchOptions{Workers: workers})
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("SearchPaths->scoreAll.FileSearchCancelled") || !errors.Is(err, context.Canceled) {
			t.Errorf("workers %d: got %v", workers, err)
		}
	}
}
