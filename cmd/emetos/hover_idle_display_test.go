//go:build emetos_display

package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// readPNG decodes a screenshot.
func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return img
}

// differingPixels counts the pixels of the rectangle in which two images differ.
func differingPixels(a, b image.Image, r image.Rectangle) int {
	r = r.Intersect(a.Bounds()).Intersect(b.Bounds())
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ar, ag, ab, _ := a.At(x, y).RGBA()
			br, bg, bb, _ := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb {
				n++
			}
		}
	}
	return n
}

// TestHoverAnsweredAfterTheWindowWentIdleIsDrawnWithoutInput (step 24b.10, the unverified report of the dropped F# work): a
// language server answers a hover 2.5 s after the pointer rested on a word, while nothing else happens (no input, no animation:
// the cursor does not blink and no tool asks the window anything). The hover must be on the screen as soon as the answer
// arrives, not at the next frame that something else causes. The test looks at what the compositor shows (grim, which asks
// the editor nothing, unlike every dev query, whose request would itself wake the event loop and hide the fault) and asks the
// editor only afterwards.
func TestHoverAnsweredAfterTheWindowWentIdleIsDrawnWithoutInput(t *testing.T) {
	a := display.Start(t, display.Options{
		Args:    []string{".", "app.go"},
		Project: map[string]string{"app.go": goApp},
		Prepare: withFakeGopls(t, goplsOptions{
			HoverDelayMS: 2500,
			Settings:     map[string]any{"editor.cursorBlinking": "solid", "editor.hover.delay": 300},
		}),
	})
	a.Settle()
	// the server starts with the first document: a hover asked before it runs is answered with nothing. The Go item of the status
	// bar shows the check mark of Codicons (U+EAB2) when gopls is running.
	a.EventuallyFor(30*time.Second, "the status item of gopls to show the check mark", func() (bool, string) {
		for _, tx := range a.Texts() {
			if tx.Y > 770 && tx.Text == "\ueab2" {
				return true, ""
			}
		}
		return false, a.Dump()
	})
	// the virtual pointer may rest where an earlier test left it, on the word itself, and then moving to it sends no motion:
	// the pointer goes to the Explorer first
	a.MoveTo(150, 400)
	a.Settle()
	dir := proofDir(t)
	before := filepath.Join(dir, "hover-idle-before.png")
	a.Screenshot(before)
	// the pointer rests on the word main (line 5, columns 5 to 9); from here on the test does not touch the window
	a.MoveTo(editorColumnX(7), editorLine(5))
	// the request leaves after 300 ms and is answered 2.5 s later; wait until well after the answer
	time.Sleep(6 * time.Second)
	idle := filepath.Join(dir, "hover-idle-drawn.png")
	a.Screenshot(idle)
	// now the editor is asked, which wakes it, and the same picture is taken again
	hover := a.Editor().HoverVisible
	a.Settle()
	after := filepath.Join(dir, "hover-idle-asked.png")
	a.Screenshot(after)

	if !hover {
		t.Fatal("the editor has no hover after the server answered (the answer never arrived)")
	}
	// the hover box is above or below the word, near the text of the first lines
	region := image.Rect(300, 60, 1000, 400)
	imgBefore, imgIdle, imgAfter := readPNG(t, before), readPNG(t, idle), readPNG(t, after)
	drawnWhenIdle := differingPixels(imgBefore, imgIdle, region)
	drawnAfterAsking := differingPixels(imgBefore, imgAfter, region)
	t.Logf("pixels that differ from the picture before the hover: while idle %d, after the editor was asked %d", drawnWhenIdle, drawnAfterAsking)
	if drawnAfterAsking < 500 {
		t.Fatalf("the hover is not in the picture even after the editor was asked (%d pixels differ): the test does not see it", drawnAfterAsking)
	}
	if moved := differingPixels(imgIdle, imgAfter, region); moved > 50 {
		t.Errorf("the picture of the idle window and the one after the editor was asked differ in %d pixels: the hover was changed by the query", moved)
	}
	if drawnWhenIdle < drawnAfterAsking/2 {
		t.Errorf("the hover answered while the window was idle was not drawn until something woke the event loop: %d pixels differ while idle, %d afterwards", drawnWhenIdle, drawnAfterAsking)
	}
}
