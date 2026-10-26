package golang

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheSettingsOfTheExtension(t *testing.T) {
	m := New().Manifest()
	byKey := map[string]extension.SettingSpec{}
	for _, s := range m.Contributes.Settings {
		byKey[s.Key] = s
	}
	tests := []struct {
		key  string
		typ  string
		def  string
		text string
	}{
		{GoplsSetting, "object", "{}", "Configure the default Go language server ('gopls')"},
		{AlternateToolsSetting, "object", "{}", "Alternate tools or alternate paths"},
		{ToolsManagementGoSetting, "string", `""`, "install the Go tools"},
		{ToolsGopathSetting, "string", `""`, "Location to install the Go tools"},
		{ToolsEnvVarsSetting, "object", "{}", "Environment variables"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			s, ok := byKey[tt.key]
			if !ok {
				t.Fatal("not declared")
			}
			def, _ := json.Marshal(s.Default)
			if s.Type != tt.typ || string(def) != tt.def || !strings.Contains(s.Description, tt.text) || s.Category != "Go" {
				t.Errorf("spec = %+v", s)
			}
		})
	}
	if len(m.Contributes.Settings) != len(tests) {
		t.Errorf("%d settings declared, want %d", len(m.Contributes.Settings), len(tests))
	}
	// the host registers go.goplsPath for the server
	if path := m.Contributes.LanguageServers[0].PathSetting; path != GoplsPathSetting {
		t.Errorf("path setting = %s", path)
	}
}

// TestTheDescriptionOfGoplsListsEverySettingOfTheServer reads VS Code's list of the settings of gopls and checks that the
// description names each with its type and default.
func TestTheDescriptionOfGoplsListsEverySettingOfTheServer(t *testing.T) {
	props, err := goplsProperties()
	if err != nil {
		t.Fatal(err)
	}
	if len(props) < 40 {
		t.Fatalf("only %d settings of gopls in the list", len(props))
	}
	desc := goplsDescription()
	for name, p := range props {
		def, _ := json.Marshal(p.Default)
		want := "- `" + name + "` (" + typeName(p.Type) + ", default `" + string(def) + "`"
		if !strings.Contains(desc, want) {
			t.Errorf("the description does not have %q", want)
		}
	}
	// spot checks against what the Go extension of VS Code documents
	for name, want := range map[string]string{
		"ui.completion.matcher":         "; one of `CaseInsensitive`, `CaseSensitive`, `Fuzzy`)",
		"formatting.gofumpt":            "(boolean, default `false`)",
		"ui.diagnostic.staticcheck":     "(boolean, default `false`)",
		"build.directoryFilters":        "default `[\"-**/node_modules\"]`",
		"ui.documentation.linksInHover": "(boolean|string, default `true`",
	} {
		if i := strings.Index(desc, "`"+name+"`"); i < 0 || !strings.Contains(desc[i:i+300], want) {
			t.Errorf("%s: want %q near %q", name, want, desc[max(0, strings.Index(desc, "`"+name+"`")):][:min(300, len(desc))])
		}
	}
	if !strings.Contains(desc, "\"go.gopls\": {\"ui.semanticTokens\": true}") {
		t.Error("the description does not say how to write a setting")
	}
}

func TestFirstParagraphAndTypeName(t *testing.T) {
	long := strings.Repeat("word ", 100)
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"one line", "buildFlags is the set of flags.", "buildFlags is the set of flags."},
		{"the first paragraph, on one line", "First line\nsecond line.\n\nAnother paragraph.", "First line second line."},
		{"extra white space", "  a   b \n c ", "a b c"},
		{"cut", long, strings.TrimSpace(strings.Join(strings.Fields(long), " ")[:maxSummary]) + "..."},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstParagraph(tt.in); got != tt.want {
				t.Errorf("firstParagraph = %q, want %q", got, tt.want)
			}
		})
	}
	for _, tt := range []struct {
		in   any
		want string
	}{{"object", "object"}, {[]any{"string", "null"}, "string|null"}, {3, "any"}, {nil, "any"}} {
		if got := typeName(tt.in); got != tt.want {
			t.Errorf("typeName(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
