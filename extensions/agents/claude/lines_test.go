package claude

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestLineReader(t *testing.T) {
	type line struct {
		text    string
		tooLong bool
	}
	big := strings.Repeat("a", 200_000) // longer than the buffer of the reader, shorter than the limit
	tests := []struct {
		name  string
		in    string
		limit int
		want  []line
	}{
		{"lines", "one\ntwo\nthree\n", 100, []line{{"one", false}, {"two", false}, {"three", false}}},
		{"carriage returns", "one\r\ntwo\r\n", 100, []line{{"one", false}, {"two", false}}},
		{"empty lines", "a\n\nb\n", 100, []line{{"a", false}, {"", false}, {"b", false}}},
		{"no final newline", "a\nb", 100, []line{{"a", false}, {"b", false}}},
		{"a line longer than the buffer", big + "\nend\n", 1 << 20, []line{{big, false}, {"end", false}}},
		{"a line over the limit is cut and skipped", strings.Repeat("x", 50) + "\nok\n", 10, []line{{strings.Repeat("x", 10), true}, {"ok", false}}},
		{"an unfinished line over the limit", strings.Repeat("x", 300_000), 1000, []line{{strings.Repeat("x", 1000), true}}},
		{"the limit exactly", "12345\nrest\n", 5, []line{{"12345", false}, {"rest", false}}},
		{"nothing", "", 10, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lr := newLineReader(iotest.OneByteReader(strings.NewReader(tt.in)), tt.limit)
			if len(tt.in) > 1000 {
				lr = newLineReader(strings.NewReader(tt.in), tt.limit)
			}
			var got []line
			for {
				text, tooLong, err := lr.next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, line{string(text), tooLong})
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d lines, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line %d = {%.20q... %d bytes, %v}, want {%.20q... %d bytes, %v}", i, got[i].text, len(got[i].text), got[i].tooLong, tt.want[i].text, len(tt.want[i].text), tt.want[i].tooLong)
				}
			}
		})
	}
}
