package extension

import (
	"reflect"
	"testing"
)

func TestSCMArgs(t *testing.T) {
	src := map[string]any{"sourceControl": "scm1"}
	grp := map[string]any{"sourceControl": "scm1", "group": "index", "contextValue": "staged"}
	res := map[string]any{"sourceControl": "scm1", "group": "index", "uri": "file:///a.go", "contextValue": "m"}
	tests := []struct {
		name string
		args any
		want []SCMArg
	}{
		{"nil", nil, nil},
		{"a value that is no list", "file:///a.go", nil},
		{"the file args of the Explorer", map[string]any{"uri": "file:///a.go"}, nil},
		{"a list of the three kinds", []any{src, grp, res}, []SCMArg{
			{SourceControl: "scm1"}, {SourceControl: "scm1", Group: "index", ContextValue: "staged"},
			{SourceControl: "scm1", Group: "index", URI: "file:///a.go", ContextValue: "m"}}},
		{"a list of maps", []map[string]any{res}, []SCMArg{{SourceControl: "scm1", Group: "index", URI: "file:///a.go", ContextValue: "m"}}},
		{"a list with other values", []any{"x", 3, res}, []SCMArg{{SourceControl: "scm1", Group: "index", URI: "file:///a.go", ContextValue: "m"}}},
		{"members of the wrong type", []any{map[string]any{"sourceControl": 3, "uri": true}}, []SCMArg{{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SCMArgs(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SCMArgs = %+v\nwant %+v", got, tt.want)
			}
		})
	}
	a := SCMArgs([]any{src, grp, res})
	if !a[0].IsSourceControl() || a[0].IsGroup() || a[0].IsResource() || !a[1].IsGroup() || !a[2].IsResource() || a[2].IsSourceControl() {
		t.Error("IsSourceControl, IsGroup and IsResource")
	}
}

// The menu constants have VS Code's MenuId names as values, and no two are the same.
func TestTheSCMMenuIDs(t *testing.T) {
	ids := map[string]string{
		"MenuSCMTitle": MenuSCMTitle, "MenuSCMRepository": MenuSCMRepository, "MenuSCMSourceControl": MenuSCMSourceControl,
		"MenuSCMResourceGroupContext": MenuSCMResourceGroupContext, "MenuSCMResourceStateContext": MenuSCMResourceStateContext,
		"MenuSCMResourceFolderContext": MenuSCMResourceFolderContext, "MenuSCMChangeTitle": MenuSCMChangeTitle,
		"MenuSCMHistoryTitle": MenuSCMHistoryTitle, "MenuSCMHistoryItemContext": MenuSCMHistoryItemContext,
		"MenuSCMHistoryItemRefContext": MenuSCMHistoryItemRefContext, "MenuTimelineItemContext": MenuTimelineItemContext,
		"MenuEditorContent": MenuEditorContent, "MenuEditorTitle": MenuEditorTitle, "MenuDiffEditorGutterHunk": MenuDiffEditorGutterHunk,
		"MenuDiffEditorGutterSelection": MenuDiffEditorGutterSelection, "MenuMultiDiffFileToolbar": MenuMultiDiffFileToolbar,
	}
	want := map[string]string{
		"MenuSCMTitle": "SCMTitle", "MenuSCMRepository": "SCMSourceControlInline", "MenuSCMSourceControl": "SCMSourceControl",
		"MenuSCMResourceGroupContext": "SCMResourceGroupContext", "MenuSCMResourceStateContext": "SCMResourceContext",
		"MenuSCMResourceFolderContext": "SCMResourceFolderContext", "MenuSCMChangeTitle": "SCMChangeContext",
		"MenuSCMHistoryTitle": "SCMHistoryTitle", "MenuSCMHistoryItemContext": "SCMHistoryItemContext",
		"MenuSCMHistoryItemRefContext": "SCMHistoryItemRefContext", "MenuTimelineItemContext": "TimelineItemContext",
		"MenuEditorContent": "EditorContent", "MenuEditorTitle": "EditorTitle", "MenuDiffEditorGutterHunk": "DiffEditorHunkToolbar",
		"MenuDiffEditorGutterSelection": "DiffEditorSelectionToolbar", "MenuMultiDiffFileToolbar": "MultiDiffEditorFileToolbar",
	}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("menu ids = %v\nwant %v", ids, want)
	}
}
