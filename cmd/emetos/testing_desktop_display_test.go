//go:build emetos_display

package main

import (
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The whole of Testing with the real go and real input (step 26.10, SPEC §10.32 "Tests"): the Go fixture of the proofs of step 26.5 is
// opened, the Test Explorer shows its tests, Run All Tests runs them with `go test`, the failing test shows its glyph and its message, a
// click on the glyph runs it again, the peek shows the expected and actual values, Rerun Failed Tests and Run All Tests with Coverage
// follow with the peek open, the Test Coverage view and a split editor show the coverage of cart.go, and Escape closes the peek. The
// screenshot of that state is the proof of the step (docs/implemented-proof-images/26.10-desktop-pass.png). It is skipped when go is
// missing.
//
//	scripts/realinput.sh --shots docs/implemented-proof-images -- -tags emetos_display ./cmd/emetos -run TestRealInputTheWholeOfTesting -v -count=2

// goTestingProject is the Go fixture of step 26.5 in a project of the display harness, with go in the PATH, a build cache of the test's
// own and no network.
func goTestingProject(t *testing.T) display.Options {
	t.Helper()
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not installed")
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "proof", "26.5", "project", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	return display.Options{
		Args: []string{".", "cart_test.go"},
		Project: map[string]string{
			"cart.go":      read("cart.go"),
			"cart_test.go": read("cart_test.go"),
			"go.mod":       read("go.mod"),
		},
		Config: map[string]string{"settings.json": `{
	"workbench.colorTheme": "Dark+",
	"editor.minimap.enabled": false,
	"git.enabled": false,
	"editor.codeLens": false,
	"testing.openTesting": "neverOpen",
	"testing.followRunningTest": true,
	"testing.alwaysRevealTestOnStateChange": true,
	"testing.coverageToolbarEnabled": true
}`},
		Env: []string{"PATH=" + filepath.Dir(goPath) + ":/usr/bin:/bin", "GOCACHE=" + filepath.Join(t.TempDir(), "gocache"), "GOTOOLCHAIN=local",
			"GOPROXY=off", "GOFLAGS=", "GOENV=off", "GOWORK=off", "CGO_ENABLED=0"},
	}
}

func TestRealInputTheWholeOfTesting(t *testing.T) {
	a := display.Start(t, goTestingProject(t))
	a.Settle()
	dir := proofDir(t)
	shotAt := func(name string) image.Image {
		a.Settle()
		path := filepath.Join(dir, name+".png")
		a.Screenshot(path)
		return readPNG(t, path)
	}
	messageRed := func(img image.Image, line int) int {
		y := int(editorLine(line))
		return countPixels(img, image.Rect(700, y-8, 1150, y+8), isBadgeRed)
	}
	dismissToasts(a)

	// the Test Explorer lists the tests of the package, found by the Go extension
	a.Dev("workbench.view.testing.focus", nil)
	a.EventuallyFor(60*time.Second, "the Go package in the Test Explorer", func() (bool, string) {
		return hasText(a, "project", 60, 150), a.Dump()
	})
	a.Eventually("the run icons in the margin", func() (bool, string) {
		img := shotAt("testing-desktop-before")
		return countPixels(img, glyphBox(5), isGreenGlyph) > 6 && countPixels(img, glyphBox(24), isGreenGlyph) > 6 &&
			countPixels(img, glyphBox(34), isGreenGlyph) > 6 && countPixels(img, glyphBox(25), isGreenGlyph) == 0, a.Dump()
	})

	// Run All Tests (Ctrl+; A): go builds and runs the package; TestDiscount fails, the others pass
	a.Press("ctrl+semicolon", "a")
	a.EventuallyFor(120*time.Second, "the result of the run", func() (bool, string) {
		img := shotAt("testing-desktop-all")
		return textsContaining(a, "5/6 tests passed") && messageRed(img, 30) > 150 && countPixels(img, glyphBox(24), isSalmonGlyph) > 6 &&
			countPixels(img, glyphBox(34), isGreenGlyph) > 6, a.Dump()
	})
	for _, label := range []string{"TestTotal", "TestLabel"} {
		if !rowShown(a, label) {
			t.Errorf("no row %s in the Test Explorer: %s", label, a.Dump())
		}
	}
	a.Eventually("the subtests the run added", func() (bool, string) {
		return rowShown(a, "one_item") && rowShown(a, "three_items"), a.Dump()
	})

	// a click on the glyph of the failed test runs it again: the summary counts the one test, which fails again
	a.Click(359, editorLine(24))
	a.EventuallyFor(60*time.Second, "the run of TestDiscount alone", func() (bool, string) {
		img := shotAt("testing-desktop-glyph")
		return textsContaining(a, "0/1 tests passed") && messageRed(img, 30) > 150, a.Dump()
	})

	// the cursor in the test and Peek Output (Ctrl+; Ctrl+M): the expected and actual values of the failure
	a.Click(editorColumnX(10), editorLine(29))
	a.Press("ctrl+semicolon", "ctrl+m")
	a.EventuallyFor(20*time.Second, "the peek of TestDiscount", func() (bool, string) { return peekShown(a), a.Dump() })
	shotAt("testing-desktop-peek")

	// Rerun Failed Tests (Ctrl+; E): one failed test runs again, and the summary changes to the new run's
	before := summaryText(a)
	a.Press("ctrl+semicolon", "e")
	a.EventuallyFor(60*time.Second, "the rerun of the failed test", func() (bool, string) {
		now := summaryText(a)
		shotAt("testing-desktop-rerun")
		return strings.HasPrefix(now, "0/1 tests passed") && now != before, a.Dump()
	})

	// Run All Tests with Coverage (Ctrl+; Ctrl+Shift+A): the Test Coverage view lists cart.go
	a.Press("ctrl+semicolon", "ctrl+shift+a")
	a.EventuallyFor(120*time.Second, "the Test Coverage view", func() (bool, string) {
		return textsContaining(a, "86.67%") && strings.HasPrefix(summaryText(a), "5/6 tests passed"), a.Dump()
	})

	// the coverage in the editor: the editor splits, and Quick Open puts cart.go in the new group (on the right), with its covered and
	// uncovered lines
	a.Press("ctrl+backslash")
	a.Eventually("the split editor", func() (bool, string) { return tabsNamed(a, "cart_test.go", 0) == 2, a.Dump() })
	a.Press("ctrl+p")
	a.Type("cart.go")
	a.WaitFocusedText("cart.go")
	a.Press("enter")
	a.EventuallyFor(30*time.Second, "cart.go in the group on the right", func() (bool, string) {
		return tabsNamed(a, "cart.go", 815) == 1, a.Dump()
	})
	a.EventuallyFor(30*time.Second, "the coverage of cart.go", func() (bool, string) {
		img := shotAt("testing-desktop-coverage")
		box := image.Rect(830, 180, 1270, 700)
		return countPixels(img, box, isCoveredGreen) > 2000 && countPixels(img, box, isUncoveredRed) > 500, a.Dump()
	})

	// Escape closes the peek, in the editor that shows it (the first group). The peek is there still when the reruns left it open;
	// when a run closed it, Peek Output opens it again
	a.Press("ctrl+1")
	a.Settle()
	if !peekShown(a) {
		t.Log("the peek was closed by a run: Peek Output opens it again")
		a.Click(editorColumnX(10), editorLine(29))
		a.Press("ctrl+semicolon", "ctrl+m")
	}
	a.EventuallyFor(20*time.Second, "the peek to be on the screen", func() (bool, string) { return peekShown(a), a.Dump() })
	shotAt("26.10-desktop-pass")
	a.Press("escape")
	a.Eventually("the peek to close", func() (bool, string) { return !peekShown(a), a.Dump() })
}

// peekShown tells whether the peek of the failure is on screen: the message in its left pane, below the line of the editor that holds the
// failure's own badge (which has the same words).
func peekShown(a *display.App) bool {
	return hasText(a, "Discount(200, 15) = 170, want 175", 352, 680)
}

// summaryText is the summary line of the Test Explorer ("5/6 tests passed (83.3%), 6.3 s") or "".
func summaryText(a *display.App) string {
	for _, tx := range a.Texts() {
		if tx.X < 340 && strings.Contains(tx.Text, " tests passed (") {
			return tx.Text
		}
	}
	return ""
}

// isCoveredGreen and isUncoveredRed are the dark olive and dark red backgrounds of the covered and uncovered lines of Dark+ (the editor's
// own background is a grey with no tint).
func isCoveredGreen(c color.RGBA) bool {
	return c.G > c.B+15 && c.R > c.B+8 && c.G < 120 && c.R < 120 && c.G+10 > c.R
}

func isUncoveredRed(c color.RGBA) bool {
	return c.R > c.G+40 && c.R > c.B+40 && c.R > 80 && c.R < 190
}

// rowShown tells whether the Test Explorer shows a row with the label (its text is the label and the duration after it).
func rowShown(a *display.App, label string) bool {
	for _, tx := range a.Texts() {
		if tx.X < 340 && tx.Y > 150 && (tx.Text == label || strings.HasPrefix(tx.Text, label+" ")) {
			return true
		}
	}
	return false
}

// tabsNamed counts the tabs of the editors named name (a tab's text is the name, or the name and the coverage of a file after it) to the
// right of x.
func tabsNamed(a *display.App, name string, x float64) int {
	n := 0
	for _, tx := range a.Texts() {
		if tx.Y < 60 && tx.X > x && (tx.Text == name || strings.HasPrefix(tx.Text, name+" ")) {
			n++
		}
	}
	return n
}
