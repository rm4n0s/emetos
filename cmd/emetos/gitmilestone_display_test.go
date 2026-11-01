//go:build emetos_display

package main

import (
	"image"
	"image/color"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The whole of the milestone, as a user meets it (step 24.23, SPEC §10.30, §15.1): a real repository in a temporary folder, a file
// edited with the keys, the quick diff mark in the gutter, the file staged from the palette, the commit made with the box of the Source
// Control view and Ctrl+Enter, the Graph and the Timeline showing the new commit, and the blame of the line in the status bar. The
// repository, its configuration and its author are the test's own; the editor runs with a HOME of its own.

const milestoneFile = `package loader

import "os"

func Load(path string) ([]byte, error) {
	retries := 1
	clean := path
	data, err := os.ReadFile(clean)
	if err != nil {
		return nil, err
	}
	_ = retries
	return data, err
}
`

// gutterColors lists the colors of the pixels of a column of a screenshot between two rows.
func gutterColors(t *testing.T, path string, x, y0, y1 int) map[color.RGBA]bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	out := map[color.RGBA]bool{}
	for y := y0; y < y1; y++ {
		r, g, b, a := img.At(x, y).RGBA()
		out[color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}] = true
	}
	return out
}

func TestRealInputTheWholeOfGit(t *testing.T) {
	gittest.Require(t)
	e := gittest.NewEnv(t)
	var repo *gittest.Repo
	a := display.Start(t, display.Options{
		Args: []string{".", "loader.go"},
		Prepare: func(dir string) {
			repo = e.InitAt(dir)
			repo.Git("config", "user.name", gittest.AuthorName)
			repo.Git("config", "user.email", gittest.AuthorEmail)
			repo.Write("loader.go", milestoneFile)
			repo.Commit("Add the loader")
		},
		Env: []string{"EMETOS_DEV_GIT_TRUST=" + os.TempDir(), "GIT_CONFIG_NOSYSTEM=1"},
	})
	a.Settle()
	const stripeX = 432 // the gutter of the quick diff, as in the test of the peek
	shotPath := filepath.Join(t.TempDir(), "gutter.png")
	gutter := func() map[color.RGBA]bool {
		a.Screenshot(shotPath)
		return gutterColors(t, shotPath, stripeX, 100, 400)
	}
	before := gutter()

	// edit: a line is changed with the keys, and the gutter gets a stripe
	a.Press("ctrl+Home")
	a.Press("Down", "Down", "Down", "Down", "Down")
	a.Press("End")
	a.Type(" // edited")
	a.Eventually("the stripe in the gutter", func() (bool, string) {
		for c := range gutter() {
			if !before[c] {
				return true, ""
			}
		}
		return false, "the colors of the gutter did not change"
	})
	shot(t, a, "git-milestone-quickdiff")
	a.Press("ctrl+s")
	a.Eventually("the file on disk", func() (bool, string) {
		return strings.Contains(repo.Read("loader.go"), "// edited"), repo.Read("loader.go")
	})

	// the Source Control view lists the change; Stage All Changes puts it in the index
	a.Press("ctrl+shift+g")
	a.WaitText("Changes")
	a.Press("ctrl+shift+p")
	a.Type("Git: Stage All Changes")
	a.WaitText("Git: Stage All Changes")
	a.Press("Enter")
	a.WaitText("Staged Changes")
	if got := repo.Git("diff", "--cached", "--name-only"); got != "loader.go" {
		t.Errorf("the index has %q", got)
	}

	// commit with the box
	var box app.DevInput
	a.Eventually("the commit box", func() (bool, string) {
		in, ok := commitBox(a)
		box = in
		return ok, a.Dump()
	})
	a.ClickInput(box)
	a.Eventually("the click to give the box the keyboard", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.X < 350, a.FocusedText()
	})
	const message = "Mark the edit of the loader"
	a.Type(message)
	a.WaitFocusedText(message)
	a.Press("ctrl+Enter")
	a.Eventually("the commit", func() (bool, string) { return repo.Git("rev-list", "--count", "HEAD") == "2", a.Dump() })
	if got := repo.Git("log", "-1", "--format=%s"); got != message {
		t.Errorf("the subject is %q", got)
	}
	a.WaitNoText("Staged Changes")

	// the Graph (second view of the container) shows the commit
	waitTextStartingWith(a, message)
	shot(t, a, "git-milestone-graph")

	// the Timeline (a folded view of the Explorer) lists it for the file in front
	a.Press("ctrl+shift+e")
	a.Eventually("the Timeline header", func() (bool, string) {
		_, ok := a.Text("TIMELINE")
		return ok, a.Dump()
	})
	a.ClickText("TIMELINE")
	waitTextStartingWith(a, message)
	shot(t, a, "git-milestone-timeline")

	// blame: the status bar names the author of the line the cursor is on
	a.Press("ctrl+1")
	a.Eventually("the author in the status bar", func() (bool, string) {
		for _, tx := range a.Texts() {
			if strings.Contains(tx.Text, gittest.AuthorName) && tx.Y > 600 {
				return true, ""
			}
		}
		return false, a.Dump()
	})
	a.Quit()
}

// waitTextStartingWith waits until a run of text on screen starts with prefix (a row of the Graph holds the subject and the author).
func waitTextStartingWith(a *display.App, prefix string) {
	a.Eventually("a text starting with "+prefix, func() (bool, string) {
		for _, tx := range a.Texts() {
			if strings.HasPrefix(tx.Text, prefix) {
				return true, ""
			}
		}
		return false, a.Dump()
	})
}
