//go:build emetos_display

package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/devdebug"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The debugger in the editor, with real input (step 25.5, SPEC §10.31): F9 in the editor sets a breakpoint that the margin shows, F5
// starts the fake adapter of daptest through the dev debugger and the program stops at it, the pointer on a word shows the value in the
// debug hover, and F10 steps to the next line. What is on the screen is read from the pictures the compositor gives (grim), because the
// marks of the margin and the lines are painted, not widgets.

const debugLoader = `package src

import "strings"

func Load(text string) []string {
	lines := strings.Split(text, "\n")
	rows := make([]string, 0, len(lines))
	for _, l := range lines {
		rows = append(rows, l)
	}
	return rows
}
`

// debugScript is the script of the fake adapter: a stop at line 5 (the breakpoint set with F9), the value of text for the hover and a
// step to line 6.
func debugScript(t *testing.T) []byte {
	t.Helper()
	frame := func(line int) map[string]any {
		return map[string]any{"totalFrames": 1, "stackFrames": []map[string]any{
			{"id": 101, "name": "src.Load", "line": line, "column": 1, "source": map[string]any{"name": "loader.go", "path": "${workspaceFolder}/src/loader.go"}}}}
	}
	stop := func(reason string, line int) []daptest.Step {
		return []daptest.Step{
			daptest.Event("stopped", map[string]any{"reason": reason, "threadId": 1, "allThreadsStopped": true}),
			daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}}),
			daptest.Reply("stackTrace", frame(line)),
			daptest.Reply("scopes", map[string]any{"scopes": []map[string]any{{"name": "Locals", "variablesReference": 1000}}}),
			daptest.Reply("variables", map[string]any{"variables": []map[string]any{{"name": "text", "value": `"one\ntwo"`, "type": "string", "evaluateName": "text"}}}),
		}
	}
	s := daptest.Script{
		daptest.Reply("initialize", map[string]any{"supportsConfigurationDoneRequest": true}),
		daptest.Expect("launch", nil),
		daptest.Event("initialized", nil),
		daptest.Reply("setBreakpoints", map[string]any{"breakpoints": []map[string]any{{"id": 1, "verified": true, "line": 5}}}),
		daptest.Reply("configurationDone", nil),
		daptest.Respond("launch", nil),
		daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}}),
	}
	s = append(s, stop("breakpoint", 5)...)
	s = append(s, daptest.Reply("evaluate", map[string]any{"result": `"one\ntwo"`, "type": "string"}), daptest.Reply("next", nil))
	s = append(s, stop("step", 6)...)
	s = append(s, daptest.Reply("disconnect", nil), daptest.WaitClose())
	lines, err := s.Lines()
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

// countPixels counts the pixels of a rectangle that pass the test.
func countPixels(img image.Image, r image.Rectangle, keep func(c color.RGBA) bool) int {
	n := 0
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if keep(c) {
				n++
			}
		}
	}
	return n
}

func isRed(c color.RGBA) bool    { return c.R > 190 && c.G < 70 && c.B < 70 }
func isYellow(c color.RGBA) bool { return c.R > c.B+25 && c.G > c.B+20 && c.R > 40 }

// glyphBox is the glyph margin of a line of the editor (the first column of the gutter) and textBox the text of the line.
func glyphBox(line int) image.Rectangle {
	y := int(editorLine(line))
	return image.Rect(350, y-9, 372, y+9)
}

func textBox(line int) image.Rectangle {
	y := int(editorLine(line))
	return image.Rect(1100, y-8, 1160, y+8) // the right end of the line: its highlight, with no inline values on it
}

func TestRealInputDebugInTheEditorSetsABreakpointStopsAndSteps(t *testing.T) {
	adapter := daptest.Program(t)
	script := debugScript(t)
	launch := `{"version":"0.2.0","configurations":[{"type":"dev.debug.fake","request":"launch","name":"Debug the loader","script":"${workspaceFolder}/fake.jsonl"}]}`
	a := display.Start(t, display.Options{
		Args: []string{".", "src/loader.go"},
		Project: map[string]string{
			"src/loader.go":       debugLoader,
			".emetos/launch.json": launch,
		},
		Prepare: func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, "fake.jsonl"), script, 0o644); err != nil {
				t.Fatal(err)
			}
		},
		Env: []string{devdebug.AdapterEnv + "=" + adapter},
	})
	a.Settle()
	dir := proofDir(t)
	shotAt := func(name string) image.Image {
		a.Settle()
		path := filepath.Join(dir, "debug-editor-"+name+".png")
		a.Screenshot(path)
		return readPNG(t, path)
	}
	reds := func(img image.Image, line int) int { return countPixels(img, glyphBox(line), isRed) }

	// F9 on line 5: the margin shows a breakpoint there and nowhere else
	a.Click(editorColumnX(8), editorLine(5))
	a.Press("f9")
	a.Eventually("the breakpoint to show in the margin", func() (bool, string) {
		img := shotAt("breakpoint")
		return reds(img, 5) > 30 && reds(img, 4) == 0 && reds(img, 6) == 0, a.Dump()
	})
	// F9 again takes it away, and once more puts it back
	a.Press("f9")
	a.Eventually("the breakpoint to go", func() (bool, string) { return reds(shotAt("removed"), 5) == 0, a.Dump() })
	a.Press("f9")
	a.Eventually("the breakpoint to come back", func() (bool, string) { return reds(shotAt("again"), 5) > 30, a.Dump() })

	// F5 starts the session: the program stops at the line, which is highlighted, and the Variables view shows the value
	a.Press("f5")
	a.EventuallyFor(60*time.Second, "the stop", func() (bool, string) {
		_, ok := a.Text("text = \"one\\ntwo\"") // the Variables view of the stopped frame
		return ok, a.Dump()
	})
	a.Eventually("the stopped line to be highlighted", func() (bool, string) {
		img := shotAt("stopped")
		return countPixels(img, textBox(5), isYellow) > 300 && countPixels(img, textBox(4), isYellow) < 20, a.Dump()
	})

	// the pointer rests on the word text of the parameter: the debug hover shows its value
	a.MoveTo(editorColumnX(14), editorLine(5))
	a.EventuallyFor(20*time.Second, "the value in the debug hover", func() (bool, string) {
		return hasText(a, `"one\ntwo"`, 360, 60), a.Dump()
	})
	shotAt("hover")

	// F10 steps over: the next line is the stopped one
	a.MoveTo(150, 600)
	a.Press("f10")
	a.Eventually("the stop at the next line", func() (bool, string) {
		img := shotAt("stepped")
		return countPixels(img, textBox(6), isYellow) > 300 && countPixels(img, textBox(5), isYellow) < 20, a.Dump()
	})
}

// hasText tells whether a text that is not in the side bar (to the right of x) and below y shows on screen.
func hasText(a *display.App, text string, x, y float64) bool {
	for _, tx := range a.Texts() {
		if tx.Text == text && tx.X > x && tx.Y > y {
			return true
		}
	}
	return false
}
