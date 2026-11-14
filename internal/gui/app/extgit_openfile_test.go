package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/mediapreview"
)

// Open File in the menu of a change of the Source Control view, with the real Git extension and the real git: the file opens in the
// editor it opens in from the Explorer, which for an image is the image preview (SPEC §10.37), not the text editor that refuses it
// as binary.

func TestOpenFileOfAChangeInSourceControlOpensTheFileInItsEditor(t *testing.T) {
	gittest.Require(t)
	const png = "\x89PNG\r\n\x1a\n\x00\x00\x00"
	cases := []struct {
		name, file string
		// changes opens the changes of the file first, with a click on its row
		changes bool
		want    string
		tabs    string
	}{
		{"a text file", "e.txt", false, "*editorgroup.TextInput", "main.go, e.txt*"},
		{"a text file whose changes are open", "e.txt", true, "*editorgroup.TextInput", "main.go, e.txt (Working Tree), e.txt*"},
		{"an image", "pic.png", false, "*editorgroup.FileEditorInput", "main.go, pic.png*"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newMultiEnv(t, nil)
			// the git of the extension reads no configuration of the user or of the system
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			root := env.folder("project", "main.go", "e.txt")
			repo := gittest.NewEnv(t).InitAt(root)
			repo.Write("pic.png", png)
			repo.Commit("Add the files", ".")
			repo.Write("e.txt", "changed\n")
			repo.Write("pic.png", png+"changed")
			gittest.Trust(t, root)
			// the program starts as Run starts it, with the real extensions: Git looks for its program and for the repository on
			// goroutines of its own while the window is made
			a := env.runWith(Options{Folder: root, Files: []string{filepath.Join(root, "main.go")}},
				ProcessOptions{Extensions: []extension.Factory{gitscm.New, mediapreview.New}})
			w := a.workspaceOf(root)
			if err := a.command(w, "workbench.view.scm", nil); err != nil {
				t.Fatal(err)
			}
			a.waitFor("the change in the view", func() bool { return a.text(tc.file) })
			tabs := func() string { return tabsOf(a.shellOf(w)) }
			row := a.ts.Center(gluttertest.ByText(tc.file))
			if tc.changes {
				a.ts.TapAt(row)
				a.waitFor("the changes of the file", func() bool { return strings.HasSuffix(tabs(), "(Working Tree)*") })
			}
			a.ts.TapAt(row, secondaryDown)
			a.settle()
			if !a.text("Open File") {
				t.Fatalf("the menu of the change has no Open File")
			}
			a.ts.Tap(gluttertest.ByText("Open File"))
			deadline := time.Now().Add(10 * time.Second)
			for tabs() != tc.tabs && time.Now().Before(deadline) {
				a.settle()
			}
			var notes []string
			for _, it := range w.Notify.Items() {
				notes = append(notes, it.Message)
			}
			if got := tabs(); got != tc.tabs {
				t.Fatalf("the tabs are %q after Open File, want %q; notifications %v", got, tc.tabs, notes)
			}
			if got := fmt.Sprintf("%T", a.shellOf(w).activeGroup().Model.ActiveEditor()); got != tc.want {
				t.Errorf("the file is shown by %s, want %s", got, tc.want)
			}
			if len(notes) != 0 {
				t.Errorf("notifications: %v", notes)
			}
		})
	}
}
