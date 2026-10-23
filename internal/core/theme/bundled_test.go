package theme

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/errors"
)

func TestBundledThemes(t *testing.T) {
	list, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	types := map[string]Type{}
	for _, info := range list {
		got = append(got, info.Label)
		types[info.Label] = info.Type
		if !info.Bundled {
			t.Errorf("%s is bundled", info.Label)
		}
	}
	sort.Strings(got)
	if len(list) != 6 {
		t.Fatalf("%d themes: %q", len(list), got)
	}
	for label, typ := range map[string]Type{
		"Dark Modern": TypeDark, "Dark+": TypeDark, "Light Modern": TypeLight, "Light+": TypeLight,
		"Dark High Contrast": TypeHCDark, "Light High Contrast": TypeHCLight,
	} {
		if types[label] != typ {
			t.Errorf("%s has the type %q, want %q (themes: %q)", label, types[label], typ, got)
		}
	}
	for _, name := range []string{DefaultDark, DefaultLight} {
		if _, ok := types[name]; !ok {
			t.Errorf("the default %q is not bundled", name)
		}
	}
}

func TestLoadBundled(t *testing.T) {
	list, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range list {
		t.Run(info.Label, func(t *testing.T) {
			th, err := LoadBundled(info.Label)
			if err != nil {
				t.Fatal(err)
			}
			if th.Type != info.Type || th.Name == "" || len(th.Colors) == 0 || len(th.TokenColors) == 0 {
				t.Errorf("type %q, name %q, %d colors, %d rules", th.Type, th.Name, len(th.Colors), len(th.TokenColors))
			}
			if _, err := LoadBundled(info.ID); err != nil {
				t.Errorf("by id: %v", err)
			}
		})
	}
	// Dark Modern includes Dark+, which includes Visual Studio Dark: the colors of all three.
	modern, err := LoadBundled("Dark Modern")
	if err != nil {
		t.Fatal(err)
	}
	plus, err := LoadBundled("Dark+")
	if err != nil {
		t.Fatal(err)
	}
	if modern.Colors["editor.background"] != "#1F1F1F" || plus.Colors["editor.background"] != "#1E1E1E" {
		t.Errorf("backgrounds: modern %q, plus %q", modern.Colors["editor.background"], plus.Colors["editor.background"])
	}
	if len(modern.TokenColors) != len(plus.TokenColors) || !modern.SemanticHighlighting {
		t.Errorf("modern has the rules of plus: %d and %d", len(modern.TokenColors), len(plus.TokenColors))
	}
	_, err = LoadBundled("Nope")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("LoadBundled.BundledThemeNotFound") {
		t.Errorf("err = %v", err)
	}
}

// resolved colors of Dark+ for scopes that the rules of the theme decide, from the rules of
// extensions/theme-defaults/themes/dark_plus.json and dark_vs.json.
func TestBundledResolution(t *testing.T) {
	cases := []struct {
		theme  string
		scopes []string
		want   string
	}{
		{"Dark+", []string{"source.go", "keyword.package.go"}, "#569CD6"},
		{"Dark+", []string{"source.go", "keyword.control.import.go"}, "#C586C0"},
		{"Dark+", []string{"source.go", "entity.name.type.package.go"}, "#4EC9B0"},
		{"Dark+", []string{"source.go", "comment.line.double-slash.go"}, "#6A9955"},
		{"Dark+", []string{"source.go", "string.quoted.double.go"}, "#CE9178"},
		{"Dark+", []string{"source.go"}, "#D4D4D4"},
		{"Light+", []string{"source.go", "keyword.package.go"}, "#0000FF"},
		{"Light+", []string{"source.go"}, "#000000"},
		{"Dark Modern", []string{"source.go"}, "#CCCCCC"},
		{"Light Modern", []string{"source.go"}, "#3B3B3B"},
		{"Dark High Contrast", []string{"source.go"}, "#FFFFFF"},
		{"Light High Contrast", []string{"source.go"}, "#292929"},
	}
	for _, tc := range cases {
		t.Run(tc.theme+" "+strings.Join(tc.scopes, " "), func(t *testing.T) {
			th, err := LoadBundled(tc.theme)
			if err != nil {
				t.Fatal(err)
			}
			tt := th.TokenTheme(nil)
			if got := tt.Color(tt.Resolve(tc.scopes).Foreground); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveInheritsFromTheScopesBefore(t *testing.T) {
	th := themeOf(
		colorsOf("#111111", "#222222"),
		setting("source", str("italic"), str("#AAAAAA"), nil),
		setting("string", nil, nil, str("#BBBBBB")),
		setting("source.js string.quoted", str("bold"), str("#CCCCCC"), nil),
	)
	cases := []struct {
		name   string
		scopes []string
		want   [3]string
	}{
		{"nothing matches", []string{"other"}, [3]string{"none", "#111111", "#222222"}},
		{"the outer scope", []string{"source.js"}, [3]string{"italic", "#AAAAAA", "#222222"}},
		{"the inner scope adds its background", []string{"source.js", "string.quoted.double"}, [3]string{"bold", "#CCCCCC", "#BBBBBB"}},
		{"a rule that needs the parent", []string{"source.ts", "string.quoted.double"}, [3]string{"italic", "#AAAAAA", "#BBBBBB"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := th.Resolve(tc.scopes)
			got := [3]string{s.FontStyle.String(), th.Color(s.Foreground), th.Color(s.Background)}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadUserThemes(t *testing.T) {
	fsys := fstest.MapFS{
		"b-light.json":  file(`{"name": "My Light", "type": "light", "colors": {"editor.background": "#FFFFFF"}}`),
		"a.json":        file(`{"colors": {"editor.background": "#000000"}}`),
		"c.tmTheme":     file(tmTheme),
		"broken.json":   file(`{`),
		"readme.txt":    file(`not a theme`),
		"folder/x.json": file(`{}`),
	}
	themes, problems, err := LoadUserThemes(fsys)
	if err != nil {
		t.Fatal(err)
	}
	var got []Info
	for _, th := range themes {
		got = append(got, th.Info)
	}
	want := []Info{
		{ID: "a", Label: "a", Type: TypeDark, Path: "a.json"},
		{ID: "b-light", Label: "My Light", Type: TypeLight, Path: "b-light.json"},
		{ID: "c", Label: "c", Type: TypeDark, Path: "c.tmTheme"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("themes %+v, want %+v", got, want)
	}
	if len(problems) != 1 {
		t.Fatalf("problems: %v", problems)
	}
	if appErr, ok := errors.FromError(problems[0]); !ok || !appErr.HasRoute("Load->Theme.load.ThemeSyntaxError") {
		t.Errorf("problem: %v", problems[0])
	}

	_, _, err = LoadUserThemes(brokenFS{})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("LoadUserThemes.UserThemesUnreadable") {
		t.Errorf("err = %v", err)
	}
}

// brokenFS is a file system whose folder cannot be listed.
type brokenFS struct{}

func (brokenFS) Open(name string) (fs.File, error) { return nil, fs.ErrPermission }

// TestColorizeOracle checks, for every token of VS Code's colorize results and every default
// theme, that the foreground color Emetos resolves for the token's scopes is the one that
// VS Code's tokenizer gave it. The results hold the color after the rule that made it, as
// "keyword.operator: #D4D4D4".
func TestColorizeOracle(t *testing.T) {
	results := filepath.Join("..", "..", "..", "reference", "vscode", "extensions", "vscode-colorize-tests", "test", "colorize-results")
	files, err := filepath.Glob(filepath.Join(results, "*.json"))
	if err != nil || len(files) == 0 {
		t.Skip("reference/ is missing: run scripts/fetch-reference.sh")
	}
	themeFiles := map[string]string{
		"dark_plus": "Dark+", "light_plus": "Light+", "dark_modern": "Dark Modern", "light_modern": "Light Modern",
		"hc_black": "Dark High Contrast", "hc_light": "Light High Contrast",
	}
	themes := map[string]*TokenTheme{}
	for key, label := range themeFiles {
		th, err := LoadBundled(label)
		if err != nil {
			t.Fatal(err)
		}
		themes[key] = th.TokenTheme(nil)
	}
	// The Visual Studio themes are not offered, but Dark+ and Light+ include them.
	for key, file := range map[string]string{"dark_vs": "themes/dark_vs.json", "light_vs": "themes/light_vs.json"} {
		typ := TypeDark
		if key == "light_vs" {
			typ = TypeLight
		}
		th, err := Load(assets, "assets/"+file, typ)
		if err != nil {
			t.Fatal(err)
		}
		themes[key] = th.TokenTheme(nil)
	}
	checked := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var tokens []struct {
			C string            `json:"c"`
			T string            `json:"t"`
			R map[string]string `json:"r"`
		}
		if err := json.Unmarshal(data, &tokens); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		bad := 0
		for _, tok := range tokens {
			scopes := strings.Fields(tok.T)
			for key, tt := range themes {
				expected, ok := tok.R[key]
				if !ok {
					continue
				}
				want := expected[strings.LastIndex(expected, ": ")+2:]
				got := tt.Color(tt.Resolve(scopes).Foreground)
				// VS Code writes an opaque color without its alpha.
				if len(got) == 9 && strings.HasSuffix(got, "FF") {
					got = got[:7]
				}
				checked++
				if !strings.EqualFold(got, want) {
					if bad++; bad <= 3 {
						t.Errorf("%s: %q in %s with %s: got %s, want %s", filepath.Base(f), tok.C, key, tok.T, got, expected)
					}
				}
			}
		}
	}
	if checked < 100000 {
		t.Errorf("only %d token colors were checked", checked)
	}
	t.Logf("%d token colors checked", checked)
}
