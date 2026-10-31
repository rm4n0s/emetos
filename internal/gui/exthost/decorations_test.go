package exthost

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/decorationstest"
	"github.com/rm4n0s/emetos/internal/core/decorations"
)

// decorationsHarness makes the harness of the conformance suite over a tab of the host.
func decorationsHarness(t *testing.T) *decorationstest.Harness {
	t.Helper()
	_, tb, api := grabAPI(t, "emetos.decotest", nil)
	var told []decorations.Change
	tb.decos.OnChange(func(c decorations.Change) { told = append(told, c) })
	return &decorationstest.Harness{
		API: api,
		Ask: func(uri string) (decorationstest.Seen, bool) {
			d, ok := tb.decos.Get(uri, false)
			if !ok {
				return decorationstest.Seen{}, false
			}
			seen := decorationstest.Seen{Badge: d.Badge, Icon: d.Icon, Tooltip: d.Tooltip}
			if len(d.Colors) > 0 {
				seen.Color = d.Colors[0]
			}
			return seen, true
		},
		Told: func(uri string) bool {
			hit := false
			for _, c := range told {
				hit = hit || (!c.All() && c.Affects(uri))
			}
			told = nil
			return hit
		},
		Pump:       tb.pump,
		Deactivate: func() { tb.disabled = []string{"emetos.decotest"}; tb.scope.Refresh() },
		Failed:     func() bool { return tb.scope.State("emetos.decotest") == StateFailed },
	}
}

// TestDecorationsConformance runs the suite that the fake of the tests of the extensions passes too.
func TestDecorationsConformance(t *testing.T) { decorationstest.Run(t, decorationsHarness) }

type decoFunc func(string) (extension.FileDecoration, bool)

func (f decoFunc) Decoration(uri string) (extension.FileDecoration, bool) { return f(uri) }

func TestDecorationsOfExtensionsAreMergedByPriorityAndBubble(t *testing.T) {
	_, tb, api := grabAPI(t, "emetos.decotest", nil)
	reg, err := api.Decorations().Register(decoFunc(func(uri string) (extension.FileDecoration, bool) {
		switch {
		case strings.HasSuffix(uri, "/low.go"):
			return extension.FileDecoration{Badge: "M", Color: "low", Priority: 2, Propagate: true}, true
		case strings.HasSuffix(uri, "/high.go"):
			return extension.FileDecoration{Badge: "!", Color: "high", Priority: 4, Propagate: true}, true
		case strings.HasSuffix(uri, "/gone.go"):
			return extension.FileDecoration{Badge: "D", Color: "del"}, true
		}
		return extension.FileDecoration{}, false
	}))
	if err != nil {
		t.Fatal(err)
	}
	reg.Fire("file:///r/d/low.go", "file:///r/d/high.go", "file:///r/d/gone.go")
	tb.pump()
	folder, ok := tb.decos.Get("file:///r/d", true)
	if !ok || !folder.Bubble || len(folder.Colors) != 2 || folder.Colors[0] != "high" {
		t.Errorf("folder = %+v, %v; want the dot with the conflict color first", folder, ok)
	}
	gone, _ := tb.decos.Get("file:///r/d/gone.go", false)
	if gone.Badge != "D" {
		t.Errorf("gone.go = %+v", gone)
	}
	if _, ok := tb.decos.Get("file:///r/e", true); ok {
		t.Error("a folder without decorated files is decorated")
	}
}

func TestADecorationsServiceIsNeededAndAProviderThatPanicsIsContained(t *testing.T) {
	_, _, api := grabAPI(t, "emetos.decotest", func(r *rig) { r.noDecorations = map[string]bool{"one": true} })
	_, err := api.Decorations().Register(decoFunc(func(string) (extension.FileDecoration, bool) { return extension.FileDecoration{}, false }))
	mustRoute(t, err, "decorationsAPI.Register.ExtensionDecorationsUnavailable")
	_, err = api.Decorations().Register(nil)
	mustRoute(t, err, "decorationsAPI.Register.ExtensionDecorationsInvalid")
}

func TestAProviderThatPanicsFailsTheExtensionWithItsTag(t *testing.T) {
	r, tb, api := grabAPI(t, "emetos.decotest", nil)
	if _, err := api.Decorations().Register(decoFunc(func(string) (extension.FileDecoration, bool) { panic("boom") })); err != nil {
		t.Fatal(err)
	}
	if _, ok := tb.decos.Get("file:///r/a", false); ok {
		t.Error("a decoration from a provider that panicked")
	}
	tb.pump()
	st := tb.scope.State("emetos.decotest")
	if st != StateFailed {
		t.Fatalf("state = %v, want failed", st)
	}
	mustRoute(t, tb.scope.Failure("emetos.decotest").Err, "ExtensionDecorationsPanicked")
	_ = r
}

func TestAFireOfManyFilesIsAskedForInTurnsOfTheUIThread(t *testing.T) {
	_, tb, api := grabAPI(t, "emetos.decotest", nil)
	tb.deferred = true // Post queues until the test runs it, as the UI thread does between two turns
	var mu sync.Mutex
	calls := 0
	reg, err := api.Decorations().Register(decoFunc(func(uri string) (extension.FileDecoration, bool) {
		mu.Lock()
		calls++
		mu.Unlock()
		return extension.FileDecoration{Badge: "M"}, true
	}))
	if err != nil {
		t.Fatal(err)
	}
	var uris []string
	for i := 0; i < 5000; i++ {
		uris = append(uris, fmt.Sprintf("file:///r/f%05d.go", i))
	}
	reg.Fire(uris...)
	reg.Fire("file:///r/other.go") // a second call waits behind the first, in order
	runOne := func() {
		tb.evMu.Lock()
		f := tb.queued[0]
		tb.queued = tb.queued[1:]
		tb.evMu.Unlock()
		f()
	}
	want := []int{2000, 4000, 5000, 5001}
	for i, w := range want {
		if calls != 0 && i == 0 {
			t.Fatalf("the provider was asked before the UI thread's turn: %d", calls)
		}
		runOne()
		if calls != w {
			t.Errorf("after turn %d the provider was asked %d times, want %d", i+1, calls, w)
		}
	}
	tb.evMu.Lock()
	left := len(tb.queued)
	tb.evMu.Unlock()
	if left != 0 {
		t.Errorf("%d turns left", left)
	}
	// the end of the activation drops what waits
	reg.Fire(uris...)
	tb.disabled = []string{"emetos.decotest"}
	tb.scope.Refresh()
	tb.pump()
	if calls > 5001+2000 {
		t.Errorf("the provider was asked %d times after the activation ended", calls)
	}
}
