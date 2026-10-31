package extension

import (
	"slices"
	"testing"
)

func TestQuickDiffArgsRoundTripAndAreReadFromJSON(t *testing.T) {
	changes := []LineChange{{1, 2, 1, 3}, {5, 0, 8, 9}, {12, 12, 14, 0}}
	for index := range changes {
		uri, got, i, ok := ReadQuickDiffArgs(QuickDiffArgs("file:///a/b.go", changes, index))
		if !ok || uri != "file:///a/b.go" || i != index || !slices.Equal(got, changes) {
			t.Errorf("index %d: %q %v %d %v", index, uri, got, i, ok)
		}
	}
	// the arguments of a command that came through JSON have float64 numbers
	json := []any{"file:///x", []any{map[string]any{
		LineChangeOriginalStart: 1.0, LineChangeOriginalEnd: 1.0, LineChangeModifiedStart: 2.0, LineChangeModifiedEnd: 3.0,
	}}, 0.0}
	if _, got, _, ok := ReadQuickDiffArgs(json); !ok || got[0] != (LineChange{1, 1, 2, 3}) {
		t.Errorf("from JSON: %v %v", got, ok)
	}
	// a typed list is read too
	if _, got, _, ok := ReadQuickDiffArgs([]any{"file:///x", changes, 1}); !ok || !slices.Equal(got, changes) {
		t.Errorf("typed list: %v %v", got, ok)
	}
}

func TestReadQuickDiffArgsRefusesWhatIsNotTheArguments(t *testing.T) {
	member := map[string]any{LineChangeOriginalStart: 1, LineChangeOriginalEnd: 1, LineChangeModifiedStart: 1, LineChangeModifiedEnd: 1}
	tests := []struct {
		name string
		args any
	}{
		{"nothing", nil},
		{"a string", "file:///x"},
		{"two elements", []any{"file:///x", []any{}}},
		{"an empty uri", []any{"", []any{member}, 0}},
		{"a uri that is not a string", []any{1, []any{member}, 0}},
		{"an index that is not a number", []any{"file:///x", []any{member}, "0"}},
		{"a fraction as the index", []any{"file:///x", []any{member}, 0.5}},
		{"changes that are not a list", []any{"file:///x", "no", 0}},
		{"a change that is not an object", []any{"file:///x", []any{"no"}, 0}},
		{"a change with a member missing", []any{"file:///x", []any{map[string]any{LineChangeOriginalStart: 1}}, 0}},
		{"a member that is not a number", []any{"file:///x", []any{map[string]any{LineChangeOriginalStart: "1", LineChangeOriginalEnd: 1, LineChangeModifiedStart: 1, LineChangeModifiedEnd: 1}}, 0}},
		{"an index below the list", []any{"file:///x", []any{member}, -1}},
		{"an index past the list", []any{"file:///x", []any{member}, 1}},
		{"no changes", []any{"file:///x", []any{}, 0}},
	}
	for _, tt := range tests {
		if _, _, _, ok := ReadQuickDiffArgs(tt.args); ok {
			t.Errorf("%s: read as the arguments of the peek", tt.name)
		}
	}
}

func TestDiffEditorContextRoundTripsAndRefusesOtherValues(t *testing.T) {
	c := DiffEditorContext{OriginalURI: "emetos-virtual:/git/a/~/x", ModifiedURI: "file:///x", OriginalWithModifiedChanges: "text\n"}
	if got, ok := ReadDiffEditorArgs(c.Args()); !ok || got != c {
		t.Errorf("round trip = %+v, %v", got, ok)
	}
	for _, args := range []any{nil, "x", []any{}, map[string]any{DiffArgOriginalURI: "a"}, map[string]any{DiffArgOriginalURI: "a", DiffArgModifiedURI: "b", DiffArgOriginalWithModifiedChanges: 1}} {
		if _, ok := ReadDiffEditorArgs(args); ok {
			t.Errorf("%v read as the context of a toolbar", args)
		}
	}
	// an empty text is a text (the hunk that empties the file)
	if got, ok := ReadDiffEditorArgs(DiffEditorContext{OriginalURI: "a", ModifiedURI: "b"}.Args()); !ok || got.OriginalWithModifiedChanges != "" {
		t.Errorf("empty text = %+v, %v", got, ok)
	}
}
