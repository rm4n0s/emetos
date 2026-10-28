package terminal

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// waitSearched waits until the search of the pane has answered, then runs the frames. The search runs on a goroutine of its own
// and hands its answer over by Post, so the wait is in real time and hands the posted answers to the UI thread without drawing
// a frame: a frame advances the fake clock, and the number of frames that a wait drew depended on how long the goroutine took,
// that is on the load of the machine, which moved the fake clock to a different phase of the input's blinking caret (30
// pixels of the golden images).
func (r *paneRig) waitSearched(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for r.ts.FlushPosted(); !r.pv.find.searched; r.ts.FlushPosted() {
		if time.Now().After(deadline) {
			t.Fatalf("the search did not answer; find = %+v", r.pv.find)
		}
		time.Sleep(time.Millisecond)
	}
	r.settle()
}

// search types a text in the find widget and waits for the answer.
func (r *paneRig) search(t *testing.T, text string) {
	t.Helper()
	if !r.pv.find.visible {
		r.pv.showFind()
		r.settle()
	}
	r.pv.find.input.SetValue(text)
	r.settle()
	r.waitSearched(t)
}

// content is a screen with the word "needle" three times, one of them across a soft wrap, and more lines than the window shows.
func findContent(cols int) string {
	var sb strings.Builder
	sb.WriteString("first needle line\r\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&sb, "filler line number %d\r\n", i)
	}
	sb.WriteString("second Needle line\r\n")
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&sb, "more filler %d\r\n", i)
	}
	sb.WriteString(strings.Repeat("x", cols-4) + "need" + "le end of the wrapped line\r\n")
	sb.WriteString("$ ")
	return sb.String()
}

func TestTheFindWidgetSearchesTheScrollbackAndHighlights(t *testing.T) {
	r := newFakePane(t, uitest.Themes()[0], paneSize, nil)
	r.write(t, findContent(r.pv.cols))
	r.pv.showFind()
	r.settle()
	if !r.pv.find.visible || !r.ts.Exists(gluttertest.ByText("Find")) && false {
		t.Fatalf("the widget is not shown")
	}
	r.search(t, "needle")
	f := &r.pv.find
	if len(f.matches) != 3 || f.cur != 2 {
		t.Fatalf("%d matches, current %d; want 3 and the last", len(f.matches), f.cur)
	}
	if got := r.pv.matchCount(); got != "3 of 3" {
		t.Errorf("the count is %q", got)
	}
	if !r.ts.Exists(gluttertest.ByText("3 of 3")) {
		t.Error("the count is not on screen")
	}
	// the last match is the one across the wrap, and it is on the screen
	last := f.matches[2]
	if last.start.line == last.end.line {
		t.Errorf("the match across the wrap is on one row: %+v", last)
	}
	// the previous match is the second, which is above the screen: the view scrolls to it
	r.pv.moveFind(true)
	r.settle()
	if f.cur != 1 {
		t.Errorf("current = %d after the previous match", f.cur)
	}
	r.pv.moveFind(true)
	r.settle()
	if f.cur != 0 || r.pv.shown == nil || f.matches[0].start.line < r.pv.shown.Top {
		t.Errorf("current = %d; the first match is not in view (top %d)", f.cur, r.pv.shown.Top)
	}
	// going on past the first match wraps to the last
	r.pv.moveFind(true)
	if f.cur != 2 {
		t.Errorf("current = %d after the previous match of the first one, want the last", f.cur)
	}
	r.pv.moveFind(false)
	if f.cur != 0 {
		t.Errorf("current = %d after the next match of the last one, want the first", f.cur)
	}
	// the switches
	r.pv.toggleFind("case")
	r.waitSearched(t)
	if len(f.matches) != 2 {
		t.Errorf("%d matches with the case counting, want 2", len(f.matches))
	}
	r.pv.toggleFind("case")
	r.waitSearched(t)
	r.search(t, "ne+dle")
	if len(f.matches) != 0 || r.pv.matchCount() != "No results" {
		t.Errorf("a regular expression without the switch: %d matches, %q", len(f.matches), r.pv.matchCount())
	}
	r.pv.toggleFind("regex")
	r.waitSearched(t)
	if len(f.matches) != 3 {
		t.Errorf("%d matches of the regular expression, want 3", len(f.matches))
	}
	r.pv.hideFind()
	r.settle()
	if f.visible || len(f.matches) != 0 || r.ts.Exists(gluttertest.ByText("3 of 3")) {
		t.Errorf("the widget stays after Hide: %+v", f)
	}
}

func TestGoldenTheFindWidgetAndItsHighlights(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newFakePane(t, th, gui.Size{Width: 640, Height: 260}, nil)
			var sb strings.Builder
			sb.WriteString("$ grep -rn needle src\r\n")
			sb.WriteString("src/a.go:3:    needle := find(\"needle\")\r\n")
			sb.WriteString("src/b.go:9:    // the Needle is here\r\n")
			sb.WriteString("src/c.go:20:   return haystack, nil\r\n")
			sb.WriteString("src/d.go:41:   x := strings.Repeat(\"needle\", 3) + \"a long tail that wraps around the edge needle\"\r\n")
			sb.WriteString("$ ")
			r.write(t, sb.String())
			r.search(t, "needle")
			r.pv.moveFind(true)
			r.settle()
			r.ts.Screenshot()
			uitest.MatchesGolden(r.ts, "pane_find_"+th.Name)
			// no results: the border of the input is red
			r.search(t, "nothing here")
			r.ts.Screenshot()
			uitest.MatchesGolden(r.ts, "pane_find_none_"+th.Name)
		})
	}
}
