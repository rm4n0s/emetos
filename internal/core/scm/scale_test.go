package scm

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// scaleResources makes n resource states: layout "nested" spreads them over 100 packages with 50 folders each, "flat" puts
// them all in one folder.
func scaleResources(n int, layout string) []Resource {
	rs := make([]Resource, n)
	for i := range rs {
		if layout == "flat" {
			rs[i] = Resource{URI: fmt.Sprintf("file:///repo/file%06d.go", n-i)}
		} else {
			rs[i] = Resource{URI: fmt.Sprintf("file:///repo/pkg%02d/dir%02d/file%05d.go", (i*7)%100, (i*13)%50, n-i)}
		}
		rs[i].Decorations.Tooltip = [...]string{"Modified", "Deleted", "Untracked"}[i%3]
	}
	return rs
}

// TestAHundredThousandResources sorts and treats 100,000 resources under a budget of time and memory: the sorts are made once
// for a version, the tree sorts only the folders that are asked for.
func TestAHundredThousandResources(t *testing.T) {
	const n = 100000
	// the budgets are far above what the machine needs (about a second), so a loaded machine and the race detector pass them
	// and a quadratic step does not
	const budget = 10 * time.Second
	for _, layout := range []string{"nested", "flat"} {
		t.Run(layout, func(t *testing.T) {
			if testing.Short() {
				t.Skip("100,000 resources")
			}
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			rs := scaleResources(n, layout)
			m := NewModel(DefaultConfig())
			s := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///repo"})
			g := s.AddGroup("workingTree", "Changes", false, "")

			timed := func(what string, fn func()) time.Duration {
				start := time.Now()
				fn()
				d := time.Since(start)
				t.Logf("%s: %v", what, d)
				if d > budget {
					t.Errorf("%s took %v, over the budget of %v", what, d, budget)
				}
				return d
			}
			timed("SetResources", func() { g.SetResources(rs) })
			for _, key := range []SortKey{SortByPath, SortByName, SortByStatus} {
				var list []*Resource
				timed("sort by "+string(key), func() { list = g.Sorted(key) })
				if len(list) != n {
					t.Fatalf("sorted %d resources", len(list))
				}
				// the sort is by file name, so the numbers of the names tell the order: the first is file000001/file00001
				if key == SortByName {
					if got := list[0].Name(); got != "file00001.go" && got != "file000001.go" {
						t.Errorf("first by name = %q", got)
					}
				}
				timed("sort by "+string(key)+" again", func() { g.Sorted(key) })
			}
			var tree *Tree
			timed("Tree", func() { tree = g.Tree() })
			rows := 0
			timed("the first two levels, compact", func() {
				for _, e := range tree.Children(tree.Root(), true) {
					rows++
					if e.IsFolder() {
						rows += len(tree.Children(e.Node, true))
					}
				}
			})
			if layout == "nested" && rows < 100 || layout == "flat" && rows != n {
				t.Errorf("rows = %d", rows)
			}
			if got := len(tree.Root().Resources()); got != n {
				t.Errorf("the tree holds %d resources", got)
			}
			runtime.GC()
			runtime.ReadMemStats(&after)
			grown := int64(after.HeapAlloc) - int64(before.HeapAlloc)
			t.Logf("heap grown by %d MiB", grown>>20)
			if grown > 300<<20 {
				t.Errorf("the heap grew by %d MiB for %d resources", grown>>20, n)
			}
			runtime.KeepAlive(g)
		})
	}
}
