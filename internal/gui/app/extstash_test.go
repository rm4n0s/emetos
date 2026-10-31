package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The editor of a stash in the window (step 24.18): the multi-file diff editor of the kind "git-stash", whose title has the actions
// Apply, Pop and Drop that the Git extension adds for the resourceScheme "git-stash".

// stashTitleParts are the commands and the editor title items of the stash actions, taken from the manifest of the Git extension so
// that the test follows the real ones.
func stashTitleParts(t *testing.T) ([]extension.CommandSpec, []extension.MenuItem) {
	t.Helper()
	m := gitscm.New().Manifest()
	var cmds []extension.CommandSpec
	var menus []extension.MenuItem
	for _, c := range m.Contributes.Commands {
		switch c.ID {
		case "git.stashApplyEditor", "git.stashPopEditor", "git.stashDropEditor":
			cmds = append(cmds, c)
		}
	}
	for _, it := range m.Contributes.Menus {
		if it.Menu == extension.MenuEditorTitle && slices.ContainsFunc(cmds, func(c extension.CommandSpec) bool { return c.ID == it.Command }) {
			// the setting git.enabled is the extension's own; the rig has no such setting
			it.When = strings.TrimPrefix(it.When, "config.git.enabled && !git.missing && ")
			menus = append(menus, it)
		}
	}
	if len(cmds) != 3 || len(menus) != 3 {
		t.Fatalf("%d commands and %d title items of the stash editor", len(cmds), len(menus))
	}
	return cmds, menus
}

// stashFactory makes an extension with the commands and the title items of the stash editor, the provider of the kind "git-stash" over
// the rig's sets, and the documents of the scheme "tst".
func (r *multiRig) stashFactory(t *testing.T) extension.Factory {
	cmds, menus := stashTitleParts(t)
	return func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{
				ID: "emetos.stashtest", Name: "Stash test", ActivationEvents: []string{extension.OnStartup},
				Contributes: extension.Contributes{Commands: cmds, Menus: menus},
			},
			activate: func(_ context.Context, api extension.API) error {
				r.api = api
				for _, c := range cmds {
					if err := api.Commands().Handle(c.ID, func(_ context.Context, args any) error { r.ran = append(r.ran, args); return nil }); err != nil {
						return err
					}
				}
				if _, err := api.Documents().ProvideContent(extension.ContentProvider{Scheme: "tst", Text: r.text}); err != nil {
					return err
				}
				reg, err := api.Documents().ProvideMultiDiff(extension.MultiDiffProvider{Kind: gitscm.StashMultiDiffKind, Resources: r.resources})
				r.reg = reg
				return err
			},
		}
	}
}

func newStashRig(t *testing.T, names ...string) *multiRig {
	t.Helper()
	r := &multiRig{texts: map[string]string{}, sets: map[string][]extension.MultiDiffResource{}, err: map[string]error{}, asked: map[string]int{}}
	p := extensionProcess(t, r.stashFactory(t))
	w, err := NewWorkspace(context.Background(), p, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.release)
	r.groupApp = newGroupAppWith(t, w, testWindowSize, names, nil)
	if r.api == nil || r.reg == nil {
		t.Fatal("the extension did not register its providers")
	}
	return r
}

// stashFiles are the files of a stash: a changed one and an untracked one.
func (r *multiRig) stashFiles(name string) []extension.MultiDiffResource {
	file := document.FileURI(r.files[name]).String()
	r.set(tst("id/base/"+name), "package x\n// old\n")
	r.set(tst("id/stash/"+name), "package x\n// new\n")
	r.set(tst("id/untracked/notes.txt"), "A note that was not tracked.\n")
	return []extension.MultiDiffResource{
		{Original: tst("id/base/" + name), Modified: tst("id/stash/" + name), Path: "src/" + name, File: file,
			Letter: "M", Color: "gitDecoration.modifiedResourceForeground", Tooltip: "Modified"},
		{Modified: tst("id/untracked/notes.txt"), Path: "notes.txt",
			Letter: "U", Color: "gitDecoration.untrackedResourceForeground", Tooltip: "Untracked"},
	}
}

func (r *multiRig) openStash(title, key string) {
	r.t.Helper()
	if err := r.api.Documents().OpenMultiDiff(extension.MultiDiffSpec{Title: title, Kind: gitscm.StashMultiDiffKind, Key: key}); err != nil {
		r.t.Fatal(err)
	}
	waitFor(r.t, r.ts, "the files", func() bool { p := r.paneOf(key); return p != nil && p.input.Loaded() })
	r.settleBodies()
}

func (r *multiRig) titleItems() []string {
	var got []string
	for _, e := range r.win.Menus.Items(command.EditorTitle, r.shell().groupContext(r.win, r.shell().activeGroup())) {
		got = append(got, e.Command)
	}
	return got
}

func TestTheTitleOfAStashEditorHasApplyPopAndDrop(t *testing.T) {
	r := newStashRig(t, "a.go")
	r.setFiles("abc/def", r.stashFiles("a.go")...)
	r.openStash("Git Stash #0: WIP loader cache", "abc/def")
	want := []string{"git.stashApplyEditor", "git.stashPopEditor", "git.stashDropEditor"}
	all := append(slices.Clone(want), "multiDiffEditor.goToPreviousChange", "multiDiffEditor.goToNextChange", "multiDiffEditor.collapseAll")
	if got := r.titleItems(); !slices.Equal(got, all) {
		t.Fatalf("title items %v, want %v", got, all)
	}
	// the buttons run the commands with the set of the editor
	tips := []string{"Apply Stash", "Pop Stash", "Drop Stash"}
	for i, tip := range tips {
		btn := gluttertest.First(gluttertest.ByPredicate("title button "+tip, func(w widgets.Widget) bool {
			b, ok := w.(*kit.IconButton)
			return ok && b.Tooltip() == tip
		}))
		if !r.ts.Exists(btn) {
			t.Fatalf("no button %q", tip)
		}
		r.ts.Tap(btn)
		r.ts.Pump(10 * time.Millisecond)
		if len(r.ran) != i+1 {
			t.Fatalf("after %q: ran %v", tip, r.ran)
		}
		args, ok := r.ran[i].(map[string]any)
		if !ok || args["kind"] != gitscm.StashMultiDiffKind || args["key"] != "abc/def" || args["groupId"] == nil {
			t.Errorf("%q ran with %#v", tip, r.ran[i])
		}
		if _, has := args["uri"]; has {
			t.Errorf("%q got a uri for an editor that has no document: %#v", tip, args)
		}
	}
	// the actions are not in the title of another editor
	r.doc("a.go")
	r.show("a.go", false)
	for _, c := range r.titleItems() {
		if slices.Contains(want, c) {
			t.Errorf("%s is in the title of a text editor", c)
		}
	}
	r.noErrors()
}

func TestAMultiFileEditorOfAnotherKindHasNoStashActions(t *testing.T) {
	r := newMultiRig(t, "a.go")
	r.set(tst("id/~/a.go"), "old\n")
	r.setFiles("k", r.modified("a.go"))
	r.open("Tst: Changes", "k")
	for _, c := range r.titleItems() {
		if len(c) > 9 && c[:9] == "git.stash" {
			t.Errorf("%s is in the title of the editor of changes", c)
		}
	}
}

func TestAStashEditorComesBackAfterARestart(t *testing.T) {
	r := newStashRig(t, "a.go")
	r.setFiles("abc/def", r.stashFiles("a.go")...)
	r.openStash("Git Stash #0: x", "abc/def")
	in := r.pane().input
	value, ok := in.Serialize()
	if !ok {
		t.Fatal("the stash editor is not saved")
	}
	if want := `{"kind":"git-stash","key":"abc/def","title":"Git Stash #0: x"}`; value != want {
		t.Errorf("saved %s, want %s", value, want)
	}
}

// TestGoldenTheEditorOfAStashAndItsPicker looks at the stash editor with Apply, Pop and Drop in its title, and at the picker of the
// stashes with the descriptions VS Code gives them.
func TestGoldenTheEditorOfAStashAndItsPicker(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			paths := testutil.Isolate(t)
			testutil.Settings(t, paths, map[string]any{
				"workbench.colorTheme": th.theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false, "files.hotExit": "off",
				"window.menuBarVisibility": "classic",
			})
			r := &multiRig{texts: map[string]string{}, sets: map[string][]extension.MultiDiffResource{}, err: map[string]error{}, asked: map[string]int{}}
			p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{r.stashFactory(t)}})
			root := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "src/loader.go"), []byte(loaderNow), 0o644); err != nil {
				t.Fatal(err)
			}
			win, err := NewWorkspace(context.Background(), p, root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(win.release)
			r.groupApp = &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
			r.ts = testutil.New(t, ui.Size{Width: 1100, Height: 560})
			r.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
			r.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
			r.pump()

			loader := document.FileURI(filepath.Join(root, "src/loader.go")).String()
			r.set(tst("id/base/src/loader.go"), loaderOld)
			r.set(tst("id/stash/src/loader.go"), loaderNow)
			r.set(tst("id/untracked/notes.md"), "# Notes\n\nA note that was not tracked.\n")
			r.setFiles("abc/def", extension.MultiDiffResource{
				Original: tst("id/base/src/loader.go"), Modified: tst("id/stash/src/loader.go"), Path: "src/loader.go", File: loader,
				Letter: "M", Color: "gitDecoration.modifiedResourceForeground", Tooltip: "Modified",
			}, extension.MultiDiffResource{
				Modified: tst("id/untracked/notes.md"), Path: "notes.md",
				Letter: "U", Color: "gitDecoration.untrackedResourceForeground", Tooltip: "Untracked",
			})
			r.openStash("Git Stash #0: WIP loader cache", "abc/def")
			time.Sleep(50 * time.Millisecond)
			r.pump()
			testutil.MatchesGolden(r.ts, "git_stash_editor_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}

			err = r.api.QuickPick().Show(extension.QuickPickOptions{
				Placeholder: "Pick a stash to apply",
				Items: []extension.QuickPickItem{
					{Label: "#0: WIP loader cache", Description: "2 hrs • main"},
					{Label: "#1: WIP (3f2a9c1 Update the loader, revision 2)", Description: "3 days • feature/cache"},
					{Label: "#2: On main: before the rebase", Description: "2 wks • main"},
				},
			}, func(int) {})
			if err != nil {
				t.Fatal(err)
			}
			r.ts.Pump(300 * time.Millisecond)
			r.pump()
			testutil.MatchesGolden(r.ts, "git_stash_picker_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}
