package contextkey

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// A context value is nil (not set), bool, float64, string, []any or map[string]any.
// When-clauses are evaluated with the rules of JavaScript that VS Code relies on, which
// the functions below follow.

// jsNumberString writes a number as JavaScript's Number#toString does.
func jsNumberString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	abs := math.Abs(f)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	// Go writes e-07 where JavaScript writes e-7.
	mant, exp, _ := strings.Cut(s, "e")
	sign := exp[:1]
	digits := strings.TrimLeft(exp[1:], "0")
	return mant + "e" + sign + digits
}

// jsString converts a value to a string as String(value) does.
func jsString(v any) string {
	switch x := v.(type) {
	case nil:
		return "undefined"
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return jsNumberString(x)
	case int:
		return strconv.Itoa(x)
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e != nil {
				parts[i] = jsString(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// jsToNumber converts a value to a number as Number(value) does.
func jsToNumber(v any) float64 {
	switch x := v.(type) {
	case nil:
		return math.NaN()
	case bool:
		if x {
			return 1
		}
		return 0
	case float64:
		return x
	case int:
		return float64(x)
	case string:
		return stringToNumber(x)
	case []any:
		return stringToNumber(jsString(x))
	}
	return math.NaN()
}

func stringToNumber(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.ContainsAny(s, "_") {
		return math.NaN()
	}
	switch s {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if len(s) > 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X' || s[1] == 'o' || s[1] == 'O' || s[1] == 'b' || s[1] == 'B') {
		n, err := strconv.ParseInt(s[2:], map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[s[1]], 64)
		if err != nil {
			return math.NaN()
		}
		return float64(n)
	}
	if strings.ContainsAny(s, "xXpP") || strings.EqualFold(s, "inf") || strings.EqualFold(s, "nan") {
		return math.NaN()
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil && !isRangeError(err) {
		return math.NaN()
	}
	return f
}

func isRangeError(err error) bool {
	ne, ok := err.(*strconv.NumError)
	return ok && ne.Err == strconv.ErrRange
}

// isJSSpace tells white space and line terminators as JavaScript counts them.
func isJSSpace(r rune) bool {
	return unicode.IsSpace(r) || r == 0xFEFF || r == 0x2028 || r == 0x2029
}

var floatPrefix = regexp.MustCompile(`^[+-]?(Infinity|(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?)`)

// jsParseFloat is parseFloat: the number a string starts with, or NaN.
func jsParseFloat(v any) float64 {
	s := strings.TrimLeftFunc(jsString(v), isJSSpace)
	m := floatPrefix.FindString(s)
	if m == "" {
		return math.NaN()
	}
	switch strings.TrimLeft(m, "+-") {
	case "Infinity":
		if m[0] == '-' {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	f, err := strconv.ParseFloat(m, 64)
	if err != nil && !isRangeError(err) {
		return math.NaN()
	}
	return f
}

// looseEquals is a == b for a value of the context and a value of a when-clause (a string
// or a number).
func looseEquals(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return false
	case string:
		switch y := b.(type) {
		case string:
			return x == y
		case float64:
			return stringToNumber(x) == y
		}
	case float64:
		switch y := b.(type) {
		case string:
			return x == stringToNumber(y)
		case float64:
			return x == y
		}
	case bool:
		return jsToNumber(x) == jsToNumber(b)
	case []any, map[string]any:
		if s, ok := b.(string); ok {
			return jsString(a) == s
		}
		if f, ok := b.(float64); ok {
			return jsToNumber(a) == f
		}
	}
	return false
}

// jsLess is a < b for the values of when-clauses (strings and numbers).
func jsLess(a, b any) bool {
	as, aIsString := a.(string)
	bs, bIsString := b.(string)
	if aIsString && bIsString {
		return as < bs
	}
	return jsToNumber(a) < jsToNumber(b)
}

// cmpValues orders two values of when-clauses.
func cmpValues(a, b any) int {
	switch {
	case jsLess(a, b):
		return -1
	case jsLess(b, a):
		return 1
	}
	return 0
}

// regexFlagOrder is the order JavaScript lists flags in, without d and v: the scanner of
// when-clauses does not read them as flags either, so a clause with them could not be read back.
const regexFlagOrder = "gimsuy"

// canonicalFlags checks regular expression flags and puts them in JavaScript's order. It
// reports false for an unknown or repeated flag.
func canonicalFlags(flags string) (string, bool) {
	seen := map[rune]bool{}
	for _, f := range flags {
		if !strings.ContainsRune(regexFlagOrder, f) || seen[f] {
			return "", false
		}
		seen[f] = true
	}
	var out []rune
	for _, f := range regexFlagOrder {
		if seen[f] {
			out = append(out, f)
		}
	}
	return string(out), true
}

// jsRegexSource writes a pattern as RegExp#source does: a slash outside a character class
// is escaped, line breaks are written as escapes and an empty pattern is "(?:)".
func jsRegexSource(pattern string) string {
	if pattern == "" {
		return "(?:)"
	}
	var b strings.Builder
	inClass, inEscape := false, false
	for _, r := range pattern {
		switch {
		case inEscape:
			inEscape = false
			if r == '\n' {
				b.WriteString("n")
				continue
			}
		case r == '\\':
			inEscape = true
		case r == '[':
			inClass = true
		case r == ']':
			inClass = false
		case r == '/' && !inClass:
			b.WriteString(`\/`)
			continue
		case r == '\n':
			b.WriteString(`\n`)
			continue
		case r == '\r':
			b.WriteString(`\r`)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// compileRegex compiles a JavaScript pattern with the engine of the standard library. A
// pattern it cannot compile (lookahead, backreferences) is accepted when its brackets are
// balanced, and never matches: core/regex's ECMAScript mode replaces this (SPEC §8.2).
func compileRegex(source, flags string) (re *regexp.Regexp, ok bool) {
	prefix := ""
	for _, f := range flags {
		switch f {
		case 'i', 'm', 's':
			prefix += string(f)
		}
	}
	if !balanced(source) {
		return nil, false
	}
	pattern := source
	if prefix != "" {
		pattern = "(?" + prefix + ")" + pattern
	}
	if re, err := regexp.Compile(pattern); err == nil {
		return re, true
	}
	return nil, true
}

// balanced tells whether parentheses and brackets of a pattern are closed.
func balanced(pattern string) bool {
	depth, inClass, inEscape := 0, false, false
	for _, r := range pattern {
		switch {
		case inEscape:
			inEscape = false
		case r == '\\':
			inEscape = true
		case inClass:
			if r == ']' {
				inClass = false
			}
		case r == '[':
			inClass = true
		case r == '(':
			depth++
		case r == ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0 && !inClass && !inEscape
}

// sortedCopy returns a sorted copy of strings.
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
