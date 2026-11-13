package gitscm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheRequirementOfGit(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Requirements) != 1 || m.Requirements[0].ID != RequirementGit {
		t.Fatalf("requirements %+v", m.Requirements)
	}
	r := m.Requirements[0]
	gitScript := func(version string) []byte { return []byte("#!/bin/sh\necho 'git version " + version + "'\n") }
	tests := []struct {
		name    string
		setup   func(t *testing.T, bin string) map[string]any
		state   extension.RequirementState
		version string
		detail  string
	}{
		{"git in PATH", func(t *testing.T, bin string) map[string]any {
			writeExec(t, filepath.Join(bin, "git"), gitScript("2.51.0"))
			return nil
		}, extension.RequirementFound, "2.51.0", ""},
		{"git too old", func(t *testing.T, bin string) map[string]any {
			writeExec(t, filepath.Join(bin, "git"), gitScript("2.25.1"))
			return nil
		}, extension.RequirementUnusable, "2.25.1", "git 2.25.1 is older than 2.30.0"},
		{"git.path wins over PATH", func(t *testing.T, bin string) map[string]any {
			writeExec(t, filepath.Join(bin, "git"), gitScript("2.25.1"))
			other := filepath.Join(t.TempDir(), "git")
			writeExec(t, other, gitScript("2.47.3"))
			return map[string]any{"git.path": []any{other}}
		}, extension.RequirementFound, "2.47.3", ""},
		{"no git", func(*testing.T, string) map[string]any { return nil }, extension.RequirementMissing, "", "git was not found in PATH"},
		{"no git in git.path either", func(*testing.T, string) map[string]any { return map[string]any{"git.path": "/no/git"} },
			extension.RequirementMissing, "", "git was not found in git.path (/no/git) or in PATH"},
		{"Git turned off", func(*testing.T, string) map[string]any { return map[string]any{"git.enabled": false} },
			extension.RequirementUnknown, "", "git.enabled is false: Git is turned off"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			settings := tt.setup(t, bin)
			got := r.Check(context.Background(), extension.RequirementEnv{Folder: t.TempDir(), Setting: func(k string) (any, bool) { v, ok := settings[k]; return v, ok }})
			if got.State != tt.state || got.Version != tt.version || (tt.detail != "" && got.Detail != tt.detail) {
				t.Errorf("got %+v, want %s %q %q", got, tt.state, tt.version, tt.detail)
			}
		})
	}
}

func writeExec(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatal(err)
	}
}
