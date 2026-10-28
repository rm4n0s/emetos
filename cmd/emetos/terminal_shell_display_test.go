//go:build emetos_display

package main

import (
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Real-input tests of step 17d.5: shell integration with the test's own bash. The shell is bash with no arguments, so that the
// script of VS Code is injected (--init-file); the home folder of the run is empty, so the script reads no rc file of anybody.
// The tests are skipped when bash is not installed.

// shellConfig is the configuration folder of the run: bash with no arguments (the script is injected), a short prompt and no
// history file.
var shellConfig = map[string]string{
	"settings.json": `{
		"terminal.integrated.defaultProfile.linux": "bash",
		"terminal.integrated.profiles.linux": {"bash": {"path": "bash"}},
		"terminal.integrated.env.linux": {"PS1": "$ ", "HISTFILE": "/dev/null"}
	}`,
}

// startShellApp opens the editor with the terminal and waits until the shell integration is active.
func startShellApp(t *testing.T, files map[string]string, prepare func(dir string)) *display.App {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	if files == nil {
		files = map[string]string{}
	}
	files["app.go"] = "package main\n"
	a := display.Start(t, display.Options{Args: []string{".", "app.go"}, Project: files, Config: shellConfig, Prepare: prepare})
	a.Settle()
	a.Press("ctrl+grave")
	waitTerminals(t, a, 1)
	a.Eventually("shell integration to be active", func() (bool, string) {
		tm := a.Terminals()
		return len(tm) == 1 && tm[0].ShellIntegration, termText(a, 0)
	})
	return a
}

// runCommand types a line and waits until n commands have ended.
func runCommand(t *testing.T, a *display.App, line string, n int) {
	t.Helper()
	a.Type(line)
	a.Press("Return")
	a.Eventually("the command "+line+" to end", func() (bool, string) {
		tm := a.Terminals()[0]
		return tm.Commands >= n, termText(a, 0)
	})
}

// pixelAt reads a pixel of a screenshot of the window.
func pixelAt(t *testing.T, a *display.App, x, y float64) (r, g, b uint32) {
	t.Helper()
	path := a.Screenshot(filepath.Join(t.TempDir(), "shot.png"))
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return samplePixel(img, int(x), int(y))
}

// samplePixel is the color of a pixel in 8 bits.
func samplePixel(img image.Image, x, y int) (r, g, b uint32) {
	cr, cg, cb, _ := img.At(img.Bounds().Min.X+x, img.Bounds().Min.Y+y).RGBA()
	return cr >> 8, cg >> 8, cb >> 8
}

func TestRealInputShellDecorationsAreBlueAndRed(t *testing.T) {
	a := startShellApp(t, nil, nil)
	runCommand(t, a, "true", 1)
	runCommand(t, a, "false", 2)
	var tm display.Terminal
	a.Eventually("a blue and a red decoration", func() (bool, string) {
		tm = a.Terminals()[0]
		kinds := ""
		for _, d := range tm.Decorations {
			kinds += d.Kind + " "
		}
		return len(tm.Decorations) == 2, kinds
	})
	byKind := map[string]int{}
	for _, d := range tm.Decorations {
		byKind[d.Kind] = d.Row
	}
	rows := strings.Split(tm.Text, "\n")
	for kind, line := range map[string]string{"success": "$ true", "error": "$ false"} {
		row, ok := byKind[kind]
		if !ok || row >= len(rows) || rows[row] != line {
			t.Errorf("the %s decoration is on row %d (%q), want the row of %q\n%s", kind, row, rows[row%len(rows)], line, tm.Text)
		}
	}
	// the colors on screen: the circle is in the gutter, at the left of the first column
	x := tm.X + tm.PadLeft/2 - 1
	rs, gs, bs := pixelAt(t, a, x, tm.Y+(float64(byKind["success"])+0.5)*tm.CellH)
	if bs < rs+60 {
		t.Errorf("the decoration of the command that worked is (%d,%d,%d), want blue", rs, gs, bs)
	}
	re, ge, be := pixelAt(t, a, x, tm.Y+(float64(byKind["error"])+0.5)*tm.CellH)
	if re < be+60 || re < ge+60 {
		t.Errorf("the decoration of the command that failed is (%d,%d,%d), want red", re, ge, be)
	}
}

func TestRealInputShellCtrlUpScrollsToTheFirstCommand(t *testing.T) {
	a := startShellApp(t, nil, nil)
	runCommand(t, a, "echo first", 1)
	runCommand(t, a, "seq 1 60", 2)
	runCommand(t, a, "echo last", 3)
	if tm := a.Terminals()[0]; tm.Scroll != 0 {
		t.Fatalf("scroll = %d before the keys", tm.Scroll)
	}
	// Ctrl+Up goes command by command: the last (its line is at the bottom of the view already, so the view cannot move: the
	// view is clamped), the one before it with the long output, and the first
	a.Press("ctrl+Up")
	a.Press("ctrl+Up")
	a.Eventually("the command with the long output at the top", func() (bool, string) {
		tm := a.Terminals()[0]
		return tm.Scroll > 0 && strings.HasPrefix(firstRow(tm), "$ seq 1 60"), firstRow(tm) + " scroll " + itoa(tm.Scroll)
	})
	for i := 0; i < 4; i++ {
		a.Press("ctrl+Up")
	}
	a.Eventually("the first command at the top of the buffer", func() (bool, string) {
		tm := a.Terminals()[0]
		return tm.Scroll == tm.Scrollback && strings.HasPrefix(firstRow(tm), "$ echo first"), firstRow(tm) + " scroll " + itoa(tm.Scroll)
	})
	// Ctrl+Down goes back toward the bottom
	for i := 0; i < 6; i++ {
		a.Press("ctrl+Down")
	}
	a.Eventually("the view at the bottom again", func() (bool, string) {
		tm := a.Terminals()[0]
		return tm.Scroll == 0, "scroll " + itoa(tm.Scroll)
	})
}

// firstRow is the first row of the screen of a terminal.
func firstRow(tm display.Terminal) string {
	row, _, _ := strings.Cut(tm.Text, "\n")
	return row
}

func TestRealInputShellRunRecentCommand(t *testing.T) {
	a := startShellApp(t, nil, nil)
	runCommand(t, a, "echo again", 1)
	runCommand(t, a, "true", 2)
	a.Press("ctrl+alt+r")
	a.Eventually("the list of recent commands", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Text == "", "focused: " + in.Text
	})
	a.Type("echo again")
	a.WaitFocusedText("echo again")
	a.Press("Return")
	a.Eventually("the command to run again", func() (bool, string) {
		tm := a.Terminals()[0]
		n := 0
		for _, l := range strings.Split(tm.Text, "\n") {
			if l == "again" {
				n++
			}
		}
		return n == 2 && tm.Commands >= 3, tm.Text
	})
}

func TestRealInputShellStickyScrollKeepsTheCommandAtTheTop(t *testing.T) {
	a := startShellApp(t, nil, nil)
	runCommand(t, a, "seq 1 200", 1)
	a.Eventually("the sticky command", func() (bool, string) {
		tm := a.Terminals()[0]
		return tm.Sticky && len(tm.StickyRows) > 0 && strings.Contains(tm.StickyRows[0], "$ seq 1 200"), strings.Join(tm.StickyRows, "|")
	})
	// a short output keeps its command in view: no overlay
	a.Press("ctrl+l")
	runCommand(t, a, "echo short", 2)
	a.Eventually("no overlay", func() (bool, string) { return !a.Terminals()[0].Sticky, "" })
	// a click on the overlay goes to the command
	runCommand(t, a, "seq 1 200", 3)
	a.Eventually("the overlay", func() (bool, string) { return a.Terminals()[0].Sticky, "" })
	tm := a.Terminals()[0]
	a.Click(tm.X+tm.PadLeft+40, tm.Y+tm.CellH/2)
	a.Eventually("the command at the top", func() (bool, string) {
		t2 := a.Terminals()[0]
		return !t2.Sticky && strings.HasPrefix(firstRow(t2), "$ seq 1 200") && t2.Scroll > 0, firstRow(t2)
	})
}

func TestRealInputShellASplitStartsInTheFolderOfTheShell(t *testing.T) {
	a := startShellApp(t, nil, nil)
	runCommand(t, a, "cd /tmp", 1)
	a.Eventually("the folder the shell reported", func() (bool, string) { return a.Terminals()[0].Cwd == "/tmp", a.Terminals()[0].Cwd })
	a.Press("ctrl+shift+5")
	waitTerminals(t, a, 2)
	a.Eventually("the shell of the split", func() (bool, string) {
		tm := a.Terminals()
		return len(tm) == 2 && tm[1].ShellIntegration, ""
	})
	a.Type("pwd")
	a.Press("Return")
	a.Eventually("pwd in the split", func() (bool, string) {
		got := termText(a, 1)
		return hasLine(got, "/tmp"), got
	})
	if got := termText(a, 0); strings.Count(got, "/tmp\n") > 0 && !strings.Contains(got, "cd /tmp") {
		t.Errorf("the first terminal shows %q", got)
	}
}

// TestRealInputShellProof makes the proof image of step 17d.5: a passed and a failed command with their decorations, and the
// sticky scroll over the output of a long one.
func TestRealInputShellProof(t *testing.T) {
	a := startShellApp(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "README.md": "# project\n"}, nil)
	runCommand(t, a, "seq 1 100", 1)
	runCommand(t, a, "ls main.go", 2)
	runCommand(t, a, "ls missing.txt", 3)
	runCommand(t, a, "echo done", 4)
	a.Eventually("the decorations and the sticky command", func() (bool, string) {
		tm := a.Terminals()[0]
		kinds := map[string]bool{}
		for _, d := range tm.Decorations {
			kinds[d.Kind] = true
		}
		return kinds["success"] && kinds["error"] && tm.Sticky, tm.Text
	})
	a.Settle()
	a.Screenshot(filepath.Join(proofDir(t), "17d.5-shell-integration.png"))
}
