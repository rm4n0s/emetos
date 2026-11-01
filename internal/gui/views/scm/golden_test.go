package scm

import (
	"strings"
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

// TestGoldenSyncItem looks at the sync item of the repository header in the states the Git extension gives it (step 24.15): in step,
// behind and ahead, a branch to publish, a sync that runs (the spinning icon, no click), and the progress bar of the repository.
func TestGoldenSyncItem(t *testing.T) {
	states := []struct {
		name string
		item scmcore.StatusCommand
		busy bool
	}{
		{"in-step", scmcore.StatusCommand{Text: "$(sync)", Tooltip: "Synchronize Changes", Command: "git.sync"}, false},
		{"behind-and-ahead", scmcore.StatusCommand{Text: "$(sync) 2↓ 1↑", Tooltip: "Pull 2 and push 1 commits between origin/main", Command: "git.sync"}, false},
		{"publish", scmcore.StatusCommand{Text: "$(cloud-upload)", Tooltip: "Publish Branch", Command: "git.publish"}, false},
		{"running", scmcore.StatusCommand{Text: "$(sync~spin) 2↓ 1↑", Tooltip: "Synchronizing Changes..."}, true},
	}
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t)
			for _, st := range states {
				src := r.addRepo(st.name, group{"workingTree", "Changes", []change{{"a.go", "M", "Modified", colModified, false}}})
				src.SetStatusBarCommands([]scmcore.StatusCommand{
					{Text: "$(git-branch) main", Tooltip: "main, Checkout Branch/Tag...", Command: "git.checkout"}, st.item,
				})
				src.SetBusy(st.busy)
			}
			r.show(th.colors, ui.Size{Width: 360, Height: 330})
			testutil.MatchesGolden(r.ts, "sync_item_"+th.name)
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

// TestGoldenInput looks at the input under the header: empty with its placeholder, a message, a box of several lines, a box that
// stops growing at its maximum, a long line wrapped, and the button below it in the states it has.
func TestGoldenInput(t *testing.T) {
	commit := scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "$(check) Commit"}, Enabled: true,
		Secondary: [][]scmcore.Command{{{ID: "git.commitAmend", Title: "Commit (Amend)"}}}}
	disabled := scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "$(check) Commit"}, Description: "No staged changes"}
	sync := scmcore.ActionButton{Command: scmcore.Command{ID: "git.sync", Title: "$(sync) Sync Changes 1$(arrow-down) 2$(arrow-up)"}, Enabled: true}
	tests := []struct {
		name   string
		text   string
		button *scmcore.ActionButton
		max    int
	}{
		{"empty", "", &commit, 10},
		{"message", "Fix the cache key of the loader\n\nThe key ignored the scheme.", &commit, 10},
		{"long", strings.Repeat("A long line of words that wraps. ", 3), nil, 10},
		{"capped", "one\ntwo\nthree\nfour\nfive", nil, 3},
		{"disabled", "", &disabled, 10},
		{"sync", "", &sync, 10},
	}
	for _, th := range goldenThemes {
		for _, tt := range tests {
			t.Run(tt.name+" "+th.name, func(t *testing.T) {
				r := newBoxRig(t)
				src := r.repoWithBox("project", gitGroups()...)
				r.maxLines = tt.max
				if tt.button != nil {
					src.SetActionButton(tt.button)
				}
				if tt.text != "" {
					src.Input().SetValue(tt.text)
				}
				r.show(th.colors, ui.Size{Width: 320, Height: 400})
				r.settle()
				testutil.MatchesGolden(r.ts, "input_"+tt.name+"_"+th.name)
				r.noErrors()
			})
		}
	}
}

// TestGoldenCommitStates looks at the states the Git extension gives the box and the button (step 24.10): a commit that waits for the
// resolved conflicts of a merge (Continue, disabled), a branch to publish, a protected branch whose commits ask (the lock), and a subject
// that is too long for git.inputValidation (the warning under the box, which the box keeps while it has no keyboard).
func TestGoldenCommitStates(t *testing.T) {
	cmds := func(icon string) [][]scmcore.Command {
		return [][]scmcore.Command{
			{{ID: "git.commit", Title: icon + " Commit"}, {ID: "git.commitAmend", Title: icon + " Commit (Amend)"}},
			{{ID: "git.commit", Title: icon + " Commit & Push"}, {ID: "git.commit", Title: icon + " Commit & Sync"}},
		}
	}
	tests := []struct {
		name   string
		text   string
		button scmcore.ActionButton
		warn   string
	}{
		{"continue", "Merge branch 'other'", scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "$(check) Continue", Tooltip: "Continue Merge"}}, ""},
		{"publish", "", scmcore.ActionButton{Command: scmcore.Command{ID: "git.publish", Title: "$(cloud-upload) Publish Branch", Tooltip: "Publish Branch \"main\""}, Enabled: true}, ""},
		{"protected", "Fix the cache key", scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "$(lock) Commit"}, Enabled: true, Secondary: cmds("$(lock)")}, ""},
		{"overlength", "Fix the cache key of the loader so that the scheme is part of it", scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "$(check) Commit"}, Enabled: true},
			"13 characters over 50 in current line"},
	}
	for _, th := range goldenThemes {
		for _, tt := range tests {
			t.Run(tt.name+" "+th.name, func(t *testing.T) {
				r := newBoxRig(t)
				src := r.repoWithBox("project", gitGroups()...)
				src.SetActionButton(&tt.button)
				if tt.warn != "" {
					src.Input().SetValidation(func(string) *scmcore.Validation {
						return &scmcore.Validation{Message: tt.warn, Level: scmcore.ValidationWarning}
					})
				}
				if tt.text != "" {
					src.Input().SetValue(tt.text)
				}
				r.show(th.colors, ui.Size{Width: 320, Height: 400})
				if tt.warn != "" {
					r.view.inputs[src].ctl.Focus()
				}
				r.settle()
				testutil.MatchesGolden(r.ts, "commit_"+tt.name+"_"+th.name)
				r.noErrors()
			})
		}
	}
}

// TestGoldenInProgressStates looks at the view while an operation waits for the user (step 24.14): a merge with files in conflict (the
// placeholder of the empty box, Continue off), a rebase stopped at a commit whose message the box shows with the warning that it cannot be
// changed (Continue on), and a cherry-pick with its conflicts resolved.
func TestGoldenInProgressStates(t *testing.T) {
	staged := func(paths ...string) group {
		g := group{"index", "Staged Changes", nil}
		for _, p := range paths {
			g.changes = append(g.changes, change{p, "M", "Index Modified", colStaged, false})
		}
		return g
	}
	cont := func(tooltip string, enabled bool) scmcore.ActionButton {
		return scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "$(check) Continue", Tooltip: tooltip}, Enabled: enabled}
	}
	tests := []struct {
		name        string
		placeholder string
		text        string
		warn        string
		groups      []group
		button      scmcore.ActionButton
	}{
		{"merge", "Message ({0} to continue the merge)", "", "", []group{
			{"merge", "Merge Changes", []change{{"src/loader.go", "!", "Conflict: Both Modified", colConflict, false}, {"src/config.go", "!", "Conflict: Both Added", colConflict, false},
				{"old/legacy.go", "!", "Conflict: Deleted By Us", colConflict, true}}},
			staged("README.md")}, cont("Continue Merge", false)},
		{"rebase", "Message ({0} to continue the rebase)", "Retry five times",
			"It's not possible to change the commit message in the middle of a rebase. Please complete the rebase operation and use interactive rebase instead.",
			[]group{staged("src/loader.go")}, cont("Continue Rebase", true)},
		{"cherrypick", "Message ({0} to continue the cherry-pick)", "", "", []group{staged("src/loader.go", "docs/notes.md")}, cont("Continue Cherry Pick", true)},
	}
	for _, th := range goldenThemes {
		for _, tt := range tests {
			t.Run(tt.name+" "+th.name, func(t *testing.T) {
				r := newBoxRig(t)
				src := r.repoWithBox("project", tt.groups...)
				src.Input().SetPlaceholder(tt.placeholder)
				src.SetActionButton(&tt.button)
				if tt.warn != "" {
					src.Input().SetValidation(func(string) *scmcore.Validation {
						return &scmcore.Validation{Message: tt.warn, Level: scmcore.ValidationWarning}
					})
				}
				if tt.text != "" {
					src.Input().SetValue(tt.text)
				}
				r.show(th.colors, ui.Size{Width: 440, Height: 300})
				if tt.warn != "" {
					r.view.inputs[src].ctl.Focus()
				}
				r.settle()
				testutil.MatchesGolden(r.ts, "inprogress_"+tt.name+"_"+th.name)
				r.noErrors()
			})
		}
	}
}

// TestGoldenInputValidation looks at the box with the keyboard and a validation message under it, for each level, and at the box
// that kept the color of a level after Escape took the message away.
func TestGoldenInputValidation(t *testing.T) {
	levels := []struct {
		name  string
		level scmcore.ValidationLevel
	}{{"info", scmcore.ValidationInfo}, {"warning", scmcore.ValidationWarning}, {"error", scmcore.ValidationError}}
	for _, th := range goldenThemes {
		for _, lv := range levels {
			t.Run(lv.name+" "+th.name, func(t *testing.T) {
				r := newBoxRig(t)
				src := r.repoWithBox("project", gitGroups()...)
				src.SetActionButton(&scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "$(check) Commit"}, Enabled: true})
				src.Input().SetValidation(func(string) *scmcore.Validation {
					return &scmcore.Validation{Message: "The subject has 31 characters; 30 or fewer are recommended.", Level: lv.level}
				})
				src.Input().SetValue("Fix the cache key of the loader")
				r.show(th.colors, ui.Size{Width: 320, Height: 400})
				r.view.inputs[src].ctl.Focus()
				r.settle()
				testutil.MatchesGolden(r.ts, "input_validation_"+lv.name+"_"+th.name)
				if lv.name == "warning" {
					r.view.ClearValidation(src)
					r.settle()
					testutil.MatchesGolden(r.ts, "input_validation_cleared_"+th.name)
				}
				r.noErrors()
			})
		}
	}
}

// TestGoldenInputRepositories looks at several repositories: a box and a button under each header.
func TestGoldenInputRepositories(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newBoxRig(t)
			a := r.repoWithBox("emetos", group{"workingTree", "Changes", []change{{"main.go", "M", "Modified", colModified, false}}})
			a.SetActionButton(&scmcore.ActionButton{Command: scmcore.Command{ID: "git.commit", Title: "$(check) Commit"}, Enabled: true})
			a.Input().SetValue("Add the commit input")
			b := r.repoWithBox("glutter", group{"workingTree", "Changes", nil})
			b.SetActionButton(&scmcore.ActionButton{Command: scmcore.Command{ID: "git.sync", Title: "$(sync) Sync Changes"}, Enabled: true})
			r.show(th.colors, ui.Size{Width: 320, Height: 330})
			r.settle()
			testutil.MatchesGolden(r.ts, "input_repositories_"+th.name)
			r.noErrors()
		})
	}
}
