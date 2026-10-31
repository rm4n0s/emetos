package scm

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/errors"

	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// TestRows checks which rows the view makes for the repositories and their groups: no header for a single repository, a header
// for each of several, or with scm.alwaysShowRepositories; the groups that are hidden when empty are left out; a repository without
// changes lists its groups only when it is the only one.
func TestRows(t *testing.T) {
	tests := []struct {
		name  string
		setup func(r *rig)
		want  []string
	}{
		{"one repository: its groups and files", func(r *rig) {
			r.addRepo("a", group{"index", "Staged Changes", []change{{path: "x.go"}}}, group{"workingTree", "Changes", []change{{path: "y.go"}, {path: "dir/z.go"}}})
		}, []string{"Staged Changes", "x.go", "Changes", "y.go", "z.go"}},
		{"a group that is hidden when empty is left out", func(r *rig) {
			r.addRepo("a", group{"merge", "Merge Changes", nil}, group{"index", "Staged Changes", nil}, group{"workingTree", "Changes", []change{{path: "y.go"}}})
		}, []string{"Changes", "y.go"}},
		{"two repositories have headers", func(r *rig) {
			r.addRepo("a", group{"workingTree", "Changes", []change{{path: "x.go"}}})
			r.addRepo("b", group{"workingTree", "Changes", []change{{path: "y.go"}}})
		}, []string{"a", "Changes", "x.go", "b", "Changes", "y.go"}},
		{"a repository without changes shows no group when it is not the only one", func(r *rig) {
			r.addRepo("a", group{"workingTree", "Changes", nil})
			r.addRepo("b", group{"workingTree", "Changes", []change{{path: "y.go"}}})
		}, []string{"a", "b", "Changes", "y.go"}},
		{"the only repository without changes lists its groups", func(r *rig) {
			r.addRepo("a", group{"workingTree", "Changes", nil})
		}, []string{"Changes"}},
		{"scm.alwaysShowRepositories gives a single repository a header", func(r *rig) {
			r.view.opts.AlwaysShowRepositories = func() bool { return true }
			r.addRepo("a", group{"workingTree", "Changes", []change{{path: "x.go"}}})
		}, []string{"a", "Changes", "x.go"}},
		{"nothing without a repository", func(r *rig) {}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			tt.setup(r)
			r.view.Refresh()
			if got := r.labels(); !slices.Equal(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Errorf("rows = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRowsFollowTheModel checks that a change of the model reaches the rows: new resources, a removed repository, the view mode and
// the sort key.
func TestRowsFollowTheModel(t *testing.T) {
	r := newRig(t)
	src := r.addRepo("a", group{"workingTree", "Changes", []change{{path: "b.go"}, {path: "a.go"}}})
	if got := r.labels(); !slices.Equal(got, []string{"Changes", "a.go", "b.go"}) {
		t.Fatalf("rows = %v", got)
	}
	src.Group("workingTree").SetResources([]scmcore.Resource{{URI: "file:///work/a/new.go"}})
	if got := r.labels(); !slices.Equal(got, []string{"Changes", "new.go"}) {
		t.Errorf("after SetResources: %v", got)
	}
	r.model.SetViewMode(scmcore.ViewTree)
	src.Group("workingTree").SetResources([]scmcore.Resource{{URI: "file:///work/a/d/x.go"}, {URI: "file:///work/a/d/y.go"}})
	if got := r.labels(); !slices.Equal(got, []string{"Changes", "d", "x.go", "y.go"}) {
		t.Errorf("tree mode: %v", got)
	}
	r.model.SetViewMode(scmcore.ViewList)
	r.model.SetSortKey(scmcore.SortByName)
	if got := r.labels(); !slices.Equal(got, []string{"Changes", "x.go", "y.go"}) {
		t.Errorf("list again: %v", got)
	}
	r.model.Remove(src)
	if got := r.labels(); len(got) != 0 || !r.view.Empty() {
		t.Errorf("after Remove: %v, empty %v", got, r.view.Empty())
	}
}

// TestTreeCompactFolders checks the rows of the tree view mode with compact folders: a chain of folders with one folder in it is one
// row; without compact folders each is a row.
func TestTreeCompactFolders(t *testing.T) {
	for _, tt := range []struct {
		name    string
		compact bool
		want    []string
	}{
		{"compact", true, []string{"Changes", "a/b/c", "f.go", "d", "g.go"}},
		{"not compact", false, []string{"Changes", "a", "b", "c", "f.go", "d", "g.go"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			cfg := scmcore.DefaultConfig()
			cfg.CompactFolders = tt.compact
			cfg.DefaultViewMode = scmcore.ViewTree
			r.model.SetConfig(cfg)
			r.addRepo("p", group{"workingTree", "Changes", []change{{path: "a/b/c/f.go"}, {path: "d/g.go"}}})
			if got := r.labels(); !slices.Equal(got, tt.want) {
				t.Errorf("rows = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCollapse checks that a click on a group folds it and another unfolds it, that Left and Right do the same, and that Left
// on a file goes to its group.
func TestCollapse(t *testing.T) {
	r := newRig(t)
	r.addRepo("a", group{"index", "Staged Changes", []change{{path: "x.go"}}}, group{"workingTree", "Changes", []change{{path: "y.go"}, {path: "z.go"}}})
	r.show(goldenThemes[0].colors, ui.Size{Width: 300, Height: 200})
	r.tapRow(0) // Staged Changes
	if got := r.labels(); !slices.Equal(got, []string{"Staged Changes", "Changes", "y.go", "z.go"}) {
		t.Fatalf("after folding the first group: %v", got)
	}
	if !r.view.Collapsed("g:scm1:index") {
		t.Error("the group is not reported as folded")
	}
	r.tapRow(0)
	if got := r.labels(); len(got) != 5 {
		t.Fatalf("after unfolding it: %v", got)
	}
	// the keys: the focus is on the first row
	r.key(services.LogicalKeyboardKeyArrowLeft)
	if got := r.labels(); len(got) != 4 {
		t.Errorf("Left folds the focused group: %v", got)
	}
	r.key(services.LogicalKeyboardKeyArrowRight)
	if got := r.labels(); len(got) != 5 {
		t.Errorf("Right unfolds it: %v", got)
	}
	r.key(services.LogicalKeyboardKeyArrowRight) // to its first child
	if k, _ := r.view.List().Focused(); k != "f:scm1:index:file:///work/a/x.go" {
		t.Errorf("Right on an open group focuses its first child: %q", k)
	}
	r.key(services.LogicalKeyboardKeyArrowLeft) // to the parent
	if k, _ := r.view.List().Focused(); k != "g:scm1:index" {
		t.Errorf("Left on a file focuses its group: %q", k)
	}
	r.noErrors()
}

// TestCollapseRepositories checks the folding of the headers of several repositories and the context keys that tell the title.
func TestCollapseRepositories(t *testing.T) {
	r := newRig(t)
	r.addRepo("a", group{"workingTree", "Changes", []change{{path: "x.go"}}})
	if c, _ := r.view.RepositoryKeys(); c {
		t.Error("one repository has no header to fold")
	}
	r.addRepo("b", group{"workingTree", "Changes", []change{{path: "y.go"}}})
	if c, all := r.view.RepositoryKeys(); !c || all {
		t.Errorf("keys = %v %v, want collapsible and not all folded", c, all)
	}
	r.view.CollapseAllRepositories()
	if got := r.labels(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("all folded: %v", got)
	}
	if _, all := r.view.RepositoryKeys(); !all {
		t.Error("all folded is not reported")
	}
	r.view.ExpandAllRepositories()
	if got := r.labels(); len(got) != 6 {
		t.Errorf("all unfolded: %v", got)
	}
}

// TestFoldedStateIsRemembered checks that the folds are saved with the state of the window and found again by the repository, not
// by the handle it got.
func TestFoldedStateIsRemembered(t *testing.T) {
	r := newRig(t)
	r.addRepo("a", group{"index", "Staged Changes", []change{{path: "x.go"}}}, group{"workingTree", "Changes", []change{{path: "y.go"}}})
	r.tapRowKey("g:scm1:index")
	state := r.view.SaveState()
	if state == nil {
		t.Fatal("no state")
	}
	// a new window: the repository is added again, with another handle
	r2 := newRig(t)
	r2.model.Add(scmcore.SourceSpec{ProviderID: "other", Label: "O", RootURI: "file:///x"}) // takes scm1
	if err := r2.view.RestoreState(state); err != nil {
		t.Fatal(err)
	}
	r2.addRepo("a", group{"index", "Staged Changes", []change{{path: "x.go"}}}, group{"workingTree", "Changes", []change{{path: "y.go"}}})
	r2.model.SetVisible(r2.model.Sources()[1:])
	if got := r2.labels(); !slices.Equal(got, []string{"Staged Changes", "Changes", "y.go"}) {
		t.Errorf("restored rows = %v", got)
	}
	err := r2.view.RestoreState([]byte("{not json"))
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("View.RestoreState.ScmViewStateInvalid") {
		t.Errorf("route = %v", err)
	}
	if err := r2.view.RestoreState(nil); err != nil {
		t.Errorf("no state is no error: %v", err)
	}
	var raw map[string][]string
	if err := json.Unmarshal(state, &raw); err != nil || len(raw["collapsed"]) != 1 {
		t.Errorf("state = %s", state)
	}
}

// tapRowKey folds or unfolds the row of a key as a click does.
func (r *rig) tapRowKey(key string) {
	r.t.Helper()
	it, ok := r.view.List().Item(key)
	if !ok {
		r.t.Fatalf("no row %q: %v", key, r.rowKeys())
	}
	r.view.toggle(it.Data.(*row))
}

// TestClickOpens checks what a click, Enter and a double click on a file do: the command of the resource with how the editor opens
// (preview and focus kept for a click, a kept tab with the focus for Enter), else the file itself.
func TestClickOpens(t *testing.T) {
	r := newRig(t)
	src := r.addRepo("a", group{"workingTree", "Changes", []change{{path: "x.go"}, {path: "y.go"}}})
	// x.go has a command of its own, y.go has none
	rs := slices.Clone(src.Group("workingTree").Resources())
	rs[0].Command = &scmcore.Command{ID: "git.openResource", Args: map[string]any{"id": 7.0}}
	src.Group("workingTree").SetResources(rs)
	r.show(goldenThemes[0].colors, ui.Size{Width: 300, Height: 200})

	r.tapRow(1) // x.go
	want := run{"git.openResource", map[string]any{"id": 7.0, "preserveFocus": true, "pinned": false, "sideBySide": false}}
	if len(r.runs) != 1 || !reflect.DeepEqual(r.runs[0], want) {
		t.Fatalf("a click runs %+v, want %+v", r.runs, want)
	}
	r.runs = nil
	r.key(services.LogicalKeyboardKeyEnter)
	if len(r.runs) == 0 {
		t.Fatal("Enter runs nothing")
	}
	last := r.runs[len(r.runs)-1]
	if m := last.args.(map[string]any); m["pinned"] != true || m["preserveFocus"] != false {
		t.Errorf("Enter opens a kept tab and takes the focus: %+v", last)
	}
	r.tapRow(2) // y.go: no command
	if len(r.opens) != 1 || r.opens[0].uri != "file:///work/a/y.go" || !r.opens[0].o.Preserve || r.opens[0].o.Pinned {
		t.Errorf("a click on a file without a command opens the file as a preview: %+v", r.opens)
	}
	// the repository of the row is the focused one
	if f := r.model.Focused(); f != src {
		t.Errorf("focused = %v", f)
	}
	r.noErrors()
}

func TestWithOpenOptions(t *testing.T) {
	tests := []struct {
		name string
		args any
		o    OpenOptions
		want any
	}{
		{"no argument", nil, OpenOptions{Preserve: true}, map[string]any{"preserveFocus": true, "pinned": false, "sideBySide": false}},
		{"an object keeps its members", map[string]any{"pinned": "mine", "x": 1}, OpenOptions{Pinned: true, Side: true},
			map[string]any{"pinned": "mine", "x": 1, "preserveFocus": false, "sideBySide": true}},
		{"another value stays", "a string", OpenOptions{Pinned: true}, "a string"},
		{"a list stays", []any{1}, OpenOptions{}, []any{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := withOpenOptions(tt.args, tt.o); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestInlineActions checks the buttons of a row: the buttons of the hovered row, of every row with scm.alwaysShowActions, the
// arguments they run with.
func TestInlineActions(t *testing.T) {
	r := newRig(t)
	r.addRepo("a", group{"index", "Staged Changes", []change{{path: "x.go"}}}, group{"workingTree", "Changes", []change{{path: "y.go"}}})
	var rows []*row
	for _, lr := range r.view.List().Rows() {
		rows = append(rows, rowOf(lr))
	}
	ids := func(a []string) []string { return a }
	_ = ids
	got := func(i int) []string {
		var out []string
		for _, a := range r.view.buttons(rows[i]) {
			out = append(out, a.ID)
		}
		return out
	}
	if g := got(1); !slices.Equal(g, []string{"git.openFile", "git.unstage"}) {
		t.Errorf("a staged file: %v", g)
	}
	if g := got(3); !slices.Equal(g, []string{"git.openFile", "git.clean", "git.stage"}) {
		t.Errorf("a changed file: %v", g)
	}
	if g := got(0); !slices.Equal(g, []string{"git.unstageAll"}) {
		t.Errorf("the staged group: %v", g)
	}
	// run on one row
	r.view.runRowAction(rows[3], "git.stage")
	want := []any{scmcore.ResourceArg(rows[3].res)}
	if len(r.runs) != 1 || !reflect.DeepEqual(r.runs[0].args, want) {
		t.Errorf("run = %+v, want args %v", r.runs, want)
	}
}

// TestMenuArguments checks the arguments of the commands run by the menus of a row: the row alone when it is not selected, the
// selected rows when it is one of them, the resources of a folder of the tree.
func TestMenuArguments(t *testing.T) {
	r := newRig(t)
	src := r.addRepo("a", group{"index", "Staged Changes", []change{{path: "x.go"}}}, group{"workingTree", "Changes", []change{{path: "d/y.go"}, {path: "d/z.go"}, {path: "w.go"}}})
	r.show(goldenThemes[0].colors, ui.Size{Width: 300, Height: 200})
	rows := r.view.List().Rows()
	y := func(i int) []any { return r.view.args(rowOf(rows[i])) }
	res := func(g, p string) any {
		for _, rs := range src.Group(g).Resources() {
			if rs.Path == "/work/a/"+p {
				return scmcore.ResourceArg(&rs)
			}
		}
		t.Fatalf("no %s", p)
		return nil
	}
	// nothing selected: the row alone
	if got := y(1); !reflect.DeepEqual(got, []any{res("index", "x.go")}) {
		t.Errorf("one row: %v", got)
	}
	// two selected, one of them is the row: both
	r.view.List().SetSelection([]string{rows[1].Key, rows[3].Key})
	if got := y(3); len(got) != 2 {
		t.Errorf("a selected row works on the selection: %v", got)
	}
	// a row that is not selected: alone
	if got := y(4); len(got) != 1 {
		t.Errorf("a row outside the selection works alone: %v", got)
	}
	// a group header: its own argument
	if got := y(0); !reflect.DeepEqual(got, []any{scmcore.GroupArg(src.Group("index"))}) {
		t.Errorf("a group: %v", got)
	}
	// the context menu selects the row and asks for the menu of its kind
	r.view.showContextMenu(rows[4], ui.Offset{})
	if len(r.menus) != 1 || r.menus[0].menu != "SCMResourceContext" || r.menus[0].extra["scmResourceGroup"] != "workingTree" {
		t.Errorf("menus = %+v", r.menus)
	}
	if !r.view.List().IsSelected(rows[4].Key) {
		t.Error("the row of the menu is selected")
	}
	// a repository header: the source control, the context menu of the repository
	r2 := newRig(t)
	a := r2.addRepo("a", group{"workingTree", "Changes", []change{{path: "x.go"}}})
	r2.addRepo("b", group{"workingTree", "Changes", []change{{path: "y.go"}}})
	rows2 := r2.view.List().Rows()
	if got := r2.view.args(rowOf(rows2[0])); !reflect.DeepEqual(got, []any{scmcore.SourceArg(a)}) {
		t.Errorf("a repository: %v", got)
	}
	r2.view.showContextMenu(rows2[0], ui.Offset{})
	if r2.menus[0].menu != "SCMSourceControl" {
		t.Errorf("menu of a repository: %+v", r2.menus)
	}
}

// TestTreeFolderArguments checks that a command on a folder of the tree view mode gets the resources below it.
func TestTreeFolderArguments(t *testing.T) {
	r := newRig(t)
	r.model.SetViewMode(scmcore.ViewTree)
	r.addRepo("a", group{"workingTree", "Changes", []change{{path: "d/y.go"}, {path: "d/e/z.go"}, {path: "w.go"}}})
	var folder *row
	for _, lr := range r.view.List().Rows() {
		if d := rowOf(lr); d.kind == kindFolder && d.entry.Label == "d" {
			folder = d
		}
	}
	if folder == nil {
		t.Fatalf("no folder: %v", r.labels())
	}
	if got := r.view.args(folder); len(got) != 2 {
		t.Errorf("a folder gives the resources below it: %v", got)
	}
}

// TestAutoReveal checks that revealing a file selects its row, unfolds what holds it, and prefers the last group that has it.
func TestAutoReveal(t *testing.T) {
	r := newRig(t)
	r.model.SetViewMode(scmcore.ViewTree)
	r.addRepo("a", group{"index", "Staged Changes", []change{{path: "d/x.go"}}}, group{"workingTree", "Changes", []change{{path: "d/x.go"}, {path: "d/y.go"}}})
	// fold the folder of the second group and the first group
	r.view.CollapseAll(r.model.Sources()[0].Group("workingTree"))
	r.tapRowKey("g:scm1:index")
	if !r.view.Reveal("file:///work/a/d/x.go") {
		t.Fatal("the file is not found")
	}
	if k, _ := r.view.List().Selected(); k != "f:scm1:workingTree:file:///work/a/d/x.go" {
		t.Errorf("selected %q: the last group wins", k)
	}
	if r.view.Reveal("file:///elsewhere.go") || r.view.Reveal("") {
		t.Error("a file that is in no group is not revealed")
	}
	// the row is not selected again when it is the selected and focused one
	before := r.view.version
	if !r.view.Reveal("file:///work/a/d/x.go") || r.view.version != before {
		t.Error("revealing the selected row again changes nothing")
	}
}

// TestRepositoriesView checks that selecting rows of the Repositories view shows those repositories, that the last one cannot be
// hidden, and that the selection follows the model.
func TestRepositoriesView(t *testing.T) {
	r := newRig(t)
	a := r.addRepo("a", group{"workingTree", "Changes", []change{{path: "x.go"}}})
	b := r.addRepo("b", group{"workingTree", "Changes", []change{{path: "y.go"}}})
	repos := NewRepositories(r.view)
	t.Cleanup(repos.Dispose)
	if keys := repos.List().SelectedKeys(); len(keys) != 2 {
		t.Fatalf("both are shown: %v", keys)
	}
	repos.selectionChanged([]string{"r:" + b.Key()}, nil)
	if vis := r.model.Visible(); len(vis) != 1 || vis[0] != b {
		t.Errorf("visible = %v", names(vis))
	}
	if keys := repos.List().SelectedKeys(); !slices.Equal(keys, []string{"r:" + b.Key()}) {
		t.Errorf("the selection follows the model: %v", keys)
	}
	repos.selectionChanged(nil, nil)
	if len(r.model.Visible()) != 1 {
		t.Error("the last repository stays")
	}
	r.model.ToggleVisibility(a, nil)
	if keys := repos.List().SelectedKeys(); len(keys) != 2 {
		t.Errorf("after the model shows a again: %v", keys)
	}
}

func names(ss []*scmcore.Source) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name()
	}
	return out
}

// TestTreeFoldersTakeTheMarksOfTheirFiles checks that a folder row of the tree view mode shows the dot of the file decorations that bubbled
// up from the files below it (VS Code's fileDecorations { colors: false, badges: true } of a folder), and a folder of plain files does not.
func TestTreeFoldersTakeTheMarksOfTheirFiles(t *testing.T) {
	r := newRig(t)
	r.bubble = true
	r.model.SetViewMode(scmcore.ViewTree)
	r.addRepo("a", group{"workingTree", "Changes", []change{
		{path: "d/e/z.go", letter: "M", tooltip: "Modified", color: colModified},
		{path: "plain/y.go"},
		{path: "w.go", letter: "A", tooltip: "Index Added", color: colAdded},
	}})
	r.show(goldenThemes[0].colors, ui.Size{Width: 300, Height: 200})
	r.settle()
	dots := len(r.ts.Elements(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCircleSmallFilled))))
	if dots != 1 {
		t.Errorf("%d dots, want 1 (the compact folder d/e, not plain)", dots)
	}
	for _, letter := range []string{"M", "A"} {
		if err := gluttertest.FindsOneWidget(r.ts, gluttertest.ByText(letter)); err != nil {
			t.Error(err)
		}
	}
}
