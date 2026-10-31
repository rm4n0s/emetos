//go:build emetos_display

package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The multi-file diff editor of the Git extension as a user meets it (step 24.17, SPEC §10.30 "Diffs of changes"): Open Changes
// opens the files of the work tree in one scrolling column, the wheel moves it from the first file to the last, and the sections
// that come into view build their diffs.

const multiDiffFiles = 12

func multiDiffName(i int) string { return fmt.Sprintf("file%02d.txt", i) }

func multiDiffBody(i int, changed bool) string {
	var b strings.Builder
	for line := 1; line <= 60; line++ {
		if changed && line == 30 {
			fmt.Fprintf(&b, "file %d line %d CHANGED\n", i, line)
			continue
		}
		fmt.Fprintf(&b, "file %d line %d\n", i, line)
	}
	return b.String()
}

func TestRealInputMultiDiffScrollsFromTheFirstFileToTheLast(t *testing.T) {
	gittest.Require(t)
	e := gittest.NewEnv(t)
	var repo *gittest.Repo
	prepare := func(dir string) {
		r := e.InitAt(dir)
		repo = r
		for i := 0; i < multiDiffFiles; i++ {
			r.Write(multiDiffName(i), multiDiffBody(i, false))
		}
		r.Commit("first", ".")
		for i := 0; i < multiDiffFiles; i++ {
			r.Write(multiDiffName(i), multiDiffBody(i, true))
		}
	}
	a := display.Start(t, display.Options{
		Args:    []string{"."},
		Prepare: prepare,
		// the folder of the run is below the temporary folder: its repository is trusted for the session (SPEC §15.2)
		Env: []string{"EMETOS_DEV_GIT_TRUST=" + os.TempDir()},
	})
	a.Settle()
	a.Dev("git.viewChanges", nil)
	a.Eventually("the multi-file diff editor to open", func() (bool, string) {
		return strings.Contains(activeTab(a), fmt.Sprintf("(%d files)", multiDiffFiles)), joinNames(tabNames(a))
	})
	// the Explorer lists the files too: only the texts of the editor area count
	editor := inRect(350, 60, 1280, 780)
	onScreen := func(text string) bool {
		_, ok := textWhere(a, text, editor)
		return ok
	}
	waitFor := func(what, text string) {
		a.Eventually(what, func() (bool, string) { return onScreen(text), a.Dump() })
	}
	waitFor("the first file's header", multiDiffName(0))
	a.Settle()
	shot(t, a, "multidiff-top")
	if onScreen(multiDiffName(multiDiffFiles - 1)) {
		t.Fatalf("the last file is on screen before any scrolling:\n%s", a.Dump())
	}

	// the wheel over the column moves it, whichever diff editor is under the pointer
	last := multiDiffName(multiDiffFiles - 1)
	for i := 0; i < 400 && !onScreen(last); i++ {
		a.Scroll(900, 450, 0, 10)
		a.Settle()
	}
	waitFor("the last file's header", last)
	shot(t, a, "multidiff-bottom")
	if onScreen(multiDiffName(0)) {
		t.Errorf("the first file is still on screen at the end of the column:\n%s", a.Dump())
	}

	// and back to the top
	for i := 0; i < 400 && !onScreen(multiDiffName(0)); i++ {
		a.Scroll(900, 450, 0, -10)
		a.Settle()
	}
	waitFor("the first file's header again", multiDiffName(0))

	// the stage action of a header: a click on its button stages that file, which git says (the buttons are at the right end of the
	// header: Open File, Stage Changes and then the letter)
	staged := func() string { return strings.TrimSpace(repo.Git("diff", "--cached", "--name-only")) }
	if staged() != "" {
		t.Fatalf("something is staged before the click: %s", staged())
	}
	header, _ := textWhere(a, multiDiffName(0), editor)
	a.Click(1244, header.Y+header.Height/2)
	a.Eventually("git to have the file in its index", func() (bool, string) { return staged() == multiDiffName(0), staged() })
	shot(t, a, "multidiff-staged")
}
