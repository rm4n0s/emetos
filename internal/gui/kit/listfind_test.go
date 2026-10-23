package kit

import (
	"slices"
	"testing"
)

func TestMatchLabel(t *testing.T) {
	cases := []struct {
		name      string
		pattern   string
		label     string
		matchType FindMatchType
		want      []TextRange
		ok        bool
	}{
		{"an empty pattern matches", "", "main.go", FindMatchFuzzy, nil, true},
		{"a part of the label, any case", "MAIN", "main.go", FindMatchFuzzy, []TextRange{{0, 4}}, true},
		{"a part in the middle", "go", "main.go", FindMatchContiguous, []TextRange{{5, 7}}, true},
		{"the first occurrence", "a", "banana", FindMatchContiguous, []TextRange{{1, 2}}, true},
		{"fuzzy: letters in order", "mgo", "main.go", FindMatchFuzzy, []TextRange{{0, 1}, {5, 7}}, true},
		{"fuzzy letters next to each other make one range", "mang", "main.go", FindMatchFuzzy, []TextRange{{0, 2}, {3, 4}, {5, 6}}, true},
		{"fuzzy takes the first letters from the left", "ngo", "nothing.go", FindMatchFuzzy, []TextRange{{0, 1}, {6, 7}, {9, 10}}, true},
		{"fuzzy takes a contiguous part first", "go", "gopher.go", FindMatchFuzzy, []TextRange{{0, 2}}, true},
		{"contiguous does not skip letters", "mgo", "main.go", FindMatchContiguous, nil, false},
		{"letters out of order", "og", "main.go", FindMatchFuzzy, nil, false},
		{"a pattern longer than the label", "main.go.bak", "main.go", FindMatchFuzzy, nil, false},
		{"no letter in common", "zzz", "main.go", FindMatchFuzzy, nil, false},
		{"non-ASCII letters are ranges of bytes", "é", "café", FindMatchContiguous, []TextRange{{3, 5}}, true},
		{"case folding of non-ASCII letters", "É", "café", FindMatchContiguous, []TextRange{{3, 5}}, true},
		{"an empty label", "a", "", FindMatchFuzzy, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := MatchLabel(tc.pattern, tc.label, tc.matchType)
			if ok != tc.ok || !slices.Equal(got, tc.want) {
				t.Errorf("MatchLabel(%q, %q) = %v %v, want %v %v", tc.pattern, tc.label, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestFindStateNoResults(t *testing.T) {
	cases := []struct {
		name string
		s    FindState
		want bool
	}{
		{"closed", FindState{Pattern: "x", Total: 3}, false},
		{"no pattern", FindState{Open: true, Total: 3}, false},
		{"matches", FindState{Open: true, Pattern: "x", Total: 3, MatchCount: 1}, false},
		{"nothing to match", FindState{Open: true, Pattern: "x"}, false},
		{"none match", FindState{Open: true, Pattern: "x", Total: 3}, true},
	}
	for _, tc := range cases {
		if got := tc.s.NoResults(); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}
