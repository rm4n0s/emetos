package output

import "testing"

// The dropdown of the channels keeps its room, 60 px at least; the width depends on the room only, is never more than the room, and
// is the same when asked again with the width it returned.
func TestTitleActionsWidth(t *testing.T) {
	tests := []struct {
		name string
		room float64
		want float64
	}{
		{"a wide strip: the dropdown at its widest and the buttons", 2000, buttonsWidth + channelsWidth},
		{"less than the widest: the dropdown shrinks", buttonsWidth + 100, buttonsWidth + 100},
		{"the dropdown at its least", buttonsWidth + channelsMinWidth, buttonsWidth + channelsMinWidth},
		{"a pixel under the least: no dropdown, the buttons", buttonsWidth + channelsMinWidth - 1, buttonsOnlyWidth},
		{"under the buttons: nothing", buttonsOnlyWidth - 1, 0},
		{"no room", 0, 0},
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
