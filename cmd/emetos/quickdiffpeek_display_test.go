//go:build emetos_display

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The peek of a change as a user meets it (step 24.12, SPEC §10.30 "The peek"): a real repository in a temporary folder, a file with two
// changes against the index, a click on a stripe of the gutter, a click on Stage Change and one on Revert Change. The git repository
// and its configuration are the test's own.

const peekFile = `package loader

import "os"

func Load(path string) ([]byte, error) {
	retries := 2
	clean := path
	data, err := os.ReadFile(clean)
	if err != nil {
		return nil, err
	}
	_ = retries
	return data, err
}

// load is the helper of Load.
func load(path string, retries int) []byte {
	for i := 0; i < retries; i++ {
		data, err := os.ReadFile(path)
		if err == nil {
			return data
		}
	}
	return nil
}
`

// peekTitle is the paragraph of the title bar of the peek, if it is on screen.
func peekTitle(a *display.App) (app.DevText, bool) {
	for _, tx := range a.Texts() {
		if strings.Contains(tx.Text, "Git Local Changes (Working Tree) - ") {
			return tx, true
		}
	}
	return app.DevText{}, false
}

func TestRealInputThePeekOfAChangeStagesAndReverts(t *testing.T) {
	gittest.Require(t)
	e := gittest.NewEnv(t)
	var repo *gittest.Repo
	a := display.Start(t, display.Options{
		Args: []string{".", "loader.go"},
		Prepare: func(dir string) {
			repo = e.InitAt(dir)
			repo.Write("loader.go", peekFile)
			repo.Commit("Add the loader")
			changed := strings.Replace(peekFile, "retries := 2", "retries := 3", 1)
			changed = strings.Replace(changed, "return data, err\n}\n\n// load", "return data, nil\n}\n\n// load", 1)
			repo.Write("loader.go", changed)
		},
		Env: []string{"EMETOS_DEV_GIT_TRUST=" + os.TempDir(), "GIT_CONFIG_NOSYSTEM=1"},
	})
	a.Settle()
	const (
		stripeX = 432.0
		// the buttons of the title bar, from the right of a window of 1280 pixels: close, previous, next, Revert Change, Stage Change
		stageX, revertX = 1064.0, 1087.0
	)
	opened := func() bool { _, ok := peekTitle(a); return ok }

	// the marks are there once Alt+F3 opens the peek; Escape closes it again
	a.Click(editorColumnX(10), editorLine(1))
	a.Eventually("the first peek, by the key", func() (bool, string) {
		if !opened() {
			a.Press("alt+f3")
			a.Settle()
		}
		return opened(), a.Dump()
	})
	a.Press("escape")
	a.Eventually("the peek to close with Escape", func() (bool, string) { return !opened(), a.Dump() })

	// a click on the stripe of the first change opens the peek on it
	a.Click(stripeX, editorLine(6))
	a.Eventually("the peek of the first change", func() (bool, string) {
		tx, ok := peekTitle(a)
		return ok && strings.HasSuffix(tx.Text, "1 of 2 changes"), a.Dump()
	})
	title, _ := peekTitle(a)
	y := title.Y + title.Height/2

	// Stage Change writes that change to the index and not the other one, and leaves the work tree alone
	a.Click(stageX, y)
	a.Eventually("the first change in the index", func() (bool, string) {
		got := repo.Git("diff", "--cached", "-U0")
		return strings.Contains(got, "+\tretries := 3") && !strings.Contains(got, "return data, nil"), got
	})
	if got := repo.Git("diff", "-U0"); !strings.Contains(got, "return data, nil") || strings.Contains(got, "retries := 3") {
		t.Errorf("the work tree differs from the index in %q", got)
	}
	data, err := os.ReadFile(filepath.Join(a.Project, "loader.go"))
	if err != nil || !strings.Contains(string(data), "retries := 3") || !strings.Contains(string(data), "return data, nil") {
		t.Errorf("the file was changed by staging: %v %q", err, data)
	}
	a.Eventually("the peek to close after the action", func() (bool, string) { return !opened(), a.Dump() })

	// the other change is the only one left: Revert Change puts the index lines back in the file
	// (the staged change has a peek of the Index provider above it: the cursor goes to the line of the change that is left, line 13)
	a.Click(editorColumnX(10), editorLine(13))
	a.Eventually("the peek of the remaining change", func() (bool, string) {
		tx, ok := peekTitle(a)
		if !ok {
			// the model compares again some time after the staging: the key opens nothing before that
			a.Press("alt+f3")
			a.Settle()
			tx, ok = peekTitle(a)
		}
		return ok && strings.Contains(tx.Text, " 1 of 1 change"), tx.Text
	})
	title, _ = peekTitle(a)
	a.Click(revertX, title.Y+title.Height/2)
	a.Eventually("the file to be reverted and saved", func() (bool, string) {
		data, err := os.ReadFile(filepath.Join(a.Project, "loader.go"))
		return err == nil && !strings.Contains(string(data), "return data, nil") && strings.Contains(string(data), "retries := 3"), string(data)
	})
	if got := strings.TrimSpace(repo.Git("diff", "-U0")); got != "" {
		t.Errorf("the work tree still differs from the index: %q", got)
	}
	a.Eventually("the peek to close with the last change", func() (bool, string) { return !opened(), a.Dump() })
	shot(t, a, "peek-reverted")
}
