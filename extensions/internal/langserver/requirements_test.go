package langserver

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheRequirementOfAServer(t *testing.T) {
	base := t.TempDir()
	bin := filepath.Join(base, "bin")
	cargo := filepath.Join(base, "home", ".cargo", "bin")
	other := filepath.Join(base, "other")
	inst := Install{Docs: "https://example.com/ra", Commands: []InstallCommand{{Family: FamilyAny, Command: "rustup component add rust-analyzer"}}}
	cfg := Config{
		ID: "rust-analyzer", Name: "rust-analyzer", Languages: []string{"rust"}, Purpose: "the language features of Rust",
		Programs: []Program{{Name: "rust-analyzer", PathSetting: "ra.path", After: []string{"~/.cargo/bin"},
			Version: VersionCheck{Args: []string{"--version"}, Min: "1.70"}, Install: inst}},
	}
	reqs := cfg.Requirements()
	if len(reqs) != 1 {
		t.Fatalf("%d requirements", len(reqs))
	}
	r := reqs[0]
	if r.ID != "rust-analyzer" || r.Name != "rust-analyzer" || r.Kind != extension.RequirementProgram || r.Optional || r.PathSetting != "ra.path" || r.Install.Docs != inst.Docs || r.Purpose != cfg.Purpose {
		t.Errorf("requirement %+v", r)
	}
	if err := extension.CheckManifest(extension.Manifest{ID: "emetos.rust", Name: "Rust", Requirements: reqs}); err != nil {
		t.Errorf("the manifest is refused: %v", err)
	}
	tests := []struct {
		name     string
		setup    func(t *testing.T, dir string)
		settings map[string]any
		state    extension.RequirementState
		version  string
		detail   string
	}{
		{"in PATH", func(t *testing.T, d string) {
			program(t, d, "bin/rust-analyzer", "rust-analyzer 1.78.0 (abc 2024-04-29)")
		}, nil, extension.RequirementFound, "1.78.0", "found in PATH"},
		{"after PATH", func(t *testing.T, d string) { program(t, d, "home/.cargo/bin/rust-analyzer", "rust-analyzer 1.80.1") }, nil, extension.RequirementFound, "1.80.1", "found in " + cargo},
		{"through its path setting", func(t *testing.T, d string) { program(t, d, "other/ra", "rust-analyzer 1.79.0") }, map[string]any{"ra.path": filepath.Join(other, "ra")}, extension.RequirementFound, "1.79.0", "found through ra.path"},
		{"missing", func(*testing.T, string) {}, nil, extension.RequirementMissing, "", "rust-analyzer was not found in PATH or the folders the extension searches"},
		{"too old", func(t *testing.T, d string) { program(t, d, "bin/rust-analyzer", "rust-analyzer 1.60.0") }, nil, extension.RequirementUnusable, "1.60.0", "rust-analyzer 1.60.0 is too old: 1.70 or newer is needed"},
		{"a path setting that names nothing", func(t *testing.T, d string) { program(t, d, "bin/rust-analyzer", "rust-analyzer 1.78.0") }, map[string]any{"ra.path": "/nonexistent/ra"}, extension.RequirementUnusable, "", "ra.path is set to /nonexistent/ra, which does not exist"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)
			if tt.detail == "found in "+cargo {
				tt.detail = "found in " + filepath.Join(dir, "home", ".cargo", "bin")
			}
			if s, ok := tt.settings["ra.path"].(string); ok && filepath.IsAbs(s) && filepath.Dir(s) == other {
				tt.settings = map[string]any{"ra.path": filepath.Join(dir, "other", "ra")}
			}
			env := extension.RequirementEnv{Folder: dir, Setting: func(k string) (any, bool) { v, ok := tt.settings[k]; return v, ok }}
			got := cfg.checkRequirement(context.Background(), env, Env{Home: filepath.Join(dir, "home"), Path: filepath.Join(dir, "bin")})
			if got.State != tt.state || got.Version != tt.version || got.Detail != tt.detail {
				t.Errorf("got %+v, want %s %q %q", got, tt.state, tt.version, tt.detail)
			}
			if got.State == extension.RequirementFound && got.Path == "" {
				t.Error("no path")
			}
		})
	}
	_ = bin
}

func TestTheRequirementOfAServerFollowsTheChoiceOfTheSettings(t *testing.T) {
	dir := t.TempDir()
	program(t, dir, "bin/basedpyright-langserver", "1.0.0")
	pylspInstall := Install{Commands: []InstallCommand{{Family: FamilyAny, Command: "pipx install python-lsp-server"}}}
	cfg := Config{
		ID: "python", Name: "Python", Languages: []string{"python"},
		Programs: []Program{
			{Name: "basedpyright", Exe: "basedpyright-langserver", PathSetting: "basedpyright.path", Install: Install{Commands: []InstallCommand{{Family: FamilyAny, Command: "pipx install basedpyright"}}}},
			{Name: "pylsp", PathSetting: "pylsp.path", Install: pylspInstall},
		},
		Select: func(s *Settings) int {
			if s.String("python.languageServer") == "pylsp" {
				return 1
			}
			return 0
		},
		Disabled: func(s *Settings) string {
			if s.String("python.languageServer") == "none" {
				return "python.languageServer is none"
			}
			return ""
		},
	}
	r := cfg.Requirements()[0]
	if r.Name != "Python language server" || r.Purpose != "the language features of Python" || r.PathSetting != "basedpyright.path" {
		t.Errorf("requirement %+v", r)
	}
	tests := []struct {
		name, choice string
		state        extension.RequirementState
		detail       string
		pathSetting  string
		install      bool
	}{
		{"the default", "", extension.RequirementFound, "basedpyright, found in PATH", "", false},
		{"pylsp, not installed, with its own install", "pylsp", extension.RequirementMissing, "pylsp was not found in PATH or the folders the extension searches", "pylsp.path", true},
		{"turned off", "none", extension.RequirementUnknown, "Python is turned off: python.languageServer is none", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := extension.RequirementEnv{Folder: dir, Setting: func(k string) (any, bool) {
				if k == "python.languageServer" {
					return tt.choice, true
				}
				return nil, false
			}}
			got := cfg.checkRequirement(context.Background(), env, Env{Home: dir, Path: filepath.Join(dir, "bin")})
			if got.State != tt.state || got.Detail != tt.detail || got.PathSetting != tt.pathSetting || (len(got.Install.Commands) > 0) != tt.install {
				t.Errorf("got %+v", got)
			}
		})
	}
}

func TestTheRequirementOfAServerUsesThisMachine(t *testing.T) {
	dir := t.TempDir()
	program(t, dir, "bin/clangd", "clangd version 18.1.8")
	t.Setenv("PATH", filepath.Join(dir, "bin"))
	t.Setenv("HOME", dir)
	cfg := Config{ID: "clangd", Name: "clangd", Languages: []string{"c"}, Programs: []Program{{Name: "clangd", Version: VersionCheck{Args: []string{"--version"}}}}}
	got := cfg.Requirements()[0].Check(context.Background(), extension.RequirementEnv{Folder: dir})
	if got.State != extension.RequirementFound || got.Version != "18.1.8" || got.Path != filepath.Join(dir, "bin", "clangd") {
		t.Errorf("got %+v", got)
	}
	if (Config{}).Requirements() != nil {
		t.Error("an invalid Config has requirements")
	}
}

// Customize belongs to an activation (it posts to the UI, keeps what it decided); a check never calls it.
func TestTheRequirementOfAServerDoesNotCustomize(t *testing.T) {
	dir := t.TempDir()
	program(t, dir, "bin/ra", "no version here")
	called := false
	cfg := Config{ID: "ra", Name: "ra", Languages: []string{"rust"},
		Programs:         []Program{{Name: "ra", Version: VersionCheck{Args: []string{"--version"}}}},
		Customize:        func(*Launch, Env) error { called = true; panic("an activation's hook") },
		CustomizeContext: func(context.Context, *Launch, Env) error { called = true; panic("an activation's hook") },
	}
	got := cfg.checkRequirement(context.Background(), extension.RequirementEnv{Folder: dir}, Env{Home: dir, Path: filepath.Join(dir, "bin")})
	if called {
		t.Fatal("Customize was called")
	}
	if got.State != extension.RequirementFound || got.Detail != "found in PATH; its version could not be read" {
		t.Errorf("got %+v", got)
	}
}
