package diff

import (
	"fmt"
	"strings"
	"time"
)

// format writes changes as "[1,3)->[1,4) [7,7)->[8,9)".
func format(changes []LineRangeMapping) string {
	parts := make([]string, len(changes))
	for i, c := range changes {
		parts[i] = fmt.Sprintf("[%d,%d)->[%d,%d)", c.Original.StartLine, c.Original.EndLine, c.Modified.StartLine, c.Modified.EndLine)
	}
	return strings.Join(parts, " ")
}

// fields splits a text into lines the way the tests write them: "a b c" is
// three lines and "_" is an empty line. The empty string is no line at all.
func fields(s string) []string {
	if s == "" {
		return nil
	}
	out := strings.Split(s, " ")
	for i, l := range out {
		if l == "_" {
			out[i] = ""
		}
	}
	return out
}

// steppingClock returns a clock that moves forward by step at every call.
func steppingClock(step time.Duration) func() time.Time {
	t := time.Unix(1000, 0)
	return func() time.Time {
		now := t
		t = t.Add(step)
		return now
	}
}

// numbers are the elements of a test sequence: equal numbers are equal
// elements.
type numbers []int

func (n numbers) length() int                  { return len(n) }
func (n numbers) element(offset int) int       { return n[offset] }
func (n numbers) boundaryScore(offset int) int { return 1 }
func (n numbers) stronglyEqual(a, b int) bool  { return n[a] == n[b] }
