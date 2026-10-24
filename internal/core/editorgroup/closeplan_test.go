package editorgroup

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// planEnv is a grid of groups that show documents.
type planEnv struct {
	t      *testing.T
	grid   *Grid
	docs   map[string]*document.Document
	inputs map[string]EditorInput // by "group/name"
}

func newPlanEnv(t *testing.T, groups int, dirty ...string) *planEnv {
	t.Helper()
	e := &planEnv{t: t, grid: NewGrid(DefaultGridOptions()), docs: map[string]*document.Document{}, inputs: map[string]EditorInput{}}
	for i := 1; i < groups; i++ {
		if _, err := e.grid.AddGroup(e.grid.Active(), DirRight); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range dirty {
		e.dirty(name)
	}
	return e
}

// doc is the document of a name: a file, or an untitled document for a name that starts with "Untitled-".
func (e *planEnv) doc(name string) *document.Document {
	if d, ok := e.docs[name]; ok {
		return d
	}
	uri := document.FileURI("/w/" + name)
	if len(name) > 9 && name[:9] == "Untitled-" {
		uri = document.UntitledURI(int(name[9] - '0'))
	}
	d := document.New(uri, "", document.NewOptions())
	if uri.Scheme == document.SchemeFile {
		d = document.New(uri, "text\n", document.NewOptions())
	}
	e.docs[name] = d
	return d
}

// dirty gives a document unsaved changes.
func (e *planEnv) dirty(name string) {
	e.t.Helper()
	if _, err := e.doc(name).Edit([]document.Edit{{Range: buffer.Range{}, Text: "x"}}, document.EditOptions{}); err != nil {
		e.t.Fatal(err)
	}
}

// open shows a document in a group (counted from 1).
func (e *planEnv) open(group int, name string) EditorInput {
	e.t.Helper()
	in := NewTextInput(e.doc(name), TextInputOpts{})
	shown, _ := e.grid.Group(group).Model.OpenEditor(in, OpenOptions{Pinned: true})
	e.inputs[name+"@"+string(rune('0'+group))] = shown
	return shown
}

func (e *planEnv) names(plan []Unsaved) []string {
	var out []string
	for _, u := range plan {
		out = append(out, u.Input.Name())
	}
	return out
}

func TestPlanClose(t *testing.T) {
	type step func(e *planEnv) []CloseTarget
	closeAll := func(e *planEnv) []CloseTarget { return AllTargets(e.grid) }
	tests := []struct {
		name  string
		setup func(e *planEnv)
		close step
		want  []string
	}{
		{"clean editors close silently", func(e *planEnv) {
			e.open(1, "a.go")
			e.open(1, "b.go")
		}, closeAll, nil},
		{"one dirty editor", func(e *planEnv) {
			e.dirty("b.go")
			e.open(1, "a.go")
			e.open(1, "b.go")
		}, closeAll, []string{"b.go"}},
		{"several dirty editors keep the order of the tabs", func(e *planEnv) {
			e.dirty("c.go")
			e.dirty("a.go")
			e.open(1, "a.go")
			e.open(1, "b.go")
			e.open(1, "c.go")
		}, closeAll, []string{"a.go", "c.go"}},
		{"a dirty editor that is not among the closing ones stays out", func(e *planEnv) {
			e.dirty("a.go")
			e.open(1, "a.go")
			e.open(1, "b.go")
		}, func(e *planEnv) []CloseTarget {
			return []CloseTarget{{Group: e.grid.Group(1), Editors: []EditorInput{e.inputs["b.go@1"]}}}
		}, nil},
		{"a dirty document that another group still shows is not lost", func(e *planEnv) {
			e.dirty("a.go")
			e.open(1, "a.go")
			e.open(2, "a.go")
		}, func(e *planEnv) []CloseTarget {
			return []CloseTarget{{Group: e.grid.Group(1), Editors: []EditorInput{e.inputs["a.go@1"]}}}
		}, nil},
		{"closing the last editor of a dirty document asks", func(e *planEnv) {
			e.dirty("a.go")
			e.open(1, "a.go")
			e.open(2, "a.go")
		}, closeAll, []string{"a.go"}},
		{"a document in two groups is listed once", func(e *planEnv) {
			e.dirty("a.go")
			e.dirty("b.go")
			e.open(1, "a.go")
			e.open(2, "a.go")
			e.open(2, "b.go")
		}, closeAll, []string{"a.go", "b.go"}},
		{"closing one group while the other shows the document", func(e *planEnv) {
			e.dirty("a.go")
			e.dirty("b.go")
			e.open(1, "a.go")
			e.open(1, "b.go")
			e.open(2, "a.go")
		}, func(e *planEnv) []CloseTarget {
			g := e.grid.Group(1)
			return []CloseTarget{{Group: g, Editors: g.Model.Editors(Sequential, false)}}
		}, []string{"b.go"}},
		{"an untitled document with text asks, an empty one does not", func(e *planEnv) {
			e.dirty("Untitled-1")
			e.open(1, "Untitled-1")
			e.open(1, "Untitled-2")
		}, closeAll, []string{"Untitled-1"}},
		{"an input that is not in its group is left out", func(e *planEnv) {
			e.dirty("a.go")
			e.open(1, "a.go")
		}, func(e *planEnv) []CloseTarget {
			stray := NewTextInput(e.doc("a.go"), TextInputOpts{})
			return []CloseTarget{{Group: e.grid.Group(1), Editors: []EditorInput{stray}}}
		}, nil},
		{"a target without a group is left out", func(e *planEnv) {
			e.dirty("a.go")
			e.open(1, "a.go")
		}, func(e *planEnv) []CloseTarget {
			return []CloseTarget{{Editors: []EditorInput{e.inputs["a.go@1"]}}}
		}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newPlanEnv(t, 2)
			tc.setup(e)
			plan := PlanClose(e.grid, tc.close(e))
			if got := e.names(plan); !slices.Equal(got, tc.want) {
				t.Errorf("PlanClose = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAllTargetsSkipsEmptyGroups(t *testing.T) {
	e := newPlanEnv(t, 3)
	e.open(1, "a.go")
	e.open(3, "b.go")
	e.open(3, "c.go")
	targets := AllTargets(e.grid)
	if len(targets) != 2 || len(targets[0].Editors) != 1 || len(targets[1].Editors) != 2 {
		t.Errorf("targets = %+v", targets)
	}
}
