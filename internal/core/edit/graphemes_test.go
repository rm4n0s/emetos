package edit

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestGraphemeBoundaries(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want []int
	}{
		{"empty", "", []int{0}},
		{"ascii", "abc", []int{0, 1, 2, 3}},
		{"tab", "\ta", []int{0, 1, 2}},
		{"greek", "αβγ", []int{0, 2, 4, 6}},
		{"cjk", "日本語", []int{0, 3, 6, 9}},
		{"astral", "a😀b", []int{0, 1, 5, 6}},
		{"combining accent", "éx", []int{0, 3, 4}},
		{"two accents", "é̂x", []int{0, 5, 6}},
		{"carriage return and line feed", "a\r\nb", []int{0, 1, 3, 4}},
		{"two flags", "🇬🇷🇦🇧", []int{0, 8, 16}},
		{"flag and a lone indicator", "🇬🇷🇦", []int{0, 8, 12}},
		{"skin tone", "👍🏽x", []int{0, 8, 9}},
		{"joined family", "👨‍👩‍👧x", []int{0, 18, 19}},
		{"hangul jamo", "각a", []int{0, 9, 10}},
		{"invalid byte counts as one", "a\xffb", []int{0, 1, 2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GraphemeBoundaries(tt.s); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GraphemeBoundaries(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestNextAndPrevGrapheme(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		col        int
		next, prev int
	}{
		{"empty", "", 0, 0, 0},
		{"start of ascii", "abc", 0, 1, 0},
		{"middle of ascii", "abc", 1, 2, 0},
		{"end of ascii", "abc", 3, 3, 2},
		{"greek", "αβγ", 2, 4, 0},
		{"cjk", "日本語", 3, 6, 0},
		{"before an astral character", "a😀b", 1, 5, 0},
		{"after an astral character", "a😀b", 5, 6, 1},
		{"accent stays with its letter forward", "éx", 0, 3, 0},
		{"accent stays with its letter backward", "éx", 3, 4, 0},
		{"flags forward", "🇬🇷🇦🇧", 0, 8, 0},
		{"flags backward", "🇬🇷🇦🇧", 16, 16, 8},
		{"family forward", "👨‍👩‍👧x", 0, 18, 0},
		{"family backward", "👨‍👩‍👧x", 19, 19, 18},
		{"column before the start", "abc", -5, 1, 0},
		{"column after the end", "abc", 50, 3, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextGrapheme(tt.line, tt.col); got != tt.next {
				t.Errorf("NextGrapheme(%q, %d) = %d, want %d", tt.line, tt.col, got, tt.next)
			}
			if got := PrevGrapheme(tt.line, tt.col); got != tt.prev {
				t.Errorf("PrevGrapheme(%q, %d) = %d, want %d", tt.line, tt.col, got, tt.prev)
			}
		})
	}
}

// Moving forward and then backward over a line visits the same boundaries.
func TestGraphemeWalkIsReversible(t *testing.T) {
	lines := []string{"abc", "éé", "🇬🇷🇦🇧🇨", "a😀👨‍👩‍👧\t日b", "각x"}
	for _, line := range lines {
		var forward []int
		for col := 0; ; {
			forward = append(forward, col)
			next := NextGrapheme(line, col)
			if next == col {
				break
			}
			col = next
		}
		var backward []int
		for col := len(line); ; {
			backward = append(backward, col)
			prev := PrevGrapheme(line, col)
			if prev == col {
				break
			}
			col = prev
		}
		for i, j := 0, len(backward)-1; i < j; i, j = i+1, j-1 {
			backward[i], backward[j] = backward[j], backward[i]
		}
		if !reflect.DeepEqual(forward, backward) || !reflect.DeepEqual(forward, GraphemeBoundaries(line)) {
			t.Errorf("%q: forward %v, backward %v, boundaries %v", line, forward, backward, GraphemeBoundaries(line))
		}
	}
}

// In a long line only a window around the column is looked at; for ordinary
// text it must give what the whole line gives.
func TestGraphemeWindowOnLongLines(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	pieces := []string{"a", "bc", "é", "日", "😀", "é", "👨‍👩‍👧", "\t", " ", "🇬🇷"}
	var sb strings.Builder
	for sb.Len() < 20000 {
		sb.WriteString(pieces[rng.Intn(len(pieces))])
	}
	line := sb.String()
	bounds := GraphemeBoundaries(line)
	for i := 0; i < 300; i++ {
		k := rng.Intn(len(bounds) - 1)
		col := bounds[k]
		if got, want := NextGrapheme(line, col), bounds[k+1]; got != want {
			t.Fatalf("NextGrapheme(col %d) = %d, want %d", col, got, want)
		}
		if k > 0 {
			if got, want := PrevGrapheme(line, col), bounds[k-1]; got != want {
				t.Fatalf("PrevGrapheme(col %d) = %d, want %d", col, got, want)
			}
		}
	}
	// A cluster of more than the window is cut but the answer stays inside the line.
	huge := "a" + strings.Repeat("́", 3000) + "b" + strings.Repeat("x", 9000)
	if got := NextGrapheme(huge, 0); got <= 0 || got > len(huge) {
		t.Errorf("NextGrapheme of a huge cluster = %d", got)
	}
}
