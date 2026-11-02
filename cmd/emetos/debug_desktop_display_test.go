//go:build emetos_display

package main

import (
	"encoding/json"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The whole of Run and Debug with real Delve and real input (step 25.12, SPEC §10.31 "Tests"): the Go fixture of the proofs is opened,
// F9 sets a breakpoint, F5 starts dlv and the program stops at it, the pointer on a variable shows its value in the debug hover, a watch
// expression is added, F10 steps, the Debug Console evaluates an expression, and Shift+F5 stops. The screenshot of the stopped session is
// the proof of the step (docs/implemented-proof-images/25.12-desktop-pass.png). It is skipped when the pinned dlv or go is missing.
//
//	scripts/realinput.sh --shots docs/implemented-proof-images -- -tags emetos_display ./cmd/emetos -run TestRealInputTheWholeOfDebugging -v

const (
	pinnedDlv = "/var/tmp/emetos-tools/bin/dlv"
	toolsDir  = "/var/tmp/emetos-tools"
)

// goDebugProject is the Go fixture of the proofs in a project of the display harness, with the settings that point the Go extension at
// the pinned dlv and the tools' caches, and a PATH with go and no gopls.
func goDebugProject(t *testing.T) display.Options {
	t.Helper()
	if st, err := os.Stat(pinnedDlv); err != nil || st.IsDir() {
		t.Skipf("the pinned dlv is not at %s (scripts/fetch-tools.sh dlv)", pinnedDlv)
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not installed")
	}
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "proof", "25.7", "project", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	return display.Options{
		Args: []string{".", "main.go"},
		Project: map[string]string{
			"main.go": read("main.go"),
			"go.mod":  read("go.mod"),
			".emetos/launch.json": `{"version": "0.2.0", "configurations": [
	{"name": "Debug the order", "type": "go", "request": "launch", "mode": "debug", "program": "${workspaceFolder}"}
]}`,
		},
		Config: map[string]string{"settings.json": `{
	"workbench.colorTheme": "Dark+",
	"editor.minimap.enabled": false,
	"git.enabled": false,
	"go.alternateTools": {"dlv": "` + pinnedDlv + `"},
	"go.toolsEnvVars": {"GOCACHE": "` + toolsDir + `/gocache", "GOPATH": "` + toolsDir + `/gopath", "GOMODCACHE": "` + toolsDir + `/gomodcache",
		"GOFLAGS": "", "GOTOOLCHAIN": "local", "GOPROXY": "off", "CGO_ENABLED": "0"}
}`},
		// the user's gopls is not wanted here: go for dlv's build, the system's programs for the rest
		Env: []string{"PATH=" + filepath.Dir(goPath) + ":/usr/bin:/bin"},
	}
}

func TestRealInputTheWholeOfDebugging(t *testing.T) {
	a := display.Start(t, goDebugProject(t))
	a.Settle()
	dir := proofDir(t)
	shotAt := func(name string) image.Image {
		a.Settle()
		path := filepath.Join(dir, name+".png")
		a.Screenshot(path)
		return readPNG(t, path)
	}
	reds := func(img image.Image, line int) int { return countPixels(img, glyphBox(line), isRed) }
	dismissToasts(a) // "gopls was not found": the user's gopls is kept out of the test

	// F9 on line 12 (amount := order.Total * rate)
	a.Click(editorColumnX(8), editorLine(12))
	a.Press("f9")
	a.Eventually("the breakpoint to show in the margin", func() (bool, string) {
		img := shotAt("debug-desktop-breakpoint")
		return reds(img, 12) > 30 && reds(img, 11) == 0 && reds(img, 13) == 0, a.Dump()
	})

	// F5 starts dlv, which builds the program; it stops at the breakpoint and the Variables view shows the arguments
	a.Press("f5")
	a.EventuallyFor(90*time.Second, "the stop at the breakpoint", func() (bool, string) {
		_, ok := a.Text("rate = 0.1")
		return ok, a.Dump()
	})
	a.Eventually("the stopped line to be highlighted", func() (bool, string) {
		img := shotAt("debug-desktop-stopped")
		return countPixels(img, indentBox(12), isYellow) > 300 && countPixels(img, indentBox(11), isYellow) < 20, a.Dump()
	})

	// the pointer on the word rate of line 12: the debug hover shows its value
	a.MoveTo(editorColumnX(30)+4, editorLine(12)) // on the word rate
	a.EventuallyFor(20*time.Second, "the value in the debug hover", func() (bool, string) {
		return hasText(a, "0.1", 360, 60), a.Dump()
	})
	shotAt("debug-desktop-hover")

	// Add to Watch from the menu of the variable: the Watch view lists the expression
	a.MoveTo(150, 600)
	a.RightClickText("rate = 0.1")
	a.WaitText("Add to Watch")
	a.ClickText("Add to Watch")
	a.EventuallyFor(20*time.Second, "the watch expression in the Watch view", func() (bool, string) {
		return countText(a, "rate = 0.1") >= 2, a.Dump() // the Variables view and the Watch view
	})

	// F10 steps over: line 13 is the stopped one, and amount has its value
	a.Click(editorColumnX(8), editorLine(14))
	a.Press("f10")
	a.Eventually("the stop at the next line", func() (bool, string) {
		img := shotAt("debug-desktop-stepped")
		return countPixels(img, indentBox(13), isYellow) > 300 && countPixels(img, indentBox(12), isYellow) < 20, a.Dump()
	})
	a.Eventually("amount in the Variables view", func() (bool, string) { _, ok := a.Text("amount = 4.95"); return ok, a.Dump() })

	// the Debug Console evaluates in the stopped frame
	a.Press("ctrl+shift+y")
	a.Eventually("the input of the console to have the keyboard", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Y > 450, a.Dump()
	})
	a.Type("order.Total * 2")
	a.Press("enter")
	a.EventuallyFor(30*time.Second, "the result of the evaluation", func() (bool, string) { _, ok := a.Text("99"); return ok, a.Dump() })

	// the hover on amount, and the picture of the whole
	a.MoveTo(editorColumnX(13), editorLine(13)) // on the word amount
	a.EventuallyFor(20*time.Second, "the hover on amount", func() (bool, string) { return hasText(a, "4.95", 360, 60), a.Dump() })
	shotAt("25.12-desktop-pass")

	// Shift+F5 stops it: the variables go, and the stopped line with them
	a.MoveTo(150, 600)
	a.Press("shift+f5")
	a.EventuallyFor(30*time.Second, "the end of the session", func() (bool, string) {
		_, ok := a.Text("Locals")
		img := shotAt("debug-desktop-ended")
		return !ok && countPixels(img, indentBox(13), isYellow) < 20, a.Dump()
	})
}

// countText counts the runs of text on screen that are exactly text.
func countText(a *display.App, text string) int {
	n := 0
	for _, tx := range a.Texts() {
		if tx.Text == text {
			n++
		}
	}
	return n
}

// dismissToasts removes the notifications whose toasts show.
func dismissToasts(a *display.App) {
	resp, err := a.Request("dev.toasts", nil)
	if err != nil {
		return
	}
	var toasts []app.DevToast
	if json.Unmarshal(resp.Result, &toasts) != nil {
		return
	}
	for _, toast := range toasts {
		_, _ = a.Request("dev.dismissNotification", toast.ID)
	}
}

// indentBox is the empty indentation at the start of a line of the editor (the code of the fixture is indented by a tab there): the
// background of the highlight of a stopped line, with no text or inline value over it.
func indentBox(line int) image.Rectangle {
	y := int(editorLine(line))
	return image.Rect(441, y-8, 466, y+8)
}
