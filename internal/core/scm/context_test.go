package scm

import (
	"reflect"
	"testing"
)

func TestContextKeyValues(t *testing.T) {
	m := NewModel(DefaultConfig())
	a := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/a", ContextValue: "repository"})
	b := m.Add(SourceSpec{ProviderID: "svn", Label: "Subversion"})
	ga := a.AddGroup("index", "Staged Changes", false, "staged")
	gb := b.AddGroup("workingTree", "Changes", false, "")
	ga.SetResources([]Resource{{URI: "file:///work/a/x.go", ContextValue: "modified"}, {URI: "file:///work/a/y.go"}})
	gb.SetResources(resources("/n/z.go"))

	tests := []struct {
		name string
		got  map[string]any
		want map[string]any
	}{
		{"a source control with a root and a context value", a.Context(), map[string]any{
			"scmProvider": "git", "scmProviderRootUri": "file:///work/a", "scmProviderHasRootUri": true, "scmProviderContext": "repository"}},
		{"a source control with neither", b.Context(), map[string]any{"scmProvider": "svn", "scmProviderRootUri": "", "scmProviderHasRootUri": false}},
		{"a group with a context value", ga.Context(), map[string]any{
			"scmProvider": "git", "scmProviderRootUri": "file:///work/a", "scmProviderHasRootUri": true, "scmProviderContext": "repository",
			"scmResourceGroup": "index", "scmResourceGroupState": "staged", "multiDiffEditorEnableViewChanges": false}},
		{"a group with none", gb.Context(), map[string]any{
			"scmProvider": "svn", "scmProviderRootUri": "", "scmProviderHasRootUri": false, "scmResourceGroup": "workingTree", "multiDiffEditorEnableViewChanges": false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("context = %v\nwant %v", tt.got, tt.want)
			}
		})
	}
	r0, r1 := &ga.Resources()[0], &ga.Resources()[1]
	if c := r0.Context(); c["scmResourceState"] != "modified" || c["scmResourceGroup"] != "index" || c["scmProvider"] != "git" {
		t.Errorf("resource context = %v", c)
	}
	if _, ok := r1.Context()["scmResourceState"]; ok {
		t.Error("a resource without a context value has scmResourceState")
	}

	g := m.Context()
	want := map[string]any{
		"scm.providerCount": 2, "scm.historyProviderCount": 0, "scmRepositoryCount": 2, "scmRepositoryVisibleCount": 2,
		"scmRepositorySortKey": "discoveryTime", "scmRepositorySelectionMode": "multiple", "scmViewMode": "list", "scmViewSortKey": "path",
		"scmActiveRepositoryName": "a", "scmActiveRepositoryBranchName": "", "scmActiveResourceHasChanges": false, "scmActiveResourceRepository": "",
	}
	if !reflect.DeepEqual(g, want) {
		t.Errorf("global context = %v\nwant %v", g, want)
	}
	m.SetActiveResource("file:///work/a/y.go")
	if c := m.Context(); c["scmActiveResourceHasChanges"] != true || c["scmActiveResourceRepository"] != a.Key() {
		t.Errorf("with the active file in the group: %v %v", c["scmActiveResourceHasChanges"], c["scmActiveResourceRepository"])
	}
	m.SetActiveResource("file:///work/a/other.go")
	if c := m.Context(); c["scmActiveResourceHasChanges"] != false || c["scmActiveResourceRepository"] != a.Key() {
		t.Errorf("with another file of the repository: %v %v", c["scmActiveResourceHasChanges"], c["scmActiveResourceRepository"])
	}
	m.Remove(a)
	m.Remove(b)
	if c := m.Context(); c["scm.providerCount"] != 0 || c["scmActiveRepositoryName"] != "" {
		t.Errorf("without source controls: %v", c)
	}
}
