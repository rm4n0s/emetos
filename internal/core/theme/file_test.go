package theme

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/errors"
)

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

const tmTheme = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>name</key><string>Tm</string>
<key>settings</key><array>
  <dict><key>settings</key><dict>
    <key>background</key><string>#112233</string>
    <key>foreground</key><string>#AABBCC</string>
    <key>caret</key><string>#FF0000</string>
    <key>fontFamily</key><string>Mono</string>
  </dict></dict>
  <dict><key>name</key><string>Comment</string><key>scope</key><string>comment</string>
    <key>settings</key><dict><key>foreground</key><string>#888888</string><key>fontStyle</key><string>italic</string></dict></dict>
</array></dict></plist>`

func TestLoad(t *testing.T) {
	fsys := fstest.MapFS{
		"base.json": file(`{
			// a comment
			"name": "Base", "type": "light",
			"colors": {"editor.background": "#111111", "editor.foreground": "#222222", "keep": "#333333", "drop": "#444444"},
			"tokenColors": [{"scope": "comment", "settings": {"foreground": "#555555"}}],
			"semanticHighlighting": true,
			"semanticTokenColors": {"variable": "#666666", "function": {"bold": true}}
		}`),
		"child.json": file(`{
			"name": "Child", "include": "./base.json",
			"colors": {"editor.background": "#AAAAAA", "drop": "default", "ignored": 7},
			"tokenColors": [{"name": "s", "scope": ["string", "string.quoted"], "settings": {"foreground": "#777777", "fontStyle": "bold"}},
				"not a rule", {"scope": "x"}],
			"semanticTokenColors": {"variable": "#999999"}
		}`),
		"dir/deep.json":      file(`{"include": "../base.json", "tokenColors": "./syntax.tmTheme"}`),
		"dir/syntax.tmTheme": file(tmTheme),
		"old.json":           file(`{"settings": [{"settings": {"background": "#010203", "foreground": "#040506", "selection": "#070809", "fontSize": 12}}, {"scope": "string", "settings": {"foreground": "#0A0B0C"}}]}`),
		"plain.tmTheme":      file(tmTheme),
	}
	fg := func(s string) *string { return &s }
	t.Run("a theme with an include", func(t *testing.T) {
		got, err := Load(fsys, "child.json", TypeDark)
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "Child" || got.Type != TypeDark {
			t.Errorf("name %q, type %q: the name is the child's and the type the caller's, as the child has none", got.Name, got.Type)
		}
		wantColors := map[string]string{"editor.background": "#AAAAAA", "editor.foreground": "#222222", "keep": "#333333"}
		if !reflect.DeepEqual(got.Colors, wantColors) {
			t.Errorf("colors %v, want %v", got.Colors, wantColors)
		}
		if !got.SemanticHighlighting {
			t.Error("semanticHighlighting comes from the included theme")
		}
		if !reflect.DeepEqual(got.SemanticTokenColors, map[string]any{"variable": "#999999", "function": map[string]any{"bold": true}}) {
			t.Errorf("semantic token colors %v", got.SemanticTokenColors)
		}
		if len(got.TokenColors) != 3 || got.TokenColors[0].Scope.text != "comment" || got.TokenColors[1].Name != "s" || !got.TokenColors[1].Scope.isList {
			t.Errorf("token colors: the included ones come first: %+v", got.TokenColors)
		}
		if got.TokenColors[2].Settings != nil {
			t.Errorf("a rule without settings has none: %+v", got.TokenColors[2])
		}
	})
	t.Run("the type of the file", func(t *testing.T) {
		got, err := Load(fsys, "base.json", TypeDark)
		if err != nil {
			t.Fatal(err)
		}
		if got.Type != TypeLight || got.Name != "Base" {
			t.Errorf("type %q, name %q", got.Type, got.Name)
		}
	})
	t.Run("tokenColors in a file", func(t *testing.T) {
		got, err := Load(fsys, "dir/deep.json", TypeDark)
		if err != nil {
			t.Fatal(err)
		}
		// The default rule of the tmTheme sets the colors of the editor and is a token rule too.
		if got.Colors["editor.background"] != "#112233" || got.Colors["editor.foreground"] != "#AABBCC" || got.Colors["editorCursor.foreground"] != "#FF0000" {
			t.Errorf("colors from the default rule: %v", got.Colors)
		}
		if len(got.TokenColors) != 3 || got.TokenColors[2].Scope.text != "comment" {
			t.Errorf("token colors %+v", got.TokenColors)
		}
		if s := got.TokenColors[1].Settings; s.FontFamily != nil || !reflect.DeepEqual(s.Foreground, fg("#AABBCC")) {
			t.Errorf("the default rule keeps only the colors and the font style: %+v", s)
		}
	})
	t.Run("a tmTheme", func(t *testing.T) {
		got, err := Load(fsys, "plain.tmTheme", TypeLight)
		if err != nil {
			t.Fatal(err)
		}
		if got.Colors["editor.background"] != "#112233" || got.Type != TypeLight || len(got.TokenColors) != 2 {
			t.Errorf("%+v", got)
		}
	})
	t.Run("the old format", func(t *testing.T) {
		got, err := Load(fsys, "old.json", TypeDark)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"editor.background": "#010203", "editor.foreground": "#040506", "editor.selectionBackground": "#070809"}
		if !reflect.DeepEqual(got.Colors, want) || len(got.TokenColors) != 2 {
			t.Errorf("colors %v, rules %d", got.Colors, len(got.TokenColors))
		}
	})
}

func TestLoadErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"syntax.json":        file(`{"colors": `),
		"array.json":         file(`[1]`),
		"colors.json":        file(`{"colors": []}`),
		"tokens.json":        file(`{"tokenColors": 5}`),
		"include.json":       file(`{"include": 5}`),
		"noinclude.json":     file(`{"include": "./gone.json"}`),
		"notokens.json":      file(`{"tokenColors": "./gone.tmTheme"}`),
		"a.json":             file(`{"include": "./b.json"}`),
		"b.json":             file(`{"include": "./a.json"}`),
		"self.json":          file(`{"include": "./self.json"}`),
		"badplist.tmTheme":   file(`not a plist`),
		"nosettings.tmTheme": file(`<plist><dict><key>name</key><string>x</string></dict></plist>`),
		"usesbad.json":       file(`{"tokenColors": "./badplist.tmTheme"}`),
	}
	cases := []struct{ file, route string }{
		{"missing.json", "Load->Theme.load.ThemeFileUnreadable"},
		{"syntax.json", "Load->Theme.load.ThemeSyntaxError"},
		{"array.json", "Load->Theme.load.ThemeNotObject"},
		{"colors.json", "Load->Theme.load.ThemeFieldInvalid"},
		{"tokens.json", "Load->Theme.load.ThemeFieldInvalid"},
		{"include.json", "Load->Theme.load.ThemeIncludeInvalid"},
		{"noinclude.json", "Theme.load->Theme.load.ThemeFileUnreadable"},
		{"notokens.json", "Load->Theme.load.ThemeFileUnreadable"},
		{"a.json", "Theme.load->Theme.load->Theme.load.ThemeIncludeCycle"},
		{"self.json", "Theme.load->Theme.load.ThemeIncludeCycle"},
		{"badplist.tmTheme", "Theme.loadTextMateTheme.TextMateThemeInvalid"},
		{"nosettings.tmTheme", "Theme.loadTextMateTheme.TextMateThemeInvalid"},
		{"usesbad.json", "Theme.loadTextMateTheme.TextMateThemeInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			_, err := Load(fsys, tc.file, TypeDark)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tc.route)
			}
		})
	}
}

func TestParseType(t *testing.T) {
	for in, want := range map[string]Type{
		"dark": TypeDark, "vs-dark": TypeDark, "light": TypeLight, "vs": TypeLight,
		"hc-dark": TypeHCDark, "hc-black": TypeHCDark, "hc": TypeHCDark, "hc-light": TypeHCLight, "hcLight": TypeHCLight,
	} {
		if got, ok := ParseType(in); !ok || got != want {
			t.Errorf("ParseType(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := ParseType("blue"); ok {
		t.Error("an unknown type is not a type")
	}
}

func TestTokenRules(t *testing.T) {
	scopes := func(rules []RawRule) []string {
		var out []string
		for _, r := range rules {
			out = append(out, r.Scope.text)
		}
		return out
	}
	t.Run("the default rule", func(t *testing.T) {
		th := &Theme{Type: TypeDark, Colors: map[string]string{"editor.foreground": "#abcdef", "editor.background": "#123"}}
		rules := th.TokenRules(nil)
		d := rules[0].Settings
		if rules[0].hasScope() || *d.Foreground != "#ABCDEF" || *d.Background != "#123" {
			t.Errorf("default rule %+v", d)
		}
	})
	t.Run("fallback colors by type", func(t *testing.T) {
		for typ, want := range map[Type][2]string{
			TypeDark: {"#BBBBBB", "#1E1E1E"}, TypeLight: {"#333333", "#FFFFFF"}, TypeHCDark: {"#FFFFFF", "#000000"}, TypeHCLight: {"#292929", "#FFFFFF"}, "odd": {"#BBBBBB", "#1E1E1E"},
		} {
			d := (&Theme{Type: typ}).TokenRules(nil)[0].Settings
			if *d.Foreground != want[0] || *d.Background != want[1] {
				t.Errorf("%s: %s on %s", typ, *d.Foreground, *d.Background)
			}
		}
	})
	t.Run("rules, custom rules and the default tokens", func(t *testing.T) {
		th := &Theme{Type: TypeLight, TokenColors: []RawRule{
			setting("comment", nil, str("#00ff00"), nil),
			{Settings: &RuleSettings{Foreground: str("#ff0000")}}, // no scope: only the default rule counts
			{Scope: ScopeText("x")},                               // no settings
			setting("bad", nil, str("red"), str("#abc")),
		}}
		rules := th.TokenRules([]RawRule{setting("comment", nil, str("#0000ff"), nil)})
		want := []string{"", "comment", "bad", "comment", "token.info-token", "token.warn-token", "token.error-token", "token.debug-token"}
		if got := scopes(rules); !reflect.DeepEqual(got, want) {
			t.Fatalf("scopes %q, want %q", got, want)
		}
		if *rules[1].Settings.Foreground != "#00FF00" || rules[2].Settings.Foreground != nil || *rules[2].Settings.Background != "#ABC" {
			t.Errorf("colors are upper case, and invalid ones are dropped: %+v %+v", rules[1].Settings, rules[2].Settings)
		}
		if *rules[4].Settings.Foreground != "#316BCD" {
			t.Errorf("light info token: %s", *rules[4].Settings.Foreground)
		}
		// The custom rule wins over the theme's.
		theme := th.TokenTheme([]RawRule{setting("comment", nil, str("#0000ff"), nil)})
		if got, _ := foregroundOf(theme, "source", "comment.line"); got != "#0000FF" {
			t.Errorf("comment: %q", got)
		}
	})
	t.Run("a theme with its own default tokens keeps them", func(t *testing.T) {
		th := &Theme{Type: TypeDark, TokenColors: []RawRule{setting("token.info-token", nil, str("#010101"), nil)}}
		if got := scopes(th.TokenRules(nil)); !reflect.DeepEqual(got, []string{"", "token.info-token"}) {
			t.Errorf("scopes %q", got)
		}
	})
}

func TestMatchStack(t *testing.T) {
	th := themeOf(colorsOf("#111111", "#222222"), setting("meta.tag entity", nil, str("#333333"), nil))
	if _, ok := th.MatchStack(nil); !ok {
		t.Error("no scopes give the defaults")
	}
}
