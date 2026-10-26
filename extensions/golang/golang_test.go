package golang

import (
	"encoding/json"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// TestGoDeclaresGopls checks the declaration of the first language server (SPEC §9.12 "The first server").
func TestGoDeclaresGopls(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.LanguageServers) != 1 {
		t.Fatalf("servers = %+v", m.Contributes.LanguageServers)
	}
	s := m.Contributes.LanguageServers[0]
	if s.ID != "gopls" || s.Program != "gopls" || s.PathSetting != "go.goplsPath" || s.ConfigurationSection != "go.gopls" || !slices.Contains(s.Languages, "go") || s.OnActivation {
		t.Errorf("server = %+v", s)
	}
	// the extension shows the state itself, and the restart command is in the category Go
	if !s.OwnUI || s.Category != "Go" {
		t.Errorf("OwnUI %v, Category %q", s.OwnUI, s.Category)
	}
	if got := extension.TraceSetting(m.ID, s); got != "emetos.golang.gopls.trace.server" {
		t.Errorf("trace setting = %s", got)
	}
	if got := extension.RestartServerCommand(m.ID); got != RestartCommand {
		t.Errorf("restart command = %s, want %s", got, RestartCommand)
	}
}

// TestTheManifestContributesTheGoFeatures checks what step 14.1 adds to the language pack.
func TestTheManifestContributesTheGoFeatures(t *testing.T) {
	m := New().Manifest()
	if m.ID != ID || m.Name != "Go" || m.Contributes.Languages == nil || !slices.Contains(m.ActivationEvents, extension.OnStartup) {
		t.Errorf("manifest = %+v", m)
	}
	commands := map[string]extension.CommandSpec{}
	for _, c := range m.Contributes.Commands {
		commands[c.ID] = c
	}
	for id, title := range map[string]string{InstallToolsCommand: "Install/Update Tools", ShowEnvironmentCommand: "Show Go Environment"} {
		if c, ok := commands[id]; !ok || c.Title != title || c.Category != "Go" {
			t.Errorf("command %s = %+v, %v", id, c, ok)
		}
	}
	// the panel views of the tools (step 14.1), of Go: Run (step 14.2) and of the tests (step 14.3)
	if len(m.Contributes.PanelViews) != 3 || m.Contributes.PanelViews[0].ID != ToolsViewID || m.Contributes.PanelViews[1].ID != RunViewID || m.Contributes.PanelViews[2].ID != TestsViewID {
		t.Errorf("panel views = %+v", m.Contributes.PanelViews)
	}
	for _, topic := range []string{extension.TopicEditorActiveChanged, extension.TopicEditorSelectionChanged, extension.TopicDocumentSaved} {
		if !slices.Contains(m.Events.Subscribes, topic) {
			t.Errorf("subscribes = %v, want %s", m.Events.Subscribes, topic)
		}
	}
}

// TestEveryCallMakesAnExtensionOfItsOwn checks the contract of a Factory: two activations never share an instance.
func TestEveryCallMakesAnExtensionOfItsOwn(t *testing.T) {
	if New() == New() {
		t.Error("New returned the same extension twice")
	}
}

// TestTheGoLanguageBlockHasTheDefaultsOfTheGoExtension checks the settings defaults of the pack (SPEC §10.17: formatting and
// organize imports on save): the package.json of the pack has the "[go]" block of VS Code's Go extension.
func TestTheGoLanguageBlockHasTheDefaultsOfTheGoExtension(t *testing.T) {
	data, err := fs.ReadFile(New().Manifest().Contributes.Languages, "package.json")
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Contributes struct {
			ConfigurationDefaults map[string]map[string]any `json:"configurationDefaults"`
		} `json:"contributes"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatal(err)
	}
	got := pkg.Contributes.ConfigurationDefaults["[go]"]
	want := map[string]any{
		"editor.insertSpaces":      false,
		"editor.formatOnSave":      true,
		"editor.formatOnSaveMode":  "file",
		"editor.codeActionsOnSave": map[string]any{"source.organizeImports": "explicit"},
	}
	if gotJSON, wantJSON := mustJSON(t, got), mustJSON(t, want); gotJSON != wantJSON {
		t.Errorf("the [go] defaults are\n%s\nwant\n%s", gotJSON, wantJSON)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestGoURIAndGoLanguage checks which editors the status item is for.
func TestGoURIAndGoLanguage(t *testing.T) {
	for uri, want := range map[string]bool{
		"file:///a/b.go": true, "file:///a/go.mod": true, "file:///a/go.work": true, "file:///a/b.md": false, "untitled:1": false, "file:///a/mygo.mod": false,
	} {
		if got := goURI(uri); got != want {
			t.Errorf("goURI(%q) = %v", uri, got)
		}
	}
	for id, want := range map[string]bool{"go": true, "go.mod": true, "go.work": true, "go.sum": false, "markdown": false, "": false} {
		if got := goLanguage(id); got != want {
			t.Errorf("goLanguage(%q) = %v", id, got)
		}
	}
	if !strings.HasPrefix(versionLabel("go1.26.1"), "Go 1.26.1") || versionLabel("") != "Go" || versionLabel("devel +abc") != "Go devel +abc" {
		t.Errorf("labels: %q %q %q", versionLabel("go1.26.1"), versionLabel(""), versionLabel("devel +abc"))
	}
}
