package folding

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
)

func TestActions(t *testing.T) {
	text := []string{
		"class A {", "  /**", "   * doc", "   */", "  void foo() {", "    if (x) {", "      a();", "    }", "  }", "  //#region", "  int b;", "  //#endregion", "}",
	}
	markers := &Markers{Start: ecma(t, `^\s*\/\/#region`, false), End: ecma(t, `^\s*\/\/#endregion`, false)}
	setup := func(t *testing.T) *harness {
		h := newHarness(t, text...)
		h.update(h.compute(false, markers), nil)
		return h
	}
	folded := func(h *harness) string {
		var out []string
		for i := 0; i < h.model.Regions().Len(); i++ {
			if h.model.Regions().Collapsed(i) {
				out = append(out, itoa(h.model.Regions().Start(i)+1)+"-"+itoa(h.model.Regions().End(i)+1))
			}
		}
		return strings.Join(out, " ")
	}
	tests := []struct {
		name string
		run  func(h *harness)
		want string
	}{
		{"fold", func(h *harness) { Fold(h.model, []int{6}, 0, false, false) }, "6-7"},
		{"fold twice goes up", func(h *harness) { Fold(h.model, []int{6}, 0, false, false); Fold(h.model, []int{6}, 0, false, false) }, "5-8 6-7"},
		{"fold levels down", func(h *harness) { Fold(h.model, []int{4}, 2, true, false) }, "5-8 6-7"},
		{"fold levels up", func(h *harness) { Fold(h.model, []int{6}, 2, true, true) }, "5-8 6-7"},
		{"unfold", func(h *harness) { FoldAll(h.model); Unfold(h.model, []int{4}, 0, false) }, "1-12 2-4 6-7 10-12"},
		{"unfold levels up", func(h *harness) { FoldAll(h.model); Unfold(h.model, []int{6}, 3, true) }, "2-4 10-12"},
		{"toggle", func(h *harness) { Toggle(h.model, []int{1}); Toggle(h.model, []int{1}); Toggle(h.model, []int{4}) }, "5-8"},
		{"toggle recursively", func(h *harness) { ToggleRecursively(h.model, []int{4}) }, "5-8 6-7"},
		{"fold recursively", func(h *harness) { FoldRecursively(h.model, []int{4}) }, "5-8 6-7"},
		{"unfold recursively", func(h *harness) { FoldAll(h.model); UnfoldRecursively(h.model, []int{5}) }, "1-12 2-4 5-8 10-12"},
		{"fold all", func(h *harness) { FoldAll(h.model) }, "1-12 2-4 5-8 6-7 10-12"},
		{"unfold all", func(h *harness) { FoldAll(h.model); UnfoldAll(h.model) }, ""},
		{"fold level 1", func(h *harness) { FoldLevel(h.model, 1, nil) }, "1-12"},
		{"fold level 2 except the cursor's", func(h *harness) { FoldLevel(h.model, 2, []int{6}) }, "2-4 10-12"},
		{"fold all except", func(h *harness) { FoldAllExcept(h.model, []int{6}) }, "2-4 10-12"},
		{"unfold all except", func(h *harness) { FoldAll(h.model); UnfoldAllExcept(h.model, []int{6}) }, "1-12 5-8 6-7"},
		{"fold block comments", func(h *harness) { FoldAllBlockComments(h.model, "/*") }, "2-4"},
		{"fold marker regions", func(h *harness) { FoldAllMarkerRegions(h.model, markers.Start) }, "10-12"},
		{"unfold marker regions", func(h *harness) { FoldAll(h.model); UnfoldAllMarkerRegions(h.model, markers.Start) }, "1-12 2-4 5-8 6-7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := setup(t)
			tc.run(h)
			if got := folded(h); got != tc.want {
				t.Errorf("folded = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("block comments with no token do nothing", func(t *testing.T) {
		h := setup(t)
		FoldAllBlockComments(h.model, "")
		if folded(h) != "" {
			t.Errorf("folded = %q", folded(h))
		}
		FoldAllMarkerRegions(h.model, nil)
		if folded(h) != "" {
			t.Errorf("folded = %q", folded(h))
		}
	})
	t.Run("by kind", func(t *testing.T) {
		h := newHarness(t, "// c", "// c", "code", "  x", "  y", "//#region", "a", "//#endregion")
		regions, _ := FromRanges([]Range{{Start: 0, End: 1, Type: KindComment}, {Start: 3, End: 4, Type: ""}, {Start: 5, End: 7, Type: KindRegion}})
		h.update(regions, nil)
		FoldAllBlockComments(h.model, "/*")
		h.assertFolded([]expected{r(1, 2)})
		FoldAllMarkerRegions(h.model, nil)
		h.assertFolded([]expected{r(1, 2), r(6, 8)})
		UnfoldAllMarkerRegions(h.model, nil)
		h.assertFolded([]expected{r(1, 2)})
	})
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func TestModelEvents(t *testing.T) {
	h := newHarness(t, javaDoc()...)
	var updates, toggles int
	unsubscribe := h.model.OnChange(func(ev ChangeEvent) {
		if ev.Collapsed == nil {
			updates++
		} else {
			toggles += len(ev.Collapsed)
		}
	})
	h.update(h.compute(false, nil), nil)
	h.toggle(1, 5)
	h.toggle()
	if updates != 1 || toggles != 2 {
		t.Errorf("updates = %d, toggles = %d", updates, toggles)
	}
	unsubscribe()
	h.toggle(1)
	if toggles != 2 {
		t.Error("a function that was unsubscribed was called")
	}
	h.model.Dispose()
	if len(h.doc.DecorationsOf(testOwner)) != 0 {
		t.Error("the decorations are still there")
	}
}

func TestRegionsFollowEdits(t *testing.T) {
	h := newHarness(t, javaDoc()...)
	h.update(h.compute(false, nil), nil)
	h.toggle(4)
	// Lines added above move the regions; the folded state follows them.
	h.edit(buffer.Range{}, "// a\n// b\n")
	h.update(h.compute(false, nil), nil)
	h.assertRanges([]expected{r(3, 5), r(6, 9, true), r(7, 8)})
}

func TestUnfoldsTheRegionACursorIsIn(t *testing.T) {
	h := newHarness(t, javaDoc()...)
	h.update(h.compute(false, nil), nil)
	h.toggle(5)
	h.edit(buffer.Range{Start: buffer.Position{Line: 5, Col: 4}, End: buffer.Position{Line: 5, Col: 4}}, " x")
	h.update(h.compute(false, nil), LinesOf([]int{5}))
	h.assertRanges([]expected{r(1, 3), r(4, 7), r(5, 6)})
}

func TestMemento(t *testing.T) {
	h := newHarness(t, javaDoc()...)
	h.update(h.compute(false, nil), nil)
	if m := h.model.Memento(); m != nil {
		t.Errorf("memento = %v, want none", m)
	}
	h.toggle(1, 5)
	memento := h.model.Memento()
	if len(memento) != 2 || !memento[0].Collapsed || memento[0].Start != 0 || memento[1].Start != 4 {
		t.Fatalf("memento = %+v", memento)
	}

	// The same text in a new model: the folds come back.
	h2 := newHarness(t, javaDoc()...)
	h2.update(h2.compute(false, nil), nil)
	if err := h2.model.ApplyMemento(memento); err != nil {
		t.Fatal(err)
	}
	h2.assertRanges([]expected{r(1, 3, true), r(4, 7), r(5, 6, true)})

	// Text that changed under a region: its checksum differs and it stays open.
	changed := javaDoc()
	changed[1] = " * Another comment"
	h3 := newHarness(t, changed...)
	h3.update(h3.compute(false, nil), nil)
	if err := h3.model.ApplyMemento(memento); err != nil {
		t.Fatal(err)
	}
	h3.assertRanges([]expected{r(1, 3), r(4, 7), r(5, 6, true)})

	// Entries that are not regions of the text are ignored.
	h4 := newHarness(t, javaDoc()...)
	h4.update(h4.compute(false, nil), nil)
	if err := h4.model.ApplyMemento(Memento{{Start: 3, End: 3}, {Start: -1, End: 2}, {Start: 0, End: 99}}); err != nil {
		t.Fatal(err)
	}
	h4.assertRanges([]expected{r(1, 3), r(4, 7), r(5, 6)})
}

func TestComputeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := IndentProvider{}.Compute(ctx, lines{"a", "  b"}, 4)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("computeRanges.FoldingCancelled") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestUpdater(t *testing.T) {
	doc := document.New(document.UntitledURI(1), "a\n  b\nc\n  d", document.NewOptions())
	results := make(chan *Regions, 8)
	stop := Attach(doc, IndentProvider{}, UpdaterOptions{Delay: 20 * time.Millisecond}, func(r *Regions) { results <- r })
	defer stop()
	wait := func() *Regions {
		t.Helper()
		select {
		case r := <-results:
			return r
		case <-time.After(5 * time.Second):
			t.Fatal("no regions")
			return nil
		}
	}
	first := wait()
	if first.String() != "[ -] 0/1, [ -] 2/3" {
		t.Errorf("first = %s", first)
	}
	// Quick edits make one computation, for the last text.
	for _, text := range []string{"x", "y", "z"} {
		if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: text + "\n  "}}, document.EditOptions{Kind: document.KindOther}); err != nil {
			t.Fatal(err)
		}
	}
	second := wait()
	if second.String() != "[ -] 0/4, [ -] 5/6" {
		t.Errorf("second = %s", second)
	}
	select {
	case r := <-results:
		t.Errorf("an extra result: %s", r)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestUpdaterStopsAndSkipsLargeFiles(t *testing.T) {
	doc := document.New(document.UntitledURI(1), "a\n  b", document.NewOptions())
	results := make(chan *Regions, 4)
	stop := Attach(doc, IndentProvider{}, UpdaterOptions{Delay: time.Millisecond}, func(r *Regions) { results <- r })
	<-results
	stop()
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "q\n  "}}, document.EditOptions{Kind: document.KindOther}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-results:
		t.Error("regions after stop")
	case <-time.After(100 * time.Millisecond):
	}

	large := document.FromRead(document.FileURI("/big.txt"), fileio.ReadResult{Text: "a\n  b\nc\n  d", Encoding: fileio.UTF8, LineEnding: fileio.LF, LargeFile: true}, document.NewOptions())
	stopLarge := Attach(large, IndentProvider{}, UpdaterOptions{Delay: time.Millisecond}, func(r *Regions) { results <- r })
	defer stopLarge()
	select {
	case <-results:
		t.Error("a large file was folded")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHiddenLines(t *testing.T) {
	doc := document.New(document.UntitledURI(1), "a\nb\nc\nd\ne\nf\ng\nh", document.NewOptions())
	m := NewModel(doc, testOwner, testDecorations)
	regions, err := FromRanges([]Range{{Start: 0, End: 4}, {Start: 1, End: 2}, {Start: 5, End: 7}})
	if err != nil {
		t.Fatal(err)
	}
	m.UpdatePost(regions)
	if got := m.HiddenLines(); len(got) != 0 {
		t.Fatalf("nothing is folded: %v", got)
	}
	r := m.Regions()
	m.ToggleCollapseState([]Region{r.Region(1), r.Region(2)})
	if got, want := m.HiddenLines(), []LineSpan{{First: 2, Last: 2}, {First: 6, Last: 7}}; !slices.Equal(got, want) {
		t.Errorf("hidden %v, want %v", got, want)
	}
	// the outer region swallows the inner one
	m.ToggleCollapseState([]Region{r.Region(0)})
	if got, want := m.HiddenLines(), []LineSpan{{First: 1, Last: 4}, {First: 6, Last: 7}}; !slices.Equal(got, want) {
		t.Errorf("hidden %v, want %v", got, want)
	}
	// the spans follow the edits made before the regions are computed again
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{}, End: buffer.Position{}}, Text: "new\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, want := m.HiddenLines(), []LineSpan{{First: 2, Last: 5}, {First: 7, Last: 8}}; !slices.Equal(got, want) {
		t.Errorf("hidden after an edit %v, want %v", got, want)
	}
	// a folded region that lost its lines hides nothing
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: 5, Col: 1}, End: buffer.Position{Line: 7, Col: 1}}}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := m.HiddenLines(); len(got) != 1 || got[0].First != 2 {
		t.Errorf("hidden after deleting a fold's lines %v", got)
	}
}
