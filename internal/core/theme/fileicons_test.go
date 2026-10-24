package theme

import (
	"testing"

	"github.com/rm4n0s/errors"
)

// seti loads the bundled Seti theme.
func seti(t *testing.T) *FileIconTheme {
	t.Helper()
	th, err := BundledFileIconTheme(DefaultFileIconTheme)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// definition is the glyph and the color of an icon definition of the Seti theme.
type definition struct {
	glyph rune
	color string
}

func TestSetiIcons(t *testing.T) {
	th := seti(t)
	// the definitions, read from vs-seti-icon-theme.json
	def := map[string]definition{
		"go":        {0xE03A, "#519aba"},
		"go_light":  {0xE03A, "#498ba7"},
		"default":   {0xE023, "#d4d7d6"},
		"default_l": {0xE023, "#bfc2c1"},
		"markdown":  {0xE060, "#519aba"},
		"makefile":  {0xE05F, "#e37933"},
		"json":      {0xE055, "#cbcb41"},
		"license":   {0xE05A, "#cbcb41"},
		"license_l": {0xE05A, "#b7b73b"},
		"image":     {0xE04C, "#a074c4"},
		"testjs":    {0xE051, "#e37933"},
	}
	tests := []struct {
		name  string
		query FileIconQuery
		want  definition
		none  bool
	}{
		{"language beats the default", FileIconQuery{Name: "main.go", LanguageID: "go", Type: TypeDark}, def["go"], false},
		{"file name beats the language", FileIconQuery{Name: "LICENSE", LanguageID: "plaintext", Type: TypeDark}, def["license"], false},
		{"file name in light", FileIconQuery{Name: "LICENSE", Type: TypeLight}, def["license_l"], false},
		{"extension beats the language", FileIconQuery{Name: "logo.png", LanguageID: "foo", Type: TypeDark}, def["image"], false},
		{"longest extension", FileIconQuery{Name: "a.test.js", LanguageID: "javascript", Type: TypeDark}, def["testjs"], false},
		{"markdown language", FileIconQuery{Name: "guide.md", LanguageID: "markdown", Type: TypeDark}, def["markdown"], false},
		{"makefile language", FileIconQuery{Name: "Makefile", LanguageID: "makefile", Type: TypeDark}, def["makefile"], false},
		{"json language", FileIconQuery{Name: "package.json", LanguageID: "json", Type: TypeDark}, def["json"], false},
		{"jsonc takes the icon of json", FileIconQuery{Name: "settings.json", LanguageID: "jsonc", Type: TypeDark}, def["json"], false},
		{"default for an unknown file", FileIconQuery{Name: "data.zzz", Type: TypeDark}, def["default"], false},
		{"default for a plain text file", FileIconQuery{Name: "notes.txt", LanguageID: "plaintext", Type: TypeDark}, def["default"], false},
		{"light variant of a language", FileIconQuery{Name: "main.go", LanguageID: "go", Type: TypeLight}, def["go_light"], false},
		{"light variant of the default", FileIconQuery{Name: "data.zzz", Type: TypeLight}, def["default_l"], false},
		{"folders have none in Seti", FileIconQuery{Kind: FileIconFolder, Name: "src"}, definition{}, true},
		{"expanded folders have none in Seti", FileIconQuery{Kind: FileIconFolder, Name: "src", Expanded: true}, definition{}, true},
		{"root folders have none in Seti", FileIconQuery{Kind: FileIconRootFolder, Name: "project"}, definition{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := th.IconFor(tt.query)
			if ok == tt.none {
				t.Fatalf("IconFor(%+v) found = %v, want %v", tt.query, ok, !tt.none)
			}
			if tt.none {
				return
			}
			if got.Glyph != tt.want.glyph {
				t.Errorf("glyph = %U, want %U", got.Glyph, tt.want.glyph)
			}
			if !got.HasColor || got.Color.Hex() != tt.want.color {
				t.Errorf("color = %s (%v), want %s", got.Color.Hex(), got.HasColor, tt.want.color)
			}
			if got.Family != "seti" || got.Scale != 1.5 {
				t.Errorf("family = %q, scale = %v, want seti and 1.5", got.Family, got.Scale)
			}
		})
	}
}

// icon makes a theme of a few definitions and the associations of the test, in VS Code's format.
func icons(t *testing.T, associations string) *FileIconTheme {
	t.Helper()
	const defs = `"iconDefinitions": {
		"_file": {"fontCharacter": "\\E001", "fontColor": "#111111"},
		"_folder": {"fontCharacter": "\\E002"},
		"_folder_open": {"fontCharacter": "\\E003"},
		"_root": {"fontCharacter": "\\E004"},
		"_root_open": {"fontCharacter": "\\E005"},
		"_nm": {"fontCharacter": "\\E006"},
		"_nm_open": {"fontCharacter": "\\E007"},
		"_go": {"fontCharacter": "\\E010"},
		"_test": {"fontCharacter": "\\E011"},
		"_tgo": {"fontCharacter": "\\E012"},
		"_ext": {"fontCharacter": "\\E013"},
		"_lang": {"fontCharacter": "\\E014"},
		"_name": {"fontCharacter": "\\E015"},
		"_dir": {"fontCharacter": "\\E016"},
		"_hc": {"fontCharacter": "\\E017"},
		"_light": {"fontCharacter": "\\E018", "fontColor": "#222222"},
		"_big": {"fontCharacter": "\\E019", "fontSize": "200%", "fontId": "other"},
		"_none": {"fontColor": "#333333"}
	}, "fonts": [{"id": "main", "size": "125%"}, {"id": "other", "size": "100%"}],`
	th, err := ParseFileIconTheme("test", []byte("{"+defs+associations+"}"))
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func TestFileIconPriority(t *testing.T) {
	th := icons(t, `
	"file": "_file", "folder": "_folder", "folderExpanded": "_folder_open",
	"rootFolder": "_root", "rootFolderExpanded": "_root_open",
	"folderNames": {"node_modules": "_nm", "Src/lib": "_dir"},
	"folderNamesExpanded": {"node_modules": "_nm_open"},
	"languageIds": {"go": "_lang"},
	"fileExtensions": {"go": "_go", "test.go": "_test", "GO.mod": "_ext"},
	"fileNames": {"main.go": "_name", "cmd/main.go": "_dir", "none.x": "_none"},
	"light": {"file": "_light", "fileExtensions": {"go": "_light"}},
	"highContrast": {"file": "_hc"}`)
	glyph := func(q FileIconQuery) rune {
		t.Helper()
		got, ok := th.IconFor(q)
		if !ok {
			return 0
		}
		return got.Glyph
	}
	tests := []struct {
		name string
		q    FileIconQuery
		want rune
	}{
		{"the default file icon", FileIconQuery{Name: "readme", Type: TypeDark}, 0xE001},
		{"a language beats the default", FileIconQuery{Name: "readme", LanguageID: "go", Type: TypeDark}, 0xE014},
		{"an extension beats a language", FileIconQuery{Name: "x.go", LanguageID: "go", Type: TypeDark}, 0xE010},
		{"the longest extension beats a shorter one", FileIconQuery{Name: "x.test.go", LanguageID: "go", Type: TypeDark}, 0xE011},
		{"a name beats the extensions", FileIconQuery{Name: "main.go", LanguageID: "go", Type: TypeDark}, 0xE015},
		{"names are not case sensitive", FileIconQuery{Name: "MAIN.GO", Type: TypeDark}, 0xE015},
		{"extensions are not case sensitive", FileIconQuery{Name: "X.Go", Type: TypeDark}, 0xE010},
		{"an extension key in capitals", FileIconQuery{Name: "a.go.mod", Type: TypeDark}, 0xE013},
		{"a file named like an extension has no extension", FileIconQuery{Name: "go", Type: TypeDark}, 0xE001},
		{"a name in a folder needs the folder", FileIconQuery{Name: "main.go", Parent: "cmd", Type: TypeDark}, 0xE016},
		{"a name in another folder", FileIconQuery{Name: "main.go", Parent: "internal", Type: TypeDark}, 0xE015},
		{"a definition with no character is not an icon", FileIconQuery{Name: "none.x", Type: TypeDark}, 0xE001},
		{"light overrides the extension", FileIconQuery{Name: "x.go", Type: TypeLight}, 0xE018},
		{"light keeps the dark rules it does not override", FileIconQuery{Name: "main.go", Type: TypeLight}, 0xE015},
		{"light default", FileIconQuery{Name: "readme", Type: TypeLight}, 0xE018},
		{"high contrast default", FileIconQuery{Name: "readme", Type: TypeHCDark}, 0xE017},
		{"high contrast light default", FileIconQuery{Name: "readme", Type: TypeHCLight}, 0xE017},
		{"plain text is no language", FileIconQuery{Name: "readme", LanguageID: "plaintext", Type: TypeDark}, 0xE001},
		{"a folder", FileIconQuery{Kind: FileIconFolder, Name: "src"}, 0xE002},
		{"an expanded folder", FileIconQuery{Kind: FileIconFolder, Name: "src", Expanded: true}, 0xE003},
		{"a folder by name", FileIconQuery{Kind: FileIconFolder, Name: "node_modules"}, 0xE006},
		{"an expanded folder by name", FileIconQuery{Kind: FileIconFolder, Name: "Node_Modules", Expanded: true}, 0xE007},
		{"a folder name in a parent", FileIconQuery{Kind: FileIconFolder, Name: "lib", Parent: "src"}, 0xE016},
		{"a folder name in another parent", FileIconQuery{Kind: FileIconFolder, Name: "lib", Parent: "x"}, 0xE002},
		{"the root folder", FileIconQuery{Kind: FileIconRootFolder, Name: "p"}, 0xE004},
		{"the expanded root folder", FileIconQuery{Kind: FileIconRootFolder, Name: "p", Expanded: true}, 0xE005},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := glyph(tt.q); got != tt.want {
				t.Errorf("IconFor(%+v) = %U, want %U", tt.q, got, tt.want)
			}
		})
	}
}

func TestFileIconDetails(t *testing.T) {
	th := icons(t, `"file": "_file", "fileExtensions": {"big": "_big"}`)
	file, _ := th.IconFor(FileIconQuery{Name: "a", Type: TypeDark})
	if !file.HasColor || file.Color.Hex() != "#111111" || file.Family != "main" || file.Scale != 1.25 {
		t.Errorf("file icon = %+v, want #111111 in main at 1.25", file)
	}
	big, _ := th.IconFor(FileIconQuery{Name: "a.big", Type: TypeDark})
	if big.HasColor || big.Family != "other" || big.Scale != 2 {
		t.Errorf("big icon = %+v, want no color, font other at 2", big)
	}
}

func TestFileIconThemeRoutes(t *testing.T) {
	tests := []struct {
		name  string
		build func() error
		route string
	}{
		{"not json", func() error { _, err := ParseFileIconTheme("x", []byte("[")); return err }, "ParseFileIconTheme.FileIconThemeInvalid"},
		{"no definitions", func() error { _, err := ParseFileIconTheme("x", []byte("{}")); return err }, "ParseFileIconTheme.FileIconThemeInvalid"},
		{"unknown theme", func() error { _, err := BundledFileIconTheme("nope"); return err }, "BundledFileIconTheme.FileIconThemeNotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.build()
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestParseFontCharacter(t *testing.T) {
	tests := []struct {
		in   string
		want rune
		ok   bool
	}{
		{"\\E001", 0xE001, true}, {"\\1F600", 0x1F600, true}, {"x", 'x', true}, {"", 0, false}, {"\\zz", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseFontCharacter(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseFontCharacter(%q) = %U, %v; want %U, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
