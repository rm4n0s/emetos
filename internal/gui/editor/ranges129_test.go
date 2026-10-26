package editor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/folding"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// foldingServer answers the folding ranges the test gives: nil is "no answer".
type foldingServer struct {
	mu     sync.Mutex
	ranges []langfeatures.FoldingRange
	calls  int
}

func (p *foldingServer) ProvideFoldingRanges(context.Context, langfeatures.Document) ([]langfeatures.FoldingRange, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.ranges, nil
}

func (p *foldingServer) asked() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

const foldText = "package main\n\nimport (\n\t\"fmt\"\n)\n\nfunc main() {\n\tif true {\n\t\tfmt.Println()\n\t}\n}\n"

func foldedRegions(vm *ViewModel) [][2]int {
	var out [][2]int
	r := vm.FoldingModel().Regions()
	for i := 0; i < r.Len(); i++ {
		out = append(out, [2]int{r.Start(i), r.End(i)})
	}
	return out
}

// startServerFolding starts the folding of the rig the way the workbench does: the provider of the language servers with the
// indentation provider as the fallback.
func startServerFolding(r *lang129Rig, strategy string) {
	r.host()
	r.vm.StartFolding(langfeatures.FoldingProvider{
		Registry: r.reg,
		Fallback: folding.IndentProvider{},
		Strategy: func() string { return strategy },
	}, time.Millisecond)
	r.settle()
}

func TestFoldingRangesOfTheServerReplaceTheIndentation(t *testing.T) {
	server := []langfeatures.FoldingRange{
		{StartLine: 0, EndLine: 1, Kind: folding.KindComment},
		{StartLine: 2, EndLine: 4, Kind: folding.KindImports},
		{StartLine: 6, EndLine: 10},
		{StartLine: 7, EndLine: 9, CollapsedText: "{ ... }"},
	}
	indent := [][2]int{{2, 3}, {6, 9}, {7, 8}}
	tests := []struct {
		name     string
		ranges   []langfeatures.FoldingRange
		register bool
		strategy string
		want     [][2]int
	}{
		{"the ranges of the server", server, true, "auto", [][2]int{{0, 1}, {2, 4}, {6, 10}, {7, 9}}},
		{"no provider: the indentation", nil, false, "auto", indent},
		{"a provider that answers nothing: the indentation", nil, true, "auto", indent},
		{"the strategy indentation ignores the server", server, true, "indentation", indent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newLang129Rig(t, foldText, nil)
			p := &foldingServer{ranges: tt.ranges}
			if tt.register {
				r.reg.RegisterFoldingRanges("test", r.selector(), p)
			}
			startServerFolding(r, tt.strategy)
			if got := foldedRegions(r.vm); !equalSpans(got, tt.want) {
				t.Errorf("regions = %v, want %v", got, tt.want)
			}
		})
	}
}

func equalSpans(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFoldedRegionShowsTheCollapsedTextOfTheServer(t *testing.T) {
	r := newLang129Rig(t, foldText, nil)
	r.reg.RegisterFoldingRanges("test", r.selector(), &foldingServer{ranges: []langfeatures.FoldingRange{
		{StartLine: 6, EndLine: 10}, {StartLine: 7, EndLine: 9, CollapsedText: "{ ... }"},
	}})
	startServerFolding(r, "auto")
	region, ok := r.vm.FoldingModel().RegionAtLine(7)
	if !ok || region.Start() != 7 {
		t.Fatalf("region at line 7 = %v, %v", region, ok)
	}
	if got := r.vm.foldPlaceholderFor(7); got != foldPlaceholderText {
		t.Errorf("an unfolded region shows %q", got)
	}
	r.vm.FoldingModel().ToggleCollapseState([]folding.Region{region})
	if got := r.vm.foldPlaceholderFor(7); got != "{ ... }" {
		t.Errorf("the folded region shows %q, want the text of the server", got)
	}
	if got := r.vm.foldPlaceholderFor(6); got != foldPlaceholderText {
		t.Errorf("a region the server gave no text for shows %q", got)
	}
}

func TestImportsAreFoldedOnceByDefault(t *testing.T) {
	r := newLang129Rig(t, foldText, func(o *Options) { o.FoldingImportsByDefault = true })
	p := &foldingServer{ranges: []langfeatures.FoldingRange{
		{StartLine: 2, EndLine: 4, Kind: folding.KindImports}, {StartLine: 6, EndLine: 10},
	}}
	r.reg.RegisterFoldingRanges("test", r.selector(), p)
	startServerFolding(r, "auto")
	if !r.vm.FoldedLine(2) || r.vm.FoldedLine(6) {
		t.Fatalf("imports folded %v, func folded %v", r.vm.FoldedLine(2), r.vm.FoldedLine(6))
	}
	// the user unfolds them; the next computation does not fold them again
	region, _ := r.vm.FoldingModel().RegionAtLine(2)
	r.vm.FoldingModel().ToggleCollapseState([]folding.Region{region})
	r.vm.RefreshFolding()
	r.settle()
	if p.asked() < 2 {
		t.Fatalf("the server was asked %d times", p.asked())
	}
	if r.vm.FoldedLine(2) {
		t.Error("the imports were folded again")
	}
}

func TestRefreshFoldingAsksTheServerAgain(t *testing.T) {
	r := newLang129Rig(t, foldText, nil)
	p := &foldingServer{ranges: nil}
	r.reg.RegisterFoldingRanges("test", r.selector(), p)
	startServerFolding(r, "auto")
	if got := foldedRegions(r.vm); !equalSpans(got, [][2]int{{2, 3}, {6, 9}, {7, 8}}) {
		t.Fatalf("regions = %v", got)
	}
	// the server starts to answer: it asks for a refresh
	p.mu.Lock()
	p.ranges = []langfeatures.FoldingRange{{StartLine: 6, EndLine: 10}}
	p.mu.Unlock()
	r.vm.RefreshFolding()
	r.settle()
	if got := foldedRegions(r.vm); !equalSpans(got, [][2]int{{6, 10}}) {
		t.Errorf("regions after the refresh = %v", got)
	}
}

// selectionServer answers the chains of ranges it is given for the position, after its gate opens.
type selectionServer struct {
	chain []buffer.Range
	gate  chan struct{}
	err   error
}

func (p *selectionServer) ProvideSelectionRanges(ctx context.Context, _ langfeatures.Document, positions []langfeatures.Position) ([][]langfeatures.Range, error) {
	if p.gate != nil {
		select {
		case <-p.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	out := make([][]langfeatures.Range, len(positions))
	for i := range out {
		out[i] = p.chain
	}
	return out, nil
}

const selectionText = "func f(a int) {\n\tg(a, 2)\n}\n"

func expandAll(t *testing.T, r *lang129Rig, times int) []buffer.Range {
	t.Helper()
	var got []buffer.Range
	for range times {
		if err := r.vm.RunCommand("editor.action.smartSelect.expand", nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		got = append(got, r.vm.Cursors().Selections()[0].Range())
	}
	return got
}

func hasRange(rs []buffer.Range, want buffer.Range) bool {
	for _, r := range rs {
		if r == want {
			return true
		}
	}
	return false
}

func TestExpandSelectionUsesTheRangesOfTheServer(t *testing.T) {
	// `(a, 2` is not a range the editor knows: when it is among the steps, it came from the server
	odd := rg(1, 2, 1, 8)
	chain := []buffer.Range{rg(1, 3, 1, 4), odd, rg(0, 0, 2, 1)}
	tests := []struct {
		name     string
		server   *selectionServer
		wantOdd  bool
		wantLast buffer.Range
	}{
		{"the server answers", &selectionServer{chain: chain}, true, rg(0, 0, 3, 0)},
		{"the server fails: the ranges of the editor", &selectionServer{err: errors.New("ServerDown", "down")}, false, rg(0, 0, 3, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newLang129Rig(t, selectionText, nil)
			r.reg.RegisterSelectionRanges("test", r.selector(), tt.server)
			r.host()
			cursorAt(r.vm, 1, 3)
			got := expandAll(t, r, 8)
			if hasRange(got, odd) != tt.wantOdd {
				t.Errorf("steps %v: has the range of the server = %v, want %v", got, hasRange(got, odd), tt.wantOdd)
			}
			if last := got[len(got)-1]; last != tt.wantLast {
				t.Errorf("the last step is %v, want %v", last, tt.wantLast)
			}
			// Shrink goes back through the steps
			if err := r.vm.RunCommand("editor.action.smartSelect.shrink", nil); err != nil {
				t.Fatal(err)
			}
			if now := r.vm.Cursors().Selections()[0].Range(); now == tt.wantLast {
				t.Errorf("Shrink Selection did not shrink: %v", now)
			}
		})
	}
}

func TestAnAnswerForMovedCursorsIsDropped(t *testing.T) {
	r := newLang129Rig(t, selectionText, nil)
	server := &selectionServer{chain: []buffer.Range{rg(1, 3, 1, 4), rg(1, 2, 1, 8)}, gate: make(chan struct{})}
	r.reg.RegisterSelectionRanges("test", r.selector(), server)
	r.host()
	cursorAt(r.vm, 1, 3)
	if err := r.vm.RunCommand("editor.action.smartSelect.expand", nil); err != nil {
		t.Fatal(err)
	}
	// nothing happens while the server is asked; the cursor moves, and the answer comes late
	if sel := r.vm.Cursors().Selections()[0]; !sel.IsEmpty() {
		t.Fatalf("the selection changed before the answer: %v", sel)
	}
	cursorAt(r.vm, 0, 2)
	close(server.gate)
	r.settle()
	if sel := r.vm.Cursors().Selections()[0]; !sel.IsEmpty() || sel.Active.Line != 0 {
		t.Errorf("the late answer changed the selection: %v", sel)
	}
}

func TestExpandSelectionWithoutAProviderStartsAtOnce(t *testing.T) {
	r := newLang129Rig(t, selectionText, nil)
	r.host()
	cursorAt(r.vm, 1, 3)
	if err := r.vm.RunCommand("editor.action.smartSelect.expand", nil); err != nil {
		t.Fatal(err)
	}
	if sel := r.vm.Cursors().Selections()[0]; sel.IsEmpty() {
		t.Error("nothing was selected")
	}
}
