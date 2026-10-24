package workspace

import (
	"cmp"
	"context"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
)

// DefaultMaxResults is the number of results a file search returns: VS Code's MAX_RESULTS of the
// quick open.
//
// Ported from VS Code: src/vs/workbench/contrib/search/browser/anythingQuickAccess.ts
const DefaultMaxResults = 512

// SearchOptions say how a file search runs.
type SearchOptions struct {
	// Max is the number of results to return; DefaultMaxResults when not positive.
	Max int
	// Skip, when set, tells the paths to leave out (the files that the recently opened group shows).
	Skip func(path string) bool
	// Workers is the number of goroutines that score; the number of CPUs (at most 8) when not positive.
	Workers int
}

// FileHit is a file that matched a search.
type FileHit struct {
	// Path is the path relative to the folder, with "/".
	Path string
	// Score is the fuzzy score; a higher one is a better match.
	Score int
	// LabelMatch are the matches (in runes) in the file name, DescriptionMatch the ones in the folder
	// part of the path.
	LabelMatch, DescriptionMatch []fuzzy.Match
}

// pathAccessor lets the scorer see a relative path as a file name (the label), the folder it is in (the
// description) and the whole path.
type pathAccessor struct{}

func (pathAccessor) ItemLabel(p string) string { return p[strings.LastIndexByte(p, '/')+1:] }
func (pathAccessor) ItemDescription(p string) string {
	if i := strings.LastIndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return ""
}
func (pathAccessor) ItemPath(p string) string { return p }

// minChunk is the least number of paths a goroutine scores; smaller lists are scored by the caller.
const minChunk = 8192

// ctxEvery is how many paths are scored between looks at the context.
const ctxEvery = 256

// scored is a path that matched: its place in the list and its score.
type scored struct {
	idx   int32
	score int32
}

// Search finds the files whose names match the query, best first, with VS Code's fuzzy scorer (the
// query may have several words, a path separator, or quotes for an exact match). An empty query gives
// no results. The search takes the files found so far (Snapshot), scores them on several goroutines,
// and stops with an error tagged FileSearchCancelled when ctx is done. The result is the same for the
// same files and query, whatever the number of goroutines.
func (x *FileIndex) Search(ctx context.Context, query string, opts SearchOptions) ([]FileHit, error) {
	return SearchPaths(ctx, x.Snapshot().Files, query, opts)
}

// SearchPaths is Search over a list of relative paths.
func SearchPaths(ctx context.Context, paths []string, query string, opts SearchOptions) ([]FileHit, error) {
	prepared := fuzzy.PrepareQuery(query)
	if prepared.Normalized == "" || len(paths) == 0 {
		return nil, nil
	}
	maxResults := opts.Max
	if maxResults <= 0 {
		maxResults = DefaultMaxResults
	}
	hits, err := scoreAll(ctx, paths, prepared, opts)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}
	// A score decides the order whenever two scores differ, so only the files with a score at or over
	// the one at the end of the list can be in it; the comparison of VS Code breaks the ties.
	if len(hits) > maxResults {
		scores := make([]int32, len(hits))
		for i, h := range hits {
			scores[i] = h.score
		}
		slices.SortFunc(scores, func(a, b int32) int { return cmp.Compare(b, a) })
		cutoff := scores[maxResults-1]
		hits = slices.DeleteFunc(hits, func(h scored) bool { return h.score < cutoff })
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("FileSearchCancelled", err).SetMetadata("query", query)
	}
	s := fuzzy.NewScorer()
	cache := fuzzy.NewCacheSize(len(hits))
	acc := pathAccessor{}
	slices.SortFunc(hits, func(a, b scored) int {
		if c := fuzzy.CompareItemsByFuzzyScore[string](s, paths[a.idx], paths[b.idx], prepared, true, acc, cache); c != 0 {
			return c
		}
		return cmp.Compare(a.idx, b.idx)
	})
	if len(hits) > maxResults {
		hits = hits[:maxResults]
	}
	out := make([]FileHit, len(hits))
	for i, h := range hits {
		p := paths[h.idx]
		is := fuzzy.ScoreItemFuzzy[string](s, p, prepared, true, acc, cache)
		out[i] = FileHit{Path: p, Score: is.Score, LabelMatch: is.LabelMatch, DescriptionMatch: is.DescriptionMatch}
	}
	return out, nil
}

// scoreAll scores every path and returns the ones that match, in the order of the list.
func scoreAll(ctx context.Context, paths []string, query *fuzzy.PreparedQuery, opts SearchOptions) ([]scored, error) {
	workers := opts.Workers
	if workers <= 0 {
		workers = min(runtime.GOMAXPROCS(0), 8)
	}
	workers = max(1, min(workers, (len(paths)+minChunk-1)/minChunk))
	parts := make([][]scored, workers)
	var wg sync.WaitGroup
	chunk := (len(paths) + workers - 1) / workers
	run := func(w int) {
		lo, hi := w*chunk, min((w+1)*chunk, len(paths))
		s := fuzzy.NewScorer()
		acc := pathAccessor{}
		var out []scored
		for i := lo; i < hi; i++ {
			if (i-lo)%ctxEvery == ctxEvery-1 && ctx.Err() != nil {
				return
			}
			p := paths[i]
			if opts.Skip != nil && opts.Skip(p) {
				continue
			}
			if score := fuzzy.ScoreItemFuzzy[string](s, p, query, true, acc, nil).Score; score > 0 {
				out = append(out, scored{idx: int32(i), score: int32(score)})
			}
		}
		parts[w] = out
	}
	if workers == 1 {
		run(0)
	} else {
		for w := range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				run(w)
			}()
		}
		wg.Wait()
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("FileSearchCancelled", err).SetMetadata("query", query.Original)
	}
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	out := make([]scored, 0, total)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out, nil
}
