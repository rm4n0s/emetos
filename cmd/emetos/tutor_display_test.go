//go:build emetos_display

package main

import (
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/assets"
	"github.com/rm4n0s/emetos/internal/core/tutor"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The Emetos Tutor with real keys (step 30.2, SPEC §10.35): three exercises done the way a person does them.

// tutorLine is the line, counted from 1, of the first line of the lessons that starts with prefix.
func tutorLine(t *testing.T, prefix string) int {
	t.Helper()
	b, err := fs.ReadFile(assets.Tutor(), assets.TutorRoot+"/"+tutor.MainFile)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, prefix) {
			return i + 1
		}
	}
	t.Fatalf("no line of the lessons starts with %q", prefix)
	return 0
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
	goToLine(a, tutorLine(t, "---> this line does not belong"))
	a.Press("ctrl+shift+k")
	a.Eventually("the line to be deleted", func() (bool, string) {
		text := editorText(a)
		return !strings.Contains(text, "---> this line does not belong") && strings.Contains(text, "---> one\n---> two\n"), ""
	})

	// lesson 4: every "dog" becomes "cat" with Ctrl+D
	goToLine(a, tutorLine(t, "---> dog, dog and one more dog"))
	a.Press("Home")
	for range len("---> ") {
		a.Press("Right")
	}
	a.Press("ctrl+d", "ctrl+d", "ctrl+d")
	a.Type("cat")
	a.Eventually("the three words to change", func() (bool, string) {
		text := editorText(a)
		return strings.Contains(text, "---> cat, cat and one more cat\n"), ""
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
