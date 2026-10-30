package scm

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/welcome"
)

// TestGoldenChanges looks at the Changes view of one repository in the list and the tree view modes: the groups with their
// counts, the files with their icons, letters and colors, a deleted file struck through.
func TestGoldenChanges(t *testing.T) {
	for _, th := range goldenThemes {
		for _, mode := range []string{"list", "tree"} {
			t.Run(th.name+" "+mode, func(t *testing.T) {
				r := newRig(t)
				r.addRepo("project", gitGroups()...)
				if mode == "tree" {
					r.model.SetViewMode("tree")
				}
				r.show(th.colors, ui.Size{Width: 300, Height: 330})
				testutil.MatchesGolden(r.ts, "changes_"+mode+"_"+th.name)
				r.noErrors()
			})
		}
	}
}

// TestGoldenRepositories looks at several repositories: a header each with the branch and the sync item, the buttons of the title,
// the count (scm.providerCountBadge visible), one repository folded, and one running a command (the progress bar).
func TestGoldenRepositories(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t)
			a := r.addRepo("emetos", gitGroups()...)
			a.SetStatusBarCommands([]scmcore.StatusCommand{
				{Text: "$(git-branch-changes) main*+", Tooltip: "main, Checkout Branch/Tag...", Command: "git.checkout"},
				{Text: "$(sync) 1↓ 2↑", Tooltip: "Synchronize Changes", Command: "git.sync"},
			})
			b := r.addRepo("glutter", group{"workingTree", "Changes", []change{{"widgets/list.go", "M", "Modified", colModified, false}}})
			b.SetBusy(true)
			c := r.addRepo("docs", group{"workingTree", "Changes", []change{{"index.md", "M", "Modified", colModified, false}}})
			cfg := r.model.Config()
			cfg.ProviderCountBadge = scmcore.ProviderCountVisible
			r.model.SetConfig(cfg)
			r.view.setCollapsed(&row{kind: kindRepository, src: c, expanded: true, collapse: "r\x00" + c.StorageKey()}, true)
			r.show(th.colors, ui.Size{Width: 360, Height: 330})
			testutil.MatchesGolden(r.ts, "repositories_"+th.name)
			r.noErrors()
		})
	}
}

// TestGoldenRowStates looks at a row under the pointer (its inline actions), a selected row, the group header with its buttons, and
// the buttons of every row with scm.alwaysShowActions.
func TestGoldenRowStates(t *testing.T) {
	for _, th := range goldenThemes {
		for _, always := range []bool{false, true} {
			name := "hover_" + th.name
			if always {
				name = "always_" + th.name
			}
			t.Run(name, func(t *testing.T) {
				r := newRig(t)
				r.always = always
				r.addRepo("project", gitGroups()...)
				r.show(th.colors, ui.Size{Width: 300, Height: 330})
				if !always {
					r.view.SetHover("file:///work/project/src/config.go")
				} else {
					r.view.List().SetSelection([]string{"f:scm1:index:file:///work/project/docs/notes.md"})
				}
				r.settle()
				testutil.MatchesGolden(r.ts, "rows_"+name)
				r.noErrors()
			})
		}
	}
}

// TestGoldenFolded looks at folded groups and a tree folded at its folders, and at a long name that is cut.
func TestGoldenFolded(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t)
			r.model.SetViewMode(scmcore.ViewTree)
			src := r.addRepo("project", group{"merge", "Merge Changes", []change{{"src/loader.go", "U", "Conflict: Both Modified", colConflict, false}}},
				group{"index", "Staged Changes", []change{{"internal/core/scm/model.go", "M", "Index Modified", colStaged, false}, {"internal/core/scm/tree.go", "M", "Index Modified", colStaged, false}}},
				group{"workingTree", "Changes", []change{{"a/very/long/path/with/many/segments/and_a_file_name_that_is_much_too_long_for_the_view.go", "M", "Modified", colModified, false},
					{"b/c.go", "A", "Untracked", colAdded, false}}})
			r.view.setCollapsed(&row{kind: kindGroup, src: src, group: src.Group("merge"), expanded: true, collapse: "g\x00" + src.StorageKey() + "\x00merge"}, true)
			r.show(th.colors, ui.Size{Width: 300, Height: 220})
			testutil.MatchesGolden(r.ts, "folded_"+th.name)
			r.noErrors()
		})
	}
}

// TestGoldenWelcomeInView looks at the Changes view with no repository: the welcome content stands in place of the list.
func TestGoldenWelcomeInView(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t)
			r.ts = pump(t, th.colors, ui.Size{Width: 300, Height: 200}, NewBody(r.view, BodyOpts{Welcome: func(widgets.BuildContext) widgets.Widget {
				return welcome.New(welcome.Opts{Pieces: []welcome.Piece{{Contents: "No source control providers registered."}}})
			}}))
			r.settle()
			testutil.MatchesGolden(r.ts, "welcome_"+th.name)
			r.noErrors()
			// a repository takes its place
			r.addRepo("p", group{"workingTree", "Changes", []change{{path: "x.go"}}})
			r.settle()
			if r.ts.Exists(gluttertest.ByKey(WelcomeKey)) || !r.ts.Exists(gluttertest.ByKey(ListKey)) {
				t.Error("the list does not replace the welcome content")
			}
		})
	}
}

// TestGoldenRepositoriesView looks at the Repositories view: every repository, the shown ones selected.
func TestGoldenRepositoriesView(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t)
			a := r.addRepo("emetos", group{"workingTree", "Changes", []change{{path: "x.go"}, {path: "y.go"}}})
			a.SetStatusBarCommands([]scmcore.StatusCommand{{Text: "$(git-branch) main", Command: "git.checkout"}, {Text: "$(sync) 1↓", Command: "git.sync"}})
			b := r.addRepo("glutter", group{"workingTree", "Changes", []change{{path: "z.go"}}})
			b.SetStatusBarCommands([]scmcore.StatusCommand{{Text: "$(git-branch) v1.9", Command: "git.checkout"}})
			r.addRepo("third", group{"workingTree", "Changes", nil})
			r.model.ToggleVisibility(r.model.Sources()[2], nil)
			repos := NewRepositories(r.view)
			t.Cleanup(repos.Dispose)
			r.ts = pump(t, th.colors, ui.Size{Width: 320, Height: 100}, NewRepositoriesBody(repos))
			r.settle()
			testutil.MatchesGolden(r.ts, "repositories_view_"+th.name)
			r.noErrors()
		})
	}
}
