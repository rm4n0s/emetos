package annotations

import (
	"reflect"
	"sync"
	"testing"
)

func TestStore(t *testing.T) {
	type step struct {
		do   func(s *Store)
		uri  string
		want []int // the lines of the annotations of uri, in order
		told [][]string
	}
	a := func(lines ...int) []Annotation {
		var out []Annotation
		for _, l := range lines {
			out = append(out, Annotation{Line: l})
		}
		return out
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{"set and read, sorted by line", []step{
			{do: func(s *Store) { s.Set("x", "k", "file:///a", a(5, 2)) }, uri: "file:///a", want: []int{2, 5}, told: [][]string{{"file:///a"}}},
			{do: func(s *Store) {}, uri: "file:///b", want: nil},
		}},
		{"a key set again replaces its set, also in another document", []step{
			{do: func(s *Store) { s.Set("x", "k", "file:///a", a(1)) }, uri: "file:///a", want: []int{1}, told: [][]string{{"file:///a"}}},
			{do: func(s *Store) { s.Set("x", "k", "file:///b", a(3)) }, uri: "file:///a", want: nil, told: [][]string{{"file:///a", "file:///b"}}},
			{do: func(s *Store) {}, uri: "file:///b", want: []int{3}},
		}},
		{"an empty list removes the set", []step{
			{do: func(s *Store) { s.Set("x", "k", "file:///a", a(1)) }, uri: "file:///a", want: []int{1}, told: [][]string{{"file:///a"}}},
			{do: func(s *Store) { s.Set("x", "k", "file:///a", nil) }, uri: "file:///a", want: nil, told: [][]string{{"file:///a"}}},
		}},
		{"clearing a key that is not there tells nobody", []step{
			{do: func(s *Store) { s.Clear("x", "none") }, uri: "file:///a", want: nil},
		}},
		{"two keys draw together; two owners do not share keys", []step{
			{do: func(s *Store) { s.Set("x", "k", "file:///a", a(4)) }, uri: "file:///a", want: []int{4}, told: [][]string{{"file:///a"}}},
			{do: func(s *Store) { s.Set("y", "k", "file:///a", a(1)) }, uri: "file:///a", want: []int{1, 4}, told: [][]string{{"file:///a"}}},
			{do: func(s *Store) { s.Clear("x", "k") }, uri: "file:///a", want: []int{1}, told: [][]string{{"file:///a"}}},
		}},
		{"the end of an owner takes its sets away", []step{
			{do: func(s *Store) { s.Set("x", "k1", "file:///a", a(4)); s.Set("x", "k2", "file:///b", a(2)) }, uri: "file:///a", want: []int{4}, told: [][]string{{"file:///a"}, {"file:///b"}}},
			{do: func(s *Store) { s.ClearOwner("x") }, uri: "file:///a", want: nil, told: [][]string{{"file:///a", "file:///b"}}},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			var mu sync.Mutex
			var told [][]string
			s.Subscribe(func(uris []string) {
				mu.Lock()
				defer mu.Unlock()
				cp := append([]string(nil), uris...)
				told = append(told, cp)
			})
			for i, st := range tc.steps {
				mu.Lock()
				told = nil
				mu.Unlock()
				st.do(s)
				var got []int
				for _, an := range s.For(st.uri) {
					got = append(got, an.Line)
				}
				if !reflect.DeepEqual(got, st.want) {
					t.Errorf("step %d: lines %v, want %v", i, got, st.want)
				}
				mu.Lock()
				sortedEqual := len(told) == len(st.told)
				if sortedEqual {
					for j := range told {
						a, b := append([]string(nil), told[j]...), append([]string(nil), st.told[j]...)
						if !sameSet(a, b) {
							sortedEqual = false
						}
					}
				}
				if !sortedEqual {
					t.Errorf("step %d: subscribers were told %v, want %v", i, told, st.told)
				}
				mu.Unlock()
			}
		})
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

func TestStoreHasAndUnsubscribe(t *testing.T) {
	s := New()
	calls := 0
	stop := s.Subscribe(func([]string) { calls++ })
	s.Set("x", "k", "file:///a", []Annotation{{Line: 1}})
	if !s.Has("x", "k") || s.Has("x", "other") || s.Has("y", "k") {
		t.Errorf("Has is wrong")
	}
	stop()
	s.Set("x", "k", "file:///a", nil)
	if calls != 1 {
		t.Errorf("a stopped subscriber was called: %d calls", calls)
	}
}
