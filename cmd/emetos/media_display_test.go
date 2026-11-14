//go:build emetos_display

package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The viewers of M36 together (step 36.9): an image, a video, a PDF and a book opened from the Explorer in one window with the mouse,
// each in its viewer, then closed, with no FFmpeg or Poppler process left. It needs the real FFmpeg and Poppler, and is skipped
// without them.

// mediaPNG is a picture of 64 x 48 pixels.
func mediaPNG(t *testing.T) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(4 * x), G: uint8(5 * y), B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// leftOf are the FFmpeg and Poppler processes still running on a file of the folder.
func leftOf(t *testing.T, folder string) []string {
	t.Helper()
	out, err := exec.Command("ps", "-eo", "args").Output()
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for line := range strings.Lines(string(out)) {
		if strings.Contains(line, folder) && (strings.Contains(line, "ffmpeg") || strings.Contains(line, "pdftoppm") ||
			strings.Contains(line, "pdfinfo") || strings.Contains(line, "pdftotext") || strings.Contains(line, "pdftohtml")) {
			left = append(left, strings.TrimSpace(line))
		}
	}
	return left
}

func TestDesktopMediaViewers(t *testing.T) {
	for _, program := range []string{"ffmpeg", "ffprobe", "pdfinfo", "pdftoppm"} {
		if _, err := exec.LookPath(program); err != nil {
			t.Skipf("%s is not installed", program)
		}
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := os.ReadFile(filepath.Join(root, "testdata/proof/36.5/project/sample.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	book, err := os.ReadFile(filepath.Join(root, "testdata/proof/36.8/project/sample.epub"))
	if err != nil {
		t.Fatal(err)
	}
	a := display.Start(t, display.Options{Args: []string{"."},
		Project: map[string]string{"a-picture.png": string(mediaPNG(t)), "c-document.pdf": string(pdf), "d-book.epub": string(book)},
		Prepare: func(dir string) {
			// a 4 s video in codecs every FFmpeg has (FFV1 and FLAC)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=25:duration=4",
				"-f", "lavfi", "-i", "sine=frequency=440:duration=4", "-c:v", "ffv1", "-c:a", "flac", "-shortest",
				filepath.Join(dir, "b-video.mkv"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("the video cannot be made: %v\n%s", err, out)
			}
		},
	})
	a.Settle()
	explorerRows := inRect(0, 90, 348, 750)
	open := func(name, shows string) {
		t.Helper()
		clickTextWhere(t, a, name, explorerRows)
		a.Eventually(name+" to open", func() (bool, string) { return activeTab(a) == name, joinNames(tabNames(a)) })
		a.WaitText(shows)
		if kind := activeKind(a); kind != "emetos.fileEditor" {
			t.Errorf("%s opened in %q, not in a viewer", name, kind)
		}
	}
	t.Run("an image", func(t *testing.T) {
		open("a-picture.png", "64x48")
		shot(t, a, "media-image")
	})
	t.Run("a video", func(t *testing.T) {
		open("b-video.mkv", "0:00 / 0:04")
		// the Play button, left of the time (Space needs the focus, which a click on the picture moves only with the next frame; the
		// keys are tested in extensions/mediapreview)
		playButton := func() {
			t.Helper()
			tx := a.SteadyText("the time", func() (app.DevText, bool) {
				for _, tx := range a.Texts() {
					if strings.HasSuffix(tx.Text, " / 0:04") {
						return tx, true
					}
				}
				return app.DevText{}, false
			})
			a.Click(tx.X-19, tx.Y+tx.Height/2)
		}
		playButton()
		a.Eventually("the video to play", func() (bool, string) {
			_, ok := a.Text("0:00 / 0:04")
			return !ok, joinTexts(a)
		})
		shot(t, a, "media-video")
		playButton() // pause
	})
	t.Run("a PDF", func(t *testing.T) {
		open("c-document.pdf", "/ 4")
		shot(t, a, "media-pdf")
	})
	t.Run("a book", func(t *testing.T) {
		open("d-book.epub", "Alice’s Adventures in Wonderland — Down the Rabbit-Hole")
		shot(t, a, "media-epub")
	})
	t.Run("closed, nothing is left", func(t *testing.T) {
		a.Dev("workbench.action.closeAllEditors", nil)
		a.Eventually("the tabs to close", func() (bool, string) { return len(tabNames(a)) == 0, joinNames(tabNames(a)) })
		a.Eventually("FFmpeg and Poppler to end", func() (bool, string) {
			left := leftOf(t, a.Project)
			return len(left) == 0, strings.Join(left, "\n")
		})
	})
}

// activeKind is the kind of the active tab of the first group.
func activeKind(a *display.App) string {
	for _, tab := range a.Workbench().Groups[0].Tabs {
		if tab.Active {
			return tab.Kind
		}
	}
	return ""
}

// joinTexts is the texts on screen, for a failure's message.
func joinTexts(a *display.App) string {
	var list []string
	for _, tx := range a.Texts() {
		list = append(list, tx.Text)
	}
	return strings.Join(list, " | ")
}
