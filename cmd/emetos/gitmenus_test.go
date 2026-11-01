package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// stageCommands are the commands of the staging menus (step 24.8) that these tests look for in a menu.
var stageCommands = map[string]bool{
	"git.stage": true, "git.stageAll": true, "git.stageAllTracked": true, "git.stageAllUntracked": true, "git.stageAllMerge": true,
	"git.unstage": true, "git.unstageAll": true, "git.clean": true, "git.cleanAll": true, "git.cleanAllTracked": true,
	"git.cleanAllUntracked": true, "git.ignore": true,
}

// menuOf lists the staging commands that hold in a menu of the Git extension under the context keys, as "group command" in the order
// of the group and the item, the way the menu orders them.
func menuOf(t *testing.T, m extension.Manifest, menu string, keys map[string]any) []string {
	t.Helper()
	scope := contextkey.NewRoot()
	for k, v := range keys {
		scope.Set(k, v)
	}
	var items []extension.MenuItem
	for _, it := range m.Contributes.Menus {
		if it.Menu != menu || !stageCommands[it.Command] {
			continue
		}
		if it.When != "" {
			expr, err := contextkey.Parse(it.When)
			if err != nil {
				t.Fatalf("the condition %q: %v", it.When, err)
			}
			if expr == nil || !scope.Matches(expr) {
				continue
			}
		}
		items = append(items, it)
	}
	slices.SortStableFunc(items, func(a, b extension.MenuItem) int {
		if c := strings.Compare(a.Group, b.Group); c != 0 {
			return c
		}
		return int(a.Order - b.Order)
	})
	var out []string
	for _, it := range items {
		g := it.Group
		if it.Order != 0 {
			g += fmt.Sprintf("@%g", it.Order)
		}
		out = append(out, g+" "+it.Command)
	}
	return out
}

// TestTheMenusOfTheStagingCommandsHoldInTheStatesOfVSCode checks the conditions of the menus of the resources, the groups and the folders
// of the Source Control view against what VS Code's package.json lists for each state (SPEC §10.30 "Staging").
func TestTheMenusOfTheStagingCommandsHoldInTheStatesOfVSCode(t *testing.T) {
	m := gitManifest(t)
	state := func(group string, extra map[string]any) map[string]any {
		keys := map[string]any{
			"scmProvider": "git", "scmResourceGroup": group, "config.git.untrackedChanges": "mixed", "gitFreshRepository": false,
		}
		for k, v := range extra {
			keys[k] = v
		}
		return keys
	}
	tests := []struct {
		name string
		menu string
		keys map[string]any
		want []string
	}{
		{"a merge resource", extension.MenuSCMResourceStateContext, state("merge", nil), []string{"1_modification git.stage", "inline@2 git.stage"}},
		{"a staged resource", extension.MenuSCMResourceStateContext, state("index", nil), []string{"1_modification git.unstage", "inline@2 git.unstage"}},
		{"a changed resource", extension.MenuSCMResourceStateContext, state("workingTree", nil),
			[]string{"1_modification git.stage", "1_modification git.clean", "1_modification@3 git.ignore", "inline@2 git.clean", "inline@2 git.stage"}},
		{"an untracked resource", extension.MenuSCMResourceStateContext, state("untracked", nil),
			[]string{"1_modification git.stage", "1_modification git.clean", "1_modification@3 git.ignore", "inline@2 git.clean", "inline@2 git.stage"}},
		{"an untracked resource of a repository with no commit", extension.MenuSCMResourceStateContext, state("untracked", map[string]any{"gitFreshRepository": true}),
			[]string{"1_modification git.stage", "1_modification@3 git.ignore", "inline@2 git.stage"}},
		{"another provider", extension.MenuSCMResourceStateContext, state("workingTree", map[string]any{"scmProvider": "other"}), nil},

		{"the merge group", extension.MenuSCMResourceGroupContext, state("merge", nil), []string{"1_modification git.stageAllMerge", "inline@2 git.stageAllMerge"}},
		{"the staged group", extension.MenuSCMResourceGroupContext, state("index", nil), []string{"1_modification git.unstageAll", "inline@2 git.unstageAll"}},
		{"the changes group, mixed", extension.MenuSCMResourceGroupContext, state("workingTree", nil),
			[]string{"1_modification git.cleanAll", "1_modification git.stageAll", "inline@2 git.cleanAll", "inline@2 git.stageAll"}},
		{"the changes group, separate", extension.MenuSCMResourceGroupContext, state("workingTree", map[string]any{"config.git.untrackedChanges": "separate"}),
			[]string{"1_modification git.cleanAllTracked", "1_modification git.stageAllTracked", "inline@2 git.cleanAllTracked", "inline@2 git.stageAllTracked"}},
		{"the changes group, hidden", extension.MenuSCMResourceGroupContext, state("workingTree", map[string]any{"config.git.untrackedChanges": "hidden"}),
			[]string{"1_modification git.cleanAllTracked", "1_modification git.stageAllTracked", "inline@2 git.cleanAllTracked", "inline@2 git.stageAllTracked"}},
		{"the untracked group", extension.MenuSCMResourceGroupContext, state("untracked", map[string]any{"config.git.untrackedChanges": "separate"}),
			[]string{"1_modification git.cleanAllUntracked", "1_modification git.stageAllUntracked", "inline@2 git.cleanAllUntracked", "inline@2 git.stageAllUntracked"}},

		{"a folder of merge changes", extension.MenuSCMResourceFolderContext, state("merge", nil), []string{"1_modification git.stage", "inline@2 git.stage"}},
		{"a folder of staged changes", extension.MenuSCMResourceFolderContext, state("index", nil), []string{"1_modification git.unstage", "inline@2 git.unstage"}},
		{"a folder of changes", extension.MenuSCMResourceFolderContext, state("workingTree", nil),
			[]string{"1_modification git.stage", "1_modification git.clean", "1_modification@3 git.ignore", "inline@2 git.clean", "inline@2 git.stage"}},
		{"a folder of untracked files", extension.MenuSCMResourceFolderContext, state("untracked", map[string]any{"gitFreshRepository": true}),
			[]string{"1_modification git.stage", "1_modification git.clean", "1_modification@3 git.ignore", "inline@2 git.clean", "inline@2 git.stage"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := menuOf(t, m, tc.menu, tc.keys)
			// the order of items of one group and one order is the order of the manifest, so compare the sets
			a, b := slices.Clone(got), slices.Clone(tc.want)
			slices.Sort(a)
			slices.Sort(b)
			if !slices.Equal(a, b) {
				t.Errorf("got  %v\nwant %v", got, tc.want)
			}
		})
	}
}

// TestTheStagingCommandsAreInTheCommandPaletteAsInVSCode checks which of them the palette hides.
func TestTheStagingCommandsAreInTheCommandPaletteAsInVSCode(t *testing.T) {
	m := gitManifest(t)
	hidden := map[string]string{}
	for _, it := range m.Contributes.Menus {
		if it.Menu == extension.MenuCommandPalette {
			hidden[it.Command] = it.When
		}
	}
	for _, id := range []string{"git.stage", "git.stageAll", "git.stageAllTracked", "git.stageAllUntracked", "git.stageAllMerge", "git.unstage", "git.unstageAll", "git.clean",
		"git.cleanAll", "git.cleanAllTracked", "git.cleanAllUntracked", "git.ignore", "git.rename", "git.delete"} {
		when, ok := hidden[id]
		if !ok || when == "false" || !strings.Contains(when, "gitOpenRepositoryCount != 0") {
			t.Errorf("%s: palette condition %q", id, when)
		}
	}
	for _, id := range []string{"git.stageFile", "git.unstageFile"} {
		if hidden[id] != "false" {
			t.Errorf("%s is in the palette: %q", id, hidden[id])
		}
	}
}

// holds lists the items of a menu of the Git extension whose command or submenu passes keep and whose condition holds under the keys.
func holds(t *testing.T, m extension.Manifest, menu string, keys map[string]any, keep func(extension.MenuItem) bool) []string {
	t.Helper()
	scope := contextkey.NewRoot()
	for k, v := range keys {
		scope.Set(k, v)
	}
	var out []string
	for _, it := range m.Contributes.Menus {
		if it.Menu != menu || !keep(it) {
			continue
		}
		if it.When != "" {
			expr, err := contextkey.Parse(it.When)
			if err != nil {
				t.Fatalf("the condition %q: %v", it.When, err)
			}
			if expr == nil || !scope.Matches(expr) {
				continue
			}
		}
		name := it.Command
		if it.Submenu != "" {
			name = "submenu " + it.Submenu
		}
		out = append(out, it.Group+" "+name)
	}
	return out
}

// TestTheStashMenusHoldInTheStatesOfVSCode checks the Stash submenu of the "..." menus, the actions in the title of the stash editor and the
// command palette (step 24.18) against VS Code's package.json.
func TestTheStashMenusHoldInTheStatesOfVSCode(t *testing.T) {
	m := gitManifest(t)
	stash := func(it extension.MenuItem) bool {
		return strings.HasPrefix(it.Command, "git.stash") || it.Submenu == "git.stash"
	}
	enabled := map[string]any{"config.git.enabled": true, "git.missing": false, "gitOpenRepositoryCount": 1, "scmProvider": "git"}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range enabled {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	t.Run("the Stash submenu is in the menus of the view and of a repository", func(t *testing.T) {
		for _, menu := range []string{extension.MenuSCMTitle, extension.MenuSCMRepository} {
			if got := holds(t, m, menu, with(nil), stash); !slices.Equal(got, []string{"2_main submenu git.stash"}) {
				t.Errorf("%s: %v", menu, got)
			}
			if got := holds(t, m, menu, with(map[string]any{"scmProvider": "other"}), stash); len(got) != 0 {
				t.Errorf("%s of another provider: %v", menu, got)
			}
		}
	})
	t.Run("the entries of the submenu, with Stash Staged only from git 2.35", func(t *testing.T) {
		all := []string{"1_stash git.stash", "1_stash git.stashIncludeUntracked", "2_apply git.stashApplyLatest", "2_apply git.stashApply", "3_pop git.stashPopLatest",
			"3_pop git.stashPop", "4_drop git.stashDrop", "4_drop git.stashDropAll", "5_preview git.stashView"}
		withStaged := slices.Insert(slices.Clone(all), 2, "1_stash git.stashStaged")
		if got := holds(t, m, "git.stash", with(map[string]any{"gitVersion235": true}), stash); !slices.Equal(got, withStaged) {
			t.Errorf("with git 2.35: %v", got)
		}
		if got := holds(t, m, "git.stash", with(map[string]any{"gitVersion235": false}), stash); !slices.Equal(got, all) {
			t.Errorf("with an older git: %v", got)
		}
	})
	t.Run("the actions of the stash editor", func(t *testing.T) {
		title := []string{"navigation git.stashApplyEditor", "navigation git.stashPopEditor", "navigation git.stashDropEditor"}
		tests := []struct {
			name string
			keys map[string]any
			want []string
		}{
			{"the editor of a stash", with(map[string]any{"resourceScheme": "git-stash"}), title},
			{"a file", with(map[string]any{"resourceScheme": "file"}), nil},
			{"a stash while Git is off", with(map[string]any{"resourceScheme": "git-stash", "config.git.enabled": false}), nil},
			{"a stash while git is missing", with(map[string]any{"resourceScheme": "git-stash", "git.missing": true}), nil},
			{"no repository is needed", with(map[string]any{"resourceScheme": "git-stash", "gitOpenRepositoryCount": 0}), title},
		}
		for _, tc := range tests {
			if got := holds(t, m, extension.MenuEditorTitle, tc.keys, stash); !slices.Equal(got, tc.want) {
				t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
			}
		}
	})
	t.Run("the palette hides the editor actions and needs a repository for the rest", func(t *testing.T) {
		palette := map[string]string{}
		for _, it := range m.Contributes.Menus {
			if it.Menu == extension.MenuCommandPalette && strings.HasPrefix(it.Command, "git.stash") {
				palette[it.Command] = it.When
			}
		}
		for _, id := range []string{"git.stashPopEditor", "git.stashApplyEditor", "git.stashDropEditor"} {
			if palette[id] != "false" {
				t.Errorf("%s is in the palette: %q", id, palette[id])
			}
		}
		for _, id := range []string{"git.stash", "git.stashIncludeUntracked", "git.stashPop", "git.stashPopLatest", "git.stashApply", "git.stashApplyLatest", "git.stashDrop", "git.stashDropAll"} {
			if when := palette[id]; !strings.Contains(when, "gitOpenRepositoryCount != 0") {
				t.Errorf("%s: palette condition %q", id, when)
			}
		}
		if !strings.Contains(palette["git.stashStaged"], "gitVersion235") {
			t.Errorf("git.stashStaged: %q", palette["git.stashStaged"])
		}
		if palette["git.stashView"] != "config.git.enabled && !git.missing" {
			t.Errorf("git.stashView: %q", palette["git.stashView"])
		}
	})
}

// TestTheBranchAndTagMenusHoldInTheStatesOfVSCode checks Checkout to... in the header of the view and of a repository, the Branch and Tags
// submenus and their entries, and the palette (step 24.13) against VS Code's package.json.
func TestTheBranchAndTagMenusHoldInTheStatesOfVSCode(t *testing.T) {
	m := gitManifest(t)
	mine := func(it extension.MenuItem) bool {
		switch it.Command {
		case "git.checkout", "git.branch", "git.branchFrom", "git.renameBranch", "git.deleteBranch", "git.deleteRemoteBranch",
			"git.createTag", "git.deleteTag", "git.deleteRemoteTag", "git.pushTags", "git.merge", "git.rebase", "git.rebaseInteractive":
			return true
		}
		return it.Submenu == "git.branch" || it.Submenu == "git.tags"
	}
	enabled := map[string]any{"config.git.enabled": true, "git.missing": false, "gitOpenRepositoryCount": 1, "scmProvider": "git"}
	t.Run("the header and the submenus are in the menus of the view and of a repository", func(t *testing.T) {
		for _, menu := range []string{extension.MenuSCMTitle, extension.MenuSCMRepository} {
			want := []string{"1_header git.checkout", "2_main submenu git.branch", "2_main submenu git.tags"}
			if got := holds(t, m, menu, enabled, mine); !slices.Equal(got, want) {
				t.Errorf("%s: %v, want %v", menu, got, want)
			}
			other := map[string]any{"config.git.enabled": true, "scmProvider": "other"}
			if got := holds(t, m, menu, other, mine); len(got) != 0 {
				t.Errorf("%s of another provider: %v", menu, got)
			}
		}
	})
	t.Run("the entries of the submenus", func(t *testing.T) {
		branch := []string{"1_merge git.merge", "1_merge git.rebase", "1_merge git.rebaseInteractive", "2_branch git.branch", "2_branch git.branchFrom", "3_modify git.renameBranch", "3_modify git.deleteBranch", "3_modify git.deleteRemoteBranch"}
		if got := holds(t, m, "git.branch", enabled, mine); !slices.Equal(got, branch) {
			t.Errorf("Branch: %v", got)
		}
		tags := []string{"1_tags git.createTag", "1_tags git.deleteTag", "1_tags git.deleteRemoteTag", "2_tags git.pushTags"}
		if got := holds(t, m, "git.tags", enabled, mine); !slices.Equal(got, tags) {
			t.Errorf("Tags: %v", got)
		}
	})
	t.Run("the palette needs a repository", func(t *testing.T) {
		palette := map[string]string{}
		for _, it := range m.Contributes.Menus {
			if it.Menu == extension.MenuCommandPalette && mine(it) {
				palette[it.Command] = it.When
			}
		}
		for _, id := range []string{"git.checkout", "git.branch", "git.branchFrom", "git.renameBranch", "git.deleteBranch", "git.deleteRemoteBranch",
			"git.createTag", "git.deleteTag", "git.deleteRemoteTag", "git.pushTags", "git.merge", "git.rebase", "git.rebaseInteractive"} {
			if want := "config.git.enabled && !git.missing && gitOpenRepositoryCount != 0"; palette[id] != want {
				t.Errorf("%s: palette condition %q", id, palette[id])
			}
		}
	})
}

// TestTheCommitMenusHoldInTheStatesOfVSCode checks the check mark in the title of the view, the Commit submenu of the "..." menus and its
// entries (the no-verify ones only with git.allowNoVerifyCommit), and the palette (step 24.10).
func TestTheCommitMenusHoldInTheStatesOfVSCode(t *testing.T) {
	m := gitManifest(t)
	commit := func(it extension.MenuItem) bool {
		return strings.HasPrefix(it.Command, "git.commit") || it.Command == "git.undoCommit" || it.Submenu == "git.commit"
	}
	keys := func(allow bool) map[string]any {
		return map[string]any{"scmProvider": "git", "config.git.allowNoVerifyCommit": allow}
	}
	t.Run("the title of the view has the check mark and the submenu", func(t *testing.T) {
		got := holds(t, m, extension.MenuSCMTitle, keys(false), commit)
		if !slices.Equal(got, []string{"navigation git.commit", "2_main submenu git.commit"}) {
			t.Errorf("%v", got)
		}
		if got := holds(t, m, extension.MenuSCMTitle, map[string]any{"scmProvider": "other"}, commit); len(got) != 0 {
			t.Errorf("another provider: %v", got)
		}
	})
	t.Run("a repository has the submenu", func(t *testing.T) {
		if got := holds(t, m, extension.MenuSCMRepository, keys(false), commit); !slices.Equal(got, []string{"2_main submenu git.commit"}) {
			t.Errorf("%v", got)
		}
	})
	t.Run("the entries of the submenu", func(t *testing.T) {
		plain := []string{
			"1_commit git.commit", "1_commit git.commitStaged", "1_commit git.commitAll", "1_commit git.undoCommit",
			"3_amend git.commitAmend", "3_amend git.commitStagedAmend", "3_amend git.commitAllAmend",
			"5_signoff git.commitSigned", "5_signoff git.commitStagedSigned", "5_signoff git.commitAllSigned",
		}
		if got := holds(t, m, "git.commit", keys(false), commit); !slices.Equal(got, plain) {
			t.Errorf("without no-verify: %v", got)
		}
		withNoVerify := append(slices.Clone(plain),
			"2_commit_noverify git.commitNoVerify", "2_commit_noverify git.commitStagedNoVerify", "2_commit_noverify git.commitAllNoVerify",
			"4_amend_noverify git.commitAmendNoVerify", "4_amend_noverify git.commitStagedAmendNoVerify", "4_amend_noverify git.commitAllAmendNoVerify",
			"6_signoff_noverify git.commitSignedNoVerify", "6_signoff_noverify git.commitStagedSignedNoVerify", "6_signoff_noverify git.commitAllSignedNoVerify")
		got := holds(t, m, "git.commit", keys(true), commit)
		slices.Sort(got)
		slices.Sort(withNoVerify)
		if !slices.Equal(got, withNoVerify) {
			t.Errorf("with no-verify:\n%v\nwant\n%v", got, withNoVerify)
		}
	})
	t.Run("the palette", func(t *testing.T) {
		palette := map[string]string{}
		for _, it := range m.Contributes.Menus {
			if it.Menu == extension.MenuCommandPalette && (commit(it) || it.Command == "git.restoreCommitTemplate") {
				palette[it.Command] = it.When
			}
		}
		for _, id := range []string{"git.commit", "git.commitAmend", "git.commitSigned", "git.commitStaged", "git.commitEmpty", "git.commitAll", "git.undoCommit"} {
			if when := palette[id]; !strings.Contains(when, "gitOpenRepositoryCount != 0") || strings.Contains(when, "allowNoVerifyCommit") {
				t.Errorf("%s: palette condition %q", id, when)
			}
		}
		for id, when := range palette {
			if strings.HasSuffix(id, "NoVerify") && !strings.Contains(when, "config.git.allowNoVerifyCommit") {
				t.Errorf("%s: palette condition %q", id, when)
			}
		}
		for _, id := range []string{"git.commitMessageAccept", "git.commitMessageDiscard", "git.restoreCommitTemplate"} {
			if palette[id] != "false" {
				t.Errorf("%s is in the palette: %q", id, palette[id])
			}
		}
	})
}

// TestTheMergeAndRebaseStatesAreInTheMenusAndThePalette checks where the operations that stop on conflicts are offered (step 24.14): the
// aborts in the Commit submenu, the palette conditions of the aborts, the skip and the merge editor's commands, and the buttons over an
// editor.
func TestTheMergeAndRebaseStatesAreInTheMenusAndThePalette(t *testing.T) {
	m := gitManifest(t)
	state := func(it extension.MenuItem) bool {
		switch it.Command {
		case "git.rebaseAbort", "git.mergeAbort", "git.cherryPickAbort":
			return true
		}
		return false
	}
	enabled := map[string]any{"scmProvider": "git"}
	want := []string{"1_commit git.rebaseAbort", "1_commit git.mergeAbort", "1_commit git.cherryPickAbort"}
	if got := holds(t, m, "git.commit", enabled, state); !slices.Equal(got, want) {
		t.Errorf("the Commit submenu: %v, want %v", got, want)
	}
	const open = "config.git.enabled && !git.missing && gitOpenRepositoryCount != 0"
	palette := map[string]string{}
	for _, it := range m.Contributes.Menus {
		if it.Menu == extension.MenuCommandPalette {
			palette[it.Command] = it.When
		}
	}
	for id, when := range map[string]string{
		"git.merge": open, "git.rebase": open, "git.rebaseInteractive": open, "git.cherryPick": open, "git.stageAllMerge": open,
		"git.mergeAbort":       open + " && gitMergeInProgress",
		"git.rebaseAbort":      open + " && gitRebaseInProgress",
		"git.rebaseSkip":       open + " && gitRebaseInProgress",
		"git.cherryPickAbort":  open + " && gitCherryPickInProgress",
		"git.runGitMerge":      "isMergeEditor",
		"git.runGitMergeDiff3": "isMergeEditor",
		"git.acceptMerge":      "false",
	} {
		if palette[id] != when {
			t.Errorf("%s: palette condition %q, want %q", id, palette[id], when)
		}
	}
	content := holds(t, m, extension.MenuEditorContent, map[string]any{
		"isMergeResultEditor": true, "mergeEditorResultUri": "file:///a", "git.mergeChanges": []any{"file:///a"},
	}, func(it extension.MenuItem) bool { return true })
	if !slices.Equal(content, []string{" git.acceptMerge"}) { // the item has no group, as in VS Code
		t.Errorf("the buttons over a merge result: %v", content)
	}
}

// TestTheWorktreeMenusHoldInTheStatesOfVSCode checks the Worktrees submenu of the "..." menus and its entries, the items of the menu of a
// worktree repository, Compare with Workspace for the files of a worktree, and the palette (step 24.19) against VS Code's package.json.
func TestTheWorktreeMenusHoldInTheStatesOfVSCode(t *testing.T) {
	m := gitManifest(t)
	mine := func(it extension.MenuItem) bool {
		switch it.Command {
		case "git.createWorktree", "git.deleteWorktree", "git.deleteWorktree2", "git.openWorktree", "git.openWorktreeInNewWindow",
			"git.migrateWorktreeChanges", "git.compareWithWorkspace":
			return true
		}
		return it.Submenu == "git.worktrees"
	}
	enabled := map[string]any{"config.git.enabled": true, "git.missing": false, "gitOpenRepositoryCount": 1, "scmProvider": "git"}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range enabled {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	t.Run("the Worktrees submenu is in the menus of the view and of a repository", func(t *testing.T) {
		for _, menu := range []string{extension.MenuSCMTitle, extension.MenuSCMRepository} {
			if got := holds(t, m, menu, enabled, mine); !slices.Equal(got, []string{"2_main submenu git.worktrees"}) {
				t.Errorf("%s: %v", menu, got)
			}
			if got := holds(t, m, menu, map[string]any{"scmProvider": "other"}, mine); len(got) != 0 {
				t.Errorf("%s of another provider: %v", menu, got)
			}
		}
	})
	t.Run("the entries of the submenu follow the kind of the repository", func(t *testing.T) {
		for _, tc := range []struct {
			kind string
			want []string
		}{
			{"repository", []string{"worktrees git.createWorktree"}},
			{"worktree", []string{"openWorktrees git.openWorktree", "openWorktrees git.openWorktreeInNewWindow", "worktrees git.deleteWorktree2"}},
			{"submodule", nil},
		} {
			if got := holds(t, m, "git.worktrees", with(map[string]any{"scmProviderContext": tc.kind}), mine); !slices.Equal(got, tc.want) {
				t.Errorf("%s: %v, want %v", tc.kind, got, tc.want)
			}
		}
	})
	t.Run("the menu of a worktree repository opens and deletes it", func(t *testing.T) {
		want := []string{"1_worktree git.openWorktree", "1_worktree git.openWorktreeInNewWindow", "2_worktree git.deleteWorktree2"}
		if got := holds(t, m, extension.MenuSCMSourceControl, with(map[string]any{"scmProviderContext": "worktree"}), mine); !slices.Equal(got, want) {
			t.Errorf("worktree: %v", got)
		}
		for _, kind := range []string{"repository", "submodule"} {
			if got := holds(t, m, extension.MenuSCMSourceControl, with(map[string]any{"scmProviderContext": kind}), mine); len(got) != 0 {
				t.Errorf("%s: %v", kind, got)
			}
		}
	})
	t.Run("Compare with Workspace is for the changed files of a worktree", func(t *testing.T) {
		for _, tc := range []struct {
			group, state string
			want         bool
		}{
			{"workingTree", "worktree", true}, {"index", "worktree", true}, {"untracked", "worktree", false}, {"merge", "worktree", false},
			{"workingTree", "repository", false}, {"index", "submodule", false},
		} {
			got := holds(t, m, extension.MenuSCMResourceStateContext, with(map[string]any{"scmResourceGroup": tc.group, "scmResourceState": tc.state}), mine)
			if want := []string{"worktree_diff git.compareWithWorkspace"}; tc.want != slices.Equal(got, want) {
				t.Errorf("%s of a %s: %v", tc.group, tc.state, got)
			}
		}
	})
	t.Run("the palette", func(t *testing.T) {
		palette := map[string]string{}
		for _, it := range m.Contributes.Menus {
			if it.Menu == extension.MenuCommandPalette && mine(it) {
				palette[it.Command] = it.When
			}
		}
		const some = "config.git.enabled && !git.missing && gitOpenRepositoryCount != 0"
		const several = "config.git.enabled && !git.missing && gitOpenRepositoryCount > 1"
		want := map[string]string{
			"git.createWorktree": some, "git.deleteWorktree": some, "git.migrateWorktreeChanges": some, "git.deleteWorktree2": "false",
			"git.openWorktree": several, "git.openWorktreeInNewWindow": several,
		}
		for id, w := range want {
			if palette[id] != w {
				t.Errorf("%s: palette condition %q, want %q", id, palette[id], w)
			}
		}
		// VS Code lists no condition for Compare with Workspace, so it is in the palette whenever the extension is
		if _, ok := palette["git.compareWithWorkspace"]; ok {
			t.Errorf("git.compareWithWorkspace has a palette condition %q", palette["git.compareWithWorkspace"])
		}
	})
}
