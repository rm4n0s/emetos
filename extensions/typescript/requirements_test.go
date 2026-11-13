package typescript

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func tsRequirement(t *testing.T, id string) extension.Requirement {
	t.Helper()
	for _, r := range New().Manifest().Requirements {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no requirement %s", id)
	return extension.Requirement{}
}

func put(t *testing.T, path, content string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// globalInstall lays out npm's global folder: lib/node_modules/{typescript-language-server,typescript} and a link in bin.
func globalInstall(t *testing.T, prefix, tsVersion string) string {
	t.Helper()
	server := put(t, filepath.Join(prefix, "lib", "node_modules", "typescript-language-server", "lib", "cli.mjs"), "#!/bin/sh\necho 5.1.0\n", 0o755)
	lib := filepath.Join(prefix, "lib", "node_modules", "typescript", "lib")
	put(t, filepath.Join(lib, "tsserver.js"), "", 0o644)
	put(t, filepath.Join(prefix, "lib", "node_modules", "typescript", "package.json"), `{"name":"typescript","version":"`+tsVersion+`"}`, 0o644)
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(server, filepath.Join(prefix, "bin", "typescript-language-server")); err != nil {
		t.Fatal(err)
	}
	return lib
}

func TestTheRequirementsOfTypeScript(t *testing.T) {
	if err := extension.CheckManifest(New().Manifest()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		id      string
		setup   func(t *testing.T, prefix, folder string) (map[string]any, string)
		state   extension.RequirementState
		version string
		detail  string
	}{
		{"node new enough", RequirementNode, func(t *testing.T, prefix, _ string) (map[string]any, string) {
			put(t, filepath.Join(prefix, "bin", "node"), "#!/bin/sh\necho v24.1.0\n", 0o755)
			return nil, ""
		}, extension.RequirementFound, "24.1.0", ""},
		{"node too old for the server", RequirementNode, func(t *testing.T, prefix, _ string) (map[string]any, string) {
			put(t, filepath.Join(prefix, "bin", "node"), "#!/bin/sh\necho v20.10.0\n", 0o755)
			return nil, ""
		}, extension.RequirementUnusable, "20.10.0", "node 20.10.0 is too old: 22.22.2 or newer is needed"},
		{"TypeScript beside the server", RequirementTypeScript, func(t *testing.T, prefix, _ string) (map[string]any, string) {
			globalInstall(t, prefix, "6.0.3")
			return nil, ""
		}, extension.RequirementFound, "6.0.3", ""},
		{"the folder's own, chosen", RequirementTypeScript, func(t *testing.T, prefix, folder string) (map[string]any, string) {
			globalInstall(t, prefix, "6.0.3")
			put(t, filepath.Join(folder, "package.json"), "{}", 0o644)
			put(t, filepath.Join(folder, "node_modules", "typescript", "lib", "tsserver.js"), "", 0o644)
			put(t, filepath.Join(folder, "node_modules", "typescript", "package.json"), `{"name":"typescript","version":"5.9.3"}`, 0o644)
			return nil, ChoiceWorkspace
		}, extension.RequirementFound, "5.9.3", ""},
		{"no server and no tsdk", RequirementTypeScript, func(*testing.T, string, string) (map[string]any, string) { return nil, "" },
			extension.RequirementUnknown, "", ""},
		{"a server with no TypeScript beside it", RequirementTypeScript, func(t *testing.T, prefix, _ string) (map[string]any, string) {
			put(t, filepath.Join(prefix, "bin", "typescript-language-server"), "#!/bin/sh\necho 5.1.0\n", 0o755)
			return nil, ""
		}, extension.RequirementMissing, "", ""},
		{"a typescript.tsdk that names nothing", RequirementTypeScript, func(*testing.T, string, string) (map[string]any, string) {
			return map[string]any{TsdkSetting: "/no/typescript"}, ""
		}, extension.RequirementUnusable, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, folder := t.TempDir(), t.TempDir()
			t.Setenv("PATH", filepath.Join(prefix, "bin"))
			t.Setenv("HOME", t.TempDir())
			settings, chosen := tt.setup(t, prefix, folder)
			env := extension.RequirementEnv{Folder: folder,
				Setting: func(k string) (any, bool) { v, ok := settings[k]; return v, ok },
				State: func(k string) (json.RawMessage, bool) {
					if k == StateKey && chosen != "" {
						b, _ := json.Marshal(chosen)
						return b, true
					}
					return nil, false
				}}
			got := tsRequirement(t, tt.id).Check(context.Background(), env)
			if got.State != tt.state || got.Version != tt.version || (tt.detail != "" && got.Detail != tt.detail) {
				t.Errorf("got %+v, want %s %q %q", got, tt.state, tt.version, tt.detail)
			}
		})
	}
}
