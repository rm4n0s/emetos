package langserver

import (
	"encoding/json"
	"testing"
)

func TestSetNestedBuildsMapsAndLeavesOutWhatWouldClash(t *testing.T) {
	m := map[string]any{}
	SetNested(m, []string{"a", "b", "c"}, 1)
	SetNested(m, []string{"a", "b", "d"}, "x")
	SetNested(m, []string{"e"}, true)
	SetNested(m, []string{"a", "b", "c"}, 2)      // taken
	SetNested(m, []string{"e", "f"}, 3)           // e is not a map
	SetNested(m, []string{"a", "b", "c", "z"}, 4) // c is not a map
	SetNested(m, nil, 5)
	got, _ := json.Marshal(m)
	if want := `{"a":{"b":{"c":1,"d":"x"}},"e":true}`; string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
