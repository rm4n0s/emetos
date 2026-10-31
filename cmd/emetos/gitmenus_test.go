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
