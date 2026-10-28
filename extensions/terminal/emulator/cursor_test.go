package emulator

import "testing"

func TestCursorPosition(t *testing.T) {
	tests := []struct {
		name     string
		rows     int
		cols     int
		input    string
		wantLine int64
		wantCol  int
		wantWrap bool
	}{
		{"the start", 4, 10, "", 0, 0, false},
		{"after text", 4, 10, "abc", 0, 3, false},
		{"after a line break", 4, 10, "abc\r\nde", 1, 2, false},
		{"at the last column the next character wraps", 4, 5, "abcde", 0, 4, true},
		{"after scrolling the line is absolute", 2, 10, "a\r\nb\r\nc\r\nd", 3, 1, false},
		{"after a cursor motion", 4, 10, "\x1b[3;5H", 2, 4, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(tt.rows, tt.cols)
			r.feed(tt.input)
			line, col, wrap := r.CursorPosition()
			if line != tt.wantLine || col != tt.wantCol || wrap != tt.wantWrap {
				t.Errorf("CursorPosition() = (%d, %d, %v), want (%d, %d, %v)", line, col, wrap, tt.wantLine, tt.wantCol, tt.wantWrap)
			}
		})
	}
}
