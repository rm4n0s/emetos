package kit

import (
	"slices"
	"testing"
)

func TestHistoryNavigation(t *testing.T) {
	type step struct {
		op   string // "add", "next", "previous", "first", "last"
		arg  string
		want string
		ok   bool
	}
	cases := []struct {
		name    string
		start   []string
		limit   int
		steps   []step
		items   []string
		first   bool
		last    bool
		nowhere bool
	}{
		{"empty", nil, 0, []step{{op: "next"}, {op: "previous"}}, nil, false, true, true},
		{"previous from nowhere is the last", []string{"a", "b", "c"}, 0,
			[]step{{"previous", "", "c", true}, {"previous", "", "b", true}, {"previous", "", "a", true}, {"previous", "", "", false}},
			[]string{"a", "b", "c"}, true, false, false},
		{"previous at the first stays", []string{"a", "b"}, 0,
			[]step{{"first", "", "a", true}, {"previous", "", "", false}, {"next", "", "b", true}},
			[]string{"a", "b"}, false, true, false},
		{"next past the last is nowhere", []string{"a", "b"}, 0,
			[]step{{"last", "", "b", true}, {"next", "", "", false}, {"next", "", "", false}, {"previous", "", "b", true}},
			[]string{"a", "b"}, false, true, false},
		{"adding a value again moves it to the end", []string{"a", "b", "c"}, 0,
			[]step{{"add", "a", "", false}, {"previous", "", "a", true}, {"previous", "", "c", true}},
			[]string{"b", "c", "a"}, false, false, false},
		{"adding goes nowhere", []string{"a", "b"}, 0,
			[]step{{"first", "", "a", true}, {"add", "z", "", false}},
			[]string{"a", "b", "z"}, false, false, true},
		{"the oldest values go past the limit", nil, 3,
			[]step{{op: "add", arg: "1"}, {op: "add", arg: "2"}, {op: "add", arg: "3"}, {op: "add", arg: "4"}},
			[]string{"2", "3", "4"}, false, false, true},
		{"the start is cut to the limit", []string{"1", "2", "3", "4", "5"}, 2, nil, []string{"4", "5"}, false, false, true},
		{"a repeated start value is kept once", []string{"a", "b", "a"}, 0, nil, []string{"b", "a"}, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var h *History
			if tc.limit > 0 {
				h = NewHistory(tc.start, tc.limit)
			} else {
				h = NewHistory(tc.start)
			}
			for i, s := range tc.steps {
				var got string
				var ok bool
				switch s.op {
				case "add":
					h.Add(s.arg)
					continue
				case "next":
					got, ok = h.Next()
				case "previous":
					got, ok = h.Previous()
				case "first":
					got, ok = h.First()
				case "last":
					got, ok = h.Last()
				}
				if got != s.want || ok != s.ok {
					t.Errorf("step %d (%s): got %q, %v, want %q, %v", i, s.op, got, ok, s.want, s.ok)
				}
			}
			if !slices.Equal(h.Items(), tc.items) {
				t.Errorf("items %v, want %v", h.Items(), tc.items)
			}
			if h.IsFirst() != tc.first || h.IsLast() != tc.last || h.IsNowhere() != tc.nowhere {
				t.Errorf("first %v last %v nowhere %v, want %v %v %v", h.IsFirst(), h.IsLast(), h.IsNowhere(), tc.first, tc.last, tc.nowhere)
			}
		})
	}
}

func TestHistoryDefaults(t *testing.T) {
	h := NewHistory(nil)
	if h.Limit() != DefaultHistoryLimit || DefaultHistoryLimit != 100 {
		t.Errorf("limit %d", h.Limit())
	}
	for i := range 150 {
		h.Add(string(rune('A'+i%26)) + string(rune('a'+i/26)))
	}
	if h.Len() != 100 {
		t.Errorf("kept %d values, want 100", h.Len())
	}
	if !h.Has("Ff") || h.Has("Aa") {
		t.Errorf("has the newest %v, the oldest %v", h.Has("Ff"), h.Has("Aa"))
	}
	h.Clear()
	if h.Len() != 0 || !h.IsNowhere() {
		t.Errorf("after Clear: %d values", h.Len())
	}
}

func TestHistoryAddReturnsTheNavigatorToTheEnd(t *testing.T) {
	h := NewHistory([]string{"a", "b", "c"})
	h.First()
	h.Add("d")
	if cur, ok := h.Current(); ok {
		t.Errorf("current %q, want nowhere", cur)
	}
	if v, _ := h.Previous(); v != "d" {
		t.Errorf("previous %q, want d", v)
	}
}
