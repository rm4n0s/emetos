//go:build emetos_display

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Real-input tests of the terminal (step 17d.2): the keys and the pointer go through the compositor, and the screen of the
// terminal is read through dev.terminals, because its glyphs are drawn from an atlas and no text widget holds them. Every
// shell of these tests is bash with --norc --noprofile and the home folder of the run, so nothing of the user's is read.

// terminalConfig is the configuration folder of the run: bash without rc files and a short prompt.
var terminalConfig = map[string]string{
	"settings.json": `{
		"terminal.integrated.defaultProfile.linux": "bash",
		"terminal.integrated.profiles.linux": {"bash": {"path": "bash", "args": ["--norc", "--noprofile"]}},
		"terminal.integrated.env.linux": {"PS1": "$ ", "HISTFILE": "/dev/null"}
	}`,
}

// startTerminalApp starts the editor on a project with a few files and opens the terminal with Ctrl+`.
func startTerminalApp(t *testing.T, files map[string]string) *display.App {
	t.Helper()
	if files == nil {
		files = map[string]string{}
	}
	files["app.go"] = "package main\n"
	a := display.Start(t, display.Options{Args: []string{".", "app.go"}, Project: files, Config: terminalConfig})
	a.Settle()
	a.Press("ctrl+grave")
	waitTerminals(t, a, 1)
	waitPrompt(t, a, 0)
	return a
}

// waitTerminals waits until n terminals are on screen.
func waitTerminals(t *testing.T, a *display.App, n int) []display.Terminal {
	t.Helper()
	var list []display.Terminal
	a.Eventually("terminals on screen", func() (bool, string) {
		list = a.Terminals()
		return len(list) == n, "terminals on screen: " + itoa(len(list))
	})
	return list
}

// termText is the text on the screen of the i-th terminal (from the left).
func termText(a *display.App, i int) string {
	list := a.Terminals()
	if i >= len(list) {
		return ""
	}
	return list[i].Text
}

// waitTermText waits until the text on the screen of the i-th terminal has want in it.
func waitTermText(t *testing.T, a *display.App, i int, want string) {
	t.Helper()
	a.Eventually("the terminal to show "+want, func() (bool, string) {
		got := termText(a, i)
		return strings.Contains(got, want), got
	})
}

// waitPrompt waits until the i-th terminal shows the prompt of the shell.
func waitPrompt(t *testing.T, a *display.App, i int) {
	t.Helper()
	a.Eventually("the shell's prompt", func() (bool, string) {
		got := termText(a, i)
		return strings.Contains(got, "$"), got
	})
}

// hasLine tells whether a line of the text is exactly want.
func hasLine(text, want string) bool {
	for _, l := range strings.Split(text, "\n") {
		if l == want {
			return true
		}
	}
	return false
}

func TestRealInputTerminalOpensAndRunsACommand(t *testing.T) {
	a := startTerminalApp(t, nil)
	list := a.Terminals()
	if !list[0].Focused {
		t.Errorf("the terminal opened with Ctrl+` has no focus: %+v", list[0])
	}
	if w := a.Workbench(); !w.PanelVisible || !strings.Contains(w.ActivePanel, "terminal") {
		t.Errorf("the panel shows %q (visible %v)", w.ActivePanel, w.PanelVisible)
	}
	a.Type("echo hello")
	a.Press("Return")
	a.Eventually("the output of echo", func() (bool, string) {
		got := termText(a, 0)
		return hasLine(got, "hello"), got
	})
	// Ctrl+` with the terminal focused hides the panel, and shows it again with the same shell
	pid := a.Terminals()[0].Pid
	a.Press("ctrl+grave")
	a.Eventually("the panel to hide", func() (bool, string) { return !a.Workbench().PanelVisible, "" })
	a.Press("ctrl+grave")
	list = waitTerminals(t, a, 1)
	if list[0].Pid != pid || !hasLine(list[0].Text, "hello") {
		t.Errorf("the terminal after hiding and showing the panel: pid %d (was %d)\n%s", list[0].Pid, pid, list[0].Text)
	}
}

// cellPoint is the middle of a cell of a terminal on screen, in window coordinates.
func cellPoint(tm display.Terminal, col, row int) (x, y float64) {
	return tm.X + tm.PadLeft + (float64(col)+0.5)*tm.CellW, tm.Y + (float64(row)+0.5)*tm.CellH
}

func TestRealInputTerminalCopyAndPaste(t *testing.T) {
	a := startTerminalApp(t, nil)
	a.Type("echo copyme")
	a.Press("Return")
	a.Eventually("the output", func() (bool, string) { return hasLine(termText(a, 0), "copyme"), termText(a, 0) })
	tm := a.Terminals()[0]
	// the output line, not the command line: the line that is exactly the word
	row := -1
	for r, line := range strings.Split(tm.Text, "\n") {
		if line == "copyme" {
			row = r
		}
	}
	if row < 0 {
		t.Fatalf("no output line in\n%s", tm.Text)
	}
	x, y := cellPoint(tm, 2, row)
	a.DoubleClick(x, y)
	a.Eventually("the word to be selected", func() (bool, string) {
		s := a.Terminals()[0].Selected
		return s == "copyme", s
	})
	a.Press("ctrl+shift+c")
	a.Settle()
	a.Press("ctrl+shift+v")
	a.Eventually("the pasted text at the prompt", func() (bool, string) {
		got := termText(a, 0)
		return hasLine(got, "$ copyme"), got
	})
	a.Press("Return")
	a.Eventually("the shell to run the pasted word", func() (bool, string) {
		got := termText(a, 0)
		return strings.Contains(got, "copyme: command not found"), got
	})
}

func TestRealInputTerminalWheelScrollsBack(t *testing.T) {
	a := startTerminalApp(t, nil)
	a.Type("seq 1 300")
	a.Press("Return")
	a.Eventually("the last number", func() (bool, string) { return hasLine(termText(a, 0), "300"), termText(a, 0) })
	tm := a.Terminals()[0]
	if tm.Scrollback < 200 || tm.Scroll != 0 {
		t.Fatalf("scrollback %d, scroll %d before the wheel", tm.Scrollback, tm.Scroll)
	}
	a.Scroll(tm.X+tm.Width/2, tm.Y+tm.Height/2, 0, -4)
	a.Eventually("the view to go back", func() (bool, string) {
		s := a.Terminals()[0]
		return s.Scroll > 0 && !hasLine(s.Text, "300"), "scroll " + itoa(s.Scroll) + "\n" + s.Text
	})
	// a key goes to the bottom again
	a.Type("x")
	a.Eventually("the view to come back to the bottom", func() (bool, string) {
		s := a.Terminals()[0]
		return s.Scroll == 0 && hasLine(s.Text, "300"), "scroll " + itoa(s.Scroll)
	})
}

func TestRealInputTerminalCtrlLKeepsTheScrollback(t *testing.T) {
	a := startTerminalApp(t, nil)
	a.Type("echo before")
	a.Press("Return")
	a.Eventually("the output", func() (bool, string) { return hasLine(termText(a, 0), "before"), termText(a, 0) })
	a.Press("ctrl+l")
	a.Eventually("the screen to be cleared", func() (bool, string) {
		got := termText(a, 0)
		return !strings.Contains(got, "before"), got
	})
	tm := a.Terminals()[0]
	if tm.Scrollback == 0 {
		t.Fatalf("Ctrl+L left no scrollback: %+v", tm)
	}
	a.Scroll(tm.X+tm.Width/2, tm.Y+tm.Height/2, 0, -3)
	a.Eventually("the earlier output in the scrollback", func() (bool, string) {
		got := termText(a, 0)
		return hasLine(got, "before"), got
	})
}

func TestRealInputTerminalSecondTerminalAndSplit(t *testing.T) {
	a := startTerminalApp(t, nil)
	a.Press("ctrl+shift+grave")
	list := waitTerminals(t, a, 1) // the new terminal is a tab of its own: one on screen
	waitPrompt(t, a, 0)
	if !list[0].Focused {
		t.Errorf("the second terminal has no focus: %+v", list[0])
	}
	// the tabs list shows two rows now
	a.Eventually("two tabs in the list", func() (bool, string) {
		n := 0
		tm := a.Terminals()[0]
		for _, tx := range a.Texts() {
			if tx.Text == "bash" && tx.X > tm.X+tm.Width {
				n++
			}
		}
		return n == 2, itoa(n)
	})
	// a split puts a third terminal beside the second
	a.Type("echo second")
	a.Press("Return")
	waitTermText(t, a, 0, "second")
	a.Press("ctrl+shift+5")
	split := waitTerminals(t, a, 2)
	if split[0].Y != split[1].Y || split[0].X == split[1].X {
		t.Errorf("the split terminals are not side by side: %+v %+v", split[0], split[1])
	}
	a.Eventually("the new pane to have the focus", func() (bool, string) {
		l := a.Terminals()
		return len(l) == 2 && l[1].Focused && !l[0].Focused, "focus"
	})
	a.Type("echo third")
	a.Press("Return")
	waitTermText(t, a, 1, "third")
	if got := termText(a, 0); strings.Contains(got, "third") {
		t.Errorf("the output of the split went to the first pane:\n%s", got)
	}
	// Alt+Left moves the focus to the first pane
	a.Press("alt+Left")
	a.Eventually("the focus to move left", func() (bool, string) {
		l := a.Terminals()
		return len(l) == 2 && l[0].Focused, "focus"
	})
}

func TestRealInputTerminalCtrlPOpensQuickOpen(t *testing.T) {
	a := startTerminalApp(t, nil)
	a.Press("ctrl+p")
	a.Eventually("Quick Open to take the focus", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Text == "", "focused field: " + in.Text
	})
	a.Type("app")
	a.WaitFocusedText("app")
	a.Press("Escape")
	// the shell got no Ctrl+P: the prompt line is empty
	a.Eventually("the prompt to be untouched", func() (bool, string) {
		got := termText(a, 0)
		return strings.TrimSpace(got) == "$", got
	})
}

func TestRealInputTerminalExitEndsIt(t *testing.T) {
	a := startTerminalApp(t, nil)
	a.Type("exit")
	a.Press("Return")
	a.Eventually("the terminal to end", func() (bool, string) { return len(a.Terminals()) == 0, termText(a, 0) })
	a.Eventually("the panel to hide", func() (bool, string) { return !a.Workbench().PanelVisible, "" })
}

// TestRealInputTerminalProof makes the proof image of step 17d.2: a colored listing and a split, with real input.
func TestRealInputTerminalProof(t *testing.T) {
	files := map[string]string{
		"README.md": "# project\n", "main.go": "package main\n\nfunc main() {}\n", "notes.txt": "notes\n", "build.tar.gz": "x", "run.sh": "#!/bin/sh\necho hi\n",
		"src/server.go": "package src\n", "docs/guide.md": "# guide\n", "assets/logo.png": "png",
	}
	a := display.Start(t, display.Options{Args: []string{".", "main.go"}, Project: files, Config: terminalConfig,
		Prepare: func(dir string) {
			if err := os.Chmod(filepath.Join(dir, "run.sh"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("src/server.go", filepath.Join(dir, "link.go")); err != nil {
				t.Fatal(err)
			}
		}})
	a.Settle()
	a.Press("ctrl+grave")
	waitTerminals(t, a, 1)
	waitPrompt(t, a, 0)
	a.Type("ls --color=always -F")
	a.Press("Return")
	waitTermText(t, a, 0, "build.tar.gz")
	a.Press("ctrl+shift+5")
	waitTerminals(t, a, 2)
	waitPrompt(t, a, 1)
	a.Type("ls -1 --color=always")
	a.Press("Return")
	waitTermText(t, a, 1, "src")
	a.Press("ctrl+shift+grave")
	a.Eventually("the second tab", func() (bool, string) { return len(a.Terminals()) == 1, "" })
	waitPrompt(t, a, 0)
	a.Type("echo a terminal of its own")
	a.Press("Return")
	waitTermText(t, a, 0, "a terminal of its own")
	a.Press("alt+Left")
	a.Settle()
	// the tab of the split is in the list again: select it and go back
	clickTextWhere(t, a, "\u250c bash", inRect(1000, 480, 1280, 700))
	a.Eventually("the split to show again", func() (bool, string) { return len(a.Terminals()) == 2, "" })
	a.Settle()
	a.Screenshot(filepath.Join(proofDir(t), "17d.2-terminal.png"))
}
