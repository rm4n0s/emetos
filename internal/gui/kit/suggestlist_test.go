package kit

import (
	"slices"
	"testing"
)

func TestSuggestMatcher(t *testing.T) {
	var m SuggestMatcher
	tests := []struct {
		name      string
		word      string
		label     string
		wantOK    bool
		wantStart []int
	}{
		{"an empty word matches everything", "", "checkout", true, nil},
		{"a prefix", "che", "checkout", true, []int{0, 1, 2}},
		{"letters in order", "cko", "checkout", true, []int{3, 4, 5}},
		{"case does not matter", "CHE", "checkout", true, []int{0, 1, 2}},
		{"no match", "xyz", "checkout", false, nil},
		{"the word is longer than the label", "checkouts", "checkout", false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, pos, ok := m.Match(tt.word, tt.label)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantStart != nil && !slices.Equal(pos[:len(tt.wantStart)], tt.wantStart) {
				t.Errorf("positions = %v, want %v first", pos, tt.wantStart)
			}
			_ = score
		})
	}
	// a prefix scores higher than letters spread over the label
	s1, _, _ := m.Match("che", "checkout")
	s2, _, _ := m.Match("che", "archive")
	if s1 <= s2 {
		t.Errorf("the prefix scores %d and the spread match %d", s1, s2)
	}
}

func TestSuggestIconOfARow(t *testing.T) {
	colors := Colors{}
	file := SuggestRow{Kind: 17}
	if code, _ := SuggestIcon(colors, file); code != 0xEA7B {
		t.Errorf("a file's icon is %#x", code)
	}
	if code, _ := SuggestIcon(colors, SuggestRow{Kind: 9999}); code != 0xEB65 {
		t.Errorf("a kind with no icon is a property: %#x", code)
	}
	if code, _ := SuggestIcon(colors, SuggestRow{Kind: 17, Icon: "history"}); code == 0xEA7B || code == 0 {
		t.Errorf("a named Codicon replaces the kind's: %#x", code)
	}
	if code, _ := SuggestIcon(colors, SuggestRow{Kind: 17, Icon: "no-such-icon"}); code != 0xEA7B {
		t.Errorf("an unknown name keeps the kind's icon: %#x", code)
	}
}
