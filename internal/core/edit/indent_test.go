package edit

import "testing"

func TestNormalizeIndentation(t *testing.T) {
	tests := []struct {
		in           string
		size         int
		insertSpaces bool
		want         string
	}{
		{"", 4, true, ""},
		{"x", 4, true, "x"},
		{"\t", 4, true, "    "},
		{"\t", 4, false, "\t"},
		{"    ", 4, false, "\t"},
		{"      ", 4, false, "\t  "},
		{"  \t", 4, true, "    "},
		{"  \t", 4, false, "\t"},
		{" \t \t", 4, false, "\t\t"},
		{"\t  x\t", 4, false, "\t  x\t"},
		{"  \tx  y", 2, true, "    x  y"},
		{"        ", 8, false, "\t"},
		{"   ", 0, true, "   "},
		{"\t\t", 3, true, "      "},
	}
	for _, tt := range tests {
		if got := NormalizeIndentation(tt.in, tt.size, tt.insertSpaces); got != tt.want {
			t.Errorf("NormalizeIndentation(%q, %d, %v) = %q, want %q", tt.in, tt.size, tt.insertSpaces, got, tt.want)
		}
	}
}

func TestShiftedIndent(t *testing.T) {
	tests := []struct {
		line         string
		col          int
		insertSpaces bool
		unshift      bool
		want         string
	}{
		{"x", 0, true, false, "    "},
		{"  x", 2, true, false, "    "},
		{"    x", 4, true, false, "        "},
		{"      x", 6, true, true, "    "},
		{"    x", 4, true, true, ""},
		{"\tx", 1, false, false, "\t\t"},
		{"\t\tx", 2, false, true, "\t"},
		{"   x", 3, false, false, "\t"},
	}
	for _, tt := range tests {
		if got := shiftedIndent(tt.line, tt.col, 4, 4, tt.insertSpaces, tt.unshift); got != tt.want {
			t.Errorf("shiftedIndent(%q, %d, spaces=%v, unshift=%v) = %q, want %q", tt.line, tt.col, tt.insertSpaces, tt.unshift, got, tt.want)
		}
	}
}

func TestLeftDeleteOffset(t *testing.T) {
	tests := []struct {
		line string
		col  int
		want int
	}{
		{"abc", 0, 0},
		{"abc", 3, 2},
		{"é", 2, 0},
		{"a😍", 5, 1},
		{"a👶🏾", 9, 1},
		{"👨‍👩", 11, 4},
		{"❤️", 6, 0},
		{"1️⃣", 7, 4},
		{"\U0001F3FF", 4, 0},
		{"a\u200d", 4, 1},
	}
	for _, tt := range tests {
		if got := leftDeleteOffset(tt.line, tt.col); got != tt.want {
			t.Errorf("leftDeleteOffset(%q, %d) = %d, want %d", tt.line, tt.col, got, tt.want)
		}
	}
}
