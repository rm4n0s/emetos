package workspace

import (
	"context"
	"path"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
)

// QuickOpenOptions say how QuickOpen searches.
type QuickOpenOptions struct {
	// Max is the number of file results; DefaultMaxResults when not positive.
	Max int
	// IncludeHistory is search.quickOpen.includeHistory: the recently opened files that match the query
	// are shown. The recently opened files are always shown for an empty query.
	IncludeHistory bool
	// HistoryByRecency is search.quickOpen.history.filterSortOrder == "recency": the matches among the
	// recently opened files keep the order of recency instead of the order of their score.
	HistoryByRecency bool
	// Home is the folder of the user, which paths outside the folder show as "~".
	Home string
	// Workers is SearchOptions.Workers.
	Workers int
}

// QuickOpenHit is a row of the quick open: a file with the texts the list shows.
type QuickOpenHit struct {
	// Path is the absolute path of the file.
	Path string
	// Name is the name of the file (the label of the row).
	Name string
	// Description is the folder of the file: relative to the opened folder, or absolute with "~" for the
	// folder of the user when the file is outside it. It is empty for a file in the opened folder itself.
	Description string
	// LabelMatch and DescriptionMatch are the matches (in runes) of the query in Name and Description.
	LabelMatch, DescriptionMatch []fuzzy.Match
	// Score is the fuzzy score of the match; 0 for the recently opened files of an empty query.
	Score int
}

// QuickOpenResult is what the quick open shows for a query.
type QuickOpenResult struct {
	// Recent are the recently opened files shown first: all of them for an empty query, the ones that
	// match for any other.
	Recent []QuickOpenHit
	// Files are the files of the folder that match, best first, without the ones in Recent. They are
	// empty for an empty query.
	Files []QuickOpenHit
	// Complete tells whether the list of files was complete when the search ran.
	Complete bool
}

// QuickOpen answers a query of the quick open: the recently opened files that match it, then the files
// of the folder. idx may be nil (no folder is open): only the recent files are searched. recent
// are absolute paths, the most recent first.
//
// Ported from VS Code: src/vs/workbench/contrib/search/browser/anythingQuickAccess.ts (getEditorHistoryPicks,
// getAdditionalPicks)
func QuickOpen(ctx context.Context, idx *FileIndex, recent []string, query string, o QuickOpenOptions) (QuickOpenResult, error) {
	prepared := fuzzy.PrepareQuery(query)
	root := ""
	if idx != nil {
		root = idx.Root()
	}
	res := QuickOpenResult{Complete: true}
	if idx != nil {
		res.Complete = idx.Snapshot().Complete
	}
	history := recentItems(recent, root, o.Home)
	// listed are the files of the folder that the recent group shows, by relative path
	listed := map[string]struct{}{}
	switch {
	case prepared.Normalized == "":
		for _, h := range history {
			res.Recent = append(res.Recent, QuickOpenHit{Path: h.abs, Name: h.label, Description: h.desc})
		}
		return res, nil
	case o.IncludeHistory:
		res.Recent = matchRecent(history, prepared, o)
		for _, h := range res.Recent {
			if rel, ok := strings.CutPrefix(h.Path, strings.TrimSuffix(root, "/")+"/"); ok && root != "" {
				listed[rel] = struct{}{}
			}
		}
	}
	if idx == nil {
		return res, nil
	}
	sopts := SearchOptions{Max: o.Max, Workers: o.Workers}
	if len(listed) > 0 {
		sopts.Skip = func(rel string) bool { _, ok := listed[rel]; return ok }
	}
	hits, err := idx.Search(ctx, query, sopts)
	if err != nil {
		return QuickOpenResult{}, err
	}
	res.Files = make([]QuickOpenHit, len(hits))
	for i, h := range hits {
		name, dir := path.Base(h.Path), ""
		if j := strings.LastIndexByte(h.Path, '/'); j > 0 {
			dir = h.Path[:j]
		}
		res.Files[i] = QuickOpenHit{Path: idx.abs(h.Path), Name: name, Description: dir, LabelMatch: h.LabelMatch, DescriptionMatch: h.DescriptionMatch, Score: h.Score}
	}
	if err := ctx.Err(); err != nil {
		return QuickOpenResult{}, errors.NewErr("FileSearchCancelled", err).SetMetadata("query", query)
	}
	return res, nil
}

// recentItem is a recently opened file with the texts it is scored on.
type recentItem struct {
	abs, rel, label, desc string
}

// recentItems describes the recent files: relative to the folder for the ones inside it, absolute (with
// "~") for the others.
func recentItems(recent []string, root, home string) []recentItem {
	out := make([]recentItem, 0, len(recent))
	for _, abs := range recent {
		it := recentItem{abs: abs, label: path.Base(abs)}
		if rel, ok := strings.CutPrefix(abs, strings.TrimSuffix(root, "/")+"/"); ok && root != "" {
			it.rel = rel
			if i := strings.LastIndexByte(rel, '/'); i > 0 {
				it.desc = rel[:i]
			}
		} else {
			dir := path.Dir(abs)
			if home != "" && (dir == home || strings.HasPrefix(dir, strings.TrimSuffix(home, "/")+"/")) {
				dir = "~" + dir[len(home):]
			}
			it.rel, it.desc = abs, dir
		}
		out = append(out, it)
	}
	return out
}

// recentAccessor scores the recent files; Label only unless the query has a path separator.
type recentAccessor struct{ labelOnly bool }

func (a recentAccessor) ItemLabel(it recentItem) string { return it.label }
func (a recentAccessor) ItemDescription(it recentItem) string {
	if a.labelOnly {
		return ""
	}
	return it.desc
}
func (a recentAccessor) ItemPath(it recentItem) string { return it.rel }

// matchRecent scores the recent files against the query and sorts them, unless the order is the one of
// recency.
func matchRecent(history []recentItem, q *fuzzy.PreparedQuery, o QuickOpenOptions) []QuickOpenHit {
	acc := recentAccessor{labelOnly: !q.ContainsPathSeparator}
	s := fuzzy.NewScorer()
	var matched []recentItem
	scores := map[string]fuzzy.ItemScore{}
	for _, it := range history {
		is := fuzzy.ScoreItemFuzzy[recentItem](s, it, q, false, acc, nil)
		if is.Score == 0 {
			continue
		}
		matched = append(matched, it)
		scores[it.abs] = is
	}
	if !o.HistoryByRecency {
		slices.SortStableFunc(matched, func(a, b recentItem) int {
			return fuzzy.CompareItemsByFuzzyScore[recentItem](s, a, b, q, false, acc, nil)
		})
	}
	out := make([]QuickOpenHit, len(matched))
	for i, it := range matched {
		is := scores[it.abs]
		out[i] = QuickOpenHit{Path: it.abs, Name: it.label, Description: it.desc, LabelMatch: is.LabelMatch, DescriptionMatch: is.DescriptionMatch, Score: is.Score}
	}
	return out
}
