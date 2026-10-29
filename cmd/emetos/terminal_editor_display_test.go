//go:build emetos_display

package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Real-input tests of step 17d.7: terminals in the editor area, the rest of the tabs list and the persistent sessions. The keys
// and the pointer go through the compositor; every shell is bash with --norc --noprofile in the home folder of the run.

// tabsListWidth is the width of the tabs list of the panel (extensions/terminal tabsWidth) and its border.
const tabsListWidth = 180.0

// terminalsIn are the terminals on screen in a place: "view" is the panel, "editor" a tab of the editor area.
func terminalsIn(a *display.App, location string) []display.Terminal {
	var out []display.Terminal
	for _, tm := range a.Terminals() {
		if tm.Location == location {
			out = append(out, tm)
		}
	}
	return out
}

// waitLocated waits until n terminals are in the panel and m in tabs of the editor area, and returns them.
func waitLocated(t *testing.T, a *display.App, n, m int) (view, editor []display.Terminal) {
	t.Helper()
	a.Eventually("terminals in the panel and in the editor area", func() (bool, string) {
		view, editor = terminalsIn(a, "view"), terminalsIn(a, "editor")
		return len(view) == n && len(editor) == m, "in the panel " + itoa(len(view)) + ", in the editor " + itoa(len(editor))
	})
	return view, editor
}

// tabRowOf is a point on the row i of the tabs list of the panel, whose terminal on screen is tm: the list is at the right of the
// terminal's pane, a border and 180 pixels wide, with rows of 22 pixels from the top of the panel's body.
func tabRowOf(tm display.Terminal, i int) (x, y float64) {
	return tm.X + tm.Width + 1 + tabsListWidth/2, tm.Y + float64(i)*22 + 11
}

// editorAreaPoint is a point in the editor area above the panel, at a fraction of its width from the left of the terminal's pane
// (1 is the right edge of the window's editor area), 150 pixels above the panel.
func editorAreaPoint(tm display.Terminal, fraction float64) (x, y float64) {
	right := tm.X + tm.Width + 1 + tabsListWidth
	return tm.X + (right-tm.X)*fraction, tm.Y - 150
}

// runCommandOf runs a command through the palette by its title.
func runCommandOf(t *testing.T, a *display.App, title string) {
	t.Helper()
	a.Press("ctrl+shift+p")
	a.Type(title)
	a.WaitFocusedText(">" + title)
	a.Press("Return")
	a.Settle()
}

// countColor counts the pixels of a screenshot, in a box around a point, that satisfy a test.
func countColor(t *testing.T, a *display.App, x, y, half float64, is func(r, g, b uint32) bool) int {
	t.Helper()
	f, err := os.Open(a.Screenshot(filepath.Join(t.TempDir(), "shot.png")))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return countIn(img, int(x-half), int(y-half), int(x+half), int(y+half), is)
}

// countIn counts the pixels of a box of an image that satisfy a test.
func countIn(img image.Image, x0, y0, x1, y1 int, is func(r, g, b uint32) bool) int {
	n := 0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if r, g, b := samplePixel(img, x, y); is(r, g, b) {
				n++
			}
		}
	}
	return n
}

// reddish says that a pixel is the red of terminal.ansiRed (205, 49, 49 in the dark themes).
func reddish(r, g, b uint32) bool { return r > 150 && g < 100 && b < 100 }

func TestRealInputTerminalDragsFromTheTabsListToTheEditorAreaAndBack(t *testing.T) {
	a := startTerminalApp(t, nil)
	a.Type("echo first-terminal")
	a.Press("Return")
	waitTermText(t, a, 0, "first-terminal\n$")
	a.Press("ctrl+shift+grave")
	a.Eventually("the second terminal", func() (bool, string) {
		tm := a.Terminals()
		return len(tm) == 1 && tm[0].Focused && strings.Contains(tm[0].Text, "$") && !hasLine(tm[0].Text, "first-terminal"), termText(a, 0)
	})
	a.Type("echo in-the-panel")
	a.Press("Return")
	waitTermText(t, a, 0, "in-the-panel\n$")
	panel := a.Terminals()[0]
	pid := panel.Pid

	// the second row of the tabs list, dragged out of the list onto the middle of the editor area
	x, y := tabRowOf(panel, 1)
	ex, ey := editorAreaPoint(panel, 0.5)
	a.Drag(x, y, ex, ey)
	view, editor := waitLocated(t, a, 1, 1)
	if editor[0].Pid != pid || !hasLine(editor[0].Text, "in-the-panel") {
		t.Fatalf("the terminal in the editor area has pid %d (was %d) and shows:\n%s", editor[0].Pid, pid, editor[0].Text)
	}
	if hasLine(view[0].Text, "in-the-panel") || view[0].Pid == pid {
		t.Errorf("the terminal left in the panel is the dragged one: pid %d\n%s", view[0].Pid, view[0].Text)
	}
	if w := a.Workbench(); len(w.Groups) != 1 || len(w.Groups[0].Tabs) != 2 {
		t.Errorf("the editor groups: %+v", w.Groups)
	}
	// it has the keyboard, and the shell in it works
	a.Eventually("the keyboard in the terminal of the tab", func() (bool, string) {
		in, ok := a.Focused()
		return terminalsIn(a, "editor")[0].Focused, fmt.Sprintf("%+v; focused input %+v %v; %+v", terminalsIn(a, "editor"), in, ok, a.Workbench())
	})
	a.Type("echo in-the-tab")
	a.Press("Return")
	a.Eventually("the shell of the tab to answer", func() (bool, string) {
		got := terminalsIn(a, "editor")[0].Text
		return hasLine(got, "in-the-tab"), got
	})
	// Ctrl+P is Quick Open from a terminal in the editor area too
	a.Press("ctrl+p")
	a.Eventually("Quick Open over the terminal of the tab", func() (bool, string) { _, ok := a.Focused(); return ok, "" })
	a.Press("Escape")

	// back to the panel with its content: the same process
	a.Eventually("the keyboard in the terminal of the tab again", func() (bool, string) {
		l := terminalsIn(a, "editor")
		return len(l) == 1 && l[0].Focused, ""
	})
	runCommandOf(t, a, "Move Terminal into Panel")
	view, _ = waitLocated(t, a, 1, 0)
	if view[0].Pid != pid || !hasLine(view[0].Text, "in-the-panel") || !hasLine(view[0].Text, "in-the-tab") {
		t.Errorf("the terminal moved back has pid %d (was %d) and shows:\n%s", view[0].Pid, pid, view[0].Text)
	}
	if w := a.Workbench(); len(w.Groups) != 1 || len(w.Groups[0].Tabs) != 1 {
		t.Errorf("the editor tab of the terminal is still there: %+v", w.Groups)
	}
	a.Eventually("the keyboard in the terminal back in the panel", func() (bool, string) {
		l := terminalsIn(a, "view")
		return len(l) == 1 && l[0].Focused, fmt.Sprintf("%+v; editor %q", l, a.Editor().Text)
	})
	a.Type("echo back-in-the-panel")
	a.Press("Return")
	waitTermText(t, a, 0, "back-in-the-panel\n$")
}

func TestRealInputTerminalMovesToTheEditorAreaWithItsCommandAndEndsWithItsTab(t *testing.T) {
	a := startTerminalApp(t, nil)
	runCommandOf(t, a, "Move Terminal into Editor Area")
	_, editor := waitLocated(t, a, 0, 1)
	if w := a.Workbench(); w.PanelVisible {
		t.Errorf("the panel stays open with no terminal in it")
	}
	a.Eventually("the keyboard in the terminal of the tab", func() (bool, string) { return terminalsIn(a, "editor")[0].Focused, "" })
	a.Type("echo only-in-the-tab")
	a.Press("Return")
	a.Eventually("the shell to answer", func() (bool, string) {
		got := terminalsIn(a, "editor")[0].Text
		return hasLine(got, "only-in-the-tab"), got
	})
	_ = editor
	// Ctrl+W in the terminal of a tab closes the tab and ends its shell (Kill the Active Terminal in Editor Area)
	a.Press("ctrl+w")
	a.Eventually("the tab to close", func() (bool, string) {
		w := a.Workbench()
		return len(terminalsIn(a, "editor")) == 0 && len(w.Groups) == 1 && len(w.Groups[0].Tabs) == 1, a.Dump()
	})
}

func TestRealInputChangeColorMakesTheTabRed(t *testing.T) {
	a := startTerminalApp(t, nil)
	a.Press("ctrl+shift+grave")
	waitTerminals(t, a, 1)
	waitPrompt(t, a, 0)
	tm := a.Terminals()[0]
	// the icon of the active row (the second) in the tabs list, before and after
	x, y := tabRowOf(tm, 1)
	ix := tm.X + tm.Width + 1 + 8 + 7
	if n := countColor(t, a, ix, y, 9, reddish); n != 0 {
		t.Fatalf("%d red pixels in the icon before the change", n)
	}
	runCommandOf(t, a, "Change Color")
	a.WaitFocusedText("")
	a.Type("Red")
	a.WaitFocusedText("Red")
	a.Press("Return")
	a.Settle()
	a.Eventually("the icon to turn red", func() (bool, string) {
		n := countColor(t, a, ix, y, 9, reddish)
		return n >= 8, "red pixels: " + itoa(n)
	})
	if got := a.Terminals()[0]; got.IconColor != "terminal.ansiRed" {
		t.Errorf("the terminal reports the color %q", got.IconColor)
	}
	_ = x
	// and Default takes it away again
	runCommandOf(t, a, "Change Color")
	a.WaitFocusedText("")
	a.Type("Default")
	a.WaitFocusedText("Default")
	a.Press("Return")
	a.Eventually("the icon to lose its color", func() (bool, string) { return countColor(t, a, ix, y, 9, reddish) == 0, "" })
}

func TestRealInputTerminalSessionsComeBackAfterQuitting(t *testing.T) {
	a := startTerminalApp(t, nil)
	a.Type("cd /tmp")
	a.Press("Return")
	a.Type("echo first-session")
	a.Press("Return")
	waitTermText(t, a, 0, "first-session\n$")
	// a split of the first: it starts where the first is
	a.Press("ctrl+shift+5")
	waitTerminals(t, a, 2)
	a.Eventually("the split's shell", func() (bool, string) {
		l := a.Terminals()
		return len(l) == 2 && l[1].Focused && strings.Contains(l[1].Text, "$"), termText(a, 1)
	})
	a.Type("echo split-session")
	a.Press("Return")
	waitTermText(t, a, 1, "split-session\n$")
	// and a second tab in the list
	a.Press("ctrl+shift+grave")
	a.Eventually("the third terminal", func() (bool, string) {
		l := a.Terminals()
		return len(l) == 1 && l[0].Focused && strings.Contains(l[0].Text, "$") && !hasLine(l[0].Text, "first-session"), termText(a, 0)
	})
	a.Type("echo third-session")
	a.Press("Return")
	waitTermText(t, a, 0, "third-session\n$")
	oldPids := map[int]bool{}
	a.Press("ctrl+pageup") // the tab with the split
	list := waitTerminals(t, a, 2)
	for _, tm := range list {
		oldPids[tm.Pid] = true
	}

	a.Quit()
	stateFiles := func() string {
		var b strings.Builder
		_ = filepath.WalkDir(filepath.Join(filepath.Dir(a.Project), "state"), func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				fi, _ := d.Info()
				fmt.Fprintf(&b, "%s %d bytes\n", p, fi.Size())
			}
			return nil
		})
		return b.String()
	}
	t.Logf("state files after quitting:\n%s", stateFiles())
	a.Restart()
	// the panel comes back as it was with the shells new
	a.Eventually("terminals on screen after the restart", func() (bool, string) {
		return len(a.Terminals()) == 2, fmt.Sprintf("%d terminals; %+v; state files:\n%s", len(a.Terminals()), a.Workbench(), stateFiles())
	})
	list = waitTerminals(t, a, 2)
	for i, want := range []string{"first-session", "split-session"} {
		if oldPids[list[i].Pid] {
			t.Errorf("terminal %d is the old process %d", i, list[i].Pid)
		}
		waitTermText(t, a, i, want)
		waitTermText(t, a, i, "History restored")
		waitTermText(t, a, i, "History restored\n$") // the prompt of the new shell follows the line
	}
	// a new shell in the same folder: pwd answers /tmp in both
	for i := range 2 {
		x, y := cellPoint(a.Terminals()[i], 2, 1)
		a.Click(x, y)
		a.Eventually("the keyboard in the terminal", func() (bool, string) { return a.Terminals()[i].Focused, "" })
		a.Type("pwd")
		a.Press("Return")
		a.Eventually("the folder of the new shell", func() (bool, string) {
			got := termText(a, i)
			_, after, _ := strings.Cut(got, "History restored")
			return hasLine(after, "/tmp"), got
		})
	}
}

// TestRealInputProofTerminalEditor is the proof image of step 17d.7: a terminal that was dragged from the tabs list into the editor
// area, beside a file, and after Emetos was closed and started again, the two terminals back with their content and a new shell.
func TestRealInputProofTerminalEditor(t *testing.T) {
	a := startTerminalApp(t, nil)
	a.Type("echo kept-in-the-panel")
	a.Press("Return")
	waitTermText(t, a, 0, "kept-in-the-panel\n$")
	a.Press("ctrl+shift+grave")
	a.Eventually("the second terminal", func() (bool, string) {
		l := a.Terminals()
		return len(l) == 1 && l[0].Focused && !hasLine(l[0].Text, "kept-in-the-panel"), termText(a, 0)
	})
	a.Type("echo kept-in-the-tab")
	a.Press("Return")
	waitTermText(t, a, 0, "kept-in-the-tab\n$")
	panel := a.Terminals()[0]
	// the second row of the tabs list, dragged out of it onto the right of the editor area: a group of its own beside app.go
	x, y := tabRowOf(panel, 1)
	ex, ey := editorAreaPoint(panel, 0.97)
	a.Drag(x, y, ex, ey)
	waitLocated(t, a, 1, 1)
	a.Eventually("the terminal in the editor area", func() (bool, string) {
		l := terminalsIn(a, "editor")
		return len(l) == 1 && hasLine(l[0].Text, "kept-in-the-tab"), a.Dump()
	})

	t.Logf("before quitting: %+v", a.Workbench().Groups)
	a.Quit()
	saved := windowStateFile(a)
	a.Restart(".") // the folder only: the file of the first start comes back with the window state
	a.Eventually("the terminals in the panel and in the editor area after the restart", func() (bool, string) {
		return len(terminalsIn(a, "view")) == 1 && len(terminalsIn(a, "editor")) == 1, fmt.Sprintf("%d in the panel, %d in the editor\n%+v\nsaved window state: %s", len(terminalsIn(a, "view")), len(terminalsIn(a, "editor")), a.Workbench(), saved)
	})
	for _, loc := range []string{"view", "editor"} {
		a.Eventually("the restored terminal of the "+loc, func() (bool, string) {
			l := terminalsIn(a, loc)
			if len(l) != 1 {
				return false, "terminals: " + itoa(len(l))
			}
			return strings.Contains(l[0].Text, "History restored") && strings.HasSuffix(strings.TrimSpace(l[0].Text), "$"), l[0].Text
		})
	}
	if w := a.Workbench(); len(w.Groups) != 2 {
		t.Errorf("the editor groups after the restart: %+v", w.Groups)
	}
	a.Screenshot(filepath.Join(proofDir(t), "17d.7-terminal-editor.png"))
}

// windowStateFile is the content of the file of the window state in the state folder of the run, without the saved lines of the
// terminals (they are long): the structure is what a test reads in it.
func windowStateFile(a *display.App) string {
	var out string
	_ = filepath.WalkDir(filepath.Join(filepath.Dir(a.Project), "state"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(p, "windows") {
			data, _ := os.ReadFile(p)
			out = regexp.MustCompile(`"text":"[^"]*"`).ReplaceAllString(string(data), `"text":"..."`)
		}
		return nil
	})
	return out
}
