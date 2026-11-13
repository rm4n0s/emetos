package rust

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheRequirementsOfRust(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	var cargo extension.Requirement
	var ids []string
	for _, r := range m.Requirements {
		ids = append(ids, r.ID)
		if r.ID == RequirementCargo {
			cargo = r
		}
	}
	if len(ids) != 3 || ids[0] != ServerID || ids[1] != RequirementCargo || ids[2] != "native-debugger" {
		t.Fatalf("ids %v", ids)
	}
	tests := []struct {
		name    string
		where   string // "path", "cargo-home" or ""
		state   extension.RequirementState
		version string
	}{
		{"cargo in PATH", "path", extension.RequirementFound, "1.91.0"},
		{"cargo in ~/.cargo/bin", "cargo-home", extension.RequirementFound, "1.91.0"},
		{"no cargo", "", extension.RequirementMissing, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home, bin := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("PATH", bin)
			dir := map[string]string{"path": bin, "cargo-home": filepath.Join(home, ".cargo", "bin")}[tt.where]
			if dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "cargo"), []byte("#!/bin/sh\necho 'cargo 1.91.0 (ea2d97820 2025-10-10)'\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			got := cargo.Check(context.Background(), extension.RequirementEnv{Folder: t.TempDir()})
			if got.State != tt.state || got.Version != tt.version {
				t.Errorf("got %+v", got)
			}
			if tt.state == extension.RequirementMissing && got.Detail != "cargo was not found in PATH or in ~/.cargo/bin" {
				t.Errorf("detail %q", got.Detail)
			}
		})
	}
}
