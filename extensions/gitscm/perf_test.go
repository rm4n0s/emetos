//go:build emetos_perf

package gitscm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// TestPerfManyChangedFiles measures the status of a repository with 100,000 changed files (SPEC §14, step 24.5): the folder is the
// `many` fixture that scripts/perf-git.sh builds (EMETOS_PERF_GIT_DIR). It prints the lines "PERF <name> <value>" that the script
// puts into its table. Run it alone (scripts/heavy.sh --alone): the numbers are times.
func TestPerfManyChangedFiles(t *testing.T) {
	dir := os.Getenv("EMETOS_PERF_GIT_DIR")
	if dir == "" {
		t.Skip("EMETOS_PERF_GIT_DIR names the fixture of scripts/git-fixture.sh many")
	}
	gittest.Require(t)
	e := gittest.NewEnv(t)
	e.Trust(dir)
	out := func(name string, v any) { fmt.Printf("PERF %s %v\n", name, v) }

	start := time.Now()
	r := newRig(t, e, rigOptions{folder: dir, home: filepath.Dir(dir), settings: map[string]any{"git.statusLimit": 1_000_000, "git.untrackedChanges": "separate"}})
	r.api.WaitFor("the first status of the fixture", func() bool {
		r.api.Pump()
		rep := r.x.model.byRoot(dir)
		return rep != nil && rep.refreshed && !rep.refreshing
	})
	first := time.Since(start)
	rep := r.repo(dir)
	total := rep.res.total()
	out("entries", total)
	out("first-status-end-to-end", first)
	out("first-git-status-and-parse", rep.timings.Status)
	out("first-group-off-ui-thread", rep.timings.Group)
	out("first-apply-on-ui-thread", rep.timings.Apply)
	if total < 1000 {
		t.Fatalf("only %d changes: is EMETOS_PERF_GIT_DIR the many fixture?", total)
	}
	if rep.timings.Group > 150*time.Millisecond {
		t.Errorf("grouping took %v, budget 150ms", rep.timings.Group)
	}

	// the second status, with git's caches warm
	before := rep.refreshes
	rep.refresh()
	r.api.WaitFor("the second status", func() bool { r.api.Pump(); return rep.refreshes > before && !rep.refreshing })
	out("second-git-status-and-parse", rep.timings.Status)
	out("second-group-off-ui-thread", rep.timings.Group)
	out("second-apply-on-ui-thread", rep.timings.Apply)

	// a build that rewrites thousands of files: one status
	before = rep.refreshes
	var burst []extension.FileChange
	for i := 0; i < 5000; i++ {
		burst = append(burst, extension.FileChange{Kind: extension.FileChanged, Path: filepath.Join(dir, fmt.Sprintf("burst%04d.o", i))})
	}
	ws := r.api.Watches()
	if len(ws) != 1 {
		t.Fatalf("%d watches", len(ws))
	}
	ws[0].Fire(burst...)
	r.api.Pump()
	r.clock.Advance(DebounceDelay)
	r.api.WaitFor("the status of the burst", func() bool { r.api.Pump(); return rep.refreshes > before && !rep.refreshing })
	out("statuses-for-a-burst-of-5000-changes", rep.refreshes-before)
}

// TestPerfLongHistory measures the history provider of the Graph on a repository of 100,000 commits with merges all along (SPEC §14,
// step 24.20): the first page must come in 300 ms, and the pages after it in about the same time, whatever the place (git walks
// from the head and --skip is a counter). It prints "PERF <name> <value>". Run it alone (scripts/heavy.sh --alone): the numbers are times.
func TestPerfLongHistory(t *testing.T) {
	gittest.Require(t)
	e := gittest.NewEnv(t)
	buildStart := time.Now()
	repo := e.LongHistory(filepath.Join(t.TempDir(), "long"), 100_000)
	out := func(name string, v any) { fmt.Printf("PERF %s %v\n", name, v) }
	out("history-build", time.Since(buildStart))
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	r.api.WaitFor("the history provider", func() bool { r.api.Pump(); return r.source(rp.root).History() != nil })
	h := r.source(rp.root).History()
	ctx := context.Background()
	head := repo.Head()
	page := func(skip int, filter string) (time.Duration, int) {
		start := time.Now()
		items, err := h.Items(ctx, extension.HistoryOptions{Skip: skip, Limit: 50, RefIDs: []string{head}, FilterText: filter})
		if err != nil {
			t.Fatal(err)
		}
		return time.Since(start), len(items)
	}
	// git cannot order 100,000 commits topologically without looking at all of them: with no commit-graph file a page costs
	// about half a second, whatever its place. The budget is for a repository that git maintains ("git gc" and "git maintenance"
	// write the file), so it is measured after the file is written; the time without it is measured and must stay under 1.5 s.
	bare, _ := page(0, "")
	out("history-first-page-without-commit-graph", bare)
	if bare > 1500*time.Millisecond {
		t.Errorf("the first page without a commit-graph file took %v, limit 1.5s", bare)
	}
	repo.Git("commit-graph", "write", "--reachable")
	first, n := page(0, "")
	out("history-first-page", first)
	if n != 50 || first > 300*time.Millisecond {
		t.Errorf("the first page has %d items and took %v, budget 300ms", n, first)
	}
	for _, skip := range []int{1000, 50_000, 99_900} {
		d, n := page(skip, "")
		out(fmt.Sprintf("history-page-at-%d", skip), d)
		if n == 0 || d > 600*time.Millisecond {
			t.Errorf("the page at %d has %d items and took %v", skip, n, d)
		}
	}
	d, n := page(0, "revision 99999")
	out("history-search-message", d)
	if n != 1 {
		t.Errorf("the search of one message found %d commits", n)
	}
	refs := time.Now()
	if _, err := h.Refs(ctx, nil); err != nil {
		t.Fatal(err)
	}
	out("history-refs", time.Since(refs))
	anc := time.Now()
	if _, err := h.CommonAncestor(ctx, []string{"refs/heads/main"}); err != nil {
		t.Fatal(err)
	}
	out("history-common-ancestor", time.Since(anc))
	// the whole first read of the Graph: the page and the common ancestor at once (what the view waits for)
	end := time.Now()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); page(0, "") }()
	go func() { defer wg.Done(); _, _ = h.CommonAncestor(ctx, []string{"refs/heads/main", "refs/heads/main"}) }()
	wg.Wait()
	out("history-first-read", time.Since(end))
	if d := time.Since(end); d > 300*time.Millisecond {
		t.Errorf("the first read of the Graph took %v, budget 300ms", d)
	}
}
