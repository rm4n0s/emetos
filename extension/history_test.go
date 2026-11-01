package extension

import (
	"reflect"
	"testing"
)

func TestHistoryArgsReadWhatAMenuOfTheGraphGives(t *testing.T) {
	item := map[string]any{
		"id": "a1", "subject": "S", "message": "S\n\nbody", "displayId": "a1", "author": "Ada", "authorEmail": "ada@example.com",
		"parentIds":  []any{"b2", 7, "c3"},
		"references": []any{map[string]any{"id": "refs/heads/main", "name": "main", "revision": "a1", "category": "branches"}, "junk"},
	}
	tests := []struct {
		name   string
		args   any
		wantOK bool
		want   HistoryArg
	}{
		{"nothing", nil, false, HistoryArg{}},
		{"a value that is not a list", "a1", false, HistoryArg{}},
		{"a list with no object of the Graph", []any{"x", 3}, false, HistoryArg{}},
		{"the source control alone", []any{map[string]any{SCMArgSourceControl: "scm1"}}, true, HistoryArg{SourceControl: "scm1"}},
		{"a list of maps", []map[string]any{{SCMArgSourceControl: "scm2"}}, true, HistoryArg{SourceControl: "scm2"}},
		{"a range", []any{map[string]any{SCMArgSourceControl: "scm1"}, map[string]any{HistoryArgRange: map[string]any{"base": "b", "head": "h", "title": "T"}}}, true,
			HistoryArg{SourceControl: "scm1", Range: &HistoryRange{Base: "b", Head: "h", Title: "T"}}},
		{"a reference", []any{map[string]any{HistoryArgRef: "refs/tags/v1"}}, true, HistoryArg{RefID: "refs/tags/v1"}},
		{"a commit with its reference", []any{map[string]any{SCMArgSourceControl: "scm1"}, map[string]any{HistoryArgItem: item}, map[string]any{HistoryArgRef: "refs/heads/main"}}, true,
			HistoryArg{SourceControl: "scm1", RefID: "refs/heads/main", Item: &HistoryItem{
				ID: "a1", ParentIDs: []string{"b2", "c3"}, Subject: "S", Message: "S\n\nbody", DisplayID: "a1", Author: "Ada", AuthorEmail: "ada@example.com",
				References: []HistoryItemRef{{ID: "refs/heads/main", Name: "main", Revision: "a1", Category: "branches"}},
			}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := HistoryArgs(tt.args)
			if ok != tt.wantOK || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("HistoryArgs = %+v, %v\nwant %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestHistoryItemRefFindsAReferenceByID(t *testing.T) {
	it := HistoryItem{References: []HistoryItemRef{{ID: "refs/heads/a", Name: "a"}, {ID: "refs/tags/v1", Name: "v1"}}}
	if r, ok := it.Ref("refs/tags/v1"); !ok || r.Name != "v1" {
		t.Errorf("Ref = %+v, %v", r, ok)
	}
	if _, ok := it.Ref("refs/heads/none"); ok {
		t.Error("found a reference that is not there")
	}
}
