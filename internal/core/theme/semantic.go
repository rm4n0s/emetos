package theme

import (
	"slices"
	"strings"
	"sync"
)

// Semantic token styling (SPEC §8.4, §9.12 "As built (step 12.9)"): the rules of a theme's semanticTokenColors, the rules of
// the user's editor.semanticTokenColorCustomizations, and the defaults of VS Code's token classification registry that map a
// token type and its modifiers to TextMate scopes, so that a theme that knows nothing about semantic tokens still colors them.
//
// Ported from VS Code: src/vs/platform/theme/common/tokenClassificationRegistry.ts (parseTokenSelector, the registry's default
// rules) and src/vs/workbench/services/themes/common/colorThemeData.ts (getTokenStyle, readSemanticTokenRule)

// SemanticStyle is the look a semantic token gives to the text it covers. A part it does not set is left as the TextMate token
// has it.
type SemanticStyle struct {
	// Foreground is the color as "#RRGGBB" or "#RRGGBBAA", or empty for none.
	Foreground string
	// Set is the font styles the style says something about (FontStyleItalic and its kin), and On those of them that are on:
	// {Set: Italic|Bold, On: Italic} makes italic and not bold.
	Set, On FontStyle
}

// IsZero says that the style says nothing.
func (s SemanticStyle) IsZero() bool { return s.Foreground == "" && s.Set == 0 }

// Apply is the font style of a TextMate token with this style on top: the flags the style sets are its own, the others stay.
func (s SemanticStyle) Apply(base FontStyle) FontStyle {
	if base < 0 {
		base = FontStyleNone
	}
	return base&^s.Set | s.On&s.Set
}

// fontStyleFlags are the four flags of a font style, with the names the theme files give them.
var fontStyleFlags = [...]struct {
	flag FontStyle
	name string
}{{FontStyleItalic, "italic"}, {FontStyleBold, "bold"}, {FontStyleUnderline, "underline"}, {FontStyleStrikethrough, "strikethrough"}}

// semanticSelector is (*|type)(.modifier)*(:language)?, as parseClassifierString reads it.
type semanticSelector struct {
	typ       string
	modifiers []string
	language  string
	hasLang   bool
}

// parseSemanticSelector reads a selector; false for one with no type.
func parseSemanticSelector(s string) (semanticSelector, bool) {
	k := len(s)
	var sel semanticSelector
	for i := len(s) - 1; i >= 0; i-- {
		if ch := s[i]; ch == ':' || ch == '.' {
			segment := s[i+1 : k]
			k = i
			if ch == ':' {
				sel.language, sel.hasLang = segment, true
			} else {
				sel.modifiers = append(sel.modifiers, segment)
			}
		}
	}
	sel.typ = s[:k]
	return sel, sel.typ != ""
}

// superTypes are the token types that stand for another: a rule for the supertype fits them too ("member" is deprecated for
// "method").
var superTypes = map[string]string{"member": "method"}

// match scores how well the selector fits a token, -1 for not at all: a language adds 10, a type its place in the hierarchy
// of the token's type (100 for the type itself, 99 for its supertype), and each modifier 100.
func (s semanticSelector) match(typ string, modifiers []string, language string) int {
	score := 0
	if s.hasLang {
		if s.language != language {
			return -1
		}
		score += 10
	}
	if s.typ != "*" {
		level := -1
		for t, i := typ, 0; t != ""; i++ {
			if t == s.typ {
				level = i
				break
			}
			t = superTypes[t]
		}
		if level < 0 {
			return -1
		}
		score += 100 - level
	}
	for _, m := range s.modifiers {
		found := false
		for _, have := range modifiers {
			found = found || have == m
		}
		if !found {
			return -1
		}
	}
	return score + len(s.modifiers)*100
}

// semanticRule is a rule of a theme or of the settings.
type semanticRule struct {
	selector semanticSelector
	style    SemanticStyle
}

// defaultSemanticRules are the registry's rules for the token types and the combinations of a type and modifiers: each probes
// TextMate scopes (the first probe the theme has a rule for wins), in the order VS Code registers them (a later rule wins
// over an earlier one that fits as well).
var defaultSemanticRules = func() []defaultRule {
	var out []defaultRule
	add := func(selector string, probes ...string) {
		sel, _ := parseSemanticSelector(selector)
		out = append(out, defaultRule{selector: sel, probes: probes})
	}
	for _, d := range [][]string{
		{"comment", "comment"}, {"string", "string"}, {"keyword", "keyword.control"}, {"number", "constant.numeric"},
		{"regexp", "constant.regexp"}, {"operator", "keyword.operator"}, {"namespace", "entity.name.namespace"},
		{"type", "entity.name.type", "support.type"}, {"struct", "entity.name.type.struct"},
		{"class", "entity.name.type.class", "support.class"}, {"interface", "entity.name.type.interface"},
		{"enum", "entity.name.type.enum"}, {"typeParameter", "entity.name.type.parameter"},
		{"function", "entity.name.function", "support.function"}, {"member"},
		{"method", "entity.name.function.member", "support.function"}, {"macro", "entity.name.function.preprocessor"},
		{"variable", "variable.other.readwrite", "entity.name.variable"}, {"parameter", "variable.parameter"},
		{"property", "variable.other.property"}, {"enumMember", "variable.other.enummember"}, {"event", "variable.other.event"},
		{"decorator", "entity.name.decorator", "entity.name.function"}, {"label"},
		{"variable.readonly", "variable.other.constant"}, {"property.readonly", "variable.other.constant.property"},
		{"type.defaultLibrary", "support.type"}, {"class.defaultLibrary", "support.class"},
		{"interface.defaultLibrary", "support.class"}, {"variable.defaultLibrary", "support.variable", "support.other.variable"},
		{"variable.defaultLibrary.readonly", "support.constant"}, {"property.defaultLibrary", "support.variable.property"},
		{"property.defaultLibrary.readonly", "support.constant.property"}, {"function.defaultLibrary", "support.function"},
		{"member.defaultLibrary", "support.function"},
	} {
		add(d[0], d[1:]...)
	}
	return out
}()

// defaultRule maps a selector to the TextMate scopes that stand for it.
type defaultRule struct {
	selector semanticSelector
	probes   []string
}

// SemanticTheme gives the look of a semantic token in a theme. It is safe for concurrent use.
type SemanticTheme struct {
	// Enabled is the theme's semanticHighlighting: whether it asks for semantic highlighting.
	Enabled bool

	tokens *TokenTheme
	rules  []semanticRule

	mu    sync.Mutex
	cache map[semanticKey]SemanticStyle
}

type semanticKey struct {
	typ, modifiers, language string
}

// NewSemanticTheme makes the semantic theme of a theme: its semanticTokenColors, and the token theme that the defaults fall
// back to (the TextMate rules of the same theme). The rules of a selector the registry does not know of are kept, as VS Code
// keeps them.
func NewSemanticTheme(t *Theme, tokens *TokenTheme) *SemanticTheme {
	s := &SemanticTheme{Enabled: t.SemanticHighlighting, tokens: tokens, cache: map[semanticKey]SemanticStyle{}}
	s.rules = readSemanticRules(t.SemanticTokenColors)
	return s
}

// WithRules makes the semantic theme with more rules, which win over the theme's: the rules of the user's
// editor.semanticTokenColorCustomizations. The selectors are the keys; a value is a color or an object with foreground and
// fontStyle (or bold, italic, underline, strikethrough). A selector or value that does not read is left out.
func (s *SemanticTheme) WithRules(rules map[string]any) *SemanticTheme {
	extra := readSemanticRules(rules)
	if len(extra) == 0 {
		return s
	}
	out := &SemanticTheme{Enabled: s.Enabled, tokens: s.tokens, cache: map[semanticKey]SemanticStyle{}}
	out.rules = append(append([]semanticRule(nil), s.rules...), extra...)
	return out
}

// readSemanticRules reads the rules of a semanticTokenColors object, in the order of the keys.
func readSemanticRules(m map[string]any) []semanticRule {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var out []semanticRule
	for _, k := range keys {
		sel, ok := parseSemanticSelector(k)
		if !ok {
			continue
		}
		if style, ok := readSemanticStyle(m[k]); ok {
			out = append(out, semanticRule{selector: sel, style: style})
		}
	}
	return out
}

// readSemanticStyle reads the value of a rule: a color, or an object. VS Code reads a font style, bold and the like in an
// object when one of them or a foreground is there.
func readSemanticStyle(v any) (SemanticStyle, bool) {
	switch x := v.(type) {
	case string:
		return SemanticStyle{Foreground: normalizeColor(x)}, x != ""
	case map[string]any:
		var st SemanticStyle
		found := false
		if fg, ok := x["foreground"].(string); ok {
			st.Foreground, found = normalizeColor(fg), true
		}
		if fs, ok := x["fontStyle"].(string); ok {
			// a font style says all four: the ones not named are off
			st.Set, found = FontStyleItalic|FontStyleBold|FontStyleUnderline|FontStyleStrikethrough, true
			for _, f := range fontStyleFlags {
				if strings.Contains(fs, f.name) {
					st.On |= f.flag
				}
			}
			return st, found
		}
		for _, f := range fontStyleFlags {
			if b, ok := x[f.name].(bool); ok {
				st.Set, found = st.Set|f.flag, true
				if b {
					st.On |= f.flag
				}
			}
		}
		return st, found
	}
	return SemanticStyle{}, false
}

// Style gives the look of a token of a type with modifiers in a language: the rules of the theme and of the settings that fit
// it, the best fit for each part of the style (later rules win among equal fits); whatever they leave unsaid is taken from
// the TextMate rules of the theme through the registry's defaults. ok is false when nothing says anything.
//
// Ported from VS Code: colorThemeData.ts (getTokenStyle)
func (s *SemanticTheme) Style(typ string, modifiers []string, language string) (SemanticStyle, bool) {
	key := semanticKey{typ, strings.Join(modifiers, "."), language}
	s.mu.Lock()
	if st, ok := s.cache[key]; ok {
		s.mu.Unlock()
		return st, !st.IsZero()
	}
	s.mu.Unlock()
	st := s.resolve(typ, modifiers, language)
	s.mu.Lock()
	s.cache[key] = st
	s.mu.Unlock()
	return st, !st.IsZero()
}

// styleScores holds, for each part of a style, the score of the rule that set it.
type styleScores struct {
	fg    int
	flags [4]int
}

func newScores() styleScores { return styleScores{fg: -1, flags: [4]int{-1, -1, -1, -1}} }

// process lets a style set the parts it says something about, where its fit is not worse than that of the rule that set them.
func (sc *styleScores) process(out *SemanticStyle, score int, st SemanticStyle) {
	if st.Foreground != "" && sc.fg <= score {
		sc.fg, out.Foreground = score, st.Foreground
	}
	for i, f := range fontStyleFlags {
		if st.Set&f.flag != 0 && sc.flags[i] <= score {
			sc.flags[i] = score
			out.Set |= f.flag
			out.On = out.On&^f.flag | st.On&f.flag
		}
	}
}

func (s *SemanticTheme) resolve(typ string, modifiers []string, language string) SemanticStyle {
	var out SemanticStyle
	sc := newScores()
	for _, r := range s.rules {
		if score := r.selector.match(typ, modifiers, language); score >= 0 {
			sc.process(&out, score, r.style)
		}
	}
	const taken = int(^uint(0) >> 1)
	// what the rules set is not replaced by a default
	if sc.fg >= 0 {
		sc.fg = taken
	}
	for i := range sc.flags {
		if sc.flags[i] >= 0 {
			sc.flags[i] = taken
		}
	}
	if s.tokens == nil {
		return out
	}
	for _, d := range defaultSemanticRules {
		score := d.selector.match(typ, modifiers, language)
		if score < 0 {
			continue
		}
		if st, ok := s.probe(d.probes); ok {
			sc.process(&out, score, st)
		}
	}
	return out
}

// probe is the style the TextMate rules give to the first of the scopes that has a rule.
func (s *SemanticTheme) probe(scopes []string) (SemanticStyle, bool) {
	for _, scope := range scopes {
		m, ok := s.tokens.Match([]string{scope})
		if !ok {
			continue
		}
		var st SemanticStyle
		if m.Foreground != 0 {
			st.Foreground = s.tokens.Color(m.Foreground)
		}
		if m.FontStyle != FontStyleNotSet {
			st.Set, st.On = FontStyleItalic|FontStyleBold|FontStyleUnderline|FontStyleStrikethrough, m.FontStyle
		}
		if !st.IsZero() {
			return st, true
		}
	}
	return SemanticStyle{}, false
}
