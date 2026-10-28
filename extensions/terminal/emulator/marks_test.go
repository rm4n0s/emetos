package emulator

import (
	"strings"
	"testing"
)

// markRig is an emulator whose OSC 633 strings get a marker; the markers are kept by the letter after "633;".
type markRig struct {
	*Emulator
	byName map[string]MarkerID
	events []OSCEvent
}

func newMarkRig(rows, cols, scrollback int) *markRig {
	r := &markRig{byName: map[string]MarkerID{}}
	r.Emulator = New(rows, cols, Options{Scrollback: scrollback, MarkCodes: []int{633}, Callbacks: Callbacks{
		OSC: func(ev OSCEvent) {
			r.events = append(r.events, ev)
			if ev.Marker != 0 {
				r.byName[ev.Data] = ev.Marker
			}
		},
	}})
	return r
}

func (r *markRig) feed(s string) {
	if _, err := r.Write([]byte(s)); err != nil {
		panic(err)
	}
}

// lineOf is the text of the line a marker is on ("<gone>" for a marker that is gone).
func (r *markRig) lineOf(name string) string {
	line, ok := r.MarkerLine(r.byName[name])
	if !ok {
		return "<gone>"
	}
	text, _ := r.LineText(int(line - r.FirstLine()))
	return text
}

func TestMarkersFollowTheirLines(t *testing.T) {
	cases := []struct {
		name       string
		rows, cols int
		scrollback int
		in         string
		then       func(r *markRig)
		want       map[string]string // marker name to the text of its line
	}{
		{name: "a marker is on the cursor's line", rows: 4, cols: 20, scrollback: 10,
			in:   "one\r\n\x1b]633;A\x07two\r\n\x1b]633;B\x07three",
			want: map[string]string{"A": "two", "B": "three"}},
		{name: "scrolling into the scrollback keeps the line", rows: 3, cols: 20, scrollback: 10,
			in:   "\x1b]633;A\x07first\r\n2\r\n3\r\n4\r\n5\r\n6",
			want: map[string]string{"A": "first"}},
		{name: "a marker leaves with its line when the scrollback is full", rows: 2, cols: 20, scrollback: 2,
			in:   "\x1b]633;A\x07first\r\n2\r\n3\r\n4\r\n5\r\n6\r\n7",
			want: map[string]string{"A": "<gone>"}},
		{name: "a marker survives the trimming of the lines above it", rows: 2, cols: 20, scrollback: 3,
			in:   "a\r\nb\r\n\x1b]633;A\x07c\r\nd\r\ne\r\nf",
			want: map[string]string{"A": "c"}},
		{name: "ED 3 deletes the markers of the scrollback", rows: 2, cols: 20, scrollback: 10,
			in:   "\x1b]633;A\x07a\r\nb\r\nc\r\nd\x1b[3J",
			want: map[string]string{"A": "<gone>"}},
		{name: "ED 2 keeps the markers of the lines it pushes to the scrollback", rows: 3, cols: 20, scrollback: 10,
			in:   "x\r\n\x1b]633;A\x07prompt\x1b[2J",
			want: map[string]string{"A": "prompt"}},
		{name: "delete lines moves the marker below and deletes the one on a deleted line", rows: 5, cols: 20, scrollback: 10,
			in:   "l1\r\nl2\r\n\x1b]633;B\x07l3\r\n\x1b]633;C\x07l4\r\nl5\x1b[2;1H\x1b[2M",
			want: map[string]string{"B": "<gone>", "C": "l4"}},
		{name: "insert lines moves the marker down", rows: 5, cols: 20, scrollback: 10,
			in:   "l1\r\n\x1b]633;B\x07l2\r\nl3\x1b[1;1H\x1b[1L",
			want: map[string]string{"B": "l2"}},
		{name: "the alternate screen has no markers", rows: 3, cols: 20, scrollback: 10,
			in:   "\x1b[?1049h\x1b]633;A\x07alt",
			want: map[string]string{"A": "<gone>"}},
		{name: "reflow to a narrower width keeps the marker on its text", rows: 4, cols: 20, scrollback: 10,
			in: "\x1b]633;A\x07abcdefghij\r\n\x1b]633;B\x07klmnopqrst\r\n",
			then: func(r *markRig) {
				r.Resize(4, 5)
			},
			want: map[string]string{"A": "abcde", "B": "klmno"}},
		{name: "reflow to a wider width joins rows and keeps the marker on the first", rows: 6, cols: 5, scrollback: 10,
			in: "\x1b]633;A\x07abcdefghij\r\nxy\r\n",
			then: func(r *markRig) {
				r.Resize(6, 10)
			},
			want: map[string]string{"A": "abcdefghij"}},
		{name: "a marker on a wrapped row follows the row's text", rows: 6, cols: 5, scrollback: 10,
			in: "abcdef\x1b]633;A\x07ghij\r\nxy\r\n",
			then: func(r *markRig) {
				r.Resize(6, 4)
			},
			// the marker sat on the second row, which began with "f": that cell is in the second new row, "abcd|efgh|ij"
			want: map[string]string{"A": "efgh"}},
		{name: "a marker where the wrap is still pending stays on the first row", rows: 6, cols: 5, scrollback: 10,
			in: "abcde\x1b]633;A\x07fghij\r\nxy\r\n",
			then: func(r *markRig) {
				r.Resize(6, 4)
			},
			want: map[string]string{"A": "abcd"}},
		{name: "shrinking the rows keeps the markers of the lines pushed to the scrollback", rows: 5, cols: 20, scrollback: 10,
			in: "\x1b]633;A\x07a\r\nb\r\nc\r\nd\r\ne",
			then: func(r *markRig) {
				r.Resize(2, 20)
			},
			want: map[string]string{"A": "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMarkRig(tc.rows, tc.cols, tc.scrollback)
			r.feed(tc.in)
			if tc.then != nil {
				tc.then(r)
			}
			for name, want := range tc.want {
				if got := r.lineOf(name); got != want {
					t.Errorf("marker %s is on %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestMarkerBookkeeping(t *testing.T) {
	r := newMarkRig(3, 20, 10)
	r.feed("\x1b]633;A\x07a\r\n\x1b]633;B\x07b")
	if n := r.Markers(); n != 2 {
		t.Fatalf("Markers = %d, want 2", n)
	}
	for _, ev := range r.events {
		if ev.Marker == 0 {
			t.Errorf("event %q has no marker", ev.Data)
		}
	}
	id := r.byName["A"]
	r.RemoveMarker(id)
	if _, ok := r.MarkerLine(id); ok {
		t.Errorf("a removed marker is still there")
	}
	if n := r.Markers(); n != 1 {
		t.Errorf("Markers = %d, want 1", n)
	}
	// a code that is not in MarkCodes gets no marker
	r.events = nil
	r.feed("\x1b]1337;SetMark\x07")
	if len(r.events) != 1 || r.events[0].Marker != 0 {
		t.Errorf("events = %+v, want one without a marker", r.events)
	}
	// AddMarker marks the cursor's line
	m := r.AddMarker()
	if line, ok := r.MarkerLine(m); !ok || line != r.FirstLine()+1 {
		t.Errorf("AddMarker line = %d (%v), want %d", line, ok, r.FirstLine()+1)
	}
	// a reset forgets them
	r.Reset()
	if n := r.Markers(); n != 0 {
		t.Errorf("Markers after Reset = %d, want 0", n)
	}
}

// TestMarkersSurviveRandomOutput checks the invariant over scrolling, resizing and trimming: a marker, while it lives, is on
// the line whose text it was made on (every line has its own number).
func TestMarkersSurviveRandomOutput(t *testing.T) {
	r := newMarkRig(5, 16, 40)
	var names []string
	rnd := uint32(12345)
	next := func(n int) int {
		rnd = rnd*1664525 + 1013904223
		return int(rnd>>8) % n
	}
	for i := 0; i < 400; i++ {
		name := "m" + strings.Repeat("x", i%7) + string(rune('0'+i%10)) + string(rune('a'+i/10%26)) + string(rune('A'+i/260))
		r.byName[name] = 0
		line := "L" + name + "-" + strings.Repeat("y", next(30))
		r.feed("\x1b]633;" + name + "\x07" + line + "\r\n")
		names = append(names, name)
		if i%37 == 0 {
			r.Resize(3+next(5), 6+next(25))
		}
		for _, n := range names {
			// a marker line may be any row of its text after a reflow, so the row must be part of the line's text
			if got := r.lineOf(n); got != "<gone>" && !strings.Contains("L"+n+"-"+strings.Repeat("y", 30), got) {
				t.Fatalf("step %d: marker %s is on %q", i, n, got)
			}
		}
	}
}

func TestMarkerLines(t *testing.T) {
	r := newMarkRig(3, 20, 2)
	var ids []MarkerID
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		r.feed("\x1b]633;" + n + "\x07" + n + "\r\n")
		ids = append(ids, r.byName[n])
	}
	ids = append(ids, 0, 9999)
	for _, set := range [][]MarkerID{ids, ids[8:]} {
		got := r.MarkerLines(set, nil)
		for i, id := range set {
			want, ok := r.MarkerLine(id)
			if !ok {
				want = -1
			}
			if got[i] != want {
				t.Errorf("set of %d, marker %d: line %d, want %d", len(set), id, got[i], want)
			}
		}
	}
}
