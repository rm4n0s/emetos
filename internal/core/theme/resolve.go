package theme

// editorColors are the defaults of editor.foreground and editor.background for a theme that does
// not set them (the color registry of step 4.8 holds the defaults of all colors).
//
// Ported from VS Code: src/vs/platform/theme/common/colors/editorColors.ts
var editorColors = map[Type][2]string{
	TypeDark:    {"#BBBBBB", "#1E1E1E"},
	TypeLight:   {"#333333", "#FFFFFF"},
	TypeHCDark:  {"#FFFFFF", "#000000"},
	TypeHCLight: {"#292929", "#FFFFFF"},
}

// defaultTokenColors are the rules that a theme without a token.info-token rule gets, for the
// colors of the tokens of the output and log languages.
//
// Ported from VS Code: src/vs/workbench/services/themes/common/colorThemeData.ts (defaultThemeColors)
var defaultTokenColors = map[Type][4]string{
	TypeLight:   {"#316bcd", "#cd9731", "#cd3131", "#800080"},
	TypeDark:    {"#6796e6", "#cd9731", "#f44747", "#b267e6"},
	TypeHCLight: {"#316bcd", "#cd9731", "#cd3131", "#800080"},
	TypeHCDark:  {"#6796e6", "#008000", "#FF0000", "#b267e6"},
}

// Color gives the workbench color with an id as the theme sets it, or "" when it does not.
func (t *Theme) Color(id string) string { return t.Colors[id] }

// TokenRules are the token color rules of the theme as the editor uses them: first the default
// rule, with the foreground and background of the editor; then the rules of the theme and the
// custom ones (which win), with their colors in upper case; and the rules for the info, warning,
// error and debug tokens unless the theme has its own.
//
// Ported from VS Code: src/vs/workbench/services/themes/common/colorThemeData.ts (tokenColors)
func (t *Theme) TokenRules(custom []RawRule) []RawRule {
	fallback, ok := editorColors[t.Type]
	if !ok {
		fallback = editorColors[TypeDark]
	}
	foreground, background := t.Colors["editor.foreground"], t.Colors["editor.background"]
	if foreground == "" {
		foreground = fallback[0]
	}
	if background == "" {
		background = fallback[1]
	}
	fg, bg := normalizeColor(foreground), normalizeColor(background)
	out := []RawRule{{Settings: &RuleSettings{Foreground: optional(fg), Background: optional(bg)}}}
	hasDefaultTokens := false
	add := func(rule RawRule) {
		if rule.Settings == nil || !rule.hasScope() {
			return
		}
		if rule.Scope.isText && rule.Scope.text == "token.info-token" {
			hasDefaultTokens = true
		}
		s := rule.Settings
		out = append(out, RawRule{Name: rule.Name, Scope: rule.Scope, Settings: &RuleSettings{
			Foreground: optional(normalizeColor(deref(s.Foreground))),
			Background: optional(normalizeColor(deref(s.Background))),
			FontStyle:  s.FontStyle,
			FontSize:   s.FontSize,
			FontFamily: s.FontFamily,
			LineHeight: s.LineHeight,
		}})
	}
	for _, rule := range t.TokenColors {
		add(rule)
	}
	for _, rule := range custom {
		add(rule)
	}
	if !hasDefaultTokens {
		colors, ok := defaultTokenColors[t.Type]
		if !ok {
			colors = defaultTokenColors[TypeDark]
		}
		for i, scope := range []string{"token.info-token", "token.warn-token", "token.error-token", "token.debug-token"} {
			color := colors[i]
			add(RawRule{Scope: ScopeText(scope), Settings: &RuleSettings{Foreground: &color}})
		}
	}
	return out
}

// TokenTheme resolves the token rules of the theme (see TokenRules) into the theme that gives a
// style to the scopes of a token.
func (t *Theme) TokenTheme(custom []RawRule) *TokenTheme {
	return NewTokenTheme(ParseRules(t.TokenRules(custom)))
}

// hasScope tells whether the rule has a scope, as JavaScript counts it: no scope and the empty
// string are none.
func (r RawRule) hasScope() bool {
	return r.Scope.isList || r.Scope.isText && r.Scope.text != ""
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
