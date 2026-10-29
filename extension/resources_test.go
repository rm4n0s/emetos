package extension

import (
	"reflect"
	"testing"
)

func TestResourceURIs(t *testing.T) {
	tests := []struct {
		name string
		args any
		want []string
	}{
		{"nothing", nil, nil},
		{"not an object", "file:///a", nil},
		{"the pressed row only", map[string]any{"uri": "file:///w/Dockerfile"}, []string{"file:///w/Dockerfile"}},
		{"the pressed row and the selection, the pressed one first, no repeats", map[string]any{"uri": "file:///w/b", "uris": []any{"file:///w/a", "file:///w/b", "file:///w/c"}},
			[]string{"file:///w/b", "file:///w/a", "file:///w/c"}},
		{"the selection as strings", map[string]any{"uris": []string{"file:///w/a", "file:///w/a"}}, []string{"file:///w/a"}},
		{"things that are not URIs", map[string]any{"uri": 3, "uris": []any{1, "", "file:///x"}}, []string{"file:///x"}},
		{"another argument of a command", map[string]any{"groupId": 2.0}, nil},
	}
	for _, tc := range tests {
		if got := ResourceURIs(tc.args); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
