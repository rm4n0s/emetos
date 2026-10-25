package theme

import (
	"io/fs"
	"path"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/jsonc"
	"github.com/rm4n0s/emetos/internal/core/plist"
)

// Type is the kind of a theme: it decides the defaults of the colors that the theme does not set.
type Type string

// The theme types.
const (
	// TypeDark is a dark theme.
	TypeDark Type = "dark"
	// TypeLight is a light theme.
	TypeLight Type = "light"
	// TypeHCDark is a dark high contrast theme.
	TypeHCDark Type = "hc-dark"
	// TypeHCLight is a light high contrast theme.
	TypeHCLight Type = "hc-light"
)

// ParseType reads the type of a theme as a theme file or an extension's uiTheme writes it.
func ParseType(s string) (Type, bool) {
	switch s {
	case "dark", "vs-dark":
		return TypeDark, true
	case "light", "vs":
		return TypeLight, true
	case "hc-dark", "hc-black", "hc":
		return TypeHCDark, true
	case "hc-light", "hcLight":
		return TypeHCLight, true
	}
	return "", false
}

// defaultColorValue in the colors of a theme gives the color its default.
const defaultColorValue = "default"

// maxIncludeDepth is how many themes may include one another in a chain.
const maxIncludeDepth = 16

// Theme is a color theme read from its files and the files it includes.
type Theme struct {
	// Name is the name in the theme file; empty when it has none.
	Name string
	// Type is the type in the theme file, or the one the caller said.
	Type Type
	// Colors are the workbench colors by id, as hex strings. A color the theme sets to "default"
	// is not here.
	Colors map[string]string
	// TokenColors are the rules for the colors of tokens, those of the included themes first.
	TokenColors []RawRule
	// SemanticHighlighting tells whether the theme asks for semantic highlighting.
	SemanticHighlighting bool
	// SemanticTokenColors are the rules for semantic tokens as the file has them, kept for the
	// language servers (area 3). The rules of an included theme come first.
	SemanticTokenColors map[string]any
}

// Load reads the theme in the file name of fsys: JSON with comments, which may include another
// theme and may give its tokenColors in a file of their own (TextMate .tmTheme, or JSON), or a
// .tmTheme file itself. typ is the type of the theme when the file does not say; the error has
// one of the tags ThemeFileUnreadable, ThemeSyntaxError, ThemeNotObject, ThemeFieldInvalid,
// ThemeIncludeInvalid, ThemeIncludeCycle, TextMateThemeInvalid.
func Load(fsys fs.FS, name string, typ Type) (*Theme, error) {
	t := &Theme{Type: typ, Colors: map[string]string{}, SemanticTokenColors: map[string]any{}}
	if err := t.load(fsys, name, 0, map[string]bool{}); err != nil {
		return nil, err
	}
	return t, nil
}

// load reads a theme file into t. depth and active guard the includes.
func (t *Theme) load(fsys fs.FS, name string, depth int, active map[string]bool) error {
	name = path.Clean(name)
	if depth > maxIncludeDepth || active[name] {
		return errors.New("ThemeIncludeCycle", "themes include one another in a loop or too deep", "file", name)
	}
	active[name] = true
	defer delete(active, name)

	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return errors.NewErr("ThemeFileUnreadable", err).SetMetadata("file", name)
	}
	if path.Ext(name) != ".json" {
		return t.loadTextMateTheme(data, name)
	}
	value, problems := jsonc.Parse(string(data), jsonc.ParseOptions{})
	if len(problems) > 0 {
		return errors.New("ThemeSyntaxError", "the theme file is not valid JSON", "file", name, "error", problems[0].Message(), "offset", problems[0].Offset)
	}
	doc, ok := value.(map[string]any)
	if !ok {
		return errors.New("ThemeNotObject", "a theme file is a JSON object", "file", name)
	}
	if include, has := doc["include"]; has {
		s, isString := include.(string)
		if !isString || s == "" {
			return errors.New("ThemeIncludeInvalid", "include is the path of a theme file", "file", name)
		}
		if err := t.load(fsys, path.Join(path.Dir(name), s), depth+1, active); err != nil {
			return err
		}
	}
	if s, isString := doc["name"].(string); isString && depth == 0 {
		t.Name = s
	}
	if s, isString := doc["type"].(string); isString && depth == 0 {
		if typ, known := ParseType(s); known {
			t.Type = typ
		}
	}
	// The old format: a list of settings, whose first rule sets the colors of the editor.
	if settings, isList := doc["settings"].([]any); isList {
		t.addSettings(rulesOf(settings))
		return nil
	}
	if b, isBool := doc["semanticHighlighting"].(bool); isBool && b {
		t.SemanticHighlighting = true
	}
	if colors, has := doc["colors"]; has {
		m, isMap := colors.(map[string]any)
		if !isMap {
			return errors.New("ThemeFieldInvalid", "colors is an object", "file", name, "field", "colors")
		}
		for id, v := range m {
			switch s, isString := v.(string); {
			case isString && s == defaultColorValue:
				delete(t.Colors, id)
			case isString:
				t.Colors[id] = s
			}
		}
	}
	switch tokenColors := doc["tokenColors"].(type) {
	case nil:
	case []any:
		t.TokenColors = append(t.TokenColors, rulesOf(tokenColors)...)
	case string:
		file := path.Join(path.Dir(name), tokenColors)
		fileData, err := fs.ReadFile(fsys, file)
		if err != nil {
			return errors.NewErr("ThemeFileUnreadable", err).SetMetadata("file", file)
		}
		if err := t.loadTextMateTheme(fileData, file); err != nil {
			return err
		}
	default:
		return errors.New("ThemeFieldInvalid", "tokenColors is a list of rules or the path of a TextMate theme", "file", name, "field", "tokenColors")
	}
	if semantic, isMap := doc["semanticTokenColors"].(map[string]any); isMap {
		for selector, v := range semantic {
			t.SemanticTokenColors[selector] = v
		}
	}
	return nil
}

// loadTextMateTheme adds the settings of a .tmTheme file, which is a plist.
func (t *Theme) loadTextMateTheme(data []byte, name string) error {
	root, err := plist.Parse(data)
	if err != nil {
		return err
	}
	settings, isList := root["settings"].([]any)
	if !isList {
		return errors.New("TextMateThemeInvalid", "the settings of a TextMate theme are a list", "file", name)
	}
	t.addSettings(rulesOf(settings))
	return nil
}

// settingToColors maps the settings of a rule without scope in a TextMate theme to the workbench
// colors they set.
//
// Ported from VS Code: src/vs/workbench/services/themes/common/themeCompatibility.ts
var settingToColors = map[string][]string{
	"background":                {"editor.background"},
	"foreground":                {"editor.foreground"},
	"selection":                 {"editor.selectionBackground"},
	"inactiveSelection":         {"editor.inactiveSelectionBackground"},
	"selectionHighlightColor":   {"editor.selectionHighlightBackground"},
	"findMatchHighlight":        {"editor.findMatchHighlightBackground", "peekViewResult.matchHighlightBackground"},
	"currentFindMatchHighlight": {"editor.findMatchBackground"},
	"hoverHighlight":            {"editor.hoverHighlightBackground"},
	"wordHighlight":             {"editor.wordHighlightBackground"},
	"wordHighlightStrong":       {"editor.wordHighlightStrongBackground"},
	"findRangeHighlight":        {"editor.findRangeHighlightBackground"},
	"referenceHighlight":        {"peekViewEditor.matchHighlightBackground"},
	"lineHighlight":             {"editor.lineHighlightBackground"},
	"rangeHighlight":            {"editor.rangeHighlightBackground"},
	"caret":                     {"editorCursor.foreground"},
	"invisibles":                {"editorWhitespace.foreground"},
	"guide":                     {"editorIndentGuide.background1"},
	"activeGuide":               {"editorIndentGuide.activeBackground1"},
	"ansiBlack":                 {"terminal.ansiBlack"},
	"ansiRed":                   {"terminal.ansiRed"},
	"ansiGreen":                 {"terminal.ansiGreen"},
	"ansiYellow":                {"terminal.ansiYellow"},
	"ansiBlue":                  {"terminal.ansiBlue"},
	"ansiMagenta":               {"terminal.ansiMagenta"},
	"ansiCyan":                  {"terminal.ansiCyan"},
	"ansiWhite":                 {"terminal.ansiWhite"},
	"ansiBrightBlack":           {"terminal.ansiBrightBlack"},
	"ansiBrightRed":             {"terminal.ansiBrightRed"},
	"ansiBrightGreen":           {"terminal.ansiBrightGreen"},
	"ansiBrightYellow":          {"terminal.ansiBrightYellow"},
	"ansiBrightBlue":            {"terminal.ansiBrightBlue"},
	"ansiBrightMagenta":         {"terminal.ansiBrightMagenta"},
	"ansiBrightCyan":            {"terminal.ansiBrightCyan"},
	"ansiBrightWhite":           {"terminal.ansiBrightWhite"},
}

// addSettings adds the rules of a TextMate theme (or of a theme in the old format): the rules
// with no scope also set workbench colors, and keep only their foreground, background and font
// style as token colors.
//
// Ported from VS Code: src/vs/workbench/services/themes/common/themeCompatibility.ts (convertSettings)
func (t *Theme) addSettings(rules []RawRule) {
	for _, rule := range rules {
		if !rule.Scope.isText && !rule.Scope.isList || rule.Scope.isText && rule.Scope.text == "" {
			if rule.Settings == nil {
				rule.Settings = &RuleSettings{}
			} else {
				s := rule.Settings
				for key, value := range map[string]*string{"background": s.Background, "foreground": s.Foreground} {
					if value == nil {
						continue
					}
					for _, id := range settingToColors[key] {
						t.Colors[id] = *value
					}
				}
				s.FontFamily, s.FontSize, s.LineHeight = nil, nil, nil
			}
			for key, value := range rule.Settings.other {
				for _, id := range settingToColors[key] {
					t.Colors[id] = value
				}
			}
		}
		t.TokenColors = append(t.TokenColors, rule)
	}
}

// rulesOf reads the rules of a list from a theme file; what is not a rule is left out.
func rulesOf(list []any) []RawRule {
	out := make([]RawRule, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var rule RawRule
		rule.Name, _ = m["name"].(string)
		switch scope := m["scope"].(type) {
		case string:
			rule.Scope = ScopeText(scope)
		case []any:
			var selectors []string
			for _, s := range scope {
				if str, isString := s.(string); isString {
					selectors = append(selectors, str)
				}
			}
			rule.Scope = ScopeList(selectors...)
		}
		if settings, isMap := m["settings"].(map[string]any); isMap {
			rule.Settings = &RuleSettings{}
			rule.Settings.FontStyle = stringField(settings, "fontStyle")
			rule.Settings.Foreground = stringField(settings, "foreground")
			rule.Settings.Background = stringField(settings, "background")
			rule.Settings.FontFamily = stringField(settings, "fontFamily")
			rule.Settings.FontSize = numberField(settings, "fontSize")
			rule.Settings.LineHeight = numberField(settings, "lineHeight")
			for key := range settingToColors {
				if key == "foreground" || key == "background" {
					continue
				}
				if s := stringField(settings, key); s != nil {
					if rule.Settings.other == nil {
						rule.Settings.other = map[string]string{}
					}
					rule.Settings.other[key] = *s
				}
			}
		}
		out = append(out, rule)
	}
	return out
}

func stringField(m map[string]any, key string) *string {
	if s, ok := m[key].(string); ok {
		return &s
	}
	return nil
}

func numberField(m map[string]any, key string) *float64 {
	switch n := m[key].(type) {
	case float64:
		return &n
	case int64:
		f := float64(n)
		return &f
	}
	return nil
}

// normalizeColor writes a hex color with its digits in upper case, or gives "" when it is not
// one (a color with 3, 4, 6 or 8 digits).
func normalizeColor(color string) string {
	if !isValidHexColor(color) {
		return ""
	}
	return strings.ToUpper(color)
}
