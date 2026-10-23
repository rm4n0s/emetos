package textmate

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: reference/vscode-textmate/src/rule.ts (vscode-textmate, MIT)

// endRuleID marks a match of the end pattern of a rule in a list of patterns; whileRuleID one of
// the while pattern.
const (
	endRuleID   = -1
	whileRuleID = -2
)

// ruleKind is the kind of a rule.
type ruleKind int

const (
	ruleUnset ruleKind = iota
	ruleCapture
	ruleMatch
	ruleIncludeOnly
	ruleBeginEnd
	ruleBeginWhile
)

// rule is a compiled rule of a grammar.
type rule struct {
	id   int
	kind ruleKind
	// name and contentName are the scopes of the rule; they may refer to captures as $1.
	name, contentName *template
	// match is the pattern of a match rule, begin the one of a rule with an end.
	match, begin *regexSource
	// end is the end pattern of a begin/end rule, while the one of a begin/while rule.
	end, while          *regexSource
	beginCaptures       []*rule
	endCaptures         []*rule
	whileCaptures       []*rule
	captures            []*rule
	applyEndPatternLast bool
	hasMissingPatterns  bool
	patterns            []int
	// retokenizeCapturedWithRuleID, for a capture rule, is the rule that tokenizes the text
	// captured again; 0 for none.
	retokenizeCapturedWithRuleID int

	mu sync.Mutex
	// static is the scanner over the patterns of the rule, made when it is first needed.
	static *compiledRule
	// dynamic is the scanner for the last end pattern that had references to the captures.
	dynamic       *compiledRule
	dynamicSource string
	// whileStatic and whileDynamic are the same for the while pattern.
	whileStatic        *compiledRule
	whileDynamic       *compiledRule
	whileDynamicSource string
}

// compiledRule is a scanner over the patterns of a rule and the rules they belong to.
type compiledRule struct {
	scanner *regex.Scanner
	ruleIDs []int
}

// regexSource is a pattern of a rule.
type regexSource struct {
	source string
	ruleID int
	// hasBackReferences tells whether the pattern refers to the captures of the begin pattern
	// (\1) and has to be completed with their text before it is used.
	hasBackReferences bool
}

var backReferenceRE = regexp.MustCompile(`\\(\d+)`)

// newRegexSource makes the pattern of a rule. A \z is written as the end of the line without its
// line break, as the tokenizer does, because the lines it works on end with one.
func newRegexSource(source string, ruleID int) *regexSource {
	if source != "" {
		source = replaceEndOfText(source)
	}
	return &regexSource{source: source, ruleID: ruleID, hasBackReferences: backReferenceRE.MatchString(source)}
}

// replaceEndOfText replaces \z, outside of escapes, by "$(?!\n)(?<!\n)".
func replaceEndOfText(source string) string {
	if !strings.Contains(source, `\z`) {
		return source
	}
	var sb strings.Builder
	last := 0
	for i := 0; i < len(source); i++ {
		if source[i] != '\\' || i+1 >= len(source) {
			continue
		}
		if source[i+1] == 'z' {
			sb.WriteString(source[last:i])
			sb.WriteString(`$(?!\n)(?<!\n)`)
			last = i + 2
		}
		i++
	}
	sb.WriteString(source[last:])
	return sb.String()
}

// resolveBackReferences replaces \N in the pattern by the escaped text of the capture N.
func (s *regexSource) resolveBackReferences(line string, caps []regex.Capture) string {
	return backReferenceRE.ReplaceAllStringFunc(s.source, func(m string) string {
		n, err := strconv.Atoi(m[1:])
		if err != nil || n >= len(caps) || caps[n].Start < 0 {
			return ""
		}
		return escapeRegExpCharacters(line[caps[n].Start:caps[n].End])
	})
}

// escapeRegExpCharacters escapes the characters that mean something in a pattern.
func escapeRegExpCharacters(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '-', '\\', '{', '}', '*', '+', '?', '|', '^', '$', '.', ',', '[', ']', '(', ')', '#':
			sb.WriteByte('\\')
		default:
			if isJSSpace(r) {
				sb.WriteByte('\\')
			}
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// isJSSpace tells whether r is white space for a JavaScript regular expression (\s).
func isJSSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// template is the name of a scope that may take the text of captures: "string.$1" or
// "${1:/downcase}".
type template struct {
	text      string
	capturing bool
}

var capturingRE = regexp.MustCompile(`\$(\d+)|\$\{(\d+):/(downcase|upcase)\}`)

// newTemplate makes a template; the empty text is no template.
func newTemplate(text string) *template {
	if text == "" {
		return nil
	}
	return &template{text: text, capturing: capturingRE.MatchString(text)}
}

// value is the text of the template for a match of line with captures caps. A template without
// captures is its text; the result of one with captures has the text of the captures with the
// leading dots taken off.
func (t *template) value(line string, caps []regex.Capture) string {
	if !t.capturing || caps == nil {
		return t.text
	}
	return capturingRE.ReplaceAllStringFunc(t.text, func(m string) string {
		sub := capturingRE.FindStringSubmatch(m)
		index, command := sub[1], sub[3]
		if index == "" {
			index = sub[2]
		}
		n, err := strconv.Atoi(index)
		if err != nil || n >= len(caps) {
			return m
		}
		result := ""
		if caps[n].Start >= 0 {
			result = line[caps[n].Start:caps[n].End]
		}
		result = strings.TrimLeft(result, ".")
		switch command {
		case "downcase":
			return strings.ToLower(result)
		case "upcase":
			return strings.ToUpper(result)
		}
		return result
	})
}

// itoa writes a number.
func itoa(n int) string { return strconv.Itoa(n) }
