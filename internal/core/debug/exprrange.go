package debug

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// The expression under the pointer or the cursor, for the debug hover, Add to Watch and the inline values (step 25.5).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugUtils.ts (getExactExpressionStartAndEnd,
// getEvaluatableExpressionAtPosition: the one-size-fits-all strategy, the editor has no evaluatable expression providers)

// expressionRE matches any run of characters except a set of characters which often break interesting sub-expressions. Some supported
// expressions: myVar.prop, a.b.c.d, myVar?.prop, myVar->prop, MyClass::StaticProp, *myVar, ...foo
var expressionRE = regexp.MustCompile("(?:[^()\\[\\]{}<>\\s+\\-/%~#^;=|,`!]|->)+")

// subExpressionRE matches the words of an expression (JavaScript's `\w` and `\p{L}`).
var subExpressionRE = regexp.MustCompile(`[\p{L}0-9_]+`)

// ExactExpression finds the expression around the columns looseStart to looseEnd of a line. The columns count characters from 1; the
// result is the first and the last column of the expression, or 0 and 0 when there is none. When there are non-word characters after
// the cursor the expression is cut there: in `a.b.c.d` with the cursor under `b`, `a.b` is the expression.
func ExactExpression(line string, looseStart, looseEnd int) (start, end int) {
	matching, startOffset, found := "", 0, false
	for _, m := range expressionRE.FindAllStringIndex(line, -1) {
		s := utf8.RuneCountInString(line[:m[0]]) + 1
		text := line[m[0]:m[1]]
		e := s + utf8.RuneCountInString(text)
		if s <= looseStart && e >= looseEnd {
			matching, startOffset, found = text, s, true
			break
		}
	}
	if !found {
		return 0, 0
	}
	// the spread syntax: just the identifier
	if rest, ok := strings.CutPrefix(matching, "..."); ok && rest != "" {
		matching = rest
		startOffset += 3
	}
	for _, m := range subExpressionRE.FindAllStringIndex(matching, -1) {
		subEnd := utf8.RuneCountInString(matching[:m[0]]) + 1 + startOffset + utf8.RuneCountInString(matching[m[0]:m[1]])
		if subEnd >= looseEnd {
			matching = matching[:m[1]]
			break
		}
	}
	return startOffset, startOffset + utf8.RuneCountInString(matching) - 1
}

// EvaluatableExpression is the expression to evaluate at a column of a line (counted from 1, in characters): its text and the columns
// it covers, the end column excluded. The text is empty when the position is not in an expression.
func EvaluatableExpression(line string, column int) (text string, startColumn, endColumn int) {
	start, end := ExactExpression(line, column, column)
	if start == 0 || end < start {
		return "", 0, 0
	}
	runes := []rune(line)
	if start > len(runes) || end > len(runes) {
		return "", 0, 0
	}
	text = string(runes[start-1 : end])
	return text, start, start + utf8.RuneCountInString(text)
}
