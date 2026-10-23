package indent

import (
	"strings"
	"testing"
)

// textLines is a Lines over a slice.
type textLines []string

func (t textLines) LineCount() int              { return len(t) }
func (t textLines) LineContent(line int) string { return t[line] }

// Ported from VS Code: textModel.test.ts (testGuessIndentation)
func checkGuess(t *testing.T, defaultSpaces bool, defaultTabSize int, wantSpaces bool, wantTabSize int, lines []string, msg string) {
	t.Helper()
	got := Guess(textLines(lines), defaultTabSize, defaultSpaces)
	if got.InsertSpaces != wantSpaces || got.TabSize != wantTabSize {
		t.Errorf("%s: defaults (%v, %d): got spaces=%v tab=%d, want spaces=%v tab=%d", msg, defaultSpaces, defaultTabSize, got.InsertSpaces, got.TabSize, wantSpaces, wantTabSize)
	}
}

// Ported from VS Code: textModel.test.ts (assertGuess), which runs each case with both defaults.
func TestGuessIndentationOfVSCode(t *testing.T) {
	const (
		tabTrue  = 13370
		tabFalse = 13371
	)
	for _, tc := range guessCases {
		t.Run(tc.name, func(t *testing.T) {
			// What the guess must find, for the default insertSpaces of true and of false.
			var wantTrue, wantFalse struct {
				spaces bool
				tab    int
			}
			switch tc.insertSpaces {
			case "undefined": // the defaults stay
				wantTrue.spaces, wantFalse.spaces = true, false
			case "true":
				wantTrue.spaces, wantFalse.spaces = true, true
			case "false":
				wantTrue.spaces, wantFalse.spaces = false, false
			}
			switch {
			case tc.tabSize == 0:
				wantTrue.tab, wantFalse.tab = tabTrue, tabFalse
			case !tc.tabSizeOnlyForSpaces:
				wantTrue.tab, wantFalse.tab = tc.tabSize, tc.tabSize
			default:
				// A tab size is found only when the indentation is spaces.
				wantTrue.tab, wantFalse.tab = tc.tabSize, tabFalse
				if tc.insertSpaces == "true" {
					wantFalse.tab = tc.tabSize
				}
				if tc.insertSpaces == "false" {
					wantTrue.tab, wantFalse.tab = tabTrue, tabFalse
				}
			}
			checkGuess(t, true, tabTrue, wantTrue.spaces, wantTrue.tab, tc.lines, tc.name)
			checkGuess(t, false, tabFalse, wantFalse.spaces, wantFalse.tab, tc.lines, tc.name)
		})
	}
}

// The calls of testGuessIndentation of 'issue #62143' with their defaults.
func TestGuessIndentationIssue62143(t *testing.T) {
	cases := [][]string{
		{"x x", "  x", "  x"},
		{"x x", "  x", "  x", "    x"},
		{"<!--test1.md -->", "- item1", "  - item2", "    - item3"},
	}
	for i, lines := range cases {
		checkGuess(t, true, 2, true, 2, lines, "case "+string(rune('1'+i)))
	}
}

func TestGuessIndentationEdges(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		spaces bool
		tab    int
	}{
		{"an empty text keeps the defaults", "", true, 4},
		{"only white space lines say nothing", "   \n\t\n  ", true, 4},
		{"a tie between tabs and spaces keeps the default (and the tab and the two spaces differ by two)", "\tx\n  x", true, 2},
		{"tabs win", "\tx\n\ty\n\tz\n  a", false, 4},
		{"4 spaces", "a\n    b\n        c\n    d\ne", true, 4},
		{"3 spaces", "a\n   b\n      c\n   d\ne", true, 3},
		{"2 wins over 4 with two thirds of its occurrences", "a\n  b\n    c\n  d\n    e\n  f\ne", true, 2},
		{"4 wins over 2 with fewer", "a\n    b\n      c\n    d\n        e\n    f\ng\n    h", true, 4},
		{"a difference larger than 8 counts for nothing", "a\n          b\n          c", true, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Guess(textLines(strings.Split(tc.text, "\n")), 4, true)
			if got.InsertSpaces != tc.spaces || got.TabSize != tc.tab {
				t.Errorf("got %+v, want spaces=%v tab=%d", got, tc.spaces, tc.tab)
			}
		})
	}
	// Only the first 10,000 lines are read.
	long := make(textLines, 20000)
	for i := range long {
		long[i] = "x"
	}
	for i := 10000; i < len(long); i += 2 {
		long[i], long[i+1] = "x", "\ty"
	}
	if got := Guess(long, 4, true); !got.InsertSpaces {
		t.Errorf("tabs after the 10,000th line are not read: %+v", got)
	}
}
