package editorgroup

import (
	"encoding/json"
	"slices"
	"testing"
)

// The state that a window keeps per input (its view state) goes through Serialize and Restore untouched, and
// an input that cannot be restored is left out with its state.
func TestSerializeWithKeepsTheStateOfEachInput(t *testing.T) {
	m := NewModel(1, DefaultGridOptions().Model)
	a, b, c := &testInput{id: "a"}, &testInput{id: "b"}, &testInput{id: "c"}
	m.OpenEditor(a, OpenOptions{Pinned: true, Active: true})
	m.OpenEditor(b, OpenOptions{Pinned: true, Active: true})
	m.OpenEditor(c, OpenOptions{Pinned: true, Active: true})
	state := func(e EditorInput) json.RawMessage { return json.RawMessage(`{"of":"` + e.Name() + `"}`) }

	saved := m.SerializeWith(state)
	var states []string
	for _, in := range saved.Editors {
		states = append(states, string(in.State))
	}
	if want := []string{`{"of":"a"}`, `{"of":"b"}`, `{"of":"c"}`}; !slices.Equal(states, want) {
		t.Fatalf("states %v, want %v", states, want)
	}
	if plain := m.Serialize(); plain.Editors[0].State != nil {
		t.Errorf("Serialize without a function gave a state: %s", plain.Editors[0].State)
	}

	// through JSON, as the window state file does
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var again SerializedModel
	if err := json.Unmarshal(data, &again); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	restored := RestoreModelWith(again, DefaultGridOptions().Model, func(in SerializedInput) (EditorInput, bool) {
		if in.Value == "b" {
			return nil, false // its file is gone
		}
		got[in.Value] = string(in.State)
		return &testInput{id: in.Value}, true
	})
	if want := map[string]string{"a": `{"of":"a"}`, "c": `{"of":"c"}`}; len(got) != 2 || got["a"] != want["a"] || got["c"] != want["c"] {
		t.Errorf("states given to the factory %v, want %v", got, want)
	}
	var names []string
	for _, e := range restored.Editors(Sequential, false) {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"a", "c"}) || restored.ActiveEditor().Name() != "c" {
		t.Errorf("editors %v, active %v", names, restored.ActiveEditor())
	}
}
