package httpclient

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/checks"
)

// bulkRig is a rig with a collection "bulk" of n requests.
func bulkRig(t *testing.T, n int) *rig {
	t.Helper()
	return newRig(t, func(folder string) {
		root := filepath.Join(folder, ".emetos", "http", "collections", "bulk")
		writeFile(t, filepath.Join(root, "collection.jsonc"), `{"emetosHttp": 1, "name": "Bulk"}`)
		for i := range n {
			writeFile(t, filepath.Join(root, fmt.Sprintf("r%03d.request.jsonc", i)), fmt.Sprintf(
				`{"emetosHttp": 1, "name": "Item %d", "method": "GET", "url": "{{baseUrl}}/item/%d"}`, i, i))
		}
	})
}

func TestFiveHundredStreamedResultsStayInTheFrameBudget(t *testing.T) {
	// the UI thread's processor time of each frame (the test goroutine is locked to its thread), as in the other budget tests
	factor := time.Duration(1)
	if raceEnabled {
		factor = 10
	}
	const n = 500
	for _, perFrame := range []int{1, 55} {
		t.Run(fmt.Sprintf("%d a frame", perFrame), func(t *testing.T) {
			runtime.LockOSThread()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			// the longest frame of one run; the first frame of a run lays the whole view out for the first time (cold caches, first
			// paint of every widget kind), which is not what is under test, so it is pumped but not counted
			run := func() time.Duration {
				r := bulkRig(t, n)
				tab := r.openRunner("bulk")
				ts := r.showRunner(uitest.Themes()[0], tab, 980, 700)
				ts.Pump(16 * time.Millisecond)
				tab.running = true
				var frames []time.Duration
				for sent := 0; sent < n; {
					for range perFrame {
						if sent == n {
							break
						}
						res := runResult{Iteration: 1, Item: sent, Name: fmt.Sprintf("Item %d", sent), Method: "GET", Status: 200, StatusText: "OK",
							Duration: 3 * time.Millisecond, Size: 40, Checks: []checks.Result{{Passed: true}, {Passed: true}}}
						r.api.Post(func() { tab.addResult(res) })
						sent++
					}
					s := threadCPU(t)
					r.api.Pump()
					ts.Pump(16 * time.Millisecond)
					frames = append(frames, threadCPU(t)-s)
				}
				if len(tab.results) != n {
					t.Fatalf("%d results", len(tab.results))
				}
				return slices.Max(frames[1:])
			}
			// the processor time of a thread still grows when the machine is busy (shared caches and cores), so a run that is over
			// the limit is measured again, up to three times, and the best one counts: a real slowdown is over the limit every time
			limit := 16 * time.Millisecond * factor
			var best time.Duration
			for attempt := range 3 {
				worst := run()
				t.Logf("%d at a time, attempt %d: longest frame %v", perFrame, attempt+1, worst)
				if attempt == 0 || worst < best {
					best = worst
				}
				if best <= limit {
					break
				}
			}
			if best > limit {
				t.Errorf("a frame took %v of the UI thread while %d results came in %d at a time", best, n, perFrame)
			}
		})
	}
}
