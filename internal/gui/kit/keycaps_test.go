package kit

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"
)

func TestKeyCaps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chords [][]string
		opts   KeyCapsOpts
		texts  map[string]int
	}{
		{"one key", [][]string{{"Enter"}}, KeyCapsOpts{}, map[string]int{"Enter": 1, "+": 0}},
		{"a chord of two keys", [][]string{{"Ctrl", "K"}}, KeyCapsOpts{}, map[string]int{"Ctrl": 1, "K": 1, "+": 1}},
		{"two chords", [][]string{{"Ctrl", "K"}, {"Ctrl", "S"}}, KeyCapsOpts{}, map[string]int{"Ctrl": 2, "K": 1, "S": 1, "+": 2}},
		{"highlighted", [][]string{{"Ctrl", "K"}}, KeyCapsOpts{Highlight: func(chord, key int) bool { return chord == 0 && key == 1 }}, map[string]int{"Ctrl": 1, "K": 1}},
		{"lit", [][]string{{"Shift", "A"}}, KeyCapsOpts{Lit: true}, map[string]int{"Shift": 1, "A": 1}},
		{"no keys", nil, KeyCapsOpts{}, map[string]int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := pumpControl(t, widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
				return NewKeyCaps(bc, tc.chords, tc.opts)
			}))
			for text, want := range tc.texts {
				if got := ts.Count(gluttertest.ByText(text)); got != want {
					t.Errorf("%q shown %d times, want %d", text, got, want)
				}
			}
		})
	}
}
