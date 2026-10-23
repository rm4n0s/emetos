package indent

import (
	"strings"

	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: src/vs/editor/common/languages/supports/onEnter.ts (MIT)

// onEnterBracket is a pair of brackets with the patterns that find them at the end and at the
// start of a text.
type onEnterBracket struct {
	open, close *regex.Regexp
}

// OnEnterSupport decides what Enter does from the text around the cursor: the on-enter rules of
// the language, and the brackets (VS Code's OnEnterSupport).
type OnEnterSupport struct {
	brackets []onEnterBracket
	rules    []language.OnEnterRule
}

// defaultBrackets are the brackets used when a configuration has indentation or on-enter rules but
// no brackets.
var defaultBrackets = []language.Pair{{Open: "(", Close: ")"}, {Open: "{", Close: "}"}, {Open: "[", Close: "]"}}

// NewOnEnterSupport makes the support of a language configuration. Like VS Code, a language that
// has no brackets, no indentation rules and no on-enter rules has none (nil).
func NewOnEnterSupport(c *language.Configuration) *OnEnterSupport {
	if c == nil || (c.Brackets == nil && c.IndentationRules == nil && c.OnEnterRules == nil) {
		return nil
	}
	pairs := c.Brackets
	if pairs == nil {
		pairs = defaultBrackets
	}
	s := &OnEnterSupport{rules: c.OnEnterRules}
	for _, p := range pairs {
		open, errOpen := compileECMAScript(openBracketPattern(p.Open))
		closing, errClose := compileECMAScript(closeBracketPattern(p.Close))
		if errOpen != nil || errClose != nil {
			continue
		}
		s.brackets = append(s.brackets, onEnterBracket{open: open, close: closing})
	}
	return s
}

func compileECMAScript(pattern string) (*regex.Regexp, error) {
	return regex.Compile(pattern, regex.Options{Syntax: regex.ECMAScript})
}

// escapeRegExp escapes the characters that mean something in a regular expression (VS Code's
// escapeRegExpCharacters).
func escapeRegExp(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(`\{}*+?|^$.[]()`, s[i]) >= 0 {
			sb.WriteByte('\\')
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

// isWordByte reports whether c is a character of \w.
func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// openBracketPattern finds the bracket at the end of a text.
func openBracketPattern(bracket string) string {
	s := escapeRegExp(bracket)
	if bracket != "" && isWordByte(bracket[0]) {
		s = `\b` + s
	}
	return s + `\s*$`
}

// closeBracketPattern finds the bracket at the start of a text.
func closeBracketPattern(bracket string) string {
	s := escapeRegExp(bracket)
	if bracket != "" && isWordByte(bracket[len(bracket)-1]) {
		s += `\b`
	}
	return `^\s*` + s
}

// OnEnter gives what pressing Enter does when the text before the cursor, the text after it and
// the line above are as given: the first on-enter rule that matches (from the level "advanced"),
// else IndentOutdent between a pair of brackets, else Indent after an opening bracket (from the
// level "brackets"). It is nil-safe: a language without the support answers false.
func (s *OnEnterSupport) OnEnter(auto AutoIndent, previousLine, beforeEnter, afterEnter string) (language.EnterAction, bool) {
	if s == nil {
		return language.EnterAction{}, false
	}
	if auto >= AutoIndentAdvanced {
		for _, rule := range s.rules {
			if !matches(rule.BeforeText, beforeEnter) {
				continue
			}
			if rule.AfterText != nil && !matches(rule.AfterText, afterEnter) {
				continue
			}
			if rule.PreviousLineText != nil && !matches(rule.PreviousLineText, previousLine) {
				continue
			}
			return rule.Action, true
		}
	}
	if auto >= AutoIndentBrackets {
		if beforeEnter != "" && afterEnter != "" {
			for _, b := range s.brackets {
				if matches(b.open, beforeEnter) && matches(b.close, afterEnter) {
					return language.EnterAction{Indent: language.IndentOutdent}, true
				}
			}
		}
		if beforeEnter != "" {
			for _, b := range s.brackets {
				if matches(b.open, beforeEnter) {
					return language.EnterAction{Indent: language.IndentIndent}, true
				}
			}
		}
	}
	return language.EnterAction{}, false
}
