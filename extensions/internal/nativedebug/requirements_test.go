package nativedebug

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheRequirementOfTheNativeDebugger(t *testing.T) {
	r := Requirement("C and C++")
	if err := extension.CheckManifest(extension.Manifest{ID: "emetos.cpp", Name: "C", Requirements: []extension.Requirement{r}}); err != nil {
		t.Fatal(err)
	}
	if r.Purpose != "debugs C and C++ programs (gdb 14 or newer, or lldb-dap)" || !r.Optional {
		t.Errorf("requirement %+v", r)
	}
	if c, ok := r.Install.CommandFor(extension.FamilyMacOS); !ok || c.Command != "xcode-select --install" {
		t.Errorf("macOS %+v", c)
	}
	tests := []struct {
		name     string
		programs map[string]string
		settings map[string]any
		state    extension.RequirementState
		version  string
		detail   string
	}{
		{"gdb new enough", map[string]string{"gdb": "GNU gdb (Fedora Linux) 16.3-1.fc45", "lldb-dap": "x"}, nil, extension.RequirementFound, "16.3", "gdb"},
		{"gdb too old, lldb-dap there", map[string]string{"gdb": "GNU gdb (Ubuntu 12.1-0ubuntu1~22.04.2) 12.1", "lldb-dap": "x"}, nil, extension.RequirementFound, "", "lldb-dap (gdb 12.1 is too old: 14 or newer is needed)"},
		{"lldb-vscode alone", map[string]string{"lldb-vscode": "x"}, nil, extension.RequirementFound, "", "lldb-dap"},
		{"gdb too old alone", map[string]string{"gdb": "GNU gdb 12.1"}, nil, extension.RequirementUnusable, "", "gdb 12.1 is too old: 14 or newer is needed, and lldb-dap was not found"},
		{"a gdb.path that names nothing", map[string]string{}, map[string]any{GDBPathSetting: "/no/gdb"}, extension.RequirementUnusable, "", "gdb cannot be used: /no/gdb does not exist, and lldb-dap was not found"},
		{"neither", nil, nil, extension.RequirementMissing, "", "neither gdb nor lldb-dap was found in PATH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "bin")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, out := range tt.programs {
				if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho '"+out+"'\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin)
			env := extension.RequirementEnv{Folder: t.TempDir(), Setting: func(k string) (any, bool) { v, ok := tt.settings[k]; return v, ok }}
			got := r.Check(context.Background(), env)
			if got.State != tt.state || got.Version != tt.version || got.Detail != tt.detail {
				t.Errorf("got %+v, want %s %q %q", got, tt.state, tt.version, tt.detail)
			}
		})
	}
}
