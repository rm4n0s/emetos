package extension

import (
	"reflect"
	"testing"
)

func TestTimelineArgs(t *testing.T) {
	item := map[string]any{"source": "git-history", "id": "abc", "uri": "file:///a.go", "contextValue": "git:file:commit", "label": "Fix", "args": map[string]any{"ref": "abc"}}
	tests := []struct {
		name string
		args any
		want []TimelineArg
	}{
		{"nothing", nil, nil},
		{"a text", "abc", nil},
		{"a list with an item", []any{item}, []TimelineArg{{Source: "git-history", ID: "abc", URI: "file:///a.go", ContextValue: "git:file:commit", Label: "Fix", Args: map[string]any{"ref": "abc"}}}},
		{"maps in a typed list", []map[string]any{item}, []TimelineArg{{Source: "git-history", ID: "abc", URI: "file:///a.go", ContextValue: "git:file:commit", Label: "Fix", Args: map[string]any{"ref": "abc"}}}},
		{"objects without a source and an ID are not items", []any{map[string]any{"uri": "file:///a.go"}, "x", 3}, nil},
		{"two items", []any{item, map[string]any{"source": "s", "id": "2"}}, []TimelineArg{
			{Source: "git-history", ID: "abc", URI: "file:///a.go", ContextValue: "git:file:commit", Label: "Fix", Args: map[string]any{"ref": "abc"}}, {Source: "s", ID: "2"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TimelineArgs(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("TimelineArgs = %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
