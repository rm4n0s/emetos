package references

import (
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestWordAndTokenAt(t *testing.T) {
	// Ported from VS Code: utils.ts (isValidRequestPosition: the word at the position, else a run of characters that are not white
	// space); the columns are bytes.
	tests := []struct {
		name       string
		line       string
		col        int
		word       string
		wordOK     bool
		token      string
		wordOrTokn bool
	}{
		{"inside a word", "fmt.Println(x)", 6, "Println", true, "fmt.Println(x)", true},
		{"at the start of a word", "fmt.Println(x)", 4, "Println", true, "fmt.Println(x)", true},
		{"at the end of a word", "fmt.Println(x)", 11, "Println", true, "fmt.Println(x)", true},
		{"touching the end of a word", "a := b", 1, "a", true, "a", true},
		{"on punctuation only", "  ;;  ", 3, "", false, ";;", true},
		{"on white space", "ab   cd", 3, "", false, "", false},
		{"an empty line", "", 0, "", false, "", false},
		{"non-ASCII", "größe := 1", 3, "größe", true, "größe", true},
		{"a dollar and an underscore are word characters", "$x_1 = 2", 1, "$x_1", true, "$x_1", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, e, ok := wordAt(tc.line, tc.col)
			if ok != tc.wordOK || (ok && tc.line[s:e] != tc.word) {
				t.Errorf("wordAt = %q %v, want %q %v", tc.line[s:e], ok, tc.word, tc.wordOK)
			}
			s, e, ok = wordOrToken(tc.line, tc.col)
			if ok != tc.wordOrTokn {
				t.Errorf("wordOrToken ok = %v, want %v", ok, tc.wordOrTokn)
			}
			if ts, te, tok := tokenAt(tc.line, tc.col); tok && tc.token != "" && tc.line[ts:te] != tc.token {
				t.Errorf("tokenAt = %q, want %q", tc.line[ts:te], tc.token)
			}
			_, _ = s, e
		})
	}
}

func TestPreviewChunks(t *testing.T) {
	// Ported from VS Code: utils.ts (getPreviewChunks). The columns are bytes; the cases with non-ASCII text were worked out again
	// for bytes.
	tests := []struct {
		name             string
		line             string
		start, end       int
		beforeLen        int
		trim             bool
		before, in, rest string
	}{
		{"the word of a call", "\tfmt.Println(Greet(\"x\"))  ", 13, 18, 8, true, "Println(", "Greet", "(\"x\"))"},
		{"the start of the line", "Greet()", 0, 5, 8, true, "", "Greet", "()"},
		{"the before text goes back to the start of the word", "x := someVeryLongName.Greet()", 22, 27, 8, true, "someVeryLongName.", "Greet", "()"},
		{"leading white space is trimmed", "        Greet()", 8, 13, 8, true, "", "Greet", "()"},
		{"nothing is trimmed for a copy", "        Greet()  ", 8, 13, 21, false, "        ", "Greet", "()  "},
		{"non-ASCII before the match", "größe := Greet()", 11, 16, 8, true, "größe := ", "Greet", "()"},
		{"a column past the end is clamped", "ab", 5, 9, 8, true, "ab", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before, inside, after := previewChunks(tc.line, tc.start, tc.end, tc.beforeLen, tc.trim)
			if before != tc.before || inside != tc.in || after != tc.rest {
				t.Errorf("chunks = %q %q %q, want %q %q %q", before, inside, after, tc.before, tc.in, tc.rest)
			}
		})
	}
}

func TestThePreviewAfterTheMatchIsBounded(t *testing.T) {
	long := "Greet" + string(make([]byte, 0)) + stringsOf('x', 1000)
	_, _, after := previewChunks(long, 0, 5, 8, true)
	if len(after) != afterLen {
		t.Errorf("the text after the match has %d bytes, want %d", len(after), afterLen)
	}
}

func stringsOf(r rune, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}

func TestUTF16Column(t *testing.T) {
	tests := []struct {
		line string
		byte int
		want int
	}{
		{"abc", 2, 2},
		{"größe", 4, 3}, // ö and ß are 2 bytes and 1 unit each
		{"a😀b", 5, 3},   // an emoji is 4 bytes and 2 units
		{"abc", 99, 3},  // past the end
		{"abc", -4, 0},  // before the start
		{"\tx", 1, 1},
	}
	for _, tc := range tests {
		if got := utf16Col(tc.line, tc.byte); got != tc.want {
			t.Errorf("utf16Col(%q, %d) = %d, want %d", tc.line, tc.byte, got, tc.want)
		}
	}
}

func TestSplitLinesDropsCarriageReturns(t *testing.T) {
	lines := splitLines("a\r\nb\n\nc")
	want := []string{"a", "b", "", "c"}
	for i, w := range want {
		if lineOf(lines, i) != w {
			t.Errorf("line %d = %q, want %q", i, lineOf(lines, i), w)
		}
	}
	if lineOf(lines, 9) != "" || lineOf(lines, -1) != "" {
		t.Error("a line that is not there is not empty")
	}
}

func TestRangeEndOfASeveralLineRangeIsTheEndOfTheLine(t *testing.T) {
	multi := extension.Range{Start: extension.Position{Line: 1, Col: 2}, End: extension.Position{Line: 3, Col: 4}}
	if got := rangeEnd(1, multi, 10); got != 10 {
		t.Errorf("rangeEnd = %d, want 10", got)
	}
	single := extension.Range{Start: extension.Position{Line: 1, Col: 2}, End: extension.Position{Line: 1, Col: 6}}
	if got := rangeEnd(1, single, 10); got != 6 {
		t.Errorf("rangeEnd = %d, want 6", got)
	}
}
