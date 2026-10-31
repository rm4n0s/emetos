package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

const loaderNow = `package src

import "strings"

// Load reads the named entries.
func Load(names []string) map[string]int {
	out := map[string]int{}
	for i, name := range names {
		key := strings.ToLower(name)
		out[key] = i + 1
	}
	return out
}

// Count is the number of entries.
func Count(m map[string]int) int {
	return len(m)
}
`

const loaderOld = `package src

import "strings"

// Load reads the named entries.
func Load(names []string) map[string]int {
	out := map[string]int{}
	for i, name := range names {
		key := strings.TrimSpace(name)
		out[key] = i
	}
	return out
}

// Count is the number of entries.
func Count(m map[string]int) int {
	return len(m)
}
`

// TestGoldenDiffEditorsWithTheTitlesOfGit looks at the tabs of the diffs and the document that an extension opens with the titles of VS Code's git
// extension: a preview in italics, a kept diff, and a version opened on its own.
func TestGoldenDiffEditorsWithTheTitlesOfGit(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			paths := testutil.Isolate(t)
			testutil.Settings(t, paths, map[string]any{
				"workbench.colorTheme": th.theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false, "files.hotExit": "off",
				"window.menuBarVisibility": "classic",
			})
			rig := &contentRig{texts: map[string]string{}, asked: map[string]int{}}
			p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{rig.factory()}})
			root := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
				t.Fatal(err)
			}
			for name, text := range map[string]string{"src/loader.go": loaderNow, "src/other.go": "package src\n"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			win, err := NewWorkspace(context.Background(), p, root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(win.release)
			rig.groupApp = &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
			rig.ts = testutil.New(t, ui.Size{Width: 1100, Height: 420})
			rig.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
			rig.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
			rig.pump()

			id := "0123456789ab"
			file := document.FileURI(filepath.Join(root, "src/loader.go")).String()
			rig.set(tst(id+"/HEAD/src/loader.go"), loaderOld)
			rig.set(tst(id+"/~/src/loader.go"), loaderOld)
			docs := rig.api.Documents()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			// one after the other: each is made in the background and the tabs come in the order they arrive
			must(docs.OpenDocument(extension.OpenSpec{URI: tst(id + "/HEAD/src/loader.go"), Title: "loader.go (HEAD)"}))
			waitFor(t, rig.ts, "the first tab", func() bool { return len(rig.tabs()) == 1 })
			must(docs.OpenDiff(extension.DiffSpec{Left: tst(id + "/HEAD/src/loader.go"), Right: tst(id + "/~/src/loader.go"), Title: "loader.go (Index)"}))
			waitFor(t, rig.ts, "the second tab", func() bool { return len(rig.tabs()) == 2 })
			must(docs.OpenDiff(extension.DiffSpec{Left: tst(id + "/~/src/loader.go"), Right: file, Title: "loader.go (Working Tree)", Preview: true}))
			waitFor(t, rig.ts, "the third tab", func() bool { return len(rig.tabs()) == 3 })
			waitFor(t, rig.ts, "the diff to be computed", func() bool {
				panes := rig.shell().diffPanes()
				return len(panes) == 1 && panes[0].Model().Computed() && !panes[0].Model().Computing()
			})
			time.Sleep(50 * time.Millisecond)
			rig.pump()
			if got := strings.Join(rig.tabNames(), " | "); got != "loader.go (HEAD) | loader.go (Index) | loader.go (Working Tree)" {
				t.Fatalf("tabs %s", got)
			}
			testutil.MatchesGolden(rig.ts, "git_diff_titles_"+th.name)
			if errs := rig.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}
