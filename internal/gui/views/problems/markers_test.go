package problems

import (
	"reflect"
	"sync"
	"testing"
)

func mk(sev Severity, msg string, line, col int) Marker {
	return Marker{Severity: sev, Message: msg, StartLine: line, StartColumn: col, EndLine: line, EndColumn: col + 1}
}

func messages(ms []Marker) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Message
	}
	return out
}

func TestServiceSetReplacesAndRemoves(t *testing.T) {
	tests := []struct {
		name  string
		steps func(s *Service)
		read  ReadFilter
		want  []string
		stats Stats
	}{
		{
			name: "set then read",
			steps: func(s *Service) {
				s.Set("go", "/a.go", []Marker{mk(SeverityError, "e1", 3, 1), mk(SeverityWarning, "w1", 1, 1)})
			},
			want:  []string{"e1", "w1"},
			stats: Stats{Errors: 1, Warnings: 1},
		},
		{
			name: "set replaces the markers of the owner for the resource",
			steps: func(s *Service) {
				s.Set("go", "/a.go", []Marker{mk(SeverityError, "old", 1, 1)})
				s.Set("go", "/a.go", []Marker{mk(SeverityInfo, "new", 2, 2)})
			},
			want:  []string{"new"},
			stats: Stats{Infos: 1},
		},
		{
			name: "owners are independent",
			steps: func(s *Service) {
				s.Set("go", "/a.go", []Marker{mk(SeverityError, "from go", 1, 1)})
				s.Set("lint", "/a.go", []Marker{mk(SeverityError, "from lint", 2, 1)})
			},
			want:  []string{"from go", "from lint"},
			stats: Stats{Errors: 2},
		},
		{
			name: "an empty list removes",
			steps: func(s *Service) {
				s.Set("go", "/a.go", []Marker{mk(SeverityError, "e", 1, 1)})
				s.Set("go", "/a.go", nil)
			},
			want: nil,
		},
		{
			name: "remove by owner takes all its resources",
			steps: func(s *Service) {
				s.Set("go", "/a.go", []Marker{mk(SeverityError, "a", 1, 1)})
				s.Set("go", "/b.go", []Marker{mk(SeverityError, "b", 1, 1)})
				s.Set("lint", "/b.go", []Marker{mk(SeverityWarning, "lint", 1, 1)})
				s.Remove("go")
			},
			want:  []string{"lint"},
			stats: Stats{Warnings: 1},
		},
		{
			name: "remove by owner and resources",
			steps: func(s *Service) {
				s.Set("go", "/a.go", []Marker{mk(SeverityError, "a", 1, 1)})
				s.Set("go", "/b.go", []Marker{mk(SeverityError, "b", 1, 1)})
				s.Remove("go", "/a.go")
			},
			want:  []string{"b"},
			stats: Stats{Errors: 1},
		},
		{
			name: "a marker with no message is dropped",
			steps: func(s *Service) {
				s.Set("go", "/a.go", []Marker{mk(SeverityError, "", 1, 1), mk(SeverityError, "kept", 1, 1)})
			},
			want:  []string{"kept"},
			stats: Stats{Errors: 1},
		},
		{
			name: "sorted by severity then resource then position",
			steps: func(s *Service) {
				s.Set("o", "/b.go", []Marker{mk(SeverityError, "b-err-2", 2, 1), mk(SeverityError, "b-err-1", 1, 5), mk(SeverityInfo, "b-info", 1, 1)})
				s.Set("o", "/a.go", []Marker{mk(SeverityWarning, "a-warn", 9, 1), mk(SeverityError, "a-err", 7, 1)})
			},
			want:  []string{"a-err", "b-err-1", "b-err-2", "a-warn", "b-info"},
			stats: Stats{Errors: 3, Warnings: 1, Infos: 1},
		},
		{
			name: "filter by severity",
			steps: func(s *Service) {
				s.Set("o", "/a.go", []Marker{mk(SeverityError, "e", 1, 1), mk(SeverityWarning, "w", 2, 1), mk(SeverityHint, "h", 3, 1)})
			},
			read:  ReadFilter{Severities: Severities(SeverityWarning | SeverityHint)},
			want:  []string{"w", "h"},
			stats: Stats{Errors: 1, Warnings: 1, Unknowns: 1},
		},
		{
			name: "filter by owner, resource and take",
			steps: func(s *Service) {
				s.Set("go", "/a.go", []Marker{mk(SeverityError, "g1", 1, 1), mk(SeverityError, "g2", 2, 1)})
				s.Set("lint", "/a.go", []Marker{mk(SeverityError, "l1", 3, 1)})
				s.Set("go", "/b.go", []Marker{mk(SeverityError, "other", 1, 1)})
			},
			read:  ReadFilter{Owner: "go", Resource: "/a.go", Take: 1},
			want:  []string{"g1"},
			stats: Stats{Errors: 4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService()
			tt.steps(s)
			if got := messages(s.Read(tt.read)); !reflect.DeepEqual(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Errorf("Read = %v, want %v", got, tt.want)
			}
			if got := s.Stats(); got != tt.stats {
				t.Errorf("Stats = %+v, want %+v", got, tt.stats)
			}
		})
	}
}

func TestServiceSanitizesTheRange(t *testing.T) {
	tests := []struct {
		name string
		in   Marker
		want Marker
	}{
		{"zero positions count from 1", Marker{Message: "m"}, Marker{Message: "m", StartLine: 1, StartColumn: 1, EndLine: 1, EndColumn: 1}},
		{"the end is not before the start", Marker{Message: "m", StartLine: 5, StartColumn: 3, EndLine: 2, EndColumn: 0}, Marker{Message: "m", StartLine: 5, StartColumn: 3, EndLine: 5, EndColumn: 3}},
		{"a valid range stays", Marker{Message: "m", StartLine: 2, StartColumn: 4, EndLine: 3, EndColumn: 8}, Marker{Message: "m", StartLine: 2, StartColumn: 4, EndLine: 3, EndColumn: 8}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService()
			s.Set("o", "/a", []Marker{tt.in})
			got := s.Read(ReadFilter{})[0]
			tt.want.Owner, tt.want.Resource = "o", "/a"
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestServiceChangeEvents(t *testing.T) {
	tests := []struct {
		name  string
		steps func(s *Service)
		want  [][]string
	}{
		{"set tells the resource", func(s *Service) { s.Set("o", "/a", []Marker{mk(SeverityError, "m", 1, 1)}) }, [][]string{{"/a"}}},
		{"setting nothing where nothing was tells nobody", func(s *Service) { s.Set("o", "/a", nil) }, nil},
		{"setting only bad markers where nothing was tells nobody", func(s *Service) { s.Set("o", "/a", []Marker{{}}) }, nil},
		{"removing tells the resources in order", func(s *Service) {
			s.Set("o", "/b", []Marker{mk(SeverityError, "m", 1, 1)})
			s.Set("o", "/a", []Marker{mk(SeverityError, "m", 1, 1)})
			s.Remove("o")
		}, [][]string{{"/b"}, {"/a"}, {"/a", "/b"}}},
		{"removing an unknown owner tells nobody", func(s *Service) { s.Remove("nobody") }, nil},
		{"clearing a resource tells it", func(s *Service) {
			s.Set("o", "/a", []Marker{mk(SeverityError, "m", 1, 1)})
			s.Set("o", "/a", nil)
		}, [][]string{{"/a"}, {"/a"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService()
			var got [][]string
			s.Subscribe(func(c Change) { got = append(got, c.Resources) })
			tt.steps(s)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("changes = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceUnsubscribeAndConcurrency(t *testing.T) {
	s := NewService()
	var mu sync.Mutex
	n := 0
	stop := s.Subscribe(func(Change) { mu.Lock(); n++; mu.Unlock() })
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Set("o", string(rune('a'+i)), []Marker{mk(SeverityError, "m", 1, 1)})
			_ = s.Read(ReadFilter{})
			_ = s.Stats()
		}()
	}
	wg.Wait()
	stop()
	s.Set("o", "/z", []Marker{mk(SeverityError, "m", 1, 1)})
	mu.Lock()
	defer mu.Unlock()
	if n != 8 {
		t.Errorf("subscriber heard %d changes, want 8 (none after unsubscribing)", n)
	}
	if got := s.Stats().Errors; got != 9 {
		t.Errorf("errors = %d, want 9", got)
	}
}

func TestSeverityNames(t *testing.T) {
	tests := []struct {
		sev  Severity
		name string
	}{{SeverityError, "Error"}, {SeverityWarning, "Warning"}, {SeverityInfo, "Info"}, {SeverityHint, ""}}
	for _, tt := range tests {
		if got := tt.sev.String(); got != tt.name {
			t.Errorf("%d.String() = %q, want %q", tt.sev, got, tt.name)
		}
	}
	for _, name := range []string{"error", "warning", "info", "hint"} {
		if _, ok := ParseSeverity(name); !ok {
			t.Errorf("ParseSeverity(%q) failed", name)
		}
	}
	if _, ok := ParseSeverity("fatal"); ok {
		t.Error("ParseSeverity(fatal) succeeded")
	}
}
