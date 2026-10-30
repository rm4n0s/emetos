package scm

import (
	"slices"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// TestKeyboardNavigation checks the keys of the list: the arrows, Home and End move the focus, Left folds a row or goes to its
// parent, Right unfolds it or goes to its first child, Enter opens a file as a kept tab (beside the editor with Control or Alt
// held), Space and a fold row's Enter fold it.
func TestKeyboardNavigation(t *testing.T) {
	r := newRig(t)
	r.addRepo("a", gitGroups()...)
	r.show(goldenThemes[0].colors, ui.Size{Width: 300, Height: 300})
	r.view.Focus()
	r.settle()
	if !r.view.HasFocus() {
		t.Fatal("the list did not take the focus")
	}
	focused := func() string {
		k, ok := r.view.List().Focused()
		if !ok {
			return ""
		}
		return k
	}
	// 0 Merge Changes, 1 loader.go, 2 Staged Changes, 3 loader_test.go, 4 notes.md, 5 Changes, 6 config.go ...
	want := r.rowKeys()
	if len(want) != 10 {
		t.Fatalf("rows %v", want)
	}
	steps := []struct {
		name string
		key  services.LogicalKeyboardKey
		want int
	}{
		{"Home", services.LogicalKeyboardKeyHome, 0},
		{"Down", services.LogicalKeyboardKeyArrowDown, 1},
		{"Down again", services.LogicalKeyboardKeyArrowDown, 2},
		{"Up", services.LogicalKeyboardKeyArrowUp, 1},
		{"End", services.LogicalKeyboardKeyEnd, 9},
		{"Down at the end stays", services.LogicalKeyboardKeyArrowDown, 9},
		{"Up", services.LogicalKeyboardKeyArrowUp, 8},
	}
	for _, s := range steps {
		r.key(s.key)
		if got := focused(); got != want[s.want] {
			t.Fatalf("%s: focus is %q, want row %d (%q)", s.name, got, s.want, want[s.want])
		}
	}

	// Left on a file goes to its group, Left on the open group folds it, Right unfolds it, Right on it again goes to its first file
	r.key(services.LogicalKeyboardKeyArrowLeft)
	if got := focused(); got != want[5] {
		t.Fatalf("Left on a file: focus %q, want the group %q", got, want[5])
	}
	r.key(services.LogicalKeyboardKeyArrowLeft)
	if got := len(r.view.List().Rows()); got != 6 {
		t.Fatalf("Left on an open group left %d rows, want 6", got)
	}
	r.key(services.LogicalKeyboardKeyArrowRight)
	if got := len(r.view.List().Rows()); got != 10 {
		t.Fatalf("Right on a folded group left %d rows, want 10", got)
	}
	r.key(services.LogicalKeyboardKeyArrowRight)
	if got := focused(); got != want[6] {
		t.Fatalf("Right on an open group: focus %q, want its first file %q", got, want[6])
	}

	// Enter on a file opens it as a kept tab with the focus; with Control or Alt it opens beside
	r.key(services.LogicalKeyboardKeyEnter)
	if n := len(r.opens); n == 0 || !r.opens[n-1].o.Pinned || r.opens[n-1].o.Preserve || r.opens[n-1].o.Side || r.opens[n-1].uri != "file:///work/a/notes.txt" {
		t.Fatalf("Enter opened %+v", r.opens)
	}
	r.side = true
	r.key(services.LogicalKeyboardKeyEnter)
	if n := len(r.opens); n < 2 || !r.opens[n-1].o.Side || !r.opens[n-1].o.Pinned {
		t.Errorf("Enter with Control or Alt opened %+v", r.opens)
	}
	r.side = false

	// Enter and Space on a group fold it
	r.key(services.LogicalKeyboardKeyHome)
	r.key(services.LogicalKeyboardKeyEnter)
	if got := len(r.view.List().Rows()); got != 9 {
		t.Errorf("Enter on a group left %d rows, want 9", got)
	}
	r.key(services.LogicalKeyboardKeySpace)
	if got := len(r.view.List().Rows()); got != 10 {
		t.Errorf("Space on a folded group left %d rows, want 10", got)
	}
	r.noErrors()
}

// TestAlwaysShowActions checks scm.alwaysShowActions: the buttons of a file or group row are drawn for the row under the pointer
// or with the focus, and for every row when the setting is on; the buttons of a repository are always drawn.
func TestAlwaysShowActions(t *testing.T) {
	r := newRig(t)
	r.addRepo("a", gitGroups()...)
	r.addRepo("b", group{"workingTree", "Changes", []change{{path: "z.go"}}})
	r.show(goldenThemes[0].colors, ui.Size{Width: 300, Height: 400})
	// the buttons are icons of the Codicon font: count the action bars on screen
	bars := func() int { return r.ts.Count(gluttertest.ByType[*kit.ActionBar]()) }
	repos := bars()
	if repos != 2 {
		t.Fatalf("%d action bars with nothing hovered, want the 2 of the repositories", repos)
	}
	r.always = true
	r.view.Refresh()
	r.settle()
	// every group and file with buttons: merge group none (a "merge" group has none in the fake menus), 1 file, staged group 1 + 2
	// files, changes group 1 + 4 files, the second repository's group 1 + 1 file
	if got := bars(); got != 2+1+3+5+2 {
		t.Errorf("%d action bars with the setting on", got)
	}
	r.always = false
	r.view.Refresh()
	r.settle()
	if got := bars(); got != repos {
		t.Errorf("%d action bars after the setting went off, want %d", got, repos)
	}
	r.noErrors()
}

// TestMenusFollowTheRowContext checks the keys a menu is evaluated with: the provider and the group of a file and a group, so that
// the `when` of an item can choose by them, and the item that is a button against the one that is in the context menu.
func TestMenusFollowTheRowContext(t *testing.T) {
	r := newRig(t)
	var seen []map[string]any
	var menus []string
	r.view.opts.Entries = func(menu string, extra map[string]any) []command.MenuEntry {
		menus = append(menus, menu)
		seen = append(seen, extra)
		return fakeMenus(menu, extra)
	}
	r.addRepo("a", gitGroups()...)
	r.show(goldenThemes[0].colors, ui.Size{Width: 300, Height: 300})
	rows := r.view.List().Rows()
	for _, lr := range rows {
		r.view.buttons(rowOf(lr))
	}
	find := func(menu, group string) map[string]any {
		for i, m := range menus {
			if m == menu && seen[i]["scmResourceGroup"] == group {
				return seen[i]
			}
		}
		return nil
	}
	for _, tc := range []struct{ menu, group string }{
		{command.SCMResourceContext, "merge"}, {command.SCMResourceContext, "index"}, {command.SCMResourceContext, "workingTree"},
		{command.SCMResourceGroupContext, "index"}, {command.SCMResourceGroupContext, "workingTree"},
	} {
		ctx := find(tc.menu, tc.group)
		if ctx == nil {
			t.Errorf("no evaluation of %s for the group %s", tc.menu, tc.group)
			continue
		}
		if ctx["scmProvider"] != "git" {
			t.Errorf("%s/%s: scmProvider = %v", tc.menu, tc.group, ctx["scmProvider"])
		}
	}
	// the items of the context menu are the ones that are not inline buttons: the menu is asked for with the same context
	var got []string
	for _, e := range fakeMenus(command.SCMResourceContext, find(command.SCMResourceContext, "workingTree")) {
		if e.Group == "inline" {
			got = append(got, e.Command)
		}
	}
	if !slices.Equal(got, []string{"git.openFile", "git.clean", "git.stage"}) {
		t.Errorf("buttons of a changed file %v", got)
	}
	r.noErrors()
}
