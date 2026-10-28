//go:build emetos_display

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Real-input tests of step 17d.6: terminal IntelliSense with the test's own bash. The programs the commands name are in a folder
// of the test that is the whole PATH: git is a fake script, so that the real git is never run, and nothing of the machine decides
// what is offered. The shell starts in an empty home folder, so that no rc file of anybody is read.

// suggestApp opens the editor with the terminal, with the quick suggestions in the settings (nil is the default: all off), and
// waits until the shell integration is active. The project folder has the files of the project and the shell starts in it.
func suggestApp(t *testing.T, quick map[string]string, extra map[string]string) *display.App {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	bin := t.TempDir()
	for _, name := range []string{"git", "gitk", "cat", "grep"} {
		script := "#!/bin/sh\necho fake " + name + " \"$@\"\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// the scripts of the shell integration call these
	for _, name := range []string{"uname", "sed"} {
		if real, err := exec.LookPath(name); err == nil {
			if err := os.Symlink(real, filepath.Join(bin, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	settings := map[string]any{
		"terminal.integrated.defaultProfile.linux": "bash",
		"terminal.integrated.profiles.linux":       map[string]any{"bash": map[string]any{"path": bash}},
		"terminal.integrated.env.linux":            map[string]any{"PS1": "$ ", "HISTFILE": "/dev/null"},
	}
	if quick != nil {
		settings["terminal.integrated.suggest.quickSuggestions"] = quick
	}
	text, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"app.go": "package main\n", "a.txt": "a\n", "b.txt": "b\n", "sub/c.go": "package sub\n"}
	for k, v := range extra {
		files[k] = v
	}
	a := display.Start(t, display.Options{
		Args: []string{".", "app.go"}, Project: files, Config: map[string]string{"settings.json": string(text)}, Env: []string{"PATH=" + bin},
	})
	a.Settle()
	a.Press("ctrl+grave")
	waitTerminals(t, a, 1)
	a.Eventually("shell integration to be active", func() (bool, string) {
		tm := a.Terminals()
		return len(tm) == 1 && tm[0].ShellIntegration, termText(a, 0)
	})
	return a
}

var suggestQuickOn = map[string]string{"commands": "on", "arguments": "on", "unknown": "off"}

// waitSuggest waits until the list is shown and has the row; it returns the terminal.
func waitSuggest(t *testing.T, a *display.App, row string) display.Terminal {
	t.Helper()
	var tm display.Terminal
	a.Eventually("the suggest list with "+row, func() (bool, string) {
		tm = a.Terminals()[0]
		return tm.Suggest && slices.Contains(tm.SuggestRows, row), strings.Join(tm.SuggestRows, ",") + "\n" + tm.Text
	})
	return tm
}

// waitNoSuggest waits until the list is closed.
func waitNoSuggest(t *testing.T, a *display.App) {
	t.Helper()
	a.Eventually("the suggest list to close", func() (bool, string) {
		tm := a.Terminals()[0]
		return !tm.Suggest, strings.Join(tm.SuggestRows, ",")
	})
}

// lastRow is the last row of the screen that has text.
func lastRow(tm display.Terminal) string {
	rows := strings.Split(strings.TrimRight(tm.Text, "\n"), "\n")
	return rows[len(rows)-1]
}

func TestRealInputSuggestTypingOffersTheCommandAndTabCompletesIt(t *testing.T) {
	a := suggestApp(t, suggestQuickOn, nil)
	a.Type("gi")
	tm := waitSuggest(t, a, "git")
	if tm.SuggestRows[0] != "git" || tm.SuggestFocus != 0 {
		t.Errorf("the rows are %v with %d selected", tm.SuggestRows, tm.SuggestFocus)
	}
	a.Press("Tab")
	a.Eventually("git on the line", func() (bool, string) {
		tm := a.Terminals()[0]
		return lastRow(tm) == "$ git" && !tm.Suggest, tm.Text
	})
	a.Press("Return")
	waitTermText(t, a, 0, "fake git")
}

func TestRealInputSuggestGitCheOffersCheckout(t *testing.T) {
	a := suggestApp(t, suggestQuickOn, nil)
	a.Type("git che")
	tm := waitSuggest(t, a, "checkout")
	if tm.SuggestRows[0] != "checkout" {
		t.Errorf("the rows are %v", tm.SuggestRows)
	}
	// the list is under the cursor's row, at the start of the word
	cursorRow := float64(len(strings.Split(strings.TrimRight(tm.Text, "\n"), "\n")) - 1)
	if want := (cursorRow + 1) * tm.CellH; tm.SuggestY < want-1 || tm.SuggestY > want+1 {
		t.Errorf("the list is at y=%v, want just under the row of the prompt (%v)", tm.SuggestY, want)
	}
	if want := tm.PadLeft + float64(len("$ git "))*tm.CellW; tm.SuggestX < want-1 || tm.SuggestX > want+1 {
		t.Errorf("the list is at x=%v, want the start of the word (%v)", tm.SuggestX, want)
	}
	a.Press("Tab")
	a.Eventually("git checkout on the line", func() (bool, string) {
		tm := a.Terminals()[0]
		return lastRow(tm) == "$ git checkout" && !tm.Suggest, tm.Text
	})
	a.Press("Return")
	waitTermText(t, a, 0, "fake git checkout")
}

func TestRealInputSuggestCatListsTheFilesOfTheFolder(t *testing.T) {
	a := suggestApp(t, nil, nil) // the defaults: nothing opens by itself
	a.Type("cat ")
	waitTermText(t, a, 0, "$ cat")
	a.Settle()
	a.Press("ctrl+space")
	tm := waitSuggest(t, a, "./a.txt")
	for _, want := range []string{"./b.txt", "./sub/", "./app.go", "."} {
		if !slices.Contains(tm.SuggestRows, want) && tm.SuggestTotal <= len(tm.SuggestRows) {
			t.Errorf("%q is not in the list %v", want, tm.SuggestRows)
		}
	}
	// typing narrows it, and Tab completes the file
	a.Type("b")
	a.Eventually("the list for b", func() (bool, string) {
		tm := a.Terminals()[0]
		return tm.Suggest && len(tm.SuggestRows) > 0 && tm.SuggestRows[0] == "./b.txt", strings.Join(tm.SuggestRows, ",")
	})
	a.Press("Tab")
	a.Eventually("the file on the line", func() (bool, string) {
		tm := a.Terminals()[0]
		return lastRow(tm) == "$ cat ./b.txt" && !tm.Suggest, tm.Text
	})
}

func TestRealInputSuggestNothingOpensByItselfWithTheDefaults(t *testing.T) {
	a := suggestApp(t, nil, nil)
	a.Type("gi")
	waitTermText(t, a, 0, "$ gi")
	a.Settle()
	if tm := a.Terminals()[0]; tm.Suggest {
		t.Fatalf("the list opened with quickSuggestions off: %v", tm.SuggestRows)
	}
	// Tab is the shell's own then: it completes nothing here (the only programs are not gi...), and no list is made
	a.Press("ctrl+space")
	waitSuggest(t, a, "git")
}

func TestRealInputSuggestEscapeClosesTheList(t *testing.T) {
	a := suggestApp(t, suggestQuickOn, nil)
	a.Type("gi")
	waitSuggest(t, a, "git")
	a.Press("Escape")
	waitNoSuggest(t, a)
	if got := lastRow(a.Terminals()[0]); got != "$ gi" {
		t.Errorf("the line is %q: Escape changes nothing", got)
	}
	// the key is the terminal's again: Escape does not reach the shell as a list command, and typing opens the list again
	a.Type("t")
	waitSuggest(t, a, "git")
}

func TestRealInputSuggestDownThenEnterAcceptsARow(t *testing.T) {
	a := suggestApp(t, suggestQuickOn, nil)
	a.Type("gi")
	tm := waitSuggest(t, a, "gitk")
	second := tm.SuggestRows[1]
	a.Press("Down")
	a.Eventually("the second row selected", func() (bool, string) {
		tm := a.Terminals()[0]
		return tm.Suggest && tm.SuggestFocus == 1, tm.Text
	})
	a.Press("Return")
	a.Eventually("the second row on the line", func() (bool, string) {
		tm := a.Terminals()[0]
		return lastRow(tm) == "$ "+second && !tm.Suggest, tm.Text
	})
}

// TestRealInputSuggestProof makes the proof image of step 17d.6: the list over git che.
func TestRealInputSuggestProof(t *testing.T) {
	a := suggestApp(t, suggestQuickOn, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	runCommand(t, a, "cat main.go", 1)
	a.Type("git che")
	waitSuggest(t, a, "checkout")
	a.Settle()
	a.Screenshot(filepath.Join(proofDir(t), "17d.6-suggest.png"))
}
