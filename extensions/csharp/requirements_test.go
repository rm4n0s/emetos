package csharp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheRequirementOfTheDotnetSDK(t *testing.T) {
	if err := extension.CheckManifest(New().Manifest()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		where  string // "root", "path" or ""
		state  extension.RequirementState
		detail string
	}{
		{"DOTNET_ROOT", "root", extension.RequirementFound, "found through DOTNET_ROOT"},
		{"PATH", "path", extension.RequirementFound, "found through PATH"},
		{"none, and nothing is run", "", extension.RequirementMissing, "dotnet was not found in DOTNET_ROOT, PATH or the folders .NET is installed in"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, bin := t.TempDir(), t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PATH", bin)
			t.Setenv("DOTNET_ROOT", "")
			marker := filepath.Join(t.TempDir(), "RAN")
			script := []byte("#!/bin/sh\ntouch " + marker + "\n")
			switch tt.where {
			case "root":
				t.Setenv("DOTNET_ROOT", root)
				if err := os.WriteFile(filepath.Join(root, "dotnet"), script, 0o755); err != nil {
					t.Fatal(err)
				}
			case "path":
				if err := os.WriteFile(filepath.Join(bin, "dotnet"), script, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			c := &csharp{} // no system folders: the machine's own .NET must not count
			got := c.requirements()[0].Check(context.Background(), extension.RequirementEnv{Folder: t.TempDir()})
			if got.State != tt.state || got.Detail != tt.detail || got.Version != "" {
				t.Errorf("got %+v", got)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Error("dotnet was run")
			}
		})
	}
}
