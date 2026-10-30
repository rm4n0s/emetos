package scm

import (
	"reflect"
	"testing"
)

func TestMenuArguments(t *testing.T) {
	m := NewModel(DefaultConfig())
	s := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///repo"})
	idx := s.AddGroup("index", "Staged Changes", false, "staged")
	wt := s.AddGroup("workingTree", "Changes", false, "")
	idx.SetResources([]Resource{{URI: "file:///repo/a.go", ContextValue: "m"}, {URI: "file:///repo/dir/b.go"}, {URI: "file:///repo/dir/sub/c.go"}})
	wt.SetResources(resources("/repo/d.go", "/repo/dir/e.go"))
	key := s.Key()

	res := func(g *Group, i int) Row { return Row{Resource: &g.Resources()[i]} }
	arg := func(group, uri, cv string) map[string]any {
		a := map[string]any{"sourceControl": key, "group": group}
		if uri != "" {
			a["uri"] = uri
		}
		if cv != "" {
			a["contextValue"] = cv
		}
		return a
	}
	tree := idx.Tree()
	var dirRow Row
	for _, e := range tree.Children(tree.Root(), true) {
		if e.Label == "dir" {
			dirRow = Row{Folder: e.Node, FolderGroup: idx}
		}
	}
	if dirRow.Folder == nil {
		t.Fatal("no folder dir")
	}

	tests := []struct {
		name string
		on   Row
		sel  []Row
		want []any
	}{
		{"one resource, nothing selected", res(wt, 0), nil, []any{arg("workingTree", "file:///repo/d.go", "")}},
		{"one resource that is the selection", res(idx, 0), []Row{res(idx, 0)}, []any{arg("index", "file:///repo/a.go", "m")}},
		{"a resource that is not selected runs alone, whatever is selected", res(wt, 1), []Row{res(wt, 0), res(idx, 0)}, []any{arg("workingTree", "file:///repo/dir/e.go", "")}},
		{"many resources of one group", res(idx, 1), []Row{res(idx, 0), res(idx, 1)}, []any{arg("index", "file:///repo/a.go", "m"), arg("index", "file:///repo/dir/b.go", "")}},
		{"resources of two groups, in the order of the selection", res(wt, 0), []Row{res(wt, 1), res(idx, 2), res(wt, 0)}, []any{
			arg("workingTree", "file:///repo/dir/e.go", ""), arg("index", "file:///repo/dir/sub/c.go", ""), arg("workingTree", "file:///repo/d.go", "")}},
		{"a group", Row{Group: idx}, nil, []any{arg("index", "", "staged")}},
		{"a group and a resource selected together", Row{Group: wt}, []Row{{Group: wt}, res(idx, 0)}, []any{arg("workingTree", "", ""), arg("index", "file:///repo/a.go", "m")}},
		{"a folder is the resources below it", dirRow, nil, []any{arg("index", "file:///repo/dir/b.go", ""), arg("index", "file:///repo/dir/sub/c.go", "")}},
		{"a folder and a resource selected together", dirRow, []Row{res(idx, 0), dirRow}, []any{
			arg("index", "file:///repo/a.go", "m"), arg("index", "file:///repo/dir/b.go", ""), arg("index", "file:///repo/dir/sub/c.go", "")}},
		{"an empty row", Row{}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectionMenuArgs(tt.on, tt.sel)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("args = %v\nwant %v", got, tt.want)
			}
		})
	}
	if got := SourceMenuArgs(s); !reflect.DeepEqual(got, []any{map[string]any{"sourceControl": key}}) {
		t.Errorf("source args = %v", got)
	}
}
