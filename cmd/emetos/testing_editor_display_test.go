//go:build emetos_display

package main

import (
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Testing in the editor, with real input (step 26.3, SPEC §10.32): the glyphs of the tests in the margin, a click on one runs its test,
// the failure shows after its line, Peek Output and Alt+F8 show the peek, and Rerun Failed Tests runs the failures again. The controller is
// the fake one of extensions/devtesting. What the glyphs and the message look like is read from the pictures the compositor gives (grim),
// because they are painted, not widgets; the summary of the Test Explorer and the text of the peek are widgets.

const testingEditorSource = `package calc

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Errorf("Add = %d", got)
	}
}

func TestDivide(t *testing.T) {
	got, _ := Divide(5, 2)
	if got != 2.5 {
		t.Errorf("Divide(5, 2) = %v", got)
	}
}

func TestReport(t *testing.T) {
	got := Report(12)
	if got != "Total: 12\nTax: 2.40" {
		t.Errorf("Report(12) = %q", got)
	}
}
`

const testingEditorScript = `{
  "label": "Fake Tests (dev)",
  "delayMs": 5,
  "tests": [
    {"id": "TestAdd", "label": "TestAdd", "file": "calc_test.go", "line": 5, "endLine": 9, "result": "passed", "ms": 2},
    {"id": "TestDivide", "label": "TestDivide", "file": "calc_test.go", "line": 11, "endLine": 16, "result": "failed", "ms": 12,
      "message": "Divide(5, 2) = 2, want 2.5", "failLine": 14,
      "stack": [{"label": "calc.TestDivide", "file": "calc_test.go", "line": 14}, {"label": "testing.tRunner"}]},
    {"id": "TestReport", "label": "TestReport", "file": "calc_test.go", "line": 18, "endLine": 23, "result": "failed", "ms": 3,
      "message": "Report(12) is wrong", "failLine": 21, "expected": "Total: 12\nTax: 2.40", "actual": "Total: 12\nTax: 2.04"}
  ]
}`

func isGreenGlyph(c color.RGBA) bool  { return c.G > 170 && c.G > c.R+50 && c.G > c.B+20 && c.R < 160 }
func isSalmonGlyph(c color.RGBA) bool { return c.R > 200 && c.R > c.G+70 && c.G < 175 && c.B < 150 }
func isBadgeRed(c color.RGBA) bool    { return c.R > 215 && c.G < 110 && c.B < 110 }

// textsContaining tells whether some text on screen has the words.
func textsContaining(a *display.App, words string) bool {
	for _, tx := range a.Texts() {
		if strings.Contains(tx.Text, words) {
			return true
		}
	}
	return false
}

func TestRealInputTestingInTheEditorRunsFromTheGlyphShowsThePeekAndRerunsTheFailed(t *testing.T) {
	a := display.Start(t, display.Options{
		Args:    []string{".", "calc_test.go"},
		Config:  map[string]string{"settings.json": `{"testing.openTesting": "neverOpen", "editor.minimap.enabled": false, "editor.codeLens": false}`},
		Project: map[string]string{"calc_test.go": testingEditorSource, "fake-tests.json": testingEditorScript},
	})
	a.Settle()
	dir := proofDir(t)
	shot := func(name string) image.Image {
		a.Settle()
		path := filepath.Join(dir, "testing-editor-"+name+".png")
		a.Screenshot(path)
		return readPNG(t, path)
	}
	glyph := func(img image.Image, line int, keep func(color.RGBA) bool) int {
		return countPixels(img, glyphBox(line), keep)
	}
	messageRed := func(img image.Image, line int) int {
		y := int(editorLine(line))
		return countPixels(img, image.Rect(800, y-8, 1150, y+8), isBadgeRed)
	}

	// the Test Explorer shows the summary; the margin has the run icon on the lines of the three tests, and the others have none
	a.Dev("workbench.view.testing.focus", nil)
	a.Eventually("the run icons in the margin", func() (bool, string) {
		img := shot("before")
		return glyph(img, 5, isGreenGlyph) > 6 && glyph(img, 11, isGreenGlyph) > 6 && glyph(img, 18, isGreenGlyph) > 6 &&
			glyph(img, 6, isGreenGlyph) == 0 && glyph(img, 11, isSalmonGlyph) == 0, a.Dump()
	})

	// a click on the glyph of TestDivide runs it: its glyph turns to the failure, the message shows after the line of the failure
	a.Click(359, editorLine(11))
	a.EventuallyFor(30*time.Second, "the failure of TestDivide", func() (bool, string) {
		img := shot("divide")
		return glyph(img, 11, isSalmonGlyph) > 6 && messageRed(img, 14) > 150 && messageRed(img, 21) == 0 && textsContaining(a, "0/1 tests passed"), a.Dump()
	})

	// Run All Tests: both failures show their messages, and the summary counts them
	a.Press("ctrl+semicolon", "a")
	a.EventuallyFor(30*time.Second, "both failures", func() (bool, string) {
		img := shot("all")
		return messageRed(img, 14) > 150 && messageRed(img, 21) > 150 && glyph(img, 5, isGreenGlyph) > 6 && textsContaining(a, "1/3 tests passed"), a.Dump()
	})

	// Peek Output with the cursor at the second failure: the peek shows its message; Alt+F8 goes to the other failure
	a.Click(editorColumnX(10), editorLine(20))
	a.Press("ctrl+semicolon", "ctrl+m")
	a.EventuallyFor(20*time.Second, "the peek of TestReport", func() (bool, string) {
		_, ok := a.Text("Report(12) is wrong")
		return ok, a.Dump()
	})
	shot("peek")
	a.Press("alt+f8")
	a.EventuallyFor(20*time.Second, "the peek of TestDivide", func() (bool, string) {
		_, ok := a.Text("Divide(5, 2) = 2, want 2.5")
		_, other := a.Text("Report(12) is wrong")
		return ok && !other, a.Dump()
	})
	shot("peek-next")
	a.Press("escape")
	a.Eventually("the peek to close", func() (bool, string) {
		_, ok := a.Text("Divide(5, 2) = 2, want 2.5")
		return !ok, a.Dump()
	})

	// Rerun Failed Tests runs the two failures again: the newest run has two tests, none of which passed
	a.Press("ctrl+semicolon", "e")
	a.EventuallyFor(30*time.Second, "the run of the failed tests", func() (bool, string) {
		return textsContaining(a, "0/2 tests passed"), a.Dump()
	})
}
