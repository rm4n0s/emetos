package javascript

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func jsRequirement(t *testing.T, id string) extension.Requirement {
	t.Helper()
	for _, r := range New().Manifest().Requirements {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no requirement %s", id)
	return extension.Requirement{}
}

func writeExe(t *testing.T, path, out string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho '"+out+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheRequirementsOfJavaScript(t *testing.T) {
	if err := extension.CheckManifest(New().Manifest()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		id      string
		setup   func(t *testing.T, bin, data string) map[string]any
		state   extension.RequirementState
		version string
		detail  string
	}{
		{"node in PATH", RequirementNode, func(t *testing.T, bin, _ string) map[string]any {
			writeExe(t, filepath.Join(bin, "node"), "v22.11.0")
			return nil
		}, extension.RequirementFound, "22.11.0", ""},
		{"node too old", RequirementNode, func(t *testing.T, bin, _ string) map[string]any {
			writeExe(t, filepath.Join(bin, "node"), "v16.20.2")
			return nil
		}, extension.RequirementUnusable, "16.20.2", "node 16.20.2 is too old: 18 or newer is needed"},
		{"node missing", RequirementNode, func(*testing.T, string, string) map[string]any { return nil },
			extension.RequirementMissing, "", "node was not found in PATH: js-debug is a Node.js program"},
		{"node through typescript.nodePath", RequirementNode, func(t *testing.T, _, _ string) map[string]any {
			return map[string]any{NodePathSetting: writeExe(t, filepath.Join(t.TempDir(), "node22"), "v22.1.0")}
		}, extension.RequirementFound, "22.1.0", "found through typescript.nodePath"},
		{"typescript.nodePath that names nothing", RequirementNode, func(*testing.T, string, string) map[string]any {
			return map[string]any{NodePathSetting: "/no/node"}
		}, extension.RequirementMissing, "", "typescript.nodePath is set to /no/node: /no/node does not exist"},
		{"js-debug not downloaded", RequirementJsDebug, func(*testing.T, string, string) map[string]any { return nil },
			extension.RequirementMissing, "", "js-debug was not downloaded: run \"JavaScript Debugger: Download\" or set debug.javascript.adapterPath"},
		{"js-debug downloaded", RequirementJsDebug, func(t *testing.T, _, data string) map[string]any {
			writeExe(t, filepath.Join(data, "emetos", "js-debug", "1.105.0", "js-debug", "src", "dapDebugServer.js"), "")
			return nil
		}, extension.RequirementFound, "1.105.0", ""},
		{"an adapter path that is not js-debug", RequirementJsDebug, func(t *testing.T, _, _ string) map[string]any {
			return map[string]any{AdapterPathSetting: t.TempDir()}
		}, extension.RequirementUnusable, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin, data := filepath.Join(t.TempDir(), "bin"), t.TempDir()
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			t.Setenv("XDG_DATA_HOME", data)
			settings := tt.setup(t, bin, data)
			env := extension.RequirementEnv{Folder: t.TempDir(), Setting: func(k string) (any, bool) { v, ok := settings[k]; return v, ok }}
			got := jsRequirement(t, tt.id).Check(context.Background(), env)
			if got.State != tt.state || got.Version != tt.version || (tt.detail != "" && got.Detail != tt.detail) {
				t.Errorf("got %+v, want %s %q %q", got, tt.state, tt.version, tt.detail)
			}
		})
	}
}
