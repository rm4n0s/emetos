package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// commandsHolding lists the commands of a menu of the Git extension whose condition holds under the context keys, in the order of the
// manifest.
func commandsHolding(t *testing.T, m extension.Manifest, menu string, keys map[string]any) []string {
	t.Helper()
	scope := contextkey.NewRoot()
	for k, v := range keys {
		scope.Set(k, v)
	}
	var out []string
	for _, it := range m.Contributes.Menus {
		if it.Menu != menu {
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
		out = append(out, it.Command)
	}
	return out
}

// lineCommands keeps the commands by lines out of a list.
func lineCommands(list []string) []string {
	var out []string
	for _, c := range list {
		switch c {
		case "git.stageChange", "git.revertChange", "git.unstageChange", "git.diff.stageHunk", "git.diff.stageSelection",
			"git.stageSelectedRanges", "git.unstageSelectedRanges", "git.revertSelectedRanges", "git.stage", "git.unstage":
			out = append(out, c)
		}
	}
	return out
}

func TestThePeekOffersStageAndRevertForTheIndexVersionOnly(t *testing.T) {
	m := gitManifest(t)
	const index = "emetos-virtual:/git/0123456789ab//src/loader.go"
	base := map[string]any{"config.git.enabled": true, "git.missing": false}
	with := func(original string) map[string]any {
		keys := map[string]any{"originalResource": original}
		for k, v := range base {
			keys[k] = v
		}
		return keys
	}
	tests := []struct {
		name     string
		original string
		want     []string
	}{
		{"the index version", index, []string{"git.stageChange", "git.revertChange"}},
		{"a path with a space and an accent", "emetos-virtual:/git/0123456789ab//dir with space/ünï.go", []string{"git.stageChange", "git.revertChange"}},
		{"HEAD", "emetos-virtual:/git/0123456789ab/HEAD/src/loader.go", nil},
		{"the version the work tree is compared with", "emetos-virtual:/git/0123456789ab/~/src/loader.go", nil},
		{"a document of another extension", "emetos-virtual:/other/x//a.go", nil},
		{"a file", "file:///work/a.go", nil},
	}
	for _, tt := range tests {
		got := lineCommands(commandsHolding(t, m, extension.MenuSCMChangeTitle, with(tt.original)))
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
	// the unstage command is never offered, as VS Code's when is false
	if got := commandsHolding(t, m, extension.MenuSCMChangeTitle, with(index)); slices.Contains(got, "git.unstageChange") {
		t.Error("Unstage Change is offered in the peek")
	}
	// the setting turns them off
	off := with(index)
	off["config.git.enabled"] = false
	if got := lineCommands(commandsHolding(t, m, extension.MenuSCMChangeTitle, off)); len(got) != 0 {
		t.Errorf("the peek offers %v with Git off", got)
	}
}

func TestTheGutterOfTheDiffEditorOffersStageBlockForAWorkTreeDiffOnly(t *testing.T) {
	m := gitManifest(t)
	for _, tt := range []struct {
		name, original string
		want           bool
	}{
		{"a diff of the work tree", "emetos-virtual:/git/0123456789ab/~/src/loader.go", true},
		{"a diff of a staged file", "emetos-virtual:/git/0123456789ab/HEAD/src/loader.go", false},
		{"a diff of two files", "file:///work/a.go", false},
	} {
		keys := map[string]any{"diffEditorOriginalUri": tt.original}
		hunk := commandsHolding(t, m, extension.MenuDiffEditorGutterHunk, keys)
		sel := commandsHolding(t, m, extension.MenuDiffEditorGutterSelection, keys)
		if tt.want != (slices.Equal(hunk, []string{"git.diff.stageHunk"}) && slices.Equal(sel, []string{"git.diff.stageSelection"})) {
			t.Errorf("%s: hunk toolbar %v, selection toolbar %v", tt.name, hunk, sel)
		}
	}
}

func TestTheTitleAndTheContextMenuOfTheRightSideOfADiff(t *testing.T) {
	m := gitManifest(t)
	keys := func(scheme string, over map[string]any) map[string]any {
		k := map[string]any{
			"config.git.enabled": true, "git.missing": false, "gitOpenRepositoryCount": 1, "isInDiffEditor": true, "isInDiffRightEditor": true,
			"isEmbeddedDiffEditor": false, "resourceScheme": scheme,
		}
		for a, b := range over {
			k[a] = b
		}
		return k
	}
	title := func(k map[string]any) []string {
		return lineCommands(commandsHolding(t, m, extension.MenuEditorTitle, k))
	}
	context := func(k map[string]any) []string {
		return lineCommands(commandsHolding(t, m, extension.MenuEditorContext, k))
	}
	if got, want := title(keys("file", nil)), []string{"git.stage", "git.stageSelectedRanges", "git.revertSelectedRanges"}; !slices.Equal(got, want) {
		t.Errorf("title, a work tree diff: %v, want %v", got, want)
	}
	if got, want := title(keys("git", nil)), []string{"git.unstage", "git.unstageSelectedRanges"}; !slices.Equal(got, want) {
		t.Errorf("title, a staged diff: %v, want %v", got, want)
	}
	if got, want := context(keys("file", nil)), []string{"git.stageSelectedRanges", "git.revertSelectedRanges"}; !slices.Equal(got, want) {
		t.Errorf("context menu, a work tree diff: %v, want %v", got, want)
	}
	if got, want := context(keys("git", nil)), []string{"git.unstageSelectedRanges"}; !slices.Equal(got, want) {
		t.Errorf("context menu, a staged diff: %v, want %v", got, want)
	}
	for _, off := range []map[string]any{{"isInDiffRightEditor": false}, {"isEmbeddedDiffEditor": true}, {"isInDiffEditor": false}, {"gitOpenRepositoryCount": 0}} {
		// outside a diff editor Stage is the one command left (its own item of the title, for a file in the work tree)
		got := title(keys("file", off))
		onlyStage := len(got) == 1 && got[0] == "git.stage" && off["isInDiffEditor"] == false
		if len(got) != 0 && !onlyStage {
			t.Errorf("title with %v: %v", off, got)
		}
		if got := context(keys("file", off)); len(got) != 0 {
			t.Errorf("context menu with %v: %v", off, got)
		}
	}
}

func TestTheKeysOfTheCommandsOnTheSelectionAreVSCodes(t *testing.T) {
	m := gitManifest(t)
	want := map[string]struct{ key, when string }{
		"git.stageSelectedRanges":   {"ctrl+k ctrl+alt+s", "editorTextFocus && resourceScheme == file"},
		"git.unstageSelectedRanges": {"ctrl+k ctrl+n", "editorTextFocus && isInDiffEditor && isInDiffRightEditor && resourceScheme == git"},
		"git.revertSelectedRanges":  {"ctrl+k ctrl+r", "editorTextFocus && resourceScheme == file"},
	}
	got := map[string]struct{ key, when string }{}
	for _, k := range m.Contributes.Keybindings {
		if strings.HasPrefix(k.Command, "git.") {
			got[k.Command] = struct{ key, when string }{k.Key, k.When}
		}
	}
	for cmd, w := range want {
		if got[cmd] != w {
			t.Errorf("%s is bound to %+v, want %+v", cmd, got[cmd], w)
		}
		if _, err := contextkey.Parse(w.when); err != nil {
			t.Errorf("the condition of %s: %v", cmd, err)
		}
	}
	// the commands are not in the palette where VS Code hides them, and the palette conditions parse
	for _, it := range m.Contributes.Menus {
		if it.Menu == extension.MenuCommandPalette && strings.HasPrefix(it.Command, "git.") && it.When != "" {
			if _, err := contextkey.Parse(it.When); err != nil {
				t.Errorf("the palette condition of %s: %v", it.Command, err)
			}
		}
	}
}
