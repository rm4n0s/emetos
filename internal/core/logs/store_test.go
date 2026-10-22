package logs

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestStoreAppendLinesNames(t *testing.T) {
	s := NewStore(0)
	if got := s.Lines("none"); len(got) != 0 {
		t.Errorf("Lines of an unknown channel = %v, want none", got)
	}
	s.Append("b", "1")
	s.Append("a", "2")
	s.Append("b", "3")
	if got, want := s.Names(), []string{"b", "a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Names = %v, want %v (creation order)", got, want)
	}
	if got, want := s.Lines("b"), []string{"1", "3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Lines(b) = %v, want %v", got, want)
	}
	// Lines returns a copy.
	got := s.Lines("b")
	got[0] = "changed"
	if s.Lines("b")[0] != "1" {
		t.Error("changing the result of Lines changed the store")
	}
}

func TestStoreKeepsTheLastLines(t *testing.T) {
	tests := []struct {
		name     string
		max      int
		appended int
	}{
		{"below the limit", 5, 3},
		{"at the limit", 5, 5},
		{"just over", 5, 6},
		{"at the bulk trim", 5, 10},
		{"well over", 5, 47},
		{"a limit of one", 1, 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore(tt.max)
			for i := 1; i <= tt.appended; i++ {
				s.Append("c", fmt.Sprint(i))
			}
			from := max(1, tt.appended-tt.max+1)
			var want []string
			for i := from; i <= tt.appended; i++ {
				want = append(want, fmt.Sprint(i))
			}
			if got := s.Lines("c"); !reflect.DeepEqual(got, want) {
				t.Errorf("Lines = %v, want %v", got, want)
			}
			if n := len(s.lines["c"]); n >= 2*tt.max && tt.max > 0 {
				t.Errorf("the store holds %d lines, want fewer than %d", n, 2*tt.max)
			}
		})
	}
}

func TestStoreSubscribe(t *testing.T) {
	s := NewStore(0)
	var first, second []Event
	stopFirst := s.Subscribe(func(e Event) { first = append(first, e) })
	s.Subscribe(func(e Event) {
		// A subscriber may read the store: no lock is held while it runs.
		_ = s.Lines(e.Channel)
		second = append(second, e)
	})

	s.Append("c", "one")
	s.Append("c", "two")
	stopFirst()
	s.Append("d", "three")

	wantFirst := []Event{
		{EventChannelAdded, "c", ""}, {EventLine, "c", "one"}, {EventLine, "c", "two"},
	}
	wantSecond := append(append([]Event{}, wantFirst...),
		Event{EventChannelAdded, "d", ""}, Event{EventLine, "d", "three"})
	if !reflect.DeepEqual(first, wantFirst) {
		t.Errorf("first subscriber got %v, want %v", first, wantFirst)
	}
	if !reflect.DeepEqual(second, wantSecond) {
		t.Errorf("second subscriber got %v, want %v", second, wantSecond)
	}
}

func TestStoreConcurrentAppend(t *testing.T) {
	s := NewStore(100000)
	var seen int
	var mu sync.Mutex
	s.Subscribe(func(e Event) {
		if e.Kind == EventLine {
			mu.Lock()
			seen++
			mu.Unlock()
		}
	})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				s.Append(fmt.Sprint("c", g%2), "line")
				_ = s.Lines("c0")
			}
		}()
	}
	wg.Wait()
	if got := len(s.Lines("c0")) + len(s.Lines("c1")); got != 4000 {
		t.Errorf("%d lines stored, want 4000", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen != 4000 {
		t.Errorf("subscriber saw %d lines, want 4000", seen)
	}
}
