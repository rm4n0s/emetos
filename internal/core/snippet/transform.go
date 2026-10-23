package snippet

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: src/vs/editor/contrib/snippet/browser/snippetParser.ts (Transform, FormatString)

// transformTimeout is the longest a regular expression of a transform may search.
const transformTimeout = time.Second

// Transform is the ${name/regex/format/flags} part of a variable or placeholder: the regular
// expression, with the format strings and text that replace what it matches as its children.
type Transform struct {
	markerBase
	// Pattern is the regular expression as it was written, in JavaScript's syntax.
	Pattern string
	// Global replaces every match (the g flag), otherwise only the first.
	Global bool
	// IgnoreCase is the i flag.
	IgnoreCase bool
	// flags are the other flags that were written (m, s, u, y, d, v), kept for the regexp.
	flags string
	re    *regex.Regexp
}

// NewTransform makes a transform from a pattern and flags (any of d g i m s u v y, each once). It
// fails when the flags or the pattern are not valid: with the tag SnippetInvalidFlags, or with
// the error of package regex for a pattern that does not compile.
func NewTransform(pattern, flags string) (*Transform, error) {
	t := &Transform{}
	if err := t.init(pattern, flags); err != nil {
		return nil, err
	}
	return t, nil
}

// init sets the pattern and the flags of a transform and compiles the pattern.
func (t *Transform) init(pattern, flags string) error {
	if !validFlags(flags) {
		return errors.New("SnippetInvalidFlags", "the flags of a regular expression are not valid", "flags", flags)
	}
	re, err := regex.Compile(pattern, regex.Options{
		Syntax: regex.ECMAScript, IgnoreCase: strings.Contains(flags, "i"), Multiline: strings.Contains(flags, "m"),
		DotAll: strings.Contains(flags, "s"), Unicode: strings.ContainsAny(flags, "uv"), Timeout: transformTimeout,
	})
	if err != nil {
		return err
	}
	t.Pattern, t.Global, t.IgnoreCase, t.flags, t.re = pattern, strings.Contains(flags, "g"), strings.Contains(flags, "i"), flags, re
	return nil
}

// validFlags tells whether the flags of a regular expression are valid in JavaScript: letters of
// "dgimsuvy", none twice, and not both u and v.
func validFlags(flags string) bool {
	seen := map[rune]bool{}
	for _, f := range flags {
		if !strings.ContainsRune("dgimsuvy", f) || seen[f] {
			return false
		}
		seen[f] = true
	}
	return !seen['u'] || !seen['v']
}

// AppendChild implements Marker.
func (t *Transform) AppendChild(child Marker) { appendChild(t, child) }

// String implements Marker: a transform has no text of its own.
func (t *Transform) String() string { return "" }

// Source is the pattern as JavaScript writes it back: a slash outside a character class has a
// backslash before it, and an empty pattern is (?:).
func (t *Transform) Source() string {
	if t.Pattern == "" {
		return "(?:)"
	}
	var sb strings.Builder
	inClass, escaped := false, false
	for _, r := range t.Pattern {
		switch {
		case escaped:
			escaped = false
		case r == '\\':
			escaped = true
		case r == '[':
			inClass = true
		case r == ']':
			inClass = false
		case r == '/' && !inClass:
			sb.WriteByte('\\')
		}
		switch r {
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// ToTextmateString implements Marker.
func (t *Transform) ToTextmateString() string {
	flags := ""
	if t.IgnoreCase {
		flags += "i"
	}
	if t.Global {
		flags += "g"
	}
	return "/" + t.Source() + "/" + textmateOf(t.children) + "/" + flags
}

// Clone implements Marker.
func (t *Transform) Clone() Marker {
	ret := &Transform{Pattern: t.Pattern, Global: t.Global, IgnoreCase: t.IgnoreCase, flags: t.flags, re: t.re}
	cloneChildren(t, ret)
	return ret
}

// Resolve replaces the matches of the regular expression in a value by the format of the
// transform. When nothing matched and a format string has an else branch, the else branches are
// the result (as for an empty match).
func (t *Transform) Resolve(value string) string {
	if t.re == nil {
		return value
	}
	text := []rune(value)
	var sb strings.Builder
	didMatch := false
	last, from := 0, 0
	sticky := strings.Contains(t.flags, "y")
	for from <= len(text) {
		m, ok, err := t.re.Find(text, from)
		if err != nil || !ok {
			// a search that ran past its limit ends the replacing, like one that finds nothing
			break
		}
		span := m.Groups[0]
		if sticky && span.Start != from {
			break
		}
		didMatch = true
		sb.WriteString(string(text[last:span.Start]))
		groups := make([]string, len(m.Groups))
		for i, g := range m.Groups {
			if g.Start >= 0 {
				groups[i] = string(text[g.Start:g.End])
			}
		}
		sb.WriteString(t.replace(groups))
		last = span.End
		if !t.Global {
			break
		}
		from = span.End
		if span.End == span.Start {
			from++ // an empty match: go on one character further
		}
	}
	if !didMatch {
		if t.hasElse() {
			return t.replace(nil)
		}
		return value
	}
	sb.WriteString(string(text[min(last, len(text)):]))
	return sb.String()
}

func (t *Transform) hasElse() bool {
	for _, c := range t.children {
		if f, ok := c.(*FormatString); ok && f.ElseValue != nil && *f.ElseValue != "" {
			return true
		}
	}
	return false
}

// replace is the text that replaces a match with the given groups (the whole match first).
func (t *Transform) replace(groups []string) string {
	var sb strings.Builder
	for _, c := range t.children {
		if f, ok := c.(*FormatString); ok {
			value := ""
			if f.Index < len(groups) {
				value = groups[f.Index]
			}
			sb.WriteString(f.Resolve(value))
			continue
		}
		sb.WriteString(c.String())
	}
	return sb.String()
}

// FormatString is a part of the format of a transform that uses a group of the match: $1, ${1},
// ${1:/upcase}, ${1:+if}, ${1:-else}, ${1:?if:else} or ${1:else}.
type FormatString struct {
	markerBase
	// Index is the number of the group; 0 is the whole match.
	Index int
	// Shorthand is the name of a change of case (upcase, downcase, capitalize, pascalcase,
	// camelcase, kebabcase, snakecase), or empty. An unknown name leaves the value as it is.
	Shorthand string
	// IfValue is what is inserted when the group matched something, ElseValue what is inserted
	// when it did not; nil when there is none.
	IfValue, ElseValue *string
}

// NewFormatString makes a format string.
func NewFormatString(index int, shorthand string, ifValue, elseValue *string) *FormatString {
	return &FormatString{Index: index, Shorthand: shorthand, IfValue: ifValue, ElseValue: elseValue}
}

// Resolve formats the text of the group, which is empty when the group matched nothing.
func (f *FormatString) Resolve(value string) string {
	switch f.Shorthand {
	case "upcase":
		return strings.ToUpper(value)
	case "downcase":
		return strings.ToLower(value)
	case "capitalize":
		return upperFirst(value)
	case "pascalcase":
		return toPascalCase(value)
	case "camelcase":
		return toCamelCase(value)
	case "kebabcase":
		return toKebabCase(value)
	case "snakecase":
		return toSnakeCase(value)
	}
	switch {
	case value != "" && f.IfValue != nil:
		return *f.IfValue
	case value == "" && f.ElseValue != nil:
		return *f.ElseValue
	}
	return value
}

// AppendChild implements Marker.
func (f *FormatString) AppendChild(child Marker) { appendChild(f, child) }

// ToTextmateString implements Marker.
func (f *FormatString) ToTextmateString() string {
	value := "${" + strconv.Itoa(f.Index)
	switch {
	case f.Shorthand != "":
		value += ":/" + f.Shorthand
	case f.IfValue != nil && *f.IfValue != "" && f.ElseValue != nil && *f.ElseValue != "":
		value += ":?" + *f.IfValue + ":" + *f.ElseValue
	case f.IfValue != nil && *f.IfValue != "":
		value += ":+" + *f.IfValue
	case f.ElseValue != nil && *f.ElseValue != "":
		value += ":-" + *f.ElseValue
	}
	return value + "}"
}

// Clone implements Marker.
func (f *FormatString) Clone() Marker {
	return NewFormatString(f.Index, f.Shorthand, f.IfValue, f.ElseValue)
}

// upperFirst is the text with its first character in upper case; empty stays empty.
func upperFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return ""
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// lowerFirst is the text with its first character in lower case.
func lowerFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return ""
	}
	return string(unicode.ToLower(r)) + s[size:]
}

// jsSpace are the characters that JavaScript's \s and trim() take for white space.
const jsSpace = `\t\n\v\f\r\p{Z}\x{FEFF}`

var (
	trimUnderscores = regexp.MustCompile(`^_+|_+$`)
	snakeCamel      = regexp.MustCompile(`(\p{Ll})(\p{Lu})`)
	snakeSeparators = regexp.MustCompile(`[` + jsSpace + `\-]+`)
	kebabSeparators = regexp.MustCompile(`[` + jsSpace + `_-]+`)
)

// words and kebabWords are the regular expressions of the case changes; they need the look-aheads
// of package regex.
var (
	wordsRegexp = sync.OnceValue(func() *regex.Regexp {
		return mustCompileUnicode(`[\p{L}0-9]+`)
	})
	kebabWordsRegexp = sync.OnceValue(func() *regex.Regexp {
		return mustCompileUnicode(`\p{Lu}{2,}(?=\p{Lu}\p{Ll}+[0-9]*|[\s_-]|$)|\p{Lu}?\p{Ll}+[0-9]*|\p{Lu}(?=\p{Lu}\p{Ll})|\p{Lu}(?=[\s_-]|$)|[0-9]+`)
	})
)

func mustCompileUnicode(pattern string) *regex.Regexp {
	re, err := regex.Compile(pattern, regex.Options{Syntax: regex.ECMAScript, Unicode: true, Timeout: transformTimeout})
	if err != nil {
		// the patterns are constants of this package: this is a bug that the tests find
		panic(err)
	}
	return re
}

// findAll are the texts of all the matches of a regular expression.
func findAll(re *regex.Regexp, s string) []string {
	text := []rune(s)
	var out []string
	for from := 0; from <= len(text); {
		m, ok, err := re.Find(text, from)
		if err != nil || !ok {
			break
		}
		g := m.Groups[0]
		out = append(out, string(text[g.Start:g.End]))
		from = g.End
		if g.End == g.Start {
			from++
		}
	}
	return out
}

func trimJS(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return unicode.Is(unicode.Z, r) || strings.ContainsRune("\t\n\v\f\r\uFEFF", r) })
}

func toKebabCase(value string) string {
	if value == "" {
		return ""
	}
	if len(findAll(wordsRegexp(), value)) == 0 {
		return value
	}
	cleaned := trimUnderscores.ReplaceAllString(trimJS(value), "")
	words := findAll(kebabWordsRegexp(), cleaned)
	if len(words) == 0 {
		for _, w := range kebabSeparators.Split(cleaned, -1) {
			if w != "" {
				words = append(words, w)
			}
		}
	}
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, "-")
}

func toPascalCase(value string) string {
	if value == "" {
		return ""
	}
	words := findAll(wordsRegexp(), value)
	if len(words) == 0 {
		return value
	}
	for i, w := range words {
		words[i] = upperFirst(w)
	}
	return strings.Join(words, "")
}

func toCamelCase(value string) string {
	if value == "" {
		return ""
	}
	words := findAll(wordsRegexp(), value)
	if len(words) == 0 {
		return value
	}
	for i, w := range words {
		if i == 0 {
			words[i] = lowerFirst(w)
		} else {
			words[i] = upperFirst(w)
		}
	}
	return strings.Join(words, "")
}

func toSnakeCase(value string) string {
	if value == "" {
		return ""
	}
	value = snakeCamel.ReplaceAllString(value, "${1}_${2}")
	return strings.ToLower(snakeSeparators.ReplaceAllString(value, "_"))
}
