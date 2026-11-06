//go:build emetos_display

package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/tutor"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The Emetos Tutor with real keys (step 30.2, SPEC §10.35): three exercises done the way a person does them.

// tutorLine is the line, counted from 1, of the first line of the lessons in the editor that starts with prefix. It reads
// the text as it is now: an exercise done before moves the lines below it.
func tutorLine(t *testing.T, a *display.App, prefix string) int {
	t.Helper()
	for i, line := range strings.Split(editorText(a), "\n") {
		if strings.HasPrefix(line, prefix) {
			return i + 1
		}
	}
	t.Fatalf("no line of the lessons starts with %q", prefix)
	return 0
}

// lineOf is the line of the editor's text that starts with prefix, for the message of a failure.
func lineOf(a *display.App, prefix string) string {
	for _, line := range strings.Split(editorText(a), "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return "(no line starts with " + prefix + ")"
}

// goToLine goes to a line with Go to Line (Ctrl+G), as lesson 1 teaches.
func goToLine(a *display.App, line int) {
	a.Press("ctrl+g")
	a.Type(strconv.Itoa(line))
	a.WaitFocusedText(":" + strconv.Itoa(line))
	a.Press("Return")
	a.Eventually("the cursor to reach the line", func() (bool, string) {
		ed := a.Editor()
		return ed.Line == line, "line " + strconv.Itoa(ed.Line)
	})
}

func TestRealInputTheTutorsExercises(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{"--tutor"}})
	a.Eventually("the tutor to open", func() (bool, string) {
		ed := a.Editor()
		return filepath.Base(ed.Path) == tutor.MainFile, ed.Path
	})
	a.Settle()

	// lesson 2: delete the line that does not belong
	goToLine(a, tutorLine(t, a, "---> this line does not belong"))
	a.Press("ctrl+shift+k")
	a.Eventually("the line to be deleted", func() (bool, string) {
		text := editorText(a)
		return !strings.Contains(text, "---> this line does not belong") && strings.Contains(text, "---> one\n---> two\n"), ""
	})

	// lesson 4: every "dog" becomes "cat" with Ctrl+D
	goToLine(a, tutorLine(t, a, "---> dog, dog and one more dog"))
	a.Press("Home")
	for range len("---> ") {
		a.Press("Right")
	}
	a.Press("ctrl+d", "ctrl+d", "ctrl+d")
	a.Type("cat")
	a.Eventually("the three words to change", func() (bool, string) {
		return strings.Contains(editorText(a), "---> cat, cat and one more cat\n"), lineOf(a, "---> ")
	})

	// lesson 6: Quick Open finds the fruits
	a.Press("ctrl+p")
	a.Type("fru")
	a.WaitFocusedText("fru")
	a.Press("Return")
	a.Eventually("fruits.txt to open", func() (bool, string) {
		ed := a.Editor()
		return filepath.Base(ed.Path) == "fruits.txt" && strings.Contains(ed.Text, "banana"), ed.Path
	})
}
