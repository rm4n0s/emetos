package quickinput

import (
	"fmt"
	"testing"
)

func TestQuickNavigateTriggers(t *testing.T) {
	ctrl := Modifiers{Ctrl: true}
	ctrlShift := Modifiers{Ctrl: true, Shift: true}
	for _, tc := range []struct {
		name     string
		chords   []Modifiers
		released Modifier
		held     Modifiers
		want     bool
	}{
		{"ctrl released", []Modifiers{ctrl}, ModCtrl, Modifiers{}, true},
		{"ctrl released with shift held", []Modifiers{ctrl}, ModCtrl, Modifiers{Shift: true}, true},
		{"shift released for a ctrl chord", []Modifiers{ctrl}, ModShift, Modifiers{Ctrl: true}, false},
		{"alt released for a ctrl chord", []Modifiers{ctrl}, ModAlt, Modifiers{Ctrl: true}, false},
		{"shift released while ctrl is held", []Modifiers{ctrlShift}, ModShift, Modifiers{Ctrl: true}, false},
		{"ctrl released for ctrl+shift", []Modifiers{ctrlShift}, ModCtrl, Modifiers{Shift: true}, true},
		{"shift alone released for a shift chord", []Modifiers{{Shift: true}}, ModShift, Modifiers{}, true},
		{"alt released", []Modifiers{{Alt: true}}, ModAlt, Modifiers{}, true},
		{"meta released", []Modifiers{{Meta: true}}, ModMeta, Modifiers{}, true},
		{"any of the chords", []Modifiers{{Alt: true}, ctrl}, ModCtrl, Modifiers{}, true},
		{"no chords", nil, ModCtrl, Modifiers{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &QuickNavigate{Chords: tc.chords}
			if got := q.triggered(tc.released, tc.held); got != tc.want {
				t.Errorf("triggered = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestQuickNavigateHeldBy(t *testing.T) {
	q := &QuickNavigate{Chords: []Modifiers{{Ctrl: true, Shift: true}}}
	for _, tc := range []struct {
		held Modifiers
		want bool
	}{
		{Modifiers{Ctrl: true, Shift: true}, true},
		{Modifiers{Ctrl: true, Shift: true, Alt: true}, true},
		{Modifiers{Ctrl: true}, false},
		{Modifiers{}, false},
	} {
		if got := q.heldBy(tc.held); got != tc.want {
			t.Errorf("heldBy(%+v) = %v, want %v", tc.held, got, tc.want)
		}
	}
}

func TestPickerItemActivation(t *testing.T) {
	items := func(n int, separators bool) []Item {
		var out []Item
		for i := 0; i < n; i++ {
			if separators && i%2 == 0 {
				out = append(out, Item{Separator: true, Label: fmt.Sprintf("group %d", i)})
			}
			out = append(out, Item{ID: fmt.Sprint(i), Label: fmt.Sprintf("item %d", i)})
		}
		return out
	}
	for _, tc := range []struct {
		name       string
		activation ItemActivation
		n          int
		separators bool
		want       int
	}{
		{"first", ItemFirst, 4, false, 0},
		{"second", ItemSecond, 4, false, 1},
		{"second with one item is the first", ItemSecond, 1, false, 0},
		{"second skips the heading", ItemSecond, 4, true, 1},
		{"last", ItemLast, 4, false, 3},
		{"none", ItemNone, 4, false, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPicker()
			p.SetItemActivation(tc.activation)
			p.SetItems(items(tc.n, tc.separators))
			if got := p.ActiveIndex(); got != tc.want {
				t.Errorf("active = %d, want %d", got, tc.want)
			}
		})
	}
	t.Run("it applies once", func(t *testing.T) {
		p := NewPicker()
		p.SetItemActivation(ItemLast)
		p.SetItems(nil) // no rows yet: it waits for them
		p.SetItems(items(3, false))
		if p.ActiveIndex() != 2 {
			t.Fatalf("active = %d", p.ActiveIndex())
		}
		p.SetItems(items(3, false))
		if p.ActiveIndex() != 0 {
			t.Errorf("active = %d after the items were set again, want the first", p.ActiveIndex())
		}
	})
}
