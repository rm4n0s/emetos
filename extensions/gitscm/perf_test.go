//go:build emetos_perf

package gitscm

import (
	"fmt"
	"os"
	"path/filepath"
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
