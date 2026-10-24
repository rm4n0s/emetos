package quickinput

import (
	"reflect"
	"testing"
)

func TestKeyCaps(t *testing.T) {
	tests := []struct {
		label string
		want  [][]string
	}{
		{"", nil},
		{"Ctrl+G", [][]string{{"Ctrl", "G"}}},
		{"Ctrl+Shift+Alt+DownArrow", [][]string{{"Ctrl", "Shift", "Alt", "DownArrow"}}},
		{"F1", [][]string{{"F1"}}},
		{"Ctrl+K Ctrl+C", [][]string{{"Ctrl", "K"}, {"Ctrl", "C"}}},
		{"Ctrl++", [][]string{{"Ctrl", "+"}}},
		{"+", [][]string{{"+"}}},
		{"Ctrl+Shift++", [][]string{{"Ctrl", "Shift", "+"}}},
	}
	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			if got := KeyCaps(tc.label); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
