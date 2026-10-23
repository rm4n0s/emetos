package theme

import (
	"sort"
	"strings"
)

// FontStyle is the font style of a rule or token: a set of the flags below, or FontStyleNotSet.
type FontStyle int

// The font styles, with the values of vscode-textmate.
const (
	// FontStyleNotSet is the style of a rule that does not say.
	FontStyleNotSet FontStyle = -1
	// FontStyleNone is a rule that clears the style.
	FontStyleNone FontStyle = 0
	// FontStyleItalic is italic text.
	FontStyleItalic FontStyle = 1
	// FontStyleBold is bold text.
	FontStyleBold FontStyle = 2
	// FontStyleUnderline is underlined text.
	FontStyleUnderline FontStyle = 4
	// FontStyleStrikethrough is struck-through text.
	FontStyleStrikethrough FontStyle = 8
)

// String writes the style as vscode-textmate's fontStyleToString does: "not set", "none" or the
// names of the flags in the order italic, bold, underline, strikethrough.
func (f FontStyle) String() string {
	if f == FontStyleNotSet {
		return "not set"
	}
	var names []string
	for _, flag := range []struct {
		style FontStyle
		name  string
	}{{FontStyleItalic, "italic"}, {FontStyleBold, "bold"}, {FontStyleUnderline, "underline"}, {FontStyleStrikethrough, "strikethrough"}} {
		if f&flag.style != 0 {
			names = append(names, flag.name)
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, " ")
}

// Scope is the scope of a rule as the theme file gives it: a string of selectors separated by
// commas, a list of selectors, or nothing (the rule is a default).
type Scope struct {
	text   string
	list   []string
	isList bool
	isText bool
}

// ScopeText is a scope that is a string, such as "comment, string".
func ScopeText(s string) Scope { return Scope{text: s, isText: true} }

// ScopeList is a scope that is a list of selectors.
func ScopeList(selectors ...string) Scope { return Scope{list: selectors, isList: true} }

// RuleSettings are the settings of a rule. A nil pointer is a setting that is not there.
type RuleSettings struct {
	FontStyle  *string
	Foreground *string
	Background *string
	FontFamily *string
	FontSize   *float64
	LineHeight *float64

	// other holds the settings of a TextMate theme's rule without scope that set workbench
	// colors (selection, caret, ...), which the loader turns into colors.
	other map[string]string
}

// RawRule is a rule of the tokenColors of a theme as the file has it.
type RawRule struct {
	// Name is the name of the rule, which only people read.
	Name string
	// Scope is where the rule applies; no scope makes the rule a default.
	Scope Scope
	// Settings are what the rule sets; a rule with no settings is ignored.
	Settings *RuleSettings
}

// Rule is a rule with its scope in one selector, as ParseRules makes it.
type Rule struct {
	// Scope is the last part of the selector, such as "string" in "meta.tag string".
	Scope string
	// ParentScopes are the other parts of the selector, the nearest ancestor first; nil when the
	// selector is only a scope. A part can be ">", for a child combinator.
	ParentScopes []string
	// Index is the index of the rule in the theme, which makes the later of two equal rules win.
	Index      int
	FontStyle  FontStyle
	Foreground string
	Background string
	FontFamily string
	FontSize   float64
	LineHeight float64
}

// ParseRules turns the rules of a theme into one rule for each selector: the selectors of a
// string are split at the commas (not those of a list), invalid colors are dropped and the font
// style is read.
//
// Ported from vscode-textmate: src/theme.ts (parseTheme)
func ParseRules(raw []RawRule) []Rule {
	var out []Rule
	for i, entry := range raw {
		if entry.Settings == nil {
			continue
		}
		var scopes []string
		switch {
		case entry.Scope.isText:
			text := strings.TrimLeft(entry.Scope.text, ",")
			text = strings.TrimRight(text, ",")
			scopes = strings.Split(text, ",")
		case entry.Scope.isList:
			scopes = entry.Scope.list
		default:
			scopes = []string{""}
		}
		s := entry.Settings
		fontStyle := FontStyleNotSet
		if s.FontStyle != nil {
			fontStyle = FontStyleNone
			for _, segment := range strings.Split(*s.FontStyle, " ") {
				switch segment {
				case "italic":
					fontStyle |= FontStyleItalic
				case "bold":
					fontStyle |= FontStyleBold
				case "underline":
					fontStyle |= FontStyleUnderline
				case "strikethrough":
					fontStyle |= FontStyleStrikethrough
				}
			}
		}
		rule := Rule{Index: i, FontStyle: fontStyle}
		if s.Foreground != nil && isValidHexColor(*s.Foreground) {
			rule.Foreground = *s.Foreground
		}
		if s.Background != nil && isValidHexColor(*s.Background) {
			rule.Background = *s.Background
		}
		if s.FontFamily != nil {
			rule.FontFamily = *s.FontFamily
		}
		if s.FontSize != nil {
			rule.FontSize = *s.FontSize
		}
		if s.LineHeight != nil {
			rule.LineHeight = *s.LineHeight
		}
		for _, selector := range scopes {
			segments := strings.Split(strings.TrimSpace(selector), " ")
			r := rule
			r.Scope = segments[len(segments)-1]
			if len(segments) > 1 {
				r.ParentScopes = make([]string, 0, len(segments)-1)
				for j := len(segments) - 2; j >= 0; j-- {
					r.ParentScopes = append(r.ParentScopes, segments[j])
				}
			}
			out = append(out, r)
		}
	}
	return out
}

// compareStrings is strcmp of vscode-textmate.
func compareStrings(a, b string) int { return strings.Compare(a, b) }

// compareStringLists is strArrCmp of vscode-textmate: nil is before everything, then shorter
// lists, then the first part that differs.
func compareStringLists(a, b []string, aNil, bNil bool) int {
	if aNil && bNil {
		return 0
	}
	if aNil {
		return -1
	}
	if bNil {
		return 1
	}
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	for i := range a {
		if c := compareStrings(a[i], b[i]); c != 0 {
			return c
		}
	}
	return 0
}

// sortRules orders rules by scope, then parent scopes, then index, as the resolution of
// vscode-textmate does, so that the more general rules are inserted first.
func sortRules(rules []Rule) {
	sort.SliceStable(rules, func(i, j int) bool {
		a, b := rules[i], rules[j]
		if c := compareStrings(a.Scope, b.Scope); c != 0 {
			return c < 0
		}
		if c := compareStringLists(a.ParentScopes, b.ParentScopes, a.ParentScopes == nil, b.ParentScopes == nil); c != 0 {
			return c < 0
		}
		return a.Index < b.Index
	})
}
