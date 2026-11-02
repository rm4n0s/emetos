package debug_test

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/debug"
)

// The cases of `getExactExpressionStartAndEnd` of VS Code's debugUtils.test.ts (all of them: columns count characters from 1, as the
// original counts UTF-16 units, and every character of the cases is in the basic plane).
func TestExactExpression(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		start, end   int
		wantS, wantE int
	}{
		{"a word", "foo", 1, 2, 1, 3},
		{"after a bang", "!foo", 2, 3, 2, 4},
		{"the whole word", "foo", 1, 3, 1, 3},
		{"past the end", "foo", 1, 4, 1, 3},
		{"a property", "this.name = \"John\"", 1, 10, 1, 9},
		{"a property from its middle", "this.name = \"John\"", 6, 10, 1, 9},
		{"an arrow", "this->address = \"Main street\"", 6, 10, 1, 13},
		{"a chain", "var t = a.b.c.d.name", 16, 20, 9, 20},
		{"a scope", "MyClass::StaticProp", 10, 20, 1, 19},
		{"optional chaining", "largeNumber = myVar?.prop", 21, 25, 15, 25},
		{"cut after the word under the cursor", "var t = a.b.c.d.name", 11, 12, 9, 11},
		{"a semicolon ends it", "var t = a.b;c.d.name", 16, 20, 13, 20},
		{"a minus ends it", "var t = a.b.c-d.name", 16, 20, 15, 20},
		{"letters", "var aøñéå文 = a.b.c-d.name", 5, 5, 5, 10},
		{"a chain of letters", "aøñéå文.aøñéå文.aøñéå文", 9, 9, 1, 13},
		{"a spread", "[...bar]", 5, 7, 5, 7},
		{"a spread alone", "...variable", 5, 5, 4, 11},
		{"nothing", "  = ", 2, 2, 0, 0},
		{"an empty line", "", 1, 1, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, e := debug.ExactExpression(tc.line, tc.start, tc.end)
			if s != tc.wantS || e != tc.wantE {
				t.Errorf("ExactExpression(%q, %d, %d) = %d, %d; want %d, %d", tc.line, tc.start, tc.end, s, e, tc.wantS, tc.wantE)
			}
		})
	}
}

func TestEvaluatableExpression(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		column     int
		want       string
		start, end int
	}{
		{"the chain up to the cursor", "total += order.Items[i]", 18, "order.Items", 10, 21},
		{"the first word of a chain", "total += order.Items[i]", 12, "order", 10, 15},
		{"a name", "sum := 0", 2, "sum", 1, 4},
		{"between words", "a  b", 3, "", 0, 0},
		{"letters", "größe := 1", 3, "größe", 1, 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, s, e := debug.EvaluatableExpression(tc.line, tc.column)
			if got != tc.want || s != tc.start || e != tc.end {
				t.Errorf("EvaluatableExpression(%q, %d) = %q, %d, %d; want %q, %d, %d", tc.line, tc.column, got, s, e, tc.want, tc.start, tc.end)
			}
		})
	}
}
