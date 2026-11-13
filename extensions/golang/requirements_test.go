package golang

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// fakeBin writes executable scripts that print a line into a folder and returns the folder.
func fakeBin(t *testing.T, programs map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, out := range programs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho '"+out+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// reqEnv is a RequirementEnv over a map of settings.
func reqEnv(folder string, settings map[string]any) extension.RequirementEnv {
	return extension.RequirementEnv{Folder: folder, Setting: func(k string) (any, bool) { v, ok := settings[k]; return v, ok }}
}

func requirementByID(t *testing.T, id string) extension.Requirement {
	t.Helper()
	for _, r := range New().Manifest().Requirements {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no requirement %s", id)
	return extension.Requirement{}
}

func TestTheManifestDeclaresTheRequirementsOfGo(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{RequirementGo: false, RequirementGopls: false, RequirementDlv: true, RequirementLintTool: true}
	if len(m.Requirements) != len(want) {
		t.Fatalf("%d requirements", len(m.Requirements))
	}
	for _, r := range m.Requirements {
		optional, ok := want[r.ID]
		if !ok || r.Optional != optional {
			t.Errorf("requirement %s optional %v", r.ID, r.Optional)
		}
	}
	if r := requirementByID(t, RequirementGopls); r.PathSetting != GoplsPathSetting || r.InstallAction != InstallToolsCommand ||
		r.Install.Commands[0].Command != "go install golang.org/x/tools/gopls@latest" {
		t.Errorf("gopls %+v", r)
	}
}

func TestTheRequirementsOfGo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	tests := []struct {
		name     string
		id       string
		programs map[string]string
		settings func(bin string) map[string]any
		state    extension.RequirementState
		version  string
		detail   string
		install  string
	}{
		{"go in PATH", RequirementGo, map[string]string{"go": "go version go1.27.1 linux/amd64"}, nil, extension.RequirementFound, "1.27.1", "", ""},
		{"go missing", RequirementGo, nil, nil, extension.RequirementMissing, "", "go was not found in PATH", ""},
		{"go through go.alternateTools", RequirementGo, map[string]string{"go1.26": "go version go1.26.0 linux/amd64"}, func(string) map[string]any {
			return map[string]any{AlternateToolsSetting: map[string]any{"go": "go1.26"}}
		}, extension.RequirementFound, "1.26.0", "", ""},
		{"an alternate go that is not there", RequirementGo, map[string]string{"go": "go version go1.27.1"}, func(string) map[string]any {
			return map[string]any{AlternateToolsSetting: map[string]any{"go": "/no/go"}}
		}, extension.RequirementMissing, "", "go.alternateTools names /no/go for go, which cannot be run", ""},
		{"gopls in PATH", RequirementGopls, map[string]string{"gopls": "golang.org/x/tools/gopls v0.20.0"}, nil, extension.RequirementFound, "0.20.0", "", ""},
		{"gopls through go.goplsPath", RequirementGopls, map[string]string{"my-gopls": "golang.org/x/tools/gopls v0.19.2"}, func(bin string) map[string]any {
			return map[string]any{GoplsPathSetting: filepath.Join(bin, "my-gopls")}
		}, extension.RequirementFound, "0.19.2", "found through go.goplsPath", ""},
		{"go.goplsPath that names nothing", RequirementGopls, map[string]string{"gopls": "golang.org/x/tools/gopls v0.20.0"}, func(string) map[string]any {
			return map[string]any{GoplsPathSetting: "/no/gopls"}
		}, extension.RequirementMissing, "", "go.goplsPath is set to /no/gopls, which cannot be run", ""},
		{"gopls missing", RequirementGopls, nil, nil, extension.RequirementMissing, "", "gopls was not found in PATH", ""},
		{"dlv in PATH", RequirementDlv, map[string]string{"dlv": "Delve Debugger Version: 1.25.1"}, nil, extension.RequirementFound, "1.25.1", "", ""},
		{"dlv missing", RequirementDlv, nil, nil, extension.RequirementMissing, "", "dlv was not found in go.alternateTools, in PATH or in the bin folder of GOBIN or GOPATH", ""},
		{"the default lint tool", RequirementLintTool, map[string]string{"staticcheck": "staticcheck 2025.1.1 (0.6.1)"}, nil, extension.RequirementFound, "2025.1.1", "staticcheck", "go install honnef.co/go/tools/cmd/staticcheck@latest"},
		{"revive, missing", RequirementLintTool, map[string]string{"staticcheck": "staticcheck 2025.1.1"}, func(string) map[string]any {
			return map[string]any{LintToolSetting: "revive"}
		}, extension.RequirementMissing, "", "revive (go.lintTool revive) was not found in PATH or go.alternateTools", "go install github.com/mgechev/revive@latest"},
		{"golangci-lint v2 is the program golangci-lint", RequirementLintTool, map[string]string{"golangci-lint": "golangci-lint has version 2.5.0 built with go1.25"}, func(string) map[string]any {
			return map[string]any{LintToolSetting: "golangci-lint-v2"}
		}, extension.RequirementFound, "2.5.0", "golangci-lint-v2", "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := fakeBin(t, tt.programs)
			t.Setenv("PATH", bin)
			var settings map[string]any
			if tt.settings != nil {
				settings = tt.settings(bin)
			}
			got := requirementByID(t, tt.id).Check(context.Background(), reqEnv(t.TempDir(), settings))
			if got.State != tt.state || got.Version != tt.version || got.Detail != tt.detail {
				t.Errorf("got %+v, want %s %q %q", got, tt.state, tt.version, tt.detail)
			}
			if got.State == extension.RequirementFound && !strings.HasPrefix(got.Path, bin) {
				t.Errorf("path %q", got.Path)
			}
			if tt.install != "" && (len(got.Install.Commands) == 0 || got.Install.Commands[0].Command != tt.install) {
				t.Errorf("install %+v", got.Install)
			}
		})
	}
}

// gopls that Install/Update Tools put in GOPATH/bin, which is not in PATH, is not found by the host: it cannot be used as it is.
func TestGoplsOutsidePathCannotBeUsed(t *testing.T) {
	gopath := t.TempDir()
	tools := filepath.Join(gopath, "bin")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tools, "gopls"), []byte("#!/bin/sh\necho v0.20.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", gopath)
	got := requirementByID(t, RequirementGopls).Check(context.Background(), reqEnv(t.TempDir(), nil))
	want := "gopls is in " + tools + ", which is not in PATH: add the folder to PATH or set go.goplsPath"
	if got.State != extension.RequirementUnusable || got.Detail != want || got.Path != filepath.Join(tools, "gopls") {
		t.Errorf("got %+v", got)
	}
	// dlv in the same place is found: the extension looks there itself
	if err := os.WriteFile(filepath.Join(tools, "dlv"), []byte("#!/bin/sh\necho Version: 1.25.1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := requirementByID(t, RequirementDlv).Check(context.Background(), reqEnv(t.TempDir(), nil)); got.State != extension.RequirementFound || got.Path != filepath.Join(tools, "dlv") {
		t.Errorf("dlv %+v", got)
	}
}
