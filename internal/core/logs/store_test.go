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

func TestStoreClear(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(s *Store)
		clear      string
		wantLines  []string
		wantNames  []string
		wantEvents []Event
	}{
		{
			name:       "a channel with lines is emptied and stays",
			setup:      func(s *Store) { s.Append("a", "1"); s.Append("a", "2"); s.Append("b", "3") },
			clear:      "a",
			wantLines:  nil,
			wantNames:  []string{"a", "b"},
			wantEvents: []Event{{Kind: EventCleared, Channel: "a"}},
		},
		{
			name:      "an unknown channel is left alone",
			setup:     func(s *Store) { s.Append("a", "1") },
			clear:     "none",
			wantLines: nil,
			wantNames: []string{"a"},
		},
		{
			name:      "an empty channel tells nobody",
			setup:     func(s *Store) { s.Append("a", "1"); s.Clear("a") },
			clear:     "a",
			wantLines: nil,
			wantNames: []string{"a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore(0)
			tt.setup(s)
			var events []Event
			s.Subscribe(func(e Event) { events = append(events, e) })
			s.Clear(tt.clear)
			if got := s.Lines(tt.clear); len(got) != 0 {
				t.Errorf("Lines = %v, want none", got)
			}
			if got := s.Names(); !reflect.DeepEqual(got, tt.wantNames) {
				t.Errorf("Names = %v, want %v", got, tt.wantNames)
			}
			if !reflect.DeepEqual(events, tt.wantEvents) {
				t.Errorf("events = %v, want %v", events, tt.wantEvents)
			}
		})
	}
}

func TestStoreTailsAfterClear(t *testing.T) {
	s := NewStore(3)
	if s.MaxLines() != 3 {
		t.Errorf("MaxLines = %d, want 3", s.MaxLines())
	}
	var seen []string
	s.Subscribe(func(e Event) {
		if e.Kind == EventLine {
			seen = append(seen, e.Line)
		}
	})
	s.Append("c", "1")
	s.Clear("c")
	s.Append("c", "2")
	s.Append("c", "3")
	if want := []string{"1", "2", "3"}; !reflect.DeepEqual(seen, want) {
		t.Errorf("tailed %v, want %v", seen, want)
	}
	if got, want := s.Lines("c"), []string{"2", "3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Lines = %v, want %v", got, want)
	}
}

func TestStoreWatch(t *testing.T) {
	tests := []struct {
		name      string
		before    []string
		after     []string
		max       int
		wantLines []string
		wantTail  []string
	}{
		{"a channel with lines then more", []string{"1", "2"}, []string{"3", "4"}, 10, []string{"1", "2"}, []string{"3", "4"}},
		{"a new channel", nil, []string{"a"}, 10, nil, []string{"a"}},
		{"only the last lines of a long channel", []string{"1", "2", "3", "4"}, []string{"5"}, 2, []string{"3", "4"}, []string{"5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore(tt.max)
			for _, l := range tt.before {
				s.Append("c", l)
			}
			var tail []string
			lines, stop := s.Watch("c", func(e Event) {
				if e.Kind == EventLine && e.Channel == "c" {
					tail = append(tail, e.Line)
				}
			})
			for _, l := range tt.after {
				s.Append("c", l)
			}
			stop()
			s.Append("c", "after stop")
			if len(lines) != len(tt.wantLines) || (len(lines) > 0 && !reflect.DeepEqual(lines, tt.wantLines)) {
				t.Errorf("lines = %v, want %v", lines, tt.wantLines)
			}
			if !reflect.DeepEqual(tail, tt.wantTail) {
				t.Errorf("tail = %v, want %v (nothing after stopping)", tail, tt.wantTail)
			}
		})
	}
}

func TestStoreWatchSeesEveryLineOnceUnderConcurrentAppends(t *testing.T) {
	s := NewStore(100000)
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 200 {
				s.Append("c", fmt.Sprintf("%d-%d", g, i))
			}
		}()
	}
	var mu sync.Mutex
	seen := map[string]int{}
	lines, stop := s.Watch("c", func(e Event) {
		if e.Kind == EventLine {
			mu.Lock()
			seen[e.Line]++
			mu.Unlock()
		}
	})
	wg.Wait()
	stop()
	mu.Lock()
	defer mu.Unlock()
	for _, l := range lines {
		seen[l]++
	}
	if len(seen) != 800 {
		t.Fatalf("saw %d distinct lines, want 800", len(seen))
	}
	for l, n := range seen {
		if n != 1 {
			t.Errorf("line %s seen %d times", l, n)
		}
	}
}
