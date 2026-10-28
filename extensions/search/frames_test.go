package search

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// TestTheUIThreadStaysUnderAFrameWhileAHugeSearchStreams holds the row "Search" of SPEC §14: while the results of a search of 20,000
// files stream in (the engine hands over a file at a time, as fast as it finds them), no turn of the UI thread (what the
// extension does when it takes the batch, and the frame that shows the view) takes more than 16 ms.
func TestTheUIThreadStaysUnderAFrameWhileAHugeSearchStreams(t *testing.T) {
	if testing.Short() {
		t.Skip("a long run")
	}
	const files, perFile = 2000, 10 // 20,000 matches: search.maxResults
	budget := 16 * time.Millisecond
	if raceEnabled {
		budget *= 8
	}
	r := newRig(t, project)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 340, Height: 400}, widgetOf(r))
	r.ext.searchBox.SetValue("needle")
	r.ext.status = searchStatus{running: true}
	r.ext.gen++
	gen := r.ext.gen

	done := make(chan struct{})
	go func() { // the engine: a file at a time, as fast as it can
		defer close(done)
		for i := range files {
			f := extension.TextSearchFile{Path: fmt.Sprintf("d%03d/src/f%05d.txt", i/10, i), URI: "file:///work/f" + strconv.Itoa(i)}
			for j := range perFile {
				f.Matches = append(f.Matches, extension.TextSearchMatch{
					Range:   extension.Range{Start: extension.Position{Line: j, Col: 4}, End: extension.Position{Line: j, Col: 10}},
					Preview: "the needle is here", PreviewStart: 4, PreviewEnd: 10, Text: "needle",
				})
			}
			r.ext.push(gen, f)
		}
	}()

	var worst time.Duration
	turns := 0
	measure := func(what string, f func()) {
		start := time.Now()
		f()
		if d := time.Since(start); d > worst {
			worst = d
			if d > budget {
				t.Errorf("a turn of the UI thread (%s) took %v, the budget is %v", what, d, budget)
			}
		}
		turns++
	}
	for {
		measure("the extension's turn", func() { r.api.Pump() })
		measure("the frame", func() { ts.Pump(16 * time.Millisecond) })
		select {
		case <-done:
			measure("the end", func() { r.ext.finish(gen, extension.TextSearchSummary{}, nil) })
			measure("the last frame", func() { ts.Pump(16 * time.Millisecond) })
			goto end
		default:
		}
	}
end:
	for range 40 { // the batches that are still on their way
		r.api.Pump()
		time.Sleep(time.Millisecond)
	}
	matches, nfiles := r.ext.res.counts()
	if matches != files*perFile || nfiles != files {
		t.Errorf("the results have %d matches in %d files, want %d in %d", matches, nfiles, files*perFile, files)
	}
	t.Logf("%d turns of the UI thread, the longest %v", turns, worst)
}
