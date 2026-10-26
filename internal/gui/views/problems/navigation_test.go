package problems

import (
	"testing"
)

// nav is a marker of the tests: three characters at a line and a column.
func nav(sev Severity, line, col int, msg string) Marker {
	return Marker{Severity: sev, Message: msg, StartLine: line, StartColumn: col, EndLine: line, EndColumn: col + 3}
}

// navService has markers in two files, a, then b, set in the wrong order.
func navService() *Service {
	svc := NewService()
	svc.Set("o", "/w/b.go", []Marker{nav(SeverityWarning, 2, 1, "b warning"), nav(SeverityError, 9, 1, "b error")})
	svc.Set("o", "/w/a.go", []Marker{
		nav(SeverityInfo, 1, 1, "a info"), nav(SeverityError, 20, 5, "a error late"),
		nav(SeverityError, 10, 1, "a error early"), nav(SeverityHint, 3, 1, "a hint (not in the list)"),
	})
	return svc
}

func TestMarkerListOrderAcrossFiles(t *testing.T) {
	l := NewMarkerList(navService(), "")
	if l.Total() != 5 {
		t.Fatalf("%d markers, hints do not count", l.Total())
	}
	// from a position in no file, the first move goes to the first marker of the file after it by name, and then
	// every move goes to the next: by file, errors first, then by position
	var got []string
	l.Move(true, "/0.go", 1, 1)
	for range l.Total() {
		m, _, _, ok := l.Selected()
		if !ok {
			t.Fatal("nothing selected")
		}
		got = append(got, m.Message)
		l.Move(true, "/0.go", 1, 1)
	}
	want := []string{"a error early", "a error late", "a info", "b error", "b warning"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order = %v, want %v", got, want)
			break
		}
	}
	// the end wraps to the start, and backwards goes the other way round
	if m, i, n, _ := l.Selected(); m.Message != "a error early" || i != 1 || n != 5 {
		t.Errorf("after the last the first comes again: %v %d of %d", m.Message, i, n)
	}
	l.Move(false, "", 0, 0)
	if m, _, _, _ := l.Selected(); m.Message != "b warning" {
		t.Errorf("one back from the first is the last: %v", m.Message)
	}
}

func TestMarkerListStartsFromThePosition(t *testing.T) {
	for _, tc := range []struct {
		name      string
		forward   bool
		resource  string
		line, col int
		list      string
		want      string
	}{
		{"before the first marker of the file", true, "/w/a.go", 1, 1, "", "a error early"},
		{"inside a marker, forward", true, "/w/a.go", 10, 2, "", "a error early"},
		{"between two markers, forward", true, "/w/a.go", 15, 1, "", "a error late"},
		{"between two markers, backward", false, "/w/a.go", 15, 1, "", "a error early"},
		{"after the last one of the file, forward", true, "/w/a.go", 99, 1, "", "a error early"},
		{"after the last one of the file, backward", false, "/w/a.go", 99, 1, "", "b warning"},
		{"a file with no markers, forward", true, "/w/ab.go", 1, 1, "", "b error"},
		{"a file with no markers, backward", false, "/w/ab.go", 1, 1, "", "a info"},
		{"a file after all, forward wraps", true, "/w/z.go", 1, 1, "", "a error early"},
		{"in one file only", true, "/w/b.go", 1, 1, "/w/b.go", "b error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := NewMarkerList(navService(), tc.list)
			l.Move(tc.forward, tc.resource, tc.line, tc.col)
			m, _, _, ok := l.Selected()
			if !ok || m.Message != tc.want {
				t.Errorf("went to %q, want %q", m.Message, tc.want)
			}
		})
	}
}

func TestMarkerListFollowsTheMarkers(t *testing.T) {
	svc := navService()
	l := NewMarkerList(svc, "/w/a.go")
	l.Move(true, "/w/a.go", 1, 1)
	if _, _, _, ok := l.Selected(); !ok {
		t.Fatal("no place")
	}
	if l.Refresh() {
		t.Error("nothing changed")
	}
	svc.Set("o", "/w/a.go", []Marker{nav(SeverityError, 5, 1, "new")})
	if !l.Refresh() || l.Total() != 1 {
		t.Fatalf("refresh = %d markers", l.Total())
	}
	if _, _, _, ok := l.Selected(); ok {
		t.Error("the place survived a change of the markers")
	}
	if !l.Matches("/w/a.go") || l.Matches("") || l.Matches("/w/b.go") {
		t.Error("Matches")
	}
	l.Move(true, "/w/a.go", 9, 9)
	if m, _, _, _ := l.Selected(); m.Message != "new" {
		t.Errorf("went to %q", m.Message)
	}
	empty := NewMarkerList(NewService(), "")
	if empty.Move(true, "/w/a.go", 1, 1) {
		t.Error("moved in an empty list")
	}
}

func TestMarkerHolds(t *testing.T) {
	m := Marker{Resource: "/w/a.go", StartLine: 2, StartColumn: 4, EndLine: 3, EndColumn: 2}
	for _, tc := range []struct {
		line, col int
		want      bool
	}{
		{2, 3, false}, {2, 4, true}, {2, 90, true}, {3, 1, true}, {3, 2, true}, {3, 3, false}, {1, 9, false}, {4, 1, false},
	} {
		if got := m.Holds("/w/a.go", tc.line, tc.col); got != tc.want {
			t.Errorf("holds(%d, %d) = %v, want %v", tc.line, tc.col, got, tc.want)
		}
	}
	if m.Holds("/w/b.go", 2, 4) {
		t.Error("another file")
	}
}
