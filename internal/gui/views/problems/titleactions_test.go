package problems

import "testing"

// The actions of the panel keep their room (VS Code's 200 px for the filter, 150 px at least): a width that depends on the room only,
// never more than the room, and the same when asked again with the width it returned.
func TestTitleActionsWidth(t *testing.T) {
	tests := []struct {
		name string
		room float64
		want float64
	}{
		{"a wide strip: the filter at its widest and the buttons", 2000, buttonsWidth + 200},
		{"exactly the widest", buttonsWidth + 200, buttonsWidth + 200},
		{"less than the widest: the filter shrinks", buttonsWidth + 170, buttonsWidth + 170},
		{"the filter at its least", buttonsWidth + 150, buttonsWidth + 150},
		{"a pixel under the least: no filter, the buttons", buttonsWidth + 149, switchesWidth},
		{"just the buttons", switchesWidth, switchesWidth},
		{"under the buttons: nothing", switchesWidth - 1, 0},
		{"no room", 0, 0},
		{"negative room", -20, 0},
	}
	a := NewActions(nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := a.TitleActionsWidth(tc.room)
			if got != tc.want {
				t.Errorf("TitleActionsWidth(%v) = %v, want %v", tc.room, got, tc.want)
			}
			if got > max(tc.room, 0) {
				t.Errorf("TitleActionsWidth(%v) = %v: more than the room", tc.room, got)
			}
			if again := a.TitleActionsWidth(got); again != got {
				t.Errorf("TitleActionsWidth(%v) = %v, asked again with that width %v", tc.room, got, again)
			}
		})
	}
}
