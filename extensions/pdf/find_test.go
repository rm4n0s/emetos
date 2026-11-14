package pdf

import (
	"math"
	"testing"

	"github.com/rm4n0s/emetos/extensions/pdf/poppler"
)

// line makes a line of words of 10 points a character, a point apart, at a height.
func line(y float64, words ...string) poppler.Line {
	var l poppler.Line
	x := 0.0
	for _, w := range words {
		n := float64(len([]rune(w)))
		l.Words = append(l.Words, poppler.Word{Text: w, XMin: x, YMin: y, XMax: x + 10*n, YMax: y + 12})
		x += 10*n + 5
	}
	return l
}

func near(a, b box) bool {
	return math.Abs(a.x0-b.x0) < 1e-9 && math.Abs(a.x1-b.x1) < 1e-9 && a.y0 == b.y0 && a.y1 == b.y1
}

func TestFindingInAPage(t *testing.T) {
	words := poppler.PageWords{Width: 612, Height: 792, Lines: []poppler.Line{
		line(0, "A", "drawing", "of", "Drawings"),
		line(20, "draw", "ing"),
		line(40, "Ωmega", "ΩMEGA"),
	}}
	tests := []struct {
		name      string
		text      string
		matchCase bool
		want      [][]box
	}{
		{"one word, any case", "drawing", false, [][]box{
			{{x0: 15, y0: 0, x1: 85, y1: 12}},
			{{x0: 115, y0: 0, x1: 185, y1: 12}}, // the first 7 of "Drawings"
		}},
		{"match case", "Drawing", true, [][]box{{{x0: 115, y0: 0, x1: 185, y1: 12}}}},
		{"across words of a line", "drawing of d", false, [][]box{
			{{x0: 15, y0: 0, x1: 85, y1: 12}, {x0: 90, y0: 0, x1: 110, y1: 12}, {x0: 115, y0: 0, x1: 125, y1: 12}},
		}},
		{"a part of a word", "aw", false, [][]box{
			{{x0: 35, y0: 0, x1: 55, y1: 12}},
			{{x0: 135, y0: 0, x1: 155, y1: 12}},
			{{x0: 20, y0: 20, x1: 40, y1: 32}},
		}},
		{"not across lines", "ing draw", false, nil},
		{"two words of the next line", "draw ing", false, [][]box{{{x0: 0, y0: 20, x1: 40, y1: 32}, {x0: 45, y0: 20, x1: 75, y1: 32}}}},
		{"Greek, any case", "ωmega", false, [][]box{{{x0: 0, y0: 40, x1: 50, y1: 52}}, {{x0: 55, y0: 40, x1: 105, y1: 52}}}},
		{"nothing", "", false, nil},
		{"not there", "circle", false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findInPage(3, words, tt.text, tt.matchCase)
			if len(got) != len(tt.want) {
				t.Fatalf("%d matches %+v, want %d", len(got), got, len(tt.want))
			}
			for i, m := range got {
				if m.page != 3 || len(m.boxes) != len(tt.want[i]) {
					t.Fatalf("match %d: %+v, want %+v", i, m, tt.want[i])
				}
				for j, b := range m.boxes {
					if !near(b, tt.want[i][j]) {
						t.Errorf("match %d box %d: %+v, want %+v", i, j, b, tt.want[i][j])
					}
				}
			}
		})
	}
	// matches do not overlap
	if got := findInPage(0, poppler.PageWords{Lines: []poppler.Line{line(0, "aaaa")}}, "aa", true); len(got) != 2 {
		t.Errorf("%d matches of aa in aaaa, want 2", len(got))
	}
}
